package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// No command changed a file: fixing one attribute meant writing Go, or
// reaching for dcmodify or pydicom (#332).

func runModify(args ...string) error {
	return (&ModifyCommand{}).Execute(args)
}

// copyFixture copies the committed big endian fixture into a temporary
// directory, so a test may change it.
func copyFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "in.dcm")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// rawValue returns an element's stored bytes, or nil when it is absent.
func rawValue(ds *dataset.Dataset, t tag.Tag) []byte {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	b, _ := elem.GetValue().([]byte)
	return b
}

// TestModifyAcrossTransferSyntaxes makes the same edits in files of six
// transfer syntaxes, from pydicom's corpus, and reads each result back: the
// values are the ones asked for, in the sequence item asked for, the transfer
// syntax is the source's, the pixel data's bytes are the source's (nothing
// decoded or encoded again), the instance keeps its UID, and the input is
// not touched.
func TestModifyAcrossTransferSyntaxes(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{
		"CT_small.dcm",           // explicit VR little endian
		"MR_small_implicit.dcm",  // implicit VR little endian
		"MR_small_bigendian.dcm", // explicit VR big endian
		"MR_small_RLE.dcm",       // RLE Lossless, encapsulated
		"JPEG2000.dcm",           // JPEG 2000, encapsulated
		"image_dfl.dcm",          // deflated
	} {
		t.Run(name, func(t *testing.T) {
			in := filepath.Join(corpus, name)
			before, err := os.ReadFile(in)
			if err != nil {
				t.Skipf("%s is not present: %v", name, err)
			}
			out := filepath.Join(t.TempDir(), "out.dcm")
			if err := runModify(
				"-set", "PatientName=Doe^Jane",
				"-insert", "AccessionNumber=A1234",
				"-ignore-missing", "-delete", "StudyTime",
				"-insert", "RequestAttributesSequence[1].RequestedProcedureID=RP1",
				"-insert", `AcquisitionMatrix=0\64\64\0`,
				in, out,
			); err != nil {
				t.Fatalf("modify: %v", err)
			}

			after, err := os.ReadFile(in)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("the input was changed")
			}
			sourceMeta, source := readAnonymized(t, in)
			meta, ds := readAnonymized(t, out)

			if meta.TransferSyntaxUID != sourceMeta.TransferSyntaxUID {
				t.Errorf("transfer syntax %s, want the source's %s", meta.TransferSyntaxUID, sourceMeta.TransferSyntaxUID)
			}
			if got, want := text(ds, tagSOPInstanceUID), text(source, tagSOPInstanceUID); got != want || meta.MediaStorageSOPInstanceUID != want {
				t.Errorf("SOP Instance UID %q, meta %q; want both kept as %q", got, meta.MediaStorageSOPInstanceUID, want)
			}
			if got := text(ds, tagPatientName); got != "Doe^Jane" {
				t.Errorf("PatientName = %q", got)
			}
			if got := text(ds, tag.New(0x0008, 0x0050)); got != "A1234" {
				t.Errorf("AccessionNumber = %q", got)
			}
			if ds.Contains(tag.New(0x0008, 0x0030)) {
				t.Error("StudyTime is still there")
			}
			matrix := rawValue(ds, tag.New(0x0018, 0x1310))
			if len(matrix) != 8 || binary.LittleEndian.Uint16(matrix[2:]) != 64 || binary.LittleEndian.Uint16(matrix[4:]) != 64 {
				t.Errorf("AcquisitionMatrix = % x, want 0, 64, 64, 0", matrix)
			}
			seq, err := ds.GetSequence(tag.New(0x0040, 0x0275))
			if err != nil || seq.Length() != 2 {
				t.Fatalf("RequestAttributesSequence: %v, want two items", err)
			}
			item, _ := ds.GetSequenceDataset(tag.New(0x0040, 0x0275), 1)
			if got := text(item, tag.New(0x0040, 0x1001)); got != "RP1" {
				t.Errorf("RequestedProcedureID in item 1 = %q", got)
			}
			if got, want := rawValue(ds, tag.New(0x7FE0, 0x0010)), rawValue(source, tag.New(0x7FE0, 0x0010)); len(want) == 0 || !bytes.Equal(got, want) {
				t.Errorf("Pixel Data: %d bytes, want the source's %d unchanged", len(got), len(want))
			}
		})
	}
}

