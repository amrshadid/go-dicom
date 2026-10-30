package fileset

import (
	"strings"

	"github.com/amrshadid/go-dicom/tag"
)

// The leaf record types of PS3.3 F.5 other than IMAGE. An instance that is not
// an image was indexed as one, and a reader looking for its SR documents or RT
// plans by record type found none (#215).
const (
	RecordRTDose         = "RT DOSE"
	RecordRTStructureSet = "RT STRUCTURE SET"
	RecordRTPlan         = "RT PLAN"
	RecordRTTreatRecord  = "RT TREAT RECORD"
	RecordPresentation   = "PRESENTATION"
	RecordWaveform       = "WAVEFORM"
	RecordSRDocument     = "SR DOCUMENT"
	RecordKeyObjectDoc   = "KEY OBJECT DOC"
	RecordRawData        = "RAW DATA"
	RecordRegistration   = "REGISTRATION"
	RecordFiducial       = "FIDUCIAL"
	RecordEncapDoc       = "ENCAP DOC"
	RecordValueMap       = "VALUE MAP"
	RecordSurface        = "SURFACE"
	RecordTract          = "TRACT"
)

const storageRoot = "1.2.840.10008.5.1.4.1.1."

// leafRecordTypes maps a SOP Class UID, less the storage root, to its record
// type, as pydicom's fileset does. A class not listed is indexed as IMAGE.
var leafRecordTypes = map[string]string{
	"481.2": RecordRTDose,
	"481.3": RecordRTStructureSet,
	"481.5": RecordRTPlan, // RT Plan
	"481.8": RecordRTPlan, // RT Ion Plan
	"481.4": RecordRTTreatRecord,
	"481.6": RecordRTTreatRecord,
	"481.7": RecordRTTreatRecord,
	"481.9": RecordRTTreatRecord,
	"11.1":  RecordPresentation,
	"11.2":  RecordPresentation,
	"11.3":  RecordPresentation,
	"11.4":  RecordPresentation,
	"11.5":  RecordPresentation,
	"131":   RecordPresentation, // Basic Structured Display
	"88.59": RecordKeyObjectDoc,
	"66":    RecordRawData,
	"66.1":  RecordRegistration,
	"66.3":  RecordRegistration,
	"66.2":  RecordFiducial,
	"66.5":  RecordSurface,
	"66.6":  RecordTract,
	"67":    RecordValueMap,
}

// leafRecordType returns the record type an instance of sopClass is indexed
// under. Every waveform class sits under 9, every SR class under 88 and every
// encapsulated document under 104, so those go by prefix.
func leafRecordType(sopClass string) string {
	rest, ok := strings.CutPrefix(sopClass, storageRoot)
	if !ok {
		return RecordImage
	}
	if recordType, ok := leafRecordTypes[rest]; ok {
		return recordType
	}
	switch {
	case strings.HasPrefix(rest, "9."):
		return RecordWaveform
	case strings.HasPrefix(rest, "88."):
		return RecordSRDocument
	case strings.HasPrefix(rest, "104."):
		return RecordEncapDoc
	}
	return RecordImage
}

var (
	instanceNumber          = tag.New(0x0020, 0x0013)
	contentDate             = tag.New(0x0008, 0x0023)
	contentTime             = tag.New(0x0008, 0x0033)
	contentLabel            = tag.New(0x0070, 0x0080)
	contentDescription      = tag.New(0x0070, 0x0081)
	contentCreatorName      = tag.New(0x0070, 0x0084)
	conceptNameCodeSequence = tag.New(0x0040, 0xA043)
)

// contentIdentification is the Content Identification Macro that several
// record types carry.
var contentIdentification = []tag.Tag{
	instanceNumber, contentDate, contentTime, contentLabel, contentDescription, contentCreatorName,
}

// leafRecordKeys are the keys each leaf type copies from its instance, written
// empty when the instance lacks them, as requiredRecordKeys are. They follow
// pydicom's DIRECTORY_RECORDERS.
var leafRecordKeys = map[string][]tag.Tag{
	RecordRTDose: {instanceNumber, tag.New(0x3004, 0x000A)}, // Dose Summation Type
	RecordRTStructureSet: {instanceNumber,
		tag.New(0x3006, 0x0002), tag.New(0x3006, 0x0008), tag.New(0x3006, 0x0009)}, // Structure Set Label, Date, Time
	RecordRTPlan: {instanceNumber,
		tag.New(0x300A, 0x0002), tag.New(0x300A, 0x0006), tag.New(0x300A, 0x0007)}, // RT Plan Label, Date, Time
	RecordRTTreatRecord: {instanceNumber, tag.New(0x3008, 0x0250), tag.New(0x3008, 0x0251)}, // Treatment Date, Time
	RecordPresentation: {tag.New(0x0070, 0x0082), tag.New(0x0070, 0x0083), // Presentation Creation Date, Time
		instanceNumber, contentLabel, contentDescription, contentCreatorName},
	RecordWaveform: {instanceNumber, contentDate, contentTime},
	RecordRawData:  {instanceNumber, contentDate, contentTime},
	RecordSRDocument: {instanceNumber, tag.New(0x0040, 0xA491), tag.New(0x0040, 0xA493), // Completion, Verification Flag
		contentDate, contentTime, conceptNameCodeSequence},
	RecordKeyObjectDoc: {contentDate, contentTime, instanceNumber, conceptNameCodeSequence},
	RecordEncapDoc: {contentDate, contentTime, instanceNumber, tag.New(0x0042, 0x0010), // Document Title
		conceptNameCodeSequence, tag.New(0x0042, 0x0012)}, // MIME Type of Encapsulated Document
	RecordRegistration: contentIdentification,
	RecordFiducial:     contentIdentification,
	RecordValueMap:     contentIdentification,
	RecordSurface:      contentIdentification,
	RecordTract:        contentIdentification,
}

// conditionalRecordKeys are copied only when the instance has them: they are
// Type 1C, and an empty one would claim a condition that does not hold.
var conditionalRecordKeys = map[string][]tag.Tag{
	RecordSRDocument:   {tag.New(0x0040, 0xA030)}, // Verification DateTime
	RecordPresentation: {tag.New(0x0008, 0x1115)}, // Referenced Series Sequence
}

var tagSpecificCharacterSet = tag.New(0x0008, 0x0005)

// conformantFileID reports whether components form a File ID PS3.10 8.5
// allows.
func conformantFileID(components []string) bool {
	if len(components) == 0 || len(components) > 8 {
		return false
	}
	for _, c := range components {
		if c == "" || len(c) > 8 {
			return false
		}
		for _, r := range c {
			if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
				return false
			}
		}
	}
	return true
}
