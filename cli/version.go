package cli

import (
	"flag"
	"fmt"
)

// VersionCommand displays version information.
type VersionCommand struct {
	AppName    string
	AppVersion string
}

// Name returns the command name.
func (vc *VersionCommand) Name() string {
	return "version"
}

// Description returns the command description.
func (vc *VersionCommand) Description() string {
	return "Display version information"
}

// AddFlags adds command flags.
func (vc *VersionCommand) AddFlags(fs *flag.FlagSet) {
}

// Execute runs the version command.
func (vc *VersionCommand) Execute(args []string) error {
	PrintVersion(vc.AppName, vc.AppVersion)
	return nil
}

// PrintVersion prints version information to stdout.
func PrintVersion(name, version string) {
	fmt.Printf("%s version %s\n", name, version)
}

// PrintUsage prints usage information to stdout.
func PrintUsage(name string) {
	fmt.Printf("Usage: %s [command] [options]\n\n", name)
	fmt.Println("Commands:")
	fmt.Println("  show     Display DICOM file contents")
	fmt.Println("  info     Show file metadata and information")
	fmt.Println("  convert  Convert DICOM to other formats")
	fmt.Println("\nOptions:")
	fmt.Println("  -v, --version  Show version")
	fmt.Println("  -h, --help     Show help")
	fmt.Println("\nExamples:")
	fmt.Println("  dicom show patient.dcm")
	fmt.Println("  dicom show -exclude-private patient.dcm")
	fmt.Println("  dicom info -verbose patient.dcm")
	fmt.Println("  dicom convert --format=json patient.dcm output.json")
}

// PrintHelp prints detailed help information to stdout.
func PrintHelp() {
	fmt.Println("PyDICOM Go - DICOM File Manipulation Tool")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  dicom [command] [options] [arguments]")
	fmt.Println()
	fmt.Println("COMMANDS:")
	fmt.Println("  show SPEC              Display DICOM file contents")
	fmt.Println("    Options:")
	fmt.Println("      -exclude-private   Hide private tags")
	fmt.Println("      -top               Only show top-level elements")
	fmt.Println("      -quiet             Minimal output")
	fmt.Println()
	fmt.Println("  info FILE              Show file metadata and information")
	fmt.Println("    Options:")
	fmt.Println("      -verbose           Show detailed information")
	fmt.Println("      -stats             Show file statistics")
	fmt.Println()
	fmt.Println("  convert INPUT OUTPUT   Convert DICOM to other formats")
	fmt.Println("    Options:")
	fmt.Println("      -format=FORMAT     Output format (json, csv, nifti)")
	fmt.Println("      -compress          Apply compression")
	fmt.Println()
	fmt.Println("FILE SPECIFICATION:")
	fmt.Println("  file.dcm               Entire file")
	fmt.Println("  file.dcm::ElementName  Specific element")
	fmt.Println("  file.dcm::0010,0010    Specific tag")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  dicom show patient.dcm")
	fmt.Println("  dicom show patient.dcm::PatientName")
	fmt.Println("  dicom show -exclude-private patient.dcm")
	fmt.Println("  dicom info patient.dcm")
	fmt.Println("  dicom info -verbose -stats patient.dcm")
	fmt.Println("  dicom convert patient.dcm output.json")
	fmt.Println("  dicom convert --format=csv patient.dcm output.csv")
}
