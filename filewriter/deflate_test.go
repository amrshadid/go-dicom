package filewriter_test

import (
	"bytes"
	"compress/flate"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// growBuffer is an io.WriteSeeker over memory. Only append-style writing is
// used, so Seek reports the current end.
type growBuffer struct{ buf bytes.Buffer }

func (g *growBuffer) Write(p []byte) (int, error)        { return g.buf.Write(p) }
func (g *growBuffer) Seek(_ int64, _ int) (int64, error) { return int64(g.buf.Len()), nil }
func (g *growBuffer) Bytes() []byte                      { return g.buf.Bytes() }

// writeDeflated writes the given elements as a file declaring the Deflated
// transfer syntax.
func writeDeflated(t *testing.T, elements []*filewriter.DataElement) []byte {
	t.Helper()

	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1.99",
	})
	for _, e := range elements {
		if err := w.AddDataElement(e); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return out.Bytes()
}

func deflatedElements() []*filewriter.DataElement {
	return []*filewriter.DataElement{
		{Tag: tag.New(0x0008, 0x0060), VR: "CS", Value: []byte("MR"), Length: 2},
		{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Doe^John"), Length: 8},
		{Tag: tag.New(0x0010, 0x0020), VR: "LO", Value: []byte("ID-0001 "), Length: 8},
	}
}

// TestDeflatedFileRoundTrips verifies a file written under the Deflated syntax
// can be read back.
//
// The writer previously ignored the syntax and wrote the data set
// uncompressed, so a file declaring 1.2.840.10008.1.2.1.99 had a plain body.
// Nothing could read it — including this library, whose reader inflates on the
// strength of that declaration and failed with "flate: corrupt input before
// offset 5".
func TestDeflatedFileRoundTrips(t *testing.T) {
	raw := writeDeflated(t, deflatedElements())

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}

	if got := df.FileMetaInfo.TransferSyntaxUID; got != "1.2.840.10008.1.2.1.99" {
		t.Errorf("transfer syntax = %q, want the deflated one", got)
	}
	if len(df.DataElements) != 3 {
		t.Fatalf("read %d elements, want 3", len(df.DataElements))
	}

	ds := df.GetDataset()
	for _, want := range []struct {
		tg    tag.Tag
		value string
	}{
		{tag.New(0x0008, 0x0060), "MR"},
		{tag.New(0x0010, 0x0010), "Doe^John"},
		{tag.New(0x0010, 0x0020), "ID-0001 "},
	} {
		elem, ok := ds.Get(want.tg)
		if !ok {
			t.Errorf("%s missing after the round trip", want.tg)
			continue
		}
		if got := string(elem.GetValue().([]byte)); got != want.value {
			t.Errorf("%s = %q, want %q", want.tg, got, want.value)
		}
	}
}

// TestDeflatedBodyIsActuallyCompressed checks the bytes rather than trusting
// the round trip.
//
// A reader and writer that agreed on leaving the body uncompressed would round
// trip perfectly and still produce a file no other implementation accepts —
// which is the failure mode this project keeps hitting. So the body is inflated
// here directly, with the standard library, independently of this library's
// reader.
func TestDeflatedBodyIsActuallyCompressed(t *testing.T) {
	raw := writeDeflated(t, deflatedElements())

	// The body is located by arithmetic on the file layout rather than by
	// asking this library's reader, so the check does not depend on the code it
	// is checking.
	//
	// Layout: preamble (128) + "DICM" (4) + group length element
	// (12 bytes: tag, VR, length, value) + the declared group length.
	const headerStart = 128 + 4
	groupLength := int(uint32(raw[headerStart+8]) |
		uint32(raw[headerStart+9])<<8 |
		uint32(raw[headerStart+10])<<16 |
		uint32(raw[headerStart+11])<<24)
	bodyStart := headerStart + 12 + groupLength

	if bodyStart >= len(raw) {
		t.Fatalf("computed body start %d beyond the %d byte file", bodyStart, len(raw))
	}

	zr := flate.NewReader(bytes.NewReader(raw[bodyStart:]))
	defer zr.Close()
	inflated, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("the body is not a valid DEFLATE stream: %v", err)
	}

	// The inflated body must contain the element values; the compressed body
	// must not, or nothing was compressed.
	if !bytes.Contains(inflated, []byte("Doe^John")) {
		t.Error("inflated body does not contain the element value")
	}
	if bytes.Contains(raw[bodyStart:], []byte("Doe^John")) {
		t.Error("the value appears uncompressed in the file body — it was not deflated")
	}
}

// TestUncompressedSyntaxIsNotDeflated guards the other branch: only the
// deflated syntax gets compressed.
func TestUncompressedSyntaxIsNotDeflated(t *testing.T) {
	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	for _, e := range deflatedElements() {
		if err := w.AddDataElement(e); err != nil {
			t.Fatalf("AddDataElement: %v", err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("Doe^John")) {
		t.Error("an explicit VR little endian file was compressed; only the deflated syntax should be")
	}
}
