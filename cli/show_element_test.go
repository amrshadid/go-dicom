package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// writeShowFixture writes a small file that has the elements the help text of
// show names: Patient's Name, Study Instance UID and Image Comments, and a
// private element.
func writeShowFixture(t *testing.T) string {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	add := func(tg tag.Tag, vr dataelem.VR, value interface{}) {
		if err := ds.Add(dataelem.NewDataElement(tg, vr, value)); err != nil {
			t.Fatal(err)
		}
	}
	add(tagSOPClassUID, dataelem.UI, "1.2.840.10008.5.1.4.1.1.7")
	add(tagSOPInstanceUID, dataelem.UI, "1.2.826.0.1.3680043.10.511.384.1")
	add(tagPatientName, dataelem.PN, "Doe^John")
	add(tagStudyUID, dataelem.UI, testStudyUID)
	add(tag.New(0x0020, 0x4000), dataelem.LT, "a comment")
	add(tag.New(0x0009, 0x0010), dataelem.LO, "ACME")
	add(tag.New(0x0009, 0x1001), dataelem.LO, "private value")
	path := filepath.Join(t.TempDir(), "patient.dcm")
	if err := writeDICOMFile(path, "1.2.840.10008.5.1.4.1.1.7", "1.2.826.0.1.3680043.10.511.384.1", ds); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestShowElementTakesAKeywordOrATag proves file::Element finds an element by
// its keyword, by its tag in each form people write it, and by the dictionary
// name it matched before, and that all of them print the same row. Before, the
// keyword PatientName never matched, because the comparison was with the name
// "Patient's Name".
func TestShowElementTakesAKeywordOrATag(t *testing.T) {
	path := writeShowFixture(t)
	want := showText(t, path+"::0010,0010")
	if !strings.HasPrefix(want, "0010,0010") || !strings.Contains(want, "Doe^John") {
		t.Fatalf("by tag, show printed %q", want)
	}
	for _, name := range []string{"PatientName", "(0010,0010)", "00100010", "0010,0010", "Patient's Name", "patient's name"} {
		if got := showText(t, path+"::"+name); got != want {
			t.Errorf("show ::%s printed %q, want %q", name, got, want)
		}
	}
	if got := showText(t, path+"::StudyInstanceUID"); !strings.HasPrefix(got, "0020,000D") || !strings.Contains(got, testStudyUID) {
		t.Errorf("show ::StudyInstanceUID printed %q", got)
	}
	if got := showText(t, path+"::ImageComments"); !strings.HasPrefix(got, "0020,4000") || !strings.Contains(got, "a comment") {
		t.Errorf("show ::ImageComments printed %q", got)
	}
}

// TestShowElementNotInTheFileIsReported proves a keyword the dictionary knows
// but the file does not hold, and a word that is neither a keyword nor a tag,
// are both refused by name.
func TestShowElementNotInTheFileIsReported(t *testing.T) {
	path := writeShowFixture(t)
	for _, name := range []string{"Modality", "NotAKeyword", "(0008,0060)"} {
		err := (&ShowCommand{}).Execute([]string{path + "::" + name})
		if err == nil || !strings.Contains(err.Error(), "element '"+name+"' not found") {
			t.Errorf("show ::%s: err = %v, want \"element '%s' not found\"", name, err, name)
		}
	}
}

// TestEveryExampleInShowHelpRuns runs each example in show's help text, with
// the file it names replaced by one that has what the examples ask for, and
// checks what it prints. The help advertised patient.dcm::ImageComments for as
// long as the keyword did not work, and nothing ran the examples.
func TestEveryExampleInShowHelpRuns(t *testing.T) {
	path := writeShowFixture(t)
	help, err := captured(t, func() error { return NewShowCommand().Execute([]string{"-h"}) })
	if err != nil {
		t.Fatal(err)
	}
	_, examples, ok := strings.Cut(help, "EXAMPLES:\n")
	if !ok {
		t.Fatal("show's help has no EXAMPLES")
	}
	line := regexp.MustCompile(`^\s+` + regexp.QuoteMeta(invokedAs) + ` show (.*?)(\s{2,}.*)?$`)
	ran := 0
	for _, text := range strings.Split(examples, "\n") {
		m := line.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		ran++
		args := strings.Fields(strings.ReplaceAll(m[1], "patient.dcm", path))
		t.Run(strings.TrimSpace(m[1]), func(t *testing.T) {
			out, err := captured(t, func() error { return NewShowCommand().Execute(args) })
			if err != nil {
				t.Fatalf("show %s: %v", m[1], err)
			}
			switch {
			case strings.Contains(m[1], "-h"):
				if !strings.Contains(out, "COMMAND: show") {
					t.Errorf("printed %q", out)
				}
			case strings.Contains(m[1], "--json"):
				if !json.Valid([]byte(out)) {
					t.Errorf("printed no JSON: %q", out)
				}
			case strings.Contains(m[1], "::"):
				_, element, _ := strings.Cut(m[1], "::")
				want, err := parseTagOrKeyword(strings.Fields(element)[0])
				if err != nil {
					t.Fatalf("the example names %q: %v", element, err)
				}
				if prefix := fmt.Sprintf("%04X,%04X", want.Group(), want.Element()); !strings.HasPrefix(out, prefix) {
					t.Errorf("printed %q, want a row of %s", out, prefix)
				}
			default:
				if !strings.Contains(out, "0010,0010") {
					t.Errorf("printed no listing of the file: %q", out)
				}
			}
		})
	}
	if ran < 6 {
		t.Errorf("found %d examples in show's help, expected at least 6", ran)
	}
}
