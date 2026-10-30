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

// methodCode is one entry of CID 7050, De-identification Method.
type methodCode struct{ value, meaning string }

var basicProfileCode = methodCode{"113100", "Basic Application Confidentiality Profile"}

// optionCodes are the CID 7050 codes of the options a profile applies on top
// of the Basic Profile.
var optionCodes = map[Profile]methodCode{
	CleanDescriptorsProfile:       {"113105", "Clean Descriptors Option"},
	CleanGraphicsProfile:          {"113103", "Clean Graphics Option"},
	CleanStructuredContentProfile: {"113104", "Clean Structured Content Option"},
	RetainLongFullDatesProfile:    {"113106", "Retain Longitudinal Temporal Information Full Dates Option"},
	RetainPatientCharsProfile:     {"113108", "Retain Patient Characteristics Option"},
	RetainDeviceIdentProfile:      {"113109", "Retain Device Identity Option"},
	RetainUIDsProfile:             {"113110", "Retain UIDs Option"},
	RetainSafePrivateProfile:      {"113111", "Retain Safe Private Option"},
}

// recordMethod adds Patient Identity Removed and the method used, which PS3.15
// E.1.1 requires of a de-identified data set: without them a downstream system
// cannot tell it was de-identified at all (#213).
func (a *Anonymizer) recordMethod(ds *dataset.Dataset) error {
	codes := []methodCode{basicProfileCode}
	if option, ok := optionCodes[a.profile]; ok {
		codes = append(codes, option)
	}

	meanings := make([]string, len(codes))
	items := sequence.New()
	for i, code := range codes {
		meanings[i] = code.meaning
		item := dataset.NewDataset()
		for _, elem := range []*dataelem.DataElement{
			dataelem.NewDataElement(tag.New(0x0008, 0x0100), dataelem.SH, code.value),
			dataelem.NewDataElement(tag.New(0x0008, 0x0102), dataelem.SH, "DCM"),
			dataelem.NewDataElement(tag.New(0x0008, 0x0104), dataelem.LO, code.meaning),
		} {
			if err := item.Add(elem); err != nil {
				return fmt.Errorf("recording the de-identification method: %w", err)
			}
		}
		if err := items.Append(item); err != nil {
			return fmt.Errorf("recording the de-identification method: %w", err)
		}
	}

	// Replaced rather than added to: the data set may carry these from an
	// earlier pass.
	for _, elem := range []*dataelem.DataElement{
		dataelem.NewDataElement(tag.New(0x0012, 0x0062), dataelem.CS, "YES"),
		dataelem.NewDataElement(tag.New(0x0012, 0x0063), dataelem.LO, strings.Join(meanings, `\`)),
		dataelem.NewDataElement(tag.New(0x0012, 0x0064), dataelem.SQ, items),
	} {
		t, _ := elem.Tag()
		ds.Remove(t)
		if err := ds.Add(elem); err != nil {
			return fmt.Errorf("recording the de-identification method: %w", err)
		}
	}
	return nil
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
