package dataset_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// encoded is a picture written as PNG, and as JPEG when it is in color: the
// bytes convert writes. A grayscale picture is copied into an *image.Gray
// for JPEG, as convert does, since image/jpeg writes three components for
// any other.
func encoded(t *testing.T, img image.Image) (pngBytes, jpegBytes []byte) {
	t.Helper()
	var p, j bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if img.ColorModel() == color.GrayModel {
		gray := image.NewGray(img.Bounds())
		for y := 0; y < gray.Rect.Dy(); y++ {
			for x := 0; x < gray.Rect.Dx(); x++ {
				gray.Pix[gray.PixOffset(x, y)] = img.At(x, y).(color.Gray).Y
			}
		}
		img = gray
	}
	if err := jpeg.Encode(&j, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return p.Bytes(), j.Bytes()
}

// TestRenderFramesFromAcrossTheCorpus: every frame of every pydicom corpus
// file rendered from the file by position, a row or two at a time, encodes
// to the same PNG and JPEG bytes as the frame rendered from the whole file,
// with the file's own window, with one given, and over the stored range.
// The corpus holds compressed, multi-frame, big endian, color by plane,
// YBR_FULL_422, palette, 1-bit and signed images; a file the whole read
// cannot render must be refused by position too.
func TestRenderFramesFromAcrossTheCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	// A band of about one row of a small image, so every frame is many bands.
	defer dataset.SetRenderBandBytes(64)()

	files, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	options := []*dataset.RenderOptions{
		nil,
		{Window: &dataset.Window{Center: 100, Width: 500}},
		{StoredRange: true},
	}
	compared, refused := 0, 0
	for _, name := range files {
		whole, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(whole)))
		if err != nil {
			continue
		}
		parsed, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(whole)),
			filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
		if err != nil || parsed.Deflated || parsed.PixelDataElement() == nil ||
			parsed.PixelDataElement().Tag != tag.New(0x7FE0, 0x0010) {
			continue
		}
		frames, err := parsed.FrameReader(bytes.NewReader(whole))
		if err != nil {
			continue
		}
		attributes := parsed.GetDataset()
		for _, opts := range options {
			want, wantErr := df.GetDataset().RenderFrames(opts)
			var list []int
			for f := range want {
				list = append(list, f)
			}
			if wantErr != nil {
				info, err := attributes.GetPixelDataInfo()
				if err != nil {
					continue
				}
				for f := 0; f < info.NumberOfFrames; f++ {
					list = append(list, f)
				}
			}
			var got [][2][]byte
			err := attributes.RenderFramesFrom(frames, list, opts, func(_ int, img image.Image) error {
				p, j := encoded(t, img)
				got = append(got, [2][]byte{p, j})
				return nil
			})
			if wantErr != nil {
				if err == nil {
					t.Errorf("%s: the whole file is refused (%v) and the frames read by position are rendered",
						filepath.Base(name), wantErr)
				}
				refused++
				continue
			}
			if err != nil {
				t.Errorf("%s, %+v: %v", filepath.Base(name), opts, err)
				continue
			}
			for f, img := range want {
				p, j := encoded(t, img)
				if !bytes.Equal(p, got[f][0]) {
					t.Errorf("%s frame %d, %+v: the PNG differs from the whole file's", filepath.Base(name), f, opts)
				}
				if !bytes.Equal(j, got[f][1]) {
					t.Errorf("%s frame %d, %+v: the JPEG differs from the whole file's", filepath.Base(name), f, opts)
				}
				compared++
			}
		}
	}
	if compared == 0 {
		t.Fatal("no frame was compared")
	}
	t.Logf("%d frames compared, %d renderings refused alike", compared, refused)
}

// frameFile is a native image held as a FrameReader would find it in a
// file, recording which frames are read.
type frameFile struct {
	ds    *dataset.Dataset
	data  []byte // every frame, one after another
	size  int    // bytes a frame
	read  []int  // the frame of every read, in order
	fails func(frame int, offset int64) bool
}

func (f *frameFile) ReadFrame(i int) ([]byte, error) {
	f.read = append(f.read, i)
	if f.fails != nil && f.fails(i, 0) {
		return nil, errors.New("the disk is gone")
	}
	return slices.Clone(f.data[i*f.size : (i+1)*f.size]), nil
}

func (f *frameFile) ReadFrameBytes(i int, offset int64, p []byte) error {
	f.read = append(f.read, i)
	if f.fails != nil && f.fails(i, offset) {
		return errors.New("the disk is gone")
	}
	copy(p, f.data[i*f.size+int(offset):])
	return nil
}

