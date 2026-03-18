package network

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// TLSConfig holds TLS configuration for encrypted DICOM communication.
// DICOM Part 15 recommends TLS for HIPAA compliance and PHI protection.
type TLSConfig struct {
	// CertFile is the path to the TLS certificate file (PEM format).
	CertFile string

	// KeyFile is the path to the TLS private key file (PEM format).
	KeyFile string

	// CAFile is the path to the CA certificate file for client verification.
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

// buildTLSConfig creates a *tls.Config from TLSConfig settings.
func (t *TLSConfig) buildTLSConfig() (*tls.Config, error) {
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

	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// DialTLS establishes a TLS-encrypted TCP connection.
func DialTLS(ctx context.Context, address string, timeout time.Duration, tlsCfg *TLSConfig) (*Transport, error) {
	if timeout == 0 {
		timeout = DefaultNetworkTimeout
	}

	config, err := tlsCfg.buildTLSConfig()
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
	config, err := tlsCfg.buildTLSConfig()
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
type SCUConfigTLS struct {
	SCUConfig
	TLS *TLSConfig
}

// SCPConfigTLS extends SCPConfig with TLS settings.
type SCPConfigTLS struct {
	SCPConfig
	TLS *TLSConfig
}
