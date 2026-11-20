package filewriter_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// writeFile writes elements as a file in syntax, copying values left in the
// file from source when it is given.
func writeFile(t *testing.T, elements []*filewriter.DataElement, syntax string, source io.ReaderAt, sourceLittleEndian bool) []byte {
	t.Helper()
	out := &growBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.3.4",
		TransferSyntaxUID:          syntax,
	})
	if source != nil {
		w.SetValueSource(source, sourceLittleEndian)
	}
	if err := w.AddDataElements(elements); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("writing as %s: %v", syntax, err)
	}
	return out.Bytes()
}

// Native syntaxes a file without encapsulated pixels can be written in.
const (
	implicitLittle = "1.2.840.10008.1.2"
	explicitLittle = "1.2.840.10008.1.2.1"
	explicitBig    = "1.2.840.10008.1.2.2"
)

// Every file of pydicom's corpus, read with every binary value and the Pixel
// Data left in the file and written back with those values copied from it,
// is byte for byte the file written from a whole read. That holds for the
// file's own transfer syntax, and for an uncompressed file in each native
// syntax as well, so a value is copied across byte orders in both directions
// (a big endian file written little endian, a little endian one written big
// endian). The lengths, the group lengths, the sequences that were of
// undefined length and the fragments of encapsulated pixel data are all in
// what is compared. A deflated file is copied from in the same way: its values
// are in the inflated data set, which the source GetDatasetWithValuesIn gives
// inflates as they are asked for, and the method that has no file to make that
// source refuses it by name.
func TestValuesCopiedFromTheFileAcrossThePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	if len(paths) == 0 {
		t.Fatalf("no .dcm files in %s", dir)
	}

	compared, copied, deflated := 0, 0, 0
	syntaxes := map[string]bool{}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(raw)))
			if err != nil {
				t.Skipf("not readable as supplied: %v", err)
			}
			skipped, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(raw)),
				filereader.ReadOptions{SkipBulkData: true})
			if err != nil {
				t.Fatalf("read whole, and not with bulk data skipped: %v", err)
			}
			// The value source is the file for a file that is not deflated, and
			// for one that is, the inflated data set its offsets count.
			referenced, source, err := skipped.GetDatasetWithValuesIn(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if whole.Deflated {
				// The method that takes no file cannot give the source, and a
				// writer given the file itself would copy the wrong bytes: it
				// is refused by name.
				if _, err := skipped.GetDatasetWithValuesInFile(); err == nil || !strings.Contains(err.Error(), "deflated") {
					t.Fatalf("a deflated file gave %v, want it refused by name", err)
				}
				deflated++
			}
			copied += countInFile(referenced)

			own := whole.FileMetaInfo.TransferSyntaxUID
			targets := []string{own}
			if !compress.IsEncapsulated(own) {
				targets = []string{implicitLittle, explicitLittle, explicitBig}
				if whole.Deflated {
					targets = append(targets, own)
				}
			}
			for _, syntax := range targets {
				if syntax == "" {
					continue
				}
				want := writeFile(t, filewriter.ElementsFromDataset(whole.GetDataset()), syntax, nil, true)
				got := writeFile(t, filewriter.ElementsFromDataset(referenced), syntax, source, skipped.IsLittleEndian)
				if !bytes.Equal(got, want) {
					t.Errorf("as %s: %d bytes copied from the file, %d written from memory; first difference at %d",
						syntax, len(got), len(want), firstDifference(got, want))
				}
				syntaxes[syntax] = true
				compared++
			}
		})
	}
	if deflated == 0 {
		t.Error("no deflated file in the corpus was copied from")
	}
	t.Logf("%d writes compared in %d syntaxes, %d values copied from the file, %d of the files deflated",
		compared, len(syntaxes), copied, deflated)
}

// countInFile counts the values a data set leaves in the file, sequences
// included.
func countInFile(ds *dataset.Dataset) int {
	n := 0
	for _, e := range filewriter.ElementsFromDataset(ds) {
		n += countElementsInFile(e)
	}
	return n
}

func countElementsInFile(e *filewriter.DataElement) int {
	n := 0
	if e.InFile != nil {
		n++
	}
	for _, item := range e.Items {
		for _, nested := range item.Elements {
			n += countElementsInFile(nested)
		}
	}
	return n
}

