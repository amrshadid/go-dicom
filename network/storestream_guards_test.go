package network

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

// sendDataChunks sends total bytes of a data set already begun, in PDVs of
// 16000 bytes generated as they go, the last marked last when last is set.
func sendDataChunks(ctx context.Context, scu *SCU, contextID byte, total int64, last bool) error {
	chunk := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 4000)
	for sent := int64(0); sent < total; {
		n := min(int64(len(chunk)), total-sent)
		sent += n
		pdv := PDVItem{PresentationContextID: contextID, IsLast: last && sent == total, Data: chunk[:n]}
		if err := sendPDUs(ctx, scu, [][]PDVItem{{pdv}}); err != nil {
			return err
		}
	}
	return nil
}

// noResponseWithin fails if the SCP sends anything within d.
func noResponseWithin(ctx context.Context, t *testing.T, scu *SCU, d time.Duration, why string) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	if pdu, err := scu.Association().transport.ReadPDU(readCtx); err == nil {
		t.Fatalf("%s: the SCP sent %T before the data set had arrived", why, pdu)
	} else if timeout := (*TimeoutError)(nil); !errors.As(err, &timeout) && !errors.Is(err, context.DeadlineExceeded) {
		// The read's own deadline and the context's are the same instant, and
		// either may be the one that reports it.
		t.Fatalf("%s: %v", why, err)
	}
}

// TestStreamedStoreDrainsWhatTheHandlerLeft: a handler that returns before
// the end of its data set has the rest read and discarded before its status
// goes out, as the data set arrives and without holding it: no response comes
// while the peer is still sending, the response then answers the right
// message, and a remainder of 256 MiB leaves the heap under 64 MiB. Without
// the drain the status went out at once and the rest of the data set was
// read, fragment after fragment, as stray data.
func TestStreamedStoreDrainsWhatTheHandlerLeft(t *testing.T) {
	const limit = 64 << 20
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))
	remainder := int64(256 << 20)
	if testing.Short() {
		remainder = 4 << 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	returned := make(chan struct{}, 1)
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		defer func() { returned <- struct{}{} }()
		if _, err := io.ReadFull(data, make([]byte, 10)); err != nil {
			return nil, err
		}
		return &CStoreResponse{Status: StatusWarning}, nil
	}})

	if err := sendPDUs(ctx, scu, packed(fragmentsOf(ctxExplicit, true, storeCommand(7, "1.2.3.40"), 16000), 1)); err != nil {
		t.Fatal(err)
	}
	if err := sendDataChunks(ctx, scu, ctxExplicit, 100_000, false); err != nil {
		t.Fatal(err)
	}
	<-returned
	noResponseWithin(ctx, t, scu, 300*time.Millisecond, "half the data set sent")

	runtime.GC()
	stop := make(chan struct{})
	peak := peakHeap(stop)
	if err := sendDataChunks(ctx, scu, ctxExplicit, remainder, true); err != nil {
		t.Fatal(err)
	}
	status, answers := responseStatus(ctx, t, scu)
	close(stop)
	got := <-peak
	if status != StatusWarning || answers != 7 {
		t.Fatalf("status 0x%04X answering %d, want 0x0001 answering 7", status, answers)
	}
	t.Logf("drained %d MiB with a peak heap of %.1f MiB", remainder>>20, float64(got)/(1<<20))
	if got > limit {
		t.Errorf("peak heap %.1f MiB while draining: the remainder was held", float64(got)/(1<<20))
	}

	// And the association is in step for the next.
	sendStore(ctx, t, scu, ctxExplicit, 8, "1.2.3.41", make([]byte, 50), 16000, 1)
	if status, answers := responseStatus(ctx, t, scu); status != StatusWarning || answers != 8 {
		t.Fatalf("next store: status 0x%04X answering %d", status, answers)
	}
}

