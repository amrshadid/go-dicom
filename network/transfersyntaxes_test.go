package network

import (
	"context"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestDefaultTransferSyntaxesCoverTheUncompressedFamily verifies all four
// uncompressed syntaxes are negotiated, not two.
//
// The data set codec handles each of them, including the byte swap for big
// endian and the deflate stream. Only Implicit and Explicit VR Little Endian
// were listed, so a peer offering deflated or big endian data had no context to
// send it on even though this library reads both from disk.
func TestDefaultTransferSyntaxesCoverTheUncompressedFamily(t *testing.T) {
	got := make(map[string]bool)
	for _, ts := range DefaultTransferSyntaxes() {
		got[ts] = true
	}

	for _, want := range []string{
		ImplicitVRLittleEndianUID,
		ExplicitVRLittleEndianUID,
		DeflatedExplicitVRLittleEndianUID,
		ExplicitVRBigEndianUID,
	} {
		if !got[want] {
			t.Errorf("%s is not negotiated by default, though the codec supports it", want)
		}
	}

	// Compressed syntaxes stay opt-in, so a default association request does
	// not carry every combination of SOP class and transfer syntax.
	if got[RLELosslessUID] {
		t.Error("a compressed syntax is in the default set; that belongs in AllTransferSyntaxes")
	}
}

// TestAllTransferSyntaxesPrefersUncompressed verifies the order, which decides
// what a peer offering both forms is met with.
func TestAllTransferSyntaxesPrefersUncompressed(t *testing.T) {
	all := AllTransferSyntaxes()
	if len(all) < 30 {
		t.Fatalf("AllTransferSyntaxes returned %d syntaxes, far fewer than the standard defines", len(all))
	}

	// The four uncompressed ones come first, so a peer that offers an
	// uncompressed alternative is met with the one this library can decode.
	for i, want := range DefaultTransferSyntaxes() {
		if all[i] != want {
			t.Errorf("position %d is %s, want %s — uncompressed syntaxes must be preferred",
				i, all[i], want)
		}
	}

	seen := make(map[string]bool, len(all))
	for _, ts := range all {
		if seen[ts] {
			t.Errorf("%s appears twice; a duplicated transfer syntax in a context is malformed", ts)
		}
		seen[ts] = true
	}
}

// TestCompressedInstanceCanBeStored is the point of the change: an SCP that opts
// in can receive an instance in a syntax it cannot decode.
//
// Storing and forwarding does not require decoding — an archive keeps the pixel
// data as bytes and hands it back later. Declining these contexts made go-dicom
// unable to receive most real-world imaging, since almost everything a modality
// produces is compressed.
func TestCompressedInstanceCanBeStored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var storedSyntax string
	var storedBytes int
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "COMP_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, &StorageHandler{
		OnStore: func(_ context.Context, _, _ string, ds *dataset.Dataset) uint16 {
			storedSyntax = ds.TransferSyntaxUID()
			if elem, ok := ds.Get(tag.New(0x7FE0, 0x0010)); ok {
				if b, ok := elem.GetValue().([]byte); ok {
					storedBytes = len(b)
				}
			}
			return StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	// Opt in to the full set, as an archive would.
	server.SetSupportedTransferSyntaxes(AllTransferSyntaxes())

	scu := NewSCU(SCUConfig{
		CallingAE: "COMP_SCU", CalledAE: "COMP_SCP", Address: server.Addr(),
	})

	// Propose CT storage over RLE only, so the association can only succeed if
	// the compressed syntax is genuinely negotiated.
	if err := scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   CTImageStorageUID,
		TransferSyntaxes: []string{RLELosslessUID},
	}}); err != nil {
		t.Fatalf("Associate over a compressed syntax: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	pcID, ok := FindPresentationContextID(scu.Association().AcceptedContexts(), CTImageStorageUID)
	if !ok {
		t.Fatal("the CT storage context was not accepted over RLE")
	}
	if got := scu.Association().TransferSyntaxFor(pcID); got != RLELosslessUID {
		t.Fatalf("negotiated %s, want RLE Lossless", got)
	}

	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5\x00")))
	// Opaque pixel bytes: the SCP never decodes them, which is the point.
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB,
		[]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}))

	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store over a compressed syntax: %v", err)
	}

	if storedBytes != 8 {
		t.Errorf("the SCP received %d pixel bytes, want 8", storedBytes)
	}
	_ = storedSyntax
}