// TestModifyRefusesWhatTheVRDoesNotAllow: each value is checked against its
// VR before anything is written, so a refused edit leaves no output.
func TestModifyRefusesWhatTheVRDoesNotAllow(t *testing.T) {
	in := copyFixture(t)
	for _, c := range []struct{ arg, want string }{
		{"StudyDate=20201301", "DA"},
		{"Modality=us", "CS"},
		{"SOPInstanceUID=1.2.03", "UI"},
		{"SeriesNumber=1.5", "IS"},
		{"ImageType=ORIGINAL", "multiplicity"},
		{"PatientName=Jérôme", "Specific Character Set"},
		{"StudyInstanceUID=" + strings.Repeat("1.", 40) + "1", "UI"},
	} {
		out := filepath.Join(t.TempDir(), "out.dcm")
		err := runModify("-set", c.arg, in, out)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("-set %s: got %v, want a refusal naming %s", c.arg, err, c.want)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("-set %s: an output was written", c.arg)
		}
	}
	for _, c := range []struct{ arg, want string }{
		{"Rows=70000", "16-bit"},
		{"Rows=-1", "unsigned"},
		{"PixelData=1", "Pixel Data"},
		{"TransferSyntaxUID=1.2.840.10008.1.2", "file meta"},
		{"SpecificCharacterSet=ISO_IR 100", "ISO_IR 192"},
		{"NoSuchKeyword=1", "keyword"},
		{"PatientName[0].PatientID=1", "not a sequence"},
		{"ReferencedImageSequence.ReferencedSOPInstanceUID=1.2", "needs an item"},
	} {
		out := filepath.Join(t.TempDir(), "out.dcm")
		err := runModify("-insert", c.arg, in, out)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("-insert %s: got %v, want a refusal naming %q", c.arg, err, c.want)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("-insert %s: an output was written", c.arg)
		}
	}
}

// TestModifyNamesPrivateAttributesByTheirCreator: an insert reserves a block
// for its creator in the first free slot (PS3.5 7.8.1), a later edit finds
// the attribute through the creator whatever block it landed in, and a
// private tag given by number with no reservation is refused.
func TestModifyNamesPrivateAttributesByTheirCreator(t *testing.T) {
	in := copyFixture(t)
	out := filepath.Join(t.TempDir(), "out.dcm")
	if err := runModify("-insert", `(0029,xx10,"ACME 1.0",LO)=first`, "-set", `(0029,xx10,"ACME 1.0")=second`, in, out); err != nil {
		t.Fatalf("modify: %v", err)
	}
	_, ds := readAnonymized(t, out)
	if got := text(ds, tag.New(0x0029, 0x0010)); got != "ACME 1.0" {
		t.Errorf("(0029,0010) = %q, want the creator", got)
	}
	if got := text(ds, tag.New(0x0029, 0x1010)); got != "second" {
		t.Errorf("(0029,1010) = %q, want second", got)
	}

	err := runModify("-insert", "(0029,1110)=x", in, filepath.Join(t.TempDir(), "o.dcm"))
	if err == nil || !strings.Contains(err.Error(), "private creator") {
		t.Errorf("a private tag with no reservation: %v", err)
	}
	err = runModify("-set", `(0029,xx10,"NOBODY")=x`, in, filepath.Join(t.TempDir(), "o.dcm"))
	if err == nil || !strings.Contains(err.Error(), "not in the file") {
		t.Errorf("-set through a creator the file lacks: %v", err)
	}
}

// TestModifyGivesANewUIDOnlyWhenAsked: the data set and the file meta change
// together with -new-uid, and neither changes without it.
func TestModifyGivesANewUIDOnlyWhenAsked(t *testing.T) {
	in := copyFixture(t)
	_, source := readAnonymized(t, in)
	original := text(source, tagSOPInstanceUID)

	kept := filepath.Join(t.TempDir(), "kept.dcm")
	if err := runModify("-set", "StationName=X", in, kept); err != nil {
		t.Fatal(err)
	}
	meta, ds := readAnonymized(t, kept)
	if text(ds, tagSOPInstanceUID) != original || meta.MediaStorageSOPInstanceUID != original {
		t.Errorf("without -new-uid the UID became %q (meta %q)", text(ds, tagSOPInstanceUID), meta.MediaStorageSOPInstanceUID)
	}

	renewed := filepath.Join(t.TempDir(), "new.dcm")
	if err := runModify("-new-uid", in, renewed); err != nil {
		t.Fatal(err)
	}
	meta, ds = readAnonymized(t, renewed)
	got := text(ds, tagSOPInstanceUID)
	if got == original || !strings.HasPrefix(got, "2.25.") || meta.MediaStorageSOPInstanceUID != got {
		t.Errorf("-new-uid gave %q, meta %q; want a new UID in both", got, meta.MediaStorageSOPInstanceUID)
	}
}

