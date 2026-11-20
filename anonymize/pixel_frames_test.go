package anonymize_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/anonymize"
	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// Redacting the pixels of an image worked on all of it at once: the whole
// image decoded, a copy of it filled, and a clone of the data set under that.
// A 4 GB image needed several times that in memory. These tests hold the
// redaction a frame at a time to the output it gave before, and to a bound
// on memory.

// writeFileOf writes ds as a file and returns its bytes. source is the file
// ds's values are left in, if any. The file meta names a fixed instance, so
// two writes of one image are the same bytes.
func writeFileOf(t *testing.T, ds *dataset.Dataset, source io.ReaderAt, littleEndian bool, syntax string) ([]byte, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.dcm")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	if source != nil {
		w.SetValueSource(source, littleEndian)
	}
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.3.4",
		TransferSyntaxUID:          syntax,
	})
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// syntaxToWrite is the transfer syntax a cleaned data set is written in: the
// one it holds, which is the file's own unless cleaning decoded the image.
func syntaxToWrite(ds *dataset.Dataset, fallback string) string {
	if syntax := ds.TransferSyntaxUID(); syntax != "" {
		return syntax
	}
	return fallback
}

// regionSets are the regions the corpus is cleaned with: two, one of which
// crosses the edge of every frame; that one alone; and one outside the image.
func regionSets(cols, rows int) map[string][]dataset.PixelRegion {
	inside := dataset.PixelRegion{X: 1, Y: 1, Width: 5, Height: 3}
	edge := dataset.PixelRegion{X: cols - 3, Y: rows - 2, Width: 8, Height: 8}
	outside := dataset.PixelRegion{X: cols + 10, Y: rows + 10, Width: 4, Height: 4}
	return map[string][]dataset.PixelRegion{
		"two regions":       {inside, edge},
		"across the edge":   {edge},
		"outside the image": {outside},
	}
}

// inRegions reports whether pixel (x, y) is in one of regions.
func inRegions(regions []dataset.PixelRegion, x, y int) bool {
	for _, r := range regions {
		if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return true
		}
	}
	return false
}

// checkFilled reports what is wrong with cleaned, the native samples of an
// image after its regions were filled, against decoded, the samples of the
// same image decoded and not filled: every sample outside the regions is as
// decoded, and all of a region's pixels are one value. It is written from the
// layout of the standard and shares nothing with the code it checks.
func checkFilled(info *dataset.PixelDataInfo, regions []dataset.PixelRegion, decoded, cleaned []byte) string {
	width := info.BitsAllocated / 8
	planes := info.SamplesPerPixel > 1 && info.PlanarConfiguration == 1
	plane := info.Rows * info.Columns * width
	frame := info.BytesPerFrame
	if len(cleaned) != len(decoded) {
		return fmt.Sprintf("%d bytes, decoded %d", len(cleaned), len(decoded))
	}
	for f := 0; f < info.NumberOfFrames; f++ {
		var filled []byte
		for y := 0; y < info.Rows; y++ {
			for x := 0; x < info.Columns; x++ {
				var pixel []byte
				for s := 0; s < info.SamplesPerPixel; s++ {
					at := f*frame + ((y*info.Columns+x)*info.SamplesPerPixel+s)*width
					if planes {
						at = f*frame + s*plane + (y*info.Columns+x)*width
					}
					got, want := cleaned[at:at+width], decoded[at:at+width]
					if !inRegions(regions, x, y) && !bytes.Equal(got, want) {
						return fmt.Sprintf("frame %d (%d,%d) sample %d is % x, decoded % x", f, x, y, s, got, want)
					}
					pixel = append(pixel, got...)
				}
				if inRegions(regions, x, y) {
					if filled == nil {
						filled = pixel
					} else if !bytes.Equal(filled, pixel) {
						return fmt.Sprintf("frame %d (%d,%d) is % x in a region filled with % x", f, x, y, pixel, filled)
					}
				}
			}
		}
	}
	return ""
}

