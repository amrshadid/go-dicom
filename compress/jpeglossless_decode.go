package compress

import (
	"encoding/binary"
	"fmt"
)

// maxLosslessPixels bounds the frame a header may declare, so a corrupt or
// hostile SOF3 cannot ask for an allocation the machine cannot serve.
//
// 2^25 samples is 8192x4096, past digital mammography and the largest CR
// plates. It bounds work as well as memory: a frame this size claimed by a
// handful of bytes would otherwise be allocated and walked before the missing
// entropy data was noticed.
const maxLosslessPixels = 1 << 25

// maxTrailingSlack is how many bytes past the end of the entropy-coded data a
// valid frame may appear to need.
//
// The bit reader keeps a 32-bit window filled, so it reads ahead of what it
// consumes, and an encoder stops emitting once the final sample is unambiguous
// rather than padding to a byte boundary the decoder might look past. Four bytes
// covers the window; anything beyond it means samples were decoded from bits
// that were never written.
const maxTrailingSlack = 4

// decodeLosslessJPEG parses a lossless JPEG stream and decodes its scans.
func decodeLosslessJPEG(data []byte) (*losslessImage, error) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != markerSOI {
		return nil, fmt.Errorf("%w: missing SOI", errNotLosslessJPEG)
	}

	var (
		img          *losslessImage
		dcTables     [4]*huffTable
		restart      int
		pos          = 2
		decodedScans int
	)

	for pos+1 < len(data) {
		if data[pos] != 0xFF {
			pos++
			continue
		}
		marker := data[pos+1]
		pos += 2

		switch marker {
		case 0xFF: // fill byte, the next byte is the real marker
			pos--
			continue
		case markerSOI, markerEOI, markerTEM:
			continue
		}
		if marker >= markerRST0 && marker <= markerRST7 {
			continue
		}

		if pos+2 > len(data) {
			return nil, fmt.Errorf("jpeglossless: segment 0x%02X has no length", marker)
		}
		segLen := int(binary.BigEndian.Uint16(data[pos:]))
		if segLen < 2 || pos+segLen > len(data) {
			return nil, fmt.Errorf("jpeglossless: segment 0x%02X declares %d bytes, %d remain",
				marker, segLen, len(data)-pos)
		}
		body := data[pos+2 : pos+segLen]

		switch marker {
		case markerSOF3:
			var err error
			if img, err = parseLosslessFrameHeader(body); err != nil {
				return nil, err
			}
			pos += segLen

		case markerSOF0, markerSOF1, markerSOF5, markerSOF7:
			return nil, fmt.Errorf("%w: frame marker 0x%02X is not lossless Huffman (SOF3)",
				errNotLosslessJPEG, marker)

		case markerDHT:
			if err := parseHuffmanTables(body, &dcTables); err != nil {
				return nil, err
			}
			pos += segLen

		case markerDRI:
			if len(body) < 2 {
				return nil, fmt.Errorf("jpeglossless: DRI segment is %d bytes, want 2", len(body))
			}
			restart = int(binary.BigEndian.Uint16(body))
			pos += segLen

		case markerSOS:
			if img == nil {
				return nil, fmt.Errorf("jpeglossless: scan before any frame header")
			}
			scanned, err := decodeLosslessScan(img, body, data[pos+segLen:], &dcTables, restart)
			if err != nil {
				return nil, err
			}
			decodedScans++
			pos += segLen + scanned

		default:
			pos += segLen
		}
	}

	if img == nil {
		return nil, fmt.Errorf("%w: no SOF3 frame header", errNotLosslessJPEG)
	}
	if decodedScans == 0 {
		return nil, fmt.Errorf("jpeglossless: frame carries no scan")
	}
	return img, nil
}

