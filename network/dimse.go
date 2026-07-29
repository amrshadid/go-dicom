package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// DIMSE command field values (DICOM Part 7, Annex E).
const (
	CommandCStoreRQ  uint16 = 0x0001
	CommandCStoreRSP uint16 = 0x8001
	CommandCGetRQ    uint16 = 0x0010
	CommandCGetRSP   uint16 = 0x8010
	CommandCFindRQ   uint16 = 0x0020
	CommandCFindRSP  uint16 = 0x8020
	CommandCMoveRQ   uint16 = 0x0021
	CommandCMoveRSP  uint16 = 0x8021
	CommandCEchoRQ   uint16 = 0x0030
	CommandCEchoRSP  uint16 = 0x8030
	CommandCCancelRQ uint16 = 0x0FFF
)

// DICOM status values.
const (
	StatusSuccess              uint16 = 0x0000
	StatusCancel               uint16 = 0xFE00
	StatusPending              uint16 = 0xFF00
	StatusPendingWarning       uint16 = 0xFF01
	StatusWarning              uint16 = 0x0001 // Coercion of Data Elements warning
	StatusOutOfResources       uint16 = 0xA700
	StatusUnableToProcess      uint16 = 0xC000
	StatusDataSetNotMatch      uint16 = 0xA900
	StatusMoveDestUnknown      uint16 = 0xA801
	StatusClassNotSupported    uint16 = 0x0122
	StatusDuplicateSOPInstance uint16 = 0x0111

	// StatusGetWarningPartial reports that a C-GET or C-MOVE completed but one
	// or more sub-operations failed (PS3.4 Annex C.4.3.1.4).
	StatusGetWarningPartial uint16 = 0xB000
)

// Data set tags referenced when transferring instances.
var (
	tagSOPClassUID    = tag.New(0x0008, 0x0016)
	tagSOPInstanceUID = tag.New(0x0008, 0x0018)
)

// DICOM command dataset type values.
const (
	CommandDataSetTypeNull    uint16 = 0x0101 // No dataset present
	CommandDataSetTypePresent uint16 = 0x0000 // Dataset present (any value != 0x0101 would work per spec, but 0 is unused)
)

// DICOM priority values.
const (
	PriorityMedium uint16 = 0x0000
	PriorityHigh   uint16 = 0x0001
	PriorityLow    uint16 = 0x0002
)

// Command tag constants (Group 0000).
var (
	tagCommandGroupLength        = tag.New(0x0000, 0x0000)
	tagAffectedSOPClassUID       = tag.New(0x0000, 0x0002)
	tagRequestedSOPClassUID      = tag.New(0x0000, 0x0003)
	tagCommandField              = tag.New(0x0000, 0x0100)
	tagMessageID                 = tag.New(0x0000, 0x0110)
	tagMessageIDBeingRespondedTo = tag.New(0x0000, 0x0120)
	tagMoveDestination           = tag.New(0x0000, 0x0600)
	tagPriority                  = tag.New(0x0000, 0x0700)
	tagCommandDataSetType        = tag.New(0x0000, 0x0800)
	tagStatus                    = tag.New(0x0000, 0x0900)
	tagAffectedSOPInstanceUID    = tag.New(0x0000, 0x1000)

	// Requested SOP Instance UID. N-GET, N-SET, N-ACTION and N-DELETE name
	// their target with this, not with Affected SOP Instance UID — the
	// "affected" pair is for messages that create or report on an instance
	// (PS3.7 Table 10.1-4 and neighbors). Sending the wrong one produced a
	// command with no target at all as far as any other implementation was
	tagRequestedSOPInstanceUID        = tag.New(0x0000, 0x1001)
	tagNumberOfRemainingSuboperations = tag.New(0x0000, 0x1020)
	tagNumberOfCompletedSuboperations = tag.New(0x0000, 0x1021)
	tagNumberOfFailedSuboperations    = tag.New(0x0000, 0x1022)
	tagNumberOfWarningSuboperations   = tag.New(0x0000, 0x1023)
)

// CEchoRequest represents a C-ECHO-RQ message.
type CEchoRequest struct {
	MessageID        uint16
	AffectedSOPClass string
}

// CEchoResponse represents a C-ECHO-RSP message.
type CEchoResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	Status               uint16
}

// CStoreRequest represents a C-STORE-RQ message.
type CStoreRequest struct {
	MessageID           uint16
	AffectedSOPClass    string
	AffectedSOPInstance string
	Priority            uint16
	MoveOriginatorAE    string
	MoveOriginatorMsgID uint16
	DataSet             *dataset.Dataset
}

// CStoreResponse represents a C-STORE-RSP message.
type CStoreResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	AffectedSOPInstance  string
	Status               uint16
}

