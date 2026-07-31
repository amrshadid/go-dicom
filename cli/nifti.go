package cli

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// NIfTI-1 export.
//
// What was here before wrote 348 zero bytes with the magic string at the end
// and appended the pixel data element's raw value. Every field a reader needs
// was zero — including sizeof_hdr, which is the first thing any reader checks —
// so nibabel answered "Cannot work out file type" while the command printed
// "Conversion completed successfully!".
//
// The raw value was wrong too. For a compressed instance it is a JPEG or RLE
// stream, so the file would have held a codestream described as an image even
// if the header had been right.
//
// The header below is the NIfTI-1.1 layout of nifti1.h. Its offsets are fixed
// by the format and are written explicitly rather than through a struct, since
// the format's field order and its padding are what matter, not Go's.

const (
	niftiHeaderSize = 348
	// The single file form puts the data straight after the header and a
	// four-byte extension flag, so the offset is 352 and never anything else.
	niftiVoxelOffset = 352
)

// NIfTI-1 datatype codes, nifti1.h.
const (
	niftiUint8   = 2
	niftiInt16   = 4
	niftiInt32   = 8
	niftiFloat32 = 16
	niftiRGB24   = 128
	niftiInt8    = 256
	niftiUint16  = 512
	niftiUint32  = 768
)

// convertToNifti reads a DICOM file and returns it as a NIfTI-1 single file.
//
// It takes the path rather than the flattened elements the other converters
// use, because an image is more than its pixel bytes: the geometry lives in
// attributes those elements have already turned into display strings, and the
// pixels themselves have to be decoded rather than copied.
func convertToNifti(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	ds := df.GetDataset()

	rows := niftiUint16Value(ds, tag.New(0x0028, 0x0010))
	columns := niftiUint16Value(ds, tag.New(0x0028, 0x0011))
	if rows == 0 || columns == 0 {
		return nil, fmt.Errorf("the instance has no image dimensions (rows %d, columns %d), "+
			"so there is nothing to convert", rows, columns)
	}

	samplesPerPixel := niftiUint16Value(ds, tag.New(0x0028, 0x0002))
	if samplesPerPixel == 0 {
		samplesPerPixel = 1
	}
	bitsAllocated := niftiUint16Value(ds, tag.New(0x0028, 0x0100))
	signed := niftiUint16Value(ds, tag.New(0x0028, 0x0103)) == 1

	datatype, bitpix, err := niftiDatatype(bitsAllocated, samplesPerPixel, signed)
	if err != nil {
		return nil, err
	}

	// Decoded, not raw. A compressed instance's stored value is a codestream.
	pixels, err := ds.DecodedPixelData()
	if err != nil {
		return nil, fmt.Errorf("decoding the pixel data: %w", err)
	}

	frames := 1
	if n := niftiIntString(ds, tag.New(0x0028, 0x0008)); n > 0 {
		frames = n
	}

	rowSpacing, columnSpacing := niftiPixelSpacing(ds)
	sliceThickness := float32(niftiFloatString(ds, tag.New(0x0018, 0x0050)))
	if sliceThickness == 0 {
		sliceThickness = 1
	}

	header := make([]byte, niftiHeaderSize)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(header[off:], v) }
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(header[off:], v) }
	putFloat := func(off int, v float32) { put32(off, math.Float32bits(v)) }

	// sizeof_hdr. A reader that finds anything else here decides the file is
	// not NIfTI, or is byte-swapped, before looking at another field.
	put32(0, niftiHeaderSize)

	// dim: dim[0] is how many of the rest are meaningful. A single-frame image
	// is three-dimensional with one slice rather than two-dimensional, so that
	// slice thickness has somewhere to go.
	put16(40, 3)
	put16(42, columns)
	put16(44, rows)
	put16(46, uint16(frames))
	for i := 4; i < 8; i++ {
		put16(40+i*2, 1)
	}

	put16(70, uint16(datatype))
	put16(72, uint16(bitpix))

	// pixdim: pixdim[0] is the qfactor, which is 1 unless the transform is a
	// left-handed one. The rest are the voxel dimensions matching dim.
	putFloat(76, 1)
	putFloat(80, columnSpacing)
	putFloat(84, rowSpacing)
	putFloat(88, sliceThickness)

	putFloat(108, niftiVoxelOffset)

	// scl_slope and scl_inter carry Rescale Slope and Intercept, so a CT keeps
	// its Hounsfield units instead of arriving as stored values. A slope of
	// zero means no scaling, which is not the same as a slope of one, so an
	// absent Rescale Slope is written as one.
	slope := float32(niftiFloatString(ds, tag.New(0x0028, 0x1053)))
	if slope == 0 {
		slope = 1
	}
	putFloat(112, slope)
	putFloat(116, float32(niftiFloatString(ds, tag.New(0x0028, 0x1052))))

	// xyzt_units: millimeters and seconds, which is what DICOM measures in.
	header[123] = 2 | 8

	copy(header[148:228], []byte(niftiDescription(ds)))

	// sform gives the voxel-to-world transform. Only the scaling and the
	// position are filled in: deriving the full rotation needs Image
	// Orientation (Patient), and writing a wrong rotation is worse than
	// writing none, since a reader cannot tell a wrong one from a right one.
	put16(254, 1) // sform_code: scanner anatomical
	putFloat(280, columnSpacing)
	putFloat(296+4, rowSpacing)
	putFloat(312+8, sliceThickness)
	x, y, z := niftiImagePosition(ds)
	putFloat(280+12, x)
	putFloat(296+12, y)
	putFloat(312+12, z)

	copy(header[344:348], []byte("n+1\x00"))

	out := make([]byte, 0, niftiVoxelOffset+len(pixels))
	out = append(out, header...)
	out = append(out, 0, 0, 0, 0) // extension flag: no extensions follow
	out = append(out, pixels...)
	return out, nil
}

