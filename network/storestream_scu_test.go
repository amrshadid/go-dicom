package network

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A program that keeps instances as files and sends what it stored needs the
// bytes to go out as they are, from a reader, in bounded memory. These tests
// send to this library's own SCP, whose streaming handler sees the data set
// exactly as it arrived; the interoperability tests send to dcmtk and
// pynetdicom.

// sinkHandler is a CStoreStreamer that keeps what each C-STORE delivered, how
// the read of it ended, and the status to answer.
type sinkHandler struct {
	BaseHandler
	status uint16

	mu     sync.Mutex
	data   [][]byte
	ts     []string
	endErr []error
	calls  atomic.Int32
}

func (h *sinkHandler) HandleCStoreStream(_ context.Context, _ *CStoreRequest, ts string,
	data io.Reader) (*CStoreResponse, error) {
	h.calls.Add(1)
	got, err := io.ReadAll(data)
	h.mu.Lock()
	h.data, h.ts, h.endErr = append(h.data, got), append(h.ts, ts), append(h.endErr, err)
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &CStoreResponse{Status: h.status}, nil
}

// patternReader is n bytes of a repeating pattern, generated as they are read.
type patternReader struct {
	n, at int64
}

func (p *patternReader) Read(b []byte) (int, error) {
	if p.at >= p.n {
		return 0, io.EOF
	}
	if rest := p.n - p.at; int64(len(b)) > rest {
		b = b[:rest]
	}
	for i := range b {
		b[i] = byte((p.at + int64(i)) * 31)
	}
	p.at += int64(len(b))
	return len(b), nil
}

// failingReader yields n bytes of the pattern, then err instead of io.EOF.
type failingReader struct {
	patternReader
	err error
}

func (f *failingReader) Read(b []byte) (int, error) {
	if f.at >= f.n {
		return 0, f.err
	}
	return f.patternReader.Read(b)
}

// TestStoreStreamSendsTheBytesGiven: the data set the peer receives is the
// reader's bytes, byte for byte and in the transfer syntax named, for lengths
// that fall on, just under and just over a fragment (the peer's Maximum Length
// is 4096, so 4084 bytes of data fit a PDU), for an empty data set, and for
// one many fragments long.
func TestStoreStreamSendsTheBytesGiven(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sink := &sinkHandler{}
	_, scu := streamServer(t, ctx, NetworkConfig{MaxPDUSize: 4096}, sink)

	for _, n := range []int64{0, 1, 4083, 4084, 4085, 4084 * 3, 4084*3 + 1, 300_000} {
		want, _ := io.ReadAll(&patternReader{n: n})
		status, err := scu.StoreStream(ctx, StoreStreamRequest{
			SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3.4",
			TransferSyntaxUID: ExplicitVRLittleEndianUID, DataSet: &patternReader{n: n},
		})
		if err != nil || status != StatusSuccess {
			t.Fatalf("%d bytes: status 0x%04X, err %v", n, status, err)
		}
		sink.mu.Lock()
		got, ts, endErr := sink.data[len(sink.data)-1], sink.ts[len(sink.ts)-1], sink.endErr[len(sink.endErr)-1]
		sink.mu.Unlock()
		if endErr != nil || !bytes.Equal(got, want) {
			t.Errorf("%d bytes: the peer got %d bytes (end: %v), not the same bytes", n, len(got), endErr)
		}
		if ts != ExplicitVRLittleEndianUID {
			t.Errorf("%d bytes: arrived on %s, want the transfer syntax named", n, ts)
		}
	}
}

// TestStoreStreamReturnsThePeersStatus: success, each warning and a failure
// all come back as the status with a nil error, since each is an answer. A
// warning is not turned into an error as Store's failure statuses are.
func TestStoreStreamReturnsThePeersStatus(t *testing.T) {
	for _, want := range []uint16{StatusSuccess, 0xB000, 0xB006, 0xB007, 0xA700, 0xC000} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, scu := streamServer(t, ctx, NetworkConfig{}, &sinkHandler{status: want})
		status, err := scu.StoreStream(ctx, StoreStreamRequest{
			SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3.4",
			TransferSyntaxUID: ImplicitVRLittleEndianUID, DataSet: &patternReader{n: 100},
		})
		if err != nil || status != want {
			t.Errorf("peer answered 0x%04X: StoreStream returned 0x%04X, %v", want, status, err)
		}
		cancel()
	}
}