// TestCleanPixelDataOneFrameAtATimeWritesTheFileItWroteWhole: for every image
// of pydicom's corpus that can be cleaned, the file written after cleaning is
// the same bytes however the image was held: whole, as it was until now (the
// image decoded and the regions filled in it with DecompressInPlace and
// RedactPixelRegions), or left in the file and filled, and decoded if it was
// compressed, a frame at a time as the output is written. The regions are
// two, one across the edge of every frame, that one alone, and one outside
// the image, which every way refuses. Every sample outside a region is
// checked against the image decoded alone, and the region against being one
// value. Without the corpus (GODICOM_PYDICOM_DATA) it is skipped.
func TestCleanPixelDataOneFrameAtATimeWritesTheFileItWroteWhole(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)

	equal, refused, leftInFile, filesCleaned := 0, 0, 0, map[string]bool{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(corpus, entry.Name())
		whole, err := readDataset(t, path)
		if err != nil {
			continue
		}
		if _, ok := whole.Get(tagPixelData); !ok {
			continue
		}
		info, err := whole.GetPixelDataInfo()
		if err != nil {
			continue
		}
		sets := regionSets(info.Columns, info.Rows)
		names := make([]string, 0, len(sets))
		for name := range sets {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			regions := sets[name]
			label := entry.Name() + ", " + name

			// What it was: decoded whole, then filled whole.
			want, err := readDataset(t, path)
			if err != nil {
				t.Fatal(err)
			}
			decoded := want.Clone()
			wantErr := decoded.DecompressInPlace()
			if wantErr == nil {
				wantErr = want.DecompressInPlace()
			}
			if wantErr == nil {
				wantErr = want.RedactPixelRegions(regions...)
			}

			held, err := readDataset(t, path)
			if err != nil {
				t.Fatal(err)
			}
			heldErr := a.CleanPixelData(held, regions...)
			if (heldErr == nil) != (wantErr == nil) {
				t.Errorf("%s: cleaning the image held in memory gave %v; whole it gave %v", label, heldErr, wantErr)
				continue
			}
			if wantErr != nil {
				refused++
				continue
			}
			filesCleaned[entry.Name()] = true

			// The reference gets the attributes cleaning sets, through the
			// same call: the regions are filled already, and filling them again
			// changes no sample.
			if err := a.CleanPixelData(want, regions...); err != nil {
				t.Errorf("%s: %v", label, err)
				continue
			}
			syntax := syntaxToWrite(want, "1.2.840.10008.1.2.1")
			wantFile, err := writeFileOf(t, want, nil, true, syntax)
			if err != nil {
				t.Errorf("%s: writing the image cleaned whole: %v", label, err)
				continue
			}
			heldFile, err := writeFileOf(t, held, nil, true, syntax)
			if err != nil || !bytes.Equal(heldFile, wantFile) {
				t.Errorf("%s: the file written after cleaning in memory is %d bytes and differs from the one written cleaned whole, %d (%v)",
					label, len(heldFile), len(wantFile), err)
				continue
			}

			// The pixels, against the image decoded and not filled.
			if elem, ok := want.Get(tagPixelData); ok {
				cleaned, _ := elem.GetValue().([]byte)
				plain, _ := decoded.Get(tagPixelData)
				plainBytes, _ := plain.GetValue().([]byte)
				cleanedInfo, _ := want.GetPixelDataInfo()
				if msg := checkFilled(cleanedInfo, regions, plainBytes, cleaned); msg != "" {
					t.Errorf("%s: %s", label, msg)
					continue
				}
			}

			// Left in the file, and filled as it is written.
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
				filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
			if err != nil || df.Deflated {
				_ = f.Close()
				continue
			}
			inFile, err := df.GetDatasetWithValuesInFile()
			if err != nil {
				_ = f.Close()
				continue
			}
			if err := a.CleanPixelData(inFile, regions...); err != nil {
				t.Errorf("%s: cleaning the image left in the file: %v", label, err)
				_ = f.Close()
				continue
			}
			inFileBytes, err := writeFileOf(t, inFile, f, df.IsLittleEndian, syntax)
			_ = f.Close()
			if err != nil {
				t.Errorf("%s: writing the image left in the file: %v", label, err)
				continue
			}
			if !bytes.Equal(inFileBytes, wantFile) {
				t.Errorf("%s: the file written with the pixels left in the file is %d bytes and differs from the one written cleaned whole, %d",
					label, len(inFileBytes), len(wantFile))
				continue
			}
			leftInFile++
			equal++
		}
	}
	t.Logf("%d files cleaned; %d combinations of file and regions written equal three ways; %d refused alike",
		len(filesCleaned), equal, refused)
	if equal == 0 || leftInFile == 0 {
		t.Errorf("nothing compared: %d equal, %d left in the file", equal, leftInFile)
	}
}

