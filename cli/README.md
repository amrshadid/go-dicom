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
    app := cli.NewCLI("go-dicom", "1.0.0")
    app.RegisterCommand(cli.NewShowCommand())
    app.RegisterCommand(cli.NewInfoCommand())
    app.RegisterCommand(cli.NewConvertCommand())
    if err := app.Run(os.Args[1:]); err != nil {
        os.Exit(1)
    }
}
```

```bash
go-dicom show patient.dcm                    # Display file contents
go-dicom show patient.dcm::PatientName       # Show specific element
go-dicom info -verbose -stats patient.dcm    # File metadata
go-dicom convert patient.dcm output.json     # Convert to JSON
go-dicom anonymize study/ clean-study/       # De-identify a file or a directory
go-dicom anonymize -profile retain-dates scan.dcm clean.dcm
go-dicom anonymize -redact 0,0,640,40 us.dcm clean.dcm   # Black out burned-in text
go-dicom share ct.dcm ct.png --window 40,400             # A picture that is safe to hand on
go-dicom share -redact 0,0,640,40 us.dcm us.png          # ...with burned-in text blacked out
go-dicom convert ct.dcm ct.png --format png  # Render as PNG (or jpeg), windowed
go-dicom convert cine.dcm f.png --format png --frame all   # f-0001.png, f-0002.png, ...
go-dicom codify patient.dcm -output struct.go # Generate Go code
go-dicom tag-doc -keyword PatientName        # Tag documentation
go-dicom qido -match "PatientName=Doe*" https://pacs.example.org/dicom-web   # Search a DICOMweb service
go-dicom wado -study 1.2.840.1 https://pacs.example.org/dicom-web study/     # Download a study
go-dicom stow https://pacs.example.org/dicom-web study/                      # Upload files
```

## The event log

Every network command (`echoscu`, `echoscp`, `storescu`, `storescp`, `findscu`,
`movescu`, `getscu`, `commitscu`, `qrscp`) takes `-events <file>`, or `-events -`
for standard output. It appends one JSON object per line as things happen, so
the file can be followed while the command runs:

```json
{"event":"EVT_DIMSE_RECV","time":"2026-11-14T18:20:11.482913Z","associationId":1,"role":"SCU","localAE":"STORESCU","remoteAE":"PACS","localAddr":"10.0.0.5:51822","remoteAddr":"10.0.0.9:11112","dimse":{"command":"C-STORE-RSP","commandField":32769,"contextId":1,"messageIdBeingRespondedTo":1,"affectedSOPClassUID":"1.2.840.10008.5.1.4.1.1.2","affectedSOPInstanceUID":"1.2.3.4","hasDataSet":false,"status":0}}
```

| Event | When | Carries |
|---|---|---|
| `EVT_CONN_OPEN`, `EVT_CONN_CLOSE` | the connection opens and closes | addresses |
| `EVT_PDU_SENT`, `EVT_PDU_RECV` | every PDU, either way | `pdu`: its type, length and bytes in hex; an A-ASSOCIATE-RQ's and -AC's presentation contexts and maximum PDU length; an A-ASSOCIATE-RJ's result, source and reason; an A-ABORT's source and reason; a P-DATA-TF's PDVs |
| `EVT_REQUESTED`, `EVT_ACCEPTED`, `EVT_ESTABLISHED`, `EVT_REJECTED`, `EVT_RELEASED`, `EVT_ABORTED` | the association's own events | the PDU that caused it; on accepted and established, `presentationContexts`: each as proposed and as answered |
| `EVT_DIMSE_SENT`, `EVT_DIMSE_RECV` | every DIMSE message, either way | `dimse`: command, message ID, SOP class and instance UIDs, status, and the remaining, completed, failed and warning counts of a C-GET or C-MOVE |
| `EVT_C_ECHO`, `EVT_C_STORE`, `EVT_C_FIND`, `EVT_C_GET`, `EVT_C_MOVE`, `EVT_N_…` | a request arrives | `dimse` |
| `STORED` | `storescp` or `qrscp` kept an instance | `stored`: its UIDs and the file's path. The command's own line, not a network event |

`role` is `SCU` on the side that requested the association and `SCP` on the
side that accepted it. `associationId` tells one association from another
within a run; `qrscp` logs the association a C-MOVE opens to its destination
under its own. Two commands' logs are paired by their addresses. A PDU's bytes
are cut at `-events-pdu-bytes` (65536 by default, 0 for all), and
`bytesTruncated` says when.

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
