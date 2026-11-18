package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/valuerep"
)

// SOP classes create writes, and the syntaxes it writes them in.
const (
	secondaryCaptureClass = "1.2.840.10008.5.1.4.1.1.7"
	encapsulatedPDFClass  = "1.2.840.10008.5.1.4.1.1.104.1"
	explicitLittleEndian  = "1.2.840.10008.1.2.1"
	jpegBaselineSyntax    = "1.2.840.10008.1.2.4.50"
)

// createFromJSON reads a DICOM JSON object, or an array of one, with the
// reader the DICOMweb client uses (Dataset.FromDICOMJSON).
func createFromJSON(path, syntax string) (*created, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	object := bytes.TrimSpace(data)
	if len(object) > 0 && object[0] == '[' {
		var objects []json.RawMessage
		if err := json.Unmarshal(object, &objects); err != nil {
			return nil, fmt.Errorf("not DICOM JSON: %w", err)
		}
		if len(objects) != 1 {
			return nil, fmt.Errorf("the array holds %d objects; one file is made from one", len(objects))
		}
		object = objects[0]
	}
	var generic any
	if err := json.Unmarshal(object, &generic); err != nil {
		return nil, fmt.Errorf("not DICOM JSON: %w", err)
	}
	if _, ok := generic.(map[string]any); !ok {
		return nil, errors.New("not DICOM JSON: it is not an object")
	}
	// The reader records a value held by URI as empty, which is as much as
	// can be known without fetching it; a file written from that would hold
	// an empty Pixel Data and look complete.
	if at := bulkDataURI(generic, ""); at != "" {
		return nil, fmt.Errorf("%s is held at a BulkDataURI, which is not fetched; give it as InlineBinary", at)
	}
	ds := dataset.NewDataset()
	if err := ds.FromDICOMJSONString(string(object)); err != nil {
		return nil, err
	}
	if syntax == "" {
		syntax = explicitLittleEndian
	} else if err := valuerep.ValidateUID(syntax); err != nil {
		return nil, fmt.Errorf("-syntax %q is not a UID", syntax)
	}
	// What the pixel data holds has to be what the file says it is: an
	// encapsulated value under a native syntax, or the other way round, is
	// read as something it is not.
	if raw := rawBytes(ds, tag.New(0x7FE0, 0x0010)); len(raw) >= 4 {
		fragments := binary.LittleEndian.Uint32(raw) == 0xE000FFFE
		if fragments != compress.IsEncapsulated(syntax) {
			if fragments {
				return nil, errors.New("its Pixel Data is encapsulated fragments; give -syntax with the transfer syntax they are in")
			}
			return nil, fmt.Errorf("its Pixel Data is native and -syntax %s is a compressed syntax", syntax)
		}
	}
	ds.SetTransferSyntaxUID(syntax)
	return &created{ds: ds, syntax: syntax, what: "from DICOM JSON"}, nil
}

// bulkDataURI returns the tag path of the first element held by URI.
func bulkDataURI(v any, at string) string {
	switch value := v.(type) {
	case map[string]any:
		if _, ok := value["BulkDataURI"]; ok {
			return at
		}
		for key, member := range value {
			if found := bulkDataURI(member, strings.TrimPrefix(at+"."+key, ".")); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range value {
			if found := bulkDataURI(item, at); found != "" {
				return found
			}
		}
	}
	return ""
}

func rawBytes(ds *dataset.Dataset, t tag.Tag) []byte {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	b, _ := elem.GetValue().([]byte)
	return b
}

// createFromPicture makes a Secondary Capture image (PS3.3 A.8.1).
func createFromPicture(path string) (*created, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ds := dataset.NewDataset()
	setText(ds, sopClassUIDTag, dataelem.UI, secondaryCaptureClass)
	setText(ds, tag.New(0x0008, 0x0008), dataelem.CS, `DERIVED\SECONDARY`)
	setText(ds, tag.New(0x0020, 0x0011), dataelem.IS, "1") // Series Number
	setText(ds, tag.New(0x0020, 0x0013), dataelem.IS, "1") // Instance Number
	// General Image, Patient Orientation, Type 2C: a picture has none to
	// give, and an empty value says so (PS3.3 C.7.6.1).
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0020), dataelem.CS, []byte{}))

	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		return pictureFromJPEG(ds, data)
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return pictureFromPNG(ds, data)
	}
	return nil, errors.New("neither a PNG nor a JPEG")
}

