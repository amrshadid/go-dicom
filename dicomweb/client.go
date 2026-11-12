package dicomweb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
)

// DefaultMaxResponseBytes bounds the body of one response when the client is
// not given another bound: 4 GiB, more than any study a client should hold
// in memory at once, and less than whatever a server chooses to send.
const DefaultMaxResponseBytes = 4 << 30

// maxErrorBody is how much of an error response's body is kept. It is where
// a server says why, and a server may say a great deal.
const maxErrorBody = 4 << 10

// Client talks to one DICOMweb service: the URL under which its studies,
// series and instances resources live.
//
// A Client is safe for use by several goroutines.
type Client struct {
	base        *url.URL
	http        *http.Client
	header      http.Header
	maxResponse int64
	transfer    string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient uses c for every request: its transport carries the TLS
// configuration, proxies and timeouts.
func WithHTTPClient(c *http.Client) Option {
	return func(client *Client) {
		if c != nil {
			client.http = c
		}
	}
}

// WithBearerToken sends an OAuth 2.0 bearer token with every request.
func WithBearerToken(token string) Option {
	return WithHeader("Authorization", "Bearer "+token)
}

// WithBasicAuth sends HTTP Basic credentials with every request. Over plain
// http they cross the network readable.
func WithBasicAuth(user, password string) Option {
	return WithHeader("Authorization",
		"Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
}

// WithHeader sends a header with every request, for what a deployment asks
// for beyond the standard: an API key, a tenant.
func WithHeader(name, value string) Option {
	return func(client *Client) { client.header.Set(name, value) }
}

// WithMaxResponseBytes bounds the body of one response. A response that
// runs past it is an error and is not read further.
func WithMaxResponseBytes(n int64) Option {
	return func(client *Client) {
		if n > 0 {
			client.maxResponse = n
		}
	}
}

// WithRetrieveTransferSyntax asks WADO-RS for instances in one transfer
// syntax, by UID. The default is "*", which asks for each as it is stored.
func WithRetrieveTransferSyntax(uid string) Option {
	return func(client *Client) {
		if uid != "" {
			client.transfer = uid
		}
	}
}

// NewClient returns a client for the DICOMweb service at baseURL, such as
// https://pacs.example.org/dicom-web. The URL is absolute, http or https.
//
// With no WithHTTPClient option, requests have no overall time limit, since
// retrieving a study may properly take long: the context of each call is
// what stops it. Waiting for a server to begin its answer is limited to a
// minute.
func NewClient(baseURL string, options ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("dicomweb: the service URL %q: %w", baseURL, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("dicomweb: the service URL %q is not an absolute http or https URL", baseURL)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = time.Minute
	client := &Client{
		base:        u,
		http:        &http.Client{Transport: transport},
		header:      http.Header{},
		maxResponse: DefaultMaxResponseBytes,
		transfer:    "*",
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

// StatusError is a response whose status the request did not want.
type StatusError struct {
	StatusCode int
	Status     string
	// URL is the resource asked for, without its query, which may hold
	// identifiers.
	URL string
	// Body is the start of what the server sent, which is where it says why.
	Body string
}

func (e *StatusError) Error() string {
	message := fmt.Sprintf("dicomweb: %s answered %s", e.URL, e.Status)
	if body := strings.TrimSpace(e.Body); body != "" {
		message += ": " + body
	}
	return message
}

// resource builds the URL of a resource under the service, each segment
// escaped.
func (c *Client) resource(segments ...string) *url.URL {
	u := *c.base
	path, escaped := c.base.Path, c.base.EscapedPath()
	for _, segment := range segments {
		// A UID holds nothing that needs escaping; anything else a caller
		// passes must not be able to add a segment of its own.
		path += "/" + segment
		escaped += "/" + url.PathEscape(segment)
	}
	u.Path, u.RawPath = path, escaped
	return &u
}

// do sends a request and returns the response when its status is one of
// want. Any other status is a *StatusError. The caller closes the body.
func (c *Client) do(ctx context.Context, method string, u *url.URL, accept, contentType string,
	body io.Reader, want ...int) (*http.Response, error) {
	response, err := c.send(ctx, method, u, accept, contentType, body)
	if err != nil {
		return nil, err
	}
	for _, status := range want {
		if response.StatusCode == status {
			return response, nil
		}
	}
	defer func() { _ = response.Body.Close() }()
	start, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	return nil, statusError(u, response, start)
}

// send sends a request and returns the response, whatever its status.
func (c *Client) send(ctx context.Context, method string, u *url.URL, accept, contentType string,
	body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("dicomweb: %w", err)
	}
	for name, values := range c.header {
		request.Header[name] = append([]string(nil), values...)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("dicomweb: %w", err)
	}
	return response, nil
}

// statusError is the error for a response, with the start of its body.
func statusError(u *url.URL, response *http.Response, body []byte) *StatusError {
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody]
	}
	bare := *u
	bare.RawQuery = ""
	return &StatusError{
		StatusCode: response.StatusCode, Status: response.Status, URL: bare.String(), Body: string(body),
	}
}

// limited reads a response body up to the client's bound, and reports
// running past it as an error, not as the end of the body.
func (c *Client) limited(body io.Reader) io.Reader {
	return &boundedReader{r: body, left: c.maxResponse, max: c.maxResponse}
}

type boundedReader struct {
	r         io.Reader
	left, max int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.left <= 0 {
		// One byte more decides between a body of exactly the bound and one
		// that runs past it.
		var one [1]byte
		if n, _ := b.r.Read(one[:]); n > 0 {
			return 0, fmt.Errorf("dicomweb: the response is larger than the %d bytes allowed", b.max)
		}
		return 0, io.EOF
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}

// jsonDatasets reads a DICOM JSON response: an array of data sets, or a
// single one.
func (c *Client) jsonDatasets(response *http.Response) ([]*dataset.Dataset, error) {
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	data, err := io.ReadAll(c.limited(response.Body))
	if err != nil {
		return nil, fmt.Errorf("dicomweb: reading the response: %w", err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}

	var objects []json.RawMessage
	if data[0] == '{' {
		objects = []json.RawMessage{data}
	} else if err := json.Unmarshal(data, &objects); err != nil {
		return nil, fmt.Errorf("dicomweb: the response is not DICOM JSON: %w", err)
	}
	out := make([]*dataset.Dataset, 0, len(objects))
	for i, object := range objects {
		ds := dataset.NewDataset()
		if err := ds.FromDICOMJSONString(string(object)); err != nil {
			return nil, fmt.Errorf("dicomweb: result %d: %w", i, err)
		}
		out = append(out, ds)
	}
	return out, nil
}
