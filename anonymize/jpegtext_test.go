package anonymize

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// jpegCorpusFile returns a file of pydicom's corpus, or skips the test.
func jpegCorpusFile(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("GODICOM_PYDICOM_DATA")
	if dir == "" {
		t.Skip("set GODICOM_PYDICOM_DATA to pydicom's test_files directory to run this")
	}
	return filepath.Join(dir, name)
}

// readDICOM reads a file whole into a data set.
func readDICOM(t *testing.T, path string) *dataset.Dataset {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return df.GetDataset()
}

// writeDICOM writes a data set as a Part 10 file in its own transfer syntax.
func writeDICOM(t *testing.T, ds *dataset.Dataset, path string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    textOf(ds, tag.New(0x0008, 0x0016)),
		MediaStorageSOPInstanceUID: textOf(ds, tag.New(0x0008, 0x0018)),
		TransferSyntaxUID:          ds.TransferSyntaxUID(),
	})
	err = w.AddDataElements(filewriter.ElementsFromDataset(ds))
	if err == nil {
		err = w.Write()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("writing %s: %v", filepath.Base(path), err)
	}
}

// setFrames replaces a data set's encapsulated Pixel Data with the frames.
func setFrames(t *testing.T, ds *dataset.Dataset, frames [][]byte) {
	t.Helper()
	for i := range frames {
		if len(frames[i])%2 == 1 {
			frames[i] = append(frames[i], 0)
		}
	}
	value, err := compress.EncapsulateFrames(frames, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	elem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	elem.SetVR(dataelem.OB)
	if err := ds.SetValue(tag.New(0x7FE0, 0x0010), value); err != nil {
		t.Fatal(err)
	}
}

// segmentOf encodes a marker segment.
func segmentOf(marker byte, payload string) []byte {
	n := len(payload) + 2
	return append([]byte{0xFF, marker, byte(n >> 8), byte(n)}, payload...)
}

// The text a program of the kind the segments hide, written by a tool of the
// test's own: none of the files has any.
const (
	hiddenComment = "Patient: DOE^JANE ID 4455667"
	hiddenExif    = "Exif\x00\x00DeviceSerial 99887766 Owner DOE^JANE"
	hiddenICC     = "ICC_PROFILE\x00\x01\x01Description of DOE^JANE"
	hiddenAfter   = "NOTE DOE^JANE 4455667"
)

// injectText returns a frame with a comment, an Exif block, an ICC profile
// segment and a note after the end of the image added: the segments go
// before the first scan, where the format allows them, and the note at the
// end. The image data is not touched.
func injectText(t *testing.T, frame []byte) []byte {
	t.Helper()
	parsed, err := scanJPEG(frame)
	if err != nil {
		t.Fatal(err)
	}
	var sos int
	for _, s := range parsed.segments {
		if s.marker == 0xDA {
			sos = s.start
			break
		}
	}
	var out []byte
	out = append(out, frame[:sos]...)
	out = append(out, segmentOf(0xFE, hiddenComment)...)
	out = append(out, segmentOf(0xE1, hiddenExif)...)
	out = append(out, segmentOf(0xE2, hiddenICC)...)
	out = append(out, frame[sos:parsed.eoi+2]...)
	out = append(out, hiddenAfter...)
	if len(out)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// frameBytes returns the frames of a data set, copied.
func frameBytes(t *testing.T, ds *dataset.Dataset) [][]byte {
	t.Helper()
	frames, err := jpegFrames(ds)
	if err != nil {
		t.Fatal(err)
	}
	out := make([][]byte, len(frames))
	for i, f := range frames {
		out[i] = append([]byte{}, f...)
	}
	return out
}

// dcmtkDecode decodes a file with a dcmtk decoder and returns the samples of
// the native file it writes, or skips the test where dcmtk is not found.
func dcmtkDecode(t *testing.T, tool, in string) []byte {
	t.Helper()
	samples, err := dcmtkDecodeErr(t, tool, in)
	if err != nil {
		t.Fatal(err)
	}
	return samples
}

// dcmtkDecodeErr is dcmtkDecode that returns dcmtk's refusal.
func dcmtkDecodeErr(t *testing.T, tool, in string) ([]byte, error) {
	t.Helper()
	path, err := exec.LookPath(tool)
	if err != nil {
		if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
			t.Fatalf("GODICOM_REQUIRE_INTEROP is set but %s is not available: %v", tool, err)
		}
		t.Skipf("%s is not available: %v", tool, err)
	}
	out := filepath.Join(t.TempDir(), "decoded.dcm")
	if msg, err := exec.Command(path, in, out).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%s %s: %v: %s", tool, filepath.Base(in), err, firstLine(string(msg)))
	}
	return readDICOM(t, out).RawPixelData()
}

