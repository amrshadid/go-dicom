package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
	"github.com/amrshadid/go-dicom/valuerep"
)

// CreateCommand makes a DICOM file from DICOM JSON, a picture or a PDF, or
// one file for each object of an array of DICOM JSON.
type CreateCommand struct {
	from     string
	syntax   string
	template string
	burnedIn string
	title    string
	bulkDir  string
	help     bool

	// Patient and study attributes, each a tag, VR and value given by a flag.
	values map[string]*string
}

// createFields are the attributes a flag may set, with the flag's name. They
// are the Patient and General Study modules' identifying attributes (PS3.3
// C.7.1.1, C.7.2.1), and the series description and modality of the new
// series.
var createFields = []struct {
	flag, usage string
	tag         tag.Tag
	vr          dataelem.VR
}{
	{"patient-name", "Patient's Name, as Family^Given", tag.New(0x0010, 0x0010), dataelem.PN},
	{"patient-id", "Patient ID", tag.New(0x0010, 0x0020), dataelem.LO},
	{"birth-date", "Patient's Birth Date, YYYYMMDD", tag.New(0x0010, 0x0030), dataelem.DA},
	{"sex", "Patient's Sex: M, F or O", tag.New(0x0010, 0x0040), dataelem.CS},
	{"study-uid", "Study Instance UID of the study to file into (default: a new study)", tag.New(0x0020, 0x000D), dataelem.UI},
	{"study-id", "Study ID", tag.New(0x0020, 0x0010), dataelem.SH},
	{"accession", "Accession Number", tag.New(0x0008, 0x0050), dataelem.SH},
	{"study-date", "Study Date, YYYYMMDD (default for a new study: today)", tag.New(0x0008, 0x0020), dataelem.DA},
	{"study-time", "Study Time, HHMMSS", tag.New(0x0008, 0x0030), dataelem.TM},
	{"study-description", "Study Description", tag.New(0x0008, 0x1030), dataelem.LO},
	{"referring-physician", "Referring Physician's Name", tag.New(0x0008, 0x0090), dataelem.PN},
	{"series-description", "Series Description", tag.New(0x0008, 0x103E), dataelem.LO},
	{"modality", "Modality of the new series (default: OT for a picture, DOC for a PDF)", tag.New(0x0008, 0x0060), dataelem.CS},
}

// templateStudyTags are what a template contributes besides group 0010, the
// Patient module: the General Study module (PS3.3 C.7.2.1), so the new
// instance files into the template's study.
var templateStudyTags = []tag.Tag{
	tag.New(0x0020, 0x000D), // Study Instance UID
	tag.New(0x0008, 0x0020), // Study Date
	tag.New(0x0008, 0x0030), // Study Time
	tag.New(0x0008, 0x0090), // Referring Physician's Name
	tag.New(0x0008, 0x0096), // Referring Physician Identification Sequence
	tag.New(0x0020, 0x0010), // Study ID
	tag.New(0x0008, 0x0050), // Accession Number
	tag.New(0x0008, 0x0051), // Issuer of Accession Number Sequence
	tag.New(0x0008, 0x1030), // Study Description
	tag.New(0x0008, 0x1048), // Physician(s) of Record
	tag.New(0x0008, 0x1060), // Name of Physician(s) Reading Study
	tag.New(0x0008, 0x1032), // Procedure Code Sequence
	tag.New(0x0008, 0x1110), // Referenced Study Sequence
}

// Name returns the command name.
func (cc *CreateCommand) Name() string { return "create" }

// Description returns the command description.
func (cc *CreateCommand) Description() string {
	return "Make a DICOM file from DICOM JSON, a PNG or JPEG picture, or a PDF"
}

