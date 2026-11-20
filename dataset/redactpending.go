package dataset

import (
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/encaps"
)

// pendingRedaction is Pixel Data that is left in the file and has regions to
// fill: what dataelem.StreamedValue writes for it, and what a later
// redaction adds to. It is kept in the value's Origin.
//
// Native Pixel Data is copied from the file and filled. Compressed Pixel
// Data is read, decoded and filled a frame at a time, and comes out native.
type pendingRedaction struct {
	ref dataelem.ValueInFile
	vr  dataelem.VR

	// before is the data set as it was read, which describes the stored
	// pixels, infoBefore the image so described; view and infoAfter
	// describe the samples written. They are the same for native pixels.
	before, view *Dataset
	infoBefore   *PixelDataInfo
	infoAfter    *PixelDataInfo

	compression compress.CompressionType
	compressed  bool
	extended    func(io.ReaderAt) ([]uint64, error)

	// fills are the regions to fill, as asked in turn.
	fills []*regionFill
}

// pendingOf returns the redaction an element's value is, or nil.
func pendingOf(elem *dataelem.DataElement) *pendingRedaction {
	if stream, ok := elem.GetValue().(*dataelem.StreamedValue); ok {
		if p, ok := stream.Origin.(*pendingRedaction); ok {
			return p
		}
	}
	return nil
}

// withFill is p with one more fill. It is a copy: a clone of the data set
// shares the value, and filling a region in the clone must not fill it in
// the original.
func (p *pendingRedaction) withFill(fill *regionFill) *pendingRedaction {
	next := *p
	next.fills = append(append([]*regionFill(nil), p.fills...), fill)
	return &next
}

// size is how many bytes of Pixel Data are written.
func (p *pendingRedaction) size() int64 {
	if p.compressed {
		return int64(p.infoAfter.BytesPerFrame) * int64(p.infoAfter.NumberOfFrames)
	}
	return p.ref.Length
}

// apply fills what has been asked for in buf, the bytes of the written
// Pixel Data from byte start on.
func (p *pendingRedaction) apply(buf []byte, start int64) {
	for _, fill := range p.fills {
		fill.apply(buf, start)
	}
}

// stream is the value that writes the Pixel Data, filled.
func (p *pendingRedaction) stream() *dataelem.StreamedValue {
	write := p.copyNative
	if p.compressed {
		write = p.decodeFrames
	}
	return &dataelem.StreamedValue{Length: p.size(), WriteValue: write, Origin: p}
}

// copyNative reads native Pixel Data from the file a chunk at a time, fills
// it, and writes it.
func (p *pendingRedaction) copyNative(src io.ReaderAt, littleEndian bool, w io.Writer) error {
	if src == nil {
		return fmt.Errorf("redact: the Pixel Data is %s and there is no file to read it from", p.ref)
	}
	// The data set holds samples little endian, and the file may not: a
	// sample is reversed, at the width a whole read reverses Pixel Data at,
	// before it is filled.
	swap := 0
	if !littleEndian && dataelem.IsByteOrderSensitive(p.vr) {
		swap = dataelem.PixelDataSwapWidth(p.vr, p.infoBefore.BitsAllocated)
	}
	buf := make([]byte, min(int64(copyChunk), p.ref.Length))
	for done := int64(0); done < p.ref.Length; {
		chunk := buf[:min(int64(len(buf)), p.ref.Length-done)]
		if _, err := src.ReadAt(chunk, p.ref.Offset+done); err != nil {
			return fmt.Errorf("redact: reading Pixel Data at offset %d: %w", p.ref.Offset+done, err)
		}
		dataelem.SwapBytes(chunk, swap)
		p.apply(chunk, done)
		if _, err := w.Write(chunk); err != nil {
			return err
		}
		done += int64(len(chunk))
	}
	return nil
}

// decodeFrames reads each compressed frame from the file, decodes it, fills
// it and writes it, before it reads the next.
func (p *pendingRedaction) decodeFrames(src io.ReaderAt, _ bool, w io.Writer) error {
	if src == nil {
		return fmt.Errorf("redact: the Pixel Data is %s and there is no file to read it from", p.ref)
	}
	items, err := encapsulatedItems(src, p.ref)
	if err != nil {
		return err
	}
	table, err := p.extended(src)
	if err != nil {
		return err
	}
	ranges, err := encaps.FrameRanges(src, items, table, p.infoBefore.NumberOfFrames)
	if err != nil {
		return fmt.Errorf("redact: %w", err)
	}
	perFrame := int64(p.infoAfter.BytesPerFrame)
	for i, parts := range ranges {
		var codestream []byte
		for _, part := range parts {
			at := len(codestream)
			codestream = append(codestream, make([]byte, part.Length)...)
			if _, err := src.ReadAt(codestream[at:], part.Offset); err != nil {
				return fmt.Errorf("redact: reading %s frame %d: %w", p.compression, i, err)
			}
		}
		frame, err := p.decode(i, codestream)
		if err != nil {
			return err
		}
		p.apply(frame, int64(i)*perFrame)
		if _, err := w.Write(frame); err != nil {
			return err
		}
	}
	return nil
}

