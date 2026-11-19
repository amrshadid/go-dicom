package iccembed

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/amrshadid/go-dicom/internal/icctest"
)

// picture is a small color picture with something in it.
func picture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 37, 21))
	for y := 0; y < 21; y++ {
		for x := 0; x < 37; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 7), uint8(y * 12), uint8(x * y), 255})
		}
	}
	return img
}

// pngChunks returns the type and data of every chunk of a PNG, in order.
func pngChunks(t *testing.T, data []byte) (types []string, bodies [][]byte) {
	t.Helper()
	data = data[8:]
	for len(data) >= 12 {
		n := binary.BigEndian.Uint32(data)
		types = append(types, string(data[4:8]))
		bodies = append(bodies, data[8:8+n])
		data = data[12+n:]
	}
	return types, bodies
}

// TestPNGCarriesTheProfile: the PNG holds an iCCP chunk right after IHDR
// whose profile, inflated, is the bytes given, and the rest of the file is
// the PNG image/png writes without it, so the pixels are the same. Go's
// decoder checks every chunk's CRC, so it reading the file proves the
// chunk's.
func TestPNGCarriesTheProfile(t *testing.T) {
	profile := icctest.Profile("RGB ", "a test profile")
	var plain, with bytes.Buffer
	if err := png.Encode(&plain, picture()); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(PNG(&with, profile), picture()); err != nil {
		t.Fatal(err)
	}
	types, bodies := pngChunks(t, with.Bytes())
	if len(types) < 2 || types[0] != "IHDR" || types[1] != "iCCP" {
		t.Fatalf("chunks %v; want IHDR then iCCP", types)
	}
	name, rest, _ := bytes.Cut(bodies[1], []byte{0})
	if string(name) != "ICC profile" || rest[0] != 0 {
		t.Fatalf("iCCP names %q with compression method %d", name, rest[0])
	}
	z, err := zlib.NewReader(bytes.NewReader(rest[1:]))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(z)
	if err != nil || !bytes.Equal(got, profile) {
		t.Fatalf("the iCCP chunk inflates to %d bytes (%v); want the %d given", len(got), err, len(profile))
	}
	chunk := 12 + len(bodies[1])
	if !bytes.Equal(append(with.Bytes()[:33:33], with.Bytes()[33+chunk:]...), plain.Bytes()) {
		t.Error("the PNG with the profile is not the PNG without it with one chunk added")
	}
	if _, err := png.Decode(bytes.NewReader(with.Bytes())); err != nil {
		t.Errorf("image/png cannot read it back: %v", err)
	}
}

// TestJPEGCarriesTheProfile: a profile larger than one segment holds is
// split across numbered APP2 ICC_PROFILE segments right after the start of
// image, which put back together are the bytes given, and the rest is the
// JPEG image/jpeg writes without them.
func TestJPEGCarriesTheProfile(t *testing.T) {
	profile := icctest.Profile("RGB ", "a test profile")
	profile = append(profile, make([]byte, 150000)...) // three segments
	var plain, with bytes.Buffer
	if err := jpeg.Encode(&plain, picture(), nil); err != nil {
		t.Fatal(err)
	}
	w, err := JPEG(&with, profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(w, picture(), nil); err != nil {
		t.Fatal(err)
	}
	data := with.Bytes()[2:]
	var got []byte
	for i := 1; len(data) > 4 && data[0] == 0xFF && data[1] == 0xE2; i++ {
		n := int(binary.BigEndian.Uint16(data[2:]))
		segment := data[4 : 2+n]
		if string(segment[:12]) != "ICC_PROFILE\x00" || int(segment[12]) != i || segment[13] != 3 {
			t.Fatalf("segment %d starts % x", i, segment[:14])
		}
		got = append(got, segment[14:]...)
		data = data[2+n:]
	}
	if !bytes.Equal(got, profile) {
		t.Fatalf("the APP2 segments hold %d bytes; want the %d given", len(got), len(profile))
	}
	if !bytes.Equal(append(with.Bytes()[:2:2], data...), plain.Bytes()) {
		t.Error("the JPEG with the profile is not the JPEG without it with the segments added")
	}
	if _, err := jpeg.Decode(bytes.NewReader(with.Bytes())); err != nil {
		t.Errorf("image/jpeg cannot read it back: %v", err)
	}
	if _, err := JPEG(io.Discard, make([]byte, 256*maxSegment)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a profile of 256 segments gave %v", err)
	}
}

// TestFits: an RGB profile describes a color picture and a gray profile a
// grayscale one, and neither the other, which PNG forbids.
func TestFits(t *testing.T) {
	rgb, gray := icctest.Profile("RGB ", "rgb"), icctest.Profile("GRAY", "gray")
	grayPicture := image.NewGray(image.Rect(0, 0, 2, 2))
	for _, c := range []struct {
		profile []byte
		img     image.Image
		fits    bool
	}{
		{rgb, picture(), true},
		{gray, grayPicture, true},
		{rgb, grayPicture, false},
		{gray, picture(), false},
		{rgb[:10], picture(), false},
	} {
		if err := Fits(c.profile, c.img); (err == nil) != c.fits {
			t.Errorf("a profile of %d bytes, a %T: %v", len(c.profile), c.img, err)
		}
	}
}
