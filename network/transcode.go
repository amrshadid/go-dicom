package network

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/dataelem"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// compressToRLE re-encodes a data set's pixel data as RLE Lossless.
//
// This is the one direction that can be encoded here. It covers both starting
// points: native pixel data is compressed directly, and pixel data already in
// another compressed syntax is decoded first — which only works for a syntax
// there is a decoder for, and says so plainly when there is not.
func compressToRLE(ds *dataset.Dataset, source, targetSyntax string) (*dataset.Dataset, error) {
	if _, ok := ds.Get(pixelDataTag); !ok {
		// Nothing to encode, so the syntaxes differ only in how the data set is
		// written, which the codec handles.
		return ds, nil
	}

	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, fmt.Errorf("cannot encode pixel data as %s: %w", targetSyntax, err)
	}
	if info.NumberOfFrames < 1 || info.Rows < 1 || info.Columns < 1 {
		return nil, fmt.Errorf("cannot encode pixel data as %s: the image is %dx%d in %d frames",
			targetSyntax, info.Columns, info.Rows, info.NumberOfFrames)
	}

	pixels, err := ds.DecodedPixelData()
	if err != nil {
		return nil, fmt.Errorf("cannot encode a %s instance as %s: its pixel data has to be "+
			"decoded first and %w", source, targetSyntax, err)
	}

	// Encode exactly what the image describes, not what the element happens to
	// hold. Stored pixel data may carry padding past the last sample — DICOM
	// pads values to an even length, and writers have been known to leave more —
	// and compressing it produces a frame that decodes longer than the image.
	// Our own decoder tolerates the extra byte; pydicom's does not, and neither
	// should it.
	frameLen := info.Rows * info.Columns * info.SamplesPerPixel * info.BitsAllocated / 8
	if frameLen <= 0 {
		return nil, fmt.Errorf("cannot encode pixel data as %s: a frame of %dx%d with %d samples "+
			"at %d bits has no size", targetSyntax, info.Columns, info.Rows,
			info.SamplesPerPixel, info.BitsAllocated)
	}
	if need := frameLen * info.NumberOfFrames; len(pixels) < need {
		return nil, fmt.Errorf("cannot encode pixel data as %s: %d bytes for %d frames of %d",
			targetSyntax, len(pixels), info.NumberOfFrames, frameLen)
	}

	encoder := compress.NewRLECompressor()
	frames := make([][]byte, 0, info.NumberOfFrames)
	for i := 0; i < info.NumberOfFrames; i++ {
		frame, err := encoder.CompressFrame(
			pixels[i*frameLen:(i+1)*frameLen], info.SamplesPerPixel, info.BitsAllocated)
		if err != nil {
			return nil, fmt.Errorf("encoding frame %d as %s: %w", i, targetSyntax, err)
		}
		frames = append(frames, frame)
	}

	// A copy, so sending a data set does not rewrite the caller's.
	out := ds.Clone()
	// Encapsulated pixel data is OB whatever the sample width — PS3.5 A.4, and
	// dcmtk says so plainly when it is not.
	if err := replacePixelData(out, dataelem.OB, encapsulate(frames)); err != nil {
		return nil, fmt.Errorf("replacing pixel data with its encoded form: %w", err)
	}
	out.SetTransferSyntaxUID(targetSyntax)
	return out, nil
}

// replacePixelData swaps the value and VR of (7FE0,0010) without moving it.
//
// Removing the element and adding it back would be simpler and is what this did
// first, but it appends: the element lands at the end of the data set, after
// anything that already followed it. Data Set Trailing Padding (FFFC,FFFC) is
// the case that shows it — pixel data written after that leaves the tags
// descending, which dcmtk reports and a strict reader may refuse.
func replacePixelData(ds *dataset.Dataset, vr dataelem.VR, value []byte) error {
	elem, ok := ds.Get(pixelDataTag)
	if !ok {
		return fmt.Errorf("the data set has no pixel data to replace")
	}
	elem.SetVR(vr)
	return ds.SetValue(pixelDataTag, value)
}

// encapsulate wraps compressed frames in the fragment structure PS3.5 A.4
// requires: an offset table item, then one item per frame.
//
// The offset table is written empty. It is optional, and a reader that wants
// random access to frames can build one; getting it wrong is worse than omitting
// it, because a reader that trusts it seeks to the wrong place.
func encapsulate(frames [][]byte) []byte {
	var out []byte

	item := func(payload []byte) {
		header := make([]byte, 8)
		binary.LittleEndian.PutUint16(header[0:], 0xFFFE)
		binary.LittleEndian.PutUint16(header[2:], 0xE000)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(payload)))
		out = append(out, header...)
		out = append(out, payload...)
	}

	item(nil)
	for _, frame := range frames {
		// Fragments are of even length, like every other DICOM value.
		if len(frame)%2 == 1 {
			frame = append(append([]byte(nil), frame...), 0x00)
		}
		item(frame)
	}
	return out
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

	if !sourceCompressed && !targetCompressed {
		// Both uncompressed: the codec handles byte order and deflating on its
		// own, so the data set travels as it is.
		return ds, nil
	}

	if targetCompressed {
		if targetSyntax != RLELosslessUID {
			// RLE is the only syntax this library encodes. Decoding and
			// re-encoding to JPEG or JPEG 2000 would need an encoder for those.
			return nil, fmt.Errorf("cannot encode pixel data as %s: RLE Lossless is the only "+
				"compressed syntax this library writes", targetSyntax)
		}
		return compressToRLE(ds, source, targetSyntax)
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
	// Native pixel data is OW when a sample is wider than a byte and OB when it
	// is not.
	vr := dataelem.OB
	if info, err := ds.GetPixelDataInfo(); err == nil && info.BitsAllocated > 8 {
		vr = dataelem.OW
	}
	if err := replacePixelData(out, vr, pixels); err != nil {
		return nil, fmt.Errorf("replacing pixel data with its decoded form: %w", err)
	}
	out.SetTransferSyntaxUID(targetSyntax)

	return out, nil
}