// parseLosslessFrameHeader reads an SOF3 segment.
func parseLosslessFrameHeader(body []byte) (*losslessImage, error) {
	if len(body) < 6 {
		return nil, fmt.Errorf("jpeglossless: SOF3 header is %d bytes, want at least 6", len(body))
	}

	precision := int(body[0])
	height := int(binary.BigEndian.Uint16(body[1:]))
	width := int(binary.BigEndian.Uint16(body[3:]))
	numComponents := int(body[5])

	if precision < 2 || precision > 16 {
		return nil, fmt.Errorf("jpeglossless: precision %d is outside the 2..16 the standard allows", precision)
	}
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("jpeglossless: frame is %dx%d", width, height)
	}
	if numComponents == 0 {
		return nil, fmt.Errorf("jpeglossless: frame declares no components")
	}
	if width*height > maxLosslessPixels/max(numComponents, 1) {
		return nil, fmt.Errorf("jpeglossless: frame of %dx%d with %d components is too large to decode",
			width, height, numComponents)
	}
	if len(body) < 6+numComponents*3 {
		return nil, fmt.Errorf("jpeglossless: SOF3 declares %d components but carries %d bytes",
			numComponents, len(body)-6)
	}

	img := &losslessImage{width: width, height: height, precision: precision}
	for i := 0; i < numComponents; i++ {
		off := 6 + i*3
		h := int(body[off+1] >> 4)
		v := int(body[off+1] & 0x0F)
		if h != 1 || v != 1 {
			// Subsampling in a lossless frame would discard samples, which is a
			// contradiction; no DICOM encoder produces it.
			return nil, fmt.Errorf("jpeglossless: component %d has sampling factors %dx%d, "+
				"only 1x1 is supported", i, h, v)
		}
		img.components = append(img.components, &losslessComponent{
			id:       body[off],
			h:        h,
			v:        v,
			rowWidth: width,
			rows:     height,
			samples:  make([]int, width*height),
		})
	}
	return img, nil
}

// parseHuffmanTables reads a DHT segment, which may carry several tables.
func parseHuffmanTables(body []byte, tables *[4]*huffTable) error {
	for len(body) > 0 {
		if len(body) < 17 {
			return fmt.Errorf("jpeglossless: DHT entry is %d bytes, want at least 17", len(body))
		}
		class := body[0] >> 4
		id := body[0] & 0x0F
		if id > 3 {
			return fmt.Errorf("jpeglossless: Huffman table id %d is outside 0..3", id)
		}

		counts := body[1:17]
		total := 0
		for _, c := range counts {
			total += int(c)
		}
		if len(body) < 17+total {
			return fmt.Errorf("jpeglossless: Huffman table declares %d values but %d bytes remain",
				total, len(body)-17)
		}

		table, err := buildHuffTable(counts, body[17:17+total])
		if err != nil {
			return err
		}
		// Lossless coding uses only the DC class; an AC table would belong to a
		// DCT process and cannot appear in a frame this decoder accepts.
		if class == 0 {
			tables[id] = table
		}
		body = body[17+total:]
	}
	return nil
}

// decodeLosslessScan decodes one scan and returns how many bytes of
// entropy-coded data it consumed.
func decodeLosslessScan(img *losslessImage, header, entropy []byte,
	tables *[4]*huffTable, restart int) (int, error) {

	if len(header) < 1 {
		return 0, fmt.Errorf("jpeglossless: empty SOS header")
	}
	ns := int(header[0])
	if ns < 1 || ns > len(img.components) {
		return 0, fmt.Errorf("jpeglossless: scan names %d components, frame has %d",
			ns, len(img.components))
	}
	if len(header) < 1+ns*2+3 {
		return 0, fmt.Errorf("jpeglossless: SOS header is %d bytes, too short for %d components",
			len(header), ns)
	}

	scanComps := make([]*losslessComponent, 0, ns)
	for i := 0; i < ns; i++ {
		id := header[1+i*2]

		// The table selector is four bits, so it can name a table beyond the
		// four the standard defines. Checked before it indexes anything: a
		// value of 4 or more read straight into a fixed-size array is a panic
		// on input a file supplies.
		td := header[1+i*2+1] >> 4
		if int(td) >= len(tables) {
			return 0, fmt.Errorf("jpeglossless: scan names Huffman table %d, outside the 0..%d defined",
				td, len(tables)-1)
		}

		var comp *losslessComponent
		for _, c := range img.components {
			if c.id == id {
				comp = c
				break
			}
		}
		if comp == nil {
			return 0, fmt.Errorf("jpeglossless: scan names component %d, which the frame does not declare", id)
		}
		if tables[td] == nil {
			return 0, fmt.Errorf("jpeglossless: scan uses Huffman table %d, which was never defined", td)
		}
		comp.dcTable = int(td)
		scanComps = append(scanComps, comp)
	}

	// Ss carries the predictor selection value in a lossless scan, where a DCT
	// process would put the spectral selection start.
	predictor := int(header[1+ns*2])
	pointTransform := int(header[1+ns*2+2] & 0x0F)

	if predictor > 7 {
		return 0, fmt.Errorf("jpeglossless: predictor %d is outside the 0..7 the standard defines", predictor)
	}
	// T.81 requires 0 <= Pt <= P-1. The field is four bits, so a file can name a
	// transform larger than the precision, which would leave the default
	// prediction shifted by a negative amount.
	if pointTransform >= img.precision {
		return 0, fmt.Errorf("jpeglossless: point transform %d is not below the precision %d",
			pointTransform, img.precision)
	}

	reader := &bitReader{data: entropy}
	if err := decodeLosslessSamples(img, scanComps, tables, reader,
		predictor, pointTransform, restart); err != nil {
		return 0, err
	}

	// A frame that needed more bytes than it carried was truncated. A few bytes
	// of slack is normal — the encoder stops once the last sample is
	// unambiguous, and the reader looks ahead of what it strictly needs.
	if reader.overrun > maxTrailingSlack {
		return 0, fmt.Errorf("jpeglossless: entropy-coded data ends %d bytes early; "+
			"the frame is truncated and the samples past that point would be invented",
			reader.overrun)
	}
	return reader.pos, nil
}

