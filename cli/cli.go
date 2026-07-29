package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Command interface all CLI commands must implement.
type Command interface {
	Name() string
	Description() string
	AddFlags(*flag.FlagSet)
	Execute([]string) error
}

// CLI represents the command-line interface.
type CLI struct {
	Name     string
	Version  string
	Commands map[string]Command
}

// FileSpec represents a parsed file specification (file.dcm or file.dcm::ElementName).
type FileSpec struct {
	FilePath string
	Element  string
	Index    int
}

// NewCLI creates a new CLI instance.
func NewCLI(name, version string) *CLI {
	return &CLI{
		Name:     name,
		Version:  version,
		Commands: make(map[string]Command),
	}
}

// RegisterCommand adds a command to the CLI.
func (c *CLI) RegisterCommand(cmd Command) {
	c.Commands[cmd.Name()] = cmd
}

// Run executes a command based on arguments.
func (c *CLI) Run(args []string) error {
	// Handle global flags first, before trying to parse commands
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			c.ShowHelp()
			return nil
		case "-v", "--version":
			PrintVersion(c.Name, c.Version)
			return nil
		}
	}

	// If no arguments provided, show help instead of error
	if len(args) == 0 {
		c.ShowHelp()
		return nil
	}

	cmdName := args[0]
	cmd, exists := c.Commands[cmdName]
	if !exists {
		return fmt.Errorf("unknown command: %s\nRun '%s --help' for usage", cmdName, c.Name)
	}

	return cmd.Execute(args[1:])
}

// ShowHelp displays the main help message for the CLI.
func (c *CLI) ShowHelp() {
	fmt.Printf("Go DICOM Command Line Interface\n\n")
	fmt.Printf("USAGE:\n")
	fmt.Printf("  %s <command> [options] [arguments]\n\n", c.Name)
	fmt.Printf("COMMANDS:\n")

	// Display commands in a specific order
	fmt.Printf("  FILE COMMANDS:\n")
	fileCommands := []string{"show", "info", "convert", "tag-doc", "codify"}
	fileDescriptions := map[string]string{
		"show":    "Display DICOM file contents",
		"info":    "Show DICOM file metadata and information",
		"convert": "Convert DICOM files to other formats (JSON, CSV, NIfTI)",
		"tag-doc": "Generate documentation for DICOM tags",
		"codify":  "Read a DICOM file and produce Go code to create it",
	}
	for _, cmdName := range fileCommands {
		if desc, ok := fileDescriptions[cmdName]; ok {
			fmt.Printf("    %-12s %s\n", cmdName, desc)
		}
	}

	fmt.Printf("\n  NETWORK COMMANDS:\n")
	netCommands := []string{"echoscu", "echoscp", "storescu", "storescp", "findscu", "movescu", "getscu", "commitscu", "qrscp"}
	netDescriptions := map[string]string{
		"echoscu":   "DICOM Echo SCU — verification (ping)",
		"echoscp":   "DICOM Echo SCP — verification server",
		"storescu":  "DICOM Store SCU — send DICOM files (.dcm, .ima, etc.)",
		"storescp":  "DICOM Store SCP — receive and save DICOM files",
		"findscu":   "DICOM Find SCU — query for patients/studies/series",
		"movescu":   "DICOM Move SCU — retrieve studies to a destination",
		"getscu":    "DICOM Get SCU — retrieve on same association",
		"commitscu": "DICOM Storage Commitment SCU — ask a peer to take responsibility",
		"qrscp":     "DICOM Q/R SCP — combined store + query/retrieve server",
	}
	for _, cmdName := range netCommands {
		if desc, ok := netDescriptions[cmdName]; ok {
			fmt.Printf("    %-12s %s\n", cmdName, desc)
		}
	}

	fmt.Printf("\n  OTHER:\n")
	fmt.Printf("    %-12s %s\n", "help", "Display help for commands")
	fmt.Printf("    %-12s %s\n", "version", "Display version information")

	fmt.Printf("\nGLOBAL OPTIONS:\n")
	fmt.Printf("  -h, --help      Show help message\n")
	fmt.Printf("  -v, --version   Show version information\n")
	fmt.Printf("\nEXAMPLES:\n")
	fmt.Printf("  %s show patient.dcm\n", c.Name)
	fmt.Printf("  %s echoscu pacs.hospital.com:11112\n", c.Name)
	fmt.Printf("  %s storescu -aec PACS pacs:11112 study/*.dcm\n", c.Name)
	fmt.Printf("  %s storescp -port 11112 -output ./received/\n", c.Name)
	fmt.Printf("  %s findscu -patient-name \"Smith*\" pacs:11112\n", c.Name)
	fmt.Printf("  %s movescu -dest MY_SCP -study 1.2.3.4 pacs:11112\n", c.Name)
	fmt.Printf("\nUse '%s help <command>' for more information on a specific command.\n", c.Name)
}

// ParseFileSpec parses a file specification string (file.dcm or file.dcm::ElementName).
func ParseFileSpec(spec string) (*FileSpec, error) {
	fs := &FileSpec{
		Index: -1,
	}

	parts := strings.Split(spec, "::")
	fs.FilePath = parts[0]

	if len(parts) > 1 {
		fs.Element = parts[1]
	}

	if _, err := os.Stat(fs.FilePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", fs.FilePath)
	}

	return fs, nil
}
