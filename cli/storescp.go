package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/fileutil"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
)

// StoreSCPCommand implements the storescp CLI command.
type StoreSCPCommand struct {
	aeTitle   string
	port      int
	outputDir string
}

func (c *StoreSCPCommand) Name() string        { return "storescp" }
func (c *StoreSCPCommand) Description() string { return "DICOM Store SCP (receive files)" }

func (c *StoreSCPCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.aeTitle, "aet", "STORESCP", "AE title")
	fs.IntVar(&c.port, "port", 11112, "Listen port")
	fs.StringVar(&c.outputDir, "output", ".", "Output directory for received files")
}

func (c *StoreSCPCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("storescp", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ensure output directory exists
	if err := os.MkdirAll(c.outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	scp := network.NewSCP(network.SCPConfig{
		AETitle: c.aeTitle,
		Port:    c.port,
	})

	// Accept compressed pixel data as well as the four uncompressed syntaxes the
	// library proposes by default.
	//
	// A storage SCP's job is to keep what it is sent, and it does not have to
	// decode an instance to store it — CONFORMANCE.md §8.2 makes that point about
	// archives and routers. Refusing every compressed syntax meant a modality
	// storing JPEG-LS or JPEG 2000 natively, which is most modern equipment, could
	// not store here at all.
	//
	// The library default stays as it is, matching pynetdicom. This is a decision
	// about a server whose purpose is known, not about the default for every caller.
	scp.SetSupportedTransferSyntaxes(network.AllTransferSyntaxes())

	received := 0
	handler := &network.StorageHandler{
		OnStore: func(_ context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16 {
			received++
			// The UID comes from the peer, so it decides the filename. Validate it
			// before it becomes a path: joining it unchecked let a UID of
			// "../../etc/cron.d/pwn" write outside the output directory.
			filename, err := fileutil.InstanceFilePath(c.outputDir, sopInstanceUID)
			if err != nil {
				fmt.Printf("Refused an instance from the peer: %v\n", err)
				return network.StatusUnableToProcess
			}
			if err := writeDICOMFile(filename, sopClassUID, sopInstanceUID, ds); err != nil {
				fmt.Printf("Error writing %s: %v\n", filename, err)
				return network.StatusUnableToProcess
			}
			fmt.Printf("Received: %s (SOP Class: %s) -> %s\n", sopInstanceUID, sopClassUID, filename)
			return network.StatusSuccess
		},
	}
	scp.SetHandler(handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
		scp.Close()
	}()

	fmt.Printf("Starting DICOM Store SCP\n")
	fmt.Printf("  AE Title: %s\n", c.aeTitle)
	fmt.Printf("  Port:     %d\n", c.port)
	fmt.Printf("  Output:   %s\n", c.outputDir)
	fmt.Println("Listening for associations... (Ctrl+C to stop)")

	if err := scp.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	fmt.Printf("Received %d file(s)\n", received)
	return nil
}

// writeDICOMFile writes a dataset to a DICOM Part 10 file.
func writeDICOMFile(filename, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) error {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(f))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    sopClassUID,
		MediaStorageSOPInstanceUID: sopInstanceUID,
		TransferSyntaxUID:          network.ExplicitVRLittleEndianUID,
		ImplementationClassUID:     network.DefaultImplementationClassUID,
		ImplementationVersionName:  network.DefaultImplementationVersionName,
	})

	for _, elem := range filewriter.ElementsFromDataset(ds) {
		if err := w.AddDataElement(elem); err != nil {
			return fmt.Errorf("add element %s: %w", elem.Tag, err)
		}
	}

	if err := w.Write(); err != nil {
		return fmt.Errorf("write DICOM: %w", err)
	}
	return w.Close()
}

// toWriterElements converts a dataset into writer elements, descending into
// sequences so nested items are preserved. Reading elem.Value alone would drop
