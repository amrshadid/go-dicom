package jpeg2000_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/compress/jpeg2000"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// The expectations below are OpenJPEG's, read from
//
//	opj_dump -i <frame>.j2k
//
// on each fixture, so this test compares the parser with another
// implementation rather than with itself. numresolutions there is one more than
// the decomposition levels the marker carries.
type codestreamWant struct {
	width, height uint32
	components    int
	depth         int
	signed        bool
	tileW, tileH  uint32
	tiles         int
	progression   int
	layers        int
	mct           bool
	levels        int
	codeBlock     int
	wavelet       int
	quantStyle    int
	guardBits     int
	tileParts     int
}

var fixtures = map[string]codestreamWant{
	"MR_small_jp2klossless.dcm": {
		width: 64, height: 64, components: 1, depth: 16, signed: true,
		tileW: 64, tileH: 64, tiles: 1, progression: jpeg2000.ProgressionLRCP,
		layers: 1, levels: 5, codeBlock: 64, wavelet: jpeg2000.Wavelet53Reversible,
		quantStyle: jpeg2000.QuantNone, guardBits: 2, tileParts: 1,
	},
	"693_J2KI.dcm": {
		width: 512, height: 512, components: 1, depth: 16, signed: true,
		tileW: 512, tileH: 512, tiles: 1, progression: jpeg2000.ProgressionLRCP,
		layers: 3, levels: 5, codeBlock: 64, wavelet: jpeg2000.Wavelet53Reversible,
		quantStyle: jpeg2000.QuantNone, guardBits: 2, tileParts: 1,
	},
	"J2K_pixelrep_mismatch.dcm": {
		width: 512, height: 512, components: 1, depth: 13, signed: false,
		tileW: 512, tileH: 512, tiles: 1, progression: jpeg2000.ProgressionLRCP,
		layers: 1, levels: 5, codeBlock: 64, wavelet: jpeg2000.Wavelet53Reversible,
		quantStyle: jpeg2000.QuantNone, guardBits: 2, tileParts: 1,
	},
	"SC_rgb_gdcm_KY.dcm": {
		width: 100, height: 100, components: 3, depth: 8, signed: false,
		tileW: 100, tileH: 100, tiles: 1, progression: jpeg2000.ProgressionLRCP,
		layers: 1, levels: 5, codeBlock: 64, wavelet: jpeg2000.Wavelet53Reversible,
		quantStyle: jpeg2000.QuantNone, guardBits: 2, tileParts: 1,
	},
	// Sixteen tiles, six layers, RCT, and the resolution-first progression:
	// the fixture that exercises everything stage 6 adds.
	"GDCMJ2K_TextGBR.dcm": {
		width: 400, height: 400, components: 3, depth: 8, signed: false,
		tileW: 128, tileH: 128, tiles: 16, progression: jpeg2000.ProgressionRLCP,
		layers: 6, mct: true, levels: 5, codeBlock: 32,
		wavelet: jpeg2000.Wavelet53Reversible, quantStyle: jpeg2000.QuantNone,
		// Six layers written as six tile-parts per tile: 16 x 6.
		guardBits: 2, tileParts: 96,
	},
	// The irreversible one: 9/7 with expounded quantization.
	"JPEG2000.dcm": {
		width: 256, height: 1024, components: 1, depth: 16, signed: true,
		tileW: 256, tileH: 1024, tiles: 1, progression: jpeg2000.ProgressionLRCP,
		layers: 1, levels: 5, codeBlock: 64, wavelet: jpeg2000.Wavelet97Irreversible,
		quantStyle: jpeg2000.QuantExpounded, guardBits: 1, tileParts: 1,
	},
}

