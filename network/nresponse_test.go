package network

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// replyingNSCP accepts one association and answers N-CREATE, N-ACTION and
// N-EVENT-REPORT with a data set after the response, as PS3.7 10.1 lets an SCP
// do, and N-SET with 0x0110 Processing failure and no data set.
func replyingNSCP(t *testing.T, conn net.Conn, ctx context.Context) {
	t.Helper()
	transport := NewTransport(conn, DefaultMaxPDUSize)
	defer transport.Close()

	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		t.Errorf("mock SCP: read RQ: %v", err)
		return
	}
	rq, ok := pdu.(*AssociateRQ)
	if !ok {
		t.Errorf("mock SCP: expected AssociateRQ, got %T", pdu)
		return
	}
	assoc := NewAssociation(transport)
	supportedAS := map[string]bool{ModalityPerformedProcedureStepUID: true}
	supportedTS := map[string]bool{ExplicitVRLittleEndianUID: true, ImplicitVRLittleEndianUID: true}
	if err := assoc.AcceptAssociation(ctx, rq, supportedAS, supportedTS, DefaultMaxPDUSize); err != nil {
		t.Errorf("mock SCP: accept: %v", err)
		return
	}

	for {
		pcID, cmdData, isCmd, err := assoc.ReceivePData(ctx)
		if err != nil {
			return
		}
		if !isCmd {
			continue // a request's data set; its command was answered already
		}
		cmdDS, err := DecodeCommandDataset(cmdData)
		if err != nil {
			t.Errorf("mock SCP: decode: %v", err)
			return
		}
		field, msgID, _, _ := ParseCommandDataset(cmdDS)
		if HasDataSet(cmdDS) {
			if _, _, _, err := assoc.ReceivePData(ctx); err != nil {
				return
			}
		}

		var rsp *dataset.Dataset
		var reply string
		switch field {
		case CommandNCreateRQ:
			rsp, reply = BuildNCreateRSP(msgID, ModalityPerformedProcedureStepUID, "1.2.3.4", StatusSuccess), "CREATED"
		case CommandNActionRQ:
			rsp, reply = BuildNActionRSP(msgID, ModalityPerformedProcedureStepUID, "1.2.3.4", 1, StatusSuccess), "ACTED"
		case CommandNEventReportRQ:
			rsp, reply = BuildNEventReportRSP(msgID, ModalityPerformedProcedureStepUID, "1.2.3.4", 1, StatusSuccess), "REPORTED"
		case CommandNSetRQ:
			rsp = BuildNSetRSP(msgID, ModalityPerformedProcedureStepUID, "1.2.3.4", StatusProcessingFailure)
		default:
			t.Errorf("mock SCP: unexpected command 0x%04X", field)
			return
		}
		if reply != "" {
			setDataSetType(rsp, CommandDataSetTypePresent)
		}
		rspBytes, _ := EncodeCommandDataset(rsp)
		if err := assoc.SendPData(ctx, pcID, rspBytes, true); err != nil {
			return
		}
		if reply != "" {
			ds := dataset.NewDataset()
			_ = ds.Add(dataelem.NewDataElement(tag.New(0x0040, 0x0252), dataelem.CS, []byte(padEven(reply))))
			data, _ := EncodeDataset(ds, assoc.TransferSyntaxFor(pcID))
			if err := assoc.SendPData(ctx, pcID, data, false); err != nil {
				return
			}
		}
	}
}

// setDataSetType replaces a command's Command Data Set Type.
func setDataSetType(cmd *dataset.Dataset, value uint16) {
	cmd.Remove(tagCommandDataSetType)
	addUSElement(cmd, tagCommandDataSetType, value)
}

// padEven pads a CS value to even length (PS3.5 6.2).
func padEven(s string) string {
	if len(s)%2 != 0 {
		return s + " "
	}
	return s
}

// TestNServiceResponseDataSetIsRead: an N-CREATE, N-ACTION and N-EVENT-REPORT
// response may carry a data set (PS3.7 10.1), and the SCU read only the
// command, so the data set stayed on the association and the next operation
// read it as its response (#379). With MPPS that was the N-SET reporting the
// step complete: it panicked decoding a data set as a command, or read status
// 0 from it and reported the SCP's failure as success. Each response's data
// set is now returned with it, and the N-SET gets its own status.
func TestNServiceResponseDataSetIsRead(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go replyingNSCP(t, server, ctx)

	scu := NewSCU(SCUConfig{CallingAE: "TEST_SCU", CalledAE: "TEST_SCP"})
	scu.association = NewAssociation(NewTransport(client, DefaultMaxPDUSize))
	contexts := []PresentationContextItem{{ID: 1, AbstractSyntax: ModalityPerformedProcedureStepUID,
		TransferSyntaxes: []string{ExplicitVRLittleEndianUID}}}
	if err := scu.association.RequestAssociation(ctx, "TEST_SCU", "TEST_SCP", contexts, DefaultMaxPDUSize); err != nil {
		t.Fatalf("associate: %v", err)
	}

	attrs := dataset.NewDataset()
	_ = attrs.Add(dataelem.NewDataElement(tag.New(0x0040, 0x0252), dataelem.CS, []byte("IN PROGRESS ")))
	replied := func(op string, ds *dataset.Dataset, want string) {
		t.Helper()
		got := ""
		if ds != nil {
			if elem, ok := ds.Get(tag.New(0x0040, 0x0252)); ok {
				if b, ok := elem.GetValue().([]byte); ok {
					got = strings.TrimRight(string(b), " ")
				}
			}
		}
		if got != want {
			t.Errorf("%s: the response's data set says %q, want %q", op, got, want)
		}
	}

	created, err := scu.NCreate(ctx, ModalityPerformedProcedureStepUID, "1.2.3.4", attrs)
	if err != nil {
		t.Fatalf("N-CREATE: %v", err)
	}
	replied("N-CREATE", created.DataSet, "CREATED")

	set, err := scu.NSet(ctx, ModalityPerformedProcedureStepUID, "1.2.3.4", attrs)
	if err != nil {
		t.Fatalf("N-SET: %v", err)
	}
	if set.Status != StatusProcessingFailure {
		t.Errorf("N-SET: status 0x%04X, want 0x0110: the SCP's failure was not the status read", set.Status)
	}
	if set.DataSet != nil {
		t.Error("N-SET: a data set was returned where the SCP sent none")
	}

	acted, err := scu.NAction(ctx, ModalityPerformedProcedureStepUID, "1.2.3.4", 1, nil)
	if err != nil {
		t.Fatalf("N-ACTION: %v", err)
	}
	replied("N-ACTION", acted.DataSet, "ACTED")

	reported, err := scu.NEventReport(ctx, ModalityPerformedProcedureStepUID, "1.2.3.4", 1, attrs)
	if err != nil {
		t.Fatalf("N-EVENT-REPORT: %v", err)
	}
	replied("N-EVENT-REPORT", reported.DataSet, "REPORTED")
}
