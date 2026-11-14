package cli

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// eventsFlag is the -events option the network commands share: a log of
// what happened on each association, one JSON object a line.
type eventsFlag struct {
	path     string
	maxBytes int
	writer   *eventWriter
}

func (f *eventsFlag) add(fs *flag.FlagSet) {
	fs.StringVar(&f.path, "events", "",
		"Append what happens on each association to this file as JSON lines, or to standard output for '-': "+
			"every PDU with its bytes, the association's events and every DIMSE message")
	fs.IntVar(&f.maxBytes, "events-pdu-bytes", 65536,
		"The most bytes of one PDU written to -events; 0 writes every byte")
}

// open returns the manager the command's SCU or SCP reports to, nil when no
// log was asked for, and what to call when the command is done.
func (f *eventsFlag) open() (*network.EventManager, func(), error) {
	if f.path == "" {
		return nil, func() {}, nil
	}
	if f.maxBytes < 0 {
		return nil, nil, fmt.Errorf("-events-pdu-bytes cannot be negative")
	}
	var out io.Writer = os.Stdout
	done := func() {}
	if f.path != "-" {
		// Appended to, so that a client and a server, or one run after
		// another, can share a file.
		file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("-events: %w", err)
		}
		out, done = file, func() { _ = file.Close() }
	}
	f.writer = &eventWriter{out: out, maxBytes: f.maxBytes}
	manager := network.NewEventManager()
	manager.OnAny(f.writer.event)
	return manager, done, nil
}

// stored records that the command kept an instance it was sent, and where.
// It is the command's own line, not one of the EVT_* events.
func (f *eventsFlag) stored(sopClassUID, sopInstanceUID, path string) {
	if f.writer == nil {
		return
	}
	f.writer.write(eventRecord{
		Event: "STORED", Time: eventTime(time.Now()),
		Stored: &storedRecord{SOPClassUID: sopClassUID, SOPInstanceUID: sopInstanceUID, Path: path},
	})
}

// eventWriter writes each event as one line. A line is written whole and at
// once, so a reader following the file never sees part of one.
type eventWriter struct {
	mu       sync.Mutex
	out      io.Writer
	maxBytes int
}

