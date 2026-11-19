package dataset_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// TestJPEGWithRGBPhotometricDecodesAsDcmtkDoes proves pydicom's two JPEG
// Baseline files whose Photometric Interpretation is RGB decode to the picture
// dcmtk 3.7.0's dcmdjpeg gives by default, within the one level two IDCTs may
// differ by. SC_jpeg_no_color_transform.dcm has no APP marker and component
// identifiers 0, 1, 2; its samples were converted as YCbCr and a gray picture
// came out magenta, 137 levels off. SC_jpeg_no_color_transform_2.dcm has an
// Adobe marker saying RGB and decoded right before. The expected pictures in
// testdata/jpegcolor are dcmdjpeg's output saved as PNG.
func TestJPEGWithRGBPhotometricDecodesAsDcmtkDoes(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	for _, name := range []string{"SC_jpeg_no_color_transform.dcm", "SC_jpeg_no_color_transform_2.dcm"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, name))
			if err != nil {
				t.Skipf("%s is not in this corpus", name)
			}
			defer func() { _ = f.Close() }()
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatal(err)
			}
			arr, err := df.GetDataset().PixelArrayBySample()
			if err != nil {
				t.Fatal(err)
			}
			frames, ok := arr.([][][][]uint8)
			if !ok || len(frames) != 1 {
				t.Fatalf("PixelArrayBySample returned %T for one 8-bit color frame", arr)
			}
			frame := frames[0]

			pf, err := os.Open(filepath.Join("testdata", "jpegcolor", name[:len(name)-4]+".dcmdjpeg.png"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = pf.Close() }()
			img, err := png.Decode(pf)
			if err != nil {
				t.Fatal(err)
			}
			want, ok := img.(*image.RGBA)
			if !ok {
				nrgba, isN := img.(*image.NRGBA)
				if !isN {
					t.Fatalf("expected picture is %T", img)
				}
				want = &image.RGBA{Pix: nrgba.Pix, Stride: nrgba.Stride, Rect: nrgba.Rect}
			}
			if len(frame) != want.Rect.Dy() || len(frame[0]) != want.Rect.Dx() {
				t.Fatalf("decoded %dx%d, dcmdjpeg %dx%d", len(frame[0]), len(frame), want.Rect.Dx(), want.Rect.Dy())
			}
			worst := 0
			for y := range frame {
				for x := range frame[y] {
					for c := 0; c < 3; c++ {
						d := int(frame[y][x][c]) - int(want.Pix[y*want.Stride+x*4+c])
						if d < 0 {
							d = -d
						}
						worst = max(worst, d)
					}
				}
			}
			if worst > 1 {
				t.Errorf("decodes up to %d levels from dcmdjpeg's picture; pixel [0,0] is %v", worst, frame[0][0])
			}
		})
	}
}
