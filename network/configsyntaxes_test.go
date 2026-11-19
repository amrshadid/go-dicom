package network

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// syntaxProbe is what a requestor asking for CT storage, Study Root FIND and
// Verification is given: the abstract syntaxes accepted, by UID.
func syntaxProbe(t *testing.T, addr string, transferSyntaxes []string) (map[string]bool, error) {
	t.Helper()
	scu := NewSCU(SCUConfig{CallingAE: "SCANNER", CalledAE: "ARCHIVE", Address: addr})
	err := scu.Associate(context.Background(), []PresentationContextItem{
		{ID: 1, AbstractSyntax: CTImageStorageUID, TransferSyntaxes: transferSyntaxes},
		{ID: 3, AbstractSyntax: StudyRootQueryRetrieveFind, TransferSyntaxes: transferSyntaxes},
		{ID: 5, AbstractSyntax: VerificationSOPClassUID, TransferSyntaxes: transferSyntaxes},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = scu.Release(context.Background()) }()
	accepted := map[string]bool{}
	for _, pc := range scu.Association().AcceptedContexts() {
		accepted[pc.AbstractSyntax] = true
	}
	return accepted, nil
}

// syntaxServer starts a server with the given configuration on any free
// port and returns its address.
func syntaxServer(t *testing.T, cfg SCPConfig) string {
	t.Helper()
	cfg.AETitle, cfg.Port, cfg.BindAddress = "ARCHIVE", 0, "127.0.0.1"
	server, err := StartServer(context.Background(), cfg, &BaseHandler{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server.Addr()
}

// TestConfiguredSyntaxesApplyFromTheFirstAssociation: a requestor that
// connects in a loop while the server starts never negotiates a syntax
// outside SCPConfig.AbstractSyntaxes, from the first association it gets.
// Set by method after StartServer, they arrived after the accept loop had
// started, and Study Root FIND, which the defaults include, was accepted in
// the meantime (#468).
func TestConfiguredSyntaxesApplyFromTheFirstAssociation(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(probe.Addr().(*net.TCPAddr).Port)
	probe.Close()

	type result struct {
		accepted map[string]bool
		err      error
	}
	results := make(chan result, 64)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				accepted, err := syntaxProbe(t, addr, []string{ExplicitVRLittleEndianUID})
				if err != nil {
					time.Sleep(time.Millisecond)
					continue
				}
				select {
				case results <- result{accepted, nil}:
				case <-stop:
					return
				}
			}
		}()
	}

	server, err := StartServer(context.Background(), SCPConfig{
		AETitle: "ARCHIVE", Port: probe.Addr().(*net.TCPAddr).Port, BindAddress: "127.0.0.1",
		AbstractSyntaxes: StorageAndVerificationSyntaxes(),
	}, &BaseHandler{})
	if err != nil {
		close(stop)
		wg.Wait()
		t.Fatal(err)
	}
	defer server.Stop()

	got := 0
	timeout := time.After(10 * time.Second)
	for got < 20 {
		select {
		case r := <-results:
			got++
			if r.accepted[StudyRootQueryRetrieveFind] {
				t.Errorf("association %d accepted Study Root FIND, which the configured syntaxes leave out", got)
			}
			if !r.accepted[CTImageStorageUID] || !r.accepted[VerificationSOPClassUID] {
				t.Errorf("association %d accepted %v, want CT storage and Verification", got, r.accepted)
			}
		case <-timeout:
			t.Fatalf("only %d associations in 10s", got)
		}
	}
	close(stop)
	wg.Wait()
}

// TestUnsetSyntaxesKeepTheDefaults: an SCPConfig that does not set
// AbstractSyntaxes or TransferSyntaxes negotiates as before, the query and
// retrieve classes included.
func TestUnsetSyntaxesKeepTheDefaults(t *testing.T) {
	accepted, err := syntaxProbe(t, syntaxServer(t, SCPConfig{}), []string{ImplicitVRLittleEndianUID})
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{CTImageStorageUID, StudyRootQueryRetrieveFind, VerificationSOPClassUID} {
		if !accepted[uid] {
			t.Errorf("%s was not accepted with the syntaxes unset; the defaults include it", uid)
		}
	}
}

// TestEmptySyntaxesAcceptNothing: a non-nil empty AbstractSyntaxes, or an
// empty TransferSyntaxes, is used as given and accepts no presentation
// context; it is not read as "the defaults".
func TestEmptySyntaxesAcceptNothing(t *testing.T) {
	for name, cfg := range map[string]SCPConfig{
		"AbstractSyntaxes": {AbstractSyntaxes: []string{}},
		"TransferSyntaxes": {TransferSyntaxes: []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			accepted, err := syntaxProbe(t, syntaxServer(t, cfg), []string{ImplicitVRLittleEndianUID})
			if err == nil && len(accepted) > 0 {
				t.Errorf("an empty %s accepted %v, want nothing", name, accepted)
			}
		})
	}
}

// TestStorageAndVerificationSyntaxes: the ready-made list accepts storage and
// Verification and refuses query and retrieve, and a configured transfer
// syntax list refuses a context that offers only another.
func TestStorageAndVerificationSyntaxes(t *testing.T) {
	addr := syntaxServer(t, SCPConfig{
		AbstractSyntaxes: StorageAndVerificationSyntaxes(),
		TransferSyntaxes: []string{ExplicitVRLittleEndianUID},
	})
	accepted, err := syntaxProbe(t, addr, []string{ExplicitVRLittleEndianUID})
	if err != nil {
		t.Fatal(err)
	}
	if !accepted[CTImageStorageUID] || !accepted[VerificationSOPClassUID] || accepted[StudyRootQueryRetrieveFind] {
		t.Errorf("accepted %v, want CT storage and Verification and not Study Root FIND", accepted)
	}
	if accepted, err := syntaxProbe(t, addr, []string{ImplicitVRLittleEndianUID}); err == nil && len(accepted) > 0 {
		t.Errorf("a server configured for Explicit VR Little Endian accepted %v offered only Implicit", accepted)
	}
}
