package cli

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// An implicit VR file states no VR, and the dictionary's "US or SS" and
// "OB or OW" were shown as they stand: numbers as bytes, and Pixel Data as
// a string of its bytes in `show --json` (#272).

// implicitImage is an Implicit VR Little Endian file of a signed 16-bit
// image, with a sequence item that leaves Pixel Representation to the data
// set it is in.
func implicitImage(t *testing.T) string {
	t.Helper()
	le16 := func(v int) []byte { return binary.LittleEndian.AppendUint16(nil, uint16(v)) }
	element := func(group, elem uint16, value []byte) []byte {
		out := binary.LittleEndian.AppendUint16(nil, group)
		out = binary.LittleEndian.AppendUint16(out, elem)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(value)))
		return append(out, value...)
	}
	const syntax = "1.2.840.10008.1.2\x00"

	var meta bytes.Buffer
	meta.Write([]byte{0x02, 0x00, 0x10, 0x00, 'U', 'I', byte(len(syntax)), 0})
	meta.WriteString(syntax)

	var f bytes.Buffer
	f.Write(make([]byte, 128))
	f.WriteString("DICM")
	f.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(meta.Len())))
	f.Write(meta.Bytes())

	f.Write(element(0x0028, 0x0100, le16(16)))    // Bits Allocated
	f.Write(element(0x0028, 0x0103, le16(1)))     // Pixel Representation: signed
	f.Write(element(0x0028, 0x0106, le16(-1000))) // Smallest Image Pixel Value
	f.Write(element(0x0028, 0x0107, le16(4000)))  // Largest Image Pixel Value
	// (0028,3010) VOI LUT Sequence, one item holding (0028,0106) again.
	item := element(0x0028, 0x0106, le16(-5))
	sequence := append(element(0xFFFE, 0xE000, nil)[:4], binary.LittleEndian.AppendUint32(nil, uint32(len(item)))...)
	sequence = append(sequence, item...)
	f.Write(element(0x0028, 0x3010, sequence))
	f.Write(element(0x7FE0, 0x0010, []byte{0x01, 0x02, 0x03, 0x04, 0x41, 0x42, 0x43, 0x44}))

	path := filepath.Join(t.TempDir(), "implicit.dcm")
	if err := os.WriteFile(path, f.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShowResolvesAmbiguousVRs(t *testing.T) {
	path := implicitImage(t)
	doc := showJSON(t, path)

	byTag := map[string]jsonElement{}
	for _, e := range doc.DataSet {
		byTag[e.Tag] = e
	}
	for tagName, want := range map[string]string{"(0028,0106)": "-1000", "(0028,0107)": "4000"} {
		e := byTag[tagName]
		if e.VR != "SS" || e.Value == nil || *e.Value != want || e.VM != 1 {
			t.Errorf("%s is %s %v (vm %d), want SS %s", tagName, e.VR, e.Value, e.VM, want)
		}
	}
	// Signed because the data set it is an item of is.
	nested := byTag["(0028,3010)"].Items[0].Elements[0]
	if nested.VR != "SS" || nested.Value == nil || *nested.Value != "-5" {
		t.Errorf("the nested value is %s %v, want SS -5", nested.VR, nested.Value)
	}
	// Bulk data, given by its position and not as text.
	pixels := byTag["(7FE0,0010)"]
	if pixels.VR != "OW" || pixels.Value != nil || pixels.ValueLength != 8 {
		t.Errorf("Pixel Data is %s with value %v, want OW and no value", pixels.VR, pixels.Value)
	}

	// The text table goes by the same VRs.
	elements, err := readDICOMFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range elements {
		switch e.Tag {
		case "0028,0107":
			if e.VR != "SS" || formatValue(e.VR, e.Value, 100) != "4000" {
				t.Errorf("the table shows (0028,0107) as %s %q", e.VR, formatValue(e.VR, e.Value, 100))
			}
		case "7FE0,0010":
			if e.VR != "OW" {
				t.Errorf("the table shows Pixel Data as %q", e.VR)
			}
		}
	}
}

// The same object in implicit and in explicit VR is shown the same.
func TestShowOfImplicitMatchesExplicit(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	explicit := showJSON(t, filepath.Join(dir, "MR_small.dcm"))
	implicit := showJSON(t, filepath.Join(dir, "MR_small_implicit.dcm"))
	// The two files do not hold quite the same elements; what both hold is
	// compared.
	other := map[string]jsonElement{}
	for _, e := range implicit.DataSet {
		other[e.Tag] = e
	}
	common := 0
	for _, e := range explicit.DataSet {
		o, ok := other[e.Tag]
		if !ok {
			continue
		}
		common++
		same := e.VR == o.VR && e.VM == o.VM && (e.Value == nil) == (o.Value == nil) &&
			(e.Value == nil || *e.Value == *o.Value)
		if !same {
			t.Errorf("%s: explicit %s vm %d %v, implicit %s vm %d %v", e.Tag, e.VR, e.VM, e.Value, o.VR, o.VM, o.Value)
		}
	}
	if common < 70 {
		t.Errorf("only %d elements are in both files", common)
	}
}
