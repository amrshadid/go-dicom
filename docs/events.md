# The `-events` log

The nine network commands (`echoscu`, `echoscp`, `storescu`, `storescp`,
`findscu`, `movescu`, `getscu`, `commitscu` and `qrscp`) take `-events` and write what happens on each association to a log, one JSON object per
line. A program can follow the file while the command runs. This page is the
contract for a line; the schema is [events.schema.json](events.schema.json), a
JSON Schema (draft 2020-12) for **one line**, not for the file.

```
go-dicom storescu -aec ARCHIVE -events association.jsonl 127.0.0.1:11112 ct.dcm
go-dicom qrscp -events - -output received          # to standard output
```

- `-events <file>` appends to the file, creating it; a client and a server, or
  one run after another, can share one file. `-events -` writes to standard
  output.
- `-events-pdu-bytes <n>` is the most bytes of one PDU written (default 65536;
  0 writes every byte). A longer PDU is cut and its line says so.
- A line is written whole and in one write, so a reader following the file
  never sees part of one. The order of lines is the order the events happened
  in one association; lines of different associations are interleaved.

## What version 1 means

Every line ends with `"schemaVersion":1`, the version of the format this page
and the schema describe:

```
{"event":"EVT_C_ECHO","time":"2026-11-18T19:14:07.082782Z",...,"schemaVersion":1}
```

It is on **every line**, not in a record at the start of the file: a reader
that starts following a file midway, or reads one that a client and a server
append to, can check each line it reads, and every line keeps the `event` and
`time` a line of this log has. It is the **last** property, so a line is
exactly the line go-dicom 1.10.0 wrote with `,"schemaVersion":1` before the
closing brace. go-dicom 1.10.0 wrote no `schemaVersion`; a line without one is
version 1.

Version 1 means:

- **A property or an event name added later does not make a new version.** A
  reader ignores properties and events it does not know. `schemaVersion`
  itself was added this way.
- **A property that is documented here keeps its name, its type and its
  meaning.** Renaming or removing one, or changing what it means, would be a
  new version, with a new number on every line. A reader treats a line with a
  `schemaVersion` it does not know as one it cannot read, and a line with
  none, or with 1, as version 1.
- What is not part of the contract: the order of properties inside a line, and
  the text of `description` and `error`, which are for a person. (go-dicom
  writes `schemaVersion` last all the same, and keeps it there.)

## A line

Every line has `event`, `time` and `schemaVersion`. The rest is present when it
is known.

| Property | Type | Meaning |
|---|---|---|
| `event` | string | Which event: a name from the table below, or `STORED`. |
| `time` | string | When, in UTC, with microseconds: `2026-11-18T19:14:07.082782Z`. |
| `associationId` | integer | The association the line belongs to, from 1. Numbered in each process, so two processes writing to one file reuse numbers; pair it with `localAddr` and `remoteAddr`. |
| `role` | string | `SCU` on the side that requested the association, `SCP` on the side that accepted it. |
| `localAE`, `remoteAE` | string | The AE titles of this side and the other. |
| `localAddr`, `remoteAddr` | string | `host:port` of each side. |
| `pdu` | object | The PDU, on a line about one (see below). |
| `presentationContexts` | array | On `EVT_ACCEPTED` and `EVT_ESTABLISHED`: each context as proposed and as answered: `id`, `abstractSyntax`, `transferSyntaxes` (proposed), `result`, `resultText`, `transferSyntax` (when accepted). |
| `dimse` | object | A DIMSE message, on a line about one (see below). |
| `stored` | object | On a `STORED` line. |
| `description`, `error` | string | Free text, and the error that failed or ended what the line reports. |
| `schemaVersion` | integer | 1, the version of the format; the last property of every line. |

`EVT_CONN_OPEN` comes before the association is negotiated, so it has the
addresses and no role or AE titles.

### Events

