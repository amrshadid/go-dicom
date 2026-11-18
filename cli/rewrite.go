package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filewriter"
)

// What a command that rewrites a DICOM file in place or to a new one shares
// (modify, compress, decompress): its paths, and a write over the input
// that never leaves it half written.

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