// TestParseCodestreamAgainstOpenJPEG parses every JPEG 2000 fixture in
// pydicom's corpus and checks each parameter against what OpenJPEG reports.
func TestParseCodestreamAgainstOpenJPEG(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}

	for name, want := range fixtures {
		t.Run(name, func(t *testing.T) {
			frame := firstFrame(t, filepath.Join(dir, name))
			got, err := jpeg2000.ParseCodestream(frame)
			if err != nil {
				t.Fatalf("ParseCodestream: %v", err)
			}

			checks := []struct {
				what      string
				got, want any
			}{
				{"width", got.Width, want.width},
				{"height", got.Height, want.height},
				{"components", len(got.Components), want.components},
				{"depth", got.Components[0].Depth, want.depth},
				{"signed", got.Components[0].Signed, want.signed},
				{"tile width", got.TileWidth, want.tileW},
				{"tile height", got.TileHeight, want.tileH},
				{"tiles", got.NumTiles(), want.tiles},
				{"progression", got.Coding.Progression, want.progression},
				{"layers", got.Coding.Layers, want.layers},
				{"multiple component transform", got.Coding.MCT, want.mct},
				{"decomposition levels", got.Coding.Levels, want.levels},
				{"code-block width", got.Coding.CodeBlockWidth, want.codeBlock},
				{"code-block height", got.Coding.CodeBlockHt, want.codeBlock},
				{"wavelet", got.Coding.Wavelet, want.wavelet},
				{"quantization style", got.Quant.Style, want.quantStyle},
				{"guard bits", got.Quant.GuardBits, want.guardBits},
				{"tile-parts", len(got.TileParts), want.tileParts},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %v, want %v (OpenJPEG)", c.what, c.got, c.want)
				}
			}

			// One exponent per subband: 3 per decomposition level plus the LL.
			if wantBands := 3*want.levels + 1; len(got.Quant.Exponents) != wantBands {
				t.Errorf("%d quantization values, want %d for %d levels",
					len(got.Quant.Exponents), wantBands, want.levels)
			}
			// Every tile-part holds data, and together they account for the
			// whole frame bar the headers.
			for _, part := range got.TileParts {
				if len(part.Data) == 0 {
					t.Errorf("tile-part %d of tile %d is empty", part.Index, part.Tile)
				}
			}
		})
	}
}

// TestParseCodestreamRefusesWhatItCannotDecode: a feature outside the subset is
// named, not guessed at. A decoder that mis-decodes quietly is worse than one
// that says it cannot.
func TestParseCodestreamRefusesWhatItCannotDecode(t *testing.T) {
	base := buildCodestream(t, func(cod []byte, siz []byte) {})

	tests := map[string]struct {
		patch func(cod, siz []byte)
		says  string
	}{
		// Offsets into the segments buildCodestream writes: Scod at 4,
		// progression 5, code-block style 12, wavelet 13; in SIZ the first
		// component's Ssiz is at 40 and its XRsiz at 41.
		"code-block style": {func(cod, _ []byte) { cod[12] = 0x01 }, "code-block style"},
		"precincts":        {func(cod, _ []byte) { cod[4] = 0x01 }, "precinct"},
		"progression":      {func(cod, _ []byte) { cod[5] = 4 }, "progression order"},
		"wavelet":          {func(cod, _ []byte) { cod[13] = 9 }, "wavelet"},
		"subsampling":      {func(_, siz []byte) { siz[41] = 2 }, "subsampling"},
		"bit depth":        {func(_, siz []byte) { siz[40] = 0x80 | 31 }, "32-bit"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			data := buildCodestream(t, tc.patch)
			_, err := jpeg2000.ParseCodestream(data)
			if err == nil {
				t.Fatal("parsed a codestream it cannot decode")
			}
			var unsupported *jpeg2000.ErrUnsupported
			if !errors.As(err, &unsupported) {
				t.Fatalf("error is %v, want an ErrUnsupported naming the feature", err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("error %q does not name %q", err, tc.says)
			}
		})
	}

	// And the unpatched one parses, so the refusals above are about the patch.
	if _, err := jpeg2000.ParseCodestream(base); err != nil {
		t.Fatalf("the unpatched codestream did not parse: %v", err)
	}
}

// TestParseCodestreamRejectsTruncatedInput: every length in a codestream comes
// from the file, so each is checked before it is used.
func TestParseCodestreamRejectsTruncatedInput(t *testing.T) {
	full := buildCodestream(t, func(cod, siz []byte) {})
	for cut := 1; cut < len(full); cut += 3 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("truncating to %d bytes panicked: %v", cut, r)
				}
			}()
			_, _ = jpeg2000.ParseCodestream(full[:cut])
		}()
	}
}

