package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/validate"
)

// ValidateCommand checks a file against the IOD of its SOP class.
type ValidateCommand struct {
	json bool
	help bool
}

// Name returns the command name.
func (vc *ValidateCommand) Name() string { return "validate" }

// Description returns the command description.
func (vc *ValidateCommand) Description() string {
	return "Check a file against what its SOP class requires (PS3.3)"
}

// AddFlags adds command flags to the flag set.
func (vc *ValidateCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&vc.help, "h", false, "Show help for this command")
	fs.BoolVar(&vc.help, "help", false, "Show help for this command")
	fs.BoolVar(&vc.json, "json", false, "Print the findings as JSON")
}

// Exit statuses of validate, by the worst finding. 1 is any command that
// could not run.
const (
	validateWarnings = 2
	validateErrors   = 3
)

// ExitStatusError is an error that asks for a particular exit status, for a
// command whose status says more than success or failure.
type ExitStatusError struct {
	Status  int
	Message string
}

// Error returns the message.
func (e *ExitStatusError) Error() string { return e.Message }

// ExitCode is the status the process exits with.
func (e *ExitStatusError) ExitCode() int { return e.Status }

// Execute runs the validate command.
func (vc *ValidateCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	vc.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("validate: failed to parse flags: %v", err)
	}
	if vc.help {
		vc.showHelp()
		return nil
	}
	if len(positional) != 1 {
		return fmt.Errorf("validate: expected one file\nRun 'go-dicom validate -h' for usage")
	}
	path := positional[0]

	report, err := validateFile(path)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", path, err)
	}
	report.Sort()

	if vc.json {
		out := struct {
			File string `json:"file"`
			*validate.Report
			Counts map[string]int `json:"counts"`
		}{path, report, map[string]int{
			"error":       report.Count(validate.Error),
			"warning":     report.Count(validate.Warning),
			"not checked": report.Count(validate.NotChecked),
		}}
		if out.Findings == nil {
			out.Findings = []validate.Finding{}
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("validate: %w", err)
		}
		fmt.Println(string(data))
	} else {
		printReport(path, report)
	}

	errors, warnings := report.Count(validate.Error), report.Count(validate.Warning)
	switch {
	case errors > 0:
		return &ExitStatusError{validateErrors, fmt.Sprintf("validate: %s: %d error(s), %d warning(s)", path, errors, warnings)}
	case warnings > 0:
		return &ExitStatusError{validateWarnings, fmt.Sprintf("validate: %s: %d warning(s)", path, warnings)}
	}
	return nil
}

// validateFile reads a file and checks it. The IOD is the data set's SOP
// Class UID's, or the file meta's when the data set has none, which is then
// reported as missing like any other Type 1 attribute.
func validateFile(path string) (*validate.Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, err
	}
	ds := df.GetDataset()
	if datasetText(ds, tag.New(0x0008, 0x0016)) == "" && df.FileMetaInfo != nil && df.FileMetaInfo.MediaStorageSOPClassUID != "" {
		return validate.DatasetAs(ds, df.FileMetaInfo.MediaStorageSOPClassUID)
	}
	return validate.Dataset(ds)
}

// printReport prints a report as text: what it was checked as, each finding
// on a line, worst first, and the counts.
func printReport(path string, r *validate.Report) {
	fmt.Printf("%s: %s, checked as the %s IOD (PS3.3 %s, %s)\n", path, r.SOPClassUID, r.IOD, r.IODSection, r.Edition)
	for _, f := range r.Findings {
		where := f.Module
		if f.Path != "" {
			where = f.Path + " " + f.Name
		}
		fmt.Printf("  %-11s %s: %s: %s [%s; %s]\n", f.Severity, where, f.Rule, f.Message, f.Module, f.Reference)
	}
	fmt.Printf("%d error(s), %d warning(s), %d not checked\n",
		r.Count(validate.Error), r.Count(validate.Warning), r.Count(validate.NotChecked))
}

func (vc *ValidateCommand) showHelp() {
	printHelp(`Check a file against what its SOP class requires

USAGE:
  go-dicom validate [--json] <file>

  The file's SOP Class UID names its IOD (PS3.4 Table B.5-1), and the IOD
  its modules (PS3.3 Annex A). Each module that is Mandatory, or that is
  present, is checked attribute by attribute against its table (PS3.3 Annex
  C), into sequences:

    Type 1   present, with a value          Type 2   present
    Value Multiplicity, from the dictionary (PS3.6)
    Enumerated Values (an error) and Defined Terms (a warning) where the
      table, or the section it names for the attribute, lists them
    how many items a sequence holds, where its description says

  The rules are generated from the standard (the edition is printed), not
  written by hand.

NOT CHECKED:
  What PS3.3 states in prose is reported as "not checked", never passed:
  the condition of a Conditional module, and of a Type 1C or 2C attribute
  that is absent. Also not checked: attributes of repeating groups such as
  (60xx,eeee), the Functional Group Macros of the enhanced IODs, context
  groups (PS3.16), rules that tie one attribute to another, and attributes
  the IOD does not name.

OPTIONS:
  --json       Print the findings as JSON
  -h, -help    Show this help message

EXIT STATUS:
  0  no error or warning     2  warnings, no error
  1  the file could not be checked     3  errors

EXAMPLES:
  go-dicom validate ct.dcm
  go-dicom validate --json ct.dcm
`)
}
