package compress_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The color space of a three-component JPEG is not in the JPEG Interchange
// Format. Without a data set a decoder guesses it from a JFIF or Adobe marker
// or from component identifiers spelling R, G, B; with one, PS3.5 8.2.1 has
// the Photometric Interpretation say it, and dcmtk decodes that way by
// default. These tests cross every Photometric Interpretation a color JPEG can
// carry with every kind of evidence a stream can.

// colorEvidence rewrites a JPEG's color evidence: every APPn segment is
// dropped, app is put after SOI, and the component identifiers in SOF and SOS
// become ids. The entropy-coded data, and so the stored components, are
// unchanged.
func colorEvidence(t *testing.T, stream []byte, ids [3]byte, app []byte) []byte {
	t.Helper()
	out := append([]byte{0xFF, 0xD8}, app...)
	for i := 2; i+4 <= len(stream); {
		marker := stream[i+1]
		if marker == 0xDA { // SOS: its header, then the rest of the stream
			n := int(stream[i+2])<<8 | int(stream[i+3])
			seg := append([]byte{}, stream[i:i+2+n]...)
			for k := 0; k < int(seg[4]); k++ {
				seg[5+2*k] = ids[k]
			}
			out = append(out, seg...)
			return append(out, stream[i+2+n:]...)
		}
		n := int(stream[i+2])<<8 | int(stream[i+3])
		seg := append([]byte{}, stream[i:i+2+n]...)
		i += 2 + n
		if marker >= 0xE0 && marker <= 0xEF {
			continue
		}
		if marker == 0xC0 || marker == 0xC1 {
			for k := 0; k < int(seg[9]); k++ {
				seg[10+3*k] = ids[k]
			}
		}
		out = append(out, seg...)
	}
	t.Fatal("no SOS in the stream")
	return nil
}

var (
	jfifAPP0 = []byte{0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	idsPlain = [3]byte{1, 2, 3}
	idsRGB   = [3]byte{'R', 'G', 'B'}
)

func adobeAPP14(transform byte) []byte {
	return []byte{0xFF, 0xEE, 0x00, 0x0E, 'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, transform}
}

// yccToRGB is the JFIF conversion (T.871), in floating point.
func yccToRGB(stored []byte) []byte {
	out := make([]byte, len(stored))
	clamp := func(v float64) byte { return byte(math.Max(0, math.Min(255, math.Round(v)))) }
	for i := 0; i+2 < len(stored); i += 3 {
		y, cb, cr := float64(stored[i]), float64(stored[i+1])-128, float64(stored[i+2])-128
		out[i] = clamp(y + 1.402*cr)
		out[i+1] = clamp(y - 0.344136*cb - 0.714136*cr)
		out[i+2] = clamp(y + 1.772*cb)
	}
	return out
}

func maxDiff(a, b []byte) int {
	worst := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		worst = max(worst, d)
	}
	return worst
}

// TestJPEGColorComesFromThePhotometricInterpretation proves that with a
// Photometric Interpretation of RGB, YBR_FULL or YBR_FULL_422 the three
// components come back exactly as stored whatever the stream says (no marker,
// JFIF, Adobe transform 0 or 1, identifiers R, G, B), which is dcmtk's default
// and what the data set describes. Without one the decoder still guesses, as
// libjpeg does: converted from YCbCr unless an Adobe marker says transform 0
// or the identifiers say R, G, B. The case that failed was RGB with no marker,
// decoded as YCbCr, which turned pydicom's gray SC_jpeg_no_color_transform.dcm
// magenta.
func TestJPEGColorComesFromThePhotometricInterpretation(t *testing.T) {
	const w, h = 16, 16
	samples := make([]byte, 0, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			samples = append(samples, byte(40+x*12), byte(60+y*9), byte(200-x*5-y*3))
		}
	}
	base, err := compress.EncodeJPEGBaseline(samples, w, h, 3, 95)
	if err != nil {
		t.Fatal(err)
	}

	// The stored components: the stream marked RGB, decoded with no data set.
	stored, err := compress.NewJPEGDecompressor().Decompress(colorEvidence(t, base, idsPlain, adobeAPP14(0)))
	if err != nil {
		t.Fatal(err)
	}
	if d := maxDiff(stored, samples); d > 4 {
		t.Fatalf("the stored components are %d away from what was coded", d)
	}
	converted := yccToRGB(stored)

	evidence := []struct {
		name     string
		ids      [3]byte
		app      []byte
		guessRGB bool // what a decoder with no data set takes the components for
	}{
		{"no marker", idsPlain, nil, false},
		{"JFIF", idsPlain, jfifAPP0, false},
		{"Adobe 0", idsPlain, adobeAPP14(0), true},
		{"Adobe 1", idsPlain, adobeAPP14(1), false},
		{"ids R, G, B", idsRGB, nil, true},
	}
	for _, ev := range evidence {
		stream := colorEvidence(t, base, ev.ids, ev.app)
		for _, pi := range []string{"RGB", "YBR_FULL", "YBR_FULL_422"} {
			got, err := compress.NewJPEGDecompressorForPhotometric(pi).Decompress(stream)
			if err != nil {
				t.Fatalf("%s, %s: %v", pi, ev.name, err)
			}
			if !bytes.Equal(got, stored) {
				t.Errorf("%s, %s: not the stored components (%d away; %d from them converted as YCbCr)",
					pi, ev.name, maxDiff(got, stored), maxDiff(got, converted))
			}
		}
		for _, pi := range []string{"", "MONOCHROME2"} {
			got, err := compress.NewJPEGDecompressorForPhotometric(pi).Decompress(stream)
			if err != nil {
				t.Fatalf("%q, %s: %v", pi, ev.name, err)
			}
			want, tolerance, as := converted, 2, "converted from YCbCr"
			if ev.guessRGB {
				want, tolerance, as = stored, 0, "as stored"
			}
			if d := maxDiff(got, want); d > tolerance {
				t.Errorf("no Photometric Interpretation (%q), %s: should guess %s, %d away", pi, ev.name, as, d)
			}
		}
	}
}

// TestJPEGSubsampledColorIsUpsampledOneWay proves a 4:2:0 frame under a YBR
// Photometric Interpretation decodes to the same samples whether or not its
// stream carries an Adobe marker saying RGB or identifiers R, G, B. With one,
// image/jpeg took the frame for RGB and repeated its chroma along each row
// only, so the same data set decoded to two different pictures depending on a
// marker the data set overrules.
func TestJPEGSubsampledColorIsUpsampledOneWay(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 32, 24), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = byte(i * 7)
	}
	for i := range src.Cb {
		src.Cb[i], src.Cr[i] = byte(i*13), byte(255-i*11)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	decoder := compress.NewJPEGDecompressorForPhotometric("YBR_FULL_422")
	want, err := decoder.Decompress(colorEvidence(t, buf.Bytes(), idsPlain, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []struct {
		name string
		ids  [3]byte
		app  []byte
	}{{"Adobe 0", idsPlain, adobeAPP14(0)}, {"ids R, G, B", idsRGB, nil}, {"JFIF", idsPlain, jfifAPP0}} {
		got, err := decoder.Decompress(colorEvidence(t, buf.Bytes(), ev.ids, ev.app))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: decodes %d away from the same frame without it", ev.name, maxDiff(got, want))
		}
	}
}
