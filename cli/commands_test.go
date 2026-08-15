package cli

import (
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// allCommands returns one of every command the binary registers.
//
// Kept in step with the help output by TestHelpListingMatchesRealCommands, so a
// network command cannot be added to one without the other noticing.
func allCommands() []Command {
	return []Command{
		&ShowCommand{},
		&InfoCommand{},
		&ConvertCommand{},
		&TagDocCommand{},
		&CodifyCommand{},
		&HelpCommand{},
		&VersionCommand{},
		&EchoSCUCommand{},
		&EchoSCPCommand{},
		&StoreSCUCommand{},
		&StoreSCPCommand{},
		&CommitSCUCommand{},
		&FindSCUCommand{},
		&MoveSCUCommand{},
		&GetSCUCommand{},
		&QRSCPCommand{},
	}
}

// TestEveryCommandIsWellFormed covers the contract the CLI relies on for each
// command it dispatches to.
//
// A command with an empty name is unreachable — RegisterCommand keys the map by
// it, so it silently overwrites whatever else has an empty name. A duplicate
// name is worse: the second registration replaces the first, and the command
// that vanishes does so with no error anywhere.
func TestEveryCommandIsWellFormed(t *testing.T) {
	seen := make(map[string]Command)

	for _, cmd := range allCommands() {
		name := cmd.Name()

		if name == "" {
			t.Errorf("%T has an empty name; RegisterCommand keys on it, so it is unreachable", cmd)
			continue
		}
		if strings.TrimSpace(name) != name {
			t.Errorf("%T name %q has surrounding whitespace; it will never match an argument", cmd, name)
		}
		if previous, dup := seen[name]; dup {
			t.Errorf("%T and %T both claim the name %q; registration order decides which survives",
				previous, cmd, name)
		}
		seen[name] = cmd

		if cmd.Description() == "" {
			t.Errorf("%s has no description; it appears blank in the help output", name)
		}
	}
}

// TestAddFlagsIsSafeOnAFreshFlagSet verifies every command can install its flags
// without panicking.
//
// Registering the same flag name twice panics rather than returning an error, so
// a command that defines a duplicate takes the whole binary down the first time
// anybody runs it — including on --help.
func TestAddFlagsIsSafeOnAFreshFlagSet(t *testing.T) {
	for _, cmd := range allCommands() {
		t.Run(cmd.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("AddFlags panicked: %v", r)
				}
			}()
			fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
			fs.SetOutput(&strings.Builder{})
			cmd.AddFlags(fs)
		})
	}
}

// TestRegisterAndRunUnknownCommand covers the dispatch path for a name that was
// never registered, which is what a typo produces.
func TestRegisterAndRunUnknownCommand(t *testing.T) {
	c := NewCLI("dicom", "test")
	c.RegisterCommand(&EchoSCUCommand{})

	if err := c.Run([]string{"echoscp"}); err == nil {
		t.Error("running an unregistered command succeeded")
	}
}

// TestRegisterCommandIsKeyedByName documents that registration is a map write,
// so two commands sharing a name means one disappears.
func TestRegisterCommandIsKeyedByName(t *testing.T) {
	c := NewCLI("dicom", "test")

	c.RegisterCommand(&EchoSCUCommand{})
	if _, ok := c.Commands["echoscu"]; !ok {
		t.Fatal("the command was not registered under its own name")
	}
	if len(c.Commands) != 1 {
		t.Errorf("registered one command, map holds %d", len(c.Commands))
	}
}

