package network_test

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only JPEG Baseline for a storage class could not be sent
// a native image at all (#243). It can now, when the sender has said how
// much loss it accepts, and not otherwise: nothing is made lossy unasked.

// nativeGray is a 16 by 16, 8-bit Secondary Capture image.
func nativeGray(t *testing.T) (*dataset.Dataset, []byte) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID(network.ExplicitVRLittleEndianUID)
	add := func(tg tag.Tag, vr dataelem.VR, value []byte) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	add(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.7\x00"))
	add(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.826.0.1.3680043.10.511.9.243"))
	add(tag.New(0x0028, 0x0002), dataelem.US, us(1))
	add(tag.New(0x0028, 0x0004), dataelem.CS, []byte("MONOCHROME2 "))
	add(tag.New(0x0028, 0x0010), dataelem.US, us(16))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(16))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))
	px := make([]byte, 256)
	for i := range px {
		px[i] = byte(40 + (i%16)*8 + (i/16)*3)
	}
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, px)
	return ds, px
}

// jpegOnlyStore sends ds to an SCP that accepts JPEG Baseline alone, and
// returns what the SCP received and the error from the send.
func jpegOnlyStore(t *testing.T, ds *dataset.Dataset, quality int) (*dataset.Dataset, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "JPEG_ONLY", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedTransferSyntaxes([]string{network.JPEGBaselineUID})

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "JPEG_SCU", CalledAE: "JPEG_ONLY", Address: server.Addr(),
		Network: network.NetworkConfig{JPEGQuality: quality},
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: "1.2.840.10008.5.1.4.1.1.7",
		TransferSyntaxes: []string{network.JPEGBaselineUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	err = scu.Store(ctx, ds)
	return received, err
}

// TestLossyTranscodingIsNotDoneUnasked: with no quality set the send fails,
// and says what would allow it.
func TestLossyTranscodingIsNotDoneUnasked(t *testing.T) {
	ds, _ := nativeGray(t)
	_, err := jpegOnlyStore(t, ds, 0)
	if err == nil {
		t.Fatal("a native image was sent as JPEG Baseline with no quality asked for")
	}
	if !strings.Contains(err.Error(), "JPEGQuality") {
		t.Errorf("the refusal %q does not name the setting that allows it", err)
	}
}

// TestNativeImageIsSentAsJPEGBaselineWhenAsked: the receiver gets a JPEG
// Baseline image that decodes close to the original, described as lossy, and
// the sender's own data set is as it was.
func TestNativeImageIsSentAsJPEGBaselineWhenAsked(t *testing.T) {
	ds, original := nativeGray(t)
	received, err := jpegOnlyStore(t, ds, 95)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if received == nil {
		t.Fatal("nothing was received")
	}

	if got := received.TransferSyntaxUID(); got != network.JPEGBaselineUID {
		t.Errorf("received as %q, want JPEG Baseline", got)
	}
	if got := lossyFlag(received); got != "01" {
		t.Errorf("Lossy Image Compression is %q at the receiver, want 01", got)
	}
	decoded, err := received.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding what was received: %v", err)
	}
	for i := range original {
		if d := int(decoded[i]) - int(original[i]); d > 4 || d < -4 {
			t.Fatalf("sample %d arrived as %d, sent as %d", i, decoded[i], original[i])
		}
	}

	// Sending does not make the sender's copy lossy.
	if got := ds.TransferSyntaxUID(); got != network.ExplicitVRLittleEndianUID {
		t.Errorf("the sender's data set became %q", got)
	}
	if _, ok := ds.Get(tag.New(0x0028, 0x2110)); ok {
		t.Error("the sender's data set was marked lossy")
	}
}

// lossyFlag reads Lossy Image Compression (0028,2110).
func lossyFlag(ds *dataset.Dataset) string {
	elem, ok := ds.Get(tag.New(0x0028, 0x2110))
	if !ok {
		return ""
	}
	raw, _ := elem.GetValue().([]byte)
	return strings.TrimRight(string(raw), " \x00")
}
