package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/valuerep"
)

// CStoreStreamer is implemented by a Handler that takes the data set of a
// C-STORE as the bytes the peer sent, rather than decoded.
//
// HandleCStore is given a decoded data set, which the SCP builds by holding
// the whole message in memory and then decoding it, so memory grows with the
// instance received, and a handler that writes it to a file writes this
// library's encoding of it rather than what the peer sent. A handler that
// implements CStoreStreamer is called instead of HandleCStore for every
// C-STORE, and HandleCStore is not called. CompositeHandler passes it on
// when its store handler implements it.
//
// HandleCStoreStream is given the command, with DataSet nil; the transfer
// syntax of the presentation context the message arrived on, in which the
// bytes are encoded; and data, the data set's bytes exactly as received,
// fragment after fragment. The bytes are read from the association as the
// handler reads them: what is held at any moment is the P-DATA-TF last read
// and the fragment being handed out of it, two PDUs at most, each bounded by
// the Maximum Length the SCP advertised. So memory stays bounded whatever the
// instance's size, and a handler that reads slowly slows the peer, through
// TCP, rather than growing a buffer. WriteCStoreFile writes the bytes as a
// Part 10 file.
//
// Read returns io.EOF once, and only once, the fragment the peer marked last
// has been read. Any other end is an error for which errors.Is(err,
// io.ErrUnexpectedEOF) is true and errors.Is(err, io.EOF) is false, whatever
// caused it, with the cause in its text: the peer aborted, released or closed
// the connection partway, sent more than NetworkConfig.MaxDataSetSize, sent
// something that is not the rest of this data set, went quiet for longer than
// DIMSETimeout, or ctx was canceled. Then the association is already over, no
// response is sent whatever the handler returns, and whatever the handler
// made of the bytes is incomplete and must not be kept as the instance.
//
// The status returned is sent as the C-STORE-RSP once the whole data set has
// arrived. A handler that returns before reading to the end, whatever the
// status, has the rest read and discarded by the SCP, still bounded by
// MaxDataSetSize, and then its status sent: a DIMSE response follows its
// request, and the association goes on to the next message. An error, or a
// nil response, is sent as StatusUnableToProcess the same way. With
// MaxDataSetSize at zero, a handler that refuses early still has the SCP
// receive and discard the whole data set, however large the peer makes it:
// set MaxDataSetSize on an SCP that refuses.
//
// The SCP has no timeout of its own on the handler. DIMSETimeout bounds each
// PDU the peer sends, and while the handler does not read, nothing is read,
// so a handler that blocks without reading holds the association for as long
// as it blocks. A handler that may wait, on a disk or a database, waits on
// ctx too, and returns when it is done.
//
// A C-STORE-RQ that says no data set follows is refused with
// StatusUnableToProcess, and the handler is not called: a C-STORE-RQ carries a
// data set (PS3.7 9.3.1.1, Command Data Set Type "shall be set to any value
// other than 0101H"), and there is nothing to store.
//
// data is valid only until HandleCStoreStream returns; a Read after that
// fails. Nothing else is read from the association while the data set is
// being read: an SCP takes one message at a time, and the fragments of a
// message are not interleaved with another's (PS3.8 E.2).
type CStoreStreamer interface {
	HandleCStoreStream(ctx context.Context, req *CStoreRequest, transferSyntax string,
		data io.Reader) (*CStoreResponse, error)
}

// errStreamClosed is what a read of a data set stream returns after the
// handler it was given to has returned.
var errStreamClosed = errors.New("network: the C-STORE data set was read after its handler returned")

// incompleteDataSetError is how a data set stream reports any end other than
// the last fragment.
//
// It unwraps to io.ErrUnexpectedEOF and to nothing else. What ended the read
// can itself wrap io.EOF, as a connection the peer closed does, and a handler
// that tests errors.Is(err, io.EOF) to find the end of the data set would take
// that for a whole one and keep a truncated file as the instance. The cause is
// kept for the error's text, not for its chain.
type incompleteDataSetError struct{ cause error }

func (e *incompleteDataSetError) Error() string {
	return "network: the C-STORE data set did not arrive whole: " + e.cause.Error()
}

func (e *incompleteDataSetError) Unwrap() error { return io.ErrUnexpectedEOF }

