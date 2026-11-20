package network_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// StoreStream sends the bytes a file holds. These send corpus files with it to
// dcmtk's storescp and to a pynetdicom SCP, neither of which shares code with
// this library, and compare the data set each stored with the file's own,
// byte for byte.

// storeStreamCorpus is a corpus file per family of transfer syntax a file is
// stored in: implicit, explicit, big endian, deflated-free encapsulated RLE,
// JPEG 2000 and JPEG-LS.
var storeStreamCorpus = []string{
	"MR_small_implicit.dcm",
	"CT_small.dcm",
	"MR_small_bigendian.dcm",
	"MR_small_RLE.dcm",
	"MR_small_jp2klossless.dcm",
	"MR_small_jpeg_ls_lossless.dcm",
	"JPEG-lossy.dcm",
}

// openStoreStream opens a corpus file and builds its request.
func openStoreStream(t *testing.T, path string) (network.StoreStreamRequest, []byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	req, err := network.StoreStreamRequestFromFile(f, st.Size())
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return req, file
}

// sendStream associates with addr proposing exactly the request's SOP class
// in exactly its transfer syntax, and sends it with StoreStream.
func sendStream(ctx context.Context, t *testing.T, addr, calledAE string, req network.StoreStreamRequest) (uint16, error) {
	t.Helper()
	scu := network.NewSCU(network.SCUConfig{CallingAE: "GO_SENDER", CalledAE: calledAE, Address: addr})
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: req.SOPClassUID, TransferSyntaxes: []string{req.TransferSyntaxUID},
	}}); err != nil {
		t.Fatalf("associate: %v", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	return scu.StoreStream(ctx, req)
}

// TestStoreStreamRequestFromFile: the request a Part 10 file gives is the
// file's own SOP class, instance and transfer syntax, and a reader over
// exactly the bytes after its File Meta Information, found by pydicom's reader
// as well as this library's. A file with no meta group is refused, as is one
// that is not DICOM.
func TestStoreStreamRequestFromFile(t *testing.T) {
	dir := corpusDir(t)
	requirePynetdicom(t)
	script := filepath.Join(t.TempDir(), "offset.py")
	const offsetScript = `
import sys
import pydicom
from pynetdicom.dsutils import split_dataset
meta, offset = split_dataset(sys.argv[1])
ds = pydicom.dcmread(sys.argv[1], stop_before_pixels=True)
print(offset, ds.file_meta.MediaStorageSOPClassUID, ds.file_meta.MediaStorageSOPInstanceUID, ds.file_meta.TransferSyntaxUID)
`
	if err := os.WriteFile(script, []byte(offsetScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range storeStreamCorpus {
		t.Run(name, func(t *testing.T) {
			req, file := openStoreStream(t, filepath.Join(dir, name))
			out, err := exec.Command("python3", script, filepath.Join(dir, name)).CombinedOutput()
			if err != nil {
				t.Fatalf("pydicom: %v\n%s", err, out)
			}
			var offset int
			var class, instance, syntax string
			if _, err := fmt.Sscan(string(out), &offset, &class, &instance, &syntax); err != nil {
				t.Fatalf("unexpected pydicom output %q", out)
			}
			if req.SOPClassUID != class || req.SOPInstanceUID != instance || req.TransferSyntaxUID != syntax {
				t.Errorf("request %s %s %s, pydicom reads %s %s %s", req.SOPClassUID, req.SOPInstanceUID,
					req.TransferSyntaxUID, class, instance, syntax)
			}
			got := new(bytes.Buffer)
			if _, err := got.ReadFrom(req.DataSet); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes(), file[offset:]) {
				t.Errorf("the data set is %d bytes, pydicom finds %d after the meta group at %d",
					got.Len(), len(file)-offset, offset)
			}
		})
	}

	for _, name := range []string{"ExplVR_BigEndNoMeta.dcm", "ExplVR_LitEndNoMeta.dcm"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		st, _ := f.Stat()
		if _, err := network.StoreStreamRequestFromFile(f, st.Size()); err == nil ||
			!strings.Contains(err.Error(), "File Meta Information") {
			t.Errorf("%s: error %v, want a refusal naming the missing File Meta Information", name, err)
		}
		_ = f.Close()
	}
	if _, err := network.StoreStreamRequestFromFile(strings.NewReader("not a DICOM file"), 16); err == nil {
		t.Error("a file that is not DICOM was accepted")
	}
}

// compareStoredScript compares the data set of the file dcmtk wrote with the
// source file's, after each one's File Meta Information as pynetdicom finds it.
const compareStoredScript = `
import sys
from pynetdicom.dsutils import split_dataset
_, so = split_dataset(sys.argv[1])
_, wo = split_dataset(sys.argv[2])
a = open(sys.argv[1], "rb").read()[so:]
b = open(sys.argv[2], "rb").read()[wo:]
assert a == b, "the data set differs: %d bytes sent, %d stored" % (len(a), len(b))
print("%s: %d data set bytes identical" % (sys.argv[1].rsplit("/", 1)[-1], len(a)))
`

// TestStoreStreamToDcmtk: dcmtk's storescp, writing each data set as it
// received it, stores the bytes of each corpus file's data set unchanged.
func TestStoreStreamToDcmtk(t *testing.T) {
	dir := corpusDir(t)
	storescp := dcmtkTool(t, "storescp")
	requirePynetdicom(t)
	script := filepath.Join(t.TempDir(), "compare.py")
	if err := os.WriteFile(script, []byte(compareStoredScript), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range storeStreamCorpus {
		t.Run(name, func(t *testing.T) {
			scp := bitPreservingStorescp(t, storescp)
			req, _ := openStoreStream(t, filepath.Join(dir, name))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			status, err := sendStream(ctx, t, "127.0.0.1:"+scp.port, "ARCHIVE", req)
			if err != nil || status != network.StatusSuccess {
				t.Fatalf("status 0x%04X, err %v", status, err)
			}
			var stored []string
			deadline := time.Now().Add(10 * time.Second)
			for len(stored) == 0 && time.Now().Before(deadline) {
				stored, _ = filepath.Glob(filepath.Join(scp.dir, "*"))
				if len(stored) == 0 {
					time.Sleep(20 * time.Millisecond)
				}
			}
			if len(stored) != 1 {
				t.Fatalf("storescp wrote %v, want one file", stored)
			}
			out, err := exec.Command("python3", script, filepath.Join(dir, name), stored[0]).CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			t.Logf("dcmtk storescp: %s", strings.TrimSpace(string(out)))
		})
	}
}

// pynetdicomStoreSCP is a pynetdicom SCP that writes the encoded data set of
// each C-STORE, as the bytes that arrived, to the path argument and answers
// with the status argument.
const pynetdicomStoreSCP = `
import sys, time
from pynetdicom import AE, evt, ALL_TRANSFER_SYNTAXES
from pynetdicom.sop_class import CTImageStorage, MRImageStorage, SecondaryCaptureImageStorage

out, status = sys.argv[1], int(sys.argv[2], 0)

def on_store(event):
    with open(out, "wb") as f:
        f.write(event.request.DataSet.getvalue())
    print("STORED %s %s" % (event.context.transfer_syntax, event.request.AffectedSOPInstanceUID), flush=True)
    return status

ae = AE(ae_title="PYSTORE")
ae.supported_contexts = []
for cls in (CTImageStorage, MRImageStorage, SecondaryCaptureImageStorage):
    ae.add_supported_context(cls, ALL_TRANSFER_SYNTAXES)
server = ae.start_server(("127.0.0.1", 0), block=False, evt_handlers=[(evt.EVT_C_STORE, on_store)])
print("PORT %d" % server.server_address[1], flush=True)
time.sleep(120)
`

// startPynetdicomStore runs pynetdicomStoreSCP and returns its address.
func startPynetdicomStore(t *testing.T, out, status string) string {
	t.Helper()
	source := strings.Replace(pynetdicomStoreSCP, "sys.argv[1]", quote(out), 1)
	source = strings.Replace(source, "int(sys.argv[2], 0)", status, 1)
	return startPynetdicomSCP(t, source)
}

// TestStoreStreamToPynetdicom: a pynetdicom SCP receives the bytes of each
// corpus file's data set exactly, in the transfer syntax of the file.
func TestStoreStreamToPynetdicom(t *testing.T) {
	dir := corpusDir(t)
	for _, name := range storeStreamCorpus {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "stored.bin")
			addr := startPynetdicomStore(t, out, "0")
			req, file := openStoreStream(t, filepath.Join(dir, name))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			status, err := sendStream(ctx, t, addr, "PYSTORE", req)
			if err != nil || status != network.StatusSuccess {
				t.Fatalf("status 0x%04X, err %v", status, err)
			}
			stored, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("pynetdicom stored nothing: %v", err)
			}
			if !bytes.HasSuffix(file, stored) || len(stored) == 0 {
				t.Errorf("pynetdicom stored %d bytes, which are not the end of the %d byte file", len(stored), len(file))
			}
			t.Logf("pynetdicom stored %d data set bytes identical to the file's, in %s", len(stored), req.TransferSyntaxUID)
		})
	}
}

// TestStoreStreamReportsAWarningFromPynetdicom: an SCP that stores the
// instance and answers 0xB000, coercion of data elements, gives StoreStream
// the status 0xB000 and a nil error: a warning is a stored instance.
func TestStoreStreamReportsAWarningFromPynetdicom(t *testing.T) {
	dir := corpusDir(t)
	for _, status := range []uint16{0xB000, 0xB006, 0xB007, 0xA700} {
		out := filepath.Join(t.TempDir(), "stored.bin")
		addr := startPynetdicomStore(t, out, fmt.Sprintf("0x%04X", status))
		req, _ := openStoreStream(t, filepath.Join(dir, "CT_small.dcm"))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		got, err := sendStream(ctx, t, addr, "PYSTORE", req)
		cancel()
		if err != nil || got != status {
			t.Errorf("pynetdicom answered 0x%04X: StoreStream returned 0x%04X, %v", status, got, err)
		}
	}
}
