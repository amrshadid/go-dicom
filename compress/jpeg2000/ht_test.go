package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCxtVLCTablesAreCompletePrefixCodes checks the Annex C tables against
// what T.814 says of them, which is what a transcription error would break.
// Each context's codewords are a prefix code (7.3.5 reads bits until exactly
// one matches) and a complete one, so that every slot of the lookup table is
// filled; every entry's patterns agree with each other (7.3.2: an EMB bit is
// known only for a significant sample, a known-1 bit only where it is known,
// and nothing is known without a residual); and every quad an encoder can
// meet has a codeword (F.3), the all-zero-context quads apart from the
// insignificant one, which the MEL stream codes alone.
func TestCxtVLCTablesAreCompletePrefixCodes(t *testing.T) {
	for table, entries := range [2][]cxtVLCEntry{cxtVLCTable0[:], cxtVLCTable1[:]} {
		for context := uint8(0); context < 8; context++ {
			var mine []cxtVLCEntry
			for _, e := range entries {
				if e.context == context {
					mine = append(mine, e)
				}
			}
			kraft := 0.0
			for i, a := range mine {
				if a.length < 1 || a.length > 7 || a.codeword >= 1<<a.length {
					t.Errorf("table %d context %d: codeword %#x of %d bits", table, context, a.codeword, a.length)
				}
				kraft += math.Ldexp(1, -int(a.length))
				for j, b := range mine {
					if i != j && a.length <= b.length && b.codeword&(1<<a.length-1) == a.codeword {
						t.Errorf("table %d context %d: codeword %#x/%d is a prefix of %#x/%d",
							table, context, a.codeword, a.length, b.codeword, b.length)
					}
				}
				if a.ek&^a.rho != 0 || a.e1&^a.ek != 0 || (a.uOff == 0 && a.ek != 0) || a.uOff > 1 {
					t.Errorf("table %d context %d: rho %X uOff %d ek %X e1 %X are inconsistent",
						table, context, a.rho, a.uOff, a.ek, a.e1)
				}
				if context == 0 && a.rho == 0 {
					t.Errorf("table %d: an insignificant quad in the all-zero context has a codeword", table)
				}
			}
			if kraft != 1 {
				t.Errorf("table %d context %d: the codeword lengths sum to %v of the code space, not all of it",
					table, context, kraft)
			}
			for next := 0; next < 128; next++ {
				if cxtVLCLookup[table][context][next].length == 0 {
					t.Errorf("table %d context %d: no codeword begins with the bits %07b", table, context, next)
				}
			}
			for rho := uint8(0); rho < 16; rho++ {
				if context == 0 && rho == 0 {
					continue
				}
				for emb := uint8(0); emb < 16; emb++ {
					if emb&^rho != 0 {
						continue
					}
					found := false
					for _, e := range mine {
						if e.rho == rho && (e.uOff == 1) == (emb != 0) && e.e1 == emb&e.ek {
							found = true
						}
					}
					if !found {
						t.Errorf("table %d context %d: no codeword for rho %X with EMB pattern %X", table, context, rho, emb)
					}
				}
			}
		}
	}
	if n0, n1 := len(cxtVLCTable0), len(cxtVLCTable1); n0 != 444 || n1 != 358 {
		t.Errorf("the tables hold %d and %d entries, the standard's text %d and %d", n0, n1, 444, 358)
	}
}

// TestMELDecoderReadsWhatAnnexFWrites runs random MEL symbol sequences,
// sparse and dense, through the encoding procedure of T.814 F.3 and back
// through decodeMELSym (7.3.3), across bytes of 0xFF and the stuffed bit
// after them.
func TestMELDecoderReadsWhatAnnexFWrites(t *testing.T) {
	random := rand.New(rand.NewSource(15))
	for trial := 0; trial < 200; trial++ {
		p := random.Float64()
		symbols := make([]int, random.Intn(3000)+1)
		for i := range symbols {
			if random.Float64() < p {
				symbols[i] = 1
			}
		}
		enc := newMELPacker()
		for _, s := range symbols {
			enc.encode(s)
		}
		if enc.run > 0 {
			enc.emitBit(1)
		}
		if enc.rem != 8 {
			enc.buf = append(enc.buf, enc.tmp<<uint(enc.rem))
		}
		dec := melDecoder{data: enc.buf}
		for i, want := range symbols {
			if got := dec.symbol(); got != want {
				t.Fatalf("trial %d: symbol %d is %d, want %d", trial, i, got, want)
			}
		}
	}
}

