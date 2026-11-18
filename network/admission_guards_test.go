package network

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// rawRejection sends an A-ASSOCIATE-RQ as it is, with AE titles a client
// library would not let through, and returns the refusal, nil when the SCP
// accepted.
func rawRejection(t *testing.T, addr string, rq *AssociateRQ) *AssociationRejection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport, err := Dial(ctx, addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if err := transport.WritePDU(ctx, rq); err != nil {
		t.Fatal(err)
	}
	pdu, err := transport.ReadPDU(ctx)
	if err != nil {
		t.Fatalf("calling %q, called %q: %v", rq.CallingAE, rq.CalledAE, err)
	}
	switch p := pdu.(type) {
	case *AssociateRJ:
		return &AssociationRejection{p.Result, p.Source, p.Reason}
	case *AssociateAC:
		return nil
	}
	t.Fatalf("calling %q: the SCP answered %T", rq.CallingAE, pdu)
	return nil
}

// TestAdmitIsAskedOnlyAboutWellFormedAETitles: the function was given each AE
// title as its bytes arrived, less the trailing spaces: " PRISMA1" with its
// space, "PRISMA1" followed by a NUL or a tab, a title holding a newline or a
// backslash, bytes outside the default repertoire, and sixteen spaces as "".
// An exact allow list refused all of those; a deny list, a prefix match or a
// blank entry did not. Spaces are now removed from both ends (PS3.8 Table
// 9-11), and a request whose calling or called title is not an AE title
// (PS3.5 6.2) is refused by the SCP without the function being asked.
func TestAdmitIsAskedOnlyAboutWellFormedAETitles(t *testing.T) {
	asked := make(chan [2]string, 1)
	addr, _ := admitServer(t, SCPConfig{Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		asked <- [2]string{req.CallingAE, req.CalledAE}
		return nil
	}})
	wasAsked := func() (got [2]string, ok bool) {
		select {
		case got = <-asked:
			return got, true
		default:
			return got, false
		}
	}

	// Padding on either side is not part of the title; case is.
	for sent, want := range map[string]string{"PRISMA1": "PRISMA1", " PRISMA1": "PRISMA1", "  PRISMA1  ": "PRISMA1",
		"prisma1": "prisma1", "CT SCANNER": "CT SCANNER"} {
		if r := rawRejection(t, addr, associateRQ(sent, " ARCHIVE")); r != nil {
			t.Errorf("calling %q was refused: %s", sent, r)
		}
		if got, ok := wasAsked(); !ok || got != [2]string{want, "ARCHIVE"} {
			t.Errorf("calling %q, called \" ARCHIVE\": the function was asked %v about %q", sent, ok, got)
		}
	}

	malformed := []string{"PRISMA1\x00", "PRISMA1\t", "                ", strings.Repeat("\x00", 16),
		"A\nB", "PRISMA\\1", "PR\xcdSMA", "PRISMA\x7f"}
	for _, title := range malformed {
		if r := rawRejection(t, addr, associateRQ(title, "ARCHIVE")); r == nil || *r != *RejectCallingAE() {
			t.Errorf("calling %q: answered %v, want %s", title, r, RejectCallingAE())
		}
		if got, ok := wasAsked(); ok {
			t.Errorf("calling %q: the function was asked, about %q", title, got)
		}
		if r := rawRejection(t, addr, associateRQ("PRISMA1", title)); r == nil || *r != *RejectCalledAE() {
			t.Errorf("called %q: answered %v, want %s", title, r, RejectCalledAE())
		}
		if got, ok := wasAsked(); ok {
			t.Errorf("called %q: the function was asked, about %q", title, got)
		}
	}
}

