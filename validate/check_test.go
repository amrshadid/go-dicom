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