// AddFlags adds command flags to the flag set.
func (cc *CreateCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&cc.help, "h", false, "Show help for this command")
	fs.BoolVar(&cc.help, "help", false, "Show help for this command")
	fs.StringVar(&cc.from, "from", "", "What the source is: json, picture or pdf (default: from its extension)")
	fs.StringVar(&cc.syntax, "syntax", "", "For json: the transfer syntax its pixel data is in (default: Explicit VR Little Endian)")
	fs.StringVar(&cc.template, "template", "", "A DICOM file whose patient and study the new instance joins")
	fs.StringVar(&cc.bulkDir, "bulk-dir", "", "For json: the directory a BulkDataURI may name a file in (default: the JSON file's)")
	fs.StringVar(&cc.burnedIn, "burned-in", "yes", "For pdf: whether the document names the patient, yes or no")
	fs.StringVar(&cc.title, "title", "", "For pdf: Document Title (default: the file's name)")
	cc.values = make(map[string]*string, len(createFields))
	for _, f := range createFields {
		cc.values[f.flag] = fs.String(f.flag, "", f.usage)
	}
}

// Execute runs the create command.
func (cc *CreateCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	cc.AddFlags(flags)
	flags.SetOutput(io.Discard)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("create: %v\nRun 'go-dicom create -h' for usage", err)
	}
	if cc.help {
		cc.showHelp()
		return nil
	}
	if len(positional) != 2 {
		return fmt.Errorf("create: expected a source and an output\nRun 'go-dicom create -h' for usage")
	}
	source, output := positional[0], positional[1]
	if sameFile(source, output) {
		return fmt.Errorf("create: the output is the source")
	}
	kind, err := sourceKind(cc.from, source)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if cc.syntax != "" && kind != "json" {
		return fmt.Errorf("create: -syntax is for json; a %s is written in the syntax it needs", kind)
	}
	if cc.bulkDir != "" && kind != "json" {
		return fmt.Errorf("create: -bulk-dir is for json")
	}
	if kind != "pdf" && (cc.title != "" || cc.burnedIn != "yes") {
		return fmt.Errorf("create: -title and -burned-in are for pdf")
	}

	// The patient and study first: a refused value is reported before the
	// source is read.
	patient, err := cc.patientAndStudy()
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}

	var all []*created
	switch kind {
	case "json":
		all, err = createFromJSON(source, cc.syntax, cc.bulkDir)
	case "picture":
		var made *created
		made, err = createFromPicture(source)
		all = []*created{made}
	case "pdf":
		var made *created
		made, err = createFromPDF(source, cc.title, cc.burnedIn)
		all = []*created{made}
	}
	if err != nil {
		return fmt.Errorf("create: %s: %w", source, err)
	}

	// Every object is made ready, and every name settled, before the first
	// file is written, so a refused one leaves nothing behind.
	many := len(all) > 1
	files := make([]outputFile, len(all))
	for i, made := range all {
		meta, err := finishInstance(made, kind, source, patient, objectLabel(i, many))
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		files[i] = outputFile{path: output, meta: meta, made: made}
	}
	if many {
		if info, err := os.Stat(output); err == nil && !info.IsDir() {
			return fmt.Errorf("create: %s holds %d objects, one file each, so the output is a directory and %s is a file", source, len(all), output)
		}
		seen := make(map[string]int, len(files))
		for i := range files {
			name := files[i].meta.MediaStorageSOPInstanceUID
			// The UID is the file's name, so it is held to the form of a UID
			// first: a value with a slash in it is a path.
			if err := valuerep.ValidateUID(name); err != nil {
				return fmt.Errorf("create: object %d has the SOP Instance UID %q, which is not a UID and cannot name a file", i+1, name)
			}
			if first, ok := seen[name]; ok {
				return fmt.Errorf("create: objects %d and %d have the same SOP Instance UID %s, so they would be one file", first, i+1, name)
			}
			seen[name] = i + 1
			files[i].path = filepath.Join(output, name+".dcm")
		}
	}
	for _, f := range files {
		if err := writeDICOMInstance(f.path, f.meta, f.made.ds); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		fmt.Printf("Wrote %s: %s, %s\n", f.path, f.made.what, f.meta.TransferSyntaxUID)
	}
	if many {
		fmt.Printf("Wrote %d files to %s\n", len(files), output)
	}
	return nil
}

// outputFile is one file create is about to write.
type outputFile struct {
	path string
	meta *filewriter.FileMetaInfo
	made *created
}

// objectLabel names an object of an array in a message, or nothing when the
// source made only one.
func objectLabel(i int, many bool) string {
	if !many {
		return ""
	}
	return fmt.Sprintf("object %d: ", i+1)
}

