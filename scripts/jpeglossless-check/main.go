// Command jpeglossless-check decodes dcmtk-encoded lossless JPEG fixtures and
// compares them with the uncompressed original they were made from.
//
// It exists because the lossless JPEG decoder was written from ITU-T T.81, and a
// test built on frames this project encoded would only show the two halves
// agreeing. Here dcmtk is the encoder and the uncompressed file is the answer,
// so neither side of the comparison came from the code under test.
//
// Usage: jpeglossless-check <directory containing orig.dcm and the encoded files>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: jpeglossless-check <dir>")
		os.Exit(2)
	}
	dir := os.Args[1]

	want, err := samplesOf(filepath.Join(dir, "orig.dcm"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading the uncompressed original: %v\n", err)
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

		got, err := samplesOf(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			failures++
			continue
		}
		if len(got) != len(want) {
			fmt.Fprintf(os.Stderr, "%s: decoded %d samples, the original has %d\n",
				name, len(got), len(want))
			failures++
			continue
		}
		if i, ok := firstDifference(got, want); !ok {
			fmt.Fprintf(os.Stderr, "%s: sample %d is %d, the original has %d\n",
				name, i, got[i], want[i])
			failures++
			continue
		}
		fmt.Printf("%s: %d samples identical to the original\n", name, len(got))
	}

	if checked == 0 {
		fmt.Fprintln(os.Stderr, "no encoded fixtures were found next to orig.dcm")
		os.Exit(1)
	}
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "%d of %d fixtures did not match\n", failures, checked)
		os.Exit(1)
	}
}

// samplesOf reads a file and flattens its pixel data to signed values.
func samplesOf(path string) ([]int64, error) {
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

	var out []int64
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
			out = append(out, v.Int())
		default:
			out = append(out, int64(v.Uint()))
		}
	}
	walk(reflect.ValueOf(arr))
	return out, nil
}

// firstDifference returns the index of the first differing sample, and whether
// the two are equal.
func firstDifference(a, b []int64) (int, bool) {
	for i := range a {
		if a[i] != b[i] {
			return i, false
		}
	}
	return 0, true
}
