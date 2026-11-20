package filewriter_test

import (
	"bytes"
	"compress/flate"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

const deflatedSyntax = "1.2.840.10008.1.2.1.99"

// patterned returns n bytes that compress well and are not all alike, so that a
// copy from the wrong place is a different value.
func patterned(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%251) + seed + byte(i>>20)
	}
	return b
}

// deflatedImage writes a deflated file with values of the sizes given before,
// between and after small elements: private binary values (the first inside
// a defined-length run of text), then Pixel Data.
func deflatedImage(t *testing.T, sizes ...int) []byte {
	t.Helper()
	elements := []*filewriter.DataElement{
		{Tag: tag.New(0x0008, 0x0016), VR: "UI", Value: []byte("1.2.840.10008.5.1.4.1.1.7\x00"), Length: 26},
		{Tag: tag.New(0x0010, 0x0010), VR: "PN", Value: []byte("Doe^John"), Length: 8},
		{Tag: tag.New(0x0028, 0x0100), VR: "US", Value: []byte{8, 0}, Length: 2},
		{Tag: tag.New(0x0029, 0x0010), VR: "LO", Value: []byte("ACME"), Length: 4},
	}
	for i, size := range sizes[:len(sizes)-1] {
		value := patterned(size, byte(i))
		elements = append(elements,
			&filewriter.DataElement{Tag: tag.New(0x0029, uint16(0x1001+i*2)), VR: "OB", Value: value, Length: uint32(len(value))},
			&filewriter.DataElement{Tag: tag.New(0x0029, uint16(0x1002+i*2)), VR: "LO", Value: []byte("after"), Length: 5})
	}
	pixels := patterned(sizes[len(sizes)-1], 99)
	elements = append(elements,
		&filewriter.DataElement{Tag: tag.New(0x7FE0, 0x0010), VR: "OB", Value: pixels, Length: uint32(len(pixels))})
	return writeFile(t, elements, deflatedSyntax, nil, true)
}

// rewriteInPlace reads file with its values left in it, and writes it back
// with them copied from the source GetDatasetWithValuesIn gives, in syntax.
func rewriteInPlace(t *testing.T, file []byte, syntax string) []byte {
	t.Helper()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(file)),
		filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		t.Fatal(err)
	}
	ds, source, err := df.GetDatasetWithValuesIn(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, filewriter.ElementsFromDataset(ds), syntax, source, df.IsLittleEndian)
}

// rewriteWhole reads file whole and writes it back in syntax.
func rewriteWhole(t *testing.T, file []byte, syntax string) []byte {
	t.Helper()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
	if err != nil {
		t.Fatal(err)
	}
	return writeFile(t, filewriter.ElementsFromDataset(df.GetDataset()), syntax, nil, true)
}

// A deflated file with values of several sizes, before and after small
// elements, is rewritten in place to the bytes a whole read writes, deflated
// again and in a native syntax. The writer asks for the values in increasing
// order, so the inflating goes through the data set once, with the gaps between
// the values inflated and dropped.
func TestDeflatedFileIsRewrittenInPlaceAsAWholeReadWritesIt(t *testing.T) {
	for _, sizes := range [][]int{{1000}, {70000, 3, 1 << 20}, {300 << 10, 300 << 10, 300 << 10, 1<<20 + 1}} {
		file := deflatedImage(t, sizes...)
		for _, syntax := range []string{deflatedSyntax, explicitLittle, implicitLittle} {
			want := rewriteWhole(t, file, syntax)
			got := rewriteInPlace(t, file, syntax)
			if !bytes.Equal(got, want) {
				t.Errorf("sizes %v as %s: %d bytes in place, %d from a whole read; first difference at %d",
					sizes, syntax, len(got), len(want), firstDifference(got, want))
			}
		}
	}
}

// Reading a deflated file with its values left in it finds every element where
// a whole read does: the same tags, VRs, lengths and positions, which count the
// inflated bytes, and the same warnings; only the bulk values are not read.
func TestDeflatedFileReadWithValuesLeftHasTheWholeReadsPositions(t *testing.T) {
	file := deflatedImage(t, 100000, 5, 400000)
	whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(file)),
		filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		t.Fatal(err)
	}
	if !skipped.Deflated || len(skipped.DataElements) != len(whole.DataElements) {
		t.Fatalf("deflated %v, %d elements, want %d", skipped.Deflated, len(skipped.DataElements), len(whole.DataElements))
	}
	left := 0
	for i, e := range skipped.DataElements {
		w := whole.DataElements[i]
		if e.Tag != w.Tag || e.VR != w.VR || e.Length != w.Length || !reflect.DeepEqual(e.Position, w.Position) {
			t.Errorf("element %d: %v %s %d %+v; whole read %v %s %d %+v",
				i, e.Tag, e.VR, e.Length, e.Position, w.Tag, w.VR, w.Length, w.Position)
		}
		if e.ValueSkipped {
			left++
		} else if !bytes.Equal(e.Value, w.Value) {
			t.Errorf("element %d %v: a value that is read differs", i, e.Tag)
		}
	}
	if left != 3 {
		t.Errorf("%d values left in the file, want the two private OB and Pixel Data", left)
	}
	if !reflect.DeepEqual(skipped.Warnings, whole.Warnings) {
		t.Errorf("warnings differ: %q and %q", skipped.Warnings, whole.Warnings)
	}
}

