package dataset_test

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A Modality LUT Sequence, a VOI LUT Sequence and a Presentation LUT were read
// by nothing that renders (#334): a table-driven image came out as a straight
// ramp, a Modality LUT image black, an INVERSE image the wrong way round. No
// file in pydicom's corpus has any of them, so scripts/render-lut-fixture.py
// writes them with pydicom, and with each the picture pydicom's
// apply_modality_lut, apply_voi_lut and apply_presentation_lut make of it.

// readLUTFixture reads one of the generated files.
func readLUTFixture(t *testing.T, name string) *dataset.Dataset {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "lut", name+".dcm"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return df.GetDataset()
}

// TestRenderLookupTablesMatchPydicom renders each generated file and compares
// every pixel with pydicom's picture of it. Each image is a ramp running past
// both ends of its table, so the first and last entries and the clamping
// beyond them are all used.
func TestRenderLookupTablesMatchPydicom(t *testing.T) {
	for _, name := range []string{
		"voi_lut_sequence",          // 16-bit entries, preferred over the window beside it
		"voi_lut_8bit_offset",       // 8-bit entries, first mapped value 1000
		"voi_lut_signed",            // signed samples, first mapped value -1024 (SS)
		"modality_lut_sequence",     // a Modality LUT, then a window in its units
		"modality_and_voi_lut",      // both tables
		"presentation_lut_inverse",  // Presentation LUT Shape INVERSE on MONOCHROME2
		"presentation_lut_sequence", // a 10-bit Presentation LUT after a window
	} {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "lut", name+"_expected.raw"))
			if err != nil {
				t.Fatalf("read expected: %v", err)
			}
			got := grays(t, readLUTFixture(t, name), nil)
			if len(got) != len(want) {
				t.Fatalf("%d pixels, pydicom has %d", len(got), len(want))
			}
			differ := 0
			for i := range got {
				if got[i] != want[i] {
					differ++
				}
			}
			if differ > 0 {
				t.Errorf("%d of %d pixels differ from pydicom's\n got  %v\n want %v",
					differ, len(got), got[:16], want[:16])
			}
		})
	}
}

// TestRenderWindowOptionOverridesTheVOILUT: the caller's window is the
// caller's choice and wins over the file's table, as it wins over the file's
// window.
func TestRenderWindowOptionOverridesTheVOILUT(t *testing.T) {
	ds := readLUTFixture(t, "voi_lut_sequence")
	got := grays(t, ds, &dataset.RenderOptions{Window: &dataset.Window{Center: 2048, Width: 4096}})
	// The fixture's first row runs 0, 273, 546, ... 4095: a straight ramp
	// through this window, where the table is a square root curve.
	if got[0] != 0 || got[15] != 255 || got[8] < 128 || got[8] > 140 {
		t.Errorf("the window option gave %v; the table seems to have been applied", got[:16])
	}
}

// presentationShape is a one-row 8-bit image with a 0/255 window, so the
// rendered level equals the stored value before any inversion.
func presentationShape(photometric, shape string) *dataset.Dataset {
	ds := picture{allocated: 8, stored: 8, photometric: photometric, px: []byte{0, 64, 255},
		text: map[tag.Tag]string{tagWindowCenter: "128", tagWindowWidth: "256"}}.dataset()
	if shape != "" {
		if len(shape)%2 == 1 {
			shape += " "
		}
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x2050, 0x0020), dataelem.CS, []byte(shape)))
	}
	return ds
}

// TestRenderPresentationLUTShape: INVERSE inverts once. On MONOCHROME1 the
// INVERSE that DX, MG and IO require is that image's inversion, not a second
// one (PS3.3 C.8.11.1.1.1), and IDENTITY says the values are already
// P-values, brighter as they rise.
func TestRenderPresentationLUTShape(t *testing.T) {
	for _, c := range []struct {
		photometric, shape string
		want               []uint8
	}{
		{"MONOCHROME2", "", []uint8{0, 64, 255}},
		{"MONOCHROME2", "IDENTITY", []uint8{0, 64, 255}},
		{"MONOCHROME2", "INVERSE", []uint8{255, 191, 0}},
		{"MONOCHROME1", "", []uint8{255, 191, 0}},
		{"MONOCHROME1", "INVERSE", []uint8{255, 191, 0}},
		{"MONOCHROME1", "IDENTITY", []uint8{0, 64, 255}},
	} {
		got := grays(t, presentationShape(c.photometric, c.shape), nil)
		if !sameLevels(got, c.want) {
			t.Errorf("%s with shape %q: levels %v, want %v", c.photometric, c.shape, got, c.want)
		}
	}
}

// TestRenderRefusesAnUnknownPresentationLUTShape: a shape this does not know
// is refused by name rather than ignored, since ignoring it may show the
// image's negative.
func TestRenderRefusesAnUnknownPresentationLUTShape(t *testing.T) {
	_, err := presentationShape("MONOCHROME2", "LIN OD").RenderFrame(0, nil)
	if err == nil || !strings.Contains(err.Error(), "LIN OD") {
		t.Errorf("a Presentation LUT Shape of LIN OD gave %v, want a refusal naming it", err)
	}
}

// itemIn returns a sequence holding one item.
func itemIn(t *testing.T, item *dataset.Dataset) *sequence.Sequence {
	t.Helper()
	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatalf("append: %v", err)
	}
	return seq
}

// TestRenderFrameVOILUTFromItsFunctionalGroup: an enhanced image may give a
// frame its own VOI LUT Sequence in Frame VOI LUT (PS3.3 C.7.6.16.2.10), and
// the nearest wins: frame 0's own table over the whole image's window, while
// frame 1, with none of its own, takes the window.
func TestRenderFrameVOILUTFromItsFunctionalGroup(t *testing.T) {
	ds := picture{allocated: 8, stored: 8, photometric: "MONOCHROME2", frames: 2,
		px:   []byte{0, 1, 2, 3, 0, 1, 2, 3},
		text: map[tag.Tag]string{tagWindowCenter: "128", tagWindowWidth: "256"}}.dataset()

	// A four-entry table of 8 bits that reverses its input.
	table := dataset.NewDataset()
	_ = table.Add(dataelem.NewDataElement(tag.New(0x0028, 0x3002), dataelem.US, words(4, 0, 8)))
	_ = table.Add(dataelem.NewDataElement(tag.New(0x0028, 0x3006), dataelem.OW, words(255, 170, 85, 0)))
	frameVOI := dataset.NewDataset()
	if err := frameVOI.AddSequence(tag.New(0x0028, 0x3010), itemIn(t, table)); err != nil {
		t.Fatal(err)
	}
	own := dataset.NewDataset()
	if err := own.AddSequence(tag.New(0x0028, 0x9132), itemIn(t, frameVOI)); err != nil {
		t.Fatal(err)
	}
	perFrame := sequence.New()
	for _, item := range []*dataset.Dataset{own, dataset.NewDataset()} {
		if err := perFrame.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := ds.AddSequence(tag.New(0x5200, 0x9230), perFrame); err != nil {
		t.Fatal(err)
	}

	frames, err := ds.RenderFrames(nil)
	if err != nil {
		t.Fatalf("RenderFrames: %v", err)
	}
	if got, want := frames[0].(*image.Gray).Pix, []uint8{255, 170, 85, 0}; !sameLevels(got, want) {
		t.Errorf("frame 0, with its own table: %v, want %v", got, want)
	}
	if got, want := frames[1].(*image.Gray).Pix, []uint8{0, 1, 2, 3}; !sameLevels(got, want) {
		t.Errorf("frame 1, with the image's window: %v, want %v", got, want)
	}
}
