package dicomweb

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// A frame encoded on request (PS3.18 8.7.3.3.2): when a request names a
// compressed media type, or a transfer syntax of one, that the instance is
// not stored in, the frame is encoded into it with the library's own coders,
// the ones compress and a send use, on a data set holding that frame alone.
//
// A media type named without a syntax gets the default Table 8.7.3-5 gives
// it, and every default is lossless. A lossy syntax is used only when the
// request names it; how much is lost is the server's, in FrameEncoding,
// since PS3.18 gives a request no way to say.

// mediaDefault is the transfer syntax Table 8.7.3-5 marks as the default of
// each compressed media type.
var mediaDefault = map[string]string{
	"image/jpeg": compress.JPEGLossless,
	"image/jls":  compress.JPEGLSLossless,
	"image/jp2":  compress.JPEG2000Lossless,
	"image/jpx":  "1.2.840.10008.1.2.4.92",
	// HTJ2K is decoded and not encoded, so a frame not already held in it
	// is refused as image/jpx is.
	"image/jphc": compress.HTJ2KLossless,
	mediaRLE:     compress.RLELossless,
}

// lossySyntaxes are the syntaxes that may lose detail. A frame stored in one
// is not encoded into another compressed syntax: the copy would carry a loss
// its syntax does not say, or add a second one. They are the lossy syntaxes
// of the library's table of coders, and the ones it reads but does not
// write: JPEG 2000 Part 2, lossy, and High-Throughput JPEG 2000, lossy.
var lossySyntaxes = func() map[string]bool {
	lossy := map[string]bool{
		"1.2.840.10008.1.2.4.93": true,
		compress.HTJ2K:           true,
	}
	for _, e := range dataset.PixelEncoders() {
		if e.Lossy {
			lossy[e.UID] = true
		}
	}
	return lossy
}()

// FrameEncoding is how much a frame encoded on request into a lossy syntax
// may lose. A request names the syntax; the amount is the server's.
type FrameEncoding struct {
	// JPEGQuality is the quality, 1 to 100, of a frame encoded as JPEG
	// Baseline or JPEG Extended.
	JPEGQuality int
	// JPEGLSNear is the error allowed each sample, NEAR of ITU-T T.87, of a
	// frame encoded as JPEG-LS Near-Lossless.
	JPEGLSNear int
	// JPEG2000Rate is the rate a frame encoded as lossy JPEG 2000 is to
	// meet.
	JPEG2000Rate compress.JPEG2000Rate
}

// DefaultFrameEncoding is what a server loses when it is not told: JPEG at
// quality 90, the quality a rendered JPEG has here when the request gives
// none; JPEG-LS within 2 of each sample, dcmtk's dcmcjpls default; and JPEG
// 2000 at a ratio of 10 to 1.
var DefaultFrameEncoding = FrameEncoding{JPEGQuality: 90, JPEGLSNear: 2, JPEG2000Rate: compress.JPEG2000Rate{Ratio: 10}}

// WithFrameEncoding sets how much a frame encoded on request into a lossy
// syntax may lose, in place of DefaultFrameEncoding.
func WithFrameEncoding(encoding FrameEncoding) ServerOption {
	return func(server *Server) {
		server.encoding = encoding
	}
}

// frameEncoder is the coder a frame can be encoded into syntax with: the
// library's one table (dataset.PixelEncoderFor), the coders compress and a
// send use.
func frameEncoder(syntax string) (dataset.PixelEncoder, bool) {
	return dataset.PixelEncoderFor(syntax)
}

// encoderParams are what a lossy coder is asked with: the server's encoding.
func (e FrameEncoding) encoderParams() dataset.EncoderParams {
	return dataset.EncoderParams{JPEGQuality: e.JPEGQuality, JPEGLSNear: e.JPEGLSNear, JPEG2000Rate: e.JPEG2000Rate}
}

// tagLossyImageCompression is Lossy Image Compression (0028,2110), "01"
// when the pixels have been through a lossy coding at some time.
var tagLossyImageCompression = tag.New(0x0028, 0x2110)

// encodeTo returns the syntax a want asks the frames to be encoded into, or
// the reason they cannot be.
func (f *frameSource) encodeTo(want frameWant) (string, error) {
	target := want.syntax
	if target == "" {
		target = mediaDefault[want.media]
	}
	if target == "" || frameMedia[target] != want.media {
		return "", fmt.Errorf("%s is not a transfer syntax of %s", target, want.media)
	}
	if _, ok := frameEncoder(target); !ok {
		return "", fmt.Errorf("this server does not encode %s", target)
	}
	if lossySyntaxes[f.syntax] || strings.TrimSpace(text(f.attributes, tagLossyImageCompression)) == "01" {
		return "", fmt.Errorf("the frames are held lossy, and are not encoded again into %s", target)
	}
	if compress.IsEncapsulated(f.syntax) {
		if _, err := compress.TransferSyntaxToCompressionType(f.syntax); err != nil {
			return "", fmt.Errorf("the frames are held in %s, which this server does not decode", f.syntax)
		}
	}
	return target, nil
}

// encode returns frame n, counted from 1, encoded into syntax: the frame
// alone in a data set, decoded first when it is held compressed, given to
// the syntax's coder, and its one fragment taken out.
func (f *frameSource) encode(n int, syntax string, encoding FrameEncoding) ([]byte, error) {
	ds, err := f.alone(n)
	if err != nil {
		return nil, err
	}
	if compress.IsEncapsulated(ds.TransferSyntaxUID()) {
		if err := ds.DecompressInPlace(); err != nil {
			return nil, err
		}
	}
	// encodeTo has found the coder; one that is not there refuses through
	// EncodeInPlace rather than being called.
	encoder, _ := frameEncoder(syntax)
	if err := encoder.EncodeInPlace(ds, encoding.encoderParams()); err != nil {
		return nil, refuse(http.StatusNotAcceptable, "frame %d cannot be encoded as %s: %v", n, syntax, err)
	}
	return ds.GetEncapsulatedFrame(0)
}
