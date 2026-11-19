package dicomweb

import (
	"bytes"
	"errors"
	raster "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/internal/iccembed"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Rendered images and thumbnails (PS3.18 10.4.1.1.3 and 10.4.1.1.6): a
// frame as a picture a browser shows, through Dataset.RenderFrame, which
// applies the Modality LUT and the VOI window and converts color.
//
//	…/instances/{instance}/rendered           every frame of the instance
//	…/instances/{instance}/frames/{list}/rendered
//	…/instances/{instance}/thumbnail          and frames/{list}/thumbnail,
//	…/series/{series}/thumbnail               series/{series}/thumbnail and
//	/studies/{study}/thumbnail                studies/{study}/thumbnail
//
// A frame is read from the file by its position, as the frames resource
// reads it, and rendered from a data set that holds that frame alone.

// The rendered media types this server writes. JPEG is the one a request
// that says nothing gets.
const (
	mediaJPEG = "image/jpeg"
	mediaPNG  = "image/png"
)

// defaultQuality is the JPEG quality when the request names none (PS3.18
// leaves its meaning to the server).
const defaultQuality = 90

// thumbnailSize is the box a thumbnail is fitted to when the request gives
// no viewport. It is never enlarged to fill it.
const thumbnailSize = 128

// renderTarget is what a rendered or thumbnail resource asks for.
type renderTarget struct {
	thumbnail bool
	media     string
	// bare allows one picture to be sent as itself, multipart allows
	// several in a multipart body.
	bare, multipart bool
	quality         int
	window          *dataset.Window
	viewport        *viewport
	// warnings are for a Warning header: parameters understood and not
	// applied (PS3.18 8.3.5.1.1).
	warnings []string
	// iccProfile is iccprofile=yes, and profile the instance's profile
	// that each picture carries then, nil when there is none to carry.
	iccProfile bool
	profile    []byte
}

// warn adds a warning once.
func (t *renderTarget) warn(text string) {
	if !slices.Contains(t.warnings, text) {
		t.warnings = append(t.warnings, text)
	}
}

// viewport is the viewport query parameter (PS3.18 8.3.5.1.3): the box to
// fit the picture in, and the region of the frame to take, which is the
// whole frame when its width and height are 0.
type viewport struct {
	width, height                        int
	regionX, regionY, regionW, regionH   int
	hasRegion, regionWidth, regionHeight bool
}

// renderTargetOf reads what a rendered or thumbnail request asks for: its
// media type from Accept, and the query parameters of PS3.18 8.3.5.1. A
// parameter that is not well formed, or asks for what this server does not
// do, is 400, as the standard says for each; a media type it does not write
// is 406.
func renderTargetOf(r *http.Request, thumbnail bool) (*renderTarget, *requestError) {
	target := &renderTarget{thumbnail: thumbnail, quality: defaultQuality}
	query := r.URL.Query()

	accept := strings.Join(r.Header.Values("Accept"), ",")
	if parameter := query.Get("accept"); parameter != "" {
		accept = parameter
	}
	if strings.TrimSpace(accept) == "" {
		target.media, target.bare, target.multipart = mediaJPEG, true, true
	}
	for _, entry := range strings.Split(accept, ",") {
		if target.media != "" || strings.TrimSpace(entry) == "" {
			break
		}
		media, parameters, err := mime.ParseMediaType(entry)
		if err != nil {
			continue
		}
		if q, err := strconv.ParseFloat(parameters["q"], 64); err == nil && q == 0 {
			continue
		}
		switch media {
		case "*/*", "image/*":
			target.media, target.bare, target.multipart = mediaJPEG, true, media == "*/*"
		case mediaJPEG, mediaPNG:
			target.media, target.bare = media, true
		case "multipart/related", "multipart/*":
			switch kind := strings.ToLower(parameters["type"]); kind {
			case mediaJPEG, mediaPNG:
				target.media, target.multipart = kind, true
			case "", "image/*":
				target.media, target.multipart = mediaJPEG, true
			}
		}
	}
	if target.media == "" {
		return nil, refuse(http.StatusNotAcceptable,
			"rendered images are %s or %s; the request accepts %q", mediaJPEG, mediaPNG, accept)
	}

	if values, ok := query["quality"]; ok {
		n, err := strconv.Atoi(values[0])
		if err != nil || n < 1 || n > 100 {
			return nil, refuse(http.StatusBadRequest, "quality=%q; it is a whole number from 1 to 100", values[0])
		}
		target.quality = n
	}
	if values, ok := query["window"]; ok {
		window, refused := windowOf(values[0])
		if refused != nil {
			return nil, refused
		}
		target.window = window
	}
	if values, ok := query["viewport"]; ok {
		port, refused := viewportOf(values[0], thumbnail)
		if refused != nil {
			return nil, refused
		}
		target.viewport = port
	}
	if values, ok := query["annotation"]; ok {
		for _, keyword := range strings.Split(values[0], ",") {
			if keyword != "patient" && keyword != "technique" {
				return nil, refuse(http.StatusBadRequest, "annotation=%q; it is patient, technique or both", values[0])
			}
		}
		// Understood, and not drawn: the standard has the server say so
		// and go on (PS3.18 8.3.5.1.1).
		target.warnings = append(target.warnings, "annotation is not applied by this server")
	}
	if values, ok := query["iccprofile"]; ok {
		// PS3.18 8.3.5.1.5. yes is the instance's own profile with the
		// pixels as they are, which PNG (iCCP) and JPEG (APP2) both carry.
		// A named color space means the pixels converted into it, which
		// needs a color management engine this server does not have.
		switch values[0] {
		case "no":
		case "yes":
			target.iccProfile = true
		case "srgb", "adobergb", "rommrgb", "displayp3":
			return nil, refuse(http.StatusBadRequest,
				"iccprofile=%q asks for the picture converted into that color space, which this server does not do; "+
					"iccprofile=yes embeds the instance's own profile, the pixels unchanged", values[0])
		default:
			return nil, refuse(http.StatusBadRequest,
				"iccprofile=%q; it is no, yes, srgb, adobergb, rommrgb or displayp3", values[0])
		}
	}
	return target, nil
}

// windowOf reads window=center,width,function (PS3.18 8.3.5.1.4). All
// three are required.
func windowOf(value string) (*dataset.Window, *requestError) {
	bad := refuse(http.StatusBadRequest,
		"window=%q; it is center,width,function with function linear, linear-exact or sigmoid", value)
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return nil, bad
	}
	center, errCenter := strconv.ParseFloat(parts[0], 64)
	width, errWidth := strconv.ParseFloat(parts[1], 64)
	if errCenter != nil || errWidth != nil || math.IsNaN(center) || math.IsInf(center, 0) ||
		math.IsNaN(width) || width < 1 || math.IsInf(width, 0) {
		return nil, bad
	}
	function := map[string]string{"linear": "LINEAR", "linear-exact": "LINEAR_EXACT", "sigmoid": "SIGMOID"}[parts[2]]
	if function == "" {
		return nil, bad
	}
	return &dataset.Window{Center: center, Width: width, Function: function}, nil
}

