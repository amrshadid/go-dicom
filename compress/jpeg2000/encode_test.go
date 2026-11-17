package jpeg2000

import (
	"math/rand"
	"testing"
)

// Encode is lossless, so its test is equality: an image encoded and decoded
// is the image. Here the decoder is this package's own, which reads what
// OpenJPEG and pydicom's corpus write; that OpenJPEG reads what Encode
// writes is checked in the corpus tests and the interoperability suite.

// testImage builds an image whose samples come from value.
func testImage(w, h, components, depth int, signed bool, value func(c, x, y int) int32) *Image {
	img := &Image{Width: w, Height: h}
	for c := 0; c < components; c++ {
		plane := make([]int32, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				plane[y*w+x] = value(c, x, y)
			}
		}
		img.Components = append(img.Components, plane)
		img.Depths = append(img.Depths, depth)
		img.Signed = append(img.Signed, signed)
	}
	return img
}

// encodeDecode encodes an image, parses the codestream and decodes it.
func encodeDecode(t *testing.T, img *Image) (*Codestream, *Image, []byte) {
	t.Helper()
	data, err := Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	codestream, err := ParseCodestream(data)
	if err != nil {
		t.Fatalf("ParseCodestream of what Encode wrote: %v", err)
	}
	decoded, err := Decode(codestream)
	if err != nil {
		t.Fatalf("Decode of what Encode wrote: %v", err)
	}
	return codestream, decoded, data
}

func sameImage(t *testing.T, name string, got, want *Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height || len(got.Components) != len(want.Components) {
		t.Fatalf("%s: decoded %dx%d with %d components, encoded %dx%d with %d",
			name, got.Width, got.Height, len(got.Components), want.Width, want.Height, len(want.Components))
	}
	for c := range want.Components {
		for i, v := range want.Components[c] {
			if got.Components[c][i] != v {
				t.Fatalf("%s: component %d sample %d came back %d, was %d", name, c, i, got.Components[c][i], v)
			}
		}
	}
}

// TestEncodeIsLossless covers what a DICOM image can be: 8, 12 and 16 bits
// and the odd depths between, signed and unsigned, one component and three,
// sizes from one sample to several code-blocks and not a multiple of
// anything, and content from flat to noise at the full range.
func TestEncodeIsLossless(t *testing.T) {
	random := rand.New(rand.NewSource(90))
	sizes := [][2]int{{1, 1}, {1, 5}, {5, 1}, {2, 2}, {3, 7}, {16, 16}, {64, 64}, {65, 63}, {100, 37}, {200, 130}}
	for _, depth := range []int{1, 8, 10, 12, 15, 16} {
		for _, signed := range []bool{false, true} {
			lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
			span := int64(hi) - int64(lo) + 1
			contents := map[string]func(c, x, y int) int32{
				"noise at full range": func(int, int, int) int32 { return lo + int32(random.Int63n(span)) },
				"flat at the lowest":  func(int, int, int) int32 { return lo },
				"flat at the highest": func(int, int, int) int32 { return hi },
				"a gradient": func(c, x, y int) int32 {
					return lo + int32((int64(x)*31+int64(y)*17+int64(c)*5)%span)
				},
				"extremes side by side": func(_, x, y int) int32 {
					if (x+y)%2 == 0 {
						return lo
					}
					return hi
				},
			}
			for name, value := range contents {
				for _, size := range sizes {
					for _, components := range []int{1, 3} {
						img := testImage(size[0], size[1], components, depth, signed, value)
						codestream, decoded, _ := encodeDecode(t, img)
						sameImage(t, name, decoded, img)
						if got := codestream.Coding.MCT; got != (components == 3) {
							t.Fatalf("%s: the component transform is %v for %d components", name, got, components)
						}
						if decoded.Depths[0] != depth || decoded.Signed[0] != signed {
							t.Fatalf("%s: decoded as %d bits signed=%v", name, decoded.Depths[0], decoded.Signed[0])
						}
					}
				}
			}
		}
	}
}

