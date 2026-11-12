package anonymize_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
)

// Ultrasound and screen captures are the images that carry burned-in text,
// and they are usually stored compressed, which CleanPixelData refused (#240).

// compressedUltrasound is ultrasound's image, RLE compressed.
func compressedUltrasound(t *testing.T) *dataset.Dataset {
	t.Helper()
	ds := ultrasound(t)
	elem, _ := ds.Get(tagPixelData)
	stream, err := compress.NewRLECompressor().CompressFrame(elem.GetValue().([]byte), 1, 8)
	if err != nil {
		t.Fatalf("CompressFrame: %v", err)
	}
	var value bytes.Buffer
	item := func(data []byte) {
		value.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		_ = binary.Write(&value, binary.LittleEndian, uint32(len(data)))
		value.Write(data)
	}
	item(nil)
	item(stream)
	value.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	ds.Remove(tagPixelData)
	if err := ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OB, value.Bytes())); err != nil {
		t.Fatal(err)
	}
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.5")
	return ds
}

// TestCleanPixelDataDecompressesFirst: the image comes out native, with the
// region filled and every other sample as it decoded. Nothing is encoded
// again, so cleaning adds no loss of its own.
func TestCleanPixelDataDecompressesFirst(t *testing.T) {
	ds := compressedUltrasound(t)
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.CleanPixelData(ds, dataset.PixelRegion{X: 0, Y: 0, Width: 4, Height: 1}); err != nil {
		t.Fatalf("CleanPixelData: %v", err)
	}
	if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.1" {
		t.Errorf("transfer syntax %q, want Explicit VR Little Endian", got)
	}
	elem, _ := ds.Get(tagPixelData)
	px, _ := elem.GetValue().([]byte)
	want := []byte{0, 0, 0, 0, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200, 200}
	if !bytes.Equal(px, want) {
		t.Errorf("Pixel Data is %v, want %v", px, want)
	}
	if got := textValue(ds, tagBurnedIn); got != "NO" {
		t.Errorf("Burned In Annotation is %q", got)
	}
	if got := methodCodes(ds); got != "113101" {
		t.Errorf("method codes %q, want 113101", got)
	}
}

// TestCleanPixelDataThatFailsLeavesItCompressed: an image that was not
// cleaned must not come out changed, decompressed included.
func TestCleanPixelDataThatFailsLeavesItCompressed(t *testing.T) {
	ds := compressedUltrasound(t)
	elem, _ := ds.Get(tagPixelData)
	before := append([]byte(nil), elem.GetValue().([]byte)...)

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.CleanPixelData(ds, dataset.PixelRegion{X: 50, Y: 50, Width: 2, Height: 2}); err == nil {
		t.Fatal("a region outside the image was accepted")
	}
	if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.5" {
		t.Errorf("transfer syntax became %q", got)
	}
	elem, _ = ds.Get(tagPixelData)
	if after, _ := elem.GetValue().([]byte); !bytes.Equal(after, before) {
		t.Error("Pixel Data was changed by a clean that failed")
	}
	if got := methodCodes(ds); got != "" {
		t.Errorf("method codes %q were recorded", got)
	}
}
