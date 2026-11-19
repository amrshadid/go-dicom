package validate_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/validate"
)

// corpusFile reads a file of pydicom's corpus as a data set, or skips.
func corpusFile(t *testing.T, name string) *dataset.Dataset {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return df.GetDataset()
}

// errorsAndWarnings lists the findings that are not "not checked" as
// "severity tag rule", sorted.
func errorsAndWarnings(r *validate.Report) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Severity != validate.NotChecked {
			out = append(out, f.Severity.String()+" "+f.Path+" "+f.Rule)
		}
	}
	sort.Strings(out)
	return out
}

func check(t *testing.T, ds *dataset.Dataset) *validate.Report {
	t.Helper()
	r, err := validate.Dataset(ds)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The CT the backlog broke: Modality, Patient ID, both study and series
// UIDs, Image Position (Patient), Pixel Spacing and Rescale Slope deleted,
// and Bits Stored set to 99. Each change is one finding, with the rule PS3.3
// states for it, and nothing else is an error: the same file unbroken has
// none.
func TestTheBrokenCTIsReportedForEachChange(t *testing.T) {
	if r := check(t, corpusFile(t, "CT_small.dcm")); len(errorsAndWarnings(r)) != 0 {
		t.Fatalf("CT_small.dcm as it is: %v", errorsAndWarnings(r))
	}

	ds := corpusFile(t, "CT_small.dcm")
	for _, gone := range []tag.Tag{
		tag.New(0x0008, 0x0060), tag.New(0x0010, 0x0020), tag.New(0x0020, 0x000D), tag.New(0x0020, 0x000E),
		tag.New(0x0020, 0x0032), tag.New(0x0028, 0x0030), tag.New(0x0028, 0x1053),
	} {
		ds.Remove(gone)
	}
	if err := ds.SetValue(tag.New(0x0028, 0x0101), []byte{99, 0}); err != nil {
		t.Fatal(err)
	}

	r := check(t, ds)
	want := []string{
		"error (0008,0060) Type 1",
		"error (0010,0020) Type 2",
		"error (0020,000D) Type 1",
		"error (0020,000E) Type 1",
		"error (0020,0032) Type 1",
		"error (0028,0030) Type 1",
		"error (0028,0101) Enumerated Values",
		"error (0028,1053) Type 1",
	}
	if got := errorsAndWarnings(r); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, f := range r.Findings {
		if f.Tag == "(0028,0101)" && f.Reference != "PS3.3 C.8.2.1.1.5" {
			t.Errorf("Bits Stored's rule is cited as %q; the CT Image Module states it in C.8.2.1.1.5", f.Reference)
		}
	}
	if r.IOD != "CT Image" || r.IODSection != "A.3" || r.Edition == "" {
		t.Errorf("checked as %q (%s) from edition %q", r.IOD, r.IODSection, r.Edition)
	}
}

// Each kind of rule is applied: a Type 1 attribute present and empty, a
// value multiplicity below the dictionary's, a value outside the Enumerated
// Values, a Defined Term not listed (a warning), and a sequence with more
// items than its description allows.
func TestEachKindOfRule(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")
	set := func(tg tag.Tag, vr dataelem.VR, value []byte) {
		t.Helper()
		ds.Remove(tg)
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	set(tag.New(0x0008, 0x0060), dataelem.CS, []byte("  "))                  // Modality: empty
	set(tag.New(0x0008, 0x0008), dataelem.CS, []byte("ORIGINAL"))            // Image Type: VM 2-n
	set(tag.New(0x0010, 0x0040), dataelem.CS, []byte("X"))                   // Patient's Sex: M, F, O
	set(tag.New(0x0018, 0x5100), dataelem.CS, []byte("SITTING"))             // Patient Position: not a term of General Series
	set(tag.New(0x0028, 0x0004), dataelem.CS, []byte("MONOCHROME2\\EXTRA ")) // Photometric: VM 1
	route := sequence.New()
	for range 2 {
		item := dataset.NewDataset()
		_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("1")))
		_ = route.Append(item)
	}
	ds.Remove(tag.New(0x0018, 0x0014))
	_ = ds.AddSequence(tag.New(0x0018, 0x0014), route) // Contrast/Bolus Administration Route: one item

	got := strings.Join(errorsAndWarnings(check(t, ds)), "\n")
	for _, want := range []string{
		"error (0008,0060) Type 1",
		"error (0008,0008) Value Multiplicity",
		"error (0010,0040) Enumerated Values",
		"warning (0018,5100) Defined Terms",
		"error (0028,0004) Value Multiplicity",
		"error (0018,0014) Items",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q among:\n%s", want, got)
		}
	}
}

