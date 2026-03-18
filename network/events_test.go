package network

import (
	"testing"
	"time"
)

func TestEventManager(t *testing.T) {
	em := NewEventManager()

	count := 0
	em.On(EVTAssocEstablished, func(e *Event) {
		count++
	})

	if !em.HasHandlers(EVTAssocEstablished) {
		t.Error("should have handlers")
	}
	if em.HasHandlers(EVTAssocReleased) {
		t.Error("should not have handlers for released")
	}

	em.Emit(&Event{Type: EVTAssocEstablished})
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	em.Emit(&Event{Type: EVTAssocEstablished})
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestEventManagerMultipleHandlers(t *testing.T) {
	em := NewEventManager()

	a, b := 0, 0
	em.On(EVTCStore, func(e *Event) { a++ })
	em.On(EVTCStore, func(e *Event) { b++ })

	em.Emit(&Event{Type: EVTCStore})
	if a != 1 || b != 1 {
		t.Errorf("expected a=1 b=1, got a=%d b=%d", a, b)
	}
}

func TestEventManagerOff(t *testing.T) {
	em := NewEventManager()

	count := 0
	em.On(EVTConnOpen, func(e *Event) { count++ })
	em.Off(EVTConnOpen)

	em.Emit(&Event{Type: EVTConnOpen})
	if count != 0 {
		t.Errorf("expected 0 after Off, got %d", count)
	}
}

func TestEventTimestamp(t *testing.T) {
	em := NewEventManager()

	var ts time.Time
	em.On(EVTConnOpen, func(e *Event) { ts = e.Timestamp })

	em.Emit(&Event{Type: EVTConnOpen})
	if ts.IsZero() {
		t.Error("timestamp should be set automatically")
	}
}

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		et       EventType
		expected string
	}{
		{EVTConnOpen, "EVT_CONN_OPEN"},
		{EVTConnClose, "EVT_CONN_CLOSE"},
		{EVTAssocEstablished, "EVT_ESTABLISHED"},
		{EVTAssocReleased, "EVT_RELEASED"},
		{EVTAssocAborted, "EVT_ABORTED"},
		{EVTPDURecv, "EVT_PDU_RECV"},
		{EVTDIMSERecv, "EVT_DIMSE_RECV"},
		{EVTCStore, "EVT_C_STORE"},
		{EVTNEventReport, "EVT_N_EVENT_REPORT"},
		{EVTNCreate, "EVT_N_CREATE"},
		{EVTAsyncOps, "EVT_ASYNC_OPS"},
		{EVTUserID, "EVT_USER_ID"},
		{EVTFSMTransition, "EVT_FSM_TRANSITION"},
		{EVTDataRecv, "EVT_DATA_RECV"},
		{EVTACSERecv, "EVT_ACSE_RECV"},
		{EventType(999), "EVT_UNKNOWN"},
	}

	for _, tt := range tests {
		result := EventTypeString(tt.et)
		if result != tt.expected {
			t.Errorf("EventTypeString(%d) = %q, want %q", tt.et, result, tt.expected)
		}
	}
}
