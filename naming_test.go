package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The 1.5.0 rename left the old binary name behind in nine places, including
// doc.go, which is what pkg.go.dev shows, and the release notes, which told
// everyone to download files that do not exist (#120). The rename was checked
// by grepping the files it touched, which is why it missed the ones it did not.
//
// binaryNamePatterns are the ways the old name appears as a command.
var binaryNamePatterns = []*regexp.Regexp{
	// "dicom show", "$ dicom help", "`dicom qrscp`" — but not "go-dicom show".
	regexp.MustCompile(`(^|[^a-zA-Z0-9_/-])dicom (show|info|convert|codify|tag-doc|anonymize|validate|` +
		`echoscu|echoscp|storescu|storescp|findscu|movescu|getscu|commitscu|qrscp|help|version)\b`),
	// "go build -o dicom", "./dicom", "GODICOM: ./dicom"
	regexp.MustCompile(`-o dicom\b`),
	regexp.MustCompile(`(^|[^-])\./dicom\b`), // ./dicom, including "${GODICOM:-./dicom}"
	regexp.MustCompile(`:-\./dicom\b`),
	// The release assets are go-dicom-<platform>; anything else is a broken link.
	regexp.MustCompile("`dicom-(linux|macos|windows)"),
}

// exempt files where the old name is deliberate: the history in the CHANGELOG,
// and the tests and comments that exist because the name must not be hardcoded.
var exemptFromNameCheck = map[string]bool{
	"CHANGELOG.md":         true,
	"naming_test.go":       true,
	"main.go":              true, // the comment explaining why the name is a variable
	"cli/help.go":          true, // the same
	"cli/cli_test.go":      true, // passes "dicom" to prove the name is not assumed
	"cli/commands_test.go": true, // runs the binary under three names on purpose
}

// TestTheBinaryIsCalledGoDicomEverywhere greps the tracked files. The CLI is one
// binary with one name; a document that calls it something else sends a reader
// to a command they do not have.
func TestTheBinaryIsCalledGoDicomEverywhere(t *testing.T) {
	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}

	for _, path := range strings.Fields(string(out)) {
		if exemptFromNameCheck[path] || strings.HasPrefix(path, ".vscode/") {
			continue
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".sh", ".yml", ".yaml", ".mod", "":
		default:
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			for _, pattern := range binaryNamePatterns {
				if pattern.MatchString(line) {
					t.Errorf("%s calls the binary \"dicom\": %s", path, strings.TrimSpace(line))
				}
			}
		}
	}
}
