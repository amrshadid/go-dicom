package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amrshadid/go-dicom/cli"
)

// Version of the CLI application.
//
// Declared as a var, not a const, so release builds can stamp the real tag in
// with -ldflags "-X main.Version=...". The linker cannot rewrite a const, so
// making this a const silently turns that injection into a no-op.
var Version = "1.5.0"

// programName is what this binary was invoked as.
//
// It cannot be a constant. The same source ships under two names: `go install` builds
// it as go-dicom, after the last element of the module path, while the Makefile and
// the release assets call it dicom. Hardcoding either one meant the help told half of
// its users to run a command they do not have —
//
//	$ dicom
//	USAGE:
//	  go-dicom <command> [options] [arguments]
//	$ go-dicom
//	zsh: command not found: go-dicom
//
// Taking it from argv also covers a binary the user renamed themselves.
func programName() string {
	name := filepath.Base(os.Args[0])

	// Windows invokes it with the extension, which is not how anyone types it.
	name = strings.TrimSuffix(name, ".exe")

	// os.Args[0] is not guaranteed to be set or meaningful — an empty or relative
	// oddity should not produce help that reads "  . <command>".
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "go-dicom"
	}
	return name
}

func main() {
	// Create CLI instance
	dicomCLI := cli.NewCLI(programName(), Version)

	// Register file commands
	dicomCLI.RegisterCommand(&cli.ShowCommand{})
	dicomCLI.RegisterCommand(&cli.InfoCommand{})
	dicomCLI.RegisterCommand(&cli.ConvertCommand{})
	dicomCLI.RegisterCommand(&cli.TagDocCommand{})
	dicomCLI.RegisterCommand(&cli.CodifyCommand{})
	dicomCLI.RegisterCommand(&cli.HelpCommand{})
	dicomCLI.RegisterCommand(&cli.VersionCommand{
		AppName:    "go-dicom",
		AppVersion: Version,
	})

	// Register network commands
	dicomCLI.RegisterCommand(&cli.EchoSCUCommand{})
	dicomCLI.RegisterCommand(&cli.EchoSCPCommand{})
	dicomCLI.RegisterCommand(&cli.StoreSCUCommand{})
	dicomCLI.RegisterCommand(&cli.StoreSCPCommand{})
	dicomCLI.RegisterCommand(&cli.CommitSCUCommand{})
	dicomCLI.RegisterCommand(&cli.FindSCUCommand{})
	dicomCLI.RegisterCommand(&cli.MoveSCUCommand{})
	dicomCLI.RegisterCommand(&cli.GetSCUCommand{})
	dicomCLI.RegisterCommand(&cli.QRSCPCommand{})

	// Run CLI with command-line arguments
	if err := dicomCLI.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