// pictureFromPNG decodes the PNG and stores its samples native.
func pictureFromPNG(ds *dataset.Dataset, data []byte) (*created, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if opaque, ok := img.(interface{ Opaque() bool }); ok && !opaque.Opaque() {
		return nil, errors.New("it has transparent pixels, and DICOM has no transparency; flatten it first")
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 || width > 0xFFFF || height > 0xFFFF {
		return nil, fmt.Errorf("a picture of %dx%d does not fit Rows and Columns", width, height)
	}

	samples, bits := 3, 8
	switch img.(type) {
	case *image.Gray:
		samples = 1
	case *image.Gray16:
		samples, bits = 1, 16
	case *image.RGBA64, *image.NRGBA64:
		bits = 16
	}
	pixels := make([]byte, 0, width*height*samples*bits/8)
	put := func(v uint32) { // v is 16-bit, as color.Color gives it
		if bits == 8 {
			pixels = append(pixels, byte(v>>8))
		} else {
			pixels = binary.LittleEndian.AppendUint16(pixels, uint16(v))
		}
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if samples == 1 {
				g := color.Gray16Model.Convert(img.At(x, y)).(color.Gray16)
				put(uint32(g.Y))
				continue
			}
			// Opaque, so premultiplied and straight are the same.
			r, g, b, _ := img.At(x, y).RGBA()
			put(r)
			put(g)
			put(b)
		}
	}
	photometric := "MONOCHROME2"
	if samples == 3 {
		photometric = "RGB"
	}
	describeImage(ds, width, height, samples, bits, photometric)
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), pixelVR(bits), compress.PadToEven(pixels)))
	ds.SetTransferSyntaxUID(explicitLittleEndian)
	return &created{ds: ds, syntax: explicitLittleEndian, modality: "OT", what: "a Secondary Capture image from a PNG"}, nil
}

func pixelVR(bits int) dataelem.VR {
	if bits > 8 {
		return dataelem.OW
	}
	return dataelem.OB
}

// pictureFromJPEG puts the JPEG in the file as it is. A JPEG Baseline file is
// a baseline JPEG bitstream in a fragment, so the picture needs no decoding
// and loses nothing more; dcmtk's img2dcm does the same.
func pictureFromJPEG(ds *dataset.Dataset, data []byte) (*created, error) {
	frame, err := scanJPEG(data)
	if err != nil {
		return nil, err
	}
	describeImage(ds, frame.width, frame.height, frame.components, 8, frame.photometric)
	encapsulated, err := compress.EncapsulateFrames([][]byte{data}, 1, true)
	if err != nil {
		return nil, err
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OB, encapsulated))
	// A JPEG picture was lossily compressed before it got here, and once an
	// image has been it says so (PS3.3 C.7.6.1.1.5).
	setText(ds, tag.New(0x0028, 0x2110), dataelem.CS, "01")
	setText(ds, tag.New(0x0028, 0x2114), dataelem.CS, "ISO_10918_1")
	ds.SetTransferSyntaxUID(jpegBaselineSyntax)
	return &created{ds: ds, syntax: jpegBaselineSyntax, modality: "OT", what: "a Secondary Capture image from a JPEG"}, nil
}

// describeImage writes the Image Pixel module (PS3.3 C.7.6.3).
func describeImage(ds *dataset.Dataset, width, height, samples, bits int, photometric string) {
	us := func(element uint16, v int) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
	}
	us(0x0002, samples)
	setText(ds, tag.New(0x0028, 0x0004), dataelem.CS, photometric)
	if samples > 1 {
		us(0x0006, 0) // Planar Configuration: by pixel
	}
	us(0x0010, height)
	us(0x0011, width)
	us(0x0100, bits)
	us(0x0101, bits)
	us(0x0102, bits-1)
	us(0x0103, 0)
}

// jpegFrame is what a JPEG's frame header and markers say about the image.
type jpegFrame struct {
	width, height, components int
	photometric               string
}

