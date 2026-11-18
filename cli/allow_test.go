package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/network"
)

// TestAllowReadsTitlesAndAddresses: an entry that is an IP address or a CIDR
// range is an address, any other is a calling AE title, and an entry that is
// neither an address nor a possible AE title is an error rather than a line
// in the list that admits no one.
func TestAllowReadsTitlesAndAddresses(t *testing.T) {
	var a allowFlag
	for _, value := range []string{" PRISMA1 , CT_2", "10.1.2.3/8,192.168.4.7", "fd00::/8", "::ffff:172.16.0.0/108"} {
		if err := a.Set(value); err != nil {
			t.Fatalf("Set(%q): %v", value, err)
		}
	}
	if got, want := a.String(), "PRISMA1,CT_2,10.0.0.0/8,192.168.4.7/32,fd00::/8,172.16.0.0/12"; got != want {
		t.Errorf("read as %q, want %q", got, want)
	}
	if got, want := a.describe(), "AE titles PRISMA1, CT_2; addresses 10.0.0.0/8, 192.168.4.7/32, fd00::/8, 172.16.0.0/12"; got != want {
		t.Errorf("described as %q, want %q", got, want)
	}

	for _, bad := range []string{"10.0.0.300", "10.0.0.0/33", "fe80::1%en0", "fd00::/129", "A\\B",
		"SEVENTEEN_CHARS_X", "TAB\tIN", "10.0.0"} {
		var b allowFlag
		if err := b.Set(bad); err == nil {
			t.Errorf("Set(%q) was accepted as %q", bad, b.String())
		}
	}
}

// TestAllowDecides: entries of one kind are alternatives, the two kinds are
// both required, and each refusal carries the reason PS3.8 Table 9-21 has for
// it: an unlisted title is calling-AE-title-not-recognized, and an unlisted
// address, for which the table has no reason, is no-reason-given. An IPv4
// caller that arrives IPv4-mapped is matched as IPv4.
func TestAllowDecides(t *testing.T) {
	titleRefused := network.RejectCallingAE()
	addressRefused := &network.AssociationRejection{
		Result: network.RJResultRejectedPermanent, Source: network.RJSourceServiceUser, Reason: network.RJReasonUserNoReason,
	}
	tcp := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 40000} }

	cases := []struct {
		allow   string
		calling string
		from    net.Addr
		want    *network.AssociationRejection
	}{
		{"PRISMA1,CT_2", "CT_2", tcp("203.0.113.9"), nil},
		{"PRISMA1,CT_2", "NOBODY", tcp("203.0.113.9"), titleRefused},
		{"PRISMA1", "prisma1", tcp("203.0.113.9"), titleRefused},
		{"10.0.0.0/8,192.168.4.7", "ANYONE", tcp("10.200.1.1"), nil},
		{"10.0.0.0/8,192.168.4.7", "ANYONE", tcp("192.168.4.7"), nil},
		{"10.0.0.0/8,192.168.4.7", "ANYONE", tcp("192.168.4.8"), addressRefused},
		{"10.0.0.0/8", "ANYONE", tcp("::ffff:10.9.9.9"), nil},
		{"fd00::/8", "ANYONE", tcp("fd12::1"), nil},
		{"fd00::/8", "ANYONE", tcp("10.9.9.9"), addressRefused},
		{"PRISMA1,10.0.0.0/8", "PRISMA1", tcp("10.1.1.1"), nil},
		{"PRISMA1,10.0.0.0/8", "PRISMA1", tcp("172.16.0.1"), addressRefused},
		{"PRISMA1,10.0.0.0/8", "NOBODY", tcp("10.1.1.1"), titleRefused},
		// From a refused address the title is not looked at, so the caller
		// cannot learn from the reason whether its title is known.
		{"PRISMA1,10.0.0.0/8", "NOBODY", tcp("172.16.0.1"), addressRefused},
		{"10.0.0.0/8", "ANYONE", nil, addressRefused},
	}
	for _, c := range cases {
		var a allowFlag
		if err := a.Set(c.allow); err != nil {
			t.Fatal(err)
		}
		var reported []string
		admit := a.admission(func(format string, args ...any) { reported = append(reported, fmt.Sprintf(format, args...)) })
		got := admit(context.Background(), &network.AssociationRequest{CallingAE: c.calling, CalledAE: "ARCH", RemoteAddr: c.from})
		name := fmt.Sprintf("-allow %s, %q from %v", c.allow, c.calling, c.from)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("%s: refused, %s", name, got)
		case c.want != nil && (got == nil || *got != *c.want):
			t.Errorf("%s: got %v, want %s", name, got, c.want)
		case c.want != nil && len(reported) != 1:
			t.Errorf("%s: refused with %d lines reported, want 1", name, len(reported))
		case c.want == nil && len(reported) != 0:
			t.Errorf("%s: admitted, and reported %q", name, reported)
		}
	}

	var none allowFlag
	if none.admission(printRefusal) != nil {
		t.Error("without -allow the server was given an admission function")
	}
}

