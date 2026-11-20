package network_test

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// largeCTScript writes CT_small.dcm from the corpus again with 96 MiB of
// Pixel Data (three 4096 x 4096 frames of 16 bits), large enough that a send
// to a server advertising 4 KiB PDUs is still under way when it is told to
// stop.
const largeCTScript = `
import sys
import pydicom
ds = pydicom.dcmread(sys.argv[1])
ds.Rows, ds.Columns, ds.NumberOfFrames = 4096, 4096, 3
ds.BitsAllocated, ds.BitsStored, ds.HighBit = 16, 16, 15
ds.PixelData = bytes(range(256)) * (4096 * 4096 * 3 * 2 // 256)
ds.save_as(sys.argv[2])
`

// startedWriter is a fileWriter that says when a store has begun.
type startedWriter struct {
	*fileWriter
	started chan struct{}
}

func (h *startedWriter) HandleCStoreStream(ctx context.Context, req *network.CStoreRequest, ts string,
	data io.Reader) (*network.CStoreResponse, error) {
	h.started <- struct{}{}
	return h.fileWriter.HandleCStoreStream(ctx, req, ts, data)
}

// TestDcmtkStoreFinishesWhileTheServerShutsDown: dcmtk's storescu sends a
// 96 MiB instance, and the server is told to Shutdown as soon as the store
// begins. storescu is answered Success and releases, the whole data set is
// written, and Shutdown returns nil: nothing was cut short. Stop, in the same
// place, failed the store (#464).
func TestDcmtkStoreFinishesWhileTheServerShutsDown(t *testing.T) {
	dir := corpusDir(t)
	storescu := dcmtkTool(t, "storescu")
	requirePynetdicom(t)

	large := filepath.Join(t.TempDir(), "large.dcm")
	if out, err := exec.Command("python3", "-c", largeCTScript, filepath.Join(dir, "CT_small.dcm"), large).CombinedOutput(); err != nil {
		t.Fatalf("writing the large instance: %v\n%s", err, out)
	}

	writer := &startedWriter{fileWriter: &fileWriter{dir: t.TempDir()}, started: make(chan struct{}, 1)}
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		Network: network.NetworkConfig{MaxPDUSize: 4096},
	}, writer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	host, port, _ := strings.Cut(server.Addr(), ":")

	cmd := exec.Command(storescu, "-v", "-xe", "-aet", "SCANNER", "-aec", "ARCHIVE", host, port, large)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the store never began")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stopErr := server.Shutdown(ctx)
	runErr := cmd.Wait()
	var lines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "XMIT") {
			lines = append(lines, line)
		}
	}
	t.Logf("storescu, its progress line left out:\n%s", strings.TrimSpace(strings.Join(lines, "\n")))
	if stopErr != nil {
		t.Errorf("Shutdown returned %v, want nil", stopErr)
	}
	if runErr != nil || !strings.Contains(out.String(), "Received Store Response (Success)") ||
		!strings.Contains(out.String(), "Releasing Association") {
		t.Errorf("storescu: %v; want the store answered Success and the association released", runErr)
	}
	if len(writer.errs) > 0 {
		t.Fatalf("the handler failed: %v", writer.errs)
	}
	written, _ := filepath.Glob(filepath.Join(writer.dir, "*.dcm"))
	if len(written) != 1 {
		t.Fatalf("the server wrote %v, want one file", written)
	}
	info, err := os.Stat(written[0])
	if err != nil || info.Size() < 96<<20 {
		t.Errorf("the written file is %v bytes (%v), want the whole 96 MiB data set", info.Size(), err)
	}
}

// refusedScript associates as pynetdicom, which does not retry, and prints how
// the association was answered: the result, source and reason of the
// A-ASSOCIATE-RJ as pynetdicom read them.
const refusedScript = `
import sys
from pynetdicom import AE
from pynetdicom.sop_class import CTImageStorage

ae = AE(ae_title="SCANNER")
ae.add_requested_context(CTImageStorage)
assoc = ae.associate(sys.argv[1], int(sys.argv[2]), ae_title="ARCHIVE")
p = assoc.acceptor.primitive
print("rejected=%s result=%s source=%s diagnostic=%s" % (assoc.is_rejected, p.result, p.result_source, p.diagnostic))
`

// blockedReader yields a few bytes and then waits to be told to end.
type blockedReader struct {
	first   []byte
	release chan struct{}
}

func (r *blockedReader) Read(p []byte) (int, error) {
	if len(r.first) > 0 {
		n := copy(p, r.first)
		r.first = r.first[n:]
		return n, nil
	}
	<-r.release
	return 0, io.EOF
}

// TestPynetdicomIsRefusedTransientlyDuringShutdown: a store is in flight when
// Shutdown begins, and pynetdicom, requesting an association during the drain,
// is accepted by the listener and answered with an A-ASSOCIATE-RJ rejected
// transient (2), service-provider presentation (3), temporary congestion (1),
// as PS3.8 Table 9-21 numbers them. It was refused at the TCP level. The store
// finishes, Shutdown returns nil, and the listener is then closed.
func TestPynetdicomIsRefusedTransientlyDuringShutdown(t *testing.T) {
	requirePynetdicom(t)
	writer := &startedWriter{fileWriter: &fileWriter{dir: t.TempDir()}, started: make(chan struct{}, 1)}
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
	}, writer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	addr := server.Addr()
	host, port, _ := strings.Cut(addr, ":")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	scu := network.NewSCU(network.SCUConfig{CallingAE: "SENDER", CalledAE: "ARCHIVE", Address: addr})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: network.CTImageStorageUID, TransferSyntaxes: []string{network.ExplicitVRLittleEndianUID},
	}}); err != nil {
		t.Fatal(err)
	}
	reader := &blockedReader{first: []byte("not a whole data set"), release: make(chan struct{})}
	sent := make(chan error, 1)
	go func() {
		_, err := scu.StoreStream(ctx, network.StoreStreamRequest{
			SOPClassUID: network.CTImageStorageUID, SOPInstanceUID: "1.2.3.4",
			TransferSyntaxUID: network.ExplicitVRLittleEndianUID, DataSet: reader,
		})
		sent <- err
	}()
	select {
	case <-writer.started:
	case <-time.After(30 * time.Second):
		t.Fatal("the store never began")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- server.Shutdown(ctx) }()
	// Shutdown has begun once a new caller is told to come back; until then
	// the server is still taking associations, so the script is run again.
	var out []byte
	for deadline := time.Now().Add(30 * time.Second); ; {
		out, err = exec.Command("python3", "-c", refusedScript, host, port).CombinedOutput()
		if err != nil {
			t.Fatalf("pynetdicom: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "rejected=True") || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("pynetdicom, requesting an association during the drain: %s", strings.TrimSpace(string(out)))
	want := "rejected=True result=2 source=3 diagnostic=1"
	if !strings.Contains(string(out), want) {
		t.Errorf("pynetdicom read %q, want %q", strings.TrimSpace(string(out)), want)
	}

	close(reader.release)
	<-sent
	_ = scu.Release(ctx)
	if err := <-stopped; err != nil {
		t.Errorf("Shutdown returned %v, want nil", err)
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("the listener was left open after the drain")
	}
}
