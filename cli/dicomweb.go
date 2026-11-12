package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/dicomweb"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// webTokenEnv holds a bearer token for the DICOMweb commands. A token given
// as an option is readable in the process list by everyone on the machine.
const webTokenEnv = "DICOMWEB_TOKEN"

// webFlags are what every DICOMweb command needs to reach a service.
type webFlags struct {
	token    string
	user     string
	headers  stringList
	ca       string
	insecure bool
	timeout  int
	help     bool
}

// stringList collects a repeated option.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func (w *webFlags) add(fs *flag.FlagSet) {
	fs.BoolVar(&w.help, "h", false, "Show help for this command")
	fs.BoolVar(&w.help, "help", false, "Show help for this command")
	fs.StringVar(&w.token, "token", "", "Bearer token to send (or set "+webTokenEnv+")")
	fs.StringVar(&w.user, "user", "", "user:password to send as HTTP Basic credentials")
	fs.Var(&w.headers, "header", "Header to send with every request, as 'Name: value' (repeatable)")
	fs.StringVar(&w.ca, "tls-ca", "", "PEM file of CAs to verify the server against, instead of the system roots")
	fs.BoolVar(&w.insecure, "tls-insecure", false, "Do not verify the server's certificate (testing only)")
	fs.IntVar(&w.timeout, "timeout", 0, "Give up after this many seconds (default: no limit)")
}

// client builds the client for a service URL.
func (w *webFlags) client(base string, more ...dicomweb.Option) (*dicomweb.Client, error) {
	// A server's answer may hold retired attributes, which the library warns
	// of as it builds a data set. They are the server's, not something the
	// person searching can act on.
	config.SetLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	var options []dicomweb.Option
	if w.ca != "" || w.insecure {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = time.Minute
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: w.insecure}
		if w.ca != "" {
			pem, err := os.ReadFile(w.ca)
			if err != nil {
				return nil, fmt.Errorf("-tls-ca: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("-tls-ca: %s holds no PEM certificate", w.ca)
			}
			transport.TLSClientConfig.RootCAs = pool
		}
		options = append(options, dicomweb.WithHTTPClient(&http.Client{Transport: transport}))
	}

	token := w.token
	if token == "" {
		token = os.Getenv(webTokenEnv)
	}
	if token != "" && w.user != "" {
		return nil, errors.New("-token and -user are two ways to sign in; give one")
	}
	if token != "" {
		options = append(options, dicomweb.WithBearerToken(token))
	}
	if w.user != "" {
		user, password, ok := strings.Cut(w.user, ":")
		if !ok || user == "" {
			return nil, errors.New("-user is user:password")
		}
		options = append(options, dicomweb.WithBasicAuth(user, password))
	}
	for _, header := range w.headers {
		name, value, ok := strings.Cut(header, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("-header %q is not 'Name: value'", header)
		}
		options = append(options, dicomweb.WithHeader(strings.TrimSpace(name), strings.TrimSpace(value)))
	}
	return dicomweb.NewClient(base, append(options, more...)...)
}

// context is the command's deadline, when it was given one.
func (w *webFlags) context() (context.Context, context.CancelFunc) {
	if w.timeout > 0 {
		return context.WithTimeout(context.Background(), time.Duration(w.timeout)*time.Second)
	}
	return context.WithCancel(context.Background())
}

// webOptionsHelp is the part of each command's help the three share.
const webOptionsHelp = `  -token <token>     Bearer token to send. Better set in the environment as
                     DICOMWEB_TOKEN: an option is readable in the process list.
  -user <user:pass>  HTTP Basic credentials to send
  -header <'N: v'>   A header to send with every request (repeatable)
  -tls-ca <file>     PEM file of CAs to verify the server against, instead of
                     the system roots
  -tls-insecure      Do not verify the server's certificate (testing only)
  -timeout <seconds> Give up after this long (default: no limit)
  -h, --help         Show this help
`

func orStdout(out io.Writer) io.Writer {
	if out == nil {
		return os.Stdout
	}
	return out
}

// QidoCommand searches a DICOMweb service (QIDO-RS).
type QidoCommand struct {
	web     webFlags
	level   string
	study   string
	series  string
	match   stringList
	include stringList
	limit   int
	offset  int
	fuzzy   bool
	json    bool
	out     io.Writer
}

func (c *QidoCommand) Name() string { return "qido" }
func (c *QidoCommand) Description() string {
	return "Search a DICOMweb service for studies, series or instances (QIDO-RS)"
}

