package network_test

import (
	"context"
	"io"
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
