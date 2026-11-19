package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// UPSSCUCommand implements the upsscu CLI command: it works Unified
// Procedure Steps on an SCP (PS3.4 Annex CC) with the operations UPS Push,
// Pull and Query define. A step is created SCHEDULED, claimed by the
// performer, which is given a Transaction UID, updated while it is IN
// PROGRESS, and made COMPLETED or CANCELED by that performer alone, the
// Transaction UID proving it is the one (CC.1.1).
//
// The library's SCU has no UPS call of its own; the command is built on its
// N-CREATE, N-SET, N-GET, N-ACTION and C-FIND. Watch is not here: a
// subscription is only of use to a system that receives the event reports,
// and no command receives them.
type UPSSCUCommand struct {
	tls       tlsFlags
	events    eventsFlag
	callingAE string
	calledAE  string
	timeout   int
	verbose   bool

	create                          bool
	query                           bool
	get, claim, set                 string
	complete, cancel, requestCancel string
	transaction                     string
	uid, jsonFile                   string

	patientName, patientID string
	label, priority, start string
	state, reason          string
}

func (c *UPSSCUCommand) Name() string { return "upsscu" }

func (c *UPSSCUCommand) Description() string {
	return "DICOM Unified Procedure Step SCU (N-CREATE, N-SET, N-GET, N-ACTION, C-FIND)"
}

func (c *UPSSCUCommand) AddFlags(fs *flag.FlagSet) {
	addClientTLSFlags(fs, &c.tls)
	c.events.add(fs)
	fs.StringVar(&c.callingAE, "aet", "UPSSCU", "Calling AE title")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 30, "Connection timeout in seconds")
	fs.BoolVar(&c.verbose, "v", false, "Print the data set sent")

	fs.BoolVar(&c.create, "create", false, "N-CREATE a step, SCHEDULED (UPS Push)")
	fs.BoolVar(&c.query, "query", false, "C-FIND steps (UPS Query); -patient-id, -state and -json are the keys")
	fs.StringVar(&c.get, "get", "", "N-GET every attribute of the step with this UID (UPS Pull)")
	fs.StringVar(&c.claim, "claim", "", "Set the step with this UID IN PROGRESS; prints the Transaction UID (generated unless -transaction)")
	fs.StringVar(&c.set, "set", "", "N-SET the step with this UID to the -json attributes; needs -transaction")
	fs.StringVar(&c.complete, "complete", "", "Set the step with this UID COMPLETED; needs -transaction")
	fs.StringVar(&c.cancel, "cancel", "", "Set the step with this UID CANCELED; needs -transaction")
	fs.StringVar(&c.requestCancel, "request-cancel", "", "Ask the performer of the step with this UID to cancel it")
	fs.StringVar(&c.transaction, "transaction", "", "The Transaction UID the step was claimed with")
	fs.StringVar(&c.uid, "uid", "", "With -create: the new step's SOP Instance UID (generated if empty)")
	fs.StringVar(&c.jsonFile, "json", "",
		"A DICOM JSON object: with -create, -set and -query, its attributes are added to what the other flags give, replacing any with the same tag")

	fs.StringVar(&c.patientName, "patient-name", "", "With -create or -query: Patient's Name")
	fs.StringVar(&c.patientID, "patient-id", "", "With -create or -query: Patient ID")
	fs.StringVar(&c.label, "label", "", "With -create: Procedure Step Label")
	fs.StringVar(&c.priority, "priority", "MEDIUM", "With -create: Scheduled Procedure Step Priority, HIGH, MEDIUM or LOW")
	fs.StringVar(&c.start, "start", "", "With -create: Scheduled Procedure Step Start DateTime, YYYYMMDDHHMMSS (default now)")
	fs.StringVar(&c.state, "state", "", "With -query: Procedure Step State to match (SCHEDULED, IN PROGRESS, ...)")
	fs.StringVar(&c.reason, "reason", "", "With -request-cancel: Reason For Cancellation")
}

// upsAction is what one run of upsscu does.
type upsAction struct {
	name string // the flag
	uid  string // the step, for the actions on one
}

