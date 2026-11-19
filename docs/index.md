# go-dicom

A DICOM library and command line in Go: reading and writing files, pixel data
in every transfer syntax it lists, the network services, a DICOMweb client and
server, de-identification, and validation.

These pages are the guides. The reference for every package is on
[pkg.go.dev](https://pkg.go.dev/github.com/amrshadid/go-dicom), and what the
library does and does not do, service by service and syntax by syntax, is in
the [conformance statement](https://github.com/amrshadid/go-dicom/blob/main/CONFORMANCE.md).

## Start here

- [Examples: the common jobs](examples.md): each job as a command line and as
  Go, every sample run by the tests.
- [Coming from pydicom](coming-from-pydicom.md): the calls you know and their
  equivalents here.
- [Coming from dcmtk](coming-from-dcmtk.md): the commands you know and their
  equivalents here.

## Output other programs read

Two formats are written down, each with a JSON Schema, and the tests hold the
output to them:

- [`show --json`](show-json.md): every element of a file with its byte offsets
  ([schema](show-json.schema.json)).
- [The `-events` log](events.md): every PDU, presentation context and DIMSE
  message of an association, one JSON object a line
  ([schema](events.schema.json)).

## Install

```
go install github.com/amrshadid/go-dicom@latest
```

or take a binary from the
[releases](https://github.com/amrshadid/go-dicom/releases). The
[README](https://github.com/amrshadid/go-dicom#readme) has the rest.
