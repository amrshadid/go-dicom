package network_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// The instances replyingHandler is addressed through say what it answers with.
const (
	instanceWithReply    = "1.2.826.0.1.3680043.10.511.399.1"
	instanceWithoutReply = "1.2.826.0.1.3680043.10.511.399.2"
	instanceBrokenReply  = "1.2.826.0.1.3680043.10.511.399.3"
)

var tagPPSStatus = tag.New(0x0040, 0x0252)

// replyingHandler answers N-CREATE, N-SET, N-ACTION, N-EVENT-REPORT and N-GET with
// success. For instanceWithReply the response carries a data set naming the
// service, for instanceWithoutReply it carries none, and for
// instanceBrokenReply it carries one holding a value no transfer syntax can
// encode.
type replyingHandler struct {
	network.BaseHandler
	t *testing.T
}

func (h replyingHandler) reply(instance, service string) *dataset.Dataset {
	switch instance {
	case instanceWithReply:
		ds := dataset.NewDataset()
		add(h.t, ds, 0x0040, 0x0252, dataelem.CS, padCS(service))
		return ds
	case instanceBrokenReply:
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tagPPSStatus, dataelem.CS, struct{}{}))
		return ds
	}
	return nil
}

func (h replyingHandler) HandleNCreate(_ context.Context, req *network.NCreateRequest) (*network.NCreateResponse, error) {
	return &network.NCreateResponse{Status: network.StatusSuccess,
		DataSet: h.reply(req.AffectedSOPInstance, "CREATED")}, nil
}

func (h replyingHandler) HandleNSet(_ context.Context, req *network.NSetRequest) (*network.NSetResponse, error) {
	return &network.NSetResponse{Status: network.StatusSuccess,
		DataSet: h.reply(req.RequestedSOPInstance, "SET")}, nil
}

func (h replyingHandler) HandleNAction(_ context.Context, req *network.NActionRequest) (*network.NActionResponse, error) {
	return &network.NActionResponse{Status: network.StatusSuccess, ActionTypeID: req.ActionTypeID,
		DataSet: h.reply(req.RequestedSOPInstance, "ACTED")}, nil
}

func (h replyingHandler) HandleNGet(_ context.Context, req *network.NGetRequest) (*network.NGetResponse, error) {
	return &network.NGetResponse{Status: network.StatusSuccess,
		DataSet: h.reply(req.RequestedSOPInstance, "GOT")}, nil
}

func (h replyingHandler) HandleNEventReport(_ context.Context, req *network.NEventReportRequest) (*network.NEventReportResponse, error) {
	return &network.NEventReportResponse{Status: network.StatusSuccess, EventTypeID: req.EventTypeID,
		DataSet: h.reply(req.AffectedSOPInstance, "REPORTED")}, nil
}

// padCS pads a CS value to even length (PS3.5 6.2).
func padCS(s string) string {
	if len(s)%2 != 0 {
		return s + " "
	}
	return s
}

// nResult is what one N-service call returned, whichever service it was.
type nResult struct {
	status uint16
	data   *dataset.Dataset
}

// callEachNService sends N-CREATE, N-SET, N-ACTION, N-EVENT-REPORT and N-GET for the
// instance, in that order, and returns what each response carried.
func callEachNService(ctx context.Context, t *testing.T, scu *network.SCU, instance string) map[string]nResult {
	t.Helper()
	class := network.ModalityPerformedProcedureStepUID
	request := dataset.NewDataset()
	add(t, request, 0x0040, 0x0252, dataelem.CS, "IN PROGRESS ")
	got := map[string]nResult{}

	created, err := scu.NCreate(ctx, class, instance, request)
	if err != nil {
		t.Fatalf("N-CREATE %s: %v", instance, err)
	}
	got["N-CREATE"] = nResult{created.Status, created.DataSet}
	set, err := scu.NSet(ctx, class, instance, request)
	if err != nil {
		t.Fatalf("N-SET %s: %v", instance, err)
	}
	got["N-SET"] = nResult{set.Status, set.DataSet}
	acted, err := scu.NAction(ctx, class, instance, 1, request)
	if err != nil {
		t.Fatalf("N-ACTION %s: %v", instance, err)
	}
	got["N-ACTION"] = nResult{acted.Status, acted.DataSet}
	reported, err := scu.NEventReport(ctx, class, instance, 1, request)
	if err != nil {
		t.Fatalf("N-EVENT-REPORT %s: %v", instance, err)
	}
	got["N-EVENT-REPORT"] = nResult{reported.Status, reported.DataSet}
	fetched, err := scu.NGet(ctx, class, instance)
	if err != nil {
		t.Fatalf("N-GET %s: %v", instance, err)
	}
	got["N-GET"] = nResult{fetched.Status, fetched.DataSet}
	return got
}

