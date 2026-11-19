package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/matching"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// WLMSCPCommand implements the wlmscp CLI command: a Modality Worklist SCP
// that serves the worklist entries kept as files in a directory, as dcmtk's
// wlmscpfs does.
//
// It answers Modality Worklist C-FIND (PS3.4 K.6.1) and C-ECHO, and refuses
// every other SOP class at negotiation: a server that accepted a storage or a
// query/retrieve context here would accept requests it has nothing to answer
// with.
type WLMSCPCommand struct {
	tls     tlsFlags
	events  eventsFlag
	allow   allowFlag
	aeTitle string
	port    int
	dir     string
}

func (c *WLMSCPCommand) Name() string { return "wlmscp" }
func (c *WLMSCPCommand) Description() string {
	return "DICOM Modality Worklist SCP (serve worklist files)"
}

func (c *WLMSCPCommand) AddFlags(fs *flag.FlagSet) {
	addServerTLSFlags(fs, &c.tls)
	c.events.add(fs)
	c.allow.add(fs)
	fs.StringVar(&c.aeTitle, "aet", "WLMSCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
	fs.StringVar(&c.dir, "dir", "",
		"Directory of worklist files, one entry per file whatever its name, with File Meta "+
			"Information or without (dcmtk's wlistdb/OFFIS is one), read again at every query so a "+
			"file added, changed or removed while the server runs is served as it is; a file that "+
			"is not a worklist entry is not served, and is reported once (required)")
}

func (c *WLMSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("wlmscp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tlsCfg, err := c.tls.config()
	if err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: the worklist directory is given with -dir", fs.Arg(0))
	}
	if c.dir == "" {
		return errors.New("usage: wlmscp -dir <worklist directory> [options]")
	}
	if info, err := os.Stat(c.dir); err != nil {
		return fmt.Errorf("the worklist directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("the worklist directory %s is not a directory", c.dir)
	}
	events, closeEvents, err := c.events.open()
	if err != nil {
		return err
	}
	defer closeEvents()

	// The first reading is reported after the banner rather than before it,
	// so the files not served are listed under the directory they are in.
	var firstReading []string
	source := newWorklistDirectory(c.dir, func(format string, args ...any) {
		firstReading = append(firstReading, fmt.Sprintf(format, args...))
	})
	entries, err := source.entries()
	if err != nil {
		return err
	}

	scp := network.NewSCP(network.SCPConfig{
		TLS:     tlsCfg,
		Events:  events,
		AETitle: c.aeTitle,
		Port:    c.port,
		Admit:   c.allow.admission(printRefusal),
	})
	scp.SetHandler(&network.WorklistHandler{
		OnWorklist: func(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
			return source.answer(ctx, query)
		},
	})
	// Verification as well, since a modality checks a worklist server with
	// C-ECHO before it queries it, and wlmscpfs answers one.
	scp.SetSupportedAbstractSyntaxes([]string{
		network.VerificationSOPClassUID,
		network.ModalityWorklistInformationModelFindUID,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		scp.Close()
	}()

	fmt.Printf("Starting DICOM Modality Worklist SCP\n")
	fmt.Printf("  AE Title:  %s\n", c.aeTitle)
	fmt.Printf("  Port:      %d\n", c.port)
	fmt.Printf("  Worklist:  %s (%d entries)\n", c.dir, len(entries))
	if allowed := c.allow.describe(); allowed != "" {
		fmt.Printf("  Allow:     %s\n", allowed)
	}
	for _, line := range firstReading {
		fmt.Print(line)
	}
	// Nothing reads the directory again until the server answers a query.
	source.report = func(format string, args ...any) { fmt.Printf(format, args...) }
	fmt.Println("Listening for worklist queries... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// tagScheduledProcedureStepSequence is (0040,0100), the attribute that makes a
// data set a worklist entry: the step a modality is asked to perform.
var tagScheduledProcedureStepSequence = tag.New(0x0040, 0x0100)

// worklistOptions are how the server reads a query. Basic Worklist has no
// Query/Retrieve Level (PS3.4 K.6.1.2); a query that carries one is not
// refused for it, and it is neither matched nor returned, as wlmscpfs does,
// since no entry holds one to compare.
var worklistOptions = matching.Options{
	Skip: func(parent tag.Tag, elem *dataelem.DataElement) bool {
		t, _ := elem.Tag()
		return parent == 0 && t == tag.New(0x0008, 0x0052)
	},
}

// worklistDirectory is the worklist a directory holds: one entry per file.
//
// The directory is read again at every query, so a file added, changed or
// removed while the server runs is served as it is then; that is how a RIS
// that writes worklist files, or a person editing one, expects wlmscpfs to
// behave. A file is parsed again only when its size or modification time
// has changed, so a query does not read the whole worklist every time.
//
// Every regular file in the directory is read, whatever its name: wlmscpfs
// takes *.wl only, and files written by other tools are .dcm or have no
// extension at all. A file that is not a worklist entry — not DICOM, or a
// data set with no Scheduled Procedure Step Sequence, such as an image or
// wlmscpfs's empty lockfile — is not served, and is reported once with the
// reason, and again only if it changes. One unreadable file does not stop the
// rest of the worklist from being served. Names starting with a dot and
// subdirectories are passed over.
type worklistDirectory struct {
	dir    string
	report func(format string, args ...any)

	mu    sync.Mutex
	files map[string]worklistFile
}

// worklistFile is what one file held when it was last read.
type worklistFile struct {
	size    int64
	modTime time.Time
	entry   *dataset.Dataset // nil when the file is not a worklist entry
}

func newWorklistDirectory(dir string, report func(format string, args ...any)) *worklistDirectory {
	return &worklistDirectory{dir: dir, report: report, files: map[string]worklistFile{}}
}

// entries returns the worklist entries the directory holds now, in the order
// of their file names, so one query asked twice is answered in one order.
func (w *worklistDirectory) entries() ([]*dataset.Dataset, error) {
	listing, err := os.ReadDir(w.dir)
	if err != nil {
		return nil, fmt.Errorf("reading the worklist directory: %w", err)
	}
	sort.Slice(listing, func(i, j int) bool { return listing[i].Name() < listing[j].Name() })

	w.mu.Lock()
	defer w.mu.Unlock()

	present := make(map[string]bool, len(listing))
	var entries []*dataset.Dataset
	for _, item := range listing {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		// Stat rather than the listing's own type, so a link to a file is
		// served as the file.
		info, err := os.Stat(filepath.Join(w.dir, name))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		present[name] = true

		cached, ok := w.files[name]
		if !ok || cached.size != info.Size() || !cached.modTime.Equal(info.ModTime()) {
			entry, reason := readWorklistEntry(filepath.Join(w.dir, name))
			if reason != "" {
				w.report("  Not served: %s: %s\n", name, reason)
			}
			cached = worklistFile{size: info.Size(), modTime: info.ModTime(), entry: entry}
			w.files[name] = cached
		}
		if cached.entry != nil {
			entries = append(entries, cached.entry)
		}
	}
	for name := range w.files {
		if !present[name] {
			delete(w.files, name)
		}
	}
	return entries, nil
}

// readWorklistEntry reads one file, with File Meta Information or without, as
// wlmscpfs takes them. reason says why it is not a worklist entry.
func readWorklistEntry(path string) (*dataset.Dataset, string) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err.Error()
	}
	defer func() { _ = file.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(file))
	if err != nil {
		return nil, fmt.Sprintf("not a DICOM file (%v)", err)
	}
	ds := df.GetDataset()
	if len(ds.GetAll()) == 0 {
		// The reader is lenient with what it is given, and an empty file or a
		// line of text reads as a data set of nothing.
		return nil, "no DICOM data set in it"
	}
	steps, err := ds.GetSequence(tagScheduledProcedureStepSequence)
	if err != nil || steps == nil || steps.Length() == 0 {
		return nil, "no Scheduled Procedure Step Sequence (0040,0100), so it schedules nothing"
	}
	return ds, ""
}

// answer matches a query against the worklist and builds the answers: the
// keys asked for, with each matching entry's values, the scheduled step cut to
// the keys asked for in it (PS3.4 K.6.1.2.2).
func (w *worklistDirectory) answer(ctx context.Context, query *dataset.Dataset) ([]*dataset.Dataset, error) {
	id, err := matching.Compile(query, worklistOptions)
	if err != nil {
		return nil, err
	}
	entries, err := w.entries()
	if err != nil {
		return nil, err
	}

	var answers []*dataset.Dataset
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !id.MatchDataset(entry) {
			continue
		}
		answer, err := matching.Response(query, entry, worklistOptions)
		if err != nil {
			return nil, err
		}
		answers = append(answers, answer)
	}

	caller := "a caller"
	if info := network.AssociationInfoFromContext(ctx); info != nil && info.CallingAE != "" {
		caller = info.CallingAE
	}
	w.report("  Query from %s: %d of %d entries matched\n", caller, len(answers), len(entries))
	return answers, nil
}
