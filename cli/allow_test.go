package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	output := &syncBuffer{}
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

// TestAllowReadsPairs: TITLE@ADDRESS is a title admitted from an address or a
// range; an @ in a title that is not followed by an address stays a title, as
// it was; and a pairing that is mistyped, or has no title, is an error and
// not a title that nobody has.
func TestAllowReadsPairs(t *testing.T) {
	var a allowFlag
	if err := a.Set("PRISMA1@10.0.0.5, CT_2@10.1.2.3/24,A@B@fd00::1, WS@HOME"); err != nil {
		t.Fatal(err)
	}
	if got, want := a.String(), "WS@HOME,PRISMA1@10.0.0.5/32,CT_2@10.1.2.0/24,A@B@fd00::1/128"; got != want {
		t.Errorf("read as %q, want %q", got, want)
	}
	if got, want := a.describe(), "AE titles WS@HOME; titles from addresses PRISMA1@10.0.0.5/32, CT_2@10.1.2.0/24, A@B@fd00::1/128"; got != want {
		t.Errorf("described as %q, want %q", got, want)
	}
	for _, bad := range []string{"@10.0.0.1", "CT@10.0.0", "CT@10.0.0.300", "CT@10.0.0.0/33", "SEVENTEEN_CHARS_X@10.0.0.1", "CT@fe80::1%en0"} {
		var b allowFlag
		if err := b.Set(bad); err == nil {
			t.Errorf("Set(%q) was accepted as %q", bad, b.String())
		}
	}
}

// TestAllowDecidesPairs: a title paired with an address is admitted from
// there and from nowhere else; each pair is a way in of its own beside the
// titles and addresses given together, whose rule is as it was; a listed
// title from an address it is not admitted from is refused with no reason
// given, since "not recognized" would be untrue of it; a title that is not
// listed is not recognized, from an address that is listed for another; and
// from an address that no entry names the title is not looked at.
func TestAllowDecidesPairs(t *testing.T) {
	titleRefused := network.RejectCallingAE()
	addressRefused := &network.AssociationRejection{
		Result: network.RJResultRejectedPermanent, Source: network.RJSourceServiceUser, Reason: network.RJReasonUserNoReason,
	}
	tcp := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 40000} }
	cases := []struct {
		allow, calling, from string
		want                 *network.AssociationRejection
	}{
		{"PRISMA1@10.0.0.5", "PRISMA1", "10.0.0.5", nil},
		{"PRISMA1@10.0.0.5", "PRISMA1", "10.0.0.6", addressRefused},
		{"PRISMA1@10.0.0.5", "CT_2", "10.0.0.5", titleRefused},
		{"PRISMA1@10.0.0.5", "CT_2", "10.0.0.6", addressRefused},
		{"PRISMA1@10.0.0.0/24", "PRISMA1", "10.0.0.200", nil},
		{"PRISMA1@10.0.0.0/24", "PRISMA1", "::ffff:10.0.0.7", nil},
		{"PRISMA1@fd00::/8", "PRISMA1", "fd12::1", nil},
		{"PRISMA1@10.0.0.5,CT_2@192.168.1.5", "CT_2", "192.168.1.5", nil},
		// A title is admitted from the address it is paired with, not from
		// another's.
		{"PRISMA1@10.0.0.5,CT_2@192.168.1.5", "CT_2", "10.0.0.5", addressRefused},
		{"PRISMA1@10.0.0.5,CT_2@192.168.1.5", "NOBODY", "10.0.0.5", titleRefused},
		{"PRISMA1@10.0.0.5,CT_2@192.168.1.5", "NOBODY", "172.16.0.1", addressRefused},
		// The same title from two places.
		{"PRISMA1@10.0.0.5,PRISMA1@192.168.1.5", "PRISMA1", "192.168.1.5", nil},
		// Beside the titles and addresses given together: that is one way in
		// with its own rule, and each pair another.
		{"CT_2,10.0.0.0/8,PRISMA1@192.168.1.5", "CT_2", "10.9.9.9", nil},
		{"CT_2,10.0.0.0/8,PRISMA1@192.168.1.5", "PRISMA1", "192.168.1.5", nil},
		{"CT_2,10.0.0.0/8,PRISMA1@192.168.1.5", "PRISMA1", "10.9.9.9", addressRefused},
		{"CT_2,10.0.0.0/8,PRISMA1@192.168.1.5", "CT_2", "192.168.1.5", addressRefused},
		{"CT_2,PRISMA1@192.168.1.5", "CT_2", "172.16.0.1", nil},
		{"10.0.0.0/8,PRISMA1@192.168.1.5", "ANYONE", "10.9.9.9", nil},
		{"10.0.0.0/8,PRISMA1@192.168.1.5", "ANYONE", "192.168.1.5", titleRefused},
	}
	for _, c := range cases {
		var a allowFlag
		if err := a.Set(c.allow); err != nil {
			t.Fatal(err)
		}
		var reported []string
		admit := a.admission(func(format string, args ...any) { reported = append(reported, fmt.Sprintf(format, args...)) })
		got := admit(context.Background(), &network.AssociationRequest{CallingAE: c.calling, CalledAE: "ARCH", RemoteAddr: tcp(c.from)})
		name := fmt.Sprintf("-allow %s, %q from %s", c.allow, c.calling, c.from)
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

	// What the server prints says why.
	var a allowFlag
	if err := a.Set("PRISMA1@10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	var reported string
	a.admission(func(format string, args ...any) { reported = fmt.Sprintf(format, args...) })(context.Background(),
		&network.AssociationRequest{CallingAE: "PRISMA1", RemoteAddr: tcp("10.0.0.6")})
	if want := `Refused "PRISMA1" from 10.0.0.6:40000: the address is not one "PRISMA1" is admitted from on -allow`; !strings.Contains(reported, want) {
		t.Errorf("reported %q, want it to say %q", reported, want)
	}
}

// TestAllowReadsAFile: one entry to a line, a comment on a line of its own or
// after a space or tab, blank lines, CRLF line ends and a byte order mark are
// all read, and the entries are those the same text gives -allow; a bad entry
// is named by file and line; a file that gives no entry, or is not there, or
// is too long, is an error, since an empty list admits every caller.
func TestAllowReadsAFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good.txt", "\xEF\xBB\xBF# modalities of the radiology department\r\n"+
		"PRISMA1@10.0.0.5   # the MR\r\n"+
		"\r\n"+
		"CT_2\t# a title, then a tab\r\n"+
		"   10.1.0.0/16\r\n"+
		"  # an indented comment\r\n"+
		"WITH#HASH\r\n"+
		"PAIR@fd00::/8")
	var fromFile, fromFlag allowFlag
	if err := fromFile.readFile(good); err != nil {
		t.Fatal(err)
	}
	if err := fromFlag.Set("PRISMA1@10.0.0.5,CT_2,10.1.0.0/16,WITH#HASH,PAIR@fd00::/8"); err != nil {
		t.Fatal(err)
	}
	if got, want := fromFile.String(), fromFlag.String(); got != want || got == "" {
		t.Errorf("the file gave %q, the same entries on the command line give %q", got, want)
	}

	// Several files and -allow are joined.
	var joined allowFlag
	flags := flag.NewFlagSet("x", flag.ContinueOnError)
	joined.add(flags)
	if err := flags.Parse([]string{"-allow", "A_1", "-allow-file", good, "-allow-file", write("more.txt", "B_2\n"), "-allow", "C_3"}); err != nil {
		t.Fatal(err)
	}
	if got := joined.String(); !strings.HasPrefix(got, "A_1,CT_2,WITH#HASH,B_2,C_3") {
		t.Errorf("joined as %q", got)
	}

	for _, c := range []struct{ name, content, want string }{
		{"bad.txt", "PRISMA1\n10.0.0.300\n", "bad.txt:2: -allow entry"},
		{"typo.txt", "# first\nCT@10.0.0\n", "typo.txt:2:"},
		{"empty.txt", "", "has no entry"},
		{"comments.txt", "# nothing\n\n   # here\n", "has no entry"},
		{"long.txt", strings.Repeat("# padding\n", maxAllowFile/10+1), "longer than"},
	} {
		var a allowFlag
		err := a.readFile(write(c.name, c.content))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error naming %q", c.name, err, c.want)
		}
	}
	var missing allowFlag
	if err := missing.readFile(filepath.Join(dir, "absent.txt")); err == nil || !strings.Contains(err.Error(), "absent.txt") {
		t.Errorf("a file that is not there: %v", err)
	}
}