// firstLine is the first line of a program's output.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// jpegCases are the files the segments are searched in: one of each family
// from pydicom's corpus, with the dcmtk decoder that reads it.
var jpegCases = []struct {
	file, decoder string
}{
	{"SC_rgb_dcmtk_+eb+cr.dcm", "dcmdjpeg"},        // Baseline, written by dcmtk
	{"SC_rgb_jpeg_app14_dcmd.dcm", "dcmdjpeg"},     // Baseline, with an Adobe APP14
	{"JPGExtended.dcm", "dcmdjpeg"},                // Extended, 12 bits
	{"SC_rgb_jpeg_gdcm.dcm", "dcmdjpeg"},           // Lossless, written by GDCM
	{"MR_small_jpeg_ls_lossless.dcm", "dcmdjpls"},  // JPEG-LS lossless
	{"JPEGLSNearLossless_16.dcm", "dcmdjpls"},      // JPEG-LS near-lossless, 16 bits
	{"examples_ybr_color.dcm", "dcmdjpeg"},         // Baseline, 30 frames
	{"SC_rgb_jls_lossy_sample.dcm", "dcmdjpls"},    // JPEG-LS, color by sample
	{"SC_jpeg_no_color_transform.dcm", "dcmdjpeg"}, // Baseline, no color transform
	{"SC_rgb_small_odd_jpeg.dcm", "dcmdjpeg"},      // Baseline, odd sized
}

// TestJPEGSegmentTextFindsWhatWasHidden proves the comment and application
// segments and the bytes after the end of the image are found in every frame
// of Baseline, Extended, Lossless and JPEG-LS files, with their text, and
// that none are reported where a file has none of its own besides the JFIF or
// Adobe header.
func TestJPEGSegmentTextFindsWhatWasHidden(t *testing.T) {
	for _, tc := range jpegCases {
		t.Run(tc.file, func(t *testing.T) {
			ds := readDICOM(t, jpegCorpusFile(t, tc.file))
			own, err := JPEGSegmentText(ds)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range own {
				t.Logf("the file's own %s in frame %d at %d: %d bytes, %q", s.Name(), s.Frame, s.Offset, len(s.Data), s.Text)
				if s.Marker == 0xFE || s.Marker == 0 {
					t.Errorf("the file itself has a %s: %q", s.Name(), s.Text)
				}
			}

			frames := frameBytes(t, ds)
			for i := range frames {
				frames[i] = injectText(t, frames[i])
			}
			setFrames(t, ds, frames)

			found, err := JPEGSegmentText(ds)
			if err != nil {
				t.Fatal(err)
			}
			perFrame := map[int]map[string]string{}
			for _, s := range found {
				if perFrame[s.Frame] == nil {
					perFrame[s.Frame] = map[string]string{}
				}
				perFrame[s.Frame][s.Name()] = s.Text
			}
			if len(perFrame) != len(frames) {
				t.Fatalf("segments found in %d frames of %d", len(perFrame), len(frames))
			}
			for i := range frames {
				got := perFrame[i]
				if got["COM"] != hiddenComment {
					t.Errorf("frame %d COM = %q, want %q", i, got["COM"], hiddenComment)
				}
				if !strings.Contains(got["APP1"], "DeviceSerial 99887766 Owner DOE^JANE") {
					t.Errorf("frame %d APP1 text = %q", i, got["APP1"])
				}
				if !strings.Contains(got["APP2"], "DOE^JANE") {
					t.Errorf("frame %d APP2 text = %q", i, got["APP2"])
				}
				if !strings.Contains(got["trailing data"], hiddenAfter) {
					t.Errorf("frame %d trailing text = %q", i, got["trailing data"])
				}
			}
		})
	}
}