// bigImage writes a file of an image of frames of rows by cols 16-bit samples
// without holding it: the pixels are made as they are written. Sample (x, y)
// of frame f is sampleOf(f, x, y).
func bigImage(t *testing.T, path string, frames, rows, cols int) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")
	addImageModule(t, ds, frames, rows, cols)
	size := int64(frames) * int64(rows) * int64(cols) * 2
	pixels := &dataelem.StreamedValue{Length: size, WriteValue: func(_ io.ReaderAt, _ bool, w io.Writer) error {
		frame := make([]byte, rows*cols*2)
		for f := 0; f < frames; f++ {
			for y := 0; y < rows; y++ {
				for x := 0; x < cols; x++ {
					binary.LittleEndian.PutUint16(frame[(y*cols+x)*2:], sampleOf(f, x, y))
				}
			}
			if _, err := w.Write(frame); err != nil {
				return err
			}
		}
		return nil
	}}
	if err := ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OW, pixels)); err != nil {
		t.Fatal(err)
	}
	writeTo(t, path, ds, nil, true)
}

// sampleOf is the value of the sample at (x, y) of frame f in the images
// bigImage and bigCompressedImage make, never 0.
func sampleOf(f, x, y int) uint16 { return uint16(1 + (f*131+x*7+y*3)%4000) }

// addImageModule gives ds the attributes of a 16-bit grayscale image.
func addImageModule(t *testing.T, ds *dataset.Dataset, frames, rows, cols int) {
	t.Helper()
	for element, v := range map[uint16]int{
		0x0002: 1, 0x0008: frames, 0x0010: rows, 0x0011: cols, 0x0100: 16, 0x0101: 16, 0x0102: 15, 0x0103: 0,
	} {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		if element == 0x0008 {
			// Number of Frames is an integer string.
			if err := ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.IS, fmt.Sprint(v))); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := ds.Add(dataelem.NewDataElement(tag.New(0x0028, element), dataelem.US, b)); err != nil {
			t.Fatal(err)
		}
	}
	withValue(t, ds, tag.New(0x0028, 0x0004), dataelem.CS, "MONOCHROME2")
	withValue(t, ds, tagModality, dataelem.CS, "US")
	withValue(t, ds, tag.New(0x0010, 0x0010), dataelem.PN, "Doe^John")
}

// bigCompressedImage writes a file of an RLE Lossless image of frames of rows
// by cols 16-bit samples, every frame the same: sample (x, y) is
// sampleOf(0, 0, y).
func bigCompressedImage(t *testing.T, path string, frames, rows, cols int) {
	t.Helper()
	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.5")
	addImageModule(t, ds, frames, rows, cols)
	frame := make([]byte, rows*cols*2)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			binary.LittleEndian.PutUint16(frame[(y*cols+x)*2:], sampleOf(0, 0, y))
		}
	}
	stream, err := compress.NewRLECompressor().CompressFrame(frame, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	var value bytes.Buffer
	item := func(data []byte) {
		value.Write([]byte{0xFE, 0xFF, 0x00, 0xE0})
		_ = binary.Write(&value, binary.LittleEndian, uint32(len(data)))
		value.Write(data)
	}
	item(nil)
	for f := 0; f < frames; f++ {
		item(stream)
	}
	value.Write([]byte{0xFE, 0xFF, 0xDD, 0xE0, 0, 0, 0, 0})
	if err := ds.Add(dataelem.NewDataElement(tagPixelData, dataelem.OB, value.Bytes())); err != nil {
		t.Fatal(err)
	}
	writeTo(t, path, ds, nil, true)
}

// writeTo writes ds to path.
func writeTo(t *testing.T, path string, ds *dataset.Dataset, source io.ReaderAt, littleEndian bool) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	if source != nil {
		w.SetValueSource(source, littleEndian)
	}
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.7",
		MediaStorageSOPInstanceUID: "1.2.3.4",
		TransferSyntaxUID:          syntaxToWrite(ds, "1.2.840.10008.1.2.1"),
	})
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

// peakHeap samples the heap in use until stop is closed and returns the most
// it saw.
func peakHeap(stop <-chan struct{}) <-chan uint64 {
	out := make(chan uint64, 1)
	go func() {
		var peak uint64
		var m runtime.MemStats
		tick := time.NewTicker(2 * time.Millisecond)
		defer tick.Stop()
		for {
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
			select {
			case <-stop:
				out <- peak
				return
			case <-tick.C:
			}
		}
	}()
	return out
}