// dataSetStream reads the data set of one message from the association,
// fragment by fragment, as its reader asks for bytes.
type dataSetStream struct {
	mu        sync.Mutex
	ctx       context.Context
	assoc     *Association
	contextID byte
	max       int64

	// cur is what is left of the fragment being read, and last whether that
	// fragment was marked the last of the data set.
	cur  []byte
	last bool
	// n counts the data set's bytes read from the association so far.
	n int64
	// err is sticky: io.EOF after the last fragment, or an
	// incompleteDataSetError saying what ended the read.
	err error
	// failed records that the association, not the data set, ended the read,
	// so that no response can follow.
	failed bool
	// closed is set when the handler returns.
	closed bool
}

func newDataSetStream(ctx context.Context, assoc *Association, contextID byte) *dataSetStream {
	assoc.mu.RLock()
	maxDataSet := assoc.maxDataSetSize
	assoc.mu.RUnlock()
	return &dataSetStream{ctx: ctx, assoc: assoc, contextID: contextID, max: maxDataSet}
}

// Read reads the data set's bytes as the peer sent them.
func (s *dataSetStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errStreamClosed
	}
	if err := s.fill(); err != nil {
		return 0, err
	}
	n := copy(p, s.cur)
	s.cur = s.cur[n:]
	return n, nil
}

// fill makes cur hold unread bytes, reading the next fragment when it is
// empty, or returns why there are none. Called with mu held.
func (s *dataSetStream) fill() error {
	for len(s.cur) == 0 {
		if s.err != nil {
			return s.err
		}
		if s.last {
			s.err = io.EOF
			return s.err
		}
		data, last, err := s.nextFragment()
		if err == nil {
			s.n += int64(len(data))
			// Counted as the bytes pass, as when the message is held whole.
			if s.max > 0 && s.n > s.max {
				err = s.assoc.abortReceive(s.ctx, abortInvalidPDUParameter,
					fmt.Sprintf("data set over the %d bytes MaxDataSetSize allows", s.max))
			}
		}
		if err != nil {
			s.err, s.failed = &incompleteDataSetError{cause: err}, true
			return s.err
		}
		s.cur, s.last = data, last
	}
	return nil
}

// nextFragment returns the bytes of the data set's next fragment and whether
// it is the last.
func (s *dataSetStream) nextFragment() ([]byte, bool, error) {
	// A cancel watcher may have read the data set ahead, whole, while it
	// looked for a C-CANCEL; it is queued for whoever reads next.
	if msg, ok := s.assoc.popPending(); ok {
		if msg.isCommand || msg.contextID != s.contextID {
			return nil, false, s.assoc.abortReceive(s.ctx, abortUnexpectedPDUParameter,
				"a command arrived where the C-STORE's data set was due")
		}
		return msg.data, true, nil
	}
	pdv, err := s.assoc.readFragment(s.ctx)
	if err != nil {
		return nil, false, err
	}
	// The data set follows its command on the same presentation context, and
	// nothing else comes between them (PS3.8 E.2): its transfer syntax is the
	// one the handler was told. A command here, or data on another context,
	// is not part of this data set, and reading it as such would hand the
	// handler bytes encoded some other way.
	if pdv.IsCommand {
		return nil, false, s.assoc.abortReceive(s.ctx, abortUnexpectedPDUParameter,
			"a command fragment arrived partway through a C-STORE data set")
	}
	if pdv.PresentationContextID != s.contextID {
		return nil, false, s.assoc.abortReceive(s.ctx, abortUnexpectedPDUParameter,
			fmt.Sprintf("a data set fragment on presentation context %d, for a C-STORE on context %d",
				pdv.PresentationContextID, s.contextID))
	}
	return pdv.Data, pdv.IsLast, nil
}

// finish closes the stream to its handler and reads what the handler left of
// the data set, discarding it. It reports whether the association can carry
// a response: false when the data set did not arrive whole.
func (s *dataSetStream) finish() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for {
		s.cur = nil
		if err := s.fill(); err != nil {
			return err == io.EOF
		}
	}
}

// popPending takes the oldest message read ahead of its recipient, if any.
func (a *Association) popPending() (pendingMessage, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) == 0 {
		return pendingMessage{}, false
	}
	msg := a.pending[0]
	a.pending = a.pending[1:]
	return msg, true
}

