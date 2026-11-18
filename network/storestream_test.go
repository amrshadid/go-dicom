package network

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// HandleCStore was given a decoded data set, which the SCP built by holding
// the whole message in memory, so memory grew with the instance, and a file
// written from it held this library's encoding rather than the bytes sent. A
// CStoreStreamer reads the data set as it arrives.

// streamHandler is a CStoreStreamer made of a function.
type streamHandler struct {
	BaseHandler
	fn func(ctx context.Context, req *CStoreRequest, ts string, data io.Reader) (*CStoreResponse, error)
}

func (h *streamHandler) HandleCStoreStream(ctx context.Context, req *CStoreRequest, ts string,
	data io.Reader) (*CStoreResponse, error) {
	return h.fn(ctx, req, ts, data)
}

// The presentation contexts streamServer's SCU proposes, one per syntax.
const (
	ctxImplicit byte = 1
	ctxExplicit byte = 3
	ctxRLE      byte = 5
	ctxOther    byte = 7
)

// streamServer starts an SCP that accepts every transfer syntax, with the
// given handler and network settings, and returns an SCU associated with it.
func streamServer(t testing.TB, ctx context.Context, cfg NetworkConfig, handler Handler) (*Server, *SCU) {
	t.Helper()
	server, err := StartServer(ctx, SCPConfig{AETitle: "STREAM", Port: 0, BindAddress: "127.0.0.1", Network: cfg}, handler)
	if err != nil {
		t.Fatal(err)
	}
	server.SetSupportedTransferSyntaxes(AllTransferSyntaxes())
	t.Cleanup(server.Stop)

	scu := NewSCU(SCUConfig{CallingAE: "SENDER", CalledAE: "STREAM", Address: server.Addr()})
	if err := scu.Associate(ctx, []PresentationContextItem{
		{ID: ctxImplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
		{ID: ctxExplicit, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{ExplicitVRLittleEndianUID}},
		{ID: ctxRLE, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: []string{RLELosslessUID}},
		{ID: ctxOther, AbstractSyntax: MRImageStorageUID, TransferSyntaxes: []string{ImplicitVRLittleEndianUID}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scu.Association().transport.Close() })
	return server, scu
}

// storeCommand is the encoded command set of a C-STORE-RQ.
func storeCommand(messageID uint16, instance string) []byte {
	cmd, err := EncodeCommandDataset(BuildCStoreRQ(messageID, CTImageStorageUID, instance, PriorityMedium))
	if err != nil {
		panic(err) // a command built here always encodes
	}
	return cmd
}

// fragmentsOf cuts b into PDVs of at most size bytes, the last marked last.
func fragmentsOf(contextID byte, isCommand bool, b []byte, size int) []PDVItem {
	var out []PDVItem
	for len(b) > size {
		out = append(out, PDVItem{PresentationContextID: contextID, IsCommand: isCommand, Data: b[:size]})
		b = b[size:]
	}
	return append(out, PDVItem{PresentationContextID: contextID, IsCommand: isCommand, IsLast: true, Data: b})
}

// packed puts PDVs into P-DATA-TFs of perPDU each, in order, so that the last
// fragment of a command and the first of its data set can share one.
func packed(pdvs []PDVItem, perPDU int) [][]PDVItem {
	var out [][]PDVItem
	for len(pdvs) > perPDU {
		out = append(out, pdvs[:perPDU])
		pdvs = pdvs[perPDU:]
	}
	return append(out, pdvs)
}

// sendPDUs writes each group of PDVs as one P-DATA-TF.
func sendPDUs(ctx context.Context, scu *SCU, pdus [][]PDVItem) error {
	for _, pdvs := range pdus {
		if err := scu.Association().transport.WritePDU(ctx, &PDataTF{PDVItems: pdvs}); err != nil {
			return err
		}
	}
	return nil
}

// sendStore sends a C-STORE whose command and data set are cut into PDVs of
// fragment bytes, packed perPDU to a P-DATA-TF.
func sendStore(ctx context.Context, t *testing.T, scu *SCU, contextID byte, messageID uint16, instance string,
	data []byte, fragment, perPDU int) {
	t.Helper()
	pdvs := fragmentsOf(contextID, true, storeCommand(messageID, instance), fragment)
	pdvs = append(pdvs, fragmentsOf(contextID, false, data, fragment)...)
	if err := sendPDUs(ctx, scu, packed(pdvs, perPDU)); err != nil {
		t.Fatal(err)
	}
}

// responseStatus reads the next message, which must be a C-STORE-RSP, and
// returns its status and the message it answers.
func responseStatus(ctx context.Context, t *testing.T, scu *SCU) (status, answers uint16) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, rsp, isCmd, err := scu.Association().ReceivePData(readCtx)
	if err != nil {
		t.Fatalf("no C-STORE-RSP: %v", err)
	}
	if !isCmd {
		t.Fatal("the response was not a command")
	}
	ds, err := DecodeCommandDataset(rsp)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = getUSValue(ds, tagStatus)
	answers, _ = getUSValue(ds, tagMessageIDBeingRespondedTo)
	return status, answers
}

// syntheticInstance is a CT instance with n bytes of generated pixel data.
func syntheticInstance(t *testing.T, instance string, n int) *dataset.Dataset {
	t.Helper()
	ds := dataset.NewDataset()
	pixels := make([]byte, n)
	for i := range pixels {
		pixels[i] = byte(i * 7)
	}
	for _, e := range []*dataelem.DataElement{
		dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte(CTImageStorageUID)),
		dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte(instance)),
		dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("SYNTHETIC^STREAM")),
		dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, pixels),
	} {
		if err := ds.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return ds
}

