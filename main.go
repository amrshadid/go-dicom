package main

import (
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/cli"
)

// Version of the CLI application
const Version = "1.0.0"

func main() {
	// Create CLI instance
	dicomCLI := cli.NewCLI("go-dicom", Version)

	// Register commands
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

	// Run CLI with command-line arguments
	if err := dicomCLI.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