// eventRecord is one line of the log.
type eventRecord struct {
	Event         string `json:"event"`
	Time          string `json:"time"`
	AssociationID uint64 `json:"associationId,omitempty"`
	// Role is SCU on the side that requested the association, SCP on the
	// side that accepted it.
	Role       string `json:"role,omitempty"`
	LocalAE    string `json:"localAE,omitempty"`
	RemoteAE   string `json:"remoteAE,omitempty"`
	LocalAddr  string `json:"localAddr,omitempty"`
	RemoteAddr string `json:"remoteAddr,omitempty"`

	PDU      *pduRecord      `json:"pdu,omitempty"`
	Contexts []contextRecord `json:"presentationContexts,omitempty"`
	DIMSE    *dimseRecord    `json:"dimse,omitempty"`
	Stored   *storedRecord   `json:"stored,omitempty"`

	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

type pduRecord struct {
	// Type is the PDU's name, such as A-ASSOCIATE-RQ, and TypeCode its
	// first byte.
	Type     string `json:"type"`
	TypeCode int    `json:"typeCode"`
	// Length is the PDU's own length field: the bytes after its 6-byte
	// header.
	Length int `json:"length"`
	// Bytes is the PDU as it crossed the connection, in hex. When it was
	// longer than -events-pdu-bytes, the start of it.
	Bytes          string `json:"bytes"`
	BytesTruncated bool   `json:"bytesTruncated,omitempty"`

	CallingAE string `json:"callingAE,omitempty"`
	CalledAE  string `json:"calledAE,omitempty"`
	// MaxPDULength is the largest PDU the sender of an A-ASSOCIATE-RQ or
	// -AC will receive.
	MaxPDULength           *uint32 `json:"maxPDULength,omitempty"`
	ImplementationClassUID string  `json:"implementationClassUID,omitempty"`
	ImplementationVersion  string  `json:"implementationVersion,omitempty"`

	Proposed []proposedRecord `json:"proposedContexts,omitempty"`
	Answered []answeredRecord `json:"answeredContexts,omitempty"`

	// Result, Source and Reason are those of an A-ASSOCIATE-RJ; an A-ABORT
	// has the last two.
	Result *int `json:"result,omitempty"`
	Source *int `json:"source,omitempty"`
	Reason *int `json:"reason,omitempty"`

	PDVs []pdvRecord `json:"pdvs,omitempty"`
}

type proposedRecord struct {
	ID               int      `json:"id"`
	AbstractSyntax   string   `json:"abstractSyntax"`
	TransferSyntaxes []string `json:"transferSyntaxes"`
}

type answeredRecord struct {
	ID             int    `json:"id"`
	Result         int    `json:"result"`
	ResultText     string `json:"resultText"`
	TransferSyntax string `json:"transferSyntax,omitempty"`
}

type pdvRecord struct {
	ContextID int  `json:"contextId"`
	Command   bool `json:"command"`
	Last      bool `json:"last"`
	Length    int  `json:"length"`
}

// contextRecord is a presentation context as proposed and as answered.
type contextRecord struct {
	ID               int      `json:"id"`
	AbstractSyntax   string   `json:"abstractSyntax"`
	TransferSyntaxes []string `json:"transferSyntaxes"`
	Result           int      `json:"result"`
	ResultText       string   `json:"resultText"`
	TransferSyntax   string   `json:"transferSyntax,omitempty"`
}

type dimseRecord struct {
	Command                   string `json:"command"`
	CommandField              int    `json:"commandField"`
	ContextID                 int    `json:"contextId"`
	MessageID                 *int   `json:"messageId,omitempty"`
	MessageIDBeingRespondedTo *int   `json:"messageIdBeingRespondedTo,omitempty"`
	AffectedSOPClassUID       string `json:"affectedSOPClassUID,omitempty"`
	AffectedSOPInstanceUID    string `json:"affectedSOPInstanceUID,omitempty"`
	RequestedSOPClassUID      string `json:"requestedSOPClassUID,omitempty"`
	RequestedSOPInstanceUID   string `json:"requestedSOPInstanceUID,omitempty"`
	MoveDestination           string `json:"moveDestination,omitempty"`
	HasDataSet                bool   `json:"hasDataSet"`
	Status                    *int   `json:"status,omitempty"`
	Remaining                 *int   `json:"remaining,omitempty"`
	Completed                 *int   `json:"completed,omitempty"`
	Failed                    *int   `json:"failed,omitempty"`
	Warning                   *int   `json:"warning,omitempty"`
}

type storedRecord struct {
	SOPClassUID    string `json:"sopClassUID"`
	SOPInstanceUID string `json:"sopInstanceUID"`
	Path           string `json:"path"`
}

func eventTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z07:00") }

func (w *eventWriter) write(record eventRecord) {
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.out.Write(append(line, '\n'))
}

// event writes one event.
func (w *eventWriter) event(e *network.Event) {
	record := eventRecord{
		Event: network.EventTypeString(e.Type), Time: eventTime(e.Timestamp),
		AssociationID: e.AssociationID, Role: e.Role,
		LocalAE: e.LocalAE, RemoteAE: e.RemoteAE,
		LocalAddr: e.LocalAddr, RemoteAddr: e.RemoteAddr,
		Description: e.Description,
	}
	if e.Error != nil {
		record.Error = e.Error.Error()
	}
	if e.PDU != nil {
		record.PDU = w.pdu(e.PDU, e.PDUBytes())
	}
	for _, pc := range e.PresentationContexts() {
		record.Contexts = append(record.Contexts, contextRecord{
			ID: int(pc.ID), AbstractSyntax: pc.AbstractSyntax, TransferSyntaxes: pc.Proposed,
			Result: int(pc.Result), ResultText: contextResult(pc.Result), TransferSyntax: pc.TransferSyntax,
		})
	}
	if c := e.Command; c != nil {
		number := func(v *uint16) *int {
			if v == nil {
				return nil
			}
			n := int(*v)
			return &n
		}
		dimse := &dimseRecord{
			Command: c.Name, CommandField: int(c.CommandField), ContextID: int(c.ContextID),
			AffectedSOPClassUID: c.AffectedSOPClassUID, AffectedSOPInstanceUID: c.AffectedSOPInstanceUID,
			RequestedSOPClassUID: c.RequestedSOPClassUID, RequestedSOPInstanceUID: c.RequestedSOPInstanceUID,
			MoveDestination: c.MoveDestination, HasDataSet: c.HasDataSet,
			Status: number(c.Status), Remaining: number(c.Remaining), Completed: number(c.Completed),
			Failed: number(c.Failed), Warning: number(c.Warning),
		}
		// A request has its own ID, a response the ID of what it answers.
		if c.IsResponse() {
			dimse.MessageIDBeingRespondedTo = number(&c.MessageIDBeingRespondedTo)
		} else {
			dimse.MessageID = number(&c.MessageID)
		}
		record.DIMSE = dimse
	}
	w.write(record)
}

func (w *eventWriter) pdu(pdu network.PDU, raw []byte) *pduRecord {
	record := &pduRecord{TypeCode: int(pdu.Type()), Length: len(raw) - 6}
	if record.Length < 0 {
		record.Length = 0
	}
	if w.maxBytes > 0 && len(raw) > w.maxBytes {
		raw, record.BytesTruncated = raw[:w.maxBytes], true
	}
	record.Bytes = hex.EncodeToString(raw)

	number := func(b byte) *int { n := int(b); return &n }
	user := func(u network.UserInformationItem) {
		if u.MaxPDULength != 0 {
			record.MaxPDULength = &u.MaxPDULength
		}
		record.ImplementationClassUID, record.ImplementationVersion = u.ImplementationClassUID, u.ImplementationVersion
	}
	switch p := pdu.(type) {
	case *network.AssociateRQ:
		record.Type, record.CallingAE, record.CalledAE = "A-ASSOCIATE-RQ", p.CallingAE, p.CalledAE
		user(p.UserInformation)
		for _, pc := range p.PresentationContexts {
			record.Proposed = append(record.Proposed, proposedRecord{int(pc.ID), pc.AbstractSyntax, pc.TransferSyntaxes})
		}
	case *network.AssociateAC:
		record.Type, record.CallingAE, record.CalledAE = "A-ASSOCIATE-AC", p.CallingAE, p.CalledAE
		user(p.UserInformation)
		for _, pc := range p.PresentationContexts {
			answer := answeredRecord{ID: int(pc.ID), Result: int(pc.Result), ResultText: contextResult(pc.Result)}
			if pc.Result == network.PCResultAcceptance {
				answer.TransferSyntax = pc.TransferSyntax
			}
			record.Answered = append(record.Answered, answer)
		}
	case *network.AssociateRJ:
		record.Type = "A-ASSOCIATE-RJ"
		record.Result, record.Source, record.Reason = number(p.Result), number(p.Source), number(p.Reason)
	case *network.PDataTF:
		record.Type = "P-DATA-TF"
		for _, pdv := range p.PDVItems {
			record.PDVs = append(record.PDVs, pdvRecord{int(pdv.PresentationContextID), pdv.IsCommand, pdv.IsLast, len(pdv.Data)})
		}
	case *network.ReleaseRQ:
		record.Type = "A-RELEASE-RQ"
	case *network.ReleaseRP:
		record.Type = "A-RELEASE-RP"
	case *network.AbortPDU:
		record.Type = "A-ABORT"
		record.Source, record.Reason = number(p.Source), number(p.Reason)
	default:
		record.Type = "UNKNOWN"
	}
	return record
}

// contextResult names the answer to a presentation context (PS3.8 Table 9-18).
func contextResult(result byte) string {
	switch result {
	case network.PCResultAcceptance:
		return "acceptance"
	case network.PCResultUserRejection:
		return "user-rejection"
	case network.PCResultNoReason:
		return "no-reason"
	case network.PCResultAbstractSyntaxNotSupported:
		return "abstract-syntax-not-supported"
	case network.PCResultTransferSyntaxNotSupported:
		return "transfer-syntaxes-not-supported"
	}
	return fmt.Sprintf("unknown (%d)", result)
}
