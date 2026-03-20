# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.1] - 2026-03-20

### Added
- **Association context info** (`network.AssociationInfo`) — Handlers can now access full association details via `AssociationInfoFromContext(ctx)`:
  - `CallingAE` / `CalledAE` — peer and local AE titles
  - `RemoteAddr` / `LocalAddr` — network addresses (IP:port)
  - `MaxPDUSize` — negotiated maximum PDU size
  - `AcceptedContexts` — negotiated presentation contexts
  - `PeerImplementationClassUID` / `PeerImplementationVersion` — peer's DICOM implementation info
- **Networking example** updated with `associnfo` subcommand demonstrating association context usage

## [1.1.0] - 2026-03-18

### Added
- **DICOM Networking module** (`network/`) - Complete DICOM Upper Layer Protocol implementation
  - **SCU (Client)**: `NewSCU()` with `Echo()`, `Store()`, `Find()`, `Move()`, `Release()`, `Abort()`
  - **SCP (Server)**: `NewSCP()` with `ListenAndServe()`, goroutine-per-association concurrency
  - **C-DIMSE services**: C-ECHO, C-STORE, C-FIND, C-MOVE, C-GET with full encode/decode
  - **N-DIMSE services**: N-EVENT-REPORT, N-GET, N-SET, N-ACTION, N-CREATE, N-DELETE
  - **PDU encoding/decoding**: A-ASSOCIATE-RQ/AC/RJ, P-DATA-TF, A-RELEASE-RQ/RP, A-ABORT
  - **Association state machine**: Full DICOM Part 8 state transitions with context-aware timeouts
  - **Presentation context negotiation**: Abstract syntax + transfer syntax matching
  - **Extended negotiation**: Async operations window, SCP/SCU role selection, user identity (username/password, Kerberos, SAML, JWT)
  - **TLS support**: `DialTLS()` and `ListenTLS()` for encrypted DICOM communication
  - **80+ Storage SOP Classes**: CT, MR, US, PET, NM, RT, XR, CR, DX, MG, VL, SR, waveforms, encapsulated documents (PDF, CDA, STL, OBJ), segmentation, parametric maps
  - **15 Transfer Syntaxes**: All uncompressed + JPEG, JPEG-LS, JPEG 2000, RLE, Deflated
  - **Query/Retrieve**: Patient Root and Study Root models (Find, Move, Get)
  - **Modality Worklist**: MWL SOP Class with `WorklistHandler`
  - **MPPS**: Modality Performed Procedure Step via N-CREATE/N-SET
  - **Print Management**: Film Session, Film Box, Image Box SOP Classes
  - **Handler system**: `BaseHandler` (embeddable defaults), `EchoHandler`, `StorageHandler`, `QueryRetrieveHandler`, `WorklistHandler`, `CompositeHandler`
  - **71 tests** with race detection, covering PDU encode/decode, DIMSE messages, association negotiation, SCU/SCP integration
  - Inspired by [pynetdicom](https://github.com/pydicom/pynetdicom), reimplemented with Go idioms (goroutines, channels, context.Context)

- **Network CLI commands** — equivalent to pynetdicom's CLI tools
  - `echoscu` — DICOM Echo verification (ping)
  - `storescu` — Send DICOM files (.dcm, .ima, any DICOM format)
  - `storescp` — Receive and save DICOM files
  - `findscu` — Query for patients/studies/series with wildcards
  - `movescu` — Retrieve studies to a destination AE

- **Networking example** (`examples/networking/`) — SCU/SCP usage, handler patterns, supported modalities

### Changed
- **Version** bumped to 1.1.0
- **README** rewritten with networking documentation, pynetdicom feature parity table, handler patterns, CLI network commands
- **CLI help** updated to show file and network command categories

## [Unreleased]

### Added
- **Anonymization module** (`anonymize/`) - DICOM de-identification per PS3.15 Annex E
  - Multiple profiles: Basic, Clean Descriptors, Retain Dates, Retain Patient Chars, etc.
  - Consistent UID remapping across anonymization sessions
  - Custom per-tag action overrides
- **Private tag dictionary** - 10,500+ private tags from major vendors (GE, Siemens, Philips, Toshiba, and more)
- **Project infrastructure** - CONTRIBUTING.md, CODE_OF_CONDUCT.md, SECURITY.md, Makefile, .golangci.yml
- **GitHub templates** - Issue templates (bug report, feature request), PR template

### Changed
- **Test organization** - Moved tests from `tests/` subdirectories to same directory as source (idiomatic Go)
- **Go version** - Minimum Go version set to 1.22 (aligned across go.mod, README, and CI)
- **CI/CD** - Updated GitHub Actions to latest versions (setup-go@v5, golangci-lint-action@v6)
- **README** - Complete rewrite with accurate feature matrix, examples, and architecture overview

### Fixed
- **Build error** - Added missing `privateDictionary` variable that prevented compilation
- **Deprecated API** - Replaced `ioutil.TempFile` with `os.CreateTemp` in tests
- **VM validation** - Implemented full Value Multiplicity range validation (1-n, 2-2n, etc.)
- **External codecs** - JPEG-LS, JPEG-2000, JPEG Lossless now return proper errors instead of nil
- **Waveform serialization** - Implemented sequence serialization in filewriter

## [1.0.0] - 2024-10-24

### Added

#### Core Features
- **27 Specialized DICOM Modules** organized by responsibility
  - **Core I/O**: filebase, filereader, filewriter, fileutil, fileset
  - **Data Model**: dataset, dataelem, tag, element, sequence, uid, valuerep, values, multival
  - **Encoding**: charset (30+ encodings), compress, encaps
  - **Imaging**: pixels, overlays, waveforms
  - **Clinical**: sr (structured reports)
  - **Serialization**: jsonrep (DICOM JSON Model)
  - **Infrastructure**: config, errors, hooks, util
  - **CLI**: 7 commands (show, info, convert, codify, tag-doc, help, version)

#### DICOM Standards Compliance
- DICOM PS3.5 - Data Structures and Encoding
- DICOM PS3.6 - Data Dictionary (5,000+ standard tags)
- DICOM PS3.10 - Media Storage and File Format
- ISO 2022 - Character set escape sequences
- DICOM JSON Model (Part 18)

#### Key Features
- Thread-safe operations with `sync.RWMutex`
- O(1) tag dictionary lookup
- Streaming I/O for large files
- Buffer pooling for reduced allocations
- Pixel data extraction (8/16/32-bit, multi-frame)
- Windowing and color space conversion
- Decompression: DEFLATE, RLE, JPEG
- Physiological waveform support (ECG, EEG)
- Structured report handling with coded concepts
- Overlay management with ROI analysis
- Hook/plugin system for extensibility
- 30+ international character encodings

### License

MIT License - See [LICENSE](./LICENSE) for details.

---

[1.1.1]: https://github.com/amrshadid/go-dicom/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/amrshadid/go-dicom/compare/v1.0.0...v1.1.0
[Unreleased]: https://github.com/amrshadid/go-dicom/compare/v1.1.1...HEAD
[1.0.0]: https://github.com/amrshadid/go-dicom/releases/tag/v1.0.0
