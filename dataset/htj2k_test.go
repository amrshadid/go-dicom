package dataset_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// htj2kSamples are DICOM files in the three High-Throughput JPEG 2000
// syntaxes, with the digest of the samples pydicom 3 decodes them to (through
// pylibjpeg-openjpeg 2.5.0, that is OpenJPEG): sha256 of numpy's bytes, the
// first 16 hex digits, which for these little-endian, pixel-interleaved images
// is the digest of the native Pixel Data.
//
// The two pydicom-data files are not committed, and their path is given by
// GODICOM_HTJ2K_DICOM. The three in testdata/htj2k are generated: images
// written by opj_compress, their code-blocks coded again as HT ones by the
// test encoder of compress/jpeg2000 (htTranscode), and the codestreams
// wrapped by pydicom 3. No third-party file in 1.2.840.10008.1.2.4.202 was
// found anywhere, so htj2k_lossless_rpcl_16bit.dcm, an RPCL codestream, is
// the only one in that syntax.
var htj2kSamples = []struct {
	file, syntax, digest string
	committed            bool
}{
	{"htj2k_lossless_rgb.dcm", "1.2.840.10008.1.2.4.201", "746f7d32eb7cd276", true},
	{"htj2k_lossless_rpcl_16bit.dcm", "1.2.840.10008.1.2.4.202", "3663c9abef3d1384", true},
	{"htj2k_irreversible.dcm", "1.2.840.10008.1.2.4.203", "5d0d0efe8e5cb8b9", true},
	// pydicom-data: 640x480 RGB secondary captures.
	{"HTJ2KLossless_08_RGB.dcm", "1.2.840.10008.1.2.4.201", "9d87240604f5d752", false},
}

// TestHTJ2KDecodesToPydicomsSamples decompresses each HTJ2K sample in place
// and requires pydicom's samples, by digest, and a data set that says what it
// now holds: Explicit VR Little Endian and RGB for a YBR_RCT image.
//
// pydicom-data's lossy HTJ2K_08_RGB.dcm (.203) is not listed: OpenJPEG and
// this decoder land either side of a half on 691 of its 921600 samples, the
// 9/7 tolerance of CONFORMANCE.md §8.1, which compress/jpeg2000 checks
// against OpenJPEG sample by sample.
func TestHTJ2KDecodesToPydicomsSamples(t *testing.T) {
	external := os.Getenv("GODICOM_HTJ2K_DICOM")
	for _, sample := range htj2kSamples {
		t.Run(sample.file, func(t *testing.T) {
			path := filepath.Join("testdata", "htj2k", sample.file)
			if !sample.committed {
				if external == "" {
					t.Skip("set GODICOM_HTJ2K_DICOM to the directory of pydicom-data's HTJ2K files")
				}
				path = filepath.Join(external, sample.file)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
			if err != nil {
				t.Fatal(err)
			}
			ds := df.GetDataset()
			if got := ds.TransferSyntaxUID(); got != sample.syntax {
				t.Fatalf("transfer syntax %s, want %s", got, sample.syntax)
			}
			if err := ds.DecompressInPlace(); err != nil {
				t.Fatalf("DecompressInPlace: %v", err)
			}
			if got := ds.TransferSyntaxUID(); got != "1.2.840.10008.1.2.1" {
				t.Errorf("decompressed to %s", got)
			}
			elem, ok := ds.Get(tag.New(0x7FE0, 0x0010))
			if !ok {
				t.Fatal("no Pixel Data after decompressing")
			}
			pixels, _ := elem.GetValue().([]byte)
			info, err := ds.GetPixelDataInfo()
			if err != nil {
				t.Fatal(err)
			}
			n := info.BytesPerFrame * info.NumberOfFrames
			if len(pixels) < n {
				t.Fatalf("%d bytes of pixels for %d", len(pixels), n)
			}
			sum := sha256.Sum256(pixels[:n])
			if got := hex.EncodeToString(sum[:])[:16]; got != sample.digest {
				t.Errorf("the samples hash to %s, pydicom's to %s", got, sample.digest)
			}
			if ds.GetStringByKeyword("SamplesPerPixel") == "3" {
				if p := ds.GetStringByKeyword("PhotometricInterpretation"); p != "RGB" {
					t.Errorf("Photometric Interpretation %q after decoding a YBR_RCT image", p)
				}
			}
		})
	}
}
