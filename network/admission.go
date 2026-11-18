package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
)

// A-ASSOCIATE-RJ reasons (PS3.8 Table 9-21). What a reason means depends on
// the source it is sent with, so each constant names its source.
const (
	// With RJSourceServiceUser.
	RJReasonUserNoReason                byte = 1
	RJReasonApplicationContextName      byte = 2
	RJReasonCallingAETitleNotRecognized byte = 3
	RJReasonCalledAETitleNotRecognized  byte = 7

	// With RJSourceServiceProviderACSE.
	RJReasonACSENoReason               byte = 1
	RJReasonProtocolVersionUnsupported byte = 2

	// With RJSourceServiceProviderPresentation.
	RJReasonTemporaryCongestion byte = 1
	RJReasonLocalLimitExceeded  byte = 2
)

// AssociationRequest is what an SCP knows of a caller when it decides whether
// to admit it: the two AE titles of the A-ASSOCIATE-RQ and where the
// connection comes from. Nothing has been negotiated yet.
type AssociationRequest struct {
	// CallingAE is the AE title the caller gave for itself, and CalledAE the
	// one it asked for, both without their padding. Either can be anything
	// the caller chose to send: over plain TCP neither is proof of who is
	// calling.
	CallingAE string
	CalledAE  string

	// RemoteAddr is the caller's address and LocalAddr the one it reached.
	RemoteAddr net.Addr
	LocalAddr  net.Addr

	// VerifiedChains are the caller's certificate chains as TLS verified them
	// against TLSConfig.CAFile, leaf first. They are nil over plain TCP, and
	// over TLS when the server does not ask for a client certificate.
	VerifiedChains [][]*x509.Certificate
}

// AssociationRejection is the A-ASSOCIATE-RJ an SCP sends to refuse an
// association: its result, source and reason (PS3.8 Table 9-21).
type AssociationRejection struct {
	// Result is RJResultRejectedPermanent or RJResultRejectedTransient. A
	// transient refusal tells the caller it may try again later.
	Result byte
	// Source is RJSourceServiceUser, RJSourceServiceProviderACSE or
	// RJSourceServiceProviderPresentation.
	Source byte
	// Reason is one of the RJReason constants for that source.
	Reason byte
}

// RejectCallingAE refuses permanently a caller whose calling AE title is not
// one the SCP knows.
func RejectCallingAE() *AssociationRejection {
	return &AssociationRejection{RJResultRejectedPermanent, RJSourceServiceUser, RJReasonCallingAETitleNotRecognized}
}

// RejectCalledAE refuses permanently a caller that asked for an AE title the
// SCP does not answer to.
func RejectCalledAE() *AssociationRejection {
	return &AssociationRejection{RJResultRejectedPermanent, RJSourceServiceUser, RJReasonCalledAETitleNotRecognized}
}

// RejectBusy refuses for now a caller the SCP cannot take at the moment, so
// that it tries again later.
func RejectBusy() *AssociationRejection {
	return &AssociationRejection{RJResultRejectedTransient, RJSourceServiceProviderPresentation, RJReasonTemporaryCongestion}
}

// valid reports whether the result, source and reason are a combination PS3.8
// Table 9-21 defines. The others are reserved and are not to be sent.
func (r AssociationRejection) valid() bool {
	if r.Result != RJResultRejectedPermanent && r.Result != RJResultRejectedTransient {
		return false
	}
	switch r.Source {
	case RJSourceServiceUser:
		switch r.Reason {
		case RJReasonUserNoReason, RJReasonApplicationContextName,
			RJReasonCallingAETitleNotRecognized, RJReasonCalledAETitleNotRecognized:
			return true
		}
	case RJSourceServiceProviderACSE:
		return r.Reason == RJReasonACSENoReason || r.Reason == RJReasonProtocolVersionUnsupported
	case RJSourceServiceProviderPresentation:
		return r.Reason == RJReasonTemporaryCongestion || r.Reason == RJReasonLocalLimitExceeded
	}
	return false
}