// What PS3.3 states in prose is reported as not checked, never passed: a
// Conditional module's condition whether the module is there or not, and an
// absent Type 1C attribute's.
func TestProseIsReportedAsNotChecked(t *testing.T) {
	r := check(t, corpusFile(t, "CT_small.dcm"))
	var said []string
	for _, f := range r.Findings {
		if f.Severity == validate.NotChecked {
			said = append(said, f.Module+"|"+f.Path+"|"+f.Rule+"|"+f.Message)
		}
	}
	all := strings.Join(said, "\n")
	for _, want := range []string{
		"Synchronization||Usage C|absent; whether it is required was not checked: Required if time synchronization was applied.",
		"Contrast/Bolus||Usage C|present, and checked; whether it is required was not checked",
		"CT Image|(0028,1054)|Type 1C|absent; whether it is required was not checked: Required if the Rescale Type is not HU",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("not said: %q", want)
		}
	}
}

// Files that hold what their IODs require, as far as the rules here reach,
// have no error and no warning. Each was once reported for something it does
// not lack:
//
//   - SC_rgb_rle.dcm has Pixel Spacing, which the SC Image module names as
//     well as the Image Plane module. Taken for Image Plane, it was held to
//     lack Image Position and Orientation (Patient).
//   - test-SR.dcm has a 2D spatial coordinate with Graphic Type CIRCLE. The
//     2D and 3D coordinate macros both name Graphic Type, and the item was
//     checked against the 3D macro's Enumerated Values.
//   - rtplan.dcm has Patient Position HFS, which the RT Patient Setup
//     module allows by adding SITTING to the General Series terms.
//   - examples_ybr_color.dcm has Frame Increment Pointer (0018,1063), an
//     AT value, which PS3.3 lists as 00181063.
func TestConformingCorpusFilesHaveNoFindings(t *testing.T) {
	for _, name := range []string{"CT_small.dcm", "MR_small.dcm", "SC_rgb_rle.dcm", "test-SR.dcm",
		"rtplan.dcm", "examples_ybr_color.dcm"} {
		if got := errorsAndWarnings(check(t, corpusFile(t, name))); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// said lists the findings about one attribute or module (its tag, or its
// module name for a module finding) as "severity rule message".
func said(r *validate.Report, about string) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Tag == about || f.Tag == "" && f.Module == about && strings.HasPrefix(f.Rule, "Usage") {
			out = append(out, f.Severity.String()+" "+f.Rule+" "+f.Message)
		}
	}
	return out
}

// expect fails unless exactly one finding about the attribute or module is
// there and starts with want, or, for want "", none is there.
func expect(t *testing.T, r *validate.Report, about, want, when string) {
	t.Helper()
	got := said(r, about)
	switch {
	case want == "" && len(got) != 0:
		t.Errorf("%s: %s: want no finding, got %q", when, about, got)
	case want != "" && (len(got) != 1 || !strings.HasPrefix(got[0], want)):
		t.Errorf("%s: %s: want one finding %q..., got %q", when, about, want, got)
	}
}

