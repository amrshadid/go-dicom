package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
)

// redactions are what -redact is given in the tests below: two regions, one
// across the edge of a small frame; a detector alone; and both.
var redactions = map[string]redaction{
	"two regions": {regions: []dataset.PixelRegion{
		{X: 0, Y: 0, Width: 20, Height: 10}, {X: 40, Y: 50, Width: 30, Height: 30},
	}},
	"auto":                {auto: true},
	"a region and auto":   {regions: []dataset.PixelRegion{{X: 0, Y: 0, Width: 20, Height: 10}}, auto: true},
	"one across the edge": {regions: []dataset.PixelRegion{{X: 60, Y: 60, Width: 400, Height: 400}}},
}

// TestAnonymizeRedactingWithPixelsLeftInTheFileWritesTheSameFile: the files
// the command writes with -redact are the same, byte for byte but for a
// Patient ID, whether it read the pixels (as it always did) or left them in
// the input and filled the regions, and detected text, as it copied them
// into the output a frame at a time. It is checked for every file of pydicom's
// corpus that has pixels, with regions given, with a detector, and with both,
// and a file that one way refuses the other refuses for the same reason.
// Without the corpus (GODICOM_PYDICOM_DATA) it is skipped.
func TestAnonymizeRedactingWithPixelsLeftInTheFileWritesTheSameFile(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	written, refused := 0, 0
	for _, entry := range entries {
		input := filepath.Join(corpus, entry.Name())
		if !entry.Type().IsRegular() || !hasPixelData(input) {
			continue
		}
		for name, redact := range redactions {
			anonymizer := anonymize.NewAnonymizer(anonymize.BasicProfile)
			ac := &AnonymizeCommand{redact: redact}

			wholePath := filepath.Join(dir, "whole.dcm")
			_ = os.Remove(wholePath)
			wholeErr := ac.anonymizeFileAs(anonymizer, input, wholePath, anonymizeRead{whole: true})
			want, _ := os.ReadFile(wholePath)

			path := filepath.Join(dir, "infile.dcm")
			_ = os.Remove(path)
			err := ac.anonymizeFileAs(anonymizer, input, path, anonymizeRead{threshold: leftInFileAbove})
			// An image whose frames will not decode is refused when its pixels
			// are written, not when they are read, so the message says where
			// it was found and then what the whole read says.
			if (err == nil) != (wholeErr == nil) || err != nil && !strings.Contains(err.Error(), wholeErr.Error()) {
				t.Errorf("%s, %s: %v; read whole: %v", entry.Name(), name, err, wholeErr)
				continue
			}
			if err != nil {
				refused++
				continue
			}
			got, _ := os.ReadFile(path)
			if where := differenceOutsidePatientID(t, want, got); where >= 0 {
				t.Errorf("%s, %s: %d bytes, %d read whole; they differ at %d, which is not in a Patient ID",
					entry.Name(), name, len(got), len(want), where)
			}
			written++
		}
	}
	if written == 0 {
		t.Fatal("no file was written")
	}
	t.Logf("%d outputs compared, %d refused alike", written, refused)
}

// TestShareRedactingWithPixelsLeftInTheFileWritesTheSamePicture: the picture
// share writes with -redact is the same bytes whether it read the whole image
// and filled it, as it always did, or left the pixels in the file, looked for
// text a frame at a time, and filled and decoded each frame as its picture was
// made; for every file of pydicom's corpus that has pixels, with regions
// given, with a detector, and with both. Where one way refuses, so does the
// other. Without the corpus (GODICOM_PYDICOM_DATA) it is skipped.
func TestShareRedactingWithPixelsLeftInTheFileWritesTheSamePicture(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	compared, refused := 0, 0
	for _, entry := range entries {
		input := filepath.Join(corpus, entry.Name())
		if !entry.Type().IsRegular() || !hasPixelData(input) {
			continue
		}
		for name, redact := range redactions {
			sc := &ShareCommand{redact: redact}
			for _, frame := range []string{"1", "all"} {
				opts := imageOptions{format: "png", frame: frame, quality: 90}
				wholeOut := filepath.Join(dir, "whole", "p.png")
				leftOut := filepath.Join(dir, "left", "p.png")
				_ = os.RemoveAll(filepath.Join(dir, "whole"))
				_ = os.RemoveAll(filepath.Join(dir, "left"))
				for _, d := range []string{"whole", "left"} {
					if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
						t.Fatal(err)
					}
				}

				wholeWritten, _, wholeErr := sc.shareWhole(input, wholeOut, opts)
				leftWritten, _, handled, leftErr := sc.shareRedactedInFile(input, leftOut, opts)
				if !handled {
					// Not a file that is read a frame at a time: shared whole.
					continue
				}
				if (leftErr == nil) != (wholeErr == nil) {
					t.Errorf("%s, %s, frame %s: left in the file: %v; whole: %v", entry.Name(), name, frame, leftErr, wholeErr)
					continue
				}
				if leftErr != nil {
					refused++
					continue
				}
				if len(leftWritten) != len(wholeWritten) {
					t.Errorf("%s, %s, frame %s: %d pictures, %d whole", entry.Name(), name, frame, len(leftWritten), len(wholeWritten))
					continue
				}
				for i := range leftWritten {
					a, _ := os.ReadFile(leftWritten[i])
					b, _ := os.ReadFile(wholeWritten[i])
					if !bytes.Equal(a, b) {
						t.Errorf("%s, %s, frame %s: picture %d is %d bytes and differs from %d whole",
							entry.Name(), name, frame, i+1, len(a), len(b))
					}
					compared++
				}
			}
		}
	}
	if compared == 0 {
		t.Fatal("no picture was compared")
	}
	t.Logf("%d pictures compared, %d refused alike", compared, refused)
}
