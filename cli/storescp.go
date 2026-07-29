package cli

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
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

	received := 0
	handler := &network.StorageHandler{
		OnStore: func(_ context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16 {
			received++
			filename := filepath.Join(c.outputDir, sopInstanceUID+".dcm")
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

	for _, elem := range toWriterElements(ds) {
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
// them: a sequence holds child datasets, not a byte value.
func toWriterElements(ds *dataset.Dataset) []*filewriter.DataElement {
	var out []*filewriter.DataElement

	for _, elem := range ds.GetAll() {
		// Skipping here writes a file that is missing an attribute the sender
		// transmitted, with nothing recording that it happened. Reporting is
		// the least a receiver can do; the element is still dropped, because
		// there is no tag to write it under.
		t, ok := elem.Tag()
		if !ok {
			log.Printf("storescp: dropping an element with an unreadable tag (%T) from the stored file",
				elem.GetTag())
			continue
		}

		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			items := make([]*filewriter.SequenceItem, 0, seq.Length())
			for _, item := range seq.Items() {
				child, ok := item.(*dataset.Dataset)
				if !ok {
					continue
				}
				items = append(items, &filewriter.SequenceItem{
					Elements: toWriterElements(child),
				})
			}
			out = append(out, &filewriter.DataElement{
				Tag: t, VR: "SQ", Items: items,
			})
			continue
		}

		data, ok := elem.GetValue().([]byte)
		if !ok {
			continue
		}
		out = append(out, &filewriter.DataElement{
			Tag:    t,
			VR:     string(elem.GetVR()),
			Value:  data,
			Length: uint32(len(data)),
		})
	}

	return out
}
