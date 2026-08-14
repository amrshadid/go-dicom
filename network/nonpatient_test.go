package network

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// The SOP classes CONFORMANCE.md §8.5 calls "reachable through the N-DIMSE
// primitives" — Relevant Patient Information Query, Display System, the
// non-patient object information models — had UID constants, presentation-context
// helpers, and nothing exercising them. §8.5 said so: "Nothing verifies them
// against a peer."
//
// Unverified is close to unknown in this codebase. The four N-DIMSE defects found
// in 1.4.0 were in exactly this kind of code: implemented, plausible, and never
// driven end to end, so an SCP read back the same wrong tag its SCU wrote.
//
// These drive each one over a real association. They are not a substitute for the
// interoperability tests — a peer that is not this library is the only real check —
// but they run on every push, and they establish that the dispatch reaches a
// handler with the SOP class the requestor named.

// serviceRecorder notes what reached it, so a test can assert the dispatch was
// correct rather than merely that something happened.
type serviceRecorder struct {
	BaseHandler

	mu sync.Mutex

	findSOPClasses []string
	findQueries    []*dataset.Dataset
	ngetSOPClasses []string
	ngetInstances  []string

	findResults []*dataset.Dataset
}

func (h *serviceRecorder) HandleCFind(_ context.Context, req *CFindRequest) ([]*CFindResponse, error) {
	h.mu.Lock()
	h.findSOPClasses = append(h.findSOPClasses, req.AffectedSOPClass)
	h.findQueries = append(h.findQueries, req.DataSet)
	results := h.findResults
	h.mu.Unlock()

	responses := make([]*CFindResponse, 0, len(results))
	for _, ds := range results {
		responses = append(responses, &CFindResponse{
			MessageIDRespondedTo: req.MessageID,
			AffectedSOPClass:     req.AffectedSOPClass,
			Status:               StatusPending,
			DataSet:              ds,
		})
	}
	return responses, nil
}

func (h *serviceRecorder) HandleNGet(_ context.Context, req *NGetRequest) (*NGetResponse, error) {
	h.mu.Lock()
	h.ngetSOPClasses = append(h.ngetSOPClasses, req.RequestedSOPClass)
	h.ngetInstances = append(h.ngetInstances, req.RequestedSOPInstance)
	h.mu.Unlock()

	// Answer with a plausible Display System attribute so the requestor has
	// something to read back.
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x1090), dataelem.LO, "GO-DICOM DISPLAY"))

	return &NGetResponse{
		MessageIDRespondedTo: req.MessageID,
		AffectedSOPClass:     req.RequestedSOPClass,
		AffectedSOPInstance:  req.RequestedSOPInstance,
		Status:               StatusSuccess,
		DataSet:              ds,
	}, nil
}

func (h *serviceRecorder) snapshot() ([]string, []*dataset.Dataset, []string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.findSOPClasses...),
		append([]*dataset.Dataset(nil), h.findQueries...),
		append([]string(nil), h.ngetSOPClasses...),
		append([]string(nil), h.ngetInstances...)
}

