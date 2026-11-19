package cli

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/sr"
	"github.com/amrshadid/go-dicom/tag"
)

// srSchemaVersion is the version of the document `sr --json` prints. It goes
// up when a field is renamed, removed or changes meaning; a field added does
// not change it. The verifying observers, predecessor documents, template
// identifiers and value mappings below were added under that rule: each is
// a field that is absent unless the document has it, so a reader that does
// not know it reads what it always did.
const srSchemaVersion = 1

// SRCommand prints the content tree of a structured report.
type SRCommand struct {
	asJSON bool
	help   bool
	// out is where the document goes; nil means standard output.
	out io.Writer
}

// NewSRCommand creates a new sr command.
func NewSRCommand() *SRCommand {
	return &SRCommand{}
}

// Name returns the command name.
func (sc *SRCommand) Name() string {
	return "sr"
}

// Description returns the command description.
func (sc *SRCommand) Description() string {
	return "Print the content tree of a structured report"
}

// AddFlags adds command flags to the flag set.
func (sc *SRCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&sc.help, "h", false, "Show help for this command")
	fs.BoolVar(&sc.help, "help", false, "Show help for this command")
	fs.BoolVar(&sc.asJSON, "json", false, "Print the report as JSON")
}

// Execute runs the sr command.
func (sc *SRCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("sr", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("sr: failed to parse flags: %v", err)
	}
	if sc.help {
		sc.showHelp()
		return nil
	}
	if len(positional) != 1 {
		return fmt.Errorf("sr: expected one file, got %d\nRun 'go-dicom sr -h' for usage", len(positional))
	}

	doc, err := readSRDocument(positional[0])
	if err != nil {
		return fmt.Errorf("sr: %v", err)
	}
	out := orStdout(sc.out)
	if sc.asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(doc)
	}
	doc.writeText(out)
	return nil
}

// srDocument is what `sr --json` prints.
type srDocument struct {
	SchemaVersion int      `json:"schemaVersion"`
	File          string   `json:"file"`
	Document      srHeader `json:"document"`
	Content       srNode   `json:"content"`
	Warnings      []string `json:"warnings"`
}

// srHeader is what the document says about itself outside its content tree.
type srHeader struct {
	SOPClassUID  string `json:"sopClassUID"`
	SOPClassName string `json:"sopClassName,omitempty"`
	PatientName  string `json:"patientName,omitempty"`
	// CompletionFlag and VerificationFlag are those of the SR Document General
	// Module (0040,A491), (0040,A493).
	CompletionFlag   string `json:"completionFlag,omitempty"`
	VerificationFlag string `json:"verificationFlag,omitempty"`
	// ContentDate and ContentTime are as written, (0008,0023) and (0008,0033).
	ContentDate string `json:"contentDate,omitempty"`
	ContentTime string `json:"contentTime,omitempty"`
	// Template is the Template Identifier of the root item's Content Template
	// Sequence, and its Mapping Resource.
	TemplateID       string `json:"templateID,omitempty"`
	MappingResource  string `json:"mappingResource,omitempty"`
	ContentItemCount int    `json:"contentItems"`
	// VerifyingObservers are the Verifying Observer Sequence (0040,A073), in
	// the order the document gives them.
	VerifyingObservers []srVerifier `json:"verifyingObservers,omitempty"`
	// PredecessorDocuments are the documents this one replaces, the
	// Predecessor Documents Sequence (0040,A360): the studies and series they
	// are in, and the instances.
	PredecessorDocuments []srPredecessor `json:"predecessorDocuments,omitempty"`
}

// srVerifier is one verifying observer (PS3.3 C.17.2).
type srVerifier struct {
	// Name is the Verifying Observer Name (0040,A075), Organization the
	// Verifying Organization (0040,A027), and DateTime the Verification
	// DateTime (0040,A030) as written.
	Name         string `json:"name,omitempty"`
	Organization string `json:"organization,omitempty"`
	DateTime     string `json:"dateTime,omitempty"`
	// Identification is the Verifying Observer Identification Code Sequence
	// (0040,A088), where it has an item.
	Identification *srCode `json:"identification,omitempty"`
}

// srPredecessor is one study of the Predecessor Documents Sequence, with
// the series of it a predecessor is in.
type srPredecessor struct {
	StudyInstanceUID string                `json:"studyInstanceUID"`
	Series           []srPredecessorSeries `json:"series,omitempty"`
}

// srPredecessorSeries is a series of a predecessor study and the instances
// of it that are predecessors.
type srPredecessorSeries struct {
	SeriesInstanceUID string       `json:"seriesInstanceUID"`
	Instances         []srInstance `json:"instances,omitempty"`
}

// srInstance is an instance named by its SOP class and its UID.
type srInstance struct {
	SOPClassUID    string `json:"sopClassUID"`
	SOPClassName   string `json:"sopClassName,omitempty"`
	SOPInstanceUID string `json:"sopInstanceUID"`
}

