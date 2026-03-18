package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// MoveSCUCommand implements the movescu CLI command.
type MoveSCUCommand struct {
	callingAE string
	calledAE  string
	moveDest  string
	timeout   int
	studyUID  string
	seriesUID string
	level     string
}

func (c *MoveSCUCommand) Name() string        { return "movescu" }
func (c *MoveSCUCommand) Description() string { return "DICOM Move SCU (retrieve)" }

func (c *MoveSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "MOVESCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.StringVar(&c.moveDest, "dest", "", "Move destination AE title (required)")
	fs.IntVar(&c.timeout, "timeout", 120, "Connection timeout in seconds")
	fs.StringVar(&c.studyUID, "study", "", "Study Instance UID")
	fs.StringVar(&c.seriesUID, "series", "", "Series Instance UID")
	fs.StringVar(&c.level, "level", "STUDY", "Query retrieve level")
}

func (c *MoveSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("movescu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: movescu [options] host:port")
	}

	if c.moveDest == "" {
		return fmt.Errorf("--dest (move destination AE title) is required")
	}

	address := fs.Arg(0)

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

	if err := scu.Associate(ctx, nil); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer scu.Release(ctx)

	// Build query dataset
	queryDS := dataset.NewDataset()
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte(c.level)))

	if c.studyUID != "" {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte(c.studyUID)))
	}
	if c.seriesUID != "" {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000E), dataelem.UI, []byte(c.seriesUID)))
	}

	fmt.Printf("Moving from %s to %s (level: %s)\n", address, c.moveDest, c.level)

	if err := scu.Move(ctx, queryDS, c.moveDest); err != nil {
		return fmt.Errorf("C-MOVE failed: %w", err)
	}

	fmt.Println("C-MOVE completed successfully")
	return nil
}