// TestAETitlePaddingIsNotPartOfTheTitle: without a function too, a called AE
// title padded on the left is the SCP's own title. It was refused as another.
func TestAETitlePaddingIsNotPartOfTheTitle(t *testing.T) {
	addr, _ := admitServer(t, SCPConfig{})
	if r := rawRejection(t, addr, associateRQ("SCANNER", "  ARCHIVE")); r != nil {
		t.Fatalf("called \"  ARCHIVE\" was refused: %s", r)
	}
	encoded, err := associateRQ(" A B ", "  C").Encode()
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := DecodePDU(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if rq := pdu.(*AssociateRQ); rq.CallingAE != "A B" || rq.CalledAE != "C" {
		t.Errorf("decoded calling %q and called %q, want \"A B\" and \"C\"", rq.CallingAE, rq.CalledAE)
	}
}

// TestAdmitDoesNotOpenTheCalledAE: setting a function used to switch off the
// SCP's own check of the called AE title, so a function added only to log or
// to filter by address made the SCP answer to every title. The check stays:
// a caller the function admits is refused when it asked for another title,
// and one the function refuses is told what the function chose whichever
// title it asked for. AcceptAnyCalledAE is what leaves the title to the
// function.
func TestAdmitDoesNotOpenTheCalledAE(t *testing.T) {
	observe := func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		if req.CallingAE == "STRANGER" {
			return RejectCallingAE()
		}
		return nil
	}
	addr, events := admitServer(t, SCPConfig{Admit: observe})
	if r := associateAs(t, addr, "SCANNER", "ARCHIVE"); r != nil {
		t.Fatalf("the SCP's own title was refused: %v", r)
	}
	wantRejection(t, "an admitted caller asking for OTHER", associateAs(t, addr, "SCANNER", "OTHER"), *RejectCalledAE())
	wantRejection(t, "a refused caller asking for OTHER", associateAs(t, addr, "STRANGER", "OTHER"), *RejectCallingAE())
	if n := len(events.of(EVTAssocAccepted)); n != 1 {
		t.Errorf("%d associations accepted, want 1", n)
	}

	open, _ := admitServer(t, SCPConfig{Admit: observe, AcceptAnyCalledAE: true})
	if r := associateAs(t, open, "SCANNER", "OTHER"); r != nil {
		t.Fatalf("with AcceptAnyCalledAE, OTHER was refused: %v", r)
	}

	// Without a function the field opens nothing.
	plain, _ := admitServer(t, SCPConfig{AcceptAnyCalledAE: true})
	wantRejection(t, "AcceptAnyCalledAE without Admit", associateAs(t, plain, "SCANNER", "OTHER"), *RejectCalledAE())
}

// TestAdmitIsGivenOnlyVerifiedChains: VerifiedChains holds a certificate only
// when the handshake verified it. A server that asks for a certificate
// without verifying it (tls.RequestClientCert, tls.RequireAnyClientCert)
// completes the handshake with whatever the caller presents, and a function
// that admits on len(VerifiedChains) > 0 must not be shown that certificate.
// Nothing pinned it: handing the function the presented certificates passed
// every test.
func TestAdmitIsGivenOnlyVerifiedChains(t *testing.T) {
	pki := newTestPKI(t)
	foreign := newTestPKI(t) // signed by a CA the server does not know
	caPEM, err := os.ReadFile(pki.caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	serverCert, err := tls.LoadX509KeyPair(pki.serverCert, pki.serverKey)
	if err != nil {
		t.Fatal(err)
	}
	asking := func(mode tls.ClientAuthType) *TLSConfig {
		return &TLSConfig{Config: &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: pool,
			ClientAuth: mode, MinVersion: tls.VersionTLS12}}
	}
	verifying := &TLSConfig{CertFile: pki.serverCert, KeyFile: pki.serverKey, CAFile: pki.caFile}
	known := &TLSConfig{CertFile: pki.clientCert, KeyFile: pki.clientKey, CAFile: pki.caFile}
	unknown := &TLSConfig{CertFile: foreign.clientCert, KeyFile: foreign.clientKey, CAFile: pki.caFile}
	none := &TLSConfig{CAFile: pki.caFile}

	const notAsked = -1
	for _, c := range []struct {
		name           string
		server, client *TLSConfig
		chains         int // what the function is given, or notAsked
	}{
		{"verified against CAFile", verifying, known, 1},
		{"CAFile, a certificate of another CA", verifying, unknown, notAsked},
		{"no client certificate asked for", &TLSConfig{CertFile: pki.serverCert, KeyFile: pki.serverKey}, known, 0},
		{"RequestClientCert, a certificate of another CA", asking(tls.RequestClientCert), unknown, 0},
		{"RequireAnyClientCert, a certificate of another CA", asking(tls.RequireAnyClientCert), unknown, 0},
		{"RequireAnyClientCert, a certificate of the right CA, unverified", asking(tls.RequireAnyClientCert), known, 0},
		{"VerifyClientCertIfGiven, none given", asking(tls.VerifyClientCertIfGiven), none, 0},
		{"VerifyClientCertIfGiven, verified", asking(tls.VerifyClientCertIfGiven), known, 1},
		{"VerifyClientCertIfGiven, a certificate of another CA", asking(tls.VerifyClientCertIfGiven), unknown, notAsked},
	} {
		t.Run(c.name, func(t *testing.T) {
			given := make(chan int, 1)
			addr, _ := admitServer(t, SCPConfig{TLS: c.server, Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
				given <- len(req.VerifiedChains)
				return nil
			}})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			scu := NewSCU(SCUConfig{CallingAE: "A", CalledAE: "ARCHIVE", Address: addr, TLS: c.client})
			err := scu.Associate(ctx, DefaultVerificationContexts())
			if err == nil {
				_ = scu.Release(ctx)
			}
			got := notAsked
			select {
			case got = <-given:
			default:
			}
			if got != c.chains {
				t.Errorf("the function was given %d verified chains (-1: not asked), want %d; associate: %v", got, c.chains, err)
			}
			if (c.chains == notAsked) != (err != nil) {
				t.Errorf("associate: %v", err)
			}
		})
	}
}