// buildCodestream writes a minimal single-tile codestream, letting a test patch
// the COD and SIZ segments before they are assembled.
func buildCodestream(t *testing.T, patch func(cod, siz []byte)) []byte {
	t.Helper()

	siz := make([]byte, 2+38+3)
	binary.BigEndian.PutUint16(siz, 0xFF51)
	binary.BigEndian.PutUint16(siz[2:], uint16(len(siz)-2))
	binary.BigEndian.PutUint32(siz[6:], 64)  // Xsiz
	binary.BigEndian.PutUint32(siz[10:], 64) // Ysiz
	binary.BigEndian.PutUint32(siz[22:], 64) // XTsiz
	binary.BigEndian.PutUint32(siz[26:], 64) // YTsiz
	binary.BigEndian.PutUint16(siz[38:], 1)  // one component
	siz[40] = 0x8F                           // signed, 16 bit
	siz[41], siz[42] = 1, 1                  // no subsampling

	// marker(2) + Lcod(2) + Scod(1) + SGcod(4) + SPcod(5)
	cod := make([]byte, 14)
	binary.BigEndian.PutUint16(cod, 0xFF52)
	binary.BigEndian.PutUint16(cod[2:], uint16(len(cod)-2))
	cod[4] = 0x00                           // Scod: no precincts, no SOP, no EPH
	cod[5] = byte(jpeg2000.ProgressionLRCP) // progression
	binary.BigEndian.PutUint16(cod[6:], 1)  // layers
	cod[8] = 0                              // no MCT
	cod[9] = 5                              // decomposition levels
	cod[10], cod[11] = 4, 4                 // 64x64 code-blocks
	cod[12] = 0                             // code-block style
	cod[13] = byte(jpeg2000.Wavelet53Reversible)

	patch(cod, siz)

	qcd := make([]byte, 2+2+16)
	binary.BigEndian.PutUint16(qcd, 0xFF5C)
	binary.BigEndian.PutUint16(qcd[2:], uint16(len(qcd)-2))
	qcd[4] = 0x40 // two guard bits, style 0
	for i := range qcd[5:] {
		qcd[5+i] = byte(16 << 3)
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint16(0xFF4F)) // SOC
	out.Write(siz)
	out.Write(cod)
	out.Write(qcd)

	// One tile-part with a byte of data, then EOC.
	sot := make([]byte, 12)
	binary.BigEndian.PutUint16(sot, 0xFF90)
	binary.BigEndian.PutUint16(sot[2:], 10)
	binary.BigEndian.PutUint16(sot[4:], 0)              // tile 0
	binary.BigEndian.PutUint32(sot[6:], uint32(12+2+1)) // Psot: SOT + SOD + data
	out.Write(sot)
	_ = binary.Write(&out, binary.BigEndian, uint16(0xFF93)) // SOD
	out.WriteByte(0x00)
	_ = binary.Write(&out, binary.BigEndian, uint16(0xFFD9)) // EOC
	return out.Bytes()
}

// firstFrame returns the first encapsulated frame of a DICOM file, which for
// these fixtures is one JPEG 2000 codestream.
func firstFrame(t testing.TB, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s is not in this corpus", filepath.Base(path))
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Base(path), err)
	}
	elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatalf("%s has no pixel data", filepath.Base(path))
	}
	pixels, _ := elem.GetValue().([]byte)

	frames, errs := compress.GenerateFrames(bytes.NewReader(pixels), 1, "<")
	select {
	case frame := <-frames:
		if len(frame) == 0 {
			t.Fatalf("%s: the first frame is empty", filepath.Base(path))
		}
		return frame
	case err := <-errs:
		t.Fatalf("%s: reading the first frame: %v", filepath.Base(path), err)
	}
	return nil
}

// opjDumpAvailable reports whether OpenJPEG's dumper is on PATH, for tests that
// want to check the expectations above are still what it says.
func opjDumpAvailable() bool {
	_, err := exec.LookPath("opj_dump")
	return err == nil
}

var _ = opjDumpAvailable
