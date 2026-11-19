package validate_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/validate"
)

var (
	sharedGroups   = tag.New(0x5200, 0x9229)
	perFrameGroups = tag.New(0x5200, 0x9230)
	pixelMeasures  = tag.New(0x0028, 0x9110)
	frameContent   = tag.New(0x0020, 0x9111)
	segmentID      = tag.New(0x0062, 0x000A)
	planeOrient    = tag.New(0x0020, 0x9116)
)

// items makes a sequence of the items given.
func items(t *testing.T, of ...*dataset.Dataset) *sequence.Sequence {
	t.Helper()
	seq := sequence.New()
	for _, item := range of {
		if err := seq.Append(item); err != nil {
			t.Fatal(err)
		}
	}
	return seq
}

// putSequence replaces a sequence of a data set.
func putSequence(t *testing.T, ds *dataset.Dataset, tg tag.Tag, seq *sequence.Sequence) {
	t.Helper()
	ds.Remove(tg)
	if err := ds.AddSequence(tg, seq); err != nil {
		t.Fatal(err)
	}
}

// itemOf is the item of a sequence a data set holds.
func itemOf(t *testing.T, ds *dataset.Dataset, tg tag.Tag, i int) *dataset.Dataset {
	t.Helper()
	elem, ok := ds.Get(tg)
	if !ok {
		t.Fatalf("no %s", tg)
	}
	seq := elem.GetValue().(*sequence.Sequence)
	return seq.Items()[i].(*dataset.Dataset)
}

// macroSaid lists the findings about one functional group macro, as
// "severity rule message".
func macroSaid(r *validate.Report, macro string) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Module == macro+" (functional group)" && f.Tag == "" {
			out = append(out, f.Severity.String()+" "+f.Rule+" "+f.Message)
		}
	}
	return out
}

// errorsAbout lists the errors and warnings about one attribute, as
// "severity path message".
func errorsAbout(r *validate.Report, tg tag.Tag) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Tag == tg.String() && f.Severity != validate.NotChecked {
			out = append(out, f.Severity.String()+" "+f.Path+" "+f.Message)
		}
	}
	return out
}

func expectMacro(t *testing.T, r *validate.Report, macro, want, when string) {
	t.Helper()
	got := macroSaid(r, macro)
	var errs []string
	for _, g := range got {
		if !strings.HasPrefix(g, "not checked") {
			errs = append(errs, g)
		}
	}
	switch {
	case want == "" && len(errs) != 0:
		t.Errorf("%s: %s: want no error, got %q", when, macro, errs)
	case want != "" && (len(errs) != 1 || !strings.HasPrefix(errs[0], want)):
		t.Errorf("%s: %s: want %q..., got %q", when, macro, want, errs)
	}
}

// The Enhanced CT Image IOD's Functional Group Macros table (A.38-2) is read
// from PS3.3 with each macro's usage and placement: Pixel Measures is
// Mandatory and may be shared or per-frame, Frame Content is Mandatory and
// "May not be used as a Shared Functional Group", and CT Acquisition Type is
// Conditional on "Image Type (0008,0008) Value 1 is ORIGINAL or MIXED, may be
// present otherwise", read as data.
func TestTheFunctionalGroupMacrosTablesAreGenerated(t *testing.T) {
	iod, ok := validate.IODForSOPClass("1.2.840.10008.5.1.4.1.1.2.1")
	if !ok || iod.FunctionalGroupsTable != "A.38-2" {
		t.Fatalf("Enhanced CT: %v, table %q", ok, iod.FunctionalGroupsTable)
	}
	byName := map[string]validate.FunctionalGroup{}
	for _, fg := range iod.FunctionalGroups {
		byName[fg.Macro] = fg
		if _, ok := validate.LookupTable(fg.Table); !ok {
			t.Errorf("%s: its table %s was not generated", fg.Macro, fg.Table)
		}
	}
	if fg := byName["Pixel Measures"]; fg.Usage != validate.Mandatory || fg.Placement != validate.SharedOrPerFrame {
		t.Errorf("Pixel Measures: %+v", fg)
	}
	if fg := byName["Frame Content"]; fg.Usage != validate.Mandatory || fg.Placement != validate.PerFrameOnly {
		t.Errorf("Frame Content: %+v", fg)
	}
	fg := byName["CT Acquisition Type"]
	if fg.Usage != validate.Conditional || fg.When == nil || fg.When.Test != validate.HasValue ||
		fg.When.Tag != 0x00080008 || fg.When.Value != 1 || strings.Join(fg.When.Values, ",") != "ORIGINAL,MIXED" ||
		!fg.When.Otherwise {
		t.Errorf("CT Acquisition Type: %+v, condition %+v", fg, fg.When)
	}
}

