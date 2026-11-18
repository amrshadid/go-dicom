package dicomweb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// A frame is sent as the bytes that hold it, so these tests compare bytes:
// against the frame the library finds by reading the whole file, and, in
// scripts/interop-test.sh, against what pydicom divides out of the same
// file and what Orthanc sends for it.

const (
	heldInstances = "/studies/1.2.3.100/series/1.2.3.9/instances/"
	acceptStored  = `multipart/related; type="application/octet-stream"; transfer-syntax=*`
)

// frameParts reads a multipart response of frames into its parts' bodies
// and types, and the type the multipart says its parts are.
func frameParts(t *testing.T, response *http.Response, body string) (bodies [][]byte, types []string, of string) {
	t.Helper()
	media, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "multipart/related" {
		t.Fatalf("Content-Type is %q: %s", response.Header.Get("Content-Type"), body)
	}
	reader := multipart.NewReader(strings.NewReader(body), parameters["boundary"])
	for {
		part, err := reader.NextRawPart()
		if err != nil {
			break
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, data)
		types = append(types, part.Header.Get("Content-Type"))
	}
	return bodies, types, parameters["type"]
}

// multiFrame is an 8-bit image of several frames, each frame's samples
// starting at its own number so no two frames are alike.
func multiFrame(t *testing.T, uid string, frames, rows, columns int) *dataset.Dataset {
	t.Helper()
	ds := image(t, uid)
	pixels := make([]byte, frames*rows*columns)
	for i := range pixels {
		pixels[i] = byte(i/(rows*columns)*7 + i%13)
	}
	for _, e := range []struct {
		t     tag.Tag
		vr    dataelem.VR
		value any
	}{
		{tag.New(0x0020, 0x000E), dataelem.UI, "1.2.3.9"},
		{tag.New(0x0028, 0x0008), dataelem.IS, fmt.Sprint(frames)},
		{tag.New(0x0028, 0x0010), dataelem.US, uint16(rows)},
		{tag.New(0x0028, 0x0011), dataelem.US, uint16(columns)},
		{tag.New(0x7FE0, 0x0010), dataelem.OB, pixels},
	} {
		ds.Remove(e.t)
		if err := ds.Add(dataelem.NewDataElement(e.t, e.vr, e.value)); err != nil {
			t.Fatal(err)
		}
	}
	return ds
}

// TestFramesNative checks frames of native pixel data: the frames asked
// for, in the order asked, each the frame's bytes, labeled uncompressed
// Explicit VR Little Endian whatever the request said of the syntax.
func TestFramesNative(t *testing.T) {
	ds := multiFrame(t, "1.2.3.7", 5, 4, 6)
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	pixels, _ := ds.Get(tag.New(0x7FE0, 0x0010))
	all := pixels.GetValue().([]byte)
	frame := func(n int) []byte { return all[(n-1)*24 : n*24] }

	for _, accept := range []string{"", "*/*", acceptStored,
		`multipart/related; type="application/octet-stream"`,
		`multipart/related; type="application/octet-stream"; transfer-syntax=` + syntaxExplicitLE} {
		response, body := get(t, backend, heldInstances+"1.2.3.7/frames/3,1,5,3", "Accept", accept)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Accept %q: status %d: %s", accept, response.StatusCode, body)
		}
		bodies, types, of := frameParts(t, response, body)
		if of != "application/octet-stream" || len(bodies) != 4 {
			t.Fatalf("Accept %q: %d parts of %q", accept, len(bodies), of)
		}
		for i, n := range []int{3, 1, 5, 3} {
			if !bytes.Equal(bodies[i], frame(n)) {
				t.Errorf("Accept %q: part %d is not frame %d", accept, i, n)
			}
			if types[i] != "application/octet-stream; transfer-syntax="+syntaxExplicitLE {
				t.Errorf("Accept %q: part %d is labeled %q", accept, i, types[i])
			}
		}
	}
}