// CFindRequest represents a C-FIND-RQ message.
type CFindRequest struct {
	MessageID        uint16
	AffectedSOPClass string
	Priority         uint16
	DataSet          *dataset.Dataset
}

// CFindResponse represents a C-FIND-RSP message.
type CFindResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	Status               uint16
	DataSet              *dataset.Dataset
}

// CMoveRequest represents a C-MOVE-RQ message.
type CMoveRequest struct {
	MessageID        uint16
	AffectedSOPClass string
	Priority         uint16
	MoveDestination  string
	DataSet          *dataset.Dataset
}

// CMoveResponse represents a C-MOVE-RSP message.
type CMoveResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	Status               uint16
	NumberOfRemaining    uint16
	NumberOfCompleted    uint16
	NumberOfFailed       uint16
	NumberOfWarning      uint16
	DataSet              *dataset.Dataset

	// Instances are the matching instances to send to the move destination as
	// C-STORE sub-operations over a new association. Populating this is what
	// makes a C-MOVE actually move data; leaving it empty returns a status and
	// nothing else.
	//
	// Each instance must carry SOP Class UID (0008,0016) and SOP Instance UID
	// (0008,0018). The destination AE title is resolved to an address through
	// SCPConfig.MoveDestinations or SCPConfig.ResolveMoveDestination.
	Instances []*dataset.Dataset
}

// CGetRequest represents a C-GET-RQ message.
type CGetRequest struct {
	MessageID        uint16
	AffectedSOPClass string
	Priority         uint16
	DataSet          *dataset.Dataset
}

// CGetResponse represents a C-GET-RSP message.
type CGetResponse struct {
	MessageIDRespondedTo uint16
	AffectedSOPClass     string
	Status               uint16
	NumberOfRemaining    uint16
	NumberOfCompleted    uint16
	NumberOfFailed       uint16
	NumberOfWarning      uint16
	DataSet              *dataset.Dataset

	// Instances are the matching instances to transfer to the requestor as
	// C-STORE sub-operations over the same association. Populating this is
	// what makes a C-GET actually move data; leaving it empty returns a
	// status and nothing else.
	//
	// Each instance must carry SOP Class UID (0008,0016) and SOP Instance UID
	// (0008,0018), and its SOP Class must have an accepted presentation
	// context on the association with the SCP role negotiated.
	Instances []*dataset.Dataset
}

// EncodeCommandDataset creates a DICOM command dataset as Implicit VR Little Endian bytes.
func EncodeCommandDataset(ds *dataset.Dataset) ([]byte, error) {
	// Command datasets are always Implicit VR Little Endian
	var buf bytes.Buffer

	// First encode all elements except CommandGroupLength
	var elemBuf bytes.Buffer
	elements := ds.GetAll()
	for _, elem := range elements {
		t := elem.GetTag()
		elemTag, ok := t.(tag.Tag)
		if !ok {
			continue
		}
		if elemTag.Equals(tagCommandGroupLength) {
			continue // Skip, we'll compute this
		}
		if err := encodeCommandElement(&elemBuf, elemTag, elem); err != nil {
			return nil, err
		}
	}

	// Write CommandGroupLength first
	groupLenData := elemBuf.Bytes()
	// Tag (4 bytes) + Length (4 bytes) + Value (4 bytes) for UL
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0000))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0x0000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(groupLenData)))

	// Write remaining elements
	buf.Write(groupLenData)

	return buf.Bytes(), nil
}

// DecodeCommandDataset decodes a DICOM command dataset from Implicit VR Little Endian bytes.
func DecodeCommandDataset(data []byte) (*dataset.Dataset, error) {
	ds := dataset.NewDataset()
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		// Read tag (4 bytes: group + element, little endian)
		var group, element uint16
		if err := binary.Read(r, binary.LittleEndian, &group); err != nil {
			break
		}
		if err := binary.Read(r, binary.LittleEndian, &element); err != nil {
			return nil, NewPDUError("DECODE_CMD", "failed to read element number")
		}
		t := tag.New(group, element)

		// Read length (4 bytes, little endian)
		var length uint32
		if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
			return nil, NewPDUError("DECODE_CMD", "failed to read element length")
		}

		// The length is peer-controlled; verify it against the bytes actually
		// remaining before allocating, so a bogus length cannot force a huge
		// allocation or leave a partially-filled buffer behind.
		if uint64(length) > uint64(r.Len()) {
			return nil, NewPDUErrorf("DECODE_CMD",
				"element %s declares %d bytes but only %d remain", t.String(), length, r.Len())
		}

		// Read value. io.ReadFull rather than r.Read: a short read would
		// otherwise silently yield a zero-padded value.
		value := make([]byte, length)
		if _, err := io.ReadFull(r, value); err != nil {
			return nil, NewPDUError("DECODE_CMD", "failed to read element value")
		}

		// Determine VR from dictionary
		vr := getCommandVR(t)
		elem := dataelem.NewDataElement(t, vr, value)
		_ = ds.Add(elem)
	}

	return ds, nil
}