// TestStreamedStoreDrainStopsAtMaxDataSetSize: the drain is bounded as the
// read is. A handler that refuses early does not make the SCP take more than
// MaxDataSetSize: past it the association is aborted and no response sent.
func TestStreamedStoreDrainStopsAtMaxDataSetSize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	returned := make(chan struct{}, 1)
	_, scu := streamServer(t, ctx, NetworkConfig{MaxDataSetSize: 1 << 20}, &streamHandler{fn: func(context.Context, *CStoreRequest, string, io.Reader) (*CStoreResponse, error) {
		defer func() { returned <- struct{}{} }()
		return &CStoreResponse{Status: StatusOutOfResources}, nil
	}})
	if err := sendPDUs(ctx, scu, packed(fragmentsOf(ctxExplicit, true, storeCommand(1, "1.2.3.42"), 16000), 1)); err != nil {
		t.Fatal(err)
	}
	go func() { _ = sendDataChunks(ctx, scu, ctxExplicit, 4<<20, true) }()
	<-returned
	expectAbort(ctx, t, scu, "4 MiB drained against a 1 MiB MaxDataSetSize")
}

// TestCompositeHandlerPassesTheStreamOn: a CStoreStreamer set as a
// CompositeHandler's store handler was reached through HandleCStore, which
// BaseHandler answers with Success: every instance was decoded, discarded and
// acknowledged. The composite now passes the stream on, and a store handler
// that does not stream still gets the decoded data set.
func TestCompositeHandlerPassesTheStreamOn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	data, err := EncodeDataset(syntheticInstance(t, "1.2.3.43", 100), ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}

	var streamed atomic.Int64
	comp := NewCompositeHandler()
	comp.SetStoreHandler(&streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		n, err := io.Copy(io.Discard, data)
		streamed.Store(n)
		return &CStoreResponse{Status: StatusSuccess}, err
	}})
	_, scu := streamServer(t, ctx, NetworkConfig{}, comp)
	sendStore(ctx, t, scu, ctxImplicit, 1, "1.2.3.43", data, 16000, 1)
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess || streamed.Load() != int64(len(data)) {
		t.Fatalf("status 0x%04X, the streaming store handler read %d of %d bytes", status, streamed.Load(), len(data))
	}

	var decoded atomic.Pointer[dataset.Dataset]
	plain := NewCompositeHandler()
	plain.SetStoreHandler(&StorageHandler{OnStore: func(_ context.Context, _, _ string, ds *dataset.Dataset) uint16 {
		decoded.Store(ds)
		return StatusSuccess
	}})
	_, scu = streamServer(t, ctx, NetworkConfig{}, plain)
	sendStore(ctx, t, scu, ctxImplicit, 1, "1.2.3.43", data, 16000, 1)
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess || decoded.Load() == nil {
		t.Fatalf("status 0x%04X; a store handler that does not stream was given %v", status, decoded.Load())
	}

	// A composite with no store handler does not claim to stream.
	if _, ok := streamsCStore(NewCompositeHandler()); ok {
		t.Error("an empty CompositeHandler is served as a streamer")
	}
}

