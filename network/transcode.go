package network

import (
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

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
func transcodePixelData(ds *dataset.Dataset, targetSyntax string) (*dataset.Dataset, error) {
	if ds == nil {
		return ds, nil
	}

	source := ds.TransferSyntaxUID()
	if source == "" || source == targetSyntax {
		// Nothing recorded, or nothing to change. A data set assembled in memory
		// has no syntax of its own and is encoded in whatever was negotiated.
		return ds, nil
	}

	sourceCompressed := compress.IsCompressed(source)
	targetCompressed := compress.IsCompressed(targetSyntax)

	if !sourceCompressed {
		if !targetCompressed {
			// Both uncompressed: the codec handles byte order and deflating on
			// its own, so the data set travels as it is.
			return ds, nil
		}
		return nil, fmt.Errorf("cannot send a data set stored as %s over a context that "+
			"negotiated %s: this library compresses no pixel data, so the instance would "+
			"arrive described as compressed while holding native pixels",
			source, targetSyntax)
	}

	if targetCompressed {
		// Compressed to a different compressed syntax would mean decoding and
		// re-encoding, and there is no encoder for these.
		return nil, fmt.Errorf("cannot re-encode pixel data from %s to %s", source, targetSyntax)
	}

	// Compressed to uncompressed: decode.
	if _, ok := ds.Get(pixelDataTag); !ok {
		// No pixel data at all, so the syntaxes differ only in how the data set
		// itself is encoded, which the codec handles.
		return ds, nil
	}

	pixels, err := ds.DecodedPixelData()
	if err != nil {
		return nil, fmt.Errorf("cannot send a %s instance over a context that negotiated %s: "+
			"its pixel data has to be decoded first and %w", source, targetSyntax, err)
	}

	// A copy, so a caller's data set is not mutated by the act of sending it —
	// which would matter to anything holding it, and to a C-MOVE sending the
	// same instance to more than one destination.
	out := ds.Clone()

	// DICOM values are of even length; native pixel data of odd length is padded.
	if len(pixels)%2 == 1 {
		pixels = append(pixels, 0x00)
	}
	out.Remove(pixelDataTag)
	if err := out.Add(dataelem.NewDataElement(pixelDataTag, dataelem.OB, pixels)); err != nil {
		return nil, fmt.Errorf("replacing pixel data with its decoded form: %w", err)
	}
	out.SetTransferSyntaxUID(targetSyntax)

	return out, nil
}
