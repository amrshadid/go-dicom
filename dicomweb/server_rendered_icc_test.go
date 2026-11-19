package dicomweb

import (
	"bytes"
	"image/jpeg"
	"image/png"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/icctest"
	"github.com/amrshadid/go-dicom/tag"
)

// withProfile gives an image an ICC Profile (0028,2000), padded to an even
// length as an OB value is, and, for rgb, makes it an RGB image of the
// same frames.
func withProfile(t *testing.T, ds *dataset.Dataset, profile []byte, rgb bool) {
	t.Helper()
	set := func(tg tag.Tag, vr dataelem.VR, value any) {
		ds.Remove(tg)
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	if len(profile)%2 == 1 {
		profile = append(profile[:len(profile):len(profile)], 0)
	}
	set(tag.New(0x0028, 0x2000), dataelem.OB, profile)
	if rgb {
		element, _ := ds.Get(tag.New(0x7FE0, 0x0010))
		gray := element.GetValue().([]byte)
		pixels := make([]byte, 0, 3*len(gray))
		for i, v := range gray {
			pixels = append(pixels, v, byte(i), 255-v)
		}
		set(tag.New(0x0028, 0x0002), dataelem.US, uint16(3))
		set(tag.New(0x0028, 0x0004), dataelem.CS, "RGB")
		set(tag.New(0x0028, 0x0006), dataelem.US, uint16(0))
		set(tag.New(0x7FE0, 0x0010), dataelem.OB, pixels)
	}
}

// TestRenderedCarriesTheICCProfileWhenAsked: with iccprofile=yes (PS3.18
// 8.3.5.1.5) a rendered frame, as PNG and as JPEG, and a thumbnail carry
// the instance's own profile, byte for byte, and the same pixels as
// without it; without the parameter, or with iccprofile=no, they carry
// none, as before. The gray profile here is longer than the 1 KiB that
// metadata holds inline; the read that finds the frames leaves every
// binary value in the file, so the profile has to be read from it again.
func TestRenderedCarriesTheICCProfileWhenAsked(t *testing.T) {
	grayProfile := icctest.Profile("GRAY", strings.Repeat("a long gray profile ", 80)+"!")
	rgbProfile := icctest.Profile("RGB ", "an RGB profile")
	gray := multiFrame(t, "1.2.3.20", 2, 16, 16)
	withProfile(t, gray, grayProfile, false)
	color := multiFrame(t, "1.2.3.21", 2, 16, 16)
	withProfile(t, color, rgbProfile, true)
	backend := &recorder{}
	for uid, ds := range map[string]*dataset.Dataset{"1.2.3.20": gray, "1.2.3.21": color} {
		file, err := encodeFile(ds)
		if err != nil {
			t.Fatal(err)
		}
		backend.hold(uid, file)
	}

	for uid, profile := range map[string][]byte{"1.2.3.20": grayProfile, "1.2.3.21": rgbProfile} {
		for _, media := range []string{mediaPNG, mediaJPEG} {
			frame := heldInstances + uid + "/frames/2/rendered"
			response, with := get(t, backend, frame+"?iccprofile=yes", "Accept", media)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s as %s: status %d: %s", uid, media, response.StatusCode, with)
			}
			if got, err := icctest.Embedded([]byte(with)); err != nil || !bytes.Equal(got, profile) {
				t.Errorf("%s as %s with iccprofile=yes: %d bytes of profile (%v); want the instance's %d",
					uid, media, len(got), err, len(profile))
			}
			if warnings := response.Header.Values("Warning"); len(warnings) > 0 {
				t.Errorf("%s as %s: %v", uid, media, warnings)
			}
			for _, query := range []string{"", "?iccprofile=no"} {
				_, without := get(t, backend, frame+query, "Accept", media)
				if got, _ := icctest.Embedded([]byte(without)); got != nil {
					t.Errorf("%s as %s with %q carries a profile", uid, media, query)
				}
				decode := png.Decode
				if media == mediaJPEG {
					decode = jpeg.Decode
				}
				a, errA := decode(strings.NewReader(with))
				b, errB := decode(strings.NewReader(without))
				if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
					t.Errorf("%s as %s: the pixels with the profile are not those without (%v, %v)", uid, media, errA, errB)
				}
			}
		}
	}

	_, thumbnail := get(t, backend, heldInstances+"1.2.3.21/thumbnail?iccprofile=yes")
	if got, _ := icctest.Embedded([]byte(thumbnail)); !bytes.Equal(got, rgbProfile) {
		t.Errorf("the thumbnail carries %d bytes of profile; want %d", len(got), len(rgbProfile))
	}
}

// TestRenderedICCProfileRefusals: an instance with no profile asked for
// one is drawn without, and a Warning says so; a named color space asks
// for the pixels converted, which is not done, and is refused with 400
// saying so; any other value is 400.
func TestRenderedICCProfileRefusals(t *testing.T) {
	backend, _ := rendering(t)
	frame := heldInstances + "1.2.3.7/frames/1/rendered"
	response, body := get(t, backend, frame+"?iccprofile=yes", "Accept", mediaPNG)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if got, _ := icctest.Embedded([]byte(body)); got != nil {
		t.Error("a profile was embedded for an instance that has none")
	}
	if warning := strings.Join(response.Header.Values("Warning"), " "); !strings.Contains(warning, "has no ICC Profile") {
		t.Errorf("Warning %q; want it to say the instance has no ICC Profile", warning)
	}
	for _, value := range []string{"srgb", "adobergb", "rommrgb", "displayp3"} {
		response, body := get(t, backend, frame+"?iccprofile="+value)
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "converted") {
			t.Errorf("iccprofile=%s: status %d: %s", value, response.StatusCode, body)
		}
	}
	if response, _ := get(t, backend, frame+"?iccprofile=maybe"); response.StatusCode != http.StatusBadRequest {
		t.Errorf("iccprofile=maybe: status %d", response.StatusCode)
	}
}
