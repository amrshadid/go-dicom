# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Correctness release. Everything below was found by running this library against
pydicom's test corpus or a live peer — not by its own tests, which passed
throughout.

### Fixed

- **Explicit VR Big Endian files could not be read, and files written as big
  endian could not be read by anything else.** Three separate defects:
  - The short-form value length was assembled by hand as little endian at two
    sites in `filereader`, ignoring the byte order the reader had already been
    given. `MR_small_bigendian.dcm` parsed to **1 element**; pydicom reads 72.
  - Values themselves stayed in big endian once the lengths were fixed, so
    BitsAllocated of 16 read back as 4096 — the same bits reversed. Values are
    now normalised to little endian as they are parsed, so `Dataset` needs no
    byte-order concept and everything downstream can assume one order.
  - The write side had no inverse. A file round-tripped through the library
    declared big endian while holding little endian values, and `filewriter`
    wrote the file meta header in the data set's byte order although PS3.10
    §7.1 requires it always be Explicit VR Little Endian — producing a header
    whose first tag read back as `(0200,0000)`.

  *Verified:* `MR_small_bigendian.dcm` now parses to 72 elements and decodes to
  pixels identical to pydicom's; a file written by this library and re-read in
  pydicom matches on both element values and pixel data.

- **C-CANCEL aborted the association.** `SCU.Cancel` sent a well-formed
  C-CANCEL-RQ and the SCP had no case for it, so it fell to the default branch,
  which aborts. Cancelling did not merely fail — it tore down the connection and
  discarded results the requestor had already received. It is now dispatched and
  the association survives.

- **The `file` input to the coverage upload was silently ignored** in CI, and
  `govulncheck` was installed from `@latest`, which broke the build when the
  tool raised its own Go requirement.

### Added

- **Compressed frames can be extracted.** `filereader` discarded the Basic
  Offset Table and item headers of encapsulated Pixel Data and concatenated the
  fragment payloads, on the reasoning that frame boundaries would be recovered
  later. They could not be — the `encaps` package recovers them by parsing that
  structure, so `ExtractEncapsulatedFrames` failed with "failed to parse basic
  offset table" on every compressed file, and multi-frame images could not be
  split at all. `PixelData` now holds the encapsulation exactly as it appears in
  the file, matching what pydicom exposes.

  *Verified against pydicom:* `MR_small_RLE.dcm` gives one 6108-byte fragment
  from 6128 bytes of pixel data, and `SC_rgb_rle_2frame.dcm` splits into two
  664-byte fragments with a two-entry offset table — identical in both cases.

  This does not decode anything: `PixelArray()` still cannot decompress. It is
  the step that had to come first.

- **Deflated Explicit VR Little Endian files can be read.** `filereader` never
  inflated, so `image_dfl.dcm` parsed to **0 elements**; it now parses to 29,
  matching pydicom, with its pixel data decoding correctly. Writing is *not*
  supported — `filewriter` does not deflate, and the README says so.
- **Patient/Study Only query/retrieve information model**
  (`1.2.840.10008.5.1.4.1.2.3.x`). Retired in the current standard but still the
  only model some archives offer. Added to the SCP's default contexts and to the
  SCU's Find/Move/Get fallback chain as a third rung.
- `dataelem.SwapByteOrder` and `dataelem.IsByteOrderSensitive` — one byte-order
  implementation shared by the reader and the writer, so they cannot drift apart.
- `compress.InflateLimitFor`, `MaxInflateRatio`, `MinInflateAllowance`.

### Security

- **A deflated file could force an allocation 50,000x its own size.** The 256 MiB
  decompression-bomb ceiling bounded the output but let the attacker choose the
  cost: a 300 KB file produced a measured **603 MiB peak heap** before being
  rejected, because `io.ReadAll` grows its buffer by doubling. The allowance is
  now the smaller of the ceiling and 1000x the compressed size, with an 8 MiB
  floor — DEFLATE reaches its theoretical maximum ratio on genuinely blank
  medical images, so a ratio alone would reject an all-black frame. Both the
  file and network paths share one implementation.
- **The security policy named a reporting channel that did not exist.** It
  forbade public issues and asked for an email that appeared nowhere in the
  repository. Reports now go through GitHub private vulnerability reporting,
  which has been enabled.