// TestWriteCStoreFileRefusesWhatTheHeaderCannotHold: the UIDs and titles of
// the File Meta Information come from the peer. A 70,000-character SOP
// Instance UID was written with its 16-bit length truncated to 4466 while
// the group length counted all of it, and one holding a newline, a backslash
// or "/../" was written as it was. Each is now refused before anything is
// written, and an AE title that is not one is left out.
func TestWriteCStoreFileRefusesWhatTheHeaderCannotHold(t *testing.T) {
	data := []byte{0x08, 0x00, 0x18, 0x00, 'U', 'I', 2, 0, '1', 0}
	for _, c := range []struct {
		name      string
		req       CStoreRequest
		ts        string
		wantError error
	}{
		{"a UID too long for its length field", CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1." + strings.Repeat("2", 70_000)}, ExplicitVRLittleEndianUID, ErrInvalidSOPInstanceUID},
		{"control characters and a path", CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2\n3\\..\x01/../x"}, ExplicitVRLittleEndianUID, ErrInvalidSOPInstanceUID},
		{"65 characters", CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1." + strings.Repeat("3", 63)}, ExplicitVRLittleEndianUID, ErrInvalidSOPInstanceUID},
		{"a leading zero", CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.02.3"}, ExplicitVRLittleEndianUID, ErrInvalidSOPInstanceUID},
		{"a SOP Class that is not a UID", CStoreRequest{AffectedSOPClass: "CT", AffectedSOPInstance: "1.2.3"}, ExplicitVRLittleEndianUID, ErrInvalidStoreCommand},
		{"a transfer syntax that is not a UID", CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2.3"}, "explicit", ErrInvalidStoreCommand},
	} {
		var file bytes.Buffer
		reader := bytes.NewReader(data)
		n, err := WriteCStoreFile(context.Background(), &file, &c.req, c.ts, reader)
		if !errors.Is(err, c.wantError) {
			t.Errorf("%s: %v, want %v", c.name, err, c.wantError)
		}
		if file.Len() != 0 || n != 0 || reader.Len() != len(data) {
			t.Errorf("%s: wrote %d bytes and read %d of the data set before refusing", c.name, file.Len(), len(data)-reader.Len())
		}
	}

	ctx := ContextWithAssociationInfo(context.Background(), &AssociationInfo{CallingAE: "BAD\\AE\n", CalledAE: " ARCHIVE "})
	var file bytes.Buffer
	req := &CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2.3"}
	if _, err := WriteCStoreFile(ctx, &file, req, ExplicitVRLittleEndianUID, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	parsed, err := readFile(file.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.FileMetaInfo.SendingApplicationEntityTitle; got != "" {
		t.Errorf("an AE title holding a backslash and a newline was written: %q", got)
	}
	if got := strings.TrimRight(parsed.FileMetaInfo.ReceivingApplicationEntityTitle, " "); got != "ARCHIVE" {
		t.Errorf("Receiving AE Title %q, want ARCHIVE", got)
	}
}

// eofWriter takes ok writes, then fails each with an error that wraps io.EOF,
// as a pipe whose reader has gone can.
type eofWriter struct{ ok int }

func (w *eofWriter) Write(p []byte) (int, error) {
	if w.ok > 0 {
		w.ok--
		return len(p), nil
	}
	return 0, fmt.Errorf("the disk went away: %w", io.EOF)
}

// TestWriteCStoreFileNeverReturnsEOF: a handler tells a whole data set from a
// cut one by the error, so no failure of WriteCStoreFile may read as the end
// of the data: a writer's error that wraps io.EOF, on the header or on the
// data set, is returned as io.ErrUnexpectedEOF, with its text.
func TestWriteCStoreFileNeverReturnsEOF(t *testing.T) {
	req := &CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2.3"}
	for ok, where := range []string{"the header", "the data set"} {
		_, err := WriteCStoreFile(context.Background(), &eofWriter{ok: ok}, req, ExplicitVRLittleEndianUID,
			bytes.NewReader(make([]byte, 10)))
		if err == nil || errors.Is(err, io.EOF) || !errors.Is(err, io.ErrUnexpectedEOF) ||
			!strings.Contains(err.Error(), "the disk went away") {
			t.Errorf("a failed write of %s returned %v", where, err)
		}
	}
}

// TestStreamedStoreOfAnInvalidUIDOverTheWire: a peer's C-STORE naming a SOP
// Instance UID no file can carry is answered with the status the handler
// chooses for WriteCStoreFile's error, 0117H as PS3.7 9.1.1.1.9 gives it,
// nothing is written, and the association goes on.
func TestStreamedStoreOfAnInvalidUIDOverTheWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var written atomic.Int64
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(ctx context.Context, req *CStoreRequest, ts string, data io.Reader) (*CStoreResponse, error) {
		var file bytes.Buffer
		_, err := WriteCStoreFile(ctx, &file, req, ts, data)
		written.Add(int64(file.Len()))
		switch {
		case errors.Is(err, ErrInvalidSOPInstanceUID):
			return &CStoreResponse{Status: StatusInvalidObjectInstance}, nil
		case err != nil:
			return nil, err
		}
		return &CStoreResponse{Status: StatusSuccess}, nil
	}})
	for i, uid := range []string{"1." + strings.Repeat("2", 70_000), "1.2\n3\\..\x01/../x"} {
		sendStore(ctx, t, scu, ctxImplicit, uint16(i+1), uid, make([]byte, 40_000), 16000, 1)
		if status, answers := responseStatus(ctx, t, scu); status != StatusInvalidObjectInstance || answers != uint16(i+1) {
			t.Fatalf("case %d: status 0x%04X answering %d, want 0x0117", i, status, answers)
		}
	}
	if written.Load() != 0 {
		t.Errorf("%d bytes written for commands that name no valid instance", written.Load())
	}
	sendStore(ctx, t, scu, ctxImplicit, 3, "1.2.3.44", make([]byte, 40), 16000, 1)
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
		t.Fatalf("the next store: status 0x%04X", status)
	}
}

// TestStoreRequestCarriesTheCommand: Priority and, for a C-MOVE's
// sub-operation, Move Originator AE Title and Message ID reach the handler,
// whichever way it takes the store. The decoded path filled none of them and
// the stream only Priority.
func TestStoreRequestCarriesTheCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := BuildCStoreRQ(1, CTImageStorageUID, "1.2.3.45", PriorityHigh)
	addAEElement(cmd, tagMoveOriginatorAE, "MOVER")
	addUSElement(cmd, tagMoveOriginatorMessageID, 42)
	encoded, err := EncodeCommandDataset(cmd)
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeDataset(syntheticInstance(t, "1.2.3.45", 10), ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	want := CStoreRequest{MessageID: 1, AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2.3.45",
		Priority: PriorityHigh, MoveOriginatorAE: "MOVER", MoveOriginatorMsgID: 42}

	var got atomic.Pointer[CStoreRequest]
	record := func(req *CStoreRequest) {
		c := *req
		c.DataSet = nil
		got.Store(&c)
	}
	for name, handler := range map[string]Handler{
		"HandleCStore": &recordingStore{record: record},
		"CStoreStreamer": &streamHandler{fn: func(_ context.Context, req *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
			record(req)
			_, err := io.Copy(io.Discard, data)
			return &CStoreResponse{Status: StatusSuccess}, err
		}},
	} {
		got.Store(nil)
		_, scu := streamServer(t, ctx, NetworkConfig{}, handler)
		pdvs := append(fragmentsOf(ctxImplicit, true, encoded, 16000), fragmentsOf(ctxImplicit, false, data, 16000)...)
		if err := sendPDUs(ctx, scu, packed(pdvs, 1)); err != nil {
			t.Fatal(err)
		}
		if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
			t.Fatalf("%s: status 0x%04X", name, status)
		}
		if r := got.Load(); r == nil || *r != want {
			t.Errorf("%s was given %+v, want %+v", name, r, want)
		}
	}
}

// recordingStore is a HandleCStore handler that records its request.
type recordingStore struct {
	BaseHandler
	record func(*CStoreRequest)
}

func (h *recordingStore) HandleCStore(_ context.Context, req *CStoreRequest) (*CStoreResponse, error) {
	h.record(req)
	return &CStoreResponse{Status: StatusSuccess}, nil
}

// TestStreamedStoreAtMaxDataSetSize: a data set of exactly MaxDataSetSize is
// taken and one byte more is not, whether the handler reads it all or the
// SCP drains it.
func TestStreamedStoreAtMaxDataSetSize(t *testing.T) {
	const max = 50_000
	for _, early := range []bool{false, true} {
		for _, size := range []int{max - 1, max, max + 1} {
			t.Run(fmt.Sprintf("early=%v/size=%d", early, size), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				done := make(chan struct{})
				_, scu := streamServer(t, ctx, NetworkConfig{MaxDataSetSize: max}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
					defer close(done)
					if early {
						_, err := io.ReadFull(data, make([]byte, 10))
						return &CStoreResponse{Status: StatusOutOfResources}, err
					}
					_, err := io.Copy(io.Discard, data)
					return &CStoreResponse{Status: StatusSuccess}, err
				}})
				go func() {
					pdvs := append(fragmentsOf(ctxImplicit, true, storeCommand(1, "1.2.3.46"), 16000),
						fragmentsOf(ctxImplicit, false, make([]byte, size), 16000)...)
					_ = sendPDUs(ctx, scu, packed(pdvs, 1))
				}()
				<-done
				readCtx, cancelRead := context.WithTimeout(ctx, 3*time.Second)
				defer cancelRead()
				pdu, err := scu.Association().transport.ReadPDU(readCtx)
				_, isData := pdu.(*PDataTF)
				if size > max && isData {
					t.Error("a response was sent for a data set over the bound")
				}
				if size <= max && !isData {
					t.Errorf("no response for a data set within the bound: %T %v", pdu, err)
				}
			})
		}
	}
}

