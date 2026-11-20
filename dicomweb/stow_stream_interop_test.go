package dicomweb

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// No STOW-RS server of another project is available here: dcmtk has none and
// pynetdicom is DIMSE only. What can be checked against an implementation that
// shares nothing with this one is the request itself, a chunked
// multipart/related body written through a pipe, which is read by aiohttp's
// HTTP and multipart parsers, each part is read by pydicom, and the bytes
// compared.

// stowReceiver is an aiohttp server that answers a store request the way
// PS3.18 10.5.3 says, after reading it with aiohttp's multipart parser and
// pydicom, and prints one JSON line per request: what it saw.
const stowReceiver = `
import io, sys, json, hashlib
from aiohttp import web
import pydicom

async def stow(request):
    reader = await request.multipart()
    seen, refs = [], []
    while True:
        part = await reader.next()
        if part is None:
            break
        data = await part.read(decode=False)
        ds = pydicom.dcmread(io.BytesIO(data))
        seen.append({"type": part.headers.get("Content-Type"), "sha256": hashlib.sha256(data).hexdigest(),
                     "sop": str(ds.SOPInstanceUID), "length": len(data)})
        refs.append({"00081150": {"vr": "UI", "Value": [str(ds.SOPClassUID)]},
                     "00081155": {"vr": "UI", "Value": [str(ds.SOPInstanceUID)]}})
    print("REQUEST " + json.dumps({"path": request.path, "chunked": request.headers.get("Transfer-Encoding"),
          "content_length": request.headers.get("Content-Length"), "parts": seen}), flush=True)
    return web.Response(status=200, content_type="application/dicom+json",
                        text=json.dumps({"00081199": {"vr": "SQ", "Value": refs}}))

async def main():
    import asyncio
    app = web.Application(client_max_size=1 << 30)
    app.router.add_post("/studies", stow)
    app.router.add_post("/studies/{study}", stow)
    runner = web.AppRunner(app)
    await runner.setup()
    site = web.TCPSite(runner, "127.0.0.1", 0)
    await site.start()
    print("PORT %d" % site._server.sockets[0].getsockname()[1], flush=True)
    await asyncio.sleep(120)

import asyncio
asyncio.run(main())
`

// TestStoreStreamAgainstAiohttp: aiohttp receives the chunked body as the
// parts sent, with the content type of each, and pydicom reads each as the
// instance it was; the result is the account aiohttp gave.
func TestStoreStreamAgainstAiohttp(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	if exec.Command(python, "-c", "import aiohttp, pydicom").Run() != nil {
		t.Skip("aiohttp and pydicom are not importable; skipping the check against another implementation")
	}
	script := filepath.Join(t.TempDir(), "stow.py")
	if err := os.WriteFile(script, []byte(stowReceiver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, script)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(nil, 1<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	wait := func(prefix string) string {
		t.Helper()
		timeout := time.After(30 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("aiohttp exited before printing %s", prefix)
				}
				if rest, found := strings.CutPrefix(line, prefix); found {
					return rest
				}
			case <-timeout:
				t.Fatalf("aiohttp never printed %s", prefix)
			}
		}
	}
	port := strings.TrimSpace(wait("PORT "))

	files := [][]byte{part10(t, "1.2.3.1"), part10(t, "1.2.3.2"), part10(t, "1.2.3.3")}
	client, err := NewClient("http://127.0.0.1:" + port)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := readers(files...)
	result, err := client.StoreStream(ctx(t), "1.2.3.100", next)
	if err != nil {
		t.Fatalf("StoreStream: %v", err)
	}

	var seen struct {
		Path          string
		Chunked       string
		ContentLength *string `json:"content_length"`
		Parts         []struct {
			Type, Sha256, Sop string
			Length            int
		}
	}
	if err := json.NewDecoder(bytes.NewReader([]byte(wait("REQUEST ")))).Decode(&seen); err != nil {
		t.Fatal(err)
	}
	if seen.Path != "/studies/1.2.3.100" || seen.Chunked != "chunked" || seen.ContentLength != nil {
		t.Errorf("aiohttp saw %s, Transfer-Encoding %q, Content-Length %v", seen.Path, seen.Chunked, seen.ContentLength)
	}
	if len(seen.Parts) != len(files) {
		t.Fatalf("aiohttp read %d parts, want %d", len(seen.Parts), len(files))
	}
	for i, part := range seen.Parts {
		sum := sha256.Sum256(files[i])
		if part.Type != "application/dicom" || part.Sha256 != hex.EncodeToString(sum[:]) ||
			part.Sop != "1.2.3."+string(rune('1'+i)) || part.Length != len(files[i]) {
			t.Errorf("part %d: %+v is not file %d", i, part, i)
		}
	}
	if len(result.Stored) != 3 || result.Stored[2].SOPInstanceUID != "1.2.3.3" {
		t.Errorf("the result is %+v", result)
	}
	t.Logf("aiohttp read %d chunked parts, each byte for byte the file sent and read by pydicom", len(seen.Parts))
}
