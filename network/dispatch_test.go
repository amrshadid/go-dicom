package network

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingHandler notes which N-DIMSE handler methods the SCP reached.
type recordingHandler struct {
	BaseHandler
	mu      sync.Mutex
	reached map[string]bool
}

func newRecordingHandler() *recordingHandler {
	return &recordingHandler{reached: map[string]bool{}}
}

func (h *recordingHandler) note(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reached[name] = true
}

func (h *recordingHandler) sawAll() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.reached))
	for k, v := range h.reached {
		out[k] = v
	}
	return out
}

func (h *recordingHandler) HandleNGet(_ context.Context, _ *NGetRequest) (*NGetResponse, error) {
	h.note("N-GET")
	return &NGetResponse{Status: StatusSuccess}, nil
}

func (h *recordingHandler) HandleNSet(_ context.Context, _ *NSetRequest) (*NSetResponse, error) {
	h.note("N-SET")
	return &NSetResponse{Status: StatusSuccess}, nil
}

func (h *recordingHandler) HandleNAction(_ context.Context, _ *NActionRequest) (*NActionResponse, error) {
	h.note("N-ACTION")
	return &NActionResponse{Status: StatusSuccess}, nil
}

func (h *recordingHandler) HandleNCreate(_ context.Context, _ *NCreateRequest) (*NCreateResponse, error) {
	h.note("N-CREATE")
	return &NCreateResponse{Status: StatusSuccess}, nil
}

func (h *recordingHandler) HandleNDelete(_ context.Context, _ *NDeleteRequest) (*NDeleteResponse, error) {
	h.note("N-DELETE")
	return &NDeleteResponse{Status: StatusSuccess}, nil
}

// TestEveryNDIMSECommandIsDispatched sends each N-DIMSE request and requires it
// to reach its handler.
//
// The existing N-DIMSE tests only build and parse command data sets; none of
// them exercise the SCP's command switch. A case deleted from that switch
// therefore passed the entire suite — the request fell through to `default:`,
// which aborts the association, and nothing noticed. That happened while adding
// C-CANCEL: the N-DELETE case was lost and only the unused-function linter
// caught it.
//
// Asserting the handler was reached, rather than that no error came back, is
// what makes this test able to fail: an abort surfaces as a transport error on
// the next operation, which is easy to mistake for an unrelated flake.
func TestEveryNDIMSECommandIsDispatched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// MPPS is the natural carrier for N-DIMSE, and it is not in the default
	// contexts, so both ends are told about it explicitly.
	const (
		sopClass    = ModalityPerformedProcedureStepUID
		sopInstance = "1.2.826.0.1.3680043.8.498.10101"
	)

	handler := newRecordingHandler()
	server, err := StartServer(ctx, SCPConfig{
		AETitle: "NDIMSE_SCP", Port: 0, BindAddress: "127.0.0.1",
	}, handler)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer server.Stop()
	server.SetSupportedAbstractSyntaxes([]string{VerificationSOPClassUID, sopClass})

	scu := NewSCU(SCUConfig{
		CallingAE: "NDIMSE_SCU", CalledAE: "NDIMSE_SCP", Address: server.Addr(),
	})
	err = scu.Associate(ctx, []PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   sopClass,
		TransferSyntaxes: DefaultTransferSyntaxes(),
	}})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	calls := []struct {
		name string
		send func() error
	}{
		{"N-GET", func() error { _, err := scu.NGet(ctx, sopClass, sopInstance); return err }},
		{"N-SET", func() error { _, err := scu.NSet(ctx, sopClass, sopInstance, nil); return err }},
		{"N-ACTION", func() error { _, err := scu.NAction(ctx, sopClass, sopInstance, 1, nil); return err }},
		{"N-CREATE", func() error { _, err := scu.NCreate(ctx, sopClass, sopInstance, nil); return err }},
		{"N-DELETE", func() error { _, err := scu.NDelete(ctx, sopClass, sopInstance); return err }},
	}

	for _, c := range calls {
		if err := c.send(); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}

	reached := handler.sawAll()
	for _, c := range calls {
		if !reached[c.name] {
			t.Errorf("%s never reached its handler — the SCP command switch has no case for it, "+
				"so it fell through to the abort branch", c.name)
		}
	}
}
