// Package commonjobs holds the runnable examples that docs/examples.md, and the
// pages for readers coming from pydicom and dcmtk, point to: reading a file,
// changing and saving it, reading the pixels, rendering a picture,
// de-identifying, walking a sequence, compressing, and sending over the
// network.
//
// The examples are Example functions, so `go test ./examples/commonjobs`
// runs every one of them and compares what each prints with the output
// written under it. A sample on the documentation pages that stopped working
// would fail the build here first. They read testdata/ct.dcm, a made-up 8 by 8
// CT image written by pydicom (scripts/commonjobs-fixture.py).
//
// The package has no code of its own.
package commonjobs