// TestEncodeWritesWhatItSays checks the header against the image and the
// choices Encode makes: one tile over the whole image, one layer, the
// reversible filter, no quantization, blocks of 64, and every byte of the
// tile accounted for by its packets.
func TestEncodeWritesWhatItSays(t *testing.T) {
	random := rand.New(rand.NewSource(4))
	img := testImage(300, 200, 3, 12, false, func(int, int, int) int32 { return int32(random.Intn(4096)) })
	c, _, data := encodeDecode(t, img)

	if c.Width != 300 || c.Height != 200 || c.TileWidth != 300 || c.TileHeight != 200 || c.NumTiles() != 1 {
		t.Errorf("image %dx%d in tiles of %dx%d", c.Width, c.Height, c.TileWidth, c.TileHeight)
	}
	style := c.Coding
	if style.Layers != 1 || style.Wavelet != Wavelet53Reversible || style.Levels != 5 ||
		style.CodeBlockWidth != 64 || style.CodeBlockHt != 64 || style.Progression != ProgressionLRCP ||
		style.BlockStyle != 0 || style.SOP || style.EPH || style.UsePrecincts {
		t.Errorf("coding style %+v", style)
	}
	if c.Quant.Style != QuantNone || len(c.Quant.Exponents) != 1+3*5 {
		t.Errorf("quantization %+v", c.Quant)
	}
	if string(data[:2]) != "\xFF\x4F" || string(data[len(data)-2:]) != "\xFF\xD9" {
		t.Errorf("the codestream does not begin with SOC and end with EOC")
	}

	packets, err := c.ReadTilePackets(0)
	if err != nil {
		t.Fatal(err)
	}
	if packets.BytesRead != packets.TileBytes {
		t.Errorf("the packets account for %d of the tile's %d bytes", packets.BytesRead, packets.TileBytes)
	}
}

// TestEncodeCompresses checks that a smooth image comes out much smaller
// than its samples and noise does not: the coder is doing its work, and is
// not expanding what cannot be compressed by more than its overhead.
func TestEncodeCompresses(t *testing.T) {
	smooth := testImage(256, 256, 1, 16, false, func(_, x, y int) int32 { return int32(x*100 + y*50) })
	data, err := Encode(smooth)
	if err != nil {
		t.Fatal(err)
	}
	if raw := 256 * 256 * 2; len(data) > raw/10 {
		t.Errorf("a smooth 16-bit ramp of %d bytes took %d", raw, len(data))
	}

	random := rand.New(rand.NewSource(1))
	noise := testImage(256, 256, 1, 8, false, func(int, int, int) int32 { return int32(random.Intn(256)) })
	data, err = Encode(noise)
	if err != nil {
		t.Fatal(err)
	}
	if raw := 256 * 256; len(data) > raw+raw/8 {
		t.Errorf("8-bit noise of %d bytes took %d", raw, len(data))
	}
}

// TestEncodeRefusals checks what cannot be written: nothing a decoder would
// return differently is encoded.
func TestEncodeRefusals(t *testing.T) {
	good := func() *Image { return testImage(4, 4, 1, 8, false, func(int, int, int) int32 { return 7 }) }
	cases := map[string]func(*Image) *Image{
		"no image":                        func(*Image) *Image { return nil },
		"no size":                         func(i *Image) *Image { i.Width = 0; return i },
		"no components":                   func(i *Image) *Image { i.Components = nil; return i },
		"a plane of the wrong size":       func(i *Image) *Image { i.Components[0] = i.Components[0][:15]; return i },
		"17 bits":                         func(i *Image) *Image { i.Depths[0] = 17; return i },
		"no bits":                         func(i *Image) *Image { i.Depths[0] = 0; return i },
		"a sample above its depth":        func(i *Image) *Image { i.Components[0][3] = 256; return i },
		"a negative sample, unsigned":     func(i *Image) *Image { i.Components[0][3] = -1; return i },
		"a sample below its signed depth": func(i *Image) *Image { i.Signed[0] = true; i.Components[0][3] = -129; return i },
		"components of different depths": func(i *Image) *Image {
			i.Components = append(i.Components, i.Components[0])
			i.Depths = append(i.Depths, 12)
			i.Signed = append(i.Signed, false)
			return i
		},
	}
	for name, spoil := range cases {
		if _, err := Encode(spoil(good())); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
}

// FuzzEncodeRoundTrip gives Encode arbitrary samples, sizes and depths.
func FuzzEncodeRoundTrip(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, uint8(3), uint8(8), false, false)
	f.Add([]byte{255, 255, 0, 0, 128, 7, 9, 200}, uint8(2), uint8(16), true, false)
	f.Fuzz(func(t *testing.T, raw []byte, width uint8, depthByte uint8, signed, color bool) {
		depth := int(depthByte%16) + 1
		w := int(width%40) + 1
		components := 1
		if color {
			components = 3
		}
		h := len(raw) / 2 / w / components
		if h == 0 || h > 80 {
			return
		}
		lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
		span := int64(hi) - int64(lo) + 1
		k := 0
		img := testImage(w, h, components, depth, signed, func(int, int, int) int32 {
			v := int64(raw[k])<<8 | int64(raw[k+1])
			k += 2
			return lo + int32(v%span)
		})
		_, decoded, _ := encodeDecode(t, img)
		sameImage(t, "fuzzed", decoded, img)
	})
}
