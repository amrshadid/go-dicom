package filereader

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// The reader knew where it was at every step and kept none of it, so a
// decoded attribute could not be tied back to its bytes (#259). These tests
// hold each recorded position against the file itself.

// tiling checks that the elements account for every byte of data from start
// to its end with no gap and no overlap, and that each element's parts are
// where its position says. It returns where the last element ended.
func tiling(t *testing.T, data []byte, order binary.ByteOrder, elements []*DataElementValue, start int64, where string) int64 {
	t.Helper()
	at := start
	for _, e := range elements {
		p := e.Position
		name := fmt.Sprintf("%s %s", where, e.Tag)
		if p.Offset != at {
			t.Fatalf("%s starts at %d, but the element before it ended at %d", name, p.Offset, at)
		}
		if got := tag.New(order.Uint16(data[p.Offset:]), order.Uint16(data[p.Offset+2:])); got != e.Tag {
			t.Fatalf("%s: the bytes at %d are the tag %s", name, p.Offset, got)
		}
		if p.ExplicitVR {
			// A VR the file wrote as UN is read as the dictionary's.
			if wire := string(data[p.Offset+4 : p.Offset+6]); wire != e.VR && wire != "UN" {
				t.Fatalf("%s: the VR in the file is %q, the element's %q", name, wire, e.VR)
			}
		}
		if p.LengthOffset+int64(p.LengthSize) != p.ValueOffset {
			t.Fatalf("%s: a %d-byte length at %d does not end at the value, %d", name, p.LengthSize, p.LengthOffset, p.ValueOffset)
		}
		var stated uint32
		if p.LengthSize == 2 {
			stated = uint32(order.Uint16(data[p.LengthOffset:]))
		} else {
			stated = order.Uint32(data[p.LengthOffset:])
		}
		if stated != e.Length {
			t.Fatalf("%s: the length field holds %d, the element's length is %d", name, stated, e.Length)
		}

		end := p.ValueOffset
		switch {
		case e.Items != nil || (e.VR == "SQ"):
			for i, item := range e.Items {
				in := fmt.Sprintf("%s item %d", name, i)
				if item.Offset != end {
					t.Fatalf("%s starts at %d, want %d", in, item.Offset, end)
				}
				if got := tag.New(order.Uint16(data[item.Offset:]), order.Uint16(data[item.Offset+2:])); got != tag.ItemTag {
					t.Fatalf("%s: the bytes at %d are %s, not an item", in, item.Offset, got)
				}
				if got := order.Uint32(data[item.Offset+4:]); got != item.DeclaredLength {
					t.Fatalf("%s: the header states %d, recorded %d", in, got, item.DeclaredLength)
				}
				end = tiling(t, data, order, item.Elements, item.Offset+8, in)
				if item.DelimiterOffset != 0 {
					if item.DelimiterOffset != end {
						t.Fatalf("%s: delimiter at %d, its elements end at %d", in, item.DelimiterOffset, end)
					}
					if got := tag.New(order.Uint16(data[end:]), order.Uint16(data[end+2:])); got != tag.ItemDelimiterTag {
						t.Fatalf("%s: the bytes at %d are %s, not an item delimiter", in, end, got)
					}
					end += 8
				}
				if item.End != end {
					t.Fatalf("%s ends at %d, recorded %d", in, end, item.End)
				}
			}
		case p.Fragments != nil:
			for i, f := range p.Fragments {
				if f.Offset != end || f.ValueOffset != f.Offset+8 {
					t.Fatalf("%s fragment %d at %d (value %d), want %d", name, i, f.Offset, f.ValueOffset, end)
				}
				if got := int64(order.Uint32(data[f.Offset+4:])); got != f.End-f.ValueOffset {
					t.Fatalf("%s fragment %d: header states %d bytes, recorded %d", name, i, got, f.End-f.ValueOffset)
				}
				end = f.End
			}
		default:
			end = p.ValueOffset + int64(e.Length)
			// Big endian values are converted as they are read.
			if order == binary.ByteOrder(binary.LittleEndian) && !bytes.Equal(data[p.ValueOffset:end], e.Value) {
				t.Fatalf("%s: the bytes at %d are not its value", name, p.ValueOffset)
			}
		}
		if p.DelimiterOffset != 0 {
			if p.DelimiterOffset != end {
				t.Fatalf("%s: delimiter at %d, its content ends at %d", name, p.DelimiterOffset, end)
			}
			if got := tag.New(order.Uint16(data[end:]), order.Uint16(data[end+2:])); got != tag.SequenceDelimiterTag {
				t.Fatalf("%s: the bytes at %d are %s, not a sequence delimiter", name, end, got)
			}
			end += 8
		}
		if p.End != end {
			t.Fatalf("%s ends at %d, recorded %d", name, end, p.End)
		}
		at = end
	}
	return at
}