// TestModifyWritesOverTheInputOnlyWhenAsked: the output may not be the input
// without -in-place, and -in-place replaces the input with the edited file.
func TestModifyWritesOverTheInputOnlyWhenAsked(t *testing.T) {
	in := copyFixture(t)
	if err := runModify("-set", "StationName=X", in, in); err == nil || !strings.Contains(err.Error(), "-in-place") {
		t.Errorf("output == input: %v", err)
	}
	if err := runModify("-in-place", "-set", "StationName=EDITED", in); err != nil {
		t.Fatalf("-in-place: %v", err)
	}
	meta, ds := readAnonymized(t, in)
	if got := text(ds, tag.New(0x0008, 0x1010)); got != "EDITED" {
		t.Errorf("StationName = %q after -in-place", got)
	}
	if meta.TransferSyntaxUID != "1.2.840.10008.1.2.2" {
		t.Errorf("the big endian input was rewritten as %s", meta.TransferSyntaxUID)
	}
	entries, _ := os.ReadDir(filepath.Dir(in))
	if len(entries) != 1 {
		t.Errorf("-in-place left %d files in the directory, want only the input", len(entries))
	}
}

// TestModifyDeletesAnItemAndReportsWhatIsMissing: a delete path ending in an
// item removes that item; a -set or -delete of something absent fails unless
// -ignore-missing says to skip it.
func TestModifyDeletesAnItemAndReportsWhatIsMissing(t *testing.T) {
	in := copyFixture(t)
	two := filepath.Join(t.TempDir(), "two.dcm")
	if err := runModify("-insert", "ReferencedImageSequence[1].ReferencedSOPInstanceUID=1.2.3", in, two); err != nil {
		t.Fatal(err)
	}
	one := filepath.Join(t.TempDir(), "one.dcm")
	if err := runModify("-delete", "ReferencedImageSequence[0]", two, one); err != nil {
		t.Fatal(err)
	}
	_, ds := readAnonymized(t, one)
	seq, err := ds.GetSequence(tag.New(0x0008, 0x1140))
	if err != nil || seq.Length() != 1 {
		t.Fatalf("after deleting item 0: %v, %v", seq, err)
	}
	item, _ := seq.Get(0)
	if got := text(item.(*dataset.Dataset), tag.New(0x0008, 0x1155)); got != "1.2.3" {
		t.Errorf("the item left is %q, want the one that was second", got)
	}

	missing := filepath.Join(t.TempDir(), "m.dcm")
	if err := runModify("-delete", "OperatorsName", in, missing); err == nil || !strings.Contains(err.Error(), "not in the file") {
		t.Errorf("deleting an absent attribute: %v", err)
	}
	if err := runModify("-ignore-missing", "-delete", "OperatorsName", "-set", "StationName=Y", in, missing); err != nil {
		t.Errorf("-ignore-missing: %v", err)
	}
}

