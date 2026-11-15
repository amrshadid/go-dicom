package filereader

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// A program that wants the structure of a file, what it holds and where,
// had every value read into memory to get it, Pixel Data included: 218 MB
// for a 200 MB image, to print its position (#274).

// bulkFile is an Explicit VR Little Endian file with text, a short and a
// long binary value, and native Pixel Data.
func bulkFile() []byte {
	var ds bytes.Buffer
	ds.Write([]byte{0x10, 0x00, 0x10, 0x00, 'P', 'N', 4, 0, 'D', 'o', 'e', ' '})
	ds.Write([]byte{0x28, 0x00, 0x10, 0x00, 'U', 'S', 2, 0, 16, 0})
	// (0042,0011) Encapsulated Document, OB: 4 bytes, then 300.
	ds.Write([]byte{0x42, 0x00, 0x11, 0x00, 'O', 'B', 0, 0})
	ds.Write(le32(4))
	ds.Write([]byte{1, 2, 3, 4})
	ds.Write([]byte{0x00, 0x54, 0x10, 0x10, 'O', 'W', 0, 0}) // (5400,1010) Waveform Data
	ds.Write(le32(300))
	ds.Write(bytes.Repeat([]byte{0xAB}, 300))
	ds.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 'O', 'W', 0, 0})
	ds.Write(le32(8))
	ds.Write([]byte{9, 8, 7, 6, 5, 4, 3, 2})
	return part10("1.2.840.10008.1.2.1", ds.Bytes())
}

func elementAt(df *DICOMFile, group, element uint16) *DataElementValue {
	for _, e := range df.DataElements {
		if e.Tag == tag.New(group, element) {
			return e
		}
	}
	return nil
}

func TestSkipBulkDataLeavesBulkValuesInTheFile(t *testing.T) {
	data := bulkFile()

	// Every binary value, whatever its length.
	df := wholeFileWith(t, data, ReadOptions{SkipBulkData: true})
	if name := elementAt(df, 0x0010, 0x0010); name.ValueSkipped || string(name.Value) != "Doe " {
		t.Errorf("the name is %+v", name)
	}
	if rows := elementAt(df, 0x0028, 0x0010); rows.ValueSkipped || len(rows.Value) != 2 {
		t.Errorf("Rows is %+v", rows)
	}
	for _, at := range [][2]uint16{{0x0042, 0x0011}, {0x5400, 0x1010}, {0x7FE0, 0x0010}} {
		e := elementAt(df, at[0], at[1])
		if !e.ValueSkipped || e.Value != nil || e.Length == 0 || e.Position.End != e.Position.ValueOffset+int64(e.Length) {
			t.Errorf("%s was read: %+v", e.Tag, e)
		}
	}

	// With a threshold, the short one is read and the long one is not, and
	// Pixel Data is not however short it is.
	df = wholeFileWith(t, data, ReadOptions{SkipBulkData: true, BulkDataThreshold: 64})
	if short := elementAt(df, 0x0042, 0x0011); short.ValueSkipped || !bytes.Equal(short.Value, []byte{1, 2, 3, 4}) {
		t.Errorf("a value under the threshold is %+v", short)
	}
	if long := elementAt(df, 0x5400, 0x1010); !long.ValueSkipped {
		t.Error("a value over the threshold was read")
	}
	if pixels := elementAt(df, 0x7FE0, 0x0010); !pixels.ValueSkipped {
		t.Error("Pixel Data under the threshold was read")
	}

	// Unasked, nothing is skipped.
	df = wholeFile(t, data)
	for _, e := range df.DataElements {
		if e.ValueSkipped || e.Value == nil {
			t.Errorf("%s was skipped with no option set", e.Tag)
		}
	}
}

func TestSkipBulkDataOfEncapsulatedPixelData(t *testing.T) {
	var ds bytes.Buffer
	ds.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 'O', 'B', 0, 0})
	ds.Write(le32(0xFFFFFFFF))
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 0, 0, 0, 0})
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 2, 0, 0, 0, 0xAA, 0xBB})
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 4, 0, 0, 0, 1, 2, 3, 4})
	ds.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	// Something after it, which is read only if the fragments were passed
	// over exactly.
	ds.Write([]byte{0xFC, 0xFF, 0xFC, 0xFF, 'O', 'B', 0, 0})
	ds.Write(le32(2))
	ds.Write([]byte{0, 0})
	data := part10("1.2.840.10008.1.2.4.70", ds.Bytes())

	read := wholeFile(t, data).DataElements[0]
	skipped := wholeFileWith(t, data, ReadOptions{SkipBulkData: true}).DataElements[0]
	if !skipped.ValueSkipped || len(skipped.Value) != 0 || len(read.Value) == 0 {
		t.Errorf("the pixel data was read: %d bytes kept, %d when asked for", len(skipped.Value), len(read.Value))
	}
	if !reflect.DeepEqual(skipped.Position, read.Position) || len(skipped.Position.Fragments) != 3 {
		t.Errorf("the positions differ:\n skipped %+v\n read    %+v", skipped.Position, read.Position)
	}
}

