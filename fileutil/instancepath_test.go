package fileutil_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/fileutil"
)

// A SOP Instance UID arrives from a network peer, and the CLI used to join it
// into an output path unchecked:
//
//	filename := filepath.Join(c.outputDir, sopInstanceUID+".dcm")
//
// filepath.Join cleans a path but does not confine it, so a UID of
// "../../etc/cron.d/pwn" escaped the output directory and let anything that could
// reach the port choose where a file was written.
func TestInstanceFilePathRefusesTraversal(t *testing.T) {
	dir := t.TempDir()

	hostile := []string{
		"../../../../tmp/evil",
		"../../etc/cron.d/pwn",
		"..",
		"../sibling",
		"/etc/passwd",
		"/absolute",
		"subdir/../../escape",
		`..\..\windows`,
		"1.2.3/../../../escape",
	}

	for _, value := range hostile {
		path, err := fileutil.InstanceFilePath(dir, value)
		if err == nil {
			t.Errorf("InstanceFilePath accepted %q and returned %q", value, path)
			continue
		}
		if path != "" {
			t.Errorf("InstanceFilePath(%q) returned both a path %q and an error", value, path)
		}
	}
}

func TestInstanceFilePathAcceptsRealUIDs(t *testing.T) {
	dir := t.TempDir()

	for _, value := range []string{
		"1.2.3.4",
		"1.2.840.10008.5.1.4.1.1.2",
		"1.2.840.113619.2.55.3.604688119.971.1618395946.499",
		"0.0",
	} {
		path, err := fileutil.InstanceFilePath(dir, value)
		if err != nil {
			t.Errorf("InstanceFilePath rejected the valid UID %q: %v", value, err)
			continue
		}
		if want := filepath.Join(dir, value+".dcm"); path != want {
			t.Errorf("InstanceFilePath(%q) = %q, want %q", value, path, want)
		}
	}
}

func TestInstanceFilePathRefusesMalformedUIDs(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		value string
		why   string
	}{
		{"", "empty"},
		{"1.2.3.a", "a letter is not dotted decimal"},
		{"1..2", "an empty component"},
		{"1.2.", "a trailing dot leaves an empty component"},
		{".1.2", "a leading dot leaves an empty component"},
		{"12345", "no dot at all, so not a UID"},
		{"1.2.3 4", "a space"},
		{"1.2.3\x00", "a NUL, which some filesystems truncate at"},
		{"1.2.3\n", "a newline"},
		{strings.Repeat("1.", 40) + "1", "longer than the 64 characters PS3.5 9.1 allows"},
	}

	for _, tc := range cases {
		if _, err := fileutil.InstanceFilePath(dir, tc.value); err == nil {
			t.Errorf("InstanceFilePath accepted %q (%s)", tc.value, tc.why)
		}
	}
}

// The length limit is worth its own check: a UID at the limit must still work, or
// the fix has broken instances that were previously storable.
func TestInstanceFilePathAcceptsAUIDAtTheLengthLimit(t *testing.T) {
	dir := t.TempDir()

	// 64 characters exactly: "1." repeated 31 times is 62, plus "12" is 64.
	value := strings.Repeat("1.", 31) + "12"
	if len(value) != fileutil.MaxUIDLength {
		t.Fatalf("the test UID is %d characters, not %d", len(value), fileutil.MaxUIDLength)
	}

	if _, err := fileutil.InstanceFilePath(dir, value); err != nil {
		t.Errorf("a UID of exactly %d characters was refused: %v", fileutil.MaxUIDLength, err)
	}

	if _, err := fileutil.InstanceFilePath(dir, value+"1"); err == nil {
		t.Errorf("a UID of %d characters was accepted", len(value)+1)
	}
}

func TestValidateUIDForPathErrorsAreExplanatory(t *testing.T) {
	// The error has to say what is wrong, since it surfaces to an operator whose
	// modality is failing to store.
	err := fileutil.ValidateUIDForPath("../escape")
	if err == nil {
		t.Fatal("ValidateUIDForPath accepted a traversal")
	}
	if !strings.Contains(err.Error(), "dotted decimal") {
		t.Errorf("the error does not say what a UID is: %v", err)
	}
}
