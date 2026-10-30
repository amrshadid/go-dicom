package sr

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A Structured Report is a tree, and the tree is the document. PS3.3 C.17
// builds it out of content items: each has a value type saying what it carries,
// a coded concept name saying what it means, a relationship to its parent, and
// a Content Sequence holding its children.
//
// Everything a report says lives in that tree. A reader that does not walk it
// has not read the report — it has read the patient and study attributes that
// surround it.
//
// This package's own model — Finding, Observation, ReportTemplate — is a
// different thing, and the two are not interchangeable. It cannot represent an
// arbitrary report and it does not produce a conformant one: it wrote the
// number of findings into Concept Name Code Sequence (0040,A043), whose value
// representation is SQ, and put the first finding's description into Study
// Description. Reading pydicom's test-SR.dcm, which holds 28 content items, it
// found no findings, no conclusions, no observations and no references.
//
// So the tree is here, separately, and the older model is left alone.

// Content item tags, PS3.3 Table C.17-5.
var (
	tagRelationshipType     = tag.New(0x0040, 0xA010)
	tagValueType            = tag.New(0x0040, 0xA040)
	tagConceptNameCodeSeq   = tag.New(0x0040, 0xA043)
	tagContinuityOfContent  = tag.New(0x0040, 0xA050)
	tagTextValue            = tag.New(0x0040, 0xA160)
	tagPersonName           = tag.New(0x0040, 0xA123)
	tagUIDValue             = tag.New(0x0040, 0xA124)
	tagDateValue            = tag.New(0x0040, 0xA121)
	tagTimeValue            = tag.New(0x0040, 0xA122)
	tagDateTimeValue        = tag.New(0x0040, 0xA120)
	tagConceptCodeSeq       = tag.New(0x0040, 0xA168)
	tagMeasuredValueSeq     = tag.New(0x0040, 0xA300)
	tagNumericValue         = tag.New(0x0040, 0xA30A)
	tagMeasurementUnitsSeq  = tag.New(0x0040, 0x08EA)
	tagReferencedSOPSeq     = tag.New(0x0008, 0x1199)
	tagReferencedSOPClass   = tag.New(0x0008, 0x1150)
	tagReferencedSOPInst    = tag.New(0x0008, 0x1155)
	tagContentSequence      = tag.New(0x0040, 0xA730)
	tagReferencedContentID  = tag.New(0x0040, 0xDB73)
	tagCodeValue            = tag.New(0x0008, 0x0100)
	tagCodingSchemeDesignat = tag.New(0x0008, 0x0102)
	tagCodingSchemeVersion  = tag.New(0x0008, 0x0103)
	tagCodeMeaning          = tag.New(0x0008, 0x0104)
)

// The value types of PS3.3 Table C.17.3-7.
const (
	ValueTypeText      = "TEXT"
	ValueTypeNum       = "NUM"
	ValueTypeCode      = "CODE"
	ValueTypeDate      = "DATE"
	ValueTypeTime      = "TIME"
	ValueTypeDateTime  = "DATETIME"
	ValueTypeUIDRef    = "UIDREF"
	ValueTypePName     = "PNAME"
	ValueTypeComposite = "COMPOSITE"
	ValueTypeImage     = "IMAGE"
	ValueTypeWaveform  = "WAVEFORM"
	ValueTypeSCoord    = "SCOORD"
	ValueTypeSCoord3D  = "SCOORD3D"
	ValueTypeTCoord    = "TCOORD"
	ValueTypeContainer = "CONTAINER"
)

// The relationships of PS3.3 Table C.17.3-8.
const (
	RelationshipContains      = "CONTAINS"
	RelationshipHasObsContext = "HAS OBS CONTEXT"
	RelationshipHasAcqContext = "HAS ACQ CONTEXT"
	RelationshipHasConceptMod = "HAS CONCEPT MOD"
	RelationshipHasProperties = "HAS PROPERTIES"
	RelationshipInferredFrom  = "INFERRED FROM"
	RelationshipSelectedFrom  = "SELECTED FROM"
)

