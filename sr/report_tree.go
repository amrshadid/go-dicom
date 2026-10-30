package sr

import (
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// StructuredReport's findings, observations, conclusions and references are
// written as a PS3.3 C.17 content tree, which is the only place an SR
// document says anything. ToDataset used to put the number of findings into
// Concept Name Code Sequence as IS and the first finding into Study
// Description, and dcmtk refused the file (#216).
//
// Standard codes are used where one says what the model means. The model's
// own fields with no standard concept, a finding's identifier and confidence
// and an observation's code, use the local scheme 99GODICOM, which PS3.16 8.2
// reserves the 99 prefix for.

const localScheme = "99GODICOM"

var (
	codeReportTitle  = Code{Value: "18748-4", SchemeDesignator: "LN", Meaning: "Diagnostic Imaging Report"}
	codeFinding      = Code{Value: "121071", SchemeDesignator: "DCM", Meaning: "Finding"}
	codeConclusion   = Code{Value: "121077", SchemeDesignator: "DCM", Meaning: "Conclusion"}
	codeFindingSite  = Code{Value: "363698007", SchemeDesignator: "SCT", Meaning: "Finding Site"}
	codeSeverity     = Code{Value: "246112005", SchemeDesignator: "SCT", Meaning: "Severity"}
	codeFindingID    = Code{Value: "FINDING_ID", SchemeDesignator: localScheme, Meaning: "Finding Identifier"}
	codeConfidence   = Code{Value: "CONFIDENCE", SchemeDesignator: localScheme, Meaning: "Confidence"}
	codeNoUnits      = Code{Value: "1", SchemeDesignator: "UCUM", Meaning: "no units"}
	basicTextSRClass = "1.2.840.10008.5.1.4.1.1.88.11"
)

// contentTree builds the report's content tree. A Basic Text SR has no NUM
// value type (PS3.3 A.35.1), so there an observation is written as TEXT.
func (r *StructuredReport) contentTree() *ContentItem {
	title := codeReportTitle
	root := &ContentItem{ValueType: ValueTypeContainer, ConceptName: &title, ContinuityOfContent: "SEPARATE"}
	if r.ReportContent == nil {
		return root
	}
	content := r.ReportContent

	for _, f := range content.Findings {
		item := textItem(RelationshipContains, codeFinding, f.Description)
		for _, prop := range []struct {
			code  Code
			value string
		}{
			{codeFindingID, f.ID},
			{codeFindingSite, f.Location},
			{codeSeverity, f.Severity},
			{codeConfidence, formatConfidence(f.Confidence)},
		} {
			if prop.value != "" {
				item.Children = append(item.Children, textItem(RelationshipHasProperties, prop.code, prop.value))
			}
		}
		root.Children = append(root.Children, item)
	}

	for _, o := range content.Observations {
		concept := Code{Value: o.Code, SchemeDesignator: localScheme, Meaning: o.Code}
		if _, err := strconv.ParseFloat(o.Value, 64); err != nil || r.SOPClassUID == basicTextSRClass {
			value := strings.TrimSpace(o.Value + " " + o.Unit)
			root.Children = append(root.Children, textItem(RelationshipContains, concept, value))
			continue
		}
		units := codeNoUnits
		if o.Unit != "" {
			units = Code{Value: o.Unit, SchemeDesignator: "UCUM", Meaning: o.Unit}
		}
		root.Children = append(root.Children, &ContentItem{
			ValueType: ValueTypeNum, RelationshipType: RelationshipContains, ConceptName: &concept,
			Measurement: &Measurement{Value: o.Value, Units: &units},
		})
	}

	for _, c := range content.Conclusions {
		root.Children = append(root.Children, textItem(RelationshipContains, codeConclusion, c))
	}

	for _, ref := range content.References {
		valueType := ValueTypeComposite
		switch strings.ToUpper(ref.ReferenceType) {
		case ValueTypeImage:
			valueType = ValueTypeImage
		case ValueTypeWaveform:
			valueType = ValueTypeWaveform
		}
		root.Children = append(root.Children, &ContentItem{
			ValueType: valueType, RelationshipType: RelationshipContains,
			Reference: &Reference{SOPClassUID: ref.ReferencedSOPClassUID, SOPInstanceUID: ref.ReferencedSOPInstanceUID},
		})
	}
	return root
}

func textItem(relationship string, concept Code, text string) *ContentItem {
	return &ContentItem{ValueType: ValueTypeText, RelationshipType: relationship, ConceptName: &concept, Text: text}
}

func formatConfidence(c float64) string {
	if c == 0 {
		return ""
	}
	return strconv.FormatFloat(c, 'f', -1, 64)
}

// readReportContent is contentTree's inverse, for a tree this package wrote
// and for any other SR document: items it does not recognize are left out,
// not misread.
func readReportContent(root *ContentItem) *ReportContent {
	content := &ReportContent{
		Findings: []Finding{}, Observations: []Observation{}, Conclusions: []string{}, References: []ReportReference{},
	}
	root.Walk(func(item *ContentItem) bool {
		switch {
		case item.ConceptName != nil && sameConcept(*item.ConceptName, codeFinding) && item.ValueType == ValueTypeText:
			f := Finding{Description: item.Text}
			for _, prop := range item.Children {
				if prop.ConceptName == nil {
					continue
				}
				switch {
				case sameConcept(*prop.ConceptName, codeFindingID):
					f.ID = prop.Text
				case sameConcept(*prop.ConceptName, codeFindingSite):
					f.Location = prop.Text
				case sameConcept(*prop.ConceptName, codeSeverity):
					f.Severity = prop.Text
				case sameConcept(*prop.ConceptName, codeConfidence):
					f.Confidence, _ = strconv.ParseFloat(prop.Text, 64)
				}
			}
			content.Findings = append(content.Findings, f)
			return false
		case item.ConceptName != nil && sameConcept(*item.ConceptName, codeConclusion) && item.ValueType == ValueTypeText:
			content.Conclusions = append(content.Conclusions, item.Text)
		case item.ConceptName != nil && item.ConceptName.SchemeDesignator == localScheme && item.ValueType == ValueTypeNum &&
			item.Measurement != nil:
			o := Observation{Code: item.ConceptName.Value, Value: item.Measurement.Value}
			if u := item.Measurement.Units; u != nil && !sameConcept(*u, codeNoUnits) {
				o.Unit = u.Value
			}
			content.Observations = append(content.Observations, o)
		case item.ConceptName != nil && item.ConceptName.SchemeDesignator == localScheme && item.ValueType == ValueTypeText &&
			item.RelationshipType == RelationshipContains:
			value, unit := item.Text, ""
			if i := strings.LastIndexByte(value, ' '); i > 0 {
				if _, err := strconv.ParseFloat(value[:i], 64); err == nil {
					value, unit = value[:i], value[i+1:]
				}
			}
			content.Observations = append(content.Observations, Observation{Code: item.ConceptName.Value, Value: value, Unit: unit})
		case item.Reference != nil:
			content.References = append(content.References, ReportReference{
				ReferencedSOPClassUID: item.Reference.SOPClassUID, ReferencedSOPInstanceUID: item.Reference.SOPInstanceUID,
				ReferenceType: item.ValueType,
			})
		}
		return true
	})
	return content
}

func sameConcept(a, b Code) bool {
	return a.Value == b.Value && a.SchemeDesignator == b.SchemeDesignator
}

// addEmpty adds an attribute with no value, which is how a Type 2 attribute
// says it is not known.
func addEmpty(ds *dataset.Dataset, t tag.Tag, vr dataelem.VR) {
	if vr == dataelem.SQ {
		_ = ds.Add(dataelem.NewDataElement(t, vr, sequence.New()))
		return
	}
	_ = ds.Add(dataelem.NewDataElement(t, vr, []byte{}))
}

// addRequired adds value, or the attribute empty when there is none.
func addRequired(ds *dataset.Dataset, t tag.Tag, vr dataelem.VR, value string) {
	if value == "" {
		addEmpty(ds, t, vr)
		return
	}
	addString(ds, t, vr, value)
}