// randomFragments cuts b into PDVs of random size, from empty to a whole PDU,
// with empty PDVs that are not last scattered among them.
func randomFragments(rng *rand.Rand, contextID byte, isCommand bool, b []byte, maxData int) []PDVItem {
	var out []PDVItem
	for {
		if rng.Intn(8) == 0 {
			out = append(out, PDVItem{PresentationContextID: contextID, IsCommand: isCommand, Data: []byte{}})
		}
		var n int
		switch rng.Intn(4) {
		case 0:
			n = 1
		case 1:
			n = rng.Intn(64)
		case 2:
			n = rng.Intn(maxData + 1)
		default:
			n = maxData
		}
		if n >= len(b) {
			return append(out, PDVItem{PresentationContextID: contextID, IsCommand: isCommand, IsLast: true, Data: b})
		}
		out = append(out, PDVItem{PresentationContextID: contextID, IsCommand: isCommand, Data: b[:n]})
		b = b[n:]
	}
}

// packWithin packs PDVs into P-DATA-TFs of at most budget bytes, a random
// number to each.
func packWithin(rng *rand.Rand, pdvs []PDVItem, budget int) [][]PDVItem {
	var out [][]PDVItem
	var cur []PDVItem
	size, want := 0, 1+rng.Intn(6)
	for _, p := range pdvs {
		item := 6 + len(p.Data)
		if len(cur) > 0 && (size+item > budget || len(cur) >= want) {
			out = append(out, cur)
			cur, size, want = nil, 0, 1+rng.Intn(6)
		}
		cur = append(cur, p)
		size += item
	}
	return append(out, cur)
}