// TestFramesCompressed checks an instance held as JPEG Baseline: as stored,
// each frame's codestream, labeled image/jpeg; uncompressed, the frame
// decoded, the samples DecompressInPlace gives for the same frame; and a
// compressed type it is not held in refused, since nothing is encoded on
// request.
func TestFramesCompressed(t *testing.T) {
	ds := multiFrame(t, "1.2.3.8", 3, 16, 16)
	if err := ds.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	file, err := encodeFile(ds)
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.8", file)
	path := heldInstances + "1.2.3.8/frames/2,3"

	for _, accept := range []string{acceptStored, `multipart/related; type="image/jpeg"`,
		`multipart/related; type="image/jpeg"; transfer-syntax=` + syntaxJPEGBaseline,
		`multipart/related; type="image/jls", multipart/related; type="image/jpeg"`} {
		response, body := get(t, backend, path, "Accept", accept)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Accept %q: status %d: %s", accept, response.StatusCode, body)
		}
		bodies, types, of := frameParts(t, response, body)
		if of != "image/jpeg" || len(bodies) != 2 {
			t.Fatalf("Accept %q: %d parts of %q", accept, len(bodies), of)
		}
		for i, n := range []int{2, 3} {
			want, err := ds.GetEncapsulatedFrame(n - 1)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bodies[i], want) {
				t.Errorf("Accept %q: part %d is not frame %d's codestream", accept, i, n)
			}
			if types[i] != "image/jpeg; transfer-syntax="+syntaxJPEGBaseline {
				t.Errorf("Accept %q: part %d is labeled %q", accept, i, types[i])
			}
		}
	}

	response, body := get(t, backend, path)
	bodies, types, _ := frameParts(t, response, body)
	decoded := multiFrame(t, "1.2.3.8", 3, 16, 16)
	if err := decoded.CompressJPEGBaselineInPlace(90); err != nil {
		t.Fatal(err)
	}
	if err := decoded.DecompressInPlace(); err != nil {
		t.Fatal(err)
	}
	pixels, _ := decoded.Get(tag.New(0x7FE0, 0x0010))
	all := pixels.GetValue().([]byte)
	for i, n := range []int{2, 3} {
		if !bytes.Equal(bodies[i], all[(n-1)*256:n*256]) {
			t.Errorf("uncompressed part %d is not frame %d as DecompressInPlace decodes it", i, n)
		}
		if types[i] != "application/octet-stream; transfer-syntax="+syntaxExplicitLE {
			t.Errorf("uncompressed part %d is labeled %q", i, types[i])
		}
	}

	for _, accept := range []string{`multipart/related; type="image/jls"`,
		`multipart/related; type="image/jpeg"; transfer-syntax=` + syntaxJPEGLossless, "image/jpeg"} {
		if response, _ := get(t, backend, path, "Accept", accept); response.StatusCode != http.StatusNotAcceptable {
			t.Errorf("Accept %q: status %d, want 406", accept, response.StatusCode)
		}
	}
}

// TestFramesRefusals checks what is not served: a list that is not frame
// numbers counted from 1 is a bad request; a frame past the last, an
// instance not held, and an instance with no pixel data are resources that
// do not exist (PS3.18 10.4.3).
func TestFramesRefusals(t *testing.T) {
	file, err := encodeFile(multiFrame(t, "1.2.3.7", 3, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	backend.hold("1.2.3.1", part10(t, "1.2.3.1"))
	cases := []struct {
		path   string
		status int
	}{
		{heldInstances + "1.2.3.7/frames/0", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/frames/-1", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/frames/1,,2", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/frames/one", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/frames/+1", http.StatusBadRequest},
		{heldInstances + "1.2.3.7/frames/4", http.StatusNotFound},
		{heldInstances + "1.2.3.7/frames/1,4", http.StatusNotFound},
		{heldInstances + "1.2.3.6/frames/1", http.StatusNotFound},
		{heldInstances + "1.2.3.1/frames/1", http.StatusNotFound},
		{heldInstances + "1.2.3.x/frames/1", http.StatusBadRequest},
	}
	for _, c := range cases {
		if response, body := get(t, backend, c.path); response.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d: %s", c.path, response.StatusCode, c.status, body)
		}
	}
	if response, _ := send(t, backend, http.MethodPost, heldInstances+"1.2.3.7/frames/1"); response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to frames: status %d", response.StatusCode)
	}
}

// countingBackend opens its files through a reader that counts the bytes
// read from them.
type countingBackend struct {
	*recorder
	read int64
}

type countingFile struct {
	*bytes.Reader
	read *int64
}

func (f countingFile) Read(p []byte) (int, error) {
	n, err := f.Reader.Read(p)
	*f.read += int64(n)
	return n, err
}

func (f countingFile) ReadAt(p []byte, offset int64) (int, error) {
	n, err := f.Reader.ReadAt(p, offset)
	*f.read += int64(n)
	return n, err
}

func (countingFile) Close() error { return nil }

func (b *countingBackend) Open(_ context.Context, uid string) (io.ReadSeekCloser, error) {
	return countingFile{bytes.NewReader(b.files[uid]), &b.read}, nil
}

