package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// StoreSCUCommand implements the storescu CLI command.
type StoreSCUCommand struct {
	callingAE string
	calledAE  string
	timeout   int
}

func (c *StoreSCUCommand) Name() string        { return "storescu" }
func (c *StoreSCUCommand) Description() string { return "DICOM Store SCU (send files)" }

func (c *StoreSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "STORESCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 60, "Connection timeout in seconds")
}

func (c *StoreSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("storescu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 2 {
		return fmt.Errorf("usage: storescu [options] host:port file1.dcm [file2.dcm ...]")
	}

	address := fs.Arg(0)
	files := fs.Args()[1:]

	scu := network.NewSCU(network.SCUConfig{
		CallingAE: c.callingAE,
		CalledAE:  c.calledAE,
		Address:   address,
		Network: network.NetworkConfig{
			NetworkTimeout: time.Duration(c.timeout) * time.Second,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeout)*time.Second)
	defer cancel()

	fmt.Printf("Requesting Association with %s (AE: %s)\n", address, c.calledAE)

	if err := scu.Associate(ctx, nil); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer scu.Release(ctx)

	fmt.Println("Association accepted")

	sent, failed := 0, 0
	for _, filePath := range files {
		// Support .dcm, .ima, .dicom, and extensionless DICOM files
		ext := strings.ToLower(filepath.Ext(filePath))
		_ = ext // All files treated as DICOM regardless of extension

		f, err := os.Open(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening %s: %v\n", filePath, err)
			failed++
			continue
		}

		fbReader := filebase.NewFileReader(f)
		dicomFile, err := filereader.ReadDICOMFile(fbReader)
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", filePath, err)
			failed++
			continue
		}

		// Convert to dataset for network transfer
		ds := dicomFileToDataset(dicomFile)
		fmt.Printf("Sending: %s\n", filePath)

		if err := scu.Store(ctx, ds); err != nil {
			fmt.Fprintf(os.Stderr, "C-STORE failed for %s: %v\n", filePath, err)
			failed++
			continue
		}

		sent++
		fmt.Printf("  Stored successfully\n")
	}

	fmt.Printf("\nResults: %d sent, %d failed\n", sent, failed)
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to send", failed)
	}
	return nil
}

// dicomFileToDataset converts a filereader.DICOMFile to a dataset.Dataset.
func dicomFileToDataset(df *filereader.DICOMFile) *dataset.Dataset {
	ds := dataset.NewDataset()
	for _, elem := range df.DataElements {
		t := tag.New(elem.Tag.Group(), elem.Tag.Element())
		de := dataelem.NewDataElement(t, dataelem.VR(elem.VR), elem.Value)
		ds.Add(de)
	}
	return ds
}