### Changed

- The README transfer syntax table separated data set support, pixel decoding,
  and network transfer into distinct columns. A compressed syntax marked
  "Read/Write" had implied its pixels were usable when only the data set parsed.
  No compressed syntax claims pixel decoding — including RLE, whose decoder
  returns 8736 bytes where 8192 is correct.
- CI runs on Node 24 actions throughout, and lints with golangci-lint v2.

### Known limitations

- No compressed pixel data decodes, RLE included. Frames can now be separated,
  but the RLE decoder itself is still wrong — it returns 8736 bytes where 8192
  is correct — and `PixelArray()` does not yet route compressed data through a
  decoder. Compressed instances parse, store, and transfer with their pixel data
  intact.
- Deflated files can be read but not written.
- Handlers still cannot observe a C-CANCEL and stop early; that needs streaming
  handler signatures.

## [1.3.0] - 2026-07-27

> **Upgrade from 1.2.0 without delay.** The `ItemTag` constant in 1.2.0 was wrong, so the
> nested sequence parsing introduced in that release failed on every real DICOM file
> containing a sequence. 1.2.0 should be skipped.

### Added

- **C-GET sub-operations** — a C-GET previously invoked the handler and returned a status
  but transferred nothing. The SCP now sends matching instances back as C-STORE
  sub-operations over the same association (PS3.4 Annex C.4.3), each on the presentation
  context negotiated for its own SOP Class, with pending responses carrying the
  remaining/completed/failed/warning counts. The SCU dispatches incoming C-STORE-RQ
  messages to `SCUConfig.OnCStore` and acknowledges each one.
  - `CGetResponse.Instances` supplies the instances to transfer
  - `SCUConfig.OnCStore` receives them on the requesting side
  - `qrscp` retains received datasets and serves them, making it a working
    in-memory query/retrieve server
- **C-MOVE sub-operations** — the SCP now opens an association to the move destination and
  sends the matching instances there as C-STORE sub-operations (PS3.4 Annex C.4.2),
  reporting progress back to the requestor. This completes query/retrieve: C-FIND,
  C-GET, and C-MOVE all work as both SCU and SCP.
  - `CMoveResponse.Instances` supplies the instances to move
  - `QueryRetrieveHandler.OnMoveInstances` returns them from a handler; the older
    `OnMove`, which could not transfer anything, is deprecated but still honored
  - `SCPConfig.MoveDestinations` and `SCPConfig.ResolveMoveDestination` resolve a
    destination AE title to an address; an unresolvable title is answered with
    `StatusMoveDestUnknown`
  - `qrscp -move-dest AETITLE=host:port` configures destinations from the CLI
- **Sequence writing in `filewriter`** — `DataElement.Items` holds nested
  `SequenceItem` values, closing the read → write → read round trip. Items are written
  with explicit lengths and implicit-style item headers as PS3.5 Section 7.5 requires.
- **Raw DICOM data sets without a file meta header** — `ReadDICOMFile` required the
  128-byte preamble and DICM prefix, so a raw stream, which is what modalities produce and
  what travels on the network, could not be read despite the README listing it as
  supported. The reader now detects which form the stream is and falls back to implicit VR
  little endian per PS3.5 Section 10.1, recording the outcome in `DICOMFile.HasPreamble`.
- **`DICOMFile.MetaElements`** — the group-0002 elements as they appeared in the file,
  for callers that need to display the header verbatim.
- **Interoperability testing against pynetdicom and dcmtk** — `scripts/interop-test.sh`
  plus a CI job. Exercises C-ECHO and C-STORE in both directions and C-GET
  sub-operations, using pydicom's `CT_small.dcm` as the fixture and pydicom as the
  verifier rather than this library's own reader. Fails when no third-party peer is
  available, so a broken install cannot pass by skipping.

### Fixed

- **The CLI parsed files with a second, broken parser** — `show`, `info`, `convert`, and
  `codify` used a parser in `cli/helpers.go` separate from `filereader`, which received
  none of this cycle's fixes. It read the file in 64 KiB chunks and parsed each
  independently, so an element straddling a boundary desynchronized the stream: on a
  268 KB file where pydicom reports 258 elements it printed roughly 38 and ended with an
  invented element whose VR was two arbitrary bytes. It also never descended into
  sequences, and classified SQ and UT as short-form VRs. The CLI now uses `filereader`,
  reports 269 elements for the same file, and indents sequence contents by nesting depth.
