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

### Read a file and write it back

```go
df, _ := filereader.ReadDICOMFile(filebase.NewFileReader(in))

w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
w.SetFileMetaInfo(&filewriter.FileMetaInfo{ /* ... */ })
for _, e := range filewriter.ElementsFromDataset(df.GetDataset()) {
    _ = w.AddDataElement(e)
}
_ = w.Write()
```

`ElementsFromDataset` descends into sequences. Copying `Value` and ignoring
`Items` writes a file that looks complete with every nested item missing — the
element is present, its length is zero, and nothing reports it.

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

# Ask an archive to take responsibility for instances you have sent
./dicom commitscu -aec PACS -instance 1.2.840.10008.5.1.4.1.1.2:1.2.3.4 -wait pacs:11112

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
| [charset](./charset/) | 30+ encodings (ISO 2022, Unicode, CJK), applied automatically on read |
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
| C-MOVE (Retrieve) | Yes | Yes | `scu.Move(ctx, ds, dest)`; SCP opens an association to the destination AE and transfers via C-STORE sub-operations |
| C-GET (Get) | Yes | Yes | `scu.Get(ctx, ds)` with `SCUConfig.OnCStore`; SCP transfers instances via C-STORE sub-operations |
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
| Storage SOP Classes | 165 | 169 | Every one pynetdicom lists, plus 4 recent standard additions |
| SOP Class UIDs named | 256 | 256 | Full parity, checked against pynetdicom's own table |
| Transfer Syntax Support | 37 | 37 | 4 negotiated by default, all via `AllTransferSyntaxes()` |
| Storage Commitment | Yes | Yes | N-ACTION request, N-EVENT-REPORT result |
| Query/Retrieve Models | 2 | 4 | Patient Root, Study Root, Patient/Study Only, Modality Worklist |
| Modality Worklist | Yes | Yes | MWL SOP Class with WorklistHandler |
| MPPS | Yes | Yes | N-CREATE/N-SET both directions, verified against pynetdicom |
| Print Management | Yes | Yes | Film session, film box, image box, print action |
| Unified Procedure Step | Yes | Yes | Push: state machine and Transaction UID enforced by `UPSHandler` |
| Handler Interface | evt_handlers | Handler interface | Go-idiomatic with BaseHandler embedding |
| CLI Tools | 8 | 9 | All 8 of pynetdicom's, plus `commitscu` |
| Async Operations | Thread pool | Goroutines | Native Go concurrency |
| Context/Cancellation | N/A | context.Context | Timeouts, graceful shutdown |

### Limitations

Known gaps, stated plainly so you can judge fit before adopting:

| Area | Status |
|------|--------|
| **Move destination resolution** | A C-MOVE names its destination only by AE title, so the SCP must be told how to reach it via `SCPConfig.MoveDestinations` or `SCPConfig.ResolveMoveDestination`. An unresolvable title is answered with `StatusMoveDestUnknown` rather than guessed at. |
| **Asynchronous operations** | Negotiated on the wire and reported to the peer, but not enforced — the SCU issues one operation at a time and waits for the response. |
| **Cancelling a slice handler** | A C-FIND, C-GET or C-MOVE handler that returns a slice cannot be interrupted while it builds one. Implement `CFindStreamer`, `CGetStreamer` or `CMoveStreamer` to stop on C-CANCEL; sub-operations are abandoned on cancel either way. |
| **Compressing pixel data** | A compressed instance sent over a context that negotiated an uncompressed syntax is decoded first, and the send fails if it cannot be. The reverse is not available: this library compresses no pixel data, so a native instance cannot be sent over a context that negotiated a compressed syntax. |
| **Concurrent use of one SCU** | An `SCU` issues one DIMSE operation at a time. Use one `SCU` per goroutine rather than sharing one across goroutines. |

### Interoperability

