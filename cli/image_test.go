package cli

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// convert wrote JSON, CSV and NIfTI, and no image anything outside medical
// imaging can open (#234).

// writeTwoFrameImage writes a 2 by 2, two-frame, 8-bit image with a window
// that maps 0 to black and 255 to white.
func writeTwoFrameImage(t *testing.T, path string) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(tg tag.Tag, vr dataelem.VR, value interface{}) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	us := func(v uint16) []byte {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		return b
	}
	add(tag.New(0x0008, 0x0016), dataelem.UI, "1.2.840.10008.5.1.4.1.1.7.2")
	add(tag.New(0x0008, 0x0018), dataelem.UI, "1.2.826.0.1.3680043.10.511.9.8")
	add(tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John")
	add(tag.New(0x0028, 0x0002), dataelem.US, us(1))
	add(tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	add(tag.New(0x0028, 0x0008), dataelem.IS, "2 ")
	add(tag.New(0x0028, 0x0010), dataelem.US, us(2))
	add(tag.New(0x0028, 0x0011), dataelem.US, us(2))
	add(tag.New(0x0028, 0x0100), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0101), dataelem.US, us(8))
	add(tag.New(0x0028, 0x0102), dataelem.US, us(7))
	add(tag.New(0x0028, 0x0103), dataelem.US, us(0))
	add(tag.New(0x0028, 0x1050), dataelem.DS, "128 ")
	add(tag.New(0x0028, 0x1051), dataelem.DS, "256 ")
	add(tag.New(0x7FE0, 0x0010), dataelem.OB, []byte{0, 255, 255, 0, 255, 255, 0, 0})
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.7.2", "1.2.826.0.1.3680043.10.511.9.8", ds); err != nil {
		t.Fatal(err)
	}
}

func runConvert(args ...string) error {
	return (&ConvertCommand{}).Execute(args)
}

func decodeGray(t *testing.T, path string) []uint8 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the image was not written: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s is not a PNG: %v", path, err)
	}
	gray, ok := img.(*image.Gray)
	if !ok {
		t.Fatalf("%s decodes as %T, want 8-bit grayscale", path, img)
	}
	return gray.Pix
}

// TestConvertToPNG: the first frame by default, as the window shows it, in a
// file that holds the picture and nothing about the patient.
func TestConvertToPNG(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.png")
	writeTwoFrameImage(t, in)

	if err := runConvert(in, out, "--format", "png"); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := decodeGray(t, out); !bytes.Equal(got, []byte{0, 255, 255, 0}) {
		t.Errorf("frame 0 is %v", got)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Doe")) {
		t.Error("the patient's name is in the PNG")
	}
	// A PNG is chunks of length, type, data and checksum after an 8-byte
	// signature. Only the three that make an image may be there: a text
	// chunk is where metadata would ride along.
	for off := 8; off+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[off:]))
		kind := string(data[off+4 : off+8])
		if kind != "IHDR" && kind != "IDAT" && kind != "IEND" {
			t.Errorf("the PNG has a %s chunk", kind)
		}
		off += 12 + length
	}
}

// TestConvertFrames: --frame picks one, counted from 1 as viewers count
// them, and "all" writes each beside the name given.
func TestConvertFrames(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.dcm")
	writeTwoFrameImage(t, in)

	second := filepath.Join(dir, "second.png")
	if err := runConvert(in, second, "--format", "png", "--frame", "2"); err != nil {
		t.Fatalf("--frame 2: %v", err)
	}
	if got := decodeGray(t, second); !bytes.Equal(got, []byte{255, 255, 0, 0}) {
		t.Errorf("frame 2 is %v", got)
	}

	if err := runConvert(in, filepath.Join(dir, "cine.png"), "--format", "png", "--frame", "all"); err != nil {
		t.Fatalf("--frame all: %v", err)
	}
	if got := decodeGray(t, filepath.Join(dir, "cine-0001.png")); !bytes.Equal(got, []byte{0, 255, 255, 0}) {
		t.Errorf("cine-0001.png is %v", got)
	}
	if got := decodeGray(t, filepath.Join(dir, "cine-0002.png")); !bytes.Equal(got, []byte{255, 255, 0, 0}) {
		t.Errorf("cine-0002.png is %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "cine.png")); err == nil {
		t.Error("--frame all also wrote the unnumbered name")
	}

	for _, frame := range []string{"0", "3", "-1", "two"} {
		err := runConvert(in, filepath.Join(dir, "bad.png"), "--format", "png", "--frame", frame)
		if err == nil {
			t.Errorf("--frame %s of a 2-frame image was accepted", frame)
		}
	}
}

// TestConvertWindow: --window replaces the file's.
func TestConvertWindow(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.png")
	writeTwoFrameImage(t, in)

	// Center 300, width 2: everything in the image is below it.
	if err := runConvert(in, out, "--format", "png", "--window", "300,2"); err != nil {
		t.Fatalf("--window: %v", err)
	}
	if got := decodeGray(t, out); !bytes.Equal(got, []byte{0, 0, 0, 0}) {
		t.Errorf("windowed frame is %v", got)
	}
	for _, window := range []string{"300", "a,b", "300,0", "1,2,3"} {
		if err := runConvert(in, out, "--format", "png", "--window", window); err == nil {
			t.Errorf("--window %s was accepted", window)
		}
	}
}

// TestConvertToJPEG: a JPEG of the right size. Its samples are not exact,
// which is what the format is.
func TestConvertToJPEG(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.jpg")
	writeTwoFrameImage(t, in)

	if err := runConvert(in, out, "--format", "jpeg", "--quality", "95"); err != nil {
		t.Fatalf("convert: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("the output is not a JPEG: %v", err)
	}
	if size := img.Bounds().Size(); size.X != 2 || size.Y != 2 {
		t.Errorf("the JPEG is %v, want 2 by 2", size)
	}
	if err := runConvert(in, out, "--format", "jpeg", "--quality", "0"); err == nil {
		t.Error("--quality 0 was accepted")
	}
}

// TestConvertImageOfWhatHasNone: a report has no picture, and saying so beats
// an empty file.
func TestConvertImageOfWhatHasNone(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.png")
	writePatientInstanceForImage(t, in)
	err := runConvert(in, out, "--format", "png")
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Errorf("converting an instance with no pixel data gave %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("an output file was written for an instance with no image")
	}
}

// writePatientInstanceForImage writes an instance with no pixel data.
func writePatientInstanceForImage(t *testing.T, path string) {
	t.Helper()
	ds := dataset.NewDataset()
	for tg, v := range map[tag.Tag]string{
		tag.New(0x0008, 0x0016): "1.2.840.10008.5.1.4.1.1.88.11",
		tag.New(0x0008, 0x0018): "1.2.826.0.1.3680043.10.511.9.9",
	} {
		if err := ds.Add(dataelem.NewDataElement(tg, dataelem.UI, v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.88.11", "1.2.826.0.1.3680043.10.511.9.9", ds); err != nil {
		t.Fatal(err)
	}
}
