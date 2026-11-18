package network_test

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only lossy JPEG 2000 for a class could not be sent an
// image (#339). It is lossy, so it is sent only at a rate the sender gives in
// NetworkConfig.JPEG2000Rate.

// jpeg2000LossyOnlyStore sends ds, from an SCU with config, to an SCP that
// accepts 1.2.840.10008.1.2.4.91 alone for ds's class, and returns what it
// received and the error from the send.
func jpeg2000LossyOnlyStore(t *testing.T, ds *dataset.Dataset, config network.NetworkConfig) (*dataset.Dataset, error) {
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
		AETitle: "J2K_LOSSY", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedTransferSyntaxes([]string{network.JPEG2000UID})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "J2K_SCU", CalledAE: "J2K_LOSSY", Address: server.Addr(), Network: config,
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: class, TransferSyntaxes: []string{network.JPEG2000UID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	err = scu.Store(ctx, ds)
	return received, err
}

// TestNativeImageIsSentAsLossyJPEG2000 proves pydicom's signed CT_small.dcm
// is refused for a .91-only peer without a rate, naming JPEG2000Rate, and
// arrives with one: in that syntax, marked lossy with ISO_15444_1, its signed
// samples close to what was sent, and the sender's data set unchanged.
func TestNativeImageIsSentAsLossyJPEG2000(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")
	original, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg2000LossyOnlyStore(t, ds, network.NetworkConfig{}); err == nil ||
		!strings.Contains(err.Error(), "JPEG2000Rate") {
		t.Errorf("without a rate: err = %v, want a refusal naming JPEG2000Rate", err)
	}

	received, err := jpeg2000LossyOnlyStore(t, ds, network.NetworkConfig{
		JPEG2000Rate: compress.JPEG2000Rate{Ratio: 10},
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if got := received.TransferSyntaxUID(); got != network.JPEG2000UID {
		t.Errorf("received as %q", got)
	}
	if got := lossyFlag(received); got != "01" {
		t.Errorf("Lossy Image Compression is %q at the receiver", got)
	}
	if method, _ := received.GetTextValue(tag.New(0x0028, 0x2114)); !strings.Contains(method, "ISO_15444_1") {
		t.Errorf("the method is %q", method)
	}
	decoded, err := received.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding what was received: %v", err)
	}
	var sum float64
	for i := 0; i+1 < len(original); i += 2 {
		d := float64(int16(binary.LittleEndian.Uint16(original[i:])) - int16(binary.LittleEndian.Uint16(decoded[i:])))
		sum += d * d
	}
	if p := 10 * math.Log10(65535*65535/(sum/float64(len(original)/2))); p < 60 {
		t.Errorf("CT_small at 10:1 arrived at %.1f dB", p)
	}
	if got := ds.TransferSyntaxUID(); got != network.ExplicitVRLittleEndianUID || lossyFlag(ds) != "" {
		t.Errorf("the sender's data set became %q, lossy %q", got, lossyFlag(ds))
	}
}