- **`ItemTag` was `0xFFFE0000`, not `0xFFFEE000`** *(critical)* — the constant was missing
  a digit and decoded as (FFFE,0000). Sequence parsing, added in 1.2.0, therefore failed
  on every real DICOM file containing a sequence, rejecting the correct item tag as
  unexpected. The unit tests passed because they built their fixtures with the same wrong
  constant.
- **`tag.FromBytes` and `tag.ToBytes` transposed group and element** — both treated the
  4-byte tag as a single uint32 rather than two consecutive 16-bit values. Undetected
  because the only test case was PatientName (0010,0010), where group equals element, and
  because the two functions were each other's inverse.
- **Sequences were lost over the network** — `EncodeDataset` skipped sequence elements,
  and `storescu` built its dataset from `elem.Value`, which is nil for a sequence. An
  instance sent to a peer arrived with its sequence present but empty.
- **An empty data set sent no PDU at all** — `SendPData` looped over the payload, so a
  zero-length data set produced nothing after the command had already announced one,
  leaving the peer blocked until the DIMSE timeout. Reachable with any keyless C-FIND or
  C-GET identifier.
- **`QueryRetrieveHandler.OnGet` results are now transferred** rather than only counted.

## [1.2.0] - 2026-07-26

### Security

- **SCP crash on malformed association request** — a peer proposing a presentation
  context with zero Transfer Syntax sub-items caused an index-out-of-range panic in
  `NegotiatePresentationContexts`, terminating the whole server process. Rejected
  contexts now fall back to a valid Transfer Syntax UID as PS3.8 9.3.3.2 requires.
- **Unbounded allocation from the PDU length field** — `DecodePDU` allocated a buffer
  from a peer-controlled 32-bit length before reading any payload, so a declared
  ~4 GiB PDU forced a 4 GiB allocation. Declared lengths are now capped at
  `MaxPDULengthLimit` (128 MiB) and PDV lengths are validated against the enclosing PDU.
- **Unbounded allocation from element length fields** — the file reader allocated
  directly from the declared Value Length, so an element claiming 0xFFFFFFFF (or any
  oversized value) triggered a multi-gigabyte allocation. Lengths above 16 MiB are now
  verified against the bytes actually remaining in the stream.
- **Out-of-bounds reads in extended negotiation decoders** — `DecodeSCPSCURoleSelection`
  and `DecodeUserIdentityNegotiation` trusted peer-supplied length fields. Both now
  bounds-check against the sub-item before slicing.
- **Truncated DIMSE command values** — `DecodeCommandDataset` used `Read` instead of
  `io.ReadFull`, silently producing zero-padded values on a short read, and did not
  validate declared lengths against the remaining buffer.

### Added

- **Nested sequence (SQ) parsing** — the file reader now parses sequences recursively
  instead of stopping at the first delimiter. Supports defined- and undefined-length
  sequences and items, sequences under implicit VR (VR recovered from the dictionary),
  empty sequences, and encapsulated (fragmented) pixel data. Nesting is bounded by
  `MaxSequenceDepth` (64) so a crafted file cannot drive unbounded recursion.
  - `DataElementValue.Items` holds parsed sequence items
  - `DataElementValue.UndefinedLength` reports delimited elements
  - `SequenceItemValue` holds the elements nested within one item
- **`DICOMFile.GetDataset()`** — converts a parsed file into a `Dataset`, materializing
  nested sequences as `sequence.Sequence` values holding child `Dataset`s with parent
  pointers wired. This API was documented in the README but did not exist.
- **`DICOMFile.Warnings`** — non-fatal parse issues (unknown tags, retired tags, VR
  mismatches, truncated meta elements) are collected instead of printed.
