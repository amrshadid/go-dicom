package network

import (
	"bytes"
	"encoding/binary"
	"io"
)

// Extended negotiation sub-item types within User Information.
const (
	ItemTypeAsyncOperationsWindow  byte = 0x53
	ItemTypeSCPSCURoleSelection    byte = 0x54
	ItemTypeSOPClassExtended       byte = 0x56
	ItemTypeSOPClassCommonExtended byte = 0x57
	ItemTypeUserIdentity           byte = 0x58
	ItemTypeUserIdentityAC         byte = 0x59
)

// UserIdentityType defines the type of user identity negotiation.
type UserIdentityType byte

const (
	UserIdentityUsername         UserIdentityType = 1
	UserIdentityUsernamePassword UserIdentityType = 2
	UserIdentityKerberos         UserIdentityType = 3
	UserIdentitySAML             UserIdentityType = 4
	UserIdentityJWT              UserIdentityType = 5
)

// AsynchronousOperationsWindow negotiates the number of asynchronous operations.
type AsynchronousOperationsWindow struct {
	MaxOperationsInvoked   uint16
	MaxOperationsPerformed uint16
}

// Encode serializes the async operations window sub-item.
func (a *AsynchronousOperationsWindow) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeAsyncOperationsWindow)
	buf.WriteByte(0x00)
	binary.Write(&buf, binary.BigEndian, uint16(4))
	binary.Write(&buf, binary.BigEndian, a.MaxOperationsInvoked)
	binary.Write(&buf, binary.BigEndian, a.MaxOperationsPerformed)
	return buf.Bytes()
}

// DecodeAsyncOperationsWindow decodes an async operations window sub-item.
func DecodeAsyncOperationsWindow(data []byte) (*AsynchronousOperationsWindow, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "async operations window data too short")
	}
	return &AsynchronousOperationsWindow{
		MaxOperationsInvoked:   binary.BigEndian.Uint16(data[0:2]),
		MaxOperationsPerformed: binary.BigEndian.Uint16(data[2:4]),
	}, nil
}

// SCPSCURoleSelection negotiates SCP/SCU roles for a SOP Class.
type SCPSCURoleSelection struct {
	SOPClassUID string
	SCURole     bool
	SCPRole     bool
}

// Encode serializes the SCP/SCU role selection sub-item.
func (r *SCPSCURoleSelection) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeSCPSCURoleSelection)
	buf.WriteByte(0x00)

	uidBytes := []byte(r.SOPClassUID)
	itemLen := uint16(2 + len(uidBytes) + 2) // UID length field + UID + SCU role + SCP role
	binary.Write(&buf, binary.BigEndian, itemLen)
	binary.Write(&buf, binary.BigEndian, uint16(len(uidBytes)))
	buf.Write(uidBytes)

	if r.SCURole {
		buf.WriteByte(0x01)
	} else {
		buf.WriteByte(0x00)
	}
	if r.SCPRole {
		buf.WriteByte(0x01)
	} else {
		buf.WriteByte(0x00)
	}

	return buf.Bytes()
}

// DecodeSCPSCURoleSelection decodes a SCP/SCU role selection sub-item.
func DecodeSCPSCURoleSelection(data []byte) (*SCPSCURoleSelection, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "SCP/SCU role selection data too short")
	}
	r := bytes.NewReader(data)

	var uidLen uint16
	binary.Read(r, binary.BigEndian, &uidLen)
	uid := make([]byte, uidLen)
	io.ReadFull(r, uid)

	scuRole, _ := r.ReadByte()
	scpRole, _ := r.ReadByte()

	return &SCPSCURoleSelection{
		SOPClassUID: string(uid),
		SCURole:     scuRole == 0x01,
		SCPRole:     scpRole == 0x01,
	}, nil
}

// UserIdentityNegotiation provides user identity in association requests.
type UserIdentityNegotiation struct {
	Type                      UserIdentityType
	PositiveResponseRequested bool
	PrimaryField              []byte // Username, Kerberos ticket, SAML assertion, or JWT
	SecondaryField            []byte // Password (only for UserIdentityUsernamePassword)
}

// Encode serializes the user identity negotiation sub-item.
func (u *UserIdentityNegotiation) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeUserIdentity)
	buf.WriteByte(0x00)

	var itemBuf bytes.Buffer
	itemBuf.WriteByte(byte(u.Type))
	if u.PositiveResponseRequested {
		itemBuf.WriteByte(0x01)
	} else {
		itemBuf.WriteByte(0x00)
	}
	binary.Write(&itemBuf, binary.BigEndian, uint16(len(u.PrimaryField)))
	itemBuf.Write(u.PrimaryField)
	binary.Write(&itemBuf, binary.BigEndian, uint16(len(u.SecondaryField)))
	if len(u.SecondaryField) > 0 {
		itemBuf.Write(u.SecondaryField)
	}

	binary.Write(&buf, binary.BigEndian, uint16(itemBuf.Len()))
	buf.Write(itemBuf.Bytes())
	return buf.Bytes()
}

// DecodeUserIdentityNegotiation decodes a user identity negotiation sub-item.
func DecodeUserIdentityNegotiation(data []byte) (*UserIdentityNegotiation, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "user identity data too short")
	}

	r := bytes.NewReader(data)
	identType, _ := r.ReadByte()
	posResp, _ := r.ReadByte()

	var primaryLen uint16
	binary.Read(r, binary.BigEndian, &primaryLen)
	primary := make([]byte, primaryLen)
	io.ReadFull(r, primary)

	var secondaryLen uint16
	binary.Read(r, binary.BigEndian, &secondaryLen)
	secondary := make([]byte, secondaryLen)
	if secondaryLen > 0 {
		io.ReadFull(r, secondary)
	}

	return &UserIdentityNegotiation{
		Type:                      UserIdentityType(identType),
		PositiveResponseRequested: posResp == 0x01,
		PrimaryField:              primary,
		SecondaryField:            secondary,
	}, nil
}

// UserIdentityResponse represents the server response to user identity negotiation.
type UserIdentityResponse struct {
	ServerResponse []byte
}

// Encode serializes the user identity response sub-item.
func (u *UserIdentityResponse) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeUserIdentityAC)
	buf.WriteByte(0x00)
	itemLen := uint16(2 + len(u.ServerResponse))
	binary.Write(&buf, binary.BigEndian, itemLen)
	binary.Write(&buf, binary.BigEndian, uint16(len(u.ServerResponse)))
	buf.Write(u.ServerResponse)
	return buf.Bytes()
}

// ExtendedNegotiation holds all extended negotiation items.
type ExtendedNegotiation struct {
	AsyncOperations *AsynchronousOperationsWindow
	RoleSelections  []SCPSCURoleSelection
	UserIdentity    *UserIdentityNegotiation
}

// SOPClassExtendedNegotiation carries service-specific negotiation data.
type SOPClassExtendedNegotiation struct {
	SOPClassUID string
	ServiceData []byte
}

// Encode serializes the SOP Class Extended Negotiation sub-item.
func (s *SOPClassExtendedNegotiation) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeSOPClassExtended)
	buf.WriteByte(0x00)

	uidBytes := []byte(s.SOPClassUID)
	itemLen := uint16(2 + len(uidBytes) + len(s.ServiceData))
	binary.Write(&buf, binary.BigEndian, itemLen)
	binary.Write(&buf, binary.BigEndian, uint16(len(uidBytes)))
	buf.Write(uidBytes)
	buf.Write(s.ServiceData)
	return buf.Bytes()
}
