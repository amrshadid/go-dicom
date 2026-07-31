package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// PDU type constants (DICOM Part 8, Section 9.3).
const (
	PDUTypeAssociateRQ byte = 0x01
	PDUTypeAssociateAC byte = 0x02
	PDUTypeAssociateRJ byte = 0x03
	PDUTypeDataTF      byte = 0x04
	PDUTypeReleaseRQ   byte = 0x05
	PDUTypeReleaseRP   byte = 0x06
	PDUTypeAbort       byte = 0x07
)

// Item type constants for sub-items within PDUs.
const (
	ItemTypeApplicationContext    byte = 0x10
	ItemTypePresentationContextRQ byte = 0x20
	ItemTypePresentationContextAC byte = 0x21
	ItemTypeAbstractSyntax        byte = 0x30
	ItemTypeTransferSyntax        byte = 0x40
	ItemTypeUserInformation       byte = 0x50
	ItemTypeMaxPDULength          byte = 0x51
	ItemTypeImplementationClass   byte = 0x52
	ItemTypeImplementationVersion byte = 0x55
)

// A-ASSOCIATE-RJ result values.
const (
	RJResultRejectedPermanent byte = 1
	RJResultRejectedTransient byte = 2
)

// A-ASSOCIATE-RJ source values.
const (
	RJSourceServiceUser                 byte = 1
	RJSourceServiceProviderACSE         byte = 2
	RJSourceServiceProviderPresentation byte = 3
)

// A-ABORT source values.
const (
	AbortSourceServiceUser     byte = 0
	AbortSourceServiceProvider byte = 2
)

// Presentation context result values.
const (
	PCResultAcceptance                 byte = 0
	PCResultUserRejection              byte = 1
	PCResultNoReason                   byte = 2
	PCResultAbstractSyntaxNotSupported byte = 3
	PCResultTransferSyntaxNotSupported byte = 4
)

// DefaultApplicationContextUID is the DICOM Application Context Name.
const DefaultApplicationContextUID = "1.2.840.10008.3.1.1.1"

// DefaultImplementationClassUID is a placeholder implementation class UID.
const DefaultImplementationClassUID = "1.2.826.0.1.3680043.10.511"

// DefaultImplementationVersionName identifies this implementation to peers in
// the A-ASSOCIATE User Information item. Limited to 16 characters by PS3.7 D.3.3.2.
const DefaultImplementationVersionName = "GO-DICOM-1.4.0"

// PDU is the interface for all Protocol Data Units.
type PDU interface {
	Type() byte
	Encode() ([]byte, error)
}

// AssociateRQ represents an A-ASSOCIATE-RQ PDU.
type AssociateRQ struct {
	ProtocolVersion       uint16
	CalledAE              string
	CallingAE             string
	ApplicationContextUID string
	PresentationContexts  []PresentationContextItem
	UserInformation       UserInformationItem
}

func (p *AssociateRQ) Type() byte { return PDUTypeAssociateRQ }

func (p *AssociateRQ) Encode() ([]byte, error) {
	var buf bytes.Buffer

	// Protocol version
	if err := binary.Write(&buf, binary.BigEndian, p.ProtocolVersion); err != nil {
		return nil, err
	}

	// Reserved (2 bytes)
	buf.Write([]byte{0x00, 0x00})

	// Called AE title (16 bytes, space padded)
	buf.Write(padAETitle(p.CalledAE))

	// Calling AE title (16 bytes, space padded)
	buf.Write(padAETitle(p.CallingAE))

	// Reserved (32 bytes)
	buf.Write(make([]byte, 32))

	// Application Context Item
	if err := writeApplicationContext(&buf, p.ApplicationContextUID); err != nil {
		return nil, err
	}

	// Presentation Context Items
	for _, pc := range p.PresentationContexts {
		if err := pc.encodeRQ(&buf); err != nil {
			return nil, err
		}
	}

	// User Information Item
	if err := p.UserInformation.encode(&buf); err != nil {
		return nil, err
	}

	return wrapPDU(PDUTypeAssociateRQ, buf.Bytes()), nil
}

// AssociateAC represents an A-ASSOCIATE-AC PDU.
type AssociateAC struct {
	ProtocolVersion       uint16
	CalledAE              string
	CallingAE             string
	ApplicationContextUID string
	PresentationContexts  []PresentationContextResultItem
	UserInformation       UserInformationItem
}

func (p *AssociateAC) Type() byte { return PDUTypeAssociateAC }

