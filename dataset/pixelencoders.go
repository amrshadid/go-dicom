package dataset

import (
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
)

// Which transfer syntax is written by which coder, once for the library.
//
// A send (network), the compress command (cli) and a frame encoded on a
// DICOMweb request (dicomweb) each kept a table of their own mapping a
// syntax to one of the Compress…InPlace methods below, with which of them
// lose detail and what each takes. Three copies of one fact drift: a coder
// added to one of them is missing from the others until someone notices.
// The table is here because the methods are, so every caller can read it
// without an import cycle. What each caller does with a syntax it does not
// find, or with a parameter it was not given, stays its own: a send refuses
// lossy coding nobody configured, the command asks for a flag, and a server
// uses its own defaults.

// EncoderParam is the one parameter a coder takes. Every lossy coder takes
// one, which says how much may be lost; a lossless coder takes none.
type EncoderParam int

const (
	// EncoderParamNone is a lossless coder's: it takes nothing.
	EncoderParamNone EncoderParam = iota
	// EncoderParamJPEGQuality is the quality, 1 to 100, of JPEG Baseline
	// and JPEG Extended: both scale the same tables, so the number means the
	// same in either.
	EncoderParamJPEGQuality
	// EncoderParamJPEGLSNear is NEAR of ITU-T T.87, the error allowed on
	// each sample, 0 to 255. At 0 nothing is lost.
	EncoderParamJPEGLSNear
	// EncoderParamJPEG2000Rate is the compression ratio or the PSNR lossy
	// JPEG 2000 is to meet, one of them.
	EncoderParamJPEG2000Rate
)

// EncoderParams holds what a lossy coder is asked with. A coder reads only
// the field of its own EncoderParam and ignores the others.
type EncoderParams struct {
	// JPEGQuality is for JPEG Baseline and JPEG Extended.
	JPEGQuality int
	// JPEGLSNear is for JPEG-LS Near-Lossless.
	JPEGLSNear int
	// JPEG2000Rate is for lossy JPEG 2000.
	JPEG2000Rate compress.JPEG2000Rate
}

// PixelEncoder is one transfer syntax this library writes pixel data in,
// and the Compress…InPlace method of Dataset that writes it.
type PixelEncoder struct {
	// UID is the transfer syntax.
	UID string
	// Name is the syntax's name, short, as the library's messages give it.
	Name string
	// Lossy is set for a syntax that may lose detail. JPEG-LS Near-Lossless
	// is one, though at NEAR 0 it loses nothing; Loses says which.
	Lossy bool
	// Param is the parameter the coder takes, EncoderParamNone for a
	// lossless one.
	Param EncoderParam

	encode func(*Dataset, EncoderParams) error
}

// EncodeInPlace codes ds's pixel data in the encoder's syntax, with the
// field of params its Param names, exactly as calling the Compress…InPlace
// method does.
func (e PixelEncoder) EncodeInPlace(ds *Dataset, params EncoderParams) error {
	if e.encode == nil {
		return fmt.Errorf("compress: this library has no coder for %q", e.UID)
	}
	return e.encode(ds, params)
}

// Loses reports whether coding with params loses anything: every lossy
// coder does, but JPEG-LS Near-Lossless with NEAR 0 is lossless, a coding
// the syntax includes.
func (e PixelEncoder) Loses(params EncoderParams) bool {
	if e.Param == EncoderParamJPEGLSNear {
		return params.JPEGLSNear > 0
	}
	return e.Lossy
}

// pixelEncoders is the table, lossless syntaxes first and in the order the
// compress command has always listed them.
var pixelEncoders = []PixelEncoder{
	{rleLosslessSyntax, "RLE Lossless", false, EncoderParamNone,
		func(ds *Dataset, _ EncoderParams) error { return ds.CompressRLELosslessInPlace() }},
	{jpeglsLosslessSyntax, "JPEG-LS Lossless", false, EncoderParamNone,
		func(ds *Dataset, _ EncoderParams) error { return ds.CompressJPEGLSLosslessInPlace() }},
	{compress.JPEGLSNearLossless, "JPEG-LS Near-Lossless", true, EncoderParamJPEGLSNear,
		func(ds *Dataset, p EncoderParams) error { return ds.CompressJPEGLSNearLosslessInPlace(p.JPEGLSNear) }},
	{compress.JPEG2000Lossless, "JPEG 2000 Lossless", false, EncoderParamNone,
		func(ds *Dataset, _ EncoderParams) error { return ds.CompressJPEG2000LosslessInPlace() }},
	{jpeg2000Syntax, "JPEG 2000", true, EncoderParamJPEG2000Rate,
		func(ds *Dataset, p EncoderParams) error { return ds.CompressJPEG2000InPlace(p.JPEG2000Rate) }},
	{compress.JPEGLosslessNonHierarch, "JPEG Lossless", false, EncoderParamNone,
		func(ds *Dataset, _ EncoderParams) error { return ds.CompressJPEGLosslessInPlace() }},
	{compress.JPEGLossless, "JPEG Lossless, Selection Value 1", false, EncoderParamNone,
		func(ds *Dataset, _ EncoderParams) error { return ds.CompressJPEGLosslessSV1InPlace() }},
	{compress.JPEGBaseline, "JPEG Baseline", true, EncoderParamJPEGQuality,
		func(ds *Dataset, p EncoderParams) error { return ds.CompressJPEGBaselineInPlace(p.JPEGQuality) }},
	{compress.JPEGExtended, "JPEG Extended", true, EncoderParamJPEGQuality,
		func(ds *Dataset, p EncoderParams) error { return ds.CompressJPEGExtendedInPlace(p.JPEGQuality) }},
}

// PixelEncoders returns every transfer syntax this library writes pixel data
// in, with its coder. The slice is a copy: changing it changes nothing here.
func PixelEncoders() []PixelEncoder {
	return append([]PixelEncoder(nil), pixelEncoders...)
}

// PixelEncoderFor returns the encoder of a transfer syntax, and false when
// this library does not write pixel data in it. A native syntax is not
// among them: it needs no coder, and the codec writes it.
func PixelEncoderFor(syntax string) (PixelEncoder, bool) {
	for _, e := range pixelEncoders {
		if e.UID == syntax {
			return e, true
		}
	}
	return PixelEncoder{}, false
}