// encapsulatedInstance is the bytes of an Explicit VR Little Endian data set
// whose Pixel Data is encapsulated in fragments, as an RLE or JPEG instance
// carries it (PS3.5 A.4): written by hand, so the bytes compared are not this
// library's encoder's.
func encapsulatedInstance(instance string, fragments ...[]byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	element := func(group, elem uint16, vr string, value []byte) {
		if len(value)%2 == 1 {
			value = append(value, 0)
		}
		_ = binary.Write(&b, le, [2]uint16{group, elem})
		b.WriteString(vr)
		_ = binary.Write(&b, le, uint16(len(value)))
		b.Write(value)
	}
	item := func(group, elem uint16, value []byte) {
		_ = binary.Write(&b, le, [2]uint16{group, elem})
		_ = binary.Write(&b, le, uint32(len(value)))
		b.Write(value)
	}
	element(0x0008, 0x0016, "UI", []byte(CTImageStorageUID))
	element(0x0008, 0x0018, "UI", []byte(instance))
	element(0x0028, 0x0010, "US", []byte{8, 0})
	_ = binary.Write(&b, le, [2]uint16{0x7FE0, 0x0010})
	b.WriteString("OB\x00\x00")
	_ = binary.Write(&b, le, uint32(0xFFFFFFFF))
	item(0xFFFE, 0xE000, nil) // an empty Basic Offset Table
	for _, f := range fragments {
		item(0xFFFE, 0xE000, f)
	}
	item(0xFFFE, 0xE0DD, nil)
	return b.Bytes()
}

// fileRecorder writes each streamed data set with WriteCStoreFile and
// keeps the file and what writing it returned.
type fileRecorder struct {
	mu    sync.Mutex
	files map[string][]byte
	ts    map[string]string
	errs  map[string]error
}

func newFileRecorder() *fileRecorder {
	return &fileRecorder{files: map[string][]byte{}, ts: map[string]string{}, errs: map[string]error{}}
}

