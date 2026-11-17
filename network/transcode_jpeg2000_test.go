package network_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only JPEG 2000 Lossless for a storage class could not be
// sent a native image: there was no encoder (#308). The coding is lossless,
// so unlike JPEG Baseline nothing has to be asked for, and what arrives
// decodes to what was sent.

// jpeg2000OnlyStore sends ds to an SCP that accepts JPEG 2000 Lossless
// alone, and returns what the SCP received and the error from the send.
func jpeg2000OnlyStore(t *testing.T, ds *dataset.Dataset) (*dataset.Dataset, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "J2K_ONLY", Port: 0, BindAddress: "127.0.0.1",
	}, &network.StorageHandler{
		OnStore: func(_ context.Context, _, _ string, got *dataset.Dataset) uint16 {
			received = got
			return network.StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{"1.2.840.10008.5.1.4.1.1.7"})
	server.SetSupportedTransferSyntaxes([]string{network.JPEG2000LosslessUID})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "J2K_SCU", CalledAE: "J2K_ONLY", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: "1.2.840.10008.5.1.4.1.1.7",
		TransferSyntaxes: []string{network.JPEG2000LosslessUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	err = scu.Store(ctx, ds)
	return received, err
}

// TestNativeImageIsSentAsJPEG2000Lossless: the receiver gets a JPEG 2000
// Lossless image that decodes to the samples sent, not marked lossy, and
// the sender's own data set is as it was.
func TestNativeImageIsSentAsJPEG2000Lossless(t *testing.T) {
	ds, original := nativeGray(t)
	received, err := jpeg2000OnlyStore(t, ds)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if received == nil {
		t.Fatal("nothing was received")
	}
	if got := received.TransferSyntaxUID(); got != network.JPEG2000LosslessUID {
		t.Errorf("received as %q, want JPEG 2000 Lossless", got)
	}
	if got := lossyFlag(received); got != "" {
		t.Errorf("Lossy Image Compression is %q at the receiver: nothing was lost", got)
	}
	decoded, err := received.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding what was received: %v", err)
	}
	if !bytes.Equal(decoded[:len(original)], original) {
		t.Error("what was received does not decode to what was sent")
	}

	// Sending does not rewrite the sender's copy.
	if got := ds.TransferSyntaxUID(); got != network.ExplicitVRLittleEndianUID {
		t.Errorf("the sender's data set became %q", got)
	}
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	if raw, _ := elem.GetValue().([]byte); !bytes.Equal(raw, original) {
		t.Error("the sender's pixel data was changed by the send")
	}
}