// TestStreamedStoreAnyFragmentation: many C-STOREs on one association, each
// command and data set cut at random (empty PDVs, 1-byte PDVs, PDVs filling a
// PDU) and packed at random, so a data set's last fragment shares a PDU with
// the next command. The handler reads exactly what was sent, every time.
func TestStreamedStoreAnyFragmentation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var mu sync.Mutex
	sums := map[string][32]byte{}
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(_ context.Context, req *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		h := sha256.New()
		_, err := io.Copy(h, data)
		var sum [32]byte
		copy(sum[:], h.Sum(nil))
		mu.Lock()
		sums[req.AffectedSOPInstance] = sum
		mu.Unlock()
		return &CStoreResponse{Status: StatusSuccess}, err
	}})
	maxData := DefaultMaxPDUSize - 6
	rng := rand.New(rand.NewSource(321))
	messageID := uint16(0)
	rounds := 60
	if testing.Short() {
		rounds = 10
	}
	for round := 0; round < rounds; round++ {
		var pdvs []PDVItem
		want := map[string][32]byte{}
		var order []string
		for m := 0; m < 1+rng.Intn(5); m++ {
			var size int
			switch rng.Intn(6) {
			case 0:
				size = 0
			case 1:
				size = 1 + 2*rng.Intn(5)
			case 2:
				size = maxData
			case 3:
				size = maxData + 1
			default:
				size = rng.Intn(200_000)
			}
			data := make([]byte, size)
			rng.Read(data)
			messageID++
			id := fmt.Sprintf("1.2.%d.%d", round+1, m+1)
			contextID := []byte{ctxImplicit, ctxExplicit, ctxRLE}[rng.Intn(3)]
			pdvs = append(pdvs, randomFragments(rng, contextID, true, storeCommand(messageID, id), maxData)...)
			pdvs = append(pdvs, randomFragments(rng, contextID, false, data, maxData)...)
			want[id] = sha256.Sum256(data)
			order = append(order, id)
		}
		sent := make(chan error, 1)
		go func() { sent <- sendPDUs(ctx, scu, packWithin(rng, pdvs, DefaultMaxPDUSize)) }()
		for range order {
			if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
				t.Fatalf("round %d: status 0x%04X", round, status)
			}
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		for _, id := range order {
			if sums[id] != want[id] {
				t.Errorf("%s: the handler did not read what was sent", id)
			}
		}
		mu.Unlock()
	}
}

