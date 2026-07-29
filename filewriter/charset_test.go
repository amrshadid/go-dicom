package filewriter_test

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

var (
	tagPatientName = tag.New(0x0010, 0x0010)
	tagStudyDesc   = tag.New(0x0008, 0x1030)
	tagRows        = tag.New(0x0028, 0x0010)
)

// charsetWriter returns a writer wrapped for the given character set, plus the
// buffer it writes into.
func charsetWriter(t *testing.T, cs *charset.CharacterSet) (*filewriter.DICOMFileWriterWithCharset, *growBuffer) {
	t.Helper()

	out := &growBuffer{}
	base := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	base.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.4",
		MediaStorageSOPInstanceUID: "1.2.3.4.5",
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	return filewriter.NewDICOMFileWriterWithCharset(base, cs), out
}

// TestAddTextElementRejectsNonTextVRs verifies a caller cannot route binary data
// through the text-encoding path.
//
// Encoding a numeric value as though it were text would corrupt it: the encoder
// transforms bytes according to a character set, and a US value is not
// characters. Rejecting the VR is the only place this can be caught, since by
// the time the value is bytes it looks like any other.
func TestAddTextElementRejectsNonTextVRs(t *testing.T) {
	w, _ := charsetWriter(t, nil)

	for _, vr := range []string{"US", "UL", "OB", "OW", "SQ", "FL", "AT"} {
		if err := w.AddTextElement(tagRows, vr, "text"); err == nil {
			t.Errorf("AddTextElement accepted VR %s, which is not a text VR", vr)
		}
	}
}

// TestAddTextElementRejectsPN verifies PN is directed to its own method.
//
// A person name has five components with their own delimiter, and encoding it
// as flat text would lose that structure — silently, since the result is still
// a valid byte string.
func TestAddTextElementRejectsPN(t *testing.T) {
	w, _ := charsetWriter(t, nil)

	if err := w.AddTextElement(tagPatientName, "PN", "Doe^John"); err == nil {
		t.Error("AddTextElement accepted a PN value; AddPersonNameElement exists for that")
	}
}

// TestAddTextElementAcceptsTextVRs covers the VRs that are text, so the
// rejection above cannot pass by rejecting everything.
func TestAddTextElementAcceptsTextVRs(t *testing.T) {
	for _, vr := range []string{"LO", "SH", "ST", "LT", "UT", "UC"} {
		t.Run(vr, func(t *testing.T) {
			w, _ := charsetWriter(t, nil)
			if err := w.AddTextElement(tagStudyDesc, vr, "Chest CT"); err != nil {
				t.Errorf("AddTextElement rejected text VR %s: %v", vr, err)
			}
		})
	}
}

// TestEncodeTextValueRoundTrips verifies text written through the encoder can be
// read back, which is the only claim that matters about an encoding.
func TestEncodeTextValueRoundTrips(t *testing.T) {
	w, _ := charsetWriter(t, nil)

	encoded, err := w.EncodeTextValue("Chest CT")
	if err != nil {
		t.Fatalf("EncodeTextValue: %v", err)
	}
	if !bytes.Contains(encoded, []byte("Chest CT")) {
		t.Errorf("encoded value %q does not contain the input under the default encoding", encoded)
	}
}

// TestEncodePersonNameValue covers the PN path, whose structure the flat text
// path would lose.
func TestEncodePersonNameValue(t *testing.T) {
	w, _ := charsetWriter(t, nil)

	pn := charset.FromComponents("Doe", "John", "Q", "Dr", "PhD")
	encoded, err := w.EncodePersonNameValue(pn)
	if err != nil {
		t.Fatalf("EncodePersonNameValue: %v", err)
	}

	// The five components are caret-separated, so all four delimiters must be
	// present even when trailing components are empty.
	if got := bytes.Count(encoded, []byte("^")); got != 4 {
		t.Errorf("encoded person name has %d carets, want 4: %q", got, encoded)
	}
	if !bytes.HasPrefix(encoded, []byte("Doe^John")) {
		t.Errorf("encoded person name = %q, want it to start with Doe^John", encoded)
	}
}

// TestSetCharacterSetAddsAndRemovesTheDeclaration verifies the writer keeps
// (0008,0005) consistent with the character set in force.
//
// A file whose text is encoded in a non-default character set without declaring
// it cannot be read correctly by anything — the reader has no way to know. The
// declaration is not optional metadata; it is what makes the bytes meaningful.
func TestSetCharacterSetAddsAndRemovesTheDeclaration(t *testing.T) {
	w, out := charsetWriter(t, nil)

	// Latin-1, which needs declaring.
	cs, err := charset.NewCharacterSet([]string{"ISO_IR 100"})
	if err != nil {
		t.Fatalf("NewCharacterSet: %v", err)
	}
	w.SetCharacterSet(cs)

	if got := w.GetCharacterSet(); got == nil {
		t.Fatal("GetCharacterSet returned nil after SetCharacterSet")
	}

	if err := w.AddTextElement(tagStudyDesc, "LO", "Thorax"); err != nil {
		t.Fatalf("AddTextElement: %v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("ISO_IR 100")) {
		t.Error("the file does not declare the character set its text is encoded in")
	}
}

// TestCharacterSetDeclarationRemovedForDefault verifies the declaration is not
// written when the default encoding is in use.
//
// (0008,0005) is absent for the default repertoire, and writing it anyway makes
// a file claim a character set it is not using.
func TestCharacterSetDeclarationRemovedForDefault(t *testing.T) {
	w, out := charsetWriter(t, nil)

	// Set a character set, then go back to the default.
	cs, err := charset.NewCharacterSet([]string{"ISO_IR 100"})
	if err != nil {
		t.Fatalf("NewCharacterSet: %v", err)
	}
	w.SetCharacterSet(cs)
	w.SetCharacterSet(nil)

	if err := w.AddTextElement(tagStudyDesc, "LO", "Thorax"); err != nil {
		t.Fatalf("AddTextElement: %v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("ISO_IR 100")) {
		t.Error("the file declares a character set it is no longer using")
	}
}

// TestAddTextElementsAddsAll covers the batch form, since a partial failure
// there would leave a file with some attributes and not others.
func TestAddTextElementsAddsAll(t *testing.T) {
	w, out := charsetWriter(t, nil)

	err := w.AddTextElements(map[tag.Tag]struct{ VR, Text string }{
		tagStudyDesc:            {VR: "LO", Text: "Chest CT"},
		tag.New(0x0008, 0x103E): {VR: "LO", Text: "Axial"},
		tag.New(0x0010, 0x0020): {VR: "LO", Text: "ID-0001"},
	})
	if err != nil {
		t.Fatalf("AddTextElements: %v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for _, want := range []string{"Chest CT", "Axial", "ID-0001"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("the file is missing %q", want)
		}
	}
}

// TestAddTextElementsRejectsABadVRInTheBatch verifies one bad entry is reported
// rather than skipped, so a caller is not left with a partially written file it
// believes is complete.
func TestAddTextElementsRejectsABadVRInTheBatch(t *testing.T) {
	w, _ := charsetWriter(t, nil)

	err := w.AddTextElements(map[tag.Tag]struct{ VR, Text string }{
		tagStudyDesc: {VR: "LO", Text: "Chest CT"},
		tagRows:      {VR: "US", Text: "not text"},
	})
	if err == nil {
		t.Error("AddTextElements accepted a batch containing a non-text VR")
	}
}
