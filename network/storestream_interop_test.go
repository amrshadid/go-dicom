package network_test

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// A streamed C-STORE is only worth having if what is written is what the peer
// sent. These have dcmtk and pynetdicom send corpus files to an SCP whose
// handler writes each data set with WriteCStoreFile, then compare the written
// file's data set with the original's byte for byte, and read the written file
// with dcmtk's dcmdump and with pydicom, which share no code with this
// library. The SCP advertises a 4 KiB maximum PDU, so each instance arrives in
// many small PDUs.

// corpusDir returns the pydicom test corpus, from GODICOM_PYDICOM_DATA.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
			t.Fatal("GODICOM_REQUIRE_INTEROP is set but GODICOM_PYDICOM_DATA is not")
		}
		t.Skip("GODICOM_PYDICOM_DATA is not set; skipping the corpus-backed interoperability check")
	}
	return dir
}

// One corpus file per family of transfer syntax, with the storescu option
// that proposes it.
var streamedCorpus = []struct {
	file, storescuOption string
}{
	{"MR_small_implicit.dcm", "-xi"}, // implicit VR little endian
	{"CT_small.dcm", "-xe"},          // explicit VR little endian
	{"MR_small_RLE.dcm", "-xr"},      // RLE lossless, encapsulated
}

// fileWriter is a CStoreStreamer that writes each instance to dir, named for
// its SOP Instance UID, and keeps what writing it returned.
type fileWriter struct {
	network.BaseHandler
	dir  string
	mu   sync.Mutex
	errs []error
}

func (h *fileWriter) HandleCStoreStream(ctx context.Context, req *network.CStoreRequest, ts string,
	data io.Reader) (*network.CStoreResponse, error) {
	f, err := os.Create(filepath.Join(h.dir, req.AffectedSOPInstance+".dcm"))
	if err == nil {
		_, err = network.WriteCStoreFile(ctx, f, req, ts, data)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		h.mu.Lock()
		h.errs = append(h.errs, err)
		h.mu.Unlock()
		return nil, err
	}
	return &network.CStoreResponse{Status: network.StatusSuccess}, nil
}

// streamingArchive starts an SCP with a fileWriter, accepting every transfer
// syntax, and returns its host, port and the writer.
func streamingArchive(t *testing.T) (string, string, *fileWriter) {
	t.Helper()
	writer := &fileWriter{dir: t.TempDir()}
	server, err := network.StartServer(context.Background(), network.SCPConfig{
		AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		Network: network.NetworkConfig{MaxPDUSize: 4096},
	}, writer)
	if err != nil {
		t.Fatal(err)
	}
	server.SetSupportedTransferSyntaxes(network.AllTransferSyntaxes())
	t.Cleanup(server.Stop)
	host, port, _ := strings.Cut(server.Addr(), ":")
	return host, port, writer
}

// compareScript compares the file a peer sent, or a faithful copy of it, with
// the file written here: the data set bytes after each one's File Meta
// Information, and the written file as pydicom reads it, its meta, its
// elements and its pixels against the other's.
const compareScript = `
import sys
import numpy as np
import pydicom
from pynetdicom.dsutils import split_dataset

for original, written in [(sys.argv[1], sys.argv[2])]:
    om, oo = split_dataset(original)
    wm, wo = split_dataset(written)
    a = open(original, "rb").read()[oo:]
    b = open(written, "rb").read()[wo:]
    assert a == b, "%s: the data set differs: %d bytes sent, %d written" % (original, len(a), len(b))
    o = pydicom.dcmread(original)
    w = pydicom.dcmread(written)
    assert w.file_meta.TransferSyntaxUID == o.file_meta.TransferSyntaxUID, w.file_meta.TransferSyntaxUID
    assert w.file_meta.MediaStorageSOPInstanceUID == o.SOPInstanceUID
    assert w.file_meta.MediaStorageSOPClassUID == o.SOPClassUID
    for elem in o:
        assert w[elem.tag].value == elem.value, elem.tag
    assert np.array_equal(w.pixel_array, o.pixel_array)
    print("%s: %d data set bytes identical, transfer syntax %s, pixels equal (pydicom %s)" % (
        original.rsplit("/", 1)[-1], len(b), w.file_meta.TransferSyntaxUID.name, pydicom.__version__))
`