// TestFramesAreReadByPosition checks that a frame costs that frame: one
// frame of a 200-frame image, 64 KiB a frame, is served having read little
// more than the frame from a 12.5 MiB file.
func TestFramesAreReadByPosition(t *testing.T) {
	const frames, rows, columns = 200, 256, 256
	file, err := encodeFile(multiFrame(t, "1.2.3.7", frames, rows, columns))
	if err != nil {
		t.Fatal(err)
	}
	backend := &countingBackend{recorder: &recorder{}}
	backend.hold("1.2.3.7", file)
	response, body := get(t, backend, heldInstances+"1.2.3.7/frames/150")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	bodies, _, _ := frameParts(t, response, body)
	start := len(file) - frames*rows*columns + 149*rows*columns
	if len(bodies) != 1 || !bytes.Equal(bodies[0], file[start:start+rows*columns]) {
		t.Fatal("the part is not frame 150")
	}
	if limit := int64(rows*columns + 64<<10); backend.read > limit {
		t.Errorf("read %d bytes of a %d-byte file for one %d-byte frame", backend.read, len(file), rows*columns)
	}
}

// TestFramesOfCorpusFiles checks frames read by position against the same
// frames found by reading the whole file, across the corpus's multi-frame,
// encapsulated, big endian and deflated files: the encapsulated frame
// GetEncapsulatedFrame gives, or the native frame of the value the reader
// has put in little endian.
func TestFramesOfCorpusFiles(t *testing.T) {
	corpus := os.Getenv("GODICOM_PYDICOM_DATA")
	if corpus == "" {
		t.Skip("GODICOM_PYDICOM_DATA is not set")
	}
	for _, name := range []string{
		"rtdose.dcm", "rtdose_expb.dcm", "rtdose_rle.dcm", "SC_rgb_rle_2frame.dcm",
		"SC_rgb_rle_16bit_2frame.dcm", "SC_rgb_rle_32bit_2frame.dcm", "examples_ybr_color.dcm",
		"examples_jpeg2k.dcm", "MR_small_bigendian.dcm", "SC_rgb_small_odd_big_endian.dcm",
		"MR_small_jpeg_ls_lossless.dcm", "JPEG2000.dcm", "image_dfl.dcm", "MR_small_implicit.dcm",
		"SC_ybr_full_422_uncompressed.dcm", "liver_1frame.dcm",
	} {
		t.Run(name, func(t *testing.T) {
			file, err := os.ReadFile(filepath.Join(corpus, name))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
			if err != nil {
				t.Fatal(err)
			}
			ds := parsed.GetDataset()
			info, err := ds.GetPixelDataInfo()
			if err != nil {
				t.Fatal(err)
			}
			want := make([][]byte, info.NumberOfFrames)
			pixels, _ := ds.Get(tag.New(0x7FE0, 0x0010))
			value, _ := pixels.GetValue().([]byte)
			// The value may end in a pad byte that is no frame's.
			length := (info.Rows*info.Columns*info.SamplesPerPixel*info.BitsAllocated + 7) / 8
			if info.PhotometricInterpretation == "YBR_FULL_422" {
				length = length * 2 / 3
			}
			for i := range want {
				if strings.HasPrefix(ds.TransferSyntaxUID(), "1.2.840.10008.1.2.4") || ds.TransferSyntaxUID() == syntaxRLE {
					if want[i], err = ds.GetEncapsulatedFrame(i); err != nil {
						t.Fatal(err)
					}
				} else {
					want[i] = value[i*length : (i+1)*length]
				}
			}

			uid := text(ds, tagSOPInstance)
			study, series := text(ds, tagStudyUID), text(ds, tag.New(0x0020, 0x000E))
			backend := &recorder{}
			backend.hold(uid, file)
			list := make([]string, info.NumberOfFrames)
			for i := range list {
				list[i] = fmt.Sprint(len(list) - i)
			}
			response, body := get(t, backend, "/studies/"+study+"/series/"+series+"/instances/"+uid+
				"/frames/"+strings.Join(list, ","), "Accept", acceptStored)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", response.StatusCode, body)
			}
			bodies, _, _ := frameParts(t, response, body)
			if len(bodies) != len(want) {
				t.Fatalf("%d parts for %d frames", len(bodies), len(want))
			}
			for i := range bodies {
				n := len(want) - i
				if !bytes.Equal(bodies[i], want[n-1]) {
					t.Errorf("frame %d: %d bytes, the whole file's reading gives %d, and they differ",
						n, len(bodies[i]), len(want[n-1]))
				}
			}
		})
	}
}

// syntaxRLE is RLE Lossless.
const syntaxRLE = "1.2.840.10008.1.2.5"
