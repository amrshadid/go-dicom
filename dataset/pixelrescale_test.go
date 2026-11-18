package dataset_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// pydicomRescale is the Rescale Slope and Intercept pydicom reads from every
// file of its corpus that has pixel data (1 and 0 where a file has none),
// produced by reading each file with pydicom and printing RescaleSlope and
// RescaleIntercept.
var pydicomRescale = []struct {
	file             string
	slope, intercept float64
}{
	{"693_J2KI.dcm", 1, -1024},
	{"CT_small.dcm", 1, -1024},
	{"ExplVR_BigEnd.dcm", 1, 0},
	{"GDCMJ2K_TextGBR.dcm", 1, 0},
	{"J2K_pixelrep_mismatch.dcm", 1, 0},
	{"JPEG-lossy.dcm", 1, 0},
	{"JPEG2000-embedded-sequence-delimiter.dcm", 1, 0},
	{"JPEG2000.dcm", 1, 0},
	{"JPEGLSNearLossless_08.dcm", 1, 0},
	{"JPEGLSNearLossless_16.dcm", 1, 0},
	{"JPGExtended.dcm", 1, 0},
	{"MR_small.dcm", 1, 0},
	{"MR_small_RLE.dcm", 1, 0},
	{"MR_small_bigendian.dcm", 1, 0},
	{"MR_small_expb.dcm", 1, 0},
	{"MR_small_implicit.dcm", 1, 0},
	{"MR_small_jp2klossless.dcm", 1, 0},
	{"MR_small_jpeg_ls_lossless.dcm", 1, 0},
	{"MR_small_padded.dcm", 1, 0},
	{"MR_truncated.dcm", 1, 0},
	{"SC_jpeg_no_color_transform.dcm", 1, 0},
	{"SC_jpeg_no_color_transform_2.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cr.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cy+n1.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cy+n2.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cy+np.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cy+s2.dcm", 1, 0},
	{"SC_rgb_dcmtk_+eb+cy+s4.dcm", 1, 0},
	{"SC_rgb_gdcm_KY.dcm", 1, 0},
	{"SC_rgb_jls_lossy_line.dcm", 1, 0},
	{"SC_rgb_jls_lossy_sample.dcm", 1, 0},
	{"SC_rgb_jpeg.dcm", 1, 0},
	{"SC_rgb_jpeg_app14_dcmd.dcm", 1, 0},
	{"SC_rgb_jpeg_dcmd.dcm", 1, 0},
	{"SC_rgb_jpeg_dcmtk.dcm", 1, 0},
	{"SC_rgb_jpeg_gdcm.dcm", 1, 0},
	{"SC_rgb_jpeg_lossy_gdcm.dcm", 1, 0},
	{"SC_rgb_rle.dcm", 1, 0},
	{"SC_rgb_rle_16bit.dcm", 1, 0},
	{"SC_rgb_rle_16bit_2frame.dcm", 1, 0},
	{"SC_rgb_rle_2frame.dcm", 1, 0},
	{"SC_rgb_rle_32bit.dcm", 1, 0},
	{"SC_rgb_rle_32bit_2frame.dcm", 1, 0},
	{"SC_rgb_small_odd.dcm", 1, 0},
	{"SC_rgb_small_odd_big_endian.dcm", 1, 0},
	{"SC_rgb_small_odd_jpeg.dcm", 1, 0},
	{"SC_ybr_full_422_uncompressed.dcm", 1, 0},
	{"badVR.dcm", 1, 0},
	{"examples_jpeg2k.dcm", 1, 0},
	{"examples_overlay.dcm", 1, 0},
	{"examples_palette.dcm", 1, 0},
	{"examples_rgb_color.dcm", 1, 0},
	{"examples_ybr_color.dcm", 1, 0},
	{"image_dfl.dcm", 1, 0},
	{"liver_1frame.dcm", 1, 0},
	{"liver_expb_1frame.dcm", 1, 0},
	{"meta_missing_tsyntax.dcm", 1, 0},
	{"nested_priv_SQ.dcm", 1, 0},
	{"rtdose.dcm", 1, 0},
	{"rtdose_1frame.dcm", 1, 0},
	{"rtdose_expb.dcm", 1, 0},
	{"rtdose_expb_1frame.dcm", 1, 0},
	{"rtdose_rle.dcm", 1, 0},
	{"rtdose_rle_1frame.dcm", 1, 0},
}

func readCorpus(t *testing.T, name string) *dataset.Dataset {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		t.Skipf("%s not present in the corpus", name)
	}
	defer f.Close()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("ReadDICOMFile(%s): %v", name, err)
	}
	return df.GetDataset()
}