// TestStorescpAndQrscpAdmitByPairAndFile runs each server command with the
// list in a file, a title paired with the loopback and another with an
// address that is not: the first is admitted, the second is refused with no
// reason given, and one that is not listed is not recognized, as the caller
// is told and as the server prints.
func TestStorescpAndQrscpAdmitByPairAndFile(t *testing.T) {
	binary := buildCLI(t)
	list := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(list, []byte("# the archive's callers\nPRISMA1@127.0.0.1\nELSEWHERE@10.0.0.5  # not here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"storescp", "qrscp"} {
		t.Run(command, func(t *testing.T) {
			address, output := startAllowServer(t, binary, command, "-aet", "ARCH", "-allow-file", list)
			if err := allowEcho(address, "PRISMA1"); err != nil {
				t.Errorf("PRISMA1 from 127.0.0.1 was not admitted: %v", err)
			}
			assertRefusal(t, allowEcho(address, "ELSEWHERE"), network.RJReasonUserNoReason)
			// Not listed, from an address that is: not recognized.
			assertRefusal(t, allowEcho(address, "NOBODY"), network.RJReasonCallingAETitleNotRecognized)

			want := `Refused "ELSEWHERE" from 127.0.0.1:`
			deadline := time.Now().Add(5 * time.Second)
			for !strings.Contains(output.String(), want) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if got := output.String(); !strings.Contains(got, want) ||
				!strings.Contains(got, `is not one "ELSEWHERE" is admitted from`) ||
				!strings.Contains(got, "titles from addresses PRISMA1@127.0.0.1/32, ELSEWHERE@10.0.0.5/32") {
				t.Errorf("%s did not print its list and the refusal:\n%s", command, got)
			}

			// Beside a pair, on the command line: a title listed alone is
			// admitted from anywhere, and the pair's from its own range only.
			mixed, _ := startAllowServer(t, binary, command, "-aet", "ARCH", "-allow", "CT_2,PRISMA1@10.0.0.0/8")
			if err := allowEcho(mixed, "CT_2"); err != nil {
				t.Errorf("CT_2 was not admitted: %v", err)
			}
			assertRefusal(t, allowEcho(mixed, "PRISMA1"), network.RJReasonUserNoReason)
		})
	}
	for _, args := range [][]string{{"-allow-file", filepath.Join(t.TempDir(), "none.txt")}, {"-allow", "CT@10.0.0"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := exec.CommandContext(ctx, binary, append([]string{"storescp"}, args...)...).CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(out), "-allow") {
			t.Errorf("storescp %v started or said nothing of -allow: %v\n%s", args, err, out)
		}
	}
}
