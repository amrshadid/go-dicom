package network

import (
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// N-DIMSE command field values (DICOM Part 7, Annex E).
const (
	CommandNEventReportRQ  uint16 = 0x0100
	CommandNEventReportRSP uint16 = 0x8100
	CommandNGetRQ          uint16 = 0x0110
	CommandNGetRSP         uint16 = 0x8110
	CommandNSetRQ          uint16 = 0x0120
	CommandNSetRSP         uint16 = 0x8120
	CommandNActionRQ       uint16 = 0x0130
	CommandNActionRSP      uint16 = 0x8130
	CommandNCreateRQ       uint16 = 0x0140
	CommandNCreateRSP      uint16 = 0x8140
	CommandNDeleteRQ       uint16 = 0x0150
	CommandNDeleteRSP      uint16 = 0x8150
)

// Additional command tag constants for N-DIMSE.
var (
	tagEventTypeID  = tag.New(0x0000, 0x1002)
	tagActionTypeID = tag.New(0x0000, 0x1008)
)

// NEventReportRequest represents an N-EVENT-REPORT-RQ message.
type NEventReportRequest struct {
	MessageID           uint16
	AffectedSOPClass    string
	AffectedSOPInstance string
	EventTypeID         uint16
	DataSet             *dataset.Dataset
}

// NEventReportResponse represents an N-EVENT-REPORT-RSP message.
type NEventReportResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	EventTypeID          uint16
	Status               uint16
	DataSet              *dataset.Dataset
}

// NGetRequest represents an N-GET-RQ message.
type NGetRequest struct {
	MessageID            uint16
	RequestedSOPClass    string
	RequestedSOPInstance string
	AttributeList        []tag.Tag
}

// NGetResponse represents an N-GET-RSP message.
type NGetResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	Status               uint16
	DataSet              *dataset.Dataset
}

// NSetRequest represents an N-SET-RQ message.
type NSetRequest struct {
	MessageID            uint16
	RequestedSOPClass    string
	RequestedSOPInstance string
	DataSet              *dataset.Dataset
}

// NSetResponse represents an N-SET-RSP message.
type NSetResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	Status               uint16
	DataSet              *dataset.Dataset
}

// NActionRequest represents an N-ACTION-RQ message.
type NActionRequest struct {
	MessageID            uint16
	RequestedSOPClass    string
	RequestedSOPInstance string
	ActionTypeID         uint16
	DataSet              *dataset.Dataset
}

// NActionResponse represents an N-ACTION-RSP message.
type NActionResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	ActionTypeID         uint16
	Status               uint16
	DataSet              *dataset.Dataset
}

// NCreateRequest represents an N-CREATE-RQ message.
type NCreateRequest struct {
	MessageID           uint16
	AffectedSOPClass    string
	AffectedSOPInstance string
	DataSet             *dataset.Dataset
}

// NCreateResponse represents an N-CREATE-RSP message.
type NCreateResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	Status               uint16
	DataSet              *dataset.Dataset
}

// NDeleteRequest represents an N-DELETE-RQ message.
type NDeleteRequest struct {
	MessageID            uint16
	RequestedSOPClass    string
	RequestedSOPInstance string
}

// NDeleteResponse represents an N-DELETE-RSP message.
type NDeleteResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	Status               uint16
}

// BuildNEventReportRQ builds an N-EVENT-REPORT-RQ command dataset.
func BuildNEventReportRQ(messageID uint16, sopClassUID, sopInstanceUID string, eventTypeID uint16, hasDataSet bool) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNEventReportRQ)
	addUSElement(ds, tagMessageID, messageID)
	dsType := CommandDataSetTypeNull
	if hasDataSet {
		dsType = CommandDataSetTypePresent
	}
	addUSElement(ds, tagCommandDataSetType, dsType)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagEventTypeID, eventTypeID)
	return ds
}

// BuildNEventReportRSP builds an N-EVENT-REPORT-RSP command dataset.
func BuildNEventReportRSP(messageID uint16, sopClassUID, sopInstanceUID string, eventTypeID, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNEventReportRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagEventTypeID, eventTypeID)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildNGetRQ builds an N-GET-RQ command dataset.
func BuildNGetRQ(messageID uint16, sopClassUID, sopInstanceUID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagRequestedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNGetRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagRequestedSOPInstanceUID, sopInstanceUID)
	return ds
}

// BuildNGetRSP builds an N-GET-RSP command dataset.
func BuildNGetRSP(messageID uint16, sopClassUID, sopInstanceUID string, status uint16, hasDataSet bool) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNGetRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	dsType := CommandDataSetTypeNull
	if hasDataSet {
		dsType = CommandDataSetTypePresent
	}
	addUSElement(ds, tagCommandDataSetType, dsType)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildNSetRQ builds an N-SET-RQ command dataset.
func BuildNSetRQ(messageID uint16, sopClassUID, sopInstanceUID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagRequestedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNSetRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypePresent)
	addUIElement(ds, tagRequestedSOPInstanceUID, sopInstanceUID)
	return ds
}

// BuildNSetRSP builds an N-SET-RSP command dataset.
func BuildNSetRSP(messageID uint16, sopClassUID, sopInstanceUID string, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNSetRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildNActionRQ builds an N-ACTION-RQ command dataset.
func BuildNActionRQ(messageID uint16, sopClassUID, sopInstanceUID string, actionTypeID uint16, hasDataSet bool) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagRequestedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNActionRQ)
	addUSElement(ds, tagMessageID, messageID)
	dsType := CommandDataSetTypeNull
	if hasDataSet {
		dsType = CommandDataSetTypePresent
	}
	addUSElement(ds, tagCommandDataSetType, dsType)
	addUIElement(ds, tagRequestedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagActionTypeID, actionTypeID)
	return ds
}

// BuildNActionRSP builds an N-ACTION-RSP command dataset.
func BuildNActionRSP(messageID uint16, sopClassUID, sopInstanceUID string, actionTypeID, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNActionRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagActionTypeID, actionTypeID)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildNCreateRQ builds an N-CREATE-RQ command dataset.
func BuildNCreateRQ(messageID uint16, sopClassUID, sopInstanceUID string, hasDataSet bool) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNCreateRQ)
	addUSElement(ds, tagMessageID, messageID)
	dsType := CommandDataSetTypeNull
	if hasDataSet {
		dsType = CommandDataSetTypePresent
	}
	addUSElement(ds, tagCommandDataSetType, dsType)
	if sopInstanceUID != "" {
		addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	}
	return ds
}

// BuildNCreateRSP builds an N-CREATE-RSP command dataset.
func BuildNCreateRSP(messageID uint16, sopClassUID, sopInstanceUID string, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNCreateRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildNDeleteRQ builds an N-DELETE-RQ command dataset.
func BuildNDeleteRQ(messageID uint16, sopClassUID, sopInstanceUID string) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagRequestedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNDeleteRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagRequestedSOPInstanceUID, sopInstanceUID)
	return ds
}

// BuildNDeleteRSP builds an N-DELETE-RSP command dataset.
func BuildNDeleteRSP(messageID uint16, sopClassUID, sopInstanceUID string, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandNDeleteRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	addUSElement(ds, tagStatus, status)
	return ds
}