// String names the result, source and reason as PS3.8 Table 9-21 does, such
// as "rejected-permanent, service-user: calling-AE-title-not-recognized".
func (r AssociationRejection) String() string {
	result := fmt.Sprintf("result %d", r.Result)
	switch r.Result {
	case RJResultRejectedPermanent:
		result = "rejected-permanent"
	case RJResultRejectedTransient:
		result = "rejected-transient"
	}
	source, reason := fmt.Sprintf("source %d", r.Source), fmt.Sprintf("reason %d", r.Reason)
	switch r.Source {
	case RJSourceServiceUser:
		source = "service-user"
		switch r.Reason {
		case RJReasonUserNoReason:
			reason = "no-reason-given"
		case RJReasonApplicationContextName:
			reason = "application-context-name-not-supported"
		case RJReasonCallingAETitleNotRecognized:
			reason = "calling-AE-title-not-recognized"
		case RJReasonCalledAETitleNotRecognized:
			reason = "called-AE-title-not-recognized"
		}
	case RJSourceServiceProviderACSE:
		source = "service-provider (ACSE)"
		switch r.Reason {
		case RJReasonACSENoReason:
			reason = "no-reason-given"
		case RJReasonProtocolVersionUnsupported:
			reason = "protocol-version-not-supported"
		}
	case RJSourceServiceProviderPresentation:
		source = "service-provider (presentation)"
		switch r.Reason {
		case RJReasonTemporaryCongestion:
			reason = "temporary-congestion"
		case RJReasonLocalLimitExceeded:
			reason = "local-limit-exceeded"
		}
	}
	return result + ", " + source + ": " + reason
}

// The refusals the SCP chooses itself, when the admission function cannot
// give an answer.
var (
	// A function that panicked, or chose values the standard reserves, has
	// decided nothing, and an SCP closed by default admits nobody it has not
	// decided to admit. Permanent, because a caller retrying in a loop would
	// only meet the same fault again.
	rejectNoReason = AssociationRejection{RJResultRejectedPermanent, RJSourceServiceUser, RJReasonUserNoReason}
	// A function that has not answered within ARTIM is a server too busy to
	// decide: the caller may try again.
	rejectAfterTimeout = AssociationRejection{RJResultRejectedTransient, RJSourceServiceProviderPresentation, RJReasonTemporaryCongestion}
)

// associationRequest describes the caller of a connection whose
// A-ASSOCIATE-RQ has been read.
func associationRequest(transport *Transport, rq *AssociateRQ) *AssociationRequest {
	req := &AssociationRequest{
		CallingAE:  rq.CallingAE,
		CalledAE:   rq.CalledAE,
		RemoteAddr: transport.RemoteAddr(),
		LocalAddr:  transport.LocalAddr(),
	}
	// The handshake is over by now: it ran on the first read, which was the
	// A-ASSOCIATE-RQ's.
	if conn, ok := transport.conn.(*tls.Conn); ok {
		req.VerifiedChains = conn.ConnectionState().VerifiedChains
	}
	return req
}

// admit asks the configured function whether to admit a caller, and returns
// the refusal to send, or nil to go on.
//
// The function runs on a goroutine of its own so that one which blocks cannot
// hold the connection, and its association slot, past the ARTIM timer: the
// A-ASSOCIATE-RQ has to be answered within it (PS3.8 9.1.5), as the request
// had to arrive within it. Its context is canceled then; a function that
// ignores that holds only its own goroutine, until it returns. A panic in it
// is recovered there, so it refuses the association and does not take the
// server down.
func (s *SCP) admit(ctx context.Context, req *AssociationRequest) *AssociationRejection {
	decide := s.config.Admit
	if decide == nil {
		return nil
	}

	var cancel context.CancelFunc
	if timeout := s.config.Network.ARTIMTimeout; timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	type answer struct {
		rejection *AssociationRejection
		panicked  any
	}
	answers := make(chan answer, 1) // buffered: a late answer is dropped, not blocked on
	go func() {
		defer func() {
			if p := recover(); p != nil {
				answers <- answer{panicked: p}
			}
		}()
		answers <- answer{rejection: decide(ctx, req)}
	}()

	select {
	case a := <-answers:
		if a.panicked != nil {
			DefaultLogger.Error("refusing %q from %s: the admission function panicked: %v",
				req.CallingAE, req.RemoteAddr, a.panicked)
			r := rejectNoReason
			return &r
		}
		if a.rejection == nil {
			return nil
		}
		r := *a.rejection
		if !r.valid() {
			// Reserved values are not to be sent (PS3.8 9.3.4), and a peer
			// may read one as a malformed PDU rather than as a refusal.
			DefaultLogger.Warn("admission function chose result %d, source %d, reason %d, which PS3.8 "+
				"Table 9-21 does not define; refusing with %s instead", r.Result, r.Source, r.Reason, rejectNoReason)
			r = rejectNoReason
		}
		DefaultLogger.Info("refusing %q calling %q from %s: %s", req.CallingAE, req.CalledAE, req.RemoteAddr, r)
		return &r
	case <-ctx.Done():
		DefaultLogger.Warn("refusing %q from %s: the admission function did not answer within %v",
			req.CallingAE, req.RemoteAddr, s.config.Network.ARTIMTimeout)
		r := rejectAfterTimeout
		return &r
	}
}
