package cli

import (
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
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

// writeDICOMInstance writes ds to path as a Part 10 file with the meta given, and
// removes what it wrote if any step fails.
//
// The commands that write a DICOM file share this, so a half-written file is
// treated the same way by all of them: a partial file at the output path
// passes for a complete one to whatever reads it next.
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
