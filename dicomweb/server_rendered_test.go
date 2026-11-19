package dicomweb

import (
	"bytes"
	goimage "image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A rendered image is held against Dataset.RenderFrame of the same frame,
// which is tested against its definitions where it lives; here the
// question is whether the right frame, window, region and size reach it,
// and whether what is not served is refused as PS3.18 says. PNG is used
// where pixels are compared, since it is lossless.

// decodePNG reads a PNG response.
func decodePNG(t *testing.T, response *http.Response, body string) goimage.Image {
	t.Helper()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != mediaPNG {
		t.Fatalf("status %d, Content-Type %q: %s", response.StatusCode, response.Header.Get("Content-Type"), body)
	}
	img, err := png.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// samePicture reports whether two pictures have the same size and pixels.
func samePicture(a, b goimage.Image) bool {
	if a.Bounds().Size() != b.Bounds().Size() {
		return false
	}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			r1, g1, b1, a1 := a.At(a.Bounds().Min.X+x, a.Bounds().Min.Y+y).RGBA()
			r2, g2, b2, a2 := b.At(b.Bounds().Min.X+x, b.Bounds().Min.Y+y).RGBA()
			if r1>>8 != r2>>8 || g1>>8 != g2>>8 || b1>>8 != b2>>8 || a1>>8 != a2>>8 {
				return false
			}
		}
	}
	return true
}

// rendering holds a three-frame 8-bit image of 16 by 16, and the same as
// JPEG.
func rendering(t *testing.T) (*recorder, *dataset.Dataset) {
	t.Helper()
	ds := multiFrame(t, "1.2.3.7", 3, 16, 16)
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	compressed := multiFrame(t, "1.2.3.8", 3, 16, 16)
	if err := compressed.CompressJPEGBaselineInPlace(95); err != nil {
		t.Fatal(err)
	}
	jpegFile, err := encodeFile(compressed)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	backend.hold("1.2.3.8", jpegFile)
	backend.hold("1.2.3.1", part10(t, "1.2.3.1"))
	return backend, ds
}

// TestRenderedFrameIsRenderFrame checks a frame rendered with a window:
// the picture RenderFrame makes of that frame with that window, as PNG,
// and as JPEG by default.
func TestRenderedFrameIsRenderFrame(t *testing.T) {
	backend, ds := rendering(t)
	want, err := ds.RenderFrame(1, &dataset.RenderOptions{Window: &dataset.Window{Center: 20, Width: 40, Function: "LINEAR"}})
	if err != nil {
		t.Fatal(err)
	}
	path := heldInstances + "1.2.3.7/frames/2/rendered?window=20,40,linear"
	response, body := get(t, backend, path, "Accept", "image/png")
	if got := decodePNG(t, response, body); !samePicture(got, want) {
		t.Error("frame 2 rendered is not RenderFrame's frame 2 with the window")
	}

	response, body = get(t, backend, path)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != mediaJPEG {
		t.Fatalf("by default: status %d, Content-Type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	if img, err := jpeg.Decode(strings.NewReader(body)); err != nil || img.Bounds().Dx() != 16 {
		t.Errorf("by default: not a 16-pixel-wide JPEG: %v", err)
	}

	// The same frame of the instance held as JPEG renders too: it is
	// decoded first.
	response, body = get(t, backend, heldInstances+"1.2.3.8/frames/2/rendered?window=20,40,linear", "Accept", "image/png")
	if got := decodePNG(t, response, body); got.Bounds().Dx() != 16 {
		t.Errorf("the JPEG instance's frame rendered %v", got.Bounds())
	}
}

// TestRenderedFrameTakesItsOwnWindow checks an enhanced image, which keeps
// each frame's window in that frame's functional group: frame 2 rendered
// alone is drawn with frame 2's window, as RenderFrame draws it in the
// whole image, and not with frame 1's.
func TestRenderedFrameTakesItsOwnWindow(t *testing.T) {
	ds := multiFrame(t, "1.2.3.7", 3, 16, 16)
	groups := sequence.New()
	for _, window := range [][2]string{{"5", "4"}, {"30", "60"}, {"12", "2"}} {
		voi := dataset.NewDataset()
		_ = voi.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, window[0]))
		_ = voi.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1051), dataelem.DS, window[1]))
		group := dataset.NewDataset()
		_ = group.Add(dataelem.NewDataElement(tag.New(0x0028, 0x9132), dataelem.SQ, sequence.NewFromItems(voi)))
		if err := groups.Append(group); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.Add(dataelem.NewDataElement(tag.New(0x5200, 0x9230), dataelem.SQ, groups)); err != nil {
		t.Fatal(err)
	}
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	want, err := ds.RenderFrame(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, body := get(t, backend, heldInstances+"1.2.3.7/frames/2/rendered", "Accept", "image/png")
	if got := decodePNG(t, response, body); !samePicture(got, want) {
		t.Error("frame 2 was not drawn with its own functional group's window")
	}
}