// scanJPEG reads the markers before the first scan: the frame header, and
// the Adobe segment that says whether three components are RGB.
func scanJPEG(data []byte) (jpegFrame, error) {
	var frame jpegFrame
	adobeTransform := -1
	sampling := []byte{}
	ids := []byte{}
	for at := 2; at+4 <= len(data); {
		if data[at] != 0xFF {
			return frame, fmt.Errorf("not a JPEG marker at byte %d", at)
		}
		marker := data[at+1]
		if marker == 0xFF { // fill byte
			at++
			continue
		}
		length := int(binary.BigEndian.Uint16(data[at+2:]))
		if length < 2 || at+2+length > len(data) {
			return frame, fmt.Errorf("a marker segment at byte %d runs past the end", at)
		}
		segment := data[at+4 : at+2+length]
		switch {
		case marker == 0xEE && len(segment) >= 12 && string(segment[:5]) == "Adobe":
			adobeTransform = int(segment[11])
		case marker == 0xC0:
			if len(segment) < 6 || segment[0] != 8 {
				return frame, errors.New("a baseline JPEG has 8-bit samples, and this one does not")
			}
			frame.height = int(binary.BigEndian.Uint16(segment[1:]))
			frame.width = int(binary.BigEndian.Uint16(segment[3:]))
			frame.components = int(segment[5])
			for c := 0; c < frame.components && 6+3*c+2 < len(segment); c++ {
				ids = append(ids, segment[6+3*c])
				sampling = append(sampling, segment[6+3*c+1])
			}
		case marker >= 0xC1 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
			kind := map[byte]string{0xC1: "extended sequential", 0xC2: "progressive", 0xC3: "lossless"}[marker]
			if kind == "" {
				kind = "arithmetic or hierarchical"
			}
			return frame, fmt.Errorf("a %s JPEG (SOF%d); JPEG Baseline holds a baseline JPEG, so save it as one or as a PNG", kind, marker-0xC0)
		case marker == 0xDA:
			if frame.components == 0 {
				return frame, errors.New("the JPEG has no frame header before its scan")
			}
			if frame.width == 0 || frame.height == 0 {
				return frame, errors.New("the JPEG gives its height in a DNL marker, which is not read here")
			}
			switch frame.components {
			case 1:
				frame.photometric = "MONOCHROME2"
			case 3:
				frame.photometric = jpegColor(adobeTransform, ids, sampling)
			default:
				return frame, fmt.Errorf("a JPEG of %d components; a picture is gray or three-component color", frame.components)
			}
			return frame, nil
		}
		at += 2 + length
	}
	return frame, errors.New("the JPEG has no scan")
}

// jpegColor names three components as PS3.5 8.2.1 has JPEG Baseline describe
// them: RGB when the Adobe segment says no transform was applied, or the
// components are named R, G and B; otherwise YCbCr, which is YBR_FULL_422
// when the chroma is subsampled and YBR_FULL when it is not.
func jpegColor(adobeTransform int, ids, sampling []byte) string {
	if adobeTransform == 0 || string(ids) == "RGB" {
		return "RGB"
	}
	for _, hv := range sampling {
		if hv != sampling[0] {
			return "YBR_FULL_422"
		}
	}
	return "YBR_FULL"
}

// createFromPDF makes an Encapsulated PDF (PS3.3 A.45.1, C.24.2).
func createFromPDF(path, title, burnedIn string) (*created, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("not a PDF: it does not begin with %PDF-")
	}
	if uint64(len(data)) >= 0xFFFFFFFE {
		return nil, fmt.Errorf("%d bytes is more than an element holds", len(data))
	}
	burnedIn = strings.ToUpper(strings.TrimSpace(burnedIn))
	if burnedIn != "YES" && burnedIn != "NO" {
		return nil, fmt.Errorf("-burned-in %q: yes or no", burnedIn)
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if err := valuerep.ValidateValue("ST", title); err != nil {
		return nil, fmt.Errorf("-title: %v", err)
	}
	ds := dataset.NewDataset()
	setText(ds, sopClassUIDTag, dataelem.UI, encapsulatedPDFClass)
	setText(ds, tag.New(0x0020, 0x0011), dataelem.IS, "1")                                    // Series Number, Type 1 here
	setText(ds, tag.New(0x0020, 0x0013), dataelem.IS, "1")                                    // Instance Number, Type 1
	setText(ds, tag.New(0x0028, 0x0301), dataelem.CS, burnedIn)                               // Burned In Annotation, Type 1
	setText(ds, tag.New(0x0042, 0x0010), dataelem.ST, title)                                  // Document Title
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x002A), dataelem.DT, []byte{}))       // Acquisition DateTime, Type 2
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0040, 0xA043), dataelem.SQ, sequence.New())) // Concept Name Code Sequence, Type 2
	setText(ds, tag.New(0x0042, 0x0012), dataelem.LO, "application/pdf")
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0042, 0x0011), dataelem.OB, compress.PadToEven(data)))
	// The value is padded to an even length; the document's own length is
	// what a reader cuts it back to (PS3.3 C.24.2).
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, uint32(len(data)))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0042, 0x0015), dataelem.UL, length))
	ds.SetTransferSyntaxUID(explicitLittleEndian)
	return &created{ds: ds, syntax: explicitLittleEndian, modality: "DOC", what: "an Encapsulated PDF"}, nil
}
