package network

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that supports a SOP class but refuses every transfer syntax proposed
// for it is the commonest negotiation failure in the field: the default proposal
// is the four uncompressed syntaxes, and a modality storing JPEG-LS natively has
// nothing to accept. The A-ASSOCIATE-AC says exactly this — result 4, transfer
// syntaxes not supported — and the information used to be discarded, leaving the
// caller with "no accepted presentation context for SOP Class 1.2.840...", which
// is true and tells them nothing about what to change.
func TestStoreExplainsATransferSyntaxRefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// An SCP that speaks CT Storage but only over RLE, which is not in the
	// default proposal.
	scp := NewSCP(SCPConfig{AETitle: "RLE_ONLY", BindAddress: "127.0.0.1"})
	scp.SetHandler(&StorageHandler{
		OnStore: func(context.Context, string, string, *dataset.Dataset) uint16 {
			return StatusSuccess
		},
	})
	scp.SetSupportedAbstractSyntaxes([]string{CTImageStorageUID})
	scp.SetSupportedTransferSyntaxes([]string{RLELosslessUID})

	addr := serveSCP(ctx, t, scp)

	scu := NewSCU(SCUConfig{CallingAE: "TEST_SCU", CalledAE: "RLE_ONLY", Address: addr})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	err := scu.Store(ctx, ctImageDataset())
	if err == nil {
		t.Fatal("Store succeeded over an association with no accepted context")
	}

	msg := err.Error()
	for _, want := range []string{
		"none of the transfer syntaxes", // which of the two reasons it was
		"1.2.840.10008.1.2.1",           // one of the syntaxes actually proposed
		"AllTransferSyntaxes",           // and the call that fixes it
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q:\n%s", want, msg)
		}
	}
	t.Logf("error reads: %s", msg)
}

// The other refusal reason needs a different fix from the caller, so it must read
// differently.
func TestStoreExplainsAnAbstractSyntaxRefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verification only: the SCP does not do CT Storage at all.
	scp := NewSCP(SCPConfig{AETitle: "ECHO_ONLY", BindAddress: "127.0.0.1"})
	scp.SetHandler(&EchoHandler{})
	scp.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID})

	addr := serveSCP(ctx, t, scp)

	scu := NewSCU(SCUConfig{CallingAE: "TEST_SCU", CalledAE: "ECHO_ONLY", Address: addr})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	err := scu.Store(ctx, ctImageDataset())
	if err == nil {
		t.Fatal("Store succeeded over an association with no accepted context")
	}

	msg := err.Error()
	if !strings.Contains(msg, "does not support that SOP class") {
		t.Errorf("the error does not name the SOP class as the cause:\n%s", msg)
	}
	// Widening the transfer syntaxes would not help here, so it must not be
	// suggested.
	if strings.Contains(msg, "AllTransferSyntaxes") {
		t.Errorf("the error suggests AllTransferSyntaxes for a SOP class refusal, "+
			"which would not fix it:\n%s", msg)
	}
	t.Logf("error reads: %s", msg)
}

// An association where every context is refused is established and useless. From
// the SCP side that used to look like nothing had happened at all, so an operator
// had no way to see why a modality kept failing.
func TestAnSCPReportsRefusingEveryContext(t *testing.T) {
	logged := withConfigLogger(t, slog.LevelDebug)

	// Debug, because an individual refusal is what ordinary negotiation looks
	// like: a requestor proposes broadly and a server accepts what it supports.
	// Only refusing every context is a warning, and this asserts both.
	withDefaultLoggerLevel(t, LogLevelDebug)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	scp := NewSCP(SCPConfig{AETitle: "RLE_ONLY", BindAddress: "127.0.0.1"})
	scp.SetHandler(&StorageHandler{
		OnStore: func(context.Context, string, string, *dataset.Dataset) uint16 {
			return StatusSuccess
		},
	})
	scp.SetSupportedAbstractSyntaxes([]string{CTImageStorageUID})
	scp.SetSupportedTransferSyntaxes([]string{RLELosslessUID})

	addr := serveSCP(ctx, t, scp)

	scu := NewSCU(SCUConfig{CallingAE: "MODALITY", CalledAE: "RLE_ONLY", Address: addr})
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: []string{ExplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	_ = scu.Release(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logged.String(), "refused") {
		time.Sleep(20 * time.Millisecond)
	}

	got := logged.String()
	for _, want := range []string{
		"refused presentation context",
		"MODALITY",            // who was proposing
		"1.2.840.10008.1.2.1", // what they proposed
		"AllTransferSyntaxes", // and how to accept it
		"no usable presentation context",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the SCP did not report %q:\n%s", want, got)
		}
	}
}

