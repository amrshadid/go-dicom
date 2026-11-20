package anonymize

import (
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// MethodCodingScheme is the coding scheme designator of the codes in
// PS3.16 CID 7050: DCM, the DICOM Controlled Terminology.
const MethodCodingScheme = "DCM"

// MethodCodeValue is the code value of an entry of PS3.16 CID 7050,
// De-identification Method: the profile or option that was applied. The
// values and meanings were checked against the code table in pydicom's
// source and against the standard.
type MethodCodeValue string

// The code values of CID 7050. Each is in the DCM scheme; [MethodCodeValue.Meaning]
// gives the meaning the standard attaches to it.
const (
	// MethodBasicProfile is "Basic Application Confidentiality Profile".
	MethodBasicProfile MethodCodeValue = "113100"
	// MethodCleanPixelData is "Clean Pixel Data Option".
	MethodCleanPixelData MethodCodeValue = "113101"
	// MethodCleanRecognizableVisualFeatures is "Clean Recognizable Visual
	// Features Option".
	MethodCleanRecognizableVisualFeatures MethodCodeValue = "113102"
	// MethodCleanGraphics is "Clean Graphics Option".
	MethodCleanGraphics MethodCodeValue = "113103"
	// MethodCleanStructuredContent is "Clean Structured Content Option".
	MethodCleanStructuredContent MethodCodeValue = "113104"
	// MethodCleanDescriptors is "Clean Descriptors Option".
	MethodCleanDescriptors MethodCodeValue = "113105"
	// MethodRetainLongitudinalFullDates is "Retain Longitudinal Temporal
	// Information Full Dates Option".
	MethodRetainLongitudinalFullDates MethodCodeValue = "113106"
	// MethodRetainLongitudinalModifiedDates is "Retain Longitudinal Temporal
	// Information Modified Dates Option".
	MethodRetainLongitudinalModifiedDates MethodCodeValue = "113107"
	// MethodRetainPatientCharacteristics is "Retain Patient Characteristics
	// Option".
	MethodRetainPatientCharacteristics MethodCodeValue = "113108"
	// MethodRetainDeviceIdentity is "Retain Device Identity Option".
	MethodRetainDeviceIdentity MethodCodeValue = "113109"
	// MethodRetainUIDs is "Retain UIDs Option".
	MethodRetainUIDs MethodCodeValue = "113110"
	// MethodRetainSafePrivate is "Retain Safe Private Option".
	MethodRetainSafePrivate MethodCodeValue = "113111"
	// MethodRetainInstitutionIdentity is "Retain Institution Identity Option".
	MethodRetainInstitutionIdentity MethodCodeValue = "113112"
)

// methodMeanings are the code meanings of CID 7050.
var methodMeanings = map[MethodCodeValue]string{
	MethodBasicProfile:                    "Basic Application Confidentiality Profile",
	MethodCleanPixelData:                  "Clean Pixel Data Option",
	MethodCleanRecognizableVisualFeatures: "Clean Recognizable Visual Features Option",
	MethodCleanGraphics:                   "Clean Graphics Option",
	MethodCleanStructuredContent:          "Clean Structured Content Option",
	MethodCleanDescriptors:                "Clean Descriptors Option",
	MethodRetainLongitudinalFullDates:     "Retain Longitudinal Temporal Information Full Dates Option",
	MethodRetainLongitudinalModifiedDates: "Retain Longitudinal Temporal Information Modified Dates Option",
	MethodRetainPatientCharacteristics:    "Retain Patient Characteristics Option",
	MethodRetainDeviceIdentity:            "Retain Device Identity Option",
	MethodRetainUIDs:                      "Retain UIDs Option",
	MethodRetainSafePrivate:               "Retain Safe Private Option",
	MethodRetainInstitutionIdentity:       "Retain Institution Identity Option",
}

// Meaning returns the code meaning CID 7050 gives the value, or an empty
// string for a value the context group does not hold.
func (v MethodCodeValue) Meaning() string { return methodMeanings[v] }

// Code returns the value as a code of the DCM scheme with its meaning, ready
// for [RecordDeidentificationMethod].
func (v MethodCodeValue) Code() MethodCode {
	return MethodCode{Value: string(v), Scheme: MethodCodingScheme, Meaning: v.Meaning()}
}

// MethodCode is one item of De-identification Method Code Sequence
// (0012,0064): a code value, its coding scheme and its meaning. The codes of
// CID 7050 come from [MethodCodeValue.Code]; a code of another scheme, such as
// one an earlier tool recorded, can be given as it is.
type MethodCode struct {
	// Value is Code Value (0008,0100).
	Value string
	// Scheme is Coding Scheme Designator (0008,0102); DCM when empty.
	Scheme string
	// Meaning is Code Meaning (0008,0104).
	Meaning string
}

var basicProfileCode = MethodBasicProfile.Code()

// optionCodes are the CID 7050 codes of the options a profile applies on top
// of the Basic Profile.
var optionCodes = map[Profile]MethodCode{
	CleanDescriptorsProfile:       MethodCleanDescriptors.Code(),
	CleanGraphicsProfile:          MethodCleanGraphics.Code(),
	CleanStructuredContentProfile: MethodCleanStructuredContent.Code(),
	RetainLongFullDatesProfile:    MethodRetainLongitudinalFullDates.Code(),
	RetainPatientCharsProfile:     MethodRetainPatientCharacteristics.Code(),
	RetainDeviceIdentProfile:      MethodRetainDeviceIdentity.Code(),
	RetainUIDsProfile:             MethodRetainUIDs.Code(),
	RetainSafePrivateProfile:      MethodRetainSafePrivate.Code(),
}

// recordMethod adds Patient Identity Removed and the method used, which PS3.15
// E.1.1 requires of a de-identified data set: without them a downstream system
// cannot tell it was de-identified at all (#213).
func (a *Anonymizer) recordMethod(ds *dataset.Dataset) error {
	codes := []MethodCode{basicProfileCode}
	if option, ok := optionCodes[a.profile]; ok {
		codes = append(codes, option)
	}
	// Cleaning the pixels is recorded by CleanPixelData, which may have run
	// first; the profile is added to it, not written over it.
	if hasMethodCode(ds, cleanPixelDataCode) {
		codes = append(codes, cleanPixelDataCode)
	}
	// So is an automatic redaction, which has no code: it is said in words,
	// and the words are put back after the codes' meanings replace them.
	automatic := hasAutomaticNote(ds)
	if err := RecordDeidentificationMethod(ds, codes, MethodRecord{IdentityRemoved: true}); err != nil {
		return err
	}
	if automatic {
		return noteAutomaticRedaction(ds)
	}
	return nil
}

// MethodRecord says what RecordDeidentificationMethod writes beside the
// method and its codes.
type MethodRecord struct {
	// IdentityRemoved sets Patient Identity Removed (0012,0062) to YES, which
	// PS3.15 E.1.1 requires of a data set whose attributes were de-identified.
	// When false the attribute is left as it was.
	IdentityRemoved bool
	// BurnedInAnnotation, when not empty, sets Burned In Annotation
	// (0028,0301) to "YES" or "NO". NO is a claim that the pixels hold no
	// identifying text, which the Clean Pixel Data Option makes. When empty
	// the attribute is left as it was.
	BurnedInAnnotation string
	// LongitudinalTemporalInformation, when not empty, sets Longitudinal
	// Temporal Information Modified (0028,0303) to "UNMODIFIED", "MODIFIED" or
	// "REMOVED", which the Retain Longitudinal Temporal Information options
	// say of the dates they keep. When empty the attribute is left as it was.
	LongitudinalTemporalInformation string
}

var (
	tagIdentityRemoved   = tag.New(0x0012, 0x0062)
	tagLongitudinalModif = tag.New(0x0028, 0x0303)
)

// RecordDeidentificationMethod records how a data set was de-identified, as
// PS3.15 E.1.1 asks: it writes De-identification Method (0012,0063), the
// meanings of the codes joined with a backslash, and De-identification Method
// Code Sequence (0012,0064) with one item per code, and, as record says,
// Patient Identity Removed (0012,0062), Burned In Annotation (0028,0301) and
// Longitudinal Temporal Information Modified (0028,0303). The codes come from
// PS3.16 CID 7050: the [MethodCodeValue] constants and their Code method.
// What the data set carried from an earlier pass in (0012,0063) and
// (0012,0064) is replaced, so a caller that wants to keep it passes those
// codes again.
//
// It is the one implementation: [Anonymizer.Anonymize] and
// [Anonymizer.CleanPixelData] record their method through it. It does not
// de-identify anything; recording a method that was not applied is the
// caller's to avoid. No code, a code without a value, and a record that the
// attributes do not allow are errors, and nothing is written.
func RecordDeidentificationMethod(ds *dataset.Dataset, codes []MethodCode, record MethodRecord) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}
	if len(codes) == 0 {
		return fmt.Errorf("recording the de-identification method: no code given")
	}
	for _, code := range codes {
		if code.Value == "" {
			return fmt.Errorf("recording the de-identification method: a code has no value")
		}
	}
	switch record.BurnedInAnnotation {
	case "", "YES", "NO":
	default:
		return fmt.Errorf("recording the de-identification method: Burned In Annotation %q is not YES or NO", record.BurnedInAnnotation)
	}
	switch record.LongitudinalTemporalInformation {
	case "", "UNMODIFIED", "MODIFIED", "REMOVED":
	default:
		return fmt.Errorf("recording the de-identification method: Longitudinal Temporal Information Modified %q is not UNMODIFIED, MODIFIED or REMOVED",
			record.LongitudinalTemporalInformation)
	}
	if err := writeMethod(ds, codes, record.IdentityRemoved); err != nil {
		return err
	}
	for _, c := range []struct {
		tg    tag.Tag
		value string
	}{
		{tagBurnedInAnnotation, record.BurnedInAnnotation},
		{tagLongitudinalModif, record.LongitudinalTemporalInformation},
	} {
		if c.value == "" {
			continue
		}
		ds.Remove(c.tg)
		if err := ds.Add(dataelem.NewDataElement(c.tg, dataelem.CS, c.value)); err != nil {
			return fmt.Errorf("recording the de-identification method: %w", err)
		}
	}
	return nil
}

