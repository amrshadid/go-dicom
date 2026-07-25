package main

import (
	"fmt"
	"os"

	"github.com/amrshadid/go-dicom/cli"
)

// Version of the CLI application
const Version = "1.2.0"

func main() {
	// Create CLI instance
	dicomCLI := cli.NewCLI("go-dicom", Version)

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