func (r *fileRecorder) handler() *streamHandler {
	return &streamHandler{fn: func(ctx context.Context, req *CStoreRequest, ts string, data io.Reader) (*CStoreResponse, error) {
		var file bytes.Buffer
		_, err := WriteCStoreFile(ctx, &file, req, ts, data)
		r.mu.Lock()
		r.files[req.AffectedSOPInstance], r.ts[req.AffectedSOPInstance], r.errs[req.AffectedSOPInstance] = file.Bytes(), ts, err
		r.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return &CStoreResponse{Status: StatusSuccess}, nil
	}}
}

func (r *fileRecorder) file(instance string) ([]byte, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.files[instance], r.ts[instance], r.errs[instance]
}

// readFile reads a Part 10 file with this library's reader.
func readFile(file []byte) (*filereader.DICOMFile, error) {
	return filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
}

// dataSetOf returns the data set of a Part 10 file: what follows the
// preamble, "DICM" and the group the File Meta Information Group Length
// (0002,0000) measures.
func dataSetOf(t *testing.T, file []byte) []byte {
	t.Helper()
	if len(file) < 144 || string(file[128:132]) != "DICM" {
		t.Fatalf("not a Part 10 file: %d bytes", len(file))
	}
	if !bytes.Equal(file[132:138], []byte{0x02, 0x00, 0x00, 0x00, 'U', 'L'}) {
		t.Fatalf("the file meta does not begin with its group length: % x", file[132:138])
	}
	groupLength := binary.LittleEndian.Uint32(file[140:144])
	return file[144+int(groupLength):]
}

