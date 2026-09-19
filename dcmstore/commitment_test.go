package dcmstore_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/network"
)

// The compile-time half of #113: a signature change to either side breaks the
// build instead of silently dropping the capability.
var _ network.StorageCommitmentProvider = (*dcmstore.Handler)(nil)

// TestStorageCommitmentAnswersFromTheStore covers what "committed" means for an
// archive built on this package. Successful is a promise that the instance is
// stored durably, so it is made only for an instance the store holds, under the
// class the requestor named, with its file on disk. The index is a cache of the
// files; committing on it alone would promise an instance whose file is gone.
func TestStorageCommitmentAnswersFromTheStore(t *testing.T) {
	ctx := context.Background()
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{"1.2.113.1", "1.2.113.2", "1.2.113.3"} {
		if _, err := store.Store(ctx, instance{
			patientID: "P113", studyUID: "1.2.113", seriesUID: "1.2.113.0", sopInstanceUID: uid,
		}.dataset()); err != nil {
			t.Fatal(err)
		}
	}
	// The third one's file disappears from under the index.
	gone, _ := store.Instance("1.2.113.3")
	if err := os.Remove(filepath.Join(store.Root(), filepath.FromSlash(gone.Path))); err != nil {
		t.Fatal(err)
	}

	const ct = "1.2.840.10008.5.1.4.1.1.2"
	ref := func(class, uid string) network.SOPInstanceReference {
		return network.SOPInstanceReference{SOPClassUID: class, SOPInstanceUID: uid}
	}
	result, err := dcmstore.NewHandler(store).HandleStorageCommitment(ctx, &network.StorageCommitmentRequest{
		TransactionUID: "1.2.113.99",
		Instances: []network.SOPInstanceReference{
			ref(ct, "1.2.113.1"),
			ref(ct, "1.2.113.2"),
			ref(ct, "1.2.113.404"),                        // never stored
			ref("1.2.840.10008.5.1.4.1.1.4", "1.2.113.1"), // stored, but as CT, not MR
			ref(ct, "1.2.113.3"),                          // indexed, file deleted
		},
	})
	if err != nil {
		t.Fatalf("HandleStorageCommitment: %v", err)
	}

	if len(result.Successful) != 2 {
		t.Errorf("committed %d instances, want 2: %+v", len(result.Successful), result.Successful)
	}
	want := map[string]uint16{
		"1.2.113.404": network.StorageCommitmentFailureNoSuchObject,          // 0112H
		"1.2.113.1":   network.StorageCommitmentFailureClassInstanceConflict, // 0119H
		"1.2.113.3":   network.StorageCommitmentFailureProcessingFailure,     // 0110H
	}
	if len(result.Failed) != len(want) {
		t.Errorf("got %d failures, want %d: %+v", len(result.Failed), len(want), result.Failed)
	}
	for _, f := range result.Failed {
		if reason, ok := want[f.SOPInstanceUID]; !ok || f.Reason != reason {
			t.Errorf("%s failed with 0x%04X, want 0x%04X", f.SOPInstanceUID, f.Reason, reason)
		}
	}
	if result.Deferred {
		t.Error("the result was deferred; the store can answer at once, and nothing would send it later")
	}
}

// TestStorageCommitmentStopsWhenCanceled: a canceled context is an error, not a
// result claiming the unchecked instances either way.
func TestStorageCommitmentStopsWhenCanceled(t *testing.T) {
	store, err := dcmstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dcmstore.NewHandler(store).HandleStorageCommitment(ctx, &network.StorageCommitmentRequest{
		TransactionUID: "1.2.113.98",
		Instances:      []network.SOPInstanceReference{{SOPClassUID: "1.2", SOPInstanceUID: "1.2.3"}},
	})
	if err == nil {
		t.Error("a canceled request returned a result")
	}
}
