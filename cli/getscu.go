package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/fileutil"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// GetSCUCommand implements the getscu CLI command.
type GetSCUCommand struct {
	callingAE string
	calledAE  string
	timeout   int
	studyUID  string
	seriesUID string
	level     string
	outputDir string
}

func (c *GetSCUCommand) Name() string        { return "getscu" }
func (c *GetSCUCommand) Description() string { return "DICOM Get SCU (retrieve on same association)" }

func (c *GetSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "GETSCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 120, "Connection timeout in seconds")
	fs.StringVar(&c.studyUID, "study", "", "Study Instance UID")
	fs.StringVar(&c.seriesUID, "series", "", "Series Instance UID")
	fs.StringVar(&c.level, "level", "STUDY", "Query retrieve level")
	fs.StringVar(&c.outputDir, "output", ".", "Directory to write retrieved instances to")
}

func (c *GetSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("getscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: getscu [options] host:port")
	}

	address := fs.Arg(0)

	if err := os.MkdirAll(c.outputDir, 0o755); err != nil {
		return fmt.Errorf("cannot create the output directory: %w", err)
	}

	received := 0
	scu := network.NewSCU(network.SCUConfig{
		CallingAE: c.callingAE,
		CalledAE:  c.calledAE,
		Address:   address,
		Network: network.NetworkConfig{
			NetworkTimeout: time.Duration(c.timeout) * time.Second,
		},
		// C-GET sends the instances back on this same association as C-STORE
		// sub-operations. Without a handler for them the retrieval succeeds and
		// every instance is discarded — the command would report success having
		// saved nothing.
		OnCStore: func(_ context.Context, sopClassUID, sopInstanceUID string, ds *dataset.Dataset) uint16 {
			received++
			// The UID comes from the archive we queried, so it decides the
			// filename. Validate it before it becomes a path.
			filename, err := fileutil.InstanceFilePath(c.outputDir, sopInstanceUID)
			if err != nil {
				fmt.Printf("Refused an instance from the peer: %v\n", err)
				return network.StatusUnableToProcess
			}
			if err := writeDICOMFile(filename, sopClassUID, sopInstanceUID, ds); err != nil {
				fmt.Printf("Error writing %s: %v\n", filename, err)
				return network.StatusUnableToProcess
			}
			fmt.Printf("Received: %s -> %s\n", sopInstanceUID, filename)
			return network.StatusSuccess
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeout)*time.Second)
	defer cancel()

	if err := scu.Associate(ctx, nil); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	queryDS := dataset.NewDataset()
	_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte(c.level)))

	if c.studyUID != "" {
		_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte(c.studyUID)))
	}
	if c.seriesUID != "" {
		_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000E), dataelem.UI, []byte(c.seriesUID)))
	}

	fmt.Printf("C-GET from %s (level: %s)\n", address, c.level)

	// C-GET, not C-MOVE. This called Move with an empty destination, which is a
	// C-MOVE naming nowhere to send to — the SCP answered 0xA801, Move
	// Destination Unknown, and the command had never performed a C-GET at all.
	// The comment that used to sit here said C-GET uses the same association,
	// which is exactly right and exactly what the call did not do.
	if err := scu.Get(ctx, queryDS); err != nil {
		return fmt.Errorf("C-GET failed: %w", err)
	}

	fmt.Printf("C-GET completed: %d instance(s) retrieved to %s\n", received, c.outputDir)
	return nil
}