func (c *QidoCommand) AddFlags(fs *flag.FlagSet) {
	c.web.add(fs)
	fs.StringVar(&c.level, "level", "study", "What to search for: study, series or instance")
	fs.StringVar(&c.study, "study", "", "Search within this Study Instance UID")
	fs.StringVar(&c.series, "series", "", "Search within this Series Instance UID (with -study)")
	fs.Var(&c.match, "match", "Attribute=value to match, by keyword or eight-digit tag (repeatable)")
	fs.Var(&c.include, "include", "Attribute to return as well, by keyword or tag, or 'all' (repeatable)")
	fs.IntVar(&c.limit, "limit", 0, "Return at most this many results")
	fs.IntVar(&c.offset, "offset", 0, "Skip this many results first")
	fs.BoolVar(&c.fuzzy, "fuzzy", false, "Ask for fuzzy matching of names")
	fs.BoolVar(&c.json, "json", false, "Print the results as DICOM JSON")
}

func (c *QidoCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("qido", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	c.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("qido: %v\nRun 'go-dicom qido -h' for usage", err)
	}
	out := orStdout(c.out)
	if c.web.help {
		c.showHelp(out)
		return nil
	}
	if len(positional) != 1 {
		return fmt.Errorf("qido: expected the service URL\nRun 'go-dicom qido -h' for usage")
	}

	query := dicomweb.Query{
		Match: map[string]string{}, IncludeFields: c.include,
		Limit: c.limit, Offset: c.offset, FuzzyMatching: c.fuzzy,
	}
	for _, match := range c.match {
		attribute, value, ok := strings.Cut(match, "=")
		if attribute = strings.TrimSpace(attribute); !ok || attribute == "" {
			return fmt.Errorf("qido: -match %q is not Attribute=value", match)
		}
		query.Match[attribute] = value
	}

	client, err := c.web.client(positional[0])
	if err != nil {
		return fmt.Errorf("qido: %w", err)
	}
	ctx, cancel := c.web.context()
	defer cancel()

	var results []*dataset.Dataset
	switch c.level {
	case "study":
		if c.study != "" || c.series != "" {
			return fmt.Errorf("qido: -study and -series narrow a search for series or instances; " +
				"to find one study, use -match StudyInstanceUID=<uid>")
		}
		results, err = client.SearchForStudies(ctx, query)
	case "series":
		if c.series != "" {
			return fmt.Errorf("qido: to find one series, use -match SeriesInstanceUID=<uid>")
		}
		results, err = client.SearchForSeries(ctx, c.study, query)
	case "instance":
		results, err = client.SearchForInstances(ctx, c.study, c.series, query)
	default:
		return fmt.Errorf("qido: unknown -level %q; the levels are study, series and instance", c.level)
	}
	if err != nil {
		return fmt.Errorf("qido: %w", err)
	}

	if c.json {
		return printDICOMJSON(out, results)
	}
	for i, result := range results {
		fmt.Fprintf(out, "--- Result %d ---\n", i+1)
		printWebDataset(out, result, 1)
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "Found %d result(s)\n", len(results))
	return nil
}

func (c *QidoCommand) showHelp(out io.Writer) {
	fmt.Fprint(out, `Search a DICOMweb service for studies, series or instances (QIDO-RS)

USAGE:
  go-dicom qido [options] <url>

  <url> is the service: the address its studies resource lives under, such
  as https://pacs.example.org/dicom-web.

OPTIONS:
  -level <level>     What to search for: study (default), series or instance
  -study <uid>       Search within this study (series and instances)
  -series <uid>      Search within this series of -study (instances)
  -match <A=value>   An attribute to match, by keyword or eight-digit tag
                     (repeatable). The value may be a wildcard (Doe*) or a
                     range (20240101-20241231).
  -include <A>       An attribute to return as well, or 'all' (repeatable)
  -limit <n>         Return at most this many results
  -offset <n>        Skip this many results first
  -fuzzy             Ask for fuzzy matching of names
  -json              Print the results as DICOM JSON
`+webOptionsHelp+`
EXAMPLES:
  go-dicom qido -match "PatientName=Doe*" https://pacs.example.org/dicom-web
  go-dicom qido -match StudyDate=20240101-20241231 -match ModalitiesInStudy=CT -limit 20 <url>
  go-dicom qido -level series -study 1.2.840.1 <url>
  go-dicom qido -level instance -study 1.2.840.1 -series 1.2.840.2 -json <url>
`)
}

