package compress_test

import (
	"fmt"

	"github.com/amrshadid/go-dicom/compress"
)

// passthroughDecoder stands in for a real codec. A production decoder would
// wrap a JPEG 2000 library; what matters here is the shape it has to satisfy,
// which is the two-method Decompressor interface and nothing more.
type passthroughDecoder struct{}

func (passthroughDecoder) Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty frame")
	}
	// A real decoder turns the compressed frame into raw samples here.
	return data, nil
}

func (passthroughDecoder) CanDecompress(data []byte) bool {
	return len(data) > 0
}

// ExampleExternalDecoderRegistry_RegisterExternalDecoder shows how to put your
// own decoder in place of a bundled one.
//
// Every codec in the registry now has a pure-Go decoder, so registering is no
// longer about filling a gap — it is about substitution: a faster codec, a
// CGO binding to a C library, or one that accepts something this module
// refuses. Whatever is registered last is what Dataset.PixelArray uses.
//
// The three bytes below are not a JPEG 2000 frame and the bundled decoder would
// refuse them. That they decode is the proof that the substitution took effect.
func ExampleExternalDecoderRegistry_RegisterExternalDecoder() {
	registry := compress.GetExternalRegistry()

	fmt.Println("bundled:", registry.IsExternalDecoderAvailable(compress.JPEG_2000))

	if err := registry.RegisterExternalDecoder(compress.JPEG_2000, passthroughDecoder{}); err != nil {
		fmt.Println("register:", err)
		return
	}

	decoder, err := registry.GetExternalDecoder(compress.JPEG_2000)
	if err != nil {
		fmt.Println("get:", err)
		return
	}
	out, err := decoder.Decompress([]byte{0x01, 0x02, 0x03})
	if err != nil {
		fmt.Println("decompress:", err)
		return
	}
	fmt.Println("decoded", len(out), "bytes")

	// Put the bundled decoder back, since the registry is process-wide.
	_ = registry.RegisterExternalDecoder(compress.JPEG_2000, compress.NewJPEG2000Decompressor())

	// Output:
	// bundled: true
	// decoded 3 bytes
}
