package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filewriter"
)

// CStoreStreamer is implemented by a Handler that takes the data set of a
// C-STORE as the bytes the peer sent, rather than decoded.
//
// HandleCStore is given a decoded data set, which the SCP builds by holding
// the whole message in memory and then decoding it, so memory grows with the
// instance received, and a handler that writes it to a file writes this
// library's encoding of it rather than what the peer sent. A handler that
// implements CStoreStreamer is called instead of HandleCStore for every
// C-STORE, and HandleCStore is not called.
//
// HandleCStoreStream is given the command, with DataSet nil; the transfer
// syntax of the presentation context the message arrived on, in which the
// bytes are encoded; and data, the data set's bytes exactly as received,
// fragment after fragment. The bytes are read from the association as the
// handler reads them and no further ahead than one PDU, so memory stays
// bounded whatever the instance's size, and a handler that reads slowly slows
// the peer, through TCP, rather than growing a buffer. WriteCStoreFile writes
// them as a Part 10 file.
//
// Read returns io.EOF once the fragment the peer marked last has been read.
// Any other error means the data set did not arrive whole: the peer aborted
// or released partway, sent more than NetworkConfig.MaxDataSetSize, sent
// something that is not the rest of this data set, went quiet for longer
// than DIMSETimeout, or ctx was canceled. Then the association is already
// over, no response is sent whatever the handler returns, and whatever the
// handler made of the bytes is incomplete and must not be kept as the
// instance.
//
// The status returned is sent as the C-STORE-RSP once the whole data set has
// arrived. A handler that returns before reading to the end, whatever the
// status, has the rest read and discarded by the SCP, still bounded by
// MaxDataSetSize, and then its status sent: a DIMSE response follows its
// request, and the association goes on to the next message. An error, or a
// nil response, is sent as StatusUnableToProcess the same way.
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
	// err is sticky: io.EOF after the last fragment, or what ended the read.
	err error
	// failed records that the association, not the data set, ended the read,
	// so that no response can follow.
	failed bool
	// closed is set when the handler returns.
	closed bool
}

func newDataSetStream(ctx context.Context, assoc *Association, contextID byte, hasDataSet bool) *dataSetStream {
	assoc.mu.RLock()
	maxDataSet := assoc.maxDataSetSize
	assoc.mu.RUnlock()
	s := &dataSetStream{ctx: ctx, assoc: assoc, contextID: contextID, max: maxDataSet}
	// A command that says no data set follows has an empty one: the next PDV
	// on the association belongs to the next message.
	if !hasDataSet {
		s.last = true
	}
	return s
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
		if err != nil {
			s.err, s.failed = err, true
			return err
		}
		s.n += int64(len(data))
		// Counted as the bytes pass, as when the message is held whole.
		if s.max > 0 && s.n > s.max {
			s.err, s.failed = s.assoc.abortReceive(s.ctx, abortInvalidPDUParameter,
				fmt.Sprintf("data set over the %d bytes MaxDataSetSize allows", s.max)), true
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
// it arrives. See CStoreStreamer for what happens when.
func (s *SCP) handleCStoreStream(ctx context.Context, assoc *Association, streamer CStoreStreamer,
	ctxID byte, messageID uint16, cmdDS *dataset.Dataset) {

	sopClassUID, _ := GetAffectedSOPClassUID(cmdDS)
	sopInstanceUID, _ := GetAffectedSOPInstanceUID(cmdDS)
	priority, _ := getUSValue(cmdDS, tagPriority)
	req := &CStoreRequest{
		MessageID:           messageID,
		AffectedSOPClass:    sopClassUID,
		AffectedSOPInstance: sopInstanceUID,
		Priority:            priority,
	}

	stream := newDataSetStream(ctx, assoc, ctxID, HasDataSet(cmdDS))
	resp, err := streamer.HandleCStoreStream(ctx, req, assoc.TransferSyntaxFor(ctxID), stream)
	if !stream.finish() {
		// What ended the read already aborted the association, or the peer
		// did. A timeout or a canceled context has not: the association is
		// left partway through a message no response can follow, so it is
		// aborted here (PS3.8 9.1.5 for the timeout).
		DefaultLogger.Error("C-STORE of %s did not arrive whole: %v", sopInstanceUID, stream.err)
		if assoc.State() == StateAssociated {
			_ = assoc.Abort(ctx, AbortSourceServiceProvider, 0)
		}
		return
	}

	status := StatusUnableToProcess
	if err == nil && resp != nil {
		status = resp.Status
	} else if err != nil {
		DefaultLogger.Error("C-STORE handler failed for %s: %v", sopInstanceUID, err)
	}
	rspBytes, encErr := EncodeCommandDataset(BuildCStoreRSP(messageID, sopClassUID, sopInstanceUID, status))
	if encErr != nil {
		DefaultLogger.Error("failed to encode C-STORE-RSP: %v", encErr)
		return
	}
	_ = assoc.SendPData(ctx, ctxID, rspBytes, true)
}

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
// (0002,0017) and (0002,0018). Nothing in the data set is read or changed,
// so a checksum of the file's data set is a checksum of what the peer sent.
//
// An error from data is returned as it is. Then the file holds part of a
// data set and is not the instance: delete it, or keep it under a name
// nothing reads as whole.
func WriteCStoreFile(ctx context.Context, w io.Writer, req *CStoreRequest, transferSyntax string,
	data io.Reader) (int64, error) {

	if req == nil {
		return 0, errors.New("network: WriteCStoreFile needs the C-STORE request")
	}
	if transferSyntax == "" {
		// A context that was never accepted has none, and its data set is
		// read as the default (PS3.5 10.1).
		transferSyntax = ImplicitVRLittleEndianUID
	}
	meta := &filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    req.AffectedSOPClass,
		MediaStorageSOPInstanceUID: req.AffectedSOPInstance,
		TransferSyntaxUID:          transferSyntax,
	}
	if info := AssociationInfoFromContext(ctx); info != nil {
		meta.SendingApplicationEntityTitle = info.CallingAE
		meta.ReceivingApplicationEntityTitle = info.CalledAE
	}
	header, err := filewriter.EncodeFileHeader(meta)
	if err != nil {
		return 0, fmt.Errorf("network: build the file meta information: %w", err)
	}
	if _, err := w.Write(header); err != nil {
		return 0, err
	}
	return io.Copy(w, data)
}