// TestStreamedStoreIsByteIdentical: the file WriteCStoreFile writes holds the
// data set exactly as it was sent, for each family of transfer syntax, sent
// in fragments of every size from 1 byte to whole, several to a PDU and with
// the command's last fragment sharing a PDU with the data set's first. This
// library's own reader reads the file, with the meta the command implies.
func TestStreamedStoreIsByteIdentical(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rec := newFileRecorder()
	_, scu := streamServer(t, ctx, NetworkConfig{}, rec.handler())

	implicit, err := EncodeDataset(syntheticInstance(t, "1.2.3.1", 50_001), ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := EncodeDataset(syntheticInstance(t, "1.2.3.2", 50_001), ExplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	encapsulated := encapsulatedInstance("1.2.3.3", bytes.Repeat([]byte{1, 2, 3}, 9000), []byte{9, 8, 7, 6})

	messageID := uint16(0)
	for _, c := range []struct {
		name      string
		contextID byte
		ts        string
		data      []byte
	}{
		{"implicit little endian", ctxImplicit, ImplicitVRLittleEndianUID, implicit},
		{"explicit little endian", ctxExplicit, ExplicitVRLittleEndianUID, explicit},
		{"RLE lossless, encapsulated", ctxRLE, RLELosslessUID, encapsulated},
	} {
		for _, split := range []struct{ fragment, perPDU int }{
			{1 << 20, 1}, // whole, each in a PDU of its own (the SCP reads up to its advertised maximum)
			{4000, 1},
			{4000, 3}, // the command's last fragment and the data set's first in one PDU
			{17, 400},
			{1, 2000},
		} {
			if split.fragment > 16000 {
				split.fragment = 16000 // the SCP advertised 16 KiB
			}
			messageID++
			instance := c.name + "/" + strconv.Itoa(split.fragment) + "x" + strconv.Itoa(split.perPDU)
			sendStore(ctx, t, scu, c.contextID, messageID, instance, c.data, split.fragment, split.perPDU)
			if status, answers := responseStatus(ctx, t, scu); status != StatusSuccess || answers != messageID {
				t.Fatalf("%s: status 0x%04X answering message %d, want 0x0000 answering %d", instance, status, answers, messageID)
			}
			file, ts, err := rec.file(instance)
			if err != nil {
				t.Fatalf("%s: %v", instance, err)
			}
			if ts != c.ts {
				t.Errorf("%s: the handler was told transfer syntax %s, want %s", instance, ts, c.ts)
			}
			if got := dataSetOf(t, file); !bytes.Equal(got, c.data) {
				t.Fatalf("%s: the file's data set is %d bytes and differs from the %d sent", instance, len(got), len(c.data))
			}
			parsed, err := readFile(file)
			if err != nil {
				t.Fatalf("%s: the file does not read back: %v", instance, err)
			}
			if got := parsed.FileMetaInfo.TransferSyntaxUID; got != c.ts {
				t.Errorf("%s: the file says transfer syntax %q, want %q", instance, got, c.ts)
			}
			if !bytes.Equal(file[parsed.DataSetOffset:], c.data) {
				t.Errorf("%s: the data set this library's reader finds is not the one sent", instance)
			}
		}
	}
}

// TestCommandAndDataInOnePDU: a P-DATA-TF may carry the last fragment of a
// command and the first of its data set (PS3.8 9.3.5). Whatever came after the
// fragment a read stopped at was dropped with the PDU, so the data set never
// arrived and the SCP waited until its timeout (#320). This is the decoded
// path, unchanged otherwise.
func TestCommandAndDataInOnePDU(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got atomic.Pointer[dataset.Dataset]
	_, scu := streamServer(t, ctx, NetworkConfig{DIMSETimeout: 2 * time.Second}, &StorageHandler{
		OnStore: func(_ context.Context, _, _ string, ds *dataset.Dataset) uint16 {
			got.Store(ds)
			return StatusSuccess
		},
	})
	data, err := EncodeDataset(syntheticInstance(t, "1.2.3.4", 100), ImplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	pdvs := append(fragmentsOf(ctxImplicit, true, storeCommand(1, "1.2.3.4"), 1<<20),
		fragmentsOf(ctxImplicit, false, data, 1<<20)...)
	if err := sendPDUs(ctx, scu, [][]PDVItem{pdvs}); err != nil {
		t.Fatal(err)
	}
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
		t.Fatalf("status 0x%04X", status)
	}
	if ds := got.Load(); ds == nil || ds.Length() != 4 {
		t.Fatalf("the handler was given %v", ds)
	}
}

// TestStreamedStoreHandlerStopsEarly: a handler that returns before reading to
// the end has the rest drained and its status sent, and the association goes
// on in step: the next C-STORE is answered, and answered as its own.
func TestStreamedStoreHandlerStopsEarly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var calls atomic.Int32
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(_ context.Context, req *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		calls.Add(1)
		if _, err := io.ReadFull(data, make([]byte, 10)); err != nil {
			return nil, err
		}
		switch req.AffectedSOPInstance {
		case "duplicate":
			return &CStoreResponse{Status: StatusWarning}, nil
		case "refused":
			return nil, errors.New("no room")
		}
		return &CStoreResponse{Status: StatusSuccess}, nil
	}})
	data := bytes.Repeat([]byte{0xAB}, 200_000)
	for i, c := range []struct {
		instance string
		want     uint16
	}{
		{"duplicate", StatusWarning},
		{"refused", StatusUnableToProcess},
		{"whole", StatusSuccess},
	} {
		id := uint16(i + 1)
		sendStore(ctx, t, scu, ctxImplicit, id, c.instance, data, 16000, 1)
		if status, answers := responseStatus(ctx, t, scu); status != c.want || answers != id {
			t.Fatalf("%s: status 0x%04X answering %d, want 0x%04X answering %d", c.instance, status, answers, c.want, id)
		}
	}
	if calls.Load() != 3 {
		t.Errorf("the handler was called %d times, want 3", calls.Load())
	}
}

// streamOutcome is what a streaming handler saw: the error its reads ended
// with, and whether it returned.
type streamOutcome struct {
	read     int64
	err      error
	returned chan struct{}
}

