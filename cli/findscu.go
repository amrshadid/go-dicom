package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
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
	sopClass    string
	keys        queryKeys
}

// queryKeys collects repeated -key flags.
//
// A model outside the patient hierarchy has its own matching keys — Hanging
// Protocol Name, Content Label — and no fixed set this command could hardcode, so
// the caller names them.
type queryKeys []string

func (k *queryKeys) String() string { return strings.Join(*k, ",") }

func (k *queryKeys) Set(value string) error {
	*k = append(*k, value)
	return nil
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
	fs.StringVar(&c.sopClass, "sop-class", "",
		"Information model UID to query (default: whichever of Patient Root, Study Root, "+
			"Patient/Study Only or Modality Worklist the peer accepted). Naming one is the "+
			"only way to reach a model outside that set, such as Hanging Protocol or "+
			"Relevant Patient Information")
	fs.Var(&c.keys, "key",
		"Additional query key as GGGG,EEEE[=value], repeatable. An empty value requests the "+
			"attribute without matching on it")
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

	// A model outside the default set has no context unless one is proposed for
	// it, so naming a SOP class has to change what is negotiated as well as what
	// is sent.
	var contexts []network.PresentationContextItem
	if c.sopClass != "" {
		contexts = []network.PresentationContextItem{{
			ID:               1,
			AbstractSyntax:   c.sopClass,
			TransferSyntaxes: network.DefaultTransferSyntaxes(),
		}}
	}

	if err := scu.Associate(ctx, contexts); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	// Build query dataset
	queryDS := dataset.NewDataset()

	// The patient hierarchy keys are only sent for a model that has one. A
	// non-patient model — Hanging Protocol, Color Palette — has a single level and
	// no QueryRetrieveLevel at all (PS3.4 GG.2), and sending patient keys to it
	// would be asking about attributes its objects do not carry.
	if c.sopClass == "" || isPatientHierarchyModel(c.sopClass) {
		_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0052), dataelem.CS, []byte(c.level)))

		if c.patientName != "" {
			_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte(c.patientName)))
		} else {
			_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte{}))
		}

		if c.patientID != "" {
			_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte(c.patientID)))
		} else {
			_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte{}))
		}

		_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D), dataelem.UI, []byte{}))
		_ = queryDS.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0020), dataelem.DA, []byte{}))
	}

	// Then whatever the caller named.
	for _, spec := range c.keys {
		t, value, err := parseQueryKey(spec)
		if err != nil {
			return err
		}
		_ = queryDS.Add(dataelem.NewDataElement(t, dataelem.VR(t.GetVR()), []byte(value)))
	}

	if queryDS.Length() == 0 {
		return fmt.Errorf("the query is empty; pass -key GGGG,EEEE to name an attribute to " +
			"return, or drop -sop-class to query a patient hierarchy model")
	}

	if c.sopClass != "" {
		fmt.Printf("Querying %s (SOP class: %s)\n", address, c.sopClass)
	} else {
		fmt.Printf("Querying %s (level: %s)\n", address, c.level)
	}

	var (
		results <-chan *network.CFindResult
		err     error
	)
	if c.sopClass != "" {
		results, err = scu.FindWithSOPClass(ctx, c.sopClass, queryDS)
	} else {
		results, err = scu.Find(ctx, queryDS)
	}
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

// isPatientHierarchyModel reports whether a query model organizes its objects
// under a patient, and so uses QueryRetrieveLevel and the patient keys.
//
// The three query/retrieve roots and Modality Worklist do. The non-patient object
// models and Relevant Patient Information Query do not: they have a single level,
// so a QueryRetrieveLevel in the identifier is meaningless and the patient keys
// name attributes their objects do not carry.
func isPatientHierarchyModel(sopClassUID string) bool {
	switch sopClassUID {
	case network.PatientRootQueryRetrieveFind,
		network.StudyRootQueryRetrieveFind,
		network.PatientStudyOnlyQueryRetrieveFind,
		network.ModalityWorklistInformationModelFindUID:
		return true
	default:
		return false
	}
}

// parseQueryKey parses a -key value of the form GGGG,EEEE[=value].
func parseQueryKey(spec string) (tag.Tag, string, error) {
	tagPart, value, _ := strings.Cut(spec, "=")

	group, element, found := strings.Cut(strings.TrimSpace(tagPart), ",")
	if !found {
		return 0, "", fmt.Errorf("invalid -key %q, want GGGG,EEEE[=value]", spec)
	}

	g, err := strconv.ParseUint(strings.TrimSpace(group), 16, 16)
	if err != nil {
		return 0, "", fmt.Errorf("invalid group in -key %q: %w", spec, err)
	}
	e, err := strconv.ParseUint(strings.TrimSpace(element), 16, 16)
	if err != nil {
		return 0, "", fmt.Errorf("invalid element in -key %q: %w", spec, err)
	}

	return tag.New(uint16(g), uint16(e)), value, nil
}