// Code is a coded concept: a value in a scheme, and what it means.
type Code struct {
	Value            string // (0008,0100)
	SchemeDesignator string // (0008,0102)
	SchemeVersion    string // (0008,0103)
	Meaning          string // (0008,0104)
}

// String renders the code the way reports usually quote it.
func (c *Code) String() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("(%s, %s, %q)", c.Value, c.SchemeDesignator, c.Meaning)
}

// Measurement is a NUM content item's value: a number and its units.
type Measurement struct {
	Value string // (0040,A30A), kept as written since DS is decimal text
	Units *Code  // (0040,08EA)
}

// Reference identifies another instance, for IMAGE, WAVEFORM and COMPOSITE.
type Reference struct {
	SOPClassUID    string // (0008,1150)
	SOPInstanceUID string // (0008,1155)
}

// ContentItem is one node of the tree.
//
// Which of the value fields carries the content depends on ValueType, and only
// one of them ever does. They are separate fields rather than one interface so
// that reading a report does not require a type switch at every node.
type ContentItem struct {
	// ValueType is what this item carries: TEXT, NUM, CODE, CONTAINER and the
	// rest of PS3.3 Table C.17.3-7.
	ValueType string

	// RelationshipType is how this item relates to its parent. The root has
	// none, since it relates to nothing.
	RelationshipType string

	// ConceptName is what the item means — the question, where the value is the
	// answer.
	ConceptName *Code

	// Text carries TEXT.
	Text string
	// Code carries CODE.
	Code *Code
	// Measurement carries NUM.
	Measurement *Measurement
	// DateTime carries DATE, TIME and DATETIME, as written.
	DateTime string
	// UID carries UIDREF.
	UID string
	// PersonName carries PNAME.
	PersonName string
	// Reference carries IMAGE, WAVEFORM and COMPOSITE.
	Reference *Reference
	// ContinuityOfContent carries CONTAINER: SEPARATE or CONTINUOUS.
	ContinuityOfContent string

	// ReferencedContentItem names another item in the same document by its
	// position rather than repeating its content — PS3.3 C.17.3.1 calls this a
	// by-reference relationship.
	//
	// The identifier is the path from the root as one-based ordinals, so
	// [1 2 3] is the third child of the second child of the root. Such an item
	// has a relationship and nothing else: no value type, no concept name and
	// no value, because all of those belong to the item it points at.
	//
	// pydicom's test-SR.dcm uses two of them. A reader that expects every item
	// to have a value type does not merely miss them, it fails on them.
	ReferencedContentItem []int

	// Children are the items below this one, from its Content Sequence.
	Children []*ContentItem

	// Dataset is the item as stored, for anything the fields above do not
	// model — spatial coordinates, temporal coordinates, and whatever a future
	// edition adds.
	Dataset *dataset.Dataset
}

// Walk calls fn for this item and every item below it, depth first.
//
// Returning false stops the walk below that item but not beside it.
func (c *ContentItem) Walk(fn func(*ContentItem) bool) {
	if c == nil || !fn(c) {
		return
	}
	for _, child := range c.Children {
		child.Walk(fn)
	}
}

// Count returns the number of items in the tree, this one included.
func (c *ContentItem) Count() int {
	n := 0
	c.Walk(func(*ContentItem) bool { n++; return true })
	return n
}

// ReadContentTree builds the content tree of an SR document.
//
// The root is the data set itself: PS3.3 C.17.3 puts the root item's value
// type, concept name and content sequence directly in the document rather than
// wrapping them in an item of their own.
func ReadContentTree(ds *dataset.Dataset) (*ContentItem, error) {
	if ds == nil {
		return nil, fmt.Errorf("sr: no data set to read a content tree from")
	}
	valueType := srString(ds, tagValueType)
	if valueType == "" {
		return nil, fmt.Errorf("sr: the data set has no Value Type (0040,A040), so it is not " +
			"an SR document; its root item would say CONTAINER")
	}
	return readContentItem(ds, 0)
}

