package dataset

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/encaps"
	"github.com/amrshadid/go-dicom/tag"
)

// copyChunk is how much native Pixel Data a value that fills regions as it
// is copied from a file holds at once. A multiple of eight, so reversing the
// byte order of a chunk never splits a number across two.
const copyChunk = 1 << 20

// DecompressRedactingRegions is DecompressInPlace and RedactPixelRegions in
// one pass, a frame at a time: it fills the regions with black in every frame
// of the image and leaves the Pixel Data native, described as
// DecompressInPlace describes it.
//
// The result is what DecompressInPlace followed by RedactPixelRegions leaves,
// byte for byte, in the attributes as well as the pixels. What differs is
// memory. Each compressed frame is decoded, filled and put in its place
// before the next is decoded, so beside the native samples that are the
// result only one frame is held, and not a second copy of all of them.
//
// Pixel Data left in the file the data set was read from, with
// filereader.DICOMFile.GetDatasetWithValuesInFile, is not read at all. It is
// replaced by a dataelem.StreamedValue, and filewriter, given the file as its
// value source, reads and decodes each frame, fills it and writes it as it
// reaches the element, so that a 4 GB image is redacted while it is written
// and never held. The cost is that what is wrong with the frames, one that
// will not decode, is found when the element is written and not here. Every
// error that can be found from the attributes is returned here, with the data
// set as it was.
//
// Native Pixel Data is filled in place of being decompressed: see
// RedactPixelRegions. The layouts that cannot be filled are refused as there.
func (ds *Dataset) DecompressRedactingRegions(regions ...PixelRegion) error {
	pixelData := tag.New(0x7FE0, 0x0010)
	elem, ok := ds.Get(pixelData)
	if !ok {
		return ds.redact(-1, regions)
	}
	if pending := pendingOf(elem); pending != nil {
		return ds.redact(-1, regions)
	}
	info, err := ds.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}

	ref, inFile := elem.GetValue().(dataelem.ValueInFile)
	var (
		stored      []byte
		compression compress.CompressionType
		compressed  bool
	)
	if inFile {
		if compression, compressed, err = ds.compressionOfValueInFile(); err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
	} else {
		if stored, err = extractBytesValue(elem); err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
		compression, compressed = ds.pixelCompression(stored, info)
	}
	if !compressed {
		return ds.redact(-1, regions)
	}
	if compression == "" {
		return fmt.Errorf("decompress: %w", ds.undecodedSyntax())
	}
	if len(regions) == 0 {
		return fmt.Errorf("redact: no region given")
	}

	// What the image is once decoded, which is what the regions are placed
	// in: a 4:2:2 JPEG comes out with chroma for every pixel, and by pixel.
	described := ds.cloneWithoutPixelData()
	describeDecompressed(described, info)
	after, err := described.GetPixelDataInfo()
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	size := int64(after.BytesPerFrame) * int64(after.NumberOfFrames)
	fill, err := newRegionFill(described, after, -1, regions, size)
	if err != nil {
		return err
	}

	// Native pixel data is OW when a sample is wider than a byte.
	vr := dataelem.OB
	if info.BitsAllocated > 8 {
		vr = dataelem.OW
	}

	if inFile {
		pending := &pendingRedaction{
			ref: ref, vr: vr, before: ds.cloneWithoutPixelData(), infoBefore: info,
			compression: compression, compressed: true,
			view: described, infoAfter: after,
			extended: ds.extendedOffsetTable(),
			fills:    []*regionFill{fill},
		}
		elem.SetVR(vr)
		elem.SetValue(pending.stream())
		describeDecompressed(ds, info)
		return nil
	}
	samples, err := fill.decodeFrames(ds, compression, info, size)
	if err != nil {
		return err
	}
	elem.SetVR(vr)
	elem.SetValue(samples)
	describeDecompressed(ds, info)
	return nil
}

// decodeFrames decodes the frames of Pixel Data held in memory, filling the
// regions of each as it is decoded, and returns the samples of all of them.
func (f *regionFill) decodeFrames(ds *Dataset, compression compress.CompressionType, info *PixelDataInfo, size int64) ([]byte, error) {
	encData, err := ds.ExtractEncapsulatedFrames()
	if err != nil {
		return nil, fmt.Errorf("decompress: failed to read encapsulated pixel data: %w", err)
	}
	// A frame may span fragments (PS3.5 A.4).
	frames, err := encaps.Frames(encData, info.NumberOfFrames)
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	// Room for the result and one more byte, the pad of an odd length, so
	// that the result is not copied to give it.
	samples := make([]byte, 0, size+size%2)
	for i, fragment := range frames {
		frame, err := decompressPixelFrame(compression, fragment, info)
		if err != nil {
			return nil, fmt.Errorf("decompress: failed to decode %s frame %d: %w", compression, i, err)
		}
		f.apply(frame, int64(len(samples)))
		samples = append(samples, frame...)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("decompress: encapsulated pixel data held no frames")
	}
	if int64(len(samples)) < size {
		return nil, fmt.Errorf("decompress: the frames decode to %d bytes and the image needs %d", len(samples), size)
	}
	// DICOM values are of even length.
	if len(samples)%2 == 1 {
		samples = append(samples, 0x00)
	}
	return samples, nil
}