// TestInstanceListParsing covers the repeatable -instance flag on commitscu.
//
// The value is "sopClassUID:sopInstanceUID", and a UID contains no colon, so the
// split is on the last one. Accepting a malformed value would send a storage
// commitment request naming an instance the archive cannot identify — and the
// requestor would then delete its copy on the strength of an answer about
// something else.
func TestInstanceListParsing(t *testing.T) {
	t.Run("accepts a well-formed pair", func(t *testing.T) {
		var l instanceList
		if err := l.Set("1.2.840.10008.5.1.4.1.1.2:1.2.3.4"); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if len(l) != 1 {
			t.Fatalf("got %d instances, want 1", len(l))
		}
		if l[0].SOPClassUID != "1.2.840.10008.5.1.4.1.1.2" {
			t.Errorf("SOP class = %q", l[0].SOPClassUID)
		}
		if l[0].SOPInstanceUID != "1.2.3.4" {
			t.Errorf("SOP instance = %q", l[0].SOPInstanceUID)
		}
	})

	t.Run("repeatable", func(t *testing.T) {
		var l instanceList
		for _, v := range []string{"1.2.3:4.5.6", "1.2.3:7.8.9"} {
			if err := l.Set(v); err != nil {
				t.Fatalf("Set(%q): %v", v, err)
			}
		}
		if len(l) != 2 {
			t.Errorf("got %d instances after two Set calls, want 2", len(l))
		}
	})

	t.Run("rejects malformed values", func(t *testing.T) {
		for _, v := range []string{
			"",       // nothing
			"1.2.3",  // no separator
			":1.2.3", // no SOP class
			"1.2.3:", // no instance
			":",      // both empty
		} {
			var l instanceList
			if err := l.Set(v); err == nil {
				t.Errorf("Set(%q) was accepted, giving %+v", v, l)
			}
		}
	})

	t.Run("String reports the count", func(t *testing.T) {
		var l instanceList
		_ = l.Set("1.2.3:4.5.6")
		if got := l.String(); !strings.Contains(got, "1") {
			t.Errorf("String() = %q, want it to mention the count", got)
		}
	})
}

// TestHelpListingMatchesRealCommands verifies the help output names commands
// that exist, and that every command it names has a description.
//
// The listing is a hardcoded slice and map. A name in the slice with no entry in
// the map is skipped by the loop that prints them, so the command vanishes from
// the help output while remaining perfectly runnable — nothing reports it, and a
// user simply never learns the command is there. A name in neither is a command
// nobody can discover at all.
func TestHelpListingMatchesRealCommands(t *testing.T) {
	real := make(map[string]bool)
	for _, cmd := range allCommands() {
		real[cmd.Name()] = true
	}

	for _, name := range netCommands {
		if !real[name] {
			t.Errorf("help lists %q, which is not a command", name)
		}
		if netDescriptions[name] == "" {
			t.Errorf("help lists %q with no description, so it is silently omitted from the output", name)
		}
	}

	// The reverse: a network command missing from the listing.
	networkish := []string{"echoscu", "echoscp", "storescu", "storescp",
		"findscu", "movescu", "getscu", "commitscu", "qrscp"}
	listed := make(map[string]bool)
	for _, n := range netCommands {
		listed[n] = true
	}
	for _, n := range networkish {
		if real[n] && !listed[n] {
			t.Errorf("%q exists but is not in the help listing, so nobody can discover it", n)
		}
	}

	// A description for a name that is not listed is dead weight, and usually
	// means a rename happened in one place only.
	for name := range netDescriptions {
		if !listed[name] {
			t.Errorf("netDescriptions has an entry for %q, which the listing does not include", name)
		}
	}
}

// The help command must be given the command registry.
//
// `help <name>` falls back to a command's own -h for anything it has no
// hand-written page for. That fallback needs the registry, and nothing was passing
// it: main registered a bare &HelpCommand{}, SetCommands was never called, and the
// map stayed nil. The nine network commands each reported "unknown command
// 'storescu'" while `storescu -h` printed a full page and the command ran.
//
// This checks the wiring rather than the output, because the network commands parse
// flags with flag.ExitOnError — their -h calls os.Exit, so it cannot be invoked from
// inside a test. TestHelpWorksForEveryCommand in the root package drives the built
// binary and covers the behavior end to end.
func TestTheHelpCommandIsGivenTheRegistry(t *testing.T) {
	c := NewCLI("go-dicom", "test")
	for _, cmd := range allCommands() {
		c.RegisterCommand(cmd)
	}

	help, ok := c.Commands["help"].(*HelpCommand)
	if !ok {
		t.Fatal("the help command is not registered, so nothing here is being tested")
	}

	if help.allCommands == nil {
		t.Fatal("the help command was never given the registry, so `help <name>` can " +
			"only answer for commands with a hand-written page and reports every " +
			"other one as unknown")
	}

	for name := range c.Commands {
		if _, found := help.allCommands[name]; !found {
			t.Errorf("the help command's registry is missing %q; `help %s` will report "+
				"it as an unknown command", name, name)
		}
	}
}

// And help must still reject a name that is not a command, rather than the registry
// fallback quietly succeeding on anything.
func TestHelpStillRejectsAnUnknownCommand(t *testing.T) {
	c := NewCLI("go-dicom", "test")
	for _, cmd := range allCommands() {
		c.RegisterCommand(cmd)
	}

	help := c.Commands["help"].(*HelpCommand)

	err := help.displayCommandHelp("definitely-not-a-command")
	if err == nil {
		t.Error("help accepted a command that does not exist; the registry fallback " +
			"must not turn every unknown name into a success")
	} else if !strings.Contains(err.Error(), "definitely-not-a-command") {
		t.Errorf("the error should name what was asked for, got: %v", err)
	}
}

