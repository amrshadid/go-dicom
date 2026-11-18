package dicomweb

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	// Locate names the instances the archive holds of a study: all of them,
	// those of one series when series is given, or the one instance when
	// instance is given too. It returns their SOP Instance UIDs in an order
	// that does not change between calls, and none, with no error, when it
	// holds nothing of what was named.
	Locate(ctx context.Context, study, series, instance string) ([]string, error)

	// Open returns the Part 10 file of an instance Locate named, as the
	// archive holds it. The caller closes it.
	Open(ctx context.Context, sopInstanceUID string) (io.ReadSeekCloser, error)

	// Put keeps an instance, replacing one the archive already holds under
	// the same SOP Instance UID. The data set knows the transfer syntax it
	// arrived in, and the archive keeps a compressed one as it came.
	Put(ctx context.Context, instance *dataset.Dataset) error
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
// and the retrieve service, WADO-RS (PS3.18 10.4), for instances and for
// their metadata:
//
//	GET /studies/{study}
//	GET /studies/{study}/series/{series}
//	GET /studies/{study}/series/{series}/instances/{instance}
//	GET each of these with /metadata
//	GET /studies/{study}/series/{series}/instances/{instance}/frames/{list}
//
// and the store service, STOW-RS (PS3.18 10.5):
//
//	POST /studies
//	POST /studies/{study}
//
// The paths are relative to wherever the handler is mounted, so a service at
// /dicom-web is http.StripPrefix("/dicom-web", server).
//
// A Server holds no state of its own and is safe for use by several
// goroutines when its Backend is. It does not authenticate: it is a handler,
// and what guards it is the handler it is wrapped in.
type Server struct {
	backend  Backend
	maxStore int64
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithMaxStoreBytes bounds the body of one store request. A request that
// runs past it is refused with 413.
func WithMaxStoreBytes(n int64) ServerOption {
	return func(server *Server) {
		if n > 0 {
			server.maxStore = n
		}
	}
}

// NewServer returns a server over backend.
func NewServer(backend Backend, options ...ServerOption) *Server {
	server := &Server{backend: backend, maxStore: DefaultMaxStoreBytes}
	for _, option := range options {
		option(server)
	}
	return server
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

	retrieved, isRetrieve, err := retrieveTargetOf(r.URL.Path)
	var searched searchTarget
	if !isRetrieve {
		searched, err = searchTargetOf(r.URL.Path)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	// The studies resource and a study are where instances are stored to
	// as well as searched for and retrieved from.
	storesTo := (!isRetrieve && searched.level == levelStudy) ||
		(isRetrieve && retrieved.series == "" && !retrieved.metadata)
	switch {
	case r.Method == http.MethodPost && storesTo:
		s.store(w, r, retrieved.study)
	case r.Method != http.MethodGet:
		allow := http.MethodGet
		if storesTo {
			allow += ", " + http.MethodPost
		}
		w.Header().Set("Allow", allow)
		s.fail(w, refuse(http.StatusMethodNotAllowed, "%s is not a method of this resource", r.Method))
	case !isRetrieve:
		s.search(w, r, searched)
	case retrieved.metadata:
		s.metadata(w, r, retrieved)
	case retrieved.frames != "":
		s.frames(w, r, retrieved)
	default:
		s.retrieve(w, r, retrieved)
	}
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
