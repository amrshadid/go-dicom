package anonymize

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/uid"
)

// TestReplacementUIDKeepsNothingOfTheOriginal: a replacement UID was built by
// keeping the original's first three components as a "root" and appending a
// random number, so any UID of three components or fewer, every 2.25 UID
// among them, was kept whole inside its replacement, and the cut to 64
// characters returned a 64-character original unchanged and a 63-character one
// ending in a dot (#211). PS3.15 E.1.1 requires these UIDs to be replaced;
// one the original can be read from is not replaced.
func TestReplacementUIDKeepsNothingOfTheOriginal(t *testing.T) {
	originals := []string{
		"2.25.123456789012345678901234567890123456789",
		"1.2.98765432109876543210",
		"1.2.840.113619.2.55.3.604688119.971.1155563421.123",
		"1.2." + strings.Repeat("3", 60), // 64 characters, three components
		"1.2." + strings.Repeat("3", 59), // 63 characters
	}
	a := NewAnonymizer(BasicProfile)
	for _, original := range originals {
		replaced := a.generateNewUID(original)
		components := strings.Split(original, ".")
		if strings.Contains(replaced, components[len(components)-1]) || strings.Contains(replaced, original) {
			t.Errorf("%s was replaced by %s, which still contains it", original, replaced)
		}
		if !uid.New(replaced).IsValid() || len(replaced) > 64 || strings.HasSuffix(replaced, ".") {
			t.Errorf("%s was replaced by %q, which is not a valid UID", original, replaced)
		}
		if again := a.generateNewUID(original); again != replaced {
			t.Errorf("%s was replaced by %s, then by %s", original, replaced, again)
		}
	}
}
