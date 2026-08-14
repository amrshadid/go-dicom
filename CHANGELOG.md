# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Everything here came from reading the documentation against the code. Three
defects turned up in the process, all of the shape 1.4.0 was about: prose and
tests agreeing with each other while the code did something else.

### Changed

**The library no longer writes to your stderr uninvited.**

`network` reported through `log.Printf` at 49 call sites, onto the standard
logger, which a consumer cannot redirect or silence. `DefaultLogger` existed,
was documented as "silent by default", and had 3 call sites against those 49 —
so a program embedding an SCP got association errors, rejections and abandoned
sub-operations on its own stderr, and `DefaultLogger.SetLevel` did nothing about
it because nothing consulted it.

All 49 now go through `DefaultLogger`, which writes to `config.Logger`. One
`config.SetLogger` call therefore controls everything this library emits, and
messages carry a `component=network` attribute so a shared logger can filter the
network layer in or out.

Three notes for existing callers:

- **`DefaultLogger` now defaults to `LogLevelWarn`, not `LogLevelSilent`.** Every
  one of the 49 messages was error- or warning-level and previously printed
  unconditionally, so this preserves what you actually saw while making it
  controllable. `network.SetDefaultLogLevel(network.LogLevelSilent)` reports
  nothing; `config.SetLogger` redirects it.
- **`NewLogger(level, nil)` now means config.Logger** rather than stderr
  directly. config.Logger writes to stderr by default, so the observable
  behaviour is unchanged unless you have replaced it.
- **`Logger.SetOutput(nil)` restores** the default of writing through
  config.Logger, which previously had no way to be expressed.

`LogLevel` gained a `String` method, and `DebugLogger()` still writes to stderr
directly — config.Logger's own level would otherwise discard the debug messages
it is called to reveal.

A test reads this package's source and fails on any direct import of `log`,
since a behavioral test cannot catch one new `log.Printf` in a branch it does not
reach. Another runs an SCP with `os.Stderr` swapped for a pipe and asserts the
error paths report to `config.Logger` and write nothing to the process's stderr.

The corrections to what the documentation claimed:

- **The README no longer contradicts itself about codec support.** It claimed
  JPEG-LS and JPEG Lossless "have no bundled decoder" one paragraph after a table
  correctly marking both as decoding. Both have had pure-Go decoders, registered
  at init, since 1.3.0. JPEG 2000 is the only syntax that does not decode, and
  the section now says so once.

- **`doc.go` no longer advertises a limitation that was fixed.** It named
  C-MOVE and C-GET not performing C-STORE sub-operations as the most significant
  known gap; both have been implemented since 1.3.0, and `CONFORMANCE.md` §2.3
  and the README parity table both already said so. This is the module's landing
  text on pkg.go.dev.

- **`network/doc.go` claimed data sets are not transcoded between transfer
  syntaxes.** They are — `transcodePixelData` is wired into `datasetcodec.go` and
  covered by `transcoding_test.go`. The real limitation, already stated correctly
  in `CONFORMANCE.md` §8.2 and the README, is that RLE Lossless is the only syntax
  pixel data is compressed *to*.

- **`compress` no longer describes its own decoders as placeholders.** The
  JPEG-LS and JPEG Lossless implementation guides told the reader to install
  libcharls or libjpeg-turbo, uncomment a CGO block and rebuild with
  `CGO_ENABLED=1` — for codecs this package decodes in pure Go, and via a CGO
  path that has never existed in this module. `TestGetImplementationGuide`
  asserted the same wrong claim, requiring the JPEG-LS guide to name libcharls,
  so the guides and the test agreed with each other and not with the code. The
  guides now state what is bundled and how to substitute a decoder, and the test
  is pinned to the registry rather than to a hardcoded library name, so bundling
  a JPEG 2000 decoder will fail it until its guide is rewritten.

  `JPEGLSDecoderSkeleton` and `JPEGLosslessDecoderSkeleton` are deprecated: they
  have no fields, no methods, and never had an implementation behind them.