// wholeFile checks the positions of everything in a file that was read
// without a warning: the meta group and the data set, end to end.
func wholeFile(t *testing.T, data []byte) *DICOMFile {
	t.Helper()
	df, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	at := int64(0)
	if df.HasPreamble {
		var meta []*DataElementValue
		if df.MetaGroupLength != nil {
			meta = append(meta, df.MetaGroupLength)
		}
		at = tiling(t, data, binary.LittleEndian, append(meta, df.MetaElements...), 132, "meta")
	}
	if df.DataSetOffset != at {
		t.Fatalf("the data set is recorded at %d; the meta group ends at %d", df.DataSetOffset, at)
	}
	if df.Deflated {
		return df
	}
	var order binary.ByteOrder = binary.LittleEndian
	if !df.IsLittleEndian {
		order = binary.BigEndian
	}
	if end := tiling(t, data, order, df.DataElements, at, "data set"); end != int64(len(data)) {
		t.Fatalf("the elements end at %d; the file is %d bytes", end, len(data))
	}
	return df
}

// part10 puts a preamble and an Explicit VR Little Endian meta group naming
// syntax before a data set.
func part10(syntax string, dataSet []byte) []byte {
	if len(syntax)%2 == 1 {
		syntax += "\x00"
	}
	var meta bytes.Buffer
	meta.Write([]byte{0x02, 0x00, 0x10, 0x00, 'U', 'I'})
	_ = binary.Write(&meta, binary.LittleEndian, uint16(len(syntax)))
	meta.WriteString(syntax)

	var out bytes.Buffer
	out.Write(make([]byte, 128))
	out.WriteString("DICM")
	out.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	_ = binary.Write(&out, binary.LittleEndian, uint32(meta.Len()))
	out.Write(meta.Bytes())
	out.Write(dataSet)
	return out.Bytes()
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func TestPositionsExplicitVR(t *testing.T) {
	var ds bytes.Buffer
	// A short VR, a long one, and a sequence closed by delimiters holding an
	// item closed by one.
	ds.Write([]byte{0x08, 0x00, 0x05, 0x00, 'C', 'S', 10, 0})
	ds.WriteString("ISO_IR 100")
	ds.Write([]byte{0x08, 0x00, 0x40, 0x11, 'S', 'Q', 0, 0})
	ds.Write(le32(0xFFFFFFFF))
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	ds.Write(le32(0xFFFFFFFF))
	ds.Write([]byte{0x08, 0x00, 0x50, 0x11, 'U', 'I', 4, 0, '1', '.', '2', 0})
	ds.Write([]byte{0xFE, 0xFF, 0x0D, 0xE0, 0, 0, 0, 0})
	ds.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	// A sequence and an item of stated length.
	ds.Write([]byte{0x08, 0x00, 0x15, 0x11, 'S', 'Q', 0, 0})
	ds.Write(le32(20))
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	ds.Write(le32(12))
	ds.Write([]byte{0x08, 0x00, 0x50, 0x11, 'U', 'I', 4, 0, '1', '.', '3', 0})
	ds.Write([]byte{0x10, 0x00, 0x10, 0x00, 'P', 'N', 4, 0, 'D', 'o', 'e', ' '})
	// Encapsulated pixel data: an empty offset table and two fragments.
	ds.Write([]byte{0xE0, 0x7F, 0x10, 0x00, 'O', 'B', 0, 0})
	ds.Write(le32(0xFFFFFFFF))
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 0, 0, 0, 0})
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 2, 0, 0, 0, 0xAA, 0xBB})
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 4, 0, 0, 0, 1, 2, 3, 4})
	ds.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})

	data := part10("1.2.840.10008.1.2.4.70", ds.Bytes())
	df := wholeFile(t, data)

	if df.MetaGroupLength == nil || df.MetaGroupLength.Position.Offset != 132 || df.MetaGroupLength.Position.End != 144 {
		t.Errorf("the group length element is %+v, want 132 to 144", df.MetaGroupLength)
	}
	first := df.DataElements[0].Position
	if first.Offset != df.DataSetOffset || !first.ExplicitVR || first.LengthSize != 2 || first.ValueOffset != first.Offset+8 {
		t.Errorf("a short explicit element is at %+v", first)
	}
	seq := df.DataElements[1]
	if seq.Position.LengthSize != 4 || seq.Position.ValueOffset != seq.Position.Offset+12 ||
		seq.Position.DelimiterOffset == 0 || seq.Items[0].DelimiterOffset == 0 {
		t.Errorf("the delimited sequence is at %+v, item %+v", seq.Position, seq.Items[0])
	}
	if fixed := df.DataElements[2]; fixed.Position.DelimiterOffset != 0 || fixed.Items[0].DelimiterOffset != 0 ||
		fixed.Items[0].DeclaredLength != 12 {
		t.Errorf("the sequence of stated length is at %+v, item %+v", fixed.Position, fixed.Items[0])
	}
	pixels := df.DataElements[4].Position
	if len(pixels.Fragments) != 3 || pixels.DelimiterOffset == 0 || pixels.End != int64(len(data)) {
		t.Errorf("the pixel data is at %+v, want three items and a delimiter at the end of the file", pixels)
	}
}