// finishInstance gives a made data set what the write needs and returns its
// file meta: the patient and study the template and the flags say, which
// replace what the source said (a JSON source keeps what they leave), the
// identifiers a picture or a PDF is new in, and the UTF-8 declaration.
func finishInstance(made *created, kind, source string, patient *dataset.Dataset, label string) (*filewriter.FileMetaInfo, error) {
	ds := made.ds
	for _, elem := range patient.GetAll() {
		_ = ds.Add(cloneElement(elem))
	}
	if kind != "json" {
		fillNewInstance(ds, made.modality)
	}
	if !ds.Contains(sopInstanceUIDTag) {
		u, err := newInstanceUID(ds)
		if err != nil {
			return nil, fmt.Errorf("%s%w", label, err)
		}
		fmt.Printf("  %sthe source has no SOP Instance UID; it is given %s\n", label, u)
	}
	if datasetText(ds, sopClassUIDTag) == "" {
		return nil, fmt.Errorf("%s%s has no SOP Class UID, so it is no kind of instance", label, source)
	}
	declareUTF8IfNeeded(ds)
	return &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    datasetText(ds, sopClassUIDTag),
		MediaStorageSOPInstanceUID: datasetText(ds, sopInstanceUIDTag),
		TransferSyntaxUID:          made.syntax,
	}, nil
}

// created is what a source made: the data set, the syntax it is written in,
// and, for a picture or a PDF, the modality its series takes by default.
type created struct {
	ds       *dataset.Dataset
	syntax   string
	modality string
	what     string
}

// sourceKind is -from, or the source's extension.
func sourceKind(from, source string) (string, error) {
	switch strings.ToLower(from) {
	case "json", "picture", "pdf":
		return strings.ToLower(from), nil
	case "":
	default:
		return "", fmt.Errorf("-from %q: the sources are json, picture and pdf", from)
	}
	switch strings.ToLower(filepath.Ext(source)) {
	case ".json":
		return "json", nil
	case ".png", ".jpg", ".jpeg":
		return "picture", nil
	case ".pdf":
		return "pdf", nil
	}
	return "", fmt.Errorf("%s: the extension does not say what it is; give -from json, picture or pdf", source)
}

// patientAndStudy gathers the template's patient and study, and the flags
// over them, as a data set of the elements to put in the new instance.
func (cc *CreateCommand) patientAndStudy() (*dataset.Dataset, error) {
	out := dataset.NewDataset()
	if cc.template != "" {
		df, err := readInstance(cc.template)
		if err != nil {
			return nil, fmt.Errorf("-template %s: %w", cc.template, err)
		}
		tmpl := df.GetDataset()
		if tmpl.Length() == 0 {
			return nil, fmt.Errorf("-template %s does not look like a DICOM file", cc.template)
		}
		for _, elem := range tmpl.GetAll() {
			t, ok := elem.Tag()
			if ok && t.Group() == 0x0010 {
				_ = out.Add(cloneElement(elem))
			}
		}
		for _, t := range templateStudyTags {
			if elem, ok := tmpl.Get(t); ok {
				_ = out.Add(cloneElement(elem))
			}
		}
		if !out.Contains(tag.New(0x0020, 0x000D)) {
			return nil, fmt.Errorf("-template %s has no Study Instance UID to join", cc.template)
		}
	}
	for _, f := range createFields {
		value := strings.TrimSpace(*cc.values[f.flag])
		if value == "" {
			continue
		}
		if err := valuerep.ValidateValue(string(f.vr), value); err != nil {
			return nil, fmt.Errorf("-%s %q: %v", f.flag, value, err)
		}
		if f.flag == "sex" && value != "M" && value != "F" && value != "O" {
			return nil, fmt.Errorf("-sex %q: M, F or O (PS3.3 C.7.1.1)", value)
		}
		setText(out, f.tag, f.vr, value)
	}
	return out, nil
}

// cloneElement copies an element, items and all, so the template's data set
// is not shared with the new one.
func cloneElement(elem *dataelem.DataElement) *dataelem.DataElement {
	t, _ := elem.Tag()
	holder := dataset.NewDataset()
	_ = holder.Add(elem)
	copied, _ := holder.Clone().Get(t)
	return copied
}

