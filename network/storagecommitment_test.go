package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

const ctImageStorage = "1.2.840.10008.5.1.4.1.1.2"

func instanceRefs(uids ...string) []SOPInstanceReference {
	refs := make([]SOPInstanceReference, len(uids))
	for i, uid := range uids {
		refs[i] = SOPInstanceReference{SOPClassUID: ctImageStorage, SOPInstanceUID: uid}
	}
	return refs
}

// TestStorageCommitmentEndToEnd runs the full flow over a real connection:
// N-ACTION out, N-ACTION-RSP back, then N-EVENT-REPORT with the outcome and its
// acknowledgement.
//
// The result is checked in both directions — what the handler was asked to
// commit, and what the requestor learned — because a service that reports
// success for instances nobody asked about, or loses the ones that failed, is
// worse than one that refuses outright: the requestor deletes its only copy on
// the strength of the answer.
func TestStorageCommitmentEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const transactionUID = "1.2.826.0.1.3680043.8.498.77701"

	var sawRequest *StorageCommitmentRequest
	handler := &StorageCommitmentHandler{
		OnCommit: func(_ context.Context, req *StorageCommitmentRequest) (*StorageCommitmentResult, error) {
			sawRequest = req
			return &StorageCommitmentResult{
				Successful: instanceRefs("1.2.3.1", "1.2.3.2"),
				Failed: []StorageCommitmentFailure{{
					SOPInstanceReference: SOPInstanceReference{
						SOPClassUID: ctImageStorage, SOPInstanceUID: "1.2.3.3",
					},
					Reason: StorageCommitmentFailureNoSuchObject,
				}},
			}, nil
		},
	}

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "COMMIT_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "COMMIT_SCU", CalledAE: "COMMIT_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	resp, err := scu.RequestStorageCommitment(ctx, &StorageCommitmentRequest{
		TransactionUID: transactionUID,
		Instances:      instanceRefs("1.2.3.1", "1.2.3.2", "1.2.3.3"),
	})
	if err != nil {
		t.Fatalf("RequestStorageCommitment: %v", err)
	}
	if resp.Status != StatusSuccess {
		t.Fatalf("N-ACTION status = 0x%04X, want success", resp.Status)
	}

	// What the SCP was asked to commit.
	if sawRequest == nil {
		t.Fatal("the handler was never called")
	}
	if sawRequest.TransactionUID != transactionUID {
		t.Errorf("handler saw transaction %q, want %q", sawRequest.TransactionUID, transactionUID)
	}
	if len(sawRequest.Instances) != 3 {
		t.Fatalf("handler saw %d instances, want 3", len(sawRequest.Instances))
	}
	if got := sawRequest.Instances[0]; got.SOPInstanceUID != "1.2.3.1" || got.SOPClassUID != ctImageStorage {
		t.Errorf("first instance = %+v, want CT storage / 1.2.3.1", got)
	}

	// What the requestor learned.
	result, err := scu.ReceiveStorageCommitmentResult(ctx)
	if err != nil {
		t.Fatalf("ReceiveStorageCommitmentResult: %v", err)
	}

	if result.TransactionUID != transactionUID {
		t.Errorf("result transaction = %q, want %q — the requestor cannot match this to its request",
			result.TransactionUID, transactionUID)
	}
	if len(result.Successful) != 2 {
		t.Errorf("got %d committed, want 2", len(result.Successful))
	}
	if len(result.Failed) != 1 {
		t.Fatalf("got %d failures, want 1", len(result.Failed))
	}
	if result.Failed[0].SOPInstanceUID != "1.2.3.3" {
		t.Errorf("failed instance = %q, want 1.2.3.3", result.Failed[0].SOPInstanceUID)
	}
	if result.Failed[0].Reason != StorageCommitmentFailureNoSuchObject {
		t.Errorf("failure reason = 0x%04X, want 0x%04X",
			result.Failed[0].Reason, StorageCommitmentFailureNoSuchObject)
	}
}

