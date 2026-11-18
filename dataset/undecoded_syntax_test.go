package dataset_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A file in High-Throughput JPEG 2000 or JPEG XL is read and carried, and its
// pixel data is not decoded. It used to be taken for native: the fragments
// were read as samples, and every refusal was a count of bytes ("the pixel
// data holds 4330 bytes and the image needs 8192") that named neither the
// syntax nor the reason. pydicom 3 decodes HTJ2K through OpenJPEG; nothing
// here does yet.

// undecodedSyntaxes are the transfer syntaxes with encapsulated pixel data and
// no decoder here, by the name the refusal has to give.
var undecodedSyntaxes = map[string]string{
	"1.2.840.10008.1.2.4.201": "High-Throughput JPEG 2000 Image Compression (Lossless Only)",
	"1.2.840.10008.1.2.4.202": "High-Throughput JPEG 2000 with RPCL Options Image Compression (Lossless Only)",
	"1.2.840.10008.1.2.4.203": "High-Throughput JPEG 2000 Image Compression",
	"1.2.840.10008.1.2.4.110": "JPEG XL Lossless",
	"1.2.840.10008.1.2.4.111": "JPEG XL JPEG Recompression",
	"1.2.840.10008.1.2.4.112": "JPEG XL",
}

// encapsulatedImage is a one-row 8-bit image of four pixels whose Pixel Data
// is encapsulated: an empty Basic Offset Table and one fragment of bytes that
// are not samples. Read as native, the value is longer than the image, so it
// passed for one: it rendered, and a redaction wrote black into it.
func encapsulatedImage(syntax string) *dataset.Dataset {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: make([]byte, 4)}.dataset()
	ds.SetTransferSyntaxUID(syntax)
	value := []byte{0xFE, 0xFF, 0x00, 0xE0, 0x00, 0x00, 0x00, 0x00, // Basic Offset Table, empty
		0xFE, 0xFF, 0x00, 0xE0, 0x10, 0x00, 0x00, 0x00} // one fragment of 16 bytes
	value = append(value, []byte("not a codestream")...)
	value = append(value, 0xFE, 0xFF, 0xDD, 0xE0, 0x00, 0x00, 0x00, 0x00) // sequence delimiter
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, value))
	return ds
}

// TestUndecodedSyntaxIsRefusedByName: rendering, reading the samples,
// decompressing and redacting each refuse an HTJ2K or JPEG XL image with the
// syntax's name and UID, and the data set is left as it was.
func TestUndecodedSyntaxIsRefusedByName(t *testing.T) {
	for syntax, name := range undecodedSyntaxes {
		for job, run := range map[string]func(*dataset.Dataset) error{
			"RenderFrame": func(ds *dataset.Dataset) error { _, err := ds.RenderFrame(0, nil); return err },
			"PixelArrayInterpreted": func(ds *dataset.Dataset) error {
				_, err := ds.PixelArrayInterpreted()
				return err
			},
			"DecompressInPlace": func(ds *dataset.Dataset) error { return ds.DecompressInPlace() },
			"RedactPixelRegions": func(ds *dataset.Dataset) error {
				return ds.RedactPixelRegions(dataset.PixelRegion{X: 0, Y: 0, Width: 2, Height: 2})
			},
		} {
			ds := encapsulatedImage(syntax)
			before, _ := ds.Get(tag.New(0x7FE0, 0x0010))
			stored := append([]byte(nil), before.GetValue().([]byte)...)

			err := run(ds)
			if err == nil || !strings.Contains(err.Error(), name+" ("+syntax+")") {
				t.Errorf("%s on %s: %v; want a refusal naming %q", job, syntax, err, name)
			}
			after, _ := ds.Get(tag.New(0x7FE0, 0x0010))
			if string(after.GetValue().([]byte)) != string(stored) || ds.TransferSyntaxUID() != syntax {
				t.Errorf("%s on %s changed the data set it refused", job, syntax)
			}
		}
	}
}

// TestUnknownSyntaxWithNativePixelsStillDecodes: a syntax with no decoder
// whose Pixel Data is not encapsulated is read as native, as before. Only
// pixel data that is fragments is refused.
func TestUnknownSyntaxWithNativePixelsStillDecodes(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", px: []byte{0, 85, 170, 255}}.dataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.4.201")
	if got := grays(t, ds, nil); !sameLevels(got, []uint8{0, 85, 170, 255}) {
		t.Errorf("levels %v", got)
	}
}