// countEverywhere counts the occurrences of t in ds and in every item of
// every sequence under it, and returns them with the number of elements in
// all.
func countEverywhere(t *testing.T, ds *dataset.Dataset, want tag.Tag) (occurrences, elements int) {
	t.Helper()
	if err := ds.IterAll(func(_ *dataset.Dataset, e *dataelem.DataElement) error {
		elements++
		if e.GetTag() == want {
			occurrences++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return occurrences, elements
}

// TestModifySetAndDeleteAllReachEveryItem: an attribute that pydicom's
// rtplan.dcm repeats four times, three sequences deep, is removed or changed
// at all four places by one flag, and nothing else is touched. The same edit
// by path would need the item counts of two sequences.
func TestModifySetAndDeleteAllReachEveryItem(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	in := filepath.Join(corpus, "rtplan.dcm")
	if _, err := os.Stat(in); err != nil {
		t.Skipf("rtplan.dcm is not present: %v", err)
	}
	_, source := readAnonymized(t, in)
	coefficient := tag.New(0x300A, 0x010C) // CumulativeDoseReferenceCoefficient
	referenced := tag.New(0x300C, 0x0051)  // ReferencedDoseReferenceNumber
	has, total := countEverywhere(t, source, coefficient)
	if has != 4 {
		t.Fatalf("the corpus file has %d occurrences, the test expects 4", has)
	}

	out := filepath.Join(t.TempDir(), "out.dcm")
	if err := runModify("-delete-all", "CumulativeDoseReferenceCoefficient", "-set-all", "(300C,0051)=9", in, out); err != nil {
		t.Fatalf("modify: %v", err)
	}
	_, ds := readAnonymized(t, out)
	if left, elements := countEverywhere(t, ds, coefficient); left != 0 || elements != total-4 {
		t.Errorf("after -delete-all: %d occurrences left and %d elements, want 0 and %d", left, elements, total-4)
	}
	nine := 0
	if err := ds.IterAll(func(_ *dataset.Dataset, e *dataelem.DataElement) error {
		if e.GetTag() == referenced {
			if b, _ := e.GetValue().([]byte); strings.TrimSpace(string(b)) == "9" {
				nine++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if nine != 4 {
		t.Errorf("-set-all changed %d of the 4 occurrences", nine)
	}

	// Nothing in the file is named by this one: the edit is refused, as a
	// -set of an absent attribute is, unless -ignore-missing says to skip.
	err := runModify("-set-all", "PatientAge=030Y", in, filepath.Join(t.TempDir(), "o.dcm"))
	if err == nil || !strings.Contains(err.Error(), "not in the file") {
		t.Errorf("-set-all of an attribute that is nowhere: %v", err)
	}
	if err := runModify("-ignore-missing", "-delete-all", "PatientAge", in, filepath.Join(t.TempDir(), "o.dcm")); err != nil {
		t.Errorf("-ignore-missing -delete-all: %v", err)
	}
	for _, arg := range []string{"ReferencedImageSequence[0].ReferencedSOPInstanceUID", "BeamSequence[*].BeamName"} {
		err := runModify("-delete-all", arg, in, filepath.Join(t.TempDir(), "o.dcm"))
		if err == nil || !strings.Contains(err.Error(), "one attribute") {
			t.Errorf("-delete-all %s: %v, want a refusal that it names one attribute", arg, err)
		}
	}
}

// TestModifyTakesABinaryValueFromAFile: the bytes of a file become the value
// of an OB and of an OW attribute, in a big endian file whose writer swaps
// the words, with an odd length padded to even (PS3.5 7.1.1); a file that is
// not a whole number of words, or an attribute that is not binary, is
// refused with no output written.
func TestModifyTakesABinaryValueFromAFile(t *testing.T) {
	in := copyFixture(t)
	dir := t.TempDir()
	blob := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(blob, []byte{1, 2, 3, 4, 5, 6, 7, 8}, 0o644); err != nil {
		t.Fatal(err)
	}
	odd := filepath.Join(dir, "odd.bin")
	if err := os.WriteFile(odd, []byte{9, 8, 7}, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.dcm")
	if err := runModify(
		"-insert-file", `(0009,xx10,"ACME",OW)=`+blob,
		"-insert-file", `(0009,xx11,"ACME",OB)=`+odd,
		"-insert-file", `(0009,xx12,"ACME",OB)=`+blob,
		"-set-file", `(0009,xx12,"ACME")=`+odd,
		in, out,
	); err != nil {
		t.Fatalf("modify: %v", err)
	}
	meta, ds := readAnonymized(t, out)
	if meta.TransferSyntaxUID != "1.2.840.10008.1.2.2" {
		t.Errorf("transfer syntax %s, want the big endian source's", meta.TransferSyntaxUID)
	}
	if got := rawValue(ds, tag.New(0x0009, 0x1010)); !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Errorf("the OW value read back as % x, want the file's bytes", got)
	}
	if got := rawValue(ds, tag.New(0x0009, 0x1011)); !bytes.Equal(got, []byte{9, 8, 7, 0}) {
		t.Errorf("the odd OB value read back as % x, want the file padded with a NUL", got)
	}
	if got := rawValue(ds, tag.New(0x0009, 0x1012)); !bytes.Equal(got, []byte{9, 8, 7, 0}) {
		t.Errorf("-set-file over an existing value read back as % x", got)
	}

	for _, c := range []struct{ flag, arg, want string }{
		{"-insert-file", `(0009,xx10,"ACME",OW)=` + odd, "2-byte words"},
		{"-insert-file", `(0009,xx10,"ACME",OF)=` + odd, "4-byte words"},
		{"-insert-file", `(0009,xx10,"ACME",LO)=` + blob, "not binary"},
		{"-set-file", "PatientName=" + blob, "not binary"},
		{"-insert-file", `(0009,xx10,"ACME",OB)=` + filepath.Join(dir, "absent.bin"), "absent.bin"},
		{"-insert-file", `(0009,xx10,"ACME",OB)=` + dir, "directory"},
		{"-insert-file", "PixelData=" + blob, "Pixel Data"},
		{"-insert", `(0009,xx10,"ACME",OB)=` + blob, "-set-file"},
	} {
		refused := filepath.Join(t.TempDir(), "o.dcm")
		err := runModify(c.flag, c.arg, in, refused)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %s: got %v, want a refusal naming %q", c.flag, c.arg, err, c.want)
		}
		if _, statErr := os.Stat(refused); statErr == nil {
			t.Errorf("%s %s: an output was written", c.flag, c.arg)
		}
	}
}
