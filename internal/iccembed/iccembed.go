// Package iccembed writes an ICC profile into a PNG or a JPEG as it is
// encoded by image/png or image/jpeg, neither of which can carry one: an
// iCCP chunk in a PNG (PNG third edition 11.3.2.4) and APP2 ICC_PROFILE
// segments in a JPEG (ICC.1 Annex B.4). The pixels are not touched; the
// profile only says what color space they are in, so that a viewer that
// manages color shows them as the image means.
package iccembed

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"io"
)

// Fits reports whether profile can describe img as image/png and
// image/jpeg encode it: a grayscale profile for an *image.Gray, which both
// write with one sample a pixel, and an RGB profile for anything else,
// written as RGB. PNG requires the profile's color space to be the image's
// (11.3.2.4), and a JPEG of one component cannot be RGB either. It says
// why not when it does not.
func Fits(profile []byte, img image.Image) error {
	if len(profile) < 20 {
		return errors.New("the ICC profile is too short to have a header")
	}
	space := string(profile[16:20]) // ICC.1 7.2.6, data color space
	gray := img.ColorModel() == color.GrayModel
	if _, ok := img.(*image.Gray); !ok && gray {
		// Neither encoder writes one sample a pixel for any other
		// grayscale picture.
		gray = false
	}
	switch {
	case gray && space == "GRAY", !gray && space == "RGB ":
		return nil
	case gray:
		return fmt.Errorf("the ICC profile is for %q and the picture is grayscale", space)
	}
	return fmt.Errorf("the ICC profile is for %q and the picture is RGB", space)
}

// PNG returns a writer for image/png to encode into, which writes to w
// what it is given with an iCCP chunk holding profile after the header.
// image/png writes the 8-byte signature and the 25-byte IHDR chunk first,
// and iCCP has to come before PLTE and IDAT (PNG 5.6).
func PNG(w io.Writer, profile []byte) io.Writer {
	var chunk bytes.Buffer
	// The keyword is free text of 1 to 79 Latin-1 characters; then a null,
	// compression method 0 (zlib) and the compressed profile.
	chunk.WriteString("iCCP")
	chunk.WriteString("ICC profile")
	chunk.Write([]byte{0, 0})
	z := zlib.NewWriter(&chunk)
	_, _ = z.Write(profile)
	_ = z.Close()
	data := chunk.Bytes()
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(len(data)-4))
	out.Write(data)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(data))
	return &inserter{w: w, after: 8 + 25, insert: out.Bytes()}
}

// maxSegment is the profile bytes one APP2 segment holds: a segment is at
// most 65535 bytes with its length, less "ICC_PROFILE\0", the sequence
// number and the count.
const maxSegment = 65535 - 2 - 12 - 2

// ErrTooLarge is a profile more than 255 APP2 segments can hold, the most
// ICC.1 B.4 numbers.
var ErrTooLarge = errors.New("the ICC profile is larger than a JPEG can carry")

// JPEG returns a writer for image/jpeg to encode into, which writes to w
// what it is given with the APP2 segments that hold profile after the
// start of image. image/jpeg writes no APP segment of its own, so they are
// the first.
func JPEG(w io.Writer, profile []byte) (io.Writer, error) {
	count := (len(profile) + maxSegment - 1) / maxSegment
	if count > 255 {
		return nil, ErrTooLarge
	}
	var out bytes.Buffer
	for i := 0; i < count; i++ {
		part := profile[i*maxSegment : min((i+1)*maxSegment, len(profile))]
		out.Write([]byte{0xFF, 0xE2})
		_ = binary.Write(&out, binary.BigEndian, uint16(2+12+2+len(part)))
		out.WriteString("ICC_PROFILE\x00")
		out.Write([]byte{byte(i + 1), byte(count)})
		out.Write(part)
	}
	return &inserter{w: w, after: 2, insert: out.Bytes()}, nil
}

// inserter passes bytes through to w, writing insert once the first after
// bytes have gone.
type inserter struct {
	w      io.Writer
	after  int
	insert []byte
	seen   int
	done   bool
}

func (in *inserter) Write(p []byte) (int, error) {
	if in.done {
		return in.w.Write(p)
	}
	head := min(in.after-in.seen, len(p))
	if n, err := in.w.Write(p[:head]); err != nil {
		return n, err
	}
	in.seen += head
	if in.seen < in.after {
		return len(p), nil
	}
	in.done = true
	if _, err := in.w.Write(in.insert); err != nil {
		return head, err
	}
	n, err := in.w.Write(p[head:])
	return head + n, err
}