// enhancedCT is an Enhanced CT Image data set with two frames, its Shared
// Functional Groups item and both Per-Frame items empty: every macro is
// missing.
func enhancedCT(t *testing.T) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	put(t, ds, tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.2.1\x00"))
	put(t, ds, tag.New(0x0008, 0x0008), dataelem.CS, []byte(`DERIVED\PRIMARY\AXIAL\NONE`))
	put(t, ds, tag.New(0x0028, 0x0008), dataelem.IS, []byte("2 "))
	putSequence(t, ds, sharedGroups, items(t, dataset.NewDataset()))
	putSequence(t, ds, perFrameGroups, items(t, dataset.NewDataset(), dataset.NewDataset()))
	return ds
}

// pixelMeasuresItem is a Pixel Measures Sequence of one item with Pixel
// Spacing and Slice Thickness, its Type 1 attributes in an Enhanced CT.
func pixelMeasuresItem(t *testing.T) *sequence.Sequence {
	item := dataset.NewDataset()
	put(t, item, tag.New(0x0028, 0x0030), dataelem.DS, []byte(`0.5\0.5 `))
	put(t, item, tag.New(0x0018, 0x0050), dataelem.DS, []byte("1 "))
	return items(t, item)
}

// A Mandatory macro is in the shared item or in the item of every frame:
// absent from both is one error, absent from one frame names the frame, and
// in the shared item alone, or in every frame, it is there.
func TestAMandatoryFunctionalGroupIsRequired(t *testing.T) {
	ds := enhancedCT(t)
	expectMacro(t, check(t, ds), "Pixel Measures",
		"error Usage M absent from the shared item and from every per-frame item", "nowhere")

	putSequence(t, itemOf(t, ds, perFrameGroups, 0), pixelMeasures, pixelMeasuresItem(t))
	expectMacro(t, check(t, ds), "Pixel Measures",
		"error Usage M absent from 1 of the 2 per-frame items (frame 2 first) and from the shared item", "frame 1 only")

	putSequence(t, itemOf(t, ds, perFrameGroups, 1), pixelMeasures, pixelMeasuresItem(t))
	expectMacro(t, check(t, ds), "Pixel Measures", "", "both frames")

	ds = enhancedCT(t)
	putSequence(t, itemOf(t, ds, sharedGroups, 0), pixelMeasures, pixelMeasuresItem(t))
	expectMacro(t, check(t, ds), "Pixel Measures", "", "shared")
}

// "M - May not be used as a Shared Functional Group": Frame Content in the
// shared item is an error, and a macro in the shared item and in a frame is
// another (Table C.7.6.16-1).
func TestAFunctionalGroupIsWhereTheTableAllows(t *testing.T) {
	content := func() *sequence.Sequence {
		item := dataset.NewDataset()
		put(t, item, tag.New(0x0020, 0x9157), dataelem.UL, []byte{1, 0, 0, 0})
		return items(t, item)
	}
	ds := enhancedCT(t)
	putSequence(t, itemOf(t, ds, sharedGroups, 0), frameContent, content())
	expectMacro(t, check(t, ds), "Frame Content",
		"error Functional Groups in the shared item; the IOD's table says it may not be used as a Shared Functional Group",
		"Frame Content shared")

	ds = enhancedCT(t)
	putSequence(t, itemOf(t, ds, sharedGroups, 0), pixelMeasures, pixelMeasuresItem(t))
	putSequence(t, itemOf(t, ds, perFrameGroups, 1), pixelMeasures, pixelMeasuresItem(t))
	expectMacro(t, check(t, ds), "Pixel Measures",
		"error Functional Groups in the shared item and in 1 per-frame item(s)", "Pixel Measures in both")
}

