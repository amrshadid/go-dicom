package cli

import (
	"encoding/binary"
	"testing"
)

// The text table began the file meta group at (0002,0001), and
// `show file::0002,0000` said the element was not found, though it is the
// first one in the file and `show --json` lists it (#271).
func TestShowListsTheMetaGroupLength(t *testing.T) {
	path := writeInstance(t, t.TempDir(), "a.dcm", "1.2.840.10008.5.1.4.1.1.2", "1.2.271.1", "1.2.840.10008.1.2.1", nil)

	elements, err := readDICOMElements(path, true)
	if err != nil {
		t.Fatal(err)
	}
	first := elements[0]
	if first.Tag != "0002,0000" || first.VR != "UL" || len(first.Value) != 4 || binary.LittleEndian.Uint32(first.Value) == 0 {
		t.Errorf("the table starts with %+v, want the group length", first)
	}
	if elements[1].Tag != "0002,0001" {
		t.Errorf("the second row is %s, want (0002,0001)", elements[1].Tag)
	}

	if err := (&ShowCommand{}).Execute([]string{path + "::0002,0000"}); err != nil {
		t.Errorf("show file::0002,0000: %v", err)
	}

	// What converts or regenerates a file works from its content, and the
	// group length is the writer's to compute: those keep the list they had.
	plain, err := readDICOMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if plain[0].Tag == "0002,0000" || len(plain) != len(elements)-1 {
		t.Errorf("convert and codify would see %d elements starting with %s", len(plain), plain[0].Tag)
	}
}
