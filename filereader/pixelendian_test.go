package filereader_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// wideImage writes a one-row image of 32-bit samples in the given byte order,
// with the attributes that say how wide a sample is.
func wideImage(order binary.ByteOrder, samples []uint32, vr string) []byte {
	var ds bytes.Buffer
	us := func(group, element, v uint16) {
		b := make([]byte, 2)
		order.PutUint16(b, v)
		ds.Write(explicitElementIn(order, group, element, "US", b))
	}
	us(0x0028, 0x0002, 1) // Samples per Pixel
	ds.Write(explicitElementIn(order, 0x0028, 0x0004, "CS", []byte("MONOCHROME2 ")))
	us(0x0028, 0x0010, 1)                    // Rows
	us(0x0028, 0x0011, uint16(len(samples))) // Columns
	us(0x0028, 0x0100, 32)                   // Bits Allocated
	us(0x0028, 0x0101, 32)                   // Bits Stored
	us(0x0028, 0x0102, 31)                   // High Bit
	us(0x0028, 0x0103, 0)                    // Pixel Representation

	pixels := make([]byte, 4*len(samples))
	for i, s := range samples {
		order.PutUint32(pixels[i*4:], s)
	}
	ds.Write(longFormElement(order, 0x7FE0, 0x0010, vr, pixels))
	return ds.Bytes()
}

func explicitElementIn(order binary.ByteOrder, group, element uint16, vr string, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, order, group)
	_ = binary.Write(&buf, order, element)
	buf.WriteString(vr)
	_ = binary.Write(&buf, order, uint16(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

func longFormElement(order binary.ByteOrder, group, element uint16, vr string, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, order, group)
	_ = binary.Write(&buf, order, element)
	buf.WriteString(vr)
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, order, uint32(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

// TestWidePixelSamplesSurviveBigEndian covers #124. Pixel Data is OW, so
// reversing byte order by VR is two-byte words. At Bits Allocated 32 that
// leaves each sample's halves transposed: an RT Dose of 1249000 was stored as
// 250085395. The accessor compensated, but only while the transfer syntax was
// still big endian, so reading a file looked right and rewriting it handed
// every other reader the scramble.
func TestWidePixelSamplesSurviveBigEndian(t *testing.T) {
	samples := []uint32{1249000, 1250000, 0x01020304}

	le := readFile(t, buildFile("1.2.840.10008.1.2.1", wideImage(binary.LittleEndian, samples, "OW"), false))
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", wideImage(binary.BigEndian, samples, "OW"), false))

	lePixels := pixelBytes(t, le)
	bePixels := pixelBytes(t, be)
	if !bytes.Equal(lePixels, bePixels) {
		t.Fatalf("the big endian file stores % x, the little endian one % x", bePixels, lePixels)
	}
	for i, want := range samples {
		if got := binary.LittleEndian.Uint32(bePixels[i*4:]); got != want {
			t.Errorf("sample %d is %d, want %d", i, got, want)
		}
	}

	// And the data set is right, not just the accessor: written back in either
	// byte order, the samples come out the same.
	for _, syntax := range []string{"1.2.840.10008.1.2.1", "1.2.840.10008.1.2.2"} {
		out := &growBufferLocal{}
		w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
		w.SetFileMetaInfo(&filewriter.FileMetaInfo{
			MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.481.2",
			MediaStorageSOPInstanceUID: "1.2.3.124",
			TransferSyntaxUID:          syntax,
		})
		if err := w.AddDataElements(filewriter.ElementsFromDataset(be.GetDataset())); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(); err != nil {
			t.Fatal(err)
		}
		back := readFile(t, out.Bytes())
		for i, want := range samples {
			if got := binary.LittleEndian.Uint32(pixelBytes(t, back)[i*4:]); got != want {
				t.Errorf("%s: sample %d reads back as %d, want %d", syntax, i, got, want)
			}
		}
	}
}

// TestOBPixelDataIsNotSwappedAtSampleWidth: OB is bytes and is never reversed
// (PS3.5 7.3), whatever Bits Allocated says. A file with 32-bit samples in OB is
// non-conformant, and reversing it would corrupt data the VR says to leave alone.
func TestOBPixelDataIsNotSwappedAtSampleWidth(t *testing.T) {
	samples := []uint32{0x01020304}
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", wideImage(binary.BigEndian, samples, "OB"), false))
	if got := pixelBytes(t, be); !bytes.Equal(got, []byte{0x01, 0x02, 0x03, 0x04}) {
		t.Errorf("OB pixel data was reordered to % x", got)
	}
}

// TestWideBigEndianCorpusFilesMatchPydicom: rtdose_expb is the real case.
func TestWideBigEndianCorpusFilesMatchPydicom(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, pair := range [][2]string{
		{"rtdose_1frame.dcm", "rtdose_expb_1frame.dcm"},
		{"rtdose.dcm", "rtdose_expb.dcm"},
	} {
		t.Run(pair[1], func(t *testing.T) {
			read := func(name string) []byte {
				raw, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Skipf("%s is not in this corpus", name)
				}
				df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				return pixelBytes(t, df)
			}
			if little, big := read(pair[0]), read(pair[1]); !bytes.Equal(little, big) {
				t.Errorf("%s and %s hold different pixel bytes", pair[0], pair[1])
			}
		})
	}
}

func pixelBytes(t *testing.T, df *filereader.DICOMFile) []byte {
	t.Helper()
	elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("no pixel data")
	}
	return elem.GetValue().([]byte)
}

type growBufferLocal struct{ buf bytes.Buffer }

func (g *growBufferLocal) Write(p []byte) (int, error)        { return g.buf.Write(p) }
func (g *growBufferLocal) Seek(_ int64, _ int) (int64, error) { return int64(g.buf.Len()), nil }
func (g *growBufferLocal) Bytes() []byte                      { return g.buf.Bytes() }
