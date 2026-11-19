package jpeg2000

import (
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
)

// allocatedBy reports how many bytes fn allocated on the heap, as network's
// PDU tests measure it.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// headersWithoutData are the two inputs FuzzParseCodestream found (#526): a
// SIZ of about 185 million samples, decomposed 32 times into 4x4 code-blocks,
// over a tile of a byte or two. Decode allocated 4.3 and 4.6 GB for them. They
// are also seeds of the fuzz test, under testdata/fuzz.
var headersWithoutData = []string{
	"\xffO\xffQ\x00)00\x00\x00  \x00\x00X\x15\x00\x00\x00\x12\x00\x00\x00\x0000000000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\a\x01\x01\xffR\x00\f0\x0000\x00 000\x01\xff\\\x00\x04 0\xff\x90\x00\n\x00\x00\x00\x00\x00\x00000",
	"\xffO\xffQ\x00)00\x00\x0000\x00\x0000\x00\x00\x00\x00\x00\x00\x00\x0000000000\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\a\x01\x01\xffR\x00\f2\x0000\x00 00\x00\x01\xff\\\x00\x04 0\xff\x90\x00\n\x00\x00\x00\x00\x00\x00000",
}

// TestAHeaderTheDataCannotBackIsRefusedBeforeItIsAllocated decodes the inputs
// of #526 and requires an error that says why, with less than a megabyte
// allocated on the way: the header declares more precincts than the tile has
// bytes, so the image it describes is never laid out.
func TestAHeaderTheDataCannotBackIsRefusedBeforeItIsAllocated(t *testing.T) {
	for i, input := range headersWithoutData {
		c, err := ParseCodestream([]byte(input))
		if err != nil {
			t.Fatalf("input %d: the header no longer parses, so this test no longer reaches Decode: %v", i, err)
		}
		var decodeErr error
		allocated := allocatedBy(func() { _, decodeErr = Decode(c) })
		if decodeErr == nil || !strings.Contains(decodeErr.Error(), "more precincts than its") {
			t.Errorf("input %d: Decode returned %v, want the precincts refused", i, decodeErr)
		}
		if allocated > 1<<20 {
			t.Errorf("input %d (%dx%d samples): Decode allocated %d bytes", i,
				c.Width-c.XOffset, c.Height-c.YOffset, allocated)
		}
	}
}

// emptyCodestream builds a codestream of one 8-bit component, one tile and
// one precinct per resolution, in 4x4 code-blocks, whose packets all say that
// no block is included yet: each sets the packet's first bit and then one 0
// bit per band, which takes the inclusion tree's root past the layer. It is
// the cheapest complete description of a large image, which decodes to
// mid-gray.
func emptyCodestream(side, levels, layers int) []byte {
	be16 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	be32 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }

	out := []byte{0xFF, 0x4F, 0xFF, 0x51}
	out = be16(out, 41)
	out = be16(out, 0)
	for _, v := range []int{side, side, 0, 0, side, side, 0, 0} {
		out = be32(out, v)
	}
	out = be16(out, 1)
	out = append(out, 7, 1, 1)

	out = append(out, 0xFF, 0x52)
	out = be16(out, 12)
	out = append(out, 0, ProgressionLRCP)
	out = be16(out, layers)
	out = append(out, 0, byte(levels), 0, 0, 0, Wavelet53Reversible)

	out = append(out, 0xFF, 0x5C)
	out = be16(out, 3+1+3*levels)
	out = append(out, 2<<5|QuantNone)
	for i := 0; i < 1+3*levels; i++ {
		out = append(out, 10<<3)
	}

	packets := layers * (levels + 1)
	out = append(out, 0xFF, 0x90)
	out = be16(out, 10)
	out = be16(out, 0)
	out = be32(out, 12+2+packets)
	out = append(out, 0, 1, 0xFF, 0x93)
	for i := 0; i < packets; i++ {
		out = append(out, 0x80)
	}
	return append(out, 0xFF, 0xD9)
}

// TestPacketHeadersCostTheirBitsNotTheirPrecincts reads the packets of an
// image of 2048 by 2048 in 4x4 code-blocks, 349,525 blocks in one precinct
// per resolution, over 20 layers of packets that include none of them, and
// requires the reading to allocate under 4 MiB. Visiting every block of a
// precinct in every packet, with a state for each, allocated hundreds of
// megabytes for these 120 bytes. The image must still decode, to mid-gray.
func TestPacketHeadersCostTheirBitsNotTheirPrecincts(t *testing.T) {
	data := emptyCodestream(2048, 5, 20)
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	var tile *TileData
	allocated := allocatedBy(func() { tile, err = c.ReadTilePackets(0) })
	if err != nil {
		t.Fatal(err)
	}
	if tile.BytesRead != tile.TileBytes || len(tile.Blocks) != 0 {
		t.Fatalf("read %d of %d bytes and %d blocks, want every byte and no block",
			tile.BytesRead, tile.TileBytes, len(tile.Blocks))
	}
	if allocated > 4<<20 {
		t.Errorf("reading %d bytes of packets allocated %d bytes", tile.TileBytes, allocated)
	}

	img, err := Decode(c)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range img.Components[0] {
		if v != 128 {
			t.Fatalf("sample %d is %d, want 128", i, v)
		}
	}
}

// TestDecodeCostsItsImage decodes a codestream that is within every bound,
// an empty image of 2048 by 2048 in 4x4 code-blocks over 20 layers, and holds
// what Decode allocates to 16 bytes a sample: the decoded planes at four, and
// the coefficients of the tile while it is rebuilt. This is the cost the
// package documentation states for a codestream that is small and describes a
// large image, as JPEG 2000 can; the test is what keeps it that cost.
func TestDecodeCostsItsImage(t *testing.T) {
	c, err := ParseCodestream(emptyCodestream(2048, 5, 20))
	if err != nil {
		t.Fatal(err)
	}
	allocated := allocatedBy(func() { _, err = Decode(c) })
	if err != nil {
		t.Fatal(err)
	}
	samples := uint64(2048 * 2048)
	t.Logf("%d bytes for %d samples, %.1f a sample", allocated, samples, float64(allocated)/float64(samples))
	if allocated > 16*samples {
		t.Errorf("Decode allocated %d bytes for %d samples, more than 16 a sample", allocated, samples)
	}
}

// TestComponentsCountTowardsTheImageLimit declares an image of 8192 by 8192
// in five components: each plane is within the limit and the five are not.
func TestComponentsCountTowardsTheImageLimit(t *testing.T) {
	c := &Codestream{Width: 8192, Height: 8192, Components: make([]Component, 5)}
	if err := checkImageSize(c); err == nil || !strings.Contains(err.Error(), "in 5 components") {
		t.Fatalf("checkImageSize = %v, want the five planes refused", err)
	}
	c.Components = c.Components[:4]
	if err := checkImageSize(c); err != nil {
		t.Fatalf("four planes of 2^26 samples are 2^28: %v", err)
	}
}
