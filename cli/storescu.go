package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// StoreSCUCommand implements the storescu CLI command.
type StoreSCUCommand struct {
	tls       tlsFlags
	events    eventsFlag
	tlsConfig *network.TLSConfig
	// eventLog is where each association reports, nil with no -events.
	eventLog  *network.EventManager
	callingAE string
	calledAE  string
	timeout   int
	// jpegQuality is the quality to send 8-bit images as JPEG Baseline at,
	// or 0 to send nothing that way.
	jpegQuality int
}

func (c *StoreSCUCommand) Name() string        { return "storescu" }
func (c *StoreSCUCommand) Description() string { return "DICOM Store SCU (send files)" }

func (c *StoreSCUCommand) AddFlags(fs *flag.FlagSet) {
	addClientTLSFlags(fs, &c.tls)
	c.events.add(fs)
	fs.StringVar(&c.callingAE, "aet", "STORESCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 60, "Connection timeout in seconds")
	fs.IntVar(&c.jpegQuality, "jpeg-quality", 0,
		"Send uncompressed 8-bit images as JPEG Baseline at this quality, 1 to 100, when the peer accepts it. Lossy; the SOP Instance UID is kept")
}

func (c *StoreSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("storescu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tlsCfg, err := c.tls.config()
	if err != nil {
		return err
	}
	events, closeEvents, err := c.events.open()
	if err != nil {
		return err
	}
	defer closeEvents()
	c.eventLog = events
	c.tlsConfig = tlsCfg

	if c.jpegQuality < 0 || c.jpegQuality > 100 {
		return fmt.Errorf("-jpeg-quality runs from 1 to 100, got %d", c.jpegQuality)
	}

	if fs.NArg() < 2 {
		return fmt.Errorf("usage: storescu [options] host:port file1.dcm [file2.dcm ...]")
	}

	address := fs.Arg(0)
	files := fs.Args()[1:]

	sent, failed := 0, 0

	// Each file's SOP class and syntax decide what is proposed, so read the
	// headers first. A file that cannot be read is reported and counted, and
	// the rest still go.
	var readable []storeFile
	for _, path := range files {
		sf, err := scanForStore(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			failed++
			continue
		}
		readable = append(readable, sf)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeout)*time.Second)
	defer cancel()

	for _, batch := range planStoreBatches(readable, c.jpegQuality > 0) {
		s, f := c.sendBatch(ctx, address, batch)
		sent += s
		failed += f
	}

	fmt.Printf("\nResults: %d sent, %d failed\n", sent, failed)
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to send", failed)
	}
	return nil
}

// sendBatch sends one batch of files over its own association.
func (c *StoreSCUCommand) sendBatch(ctx context.Context, address string, batch storeBatch) (sent, failed int) {
	scu := network.NewSCU(network.SCUConfig{
		TLS:       c.tlsConfig,
		Events:    c.eventLog,
		CallingAE: c.callingAE,
		CalledAE:  c.calledAE,
		Address:   address,
		Network: network.NetworkConfig{
			NetworkTimeout: time.Duration(c.timeout) * time.Second,
		},
	})

	fmt.Printf("Requesting Association with %s (AE: %s)\n", address, c.calledAE)
	if err := scu.Associate(ctx, batch.contexts); err != nil {
		fmt.Fprintf(os.Stderr, "Association failed: %v\n", err)
		return 0, len(batch.files)
	}
	defer func() { _ = scu.Release(ctx) }()
	fmt.Println("Association accepted")

	for _, sf := range batch.files {
		f, err := os.Open(sf.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", sf.path, err)
			failed++
			continue
		}
		dicomFile, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
		_ = f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", sf.path, err)
			failed++
			continue
		}

		// GetDataset materializes nested sequences as child datasets; building
		// one by hand from DataElements drops them, because a sequence element
		// carries its content in Items rather than Value.
		ds := dicomFile.GetDataset()
		fmt.Printf("Sending: %s\n", sf.path)
		if c.jpegQuality > 0 && isNativeSyntax(sf.syntax) {
			c.compressForPeer(scu, ds, sf)
		}
		if err := scu.Store(ctx, ds); err != nil {
			fmt.Fprintf(os.Stderr, "C-STORE failed for %s: %v\n", sf.path, err)
			failed++
			continue
		}
		sent++
		fmt.Printf("  Stored successfully\n")
	}
	return sent, failed
}

// compressForPeer makes ds a JPEG Baseline image when the peer accepted that
// syntax for its class, and says what it did. Store then sends it on that
// context, since a data set goes out on the context of its own syntax first.
//
// The choice is made here and not left to the association: offered beside
// the uncompressed syntaxes in one context, which of them is used would be
// the peer's to pick.
func (c *StoreSCUCommand) compressForPeer(scu *network.SCU, ds *dataset.Dataset, sf storeFile) {
	if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); !hasPixels {
		return
	}
	accepted := false
	if assoc := scu.Association(); assoc != nil {
		for _, pc := range assoc.AcceptedContexts() {
			if pc.AbstractSyntax == sf.sopClass && pc.TransferSyntax == network.JPEGBaselineUID {
				accepted = true
			}
		}
	}
	if !accepted {
		fmt.Println("  The peer does not accept JPEG Baseline for it; sending it as it is")
		return
	}
	// On an error the data set is unchanged.
	if err := ds.CompressJPEGBaselineInPlace(c.jpegQuality); err != nil {
		fmt.Printf("  Not sent as JPEG Baseline, as it is instead: %v\n", err)
		return
	}
	fmt.Printf("  As JPEG Baseline, quality %d\n", c.jpegQuality)
}

