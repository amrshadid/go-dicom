package network

import (
	"bytes"
	"encoding/binary"
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
	_ = binary.Write(&buf, binary.BigEndian, uint16(4))
	_ = binary.Write(&buf, binary.BigEndian, a.MaxOperationsInvoked)
	_ = binary.Write(&buf, binary.BigEndian, a.MaxOperationsPerformed)
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
	_ = binary.Write(&buf, binary.BigEndian, itemLen)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(uidBytes)))
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
	uidLen := int(binary.BigEndian.Uint16(data[0:2]))
	// The UID length plus the two role bytes must fit within the sub-item.
	if uidLen+2 > len(data)-2 {
		return nil, NewPDUErrorf("INVALID",
			"role selection UID length %d exceeds available data %d", uidLen, len(data)-2)
	}

	return &SCPSCURoleSelection{
		SOPClassUID: string(data[2 : 2+uidLen]),
		SCURole:     data[2+uidLen] == 0x01,
		SCPRole:     data[3+uidLen] == 0x01,
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
	_ = binary.Write(&itemBuf, binary.BigEndian, uint16(len(u.PrimaryField)))
	itemBuf.Write(u.PrimaryField)
	_ = binary.Write(&itemBuf, binary.BigEndian, uint16(len(u.SecondaryField)))
	if len(u.SecondaryField) > 0 {
		itemBuf.Write(u.SecondaryField)
	}

	_ = binary.Write(&buf, binary.BigEndian, uint16(itemBuf.Len()))
	buf.Write(itemBuf.Bytes())
	return buf.Bytes()
}

// DecodeUserIdentityNegotiation decodes a user identity negotiation sub-item.
func DecodeUserIdentityNegotiation(data []byte) (*UserIdentityNegotiation, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "user identity data too short")
	}

	identType := data[0]
	posResp := data[1]

	primaryLen := int(binary.BigEndian.Uint16(data[2:4]))
	if primaryLen > len(data)-4 {
		return nil, NewPDUErrorf("INVALID",
			"user identity primary field length %d exceeds available data %d", primaryLen, len(data)-4)
	}
	primary := data[4 : 4+primaryLen]

	// The secondary field (password) is optional; peers may omit its length.
	var secondary []byte
	rest := data[4+primaryLen:]
	if len(rest) >= 2 {
		secondaryLen := int(binary.BigEndian.Uint16(rest[0:2]))
		if secondaryLen > len(rest)-2 {
			return nil, NewPDUErrorf("INVALID",
				"user identity secondary field length %d exceeds available data %d",
				secondaryLen, len(rest)-2)
		}
		secondary = rest[2 : 2+secondaryLen]
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
	_ = binary.Write(&buf, binary.BigEndian, itemLen)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(u.ServerResponse)))
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

// DecodeSOPClassExtendedNegotiation decodes a SOP Class Extended Negotiation
// sub-item: a 2-byte UID length, the SOP Class UID, then service-class-specific
// application information filling the remainder.
func DecodeSOPClassExtendedNegotiation(data []byte) (*SOPClassExtendedNegotiation, error) {
	if len(data) < 2 {
		return nil, NewPDUError("SHORT_DATA", "SOP class extended negotiation data too short")
	}

	uidLen := int(binary.BigEndian.Uint16(data[0:2]))
	if uidLen > len(data)-2 {
		return nil, NewPDUErrorf("INVALID",
			"SOP class UID length %d exceeds available data %d", uidLen, len(data)-2)
	}

	return &SOPClassExtendedNegotiation{
		SOPClassUID: string(data[2 : 2+uidLen]),
		ServiceData: data[2+uidLen:],
	}, nil
}

// Encode serializes the SOP Class Extended Negotiation sub-item.
func (s *SOPClassExtendedNegotiation) Encode() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ItemTypeSOPClassExtended)
	buf.WriteByte(0x00)

	uidBytes := []byte(s.SOPClassUID)
	itemLen := uint16(2 + len(uidBytes) + len(s.ServiceData))
	_ = binary.Write(&buf, binary.BigEndian, itemLen)
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(uidBytes)))
	buf.Write(uidBytes)
	buf.Write(s.ServiceData)
	return buf.Bytes()
}

// truthfulAsyncWindow reports the asynchronous operations window to propose.
//
// It used to reduce anything above one to one, because the SCU issued a single
// operation and waited: proposing more told the peer something untrue, and a peer
// may size its own buffers by what it is told.
//
// The window is now enforced rather than clamped. Echo, Store, Find and the
// N-services each wait for their own response by message ID, so the number
// outstanding is bounded by what was negotiated — see SCU.beginOperation. C-MOVE
// and C-GET still take the association exclusively, because both interleave
// traffic that is not their own response on it.
//
// So the caller's window is passed through, with one thing still checked: a
// proposal is a claim about behavior, and zero means unlimited in PS3.7 D.3.3.3.
// Unlimited is not something this implementation can honor — every outstanding
// operation holds a goroutine waiting on a response — so it is reported rather
// than accepted silently.
func truthfulAsyncWindow(proposed *AsynchronousOperationsWindow) *AsynchronousOperationsWindow {
	if proposed == nil {
		return nil
	}

	invoked := proposed.MaxOperationsInvoked
	if invoked == 0 {
		DefaultLogger.Warn("asynchronous operations: an unlimited window of operations " +
			"invoked was requested; proposing 1 instead, because unlimited is not a " +
			"bound this implementation can hold to")
		invoked = 1
	}

	return &AsynchronousOperationsWindow{
		MaxOperationsInvoked:   invoked,
		MaxOperationsPerformed: proposed.MaxOperationsPerformed,
	}
}