// niftiDatatype maps the DICOM description of a sample to a NIfTI code.
func niftiDatatype(bitsAllocated, samplesPerPixel uint16, signed bool) (int, int, error) {
	if samplesPerPixel == 3 {
		if bitsAllocated != 8 {
			return 0, 0, fmt.Errorf("color images are convertible at 8 bits per sample, "+
				"and this one has %d", bitsAllocated)
		}
		return niftiRGB24, 24, nil
	}
	if samplesPerPixel != 1 {
		return 0, 0, fmt.Errorf("an image with %d samples per pixel has no NIfTI datatype",
			samplesPerPixel)
	}

	switch {
	case bitsAllocated == 8 && !signed:
		return niftiUint8, 8, nil
	case bitsAllocated == 8:
		return niftiInt8, 8, nil
	case bitsAllocated == 16 && !signed:
		return niftiUint16, 16, nil
	case bitsAllocated == 16:
		return niftiInt16, 16, nil
	case bitsAllocated == 32 && !signed:
		return niftiUint32, 32, nil
	case bitsAllocated == 32:
		return niftiInt32, 32, nil
	}
	return 0, 0, fmt.Errorf("%d bits per sample has no NIfTI datatype", bitsAllocated)
}

// niftiPixelSpacing reads Pixel Spacing, which is row spacing then column
// spacing — the opposite order from the one the dimensions are written in.
func niftiPixelSpacing(ds *dataset.Dataset) (row, column float32) {
	row, column = 1, 1
	value := niftiString(ds, tag.New(0x0028, 0x0030))
	parts := strings.Split(value, "\\")
	if len(parts) > 0 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 32); err == nil && v != 0 {
			row = float32(v)
		}
	}
	if len(parts) > 1 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 32); err == nil && v != 0 {
			column = float32(v)
		}
	}
	return row, column
}

// niftiImagePosition reads Image Position (Patient), the position of the first
// voxel in the patient coordinate system.
func niftiImagePosition(ds *dataset.Dataset) (x, y, z float32) {
	parts := strings.Split(niftiString(ds, tag.New(0x0020, 0x0032)), "\\")
	out := [3]float32{}
	for i := 0; i < 3 && i < len(parts); i++ {
		if v, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 32); err == nil {
			out[i] = float32(v)
		}
	}
	return out[0], out[1], out[2]
}

// niftiDescription fills the 80-byte descrip field with something identifying.
func niftiDescription(ds *dataset.Dataset) string {
	modality := niftiString(ds, tag.New(0x0008, 0x0060))
	series := niftiString(ds, tag.New(0x0008, 0x103E))
	out := strings.TrimSpace(modality + " " + series)
	if len(out) > 79 {
		out = out[:79]
	}
	return out
}

// niftiString reads a text value without its padding.
func niftiString(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(raw), " \x00")
}

// niftiUint16Value reads a two-byte unsigned value.
func niftiUint16Value(ds *dataset.Dataset, t tag.Tag) uint16 {
	elem, ok := ds.Get(t)
	if !ok {
		return 0
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok || len(raw) < 2 {
		return 0
	}
	return binary.LittleEndian.Uint16(raw)
}

// niftiIntString reads an integer stored as text.
func niftiIntString(ds *dataset.Dataset, t tag.Tag) int {
	n, err := strconv.Atoi(strings.TrimSpace(niftiString(ds, t)))
	if err != nil {
		return 0
	}
	return n
}

// niftiFloatString reads a decimal stored as text.
func niftiFloatString(ds *dataset.Dataset, t tag.Tag) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(niftiString(ds, t)), 64)
	if err != nil {
		return 0
	}
	return v
}
