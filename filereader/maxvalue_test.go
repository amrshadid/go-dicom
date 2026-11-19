package filereader

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/tag"
)

// A value was allocated at the length the file stated whenever the size of
// the source could not be measured, before a byte of it arrived: a 200-byte
// stream stating 1 GiB cost 1 GiB. The File Meta group was never checked at
// all, from any source (#458).

// stream cannot seek at all, as a socket or an upload body cannot: the size
// of what it holds is unknown until it ends.
type stream struct{ r io.Reader }

func (s stream) Read(p []byte) (int, error)     { return s.r.Read(p) }
func (s stream) Seek(int64, int) (int64, error) { return 0, errors.New("the source cannot seek") }

func fromStream(data []byte) filebase.Reader {
	return filebase.NewFileReader(stream{bytes.NewReader(data)})
}

func fromFile(data []byte) filebase.Reader {
	return filebase.NewFileReader(bytes.NewReader(data))
}

// header is an explicit VR little endian element header stating length.
func header(group, element uint16, vr string, length uint32) []byte {
	out := binary.LittleEndian.AppendUint16(nil, group)
	out = binary.LittleEndian.AppendUint16(out, element)
	if vr == "" {
		return binary.LittleEndian.AppendUint32(out, length)
	}
	out = append(out, vr...)
	if isShortVR(vr) {
		return binary.LittleEndian.AppendUint16(out, uint16(length))
	}
	return binary.LittleEndian.AppendUint32(append(out, 0, 0), length)
}

// oneGiB is what each hostile header states, followed by a few bytes.
const oneGiB = 1 << 30

// hostile is a stream whose header states far more than it holds, at each
// place the reader takes a length from the file.
type hostile struct {
	name string
	data []byte
	// value is set where the length is a value's, which MaxValueLength
	// refuses; a sequence's or an item's is not a value.
	value bool
	tag   tag.Tag
}

func hostileStreams() []hostile {
	tail := make([]byte, 16)
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	// A File Meta element after the Transfer Syntax UID.
	syntax := "1.2.840.10008.1.2.1\x00"
	meta := cat(header(0x0002, 0x0010, "UI", uint32(len(syntax))), []byte(syntax),
		header(0x0002, 0x0102, "OB", oneGiB), tail)
	metaFile := cat(make([]byte, 128), []byte("DICM"), header(0x0002, 0x0000, "UL", 4),
		binary.LittleEndian.AppendUint32(nil, uint32(len(meta))), meta)

	return []hostile{
		{"an element value", part10("1.2.840.10008.1.2.1",
			cat(header(0x0009, 0x1010, "OB", oneGiB), tail)), true, tag.New(0x0009, 0x1010)},
		{"a text value", part10("1.2.840.10008.1.2.1",
			cat(header(0x0008, 0x0119, "UC", oneGiB), tail)), true, tag.New(0x0008, 0x0119)},
		{"an implicit VR value", part10("1.2.840.10008.1.2",
			cat(header(0x0010, 0x4000, "", oneGiB), tail)), true, tag.New(0x0010, 0x4000)},
		{"a File Meta element", metaFile, true, tag.New(0x0002, 0x0102)},
		{"an encapsulated fragment", part10("1.2.840.10008.1.2.4.50",
			cat(header(0x7FE0, 0x0010, "OB", UndefinedLength), header(0xFFFE, 0xE000, "", 0),
				header(0xFFFE, 0xE000, "", oneGiB), tail)), true, tag.New(0x7FE0, 0x0010)},
		{"a sequence", part10("1.2.840.10008.1.2.1",
			cat(header(0x0008, 0x1115, "SQ", 0xFFFFFFF0), header(0xFFFE, 0xE000, "", UndefinedLength), tail)),
			false, tag.New(0x0008, 0x1115)},
		{"a sequence item", part10("1.2.840.10008.1.2.1",
			cat(header(0x0008, 0x1115, "SQ", UndefinedLength), header(0xFFFE, 0xE000, "", oneGiB), tail)),
			false, tag.New(0x0008, 0x1115)},
	}
}