// maxContentDepth bounds recursion into a tree, which a crafted file could
// otherwise make unbounded.
const maxContentDepth = 128

// readContentItem reads one item and everything below it.
func readContentItem(ds *dataset.Dataset, depth int) (*ContentItem, error) {
	if depth > maxContentDepth {
		return nil, fmt.Errorf("sr: content items nest deeper than %d levels", maxContentDepth)
	}

	item := readItemFields(ds)
	for _, child := range items(ds, tagContentSequence) {
		read, err := readContentItem(child, depth+1)
		if err != nil {
			return nil, err
		}
		item.Children = append(item.Children, read)
	}
	return item, nil
}

// readItemFields reads one item's own fields, not its children.
func readItemFields(ds *dataset.Dataset) *ContentItem {
	item := &ContentItem{
		ValueType:           srString(ds, tagValueType),
		RelationshipType:    srString(ds, tagRelationshipType),
		ConceptName:         readCode(ds, tagConceptNameCodeSeq),
		Text:                srString(ds, tagTextValue),
		PersonName:          srString(ds, tagPersonName),
		UID:                 srString(ds, tagUIDValue),
		ContinuityOfContent: srString(ds, tagContinuityOfContent),
		Dataset:             ds,
	}

	// DATE, TIME and DATETIME each have their own attribute, and an item uses
	// exactly one of them.
	for _, t := range []tag.Tag{tagDateTimeValue, tagDateValue, tagTimeValue} {
		if v := srString(ds, t); v != "" {
			item.DateTime = v
			break
		}
	}

	item.Code = readCode(ds, tagConceptCodeSeq)
	item.ReferencedContentItem = readOrdinals(ds, tagReferencedContentID)

	// A NUM item holds its value one level down, in Measured Value Sequence,
	// together with the units that make the number mean anything.
	if measured := firstItem(ds, tagMeasuredValueSeq); measured != nil {
		item.Measurement = &Measurement{
			Value: srString(measured, tagNumericValue),
			Units: readCode(measured, tagMeasurementUnitsSeq),
		}
	}

	if ref := firstItem(ds, tagReferencedSOPSeq); ref != nil {
		item.Reference = &Reference{
			SOPClassUID:    srString(ref, tagReferencedSOPClass),
			SOPInstanceUID: srString(ref, tagReferencedSOPInst),
		}
	}
	return item
}

// WriteContentTree writes a content tree into a data set.
//
// The root's own attributes go directly into the data set, as PS3.3 C.17.3
// requires, and its children become the Content Sequence.
//
// An item read from a document keeps what it was read with: an attribute the
// fields do not model, such as GraphicData or ObservationDateTime, is written
// from ContentItem.Dataset as it was, and a modeled value left unchanged is
// written as the element it was read from, so a code keeps its Coding Scheme
// UID and a reference its frame numbers. Only a value that was changed is
// written from the fields. An item given another value type is written from
// its fields alone. Without this a tree read and written back lost what it did
// not model, and dcmtk refused the result (#216).
func WriteContentTree(ds *dataset.Dataset, root *ContentItem) error {
	if ds == nil || root == nil {
		return fmt.Errorf("sr: a data set and a root item are both required")
	}
	if root.RelationshipType != "" {
		return fmt.Errorf("sr: the root item has relationship %q; the root relates to "+
			"nothing and carries no relationship type", root.RelationshipType)
	}
	written, err := writeContentItem(root, 0)
	if err != nil {
		return err
	}
	for _, elem := range written.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			continue
		}
		if err := ds.Add(elem); err != nil {
			return fmt.Errorf("sr: writing %s: %w", t.String(), err)
		}
	}
	return nil
}

