package network_test

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// A server on a listener the caller made must answer the programs that call
// it: dcmtk's echoscu and a pynetdicom client, on a listener bound to the
// loopback and on one bound to the IPv4 wildcard, a choice a caller who made
// the socket has made for itself.

// echoScript verifies the server with pynetdicom: it associates, sends a
// C-ECHO, and prints the status.
const echoScript = `
import sys
from pynetdicom import AE
from pynetdicom.sop_class import Verification

ae = AE(ae_title="PYECHO")
ae.add_requested_context(Verification)
assoc = ae.associate(sys.argv[1], int(sys.argv[2]), ae_title="ARCHIVE")
assert assoc.is_established, "not established"
status = assoc.send_c_echo()
print("C-ECHO status 0x%04X" % status.Status)
assoc.release()
`

// TestServerOnACallersListenerAnswersDcmtkAndPynetdicom: echoscu and pynetdicom
// each verify a server running on a listener bound here, on 127.0.0.1 and on
// 0.0.0.0, and Stop closes it.
func TestServerOnACallersListenerAnswersDcmtkAndPynetdicom(t *testing.T) {
	echoscu := dcmtkTool(t, "echoscu")
	requirePynetdicom(t)
	for _, c := range []struct{ name, network, bind string }{
		{"IPv4", "tcp4", "127.0.0.1:0"},
		{"IPv4 wildcard", "tcp4", "0.0.0.0:0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, err := net.Listen(c.network, c.bind)
			if err != nil {
				t.Skipf("cannot listen on %s: %v", c.bind, err)
			}
			server, err := network.StartServerOn(context.Background(), network.SCPConfig{AETitle: "ARCHIVE"},
				&network.EchoHandler{}, l)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.Stop)
			_, port, err := net.SplitHostPort(server.Addr())
			if err != nil {
				t.Fatal(err)
			}
			host := "127.0.0.1"

			out, err := exec.Command(echoscu, "-aet", "DCMECHO", "-aec", "ARCHIVE", host, port).CombinedOutput()
			if err != nil {
				t.Fatalf("echoscu: %v\n%s", err, out)
			}
			t.Logf("echoscu %s %s: exit 0", host, port)

			out, err = exec.Command("python3", "-c", echoScript, host, port).CombinedOutput()
			if err != nil || !strings.Contains(string(out), "C-ECHO status 0x0000") {
				t.Fatalf("pynetdicom: %v\n%s", err, out)
			}
			t.Logf("pynetdicom %s %s: %s", host, port, strings.TrimSpace(string(out)))

			server.Stop()
			server.Wait()
			if conn, err := net.DialTimeout(c.network, host+":"+port, time.Second); err == nil {
				conn.Close()
				t.Error("Stop left the caller's listener open")
			}
		})
	}
}
