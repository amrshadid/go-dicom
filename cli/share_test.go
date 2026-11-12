package cli

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// From an image to a picture that can be handed on took anonymize, then
// convert, and leaving out the first gave a picture of an image nobody had
// checked (#241).

func runShare(args ...string) error {
	return (&ShareCommand{}).Execute(args)
}

// TestShareRefusesAnImageThatMayBeDrawnOn: the burned-in rule is anonymize's.
// An image of a kind that is drawn on is not rendered until someone says
// where the text is, or that there is none.
func TestShareRefusesAnImageThatMayBeDrawnOn(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "us.dcm"), filepath.Join(dir, "us.png")
	writeDrawnOnInstance(t, in, "US")

	err := runShare(in, out)
	if err == nil {
		t.Fatal("an ultrasound was rendered with its pixels unexamined")
	}
	for _, want := range []string{"-redact", "-allow-burned-in"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %s", err, want)
		}
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("the refused picture was written")
	}

	if err := runShare("-allow-burned-in", in, out); err != nil {
		t.Fatalf("-allow-burned-in: %v", err)
	}
	// Every sample is 200 and the image has no window, so the data range
	// has no width and the picture is black; what matters is that it is there.
	if got := decodeGray(t, out); len(got) != 16 {
		t.Errorf("the picture has %d pixels, want 16", len(got))
	}

	ct := filepath.Join(dir, "ct.dcm")
	writeDrawnOnInstance(t, ct, "CT")
	if err := runShare(ct, filepath.Join(dir, "ct.png")); err != nil {
		t.Errorf("a CT was refused: %v", err)
	}
}

// TestShareRedactsBeforeRendering: the rectangle is black in the picture and
// the rest is the image.
func TestShareRedactsBeforeRendering(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "us.dcm"), filepath.Join(dir, "us.png")
	writeDrawnOnInstance(t, in, "US")
	before, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}

	// A window that shows 200 as mid-gray, so black is the redaction's.
	if err := runShare("-redact", "0,0,4,1", "--window", "200,100", in, out); err != nil {
		t.Fatalf("share -redact: %v", err)
	}
	got := decodeGray(t, out)
	for i, v := range got {
		if i < 4 && v != 0 {
			t.Errorf("pixel %d of the redacted row is %d", i, v)
		}
		if i >= 4 && (v < 120 || v > 136) {
			t.Errorf("pixel %d outside the rectangle is %d, want mid-gray", i, v)
		}
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Doe")) {
		t.Error("the patient's name is in the picture file")
	}
	for off := 8; off+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[off:]))
		if kind := string(data[off+4 : off+8]); kind != "IHDR" && kind != "IDAT" && kind != "IEND" {
			t.Errorf("the PNG has a %s chunk", kind)
		}
		off += 12 + length
	}
	if after, _ := os.ReadFile(in); !bytes.Equal(after, before) {
		t.Error("the input file was changed")
	}
}

// TestShareFormatIsTheExtension: there is no --format to get wrong.
func TestShareFormatIsTheExtension(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ct.dcm")
	writeDrawnOnInstance(t, in, "CT")

	for _, name := range []string{"a.jpg", "b.jpeg", "c.JPG"} {
		out := filepath.Join(dir, name)
		if err := runShare(in, out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f, err := os.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		_, err = jpeg.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Errorf("%s is not a JPEG: %v", name, err)
		}
	}
	for _, name := range []string{"x.dcm", "x.gif", "x"} {
		err := runShare(in, filepath.Join(dir, name))
		if err == nil || !strings.Contains(err.Error(), ".png") {
			t.Errorf("%s gave %v, want an error naming the extensions", name, err)
		}
	}
	if err := runShare(in); err == nil {
		t.Error("one argument was accepted")
	}
}

// TestShareACompressedUltrasound: redaction decompresses first, so the
// images that most need it can be shared.
func TestShareACompressedUltrasound(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	in := filepath.Join(corpus, "SC_rgb_rle.dcm")
	if _, err := os.Stat(in); err != nil {
		t.Skipf("SC_rgb_rle.dcm is not present: %v", err)
	}
	out := filepath.Join(t.TempDir(), "sc.png")
	if err := runShare(in, out); err == nil {
		t.Fatal("a secondary capture was rendered unasked")
	}
	if err := runShare("-redact", "0,0,100,10", in, out); err != nil {
		t.Fatalf("share -redact on a compressed image: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("the picture was not written: %v", err)
	}
}
