// Command roundtrip-check reads every DICOM file in a directory and writes it
// back out, so another toolkit can judge the result.
//
// It exists because writing was only ever checked by reading the output back
// with this library, which cannot catch a file that is self-consistent and
// non-conformant. dcmtk refused 34 of pydicom's 69 corpus files after a round
// trip through this writer — every compressed one, because encapsulated pixel
// data was written with an explicit length where the standard requires an
// undefined one.
//
// Usage: roundtrip-check <source dir> <output dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
)

func main() {
	corpus, out := os.Args[1], os.Args[2]
	files, _ := filepath.Glob(filepath.Join(corpus, "*.dcm"))
	sort.Strings(files)

	var written, failed int
	for _, path := range files {
		name := filepath.Base(path)
		if err := roundTrip(path, filepath.Join(out, name)); err != nil {
			fmt.Printf("SKIP %s: %v\n", name, err)
			failed++
			continue
		}
		written++
	}
	fmt.Printf("wrote %d, skipped %d\n", written, failed)
}

func roundTrip(in, out string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	ts := df.FileMetaInfo.TransferSyntaxUID
	if ts == "" {
		ts = "1.2.840.10008.1.2.1"
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(dst))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    df.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: df.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          ts,
	})
	for _, e := range filewriter.ElementsFromDataset(df.GetDataset()) {
		if err := w.AddDataElement(e); err != nil {
			return fmt.Errorf("add %s: %w", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
