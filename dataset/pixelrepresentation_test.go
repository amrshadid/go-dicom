package dataset_test

import (
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// signedDataset is one 16-bit signed sample of the given value.
func signedDataset(t *testing.T, value int16, representation uint16) *dataset.Dataset {
	t.Helper()

	ds := dataset.NewDataset()
	ds.SetTransferSyntaxUID("1.2.840.10008.1.2.1")

	addUS := func(group, element, v uint16) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, v)
		_ = ds.Add(dataelem.NewDataElement(tag.New(group, element), dataelem.US, b))
	}
	addUS(0x0028, 0x0002, 1)  // SamplesPerPixel
	addUS(0x0028, 0x0010, 1)  // Rows
	addUS(0x0028, 0x0011, 1)  // Columns
	addUS(0x0028, 0x0100, 16) // BitsAllocated
	addUS(0x0028, 0x0101, 16) // BitsStored
	addUS(0x0028, 0x0102, 15) // HighBit
	addUS(0x0028, 0x0103, representation)

	px := make([]byte, 2)
	binary.LittleEndian.PutUint16(px, uint16(value))
	_ = ds.Add(dataelem.NewDataElement(tag.New(0x7FE0, 0x0010), dataelem.OW, px))
	return ds
}

// TestAccessorReadsSignedSamplesAsSigned is the bug this file exists for.
//
// Three places in this package built a pixels.PixelData and set
//
//	pd.PixelRepresentation = 0 // unsigned by default
//
// discarding (0028,0103), which the very same info struct was holding. There is
// no "default" about it: the attribute is Type 1 on every image module, always
// present, and it is the only thing that says whether a sample is signed.
//
// The pixels package has always handled both — it has signExtend, and
// GetInterpretedValue branches on the representation — so the support was there
// and the value never reached it. A CT sample of -2000 HU, which is air, came
// back as 63536.
func TestAccessorReadsSignedSamplesAsSigned(t *testing.T) {
	for _, c := range []struct {
		name           string
		stored         int16
		representation uint16
		want           float64
	}{
		{"signed negative", -2000, 1, -2000},
		{"signed positive", 1500, 1, 1500},
		{"unsigned stays unsigned", -2000, 0, 63536},
	} {
		t.Run(c.name, func(t *testing.T) {
			ds := signedDataset(t, c.stored, c.representation)

			accessor, err := ds.PixelArrayWithAccessor()
			if err != nil {
				t.Fatal(err)
			}
			got, err := accessor.GetInterpretedValue(0, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("the sample reads as %v, want %v", got, c.want)
			}
		})
	}
}

// TestPixelStatisticsUseTheDeclaredRepresentation covers the other two places
// the representation was dropped. Statistics over a signed image were computed
// on the unsigned reading of the same bytes, so a minimum below zero could not
// be reported at all.
func TestPixelStatisticsUseTheDeclaredRepresentation(t *testing.T) {
	ds := signedDataset(t, -2000, 1)

	stats, err := ds.GetPixelStatistics()
	if err != nil {
		t.Fatal(err)
	}
	if stats.MinValue > 0 {
		t.Fatalf("the minimum of a single -2000 sample is %v, which is not negative; "+
			"the signed representation was dropped", stats.MinValue)
	}
}