func firstDifference(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// A big endian image of 32-bit samples, odd in length, inside and outside a
// sequence, comes back from the file as it does from memory, in either byte
// order. The samples are reversed at four bytes, not at the two OW names
// (#124), and the odd value gets its padding byte.
func TestValuesCopiedFromTheFileKeepSampleWidthAndPadding(t *testing.T) {
	samples := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	icon := dataset.NewDataset()
	_ = icon.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.US, []byte{32, 0}))
	_ = icon.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, samples))
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.7")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x0100), dataelem.US, []byte{32, 0}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0029, 0x0010), dataelem.LO, []byte("ACME")))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x0029, 0x1001), dataelem.OB, []byte{1, 2, 3}))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, samples))
	seq := sequence.New()
	_ = seq.Append(icon)
	_ = ds.AddSequence(tag.New(0x0088, 0x0200), seq)

	for _, source := range []string{explicitBig, explicitLittle} {
		file := writeFile(t, filewriter.ElementsFromDataset(ds), source, nil, true)
		whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
		if err != nil {
			t.Fatal(err)
		}
		skipped, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(file)),
			filereader.ReadOptions{SkipBulkData: true})
		if err != nil {
			t.Fatal(err)
		}
		referenced, err := skipped.GetDatasetWithValuesInFile()
		if err != nil {
			t.Fatal(err)
		}
		if n := countInFile(referenced); n != 3 {
			t.Fatalf("from %s: %d values left in the file, want both Pixel Data and the private OB", source, n)
		}
		for _, target := range []string{explicitBig, explicitLittle, implicitLittle} {
			want := writeFile(t, filewriter.ElementsFromDataset(whole.GetDataset()), target, nil, true)
			got := writeFile(t, filewriter.ElementsFromDataset(referenced), target, bytes.NewReader(file), skipped.IsLittleEndian)
			if !bytes.Equal(got, want) {
				t.Errorf("%s written as %s: first difference at %d of %d", source, target, firstDifference(got, want), len(want))
			}
		}
	}
}

// A source whose binary value is odd in length, which PS3.5 7.1.1 does not
// allow and writers produce, is written with the padding byte a value in
// memory gets, so the elements after it stay where a reader looks for them.
func TestAnOddValueCopiedFromTheFileIsPadded(t *testing.T) {
	file := writeFile(t, nil, explicitLittle, nil, true)
	file = append(file,
		0x29, 0x00, 0x10, 0x00, 'L', 'O', 4, 0, 'A', 'C', 'M', 'E',
		0x29, 0x00, 0x01, 0x10, 'O', 'B', 0, 0, 3, 0, 0, 0, 1, 2, 3,
		0x29, 0x00, 0x02, 0x10, 'L', 'O', 2, 0, 'O', 'K')

	whole, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(bytes.NewReader(file)),
		filereader.ReadOptions{SkipBulkData: true})
	if err != nil {
		t.Fatal(err)
	}
	referenced, err := skipped.GetDatasetWithValuesInFile()
	if err != nil {
		t.Fatal(err)
	}
	if n := countInFile(referenced); n != 1 {
		t.Fatalf("%d values left in the file, want the OB", n)
	}
	want := writeFile(t, filewriter.ElementsFromDataset(whole.GetDataset()), explicitLittle, nil, true)
	got := writeFile(t, filewriter.ElementsFromDataset(referenced), explicitLittle, bytes.NewReader(file), true)
	if !bytes.Equal(got, want) {
		t.Errorf("first difference at %d of %d", firstDifference(got, want), len(want))
	}
}

// A value left in the file is never written as nothing. With no source to
// copy it from the write fails, and the network encoder's rendering refuses
// it rather than sending an empty image.
func TestAValueInTheFileWithoutASourceIsRefused(t *testing.T) {
	ref := dataelem.ValueInFile{Offset: 1000, Length: 8}
	ds := dataset.NewDataset()
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, ref))

	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&growBuffer{}))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{TransferSyntaxUID: explicitLittle})
	_ = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	err := w.Write()
	if err == nil || !strings.Contains(err.Error(), "no value source") {
		t.Errorf("writing without a source gave %v", err)
	}

	if _, err := dataelem.ValueBytes(dataelem.OW, ref); err == nil || !strings.Contains(err.Error(), "not in memory") {
		t.Errorf("ValueBytes gave %v", err)
	}

	// A source shorter than the value: the read fails, and so does the write.
	w = filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&growBuffer{}))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{TransferSyntaxUID: explicitLittle})
	w.SetValueSource(bytes.NewReader(make([]byte, 1004)), true)
	_ = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err := w.Write(); err == nil || !strings.Contains(err.Error(), "copying its value") {
		t.Errorf("a short source gave %v", err)
	}
}
