package attributes

import (
	"context"
	"fmt"

	"github.com/amrshadid/go-dicom/internal/dicomjson"
	fileparse "github.com/amrshadid/go-dicom/internal/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// JSONElement is one element in the DICOM JSON Model (PS3.18 Annex F). It is
// the type dataset.DICOMJSONElement names, so what DICOMJSON returns and what
// dataset.Dataset.ToDICOMJSON returns are the same type and marshal the same.
//
//	type JSONElement struct {
//		VR           string `json:"vr"`
//		Value        []any  `json:"Value,omitempty"`
//		InlineBinary string `json:"InlineBinary,omitempty"`
//		BulkDataURI  string `json:"BulkDataURI,omitempty"`
//	}
//
// Exactly one of Value, InlineBinary and BulkDataURI is present, or none when
// the element is empty. Value is nil for an element with no value and non-nil
// for one with a value, which for a sequence includes one with no items; the
// items of a sequence are in Value as map[string]JSONElement.
type JSONElement = dicomjson.Element

// BulkDataStep is one sequence a value is nested in.
type BulkDataStep struct {
	// Sequence is the tag of the sequence element and Item the zero-based
	// index of the item the value is in.
	Sequence tag.Tag
	Item     int
}

// BulkDataValue is a value DICOMJSON is about to write, offered to
// JSONOptions.BulkData so that the caller can give a URI for it instead.
type BulkDataValue struct {
	// Tag is the element's tag, and VR the VR it is written with: the one the
	// file gave, or for an element with none, or a choice of two, the one the
	// standard's rules settle on (a pixel data of 8 bits allocated is OB).
	Tag tag.Tag
	VR  string
	// Path lists the sequences the element is nested in, outermost first. It
	// is empty for an element of the top level.
	Path []BulkDataStep
	// InFile reports that the file was read with ReadOptions.SkipBulkData and
	// the value was left in it: Value is nil, and Offset and Length say where
	// the bytes are.
	InFile bool
	// Offset is where the value begins in the bytes the file was read from,
	// and Length how many bytes it has. For a value left in the file, Length is
	// what the file states, and for encapsulated Pixel Data the extent of its
	// items. For a deflated file the offset counts inflated bytes (File.Deflated).
	// For a value in memory Length is len(Value), which for text is its length
	// in UTF-8.
	Offset, Length int64
	// Value is the value's bytes, text decoded into UTF-8, or nil for a value
	// left in the file. It must not be modified.
	Value []byte
}

// BulkDataDecision is what JSONOptions.BulkData decides for a value. The zero
// value writes the value as DICOMJSON does without a hook: inline, as base64
// for the binary VRs and as Value for the rest.
type BulkDataDecision struct {
	// URI, when not empty, is written as the element's BulkDataURI in place of
	// its value (PS3.18 F.2.6).
	URI string
	// Omit leaves the element out of the JSON altogether.
	Omit bool
}

// JSONOptions choose how DICOMJSON writes.
type JSONOptions struct {
	// BulkData, when not nil, is called for every value of an element that is
	// not a sequence and has bytes or was left in the file by SkipBulkData, in
	// the order they are written, and decides whether to write it, give its
	// BulkDataURI or leave the element out. It is how a caller chooses by VR,
	// length or tag: base64 turns a 500 KB image into 680 KB of JSON, which
	// most readers hold whole, and DICOMweb serves such values apart for that
	// reason.
	//
	// A value left in the file has no bytes to write, so for it a decision with
	// neither a URI nor Omit is an error.
	BulkData func(BulkDataValue) BulkDataDecision

	// Warn, when not nil, receives each decision about the text that the
	// caller should know about, in the words filereader.DICOMFile.Warnings
	// holds them: text under no usable Specific Character Set decoded as
	// ISO-8859-1, which is a guess. It may be given the same words for an item
	// of a sequence as for the data set around it.
	Warn func(string)
}

// DICOMJSON writes the elements of a data set as the DICOM JSON Model of
// PS3.18 Annex F, without linking the package that builds a data set or the
// image codecs. The result is what dataset.Dataset.ToDICOMJSON returns for
// filereader's GetDataset of the same file, member for member; the two share
// one implementation.
//
// Pass File.DataElements for the data set of a file. The File Meta group is
// not part of it, as it is not part of GetDataset; pass File.MetaElements for
// that, or both together, to write it too.
//
// Text is written in UTF-8 as GetDataset decodes it under the Specific
// Character Set, and (0008,0005) says ISO_IR 192 over it, as it does there. A
// person name is an object with Alphabetic, Ideographic and Phonetic members;
// DS, IS and the binary number VRs are numbers; AT is eight hexadecimal
// digits; OB, OD, OF, OL, OV, OW and UN are base64 in InlineBinary; a
// sequence holds its items as objects. A DS or IS that is not a number is
// kept as the string it is, and infinity and NaN are the strings "inf",
// "-inf" and "NaN", as dataset writes them.
//
// A file read with SkipBulkData has values with no bytes. DICOMJSON refuses
// to write them unless JSONOptions.BulkData gives each a URI or leaves it out.
//
//	file, _ := attributes.Read(reader, attributes.ReadOptions{SkipBulkData: true})
//	doc, err := attributes.DICOMJSON(file.DataElements, attributes.JSONOptions{
//		BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
//			if v.VR == "OB" || v.VR == "OW" { // the pixels, and any large binary
//				return attributes.BulkDataDecision{
//					URI: fmt.Sprintf("bulk/%d+%d", v.Offset, v.Length)}
//			}
//			return attributes.BulkDataDecision{}
//		},
//	})
//	out, _ := json.Marshal(doc)
func DICOMJSON(elements []*DataElementValue, opts JSONOptions) (map[string]JSONElement, error) {
	return DICOMJSONWithContext(context.Background(), elements, opts)
}

// DICOMJSONWithContext is DICOMJSON, logging to the logger ctx carries
// (config.WithLogger) instead of config.Logger: what is wrong with a character
// set declaration, as TextEncodingsWithContext logs it.
func DICOMJSONWithContext(ctx context.Context, elements []*DataElementValue, opts JSONOptions) (map[string]JSONElement, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var bulk func(dicomjson.BulkValue) dicomjson.BulkDecision
	if opts.BulkData != nil {
		bulk = func(v dicomjson.BulkValue) dicomjson.BulkDecision {
			var path []BulkDataStep
			if len(v.Path) > 0 {
				path = make([]BulkDataStep, len(v.Path))
				for i, step := range v.Path {
					path[i] = BulkDataStep{Sequence: step.Sequence, Item: step.Item}
				}
			}
			d := opts.BulkData(BulkDataValue{
				Tag: v.Tag, VR: v.VR, Path: path, InFile: v.InFile,
				Offset: v.Offset, Length: v.Length, Value: v.Value,
			})
			return dicomjson.BulkDecision{URI: d.URI, Omit: d.Omit}
		}
	}
	out, err := dicomjson.Convert(jsonItem(ctx, elements, nil, opts.Warn), dicomjson.Options{Bulk: bulk})
	if err != nil {
		return nil, fmt.Errorf("attributes: %w", err)
	}
	return out, nil
}

// jsonItem gives the elements of one data set to the writer, text decoded the
// way the data set builder decodes it (fileparse.DecodeItem).
func jsonItem(ctx context.Context, elements []*DataElementValue, inherited []string, warn func(string)) *dicomjson.Item {
	item := fileparse.DecodeItem(ctx, elements, inherited, warn)
	attrs := make([]dicomjson.Attribute, 0, len(item.Elements))
	for _, d := range item.Elements {
		a := dicomjson.Attribute{Tag: d.Tag, VR: d.VR}
		switch {
		case d.Source == nil:
			a.Value = d.Value // the declaration added
		case fileparse.IsSequenceElement(d.Source):
			a.Sequence = true
			for _, child := range d.Source.Items {
				a.Items = append(a.Items, jsonItem(ctx, child.Elements, item.Encodings, warn))
			}
		case d.Source.ValueSkipped:
			a.InFile = true
			a.Offset, a.Length = fileparse.ValueExtent(d.Source)
		default:
			a.Value = d.Value
			a.Offset = d.Source.Position.ValueOffset
		}
		attrs = append(attrs, a)
	}
	return dicomjson.NewItem(attrs)
}