// writeContentItem renders one item as a data set.
func writeContentItem(item *ContentItem, depth int) (*dataset.Dataset, error) {
	if depth > maxContentDepth {
		return nil, fmt.Errorf("sr: content items nest deeper than %d levels", maxContentDepth)
	}
	// A by-reference item has a relationship and an identifier and nothing
	// else: the value type, concept name and value all belong to the item it
	// points at.
	if len(item.ReferencedContentItem) > 0 {
		ds := dataset.NewDataset()
		addString(ds, tagRelationshipType, dataelem.CS, item.RelationshipType)
		addOrdinals(ds, tagReferencedContentID, item.ReferencedContentItem)
		return ds, nil
	}
	if item.ValueType == "" {
		return nil, fmt.Errorf("sr: a content item has neither a value type nor a " +
			"referenced content item identifier, so nothing can tell what it says")
	}

	ds := dataset.NewDataset()

	// Relationship first, then value type, which is the order PS3.3 lists them
	// and the order a data set stores them in.
	if item.RelationshipType != "" {
		addString(ds, tagRelationshipType, dataelem.CS, item.RelationshipType)
	}
	addString(ds, tagValueType, dataelem.CS, item.ValueType)
	if err := addCode(ds, tagConceptNameCodeSeq, item.ConceptName); err != nil {
		return nil, err
	}

	switch item.ValueType {
	case ValueTypeText:
		addString(ds, tagTextValue, dataelem.UT, item.Text)
	case ValueTypePName:
		addString(ds, tagPersonName, dataelem.PN, item.PersonName)
	case ValueTypeUIDRef:
		addString(ds, tagUIDValue, dataelem.UI, item.UID)
	case ValueTypeDate:
		addString(ds, tagDateValue, dataelem.DA, item.DateTime)
	case ValueTypeTime:
		addString(ds, tagTimeValue, dataelem.TM, item.DateTime)
	case ValueTypeDateTime:
		addString(ds, tagDateTimeValue, dataelem.DT, item.DateTime)
	case ValueTypeContainer:
		// Continuity of Content is required of a container: it says whether the
		// children read as one flowing text or as separate statements.
		continuity := item.ContinuityOfContent
		if continuity == "" {
			continuity = "SEPARATE"
		}
		addString(ds, tagContinuityOfContent, dataelem.CS, continuity)
	case ValueTypeCode:
		if err := addCode(ds, tagConceptCodeSeq, item.Code); err != nil {
			return nil, err
		}
	case ValueTypeNum:
		if item.Measurement != nil {
			measured := dataset.NewDataset()
			addString(measured, tagNumericValue, dataelem.DS, item.Measurement.Value)
			if err := addCode(measured, tagMeasurementUnitsSeq, item.Measurement.Units); err != nil {
				return nil, err
			}
			if err := addSequence(ds, tagMeasuredValueSeq, measured); err != nil {
				return nil, err
			}
		}
	case ValueTypeImage, ValueTypeWaveform, ValueTypeComposite:
		if item.Reference != nil {
			ref := dataset.NewDataset()
			addString(ref, tagReferencedSOPClass, dataelem.UI, item.Reference.SOPClassUID)
			addString(ref, tagReferencedSOPInst, dataelem.UI, item.Reference.SOPInstanceUID)
			if err := addSequence(ds, tagReferencedSOPSeq, ref); err != nil {
				return nil, err
			}
		}
	}

	if item.Dataset != nil {
		keepWhatWasRead(ds, item, depth == 0)
	}

	if len(item.Children) > 0 {
		seq := sequence.New()
		for _, child := range item.Children {
			written, err := writeContentItem(child, depth+1)
			if err != nil {
				return nil, err
			}
			if err := seq.Append(written); err != nil {
				return nil, fmt.Errorf("sr: appending a content item: %w", err)
			}
		}
		if err := ds.Add(dataelem.NewDataElement(tagContentSequence, dataelem.SQ, seq)); err != nil {
			return nil, fmt.Errorf("sr: writing the content sequence: %w", err)
		}
	}
	return ds, nil
}