- **Extended negotiation on the wire** — async operations window, SCP/SCU role
  selection, user identity, and SOP Class extended negotiation are now encoded into
  and parsed from the A-ASSOCIATE User Information item. Previously these types
  existed as a codec that nothing called.
  - `SCUConfig.ExtendedNegotiation` proposes items during association
  - `Association.RequestAssociationWithNegotiation` for direct control
  - `Association.PeerUserInformation()` and `Association.RoleSelectionFor()` inspect results
  - The SCP echoes back role selections for supported SOP Classes
  - `DecodeSOPClassExtendedNegotiation` completes the codec (the encoder had no counterpart)
- **`Association.TransferSyntaxFor(contextID)`** — reports the syntax negotiated per
  presentation context.
- **`SCU.Association()`** — exposes the active association for inspecting negotiated
  parameters.
- **Exported dataset codec** — `EncodeDataset` / `DecodeDataset` handle implicit VR LE,
  explicit VR LE, explicit VR BE, and Deflated Explicit VR LE.

### Fixed

- **Written DICOM files were not valid DICOM** *(critical)* — `WriteFileMetaInfo` used the
  wrong group-0002 tags: the SOP Class UID went to (0002,0010) (Transfer Syntax UID), the
  SOP Instance UID to (0002,0012) (Implementation Class UID), and so on down the list.
  Every file this library produced was unreadable by conforming DICOM software, and could
  not even be read back by this library's own reader. Tags now follow PS3.6:
  (0002,0002) SOP Class, (0002,0003) SOP Instance, (0002,0010) Transfer Syntax,
  (0002,0012) Implementation Class, (0002,0013) Implementation Version,
  (0002,0016..0018) AE titles. The Type 1 (0002,0001) File Meta Information Version is
  now always written.
- **Odd-length values corrupted written files** — `WriteDataElement` wrote values without
  padding them to even length, and a single odd value misaligns every element after it.
  Padding is now applied centrally in the writer using the VR's designated character,
  rather than being left to callers.
- **Reader looked for AE titles at the wrong tags** — (0002,0100..0102) instead of
  (0002,0016..0018); the former range belongs to Private Information attributes.
- **`QueryRetrieveHandler.OnGet` was never called** — the struct exposed the field but had
  no `HandleCGet` method, so setting it silently did nothing and C-GET fell through to
  `BaseHandler`'s not-implemented error.
- **`SCPConfig.MaxAssociations` was ignored** — the field was documented but never read, so
  the server accepted unbounded concurrent associations. The limit is now enforced, and
  connections over it receive an A-ASSOCIATE-RJ with reason "local-limit-exceeded"
  rather than a silently dropped socket.
- **C-STORE/C-FIND data sets ignored the negotiated transfer syntax** — data sets were
  always encoded as Implicit VR Little Endian regardless of what was agreed. Since
  Explicit VR Little Endian is proposed first by default, any peer accepting it
  received an unparsable data set. All DIMSE data set encode/decode paths now use the
  transfer syntax negotiated for the presentation context in use.
- **Odd-length values violated PS3.5 Section 7.1.1** — values are now padded to even
  length with the VR's designated character (NUL for UI and binary VRs, space for text
  VRs). Odd-length values are rejected by conforming PACS implementations.
- **`SCU.NEventReport` reported the wrong message ID** and consumed an extra ID by
  calling `nextMessageID()` a second time to derive `MessageIDRespondedTo`.
- **Library wrote warnings to stdout** — `dataset.Add`, the file reader, and the file
  writer's validation path used `fmt.Printf`. All now route through the configurable
  `config.Logger` (slog, stderr, WARN level), which callers can redirect via
  `config.SetLogger`.

### Changed

- **Version** bumped to 1.2.0

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

## Unversioned — predates 1.1.0

<!-- These entries predate the 1.1.0 release and were never assigned a version.
     They are kept in place rather than folded in, since which release shipped
     each one cannot now be determined from the history.

     This section was previously headed "[Unreleased]", which collided with the
     real Unreleased section at the top of the file: an edit intended for one
     matched both, and release-note extraction had two candidate ranges. -->

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

[1.3.0]: https://github.com/amrshadid/go-dicom/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/amrshadid/go-dicom/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/amrshadid/go-dicom/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/amrshadid/go-dicom/compare/v1.0.0...v1.1.0
[Unreleased]: https://github.com/amrshadid/go-dicom/compare/v1.3.0...HEAD
[1.0.0]: https://github.com/amrshadid/go-dicom/releases/tag/v1.0.0