// TestStorescpAndQrscpRefuseByAllow runs each server command with -allow and
// associates with it as a caller on the list, as one whose title is not, and
// as one calling from an address that is not, checking what the caller is
// told and what the server prints.
func TestStorescpAndQrscpRefuseByAllow(t *testing.T) {
	binary := buildCLI(t)
	for _, command := range []string{"storescp", "qrscp"} {
		t.Run(command, func(t *testing.T) {
			address, output := startAllowServer(t, binary, command, "-aet", "ARCH", "-allow", "PRISMA1", "-allow", "127.0.0.0/8")
			if err := allowEcho(address, "PRISMA1"); err != nil {
				t.Errorf("PRISMA1 from 127.0.0.1 was not admitted: %v", err)
			}
			assertRefusal(t, allowEcho(address, "NOBODY_KNOWS_ME"), network.RJReasonCallingAETitleNotRecognized)

			// The loopback is not on this list, so nothing from it is admitted.
			elsewhere, _ := startAllowServer(t, binary, command, "-aet", "ARCH", "-allow", "PRISMA1,10.0.0.0/8")
			assertRefusal(t, allowEcho(elsewhere, "PRISMA1"), network.RJReasonUserNoReason)

			want := `Refused "NOBODY_KNOWS_ME" from 127.0.0.1:`
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(output.String(), want) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if got := output.String(); !strings.Contains(got, want) ||
				!strings.Contains(got, "Allow:") || !strings.Contains(got, "AE titles PRISMA1; addresses 127.0.0.0/8") {
				t.Errorf("%s did not print its list and the refusal:\n%s", command, got)
			}
		})
	}
}

// assertRefusal checks an association was refused rejected-permanent by the
// service-user with the given reason.
func assertRefusal(t *testing.T, err error, reason byte) {
	t.Helper()
	var rj *network.AssociationError
	if !errors.As(err, &rj) {
		t.Fatalf("want an A-ASSOCIATE-RJ with reason %d, got %v", reason, err)
	}
	if rj.Result != network.RJResultRejectedPermanent || rj.Source != network.RJSourceServiceUser || rj.Reason != reason {
		t.Errorf("refused with result %d, source %d, reason %d; want 1, 1, %d", rj.Result, rj.Source, rj.Reason, reason)
	}
}

// allowEcho associates as callingAE and sends a C-ECHO.
func allowEcho(address, callingAE string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	scu := network.NewSCU(network.SCUConfig{CallingAE: callingAE, CalledAE: "ARCH", Address: address})
	if err := scu.Associate(ctx, network.DefaultVerificationContexts()); err != nil {
		return err
	}
	defer func() { _ = scu.Release(ctx) }()
	return scu.Echo(ctx)
}

// syncBuffer is a bytes.Buffer a server subprocess writes to while the test
// reads it.
type syncBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func newSyncBuffer() *syncBuffer { return &syncBuffer{mu: make(chan struct{}, 1)} }

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu <- struct{}{}
	defer func() { <-b.mu }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu <- struct{}{}
	defer func() { <-b.mu }()
	return b.buf.String()
}

// startAllowServer starts a server command on a free port of the loopback,
// writing into a directory of the test's, and returns its address once it
// listens.
func startAllowServer(t *testing.T, binary, command string, args ...string) (string, *syncBuffer) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	_ = probe.Close()

	args = append([]string{command, "-port", port, "-output", t.TempDir()}, args...)
	server := exec.Command(binary, args...)
	output := newSyncBuffer()
	server.Stdout, server.Stderr = output, output
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })

	address := "127.0.0.1:" + port
	deadline := time.Now().Add(10 * time.Second)
	for {
		if conn, err := net.Dial("tcp", address); err == nil {
			_ = conn.Close()
			return address, output
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never listened:\n%s", command, output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