func TestPositionsImplicitVR(t *testing.T) {
	var ds bytes.Buffer
	ds.Write([]byte{0x08, 0x00, 0x05, 0x00})
	ds.Write(le32(10))
	ds.WriteString("ISO_IR 100")
	ds.Write([]byte{0x08, 0x00, 0x40, 0x11})
	ds.Write(le32(0xFFFFFFFF))
	ds.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
	ds.Write(le32(12))
	ds.Write([]byte{0x08, 0x00, 0x50, 0x11})
	ds.Write(le32(4))
	ds.Write([]byte{'1', '.', '2', 0})
	ds.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})

	df := wholeFile(t, part10("1.2.840.10008.1.2", ds.Bytes()))
	if p := df.DataElements[0].Position; p.ExplicitVR || p.LengthSize != 4 || p.ValueOffset != p.Offset+8 {
		t.Errorf("an implicit element is at %+v", p)
	}

	// With no preamble the data set starts the file.
	bare := wholeFile(t, ds.Bytes())
	if bare.DataSetOffset != 0 || bare.MetaGroupLength != nil {
		t.Errorf("a bare data set is recorded at %d", bare.DataSetOffset)
	}
}

// Every clean file of pydicom's corpus, end to end.
func TestPositionsAcrossThePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	checked := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		df, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
		// A file that is truncated or misdeclared has no clean account of
		// its bytes to hold the positions against.
		if err != nil || truncatedOrRepaired(df.Warnings) {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) { wholeFile(t, data) })
		checked++
	}
	if checked < 60 {
		t.Errorf("only %d files were checked", checked)
	}
}

func truncatedOrRepaired(warnings []string) bool {
	for _, w := range warnings {
		for _, sign := range []string{"truncated", "declares", "skipped", "left in the sequence", "none left"} {
			if strings.Contains(w, sign) {
				return true
			}
		}
	}
	return false
}
