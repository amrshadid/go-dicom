# CLI

Command-line interface framework for DICOM file manipulation and analysis, with built-in commands for display, conversion, code generation, and tag documentation.

## Quick Start

```go
package main

import (
    "os"
    "github.com/amrshadid/go-dicom/cli"
)

func main() {
    app := cli.NewCLI("dicom", "1.0.0")
    app.RegisterCommand(cli.NewShowCommand())
    app.RegisterCommand(cli.NewInfoCommand())
    app.RegisterCommand(cli.NewConvertCommand())
    if err := app.Run(os.Args[1:]); err != nil {
        os.Exit(1)
    }
}
```

```bash
dicom show patient.dcm                    # Display file contents
dicom show patient.dcm::PatientName       # Show specific element
dicom info -verbose -stats patient.dcm    # File metadata
dicom convert patient.dcm output.json     # Convert to JSON
dicom codify patient.dcm -output struct.go # Generate Go code
dicom tag-doc -keyword PatientName        # Tag documentation
```

## API Reference

```go
type CLI struct { Name, Version string; Commands map[string]Command }
func NewCLI(name, version string) *CLI
func (c *CLI) RegisterCommand(cmd Command)
func (c *CLI) Run(args []string) error

type Command interface {
    Name() string
    Description() string
    AddFlags(*flag.FlagSet)
    Execute([]string) error
}

type FileSpec struct { FilePath, Element string; Index int }
func ParseFileSpec(spec string) (*FileSpec, error)
```

## References

- [DICOM PS3.6: Data Dictionary](https://dicom.nema.org/medical/dicom/current/output/html/part06.html)
- [DICOM PS3.10: Media Storage and File Format](https://dicom.nema.org/medical/dicom/current/output/html/part10.html)