// TestStoreStreamUsesOnlyTheExactContext: when no accepted context has the
// SOP class in exactly the transfer syntax, the error names both and nothing
// is sent, even though another syntax for the class was accepted and a
// transcoding sender would have used it. Where the class has several accepted
// syntaxes, the one named is the one used.
func TestStoreStreamUsesOnlyTheExactContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sink := &sinkHandler{}
	_, scu := streamServer(t, ctx, NetworkConfig{}, sink)

	for _, c := range []struct{ class, syntax string }{
		{CTImageStorageUID, JPEGBaselineUID},                       // accepted for no class here
		{MRImageStorageUID, ExplicitVRLittleEndianUID},             // MR was proposed in Implicit only
		{"1.2.840.10008.5.1.4.1.1.6.1", ImplicitVRLittleEndianUID}, // never proposed
	} {
		_, err := scu.StoreStream(ctx, StoreStreamRequest{
			SOPClassUID: c.class, SOPInstanceUID: "1.2.3", TransferSyntaxUID: c.syntax,
			DataSet: &patternReader{n: 10},
		})
		if err == nil || !strings.Contains(err.Error(), c.class) || !strings.Contains(err.Error(), c.syntax) {
			t.Errorf("%s in %s: error %v, want one naming both", c.class, c.syntax, err)
		}
	}
	if n := sink.calls.Load(); n != 0 {
		t.Errorf("the peer's handler was called %d times for contexts that were never accepted", n)
	}

	// The association is intact after a refusal that sent nothing.
	if status, err := scu.StoreStream(ctx, StoreStreamRequest{
		SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3", TransferSyntaxUID: RLELosslessUID,
		DataSet: &patternReader{n: 10},
	}); err != nil || status != StatusSuccess {
		t.Errorf("a store after the refusals: 0x%04X, %v", status, err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.ts) != 1 || sink.ts[0] != RLELosslessUID {
		t.Errorf("syntaxes the peer received on: %v, want only %s", sink.ts, RLELosslessUID)
	}
}

// TestStoreStreamAbortsWhenTheReaderFails: a reader that fails after sending
// part of the data set gets an A-ABORT and not a last fragment, so the peer's
// handler sees the data set end unexpectedly, never io.EOF, and no C-STORE is
// answered or kept. StoreStream returns the reader's error. The peer's
// handler is also never called for a plain HandleCStore handler, which is only
// called with a whole data set.
func TestStoreStreamAbortsWhenTheReaderFails(t *testing.T) {
	boom := errors.New("the disk went away")
	for _, c := range []struct {
		name string
		n    int64
	}{
		{"before the first fragment is full", 100},
		{"on a fragment boundary", 4084 * 5},
		{"many fragments in", 200_000},
		{"before any byte", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			sink := &sinkHandler{}
			_, scu := streamServer(t, ctx, NetworkConfig{MaxPDUSize: 4096}, sink)

			status, err := scu.StoreStream(ctx, StoreStreamRequest{
				SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3", TransferSyntaxUID: ExplicitVRLittleEndianUID,
				DataSet: &failingReader{patternReader{n: c.n}, boom},
			})
			if !errors.Is(err, boom) || status != 0 {
				t.Fatalf("StoreStream returned 0x%04X, %v; want the reader's error", status, err)
			}
			if st := scu.Association().State(); st == StateAssociated {
				t.Error("the association is still up after an abort")
			}

			// The handler is called once the command has arrived, and its read ends
			// with the abort: waited for rather than slept for.
			deadline := time.Now().Add(10 * time.Second)
			for sink.calls.Load() == 0 || func() bool { sink.mu.Lock(); defer sink.mu.Unlock(); return len(sink.endErr) == 0 }() {
				if time.Now().After(deadline) {
					t.Fatal("the peer's handler never finished reading")
				}
				time.Sleep(5 * time.Millisecond)
			}
			sink.mu.Lock()
			defer sink.mu.Unlock()
			if err := sink.endErr[0]; !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				t.Errorf("the peer's read ended with %v, want an unexpected end that is not io.EOF", err)
			}
			if !strings.Contains(sink.endErr[0].Error(), "abort") {
				t.Errorf("the peer was not told of an abort: %v", sink.endErr[0])
			}
		})
	}
}

// TestStoreStreamNeverCompletesADamagedDataSet: with a plain handler, the one
// that is given only whole data sets, a reader that fails never reaches it.
func TestStoreStreamNeverCompletesADamagedDataSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var calls atomic.Int32
	handler := &countingStoreHandler{calls: &calls}
	server, scu := streamServer(t, ctx, NetworkConfig{MaxPDUSize: 4096}, handler)

	_, err := scu.StoreStream(ctx, StoreStreamRequest{
		SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3", TransferSyntaxUID: ExplicitVRLittleEndianUID,
		DataSet: &failingReader{patternReader{n: 50_000}, io.ErrClosedPipe},
	})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("StoreStream returned %v, want the reader's error", err)
	}
	// The server ends the association once it has read the abort: Stop waits
	// for its goroutines, so the handler has had every chance to run.
	server.Stop()
	if n := calls.Load(); n != 0 {
		t.Errorf("the handler was called %d times with a data set the sender cut short", n)
	}
}