func (p *AssociateAC) Encode() ([]byte, error) {
	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.BigEndian, p.ProtocolVersion); err != nil {
		return nil, err
	}
	buf.Write([]byte{0x00, 0x00})
	buf.Write(padAETitle(p.CalledAE))
	buf.Write(padAETitle(p.CallingAE))
	buf.Write(make([]byte, 32))

	if err := writeApplicationContext(&buf, p.ApplicationContextUID); err != nil {
		return nil, err
	}

	for _, pc := range p.PresentationContexts {
		if err := pc.encode(&buf); err != nil {
			return nil, err
		}
	}

	if err := p.UserInformation.encode(&buf); err != nil {
		return nil, err
	}

	return wrapPDU(PDUTypeAssociateAC, buf.Bytes()), nil
}

// AssociateRJ represents an A-ASSOCIATE-RJ PDU.
type AssociateRJ struct {
	Result byte
	Source byte
	Reason byte
}

func (p *AssociateRJ) Type() byte { return PDUTypeAssociateRJ }

func (p *AssociateRJ) Encode() ([]byte, error) {
	data := []byte{0x00, p.Result, p.Source, p.Reason}
	return wrapPDU(PDUTypeAssociateRJ, data), nil
}

// PDataTF represents a P-DATA-TF PDU containing one or more PDV items.
type PDataTF struct {
	PDVItems []PDVItem
}

func (p *PDataTF) Type() byte { return PDUTypeDataTF }

func (p *PDataTF) Encode() ([]byte, error) {
	var buf bytes.Buffer
	for _, pdv := range p.PDVItems {
		data, err := pdv.encode()
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}
	return wrapPDU(PDUTypeDataTF, buf.Bytes()), nil
}

// PDVItem represents a Presentation Data Value item within a P-DATA-TF PDU.
type PDVItem struct {
	PresentationContextID byte
	IsCommand             bool
	IsLast                bool
	Data                  []byte
}

func (p *PDVItem) encode() ([]byte, error) {
	var buf bytes.Buffer
	// PDV length = 2 (context ID + message control header) + data length
	pdvLen := uint32(2 + len(p.Data))
	if err := binary.Write(&buf, binary.BigEndian, pdvLen); err != nil {
		return nil, err
	}
	buf.WriteByte(p.PresentationContextID)

	var mch byte
	if p.IsCommand {
		mch |= 0x01
	}
	if p.IsLast {
		mch |= 0x02
	}
	buf.WriteByte(mch)
	buf.Write(p.Data)
	return buf.Bytes(), nil
}

// ReleaseRQ represents an A-RELEASE-RQ PDU.
type ReleaseRQ struct{}

func (p *ReleaseRQ) Type() byte { return PDUTypeReleaseRQ }

func (p *ReleaseRQ) Encode() ([]byte, error) {
	return wrapPDU(PDUTypeReleaseRQ, []byte{0x00, 0x00, 0x00, 0x00}), nil
}

// ReleaseRP represents an A-RELEASE-RP PDU.
type ReleaseRP struct{}

func (p *ReleaseRP) Type() byte { return PDUTypeReleaseRP }

func (p *ReleaseRP) Encode() ([]byte, error) {
	return wrapPDU(PDUTypeReleaseRP, []byte{0x00, 0x00, 0x00, 0x00}), nil
}

// AbortPDU represents an A-ABORT PDU.
type AbortPDU struct {
	Source byte
	Reason byte
}

func (p *AbortPDU) Type() byte { return PDUTypeAbort }

func (p *AbortPDU) Encode() ([]byte, error) {
	data := []byte{0x00, 0x00, p.Source, p.Reason}
	return wrapPDU(PDUTypeAbort, data), nil
}

// PresentationContextItem represents a presentation context in an A-ASSOCIATE-RQ.
type PresentationContextItem struct {
	ID               byte
	AbstractSyntax   string
	TransferSyntaxes []string
}

func (p *PresentationContextItem) encodeRQ(w *bytes.Buffer) error {
	var itemBuf bytes.Buffer

	// Presentation context ID
	itemBuf.WriteByte(p.ID)
	// Reserved (3 bytes)
	itemBuf.Write([]byte{0x00, 0x00, 0x00})

	// Abstract Syntax sub-item
	if err := writeSubItem(&itemBuf, ItemTypeAbstractSyntax, []byte(p.AbstractSyntax)); err != nil {
		return err
	}

	// Transfer Syntax sub-items
	for _, ts := range p.TransferSyntaxes {
		if err := writeSubItem(&itemBuf, ItemTypeTransferSyntax, []byte(ts)); err != nil {
			return err
		}
	}

	// Write the presentation context item header
	w.WriteByte(ItemTypePresentationContextRQ)
	w.WriteByte(0x00) // Reserved
	if err := binary.Write(w, binary.BigEndian, uint16(itemBuf.Len())); err != nil {
		return err
	}
	w.Write(itemBuf.Bytes())
	return nil
}