// outcomeHandler copies the data set to WriteCStoreFile and reports how it
// ended. started is closed when the first byte has been read.
func outcomeHandler(out *streamOutcome, started chan struct{}) *streamHandler {
	out.returned = make(chan struct{})
	var once sync.Once
	return &streamHandler{fn: func(ctx context.Context, req *CStoreRequest, ts string, data io.Reader) (*CStoreResponse, error) {
		defer close(out.returned)
		n, err := WriteCStoreFile(ctx, io.Discard, req, ts, readerFunc(func(p []byte) (int, error) {
			n, err := data.Read(p)
			if n > 0 {
				once.Do(func() { close(started) })
			}
			return n, err
		}))
		out.read, out.err = n, err
		if err != nil {
			return nil, err
		}
		return &CStoreResponse{Status: StatusSuccess}, nil
	}}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// goroutinesSettle waits for the goroutine count to fall back to base.
func goroutinesSettle(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("%d goroutines, %d before:\n%s", runtime.NumGoroutine(), base, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestStreamedStoreEndsWithoutWhole: when a data set does not arrive whole,
// the handler's read fails, WriteCStoreFile returns that error rather than a
// count that looks complete, no response is sent, and no goroutine is left:
// the peer aborting midway, data on another presentation context, a command
// where data was due, more than MaxDataSetSize, a peer that goes quiet, and
// the server's context canceled.
func TestStreamedStoreEndsWithoutWhole(t *testing.T) {
	half := bytes.Repeat([]byte{0x5A}, 40_000)
	cases := []struct {
		name string
		cfg  NetworkConfig
		// then sends what follows the first half of the data set, or cancels.
		then func(ctx context.Context, scu *SCU, cancelServer context.CancelFunc)
		// peerSees is true when the SCP is expected to send an A-ABORT.
		peerSees bool
	}{
		{"peer aborts", NetworkConfig{}, func(ctx context.Context, scu *SCU, _ context.CancelFunc) {
			_ = scu.Association().transport.WritePDU(ctx, &AbortPDU{Source: AbortSourceServiceUser})
		}, false},
		{"data on another context", NetworkConfig{}, func(ctx context.Context, scu *SCU, _ context.CancelFunc) {
			_ = sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: ctxOther, IsLast: true, Data: []byte{1, 2}}}})
		}, true},
		{"a command where data was due", NetworkConfig{}, func(ctx context.Context, scu *SCU, _ context.CancelFunc) {
			_ = sendPDUs(ctx, scu, packed(fragmentsOf(ctxImplicit, true, storeCommand(9, "x"), 16000), 1))
		}, true},
		{"over MaxDataSetSize", NetworkConfig{MaxDataSetSize: 64 << 10}, func(ctx context.Context, scu *SCU, _ context.CancelFunc) {
			_ = sendPDUs(ctx, scu, packed(fragmentsOf(ctxImplicit, false, half, 16000), 1))
		}, true},
		{"peer goes quiet", NetworkConfig{DIMSETimeout: 300 * time.Millisecond}, func(context.Context, *SCU, context.CancelFunc) {}, true},
		{"server context canceled", NetworkConfig{}, func(_ context.Context, _ *SCU, cancelServer context.CancelFunc) {
			cancelServer()
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := runtime.NumGoroutine()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			serverCtx, cancelServer := context.WithCancel(ctx)
			defer cancelServer()

			var out streamOutcome
			started := make(chan struct{})
			server, scu := streamServer(t, serverCtx, c.cfg, outcomeHandler(&out, started))

			// The command, and the first half of the data set, not marked last.
			pdvs := fragmentsOf(ctxImplicit, true, storeCommand(1, "partial"), 16000)
			for _, f := range fragmentsOf(ctxImplicit, false, half, 16000) {
				f.IsLast = false
				pdvs = append(pdvs, f)
			}
			if err := sendPDUs(ctx, scu, packed(pdvs, 1)); err != nil {
				t.Fatal(err)
			}
			<-started
			c.then(ctx, scu, cancelServer)

			select {
			case <-out.returned:
			case <-time.After(10 * time.Second):
				t.Fatal("the handler never returned")
			}
			if out.err == nil {
				t.Fatalf("the handler read %d bytes of a data set that did not arrive whole, and no error", out.read)
			}
			if out.read >= int64(len(half)*2) {
				t.Errorf("read %d bytes", out.read)
			}
			t.Logf("the handler read %d bytes, then: %v", out.read, out.err)

			// What the peer gets is an A-ABORT or a closed connection; never
			// a C-STORE-RSP for an instance not received whole.
			readCtx, cancelRead := context.WithTimeout(ctx, 5*time.Second)
			defer cancelRead()
			for {
				pdu, err := scu.Association().transport.ReadPDU(readCtx)
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("the SCP neither aborted nor closed the connection")
					}
					break
				}
				if tf, ok := pdu.(*PDataTF); ok {
					t.Fatalf("the SCP answered a data set that did not arrive whole: %+v", tf.PDVItems)
				}
				if _, ok := pdu.(*AbortPDU); ok {
					if !c.peerSees {
						t.Error("the SCP sent an A-ABORT to a peer that had aborted")
					}
					break
				}
			}
			_ = scu.Association().transport.Close()
			server.Stop()
			goroutinesSettle(t, base)
		})
	}
}

