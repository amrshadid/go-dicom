package dataset_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A frame may be stored in several fragments (PS3.5 A.4). Each fragment was
// decoded as a frame of its own, so an image stored that way did not decode
// (#238). The corpus test skips a file that fails to decode, which is how
// pydicom's examples_jpeg2k.dcm went unnoticed; this fails instead.

// rleImage is an 8-bit, 4 by 4 image of frames frames, RLE compressed, with
// each frame's codestream cut into parts fragments.
func rleImage(t *testing.T, frames, parts int, table bool) (*dataset.Dataset, []byte) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.5")
	us := func(element uint16, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	us(0x0002, 1)
	us(0x0010, 4)
	us(0x0011, 4)
	us(0x0100, 8)
	us(0x0101, 8)
	us(0x0102, 7)
	us(0x0103, 0)
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0004), dataelem.CS, []byte("MONOCHROME2 ")))
	if frames > 1 {
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte{byte('0' + frames), ' '}))
	}

	var want []byte
	var items bytes.Buffer
	var offsets []uint32
	item := func(buf *bytes.Buffer, data []byte) {
		buf.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(data)))
		buf.Write(data)
	}
	for f := 0; f < frames; f++ {
		raw := make([]byte, 16)
		for i := range raw {
			raw[i] = byte(f*50 + i*3)
		}
		want = append(want, raw...)
		stream, err := compress.NewRLECompressor().CompressFrame(raw, 1, 8)
		if err != nil {
			t.Fatalf("CompressFrame: %v", err)
		}
		offsets = append(offsets, uint32(items.Len()))
		// Cut on even boundaries, as fragments are of even length.
		size := (len(stream)/parts + 1) &^ 1
		for start := 0; start < len(stream); start += size {
			item(&items, stream[start:min(start+size, len(stream))])
		}
	}

	var value bytes.Buffer
	tableBytes := []byte{}
	if table {
		tableBytes = make([]byte, 4*len(offsets))
		for i, o := range offsets {
			binary.LittleEndian.PutUint32(tableBytes[4*i:], o)
		}
	}
	item(&value, tableBytes)
	value.Write(items.Bytes())
	value.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, value.Bytes()))
	return ds, want
}

func TestFrameStoredInSeveralFragments(t *testing.T) {
	for name, c := range map[string]struct {
		frames, parts int
		table         bool
	}{
		"one frame, three fragments":             {1, 3, false},
		"one frame, one fragment":                {1, 1, false},
		"two frames, a fragment each":            {2, 1, false},
		"two frames, two fragments each, tabled": {2, 2, true},
	} {
		t.Run(name, func(t *testing.T) {
			ds, want := rleImage(t, c.frames, c.parts, c.table)
			arr, err := ds.PixelArray()
			if err != nil {
				t.Fatalf("PixelArray: %v", err)
			}
			var got []byte
			for _, frame := range arr.([][][]uint8) {
				for _, row := range frame {
					got = append(got, row...)
				}
			}
			if !bytes.Equal(got, want) {
				t.Errorf("decoded %v, want %v", got, want)
			}
		})
	}

	// Two frames in four fragments with no table: RLE has no marker to find
	// the boundary by, and saying so beats decoding half a frame.
	ds, _ := rleImage(t, 2, 2, false)
	if _, err := ds.PixelArray(); err == nil {
		t.Error("fragments that cannot be assigned to frames were decoded")
	}
}