// PresentationContextResultItem represents a presentation context result in an A-ASSOCIATE-AC.
type PresentationContextResultItem struct {
	ID             byte
	Result         byte
	TransferSyntax string
}

func (p *PresentationContextResultItem) encode(w *bytes.Buffer) error {
	var itemBuf bytes.Buffer

	itemBuf.WriteByte(p.ID)
	itemBuf.WriteByte(0x00) // Reserved
	itemBuf.WriteByte(p.Result)
	itemBuf.WriteByte(0x00) // Reserved

	// Transfer Syntax sub-item
	if err := writeSubItem(&itemBuf, ItemTypeTransferSyntax, []byte(p.TransferSyntax)); err != nil {
		return err
	}

	w.WriteByte(ItemTypePresentationContextAC)
	w.WriteByte(0x00)
	if err := binary.Write(w, binary.BigEndian, uint16(itemBuf.Len())); err != nil {
		return err
	}
	w.Write(itemBuf.Bytes())
	return nil
}

// UserInformationItem holds user information sub-items.
type UserInformationItem struct {
	MaxPDULength           uint32
	ImplementationClassUID string
	ImplementationVersion  string

	// AsyncOperations carries the Asynchronous Operations Window sub-item
	// (PS3.7 D.3.3.3) when the peer negotiates one. Nil when absent.
	AsyncOperations *AsynchronousOperationsWindow

	// RoleSelections carries SCP/SCU Role Selection sub-items (PS3.7 D.3.3.4),
	// which let an SCU also act as an SCP for a SOP Class — required by C-GET.
	RoleSelections []SCPSCURoleSelection

	// UserIdentity carries the User Identity Negotiation sub-item
	// (PS3.7 D.3.3.7) used for username/password, Kerberos, SAML, or JWT auth.
	UserIdentity *UserIdentityNegotiation

	// UserIdentityResponse carries the server's identity response in an
	// A-ASSOCIATE-AC. Nil when absent.
	UserIdentityResponse *UserIdentityResponse

	// SOPClassExtended carries SOP Class Extended Negotiation sub-items
	// (PS3.7 D.3.3.5) holding service-class-specific data.
	SOPClassExtended []SOPClassExtendedNegotiation
}

func (u *UserInformationItem) encode(w *bytes.Buffer) error {
	var itemBuf bytes.Buffer

	// Max PDU Length sub-item
	itemBuf.WriteByte(ItemTypeMaxPDULength)
	itemBuf.WriteByte(0x00)
	if err := binary.Write(&itemBuf, binary.BigEndian, uint16(4)); err != nil {
		return err
	}
	if err := binary.Write(&itemBuf, binary.BigEndian, u.MaxPDULength); err != nil {
		return err
	}

	// Implementation Class UID sub-item
	if u.ImplementationClassUID != "" {
		if err := writeSubItem(&itemBuf, ItemTypeImplementationClass, []byte(u.ImplementationClassUID)); err != nil {
			return err
		}
	}

	// Implementation Version sub-item
	if u.ImplementationVersion != "" {
		if err := writeSubItem(&itemBuf, ItemTypeImplementationVersion, []byte(u.ImplementationVersion)); err != nil {
			return err
		}
	}

	// Extended negotiation sub-items
	if u.AsyncOperations != nil {
		itemBuf.Write(u.AsyncOperations.Encode())
	}
	for i := range u.RoleSelections {
		itemBuf.Write(u.RoleSelections[i].Encode())
	}
	if u.UserIdentity != nil {
		itemBuf.Write(u.UserIdentity.Encode())
	}
	if u.UserIdentityResponse != nil {
		itemBuf.Write(u.UserIdentityResponse.Encode())
	}
	for i := range u.SOPClassExtended {
		itemBuf.Write(u.SOPClassExtended[i].Encode())
	}

	w.WriteByte(ItemTypeUserInformation)
	w.WriteByte(0x00)
	if err := binary.Write(w, binary.BigEndian, uint16(itemBuf.Len())); err != nil {
		return err
	}
	w.Write(itemBuf.Bytes())
	return nil
}