// getCommandVR returns the VR for a command group tag.
func getCommandVR(t tag.Tag) dataelem.VR {
	info := t.GetInfo()
	if info != nil {
		return dataelem.VR(info.VR)
	}
	return dataelem.UN
}

// BuildCEchoRQ builds a C-ECHO-RQ command dataset.
func BuildCEchoRQ(messageID uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, VerificationSOPClassUID)
	addUSElement(ds, tagCommandField, CommandCEchoRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	return ds
}

// BuildCEchoRSP builds a C-ECHO-RSP command dataset.
func BuildCEchoRSP(messageID uint16, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, VerificationSOPClassUID)
	addUSElement(ds, tagCommandField, CommandCEchoRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildCStoreRQ builds a C-STORE-RQ command dataset.
func BuildCStoreRQ(messageID uint16, sopClassUID, sopInstanceUID string, priority uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCStoreRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagPriority, priority)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypePresent)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	return ds
}

// BuildCStoreRSP builds a C-STORE-RSP command dataset.
func BuildCStoreRSP(messageID uint16, sopClassUID, sopInstanceUID string, status uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCStoreRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUSElement(ds, tagStatus, status)
	addUIElement(ds, tagAffectedSOPInstanceUID, sopInstanceUID)
	return ds
}

// BuildCFindRQ builds a C-FIND-RQ command dataset.
func BuildCFindRQ(messageID uint16, sopClassUID string, priority uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCFindRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagPriority, priority)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypePresent)
	return ds
}

// BuildCFindRSP builds a C-FIND-RSP command dataset.
func BuildCFindRSP(messageID uint16, sopClassUID string, status uint16, hasDataSet bool) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCFindRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	dsType := CommandDataSetTypeNull
	if hasDataSet {
		dsType = CommandDataSetTypePresent
	}
	addUSElement(ds, tagCommandDataSetType, dsType)
	addUSElement(ds, tagStatus, status)
	return ds
}

// BuildCMoveRQ builds a C-MOVE-RQ command dataset.
func BuildCMoveRQ(messageID uint16, sopClassUID, moveDestination string, priority uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCMoveRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagPriority, priority)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypePresent)
	addAEElement(ds, tagMoveDestination, moveDestination)
	return ds
}

// BuildCMoveRSP builds a C-MOVE-RSP command dataset.
func BuildCMoveRSP(messageID uint16, sopClassUID string, status uint16,
	remaining, completed, failed, warning uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCMoveRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUSElement(ds, tagStatus, status)
	addUSElement(ds, tagNumberOfRemainingSuboperations, remaining)
	addUSElement(ds, tagNumberOfCompletedSuboperations, completed)
	addUSElement(ds, tagNumberOfFailedSuboperations, failed)
	addUSElement(ds, tagNumberOfWarningSuboperations, warning)
	return ds
}

// BuildCGetRQ builds a C-GET-RQ command dataset.
func BuildCGetRQ(messageID uint16, sopClassUID string, priority uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCGetRQ)
	addUSElement(ds, tagMessageID, messageID)
	addUSElement(ds, tagPriority, priority)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypePresent)
	return ds
}

// BuildCGetRSP builds a C-GET-RSP command dataset.
func BuildCGetRSP(messageID uint16, sopClassUID string, status uint16,
	remaining, completed, failed, warning uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUIElement(ds, tagAffectedSOPClassUID, sopClassUID)
	addUSElement(ds, tagCommandField, CommandCGetRSP)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageID)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	addUSElement(ds, tagStatus, status)
	addUSElement(ds, tagNumberOfRemainingSuboperations, remaining)
	addUSElement(ds, tagNumberOfCompletedSuboperations, completed)
	addUSElement(ds, tagNumberOfFailedSuboperations, failed)
	addUSElement(ds, tagNumberOfWarningSuboperations, warning)
	return ds
}

// ParseCommandDataset extracts common DIMSE fields from a command dataset.
func ParseCommandDataset(ds *dataset.Dataset) (commandField uint16, messageID uint16, status uint16, err error) {
	commandField, err = getUSValue(ds, tagCommandField)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("missing CommandField: %w", err)
	}

	// MessageID or MessageIDBeingRespondedTo depending on direction
	msgID, err1 := getUSValue(ds, tagMessageID)
	msgIDResp, err2 := getUSValue(ds, tagMessageIDBeingRespondedTo)
	if err1 == nil {
		messageID = msgID
	} else if err2 == nil {
		messageID = msgIDResp
	}

	status, _ = getUSValue(ds, tagStatus)
	return commandField, messageID, status, nil
}

