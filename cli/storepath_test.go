package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/fileutil"
)

// storescp, getscu and qrscp all name the file they write after a SOP Instance
// UID supplied by the peer. Each used to join it into the output path unchecked:
//
//	filename := filepath.Join(c.outputDir, sopInstanceUID+".dcm")
//
// filepath.Join cleans a path but does not confine it, so a peer sending a UID of
// "../../etc/cron.d/pwn" chose where the file went. Anything that could reach the
// port could write a file anywhere the process could.
//
// This is the demonstration, kept as a test so the traversal cannot come back: it
// asserts both that the old expression escapes and that the replacement does not.
func TestTheOldPathConstructionEscapedAndTheNewOneDoesNot(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "received")
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		t.Fatalf("creating the output directory: %v", err)
	}

	hostile := "../../etc/cron.d/pwn"

	// What the code used to do.
	escaped := filepath.Join(outputDir, hostile+".dcm")
	if strings.HasPrefix(escaped, outputDir) {
		t.Fatalf("this test no longer demonstrates anything: %q is inside %q",
			escaped, outputDir)
	}

	// What it does now.
	if _, err := fileutil.InstanceFilePath(outputDir, hostile); err == nil {
		t.Errorf("InstanceFilePath accepted the traversal %q", hostile)
	}

	// And a real UID still works, so the fix has not broken storing anything.
	got, err := fileutil.InstanceFilePath(outputDir, "1.2.840.113619.2.55.3.12345")
	if err != nil {
		t.Fatalf("a valid UID was refused: %v", err)
	}
	if !strings.HasPrefix(got, outputDir) {
		t.Errorf("a valid UID produced %q, which is outside %q", got, outputDir)
	}
}

// Every CLI command that writes an instance must validate the UID. Checking the
// source keeps a new command, or a revert, from reintroducing the traversal in a
// path no test happens to exercise.
func TestNoCLICommandJoinsAUIDIntoAPathDirectly(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		checked++

		for i, line := range strings.Split(string(source), "\n") {
			if !strings.Contains(line, "filepath.Join") {
				continue
			}
			// A UID in a Join argument is the pattern to catch. The safe path goes
			// through fileutil.InstanceFilePath, which does the Join itself after
			// validating.
			if strings.Contains(line, "SOPInstance") || strings.Contains(line, "sopInstance") ||
				strings.Contains(line, "InstanceUID") || strings.Contains(line, "instanceUID") {
				t.Errorf("%s:%d joins a SOP Instance UID into a path directly:\n\t%s\n\n"+
					"Use fileutil.InstanceFilePath, which validates the UID first. The UID "+
					"comes from the peer, and filepath.Join does not confine it.",
					name, i+1, strings.TrimSpace(line))
			}
		}
	}

	if checked < 5 {
		t.Fatalf("only %d source files were scanned; the check is not looking at the package", checked)
	}
}