// MaxPDULengthLimit is the hard ceiling on the declared length of a single
// received PDU. The PDU length field is a peer-controlled 32-bit value, so
// without a limit a remote peer could declare ~4 GiB and force an allocation
// of that size before a single byte of payload is read. 128 MiB is far above
// any legitimate DICOM PDU (negotiated maximums are typically 16-128 KB) while
// keeping a malicious declaration cheap to reject.
const MaxPDULengthLimit uint32 = 128 << 20

// DecodePDU reads and decodes a PDU from a reader.
func DecodePDU(r io.Reader) (PDU, error) {
	// Read PDU header: type (1 byte) + reserved (1 byte) + length (4 bytes)
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, NewCommunicationError("READ_PDU", "failed to read PDU header", err)
	}

	pduType := header[0]
	pduLength := binary.BigEndian.Uint32(header[2:6])

	if pduLength > MaxPDULengthLimit {
		return nil, NewPDUErrorf("TOO_LARGE",
			"PDU length %d exceeds maximum allowed %d", pduLength, MaxPDULengthLimit)
	}

	// Read PDU data. io.ReadFull fails if the peer declared more than it sends,
	// so the allocation above is bounded and the read is never short.
	data := make([]byte, pduLength)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, NewCommunicationError("READ_PDU", "failed to read PDU data", err)
	}

	switch pduType {
	case PDUTypeAssociateRQ:
		return decodeAssociateRQ(data)
	case PDUTypeAssociateAC:
		return decodeAssociateAC(data)
	case PDUTypeAssociateRJ:
		return decodeAssociateRJ(data)
	case PDUTypeDataTF:
		return decodeDataTF(data)
	case PDUTypeReleaseRQ:
		return &ReleaseRQ{}, nil
	case PDUTypeReleaseRP:
		return &ReleaseRP{}, nil
	case PDUTypeAbort:
		return decodeAbort(data)
	default:
		return nil, NewPDUErrorf("UNKNOWN_TYPE", "unknown PDU type: 0x%02X", pduType)
	}
}

func decodeAssociateRQ(data []byte) (*AssociateRQ, error) {
	if len(data) < 68 {
		return nil, NewPDUError("SHORT_DATA", "A-ASSOCIATE-RQ data too short")
	}

	pdu := &AssociateRQ{
		ProtocolVersion: binary.BigEndian.Uint16(data[0:2]),
		CalledAE:        trimAETitle(data[4:20]),
		CallingAE:       trimAETitle(data[20:36]),
	}

	// Parse variable items starting at offset 68
	r := bytes.NewReader(data[68:])
	for r.Len() > 0 {
		itemType, err := r.ReadByte()
		if err != nil {
			break
		}
		// Reserved byte
		if _, err := r.ReadByte(); err != nil {
			return nil, NewPDUError("PARSE", "failed to read reserved byte")
		}
		var itemLen uint16
		if err := binary.Read(r, binary.BigEndian, &itemLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read item length")
		}
		itemData := make([]byte, itemLen)
		if _, err := io.ReadFull(r, itemData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read item data")
		}

		switch itemType {
		case ItemTypeApplicationContext:
			pdu.ApplicationContextUID = string(itemData)
		case ItemTypePresentationContextRQ:
			pc, err := decodePresentationContextRQ(itemData)
			if err != nil {
				return nil, err
			}
			pdu.PresentationContexts = append(pdu.PresentationContexts, *pc)
		case ItemTypeUserInformation:
			ui, err := decodeUserInformation(itemData)
			if err != nil {
				return nil, err
			}
			pdu.UserInformation = *ui
		}
	}

	return pdu, nil
}

