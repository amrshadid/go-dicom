package cli

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// NIfTI export wrote 348 zero bytes with the magic string at the end and
// appended the pixel data element's raw value.
//
// Every field a reader needs was zero — including sizeof_hdr, the first thing
// any reader checks — so nibabel answered "Cannot work out file type of
// out.nii" while the command printed "Conversion completed successfully!".
//
// The value was wrong as well as the header: for a compressed instance the
// stored value is a JPEG or RLE codestream, so the file would have held a
// codestream described as an image even if every field had been right.
//
// The expectations below are read back from the header at the offsets nifti1.h
// fixes, and the whole file is checked against nibabel in the interoperability
// script.

func niftiOf(t *testing.T, name string) []byte {
	t.Helper()

	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	path := filepath.Join(corpus, name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s is not present: %v", name, err)
	}
	data, err := convertToNifti(path)
	if err != nil {
		t.Fatalf("convertToNifti(%s): %v", name, err)
	}
	return data
}

func i32(b []byte, off int) int32   { return int32(binary.LittleEndian.Uint32(b[off:])) }
func i16(b []byte, off int) int16   { return int16(binary.LittleEndian.Uint16(b[off:])) }
func f32(b []byte, off int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b[off:])) }

// TestNiftiHeaderIsRecognizable covers the fields that decide whether a reader
// will look at the file at all.
func TestNiftiHeaderIsRecognizable(t *testing.T) {
	data := niftiOf(t, "CT_small.dcm")

	if len(data) < niftiVoxelOffset {
		t.Fatalf("the file is %d bytes, shorter than a header and its offset", len(data))
	}
	// sizeof_hdr. Zero here is what made nibabel refuse the file outright.
	if got := i32(data, 0); got != niftiHeaderSize {
		t.Errorf("sizeof_hdr is %d, want %d; a reader decides from this alone whether "+
			"the file is NIfTI", got, niftiHeaderSize)
	}
	if got := string(data[344:348]); got != "n+1\x00" {
		t.Errorf("the magic is %q, want n+1", got)
	}
	// vox_offset has to match where the data actually starts, or the image is
	// read from the wrong place.
	if got := f32(data, 108); int(got) != niftiVoxelOffset {
		t.Errorf("vox_offset is %v, want %d", got, niftiVoxelOffset)
	}
}

// TestNiftiDescribesTheImage covers the fields that say what the data is.
//
// CT_small.dcm is 128 by 128, 16-bit signed, with 0.661468 mm pixels and 5 mm
// slices. Those numbers are the file's, not this library's.
func TestNiftiDescribesTheImage(t *testing.T) {
	data := niftiOf(t, "CT_small.dcm")

	if got := i16(data, 40); got != 3 {
		t.Errorf("dim[0] is %d, want 3", got)
	}
	if got := i16(data, 42); got != 128 {
		t.Errorf("dim[1] is %d, want 128 columns", got)
	}
	if got := i16(data, 44); got != 128 {
		t.Errorf("dim[2] is %d, want 128 rows", got)
	}
	if got := i16(data, 46); got != 1 {
		t.Errorf("dim[3] is %d, want 1 slice", got)
	}
	if got := i16(data, 70); got != niftiInt16 {
		t.Errorf("datatype is %d, want %d (int16)", got, niftiInt16)
	}
	if got := i16(data, 72); got != 16 {
		t.Errorf("bitpix is %d, want 16", got)
	}
	if got := f32(data, 88); got != 5 {
		t.Errorf("pixdim[3] is %v, want the 5 mm slice thickness", got)
	}
	if got := f32(data, 80); math.Abs(float64(got)-0.661468) > 1e-5 {
		t.Errorf("pixdim[1] is %v, want the 0.661468 mm pixel spacing", got)
	}

	// The data has to be exactly as long as the dimensions say.
	want := 128 * 128 * 1 * 2
	if got := len(data) - niftiVoxelOffset; got != want {
		t.Errorf("the file holds %d bytes of image data, want %d", got, want)
	}
}

// TestNiftiCarriesTheRescale covers the difference between stored values and
// what they mean.
//
// A CT stores values that become Hounsfield units only after the rescale is
// applied. Dropping it leaves an image whose numbers are off by a thousand.
func TestNiftiCarriesTheRescale(t *testing.T) {
	data := niftiOf(t, "CT_small.dcm")

	slope := f32(data, 112)
	if slope == 0 {
		t.Error("scl_slope is 0, which tells a reader not to scale at all; " +
			"an absent Rescale Slope is 1, not 0")
	}
	if inter := f32(data, 116); inter == 0 {
		t.Error("scl_inter is 0; CT_small.dcm has a Rescale Intercept of -1024")
	}
}

// TestNiftiDecodesCompressedPixelData covers the other half of what was wrong.
//
// The old converter copied the pixel data element's stored value. For a
// compressed instance that is a codestream, so the file described a JPEG as an
// array of samples.
func TestNiftiDecodesCompressedPixelData(t *testing.T) {
	data := niftiOf(t, "MR_small_jpeg_ls_lossless.dcm")

	rows := int(i16(data, 44))
	columns := int(i16(data, 42))
	bitpix := int(i16(data, 72))
	want := rows * columns * bitpix / 8

	if got := len(data) - niftiVoxelOffset; got != want {
		t.Errorf("the file holds %d bytes of image data for a %dx%d image at %d bits, "+
			"want %d; a codestream would be shorter and of no particular length",
			got, columns, rows, bitpix, want)
	}
}

// TestNiftiRefusesWhatItCannotDescribe covers the images with no datatype.
func TestNiftiRefusesWhatItCannotDescribe(t *testing.T) {
	for _, tc := range []struct {
		bits, samples uint16
		signed        bool
	}{
		{12, 1, false}, // no 12-bit NIfTI datatype
		{16, 3, false}, // color is 8 bits per sample or nothing
		{16, 2, false}, // two samples per pixel is not a thing NIfTI describes
	} {
		if _, _, err := niftiDatatype(tc.bits, tc.samples, tc.signed); err == nil {
			t.Errorf("%d bits and %d samples per pixel was accepted", tc.bits, tc.samples)
		}
	}
}

// TestNiftiDatatypes pins the mapping.
func TestNiftiDatatypes(t *testing.T) {
	tests := []struct {
		bits, samples uint16
		signed        bool
		datatype      int
		bitpix        int
	}{
		{8, 1, false, niftiUint8, 8},
		{8, 1, true, niftiInt8, 8},
		{16, 1, false, niftiUint16, 16},
		{16, 1, true, niftiInt16, 16},
		{32, 1, false, niftiUint32, 32},
		{32, 1, true, niftiInt32, 32},
		{8, 3, false, niftiRGB24, 24},
	}
	for _, tc := range tests {
		datatype, bitpix, err := niftiDatatype(tc.bits, tc.samples, tc.signed)
		if err != nil {
			t.Errorf("%d bits, %d samples, signed=%v: %v", tc.bits, tc.samples, tc.signed, err)
			continue
		}
		if datatype != tc.datatype || bitpix != tc.bitpix {
			t.Errorf("%d bits, %d samples, signed=%v gave datatype %d bitpix %d, want %d and %d",
				tc.bits, tc.samples, tc.signed, datatype, bitpix, tc.datatype, tc.bitpix)
		}
	}
}