// isNativeSyntax reports whether a transfer syntax holds pixel data
// uncompressed.
func isNativeSyntax(uid string) bool {
	switch uid {
	case network.ImplicitVRLittleEndianUID, network.ExplicitVRLittleEndianUID,
		network.ExplicitVRBigEndianUID, network.DeflatedExplicitVRLittleEndianUID:
		return true
	}
	return false
}

// storeFile is what the proposal needs to know about a file before sending it.
type storeFile struct {
	path, sopClass, syntax string
}

// scanForStore reads a file's SOP class and transfer syntax. The meta header is
// enough, and reading only that keeps a directory of large images cheap to plan;
// a file without one is read whole, as the reader would to send it.
func scanForStore(path string) (storeFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return storeFile{}, err
	}
	r := filereader.NewDCMFileReader(filebase.NewFileReader(f))
	if r.ReadPreamble() == nil && r.ReadDICMPrefix() == nil {
		if meta, err := r.ReadFileMetaInfo(); err == nil &&
			meta.MediaStorageSOPClassUID != "" && meta.TransferSyntaxUID != "" {
			_ = f.Close()
			return storeFile{path, meta.MediaStorageSOPClassUID, meta.TransferSyntaxUID}, nil
		}
	}
	_ = f.Close()

	f, err = os.Open(path)
	if err != nil {
		return storeFile{}, err
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return storeFile{}, err
	}
	ds := df.GetDataset()
	class, _ := ds.GetTextValue(tag.New(0x0008, 0x0016))
	class = strings.TrimRight(class, " \x00")
	if class == "" {
		return storeFile{}, fmt.Errorf("no SOP Class UID (0008,0016), so there is nothing to propose it as")
	}
	return storeFile{path, class, ds.TransferSyntaxUID()}, nil
}

// maxContexts is how many presentation contexts one association can carry: IDs
// are odd numbers from 1 to 255 (PS3.8 9.3.2.2).
const maxContexts = 128

// storeBatch is the files sent over one association and what it proposes.
type storeBatch struct {
	contexts []network.PresentationContextItem
	files    []storeFile
}

// planStoreBatches proposes what the files hold, and nothing else.
//
// storescu used to propose the library's default set, whatever it was sending.
// That set is short, so an RT Dose, RT Plan, Structured Report or ECG failed
// with "not among the presentation contexts proposed" against an SCP that
// supports them. Widening the default is not the answer: an association carries
// at most 128 contexts, and there are more storage classes than that. dcmtk
// and pynetdicom read the files first and propose what they contain, and so
// does this.
//
// One context per SOP class and syntax, offering the file's own syntax and the
// two uncompressed Little Endian ones, so an SCP that will not take the
// compressed form can still take the instance decoded. A class held in two
// syntaxes gets two contexts, and SCU.Store sends each file on the one matching
// it. More than 128 contexts are split over several associations.
//
// With jpeg, each class held uncompressed also gets a context offering JPEG
// Baseline alone, so that whether the peer takes it is known before a file
// is compressed for it.
func planStoreBatches(files []storeFile, jpeg bool) []storeBatch {
	type key struct{ class, syntax string }
	var keys []key
	byKey := map[key][]storeFile{}
	for _, f := range files {
		k := key{f.sopClass, f.syntax}
		if _, seen := byKey[k]; !seen {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], f)
		if jk := (key{f.sopClass, network.JPEGBaselineUID}); jpeg && isNativeSyntax(f.syntax) {
			if _, seen := byKey[jk]; !seen {
				keys = append(keys, jk)
				byKey[jk] = nil
			}
		}
	}

	var batches []storeBatch
	for start := 0; start < len(keys); start += maxContexts {
		end := min(start+maxContexts, len(keys))
		var batch storeBatch
		for i, k := range keys[start:end] {
			syntaxes := []string{k.syntax}
			// A context with no file of its own is the JPEG Baseline offer.
			for _, s := range []string{network.ExplicitVRLittleEndianUID, network.ImplicitVRLittleEndianUID} {
				if s != k.syntax && (len(byKey[k]) > 0 || !jpeg) {
					syntaxes = append(syntaxes, s)
				}
			}
			batch.contexts = append(batch.contexts, network.PresentationContextItem{
				ID:               byte(2*i + 1),
				AbstractSyntax:   k.class,
				TransferSyntaxes: syntaxes,
			})
			batch.files = append(batch.files, byKey[k]...)
		}
		batches = append(batches, batch)
	}
	return batches
}