// TestRenderedInstanceOfSeveralFrames checks an instance of three frames:
// a picture a frame in a multipart body, which a request for a single
// picture cannot be given.
func TestRenderedInstanceOfSeveralFrames(t *testing.T) {
	backend, ds := rendering(t)
	window := &dataset.RenderOptions{Window: &dataset.Window{Center: 20, Width: 40}}
	response, body := get(t, backend, heldInstances+"1.2.3.7/rendered?window=20,40,linear",
		"Accept", `multipart/related; type="image/png"`)
	bodies, types, of := frameParts(t, response, body)
	if of != mediaPNG || len(bodies) != 3 {
		t.Fatalf("%d parts of %q", len(bodies), of)
	}
	for i := range bodies {
		got, err := png.Decode(bytes.NewReader(bodies[i]))
		if err != nil || types[i] != mediaPNG {
			t.Fatalf("part %d, %q: %v", i, types[i], err)
		}
		if want, _ := ds.RenderFrame(i, window); !samePicture(got, want) {
			t.Errorf("part %d is not frame %d rendered", i, i+1)
		}
	}
	if response, _ := get(t, backend, heldInstances+"1.2.3.7/rendered", "Accept", "image/png"); response.StatusCode != http.StatusNotAcceptable {
		t.Errorf("three frames as one PNG: status %d, want 406", response.StatusCode)
	}
}

// TestRenderedViewport checks viewport (PS3.18 8.3.5.1.3): the region cut
// from the frame, then scaled to the largest size that fits the box
// without distortion.
func TestRenderedViewport(t *testing.T) {
	backend, ds := rendering(t)
	frame, err := ds.RenderFrame(0, &dataset.RenderOptions{Window: &dataset.Window{Center: 20, Width: 40}})
	if err != nil {
		t.Fatal(err)
	}
	gray := frame.(*goimage.Gray)

	// The region 4,2 to 12,6, eight by four, into a box of 16 by 16: twice
	// as large, sixteen by eight, each pixel doubled.
	response, body := get(t, backend, heldInstances+"1.2.3.7/frames/1/rendered?window=20,40,linear&viewport=16,16,4,2,8,4",
		"Accept", "image/png")
	got := decodePNG(t, response, body)
	if got.Bounds().Dx() != 16 || got.Bounds().Dy() != 8 {
		t.Fatalf("the picture is %v, want 16x8", got.Bounds())
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			r, _, _, _ := got.At(x, y).RGBA()
			if want := gray.GrayAt(4+x/2, 2+y/2).Y; uint8(r>>8) != want {
				t.Fatalf("pixel %d,%d is %d, the region's is %d", x, y, r>>8, want)
			}
		}
	}

	// Into a box of 8 by 4, the whole 16 by 16 frame is 4 by 4.
	response, body = get(t, backend, heldInstances+"1.2.3.7/frames/1/rendered?viewport=8,4", "Accept", "image/png")
	if got := decodePNG(t, response, body); got.Bounds().Dx() != 4 || got.Bounds().Dy() != 4 {
		t.Errorf("into 8x4: %v, want 4x4", got.Bounds())
	}
}

// TestThumbnails checks the thumbnail of an instance, of frames, of a
// series and of a study: one picture each, fitted to the viewport asked
// for or to 128 pixels, never enlarged by default.
func TestThumbnails(t *testing.T) {
	backend, _ := rendering(t)
	for _, path := range []string{
		heldInstances + "1.2.3.7/thumbnail",
		heldInstances + "1.2.3.7/frames/3,1/thumbnail",
		"/studies/1.2.3.100/series/1.2.3.9/thumbnail",
		"/studies/1.2.3.100/thumbnail",
	} {
		response, body := get(t, backend, path, "Accept", "image/png")
		if got := decodePNG(t, response, body); got.Bounds().Dx() != 16 {
			t.Errorf("%s: %v; a 16-pixel frame is not enlarged", path, got.Bounds())
		}
	}
	response, body := get(t, backend, heldInstances+"1.2.3.7/thumbnail?viewport=8,8", "Accept", "image/png")
	if got := decodePNG(t, response, body); got.Bounds().Dx() != 8 {
		t.Errorf("thumbnail into 8x8: %v", got.Bounds())
	}
}

