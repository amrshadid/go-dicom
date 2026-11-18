package compress_test

import (
	"bytes"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// endsOnFF are images whose JPEG-LS scan, before the fix, ended on a byte of
// 0xFF with the EOI marker straight after it. Found by searching small random
// images; libjpeg decoded the last sample of each wrong.
var endsOnFF = []struct {
	width, height int
	samples       []byte
}{
	{4, 1, []byte{159, 132, 230, 6}},
	{5, 1, []byte{184, 6, 45, 162, 228}},
	{1, 3, []byte{113, 240, 177}},
	{2, 4, []byte{100, 162, 123, 117, 164, 183, 228, 226}},
}

// TestJPEGLSScanDoesNotEndOnFF proves no scan ends on 0xFF: the byte after a
// 0xFF in a JPEG-LS scan has its high bit clear (T.87 A.1), and the marker that
// follows a scan does not, so a decoder that honors the rule ends the scan one
// byte early. Checked for the images that did, and for a thousand random ones.
func TestJPEGLSScanDoesNotEndOnFF(t *testing.T) {
	check := func(name string, stream []byte) {
		t.Helper()
		end := bytes.LastIndex(stream, []byte{0xFF, 0xD9})
		if end < 1 {
			t.Fatalf("%s: no EOI marker", name)
		}
		if stream[end-1] == 0xFF {
			t.Errorf("%s: the scan ends on 0xFF, straight before EOI: % x", name, stream[max(0, end-4):])
		}
	}
	for _, im := range endsOnFF {
		stream, err := compress.EncodeJPEGLS(im.samples, im.width, im.height, 1, 8)
		if err != nil {
			t.Fatal(err)
		}
		check("known", stream)
		got, err := compress.NewJPEGLSDecompressor().Decompress(stream)
		if err != nil || !bytes.Equal(got, im.samples) {
			t.Errorf("the stuffed stream does not decode here: %v", err)
		}
	}
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 1000; i++ {
		w, h := 1+r.Intn(9), 1+r.Intn(5)
		samples := make([]byte, w*h)
		for j := range samples {
			samples[j] = byte(r.Intn(256))
		}
		stream, err := compress.EncodeJPEGLS(samples, w, h, 1, 8)
		if err != nil {
			t.Fatal(err)
		}
		check("random", stream)
	}
}

// TestJPEGLSScanEndingOnFFIsReadByLibjpeg proves libjpeg, through
// pylibjpeg-libjpeg, returns every sample of the images whose scans used to end
// on 0xFF. Skipped, saying so, where it is not installed.
func TestJPEGLSScanEndingOnFFIsReadByLibjpeg(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not on PATH, so libjpeg cannot be asked to read the output")
	}
	if err := exec.Command(python, "-c", "import libjpeg, numpy").Run(); err != nil {
		t.Skip("pylibjpeg-libjpeg is not installed; install it to verify the encoder against libjpeg")
	}
	dir := t.TempDir()
	var args []string
	for i, im := range endsOnFF {
		stream, err := compress.EncodeJPEGLS(im.samples, im.width, im.height, 1, 8)
		if err != nil {
			t.Fatal(err)
		}
		stem := filepath.Join(dir, string(rune('a'+i)))
		if err := os.WriteFile(stem+".jls", stream, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stem+".raw", im.samples, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, stem)
	}
	script := `
import sys, numpy as np
from libjpeg import decode
for stem in sys.argv[1:]:
    want = np.frombuffer(open(stem + ".raw", "rb").read(), np.uint8)
    got = np.asarray(decode(open(stem + ".jls", "rb").read(), 0)).ravel()
    if got.size != want.size or not (got == want).all():
        sys.exit(f"{stem}: libjpeg decoded {list(got)}, want {list(want)}")
print(f"libjpeg decoded all {len(sys.argv) - 1} to every sample")
`
	out, err := exec.Command(python, append([]string{"-c", script}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Logf("%s", out)
}
