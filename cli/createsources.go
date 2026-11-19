package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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

// createFromJSON reads DICOM JSON: one object, or an array of them, as a
// DICOMweb metadata response holds a study. Each object is a data set with the
// reader the DICOMweb client uses (Dataset.FromDICOMJSON). A value held at a
// BulkDataURI is read from the local file it names, within bulkDir (default:
// the directory of the JSON file).
func createFromJSON(path, syntax, bulkDir string) ([]*created, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if bulkDir == "" {
		bulkDir = filepath.Dir(path)
	}
	text := bytes.TrimSpace(data)
	objects := []json.RawMessage{text}
	if len(text) > 0 && text[0] == '[' {
		objects = nil
		if err := json.Unmarshal(text, &objects); err != nil {
			return nil, fmt.Errorf("not DICOM JSON: %w", err)
		}
		if len(objects) == 0 {
			return nil, errors.New("the array holds no objects")
		}
	}
	made := make([]*created, 0, len(objects))
	for i, object := range objects {
		c, err := createObjectFromJSON(object, syntax, bulkDir)
		if err != nil {
			if len(objects) > 1 {
				return nil, fmt.Errorf("object %d of %d: %w", i+1, len(objects), err)
			}
			return nil, err
		}
		made = append(made, c)
	}
	return made, nil
}

// createObjectFromJSON makes the data set of one DICOM JSON object.
func createObjectFromJSON(object json.RawMessage, syntax, bulkDir string) (*created, error) {
	// Numbers are kept as written: a value read as a float64 and written
	// back would not be the digits it came as.
	decoder := json.NewDecoder(bytes.NewReader(object))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("not DICOM JSON: %w", err)
	}
	if _, ok := generic.(map[string]any); !ok {
		return nil, errors.New("not DICOM JSON: it is not an object")
	}
	// The reader records a value held by URI as empty, which is as much as
	// can be known without fetching it; a file written from that would hold
	// an empty Pixel Data and look complete. So each is read here, from a
	// local file, and given to the reader as InlineBinary.
	if n, err := inlineBulkData(generic, "", bulkDir); err != nil {
		return nil, err
	} else if n > 0 {
		if object, err = json.Marshal(generic); err != nil {
			return nil, err
		}
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

// bulkBinaryVRs are the VRs a value may be read from a file for: the ones
// DICOM JSON carries as InlineBinary, and so as bulk data (PS3.18 F.2.7).
var bulkBinaryVRs = map[string]bool{
	"OB": true, "OD": true, "OF": true, "OL": true, "OV": true, "OW": true, "UN": true,
}

// inlineBulkData replaces each element held by BulkDataURI, in v and in every
// item under it, with the bytes of the local file the URI names, as
// InlineBinary. It returns how many it replaced. at is the tag path, for the
// messages.
func inlineBulkData(v any, at, bulkDir string) (int, error) {
	replaced := 0
	switch value := v.(type) {
	case map[string]any:
		if uri, ok := value["BulkDataURI"]; ok {
			vr, _ := value["vr"].(string)
			if !bulkBinaryVRs[vr] {
				return 0, fmt.Errorf("%s is a BulkDataURI of VR %q; only a binary value (OB, OW, OF, OD, OL, OV, UN) is read from a file", at, vr)
			}
			name, _ := uri.(string)
			raw, err := readBulkFile(name, bulkDir)
			if err != nil {
				return 0, fmt.Errorf("%s: BulkDataURI %q: %w", at, name, err)
			}
			delete(value, "BulkDataURI")
			value["InlineBinary"] = base64.StdEncoding.EncodeToString(compress.PadToEven(raw))
			return 1, nil
		}
		for key, member := range value {
			n, err := inlineBulkData(member, strings.TrimPrefix(at+"."+key, "."), bulkDir)
			if err != nil {
				return replaced, err
			}
			replaced += n
		}
	case []any:
		for _, item := range value {
			n, err := inlineBulkData(item, at, bulkDir)
			if err != nil {
				return replaced, err
			}
			replaced += n
		}
	}
	return replaced, nil
}

// within reports whether path is dir or lies under it, by name.
func within(dir, path string) bool {
	absDir, errDir := filepath.Abs(dir)
	absPath, errPath := filepath.Abs(path)
	if errDir != nil || errPath != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readBulkFile reads the file a BulkDataURI names. It is a file: URI or a
// path, which is relative to bulkDir; a URI that reaches the network is
// refused by name, since a command that makes a file does not reach out
// unasked. Whatever the URI says, the file must lie inside bulkDir: the JSON
// may have come from a server, and a path that climbs out of the directory
// would put any file of the machine into the one this command makes.
func readBulkFile(uri, bulkDir string) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("not a URI: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "", "file":
	case "http", "https":
		return nil, errors.New("it is at a network address, and create does not fetch: a command that makes a file does not reach the network unasked; " +
			"fetch it first (go-dicom wado, or curl) and name the file in the BulkDataURI")
	default:
		return nil, fmt.Errorf("a %s: URI is not read; only a file: URI or a path is", u.Scheme)
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return nil, fmt.Errorf("the host %q is not this machine", u.Host)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("a query or a fragment (a byte range) is not read; the whole file is the value")
	}
	name := u.Path
	if name == "" {
		name = u.Opaque // file:relative/path
		if unescaped, err := url.PathUnescape(name); err == nil {
			name = unescaped
		}
	}
	if name == "" {
		return nil, errors.New("it names no file")
	}
	if runtime.GOOS == "windows" && len(name) > 2 && name[0] == '/' && name[2] == ':' {
		name = name[1:] // file:///C:/x
	}
	name = filepath.FromSlash(name)
	if !filepath.IsAbs(name) {
		name = filepath.Join(bulkDir, name)
	}
	// Contained by its name first, so that a path out of the directory is
	// refused as that whether or not the file exists, and then by where it
	// really is, so that a symbolic link in the directory cannot lead out.
	outside := fmt.Errorf("it is outside %s; give -bulk-dir the directory BulkDataURIs may be read from", bulkDir)
	if !within(bulkDir, name) {
		return nil, outside
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(bulkDir)
	if err != nil {
		return nil, fmt.Errorf("-bulk-dir: %w", err)
	}
	if !within(root, resolved) {
		return nil, outside
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("it is not a file")
	}
	if info.Size() > 0xFFFFFFFD {
		return nil, fmt.Errorf("it is %d bytes, more than one value can hold", info.Size())
	}
	return os.ReadFile(resolved)
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