// GetDataSetType extracts the CommandDataSetType from a command dataset.
func GetDataSetType(ds *dataset.Dataset) (uint16, error) {
	return getUSValue(ds, tagCommandDataSetType)
}

// HasDataSet returns whether the command indicates a data set follows.
func HasDataSet(ds *dataset.Dataset) bool {
	dsType, err := GetDataSetType(ds)
	if err != nil {
		return false
	}
	return dsType != CommandDataSetTypeNull
}

// GetAffectedSOPClassUID extracts the Affected SOP Class UID from a command dataset.
func GetAffectedSOPClassUID(ds *dataset.Dataset) (string, error) {
	return getUIValue(ds, tagAffectedSOPClassUID)
}

// GetAffectedSOPInstanceUID extracts the Affected SOP Instance UID from a command dataset.
func GetAffectedSOPInstanceUID(ds *dataset.Dataset) (string, error) {
	return getUIValue(ds, tagAffectedSOPInstanceUID)
}

// GetRequestedSOPInstanceUID reads (0000,1001) from a command data set, falling
// back to Affected SOP Instance UID.
//
// The fallback is for peers that make the mistake this library used to make:
// N-GET, N-SET, N-ACTION and N-DELETE name their target with Requested SOP
// Instance UID, but an implementation that sends Affected instead is otherwise
// well-formed, and refusing it gains nothing.
func GetRequestedSOPInstanceUID(ds *dataset.Dataset) (string, error) {
	if uid, err := getUIValue(ds, tagRequestedSOPInstanceUID); err == nil && uid != "" {
		return uid, nil
	}
	return getUIValue(ds, tagAffectedSOPInstanceUID)
}

// Helper functions for building command datasets

func addUSElement(ds *dataset.Dataset, t tag.Tag, value uint16) {
	data := make([]byte, 2)
	binary.LittleEndian.PutUint16(data, value)
	_ = ds.Add(dataelem.NewDataElement(t, dataelem.US, data))
}

func addUIElement(ds *dataset.Dataset, t tag.Tag, value string) {
	data := []byte(value)
	// UI values must be even length (padded with null)
	if len(data)%2 != 0 {
		data = append(data, 0x00)
	}
	_ = ds.Add(dataelem.NewDataElement(t, dataelem.UI, data))
}

func addAEElement(ds *dataset.Dataset, t tag.Tag, value string) {
	data := []byte(value)
	// AE values are padded with spaces to even length
	if len(data)%2 != 0 {
		data = append(data, ' ')
	}
	_ = ds.Add(dataelem.NewDataElement(t, dataelem.AE, data))
}

func getUSValue(ds *dataset.Dataset, t tag.Tag) (uint16, error) {
	elem, ok := ds.Get(t)
	if !ok {
		return 0, fmt.Errorf("element %s not found", t)
	}
	val := elem.GetValue()
	data, ok := val.([]byte)
	if !ok || len(data) < 2 {
		return 0, fmt.Errorf("invalid US value for %s", t)
	}
	return binary.LittleEndian.Uint16(data[:2]), nil
}

func getUIValue(ds *dataset.Dataset, t tag.Tag) (string, error) {
	elem, ok := ds.Get(t)
	if !ok {
		return "", fmt.Errorf("element %s not found", t)
	}
	val := elem.GetValue()
	data, ok := val.([]byte)
	if !ok {
		return "", fmt.Errorf("invalid UI value for %s", t)
	}
	// Trim padding null bytes
	s := string(data)
	for len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return s, nil
}

// encodeCommandElement encodes a single command element in Implicit VR Little Endian.
func encodeCommandElement(buf *bytes.Buffer, t tag.Tag, elem *dataelem.DataElement) error {
	val := elem.GetValue()
	data, ok := val.([]byte)
	if !ok {
		return fmt.Errorf("element %s value is not []byte", t)
	}

	// Tag: group (2 bytes LE) + element (2 bytes LE)
	_ = binary.Write(buf, binary.LittleEndian, t.Group())
	_ = binary.Write(buf, binary.LittleEndian, t.Element())
	// Length (4 bytes LE)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(data)))
	// Value
	buf.Write(data)
	return nil
}

// BuildCCancelRQ builds a C-CANCEL-RQ command dataset to cancel an in-progress operation.
func BuildCCancelRQ(messageIDBeingRespondedTo uint16) *dataset.Dataset {
	ds := dataset.NewDataset()
	addUSElement(ds, tagCommandField, CommandCCancelRQ)
	addUSElement(ds, tagMessageIDBeingRespondedTo, messageIDBeingRespondedTo)
	addUSElement(ds, tagCommandDataSetType, CommandDataSetTypeNull)
	return ds
}

// IsPending returns true if the status indicates more results to follow.
func IsPending(status uint16) bool {
	return status == StatusPending || status == StatusPendingWarning
}
