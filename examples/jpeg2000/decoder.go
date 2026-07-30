// Package jpeg2000 is a reference external decoder for JPEG 2000 pixel data.
//
// go-dicom bundles no JPEG 2000 codec. Wavelet transforms plus EBCOT arithmetic
// coding is thousands of lines to implement correctly, and the alternative —
// CGO against openjpeg — would cost the library a property it advertises: no
// hidden native dependency. So the decoder is left to the caller, and this is
// what a caller can copy.
//
// It shells out to openjpeg's opj_decompress, which is the reference
// implementation and is packaged nearly everywhere. That makes it correct and
// portable at the cost of a process per frame, which is the right trade for a
// converter or an ingest pipeline and the wrong one for an interactive viewer.
// Anything satisfying compress.Decompressor will do instead — a CGO binding, a
// service call, another Go library.
//
// This package is an example rather than part of the library: nothing in
// go-dicom executes an external program, and adding it here would not change
// that.
//
// Usage:
//
//	dec, err := jpeg2000.NewDecoder()      // finds opj_decompress on PATH
//	if err != nil { ... }
//	compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_2000, dec)
//
// after which (7FE0,0010) in a JPEG 2000 instance decodes like any other.
package jpeg2000

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Decoder decodes JPEG 2000 codestreams by invoking opj_decompress.
type Decoder struct {
	// Program is the decompressor to run. Defaults to opj_decompress on PATH.
	Program string
}

// NewDecoder returns a Decoder, failing if opj_decompress cannot be found.
//
// Checked up front rather than at the first frame: a decoder registered and then
// unable to run turns a missing dependency into a decoding error on some
// arbitrary instance much later.
func NewDecoder() (*Decoder, error) {
	path, err := exec.LookPath("opj_decompress")
	if err != nil {
		return nil, fmt.Errorf("opj_decompress is not on PATH: %w", err)
	}
	return &Decoder{Program: path}, nil
}

// CanDecompress reports whether data looks like a JPEG 2000 codestream.
//
// Two framings occur in DICOM: the raw codestream, which starts with the SOC
// marker FF4F, and the JP2 container, whose signature box starts with a length
// and the tag "jP  ". The standard calls for the former, and encoders produce
// the latter anyway.
func (d *Decoder) CanDecompress(data []byte) bool {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0x4F {
		return true
	}
	return len(data) >= 12 && bytes.Equal(data[4:8], []byte("jP  "))
}

// Decompress decodes one frame to native little-endian samples.
func (d *Decoder) Decompress(data []byte) ([]byte, error) {
	if !d.CanDecompress(data) {
		return nil, errors.New("jpeg2000: not a JPEG 2000 codestream")
	}

	dir, err := os.MkdirTemp("", "godicom-j2k")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The extension tells opj_decompress which framing to expect.
	name := "frame.jp2"
	if bytes.Equal(data[:2], []byte{0xFF, 0x4F}) {
		name = "frame.j2k"
	}
	in := filepath.Join(dir, name)
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}

	// The output format is chosen by extension, and the two netpbm formats hold
	// one and three components respectively — so the component count has to be
	// known before the decoder runs. It is in the codestream's SIZ marker, along
	// with the precision and signedness needed to interpret the result.
	info, err := parseSIZ(data)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "frame.pgm")
	if info.components > 1 {
		out = filepath.Join(dir, "frame.ppm")
	}

	program := d.Program
	if program == "" {
		program = "opj_decompress"
	}
	cmd := exec.Command(program, "-i", in, "-o", out)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("jpeg2000: %s failed: %w\n%s", program, err, combined)
	}

	decoded, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("jpeg2000: %s wrote no output: %w", program, err)
	}
	return parseNetpbm(decoded, info)
}

// sizInfo is what the SIZ marker says about the frame's samples.
type sizInfo struct {
	components int
	precision  int
	signed     bool
}

