package network

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestCMoveTransfersToDestination is the end-to-end check that C-MOVE moves
// data to a third party. Three parties are involved, which is what
// distinguishes C-MOVE from C-GET: a requestor asks the source to send
// instances to a destination it is not itself.
func TestCMoveTransfersToDestination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// --- The destination: an ordinary storage SCP ---
	var mu sync.Mutex
	var stored []string

	destination, err := StartServer(ctx, SCPConfig{
		AETitle: "DEST_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			stored = append(stored, sopInstance)
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer (destination): %v", err)
	}
	defer destination.Stop()
	destination.SetSupportedAbstractSyntaxes([]string{
		VerificationSOPClassUID, CTImageStorageUID, MRImageStorageUID,
	})

	want := []*dataset.Dataset{
		makeInstance(CTImageStorageUID, "1.2.9.1", "Move^One"),
		makeInstance(CTImageStorageUID, "1.2.9.2", "Move^Two"),
		makeInstance(MRImageStorageUID, "1.2.9.3", "Move^Three"),
	}

	// --- The source: holds the instances and performs the move ---
	source, err := StartServer(ctx, SCPConfig{
		AETitle: "SRC_SCP", Port: 0, BindAddress: "127.0.0.1",
		// Resolving the destination AE title is what makes the move possible.
		MoveDestinations: map[string]string{
			"DEST_SCP": destination.Addr(),
		},
	}, &QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return want, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer (source): %v", err)
	}
	defer source.Stop()

	// --- The requestor: asks the source to move to the destination ---
	scu := NewSCU(SCUConfig{
		CallingAE: "MOVE_SCU", CalledAE: "SRC_SCP", Address: source.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	query := dataset.NewDataset()
	_ = query.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte("STUDY ")))

	if err := scu.Move(ctx, query, "DEST_SCP"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(stored) != len(want) {
		t.Fatalf("destination received %d instances, want %d (got %v)", len(stored), len(want), stored)
	}
	for i, uid := range []string{"1.2.9.1", "1.2.9.2", "1.2.9.3"} {
		if stored[i] != uid {
			t.Errorf("instance %d: SOP Instance = %q, want %q", i, stored[i], uid)
		}
	}
}

// TestCMoveUnknownDestinationIsReported verifies that an unresolvable
// destination AE title fails cleanly with the status the standard defines,
// rather than hanging or reporting success.
func TestCMoveUnknownDestinationIsReported(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	source, err := StartServer(ctx, SCPConfig{
		AETitle: "SRC_NODEST", Port: 0, BindAddress: "127.0.0.1",
		// No MoveDestinations configured.
	}, &QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{makeInstance(CTImageStorageUID, "1.2.9.9", "Nowhere^One")}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer source.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "MOVE_SCU", CalledAE: "SRC_NODEST", Address: source.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	err = scu.Move(ctx, dataset.NewDataset(), "GHOST_AE")
	if err == nil {
		t.Fatal("expected an error for an unresolvable destination, got nil")
	}

	dimseErr, ok := err.(*DIMSEError)
	if !ok {
		t.Fatalf("expected *DIMSEError, got %T: %v", err, err)
	}
	if dimseErr.Status != StatusMoveDestUnknown {
		t.Errorf("status = 0x%04X, want 0x%04X (Move Destination Unknown)",
			dimseErr.Status, StatusMoveDestUnknown)
	}
}

// TestCMoveResolverTakesPrecedence verifies the resolver function is consulted
// ahead of the static map, which is how a caller backs destinations with a
// database rather than configuration.
func TestCMoveResolverTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var mu sync.Mutex
	var stored []string

	destination, err := StartServer(ctx, SCPConfig{
		AETitle: "DEST2", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, sopInstance string, _ *dataset.Dataset) uint16 {
			mu.Lock()
			stored = append(stored, sopInstance)
			mu.Unlock()
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer (destination): %v", err)
	}
	defer destination.Stop()
	destination.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, CTImageStorageUID})

	var resolverCalls int
	source, err := StartServer(ctx, SCPConfig{
		AETitle: "SRC2", Port: 0, BindAddress: "127.0.0.1",
		// The map points somewhere useless; the resolver must win.
		MoveDestinations: map[string]string{"DEST2": "127.0.0.1:1"},
		ResolveMoveDestination: func(aeTitle string) (string, bool) {
			resolverCalls++
			if aeTitle == "DEST2" {
				return destination.Addr(), true
			}
			return "", false
		},
	}, &QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return []*dataset.Dataset{makeInstance(CTImageStorageUID, "1.2.9.20", "Resolved^One")}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer (source): %v", err)
	}
	defer source.Stop()

	scu := NewSCU(SCUConfig{CallingAE: "MOVE_SCU", CalledAE: "SRC2", Address: source.Addr()})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Move(ctx, dataset.NewDataset(), "DEST2"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if resolverCalls == 0 {
		t.Error("the resolver was never consulted")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(stored) != 1 || stored[0] != "1.2.9.20" {
		t.Errorf("destination received %v, want [1.2.9.20]", stored)
	}
}

// TestCMoveWithNoMatchesSucceeds verifies a query matching nothing completes
// without contacting a destination.
func TestCMoveWithNoMatchesSucceeds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	source, err := StartServer(ctx, SCPConfig{
		AETitle: "SRC_EMPTY", Port: 0, BindAddress: "127.0.0.1",
	}, &QueryRetrieveHandler{
		OnMoveInstances: func(_ context.Context, _, _ string, _ *dataset.Dataset) ([]*dataset.Dataset, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer source.Stop()

	scu := NewSCU(SCUConfig{CallingAE: "MOVE_SCU", CalledAE: "SRC_EMPTY", Address: source.Addr()})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// No destination is configured, but none is needed when nothing matches.
	if err := scu.Move(ctx, dataset.NewDataset(), "GHOST_AE"); err != nil {
		t.Fatalf("Move with no matches: %v", err)
	}
}

// TestStorageContextsFor verifies presentation contexts are built for each
// distinct SOP Class, with the odd IDs the standard requires.
func TestStorageContextsFor(t *testing.T) {
	instances := []*dataset.Dataset{
		makeInstance(CTImageStorageUID, "1.1", "A"),
		makeInstance(CTImageStorageUID, "1.2", "B"), // duplicate SOP Class
		makeInstance(MRImageStorageUID, "1.3", "C"),
		nil, // must be tolerated
	}

	contexts := storageContextsFor(instances)

	if len(contexts) != 2 {
		t.Fatalf("got %d contexts, want 2 (one per distinct SOP Class)", len(contexts))
	}
	seen := map[string]bool{}
	for _, c := range contexts {
		if c.ID%2 == 0 {
			t.Errorf("context ID %d is even; presentation context IDs must be odd", c.ID)
		}
		if len(c.TransferSyntaxes) == 0 {
			t.Errorf("context %d proposes no transfer syntax", c.ID)
		}
		seen[c.AbstractSyntax] = true
	}
	if !seen[CTImageStorageUID] || !seen[MRImageStorageUID] {
		t.Errorf("contexts cover %v, want both CT and MR storage", seen)
	}
}
