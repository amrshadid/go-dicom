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

// The command exists to produce files that can leave the building, and it
// wrote an ultrasound with the patient's name drawn across it as readily as
// anything else (#233).

// writeDrawnOnInstance writes a 4 by 4, 8-bit image of the given modality,
// every sample 200.
func writeDrawnOnInstance(t *testing.T, path, modality string) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(tg tag.Tag, vr dataelem.VR, value interface{}) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	add(tagSOPClassUID, dataelem.UI, "1.2.840.10008.5.1.4.1.1.6.1")
	add(tagSOPInstanceUID, dataelem.UI, "1.2.826.0.1.3680043.10.511.9.7")
	add(tag.New(0x0008, 0x0060), dataelem.CS, modality)
	add(tagPatientName, dataelem.PN, "Doe^John")
	add(tagStudyUID, dataelem.UI, testStudyUID)
	add(tag.New(0x0028, 0x0002), dataelem.US, us(1))
	add(tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	add(tag.New(0x0028, 0x0010), dataelem.US, us(4))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(4))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))
	px := make([]byte, 16)
	for i := range px {
		px[i] = 200
	}
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, px)
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.6.1", "1.2.826.0.1.3680043.10.511.9.7", ds); err != nil {
		t.Fatal(err)
	}
}

// TestAnonymizeRefusesAnImageThatMayBeDrawnOn: without being told where the
// text is, or that there is none, an image of a kind that is drawn on is not
// written at all.
func TestAnonymizeRefusesAnImageThatMayBeDrawnOn(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "us.dcm"), filepath.Join(dir, "out.dcm")
	writeDrawnOnInstance(t, in, "US")

	err := runAnonymize(in, out)
	if err == nil {
		t.Fatal("an ultrasound was written with its pixels unexamined")
	}
	for _, want := range []string{"-redact", "-allow-burned-in", "US"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %s", err, want)
		}
	}
	if _, statErr := filepath.Glob(out); statErr != nil {
		t.Fatal(statErr)
	}
	if matches, _ := filepath.Glob(out); len(matches) != 0 {
		t.Error("the refused file was written")
	}

	// The caller's word that it is clean is enough.
	if err := runAnonymize("-allow-burned-in", in, out); err != nil {
		t.Fatalf("-allow-burned-in: %v", err)
	}
	_, ds := readAnonymized(t, out)
	if px := pixelsOf(t, ds); px[0] != 200 {
		t.Error("-allow-burned-in changed the pixels")
	}

	// A CT is not a kind that is drawn on, and needs neither.
	ct := filepath.Join(dir, "ct.dcm")
	writeDrawnOnInstance(t, ct, "CT")
	if err := runAnonymize(ct, filepath.Join(dir, "ct-out.dcm")); err != nil {
		t.Errorf("a CT was refused: %v", err)
	}
}

// TestAnonymizeRedacts: the regions are black in the file written, the rest
// of the image is as it was, and the file says its pixels were cleaned.
func TestAnonymizeRedacts(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "us.dcm"), filepath.Join(dir, "out.dcm")
	writeDrawnOnInstance(t, in, "US")

	if err := runAnonymize("-redact", "0,0,4,1", "-redact", "3,3,1,1", in, out); err != nil {
		t.Fatalf("anonymize -redact: %v", err)
	}
	_, ds := readAnonymized(t, out)
	px := pixelsOf(t, ds)
	for i, v := range px {
		want := byte(200)
		if i < 4 || i == 15 {
			want = 0
		}
		if v != want {
			t.Errorf("pixel %d is %d, want %d", i, v, want)
		}
	}
	if got := text(ds, tag.New(0x0028, 0x0301)); got != "NO" {
		t.Errorf("Burned In Annotation is %q, want NO", got)
	}
	if got := text(ds, tagIdentityGone); got != "YES" {
		t.Errorf("Patient Identity Removed is %q", got)
	}

	for _, bad := range []string{"1,2,3", "a,b,c,d", "0,0,0,5", "0,0,4,-1", ""} {
		if err := runAnonymize("-redact", bad, in, filepath.Join(dir, "bad.dcm")); err == nil {
			t.Errorf("-redact %q was accepted", bad)
		}
	}
	// A region that misses the image fails the file; nothing is written.
	missed := filepath.Join(dir, "missed.dcm")
	if err := runAnonymize("-redact", "10,10,2,2", in, missed); err == nil {
		t.Error("a region outside the image was accepted")
	}
	if matches, _ := filepath.Glob(missed); len(matches) != 0 {
		t.Error("a file whose region missed was written")
	}
}

func pixelsOf(t *testing.T, ds *dataset.Dataset) []byte {
	t.Helper()
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("the output has no Pixel Data")
	}
	raw, _ := elem.GetValue().([]byte)
	if len(raw) < 16 {
		t.Fatalf("Pixel Data is %d bytes", len(raw))
	}
	return raw
}

// TestAnonymizeRedactsACompressedImage: the images that carry burned-in text
// are mostly stored compressed, and -redact refused them (#240). The output
// is native, the region is black, and every other pixel is what the
// compressed image decodes to.
func TestAnonymizeRedactsACompressedImage(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	in := filepath.Join(corpus, "SC_rgb_rle.dcm")
	if _, err := os.Stat(in); err != nil {
		t.Skipf("SC_rgb_rle.dcm is not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.dcm")
	if err := runAnonymize("-redact", "0,0,100,10", in, out); err != nil {
		t.Fatalf("anonymize -redact: %v", err)
	}

	_, source := readAnonymized(t, in)
	meta, ds := readAnonymized(t, out)
	if meta.TransferSyntaxUID != "1.2.840.10008.1.2.1" {
		t.Errorf("transfer syntax %q, want Explicit VR Little Endian", meta.TransferSyntaxUID)
	}
	want, err := source.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	got := pixelsOf(t, ds)
	// 100 by 100 RGB: the first ten rows are the region.
	region := 10 * 100 * 3
	if len(got) < len(want) {
		t.Fatalf("Pixel Data is %d bytes, the image %d", len(got), len(want))
	}
	for i := 0; i < region; i++ {
		if got[i] != 0 {
			t.Fatalf("byte %d of the region is %d", i, got[i])
		}
	}
	if !bytes.Equal(got[region:len(want)], want[region:]) {
		t.Error("pixels outside the region differ from the decoded source")
	}
}