// TestRemoveJPEGTextLeavesTheSamplesAsTheyWere proves that removing the
// segments changes no sample: for each of Baseline, Extended, Lossless and
// JPEG-LS, the frames with text hidden in them and the cleaned frames decode,
// with this library's decoder and with dcmtk's, to the bytes the untouched
// file decodes to, the cleaned frames hold no comment or trailing data, and
// the entropy-coded data is the same bytes.
func TestRemoveJPEGTextLeavesTheSamplesAsTheyWere(t *testing.T) {
	for _, tc := range jpegCases {
		t.Run(tc.file, func(t *testing.T) {
			cleanAndCompare(t, jpegCorpusFile(t, tc.file), tc.decoder)
		})
	}
}

// cleanAndCompare hides text in every frame of the file, removes it, and
// checks the samples against the file's own decode, with this library's
// decoder and with dcmtk's.
func cleanAndCompare(t *testing.T, path, decoder string) {
	t.Helper()
	original := readDICOM(t, path)
	want, err := original.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	wantDcmtk := dcmtkDecode(t, decoder, path)

	ds := readDICOM(t, path)
	frames := frameBytes(t, ds)
	for i := range frames {
		frames[i] = injectText(t, frames[i])
	}
	setFrames(t, ds, frames)
	dirty := filepath.Join(t.TempDir(), "dirty.dcm")
	writeDICOM(t, ds, dirty)

	removed, err := RemoveJPEGText(ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) == 0 {
		t.Fatal("nothing was removed")
	}
	left, err := JPEGSegmentText(ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range left {
		if s.Marker == 0xFE || s.Marker == 0 || s.Marker == 0xE1 || s.Marker == 0xE2 {
			t.Errorf("a %s is left: %q", s.Name(), s.Text)
		}
	}

	// This library's decoder, on the dirty frames and the clean ones.
	got, err := ds.DecodedPixelData()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("this library decodes the cleaned frames to %d bytes that differ from the %d of the original", len(got), len(want))
	}
	dirtyDS := readDICOM(t, dirty)
	if got, err := dirtyDS.DecodedPixelData(); err != nil || !bytes.Equal(got, want) {
		t.Errorf("this library decodes the frames with the text in them differently from the original: %v", err)
	}

	// The entropy-coded data, to the last byte: the cleaned frame is
	// the original frame, plus segments that a decoder needs.
	cleanFrames, origFrames := frameBytes(t, ds), frameBytes(t, original)
	for i := range cleanFrames {
		a, _ := scanJPEG(cleanFrames[i])
		b, _ := scanJPEG(origFrames[i])
		if !bytes.Equal(imageData(cleanFrames[i], a), imageData(origFrames[i], b)) {
			t.Errorf("frame %d: the image data changed", i)
		}
	}

	// dcmtk's decoder, on the file as written.
	clean := filepath.Join(t.TempDir(), "clean.dcm")
	writeDICOM(t, ds, clean)
	if got := dcmtkDecode(t, decoder, clean); !bytes.Equal(got, wantDcmtk) {
		t.Errorf("%s decodes the cleaned file differently from the original", decoder)
	}
	// CharLS, which dcmdjpls is built on, refuses a JPEG-LS frame with an
	// Exif APP1 in it, so there the file with the text has no decode
	// to compare; it is said and not failed.
	switch got, err := dcmtkDecodeErr(t, decoder, dirty); {
	case err == nil && !bytes.Equal(got, wantDcmtk):
		t.Errorf("%s decodes the file with the text in it differently from the original", decoder)
	case err != nil && decoder == "dcmdjpls":
		t.Logf("dcmdjpls refuses the file with the text in it, which the cleaned file fixes: %v", err)
	case err != nil:
		t.Errorf("%v", err)
	}
}

// imageData returns a frame's scans: from the first start-of-scan marker to
// the end-of-image marker.
func imageData(frame []byte, p *jpegScan) []byte {
	for _, s := range p.segments {
		if s.marker == 0xDA {
			return frame[s.start:p.eoi]
		}
	}
	return nil
}

