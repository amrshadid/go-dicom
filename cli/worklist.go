package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// worklistKey is one attribute of a Modality Worklist query: its tag, its VR,
// and the value to match, empty to ask for the attribute without matching.
type worklistKey struct {
	tag   tag.Tag
	vr    dataelem.VR
	value string
}

// worklistIdentifier builds the identifier of a Modality Worklist query with
// the keys a modality asks for (PS3.4 K.6.1.2.2, Table K.6-1): the patient,
// the requested procedure and the order, and in the Scheduled Procedure Step
// Sequence the step itself. Those are the keys a worklist SCP is required to
// support; a key it does not know it may ignore, a key missing from the query
// it does not return.
//
// There is no Query/Retrieve Level. Basic Worklist has none, and dcmtk's
// wlmscpfs reports one as an unsupported attribute in the search mask.
func worklistIdentifier(patientName, patientID, modality, date, stationAET string) (*dataset.Dataset, error) {
	ds := dataset.NewDataset()
	top := []worklistKey{
		{tag.New(0x0008, 0x0050), dataelem.SH, ""},          // Accession Number
		{tag.New(0x0008, 0x0090), dataelem.PN, ""},          // Referring Physician's Name
		{tag.New(0x0010, 0x0010), dataelem.PN, patientName}, // Patient's Name
		{tag.New(0x0010, 0x0020), dataelem.LO, patientID},   // Patient ID
		{tag.New(0x0010, 0x0030), dataelem.DA, ""},          // Patient's Birth Date
		{tag.New(0x0010, 0x0040), dataelem.CS, ""},          // Patient's Sex
		{tag.New(0x0020, 0x000D), dataelem.UI, ""},          // Study Instance UID
		{tag.New(0x0032, 0x1060), dataelem.LO, ""},          // Requested Procedure Description
		{tag.New(0x0040, 0x1001), dataelem.SH, ""},          // Requested Procedure ID
	}
	for _, k := range top {
		if err := ds.Add(dataelem.NewDataElement(k.tag, k.vr, []byte(k.value))); err != nil {
			return nil, err
		}
	}

	// The step's keys are matched inside its one item (PS3.4 C.2.2.2.6): a
	// worklist entry matches when one of its scheduled steps does.
	step := dataset.NewDataset()
	for _, k := range []worklistKey{
		{tag.New(0x0008, 0x0060), dataelem.CS, modality},   // Modality
		{tag.New(0x0040, 0x0001), dataelem.AE, stationAET}, // Scheduled Station AE Title
		{tag.New(0x0040, 0x0002), dataelem.DA, date},       // Scheduled Procedure Step Start Date
		{tag.New(0x0040, 0x0003), dataelem.TM, ""},         // Scheduled Procedure Step Start Time
		{tag.New(0x0040, 0x0006), dataelem.PN, ""},         // Scheduled Performing Physician's Name
		{tag.New(0x0040, 0x0007), dataelem.LO, ""},         // Scheduled Procedure Step Description
		{tag.New(0x0040, 0x0009), dataelem.SH, ""},         // Scheduled Procedure Step ID
		{tag.New(0x0040, 0x0010), dataelem.SH, ""},         // Scheduled Station Name
		{tag.New(0x0040, 0x0011), dataelem.SH, ""},         // Scheduled Procedure Step Location
	} {
		if err := step.Add(dataelem.NewDataElement(k.tag, k.vr, []byte(k.value))); err != nil {
			return nil, err
		}
	}
	steps := sequence.New()
	if err := steps.Append(step); err != nil {
		return nil, err
	}
	if err := ds.AddSequence(tag.New(0x0040, 0x0100), steps); err != nil {
		return nil, err
	}
	return ds, nil
}

// worklistDate turns -date into a DA matching value: "today" is this
// machine's date, and anything else is passed as given, so a range such as
// 20261118-20261120 works as PS3.4 C.2.2.2.5 defines it.
func worklistDate(value string, now time.Time) string {
	if strings.EqualFold(value, "today") {
		return now.Format("20060102")
	}
	return value
}

// printDataSet writes a C-FIND answer's attributes, one per line, and a
// sequence's items indented under it. A worklist entry keeps the step it
// schedules in a sequence, and printing values alone left it out.
func printDataSet(out io.Writer, ds *dataset.Dataset, indent string) {
	for _, elem := range ds.GetAll() {
		name := ""
		if t, ok := elem.GetTag().(tag.Tag); ok {
			name = t.GetName()
		}
		if value, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			fmt.Fprintf(out, "%s%s\n", indent, name)
			for i := 0; i < value.Length(); i++ {
				raw, err := value.Get(i)
				if err != nil {
					continue
				}
				item, ok := raw.(*dataset.Dataset)
				if !ok {
					continue
				}
				fmt.Fprintf(out, "%s  Item %d\n", indent, i+1)
				printDataSet(out, item, indent+"    ")
			}
			continue
		}
		// Through Bytes: a value held as anything but []byte was left out
		// of the line altogether (#420).
		if raw, ok := elem.Bytes(); ok {
			fmt.Fprintf(out, "%s%-40s %s\n", indent, name, string(raw))
		}
	}
}
