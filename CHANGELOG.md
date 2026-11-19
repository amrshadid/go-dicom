# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Which transfer syntax each coder writes is one table** (#534). A send,
  the `compress` command and a frame encoded over DICOMweb each kept their
  own map from a syntax to a `Dataset.Compress…InPlace` method, with which
  of them lose detail and what each takes, so a coder added to one was
  missing from the other two. `dataset.PixelEncoders` and
  `PixelEncoderFor` are that table, with each syntax's name, whether it is
  lossy and its parameter (JPEG quality, JPEG-LS NEAR, a JPEG 2000 rate),
  and all three read it. Nothing they accept, refuse or write changed: for
  every transfer syntax in the dictionary, each of the three gives the same
  answer, message and bytes as before.

- **A query can be matched against a data set** (#515). C-FIND matching
  lived only inside `dcmstore`, unexported and written against its index, so
  a program with entries of its own, a worklist for one, had nothing to call:
  `WorklistHandler` sends whatever `OnWorklist` returns. Package `matching`
  reads a query once (`Compile`) and matches it against any data set, by
  every rule of PS3.4 C.2.2.2: universal, single value, list of UID,
  wildcard, range, sequence matching item by item, so a step at one station
  and a date from another step is not a match, and the two rules nothing
  had: `""` matches an entity where the attribute is empty, and a key of
  more than one value, `CT\MR`, matches either. Both were compared as plain
  text before and never matched. `Response` builds the answer from a match:
  the keys asked for, empty where the entry has none, sequences cut to the
  items that matched and the keys asked for in them, text in UTF-8 with
  ISO_IR 192 declared when it needs it. `dcmstore` now matches through it
  rather than its own code. Over the pydicom corpus and 24 generated
  instances, 192 queries at the four levels give answers byte for byte the
  same as before, except the 12 that use the two new rules, which matched
  nothing before. A text value of LT, ST, UT or UR is one value: split at a
  backslash, a comment `a\b` matched a query for `a` and never itself.

- **An N-GET can ask for the attributes it wants** (#486). An SCP has read
  the Attribute Identifier List (0000,1005) since #380, but `SCU.NGet` could
  not send one, so a requester always asked for every attribute.
  `SCU.NGetAttributes` takes the tags wanted and sends them in that order
  (PS3.7 10.1.2.1.4). `NGet` is unchanged and sends no list. It is a new
  method, not a parameter added to `NGet`, so that a method value or an
  interface written against `NGet` keeps compiling. Against pynetdicom's
  N-GET SCP, `NGetAttributes` with Manufacturer and Device Serial Number
  receives those two of the five attributes, and pynetdicom reports the two
  tags as the list it was given.

- **`modify` takes a binary value from a file, and changes or removes an
  attribute everywhere it occurs** (#439). `-set-file` and `-insert-file`
  (dcmodify's `-mf` and `-if`) give an OB, OW, OF, OD, OL, OV or UN attribute
  the bytes of a file, padded to an even length and checked to be a whole
  number of words; until now every binary value was refused, so a palette or
  a private blob could not be set without writing Go. `-set-all` and
  `-delete-all` (`-ma` and `-ea`) act on an attribute in the data set and in
  every item of every sequence, to any depth, where a path needed one edit
  per item and the item counts. Over pydicom's corpus the results match
  pydicom's element by element, and dcmodify's. No invocation that worked
  before changes; the refusal of a binary value given as text now names the
  new flags.

- **`show --json` and the `-events` log have a written contract.** Two other
  programs read them, and what `schemaVersion` 1 promised was written nowhere.
  `docs/show-json.md` and `docs/events.md` say what each property means and
  what a version number covers, and `docs/show-json.schema.json` and
  `docs/events.schema.json` are JSON Schemas for them. The tests check the
  output of every corpus file, and of real associations over TCP, against the
  schemas, and fail on a property the schemas do not describe. Nothing the
  commands print changed. The `-events` lines carry no `schemaVersion`; the
  document says so and what a future one would mean.

- **Every `-events` line says `"schemaVersion":1`** (#480). A reader could not
  tell a version 1 line from a later one by anything in the line. The
  property is on every line, so a reader that starts following the file
  midway, or reads a file two processes append to, can check each line, and
  it is the last property: a line is the line 1.10.0 wrote with
  `,"schemaVersion":1` before the closing brace, every other property in its
  place, byte for byte. A line without one, as 1.10.0 wrote, is version 1;
  the schema does not require it.

- **Media Creation Management and the two event logging classes are
  verified against a peer** (#370). CONFORMANCE §8.5 said they "still have
  no test against a peer": they had UID constants and presentation context
  helpers, and no message of theirs had reached another implementation.
  Each is now run against pynetdicom in both directions by
  `scripts/interop-test.sh`. Media Creation goes through N-CREATE with a
  Referenced SOP Sequence, N-ACTION to initiate and N-GET of the Execution
  Status. Procedural Event Logging and Substance Administration Logging go
  through N-ACTION to their well-known instances. Each side reports what it
  read. Writing them found two defects in the N-DIMSE primitives, #379 and
  #380, which are recorded separately.

- **`findscu -worklist` queries a Modality Worklist** (#389). A worklist
  could be reached only by naming its UID with `-sop-class`, and that query
  was the patient hierarchy's: it sent a Query/Retrieve Level, which Basic
  Worklist does not have and dcmtk's `wlmscpfs` reports as unsupported, sent
  none of the worklist keys, and printed only top-level values, so the
  scheduled step, which is held in a sequence, was not shown. `-worklist`
  sends the keys a modality asks for (PS3.4 Table K.6-1), the Scheduled
  Procedure Step Sequence among them, with `-modality`, `-date` (a date, a
  range or `today`) and `-station-aet` matched inside it. Naming the model
  by UID now sends the same query. Every `findscu` answer prints its
  sequences, each item indented under its sequence. There is no command
  for UPS, for MPPS or for serving a worklist.

- **`mppsscu` reports a Modality Performed Procedure Step** (#495). The
  library's SCU sends N-CREATE and N-SET, and no command used them, so a
  modality's report that an exam had started and was done could not be
  made from the shell. `mppsscu` sends an N-CREATE that makes a step IN
  PROGRESS. It carries every attribute PS3.4 Table F.7.2-1 requires at
  creation: the patient, the Scheduled Step Attributes Sequence (study,
  accession, requested procedure, scheduled step), the performed step's ID,
  station, start date and time, and modality. Type 2 attributes are present
  and empty when no flag gives them. The step's UID is printed.
  `-complete UID` or `-discontinue UID` sends the N-SET that ends it, with
  its end date and time and, from `-series`, `-protocol` and `-instance`, a
  Performed Series Sequence. `-json` adds a DICOM JSON object's attributes
  over what the flags give. A refusal is printed with its status and exits
  nonzero. Run against pynetdicom's MPPS SCP in `scripts/interop-test.sh`:
  two steps created and ended, their attributes read back by pydicom where
  the flags put them, and a final step and an unknown one refused (0110H,
  0112H). dcmtk has no MPPS tool to compare with.

- **A rendered image goes through its lookup tables** (#334). `RenderFrame`,
  and so `convert --format png|jpeg`, `share` and the burned-in text
  detector, applied Rescale Slope and Intercept and a window and nothing
  else. An image whose Modality LUT or VOI LUT is a table came out as a
  straight ramp, or black, and one marked Presentation LUT Shape INVERSE the
  wrong way round. It now applies the Modality LUT Sequence, the VOI LUT
  Sequence (chosen over a window when a file has both, as pydicom does;
  `--window` still wins), Presentation LUT Shape and the Presentation LUT
  Sequence (PS3.3 C.11). No file in pydicom's corpus has any of these, so
  `scripts/render-lut-fixture.py` writes seven that do, with pydicom's
  picture of each: every pixel is equal. dcmtk's `dcm2pnm` agrees within 1
  on the six it applies; it leaves a Presentation LUT Sequence in an image
  alone. The 59 corpus files that rendered before render to the same bytes.
  An ICC profile is still not applied.

- **Pages for readers coming from pydicom and from dcmtk** (#391). The README
  was the only manual, and nothing said what `dcmread`, `pixel_array`,
  `dcmdump` or `dcm2pnm` are called here. `docs/coming-from-pydicom.md` and
  `docs/coming-from-dcmtk.md` map each familiar call and command to its
  equivalent, and say where there is none; `docs/examples.md` shows the
  common jobs as command lines and as Go. Every Go sample is an `Example`
  function in `examples/commonjobs`, run and checked by `go test`, and
  every command line was run on a file from pydicom's corpus before it was
  written down.

- **`diff` compares two files** (#329). Every check of a file against its
  anonymized or recompressed copy was a throwaway script, and `show` of each
  followed by `diff(1)` is unreadable: the offsets move when the elements do
  not. `go-dicom diff a.dcm b.dcm` compares the file meta group and the data
  set element by element, into sequences and items, and prints the elements
  only one file has and those whose VR or value differ (nothing when there is
  none), or with `--json` a document with `schemaVersion` 1. The exit status
  is the answer, as in `diff(1)`: 0 equal, 1 different, 2 the comparison could
  not be made. Bulk values are read from the two files in chunks and never
  held whole, and report the byte they first differ at; a value is compared
  as what it means, so padding, a character set and the byte order of a number
  or of pixel samples do not make files differ. Big endian Pixel Data is
  reversed at the sample width Bits Allocated gives, as the reader does: in
  OW's two-byte words, a 32-bit RT Dose and its correct little endian copy
  differed from the first byte (#426). `--ignore` (a tag or keyword,
  at every depth), `--ignore-meta` and `--ignore-private` leave elements out.
  `cli.ExitError` lets a command choose its exit status; the others still end
  with 1.

- **An SCP can refuse a caller before anything is negotiated** (#316). An SCP
  accepted every association that reached its port, whatever the calling AE
  title and wherever it came from; a handler learned who was calling only
  after the association was accepted, and could do no more than fail each
  message. `SCPConfig.Admit` is called once per association, after the
  A-ASSOCIATE-RQ is read and before it is answered, with the calling and
  called AE titles, the remote address and, over TLS, the verified
  certificate chains. It admits the caller, or refuses it with the result,
  source and reason it chose from PS3.8 Table 9-21, and a refused caller
  learns nothing of what the SCP supports. dcmtk's `echoscu` and `storescu`
  and pynetdicom print the refusal as it was chosen. It is asked only about
  well-formed AE titles (PS3.5 6.2), given without their padding: a request
  whose calling or called title holds a control character, a backslash or a
  byte outside the default repertoire, or is all spaces, is refused before
  it is asked. It is also given the request's User Identity Negotiation
  item, unchecked. The SCP goes on checking the called AE title
  itself, so a function that only observes does not open the SCP to every
  title; `SCPConfig.AcceptAnyCalledAE` leaves the title to the function, so
  one SCP can answer to several. It has the ARTIM timer to answer, and a
  panic in it refuses that caller without taking the server down. Unset,
  nothing changes.

- **`EVTAssocRejected` says why.** Its `Description` names the result, source
  and reason of the A-ASSOCIATE-RJ. It was empty, so `LoggingEventHandlers`
  logged "Association rejected: SCU -> SCP: " with nothing after the colon.

- **`storescp` and `qrscp` admit only the callers they are given** (#351).
  Both answered anyone who reached the port and named their AE title.
  `-allow` takes calling AE titles, and IP addresses or CIDR ranges, comma
  separated and repeatable, and hands them to `SCPConfig.Admit`. With both
  kinds a caller needs a listed title and a listed address. Any other caller
  is refused before anything is negotiated: an unlisted title as
  calling-AE-title-not-recognized, which dcmtk's `echoscu` prints as `Reason:
  Calling AE Title Not Recognized`, and an unlisted address with no reason
  given, since PS3.8 Table 9-21 has none for an address. The server prints
  its list when it starts and a line for each caller it refuses. A host name
  is not resolved. `-allow` covers DIMSE only: `qrscp -web` has its own
  token.

- **A C-STORE SCP can take the data set as a stream** (#317). `HandleCStore`
  is given a decoded data set, which the SCP built by holding every
  fragment of the message in memory and then decoding it: memory grew with
  the instance, once for the bytes and again for the values, and a handler
  that wrote a file wrote this library's encoding of the data set rather
  than what the peer sent. A handler that also implements `CStoreStreamer`
  is given the command, the transfer syntax of its presentation context,
  and an `io.Reader` over the data set's bytes exactly as received, read
  from the association as the handler reads. A 1 GiB instance is received
  with the heap under 15 MiB, where `HandleCStore` peaks at 278 MiB for an
  eighth of that, and a slow handler slows the peer through TCP.
  `WriteCStoreFile` writes the stream as a Part 10 file, File Meta
  Information built from the command and then the bytes untouched; dcmtk's
  `dcmdump` and pydicom read the files, and their data sets are what
  `storescu` and pynetdicom sent, byte for byte, for implicit and explicit
  little endian and RLE. `MaxDataSetSize` still applies, counted as the
  bytes pass. A handler that does not implement it is served as before.
  A data set that does not arrive whole ends the read with an error that is
  `io.ErrUnexpectedEOF` and never `io.EOF`, by `==` or by `errors.Is`,
  whatever cut it short: a connection the peer closes reports an error that
  wraps `io.EOF`, and a handler testing for the end with `errors.Is` would
  have kept a truncated file as the instance. `WriteCStoreFile` checks the
  UIDs the peer put in the command (PS3.5 9.1) before it writes anything and
  refuses with `ErrInvalidSOPInstanceUID` or `ErrInvalidStoreCommand`, and
  leaves out an AE title that is not one. A C-STORE-RQ that says no data set
  follows is refused with 0xC000 without the handler being called.
  `CompositeHandler` passes the stream to a store handler that implements
  `CStoreStreamer`.

- `CStoreRequest` carries the command's Priority, Move Originator AE Title
  and Move Originator Message ID to the handler, whichever way it takes the
  store. `HandleCStore` was given none of them.

- `filewriter.EncodeFileHeader` returns the preamble, `DICM` and File Meta
  Information that come before a data set, for a writer that has the data
  set encoded already.

- **`dicomdir` writes and lists a DICOMDIR** (#348). `fileset.WriteDICOMDIR`
  and `ReadDICOMDIR` existed, and indexing a directory still meant writing a
  program. `go-dicom dicomdir <directory>` writes the DICOMDIR of the DICOM
  files under it, and `dicomdir --list <DICOMDIR or directory>` prints one as
  a tree, or as JSON with `schemaVersion` 1. A File ID is at most eight
  components of at most eight characters from A-Z, 0-9 and `_` (PS3.10 8.5),
  which `ct.dcm` is not: the command refuses by name, every such file at
  once, and writes nothing. It never renames. `--copy-to <new directory>`
  copies the files there as `DICOM/IM000001` onward and indexes the copies.
  A data set without the file meta information of Part 10 is left out and
  named, as is anything that is not DICOM; an existing DICOMDIR is replaced
  only with `--force`. `fileset.ValidFileID` exports the File ID check.

- **`sr` prints a structured report as a document** (#359). The `sr` package
  reads a content tree and no command printed one; `show` gives the flat
  element list, which does not read as a report. `go-dicom sr file.dcm` prints
  a header and then the content items as an indented tree: relationship, value
  type, concept name and value, with a number's unit, a code's scheme, dates
  and times written as people read them, the SOP class and instance an
  IMAGE, WAVEFORM or COMPOSITE item points at (frames, segments, channels and
  presentation state too), spatial and temporal coordinates, and an item that
  only refers to another as `-> 1.2.3`. `--json` gives the same tree with each
  item's position and `schemaVersion` 1. Compared with dcmtk's `dsrdump` on
  the three reports of the pydicom corpus and one more with what they lack:
  the same nodes in the same order with the same values.

- **A data set can be written back without its pixels in memory** (#331).
  `filereader.DICOMFile.GetDatasetWithValuesInFile` gives a file read with
  `ReadOptions.SkipBulkData` as a data set whose skipped values say where
  they are (`dataelem.ValueInFile`), and `filewriter`'s `SetValueSource`
  copies them from that file as each element is written, a megabyte at a
  time, swapping byte order when the target's differs. The file is the one a
  whole read writes, lengths, sequences and encapsulated fragments included:
  across pydicom's corpus, 151 writes in 11 transfer syntaxes are identical
  byte for byte. A deflated file is refused by name, since its values are
  not in the file; an element whose value is in the file, written with no
  source, is an error and never an empty value, and `dataelem.ValueBytes`,
  and with it the network encoder, refuses one too.

- **An instance is checked against what its SOP class requires** (#383).
  Nothing in the library knew what a CT must hold: a CT with no Modality,
  Patient ID or study and series UIDs, and Bits Stored 99, was read, shown
  and sent without a word. Package `validate` and `go-dicom validate` check
  an instance against its IOD: the modules PS3.3 Annex A gives it, and in
  each one that is Mandatory or present, every attribute's Type, its value
  multiplicity, its Enumerated Values (an error) and Defined Terms (a
  warning), and the number of items of a sequence, into sequences at any
  depth. Each finding names the tag, the path through the sequences, the
  module, the rule and the section of PS3.3 it is in. The tables are
  generated from the standard's own XML, PS3.3 and PS3.4 2026d, by
  `scripts/iod-tables.py`: 153 IODs and 528 module and macro tables, for 164
  Storage SOP classes, and none of it is typed by hand. Conditions are prose
  in PS3.3, and none is evaluated: a Conditional module, an absent Type 1C
  or 2C attribute, and a macro included under a condition are reported as
  not checked, with the condition, never passed. That CT gets its eight
  errors and nothing else; across pydicom's corpus 71 files are checked and
  the rest are refused by name for having no SOP Class UID. `--json` gives
  the findings to a program, and the exit status is 3 for errors and 2 for
  warnings. No other validator was at hand to compare with, so five corpus
  files were compared with the standard by hand.

- **`modify` sets, inserts and deletes attributes of a file** (#332). The
  library could change a data set and write it back in any syntax, and no
  command reached it, so fixing a patient name meant writing Go or reaching
  for `dcmodify`. `-set` changes what is there, `-insert` creates what is
  not, with the sequences and items on its path, and `-delete` removes an
  attribute or an item; paths are dcmodify's, by keyword or tag:
  `ReferencedSeriesSequence[0].SeriesInstanceUID`, `[*]` for every item. A
  private attribute is named with its creator, `(0029,xx10,"ACME 1.0")`,
  and an insert reserves the block. Every value is checked against its VR
  and multiplicity before anything is written. The transfer syntax is kept
  and the pixel data copied as read; a new SOP Instance UID only with
  `-new-uid`; the input is changed only with `-in-place`. Checked against
  pydicom making the same edits over pydicom's corpus: identical, element by
  element, in 73 of the 77 files either reads; the other four are files the
  reader already treats differently (two truncated values it drops, two
  private sequences in an implicit VR file, #354).

- **A data set is compressed to RLE Lossless and JPEG-LS Lossless in place**
  (#363). Both were written only when a network send needed them, by code in
  the network package, so a file could be written in neither.
  `Dataset.CompressRLELosslessInPlace` and
  `Dataset.CompressJPEGLSLosslessInPlace` sit beside the JPEG 2000 and JPEG
  Baseline methods, and the network now uses them: one coder for a file and
  a send. dcmtk (`dcmdrle`, and `dcmdjpls`, which is CharLS) and pydicom's
  decoders return the samples coded for every image of pydicom's corpus that
  can be written in each: 56 as RLE, 48 as JPEG-LS.

- **`compress` and `decompress` write a file in another transfer syntax**
  (#390). The library's four coders and its decoders had no command, so
  this meant writing Go or using dcmtk. `compress -syntax rle|jpeg-ls|
  jpeg2000` is lossless and keeps the instance's UID; `-syntax jpeg` is
  JPEG Baseline, lossy, and refused without `-quality`; its file records the
  loss and is a new instance with a new SOP Instance UID (PS3.3
  C.7.6.1.1.5). A UID works as well as a name. What no coder here writes is
  refused by name: lossy JPEG 2000, JPEG Lossless, 12-bit JPEG, JPEG-LS
  Near-Lossless. `decompress` writes native pixel data and keeps the UID and
  any record of a loss. Over pydicom's corpus, pydicom and dcmtk decode
  every lossless output to the source's samples (RLE 47, JPEG-LS 47, JPEG
  2000 44 by pydicom), and the 16 JPEG outputs to within 2 of each other.

- **`create` makes a DICOM file from DICOM JSON, a picture or a PDF**
  (#402). `convert` went one way, and nothing made a file. From DICOM JSON
  (PS3.18 Annex F), read with the reader the DICOMweb client uses; from a
  PNG or JPEG, a Secondary Capture image (PS3.3 A.8.1), the JPEG put in as
  it is in JPEG Baseline and recorded as lossy, the PNG decoded; from a PDF,
  an Encapsulated PDF (A.45.1) with its length beside it. UIDs come from the
  library's generator; the patient and study from `-template` or flags.
  `convert --format dicom-json` writes what `create` reads: the existing
  `json` format has no sequences or binary values and stays as it is.
  Checked: 66 of the 74 corpus files pydicom reads come back from DICOM JSON
  as the same data set (six have no SOP Class UID and are refused, two are
  truncated files the reader drops a value of), and so does pydicom's own
  `to_json` of five; pydicom shows the pictures' pixels as PIL reads them;
  `dcmdecap` gives back the PDF byte for byte; `dcmdump` reads every file.

- **A file or a send can be compressed to lossless JPEG** (#336). Nothing
  wrote T.81 Process 14, so a peer that accepted only `.57` or `.70` could
  not be sent an image, though both decoded here.
  `Dataset.CompressJPEGLosslessSV1InPlace` writes `.70`, each sample
  predicted from the one to its left, and `Dataset.CompressJPEGLosslessInPlace`
  writes `.57` with the predictor that codes each frame smallest. Both
  syntaxes are now ones a send transcodes to. The coding is exact: 2 to 16
  bits, signed or unsigned, gray or color, with Huffman tables built for each
  frame, since the example tables stop at 11 bits. `compress.EncodeJPEGLossless`
  codes one frame. dcmtk and libjpeg decode every image of pydicom's corpus
  written in either syntax to the samples that went in, and the
  interoperability suite has dcmtk check it.

- **JPEG 2000 in every progression order, with any precincts** (#346).
  The decoder read only LRCP and RLCP with one precinct per resolution, and
  refused the RPCL, PCRL and CPRL orders and a coding style with its own
  precinct sizes. RPCL with precincts is a common encoder default, so a file
  written for resolution or spatial access could not be displayed without
  an external decoder. All five orders and user-defined precincts now
  decode. OpenJPEG returns the same samples on 95 codestreams across depths,
  signs, component counts, tiles, image offsets, layers and both wavelets,
  and CI checks it on every push.
  A codestream that declares a one-sample precinct above the lowest
  resolution is refused, as T.800 A.6.1 requires and as OpenJPEG does.
  `opj_compress -c` writes one when the precinct list runs out too soon.

- **JPEG 2000 with the code-block style options** (#372). Selective
  arithmetic coding bypass, reset of context probabilities, termination on
  each coding pass, vertically causal context, predictable termination and
  segmentation symbols were each refused. Encoders use them for error
  resilience, speed and hardware. All six decode, in any combination, with
  a block split into several codeword segments across layers. OpenJPEG
  returns the same samples for all 63 combinations, and with tiles,
  precincts, a position-driven order and the 9/7 filter. A block coded
  without them takes the loop it took before.

- **JPEG 2000 with a region of interest** (#396). An RGN segment, which an
  encoder writes to give part of the image priority by the Maxshift method,
  was refused in the main header and taken for an unexpected marker in a
  tile-part header. Both now decode: the band's bit planes are counted s
  higher and every coefficient of 2^s or more is scaled back down. OpenJPEG
  returns the same samples across shifts, components, layers, tiles,
  precincts, the style options and the 9/7 filter. With selective
  arithmetic coding bypass as well, OpenJPEG 2.5.4 cannot decode its own
  lossless output, and this decoder returns every sample that was encoded.

- **JPEG 2000 with progression order changes, and JP2 boxes with 64-bit
  lengths** (#417). A POC segment, which divides a tile's packets into
  volumes each in its own order, was refused in the main header and taken
  for an unexpected marker in a tile-part header, which is where
  `opj_compress -POC` writes it. Both now decode, and a packet an earlier
  volume carried is not read again. A JP2 box giving its length in 64 bits
  was refused, and is now read. OpenJPEG returns the same samples. Packed
  packet headers and subsampled components are still refused by name.

- **The DICOMweb server serves frames** (#330). `…/instances/{uid}/frames/1`
  answered 404, and a web viewer loads an image by frame, so none could show
  what `qrscp -web` holds. `GET …/frames/{list}` now answers with one part
  per frame, in the order asked: uncompressed (`application/octet-stream`,
  Explicit VR Little Endian) by default, a compressed frame decoded for it;
  as stored for `transfer-syntax=*` or the frame's own type (`image/jpeg`,
  `image/jls`, `image/jp2`, `image/dicom-rle`). A frame is read from the file
  by where it lies, so one frame of a 200-frame image costs that frame:
  `filereader.DICOMFile.FrameRanges` says which bytes hold each frame, with
  the Extended and Basic Offset Tables and a frame split over fragments
  (`encaps.FrameRanges`, which shares its rules with `encaps.Frames`), and
  `Dataset.DecodeFrame` decodes one. A frame number that is not in the image
  is 404, a list that is not frame numbers 400, and a compressed type the
  frame is not held in 406. Across pydicom's corpus, 56 files, seven of them
  multi-frame: every frame is the bytes pydicom divides out of the same file,
  and Orthanc sends the same bytes for 49 of them; two more are 32-bit big
  endian, which Orthanc reverses in two-byte words and pydicom does not.

- **The DICOMweb server serves bulk data, and metadata points at it**
  (#368). Metadata left each value over 1 KiB out and named it by its VR
  alone, so an overlay, an icon, a document or the pixels could be had only
  by downloading the instance. Each now carries a `BulkDataURI`
  (PS3.18 F.2.6), inside a sequence as well, and
  `…/instances/{uid}/bulkdata/{tag}[/{item}/{tag}…]` serves the value, read
  from the file by where it lies: `application/octet-stream`, little endian.
  Pixel Data is sent as its frames are: one part a frame in their own type
  when that is asked for, otherwise every frame in one part, uncompressed.
  Across pydicom's corpus, all 61 URIs that metadata gives serve the bytes
  pydicom reads at that place in the file.

- **The DICOMweb server serves the bulk data of a study, a series or an
  instance in one answer** (#454). `/studies/{uid}/bulkdata`, the series'
  and the instance's answered 404, so a client that held a series' metadata
  asked for each value on its own. They are served now (PS3.18 Table
  10.4.1.5-1): every value metadata names, a part each, with the
  `BulkDataURI` metadata gives it as the part's `Content-Location` (PS3.18
  8.6.1.2), one instance open at a time and each value read from the file
  by where it lies. Pixel Data is uncompressed in one part, or, for
  `transfer-syntax=*`, compressed frames as stored, a part a frame at the
  frame's resource. Another type than
  `multipart/related; type="application/octet-stream"` is 406, and a
  resource with no bulk value 204. Across pydicom's corpus, each of the 56
  instances' bulk data holds exactly the values its metadata names, at
  those URIs, with the bytes pydicom reads. Orthanc 1.13 answers 404.

- **The DICOMweb server renders images and thumbnails** (#382).
  `…/rendered` and `…/thumbnail` answered 404. They are served now for an
  instance and for frames, and the thumbnail for a series and a study too,
  as JPEG or PNG by `Accept`, through `Dataset.RenderFrame` on a frame read
  by position. `window=center,width,function`, `viewport=vw,vh[,sx,sy,sw,sh]`
  and `quality` are applied. What is not done is refused, not ignored: a
  flipped region, an ICC profile, a malformed parameter are 400, GIF and the
  other types 406, a study or series rendered whole, or an instance with no
  image, 501; `annotation` is answered with the picture and a `Warning: 299`
  that says it was not drawn. `RenderOptions.Window` gains `Function`, so a
  window given by the caller can be LINEAR_EXACT or SIGMOID. Across
  pydicom's corpus, 21 grayscale frames are within one level of pydicom's
  windowing and 26 color frames within two levels of its pixels, but for
  four lossy JPEGs whose subsampled chroma the two decoders upsample
  differently at color edges.

- **The DICOMweb server serves the frames of float and packed 1-bit
  images** (#441). An image held in Float Pixel Data (7FE0,0008) or Double
  Float Pixel Data (7FE0,0009), a Parametric Map for one, was answered as an
  instance with no pixels (404). A 1-bit image whose frame is not a whole
  number of bytes, 5 x 5 for one, was answered 500: its frames are packed with
  no padding between them (PS3.5 8.1.1), so all but the first start inside a
  byte, and `FrameRanges` refused it. Float frames are now sent as
  `application/octet-stream`, their floats little endian whatever the file's
  byte order. A packed 1-bit frame is sent as a 1-bit image of one frame is
  laid out: its first pixel in the lowest bit of the first byte, zero bits
  after its last; its rendered picture is drawn too. A float image is not
  drawn, and its rendered resource says so (501). `FrameRanges` gives such a
  frame the bytes that hold its bits, and `DICOMFile.FrameFirstBit` the bit
  it starts at; `DICOMFile.PixelDataElement` finds whichever of the three
  pixel elements an image has. Files pydicom writes, float and double, little
  and big endian, and a 3-frame 5 x 5 1-bit image: every frame equals
  pydicom's `pixel_array`. Orthanc 1.13 answers all three with 400.

- **A file or a send can be compressed to JPEG-LS Near-Lossless** (#337).
  The JPEG-LS encoder coded NEAR 0 only, so `1.2.840.10008.1.2.4.81` was
  refused by name and a peer that accepted only it could not be sent an
  image. `compress.EncodeJPEGLSNearLossless` takes the allowed error, and
  every sample decodes to within it. `Dataset.CompressJPEGLSNearLosslessInPlace(near)`
  writes a file in it, marked Lossy Image Compression `01` with its ratio
  and `ISO_14495_1` when the error is above zero. A send that needs `.81`
  uses `NetworkConfig.JPEGLSNear`; at the default, zero, the image is coded
  losslessly under that syntax, so nothing is lost unasked. A signed image
  is coded only losslessly, as dcmtk does. dcmtk and libjpeg decode every
  unsigned image of pydicom's corpus coded at NEAR 1, 2 and 5 to within the
  error and to the same samples as each other, and the interoperability
  suite has dcmtk check it at NEAR 2.

- **A file or a send can be compressed to 12-bit JPEG** (#338). JPEG
  Baseline is 8-bit, so a CT, MR or CR of 12 bits could be written lossy
  only by dropping 4 bits, which was refused, and a peer that accepted only
  `1.2.840.10008.1.2.4.51` could not be sent one.
  `Dataset.CompressJPEGExtendedInPlace(quality)` writes JPEG Extended: 12
  bits for an image of up to 12 stored in 16, 8 bits for an 8-bit one, with
  Huffman tables built for each frame since the example tables do not hold
  12-bit coefficients, marked Lossy Image Compression `01` with its ratio
  and `ISO_10918_1`. A send needing `.51` uses `NetworkConfig.JPEGQuality`,
  as JPEG Baseline does, and is refused without it. Signed images and more
  than 12 bits stored are refused by name. `compress.EncodeJPEGExtended`
  codes one frame. dcmtk and libjpeg decode every corpus image that fits; at
  quality 90 the size and PSNR match dcmtk's own encoder at the same
  quality.

- **A file or a send can be compressed to lossy JPEG 2000** (#339). The
  JPEG 2000 encoder was lossless only, so `1.2.840.10008.1.2.4.91` was
  refused by name and a peer that accepted only it could not be sent an
  image. `jpeg2000.EncodeLossy` codes the irreversible 9/7 wavelet, the
  irreversible component transform and a quantization step for each
  subband, and meets a compression ratio or a PSNR by searching for the
  step. `Dataset.CompressJPEG2000InPlace` writes a file in it: an RGB image
  becomes `YBR_ICT`, a signed one stays signed, and the data set is marked
  Lossy Image Compression `01` with its ratio and `ISO_15444_1`. A send
  needing `.91` uses `NetworkConfig.JPEG2000Rate` and is refused without
  it. A ratio a small image cannot meet is refused rather than met with an
  empty picture. OpenJPEG and pylibjpeg-openjpeg decode every corpus image
  coded at 10:1 and at 45 dB, and at the same size the PSNR matches
  OpenJPEG's own encoder or betters it by a few tenths of a decibel. The rate
  is reached by quantization alone, in one quality layer, not by the
  pass-by-pass truncation of Annex J.14.

- **`compress` writes every syntax the library writes.** It offered RLE,
  JPEG-LS Lossless, JPEG 2000 Lossless and JPEG Baseline, and refused the
  rest by name. It now also writes `-syntax jpeg-lossless` (`.57`) and
  `jpeg-lossless-sv1` (`.70`), and the lossy ones only when asked with
  their parameter: `jpeg-ls-near -near N` (`.81`; `-near 0` is lossless
  and keeps the UID), `jpeg-extended -quality Q` (`.51`), and
  `jpeg2000-lossy -ratio R` or `-psnr DB` (`.91`). A lossy file records
  the loss and is a new instance, as `jpeg` already did; a parameter given
  to a syntax that does not take it is refused. dcmtk, libjpeg and OpenJPEG
  decode what it writes in each new syntax.

- **`create` makes a file for each object of a DICOM JSON array, and reads a
  BulkDataURI that names a local file** (#448). An array of more than one
  object, which is what a DICOMweb metadata response is, was refused ("one
  file is made from one"), and any value held at a BulkDataURI was refused
  because the reader would have written it empty. Now an array makes one file
  for each object in the output directory, named by SOP Instance UID as `wado`
  names them, with nothing written when any object is refused or two share a
  UID. A BulkDataURI that is a `file:` URI or a path is read into the value
  from inside `-bulk-dir` (by default the JSON file's directory), links
  followed, so JSON from a server cannot pull another file of the machine into
  the one made; a `http:` or `https:` one is refused by name and not fetched,
  since a command that makes a file does not reach the network unasked. Checked
  against the JSON pydicom writes for a list of instances, with its values
  inline and with them in files, and against the metadata and bulk data
  `qrscp -web` serves: each comes back as the original data sets. An array of
  one object, and every object that worked, are as they were.

- **`sr` prints what it left out: verifying observers, predecessor documents,
  nested template identifiers and Real World Value Mapping references**
  (#455). The help said the document's verifier and the reports it replaces
  were not printed, and a reader had to fall back on `show`. The header now
  lists each verifying observer (name, organization, time and code) and each
  predecessor document with its series and study; an item built from a
  template other than the root's ends with `# TID 1411 (DCMR)`, as dsrdump's
  `+Pt` prints it; an image reference ends with `mapped by` and the mapping
  instances it names. `--json` has them as `verifyingObservers` and
  `predecessorDocuments` in the document, `template` on an item and
  `realWorldValueMappings` in a reference, each present only where the report
  has it, so `schemaVersion` stays 1 under the rule it already had: it changes
  when a field is renamed, removed or changes its meaning, not when one is
  added. A report with none of them prints as it did. Compared with dcmtk's
  `dsr2xml +Wt` and `dsrdump +Pt` on four reports written with pydicom (all
  four features, none, several predecessor studies and series, an image with a
  presentation state and a mapping deep in the tree) and on `test-SR.dcm`: the
  same observers, predecessor instances, templates and mappings. The identical
  documents of a report are still not printed.

- **`-allow` pairs a title with an address, and its list can be read from a
  file** (#462). `-allow` kept titles and addresses as two lists a caller had
  to match both of, so "PRISMA1 only from 10.0.0.5" could not be said:
  `PRISMA1@10.0.0.5` was read as an AE title that nobody has, and the server
  refused its own scanner. `TITLE@ADDRESS` (an address or a CIDR range) now
  admits that title from there and from nowhere else; each pair is a way in of
  its own beside the titles and addresses given together, whose rule is as it
  was. An @ in a title that is not followed by an address stays a title, and
  one followed by something that reads as an address and is not (`CT@10.0.0`)
  is an error. `-allow-file` reads entries from a file, one to a line, with `#`
  comments on a line of their own or after a space, and is joined with `-allow`;
  a file with no entry, or none that is there, stops the server, since an empty
  list admits every caller. A listed title calling from an address it is not
  admitted from is refused with no reason given, as an unlisted address is,
  since "not recognized" would be untrue of it, and the server prints which.
  Checked with dcmtk's `echoscu` and pynetdicom against `storescp` and
  `qrscp`: admitted and refused callers, and the reason each is shown, match.
  Admission by TLS client certificate is not part of `-allow`: it names a
  certificate by subject, SAN or fingerprint, a third kind of entry, and would
  be a flag of its own.

- **`validate` evaluates the conditions PS3.3 states in a fixed form**
  (#449). Every Type 1C and 2C attribute and every Conditional module was
  reported "not checked", so a CT image with no Pixel Data and no Pixel Data
  Provider URL passed with exit status 0. The generator now reads a
  condition as data when the whole of what PS3.3 says about presence is one
  sentence of a few forms naming one attribute by its tag ("Required if
  Pixel Data Provider URL (0028,7FE0) is not present", "... is present",
  "... has a Value of YES", "Image Type (0008,0008) Value 1 is ORIGINAL or
  MIXED", "... is present and has a Value of X"), with "May be present
  otherwise" when it is said. Of 1681 Type 1C and 2C attributes in the tables
  of PS3.3 2026d, 580 are read so, and 25 of 316 Conditional modules; the
  rest, and any sentence matching a form only in part, stay prose and are
  still reported as not checked with their text. An attribute or module
  absent where its condition holds is an error, and so is one present where
  it does not hold unless PS3.3 allows it otherwise (PS3.5 7.4.2, 7.4.4;
  PS3.3 A.1.3.2). Over pydicom's corpus, seven findings are new, each
  checked against the file with pydicom, and 524 fewer are not checked.
  Exit statuses and `--json` are as they were.

- **`validate` checks the functional groups of an enhanced multi-frame
  image** (#463). The IOD's Functional Group Macros table was not read, so
  an Enhanced CT with Pixel Measures removed, with Frame Content moved into
  the shared item, or with a frame missing its Plane Position, passed with
  no error. The generator now reads the 29 Functional Group Macros tables of
  the enhanced Storage IODs (437 rows), with each macro's usage, condition
  and where it may be ("May not be used as a Shared Functional Group"), and
  the 105 macro tables they name. A macro the IOD requires must be in the
  shared item or in every frame's, none may be in both or where the table
  forbids it, the per-frame sequence holds one item for each frame, and
  each macro present is checked attribute by attribute, a fault found in
  many frames being one finding that says in how many. Reading those tables
  also reads 65 more Type 1C and 2C conditions as data, among them the
  "..., may be present otherwise." form many of them use.

- **`validate`'s findings are compared with the standard on ten more IODs**
  (#482). No other IOD validator can be run where it is built (`dciodvfy`
  and the Python `dicom-validator` are not installed), and CONFORMANCE said
  so without saying what had been compared instead. Ten corpus files of ten
  IODs (RT Structure Set, RT Plan, RT Ion Plan, Segmentation, both
  Ultrasound IODs, Basic Text and Comprehensive SR, 12-Lead ECG, Secondary
  Capture) were compared with PS3.3 by hand: each of their ten errors is a
  real defect of the file, and a second reading of the tables found no
  missing Type 1 or Type 2 attribute of a Mandatory module that it had not
  reported. CONFORMANCE §8.6 says what was compared, and what was not.

- **A read logs to a logger of its own** (#481). A file read logged only to
  the global `config.Logger`, stderr by default, so a server could not give
  one read its own logger, or keep one read quiet, without changing it for
  every read in the process. `ReadOptions.Logger` takes a `*slog.Logger` for
  everything the read logs, the reader, the character set decisions and the
  validation `GetDataset` runs, and none of it reaches the global one; nil
  is the global one, as before. `slog.New(slog.DiscardHandler)` reads
  silently. The logger is carried in a context, by the new
  `config.WithLogger` and `config.LoggerFromContext`, to `charset`'s
  `WithContext` functions and the new `Dataset.AddWithContext` and
  `AddSequenceWithContext`. The character set lines printed the file's own
  Specific Character Set value; they now name only the defined term assumed,
  or the length of one that is not a term.

- **Attributes are read without linking the codecs** (#498). `filereader`
  was the only way to read a file, and it depends on `dataset`, `encaps`,
  `compress`, `compress/jpeg2000`, `pixels` and `waveforms` through
  `GetDataset`, `FrameRanges` and `GetWaveforms`, so a program that only
  reads attributes linked them all. The new `filereader/attributes` package is
  the parser alone: `Read`, `TextEncodings` and `DecodeText`, returning the
  same element types as `filereader`. A test runs `go list -deps` on it and
  fails if any of those packages, or `dataelem`, joins its dependencies, and
  another holds its elements to `GetDataset`'s for every corpus file. The
  parser moved to an internal package that `filereader` is now built on;
  every `filereader` identifier is where it was, the element types by alias
  and printing with `%T` as they did (`*filereader.DataElementValue`).
  A program that prints a file's attributes is 235 KB smaller built this
  way (5.67 MB against 5.91 MB). What it saves depends on what else a
  program calls, since the linker drops unreachable code.

- **What a server accepts can be set before it listens** (#468). The abstract
  and transfer syntaxes could only be set by method, and a `Server` exists only
  once `StartServer` has started accepting, so a peer that connected in the
  gap negotiated with the defaults: a storage archive accepted Study Root FIND
  and MOVE from it. `SCPConfig.AbstractSyntaxes` and
  `SCPConfig.TransferSyntaxes` are read before the listener opens. Nil keeps
  the defaults; a non-nil list is used exactly as given, so an empty one
  accepts nothing. `StorageAndVerificationSyntaxes()` returns Verification and
  every storage class. The setters still work.

- **A server can stop without cutting the stores in progress** (#464).
  `Server.Stop` cancels the context every handler is given before anything
  else, so a C-STORE in flight was cut wherever it was: dcmtk's storescu,
  sending a 96 MiB instance, was reset partway and failed. `Server.Shutdown(ctx)`
  closes the listener, refuses an association asked for on a connection
  already open (rejected-transient, temporary-congestion), aborts at once an
  association waiting for its next request, since everything it sent was
  answered, and lets one in the middle of an operation finish it and release.
  When `ctx` ends first, what is left is aborted and the returned
  `*ShutdownError` counts the associations cut short. The same storescu send
  is now answered Success and released. `Stop` is unchanged.

- **An abort says why** (#470). A timeout, a protocol violation by the peer,
  a handler that panicked and an unknown command all sent an A-ABORT that was
  reported to events with nothing but the PDU, and a timeout and a panic were
  byte for byte the same. The EVTPDUSent and EVTAssocAborted of an A-ABORT this
  side sends now carry `Event.LocalAbort`, with its `Cause`
  (`AbortCauseTimeout`, `AbortCauseProtocolViolation`, `AbortCauseHandlerPanic`,
  `AbortCauseStopping`, `AbortCauseRequested`) and the error behind it. An
  abort the peer sent has none. The PDU on the wire is unchanged, and the
  `-events` log does not write the cause.

- **A failed TLS handshake is an event** (#471). A TLS server runs the
  handshake inside its read of the A-ASSOCIATE-RQ, so a client it turned away
  left only a log line, and events saw the connection open and close as for a
  port scan. It now emits `EVTTLSHandshakeFailed`, before any association
  exists, with the client's address and a `*TLSHandshakeError` whose `Check`
  names what failed: `TLSCheckNotTLS`, `TLSCheckNoClientCertificate`,
  `TLSCheckUnknownAuthority`, `TLSCheckCertificateInvalid`,
  `TLSCheckPeerAlert`, `TLSCheckProtocol` or `TLSCheckConnection`. It carries
  nothing of the client's certificate: crypto/tls's verification error holds
  the certificates themselves, and is not passed on. `OnAny` includes it, so
  the `-events` log of a TLS server writes an `EVT_TLS_HANDSHAKE_FAILED` line
  where it wrote none.

- **A server or a client can have its own logger** (#472). Every line the
  network package logged went through the global `DefaultLogger`, and the
  lines name AE titles, addresses and SOP Instance UIDs, so two servers in one
  process logged into one stream that did not say which was which.
  `SCPConfig.Logger` and `SCUConfig.Logger`, a `*slog.Logger` as
  `filereader.ReadOptions.Logger` is, take everything that server or client
  logs, at every level: its associations and their negotiation, refusals, a
  handler panic, a timeout, a failed TLS handshake, the associations a server
  opens itself, a panic in a handler of its events, and a `Server`'s start-up
  line (`Server.Logger` writes there). Nothing of it reaches the global logger.
  Nil keeps the global one. Not covered: the validation warnings of the
  `dataset`, `dataelem` and `charset` packages while a data set is decoded,
  which still go to `config.Logger` (#503).

- **The DICOMweb server encodes a frame into the syntax a request names**
  (#474). A frame was sent in a compressed type only when the instance was
  stored in it; anything else was 406, "nothing is encoded on request",
  though the library has encoders for most of them. Now a frame is encoded
  with them (PS3.18 8.7.3.3.2): RLE Lossless, JPEG-LS Lossless and
  Near-Lossless, JPEG Lossless (Process 14 and SV1), JPEG Baseline and
  Extended, and JPEG 2000 Lossless and lossy. A type named without a syntax
  gets its default from Table 8.7.3-5, which is lossless for every type. A
  lossy syntax is used only when the request names it, and loses what
  `dicomweb.WithFrameEncoding` sets: JPEG quality 90, JPEG-LS NEAR 2 and JPEG
  2000 at 10:1 unless told otherwise. The forms are taken in the order the
  request lists them. JPEG 2000 Part 2, an image the coder does not take
  (16 bits to JPEG Baseline, for one) and a frame held lossy, which is not
  coded again, are 406 with the reason. Seven corpus images were asked for
  in every syntax and each returned frame was decoded by pylibjpeg's
  libjpeg and OpenJPEG or pydicom. Lossless encodings (32) decoded to the
  stored pixels, JPEG-LS near-lossless within 2, and JPEG and lossy JPEG 2000
  at 38 dB or better. The interop job installs pylibjpeg for this step.

- **`upsscu` works Unified Procedure Steps** (#500). The library serves UPS,
  but its SCU has no UPS call, and no command sent one, so a performer or a
  test of a UPS SCP had to be written in code. `upsscu` builds each
  operation from the SCU's N-DIMSE primitives and C-FIND, on the SOP class
  PS3.4 CC.2 defines it on:
  - `-create` (N-CREATE SCHEDULED, Push);
  - `-query` (C-FIND, Query, answers with their sequences);
  - `-get` (N-GET, Pull);
  - `-claim` (Change State to IN PROGRESS with a Transaction UID it prints);
  - `-set` (N-SET with that Transaction UID);
  - `-complete` and `-cancel` (Change State with it);
  - `-request-cancel` (Push).

  `-json` adds a DICOM JSON object to what is sent. A refusal prints its
  status and exits nonzero. Watch is left out, since no command receives
  the event reports. Run against pynetdicom's UPS SOP classes in
  `scripts/interop-test.sh`: one step's whole life, each request on its SOP
  class, and a completion under another Transaction UID (C301H) and a
  cancellation of a final step (C311H) refused.

- **`wlmscp` serves a Modality Worklist from a directory of files** (#506).
  The command line could ask a worklist (`findscu -worklist`) and not answer
  one, so a modality, or `findscu -worklist` itself, could not be tested
  against this tool alone. `wlmscp -dir` serves the worklist files of a
  directory, one entry per file, with File Meta Information or without, as
  dcmtk's `wlmscpfs` does. It reads the directory again at every query, so
  a file added, changed or removed while it runs is served as it is; a file
  that is not a worklist entry, not DICOM or with no Scheduled Procedure Step
  Sequence, is not served and is reported once. It answers Modality Worklist
  C-FIND by package `matching`, the step matched item by item and each
  answer holding the keys asked for, answers C-ECHO, refuses every other SOP
  class, and takes `-tls`, `-events` and `-allow`. Over dcmtk's sample
  worklist database, dcmtk's `findscu -W` and pynetdicom get the same
  entries, attribute for attribute, from `wlmscp` as from `wlmscpfs`, for 14
  queries by modality, date, date range, time range, station, patient name
  with wildcards, universal matching and one that matches nothing; so does
  `go-dicom findscu -worklist`. They differ on a name in another case:
  `wlmscp` finds VIVALDI^ANTONIO for `vivaldi*`, as the archive does, and
  `wlmscpfs` does not.

- **The JPEG 2000 decoder decodes High-Throughput code-blocks** (#403). An
  HTJ2K codestream (ISO/IEC 15444-15, ITU-T T.814) keeps everything of Part 1
  but the block coder, and `jpeg2000.ParseCodestream` stopped at its CAP
  marker: `unexpected marker FF50`. `compress/jpeg2000` now reads the CAP
  segment and decodes HT code-blocks, the cleanup pass and the SigProp and
  MagRef refinement passes, written from the standard's text, inside the
  existing decoder, so tiles, precincts, progressions, layers and both filters
  carry over. pydicom-data's two HTJ2K files and openjpeg-data's HT
  codestreams decode to OpenJPEG 2.5.4's samples, exactly where reversible, and
  so do 390 codestreams transcoded to HT by the tests from `opj_compress`
  output at 8 to 16 bits, signed and unsigned, gray and color. HTJ2K pixel
  data in a data set is still refused by name until the transfer syntaxes are
  wired to it. Placeholder passes, more than one HT set, a region of interest
  on HT blocks, and HT and Part 1 blocks mixed in one tile-component are
  refused by name; HTJ2K is not encoded.

- **High-Throughput JPEG 2000 images decode** (#403). Pixel data in
  `1.2.840.10008.1.2.4.201`, `.202` and `.203` was refused by name since #406:
  `the pixel data is in High-Throughput JPEG 2000 Image Compression ...,
  which is read and carried and not decoded`. It now decodes through the JPEG
  2000 decoder wherever pixel data is decoded: the samples, rendering,
  `decompress`, `convert`, redaction, DICOMweb frames (as stored, typed
  `image/jphc`), and a send to a peer that accepts only uncompressed syntaxes.
  pydicom-data's `.201` file decodes to pydicom 3's samples; no third-party
  `.202` file exists, so a generated RPCL one, wrapped by pydicom 3, is in the
  tests with two others, and all three decode to pydicom's samples. Nothing is
  encoded into HTJ2K. JPEG XL stays refused by name.

- **The text detector finds a clock and short labels in a small fixed-pitch
  font** (#535). On the ultrasound screen in pydicom's corpus,
  `examples_rgb_color.dcm`, the clock and the `PWR` labels beside the color
  bars were not found: their glyphs are 4 by 6 pixels with soft edges, and
  measured one by one a soft glyph is drawn like a blob (a `W` fills its
  box, a `0` has a stroke 4 pixels wide), so what was left of each label was
  two shapes. A screen's character generator puts every glyph in a cell of
  one size, so its glyphs have identical boxes a whole number of cells apart
  however soft their strokes; the detector now looks for three or more on
  such a grid, on a plain ground, and adds what it finds where nothing else
  was. On that screen 15 of 17 lines are covered whole, from 11; the fourth
  `PWR` touches its color bar and is still missed. Every other region over
  the 79 files of the corpus, and on every picture the tests draw, is
  unchanged. Lettering blurred until its characters run together is still
  not found (the 7 drawn known misses), and `auto` stays not the default.
  A new test measures the detector on lettering it was not shaped by: a
  second font at three sizes, four kinds of blur, over six real scans of the
  corpus (51 of 164 lines found, from 50, and no more of any scan marked).

- **The text of a file read for its attributes is decoded to the read's
  logger** (#539). `attributes.TextEncodings` and `DecodeText` take no
  logger, so what decoding logs, a misspelled Specific Character Set read
  as the term it means, an unknown one read as the default repertoire, a
  value that does not decode, went to `config.Logger` even for a read
  given a logger of its own (`ReadOptions.Logger`).
  `TextEncodingsWithContext` and `DecodeTextWithContext` log to the logger
  the context carries (`config.WithLogger`), as the rest of a read does.
  Every corpus file and two made from charset files with a misspelled and
  an unknown term decode with nothing reaching the global logger.

### Changed

- **`convert --format png` and `jpeg` read the frame asked for, not the
  image** (#533). The renderer read every sample from the whole Pixel Data in
  memory, so one frame of a hundred cost all hundred, and a 200 MB frame took
  340 MB to become a PNG: the samples and the 8-bit picture together. The
  Pixel Data now stays in the file. A compressed frame is read and decoded
  alone; a native frame is read a band of rows at a time, about a megabyte,
  as the PNG encoder asks for its rows. On a Mac, frame 50 of a hundred
  frames of 1024 x 1024 takes 28 MB where it took 237 MB, all hundred 29 MB
  where they took 239 MB, and the 200 MB frame 28 MB as PNG and 132 MB as
  JPEG, where both took 340 MB. A grayscale JPEG still holds the whole
  picture: `image/jpeg` writes one component only from an `*image.Gray`. An
  image with no window is still drawn over the range of all its frames, so
  every frame is read once to find it before the first row is drawn, a band
  at a time; that reads the file twice, and decodes a compressed image twice.
  The pictures are the same, byte for byte: every PNG (133, every frame) and
  JPEG (59) of pydicom's corpus, with the file's window and with one given,
  and the DICOMweb rendered frames and thumbnails. `Dataset.RenderFramesFrom`
  renders from a `dataset.FrameReader`, which `filereader.DICOMFile.FrameReader`
  gives for a file; the DICOMweb server reads its frames through the same
  reader. A deflated file, one without a transfer syntax, or one in a syntax
  with no decoder is read whole as before.

- **dcmtk reads this library's JPEG-LS frames below 8 bits** (#521). dcmtk
  3.7.0 bundles a CharLS from before 2.1, which works out the default
  thresholds differently from T.87 below 8 bits: losslessly at 6 and 7 bits,
  and at most NEAR values from 2 to 7 bits. It refused most of those frames
  and decoded the odd one to a slightly different image without an error.
  Where the two sets differ, and only there, the encoder now writes T.87's
  thresholds in an LSE segment, as T.87 C.2.4.1.1 allows, so dcmtk uses
  them. At 6 and 7 bits dcmtk now decodes every frame, at every NEAR, to
  CharLS 2.2's and libjpeg's samples, where it refused 52 of 65. Every other
  frame is unchanged, and the ones that gained the segment are byte for byte
  what CharLS 2.2 writes when given the thresholds.

- **Events no longer copy every received byte** (#469). A received PDU was
  copied whenever its connection reported events, and a `Server` always has
  an `EventManager`, so every instance received was copied once more with no
  handler registered: 64 MiB more for a 64 MiB C-STORE. Whether to keep a
  PDU's bytes is now decided from its type before its body is read: a
  P-DATA-TF is copied only while a handler of EVTPDURecv, the one event that
  is given its bytes, is registered; every other PDU, small and bounded, is
  copied as before, so the association events keep their `PDUBytes`. A
  server with only association-level handlers allocates 0.5 MiB over one with
  no `EventManager` for a 64 MiB store, where it allocated 67 MiB. The
  `-events` log registers every event type and is unchanged, byte for byte.

- **`validate` no longer holds the image to check the attributes** (#440).
  It read every value of the file, Pixel Data included, though the checker
  needs only a value's presence and length: a 200 MB CT took 240 MB of
  memory. Pixel Data and binary values longer than 1 KB now stay in the file,
  and the same file takes 28 MB. The findings are the same, text, `--json`
  and exit status, for every file of pydicom's corpus. A deflated file is
  still read whole, as its values are not in the file as written.

- **A subsampled color JPEG decodes to the picture libjpeg gives** (#398).
  The decoder repeated each chroma sample of a 4:2:2 or 4:2:0 frame, where
  libjpeg, which both dcmtk and pydicom decode with, filters between
  neighboring samples by default. Neither is wrong under T.81, but the same
  file came out up to 43 levels away from dcmtk and pydicom at a color edge
  in pydicom's corpus (`SC_rgb_jpeg_lossy_gdcm.dcm`), and up to 75 once
  rendered to RGB. The chroma is now filtered as libjpeg's "fancy
  upsampling" does, and every subsampled color JPEG in the corpus decodes
  within one level of both, the inverse DCT's difference. The interop
  script's rendered-frame check holds subsampled JPEG to two levels, like
  every other color frame, instead of to a mean of three.

- **`info` no longer holds the image to print a few lines.** It read every
  value of the file, Pixel Data included, so a 200 MB image took 218 MB of
  memory to print the patient, the modality and the dimensions. Bulk values
  now stay in the file, as they have for `show` since 1.9.1: the same file
  takes 17 MB. What `info` prints does not change, including the sizes of
  `--stats` and `--verbose`, which count the bytes in the file (#328).

- **`info` prints the frames, and the image's own size and patient.** It read
  Number of Frames from (0020,1209), so a multi-frame file never got a
  `Frames:` line. And it took the last match of every key element, items of
  sequences included: `examples_overlay.dcm` printed the 64x64 of its icon
  for an image of 300x484, and `CT_small.dcm` the ID of an "other patient" for
  its own. Only the data set's own elements are read now (#335).

- **`anonymize` no longer holds the image in memory** (#331). A file was read
  whole, Pixel Data included, to change a few hundred bytes of text, so
  memory followed the size of the image: 220 MB for a 200 MB CT, and a
  whole-slide image of several gigabytes could not be de-identified on a
  small machine. Unless `-redact` is given, the pixels now stay in the input
  and are copied into the output as it is written: the same CT takes 20 MB,
  as a 39 KB one does. The output is the same, byte for byte, as before:
  checked over every file of pydicom's corpus under every profile, with the
  pixels left in the file and with every binary value left there.

- **`convert --format png` and `jpeg` hold one picture at a time** (#331).
  Every frame asked for was rendered before the first was written, and each
  was encoded into memory before its file was made: a hundred frames of a
  megapixel took 418 MB, and a 200 MB image of noise took 482 MB to become
  one PNG of 100 MB. Each frame is now rendered, encoded straight into its
  file and let go before the next: 223 MB and 321 MB, with the same pictures,
  byte for byte, over every file of pydicom's corpus. The files are written
  under temporary names and take theirs together, so a failure still leaves
  none of them. `Dataset.RenderEachFrame` is the new call that hands frames
  over one at a time. What is left is the image itself: the whole Pixel Data
  is read, and one frame of a multi-frame image still costs every frame,
  since the renderer reads samples from one slice and finds the range of an
  image with no window across all frames.

- **A store costs the same at the hundred-thousandth instance as at the
  first** (#410). Every store rewrote the archive's whole index, so it took
  time in proportion to the archive: 11 ms an instance over the first
  thousand and 865 ms over the hundredth, when the index had grown to
  108 MB. A thousand-image study received at that size took a quarter of an
  hour. A store now appends a record to `index.log` beside `index.json`;
  `Open` replays it, and it is folded into a new `index.json` once it holds
  more records than the archive has instances. Measured the same way, the
  hundredth thousand cost 13 to 30 ms an instance, the first thousand 12 to
  29: the cost of syncing each instance's file, which varies with the disk's
  load. A crash between putting an instance's file in place and indexing it
  was found only by a full rebuild; each change is now recorded and synced
  before its file is put in place or deleted, and one left unfinished is
  settled from the disk at the next `Open`. An index of version 3 is read
  as it is and written again as version 4, which an older release rebuilds
  rather than read without its log.

### Fixed

- **A frame that is not the image its data set describes is refused**
  (#548). Nothing compared a compressed frame's own size with Rows, Columns
  and Samples per Pixel; only a frame shorter than the image was refused. A
  1888-byte file of a 64x64 image holding a 4096x4096 JPEG 2000 frame was
  decoded at 4096x4096 (300 MB resident) and `decompress` wrote its 32 MiB
  out under Rows and Columns of 64, and a frame of the right length in
  another shape, 64x64 read as 32x128 or a color frame as one sample a pixel,
  decoded as a picture it is not. Every codec had the gap. PS3.5 8.2 has the
  data set describe the decompressed image and agree with the compressed
  stream, so a frame whose columns, rows or components differ is now refused
  with both sets of numbers, before it is decoded where its header states
  them: JPEG 2000's SIZ, JPEG's frame header, JPEG-LS's SOF55. The 1888-byte
  file is refused with under a megabyte allocated. An RLE frame has no size
  of its own; each segment must decode to Rows times Columns bytes and is
  refused as soon as it passes that, so a 4096x4096 frame in a 64x64 data set
  costs the 64x64; a reshaped RLE frame of the right length cannot be told
  from the right one. Every path that decodes a frame has the rule: decoded
  pixel data, `decompress`, rendering and `convert`, DICOMweb frames and
  rendered, redaction and a send that transcodes. Any frame longer than the
  image is refused too, as a shorter one was, and the JPEG decoders refuse a
  second frame header. `compress.FrameHeaderReader` and `ErrFrameMismatch`
  let a decoder of the caller's take part. Every file of pydicom's corpus,
  and dcmtk's encodings of nine of them, decodes to the same samples as
  before.

- **A refusal is logged once where a server has its own logger** (#537).
  `LoggingEventHandlers` leaves out a refusal the SCP has already logged to
  the same log, and asked `DefaultLogger` whether it had; since a server can
  have its own logger (`SCPConfig.Logger`), the SCP's line goes there. A
  server with its own logger, logging its events to `server.Logger`, had
  each refusal in its log twice, and one logging its events to the global
  log with `DefaultLogger` at the info level had the refusal in neither.
  The event now carries where the SCP logged its line and at what level,
  and the handler gives way only where that line reached its own log. A
  stopping server's refusal of an association on a connection it had
  already accepted was logged by nothing at all; the SCP logs it, at the
  info level, as it logs every refusal it decides.

- **A JPEG 2000 codestream of a few hundred bytes no longer costs
  gigabytes to decode** (#526). The decoder sized its work from the header
  before it read a packet: a list of every code-block of the image, a state
  for each block of a precinct as soon as a packet named the precinct, and a
  visit to each of those blocks in every packet. A codestream of 244 bytes
  declaring 185 million samples in 4x4 code-blocks made `Decode` allocate
  4.3 GB, and `Dataset.PixelArray` and rendering decode `.90` and `.91`
  unasked, so a small file or a C-STORE was enough. Now a tile must hold a
  byte for each precinct it declares, which the 244 bytes do not, and is
  refused with less than a megabyte allocated. Code-blocks are found by
  arithmetic rather than listed, a block has state only once a packet
  includes it, and the blocks a tag tree answers for together are stepped
  over together, so reading packet headers costs what their bits say: the
  120 bytes of an empty 2048 by 2048 image in 4x4 blocks and 20 layers took
  475 MB to read and take under 4 MB. The image limit of 2^28 samples now
  counts every component. Nothing sized by the image is allocated until
  every tile's packets are read. A codestream within these bounds still
  decodes at the size it declares, which JPEG 2000 can describe in very few
  bytes; the package documentation states the rule. Every Part 1 codestream
  that decoded before decodes to the same samples.

- **A C-GET goes on past an instance it cannot encode** (#518). A C-GET
  sub-operation sent its C-STORE-RQ, which says a data set follows, and
  encoded the instance only then. When the instance could not be put in the
  syntax the requestor had negotiated, a lossy one that is not sent unasked
  for instance, nothing followed: the requestor read the next sub-operation's
  C-STORE-RQ where it was owed a data set, and the whole retrieve ended with
  "expected a data set for the C-STORE sub-operation, got a command". The
  instance is now encoded before anything is sent, so one that cannot be is
  one failed sub-operation and the rest are delivered. C-MOVE was not
  affected.

- **An A-ABORT is read by the peer before the connection is closed** (#513).
  This side wrote its A-ABORT and closed the connection behind it. A peer
  still sending had bytes on their way that were never read, and a connection
  closed with bytes unread is reset, which discards what the peer had not yet
  read: the A-ABORT. On Windows a client that sent a request and then read
  got "connection forcibly closed" and never learned it had been aborted, or
  why. This side now stops sending, reads and drops what arrives until the
  peer closes or a second has passed, and closes then, on a goroutine of its
  own: the association gives back its slot and its handler at once. PS3.8
  gives the wait ARTIM; a second is kept so that a peer that will not close
  holds a connection and nothing else.

- **An Implicit VR image is no longer warned about for its own VR** (#483).
  Where the dictionary offers a choice of VRs, an element read from an
  Implicit VR file carries the whole choice, and the check added with the fix
  for LUT Data compared it with each choice only. Pixel Data was reported as
  `expected OB or OW, got OB or OW`, and Smallest and Largest Image Pixel
  Value the same way, on ordinary images. Never in a release.

- **A C-STORE with no data set is refused whatever the handler** (#457). A
  C-STORE-RQ whose Command Data Set Type says no data set follows was
  refused with C000H by a handler that streams, but a handler given the
  decoded data set was called with a nil one and its status sent:
  pynetdicom's request got Success from `storescp`, which wrote a 316-byte
  file with an empty data set. PS3.7 9.3.1.1 requires the data set. The
  check now comes before either kind of handler is chosen, so both answer
  C000H without being called, and `CStoreRequest.DataSet` is never nil for
  a handler given the decoded data set.

- **A refused caller is left to close the connection** (#430). An SCP sent
  its A-ASSOCIATE-RJ and closed the connection behind it. PS3.8 9.2 has the
  acceptor start ARTIM and wait for the caller to close (AE-8, Sta13), and
  close it itself only when the timer expires (AA-2). The close could reach
  a caller before it had acted on the rejection: pynetdicom, on a loaded
  machine, then held the PDU with the right result, source and reason and
  did not count the association as rejected. The SCP now waits, up to
  `NetworkConfig.ARTIMTimeout`, and answers nothing that arrives meanwhile.

- **A server at its association limit says it is full** (#318). A caller
  over `SCPConfig.MaxAssociations` was refused from the service-user with
  reason 2, which from that source is "application context name not
  supported": dcmtk's `echoscu` printed `Reason: App Context Name Not
  Supported`, a misconfiguration on the caller's side, when the server was
  only busy. The refusal now comes from the service-provider (presentation
  related function), and reason 2 from there is local-limit-exceeded
  (PS3.8 Table 9-21). It is still transient, so a caller tries again.

- **Six bytes no longer buy 128 MiB of a server's memory** (#323). A PDU's
  body was allocated from the length in its header before any of it had
  arrived, for every PDU type, up to `MaxPDULengthLimit`. The first PDU on
  a connection is read before the called AE title is looked at, so eight
  connections each sending the header of a 128 MiB A-ASSOCIATE-RQ and then
  1 MiB took a default server's heap from 2 MiB to 1039 MiB and held it
  until ARTIM expired. A length is now checked against what a PDU of its
  type can be, from the header: an A-ASSOCIATE-RQ or -AC against the new
  `MaxAssociatePDULength` (1 MiB; all 128 presentation contexts with 40
  transfer syntaxes each are 358 KB), an A-ASSOCIATE-RJ, A-RELEASE-RQ,
  A-RELEASE-RP and A-ABORT against the four bytes PS3.8 gives them, and a
  P-DATA-TF against the Maximum Length the SCP advertises, before the
  association as well as after. A PDU of an unknown type is refused without
  its body being read. A body within its bound is allocated as its bytes
  arrive, so memory follows what a peer has sent: a P-DATA-TF stating
  128 MiB and sending 1 MiB costs 3 MiB.

- **The conformance statement names every JPEG 2000 feature the decoder
  refuses** (#340). §8.1 listed five, and the decoder refuses more: three of
  the five progression orders (RPCL, PCRL, CPRL), a region of interest,
  predictable termination, components deeper than 16 bits and a JP2 box with
  a 64-bit length. A reader expecting an RPCL file to decode would have been
  told nothing. Each is now one line, with the error text a caller sees.

- **The irreversible JPEG 2000 tolerance is stated as measured** (#360). The
  statement gave one sample against another decoder. Against OpenJPEG that
  holds up to 12 bits; at 16 bits OpenJPEG lands up to two away, and this
  decoder is the nearer of the two to what was encoded.

- **A panic in a handler ends its association, not the server** (#325).
  Nothing in `network` recovered from a panic. A handler is the
  application's code, called on a goroutine the SCP started, so a nil map
  or an index out of range in one, reached by a single instance from a
  single peer, ended the process: the listener and every other association
  in progress. A panic in a DIMSE handler, including the streamed C-FIND,
  C-GET and C-MOVE handlers that run on a goroutine of their own, is now
  logged with its stack, the peer is sent an A-ABORT (service-provider,
  reason-not-specified), and that association's slot is given back; the
  associations beside it go on and the next caller is served. No status is
  sent for the message in progress, since how far it got is not known. A
  panic in an event handler is logged and the handlers registered after it
  are still called.

- **The JPEG 2000 decoder is checked against OpenJPEG in CI** (#322). The
  interoperability check that decodes an OpenJPEG codestream built its
  fixture with numpy, which the CI venv does not have, and reported that as
  a skip, so it never ran. Where it did run, the fixture was wrong:
  OpenJPEG's PGX reader takes the precision from the largest sample, so
  `MR_small.dcm` was encoded signed 12-bit and every sample over 2047
  clipped. The fixture is now built from the data set's own samples with
  the depth stated, OpenJPEG must say 16 bits and return the original before
  the decoder is asked, a fixture that cannot be built fails, and the job
  fails if the check did not pass. The decoder itself was right.

- **An N-GET handler is given the attributes that were asked for** (#380).
  `NGetRequest.AttributeList` was always empty: the SCP never read
  Attribute Identifier List (0000,1005) from the request, and to a handler
  an empty list means every attribute (PS3.7 10.1.2.1.4). pynetdicom asking
  a Media Creation SCP for Execution Status got a handler that saw no list.
  The list now reaches the handler in the order it was sent. A list whose
  length is not a whole number of tags is refused with 0x0110 Processing
  failure, and the handler is not called.

- **An N-service response's data set is read with it** (#379).
  `SCU.NCreate`, `NSet`, `NAction` and `NEventReport` read the response's
  command and stopped. When the peer sent a data set after it, as PS3.7
  10.1 lets it for each of them, the data set stayed on the association
  and the next operation read it as its own response. Against pynetdicom
  as an MPPS SCP that returns the attribute list on N-CREATE, the N-SET
  that reports the step complete panicked. Where the stray data set
  decoded as a command, the status read from it was usually 0, so a
  failure was reported as success. Each response is now read whole, by its
  message ID, as the C-services read theirs. Its data set is in the
  response's `DataSet`, which was always nil, and a response that does not
  decode is an error. The SCP side had the mirror defect; see the next
  entry.

- **An SCP sends the data set its handler returns with an N-service
  response** (#399). `NCreateResponse.DataSet`, `NSetResponse.DataSet`,
  `NActionResponse.DataSet` and `NEventReportResponse.DataSet` were dropped:
  the SCP sent only the status, with a Command Data Set Type saying no data
  set follows. An MPPS SCP could not return the attribute list of the step
  it created (PS3.4 F.7.2.1.2); pynetdicom read an empty one. An N-ACTION
  whose reply carries information could not reply. Each is now sent after
  its response, and the Command Data Set Type says a data set follows only
  when one does (PS3.7 E.2). A data set that cannot be encoded is answered
  with 0x0110 Processing failure and no data set. N-GET, the one that did
  send its data set, sent an empty one after such a failure and reported
  success.

- **An HTJ2K or JPEG XL image is refused by name, not read as samples or
  sent as them** (#404). Pixel data in a syntax with no decoder here was
  taken for native. `storescu` sent pydicom-data's HTJ2K file to dcmtk's
  `storescp` as Explicit VR Little Endian with its fragments as the pixels,
  and both sides reported success; rendering and redacting refused with a
  count of bytes, or, where the fragments were longer than the image,
  rendered codestream bytes and wrote black into them. Encapsulated pixel
  data in High-Throughput JPEG 2000 (`.201`, `.202`, `.203`) or JPEG XL
  (`.110`, `.111`, `.112`), or any other syntax without a decoder, is now
  refused by `RenderFrame`, `PixelArray*`, `DecompressInPlace`, redaction
  and the network transcoder with the syntax's name, and the six UIDs are
  in the `uid` dictionary. The file is still read, stored, de-identified
  and sent as it is to a peer that accepts its syntax. What exists to
  decode them against is in #403.

- **A caption over saturated texture no longer takes a third of the scan
  with it** (#367). The text detector found the caption, then grew its
  region over every pixel of the caption's color, and over texture
  stretched to full white every pixel is: the region for a caption 94
  pixels wide ran across the scan and down it, and `-redact auto` would
  have blacked out a third of the picture. Over texture a line is now grown
  only where its ground is plain. The detector's other known misses are now
  measured by a test rather than remembered: blurred labels of two or three
  characters and a blurred clock are still not found (0 of 7), so `auto`
  stays not the default. Across pydicom's corpus every region is unchanged.

- **An AE title padded on the left is the same title.** Leading and
  trailing spaces of the calling and called AE titles are both
  non-significant (PS3.8 Table 9-11), and only the trailing ones were
  removed: a caller asking for `"  ARCHIVE"` was refused as asking for
  another title, and a handler was told the calling title with its spaces.

- **A data set that shares a P-DATA-TF with its command is no longer lost**
  (#320). A P-DATA-TF may carry several PDVs, and the last fragment of a
  command may travel with the first of its data set. The PDVs after the
  fragment a read stopped at were dropped with the PDU, so the SCP waited
  for a data set it had already received and aborted the association when
  its timeout ran out. What a read leaves of a P-DATA-TF now waits for the
  next read. dcmtk and pynetdicom send one PDV to a PDU, which is why it
  did not show against them.

- **A value too long for a 16-bit length is refused, not written with a
  wrong length.** UI, AE, SH and the other short VRs state their length in
  16 bits in explicit VR. `filewriter` wrote a longer value with the low 16
  bits of its length: a 70,002-byte Media Storage SOP Instance UID went out
  as length 4466 while the group length (0002,0000) counted all of it, so
  the File Meta Information pointed 65,536 bytes past where its elements
  ended. `WriteDataElement` and `WriteFileMetaInfo` now return an error
  before writing any of the element or the group.

- **A time with fractional seconds is a valid TM, and a DT is checked**
  (#356). `valuerep.ValidateValue` refused `120000.5`, which PS3.5 Table
  6.2-1 allows (up to six digits after the dot), and passed any text as a
  DT. Both now follow the table; `DataElement.ValidateVR` and
  `SetValueWithValidation`, which use it, follow with them.

- **A JPEG-LS frame that ends on 0xFF decodes everywhere** (#385). The
  encoder stopped after the last byte of a scan, and when that byte was
  0xFF the EOI marker followed it at once. The byte after a 0xFF in a
  JPEG-LS scan has its high bit clear (T.87 A.1), so CharLS and libjpeg
  ended the scan a byte early: dcmtk refused the frame, libjpeg decoded its
  last samples wrong, and this library, which reads zeros past the end,
  read it back without complaint. About one frame in 256 sent as JPEG-LS
  Lossless ended so. A scan that ends on 0xFF now has the stuffed zero byte
  after it.

- **A native 4:2:2 image sent as RLE or JPEG-LS was still called
  YBR_FULL_422** (#363). Its chroma was expanded to every pixel before
  coding, and a receiver believing the description expected half of it. It
  is YBR_FULL now, with Planar Configuration 0 for any color image. A
  JPEG-LS send of 8 bits stored in 16 allocated was refused, and so was one
  whose samples used the bits above Bits Stored; both are coded exactly.

- **A value held as a Go string is not absent** (#377). `Dataset.GetValue`
  returned the value only when it was held as `[]byte`, and
  `GetValueByKeyword`, `GetStringByKeyword`, `GetByKeywordWithDefault` and
  `GetStringByKeywordWithDefault` went through it. The library builds some
  elements with a string (de-identification records Patient Identity Removed
  as `"YES"`), so after `Anonymize`, `GetStringByKeyword("PatientIdentityRemoved")`
  answered `""` for an element `ContainsByKeyword` reported present, while
  `show` on the written file printed `YES`. The private-element accessors,
  `String`, `GetStatistics`, `ToJSON` and the text formatter had the same
  assertion. They now ask one helper, which uses `dataelem.ValueBytes`, the
  conversion the writer already used, so reading and writing agree on what an
  element holds. A data set built in memory, one read from a file and one
  decoded from DICOM JSON answer alike. Nothing `show` prints changed.
  Packages other than `dataset` still assert `[]byte` in places; #420 lists
  them.

- **De-identification reads a value held as a list of strings** (#420).
  The UID action knew a value held as `[]byte` or a string, so a UID list
  built in memory as a `[]string`, such as Failed SOP Instance UID List, was
  taken for no UID and kept its original values, which link the result back
  to the source instances; the same list read from a file was replaced.
  Retain Safe Private read a private creator held as a string as no creator
  and dropped the safe attributes it owned, and a De-identification Method
  held as a list lost the automatic redaction note on a second pass. Every
  read now goes through the new `DataElement.Bytes`, the conversion the file
  writer uses, and `dataset` uses it in place of its own helper. A test runs
  every profile over pydicom's corpus twice, once as read and once rebuilt
  with string values, and requires the same result.

- **`GetPixelDataInfo` holds the rescale** (#378). `PixelDataInfo` has
  `RescaleSlope` and `RescaleIntercept`, and nothing ever set them, so both
  were 0 for every image: a CT with intercept -1024 read `0 0`, and
  `624*slope + intercept` came to 0 rather than -400, as pydicom's
  `apply_modality_lut` gives. They now hold Rescale Slope (0028,1053) and
  Intercept (0028,1052), and 1 and 0 when the image has none, since an absent
  slope means no scaling and not a slope of 0. An enhanced multi-frame image
  keeps them per frame in the functional groups; there the fields hold frame
  0's values, its own group's or else the shared one's, and the doc comment
  says so. The rendering code and this share one lookup, which now also finds
  a Pixel Value Transformation whose slope is absent but whose intercept is
  there. Compared with pydicom on the 62 corpus files that have pixels.

- **LUT Data is not reported as a VR mismatch** (#352). The check made when an
  element is added to a data set, when one is validated and when a file is read
  accepted a dictionary entry that offers a choice only if the string was in a
  hand-written list, and there were three lists, each different. None had both
  `US or OW` (LUT Data, (0028,3006), the table in every Modality, VOI and
  Presentation LUT Sequence item) and `US or SS or OW` (Gray Lookup Table
  Data), so reading a file whose LUT Data is OW, or US, logged `VR mismatch for
  (0028,3006): expected US or OW, got OW`, a warning on a conformant element.
  There is now one function, `dataelem.VRMatchesDictionary`, used by all three,
  and it reads the choices out of the dictionary's own string (`VRChoices`), so
  `OB or OW`, `US or SS`, `US or OW` and `US or SS or OW` work, and so would a
  choice a later edition adds. The other places that tested a VR for `" or "`
  by hand use `OffersVRChoice`. Read with the LUT test files, the warning is
  gone, and a VR the dictionary does not offer is still reported.

- **`show file::Element` takes a keyword** (#384). The element was matched
  against its tag and its dictionary name, which for Patient's Name is
  `Patient's Name`, so `show ct.dcm::PatientName` answered `element
  'PatientName' not found` for an element that was there, and so did every
  keyword of two words or more, `StudyInstanceUID` among them. The help's own
  example, `patient.dcm::ImageComments`, was one. The element is now read
  with the parser `diff` uses for its tags, `parseTagOrKeyword`: a keyword, or
  a tag as `0010,0010`, `(0010,0010)` or `00100010`. The dictionary name still
  works, and is all a private element has. A test runs every example in
  `show`'s help text against a file that has what they ask for, so the help
  cannot go stale again. `show --json` is untouched. dcmtk's `dcmdump +P
  PatientName` prints the same value.

- **A private sequence written to an Implicit VR file is still a sequence**
  (#354). Every sequence was written with its byte count. Implicit VR names
  no VR, and a private tag has no dictionary entry, so the only thing that
  tells a reader such an element holds items is an undefined length
  (PS3.5 6.2.2 Note 5). dcmtk, pydicom and this library's own reader read the
  rewritten sequence as one opaque value: pydicom's `nested_priv_SQ.dcm`
  through `modify` came out with `(0001,0001) ?? fe\ff\00\e0...` in
  `dcmdump`. Every command that writes a file did this to an Implicit VR
  file, and to an Explicit VR file's private sequence when it was written
  as Implicit VR. In Implicit VR a sequence the dictionary does not know is
  now written with undefined length and a Sequence Delimitation Item; every
  other sequence keeps its bytes. Across pydicom's corpus, every file written
  back in its own syntax and in Implicit VR reads in pydicom to the elements
  of the original, the three files with private sequences included,
  except two truncated files whose cut-off element this library does not
  read. The network codec had the same defect (#424, below).

- **A private sequence crosses an association as a sequence** (#424). The
  network codec sent every sequence with its byte count, so in Implicit VR a
  vendor's private sequence reached the peer as one opaque value: dcmtk's
  `storescp +B` stored `nested_priv_SQ.dcm`'s `(0001,0001)` as
  `?? fe\ff\00\e0...`, 66 bytes. On receipt it read an undefined-length
  element the dictionary does not call a sequence as encapsulated pixel data,
  so the same instance from pynetdicom's `storescu` was refused with 0110
  ("a pixel data fragment declares 4294967295 bytes but only 179 remain"), on
  every path that hands a handler the decoded data set: `storescp`, `qrscp`,
  queries and storage commitment. Inside an item such an element silently
  ended the item. Sending now follows the file writer's rule, undefined
  length and a delimiter for a sequence the dictionary does not know in
  Implicit VR, and receiving follows the file reader's: undefined length on
  anything but pixel data is a sequence, and a UN element's items are
  Implicit VR Little Endian (PS3.5 6.2.2). Both writers and both readers ask
  `tag.Tag` the same two questions, `SequenceNeedsUndefinedLength` and
  `IsPixelData`. `nested_priv_SQ.dcm` and `UN_sequence.dcm` now cross in
  Implicit VR from pynetdicom to `storescp`, and from `storescu` to dcmtk's
  `storescp` and to pynetdicom, and read in pydicom element for element like
  the original. dcmtk's own `storescu` still sends a private sequence with its
  byte count, which no receiver can tell from a value; dcmtk's `storescp`
  stores it so too.

- **A file-set does not take in a data set with no File Meta Information**
  (#394). `FileSet.AddFile` and `ScanDirectory` accepted a bare data set,
  with no preamble, no `DICM` and no group 0002, when it carried an
  instance's UIDs, and `WriteDICOMDIR` gave it a record. A file in a
  File-set is in the DICOM File Format (PS3.10 7.1, 8.1): dcmtk's `dcmmkdir`
  refuses pydicom's `rtstruct.dcm` with "file not in part 10 format (no file
  meta information)", and pydicom's `FileSet` could not load the file the
  DICOMDIR named. `AddFile` now returns an error wrapping the new
  `fileset.ErrNoFileMeta`. A directory scan leaves such a file out and goes
  on, because a scan that failed on the first stray file would index
  nothing. The new `FileSet.Scan` says which files it left out and why
  (`ErrNoFileMeta`, `ErrNotDICOM`), and `ScanDirectory` is the same scan
  returning the count. The `dicomdir` command checked the file's first
  bytes itself; it now prints the library's answer, and its output is
  unchanged. Over 68 corpus files, the files the library leaves out for
  missing File Meta Information are the three `dcmmkdir` refuses for it,
  and pydicom's `FileSet` loads all 64 the DICOMDIR names.

- **A native YBR_FULL_422 image compressed to JPEG 2000 says YBR_FULL**
  (#374). `CompressJPEG2000LosslessInPlace` coded the image with its chroma
  expanded to every pixel and kept the description `YBR_FULL_422`, which
  PS3.5 Table 8.2.4-1 does not allow for JPEG 2000, and a reader that
  believed it looked for half the chroma there was. The RLE and JPEG-LS
  methods had been fixed for the same thing, and the lossless JPEG and
  JPEG-LS Near-Lossless methods had copies of that fix; every
  `Compress…InPlace` method now describes its result through one method.
  All of them, run on ten corpus and generated color and grayscale images,
  give a Photometric Interpretation and Planar Configuration PS3.5 8.2 allows
  for the syntax, and dcmtk decodes every lossless result to the samples
  that were coded.

- **A frame coded at 8 bits in a 16-bit image decodes to the whole image**
  (#362). The JPEG-LS, lossless JPEG and JPEG Baseline/Extended decoders
  unpacked a frame at its own precision, one byte a sample at 8 bits or
  fewer, so an image of 16 bits allocated whose frames were coded at 8 came
  back from `DecodedPixelData` at half its size with no error, and was read
  as 16-bit samples by everything after it. T.81 and T.87 allow such a frame;
  dcmtk's `dcmdjpeg` decodes it, `dcmdjpls` refuses it, and pydicom's main
  branch widens a JPEG-LS one. Every bundled decoder now writes the width
  Bits Allocated says, through the new `compress.FrameDecompressor`
  interface that JPEG 2000's `DecompressFrame` already had the shape of, and
  refuses a frame coded at more bits than Bits Allocated instead of writing
  it two bytes a sample or, for JPEG 2000, keeping its low bits. A decoded
  frame shorter than the image is now an error from `DecodedPixelData` too,
  as it already was from `DecompressInPlace`.

- **A value held as a Go string or number is read in every package**
  (#420). `validate`, `fileset`, `network`, `sr`, `dcmstore` and the
  commands read a value only when it was held as `[]byte`, so a data set
  built in memory read differently from the same data set read from a file:
  a file-set of such data sets could not be indexed (its UIDs read as
  absent), a storage commitment report read as no instances, a UPS state
  as none, a command set built with numbers could not be encoded, a SOP
  Instance UID held as a `[]string` was sent as `[1.2.3]`, an SR lost its
  text and its by-reference items, and the store indexed a person name with
  its other groups as the alphabetic group alone. Each now reads through
  `DataElement.Bytes`. Files read from disk hold `[]byte` and read as
  before; `show --json` is unchanged.

- **A refusal is logged once** (#476). An SCP logs each refusal of
  `SCPConfig.Admit` at the info level, and `LoggingEventHandlers` logged its
  `EVTAssocRejected` again, so a server logging its events had every refusal
  twice. Making the SCP give way cannot work, because a `Server` always has
  an `EventManager`. So the event handler gives way, and only when the
  SCP's line went to the same log: at the default warning level, where the
  SCP's line is not written, the handler's line is still the one report. An
  SCP with no admission function now logs its refusal of a called AE title
  that is not its own, which before only the event handler reported.

- **A JPEG whose Photometric Interpretation is RGB is no longer decoded as
  YCbCr** (#443). The decoder let image/jpeg guess the color space from the
  stream, and with no JFIF or Adobe marker and component identifiers other
  than R, G, B it guesses YCbCr and converts. pydicom's
  `SC_jpeg_no_color_transform.dcm` is such a frame: a gray picture, which
  came out magenta, `[255 122 255]` where dcmtk shows `[244 244 244]`, in
  decoded pixel data, `decompress`, `convert`, rendering, the DICOMweb
  frames and rendered endpoints and `share`. **Pictures change color with
  this fix, and v1.10.0 shows the same magenta.** PS3.5 8.2.1 has the data
  set say what the components are, so with RGB, YBR_FULL or YBR_FULL_422
  they are now returned as stored, whatever markers the stream carries, as
  dcmtk's `dcmdjpeg` decodes by default; a YBR frame with an Adobe marker
  saying RGB is no longer left half-decided either. Every color JPEG in
  pydicom's corpus renders within one level of dcmtk's picture; this one
  was 137 levels off. CONFORMANCE §8.1 says where pydicom 3 decides
  differently.

- **JPEG-LS thresholds follow T.87 at every precision, and a frame read with
  the wrong ones is refused by name** (#422). Near-lossless coding with a
  large NEAR clamped a default threshold above MAXVAL to MAXVAL, where T.87
  C.2.4.1.1 sets it to the threshold below: at 8 bits and NEAR 127 the frame
  was coded with 255, 255, 255 instead of 128, 128, 128, and CharLS and dcmtk
  refused it while libjpeg decoded it to a different image. The same holds
  from 3 bits upward wherever NEAR is large, and frames of theirs failed
  here the same way. An LSE segment that gave some thresholds and left the
  others at zero was refused instead of having each zero one defaulted. At 6
  and 7 bits this library was right and dcmtk is not: its bundled CharLS
  predates the 2.1 fix of the default thresholds there, so each refuses the
  other's frames. A 7-bit dcmtk frame decoded here without an error to an
  image up to 127 levels off; a scan whose samples are all decoded with more
  than a byte of its data unread is now refused, and at 6 and 7 bits the
  error says dcmtk's thresholds may be the cause, gives both sets, and how
  dcmcjpls codes a frame everyone reads. Lossless frames from 2 to 8 bits
  are byte for byte CharLS 2.2's; CONFORMANCE §8.2 has the comparison.

- **A JPEG-LS run that reaches the end of a line is coded as T.87 codes it**
  (#453). When the line ended partway through a block, the encoder wrote a 0
  bit and the length left, as for a run that a sample interrupts, where T.87
  A.7.1.2 writes a single 1 bit. Every decoder read both to the same
  samples, so no image was wrong, but such a line cost up to 15 bits more
  and the output could not be compared with CharLS's byte for byte. Frames
  are now byte for byte CharLS 2.2's at every precision from 2 to 16,
  lossless and near-lossless (above 12 bits CharLS adds an LSE segment
  repeating the default parameters). Frames written by 1.10.0 still decode.

- **A JPEG decoded to RGB is converted as libjpeg converts it** (#497). The
  conversion was image/color's, which scales Y with the products and
  truncates, so a frame decoded straight to RGB differed from dcmtk and
  Pillow by up to two levels where their inverse DCT agreed with this one,
  and three on pydicom's corpus. It now rounds as libjpeg's `jdcolor.c`
  does: on a frame of flat blocks every sample equals Pillow's and dcmtk's,
  where one in six differed, and every color JPEG in the corpus is within
  one level of both except `examples_ybr_color.dcm`, at two, from the
  inverse DCT.

- **A value read from a stream costs what arrived, not what its header
  states, and a read can set a maximum** (#458). A declared length was held
  to the bytes left only when the source could seek; from a socket or an
  upload body the value was allocated whole first, so a 200-byte stream
  whose first element stated 1 GiB cost 1 GiB. A File Meta element was never
  checked at all, from any source. Such a value is now read in pieces as its
  bytes arrive, never holding more than twice what came, and a meta element
  is held to the size of a file like any other. `ReadOptions.MaxValueLength`
  refuses a longer value before allocating it, with a `*ValueTooLongError`
  (`errors.Is(err, filereader.ErrValueTooLong)`) naming the attribute, its
  tag, offset and stated length; zero, the default, is no maximum. With
  `SkipBulkData`, Pixel Data is passed over and everything read is held to
  the maximum.

- **The frames of one image rendered over DICOMweb share one scale when
  neither the request nor the image gives a window** (#489). Each frame was
  drawn from a data set holding only that frame, so with no window it was
  stretched over its own lowest and highest value: frames of a 12-bit image
  holding 0 to 999, 1000 to 1999 and 0 to 3999 came out as the same picture,
  one gray a different value in each. PS3.3 C.11.2.1.2.2 makes the VOI LUT
  the identity when an image has neither a window nor a VOI LUT, so a frame
  is now drawn over the range its Bits Stored can hold, through its rescale
  or Modality LUT; those three frames end at 62, run from 62 to 124, and end
  at 249. The range is found without reading a pixel. An image whose values
  fill little of it, 32-bit RT Dose for one, is dark until a `window` is
  given. `RenderOptions.StoredRange` asks `RenderFrame` for this; without it
  `RenderFrame` and `convert` keep spanning the values across all frames.
  Orthanc 1.13 stretches each frame to its own values, as this server did.

- **A server or client given its own logger gets the warnings of what it
  receives** (#503). The data sets and command sets the network decoded were
  built with `dataset.Add`, whose validation warnings (a retired attribute, a
  VR the dictionary does not give) went to `config.Logger` whatever logger
  the server or client had: the corpus sent as C-STOREs wrote 14 lines to the
  global logger and none to the server's. They are now built with
  `dataset.AddWithContext` under a context carrying that logger, as a file
  read with its own logger builds its data set (#481): C-STORE, the C-FIND,
  C-MOVE and C-GET identifiers and results, the C-GET sub-operations a client
  receives, the N-service data sets both ways, storage commitment, and the
  command sets. A server's handlers, and a client's `OnCStore`, are given the
  same context, so the built-in UPS handler's warnings, as it applies an
  N-SET, go to the server's logger too. With no logger given, nothing
  changes. Never in a release.

- **A JPEG-LS frame with restart markers decodes** (#452). A frame coded with
  a restart interval, a DRI segment and an RSTm marker every so many lines
  as T.87 allows, was refused as corrupt at the first marker: the scan was
  read straight through it, without starting the decoder again or moving to
  the byte after it. A frame whose only marker came near the end could
  decode with no error to a wrong last few lines. CharLS's restart test
  files now decode to the T.87 test images, as libjpeg decodes them, and
  1237 generated frames at 2 to 16 bits in all three interleave modes decode
  as libjpeg decodes them. A marker missing, out of order or where the DRI
  does not put it is refused with an error saying so.

- **`-redact auto` no longer blacks out half of a scan with no text in it**
  (#536). On pydicom's `examples_overlay.dcm`, an abdominal MR, smoothed as
  a picture resampled by half a pixel is, the text detector took three
  blobs of anatomy at a quarter of the picture's size for large lettering
  and grew the line over the scan: one region of 46.6% of the picture, which
  `anonymize -redact auto` and `share -redact auto` filled with black. Taken
  at sixteen offsets and five kinds of smoothing, the same scan is marked
  over 38% in 40 of 80 pictures, up to 86.5%. No rule of the detector was
  found that tells those blobs from large lettering at every offset without
  losing lettering, so the detector is unchanged and the redaction is
  guarded instead: when the regions found cover more than a quarter of the
  picture, nothing is filled, the command fails naming the share, and in a
  directory the other files are still written. On every scan of the corpus
  without text the detector marks either 1.6% or less or 38.7% or more; on
  the real screens it marks at most 17.8%, and on the scans the tests draw
  labels over at most 20.0%. A picture that is nothing but large lettering
  is over a quarter too and is refused, which destroys nothing: its regions
  can be given with `-redact x,y,w,h`. `-detect` and `share -preview` still
  show everything found. In Go, `RedactDetectedText` returns an error
  wrapping `ErrImplausibleRedaction`, and `CheckDetectedShare` applies the
  same bound to a caller's own regions.

## [1.10.0] - 2026-11-17

Three things the library could only do from the other side, it now does
itself, and the command line is finished where programs and people lean on
it:

- **A DICOMweb server.** Package `dicomweb` was a client. `dicomweb.NewServer`
  is the origin server: search (QIDO-RS), retrieve with metadata (WADO-RS)
  and store (STOW-RS), over any backend, and `dcmstore.Store` is one. So
  `qrscp -web <address>` serves one archive over DIMSE and over HTTP, on
  this machine only unless a bearer token is given.
  Frames, rendered images and bulk data URIs are not served.
- **A JPEG 2000 lossless encoder.** `compress/jpeg2000` decoded and nothing
  wrote the format. A data set is compressed to JPEG 2000 Lossless in
  place, and a send is transcoded to it when that is what the peer accepts.
  OpenJPEG returns every sample of what it writes. Lossy is not written.
- **Text burned into the pixels is found.** `anonymize -redact auto` and
  `share -redact auto` black out the lettering a pure Go detector finds,
  `-detect` prints the regions, and `share -preview` shows them. A detector
  can miss, so `auto` is never the default, what it did is recorded as
  unverified, and only a person looking confirms an image is clean.
- **The command line.** `show file::Element` prints a whole value and the
  table has `--full`; the table prints every value of a number; `STORED`
  carries its association's id; the commands print their own output and not
  the library's log; a file with elements written without a VR says so in
  its warnings.
- **The archive.** Its index is UTF-8, so a C-FIND answer declares its
  character set and non-Latin names match; a person name matches whatever
  its case; a date written `1997.04.24` matches a range; an answer carries
  the size of the image.
- **Releases.** A release merged into `main` tags and publishes itself.

Nothing in the format of `show --json` changes: `schemaVersion` is still 1.
A program reading it sees two differences from 1.9.1: the new warning on a
file that has elements written without a VR, and an empty value of a bulk
binary VR, which was printed as `"value": ""` and now has none.

For programs using the library there are additions and no changes; nothing
is removed or renamed.

### Added

- **Text burned into the pixels is found** (#310). `-redact` took rectangles
  a person had measured. `anonymize.NewTextDetector` now finds lettering in
  a rendered frame: detection, not recognition, in pure Go, with no OCR
  engine and no new dependency. It marks the pixels that stand out from
  their surroundings, keeps the connected shapes that are drawn like
  characters, and takes runs of them that line up, in one color, for
  lines of text, at several scales. On the commands:
  `anonymize -redact auto` and `share -redact auto` black out what it
  finds; `-detect` prints the regions and writes nothing; `share -preview`
  writes the picture with the regions outlined. A detector can miss, and
  a missed name is a leak, so `auto` is never the default, an image at
  risk is still refused unless something is asked for, an automatic
  redaction is recorded in De-identification Method as "Burned-in text
  redacted where detected, unverified", and Burned In Annotation is not
  set to NO by it. Measured on pictures the tests draw, where every
  letter's place is known: 24 of 24 lines of text covered whole, in five
  sizes, four colors, on plain grounds and beside and over ultrasound-like
  texture, with at most 3.9% of a picture marked beside the text, and a
  picture of texture alone left unmarked. On the two ultrasound screens
  in pydicom's corpus every caption block is found; in the one whose
  lettering is six pixels tall and blurred, a clock and three two- and
  three-letter labels are not. `anonymize.TextDetector` is an interface,
  so a program with a recognition engine can use its own.

- **A JPEG 2000 lossless encoder** (#308). `compress/jpeg2000` decoded and
  nothing wrote the format. `jpeg2000.Encode` now writes an image as a
  lossless codestream (ISO/IEC 15444-1): the reversible 5/3 wavelet, and
  the reversible component transform for three components, at 1 to 16
  bits, signed or unsigned. One tile, one quality layer, code-blocks of
  64 by 64. Each stage is the mirror of a decoder stage and is tested
  against it: the MQ coder, the wavelet transform, the block coder
  (`EncodeBlock`) and the packet headers, with the rules for a
  coefficient's context shared by both so they cannot differ. OpenJPEG
  decodes what it writes: across 360 images of every depth, sign and shape
  tried, `opj_decompress` returned each sample as it was encoded. The next
  entry is what makes `1.2.840.10008.1.2.4.90` something a file or a send
  can be compressed to.

- **A file or a send can be compressed to JPEG 2000 Lossless** (#308).
  `Dataset.CompressJPEG2000LosslessInPlace` replaces the Pixel Data with
  `.90` frames and makes the data set say so: an RGB image becomes
  `YBR_RCT` with Planar Configuration 0, as PS3.5 8.2.4 has it, and decodes
  back to RGB. `1.2.840.10008.1.2.4.90` is now a transfer syntax a send
  transcodes to, as RLE and JPEG-LS Lossless are, so a peer that accepts
  only JPEG 2000 can be sent an uncompressed image. An image held in
  another compressed syntax is decoded first. A frame is coded at Bits
  Stored, or at Bits Allocated when a sample uses the bits above it, so
  what was in the file is what comes back. `compress.EncodeJPEG2000Lossless`
  codes one frame of native samples. Checked in the interoperability suite,
  which now has OpenJPEG: every uncompressed image of pydicom's corpus is
  written as `.90`, each frame is decoded by `opj_decompress` to the
  samples of the source, dcmtk reads each file, and dcmtk receives one over
  the network with its codestream intact.

- **A DICOMweb search service** (#294). Package `dicomweb` was a client; it
  now has the server's side of QIDO-RS (PS3.18 10.6).
  `dicomweb.NewServer(backend)` is an `http.Handler` for `/studies`,
  `/series`, `/instances` and the same within a study or a series. A search
  is turned into the identifier a C-FIND would carry and put to the
  archive, so what matches over DIMSE matches over HTTP: wildcards, date
  ranges, lists of UIDs and attributes inside a sequence
  (`RequestAttributesSequence.RequestedProcedureID=...`), with
  `includefield`, `limit` and `offset`. The answer is DICOM JSON, or 204
  when nothing matches. `dicomweb.Backend` is what the server asks of an
  archive, and a `dcmstore.Store` is one as it is. Compared with Orthanc
  over 77 files of pydicom's corpus, 25 of 26 searches return the same
  results and the same values, once the matching fixes listed under Fixed
  and Changed were in; the one left is a date range, where Orthanc also
  returns the studies that have no date. The entries after this one add
  the archive's side, the command, retrieving and storing.

- **The archive answers with the size of an image** (#294). The index held
  the C-FIND keys of PS3.4. It now also holds what a DICOMweb search
  returns for each level (PS3.18 10.6.3.3): Rows, Columns, Bits Allocated,
  Number of Frames, Timezone Offset From UTC and Performed Procedure Step
  Start Date and Time, and Instance Availability is answered `ONLINE`.
  They are return keys of a C-FIND as well, and can be matched on: a
  number is matched whether a query carries it as bytes or, from a URL, as
  text, and is answered as a number of its VR. The index file is version
  3, so an index written before is rebuilt once on opening. Against
  Orthanc over the same 77 files, a search returns every attribute
  Orthanc does.

- **`qrscp -web` serves the archive over HTTP as well** (#294).
  `go-dicom qrscp -web 8080` adds a DICOMweb search service at
  `http://127.0.0.1:8080/dicom-web` over the store its DIMSE side writes
  to, so what a modality stores a web client finds. A port alone listens
  on this machine only. An address other machines can reach is refused
  unless `-web-token` or `DICOMWEB_TOKEN` gives a bearer token, which every
  request must then carry. Under `-tls` the service is HTTPS with the same
  certificate. Each request is printed with its method, path and status,
  and never its query, which is where a search carries a patient's name.
  The interoperability suite stores three files with pynetdicom over DIMSE
  and searches for them over HTTP with a client that is not this library:
  the answers agree with pydicom's reading of the files, with a C-FIND on
  the same archive, and with Orthanc holding the same files.

- **The archive is retrieved from over HTTP** (#294). The DICOMweb server
  gains WADO-RS (PS3.18 10.4): a study, a series or an instance as
  `multipart/related; type="application/dicom"`, and `/metadata` for each
  as DICOM JSON without its bulk data. With `transfer-syntax=*`, or a
  request that accepts anything, an instance is the file the archive holds,
  byte for byte. A request that names no transfer syntax asks for Explicit
  VR Little Endian, and an instance held compressed is decoded for it. Any
  other syntax is given the instances already in it: 206 when some are
  not, 406 when none is. One instance asked for as `application/dicom` is
  the file itself, which is what `curl -O` wants. A search result now
  carries its Retrieve URL. `dicomweb.Backend` gains `Locate` and `Open`,
  which `dcmstore.Store` has. In the interoperability suite, files stored
  by pynetdicom are retrieved by urllib: the pixel data is what was sent,
  an RLE image decoded for the default syntax has the pixels pydicom
  decodes, the metadata has every attribute Orthanc's has with the same
  values, and `go-dicom wado` writes the bytes the server sent.

- **The archive is stored to over HTTP** (#294). The DICOMweb server gains
  STOW-RS (PS3.18 10.5), which completes it: `POST /studies` and
  `POST /studies/{study}` take Part 10 files as the parts of one request.
  Each instance is kept or refused on its own, and the answer lists both:
  200 when all were kept, 202 when some were, 409 when none was. An
  instance sent into a study it is not of is refused with A900H. A request
  is bounded at 4 GiB unless `dicomweb.WithMaxStoreBytes` says otherwise.
  `dicomweb.Backend` gains `Put`, which `dcmstore.Store` has. With this an
  instance stored over HTTP is found by C-FIND, and one stored over DIMSE
  is retrieved over HTTP: the interoperability suite does both with
  pynetdicom and urllib, and `go-dicom stow` against the archive as well.

- **`show` prints a whole value when asked for one, and `--full` prints
  them all** (#282). The table printed the first hundred bytes of a value,
  and so did `show file::Element`, so a long text value or a list of numbers
  could be read from the command line only through `--json`. Asking for one
  element now prints its value whole. The table keeps its cut, since it is
  read in a terminal, and `--full` lifts it. Bulk binary such as Pixel Data
  is a count of bytes either way, and stays in the file.

- **A handler knows which association it is answering, and `STORED` says
  so** (#284). The events of an association carry an id, and a handler's
  context did not, so `storescp` and `qrscp` wrote their `STORED` line with
  none and a reader paired it with the `EVT_C_STORE` before it by SOP
  Instance UID, which two associations sending the same instance at once
  make ambiguous. `network.AssociationInfo.ID` is that id, and the `STORED`
  line now has `associationId`, `role` and the AE titles and addresses, as
  the association's own events do.

### Changed

- **A file with elements written without a VR says so in its warnings**
  (#305). A file that declares explicit VR and holds elements with none is
  read, each such element as implicit, as pydicom reads it. That it
  happened was said only in the library's log, which the commands silence
  since #283, so nothing a program could read said the file is not
  conformant. `DICOMFile.Warnings`, and so `warnings` in `show --json`, now
  has one line for it: how many elements, and the first. The log keeps a
  line for each at debug level. Elements inside a sequence written as UN
  are not counted: there the rule is implicit VR (PS3.5 6.2.2). Across
  pydicom's corpus the warning appears for the one file pydicom warns of
  for the same reason, and `show --json` is otherwise unchanged.

- **The archive matches a person name whatever its case** (#301). A C-FIND
  for `smith*` did not find `SMITH^JOHN`. PS3.4 C.2.2.2.1 lets an SCP
  ignore case for person names, Orthanc and dcm4chee do, and modalities
  disagree on how they write one, so matching as written found only some
  of a patient's studies. Single value and wildcard matching of a PN now
  ignore case. Every other VR is matched as before, and accents are kept:
  `Jerome` does not match `Jérôme`. CONFORMANCE.md section 2.7 lists this
  and the archive's other choices in matching.

- **A release merged into `main` publishes itself** (#287). The release
  workflow ran only for a pushed tag, so a release sat merged and
  unpublished until someone tagged the right commit by hand. It now also
  runs on a push to `main`: when `main.go`'s version has no tag and the
  changelog has that version's section, it builds and publishes, and the
  release carries the tag. Any other push to `main` builds nothing. A tag
  pushed by hand is released as before, and starting the workflow by hand
  builds every binary and publishes nothing.

### Fixed

- **The commands print their own output and not the library's log** (#283).
  The library logs what it notices as it reads: a retired attribute, an
  element written without its VR. Under a command those lines went to the
  terminal in the logger's format, among the command's output: over
  pydicom's corpus `show`, `info`, `convert` and `codify` each printed
  forty of them, and `anonymize` and `share` printed them too. A command
  now logs the library's errors alone. `GODICOM_LOG=warn`, `info` or
  `debug` brings the rest back to diagnose a file. A program that uses the
  packages as a library keeps the logger it set.

- **The table prints every value of a number** (#282). A binary number VR
  was printed as a number only when it held exactly one: `256\0\16` came
  out as `<6 bytes>`, and some came out blank or as bytes that are not
  valid UTF-8. The table now formats numbers with the function `--json`
  uses, so the two agree. Floats are printed as `--json` prints them, `2`
  and `0.5` where the table had `2.000000` and `0.500000`. Across pydicom's
  corpus 289 of 9,117 rows change, all of them numbers. `--json` is the
  same byte for byte but for one thing: an empty value of a bulk binary VR
  was printed with `"value": ""`, the only place such a VR had a value, and
  now has none, as when it holds bytes.

- **DICOM JSON carries the values of a data set built in memory** (#295).
  `Dataset.ToDICOMJSON` wrote a value only when the element held it as
  bytes, as the file reader leaves it. An element given a string, a list, a
  number, a `PersonName` or a tag was written with its VR and no `Value`,
  with no error, so a data set that wrote to disk and sent over DIMSE whole
  came out of JSON empty. The JSON writer now renders a value with
  `dataelem.ValueBytes`, as the file writer and the network encoder do, and
  one that does not fit its VR is an error that names the element. The JSON
  of a data set read from a file is unchanged.

- **A C-FIND answer says which character set it is in** (#297). The archive
  indexed an instance's text as the bytes it arrived with and answered a
  C-FIND with them and no Specific Character Set, so a peer could not read
  a name outside ASCII: of seven such names in pydicom's character set
  files, pynetdicom read none correctly when `go-dicom storescu` had sent
  them and two when dcmtk had. The index now holds text as UTF-8 however
  an instance arrived, a query is read in the character set it declares,
  so `Jérôme` in UTF-8 finds a name stored in ISO_IR 100, and an answer
  with anything outside ASCII declares `ISO_IR 192`. All seven names now
  read back as they are in the files, for both senders. An answer in ASCII
  is unchanged, except that a query which declared a character set no
  longer gets an empty one back. The index file has a version, and an
  index written before is rebuilt from the files once, on opening.

- **A date written `1997.04.24` matches a date range** (#300). That is the
  date form of ACR-NEMA 300, which older equipment still writes and PS3.5
  lets a reader accept. The archive read the old form of a time and not
  of a date, so such a study matched no range and no `19970424`. Found by
  asking Orthanc and the archive for the same range over the same files.

## [1.9.1] - 2026-11-15

`show` is what other programs read a file through, and this release makes
what it prints right in the cases 1.9.0 got wrong:

- **Implicit VR files.** Numbers were printed as bytes, and `show --json`
  wrote the whole of Pixel Data into its output as text: 772 MB of JSON and
  4 GB of memory for a 200 MB image. Each value now has its real VR.
- **Text that is not Latin.** Greek, Arabic, Hebrew, Cyrillic and UTF-8 were
  all read as Latin-1 by `show`, `info`, `convert` and `qido`. They are read
  in the character set the file declares.
- **Memory.** `show` in both its forms leaves bulk data in the file: 18 MB
  for a 200 MB image where it took 218. The output is the same.
- **The file meta group.** `show` lists its first element, (0002,0000),
  which it left out, and a file whose meta group does not state the transfer
  syntax is read as it is actually written, with a warning.

Nothing in the format of `show --json` changes: `schemaVersion` is still 1,
and every offset is where it was. Programs reading it get corrected values
in the cases above and nothing else moves.

For programs using the library there are additions and no changes:
`filereader.ReadDICOMFileWithOptions` and `ReadOptions`, to read a file
without its bulk data, and `filereader.TextEncodings` and `DecodeText`.

### Added

- **A file can be read without its bulk data, and `show` reads it so**
  (#274). `show` prints a bulk value's position, or its first hundred
  bytes, and read every one whole to do it: 218 MB of memory for a 200 MB
  image.
  `filereader.ReadDICOMFileWithOptions` with `ReadOptions.SkipBulkData`
  leaves Pixel Data, the fragments of encapsulated Pixel Data and the
  values of the binary VRs in the file, seeking past them where the stream
  can seek. Each such element has `ValueSkipped` set and keeps its length
  and position. `BulkDataThreshold` reads the short ones. `show` reads
  this way in both its forms: the table needs the first hundred bytes of a
  value at most, and reads those. Either form now takes 18 MB for that
  file, and prints what it printed: across pydicom's corpus the JSON
  document and every row of the table are identical either way.

### Fixed

- **`show`, `info` and `convert` show text in the character set the file
  declares** (#270). They read every text value as Latin-1 whatever
  Specific Character Set said, so a Greek or Arabic name came out as
  accented Latin letters, and so did one in UTF-8. The library decoded them
  correctly all along; the commands did not ask it to. They now use the
  reader's own rules, which `filereader.TextEncodings` and
  `filereader.DecodeText` make available: the data set's declared
  character set, or the enclosing data set's for an item with none, applied
  to the VRs a character set covers. Of pydicom's 17 character set files,
  the name in each of the 15 that have one is shown as pydicom reads it.
  `qido` printed such a name in a search result as a count of bytes, and
  prints it now. `codify` is unchanged, since the code it writes carries
  the bytes and their declaration.

- **`show` settles a VR the file does not state** (#272). An implicit VR
  file carries no VRs, and for some attributes the dictionary offers a
  choice. `show` printed the choice: Pixel Data as "OB or OW", with its
  bytes as an 8192-character string in `show --json`, growing with the
  image, and Smallest and Largest Image Pixel Value as "US or SS" with their
  bytes for a value. Both forms now apply `dataset.ResolveVR`, the rules the
  file writer and the network already used, so the signed or unsigned
  choice follows Pixel Representation, from an enclosing data set where an
  item does not repeat it, and Pixel Data is bulk data given by position.
  A private creator the file gives no VR is shown as LO. Any other
  element nobody knows the VR of keeps its value in `show --json` when the
  value is text, and is given by position when it is not. `convert` and
  `codify` take the same VRs. Across pydicom's corpus no VR is left as a
  choice, and 6,134 of 6,135 are the VR pydicom settles on; the other is
  8-bit Pixel Data in implicit VR, OB here and OW there, where the standard
  allows either.

- **`show` lists the File Meta Information Group Length** (#271). The text
  table began the file meta group at (0002,0001), and `show file::0002,0000`
  answered that the element was not found, though it is the first one in the
  file and `show --json` lists it. Both forms now show it. `convert`,
  `codify` and `info` are unchanged: the element counts the bytes of its
  group, which a writer works out, so it is not carried into what they
  produce.

- **A file meta group that does not state the encoding is said to, and the
  data set is read as it is written** (#273). Transfer Syntax UID
  (0002,0010) is required, and files are written without it. The reader
  took such a file for implicit VR little endian without a word, so a guess
  could not be told from a declaration, and where the data set was in fact
  explicit VR every element of it was lost. The reader now looks at the data
  set's first element, as it does for a file with no meta group, and records
  in `DICOMFile.Warnings` that the UID was missing and what it read the data
  set as. A UID outside the standard's transfer syntax registry is treated
  the same way.

## [1.9.0] - 2026-11-14

This release shows what the library sees: where every element sits in a
file, and what crosses a connection.

- **`go-dicom show file.dcm --json`** gives every element with the offsets
  of its tag, VR, length field and value, nested items and their delimiters
  included, and the preamble, prefix, file meta and data set regions. A hex
  viewer can be drawn from it without a second parser. Its value offsets are
  the ones pydicom records, across pydicom's corpus.
- **`-events trace.jsonl`** on every network command logs what happens on an
  association, one JSON object a line: each PDU with its bytes, each
  presentation context as proposed and as answered, each DIMSE message with
  its status. Programs get the same through `SCUConfig.Events` and
  `SCPConfig.Events`. pynetdicom's own decoder reads every logged PDU.
- **32-bit targets work again.** 1.8.0 did not compile for 386 or 32-bit
  ARM. It does now, the tests run on 386 in CI, and releases carry a static
  `go-dicom-linux-386` that runs on Alpine.

It also corrects `uid.CTImageStorage()`, which returned Enhanced CT's UID,
and a C-STORE handler that could leave the sender waiting.

**Upgrading:** `uid.CTImageStorage()` now returns
`1.2.840.10008.5.1.4.1.1.2`. A program that used it was naming Enhanced CT
Image Storage; `uid.EnhancedCTImageStorage()` returns that. Nothing else
changes for existing code.

### Added

- **`-events` logs what happens on an association, and the library reports
  it** (#260). `network/events.go` declared pynetdicom's `EVT_*` set and
  emitted two of them, from an SCP only; an SCU had nowhere to report to at
  all. `SCUConfig.Events` and `SCPConfig.Events` now take an `EventManager`
  that is told of every PDU either way, with its bytes; of the association
  being requested, accepted, established, rejected, released and aborted,
  with each presentation context as proposed and as answered; of every DIMSE
  message either way; and of each request arriving (`EVTCStore` and the
  rest). The bytes and the contexts are read with `Event.PDUBytes` and
  `Event.PresentationContexts`. An SCP's own associations, such as the one a C-MOVE opens to its
  destination, report to the same place. `EventManager.OnAny` registers one
  handler for all of them. The nine network commands take `-events <file|->`
  and append one JSON object a line. pynetdicom's own PDU classes decode
  every logged PDU to what the log says of it.

- **A linux/386 binary** (#258). Releases carry `go-dicom-linux-386` beside
  the 64-bit builds, for 32-bit x86: older hardware, embedded boxes, and
  emulators with no 64-bit mode. `install.sh` picks it on such a machine.

- **`show --json` says where every element sits in the file** (#259). `show`
  printed the decoded tree and nothing about its bytes, so a tool tying an
  attribute to them needed a parser of its own. `--json` gives, for each
  element, its value and the offsets of its tag, VR, length field and value
  and of its end, for nested items and their delimiters as well, the
  fragments of encapsulated pixel data, and the preamble, prefix, file meta
  and data set regions. The reader records these as it goes:
  `filereader.DataElementValue.Position`, and `DataSetOffset`,
  `MetaGroupLength` and `Deflated` on `DICOMFile`. Across pydicom's corpus
  the positions account for every byte of each file with no gap or overlap,
  and each value offset is the one pydicom records.

### Changed

- **The Linux release binaries are static, and all are smaller** (#258).
  They are built without cgo, so they run on a musl system such as Alpine,
  and every release binary is built with `-trimpath -ldflags "-s -w"`.

### Fixed

- **A C-STORE or C-ECHO response carries the request's Message ID whatever
  the handler returned** (#266). The SCP used the ID in the handler's
  response. A handler returning `&CStoreResponse{Status: StatusSuccess}`
  answered message 0, which the requestor never matched to its request, so
  `SCU.Store` waited out its timeout for an instance that had been stored.
  A handler returning no response and no error made the SCP dereference nil;
  it is now answered `0xC000`.

- **The module builds for 32-bit targets again** (#258). 1.8.0 did not
  compile for `GOARCH=386` or `arm`: the JPEG Baseline encapsulation compared
  a size with a constant that does not fit a 32-bit `int`. 1.7.0 built. CI
  now runs the tests on 386 and builds for 32-bit ARM, which nothing did.

- **A crafted image cannot overflow a size on a 32-bit platform** (#258).
  The first runs of the tests on 386 found two. A JPEG-LS header declaring
  65535 by 65535 passed the size check, because the product wraps a 32-bit
  `int`, and the decoder panicked allocating for it. An RLE segment offset
  past 2^31 is negative as an `int`, passed for one inside the frame, and
  the decoder panicked slicing at it. The size checks of the JPEG-LS, JPEG
  Lossless, JPEG Extended and JPEG 2000 decoders and the RLE offsets are
  compared in 64 bits, and `GetPixelDataInfo` refuses an image whose size
  the platform cannot address, on which every later size depends. 64-bit
  builds were not affected.

- **`uid.CTImageStorage()` returns CT Image Storage, and the UID table names
  the right classes** (#264). The function returned
  `1.2.840.10008.5.1.4.1.1.2.1`, which is Enhanced CT Image Storage, and a
  test pinned it, since 1.0.0. It returns `1.2.840.10008.5.1.4.1.1.2`, and
  `EnhancedCTImageStorage()` is new. `GetUIDInfo` called CT Image Storage
  "CR" and Enhanced CT "CT", and listed `1.2.840.10008.5.1.4.1.1.6.4` as
  Ultrasound, which is not a registered UID. The table now has Computed
  Radiography (`.1.1.1`), CT, Enhanced CT and Ultrasound (`.1.1.6.1`) under
  the standard's names, marks Explicit VR Big Endian retired, and no longer
  calls `1.2.840.10008.1.2.4.91` lossy, which it need not be. The `network`
  package's constants were right throughout.

## [1.8.0] - 2026-11-12

This release takes a study from the archive to something that can be handed
on, without writing a program:

- **De-identify from the command line.** `go-dicom anonymize study/ clean/`
  applies the PS3.15 Annex E profiles to a file or a directory, and
  `-redact x,y,width,height` blacks out text burned into the pixels, in
  compressed images too. An image of a kind that commonly carries such text
  is refused until you say where it is or that there is none.
- **A picture that is safe to share.** `go-dicom share scan.dcm scan.png`
  applies the same rule and writes pixels only. `convert --format png` and
  `Dataset.RenderFrame` render any frame as a viewer shows it: rescaled,
  windowed, MONOCHROME1, YBR and palette color. A windowed CT matches the
  PS3.3 formula to within half a gray level.
- **DICOMweb.** A client for QIDO-RS, WADO-RS and STOW-RS, and `qido`, `wado`
  and `stow` commands. Against Orthanc, a retrieved file is the one Python's
  requests fetches, byte for byte, and stored files come back byte for byte.
- **JPEG Baseline can be written**, and `storescu -jpeg-quality` sends an
  image that way. dcmtk and pydicom decode the result.

It also fixes images that were read or sent wrong: a compressed frame split
over several fragments, a 4:2:2 JPEG sent uncompressed and misread by the
receiver, 1-bit images, and `SaveAsPNG` showing a CT as white.

Nothing is removed or changed incompatibly. The interoperability suite runs
34 checks against pynetdicom and dcmtk in both directions.

### Added

- **`storescu -jpeg-quality` sends an image as JPEG Baseline** (#252). The
  library could send an 8-bit image that way and the command could not ask it
  to. With the option, `storescu` offers JPEG Baseline beside each class it
  holds uncompressed, and compresses an image at the given quality when the
  peer accepts it. A peer that does not, and an image Baseline cannot hold,
  get the file as it is, and the command says which. Nothing lossy is sent
  without the option. dcmtk receives the result and decodes it to the
  original within the loss of the quality.

- **A DICOMweb client, and `qido`, `wado` and `stow` commands** (#244). The
  library reached an archive over DIMSE only, and many now speak HTTP. The new
  `dicomweb` package searches a service for studies, series and instances
  (QIDO-RS), retrieves them as DICOM files or as metadata without the pixel
  data (WADO-RS), and stores files (STOW-RS), with a bearer token, Basic
  credentials or any header a deployment asks for. `Client.RetrieveEach`
  hands each instance over as it arrives, so a study need not be held whole,
  and a response is bounded in size. A store reports what the server kept and
  what it refused, instance by instance. `go-dicom qido`, `wado` and `stow`
  do the same from the command line. Search and retrieve return what Python's
  requests and pydicom do against a public Orthanc server, byte for byte, and
  files stored to a local Orthanc come back byte for byte.
  Frames, rendered images, bulk data on its own, the XML encoding and the
  server side are not implemented.

- **`go-dicom anonymize` de-identifies a file or a directory** (#232). The
  `anonymize` package had no command, so using it meant writing a program.
  `go-dicom anonymize <input> <output>` applies the Basic Profile, or the
  option `-profile` names, to a file or to every DICOM file under a directory,
  which keeps its layout. One mapping serves the whole run, so the instances
  of a study get the same new Study Instance UID, and the file meta gets the
  data set's new SOP Instance UID. `-uid-map` writes the original-to-new
  mapping, which is off by default because it links the output back to the
  patient. Files that are not DICOM and DICOMDIRs are left out of the output
  and reported.

- **Burned-in text can be removed, and an image that may hold it is not
  written unasked** (#233). De-identification stopped at the attributes, so a
  name drawn into an ultrasound or a screen capture survived it.
  `Dataset.RedactPixelRegions` fills rectangles of native pixel data with
  black, in every frame, at any bit depth and sample layout.
  `Anonymizer.CleanPixelData` does that, sets Burned In Annotation to NO and
  records the Clean Pixel Data Option of PS3.15 E.3.1, and
  `anonymize.BurnedInRisk` says whether an image is of a kind that is
  commonly drawn on. `go-dicom anonymize -redact x,y,w,h` applies it, and the
  command refuses an image at risk unless `-redact` or `-allow-burned-in` is
  given. Nothing here finds the text: the rectangles are the caller's.
  Native `YBR_FULL_422`, whose chroma is shared between pixels, is refused.

- **Compressed images can be redacted** (#240). `-redact` and
  `CleanPixelData` refused compressed pixel data, which is how most
  ultrasound and screen captures are stored. They now decompress first.
  `Dataset.DecompressInPlace` replaces encapsulated Pixel Data with its
  samples and makes the data set describe them: Explicit VR Little Endian,
  Planar Configuration 0, `YBR_FULL_422` as `YBR_FULL`, and the JPEG 2000
  `YBR_RCT` and `YBR_ICT` as `RGB`. Nothing is encoded again, so redacting
  adds no loss; the file is written uncompressed and is larger. An image
  that is not redacted keeps its syntax.

- **`go-dicom share` renders an image as a picture that is safe to hand on**
  (#241). Getting from a study to a PNG took `anonymize` and then `convert`,
  and leaving out the first gave a picture of an image nobody had checked.
  `share <input.dcm> <output.png|.jpg>` renders as `convert` does, with
  `--window`, `--frame` and `--quality`, and applies `anonymize`'s rule
  first: an image that may hold burned-in text is refused unless `-redact`
  says where it is or `-allow-burned-in` says there is none. Redaction
  decompresses a compressed image in memory. The picture holds pixels only,
  and the input is not changed.

- **`go-dicom convert` writes PNG and JPEG, and `Dataset.RenderFrame` renders
  an image for display** (#234). The library decoded every image it read and
  could not show one. `RenderFrame` and `RenderFrames` return a frame as a
  viewer shows it, at 8 bits: stored values read with their sign, Rescale
  Slope and Intercept applied, then the window, which is the caller's, the
  file's first Window Center and Width with its VOI LUT Function, or the range
  of the data when the file has none. MONOCHROME1 is inverted, YBR is
  converted to RGB, PALETTE COLOR goes through its tables, and an enhanced
  multi-frame image takes its rescale and window from its functional groups.
  `convert --format png|jpeg` writes it, with `--window center,width`,
  `--frame n|all` and `--quality`. The file holds pixels only, so none of the
  data set's attributes are in it. The existing `ApplyWindowing` reads samples
  as unsigned and applies no rescale; it is unchanged.

- **JPEG Baseline can be written** (#243). The library decoded the most widely
  accepted compressed syntax and could not produce it, so a peer that takes
  only `1.2.840.10008.1.2.4.50` for a storage class could not be sent a native
  image. `compress.EncodeJPEGBaseline` codes 8-bit frames of one component or
  three, each at full resolution.
  `Dataset.CompressJPEGBaselineInPlace(quality)` encodes an image and makes
  the data set describe it: color as `YBR_FULL`, a fragment per frame with a
  Basic Offset Table, Lossy Image Compression `01` with its ratio and method.
  The network sends an image this way only when `NetworkConfig.JPEGQuality` is
  set; with it unset the send fails and names the setting, since nothing lossy
  is done unasked. Deeper images and PALETTE COLOR are refused. dcmtk and
  pydicom decode the output, at the quality and size libjpeg gives for the
  same setting.

### Changed

- **`help <command>` is the command's own help, and names the binary it was
  run as** (#242). `help` had a page of its own for `show`, `info`, `tag-doc`
  and `codify`, written by hand beside each command's `-h`; the `tag-doc`
  page left out `--limit` and `--output`. `help` now runs the command's `-h`
  for every command, so the two cannot differ. Each command's help also uses
  the name the binary was invoked under, where `-h` used to say `go-dicom`
  whatever it was called.

### Fixed

- **A compressed color image sent uncompressed is described as it was
  decoded** (#246). Sent over a context that negotiated an uncompressed
  syntax, a compressed instance was decoded and still described as stored. A
  4:2:2 JPEG arrived with chroma for every pixel and saying `YBR_FULL_422`,
  which in native data means two bytes a pixel, so the receiver read the
  bytes for the wrong pixels: pydicom's reading was off by up to 255, and the
  send reported success. The transcoder now uses `Dataset.DecompressInPlace`,
  so Photometric Interpretation and Planar Configuration describe what is
  sent, for C-STORE and for C-GET and C-MOVE sub-operations, and from one
  compressed syntax to another as well.

- **A compressed frame stored in more than one fragment decodes** (#238).
  PS3.5 A.4 lets a frame span several fragments, and each fragment was decoded
  as a frame of its own, so such an image failed inside the codec: pydicom's
  `examples_jpeg2k.dcm`, one JPEG 2000 frame in three fragments, gave
  "tile-part 0 declares 152189 bytes, 65434 remain". `encaps.Frames` now
  groups fragments into frames, by the Basic Offset Table when it has an entry
  per frame, by count when there is a fragment per frame or a single frame,
  and by the end-of-image marker otherwise, and refuses what it cannot assign.
  `PixelArray` and everything built on it use it. `Extractor.ExtractFrame`,
  behind `Dataset.GetEncapsulatedFrame`, returned a fragment twice when a
  table was present and counted the table's offsets without the item headers
  they include; it uses the same grouping.

- **1-bit images render** (#242). `RenderFrame` and `convert --format png`
  refused Bits Allocated 1, which is how segmentations are stored. They are
  read eight samples to a byte from the low bit up, with frames following one
  another unpadded, and render as black and white.

- **A redacted region of a palette image is black** (#242).
  `RedactPixelRegions` filled a PALETTE COLOR image with index 0, which is
  whatever color the palette gives it. It now fills with the palette's
  darkest entry, and falls back to index 0 only when the tables cannot be
  read.

- **`GetImage`, `SaveAsPNG` and `SaveAsJPEG` show an image as a viewer does**
  (#253). They turned 16 bits into 8 with a window of 40/400 over the stored
  values read as unsigned, whatever the image: a CT came out white, a signed
  image wrong, and Rescale Slope and Intercept, the data set's own window and
  MONOCHROME1 were ignored. YBR color was shown as RGB. They now render
  through `RenderFrame`. `SaveAsWindowedPNG`, `SaveAsWindowedJPEG` and
  `SaveWithOptions` take their window in rescaled units, Hounsfield units for
  CT, where they applied it to stored values, and no longer refuse an image
  that is not 16-bit. `SaveWithOptions` scaled a one-row image to no rows.

## [1.7.0] - 2026-10-30

### Added

- **A private dictionary taken from vendor documentation** (#188). The one it
  replaces was generated, with VRs given in rotation and invented names, and
  #186 had to stop trusting it. This one is GDCM's, by way of pydicom, generated
  by `scripts/private-dictionary.py`: 8,858 attributes under 423 private
  creators. An implicit private attribute takes its VR from it again where the
  VR is unambiguous and the value is a whole number of values of it, and is UN
  otherwise, so a wrong entry cannot write a file pydicom refuses. Across
  pydicom's corpus every private attribute now gets the VR pydicom gives it.
  GDCM's BSD license and pydicom's MIT license are in `THIRD_PARTY_NOTICES`,
  which is published with each release.

- **TLS for every association, and on every network command** (#182). The
  README listed TLS as supported, but only `DialTLS` and `ListenTLS` had it, and
  neither reached an `SCU`, whose `Associate` always dialed plain TCP, or
  `ListenAndServe`. `SCUConfig.TLS` and `SCPConfig.TLS` now put an SCU's
  associations and an SCP's on TLS, through `Associate`, `ListenAndServe` and
  `StartServer`. `SCPConfig.OutboundTLS` covers the associations an SCP opens
  itself: C-MOVE sub-operations, storage commitment results and UPS event
  reports. It is plain TCP when unset, since a peer the SCP calls need not be
  reached the way the SCP is.

  All nine network commands take `-tls`, `-tls-cert`, `-tls-key` and
  `-tls-ca`, and the clients also take `-tls-server-name` and `-tls-insecure`;
  `qrscp -tls-outbound` puts its C-MOVE and commitment associations on TLS too.
  A `-tls-*` option given without `-tls` is an error rather than a command that
  quietly connects in the clear. `SCUConfigTLS` and `SCPConfigTLS`, which nothing
  ever read, are deprecated.

  `scripts/interop-test.sh` checks it against pynetdicom in both directions,
  with an OpenSSL-made PKI and mutual authentication: a C-STORE each way arrives
  intact, and a client without a certificate is refused.

### Changed

- **JPEG 2000 decodes up to four times faster** (#184). Code-blocks are coded
  independently (ISO 15444-1 B.7), and the decoder now decodes a
  tile-component's blocks in parallel rather than one at a time. The inverse
  wavelet reuses one scratch buffer per pass instead of allocating one for every
  row and column, and computes the symmetric extension only where it applies.
  A 512×512 lossless frame took 48.8 ms and now takes 12.0 ms, and allocates a
  third less; a 400×400 RGB frame of 16 tiles goes from 22.0 to 13.1 ms. On one
  CPU it is no slower. The samples are unchanged: all 49 decodable files in
  pydicom's corpus still match it sample for sample.

- **The network timeouts are enforced** (#206). `ARTIMTimeout` (30 s) and
  `DIMSETimeout` (60 s) were accepted and ignored. They now apply, so an SCP
  aborts an association that sends nothing for `DIMSETimeout`, and an SCU
  operation fails if no PDU of the response arrives within it, a C-MOVE whose
  sub-operations each take longer included. Raise `DIMSETimeout` for such
  peers, or set it below zero to turn it off. The entry under Fixed has the
  details.

- **A DICOMDIR refuses a path that is not a valid File ID** (#215). PS3.10
  8.5 allows at most 8 components of at most 8 characters from A–Z, 0–9 and
  `_`. A name such as `ct.dcm` was written into the index as it was, which
  no media reader has to accept. `WriteDICOMDIR` and `GenerateDICOMDIR` now
  return an error naming the file. `DICOMDIROptions.AllowNonConformantFileIDs`
  indexes such names anyway, for a local directory.

### Fixed

- **Structured reports round-trip, and the ones this library writes are
  conformant** (#216). `WriteContentTree` wrote each item from the tree's
  fields alone, so a document read and written back lost what the fields do
  not model: across pydicom's test-SR.dcm, 38 of 201 attributes, among them an
  SCOORD's GraphicData, which made dcmtk refuse the file. It now keeps what
  each item was read with, and writes from the fields only what was changed;
  all three SR documents in pydicom's corpus come back unchanged.
  `StructuredReport.ToDataset`, behind the README's `WriteSRFile`, wrote
  (0002,0000) into the data set, the number of findings into Concept Name Code
  Sequence as IS, the first finding into Study Description, and nothing else of
  the report. It now writes the modules every SR IOD requires and the report
  as a content tree, which dsrdump accepts as Basic Text, Enhanced and
  Comprehensive SR. `ReadSRFile` returned an empty report whatever the file
  held; it reads the file, and `FromDataset` reads the content tree.

- **A failed Associate closes the connection it opened** (#210). A rejected,
  aborted or timed-out request left its socket to the garbage collector, so
  200 rejected calls held 200 descriptors open with `GOGC=off`, and a client
  retrying against a peer that refused it could run out. It now holds none.

- **ARTIMTimeout and DIMSETimeout are applied** (#206). Both were documented
  and defaulted, and nothing read them, so a peer that connected and sent
  nothing held an association slot for good. A connection that arrived while
  the server was full was read inline in the accept loop, so one silent peer
  stopped the server from accepting anything. An SCU waited on a silent peer
  until its context ended, which with `context.Background` was never. ARTIM
  now bounds the wait for the A-ASSOCIATE-RQ, A-ASSOCIATE-AC and
  A-RELEASE-RP (PS3.8 9.1.5). `DIMSETimeout` bounds each PDU after that, and
  an SCP aborts an association idle for longer. An over-limit connection is
  answered off the accept loop. A negative value turns either timeout off.

- **Data sets are written in ascending tag order** (#214). The file writer
  and the network encoder wrote elements in the order they were added, so an
  attribute added to a file read from disk landed after its Pixel Data. PS3.5
  7.1 requires ascending order, and dcmtk warns on such a file. Out of order,
  a big endian write could also reach Pixel Data before Bits Allocated and
  swap 32-bit pixels as 16-bit ones, so pydicom read other values. Both now
  write in tag order, at every level of nesting, through the new
  `Dataset.ElementsInTagOrder`. `GetAll` keeps insertion order.

- **A request whose data set does not decode is refused, not answered
  Success** (#207). A C-STORE whose data set failed to decode was handed to the
  handler as an empty data set and answered 0x0000, so the sender believed an
  instance was stored that never was. A C-FIND whose identifier failed got no
  response at all, and C-GET, C-MOVE and the N-services went on with whatever
  the decoder returned. Every one of them now answers without calling the
  handler: 0xC000, cannot understand, for the C-services (PS3.4), and 0x0110,
  processing failure, for the N-services.

- **US is a valid VR** (#201). The VR table had no US entry, so
  `IsValidVR(US)` was false and `GetVRInfo(US)` nil, and `AllVRs` also left out
  OV, SV and UV. Callers fell back quietly, which is why it went unseen, but a
  check built on the table skipped every US. All 34 VRs of PS3.5 Table 6.2-1
  are there now.

- **A JPEG 2000 tile grid that does not cover the image is refused** (#164). The
  SIZ parser checked where the image starts but not where the tile grid does, and
  ISO 15444-1 A.5.1 requires the grid to start at or before the image. Every
  tile's bounds are computed unsigned, so a grid starting past the image wrapped
  the subtraction, and a 32×32 codestream asked the decoder for 144 TB. Running
  out of memory is a fatal error in Go rather than a panic: no `recover` catches
  it, and `Dataset.PixelArray` on such a file took the whole process with it.
  OpenJPEG refuses the same file for an illegal tile offset.

  Fuzzing past that found two more of the same kind. The image-size limit was
  applied by `Decode` alone, so `ParseCodestream` and `ReadTilePackets` sized
  their precincts from a 285212704×32 image and reached 6.2 GB first; the parser
  now applies it too. And the tile count was a 32-bit ceiling division, which
  counted a single tile wider than 2^31 as none. A grid of more tiles than a
  tile-part can name, 65535, is refused as well.

- **An 8-bit JPEG frame with no samples is an error** (#165). 8-bit frames are
  decoded by the standard library, which takes a frame header of zero columns,
  or of zero rows with the count left to a DNL marker it does not read, as an
  empty image and says nothing. `JPEGDecompressor` passed that on as a
  successful decode of no bytes, leaving every caller to notice the buffer was
  empty. The 12-bit path already refused the same header, and libjpeg refuses it
  too: "Empty JPEG image (DNL not supported)". `FuzzJPEGExtendedDecompress`
  found it within three seconds of running past its seeds.

- **Every way of starting a server applies `MaxAssociations`** (#170). An SCP
  accepted associations in three loops, `SCP.ListenAndServe`, `StartServer` and
  `StartServerTLS`, and only the first consulted the limit: with
  `MaxAssociations: 1`, `StartServer` accepted three associations held open at
  once. All three also retried a failed accept immediately and forever, so after
  `SCP.Close()`, which closes the listener without canceling the context,
  `ListenAndServe` never returned and logged a million lines in two seconds.

  There is one loop now. A closed listener ends it, and `ListenAndServe`
  returns nil after `Close`. Any other accept error is retried after a pause
  that doubles from 5 ms to a second, as `net/http` does. `StartServerTLS`
  emits the connection events `StartServer` always did.

- **Lossless JPEG prediction resets at every restart marker** (#174). T.81
  H.1.2.1 starts each restart interval afresh: its first sample takes the
  default prediction, and its first line is predicted from the left, as the
  image's first line is. The decoder did that at the start of the scan only, so
  in a frame with restart markers every interval after the first was predicted
  from the row before it and came out offset from the right values, with no
  error. Neither dcmtk nor pydicom's corpus has such a file, which is how it went
  unseen; `scripts/jpeglossless-restart-fixture.py` now writes two, at selection
  values 1 and 7, and keeps them only once libjpeg decodes them to the original
  samples. A restart interval that is not a whole number of rows, which T.81 H.2
  does not allow in a lossless frame, is refused by name.

- **`TLSConfig.CAFile` verifies the peer** (#180). It was documented as the CA
  that client certificates are verified against, and nothing read it. A server
  given a `CAFile` asked for no client certificate and accepted any client, so a
  deployment set up for mutual TLS had none; a client given one verified the
  server against the system roots, so a PACS certificate from a private CA
  failed. A server with a `CAFile` now requires a client certificate and
  verifies it against that CA, and a client verifies the server against it.
  Half a key pair, or a CA file with no certificate in it, is an error rather
  than ignored. OpenSSL's `s_client` is refused without a certificate and
  accepted with one from the CA.

- **An implicit private attribute is written as UN, not with an invented VR**
  (#186). `ResolveVR`, which the file writer and the network encoder use, took
  the VR of an element read without one from the private dictionary. Most of
  that dictionary was generated: `gen_private_dict_tool.go` gives a vendor's
  attributes VRs in rotation and names from a list, so `SIEMENS MR HEADER`
  (0019,1008), a CS, is listed as SL, and (0019,100A), a US, as FL. An implicit
  VR Siemens MR sent to `storescp` was stored with its text as three numbers and
  a 2-byte value labeled with a 4-byte VR, which pydicom refuses to read, and
  `ToDICOMJSON` failed on the same file. The dictionary is no longer consulted
  for a VR: a private creator is LO and any other private attribute without a
  VR is UN, its bytes unchanged, which pydicom and dcmtk both read. The README no
  longer advertises the dictionary's size, and replacing it with one taken from
  real vendor documentation is left to a later release.

- **`dcmstore` no longer loses or tears instances it acknowledged** (#189).
  Instances were written into their final path with `os.Create`, which truncates
  first, so a store killed midway left an empty or partial file where the good
  copy had been, still indexed and served. Two stores of one SOP Instance UID at
  once left a file that was neither copy: 172 of 200 trials. The saved index
  could be an older snapshot renamed in after a newer one, losing an instance
  across a restart; an instance stored again under another study or series left
  its old file, which `Rebuild` brought back after a `Remove`; and `Rebuild`
  dropped instances stored while it ran, 24 of 50, with a data race besides.

  Files are now written beside their final path, synced, and renamed into
  place, so a reader, a rebuild or a crash sees a whole copy or the previous
  one. Writers of one UID are serialized, a moved instance's old file is
  removed, the index snapshot and its rename happen under one lock, and
  `Rebuild` runs with stores held off. It also clears what an interrupted
  write leaves behind, and keeps the newer file where an older version of the
  store left two for one UID.

- **`dcmstore` matches Modalities in Study, and `?` matches a character**
  (#191). Modalities in Study was skipped with the computed keys, so a STUDY
  query for MR returned every study, some answering "CT" in the attribute that
  asked for MR. It is now matched as PS3.4 C.6.2.1 has it: a study matches when
  any value asked for, wildcards allowed, names a modality it holds. And `?`
  matched one byte, so `M?ller` missed `Müller`, whose `ü` is two bytes in
  UTF-8; it now matches one character, as C.2.2.2.4 says.

- **Thai text is decoded, and Chinese text loses its escape sequences** (#195).
  ISO_IR 166 and ISO 2022 IR 166 fell back to UTF-8 because `x/text` has no
  ISO-8859-11, so every Thai character became U+FFFD and the data set then
  declared ISO_IR 192 over them; Windows-874, the same table for every Thai
  character, now decodes and encodes them, and pydicom's reading matches. Under
  ISO 2022 IR 58 the escape `ESC $ ) A` was left for a decoder assumed to
  interpret it, and stayed in the text; it is now stripped as Korean's is, and
  `EncodeString` writes it, as PS3.5 Annex K requires.

- **`FromDICOMJSON` keeps the values it is given** (#193). Four kinds changed on
  the way in, so this library's own JSON did not survive being read back. A null
  number became 0; it is now no value, and a null among binary numbers, which
  have no empty component, drops that element with a warning rather than
  inventing a number, as a truncated element is dropped. UV and SV were read
  through float64 and lost precision above 2^53; numbers are now decoded
  exactly. An empty sequence came back as no value; it is now a sequence with no
  items. And text was kept under whatever (0008,0005) the JSON declared, though
  DICOM JSON text is UTF-8: a Latin-1 name was written back as "JÃ©rÃ´me". The
  declaration now says ISO_IR 192 wherever the text needs it. pydicom's JSON for
  every file in its corpus now reads back with no other difference.

- **Only the text VRs a character set covers are decoded with it** (#198).
  (0008,0005) applies to SH, LO, UC, ST, LT, UT and PN (PS3.5 6.1.2.3), and
  every text VR was decoded with it, so a CS of bytes E9 58 under ISO_IR 144 read
  "щX" where pydicom reads "éX". The others are now decoded in the default
  repertoire, which changes nothing for the ASCII they should hold.
  `dataelem.IsCharacterSetVR` names the seven.

- **`charset.EncodeString` writes code extensions as the standard has them**
  (#197). An empty first value of (0008,0005) was dropped, so
  `\ISO 2022 IR 149` made EUC-KR the base set and Korean was written with no
  `ESC $ ) C`; it now means the default repertoire, as PS3.5 6.1.2.5.3 says.
  Text needing a second set was cut in bytes where it had been measured in
  characters, failed, and came back as raw UTF-8 with no error; Latin-1 with Greek or Cyrillic is
  now encoded with `ESC - F` or `ESC - L`. Each component between delimiters is
  encoded from the initial designation, since a reader resets at `^`, `=` and
  the line separators, and an encoder that writes its own escapes no longer has
  another put in front: Japanese had IR 159's `ESC $ ( D` before its `ESC $ B`.
  pydicom decodes all of it to the original text. The library's own writing
  path writes UTF-8 and did not use this.

- **CI fuzzes every target, not six of nine** (#168). The fuzz job named its
  targets by hand, and the JPEG 2000 parser, its MQ decoder and the JPEG
  Extended decoder were not among them, so they ran on their seeds alone. Both
  defects above were found within seconds of running past those seeds. The job
  now finds its targets with `go test -list`, so a new one cannot be left out.
  The JPEG 2000 fuzzer is seeded with whole codestreams, where before every seed
  stopped in the main header, and RLE, which had no fuzz target, has one.

- **The pynetdicom interop tests listen on a free port** (#172). Three of them
  asked `NewSCP` for port 0, which it reads as `DefaultPort`, so they bound
  11112 and failed with "never bound a port" whenever anything else held it,
  another test run included, or talked to whatever was listening there. They now
  pick a free port, and report `ListenAndServe`'s own error if it fails.
  `SCPConfig.Port` documents that 0 means `DefaultPort` to `NewSCP` and any free
  port to `StartServer`.

- **A lossless JPEG scan that runs out before its frame does is an error**
  (#175). The bit reader counted the bytes it invented past the end of the
  entropy-coded data, but a scan ends at a marker, EOI at the latest, and zeros
  fed at a marker were not counted. A scan cut short and followed by any marker
  decoded to a full frame, its missing part decoded from zeros, which is what
  `maxLosslessPixels` says must not happen. Decoding now stops at the row where
  the data ran out, counted per restart interval. A scan with fewer than one bit
  per sample is refused before its samples are allocated, so 4396 bytes
  declaring 5792×5792 no longer allocate 319 MB to produce 67 MB of invented
  samples. A component that no scan covers is refused rather than returned as
  zeros.

- **The fuzz job no longer fails at random** (#177). Go's fuzz coordinator stops
  when `-fuzztime` expires and can keep that expiry as the run's error, because
  it checks it against the workers' context a moment before that context is
  canceled too. A target that fuzzed its whole budget without finding anything
  was then reported as `context deadline exceeded`, with no input written, and
  passed on a re-run. Exactly that outcome is now a warning; a crash, a hang, or
  an expiry that wrote an input still fails the job.

- **A de-identified UID keeps nothing of the original** (#211). The
  anonymizer built a replacement by keeping the original's first three
  components as a root and appending a random number, so a UID of three
  components or fewer, every `2.25` UID among them, survived whole inside its
  replacement: the original could be read off the de-identified data. The cut
  to 64 characters then returned a 64-character original unchanged and a
  63-character one ending in a dot. A replacement is now a fresh UUID-derived
  UID (`2.25.` and a random 128-bit number, PS3.5 B.2). The generator moved to
  `uid.Generate`, which `network.GenerateUID` now calls.

- **De-identification keeps no SR free text and no unsafe private
  attribute** (#212). Content Sequence (0040,A730) is D in PS3.15 Table
  E.1-1, but the walk went into it as into any sequence, and Text Value is not
  in the table, so an SR's free text survived every profile: across pydicom's
  corpus, `test-SR.dcm` kept 89 values, its findings among them. A sequence
  marked D is now emptied, and `CleanStructuredContentProfile`, which had no
  code at all, keeps the coded tree and empties the free values (Text Value,
  Person Name, dates, times, UIDs). `RetainSafePrivateProfile` switched
  private-tag removal off altogether, keeping a vendor field that held the
  patient's name and ID; it now keeps only the 479 attributes Table E.3.10-1
  lists, generated by `scripts/safe-private-table.py`. And Dose Calculation
  Model Name (3004,007F), the one Table E.1-1 row the package was missing, is
  removed. The values left in the corpus after de-identification fall from 745
  to 453, and none is identifying.

- **A de-identified data set says it was de-identified, and keeps no link
  back through its UIDs** (#213). Nothing wrote Patient Identity Removed
  (0012,0062) or the method, which PS3.15 E.1.1 requires, so a downstream
  system could not tell. `Anonymize` now sets it to YES and records the profile
  and option in (0012,0063) and (0012,0064), with CID 7050 codes.
  `RetainUIDsProfile` kept only the four UIDs of an old table, so a Referenced
  SOP Instance UID or Frame of Reference UID was remapped while the instance it
  points at kept its UID. It now keeps every UID the Basic Profile remaps.
  `GetDataset` leaves out the file meta, so its Media Storage SOP Instance UID
  kept the original. `AnonymizeFileMeta` and `RemapUID` give it the same new
  UID. The docs said Patient Name becomes ANONYMOUS and Patient ID a random
  ID; both are emptied, as Table E.1-1 says.

- **Written files are conformant in three more places** (#215).
  - UR was padded with NUL, so pydicom read the NUL back as part of the URI.
    PS3.5 6.2 pads it with a space, and both writers now do.
  - Implementation Class UID (0002,0012), Type 1 in PS3.10, was written only
    when the caller set one. A meta that does not name one now gets this
    library's, `uid.ImplementationClassUID`, with its version name. The
    network's `DefaultImplementationClassUID` and
    `DefaultImplementationVersionName` are now the same constants.
  - DICOMDIR records carried no Specific Character Set, so a Japanese name
    read as mojibake, and every instance was an IMAGE record. Each record now
    copies its instance's character set. SR documents, RT plans, doses,
    structure sets and treatment records, presentation states, waveforms,
    key object documents, encapsulated documents and the rest of pydicom's
    list get their own record type (PS3.3 F.5), with that type's keys,
    sequences included.
  - `scripts/roundtrip-check` keeps the source's implementation fields, since
    a DICOMDIR's record offsets count from the start of the file and a meta of
    another length moves every record.

- **What a peer sends is bounded** (#208). A P-DATA-TF longer than the
  Maximum Length this side advertised was accepted, though PS3.8 D.1 makes
  that the bound, and the fragments of a message were appended until one was
  marked last, so a peer that never marked one grew the buffer without limit.
  Such a PDU is now refused before its body is read, and the association
  aborted. So is a command set over 1 MiB, a message whose fragments switch
  presentation context or kind, and a data set over
  `NetworkConfig.MaxDataSetSize`, a new setting with no limit by default.

- **The association state machine aborts and answers as PS3.8 requires**
  (#209). P-DATA on a presentation context that was never accepted reached
  the handler, decoded as Implicit VR Little Endian; it is now aborted (PS3.8
  9.3.5). An A-ASSOCIATE-RQ, AC or RJ on an established association, or a PDU
  of an unknown type, closed the connection without saying why; it now gets
  an A-ABORT naming an unexpected or unrecognized PDU (Table 9-10, AA-8).
  When the peer released as the SCU did, `Release` failed and the peer never
  got its A-RELEASE-RP. The collision is now answered as PS3.8 9.2.7 orders
  it, and P-DATA still arriving during a release is set aside. A message
  whose fragments switch context or kind was already aborted by #208.

## [1.6.0] - 2026-09-20

### Added

- **`Dataset.PixelArrayInterpreted` reads signed samples as signed** (#159).
  `PixelArray` and `PixelArrayBySample` choose their Go type from Bits Allocated
  alone, so a signed 16-bit image came back as `[][][]uint16` and a sample of
  -2016 read as 63520. The values were the stored bits and were not wrong; the
  type did not say how to read them, and every caller of a signed image had to
  know to reinterpret.

  Changing those two would break a documented contract — code asserting
  `[][][]uint16` would begin to panic — so the signed reading is a new call
  instead. It applies `(0028,0103)` and nothing else, the way
  `pixels.Accessor.GetInterpretedValue` already does, returning `int8`, `int16`
  or `int32` for a signed data set and exactly what `PixelArray` returns for an
  unsigned one.

  Sign extension is from **Bits Stored**, not Bits Allocated. For a 13-bit
  sample in a 16-bit word the sign bit is bit 12, so a stored `0x1830` is -2000
  where a plain `int16` conversion gives 6192. Most files store the two widths
  equal and hide the difference, which is why the test that proves it is
  synthetic rather than from the corpus.

- **JPEG 2000 decodes, in pure Go** (#72). It was the last codec in the registry
  with no bundled decoder: `.90` and `.91` instances parsed, stored and
  transferred with their pixel data intact, but could not be read without a
  decoder the caller supplied. `Dataset.PixelArray` now decodes them without
  being asked — both the 5/3 reversible and 9/7 irreversible wavelets, 1 to 16
  bits, signed or unsigned, single or multi component, any number of tiles and
  quality layers, with the reversible and irreversible component transforms. No
  CGO and nothing to register; a decoder you register still takes precedence, so
  code already using `examples/jpeg2000` is unaffected.

  `TestPixelsAgainstWholePydicomCorpus` no longer skips anything: **all 49** files
  in pydicom's corpus that pydicom can decode now decode here to the same
  samples, up from 43. Reversible codestreams match sample for sample. An
  irreversible one is held to a tolerance of one, because the 9/7 filter is
  defined in real arithmetic and ISO 15444-4 grades a decoder on how close it
  comes rather than on equality.

  Features the decoder does not implement are refused by name rather than
  guessed at — subsampled components, custom precinct sizes, the code-block
  style options, packed packet headers, progression-order changes. A decoder
  that quietly mis-decodes is worse than none: the image looks plausible and the
  numbers are wrong, and in an RT Dose that is a wrong dose.

  A codestream states its own signedness and may disagree with the data set's
  Pixel Representation; real files do. The data set is the authority, so an
  unsigned sample in a data set that calls it signed is read back as two's
  complement of Bits Stored — without which `J2K_pixelrep_mismatch.dcm` reads its
  background as 6192 rather than -2000.

- **`qrscp` answers Storage Commitment** (#113). `commitscu` shipped, but no
  go-dicom server offered the Push Model, so it had no go-dicom peer. The SCP side
  already existed. `dcmstore.Handler` now implements `StorageCommitmentProvider`,
  and `dcmstore.SupportedSOPClasses()` includes the Push Model, so any archive built
  on the package gets it. An instance is committed only if the store holds it under
  the SOP Class named and its file is on disk, since the index alone could promise a
  file that has gone. Otherwise it fails with 0112H, 0119H or 0110H. pynetdicom and
  `commitscu` both get their answer from `qrscp` on the same association.

### Fixed

- **A pre-release tag is published as a pre-release** (#157). The release
  workflow triggers on `v*`, which matches `v1.6.0-rc.1` as readily as
  `v1.6.0`, and then published both the same way: no `prerelease` input was
  given, so a release candidate cut for testing would have been marked Latest
  and offered to everyone as the current version. A hyphen is what semver uses
  to mark a pre-release and it is the only signal a tag carries, so that is what
  decides it now. Its notes fall back to the base version's section, then to
  `[Unreleased]`, with a line saying what it is — a test build published with no
  notes is the one kind of release whose contents most need reading.

- **The tests run on every pull request** (#158). They were restricted to pull
  requests targeting `main` or `develop`, so a pull request based on another
  feature branch got no checks at all. Work that arrives as dependent stages —
  the JPEG 2000 decoder came as four — could then only be reviewed by pointing
  every stage at `develop` and reading overlapping diffs.

- **Three `fileset` benchmarks that had never run, run** (#155).
  `BenchmarkAddFile`, `BenchmarkListFiles` and `BenchmarkGetStatistics` built
  their filenames as `"test_" + string(rune(i)) + ".dcm"`, and at `i = 0` that
  is a NUL byte, which no filesystem accepts; each failed on its first
  iteration. `string(i)` on an integer is what `go vet`'s `stringintconv` flags,
  and wrapping it as `string(rune(i))` silences the warning while leaving the
  meaning as wrong — the tool went quiet and the code stayed broken.


- **Signed pixel values are read as signed** (#156). Three places built a
  `pixels.PixelData` and set `pd.PixelRepresentation = 0 // unsigned by default`,
  discarding (0028,0103) — which the same `info` struct was holding. There is no
  default about it: Pixel Representation is Type 1 on every image module, always
  present, and it is the only attribute that says whether a sample is signed.

  The `pixels` package had handled both all along: it has `signExtend`, and
  `GetInterpretedValue` branches on the representation. The support was there and
  the value never reached it. A CT sample of -2000 HU, which is air, came back
  from `PixelArrayWithAccessor` as 63536, and `GetPixelStatistics` reported a
  minimum of 63536 for an image whose minimum is negative — so a signed image
  could not report a minimum below zero at all.

- **32-bit big endian pixel data is no longer stored half-swapped** (#124).
  Pixel Data is OW, so reversing byte order by VR is two-byte
  words. At Bits Allocated 32 or 64 that leaves each sample's halves transposed:
  an RT Dose of 1249000 was stored as 250085395. The pixel accessor compensated,
  but only while the transfer syntax was still big endian, so reading a file
  looked right and rewriting it handed every other reader the scramble — in a
  file, over the network, and in DICOM JSON alike. Native Pixel Data is now
  swapped at the sample width Bits Allocated gives, on read and on write, and a
  sequence item uses its own Bits Allocated, so an 8-bit icon inside a 32-bit
  dose is not swapped at the parent's width.

- **A standard tag encoded as UN is read with its dictionary VR** (#117). An
  intermediary that does not know a tag re-encodes it as UN, and PS3.5 6.2.2 Note 2
  lets a receiver that does know it read the value as Implicit VR Little Endian.
  The VR was kept as UN instead, so the typed accessors refused the value and a
  UN-encoded sequence was never parsed: pydicom's `rtdose_rle.dcm` had 35 such
  elements, its Referenced RT Plan Sequence stayed an opaque blob, and the reader
  emitted 35 "VR mismatch ... got UN" warnings about a thing it could have
  resolved. It now reads like `rtdose.dcm`, the same study written normally.

  A private creator becomes LO (PS3.5 7.8.1). Every other private tag keeps UN,
  as does an ambiguous dictionary entry, and a UN element in a big endian file,
  whose value is little endian by Note 2 and must not be swapped with the rest.
  Across pydicom's corpus the only UN elements left are the four pydicom also
  leaves.

- **Text with no declared character set is decoded instead of mislabeled** (#115).
  `GetDataset` rewrote Specific Character Set (0008,0005) to `ISO_IR 192`
  unconditionally, while a value was left as it was found when there was no
  encoding to apply — and an empty declaration yielded none. A file declaring
  nothing and holding Latin-1 names therefore kept Latin-1 bytes under a UTF-8
  declaration, and the README's own round trip destroyed the names: pydicom read
  `Müller^Jürgen` from the original and `M?ller^J?rgen` after a write-back.

  An absent, empty or unknown declaration now decodes as ISO-8859-1, which is what
  pydicom does, with a warning that a guess was made. A file that had no
  declaration gains one, so the next reader is not handed UTF-8 described as
  something else. And the declaration is written only when every text value in
  scope is valid UTF-8; otherwise the file's own is kept, with a warning.

  No corpus file changes: they all declare a character set or are pure ASCII,
  which is why pydicom's seventeen charset fixtures never caught this.

- **A sequence item whose length overruns its sequence is kept** (#122). The item was dropped although every element inside it was
  complete: only its length field was wrong. pydicom's `DICOMDIR-nooffset` came
  back with 51 of its 52 records, one IMAGE record short, and nothing appeared in
  `DICOMFile.Warnings`, because data set warnings were copied to the file before
  the data set was parsed. The item is now clamped to the bytes that are there and
  its elements kept, with a warning; an item header sitting exactly at the
  sequence's end ends the sequence rather than adding an empty item.

- **`StatusRefusedOutOfResources` was 0x0112, which is No Such SOP Instance**
  (#137). PS3.7 Annex C: "Refused: Out of Resources" is a
  C-service status in the A7xxH range. An SCP handler reporting exhaustion with
  that constant told the peer the instance did not exist, and a peer that retries
  on a resource failure would not retry. `StatusNoSuchSOPInstance` is the correct
  name; the old one remains as a deprecated alias, so callers keep compiling.

- **The CLI is called `go-dicom` everywhere** (#120). The 1.5.0 rename left the old
  name in 58 lines across eight files, including `doc.go`, which is what pkg.go.dev
  shows, and the release notes, which told readers to download `dicom-linux-amd64`
  and friends while the assets are named `go-dicom-*`. CI built `-o dicom`, and the
  interop script defaulted to `./dicom`. `PrintUsage` now shows the name it was
  invoked as instead of a literal. A test greps the tracked files for the old name,
  so the next rename cannot half-land.

- **Storage Commitment's resource-limitation Failure Reason is 0213H** (#114). It
  was `0xA700`, the C-service status "Refused: Out of Resources", which is not one
  of the six Failure Reason values PS3.3 C.14.1.1 defines. So a requestor was told an
  instance failed for a reason the standard does not have. The two reasons that
  had no constant now have one:
  `StorageCommitmentFailureClassInstanceConflict` (0119H) and
  `StorageCommitmentFailureDuplicateTransactionUID` (0131H).
  `StatusStorageCommitmentResourceLimitation` in `status.go` carried the same wrong
  value and is corrected too. Code that uses the constants by name needs no change;
  anything that received `0xA700` from this library was receiving an undefined value.

- **DICOM JSON keeps LT, ST, UT and UR as one value** (#121). These hold a single
  value, and a backslash in them is a character (PS3.5 6.2). The JSON encoder split
  them like any other text. The 26,974-character private UT in
  `examples_ybr_color.dcm`, an XML document with Windows paths, came out as 17
  values with all 16 backslashes gone, and trimming the pieces removed spaces as
  well. An element holding only padding is now written with no `Value`, as pydicom
  writes it, instead of `[""]`. Across pydicom's corpus these are the only two
  differences from before, and both now match pydicom.

- **An image can be sent over a Deflated context** (#132). Deflated Explicit VR
  Little Endian compresses the data set, and its Pixel Data is native. The network
  transcoder asked whether the syntax was compressed, went looking for a pixel
  encoder for Deflated, and refused:

  ```
  cannot encode pixel data as 1.2.840.10008.1.2.1.99: this library writes RLE Lossless ...
  ```

  So a peer that chose Deflated received no image from go-dicom. dcmtk's
  `storescp +xa` does exactly that. `compress.IsEncapsulated` now answers the
  question that matters, whether Pixel Data is fragments, and the transcoder, the
  network encoder and `filewriter` share it.

- **`storescu` sends RT, SR and waveform objects** (#116). It proposed the
  library's default presentation contexts whatever it was sending, so an RT Dose,
  RT Plan, RT Structure Set, Structured Report or ECG failed with "not among the
  presentation contexts proposed", even against an SCP that supports them. Eight
  corpus objects sent in one run: 3 of 8 stored by `qrscp` and pynetdicom, and 0
  of 8 by dcmtk, which accepted Deflated for the default contexts and ran into
  #132.

  It now reads each file's header first and proposes one context per SOP class and
  syntax it holds: the file's own syntax, plus Explicit and Implicit VR Little
  Endian for an SCP that will not take it compressed. Over 128 contexts, the most
  one association carries, the files go over several associations. An unreadable
  file is reported and the rest still go. All eight now reach all three peers.

  `SCU.Store` chose among a class's accepted contexts in map order, which is
  random. It now prefers the data set's own syntax, then an uncompressed one, and
  breaks ties by the lowest ID.

- **A number set in Go is written and sent** (#119). `NewDataElement` takes any
  value, but both encoders rendered only `[]byte` and `string`. Rows set as
  `uint16(64)` went out from `EncodeDataset` as nothing, with no error, and
  `filewriter` dropped it with a warning. An image built in code reached the peer
  without Rows or Columns, and the peer accepted it.

  `dataelem.ValueBytes` renders a value by its VR, for both encoders:
  - Go integers, floats and slices of them, for the numeric VRs and for the OW, OL,
    OV, OF and OD words that pixel data uses;
  - numbers as text for IS and DS, with DS kept within its 16 characters;
  - `tag.Tag` for AT, `[]string`, and `PersonName`.

  These are also what the decoders return, so a value read can be set again. A
  value that does not fit is an error: 70000 in a US would otherwise have been
  written as 4464. `EncodeDataset` now fails on such a value instead of sending the
  data set without it.

- **Compressed pixel data sent to dcmtk no longer aborts the association** (#128).
  PS3.5 A.4 requires encapsulated Pixel Data to have undefined length and a closing
  delimiter. `EncodeDataset` wrote it with its byte count, and dcmtk refuses that:

  ```
  E: Found explicit length Pixel Data in top level dataset with transfer syntax
     JPEG Lossless, Non-hierarchical, 1st Order Prediction: Only undefined length permitted
  ```

  Every compressed data set go-dicom sent to dcmtk failed: a stored compressed file,
  pixels this library compressed to RLE or JPEG-LS, and a C-GET or C-MOVE of a
  compressed instance from `qrscp`. pynetdicom accepts both forms, and every
  compressed send in the interop suite went to pynetdicom. `filewriter` had the same
  defect and was fixed first. The interop suite now retrieves a JPEG instance from
  `qrscp` with dcmtk's `getscu`.

  Pixel Data that is not framed as items is now refused under a compressed syntax.
  Sending it would describe native pixels as fragments, which the receiver cannot
  detect.

- **`storescp` and `qrscp` store a compressed instance in the syntax it arrived in**
  (#126). Since 1.5.0 both servers accept compressed syntaxes (#93), but both wrote
  every instance as Explicit VR Little Endian. A JPEG instance became a file that
  declared native pixels and held JPEG fragments:

  ```
  pydicom: The length of the pixel data in the dataset (3884 bytes) doesn't match
           the expected length (30000 bytes)
  ```

  No reader can decode that, and nothing in the file says what the fragments are.
  The sender was told the store succeeded. #93's test sent instances with no pixel
  data and checked only the status.

  `DecodeDataset` never recorded the syntax a data set arrived in, so every writer
  took a received data set to be uncompressed. It now records it, and
  `filewriter.StorageTransferSyntax` gives the syntax to store in: the received one
  for encapsulated data, Explicit VR Little Endian otherwise. The decoder also used to
  take the rest of the data set as the encapsulated Pixel Data value. That value then
  kept the peer's closing delimiter, so the written file closed the sequence twice and
  dcmtk refused it. It also swallowed anything after Pixel Data. The decoder now reads
  the items up to the delimiter.

- **Writing an Implicit VR data set as Explicit VR no longer loses the pixel data**
  (#118). The reader gives an Implicit VR element the dictionary's VR, and for Pixel
  Data that is `OB or OW`. The writer put those eight characters into a two-byte field,
  so `OB` became the VR and `r OW` became the length:

  ```
  E: DcmElement: PixelData (7fe0,0010) larger (1464803442) than remaining bytes
  ```

  `storescp` and `qrscp` always write Explicit VR. So every image an Implicit VR sender
  stored in them was corrupt: DICOM's default transfer syntax, and what dcmtk sends.
  CT_small sent that way arrived with 25 of its 257 elements. Six files in pydicom's
  corpus lost elements when rewritten, `MR_small_implicit.dcm` its Pixel Data among
  them.

  The interop suite never noticed. Its C-STORE fixture is Explicit VR. Its round trip
  tolerated two rewrites dcmtk refuses, on the grounds that both fixtures are
  malformed, but dcmtk reads one of them, `meta_missing_tsyntax.dcm`, as supplied. Both
  rewrites were refused because of this defect. The round trip now judges each file
  against its source, and dcmtk also sends the fixture as Implicit VR.

  `Dataset.ResolveVR` now settles the VR for the file writer, the network encoder and
  DICOM JSON. Before, only the JSON path resolved it, and the network encoder sent
  `UN`. The rules are pydicom's:
  - `US or SS` follows Pixel Representation, inherited into sequence items.
  - Pixel Data is `OB` at 8 bits or fewer and `OW` otherwise.
  - LUT Data is `US` for a single entry and `OW` otherwise.
  - Anything else that offers `OW` is `OW`.

  JSON output for the corpus is byte-identical.

## [1.5.0] - 2026-08-15

### Fixed after the first tag

Thirteen fixes landed after v1.5.0 was first tagged, and the tag was moved to include
them. All thirteen were found by using the software rather than by testing it: three came
out of building a demonstration recording of the CLI, one out of auditing the CLI's
own help against the commands it advertises, and one out of rendering that help as a
cover image for the recording.

- **A group length is no longer reported as an unknown tag.** Every DICOM group may
  carry a group length element, `(gggg,0000)`, and the dictionary enumerates only the
  two with names of their own — so validating an ordinary file warned once per group:

  ```
  level=WARN msg="dataset: semantic validation" tag=(0008,0000)
     err="unknown standard tag: (0008,0000)"
  ```

  Six lines for a file with nothing wrong with it. Found by storing a file from the
  test corpus rather than one generated here: the fixtures this project generates carry
  no group lengths, which is why twelve earlier rounds of hunting log noise missed it.

- **The version is reported consistently however the binary was built.** The source
  declares `1.5.0`; the release workflow stamps the git tag, which is `v1.5.0`. So a
  released binary said `go-dicom version v1.5.0` while the same source built with
  `make` said `1.5.0`, and the Implementation Version Name sent to peers,
  `GO-DICOM-1.5.0`, agreed with neither. The test that exists to stop those drifting
  apart could not catch it: it runs against the source default and never sees the value
  the linker stamps in. A leading `v` is now stripped at the point of display, so every
  stamping form reads the same.

- **Flags are honored wherever they are written.** Go's flag package stops at the
  first positional argument, so the forms the help itself documents parsed no flags at
  all:

  ```
  $ go-dicom convert patient.dcm data.csv --format csv   # wrote JSON, exit 0
  $ go-dicom codify patient.dcm --output create.go       # wrote to stdout, exit 0
  ```

  `--format json`, `csv` and `nifti` produced byte-identical files. Both commands now
  accept flags in any position, which is what the help already promised.

- **`tag-doc <tag>` documents that tag.** The positional argument was read and
  discarded, so `tag-doc 0010,0010` printed the first fifty dictionary entries and
  exited zero. `(0010,0010)`, `0010,0010` and `00100010` are all accepted; junk is
  refused with a message saying what a tag looks like.

- **`tag-doc` with no arguments lists the dictionary, and writes no file.** It
  announced "Displaying first 50 tags", listed none, printed three hardcoded counts,
  and created `dicom_tags_text.txt` in the working directory as a side effect — one of
  which reached this repository. It now lists real entries sorted by tag, honors
  `-retired` and `-private`, counts what it actually has, and writes to a file only
  when `-output` names one.

- **`codify` produces Go that compiles.** Two defects: it emitted `package main` with
  no `main` function, so the default output of a command whose purpose is runnable Go
  would not build; and it decided whether a value was printable by ranging the string,
  which yields U+FFFD for invalid UTF-8 — so binary values were written into the source
  raw and the file failed to parse with "illegal UTF-8 encoding". Text VRs now go
  through `strconv.Quote`; numeric and binary VRs are emitted as the bytes they are,
  so the generated data set matches the file instead of carrying a placeholder.

- **A file that is not DICOM is reported as such.** `show`, `info`, `convert` and
  `codify` accepted any file at all: `show /etc/hosts` printed a header, a column
  heading, no rows, and exited zero, and `show /dev/null` did the same without even a
  warning. A file from which no element can be read is now an error.

- **The CLI is called `go-dicom`, everywhere.** It shipped under two names: `go
  install` builds it as `go-dicom`, after the last element of the module path, while
  the Makefile and the release assets called it `dicom`. The help text hardcoded
  `go-dicom`, so anyone who installed from a release was given instructions for a
  command they did not have —

  ```
  $ dicom
  USAGE:
    go-dicom <command> [options] [arguments]
  $ go-dicom
  zsh: command not found: go-dicom
  ```

  The build, the release assets and the installer now all produce `go-dicom`, and the
  help takes the name from how the binary was actually invoked, so it stays correct
  for a renamed copy too. **The release assets are renamed** accordingly:
  `go-dicom-macos-arm64` and so on.

- **`go-dicom help` lists every command.** It printed seven — the file commands —
  while the bare binary printed all sixteen grouped by category, because
  `displayMainHelp` kept a third copy of the list with its own descriptions. So the
  nine network commands, which are most of what the tool does, were absent from the
  listing a user reaches by typing `help`. Both listings now come from one place and
  print identically. Found by rendering `dicom help` as a cover image and counting
  the commands on it.

- **`help <command>` works for every command.** The top-level list advertised
  sixteen commands and `help <name>` knew seven of them: each of the nine network
  commands reported `unknown command 'storescu'`, while `storescu -h` printed a
  full page of help and the command ran correctly. A user following the CLI's own
  closing instruction — "Use 'go-dicom help <command>' for more information on a
  specific command" — was told the command did not exist. Help for a command with
  no hand-written page now comes from the command itself, so the two cannot drift
  apart again.

- **A bare TCP connect is no longer logged as an error.** Opening a connection and
  closing it without sending a PDU — which is what every health check,
  load-balancer probe and port scan does — arrived at the association read as an
  EOF and was reported at error level. A server behind a load balancer produced a
  steady stream of errors describing itself working correctly.

- **Ordinary presentation context negotiation is no longer reported as a problem.**
  Each refused context was logged at warning level, and a requestor proposing the
  default set has around twenty while any server supports a subset — so every
  association produced a handful of warnings, burying the refusals that matter.
  An individual refusal is now debug; refusing *every* context, which leaves the
  association established and useless, stays an error. Found by running the CLI for
  a demonstration, where normal traffic looked like a fault.

- **An association ending is no longer reported as an error.** A read timeout and
  an EOF were both logged at error level, and both are how associations ordinarily
  finish: a requestor that has done its work and gone leaves the server reading an
  idle connection until the network timeout fires, and a requestor that exits
  without releasing gives the server an EOF — which plenty of tools do. So every
  completed query produced an error describing healthy traffic. A read that fails
  for any other reason is still an error. Found the same way, in the same
  demonstration.

### Added

- **An installer.** `install.sh` works out the platform, downloads the matching build
  with the release's `SHA256SUMS`, and refuses to install unless the checksum matches.
  It prefers a directory already on `PATH` so nothing needs sudo, and clears the macOS
  quarantine flag that otherwise stops Gatekeeper running a downloaded binary.

  ```bash
  curl -fsSL https://raw.githubusercontent.com/amrshadid/go-dicom/main/install.sh | sh
  ```

### Documentation

- The README now opens with a recording of the CLI: reading a DICOM file, standing
  up an archive, storing two studies into it and querying them back. Nothing in it
  is staged — every command runs against a real archive that is empty when the
  recording starts.

- The README now says how to install the CLI: the script, a manual download with a
  platform table and the verification step, and building from source. It previously
  covered only `go get` of the library.

- The README's command list covered eleven of the sixteen CLI commands. `codify`,
  `echoscp`, `getscu`, `qrscp` and `tag-doc` were absent, so the only way to learn
  they existed was to run the binary with no arguments.

### The release as first tagged

This release started as a pass over the documentation, checking each claim against
the code. Ten defects came out of it, and the pattern in almost every one is the
one 1.4.0 was about: prose and tests agreeing with each other while the code did
something else. A data set that sent correctly over the network wrote to disk
empty. `qrscp` answered a query for one study with the whole archive and never
wrote a file. `make build` stamped a version three releases old. A multi-scan
JPEG-LS frame from any encoder could not be decoded.

One was a security defect: `storescp`, `getscu` and `qrscp` each wrote received
instances to a path taken from a peer-supplied SOP Instance UID without validating
it, so an unauthenticated peer could write a file anywhere the server process
could.

The features are the ones that let the library be used as an archive rather than
as a client: an on-disk instance store with a queryable index, a JPEG-LS encoder
so RLE is no longer the only syntax it can compress to, DIMSE operations that
pipeline on one association, and the tier-2 SOP classes verified against a peer
instead of only against themselves.

Where something is not done, it says so and says why — the SCP side of
asynchronous operations, JPEG 2000 encoding and decoding, sequence matching outside
the standard's own matching keys.

### Added

- **DIMSE operations pipeline on one association, bounded by the negotiated
  window.** The read side used to assume the next message belonged to the request
  just sent, which is true only when one operation runs at a time — so operations
  were serialized and the asynchronous operations window was reduced to one before
  being proposed, because anything larger would have told the peer something
  untrue.

  Messages are now routed to the operation waiting for them by Message ID, and the
  window is proposed as asked and enforced: C-ECHO, C-STORE, C-FIND and all six
  N-services may overlap up to it.

  **C-MOVE and C-GET take the association exclusively.** Both interleave traffic
  that is not their own response — a C-GET receives C-STORE sub-operation requests
  between its responses, and a C-MOVE has a cancel watcher reading the association
  while nothing else does. Two readers with different ideas of what belongs to them
  is how a retrieval loses a sub-operation.

  The demultiplexing is cooperative rather than a goroutine owning the connection:
  whichever caller needs a message does the reading and queues what belongs to
  others. A dedicated owner would have had to take the read side from four
  mechanisms that already depend on it — `Association.pending`, the cancel watcher,
  a C-GET's unsolicited requests, and the SCP, which has no message IDs of its own
  to wait on.

  **As an SCP, dispatch is still one message at a time per association**, so
  `MaxOperationsPerformed` is proposed as asked but does not bound concurrent
  dispatch — a server answers in the order received however many the requestor has
  outstanding. That is the remaining half of asynchronous operations, and
  `CONFORMANCE.md` §1.3 says so rather than leaving it to be inferred from a
  passing test.

- **A JPEG-LS encoder, so RLE is no longer the only syntax this library
  compresses to.** `compress.EncodeJPEGLS` writes lossless JPEG-LS at 2 to 16
  bits, single or multi component, one scan per component, and the transcoding
  path uses it: a C-STORE over a context that negotiated JPEG-LS Lossless now
  encodes rather than failing.

  **JPEG-LS Near-Lossless is still refused**, although the same encoder could
  produce it. It is lossy, and how much error to accept is the caller's decision
  rather than one to make silently while transcoding.

  The encoder and this package's decoder were written against each other, so a
  round trip through both proves only that they agree — which is exactly the
  failure shipped in the RLE encoder before 1.4.0, where it emitted frames with no
  segment header and its own decompressor accepted them. So the output is also
  handed to **CharLS**, through pylibjpeg, which shares no code with this library:
  it decodes the frame to identical samples. A real instance from pydicom's corpus
  compresses and round-trips to identical pixels through the network codec.

  Three defects were found and fixed while getting the round trip to work, all of
  them cases where reasoning from the standard's prose disagreed with the decoder:
  the first row has no special case (the decoder relies on a zeroed line above, and
  predicting from the left instead disagrees at the very first sample); the
  run-interruption error must be reduced modulo the range, or a 16-bit sample
  predicted from zero needs 17 bits and the escape path silently drops the top one;
  and 16-bit samples are little endian, matching what the decoder writes back and
  what DICOM native pixel data holds.

### Fixed

- **A multi-scan JPEG-LS frame did not decode.** `decodeJPEGLSScan` returned the
  bit reader's position as the number of bytes consumed, and the reader fills up to
  eight bytes ahead of what it has used — so the caller advanced past the end of
  the scan. A frame with one scan per component, which is what ILV=0 means and what
  the new encoder writes for colour, had its second scan header read as entropy
  data. A single-scan frame never noticed, because nothing after the scan but EOI
  is read, which is why this went unseen.

- **`dcmstore`: an on-disk instance store with a queryable index.** A working
  archive is now a few lines:

  ```go
  store, err := dcmstore.Open("./archive")
  scp.SetHandler(dcmstore.NewHandler(store))
  scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
  ```

  It accepts C-STORE, answers C-FIND at all four levels, and serves C-GET and
  C-MOVE from what it has received, verified over a real association.

  `QueryRetrieveHandler` takes callbacks and every adopter had to write the same
  thing behind them first: file the instances, index the query attributes, then
  implement the matching rules from PS3.4 C.2.2. Those rules are the fiddly part
  and are now implemented once — universal, single value, list of UID, wildcard,
  and DA/TM/DT ranges, with partial bounds covering the period they name so
  `2024-2024` is the whole year. Wildcards apply only to the VRs C.2.2.2.4 allows
  them in, so a `*` in a UI is a literal and not "everything".

  Wildcard matching is written directly rather than translated to a regular
  expression: the pattern comes from a peer, and a real patient name contains `.`
  and `(`. It is linear, with a test that forty stars against four thousand
  characters returns immediately.

  An attribute the index does not hold is an unsupported optional key, returned
  with a zero-length value as C.2.2.1.2 allows, rather than matched on — matching
  would return nothing, which reads to the requestor as an empty archive.

  Instances are files under a directory per study and series, with an atomically
  written `index.json`. Losing the index costs a slow start, not the archive: it
  is rebuilt by walking the tree, and a file that cannot be read is skipped with a
  warning rather than failing the rebuild. Every UID that becomes a path component
  goes through `fileutil.InstanceFilePath`.

  `Handler` implements the streaming interfaces as well as the slice-returning
  ones, so a retrieval reads one instance at a time and stops on C-CANCEL.

  Sequence matching (C.2.2.2.6) is implemented for the sequences the standard
  defines as matching keys: Scheduled Procedure Step Sequence, which every
  Modality Worklist query filters on, and Request Attributes Sequence. Every
  criterion in a query item must be satisfied by the *same* stored item — a
  station from one scheduled step and a date from another is not a match, since
  that would return a step nobody scheduled. Responses carry the item that
  matched rather than whichever came first. Indexing every nested attribute of
  every instance would put the whole data set back in memory, so any other
  sequence remains an unsupported optional key.

### Changed

- **`storescp` and `qrscp` now accept compressed pixel data.** Both offered only
  the four uncompressed transfer syntaxes — the library default, which matches
  pynetdicom — so a modality that stores JPEG-LS or JPEG 2000 natively, which is
  most modern equipment, could not store to either. Every compressed context was
  refused, the association was established with nothing usable on it, and the
  failure surfaced only on the first C-STORE.

  A storage SCP does not have to decode an instance to keep it, which is what
  `CONFORMANCE.md` §8.2 already said about archives and routers. Both servers now
  accept all 37, and `dcmstore.SupportedTransferSyntaxes()` pairs with
  `SupportedSOPClasses()` for anyone building their own.

  **The library default is unchanged.** It applies to every caller, including an
  SCU that will try to decode what it asked for, and for that case a compressed
  syntax with no decoder moves the failure from association time — where #84 now
  names the cause — to pixel access, where it does not. §3.1 records the decision
  and why the two cases differ.

- **`qrscp` now stores and queries for real.** It held instances in memory and
  never wrote them to disk at all — `-output` created a directory and nothing was
  put in it. Its C-FIND returned one *empty* identifier per stored instance, and
  its C-GET and C-MOVE ignored the query and returned **every instance in the
  store**, so a request for one study retrieved the whole archive. It is now
  backed by `dcmstore`, so all three do what they say.

**A failed negotiation now says why, and what to change.**

`Associate` succeeds even when the peer refuses every presentation context — the
association is established and useless — and the failure surfaced later as `no
accepted presentation context for SOP Class 1.2.840.10008.5.1.4.1.1.2`. True, and
unactionable: the peer either does not support that SOP class, or supports it and
refused every transfer syntax proposed for it, and those call for opposite fixes.

The A-ASSOCIATE-AC says which it is, in the result code of each refused context.
That was being discarded by `BuildAcceptedContextMap`, which kept only the
acceptances. Refusals are now retained, and the five `NO_CONTEXT` errors in `SCU`
explain themselves:

```
no accepted presentation context for SOP Class 1.2.840.10008.5.1.4.1.1.2: the peer
supports the SOP class but none of the transfer syntaxes proposed for it (proposed
1.2.840.10008.1.2.1, 1.2.840.10008.1.2, …; to offer the compressed syntaxes as well,
associate with contexts built from AllTransferSyntaxes(): …)
```

This is the commonest negotiation failure in the field, because the default
proposal is the four uncompressed syntaxes and a modality storing JPEG-LS
natively refuses all of them.

An SCP now reports each context it refuses, naming the calling AE and the
syntaxes it asked for, and reports separately when it has accepted an association
with no usable context at all — previously invisible from that side, so an
operator watching a modality fail repeatedly had nothing to go on.

New API: `Association.RefusedContexts`, `ExplainNoContextFor`,
`ExplainNoContextForAny`, `BuildContextMaps`, `PresentationContext.RefusalReason`,
and `PresentationContext.Proposed`. `BuildAcceptedContextMap` is unchanged and now
delegates to `BuildContextMaps`.

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

### Added

- **The tier-2 SOP classes are verified against a peer.** `CONFORMANCE.md` §8.5
  said of Relevant Patient Information Query, Display System and the non-patient
  object information models: "Nothing verifies them against a peer." They had UID
  constants, presentation-context helpers, and no proof — the same condition the
  four N-DIMSE defects of 1.4.0 were found in.

  All three are now driven end to end, and the two query services against
  pynetdicom in `scripts/interop-test.sh`, which reports the abstract syntax it
  received so a request naming the wrong class shows up as a mismatch rather than
  as agreement. Display System asserts the SCP is asked about the well-known
  instance the requestor named, since naming the wrong instance is exactly the
  1.4.0 defect in another service.

- **`findscu -sop-class` and `-key`.** A model outside the patient hierarchy could
  not be queried from the command line at all: `findscu` proposed only the default
  query/retrieve models and always sent a QueryRetrieveLevel with the patient
  keys. It now takes the information model to query and repeatable
  `GGGG,EEEE[=value]` keys, and omits the level and patient keys for a model that
  has no hierarchy — a non-patient object is not filed under a patient, so
  PS3.4 GG.2 gives it a single level and no level element.

- **`network.AllNonPatientSOPClassUIDs`**, and
  `NonPatientObjectPresentationContexts` now returns the query models as well as
  the storage classes.
### Changed — an SCU can be shared, and says what it does

**An `SCU` is safe to use from several goroutines.** Operations on one are
serialized, so each waits its turn rather than reading another's response. The
documentation previously told callers to use one `SCU` per goroutine, which works
and costs an association per goroutine — and association setup is the expensive
part of talking to a PACS: TCP, TLS handshake, then negotiation. A caller sending
instances across eight workers paid for eight associations where one would do, and
a peer with an association limit may refuse the rest.

Serialized is not pipelined: one operation is in flight at a time, so sharing
bounds the number of associations rather than raising throughput. #96 tracks the
demultiplexer that would change that.

Two methods are deliberately outside the serialization. `Cancel` has to reach the
peer while the operation it cancels is still running — a cancel that waited for
that operation to finish would be useless — and `Release` and `Abort` must not
wait behind a `Find` whose results have not been drained.

Verified by driving eight goroutines at one `SCU` under `-race` and requiring
every instance to arrive exactly once. Without the serialization the same test
receives **4 of 32** instances and times out at sixty seconds, the goroutines
having deadlocked reading each other's responses.

**The asynchronous operations window now says what this implementation does.** It
was sent exactly as the caller asked for and nothing enforced it: a caller
requesting a window of four had four negotiated and reported back, while the SCU
issued one operation at a time and waited. A proposed `MaxOperationsInvoked` above
1 — or 0, which PS3.7 D.3.3.3 defines as unlimited and is furthest from the truth —
is now reduced to 1 with a warning naming what was asked for and what was
negotiated. `MaxOperationsPerformed`, which bounds what the peer may send us, is
proposed as asked. The caller's struct is copied rather than rewritten, so an
`SCUConfig` reused for a second association is unchanged.

### Security

- **The pinned Go toolchain was missing two standard library security patches.**
  `go.mod` named `toolchain go1.25.12`, and govulncheck reported `GO-2026-6090`
  (crypto/tls) and `GO-2026-5972` (encoding/asn1) as reachable from this code —
  through `DialTLS`, `Transport.WritePDU`, `DecodePDU` and
  `TLSConfig.buildTLSConfig`, so on the TLS paths a HIPAA deployment relies on.

  Both are fixed in `go1.25.13`, which the toolchain directive now names.
  Anyone building with `GOTOOLCHAIN=auto` therefore gets the patched standard
  library. The `go 1.25.0` directive is unchanged, so the minimum supported
  version has not moved.

  The Module Validation job also pinned an exact patch release rather than the
  `1.25` range. govulncheck's standard library findings are relative to the
  toolchain doing the analysis, so a job resolving its own patch version reports a
  different set of vulnerabilities week to week for reasons unconnected to this
  code — which is how a scan under `go1.25.12` produced these findings while the
  workflow was asking for `1.25`.

- **`storescp`, `getscu` and `qrscp` wrote files to a path chosen by the peer.**
  All three named the file after the SOP Instance UID and joined it into the
  output directory unchecked:

  ```go
  filename := filepath.Join(c.outputDir, sopInstanceUID+".dcm")
  ```

  `filepath.Join` cleans a path but does not confine it, so the `..` segments
  survived and a UID of `../../etc/cron.d/pwn` escaped the output directory. For
  `storescp` and `qrscp` the UID comes from any peer that can reach the port —
  a DICOM SCP does not authenticate, as `CONFORMANCE.md` §7 states — so this was
  an arbitrary-location file write as the user running the server.

  New `fileutil.InstanceFilePath` validates before joining, and all three call
  it. A UID is dotted decimal per PS3.5 9.1, so anything capable of traversal is
  not a UID and is refused rather than sanitized into something resembling one;
  the 64-character limit from the same section is enforced too. The result is
  then checked to be within the output directory — redundant given the
  validation, and deliberately so.

  Tests cover nine traversal shapes, a UID at exactly the length limit, and a
  source scan over the `cli` package that fails on any `filepath.Join` taking a
  SOP Instance UID. The scan was verified to fail when the pattern is
  reintroduced.

### Fixed

- **`NonPatientObjectPresentationContexts` proposed no way to query.** It offered
  the five non-patient *storage* classes and none of the information models, so
  the documented way to reach these services could store a hanging protocol and
  never find it again. Found by writing the first test that used the helper — a
  test of the SOP class list alone would not have caught it, since every UID it
  named was present and correct.

- **`filewriter` wrote an empty value for every string-valued element.**
  `ElementsFromDataset` discarded the second return of a type assertion:

  ```go
  value, _ := elem.GetValue().([]byte)
  ```

  so a `string` value became `nil` and the element was written with length 0. A
  data set built in code — `NewDataElement(t, vr, "SMITH^JOHN")`, which is the
  obvious way to write it — produced a file whose every element was present,
  correctly typed, and empty. Data sets read by `filereader` hold `[]byte` and were
  unaffected, which is why round-tripping a file worked and this went unnoticed.

  `network.EncodeDataset` had the same job and got it right, so a data set that
  sent correctly over the network wrote to disk empty. Strings are now converted,
  and a value of a type that cannot be rendered is reported rather than silently
  written as empty. Found by `dcmstore`'s round-trip test.

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

[Unreleased]: https://github.com/amrshadid/go-dicom/compare/v1.10.0...HEAD
[1.10.0]: https://github.com/amrshadid/go-dicom/compare/v1.9.1...v1.10.0
[1.9.1]: https://github.com/amrshadid/go-dicom/compare/v1.9.0...v1.9.1
[1.9.0]: https://github.com/amrshadid/go-dicom/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/amrshadid/go-dicom/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/amrshadid/go-dicom/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/amrshadid/go-dicom/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/amrshadid/go-dicom/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/amrshadid/go-dicom/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/amrshadid/go-dicom/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/amrshadid/go-dicom/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/amrshadid/go-dicom/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/amrshadid/go-dicom/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/amrshadid/go-dicom/releases/tag/v1.0.0
