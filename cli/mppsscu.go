package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// MPPSSCUCommand implements the mppsscu CLI command: it reports a Modality
// Performed Procedure Step to an SCP (PS3.4 Annex F.7), as a modality does.
// An N-CREATE starts the step IN PROGRESS; an N-SET then makes it COMPLETED
// or DISCONTINUED, which are final (F.7.2.1.2).
type MPPSSCUCommand struct {
	tls       tlsFlags
	events    eventsFlag
	callingAE string
	calledAE  string
	timeout   int
	verbose   bool

	complete, discontinue string
	uid                   string
	jsonFile              string

	patientName, patientID, birthDate, sex    string
	studyUID, accession, requestedProcedureID string
	scheduledStepID, stepID, modality         string
	description, stationAET, stationName      string
	protocol                                  string
	series                                    stringList
	instances                                 instanceList
}

func (c *MPPSSCUCommand) Name() string { return "mppsscu" }

func (c *MPPSSCUCommand) Description() string {
	return "DICOM Modality Performed Procedure Step SCU (N-CREATE, N-SET)"
}

func (c *MPPSSCUCommand) AddFlags(fs *flag.FlagSet) {
	addClientTLSFlags(fs, &c.tls)
	c.events.add(fs)
	fs.StringVar(&c.callingAE, "aet", "MPPSSCU", "Calling AE title, and the Performed Station AE Title unless -station-aet")
	fs.StringVar(&c.calledAE, "aec", "ANY-SCP", "Called AE title")
	fs.IntVar(&c.timeout, "timeout", 30, "Connection timeout in seconds")
	fs.BoolVar(&c.verbose, "v", false, "Print the data set sent and any the SCP returns")

	fs.StringVar(&c.complete, "complete", "", "N-SET the step with this SOP Instance UID to COMPLETED")
	fs.StringVar(&c.discontinue, "discontinue", "", "N-SET the step with this SOP Instance UID to DISCONTINUED")
	fs.StringVar(&c.uid, "uid", "", "Creating: the new step's SOP Instance UID (generated if empty)")
	fs.StringVar(&c.jsonFile, "json", "",
		"A DICOM JSON object whose attributes are added to what the other flags give, replacing any with the same tag")

	fs.StringVar(&c.patientName, "patient-name", "", "Creating: Patient's Name")
	fs.StringVar(&c.patientID, "patient-id", "", "Creating: Patient ID")
	fs.StringVar(&c.birthDate, "birth-date", "", "Creating: Patient's Birth Date, YYYYMMDD")
	fs.StringVar(&c.sex, "sex", "", "Creating: Patient's Sex, M, F or O")
	fs.StringVar(&c.studyUID, "study-uid", "", "Creating: the scheduled Study Instance UID (generated if empty)")
	fs.StringVar(&c.accession, "accession", "", "Creating: Accession Number")
	fs.StringVar(&c.requestedProcedureID, "requested-procedure-id", "", "Creating: Requested Procedure ID")
	fs.StringVar(&c.scheduledStepID, "scheduled-step-id", "", "Creating: Scheduled Procedure Step ID")
	fs.StringVar(&c.stepID, "step-id", "", "Creating: Performed Procedure Step ID (made from the time if empty)")
	fs.StringVar(&c.modality, "modality", "OT", "Creating: Modality")
	fs.StringVar(&c.description, "description", "", "Creating: Performed Procedure Step Description")
	fs.StringVar(&c.stationAET, "station-aet", "", "Creating: Performed Station AE Title (default: -aet)")
	fs.StringVar(&c.stationName, "station-name", "", "Creating: Performed Station Name")

	fs.Var(&c.series, "series", "Completing: a Series Instance UID the step made, a Performed Series Sequence item each (repeatable)")
	fs.StringVar(&c.protocol, "protocol", "", "Completing: Protocol Name of the series, required with -series")
	fs.Var(&c.instances, "instance",
		"Completing: an image the step made, sopClassUID:sopInstanceUID, in the one -series (repeatable)")
}