// compressionOfValueInFile says whether Pixel Data left in the file is
// compressed, and how. With no bytes in hand the transfer syntax alone
// decides, and a syntax this package cannot decode is compressed all the
// same.
func (ds *Dataset) compressionOfValueInFile() (compress.CompressionType, bool, error) {
	syntax := ds.TransferSyntaxUID()
	if syntax == "" {
		return "", false, fmt.Errorf("the data set names no transfer syntax, " +
			"so whether Pixel Data left in the file is compressed is not known")
	}
	compression, err := compress.TransferSyntaxToCompressionType(syntax)
	if err != nil {
		return "", compress.IsEncapsulated(syntax), nil
	}
	if compression == compress.UNCOMPRESSED || compression == compress.DEFLATE {
		return compression, false, nil
	}
	return compression, true, nil
}

// refuseEncapsulatedInFile returns the refusal RedactPixelRegions makes of
// compressed Pixel Data, for Pixel Data left in the file, and nil when it is
// native.
func (ds *Dataset) refuseEncapsulatedInFile() error {
	compression, compressed, err := ds.compressionOfValueInFile()
	if err != nil {
		return err
	}
	if !compressed {
		return nil
	}
	if compression == "" {
		return ds.undecodedSyntax()
	}
	return fmt.Errorf("the pixel data is compressed (%s); "+
		"it has to be decompressed before a region of it can be filled", compression)
}

// extendedOffsetTable returns how to read the Extended Offset Table
// (7FE0,0001), which was left in the file or is held, or nil when there is
// none.
func (ds *Dataset) extendedOffsetTable() func(io.ReaderAt) ([]uint64, error) {
	elem, ok := ds.Get(tag.New(0x7FE0, 0x0001))
	if !ok {
		return func(io.ReaderAt) ([]uint64, error) { return nil, nil }
	}
	return func(src io.ReaderAt) ([]uint64, error) {
		var raw []byte
		switch v := elem.GetValue().(type) {
		case dataelem.ValueInFile:
			raw = make([]byte, v.Length)
			if _, err := src.ReadAt(raw, v.Offset); err != nil {
				return nil, fmt.Errorf("reading the Extended Offset Table: %w", err)
			}
		default:
			held, ok := elem.Bytes()
			if !ok {
				return nil, nil
			}
			raw = held
		}
		if len(raw) == 0 {
			return nil, nil
		}
		if len(raw)%8 != 0 {
			return nil, fmt.Errorf("the Extended Offset Table is %d bytes, not a whole number of 8-byte offsets", len(raw))
		}
		offsets := make([]uint64, len(raw)/8)
		for i := range offsets {
			offsets[i] = binary.LittleEndian.Uint64(raw[8*i:])
		}
		return offsets, nil
	}
}

// encapsulatedItems finds the items of encapsulated Pixel Data left in src,
// the Basic Offset Table first, each as the bytes that follow its 8-byte
// header. The value is the items from the first item's tag, without the
// Sequence Delimitation Item.
func encapsulatedItems(src io.ReaderAt, ref dataelem.ValueInFile) ([]encaps.Range, error) {
	var items []encaps.Range
	header := make([]byte, 8)
	for at := int64(0); at+8 <= ref.Length; {
		if _, err := src.ReadAt(header, ref.Offset+at); err != nil {
			return nil, fmt.Errorf("redact: reading an item of the Pixel Data at offset %d: %w", ref.Offset+at, err)
		}
		group, element := binary.LittleEndian.Uint16(header), binary.LittleEndian.Uint16(header[2:])
		if group != 0xFFFE || element == 0xE0DD {
			// The delimiter, which the value does not include, or what is
			// not an item at all.
			if element == 0xE0DD {
				break
			}
			return nil, fmt.Errorf("redact: the Pixel Data holds (%04X,%04X) where an item should be, at offset %d",
				group, element, ref.Offset+at)
		}
		length := int64(binary.LittleEndian.Uint32(header[4:]))
		if length == 0xFFFFFFFF || at+8+length > ref.Length {
			return nil, fmt.Errorf("redact: the item of the Pixel Data at offset %d is %d bytes and runs past the value",
				ref.Offset+at, length)
		}
		items = append(items, encaps.Range{Offset: ref.Offset + at + 8, Length: length})
		at += 8 + length
	}
	return items, nil
}