// TestHTBitReadersSkipStuffedBits checks the forward reader of the MagSgn and
// SigProp streams (7.1.2, 7.1.5) on a byte after 0xFF, which gives seven bits,
// refuses one whose stuffed bit is set, and past the end of the MagSgn stream
// synthesizes one 0xFF and then stops.
func TestHTBitReadersSkipStuffedBits(t *testing.T) {
	r := forwardReader{data: []byte{0xFF, 0x7F, 0x01}, pad: 0xFF}
	for _, want := range []struct {
		n int
		v uint64
	}{{8, 0xFF}, {7, 0x7F}, {8, 0x01}, {8, 0xFF}} {
		got, err := r.bits(want.n)
		if err != nil || got != want.v {
			t.Fatalf("read %d bits: %#x, %v; want %#x", want.n, got, err, want.v)
		}
	}
	if _, err := r.bits(1); err == nil {
		t.Error("the MagSgn stream gave a bit past its synthesized byte")
	}

	bad := forwardReader{data: []byte{0xFF, 0x80}, pad: 0xFF}
	if _, err := bad.bits(16); err == nil {
		t.Error("a byte after 0xFF with its stuffed bit set was read")
	}

	sp := forwardReader{data: []byte{0x05}}
	if v, err := sp.bits(24); err != nil || v != 0x05 {
		t.Errorf("the SigProp stream past its end: %#x, %v; want zeros after its byte", v, err)
	}

	// The VLC stream starts in the top four bits of the second last byte, the
	// rest of which held Scup, and reads the bytes before it backwards.
	seg := []byte{0xAB, 0x5F, 0xFF}
	v := newVLCReader(seg, 0)
	got, err := v.bits(4)
	if err != nil || got != 0x5 {
		t.Fatalf("the first VLC bits are %#x, %v; want 0x5", got, err)
	}
	if got, err = v.bits(8); err != nil || got != 0xAB {
		t.Fatalf("the next VLC byte is %#x, %v; want 0xAB", got, err)
	}
	if _, err := v.bits(1); err == nil {
		t.Error("the VLC stream read past the start of its bytes")
	}
}

// TestHTSegmentsOutsideWhatIsDecodedAreRefused requires a code-block of more
// than three passes, placeholder passes or a second HT set, to be refused by
// name, and the segment length limits of 7.1.1 to hold.
func TestHTSegmentsOutsideWhatIsDecodedAreRefused(t *testing.T) {
	block := CodeBlockData{
		Block: CodeBlock{X1: 4, Y1: 4}, Passes: 4, Style: BlockStyleHT,
		Data:     []byte{1, 2, 3, 4, 5, 6},
		Segments: []CodewordSegment{{Passes: 1, Length: 2}, {Passes: 2, Length: 2}, {Passes: 1, Length: 2}},
	}
	_, err := DecodeBlock(block, 8)
	var unsupported *ErrUnsupported
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "placeholder passes or more than one HT set") {
		t.Errorf("four passes: %v", err)
	}

	block.Passes, block.Segments, block.Data = 1, []CodewordSegment{{Passes: 1, Length: 1}}, []byte{0x10}
	if _, err := DecodeBlock(block, 8); err == nil {
		t.Error("a one-byte HT cleanup segment decoded")
	}
	block.Passes = 2
	block.Data = make([]byte, 2+2047)
	block.Segments = []CodewordSegment{{Passes: 1, Length: 2}, {Passes: 1, Length: 2047}}
	if _, err := DecodeBlock(block, 8); err == nil {
		t.Error("a 2047-byte HT refinement segment decoded")
	}
}

// htFixtureImage is the source of an HT fixture: smooth with noise, like
// openJPEGSources, from a fixed seed.
func htFixtureImage(w, h, components, depth int, signed bool, seed int64) *Image {
	random := rand.New(rand.NewSource(seed))
	lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
	span := float64(hi) - float64(lo)
	return testImage(w, h, components, depth, signed, func(c, x, y int) int32 {
		smooth := (math.Sin(float64(x)/5+float64(c))*math.Cos(float64(y)/7) + 1) / 2 * span
		v := float64(lo) + smooth + float64(random.Intn(int(span)/16+2))
		return int32(math.Min(math.Max(v, float64(lo)), float64(hi)))
	})
}

