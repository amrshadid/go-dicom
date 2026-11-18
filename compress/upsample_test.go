package compress_test

import (
	"os"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// The frames in testdata/upsample are dcmtk's (dcmcjpeg +eb, +s2 for 4:2:2
// and +n1 for 4:2:0) from RGB images of random 2x2 blocks, so the chroma
// changes sharply everywhere. Each *_libjpeg.raw is dcmtk's decode of the
// same frame left in YCbCr (dcmdjpeg +cn), which is libjpeg's, and which
// Pillow (libjpeg-turbo) matches exactly. The sizes are odd on purpose, and
// 4x9 and 1x5 have chroma one or two samples wide, which libjpeg repeats
// rather than filters.

// TestSubsampledChromaMatchesLibjpeg proves that a 4:2:2 or 4:2:0 JPEG
// decodes within one level of libjpeg in every sample. The one level is the
// inverse DCT, which T.83 does not ask two decoders to agree on exactly, and
// which is all that separates them on a frame that is not subsampled.
//
// Repeating each chroma sample, as the decoder did, differed from libjpeg by
// up to 43 levels on pydicom's corpus and by more on these frames.
func TestSubsampledChromaMatchesLibjpeg(t *testing.T) {
	for _, name := range []string{"422_37x41", "420_37x41", "422_9x7", "420_9x7", "420_101x67", "420_4x9", "420_1x5"} {
		t.Run(name, func(t *testing.T) {
			frame, err := os.ReadFile("testdata/upsample/" + name + ".jpg")
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile("testdata/upsample/" + name + "_libjpeg.raw")
			if err != nil {
				t.Fatal(err)
			}
			got, err := compress.NewJPEGDecompressorForPhotometric("YBR_FULL").Decompress(frame)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("decoded %d bytes, libjpeg %d", len(got), len(want))
			}
			worst, at := 0, 0
			for i := range want {
				d := int(got[i]) - int(want[i])
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst, at = d, i
				}
			}
			if worst > 1 {
				t.Errorf("sample %d (pixel %d, component %d) is %d, libjpeg decodes %d",
					at, at/3, at%3, got[at], want[at])
			}
		})
	}
}
