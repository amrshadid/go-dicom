package cli

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The table cut every value at a hundred bytes, and so did `show
// file::Element`: a long value could not be read from the command line
// except through --json. And a number VR holding several values was not
// printed as numbers at all (#282).

// longValues is an Explicit VR Little Endian file with a text value of 1500
// characters, numbers in twos and threes, and a binary value of 300 bytes.
func longValues(t *testing.T) (path, text string) {
	t.Helper()
	text = strings.Repeat("word ", 300)
	const syntax = "1.2.840.10008.1.2.1\x00"
	short := func(group, elem uint16, vr string, value []byte) []byte {
		out := binary.LittleEndian.AppendUint16(nil, group)
		out = binary.LittleEndian.AppendUint16(out, elem)
		out = append(out, vr...)
		out = binary.LittleEndian.AppendUint16(out, uint16(len(value)))
		return append(out, value...)
	}
	long := func(group, elem uint16, vr string, value []byte) []byte {
		out := binary.LittleEndian.AppendUint16(nil, group)
		out = binary.LittleEndian.AppendUint16(out, elem)
		out = append(out, vr...)
		out = append(out, 0, 0)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(value)))
		return append(out, value...)
	}
	var doubles []byte
	for _, v := range []float64{0.5, -2, 1e-3} {
		doubles = binary.LittleEndian.AppendUint64(doubles, math.Float64bits(v))
	}

	var meta bytes.Buffer
	meta.Write(short(0x0002, 0x0010, "UI", []byte(syntax)))
	var f bytes.Buffer
	f.Write(make([]byte, 128))
	f.WriteString("DICM")
	f.Write([]byte{0x02, 0x00, 0x00, 0x00, 'U', 'L', 4, 0})
	f.Write(binary.LittleEndian.AppendUint32(nil, uint32(meta.Len())))
	f.Write(meta.Bytes())
	f.Write(short(0x0018, 0x9087, "FD", doubles))                   // Diffusion b-value, as three doubles
	f.Write(short(0x0020, 0x4000, "LT", []byte(text)))              // Image Comments
	f.Write(short(0x0028, 0x0034, "IS", []byte("1\\1")))            // Pixel Aspect Ratio
	f.Write(short(0x0028, 0x3002, "US", []byte{0, 1, 0, 0, 16, 0})) // LUT Descriptor: 256\0\16
	f.Write(long(0x0042, 0x0011, "OB", bytes.Repeat([]byte{0xAB}, 300)))

	path = filepath.Join(t.TempDir(), "long.dcm")
	if err := os.WriteFile(path, f.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, text
}

func showText(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := (&ShowCommand{out: &out}).Execute(args); err != nil {
		t.Fatalf("show %v: %v", args, err)
	}
	return out.String()
}

func rowOf(t *testing.T, listing, tagName string) string {
	t.Helper()
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, tagName) {
			return line
		}
	}
	t.Fatalf("no row for %s in:\n%s", tagName, listing)
	return ""
}

func TestShowPrintsAWholeValueWhenAskedForOne(t *testing.T) {
	path, text := longValues(t)
	want := strings.TrimRight(text, " ")

	// The table is read in a terminal, and stays cut.
	table := showText(t, path)
	if row := rowOf(t, table, "0020,4000"); strings.Contains(row, want) || !strings.HasSuffix(row, "... (1500 bytes)") {
		t.Errorf("the table's row is not cut: %.80s ... %s", row, row[max(0, len(row)-30):])
	}

	// One element asked for is its value.
	if got := showText(t, path+"::0020,4000"); !strings.Contains(got, want) || strings.Contains(got, "... (") {
		t.Errorf("::Element does not print the whole value: %.80s ... %s", got, got[max(0, len(got)-40):])
	}
	// And the table prints whole values when told to.
	full := showText(t, path, "--full")
	if row := rowOf(t, full, "0020,4000"); !strings.Contains(row, want) || strings.Contains(row, "... (") {
		t.Errorf("--full cuts the value: %.80s", row)
	}
	// Bytes are counted, never printed, cut or not.
	if row := rowOf(t, table, "0042,0011"); !strings.HasSuffix(row, "<100 bytes>... (300 bytes)") {
		t.Errorf("the table shows the binary value as %q", row)
	}
	if row := rowOf(t, full, "0042,0011"); !strings.HasSuffix(row, "<300 bytes>") {
		t.Errorf("--full shows the binary value as %q", row)
	}
}

func TestShowPrintsEveryValueOfANumber(t *testing.T) {
	path, _ := longValues(t)
	table := showText(t, path)
	for tagName, want := range map[string]string{
		"0028,3002": `256\0\16`,
		"0018,9087": `0.5\-2\0.001`,
		"0028,0034": `1\1`,
	} {
		if row := rowOf(t, table, tagName); !strings.HasSuffix(row, want) {
			t.Errorf("%s is shown as %q, want it to end %q", tagName, row, want)
		}
	}
	// The table and --json say the same of a number.
	doc := showJSON(t, path)
	for _, e := range doc.DataSet {
		if e.Tag == "(0028,3002)" && (e.Value == nil || !strings.HasSuffix(rowOf(t, table, "0028,3002"), *e.Value)) {
			t.Errorf("--json has %v for the LUT Descriptor, the table %q", e.Value, rowOf(t, table, "0028,3002"))
		}
	}
}
