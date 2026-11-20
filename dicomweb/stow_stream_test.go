package dicomweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
)

// An archive sending a study cannot hold it in memory to hand it to Store.
// StoreStream writes the request body as the instances are read, so these
// check what the server was sent, what the client is told, and what is held
// while it happens.

// readers returns a next function over the given files, and how many times
// each reader it returned was closed.
func readers(files ...[]byte) (func() (io.Reader, error), []*atomic.Int32) {
	closed := make([]*atomic.Int32, len(files))
	for i := range closed {
		closed[i] = new(atomic.Int32)
	}
	i := 0
	return func() (io.Reader, error) {
		if i == len(files) {
			return nil, io.EOF
		}
		r := &closingReader{Reader: bytes.NewReader(files[i]), closed: closed[i]}
		i++
		return r, nil
	}, closed
}

type closingReader struct {
	io.Reader
	closed *atomic.Int32
}

func (c *closingReader) Close() error { c.closed.Add(1); return nil }

// drainParts reads the parts of a store request, returning each part's bytes
// and the error that ended the multipart body, io.EOF when it ended well.
func drainParts(r *http.Request) (parts [][]byte, end error) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			return parts, err
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return parts, err
		}
		parts = append(parts, data)
	}
}

// TestStoreStreamSendsEachReaderAsAPart: the server receives one part for
// each reader, with the bytes the reader held, to the study named, with no
// Content-Length (the body is not known up front), and what StoreStream
// returns is what Store returns for the same request. Each reader is closed.
func TestStoreStreamSendsEachReaderAsAPart(t *testing.T) {
	files := [][]byte{part10(t, "1.2.3.1"), part10(t, "1.2.3.2"), part10(t, "1.2.3.3")}
	var received [][]byte
	var path string
	var length int64
	var end error
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, length = r.URL.Path, r.ContentLength
		received, end = drainParts(r)
		w.Header().Set("Content-Type", "application/dicom+json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, storeResponse)
	})

	next, closed := readers(files...)
	streamed, err := client.StoreStream(ctx(t), "1.2.3.100", next)
	if err != nil {
		t.Fatalf("StoreStream: %v", err)
	}
	if path != "/dicom-web/studies/1.2.3.100" || length != -1 || end != io.EOF {
		t.Errorf("path %s, content length %d, the body ended with %v", path, length, end)
	}
	if len(received) != len(files) {
		t.Fatalf("the server took %d parts, want %d", len(received), len(files))
	}
	for i := range files {
		if !bytes.Equal(received[i], files[i]) {
			t.Errorf("part %d is not the file sent", i)
		}
		if n := closed[i].Load(); n != 1 {
			t.Errorf("reader %d was closed %d times", i, n)
		}
	}

	whole, err := client.Store(ctx(t), "1.2.3.100", files...)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", streamed) != fmt.Sprintf("%+v", whole) {
		t.Errorf("StoreStream returned %+v, Store %+v", streamed, whole)
	}
}

// TestStoreStreamAgainstTheServer: this library's own server keeps every
// instance of a streamed request, and its account is the same as for a
// request Store sent.
func TestStoreStreamAgainstTheServer(t *testing.T) {
	files := [][]byte{part10(t, "1.2.3.1"), part10(t, "1.2.3.2")}
	backend := &recorder{}
	server := httptest.NewServer(NewServer(backend))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	next, _ := readers(files...)
	result, err := client.StoreStream(ctx(t), "", next)
	if err != nil {
		t.Fatalf("StoreStream: %v", err)
	}
	if len(backend.put) != 2 || len(result.Stored) != 2 || len(result.Failed) != 0 {
		t.Fatalf("the archive kept %d, the result lists %d stored and %d failed",
			len(backend.put), len(result.Stored), len(result.Failed))
	}
	whole, err := client.Store(ctx(t), "", files...)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%+v", result) != fmt.Sprintf("%+v", whole) {
		t.Errorf("StoreStream returned %+v, Store %+v", result, whole)
	}
}

