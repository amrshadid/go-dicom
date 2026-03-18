package network

import (
	"sync"
	"time"
)

// EventType identifies a network event.
type EventType int

// Notification events — informational, multiple handlers allowed.
// These match pynetdicom's EVT_* constants for full compatibility.
const (
	// Connection lifecycle
	EVTConnOpen  EventType = iota + 1 // Connection opened
	EVTConnClose                      // Connection closed

	// Association lifecycle
	EVTAssocRequested   // Association requested (SCU sent A-ASSOCIATE-RQ)
	EVTAssocAccepted    // Association accepted (SCP sent A-ASSOCIATE-AC)
	EVTAssocRejected    // Association rejected (SCP sent A-ASSOCIATE-RJ)
	EVTAssocEstablished // Association established (both sides)
	EVTAssocReleased    // Association released normally
	EVTAssocAborted     // Association aborted abnormally

	// ACSE (Association Control Service Element) primitives
	EVTACSERecv // ACSE primitive received from DUL
	EVTACSESent // ACSE primitive sent to DUL

	// PDU (Protocol Data Unit) level
	EVTPDURecv // PDU received and decoded
	EVTPDUSent // PDU encoded and sent

	// Raw data level
	EVTDataRecv // Raw PDU data received from remote
	EVTDataSent // Raw PDU data sent to remote

	// DIMSE (DICOM Message Service Element) level
	EVTDIMSERecv // Complete DIMSE message received and decoded
	EVTDIMSESent // DIMSE message encoded and sent to DUL

	// State machine
	EVTFSMTransition // DUL state machine about to transition

	// C-DIMSE service events (intervention)
	EVTCEcho  // C-ECHO request received
	EVTCStore // C-STORE request received
	EVTCFind  // C-FIND request received
	EVTCMove  // C-MOVE request received
	EVTCGet   // C-GET request received

	// N-DIMSE service events (intervention)
	EVTNEventReport // N-EVENT-REPORT request received
	EVTNGet         // N-GET request received
	EVTNSet         // N-SET request received
	EVTNAction      // N-ACTION request received
	EVTNCreate      // N-CREATE request received
	EVTNDelete      // N-DELETE request received

	// Negotiation events (intervention)
	EVTAsyncOps    // Asynchronous operations negotiation requested
	EVTSOPExtended // SOP class extended negotiation requested
	EVTSOPCommon   // SOP class common extended negotiation requested
	EVTUserID      // User identity negotiation requested
)

// Event carries information about a network event.
type Event struct {
	Type      EventType
	Timestamp time.Time

	// Association context (may be nil for connection-level events)
	CallingAE string
	CalledAE  string
	RemoteAddr string

	// PDU/DIMSE context (set for PDU/DIMSE events)
	PDUType     byte
	CommandType uint16

	// Additional context
	Description string
	Error       error
}

// EventHandler is a function that handles network events.
type EventHandler func(event *Event)

// EventManager manages event handlers. Thread-safe.
type EventManager struct {
	mu       sync.RWMutex
	handlers map[EventType][]EventHandler
}

// NewEventManager creates a new EventManager.
func NewEventManager() *EventManager {
	return &EventManager{
		handlers: make(map[EventType][]EventHandler),
	}
}

// On registers a handler for an event type. Multiple handlers per event are supported.
func (em *EventManager) On(eventType EventType, handler EventHandler) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.handlers[eventType] = append(em.handlers[eventType], handler)
}

// Off removes all handlers for an event type.
func (em *EventManager) Off(eventType EventType) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.handlers, eventType)
}

// Emit fires an event, calling all registered handlers.
func (em *EventManager) Emit(event *Event) {
	em.mu.RLock()
	handlers := em.handlers[event.Type]
	em.mu.RUnlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	for _, handler := range handlers {
		handler(event)
	}
}

// HasHandlers returns true if any handlers are registered for the event type.
func (em *EventManager) HasHandlers(eventType EventType) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return len(em.handlers[eventType]) > 0
}

// EventTypeString returns a human-readable name for an event type.
func EventTypeString(et EventType) string {
	switch et {
	case EVTConnOpen:
		return "EVT_CONN_OPEN"
	case EVTConnClose:
		return "EVT_CONN_CLOSE"
	case EVTAssocRequested:
		return "EVT_REQUESTED"
	case EVTAssocAccepted:
		return "EVT_ACCEPTED"
	case EVTAssocRejected:
		return "EVT_REJECTED"
	case EVTAssocEstablished:
		return "EVT_ESTABLISHED"
	case EVTAssocReleased:
		return "EVT_RELEASED"
	case EVTAssocAborted:
		return "EVT_ABORTED"
	case EVTACSERecv:
		return "EVT_ACSE_RECV"
	case EVTACSESent:
		return "EVT_ACSE_SENT"
	case EVTPDURecv:
		return "EVT_PDU_RECV"
	case EVTPDUSent:
		return "EVT_PDU_SENT"
	case EVTDataRecv:
		return "EVT_DATA_RECV"
	case EVTDataSent:
		return "EVT_DATA_SENT"
	case EVTDIMSERecv:
		return "EVT_DIMSE_RECV"
	case EVTDIMSESent:
		return "EVT_DIMSE_SENT"
	case EVTFSMTransition:
		return "EVT_FSM_TRANSITION"
	case EVTCEcho:
		return "EVT_C_ECHO"
	case EVTCStore:
		return "EVT_C_STORE"
	case EVTCFind:
		return "EVT_C_FIND"
	case EVTCMove:
		return "EVT_C_MOVE"
	case EVTCGet:
		return "EVT_C_GET"
	case EVTNEventReport:
		return "EVT_N_EVENT_REPORT"
	case EVTNGet:
		return "EVT_N_GET"
	case EVTNSet:
		return "EVT_N_SET"
	case EVTNAction:
		return "EVT_N_ACTION"
	case EVTNCreate:
		return "EVT_N_CREATE"
	case EVTNDelete:
		return "EVT_N_DELETE"
	case EVTAsyncOps:
		return "EVT_ASYNC_OPS"
	case EVTSOPExtended:
		return "EVT_SOP_EXTENDED"
	case EVTSOPCommon:
		return "EVT_SOP_COMMON"
	case EVTUserID:
		return "EVT_USER_ID"
	default:
		return "EVT_UNKNOWN"
	}
}
