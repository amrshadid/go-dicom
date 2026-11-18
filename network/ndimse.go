package network

import (
	"encoding/binary"

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
	tagEventTypeID             = tag.New(0x0000, 0x1002)
	tagAttributeIdentifierList = tag.New(0x0000, 0x1005)
	tagActionTypeID            = tag.New(0x0000, 0x1008)
)

// attributeIdentifierList reads an N-GET-RQ's Attribute Identifier List
// (0000,1005), the attributes the requester asked for (PS3.7 10.1.2.1.4). An
// absent or empty list means every attribute, and is returned as nil.
//
// The value is AT, VM 1-n: each tag a group then an element, two bytes each,
// little endian, since a command set is always Implicit VR Little Endian
// (PS3.5 6.2, PS3.7 6.3.1). A length that is not a multiple of four is not a
// list of tags; reading what fits would hand the handler a list the requester
// did not send, so it is an error.
func attributeIdentifierList(cmd *dataset.Dataset) ([]tag.Tag, error) {
	elem, ok := cmd.Get(tagAttributeIdentifierList)
	if !ok {
		return nil, nil
	}
	data, ok := elem.GetValue().([]byte)
	if !ok {
		return nil, NewPDUErrorf("N-GET", "Attribute Identifier List holds %T, not bytes", elem.GetValue())
	}
	if len(data)%4 != 0 {
		return nil, NewPDUErrorf("N-GET",
			"Attribute Identifier List is %d bytes, not a whole number of tags", len(data))
	}
	var tags []tag.Tag
	for i := 0; i < len(data); i += 4 {
		tags = append(tags, tag.New(binary.LittleEndian.Uint16(data[i:]), binary.LittleEndian.Uint16(data[i+2:])))
	}
	return tags, nil
}

// NEventReportRequest represents an N-EVENT-REPORT-RQ message.
type NEventReportRequest struct {
	MessageID           uint16
	AffectedSOPClass    string
	AffectedSOPInstance string
	EventTypeID         uint16
	DataSet             *dataset.Dataset
}

// NEventReportResponse represents an N-EVENT-REPORT-RSP message. DataSet is
// the event reply: one a handler returns is sent after the response, and the
// SCU fills it when the peer sent one (PS3.7 10.1.1).
type NEventReportResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	EventTypeID          uint16
	Status               uint16
	DataSet              *dataset.Dataset
}

// NGetRequest represents an N-GET-RQ message. AttributeList is the Attribute
// Identifier List (0000,1005) the requester sent, in its order; nil means it
// asked for every attribute (PS3.7 10.1.2.1.4).
type NGetRequest struct {
	MessageID            uint16
	RequestedSOPClass    string
	RequestedSOPInstance string
	AttributeList        []tag.Tag
}

// NGetResponse represents an N-GET-RSP message. DataSet is the attribute list:
// one a handler returns is sent after the response, and the SCU fills it when
// the peer sent one (PS3.7 10.1.2).
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

// NSetResponse represents an N-SET-RSP message. DataSet is the attribute list:
// one a handler returns is sent after the response, and the SCU fills it when
// the peer sent one (PS3.7 10.1.3).
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

// NActionResponse represents an N-ACTION-RSP message. DataSet is the action
// reply: one a handler returns is sent after the response, and the SCU fills
// it when the peer sent one (PS3.7 10.1.4).
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

// NCreateResponse represents an N-CREATE-RSP message. DataSet is the attribute
// list of the created instance: one a handler returns is sent after the
// response, and the SCU fills it when the peer sent one (PS3.7 10.1.5).
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