// readCode reads a coded concept from a sequence that holds one item.
func readCode(ds *dataset.Dataset, t tag.Tag) *Code {
	item := firstItem(ds, t)
	if item == nil {
		return nil
	}
	return &Code{
		Value:            srString(item, tagCodeValue),
		SchemeDesignator: srString(item, tagCodingSchemeDesignat),
		SchemeVersion:    srString(item, tagCodingSchemeVersion),
		Meaning:          srString(item, tagCodeMeaning),
	}
}

// addCode writes a coded concept as a single-item sequence.
func addCode(ds *dataset.Dataset, t tag.Tag, code *Code) error {
	if code == nil {
		return nil
	}
	item := dataset.NewDataset()
	addString(item, tagCodeValue, dataelem.SH, code.Value)
	addString(item, tagCodingSchemeDesignat, dataelem.SH, code.SchemeDesignator)
	if code.SchemeVersion != "" {
		addString(item, tagCodingSchemeVersion, dataelem.SH, code.SchemeVersion)
	}
	addString(item, tagCodeMeaning, dataelem.LO, code.Meaning)
	return addSequence(ds, t, item)
}

// addSequence adds a sequence holding one item.
func addSequence(ds *dataset.Dataset, t tag.Tag, item *dataset.Dataset) error {
	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		return fmt.Errorf("sr: building the sequence at %s: %w", t.String(), err)
	}
	return ds.Add(dataelem.NewDataElement(t, dataelem.SQ, seq))
}

// addString adds a text value, padded to an even length as PS3.5 7.1.1 requires.
func addString(ds *dataset.Dataset, t tag.Tag, vr dataelem.VR, value string) {
	if value == "" {
		return
	}
	if len(value)%2 == 1 {
		if vr == dataelem.UI {
			value += "\x00"
		} else {
			value += " "
		}
	}
	_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(value)))
}

// items returns the data sets of a sequence, or nil if there is none.
func items(ds *dataset.Dataset, t tag.Tag) []*dataset.Dataset {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil
	}
	out := make([]*dataset.Dataset, 0, seq.Length())
	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			continue
		}
		if inner, ok := item.(*dataset.Dataset); ok {
			out = append(out, inner)
		}
	}
	return out
}

// firstItem returns the first data set of a sequence, which is all that the
// single-item sequences of an SR ever hold.
func firstItem(ds *dataset.Dataset, t tag.Tag) *dataset.Dataset {
	all := items(ds, t)
	if len(all) == 0 {
		return nil
	}
	return all[0]
}

// readOrdinals reads a Referenced Content Item Identifier.
//
// Its value representation is UL, so the ordinals are four-byte binary values
// rather than the backslash-separated text that most multi-valued attributes
// use. Read as text they are unparseable bytes, and the reference comes back
// empty — which looks exactly like an item that has no reference.
func readOrdinals(ds *dataset.Dataset, t tag.Tag) []int {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok || len(raw) < 4 {
		return nil
	}
	out := make([]int, 0, len(raw)/4)
	for i := 0; i+4 <= len(raw); i += 4 {
		out = append(out, int(binary.LittleEndian.Uint32(raw[i:])))
	}
	return out
}

// addOrdinals writes a Referenced Content Item Identifier.
func addOrdinals(ds *dataset.Dataset, t tag.Tag, ordinals []int) {
	if len(ordinals) == 0 {
		return
	}
	raw := make([]byte, 0, len(ordinals)*4)
	for _, n := range ordinals {
		raw = binary.LittleEndian.AppendUint32(raw, uint32(n))
	}
	_ = ds.Add(dataelem.NewDataElement(t, dataelem.UL, raw))
}