// allocatedBy reports how many bytes read allocated.
func allocatedBy(read func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	read()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// allocationBound is far above what reading any of the hostile streams needs
// and far below what one of their lengths would cost.
const allocationBound = 4 << 20

// From a source whose size is unknown, each length the file states is read
// as its bytes arrive: what is allocated follows the bytes that came, not the
// gigabyte the header claims.
func TestStatedLengthsFromAStreamAllocateOnlyWhatArrives(t *testing.T) {
	for _, h := range hostileStreams() {
		t.Run(h.name, func(t *testing.T) {
			var err error
			n := allocatedBy(func() { _, err = ReadDICOMFileWithOptions(fromStream(h.data), ReadOptions{}) })
			t.Logf("%d-byte stream: %d bytes allocated, err %v", len(h.data), n, err)
			if n > allocationBound {
				t.Errorf("reading a %d-byte stream allocated %d MiB", len(h.data), n>>20)
			}
		})
	}
}

// The meta group is held to the size of a file whose size is known, as the
// data set always was.
func TestAFileMetaElementIsHeldToTheFileSize(t *testing.T) {
	h := hostileStreams()[3]
	var df *DICOMFile
	var err error
	n := allocatedBy(func() { df, err = ReadDICOMFile(fromFile(h.data)) })
	if err != nil {
		t.Fatal(err)
	}
	if n > allocationBound {
		t.Errorf("a %d-byte file allocated %d MiB", len(h.data), n>>20)
	}
	if df.FileMetaInfo.TransferSyntaxUID != "1.2.840.10008.1.2.1" || !warned(df, "skipped file meta element (0002,0102)") {
		t.Errorf("syntax %q, warnings %q", df.FileMetaInfo.TransferSyntaxUID, df.Warnings)
	}
}

// With a maximum, a value stating more is refused by its tag and its length
// before any of it is allocated, from a stream or a file alike. A sequence or
// an item is not a value and is not refused for its length.
func TestMaxValueLengthRefusesALongerValue(t *testing.T) {
	const limit = 1 << 20
	for _, h := range hostileStreams() {
		for _, source := range []struct {
			name string
			open func([]byte) filebase.Reader
		}{{"stream", fromStream}, {"file", fromFile}} {
			t.Run(h.name+" from a "+source.name, func(t *testing.T) {
				var err error
				n := allocatedBy(func() {
					_, err = ReadDICOMFileWithOptions(source.open(h.data), ReadOptions{MaxValueLength: limit})
				})
				if n > allocationBound {
					t.Errorf("%d MiB allocated", n>>20)
				}
				var tooLong *ValueTooLongError
				if !h.value {
					if errors.As(err, &tooLong) {
						t.Errorf("refused as a value: %v", err)
					}
					return
				}
				if !errors.Is(err, ErrValueTooLong) || !errors.As(err, &tooLong) {
					t.Fatalf("err = %v, want a ValueTooLongError", err)
				}
				want := uint64(oneGiB)
				if h.tag == pixelDataTag {
					want += 16 // the empty offset table's header and the fragment's
				}
				if tooLong.Tag != h.tag || tooLong.Length != want || tooLong.Max != limit {
					t.Errorf("refused %s, %d bytes, max %d; want %s, %d bytes", tooLong.Tag, tooLong.Length, tooLong.Max, h.tag, want)
				}
				t.Logf("%v", err)
			})
		}
	}
}

// A value at the maximum is read; one byte more is refused, and the message
// says which element, where, and how long, in the dictionary's words.
func TestMaxValueLengthIsInclusive(t *testing.T) {
	data := bulkFile()
	at, err := ReadDICOMFileWithOptions(fromFile(data), ReadOptions{MaxValueLength: 300})
	if err != nil {
		t.Fatal(err)
	}
	whole, _ := ReadDICOMFile(fromFile(data))
	if !reflect.DeepEqual(at.DataElements, whole.DataElements) {
		t.Error("a read with a maximum of the longest value differs from one without")
	}
	_, err = ReadDICOMFileWithOptions(fromStream(data), ReadOptions{MaxValueLength: 299})
	const want = "Waveform Data (5400,1010) at offset 210 states a value of 300 bytes, more than the maximum of 299 this read holds"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant %s", err, want)
	}
}

// A value SkipBulkData leaves in the file is not held, so the maximum does
// not refuse it: Pixel Data of any length is passed over while the values
// that are read are held to the maximum.
func TestMaxValueLengthWithSkipBulkData(t *testing.T) {
	data := bulkFile()
	for name, open := range map[string]func([]byte) filebase.Reader{"stream": fromStream, "file": fromFile} {
		df, err := ReadDICOMFileWithOptions(open(data), ReadOptions{SkipBulkData: true, MaxValueLength: 20})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p := elementAt(df, 0x7FE0, 0x0010); p == nil || !p.ValueSkipped {
			t.Errorf("%s: the pixel data was not skipped", name)
		}
		// A value read into memory is still held to the maximum.
		_, err = ReadDICOMFileWithOptions(open(data), ReadOptions{SkipBulkData: true, BulkDataThreshold: 300, MaxValueLength: 20})
		var tooLong *ValueTooLongError
		if !errors.As(err, &tooLong) || tooLong.Tag != tag.New(0x5400, 0x1010) {
			t.Errorf("%s: a 300-byte value under the threshold was not refused: %v", name, err)
		}
	}
}

