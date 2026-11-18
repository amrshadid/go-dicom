package compress

import (
	"encoding/binary"
	"fmt"

	"github.com/amrshadid/go-dicom/compress/jpeg2000"
)

// EncodeJPEG2000Lossless encodes one frame as a lossless JPEG 2000
// codestream, which is what goes in an encapsulated pixel data fragment
// under transfer syntax 1.2.840.10008.1.2.4.90.
//
// samples holds the components interleaved by pixel, width*height pixels in
// raster order, each sample one byte when bitsAllocated is 8 and two, little
// endian, when it is 16: the layout native pixel data has. depth is how many
// of those bits a sample uses, and signed whether they are two's complement.
//
// colorTransform codes three components with the reversible component
// transform, which is for red, green and blue: a data set coded so says
// Photometric Interpretation YBR_RCT (PS3.5 8.2.4).
//
// A sample that does not fit depth is an error. Narrowing it would not be
// lossless, and the caller knows whether the bits above depth mean anything.
func EncodeJPEG2000Lossless(samples []byte, width, height, components, bitsAllocated, depth int,
	signed, colorTransform bool) ([]byte, error) {
	img, err := jpeg2000Image(samples, width, height, components, bitsAllocated, depth, signed)
	if err != nil {
		return nil, err
	}
	return jpeg2000.EncodeWithOptions(img, jpeg2000.EncodeOptions{NoComponentTransform: !colorTransform})
}

// JPEG2000Rate is how much a lossy JPEG 2000 encode may lose, given as one of
// two targets. Exactly one is set.
type JPEG2000Rate struct {
	// Ratio is the compression ratio to meet: the samples' size over the
	// codestream's, the codestream being no larger than that allows.
	Ratio float64
	// PSNR is the peak signal-to-noise ratio, in decibels, the decoded image
	// is to have at least.
	PSNR float64
}

// EncodeJPEG2000 encodes one frame as a lossy JPEG 2000 codestream, the
// irreversible 9/7 wavelet and scalar quantization at the rate given, which
// is what goes in an encapsulated pixel data fragment under transfer syntax
// 1.2.840.10008.1.2.4.91.
//
// samples, depth and signed are as EncodeJPEG2000Lossless has them.
// colorTransform codes three components with the irreversible component
// transform, which is for red, green and blue: a data set coded so says
// Photometric Interpretation YBR_ICT (PS3.5 8.2.4).
func EncodeJPEG2000(samples []byte, width, height, components, bitsAllocated, depth int,
	signed, colorTransform bool, rate JPEG2000Rate) ([]byte, error) {
	img, err := jpeg2000Image(samples, width, height, components, bitsAllocated, depth, signed)
	if err != nil {
		return nil, err
	}
	return jpeg2000.EncodeLossy(img, jpeg2000.LossyOptions{
		NoComponentTransform: !colorTransform, Ratio: rate.Ratio, PSNR: rate.PSNR,
	})
}

// jpeg2000Image takes interleaved native samples apart into the planes of a
// jpeg2000.Image, checking the arguments both encoders share.
func jpeg2000Image(samples []byte, width, height, components, bitsAllocated, depth int,
	signed bool) (*jpeg2000.Image, error) {
	if bitsAllocated != 8 && bitsAllocated != 16 {
		return nil, fmt.Errorf("jpeg2000: Bits Allocated is %d; samples of 8 and 16 bits are encoded", bitsAllocated)
	}
	if depth < 1 || depth > bitsAllocated {
		return nil, fmt.Errorf("jpeg2000: a depth of %d bits in samples of %d", depth, bitsAllocated)
	}
	if width < 1 || height < 1 || components < 1 {
		return nil, fmt.Errorf("jpeg2000: an image of %dx%d with %d components", width, height, components)
	}
	bytesPer := bitsAllocated / 8
	if want := int64(width) * int64(height) * int64(components) * int64(bytesPer); int64(len(samples)) != want {
		return nil, fmt.Errorf("jpeg2000: %d bytes of samples, want %d for %dx%d with %d component(s) at %d bits",
			len(samples), want, width, height, components, bitsAllocated)
	}

	img := &jpeg2000.Image{Width: width, Height: height}
	for c := 0; c < components; c++ {
		img.Components = append(img.Components, make([]int32, width*height))
		img.Depths = append(img.Depths, depth)
		img.Signed = append(img.Signed, signed)
	}
	at := 0
	for pixel := 0; pixel < width*height; pixel++ {
		for c := 0; c < components; c++ {
			var v int32
			switch {
			case bytesPer == 1 && signed:
				v = int32(int8(samples[at]))
			case bytesPer == 1:
				v = int32(samples[at])
			case signed:
				v = int32(int16(binary.LittleEndian.Uint16(samples[at:])))
			default:
				v = int32(binary.LittleEndian.Uint16(samples[at:]))
			}
			img.Components[c][pixel] = v
			at += bytesPer
		}
	}
	return img, nil
}

// JPEG2000Depth is the depth to encode samples at: bitsStored when every
// sample fits in it, and bitsAllocated when one does not.
//
// Bits Stored says how many bits of a sample are the pixel. The bits above
// are meant to be empty, and in old files they are not always: overlays were
// once kept there. A lossless coding returns what it was given, so when a
// sample uses those bits they are coded too, and the cost is a codestream a
// little deeper than the image says.
func JPEG2000Depth(samples []byte, bitsAllocated, bitsStored int, signed bool) int {
	if bitsStored <= 0 || bitsStored >= bitsAllocated || (bitsAllocated != 8 && bitsAllocated != 16) {
		return bitsAllocated
	}
	lo, hi := int32(0), int32(1)<<uint(bitsStored)-1
	if signed {
		lo, hi = -(int32(1) << uint(bitsStored-1)), int32(1)<<uint(bitsStored-1)-1
	}
	bytesPer := bitsAllocated / 8
	for at := 0; at+bytesPer <= len(samples); at += bytesPer {
		var v int32
		switch {
		case bytesPer == 1 && signed:
			v = int32(int8(samples[at]))
		case bytesPer == 1:
			v = int32(samples[at])
		case signed:
			v = int32(int16(binary.LittleEndian.Uint16(samples[at:])))
		default:
			v = int32(binary.LittleEndian.Uint16(samples[at:]))
		}
		if v < lo || v > hi {
			return bitsAllocated
		}
	}
	return bitsStored
}