// parseSIZ reads the SIZ marker segment.
//
// SIZ follows the SOC marker and carries the image geometry. Csiz sits 36 bytes
// past the segment length, after the eight 4-byte size and offset fields, and
// each component's Ssiz byte follows: its low seven bits are the precision less
// one, and its top bit says whether samples are signed.
func parseSIZ(data []byte) (sizInfo, error) {
	// From the start of Lsiz: Lsiz(2) + Rsiz(2) + eight 4-byte size and offset
	// fields = 36 bytes before Csiz.
	const sizOffsetToCsiz = 36

	for i := 0; i+1 < len(data); i++ {
		if data[i] != 0xFF || data[i+1] != 0x51 {
			continue
		}
		at := i + 2 + sizOffsetToCsiz
		if at+2 > len(data) {
			return sizInfo{}, errors.New("jpeg2000: SIZ marker is truncated")
		}
		n := int(binary.BigEndian.Uint16(data[at:]))
		if n < 1 {
			return sizInfo{}, fmt.Errorf("jpeg2000: SIZ declares %d components", n)
		}
		if at+2+3*n > len(data) {
			return sizInfo{}, errors.New("jpeg2000: SIZ marker does not carry all its components")
		}
		ssiz := data[at+2]
		return sizInfo{
			components: n,
			precision:  int(ssiz&0x7F) + 1,
			signed:     ssiz&0x80 != 0,
		}, nil
	}
	// A JP2 container puts the codestream in a box; falling back to a guess would
	// silently mis-scale signed data, so say so instead.
	return sizInfo{}, errors.New("jpeg2000: no SIZ marker found; cannot tell how the samples are encoded")
}

// parseNetpbm converts opj_decompress's PGM or PPM output to native samples.
//
// Two corrections, both invisible until the result is compared with something
// else:
//
// Netpbm stores 16-bit samples big-endian, so they are byte-swapped. DICOM
// native pixel data is little endian, and handing back the file's order would
// produce an image whose every value is scaled by 256.
//
// Netpbm has no signed form, so openjpeg writes signed components with a DC
// level shift of half the range added to every sample. Left in, a signed CT
// frame comes back uniformly 32768 too high — every value wrong, the image still
// looking like an image.
func parseNetpbm(data []byte, info sizInfo) ([]byte, error) {
	pos := 0

	field := func() (string, error) {
		for pos < len(data) {
			switch data[pos] {
			case '#':
				// A comment runs to the end of its line; openjpeg writes one.
				for pos < len(data) && data[pos] != '\n' {
					pos++
				}
			case ' ', '\t', '\n', '\r':
				pos++
			default:
				start := pos
				for pos < len(data) && data[pos] != ' ' && data[pos] != '\t' &&
					data[pos] != '\n' && data[pos] != '\r' {
					pos++
				}
				return string(data[start:pos]), nil
			}
		}
		return "", errors.New("jpeg2000: netpbm output ended inside its header")
	}

	magic, err := field()
	if err != nil {
		return nil, err
	}
	samplesPerPixel := 1
	switch magic {
	case "P5":
	case "P6":
		samplesPerPixel = 3
	default:
		return nil, fmt.Errorf("jpeg2000: unexpected netpbm format %q", magic)
	}

	dims := make([]int, 3)
	for i := range dims {
		token, err := field()
		if err != nil {
			return nil, err
		}
		v, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("jpeg2000: netpbm header field %q: %w", token, err)
		}
		dims[i] = v
	}
	width, height, maxVal := dims[0], dims[1], dims[2]
	if width <= 0 || height <= 0 || maxVal <= 0 {
		return nil, fmt.Errorf("jpeg2000: netpbm header says %dx%d maxval %d", width, height, maxVal)
	}

	// Exactly one whitespace byte separates the header from the raster.
	pos++

	// The shift openjpeg applied to fit signed samples into an unsigned format.
	shift := 0
	if info.signed {
		shift = 1 << (info.precision - 1)
	}

	count := width * height * samplesPerPixel
	if maxVal <= 255 {
		if pos+count > len(data) {
			return nil, fmt.Errorf("jpeg2000: netpbm output holds %d of %d samples", len(data)-pos, count)
		}
		if shift == 0 {
			return data[pos : pos+count], nil
		}
		out := make([]byte, count)
		for i := 0; i < count; i++ {
			out[i] = byte(int(data[pos+i]) - shift)
		}
		return out, nil
	}

	if pos+count*2 > len(data) {
		return nil, fmt.Errorf("jpeg2000: netpbm output holds %d of %d 16-bit samples",
			(len(data)-pos)/2, count)
	}
	out := make([]byte, count*2)
	for i := 0; i < count; i++ {
		v := int(binary.BigEndian.Uint16(data[pos+i*2:])) - shift
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(v)))
	}
	return out, nil
}
