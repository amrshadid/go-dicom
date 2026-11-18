package network_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only lossless JPEG for a storage class could not be sent
// a native image: there was no encoder (#336). The coding is lossless, so as
// for JPEG 2000 nothing has to be asked for, and what arrives decodes to what
// was sent.

// onlySyntaxStore sends ds, as an SCU configured with config, to an SCP that
// accepts syntax alone for ds's class, and returns what the SCP received and
// the error from the send.
func onlySyntaxStore(t *testing.T, ds *dataset.Dataset, syntax string,
	config network.NetworkConfig) (*dataset.Dataset, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	class, err := ds.GetTextValue(tag.New(0x0008, 0x0016))
	if err != nil {
		t.Fatalf("the data set has no SOP Class UID: %v", err)
	}
	class = strings.TrimRight(class, " \x00")

	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "ONE_SYNTAX", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedAbstractSyntaxes([]string{class})
	server.SetSupportedTransferSyntaxes([]string{syntax})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "SENDER", CalledAE: "ONE_SYNTAX", Address: server.Addr(), Network: config,
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: class, TransferSyntaxes: []string{syntax},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	err = scu.Store(ctx, ds)
	return received, err
}

// TestNativeImageIsSentAsLosslessJPEG proves, for both lossless JPEG
// syntaxes, that the receiver gets the syntax it accepted, that the image
// decodes to the samples sent and is not marked lossy, and that the sender's
// own data set is as it was. An 8-bit generated image, and pydicom's 16-bit
// signed CT_small.dcm when the corpus is there.
func TestNativeImageIsSentAsLosslessJPEG(t *testing.T) {
	images := map[string]func(t *testing.T) *dataset.Dataset{
		"8-bit": func(t *testing.T) *dataset.Dataset {
			ds, _ := nativeGray(t)
			return ds
		},
		"CT_small.dcm": func(t *testing.T) *dataset.Dataset { return corpusFile(t, "CT_small.dcm") },
	}
	for _, syntax := range []string{network.JPEGLosslessProcess14UID, network.JPEGLosslessSV1UID} {
		for name, load := range images {
			t.Run(syntax+" "+name, func(t *testing.T) {
				ds := load(t)
				original, err := ds.DecodedPixelData()
				if err != nil {
					t.Fatal(err)
				}
				before := ds.TransferSyntaxUID()
				received, err := onlySyntaxStore(t, ds, syntax, network.NetworkConfig{})
				if err != nil {
					t.Fatalf("Store: %v", err)
				}
				if received == nil {
					t.Fatal("nothing was received")
				}
				if got := received.TransferSyntaxUID(); got != syntax {
					t.Errorf("received as %q, want %s", got, syntax)
				}
				if got := lossyFlag(received); got != "" {
					t.Errorf("Lossy Image Compression is %q at the receiver: nothing was lost", got)
				}
				decoded, err := received.DecodedPixelData()
				if err != nil {
					t.Fatalf("decoding what was received: %v", err)
				}
				if len(decoded) < len(original) || !bytes.Equal(decoded[:len(original)], original) {
					t.Error("what was received does not decode to what was sent")
				}
				if got := ds.TransferSyntaxUID(); got != before {
					t.Errorf("the sender's data set became %q", got)
				}
			})
		}
	}
}
