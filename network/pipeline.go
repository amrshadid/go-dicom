package network

import (
	"context"
	"fmt"
	"sync"
)

// Message demultiplexing, so more than one DIMSE operation can be in flight on
// one association.
//
// Until this, the read side assumed the next message belonged to the request just
// sent, which is true only when one operation runs at a time. Operations on an SCU
// were therefore serialized, and the asynchronous operations window was reduced to
// one before being proposed because anything larger would have told the peer
// something untrue.
//
// # Why cooperative rather than a reader goroutine
//
// The obvious design is one goroutine owning the read side, dispatching into
// per-message-ID channels. It is not the one here, because four mechanisms already
// depend on the read path and a dedicated owner would have to take it from all of
// them: Association.pending queues messages read ahead by a cancel watcher,
// cancelWatcher holds the read side during a C-MOVE where nothing else is reading,
// a C-GET receives unsolicited C-STORE requests interleaved with its own responses,
// and an SCP reads the same association with no message IDs of its own to wait on.
//
// So the reading is cooperative: whichever caller needs a message does the reading,
// and anything that arrives for someone else is queued for them. One mutex is held
// while reading so two callers cannot interleave PDVs; the rest is bookkeeping. No
// goroutine owns the connection, which means nothing had to be taken away from the
// mechanisms above.
//
// # What a message is
//
// A DIMSE message is a command, optionally followed by a data set. Routing has to
// treat the pair as one unit: a command names the message ID, and the data set that
// follows carries no identification of its own. Reading them separately — which is
// what ReceivePData does, one PDV group at a time — and routing each independently
// would attach a data set to whichever operation happened to ask next.

// dimseMessage is one complete DIMSE message: its command, and the data set that
// followed if there was one.
type dimseMessage struct {
	contextID byte
	command   []byte
	dataSet   []byte

	// commandField says which service and whether this is a request or a
	// response; the RSP command fields all have 0x8000 set.
	commandField uint16

	// messageID is the message's own ID for a request, or the ID it responds to
	// for a response. ParseCommandDataset reads whichever is present.
	messageID uint16

	// hasDataSet records whether a data set followed, which is not the same as
	// dataSet being non-empty: a zero-length data set is legitimate.
	hasDataSet bool
}

// isResponse reports whether this message is a response rather than a request.
//
// PS3.7 9.3: every DIMSE-C and DIMSE-N response command field is its request's
// with the top bit set.
func (m *dimseMessage) isResponse() bool {
	return m.commandField&0x8000 != 0
}

// messageRouter holds messages that arrived for an operation other than the one
// doing the reading.
type messageRouter struct {
	mu sync.Mutex

	// byMessageID queues responses per operation. A C-FIND receives many
	// responses under one ID, so this is a queue rather than a single slot.
	byMessageID map[uint16][]*dimseMessage

	// unsolicited queues requests the peer originated — the C-STORE
	// sub-operations of a C-GET, which arrive while the C-GET's own responses do.
	unsolicited []*dimseMessage

	// failure is sticky. When the read fails the association is finished, and
	// every waiter has to learn rather than the one that happened to be reading.
	failure error

	// readMu serializes reading from the wire. Held only for the duration of one
	// message, so a caller waiting for a queued message does not block behind a
	// caller reading for someone else.
	readMu sync.Mutex
}

func newMessageRouter() *messageRouter {
	return &messageRouter{byMessageID: make(map[uint16][]*dimseMessage)}
}

// take removes and returns a queued response for messageID.
func (r *messageRouter) take(messageID uint16) (*dimseMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if queued := r.byMessageID[messageID]; len(queued) > 0 {
		r.byMessageID[messageID] = queued[1:]
		if len(r.byMessageID[messageID]) == 0 {
			delete(r.byMessageID, messageID)
		}
		return queued[0], nil
	}

	// The error is reported only once nothing is left to deliver: a response that
	// arrived before the association broke is still owed to its caller.
	return nil, r.failure
}

// takeUnsolicited removes and returns a queued request from the peer.
func (r *messageRouter) takeUnsolicited() (*dimseMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.unsolicited) > 0 {
		msg := r.unsolicited[0]
		r.unsolicited = r.unsolicited[1:]
		return msg, nil
	}
	return nil, r.failure
}

// route files a message for whoever is waiting on it.
func (r *messageRouter) route(msg *dimseMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg.isResponse() {
		r.byMessageID[msg.messageID] = append(r.byMessageID[msg.messageID], msg)
		return
	}
	r.unsolicited = append(r.unsolicited, msg)
}

// fail records that the association is finished.
func (r *messageRouter) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure == nil {
		r.failure = err
	}
}