// A SOP class the caller never proposed is a third case, and saying "the peer
// refused it" would be wrong — the peer was never asked.
func TestExplainNamesAnUnproposedSOPClass(t *testing.T) {
	assoc := &Association{
		acceptedContexts: map[byte]*PresentationContext{},
		refusedContexts:  map[byte]*PresentationContext{},
	}

	got := assoc.ExplainNoContextFor(CTImageStorageUID)
	if !strings.Contains(got, "not among the presentation contexts proposed") {
		t.Errorf("explanation for an unproposed SOP class reads %q", got)
	}
}

func TestExplainAcrossQueryRetrieveModels(t *testing.T) {
	assoc := &Association{
		acceptedContexts: map[byte]*PresentationContext{},
		refusedContexts: map[byte]*PresentationContext{
			1: {
				ID:             1,
				AbstractSyntax: PatientRootQueryRetrieveFind,
				Result:         PCResultAbstractSyntaxNotSupported,
			},
			3: {
				ID:             3,
				AbstractSyntax: StudyRootQueryRetrieveFind,
				Result:         PCResultTransferSyntaxNotSupported,
				Proposed:       []string{ExplicitVRLittleEndianUID},
			},
		},
	}

	got := assoc.ExplainNoContextForAny(
		PatientRootQueryRetrieveFind, StudyRootQueryRetrieveFind, PatientStudyOnlyQueryRetrieveFind)

	// Both refusals should appear, since which model to reach for depends on both.
	if !strings.Contains(got, PatientRootQueryRetrieveFind) ||
		!strings.Contains(got, StudyRootQueryRetrieveFind) {
		t.Errorf("the explanation covers only some of the models tried: %s", got)
	}
	if !strings.Contains(got, "does not support that SOP class") ||
		!strings.Contains(got, "none of the transfer syntaxes") {
		t.Errorf("the explanation does not distinguish the two reasons: %s", got)
	}
}

func TestBuildContextMapsSplitsAcceptedFromRefused(t *testing.T) {
	requested := []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
		{ID: 3, AbstractSyntax: MRImageStorageUID, TransferSyntaxes: []string{RLELosslessUID}},
	}
	results := []PresentationContextResultItem{
		{ID: 1, Result: PCResultAcceptance, TransferSyntax: ExplicitVRLittleEndianUID},
		{ID: 3, Result: PCResultTransferSyntaxNotSupported, TransferSyntax: RLELosslessUID},
	}

	accepted, refused := BuildContextMaps(requested, results)

	if len(accepted) != 1 || accepted[1] == nil {
		t.Errorf("accepted holds %d contexts, want just context 1", len(accepted))
	}
	if len(refused) != 1 || refused[3] == nil {
		t.Fatalf("refused holds %d contexts, want just context 3", len(refused))
	}
	// The proposed syntaxes are not echoed in the A-ASSOCIATE-AC, so they have to
	// come from the request — without them a refusal cannot say what was tried.
	if len(refused[3].Proposed) != 1 || refused[3].Proposed[0] != RLELosslessUID {
		t.Errorf("refused context 3 records proposed syntaxes %v, want [%s]",
			refused[3].Proposed, RLELosslessUID)
	}

	// BuildAcceptedContextMap is the older API and must keep behaving the same.
	if legacy := BuildAcceptedContextMap(requested, results); len(legacy) != len(accepted) {
		t.Errorf("BuildAcceptedContextMap returned %d contexts, BuildContextMaps %d",
			len(legacy), len(accepted))
	}
}

// serveSCP starts scp on an ephemeral port and returns its address, shutting the
// listener down when the test ends.
func serveSCP(ctx context.Context, t *testing.T, scp *SCP) string {
	t.Helper()

	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// The connection goroutines are waited for, not just abandoned when the
	// listener closes.
	//
	// Without this they outlive the test, and a test that swapped config.Logger for
	// a buffer restores it in its own cleanup — so a handler still running raced the
	// restore, reading the logger as it was being written. Detected under -race on
	// CI rather than locally, because it needs the handler to still be logging at
	// the moment the test ends.
	//
	// Cleanup runs last-in-first-out, and withConfigLogger is called before this,
	// so its restore happens after this wait returns. The test's own `defer cancel()`
	// runs before either, which is what unblocks the reads.
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			transport, acceptErr := ln.Accept(ctx)
			if acceptErr != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				scp.handleConnection(ctx, transport)
			}()
		}
	}()

	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return ln.Addr().String()
}

// ctImageDataset is a minimal CT instance: the two UIDs a C-STORE needs to pick a
// presentation context and name what it is sending.
func ctImageDataset() *dataset.Dataset {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, CTImageStorageUID))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, "1.2.3.4.5.6.7.8.9"))
	return ds
}

