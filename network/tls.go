package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// TLSConfig holds TLS configuration for encrypted DICOM communication.
// DICOM Part 15 recommends TLS for HIPAA compliance and PHI protection.
type TLSConfig struct {
	// CertFile is the path to the TLS certificate file (PEM format).
	CertFile string

	// KeyFile is the path to the TLS private key file (PEM format).
	KeyFile string

	// CAFile is the path to a PEM file of CA certificates. A server requires a
	// client certificate and verifies it against them; a client verifies the
	// server against them instead of the system roots.
	CAFile string

	// InsecureSkipVerify disables certificate verification (for testing only).
	InsecureSkipVerify bool

	// ServerName is the expected server hostname for certificate verification.
	ServerName string

	// MinVersion is the minimum TLS version (default: TLS 1.2).
	MinVersion uint16

	// Config allows providing a custom *tls.Config directly.
	// If set, CertFile/KeyFile/CAFile are ignored.
	Config *tls.Config
}

// buildTLSConfig creates a *tls.Config from TLSConfig settings, for the server
// side of a connection or the client side.
func (t *TLSConfig) buildTLSConfig(server bool) (*tls.Config, error) {
	if t == nil {
		return nil, errors.New("tls: no TLS configuration")
	}
	if t.Config != nil {
		return t.Config, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: t.InsecureSkipVerify,
		ServerName:         t.ServerName,
		MinVersion:         t.MinVersion,
	}

	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	// Half a key pair was ignored, and the side it was meant for ran with no
	// certificate at all.
	if (t.CertFile == "") != (t.KeyFile == "") {
		return nil, errors.New("tls: CertFile and KeyFile must be given together")
	}
	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if t.CAFile != "" {
		data, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("tls: %s holds no PEM certificate", t.CAFile)
		}
		// PS3.15 B.1 has each side authenticate the other.
		if server {
			tlsConfig.ClientCAs = pool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsConfig.RootCAs = pool
		}
	}

	return tlsConfig, nil
}

// DialTLS establishes a TLS-encrypted TCP connection.
func DialTLS(ctx context.Context, address string, timeout time.Duration, tlsCfg *TLSConfig) (*Transport, error) {
	if timeout == 0 {
		timeout = DefaultNetworkTimeout
	}

	config, err := tlsCfg.buildTLSConfig(false)
	if err != nil {
		return nil, err
	}

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    config,
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, NewCommunicationError("TLS_CONNECT", fmt.Sprintf("failed TLS connect to %s", address), err)
	}

	return NewTransport(conn, DefaultMaxPDUSize), nil
}

// ListenTLS creates a TLS-encrypted TCP listener.
func ListenTLS(address string, tlsCfg *TLSConfig) (*Listener, error) {
	config, err := tlsCfg.buildTLSConfig(true)
	if err != nil {
		return nil, err
	}

	ln, err := tls.Listen("tcp", address, config)
	if err != nil {
		return nil, NewCommunicationError("TLS_LISTEN", fmt.Sprintf("failed to TLS listen on %s", address), err)
	}

	return &Listener{listener: ln}, nil
}

// SCUConfigTLS extends SCUConfig with TLS settings.
//
// Deprecated: nothing reads this type, so a TLS setting placed here has no
// effect. Set SCUConfig.TLS.
type SCUConfigTLS struct {
	SCUConfig
	TLS *TLSConfig
}

// SCPConfigTLS extends SCPConfig with TLS settings.
//
// Deprecated: nothing reads this type, so a TLS setting placed here has no
// effect. Set SCPConfig.TLS.
type SCPConfigTLS struct {
	SCPConfig
	TLS *TLSConfig
}