// redactWhileWriting cleans the image of the file at input, read with its
// pixels left in the file, and writes it to output, and returns the most heap
// in use while it did.
func redactWhileWriting(t *testing.T, input, output string, regions ...dataset.PixelRegion) uint64 {
	t.Helper()
	f, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := df.GetDatasetWithValuesInFile()
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	stop := make(chan struct{})
	peak := peakHeap(stop)
	err = anonymize.NewAnonymizer(anonymize.BasicProfile).CleanPixelData(ds, regions...)
	if err == nil {
		writeTo(t, output, ds, f, df.IsLittleEndian)
	}
	close(stop)
	heap := <-peak
	if err != nil {
		t.Fatalf("CleanPixelData: %v", err)
	}
	return heap
}

// checkRedactedFile reads frames of the file at path one at a time and checks
// every sample: zero in the regions, sampleOf outside them.
func checkRedactedFile(t *testing.T, path string, frames, rows, cols int, regions []dataset.PixelRegion) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
	if err != nil {
		t.Fatal(err)
	}
	if syntax := df.GetDataset().TransferSyntaxUID(); syntax != "1.2.840.10008.1.2.1" {
		t.Fatalf("the output is in %s, want Explicit VR Little Endian", syntax)
	}
	reader, err := df.FrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if reader.Frames() != frames {
		t.Fatalf("the output has %d frames, want %d", reader.Frames(), frames)
	}
	for i := 0; i < frames; i++ {
		frame, err := reader.ReadFrame(i)
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				got := binary.LittleEndian.Uint16(frame[(y*cols+x)*2:])
				want := sampleOf(i, x, y)
				if inRegions(regions, x, y) {
					want = 0
				}
				if got != want {
					t.Fatalf("frame %d (%d,%d) is %d, want %d", i, x, y, got, want)
				}
			}
		}
	}
}

// memoryRegions are the regions the memory tests fill: one inside every
// frame and one across its edge.
func memoryRegions(rows, cols int) []dataset.PixelRegion {
	return []dataset.PixelRegion{
		{X: 10, Y: 20, Width: 100, Height: 50},
		{X: cols - 10, Y: rows - 10, Width: 50, Height: 50},
	}
}

// TestRedactingNativePixelsWhileWritingHoldsAFrameNotTheImage: a native image
// of 128 frames of 512 by 512 16-bit samples, 64 MiB, left in its file, has
// its regions filled as it is copied to the output, and the heap in use never
// passes a quarter of the image. Every sample of the output is the one
// expected. Cleaning it whole holds the image and a copy of it, and passes
// the limit: this is what the test fails on without the change.
func TestRedactingNativePixelsWhileWritingHoldsAFrameNotTheImage(t *testing.T) {
	const frames, rows, cols = 128, 512, 512
	dir := t.TempDir()
	input, output := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.dcm")
	bigImage(t, input, frames, rows, cols)

	limit := int64(frames*rows*cols*2) / 4
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))
	regions := memoryRegions(rows, cols)
	heap := redactWhileWriting(t, input, output, regions...)
	t.Logf("%d MiB redacted while it was written; peak heap %.1f MiB, limit %d MiB",
		frames*rows*cols*2>>20, float64(heap)/(1<<20), limit>>20)
	if !raceEnabled && int64(heap) > limit {
		t.Errorf("the heap peaked at %.1f MiB redacting a %d MiB image", float64(heap)/(1<<20), frames*rows*cols*2>>20)
	}
	checkRedactedFile(t, output, frames, rows, cols, regions)
}