// decode decodes frame i, counted from 0, from its codestream.
func (p *pendingRedaction) decode(i int, codestream []byte) ([]byte, error) {
	frame, err := decompressPixelFrame(p.compression, codestream, p.infoBefore)
	if err != nil {
		return nil, fmt.Errorf("decompress: failed to decode %s frame %d: %w", p.compression, i, err)
	}
	if want := int64(p.infoAfter.BytesPerFrame); int64(len(frame)) != want {
		return nil, fmt.Errorf("decompress: %s frame %d decodes to %d bytes and the image needs %d",
			p.compression, i, len(frame), want)
	}
	return frame, nil
}

// FramesAsWritten returns a reader of the frames of ds's image as a writer
// given the file will write them, and a data set that describes them, for an
// image whose Pixel Data is left in the file and has had regions filled
// (RedactPixelRegions, DecompressRedactingRegions, and so
// anonymize.Anonymizer.CleanPixelData): frames reads the stored frames, from
// the file, and what is read from the result is filled, and native where the
// stored frames were compressed. The data set is the one to render them
// with, in Dataset.RenderFramesFrom, and to look for text in.
//
// Of any other data set it returns frames and ds themselves. It reads no
// more than a frame at a time, and none until the result is read.
func (ds *Dataset) FramesAsWritten(frames FrameReader) (FrameReader, *Dataset) {
	elem, ok := ds.Get(pixelDataTag)
	if !ok {
		return frames, ds
	}
	p := pendingOf(elem)
	if p == nil {
		return frames, ds
	}
	return &filledFrames{p: p, stored: frames, held: -1}, p.view
}

// filledFrames reads the stored frames and fills them, decoding the ones
// that are compressed.
type filledFrames struct {
	p      *pendingRedaction
	stored FrameReader
	// held is the frame, and decoded its samples, last decoded, so that the
	// bands of a frame are not each a decode of all of it.
	held    int
	decoded []byte
}

// ReadFrame returns frame i, counted from 0, filled.
func (r *filledFrames) ReadFrame(i int) ([]byte, error) {
	if !r.p.compressed {
		frame, err := r.stored.ReadFrame(i)
		if err != nil {
			return nil, err
		}
		r.p.apply(frame, int64(i)*int64(r.p.infoAfter.BytesPerFrame))
		return frame, nil
	}
	frame, err := r.decodeFrame(i)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), frame...), nil
}

// ReadFrameBytes reads len(p) bytes of frame i, counted from 0, from byte
// offset within it, filled.
func (r *filledFrames) ReadFrameBytes(i int, offset int64, p []byte) error {
	perFrame := int64(r.p.infoAfter.BytesPerFrame)
	if !r.p.compressed {
		if err := r.stored.ReadFrameBytes(i, offset, p); err != nil {
			return err
		}
		r.p.apply(p, int64(i)*perFrame+offset)
		return nil
	}
	frame, err := r.decodeFrame(i)
	if err != nil {
		return err
	}
	if offset < 0 || offset+int64(len(p)) > int64(len(frame)) {
		return fmt.Errorf("bytes %d to %d of frame %d, which has %d", offset, offset+int64(len(p)), i+1, len(frame))
	}
	copy(p, frame[offset:])
	return nil
}

// decodeFrame reads frame i, decodes it and fills it, or returns it as it
// was last done.
func (r *filledFrames) decodeFrame(i int) ([]byte, error) {
	if r.held == i {
		return r.decoded, nil
	}
	codestream, err := r.stored.ReadFrame(i)
	if err != nil {
		return nil, err
	}
	frame, err := r.p.decode(i, codestream)
	if err != nil {
		return nil, err
	}
	r.p.apply(frame, int64(i)*int64(r.p.infoAfter.BytesPerFrame))
	r.held, r.decoded = i, frame
	return frame, nil
}

// cloneWithoutPixelData is Clone without Pixel Data, for the copy of a data
// set that only has to say what the image is: a clone holds its own copy of
// the pixels, which for a compressed image held in memory is a copy of all of
// them.
func (ds *Dataset) cloneWithoutPixelData() *Dataset {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	cloned := NewDataset()
	cloned.transferSyntaxUID = ds.transferSyntaxUID
	for _, t := range ds.order {
		if t == uint32(pixelDataTag) {
			continue
		}
		elem := ds.elements[t]
		cloned.elements[t] = dataelem.NewDataElement(elem.GetTag(), elem.GetVR(), cloneValue(elem.GetValue()))
		cloned.order = append(cloned.order, t)
	}
	return cloned
}