// htFixture is a committed HT codestream, how it was made, and the digest of
// the samples opj_decompress returns for it. Each was transcoded by
// htTranscode from what opj_compress wrote for htFixtureImage.
type htFixture struct {
	file       string
	image      [5]int // width, height, components, depth, signed; zero for the matrix's first source
	opjOptions []string
	options    htTranscodeOptions
	digest     string
}

// htFixtures guard the HT matrix's paths where OpenJPEG is not installed. The
// digests are OpenJPEG's. The fixture with its passes in layers is one
// OpenJPEG cannot decode (see TestHTRefinementInLaterLayers); its digest is
// OpenJPEG's for the same code-blocks in one layer, which is what it must
// decode to.
var htFixtures = []htFixture{
	// The cleanup pass alone, RPCL with two precinct sizes.
	{"ht_cleanup_rpcl.j2k", [5]int{33, 29, 1, 8, 0}, []string{"-p", "RPCL", "-n", "3", "-c", "[16,16],[8,8]"},
		htTranscodeOptions{}, "56d11cc874fc89df"},
	// SigProp and MagRef, signed color across tiles off the origin, PCRL.
	{"ht_refinement_tiles_color.j2k", [5]int{24, 20, 3, 12, 1}, []string{"-p", "PCRL", "-n", "3", "-t", "16,12", "-d", "3,1", "-c", "[8,8]"},
		htTranscodeOptions{refinement: htSigPropMagRef}, "d7eceab05e5dd1bf"},
	// SigProp alone, vertically causal, 16 bits, small code-blocks.
	{"ht_sigprop_vertical_16bit.j2k", [5]int{31, 17, 1, 16, 0}, []string{"-n", "2", "-b", "8,8"},
		htTranscodeOptions{refinement: htSigProp, vertical: true}, "838a7424f3d6a810"},
	// SigProp alone, vertically causal, where the sparse high bands give the
	// last row of a stripe neighbors below it that the style must ignore.
	{"ht_sigprop_vertical.j2k", [5]int{}, []string{"-p", "CPRL", "-n", "4", "-d", "17,9", "-c", "[16,16]"},
		htTranscodeOptions{refinement: htSigProp, vertical: true}, "d284a22dc1344f36"},
	// The 9/7 filter with both refinement passes.
	{"ht_irreversible.j2k", [5]int{27, 23, 1, 8, 0}, []string{"-I", "-p", "RPCL", "-n", "3", "-c", "[16,16]"},
		htTranscodeOptions{refinement: htSigPropMagRef, vertical: true}, "c7eb5339cc7c2f71"},
	// An HT component beside two Part 1 components (HTDECLARED).
	{"ht_beside_part1_color.j2k", [5]int{26, 19, 3, 8, 0}, []string{"-n", "3"},
		htTranscodeOptions{refinement: htSigPropMagRef, partOne: map[int]bool{1: true, 2: true}}, "92a942dd95450ea2"},
	// Each pass in a layer of its own.
	{"ht_refinement_layers.j2k", [5]int{29, 23, 1, 8, 0}, []string{"-n", "3", "-b", "16,16"},
		htTranscodeOptions{refinement: htSigPropMagRef, passLayers: true}, "21644c289da3a243"},
}

// TestDecodeHTFixtures decodes each committed HT fixture and requires
// OpenJPEG's samples, by digest. Where OpenJPEG is installed it also decodes
// the fixture (or, for the layered one, its one-layer twin) and requires the
// recorded digest, so a stale table cannot pass by agreeing with a wrong
// decoder.
func TestDecodeHTFixtures(t *testing.T) {
	_, noOpenJPEG := exec.LookPath("opj_decompress")
	for _, fixture := range htFixtures {
		t.Run(fixture.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", fixture.file))
			if err != nil {
				t.Fatal(err)
			}
			c, err := ParseCodestream(data)
			if err != nil {
				t.Fatalf("ParseCodestream: %v", err)
			}
			if !c.HasHTCapabilities {
				t.Fatal("the fixture declares no Part 15 capabilities")
			}
			img, err := Decode(c)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := samplesDigest(img.Components); got != fixture.digest {
				t.Errorf("the samples hash to %s, OpenJPEG's to %s", got, fixture.digest)
			}
			if noOpenJPEG != nil {
				return
			}
			reference := data
			if fixture.options.passLayers {
				reference = htFixtureTwin(t, fixture)
			}
			planes := decodeWithOpenJPEG(t, t.TempDir(), reference, len(img.Components))
			if got := samplesDigest(planes); got != fixture.digest {
				t.Errorf("opj_decompress now hashes to %s, the table says %s", got, fixture.digest)
			}
		})
	}
}