// srTemplate is the Content Template Sequence (0040,A504) of an item: the
// template its content was built from (PS3.3 C.17.3.2).
type srTemplate struct {
	// ID is the Template Identifier (0040,DB00) and Resource the Mapping
	// Resource (0008,0105) it belongs to.
	ID       string `json:"id"`
	Resource string `json:"resource,omitempty"`
}

// srCode is a coded concept.
type srCode struct {
	Value         string `json:"value"`
	Scheme        string `json:"scheme"`
	SchemeVersion string `json:"schemeVersion,omitempty"`
	Meaning       string `json:"meaning,omitempty"`
}

// srNumeric is the value of a NUM item.
type srNumeric struct {
	// Value is the number as written, Numeric Value (0040,A30A): DS is decimal
	// text, and it is not rounded by being read.
	Value string  `json:"value,omitempty"`
	Units *srCode `json:"units,omitempty"`
	// Qualifier is the Numeric Value Qualifier Code (0040,A301), which says why
	// there is no number, or what kind of number it is.
	Qualifier *srCode `json:"qualifier,omitempty"`
}

// srReference is the instance an IMAGE, WAVEFORM or COMPOSITE item points at.
type srReference struct {
	SOPClassUID    string `json:"sopClassUID"`
	SOPClassName   string `json:"sopClassName,omitempty"`
	SOPInstanceUID string `json:"sopInstanceUID"`
	// Frames are the Referenced Frame Number (0008,1160), Segments the
	// Referenced Segment Number (0062,000B).
	Frames   []int `json:"frames,omitempty"`
	Segments []int `json:"segments,omitempty"`
	// Channels are the Referenced Waveform Channels (0040,A0B0) of a WAVEFORM
	// item, each as a multiplex group and a channel.
	Channels [][2]int `json:"channels,omitempty"`
	// PresentationState is the instance the image is to be shown with.
	PresentationState *srReference `json:"presentationState,omitempty"`
	// ValueMappings are the Real World Value Mapping instances that give
	// the image's stored values their meaning, the Referenced Real World
	// Value Mapping Instance Sequence (0008,114B) of an image reference.
	ValueMappings []srInstance `json:"realWorldValueMappings,omitempty"`
}

// srSCoord is the SCOORD or SCOORD3D value: a shape and its points.
type srSCoord struct {
	GraphicType string `json:"graphicType"`
	// Points are (column, row) for SCOORD and (x, y, z) for SCOORD3D, as the
	// Graphic Data (0070,0022) holds them.
	Points [][]float64 `json:"points"`
	// FrameOfReferenceUID is the Referenced Frame of Reference UID (3006,0024)
	// that gives SCOORD3D points their coordinates.
	FrameOfReferenceUID string `json:"frameOfReferenceUID,omitempty"`
	// PixelOriginInterpretation is (0048,0301), where it is written.
	PixelOriginInterpretation string `json:"pixelOriginInterpretation,omitempty"`
}

// srTCoord is the TCOORD value: a kind of range and what it covers.
type srTCoord struct {
	RangeType string `json:"rangeType"`
	// SamplePositions are the Referenced Sample Positions (0040,A132),
	// TimeOffsets the Referenced Time Offsets (0040,A138), and DateTimes the
	// Referenced DateTime (0040,A13A), each as written.
	SamplePositions []uint32 `json:"samplePositions,omitempty"`
	TimeOffsets     []string `json:"timeOffsets,omitempty"`
	DateTimes       []string `json:"dateTimes,omitempty"`
}

// srNode is one item of the content tree, and the items below it.
type srNode struct {
	// Position is the item's place as dotted ordinals from the root, which is
	// 1: the second child of the root's first child is 1.1.2. A by-reference
	// relationship names its target by it.
	Position string `json:"position"`
	// Relationship is as the file writes it (CONTAINS, HAS OBS CONTEXT, ...),
	// empty for the root.
	Relationship string `json:"relationship,omitempty"`
	// ValueType is CONTAINER, TEXT, NUM and the rest, empty for an item that
	// only refers to another.
	ValueType   string  `json:"valueType,omitempty"`
	ConceptName *srCode `json:"conceptName,omitempty"`
	// ReferencedPosition is the Position of the item a by-reference
	// relationship points at; such an item has no value of its own.
	ReferencedPosition string `json:"referencedPosition,omitempty"`
	// ObservationDateTime is (0040,A032) of the item, as written.
	ObservationDateTime string `json:"observationDateTime,omitempty"`
	// Template is the template the item's content was built from, where it
	// says so: the Content Template Sequence of the item, which a report
	// gives to the root and to the containers it takes from another template.
	Template *srTemplate `json:"template,omitempty"`

	Text       *string      `json:"text,omitempty"`
	Code       *srCode      `json:"code,omitempty"`
	Numeric    *srNumeric   `json:"numeric,omitempty"`
	DateTime   *string      `json:"dateTime,omitempty"`
	UID        *string      `json:"uid,omitempty"`
	PersonName *string      `json:"personName,omitempty"`
	Reference  *srReference `json:"reference,omitempty"`
	Continuity string       `json:"continuityOfContent,omitempty"`
	Spatial    *srSCoord    `json:"spatialCoordinates,omitempty"`
	Temporal   *srTCoord    `json:"temporalCoordinates,omitempty"`
	Children   []srNode     `json:"children,omitempty"`
}