// checkWritten compares a sent corpus file with the one file the SCP wrote,
// with pydicom and dcmdump.
func checkWritten(t *testing.T, original string, writer *fileWriter) {
	t.Helper()
	if len(writer.errs) > 0 {
		t.Fatalf("the handler failed: %v", writer.errs)
	}
	written, err := filepath.Glob(filepath.Join(writer.dir, "*.dcm"))
	if err != nil || len(written) != 1 {
		t.Fatalf("the SCP wrote %v (%v), want one file", written, err)
	}
	dcmdump := dcmtkTool(t, "dcmdump")
	out, err := exec.Command(dcmdump, "-M", "+P", "0002,0010", "+P", "0008,0018", written[0]).CombinedOutput()
	if err != nil || !strings.Contains(string(out), filepath.Base(strings.TrimSuffix(written[0], ".dcm"))) {
		t.Fatalf("dcmdump does not read %s: %v\n%s", written[0], err, out)
	}
	t.Logf("dcmdump:\n%s", strings.TrimSpace(string(out)))

	script := filepath.Join(t.TempDir(), "compare.py")
	if err := os.WriteFile(script, []byte(compareScript), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command("python3", script, original, written[0]).CombinedOutput()
	if err != nil {
		t.Fatalf("pydicom: %v\n%s", err, out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}

// TestDcmtkStoresAStreamedFile: storescu sends each corpus file. storescu
// encodes the data set itself as it sends it, without the group lengths the
// corpus files carry, so what it sent is taken from dcmtk's own storescp
// running bit-preserving (+B), which writes the data set as received: the file
// written here must hold the same bytes.
func TestDcmtkStoresAStreamedFile(t *testing.T) {
	dir := corpusDir(t)
	storescu := dcmtkTool(t, "storescu")
	storescp := dcmtkTool(t, "storescp")
	requirePynetdicom(t) // pydicom and pynetdicom check what was written
	for _, c := range streamedCorpus {
		t.Run(c.file, func(t *testing.T) {
			original := filepath.Join(dir, c.file)
			send := func(host, port string) {
				t.Helper()
				out, err := exec.Command(storescu, c.storescuOption, "-aet", "SCANNER", "-aec", "ARCHIVE",
					host, port, original).CombinedOutput()
				if err != nil {
					t.Fatalf("storescu %s %s: %v\n%s", c.storescuOption, c.file, err, out)
				}
			}

			reference := bitPreservingStorescp(t, storescp)
			send("127.0.0.1", reference.port)
			sent, err := filepath.Glob(filepath.Join(reference.dir, "*"))
			if err != nil || len(sent) != 1 {
				t.Fatalf("storescp wrote %v (%v), want one file", sent, err)
			}

			host, port, writer := streamingArchive(t)
			send(host, port)
			checkWritten(t, sent[0], writer)
		})
	}
}

// dcmtkSCP is a running dcmtk storescp: its port and where it writes.
type dcmtkSCP struct{ port, dir string }

// bitPreservingStorescp starts dcmtk's storescp writing each data set exactly
// as received, and waits until it accepts connections.
func bitPreservingStorescp(t *testing.T, storescp string) dcmtkSCP {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()
	scp := dcmtkSCP{port: port, dir: t.TempDir()}
	cmd := exec.Command(storescp, "+B", "-aet", "ARCHIVE", "-od", scp.dir, "-pm", "+xa", port)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("tcp", "127.0.0.1:"+port)
		if err == nil {
			conn.Close()
			return scp
		}
		if time.Now().After(deadline) {
			t.Fatalf("storescp never listened on %s: %v", port, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// sendScript sends one file with pynetdicom, from its path, which pynetdicom
// sends without decoding.
const sendScript = `
import sys
from pydicom.uid import ImplicitVRLittleEndian, ExplicitVRLittleEndian, RLELossless
from pynetdicom import AE
from pynetdicom.sop_class import CTImageStorage, MRImageStorage

ae = AE(ae_title="SCANNER")
for cls in (CTImageStorage, MRImageStorage):
    for ts in (ImplicitVRLittleEndian, ExplicitVRLittleEndian, RLELossless):
        ae.add_requested_context(cls, ts)
assoc = ae.associate(sys.argv[1], int(sys.argv[2]), ae_title="ARCHIVE")
assert assoc.is_established
status = assoc.send_c_store(sys.argv[3])
print("pynetdicom sent %s: status 0x%04X" % (sys.argv[3].rsplit("/", 1)[-1], status.Status))
assert status.Status == 0
assoc.release()
`

// TestPynetdicomStoresAStreamedFile: pynetdicom sends each corpus file; what is
// written is what it sent.
func TestPynetdicomStoresAStreamedFile(t *testing.T) {
	dir := corpusDir(t)
	requirePynetdicom(t)
	script := filepath.Join(t.TempDir(), "send.py")
	if err := os.WriteFile(script, []byte(sendScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range streamedCorpus {
		t.Run(c.file, func(t *testing.T) {
			host, port, writer := streamingArchive(t)
			original := filepath.Join(dir, c.file)
			out, err := exec.Command("python3", script, host, port, original).CombinedOutput()
			if err != nil {
				t.Fatalf("pynetdicom: %v\n%s", err, out)
			}
			t.Logf("%s", strings.TrimSpace(string(out)))
			checkWritten(t, original, writer)
		})
	}
}