// TestStreamedStoreIsInvalidAfterReturn: a reader kept past its handler's
// return fails rather than reading the association under the next message.
func TestStreamedStoreIsInvalidAfterReturn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kept := make(chan io.Reader, 1)
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		kept <- data
		return &CStoreResponse{Status: StatusSuccess}, nil
	}})
	sendStore(ctx, t, scu, ctxImplicit, 1, "kept", make([]byte, 1000), 16000, 1)
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
		t.Fatalf("status 0x%04X", status)
	}
	if _, err := (<-kept).Read(make([]byte, 10)); !errors.Is(err, errStreamClosed) {
		t.Fatalf("a read after the handler returned gave %v", err)
	}
}

// TestStreamedStoreWithoutDataSet: a C-STORE-RQ that says no data set follows
// is streamed as an empty one, and the next message is not taken for it.
func TestStreamedStoreWithoutDataSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sizes []int
	var mu sync.Mutex
	_, scu := streamServer(t, ctx, NetworkConfig{}, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		b, err := io.ReadAll(data)
		mu.Lock()
		sizes = append(sizes, len(b))
		mu.Unlock()
		return &CStoreResponse{Status: StatusSuccess}, err
	}})
	cmd := BuildCStoreRQ(1, CTImageStorageUID, "empty", PriorityMedium)
	addUSElement(cmd, tagCommandDataSetType, CommandDataSetTypeNull)
	encoded, err := EncodeCommandDataset(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err := scu.Association().SendPData(ctx, ctxImplicit, encoded, true); err != nil {
		t.Fatal(err)
	}
	if status, _ := responseStatus(ctx, t, scu); status != StatusSuccess {
		t.Fatalf("status 0x%04X", status)
	}
	sendStore(ctx, t, scu, ctxImplicit, 2, "next", make([]byte, 30), 16000, 1)
	if status, answers := responseStatus(ctx, t, scu); status != StatusSuccess || answers != 2 {
		t.Fatalf("status 0x%04X answering %d", status, answers)
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(sizes) != "[0 30]" {
		t.Fatalf("the handler read data sets of %v bytes, want [0 30]", sizes)
	}
}

// TestWriteCStoreFile: the File Meta Information is built from the command and
// the association, and a read error is returned, not a short file passed as
// whole.
func TestWriteCStoreFile(t *testing.T) {
	ctx := ContextWithAssociationInfo(context.Background(), &AssociationInfo{CallingAE: "SCANNER", CalledAE: "ARCHIVE"})
	req := &CStoreRequest{AffectedSOPClass: CTImageStorageUID, AffectedSOPInstance: "1.2.3.5"}
	data, err := EncodeDataset(syntheticInstance(t, "1.2.3.5", 10), ExplicitVRLittleEndianUID)
	if err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	n, err := WriteCStoreFile(ctx, &file, req, ExplicitVRLittleEndianUID, bytes.NewReader(data))
	if err != nil || n != int64(len(data)) {
		t.Fatalf("wrote %d of %d data set bytes: %v", n, len(data), err)
	}
	parsed, err := readFile(file.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	meta := parsed.FileMetaInfo
	for name, c := range map[string][2]string{
		"Media Storage SOP Class UID":    {meta.MediaStorageSOPClassUID, CTImageStorageUID},
		"Media Storage SOP Instance UID": {meta.MediaStorageSOPInstanceUID, "1.2.3.5"},
		"Transfer Syntax UID":            {meta.TransferSyntaxUID, ExplicitVRLittleEndianUID},
		"Implementation Class UID":       {meta.ImplementationClassUID, DefaultImplementationClassUID},
		"Sending AE Title":               {meta.SendingApplicationEntityTitle, "SCANNER"},
		"Receiving AE Title":             {meta.ReceivingApplicationEntityTitle, "ARCHIVE"},
	} {
		// An AE title of odd length is padded with a space, which the reader
		// keeps.
		if got := strings.TrimRight(c[0], " "); got != c[1] {
			t.Errorf("%s is %q, want %q", name, c[0], c[1])
		}
	}

	broken := io.MultiReader(bytes.NewReader(data[:20]), readerFunc(func([]byte) (int, error) {
		return 0, errors.New("association aborted")
	}))
	if n, err := WriteCStoreFile(ctx, io.Discard, req, ExplicitVRLittleEndianUID, broken); err == nil {
		t.Fatalf("a data set cut short after %d bytes was written with no error", n)
	}
}

// bigStoreSize is how much pixel data the memory test sends: 1 GiB, or
// GODICOM_STREAM_TEST_MB.
func bigStoreSize(t *testing.T) int64 {
	if mb, err := strconv.Atoi(os.Getenv("GODICOM_STREAM_TEST_MB")); err == nil && mb > 0 {
		return int64(mb) << 20
	}
	return 1 << 30
}

// sendBigStore sends a C-STORE of an Explicit VR Little Endian data set with
// size bytes of pixel data, generated as it is sent so the sender holds one
// PDU of it at a time.
func sendBigStore(ctx context.Context, scu *SCU, messageID uint16, size int64) error {
	cmd, err := EncodeCommandDataset(BuildCStoreRQ(messageID, CTImageStorageUID, "1.2.3.6", PriorityMedium))
	if err != nil {
		return err
	}
	if err := scu.Association().SendPData(ctx, ctxExplicit, cmd, true); err != nil {
		return err
	}
	var header bytes.Buffer
	_ = binary.Write(&header, binary.LittleEndian, [2]uint16{0x0008, 0x0016})
	header.WriteString("UI")
	_ = binary.Write(&header, binary.LittleEndian, uint16(len(CTImageStorageUID)+1))
	header.WriteString(CTImageStorageUID + "\x00")
	_ = binary.Write(&header, binary.LittleEndian, [2]uint16{0x7FE0, 0x0010})
	header.WriteString("OB\x00\x00")
	_ = binary.Write(&header, binary.LittleEndian, uint32(size))
	if err := sendPDUs(ctx, scu, [][]PDVItem{{{PresentationContextID: ctxExplicit, Data: header.Bytes()}}}); err != nil {
		return err
	}
	chunk := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 4000)
	for sent := int64(0); sent < size; {
		n := min(int64(len(chunk)), size-sent)
		sent += n
		pdv := PDVItem{PresentationContextID: ctxExplicit, IsLast: sent == size, Data: chunk[:n]}
		if err := sendPDUs(ctx, scu, [][]PDVItem{{pdv}}); err != nil {
			return err
		}
	}
	return nil
}

// peakHeap samples the heap in use until stop is closed and returns the most
// it saw.
func peakHeap(stop <-chan struct{}) <-chan uint64 {
	out := make(chan uint64, 1)
	go func() {
		var peak uint64
		var m runtime.MemStats
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			select {
			case <-stop:
				out <- peak
				return
			case <-tick.C:
			}
		}
	}()
	return out
}

