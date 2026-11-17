package jpeg2000

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The round trip in encode_test.go is through this package's own decoder,
// which shares the encoder's reading of the standard. OpenJPEG does not. It
// is the reference implementation's decoder that has to return the samples,
// so this test hands it what Encode wrote and compares what comes back.
//
// It needs opj_decompress on PATH and is skipped without it.

// decodeWithOpenJPEG decodes a codestream with opj_decompress and returns
// each component's samples. The output is PGX, OpenJPEG's own raw format,
// one file for each component: any depth, either sign, nothing converted.
func decodeWithOpenJPEG(t *testing.T, dir string, data []byte, components int) [][]int32 {
	t.Helper()
	in := filepath.Join(dir, "encoded.j2k")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "decoded.pgx")
	if message, err := exec.Command("opj_decompress", "-i", in, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("opj_decompress refused what Encode wrote: %v\n%s", err, message)
	}

	planes := make([][]int32, components)
	for c := range planes {
		// One component is written under the name given, several as _0, _1...
		name := out
		if _, err := os.Stat(name); err != nil || components > 1 {
			name = filepath.Join(dir, fmt.Sprintf("decoded_%d.pgx", c))
		}
		planes[c] = readPGX(t, name)
	}
	return planes
}

// readPGX reads one PGX file: a text line "PG ML <sign> <depth> <width>
// <height>" and then the samples, big endian, in as many bytes as the depth
// needs.
func readPGX(t *testing.T, name string) []int32 {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(bytes.NewReader(raw))
	header, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("%s: no header line", name)
	}
	var order string
	var sign byte
	var depth, width, height int
	if _, err := fmt.Sscanf(header, "PG %s %c %d %d %d", &order, &sign, &depth, &width, &height); err != nil {
		t.Fatalf("%s: header %q: %v", name, header, err)
	}
	if order != "ML" {
		t.Fatalf("%s: byte order %s", name, order)
	}
	size := 1
	if depth > 8 {
		size = 2
	}
	samples := make([]int32, width*height)
	buf := make([]byte, size)
	for i := range samples {
		if _, err := reader.Read(buf[:1]); err != nil {
			t.Fatalf("%s: ends at sample %d of %d", name, i, len(samples))
		}
		v := int32(buf[0])
		if size == 2 {
			if _, err := reader.Read(buf[1:]); err != nil {
				t.Fatalf("%s: ends inside sample %d", name, i)
			}
			v = v<<8 | int32(buf[1])
		}
		if sign == '-' {
			// Sign extended from the width it is stored in.
			if size == 1 {
				v = int32(int8(v))
			} else {
				v = int32(int16(v))
			}
		}
		samples[i] = v
	}
	return samples
}

// TestOpenJPEGDecodesWhatEncodeWrites is the encoder's outside check: for
// the depths, signs, component counts and sizes of TestEncodeIsLossless,
// OpenJPEG returns every sample as it was encoded.
func TestOpenJPEGDecodesWhatEncodeWrites(t *testing.T) {
	if _, err := exec.LookPath("opj_decompress"); err != nil {
		t.Skip("opj_decompress is not on PATH")
	}
	random := rand.New(rand.NewSource(15444))
	sizes := [][2]int{{1, 1}, {3, 7}, {64, 64}, {65, 63}, {200, 130}, {513, 257}}
	checked := 0
	for _, depth := range []int{1, 8, 10, 12, 16} {
		for _, signed := range []bool{false, true} {
			lo, hi := sampleRange(Component{Signed: signed, Depth: depth})
			span := int64(hi) - int64(lo) + 1
			contents := map[string]func(c, x, y int) int32{
				"noise":    func(int, int, int) int32 { return lo + int32(random.Int63n(span)) },
				"gradient": func(c, x, y int) int32 { return lo + int32((int64(x)*31+int64(y)*17+int64(c)*5)%span) },
				"extremes": func(_, x, y int) int32 {
					if (x/3+y/5)%2 == 0 {
						return lo
					}
					return hi
				},
			}
			for name, value := range contents {
				for _, size := range sizes {
					for _, components := range []int{1, 3} {
						img := testImage(size[0], size[1], components, depth, signed, value)
						data, err := Encode(img)
						if err != nil {
							t.Fatalf("Encode: %v", err)
						}
						label := fmt.Sprintf("%s %dx%d, %d components of %d bits signed=%v",
							name, size[0], size[1], components, depth, signed)
						planes := decodeWithOpenJPEG(t, t.TempDir(), data, components)
						for c := range img.Components {
							if len(planes[c]) != len(img.Components[c]) {
								t.Fatalf("%s: OpenJPEG returned %d samples of %d", label, len(planes[c]), len(img.Components[c]))
							}
							for i, v := range img.Components[c] {
								if planes[c][i] != v {
									t.Fatalf("%s: component %d sample %d is %d from OpenJPEG, was %d",
										label, c, i, planes[c][i], v)
								}
							}
						}
						checked++
					}
				}
			}
		}
	}
	t.Logf("OpenJPEG returned the samples of %d images exactly", checked)
}
