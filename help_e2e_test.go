package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildCLI builds the binary once for a test and returns its path.
//
// The commands this exercises parse flags with flag.ExitOnError, so their -h calls
// os.Exit and cannot be driven from inside the test process. A subprocess is also
// what a user actually runs.
func buildCLI(t *testing.T) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "dicom")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binary
}

// advertisedCommands returns the command names the top-level help lists, which is
// the set a user can see and will therefore try.
func advertisedCommands(t *testing.T, binary string) []string {
	t.Helper()

	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running with no arguments should print help: %v\n%s", err, out)
	}

	// The command list is indented four spaces under each category heading.
	pattern := regexp.MustCompile(`(?m)^ {4}([a-z][a-z-]*)\s`)
	var names []string
	for _, m := range pattern.FindAllStringSubmatch(string(out), -1) {
		names = append(names, m[1])
	}
	if len(names) < 10 {
		t.Fatalf("only found %d commands in the help output, so the parse is wrong:\n%s",
			len(names), out)
	}
	return names
}

// Every command the CLI advertises must answer `help <name>` with its help, not with
// a claim that it does not exist.
//
// The top-level list advertised sixteen commands and `help <name>` knew seven of
// them. The nine network commands each said "unknown command 'storescu'" while
// `storescu -h` printed a full page and the command ran fine — so a user following
// the CLI's own closing instruction, "Use 'go-dicom help <command>' for more
// information on a specific command", was told the command did not exist.
func TestHelpWorksForEveryCommand(t *testing.T) {
	binary := buildCLI(t)

	for _, name := range advertisedCommands(t, binary) {
		t.Run(name, func(t *testing.T) {
			out, err := exec.Command(binary, "help", name).CombinedOutput()
			text := string(out)

			if err != nil {
				t.Fatalf("`help %s` failed: %v\n%s", name, err, text)
			}
			if strings.Contains(text, "unknown command") {
				t.Fatalf("`help %s` reports the command as unknown, but it is in the "+
					"top-level list:\n%s", name, text)
			}
			if strings.TrimSpace(text) == "" {
				t.Fatalf("`help %s` printed nothing", name)
			}
			// The output should be about the command asked for, not the general help.
			if !strings.Contains(text, name) {
				t.Errorf("`help %s` printed something that never mentions %q:\n%s",
					name, name, text)
			}
		})
	}
}

// A name that is not a command must still be reported as one, so that a typo is
// told apart from a command that exists.
func TestHelpRejectsAnUnknownCommandEndToEnd(t *testing.T) {
	binary := buildCLI(t)

	out, err := exec.Command(binary, "help", "nosuchcommand").CombinedOutput()
	if err == nil {
		t.Errorf("`help nosuchcommand` exited 0; a typo must not look like success:\n%s", out)
	}
	if !strings.Contains(string(out), "nosuchcommand") {
		t.Errorf("the error should name what was asked for, got:\n%s", out)
	}
}
