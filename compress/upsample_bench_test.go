package compress_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// colorJPEG420 is a 512x512 color picture coded by the standard library's
// encoder, which subsamples chroma 4:2:0, the case the chroma filter is for.
func colorJPEG420(b *testing.B) []byte {
	b.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8((x ^ y) * 3), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkJPEGDecompress420 decodes a 4:2:0 color frame to YCbCr samples, as
// a data set described as YBR_FULL is decoded.
func BenchmarkJPEGDecompress420(b *testing.B) {
	data := colorJPEG420(b)
	d := compress.NewJPEGDecompressorForPhotometric("YBR_FULL")
	b.SetBytes(512 * 512 * 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Decompress(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkJPEGDecompress420ToRGB decodes the same frame to RGB, as a data
// set described as RGB is decoded.
func BenchmarkJPEGDecompress420ToRGB(b *testing.B) {
	data := colorJPEG420(b)
	d := compress.NewJPEGDecompressorForPhotometric("RGB")
	b.SetBytes(512 * 512 * 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Decompress(data); err != nil {
			b.Fatal(err)
		}
	}
}
