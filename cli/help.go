package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"
)

// HelpCommand displays help for available commands.
type HelpCommand struct {
	subcommand  string
	allCommands map[string]Command

	// showMainHelp renders the CLI's own top-level help. Set by RegisterCommand, so
	// that `help` with no argument and the bare binary print the same thing.
	showMainHelp func()

	// programName is what the binary was invoked as. The help text is written with
	// "go-dicom" in it and this replaces it at print time.
	programName string
}

// SetProgramName tells the help text what this binary is called.
//
// The same source ships under two names — `go install` builds it as go-dicom, after
// the module path, while the Makefile and the release assets call it dicom — so help
// that hardcodes either one tells half of its users to run a command they do not
// have. RegisterCommand sets this from the CLI's own name, which main takes from argv.
func (hc *HelpCommand) SetProgramName(name string) {
	hc.programName = name
}

// prog returns the name to print, falling back to the module's own for a HelpCommand
// used without a CLI attached.
func (hc *HelpCommand) prog() string {
	if hc.programName == "" {
		return "go-dicom"
	}
	return hc.programName
}

// print writes help text with the program name substituted in.
//
// A replace over the finished string rather than a format verb per occurrence: there
// are dozens across these literals, and every one of them is the same substitution.
// Safe here because no literal in this file contains the module path, where
// "go-dicom" must survive untouched.
func (hc *HelpCommand) print(text string) {
	fmt.Print(strings.ReplaceAll(text, "go-dicom", hc.prog()))
}

func (hc *HelpCommand) println(text string) {
	fmt.Println(strings.ReplaceAll(text, "go-dicom", hc.prog()))
}

// NewHelpCommand creates a new help command.
func NewHelpCommand() *HelpCommand {
	return &HelpCommand{
		allCommands: make(map[string]Command),
	}
}

// Name returns the command name.
func (hc *HelpCommand) Name() string {
	return "help"
}

// Description returns the command description.
func (hc *HelpCommand) Description() string {
	return "Display help for commands"
}

// AddFlags adds command flags to the flag set.
func (hc *HelpCommand) AddFlags(fs *flag.FlagSet) {
}

// Execute runs the help command.
func (hc *HelpCommand) Execute(args []string) error {
	fs := flag.NewFlagSet(hc.Name(), flag.ContinueOnError)
	hc.AddFlags(fs)

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("help: failed to parse flags: %v", err)
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		hc.subcommand = remaining[0]
	}

	if hc.subcommand != "" {
		return hc.displayCommandHelp(hc.subcommand)
	}

	return hc.displayMainHelp()
}

// SetCommands sets the available commands for help display.
func (hc *HelpCommand) SetCommands(commands map[string]Command) {
	hc.allCommands = commands
}

// SetMainHelp supplies the CLI's own top-level help renderer, so that
// `go-dicom help` prints exactly what `go-dicom` prints.
func (hc *HelpCommand) SetMainHelp(show func()) {
	hc.showMainHelp = show
}

// displayMainHelp displays main help message
func (hc *HelpCommand) displayMainHelp() error {
	// Delegated to the CLI's own help, so that `go-dicom help` and `go-dicom` with no
	// arguments cannot disagree.
	//
	// They did. This function kept a third copy of the command list — seven file
	// commands with their own descriptions — while the CLI's own listing had all
	// sixteen grouped by category. So `go-dicom help` simply did not mention any of
	// the nine network commands, which are most of what the tool is for.
	//
	// Found by rendering `dicom help` as a cover image and counting the commands on
	// it. The audit that caught the same bug in `help <command>` had checked each
	// command individually and never compared this listing against the real one.
	if hc.showMainHelp != nil {
		hc.showMainHelp()
		return nil
	}

	// Standalone, with no CLI attached: still derived from the registry rather than
	// restated, so it can be incomplete but not wrong.
	hc.println(`Go DICOM Command Line Interface

USAGE:
  go-dicom <command> [options] [arguments]

COMMANDS:`)

	names := make([]string, 0, len(hc.allCommands))
	for name := range hc.allCommands {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Printf("  %-12s %s\n", name, hc.allCommands[name].Description())
	}

	hc.print(`
OPTIONS:
  -h, --help      Show help message
  -v, --version   Show version information

Use 'go-dicom help <command>' for more information on a specific command.
`)

	return nil
}

// displayCommandHelp displays help for a specific command
func (hc *HelpCommand) displayCommandHelp(cmdName string) error {
	switch cmdName {
	case "version":
		return hc.helpVersion()
	case "help":
		return hc.helpHelp()
	default:
		// Delegated to the command's own -h rather than hand-written here.
		//
		// This switch listed only the file commands, so `help storescu` reported
		// that storescu did not exist — while `storescu -h` printed a full page of
		// help and the top-level command list advertised it. Nine of the sixteen
		// commands were in that state: every network one.
		//
		// Writing nine more cases would have fixed the symptom and left the cause,
		// which is that a command's help lives in two places and only one of them
		// is exercised when the command is used.
		if cmd, ok := hc.allCommands[cmdName]; ok {
			return cmd.Execute([]string{"-h"})
		}
		return fmt.Errorf("help: unknown command '%s'", cmdName)
	}
}

func (hc *HelpCommand) helpVersion() error {
	hc.print(`COMMAND: version - Display version information

USAGE:
  go-dicom version

DESCRIPTION:
  Display the version of the Go DICOM command-line tool.

EXAMPLES:
  go-dicom version
`)
	return nil
}

func (hc *HelpCommand) helpHelp() error {
	hc.print(`COMMAND: help - Display help for commands

USAGE:
  go-dicom help [command]

DESCRIPTION:
  Display help information. If a command is specified, show help for that
  specific command. If no command is specified, show general help.

EXAMPLES:
  go-dicom help
  go-dicom help show
  go-dicom help info
  go-dicom help codify
`)
	return nil
}