// put replaces an attribute of a data set.
func put(t *testing.T, ds *dataset.Dataset, tg tag.Tag, vr dataelem.VR, value []byte) {
	t.Helper()
	ds.Remove(tg)
	if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
		t.Fatal(err)
	}
}

// "Required if Pixel Data Provider URL (0028,7FE0) is not present": an image
// with neither has an error for its Pixel Data (Image Pixel module, C.7-11a),
// one with the URL instead has none, and one with Pixel Data has none.
func TestAConditionThatAnAttributeIsAbsentIsEvaluated(t *testing.T) {
	pixelData := "(7FE0,0010)"
	expect(t, check(t, corpusFile(t, "CT_small.dcm")), pixelData, "", "CT_small.dcm")

	ds := corpusFile(t, "CT_small.dcm")
	ds.Remove(tag.New(0x7FE0, 0x0010))
	expect(t, check(t, ds), pixelData,
		"error Type 1C absent; the condition holds, so a Type 1C attribute is required: Required if Pixel Data Provider URL (0028,7FE0) is not present.",
		"no Pixel Data, no URL")

	put(t, ds, tag.New(0x0028, 0x7FE0), dataelem.UR, []byte("https://example.org/pixels"))
	expect(t, check(t, ds), pixelData, "", "a Pixel Data Provider URL instead")
}

// "Required if Extended Offset Table (7FE0,0001) is present", with no "May be
// present otherwise": the lengths are required with the table, and must not
// be there without it (PS3.5 7.4.2).
func TestAConditionThatAnAttributeIsPresentIsEvaluated(t *testing.T) {
	lengths := "(7FE0,0002)"
	expect(t, check(t, corpusFile(t, "CT_small.dcm")), lengths, "", "neither")

	ds := corpusFile(t, "CT_small.dcm")
	put(t, ds, tag.New(0x7FE0, 0x0001), dataelem.OV, make([]byte, 8))
	expect(t, check(t, ds), lengths, "error Type 1C absent; the condition holds", "the table without its lengths")

	put(t, ds, tag.New(0x7FE0, 0x0002), dataelem.OV, make([]byte, 8))
	expect(t, check(t, ds), lengths, "", "both")

	ds.Remove(tag.New(0x7FE0, 0x0001))
	expect(t, check(t, ds), lengths,
		"error Type 1C present; the condition does not hold, and PS3.3 does not say it may be present otherwise",
		"the lengths without the table")
}

// "Required if Multi-energy CT Acquisition (0018,9361) is YES", a condition on
// a module of the CT Image IOD (Table A.3-1): with YES and none of the
// Multi-energy CT Image module there, the module is reported absent; with NO,
// nothing is said; and the module is no longer reported as not checked.
func TestAConditionOnAValueDecidesAModule(t *testing.T) {
	module := "Multi-energy CT Image"
	ds := corpusFile(t, "CT_small.dcm")
	expect(t, check(t, ds), module, "", "no Multi-energy CT Acquisition")

	put(t, ds, tag.New(0x0018, 0x9361), dataelem.CS, []byte("YES "))
	expect(t, check(t, ds), module,
		"error Usage C absent; the condition holds, so the module is required: Required if Multi-energy CT Acquisition (0018,9361) is YES.",
		"Multi-energy CT Acquisition YES")

	put(t, ds, tag.New(0x0018, 0x9361), dataelem.CS, []byte("NO"))
	expect(t, check(t, ds), module, "", "Multi-energy CT Acquisition NO")
}

