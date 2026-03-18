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

// FindSCUCommand implements the findscu CLI command.
type FindSCUCommand struct {
	callingAE   string
	calledAE    string
	timeout     int
	level       string
	patientID   string
	patientName string
}

func (c *FindSCUCommand) Name() string        { return "findscu" }
func (c *FindSCUCommand) Description() string { return "DICOM Find SCU (query)" }

func (c *FindSCUCommand) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.callingAE, "aet", "FINDSCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 30, "Connection timeout in seconds")
	fs.StringVar(&c.level, "level", "STUDY", "Query retrieve level (PATIENT, STUDY, SERIES, IMAGE)")
	fs.StringVar(&c.patientID, "patient-id", "", "Patient ID to search")
	fs.StringVar(&c.patientName, "patient-name", "", "Patient name to search (wildcards: * ?)")
}

func (c *FindSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("findscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: findscu [options] host:port")
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

	// Query Retrieve Level
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte(c.level)))

	// Patient Name
	if c.patientName != "" {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(c.patientName)))
	} else {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte{}))
	}

	// Patient ID
	if c.patientID != "" {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(c.patientID)))
	} else {
		queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte{}))
	}

	// Also request Study Instance UID and Study Date
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte{}))
	queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0020), dataelem.DA, []byte{}))

	fmt.Printf("Querying %s (level: %s)\n", address, c.level)

	results, err := scu.Find(ctx, queryDS)
	if err != nil {
		return fmt.Errorf("C-FIND failed: %w", err)
	}

	count := 0
	for result := range results {
		if result.Err != nil {
			return fmt.Errorf("query error: %w", result.Err)
		}
		count++
		fmt.Printf("\n--- Result %d ---\n", count)
		if result.DataSet != nil {
			elements := result.DataSet.GetAll()
			for _, elem := range elements {
				t := elem.GetTag()
				name := ""
				if elemTag, ok := t.(tag.Tag); ok {
					name = elemTag.GetName()
				}
				val := elem.GetValue()
				if data, ok := val.([]byte); ok {
					fmt.Printf("  %-40s %s\n", name, string(data))
				}
			}
		}
	}

	fmt.Printf("\nFound %d result(s)\n", count)
	return nil
}
