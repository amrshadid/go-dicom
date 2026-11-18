package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// info printed no Frames line for a multi-frame file, for it read Number of
// Frames from (0020,1209), and gave the size of an Icon Image Sequence item
// as the image's, and another patient's ID as the patient's, because the
// elements inside sequences replaced the top-level ones (#335). The values
// are those pydicom reads at the top level of each file.
func TestInfoSummaryIsTheTopLevelOfTheDataSet(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, c := range []struct {
		file, line string
	}{
		{"SC_rgb_rle_2frame.dcm", "Frames:          2\n"},
		{"SC_rgb_rle_32bit_2frame.dcm", "Frames:          2\n"},
		{"examples_overlay.dcm", "Rows:            300\nColumns:         484\n"},
		{"CT_small.dcm", "Patient ID:      1CT1\n"},
	} {
		out, err := captured(t, func() error { return NewInfoCommand().Execute([]string{filepath.Join(dir, c.file)}) })
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if !strings.Contains(out, c.line) {
			t.Errorf("info %s prints\n%s\nwant a line %q", c.file, out, c.line)
		}
	}

	// A single-frame file has no Number of Frames and prints no Frames line.
	out, err := captured(t, func() error { return NewInfoCommand().Execute([]string{filepath.Join(dir, "CT_small.dcm")}) })
	if err != nil || strings.Contains(out, "Frames:") {
		t.Errorf("info CT_small.dcm prints a Frames line:\n%s", out)
	}
}
