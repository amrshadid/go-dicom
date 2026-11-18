package network

import (
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// compressCopy encodes a copy of a data set's pixel data with one of the data
// set's own lossless coders, so that sending does not rewrite the caller's
// data set.
//
// The coders are the data set's, not a second set here: a file and a send
// are encoded by the same code. They were here first, and a file could not
// be written in RLE or JPEG-LS at all (#363). A data set with no pixel data
// differs between the syntaxes only in how it is written, which the codec
// handles.
func compressCopy(ds *dataset.Dataset, source, targetSyntax string, encode func(*dataset.Dataset) error) (*dataset.Dataset, error) {
	if _, ok := ds.Get(pixelDataTag); !ok {
		return ds, nil
	}
	out := ds.Clone()
	if err := encode(out); err != nil {
		return nil, fmt.Errorf("cannot encode a %s instance as %s: %w", source, targetSyntax, err)
	}
	return out, nil
}

// pixelDataTag is (7FE0,0010).
var pixelDataTag = tag.New(0x7FE0, 0x0010)

// transcodePixelData prepares a data set to be sent on a presentation context
// that negotiated targetSyntax, decompressing its pixel data if it has to.
//
// The problem it solves is quiet and serious. A data set read from a
// JPEG-compressed file carries encapsulated fragments in (7FE0,0010). Sent over
// a context that negotiated an uncompressed syntax, those bytes arrive labeled
// as pixels, and the receiver has no way to know they are not — it renders
// whatever they happen to look like. Nothing on either side reports a problem.
//
// So the rule is: never send compressed bytes described as uncompressed. Either
// they are decoded first, or the send fails. Failing is not the worse outcome
// here, because the alternative is an image that is wrong without looking wrong.
//
// Returns the data set to encode, which is the original when nothing is needed.
func transcodePixelData(ds *dataset.Dataset, targetSyntax string, config NetworkConfig) (*dataset.Dataset, error) {
	if ds == nil {
		return ds, nil
	}

	source := ds.TransferSyntaxUID()
	if source == "" || source == targetSyntax {
		// Nothing recorded, or nothing to change. A data set assembled in memory
		// has no syntax of its own and is encoded in whatever was negotiated.
		return ds, nil
	}

	// Encapsulated, not compressed: Deflated compresses the data set and leaves
	// Pixel Data native, so it needs no pixel encoder or decoder (#132).
	sourceCompressed := compress.IsEncapsulated(source)
	targetCompressed := compress.IsEncapsulated(targetSyntax)

	if !sourceCompressed && !targetCompressed {
		// Both native: the codec handles byte order and deflating on its own, so
		// the data set travels as it is.
		return ds, nil
	}

	if targetCompressed {
		switch targetSyntax {
		case RLELosslessUID:
			return compressCopy(ds, source, targetSyntax, (*dataset.Dataset).CompressRLELosslessInPlace)
		case JPEGLSLosslessUID:
			return compressCopy(ds, source, targetSyntax, (*dataset.Dataset).CompressJPEGLSLosslessInPlace)
		case JPEG2000LosslessUID:
			return compressCopy(ds, source, targetSyntax, (*dataset.Dataset).CompressJPEG2000LosslessInPlace)
		case JPEGLosslessProcess14UID:
			return compressCopy(ds, source, targetSyntax, (*dataset.Dataset).CompressJPEGLosslessInPlace)
		case JPEGLosslessSV1UID:
			return compressCopy(ds, source, targetSyntax, (*dataset.Dataset).CompressJPEGLosslessSV1InPlace)
		case JPEGLSNearLosslessUID:
			// The error allowed is the sender's (NetworkConfig.JPEGLSNear),
			// and with none allowed the image is coded losslessly, which this
			// syntax includes. A copy: sending must not make the caller's
			// image lossy.
			return compressCopy(ds, source, targetSyntax, func(out *dataset.Dataset) error {
				return out.CompressJPEGLSNearLosslessInPlace(config.JPEGLSNear)
			})
		case JPEG2000UID:
			// Lossy, so only at a rate the sender has given.
			if config.JPEG2000Rate == (compress.JPEG2000Rate{}) {
				return nil, fmt.Errorf("cannot encode pixel data as %s: lossy JPEG 2000 loses detail and "+
					"nothing is sent that way unasked; set NetworkConfig.JPEG2000Rate to allow it",
					targetSyntax)
			}
			// A copy: sending must not make the caller's image lossy.
			return compressCopy(ds, source, targetSyntax, func(out *dataset.Dataset) error {
				return out.CompressJPEG2000InPlace(config.JPEG2000Rate)
			})
		case JPEGBaselineUID:
			// Lossy, so only for a sender that has said how much loss it
			// accepts: that decision is not one to make while transcoding.
			if config.JPEGQuality <= 0 {
				return nil, fmt.Errorf("cannot encode pixel data as %s: JPEG Baseline is lossy and "+
					"nothing is sent that way unasked; set NetworkConfig.JPEGQuality to allow it",
					targetSyntax)
			}
			// A copy: sending must not make the caller's image lossy.
			return compressCopy(ds, source, targetSyntax, func(out *dataset.Dataset) error {
				return out.CompressJPEGBaselineInPlace(config.JPEGQuality)
			})
		case JPEGExtendedUID:
			// The same quality as JPEG Baseline: both scale the same tables,
			// so the number means the same in either, and a sender who has
			// accepted that much loss for one has accepted it for the other.
			if config.JPEGQuality <= 0 {
				return nil, fmt.Errorf("cannot encode pixel data as %s: JPEG Extended is lossy and "+
					"nothing is sent that way unasked; set NetworkConfig.JPEGQuality to allow it",
					targetSyntax)
			}
			// A copy: sending must not make the caller's image lossy.
			return compressCopy(ds, source, targetSyntax, func(out *dataset.Dataset) error {
				return out.CompressJPEGExtendedInPlace(config.JPEGQuality)
			})
		default:
			// A send that cannot be satisfied fails rather than putting bytes on
			// the wire described as something they are not, which the receiver
			// could not detect.
			return nil, fmt.Errorf("cannot encode pixel data as %s: this library writes "+
				"RLE Lossless (%s), JPEG-LS Lossless (%s), JPEG-LS Near-Lossless (%s), "+
				"JPEG 2000 Lossless (%s) and JPEG Lossless (%s, %s), and JPEG Baseline (%s) and "+
				"JPEG Extended (%s) when NetworkConfig.JPEGQuality is set, and JPEG 2000 (%s) when "+
				"NetworkConfig.JPEG2000Rate is",
				targetSyntax, RLELosslessUID, JPEGLSLosslessUID, JPEGLSNearLosslessUID,
				JPEG2000LosslessUID, JPEGLosslessProcess14UID, JPEGLosslessSV1UID, JPEGBaselineUID,
				JPEGExtendedUID, JPEG2000UID)
		}
	}

	// Compressed to uncompressed: decode.
	if _, ok := ds.Get(pixelDataTag); !ok {
		// No pixel data at all, so the syntaxes differ only in how the data set
		// itself is encoded, which the codec handles.
		return ds, nil
	}

	// A copy, so a caller's data set is not mutated by the act of sending it —
	// which would matter to anything holding it, and to a C-MOVE sending the
	// same instance to more than one destination.
	out := ds.Clone()

	// DecompressInPlace, not the bytes alone: decoding changes the layout as
	// well. Samples come out by pixel, a 4:2:2 JPEG comes out with chroma for
	// every pixel, and a JPEG 2000 color transform is undone. Sent still
	// described as stored, a 4:2:2 image was read by the receiver two bytes a
	// pixel where it held three, and the send reported success (#246).
	if err := out.DecompressInPlace(); err != nil {
		return nil, fmt.Errorf("cannot send a %s instance over a context that negotiated %s: "+
			"its pixel data has to be decoded first and %w", source, targetSyntax, err)
	}
	out.SetTransferSyntaxUID(targetSyntax)

	return out, nil
}