// forwardOnly cannot move within the stream, as a pipe cannot.
type forwardOnly struct{ filebase.Reader }

func (f forwardOnly) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekCurrent {
		return f.Reader.Seek(offset, whence)
	}
	if whence == io.SeekCurrent && offset > 0 {
		return 0, errors.New("the stream cannot seek")
	}
	return f.Reader.Seek(offset, whence)
}

func TestSkipBulkDataOnAStreamThatCannotSeek(t *testing.T) {
	data := bulkFile()
	df, err := ReadDICOMFileWithOptions(forwardOnly{filebase.NewFileReader(bytes.NewReader(data))}, ReadOptions{SkipBulkData: true})
	if err != nil {
		t.Fatal(err)
	}
	pixels := elementAt(df, 0x7FE0, 0x0010)
	if pixels == nil || !pixels.ValueSkipped || pixels.Position.End != int64(len(data)) {
		t.Errorf("the pixel data is %+v, want it skipped and ending the file at %d", pixels, len(data))
	}
}

// Across pydicom's corpus the two ways of reading agree on everything but
// the bytes that were left behind.
func TestSkipBulkDataAcrossThePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	checked, skippedBytes := 0, int64(0)
	var same func(t *testing.T, a, b []*DataElementValue)
	same = func(t *testing.T, a, b []*DataElementValue) {
		if len(a) != len(b) {
			t.Fatalf("%d elements read, %d with bulk data skipped", len(a), len(b))
		}
		for i := range a {
			x, y := a[i], b[i]
			if x.Tag != y.Tag || x.VR != y.VR || x.Length != y.Length || !reflect.DeepEqual(x.Position, y.Position) {
				t.Fatalf("%s: read as %s %d at %+v, skipped as %s %d at %+v", x.Tag, x.VR, x.Length, x.Position, y.VR, y.Length, y.Position)
			}
			if y.ValueSkipped {
				skippedBytes += y.Position.End - y.Position.ValueOffset
			} else if !bytes.Equal(x.Value, y.Value) {
				t.Fatalf("%s: a value that was not skipped differs", x.Tag)
			}
			if len(x.Items) != len(y.Items) {
				t.Fatalf("%s: %d items read, %d skipped", x.Tag, len(x.Items), len(y.Items))
			}
			for j := range x.Items {
				same(t, x.Items[j].Elements, y.Items[j].Elements)
			}
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		full, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
		if err != nil || truncatedOrRepaired(full.Warnings) {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			light := wholeFileWith(t, data, ReadOptions{SkipBulkData: true})
			same(t, full.DataElements, light.DataElements)
		})
		checked++
	}
	if checked < 60 || skippedBytes == 0 {
		t.Errorf("%d files checked, %d bytes skipped", checked, skippedBytes)
	}
}

// A value nobody knows the VR of is read when it is short, since it is
// often text, and left in the file when it is long.
func TestSkipBulkDataOfAValueWithNoKnownVR(t *testing.T) {
	element := func(elem uint16, value []byte) []byte {
		out := []byte{0x09, 0x00, byte(elem), byte(elem >> 8)}
		out = append(out, le32(uint32(len(value)))...)
		return append(out, value...)
	}
	var ds bytes.Buffer
	ds.Write(element(0x0010, []byte("ACME 1.0")))
	ds.Write(element(0x1001, []byte("a note")))
	ds.Write(element(0x1002, bytes.Repeat([]byte{0x7F}, 4000)))
	data := part10("1.2.840.10008.1.2", ds.Bytes())

	df := wholeFileWith(t, data, ReadOptions{SkipBulkData: true})
	if note := elementAt(df, 0x0009, 0x1001); note.ValueSkipped || string(note.Value) != "a note" {
		t.Errorf("the short value is %+v", note)
	}
	if blob := elementAt(df, 0x0009, 0x1002); !blob.ValueSkipped || blob.Value != nil || blob.Length != 4000 {
		t.Errorf("the long value was read: %d bytes held", len(blob.Value))
	}
}
