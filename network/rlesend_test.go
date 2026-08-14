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

// Sending over a context that negotiated a compressed syntax used to fail
// outright: the library decoded pixel data but encoded none, so a native
// instance had nowhere to go. Refusing was right — arriving described as
// compressed while holding native pixels is worse — but it left an archive
// unable to talk to a peer that offers only RLE.
//
// RLE is the one syntax this library can encode, and the encoder already
// existed and was tested. What was missing was the wiring.

// TestNativeInstanceIsCompressedForAnRLEContext stores an uncompressed instance
// over a context that negotiated RLE Lossless, and checks the receiver gets
// fragments that decode to the original pixels.
func TestNativeInstanceIsCompressedForAnRLEContext(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")

	want, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding the fixture locally: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "RLE_SCP", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedTransferSyntaxes([]string{network.RLELosslessUID})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "RLE_SCU", CalledAE: "RLE_SCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: network.CTImageStorageUID,
		TransferSyntaxes: []string{network.RLELosslessUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if received == nil {
		t.Fatal("the receiver got no data set")
	}

	elem, ok := received.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("the receiver got no pixel data")
	}
	value, _ := elem.GetValue().([]byte)

	// Encapsulated data begins with an item tag. Its absence would mean native
	// pixels arrived described as RLE.
	if len(value) < 4 || !bytes.Equal(value[:4], []byte{0xFE, 0xFF, 0x00, 0xE0}) {
		t.Fatal("the receiver got native pixel data described as RLE Lossless")
	}
	if len(value) >= len(want) {
		t.Errorf("the encoded pixel data is %d bytes against %d native; it was not compressed",
			len(value), len(want))
	}

	// The receiver has to be able to decode it back to what was sent.
	received.SetTransferSyntaxUID(network.RLELosslessUID)
	back, err := received.DecodedPixelData()
	if err != nil {
		t.Fatalf("the receiver could not decode what arrived: %v", err)
	}
	if !bytes.Equal(back, want) {
		t.Fatalf("the decoded pixels differ from the original (%d bytes against %d)",
			len(back), len(want))
	}
}

// TestCompressedInstanceIsRecodedForAnRLEContext covers the other starting
// point: pixel data already compressed under a syntax this library decodes is
// decoded and re-encoded.
func TestCompressedInstanceIsRecodedForAnRLEContext(t *testing.T) {
	ds := corpusFile(t, "MR_small_jpeg_ls_lossless.dcm")

	want, err := ds.DecodedPixelData()
	if err != nil {
		t.Skipf("the fixture's pixel data does not decode here: %v", err)
	}

	encoded, err := network.EncodeDataset(ds, network.RLELosslessUID)
	if err != nil {
		t.Fatalf("EncodeDataset to RLE: %v", err)
	}
	decoded, err := network.DecodeDataset(encoded, network.RLELosslessUID)
	if err != nil {
		t.Fatalf("DecodeDataset: %v", err)
	}

	decoded.SetTransferSyntaxUID(network.RLELosslessUID)
	back, err := decoded.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding the re-encoded pixel data: %v", err)
	}
	if !bytes.Equal(back, want) {
		t.Errorf("JPEG-LS re-encoded as RLE does not round trip (%d bytes against %d)",
			len(back), len(want))
	}
}

// TestUnencodableCompressedTargetIsRefused checks the syntaxes this library
// cannot encode still fail rather than sending something mislabeled.
func TestUnencodableCompressedTargetIsRefused(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")

	// JPEG-LS Lossless is no longer here: there is an encoder for it now. What
	// remains has none, and a send that cannot be satisfied has to fail rather
	// than put bytes on the wire described as something they are not — which the
	// receiver could not detect.
	//
	// JPEG-LS *Near*-Lossless stays refused although the same encoder could
	// produce it: it is lossy, and how much error to accept is the caller's
	// decision rather than one to make silently while transcoding.
	for _, target := range []string{
		network.JPEGBaselineUID,
		network.JPEG2000LosslessUID,
		network.JPEGLSNearLosslessUID,
	} {
		t.Run(target, func(t *testing.T) {
			if _, err := network.EncodeDataset(ds, target); err == nil {
				t.Errorf("a native instance encoded for %s; there is no encoder for it", target)
			}
		})
	}
}

// The other half of that: a native instance now encodes for a JPEG-LS context, and
// what comes out has to be JPEG-LS rather than merely labeled as it.
func TestNativeInstanceIsCompressedForAJPEGLSContext(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")

	encoded, err := network.EncodeDataset(ds, network.JPEGLSLosslessUID)
	if err != nil {
		t.Fatalf("encoding a native instance for a JPEG-LS context: %v", err)
	}

	decoded, err := network.DecodeDataset(encoded, network.JPEGLSLosslessUID)
	if err != nil {
		t.Fatalf("decoding what was just encoded: %v", err)
	}

	// The pixel data must be encapsulated fragments carrying a real codestream,
	// and it must decode back to the samples the original held.
	original, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatalf("reading the original pixel data: %v", err)
	}
	decoded.SetTransferSyntaxUID(network.JPEGLSLosslessUID)
	roundTripped, err := decoded.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding the encoded pixel data: %v", err)
	}

	if len(roundTripped) != len(original) {
		t.Fatalf("decoded %d bytes of pixel data, want %d", len(roundTripped), len(original))
	}
	for i := range original {
		if roundTripped[i] != original[i] {
			t.Fatalf("pixel byte %d is 0x%02X after a JPEG-LS round trip, want 0x%02X",
				i, roundTripped[i], original[i])
		}
	}
}
