package dicomweb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
)

// TestFramesOfHTJ2K serves the frames of instances held in High-Throughput
// JPEG 2000: uncompressed by default, decoded to the samples pydicom 3 decodes
// the file to (the digests of dataset/htj2k_test.go); as stored, the
// codestream labeled image/jphc with its syntax (T.814 Annex E.3); encoded
// into JPEG 2000 Lossless when that is asked of a lossless instance; and
// refused for image/jphc from an instance not held in it, since HTJ2K is not
// encoded, and for any other compressed type from the lossy one.
func TestFramesOfHTJ2K(t *testing.T) {
	for _, held := range []struct {
		file, syntax, digest string
		lossy                bool
	}{
		{"htj2k_lossless_rpcl_16bit.dcm", compress.HTJ2KLosslessRPCL, "3663c9abef3d1384", false},
		{"htj2k_irreversible.dcm", compress.HTJ2K, "5d0d0efe8e5cb8b9", true},
	} {
		file, err := os.ReadFile(filepath.Join("..", "dataset", "testdata", "htj2k", held.file))
		if err != nil {
			t.Fatal(err)
		}
		df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(file)))
		if err != nil {
			t.Fatal(err)
		}
		codestream, err := df.GetDataset().GetEncapsulatedFrame(0)
		if err != nil {
			t.Fatal(err)
		}
		backend := &recorder{}
		backend.hold("1.2.3.7", file)
		path := heldInstances + "1.2.3.7/frames/1"

		response, body := get(t, backend, path)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", held.file, response.StatusCode, body)
		}
		bodies, types, _ := frameParts(t, response, body)
		if len(bodies) != 1 || types[0] != "application/octet-stream; transfer-syntax="+syntaxExplicitLE {
			t.Fatalf("%s: %d parts, %q", held.file, len(bodies), types)
		}
		sum := sha256.Sum256(bodies[0])
		if got := hex.EncodeToString(sum[:])[:16]; got != held.digest {
			t.Errorf("%s: the decoded frame hashes to %s, pydicom's samples to %s", held.file, got, held.digest)
		}

		for _, accept := range []string{acceptStored, `multipart/related; type="image/jphc"`} {
			response, body := get(t, backend, path, "Accept", accept)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s, Accept %q: status %d: %s", held.file, accept, response.StatusCode, body)
			}
			bodies, types, of := frameParts(t, response, body)
			if of != "image/jphc" || len(bodies) != 1 || !bytes.Equal(bodies[0], codestream) {
				t.Errorf("%s, Accept %q: %d parts of %q, not the codestream", held.file, accept, len(bodies), of)
			}
			if len(types) == 1 && types[0] != "image/jphc; transfer-syntax="+held.syntax {
				t.Errorf("%s, Accept %q: labeled %q", held.file, accept, types[0])
			}
		}

		response, body = get(t, backend, path, "Accept", `multipart/related; type="image/jp2"`)
		if held.lossy {
			if response.StatusCode != http.StatusNotAcceptable {
				t.Errorf("%s: a lossy frame encoded again as image/jp2: status %d", held.file, response.StatusCode)
			}
		} else if response.StatusCode != http.StatusOK {
			t.Errorf("%s: image/jp2: status %d: %s", held.file, response.StatusCode, body)
		}
	}

	// An instance held otherwise cannot be had as HTJ2K: it is not encoded.
	file, err := encodeFile(multiFrame(t, "1.2.3.7", 1, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	backend := &recorder{}
	backend.hold("1.2.3.7", file)
	if response, _ := get(t, backend, heldInstances+"1.2.3.7/frames/1", "Accept",
		`multipart/related; type="image/jphc"`); response.StatusCode != http.StatusNotAcceptable {
		t.Errorf("a native frame asked for as image/jphc: status %d, want 406", response.StatusCode)
	}
}
