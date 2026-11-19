package network

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/internal/heldvalue"
	"github.com/amrshadid/go-dicom/tag"
)

// TestValuesHeldAsGoValuesAreReadAsBytesAre proves that the readers of the
// network package answer the same for a data set or command set whose values
// are held as strings, lists and numbers as for the same one held as bytes,
// which is what a decoded message holds.
//
// Each asserted []byte. A storage commitment report built in memory read as no
// instances, a UPS state held as a string read as no state, a Filtered Global
// subscription's keys were dropped, an Attribute Identifier List held as tags
// was refused, a command set built with numbers could not be encoded, and a
// SOP Instance UID held as a []string went out as "[1.2.3]" (#420).
func TestValuesHeldAsGoValuesAreReadAsBytesAre(t *testing.T) {
	const class, instance = "1.2.840.10008.5.1.4.1.1.2", "1.2.826.0.1.3680043.10.511.420.5"

	t.Run("storage commitment references", func(t *testing.T) {
		report := dataset.NewDataset()
		if err := addReferencedSequence(report, tagFailedSOPSequence,
			[]SOPInstanceReference{{SOPClassUID: class, SOPInstanceUID: instance}}, []uint16{0x0112}); err != nil {
			t.Fatal(err)
		}
		wantRefs, wantReasons, err := readReferencedSequence(report, tagFailedSOPSequence)
		if err != nil {
			t.Fatal(err)
		}
		gotRefs, gotReasons, err := readReferencedSequence(heldvalue.AsGoValues(report), tagFailedSOPSequence)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(wantRefs, gotRefs) || !reflect.DeepEqual(wantReasons, gotReasons) {
			t.Errorf("held as bytes: %v %v; held as Go values: %v %v", wantRefs, wantReasons, gotRefs, gotReasons)
		}
	})

	t.Run("UPS state and subscription keys", func(t *testing.T) {
		ds := dataset.NewDataset()
		_ = ds.Add(dataelem.NewDataElement(tagProcedureStepState, dataelem.CS, []byte("SCHEDULED ")))
		_ = ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("PID12345")))
		held := heldvalue.AsGoValues(ds)
		if want, got := upsStringValue(ds, tagProcedureStepState), upsStringValue(held, tagProcedureStepState); want != got {
			t.Errorf("Procedure Step State: %q held as bytes, %q held as a string", want, got)
		}
		count := func(keys *dataset.Dataset) int {
			if keys == nil {
				return 0 // no keys at all
			}
			return len(keys.GetAll())
		}
		if want, got := count(upsFilterKeys(ds)), count(upsFilterKeys(held)); want != got {
			t.Errorf("filter keys: %d held as bytes, %d held as strings", want, got)
		}
	})

	t.Run("command set", func(t *testing.T) {
		cmd := BuildCStoreRQ(7, class, instance, 0)
		_ = cmd.Add(dataelem.NewDataElement(tagAttributeIdentifierList, dataelem.AT,
			[]byte{0x10, 0x00, 0x10, 0x00, 0x10, 0x00, 0x20, 0x00}))
		held := heldvalue.AsGoValues(cmd)

		want, err := EncodeCommandDataset(cmd)
		if err != nil {
			t.Fatal(err)
		}
		got, err := EncodeCommandDataset(held)
		if err != nil {
			t.Fatalf("a command set held as Go values does not encode: %v", err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("encoded:\n% x held as bytes\n% x held as Go values", want, got)
		}

		wantID, err1 := getUSValue(cmd, tagMessageID)
		gotID, err2 := getUSValue(held, tagMessageID)
		if wantID != gotID || (err1 == nil) != (err2 == nil) {
			t.Errorf("Message ID: %d (%v) held as bytes, %d (%v) held as a uint16", wantID, err1, gotID, err2)
		}
		wantUID, err1 := getUIValue(cmd, tagAffectedSOPClassUID)
		gotUID, err2 := getUIValue(held, tagAffectedSOPClassUID)
		if wantUID != gotUID || (err1 == nil) != (err2 == nil) {
			t.Errorf("Affected SOP Class UID: %q (%v) held as bytes, %q (%v) held as a string", wantUID, err1, gotUID, err2)
		}
		wantTags, err1 := attributeIdentifierList(cmd)
		gotTags, err2 := attributeIdentifierList(held)
		if !reflect.DeepEqual(wantTags, gotTags) || (err1 == nil) != (err2 == nil) {
			t.Errorf("Attribute Identifier List: %v (%v) held as bytes, %v (%v) held as tags", wantTags, err1, gotTags, err2)
		}
	})

	t.Run("instance UIDs", func(t *testing.T) {
		asBytes := dataset.NewDataset()
		_ = asBytes.Add(dataelem.NewDataElement(tagSOPClassUID, dataelem.UI, []byte(class+"\x00")))
		_ = asBytes.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, []byte(instance)))
		asList := dataset.NewDataset()
		_ = asList.Add(dataelem.NewDataElement(tagSOPClassUID, dataelem.UI, []string{class}))
		_ = asList.Add(dataelem.NewDataElement(tagSOPInstanceUID, dataelem.UI, []string{instance}))
		wc, wi, wok := instanceUIDs(asBytes)
		gc, gi, gok := instanceUIDs(asList)
		if wc != gc || wi != gi || wok != gok {
			t.Errorf("held as bytes: %q %q %v; held as []string: %q %q %v", wc, wi, wok, gc, gi, gok)
		}
	})
}