// A Conditional macro whose condition is read as data is required when it
// holds: CT Acquisition Type with Image Type ORIGINAL, and not with DERIVED.
func TestAConditionalFunctionalGroupFollowsItsCondition(t *testing.T) {
	ds := enhancedCT(t)
	expectMacro(t, check(t, ds), "CT Acquisition Type", "", "DERIVED")

	put(t, ds, tag.New(0x0008, 0x0008), dataelem.CS, []byte(`ORIGINAL\PRIMARY\AXIAL\NONE`))
	expectMacro(t, check(t, ds), "CT Acquisition Type",
		"error Usage C absent from the shared item and from every per-frame item; the condition holds", "ORIGINAL")
}

// The Per-Frame Functional Groups Sequence holds one item for each frame
// (Table C.7.6.16-1).
func TestThePerFrameSequenceHoldsAnItemForEachFrame(t *testing.T) {
	ds := enhancedCT(t)
	put(t, ds, tag.New(0x0028, 0x0008), dataelem.IS, []byte("3 "))
	expect(t, check(t, ds), "(5200,9230)",
		"error Items 2 item(s); the sequence holds one for each frame, and Number of Frames (0028,0008) is 3", "3 frames, 2 items")
}

// A macro in the corpus's segmentation is checked attribute by attribute in
// each frame, and what is wrong in every frame is one finding: Referenced
// Segment Number removed from the Segment Identification Sequence of all three
// frames of liver_1frame.dcm is one error saying 3 of 3 frames; removed from
// the second alone it names that frame. The file as it is has no error there. Plane
// Orientation copied into a frame while it is in the shared item is an
// error, as is Frame Content moved into the shared item.
func TestFunctionalGroupsOfACorpusSegmentation(t *testing.T) {
	base := check(t, corpusFile(t, "liver_1frame.dcm"))
	for _, f := range base.Findings {
		if f.Severity == validate.Error && strings.HasSuffix(f.Module, "(functional group)") {
			t.Errorf("liver_1frame.dcm as it is: %+v", f)
		}
	}

	segment := tag.New(0x0062, 0x000B)
	ds := corpusFile(t, "liver_1frame.dcm")
	for i := range 3 {
		itemOf(t, itemOf(t, ds, perFrameGroups, i), segmentID, 0).Remove(segment)
	}
	want := "error (5200,9230)[*] (0062,000A)[1] (0062,000B) absent; a Type 1 attribute is required (in 3 of 3 frames, frame 1 first)"
	if got := errorsAbout(check(t, ds), segment); strings.Join(got, "|") != want {
		t.Errorf("every frame: %q; want %q", got, want)
	}

	ds = corpusFile(t, "liver_1frame.dcm")
	itemOf(t, itemOf(t, ds, perFrameGroups, 1), segmentID, 0).Remove(segment)
	want = "error (5200,9230)[2] (0062,000A)[1] (0062,000B) absent; a Type 1 attribute is required"
	if got := errorsAbout(check(t, ds), segment); strings.Join(got, "|") != want {
		t.Errorf("frame 2: %q; want %q", got, want)
	}

	ds = corpusFile(t, "liver_1frame.dcm")
	orientation, _ := itemOf(t, ds, sharedGroups, 0).Get(planeOrient)
	putSequence(t, itemOf(t, ds, perFrameGroups, 0), planeOrient, orientation.GetValue().(*sequence.Sequence))
	expectMacro(t, check(t, ds), "Plane Orientation (Patient)",
		"error Functional Groups in the shared item and in 1 per-frame item(s)", "Plane Orientation in both")

	ds = corpusFile(t, "liver_1frame.dcm")
	content, _ := itemOf(t, ds, perFrameGroups, 0).Get(frameContent)
	putSequence(t, itemOf(t, ds, sharedGroups, 0), frameContent, content.GetValue().(*sequence.Sequence))
	for i := range 3 {
		itemOf(t, ds, perFrameGroups, i).Remove(frameContent)
	}
	expectMacro(t, check(t, ds), "Frame Content",
		"error Functional Groups in the shared item; the IOD's table says it may not be used as a Shared Functional Group",
		"Frame Content shared")
}