// The error tells the caller to associate with contexts built from
// AllTransferSyntaxes(). This follows that advice against the same SCP and
// requires it to work — an error message naming an API is a claim, and an earlier
// draft of this one named SCUConfig.PresentationContexts, a field that does not
// exist.
func TestTheAdviceInTheErrorActuallyWorks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stored := make(chan string, 1)
	scp := NewSCP(SCPConfig{AETitle: "RLE_ONLY", BindAddress: "127.0.0.1"})
	scp.SetHandler(&StorageHandler{
		OnStore: func(_ context.Context, _, sopInstanceUID string, _ *dataset.Dataset) uint16 {
			select {
			case stored <- sopInstanceUID:
			default:
			}
			return StatusSuccess
		},
	})
	scp.SetSupportedAbstractSyntaxes([]string{CTImageStorageUID})
	scp.SetSupportedTransferSyntaxes([]string{RLELosslessUID})

	addr := serveSCP(ctx, t, scp)

	scu := NewSCU(SCUConfig{CallingAE: "TEST_SCU", CalledAE: "RLE_ONLY", Address: addr})

	// Exactly what the error message says to do.
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: AllTransferSyntaxes(),
	}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Store(ctx, ctImageDataset()); err != nil {
		t.Fatalf("Store failed after following the advice in the error message: %v", err)
	}

	select {
	case uid := <-stored:
		if uid != "1.2.3.4.5.6.7.8.9" {
			t.Errorf("the SCP received SOP Instance UID %q", uid)
		}
	case <-time.After(2 * time.Second):
		t.Error("the C-STORE reported success but the handler never ran")
	}
}

// An ordinary association refuses several contexts — an SCU proposing the default
// set has around twenty and any server supports a subset — so refusals must not be
// warnings. Reporting each as one meant every association produced a handful,
// burying the refusals that matter.
func TestOrdinaryNegotiationIsNotReportedAsAProblem(t *testing.T) {
	logged := withConfigLogger(t, slog.LevelDebug)
	withDefaultLoggerLevel(t, LogLevelWarn) // the default: warnings and errors only

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A server that supports verification and CT only, as a real one supports a
	// subset of what a requestor offers.
	scp := NewSCP(SCPConfig{AETitle: "SUBSET", BindAddress: "127.0.0.1"})
	scp.SetHandler(&EchoHandler{})
	scp.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, CTImageStorageUID})

	addr := serveSCP(ctx, t, scp)

	scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "SUBSET", Address: addr})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}

	// The association is usable — verification was accepted — so nothing about it
	// is a problem worth a warning.
	if err := scu.Echo(ctx); err != nil {
		t.Fatalf("Echo: %v", err)
	}
	_ = scu.Release(ctx)

	if got := logged.String(); got != "" {
		t.Errorf("an ordinary association reported %d characters at warning level or above; "+
			"refusing contexts a requestor speculatively proposed is not a problem:\n%s",
			len(got), got)
	}
}

// A requestor that finishes its work and goes leaves the server reading an idle
// connection until the network timeout fires, and a requestor that exits without
// releasing gives the server an EOF. Both are how associations ordinarily end, and
// both were logged at error level — so a completed query produced an error
// describing healthy traffic, and an archive's logs filled with them.
func TestAnAssociationEndingIsNotAnError(t *testing.T) {
	cases := []struct {
		name  string
		close func(t *testing.T, scu *SCU, ctx context.Context)
	}{
		{
			name: "the requestor releases",
			close: func(t *testing.T, scu *SCU, ctx context.Context) {
				if err := scu.Release(ctx); err != nil {
					t.Errorf("Release: %v", err)
				}
			},
		},
		{
			// No release: the peer simply goes. Plenty of tools exit rather than
			// releasing, and there is nothing the server can do about it.
			name: "the requestor vanishes without releasing",
			close: func(t *testing.T, scu *SCU, _ context.Context) {
				if assoc := scu.Association(); assoc != nil {
					_ = assoc.transport.Close()
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logged := withConfigLogger(t, slog.LevelDebug)
			withDefaultLoggerLevel(t, LogLevelWarn) // the default

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			scp := NewSCP(SCPConfig{AETitle: "QUIETSCP", BindAddress: "127.0.0.1"})
			scp.SetHandler(&EchoHandler{})
			scp.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID})

			addr := serveSCP(ctx, t, scp)

			scu := NewSCU(SCUConfig{CallingAE: "SCU", CalledAE: "QUIETSCP", Address: addr})
			if err := scu.Associate(ctx, VerificationPresentationContexts()); err != nil {
				t.Fatalf("Associate: %v", err)
			}
			if err := scu.Echo(ctx); err != nil {
				t.Fatalf("Echo: %v", err)
			}

			tc.close(t, scu, ctx)

			// Give the server a moment to notice and report.
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && logged.Len() == 0 {
				time.Sleep(20 * time.Millisecond)
			}

			if got := logged.String(); got != "" {
				t.Errorf("a completed association reported at warning level or above:\n%s", got)
			}
		})
	}
}
