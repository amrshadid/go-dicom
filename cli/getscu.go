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

// GetSCUCommand implements the getscu CLI command.
type GetSCUCommand struct {
	callingAE string
	calledAE  string
	timeout   int
	studyUID  string
	seriesUID string
	level     string
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

	// C-GET uses same association for data transfer (unlike C-MOVE)
	if err := scu.Move(ctx, queryDS, ""); err != nil {
		return fmt.Errorf("C-GET failed: %w", err)
	}

	fmt.Println("C-GET completed successfully")
	return nil
}
