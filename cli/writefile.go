package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// The commands that change a file and write it back (modify, compress,
// decompress) read, describe and write it through what is here, so each
// treats the file meta and a half-written output the same way.

// sopInstanceUIDTag is (0008,0018), and sopClassUIDTag (0008,0016).
var (
	sopInstanceUIDTag = tag.New(0x0008, 0x0018)
	sopClassUIDTag    = tag.New(0x0008, 0x0016)
)

// readInstance reads a whole DICOM file: its meta and its data set, pixel data
// included.
func readInstance(path string) (*filereader.DICOMFile, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return filereader.ReadDICOMFile(filebase.NewFileReader(in))
}

// readForRewrite reads the file a command is about to change, and refuses
// one that is not DICOM: the reader salvages what it can and reports no
// error for a file with nothing in it to salvage.
func readForRewrite(command, input string) (*filereader.DICOMFile, *dataset.Dataset, error) {
	if _, err := os.Stat(input); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", command, err)
	}
	df, err := readInstance(input)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %s: %w", command, input, err)
	}
	ds := df.GetDataset()
	if ds.Length() == 0 {
		return nil, nil, fmt.Errorf("%s: %s does not look like a DICOM file: no data elements could be read", command, input)
	}
	return df, ds, nil
}

// instanceMeta is the file meta for a data set read from df and changed.
//
// The transfer syntax is the file's own, so pixel data is written back as it
// was read: encapsulated fragments untouched, native pixels in the byte order
// the syntax says. A command that changes the syntax sets it after. The SOP
// class and instance are taken from the data set, since PS3.10 7.1 has the
// meta name the instance the file holds, and an edit or a new UID may have
// changed it.
//
// What else the source meta said about where the file came from is carried
// across. The implementation that wrote the file is this one, so the writer
// names itself in (0002,0012) and (0002,0013).
func instanceMeta(df *filereader.DICOMFile, ds *dataset.Dataset) *filewriter.FileMetaInfo {
	meta := &filewriter.FileMetaInfo{}
	if read := df.FileMetaInfo; read != nil {
		meta.TransferSyntaxUID = read.TransferSyntaxUID
		meta.MediaStorageSOPClassUID = read.MediaStorageSOPClassUID
		meta.MediaStorageSOPInstanceUID = read.MediaStorageSOPInstanceUID
		meta.SourceApplicationEntityTitle = read.SourceApplicationEntityTitle
		meta.SendingApplicationEntityTitle = read.SendingApplicationEntityTitle
		meta.ReceivingApplicationEntityTitle = read.ReceivingApplicationEntityTitle
	}
	if meta.TransferSyntaxUID == "" {
		// A file with no meta, or a meta without (0002,0010), is written in
		// the encoding the reader found its data set in.
		switch {
		case !df.IsLittleEndian:
			meta.TransferSyntaxUID = uid.BigEndianTransferSyntax().String()
		case df.ExplicitVR:
			meta.TransferSyntaxUID = uid.ExplicitVRLittleEndian().String()
		default:
			meta.TransferSyntaxUID = uid.ImplicitVRLittleEndian().String()
		}
	}
	if v := datasetText(ds, sopClassUIDTag); v != "" {
		meta.MediaStorageSOPClassUID = v
	}
	if v := datasetText(ds, sopInstanceUIDTag); v != "" {
		meta.MediaStorageSOPInstanceUID = v
	}
	return meta
}

// newInstanceUID gives the data set a new SOP Instance UID and returns it.
func newInstanceUID(ds *dataset.Dataset) (string, error) {
	u := uid.Generate()
	value := []byte(u)
	if len(value)%2 == 1 {
		value = append(value, 0) // a UID is padded with a NUL (PS3.5 9.1)
	}
	return u, ds.Add(dataelem.NewDataElement(sopInstanceUIDTag, dataelem.UI, value))
}

// writeDICOMInstance writes ds to path as a Part 10 file with the meta given, and
// removes what it wrote if any step fails.
//
// A partial file at the output path passes for a complete one to whatever
// reads it next.
func writeDICOMInstance(path string, meta *filewriter.FileMetaInfo, ds *dataset.Dataset) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(meta)
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// replaceFile writes ds over path through a file beside it, renamed into place
// once it is complete, so the original is never left half overwritten.
func replaceFile(path string, meta *filewriter.FileMetaInfo, ds *dataset.Dataset) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		_ = os.Chmod(name, info.Mode().Perm())
	}
	if err := writeDICOMInstance(name, meta, ds); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// outputPaths reads the <input> <output> or -in-place <file> a command that
// rewrites a file takes, and refuses an output that is the input without
// -in-place.
func outputPaths(command string, positional []string, inPlace bool) (input, output string, err error) {
	switch {
	case inPlace && len(positional) == 1:
		return positional[0], positional[0], nil
	case inPlace:
		return "", "", fmt.Errorf("%s: -in-place takes one file, the one it changes", command)
	case len(positional) == 2:
		if sameFile(positional[0], positional[1]) {
			return "", "", fmt.Errorf("%s: the output is the input; give -in-place to change the input itself", command)
		}
		return positional[0], positional[1], nil
	}
	return "", "", fmt.Errorf("%s: expected an input and an output, or -in-place and one file\nRun 'go-dicom %s -h' for usage", command, command)
}

// writeOutput writes to output, or over it through replaceFile with -in-place.
func writeOutput(output string, inPlace bool, meta *filewriter.FileMetaInfo, ds *dataset.Dataset) error {
	if inPlace {
		return replaceFile(output, meta, ds)
	}
	return writeDICOMInstance(output, meta, ds)
}