// TestStoreStreamStopsOnASourceError: an error from next or from a reader
// ends the request without the closing boundary, so the server sees a body
// that is not well formed, and the caller gets that error, wrapped. An empty
// instance is refused as Store refuses it, and nothing to send sends nothing.
func TestStoreStreamStopsOnASourceError(t *testing.T) {
	boom := errors.New("the disk went away")
	good := part10(t, "1.2.3.1")

	type outcome struct {
		parts int
		end   error
	}
	seen := make(chan outcome, 1)
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		parts, end := drainParts(r)
		seen <- outcome{len(parts), end}
		w.WriteHeader(http.StatusBadRequest)
	})

	for _, c := range []struct {
		name  string
		next  func() (io.Reader, error)
		parts int
	}{
		{"next fails after one instance", inOrder(
			func() (io.Reader, error) { return bytes.NewReader(good), nil },
			func() (io.Reader, error) { return nil, boom }), 1},
		{"a reader fails partway", inOrder(
			func() (io.Reader, error) { return bytes.NewReader(good), nil },
			func() (io.Reader, error) {
				return io.MultiReader(bytes.NewReader(good[:100]), &errReader{boom}), nil
			}), 1},
		{"an empty instance", inOrder(
			func() (io.Reader, error) { return bytes.NewReader(good), nil },
			func() (io.Reader, error) { return bytes.NewReader(nil), nil }), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			result, err := client.StoreStream(ctx(t), "", c.next)
			if err == nil || result != nil {
				t.Fatalf("StoreStream returned %v, %v", result, err)
			}
			if c.name != "an empty instance" && !errors.Is(err, boom) {
				t.Errorf("the error is %v, want the source's", err)
			}
			select {
			case got := <-seen:
				if got.end == io.EOF {
					t.Errorf("the server was given a complete body of %d parts", got.parts)
				}
			case <-time.After(5 * time.Second):
				t.Error("the server never saw the request")
			}
		})
	}

	if _, err := client.StoreStream(ctx(t), "", func() (io.Reader, error) { return nil, io.EOF }); err == nil {
		t.Error("a store of nothing was sent")
	}
	if _, err := client.StoreStream(ctx(t), "", func() (io.Reader, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("a first instance that failed gave %v", err)
	}
}

// inOrder is a next function that calls each function once, in order, then
// returns io.EOF.
func inOrder(fns ...func() (io.Reader, error)) func() (io.Reader, error) {
	i := 0
	return func() (io.Reader, error) {
		if i == len(fns) {
			return nil, io.EOF
		}
		i++
		return fns[i-1]()
	}
}

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

// TestStoreStreamReportsAnEarlyRefusal: a server that refuses the request
// before it has read the body, as one does for a body over its limit, has the
// refusal returned as a *StatusError, not a failure of the instances, and the
// goroutine that was writing the body ends.
func TestStoreStreamReportsAnEarlyRefusal(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
	})
	before := runtime.NumGoroutine()
	big := bytes.Repeat([]byte{1}, 32<<20)
	_, err := client.StoreStream(ctx(t), "", inOrder(
		func() (io.Reader, error) { return bytes.NewReader(big), nil },
		func() (io.Reader, error) { return bytes.NewReader(big), nil }))
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("the error is %v, want a 413", err)
	}
	client.http.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines, %d before", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// generated returns a next function over count instances of size bytes of
// Pixel Data each, built one at a time as they are asked for, each with its
// own SOP Instance UID.
func generated(t *testing.T, count, size int) func() (io.Reader, error) {
	pixels := bytes.Repeat([]byte{0x5A}, size)
	i := 0
	return func() (io.Reader, error) {
		if i == count {
			return nil, io.EOF
		}
		ds := instance(t, fmt.Sprintf("1.2.826.0.1.3680043.10.511.%d", i))
		_ = ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OW, pixels))
		i++
		file, err := encodeFile(ds)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(file), nil
	}
}

// discardBackend is a Backend that counts what it is asked to keep and keeps
// none of it.
type discardBackend struct {
	recorder
	kept atomic.Int64
}

func (d *discardBackend) Put(context.Context, *dataset.Dataset) error {
	d.kept.Add(1)
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

// TestStoreStreamMemoryIsBounded: 2,000 instances of 1 MB each, 2 GB in all
// (200 under -short), are sent to this library's own server and to a server
// that only reads them, one request each, with the process held to a 64 MiB
// soft memory limit, which the collector keeps to by running more often. The
// peak heap stays under it, where a body built in memory would be 2 GB. The
// figures are logged.
func TestStoreStreamMemoryIsBounded(t *testing.T) {
	count := 2000
	if testing.Short() {
		count = 200
	}
	const size, limit = 1 << 20, 64 << 20
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))

	var counted atomic.Int64
	reading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextRawPart()
			if err != nil {
				break
			}
			_, _ = io.Copy(io.Discard, part)
			counted.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer reading.Close()
	backend := &discardBackend{}
	own := httptest.NewServer(NewServer(backend))
	defer own.Close()

	for _, c := range []struct {
		name, url string
		kept      func() int64
	}{
		{"a server that reads and drops", reading.URL, counted.Load},
		{"this library's server", own.URL, backend.kept.Load},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, err := NewClient(c.url)
			if err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			stop := make(chan struct{})
			peak := peakHeap(stop)
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_, err = client.StoreStream(ctx, "", generated(t, count, size))
			close(stop)
			heap := <-peak
			if err != nil {
				t.Fatal(err)
			}
			if got := c.kept(); got != int64(count) {
				t.Fatalf("the server took %d of %d instances", got, count)
			}
			t.Logf("%d instances of 1 MB in %v: peak heap %.1f MiB against a limit of %d MiB",
				count, time.Since(start).Round(time.Millisecond), float64(heap)/(1<<20), limit>>20)
			if heap > limit && !raceEnabled {
				t.Errorf("the heap peaked at %.1f MiB", float64(heap)/(1<<20))
			}
		})
	}
}