// fillNewInstance gives a picture or a PDF what its IOD requires that the
// source cannot say: a study when none was given, a new series and instance,
// the dates of its making, and the Type 2 attributes of the Patient and
// General Study modules, present and empty when nothing gave them a value
// (PS3.3 C.7.1.1, C.7.2.1, C.7.3.1, C.12.1).
func fillNewInstance(ds *dataset.Dataset, modality string) {
	now := time.Now()
	date, clock := now.Format("20060102"), now.Format("150405")
	newStudy := !ds.Contains(tag.New(0x0020, 0x000D))
	if newStudy {
		setText(ds, tag.New(0x0020, 0x000D), dataelem.UI, uid.Generate())
		if !ds.Contains(tag.New(0x0008, 0x0020)) {
			setText(ds, tag.New(0x0008, 0x0020), dataelem.DA, date)
		}
		if !ds.Contains(tag.New(0x0008, 0x0030)) {
			setText(ds, tag.New(0x0008, 0x0030), dataelem.TM, clock)
		}
	}
	setText(ds, tag.New(0x0020, 0x000E), dataelem.UI, uid.Generate())
	setText(ds, sopInstanceUIDTag, dataelem.UI, uid.Generate())
	if !ds.Contains(tag.New(0x0008, 0x0060)) {
		setText(ds, tag.New(0x0008, 0x0060), dataelem.CS, modality)
	}
	setText(ds, tag.New(0x0008, 0x0012), dataelem.DA, date) // Instance Creation Date
	setText(ds, tag.New(0x0008, 0x0013), dataelem.TM, clock)
	setText(ds, tag.New(0x0008, 0x0023), dataelem.DA, date) // Content Date
	setText(ds, tag.New(0x0008, 0x0033), dataelem.TM, clock)
	for _, empty := range []struct {
		t  tag.Tag
		vr dataelem.VR
	}{
		{tag.New(0x0010, 0x0010), dataelem.PN}, {tag.New(0x0010, 0x0020), dataelem.LO},
		{tag.New(0x0010, 0x0030), dataelem.DA}, {tag.New(0x0010, 0x0040), dataelem.CS},
		{tag.New(0x0008, 0x0020), dataelem.DA}, {tag.New(0x0008, 0x0030), dataelem.TM},
		{tag.New(0x0008, 0x0090), dataelem.PN}, {tag.New(0x0020, 0x0010), dataelem.SH},
		{tag.New(0x0008, 0x0050), dataelem.SH},
	} {
		if !ds.Contains(empty.t) {
			_ = ds.Add(dataelem.NewDataElement(empty.t, empty.vr, []byte{}))
		}
	}
	// SC Equipment, Conversion Type (0008,0064), Type 1 in both IODs: made
	// on a workstation (PS3.3 C.8.6.1).
	setText(ds, tag.New(0x0008, 0x0064), dataelem.CS, "WSD")
}

// setText sets a text attribute padded to an even length, a UID with a NUL
// and anything else with a space (PS3.5 7.1.1).
func setText(ds *dataset.Dataset, t tag.Tag, vr dataelem.VR, value string) {
	b := []byte(value)
	if len(b)%2 == 1 {
		if vr == dataelem.UI {
			b = append(b, 0)
		} else {
			b = append(b, ' ')
		}
	}
	ds.Remove(t)
	_ = ds.Add(dataelem.NewDataElement(t, vr, b))
}

// declareUTF8IfNeeded says the text is UTF-8, which is what flags and a
// template read by this library hold, when any of it is not ASCII and
// nothing says so already.
func declareUTF8IfNeeded(ds *dataset.Dataset) {
	if datasetText(ds, tag.New(0x0008, 0x0005)) != "" || !nonASCIIText(ds) {
		return
	}
	setText(ds, tag.New(0x0008, 0x0005), dataelem.CS, "ISO_IR 192")
}