// TestRedactingCompressedPixelsWhileWritingHoldsAFrameNotTheImage: an RLE
// Lossless image of 128 frames of 512 by 512 16-bit samples, which is 64 MiB
// decoded and a few hundred kilobytes in the file, is decoded a frame at a
// time as it is written, the regions filled in each, and the heap in use
// never passes a quarter of the decoded image. The output is the image
// decoded, native, with the regions black.
func TestRedactingCompressedPixelsWhileWritingHoldsAFrameNotTheImage(t *testing.T) {
	const frames, rows, cols = 128, 512, 512
	dir := t.TempDir()
	input, output := filepath.Join(dir, "in.dcm"), filepath.Join(dir, "out.dcm")
	bigCompressedImage(t, input, frames, rows, cols)

	limit := int64(frames*rows*cols*2) / 4
	defer debug.SetMemoryLimit(debug.SetMemoryLimit(limit))
	regions := memoryRegions(rows, cols)
	heap := redactWhileWriting(t, input, output, regions...)
	t.Logf("%d MiB decoded and redacted while it was written; peak heap %.1f MiB, limit %d MiB",
		frames*rows*cols*2>>20, float64(heap)/(1<<20), limit>>20)
	if !raceEnabled && int64(heap) > limit {
		t.Errorf("the heap peaked at %.1f MiB redacting a %d MiB image", float64(heap)/(1<<20), frames*rows*cols*2>>20)
	}
	// Every frame of the source is the same: the first frame's samples.
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := df.FrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if reader.Frames() != frames {
		t.Fatalf("the output has %d frames, want %d", reader.Frames(), frames)
	}
	for i := 0; i < frames; i++ {
		frame, err := reader.ReadFrame(i)
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				want := sampleOf(0, 0, y)
				if inRegions(regions, x, y) {
					want = 0
				}
				if got := binary.LittleEndian.Uint16(frame[(y*cols+x)*2:]); got != want {
					t.Fatalf("frame %d (%d,%d) is %d, want %d", i, x, y, got, want)
				}
			}
		}
	}
}

// TestRedactingAnImageLeftInTheFileTwiceFillsBoth: regions filled after
// others are filled in the same pass of the output, for native and compressed
// pixels, as a person's regions and a detector's are, and a clone of the data
// set is not changed by regions filled in the data set it came from.
func TestRedactingAnImageLeftInTheFileTwiceFillsBoth(t *testing.T) {
	const frames, rows, cols = 3, 64, 64
	first := dataset.PixelRegion{X: 2, Y: 2, Width: 4, Height: 4}
	second := dataset.PixelRegion{X: 30, Y: 40, Width: 10, Height: 5}
	both := []dataset.PixelRegion{first, second}
	a := anonymize.NewAnonymizer(anonymize.BasicProfile)

	for name, build := range map[string]func(*testing.T, string, int, int, int){
		"native": bigImage, "compressed": bigCompressedImage,
	} {
		dir := t.TempDir()
		input := filepath.Join(dir, "in.dcm")
		build(t, input, frames, rows, cols)
		f, err := os.Open(input)
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
			filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
		if err != nil {
			t.Fatal(err)
		}
		ds, err := df.GetDatasetWithValuesInFile()
		if err != nil {
			t.Fatal(err)
		}
		if err := a.CleanPixelData(ds, first); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		afterFirst := ds.Clone()
		if err := ds.DecompressRedactingRegions(second); err != nil {
			t.Fatalf("%s: a second set of regions: %v", name, err)
		}

		written := filepath.Join(dir, "out.dcm")
		writeTo(t, written, ds, f, df.IsLittleEndian)
		if name == "native" {
			checkRedactedFile(t, written, frames, rows, cols, both)
		}
		onlyFirst := filepath.Join(dir, "first.dcm")
		writeTo(t, onlyFirst, afterFirst, f, df.IsLittleEndian)
		if name == "native" {
			checkRedactedFile(t, onlyFirst, frames, rows, cols, []dataset.PixelRegion{first})
		}
		_ = f.Close()
	}
}

// TestRedactingAnImageLeftInTheFileThatCannotBeFilledIsRefusedAtOnce: a
// region outside the image, and a layout that cannot be filled, are refused
// when asked, with the data set as it was and no pixel read, not when the
// output is being written.
func TestRedactingAnImageLeftInTheFileThatCannotBeFilledIsRefusedAtOnce(t *testing.T) {
	const frames, rows, cols = 2, 32, 32
	dir := t.TempDir()
	input := filepath.Join(dir, "in.dcm")
	bigCompressedImage(t, input, frames, rows, cols)
	f, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: math.MaxUint32})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := df.GetDatasetWithValuesInFile()
	if err != nil {
		t.Fatal(err)
	}
	err = anonymize.NewAnonymizer(anonymize.BasicProfile).CleanPixelData(ds,
		dataset.PixelRegion{X: 100, Y: 100, Width: 3, Height: 3})
	if err == nil {
		t.Fatal("a region outside the image was accepted")
	}
	if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.5" {
		t.Errorf("the transfer syntax became %q", got)
	}
	if elem, _ := ds.Get(tagPixelData); elem != nil {
		if _, still := elem.GetValue().(dataelem.ValueInFile); !still {
			t.Errorf("Pixel Data is %T, not left in the file", elem.GetValue())
		}
	}
}