// TestStorageCommitmentRefusedWithoutProvider verifies a handler that cannot
// commit says so rather than accepting silently.
//
// Silent acceptance is the dangerous outcome: the requestor reads success and
// deletes its only copy of instances nobody promised to keep.
func TestStorageCommitmentRefusedWithoutProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// EchoHandler does not implement StorageCommitmentProvider.
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "NOCOMMIT", Port: 0, BindAddress: "127.0.0.1",
	}, &EchoHandler{})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "COMMIT_SCU", CalledAE: "NOCOMMIT", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// The refusal must come at negotiation, not after the request. A peer that
	// negotiates the context successfully has been told the service is
	// available, and only finds out otherwise after building a transaction and
	// sending it.
	accepted := scu.Association().AcceptedContexts()
	if _, ok := FindPresentationContextID(accepted, StorageCommitmentPushModelUID); ok {
		t.Error("the storage commitment context was accepted by an SCP that cannot provide it")
	}

	_, err = scu.RequestStorageCommitment(ctx, &StorageCommitmentRequest{
		TransactionUID: "1.2.826.0.1.3680043.8.498.77702",
		Instances:      instanceRefs("1.2.3.1"),
	})
	if err == nil {
		t.Error("the request succeeded against an SCP with no commitment provider")
	}
}