// TestRenderedRefusals checks what is refused, each with the status
// PS3.18 gives: a parameter that is ill formed or asks for what is not
// done, 400; a media type not written, 406; a frame that is not there,
// 404; a study rendered whole, or an instance with nothing to draw, 501.
func TestRenderedRefusals(t *testing.T) {
	backend, _ := rendering(t)
	frame := heldInstances + "1.2.3.7/frames/1/rendered"
	cases := []struct {
		path, accept string
		status       int
	}{
		{frame + "?quality=0", "", http.StatusBadRequest},
		{frame + "?quality=101", "", http.StatusBadRequest},
		{frame + "?quality=high", "", http.StatusBadRequest},
		{frame + "?window=20,40", "", http.StatusBadRequest},
		{frame + "?window=a,40,linear", "", http.StatusBadRequest},
		{frame + "?window=20,0,linear", "", http.StatusBadRequest},
		{frame + "?window=20,40,gamma", "", http.StatusBadRequest},
		{frame + "?viewport=0,10", "", http.StatusBadRequest},
		{frame + "?viewport=10", "", http.StatusBadRequest},
		{frame + "?viewport=10,10,0,0,-8,8", "", http.StatusBadRequest},
		{frame + "?viewport=10,10,12,12,8,8", "", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/thumbnail?viewport=10,10,0,0,8,8", "", http.StatusBadRequest},
		{frame + "?iccprofile=srgb", "", http.StatusBadRequest},
		{frame + "?annotation=everything", "", http.StatusBadRequest},
		{frame + "?quality=50&iccprofile=no", "", http.StatusOK},
		{frame, "image/gif", http.StatusNotAcceptable},
		{frame, `multipart/related; type="image/jp2"`, http.StatusNotAcceptable},
		{heldInstances + "1.2.3.7/frames/4/rendered", "", http.StatusNotFound},
		{heldInstances + "1.2.3.7/frames/0/rendered", "", http.StatusBadRequest},
		{heldInstances + "1.2.3.6/rendered", "", http.StatusNotFound},
		{"/studies/1.2.3.100/rendered", "", http.StatusNotImplemented},
		{"/studies/1.2.3.100/series/1.2.3.9/rendered", "", http.StatusNotImplemented},
		{heldInstances + "1.2.3.1/rendered", "", http.StatusNotImplemented},
	}
	for _, c := range cases {
		if response, body := get(t, backend, c.path, "Accept", c.accept); response.StatusCode != c.status {
			t.Errorf("%s with %q: status %d, want %d: %s", c.path, c.accept, response.StatusCode, c.status, body)
		}
	}
	if response, _ := send(t, backend, http.MethodPost, "/studies/1.2.3.100/thumbnail"); response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to a study's thumbnail: status %d", response.StatusCode)
	}

	// annotation is understood and not drawn: the picture, and a warning
	// that says so (PS3.18 8.3.5.1.1).
	response, _ := get(t, backend, frame+"?annotation=patient,technique")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Warning"), "299 ") {
		t.Errorf("annotation: status %d, Warning %q", response.StatusCode, response.Header.Get("Warning"))
	}
}

// TestRenderedFrameWithoutAWindowSpansTheStoredRange checks a 12-bit image
// with no window, whose three frames hold 0 to 999, 1000 to 1999 and 0 to
// 3999: each is drawn over 0 to 4095, the identity VOI LUT of PS3.3
// C.11.2.1.2.2, so one gray is one value in every frame. Each was drawn
// over its own range, all three the same picture.
func TestRenderedFrameWithoutAWindowSpansTheStoredRange(t *testing.T) {
	ds := multiFrame(t, "1.2.3.7", 3, 1, 3)
	samples := []int{0, 500, 999, 1000, 1500, 1999, 0, 2000, 3999}
	pixels := make([]byte, 2*len(samples))
	for i, v := range samples {
		pixels[2*i], pixels[2*i+1] = byte(v), byte(v>>8)
	}
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0028, 0x0100), dataelem.US, uint16(16)},
		{tag.New(0x0028, 0x0101), dataelem.US, uint16(12)},
		{tag.New(0x0028, 0x0102), dataelem.US, uint16(11)},
		{tag.New(0x7FE0, 0x0010), dataelem.OW, pixels},
	} {
		ds.Remove(e.t)
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	for n, want := range map[string][3]uint8{"1": {0, 31, 62}, "2": {62, 93, 124}, "3": {0, 125, 249}} {
		response, body := get(t, backend, heldInstances+"1.2.3.7/frames/"+n+"/rendered", "Accept", mediaPNG)
		img := decodePNG(t, response, body)
		for x, level := range want {
			if got := img.(*goimage.Gray).GrayAt(x, 0).Y; got != level {
				t.Errorf("frame %s pixel %d is %d, want %d", n, x, got, level)
			}
		}
	}
}