// newFrameFile makes an image of 16-bit unsigned samples, rows x columns x
// frames, each frame a ramp offset by its number, with a window when one is
// given. ds holds no Pixel Data; whole is the same data set with it.
func newFrameFile(rows, columns, frames int, window string) (file *frameFile, whole *dataset.Dataset) {
	build := func(pixels []byte) *dataset.Dataset {
		ds := dataset.NewDataset()
		ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
		us := func(element uint16, v int) {
			b := make([]byte, 2)
			binary.LittleEndian.PutUint16(b, uint16(v))
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b))
		}
		us(0x0002, 1)
		us(0x0010, rows)
		us(0x0011, columns)
		us(0x0100, 16)
		us(0x0101, 12)
		us(0x0102, 11)
		us(0x0103, 0)
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0004), dataelem.CS, []byte("MONOCHROME2 ")))
		n := strconv.Itoa(frames)
		if len(n)%2 == 1 {
			n += " "
		}
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0008), dataelem.IS, []byte(n)))
		if window != "" {
			_ = ds.Add(dataelem.NewDataElement(tagWindowCenter, dataelem.DS, []byte("1000")))
			_ = ds.Add(dataelem.NewDataElement(tagWindowWidth, dataelem.DS, []byte(window)))
		}
		if pixels != nil {
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, pixels))
		}
		return ds
	}
	size := rows * columns * 2
	data := make([]byte, size*frames)
	for f := 0; f < frames; f++ {
		for i := 0; i < rows*columns; i++ {
			binary.LittleEndian.PutUint16(data[f*size+2*i:], uint16((f*400+i*7)%4096))
		}
	}
	return &frameFile{ds: build(nil), data: data, size: size}, build(data)
}

// TestRenderFramesFromReadsOnlyTheFrameAsked: frame 2 of a windowed image of
// four is drawn from frame 2 alone, while an image with no window reads
// every frame once to find the range they share, and still draws the
// picture the whole image gives. One frame of many cost every frame (#533).
func TestRenderFramesFromReadsOnlyTheFrameAsked(t *testing.T) {
	defer dataset.SetRenderBandBytes(64)()
	for _, window := range []string{"800", ""} {
		file, whole := newFrameFile(10, 16, 4, window)
		want, err := whole.RenderFrame(2, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []byte
		err = file.ds.RenderFramesFrom(file, []int{2}, nil, func(frame int, img image.Image) error {
			got, _ = encoded(t, img)
			return nil
		})
		if err != nil {
			t.Fatalf("window %q: %v", window, err)
		}
		if p, _ := encoded(t, want); !bytes.Equal(p, got) {
			t.Errorf("window %q: frame 2 differs from the whole image's", window)
		}
		read := map[int]bool{}
		for _, f := range file.read {
			read[f] = true
		}
		switch {
		case window != "" && (len(read) != 1 || !read[2]):
			t.Errorf("a windowed frame 2 read frames %v; it needs frame 2 alone", file.read)
		case window == "" && len(read) != 4:
			t.Errorf("a frame with no window read frames %v; its range is that of all four", file.read)
		}
	}
}

// TestRenderFramesFromReturnsABandThatCannotBeRead: a band that cannot be
// read while the picture is encoded leaves the rest of the picture black
// and is returned once the encoder is done, since an image.Image has no
// way to say so itself.
func TestRenderFramesFromReturnsABandThatCannotBeRead(t *testing.T) {
	defer dataset.SetRenderBandBytes(64)()
	file, _ := newFrameFile(10, 16, 1, "800")
	file.fails = func(_ int, offset int64) bool { return offset >= 4*16*2 }
	var bottom color.Color
	err := file.ds.RenderFramesFrom(file, []int{0}, nil, func(_ int, img image.Image) error {
		if err := png.Encode(&bytes.Buffer{}, img); err != nil {
			return err
		}
		bottom = img.At(0, 9)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "the disk is gone") {
		t.Fatalf("got %v; want the read's error", err)
	}
	if bottom != (color.Gray{}) {
		t.Errorf("a row of a band that was not read is %v; want black", bottom)
	}
}

// TestRenderFramesFromRefusesAFrameItDoesNotHave: a frame number past the
// last is refused before anything is read, in RenderFrame's words.
func TestRenderFramesFromRefusesAFrameItDoesNotHave(t *testing.T) {
	file, _ := newFrameFile(2, 2, 3, "800")
	err := file.ds.RenderFramesFrom(file, []int{3}, nil, func(int, image.Image) error { return nil })
	if want := fmt.Sprintf("render: frame %d does not exist; the image has %d, counted from 0", 3, 3); err == nil || err.Error() != want {
		t.Errorf("got %v; want %q", err, want)
	}
	if len(file.read) != 0 {
		t.Errorf("frames %v were read for a frame that does not exist", file.read)
	}
}