- **`CONFORMANCE.md` §8.1 said 42 of pydicom's 49 decodable files matched.** It is
  43, and the 6 that do not are all JPEG 2000 — verified by running
  `TestPixelsAgainstWholePydicomCorpus` against the corpus rather than by
  recounting the claim.

- **`qrscp` now defaults `-output` to `./received`**, matching `storescp`. It
  defaulted to `./dcmstore`, which created a directory named after a Go import
  path and read as a package of this module that was missing.

- **`make lint` names the linter version CI uses.** `.golangci.yml` is a v2
  configuration and a v1 binary refuses it outright rather than degrading, so the
  target's `@latest` install instruction left a local run unable to lint at all.

### Fixed

- **`DeferredPixelDataReader.Get` always failed.** Its load path was a stub
  returning "file loading not implemented in this stub", so the exported reader
  could never read anything, while `fileutil/doc.go` documented it with a working
  example. `Load` also never set `data` or `loaded` on that branch, and `Get`
  read `loaded` without holding the mutex — a data race under concurrent use.

  The test did not catch it because it called the constructor and `IsLoaded()`
  and stopped there, never calling `Load` or `Get`.

  `loadFromFile` now seeks, reads the extent with `io.ReadFull` and caches it,
  bounded twice before allocating: against `MaxDeferredPixelDataLength` (1 GiB),
  since the length reaches this code from an element header and is
  attacker-controlled, and against the file's actual size — so a declared length
  longer than the file is refused rather than yielding a short buffer the caller
  cannot tell is short. Tests cover a non-zero offset, the cached second access,
  and refusal of an over-long length, an offset past the end, a length past the
  limit, a negative offset, a zero length, and a missing file.

- **`make build` stamped version 1.1.0** into every locally built binary. The
  Makefile held a `VERSION` literal that was not updated for 1.2, 1.3 or 1.4, and
  `-ldflags "-X main.Version=$(VERSION)"` wrote it over the correct default in
  `main.go`. Released binaries were never affected — `build-release.yml` stamps
  the git tag — which is why it went unnoticed for three releases. The Makefile
  now derives the version from `main.go`, so there is no third copy left to
  drift.

## [1.4.0] - 2026-08-01

Correctness release. Everything below was found by running this library against
pydicom's test corpus or a live peer — not by its own tests, which passed
throughout.

### Fixed

- **Four N-DIMSE requests named their target with the wrong element.** N-GET,
  N-SET, N-ACTION and N-DELETE identify what they act on with Requested SOP
  Instance UID `(0000,1001)`; this library sent Affected SOP Instance UID
  `(0000,1000)` — the element for messages that create or report on an instance
  — and `(0000,1001)` did not exist in the codebase at all. Its own SCP read
  back the same wrong tag, so every test passed while no other implementation
  could see a target: pynetdicom answered a well-formed N-ACTION with "Received
  unexpected N-ACTION service message" and aborted the association. Found by
  running against pynetdicom, which now happens in CI.

- **Multi-frame images reported a single frame.** `NumberOfFrames` is IS, and
  PS3.5 §6.2 pads a value to an even length with a trailing space, so `"2 "`
  failed `strconv.Atoi` and the count silently fell back to its default of 1.
  For compressed data that meant every frame after the first was discarded with
  no error at all. Text values now have their padding stripped, as the standard
  says they should.
- **The deflated transfer syntax UID was wrong** in `compress`, recorded as
  `1.2.840.10008.1.2.4.1` — a UID in the JPEG arc that is not a transfer syntax.
  Real deflated files fell through to "unknown transfer syntax".
- **The RLE encoder produced frames nothing could read.** It emitted no segment
  header, and this package's decompressor accepted that because it had the
  matching defect. Its PackBits runs were also capped out of range: a replicate
  run at 256 emitted a control byte meaning a two-byte literal, and a literal
  run at 129 emitted the no-op marker.

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

- **Text was returned as stored bytes, so most of the world's names came back as
  mojibake.** Decoding was reachable only through `DecodePersonName` and
  `DecodeTextValue`, so a caller who did not know to ask got Greek, Hebrew,
  Japanese or plain accented Latin as raw bytes. Two of pydicom's seventeen
  charset fixtures matched its reading; all seventeen do now. Values are decoded
  to UTF-8 on read and Specific Character Set is rewritten to `ISO_IR 192` so the
  data set stays self-consistent, item-scoped character sets included.

