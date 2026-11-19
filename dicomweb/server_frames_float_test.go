package dicomweb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The frames of images whose pixels are not whole bytes of integers: Float
// and Double Float Pixel Data, and a 1-bit image whose frames start inside
// a byte. scripts/interop-test.sh holds the same three kinds, written by
// pydicom, against pydicom's own reading of their frames.

// floatImage is an image of four 3 x 5 frames held in Float Pixel Data
// (bits 32) or Double Float Pixel Data (bits 64), in the transfer syntax
// given, and its frames as little endian floats.
func floatImage(t *testing.T, uid string, bits int, syntax string) (*dataset.Dataset, [][]byte) {
	t.Helper()
	const frames, rows, columns = 4, 3, 5
	ds := instance(t, uid)
	pixelTag, vr := tag.New(0x7FE0, 0x0008), dataelem.OF
	if bits == 64 {
		pixelTag, vr = tag.New(0x7FE0, 0x0009), dataelem.OD
	}
	value := make([]byte, 0, frames*rows*columns*bits/8)
	want := make([][]byte, frames)
	for i := range frames * rows * columns {
		v := (float64(i) - 7.25) * 1.5
		var sample []byte
		if bits == 32 {
			sample = binary.LittleEndian.AppendUint32(nil, math.Float32bits(float32(v)))
		} else {
			sample = binary.LittleEndian.AppendUint64(nil, math.Float64bits(v))
		}
		value = append(value, sample...)
		want[i/(rows*columns)] = append(want[i/(rows*columns)], sample...)
	}
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.30"},
		{tag.New(0x0020, 0x000E), dataelem.UI, "1.2.3.9"},
		{tag.New(0x0028, 0x0002), dataelem.US, uint16(1)},
		{tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2"},
		{tag.New(0x0028, 0x0008), dataelem.IS, fmt.Sprint(frames)},
		{tag.New(0x0028, 0x0010), dataelem.US, uint16(rows)},
		{tag.New(0x0028, 0x0011), dataelem.US, uint16(columns)},
		{tag.New(0x0028, 0x0100), dataelem.US, uint16(bits)},
		{pixelTag, vr, value},
	} {
		ds.Remove(e.t)
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	ds.SetTransferSyntaxUID(syntax)
	return ds, want
}

// TestFramesOfFloatPixelData checks that the frames of Float and Double
// Float Pixel Data are served, uncompressed by default and as stored, each
// frame's floats little endian whether the file is little or big endian,
// and that such an image is refused by name when it is to be drawn.
func TestFramesOfFloatPixelData(t *testing.T) {
	for _, c := range []struct {
		bits   int
		syntax string
	}{
		{32, syntaxExplicitLE}, {64, syntaxExplicitLE},
		{32, "1.2.840.10008.1.2.2"}, {64, "1.2.840.10008.1.2.2"},
		{32, "1.2.840.10008.1.2"},
	} {
		t.Run(fmt.Sprintf("%d-bit in %s", c.bits, c.syntax), func(t *testing.T) {
			ds, want := floatImage(t, "1.2.3.71", c.bits, c.syntax)
			file, err := encodeFile(ds)
			if err != nil {
				t.Fatal(err)
			}
			backend := &recorder{}
			backend.hold("1.2.3.71", file)
			for _, accept := range []string{"", acceptStored} {
				response, body := get(t, backend, heldInstances+"1.2.3.71/frames/4,2", "Accept", accept)
				if response.StatusCode != http.StatusOK {
					t.Fatalf("Accept %q: status %d: %s", accept, response.StatusCode, body)
				}
				bodies, types, _ := frameParts(t, response, body)
				if len(bodies) != 2 {
					t.Fatalf("Accept %q: %d parts", accept, len(bodies))
				}
				for i, n := range []int{4, 2} {
					if !bytes.Equal(bodies[i], want[n-1]) {
						t.Errorf("Accept %q: frame %d is % x, want % x", accept, n, bodies[i], want[n-1])
					}
					if types[i] != "application/octet-stream; transfer-syntax="+syntaxExplicitLE {
						t.Errorf("Accept %q: frame %d is labeled %q", accept, n, types[i])
					}
				}
			}
			response, body := get(t, backend, heldInstances+"1.2.3.71/frames/1/rendered")
			if response.StatusCode != http.StatusNotImplemented || !bytes.Contains([]byte(body), []byte("Float")) {
				t.Errorf("rendered: status %d: %s", response.StatusCode, body)
			}
		})
	}
}

// TestFramesOfPackedOneBitImage checks a 1-bit image of 5 x 5 frames, 25
// bits each, packed with no padding between frames as PS3.5 8.1.1 has it:
// every frame, whichever bit of a byte it starts at, is sent starting at
// the lowest bit of its first byte, its 25 pixels the image's, and the
// seven bits after them zero.
func TestFramesOfPackedOneBitImage(t *testing.T) {
	const frames, rows, columns = 5, 5, 5
	pixels := make([]byte, frames*rows*columns)
	for i := range pixels {
		pixels[i] = byte((i*7 + i/3) % 2)
	}
	packed := make([]byte, 16) // 125 bits, in an even number of bytes
	for i, p := range pixels {
		packed[i/8] |= p << (i % 8)
	}
	// Big endian in OW, the 16-bit words reversed (PS3.5 D.2), so a frame
	// that starts inside a byte is also cut from inside a word.
	for _, c := range []struct {
		syntax string
		vr     dataelem.VR
	}{{syntaxExplicitLE, dataelem.OB}, {"1.2.840.10008.1.2.2", dataelem.OW}} {
		syntax := c.syntax
		t.Run(syntax, func(t *testing.T) {
			ds := multiFrame(t, "1.2.3.72", frames, rows, columns)
			for _, e := range []struct {
				t     tag.Tag
				vr    dataelem.VR
				value any
			}{
				{tag.New(0x0028, 0x0100), dataelem.US, uint16(1)},
				{tag.New(0x0028, 0x0101), dataelem.US, uint16(1)},
				{tag.New(0x0028, 0x0102), dataelem.US, uint16(0)},
				{tag.New(0x7FE0, 0x0010), c.vr, packed},
			} {
				ds.Remove(e.t)
				if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
					t.Fatal(err)
				}
			}
			ds.SetTransferSyntaxUID(syntax)
			file, err := encodeFile(ds)
			if err != nil {
				t.Fatal(err)
			}
			backend := &recorder{}
			backend.hold("1.2.3.72", file)
			response, body := get(t, backend, heldInstances+"1.2.3.72/frames/2,5,1,4,3", "Accept", acceptStored)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", response.StatusCode, body)
			}
			bodies, _, _ := frameParts(t, response, body)
			for i, n := range []int{2, 5, 1, 4, 3} {
				if len(bodies[i]) != 4 {
					t.Fatalf("frame %d is %d bytes, want 4", n, len(bodies[i]))
				}
				for p := range rows * columns {
					if got, want := bodies[i][p/8]>>(p%8)&1, pixels[(n-1)*rows*columns+p]; got != want {
						t.Errorf("frame %d pixel %d is %d, want %d", n, p, got, want)
					}
				}
				if pad := bodies[i][3] >> 1; pad != 0 {
					t.Errorf("frame %d: the bits after its last pixel are %b, not zero", n, pad)
				}
			}

			// Drawn, frame 2 is its own pixels, white where they are set.
			response, body = get(t, backend, heldInstances+"1.2.3.72/frames/2/rendered", "Accept", "image/png")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("rendered: status %d: %s", response.StatusCode, body)
			}
			picture, err := png.Decode(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			for p := range rows * columns {
				gray, _, _, _ := picture.At(p%columns, p/columns).RGBA()
				if set := gray > 0x7FFF; set != (pixels[rows*columns+p] == 1) {
					t.Errorf("rendered frame 2 pixel %d is %#x, the pixel is %d", p, gray, pixels[rows*columns+p])
				}
			}
		})
	}
}
