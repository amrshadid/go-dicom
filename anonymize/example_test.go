package anonymize_test

import (
	"fmt"
	"log"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// De-identification per DICOM PS3.15 Annex E. Anonymize mutates the data set
// in place, so clone it first if the original is still needed.
func Example() {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010),
		dataelem.PN, []byte("Doe^John")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060),
		dataelem.CS, []byte("CT")))

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	if err := a.Anonymize(ds); err != nil {
		log.Fatal(err)
	}

	// Modality is not identifying, so it survives unchanged.
	if elem, ok := ds.Get(tag.New(0x0008, 0x0060)); ok {
		fmt.Printf("modality: %s\n", elem.GetValue())
	}

	// Patient's Name is emptied rather than deleted. PS3.15 Table E.1-1 gives
	// it action Z — a zero-length value — so the attribute stays present for
	// readers that require it while carrying nothing. This example used to
	// print ANONYMOUS, from a hand-written table that replaced it with a dummy
	// instead; a site that prefers a dummy can ask for one with
	// SetCustomAction.
	if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
		fmt.Printf("patient name: %q\n", elem.GetValue())
	}

	// Output:
	// modality: CT
	// patient name: ""
}

// The action for any individual tag can be overridden, which is how site
// policy is expressed on top of the standard profile.
func ExampleAnonymizer_SetCustomAction() {
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0050),
		dataelem.SH, []byte("ACC-001")))

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)
	// This site treats the accession number as non-identifying.
	a.SetCustomAction(tag.New(0x0008, 0x0050), anonymize.ActionKeep)

	if err := a.Anonymize(ds); err != nil {
		log.Fatal(err)
	}

	if elem, ok := ds.Get(tag.New(0x0008, 0x0050)); ok {
		fmt.Printf("accession: %s\n", elem.GetValue())
	}

	// Output: accession: ACC-001
}

// UID remapping is consistent within one Anonymizer, which is what preserves
// the study/series/instance hierarchy across the files of a study. Reuse a
// single Anonymizer for a whole study; creating one per file breaks those
// relationships because each generates a fresh mapping.
func ExampleAnonymizer_GetUIDMapping() {
	const studyUID = "1.2.840.113619.2.55.3.604688.1"

	makeDataset := func() *dataset.Dataset {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x000D),
			dataelem.UI, []byte(studyUID)))
		return ds
	}

	a := anonymize.NewAnonymizer(anonymize.BasicProfile)

	first, second := makeDataset(), makeDataset()
	if err := a.Anonymize(first); err != nil {
		log.Fatal(err)
	}
	if err := a.Anonymize(second); err != nil {
		log.Fatal(err)
	}

	e1, ok1 := first.Get(tag.New(0x0020, 0x000D))
	e2, ok2 := second.Get(tag.New(0x0020, 0x000D))
	if !ok1 || !ok2 {
		fmt.Println("study UID was removed rather than remapped")
		return
	}

	same := string(e1.GetValue().([]byte)) == string(e2.GetValue().([]byte))
	changed := string(e1.GetValue().([]byte)) != studyUID
	fmt.Printf("remapped: %t, consistent across files: %t\n", changed, same)

	// The mapping links back to the original identities. Treat it as
	// sensitive: store it apart from the anonymized data, or discard it.
	fmt.Printf("mapping entries: %d\n", len(a.GetUIDMapping()))

	// Output:
	// remapped: true, consistent across files: true
	// mapping entries: 1
}