// "Required if Image Type (0008,0008) Value 1 is ORIGINAL or MIXED. May be
// present otherwise.", the MR Pulse Sequence module of the Enhanced MR Image
// IOD (Table A.36-2): the condition reads the first value only, and a module
// that is there when it does not hold is allowed.
func TestAConditionOnOneValueOfSeveral(t *testing.T) {
	module := "MR Pulse Sequence"
	ds := dataset.NewDataset()
	put(t, ds, tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.4.1\x00"))
	put(t, ds, tag.New(0x0008, 0x0008), dataelem.CS, []byte(`ORIGINAL\PRIMARY\M_SE\NONE`))
	expect(t, check(t, ds), module, "error Usage C absent; the condition holds", "Value 1 ORIGINAL")

	put(t, ds, tag.New(0x0008, 0x0008), dataelem.CS, []byte(`DERIVED\ORIGINAL\M_SE\NONE`))
	expect(t, check(t, ds), module, "", "Value 1 DERIVED, ORIGINAL second")

	put(t, ds, tag.New(0x0018, 0x9005), dataelem.SH, []byte("SE")) // Pulse Sequence Name
	expect(t, check(t, ds), module, "", "the module there when not required: May be present otherwise")
}

// "Required if Partial View (0028,1350) is present with a Value of YES", the
// Breast View module of the Breast Tomosynthesis Image IOD: required with
// YES, not with NO or with no Partial View, and not allowed with NO.
func TestAConditionThatAnAttributeIsPresentWithAValue(t *testing.T) {
	codes := "(0028,1352)"
	ds := dataset.NewDataset()
	put(t, ds, tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.13.1.3"))
	expect(t, check(t, ds), codes, "", "no Partial View")

	put(t, ds, tag.New(0x0028, 0x1350), dataelem.CS, []byte("YES "))
	expect(t, check(t, ds), codes, "error Type 1C absent; the condition holds", "Partial View YES")

	put(t, ds, tag.New(0x0028, 0x1350), dataelem.CS, []byte("NO"))
	expect(t, check(t, ds), codes, "", "Partial View NO")

	seq := sequence.New()
	item := dataset.NewDataset()
	_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("1")))
	_ = seq.Append(item)
	_ = ds.AddSequence(tag.New(0x0028, 0x1352), seq)
	expect(t, check(t, ds), codes, "error Type 1C present; the condition does not hold", "codes with Partial View NO")
}

// A condition in an item is evaluated against the item: the Code Sequence
// Macro's "Required if Context Identifier (0008,010F) is present" holds in
// the item of Anatomic Region Sequence that has a Context Identifier and not
// in the one beside it, and the finding names the item.
func TestAConditionInAnItemIsEvaluatedInTheItem(t *testing.T) {
	ds := corpusFile(t, "CT_small.dcm")
	seq := sequence.New()
	for _, withContext := range []bool{false, true} {
		item := dataset.NewDataset()
		_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, []byte("T-D3000")))
		_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0102), dataelem.SH, []byte("SRT ")))
		_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0104), dataelem.LO, []byte("Chest ")))
		if withContext {
			_ = item.Add(dataelem.NewDataElement(tag.New(0x0008, 0x010F), dataelem.CS, []byte("4031")))
		}
		_ = seq.Append(item)
	}
	ds.Remove(tag.New(0x0008, 0x2218))
	_ = ds.AddSequence(tag.New(0x0008, 0x2218), seq) // Anatomic Region Sequence, General Image module
	var got []string
	for _, f := range check(t, ds).Findings {
		if f.Tag == "(0008,0105)" || f.Tag == "(0008,010F)" && f.Severity != validate.NotChecked {
			got = append(got, f.Severity.String()+" "+f.Path)
		}
	}
	if want := "error (0008,2218)[2] (0008,0105)"; strings.Join(got, "|") != want {
		t.Errorf("Mapping Resource: %q; want only %q", got, want)
	}
}