// TestSCPSendsTheHandlersResponseDataSet: an N-CREATE, N-SET, N-ACTION or
// N-EVENT-REPORT response may carry a data set (PS3.7 10.1), and the SCP sent
// only the handler's status, with a Command Data Set Type saying no data set
// follows (#399). An MPPS SCP could not return the attribute list PS3.4
// F.7.2.1.2 describes. Each handler's data set now reaches the requester with
// its own response; a handler that returns none sends none, and the
// association stays in step either way, which a command promising a data set
// it does not send would break; a data set that cannot be encoded is answered
// with 0x0110 Processing failure and no data set, rather than with a promise
// of one that never arrives. N-GET, which did send its data set, is checked
// beside them because it now goes through the same path.
func TestSCPSendsTheHandlersResponseDataSet(t *testing.T) {
	scp := network.NewSCP(network.SCPConfig{AETitle: "GO_NSCP", Port: freePort(t), BindAddress: "127.0.0.1"})
	scp.SetHandler(&replyingHandler{t: t})
	scp.SetSupportedAbstractSyntaxes([]string{network.ModalityPerformedProcedureStepUID})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addr := serveSCP(ctx, t, scp)

	scu := network.NewSCU(network.SCUConfig{CallingAE: "GO_NSCU", CalledAE: "GO_NSCP", Address: addr})
	if err := scu.Associate(ctx, network.ModalityPerformedProcedurePresentationContexts()); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	services := []struct{ name, reply string }{
		{"N-CREATE", "CREATED"}, {"N-SET", "SET"}, {"N-ACTION", "ACTED"}, {"N-EVENT-REPORT", "REPORTED"},
		{"N-GET", "GOT"},
	}

	with := callEachNService(ctx, t, scu, instanceWithReply)
	for _, s := range services {
		r := with[s.name]
		if r.status != network.StatusSuccess {
			t.Errorf("%s with a reply: status 0x%04X, want 0x0000", s.name, r.status)
		}
		if r.data == nil {
			t.Errorf("%s: the handler's data set was not sent", s.name)
			continue
		}
		if got := strings.TrimSpace(ppsStatus(r.data)); got != s.reply {
			t.Errorf("%s: the response's data set says %q, want %q", s.name, got, s.reply)
		}
	}

	without := callEachNService(ctx, t, scu, instanceWithoutReply)
	for _, s := range services {
		if r := without[s.name]; r.status != network.StatusSuccess || r.data != nil {
			t.Errorf("%s without a reply: status 0x%04X, data set %v; want 0x0000 and none",
				s.name, r.status, r.data != nil)
		}
	}

	broken := callEachNService(ctx, t, scu, instanceBrokenReply)
	for _, s := range services {
		if r := broken[s.name]; r.status != network.StatusProcessingFailure || r.data != nil {
			t.Errorf("%s with an unencodable reply: status 0x%04X, data set %v; want 0x0110 and none",
				s.name, r.status, r.data != nil)
		}
	}
}

// ppsStatus is Performed Procedure Step Status (0040,0252) as text.
func ppsStatus(ds *dataset.Dataset) string {
	elem, ok := ds.Get(tagPPSStatus)
	if !ok {
		return ""
	}
	value, _ := elem.GetValue().([]byte)
	return string(value)
}