// decodeLosslessSamples runs the prediction loop over a scan.
//
// Components in one scan are interleaved sample by sample, which with the 1x1
// sampling this decoder accepts means plain raster order across the row. A scan
// naming a single component covers that component alone, which is how encoders
// that write one scan per color plane lay a frame out.
func decodeLosslessSamples(img *losslessImage, comps []*losslessComponent,
	tables *[4]*huffTable, r *bitReader, predictor, pointTransform, restart int) error {

	// The value predicted for the very first sample of the scan, and again
	// after each restart: T.81 H.1.2.2.
	defaultPrediction := 1 << (img.precision - pointTransform - 1)

	width, height := img.width, img.height
	sinceRestart := 0

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			for _, comp := range comps {
				diff, err := decodeDifference(r, tables[comp.dcTable])
				if err != nil {
					return fmt.Errorf("at sample (%d,%d) of component %d: %w", x, y, comp.id, err)
				}

				prediction := predictSample(comp, x, y, width, predictor, defaultPrediction, sinceRestart == 0)

				// T.81 H.1.2.1: the sum is taken modulo 2^16 regardless of the
				// declared precision, so an encoder that lets a difference wrap
				// is decoded the way it was encoded.
				value := (prediction + int(diff)) & 0xFFFF
				comp.samples[y*width+x] = value
			}

			sinceRestart++
			atLastSample := y == height-1 && x == width-1
			if restart > 0 && sinceRestart == restart && !atLastSample {
				r.reset()
				sinceRestart = 0
			}
		}
	}
	return nil
}

// decodeDifference reads one Huffman-coded difference.
func decodeDifference(r *bitReader, table *huffTable) (int32, error) {
	category, err := r.decodeHuff(table)
	if err != nil {
		return 0, err
	}
	switch {
	case category == 0:
		return 0, nil
	case category == 16:
		// T.81 table H.2: category 16 stands for a difference of 32768 and
		// carries no additional bits.
		return 32768, nil
	case category > 16:
		return 0, fmt.Errorf("jpeglossless: difference category %d is outside 0..16", category)
	}
	return extend(r.readBits(int(category)), int(category)), nil
}

// predictSample computes the prediction for one sample.
//
// The edges are special-cased before the selection value is consulted, per
// T.81 H.1.2.1: the first sample of the scan has no neighbors at all, the rest
// of the first row can only look left, and the first sample of each later row
// can only look up.
func predictSample(comp *losslessComponent, x, y, width, predictor, defaultPrediction int,
	atRestart bool) int {

	if atRestart && x == 0 && y == 0 {
		return defaultPrediction
	}
	if y == 0 {
		if x == 0 {
			return defaultPrediction
		}
		return comp.samples[x-1] // Ra
	}
	if x == 0 {
		return comp.samples[(y-1)*width] // Rb
	}

	ra := comp.samples[y*width+x-1]
	rb := comp.samples[(y-1)*width+x]
	rc := comp.samples[(y-1)*width+x-1]

	switch predictor {
	case 0:
		// Only valid in a hierarchical frame, where the prediction comes from
		// the reference image rather than from neighbors.
		return 0
	case 1:
		return ra
	case 2:
		return rb
	case 3:
		return rc
	case 4:
		return ra + rb - rc
	case 5:
		return ra + ((rb - rc) >> 1)
	case 6:
		return rb + ((ra - rc) >> 1)
	case 7:
		return (ra + rb) / 2
	}
	return ra
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