// A non-patient information model has **no** QueryRetrieveLevel. Its objects are
// not filed under a patient, so there is no hierarchy to descend and PS3.4 GG.2
// defines a single level. A C-FIND for one must therefore reach the handler
// without a level, and nothing in the dispatch may require one.
func TestNonPatientInformationModelsReachTheHandler(t *testing.T) {
	models := []struct {
		name     string
		sopClass string
		key      tag.Tag
		vr       dataelem.VR
	}{
		{"hanging protocol", HangingProtocolInformationModelFindUID,
			tag.New(0x0072, 0x0002), dataelem.SH}, // HangingProtocolName
		{"color palette", ColorPaletteInformationModelFindUID,
			tag.New(0x0070, 0x0080), dataelem.CS}, // ContentLabel
		{"generic implant template", GenericImplantTemplateInformationModelFindUID,
			tag.New(0x0068, 0x6210), dataelem.LO}, // ImplantName
	}

	for _, model := range models {
		t.Run(model.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			// A match to return, so the round trip carries a data set both ways.
			match := dataset.NewDataset()
			_ = match.Add(dataelem.NewDataElement(model.key, model.vr, "EXAMPLE"))

			handler := &serviceRecorder{findResults: []*dataset.Dataset{match}}

			server, err := StartServer(ctx, SCPConfig{
				AETitle: "NPSCP", Port: 0, BindAddress: "127.0.0.1",
			}, handler)
			if err != nil {
				t.Fatalf("StartServer: %v", err)
			}
			defer server.Stop()
			server.SetSupportedAbstractSyntaxes([]string{model.sopClass})

			scu := NewSCU(SCUConfig{
				CallingAE: "NPSCU", CalledAE: "NPSCP", Address: server.Addr(),
			})

			// NonPatientObjectPresentationContexts covers all of these models.
			if err := scu.Associate(ctx, NonPatientObjectPresentationContexts()); err != nil {
				t.Fatalf("Associate: %v", err)
			}
			defer func() { _ = scu.Release(ctx) }()

			// The query carries the model's own key and deliberately no
			// QueryRetrieveLevel.
			query := dataset.NewDataset()
			_ = query.Add(dataelem.NewDataElement(model.key, model.vr, ""))

			results, err := scu.FindWithSOPClass(ctx, model.sopClass, query)
			if err != nil {
				t.Fatalf("FindWithSOPClass: %v", err)
			}

			matches := 0
			for result := range results {
				if result.Err != nil {
					t.Fatalf("C-FIND: %v", result.Err)
				}
				if result.DataSet != nil {
					matches++
				}
			}
			if matches != 1 {
				t.Errorf("got %d matches, want 1", matches)
			}

			findClasses, queries, _, _ := handler.snapshot()
			if len(findClasses) != 1 {
				t.Fatalf("the handler saw %d C-FINDs, want 1", len(findClasses))
			}
			if findClasses[0] != model.sopClass {
				t.Errorf("the handler was given SOP class %q, want %q",
					findClasses[0], model.sopClass)
			}

			// The absence of a level must survive the round trip: a dispatch that
			// supplied a default would hide a handler bug rather than expose it.
			if queries[0] == nil {
				t.Fatal("the handler received no query data set")
			}
			if queries[0].Contains(tag.New(0x0008, 0x0052)) {
				t.Error("a QueryRetrieveLevel appeared in a non-patient query; " +
					"these models have a single level and send none")
			}
			if !queries[0].Contains(model.key) {
				t.Errorf("the model's own key %s did not reach the handler", model.key.String())
			}
		})
	}
}

// Relevant Patient Information Query is a C-FIND against one of three well-known
// SOP classes, each naming a different question. All three are dispatched.
func TestRelevantPatientInformationQueryReachesTheHandler(t *testing.T) {
	for _, sopClass := range []string{
		GeneralRelevantPatientInfoQueryUID,
		BreastImagingRelevantPatientInfoQueryUID,
		CardiacRelevantPatientInfoQueryUID,
	} {
		t.Run(sopClass, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			handler := &serviceRecorder{}

			server, err := StartServer(ctx, SCPConfig{
				AETitle: "RPISCP", Port: 0, BindAddress: "127.0.0.1",
			}, handler)
			if err != nil {
				t.Fatalf("StartServer: %v", err)
			}
			defer server.Stop()
			server.SetSupportedAbstractSyntaxes([]string{sopClass})

			scu := NewSCU(SCUConfig{
				CallingAE: "RPISCU", CalledAE: "RPISCP", Address: server.Addr(),
			})
			if err := scu.Associate(ctx, RelevantPatientInformationPresentationContexts()); err != nil {
				t.Fatalf("Associate: %v", err)
			}
			defer func() { _ = scu.Release(ctx) }()

			// PS3.4 Q.2.1.1: the identifier carries the patient and a template
			// identifier, not a query/retrieve level.
			query := dataset.NewDataset()
			_ = query.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, "P1"))
			_ = query.Add(dataelem.NewDataElement(tag.New(0x0040, 0xE001), dataelem.ST, "TEMPLATE"))

			results, err := scu.FindWithSOPClass(ctx, sopClass, query)
			if err != nil {
				t.Fatalf("FindWithSOPClass: %v", err)
			}
			for result := range results {
				if result.Err != nil {
					t.Fatalf("C-FIND: %v", result.Err)
				}
			}

			findClasses, queries, _, _ := handler.snapshot()
			if len(findClasses) != 1 || findClasses[0] != sopClass {
				t.Fatalf("the handler saw %v, want one C-FIND for %s", findClasses, sopClass)
			}
			if !queries[0].Contains(tag.New(0x0010, 0x0020)) {
				t.Error("the PatientID did not reach the handler")
			}
		})
	}
}