// readSRDocument reads the content tree of a file.
func readSRDocument(path string) (*srDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %v", path, err)
	}
	ds := df.GetDataset()
	root, err := sr.ReadContentTree(ds)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}

	doc := &srDocument{
		SchemaVersion: srSchemaVersion,
		File:          path,
		Warnings:      append([]string{}, df.Warnings...),
		Content:       buildSRNode(root, "1"),
	}
	doc.Document = srHeader{
		SOPClassUID:      dsString(ds, tag.New(0x0008, 0x0016)),
		PatientName:      dsString(ds, tag.New(0x0010, 0x0010)),
		CompletionFlag:   dsString(ds, tag.New(0x0040, 0xA491)),
		VerificationFlag: dsString(ds, tag.New(0x0040, 0xA493)),
		ContentDate:      dsString(ds, tag.New(0x0008, 0x0023)),
		ContentTime:      dsString(ds, tag.New(0x0008, 0x0033)),
		ContentItemCount: root.Count(),
	}
	if doc.Document.SOPClassUID == "" && df.FileMetaInfo != nil {
		doc.Document.SOPClassUID = df.FileMetaInfo.MediaStorageSOPClassUID
	}
	doc.Document.SOPClassName = srClassName(doc.Document.SOPClassUID)
	if template := dsFirst(ds, tag.New(0x0040, 0xA504)); template != nil {
		doc.Document.TemplateID = dsString(template, tag.New(0x0040, 0xDB00))
		doc.Document.MappingResource = dsString(template, tag.New(0x0008, 0x0105))
	}
	doc.Document.VerifyingObservers = readSRVerifiers(ds)
	doc.Document.PredecessorDocuments = readSRPredecessors(ds)
	return doc, nil
}

// dsItems is every item of a sequence, none when it is absent.
func dsItems(ds *dataset.Dataset, t tag.Tag) []*dataset.Dataset {
	if ds == nil {
		return nil
	}
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil
	}
	var items []*dataset.Dataset
	for i := 0; i < seq.Length(); i++ {
		if raw, err := seq.Get(i); err == nil {
			if item, ok := raw.(*dataset.Dataset); ok {
				items = append(items, item)
			}
		}
	}
	return items
}

// readSRVerifiers reads the Verifying Observer Sequence of the document.
func readSRVerifiers(ds *dataset.Dataset) []srVerifier {
	var out []srVerifier
	for _, item := range dsItems(ds, tag.New(0x0040, 0xA073)) {
		out = append(out, srVerifier{
			Name:           dsString(item, tag.New(0x0040, 0xA075)),
			Organization:   dsString(item, tag.New(0x0040, 0xA027)),
			DateTime:       dsString(item, tag.New(0x0040, 0xA030)),
			Identification: convertSRCode(srCodeOf(item, tag.New(0x0040, 0xA088))),
		})
	}
	return out
}

// readSRPredecessors reads the Predecessor Documents Sequence: each item is
// a Hierarchical SOP Instance Reference, a study with the series in it and the
// instances in those (PS3.3 C.17.2, Table C.17-2).
func readSRPredecessors(ds *dataset.Dataset) []srPredecessor {
	var out []srPredecessor
	for _, study := range dsItems(ds, tag.New(0x0040, 0xA360)) {
		p := srPredecessor{StudyInstanceUID: dsString(study, tag.New(0x0020, 0x000D))}
		for _, series := range dsItems(study, tag.New(0x0008, 0x1115)) {
			s := srPredecessorSeries{SeriesInstanceUID: dsString(series, tag.New(0x0020, 0x000E))}
			for _, instance := range dsItems(series, tag.New(0x0008, 0x1199)) {
				s.Instances = append(s.Instances, readSRInstance(instance))
			}
			p.Series = append(p.Series, s)
		}
		out = append(out, p)
	}
	return out
}

// readSRInstance reads a Referenced SOP Class UID and Referenced SOP Instance
// UID from an item.
func readSRInstance(item *dataset.Dataset) srInstance {
	i := srInstance{
		SOPClassUID:    dsString(item, tag.New(0x0008, 0x1150)),
		SOPInstanceUID: dsString(item, tag.New(0x0008, 0x1155)),
	}
	i.SOPClassName = srClassName(i.SOPClassUID)
	return i
}