// TestAdmitIsGivenTheUserIdentity: the User Identity Negotiation item of the
// request reaches the function, which is where a user name and password or a
// token can decide admission; nil when the caller sent none.
func TestAdmitIsGivenTheUserIdentity(t *testing.T) {
	got := make(chan *UserIdentityNegotiation, 1)
	addr, _ := admitServer(t, SCPConfig{Admit: func(_ context.Context, req *AssociationRequest) *AssociationRejection {
		got <- req.UserIdentity
		if id := req.UserIdentity; id == nil || string(id.PrimaryField) != "tech" || string(id.SecondaryField) != "s3cret" {
			return RejectCallingAE()
		}
		return nil
	}})
	rq := associateRQ("SCANNER", "ARCHIVE")
	rq.UserInformation.UserIdentity = &UserIdentityNegotiation{Type: UserIdentityUsernamePassword,
		PrimaryField: []byte("tech"), SecondaryField: []byte("s3cret")}
	if r := rawRejection(t, addr, rq); r != nil {
		t.Fatalf("the right user name and password were refused: %s", r)
	}
	if id := <-got; id == nil || id.Type != UserIdentityUsernamePassword {
		t.Errorf("the function was given %+v", id)
	}
	if r := rawRejection(t, addr, associateRQ("SCANNER", "ARCHIVE")); r == nil {
		t.Fatal("a caller with no user identity was admitted")
	}
	if id := <-got; id != nil {
		t.Errorf("a request with no user identity item gave the function %+v", id)
	}
}

// lockedBuffer is a log output read while the server still writes to it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog sends everything the package logs, at every level, to the
// buffer it returns, for the length of the test. Each message is one line,
// with quotes and line ends inside it escaped.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	log := &lockedBuffer{}
	previous := DefaultLogger
	DefaultLogger = NewLogger(LogLevelDebug, log)
	t.Cleanup(func() { DefaultLogger = previous })
	return log
}

// TestRefusalLogQuotesWhatTheCallerSent: each refusal is logged once by the
// SCP, and nothing a caller puts in an AE title starts a log line of its own:
// the titles are quoted, and one holding a newline never reaches the function.
func TestRefusalLogQuotesWhatTheCallerSent(t *testing.T) {
	log := captureLog(t)
	addr, events := admitServer(t, SCPConfig{Admit: func(context.Context, *AssociationRequest) *AssociationRejection {
		return RejectCallingAE()
	}})
	wantRejection(t, "STRANGER", associateAs(t, addr, "STRANGER", "ARCHIVE"), *RejectCallingAE())
	if r := rawRejection(t, addr, associateRQ("X\n[WARN] FAKE", "ARCHIVE")); r == nil {
		t.Fatal("a calling AE title holding a newline was admitted")
	}
	events.await(t, EVTAssocRejected, 2)
	if n := strings.Count(log.String(), `refusing \"STRANGER\"`); n != 1 {
		t.Errorf("the refusal was logged %d times, want once:\n%s", n, log.String())
	}
	if strings.Contains(log.String(), "\n[WARN] FAKE") || !strings.Contains(log.String(), `X\\n[WARN] FAKE`) {
		t.Errorf("a newline in a calling AE title is not escaped in the log:\n%s", log.String())
	}
}

// TestStoppingServerDoesNotBlameTheFunction: a server stopped while the
// function is deciding refuses the caller, and does not log that the function
// "did not answer within" ARTIM, which it had not been given.
func TestStoppingServerDoesNotBlameTheFunction(t *testing.T) {
	log := captureLog(t)

	deciding := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartServer(ctx, SCPConfig{AETitle: "ARCHIVE", Port: 0, BindAddress: "127.0.0.1",
		Admit: func(ctx context.Context, _ *AssociationRequest) *AssociationRejection {
			close(deciding)
			<-ctx.Done()
			return nil
		}}, &EchoHandler{})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := Dial(context.Background(), server.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if err := transport.WritePDU(context.Background(), associateRQ("SCANNER", "ARCHIVE")); err != nil {
		t.Fatal(err)
	}
	<-deciding
	server.Stop()
	if strings.Contains(log.String(), "did not answer within") {
		t.Errorf("stopping the server was logged as the function being slow:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "the server is stopping") {
		t.Errorf("the refusal on shutdown was not logged as one:\n%s", log.String())
	}
}