// maxViewport bounds a viewport's side, so a request cannot have the server
// draw a picture larger than any screen.
const maxViewport = 8192

// viewportOf reads viewport=vw,vh[,sx,sy,sw,sh] (PS3.18 8.3.5.1.3). A
// thumbnail takes vw,vh alone. A negative sw or sh asks for the region to
// be flipped, which this server does not do, and the standard answers what
// a server does not support with 400.
func viewportOf(value string, thumbnail bool) (*viewport, *requestError) {
	bad := refuse(http.StatusBadRequest, "viewport=%q; it is vw,vh or vw,vh,sx,sy,sw,sh, with vw and vh from 1 to %d"+
		" and the region not flipped", value, maxViewport)
	if thumbnail {
		bad = refuse(http.StatusBadRequest, "viewport=%q; a thumbnail's is vw,vh, from 1 to %d", value, maxViewport)
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 && (thumbnail || len(parts) > 6) {
		return nil, bad
	}
	number := func(i int) (int, bool, bool) {
		if i >= len(parts) || parts[i] == "" {
			return 0, false, true
		}
		n, err := strconv.Atoi(parts[i])
		return n, true, err == nil && n >= 0
	}
	port := &viewport{}
	var ok bool
	if port.width, _, ok = number(0); !ok || port.width < 1 || port.width > maxViewport {
		return nil, bad
	}
	if port.height, _, ok = number(1); !ok || port.height < 1 || port.height > maxViewport {
		return nil, bad
	}
	var given bool
	if port.regionX, given, ok = number(2); !ok {
		return nil, bad
	}
	port.hasRegion = given
	if port.regionY, given, ok = number(3); !ok {
		return nil, bad
	}
	port.hasRegion = port.hasRegion || given
	if port.regionW, port.regionWidth, ok = number(4); !ok {
		return nil, bad
	}
	if port.regionH, port.regionHeight, ok = number(5); !ok {
		return nil, bad
	}
	port.hasRegion = port.hasRegion || port.regionWidth || port.regionHeight
	return port, nil
}

// rendered answers a request for rendered images or a thumbnail.
func (s *Server) rendered(w http.ResponseWriter, r *http.Request, target retrieveTarget) {
	thumbnail := target.render == "thumbnail"
	want, refused := renderTargetOf(r, thumbnail)
	if refused != nil {
		s.fail(w, refused)
		return
	}
	var numbers []int
	if target.frames != "" {
		if numbers, refused = frameList(target.frames); refused != nil {
			s.fail(w, refused)
			return
		}
	}
	if target.instance == "" && !thumbnail {
		// A study or a series rendered is every frame of every instance in
		// it, which this server does not compose.
		s.fail(w, refuse(http.StatusNotImplemented,
			"a study or a series is not rendered by this server; render its instances, or ask for its thumbnail"))
		return
	}

	file, source, err := s.representative(r, target)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer func() { _ = file.Close() }()
	if want.iccProfile {
		profile, err := iccProfileOf(file)
		switch {
		case err != nil:
			want.warn("iccprofile=yes: " + err.Error() + ", so no profile is embedded")
		case profile == nil:
			want.warn("iccprofile=yes: the instance has no ICC Profile, so no profile is embedded")
		}
		want.profile = profile
	}
	for _, n := range numbers {
		if n > source.count() {
			s.fail(w, refuse(http.StatusNotFound, "frame %d does not exist; the instance has %d", n, source.count()))
			return
		}
	}
	switch {
	case thumbnail && len(numbers) > 0:
		// A thumbnail stands for its resource with one picture.
		numbers = numbers[:1]
	case thumbnail:
		numbers = []int{1}
	case len(numbers) == 0:
		for n := 1; n <= source.count(); n++ {
			numbers = append(numbers, n)
		}
	}
	if len(numbers) > 1 && !want.multipart {
		s.fail(w, refuse(http.StatusNotAcceptable,
			"%d frames are rendered as multipart/related; type=%q, which the request does not accept; "+
				"ask for one frame with frames/{n}/rendered", len(numbers), want.media))
		return
	}

	// The first picture is made before anything is written, so one that
	// cannot be rendered can still be answered with a status.
	first, err := s.picture(source, numbers[0], want)
	if err != nil {
		s.fail(w, err)
		return
	}
	for _, warning := range want.warnings {
		w.Header().Add("Warning", `299 `+warningAgent(r)+` "`+warning+`"`)
	}
	if len(numbers) == 1 && want.bare {
		w.Header().Set("Content-Type", want.media)
		w.Header().Set("Content-Length", strconv.Itoa(len(first)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(first)
		return
	}
	body := multipart.NewWriter(w)
	w.Header().Set("Content-Type", `multipart/related; type="`+want.media+`"; boundary=`+body.Boundary())
	w.WriteHeader(http.StatusOK)
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", want.media)
	for i, n := range numbers {
		data := first
		if i > 0 {
			if data, err = s.picture(source, n, want); err != nil {
				panic(http.ErrAbortHandler)
			}
		}
		out, err := body.CreatePart(header)
		if err == nil {
			_, err = out.Write(data)
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := body.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// errFloatNotRendered is an image of Float or Double Float Pixel Data, whose
// values the renderer does not draw: RenderFrame reads integer samples. Its
// frames are served; a picture of it is a function this server lacks (PS3.18
// 8.5).
var errFloatNotRendered = refuse(http.StatusNotImplemented,
	"the instance holds Float or Double Float Pixel Data, which this server does not render")

// representative opens the instance a rendered or thumbnail resource is
// drawn from: the one named, or, for the thumbnail of a study or a series,
// the first of its instances, in the archive's order, that has frames it
// can draw.
func (s *Server) representative(r *http.Request, target retrieveTarget) (io.ReadSeekCloser, *frameSource, error) {
	if target.instance != "" {
		file, err := s.openInstance(r, target)
		if err != nil {
			return nil, nil, err
		}
		source, err := openFrames(file)
		if err != nil {
			_ = file.Close()
			if errors.Is(err, errNoFrames) {
				// Rendering is for images. Anything else, a report or a
				// document, is a SOP Class this server does not render, and
				// that is what 501 is for in DICOM (PS3.18 8.5).
				return nil, nil, refuse(http.StatusNotImplemented, "the instance has no Pixel Data, and is not rendered")
			}
			return nil, nil, err
		}
		if source.float {
			_ = file.Close()
			return nil, nil, errFloatNotRendered
		}
		return file, source, nil
	}
	uids, err := s.backend.Locate(r.Context(), target.study, target.series, "")
	if err != nil {
		return nil, nil, err
	}
	for _, uid := range uids {
		file, err := s.backend.Open(r.Context(), uid)
		if err != nil {
			return nil, nil, err
		}
		source, err := openFrames(file)
		if err == nil && !source.float {
			return file, source, nil
		}
		_ = file.Close()
		if err != nil && !errors.Is(err, errNoFrames) {
			return nil, nil, err
		}
	}
	if len(uids) == 0 {
		return nil, nil, refuse(http.StatusNotFound, "the archive holds no such instances")
	}
	return nil, nil, refuse(http.StatusNotImplemented, "none of the instances has Pixel Data this server draws, so there is nothing to draw")
}

// picture renders one frame, counted from 1, and writes it as the request
// asks: cut to the viewport's region, fitted to its box, and encoded.
func (s *Server) picture(source *frameSource, n int, want *renderTarget) ([]byte, error) {
	ds, err := source.alone(n)
	if err != nil {
		return nil, err
	}
	// With no window asked for and none in the image, the VOI LUT stage is
	// the identity (PS3.3 C.11.2.1.2.2), and the picture spans the range the
	// stored bits can hold. The data set holds this frame alone, so the
	// range of its values would be this frame's, and each frame of an image
	// would be stretched to its own: the same gray a different value in each.
	options := &dataset.RenderOptions{Window: want.window, StoredRange: true}
	img, err := ds.RenderFrame(0, options)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if port := want.viewport; port != nil {
		if port.hasRegion {
			region := raster.Rect(port.regionX, port.regionY, width, height)
			if port.regionWidth {
				region.Max.X = port.regionX + port.regionW
			}
			if port.regionHeight {
				region.Max.Y = port.regionY + port.regionH
			}
			if region.Empty() || !region.In(bounds) {
				return nil, refuse(http.StatusBadRequest,
					"the viewport's region %v is not inside the %dx%d frame", region, width, height)
			}
			img = crop(img, region)
		}
		img = fit(img, port.width, port.height, true)
	} else if want.thumbnail {
		img = fit(img, thumbnailSize, thumbnailSize, false)
	}

	var out bytes.Buffer
	var encoder io.Writer = &out
	if want.profile != nil {
		if err := iccembed.Fits(want.profile, img); err != nil {
			want.warn("iccprofile=yes: " + err.Error() + ", so no profile is embedded")
		} else if want.media == mediaPNG {
			encoder = iccembed.PNG(&out, want.profile)
		} else if encoder, err = iccembed.JPEG(&out, want.profile); err != nil {
			want.warn("iccprofile=yes: " + err.Error() + ", so no profile is embedded")
			encoder = &out
		}
	}
	if want.media == mediaPNG {
		err = png.Encode(encoder, img)
	} else {
		err = jpeg.Encode(encoder, img, &jpeg.Options{Quality: want.quality})
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// iccProfileOf reads an instance's ICC profile (Dataset.ICCProfile): its
// attributes again, this time with every binary value but Pixel Data,
// since the read that found the frames left them in the file.
func iccProfileOf(file io.ReadSeeker) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(file),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
	if err != nil {
		return nil, err
	}
	return parsed.GetDataset().ICCProfile()
}

// The attributes that say how many frames an image has, and which belong
// to which frame.
var (
	tagNumberOfFrames = tag.New(0x0028, 0x0008)
	tagPerFrameGroups = tag.New(0x5200, 0x9230)
)

// alone returns a data set that holds frame n, counted from 1, as its only
// frame: the attributes, that frame's bytes as stored, and, for an enhanced
// image, that frame's functional group alone, so RenderFrame takes its
// rescale and window from it.
//
// The range of the values it holds is this frame's alone, which is why a
// picture of it without a window is drawn over the stored range instead
// (RenderOptions.StoredRange).
func (f *frameSource) alone(n int) (*dataset.Dataset, error) {
	data, err := f.read(n - 1)
	if err != nil {
		return nil, err
	}
	ds := f.attributes.Clone()
	syntax := f.syntax
	value := data
	vr := dataelem.OB
	if compress.IsEncapsulated(syntax) {
		// One fragment after an empty Basic Offset Table, the form the
		// decoders read, padded to an even length as every value is.
		if len(data)%2 == 1 {
			data = append(data, 0)
		}
		var encapsulated bytes.Buffer
		encapsulated.Write([]byte{0xFE, 0xFF, 0x00, 0xE0, 0, 0, 0, 0, 0xFE, 0xFF, 0x00, 0xE0})
		encapsulated.Write([]byte{byte(len(data)), byte(len(data) >> 8), byte(len(data) >> 16), byte(len(data) >> 24)})
		encapsulated.Write(data)
		value = encapsulated.Bytes()
	} else {
		// read has put the samples in little endian.
		syntax = explicitVRLittleEndian
		if info, err := ds.GetPixelDataInfo(); err == nil && info.BitsAllocated > 8 {
			vr = dataelem.OW
		}
	}
	ds.Remove(tagPixelData)
	ds.Remove(tagNumberOfFrames)
	if err := ds.Add(dataelem.NewDataElement(tagPixelData, vr, value)); err != nil {
		return nil, err
	}
	if err := ds.Add(dataelem.NewDataElement(tagNumberOfFrames, dataelem.IS, "1")); err != nil {
		return nil, err
	}
	if element, ok := ds.Get(tagPerFrameGroups); ok {
		if groups, ok := element.GetValue().(*sequence.Sequence); ok && n-1 < groups.Length() {
			item, err := groups.Get(n - 1)
			if err != nil {
				return nil, err
			}
			ds.Remove(tagPerFrameGroups)
			if err := ds.Add(dataelem.NewDataElement(tagPerFrameGroups, dataelem.SQ, sequence.NewFromItems(item))); err != nil {
				return nil, err
			}
		}
	}
	ds.SetTransferSyntaxUID(syntax)
	return ds, nil
}

// crop returns the part of a picture inside region, with its origin at 0,0.
func crop(img raster.Image, region raster.Rectangle) raster.Image {
	out := raster.NewNRGBA(raster.Rect(0, 0, region.Dx(), region.Dy()))
	if gray, ok := img.(*raster.Gray); ok {
		cut := raster.NewGray(out.Rect)
		for y := 0; y < region.Dy(); y++ {
			copy(cut.Pix[y*cut.Stride:], gray.Pix[gray.PixOffset(region.Min.X, region.Min.Y+y):][:region.Dx()])
		}
		return cut
	}
	for y := 0; y < region.Dy(); y++ {
		for x := 0; x < region.Dx(); x++ {
			out.Set(x, y, img.At(region.Min.X+x, region.Min.Y+y))
		}
	}
	return out
}

// fit scales a picture to the largest size that fits a box without being
// cut or distorted (PS3.18 8.3.5.1.3), each pixel the average of the ones
// it covers. It is left as it is when it already fits and enlarge is not
// set.
func fit(img raster.Image, boxWidth, boxHeight int, enlarge bool) raster.Image {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	scale := math.Min(float64(boxWidth)/float64(width), float64(boxHeight)/float64(height))
	if scale == 1 || (scale > 1 && !enlarge) {
		return img
	}
	outW := max(1, int(math.Round(float64(width)*scale)))
	outH := max(1, int(math.Round(float64(height)*scale)))
	_, gray := img.(*raster.Gray)
	var out draw
	if gray {
		out = raster.NewGray(raster.Rect(0, 0, outW, outH))
	} else {
		out = raster.NewNRGBA(raster.Rect(0, 0, outW, outH))
	}
	for y := 0; y < outH; y++ {
		y0 := y * height / outH
		y1 := max(y0+1, (y+1)*height/outH)
		for x := 0; x < outW; x++ {
			x0 := x * width / outW
			x1 := max(x0+1, (x+1)*width/outW)
			var sum [4]uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(img.At(bounds.Min.X+sx, bounds.Min.Y+sy)).(color.NRGBA)
					sum[0] += uint64(c.R)
					sum[1] += uint64(c.G)
					sum[2] += uint64(c.B)
					sum[3] += uint64(c.A)
				}
			}
			count := uint64((y1 - y0) * (x1 - x0))
			out.Set(x, y, color.NRGBA{
				R: uint8((sum[0] + count/2) / count), G: uint8((sum[1] + count/2) / count),
				B: uint8((sum[2] + count/2) / count), A: uint8((sum[3] + count/2) / count),
			})
		}
	}
	return out
}

// draw is a picture that can be drawn on.
type draw interface {
	raster.Image
	Set(x, y int, c color.Color)
}
