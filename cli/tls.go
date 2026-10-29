package cli

import (
	"errors"
	"flag"

	"github.com/amrshadid/go-dicom/network"
)

// tlsFlags are the TLS options every network command takes. The client and
// server commands share the names, so a deployment configures both sides the
// same way.
type tlsFlags struct {
	enabled    bool
	cert, key  string
	ca         string
	serverName string
	insecure   bool
	server     bool
}

// addClientTLSFlags registers the TLS options of a command that associates.
func addClientTLSFlags(fs *flag.FlagSet, f *tlsFlags) {
	fs.BoolVar(&f.enabled, "tls", false, "Associate over TLS")
	fs.StringVar(&f.ca, "tls-ca", "", "PEM file of CAs to verify the server against, instead of the system roots")
	fs.StringVar(&f.cert, "tls-cert", "", "PEM certificate to present to the server, for mutual TLS")
	fs.StringVar(&f.key, "tls-key", "", "PEM private key for -tls-cert")
	fs.StringVar(&f.serverName, "tls-server-name", "", "Name to verify the server's certificate for, when it is not the host")
	fs.BoolVar(&f.insecure, "tls-insecure", false, "Do not verify the server's certificate (testing only)")
}

// addServerTLSFlags registers the TLS options of a command that listens.
func addServerTLSFlags(fs *flag.FlagSet, f *tlsFlags) {
	f.server = true
	fs.BoolVar(&f.enabled, "tls", false, "Accept associations over TLS only")
	fs.StringVar(&f.cert, "tls-cert", "", "PEM certificate to present to clients (required with -tls)")
	fs.StringVar(&f.key, "tls-key", "", "PEM private key for -tls-cert (required with -tls)")
	fs.StringVar(&f.ca, "tls-ca", "", "PEM file of CAs; clients must present a certificate issued by one")
}

// config returns the TLS configuration the flags describe, or nil for plain
// TCP. A TLS option given without -tls is an error rather than ignored: a
// command that quietly connects in the clear is the failure TLS is for.
func (f *tlsFlags) config() (*network.TLSConfig, error) {
	if !f.enabled {
		if f.cert != "" || f.key != "" || f.ca != "" || f.serverName != "" || f.insecure {
			return nil, errors.New("a -tls-* option was given without -tls")
		}
		return nil, nil
	}
	if f.server && (f.cert == "" || f.key == "") {
		return nil, errors.New("-tls needs -tls-cert and -tls-key to serve")
	}
	return &network.TLSConfig{
		CertFile:           f.cert,
		KeyFile:            f.key,
		CAFile:             f.ca,
		ServerName:         f.serverName,
		InsecureSkipVerify: f.insecure,
	}, nil
}
