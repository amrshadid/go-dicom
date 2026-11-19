package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

// A TLS server runs the handshake on the first read of a connection, inside
// the read of the A-ASSOCIATE-RQ, so a client turned away by TLS used to
// leave only a log line: events saw the connection open and close, the same
// as a port scanner that connects and leaves. It is now an event of its own,
// EVTTLSHandshakeFailed, emitted before any association exists.

// TLSCheck names which part of a TLS handshake failed.
type TLSCheck string

// The parts of a TLS handshake that can fail, as a TLSHandshakeError names
// them.
const (
	// TLSCheckNotTLS: what the client sent is not a TLS record; it is most
	// likely speaking plain DICOM to a TLS port.
	TLSCheckNotTLS TLSCheck = "not-tls"
	// TLSCheckNoClientCertificate: the server requires a client certificate
	// and the client presented none.
	TLSCheckNoClientCertificate TLSCheck = "no-client-certificate"
	// TLSCheckUnknownAuthority: the client's certificate is not signed by a
	// CA the server trusts.
	TLSCheckUnknownAuthority TLSCheck = "unknown-authority"
	// TLSCheckCertificateInvalid: the client's certificate is signed by a
	// trusted CA but is not valid: expired, not yet valid, or not for client
	// authentication.
	TLSCheckCertificateInvalid TLSCheck = "certificate-invalid"
	// TLSCheckPeerAlert: the client ended the handshake with an alert,
	// usually because it did not accept the server's certificate.
	TLSCheckPeerAlert TLSCheck = "peer-alert"
	// TLSCheckProtocol: the two sides share no protocol version or cipher
	// suite, or the handshake was malformed.
	TLSCheckProtocol TLSCheck = "protocol"
	// TLSCheckConnection: the connection ended, or timed out, before the
	// handshake completed.
	TLSCheckConnection TLSCheck = "connection"
)

// TLSHandshakeError is the Error of an EVTTLSHandshakeFailed event: which
// check failed, in words.
//
// It holds no part of the peer's certificate. crypto/tls reports a
// verification failure as a *tls.CertificateVerificationError, which carries
// the certificates the peer presented, and the x509 errors inside it quote
// names and dates from them; neither is kept or wrapped here, so an event
// handler that logs the error cannot leak a certificate's contents.
type TLSHandshakeError struct {
	// Check is the part of the handshake that failed.
	Check TLSCheck
	// Message says what happened, without certificate contents.
	Message string
}

// Error returns the message, prefixed with the check that failed.
func (e *TLSHandshakeError) Error() string {
	return "tls handshake failed (" + string(e.Check) + "): " + e.Message
}

// handshakeFailure returns the TLSHandshakeError of a failed first read on
// transport, or nil when the transport is not TLS or its handshake
// completed (the read failed for another reason).
func handshakeFailure(transport *Transport, err error) *TLSHandshakeError {
	conn, ok := transport.conn.(*tls.Conn)
	if !ok || conn.ConnectionState().HandshakeComplete {
		return nil
	}
	return classifyHandshake(err)
}

// classifyHandshake names the check a handshake error failed.
func classifyHandshake(err error) *TLSHandshakeError {
	var record tls.RecordHeaderError
	if errors.As(err, &record) {
		return &TLSHandshakeError{TLSCheckNotTLS, "the client did not send a TLS record"}
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return &TLSHandshakeError{TLSCheckUnknownAuthority, "the client's certificate is not signed by a trusted CA"}
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return &TLSHandshakeError{TLSCheckCertificateInvalid, invalidReason(invalid.Reason)}
	}
	var verify *tls.CertificateVerificationError
	if errors.As(err, &verify) {
		return &TLSHandshakeError{TLSCheckCertificateInvalid, "the client's certificate did not verify"}
	}
	var alert tls.AlertError
	if errors.As(err, &alert) {
		return &TLSHandshakeError{TLSCheckPeerAlert, "the client sent the alert: " + alert.Error()}
	}
	text := err.Error()
	// crypto/tls has no type for these; its message is the only sign.
	switch {
	case strings.Contains(text, "didn't provide a certificate"):
		return &TLSHandshakeError{TLSCheckNoClientCertificate, "the client presented no certificate"}
	case strings.Contains(text, "remote error: tls:"):
		return &TLSHandshakeError{TLSCheckPeerAlert, "the client refused the handshake: " + text[strings.Index(text, "remote error: tls:"):]}
	case strings.Contains(text, "tls:"):
		return &TLSHandshakeError{TLSCheckProtocol, text[strings.Index(text, "tls:"):]}
	}
	return &TLSHandshakeError{TLSCheckConnection, "the connection ended before the handshake completed"}
}

// invalidReason says why a certificate is invalid without its dates or names.
func invalidReason(reason x509.InvalidReason) string {
	switch reason {
	case x509.Expired:
		return "the client's certificate has expired or is not yet valid"
	case x509.IncompatibleUsage:
		return "the client's certificate is not for client authentication"
	case x509.NotAuthorizedToSign:
		return "the client's certificate is signed by a certificate that may not sign"
	default:
		return "the client's certificate is not valid"
	}
}
