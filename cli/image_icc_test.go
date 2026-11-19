package cli

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/icctest"
	"github.com/amrshadid/go-dicom/tag"
)

// writeImageWithProfile writes a 4 by 3 image, RGB or 8-bit grayscale,
// with profile as its ICC Profile (0028,2000).
func writeImageWithProfile(t *testing.T, path string, rgb bool, profile []byte) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(tg tag.Tag, vr dataelem.VR, value interface{}) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	samples, photometric := 1, "MONOCHROME2"
	if rgb {
		samples, photometric = 3, "RGB "
	}
	pixels := make([]byte, 4*3*samples)
	for i := range pixels {
		pixels[i] = byte(i * 19)
	}
	if len(profile)%2 == 1 {
		profile = append(profile[:len(profile):len(profile)], 0)
	}
	add(tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.7")
	add(tag.New(0x0008, 0x0018), dataelem.UI, "1.2.826.0.1.3680043.10.511.9.9")
	add(tag.New(0x0028, 0x0002), dataelem.US, us(uint16(samples)))
	add(tag.New(0x0028, 0x0004), dataelem.CS, photometric)
	if rgb {
		add(tag.New(0x0028, 0x0006), dataelem.US, us(0))
	}
	add(tag.New(0x0028, 0x0010), dataelem.US, us(3))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(4))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))
	add(tag.New(0x0028, 0x2000), dataelem.OB, profile)
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, pixels)
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.7", "1.2.826.0.1.3680043.10.511.9.9", ds); err != nil {
		t.Fatal(err)
	}
}

// TestConvertCarriesTheICCProfile: the PNG and the JPEG of an image with an
// ICC Profile carry the profile, the same bytes, without the byte that pads
// an odd one, and the same pixels as the picture without it: an RGB
// profile in an RGB picture and a gray one in a grayscale picture. A
// grayscale picture is not given an RGB profile, which PNG forbids, and is
// written without one.
func TestConvertCarriesTheICCProfile(t *testing.T) {
	dir := t.TempDir()
	rgbProfile := icctest.Profile("RGB ", "an RGB test profile 2026") // of odd length
	grayProfile := icctest.Profile("GRAY", "a gray test profile")
	for _, c := range []struct {
		name    string
		rgb     bool
		profile []byte
		carried bool
	}{
		{"rgb", true, rgbProfile, true},
		{"gray", false, grayProfile, true},
		{"gray-with-rgb-profile", false, rgbProfile, false},
	} {
		in := filepath.Join(dir, c.name+".dcm")
		writeImageWithProfile(t, in, c.rgb, c.profile)
		ds, err := readImageDataset(in)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ds.RenderFrame(0, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"png", "jpeg"} {
			out := filepath.Join(dir, c.name+"."+format)
			if err := runConvert(in, out, "--format", format); err != nil {
				t.Fatalf("%s as %s: %v", c.name, format, err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			got, err := icctest.Embedded(data)
			if err != nil {
				t.Fatalf("%s as %s: %v", c.name, format, err)
			}
			switch {
			case c.carried && !bytes.Equal(got, c.profile):
				t.Errorf("%s as %s carries %d bytes of profile; want the %d of the image's", c.name, format, len(got), len(c.profile))
			case !c.carried && got != nil:
				t.Errorf("%s as %s carries a profile it cannot describe", c.name, format)
			}
			var plain bytes.Buffer
			decode := png.Decode
			if format == "png" {
				err = png.Encode(&plain, want)
			} else {
				err = jpeg.Encode(&plain, want, &jpeg.Options{Quality: 90})
				decode = jpeg.Decode
			}
			if err != nil {
				t.Fatal(err)
			}
			a, errA := decode(bytes.NewReader(data))
			b, errB := decode(bytes.NewReader(plain.Bytes()))
			if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
				t.Errorf("%s as %s: the pixels are not those of the picture without a profile (%v, %v)", c.name, format, errA, errB)
			}
		}
	}
}