func decodeAssociateAC(data []byte) (*AssociateAC, error) {
	if len(data) < 68 {
		return nil, NewPDUError("SHORT_DATA", "A-ASSOCIATE-AC data too short")
	}

	pdu := &AssociateAC{
		ProtocolVersion: binary.BigEndian.Uint16(data[0:2]),
		CalledAE:        trimAETitle(data[4:20]),
		CallingAE:       trimAETitle(data[20:36]),
	}

	r := bytes.NewReader(data[68:])
	for r.Len() > 0 {
		itemType, err := r.ReadByte()
		if err != nil {
			break
		}
		if _, err := r.ReadByte(); err != nil {
			return nil, NewPDUError("PARSE", "failed to read reserved byte")
		}
		var itemLen uint16
		if err := binary.Read(r, binary.BigEndian, &itemLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read item length")
		}
		itemData := make([]byte, itemLen)
		if _, err := io.ReadFull(r, itemData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read item data")
		}

		switch itemType {
		case ItemTypeApplicationContext:
			pdu.ApplicationContextUID = string(itemData)
		case ItemTypePresentationContextAC:
			pc, err := decodePresentationContextAC(itemData)
			if err != nil {
				return nil, err
			}
			pdu.PresentationContexts = append(pdu.PresentationContexts, *pc)
		case ItemTypeUserInformation:
			ui, err := decodeUserInformation(itemData)
			if err != nil {
				return nil, err
			}
			pdu.UserInformation = *ui
		}
	}

	return pdu, nil
}

func decodeAssociateRJ(data []byte) (*AssociateRJ, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "A-ASSOCIATE-RJ data too short")
	}
	return &AssociateRJ{
		Result: data[1],
		Source: data[2],
		Reason: data[3],
	}, nil
}

func decodeDataTF(data []byte) (*PDataTF, error) {
	pdu := &PDataTF{}
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		var pdvLen uint32
		if err := binary.Read(r, binary.BigEndian, &pdvLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read PDV length")
		}
		if pdvLen < 2 {
			return nil, NewPDUError("INVALID", "PDV length too short")
		}
		// The PDV length is peer-controlled and independent of the enclosing PDU
		// length, so a small PDU can still declare a huge PDV. Reject anything
		// that cannot possibly be satisfied by the remaining buffer before
		// allocating for it.
		if uint64(pdvLen-2) > uint64(r.Len()) {
			return nil, NewPDUErrorf("INVALID",
				"PDV length %d exceeds remaining PDU data %d", pdvLen, r.Len())
		}

		ctxID, err := r.ReadByte()
		if err != nil {
			return nil, NewPDUError("PARSE", "failed to read context ID")
		}
		mch, err := r.ReadByte()
		if err != nil {
			return nil, NewPDUError("PARSE", "failed to read message control header")
		}

		pdvData := make([]byte, pdvLen-2)
		if _, err := io.ReadFull(r, pdvData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read PDV data")
		}

		pdu.PDVItems = append(pdu.PDVItems, PDVItem{
			PresentationContextID: ctxID,
			IsCommand:             mch&0x01 != 0,
			IsLast:                mch&0x02 != 0,
			Data:                  pdvData,
		})
	}

	return pdu, nil
}

func decodeAbort(data []byte) (*AbortPDU, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "A-ABORT data too short")
	}
	return &AbortPDU{
		Source: data[2],
		Reason: data[3],
	}, nil
}

func decodePresentationContextRQ(data []byte) (*PresentationContextItem, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "presentation context data too short")
	}

	pc := &PresentationContextItem{
		ID: data[0],
	}

	r := bytes.NewReader(data[4:])
	for r.Len() > 0 {
		itemType, err := r.ReadByte()
		if err != nil {
			break
		}
		if _, err := r.ReadByte(); err != nil {
			return nil, NewPDUError("PARSE", "failed to read reserved byte")
		}
		var itemLen uint16
		if err := binary.Read(r, binary.BigEndian, &itemLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item length")
		}
		itemData := make([]byte, itemLen)
		if _, err := io.ReadFull(r, itemData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item data")
		}

		switch itemType {
		case ItemTypeAbstractSyntax:
			pc.AbstractSyntax = string(itemData)
		case ItemTypeTransferSyntax:
			pc.TransferSyntaxes = append(pc.TransferSyntaxes, string(itemData))
		}
	}

	return pc, nil
}

func decodePresentationContextAC(data []byte) (*PresentationContextResultItem, error) {
	if len(data) < 4 {
		return nil, NewPDUError("SHORT_DATA", "presentation context AC data too short")
	}

	pc := &PresentationContextResultItem{
		ID:     data[0],
		Result: data[2],
	}

	r := bytes.NewReader(data[4:])
	for r.Len() > 0 {
		itemType, err := r.ReadByte()
		if err != nil {
			break
		}
		if _, err := r.ReadByte(); err != nil {
			return nil, NewPDUError("PARSE", "failed to read reserved byte")
		}
		var itemLen uint16
		if err := binary.Read(r, binary.BigEndian, &itemLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item length")
		}
		itemData := make([]byte, itemLen)
		if _, err := io.ReadFull(r, itemData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item data")
		}

		if itemType == ItemTypeTransferSyntax {
			pc.TransferSyntax = string(itemData)
		}
	}

	return pc, nil
}

