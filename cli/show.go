package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// ShowCommand displays DICOM file contents.
type ShowCommand struct {
	filespec       string
	element        string
	excludePrivate bool
	topLevelOnly   bool
	quiet          bool
	help           bool
}

// NewShowCommand creates a new show command.
func NewShowCommand() *ShowCommand {
	return &ShowCommand{}
}

// Name returns the command name.
func (sc *ShowCommand) Name() string {
	return "show"
}

// Description returns the command description.
func (sc *ShowCommand) Description() string {
	return "Display DICOM file contents"
}

// AddFlags adds command flags to the flag set.
func (sc *ShowCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&sc.help, "h", false, "Show help for this command")
	fs.BoolVar(&sc.help, "help", false, "Show help for this command")
	fs.BoolVar(&sc.excludePrivate, "exclude-private", false, "Exclude private tags")
	fs.BoolVar(&sc.topLevelOnly, "top", false, "Only show top-level elements")
	fs.BoolVar(&sc.quiet, "quiet", false, "Minimal output")
}

// Execute runs the show command.
func (sc *ShowCommand) Execute(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	sc.AddFlags(fs)

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("show: failed to parse flags: %v", err)
	}

	// Check if help flag was set
	if sc.help {
		sc.showHelp()
		return nil
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("show: missing file specification\nRun 'go-dicom show -h' for usage")
	}

	fileSpec := remaining[0]
	parts := strings.Split(fileSpec, "::")
	sc.filespec = parts[0]
	if len(parts) > 1 {
		sc.element = parts[1]
	}

	if _, err := os.Stat(sc.filespec); os.IsNotExist(err) {
		return fmt.Errorf("show: file not found: %s", sc.filespec)
	}

	elements, err := readDICOMFile(sc.filespec)
	if err != nil {
		return fmt.Errorf("show: failed to read DICOM file: %v", err)
	}

	if sc.element != "" {
		for _, elem := range elements {
			if strings.EqualFold(strings.ToLower(elem.Name), strings.ToLower(sc.element)) ||
				strings.EqualFold(strings.ToLower(elem.Tag), strings.ToLower(sc.element)) {
				sc.displayElement(elem)
				return nil
			}
		}
		return fmt.Errorf("show: element '%s' not found", sc.element)
	}

	if !sc.quiet {
		fmt.Printf("=== DICOM File: %s ===\n", sc.filespec)
		fmt.Println("Tag          VR  Length  Value")
		fmt.Println("─────────────────────────────────────────────")
	}

	for _, elem := range elements {
		if sc.excludePrivate && isPrivateTag(elem.Tag) {
			continue
		}

		if sc.topLevelOnly && elem.Depth > 0 {
			continue
		}

		if sc.quiet {
			fmt.Printf("%s: %s\n", elem.Tag, formatValue(elem.VR, elem.Value, 50))
		} else {
			sc.displayElement(elem)
		}
	}

	return nil
}

// displayElement displays a single element in formatted output.
func (sc *ShowCommand) displayElement(elem DicomElement) {
	value := formatValue(elem.VR, elem.Value, 100)
	fmt.Printf("%-12s %-3s %-7d %s\n", elem.Tag, elem.VR, len(elem.Value), value)
}

// showHelp displays help information for the show command.
func (sc *ShowCommand) showHelp() {
	fmt.Print(`COMMAND: show - Display DICOM file contents

USAGE:
  go-dicom show <file> [options]

DESCRIPTION:
  Display all tags and values from a DICOM file.

OPTIONS:
  -h, --help           Show help for this command
  --exclude-private    Exclude private tags
  --top                Show only top-level elements
  --quiet              Show minimal output

EXAMPLES:
  go-dicom show patient.dcm
  go-dicom show patient.dcm --exclude-private
  go-dicom show patient.dcm --top --quiet
  go-dicom show -h
`)
}
