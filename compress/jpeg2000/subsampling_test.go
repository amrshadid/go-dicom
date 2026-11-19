package jpeg2000

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// subsampledFixtures are codestreams opj_compress wrote from raw planar
// images with subsampled components (-F w,h,n,depth,sign@XRsizxYRsiz:...),
// and the digest of the samples opj_decompress 2.5.4 returns for each
// component decoded on its own (-c), at its own size. Decoding the
// components together, opj_decompress takes three subsampled ones for sYCC
// and converts them to RGB, which is not the codestream's samples.
var subsampledFixtures = []struct {
	file    string
	options string
	widths  []int
	heights []int
	digest  string
}{
	{"opj_sub_gray_rpcl_precincts.j2k", "-F 57,63,1,16,u@2x2 -c [32,32],[16,16],[8,8] -b 8,8 -p RPCL -n 3",
		[]int{29}, []int{32}, "1c19c35b6975235f"},
	{"opj_sub_mixed_origin_tiles.j2k", "-F 66,40,3,8,u@1x1:3x2:1x4 -d 5,3 -t 24,20 -T 2,1 -n 3",
		[]int{66, 22, 66}, []int{40, 20, 10}, "610cbbd4926559a0"},
	{"opj_sub_420_pcrl_layers.j2k", "-F 64,48,3,8,u@1x1:2x2:2x2 -p PCRL -r 20,5,1",
		[]int{64, 32, 32}, []int{48, 24, 24}, "63561993e8814c0f"},
}

// TestDecodeSubsampledFixtures decodes each subsampled fixture and requires
// every plane at its component's size (B.2) and the samples OpenJPEG
// returns; where opj_decompress is on PATH, component by component, and
// otherwise by the digest recorded from it.
func TestDecodeSubsampledFixtures(t *testing.T) {
	_, err := exec.LookPath("opj_decompress")
	haveOpenJPEG := err == nil
	for _, f := range subsampledFixtures {
		data, err := os.ReadFile(filepath.Join("testdata", f.file))
		if err != nil {
			t.Fatal(err)
		}
		c, err := ParseCodestream(data)
		if err != nil {
			t.Fatalf("%s: %v", f.file, err)
		}
		img, err := Decode(c)
		if err != nil {
			t.Fatalf("%s: %v", f.file, err)
		}
		if !img.Subsampled() {
			t.Errorf("%s: not reported as subsampled", f.file)
		}
		for i := range img.Components {
			w, h := img.ComponentSize(i)
			if w != f.widths[i] || h != f.heights[i] || len(img.Components[i]) != w*h {
				t.Errorf("%s: component %d is %dx%d with %d samples, want %dx%d",
					f.file, i, w, h, len(img.Components[i]), f.widths[i], f.heights[i])
			}
		}
		got := samplesDigest(img.Components)
		if got != f.digest {
			t.Errorf("%s: samples digest %s, OpenJPEG's %s", f.file, got, f.digest)
		}
		if !haveOpenJPEG {
			continue
		}
		theirs := decodeComponentsWithOpenJPEG(t, t.TempDir(), data, len(img.Components))
		for i, plane := range img.Components {
			if len(theirs[i]) != len(plane) {
				t.Fatalf("%s: component %d has %d samples, OpenJPEG's %d", f.file, i, len(plane), len(theirs[i]))
			}
			for k, v := range plane {
				if v != theirs[i][k] {
					t.Fatalf("%s: component %d sample %d is %d, OpenJPEG's %d", f.file, i, k, v, theirs[i][k])
				}
			}
		}
	}
}

// decodeComponentsWithOpenJPEG has opj_decompress decode one component at a
// time (-c), which leaves each at its own size.
func decodeComponentsWithOpenJPEG(t *testing.T, dir string, data []byte, components int) [][]int32 {
	t.Helper()
	in := filepath.Join(dir, "in.j2k")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	planes := make([][]int32, components)
	for c := range planes {
		out := filepath.Join(dir, fmt.Sprintf("c%d.pgx", c))
		if message, err := exec.Command("opj_decompress", "-i", in, "-o", out, "-c", fmt.Sprint(c)).CombinedOutput(); err != nil {
			t.Fatalf("opj_decompress -c %d: %v\n%s", c, err, message)
		}
		name := filepath.Join(dir, fmt.Sprintf("c%d_0.pgx", c))
		if _, err := os.Stat(name); err != nil {
			name = out
		}
		planes[c] = readPGX(t, name)
	}
	return planes
}

// subsampledSource is a raw planar image whose components have their own
// separations, as opj_compress reads it with -F ...@dxxdy:...
type subsampledSource struct {
	name        string
	w, h, depth int
	signed      bool
	separations [][2]int
}

// raw is the image's samples, each component at its own size, big endian
// above 8 bits: smooth with noise, so every band has something to code.
func (s subsampledSource) raw() []byte {
	random := rand.New(rand.NewSource(int64(s.w*s.h + s.depth)))
	var out []byte
	for c, sep := range s.separations {
		cw, ch := ceilDivInt(s.w, sep[0]), ceilDivInt(s.h, sep[1])
		lo, hi := sampleRange(Component{Signed: s.signed, Depth: s.depth})
		span := float64(hi) - float64(lo)
		for y := 0; y < ch; y++ {
			for x := 0; x < cw; x++ {
				v := float64(lo) + (math.Sin(float64(x)/5+float64(c))*math.Cos(float64(y)/7)+1)/2*span +
					float64(random.Intn(int(span)/32+2))
				sample := int32(math.Min(math.Max(v, float64(lo)), float64(hi)))
				if s.depth > 8 {
					out = append(out, byte(uint16(sample)>>8), byte(sample))
				} else {
					out = append(out, byte(sample))
				}
			}
		}
	}
	return out
}