// TestRemoveJPEGTextKeepsWhatADecoderNeeds proves the segments that decide
// how the samples are read stay: an Adobe APP14 and a JFIF APP0, the latter
// without its thumbnail, in a Baseline frame, and an mrfx APP8 in a JPEG-LS
// one, while an Exif APP1, a JFXX APP0 and an mrfx-less APP8 go. The frames
// are built by hand around the image data of corpus files, whose own decode
// is the reference.
func TestRemoveJPEGTextKeepsWhatADecoderNeeds(t *testing.T) {
	jfifThumb := "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x02\x01" + "\xAA\xBB\xCC\xDD\xEE\xFF" // 2 by 1 thumbnail, RGB
	adobe := "Adobe\x00\x64\x00\x00\x00\x00\x01"                                             // transform 1: YCbCr
	adobeLong := adobe + "DOE^JANE"

	t.Run("Baseline", func(t *testing.T) {
		ds := readDICOM(t, jpegCorpusFile(t, "SC_rgb_dcmtk_+eb+cr.dcm"))
		frames := frameBytes(t, ds)
		parsed, _ := scanJPEG(frames[0])
		// The frame's own segments are left out, so the ones built here are
		// the only ones.
		var first int
		for _, s := range parsed.segments {
			if !s.isText() {
				first = s.start
				break
			}
		}
		built := append([]byte{}, frames[0][:2]...)
		built = append(built, segmentOf(0xE0, jfifThumb)...)
		built = append(built, segmentOf(0xE0, "JFXX\x00\x10rest")...)
		built = append(built, segmentOf(0xE1, hiddenExif)...)
		built = append(built, segmentOf(0xEE, adobeLong)...)
		built = append(built, frames[0][first:]...)
		setFrames(t, ds, [][]byte{built})
		want, err := ds.DecodedPixelData()
		if err != nil {
			t.Fatal(err)
		}

		removed, err := RemoveJPEGText(ds)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := ds.DecodedPixelData(); err != nil || !bytes.Equal(got, want) {
			t.Errorf("the samples changed with the segments rewritten: %v", err)
		}
		kept := map[string]bool{}
		for _, s := range removed {
			kept[s.Name()+string(s.Data[:4])] = s.Kept
		}
		if !kept["APP0JFIF"] || !kept["APP14Adob"] || kept["APP1Exif"] || kept["APP0JFXX"] {
			t.Errorf("kept = %v, want the JFIF and the Adobe segments only", kept)
		}
		after := frameBytes(t, ds)[0]
		p, err := scanJPEG(after)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range p.segments {
			if s.isText() {
				payload := after[s.start+4 : s.end]
				names = append(names, string(payload))
			}
		}
		wantJFIF := "JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"
		if len(names) != 2 || names[0] != wantJFIF || names[1] != adobe {
			t.Errorf("segments left: %q, want the JFIF without thumbnail and Adobe cut to 12 bytes", names)
		}
	})

	t.Run("JPEG-LS", func(t *testing.T) {
		ds := readDICOM(t, jpegCorpusFile(t, "MR_small_jpeg_ls_lossless.dcm"))
		frames := frameBytes(t, ds)
		parsed, _ := scanJPEG(frames[0])
		var sos int
		for _, s := range parsed.segments {
			if s.marker == 0xDA {
				sos = s.start
			}
		}
		built := append([]byte{}, frames[0][:sos]...)
		built = append(built, segmentOf(0xE8, "mrfx\x01")...)
		built = append(built, segmentOf(0xE8, "SPIFF\x00")...)
		built = append(built, frames[0][sos:]...)
		setFrames(t, ds, [][]byte{built})
		removed, err := RemoveJPEGText(ds)
		if err != nil {
			t.Fatal(err)
		}
		var keptMrfx, keptSpiff bool
		for _, s := range removed {
			if bytes.HasPrefix(s.Data, []byte("mrfx")) {
				keptMrfx = s.Kept
			}
			if bytes.HasPrefix(s.Data, []byte("SPIFF")) {
				keptSpiff = s.Kept
			}
		}
		if !keptMrfx || keptSpiff {
			t.Errorf("mrfx kept %v, SPIFF kept %v; want the first kept and the second not", keptMrfx, keptSpiff)
		}
	})
}

