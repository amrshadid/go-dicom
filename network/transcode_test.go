package network_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// Sending a compressed instance over a context that negotiated an uncompressed
// syntax used to put encapsulated fragments on the wire described as native
// pixels. The receiver has no way to tell: it renders whatever the bytes happen
// to look like, and nothing on either side reports a problem.
//
// These tests are at the association boundary rather than on the conversion
// function, because the bug was that nothing called it.

// corpusFile opens a file from pydicom's test data, skipping when it is absent.
func corpusFile(t *testing.T, name string) *dataset.Dataset {
	t.Helper()

	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("GODICOM_PYDICOM_DATA is not set; skipping the corpus-backed transcoding check")
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		t.Skipf("corpus file %s is not available: %v", name, err)
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return df.GetDataset()
}

// TestCompressedInstanceIsDecodedBeforeSending stores an RLE-compressed
// instance over a context that negotiated Explicit VR Little Endian, and checks
// the receiver gets pixels rather than fragments.
func TestCompressedInstanceIsDecodedBeforeSending(t *testing.T) {
	ds := corpusFile(t, "MR_small_RLE.dcm")

	// What the pixels should be, decoded locally.
	want, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatalf("decoding the fixture locally: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var got []byte
	server, err := network.StartServer(ctx, network.SCPConfig{
		AETitle: "TRANSCODE", Port: 0, BindAddress: "127.0.0.1",
	}, &network.StorageHandler{
		OnStore: func(_ context.Context, _, _ string, received *dataset.Dataset) uint16 {
			if elem, ok := received.Get(tag.New(0x7FE0, 0x0010)); ok {
				got, _ = elem.GetValue().([]byte)
			}
			return network.StatusSuccess
		},
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: "TRANSCODE_SCU", CalledAE: "TRANSCODE", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: network.MRImageStorageUID,
		TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if err := scu.Store(ctx, ds); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if len(got) == 0 {
		t.Fatal("the receiver got no pixel data")
	}
	// An encapsulated value starts with an item tag. Seeing one here means the
	// compressed bytes traveled as though they were pixels.
	if len(got) >= 4 && bytes.Equal(got[:4], []byte{0xFE, 0xFF, 0x00, 0xE0}) {
		t.Fatal("the receiver got encapsulated fragments described as native pixel data")
	}

	// Trailing pad byte, if the decoded length was odd.
	if len(got) == len(want)+1 {
		got = got[:len(want)]
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the receiver's pixels differ from the decoded original (%d vs %d bytes)",
			len(got), len(want))
	}
}

// TestUndecodableCompressionIsRefusedNotMislabeled covers the case where the
// pixel data cannot be decoded at all.
//
// The send has to fail. Passing the bytes through would describe JPEG 2000 as
// native pixels, and the receiver would store an instance whose pixel data is
// meaningless with nothing recording that.
func TestUndecodableCompressionIsRefusedNotMislabeled(t *testing.T) {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI,
		[]byte(network.MRImageStorageUID)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI,
		[]byte("1.2.826.0.1.3680043.10.511.5.1\x00")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0010), dataelem.US, []byte{0x40, 0x00}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0011), dataelem.US, []byte{0x40, 0x00}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.US, []byte{0x10, 0x00}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0002), dataelem.US, []byte{0x01, 0x00}))
	// An encapsulated value: one item holding bytes no bundled decoder reads.
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, []byte{
		0xFE, 0xFF, 0x00, 0xE0, 0x04, 0x00, 0x00, 0x00, 0xDE, 0xAD, 0xBE, 0xEF,
	}))
	ds.SetTransferSyntaxUID(network.JPEG2000LosslessUID)

	_, err := network.EncodeDataset(ds, network.ExplicitVRLittleEndianUID)
	if err == nil {
		t.Fatal("a JPEG 2000 instance encoded for an uncompressed context; " +
			"its fragments would have arrived described as pixels")
	}
	if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "decoder") {
		t.Errorf("the error does not explain that the pixel data could not be decoded: %v", err)
	}
}

// TestUncompressedDataSetsAreUnaffected checks the common path is untouched: a
// data set with no recorded syntax, or an uncompressed one, is encoded exactly
// as before.
func TestUncompressedDataSetsAreUnaffected(t *testing.T) {
	build := func() *dataset.Dataset {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW,
			[]byte{0x01, 0x02, 0x03, 0x04}))
		return ds
	}

	for _, tc := range []struct {
		name   string
		source string
	}{
		{"no recorded syntax", ""},
		{"explicit VR little endian", network.ExplicitVRLittleEndianUID},
		{"implicit VR little endian", network.ImplicitVRLittleEndianUID},
		{"explicit VR big endian", network.ExplicitVRBigEndianUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := build()
			if tc.source != "" {
				ds.SetTransferSyntaxUID(tc.source)
			}
			encoded, err := network.EncodeDataset(ds, network.ExplicitVRLittleEndianUID)
			if err != nil {
				t.Fatalf("EncodeDataset: %v", err)
			}
			decoded, err := network.DecodeDataset(encoded, network.ExplicitVRLittleEndianUID)
			if err != nil {
				t.Fatalf("DecodeDataset: %v", err)
			}
			elem, ok := decoded.Get(tag.New(0x7FE0, 0x0010))
			if !ok {
				t.Fatal("pixel data was lost")
			}
			if v, _ := elem.GetValue().([]byte); !bytes.Equal(v, []byte{0x01, 0x02, 0x03, 0x04}) {
				t.Errorf("pixel data = % X, want 01 02 03 04", v)
			}
		})
	}
}

// TestTranscodingDoesNotMutateTheCaller checks the data set handed in is left
// alone.
//
// A C-MOVE sends the same instance to a destination and may be asked for it
// again; rewriting the caller's pixel data as a side effect of one send would
// leave the second one working from something else.
func TestTranscodingDoesNotMutateTheCaller(t *testing.T) {
	ds := corpusFile(t, "MR_small_RLE.dcm")

	before, err := ds.RawPixelData()
	if err != nil {
		t.Fatalf("reading the fixture's stored pixel data: %v", err)
	}
	beforeCopy := append([]byte(nil), before...)

	if _, err := network.EncodeDataset(ds, network.ExplicitVRLittleEndianUID); err != nil {
		t.Fatalf("EncodeDataset: %v", err)
	}

	after, err := ds.RawPixelData()
	if err != nil {
		t.Fatalf("re-reading the fixture's stored pixel data: %v", err)
	}
	if !bytes.Equal(beforeCopy, after) {
		t.Error("encoding rewrote the caller's pixel data; a second send would use the decoded form")
	}
	if ds.TransferSyntaxUID() != network.RLELosslessUID {
		t.Errorf("the caller's transfer syntax changed to %q", ds.TransferSyntaxUID())
	}
}
