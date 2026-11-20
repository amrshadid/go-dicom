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
	cleanPixelDataCode    = MethodCleanPixelData.Code()
)

// CleanPixelData fills the regions with black in every frame, sets Burned In
// Annotation to NO, and records the Clean Pixel Data Option as a method used.
//
// The regions are the caller's: this does not find text. It makes the claim
// that the image holds none once they are filled, so they have to cover all
// of it.
//
// Compressed pixel data is decompressed, as [dataset.Dataset.DecompressInPlace]
// does, and stays native: the images that carry burned-in text are mostly
// stored compressed (#240). Nothing is encoded again, so cleaning adds no
// loss of its own, and the data set grows. See
// [dataset.Dataset.RedactPixelRegions] for the layouts that are refused. On
// an error the data set is left as it was, compressed if it was.
//
// The image is worked on a frame at a time. Each compressed frame is decoded,
// filled and kept before the next is decoded, so memory is the native image
// that results and one frame, and not those and every frame decoded first.
// Pixel data that was left in the file the data set was read from
// (filereader.DICOMFile.GetDatasetWithValuesInFile) is not read here at all:
// the regions are filled, and a compressed image decoded, as filewriter
// copies the pixels into the output, a frame at a time, so a multi-gigabyte
// image is redacted while it is written and never held. An image whose frames
// will not decode is then refused when it is written, and not here.
//
// It is independent of Anonymize and may run before or after it. Alone it
// does not set Patient Identity Removed, which is the profile's to say.
func (a *Anonymizer) CleanPixelData(ds *dataset.Dataset, regions ...dataset.PixelRegion) error {
	if ds == nil {
		return fmt.Errorf("dataset is nil")
	}
	if err := redactRegions(ds, regions); err != nil {
		return err
	}

	// Patient Identity Removed is the profile's to say: it is rewritten only
	// where it is already there. A method already recorded is not rewritten,
	// which would drop the words of an automatic redaction beside it.
	_, identityRemoved := ds.Get(tagIdentityRemoved)
	codes := recordedMethodCodes(ds)
	if hasMethodCode(ds, cleanPixelDataCode) {
		codes = nil
	} else {
		codes = append(codes, cleanPixelDataCode)
	}
	if codes == nil {
		ds.Remove(tagBurnedInAnnotation)
		if err := ds.Add(dataelem.NewDataElement(tagBurnedInAnnotation, dataelem.CS, "NO")); err != nil {
			return fmt.Errorf("setting Burned In Annotation: %w", err)
		}
		return nil
	}
	return RecordDeidentificationMethod(ds, codes, MethodRecord{
		IdentityRemoved:    identityRemoved,
		BurnedInAnnotation: "NO",
	})
}

// redactRegions fills the regions with black in every frame of ds,
// decompressing it as it goes when it is compressed, one frame at a time
// (dataset.Dataset.DecompressRedactingRegions). A data set that does not
// change on an error, and that holds nothing of the decompressed image but
// the frame being filled.
func redactRegions(ds *dataset.Dataset, regions []dataset.PixelRegion) error {
	return ds.DecompressRedactingRegions(regions...)
}

// AutomaticRedactionNote is what De-identification Method (0012,0063) says
// of an image whose text was found by a detector and not by a person.
const AutomaticRedactionNote = "Burned-in text redacted where detected, unverified"

// RedactDetectedText fills with black, in every frame, the regions in which
// detector finds lettering, and returns them. A nil detector is the one
// NewTextDetector returns.
//
// It is CleanPixelData without CleanPixelData's claim. That one is told
// where the text is by someone who looked, so it sets Burned In Annotation
// to NO and records the Clean Pixel Data Option. A detector can miss, so
// this sets neither: Burned In Annotation is left as it was, and what was
// done is said in De-identification Method (0012,0063) with
// AutomaticRedactionNote, which Anonymize keeps when it runs afterwards.
// An image is clean when a person has looked at it.
//
// When nothing is found, nothing is changed and no regions are returned.
// When what is found covers more of the picture than text does, nothing is
// changed either, and the error wraps ErrImplausibleRedaction: see
// CheckDetectedShare.
func (a *Anonymizer) RedactDetectedText(ds *dataset.Dataset, detector TextDetector) ([]dataset.PixelRegion, error) {
	if ds == nil {
		return nil, fmt.Errorf("dataset is nil")
	}
	regions, err := DetectBurnedInText(ds, detector)
	return a.redactDetected(ds, regions, err)
}

// RedactDetectedTextFrom is RedactDetectedText for a data set that does not
// hold its Pixel Data: it was read with filereader.ReadOptions.SkipBulkData
// and filereader.DICOMFile.GetDatasetWithValuesInFile, and frames
// (filereader.DICOMFile.FrameReader) reads the pixels from the file, a frame
// at a time, to look for text. What it finds is filled when the data set is
// written, with filewriter given the file: see CleanPixelData. It finds, and
// refuses, what RedactDetectedText does.
func (a *Anonymizer) RedactDetectedTextFrom(ds *dataset.Dataset, frames dataset.FrameReader,
	detector TextDetector) ([]dataset.PixelRegion, error) {
	if ds == nil {
		return nil, fmt.Errorf("dataset is nil")
	}
	regions, err := DetectBurnedInTextFrom(ds, frames, detector)
	return a.redactDetected(ds, regions, err)
}

// redactDetected fills what a detector found, as RedactDetectedText says.
func (a *Anonymizer) redactDetected(ds *dataset.Dataset, regions []dataset.PixelRegion, err error) ([]dataset.PixelRegion, error) {
	if err != nil || len(regions) == 0 {
		return nil, err
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return nil, err
	}
	if err := CheckDetectedShare(regions, info.Columns, info.Rows); err != nil {
		return nil, err
	}
	if err := redactRegions(ds, regions); err != nil {
		return nil, err
	}
	return regions, noteAutomaticRedaction(ds)
}

// hasAutomaticNote reports whether ds says its text was redacted
// automatically.
func hasAutomaticNote(ds *dataset.Dataset) bool {
	for _, value := range strings.Split(textOf(ds, tagMethod), `\`) {
		if strings.TrimSpace(value) == AutomaticRedactionNote {
			return true
		}
	}
	return false
}

// noteAutomaticRedaction adds AutomaticRedactionNote to De-identification
// Method, beside whatever it already says.
func noteAutomaticRedaction(ds *dataset.Dataset) error {
	if hasAutomaticNote(ds) {
		return nil
	}
	value := AutomaticRedactionNote
	if existing := textOf(ds, tagMethod); existing != "" {
		value = existing + `\` + value
	}
	ds.Remove(tagMethod)
	if err := ds.Add(dataelem.NewDataElement(tagMethod, dataelem.LO, value)); err != nil {
		return fmt.Errorf("recording the automatic redaction: %w", err)
	}
	return nil
}

// recordedMethodCodes returns the methods ds already records.
func recordedMethodCodes(ds *dataset.Dataset) []MethodCode {
	elem, ok := ds.Get(tagMethodCodes)
	if !ok {
		return nil
	}
	seq, ok := elem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil
	}
	var codes []MethodCode
	for _, item := range seq.Items() {
		if recorded, ok := item.(*dataset.Dataset); ok {
			codes = append(codes, MethodCode{
				Value:   textOf(recorded, tag.New(0x0008, 0x0100)),
				Scheme:  textOf(recorded, tag.New(0x0008, 0x0102)),
				Meaning: textOf(recorded, tag.New(0x0008, 0x0104)),
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
