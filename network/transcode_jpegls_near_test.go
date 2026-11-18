package network_test

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only JPEG-LS Lossless and Near-Lossless for a class could
// not be sent an image: the encoder coded NEAR 0 and the syntax was refused by
// name (#337). With no error allowed it is now coded losslessly, which the
// syntax includes, and NetworkConfig.JPEGLSNear is the sender's consent to more.

// jpeglsNearOnlyStore sends ds, from an SCU with config, to an SCP that accepts
// JPEG-LS Near-Lossless alone for ds's class, and returns what it received and
// the error from the send.
func jpeglsNearOnlyStore(t *testing.T, ds *dataset.Dataset, config network.NetworkConfig) (*dataset.Dataset, error) {
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
		AETitle: "JLS_NEAR", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedTransferSyntaxes([]string{network.JPEGLSNearLosslessUID})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "JLS_SCU", CalledAE: "JLS_NEAR", Address: server.Addr(), Network: config,
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: class, TransferSyntaxes: []string{network.JPEGLSNearLosslessUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	err = scu.Store(ctx, ds)
	return received, err
}

// worstSampleError is the largest difference between two buffers of samples
// of bytesPer bytes each, little endian.
func worstSampleError(a, b []byte, bytesPer int) int {
	worst := 0
	for i := 0; i+bytesPer <= len(a) && i+bytesPer <= len(b); i += bytesPer {
		x, y := int(a[i]), int(b[i])
		if bytesPer == 2 {
			x, y = int(binary.LittleEndian.Uint16(a[i:])), int(binary.LittleEndian.Uint16(b[i:]))
		}
		if d := x - y; d > worst {
			worst = d
		} else if -d > worst {
			worst = -d
		}
	}
	return worst
}

// TestNativeImageIsSentAsJPEGLSNearLossless proves a .81-only peer receives,
// with no error allowed, the exact image and no lossy mark; with NEAR 3, an
// image every sample of which is within 3, marked lossy with the method
// ISO_14495_1; and that the sender's data set is unchanged either way.
func TestNativeImageIsSentAsJPEGLSNearLossless(t *testing.T) {
	for _, near := range []int{0, 3} {
		ds, original := nativeGray(t)
		received, err := jpeglsNearOnlyStore(t, ds, network.NetworkConfig{JPEGLSNear: near})
		if err != nil {
			t.Fatalf("NEAR %d: Store: %v", near, err)
		}
		if received == nil {
			t.Fatalf("NEAR %d: nothing was received", near)
		}
		if got := received.TransferSyntaxUID(); got != network.JPEGLSNearLosslessUID {
			t.Errorf("NEAR %d: received as %q", near, got)
		}
		decoded, err := received.DecodedPixelData()
		if err != nil {
			t.Fatalf("NEAR %d: decoding what was received: %v", near, err)
		}
		worst := worstSampleError(decoded[:len(original)], original, 1)
		if worst > near {
			t.Errorf("NEAR %d: a sample arrived %d away", near, worst)
		}
		method, _ := received.GetTextValue(tag.New(0x0028, 0x2114))
		switch {
		case near == 0 && lossyFlag(received) != "":
			t.Errorf("NEAR 0: Lossy Image Compression is %q, and nothing was lost", lossyFlag(received))
		case near > 0 && lossyFlag(received) != "01":
			t.Errorf("NEAR %d: Lossy Image Compression is %q, want 01", near, lossyFlag(received))
		case near > 0 && !strings.Contains(method, "ISO_14495_1"):
			t.Errorf("NEAR %d: the method is %q, want ISO_14495_1", near, method)
		}
		if got := ds.TransferSyntaxUID(); got != network.ExplicitVRLittleEndianUID {
			t.Errorf("NEAR %d: the sender's data set became %q", near, got)
		}
		if lossyFlag(ds) != "" {
			t.Errorf("NEAR %d: the sender's data set was marked lossy", near)
		}
	}
}

// TestSignedImageIsSentAsJPEGLSNearLosslessOnlyExactly proves pydicom's signed
// CT_small.dcm goes to a .81-only peer exactly with no error allowed, and is
// refused, saying why, when an error is allowed: an error on two's complement
// bits wraps the top of the range to the bottom.
func TestSignedImageIsSentAsJPEGLSNearLosslessOnlyExactly(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")
	original, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	received, err := jpeglsNearOnlyStore(t, ds, network.NetworkConfig{})
	if err != nil {
		t.Fatalf("NEAR 0: Store: %v", err)
	}
	decoded, err := received.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	if worst := worstSampleError(decoded, original, 2); worst != 0 || len(decoded) < len(original) {
		t.Errorf("NEAR 0: the CT arrived changed (worst %d)", worst)
	}

	_, err = jpeglsNearOnlyStore(t, ds, network.NetworkConfig{JPEGLSNear: 3})
	if err == nil || !strings.Contains(err.Error(), "signed") {
		t.Errorf("NEAR 3 on a signed image: err = %v, want a refusal naming the sign", err)
	}
}
