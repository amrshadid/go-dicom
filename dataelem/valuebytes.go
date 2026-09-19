package dataelem

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/tag"
)

// ValueBytes renders a Go value as the bytes of an element with the given VR,
// little endian. The file writer and the network encoder both use it, so a value
// that writes to disk also sends, and one that cannot be represented fails in
// both.
//
// NewDataElement takes an interface{}, and the encoders rendered only []byte
// and string. #87 added string, since building a data set the natural way wrote
// every value empty. Numbers were left behind: Rows set as uint16(64) went out
// from EncodeDataset as nothing, with no error, and filewriter dropped it with a
// warning. An image sent without Rows or Columns is accepted by the peer and
// cannot be displayed.
//
// Accepted, besides []byte and string:
//
//   - any Go integer or float, or a slice of them, for US, SS, UL, SL, UV, SV,
//     FL and FD, and for IS and DS, which hold numbers as text;
//   - the same for OW, OL and OV (16-, 32- and 64-bit words, signed or not,
//     since the image decides which) and OF and OD (float32 and float64): pixel
//     data built as a Go slice;
//   - tag.Tag, or a slice, for AT;
//   - []string for any text VR, joined with the backslash that separates values;
//   - PersonName, or a slice, for PN.
//
// These are also what the decoders in this package return, so a value read can
// be set again. A value that does not fit its VR is an error: 70000 in a US
// would otherwise be written as 4464, a different number rather than a failure.
func ValueBytes(vr VR, value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case []string:
		return []byte(strings.Join(v, `\`)), nil
	case PersonName:
		return []byte(personName(v)), nil
	case []PersonName:
		names := make([]string, len(v))
		for i, name := range v {
			names[i] = personName(name)
		}
		return []byte(strings.Join(names, `\`)), nil
	case tag.Tag:
		return attributeTags(vr, []tag.Tag{v})
	case []tag.Tag:
		return attributeTags(vr, v)
	}

	numbers, ok := numericValues(value)
	if !ok {
		return nil, fmt.Errorf("a %T cannot be written as %s", value, vr)
	}

	switch vr {
	case US, SS, UL, SL, UV, SV, FL, FD, OW, OL, OV, OF, OD:
		return binaryNumbers(vr, numbers)
	case IS:
		return integerStrings(numbers)
	case DS:
		return decimalStrings(numbers)
	}
	return nil, fmt.Errorf("a %T cannot be written as %s: it holds no numbers", value, vr)
}

// number is one Go numeric value, kept in whichever form holds it exactly.
type number struct {
	isFloat bool
	signed  bool
	i       int64
	u       uint64
	f       float64
}

// numericValues flattens a Go number or slice of numbers.
func numericValues(value any) ([]number, bool) {
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice {
		out := make([]number, rv.Len())
		for i := range out {
			n, ok := numericValue(rv.Index(i))
			if !ok {
				return nil, false
			}
			out[i] = n
		}
		return out, true
	}
	n, ok := numericValue(rv)
	if !ok {
		return nil, false
	}
	return []number{n}, true
}

func numericValue(rv reflect.Value) (number, bool) {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return number{signed: true, i: rv.Int()}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return number{u: rv.Uint()}, true
	case reflect.Float32, reflect.Float64:
		return number{isFloat: true, f: rv.Float()}, true
	}
	return number{}, false
}

// integer returns n as a signed or unsigned integer within [min, max], or an
// error naming why it does not fit.
func (n number) integer(vr VR, lo int64, hi uint64) (int64, uint64, error) {
	switch {
	case n.isFloat:
		if n.f != math.Trunc(n.f) || math.IsInf(n.f, 0) || math.IsNaN(n.f) {
			return 0, 0, fmt.Errorf("%v is not a whole number, so it cannot be written as %s", n.f, vr)
		}
		if n.f < float64(lo) || n.f > float64(hi) {
			return 0, 0, fmt.Errorf("%v is out of range for %s", n.f, vr)
		}
		if n.f < 0 {
			return int64(n.f), 0, nil
		}
		return 0, uint64(n.f), nil
	case n.signed:
		if n.i < lo || (n.i > 0 && uint64(n.i) > hi) {
			return 0, 0, fmt.Errorf("%d is out of range for %s", n.i, vr)
		}
		if n.i < 0 {
			return n.i, 0, nil
		}
		return 0, uint64(n.i), nil
	default:
		if n.u > hi {
			return 0, 0, fmt.Errorf("%d is out of range for %s", n.u, vr)
		}
		return 0, n.u, nil
	}
}

func (n number) float() float64 {
	switch {
	case n.isFloat:
		return n.f
	case n.signed:
		return float64(n.i)
	default:
		return float64(n.u)
	}
}

// binaryNumbers writes each value at the VR's width.
func binaryNumbers(vr VR, numbers []number) ([]byte, error) {
	le := binary.LittleEndian
	var out []byte
	for _, n := range numbers {
		switch vr {
		case FL, OF:
			f := n.float()
			if math.Abs(f) > math.MaxFloat32 && !math.IsInf(f, 0) {
				return nil, fmt.Errorf("%v is out of range for %s", f, vr)
			}
			out = le.AppendUint32(out, math.Float32bits(float32(f)))
		case FD, OD:
			out = le.AppendUint64(out, math.Float64bits(n.float()))
		default:
			bits, lo, hi := integerRange(vr)
			i, u, err := n.integer(vr, lo, hi)
			if err != nil {
				return nil, err
			}
			raw := u
			if i < 0 {
				raw = uint64(i)
			}
			switch bits {
			case 16:
				out = le.AppendUint16(out, uint16(raw))
			case 32:
				out = le.AppendUint32(out, uint32(raw))
			default:
				out = le.AppendUint64(out, raw)
			}
		}
	}
	return out, nil
}

// integerRange gives an integer VR's width and the values it holds.
func integerRange(vr VR) (bits int, lo int64, hi uint64) {
	switch vr {
	case US:
		return 16, 0, math.MaxUint16
	case SS:
		return 16, math.MinInt16, math.MaxInt16
	case UL:
		return 32, 0, math.MaxUint32
	case SL:
		return 32, math.MinInt32, math.MaxInt32
	case UV:
		return 64, 0, math.MaxUint64
	// A word holds either sign; which one is the image's to say.
	case OW:
		return 16, math.MinInt16, math.MaxUint16
	case OL:
		return 32, math.MinInt32, math.MaxUint32
	case OV:
		return 64, math.MinInt64, math.MaxUint64
	default: // SV
		return 64, math.MinInt64, math.MaxInt64
	}
}

// integerStrings renders IS values. PS3.5 Table 6.2-1 limits IS to 32-bit
// signed integers.
func integerStrings(numbers []number) ([]byte, error) {
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		signed, unsigned, err := n.integer(IS, math.MinInt32, math.MaxInt32)
		if err != nil {
			return nil, err
		}
		if signed < 0 {
			parts[i] = strconv.FormatInt(signed, 10)
		} else {
			parts[i] = strconv.FormatUint(unsigned, 10)
		}
	}
	return []byte(strings.Join(parts, `\`)), nil
}

// decimalStrings renders DS values in at most 16 characters each, the limit
// PS3.5 Table 6.2-1 sets, dropping precision rather than exceeding it.
func decimalStrings(numbers []number) ([]byte, error) {
	parts := make([]string, len(numbers))
	for i, n := range numbers {
		f := n.float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%v cannot be written as DS", f)
		}
		s := strconv.FormatFloat(f, 'g', -1, 64)
		for precision := 15; len(s) > 16 && precision > 0; precision-- {
			s = strconv.FormatFloat(f, 'g', precision, 64)
		}
		if len(s) > 16 {
			return nil, fmt.Errorf("%v does not fit the 16 characters of DS", f)
		}
		parts[i] = s
	}
	return []byte(strings.Join(parts, `\`)), nil
}

// attributeTags writes AT values: group then element, each little endian.
func attributeTags(vr VR, tags []tag.Tag) ([]byte, error) {
	if vr != AT && vr != "" {
		return nil, fmt.Errorf("a tag cannot be written as %s", vr)
	}
	out := make([]byte, 0, 4*len(tags))
	for _, t := range tags {
		out = binary.LittleEndian.AppendUint16(out, t.Group())
		out = binary.LittleEndian.AppendUint16(out, t.Element())
	}
	return out, nil
}

// personName joins the component groups with "=", dropping empty trailing
// groups (PS3.5 6.2.1).
func personName(pn PersonName) string {
	return strings.TrimRight(pn.Alphabetic+"="+pn.Ideographic+"="+pn.Phonetic, "=")
}
