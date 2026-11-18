package cli

import (
	"flag"
	"fmt"
	"io"
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
	json           bool
	full           bool
	help           bool
	// out is where --json writes; nil means standard output.
	out io.Writer
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
	fs.BoolVar(&sc.json, "json", false, "Print every element as JSON with its byte offsets in the file")
	fs.BoolVar(&sc.full, "full", false, "Print whole values in the table, where it prints the first 100 bytes")
}

// Execute runs the show command.
func (sc *ShowCommand) Execute(args []string) error {
	// Parse flags
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	sc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("show: failed to parse flags: %v", err)
	}

	// Check if help flag was set
	if sc.help {
		sc.showHelp()
		return nil
	}

	remaining := positional
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

	if sc.json {
		if sc.element != "" {
			return fmt.Errorf("show: --json describes the whole file; leave out ::%s", sc.element)
		}
		if err := writeShowJSON(orStdout(sc.out), sc.filespec); err != nil {
			return fmt.Errorf("show: failed to read DICOM file: %v", err)
		}
		return nil
	}

	// With the group length that opens the file meta group: show lists what
	// is in the file, as --json does.
	//
	// One element asked for by name is asked for whole. The table is read in
	// a terminal and prints the start of a long value unless told --full.
	whole := sc.full || sc.element != ""
	elements, err := readDICOMElements(sc.filespec, listing{skipBulk: true, metaGroupLength: true, whole: whole})
	if err != nil {
		return fmt.Errorf("show: failed to read DICOM file: %v", err)
	}
	out := orStdout(sc.out)
	limit, quietLimit := shownBytes, 50
	if whole {
		limit, quietLimit = 0, 0
	}

	if sc.element != "" {
		for _, elem := range elements {
			if strings.EqualFold(strings.ToLower(elem.Name), strings.ToLower(sc.element)) ||
				strings.EqualFold(strings.ToLower(elem.Tag), strings.ToLower(sc.element)) {
				displayElement(out, elem, limit)
				return nil
			}
		}
		return fmt.Errorf("show: element '%s' not found", sc.element)
	}

	if !sc.quiet {
		fmt.Fprintf(out, "=== DICOM File: %s ===\n", sc.filespec)
		fmt.Fprintln(out, "Tag          VR  Length  Value")
		fmt.Fprintln(out, "─────────────────────────────────────────────")
	}

	for _, elem := range elements {
		if sc.excludePrivate && isPrivateTag(elem.Tag) {
			continue
		}

		if sc.topLevelOnly && elem.Depth > 0 {
			continue
		}

		if sc.quiet {
			fmt.Fprintf(out, "%s: %s\n", elem.Tag, elem.display(quietLimit))
		} else {
			displayElement(out, elem, limit)
		}
	}

	return nil
}

// displayElement prints one row of the table: the value up to limit bytes,
// or whole when limit is 0.
func displayElement(out io.Writer, elem DicomElement, limit int) {
	value := elem.display(limit)

	// Indent by nesting level so elements inside a sequence item are
	// distinguishable from those at the top of the data set.
	indent := strings.Repeat("  ", elem.Depth)
	fmt.Fprintf(out, "%-12s %-3s %-7d %s%s\n", elem.Tag, elem.VR, elem.Length, indent, value)
}

// showHelp displays help information for the show command.
func (sc *ShowCommand) showHelp() {
	printHelp(`COMMAND: show - Display DICOM file contents

USAGE:
  go-dicom show <file> [options]

DESCRIPTION:
  Display all tags and values from a DICOM file.

OPTIONS:
  -h, --help           Show help for this command
  --exclude-private    Exclude private tags
  --top                Show only top-level elements
  --quiet              Show minimal output
  --full               Print whole values. The table prints the first 100
                       bytes of a value and how long it is; asking for one
                       element (file::Element) always prints it whole.
                       Bulk binary such as Pixel Data is a count of bytes
                       either way.
  --json               Describe the file as JSON: every element with its
                       value and where its tag, VR, length and value sit in
                       the file, nested items and delimiters included, and
                       the preamble, prefix, file meta and data set regions.
                       Offsets count from the start of the file, except in a
                       deflated data set, where they count from the start of
                       the inflated one; "offsetsRelativeTo" says which.

EXAMPLES:
  go-dicom show patient.dcm
  go-dicom show patient.dcm --exclude-private
  go-dicom show patient.dcm --top --quiet
  go-dicom show patient.dcm --json
  go-dicom show patient.dcm::ImageComments     One element, whole
  go-dicom show patient.dcm --full
  go-dicom show -h
`)
}