// TestStreamedStoreHostilePeer: a data set interrupted at a random point by
// something that is not the rest of it: a PDU of an unknown type, an
// A-RELEASE-RQ, an A-ABORT, data on another context or on one never
// proposed, a command fragment, silence, a closed connection, a PDV whose
// length runs past its PDU, a second A-ASSOCIATE-RQ. The handler's read ends
// in io.EOF only when nothing of the kind came, having read exactly what was
// sent; otherwise it fails, and no C-STORE-RSP reaches the peer. Empty
// P-DATA-TFs and floods of empty PDVs are harmless and change nothing.
func TestStreamedStoreHostilePeer(t *testing.T) {
	rng := rand.New(rand.NewSource(320))
	kinds := []string{"none", "unknown-type", "release-rq", "abort", "other-context", "unproposed-context",
		"command-fragment", "silence", "closed-connection", "overlong-pdv", "associate-rq", "empty-pdata", "empty-pdvs"}
	cases := 260
	if testing.Short() {
		cases = 40
	}
	for i := 0; i < cases; i++ {
		kind := kinds[i%len(kinds)]
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var mu sync.Mutex
			var got []byte
			var readErr error
			done := make(chan struct{})
			server, scu := streamServer(t, ctx, NetworkConfig{DIMSETimeout: 300 * time.Millisecond}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
				defer close(done)
				b, err := io.ReadAll(data)
				mu.Lock()
				got, readErr = b, err
				mu.Unlock()
				return &CStoreResponse{Status: StatusSuccess}, err
			}})
			defer server.Stop()
			conn := scu.Association().transport.conn

			data := make([]byte, rng.Intn(30000))
			rng.Read(data)
			fragments := randomFragments(rng, ctxImplicit, false, data, 4000)
			cut := rng.Intn(len(fragments))
			_ = sendPDUs(ctx, scu, [][]PDVItem{fragmentsOf(ctxImplicit, true, storeCommand(1, "1.2.3.47"), 16000)})
			_ = sendPDUs(ctx, scu, packWithin(rng, fragments[:cut], 16000))
			raw := func(pduType byte, body []byte) {
				header := []byte{pduType, 0, 0, 0, 0, 0}
				binary.BigEndian.PutUint32(header[2:], uint32(len(body)))
				_, _ = conn.Write(append(header, body...))
			}
			switch kind {
			case "unknown-type":
				raw(0x09, []byte{1, 2, 3, 4})
			case "release-rq":
				raw(0x05, make([]byte, 4))
			case "abort":
				raw(0x07, make([]byte, 4))
			case "other-context":
				_ = sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: ctxOther, Data: []byte{1, 2}}}})
			case "unproposed-context":
				_ = sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: 99, Data: []byte{1, 2}, IsLast: true}}})
			case "command-fragment":
				_ = sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: ctxImplicit, IsCommand: true, Data: []byte{1, 2}}}})
			case "closed-connection":
				_ = conn.Close()
			case "overlong-pdv":
				raw(0x04, []byte{0, 0, 0, 100, ctxImplicit, 0, 1, 2, 3, 4})
			case "associate-rq":
				raw(0x01, make([]byte, 68))
			case "empty-pdata":
				raw(0x04, nil)
			case "empty-pdvs":
				for k := 0; k < 50; k++ {
					_ = sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: ctxImplicit, Data: []byte{}}}})
				}
			}
			harmless := kind == "none" || kind == "empty-pdata" || kind == "empty-pdvs"
			if kind != "closed-connection" && kind != "silence" {
				_ = sendPDUs(ctx, scu, packWithin(rng, fragments[cut:], 16000))
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("case %d, %s: the handler did not return", i, kind)
			}
			mu.Lock()
			defer mu.Unlock()
			var answered bool
			if kind != "closed-connection" {
				readCtx, cancelRead := context.WithTimeout(ctx, time.Second)
				pdu, _ := scu.Association().transport.ReadPDU(readCtx)
				cancelRead()
				_, answered = pdu.(*PDataTF)
			}
			if harmless {
				if readErr != nil || !bytes.Equal(got, data) || !answered {
					t.Errorf("case %d, %s: %v, read %d of %d bytes, answered %v", i, kind, readErr, len(got), len(data), answered)
				}
				return
			}
			if readErr == nil || errors.Is(readErr, io.EOF) {
				t.Errorf("case %d, %s (cut at %d of %d): the read ended as a whole data set, %d of %d bytes: %v",
					i, kind, cut, len(fragments), len(got), len(data), readErr)
			}
			if answered {
				t.Errorf("case %d, %s: a C-STORE-RSP was sent", i, kind)
			}
		}()
	}
}