// WadoCommand retrieves instances from a DICOMweb service (WADO-RS).
type WadoCommand struct {
	web      webFlags
	study    string
	series   string
	instance string
	metadata bool
	syntax   string
	out      io.Writer
}

func (c *WadoCommand) Name() string { return "wado" }
func (c *WadoCommand) Description() string {
	return "Retrieve a study, series or instance from a DICOMweb service (WADO-RS)"
}

func (c *WadoCommand) AddFlags(fs *flag.FlagSet) {
	c.web.add(fs)
	fs.StringVar(&c.study, "study", "", "Study Instance UID to retrieve (required)")
	fs.StringVar(&c.series, "series", "", "Retrieve only this series of the study")
	fs.StringVar(&c.instance, "instance", "", "Retrieve only this instance of the series")
	fs.BoolVar(&c.metadata, "metadata", false, "Print the metadata as DICOM JSON instead of downloading")
	fs.StringVar(&c.syntax, "transfer-syntax", "", "Transfer Syntax UID to ask for (default: as stored)")
}

func (c *WadoCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("wado", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	c.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("wado: %v\nRun 'go-dicom wado -h' for usage", err)
	}
	out := orStdout(c.out)
	if c.web.help {
		c.showHelp(out)
		return nil
	}
	if c.study == "" {
		return fmt.Errorf("wado: -study names what to retrieve\nRun 'go-dicom wado -h' for usage")
	}
	if c.instance != "" && c.series == "" {
		return fmt.Errorf("wado: -instance needs the -series it belongs to")
	}
	want := 2
	if c.metadata {
		want = 1
	}
	if len(positional) != want {
		if c.metadata {
			return fmt.Errorf("wado: -metadata takes the service URL and nothing else")
		}
		return fmt.Errorf("wado: expected the service URL and an output directory\nRun 'go-dicom wado -h' for usage")
	}

	client, err := c.web.client(positional[0], dicomweb.WithRetrieveTransferSyntax(c.syntax))
	if err != nil {
		return fmt.Errorf("wado: %w", err)
	}
	ctx, cancel := c.web.context()
	defer cancel()

	if c.metadata {
		results, err := client.RetrieveMetadata(ctx, c.study, c.series, c.instance)
		if err != nil {
			return fmt.Errorf("wado: %w", err)
		}
		return printDICOMJSON(out, results)
	}

	output := positional[1]
	count := 0
	err = client.RetrieveEach(ctx, c.study, c.series, c.instance, func(part dicomweb.Part) error {
		ds, err := part.Dataset()
		if err != nil {
			return err
		}
		// Made once there is something to put in it: a study the server
		// does not have leaves nothing behind.
		if err := os.MkdirAll(output, 0o755); err != nil {
			return err
		}
		count++
		// The file is named by the server's word for it. Only a UID is
		// taken as a name: anything else could name a path.
		name := datasetText(ds, tag.New(0x0008, 0x0018))
		if !isUID(name) {
			name = fmt.Sprintf("instance-%04d", count)
		}
		path := filepath.Join(output, name+".dcm")
		// As the server sent it, byte for byte.
		if err := os.WriteFile(path, part.Data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s\n", path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("wado: after %d instance(s): %w", count, err)
	}
	fmt.Fprintf(out, "Retrieved %d instance(s) to %s\n", count, output)
	return nil
}

// isUID reports whether s has the form of a UID: digits and dots, at most 64.
func isUID(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return !strings.Contains(s, "..")
}

func (c *WadoCommand) showHelp(out io.Writer) {
	fmt.Fprint(out, `Retrieve a study, series or instance from a DICOMweb service (WADO-RS)

USAGE:
  go-dicom wado [options] -study <uid> <url> <output-dir>
  go-dicom wado [options] -study <uid> -metadata <url>

  Each instance is written to <output-dir> as <SOP Instance UID>.dcm, as the
  server sent it.

OPTIONS:
  -study <uid>       The study to retrieve (required)
  -series <uid>      Only this series of the study
  -instance <uid>    Only this instance of -series
  -metadata          Print the metadata as DICOM JSON, without the pixel
                     data, instead of downloading
  -transfer-syntax <uid>
                     Ask for the instances in this transfer syntax
                     (default: each as it is stored)
`+webOptionsHelp+`
EXAMPLES:
  go-dicom wado -study 1.2.840.1 https://pacs.example.org/dicom-web study/
  go-dicom wado -study 1.2.840.1 -series 1.2.840.2 <url> series/
  go-dicom wado -study 1.2.840.1 -metadata <url> > study.json
`)
}

// StowCommand stores files to a DICOMweb service (STOW-RS).
type StowCommand struct {
	web   webFlags
	study string
	out   io.Writer

	// batchFiles and batchBytes bound one request; zero is the default.
	batchFiles int
	batchBytes int64
}

// One request holds its files in memory, here and at the server, so a
// directory goes in several.
const (
	stowBatchFiles = 100
	stowBatchBytes = 64 << 20
)

func (c *StowCommand) Name() string { return "stow" }
func (c *StowCommand) Description() string {
	return "Store DICOM files to a DICOMweb service (STOW-RS)"
}

func (c *StowCommand) AddFlags(fs *flag.FlagSet) {
	c.web.add(fs)
	fs.StringVar(&c.study, "study", "", "Store into this Study Instance UID; the server refuses instances of another")
}

func (c *StowCommand) Execute(args []string) error {
	flags := flag.NewFlagSet("stow", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	c.AddFlags(flags)
	positional, err := ParseArgs(flags, args)
	if err != nil {
		return fmt.Errorf("stow: %v\nRun 'go-dicom stow -h' for usage", err)
	}
	out := orStdout(c.out)
	if c.web.help {
		c.showHelp(out)
		return nil
	}
	if len(positional) < 2 {
		return fmt.Errorf("stow: expected the service URL and the files to store\nRun 'go-dicom stow -h' for usage")
	}
	client, err := c.web.client(positional[0])
	if err != nil {
		return fmt.Errorf("stow: %w", err)
	}

	var files []string
	for _, path := range positional[1:] {
		found, err := dicomFilesUnder(path)
		if err != nil {
			return fmt.Errorf("stow: %w", err)
		}
		files = append(files, found...)
	}
	if len(files) == 0 {
		return fmt.Errorf("stow: no DICOM file to store")
	}

	maxFiles, maxBytes := c.batchFiles, c.batchBytes
	if maxFiles <= 0 {
		maxFiles = stowBatchFiles
	}
	if maxBytes <= 0 {
		maxBytes = stowBatchBytes
	}
	ctx, cancel := c.web.context()
	defer cancel()

	var stored, failed int
	var batch [][]byte
	var size int64
	send := func() error {
		if len(batch) == 0 {
			return nil
		}
		result, err := client.Store(ctx, c.study, batch...)
		var status *dicomweb.StatusError
		// 409 is the server keeping none of them, and it says why for each.
		refusedWhole := errors.As(err, &status) && status.StatusCode == http.StatusConflict && result != nil
		if err != nil && !refusedWhole {
			return err
		}
		for _, instance := range result.Failed {
			fmt.Fprintf(out, "  refused %s: %s\n", instance.SOPInstanceUID, stowReason(instance.FailureReason))
		}
		for _, instance := range result.Stored {
			if instance.WarningReason != 0 {
				fmt.Fprintf(out, "  stored %s with a warning: %s\n", instance.SOPInstanceUID, stowReason(instance.WarningReason))
			}
		}
		refused := len(result.Failed)
		if err != nil && refused == 0 {
			// Refused whole, by a server that did not itemize.
			refused = len(batch)
			fmt.Fprintf(out, "  %v\n", err)
		}
		failed += refused
		stored += len(batch) - refused
		batch, size = nil, 0
		return nil
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("stow: %w", err)
		}
		if len(batch) > 0 && (len(batch) >= maxFiles || size+int64(len(data)) > maxBytes) {
			if err := send(); err != nil {
				return fmt.Errorf("stow: after %d instance(s): %w", stored, err)
			}
		}
		batch = append(batch, data)
		size += int64(len(data))
	}
	if err := send(); err != nil {
		return fmt.Errorf("stow: after %d instance(s): %w", stored, err)
	}

	fmt.Fprintf(out, "Stored %d instance(s)", stored)
	if failed > 0 {
		fmt.Fprintf(out, ", %d refused", failed)
	}
	fmt.Fprintln(out)
	if failed > 0 {
		return fmt.Errorf("stow: the server refused %d instance(s)", failed)
	}
	return nil
}

// stowReason names a STOW-RS failure or warning reason (PS3.18 Table
// 10.5.3-1 and the statuses of PS3.4 Annex B it borrows).
func stowReason(code uint16) string {
	meaning := map[uint16]string{
		0x0110: "processing failure",
		0x0122: "SOP class not supported",
		0xA700: "out of resources",
		0xA900: "the data set does not match its SOP class",
		0xC000: "cannot understand",
		0xC122: "transfer syntax not supported",
		0xC123: "storage would exceed a limit",
		0xC409: "the instance is in another study than the request names",
		0xB000: "attributes were coerced",
		0xB006: "elements were discarded",
		0xB007: "the data set does not match its SOP class",
	}[code]
	if meaning == "" {
		return fmt.Sprintf("reason %04XH", code)
	}
	return fmt.Sprintf("%s (%04XH)", meaning, code)
}

// dicomFilesUnder returns path when it is a DICOM file, or the DICOM files
// under it when it is a directory. A file named outright that is not DICOM
// is an error; one met in a directory is passed over.
func dicomFilesUnder(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if _, err := scanForStore(path); err != nil {
			return nil, fmt.Errorf("%s is not a DICOM file: %v", path, err)
		}
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		// A DICOMDIR indexes the media it is on, and is not an instance.
		if file, err := scanForStore(p); err == nil && file.sopClass != mediaStorageDirectory {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func (c *StowCommand) showHelp(out io.Writer) {
	fmt.Fprint(out, `Store DICOM files to a DICOMweb service (STOW-RS)

USAGE:
  go-dicom stow [options] <url> <file-or-directory>...

  A directory is walked, and what is not a DICOM file in it is passed over.
  The files are sent as they are, in requests of up to 100 files or 64 MiB.
  The command fails if the server refuses any instance, and says which.

OPTIONS:
  -study <uid>       Store into this study; the server refuses an instance
                     of another
`+webOptionsHelp+`
EXAMPLES:
  go-dicom stow https://pacs.example.org/dicom-web study/
  go-dicom stow -study 1.2.840.1 <url> a.dcm b.dcm
  DICOMWEB_TOKEN=... go-dicom stow <url> study/
`)
}

// printDICOMJSON prints data sets as a DICOM JSON array.
func printDICOMJSON(out io.Writer, results []*dataset.Dataset) error {
	objects := make([]any, 0, len(results))
	for _, result := range results {
		object, err := result.ToDICOMJSON()
		if err != nil {
			return err
		}
		objects = append(objects, object)
	}
	encoded, err := json.MarshalIndent(objects, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(encoded))
	return err
}

// printWebDataset prints a data set a line an element, its sequences
// indented under them.
func printWebDataset(out io.Writer, ds *dataset.Dataset, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, elem := range ds.ElementsInTagOrder() {
		t, _ := elem.GetTag().(tag.Tag)
		name := t.GetName()
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			fmt.Fprintf(out, "%s%s %-2s %-32s (%d item(s))\n", indent, t, elem.GetVR(), name, seq.Length())
			for _, item := range seq.Items() {
				if inner, ok := item.(*dataset.Dataset); ok {
					printWebDataset(out, inner, depth+1)
				}
			}
			continue
		}
		fmt.Fprintf(out, "%s%s %-2s %-32s %s\n", indent, t, elem.GetVR(), name, webValue(elem))
	}
}

// webValue is an element's value as text.
func webValue(elem *dataelem.DataElement) string {
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		return fmt.Sprint(elem.GetValue())
	}
	// A binary number VR holds as many values as fit.
	size, format := 0, func([]byte) string { return "" }
	switch elem.GetVR() {
	case dataelem.US:
		size, format = 2, func(b []byte) string { return fmt.Sprint(binary.LittleEndian.Uint16(b)) }
	case dataelem.SS:
		size, format = 2, func(b []byte) string { return fmt.Sprint(int16(binary.LittleEndian.Uint16(b))) }
	case dataelem.UL:
		size, format = 4, func(b []byte) string { return fmt.Sprint(binary.LittleEndian.Uint32(b)) }
	case dataelem.SL:
		size, format = 4, func(b []byte) string { return fmt.Sprint(int32(binary.LittleEndian.Uint32(b))) }
	case dataelem.FL:
		size, format = 4, func(b []byte) string {
			return fmt.Sprint(math.Float32frombits(binary.LittleEndian.Uint32(b)))
		}
	case dataelem.FD:
		size, format = 8, func(b []byte) string {
			return fmt.Sprint(math.Float64frombits(binary.LittleEndian.Uint64(b)))
		}
	}
	if size > 0 && len(raw) > 0 && len(raw)%size == 0 {
		values := make([]string, 0, len(raw)/size)
		for i := 0; i < len(raw); i += size {
			values = append(values, format(raw[i:i+size]))
		}
		return strings.Join(values, "\\")
	}
	// Text is padded to an even length, which is not part of the value.
	return strings.TrimRight(formatValueRaw(string(elem.GetVR()), raw), " ")
}
