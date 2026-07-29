package filereader_test

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// buildFile assembles a Part 10 file declaring the given transfer syntax, with
// the data set encoded by the caller.
func buildFile(transferSyntax string, dataset []byte, deflate bool) []byte {
	var meta bytes.Buffer
	writeLE := func(buf *bytes.Buffer, group, element uint16, vr string, value []byte) {
		_ = binary.Write(buf, binary.LittleEndian, group)
		_ = binary.Write(buf, binary.LittleEndian, element)
		buf.WriteString(vr)
		_ = binary.Write(buf, binary.LittleEndian, uint16(len(value)))
		buf.Write(value)
	}
	pad := func(s string) []byte {
		b := []byte(s)
		if len(b)%2 != 0 {
			b = append(b, 0)
		}
		return b
	}
	writeLE(&meta, 0x0002, 0x0002, "UI", pad("1.2.840.10008.5.1.4.1.1.4"))
	writeLE(&meta, 0x0002, 0x0003, "UI", pad("1.2.3.4.5"))
	writeLE(&meta, 0x0002, 0x0010, "UI", pad(transferSyntax))

	var out bytes.Buffer
	out.Write(make([]byte, 128))
	out.WriteString("DICM")
	// (0002,0000) group length
	_ = binary.Write(&out, binary.LittleEndian, uint16(0x0002))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0x0000))
	out.WriteString("UL")
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))
	_ = binary.Write(&out, binary.LittleEndian, uint32(meta.Len()))
	out.Write(meta.Bytes())

	// The file meta header is never compressed; only the data set that follows.
	if deflate {
		var z bytes.Buffer
		w, _ := flate.NewWriter(&z, flate.DefaultCompression)
		_, _ = w.Write(dataset)
		_ = w.Close()
		out.Write(z.Bytes())
	} else {
		out.Write(dataset)
	}
	return out.Bytes()
}

// explicitDataset encodes elements in explicit VR with the given byte order.
func explicitDataset(order binary.ByteOrder) []byte {
	var buf bytes.Buffer
	write := func(group, element uint16, vr string, value []byte) {
		_ = binary.Write(&buf, order, group)
		_ = binary.Write(&buf, order, element)
		buf.WriteString(vr)
		_ = binary.Write(&buf, order, uint16(len(value)))
		buf.Write(value)
	}

	write(0x0008, 0x0060, "CS", []byte("MR"))
	write(0x0010, 0x0010, "PN", []byte("Doe^John"))

	// Rows = 64, Columns = 128: US values, encoded in the file's byte order.
	rows := make([]byte, 2)
	order.PutUint16(rows, 64)
	write(0x0028, 0x0010, "US", rows)

	cols := make([]byte, 2)
	order.PutUint16(cols, 128)
	write(0x0028, 0x0011, "US", cols)

	// A UL value, to cover the 4-byte swap path.
	frames := make([]byte, 4)
	order.PutUint32(frames, 305419896) // 0x12345678, asymmetric on purpose
	write(0x0018, 0x1063, "UL", frames)

	return buf.Bytes()
}

func readFile(t *testing.T, raw []byte) *filereader.DICOMFile {
	t.Helper()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("ReadDICOMFile: %v", err)
	}
	return df
}

// writeSeeker adapts a bytes.Buffer to io.WriteSeeker. Only append-style
// writing is used, so Seek reports the current length.
type writeSeeker struct{ buf *bytes.Buffer }

func (w *writeSeeker) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *writeSeeker) Seek(_ int64, _ int) (int64, error) { return int64(w.buf.Len()), nil }
