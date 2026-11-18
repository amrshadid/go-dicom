package jpeg2000

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The decoder's outside check on the parts of Part 1 the corpus does not use.
//
// pydicom's corpus is six JPEG 2000 files, and every one of them is written
// with the defaults: LRCP or RLCP, one precinct per resolution, no code-block
// style options. A feature none of them uses is checked here instead, against
// the reference implementation: opj_compress writes a codestream with the
// feature turned on, opj_decompress decodes it, and this decoder must return
// the same samples. That is exactly for a reversible codestream, and within
// one for an irreversible one, which is the tolerance CONFORMANCE.md §8.1
// states for the 9/7 filter.
//
// It needs opj_compress and opj_decompress on PATH and is skipped without
// them. The committed fixtures in TestDecodeOpenJPEGFixtures guard the same
// paths where OpenJPEG is not installed.

// openJPEGEncoding is one set of opj_compress options.
type openJPEGEncoding struct {
	name string
	args []string
}

// openJPEGEncodings are the options the decoder is checked under. Each is
// tried on every source image, so each runs at 8 and 16 bits, signed and
// unsigned, gray and color.
var openJPEGEncodings = []openJPEGEncoding{
	// The position-driven progression orders (B.12.1.3 to B.12.1.5), with the
	// default precincts and with several of their own.
	//
	// opj_compress gives the sizes from the highest resolution down and halves
	// the last one for each resolution below, and it does not stop at the
	// limit: "-c [16,16]" over six resolutions writes a precinct of one sample
	// at resolution 1, which A.6.1 forbids and opj_decompress itself refuses
	// ("Invalid precinct size"). The sizes below are chosen to stay legal.
	{"RPCL", []string{"-p", "RPCL"}},
	{"PCRL", []string{"-p", "PCRL"}},
	{"CPRL", []string{"-p", "CPRL"}},
	{"LRCP precincts", []string{"-c", "[32,32],[16,16]"}},
	{"RLCP precincts", []string{"-p", "RLCP", "-c", "[64,64],[32,32],[16,16]"}},
	{"RPCL precincts", []string{"-p", "RPCL", "-c", "[32,32],[16,16]"}},
	{"PCRL precincts", []string{"-p", "PCRL", "-c", "[32,32]"}},
	{"CPRL precincts", []string{"-p", "CPRL", "-c", "[64,32],[32,16],[8,8]"}},
	// Precincts smaller than the code-blocks, which shrink the blocks (B.7),
	// down to the two-sample precinct that makes them one sample wide.
	{"precincts below code-blocks", []string{"-p", "RPCL", "-n", "5", "-b", "32,32", "-c", "[16,16],[8,8],[4,4],[2,2]"}},
	// Tiles whose edges do not fall on precinct edges, and an image whose
	// origin is not the grid's, so the first precinct of a row straddles the
	// tile and the position loops start mid-precinct.
	{"RPCL tiles", []string{"-p", "RPCL", "-n", "4", "-t", "40,24", "-c", "[32,32]"}},
	{"PCRL tiles and origin", []string{"-p", "PCRL", "-t", "48,48", "-d", "5,3", "-c", "[32,32],[16,16]"}},
	{"CPRL origin", []string{"-p", "CPRL", "-n", "4", "-d", "17,9", "-c", "[16,16]"}},
	// One decomposition level and six (opj_compress counts resolutions),
	// layers, and the markers that can surround a packet.
	{"RPCL one level", []string{"-p", "RPCL", "-n", "2", "-c", "[8,8]"}},
	{"PCRL six levels", []string{"-p", "PCRL", "-n", "7", "-c", "[64,64]"}},
	{"RPCL layers", []string{"-p", "RPCL", "-r", "40,20,10,1", "-c", "[32,32],[16,16]"}},
	{"CPRL SOP EPH", []string{"-p", "CPRL", "-SOP", "-EPH", "-c", "[32,32]"}},
	{"RPCL tile-parts", []string{"-p", "RPCL", "-t", "32,32", "-TP", "R", "-c", "[32,32]"}},
	// And the irreversible path, where the tolerance applies.
	{"RPCL irreversible", []string{"-p", "RPCL", "-I", "-c", "[32,32],[16,16]"}},
	{"PCRL irreversible layers", []string{"-p", "PCRL", "-I", "-r", "20,5", "-c", "[32,32]"}},

	// The code-block style options (Table A.19, D.5 to D.7), which -M takes
	// as the sum of their bits: each alone, the two that split a block into
	// several codeword segments together, and all six.
	{"bypass", []string{"-M", "1"}},
	{"reset", []string{"-M", "2"}},
	{"termination on each pass", []string{"-M", "4"}},
	{"vertically causal", []string{"-M", "8"}},
	{"predictable termination", []string{"-M", "16"}},
	{"segmentation symbols", []string{"-M", "32"}},
	{"bypass and termination", []string{"-M", "5"}},
	{"bypass, reset, vertically causal", []string{"-M", "11"}},
	{"termination, segmentation symbols", []string{"-M", "36"}},
	{"every style", []string{"-M", "63"}},
	// Codeword segments that run across layers, where a block's contribution
	// to one packet ends mid-segment or spans several and its header gives a
	// length for each (B.10.7.2).
	{"bypass in layers", []string{"-M", "1", "-r", "30,10,3,1"}},
	{"termination in layers", []string{"-M", "4", "-r", "20,5,1"}},
	{"every style in layers", []string{"-M", "63", "-r", "16,4,1"}},
	// With the rest of the decoder: small code-blocks, tiles, precincts, a
	// position-driven order, and the irreversible filter.
	{"bypass, small blocks", []string{"-M", "1", "-b", "8,8"}},
	{"vertically causal, tall blocks", []string{"-M", "8", "-b", "16,64"}},
	{"every style, RPCL precincts tiles", []string{"-M", "63", "-p", "RPCL", "-c", "[32,32]", "-t", "48,40", "-n", "4"}},
	{"bypass irreversible", []string{"-M", "1", "-I", "-r", "8"}},
	{"every style irreversible", []string{"-M", "63", "-I"}},
}