func decodeUserInformation(data []byte) (*UserInformationItem, error) {
	ui := &UserInformationItem{}
	r := bytes.NewReader(data)

	for r.Len() > 0 {
		itemType, err := r.ReadByte()
		if err != nil {
			break
		}
		if _, err := r.ReadByte(); err != nil {
			return nil, NewPDUError("PARSE", "failed to read reserved byte")
		}
		var itemLen uint16
		if err := binary.Read(r, binary.BigEndian, &itemLen); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item length")
		}
		itemData := make([]byte, itemLen)
		if _, err := io.ReadFull(r, itemData); err != nil {
			return nil, NewPDUError("PARSE", "failed to read sub-item data")
		}

		switch itemType {
		case ItemTypeMaxPDULength:
			if len(itemData) >= 4 {
				ui.MaxPDULength = binary.BigEndian.Uint32(itemData[0:4])
			}
		case ItemTypeImplementationClass:
			ui.ImplementationClassUID = string(itemData)
		case ItemTypeImplementationVersion:
			ui.ImplementationVersion = string(itemData)
		case ItemTypeAsyncOperationsWindow:
			// A malformed sub-item must not fail the whole association;
			// skip what cannot be parsed and keep the rest.
			if aow, err := DecodeAsyncOperationsWindow(itemData); err == nil {
				ui.AsyncOperations = aow
			}
		case ItemTypeSCPSCURoleSelection:
			if rs, err := DecodeSCPSCURoleSelection(itemData); err == nil {
				ui.RoleSelections = append(ui.RoleSelections, *rs)
			}
		case ItemTypeUserIdentity:
			if id, err := DecodeUserIdentityNegotiation(itemData); err == nil {
				ui.UserIdentity = id
			}
		case ItemTypeUserIdentityAC:
			if len(itemData) >= 2 {
				respLen := int(binary.BigEndian.Uint16(itemData[0:2]))
				if respLen <= len(itemData)-2 {
					ui.UserIdentityResponse = &UserIdentityResponse{
						ServerResponse: itemData[2 : 2+respLen],
					}
				}
			}
		case ItemTypeSOPClassExtended:
			if ext, err := DecodeSOPClassExtendedNegotiation(itemData); err == nil {
				ui.SOPClassExtended = append(ui.SOPClassExtended, *ext)
			}
		}
	}

	return ui, nil
}

// Helper functions

func wrapPDU(pduType byte, data []byte) []byte {
	result := make([]byte, 6+len(data))
	result[0] = pduType
	result[1] = 0x00 // Reserved
	binary.BigEndian.PutUint32(result[2:6], uint32(len(data)))
	copy(result[6:], data)
	return result
}

func padAETitle(ae string) []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = ' '
	}
	copy(b, []byte(ae))
	return b
}

func trimAETitle(data []byte) string {
	end := len(data)
	for end > 0 && data[end-1] == ' ' {
		end--
	}
	return string(data[:end])
}

func writeSubItem(w *bytes.Buffer, itemType byte, data []byte) error {
	w.WriteByte(itemType)
	w.WriteByte(0x00)
	if err := binary.Write(w, binary.BigEndian, uint16(len(data))); err != nil {
		return err
	}
	w.Write(data)
	return nil
}

func writeApplicationContext(w *bytes.Buffer, uid string) error {
	return writeSubItem(w, ItemTypeApplicationContext, []byte(uid))
}

// EncodePDU encodes a PDU to bytes. This is a convenience function
// that calls the PDU's Encode method.
func EncodePDU(pdu PDU) ([]byte, error) {
	return pdu.Encode()
}

// PDUTypeString returns a human-readable string for a PDU type.
func PDUTypeString(pduType byte) string {
	switch pduType {
	case PDUTypeAssociateRQ:
		return "A-ASSOCIATE-RQ"
	case PDUTypeAssociateAC:
		return "A-ASSOCIATE-AC"
	case PDUTypeAssociateRJ:
		return "A-ASSOCIATE-RJ"
	case PDUTypeDataTF:
		return "P-DATA-TF"
	case PDUTypeReleaseRQ:
		return "A-RELEASE-RQ"
	case PDUTypeReleaseRP:
		return "A-RELEASE-RP"
	case PDUTypeAbort:
		return "A-ABORT"
	default:
		return fmt.Sprintf("Unknown(0x%02X)", pduType)
	}
}