func (c *UPSSCUCommand) action() (upsAction, error) {
	var chosen []upsAction
	if c.create {
		chosen = append(chosen, upsAction{name: "create"})
	}
	if c.query {
		chosen = append(chosen, upsAction{name: "query"})
	}
	for _, a := range []upsAction{
		{"get", c.get}, {"claim", c.claim}, {"set", c.set},
		{"complete", c.complete}, {"cancel", c.cancel}, {"request-cancel", c.requestCancel},
	} {
		if a.uid != "" {
			chosen = append(chosen, a)
		}
	}
	if len(chosen) != 1 {
		return upsAction{}, errors.New("give one of -create, -query, -get, -claim, -set, -complete, -cancel and -request-cancel")
	}
	a := chosen[0]
	switch a.name {
	case "set", "complete", "cancel":
		if c.transaction == "" {
			return a, fmt.Errorf("-%s needs -transaction: a step IN PROGRESS is changed only with the Transaction UID it was claimed with (PS3.4 CC.1.1)", a.name)
		}
	case "claim":
	default:
		if c.transaction != "" {
			return a, fmt.Errorf("-transaction is for -claim, -set, -complete and -cancel, not -%s", a.name)
		}
	}
	if a.name == "set" && c.jsonFile == "" {
		return a, errors.New("-set needs -json: the attributes to set")
	}
	if c.uid != "" && a.name != "create" {
		return a, errors.New("-uid names the step -create makes")
	}
	return a, nil
}

