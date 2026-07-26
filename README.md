# go-dicom

[![Go Reference](https://pkg.go.dev/badge/github.com/amrshadid/go-dicom.svg)](https://pkg.go.dev/github.com/amrshadid/go-dicom)
[![Tests](https://github.com/amrshadid/go-dicom/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/amrshadid/go-dicom/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/amrshadid/go-dicom?sort=semver)](https://github.com/amrshadid/go-dicom/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/amrshadid/go-dicom)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/github/license/amrshadid/go-dicom)](./LICENSE)

A comprehensive, high-performance Go library for reading, writing, manipulating, and **networking** DICOM (Digital Imaging and Communications in Medicine) data. The Go equivalent of Python's pydicom + pynetdicom.

## Overview

go-dicom is designed for healthcare IT systems, medical imaging applications, PACS systems, and clinical data management. It provides:

- **Complete DICOM file I/O** with support for all transfer syntaxes (.dcm, .ima, DICOMDIR, raw)
- **DICOM networking** — SCU/SCP with C-ECHO, C-STORE, C-FIND, C-MOVE, C-GET, and all N-DIMSE services
- **Thread-safe dataset operations** for concurrent processing
- **5,000+ standard DICOM tags** with O(1) lookup
- **10,500+ private vendor tags** (GE, Siemens, Philips, Toshiba, and more)
- **De-identification / anonymization** per DICOM PS3.15 Annex E
- **Pixel data extraction** with multi-frame and multi-bit-depth support
- **30+ international character encodings** (Japanese, Chinese, Korean, Arabic, etc.)
- **TLS support** for encrypted DICOM communication (HIPAA compliance)
- **CLI tools** for file inspection, conversion, and network operations (echoscu, storescu, storescp, findscu, movescu)

## Quick Start

### Installation

```bash
go get github.com/amrshadid/go-dicom
```

### DICOM Networking (SCU Client)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/amrshadid/go-dicom/network"
)

func main() {
    ctx := context.Background()

    // Create SCU (client) — equivalent to pynetdicom's AE().associate()
    scu := network.NewSCU(network.SCUConfig{
        CallingAE: "MY_APP",
        CalledAE:  "PACS",
        Address:   "pacs.hospital.com:11112",
    })

    // Associate with the server
    if err := scu.Associate(ctx, nil); err != nil {
        log.Fatal(err)
    }
    defer scu.Release(ctx)

    // C-ECHO (verification/ping)
    if err := scu.Echo(ctx); err != nil {
        log.Fatal(err)
    }
    fmt.Println("Server is reachable!")

    // C-STORE (send a dataset)
    // err = scu.Store(ctx, dataset)

    // C-FIND (query) — results stream on a Go channel
    // results, _ := scu.Find(ctx, queryDataset)
    // for result := range results {
    //     fmt.Println(result.DataSet)
    // }

    // C-MOVE (retrieve to another AE)
    // err = scu.Move(ctx, queryDataset, "DEST_AE")
}
```

### DICOM Networking (SCP Server)

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/amrshadid/go-dicom/dataset"
    "github.com/amrshadid/go-dicom/network"
)

func main() {
    ctx := context.Background()

    // Create SCP (server) — equivalent to pynetdicom's AE().start_server()
    scp := network.NewSCP(network.SCPConfig{
        AETitle: "MY_SCP",
        Port:    11112,
    })

    // Set handler for incoming requests
    scp.SetHandler(&network.StorageHandler{
        OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
            fmt.Printf("Received: %s\n", sopInstance)
            // Save to disk, database, forward to another PACS, etc.
            return network.StatusSuccess
        },
    })

    // Listen and serve (blocks, handles associations in goroutines)
    log.Fatal(scp.ListenAndServe(ctx))
}
```

### Read a DICOM File

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/amrshadid/go-dicom/filebase"
    "github.com/amrshadid/go-dicom/filereader"
    "github.com/amrshadid/go-dicom/tag"
)

func main() {
    file, err := os.Open("patient.dcm")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Wrap the file in a byte-order-aware reader
    reader := filebase.NewFileReader(file)

    dicomFile, err := filereader.ReadDICOMFile(reader)
    if err != nil {
        log.Fatal(err)
    }

    // Non-fatal parse issues (unknown tags, retired tags, VR mismatches)
    for _, w := range dicomFile.Warnings {
        log.Println("warning:", w)
    }

    // Convert to a Dataset — nested sequences become child Datasets
    ds := dicomFile.GetDataset()

    // Access elements by tag
    if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok { // Patient Name
        fmt.Printf("Patient: %s\n", elem.GetValue())
    }

    fmt.Println("Transfer Syntax:", dicomFile.FileMetaInfo.TransferSyntaxUID)
}
```

### Work with Sequences

Nested sequences (SQ) are parsed recursively into child `Dataset` values:

```go
ds := dicomFile.GetDataset()

// (0040,A730) ContentSequence
if seq, err := ds.GetSequence(tag.New(0x0040, 0xA730)); err == nil {
    for i := 0; i < seq.Length(); i++ {
        item, _ := seq.Get(i)
        child := item.(*dataset.Dataset)

        // Child datasets know their parent
        if code, ok := child.Get(tag.New(0x0008, 0x0100)); ok {
            fmt.Printf("item %d code value: %s\n", i, code.GetValue())
        }
    }
}
```

### Command-Line Tools

```bash
# Build the CLI
go build -o dicom .

# === File Operations ===
./dicom show patient.dcm          # Display DICOM file contents
./dicom info patient.dcm           # Display file metadata
./dicom convert patient.dcm out.json  # Convert to JSON

# === Network Operations (like pynetdicom CLI) ===
# Verification (ping a PACS)
./dicom echoscu pacs.hospital.com:11112

# Send DICOM files (.dcm, .ima, any DICOM format)
./dicom storescu -aec PACS pacs:11112 study/*.dcm

# Start a storage server (receive files)
./dicom storescp -port 11112 -output ./received/

# Query for patients/studies
./dicom findscu -patient-name "Smith*" -level STUDY pacs:11112

# Retrieve studies to a destination
./dicom movescu -dest MY_SCP -study 1.2.3.4 pacs:11112

# Get help
./dicom -h
```

## Architecture

### Module Organization

The library is organized into focused packages, each handling a specific DICOM aspect:

#### Core I/O
| Package | Description |
|---------|-------------|
| [filebase](./filebase/) | Low-level binary I/O with byte order handling |
| [filereader](./filereader/) | DICOM file reading (preamble, meta info, dataset) |
| [filewriter](./filewriter/) | DICOM file writing with validation |
| [fileutil](./fileutil/) | Byte order detection, padding, caching, codec integration |
| [fileset](./fileset/) | DICOM file collection management |

#### Data Model
| Package | Description |
|---------|-------------|
| [dataset](./dataset/) | Thread-safe in-memory dataset with rich query API |
| [dataelem](./dataelem/) | Data element representation with all 28+ VRs |
| [tag](./tag/) | Tag definitions, dictionary (5,000+ standard + 10,500+ private) |
| [element](./element/) | Value encoding, decoding, and conversion |
| [sequence](./sequence/) | Thread-safe ordered sequence container |
| [uid](./uid/) | UID management, validation, and classification |
| [valuerep](./valuerep/) | Value representation validation and parsing |
| [values](./values/) | Value conversion and encoding |
| [multival](./multival/) | Type-safe multi-value lists |

#### Networking (DICOM Upper Layer Protocol)
| Package | Description |
|---------|-------------|
| [network](./network/) | DICOM networking — SCU/SCP, DIMSE services, PDU encoding, TLS |

#### Encoding and Compression
| Package | Description |
|---------|-------------|
| [charset](./charset/) | 30+ character set encodings (ISO 2022, Unicode, CJK) |
| [compress](./compress/) | Compression/decompression (DEFLATE, RLE, JPEG) |
| [encaps](./encaps/) | Encapsulated pixel data parsing and frame extraction |

#### Imaging and Clinical
| Package | Description |
|---------|-------------|
| [pixels](./pixels/) | Pixel data access and statistical analysis |
| [overlays](./overlays/) | Overlay groups, ROI analysis, graphics |
| [waveforms](./waveforms/) | Physiological signals (ECG, EEG) with QRS detection |
| [sr](./sr/) | Structured reports with coded concepts (SNOMED-CT, LOINC) |
| [anonymize](./anonymize/) | De-identification per DICOM PS3.15 Annex E |

#### Serialization and Utilities
| Package | Description |
|---------|-------------|
| [jsonrep](./jsonrep/) | DICOM JSON Model (Part 18) with bulk data support |
| [config](./config/) | Thread-safe global configuration |
| [errors](./errors/) | DICOM-specific error types |
| [hooks](./hooks/) | Extensible callback/plugin system |
| [util](./util/) | General utilities (hex dump, dataset info) |
| [cli](./cli/) | Command-line interface framework |

## Networking Features

### pynetdicom Feature Parity

go-dicom's `network` package provides feature parity with [pynetdicom](https://github.com/pydicom/pynetdicom), reimplemented in Go with goroutines, channels, and `context.Context`.

| Feature | pynetdicom | go-dicom | Notes |
|---------|-----------|----------|-------|
| C-ECHO (Verification) | Yes | Yes | `scu.Echo(ctx)` |
| C-STORE (Storage) | Yes | Yes | `scu.Store(ctx, ds)` |
| C-FIND (Query) | Yes | Yes | `scu.Find(ctx, ds)` — streams via Go channel |
| C-MOVE (Retrieve) | Yes | SCU only | `scu.Move(ctx, ds, dest)`. SCP side returns status but does not perform C-STORE sub-operations — see Limitations |
| C-GET (Get) | Yes | SCU only | `scu.Get(ctx, ds)`. SCP side returns status but does not perform C-STORE sub-operations — see Limitations |
| N-EVENT-REPORT | Yes | Yes | Full N-DIMSE service support |
| N-GET | Yes | Yes | |
| N-SET | Yes | Yes | |
| N-ACTION | Yes | Yes | |
| N-CREATE | Yes | Yes | |
| N-DELETE | Yes | Yes | |
| SCU (Client) | Yes | Yes | `network.NewSCU()` |
| SCP (Server) | Yes | Yes | `network.NewSCP()` with goroutine-per-association |
| TLS Encryption | Yes | Yes | `network.DialTLS()` / `network.ListenTLS()` |
| Association Negotiation | Yes | Yes | Full A-ASSOCIATE-RQ/AC/RJ state machine |
| Presentation Context Negotiation | Yes | Yes | Abstract + Transfer Syntax negotiation |
| Extended Negotiation | Yes | Yes | Async ops, SCP/SCU role selection, user identity — negotiated on the wire |
| Storage SOP Classes | 100+ | 80+ | CT, MR, US, PET, RT, XR, SR, waveforms, encapsulated docs |
| Transfer Syntax Support | 15+ | 15 | All standard + compressed syntaxes |
| Query/Retrieve Models | Patient/Study Root | Yes | Find, Move, Get for both models |
| Modality Worklist | Yes | Yes | MWL SOP Class with WorklistHandler |
| MPPS | Yes | Yes | Via N-CREATE/N-SET |
| Print Management | Yes | Yes | SOP Class UIDs defined |
| Handler Interface | evt_handlers | Handler interface | Go-idiomatic with BaseHandler embedding |
| CLI Tools | 7 tools | 5 tools | echoscu, storescu, storescp, findscu, movescu |
| Async Operations | Thread pool | Goroutines | Native Go concurrency |
| Context/Cancellation | N/A | context.Context | Timeouts, graceful shutdown |

### Limitations

Known gaps, stated plainly so you can judge fit before adopting:

| Area | Status |
|------|--------|
| **C-MOVE / C-GET as an SCP** | The handler is invoked and a status is returned, but the SCP does **not** send C-STORE sub-operations to the destination. Acting as a retrieval *provider* requires implementing this yourself. Both work fully as an **SCU**. |
| **Asynchronous operations** | Negotiated on the wire and reported to the peer, but not enforced — the SCU issues one operation at a time and waits for the response. |
| **Transcoding between transfer syntaxes** | A data set is sent using the syntax negotiated for its presentation context. The library does not re-encode pixel data, so sending a JPEG-compressed data set over a context that negotiated uncompressed explicit VR will not decompress it for you. |
| **Sequence writing** | The reader parses nested sequences; `filewriter` does not yet serialize `SQ` elements back out (waveform sequences are the exception). Reading and forwarding sequences works; round-tripping them to disk does not. |
| **`show` / `info` / `convert` CLI commands** | Use a separate flat parser and do not descend into sequences. The network path and `filereader` do. |
| **Concurrent use of one SCU** | An `SCU` issues one DIMSE operation at a time. Use one `SCU` per goroutine rather than sharing one across goroutines. |

### Supported File Formats

The network module works with any DICOM data regardless of source format:

| Format | Extension | Support |
|--------|-----------|---------|
| Standard DICOM | `.dcm` | Full |
| Siemens IMA | `.ima` | Full |
| DICOMDIR | `DICOMDIR` | Full |
| Raw DICOM | (none) | Full |
| DICOM Part 10 | `.dicom` | Full |

### Handler Patterns

```go
// 1. Echo-only (verification server)
scp.SetHandler(&network.EchoHandler{})

// 2. Storage with callback
scp.SetHandler(&network.StorageHandler{
    OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
        // Save to disk, database, cloud storage, etc.
        return network.StatusSuccess
    },
})

// 3. Query/Retrieve with callbacks
scp.SetHandler(&network.QueryRetrieveHandler{
    OnFind: func(ctx context.Context, sopClass string, query *dataset.Dataset) ([]*dataset.Dataset, error) {
        // Search database, return matching results
        return results, nil
    },
})

// 4. Modality Worklist
scp.SetHandler(&network.WorklistHandler{
    OnWorklist: func(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
        // Return scheduled procedures
        return procedures, nil
    },
})

// 5. Composite handler (mix & match)
h := network.NewCompositeHandler()
h.SetStoreHandler(myStoreHandler)
h.SetFindHandler(myFindHandler)
scp.SetHandler(h)

// 6. Custom handler (implement the interface)
type MyHandler struct { network.BaseHandler }
func (h *MyHandler) HandleCStore(ctx context.Context, req *network.CStoreRequest) (*network.CStoreResponse, error) {
    // Full control over request processing
}
```

### Extended Negotiation

Extended negotiation items are carried in the A-ASSOCIATE-RQ/AC User Information
item. SCP/SCU role selection is what allows an SCU to also act as an SCP for a
SOP Class on an association it initiated — required for C-GET, where the peer
sends C-STORE sub-operations back over the same association.

```go
scu := network.NewSCU(network.SCUConfig{
    CallingAE: "MY_APP",
    CalledAE:  "PACS",
    Address:   "pacs.hospital.com:11112",
    ExtendedNegotiation: &network.ExtendedNegotiation{
        // Allow the peer to send C-STORE back to us for C-GET
        RoleSelections: []network.SCPSCURoleSelection{
            {SOPClassUID: network.CTImageStorageUID, SCURole: true, SCPRole: true},
        },
        // Permit up to 4 outstanding operations in each direction
        AsyncOperations: &network.AsynchronousOperationsWindow{
            MaxOperationsInvoked:   4,
            MaxOperationsPerformed: 4,
        },
        // Authenticate with the remote AE
        UserIdentity: &network.UserIdentityNegotiation{
            Type:           network.UserIdentityUsernamePassword,
            PrimaryField:   []byte("operator"),
            SecondaryField: []byte("password"),
        },
    },
})

// After associating, inspect what the peer agreed to
if role, ok := scu.Association().RoleSelectionFor(network.CTImageStorageUID); ok {
    fmt.Println("peer accepted SCP role:", role.SCPRole)
}
```

### Association Info in Handlers

SCP handlers can read the association's details from the request context
without changing the `Handler` interface:

```go
scp.SetHandler(&network.StorageHandler{
    OnStore: func(ctx context.Context, sopClass, sopInstance string, ds *dataset.Dataset) uint16 {
        if info, ok := network.AssociationInfoFromContext(ctx); ok {
            log.Printf("from %s (%s) via %s",
                info.CallingAE, info.RemoteAddr, info.PeerImplementationVersion)
        }
        return network.StatusSuccess
    },
})
```

## Features

### DICOM Standards Compliance

| Standard | Status |
|----------|--------|
| DICOM PS3.5 - Data Structures and Encoding | Supported (incl. nested sequences, undefined-length items, even-length padding) |
| DICOM PS3.6 - Data Dictionary | Supported (5,000+ tags) |
| DICOM PS3.7 - Message Exchange (DIMSE) | Supported |
| DICOM PS3.8 - Network Communication (Upper Layer) | Supported |
| DICOM PS3.10 - Media Storage and File Format | Supported |
| DICOM PS3.15 - Security (TLS, de-identification) | Supported |
| DICOM JSON Model (Part 18) | Supported |
| ISO 2022 - Character set escape sequences | Supported |

### Transfer Syntax Support

| Transfer Syntax | File I/O | Network |
|----------------|----------|---------|
| Implicit VR Little Endian | Read/Write | Yes |
| Explicit VR Little Endian | Read/Write | Yes |
| Explicit VR Big Endian | Read/Write | Yes |
| Deflated Explicit VR LE | Read | Yes |
| RLE Lossless | Read | Yes |
| JPEG Baseline | Read | Yes |
| JPEG Extended | Read | Yes |
| JPEG Lossless | Read | Yes |
| JPEG-LS Lossless | Read | Yes |
| JPEG-LS Near-Lossless | Read | Yes |
| JPEG 2000 Lossless | Read | Yes |
| JPEG 2000 | Read | Yes |

### Thread Safety

All mutable data structures use `sync.RWMutex` for concurrent access. Datasets, sequences, and managers are safe for concurrent reads with exclusive writes. The SCP server spawns a goroutine per association for concurrent client handling.

### De-identification Profiles

The `anonymize` package supports multiple de-identification profiles per DICOM PS3.15:

- **Basic Profile** - Standard tag removal/replacement
- **Clean Descriptors** - Remove text descriptions
- **Clean Graphics** - Remove burned-in annotations
- **Retain Long Full Dates** - Keep dates for longitudinal studies
- **Retain Patient Characteristics** - Keep age, sex, size, weight
- **Retain Device Identity** - Keep device information
- **Retain UIDs** - Keep original UIDs
- **Retain Safe Private** - Keep safe private tags

## Building

```bash
# Build the library
go build ./...

# Build the CLI tool
go build -o dicom .

# Run all tests (71 network tests + file I/O tests)
go test -race ./...

# Run network tests specifically
go test -v ./network/...

# Run tests with coverage
go test -race -coverprofile=coverage.out ./... && go tool cover -html=coverage.out

# Run linter
golangci-lint run ./...
```

## Examples

See the [examples](./examples/) directory for complete working examples:

- **[Read DICOM](./examples/input_output/read_dicom/)** - Reading DICOM files and accessing elements
- **[Write DICOM](./examples/input_output/write_dicom/)** - Creating DICOM files from scratch
- **[Modify DICOM](./examples/input_output/modify_dicom/)** - Modifying existing DICOM files
- **[Print Dataset](./examples/input_output/printing_dataset/)** - Displaying dataset contents
- **[Sequences](./examples/metadata_processing/sequences/)** - Working with nested sequences
- **[Image Processing](./examples/image_processing/)** - Reslicing, downsizing, waveforms
- **[Networking](./examples/networking/)** - SCU/SCP, C-ECHO, C-STORE, C-FIND, handler patterns

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes with tests
4. Run `make all` to verify
5. Submit a pull request

## Security

For security concerns, especially regarding Protected Health Information (PHI), please see [SECURITY.md](./SECURITY.md).

## License

MIT License - see [LICENSE](./LICENSE) for details.

## Acknowledgments

- [DICOM Standard](https://www.dicomstandard.org/) - The foundation this library is built on
- [pydicom](https://pydicom.github.io/) - Python DICOM library that inspired the API design
- [pynetdicom](https://github.com/pydicom/pynetdicom) - Python DICOM networking library that inspired the network module
- [Go Community](https://golang.org/) - Excellent standard library and ecosystem

## Resources

- [DICOM Standard Browser](https://dicom.innolitics.com/ciods) - Interactive tag browser
- [pynetdicom Documentation](https://pydicom.github.io/pynetdicom/) - Reference for DICOM networking concepts
- [DICOM PS3.5 - Data Structures](https://dicom.nema.org/medical/dicom/current/output/html/part05.html)
- [DICOM PS3.6 - Data Dictionary](https://dicom.nema.org/medical/dicom/current/output/html/part06.html)
- [DICOM PS3.7 - Message Exchange](https://dicom.nema.org/medical/dicom/current/output/html/part07.html)
- [DICOM PS3.8 - Network Communication](https://dicom.nema.org/medical/dicom/current/output/html/part08.html)
- [DICOM PS3.10 - Media Storage](https://dicom.nema.org/medical/dicom/current/output/html/part10.html)
- [DICOM PS3.15 - Security Profiles](https://dicom.nema.org/medical/dicom/current/output/html/part15.html)