// Display System is an N-GET against a well-known instance, not a query. The
// instance UID is fixed by the standard, and naming it wrongly is the shape of
// defect that made every N-DIMSE service unusable in 1.3.0 — so this asserts the
// SCP is asked about the instance the requestor named.
func TestDisplaySystemNGetReachesTheHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	handler := &serviceRecorder{}

	server, err := StartServer(ctx, SCPConfig{
		AETitle: "DISPSCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{DisplaySystemUID})

	scu := NewSCU(SCUConfig{
		CallingAE: "DISPSCU", CalledAE: "DISPSCP", Address: server.Addr(),
	})
	if err := scu.Associate(ctx, DisplaySystemPresentationContexts()); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	response, err := scu.NGet(ctx, DisplaySystemUID, DisplaySystemInstanceUID)
	if err != nil {
		t.Fatalf("NGet: %v", err)
	}
	if response.Status != StatusSuccess {
		t.Errorf("N-GET status 0x%04X, want success", response.Status)
	}
	if response.DataSet == nil {
		t.Fatal("the N-GET response carried no data set")
	}
	if elem, ok := response.DataSet.Get(tag.New(0x0008, 0x1090)); !ok {
		t.Error("the response does not carry the attribute the handler returned")
	} else if got := trimPadding(elem.GetValue().([]byte)); got != "GO-DICOM DISPLAY" {
		t.Errorf("the response attribute reads %q", got)
	}

	_, _, ngetClasses, ngetInstances := handler.snapshot()
	if len(ngetClasses) != 1 {
		t.Fatalf("the handler saw %d N-GETs, want 1", len(ngetClasses))
	}
	if ngetClasses[0] != DisplaySystemUID {
		t.Errorf("the handler was given SOP class %q, want %q", ngetClasses[0], DisplaySystemUID)
	}

	// The well-known instance from PS3.4 EE.1.1. An N-GET that named the wrong
	// instance is precisely the 1.4.0 defect, in a different service.
	if ngetInstances[0] != DisplaySystemInstanceUID {
		t.Errorf("the handler was asked about instance %q, want the well-known %q",
			ngetInstances[0], DisplaySystemInstanceUID)
	}
}

// The presentation-context helpers are the documented way to reach these
// services, so each must actually propose the SOP classes it names. A helper that
// proposed nothing would leave every test above passing against contexts the
// caller supplied by hand.
func TestTheContextHelpersProposeWhatTheyName(t *testing.T) {
	cases := []struct {
		name     string
		contexts []PresentationContextItem
		want     []string
	}{
		{"non-patient object", NonPatientObjectPresentationContexts(), []string{
			HangingProtocolInformationModelFindUID,
			ColorPaletteInformationModelFindUID,
			GenericImplantTemplateInformationModelFindUID,
		}},
		{"relevant patient information", RelevantPatientInformationPresentationContexts(), []string{
			GeneralRelevantPatientInfoQueryUID,
			BreastImagingRelevantPatientInfoQueryUID,
			CardiacRelevantPatientInfoQueryUID,
		}},
		{"display system", DisplaySystemPresentationContexts(), []string{
			DisplaySystemUID,
		}},
		{"media creation management", MediaCreationManagementPresentationContexts(), []string{
			MediaCreationManagementUID,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proposed := make(map[string]bool, len(tc.contexts))
			for _, pc := range tc.contexts {
				proposed[pc.AbstractSyntax] = true

				// A context with no transfer syntax is refused by every peer, so a
				// helper that omitted them would be useless in a way no test of the
				// SOP class list would catch.
				if len(pc.TransferSyntaxes) == 0 {
					t.Errorf("the context for %s proposes no transfer syntax", pc.AbstractSyntax)
				}

				// Presentation context IDs must be odd (PS3.8 9.3.2.2).
				if pc.ID%2 == 0 {
					t.Errorf("presentation context ID %d is even; PS3.8 9.3.2.2 requires odd", pc.ID)
				}
			}

			for _, want := range tc.want {
				if !proposed[want] {
					t.Errorf("%s is not proposed", want)
				}
			}
		})
	}
}