Every release is tested against [pynetdicom](https://github.com/pydicom/pynetdicom) and
[dcmtk](https://dcmtk.org/) in CI — C-ECHO and C-STORE in both directions, C-GET and
C-MOVE sub-operations, storage commitment, MPPS and print management — with the
transferred data verified by pydicom rather than by this library's own reader.

This matters more than the unit suite: every serious defect this project has had was
code agreeing with itself. Reintroducing one of them, the N-SET that named its target
with Affected instead of Requested SOP Instance UID, leaves the unit tests green and
fails the interoperability tests.

```bash
go build -o dicom .
PYNETDICOM_BIN=/path/to/venv/bin DCMTK_BIN=/usr/bin ./scripts/interop-test.sh
```

The fixture is pydicom's `CT_small.dcm`, not a file this project generates, so it
exercises encodings the library would not think to produce itself.

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
        // Returns nil when called outside an association, so check it.
        if info := network.AssociationInfoFromContext(ctx); info != nil {
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

### Performance

Measured against pydicom on its own test corpus, same machine, 300 iterations:

| File | go-dicom | pydicom | |
|---|---|---|---|
| `CT_small.dcm` (39 KB, 258 elements) | 110 µs | 499 µs | **4.5x** |
| `MR_small.dcm` (10 KB, 73 elements) | 23 µs | 219 µs | **9.5x** |
| `rtplan.dcm` (2.7 KB, 36 elements) | 31 µs | 138 µs | **4.5x** |

Reproduce with `go test ./filereader/ -bench=Parse` and the corpus path in
`GODICOM_PYDICOM_DATA`.

### Conformance

[CONFORMANCE.md](./CONFORMANCE.md) is a DICOM Conformance Statement in the
structure of PS3.2: the SOP classes negotiated in each role, the transfer
syntaxes, the enforced limits, and a plainly stated list of what the library does
not do. It is the document to hand to someone evaluating go-dicom for a clinical
deployment, and every claim in it was checked against the code rather than
described from memory.

### Transfer Syntax Support

Two capabilities are distinct and worth separating: whether the **data set**
parses, and whether **pixel data** can be decoded. A compressed instance
transfers and stores correctly even when its pixels cannot be decompressed —
which is enough for an archive or a router, and not enough for a viewer.

| Transfer Syntax | Data set | Pixel data | Network |
|-----------------|----------|------------|---------|
| Implicit VR Little Endian | Read/Write | Yes | Yes |
| Explicit VR Little Endian | Read/Write | Yes | Yes |
| Explicit VR Big Endian | Read/Write | Yes | Yes |
| Deflated Explicit VR LE | Read/Write | Yes | Yes |
| RLE Lossless | Read/Write | **Yes** | Yes |
| JPEG Baseline (`.50`) | Read/Write | **Yes** | Yes |
| JPEG Extended (`.51`) | Read/Write | **8-bit** | Yes |
| JPEG Lossless (`.57`, `.70`) | Read/Write | **Yes** | Yes |
| JPEG-LS Lossless / Near-Lossless (`.80`, `.81`) | Read/Write | **Yes** | Yes |
| JPEG 2000 Lossless / Lossy (`.90`, `.91`) | Read/Write | **Supply a decoder** | Yes |

Pixel data is returned in the color space the Photometric Interpretation names, so a
`YBR_FULL` instance yields YBR rather than RGB — the same as pydicom. Both planar
configurations, 1 to 64 bits, and subsampled `YBR_FULL_422` are handled.

JPEG 2000 needs a decoder you supply — `examples/jpeg2000` is a working one that
shells out to openjpeg, verified sample-for-sample against pydicom.
JPEG Extended decodes at 8 bits; 12-bit frames are refused, with precision named as
the reason. JPEG-LS decodes at 2 to 16 bits, lossless and near-lossless, single or multi component,
in both line- and sample-interleaved modes.

Values in an Explicit VR Big Endian file are normalised to little endian while
parsing and converted back on write, so byte order never reaches code above
`filereader`.

**RLE Lossless decodes.** `Dataset.PixelArray()` decompresses RLE pixel data,
single- and multi-frame, grayscale and color. Verified against pydicom on its
own test corpus, and checked in CI on every push.

**No other compressed syntax decodes.** JPEG, JPEG-LS, and JPEG 2000 instances
parse, store, and transfer with their pixel data intact as opaque bytes, but
have no bundled decoder. Baseline JPEG goes through the standard library and
works for ordinary 8-bit images; the rest need a decoder you supply.

**Compressed frames can be extracted**, which is the step before decoding. For a
compressed instance, `PixelData` holds the encapsulation exactly as it appears in
the file — the Basic Offset Table and each fragment with its `(FFFE,E000)`
header, the same bytes pydicom exposes — so frames can be separated and handed to
a decoder of your choosing:

```go
frames, err := ds.ExtractEncapsulatedFrames() // fragments and offset table
frame, err := ds.GetEncapsulatedFrame(0)      // one frame, still compressed
```

Multi-frame compressed images split correctly; verified against pydicom on
`SC_rgb_rle_2frame.dcm`.

To decode a syntax with no bundled decoder, register one:

```go
compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_2000, myDecoder)
```

These three codecs are not implemented in this module, and there is no hidden
CGO path that enables them — the error messages used to name a C library and
tell you to rebuild with `CGO_ENABLED=1`, which changed nothing because there
was no CGO implementation to enable. They now say plainly that a decoder must be
supplied.

Any type with `Decompress([]byte) ([]byte, error)` and `CanDecompress([]byte) bool`
will do; `Dataset.PixelArray()` routes frames through it automatically once
registered. `compress.GetExternalCompressionStatus()` reports which codecs
currently have a decoder.

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