- **A data set with no file meta header read as empty, with no error.** PS3.5
  §10.1 makes Implicit VR Little Endian the default, and that was assumed. An
  explicit VR stream read that way takes the VR characters as part of the length
  — `(0008,0005) CS 10` becomes a length of 676675 — which runs past the end of
  the file, so the element is dropped and every one after it with it. pydicom
  reads 24 elements from each of its two headerless fixtures; this read none. The
  encoding is now taken from the first element.

- **A truncated sequence item discarded the whole sequence.** The complete items
  before it were thrown away to punish a defect they had no part in;
  `DICOMDIR-nooffset` lost 51 good records because its 52nd is cut short.

- **De-identification covered 38 of the 655 attributes PS3.15 names.** Measured
  across pydicom's corpus, 60 identifying attributes kept their original values
  in files reported as de-identified — Patient's Sex in 49 of 69 files, Age in
  25, Weight in 14. Sequences were never descended into, attributes named by a
  range of groups (curve and overlay data) could not be looked up at all, and the
  UID action silently did nothing when it met a sequence, leaving every
  Referenced SOP Instance UID intact in 18 files — each one a link from the
  de-identified object back to its original. Zero remain.

- **`fileset` searched nothing and counted wrongly.** `FindByModality`,
  `FindByPatient`, `FindByStudyInstanceUID` and `FindBySeriesInstanceUID` ignored
  their argument and returned every record; `GetStatistics` reported the file
  count as the patient, study and series counts; `ScanDirectory` and `AddFile`
  never parsed the files they listed, so nothing above them had anything to work
  with; and `GenerateDICOMDIR` returned an empty data set.

- **The Huffman table builder dropped a bit at every empty code length.**
  Canonical codes lengthen at each length, including one no code has. Skipping
  that shift left every longer code a bit short, so the table decoded a different
  symbol than the encoder wrote. It needs an interior gap to show and no lossless
  fixture had one; JPEG Lossless was wrong on any stream whose tables skip a
  length.

- **Byte order was not swapped for the 64-bit value representations**, so a big
  endian file carrying an SV, UV or OV kept its values in the wrong order and
  said nothing about it.

- **NIfTI export produced a file no reader could open.** The header was 348 zero
  bytes with the magic string at the end, so `sizeof_hdr` was 0 and nibabel
  answered "Cannot work out file type" while the command reported success. The
  pixel data was the stored value rather than the decoded one, so a compressed
  instance contributed a codestream described as an array of samples.

### Added

- **A DICOM Conformance Statement** ([CONFORMANCE.md](./CONFORMANCE.md)), in the
  structure of PS3.2: SOP classes per role, transfer syntaxes negotiated versus
  those readable from a file, extended negotiation, configuration, character
  sets, enforced limits, and a plainly stated list of limitations. Every UID and
  exported symbol it names was checked against the code.

  One thing it makes explicit that was easy to miss: the SCP negotiates only
  Implicit and Explicit VR Little Endian by default. A big endian, deflated or
  compressed data set is read correctly from a file but is not accepted on the
  wire unless the application calls `SetSupportedTransferSyntaxes`.

- **Storage Commitment (PS3.4 Annex J)**, as both SCU and SCP. An SCU asks a
  peer to take permanent responsibility for instances it has already sent, so it
  can delete its own copies:

      resp, err := scu.RequestStorageCommitment(ctx, &network.StorageCommitmentRequest{
          TransactionUID: network.GenerateUID(),
          Instances:      refs,
      })
      result, err := scu.ReceiveStorageCommitmentResult(ctx)

  An SCP provides it by implementing `StorageCommitmentProvider`, or with the
  `StorageCommitmentHandler` convenience type. A handler that does not implement
  it causes requests to be **refused** rather than silently accepted — accepting
  tells the requestor it may delete its only copy.

  The event type is derived from the result rather than passed in, so a report
  cannot claim everything succeeded while listing failures.

  *Verified against pynetdicom*, which reads the transaction UID, action type,
  and every instance reference. That check runs in CI.

