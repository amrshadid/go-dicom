// Package heldvalue rebuilds a data set the way an application builds one in
// memory, for tests that require a package to answer the same for it as for
// the data set read from a file.
//
// A data set read from a file holds every value as []byte. dataelem's
// NewDataElement takes any value, and code that asserted []byte read a value
// held any other way as absent (#420). The tests that guard against that need
// the same data set held the other way, and this is the one place that builds
// it.
package heldvalue

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// AsGoValues returns a copy of ds with each value held as the Go type a
// program would reach for: text as a string, or a []string when it has more
// than one value; US, SS, UL, SL, FL and FD as a Go number, or a slice of
// them; AT as a tag.Tag or []tag.Tag. Sequences are rebuilt item by item.
// Everything else is kept as it is held.
//
// The bytes each value stands for are unchanged, padding included, so the
// copy and ds are one data set held two ways. Numbers are taken as little
// endian, as dataelem.ValueBytes writes them, so a value round-trips whatever
// order the file was in. A number is converted only when the element's own VR
// says it is one: an element read as UN keeps its bytes, since ValueBytes
// cannot render a Go number as UN.
func AsGoValues(ds *dataset.Dataset) *dataset.Dataset {
	out := dataset.NewDataset()
	// The syntax the file was in is the data set's, not an element's.
	out.SetTransferSyntaxUID(ds.TransferSyntaxUID())
	for _, elem := range ds.GetAll() {
		t, ok := elem.Tag()
		if !ok {
			continue
		}
		value := elem.GetValue()
		switch v := value.(type) {
		case *sequence.Sequence:
			seq := sequence.New()
			for _, item := range v.Items() {
				if inner, isDS := item.(*dataset.Dataset); isDS {
					_ = seq.Append(AsGoValues(inner))
				}
			}
			value = seq
		case []byte:
			value = goValue(elem.GetVR(), ds.ResolveVR(t, elem), v)
		}
		_ = out.Add(dataelem.NewDataElement(t, elem.GetVR(), value))
	}
	return out
}

// goValue is raw held as a Go value of its VR, or raw itself.
func goValue(own, resolved dataelem.VR, raw []byte) any {
	if dataelem.IsTextVR(resolved) {
		if strings.Contains(string(raw), `\`) {
			return strings.Split(string(raw), `\`)
		}
		return string(raw)
	}
	size := map[dataelem.VR]int{dataelem.US: 2, dataelem.SS: 2, dataelem.UL: 4, dataelem.SL: 4,
		dataelem.FL: 4, dataelem.FD: 8, dataelem.AT: 4}[own]
	if size == 0 || len(raw) == 0 || len(raw)%size != 0 {
		return raw
	}
	n := len(raw) / size
	switch own {
	case dataelem.US:
		return numbers(n, raw, func(b []byte) uint16 { return binary.LittleEndian.Uint16(b) })
	case dataelem.SS:
		return numbers(n, raw, func(b []byte) int16 { return int16(binary.LittleEndian.Uint16(b)) })
	case dataelem.UL:
		return numbers(n, raw, func(b []byte) uint32 { return binary.LittleEndian.Uint32(b) })
	case dataelem.SL:
		return numbers(n, raw, func(b []byte) int32 { return int32(binary.LittleEndian.Uint32(b)) })
	case dataelem.FL:
		return numbers(n, raw, func(b []byte) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b)) })
	case dataelem.FD:
		return numbers(n, raw, func(b []byte) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(b)) })
	case dataelem.AT:
		return numbers(n, raw, func(b []byte) tag.Tag {
			return tag.New(binary.LittleEndian.Uint16(b), binary.LittleEndian.Uint16(b[2:]))
		})
	}
	return raw
}

// numbers decodes n values of one size, as a single value when n is 1.
func numbers[T any](n int, raw []byte, decode func([]byte) T) any {
	size := len(raw) / n
	if n == 1 {
		return decode(raw)
	}
	out := make([]T, n)
	for i := range out {
		out[i] = decode(raw[i*size:])
	}
	return out
}
