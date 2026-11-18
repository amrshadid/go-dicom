package network_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Media Creation Management (PS3.4 Annex S) and the two Application Event
// Logging classes, Procedural Event Logging and Substance Administration
// Logging (PS3.4 Annex P), had UID constants and presentation context helpers
// and had never sent a message to a peer. They are built as MPPS and print
// management are, from N-CREATE, N-ACTION and N-GET, and those primitives are
// where this library's worst defect hid: the target named with the wrong
// command element, read the same wrong way by go-dicom's own SCP. So each is
// run against pynetdicom in both directions, and what pynetdicom reports is
// its reading of our bytes.

const (
	// A media creation request this test makes up; Media Creation Management
	// has no well-known instance, the SCU names (or the SCP assigns) one.
	mediaRequestUID = "1.2.826.0.1.3680043.10.511.33.1"
	// An instance the request asks to be written to the medium.
	mediaReferencedUID = "1.2.826.0.1.3680043.10.511.33.2"
	eventStudyUID      = "1.2.826.0.1.3680043.10.511.40.1"
)

// The attributes these services carry (PS3.3 C.30, PS3.4 S.3.2 and P.3).
var (
	tagLabelText             = tag.New(0x2200, 0x0002)
	tagExecutionStatus       = tag.New(0x2100, 0x0020)
	tagNumberOfCopies        = tag.New(0x2000, 0x0010)
	tagReferencedSOPSequence = tag.New(0x0008, 0x1199)
	tagRefSOPInstanceUID     = tag.New(0x0008, 0x1155)
	tagPatientID             = tag.New(0x0010, 0x0020)
	tagContentSequence       = tag.New(0x0040, 0xA730)
	tagTextValue             = tag.New(0x0040, 0xA160)
	tagProductName           = tag.New(0x0044, 0x0008)
)

// mediaCreationRequest is the N-CREATE attribute list of a request to write
// one CT instance to a medium labeled GO MEDIA.
func mediaCreationRequest(t *testing.T) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	add(t, ds, 0x2200, 0x0002, dataelem.UT, "GO MEDIA")
	add(t, ds, 0x0088, 0x0130, dataelem.SH, "GOFILESET1")
	item := dataset.NewDataset()
	add(t, item, 0x0008, 0x1150, dataelem.UI, network.CTImageStorageUID)
	add(t, item, 0x0008, 0x1155, dataelem.UI, mediaReferencedUID)
	seq := sequence.New()
	if err := seq.Append(item); err != nil {
		t.Fatal(err)
	}
	if err := ds.AddSequence(tagReferencedSOPSequence, seq); err != nil {
		t.Fatal(err)
	}
	return ds
}