// buildSRNode converts an item, and those below it.
func buildSRNode(item *sr.ContentItem, position string) srNode {
	n := srNode{
		Position:     position,
		Relationship: item.RelationshipType,
		ValueType:    item.ValueType,
		ConceptName:  convertSRCode(item.ConceptName),
	}
	ds := item.Dataset
	n.ObservationDateTime = dsString(ds, tag.New(0x0040, 0xA032))
	if template := dsFirst(ds, tag.New(0x0040, 0xA504)); template != nil {
		n.Template = &srTemplate{
			ID:       dsString(template, tag.New(0x0040, 0xDB00)),
			Resource: dsString(template, tag.New(0x0008, 0x0105)),
		}
	}

	// An item that points at another has a relationship and nothing else
	// (PS3.3 C.17.3.5): the target is named by its position.
	if len(item.ReferencedContentItem) > 0 {
		parts := make([]string, len(item.ReferencedContentItem))
		for i, ordinal := range item.ReferencedContentItem {
			parts[i] = strconv.Itoa(ordinal)
		}
		n.ReferencedPosition = strings.Join(parts, ".")
	}

	switch item.ValueType {
	case sr.ValueTypeText:
		n.Text = ptr(item.Text)
	case sr.ValueTypeCode:
		n.Code = convertSRCode(item.Code)
	case sr.ValueTypeNum:
		num := &srNumeric{Qualifier: convertSRCode(srCodeOf(ds, tag.New(0x0040, 0xA301)))}
		if item.Measurement != nil {
			num.Value = item.Measurement.Value
			num.Units = convertSRCode(item.Measurement.Units)
		}
		n.Numeric = num
	case sr.ValueTypeDate, sr.ValueTypeTime, sr.ValueTypeDateTime:
		n.DateTime = ptr(item.DateTime)
	case sr.ValueTypeUIDRef:
		n.UID = ptr(item.UID)
	case sr.ValueTypePName:
		n.PersonName = ptr(item.PersonName)
	case sr.ValueTypeComposite, sr.ValueTypeImage, sr.ValueTypeWaveform:
		n.Reference = readSRReference(ds, item.ValueType)
	case sr.ValueTypeSCoord:
		n.Spatial = readSRSCoord(ds, 2)
	case sr.ValueTypeSCoord3D:
		n.Spatial = readSRSCoord(ds, 3)
	case sr.ValueTypeTCoord:
		n.Temporal = readSRTCoord(ds)
	case sr.ValueTypeContainer:
		n.Continuity = item.ContinuityOfContent
	}

	for i, child := range item.Children {
		n.Children = append(n.Children, buildSRNode(child, position+"."+strconv.Itoa(i+1)))
	}
	return n
}

func ptr[T any](v T) *T { return &v }

// convertSRCode is a code as the document gives it, nil for none.
func convertSRCode(c *sr.Code) *srCode {
	if c == nil {
		return nil
	}
	return &srCode{Value: c.Value, Scheme: c.SchemeDesignator, SchemeVersion: c.SchemeVersion, Meaning: c.Meaning}
}

// srCodeOf reads a code from a one-item code sequence of an item.
func srCodeOf(ds *dataset.Dataset, t tag.Tag) *sr.Code {
	item := dsFirst(ds, t)
	if item == nil {
		return nil
	}
	return &sr.Code{
		Value:            dsString(item, tag.New(0x0008, 0x0100)),
		SchemeDesignator: dsString(item, tag.New(0x0008, 0x0102)),
		SchemeVersion:    dsString(item, tag.New(0x0008, 0x0103)),
		Meaning:          dsString(item, tag.New(0x0008, 0x0104)),
	}
}

// readSRReference reads the Referenced SOP Sequence of an IMAGE, WAVEFORM or
// COMPOSITE item.
func readSRReference(ds *dataset.Dataset, valueType string) *srReference {
	ref := dsFirst(ds, tag.New(0x0008, 0x1199))
	if ref == nil {
		return nil
	}
	return readSRReferenceItem(ref, valueType, true)
}