// handleCStoreStream serves a C-STORE to a handler that reads its data set as
// it arrives. See CStoreStreamer for what happens when. handleCStore has
// already refused a request that says no data set follows, for both kinds of
// handler.
func (s *SCP) handleCStoreStream(ctx context.Context, assoc *Association, streamer CStoreStreamer,
	ctxID byte, req *CStoreRequest) {

	stream := newDataSetStream(ctx, assoc, ctxID)
	resp, err := streamer.HandleCStoreStream(ctx, req, assoc.TransferSyntaxFor(ctxID), stream)
	if !stream.finish() {
		// What ended the read already aborted the association, or the peer
		// did. A timeout or a canceled context has not: the association is
		// left partway through a message no response can follow, so it is
		// aborted here (PS3.8 9.1.5 for the timeout).
		s.log().Error("C-STORE of %s: %v", req.AffectedSOPInstance, stream.err)
		if assoc.State() == StateAssociated {
			_ = assoc.abortWhy(ctx, AbortSourceServiceProvider, 0, streamAbort(stream.err))
		}
		return
	}

	status := StatusUnableToProcess
	if err == nil && resp != nil {
		status = resp.Status
	} else if err != nil {
		s.log().Error("C-STORE handler failed for %s: %v", req.AffectedSOPInstance, err)
	}
	s.sendCStoreRSP(ctx, assoc, ctxID, req, status)
}

