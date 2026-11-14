package network

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amrshadid/go-dicom/tag"
)

// associationIDs numbers the connections that report events.
var associationIDs atomic.Uint64

// observer reports what passes over one connection as events.
//
// It sits on the Transport because that is the one place every PDU goes
// through, in both directions and on both sides. The association events and
// the DIMSE events are read off the PDUs, so nothing an SCU or an SCP does
// can happen without being seen, and neither has to remember to say so.
type observer struct {
	events *EventManager
	id     uint64
	local  string
	remote string

	mu       sync.Mutex
	role     string
	localAE  string
	remoteAE string
	calling  string
	called   string
	proposed []PresentationContextItem
	// The command set being assembled from PDVs, each way: [0] sent, [1]
	// received.
	command [2]bytes.Buffer
}

// observe makes the transport report to events. Nil stops nothing and costs
// nothing.
func (t *Transport) observe(events *EventManager) {
	if events == nil {
		return
	}
	o := &observer{events: events, id: associationIDs.Add(1)}
	if addr := t.LocalAddr(); addr != nil {
		o.local = addr.String()
	}
	if addr := t.RemoteAddr(); addr != nil {
		o.remote = addr.String()
	}
	t.mu.Lock()
	t.obs = o
	t.mu.Unlock()
	o.events.Emit(o.event(EVTConnOpen))
}

// observed returns the transport's observer, nil when it has none.
func (t *Transport) observed() *observer {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.obs
}

// emit reports an event with no PDU of its own: the connection opening or
// closing.
func (t *Transport) emit(eventType EventType) {
	if o := t.observed(); o != nil {
		o.events.Emit(o.event(eventType))
	}
}

// event is an event of this connection, with what is known of it so far.
func (o *observer) event(eventType EventType) *Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	return &Event{
		Type: eventType, Timestamp: time.Now(),
		AssociationID: o.id, Role: o.role,
		LocalAE: o.localAE, RemoteAE: o.remoteAE,
		CallingAE: o.calling, CalledAE: o.called,
		LocalAddr: o.local, RemoteAddr: o.remote,
	}
}

// pdu reports a PDU that was sent or received, and whatever it means for
// the association and for the DIMSE message it is part of.
func (o *observer) pdu(sent bool, pdu PDU, raw []byte) {
	eventType, direction := EVTPDURecv, 1
	if sent {
		eventType, direction = EVTPDUSent, 0
	}
	with := func(t EventType) *Event {
		e := o.event(t)
		e.PDU, e.PDUBytes, e.PDUType = pdu, raw, pdu.Type()
		return e
	}

	switch p := pdu.(type) {
	case *AssociateRQ:
		// Whoever sends the request is the SCU of this association.
		o.mu.Lock()
		o.calling, o.called = p.CallingAE, p.CalledAE
		o.proposed = p.PresentationContexts
		if sent {
			o.role, o.localAE, o.remoteAE = "SCU", p.CallingAE, p.CalledAE
		} else {
			o.role, o.localAE, o.remoteAE = "SCP", p.CalledAE, p.CallingAE
		}
		o.mu.Unlock()
		o.events.Emit(with(eventType))
		o.events.Emit(with(EVTAssocRequested))

	case *AssociateAC:
		o.events.Emit(with(eventType))
		contexts := o.negotiated(p)
		for _, t := range []EventType{EVTAssocAccepted, EVTAssocEstablished} {
			e := with(t)
			e.PresentationContexts = contexts
			o.events.Emit(e)
		}

	case *AssociateRJ:
		o.events.Emit(with(eventType))
		o.events.Emit(with(EVTAssocRejected))

	case *ReleaseRP:
		o.events.Emit(with(eventType))
		o.events.Emit(with(EVTAssocReleased))

	case *AbortPDU:
		o.events.Emit(with(eventType))
		o.events.Emit(with(EVTAssocAborted))

	case *PDataTF:
		o.events.Emit(with(eventType))
		for _, pdv := range p.PDVItems {
			if !pdv.IsCommand {
				continue
			}
			o.mu.Lock()
			buffer := &o.command[direction]
			// A command set is a few hundred bytes. One that is not is not
			// kept: the association will refuse it for itself.
			if buffer.Len()+len(pdv.Data) <= maxCommandSetSize {
				buffer.Write(pdv.Data)
			}
			var whole []byte
			if pdv.IsLast {
				whole = append(whole, buffer.Bytes()...)
				buffer.Reset()
			}
			o.mu.Unlock()
			if pdv.IsLast {
				o.dimse(sent, pdv.PresentationContextID, whole)
			}
		}

	default:
		o.events.Emit(with(eventType))
	}
}

