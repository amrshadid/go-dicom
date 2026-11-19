// Package attributes reads the attributes of a DICOM file, the File Meta
// group and the whole data set as the file holds them, without linking what
// builds a data set or decodes pixels.
//
// How it differs from its neighbors: filereader reads the same files with the
// same parser and adds GetDataset, frames and waveforms, which link the
// codecs; element (the top-level package) reads nothing, it converts values
// already in memory between their bytes and Go types. Use attributes to index
// or inspect files, filereader to build a data set or reach the pixels.
//
// filereader reads the same way and returns a DICOMFile, whose GetDataset,
// FrameRanges and GetWaveforms need dataset, encaps, compress, pixels and
// waveforms. A program that only reads attributes, an archive indexing what
// arrives for instance, links all of them through filereader whether it calls
// them or not. This package is
// the parser alone. Its dependencies are charset, config, errors, filebase,
// tag and uid, and a test in this package fails if any of the codec packages
// joins them.
//
// What it returns is what filereader returns, by the same types: File has the
// fields of filereader.DICOMFile, and DataElementValue, ReadOptions and the
// rest are filereader's under aliases, so a value from one package is a value
// of the other. Text is in the file's character set; TextEncodings and
// DecodeText give it as UTF-8, by the rules GetDataset applies, and
// TextEncodingsWithContext and DecodeTextWithContext do the same logging to
// the read's logger.
//
//	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)) // this read's own
//	f, _ := os.Open(path)
//	file, err := attributes.Read(filebase.NewFileReader(f), attributes.ReadOptions{
//		SkipBulkData:   true,    // leave Pixel Data in the file
//		MaxValueLength: 1 << 20, // refuse a longer value
//		Logger:         logger,
//	})
//	ctx := config.WithLogger(context.Background(), logger) // decoding logs there too
//	encodings := attributes.TextEncodingsWithContext(ctx, file.DataElements, nil)
//	for _, e := range file.DataElements {
//		fmt.Println(e.Tag, e.VR, attributes.DecodeTextWithContext(ctx, e.VR, e.Value, encodings))
//	}
package attributes

import (
	"context"

	"github.com/amrshadid/go-dicom/filebase"
	fileparse "github.com/amrshadid/go-dicom/internal/filereader"
)

// File is a parsed DICOM file: preamble, meta header and data set, as the
// file holds them. It has the fields of filereader.DICOMFile.
type File = fileparse.File

// ReadOptions choose how Read reads a file; they are filereader.ReadOptions.
type ReadOptions = fileparse.ReadOptions

// FileMetaInfo is the File Meta group in typed form.
type FileMetaInfo = fileparse.FileMetaInfo

// DataElementValue is one data element as it was read: its tag, VR, length
// and value bytes, the items of a sequence, and where it is.
type DataElementValue = fileparse.DataElementValue

// ElementPosition is where a data element sits in the bytes it was read from.
type ElementPosition = fileparse.ElementPosition

// FragmentPosition is one item of encapsulated pixel data.
type FragmentPosition = fileparse.FragmentPosition

// SequenceItemValue is a single item of a sequence, holding its elements.
type SequenceItemValue = fileparse.SequenceItemValue

// ValueTooLongError is a value refused because it states more bytes than
// ReadOptions.MaxValueLength.
type ValueTooLongError = fileparse.ValueTooLongError

// ErrValueTooLong is what a read refused by ReadOptions.MaxValueLength
// matches with errors.Is. It is filereader.ErrValueTooLong.
var ErrValueTooLong = fileparse.ErrValueTooLong

// UndefinedLength is the Value Length 0xFFFFFFFF, which says an element is
// closed by a delimiter rather than measured (PS3.5 7.1).
const UndefinedLength uint32 = fileparse.UndefinedLength

// Read reads a whole file as options say, exactly as
// filereader.ReadDICOMFileWithOptions does, and returns what the file holds.
func Read(reader filebase.Reader, options ReadOptions) (*File, error) {
	return fileparse.Read(context.Background(), reader, options)
}

// TextEncodings returns the character encodings in force for the text of a
// data set: those its Specific Character Set (0008,0005) declares, or, when
// it has none, inherited, which is what the data set it is an item of uses
// (PS3.3 C.12.1.1.2). Pass nil for the top of a file. The result is what
// DecodeText takes.
func TextEncodings(elements []*DataElementValue, inherited []string) []string {
	return fileparse.TextEncodings(elements, inherited)
}

// DecodeText returns a value of the given VR as UTF-8 text, decoded under
// encodings as TextEncodings returns them. Only the VRs a character set
// applies to are decoded with it; a UI or a DA is in the default repertoire
// whatever the data set declares. A value that does not decode is returned
// as it is.
func DecodeText(vr string, value []byte, encodings []string) string {
	return fileparse.DecodeText(vr, value, encodings)
}

// TextEncodingsWithContext is TextEncodings, logging to the logger ctx
// carries (config.WithLogger) instead of config.Logger.
//
// What is logged is what is wrong with a declaration: a misspelled term,
// read as the one it plainly means, or one this library does not know, read
// as the default repertoire. TextEncodings logs it to config.Logger whatever
// logger the read was given (ReadOptions.Logger), so a program keeping each
// read's lines apart found the declaration's lines on the global logger.
// Give it the read's logger the way a read carries one:
//
//	ctx := config.WithLogger(ctx, logger) // the logger in ReadOptions.Logger
//	encodings := attributes.TextEncodingsWithContext(ctx, file.DataElements, nil)
func TextEncodingsWithContext(ctx context.Context, elements []*DataElementValue, inherited []string) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	encodings, _ := fileparse.EncodingsFor(ctx, elements, inherited)
	return encodings
}

// DecodeTextWithContext is DecodeText, logging to the logger ctx carries
// (config.WithLogger) instead of config.Logger: a value that does not decode
// under its encodings is logged when the validation mode is WARN.
func DecodeTextWithContext(ctx context.Context, vr string, value []byte, encodings []string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	return string(fileparse.DecodeTextValue(ctx, vr, value, encodings))
}