// sendCStoreRSP answers a C-STORE-RQ with status.
func (s *SCP) sendCStoreRSP(ctx context.Context, assoc *Association, ctxID byte, req *CStoreRequest, status uint16) {
	rspBytes, err := EncodeCommandDataset(BuildCStoreRSP(req.MessageID, req.AffectedSOPClass,
		req.AffectedSOPInstance, status))
	if err != nil {
		s.log().Error("failed to encode C-STORE-RSP: %v", err)
		return
	}
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

// cstoreRequest reads a C-STORE-RQ's command set into a CStoreRequest, with
// no data set: the fields both ways of handling a store are given.
func cstoreRequest(messageID uint16, cmdDS *dataset.Dataset) *CStoreRequest {
	req := &CStoreRequest{MessageID: messageID}
	req.AffectedSOPClass, _ = GetAffectedSOPClassUID(cmdDS)
	req.AffectedSOPInstance, _ = GetAffectedSOPInstanceUID(cmdDS)
	req.Priority, _ = getUSValue(cmdDS, tagPriority)
	// Present when the store is a sub-operation of a C-MOVE (PS3.7 9.3.1.1).
	if ae, err := getUIValue(cmdDS, tagMoveOriginatorAE); err == nil {
		req.MoveOriginatorAE = trimAE(ae)
	}
	req.MoveOriginatorMsgID, _ = getUSValue(cmdDS, tagMoveOriginatorMessageID)
	return req
}

// streamsCStore reports whether a handler takes C-STOREs as a stream: it
// implements CStoreStreamer, and, if it says whether it streams, as a handler
// wrapping another does, says yes.
func streamsCStore(handler Handler) (CStoreStreamer, bool) {
	streamer, ok := handler.(CStoreStreamer)
	if !ok {
		return nil, false
	}
	if chooser, ok := handler.(interface{ streamsCStore() bool }); ok && !chooser.streamsCStore() {
		return nil, false
	}
	return streamer, true
}

// ErrInvalidSOPInstanceUID is returned by WriteCStoreFile, wrapped, for a
// command whose Affected SOP Instance UID is not a valid UID. PS3.7 9.1.1.1.9
// gives C-STORE a status for it: StatusInvalidObjectInstance, 0117H, "the SOP
// Instance UID specified implied a violation of the UID construction rules".
var ErrInvalidSOPInstanceUID = errors.New("network: the C-STORE names an invalid SOP Instance UID")

// ErrInvalidStoreCommand is returned by WriteCStoreFile, wrapped, when the
// command's Affected SOP Class UID or the transfer syntax is not a valid UID,
// or a value does not fit the File Meta Information. PS3.4 B.2.3 calls a
// store the SCP cannot read "Error: Cannot understand", Cxxx;
// StatusUnableToProcess is 0xC000.
var ErrInvalidStoreCommand = errors.New("network: the C-STORE cannot be written as a file")

// WriteCStoreFile writes a data set a CStoreStreamer was given as a DICOM
// Part 10 file: the preamble, "DICM" and File Meta Information built from
// the command (PS3.10 7.1), then the bytes read from data, untouched. It
// returns the number of data set bytes written.
//
// The File Meta Information names req's SOP Class and SOP Instance as the
// Media Storage SOP Class and Instance UIDs, transferSyntax as the Transfer
// Syntax UID, and this library as the implementation. When ctx carries the
// association's information, as a handler's does, the calling and called AE
// titles are recorded as the Sending and Receiving Application Entity Titles
// (0002,0017) and (0002,0018), each only when it is a well-formed AE title
// (PS3.5 6.2). Nothing in the data set is read or changed, so a checksum of
// the file's data set is a checksum of what the peer sent.
//
// The UIDs come from the peer, and are checked before anything is written:
// each must be a valid UID (PS3.5 9.1: digits and dots, components without
// leading zeros, at most 64 characters). A command that fails is refused
// with an error wrapping ErrInvalidSOPInstanceUID or ErrInvalidStoreCommand,
// and nothing is written to w; the data set is left unread, for the SCP to
// discard.
//
// PS3.10 7.1 requires the Media Storage SOP Class and Instance UIDs to equal
// the data set's own SOP Class UID (0008,0016) and SOP Instance UID
// (0008,0018). They are not compared here: the data set is copied, not
// parsed, which is what keeps it as sent. A peer can send a command that
// disagrees with its data set, and a caller that parses the file, as an
// archive indexing it does, is where that is found.
//
// A read error from data, or a write error, is returned. Then the file holds
// part of a data set and is not the instance: delete it, or keep it under a
// name nothing reads as whole. No error returned is one errors.Is matches
// with io.EOF.
func WriteCStoreFile(ctx context.Context, w io.Writer, req *CStoreRequest, transferSyntax string,
	data io.Reader) (int64, error) {

	if req == nil {
		return 0, fmt.Errorf("%w: no request", ErrInvalidStoreCommand)
	}
	if transferSyntax == "" {
		// A context that was never accepted has none, and its data set is
		// read as the default (PS3.5 10.1).
		transferSyntax = ImplicitVRLittleEndianUID
	}
	if err := valuerep.ValidateUID(req.AffectedSOPInstance); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidSOPInstanceUID, shortened(err))
	}
	if err := valuerep.ValidateUID(req.AffectedSOPClass); err != nil {
		return 0, fmt.Errorf("%w: SOP Class: %v", ErrInvalidStoreCommand, shortened(err))
	}
	if err := valuerep.ValidateUID(transferSyntax); err != nil {
		return 0, fmt.Errorf("%w: transfer syntax: %v", ErrInvalidStoreCommand, shortened(err))
	}
	meta := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    req.AffectedSOPClass,
		MediaStorageSOPInstanceUID: req.AffectedSOPInstance,
		TransferSyntaxUID:          transferSyntax,
	}
	if info := AssociationInfoFromContext(ctx); info != nil {
		if ae, ok := cleanAETitle(info.CallingAE); ok {
			meta.SendingApplicationEntityTitle = ae
		}
		if ae, ok := cleanAETitle(info.CalledAE); ok {
			meta.ReceivingApplicationEntityTitle = ae
		}
	}
	header, err := filewriter.EncodeFileHeader(meta)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidStoreCommand, err)
	}
	if _, err := w.Write(header); err != nil {
		return 0, notEOF(err)
	}
	n, err := io.Copy(w, data)
	return n, notEOF(err)
}

// notEOF returns err, unless it is one errors.Is matches with io.EOF: a
// writer may return one, and the caller of WriteCStoreFile must never take a
// failure for the end of a data set. That is returned as io.ErrUnexpectedEOF
// with its text.
func notEOF(err error) error {
	if err != nil && errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", io.ErrUnexpectedEOF, err)
	}
	return err
}

// shortened is an error's text cut to a length fit for a log line: a UID
// from a peer can be any length.
func shortened(err error) string {
	const limit = 120
	if text := err.Error(); len(text) > limit {
		return text[:limit] + "..."
	}
	return err.Error()
}

// streamAbort is why a streamed C-STORE that did not arrive whole is
// aborted: its peer went quiet, or the server's context ended, which is the
// server stopping. Anything else that ends the read aborts the association
// where it happens.
func streamAbort(err error) *LocalAbort {
	cause := err
	if incomplete, ok := err.(*incompleteDataSetError); ok {
		cause = incomplete.cause
	}
	var timeout *TimeoutError
	if errors.As(cause, &timeout) {
		return abortedBy(AbortCauseTimeout, cause)
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return abortedBy(AbortCauseStopping, cause)
	}
	return abortedBy(AbortCauseProtocolViolation, cause)
}