// TestStreamedStoreMemoryIsBounded: an instance sixteen times the memory allowed
// is received whole by a streaming handler while the heap stays under the
// limit. The same instance received by HandleCStore passes the limit: that is
// this test failing on the existing path, and what shows the measurement
// would catch a stream that buffered.
func TestStreamedStoreMemoryIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("sends a gigabyte")
	}
	const limit = 64 << 20
	size := bigStoreSize(t)
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))

	receive := func(t *testing.T, handler Handler, size int64) uint64 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, scu := streamServer(t, ctx, NetworkConfig{}, handler)
		runtime.GC()
		stop := make(chan struct{})
		peak := peakHeap(stop)
		start := time.Now()
		if err := sendBigStore(ctx, scu, 1, size); err != nil {
			t.Fatal(err)
		}
		status, _ := responseStatus(ctx, t, scu)
		close(stop)
		got := <-peak
		t.Logf("%d MiB received in %v, status 0x%04X, peak heap %.1f MiB against a limit of %d MiB",
			size>>20, time.Since(start).Round(time.Millisecond), status, float64(got)/(1<<20), limit>>20)
		if status != StatusSuccess {
			t.Fatalf("status 0x%04X", status)
		}
		return got
	}

	t.Run("CStoreStreamer", func(t *testing.T) {
		var received atomic.Int64
		peak := receive(t, &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
			h := sha256.New()
			n, err := io.Copy(h, data)
			received.Store(n)
			return &CStoreResponse{Status: StatusSuccess}, err
		}}, size)
		if want := size + 12 + int64(len(CTImageStorageUID)+1) + 8; received.Load() != want {
			t.Errorf("the handler read %d bytes, want %d", received.Load(), want)
		}
		if peak > limit {
			t.Errorf("peak heap %.1f MiB, over the %d MiB limit", float64(peak)/(1<<20), limit>>20)
		}
	})

	t.Run("HandleCStore", func(t *testing.T) {
		// Twice the limit is enough to show it, and costs half the time.
		peak := receive(t, &StorageHandler{}, min(size, 2*limit))
		if peak <= limit {
			t.Errorf("peak heap %.1f MiB: receiving %d MiB whole should pass the %d MiB limit, so the measurement is not seeing what it measures",
				float64(peak)/(1<<20), min(size, 2*limit)>>20, limit>>20)
		}
	})
}

// BenchmarkReceiveCStore receives one instance per iteration over loopback,
// decoded for HandleCStore and streamed to a CStoreStreamer that hashes it.
// Allocations are the whole process's, the sending SCU's included, which is
// the same in both.
func BenchmarkReceiveCStore(b *testing.B) {
	for _, size := range []int64{1 << 20, 16 << 20} {
		for _, c := range []struct {
			name    string
			handler Handler
		}{
			{"HandleCStore", &StorageHandler{}},
			{"CStoreStreamer", &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
				_, err := io.Copy(sha256.New(), data)
				return &CStoreResponse{Status: StatusSuccess}, err
			}}},
		} {
			b.Run(fmt.Sprintf("%s/%dMiB", c.name, size>>20), func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				_, scu := streamServer(b, ctx, NetworkConfig{}, c.handler)
				b.SetBytes(size)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := sendBigStore(ctx, scu, uint16(i+1), size); err != nil {
						b.Fatal(err)
					}
					_, rsp, _, err := scu.Association().ReceivePData(ctx)
					if err != nil || len(rsp) == 0 {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