// buildCLI builds the CLI binary once for a test and returns its path.
//
// Built by module path, not ".", because this package is not the main package — the
// binary's main lives at the repository root.
//
// The commands this exercises parse flags with flag.ExitOnError, so their -h calls
// os.Exit and cannot be driven from inside the test process. A subprocess is also
// what a user actually runs.
func buildCLI(t *testing.T) string {
	t.Helper()

	// The .exe matters: without it the build succeeds and every exec of the result
	// fails with "executable file not found in %PATH%", because Windows decides what
	// is runnable from the extension. Caught on the windows-latest runner.
	name := "dicom"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	binary := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", binary, "github.com/amrshadid/go-dicom")
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

	// A non-zero exit is not enough on its own: a binary that cannot start also
	// gives one, with no output, and this test used to read that as success. On
	// Windows, where the missing .exe meant nothing ran at all, it passed for that
	// reason. So require that the process actually ran and then exited non-zero.
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("`help nosuchcommand` did not run to a normal exit: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "nosuchcommand") {
		t.Errorf("the error should name what was asked for, got:\n%s", out)
	}
}

// `go-dicom help` and `go-dicom` with no arguments must print the same thing.
//
// They did not. displayMainHelp kept its own copy of the command list — seven file
// commands with their own descriptions — while the CLI's listing had all sixteen
// grouped by category. So `go-dicom help` never mentioned any of the nine network
// commands, which are most of what the tool does.
//
// This is the third place the command list was duplicated, and the second bug from
// it. TestHelpWorksForEveryCommand did not catch this one: it asked for help on each
// command by name and never compared the two listings, so the listing that omitted
// nine commands looked fine from every angle it checked.
func TestTheTwoTopLevelHelpListingsAgree(t *testing.T) {
	binary := buildCLI(t)

	bare, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running with no arguments: %v\n%s", err, bare)
	}
	viaHelp, err := exec.Command(binary, "help").CombinedOutput()
	if err != nil {
		t.Fatalf("`help`: %v\n%s", err, viaHelp)
	}

	if string(bare) != string(viaHelp) {
		t.Errorf("`go-dicom` and `go-dicom help` print different things.\n\n"+
			"bare:\n%s\nvia help:\n%s\n\n"+
			"Both answer the same question and must not be maintained separately.",
			bare, viaHelp)
	}

	// And the listing has to be complete, so that agreeing on a short list is not a
	// way to pass this.
	for _, name := range advertisedCommands(t, binary) {
		if !strings.Contains(string(viaHelp), name) {
			t.Errorf("`help` does not list %q", name)
		}
	}
	if n := len(advertisedCommands(t, binary)); n < 16 {
		t.Errorf("only %d commands are listed; the CLI has 16", n)
	}
}

// The help must name the binary the user actually ran.
//
// The same source ships under two names: `go install` builds it as go-dicom, after the
// last element of the module path, while the Makefile and the release assets call it
// dicom. The help text hardcoded go-dicom, so anyone who installed from a release got
// instructions for a command they did not have:
//
//	$ dicom
//	USAGE:
//	  go-dicom <command> [options] [arguments]
//	$ go-dicom
//	zsh: command not found: go-dicom
//
// Built under two different names here, because the name comes from argv and nothing
// short of a real exec exercises that.
func TestHelpNamesTheBinaryItWasInvokedAs(t *testing.T) {
	for _, name := range []string{"dicom", "go-dicom", "dcmtool"} {
		t.Run(name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), name)
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}

			build := exec.Command("go", "build", "-o", binary, "github.com/amrshadid/go-dicom")
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}

			// Both listings, and a per-command page, since each is rendered separately.
			for _, args := range [][]string{{}, {"help"}, {"help", "show"}} {
				out, err := exec.Command(binary, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s", name, args, err, out)
				}
				text := string(out)

				if !strings.Contains(text, name+" ") {
					t.Errorf("%s %v never names the binary as %q:\n%s", name, args, name, text)
				}
				// The other names must not appear at all.
				for _, wrong := range []string{"dicom", "go-dicom", "dcmtool"} {
					if wrong == name {
						continue
					}
					// "dicom" is a substring of "go-dicom", so only flag it where it is
					// not part of the name actually in use.
					stripped := strings.ReplaceAll(text, name, "")
					if strings.Contains(stripped, wrong+" <command>") ||
						strings.Contains(stripped, "'"+wrong+" help") {
						t.Errorf("%s %v tells the user to run %q:\n%s", name, args, wrong, text)
					}
				}
			}
		})
	}
}

