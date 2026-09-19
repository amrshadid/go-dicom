package filereader_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// TestExplicitVRBigEndian verifies a big endian file parses completely and that
// its numeric values mean the same thing as the little endian encoding of the
// same data.
//
// Two defects made this fail. The short-form value length was assembled little
// endian regardless of byte order, so parsing collapsed after one element. Once
// that was fixed, numeric values were still byte-swapped: BitsAllocated of 16
// read back as 4096, the same bits in the other order.
func TestExplicitVRBigEndian(t *testing.T) {
	le := readFile(t, buildFile("1.2.840.10008.1.2.1", explicitDataset(binary.LittleEndian), false))
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false))

	if len(be.DataElements) != len(le.DataElements) {
		t.Fatalf("big endian parsed %d elements, little endian %d",
			len(be.DataElements), len(le.DataElements))
	}
	if len(be.DataElements) != 5 {
		t.Fatalf("parsed %d elements, want 5", len(be.DataElements))
	}

	// After parsing, a value must mean the same thing regardless of the file's
	// byte order — that is what lets everything downstream assume one order.
	beDS, leDS := be.GetDataset(), le.GetDataset()
	for _, tc := range []struct {
		name string
		tg   tag.Tag
	}{
		{"Rows", tag.New(0x0028, 0x0010)},
		{"Columns", tag.New(0x0028, 0x0011)},
		{"a UL value", tag.New(0x0018, 0x1063)},
		{"PatientName", tag.New(0x0010, 0x0010)},
	} {
		beElem, ok := beDS.Get(tc.tg)
		if !ok {
			t.Errorf("%s missing from the big endian file", tc.name)
			continue
		}
		leElem, _ := leDS.Get(tc.tg)
		if !bytes.Equal(beElem.GetValue().([]byte), leElem.GetValue().([]byte)) {
			t.Errorf("%s: big endian value % X, little endian % X — should match after parsing",
				tc.name, beElem.GetValue(), leElem.GetValue())
		}
	}
}

// TestBigEndianTextValuesAreNotSwapped guards the other half of normalisation:
// text and byte-oriented VRs carry no byte order and must be left alone.
func TestBigEndianTextValuesAreNotSwapped(t *testing.T) {
	be := readFile(t, buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false))

	elem, ok := be.GetDataset().Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("PatientName missing")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("PatientName = %q, want %q — a text VR was byte-swapped", got, "Doe^John")
	}
}

// TestBigEndianWriteRoundTrip verifies a big endian file written from a parsed
// big endian file preserves its values.
//
// Reading normalises big endian values to little endian so the rest of the
// library can assume one order; writing must therefore convert back. Without
// the inverse, a file round-tripped through the library kept its big endian
// declaration but held little endian values — Rows of 64 read back as 16384.
//
// The file meta header is also always Explicit VR Little Endian whatever the
// data set's syntax; writing it in the data set's order produced a header whose
// first tag read back as (0200,0000).
func TestBigEndianWriteRoundTrip(t *testing.T) {
	original := buildFile("1.2.840.10008.1.2.2", explicitDataset(binary.BigEndian), false)
	parsed := readFile(t, original)

	// Write it back out, still declaring big endian.
	var out bytes.Buffer
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&writeSeeker{&out}))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    parsed.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: parsed.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          "1.2.840.10008.1.2.2",
	})
	for _, e := range parsed.DataElements {
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag: e.Tag, VR: e.VR, Value: e.Value, Length: uint32(len(e.Value)),
		}); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Reading it back must reproduce the same values.
	back := readFile(t, out.Bytes())

	if len(back.DataElements) != len(parsed.DataElements) {
		t.Fatalf("round trip changed the element count: %d -> %d",
			len(parsed.DataElements), len(back.DataElements))
	}

	backDS, parsedDS := back.GetDataset(), parsed.GetDataset()
	for _, tg := range []tag.Tag{
		tag.New(0x0028, 0x0010), // Rows
		tag.New(0x0028, 0x0011), // Columns
		tag.New(0x0018, 0x1063), // a UL value
		tag.New(0x0010, 0x0010), // PatientName
	} {
		a, okA := parsedDS.Get(tg)
		b, okB := backDS.Get(tg)
		if !okA || !okB {
			t.Errorf("%s missing after the round trip", tg)
			continue
		}
		if !bytes.Equal(a.GetValue().([]byte), b.GetValue().([]byte)) {
			t.Errorf("%s changed: % X -> % X", tg, a.GetValue(), b.GetValue())
		}
	}
}