// TestMediaCreationAgainstPynetdicom drives a media creation request as an
// SCU would (PS3.4 S.3): N-CREATE the request with the instances to write,
// N-ACTION 1 to initiate it, N-GET to ask how it went. pynetdicom is the SCP
// and records what it read; the status it answers N-GET with comes back
// through this library's decoder.
func TestMediaCreationAgainstPynetdicom(t *testing.T) {
	record := t.TempDir() + "/events.txt"
	addr := startPynetdicomSCP(t, `
from pydicom.dataset import Dataset
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import MediaCreationManagement

record = open(`+quote(record)+`, "w", buffering=1)

def on_create(event):
    ds = event.attribute_list
    ref = ds.ReferencedSOPSequence
    record.write("CREATE %s %s %s %d %s\n" % (event.request.AffectedSOPInstanceUID,
        ds.LabelText, ds.StorageMediaFileSetID, len(ref), ref[0].ReferencedSOPInstanceUID))
    # No attribute list in the response: an SCU leaves that data set unread
    # and the next operation takes it for its response (#379).
    return 0x0000, None

def on_action(event):
    info = event.action_information
    record.write("ACTION %d %s %s %s\n" % (event.action_type,
        event.request.RequestedSOPInstanceUID, info.NumberOfCopies, info.RequestPriority))
    return 0x0000, None

def on_get(event):
    record.write("GET %s\n" % event.request.RequestedSOPInstanceUID)
    ds = Dataset()
    ds.ExecutionStatus = "DONE"
    ds.ExecutionStatusInfo = "NORMAL"
    return 0x0000, ds

ae = AE(ae_title="PY_MEDIA")
ae.add_supported_context(MediaCreationManagement, ALL_TRANSFER_SYNTAXES)
server = ae.start_server(("127.0.0.1", 0), block=False, evt_handlers=[
    (evt.EVT_N_CREATE, on_create), (evt.EVT_N_ACTION, on_action), (evt.EVT_N_GET, on_get)])
print("PORT %d" % server.server_address[1], flush=True)
import time
time.sleep(120)  # idles until the test kills it
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	scu := network.NewSCU(network.SCUConfig{CallingAE: "GO_MEDIA", CalledAE: "PY_MEDIA", Address: addr})
	if err := scu.Associate(ctx, network.MediaCreationManagementPresentationContexts()); err != nil {
		t.Fatalf("associate for Media Creation Management: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	created, err := scu.NCreate(ctx, network.MediaCreationManagementUID, mediaRequestUID, mediaCreationRequest(t))
	if err != nil || created.Status != network.StatusSuccess {
		t.Fatalf("N-CREATE: status 0x%04X, %v", statusOf(created), err)
	}

	initiate := dataset.NewDataset()
	add(t, initiate, 0x2000, 0x0010, dataelem.IS, "2 ")
	add(t, initiate, 0x2200, 0x0020, dataelem.CS, "HIGH")
	// Action Type 1 is Initiate Media Creation (PS3.4 Table S.3-3).
	acted, err := scu.NAction(ctx, network.MediaCreationManagementUID, mediaRequestUID, 1, initiate)
	if err != nil || acted.Status != network.StatusSuccess {
		t.Fatalf("N-ACTION: status 0x%04X, %v", statusOf(acted), err)
	}

	got, err := scu.NGet(ctx, network.MediaCreationManagementUID, mediaRequestUID)
	if err != nil || got.Status != network.StatusSuccess {
		t.Fatalf("N-GET: status 0x%04X, %v", statusOf(got), err)
	}
	if status := stringValue(got.DataSet, tagExecutionStatus); status != "DONE" {
		t.Errorf("N-GET: this library read Execution Status %q from pynetdicom, want DONE", status)
	}

	want := []string{
		"CREATE " + mediaRequestUID + " GO MEDIA GOFILESET1 1 " + mediaReferencedUID,
		"ACTION 1 " + mediaRequestUID + " 2 HIGH",
		"GET " + mediaRequestUID,
	}
	for i, line := range collectLines(t, record, len(want)) {
		if strings.TrimSpace(line) != want[i] {
			t.Errorf("pynetdicom read message %d as %q, want %q", i+1, strings.TrimSpace(line), want[i])
		}
	}
}

// TestMediaCreationSCPAgainstPynetdicom is the same request with the roles
// reversed: pynetdicom asks and go-dicom serves. The SCP and SCU paths build
// their command sets separately, so each direction is its own check.
func TestMediaCreationSCPAgainstPynetdicom(t *testing.T) {
	status := dataset.NewDataset()
	add(t, status, 0x2100, 0x0020, dataelem.CS, "DONE")
	recorder := &nRecorder{events: make(chan nEvent, 8), getAnswer: status}
	addr := serveRecorder(t, recorder, network.MediaCreationManagementUID)

	runPynetdicomSCU(t, `
import sys
from pydicom.dataset import Dataset
from pynetdicom import AE
from pynetdicom.sop_class import MediaCreationManagement

ae = AE(ae_title="PY_MEDIA")
ae.add_requested_context(MediaCreationManagement)
assoc = ae.associate(`+quote(strings.Split(addr, ":")[0])+`, `+strings.Split(addr, ":")[1]+`, ae_title="GO_NSCP")
if not assoc.is_established:
    sys.exit("association rejected")

ds = Dataset()
ds.LabelText = "PY MEDIA"
ref = Dataset()
ref.ReferencedSOPClassUID = "`+network.CTImageStorageUID+`"
ref.ReferencedSOPInstanceUID = "`+mediaReferencedUID+`"
ds.ReferencedSOPSequence = [ref]
status, _ = assoc.send_n_create(ds, MediaCreationManagement, "`+mediaRequestUID+`")
assert status.Status == 0x0000, "N-CREATE 0x%04X" % status.Status

info = Dataset()
info.NumberOfCopies = 3
status, _ = assoc.send_n_action(info, 1, MediaCreationManagement, "`+mediaRequestUID+`")
assert status.Status == 0x0000, "N-ACTION 0x%04X" % status.Status

status, got = assoc.send_n_get([0x21000020], MediaCreationManagement, "`+mediaRequestUID+`")
assert status.Status == 0x0000, "N-GET 0x%04X" % status.Status
assert got.ExecutionStatus == "DONE", "pynetdicom read Execution Status %r" % got.ExecutionStatus
assoc.release()
`)

	want := []nEvent{
		{"N-CREATE", network.MediaCreationManagementUID, mediaRequestUID, 0, "PY MEDIA " + mediaReferencedUID},
		{"N-ACTION", network.MediaCreationManagementUID, mediaRequestUID, 1, "3"},
		// The detail is empty: pynetdicom asked for (2100,0020), and the SCP
		// does not give a handler the Attribute Identifier List (#380).
		{"N-GET", network.MediaCreationManagementUID, mediaRequestUID, 0, ""},
	}
	recorder.expect(t, want)
}

// eventLoggingClasses are the two Application Event Logging SOP classes, each
// addressed through its well-known instance (PS3.4 P.3), with the attribute
// the test sends that belongs to that class alone.
var eventLoggingClasses = []struct {
	name, class, instance string
}{
	{"Procedural Event Logging", network.ProceduralEventLoggingUID, network.ProceduralEventLoggingInstanceUID},
	{"Substance Administration Logging", network.SubstanceAdministrationLoggingUID, network.SubstanceAdministrationLoggingInstanceUID},
}

// eventRecord is an N-ACTION's information for one logging class: the patient
// and study, and for procedural logging a content item saying what happened,
// for substance administration the product given.
func eventRecord(t *testing.T, class string) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	add(t, ds, 0x0010, 0x0020, dataelem.LO, "GO-PAT-1")
	add(t, ds, 0x0020, 0x000D, dataelem.UI, eventStudyUID)
	if class == network.ProceduralEventLoggingUID {
		item := dataset.NewDataset()
		add(t, item, 0x0040, 0xA040, dataelem.CS, "TEXT")
		add(t, item, 0x0040, 0xA160, dataelem.UT, "Contrast injected")
		seq := sequence.New()
		if err := seq.Append(item); err != nil {
			t.Fatal(err)
		}
		if err := ds.AddSequence(tagContentSequence, seq); err != nil {
			t.Fatal(err)
		}
	} else {
		add(t, ds, 0x0044, 0x0008, dataelem.LO, "Iohexol 300")
	}
	return ds
}

// TestEventLoggingAgainstPynetdicom logs one event of each class with
// N-ACTION 1 to its well-known instance, as a modality or an injector does.
// pynetdicom records the class, the instance and the action type it read from
// the command, and the attributes it read from the data set.
func TestEventLoggingAgainstPynetdicom(t *testing.T) {
	record := t.TempDir() + "/events.txt"
	addr := startPynetdicomSCP(t, `
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import ProceduralEventLogging, SubstanceAdministrationLogging

record = open(`+quote(record)+`, "w", buffering=1)

def on_action(event):
    info = event.action_information
    if "ContentSequence" in info:
        detail = info.ContentSequence[0].TextValue
    else:
        detail = info.ProductName
    record.write("%s %s %d %s %s %s\n" % (event.request.RequestedSOPClassUID,
        event.request.RequestedSOPInstanceUID, event.action_type, info.PatientID,
        info.StudyInstanceUID, detail))
    return 0x0000, None

ae = AE(ae_title="PY_LOG")
ae.add_supported_context(ProceduralEventLogging, ALL_TRANSFER_SYNTAXES)
ae.add_supported_context(SubstanceAdministrationLogging, ALL_TRANSFER_SYNTAXES)
server = ae.start_server(("127.0.0.1", 0), block=False, evt_handlers=[(evt.EVT_N_ACTION, on_action)])
print("PORT %d" % server.server_address[1], flush=True)
import time
time.sleep(120)  # idles until the test kills it
`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	scu := network.NewSCU(network.SCUConfig{CallingAE: "GO_LOG", CalledAE: "PY_LOG", Address: addr})
	if err := scu.Associate(ctx, network.EventLoggingPresentationContexts()); err != nil {
		t.Fatalf("associate for event logging: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	var want []string
	for _, c := range eventLoggingClasses {
		// Action Type 1 records the event (PS3.4 P.3.1 and P.3.2).
		resp, err := scu.NAction(ctx, c.class, c.instance, 1, eventRecord(t, c.class))
		if err != nil || resp.Status != network.StatusSuccess {
			t.Fatalf("%s N-ACTION: status 0x%04X, %v", c.name, statusOf(resp), err)
		}
		detail := "Contrast injected"
		if c.class == network.SubstanceAdministrationLoggingUID {
			detail = "Iohexol 300"
		}
		want = append(want, strings.Join([]string{c.class, c.instance, "1", "GO-PAT-1", eventStudyUID, detail}, " "))
	}
	for i, line := range collectLines(t, record, len(want)) {
		if strings.TrimSpace(line) != want[i] {
			t.Errorf("pynetdicom read %s as %q, want %q", eventLoggingClasses[i].name, strings.TrimSpace(line), want[i])
		}
	}
}

// TestEventLoggingSCPAgainstPynetdicom: pynetdicom logs one event of each
// class to go-dicom, which records what it read.
func TestEventLoggingSCPAgainstPynetdicom(t *testing.T) {
	recorder := &nRecorder{events: make(chan nEvent, 8)}
	addr := serveRecorder(t, recorder, network.ProceduralEventLoggingUID, network.SubstanceAdministrationLoggingUID)

	runPynetdicomSCU(t, `
import sys
from pydicom.dataset import Dataset
from pynetdicom import AE
from pynetdicom.sop_class import (ProceduralEventLogging, SubstanceAdministrationLogging,
    ProceduralEventLoggingInstance, SubstanceAdministrationLoggingInstance)

ae = AE(ae_title="PY_LOG")
ae.add_requested_context(ProceduralEventLogging)
ae.add_requested_context(SubstanceAdministrationLogging)
assoc = ae.associate(`+quote(strings.Split(addr, ":")[0])+`, `+strings.Split(addr, ":")[1]+`, ae_title="GO_NSCP")
if not assoc.is_established:
    sys.exit("association rejected")

procedural = Dataset()
procedural.PatientID = "PY-PAT-1"
item = Dataset()
item.ValueType = "TEXT"
item.TextValue = "Catheter placed"
procedural.ContentSequence = [item]
status, _ = assoc.send_n_action(procedural, 1, ProceduralEventLogging, ProceduralEventLoggingInstance)
assert status.Status == 0x0000, "procedural N-ACTION 0x%04X" % status.Status

substance = Dataset()
substance.PatientID = "PY-PAT-1"
substance.ProductName = "Gadobutrol"
status, _ = assoc.send_n_action(substance, 1, SubstanceAdministrationLogging, SubstanceAdministrationLoggingInstance)
assert status.Status == 0x0000, "substance N-ACTION 0x%04X" % status.Status
assoc.release()
`)

	recorder.expect(t, []nEvent{
		{"N-ACTION", network.ProceduralEventLoggingUID, network.ProceduralEventLoggingInstanceUID, 1, "PY-PAT-1 Catheter placed"},
		{"N-ACTION", network.SubstanceAdministrationLoggingUID, network.SubstanceAdministrationLoggingInstanceUID, 1, "PY-PAT-1 Gadobutrol"},
	})
}

// nEvent is one N-DIMSE request as go-dicom's SCP read it: the message, the
// SOP class and instance it named, its action type, and what it carried.
type nEvent struct {
	kind, class, instance string
	actionType            uint16
	detail                string
}

// nRecorder serves N-CREATE, N-ACTION and N-GET by recording each request
// and answering success.
type nRecorder struct {
	network.BaseHandler
	events    chan nEvent
	getAnswer *dataset.Dataset // the N-GET answer, if any
}

func (h *nRecorder) HandleNCreate(_ context.Context, req *network.NCreateRequest) (*network.NCreateResponse, error) {
	detail := stringValue(req.DataSet, tagLabelText)
	if seq, err := req.DataSet.GetSequence(tagReferencedSOPSequence); err == nil && seq.Length() == 1 {
		if raw, err := seq.Get(0); err == nil {
			if item, ok := raw.(*dataset.Dataset); ok {
				detail += " " + stringValue(item, tagRefSOPInstanceUID)
			}
		}
	}
	h.events <- nEvent{"N-CREATE", req.AffectedSOPClass, req.AffectedSOPInstance, 0, detail}
	return &network.NCreateResponse{Status: network.StatusSuccess}, nil
}

func (h *nRecorder) HandleNAction(_ context.Context, req *network.NActionRequest) (*network.NActionResponse, error) {
	var detail string
	switch req.RequestedSOPClass {
	case network.MediaCreationManagementUID:
		detail = stringValue(req.DataSet, tagNumberOfCopies)
	case network.ProceduralEventLoggingUID:
		detail = stringValue(req.DataSet, tagPatientID)
		if seq, err := req.DataSet.GetSequence(tagContentSequence); err == nil && seq.Length() == 1 {
			if raw, err := seq.Get(0); err == nil {
				if item, ok := raw.(*dataset.Dataset); ok {
					detail += " " + stringValue(item, tagTextValue)
				}
			}
		}
	default:
		detail = stringValue(req.DataSet, tagPatientID) + " " + stringValue(req.DataSet, tagProductName)
	}
	h.events <- nEvent{"N-ACTION", req.RequestedSOPClass, req.RequestedSOPInstance, req.ActionTypeID, detail}
	return &network.NActionResponse{Status: network.StatusSuccess, ActionTypeID: req.ActionTypeID}, nil
}

func (h *nRecorder) HandleNGet(_ context.Context, req *network.NGetRequest) (*network.NGetResponse, error) {
	var asked []string
	for _, t := range req.AttributeList {
		asked = append(asked, t.String())
	}
	h.events <- nEvent{"N-GET", req.RequestedSOPClass, req.RequestedSOPInstance, 0, strings.Join(asked, ",")}
	resp := &network.NGetResponse{Status: network.StatusSuccess}
	resp.DataSet = h.getAnswer
	return resp, nil
}

// expect checks the requests arrived in order and as sent.
func (h *nRecorder) expect(t *testing.T, want []nEvent) {
	t.Helper()
	for _, w := range want {
		select {
		case got := <-h.events:
			got.detail = strings.TrimSpace(got.detail)
			if got != w {
				t.Errorf("go-dicom SCP read %+v, want %+v", got, w)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("go-dicom SCP never received %s for %s", w.kind, w.class)
		}
	}
}

// serveRecorder starts a go-dicom SCP for the given SOP classes, served by
// recorder, and returns its address.
func serveRecorder(t *testing.T, recorder *nRecorder, classes ...string) string {
	t.Helper()
	requirePynetdicom(t)
	scp := network.NewSCP(network.SCPConfig{AETitle: "GO_NSCP", Port: freePort(t), BindAddress: "127.0.0.1"})
	scp.SetHandler(recorder)
	scp.SetSupportedAbstractSyntaxes(classes)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return serveSCP(ctx, t, scp)
}

// stringValue is an element's value as text, without the padding PS3.5 6.2
// allows, or "" when the data set does not hold it.
func stringValue(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	value, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(value), " \x00")
}

// statusOf is a response's status, or 0xFFFF when there was no response.
func statusOf(resp interface{}) uint16 {
	switch r := resp.(type) {
	case *network.NCreateResponse:
		if r != nil {
			return r.Status
		}
	case *network.NActionResponse:
		if r != nil {
			return r.Status
		}
	case *network.NGetResponse:
		if r != nil {
			return r.Status
		}
	}
	return 0xFFFF
}
