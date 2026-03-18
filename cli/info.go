package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// InfoCommand displays DICOM file metadata and information.
type InfoCommand struct {
	filespec   string
	verbose    bool
	statistics bool
	help       bool
}

// NewInfoCommand creates a new info command.
func NewInfoCommand() *InfoCommand {
	return &InfoCommand{}
}

// Name returns the command name.
func (ic *InfoCommand) Name() string {
	return "info"
}

// Description returns the command description.
func (ic *InfoCommand) Description() string {
	return "Show DICOM file metadata and information"
}

// AddFlags adds command flags to the flag set.
func (ic *InfoCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&ic.help, "h", false, "Show help for this command")
	fs.BoolVar(&ic.help, "help", false, "Show help for this command")
	fs.BoolVar(&ic.verbose, "verbose", false, "Show detailed information")
	fs.BoolVar(&ic.statistics, "stats", false, "Show file statistics")
}

// Execute runs the info command.
func (ic *InfoCommand) Execute(args []string) error {
	fs := flag.NewFlagSet(ic.Name(), flag.ContinueOnError)
	ic.AddFlags(fs)

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("info: failed to parse flags: %v", err)
	}

	// Check if help flag was set
	if ic.help {
		ic.showHelp()
		return nil
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("info: missing file specification\nRun 'go-dicom info -h' for usage")
	}

	ic.filespec = remaining[0]

	fileInfo, err := os.Stat(ic.filespec)
	if err != nil {
		return fmt.Errorf("info: file not found: %s", ic.filespec)
	}

	elements, err := readDICOMFile(ic.filespec)
	if err != nil {
		return fmt.Errorf("info: failed to read DICOM file: %v", err)
	}

	fmt.Println("=== DICOM File Information ===")
	fmt.Printf("File:         %s\n", ic.filespec)
	fmt.Printf("Size:         %d bytes\n", fileInfo.Size())
	fmt.Printf("Modified:     %s\n", fileInfo.ModTime().Format("2006-01-02 15:04:05"))

	if ic.verbose {
		absPath, _ := filepath.Abs(ic.filespec)
		fmt.Printf("Absolute path: %s\n", absPath)
		fmt.Printf("Permissions:   %v\n", fileInfo.Mode())
	}

	fmt.Printf("\nKey DICOM Elements:\n")
	fmt.Println("─────────────────────────────────────")

	dicomInfo := extractDICOMInfo(elements)
	if dicomInfo.PatientName != "" {
		fmt.Printf("Patient Name:    %s\n", dicomInfo.PatientName)
	}
	if dicomInfo.PatientID != "" {
		fmt.Printf("Patient ID:      %s\n", dicomInfo.PatientID)
	}
	if dicomInfo.StudyDate != "" {
		fmt.Printf("Study Date:      %s\n", dicomInfo.StudyDate)
	}
	if dicomInfo.Modality != "" {
		fmt.Printf("Modality:        %s\n", dicomInfo.Modality)
	}
	if dicomInfo.StudyDescription != "" {
		fmt.Printf("Study Desc:      %s\n", dicomInfo.StudyDescription)
	}
	if dicomInfo.SeriesDescription != "" {
		fmt.Printf("Series Desc:     %s\n", dicomInfo.SeriesDescription)
	}

	if dicomInfo.Rows > 0 || dicomInfo.Columns > 0 {
		fmt.Printf("\nImage Dimensions:\n")
		fmt.Println("─────────────────────────────────────")
		if dicomInfo.Rows > 0 {
			fmt.Printf("Rows:            %d\n", dicomInfo.Rows)
		}
		if dicomInfo.Columns > 0 {
			fmt.Printf("Columns:         %d\n", dicomInfo.Columns)
		}
		if dicomInfo.NumberOfFrames > 0 {
			fmt.Printf("Frames:          %d\n", dicomInfo.NumberOfFrames)
		}
	}

	if ic.statistics {
		ic.displayStatistics(elements)
	}

	if ic.verbose {
		ic.displayVerboseInfo(elements)
		ic.displayDictionaryInfo(elements)
	}

	return nil
}

// displayStatistics shows file statistics.
func (ic *InfoCommand) displayStatistics(elements []DicomElement) {
	fmt.Printf("\nFile Statistics:\n")
	fmt.Println("─────────────────────────────────────")
	fmt.Printf("Total Elements:  %d\n", len(elements))

	privateCount := 0
	totalDataSize := int64(0)

	for _, elem := range elements {
		if isPrivateTag(elem.Tag) {
			privateCount++
		}
		totalDataSize += int64(len(elem.Value))
	}

	fmt.Printf("Private Tags:    %d\n", privateCount)
	fmt.Printf("Total Data Size: %d bytes\n", totalDataSize)

	if len(elements) > 0 {
		avgSize := totalDataSize / int64(len(elements))
		fmt.Printf("Avg Element Size:%d bytes\n", avgSize)
	}
}

// displayVerboseInfo shows detailed element information.
func (ic *InfoCommand) displayVerboseInfo(elements []DicomElement) {
	fmt.Printf("\nDetailed Elements (first 20):\n")
	fmt.Println("─────────────────────────────────────")

	count := 0
	for _, elem := range elements {
		if count >= 20 {
			break
		}
		fmt.Printf("%-12s %-20s %-3s %d bytes\n",
			elem.Tag, elem.Name, elem.VR, len(elem.Value))
		count++
	}

	if len(elements) > 20 {
		fmt.Printf("... and %d more elements\n", len(elements)-20)
	}
}

// displayDictionaryInfo shows DICOM dictionary metadata for elements.
func (ic *InfoCommand) displayDictionaryInfo(elements []DicomElement) {
	fmt.Printf("\nDICOM Dictionary Metadata (first 10):\n")
	fmt.Println("─────────────────────────────────────")

	count := 0
	for _, elem := range elements {
		if count >= 10 {
			break
		}

		tagInfo, err := GetDICOMTagInfo(elem.Tag)
		if err != nil {
			continue
		}

		fmt.Printf("\nTag: %s\n", tagInfo["Tag"])
		if name, ok := tagInfo["Name"]; ok {
			fmt.Printf("  Name:    %s\n", name)
		}
		if vr, ok := tagInfo["VR"]; ok && vr != "" {
			fmt.Printf("  VR:      %s\n", vr)
		}
		if vm, ok := tagInfo["VM"]; ok && vm != "" {
			fmt.Printf("  VM:      %s\n", vm)
		}
		if keyword, ok := tagInfo["Keyword"]; ok && keyword != "" {
			fmt.Printf("  Keyword: %s\n", keyword)
		}
		if status, ok := tagInfo["Status"]; ok {
			fmt.Printf("  Status:  %s\n", status)
		}

		count++
	}

	if len(elements) > 10 {
		fmt.Printf("\n... and %d more elements\n", len(elements)-10)
	}
}

// showHelp displays help information for the info command.
func (ic *InfoCommand) showHelp() {
	fmt.Print(`COMMAND: info - Show DICOM file metadata

USAGE:
  go-dicom info <file> [options]

DESCRIPTION:
  Display key DICOM metadata including patient information, study details,
  and image dimensions. Integrates with DICOM dictionary for tag information.

OPTIONS:
  -h, --help    Show help for this command
  --verbose     Show detailed information and tag metadata
  --stats       Show file statistics

EXAMPLES:
  go-dicom info patient.dcm
  go-dicom info patient.dcm --verbose
  go-dicom info patient.dcm --stats
  go-dicom info -h
`)
}
