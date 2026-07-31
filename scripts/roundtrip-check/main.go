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
// With -rle the pixel data is re-encoded as RLE Lossless on the way out, which
// exercises the one compressed syntax this library writes. Another toolkit
// reading the result is the only way to know the encoder is right rather than
// merely self-consistent.
//
// Usage: roundtrip-check [-rle] <source dir> <output dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	args := os.Args[1:]
	asRLE := false
	if len(args) > 0 && args[0] == "-rle" {
		asRLE = true
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: roundtrip-check [-rle] <source dir> <output dir>")
		os.Exit(2)
	}
	corpus, out := args[0], args[1]
	files, _ := filepath.Glob(filepath.Join(corpus, "*.dcm"))
	sort.Strings(files)

	var written, failed int
	for _, path := range files {
		name := filepath.Base(path)
		if err := roundTrip(path, filepath.Join(out, name), asRLE); err != nil {
			fmt.Printf("SKIP %s: %v\n", name, err)
			failed++
			continue
		}
		written++
	}
	fmt.Printf("wrote %d, skipped %d\n", written, failed)
}

func roundTrip(in, out string, asRLE bool) (err error) {
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

	ts := df.FileMetaInfo.TransferSyntaxUID
	if ts == "" {
		ts = "1.2.840.10008.1.2.1"
	}

	ds := df.GetDataset()
	if asRLE {
		// Re-encoding goes through the same path a C-STORE takes, so what is
		// written here is what a peer negotiating RLE would receive.
		encoded, err := network.EncodeDataset(ds, network.RLELosslessUID)
		if err != nil {
			return fmt.Errorf("encode as RLE: %w", err)
		}
		recoded, err := network.DecodeDataset(encoded, network.RLELosslessUID)
		if err != nil {
			return fmt.Errorf("decode re-encoded: %w", err)
		}
		ds = recoded
		// Only relabel the file when pixel data was actually encoded. A data set
		// with none is untouched by the encoder, and declaring a compressed
		// syntax over nothing describes the file wrongly.
		if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); hasPixels {
			ts = network.RLELosslessUID
		}
	}
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(dst))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    df.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: df.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          ts,
	})
	for _, e := range filewriter.ElementsFromDataset(ds) {
		if err := w.AddDataElement(e); err != nil {
			return fmt.Errorf("add %s: %w", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
