// Package cli provides a command-line interface framework for DICOM tools.
//
// This package implements a modular CLI system with command registration,
// flag parsing, and file specification handling for DICOM file operations.
//
// # Core Components
//
// The CLI framework consists of:
//   - CLI: Main command-line interface manager
//   - Command: Interface for implementing CLI commands
//   - FileSpec: Parsed file specification with optional element selector
//
// # Built-in Commands
//
// The following commands are available:
//   - show: Display DICOM file contents
//   - info: Get file information and statistics
//   - convert: Convert DICOM files to different formats
//   - help: Display help information
//   - version: Display version information
//   - tagdoc: Look up DICOM tag documentation
//   - codify: Generate code from DICOM templates
//
// # Quick Start
//
// Creating and using a CLI:
//
//	cli := NewCLI("dicom", "1.0.0")
//	cli.RegisterCommand(NewShowCommand())
//	cli.RegisterCommand(NewInfoCommand())
//	cli.RegisterCommand(NewConvertCommand())
//	err := cli.Run(os.Args[1:])
//
// File Specifications:
//
// File specs support optional element selection using :: delimiter:
//
//	patient.dcm              // Entire file
//	patient.dcm::PatientName // Specific element
//	study.dcm::0008,1030     // Tag notation
//
// # Implementing Custom Commands
//
// Implement the Command interface:
//
//	type MyCommand struct{}
//
//	func (c *MyCommand) Name() string { return "mycommand" }
//	func (c *MyCommand) Description() string { return "My custom command" }
//	func (c *MyCommand) AddFlags(fs *flag.FlagSet) { /* add flags */ }
//	func (c *MyCommand) Execute(args []string) error { /* execute logic */ }
//
// # Flag Management
//
// Commands can define custom flags using the standard flag package:
//
//	func (c *MyCommand) AddFlags(fs *flag.FlagSet) {
//	    fs.BoolVar(&c.verbose, "v", false, "verbose output")
//	    fs.StringVar(&c.output, "o", "", "output file")
//	}
//
// # Error Handling
//
// Commands should return errors for:
//   - Missing required arguments
//   - Invalid file paths
//   - Unsupported file formats
//   - I/O failures
//
// # Thread Safety
//
// The CLI struct and Command implementations should be thread-safe
// as they may be called from multiple goroutines.
//
// # References
//
//   - DICOM Standard: https://www.dicomstandard.org/
//   - Go Flag Package: https://golang.org/pkg/flag/
package cli
