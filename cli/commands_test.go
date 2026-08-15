package cli

import (
	"errors"
	"flag"
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