// fixture returns a DICOM file committed to this repository, for tests that need to
// run a command against a real file.
func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "dataset", "testdata", "pixellayout", "planar_bigendian.dcm")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolving the fixture path: %v", err)
	}
	return abs
}

// convert must honor --format where the help says to put it.
//
// `convert in.dcm out.csv --format csv` wrote JSON. Go's flag package stops at the
// first positional, so the flag was read as a positional and the format stayed at its
// default. All three formats produced byte-identical files, exit zero, no warning.
func TestConvertHonoursFormatAfterThePositionalArguments(t *testing.T) {
	binary := buildCLI(t)
	in := fixture(t)
	dir := t.TempDir()

	csv := filepath.Join(dir, "out.csv")
	out, err := exec.Command(binary, "convert", in, csv, "--format", "csv").CombinedOutput()
	if err != nil {
		t.Fatalf("convert: %v\n%s", err, out)
	}

	body, err := os.ReadFile(csv)
	if err != nil {
		t.Fatalf("reading the output: %v", err)
	}
	if !strings.HasPrefix(string(body), "Tag,Name,VR") {
		t.Errorf("--format csv did not produce CSV. First 80 bytes:\n%.80s\n\n"+
			"If this starts with '{' the flag was ignored and JSON was written.", body)
	}

	// And the formats must actually differ, so that "it produced a file" is not
	// mistaken for "it produced the right kind of file".
	js := filepath.Join(dir, "out.json")
	if out, err := exec.Command(binary, "convert", in, js, "--format", "json").CombinedOutput(); err != nil {
		t.Fatalf("convert json: %v\n%s", err, out)
	}
	jsBody, err := os.ReadFile(js)
	if err != nil {
		t.Fatalf("reading the json: %v", err)
	}
	if string(jsBody) == string(body) {
		t.Error("--format json and --format csv produced identical bytes")
	}
}

// codify's output must be valid Go, and a package main must have a main function.
//
// It emitted `package main` with a dataset builder and nothing to call it, so the
// default output of a command whose purpose is to produce runnable Go did not build:
//
//	runtime.main_main·f: function main is undeclared in the main package
//
// Parsed here rather than compiled: go/parser proves it is syntactically valid Go and
// lets the declarations be inspected, without needing a module or the network.
func TestCodifyGeneratesCompilableGo(t *testing.T) {
	binary := buildCLI(t)
	in := fixture(t)
	dir := t.TempDir()
	generated := filepath.Join(dir, "generated.go")

	// --output after the positional, which is also the form the help documents.
	if out, err := exec.Command(binary, "codify", in, "--output", generated).CombinedOutput(); err != nil {
		t.Fatalf("codify: %v\n%s", err, out)
	}

	src, err := os.ReadFile(generated)
	if err != nil {
		t.Fatalf("codify wrote no file to --output: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "generated.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("the generated Go does not parse: %v\n\n%s", err, src)
	}

	if file.Name.Name != "main" {
		t.Fatalf("expected package main by default, got %q", file.Name.Name)
	}

	var hasMain bool
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "main" && fn.Recv == nil {
			hasMain = true
		}
	}
	if !hasMain {
		t.Error("the generated package main has no main function, so it cannot be " +
			"built or run — which is the whole point of codify")
	}
}

// tag-doc must document the tag it is given.
//
// The positional argument was read and discarded, so `tag-doc 0010,0010` printed the
// first fifty dictionary entries and exited zero: an answer to a different question,
// indistinguishable from success. Both the README and the command's own help show
// that form.
func TestTagDocDocumentsTheTagItIsGiven(t *testing.T) {
	binary := buildCLI(t)

	for _, form := range []string{"0010,0010", "(0010,0010)", "00100010"} {
		out, err := exec.Command(binary, "tag-doc", form).CombinedOutput()
		if err != nil {
			t.Errorf("tag-doc %s: %v\n%s", form, err, out)
			continue
		}
		text := string(out)
		if !strings.Contains(text, "PatientName") {
			t.Errorf("tag-doc %s did not document PatientName:\n%s", form, text)
		}
		if strings.Contains(text, "Dictionary Summary") || strings.Contains(text, "Dictionary\n=") {
			t.Errorf("tag-doc %s listed the dictionary instead of the tag asked for", form)
		}
	}

	// Junk must be refused rather than silently listing everything.
	out, err := exec.Command(binary, "tag-doc", "not-a-tag").CombinedOutput()
	if err == nil {
		t.Errorf("tag-doc accepted junk as a tag:\n%s", out)
	}
}

