// Command jpeglossless-check decodes dcmtk-encoded lossless JPEG fixtures and
// compares them with the uncompressed pixels they were made from.
//
// It exists because the lossless JPEG decoder was written from ITU-T T.81, and a
// test built on frames this project encoded would only show the two halves
// agreeing. Here dcmtk is the encoder and pydicom supplies the answer, so
// neither side of the comparison came from the code under test.
//
// The ground truth is pydicom's reading of the *uncompressed* original, dumped
// to a flat file. That matters: bare pydicom cannot decode lossless JPEG at all
// without a pylibjpeg or gdcm plugin, so asking it to decode the compressed
// fixtures would test whichever plugin happened to be installed — or fail on a
// machine with none, saying the pixels disagree when nothing had been compared.
// Reading uncompressed pixel data needs no plugin and no numpy.
//
// Usage: jpeglossless-check <dir of encoded fixtures> <uncompressed pixels>
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: jpeglossless-check <dir> <ground-truth-pixels>")
		os.Exit(2)
	}
	dir, truthPath := os.Args[1], os.Args[2]

	want, err := os.ReadFile(truthPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading the ground truth pixels: %v\n", err)
		os.Exit(1)
	}
	if len(want) == 0 {
		fmt.Fprintln(os.Stderr, "the ground truth file is empty")
		os.Exit(1)
	}

	encoded, err := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if err != nil || len(encoded) == 0 {
		fmt.Fprintln(os.Stderr, "no fixtures to check")
		os.Exit(1)
	}
	sort.Strings(encoded)

	checked, failures := 0, 0
	for _, path := range encoded {
		name := filepath.Base(path)
		if name == "orig.dcm" {
			continue
		}
		checked++

		samples, err := samplesOf(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			failures++
			continue
		}
		if len(samples) == 0 {
			fmt.Fprintf(os.Stderr, "%s: decoded no samples\n", name)
			failures++
			continue
		}
		if len(want)%len(samples) != 0 {
			fmt.Fprintf(os.Stderr, "%s: decoded %d samples, which does not divide %d bytes of original pixels\n",
				name, len(samples), len(want))
			failures++
			continue
		}

		// Compare bit patterns rather than numbers, so the comparison does not
		// have to agree with pydicom about signedness.
		got := pack(samples, len(want)/len(samples))
		if i, ok := firstDifference(got, want); !ok {
			fmt.Fprintf(os.Stderr, "%s: byte %d is 0x%02X, the original has 0x%02X\n",
				name, i, got[i], want[i])
			failures++
			continue
		}
		fmt.Printf("%s: %d samples identical to the uncompressed original\n", name, len(samples))
	}

	if checked == 0 {
		fmt.Fprintln(os.Stderr, "no encoded fixtures were found")
		os.Exit(1)
	}
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "%d of %d fixtures did not match\n", failures, checked)
		os.Exit(1)
	}
}

// samplesOf reads a file and flattens its decoded pixel data.
func samplesOf(path string) ([]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	arr, err := df.GetDataset().PixelArrayBySample()
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var out []uint64
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
			return
		}
		switch v.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			out = append(out, uint64(v.Int()))
		default:
			out = append(out, v.Uint())
		}
	}
	walk(reflect.ValueOf(arr))
	return out, nil
}

// pack serializes samples little endian at the given width, which is how
// uncompressed pixel data sits in a DICOM file.
func pack(samples []uint64, bytesPerSample int) []byte {
	out := make([]byte, len(samples)*bytesPerSample)
	for i, s := range samples {
		switch bytesPerSample {
		case 1:
			out[i] = byte(s)
		case 2:
			binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
		case 4:
			binary.LittleEndian.PutUint32(out[i*4:], uint32(s))
		default:
			binary.LittleEndian.PutUint64(out[i*8:], s)
		}
	}
	return out
}

// firstDifference returns the index of the first differing byte, and whether the
// two are equal.
func firstDifference(a, b []byte) (int, bool) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i, false
		}
	}
	if len(a) != len(b) {
		return n, false
	}
	return 0, true
}
