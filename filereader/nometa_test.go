package filereader

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
)

// A DICOM file need not have a file meta header. Some modalities write the data
// set on its own, and that is what travels on the network.
//
// PS3.5 Section 10.1 makes Implicit VR Little Endian the default, and assuming
// it is what this used to do. But the default is for a stream that says nothing
// about itself, and the first element says plenty: an explicit VR data set puts
// two ASCII letters where an implicit one puts the low half of a length.
//
// Reading an explicit stream as implicit takes those letters as part of the
// length. (0008,0005) with VR CS and length 10 is 08 00 05 00 43 53 0A 00; read
// as implicit, the length is 0x000A5343 — 676675 bytes, past the end of a
// 434-byte file. The element is dropped, and so is every one after it.
//
// The result was an empty data set and no error. A caller could not tell it
// apart from a file that genuinely held nothing. pydicom's corpus has two such
// files and read 24 elements from each; this read none.

// TestSniffDataSetEncoding covers the decision directly.
func TestSniffDataSetEncoding(t *testing.T) {
	tests := []struct {
		name         string
		head         []byte
		explicitVR   bool
		littleEndian bool
		ok           bool
	}{
		{
			// (0008,0005) CS length 10, explicit VR little endian.
			name:       "explicit little endian",
			head:       []byte{0x08, 0x00, 0x05, 0x00, 'C', 'S', 0x0A, 0x00},
			explicitVR: true, littleEndian: true, ok: true,
		},
		{
			// The same element with both the tag and the length byte-swapped.
			name:       "explicit big endian",
			head:       []byte{0x00, 0x08, 0x00, 0x05, 'C', 'S', 0x00, 0x0A},
			explicitVR: true, littleEndian: false, ok: true,
		},
		{
			// (0008,0005) with a 4-byte length and no VR: 10 little endian.
			name:       "implicit little endian",
			head:       []byte{0x08, 0x00, 0x05, 0x00, 0x0A, 0x00, 0x00, 0x00},
			explicitVR: false, littleEndian: true, ok: true,
		},
		{
			// Implicit VR big endian is not a transfer syntax the standard
			// defines, so an absent VR means little endian and nothing else.
			name:       "no VR means implicit, which is only little endian",
			head:       []byte{0x00, 0x08, 0x00, 0x05, 0x00, 0x00, 0x00, 0x0A},
			explicitVR: false, littleEndian: true, ok: true,
		},
		{
			name: "too short to tell",
			head: []byte{0x08, 0x00, 0x05},
			ok:   false, littleEndian: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			explicit, little, ok := sniffDataSetEncoding(tc.head)
			if ok != tc.ok {
				t.Fatalf("ok is %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if explicit != tc.explicitVR || little != tc.littleEndian {
				t.Errorf("read as %s, want %s",
					describeEncoding(explicit, little),
					describeEncoding(tc.explicitVR, tc.littleEndian))
			}
		})
	}
}

// rawDataSet builds a data set with no preamble and no meta header.
func rawDataSet(t *testing.T, explicitVR, littleEndian bool) []byte {
	t.Helper()

	var buf bytes.Buffer
	write := func(group, element uint16, vr string, value string) {
		if len(value)%2 == 1 {
			value += " "
		}
		put16 := func(v uint16) {
			if littleEndian {
				buf.WriteByte(byte(v))
				buf.WriteByte(byte(v >> 8))
			} else {
				buf.WriteByte(byte(v >> 8))
				buf.WriteByte(byte(v))
			}
		}
		put16(group)
		put16(element)
		if explicitVR {
			buf.WriteString(vr)
			put16(uint16(len(value)))
		} else {
			n := uint32(len(value))
			if littleEndian {
				buf.Write([]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)})
			} else {
				buf.Write([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
			}
		}
		buf.WriteString(value)
	}

	write(0x0008, 0x0016, "UI", "1.2.840.10008.5.1.4.1.1.4")
	write(0x0008, 0x0060, "CS", "MR")
	write(0x0010, 0x0010, "PN", "Doe^Jane")
	return buf.Bytes()
}

// TestRawDataSetIsReadInItsOwnEncoding is the behavior that was missing.
func TestRawDataSetIsReadInItsOwnEncoding(t *testing.T) {
	tests := []struct {
		name       string
		explicitVR bool
		little     bool
	}{
		{"explicit VR little endian", true, true},
		{"explicit VR big endian", true, false},
		{"implicit VR little endian", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := rawDataSet(t, tc.explicitVR, tc.little)

			df, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}
			if df.HasPreamble {
				t.Error("a raw data set was reported as having a preamble")
			}
			if got := len(df.GetDataset().GetAll()); got != 3 {
				t.Fatalf("read %d elements, want 3; the encoding was misread and the "+
					"elements were dropped", got)
			}
			if df.ExplicitVR != tc.explicitVR || df.IsLittleEndian != tc.little {
				t.Errorf("read as %s, want %s",
					describeEncoding(df.ExplicitVR, df.IsLittleEndian),
					describeEncoding(tc.explicitVR, tc.little))
			}
		})
	}
}

// TestNoMetaHeaderCorpusFiles reads pydicom's own headerless files.
//
// Both hold the same 24 elements, one explicit little endian and one explicit
// big endian, and neither has a preamble or a meta header to say so. They are
// the fixtures that showed the defect, and the count is pydicom's.
func TestNoMetaHeaderCorpusFiles(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for _, name := range []string{"ExplVR_LitEndNoMeta.dcm", "ExplVR_BigEndNoMeta.dcm"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join(corpus, name))
			if err != nil {
				t.Skipf("%s is not present: %v", name, err)
			}
			defer func() { _ = f.Close() }()

			df, err := ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatalf("ReadDICOMFile: %v", err)
			}
			if got := len(df.GetDataset().GetAll()); got != 24 {
				t.Errorf("read %d elements, pydicom reads 24", got)
			}
			if !df.ExplicitVR {
				t.Error("read as implicit VR; both files are explicit")
			}
		})
	}
}

// TestRawDataSetWarnsWhichEncodingItChose keeps the choice visible.
//
// The encoding is a guess, however well founded. A caller that gets a data set
// which looks wrong needs to be able to find out that it was read as something
// other than the default.
func TestRawDataSetWarnsWhichEncodingItChose(t *testing.T) {
	data := rawDataSet(t, true, false)

	df, err := ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}

	var found string
	for _, w := range df.Warnings {
		if bytes.Contains([]byte(w), []byte("no file meta header")) {
			found = w
		}
	}
	if found == "" {
		t.Fatal("reading a headerless data set produced no warning saying so")
	}
	if !bytes.Contains([]byte(found), []byte("explicit VR big endian")) {
		t.Errorf("the warning is %q, and does not name the encoding that was chosen", found)
	}
}
