package filereader_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// DICOM added three value representations in 2018: SV and UV for 64-bit
// integers, and OV for a 64-bit binary array. Nothing here knew them.
//
// An unknown VR is not a small problem in explicit encoding, because the VR
// decides the shape of the header. SV, UV and OV take the long form — two
// reserved bytes then a four-byte length — and a reader that does not know that
// reads the reserved bytes as a two-byte length, gets zero, and every element
// after it is parsed from the wrong offset. The file does not fail to read; it
// reads as something else.

// seekBuffer is an in-memory io.WriteSeeker, which the writer needs so it can
// go back and fill in the group lengths.
type seekBuffer struct {
	data []byte
	pos  int64
}

func (b *seekBuffer) Write(p []byte) (int, error) {
	end := b.pos + int64(len(p))
	if end > int64(len(b.data)) {
		grown := make([]byte, end)
		copy(grown, b.data)
		b.data = grown
	}
	copy(b.data[b.pos:], p)
	b.pos = end
	return len(p), nil
}

func (b *seekBuffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.pos = offset
	case io.SeekCurrent:
		b.pos += offset
	case io.SeekEnd:
		b.pos = int64(len(b.data)) + offset
	}
	return b.pos, nil
}

// TestSixtyFourBitVRsRoundTrip writes the three new VRs and reads them back,
// with an ordinary element after them so a misread length shows up.
func TestSixtyFourBitVRsRoundTrip(t *testing.T) {
	var sv, uv, ov [8]byte
	var negative int64 = -42
	binary.LittleEndian.PutUint64(sv[:], uint64(negative))
	binary.LittleEndian.PutUint64(uv[:], 18446744073709551615) // the largest UV
	binary.LittleEndian.PutUint64(ov[:], 0x0102030405060708)

	out := &seekBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.826.0.1.3680043.10.511.8.1",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})

	elements := []struct {
		tg    tag.Tag
		vr    dataelem.VR
		value []byte
	}{
		// Extended Offset Table (7FE0,0001) is OV; the other two tags are
		// chosen only to keep the data set in ascending order.
		{tag.New(0x0008, 0x0060), dataelem.CS, []byte("OT")},
		{tag.New(0x0018, 0x9445), dataelem.SV, sv[:]},
		{tag.New(0x0018, 0x9446), dataelem.UV, uv[:]},
		{tag.New(0x7FE0, 0x0001), dataelem.OV, ov[:]},
		// The element after them: if a long-form header was read as short form,
		// this one is parsed from the wrong offset and comes back wrong or not
		// at all.
		{tag.New(0x7FE0, 0x0002), dataelem.UL, []byte{0x0D, 0xF0, 0xAD, 0x0B}},
	}
	for _, e := range elements {
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag: e.tg, VR: string(e.vr), Value: e.value, Length: uint32(len(e.value)),
		}); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.tg.String(), err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(out.data)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	ds := df.GetDataset()

	for _, e := range elements {
		elem, ok := ds.Get(e.tg)
		if !ok {
			t.Errorf("%s (%s) did not survive the round trip", e.tg.String(), e.vr)
			continue
		}
		if got := elem.GetVR(); got != e.vr {
			t.Errorf("%s came back as %s, want %s", e.tg.String(), got, e.vr)
		}
		raw, _ := elem.GetValue().([]byte)
		if !bytes.Equal(raw, e.value) {
			t.Errorf("%s came back as % X, want % X", e.tg.String(), raw, e.value)
		}
	}

	// And the values mean what they should, not merely the same bytes.
	if elem, ok := ds.Get(tag.New(0x0018, 0x9445)); ok {
		raw, _ := elem.GetValue().([]byte)
		if got := int64(binary.LittleEndian.Uint64(raw)); got != -42 {
			t.Errorf("the SV value is %d, want -42", got)
		}
	}
	if elem, ok := ds.Get(tag.New(0x0018, 0x9446)); ok {
		raw, _ := elem.GetValue().([]byte)
		if got := binary.LittleEndian.Uint64(raw); got != 18446744073709551615 {
			t.Errorf("the UV value is %d, want the largest 64-bit unsigned value", got)
		}
	}
}

// TestSixtyFourBitVRsUseTheLongHeader checks the encoding directly.
//
// The round trip above would still pass if the writer and the reader agreed on
// a wrong header, which is the failure this project keeps finding. So the bytes
// are checked against what PS3.5 7.1.2 requires.
func TestSixtyFourBitVRsUseTheLongHeader(t *testing.T) {
	for _, vr := range []dataelem.VR{dataelem.SV, dataelem.UV, dataelem.OV} {
		t.Run(string(vr), func(t *testing.T) {
			out := &seekBuffer{}
			w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
			w.SetFileMetaInfo(&filewriter.FileMetaInfo{
				MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
				MediaStorageSOPInstanceUID: "1.2.826.0.1.3680043.10.511.8.2",
				TransferSyntaxUID:          "1.2.840.10008.1.2.1",
			})
			value := make([]byte, 8)
			if err := w.AddDataElement(&filewriter.DataElement{
				Tag: tag.New(0x0018, 0x9445), VR: string(vr),
				Value: value, Length: uint32(len(value)),
			}); err != nil {
				t.Fatalf("AddDataElement: %v", err)
			}
			if err := w.Write(); err != nil {
				t.Fatalf("Write: %v", err)
			}

			at := bytes.Index(out.data, []byte{0x18, 0x00, 0x45, 0x94})
			if at < 0 {
				t.Fatal("the element was not written")
			}
			at += 4
			if got := string(out.data[at : at+2]); got != string(vr) {
				t.Fatalf("the VR is %q, want %q", got, vr)
			}
			// Long form: two reserved bytes that must be zero, then a four-byte
			// length. Read as short form, those reserved bytes would be a length
			// of zero and the value would vanish.
			if r := out.data[at+2 : at+4]; r[0] != 0 || r[1] != 0 {
				t.Errorf("the reserved bytes are % X, want 00 00", r)
			}
			if got := binary.LittleEndian.Uint32(out.data[at+4:]); got != 8 {
				t.Errorf("the length is %d, want 8; a short-form header would put "+
					"the value bytes here", got)
			}
		})
	}
}

// TestSixtyFourBitVRsAreKnown covers the validity check the rest of the library
// consults.
func TestSixtyFourBitVRsAreKnown(t *testing.T) {
	for _, vr := range []dataelem.VR{dataelem.SV, dataelem.UV, dataelem.OV} {
		if !dataelem.IsValidVR(vr) {
			t.Errorf("%s is not recognized as a value representation", vr)
		}
	}
}
