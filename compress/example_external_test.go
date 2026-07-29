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

// ExampleExternalDecoderRegistry_RegisterExternalDecoder shows how to supply a
// decoder for a codec this module does not implement.
//
// JPEG-LS, JPEG 2000, and JPEG Lossless have no bundled implementation. Until a
// decoder is registered, reading pixel data in one of those syntaxes reports
// that none is available; once registered, Dataset.PixelArray routes frames
// through it automatically.
func ExampleExternalDecoderRegistry_RegisterExternalDecoder() {
	registry := compress.GetExternalRegistry()

	fmt.Println("before:", registry.IsExternalDecoderAvailable(compress.JPEG_2000))

	if err := registry.RegisterExternalDecoder(compress.JPEG_2000, passthroughDecoder{}); err != nil {
		fmt.Println("register:", err)
		return
	}

	fmt.Println("after: ", registry.IsExternalDecoderAvailable(compress.JPEG_2000))

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

	// Output:
	// before: false
	// after:  true
	// decoded 3 bytes
}