// source is the image a fixture was made from.
func (f htFixture) source() *Image {
	if f.image[0] == 0 {
		return openJPEGSources()[0].img
	}
	return htFixtureImage(f.image[0], f.image[1], f.image[2], f.image[3], f.image[4] == 1, 403)
}

// htFixtureTwin rebuilds a layered fixture's code-blocks in one layer, from
// its recipe, for OpenJPEG to decode.
func htFixtureTwin(t *testing.T, fixture htFixture) []byte {
	t.Helper()
	requireOpenJPEG(t)
	img := fixture.source()
	part1 := encodeWithOpenJPEG(t, t.TempDir(), img, fixture.opjOptions)
	options := fixture.options
	options.passLayers = false
	return htTranscode(t, part1, options)
}

// TestWriteHTFixtures rewrites the committed HT fixtures from their recipes
// and prints OpenJPEG's digests for the table. It runs only when
// GODICOM_WRITE_HT_FIXTURES is set.
func TestWriteHTFixtures(t *testing.T) {
	if os.Getenv("GODICOM_WRITE_HT_FIXTURES") == "" {
		t.Skip("set GODICOM_WRITE_HT_FIXTURES to rewrite the HT fixtures")
	}
	requireOpenJPEG(t)
	for _, fixture := range htFixtures {
		img := fixture.source()
		part1 := encodeWithOpenJPEG(t, t.TempDir(), img, fixture.opjOptions)
		data := htTranscode(t, part1, fixture.options)
		if err := os.WriteFile(filepath.Join("testdata", fixture.file), data, 0o644); err != nil {
			t.Fatal(err)
		}
		reference := data
		if fixture.options.passLayers {
			reference = htFixtureTwin(t, fixture)
		}
		planes := decodeWithOpenJPEG(t, t.TempDir(), reference, len(img.Components))
		t.Logf("%s: %d bytes, OpenJPEG digest %s", fixture.file, len(data), samplesDigest(planes))
	}
}

// TestHTRefinementInLaterLayers puts each HT pass in a layer of its own, so
// that the refinement segment is two codeword segments in two packets, which
// T.814 B.2 and B.3 join into one, and requires the same samples as the same
// code-blocks in one layer.
//
// OpenJPEG cannot be the reference here: its packet reader (2.5.4, t2.c)
// gives an HT block's first segment room for every pass, so passes that
// arrive in a later packet than the cleanup pass are added to the cleanup
// segment, and it then refuses the block ("2 <= Scup <= min(Lcup, 4079)").
// The one-layer twins are checked against OpenJPEG in the matrix and in
// TestDecodeHTFixtures; this checks that layers change nothing. The source is
// the package's own lossless encoder, so it runs without OpenJPEG.
func TestHTRefinementInLaterLayers(t *testing.T) {
	for _, source := range openJPEGSources() {
		part1, err := Encode(source.img)
		if err != nil {
			t.Fatal(err)
		}
		for _, refinement := range []htRefinement{htSigProp, htSigPropMagRef} {
			one := htTranscode(t, part1, htTranscodeOptions{refinement: refinement})
			layered := htTranscode(t, part1, htTranscodeOptions{refinement: refinement, passLayers: true})
			c, err := ParseCodestream(layered)
			if err != nil {
				t.Fatal(err)
			}
			if c.Coding.Layers != int(refinement)+1 {
				t.Fatalf("%s: %d layers", source.name, c.Coding.Layers)
			}
			want, got := decodeOrFail(t, one), decodeOrFail(t, layered)
			if samplesDigest(got.Components) != samplesDigest(want.Components) {
				t.Errorf("%s, %s: the layered codestream decodes differently from the one-layer one", source.name, refinement)
			}
			if refinement == htSigPropMagRef {
				sameImage(t, source.name+" in layers", got, source.img)
			}
		}
	}
}