// openJPEGSource is one image the options are tried on.
type openJPEGSource struct {
	name string
	img  *Image
}

// openJPEGSources are small enough that the matrix runs in seconds, and odd
// sized so no tile, precinct or code-block divides them evenly. Their content
// is smooth with noise on top, which gives every band something to code.
func openJPEGSources() []openJPEGSource {
	random := rand.New(rand.NewSource(800))
	content := func(depth int, signed bool) func(c, x, y int) int32 {
		lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
		span := float64(hi) - float64(lo)
		return func(c, x, y int) int32 {
			smooth := (math.Sin(float64(x)/7+float64(c))*math.Cos(float64(y)/11) + 1) / 2 * span
			v := float64(lo) + smooth + float64(random.Intn(int(span)/16+2))
			return int32(math.Min(math.Max(v, float64(lo)), float64(hi)))
		}
	}
	return []openJPEGSource{
		// At least 64 on a side, which six decomposition levels need.
		{"8-bit gray", testImage(97, 67, 1, 8, false, content(8, false))},
		{"16-bit gray", testImage(70, 65, 1, 16, false, content(16, false))},
		{"16-bit signed gray", testImage(65, 77, 1, 16, true, content(16, true))},
		{"8-bit color", testImage(64, 71, 3, 8, false, content(8, false))},
		{"12-bit signed color", testImage(66, 64, 3, 12, true, content(12, true))},
	}
}

