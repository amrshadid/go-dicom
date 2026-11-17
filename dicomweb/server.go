package dicomweb

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
)

// Backend is the archive a Server serves.
//
// The server speaks HTTP and leaves what is stored, and how it is matched,
// to the archive, so one archive answers a DIMSE peer and an HTTP client
// alike. *dcmstore.Store is a Backend as it is.
type Backend interface {
	// Query answers a search as a C-FIND is answered (PS3.4 C.2.2). The
	// identifier carries Query/Retrieve Level (0008,0052), STUDY, SERIES or
	// IMAGE; an attribute with a value is to be matched, and one without is
	// to be returned. The result is one data set for each study, series or
	// instance that matches, in an order that does not change between calls,
	// which is what lets a client page through them.
	//
	// Text in the identifier is UTF-8, held as Go strings. Text in a result
	// is in the character set the result declares.
	Query(ctx context.Context, identifier *dataset.Dataset) ([]*dataset.Dataset, error)
}

// Server is the origin-server side of DICOMweb (PS3.18): an http.Handler
// that answers the services over a Backend.
//
// It serves the search service, QIDO-RS (PS3.18 10.6):
//
//	GET /studies
//	GET /studies/{study}/series
//	GET /studies/{study}/series/{series}/instances
//	GET /studies/{study}/instances
//	GET /series
//	GET /instances
//
// The paths are relative to wherever the handler is mounted, so a service at
// /dicom-web is http.StripPrefix("/dicom-web", server).
//
// A Server holds no state of its own and is safe for use by several
// goroutines when its Backend is. It does not authenticate: it is a handler,
// and what guards it is the handler it is wrapped in.
type Server struct {
	backend Backend
}

// NewServer returns a server over backend.
func NewServer(backend Backend) *Server {
	return &Server{backend: backend}
}

// requestError is a request the server will not answer, and the status that
// says why. Its message is sent to the client, so it describes the request
// and never the archive.
type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string { return e.message }

// refuse makes a requestError.
func refuse(status int, format string, args ...any) *requestError {
	return &requestError{status: status, message: fmt.Sprintf(format, args...)}
}

// ServeHTTP answers one request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A response is data for a program. A browser that guesses at its type
	// could run what a stored name holds.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	target, err := searchTargetOf(r.URL.Path)
	if err != nil {
		s.fail(w, err)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.fail(w, refuse(http.StatusMethodNotAllowed, "%s is not a method of this resource", r.Method))
		return
	}
	s.search(w, r, target)
}

// fail writes the answer to a request that was not served.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var refused *requestError
	if !errors.As(err, &refused) {
		// What went wrong inside the archive may name its files. The client
		// is told that it failed, not how.
		refused = refuse(http.StatusInternalServerError, "the archive could not answer the request")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(refused.status)
	_, _ = w.Write([]byte(refused.message + "\n"))
}

// pathSegments splits a request path into its segments. A trailing slash
// names the same resource.
func pathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// maxUIDLength is the longest a UID may be (PS3.5 9.1).
const maxUIDLength = 64

// validUID reports whether s has the form of a UID: components of digits
// separated by dots, 64 characters at most (PS3.5 9.1).
//
// A UID from a path is checked before it reaches the archive, where it may
// name a directory.
func validUID(s string) bool {
	if s == "" || len(s) > maxUIDLength {
		return false
	}
	digits := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && digits > 0:
			digits = 0
		default:
			return false
		}
	}
	return digits > 0
}

// DICOM JSON, and the plain JSON type a client may ask for in its place
// (PS3.18 8.7.3.2).
const (
	mediaDICOMJSON = "application/dicom+json"
	mediaJSON      = "application/json"
)

// jsonContentType chooses the media type of a JSON answer from what the
// request accepts: DICOM JSON, or plain JSON when that is all it takes.
// A request with no Accept takes anything (RFC 9110 12.5.1). The accept
// query parameter stands in for the header where a client cannot set one
// (PS3.18 8.3.3.1).
func jsonContentType(r *http.Request) (string, *requestError) {
	accept := strings.Join(r.Header.Values("Accept"), ",")
	if parameter := r.URL.Query().Get("accept"); parameter != "" {
		accept = parameter
	}
	if strings.TrimSpace(accept) == "" {
		return mediaDICOMJSON, nil
	}

	plain := false
	for _, entry := range strings.Split(accept, ",") {
		media, parameters, err := mime.ParseMediaType(entry)
		if err != nil {
			continue
		}
		// q=0 names a type the client will not take.
		if q, err := strconv.ParseFloat(parameters["q"], 64); err == nil && q == 0 {
			continue
		}
		switch media {
		case mediaDICOMJSON, "application/*", "*/*":
			return mediaDICOMJSON, nil
		case mediaJSON:
			plain = true
		}
	}
	if plain {
		return mediaJSON, nil
	}
	return "", refuse(http.StatusNotAcceptable,
		"search results are %s; the request accepts %q", mediaDICOMJSON, accept)
}
