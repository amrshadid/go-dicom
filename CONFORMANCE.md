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
| Implementation Version Name | `GO-DICOM-1.3.0` |
| Documented against | `develop`, post-1.3.0 |
| Last verified | 2026-07-30 |

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

### 1.3 Sequencing

The library imposes no ordering beyond what the standard requires. An
association may carry any number of operations. Asynchronous operations are
negotiated at a window of one unless the application asks for more, so
operations are answered in the order received.

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
| Modality Worklist — FIND | `1.2.840.10008.5.1.4.31` | Not proposed by default; pass `BasicWorklistPresentationContexts()` |
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
`SetSupportedAbstractSyntaxes`.

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

**Compressed syntaxes are opt-in.** `AllTransferSyntaxes()` returns all 37 the
standard defines; pass it to `SetSupportedTransferSyntaxes` on an SCP, or use it
when building presentation contexts on an SCU. It is not the default for the
same reason it is not pynetdicom's: a context list grows with the product of SOP
classes and transfer syntaxes, and proposing every combination makes an
association request large enough to matter.

Negotiating a compressed syntax does **not** require being able to decode it.
An archive or router receives an instance, stores it, and forwards it later; the
pixel data travels as opaque bytes. [§8.1](#81-pixel-data) lists which of them
this library can actually decode.

### 3.2 Transfer syntaxes supported for files

| Transfer Syntax | Data set | Pixel data |
|---|---|---|
| Implicit VR Little Endian | read/write | yes |
| Explicit VR Little Endian | read/write | yes |
| Explicit VR Big Endian | read/write | yes |
| Deflated Explicit VR Little Endian | read/write | yes |
| RLE Lossless | read/write | **yes** |
| JPEG Baseline | read/write | yes, via the standard library |
| JPEG Extended, Lossless, JPEG-LS, JPEG 2000 | read/write | no bundled decoder |

Values in a big endian file are normalized to little endian while parsing and
converted back on write, so byte order does not reach code above the reader.

### 3.3 Extended negotiation

Supported in both directions: asynchronous operations window, SCP/SCU role
selection, and user identity negotiation. The window defaults to one.

---

## 4. Communication profiles

- **Physical media:** not applicable; this is a network and file library.
- **Transport:** TCP/IP. DICOM Upper Layer Protocol per PS3.8.
- **TLS:** available through `network.DialTLS` and `network.ListenTLS`. Plain
  DICOM over TCP is neither authenticated nor encrypted, and checking a called AE
  title is a naming convention rather than authentication.

---

## 5. Configuration

| Setting | Where |
|---|---|
| AE title | `SCPConfig.AETitle`, `SCUConfig.CallingAE` / `CalledAE` |
| Port and bind address | `SCPConfig.Port`, `SCPConfig.BindAddress` |
| Maximum PDU size | `SCPConfig.Network.MaxPDUSize` |
| Concurrent association limit | `SCPConfig.MaxAssociations` |
| Timeouts | `NetworkConfig` |
| Supported SOP classes | `SetSupportedAbstractSyntaxes` |
| Supported transfer syntaxes | `SetSupportedTransferSyntaxes` |
| C-MOVE destinations | `MoveDestinations`, `ResolveMoveDestination` |
| Commitment requestors | `CommitmentRequestors`, `ResolveCommitmentRequestor` |

---

## 6. Support of character sets

Specific Character Set (0008,0005) is honoured on read and written when a
non-default repertoire is in use. Over 30 encodings are supported, including
ISO 2022 escape sequences for Japanese, Chinese and Korean text, and the
single-byte ISO_IR sets.

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
| `network.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated data set received over an association |
| `filereader.MaxInflatedDatasetSize` | 256 MiB | Decompression bombs in a deflated file |
| `compress.MaxDecompressedSize` | 256 MiB | Decompression bombs in stored pixel data |
| `compress.MaxInflateRatio` / `MinInflateAllowance` | 1000:1, 8 MiB floor | A *small* input claiming a large expansion |
| `filereader.MaxSequenceDepth` | 64 | Unbounded recursion from deeply nested sequences |
| `valuerep.MaxUIDLength` | 64 characters | A UID longer than PS3.5 §9.1 permits |
| Element length verification | every element | An element declaring more bytes than the stream holds |

The parsers that consume untrusted input — PDU decoding, data set decoding, DIMSE
command decoding, and file reading — have fuzz targets that run in continuous
integration.

Vulnerability reporting is described in [SECURITY.md](./SECURITY.md).

---

## 8. Limitations

Stated plainly, because a conformance statement that omits them is worth less
than none.

### 8.1 Pixel data

- **JPEG 2000 does not decode.** There is no bundled codec and no hidden CGO
  path. Instances parse, store, and transfer with their pixel data intact as
  opaque bytes; supply a decoder with
  `compress.GetExternalRegistry().RegisterExternalDecoder`.

- **JPEG Extended (`.51`) decodes at 8 bits but not at 12.** The transfer syntax
  permits both, and the limitation is precision alone. A 12-bit frame is refused
  with an error naming precision as the reason.

- **JPEG-LS decodes single-component frames only.** Lossless (`.80`) and
  near-lossless (`.81`), 2 to 16 bits, which covers CT, MR, CR and the other
  grayscale modalities. A multi-component frame is **refused rather than
  decoded**: the interleaved paths were correct on small frames and diverged on
  larger ones, and a decoder that is right until row 11 is worse than one that
  declines, because the output looks like an image either way.

- JPEG Lossless (`.57` and `.70`) decodes in pure Go at every prediction
  selection value, single or multi component.
- **`PixelArray` flattens colour samples into the column dimension**, so a
  100×100 RGB frame is returned as 100 rows of 300 values. The values and their
  order are correct. `PixelArrayBySample` returns the four-dimensional shape that
  `PixelDataShape` reports.

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
- **Pixel data is decompressed to be sent uncompressed, but never compressed.**
  A data set stored under a compressed syntax and sent over a context that
  negotiated an uncompressed one has its pixel data decoded first. If it cannot
  be decoded — JPEG 2000, 12-bit JPEG Extended, color JPEG-LS — the send fails
  rather than putting encapsulated fragments on the wire described as native
  pixels, which the receiver could not detect.

  The reverse is not available: this library compresses no pixel data, so a
  native instance cannot be sent over a context that negotiated a compressed
  syntax. Negotiate an uncompressed syntax for those, which the defaults do.

### 8.3 Truncated files

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

### 8.4 Service classes

The dictionary defines UID constants for far more SOP classes than have a
service behind them, in three tiers.

**Dedicated handlers.** Verification, Storage, Query/Retrieve, Storage Commitment,
Modality Worklist and Unified Procedure Step each have a handler interface that
models the service: `StorageHandler`, `FindHandler`, `WorklistHandler`,
`UPSHandler` and so on.

Unified Procedure Step is the Push SOP class — N-CREATE, N-SET, N-GET, and
N-ACTION for changing state and requesting cancellation. `UPSHandler` enforces
the transition table in PS3.4 CC.1.1 and the Transaction UID that stops two
performers claiming one step, over a `UPSStore` the caller supplies. Subscription
and event reporting, the Watch SOP class, are **answered as unsupported rather
than accepted**: a subscription this SCP never honors would leave the requestor
waiting for reports that cannot arrive. The workflow is exercised against
pynetdicom in `network/interop_ndimse_test.go`, including the refusals.

**Reachable through the N-DIMSE primitives.** MPPS and Basic Grayscale Print
Management have no dedicated abstraction, but they are built entirely from
N-CREATE, N-SET, N-ACTION and N-DELETE, all of which are implemented in both
directions. A caller adds the SOP class with `SetSupportedAbstractSyntaxes` and
implements `HandleNCreate`/`HandleNSet`; this is also how pynetdicom serves them.
Both workflows are exercised against pynetdicom — MPPS in both directions, print
management as film session, film box, image box and print action.

Relevant Patient Information Query, Display System, Media Creation Management
and the two event logging classes are the same shape: C-FIND or a single
N-service against a well-known instance, with UID constants and
presentation-context helpers provided. Nothing verifies them against a peer.

**UIDs only.** The remaining constants in the dictionary have no service behind
them and nothing exercising them. Treat them as a starting point rather than as
support.

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
