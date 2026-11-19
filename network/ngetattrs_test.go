package network

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// nGetListRecorder records the Attribute Identifier List each N-GET handler
// call was given.
type nGetListRecorder struct {
	BaseHandler
	lists chan []tag.Tag
}

func (h *nGetListRecorder) HandleNGet(_ context.Context, req *NGetRequest) (*NGetResponse, error) {
	h.lists <- req.AttributeList
	return &NGetResponse{Status: StatusSuccess}, nil
}

// TestNGetHandlerIsGivenTheAttributeIdentifierList: the SCP read the
// Requested SOP Class and Instance UIDs of an N-GET-RQ and ignored Attribute
// Identifier List (0000,1005), so a handler could not tell a request for one
// attribute from a request for all of them (PS3.7 10.1.2.1.4, #380). Three
// requests go to one SCP over a pipe: one with two tags, which the handler
// receives in the order sent; one with no list, which it receives as nil;
// and one whose list is six bytes, not a whole number of tags, which is
// answered 0x0110 without calling the handler.
func TestNGetHandlerIsGivenTheAttributeIdentifierList(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	recorder := &nGetListRecorder{lists: make(chan []tag.Tag, 3)}
	scp := NewSCP(SCPConfig{AETitle: "TEST_SCP"})
	scp.SetHandler(recorder)
	scp.SetSupportedAbstractSyntaxes([]string{ModalityPerformedProcedureStepUID})
	go scp.handleConnection(ctx, NewTransport(serverConn, DefaultMaxPDUSize))

	assoc := NewAssociation(NewTransport(clientConn, DefaultMaxPDUSize))
	contexts := []PresentationContextItem{{ID: 1, AbstractSyntax: ModalityPerformedProcedureStepUID,
		TransferSyntaxes: []string{ImplicitVRLittleEndianUID}}}
	if err := assoc.RequestAssociation(ctx, "TEST_SCU", "TEST_SCP", contexts, DefaultMaxPDUSize); err != nil {
		t.Fatalf("associate: %v", err)
	}

	// nGet sends an N-GET-RQ whose (0000,1005) holds list, or has no
	// (0000,1005) when list is nil, and returns the response's status.
	nGet := func(messageID uint16, list []byte) uint16 {
		t.Helper()
		cmd := BuildNGetRQ(messageID, ModalityPerformedProcedureStepUID, "1.2.826.0.1.3680043.10.511.380.1")
		if list != nil {
			if err := cmd.Add(dataelem.NewDataElement(tagAttributeIdentifierList, dataelem.AT, list)); err != nil {
				t.Fatal(err)
			}
		}
		encoded, err := EncodeCommandDataset(cmd)
		if err != nil {
			t.Fatal(err)
		}
		if err := assoc.SendPData(ctx, 1, encoded, true); err != nil {
			t.Fatalf("send N-GET-RQ: %v", err)
		}
		msg, err := assoc.ReceiveResponse(ctx, messageID)
		if err != nil {
			t.Fatalf("receive N-GET-RSP: %v", err)
		}
		rsp, err := DecodeCommandDataset(msg.command)
		if err != nil {
			t.Fatal(err)
		}
		_, _, status, _ := ParseCommandDataset(rsp)
		return status
	}

	// (2100,0020) Execution Status, then (0040,0252) Performed Procedure Step
	// Status, each as group then element, little endian.
	two := []byte{0x00, 0x21, 0x20, 0x00, 0x40, 0x00, 0x52, 0x02}
	if status := nGet(1, two); status != StatusSuccess {
		t.Fatalf("N-GET with a list: status 0x%04X", status)
	}
	want := []tag.Tag{tag.New(0x2100, 0x0020), tag.New(0x0040, 0x0252)}
	if got := <-recorder.lists; !reflect.DeepEqual(got, want) {
		t.Errorf("handler was given %v, want %v", got, want)
	}

	if status := nGet(2, nil); status != StatusSuccess {
		t.Fatalf("N-GET without a list: status 0x%04X", status)
	}
	if got := <-recorder.lists; got != nil {
		t.Errorf("handler was given %v for a request with no list, want nil", got)
	}

	if status := nGet(3, two[:6]); status != StatusProcessingFailure {
		t.Errorf("N-GET with a 6-byte list: status 0x%04X, want 0x0110", status)
	}
	select {
	case got := <-recorder.lists:
		t.Errorf("the handler was called for a list that is not a list of tags, with %v", got)
	default:
	}
}

// TestSCUSendsTheAttributeIdentifierList: SCU.NGet had no way to name the
// attributes wanted, though an SCP has read the list since #380. Over a real
// association, NGetAttributes sends the tags it is given, in that order, and
// the SCP's handler receives them; NGet, and NGetAttributes with an empty
// list, send none, which the handler receives as nil: every attribute.
func TestSCUSendsTheAttributeIdentifierList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	recorder := &nGetListRecorder{lists: make(chan []tag.Tag, 3)}
	server, err := StartServer(ctx, SCPConfig{AETitle: "NGET_SCP", Port: 0, BindAddress: "127.0.0.1"}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{ModalityPerformedProcedureStepUID})

	scu := NewSCU(SCUConfig{CallingAE: "NGET_SCU", CalledAE: "NGET_SCP", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{{ID: 1, AbstractSyntax: ModalityPerformedProcedureStepUID,
		TransferSyntaxes: []string{ImplicitVRLittleEndianUID}}}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scu.Release(ctx) }()

	const instance = "1.2.826.0.1.3680043.10.511.486.1"
	want := []tag.Tag{tag.New(0x0040, 0x0252), tag.New(0x2100, 0x0020), tag.New(0x0008, 0x1150)}
	if rsp, err := scu.NGetAttributes(ctx, ModalityPerformedProcedureStepUID, instance, want); err != nil || rsp.Status != StatusSuccess {
		t.Fatalf("NGetAttributes: %v, %+v", err, rsp)
	}
	if got := <-recorder.lists; !reflect.DeepEqual(got, want) {
		t.Errorf("the handler was given %v, want %v", got, want)
	}

	if _, err := scu.NGet(ctx, ModalityPerformedProcedureStepUID, instance); err != nil {
		t.Fatalf("NGet: %v", err)
	}
	if got := <-recorder.lists; got != nil {
		t.Errorf("NGet sent the list %v, want none", got)
	}
	if _, err := scu.NGetAttributes(ctx, ModalityPerformedProcedureStepUID, instance, []tag.Tag{}); err != nil {
		t.Fatalf("NGetAttributes with an empty list: %v", err)
	}
	if got := <-recorder.lists; got != nil {
		t.Errorf("an empty list was sent as %v, want no list", got)
	}
}