// A value read from a deflated file whose other values are left in it is read
// whole, whatever its length: the stream gives a value in one read as a file
// does, where an inflater gives a block at a time, and the reader takes a
// short read for a truncated file. The file here has private values of 100 KB
// and 70 KB, read because the threshold is above them, and Pixel Data, which is
// always left.
func TestDeflatedFileReadsLongValuesItDoesNotLeaveInTheFile(t *testing.T) {
	file := deflatedImage(t, 100000, 70000, 400000)
	whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
	if err != nil {
		t.Fatal(err)
	}
	read, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(file)),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(read.DataElements) != len(whole.DataElements) || len(read.Warnings) != len(whole.Warnings) {
		t.Fatalf("%d elements and %d warnings, want %d and %d: %q",
			len(read.DataElements), len(read.Warnings), len(whole.DataElements), len(whole.Warnings), read.Warnings)
	}
	long := 0
	for i, e := range read.DataElements {
		if e.Tag.IsPixelData() {
			if !e.ValueSkipped {
				t.Errorf("Pixel Data was read")
			}
			continue
		}
		if e.ValueSkipped || !bytes.Equal(e.Value, whole.DataElements[i].Value) {
			t.Errorf("element %d %v: skipped %v, %d bytes read, whole read has %d", i, e.Tag, e.ValueSkipped,
				len(e.Value), len(whole.DataElements[i].Value))
		}
		if len(e.Value) > 32<<10 {
			long++
		}
	}
	if long != 2 {
		t.Errorf("%d long values, want 2", long)
	}
}

// Rewriting a deflated file with a 24 MB value in place allocates a few
// megabytes in all, where reading it whole and writing it allocates the value
// several times over: the inflated data set is a window of a stream, and the
// value goes from the stream into the compressor a megabyte at a time. The
// output is the same bytes either way.
func TestDeflatedFileRewrittenInPlaceHoldsNoWholeValue(t *testing.T) {
	file := deflatedImage(t, 24<<20, 1<<20)

	measure := func(f func() []byte) (out []byte, allocated uint64) {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		out = f()
		runtime.ReadMemStats(&after)
		return out, after.TotalAlloc - before.TotalAlloc
	}
	inPlace, small := measure(func() []byte { return rewriteInPlace(t, file, deflatedSyntax) })
	whole, large := measure(func() []byte { return rewriteWhole(t, file, deflatedSyntax) })

	if !bytes.Equal(inPlace, whole) {
		t.Errorf("in place %d bytes, whole %d; first difference at %d", len(inPlace), len(whole), firstDifference(inPlace, whole))
	}
	t.Logf("allocated in place: %d MB; whole: %d MB", small>>20, large>>20)
	if small > 12<<20 {
		t.Errorf("rewriting in place allocated %d MB for a 24 MB value, which is holding it", small>>20)
	}
	if large < 24<<20 {
		t.Errorf("the whole read allocated only %d MB, so the comparison measures nothing", large>>20)
	}
}

// A deflated file that expands past what its size allows is refused when it is
// read with its values left in it, as it is when read whole: the inflated
// bytes are measured before they are read.
func TestDeflatedBombIsRefusedWhenValuesAreLeftInTheFile(t *testing.T) {
	var z bytes.Buffer
	w, _ := flate.NewWriter(&z, flate.BestCompression)
	chunk := make([]byte, 1<<20)
	for written := int64(0); written < filereader.MaxInflatedDatasetSize+(1<<20); written += int64(len(chunk)) {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	raw, err := filewriter.EncodeFileHeader(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.3.4",
		TransferSyntaxUID:          deflatedSyntax,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, z.Bytes()...)

	for _, options := range []filereader.ReadOptions{{}, {SkipBulkData: true}} {
		_, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(raw)), options)
		if err == nil || !strings.Contains(err.Error(), "expands beyond") {
			t.Errorf("SkipBulkData %v: err = %v, want the expansion refused", options.SkipBulkData, err)
		}
	}
}

// Every deflated file of pydicom's corpus, and deflated files made from its
// images, are rewritten in place to the bytes a whole read writes.
func TestDeflatedFilesMadeFromTheCorpusAreRewrittenInPlace(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	compared := 0
	for _, name := range []string{"image_dfl.dcm", "CT_small.dcm", "MR_small.dcm", "rtdose.dcm", "examples_rgb_color.dcm",
		"SC_rgb_small_odd.dcm", "MR_small_bigendian.dcm", "waveform_ecg.dcm", "liver_1frame.dcm", "rtstruct.dcm"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
		if err != nil {
			t.Fatal(err)
		}
		file := raw
		if !df.Deflated {
			// A deflated file with the same data set.
			file = writeFile(t, filewriter.ElementsFromDataset(df.GetDataset()), deflatedSyntax, nil, true)
		}
		for _, syntax := range []string{deflatedSyntax, explicitLittle} {
			if want, got := rewriteWhole(t, file, syntax), rewriteInPlace(t, file, syntax); !bytes.Equal(got, want) {
				t.Errorf("%s as %s: %d bytes in place, %d from a whole read; first difference at %d",
					name, syntax, len(got), len(want), firstDifference(got, want))
			}
			compared++
		}
	}
	t.Logf("%d rewrites compared", compared)
}