// readSRReferenceItem reads one referenced instance. Its frames, segments and
// presentation state are those of an image, and its channels those of a
// waveform: what an item carries depends on its value type, and an attribute
// of another is not read as this one's.
func readSRReferenceItem(ref *dataset.Dataset, valueType string, nested bool) *srReference {
	out := &srReference{
		SOPClassUID:    dsString(ref, tag.New(0x0008, 0x1150)),
		SOPInstanceUID: dsString(ref, tag.New(0x0008, 0x1155)),
	}
	out.SOPClassName = srClassName(out.SOPClassUID)
	if valueType == sr.ValueTypeImage {
		for _, s := range strings.Split(dsString(ref, tag.New(0x0008, 0x1160)), `\`) {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				out.Frames = append(out.Frames, n)
			}
		}
		for _, v := range dsUint16s(ref, tag.New(0x0062, 0x000B)) {
			out.Segments = append(out.Segments, int(v))
		}
		if nested {
			if ps := dsFirst(ref, tag.New(0x0008, 0x1199)); ps != nil {
				out.PresentationState = readSRReferenceItem(ps, "", false)
			}
			for _, mapping := range dsItems(ref, tag.New(0x0008, 0x114B)) {
				out.ValueMappings = append(out.ValueMappings, readSRInstance(mapping))
			}
		}
	}
	if valueType == sr.ValueTypeWaveform {
		values := dsUint16s(ref, tag.New(0x0040, 0xA0B0))
		for i := 0; i+1 < len(values); i += 2 {
			out.Channels = append(out.Channels, [2]int{int(values[i]), int(values[i+1])})
		}
	}
	return out
}

// readSRSCoord reads the Graphic Type and Graphic Data of an SCOORD or
// SCOORD3D item, whose data are float pairs or triples.
func readSRSCoord(ds *dataset.Dataset, dimensions int) *srSCoord {
	out := &srSCoord{
		GraphicType:               dsString(ds, tag.New(0x0070, 0x0023)),
		FrameOfReferenceUID:       dsString(ds, tag.New(0x3006, 0x0024)),
		PixelOriginInterpretation: dsString(ds, tag.New(0x0048, 0x0301)),
		Points:                    [][]float64{},
	}
	data := dsFloat32s(ds, tag.New(0x0070, 0x0022))
	for i := 0; i+dimensions <= len(data); i += dimensions {
		point := make([]float64, dimensions)
		for d := range point {
			point[d] = float64(data[i+d])
		}
		out.Points = append(out.Points, point)
	}
	return out
}

// readSRTCoord reads the Temporal Range Type and what it covers.
func readSRTCoord(ds *dataset.Dataset) *srTCoord {
	out := &srTCoord{RangeType: dsString(ds, tag.New(0x0040, 0xA130))}
	if raw := dsBytes(ds, tag.New(0x0040, 0xA132)); len(raw)%4 == 0 {
		for i := 0; i+4 <= len(raw); i += 4 {
			out.SamplePositions = append(out.SamplePositions, binary.LittleEndian.Uint32(raw[i:]))
		}
	}
	if s := dsString(ds, tag.New(0x0040, 0xA138)); s != "" {
		out.TimeOffsets = splitMulti(s)
	}
	if s := dsString(ds, tag.New(0x0040, 0xA13A)); s != "" {
		out.DateTimes = splitMulti(s)
	}
	return out
}

func splitMulti(s string) []string {
	parts := strings.Split(s, `\`)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// dsBytes is the raw value of an element, nil when it is absent.
func dsBytes(ds *dataset.Dataset, t tag.Tag) []byte {
	if ds == nil {
		return nil
	}
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	raw, _ := elem.Bytes()
	return raw
}

// dsString is a text value without its padding.
func dsString(ds *dataset.Dataset, t tag.Tag) string {
	return strings.TrimRight(string(dsBytes(ds, t)), " \x00")
}

func dsUint16s(ds *dataset.Dataset, t tag.Tag) []uint16 {
	raw := dsBytes(ds, t)
	out := make([]uint16, 0, len(raw)/2)
	for i := 0; i+2 <= len(raw); i += 2 {
		out = append(out, binary.LittleEndian.Uint16(raw[i:]))
	}
	return out
}

func dsFloat32s(ds *dataset.Dataset, t tag.Tag) []float32 {
	raw := dsBytes(ds, t)
	out := make([]float32, 0, len(raw)/4)
	for i := 0; i+4 <= len(raw); i += 4 {
		out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(raw[i:])))
	}
	return out
}

// dsFirst is the first item of a sequence, nil when there is none.
func dsFirst(ds *dataset.Dataset, t tag.Tag) *dataset.Dataset {
	if ds == nil {
		return nil
	}
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok || seq.Length() == 0 {
		return nil
	}
	item, err := seq.Get(0)
	if err != nil {
		return nil
	}
	first, _ := item.(*dataset.Dataset)
	return first
}

// srClassNames are the names of the SOP classes a report is, and those it
// most often points at. A class not here is printed by its UID alone.
var srClassNames = map[string]string{
	"1.2.840.10008.5.1.4.1.1.88.11": "Basic Text SR",
	"1.2.840.10008.5.1.4.1.1.88.22": "Enhanced SR",
	"1.2.840.10008.5.1.4.1.1.88.33": "Comprehensive SR",
	"1.2.840.10008.5.1.4.1.1.88.34": "Comprehensive 3D SR",
	"1.2.840.10008.5.1.4.1.1.88.35": "Extensible SR",
	"1.2.840.10008.5.1.4.1.1.88.40": "Procedure Log",
	"1.2.840.10008.5.1.4.1.1.88.50": "Mammography CAD SR",
	"1.2.840.10008.5.1.4.1.1.88.59": "Key Object Selection Document",
	"1.2.840.10008.5.1.4.1.1.88.65": "Chest CAD SR",
	"1.2.840.10008.5.1.4.1.1.88.67": "X-Ray Radiation Dose SR",
	"1.2.840.10008.5.1.4.1.1.88.68": "Radiopharmaceutical Radiation Dose SR",
	"1.2.840.10008.5.1.4.1.1.88.69": "Colon CAD SR",
	"1.2.840.10008.5.1.4.1.1.88.70": "Implantation Plan SR",
	"1.2.840.10008.5.1.4.1.1.88.71": "Acquisition Context SR",
	"1.2.840.10008.5.1.4.1.1.88.72": "Simplified Adult Echo SR",
	"1.2.840.10008.5.1.4.1.1.88.73": "Patient Radiation Dose SR",
	"1.2.840.10008.5.1.4.1.1.88.74": "Planned Imaging Agent Administration SR",
	"1.2.840.10008.5.1.4.1.1.88.75": "Performed Imaging Agent Administration SR",
	"1.2.840.10008.5.1.4.1.1.88.76": "Enhanced X-Ray Radiation Dose SR",
	"1.2.840.10008.5.1.4.1.1.1":     "CR Image",
	"1.2.840.10008.5.1.4.1.1.1.1":   "Digital X-Ray Image",
	"1.2.840.10008.5.1.4.1.1.2":     "CT Image",
	"1.2.840.10008.5.1.4.1.1.2.1":   "Enhanced CT Image",
	"1.2.840.10008.5.1.4.1.1.3.1":   "Ultrasound Multi-frame Image",
	"1.2.840.10008.5.1.4.1.1.4":     "MR Image",
	"1.2.840.10008.5.1.4.1.1.4.1":   "Enhanced MR Image",
	"1.2.840.10008.5.1.4.1.1.6.1":   "Ultrasound Image",
	"1.2.840.10008.5.1.4.1.1.7":     "Secondary Capture Image",
	"1.2.840.10008.5.1.4.1.1.11.1":  "Grayscale Softcopy Presentation State",
	"1.2.840.10008.5.1.4.1.1.12.1":  "X-Ray Angiographic Image",
	"1.2.840.10008.5.1.4.1.1.20":    "Nuclear Medicine Image",
	"1.2.840.10008.5.1.4.1.1.67":    "Real World Value Mapping",
	"1.2.840.10008.5.1.4.1.1.128":   "Positron Emission Tomography Image",
	"1.2.840.10008.5.1.4.1.1.481.2": "RT Dose",
	"1.2.840.10008.5.1.4.1.1.481.3": "RT Structure Set",
	"1.2.840.10008.5.1.4.1.1.481.5": "RT Plan",
	"1.2.840.10008.5.1.4.1.1.9.1.1": "12-lead ECG Waveform",
	"1.2.840.10008.5.1.4.1.1.9.1.2": "General ECG Waveform",
	"1.2.840.10008.5.1.4.1.1.9.2.1": "Hemodynamic Waveform",
}

// srClassName is the name of a SOP class, empty when it is not known.
func srClassName(uid string) string {
	return srClassNames[uid]
}

// writeText prints the document as an indented outline.
func (d *srDocument) writeText(out io.Writer) {
	h := d.Document
	title := h.SOPClassName
	if title == "" {
		title = h.SOPClassUID
	}
	fmt.Fprintf(out, "%s\n", title)
	if h.PatientName != "" {
		fmt.Fprintf(out, "Patient: %s\n", h.PatientName)
	}
	var flags []string
	if h.CompletionFlag != "" {
		flags = append(flags, "Completion: "+h.CompletionFlag)
	}
	if h.VerificationFlag != "" {
		flags = append(flags, "Verification: "+h.VerificationFlag)
	}
	if len(flags) > 0 {
		fmt.Fprintf(out, "%s\n", strings.Join(flags, "  "))
	}
	for _, v := range h.VerifyingObservers {
		fmt.Fprintf(out, "Verifying observer: %s\n", v.text())
	}
	for _, p := range h.PredecessorDocuments {
		p.writeText(out)
	}
	if h.ContentDate != "" || h.ContentTime != "" {
		fmt.Fprintf(out, "Content date/time: %s\n", strings.TrimSpace(formatSRDate(h.ContentDate)+" "+formatSRTime(h.ContentTime)))
	}
	if h.TemplateID != "" {
		fmt.Fprintf(out, "Template: TID %s (%s)\n", h.TemplateID, h.MappingResource)
	}
	fmt.Fprintf(out, "%d content item(s)\n\n", h.ContentItemCount)
	writeSRNode(out, &d.Content, 0)
	for _, w := range d.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s\n", d.File, w)
	}
}

// text is a verifying observer as the name, the organization and the time of
// the verification, and the observer's code.
func (v srVerifier) text() string {
	var parts []string
	if v.Name != "" {
		parts = append(parts, v.Name)
	}
	if v.Organization != "" {
		parts = append(parts, v.Organization)
	}
	if v.DateTime != "" {
		parts = append(parts, formatSRDateTime(v.DateTime))
	}
	out := strings.Join(parts, ", ")
	if v.Identification != nil {
		out += " " + srCodeText(v.Identification)
	}
	return strings.TrimSpace(out)
}

// writeText prints a predecessor study: a line for each instance, with the
// series and the study it is in.
func (p srPredecessor) writeText(out io.Writer) {
	if len(p.Series) == 0 {
		fmt.Fprintf(out, "Predecessor documents: study %s\n", p.StudyInstanceUID)
	}
	for _, series := range p.Series {
		if len(series.Instances) == 0 {
			fmt.Fprintf(out, "Predecessor documents: study %s series %s\n", p.StudyInstanceUID, series.SeriesInstanceUID)
		}
		for _, instance := range series.Instances {
			fmt.Fprintf(out, "Predecessor document: %s (series %s, study %s)\n", instance.text(), series.SeriesInstanceUID, p.StudyInstanceUID)
		}
	}
}

// text is an instance as its SOP class, in brackets, and its UID.
func (i srInstance) text() string {
	name := ""
	if i.SOPClassName != "" {
		name = i.SOPClassName + " "
	}
	return fmt.Sprintf("%s[%s] %s", name, i.SOPClassUID, i.SOPInstanceUID)
}

// writeSRNode prints an item and those below it: its relationship, value
// type, concept name and value, indented by depth.
func writeSRNode(out io.Writer, n *srNode, depth int) {
	var b strings.Builder
	b.WriteString(strings.Repeat("  ", depth))
	if n.Relationship != "" {
		b.WriteString(strings.ToLower(n.Relationship))
		b.WriteString(" ")
	}
	if n.ReferencedPosition != "" {
		b.WriteString("-> " + n.ReferencedPosition)
		fmt.Fprintln(out, b.String())
		return
	}
	b.WriteString(n.ValueType)
	if n.ConceptName != nil {
		b.WriteString("  " + srCodeText(n.ConceptName))
	}
	if v := n.valueText(); v != "" {
		b.WriteString(" = " + v)
	}
	if n.ObservationDateTime != "" {
		b.WriteString("  @ " + formatSRDateTime(n.ObservationDateTime))
	}
	// The root's template is in the header; the others are said where they
	// are, as dsrdump does with +Pt.
	if n.Template != nil && n.Position != "1" {
		b.WriteString("  # TID " + n.Template.ID)
		if n.Template.Resource != "" {
			b.WriteString(" (" + n.Template.Resource + ")")
		}
	}
	fmt.Fprintln(out, b.String())
	for i := range n.Children {
		writeSRNode(out, &n.Children[i], depth+1)
	}
}

// srCodeText is a code as a reader quotes it: its meaning, then its value and
// scheme.
func srCodeText(c *srCode) string {
	scheme := c.Scheme
	if c.SchemeVersion != "" {
		scheme += " " + c.SchemeVersion
	}
	meaning := ""
	if c.Meaning != "" {
		meaning = quoteSRText(c.Meaning) + " "
	}
	return fmt.Sprintf("%s[%s, %s]", meaning, c.Value, scheme)
}

// valueText is the value of an item as one piece of text.
func (n *srNode) valueText() string {
	switch {
	case n.Text != nil:
		return quoteSRText(*n.Text)
	case n.Code != nil:
		return srCodeText(n.Code)
	case n.Numeric != nil:
		var parts []string
		if n.Numeric.Value != "" {
			parts = append(parts, n.Numeric.Value)
		}
		if u := n.Numeric.Units; u != nil {
			parts = append(parts, u.Value+" "+srCodeText(u))
		}
		if q := n.Numeric.Qualifier; q != nil {
			parts = append(parts, "qualifier "+srCodeText(q))
		}
		return strings.Join(parts, " ")
	case n.DateTime != nil:
		switch n.ValueType {
		case sr.ValueTypeDate:
			return formatSRDate(*n.DateTime)
		case sr.ValueTypeTime:
			return formatSRTime(*n.DateTime)
		}
		return formatSRDateTime(*n.DateTime)
	case n.UID != nil:
		return *n.UID
	case n.PersonName != nil:
		return quoteSRText(*n.PersonName)
	case n.Reference != nil:
		return n.Reference.text()
	case n.Spatial != nil:
		return n.Spatial.text()
	case n.Temporal != nil:
		return n.Temporal.text()
	}
	return n.Continuity
}

// text is a reference as the instance it points at and where in it.
func (r *srReference) text() string {
	var b strings.Builder
	if r.SOPClassName != "" {
		b.WriteString(r.SOPClassName + " ")
	}
	fmt.Fprintf(&b, "[%s] %s", r.SOPClassUID, r.SOPInstanceUID)
	if len(r.Frames) > 0 {
		b.WriteString(" frames " + joinInts(r.Frames))
	}
	if len(r.Segments) > 0 {
		b.WriteString(" segments " + joinInts(r.Segments))
	}
	if len(r.Channels) > 0 {
		pairs := make([]string, len(r.Channels))
		for i, c := range r.Channels {
			pairs[i] = fmt.Sprintf("%d/%d", c[0], c[1])
		}
		b.WriteString(" channels " + strings.Join(pairs, ","))
	}
	if r.PresentationState != nil {
		b.WriteString(" shown with " + r.PresentationState.text())
	}
	if len(r.ValueMappings) > 0 {
		mappings := make([]string, len(r.ValueMappings))
		for i, m := range r.ValueMappings {
			mappings[i] = m.text()
		}
		b.WriteString(" mapped by " + strings.Join(mappings, ", "))
	}
	return b.String()
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// text is a shape and its points.
func (s *srSCoord) text() string {
	points := make([]string, len(s.Points))
	for i, p := range s.Points {
		coords := make([]string, len(p))
		for j, c := range p {
			coords[j] = strconv.FormatFloat(c, 'g', -1, 32)
		}
		points[i] = strings.Join(coords, "/")
	}
	out := s.GraphicType + " " + strings.Join(points, " ")
	if s.FrameOfReferenceUID != "" {
		out += " in frame of reference " + s.FrameOfReferenceUID
	}
	return strings.TrimSpace(out)
}

// text is a range type and what it covers.
func (t *srTCoord) text() string {
	parts := []string{t.RangeType}
	if len(t.SamplePositions) > 0 {
		positions := make([]string, len(t.SamplePositions))
		for i, p := range t.SamplePositions {
			positions[i] = strconv.FormatUint(uint64(p), 10)
		}
		parts = append(parts, "samples "+strings.Join(positions, ","))
	}
	if len(t.TimeOffsets) > 0 {
		parts = append(parts, "offsets "+strings.Join(t.TimeOffsets, ","))
	}
	if len(t.DateTimes) > 0 {
		parts = append(parts, "at "+strings.Join(t.DateTimes, ","))
	}
	return strings.Join(parts, " ")
}

// quoteSRText quotes a text with its line breaks and tabs written out, so that
// one value stays on one line.
func quoteSRText(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7f || r == utf8.RuneError {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// formatSRDate writes a DA value as YYYY-MM-DD, as written when it is not
// one.
func formatSRDate(s string) string {
	if len(s) == 8 && allDigits(s) {
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	return s
}

// formatSRTime writes a TM value as hh:mm:ss.ffffff, its parts as many as the
// value has, as written when it is not one.
func formatSRTime(s string) string {
	main, fraction, _ := strings.Cut(s, ".")
	if len(main) < 2 || len(main)%2 != 0 || len(main) > 6 || !allDigits(main) || (fraction != "" && !allDigits(fraction)) {
		return s
	}
	parts := []string{main[:2]}
	for i := 2; i < len(main); i += 2 {
		parts = append(parts, main[i:i+2])
	}
	out := strings.Join(parts, ":")
	if fraction != "" {
		out += "." + fraction
	}
	return out
}

// formatSRDateTime writes a DT value as a date and a time, a UTC offset kept
// as written, and the value as written when it is not one.
func formatSRDateTime(s string) string {
	zone := ""
	if i := strings.IndexAny(s, "+-"); i > 0 {
		s, zone = s[:i], s[i:]
	}
	if len(s) >= 8 && allDigits(s[:8]) {
		out := formatSRDate(s[:8])
		if len(s) > 8 {
			t := formatSRTime(s[8:])
			if t == s[8:] {
				return s + zone
			}
			out += " " + t
		}
		return out + zone
	}
	return s + zone
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// showHelp displays help information for the sr command.
func (sc *SRCommand) showHelp() {
	printHelp(`COMMAND: sr - Print the content tree of a structured report

USAGE:
  go-dicom sr <file> [options]

DESCRIPTION:
  Print a Structured Report (PS3.3 C.17) as the document it is: a header, then
  the content items as an indented tree. Each line is the item's relationship
  to its parent (contains, has obs context, inferred from, ...), its value
  type, its concept name as a meaning and a [code, scheme], and its value:

    TEXT       the text, quoted, a line break written as \n
    CODE       the code
    NUM        the number and its unit, the unit's code, and a qualifier
    DATE, TIME, DATETIME    written as 2000-12-06, 12:00:00 and both
    UIDREF, PNAME           the UID, the name
    IMAGE, WAVEFORM, COMPOSITE   the SOP class and instance referenced, an
               image's frames, segments and presentation state, a waveform's
               channels
    SCOORD, SCOORD3D   the graphic type and its points
    TCOORD     the range type and the samples, offsets or times it covers
    CONTAINER  SEPARATE or CONTINUOUS

  An item that only points at another prints -> and the position of its
  target, which is the item's place as dotted ordinals from the root: 1.2.3 is
  the third child of the root's second child. Positions are given in --json.
  An item observed at another time ends with @ and the time. An item built
  from a template other than the root's ends with # TID and its number and
  mapping resource. An image that has Real World Value Mapping instances ends
  with "mapped by" and them.

  The header lists the verifying observers (name, organization, time and
  code) and the predecessor documents, an instance to a line with its series
  and study. The identical documents of a report are not printed.

OPTIONS:
  -h, --help    Show help for this command
  --json        Print the report as JSON, with "schemaVersion" 1: the header,
                and the tree as nested "content" items with their position,
                relationship, value type, concept name and the value in a
                field of its own kind. A field is present only where the
                document has it: "verifyingObservers" and
                "predecessorDocuments" in the header, "template" on an item,
                "realWorldValueMappings" in an image reference. They were
                added without a new schemaVersion, which changes only when a
                field is renamed, removed or changes meaning.

EXAMPLES:
  go-dicom sr report.dcm
  go-dicom sr report.dcm --json
  go-dicom sr -h
`)
}