| `event` | When |
|---|---|
| `EVT_CONN_OPEN`, `EVT_CONN_CLOSE` | The TCP connection opened, and closed. |
| `EVT_REQUESTED` | An A-ASSOCIATE-RQ was sent or received. |
| `EVT_ACCEPTED` | An A-ASSOCIATE-AC. |
| `EVT_REJECTED` | An A-ASSOCIATE-RJ. |
| `EVT_ESTABLISHED` | The association is up. |
| `EVT_RELEASED` | A normal release. |
| `EVT_ABORTED` | An A-ABORT, from either side. |
| `EVT_PDU_SENT`, `EVT_PDU_RECV` | Every PDU, either way. |
| `EVT_DIMSE_SENT`, `EVT_DIMSE_RECV` | Every DIMSE message, either way. |
| `EVT_C_ECHO`, `EVT_C_STORE`, `EVT_C_FIND`, `EVT_C_MOVE`, `EVT_C_GET` | A request of that service was received (an SCP's line). |
| `EVT_N_EVENT_REPORT`, `EVT_N_GET`, `EVT_N_SET`, `EVT_N_ACTION`, `EVT_N_CREATE`, `EVT_N_DELETE` | A request of that service was received. |
| `EVT_TLS_HANDSHAKE_FAILED` | A server started with `-tls` turned a client away during the TLS handshake, before any association. `error` names the check that failed (`not-tls`, `no-client-certificate`, `unknown-authority`, `certificate-invalid`, `peer-alert`, `protocol`, `connection`) and quotes nothing of a certificate. Only TLS servers write it. |
| `STORED` | Not a network event: the command's own line for an instance it kept, with `stored`. It carries the association's identity when it came on one. |

The pynetdicom names that go-dicom declares and does not emit
(`EVT_ACSE_RECV`, `EVT_ACSE_SENT`, `EVT_DATA_RECV`, `EVT_DATA_SENT`,
`EVT_FSM_TRANSITION` and the negotiation events) never appear.

### `pdu`

| Property | Meaning |
|---|---|
| `type` | `A-ASSOCIATE-RQ`, `A-ASSOCIATE-AC`, `A-ASSOCIATE-RJ`, `P-DATA-TF`, `A-RELEASE-RQ`, `A-RELEASE-RP`, `A-ABORT`, or `UNKNOWN`. |
| `typeCode` | The first byte of the PDU (PS3.8 section 9.3). |
| `length` | The PDU's own length field: the bytes after its 6-byte header. |
| `bytes` | The PDU as it crossed the connection, in lower-case hex. Decoding it gives the PDU back. |
| `bytesTruncated` | `true`, and only then present: `bytes` is the start of a longer PDU. |
| `callingAE`, `calledAE`, `maxPDULength`, `implementationClassUID`, `implementationVersion` | Of an A-ASSOCIATE-RQ or -AC. |
| `proposedContexts` | Of an A-ASSOCIATE-RQ: `{id, abstractSyntax, transferSyntaxes}`. |
| `answeredContexts` | Of an A-ASSOCIATE-AC: `{id, result, resultText, transferSyntax}`. |
| `result`, `source`, `reason` | Of an A-ASSOCIATE-RJ (all three) or an A-ABORT (`source` and `reason`), as PS3.8 numbers them. |
| `pdvs` | Of a P-DATA-TF: `{contextId, command, last, length}` for each presentation data value. `command` is true for a fragment of a command, `last` for the last fragment of its message. |

### `dimse`

`command` (the name, such as `C-STORE-RQ`), `commandField`, `contextId`,
`hasDataSet`, and where they apply: `messageId` on a request,
`messageIdBeingRespondedTo` on a response, `affectedSOPClassUID`,
`affectedSOPInstanceUID`, `requestedSOPClassUID`, `requestedSOPInstanceUID`,
`moveDestination`, `status`, and the sub-operation counts `remaining`,
`completed`, `failed`, `warning`. A number that is absent was not in the
message; `status` of 0 is present and means success.

### `stored`

`{sopClassUID, sopInstanceUID, path}`: the instance that was kept and where
it was written.

## Checking a line

```
python -c "
import json, sys, jsonschema
schema = json.load(open('docs/events.schema.json'))
for line in sys.stdin:
    jsonschema.validate(json.loads(line), schema)" < association.jsonl
```

The tests of the `cli` package run a store, an echo and a query over TCP and a
refused association, and check every line the client and the server wrote
against the schema with a validator of their own, and fail on a property the
schema does not name, so a field cannot be added to the log without being
described here.
