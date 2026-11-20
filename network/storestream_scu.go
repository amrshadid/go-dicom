package network

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// maxStreamFragment bounds the data one P-DATA-TF of a streamed send carries,
// whatever Maximum Length the peer advertised. A peer that offers a PDU of
// gigabytes is allowed to, and sending in smaller ones is within its limit;
// two buffers that size are all a streamed send holds.
const maxStreamFragment = 1 << 20

// StoreStreamRequest is one C-STORE for [SCU.StoreStream]: the instance's
// identity, the transfer syntax its data set is encoded in, and the data set
// itself as a stream.
type StoreStreamRequest struct {
	// SOPClassUID and SOPInstanceUID are the Affected SOP Class UID and
	// Affected SOP Instance UID of the C-STORE-RQ.
	SOPClassUID, SOPInstanceUID string

	// TransferSyntaxUID is the transfer syntax DataSet is encoded in. Only an
	// accepted presentation context for SOPClassUID with exactly this transfer
	// syntax is used; nothing is transcoded.
	TransferSyntaxUID string

	// DataSet is the encoded data set: the bytes that follow the File Meta
	// Information of a Part 10 file, with no preamble and no meta group, in
	// TransferSyntaxUID. It is read once, to its end.
	DataSet io.Reader
}

// StoreStream sends one instance with C-STORE as the bytes it is held in,
// reading them from req.DataSet as they are sent.
//
// [SCU.Store] holds a whole data set in memory, encoded by this library, and
// may transcode it to a syntax the peer accepted. A program that keeps
// instances as files and must send what it stored, byte for byte, has no need
// of that. StoreStream uses the presentation context whose abstract syntax is
// the SOP class and whose transfer syntax is req.TransferSyntaxUID, and no
// other: when the peer accepted none, the error names both, and the caller can
// associate again proposing it. Nothing is ever transcoded.
//
// The command is sent, then the data set in P-DATA-TF fragments as large as
// the peer's Maximum Length allows (at most 1 MiB), each read from the reader
// just before it is sent. What is held at any moment is two fragments, so
// memory does not grow with the size of the instance.
//
// The returned status is the peer's, for success (0x0000), warning (0xB000,
// 0xB006, 0xB007) and failure alike: a peer that refused the instance has
// answered, and has not failed as a transport, so the caller decides what each
// status means. The error is non-nil only when no status was obtained: there
// was no usable presentation context, the association is not up, the
// connection or the protocol failed, ctx ended, or the reader failed.
//
// A reader that fails before its end never completes the data set on the
// wire. A fragment is sent as the last only once the reader has reported
// io.EOF, so when it returns any other error StoreStream sends an A-ABORT
// instead (service-user, reason not specified), returns the reader's error,
// and the peer is never given a data set that was in fact cut short. The
// association is over after that.
//
// Like Store, StoreStream is safe to use from several goroutines, and one
// message is on the wire at a time: it holds the association's send lock for
// as long as the data set takes to read and send.
func (s *SCU) StoreStream(ctx context.Context, req StoreStreamRequest) (status uint16, err error) {
	if req.DataSet == nil {
		return 0, fmt.Errorf("network: StoreStream was given no data set to send")
	}
	release, err := s.beginOperation(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	s.mu.Lock()
	assoc := s.association
	s.mu.Unlock()

	if assoc == nil || assoc.State() != StateAssociated {
		return 0, NewAssociationError("NOT_ASSOCIATED", "not associated")
	}

	pcID, ok := exactStoreContext(assoc.AcceptedContexts(), req.SOPClassUID, req.TransferSyntaxUID)
	if !ok {
		return 0, NewAssociationError("NO_CONTEXT",
			fmt.Sprintf("no accepted presentation context for SOP Class %s with transfer syntax %s: %s",
				req.SOPClassUID, req.TransferSyntaxUID, assoc.ExplainNoContextFor(req.SOPClassUID)))
	}

	messageID := s.nextMessageID()
	cmdBytes, err := EncodeCommandDataset(
		BuildCStoreRQ(messageID, req.SOPClassUID, req.SOPInstanceUID, PriorityMedium))
	if err != nil {
		return 0, fmt.Errorf("failed to encode C-STORE-RQ: %w", err)
	}

	// The command and the data set go out as one message: nothing of another's
	// may come between them on this context.
	s.sendMu.Lock()
	err = assoc.SendPData(ctx, pcID, cmdBytes, true)
	if err == nil {
		err = assoc.sendDataSetStream(ctx, pcID, req.DataSet)
	}
	s.sendMu.Unlock()

	var source *streamSourceError
	if errors.As(err, &source) {
		// The peer has been told a data set follows and has not been given its
		// last fragment. An abort is the only way to say it will not come.
		_ = assoc.Abort(ctx, abortSourceServiceUser, abortReasonNotSpecified)
		return 0, fmt.Errorf("network: reading the data set to send: %w", source.err)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to send C-STORE-RQ: %w", err)
	}

	msg, err := assoc.ReceiveResponse(ctx, messageID)
	if err != nil {
		return 0, fmt.Errorf("failed to receive C-STORE-RSP: %w", err)
	}
	respDS, err := decodeCommandDataset(assoc.logContext(ctx), msg.command)
	if err != nil {
		return 0, fmt.Errorf("failed to decode C-STORE-RSP: %w", err)
	}
	_, _, status, err = ParseCommandDataset(respDS)
	if err != nil {
		return 0, err
	}
	return status, nil
}

// A-ABORT source and reason (PS3.8 Table 9-26) for an abort the application
// itself decides on.
const (
	abortSourceServiceUser  byte = 0
	abortReasonNotSpecified byte = 0
)

// exactStoreContext finds the accepted presentation context for a SOP class
// in exactly the given transfer syntax. The lowest ID wins when the peer
// accepted more than one, so the choice is the same every time.
func exactStoreContext(accepted map[byte]*PresentationContext, sopClassUID, transferSyntax string) (byte, bool) {
	var best byte
	found := false
	for id, pc := range accepted {
		if pc.AbstractSyntax != sopClassUID || pc.TransferSyntax != transferSyntax {
			continue
		}
		if !found || id < best {
			best, found = id, true
		}
	}
	return best, found
}

// streamSourceError marks an error as the data set reader's, as opposed to
// the connection's: the two are handled differently, since only the first
// leaves a message half sent that an abort must disown.
type streamSourceError struct{ err error }

func (e *streamSourceError) Error() string { return e.err.Error() }
func (e *streamSourceError) Unwrap() error { return e.err }

// sendDataSetStream sends the bytes of r as the data set of the message
// whose command was just sent on contextID.
//
// One fragment is always held back: whether a fragment is the last is only
// known once the next read has returned no more, and a fragment sent as the
// last before the reader has ended would let a reader that fails right after
// it complete a damaged data set. An error from r is returned as a
// *streamSourceError with the held fragment unsent.
func (a *Association) sendDataSetStream(ctx context.Context, contextID byte, r io.Reader) error {
	a.mu.RLock()
	if a.state != StateAssociated {
		state := a.state
		a.mu.RUnlock()
		return NewAssociationError("INVALID_STATE", fmt.Sprintf("cannot send data in state %s", state))
	}
	maxPDU := a.maxPDUSize
	a.mu.RUnlock()

	// A PDU's header, the PDV length and the PDV header take 12 bytes.
	size := int(maxPDU) - 12
	if maxPDU == 0 || size <= 0 {
		size = int(DefaultMaxPDUSize) - 12
	}
	if size > maxStreamFragment {
		size = maxStreamFragment
	}

	fill := func(buf []byte) (n int, ended bool, err error) {
		n, err = io.ReadFull(r, buf)
		switch {
		case err == nil:
			return n, false, nil
		case err == io.EOF, errors.Is(err, io.ErrUnexpectedEOF):
			// ReadFull's own words for "the reader ended": io.EOF with nothing
			// read, or io.ErrUnexpectedEOF with a partial buffer.
			return n, true, nil
		}
		return n, false, &streamSourceError{err: err}
	}
	send := func(data []byte, last bool) error {
		return a.transport.WritePDU(ctx, &PDataTF{PDVItems: []PDVItem{{
			PresentationContextID: contextID,
			IsLast:                last,
			Data:                  data,
		}}})
	}

	held, next := make([]byte, size), make([]byte, size)
	heldN, ended, err := fill(held)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			// Ended by the caller partway: the peer is owed the same abort as
			// for a reader that failed.
			return &streamSourceError{err: err}
		}
		if ended {
			return send(held[:heldN], true)
		}
		nextN, nextEnded, err := fill(next)
		if err != nil {
			return err
		}
		if nextEnded && nextN == 0 {
			// The data set ended exactly on a fragment boundary.
			return send(held[:heldN], true)
		}
		if err := send(held[:heldN], false); err != nil {
			return err
		}
		held, next = next, held
		heldN, ended = nextN, nextEnded
	}
}