// A command that is asked to print something must not also write a file.
//
// tag-doc with no arguments created dicom_tags_text.txt in the working directory as a
// side effect. One of those was committed to this repository by accident.
func TestTagDocWritesNoFileUnlessAsked(t *testing.T) {
	binary := buildCLI(t)
	dir := t.TempDir()

	cmd := exec.Command(binary, "tag-doc")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tag-doc: %v\n%s", err, out)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("tag-doc created %v without being asked to; listing goes to stdout "+
			"unless -output names a file", names)
	}
}

// A file that is not DICOM must be reported as such, not shown as an empty table.
//
// The reader is deliberately tolerant: it salvages what it can from a truncated or
// malformed file, which is right for recovery. But it returns no error when there is
// nothing to recover, so every file command accepted any file at all —
//
//	$ go-dicom show /etc/hosts
//	=== DICOM File: /etc/hosts ===
//	Tag          VR  Length  Value
//	─────────────────────────────
//	$ echo $?
//	0
//
// — and /dev/null did the same without even a warning. A mistyped filename is the
// common case and it looked exactly like success.
func TestTheFileCommandsRejectFilesThatAreNotDicom(t *testing.T) {
	binary := buildCLI(t)

	notDicom := filepath.Join(t.TempDir(), "notdicom.txt")
	if err := os.WriteFile(notDicom, []byte("this is not a DICOM file, not even close\n"), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	empty := filepath.Join(t.TempDir(), "empty.dcm")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("writing the empty fixture: %v", err)
	}

	for _, path := range []string{notDicom, empty} {
		for _, args := range [][]string{
			{"show", path},
			{"info", path},
			{"codify", path},
			{"convert", path, filepath.Join(t.TempDir(), "out.json")},
		} {
			out, err := exec.Command(binary, args...).CombinedOutput()
			if err == nil {
				t.Errorf("`%s %s` exited 0 on a file that is not DICOM:\n%s",
					args[0], filepath.Base(path), out)
				continue
			}
			if !strings.Contains(string(out), "does not look like a DICOM file") {
				t.Errorf("`%s %s` failed but not with a useful reason:\n%s",
					args[0], filepath.Base(path), out)
			}
		}
	}

	// And a real DICOM file must still be accepted, so this is not just refusing
	// everything.
	if out, err := exec.Command(binary, "show", fixture(t)).CombinedOutput(); err != nil {
		t.Errorf("show rejected a real DICOM file: %v\n%s", err, out)
	}
}

// No command may panic, whatever it is given. A panic prints a stack trace at a user
// and exits 2, which is neither a diagnosis nor a clean failure.
func TestNoCommandPanicsOnBadInput(t *testing.T) {
	binary := buildCLI(t)
	dir := t.TempDir()

	junk := filepath.Join(dir, "junk.dcm")
	if err := os.WriteFile(junk, []byte{0xFF, 0x00, 0x13, 0x37, 0xDE, 0xAD}, 0o644); err != nil {
		t.Fatalf("writing junk: %v", err)
	}

	cases := [][]string{
		{"show"}, {"info"}, {"convert"}, {"codify"}, {"tag-doc", "zzzz"},
		{"show", "/nonexistent"}, {"info", "/nonexistent"}, {"codify", "/nonexistent"},
		{"show", junk}, {"info", junk}, {"codify", junk},
		{"convert", junk, filepath.Join(dir, "o.json")},
		{"convert", fixture(t), filepath.Join(dir, "o.zzz"), "--format", "zzz"},
		{"tag-doc", "9999,9999"},
		{"findscu", "-level", "NOSUCHLEVEL", "127.0.0.1:1"},
		{"echoscu", "127.0.0.1:1"},
		{"storescu", "-aec", "X", "127.0.0.1:1", fixture(t)},
	}

	for _, args := range cases {
		out, _ := exec.Command(binary, args...).CombinedOutput()
		text := string(out)
		if strings.Contains(text, "panic:") || strings.Contains(text, "goroutine 1 [running]") {
			t.Errorf("`%s` panicked:\n%s", strings.Join(args, " "), text)
		}
	}
}
