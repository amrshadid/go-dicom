package anonymize

import (
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The profiles work on attributes. A name drawn into the image is not one,
// and it is what ultrasound machines, screen captures and secondary captures
// routinely carry. PS3.15 E.3.1 gives it an option of its own, Clean Pixel
// Data, and until this the library could neither apply it nor tell a caller
// that an image might need it (#233).

var (
	tagBurnedInAnnotation = tag.New(0x0028, 0x0301)
	cleanPixelDataCode    = methodCode{"113101", "Clean Pixel Data Option"}
)

// CleanPixelData fills the regions with black in every frame, sets Burned In
// Annotation to NO, and records the Clean Pixel Data Option as a method used.
//
// The regions are the caller's: this does not find text. It makes the claim
// that the image holds none once they are filled, so they have to cover all
// of it.
//
// Compressed pixel data is decompressed first, with
// [dataset.Dataset.DecompressInPlace], and stays native: the images that
// carry burned-in text are mostly stored compressed (#240). Nothing is
// encoded again, so cleaning adds no loss of its own, and the data set
// grows. See [dataset.Dataset.RedactPixelRegions] for the layouts that are
// refused. On an error the data set is left as it was, compressed if it was.
//
// It is independent of Anonymize and may run before or after it. Alone it
// does not set Patient Identity Removed, which is the profile's to say.
func (a *Anonymizer) CleanPixelData(ds *dataset.Dataset, regions ...dataset.PixelRegion) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}
	// On a copy, so a region that turns out to miss the image does not leave
	// the caller's data set decompressed and uncleaned.
	work := ds.Clone()
	if err := work.DecompressInPlace(); err != nil {
		return err
	}
	if err := work.RedactPixelRegions(regions...); err != nil {
		return err
	}
	for _, t := range []tag.Tag{
		tag.New(0x7FE0, 0x0010), tag.New(0x0028, 0x0004), tag.New(0x0028, 0x0006),
	} {
		if elem, ok := work.Get(t); ok {
			ds.Remove(t)
			if err := ds.Add(elem); err != nil {
				return fmt.Errorf("replacing %s: %w", t, err)
			}
		}
	}
	ds.SetTransferSyntaxUID(work.TransferSyntaxUID())

	ds.Remove(tagBurnedInAnnotation)
	if err := ds.Add(dataelem.NewDataElement(tagBurnedInAnnotation, dataelem.CS, "NO")); err != nil {
		return fmt.Errorf("setting Burned In Annotation: %w", err)
	}
	if hasMethodCode(ds, cleanPixelDataCode) {
		return nil
	}
	_, identityRemoved := ds.Get(tag.New(0x0012, 0x0062))
	return writeMethod(ds, append(recordedMethodCodes(ds), cleanPixelDataCode), identityRemoved)
}

// recordedMethodCodes returns the methods ds already records.
func recordedMethodCodes(ds *dataset.Dataset) []methodCode {
	elem, ok := ds.Get(tagMethodCodes)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil
	}
	var codes []methodCode
	for _, item := range seq.Items() {
		if recorded, ok := item.(*dataset.Dataset); ok {
			codes = append(codes, methodCode{
				value:   textOf(recorded, tag.New(0x0008, 0x0100)),
				meaning: textOf(recorded, tag.New(0x0008, 0x0104)),
			})
		}
	}
	return codes
}

// drawnOnModalities are the kinds of image whose equipment commonly draws
// the patient's name into the pixels: ultrasound, endoscopy and other
// captured video, and images that are pictures of a screen or a document.
var drawnOnModalities = map[string]string{
	"US":   "ultrasound",
	"IVUS": "intravascular ultrasound",
	"ES":   "endoscopy",
	"GM":   "general microscopy",
	"XC":   "external-camera photography",
	"SC":   "secondary capture",
	"OT":   "other",
	"XA":   "X-ray angiography",
	"RF":   "radiofluoroscopy",
}

// secondaryCaptureClasses is the prefix of the Secondary Capture Image
// Storage SOP classes, single and multi-frame.
const secondaryCaptureClasses = "1.2.840.10008.5.1.4.1.1.7"

// BurnedInRisk reports whether an image may hold identifying text drawn into
// its pixels, and why.
//
// Burned In Annotation (0028,0301) decides when it is present. When it is
// not, the modality and SOP class do: this answers for the kinds of image
// that are commonly drawn on, and is no finding about this one. False means
// nothing suggests text, not that the pixels were examined. A data set with
// no Pixel Data is no risk.
func BurnedInRisk(ds *dataset.Dataset) (risk bool, reason string) {
	if ds == nil {
		return false, ""
	}
	if _, ok := ds.Get(tag.New(0x7FE0, 0x0010)); !ok {
		return false, ""
	}
	switch strings.ToUpper(textOf(ds, tagBurnedInAnnotation)) {
	case "YES":
		return true, "Burned In Annotation (0028,0301) is YES"
	case "NO":
		return false, ""
	}

	sopClass := textOf(ds, tag.New(0x0008, 0x0016))
	if sopClass == secondaryCaptureClasses || strings.HasPrefix(sopClass, secondaryCaptureClasses+".") {
		return true, "it is a secondary capture, and Burned In Annotation (0028,0301) does not say there is none"
	}
	modality := strings.ToUpper(textOf(ds, tag.New(0x0008, 0x0060)))
	if kind, ok := drawnOnModalities[modality]; ok {
		return true, fmt.Sprintf("its modality is %s (%s), and Burned In Annotation (0028,0301) does not say there is none",
			modality, kind)
	}
	return false, ""
}
