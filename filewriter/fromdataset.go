package filewriter

import (
	"fmt"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
)

// ElementsFromDataset converts a Dataset into elements this package can write,
// descending into sequences.
//
// Reading a file and writing it back needs this conversion, and without it every
// caller writes their own — which is how sequences get lost. A sequence holds
// child data sets rather than a byte value, so a conversion that copies Value
// and ignores Items produces a file that looks complete and has every nested
// item missing. That failure is silent: the element is present, its length is
// zero, and nothing reports it.
//
// Each element is written with the VR dataset.ResolveVR settles on. A data set
// read from an Implicit VR file holds the dictionary's VR, and for Pixel Data
// that is "OB or OW". Copied into an Explicit VR header it put "OB" in the VR
// field and " or OW" where the reserved bytes and the length belong, so a reader
// took the length from "r OW" and lost Pixel Data and whatever followed it.
//
// Elements whose tag cannot be read are dropped, with a warning naming the type
// found. There is no tag to write them under, so the alternative is refusing the
// whole data set for one unreadable element.
func ElementsFromDataset(ds *dataset.Dataset) []*DataElement {
	return elementsFromDataset(ds, nil)
}

// elementsFromDataset converts ds as an item of enclosing, nearest first, which
// is where an item's elements find the Pixel Representation deciding their VR.
func elementsFromDataset(ds *dataset.Dataset, enclosing []*dataset.Dataset) []*DataElement {
	if ds == nil {
		return nil
	}

	out := make([]*DataElement, 0, ds.Length())
	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			config.Logger.Warn("filewriter: dropping an element with an unreadable tag",
				"type", elem.GetTag())
			continue
		}

		if seq, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			out = append(out, &DataElement{
				Tag:   t,
				VR:    "SQ",
				Items: sequenceItems(seq, append([]*dataset.Dataset{ds}, enclosing...)),
			})
			continue
		}

		// dataelem.ValueBytes renders what a caller may have put in the data set:
		// bytes, text, or Go numbers, which were dropped here until #119. The
		// network encoder uses the same function, so the two cannot disagree.
		vr := ds.ResolveVR(t, elem, enclosing...)
		value, err := dataelem.ValueBytes(vr, elem.GetValue())
		if err != nil {
			// A value that cannot be rendered is reported rather than written as
			// empty. Silently writing nothing is what a discarded type assertion
			// used to do here, and it produced files where every element was
			// present, correctly typed, and empty.
			config.Logger.Warn("filewriter: dropping an element whose value cannot be written",
				"tag", t.String(), "vr", vr, "type", fmt.Sprintf("%T", elem.GetValue()), "err", err)
			continue
		}

		out = append(out, &DataElement{
			Tag:    t,
			VR:     string(vr),
			Value:  value,
			Length: uint32(len(value)),
		})
	}
	return out
}

// sequenceItems converts a sequence's items, recursing through
// elementsFromDataset so nesting of any depth is carried across. enclosing is
// the chain of data sets the items sit inside, nearest first.
func sequenceItems(seq *sequence.Sequence, enclosing []*dataset.Dataset) []*SequenceItem {
	if seq == nil {
		return nil
	}

	items := make([]*SequenceItem, 0, seq.Length())
	for _, raw := range seq.Items() {
		child, ok := raw.(*dataset.Dataset)
		if !ok {
			// Not something this package produced; there is no way to write it
			// as an item, and guessing would corrupt the sequence.
			continue
		}
		items = append(items, &SequenceItem{Elements: elementsFromDataset(child, enclosing)})
	}
	return items
}

// explicitVRLittleEndianUID is what an uncompressed data set is stored as.
const explicitVRLittleEndianUID = "1.2.840.10008.1.2.1"

// StorageTransferSyntax returns the transfer syntax a data set should be written
// in by something that keeps what it is sent: an archive, or a storage SCP.
//
// Uncompressed data is written as Explicit VR Little Endian whatever it arrived
// as. It is the same data under a different encoding, and Explicit VR is the one
// every reader handles without consulting a dictionary.
//
// Encapsulated pixel data keeps the syntax it arrived in, because the syntax is
// the only record of which codec made the fragments. storescp and qrscp wrote
// every instance as Explicit VR Little Endian, so a JPEG instance became a file
// declaring native pixels and holding JPEG fragments — decodable by nothing, and
// unrecoverable, since the syntax that described it was gone.
func StorageTransferSyntax(ds *dataset.Dataset) string {
	if ds != nil && isEncapsulatedSyntax(ds.TransferSyntaxUID()) {
		return ds.TransferSyntaxUID()
	}
	return explicitVRLittleEndianUID
}