// Under the Deflated syntax the data set is inflated as it arrives, and then
// read from memory: a value it states past the maximum is refused there too.
func TestMaxValueLengthInADeflatedDataSet(t *testing.T) {
	var deflated bytes.Buffer
	w, _ := flate.NewWriter(&deflated, flate.BestCompression)
	_, _ = w.Write(header(0x0009, 0x1010, "OB", 1<<24))
	_, _ = w.Write(make([]byte, 1<<24))
	_ = w.Close()
	data := append(part10(DeflatedExplicitVRLittleEndianUID, nil), deflated.Bytes()...)

	_, err := ReadDICOMFileWithOptions(fromStream(data), ReadOptions{MaxValueLength: 1 << 20})
	var tooLong *ValueTooLongError
	if !errors.As(err, &tooLong) || tooLong.Tag != tag.New(0x0009, 0x1010) || tooLong.Length != 1<<24 {
		t.Errorf("err = %v", err)
	}
}

// Every clean file of pydicom's corpus reads the same from a stream, in
// pieces, as from a file in one allocation per value, and the same with a
// maximum at its longest value as with none.
func TestReadingFromAStreamAcrossThePydicomCorpus(t *testing.T) {
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.dcm"))
	checked, inPieces := 0, 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		whole, err := ReadDICOMFile(fromFile(data))
		// A stream cannot be looked ahead in, so a file without a preamble
		// or a group length, or one whose syntax has to be judged from its
		// data set, is read differently from a stream by design.
		if err != nil || !whole.HasPreamble || whole.MetaGroupLength == nil || truncatedOrRepaired(whole.Warnings) ||
			warned(whole, "reading the data set as") {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			streamed, err := ReadDICOMFile(fromStream(data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(streamed.DataElements, whole.DataElements) || !reflect.DeepEqual(streamed.MetaElements, whole.MetaElements) {
				t.Error("the elements read from a stream differ from those read from the file")
			}
			longest := max(longestValue(whole.DataElements), longestValue(whole.MetaElements))
			if longest > valuePieceSize {
				inPieces++
			}
			bounded, err := ReadDICOMFileWithOptions(fromStream(data), ReadOptions{MaxValueLength: max(longest, 1)})
			if err != nil {
				t.Fatalf("a maximum of the longest value, %d: %v", longest, err)
			}
			if !reflect.DeepEqual(bounded.DataElements, whole.DataElements) {
				t.Error("the elements read with a maximum differ")
			}
		})
		checked++
	}
	t.Logf("%d files checked, %d with a value read in pieces", checked, inPieces)
	if checked < 40 || inPieces == 0 {
		t.Errorf("%d files checked, %d with a value read in pieces", checked, inPieces)
	}
}

// longestValue is the longest value held among elements, an encapsulation
// counted whole.
func longestValue(elements []*DataElementValue) uint32 {
	longest := uint32(0)
	for _, e := range elements {
		longest = max(longest, uint32(len(e.Value)))
		for _, item := range e.Items {
			longest = max(longest, longestValue(item.Elements))
		}
	}
	return longest
}

// FuzzReadWithMaxValueLength reads arbitrary bytes from a stream that cannot
// seek, with a maximum, which is how a server reads what it is sent. It may
// refuse anything; it must not panic, and a value it returns is never longer
// than the maximum.
//
//	go test ./filereader/ -run=^$ -fuzz=FuzzReadWithMaxValueLength -fuzztime=5m
func FuzzReadWithMaxValueLength(f *testing.F) {
	f.Add(bulkFile())
	for _, h := range hostileStreams() {
		f.Add(h.data)
	}
	const limit = 256
	var check func(t *testing.T, elements []*DataElementValue)
	check = func(t *testing.T, elements []*DataElementValue) {
		for _, e := range elements {
			if len(e.Value) > limit {
				t.Fatalf("%s holds %d bytes, over the maximum of %d", e.Tag, len(e.Value), limit)
			}
			for _, item := range e.Items {
				check(t, item.Elements)
			}
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		df, err := ReadDICOMFileWithOptions(fromStream(data), ReadOptions{MaxValueLength: limit})
		if err != nil {
			return
		}
		check(t, df.MetaElements)
		check(t, df.DataElements)
		_ = df.GetDataset()
	})
}