// writeMethod writes the method codes, and Patient Identity Removed when the
// attributes were de-identified. They replace what the data set carried from
// an earlier pass.
func writeMethod(ds *dataset.Dataset, codes []MethodCode, identityRemoved bool) error {
	meanings := make([]string, len(codes))
	items := sequence.New()
	for i, code := range codes {
		meanings[i] = code.Meaning
		scheme := code.Scheme
		if scheme == "" {
			scheme = MethodCodingScheme
		}
		item := dataset.NewDataset()
		for _, elem := range []*dataelem.DataElement{
			dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, code.Value),
			dataelem.NewDataElement(tag.New(0x0008, 0x0102), dataelem.SH, scheme),
			dataelem.NewDataElement(tag.New(0x0008, 0x0104), dataelem.LO, code.Meaning),
		} {
			if err := item.Add(elem); err != nil {
				return fmt.Errorf("recording the de-identification method: %w", err)
			}
		}
		if err := items.Append(item); err != nil {
			return fmt.Errorf("recording the de-identification method: %w", err)
		}
	}

	elems := []*dataelem.DataElement{
		dataelem.NewDataElement(tagMethod, dataelem.LO, strings.Join(meanings, `\`)),
		dataelem.NewDataElement(tagMethodCodes, dataelem.SQ, items),
	}
	if identityRemoved {
		elems = append(elems, dataelem.NewDataElement(tagIdentityRemoved, dataelem.CS, "YES"))
	}
	for _, elem := range elems {
		t, _ := elem.Tag()
		ds.Remove(t)
		if err := ds.Add(elem); err != nil {
			return fmt.Errorf("recording the de-identification method: %w", err)
		}
	}
	return nil
}

var (
	tagMethod      = tag.New(0x0012, 0x0063)
	tagMethodCodes = tag.New(0x0012, 0x0064)
)

// hasMethodCode reports whether ds already records code as a method used.
func hasMethodCode(ds *dataset.Dataset, code MethodCode) bool {
	elem, ok := ds.Get(tagMethodCodes)
	if !ok {
		return false
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return false
	}
	for _, item := range seq.Items() {
		if recorded, ok := item.(*dataset.Dataset); ok && textOf(recorded, tag.New(0x0008, 0x0100)) == code.Value {
			return true
		}
	}
	return false
}

// textOf reads a text value without its padding.
func textOf(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, ok := elem.Bytes()
	if !ok {
		return ""
	}
	return strings.TrimRight(string(raw), " \x00")
}

// RemapUID returns the UID that Anonymize put, or would put, in place of
// original, and original itself under RetainUIDsProfile. It is for UIDs
// outside the data set, such as a file meta header written by hand.
func (a *Anonymizer) RemapUID(original string) string {
	original = strings.TrimRight(original, "\x00 ")
	if original == "" || a.profile == RetainUIDsProfile {
		return original
	}
	return a.generateNewUID(original)
}

// AnonymizeFileMeta gives the file meta the UID Anonymize gave the data set.
// GetDataset leaves the meta out, so its Media Storage SOP Instance UID keeps
// the original, and writing back the meta that was read keeps the link to the
// source instance in the file (#213).
func (a *Anonymizer) AnonymizeFileMeta(meta *filereader.FileMetaInfo) {
	if meta == nil {
		return
	}
	meta.MediaStorageSOPInstanceUID = a.RemapUID(meta.MediaStorageSOPInstanceUID)
}