func nonASCIIText(ds *dataset.Dataset) bool {
	for _, elem := range ds.GetAll() {
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			for _, item := range seq.Items() {
				if inner, ok := item.(*dataset.Dataset); ok && nonASCIIText(inner) {
					return true
				}
			}
			continue
		}
		raw, ok := elem.GetValue().([]byte)
		if !ok || !dataelem.IsCharacterSetVR(elem.GetVR()) {
			continue
		}
		for _, b := range raw {
			if b > 0x7F {
				return true
			}
		}
	}
	return false
}

func (cc *CreateCommand) showHelp() {
	printHelp(`Make a DICOM file from DICOM JSON, a PNG or JPEG picture, or a PDF

USAGE:
  go-dicom create [options] <source> <output>
  go-dicom create [options] <array.json> <directory>

  What the source is comes from its extension (.json, .png, .jpg, .jpeg,
  .pdf), or from -from.

SOURCES:
  json      A DICOM JSON object (PS3.18 Annex F), or an array of them, as a
            DICOMweb metadata response or 'convert --format dicom-json'
            writes it. An array of one object makes <output>, as an object
            does. An array of more makes one file for each, in the directory
            <output> (made if it is not there), named <SOP Instance UID>.dcm
            as 'wado' names them; two objects with one UID, or an object that
            is refused, make no file at all. Binary values come from
            InlineBinary, or from a BulkDataURI that is a file: URI or a path
            (relative to -bulk-dir, by default the JSON file's directory):
            the whole file is the value, in the little endian a data set
            holds it, and for encapsulated Pixel Data it holds the fragments
            with their item headers. A BulkDataURI is read only for a binary
            VR, and only from inside -bulk-dir, links followed. A http: or
            https: BulkDataURI is refused and not fetched: a command that
            makes a file does not reach the network unasked. Fetch it first
            (curl) and name the file. The JSON has no transfer syntax,
            so its pixel data is taken as native unless -syntax names another
            (one for every object). A missing SOP Instance UID is made;
            everything else is the JSON's.
  picture   A Secondary Capture image (PS3.3 A.8.1). A PNG is decoded: gray
            at 8 or 16 bits is MONOCHROME2, color and palette images RGB, and
            an image that is not fully opaque is refused, since DICOM has no
            transparency. A JPEG is put in the file as it is, compressed, in
            JPEG Baseline (1.2.840.10008.1.2.4.50), with nothing decoded or
            encoded again, and recorded as lossily compressed; only a baseline
            8-bit JPEG fits that syntax, and a progressive one is refused.
  pdf       An Encapsulated PDF (PS3.3 A.45.1), the document's bytes as they
            are, its length in Encapsulated Document Length.

OPTIONS:
  -from json|picture|pdf   What the source is, when the extension does not say.
  -template <file.dcm>     Join the patient and study of this file: its
                           Patient module (group 0010) and its General Study
                           attributes are copied.
  -syntax <uid>            For json: the transfer syntax of its pixel data.
  -bulk-dir <dir>          For json: the directory a BulkDataURI may name a
                           file in (default: the JSON file's directory).
  -title <text>            For pdf: Document Title (default: the file's name).
  -burned-in yes|no        For pdf: whether the document identifies the patient
                           (Burned In Annotation, Type 1; default yes).
  -h, -help                Show this help message

PATIENT AND STUDY (each replaces the template's or the JSON's value):
  -patient-name, -patient-id, -birth-date, -sex, -study-uid, -study-id,
  -accession, -study-date, -study-time, -study-description,
  -referring-physician, -series-description, -modality

  A picture or a PDF is a new instance in a new series: Series and SOP
  Instance UIDs are made with the library's generator (2.25 UUID UIDs), and
  so is a Study Instance UID when neither -template nor -study-uid gives
  one. Patient and study attributes nothing gives are present and empty, as
  their Type 2 asks.

EXAMPLES:
  go-dicom convert scan.dcm scan.json --format dicom-json
  go-dicom create scan.json copy.dcm
  go-dicom wado -study 1.2.840.1 -metadata <url> > study.json
  go-dicom create study.json instances/        # one file for each instance
  go-dicom create -template ct.dcm screenshot.png sc.dcm
  go-dicom create -patient-name 'Doe^Jane' -patient-id 12345 photo.jpg sc.dcm
  go-dicom create -template ct.dcm -title 'Radiology report' report.pdf report.dcm
`)
}
