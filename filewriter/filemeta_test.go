package filewriter

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// seekableBuffer adapts a bytes.Buffer to io.WriteSeeker for the file writer.
// Only forward-only writing is exercised, so Seek reports the current length.
type seekableBuffer struct {
	buf bytes.Buffer
}

func (s *seekableBuffer) Write(p []byte) (int, error) { return s.buf.Write(p) }

func (s *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	return int64(s.buf.Len()), nil
}

// parsedMetaElement is one decoded group-0002 element.
type parsedMetaElement struct {
	Tag    tag.Tag
	VR     string
	Length uint32
	Value  []byte
}

// parseExplicitVRElements decodes a run of explicit VR little endian elements.
// It fails the test on any misalignment, which is exactly what an odd-length
// value produces.
func parseExplicitVRElements(t *testing.T, data []byte) []parsedMetaElement {
	t.Helper()

	var out []parsedMetaElement
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		var group, element uint16
		if err := binary.Read(r, binary.LittleEndian, &group); err != nil {
			break
		}
		if err := binary.Read(r, binary.LittleEndian, &element); err != nil {
			t.Fatalf("truncated tag after %d elements", len(out))
		}

		vrBytes := make([]byte, 2)
		if _, err := r.Read(vrBytes); err != nil {
			t.Fatalf("truncated VR for tag (%04X,%04X)", group, element)
		}
		vr := string(vrBytes)

		// A misaligned stream shows up here as a VR that is not two uppercase
		// letters, which is a far clearer failure than a length mismatch later.
		for _, c := range vrBytes {
			if c < 'A' || c > 'Z' {
				t.Fatalf("element %d: VR %q is not valid — stream is misaligned "+
					"(an odd-length value was written without padding)", len(out), vr)
			}
		}

		var length uint32
		if isShortVR(vr) {
			var short uint16
			if err := binary.Read(r, binary.LittleEndian, &short); err != nil {
				t.Fatalf("truncated length for %s", vr)
			}
			length = uint32(short)
		} else {
			if _, err := r.Seek(2, 1); err != nil { // reserved
				t.Fatalf("truncated reserved bytes for %s", vr)
			}
			if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
				t.Fatalf("truncated length for %s", vr)
			}
		}

		value := make([]byte, length)
		if _, err := r.Read(value); err != nil && length > 0 {
			t.Fatalf("truncated value for tag (%04X,%04X)", group, element)
		}

		out = append(out, parsedMetaElement{
			Tag: tag.New(group, element), VR: vr, Length: length, Value: value,
		})
	}

	return out
}

// TestWriteFileMetaInfoUsesCorrectTags pins the group-0002 tag assignments to
// the DICOM data dictionary (PS3.6). These were previously wrong — the SOP
// Class UID was written to (0002,0010), the Transfer Syntax tag — which made
// every file this library produced unreadable by any conforming DICOM tool,
// including this library's own reader.
func TestWriteFileMetaInfoUsesCorrectTags(t *testing.T) {
	var sb seekableBuffer
	w := NewDCMFileWriter(filebase.NewFileWriter(&sb))

	meta := &FileMetaInfo{
		MediaStorageSOPClassUID:      "1.2.840.10008.5.1.4.1.1.2",
		MediaStorageSOPInstanceUID:   "1.2.3.4.5.6.7.8.9.100",
		TransferSyntaxUID:            "1.2.840.10008.1.2.1",
		ImplementationClassUID:       "1.2.826.0.1.3680043.10.511",
		ImplementationVersionName:    "GO-DICOM-1.2.0",
		SourceApplicationEntityTitle: "SRC_AE",
	}

	if err := w.WriteFileMetaInfo(meta); err != nil {
		t.Fatalf("WriteFileMetaInfo: %v", err)
	}

	elements := parseExplicitVRElements(t, sb.buf.Bytes())
	byTag := make(map[tag.Tag]parsedMetaElement, len(elements))
	for _, e := range elements {
		byTag[e.Tag] = e
	}

	want := []struct {
		tag   tag.Tag
		vr    string
		value string
	}{
		{tag.New(0x0002, 0x0002), "UI", "1.2.840.10008.5.1.4.1.1.2"},
		{tag.New(0x0002, 0x0003), "UI", "1.2.3.4.5.6.7.8.9.100"},
		{tag.New(0x0002, 0x0010), "UI", "1.2.840.10008.1.2.1"},
		{tag.New(0x0002, 0x0012), "UI", "1.2.826.0.1.3680043.10.511"},
		{tag.New(0x0002, 0x0013), "SH", "GO-DICOM-1.2.0"},
		{tag.New(0x0002, 0x0016), "AE", "SRC_AE"},
	}

	for _, tc := range want {
		got, ok := byTag[tc.tag]
		if !ok {
			t.Errorf("tag %s missing from the meta header", tc.tag)
			continue
		}
		if got.VR != tc.vr {
			t.Errorf("tag %s: VR = %q, want %q", tc.tag, got.VR, tc.vr)
		}
		if trimped := trimPad(got.Value); trimped != tc.value {
			t.Errorf("tag %s: value = %q, want %q", tc.tag, trimped, tc.value)
		}
	}

	// (0002,0001) File Meta Information Version is Type 1 — always required.
	if _, ok := byTag[tag.New(0x0002, 0x0001)]; !ok {
		t.Error("(0002,0001) FileMetaInformationVersion is missing")
	}

	// Group length must be present and first.
	if len(elements) == 0 || elements[0].Tag != tag.New(0x0002, 0x0000) {
		t.Error("(0002,0000) group length must be the first element")
	}
}