// TestStreamedStoresAtOnceStayBounded: eight associations each streaming an
// instance at once keep the heap under the limit one would.
func TestStreamedStoresAtOnceStayBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("sends a gigabyte")
	}
	const limit = 64 << 20
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "STREAM", Port: 0, BindAddress: "127.0.0.1"},
		&streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
			_, err := io.Copy(sha256.New(), data)
			return &CStoreResponse{Status: StatusSuccess}, err
		}})
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	stop := make(chan struct{})
	peak := peakHeap(stop)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scu := NewSCU(SCUConfig{CallingAE: "SENDER", CalledAE: "STREAM", Address: server.Addr()})
			if err := scu.Associate(ctx, []PresentationContextItem{
				{ID: ctxExplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
			}); err != nil {
				t.Error(err)
				return
			}
			defer scu.Association().transport.Close()
			if err := sendBigStore(ctx, scu, 1, 128<<20); err != nil {
				t.Error(err)
				return
			}
			if _, _, _, err := scu.Association().ReceivePData(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(stop)
	got := <-peak
	t.Logf("8 x 128 MiB at once, peak heap %.1f MiB", float64(got)/(1<<20))
	if got > limit {
		t.Errorf("peak heap %.1f MiB, over the %d MiB limit", float64(got)/(1<<20), limit>>20)
	}
	server.Stop()
	goroutinesSettle(t, base)
}

// TestStreamedStoreHandlerThatDoesNotRead: while the handler does not read,
// nothing is read: the peer is held back by TCP and the heap does not grow,
// however long the handler takes and whatever DIMSETimeout says, since the
// timeout bounds the peer, not the handler.
func TestStreamedStoreHandlerThatDoesNotRead(t *testing.T) {
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	release := make(chan struct{})
	server, scu := streamServer(t, ctx, NetworkConfig{DIMSETimeout: time.Second}, &streamHandler{fn: func(context.Context, *CStoreRequest, string, io.Reader) (*CStoreResponse, error) {
		<-release
		return &CStoreResponse{Status: StatusOutOfResources}, nil
	}})
	sent := make(chan error, 1)
	go func() { sent <- sendBigStore(ctx, scu, 1, 64<<20) }()
	time.Sleep(500 * time.Millisecond)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	time.Sleep(2 * time.Second)
	runtime.GC()
	runtime.ReadMemStats(&after)
	grew := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("held 2s past a 1s DIMSETimeout: heap %d KiB -> %d KiB", before.HeapAlloc>>10, after.HeapAlloc>>10)
	if grew > 4<<20 {
		t.Errorf("the heap grew %d KiB while the handler did not read", grew>>10)
	}
	close(release)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if status, _ := responseStatus(ctx, t, scu); status != StatusOutOfResources {
		t.Fatalf("status 0x%04X", status)
	}
	_ = scu.Association().transport.Close()
	server.Stop()
	goroutinesSettle(t, base)
}