func (c *UPSSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("upsscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tlsCfg, err := c.tls.config()
	if err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: upsscu [options] -create|-query|-get UID|-claim UID|-set UID|-complete UID|-cancel UID|-request-cancel UID host:port")
	}
	act, err := c.action()
	if err != nil {
		return err
	}
	ds, err := c.dataSet(act, time.Now())
	if err != nil {
		return err
	}

	events, closeEvents, err := c.events.open()
	if err != nil {
		return err
	}
	defer closeEvents()
	address := fs.Arg(0)
	scu := network.NewSCU(network.SCUConfig{
		TLS: tlsCfg, Events: events, CallingAE: c.callingAE, CalledAE: c.calledAE, Address: address,
		Network: network.NetworkConfig{NetworkTimeout: time.Duration(c.timeout) * time.Second},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.timeout)*time.Second)
	defer cancel()

	// Each operation goes on the SOP class that defines it (PS3.4 CC.2):
	// N-CREATE and the request to cancel on Push, which is what a system
	// that schedules work uses; N-GET, N-SET and the change of state on
	// Pull, which is what the performer uses; C-FIND on Query.
	sopClass := network.UnifiedProcedureStepPullUID
	switch act.name {
	case "create", "request-cancel":
		sopClass = network.UnifiedProcedureStepPushUID
	case "query":
		sopClass = network.UnifiedProcedureStepQueryUID
	}
	fmt.Printf("Requesting Association with %s (AE: %s)\n", address, c.calledAE)
	if err := scu.Associate(ctx, []network.PresentationContextItem{{
		ID: 1, AbstractSyntax: sopClass, TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}}); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer func() { _ = scu.Release(ctx) }()
	if c.verbose && ds != nil {
		fmt.Println("Sending:")
		printDataSet(os.Stdout, ds, "  ")
	}

	var operation string
	var code uint16
	var returned *dataset.Dataset
	switch act.name {
	case "query":
		return c.runQuery(ctx, scu, ds)
	case "create":
		operation = "N-CREATE"
		uid := c.uid
		if uid == "" {
			uid = network.GenerateUID()
		}
		act.uid = uid
		resp, err := scu.NCreate(ctx, sopClass, uid, ds)
		if err != nil {
			return fmt.Errorf("N-CREATE failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
	case "get":
		operation = "N-GET"
		resp, err := scu.NGet(ctx, sopClass, act.uid)
		if err != nil {
			return fmt.Errorf("N-GET failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
	case "set":
		operation = "N-SET"
		resp, err := scu.NSet(ctx, sopClass, act.uid, ds)
		if err != nil {
			return fmt.Errorf("N-SET failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
	default:
		operation = "N-ACTION"
		actionType := network.UPSActionChangeState
		if act.name == "request-cancel" {
			actionType = network.UPSActionRequestCancel
		}
		resp, err := scu.NAction(ctx, sopClass, act.uid, actionType, ds)
		if err != nil {
			return fmt.Errorf("N-ACTION failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
	}

	fmt.Printf("%s %s (status 0x%04X)\n", operation, network.CategorizeStatus(code), code)
	if network.IsFailure(code) {
		return fmt.Errorf("the SCP refused the %s: status 0x%04X", operation, code)
	}
	switch act.name {
	case "create":
		fmt.Printf("Procedure step %s is SCHEDULED\n", act.uid)
	case "get":
		if returned != nil {
			printDataSet(os.Stdout, returned, "  ")
		}
	case "claim":
		fmt.Printf("Procedure step %s is IN PROGRESS\n", act.uid)
		fmt.Printf("Transaction UID %s: give it to -set, -complete or -cancel\n", upsText(ds, tagTransactionUID))
	case "set":
		fmt.Printf("Procedure step %s is updated\n", act.uid)
	case "complete":
		fmt.Printf("Procedure step %s is COMPLETED\n", act.uid)
	case "cancel":
		fmt.Printf("Procedure step %s is CANCELED\n", act.uid)
	case "request-cancel":
		fmt.Printf("Cancellation of procedure step %s is requested; its performer decides\n", act.uid)
	}
	return nil
}

// runQuery sends the C-FIND and prints each answer with its sequences.
func (c *UPSSCUCommand) runQuery(ctx context.Context, scu *network.SCU, identifier *dataset.Dataset) error {
	results, err := scu.FindWithSOPClass(ctx, network.UnifiedProcedureStepQueryUID, identifier)
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
			printDataSet(os.Stdout, result.DataSet, "  ")
		}
	}
	fmt.Printf("\nFound %d procedure step(s)\n", count)
	return nil
}

// dataSet is what an action sends, before any -json attributes: none for
// N-GET.
func (c *UPSSCUCommand) dataSet(act upsAction, now time.Time) (*dataset.Dataset, error) {
	var keys []worklistKey
	var sequences []tag.Tag
	switch act.name {
	case "get":
		return nil, nil
	case "create":
		start := c.start
		if start == "" {
			start = now.Format("20060102150405")
		}
		switch strings.ToUpper(c.priority) {
		case "HIGH", "MEDIUM", "LOW":
		default:
			return nil, fmt.Errorf("-priority %q: it is HIGH, MEDIUM or LOW", c.priority)
		}
		// The attributes PS3.4 Table CC.2.5-3 requires of an N-CREATE that
		// the flags can give; Type 2 ones are present and empty when not.
		keys = []worklistKey{
			{tag.New(0x0010, 0x0010), dataelem.PN, c.patientName},                // Patient's Name
			{tag.New(0x0010, 0x0020), dataelem.LO, c.patientID},                  // Patient ID
			{tag.New(0x0010, 0x0030), dataelem.DA, ""},                           // Patient's Birth Date
			{tag.New(0x0010, 0x0040), dataelem.CS, ""},                           // Patient's Sex
			{tag.New(0x0038, 0x0010), dataelem.LO, ""},                           // Admission ID
			{tag.New(0x0040, 0x4005), dataelem.DT, start},                        // Scheduled Procedure Step Start DateTime
			{tag.New(0x0040, 0x4041), dataelem.CS, "READY"},                      // Input Readiness State
			{tag.New(0x0074, 0x1000), dataelem.CS, string(network.UPSScheduled)}, // Procedure Step State
			{tag.New(0x0074, 0x1200), dataelem.CS, strings.ToUpper(c.priority)},  // Scheduled Procedure Step Priority
			{tag.New(0x0074, 0x1204), dataelem.LO, c.label},                      // Procedure Step Label
		}
		sequences = []tag.Tag{
			tag.New(0x0040, 0x4018), // Scheduled Workitem Code Sequence
			tag.New(0x0040, 0x4021), // Input Information Sequence
			tag.New(0x0040, 0x4025), // Scheduled Station Name Code Sequence
			tag.New(0x0040, 0x4026), // Scheduled Station Class Code Sequence
			tag.New(0x0040, 0x4027), // Scheduled Station Geographic Location Code Sequence
			tag.New(0x0040, 0x4034), // Scheduled Human Performers Sequence
			tag.New(0x0040, 0xA370), // Referenced Request Sequence
		}
	case "query":
		// The keys a worklist client asks for (PS3.4 CC.2.6, Table
		// CC.2.5-3's Return Key column): matched where a flag gives a
		// value, returned where it does not.
		keys = []worklistKey{
			{tag.New(0x0008, 0x0018), dataelem.UI, ""},            // SOP Instance UID
			{tag.New(0x0010, 0x0010), dataelem.PN, c.patientName}, // Patient's Name
			{tag.New(0x0010, 0x0020), dataelem.LO, c.patientID},   // Patient ID
			{tag.New(0x0040, 0x4005), dataelem.DT, ""},            // Scheduled Procedure Step Start DateTime
			{tag.New(0x0074, 0x1000), dataelem.CS, c.state},       // Procedure Step State
			{tag.New(0x0074, 0x1200), dataelem.CS, ""},            // Scheduled Procedure Step Priority
			{tag.New(0x0074, 0x1204), dataelem.LO, ""},            // Procedure Step Label
		}
	case "set":
		keys = []worklistKey{{tagTransactionUID, dataelem.UI, c.transaction}}
	case "claim", "complete", "cancel":
		transaction := c.transaction
		if transaction == "" {
			transaction = network.GenerateUID()
		}
		state := map[string]network.UPSState{"claim": network.UPSInProgress,
			"complete": network.UPSCompleted, "cancel": network.UPSCanceled}[act.name]
		keys = []worklistKey{
			{tag.New(0x0074, 0x1000), dataelem.CS, string(state)}, // Procedure Step State
			{tagTransactionUID, dataelem.UI, transaction},
		}
	case "request-cancel":
		keys = []worklistKey{{tag.New(0x0074, 0x1238), dataelem.LT, c.reason}} // Reason For Cancellation
	}
	ds := dataset.NewDataset()
	for _, k := range keys {
		if err := ds.Add(dataelem.NewDataElement(k.tag, k.vr, []byte(k.value))); err != nil {
			return nil, err
		}
	}
	for _, t := range sequences {
		if err := ds.AddSequence(t, sequence.New()); err != nil {
			return nil, err
		}
	}
	if c.jsonFile != "" {
		switch act.name {
		case "create", "set", "query":
		default:
			return nil, fmt.Errorf("-json is for -create, -set and -query, not -%s", act.name)
		}
		made, err := createFromJSON(c.jsonFile, "", "")
		if err != nil {
			return nil, fmt.Errorf("-json %s: %w", c.jsonFile, err)
		}
		if len(made) != 1 {
			return nil, fmt.Errorf("-json %s holds %d objects; one data set is sent", c.jsonFile, len(made))
		}
		for _, element := range made[0].ds.GetAll() {
			t, ok := element.GetTag().(tag.Tag)
			if !ok {
				continue
			}
			ds.Remove(t)
			if err := ds.Add(element); err != nil {
				return nil, err
			}
		}
	}
	return ds, nil
}

// tagTransactionUID is Transaction UID (0008,1195), the lock a performer
// holds on a step IN PROGRESS.
var tagTransactionUID = tag.New(0x0008, 0x1195)

// upsText is the text of a top-level attribute, without its padding.
func upsText(ds *dataset.Dataset, t tag.Tag) string {
	element, ok := ds.Get(t)
	if !ok {
		return ""
	}
	value, _ := element.GetValue().([]byte)
	return strings.TrimRight(string(value), " \x00")
}
