package network_test

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/network"
	"github.com/amrshadid/go-dicom/tag"
)

// A peer that takes only JPEG Extended for a class could not be sent an image
// (#338). It is lossy, so it is sent only with NetworkConfig.JPEGQuality, the
// same consent JPEG Baseline asks for.

// psnrSamples is the PSNR of got against want, little endian samples of
// bytesPer bytes whose peak is 2^bits - 1.
func psnrSamples(want, got []byte, bytesPer, bits int) float64 {
	var sum float64
	n := 0
	for i := 0; i+bytesPer <= len(want) && i+bytesPer <= len(got); i += bytesPer {
		x, y := float64(want[i]), float64(got[i])
		if bytesPer == 2 {
			x, y = float64(binary.LittleEndian.Uint16(want[i:])), float64(binary.LittleEndian.Uint16(got[i:]))
		}
		sum += (x - y) * (x - y)
		n++
	}
	if sum == 0 {
		return math.Inf(1)
	}
	peak := float64(int(1)<<uint(bits) - 1)
	return 10 * math.Log10(peak*peak/(sum/float64(n)))
}

// TestNativeImageIsSentAsJPEGExtended proves a .51-only peer receives, with
// JPEGQuality 90, an image in that syntax marked lossy with ISO_10918_1 and
// close to what was sent, for an 8-bit image and for pydicom's 12-bit
// examples_overlay.dcm, the sender's data set unchanged; and that without a
// quality the send fails, naming JPEGQuality.
func TestNativeImageIsSentAsJPEGExtended(t *testing.T) {
	images := map[string]func(t *testing.T) (*dataset.Dataset, int, int){
		"8-bit": func(t *testing.T) (*dataset.Dataset, int, int) {
			ds, _ := nativeGray(t)
			return ds, 1, 8
		},
		"examples_overlay.dcm": func(t *testing.T) (*dataset.Dataset, int, int) {
			return corpusFile(t, "examples_overlay.dcm"), 2, 12
		},
	}
	for name, load := range images {
		t.Run(name, func(t *testing.T) {
			ds, bytesPer, bits := load(t)
			original, err := ds.DecodedPixelData()
			if err != nil {
				t.Fatal(err)
			}
			before := ds.TransferSyntaxUID()

			if _, err := onlySyntaxStore(t, ds, network.JPEGExtendedUID, network.NetworkConfig{}); err == nil ||
				!strings.Contains(err.Error(), "JPEGQuality") {
				t.Errorf("without a quality: err = %v, want a refusal naming JPEGQuality", err)
			}

			received, err := onlySyntaxStore(t, ds, network.JPEGExtendedUID, network.NetworkConfig{JPEGQuality: 90})
			if err != nil {
				t.Fatalf("Store: %v", err)
			}
			if got := received.TransferSyntaxUID(); got != network.JPEGExtendedUID {
				t.Errorf("received as %q", got)
			}
			if got := lossyFlag(received); got != "01" {
				t.Errorf("Lossy Image Compression is %q at the receiver", got)
			}
			if method, _ := received.GetTextValue(tag.New(0x0028, 0x2114)); !strings.Contains(method, "ISO_10918_1") {
				t.Errorf("the method is %q", method)
			}
			decoded, err := received.DecodedPixelData()
			if err != nil {
				t.Fatalf("decoding what was received: %v", err)
			}
			if p := psnrSamples(original, decoded, bytesPer, bits); p < 35 {
				t.Errorf("PSNR %.1f dB at quality 90", p)
			}
			if got := ds.TransferSyntaxUID(); got != before || lossyFlag(ds) != "" {
				t.Errorf("the sender's data set became %q, lossy %q", got, lossyFlag(ds))
			}
		})
	}
}
