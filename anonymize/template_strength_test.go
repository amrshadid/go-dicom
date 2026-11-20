package anonymize_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
)

// survivors lists the attributes, at any depth, that still hold the value they
// were read with and are not empty: what de-identification left of the
// original. Patient ID and the UIDs are compared by value like the rest, so a
// replaced one does not count.
func survivors(ds *dataset.Dataset, original map[string][]byte, path string, into map[string]bool) {
	for _, elem := range ds.GetAll() {
		tg, _ := elem.Tag()
		at := path + tg.String()
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for i, item := range seq.Items() {
				if inner, isDS := item.(*dataset.Dataset); isDS {
					survivors(inner, original, fmt.Sprintf("%s[%d].", at, i), into)
				}
			}
			continue
		}
		raw, ok := elem.Bytes()
		if !ok || len(raw) == 0 {
			continue
		}
		if was, had := original[at]; had && string(was) == string(raw) {
			into[at] = true
		}
	}
}

// templateSurvivors de-identifies the file once with the Basic Profile and
// once with an anonymizer given BasicProfileTemplate, and returns the
// attributes that keep their original value in the second and not in the
// first.
func templateSurvivors(t *testing.T, path string) (extra []string, ok bool) {
	t.Helper()
	plain, err := readDataset(t, path)
	if err != nil {
		return nil, false
	}
	templated, _ := readDataset(t, path)
	original := map[string][]byte{}
	originalValues(plain, "", original)

	if err := anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(plain); err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	a.ApplyTemplate(anonymize.BasicProfileTemplate())
	if err := a.Anonymize(templated); err != nil {
		t.Fatalf("%s with the template: %v", filepath.Base(path), err)
	}

	kept, keptWithTemplate := map[string]bool{}, map[string]bool{}
	survivors(plain, original, "", kept)
	survivors(templated, original, "", keptWithTemplate)
	for at := range keptWithTemplate {
		if !kept[at] {
			extra = append(extra, at)
		}
	}
	sort.Strings(extra)
	return extra, true
}

// TestBasicProfileTemplateIsAsStrongAsTheBasicProfile proves that an
// anonymizer given BasicProfileTemplate keeps no attribute, in any of
// pydicom's files, that one built with the Basic Profile does not.
//
// The template was built from a 39-entry table and applied as custom
// actions, which take priority over the 653-attribute table the profile
// consults, so Patient's Sex, Age, Size and Weight, the Manufacturer and
// others survived in a file the program called de-identified.
func TestBasicProfileTemplateIsAsStrongAsTheBasicProfile(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	paths := []string{identifyingFile(t)}
	if dir != "" {
		corpus, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
		for _, p := range corpus {
			if info, err := os.Stat(p); err == nil && info.Size() < 1<<20 {
				paths = append(paths, p)
			}
		}
	}

	compared, weaker := 0, 0
	for _, path := range paths {
		extra, ok := templateSurvivors(t, path)
		if !ok {
			continue
		}
		compared++
		if len(extra) > 0 {
			weaker++
			t.Errorf("%s: kept with the template, removed by the profile: %v", filepath.Base(path), extra)
		}
	}
	if compared == 0 {
		t.Fatal("no file was compared")
	}
	t.Logf("%d files compared, %d de-identified more weakly through the template", compared, weaker)
}

// TestDerivedTemplatesStayAtLeastAsStrongAsTheBasicProfile proves the three
// templates built on BasicProfileTemplate each remove or empty everything the
// Basic Profile removes, except the attributes the template is for.
//
// They were built on the weak table too, so each carried the same gaps.
func TestDerivedTemplatesStayAtLeastAsStrongAsTheBasicProfile(t *testing.T) {
	templates := map[string]func() *anonymize.Template{
		"clean descriptors": anonymize.CleanDescriptorsTemplate,
		"retain dates":      anonymize.RetainDatesTemplate,
		"retain device":     anonymize.RetainDeviceTemplate,
	}
	// What each template exists to keep, which the profile does not.
	allowed := map[string]map[string]bool{
		"clean descriptors": {"(0008,1030)": true, "(0008,103E)": true},
		"retain dates":      {"(0008,0020)": true, "(0008,0030)": true},
		"retain device":     {"(0018,1000)": true, "(0008,1010)": true},
	}

	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	paths := []string{identifyingFile(t)}
	if dir != "" {
		corpus, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
		for _, p := range corpus {
			if info, err := os.Stat(p); err == nil && info.Size() < 1<<20 {
				paths = append(paths, p)
			}
		}
	}

	for name, build := range templates {
		for _, path := range paths {
			plain, err := readDataset(t, path)
			if err != nil {
				continue
			}
			templated, _ := readDataset(t, path)
			original := map[string][]byte{}
			originalValues(plain, "", original)
			if err := anonymize.NewAnonymizer(anonymize.BasicProfile).Anonymize(plain); err != nil {
				t.Fatal(err)
			}
			a := anonymize.NewAnonymizer(anonymize.BasicProfile)
			a.ApplyTemplate(build())
			if err := a.Anonymize(templated); err != nil {
				t.Fatal(err)
			}
			kept, keptWithTemplate := map[string]bool{}, map[string]bool{}
			survivors(plain, original, "", kept)
			survivors(templated, original, "", keptWithTemplate)
			for at := range keptWithTemplate {
				leaf := at
				if i := strings.LastIndex(at, "]."); i >= 0 {
					leaf = at[i+2:]
				}
				if !kept[at] && !allowed[name][leaf] {
					t.Errorf("%s, %s template: %s kept, the profile removes it", filepath.Base(path), name, at)
				}
			}
		}
	}
}