// negotiated pairs each answer of an A-ASSOCIATE-AC with what was proposed.
func (o *observer) negotiated(ac *AssociateAC) []NegotiatedContext {
	o.mu.Lock()
	defer o.mu.Unlock()
	proposed := make(map[byte]PresentationContextItem, len(o.proposed))
	for _, pc := range o.proposed {
		proposed[pc.ID] = pc
	}
	out := make([]NegotiatedContext, 0, len(ac.PresentationContexts))
	for _, result := range ac.PresentationContexts {
		context := NegotiatedContext{
			ID:             result.ID,
			AbstractSyntax: proposed[result.ID].AbstractSyntax,
			Proposed:       proposed[result.ID].TransferSyntaxes,
			Result:         result.Result,
		}
		if result.Result == PCResultAcceptance {
			context.TransferSyntax = result.TransferSyntax
		}
		out = append(out, context)
	}
	return out
}

// dimse reports a complete command set, and the service event of a request
// that was received.
func (o *observer) dimse(sent bool, contextID byte, commandSet []byte) {
	ds, err := DecodeCommandDataset(commandSet)
	if err != nil {
		return
	}
	command := &CommandInfo{ContextID: contextID}
	field, err := getUSValue(ds, tagCommandField)
	if err != nil {
		return
	}
	command.CommandField = field
	command.Name = commandName(field)
	command.MessageID, _ = getUSValue(ds, tagMessageID)
	command.MessageIDBeingRespondedTo, _ = getUSValue(ds, tagMessageIDBeingRespondedTo)
	command.AffectedSOPClassUID, _ = getUIValue(ds, tagAffectedSOPClassUID)
	command.AffectedSOPInstanceUID, _ = getUIValue(ds, tagAffectedSOPInstanceUID)
	command.RequestedSOPClassUID, _ = getUIValue(ds, tagRequestedSOPClassUID)
	command.RequestedSOPInstanceUID, _ = getUIValue(ds, tagRequestedSOPInstanceUID)
	if destination, err := getUIValue(ds, tagMoveDestination); err == nil {
		command.MoveDestination = trimAE(destination)
	}
	if dataSetType, err := getUSValue(ds, tagCommandDataSetType); err == nil {
		command.HasDataSet = dataSetType != CommandDataSetTypeNull
	}
	optional := func(t tag.Tag) *uint16 {
		if v, err := getUSValue(ds, t); err == nil {
			return &v
		}
		return nil
	}
	command.Status = optional(tagStatus)
	command.Remaining = optional(tagNumberOfRemainingSuboperations)
	command.Completed = optional(tagNumberOfCompletedSuboperations)
	command.Failed = optional(tagNumberOfFailedSuboperations)
	command.Warning = optional(tagNumberOfWarningSuboperations)

	eventType := EVTDIMSERecv
	if sent {
		eventType = EVTDIMSESent
	}
	e := o.event(eventType)
	e.Command, e.CommandType = command, field
	o.events.Emit(e)

	// The service events are of a request arriving, as pynetdicom's are.
	if service, ok := serviceEvents[field]; ok && !sent {
		e := o.event(service)
		e.Command, e.CommandType = command, field
		o.events.Emit(e)
	}
}

// trimAE removes the padding of an AE title.
func trimAE(s string) string {
	return string(bytes.TrimRight([]byte(s), " \x00"))
}

// serviceEvents names the event of each request.
var serviceEvents = map[uint16]EventType{
	CommandCEchoRQ:        EVTCEcho,
	CommandCStoreRQ:       EVTCStore,
	CommandCFindRQ:        EVTCFind,
	CommandCMoveRQ:        EVTCMove,
	CommandCGetRQ:         EVTCGet,
	CommandNEventReportRQ: EVTNEventReport,
	CommandNGetRQ:         EVTNGet,
	CommandNSetRQ:         EVTNSet,
	CommandNActionRQ:      EVTNAction,
	CommandNCreateRQ:      EVTNCreate,
	CommandNDeleteRQ:      EVTNDelete,
}

// commandName is the usual name of a command field (PS3.7 E.1).
func commandName(field uint16) string {
	names := map[uint16]string{
		0x0001: "C-STORE", 0x0010: "C-GET", 0x0020: "C-FIND", 0x0021: "C-MOVE", 0x0030: "C-ECHO",
		0x0100: "N-EVENT-REPORT", 0x0110: "N-GET", 0x0120: "N-SET", 0x0130: "N-ACTION",
		0x0140: "N-CREATE", 0x0150: "N-DELETE",
	}
	if field == CommandCCancelRQ {
		return "C-CANCEL-RQ"
	}
	name, ok := names[field&0x7FFF]
	if !ok {
		return "UNKNOWN"
	}
	if field&0x8000 != 0 {
		return name + "-RSP"
	}
	return name + "-RQ"
}