// format is the -F argument opj_compress reads the raw image with.
func (s subsampledSource) format() string {
	sign := "u"
	if s.signed {
		sign = "s"
	}
	f := fmt.Sprintf("%d,%d,%d,%d,%s@", s.w, s.h, len(s.separations), s.depth, sign)
	for i, sep := range s.separations {
		if i > 0 {
			f += ":"
		}
		f += fmt.Sprintf("%dx%d", sep[0], sep[1])
	}
	return f
}

// TestSubsampledMatchesOpenJPEG is the subsampling matrix: six images (4:2:0,
// 4:2:2 at odd sizes, signed 12-bit 4:2:0, separations of 3x2 and 1x4, a
// subsampled gray image, four components with 4x4 chroma) under nine
// option sets (the defaults, 9/7, tiles, tiles off the origin, precincts
// with RPCL, PCRL and CPRL in layers, every code-block style), written by
// opj_compress and decoded here and, component by component, by
// opj_decompress: exactly with the 5/3 filter, within the 9/7 tolerance with
// the other. 9/7 tiles whose edge leaves a lone sample on an odd coordinate
// are left out: OpenJPEG writes and reads that sample against T.800 (#549).
func TestSubsampledMatchesOpenJPEG(t *testing.T) {
	requireOpenJPEG(t)
	sources := []subsampledSource{
		{"4:2:0", 64, 48, 8, false, [][2]int{{1, 1}, {2, 2}, {2, 2}}},
		{"4:2:2 odd", 65, 49, 8, false, [][2]int{{1, 1}, {2, 1}, {2, 1}}},
		{"4:2:0 signed 12-bit", 70, 61, 12, true, [][2]int{{1, 1}, {2, 2}, {2, 2}}},
		{"3x2 and 1x4", 66, 40, 8, false, [][2]int{{1, 1}, {3, 2}, {1, 4}}},
		{"gray 2x2", 57, 63, 16, false, [][2]int{{2, 2}}},
		{"four components", 80, 64, 8, false, [][2]int{{1, 1}, {4, 4}, {4, 4}, {1, 1}}},
	}
	options := []struct {
		name string
		args []string
	}{
		{"defaults", nil},
		{"irreversible", []string{"-I"}},
		{"tiles", []string{"-t", "32,24", "-n", "3"}},
		{"tiles off the origin", []string{"-d", "5,3", "-t", "24,20", "-T", "2,1", "-n", "3"}},
		{"RPCL precincts", []string{"-c", "[32,32],[16,16],[8,8]", "-b", "8,8", "-p", "RPCL", "-n", "3"}},
		{"PCRL layers", []string{"-p", "PCRL", "-r", "20,5,1"}},
		{"CPRL layers", []string{"-p", "CPRL", "-r", "30,10,1", "-n", "3"}},
		{"every style", []string{"-M", "63", "-n", "4"}},
		{"irreversible tiles", []string{"-I", "-t", "32,32", "-n", "3"}},
	}
	for _, s := range sources {
		for _, o := range options {
			label := s.name + ", " + o.name
			dir := t.TempDir()
			raw := filepath.Join(dir, "source.raw")
			if err := os.WriteFile(raw, s.raw(), 0o600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "source.j2k")
			args := append([]string{"-i", raw, "-F", s.format(), "-o", out}, o.args...)
			if message, err := exec.Command("opj_compress", args...).CombinedOutput(); err != nil {
				t.Fatalf("%s: opj_compress: %v\n%s", label, err, message)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			c, err := ParseCodestream(data)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			img, err := Decode(c)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			tolerance := int32(0)
			if c.Coding.Wavelet == Wavelet97Irreversible {
				tolerance = 1
				if s.depth > 12 {
					tolerance = 2
				}
			}
			theirs := decodeComponentsWithOpenJPEG(t, dir, data, len(img.Components))
			for i, plane := range img.Components {
				if len(plane) != len(theirs[i]) {
					t.Fatalf("%s: component %d has %d samples, OpenJPEG's %d", label, i, len(plane), len(theirs[i]))
				}
				for k, v := range plane {
					if d := v - theirs[i][k]; d > tolerance || d < -tolerance {
						t.Fatalf("%s: component %d sample %d is %d, OpenJPEG's %d", label, i, k, v, theirs[i][k])
					}
				}
			}
		}
	}
}

// TestSubsamplingSeparationOfZeroIsRefused proves an XRsiz or YRsiz of 0,
// which A.5.1 does not allow and which would divide the component's
// coordinates by zero, is a parse error.
func TestSubsamplingSeparationOfZeroIsRefused(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", subsampledFixtures[0].file))
	if err != nil {
		t.Fatal(err)
	}
	// SOC, SIZ marker and length, then 36 bytes before the first
	// component's Ssiz, XRsiz, YRsiz.
	for _, at := range []int{2 + 4 + 36 + 1, 2 + 4 + 36 + 2} {
		bad := append([]byte(nil), data...)
		bad[at] = 0
		if _, err := ParseCodestream(bad); err == nil {
			t.Errorf("a separation of 0 at byte %d parsed", at)
		}
	}
}