// widePixelDataset is two 32-bit OW samples plus the attributes needed to
// know the sample width. Pixel Data is a long-form VR.
func widePixelDataset(order binary.ByteOrder, samples []uint32) []byte {
	var buf bytes.Buffer
	writeShort := func(group, element uint16, vr string, value []byte) {
		_ = binary.Write(&buf, order, group)
		_ = binary.Write(&buf, order, element)
		buf.WriteString(vr)
		_ = binary.Write(&buf, order, uint16(len(value)))
		buf.Write(value)
	}
	putUS := func(group, element, v uint16) {
		b := make([]byte, 2)
		order.PutUint16(b, v)
		writeShort(group, element, "US", b)
	}
	putUS(0x0028, 0x0010, 1)  // Rows
	putUS(0x0028, 0x0011, 2)  // Columns
	putUS(0x0028, 0x0100, 32) // BitsAllocated
	putUS(0x0028, 0x0101, 32) // BitsStored
	putUS(0x0028, 0x0103, 0)  // PixelRepresentation

	pix := make([]byte, 4*len(samples))
	for i, s := range samples {
		order.PutUint32(pix[i*4:], s)
	}
	_ = binary.Write(&buf, order, uint16(0x7FE0))
	_ = binary.Write(&buf, order, uint16(0x0010))
	buf.WriteString("OW")
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, order, uint32(len(pix)))
	buf.Write(pix)
	return buf.Bytes()
}

// TestWideOWPixelDataUsesSampleWidth covers 32-bit Pixel Data in Explicit VR
// Big Endian. A VR-width (OW) swap transposes each sample's 16-bit halves, so
// 1249000 is stored as 250085395. Writing that as little endian then hands
// every other reader the scramble. Swap at BitsAllocated instead.
func TestWideOWPixelDataUsesSampleWidth(t *testing.T) {
	const a, b uint32 = 1249000, 1250000
	leRaw := buildFile("1.2.840.10008.1.2.1", widePixelDataset(binary.LittleEndian, []uint32{a, b}), false)
	beRaw := buildFile("1.2.840.10008.1.2.2", widePixelDataset(binary.BigEndian, []uint32{a, b}), false)
	le := readFile(t, leRaw)
	be := readFile(t, beRaw)

	lePix, ok := le.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("little endian PixelData missing")
	}
	bePix, ok := be.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("big endian PixelData missing")
	}
	leVal := lePix.GetValue().([]byte)
	beVal := bePix.GetValue().([]byte)
	if !bytes.Equal(leVal, beVal) {
		t.Fatalf("PixelData after parse: LE % X, BE % X — 32-bit samples should match",
			leVal, beVal)
	}
	if got := binary.LittleEndian.Uint32(beVal); got != a {
		t.Fatalf("first sample is %d, want %d", got, a)
	}

	// Writing the big-endian parse as Explicit VR Little Endian must keep the
	// samples. The old OW 16-bit swap left them transposed in the file.
	var out bytes.Buffer
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&writeSeeker{&out}))
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    be.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: be.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          "1.2.840.10008.1.2.1",
	})
	for _, e := range be.DataElements {
		if err := w.AddDataElement(&filewriter.DataElement{
			Tag: e.Tag, VR: e.VR, Value: e.Value, Length: uint32(len(e.Value)),
		}); err != nil {
			t.Fatalf("AddDataElement %s: %v", e.Tag, err)
		}
	}
	if err := w.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	back := readFile(t, out.Bytes())
	backPix, ok := back.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("PixelData missing after LE rewrite")
	}
	if got := binary.LittleEndian.Uint32(backPix.GetValue().([]byte)); got != a {
		t.Fatalf("after LE rewrite first sample is %d, want %d", got, a)
	}

	// Writing the same data set as Explicit VR Big Endian covers the writer's
	// sample-width swap. The LE rewrite above never takes that branch.
	var beOut bytes.Buffer
	wbe := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(&writeSeeker{&beOut}))
	wbe.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    be.FileMetaInfo.MediaStorageSOPClassUID,
		MediaStorageSOPInstanceUID: be.FileMetaInfo.MediaStorageSOPInstanceUID,
		TransferSyntaxUID:          "1.2.840.10008.1.2.2",
	})
	for _, e := range be.DataElements {
		if err := wbe.AddDataElement(&filewriter.DataElement{
			Tag: e.Tag, VR: e.VR, Value: e.Value, Length: uint32(len(e.Value)),
		}); err != nil {
			t.Fatalf("AddDataElement BE %s: %v", e.Tag, err)
		}
	}
	if err := wbe.Write(); err != nil {
		t.Fatalf("Write BE: %v", err)
	}
	backBE := readFile(t, beOut.Bytes())
	backBEPix, ok := backBE.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("PixelData missing after BE rewrite")
	}
	if got := binary.LittleEndian.Uint32(backBEPix.GetValue().([]byte)); got != a {
		t.Fatalf("after BE rewrite first sample is %d, want %d", got, a)
	}
}