- **`commitscu` CLI command**, and **`network.GenerateUID()`** for minting UIDs
  under the UUID-derived arc (`2.25`, ITU-T X.667), which needs no registered
  root.

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

- **RLE Lossless pixel data decodes.** `Dataset.PixelArray()` now decompresses
  encapsulated pixel data instead of handing it to the sample parsers as though
  it were raw, which failed on every compressed file with "insufficient pixel
  data at frame 0, row N, col M".

  The RLE decoder itself was rewritten. It had ignored the 64-byte segment
  header (PS3.5 §G.5) and PackBits-decoded the whole frame as one stream,
  treating the header's offsets as control bytes — 8736 bytes out of
  `MR_small_RLE.dcm` where 8192 is correct, and not pixel data at any offset.
  Segments are now decoded separately and interleaved, since each sample is
  split across one segment per byte, most significant first.

  *Verified against pydicom in both directions:* `MR_small_RLE.dcm` and both
  frames of `SC_rgb_rle_2frame.dcm` decode byte for byte to what pydicom reads,
  and pydicom decodes a frame this library encoded back to the original pixels.
  `MR_small_RLE.dcm` also decodes to exactly the values of its uncompressed
  twin `MR_small.dcm`. These comparisons run in CI.

- **`Dataset` carries its transfer syntax.** `SetTransferSyntaxUID` and
  `TransferSyntaxUID`, populated by `filereader`. Whether PixelData is raw or
  encapsulated, and which codec compressed it, are properties of the transfer
  syntax; the meta header is not part of the data set, so a `Dataset` had no way
  to learn how its own pixels were encoded.

- **Deflated Explicit VR Little Endian files can be read and written.**
  `filereader` never inflated, so `image_dfl.dcm` parsed to **0 elements**; it
  now parses to 29, matching pydicom, with its pixel data decoding correctly.

  Writing was equally broken in the other direction: `filewriter` ignored the
  transfer syntax and wrote an uncompressed body, so a file declaring
  `1.2.840.10008.1.2.1.99` could be read by nothing — including this library,
  whose reader inflates on the strength of that declaration and failed with
  "flate: corrupt input before offset 5". *Verified:* a file written this way is
  read back by **pydicom** with matching element values and identical pixel
  data.
- **Patient/Study Only query/retrieve information model**
  (`1.2.840.10008.5.1.4.1.2.3.x`). Retired in the current standard but still the
  only model some archives offer. Added to the SCP's default contexts and to the
  SCU's Find/Move/Get fallback chain as a third rung.
- `dataelem.SwapByteOrder` and `dataelem.IsByteOrderSensitive` — one byte-order
  implementation shared by the reader and the writer, so they cannot drift apart.
- `compress.InflateLimitFor`, `MaxInflateRatio`, `MinInflateAllowance`.

- **RLE Lossless compression on send.** Sending over a context that negotiated a
  compressed syntax previously failed outright. Pixel data is now transcoded in
  both directions as the negotiated context requires — from native pixels, or by
  decoding a compressed source and re-encoding. RLE remains the only syntax this
  library compresses to; every other compressed target still fails rather than
  putting bytes on the wire described as something they are not.

- **12-bit JPEG Extended decoding**, in pure Go. The standard library handles
  SOF1 frames and rejects only the precision, which is the depth the transfer
  syntax exists to carry.

- **UPS Watch** — subscriptions and N-EVENT-REPORT. All three targets of PS3.4
  CC.2.3 are supported, including the Global and Filtered Global instances, whose
  subscribers are resolved when an event happens rather than expanded when the
  subscription is made, so they cover steps created afterwards.
  `Server.ReportUPSEvent` delivers events over an association the SCP opens back
  to the subscriber. A subscriber that cannot be reached does not fail the
  N-ACTION: the transition is already stored, and refusing it would leave the SCP
  and the performer disagreeing about who owns the step.