// TestPixelDataInfoRescaleAgainstPydicom proves GetPixelDataInfo holds the
// Rescale Slope and Intercept pydicom reads, on every file of its corpus that
// has pixel data: an intercept of -1024 for the two CT files that have one, and
// 1 and 0, not 0 and 0, for the rest.
func TestPixelDataInfoRescaleAgainstPydicom(t *testing.T) {
	for _, tc := range pydicomRescale {
		t.Run(tc.file, func(t *testing.T) {
			ds := readCorpus(t, tc.file)
			info, err := ds.GetPixelDataInfo()
			if err != nil {
				// Two corpus files carry a Pixel Data element and no Rows or
				// Columns; there is no image to describe.
				t.Skipf("GetPixelDataInfo: %v", err)
			}
			if info.RescaleSlope != tc.slope || info.RescaleIntercept != tc.intercept {
				t.Errorf("slope, intercept = %v, %v; pydicom reads %v, %v",
					info.RescaleSlope, info.RescaleIntercept, tc.slope, tc.intercept)
			}
		})
	}
}

func rescaleElements(slope, intercept string) []*dataelem.DataElement {
	var out []*dataelem.DataElement
	if slope != "" {
		out = append(out, dataelem.NewDataElement(tag.New(0x0028, 0x1053), dataelem.DS, []byte(slope)))
	}
	if intercept != "" {
		out = append(out, dataelem.NewDataElement(tag.New(0x0028, 0x1052), dataelem.DS, []byte(intercept)))
	}
	return out
}

// groupWithRescale is a functional group item holding a Pixel Value
// Transformation Sequence of one item.
func groupWithRescale(slope, intercept string) *dataset.Dataset {
	item := dataset.NewDataset()
	for _, e := range rescaleElements(slope, intercept) {
		_ = item.Add(e)
	}
	inner := sequence.New()
	_ = inner.Append(item)
	group := dataset.NewDataset()
	_ = group.Add(dataelem.NewDataElement(tag.New(0x0028, 0x9145), dataelem.SQ, inner))
	return group
}

func imageWithRescale(t *testing.T, build func(ds *dataset.Dataset)) *dataset.PixelDataInfo {
	t.Helper()
	ds := dataset.NewDataset()
	for _, e := range []struct {
		t tag.Tag
		v uint16
	}{{tag.New(0x0028, 0x0010), 2}, {tag.New(0x0028, 0x0011), 2}, {tag.New(0x0028, 0x0100), 16},
		{tag.New(0x0028, 0x0101), 16}, {tag.New(0x0028, 0x0102), 15}, {tag.New(0x0028, 0x0103), 0}} {
		_ = ds.Add(dataelem.NewDataElement(e.t, dataelem.US, []byte{byte(e.v), byte(e.v >> 8)}))
	}
	build(ds)
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		t.Fatalf("GetPixelDataInfo: %v", err)
	}
	return info
}

// TestPixelDataInfoRescaleDefaultsAndGroups proves the cases the corpus does
// not have: an absent slope is 1 and not 0, an intercept alone leaves the
// slope 1, and an enhanced multi-frame image answers with frame 0's values from
// its own functional group, else from the shared one.
func TestPixelDataInfoRescaleDefaultsAndGroups(t *testing.T) {
	check := func(name string, info *dataset.PixelDataInfo, slope, intercept float64) {
		t.Helper()
		if math.Abs(info.RescaleSlope-slope) > 1e-12 || math.Abs(info.RescaleIntercept-intercept) > 1e-12 {
			t.Errorf("%s: slope, intercept = %v, %v; want %v, %v", name, info.RescaleSlope, info.RescaleIntercept, slope, intercept)
		}
	}
	top := func(slope, intercept string) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for _, e := range rescaleElements(slope, intercept) {
				_ = ds.Add(e)
			}
		}
	}
	perFrame := func(ds *dataset.Dataset) {
		seq := sequence.New()
		_ = seq.Append(groupWithRescale("4", "-10"))
		_ = seq.Append(groupWithRescale("8", "-20"))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x5200, 0x9230), dataelem.SQ, seq))
	}
	shared := func(ds *dataset.Dataset) {
		seq := sequence.New()
		_ = seq.Append(groupWithRescale("3", "7"))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x5200, 0x9229), dataelem.SQ, seq))
	}
	check("none", imageWithRescale(t, func(*dataset.Dataset) {}), 1, 0)
	check("top level", imageWithRescale(t, top("2.5", "-1024")), 2.5, -1024)
	check("intercept only", imageWithRescale(t, top("", "-1000")), 1, -1000)
	check("per frame, frame 0", imageWithRescale(t, perFrame), 4, -10)
	check("shared", imageWithRescale(t, shared), 3, 7)
	check("per frame wins over shared", imageWithRescale(t, func(ds *dataset.Dataset) { shared(ds); perFrame(ds) }), 4, -10)
}
