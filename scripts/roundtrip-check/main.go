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
// With -rle, -jpeg2000, -jpeg-lossless or -jpeg-lossless-sv1 the pixel data is
// re-encoded on the way out, as RLE Lossless, JPEG 2000 Lossless or lossless
// JPEG (.57 or .70), which exercises an encoder this library has. With
// -jpegls-near N it is coded as JPEG-LS Near-Lossless with an allowed error of
// N, and with -jpeg-extended Q as JPEG Extended at quality Q. Another toolkit
// reading the result is the only way to know the encoder is right rather than
// merely self-consistent.
//
// Usage: roundtrip-check [-rle|-jpeg2000|-jpeg-lossless|-jpeg-lossless-sv1|-jpegls-near N|-jpeg-extended Q]
// <source dir> <output dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

func main() {
	args := os.Args[1:]
	target := ""
	if len(args) > 0 {
		switch args[0] {
		case "-rle":
			target, args = network.RLELosslessUID, args[1:]
		case "-jpeg2000":
			target, args = network.JPEG2000LosslessUID, args[1:]
		case "-jpeg-lossless":
			target, args = network.JPEGLosslessProcess14UID, args[1:]
		case "-jpeg-lossless-sv1":
			target, args = network.JPEGLosslessSV1UID, args[1:]
		case "-jpegls-near":
			if len(args) > 1 {
				near, _ = strconv.Atoi(args[1])
				target, args = network.JPEGLSNearLosslessUID, args[2:]
			}
		case "-jpeg-extended":
			if len(args) > 1 {
				quality, _ = strconv.Atoi(args[1])
				target, args = network.JPEGExtendedUID, args[2:]
			}
		}
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: roundtrip-check [-rle|-jpeg2000|-jpeg-lossless|-jpeg-lossless-sv1|-jpegls-near N|-jpeg-extended Q] "+
			"<source dir> <output dir>")
		os.Exit(2)
	}
	corpus, out := args[0], args[1]
	files, _ := filepath.Glob(filepath.Join(corpus, "*.dcm"))
	sort.Strings(files)

	var written, failed int
	for _, path := range files {
		name := filepath.Base(path)
		if err := roundTrip(path, filepath.Join(out, name), target); err != nil {
			fmt.Printf("SKIP %s: %v\n", name, err)
			failed++
			continue
		}
		written++
	}
	fmt.Printf("wrote %d, skipped %d\n", written, failed)
}

// near is the allowed error for -jpegls-near, and quality the JPEG quality
// for -jpeg-extended.
var (
	near    int
	quality int
)

func roundTrip(in, out, target string) (err error) {
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
	if target == network.JPEGLSNearLosslessUID {
		// The data set's own method, which is what a send uses with
		// NetworkConfig.JPEGLSNear; EncodeDataset allows no error.
		if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); hasPixels {
			if err := ds.CompressJPEGLSNearLosslessInPlace(near); err != nil {
				return fmt.Errorf("encode as %s: %w", target, err)
			}
			ts = target
		}
	} else if target == network.JPEGExtendedUID {
		// The data set's own method, which is what a send uses with
		// NetworkConfig.JPEGQuality; EncodeDataset accepts no loss.
		if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); hasPixels {
			if err := ds.CompressJPEGExtendedInPlace(quality); err != nil {
				return fmt.Errorf("encode as %s: %w", target, err)
			}
			ts = target
		}
	} else if target != "" {
		// Re-encoding goes through the same path a C-STORE takes, so what is
		// written here is what a peer negotiating the syntax would receive.
		encoded, err := network.EncodeDataset(ds, target)
		if err != nil {
			return fmt.Errorf("encode as %s: %w", target, err)
		}
		recoded, err := network.DecodeDataset(encoded, target)
		if err != nil {
			return fmt.Errorf("decode re-encoded: %w", err)
		}
		ds = recoded
		// Only relabel the file when pixel data was actually encoded. A data set
		// with none is untouched by the encoder, and declaring a compressed
		// syntax over nothing describes the file wrongly.
		if _, hasPixels := ds.Get(tag.New(0x7FE0, 0x0010)); hasPixels {
			ts = target
		}
	}
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(dst))
	// The source's implementation is kept, so the meta is as long as it was. A
	// DICOMDIR's record offsets count from the start of the file, and a meta
	// of another length moves every record away from them.
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    df.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: df.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          ts,
		ImplementationClassUID:     df.FileMetaInfo.ImplementationClassUID,
		ImplementationVersionName:  df.FileMetaInfo.ImplementationVersionName,
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
