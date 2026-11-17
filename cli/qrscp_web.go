package cli

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dcmstore"
	"github.com/amrshadid/go-dicom/dicomweb"
	"github.com/amrshadid/go-dicom/network"
)

// qrscp serves its store two ways: to a DIMSE peer on -port, and, with -web,
// to an HTTP client as a DICOMweb service. It is one archive reached by two
// protocols, which is how Orthanc and dcm4chee are built, so what a modality
// stores is what a viewer finds.

// webPrefix is the path the DICOMweb service is served under, as Orthanc
// serves its own, so a client configured for one finds the other.
const webPrefix = "/dicom-web"

// webServeFlags are the options of the DICOMweb side of qrscp. The token
// is read from the variable the DICOMweb commands send theirs from, so one
// setting serves both ends on a machine.
type webServeFlags struct {
	address string
	token   string
}

func (f *webServeFlags) add(fs *flag.FlagSet) {
	fs.StringVar(&f.address, "web", "",
		"Also serve the store over DICOMweb at this address, under "+webPrefix+
			": a port (8080), which listens on this machine only, or host:port")
	fs.StringVar(&f.token, "web-token", "",
		"Require this bearer token of every DICOMweb request ("+webTokenEnv+
			" sets it without showing it in the process list); needed to listen beyond this machine")
}

// webListenAddress reads -web: a port alone, with or without its colon, or
// host:port. It returns the address to listen on and whether only this
// machine can reach it.
//
// A port alone is this machine only. ":8080" means every interface to
// net.Listen, and an archive of patients' images should not be on the
// network because a colon was typed: reaching it from elsewhere takes naming
// an address, and then a token.
func webListenAddress(spec string) (address string, local bool, err error) {
	spec = strings.TrimSpace(spec)
	host, port := "", strings.TrimPrefix(spec, ":")
	if _, numberErr := strconv.Atoi(port); numberErr != nil {
		host, port, err = net.SplitHostPort(spec)
		if err != nil {
			return "", false, fmt.Errorf("-web %q is neither a port nor host:port", spec)
		}
	}
	if n, numberErr := strconv.Atoi(port); numberErr != nil || n < 1 || n > 65535 {
		return "", false, fmt.Errorf("-web %q: %q is not a port", spec, port)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	local = host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	return net.JoinHostPort(host, port), local, nil
}

// resolve reads the flags and the environment: the address to listen on and
// the token to require. An archive that can be reached from another machine
// and asks nothing of who reaches it is refused.
func (f *webServeFlags) resolve() (address, token string, err error) {
	token = f.token
	if token == "" {
		token = os.Getenv(webTokenEnv)
	}
	address, local, err := webListenAddress(f.address)
	if err != nil {
		return "", "", err
	}
	if !local && token == "" {
		return "", "", fmt.Errorf("-web %s can be reached from other machines, so it needs -web-token "+
			"(or %s): without one anybody who can reach the port can read the archive", address, webTokenEnv)
	}
	return address, token, nil
}

// webHandler is the DICOMweb service of a store: the server under its
// prefix, behind the token when there is one, with each request reported
// to report.
func webHandler(store *dcmstore.Store, token string, report func(line string)) http.Handler {
	service := http.StripPrefix(webPrefix, dicomweb.NewServer(store))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		switch {
		case r.URL.Path != webPrefix && !strings.HasPrefix(r.URL.Path, webPrefix+"/"):
			http.Error(recorded, "the DICOMweb service is under "+webPrefix, http.StatusNotFound)
		case token != "" && !bearerIs(r, token):
			recorded.Header().Set("WWW-Authenticate", `Bearer realm="go-dicom"`)
			http.Error(recorded, "this service needs a bearer token", http.StatusUnauthorized)
		default:
			service.ServeHTTP(recorded, r)
		}
		if report != nil {
			// The path and not the query: a search carries a patient's
			// name there, and this line goes to a terminal or a log.
			report(fmt.Sprintf("  Web: %s %s -> %d", r.Method, r.URL.Path, recorded.status))
		}
	})
}

// bearerIs reports whether a request carries the token, compared in
// constant time so the answer's timing says nothing about how much of a
// guess was right.
func bearerIs(r *http.Request, token string) bool {
	scheme, given, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(token)) == 1
}

// statusRecorder keeps the status a handler answered with.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// Limits on a DICOMweb connection. A client that opens one and sends
// nothing holds a goroutine and a socket until something gives up on it.
const (
	webHeaderTimeout = 10 * time.Second
	webIdleTimeout   = 2 * time.Minute
	webShutdownGrace = 5 * time.Second
)

// serveWeb starts the DICOMweb service of a store and returns its URL and
// a function that stops it. The port is taken before it returns, so an
// address in use is an error here and not a line printed later.
//
// Under -tls the service is HTTPS with the certificate the DIMSE side
// presents, and asks for a client certificate when -tls-ca is given, as
// that side does.
func serveWeb(address, token string, store *dcmstore.Store, tlsCfg *network.TLSConfig,
	report func(line string)) (url string, stop func(), err error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return "", nil, fmt.Errorf("listening for DICOMweb on %s: %w", address, err)
	}
	server := &http.Server{
		Handler:           webHandler(store, token, report),
		ReadHeaderTimeout: webHeaderTimeout,
		IdleTimeout:       webIdleTimeout,
	}

	scheme := "http"
	serve := func() error { return server.Serve(listener) }
	if tlsCfg != nil {
		scheme = "https"
		server.TLSConfig, err = webTLS(tlsCfg)
		if err != nil {
			_ = listener.Close()
			return "", nil, err
		}
		serve = func() error { return server.ServeTLS(listener, tlsCfg.CertFile, tlsCfg.KeyFile) }
	}

	go func() {
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "DICOMweb: %v\n", err)
		}
	}()
	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), webShutdownGrace)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return scheme + "://" + listener.Addr().String() + webPrefix, stop, nil
}

// webTLS is the TLS configuration of the HTTPS side: TLS 1.2 at least, and
// a client certificate issued by the CA when one is named.
func webTLS(cfg *network.TLSConfig) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile == "" {
		return config, nil
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("reading -tls-ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("-tls-ca %s holds no certificate", cfg.CAFile)
	}
	config.ClientCAs = pool
	config.ClientAuth = tls.RequireAndVerifyClientCert
	return config, nil
}
