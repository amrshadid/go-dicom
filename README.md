# go-dicom

A comprehensive, high-performance Go library for reading, writing, and manipulating DICOM (Digital Imaging and Communications in Medicine) files.

## Overview

go-dicom is designed for healthcare IT systems, medical imaging applications, PACS systems, and clinical data management. It provides:

- **Complete DICOM file I/O** with support for all transfer syntaxes
- **Thread-safe dataset operations** for concurrent processing
- **5,000+ standard DICOM tags** with O(1) lookup
- **10,500+ private vendor tags** (GE, Siemens, Philips, Toshiba, and more)
- **De-identification / anonymization** per DICOM PS3.15 Annex E
- **Pixel data extraction** with multi-frame and multi-bit-depth support
- **30+ international character encodings** (Japanese, Chinese, Korean, Arabic, etc.)
- **CLI tool** for inspection, conversion, and manipulation

## Quick Start

### Installation

```bash
go get github.com/amrshadid/go-dicom
```

### Read a DICOM File

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/amrshadid/go-dicom/filereader"
    "github.com/amrshadid/go-dicom/tag"
)

func main() {
    file, err := os.Open("patient.dcm")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    dicomFile, err := filereader.ReadDICOMFile(file)
    if err != nil {
        log.Fatal(err)
    }

    ds := dicomFile.GetDataset()

    // Access elements by tag
    name, _ := ds.GetStringValue(tag.New(0x0010, 0x0010)) // Patient Name
    fmt.Println("Patient:", name)
}
```

### Write a DICOM File

```go
package main

import (
    "log"
    "os"

    "github.com/amrshadid/go-dicom/filewriter"
    "github.com/amrshadid/go-dicom/tag"
)

func main() {
    file, err := os.Create("output.dcm")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    writer := filewriter.NewDICOMFileWriter(file)

    writer.SetFileMetaInfo(&filewriter.FileMetaInfo{
        MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
        MediaStorageSOPInstanceUID: "1.2.3.4.5.6.7.8.9",
        TransferSyntaxUID:         "1.2.840.10008.1.2.1",
        ImplementationClassUID:    "1.2.3.4.5.6.7",
    })

    writer.AddDataElement(&filewriter.DataElement{
        Tag:   tag.New(0x0010, 0x0010),
        VR:    "PN",
        Value: []byte("Smith^John"),
    })

    if err := writer.Write(); err != nil {
        log.Fatal(err)
    }
}
```

### Anonymize a DICOM File

```go
package main

import (
    "log"

    "github.com/amrshadid/go-dicom/anonymize"
    "github.com/amrshadid/go-dicom/dataset"
)

func main() {
    ds := dataset.NewDataset()
    // ... load dataset from file ...

    anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
    if err := anon.Anonymize(ds); err != nil {
        log.Fatal(err)
    }

    // Patient name is now "ANONYMOUS", dates cleared, UIDs remapped
}
```

### Command-Line Tool

```bash
# Build the CLI
go build -o dicom .

# Show DICOM file contents
./dicom show patient.dcm

# Display file metadata
./dicom info patient.dcm

# Convert to JSON
./dicom convert patient.dcm output.json

# Generate Go code to recreate a DICOM file
./dicom codify patient.dcm --output create_patient.go

# Look up tag documentation
./dicom tag-doc 0010,0010

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

## Features

### DICOM Standards Compliance

| Standard | Status |
|----------|--------|
| DICOM PS3.5 - Data Structures and Encoding | Supported |
| DICOM PS3.6 - Data Dictionary | Supported (5,000+ tags) |
| DICOM PS3.10 - Media Storage and File Format | Supported |
| DICOM PS3.15 - Security and System Management (Annex E) | Supported (de-identification) |
| DICOM JSON Model (Part 18) | Supported |
| ISO 2022 - Character set escape sequences | Supported |

### Transfer Syntax Support

| Transfer Syntax | Read | Write |
|----------------|------|-------|
| Implicit VR Little Endian | Yes | Yes |
| Explicit VR Little Endian | Yes | Yes |
| Explicit VR Big Endian | Yes | Yes |
| DEFLATE (zlib) | Yes | No |
| RLE Lossless | Yes | No |
| JPEG Baseline | Yes | No |
| JPEG-LS | Planned | Planned |
| JPEG 2000 | Planned | Planned |

### Thread Safety

All mutable data structures use `sync.RWMutex` for concurrent access. Datasets, sequences, and managers are safe for concurrent reads with exclusive writes.

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

# Run all tests
go test -race ./...

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
- [Go Community](https://golang.org/) - Excellent standard library and ecosystem

## Resources

- [DICOM Standard Browser](https://dicom.innolitics.com/ciods) - Interactive tag browser
- [DICOM PS3.5 - Data Structures](https://dicom.nema.org/medical/dicom/current/output/html/part05.html)
- [DICOM PS3.6 - Data Dictionary](https://dicom.nema.org/medical/dicom/current/output/html/part06.html)
- [DICOM PS3.10 - Media Storage](https://dicom.nema.org/medical/dicom/current/output/html/part10.html)
- [DICOM PS3.15 - Security Profiles](https://dicom.nema.org/medical/dicom/current/output/html/part15.html)