// TestScanJPEGFollowsTheImageData proves the walker steps over entropy-coded
// data and does not take a byte inside it for a marker: a 0xFF followed by
// 0x00 in JPEG, and one followed by a byte of top bit 0 in JPEG-LS, are image
// data, a restart marker is inside the scan, and a COM after the scan is
// found.
func TestScanJPEGFollowsTheImageData(t *testing.T) {
	sof := segmentOf(0xC0, "\x08\x00\x01\x00\x01\x01\x01\x11\x00")
	sos := segmentOf(0xDA, "\x01\x01\x00\x00\x3F\x00")
	jpeg := append([]byte{0xFF, 0xD8}, sof...)
	jpeg = append(jpeg, sos...)
	jpeg = append(jpeg, 0x12, 0xFF, 0x00, 0xFE, 0x34, 0xFF, 0xD0, 0xFF, 0x00, 0xFE) // a COM-looking pair is stuffed data
	jpeg = append(jpeg, segmentOf(0xFE, "after scan")...)
	jpeg = append(jpeg, 0xFF, 0xD9)
	p, err := scanJPEG(jpeg)
	if err != nil {
		t.Fatal(err)
	}
	var comments []string
	for _, s := range p.segments {
		if s.marker == 0xFE {
			comments = append(comments, string(jpeg[s.start+4:s.end]))
		}
	}
	if len(comments) != 1 || comments[0] != "after scan" {
		t.Errorf("comments = %q, want the one after the scan", comments)
	}

	ls := append([]byte{0xFF, 0xD8}, segmentOf(0xF7, "\x08\x00\x01\x00\x01\x01\x01\x11\x00")...)
	ls = append(ls, segmentOf(0xDA, "\x01\x01\x00\x00\x00\x00")...)
	// 0xFF 0x7F is stuffing in JPEG-LS; 0xFF 0xFE is a COM marker.
	ls = append(ls, 0x12, 0xFF, 0x7F, 0x34)
	ls = append(ls, segmentOf(0xFE, "after ls scan")...)
	ls = append(ls, 0xFF, 0xD9)
	p, err = scanJPEG(ls)
	if err != nil {
		t.Fatal(err)
	}
	if !p.ls {
		t.Error("a start-of-frame of 0xF7 is JPEG-LS")
	}
	found := 0
	for _, s := range p.segments {
		if s.marker == 0xFE && string(ls[s.start+4:s.end]) == "after ls scan" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("the comment after the JPEG-LS scan was found %d times", found)
	}

	for name, broken := range map[string][]byte{
		"no start of image": {0x00, 0x01, 0x02, 0x03},
		"truncated segment": append([]byte{0xFF, 0xD8}, 0xFF, 0xFE, 0x00, 0x40, 'a'),
		"no end of image":   append(append([]byte{0xFF, 0xD8}, sof...), sos...),
	} {
		if _, err := scanJPEG(broken); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestRemoveJPEGTextRefusesWhatItDoesNotSearch proves a transfer syntax
// outside the JPEG families that is encapsulated, JPEG 2000, returns an
// error naming that and leaves the data set alone, while native pixel data
// and a data set without pixels return nothing and no error.
func TestRemoveJPEGTextRefusesWhatItDoesNotSearch(t *testing.T) {
	ds := readDICOM(t, jpegCorpusFile(t, "JPEG2000.dcm"))
	before, _ := ds.RawPixelData()
	if _, err := RemoveJPEGText(ds); !errors.Is(err, ErrJPEGSegmentsNotSearched) {
		t.Errorf("JPEG 2000: error = %v, want ErrJPEGSegmentsNotSearched", err)
	}
	if _, err := JPEGSegmentText(ds); !errors.Is(err, ErrJPEGSegmentsNotSearched) {
		t.Errorf("JPEG 2000, reading: error = %v, want ErrJPEGSegmentsNotSearched", err)
	}
	if after, _ := ds.RawPixelData(); !bytes.Equal(before, after) {
		t.Error("the refusal changed the pixel data")
	}

	native := readDICOM(t, jpegCorpusFile(t, "MR_small.dcm"))
	if segs, err := RemoveJPEGText(native); err != nil || len(segs) != 0 {
		t.Errorf("native pixel data: %v, %v", segs, err)
	}
	if segs, err := JPEGSegmentText(dataset.NewDataset()); err != nil || len(segs) != 0 {
		t.Errorf("no pixel data: %v, %v", segs, err)
	}
}

// TestRemoveJPEGTextIsLeftAloneWhenThereIsNothingToRemove proves a file with
// only the segments a decoder needs is not rewritten: its Pixel Data value is
// byte for byte what it was, and its fragments are as they were.
func TestRemoveJPEGTextIsLeftAloneWhenThereIsNothingToRemove(t *testing.T) {
	ds := readDICOM(t, jpegCorpusFile(t, "SC_rgb_jpeg_app14_dcmd.dcm"))
	before, _ := ds.RawPixelData()
	removed, err := RemoveJPEGText(ds)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range removed {
		if !s.Kept {
			t.Errorf("a %s was removed from a file that holds only what a decoder needs: %q", s.Name(), s.Text)
		}
	}
	if after, _ := ds.RawPixelData(); !bytes.Equal(before, after) {
		t.Error("the pixel data was rewritten with nothing to remove")
	}
}

// TestAnonymizerRemovesJPEGTextWhenAsked proves SetRemoveJPEGText makes
// Anonymize strip the segments, and that without it the pixel data is left as
// it was.
func TestAnonymizerRemovesJPEGTextWhenAsked(t *testing.T) {
	build := func() *dataset.Dataset {
		ds := readDICOM(t, jpegCorpusFile(t, "SC_rgb_dcmtk_+eb+cr.dcm"))
		frames := frameBytes(t, ds)
		frames[0] = injectText(t, frames[0])
		setFrames(t, ds, frames)
		return ds
	}
	count := func(ds *dataset.Dataset) int {
		segs, err := JPEGSegmentText(ds)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range segs {
			if s.Marker == 0xFE || s.Marker == 0 || s.Marker == 0xE1 {
				n++
			}
		}
		return n
	}

	plain := build()
	if err := NewAnonymizer(BasicProfile).Anonymize(plain); err != nil {
		t.Fatal(err)
	}
	if count(plain) == 0 {
		t.Error("the profile alone removed the segments, which it does not do")
	}

	asked := build()
	a := NewAnonymizer(BasicProfile)
	a.SetRemoveJPEGText(true)
	if err := a.Anonymize(asked); err != nil {
		t.Fatal(err)
	}
	if n := count(asked); n != 0 {
		t.Errorf("%d segments are left after Anonymize with SetRemoveJPEGText", n)
	}
}

// TestRemoveJPEGTextOnFilesDcmtkEncoded proves the same of a two-frame color
// image that dcmtk's own encoders wrote, Baseline, Extended, Lossless and
// JPEG-LS (lossless and near-lossless), so the frames come from software other
// than the corpus's writers: the cleaned file decodes, in this library and in
// dcmtk, to what the encoder's output decodes to.
func TestRemoveJPEGTextOnFilesDcmtkEncoded(t *testing.T) {
	source := readDICOM(t, jpegCorpusFile(t, "SC_rgb_rle_2frame.dcm"))
	if err := source.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(t.TempDir(), "native.dcm")
	writeDICOM(t, source, native)

	encoders := []struct {
		name, tool, decoder string
		args                []string
	}{
		{"baseline", "dcmcjpeg", "dcmdjpeg", []string{"+eb"}},
		{"lossless", "dcmcjpeg", "dcmdjpeg", []string{"+e1"}},
		{"jpeg-ls lossless", "dcmcjpls", "dcmdjpls", []string{"+el"}},
		{"jpeg-ls near-lossless", "dcmcjpls", "dcmdjpls", []string{"+en", "+md", "2"}},
	}
	for _, e := range encoders {
		t.Run(e.name, func(t *testing.T) {
			tool, err := exec.LookPath(e.tool)
			if err != nil {
				if os.Getenv("GODICOM_REQUIRE_INTEROP") != "" {
					t.Fatalf("GODICOM_REQUIRE_INTEROP is set but %s is not available: %v", e.tool, err)
				}
				t.Skipf("%s is not available: %v", e.tool, err)
			}
			encoded := filepath.Join(t.TempDir(), "encoded.dcm")
			args := append(append([]string{}, e.args...), native, encoded)
			if msg, err := exec.Command(tool, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s %v: %v\n%s", e.tool, e.args, err, msg)
			}
			if frames := frameBytes(t, readDICOM(t, encoded)); len(frames) != 2 {
				t.Fatalf("%s wrote %d frames, want 2", e.tool, len(frames))
			}
			cleanAndCompare(t, encoded, e.decoder)
		})
	}
}