// TestStorageCommitmentNegotiatedWhenProvided is the other half: an SCP that
// can commit must offer the context, or no requestor can ever use it.
func TestStorageCommitmentNegotiatedWhenProvided(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "CANCOMMIT", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageCommitmentHandler{
		OnCommit: func(_ context.Context, req *StorageCommitmentRequest) (*StorageCommitmentResult, error) {
			return &StorageCommitmentResult{Successful: req.Instances}, nil
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := NewSCU(SCUConfig{
		CallingAE: "COMMIT_SCU", CalledAE: "CANCOMMIT", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, nil); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	accepted := scu.Association().AcceptedContexts()
	if _, ok := FindPresentationContextID(accepted, StorageCommitmentPushModelUID); !ok {
		t.Fatal("an SCP that provides storage commitment did not offer the context")
	}
}

// TestStorageCommitmentEventTypeFollowsFailures verifies the event type is
// derived from the result rather than chosen independently, so a report cannot
// claim everything succeeded while listing failures.
func TestStorageCommitmentEventTypeFollowsFailures(t *testing.T) {
	complete := &StorageCommitmentResult{
		TransactionUID: "1.2.3",
		Successful:     instanceRefs("1.2.3.1"),
	}
	if got := complete.EventTypeID(); got != StorageCommitmentEventComplete {
		t.Errorf("event type = %d for a result with no failures, want %d",
			got, StorageCommitmentEventComplete)
	}

	partial := &StorageCommitmentResult{
		TransactionUID: "1.2.3",
		Successful:     instanceRefs("1.2.3.1"),
		Failed: []StorageCommitmentFailure{{
			SOPInstanceReference: SOPInstanceReference{SOPInstanceUID: "1.2.3.2"},
			Reason:               StorageCommitmentFailureProcessingFailure,
		}},
	}
	if got := partial.EventTypeID(); got != StorageCommitmentEventFailures {
		t.Errorf("event type = %d for a result with failures, want %d",
			got, StorageCommitmentEventFailures)
	}
}

// TestStorageCommitmentDataSetRoundTrip covers the encoding on its own, so a
// failure in the wire format is distinguishable from a failure in the flow.
func TestStorageCommitmentDataSetRoundTrip(t *testing.T) {
	t.Run("request", func(t *testing.T) {
		want := &StorageCommitmentRequest{
			// An odd-length UID, so the even-length padding is exercised. A UID
			// pads with NUL, and returning that padding as part of the value
			// would break the match against the result.
			TransactionUID: "1.2.826.0.1.3680043.8.498.7",
			Instances:      instanceRefs("1.2.3.1", "1.2.3.22"),
		}

		ds, err := BuildStorageCommitmentRequest(want)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got, err := ParseStorageCommitmentRequest(ds)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		if got.TransactionUID != want.TransactionUID {
			t.Errorf("transaction UID = %q, want %q", got.TransactionUID, want.TransactionUID)
		}
		if len(got.Instances) != len(want.Instances) {
			t.Fatalf("got %d instances, want %d", len(got.Instances), len(want.Instances))
		}
		for i := range want.Instances {
			if got.Instances[i] != want.Instances[i] {
				t.Errorf("instance %d = %+v, want %+v", i, got.Instances[i], want.Instances[i])
			}
		}
	})

	t.Run("result", func(t *testing.T) {
		want := &StorageCommitmentResult{
			TransactionUID: "1.2.826.0.1.3680043.8.498.7",
			Successful:     instanceRefs("1.2.3.1"),
			Failed: []StorageCommitmentFailure{{
				SOPInstanceReference: SOPInstanceReference{
					SOPClassUID: ctImageStorage, SOPInstanceUID: "1.2.3.2",
				},
				Reason: StorageCommitmentFailureResourceLimitation,
			}},
		}

		ds, err := BuildStorageCommitmentResult(want)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		got, err := ParseStorageCommitmentResult(ds)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}

		if got.TransactionUID != want.TransactionUID {
			t.Errorf("transaction UID = %q, want %q", got.TransactionUID, want.TransactionUID)
		}
		if len(got.Successful) != 1 || got.Successful[0] != want.Successful[0] {
			t.Errorf("successful = %+v, want %+v", got.Successful, want.Successful)
		}
		if len(got.Failed) != 1 {
			t.Fatalf("got %d failures, want 1", len(got.Failed))
		}
		if got.Failed[0] != want.Failed[0] {
			t.Errorf("failure = %+v, want %+v", got.Failed[0], want.Failed[0])
		}
	})
}

// TestStorageCommitmentRejectsIncompleteRequests covers the two fields without
// which the exchange is meaningless.
func TestStorageCommitmentRejectsIncompleteRequests(t *testing.T) {
	if _, err := BuildStorageCommitmentRequest(&StorageCommitmentRequest{
		Instances: instanceRefs("1.2.3.1"),
	}); err == nil {
		t.Error("a request with no Transaction UID was accepted; the result could never be matched to it")
	}

	if _, err := BuildStorageCommitmentRequest(&StorageCommitmentRequest{
		TransactionUID: "1.2.3",
	}); err == nil {
		t.Error("a request naming no instances was accepted")
	}

	if _, err := BuildStorageCommitmentResult(&StorageCommitmentResult{
		Successful: instanceRefs("1.2.3.1"),
	}); err == nil {
		t.Error("a result with no Transaction UID was accepted")
	}
}

// TestNDIMSERequestsUseRequestedSOPInstanceUID guards the tag that four
// N-DIMSE requests name their target with.
//
// N-GET, N-SET, N-ACTION and N-DELETE identify their target with Requested SOP
// Instance UID (0000,1001). This library sent Affected SOP Instance UID
// (0000,1000) instead — the element for messages that create or report on an
// instance — and its own SCP read it back from the same wrong tag, so every
// test here passed while no other implementation could see a target at all.
//
// The check is on the encoded command, not on a round trip, because a round
// trip is exactly what failed to detect this.
func TestNDIMSERequestsUseRequestedSOPInstanceUID(t *testing.T) {
	const (
		sopClass    = "1.2.840.10008.1.20.1"
		sopInstance = "1.2.840.10008.1.20.1.1"
	)

	requested := []struct {
		name string
		ds   *dataset.Dataset
	}{
		{"N-GET", BuildNGetRQ(1, sopClass, sopInstance)},
		{"N-SET", BuildNSetRQ(2, sopClass, sopInstance)},
		{"N-ACTION", BuildNActionRQ(3, sopClass, sopInstance, 1, true)},
		{"N-DELETE", BuildNDeleteRQ(4, sopClass, sopInstance)},
	}
	for _, tc := range requested {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := getUIValue(tc.ds, tagRequestedSOPInstanceUID); err != nil || got != sopInstance {
				t.Errorf("Requested SOP Instance UID (0000,1001) = %q (err %v), want %q — "+
					"no peer can tell what this message targets", got, err, sopInstance)
			}
			if _, err := getUIValue(tc.ds, tagAffectedSOPInstanceUID); err == nil {
				t.Errorf("Affected SOP Instance UID (0000,1000) is present; %s names its "+
					"target with Requested, and sending both is contradictory", tc.name)
			}
		})
	}

	// The other two genuinely use Affected: they report on or create an
	// instance rather than acting on an existing one.
	affected := []struct {
		name string
		ds   *dataset.Dataset
	}{
		{"N-EVENT-REPORT", BuildNEventReportRQ(5, sopClass, sopInstance, 1, true)},
		{"N-CREATE", BuildNCreateRQ(6, sopClass, sopInstance, true)},
	}
	for _, tc := range affected {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := getUIValue(tc.ds, tagAffectedSOPInstanceUID); err != nil || got != sopInstance {
				t.Errorf("Affected SOP Instance UID = %q (err %v), want %q", got, err, sopInstance)
			}
		})
	}
}

// TestRequestedSOPInstanceUIDFallsBackToAffected covers peers that make the
// mistake this library used to make. Refusing them gains nothing.
func TestRequestedSOPInstanceUIDFallsBackToAffected(t *testing.T) {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPInstanceUID, "1.2.3.4")

	got, err := GetRequestedSOPInstanceUID(ds)
	if err != nil {
		t.Fatalf("GetRequestedSOPInstanceUID: %v", err)
	}
	if got != "1.2.3.4" {
		t.Errorf("got %q, want the Affected value as a fallback", got)
	}
}