// writeRaw writes an image as OpenJPEG's raw format: one component after
// another, big endian, in two bytes above 8 bits.
func writeRaw(t *testing.T, path string, img *Image) {
	t.Helper()
	var out bytes.Buffer
	for c, plane := range img.Components {
		for _, v := range plane {
			if img.Depths[c] > 8 {
				_ = binary.Write(&out, binary.BigEndian, uint16(v))
			} else {
				out.WriteByte(byte(v))
			}
		}
	}
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// encodeWithOpenJPEG has opj_compress write an image with the given options.
func encodeWithOpenJPEG(t *testing.T, dir string, img *Image, args []string) []byte {
	t.Helper()
	raw := filepath.Join(dir, "source.raw")
	writeRaw(t, raw, img)
	sign := "u"
	if img.Signed[0] {
		sign = "s"
	}
	out := filepath.Join(dir, "source.j2k")
	format := fmt.Sprintf("%d,%d,%d,%d,%s", img.Width, img.Height, len(img.Components), img.Depths[0], sign)
	command := append([]string{"-i", raw, "-F", format, "-o", out}, args...)
	if message, err := exec.Command("opj_compress", command...).CombinedOutput(); err != nil {
		t.Fatalf("opj_compress %s: %v\n%s", strings.Join(args, " "), err, message)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// requireOpenJPEG skips a test without OpenJPEG's two tools, and logs which
// version it is about to compare against.
func requireOpenJPEG(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"opj_compress", "opj_decompress"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
	help, _ := exec.Command("opj_decompress", "-h").CombinedOutput()
	if version := regexp.MustCompile(`openjp2 library v[0-9]+(\.[0-9]+)*`).Find(help); version != nil {
		t.Logf("comparing with OpenJPEG %s", version)
	}
}

// compareWithOpenJPEG decodes a codestream with this package and with
// opj_decompress and requires the samples to agree: exactly when the
// codestream is reversible, and within the tolerance CONFORMANCE.md §8.1
// states when it is not. It returns how many samples differed at all.
//
// The tolerance is one up to 12 bits and two above. OpenJPEG's 9/7 filter
// loses more than half a step at a 16-bit range, and this decoder is the
// nearer of the two to the samples that were encoded there (#360), so a
// difference of two at 16 bits is OpenJPEG's and not a fault to chase here.
func compareWithOpenJPEG(t *testing.T, label string, data []byte) int {
	t.Helper()
	codestream, err := ParseCodestream(data)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	img, err := Decode(codestream)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	lossy := false
	for tile := 0; tile < codestream.NumTiles(); tile++ {
		for comp := range codestream.Components {
			if codestream.CodingForTile(tile, comp).Wavelet == Wavelet97Irreversible {
				lossy = true
			}
		}
	}

	planes := decodeWithOpenJPEG(t, t.TempDir(), data, len(img.Components))
	off := 0
	for c, plane := range img.Components {
		if len(planes[c]) != len(plane) {
			t.Fatalf("%s: component %d has %d samples, OpenJPEG's %d", label, c, len(plane), len(planes[c]))
		}
		tolerance := int64(0)
		if lossy {
			tolerance = 1
			if img.Depths[c] > 12 {
				tolerance = 2
			}
		}
		for i, v := range plane {
			diff := int64(v) - int64(planes[c][i])
			switch {
			case diff == 0:
			case diff <= tolerance && diff >= -tolerance:
				off++
			default:
				t.Fatalf("%s: component %d sample %d (%d,%d) is %d, OpenJPEG's %d",
					label, c, i, i%img.Width, i/img.Width, v, planes[c][i])
			}
		}
	}
	return off
}

// TestDecodeMatchesOpenJPEG is the matrix: every option in
// openJPEGEncodings, on every source image, decoded by this package and by
// OpenJPEG to the same samples.
func TestDecodeMatchesOpenJPEG(t *testing.T) {
	requireOpenJPEG(t)
	checked := 0
	for _, encoding := range openJPEGEncodings {
		for _, source := range openJPEGSources() {
			label := encoding.name + ", " + source.name
			data := encodeWithOpenJPEG(t, t.TempDir(), source.img, encoding.args)
			if off := compareWithOpenJPEG(t, label, data); off > 0 {
				t.Logf("%s: %d of %d samples differ, all within the tolerance", label, off,
					source.img.Width*source.img.Height*len(source.img.Components))
			}
			checked++
		}
	}
	t.Logf("OpenJPEG and this decoder agree on %d codestreams", checked)
}

// TestDecodeEveryBlockStyleMatchesOpenJPEG tries all 63 combinations of the
// six code-block style options, in three layers so that codeword segments
// cross packets, on a gray and a signed 16-bit image. The matrix above takes
// a representative few through the rest of the decoder; this takes every
// combination through tier-1, where the options interact.
func TestDecodeEveryBlockStyleMatchesOpenJPEG(t *testing.T) {
	requireOpenJPEG(t)
	sources := openJPEGSources()
	checked := 0
	for style := 1; style < 64; style++ {
		for _, source := range []openJPEGSource{sources[0], sources[2]} {
			label := fmt.Sprintf("code-block style 0x%02X, %s", style, source.name)
			args := []string{"-M", fmt.Sprint(style), "-r", "20,5,1"}
			compareWithOpenJPEG(t, label, encodeWithOpenJPEG(t, t.TempDir(), source.img, args))
			checked++
		}
	}
	t.Logf("OpenJPEG and this decoder agree on all %d combinations", checked)
}

// openJPEGFixture is a committed codestream written by opj_compress, with the
// digest of the samples opj_decompress returns for it.
type openJPEGFixture struct {
	file    string
	options string // what opj_compress was given, for regenerating it
	digest  string // sha256 of every sample as int32 little endian, first 16 hex digits
}

// openJPEGFixtures guard the matrix's paths where OpenJPEG is not installed.
// Each is a few hundred bytes. The digests are OpenJPEG's, not this
// decoder's: regenerate a fixture with the options given and take the digest
// of opj_decompress's output, never of Decode's.
var openJPEGFixtures = []openJPEGFixture{
	// RPCL with two sizes of precinct and the default below them.
	{"opj_rpcl_precincts.j2k", "-F 33,29,1,8,u -p RPCL -n 3 -c [16,16],[8,8]", "105f78642e47a847"},
	// PCRL across tiles that do not start at the origin, signed color.
	{"opj_pcrl_tiles_color.j2k", "-F 24,20,3,12,s -p PCRL -n 3 -t 16,12 -d 3,1 -c [8,8]", "f3da988a6e6f620e"},
	// CPRL with two-sample precincts, which make the blocks one sample wide,
	// in two layers, at 16 bits.
	{"opj_cprl_small_precincts.j2k", "-F 31,17,1,16,u -p CPRL -n 2 -b 4,4 -c [4,4],[2,2] -r 3,1", "5e86b086f9f904a2"},
	// RPCL with the 9/7 filter: this one happens to round alike in both.
	{"opj_rpcl_irreversible.j2k", "-F 27,23,1,8,u -p RPCL -I -n 3 -c [16,16]", "ccb4bc6b3a970e74"},
	// Bypass with its raw segments spread over three layers.
	{"opj_style_bypass_layers.j2k", "-F 29,23,1,8,u -n 4 -M 1 -r 20,5,1", "0aa82ec44889de16"},
	// Every code-block style option at once, signed color, two layers.
	{"opj_style_every_color.j2k", "-F 26,19,3,12,s -n 4 -M 63 -r 10,1", "4291e0ecdf4cdeef"},
	// Termination on each pass with the vertically causal context, 16 bits.
	{"opj_style_termall_vertical.j2k", "-F 33,31,1,16,s -n 4 -M 12 -b 16,16", "db06cf718d4add6e"},
	// Reset and segmentation symbols under the 9/7 filter.
	{"opj_style_reset_segmentation_irreversible.j2k", "-F 25,21,1,8,u -n 4 -M 34 -I", "c8ea2982ab06cbeb"},
}

// samplesDigest is the digest openJPEGFixtures records.
func samplesDigest(planes [][]int32) string {
	h := sha256.New()
	var raw [4]byte
	for _, plane := range planes {
		for _, v := range plane {
			binary.LittleEndian.PutUint32(raw[:], uint32(v))
			h.Write(raw[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// TestDecodeOpenJPEGFixtures decodes each committed fixture and requires
// OpenJPEG's samples, by digest. Where OpenJPEG is installed it also checks
// that the recorded digest is still what opj_decompress returns, so a stale
// table cannot pass by agreeing with a wrong decoder.
func TestDecodeOpenJPEGFixtures(t *testing.T) {
	_, haveOpenJPEG := exec.LookPath("opj_decompress")
	for _, fixture := range openJPEGFixtures {
		t.Run(fixture.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", fixture.file))
			if err != nil {
				t.Fatal(err)
			}
			codestream, err := ParseCodestream(data)
			if err != nil {
				t.Fatalf("ParseCodestream: %v", err)
			}
			img, err := Decode(codestream)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := samplesDigest(img.Components); got != fixture.digest {
				t.Errorf("the samples hash to %s, OpenJPEG's to %s", got, fixture.digest)
			}
			if haveOpenJPEG == nil {
				planes := decodeWithOpenJPEG(t, t.TempDir(), data, len(img.Components))
				if got := samplesDigest(planes); got != fixture.digest {
					t.Errorf("opj_decompress now hashes to %s, the table says %s", got, fixture.digest)
				}
			}
		})
	}
}