func (c *MPPSSCUCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("mppsscu", flag.ExitOnError)
	c.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	tlsCfg, err := c.tls.config()
	if err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mppsscu [options] host:port, or with -complete UID or -discontinue UID")
	}

	// What to send is settled before connecting, so a mistake in it costs
	// no association.
	var uid, status string
	switch {
	case c.complete != "" && c.discontinue != "":
		return errors.New("-complete and -discontinue each end a step; give one")
	case c.complete != "":
		uid, status = c.complete, "COMPLETED"
	case c.discontinue != "":
		uid, status = c.discontinue, "DISCONTINUED"
	}
	if status != "" && c.uid != "" {
		return errors.New("-uid names the step to create; the step to end is the UID given to -complete or -discontinue")
	}
	if len(c.instances) > 0 && len(c.series) != 1 {
		return fmt.Errorf("-instance puts images in the one -series; %d -series given (use -json for several series with images)", len(c.series))
	}
	if len(c.series) > 0 && c.protocol == "" {
		return errors.New("-series needs -protocol: Protocol Name is required in each Performed Series Sequence item (PS3.4 Table F.7.2-1)")
	}
	if status != "DISCONTINUED" && status != "COMPLETED" && (len(c.series) > 0 || len(c.instances) > 0) {
		return errors.New("-series and -instance describe what a step made; give them with -complete or -discontinue")
	}

	now := time.Now()
	var ds *dataset.Dataset
	if status == "" {
		if uid = c.uid; uid == "" {
			uid = network.GenerateUID()
		}
		ds, err = c.createDataSet(now)
	} else {
		ds, err = c.endDataSet(status, now)
	}
	if err != nil {
		return err
	}
	if c.jsonFile != "" {
		if err := mergeDICOMJSON(ds, c.jsonFile); err != nil {
			return fmt.Errorf("-json %s: %w", c.jsonFile, err)
		}
	}

	events, closeEvents, err := c.events.open()
	if err != nil {
		return err
	}
	defer closeEvents()
	address := fs.Arg(0)
	scu := network.NewSCU(network.SCUConfig{
		TLS:       tlsCfg,
		Events:    events,
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
	contexts := []network.PresentationContextItem{{
		ID:               1,
		AbstractSyntax:   network.ModalityPerformedProcedureStepUID,
		TransferSyntaxes: network.DefaultTransferSyntaxes(),
	}}
	if err := scu.Associate(ctx, contexts); err != nil {
		return fmt.Errorf("association failed: %w", err)
	}
	defer func() { _ = scu.Release(ctx) }()

	if c.verbose {
		fmt.Println("Sending:")
		printDataSet(os.Stdout, ds, "  ")
	}
	var operation string
	var code uint16
	var returned *dataset.Dataset
	if status == "" {
		operation = "N-CREATE"
		resp, err := scu.NCreate(ctx, network.ModalityPerformedProcedureStepUID, uid, ds)
		if err != nil {
			return fmt.Errorf("N-CREATE failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
		// The SCP may assign the UID when none is sent; one was, and an
		// SCP that answers with another is reported by it.
		if resp.AffectedSOPInstance != "" {
			uid = resp.AffectedSOPInstance
		}
	} else {
		operation = "N-SET"
		resp, err := scu.NSet(ctx, network.ModalityPerformedProcedureStepUID, uid, ds)
		if err != nil {
			return fmt.Errorf("N-SET failed: %w", err)
		}
		code, returned = resp.Status, resp.DataSet
	}

	fmt.Printf("%s %s (status 0x%04X)\n", operation, network.CategorizeStatus(code), code)
	if c.verbose && returned != nil {
		fmt.Println("Returned:")
		printDataSet(os.Stdout, returned, "  ")
	}
	if network.IsFailure(code) {
		return fmt.Errorf("the SCP refused the %s: status 0x%04X", operation, code)
	}
	if status == "" {
		fmt.Printf("Performed Procedure Step %s is IN PROGRESS\n", uid)
		fmt.Printf("End it with: mppsscu -complete %s (or -discontinue) %s\n", uid, address)
	} else {
		fmt.Printf("Performed Procedure Step %s is %s\n", uid, status)
	}
	return nil
}

// mppsAttribute is one attribute of an MPPS data set: a tag, its VR and its
// value, empty for a Type 2 attribute with nothing to say.
type mppsAttribute struct {
	tag   tag.Tag
	vr    dataelem.VR
	value string
}

// addAll adds attributes to a data set.
func addAll(ds *dataset.Dataset, attributes []mppsAttribute) error {
	for _, a := range attributes {
		if err := ds.Add(dataelem.NewDataElement(a.tag, a.vr, []byte(a.value))); err != nil {
			return err
		}
	}
	return nil
}

// addSequence adds a sequence of the given items, none for an empty one.
func addSequence(ds *dataset.Dataset, t tag.Tag, items ...*dataset.Dataset) error {
	s := sequence.New()
	for _, item := range items {
		if err := s.Append(item); err != nil {
			return err
		}
	}
	return ds.AddSequence(t, s)
}

// createDataSet is the N-CREATE data set: every attribute PS3.4 Table
// F.7.2-1 requires at creation, Type 1 with a value and Type 2 present,
// empty when no flag gives it. An SCP may refuse a creation that lacks one
// (0120H, Missing Attribute), so none is left out for being unknown.
func (c *MPPSSCUCommand) createDataSet(now time.Time) (*dataset.Dataset, error) {
	studyUID := c.studyUID
	if studyUID == "" {
		studyUID = network.GenerateUID()
	}
	stepID := c.stepID
	if stepID == "" {
		// Performed Procedure Step ID is SH, 16 characters.
		stepID = now.Format("060102150405")
	}
	station := c.stationAET
	if station == "" {
		station = c.callingAE
	}

	scheduled := dataset.NewDataset()
	if err := addAll(scheduled, []mppsAttribute{
		{tag.New(0x0008, 0x0050), dataelem.SH, c.accession},            // Accession Number
		{tag.New(0x0020, 0x000D), dataelem.UI, studyUID},               // Study Instance UID
		{tag.New(0x0032, 0x1060), dataelem.LO, ""},                     // Requested Procedure Description
		{tag.New(0x0040, 0x0007), dataelem.LO, ""},                     // Scheduled Procedure Step Description
		{tag.New(0x0040, 0x0009), dataelem.SH, c.scheduledStepID},      // Scheduled Procedure Step ID
		{tag.New(0x0040, 0x1001), dataelem.SH, c.requestedProcedureID}, // Requested Procedure ID
	}); err != nil {
		return nil, err
	}
	for _, empty := range []tag.Tag{
		tag.New(0x0008, 0x1110), // Referenced Study Sequence
		tag.New(0x0040, 0x0008), // Scheduled Protocol Code Sequence
	} {
		if err := addSequence(scheduled, empty); err != nil {
			return nil, err
		}
	}

	ds := dataset.NewDataset()
	if err := addAll(ds, []mppsAttribute{
		{tag.New(0x0008, 0x0060), dataelem.CS, c.modality},             // Modality
		{tag.New(0x0010, 0x0010), dataelem.PN, c.patientName},          // Patient's Name
		{tag.New(0x0010, 0x0020), dataelem.LO, c.patientID},            // Patient ID
		{tag.New(0x0010, 0x0030), dataelem.DA, c.birthDate},            // Patient's Birth Date
		{tag.New(0x0010, 0x0040), dataelem.CS, c.sex},                  // Patient's Sex
		{tag.New(0x0020, 0x0010), dataelem.SH, ""},                     // Study ID
		{tag.New(0x0040, 0x0241), dataelem.AE, station},                // Performed Station AE Title
		{tag.New(0x0040, 0x0242), dataelem.SH, c.stationName},          // Performed Station Name
		{tag.New(0x0040, 0x0243), dataelem.SH, ""},                     // Performed Location
		{tag.New(0x0040, 0x0244), dataelem.DA, now.Format("20060102")}, // Performed Procedure Step Start Date
		{tag.New(0x0040, 0x0245), dataelem.TM, now.Format("150405")},   // Performed Procedure Step Start Time
		{tag.New(0x0040, 0x0250), dataelem.DA, ""},                     // Performed Procedure Step End Date
		{tag.New(0x0040, 0x0251), dataelem.TM, ""},                     // Performed Procedure Step End Time
		{tag.New(0x0040, 0x0252), dataelem.CS, "IN PROGRESS"},          // Performed Procedure Step Status
		{tag.New(0x0040, 0x0253), dataelem.SH, stepID},                 // Performed Procedure Step ID
		{tag.New(0x0040, 0x0254), dataelem.LO, c.description},          // Performed Procedure Step Description
		{tag.New(0x0040, 0x0255), dataelem.LO, ""},                     // Performed Procedure Type Description
	}); err != nil {
		return nil, err
	}
	if err := addSequence(ds, tag.New(0x0040, 0x0270), scheduled); err != nil { // Scheduled Step Attributes Sequence
		return nil, err
	}
	for _, empty := range []tag.Tag{
		tag.New(0x0008, 0x1032), // Procedure Code Sequence
		tag.New(0x0008, 0x1120), // Referenced Patient Sequence
		tag.New(0x0040, 0x0260), // Performed Protocol Code Sequence
		tag.New(0x0040, 0x0340), // Performed Series Sequence
	} {
		if err := addSequence(ds, empty); err != nil {
			return nil, err
		}
	}
	return ds, nil
}

// endDataSet is the N-SET data set that makes a step final: its status,
// its end date and time, which are required once it is (PS3.4 Table
// F.7.2-1), and the Performed Series Sequence when series are given.
func (c *MPPSSCUCommand) endDataSet(status string, now time.Time) (*dataset.Dataset, error) {
	ds := dataset.NewDataset()
	if err := addAll(ds, []mppsAttribute{
		{tag.New(0x0040, 0x0250), dataelem.DA, now.Format("20060102")}, // Performed Procedure Step End Date
		{tag.New(0x0040, 0x0251), dataelem.TM, now.Format("150405")},   // Performed Procedure Step End Time
		{tag.New(0x0040, 0x0252), dataelem.CS, status},                 // Performed Procedure Step Status
	}); err != nil {
		return nil, err
	}
	if len(c.series) == 0 {
		return ds, nil
	}
	var items []*dataset.Dataset
	for _, seriesUID := range c.series {
		item := dataset.NewDataset()
		if err := addAll(item, []mppsAttribute{
			{tag.New(0x0008, 0x0054), dataelem.AE, ""},         // Retrieve AE Title
			{tag.New(0x0008, 0x1050), dataelem.PN, ""},         // Performing Physician's Name
			{tag.New(0x0008, 0x103E), dataelem.LO, ""},         // Series Description
			{tag.New(0x0008, 0x1070), dataelem.PN, ""},         // Operators' Name
			{tag.New(0x0018, 0x1030), dataelem.LO, c.protocol}, // Protocol Name
			{tag.New(0x0020, 0x000E), dataelem.UI, seriesUID},  // Series Instance UID
		}); err != nil {
			return nil, err
		}
		var images []*dataset.Dataset
		for _, ref := range c.instances {
			image := dataset.NewDataset()
			if err := addAll(image, []mppsAttribute{
				{tag.New(0x0008, 0x1150), dataelem.UI, ref.SOPClassUID},    // Referenced SOP Class UID
				{tag.New(0x0008, 0x1155), dataelem.UI, ref.SOPInstanceUID}, // Referenced SOP Instance UID
			}); err != nil {
				return nil, err
			}
			images = append(images, image)
		}
		if err := addSequence(item, tag.New(0x0008, 0x1140), images...); err != nil { // Referenced Image Sequence
			return nil, err
		}
		if err := addSequence(item, tag.New(0x0040, 0x0220)); err != nil { // Referenced Non-Image Composite SOP Instance Sequence
			return nil, err
		}
		items = append(items, item)
	}
	if err := addSequence(ds, tag.New(0x0040, 0x0340), items...); err != nil { // Performed Series Sequence
		return nil, err
	}
	return ds, nil
}

// mergeDICOMJSON adds the attributes of the DICOM JSON object in a file to
// a data set, replacing those it already has.
func mergeDICOMJSON(ds *dataset.Dataset, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := bytes.TrimSpace(data)
	if len(text) > 0 && text[0] == '[' {
		var objects []json.RawMessage
		if err := json.Unmarshal(text, &objects); err != nil {
			return fmt.Errorf("not DICOM JSON: %w", err)
		}
		if len(objects) != 1 {
			return fmt.Errorf("the array holds %d objects; one data set is sent", len(objects))
		}
		text = objects[0]
	}
	from := dataset.NewDataset()
	if err := from.FromDICOMJSONString(string(text)); err != nil {
		return err
	}
	for _, element := range from.GetAll() {
		t, ok := element.GetTag().(tag.Tag)
		if !ok {
			continue
		}
		ds.Remove(t)
		if err := ds.Add(element); err != nil {
			return err
		}
	}
	return nil
}
