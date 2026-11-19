# DICOM Conformance Statement

For **go-dicom**, a Go library and command-line toolkit for DICOM file handling
and network communication.

This document follows the structure of DICOM PS3.2. It describes what the
library does, not what an application built with it does: go-dicom provides the
Application Entity, and the application supplies the AE title, the SOP classes
it wants negotiated, and the handlers that answer. Where behaviour depends on
configuration, that is stated rather than assumed.

Every claim here was checked against the code at the version stated below. Where
the library falls short of the standard, that appears in
[Limitations](#8-limitations) rather than being omitted.

| | |
|---|---|
| Implementation Class UID | `1.2.826.0.1.3680043.10.511` |
| Implementation Version Name | `GO-DICOM-1.10.0` |
| Documented against | `v1.10.0` |
| Last verified | 2026-08-15 |

---

## 1. Implementation model

### 1.1 Application data flow

go-dicom provides two roles, either of which an application may use:

- **SCU** (`network.SCU`) — initiates associations. Requests verification,
  storage, query/retrieve, storage commitment, and the N-DIMSE services.
- **SCP** (`network.SCP`, `network.Server`) — accepts associations and dispatches
  to handlers the application provides.

A single process may act in both roles at once, and must for two flows: C-MOVE,
where the SCP opens an association to a third party, and deferred storage
commitment, where an archive reports back to a requestor that is itself running
a server.

### 1.2 Functional definition

An SCP accepts an association, negotiates presentation contexts against the SOP
classes it supports, and dispatches each DIMSE message to a handler. A handler
that is absent causes the service to be refused rather than silently accepted —
see [§2.4](#24-services-refused-when-unimplemented).

A C-STORE is given to the handler one of two ways. `HandleCStore` receives the
data set decoded, after the whole message has arrived and been held in memory. A
handler that also implements `CStoreStreamer` receives the bytes of the data set
exactly as the peer sent them, read from the association as it reads, so memory
stays bounded by two PDUs whatever the instance's size; `WriteCStoreFile` writes
them as a Part 10 file with File Meta Information built from the command (PS3.10
7.1), the data set untouched. A data set that does not arrive whole fails the
handler's read with an error that is never `io.EOF`, and no response is sent; one
the handler stops reading early is read to its end and discarded before the
handler's status is sent. A C-STORE-RQ that states no data set follows is
refused with C000H without the handler being called, whether it streams or is
given the decoded data set (PS3.7 9.3.1.1).

`WriteCStoreFile` checks the command's Affected SOP Class UID and Affected SOP
Instance UID, and the transfer syntax, against PS3.5 9.1 before it writes
anything, and refuses a command that fails; the handler chooses the status, and
0117H (Invalid Object Instance) is the one PS3.7 9.1.1.1.9 gives for the instance
UID. It does not compare those UIDs with the data set's own (0008,0016) and
(0008,0018), which PS3.10 7.1 requires to be equal: the data set is copied, not
parsed.

### 1.3 Sequencing

The library imposes no ordering beyond what the standard requires. An
association may carry any number of operations, answered in the order received.

**Asynchronous operations are negotiated and enforced as an SCU.** A window asked
for by the application is proposed as asked and bounds how many operations may be
outstanding: each waits for its own response by Message ID, so a response cannot
reach the wrong caller. A window of zero — unlimited, per PS3.7 D.3.3.3 — is
proposed as one instead, since every outstanding operation holds a goroutine and
unlimited is not a bound this implementation can keep.

Which operations may overlap:

| Operation | |
|---|---|
| C-ECHO, C-STORE, C-FIND, and all six N-services | Overlap, up to the negotiated window |
| C-MOVE, C-GET | Take the association exclusively |

C-MOVE and C-GET are exclusive because both interleave traffic that is not their
own response: a C-GET receives C-STORE sub-operation requests between its
responses, and a C-MOVE has a cancel watcher reading the association while nothing
else does. Two readers with different ideas of what belongs to them is how a
retrieval loses a sub-operation.

**As an SCP, dispatch is still one message at a time per association.**
`MaxOperationsPerformed` is proposed as asked but does not yet bound concurrent
dispatch, because nothing is dispatched concurrently — a server answers a
requestor's operations in the order received however many it has outstanding. That
is the remaining half of asynchronous operations and it is not implemented.

An `SCU` is safe to share between goroutines whatever the window. With the default
window of one, operations serialize and sharing bounds the number of associations a
concurrent caller needs, which matters because association setup is the expensive
part and a peer may limit how many it accepts.

---

## 2. AE specifications

### 2.1 SOP classes supported as SCU

| SOP Class | UID | Notes |
|---|---|---|
| Verification | `1.2.840.10008.1.1` | C-ECHO |
| Storage (all classes in the dictionary) | various | C-STORE |
| Patient Root Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.1.{1,2,3}` | |
| Study Root Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.2.{1,2,3}` | |
| Patient/Study Only Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.3.{1,2,3}` | Retired; offered because archives still expose it |
| Modality Worklist — FIND | `1.2.840.10008.5.1.4.31` | Not proposed by default; pass `BasicWorklistPresentationContexts()`. On the command line, `findscu -worklist` sends the keys of PS3.4 Table K.6-1 with the Scheduled Procedure Step Sequence, and no Query/Retrieve Level; `wlmscp` answers them |
| Storage Commitment Push Model | `1.2.840.10008.1.20.1` | N-ACTION out, N-EVENT-REPORT in |

`SCU.Find` chooses an information model from what the peer accepted, in the order
Patient Root, Study Root, Patient/Study Only, Modality Worklist. Use
`FindWithSOPClass` to name one — the right call whenever more than one was
negotiated, since these models answer different questions and picking by
availability is a convenience rather than a substitute for saying what you meant.

### 2.2 SOP classes supported as SCP

Negotiated by default:

| SOP Class | UID |
|---|---|
| Verification | `1.2.840.10008.1.1` |
| CT Image Storage | `1.2.840.10008.5.1.4.1.1.2` |
| Enhanced CT Image Storage | `1.2.840.10008.5.1.4.1.1.2.1` |
| MR Image Storage | `1.2.840.10008.5.1.4.1.1.4` |
| Enhanced MR Image Storage | `1.2.840.10008.5.1.4.1.1.4.1` |
| Ultrasound Image Storage | `1.2.840.10008.5.1.4.1.1.6.1` |
| Secondary Capture Image Storage | `1.2.840.10008.5.1.4.1.1.7` |
| Computed Radiography Image Storage | `1.2.840.10008.5.1.4.1.1.1` |
| Digital X-Ray Image Storage | `1.2.840.10008.5.1.4.1.1.1.1` |
| Patient Root Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.1.{1,2,3}` |
| Study Root Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.2.{1,2,3}` |
| Patient/Study Only Q/R — FIND, MOVE, GET | `1.2.840.10008.5.1.4.1.2.3.{1,2,3}` |
| Storage Commitment Push Model | `1.2.840.10008.1.20.1` |

Modality Worklist (`1.2.840.10008.5.1.4.31`) is served by `WorklistHandler` but
is **not** in the default set, since an SCP that accepts worklist queries without
a worklist to answer them from is worse than one that declines. Add it with
`SetSupportedAbstractSyntaxes`. `WorklistHandler` does no matching of its
own; package `matching` matches a query against an entry and builds the
answer (section 2.7).

`go-dicom wlmscp` is a worklist SCP over a directory of worklist files, one
entry per file, as dcmtk's `wlmscpfs` is. It accepts Verification and
Modality Worklist FIND and refuses every other SOP class. The directory is
read again at every query; a file that is not DICOM, or holds no Scheduled
Procedure Step Sequence, is not served and is reported once. A query's
Query/Retrieve Level, which Basic Worklist does not have, is neither matched
nor returned; every other key is matched by section 2.7 and returned, so
the answers carry status FF00, never FF01. Over dcmtk's sample worklist
database, dcmtk's `findscu -W` and pynetdicom get the same entries,
attribute for attribute, from `wlmscp` as from `wlmscpfs` for queries by
modality, date and date range, time range, station AE title, patient name
with wildcards, universal matching and one that matches nothing. They
differ on a person's name in another case: `wlmscp` matches `vivaldi*` to
VIVALDI^ANTONIO and `wlmscpfs` does not.

Any other SOP class may be added with `SetSupportedAbstractSyntaxes`. The
dictionary holds UID constants for far more classes than this list; a constant is
not an implementation, and only the classes above have a service behind them.

### 2.3 DIMSE services

| Service | SCU | SCP |
|---|---|---|
| C-ECHO | yes | yes |
| C-STORE | yes | yes |
| C-FIND | yes | yes |
| C-MOVE | yes | yes, with sub-operations to a third party |
| C-GET | yes | yes, with sub-operations on the same association |
| C-CANCEL | yes | yes — see [§2.5](#25-cancellation) |
| N-EVENT-REPORT | yes | yes |
| N-GET, N-SET, N-ACTION, N-CREATE, N-DELETE | yes | yes |

### 2.4 Services refused when unimplemented

Storage Commitment is withdrawn during presentation context negotiation unless
the handler implements `StorageCommitmentProvider` or
`StorageCommitmentResultReceiver`. A requestor therefore learns at association
time that the service is unavailable, rather than after building a transaction
and sending it.

This matters more than it might appear. Accepting a commitment request and then
refusing it tells the requestor nothing until it has already committed to the
exchange; accepting it and answering with success would tell the requestor it may
delete its only copy of data nobody promised to keep.

### 2.5 Cancellation

A C-CANCEL is dispatched, not treated as an unknown command. An earlier version
fell through to a default branch that aborted the association, which destroyed
results the requestor had already received.

A C-FIND handler implementing `CFindStreamer` receives a context that is canceled
when a C-CANCEL naming its message arrives, so it can stop searching. A handler
returning a complete result set cannot be interrupted — it has finished before
the first result is sent — and for those the cancel stops the transfer only.

### 2.6 Storage commitment

Both timings in PS3.4 Annex J are implemented:

- **Same association.** The handler returns a result; the SCP sends
  N-EVENT-REPORT before the association closes.
- **Second association.** The handler returns a result marked `Deferred`; the
  N-ACTION is acknowledged with nothing promised, and
  `ReportStorageCommitment` delivers the outcome later on an association the
  archive opens back to the requestor. Its address comes from
  `SCPConfig.CommitmentRequestors` or `ResolveCommitmentRequestor`.

An archive that verifies durability before promising it needs the second form.
Answering immediately is a promise made before it is true.

`dcmstore`, and so `qrscp`, answers on the same association, because a C-STORE
there is complete when it is acknowledged. An instance is committed only if the
store holds it under the SOP Class named and its file is on disk. Otherwise it
fails with 0112H (not held), 0119H (held under another class) or 0110H (file
missing), the Failure Reasons of PS3.3 C.14.1.1.

### 2.7 Matching in the archive

`dcmstore`, and so `qrscp`, matches by the rules of PS3.4 C.2.2: universal,
single value, list of UID, wildcard, range, sequence, empty value (`""`) and
multiple value matching. The rules are package `matching`'s, which matches a
query against any data set, and the store runs them against its index, so
the two answer a query alike. Where the standard leaves the choice to the
SCP:

| Choice | What the archive does |
|---|---|
| Case of a person name (C.2.2.2.1) | Ignored for PN, in single value and wildcard matching: `smith*` finds `SMITH^JOHN`. Every other VR is matched as written. |
| Accents | Kept: `Jerome` does not match `Jérôme`. |
| Fuzzy matching of names | Not offered. |
| Character set of a query | The query is read in the Specific Character Set it declares, and the index is UTF-8, so a name matches however the query and the instance were each written. |
| Character set of an answer | `ISO_IR 192` when the answer holds anything outside ASCII; none when it does not (C.4.1.1.3.2). |
| A date or time in its ACR-NEMA form | `1997.04.24` and `09:30:00` are read as `19970424` and `093000`, in a stored value and in a query. The stored value is returned as it is. |
| An attribute with no value in an instance | Matches universal matching, `*` and `""` only. A range does not match a study with no date. |
| A key of more than one value (C.2.2.2.8) | Matches an instance that matches any one of its values. A key of LT, ST, UT or UR is one value: a backslash in it is text. |
| A key the index does not hold | Not matched on, and returned with no value (C.2.2.1.2). |

---

## 3. Presentation contexts and transfer syntaxes

### 3.1 Transfer syntaxes negotiated by default

| Transfer Syntax | UID |
|---|---|
| Implicit VR Little Endian | `1.2.840.10008.1.2` |
| Explicit VR Little Endian | `1.2.840.10008.1.2.1` |
| Deflated Explicit VR Little Endian | `1.2.840.10008.1.2.1.99` |
| Explicit VR Big Endian | `1.2.840.10008.1.2.2` |

These four are the same default pynetdicom uses, and all four are fully
supported: the data set codec encodes and decodes each, including the byte swap
for big endian and the deflate stream.

**Compressed syntaxes are opt-in.** `AllTransferSyntaxes()` returns 37: the
four above and 33 that carry compressed pixel data. Pass it to
`SetSupportedTransferSyntaxes` on an SCP, or use it when building presentation
contexts on an SCU. The three JPEG XL transfer syntaxes (`.110`, `.111`,
`.112`) are not among the 37, so no server here accepts them unasked. A list
is used exactly as given, so naming their UIDs in `SCPConfig.TransferSyntaxes`
or `SetSupportedTransferSyntaxes` beside the others has an SCP accept a JPEG
XL context, and the instance is then carried as it is, not decoded (§3.2). It is not the default for the
same reason it is not pynetdicom's: a context list grows with the product of SOP
classes and transfer syntaxes, and proposing every combination makes an
association request large enough to matter.

Negotiating a compressed syntax does **not** require being able to decode it.
An archive or router receives an instance, stores it, and forwards it later; the
pixel data travels as opaque bytes. [§8.1](#81-pixel-data) lists which of them
this library can actually decode.

**A server whose purpose is to store should accept all of them.** The library
default is deliberately conservative, because it applies to every caller
including an SCU that will try to decode what it asked for — and for that case,
a compressed syntax it cannot decode moves the failure from association time,
where the error names the cause, to pixel access, where it does not.

That reasoning does not apply to a storage SCP, so the servers in this
distribution accept everything:

| Server | Transfer syntaxes |
|---|---|
| `go-dicom storescp` | the 37 of `AllTransferSyntaxes()` |
| `go-dicom qrscp` | the same 37, via `dcmstore.SupportedTransferSyntaxes()` |
| `go-dicom echoscp` | the default four; verification carries no data set |
| `go-dicom wlmscp` | the default four; a worklist query and its answers carry no pixel data |

Before this, a modality that stores JPEG-LS or JPEG 2000 natively — most modern
equipment — could not store to either server: every compressed context was
refused, the association was established with nothing usable on it, and the
failure surfaced only on the first C-STORE.

For your own SCP, pass `AllTransferSyntaxes()` to
`SetSupportedTransferSyntaxes`, or `dcmstore.SupportedTransferSyntaxes()` if you
are using that package.

### 3.2 Transfer syntaxes supported for files

| Transfer Syntax | Data set | Pixel data |
|---|---|---|
| Implicit VR Little Endian | read/write | yes |
| Explicit VR Little Endian | read/write | yes |
| Explicit VR Big Endian | read/write | yes |
| Deflated Explicit VR Little Endian | read/write | yes |
| RLE Lossless | read/write | **yes** |
| JPEG Baseline | read/write | yes, via the standard library |
| JPEG Extended, Lossless, JPEG-LS, JPEG 2000 | read/write | **yes**, all in pure Go |
| High-Throughput JPEG 2000 (`.201`, `.202`, `.203`) | read/write | **yes**, in pure Go; not encoded |
| JPEG XL (`.110`, `.111`, `.112`) | read/write, carried | **no**: refused by name |

Values in a big endian file are normalized to little endian while parsing and
converted back on write, so byte order does not reach code above the reader.

High-Throughput JPEG 2000 pixel data decodes like any other compressed
syntax: rendering, the samples, `decompress`, `convert`, redaction, DICOMweb
frames (as stored, typed `image/jphc`), and sending to a peer that accepts
only uncompressed syntaxes, which decodes it on the way. It is not encoded:
nothing is converted or sent into HTJ2K, and a DICOMweb request for a frame
as `image/jphc` that is not held so is refused with 406. pydicom-data's
`.201` file decodes to pydicom 3's samples (pydicom decodes HTJ2K through
OpenJPEG), and its `.203` file within the tolerance of §8.1; no third-party
file in `.202` exists, so the tests carry a generated one, an RPCL codestream
wrapped by pydicom 3, and decode it to pydicom's samples too.

A file in JPEG XL is read, stored, de-identified and sent as it is, to a peer
that accepts its syntax. Its pixel data is not decoded: rendering, reading the
samples, redacting, and sending it over a context that needs it decoded are
refused with the syntax's name. Nothing in pydicom, dcmtk 3.7.0 or this library
decodes JPEG XL, and no JPEG XL file in DICOM was found to check one against.

### 3.3 Extended negotiation

Supported in both directions: asynchronous operations window, SCP/SCU role
selection, and user identity negotiation. The window defaults to one.

---

## 4. Communication profiles

- **Physical media:** not applicable; this is a network and file library.
- **Transport:** TCP/IP. DICOM Upper Layer Protocol per PS3.8.
- **TLS:** `SCUConfig.TLS` puts an SCU's associations on TLS, `SCPConfig.TLS`
  an SCP's, and `-tls` does the same for every network command. A
  `TLSConfig.CAFile` makes each side authenticate the other: a server requires
  a client certificate issued by that CA, and a client verifies the server
  against it. The associations an SCP opens itself (C-MOVE sub-operations,
  storage commitment results, UPS event reports) use `SCPConfig.OutboundTLS`,
  plain TCP when it is unset, since a peer the SCP calls need not be reached
  the way the SCP is; its certificate must then allow client authentication.
  Verified against pynetdicom over TLS in both directions. Plain DICOM over TCP is neither authenticated nor encrypted, and
  checking a called AE title is a naming convention rather than authentication.

### 4.1 DICOMweb

The `dicomweb` package and the `qido`, `wado` and `stow` commands are a user
agent for the HTTP services of PS3.18. `dicomweb.Server`, and `qrscp -web`
over its store, are an origin server for the same three services; see
below.

| Service | Supported | Not supported |
|---------|-----------|---------------|
| QIDO-RS | Search for studies, series and instances, with matching, `includefield`, `limit`, `offset` and `fuzzymatching`; results as DICOM JSON | XML results |
| WADO-RS | Study, series and instance as `application/dicom` in `multipart/related`, in a named transfer syntax or as stored; metadata as DICOM JSON | Frames, rendered images, thumbnails and bulk data retrieved on their own |
| STOW-RS | `application/dicom` parts, to the service or into a study; the response's stored and failed instances, with their reasons | Metadata with separate bulk data |

As an origin server (`dicomweb.NewServer`, `qrscp -web`):

| Service | Supported | Not supported |
|---------|-----------|---------------|
| QIDO-RS | `/studies`, `/series`, `/instances` and the same within a study or a series. Matching is the archive's own (section 2.7), so a search matches what a C-FIND does: single value, wildcard, range, list of UID, and `Sequence.Attribute` for an attribute inside a sequence. `includefield`, `limit`, `offset`. Results as DICOM JSON in UTF-8, each with the attributes of PS3.18 10.6.3.3 the archive holds a value for; 204 when nothing matches | XML results. Fuzzy matching: `fuzzymatching=true` is answered by literal matching and a `Warning: 299`. `includefield=all` returns the default attributes |
| WADO-RS | Study, series and instance as `multipart/related; type="application/dicom"`. `transfer-syntax=*`, or a request that accepts anything: each instance as the archive holds it, byte for byte. No transfer syntax named: Explicit VR Little Endian, an instance held compressed being decoded for it. Any other syntax: the instances already in it, 206 when some are not and 406 when none is. One instance as `application/dicom`: the file itself. `/metadata` as DICOM JSON. Frames, `…/frames/{list}`: one part a frame, in the order asked; by default, or for `application/octet-stream`, uncompressed in Explicit VR Little Endian, a compressed frame decoded for it; for `application/octet-stream; transfer-syntax=*`, or the frame's own type (`image/jpeg`, `image/jls`, `image/jp2`, `image/jpx`, `image/dicom-rle`, `image/x-dicom-rle` accepted for it), as stored. Each frame read from the file by its position, through the Extended or Basic Offset Table, a frame over several fragments joined. Frames of Float and Double Float Pixel Data, their floats little endian. A frame of a 1-bit image that starts inside a byte (frames are packed with no padding between them, PS3.5 8.1.1) sent as a 1-bit frame on its own is laid out: from the lowest bit of its first byte, zero bits after its last. A frame number not in the image: 404; a list that is not frame numbers counted from 1: 400; a compressed type or syntax the frame is not held in: the frame encoded into it (PS3.18 8.7.3.3.2), into RLE Lossless, JPEG-LS Lossless or Near-Lossless, JPEG Lossless (Process 14 or SV1), JPEG Baseline or Extended, or JPEG 2000 Lossless or lossy; a type named alone is its lossless default of Table 8.7.3-5 (`image/jpeg` JPEG Lossless SV1, `image/jls` JPEG-LS Lossless, `image/jp2` JPEG 2000 Lossless, `image/dicom-rle` RLE), and a lossy syntax is used only when named, at what `dicomweb.WithFrameEncoding` sets (JPEG quality 90, JPEG-LS NEAR 2, JPEG 2000 at 10:1 by default); 406 with the reason for JPEG 2000 Part 2 (`image/jpx`), an image the coder does not take, and a frame held lossy, which is not coded again. Bulk data, `…/instances/{instance}/bulkdata/{tag}[/{item}/{tag}…]`: each value metadata leaves out (binary values over 1 KiB, Pixel Data at any length), read from the file by its position, as `application/octet-stream` in little endian; Pixel Data as its frames are sent, every frame in one part when uncompressed. `/metadata` gives a `BulkDataURI` for each, inside sequences too. The bulk data of a study, a series or an instance, `…/bulkdata` (PS3.18 Table 10.4.1.5-1): every value metadata names, a part each with its `BulkDataURI` as `Content-Location`, instance by instance in the archive's order; Pixel Data uncompressed in one part, or, for `transfer-syntax=*`, compressed frames as stored, a part a frame at its frame resource; only `multipart/related; type="application/octet-stream"` (406 otherwise), 204 when there is no bulk value. Rendered, `…/instances/{instance}/rendered` and `…/frames/{list}/rendered`, and thumbnails of an instance, frames, a series and a study (the first instance with an image, its first frame): `image/jpeg` (the default) or `image/png`, one picture bare, several in `multipart/related`; `window=center,width,function`, `viewport=vw,vh[,sx,sy,sw,sh]` and `quality` applied; a thumbnail fitted to 128 pixels when no viewport is given. `iccprofile=yes`: the instance's own ICC profile embedded, the pixels unchanged (PS3.3 C.11.15), with `Warning: 299` when it has none or the picture cannot carry it; no `iccprofile`, or `no`: none. Refused: a malformed parameter, a flipped viewport region, `iccprofile=srgb`, `adobergb`, `rommrgb` or `displayp3`, which ask for the pixels converted: 400; another media type: 406; a study or a series rendered whole, an instance with no Pixel Data: 501. `annotation`: the picture, not annotated, and `Warning: 299`. With no `window` and none in the image, a frame is drawn over the range its Bits Stored can hold through its rescale, the identity VOI LUT of PS3.3 C.11.2.1.2.2, the same for every frame (Orthanc 1.13 stretches each frame to its own values); an image whose values fill little of that range, such as 32-bit RT Dose, comes out dark, and `window` draws it otherwise | Frames sent outside a multipart body. A Float or Double Float Pixel Data image rendered (501). |
| STOW-RS | `application/dicom` parts in `multipart/related`, to `/studies` or into a study. Each instance kept or refused on its own: 200 when all were kept, 202 when some were, 409 when none was, with Referenced SOP Sequence and Failed SOP Sequence in the answer. Failure reasons: A900H for an instance sent into a study it is not of, 0110H when the archive could not keep it. A compressed instance is kept in the syntax it came in. A request is bounded (4 GiB unless set), 413 beyond | Metadata with separate bulk data (415). A part that is not a DICOM file has no UID to be listed under, and shows in the status alone |

`qrscp -web` serves under `/dicom-web`. A port alone listens on this machine
only. An address other machines can reach is refused without `-web-token` (or
`DICOMWEB_TOKEN`), which every request must then carry as a bearer token.
Under `-tls` the service is HTTPS with the same certificate, and asks for a
client certificate when `-tls-ca` is given. Checked in
`scripts/interop-test.sh`: files stored by pynetdicom over DIMSE are searched
for over HTTP by a client that is not this library, and the answers held
against pydicom's reading of the files, a C-FIND on the same archive, and
Orthanc holding the same files. Frames are checked the same way: every file
of pydicom's corpus with pixel data is stored, and each of its frames is the
bytes pydicom divides out of the file and the bytes Orthanc sends, except
32-bit big endian, which Orthanc 1.13 reverses in two-byte words. Float and Double Float images, little and big endian, and a packed 1-bit image, written by pydicom, have each frame equal to pydicom's `pixel_array`; Orthanc 1.13 refuses all three (400). A frame encoded on request is decoded by pylibjpeg (libjpeg, OpenJPEG) or pydicom: seven corpus images in every syntax, lossless ones to the stored pixels, JPEG-LS near-lossless within 2, JPEG and lossy JPEG 2000 at 38 dB or better.

UPS-RS and the capabilities service are not implemented. Bulk data that
metadata refers to by URI is not fetched: the element is kept, empty.

Authentication is whatever header the caller supplies: a bearer token, Basic
credentials, or another. TLS is the HTTP client's, with the system roots
unless a CA is given. A response is read up to a bound, 4 GiB by default,
and one that runs past it is an error.

Search and retrieve are checked against a public Orthanc server, read-only,
and return what Python's requests and pydicom do for the same requests, the
retrieved file byte for byte. Store, search and retrieve are checked against
Orthanc in continuous integration, by `scripts/interop-test.sh`: files of
several transfer syntaxes are stored, found, and come back byte for byte, the
search agrees with Python's reading of the same server, and a refusal is
reported instance by instance. Orthanc answers 400 for a request it kept part
of, and does not name a file it could not read; both are counted as refused.

---

## 5. Configuration

| Setting | Where |
|---|---|
| AE title | `SCPConfig.AETitle`, `SCUConfig.CallingAE` / `CalledAE` |
| Port and bind address | `SCPConfig.Port`, `SCPConfig.BindAddress` |
| Maximum PDU size | `SCPConfig.Network.MaxPDUSize` |
| Concurrent association limit | `SCPConfig.MaxAssociations`; a caller over it is refused rejected-transient, service-provider (presentation), local-limit-exceeded |
| After a rejection | An A-ASSOCIATE-RJ is followed by a wait for the caller to close the connection, up to `NetworkConfig.ARTIMTimeout` (PS3.8 9.2, AE-8 and AA-2); nothing that arrives meanwhile is answered |
| After an abort | An A-ABORT this side sends is followed by up to one second in which the peer may close the connection, what arrives being read and dropped (PS3.8 9.2, AA-1, AA-6, AA-8); the standard's timer for this is ARTIM, and one second is kept so that a peer that does not close holds only the connection |
| Who may associate | `SCPConfig.Admit`: called before anything is negotiated, with the calling and called AE titles, the remote address and the verified TLS chains; refuses with the result, source and reason it chooses (PS3.8 Table 9-21). A request whose calling or called AE title is not well formed (PS3.5 6.2) is refused before it is asked, and the called AE title must still be `AETitle` unless `SCPConfig.AcceptAnyCalledAE` is set. Without `Admit`, any caller asking for `AETitle` is admitted. On the command line, `storescp`, `qrscp` and `wlmscp` take `-allow` (calling AE titles, IP addresses, CIDR ranges, and `TITLE@ADDRESS` to admit a title only from an address) and `-allow-file` (the same entries, one to a line, `#` for comments) |
| AE title padding | Leading and trailing spaces of the calling and called AE titles are not significant (PS3.8 Table 9-11) and are removed from both ends before a title is compared |
| Stopping a server | `Server.Shutdown(ctx)`: no new connection is accepted, an A-ASSOCIATE-RQ on a connection already open is refused rejected-transient, service-provider (presentation), temporary-congestion (PS3.8 Table 9-21), an association waiting for its next request is aborted at once (A-ABORT source 0, service-user), and one in an operation finishes it, has its next PDU waited for (an A-RELEASE-RQ is answered, anything else aborted unstarted), all until `ctx` ends; what is left then is aborted and counted in the `*ShutdownError` returned. `Server.Stop` cancels at once |
| A failed TLS handshake | Reported to `SCPConfig.Events` as `EVTTLSHandshakeFailed`, with the client's address and a `*TLSHandshakeError` naming the check that failed; nothing of the client's certificate is carried |
| A fault in a handler | A panic in a DIMSE handler ends that association with an A-ABORT (source 2, service-provider; reason 0) and is logged; no status is sent for the message in progress. Other associations are not affected |
| Why this side aborted | The events of an A-ABORT this side sends carry `Event.LocalAbort`: the cause (`AbortCauseTimeout`, `AbortCauseProtocolViolation`, `AbortCauseHandlerPanic`, `AbortCauseStopping`, `AbortCauseRequested`) and the error behind it. The PDU is unchanged |
| Timeouts | `NetworkConfig`: `ARTIMTimeout` (30 s) for association setup and release, `DIMSETimeout` (60 s) for each PDU after it; an SCP aborts an association idle for longer |
| Supported SOP classes | `SCPConfig.AbstractSyntaxes`, read before the listener opens (nil keeps the defaults, which include query and retrieve; a non-nil list is used as given, so an empty one accepts nothing; `StorageAndVerificationSyntaxes()` is the storage-only list), or `SetSupportedAbstractSyntaxes` for associations from then on |
| Supported transfer syntaxes | `SCPConfig.TransferSyntaxes`, the same way, or `SetSupportedTransferSyntaxes` |
| C-MOVE destinations | `MoveDestinations`, `ResolveMoveDestination` |
| Commitment requestors | `CommitmentRequestors`, `ResolveCommitmentRequestor` |
| Logging destination and format | `config.SetLogger`, for the whole library; `SCPConfig.Logger` and `SCUConfig.Logger` (a `*slog.Logger`, nil meaning the global one) for one server or client, which then logs nothing to the global one, the validation warnings of the data sets it decodes included |
| Network log verbosity | `network.SetDefaultLogLevel`, `network.DebugLogger` |

Everything the library reports goes through `config.Logger`, so one call
redirects or silences all of it, but for a server, a client or a read
(`filereader.ReadOptions.Logger`) given a logger of its own. Network messages carry a `component=network`
attribute for filtering, and default to warnings and errors only.

---

## 6. Support of character sets

Specific Character Set (0008,0005) is applied when a file is read. Text values
reach the caller as UTF-8 through the ordinary accessors — `Get`, `GetValue`,
`GetAll` — with no separate decoding step, and the data set's own Specific
Character Set reads `ISO_IR 192` to match the values it now describes.

An item may declare its own character set, and it applies to that item and
everything below it, as PS3.5 allows.

A file that declares nothing, declares an empty value, or names a term this
build does not know is decoded as ISO-8859-1, with a warning on
`DICOMFile.Warnings` saying a guess was made. PS3.5 6.1.2.3 makes the default
repertoire ASCII, so non-ASCII text under no declaration is non-conformant, and
common from older equipment; pydicom reads it the same way. The data set then
declares `ISO_IR 192`, and gains that declaration if the file had none, so
writing it back does not hand the next reader UTF-8 bytes described as
something else.

The declaration is written only when every text value in scope really is UTF-8.
If a value did not decode, the file's own Specific Character Set is kept and a
warning says why: a data set that is wrong is better than one that says it is
right.

Over 30 encodings are supported: the single-byte ISO_IR sets, and ISO 2022
escape sequences for Japanese, Chinese and Korean. Measured against pydicom's
seventeen character set fixtures — Arabic, Greek, Hebrew, Japanese, Korean,
Russian, Chinese and accented Latin — every patient name matches what pydicom
reads, and every file survives a round trip through this writer with dcmtk able
to read the result.

`DICOMFile.DataElements` still holds the bytes exactly as stored, for a caller
who needs them.

A writer that encodes text in a non-default set declares it; one that reverts to
the default removes the declaration. A file whose text is encoded in a set it
does not declare cannot be read correctly by anything, since a reader has no
other way to know.

---

## 7. Security and enforced limits

These bound what a peer or a crafted file can cost. They are enforced, not
advisory.

| Limit | Value | Guards against |
|---|---|---|
| `network.MaxPDULengthLimit` | 128 MiB | A PDU declaring a huge length before sending any payload |
| `network.MaxAssociatePDULength` | 1 MiB | An A-ASSOCIATE-RQ or -AC declaring a huge length, before the caller is known; the four-byte PDUs are held to four bytes |
| The Maximum Length advertised in the A-ASSOCIATE-RQ or -AC | `NetworkConfig.MaxPDUSize` | A P-DATA-TF longer than the receiver agreed to; aborted (PS3.8 D.1) |
| Command set being assembled | 1 MiB | Command fragments never marked last; aborted |
| `NetworkConfig.MaxDataSetSize` | no limit unless set | A data set larger than the receiver will hold in memory, or will take from a stream; aborted |
| `NetworkConfig.ARTIMTimeout` / `DIMSETimeout` | 30 s / 60 s | A peer that connects and sends nothing, or stops partway through a PDU |
| `network.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated data set received over an association |
| `filereader.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated file |
| `compress.MaxDecompressedSize` | 256 MiB | Decompression bombs in stored pixel data |
| `compress.MaxInflateRatio` / `MinInflateAllowance` | 1000:1, 8 MiB floor | A *small* input claiming a large expansion |
| `filereader.MaxSequenceDepth` | 64 | Unbounded recursion from deeply nested sequences |
| `valuerep.MaxUIDLength` | 64 characters | A UID longer than PS3.5 §9.1 permits |
| Element length verification | every element | An element declaring more bytes than the stream holds; from a stream of unknown size, each value is allocated as its bytes arrive |
| `filereader.ReadOptions.MaxValueLength` | no limit unless set | A value longer than the reader will hold, refused by tag and length before it is allocated |

The parsers that consume untrusted input — PDU decoding, data set decoding, DIMSE
command decoding, and file reading — have fuzz targets that run in continuous
integration.

Vulnerability reporting is described in [SECURITY.md](./SECURITY.md).

---

## 8. Limitations

Stated plainly, because a conformance statement that omits them is worth less
than none.

### 8.1 Pixel data

RLE, JPEG Baseline, JPEG Lossless (`.57` and `.70`, every prediction selection
value) and JPEG-LS (`.80` and `.81`, lossless and near-lossless, both interleave
modes, with or without restart intervals) all decode in pure Go, single or multi component,
at 1, 8, 16, 32 and 64 bits, in either planar configuration, and with
horizontally subsampled `YBR_FULL_422` expanded. A frame coded at fewer bits
than Bits Allocated, such as a JPEG-LS frame of 8 bits in an image of 16 bits
allocated, decodes at the width Bits Allocated says, as JPEG 2000 always did; a
frame coded at more bits than Bits Allocated is refused rather than cut.

Every file in pydicom's corpus that pydicom can decode decodes here to the same
samples: **all 50 of them**, compared whole rather than by their leading values,
with nothing skipped for want of a decoder. What follows are two differences in
how decoded samples are shaped and coloured, and the one tolerance.

- **JPEG 2000 decodes in pure Go**, `.90` and `.91`, both wavelets, 1 to 16 bits,
  signed or unsigned, single or multi component, any number of tiles and quality
  layers, in all five progression orders and with progression order changes
  in the main or a tile-part header, with the default precincts or the
  coding style's own, with any combination of the six code-block style options
  (selective arithmetic coding bypass, reset of context probabilities,
  termination on each coding pass, vertically causal context, predictable
  termination, segmentation symbols), with a region of interest coded by the
  Maxshift method in the main header or a tile-part header, and with the
  reversible and irreversible component transforms, bare or in a JP2
  container whose boxes may give their lengths in 32 or 64 bits. There is no
  CGO and nothing to register.

  pydicom's corpus uses only the defaults, so what it does not exercise is
  checked against OpenJPEG instead: `opj_compress` writes a codestream with the
  feature, `opj_decompress` decodes it, and this decoder must return the same
  samples, at 8, 12 and 16 bits, signed and unsigned, gray and color, tiled and
  not, reversible and irreversible (`TestDecodeMatchesOpenJPEG`, run in CI).
  All 63 combinations of the code-block style options are checked this way
  (`TestDecodeEveryBlockStyleMatchesOpenJPEG`).

  One combination is checked against the samples that were encoded instead: a
  region of interest with selective arithmetic coding bypass. OpenJPEG 2.5.4
  decodes its own lossless output of that combination to thousands of wrong
  samples, because its decoder starts bypass four bit planes into the block
  without counting the planes the region's shift added (D.6). This decoder
  returns every sample (`TestRegionOfInterestWithBypassIsLossless`). Its
  boundary between region and background follows H.1, 2^s, where OpenJPEG's
  decoder uses 2^(s-1). OpenJPEG's encoder never writes background that the
  difference would show, so `TestRegionOfInterestThreshold` pins H.1's rule.
  Likewise, OpenJPEG's encoder writes progression order changes whose volumes
  overlap so that a packet carried twice would read the same as one carried
  once, and B.12.2's rule that it is carried once is checked on the walk
  itself (`TestProgressionVolumesCarryEachPacketOnce`).

  A reversible codestream is required to match pydicom sample for sample. An
  irreversible one is held to a tolerance of one up to 12 bits and two at 16:
  the 9/7 filter is defined in real arithmetic, so two conformant decoders can
  land either side of a half, and ISO 15444-4 grades a decoder on how close it
  comes rather than on equality. Against OpenJPEG the corpus's irreversible file is 22 samples of
  262144 out by one. One is the difference from OpenJPEG up to 12 bits. At 16
  bits OpenJPEG lands up to two away, and this decoder is the nearer of the two
  to the samples that were encoded: on a generated 70×65 16-bit image written
  by `opj_compress -I`, a mean squared error of 370 against OpenJPEG's 1281.

  **High-Throughput code-blocks (Part 15, ITU-T T.814) decode too**: the HT
  cleanup pass with its MEL, VLC and MagSgn streams, the HT SigProp pass
  (with the vertically causal style) and the HT MagRef pass, the CAP marker
  that declares Part 15, and HT tile-components beside Part 1 ones. Everything
  around the code-blocks is the Part 1 decoder above, so HT codestreams have
  every tiling, precinct, progression, layer and filter it has. The CxtVLC
  tables of Annex C were read out of the ITU-T text by a script and are checked
  to be complete prefix codes with consistent patterns
  (`TestCxtVLCTablesAreCompletePrefixCodes`). The third-party HT codestreams
  that exist, pydicom-data's two HTJ2K files and openjpeg-data's four HT
  codestreams, all decode to OpenJPEG 2.5.4's samples, exactly where
  reversible. None of them has a refinement pass, a second layer, tiles, more
  than 8 bits or the vertically causal style, and nothing available writes HT
  (OpenJPEG decodes it and does not encode it), so the tests carry an HT
  encoder of their own written from T.814 Annex F: every codestream of an
  `opj_compress` matrix like the one above is transcoded to HT code-blocks,
  cleanup alone or with refinement passes, and OpenJPEG's decoder is the
  reference (`TestHTDecodeMatchesOpenJPEG`, 390 codestreams, run in CI).
  Refinement passes in a later layer than their cleanup pass are the one case
  OpenJPEG 2.5.4 cannot decode: its packet reader adds them to the cleanup
  segment, where T.814 B.2 makes them a segment of their own. That case is
  checked to decode exactly as the same blocks in one layer
  (`TestHTRefinementInLaterLayers`). The three HTJ2K transfer syntaxes,
  `.201`, `.202` and `.203`, decode through this decoder.

  Features outside that set are refused rather than guessed at. A decoder that
  quietly mis-decodes is worse than none — the image looks plausible and the
  numbers are wrong, and in an RT Dose that is a wrong dose. Each of these is
  refused with an `ErrUnsupported` that names it, quoted here as the error
  reads (ITU-T T.800 clauses in brackets, T.814's marked):

  - the reserved bit of the code-block style, alone or with HT's bit 6 where
    it mixes HT and Part 1 code-blocks within a tile-component, and the bits
    T.814 leaves reserved beside bit 6: `code-block style 0x80`, `0xC0`,
    `0x41` and so on [A.6.1 Table A.19; T.814 A.4];
  - an HT code-block of more than three coding passes, which placeholder
    passes or a second HT set make: `an HT code-block of N coding passes`
    [T.814 B.1, B.2];
  - a region of interest on HT code-blocks: `a region of interest in HT
    code-blocks` [T.814 A.5]; OpenJPEG refuses all three as well;
  - the capabilities of any other part in a CAP segment: `the capabilities of
    Part N` [A.5.2];
  - a region of interest coded by a method other than Maxshift, which Part 2
    defines: `region of interest style N` [A.6.3];
  - packed packet headers: `packed packet headers (PPM/PPT)` [A.7.4, A.7.5];
  - a subsampled component in a DICOM frame: `component N subsampled DXxDY
    in a DICOM frame` [PS3.5 8.2.4]. `jpeg2000.Decode` decodes components
    with any separation (XRsiz, YRsiz, T.800 A.5.1, B.2), each to a plane of
    its own size (`Image.ComponentSize`), checked component by component
    against `opj_decompress` on 54 codestreams `opj_compress` wrote with
    4:2:0, 4:2:2, 3x2, 1x4 and 4x4 separations (`TestSubsampledMatchesOpenJPEG`)
    and on imagecodecs' 4:2:2 `ycbc.j2k`. A data set cannot describe such a
    frame: PS3.5 8.2.4 allows JPEG 2000 with MONOCHROME1 and 2, PALETTE
    COLOR, YBR_RCT, YBR_ICT, RGB and YBR_FULL, and "no other Standard
    Photometric Interpretation Values shall be used", every one of them with
    each sample at every pixel; so the frame is refused rather than
    resampled into a picture the file does not describe;
  - components deeper than 16 bits: `17-bit components` and so on [A.5.1];
  - the Part 2 wavelets and quantization styles: `wavelet transform N`,
    `quantization style N`.

  Any marker from Part 2 is refused too, but as `unexpected marker` with its
  code rather than by name.

  What a codestream may cost is bounded. The image is at most 2^16 samples on
  a side and 2^28 in all, counted over every component. A tile must hold at
  least a byte for each of its precincts, since each has a packet of at least
  a byte in every layer (T.800 B.9, B.10.3), and reading its packet headers is
  work in proportion to their bits, not to the number of code-blocks they
  name. Nothing sized by the image is allocated until every tile's packets
  have been read. A codestream within those bounds decodes at the cost of the
  image it describes, which can be large for a few bytes and honestly so:
  `opj_compress` writes a black image of 16384 by 16384 in 938 bytes.

  A codestream's own signedness may disagree with the data set's Pixel
  Representation, and real files do: the data set is the authority, so an
  unsigned sample in a data set that calls it signed is read back as two's
  complement of Bits Stored.

- **`PixelArray` flattens color samples into the column dimension**, so a
  100×100 RGB frame is returned as 100 rows of 300 values. The values and their
  order are correct. `PixelArrayBySample` returns the four-dimensional shape that
  `PixelDataShape` reports.

- **`PixelArray`'s Go type follows Bits Allocated, not Pixel Representation.** A
  signed image arrives in an unsigned type, so a sample of −2016 reads as 63520.
  The values are the stored bits and are not wrong; the type does not say how to
  read them. `PixelArrayInterpreted` applies `(0028,0103)` and returns a signed
  array for a signed data set, sign-extending from Bits Stored — which matters
  when Bits Stored is narrower than Bits Allocated, where a plain conversion of
  the whole word gives the wrong number.

- **Samples are returned in the color space the Photometric Interpretation
  names.** A `YBR_FULL` instance yields YBR, not RGB, because the attribute
  describes the samples as stored and converting them while it still says YBR
  would leave a reader to convert a second time. This matches pydicom. Convert
  with the attribute, not despite it.

  For a three-component JPEG the attribute is also what says whether the
  components were transformed at all, since the JPEG Interchange Format does
  not carry a color space (PS3.5 8.2.1). RGB means they were not, YBR_FULL and
  YBR_FULL_422 that they were, and a JFIF or Adobe marker or component
  identifiers spelling R, G, B do not overrule it. This is dcmtk's default
  (`dcmdjpeg +cp`). pydicom 3 lets a marker overrule the attribute where the
  two disagree: an RGB frame with a JFIF marker or an Adobe marker saying
  YCbCr is converted there and not here, and a YBR frame with an Adobe marker
  saying RGB or identifiers R, G, B is left as stored there and converted
  here. No file in pydicom's corpus is in one of those cells. Without a data
  set, `compress.NewJPEGDecompressor` guesses from the stream as libjpeg does.

- **Subsampled JPEG chroma is filtered as libjpeg filters it.** T.81 leaves
  the filter to the decoder. A 4:2:2 or 4:2:0 frame is brought to full
  resolution with libjpeg's default "fancy upsampling", the triangle filter
  of its `jdsample.c` (3:1 between neighbors, edges repeated, a component one
  or two samples wide repeated), so a color image decodes to what dcmtk and
  pydicom show: within one level of both in YCbCr on every subsampled color
  JPEG in pydicom's corpus and on generated frames of odd sizes, the one
  level being the inverse DCT, as on frames that are not subsampled. Other
  layouts (4:4:0, 4:1:1) are repeated, as libjpeg 6b does. 12-bit frames are
  repeated too; DICOM has no 12-bit color (PS3.5 Table 8.2.1-1).

- **A JPEG decoded to RGB is converted with libjpeg's arithmetic.** JFIF gives
  the YCbCr equations and leaves the rounding to the decoder; this one rounds
  as libjpeg's `jdcolor.c` does (each coefficient scaled by 2^16, each product
  rounded, Y added after the shift), which is what dcmtk and Pillow use. On
  frames whose inverse DCT agrees the result is theirs exactly; elsewhere a
  one-level difference in the YCbCr planes can become two in RGB, since Cb
  is multiplied by 1.772. On pydicom's corpus every color JPEG decoded to RGB
  is within one level of dcmtk and Pillow except `examples_ybr_color.dcm`, at
  two for that reason.

- **`RenderFrame` renders for display, within what it knows.** It applies the
  sign of the samples, the Modality LUT Sequence or Rescale Slope and
  Intercept, the VOI LUT Sequence or a window (LINEAR, LINEAR_EXACT or
  SIGMOID) or the range of the data, the Presentation LUT Sequence or
  Presentation LUT Shape, MONOCHROME1 inversion, the YBR to RGB conversion
  and the Palette Color tables, at 8 bits (PS3.3 C.11). Where a data set has
  both a VOI LUT Sequence and a window, which C.11.2 leaves to the viewer, the
  table is used, as pydicom does; a caller's window wins over both.
  Presentation LUT Shape decides the inversion when it is present, so
  MONOCHROME1 with INVERSE is inverted once, as dcmtk does. A Presentation LUT
  Sequence takes the VOI output range as its input range (C.11.4.1); pydicom
  scales by the image's lowest and highest values instead, which agrees when
  the image reaches both ends. It does not apply an ICC Profile (C.11.15):
  color is not color managed, since converting through a profile that holds
  lookup tables takes a color management engine. `go-dicom convert` writes
  the image's own profile into the PNG (iCCP) or JPEG (APP2), the samples
  unchanged, so that a viewer that manages color shows them as the image
  means: ICC Profile (0028,2000), or that of the one optical path with one
  (`Dataset.ICCProfile`). A grayscale picture takes only a gray profile and a
  color one only an RGB profile, as PNG requires; otherwise the picture is
  written without it and a warning says so. pydicom converts only when
  `apply_icc_profile` is called, through LittleCMS; dcmtk 3.7.0 and Orthanc
  1.13 neither apply nor embed one. It refuses 64-bit samples, segmented palettes
  and a Presentation LUT Shape other than IDENTITY or INVERSE by name.
  `go-dicom convert --format png|jpeg` is the same rendering, read from the
  file a frame at a time, and a native frame a band of rows at a time
  (`RenderFramesFrom`); a grayscale JPEG holds its whole picture. Grayscale
  output is checked against pydicom, the lookup tables on generated files
  since no corpus file has one, and color against the lossless copy of the
  same picture.

### 8.2 Network

- **Compressed transfer syntaxes are not negotiated by default**, though all
  four uncompressed ones are. Pass `AllTransferSyntaxes()` to accept compressed
  pixel data; see [§3.1](#31-transfer-syntaxes-negotiated-by-default).
- **A handler that returns a complete result set cannot observe a
  cancellation.** C-FIND, C-GET and C-MOVE each accept an optional streaming
  interface — `CFindStreamer`, `CGetStreamer`, `CMoveStreamer` — whose context is
  canceled when the requestor sends C-CANCEL, so matching stops rather than
  running to completion with its results discarded. A handler that returns a
  slice is served unchanged, but cannot be interrupted while it builds one.

  The sub-operations of a C-GET or C-MOVE are abandoned on cancel either way,
  slice or stream. `IsCanceled` tells a cancel apart from a failure, since a
  canceled retrieval is reported as status 0xFE00 with the number of
  sub-operations still outstanding.
- **Nine syntaxes are compressed *to*: RLE Lossless, JPEG-LS Lossless and
  Near-Lossless, JPEG 2000 Lossless, JPEG Lossless (`.57` and `.70`), and JPEG
  Baseline, JPEG Extended and lossy JPEG 2000 when asked.** Pixel data is
  transcoded in both directions as the negotiated context requires.

  Sending over a context that negotiated an uncompressed syntax decodes the pixel
  data first. Sending over one that negotiated **RLE Lossless** or **JPEG-LS
  Lossless** encodes it — from native pixels, or by decoding a compressed source
  and re-encoding — with `Dataset.CompressRLELosslessInPlace` and
  `Dataset.CompressJPEGLSLosslessInPlace`, which write a file the same way
  (`go-dicom compress`).
  Both code the samples pixel by pixel and say so: a color image becomes
  Planar Configuration 0, and a native YBR_FULL_422 image, coded with its
  chroma expanded, becomes YBR_FULL. RLE takes any whole number of bytes a
  sample (up to the fifteen segments of its header); JPEG-LS takes 8 or 16
  bits allocated, coded at Bits Stored, or at Bits Allocated when a sample
  uses the bits above it, and a 16-bit image at no less than 9 bits so that
  a decoder returns two bytes a sample.

  *Verified* over every image in pydicom's corpus that this library reads
  and decodes: dcmtk (`dcmdrle`, and `dcmdjpls`, which is CharLS) and pydicom
  return the samples that were coded, for all 56 written as RLE and all 48
  written as JPEG-LS. pydicom 2.4's own JPEG-LS path rearranges a color image
  wrongly, for dcmtk's files and the corpus's own as much as for these, so
  JPEG-LS color is checked with the libjpeg decoder it uses, called directly.

  The JPEG-LS encoder codes 2 to 16 bits, one scan per component, losslessly
  or with an allowed error. **JPEG-LS Near-Lossless (`.81`) uses the error the
  sender gives in `NetworkConfig.JPEGLSNear`**, NEAR of ITU-T T.87, 0 to 255:
  every sample arrives within that much of its value. How much error to accept
  is the caller's decision, so the default, zero, codes the image losslessly
  under `.81`, which the syntax allows, and nothing is lost unasked. Each error
  is quantized to a multiple of 2·NEAR+1 and every later sample is predicted
  from the reconstructed ones (T.87 A.4.4). With an error allowed, the data set
  is marked Lossy Image Compression `01` with its ratio and method
  `ISO_14495_1` (PS3.3 C.7.6.1.1.5), and the SOP Instance UID is the caller's
  to change. A signed image is coded only at NEAR 0: JPEG-LS codes samples as
  unsigned numbers, and an error on the two's complement bits would carry the
  top of the signed range to the bottom. dcmtk refuses the same images.
  `Dataset.CompressJPEGLSNearLosslessInPlace(near)` is the same encoder for
  files.

  *Verified:* dcmtk's `dcmdjpls` (CharLS) and libjpeg (through
  pylibjpeg-libjpeg) decode every unsigned corpus image of 8 or 16 bits
  allocated, 32 files coded here at NEAR 1, 2 and 5, and seven generated edge
  cases (1x1, odd sizes, planar configuration 1, a 6000 by 5000 image), every
  sample within NEAR, and the two return the same samples as each other and as
  this library's decoder. At NEAR 0 all 48 corpus images and ten edge cases,
  the signed ones included, come back exact. The size matches dcmtk's own
  encoder at the same NEAR to within a few hundred bytes. CI runs the dcmtk
  check at NEAR 2.

  **Below 8 bits the thresholds are T.87's, and dcmtk's are not at 6 and 7
  bits.** A frame with no LSE segment is coded with the default thresholds of
  T.87 C.2.4.1.1, which below MAXVAL 128 come from a formula of their own:
  T1, T2, T3 are 2, 3, 5 at 6 bits and 2, 3, 10 at 7 (3, 7, 21 at 8 bits). Its
  CLAMP sends a threshold above MAXVAL to the one below it, not to MAXVAL,
  which decides the thresholds of near-lossless coding with a large NEAR
  (128, 128, 128 at 8 bits and NEAR 127). Both rules are followed when coding
  and decoding, and an LSE segment that leaves a threshold at zero has that
  one defaulted on its own. CharLS before 2.1 used the 8-bit formula at every
  precision (CharLS issue 51). Losslessly that gives other thresholds at 6
  and 7 bits only (2, 3, 4); near-lossless it differs at most NEAR values
  from 2 to 7 bits: at NEAR 1 at 2 bits, 1 and 2 at 3 bits, 1 to 5 at 4 bits,
  1 to 10 at 5 bits, 22 of the 32 values at 6 bits and 43 of the 64 at 7.
  From 8 bits up the two agree. dcmtk 3.7.0 still bundles that CharLS.
  **Where the two differ, this library's encoder writes T.87's thresholds in
  an LSE segment** (T.87 C.2.4.1.1: MAXVAL, T1, T2, T3 and RESET, as CharLS
  writes one when given them), so dcmtk reads the frame with the right ones;
  everywhere else it writes none, and the frame is CharLS 2.2's. Without the
  segment dcmtk refused most of those frames ("Too much compressed data" or
  "Invalid compressed image data") and decoded the odd one to a slightly
  different image without an error. dcmtk's own frames at 6 and 7 bits carry
  no thresholds and are coded with the old ones, so this library refuses
  them, with an error that names the cause and both sets of thresholds.
  Decoding them with the old thresholds is not offered, because a stream does
  not say which it was coded with. A 7-bit dcmtk frame used to decode here
  without an error to an image up to 127 levels off: a scan whose samples are
  all decoded with more than a byte of its data unread is now refused, as
  CharLS and dcmtk refuse it. `dcmcjpls +t1 2 +t2 3 +t3 5` (or `+t3 10` at 7
  bits) writes the thresholds into an LSE segment, and that frame decodes the
  same everywhere.

  *Verified:* a 41 by 37 image at every precision from 2 to 8, coded at NEAR 0,
  1, 2 and the largest NEAR allowed: this library's frames, lossless and
  near-lossless, are byte for byte CharLS 2.2's, and every frame either of
  them codes decodes to the same samples in this library, CharLS 2.2 and
  libjpeg (pylibjpeg-libjpeg). The same holds from 9 to 16 bits, and for the
  corpus's JPEG-LS images coded again with their own NEAR, except that above
  12 bits CharLS writes an LSE segment that repeats the default parameters.
  A run that reaches the end of a line is a single 1 bit (T.87 A.7.1.2); the
  0 and a length that 1.10.0 wrote there still decode.
  dcmtk 3.7.0's `dcmdjpls` decodes this library's frames at 6 and 7 bits, at
  every NEAR (96 frames, and 224 three-component ones from 6 to 8 bits), to
  the same samples as CharLS 2.2 and libjpeg; before the LSE segment it
  refused 52 of the 65 whose thresholds differ and decoded one differently.
  dcmcjpls and dcmdjpls refuse below 6 bits. With the thresholds given
  explicitly all four decode dcmtk's 6 and 7 bit frames exactly.

  **JPEG-LS restart intervals are decoded.** A DRI segment gives Ri in 16, 24
  or 32 bits (T.87 C.2.5), counted in lines: one line of the component in a
  non-interleaved scan, of every component in a line-interleaved one. At each
  RSTm marker the decoder starts again as at the top of the scan, with the
  contexts and run index initialized, the line above taken as zeros and the
  bits read from the byte after the marker; fill bytes before a marker are
  skipped. A marker missing, out of order or at another line than Ri puts it,
  or an interval with data left over, is refused, where libjpeg decodes on.
  The encoder writes no restart intervals.

  *Verified:* CharLS's restart test files (`test8_ilv_none_rm_7.jls`,
  `test8_ilv_line_rm_7.jls`, `test8_ilv_sample_rm_7.jls`, `test16_rm_5.jls`),
  which were refused as corrupt, decode to the T.87 test images exactly, as
  libjpeg decodes them; CharLS 2.2 refuses them. 1237 more frames, at every
  precision from 2 to 16, in all three interleave modes, at NEAR 0, 2 and the
  largest, with Ri of 1, 2, 3, 7 and around the height, decode here and in
  libjpeg to the same samples. CharLS 2.2 cannot write restart intervals, so
  these were made from its coding of each interval as an image of its own,
  joined with the markers; made that way from the T.87 test images, CharLS's
  restart test files come out byte for byte.

  **JPEG Baseline (Process 1) is written only when the sender sets
  `NetworkConfig.JPEGQuality`**, for the same reason: it is lossy. It codes
  8-bit unsigned MONOCHROME1, MONOCHROME2, RGB and YBR_FULL images; a deeper
  image and PALETTE COLOR are refused. Color is coded with every component at
  full resolution and described as `YBR_FULL`, since the 4:2:0 that a common
  encoder defaults to has no Photometric Interpretation in this syntax. Each
  frame is one fragment, with a Basic Offset Table. The data set is marked
  Lossy Image Compression `01` with its ratio and method `ISO_10918_1`. The SOP
  Instance UID is not changed: a lossy copy is a different instance (PS3.3
  C.7.6.1.1.5), and saying whether a send is one is the caller's.
  `Dataset.CompressJPEGBaselineInPlace` is the same encoder for files. dcmtk
  and pydicom decode what it writes, to the quality and size libjpeg gives at
  the same setting.

  **JPEG 2000 Lossless (`.90`)** is written by `jpeg2000.Encode`, in pure Go:
  the reversible 5/3 wavelet with no quantization, every bit plane coded, one
  tile, one quality layer, code-blocks of 64 by 64, at 1 to 16 bits, signed or
  unsigned. An RGB image is coded with the reversible component transform and
  the data set then says `YBR_RCT`, with Planar Configuration 0 (PS3.5 8.2.4);
  three samples that are not red, green and blue are coded component by
  component, and a native YBR_FULL_422 image, coded with its chroma expanded,
  becomes YBR_FULL, as it does in every syntax written here. A frame is coded at Bits Stored, or at Bits Allocated when a
  sample uses the bits above Bits Stored, so that what was in the file comes
  back. `Dataset.CompressJPEG2000LosslessInPlace` is the same encoder for
  files, and it is what a send uses when a peer accepts only this syntax.

  *Verified:* OpenJPEG, which shares no code with this library, decodes what
  the encoder writes. `opj_decompress` returns every sample of 360 generated
  images of each depth, sign and shape, and of every uncompressed image in
  pydicom's corpus after it is written as `.90`; dcmtk reads each file and
  receives one over the network with its codestream intact. Both run in CI.

  **Lossy JPEG 2000 (`.91`) is written only when the sender sets
  `NetworkConfig.JPEG2000Rate`**: a compression ratio or a PSNR, one of
  them. `jpeg2000.EncodeLossy` codes the irreversible 9/7 wavelet (F.4.8.2),
  the irreversible component transform for RGB (G.3; the data set then says
  `YBR_ICT`, with Planar Configuration 0, PS3.5 8.2.4), and scalar deadzone
  quantization with a step for each subband (E.1, expounded), at 1 to 16
  bits, signed or unsigned, so a CT keeps its negative values. The steps are
  one base step over each subband's synthesis norm, measured from this
  library's own inverse filter. A ratio is met by the rate-distortion
  optimal truncation of Annex J.14: every code-block is coded once, finely,
  each coding pass is a truncation point with its bytes and the error it
  removes, and the blocks are cut where their convex hulls reach one slope,
  searched until the codestream fits, within half a percent of the ratio.
  Near lossless, where the decoder's rounding to whole samples decides the
  error more than the coding does, a search of the step with every pass
  coded is tried as well and the codestream that decodes nearer the image
  is kept. `LossyOptions.Layers` writes several quality layers, each half
  the size of the next, and `LossyOptions.Reversible` the reversible 5/3
  filter cut short (`.91` allows it, PS3.5 A.4.4); the data set path writes
  one irreversible layer. A PSNR is met by searching the step until the
  decoded image meets it. One tile. A ratio no codestream of the image can
  meet, as for a very small image, is refused rather than met with one that
  holds none of the picture, and so is PALETTE COLOR. The data set is marked
  Lossy Image Compression `01` with its ratio and method `ISO_15444_1`, and
  the SOP Instance UID is the caller's to change.
  `Dataset.CompressJPEG2000InPlace` is the same encoder for files. A ratio
  search on a 6000 by 5000 image takes about 20 seconds, a PSNR one about 4.

  *Verified:* OpenJPEG's `opj_decompress` and pylibjpeg-openjpeg decode
  every corpus image coded here (39 at 10:1, 44 at 45 dB; three corpus
  files already `.91` are left alone, and five too small for 10:1 are
  refused) and the generated edge cases (odd sizes, 16-bit signed, the full
  16-bit range, planar configuration 1, 6000 by 5000, and at 45 dB 1x1 as
  well), the two agreeing with each other on every file. At 10:1 the corpus
  comes back at 40 dB or better, some of its simple images exactly; at a
  PSNR of 45 every file decodes at 45.0 dB or better. Against
  OpenJPEG's own encoder (`opj_compress -I -r`) at the same size the PSNR is
  the same or a few tenths of a decibel better: `CT_small.dcm` 3268 bytes at
  77.1 dB against 3140 at 76.4, `examples_rgb_color.dcm` 22964 at 40.0
  against 22887 at 39.7. OpenJPEG counts a 12-bit sample as 1.5 bytes where
  this library counts the 2 it is stored in, so `-r 7.5` is its 10:1 here.
  CI runs the `opj_decompress` check at 10:1.

  **JPEG Lossless (`.57` and `.70`)** is ITU-T T.81 Process 14, in pure Go:
  one scan with every component interleaved at full resolution, Huffman
  tables built for each frame (T.81 K.2), since the example tables of Annex K
  stop at a difference of 11 bits, at 2 to 16 bits, signed or unsigned, one
  to four components. `.70` predicts each sample from the one to its left
  (Selection Value 1); `.57` codes each frame with the predictor of the
  seven that makes it smallest. There is no point transform and no restart
  interval. Color is coded as it is, so an RGB image stays RGB, with Planar
  Configuration 0. A frame is coded at Bits Stored, or at Bits Allocated
  when a sample uses the bits above it, and at no fewer than 9 bits in an
  image of 16 bits allocated; a signed sample is coded as its two's
  complement bits (PS3.5 8.2.1). Lossy Image Compression is not touched.
  `Dataset.CompressJPEGLosslessInPlace` and `CompressJPEGLosslessSV1InPlace`
  are the same encoder for files, and what a send uses when a peer accepts
  only one of these.

  *Verified:* dcmtk's `dcmdjpeg` and libjpeg (through pylibjpeg-libjpeg),
  neither of which shares code with this library, decode every image of
  pydicom's corpus of 8 or 16 bits allocated, 48 files, written in each
  syntax, and ten generated edge cases (1x1, odd sizes, 16-bit signed, the
  full 16-bit range, 12-bit signed, planar configuration 1, a 6000 by 5000
  image), to every sample. CI runs the dcmtk half. pydicom's own pixel
  access is not the check for color: it asks pylibjpeg-libjpeg for a YCbCr to
  RGB conversion whenever the data set says RGB, which is wrong for lossless
  JPEG, and it misreads dcmtk's own lossless RGB output in the same way.

  **JPEG Extended (`.51`) is written only when the sender sets
  `NetworkConfig.JPEGQuality`**, the same quality JPEG Baseline takes: both
  scale the example tables of T.81 Annex K as libjpeg does, so the number
  means the same in either. It is T.81 Process 4 for images of 16 bits
  allocated and up to 12 stored, MONOCHROME1 or MONOCHROME2, coded at 12
  bits, and Process 2 for 8-bit images, gray or color, as for JPEG Baseline:
  SOF1, sequential DCT, one scan, every component at full resolution, 16-bit
  quantization table entries where a quality needs them, and Huffman tables
  built for each frame, since Annex K's stop below the 15-bit DC differences
  and 14-bit AC sizes of a 12-bit image. An image of fewer bits stored than
  the codestream's precision is marked with the codestream's, since a
  decoded sample may land just above the old top. The data set is marked
  Lossy Image Compression `01` with its ratio and method `ISO_10918_1`, and
  the SOP Instance UID is the caller's to change. Refused by name: a signed
  image (two's complement through a DCT puts -1 beside the top of the
  range), more than 12 bits stored, 12-bit color and PALETTE COLOR.
  `Dataset.CompressJPEGExtendedInPlace(quality)` is the same encoder for
  files.

  *Verified:* dcmtk's `dcmdjpeg` (its 12-bit build of IJG libjpeg) and the
  separate libjpeg of pylibjpeg-libjpeg decode every corpus image that fits,
  32 files, and five generated edge cases (1x1 color, odd sizes at 8 and 12
  bits, planar configuration 1, a 6000 by 5000 12-bit image). At quality 90
  the corpus's grayscale images come back between 49 and 91 dB, the color
  ones above 38, and the generated ones, which carry noise, above 36.
  Against dcmtk's own encoder at the same quality (`dcmcjpeg +ee +q 90`) the
  size is within 0.4% and the PSNR the same to a tenth of a dB. CI runs the
  dcmtk check.

  Every other compressed target fails, naming the syntax asked for, and a send
  that cannot be satisfied fails rather than putting bytes on the wire
  described as something they are not, which the receiver could not detect.

  *Verified:* the encoder's output is decoded by libjpeg through pylibjpeg, which
  shares no code with this library. A round trip through this package's own
  decoder would only show the two agree — which is the failure mode the RLE
  encoder shipped with in 1.3.0.

  Decoding has one gap of its own: every compressed syntax in the table of
  §3.2 but JPEG XL decodes here, so an instance stored under any of the
  others can be transcoded onto whichever context a peer accepts.

### 8.3 Writing

Files this library writes are read by dcmtk and pydicom. Structured reports
from the `sr` package pass dsrdump, which checks the relationship constraints
of Basic Text, Enhanced and Comprehensive SR, and a content tree read and
written back keeps every attribute it was read with (#216). Encapsulated pixel data
is written with undefined length and a closing sequence delimiter, as PS3.5 A.4
requires, both to a file and on the network. Native pixel data is never sent under
a compressed syntax: the send fails rather than describe pixels as fragments.
In an Implicit VR file, a sequence whose tag the dictionary does not call a
sequence, a private one above all, is written with undefined length and a
Sequence Delimitation Item (PS3.5 7.5.1): nothing else tells a reader it holds
items (Note 5 below). Every other sequence and every item has an explicit length.
A data set sent on an association follows the same rule (#424).

On reading, an element encoded as UN whose tag the dictionary knows takes that
VR, and its value is read as Implicit VR Little Endian, which PS3.5 6.2.2 Note 2
allows whatever the transfer syntax; pydicom does the same by default. A private
creator is LO (PS3.5 7.8.1). Anything else private keeps UN, an ambiguous
dictionary entry keeps UN, and a UN element in a big endian file keeps UN,
because Note 2's little endian value must not be byte-swapped with the rest of
the data set. An undefined length means items whatever the dictionary says
(Note 5), and they are parsed as a sequence. A data set received on an
association is read the same way: only pixel data with undefined length is taken
for fragments, and a UN element's items are read as Implicit VR Little Endian
(#424).

On writing, a private attribute read without a VR, as every attribute of an
implicit VR file is, takes the VR GDCM's private dictionary gives it, looked up
through the file's own private creator. Where the dictionary does not know the
attribute, lists more than one VR, or lists a fixed-size VR its value is not a
whole number of, it is written as UN with its value unchanged, which PS3.5
6.2.2 provides for. Across pydicom's corpus every private attribute gets the VR
pydicom gives it. The dictionary this replaced was generated, with VRs in
rotation, and an implicit Siemens MR written through it had text relabeled as
numbers (#186, #188).

Where the dictionary gives an attribute two VRs, the one written is the one the
data set calls for, resolved as pydicom resolves it. `US or SS` follows Pixel
Representation (PS3.3 C.7.6.3), which a sequence item takes from the image
enclosing it. Pixel Data is OB at 8 bits allocated or fewer and OW above that
(PS3.5 A.1, A.2). LUT Data is US for a single entry and OW otherwise, and any
other choice offering OW is OW. An element with no known VR is written as UN.
The same rules apply to files, to data sets sent over the network, and to DICOM
JSON.

A storage SCP built on this library (`storescp`, `qrscp`, `dcmstore`) writes an
uncompressed instance as Explicit VR Little Endian, whatever it arrived as, and a
compressed one in the syntax it arrived in, with its fragments untouched. The syntax
is the only record of which codec made the fragments.

Every file in pydicom's corpus, read and written back, is read by dcmtk. That includes `SC_rgb_jpeg.dcm`, which holds implicit VR inside a file
declaring explicit. dcmtk refuses it as supplied and reads it once rewritten.

### 8.4 Truncated files

An element declaring more bytes than the file holds is **dropped**, with the
elements before it kept and a warning naming the offset. pydicom instead returns
the element with whatever bytes were present.

This is a deliberate difference. A partially read pixel buffer handed back as
PixelData can be rendered as an image with nothing looking wrong, and for a value
the caller cannot tell is short, dropping it is safer than shortening it. Two
files in pydicom's own corpus — `MR_truncated.dcm` and `rtplan_truncated.dcm` —
show the difference: pydicom reports 72 and 32 elements, go-dicom 71 and 31, and
the missing one is incomplete in both cases.

Anyone migrating from pydicom and relying on partial values should know this
before it surprises them; the warning carries the offset needed to recover the
bytes directly.

### 8.5 Service classes

The dictionary defines UID constants for far more SOP classes than have a
service behind them, in three tiers.

**Dedicated handlers.** Verification, Storage, Query/Retrieve, Storage Commitment,
Modality Worklist and Unified Procedure Step each have a handler interface that
models the service: `StorageHandler`, `FindHandler`, `WorklistHandler`,
`UPSHandler` and so on.

Unified Procedure Step covers the **Push** and **Watch** SOP classes.

Push is N-CREATE, N-SET, N-GET, and N-ACTION for changing state and requesting
cancellation. `UPSHandler` enforces the transition table in PS3.4 CC.1.1 and the
Transaction UID that stops two performers claiming one step, over a `UPSStore`
the caller supplies.

Watch is subscription and event reporting. Give `UPSHandler` a
`UPSSubscriptionStore` — `NewUPSMemorySubscriptions()` is one — and N-ACTION
types 3, 4 and 5 are honored. All three targets in PS3.4 CC.2.3 are supported:
a step's own SOP Instance UID, the well-known Global Subscription instance, and
the Filtered Global instance with matching keys. A global subscription is
consulted when an event happens rather than expanded when it is made, so it
covers steps created after it.

Events are delivered by `Server.ReportUPSEvent`, over an association the SCP
opens back to the subscriber — the same shape as a deferred storage commitment
result, and using the same address map. Set `UPSHandler.Notifier` and a state
change or cancellation request reports itself to every subscriber. A subscriber
that cannot be reached does **not** fail the N-ACTION: the transition is already
stored, and refusing it would leave the SCP and the performer disagreeing about
the step. `OnEventReportError` is how that failure is surfaced.

Without a `UPSSubscriptionStore` the subscription actions are still **answered
as unsupported rather than accepted**, since a subscription the SCP does not
keep leaves the requestor waiting for reports that cannot arrive.

The workflow is exercised against pynetdicom in `network/interop_ndimse_test.go`,
and the event path end to end in `network/upswatch_test.go`.

As an SCU, the library has no UPS call of its own; `upsscu` builds the
operations from the generic N-DIMSE primitives and C-FIND, each on the SOP
class PS3.4 CC.2 defines it on: N-CREATE (SCHEDULED) and Request Cancel on
Push; N-GET, N-SET and Change State (claim with a Transaction UID, complete,
cancel) on Pull; C-FIND on Query. N-GET asks for every attribute, since
`SCU.NGet` sends no Attribute Identifier List. Watch is not offered from the
command line: no command receives the event reports a subscription brings.
`scripts/interop-test.sh` runs a step's life against pynetdicom.

**Reachable through the N-DIMSE primitives.** MPPS and Basic Grayscale Print
Management have no dedicated abstraction, but they are built entirely from
N-CREATE, N-SET, N-ACTION and N-DELETE, all of which are implemented in both
directions. A caller adds the SOP class with `SetSupportedAbstractSyntaxes` and
implements `HandleNCreate`/`HandleNSet`; this is also how pynetdicom serves them.
Both workflows are exercised against pynetdicom — MPPS in both directions, print
management as film session, film box, image box and print action. The
`mppsscu` command reports a step as an SCU: an N-CREATE IN PROGRESS with every
attribute PS3.4 Table F.7.2-1 requires at creation, Type 2 ones empty when no
flag gives them, then an N-SET to COMPLETED or DISCONTINUED with the end date
and time and a Performed Series Sequence; a DICOM JSON object adds what the
flags do not cover. `scripts/interop-test.sh` runs it against pynetdicom's
MPPS SCP. dcmtk has no MPPS tool to compare with.

Relevant Patient Information Query, Display System, Media Creation Management
and the two event logging classes are the same shape: C-FIND or a single
N-service against a well-known instance, with UID constants and
presentation-context helpers provided.

All of them are now exercised end to end:

| SOP class | Verified by |
|---|---|
| Relevant Patient Information Query (all three) | A C-FIND over a real association, and against pynetdicom in `scripts/interop-test.sh`, which reports the abstract syntax it received |
| Display System | An N-GET for the well-known instance `1.2.840.10008.5.1.1.40.1`, asserting the SCP is asked about that instance and not another |
| Non-patient object information models | A C-FIND for hanging protocol, color palette and generic implant template, and against pynetdicom, which confirms the identifier carries **no** QueryRetrieveLevel |
| Media Creation Management | Against pynetdicom in both directions (`network/interop_mediaevent_test.go`, run by `scripts/interop-test.sh`): N-CREATE of a request with a Referenced SOP Sequence, N-ACTION 1 to initiate it, N-GET of its Execution Status. Each side reports the instance, action type and attributes it read |
| Procedural Event Logging, Substance Administration Logging | Against pynetdicom in both directions, the same way: N-ACTION 1 to each class's well-known instance, a Content Sequence item for the procedural event and a Product Name for the substance |

pynetdicom 2.1 serves all three classes, so none of them is tested against
go-dicom alone. Writing these tests found two defects in the N-DIMSE
primitives, recorded on their own: an SCU left the data set of an N-CREATE,
N-SET, N-ACTION or N-EVENT-REPORT response unread, so the next operation
read it as its response (#379, fixed, as is the SCP's mirror of it, which
never sent the data set its handler returned, #399; the MPPS SCP test now
checks that pynetdicom receives the attribute list); and an SCP never gave an
N-GET handler the Attribute Identifier List (#380, fixed: the media creation
SCP test now asserts the handler is given the two tags pynetdicom asks for, in
order). The media creation SCP answers N-CREATE without an attribute list.
The SCU sends an Attribute Identifier List with `SCU.NGetAttributes` (#486);
pynetdicom's N-GET SCP receives the tags in the order given and returns only
those attributes. `SCU.NGet` sends none, which asks for every attribute.

Writing those tests found a defect in the documented path.
`NonPatientObjectPresentationContexts` proposed only the five *storage* classes
and none of the information models, so the helper meant for these services could
store a hanging protocol and never query for one. It now returns both halves —
see `AllNonPatientSOPClassUIDs`.

**UIDs only.** The remaining constants have no service behind them and nothing
exercising them: composite instance retrieval, inventory and repository query, RT
machine verification, and the well-known instances the logging and print services
are addressed through.

The non-patient object information models were in this tier and have moved up:
they are dispatched to the ordinary find and get handlers, and tested. What they
still lack is a store that serves them — `dcmstore` indexes the patient hierarchy,
so a hanging protocol has nowhere to live in it. The service reaches a handler;
supplying that handler is the caller's.

They are present so a caller can name them, and the set matches pynetdicom's
exactly — 256 SOP class UIDs, each value checked against pynetdicom's own table
rather than transcribed. Treat them as a starting point rather than as support.

### 8.6 Validation against the IOD

`go-dicom validate` and package `validate` check an instance against the IOD
of its SOP class, with tables generated from PS3.3 and PS3.4 (the edition is
printed): module usage, attribute Types 1, 2 and the value of a present 1C,
value multiplicity from PS3.6, Enumerated Values and Defined Terms where the
tables list them, and item counts where a sequence's description states them.
A condition PS3.3 states in one of a few fixed sentence forms naming one
attribute by its tag ("... is present", "... is not present", "... has a
Value of X", "... Value 1 is X or Y", "... is present and has a Value of X",
with or without "May be present otherwise") is evaluated: 645 of the 1867
Type 1C and 2C attributes, 25 of the 316 Conditional modules and 45 of the
171 Conditional Functional Group Macros of 2026d. The Shared and Per-Frame
Functional Groups of the 29 enhanced IODs with a Functional Group Macros
table are checked against it: usage, where a macro may be, one per-frame
item for each frame, and each macro's attributes.
Any other condition is prose, and a Conditional module, an absent Type 1C or
2C attribute and a macro included under a condition whose condition is prose
are reported as not checked. Repeating groups (60xx), context groups (PS3.16), rules
between attributes and attributes the IOD does not name are not checked.

It has not been compared with another validator: neither `dciodvfy`
(dicom3tools) nor the Python `dicom-validator` was available where it was
built. Its findings were compared with PS3.3 by hand instead, for fifteen
corpus files of thirteen IODs: CT Image, MR Image, Secondary Capture Image,
RT Dose and MR Image with an overlay when it was written (#408), and RT
Structure Set, RT Plan, RT Ion Plan, Segmentation, Ultrasound Multi-frame
Image, Ultrasound Image, Basic Text SR, Comprehensive SR, 12-Lead ECG and
Secondary Capture Image again once conditions and functional groups were
checked (#482). Every error it reported in those files is a real defect of
the file, and no Type 1 or Type 2 attribute of a Mandatory module that a
second reading of the tables found missing went unreported.

---

## 9. How these claims are verified

Interoperability is checked in continuous integration against **pynetdicom** and
**dcmtk**, in both directions, with transferred data verified by **pydicom**
rather than by this library's own reader. Pixel decoding is compared against
pydicom's output on its published test corpus.

That arrangement exists for a specific reason. Every serious defect found in this
project has been code that was consistently wrong *with itself*: a writer and
reader agreeing on the same incorrect meta header, an encoder and decoder both
ignoring the negotiated transfer syntax, an RLE encoder and decoder agreeing on a
frame format no DICOM tool uses, four N-DIMSE messages naming their target with
the wrong element. Each round-tripped perfectly and passed every unit test. Unit
tests compare a codebase to itself and cannot detect a shared wrong assumption
between two halves of the same library.

The checks that can are the ones documented here.
