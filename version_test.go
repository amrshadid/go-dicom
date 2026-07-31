package main

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/network"
)

// The version lives in two places: here, for the command line, and in
// network.DefaultImplementationVersionName, which goes on the wire in the
// A-ASSOCIATE User Information item and is how a peer identifies this
// implementation in its logs.
//
// Nothing derived one from the other, so a release that bumped one and forgot
// the other would tell every peer a version this library is not — and no test
// would notice, since neither side of an association checks it.
func TestTheWireVersionMatchesTheCommandLineVersion(t *testing.T) {
	want := "GO-DICOM-" + Version
	if got := network.DefaultImplementationVersionName; got != want {
		t.Errorf("the implementation version sent to peers is %q and the command "+
			"line reports %q; a release bumped one and not the other", got, Version)
	}
	// PS3.7 D.3.3.2 caps the Implementation Version Name at 16 characters. A
	// longer one is truncated or rejected depending on the peer.
	if n := len(want); n > 16 {
		t.Errorf("the implementation version %q is %d characters, and PS3.7 D.3.3.2 "+
			"allows 16", want, n)
	}
	if strings.TrimSpace(Version) == "" {
		t.Error("the version is empty")
	}
}