func decodeOrFail(t *testing.T, data []byte) *Image {
	t.Helper()
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(c)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestHTIsLosslessWhereItCodesEveryPlane takes every source image through the
// package's lossless encoder and the test HT encoder, cleanup alone and with
// both refinement passes, with and without the vertically causal style, and
// requires the source back. It is the encoder and decoder of this package
// agreeing with each other, which OpenJPEG checks where it is installed; this
// runs everywhere.
func TestHTIsLosslessWhereItCodesEveryPlane(t *testing.T) {
	for _, source := range openJPEGSources() {
		part1, err := Encode(source.img)
		if err != nil {
			t.Fatal(err)
		}
		for _, options := range []htTranscodeOptions{{}, {refinement: htSigPropMagRef}, {refinement: htSigPropMagRef, vertical: true}} {
			sameImage(t, source.name+", "+options.refinement.String(), decodeOrFail(t, htTranscode(t, part1, options)), source.img)
		}
	}
}

// TestParseCodestreamHTCapabilities checks the CAP segment and the HT
// code-block styles: Part 15 is read, another part's capabilities are
// refused by name, HT blocks need the CAP segment, and the styles T.814
// leaves reserved, or that mix HT and Part 1 blocks in a tile-component, are
// refused by name.
func TestParseCodestreamHTCapabilities(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ht_cleanup_rpcl.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasHTCapabilities || c.Coding.BlockStyle != BlockStyleHT {
		t.Fatalf("Part 15 %v, style %#x", c.HasHTCapabilities, c.Coding.BlockStyle)
	}

	capAt := bytes.Index(data, []byte{0xFF, 0x50})
	codAt := bytes.Index(data, []byte{0xFF, 0x52})
	if capAt < 0 || codAt < 0 {
		t.Fatal("no CAP or COD segment")
	}
	patch := func(f func(d []byte) []byte) []byte { return f(append([]byte{}, data...)) }
	var unsupported *ErrUnsupported
	for _, test := range []struct {
		name        string
		data        []byte
		unsupported string
	}{
		{"Part 2 capabilities", patch(func(d []byte) []byte {
			binary.BigEndian.PutUint32(d[capAt+4:], 1<<(32-15)|1<<(32-2))
			// A second Ccap word, so only the part is wrong.
			return append(d[:capAt+10:capAt+10], append([]byte{0, 0}, d[capAt+10:]...)...)
		}), "Part 2"},
		{"mixed HT and Part 1 blocks", patch(func(d []byte) []byte { d[codAt+12] = 0xC0; return d }), "0xC0"},
		{"a reserved bit beside HT", patch(func(d []byte) []byte { d[codAt+12] = 0x41; return d }), "0x41"},
	} {
		if test.name == "Part 2 capabilities" {
			binary.BigEndian.PutUint16(test.data[capAt+2:], 10)
		}
		_, err := ParseCodestream(test.data)
		if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), test.unsupported) {
			t.Errorf("%s: %v", test.name, err)
		}
	}

	// HT blocks with no CAP segment: an error, not a decode.
	noCAP := append(append([]byte{}, data[:capAt]...), data[capAt+2+int(binary.BigEndian.Uint16(data[capAt+2:])):]...)
	if _, err := ParseCodestream(noCAP); err == nil || !strings.Contains(err.Error(), "CAP") {
		t.Errorf("HT blocks without CAP: %v", err)
	}
	// The vertically causal bit is HT's too.
	vertical := patch(func(d []byte) []byte { d[codAt+12] = 0x48; return d })
	if _, err := ParseCodestream(vertical); err != nil {
		t.Errorf("style 0x48: %v", err)
	}
	// A CAP whose Pcap promises more words than it holds.
	short := patch(func(d []byte) []byte { binary.BigEndian.PutUint32(d[capAt+4:], 3<<(32-15)); return d })
	if _, err := ParseCodestream(short); err == nil {
		t.Error("a CAP with too few Ccap words parsed")
	}
}

// TestHTWithARegionOfInterestIsRefused requires a region of interest on an HT
// component to be refused by name: T.814 allows it and nothing here can check
// a decode of it.
func TestHTWithARegionOfInterestIsRefused(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ht_cleanup_rpcl.j2k"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCodestream(data)
	if err != nil {
		t.Fatal(err)
	}
	c.ROIShift[0] = 4
	_, err = Decode(c)
	var unsupported *ErrUnsupported
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "region of interest") {
		t.Errorf("an HT component with a region of interest: %v", err)
	}
}