// StoreStreamRequestFromFile builds the request to send a Part 10 file as it
// is stored: the SOP class, SOP instance and transfer syntax from its File
// Meta Information, and the data set that follows it as a reader over the
// file, which is not read until it is sent.
//
// size is the length of the file in bytes. The file is parsed for its
// structure only, with bulk values left where they are, so memory follows its
// attributes and not its pixels. A file with no File Meta Information, or
// whose meta group lacks a SOP class, SOP instance or transfer syntax, is
// refused: nothing says which syntax its bytes are in.
//
// For a file on disk:
//
//	f, _ := os.Open(path)
//	st, _ := f.Stat()
//	req, err := network.StoreStreamRequestFromFile(f, st.Size())
//	status, err := scu.StoreStream(ctx, req)
func StoreStreamRequestFromFile(r io.ReaderAt, size int64) (StoreStreamRequest, error) {
	df, err := filereader.ReadDICOMFileWithOptions(
		filebase.NewFileReader(io.NewSectionReader(r, 0, size)),
		filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		return StoreStreamRequest{}, fmt.Errorf("network: reading the file to send: %w", err)
	}
	meta := df.FileMetaInfo
	if !df.HasPreamble || meta == nil || df.DataSetOffset <= 0 {
		return StoreStreamRequest{}, fmt.Errorf(
			"network: the file has no File Meta Information, so the transfer syntax of its data set is not known")
	}
	if meta.MediaStorageSOPClassUID == "" || meta.MediaStorageSOPInstanceUID == "" || meta.TransferSyntaxUID == "" {
		return StoreStreamRequest{}, fmt.Errorf(
			"network: the file's meta group lacks a SOP Class UID, SOP Instance UID or Transfer Syntax UID")
	}
	if df.DataSetOffset > size {
		return StoreStreamRequest{}, fmt.Errorf("network: the file ends inside its File Meta Information")
	}
	return StoreStreamRequest{
		SOPClassUID:       meta.MediaStorageSOPClassUID,
		SOPInstanceUID:    meta.MediaStorageSOPInstanceUID,
		TransferSyntaxUID: meta.TransferSyntaxUID,
		DataSet:           io.NewSectionReader(r, df.DataSetOffset, size-df.DataSetOffset),
	}, nil
}