// ReceiveResponse returns the next response to messageID, reading from the
// association if it has not arrived yet and queueing anything that arrives for
// another operation.
//
// Safe to call from several goroutines at once, each waiting on its own message ID.
func (a *Association) ReceiveResponse(ctx context.Context, messageID uint16) (*dimseMessage, error) {
	return a.receiveMatching(ctx, func(r *messageRouter) (*dimseMessage, error) {
		return r.take(messageID)
	})
}

// ReceiveRequest returns the next request the peer originated — a C-STORE
// sub-operation during a C-GET — reading if none has arrived.
func (a *Association) ReceiveRequest(ctx context.Context) (*dimseMessage, error) {
	return a.receiveMatching(ctx, func(r *messageRouter) (*dimseMessage, error) {
		return r.takeUnsolicited()
	})
}

// receiveMatching is the cooperative read loop shared by both.
func (a *Association) receiveMatching(ctx context.Context,
	take func(*messageRouter) (*dimseMessage, error)) (*dimseMessage, error) {

	router := a.router()

	for {
		if msg, err := take(router); msg != nil || err != nil {
			return msg, err
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Only one caller reads at a time, or two would interleave PDVs of
		// different messages into one another.
		router.readMu.Lock()

		// Re-check after acquiring the read lock: while waiting for it, the
		// caller that held it may have read and queued exactly what is wanted.
		if msg, err := take(router); msg != nil || err != nil {
			router.readMu.Unlock()
			return msg, err
		}

		msg, err := a.readMessage(ctx)
		if err != nil {
			// Sticky, so every other waiter learns rather than blocking forever on
			// an association that is gone.
			router.fail(err)
			router.readMu.Unlock()
			return nil, err
		}
		router.route(msg)
		router.readMu.Unlock()
	}
}

// readMessage reads one complete DIMSE message: its command, and the data set
// that follows when the command says one does.
func (a *Association) readMessage(ctx context.Context) (*dimseMessage, error) {
	contextID, commandBytes, isCommand, err := a.ReceivePData(ctx)
	if err != nil {
		return nil, err
	}
	if !isCommand {
		// A data set with no command before it. Nothing can be done with it: the
		// message it belongs to is unidentifiable, and guessing would attach it to
		// an operation that did not send it.
		return nil, NewDIMSEError("UNEXPECTED", "received a data set with no command before it", 0)
	}

	commandDS, err := DecodeCommandDataset(commandBytes)
	if err != nil {
		return nil, fmt.Errorf("decoding a command: %w", err)
	}

	commandField, messageID, _, err := ParseCommandDataset(commandDS)
	if err != nil {
		return nil, err
	}

	msg := &dimseMessage{
		contextID:    contextID,
		command:      commandBytes,
		commandField: commandField,
		messageID:    messageID,
	}

	dataSetType, err := getUSValue(commandDS, tagCommandDataSetType)
	if err != nil || dataSetType == CommandDataSetTypeNull {
		// No data set. A missing element is treated as none rather than as an
		// error: refusing the message would abandon an operation over an element
		// that only says whether more is coming.
		return msg, nil
	}

	_, dataBytes, isData, err := a.ReceivePData(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the data set of a message: %w", err)
	}
	if isData {
		return nil, NewDIMSEError("UNEXPECTED",
			"expected a data set after a command that said one follows, got another command", 0)
	}

	msg.dataSet = dataBytes
	msg.hasDataSet = true
	return msg, nil
}

// router returns the association's message router, creating it once.
func (a *Association) router() *messageRouter {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.messages == nil {
		a.messages = newMessageRouter()
	}
	return a.messages
}

// operationSlots bounds how many operations an SCU keeps in flight.
//
// The bound is the negotiated asynchronous operations window: proposing a window
// and then exceeding it would be as untrue as proposing one and never using it,
// which is what this replaced.
type operationSlots struct {
	slots chan struct{}
}

// newOperationSlots returns a bound of window operations, or nil for no bound.
//
// A window of zero means unlimited in PS3.7 D.3.3.3, and unlimited is what nil
// expresses here.
func newOperationSlots(window uint16) *operationSlots {
	if window == 0 {
		return nil
	}
	return &operationSlots{slots: make(chan struct{}, window)}
}

// acquire waits for a slot, or returns the context's error.
func (s *operationSlots) acquire(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release returns a slot.
func (s *operationSlots) release() {
	if s == nil {
		return
	}
	select {
	case <-s.slots:
	default:
		// Releasing more than were acquired would be a bug here rather than in the
		// caller, and blocking on it would hide that. It is dropped instead.
	}
}