// TestWriteFileMetaInfoPadsToEvenLength verifies every meta value is even
// length. An odd value silently misaligns everything after it.
func TestWriteFileMetaInfoPadsToEvenLength(t *testing.T) {
	var sb seekableBuffer
	w := NewDCMFileWriter(filebase.NewFileWriter(&sb))

	// Every one of these has an odd character count.
	meta := &FileMetaInfo{
		MediaStorageSOPClassUID:      "1.2.840.10008.5.1.4.1.1.2", // 25
		MediaStorageSOPInstanceUID:   "1.2.3.4.5.6.7.8.9",         // 17
		TransferSyntaxUID:            "1.2.840.10008.1.2",         // 18 (even)
		ImplementationVersionName:    "ODD-NAME-X",                // 10 (even)
		SourceApplicationEntityTitle: "ODD_AE",                    // 6 (even)
	}

	if err := w.WriteFileMetaInfo(meta); err != nil {
		t.Fatalf("WriteFileMetaInfo: %v", err)
	}

	for _, e := range parseExplicitVRElements(t, sb.buf.Bytes()) {
		if e.Length%2 != 0 {
			t.Errorf("tag %s has odd length %d", e.Tag, e.Length)
		}
	}
}

// TestWriteDataElementPadsOddValues verifies the writer pads odd-length values
// rather than trusting callers to do it.
func TestWriteDataElementPadsOddValues(t *testing.T) {
	tests := []struct {
		name    string
		vr      string
		value   string
		wantPad byte
	}{
		{"UI pads with NUL", "UI", "1.2.3.4.5", 0x00},
		{"PN pads with space", "PN", "Doe^Jon", ' '},
		{"LO pads with space", "LO", "ODD", ' '},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb seekableBuffer
			w := NewDCMFileWriter(filebase.NewFileWriter(&sb))

			err := w.WriteDataElement(&DataElement{
				Tag:    tag.New(0x0010, 0x0020),
				VR:     tc.vr,
				Value:  []byte(tc.value),
				Length: uint32(len(tc.value)),
			}, true)
			if err != nil {
				t.Fatalf("WriteDataElement: %v", err)
			}

			elements := parseExplicitVRElements(t, sb.buf.Bytes())
			if len(elements) != 1 {
				t.Fatalf("parsed %d elements, want 1", len(elements))
			}

			got := elements[0]
			if got.Length%2 != 0 {
				t.Errorf("length %d is odd", got.Length)
			}
			if int(got.Length) != len(tc.value)+1 {
				t.Fatalf("length = %d, want %d", got.Length, len(tc.value)+1)
			}
			if got.Value[len(got.Value)-1] != tc.wantPad {
				t.Errorf("pad byte = %#02x, want %#02x", got.Value[len(got.Value)-1], tc.wantPad)
			}
		})
	}
}

// TestWriteDataElementDoesNotMutateCaller verifies padding does not write back
// into the caller's slice.
func TestWriteDataElementDoesNotMutateCaller(t *testing.T) {
	var sb seekableBuffer
	w := NewDCMFileWriter(filebase.NewFileWriter(&sb))

	value := []byte("1.2.3.4.5") // 9 bytes, odd
	elem := &DataElement{
		Tag: tag.New(0x0008, 0x0018), VR: "UI", Value: value, Length: uint32(len(value)),
	}

	if err := w.WriteDataElement(elem, true); err != nil {
		t.Fatalf("WriteDataElement: %v", err)
	}

	if len(value) != 9 {
		t.Errorf("caller's slice length changed to %d, want 9", len(value))
	}
	if elem.Length != 9 {
		t.Errorf("caller's element length changed to %d, want 9", elem.Length)
	}
}

// trimPad removes trailing NUL and space padding.
func trimPad(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