- **The DICOM JSON Model of PS3.18 Annex F** — `ToDICOMJSON`, `FromDICOMJSON`
  and their string forms, with bulk data URIs. The README claimed this and the
  `jsonrep` package's documentation claimed to implement it; `jsonrep` is a
  struct of twenty-five named fields and cannot represent an arbitrary data set,
  and `Dataset.ToJSON` produces a readable rendering nothing outside this library
  can consume. Both now say what they are. Verified against pydicom's `to_json`
  across its corpus.

- **DICOMDIR reading and writing.** Directory records are stored in one flat
  sequence and describe a tree linked by byte offsets, so the tree is built from
  the offsets rather than from record order — a conforming file may store them in
  any order, which is what `DICOMDIR-reordered` exists to demonstrate. Writing
  computes those offsets by laying the file out twice and re-reads its own output
  before returning it, because a file with wrong offsets is worse than no file: a
  reader accepts it and follows it into a tree that is not there.

- **The SR content tree of PS3.3 C.17** — `ReadContentTree` and
  `WriteContentTree`, including by-reference relationships, whose Referenced
  Content Item Identifier is UL rather than the text most multi-valued attributes
  use. The package could not read a content item: pydicom sees 28 in
  `test-SR.dcm` and this saw none.

- **SV, UV and OV**, the 64-bit value representations DICOM added in 2018. An
  unrecognized VR is not a small problem in explicit encoding, because the VR
  decides the shape of the header.

- **Private attributes take their VR from the shipped vendor dictionary.** The
  6883-attribute private dictionary was never consulted. A lookup requires the
  file's own private creator, since PS3.5 7.8.1 lets a vendor claim any block
  from 0x10 to 0xFF and the same attribute is at a different tag in every file.

- **nibabel joins the interoperability suite**, alongside pynetdicom, dcmtk and
  pydicom.

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

- **The JPEG-LS, JPEG 2000, and JPEG Lossless errors no longer give advice that
  does not work.** They named a C library and told the caller to rebuild with
  `CGO_ENABLED=1`; there is no CGO implementation in this module, so following
  the instruction produced the same error. The messages, and the installation
  text `GetExternalCompressionStatus` returns, now say plainly that no decoder
  is bundled and show how to register one. An example demonstrates it.

- The README transfer syntax table separated data set support, pixel decoding,
  and network transfer into distinct columns. A compressed syntax marked
  "Read/Write" had implied its pixels were usable when only the data set parsed.
  No compressed syntax claims pixel decoding — including RLE, whose decoder
  returns 8736 bytes where 8192 is correct.
- CI runs on Node 24 actions throughout, and lints with golangci-lint v2.

### Known limitations

Six, all documented in CONFORMANCE section 8. Two are gaps; four are deliberate,
and removing them would make the library worse or wrong.

- **JPEG 2000 has no bundled decoder.** No implementation ships one in pure
  language — pydicom needs pylibjpeg or GDCM, dcmtk needs extra modules. Supply
  one through `compress.GetExternalRegistry().RegisterExternalDecoder`;
  `examples/jpeg2000` is a working decoder verified sample-for-sample against
  pydicom.
- **RLE Lossless is the only syntax this library compresses to.** Decoders exist
  for JPEG Lossless, JPEG-LS and 12-bit JPEG Extended; the encoders do not.
- `PixelArray` flattens color samples into the column dimension, so a 100x100
  RGB frame is reported as 100x300. The values and their order are correct.
  `PixelArrayBySample` returns the four-dimensional shape.
- Samples are returned in the color space the Photometric Interpretation names,
  so a `YBR_FULL` instance yields YBR rather than RGB — the same as pydicom.
  Converting while the attribute still says YBR would have the next reader
  convert again.
- Compressed transfer syntaxes are not negotiated by default, as in pynetdicom.
  Pass `AllTransferSyntaxes()` to accept them.
- A C-FIND, C-GET or C-MOVE handler that returns a complete result set cannot
  observe a cancellation, having finished before the first result was sent. The
  streaming interfaces — `CFindStreamer`, `CGetStreamer`, `CMoveStreamer` —
  receive a context that is canceled when a C-CANCEL naming the message arrives.

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
