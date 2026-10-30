package anonymize_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

func stringValue(t *testing.T, ds *dataset.Dataset, tg tag.Tag) string {
	t.Helper()
	elem, ok := ds.Get(tg)
	if !ok {
		return ""
	}
	switch v := elem.GetValue().(type) {
	case []byte:
		return strings.TrimRight(string(v), "\x00 ")
	case string:
		return strings.TrimRight(v, "\x00 ")
	}
	t.Fatalf("%s holds %T", tg, elem.GetValue())
	return ""
}

// TestDeidentificationIsRecorded: PS3.15 E.1.1 says Patient Identity Removed
// shall be YES and the method recorded, and nothing wrote group 0012, so a
// downstream system could not tell the data had been de-identified (#213).
func TestDeidentificationIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		profile anonymize.Profile
		codes   []string
	}{
		{anonymize.BasicProfile, []string{"113100"}},
		{anonymize.RetainUIDsProfile, []string{"113100", "113110"}},
		{anonymize.CleanDescriptorsProfile, []string{"113100", "113105"}},
	} {
		ds := dataset.NewDataset()
		withValue(t, ds, tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John")
		if err := anonymize.NewAnonymizer(tc.profile).Anonymize(ds); err != nil {
			t.Fatalf("Anonymize: %v", err)
		}

		if got := stringValue(t, ds, tag.New(0x0012, 0x0062)); got != "YES" {
			t.Errorf("profile %d: Patient Identity Removed is %q, want YES", tc.profile, got)
		}
		if stringValue(t, ds, tag.New(0x0012, 0x0063)) == "" {
			t.Errorf("profile %d: no De-identification Method", tc.profile)
		}

		var codes []string
		if elem, ok := ds.Get(tag.New(0x0012, 0x0064)); ok {
			if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
				for _, item := range seq.Items() {
					code := item.(*dataset.Dataset)
					if scheme := stringValue(t, code, tag.New(0x0008, 0x0102)); scheme != "DCM" {
						t.Errorf("profile %d: coding scheme %q, want DCM", tc.profile, scheme)
					}
					codes = append(codes, stringValue(t, code, tag.New(0x0008, 0x0100)))
				}
			}
		}
		if strings.Join(codes, ",") != strings.Join(tc.codes, ",") {
			t.Errorf("profile %d: method codes %v, want %v", tc.profile, codes, tc.codes)
		}
	}
}

// TestRetainUIDsKeepsEveryUID: the Retain UIDs Option keeps every attribute
// the Basic Profile would remap. It consulted a table of four, so a Referenced
// SOP Instance UID was remapped while the SOP Instance UID it points at was
// kept, and the reference no longer resolved (#213).
func TestRetainUIDsKeepsEveryUID(t *testing.T) {
	const instance, frame = "1.2.3.4.5.6", "1.2.3.4.5.7"
	item := dataset.NewDataset()
	withValue(t, item, tag.New(0x0008, 0x1155), dataelem.UI, instance)
	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatal(err)
	}

	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0008, 0x0018), dataelem.UI, instance)
	withValue(t, ds, tag.New(0x0020, 0x0052), dataelem.UI, frame)
	// Referenced Image Sequence, which the profile leaves in place.
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1140), dataelem.SQ, seq)); err != nil {
		t.Fatal(err)
	}

	if err := anonymize.NewAnonymizer(anonymize.RetainUIDsProfile).Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	for _, c := range []struct {
		ds   *dataset.Dataset
		tag  tag.Tag
		want string
	}{
		{ds, tag.New(0x0008, 0x0018), instance},
		{ds, tag.New(0x0020, 0x0052), frame},
		{item, tag.New(0x0008, 0x1155), instance},
	} {
		if got := stringValue(t, c.ds, c.tag); got != c.want {
			t.Errorf("%s is %q under Retain UIDs, want %q", c.tag, got, c.want)
		}
	}
}

// TestFileMetaGetsTheRemappedUID: GetDataset leaves the file meta out, so its
// Media Storage SOP Instance UID kept the original, and a caller writing back
// the meta it read kept the link in the file (#213).
func TestFileMetaGetsTheRemappedUID(t *testing.T) {
	const original = "1.2.3.4.5.6"
	ds := dataset.NewDataset()
	withValue(t, ds, tag.New(0x0008, 0x0018), dataelem.UI, original)
	meta := &filereader.FileMetaInfo{MediaStorageSOPInstanceUID: original}

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	a.AnonymizeFileMeta(meta)

	remapped := stringValue(t, ds, tag.New(0x0008, 0x0018))
	if remapped == original || meta.MediaStorageSOPInstanceUID != remapped {
		t.Errorf("SOP Instance UID %q, meta %q: want the same new UID in both",
			remapped, meta.MediaStorageSOPInstanceUID)
	}

	retain := anonymize.NewAnonymizer(anonymize.RetainUIDsProfile)
	kept := &filereader.FileMetaInfo{MediaStorageSOPInstanceUID: original}
	retain.AnonymizeFileMeta(kept)
	if kept.MediaStorageSOPInstanceUID != original {
		t.Errorf("Retain UIDs changed the meta's UID to %q", kept.MediaStorageSOPInstanceUID)
	}
}