// countingStoreHandler is a plain C-STORE handler that counts its calls.
type countingStoreHandler struct {
	BaseHandler
	calls *atomic.Int32
}

func (h *countingStoreHandler) HandleCStore(context.Context, *CStoreRequest) (*CStoreResponse, error) {
	h.calls.Add(1)
	return &CStoreResponse{Status: StatusSuccess}, nil
}

// TestStoreStreamStopsWhenContextEnds: a canceled context partway ends the
// send with an abort, so the peer does not wait on the rest, and the error is
// the context's.
func TestStoreStreamStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sink := &sinkHandler{}
	_, scu := streamServer(t, ctx, NetworkConfig{MaxPDUSize: 4096}, sink)

	sendCtx, stop := context.WithCancel(ctx)
	reader := &cancelingReader{patternReader: patternReader{n: 1 << 30}, after: 200_000, cancel: stop}
	_, err := scu.StoreStream(sendCtx, StoreStreamRequest{
		SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3", TransferSyntaxUID: ExplicitVRLittleEndianUID,
		DataSet: reader,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StoreStream returned %v, want context.Canceled", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		sink.mu.Lock()
		done := len(sink.endErr) > 0
		sink.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the peer never saw the data set end")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := sink.endErr[0]; !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("the peer's read ended with %v", err)
	}
}

// cancelingReader cancels a context once after bytes have been read.
type cancelingReader struct {
	patternReader
	after  int64
	cancel context.CancelFunc
}

func (c *cancelingReader) Read(b []byte) (int, error) {
	n, err := c.patternReader.Read(b)
	if c.at >= c.after {
		c.cancel()
	}
	return n, err
}

// TestStoreStreamMemoryDoesNotGrowWithTheDataSet: a data set generated as it
// is sent, 512 MB of it (GODICOM_STREAM_TEST_MB changes that; the race
// detector's build sends 128 MB and -short 32 MB), reaches the peer whole, with both ends in one process held to a
// 64 MiB soft memory limit, which the collector keeps to by running more
// often, and the peak live heap stays under it: sixteen times less than the
// data set at the full size. Memory that followed the size would pass the
// limit; the figure is logged.
func TestStoreStreamMemoryDoesNotGrowWithTheDataSet(t *testing.T) {
	const limit = 64 << 20
	size := int64(512 << 20)
	switch {
	case testing.Short():
		size = 32 << 20
	case raceEnabled:
		size = 128 << 20
	case os.Getenv("GODICOM_STREAM_TEST_MB") != "":
		size = bigStoreSize(t)
	}
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var sum [sha256.Size]byte
	var got int64
	hashing := &streamHandler{fn: func(_ context.Context, _ *CStoreRequest, _ string, data io.Reader) (*CStoreResponse, error) {
		h := sha256.New()
		n, err := io.Copy(h, data)
		got = n
		copy(sum[:], h.Sum(nil))
		if err != nil {
			return nil, err
		}
		return &CStoreResponse{Status: StatusSuccess}, nil
	}}
	_, scu := streamServer(t, ctx, NetworkConfig{}, hashing)

	want := sha256.New()
	if _, err := io.Copy(want, &patternReader{n: size}); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	stop := make(chan struct{})
	peak := peakHeap(stop)
	start := time.Now()
	status, err := scu.StoreStream(ctx, StoreStreamRequest{
		SOPClassUID: CTImageStorageUID, SOPInstanceUID: "1.2.3", TransferSyntaxUID: ExplicitVRLittleEndianUID,
		DataSet: &patternReader{n: size},
	})
	close(stop)
	heap := <-peak
	if err != nil || status != StatusSuccess {
		t.Fatalf("status 0x%04X, err %v", status, err)
	}
	if got != size || !bytes.Equal(sum[:], want.Sum(nil)) {
		t.Fatalf("the peer received %d of %d bytes, or not the same bytes", got, size)
	}
	t.Logf("%d MiB sent in %v; peak heap %.1f MiB against a limit of %d MiB",
		size>>20, time.Since(start).Round(time.Millisecond), float64(heap)/(1<<20), limit>>20)
	if overHeapLimit(t, heap, limit) {
		t.Errorf("the heap peaked at %.1f MiB sending %d MiB, over %d MiB", float64(heap)/(1<<20), size>>20, limit>>20)
	}
}