// A condition the generator did not read as data stays prose, reported as
// not checked with its text: a sentence that matches a form only in part
// (Planar Configuration's "has a Value greater than 1"), one that joins two
// tests ("is a non-human organism and if ... is not present"), and one whose
// name and tag disagree are each left with no Condition.
func TestAConditionThatIsNotOfARecognizedFormStaysProse(t *testing.T) {
	r := check(t, corpusFile(t, "CT_small.dcm"))
	expect(t, r, "(0028,0006)",
		"not checked Type 1C absent; whether it is required was not checked: Required if Samples per Pixel (0028,0002) has a Value greater than 1.",
		"Planar Configuration")
	expect(t, r, "(0010,2201)", "not checked Type 1C absent; whether it is required was not checked: Required if the Patient is a non-human organism",
		"Patient Species Description")

	for _, c := range []struct{ table, name string }{
		{"C.7-11c", "Planar Configuration"},               // "has a Value greater than 1"
		{"C.7-1", "Patient Species Description"},          // two tests joined
		{"C.7-1", "De-identification Method"},             // "is present and has a Value of YES and ... is not present"
		{"C.8-38", "Reported Values Origin"},              // "Value 3 of Image Type": not the form "Image Type Value 3"
		{"C.21.2-1", "Fiducial Identifier Code Sequence"}, // names Identifier (0070,0310), which the tables call another name
	} {
		table, ok := validate.LookupTable(c.table)
		if !ok {
			t.Fatalf("no table %s", c.table)
		}
		found := false
		for _, r := range table.Rows {
			if r.Name == c.name {
				found = true
				if r.When != nil {
					t.Errorf("%s %s: read as %+v from %q; it is prose", c.table, c.name, *r.When, r.Condition)
				}
			}
		}
		if !found {
			t.Errorf("%s has no %s", c.table, c.name)
		}
	}
}

// A data set with no SOP Class UID, or one this package has no rules for, is
// refused by name rather than passed.
func TestAnUnknownSOPClassIsRefused(t *testing.T) {
	ds := dataset.NewDataset()
	if _, err := validate.Dataset(ds); err == nil || !strings.Contains(err.Error(), "SOP Class UID") {
		t.Errorf("no SOP class: %v", err)
	}
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.3.4")))
	if _, err := validate.Dataset(ds); err == nil || !strings.Contains(err.Error(), "1.2.3.4") {
		t.Errorf("unknown SOP class: %v", err)
	}
}

// The generated rules hold together: every SOP class names an IOD, every
// module an IOD uses has its table, and every table a row includes exists.
// A table that does not resolve is a module the checker would pass over.
func TestTheRulesResolve(t *testing.T) {
	for _, uid := range []string{"1.2.840.10008.5.1.4.1.1.2", "1.2.840.10008.5.1.4.1.1.4", "1.2.840.10008.5.1.4.1.1.7"} {
		iod, ok := validate.IODForSOPClass(uid)
		if !ok {
			t.Fatalf("no IOD for %s", uid)
		}
		seen := map[string]bool{}
		var walk func(label string)
		walk = func(label string) {
			if seen[label] {
				return
			}
			seen[label] = true
			table, ok := validate.LookupTable(label)
			if !ok {
				t.Errorf("%s: table %s is not there", iod.Name, label)
				return
			}
			for _, r := range table.Rows {
				if r.Include != "" {
					walk(r.Include)
				}
			}
		}
		for _, m := range iod.Modules {
			walk(m.Table)
		}
		if len(seen) < 10 {
			t.Errorf("%s: only %d tables reached", iod.Name, len(seen))
		}
	}
}

// Every file of pydicom's corpus is checked or refused by name: none makes
// the checker fail, and every one with a known SOP class gets a report.
func TestEveryCorpusFileIsChecked(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	checked := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			continue
		}
		r, err := validate.Dataset(df.GetDataset())
		if err != nil {
			if !strings.Contains(err.Error(), "SOP Class UID") && !strings.Contains(err.Error(), "no IOD rules") {
				t.Errorf("%s: %v", filepath.Base(path), err)
			}
			continue
		}
		if r.IOD == "" {
			t.Errorf("%s: no IOD in the report", filepath.Base(path))
		}
		checked++
	}
	if checked < 60 {
		t.Errorf("only %d corpus files checked", checked)
	}
	t.Logf("%d corpus files checked", checked)
}
