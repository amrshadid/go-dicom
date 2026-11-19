package validate

import (
	"os"
	"strings"
	"testing"
	"unicode"
)

// TestTablesCarryNoInvisibleCharacters proves the generated tables hold no
// character a reader cannot see. The standard's XML has zero-width spaces in
// the names of the camera and GPS attributes of group 0016, and they reached
// the names a finding prints: "GPS Latitude" followed by one did not equal
// "GPS Latitude" to a program reading the report.
func TestTablesCarryNoInvisibleCharacters(t *testing.T) {
	src, err := os.ReadFile("tables.go")
	if err != nil {
		t.Fatal(err)
	}
	for n, line := range strings.Split(string(src), "\n") {
		for _, r := range line {
			if unicode.Is(unicode.Cf, r) {
				t.Errorf("tables.go:%d holds the invisible character %U: %.80q", n+1, r, line)
				break
			}
		}
	}
}
