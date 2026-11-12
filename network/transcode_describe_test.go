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

// TestDecodedColorImageIsDescribedAsDecoded: a compressed instance sent over
// an uncompressed context was decoded, and still described as it had been
// stored. A 4:2:2 JPEG decodes to three samples a pixel; left saying
// YBR_FULL_422, which in native data means two, the receiver reads the bytes
// for the wrong pixels, and the send reports success (#246).
func TestDecodedColorImageIsDescribedAsDecoded(t *testing.T) {
	ds := corpusFile(t, "SC_rgb_dcmtk_+eb+cy+np.dcm")
	photometric := tag.New(0x0028, 0x0004)
	if got := attrText(ds, photometric); got != "YBR_FULL_422" {
		t.Skipf("the fixture is %s, not the 4:2:2 image this is about", got)
	}
	storedBefore, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	before := append([]byte(nil), storedBefore.GetValue().([]byte)...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "DESCRIBE", Port: 0, BindAddress: "127.0.0.1",
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
	server.SetSupportedAbstractSyntaxes([]string{attrText(ds, tag.New(0x0008, 0x0016))})

	scu := network.NewSCU(network.SCUConfig{CallingAE: "DESCRIBE_SCU", CalledAE: "DESCRIBE", Address: server.Addr()})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: attrText(ds, tag.New(0x0008, 0x0016)),
		TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if received == nil {
		t.Fatal("nothing was received")
	}

	if got := attrText(received, photometric); got != "YBR_FULL" {
		t.Errorf("the receiver's Photometric Interpretation is %s, want YBR_FULL: "+
			"the chroma it was sent is at full resolution", got)
	}
	// What it describes has to be what it holds: 100 by 100 pixels of three
	// samples, not the 20000 bytes 4:2:2 would be.
	info, err := received.GetPixelDataInfo()
	if err != nil {
		t.Fatal(err)
	}
	elem, _ := received.Get(tag.New(0x7FE0, 0x0010))
	if px, _ := elem.GetValue().([]byte); len(px) != info.Rows*info.Columns*3 {
		t.Errorf("the receiver holds %d bytes for a %dx%d image", len(px), info.Columns, info.Rows)
	}

	// Sending must not rewrite the sender's data set.
	if got := attrText(ds, photometric); got != "YBR_FULL_422" {
		t.Errorf("the sender's Photometric Interpretation became %s", got)
	}
	after, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	if !bytes.Equal(after.GetValue().([]byte), before) {
		t.Error("the sender's Pixel Data was changed by sending it")
	}
}

// TestRecompressedColorImageIsDescribedAsDecoded: the same holds from one
// compressed syntax to another, which goes through the decoded image. A 4:2:2
// JPEG sent as RLE has chroma for every pixel and said YBR_FULL_422.
func TestRecompressedColorImageIsDescribedAsDecoded(t *testing.T) {
	ds := corpusFile(t, "SC_rgb_dcmtk_+eb+cy+np.dcm")
	want, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var received *dataset.Dataset
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "RECOMPRESS", Port: 0, BindAddress: "127.0.0.1",
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
	sopClass := attrText(ds, tag.New(0x0008, 0x0016))
	server.SetSupportedAbstractSyntaxes([]string{sopClass})
	server.SetSupportedTransferSyntaxes([]string{network.RLELosslessUID})

	scu := network.NewSCU(network.SCUConfig{CallingAE: "RECOMPRESS_SCU", CalledAE: "RECOMPRESS", Address: server.Addr()})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: sopClass, TransferSyntaxes: []string{network.RLELosslessUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if received == nil {
		t.Fatal("nothing was received")
	}
	if got := attrText(received, tag.New(0x0028, 0x0004)); got != "YBR_FULL" {
		t.Errorf("the receiver's Photometric Interpretation is %s, want YBR_FULL", got)
	}
	// RLE is lossless, so what arrives decodes to what the JPEG decoded to.
	got, err := received.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding what was received: %v", err)
	}
	if !bytes.Equal(got[:len(want)], want) {
		t.Error("the received image decodes to other samples than the source")
	}
}

func attrText(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, _ := elem.GetValue().([]byte)
	return strings.TrimRight(string(raw), " \x00")
}
