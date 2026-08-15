package cli

import "testing"

// A released binary reported a different version from the same source built locally.
//
// The source declares "1.5.0". The release workflow stamps the git tag —
// -ldflags "-X main.Version=${{ github.ref_name }}" — and a tag is "v1.5.0". So the
// published binary said "go-dicom version v1.5.0", make said "1.5.0", and the
// Implementation Version Name sent to peers, GO-DICOM-1.5.0, agreed with neither.
//
// TestTheWireVersionMatchesTheCommandLineVersion cannot catch this: it runs against
// the source default and never sees the value the linker stamps in.
func TestNormalizeVersionStripsTheTagPrefix(t *testing.T) {
	cases := map[string]string{
		"v1.5.0":      "1.5.0", // as the release workflow stamps it
		"1.5.0":       "1.5.0", // as the source declares it
		"V1.5.0":      "1.5.0",
		"v1.5.0-rc.1": "1.5.0-rc.1",
		"":            "",
		"v":           "v",       // not a version, left alone
		"version":     "version", // a word beginning with v, left alone
		"vNext":       "vNext",   // ditto
	}

	for in, want := range cases {
		if got := NormalizeVersion(in); got != want {
			t.Errorf("NormalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