// iconPixelDataset is a 32-bit dose plus an Icon Image Sequence whose item
// has BitsAllocated 8 and OW Pixel Data. The icon must be swapped at the
// item's sample width, not the parent's 32.
func iconPixelDataset(order binary.ByteOrder, samples []uint32, icon []byte) []byte {
	var buf bytes.Buffer
	writeShort := func(dst *bytes.Buffer, group, element uint16, vr string, value []byte) {
		_ = binary.Write(dst, order, group)
		_ = binary.Write(dst, order, element)
		dst.WriteString(vr)
		_ = binary.Write(dst, order, uint16(len(value)))
		dst.Write(value)
	}
	putUS := func(dst *bytes.Buffer, group, element, v uint16) {
		b := make([]byte, 2)
		order.PutUint16(b, v)
		writeShort(dst, group, element, "US", b)
	}
	putUS(&buf, 0x0028, 0x0010, 1)
	putUS(&buf, 0x0028, 0x0011, 2)
	putUS(&buf, 0x0028, 0x0100, 32)
	putUS(&buf, 0x0028, 0x0101, 32)
	putUS(&buf, 0x0028, 0x0103, 0)

	pix := make([]byte, 4*len(samples))
	for i, s := range samples {
		order.PutUint32(pix[i*4:], s)
	}
	_ = binary.Write(&buf, order, uint16(0x7FE0))
	_ = binary.Write(&buf, order, uint16(0x0010))
	buf.WriteString("OW")
	buf.Write([]byte{0x00, 0x00})
	_ = binary.Write(&buf, order, uint32(len(pix)))
	buf.Write(pix)

	var item bytes.Buffer
	putUS(&item, 0x0028, 0x0100, 8)
	_ = binary.Write(&item, order, uint16(0x7FE0))
	_ = binary.Write(&item, order, uint16(0x0010))
	item.WriteString("OW")
	item.Write([]byte{0x00, 0x00})
	_ = binary.Write(&item, order, uint32(len(icon)))
	item.Write(icon)

	// Icon Image Sequence (0088,0200)
	_ = binary.Write(&buf, order, uint16(0x0088))
	_ = binary.Write(&buf, order, uint16(0x0200))
	buf.WriteString("SQ")
	buf.Write([]byte{0x00, 0x00})
	itemLen := uint32(8 + item.Len())
	_ = binary.Write(&buf, order, itemLen)
	_ = binary.Write(&buf, order, uint16(0xFFFE))
	_ = binary.Write(&buf, order, uint16(0xE000))
	_ = binary.Write(&buf, order, uint32(item.Len()))
	buf.Write(item.Bytes())
	return buf.Bytes()
}

func iconPixelData(t *testing.T, f *filereader.DICOMFile) []byte {
	t.Helper()
	for _, e := range f.DataElements {
		if e.Tag != tag.New(0x0088, 0x0200) {
			continue
		}
		if len(e.Items) != 1 {
			t.Fatalf("icon sequence items %d, want 1", len(e.Items))
		}
		for _, nested := range e.Items[0].Elements {
			if nested.Tag == tag.New(0x7FE0, 0x0010) {
				return nested.Value
			}
		}
		t.Fatal("icon PixelData missing")
	}
	t.Fatal("Icon Image Sequence missing")
	return nil
}

// TestIconPixelDataUsesItemBitsAllocated covers a 32-bit dose whose Icon Image
// Sequence item is 8-bit OW. Passing the parent's BitsAllocated down would
// reverse the icon four bytes at a time.
func TestIconPixelDataUsesItemBitsAllocated(t *testing.T) {
	const a, b uint32 = 1249000, 1250000
	iconBE := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	raw := buildFile("1.2.840.10008.1.2.2", iconPixelDataset(binary.BigEndian, []uint32{a, b}, iconBE), false)
	parsed := readFile(t, raw)

	dose, ok := parsed.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("dose PixelData missing")
	}
	if got := binary.LittleEndian.Uint32(dose.GetValue().([]byte)); got != a {
		t.Fatalf("dose first sample is %d, want %d", got, a)
	}

	got := iconPixelData(t, parsed)
	// 8-bit OW is swapped as 16-bit words, not as 32-bit dose samples.
	want := []byte{0x02, 0x01, 0x04, 0x03, 0x06, 0x05, 0x08, 0x07}
	if !bytes.Equal(got, want) {
		t.Fatalf("icon PixelData % X, want % X (parent 32-bit swap would give 04 03 02 01 08 07 06 05)",
			got, want)
	}
}