// srString reads a text value without its padding.
func srString(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(raw), " \x00")
}

// modeledTags are the attributes ContentItem's fields stand for. The fields
// decide them; anything else in an item's Dataset is carried over as read.
var modeledTags = map[tag.Tag]bool{
	tagRelationshipType: true, tagValueType: true, tagConceptNameCodeSeq: true,
	tagContinuityOfContent: true, tagTextValue: true, tagPersonName: true, tagUIDValue: true,
	tagDateValue: true, tagTimeValue: true, tagDateTimeValue: true, tagConceptCodeSeq: true,
	tagMeasuredValueSeq: true, tagReferencedSOPSeq: true, tagReferencedContentID: true,
	tagContentSequence: true,
}

// rootContentTags are the attributes of the SR Document Content Module that
// belong to the root item but not to ContentItem's fields. The root's Dataset
// is the whole document, so only these are carried over from it; the
// patient, study and series attributes beside them are the caller's.
var rootContentTags = []tag.Tag{
	tag.New(0x0040, 0xA032), // Observation DateTime
	tag.New(0x0040, 0xA171), // Observation UID
	tag.New(0x0040, 0xA504), // Content Template Sequence
}

// keepWhatWasRead brings what item was read with into ds, its rendering.
//
// A modeled value equal to the one read from item.Dataset is replaced by the
// element it was read from, which may hold more than the fields do: a
// Coding Scheme UID in a code, Referenced Frame Number in a reference.
// Attributes the fields do not model are copied as they are, all of them for
// an item and the content module's for the root.
func keepWhatWasRead(ds *dataset.Dataset, item *ContentItem, root bool) {
	read := readItemFields(item.Dataset)
	unchanged := map[tag.Tag]bool{
		tagRelationshipType:    read.RelationshipType == item.RelationshipType,
		tagValueType:           read.ValueType == item.ValueType,
		tagConceptNameCodeSeq:  sameCode(read.ConceptName, item.ConceptName),
		tagContinuityOfContent: read.ContinuityOfContent == item.ContinuityOfContent,
		tagTextValue:           read.Text == item.Text,
		tagPersonName:          read.PersonName == item.PersonName,
		tagUIDValue:            read.UID == item.UID,
		tagDateValue:           read.DateTime == item.DateTime,
		tagTimeValue:           read.DateTime == item.DateTime,
		tagDateTimeValue:       read.DateTime == item.DateTime,
		tagConceptCodeSeq:      sameCode(read.Code, item.Code),
		tagMeasuredValueSeq:    sameMeasurement(read.Measurement, item.Measurement),
		tagReferencedSOPSeq:    sameReference(read.Reference, item.Reference),
		tagReferencedContentID: sameOrdinals(read.ReferencedContentItem, item.ReferencedContentItem),
	}
	sameType := unchanged[tagValueType]
	for t, same := range unchanged {
		if !same {
			continue
		}
		original, ok := item.Dataset.Get(t)
		if !ok {
			continue
		}
		// Written, or left out only because it is empty; an item whose value
		// type changed has no use for the old type's empty value.
		if _, written := ds.Get(t); written || sameType {
			_ = ds.Add(original)
		}
	}
	// Attributes beside the value describe the value read: an item given a
	// new value type is a new item, and GraphicData has no place in a TEXT.
	if !sameType {
		return
	}

	if root {
		for _, t := range rootContentTags {
			if original, ok := item.Dataset.Get(t); ok {
				_ = ds.Add(original)
			}
		}
		return
	}
	for _, original := range item.Dataset.GetAll() {
		t, ok := original.Tag()
		if !ok || modeledTags[t] {
			continue
		}
		_ = ds.Add(original)
	}
}

func sameCode(a, b *Code) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameMeasurement(a, b *Measurement) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Value == b.Value && sameCode(a.Units, b.Units)
}

func sameReference(a, b *Reference) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameOrdinals(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
