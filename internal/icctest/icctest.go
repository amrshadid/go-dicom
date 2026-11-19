// Package icctest makes small, valid ICC profiles for tests, so that no
// test carries a profile whose license is someone else's.
package icctest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Embedded returns the ICC profile a PNG (its iCCP chunk) or a JPEG (its
// APP2 ICC_PROFILE segments, joined in order) carries, nil when it carries
// none. It reads only as far as it needs and checks nothing else.
func Embedded(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		for rest := data[8:]; len(rest) >= 12; {
			n := binary.BigEndian.Uint32(rest)
			if uint64(len(rest)) < 12+uint64(n) {
				return nil, errors.New("a PNG chunk runs past the end")
			}
			switch string(rest[4:8]) {
			case "iCCP":
				_, compressed, ok := bytes.Cut(rest[8:8+n], []byte{0})
				if !ok || len(compressed) < 1 {
					return nil, errors.New("an iCCP chunk without its profile")
				}
				z, err := zlib.NewReader(bytes.NewReader(compressed[1:]))
				if err != nil {
					return nil, err
				}
				return io.ReadAll(z)
			case "IDAT":
				return nil, nil
			}
			rest = rest[12+n:]
		}
		return nil, nil
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		var profile []byte
		for rest := data[2:]; len(rest) >= 4 && rest[0] == 0xFF && rest[1] != 0xDA; {
			n := int(binary.BigEndian.Uint16(rest[2:]))
			if len(rest) < 2+n {
				return nil, errors.New("a JPEG segment runs past the end")
			}
			if segment := rest[4 : 2+n]; rest[1] == 0xE2 && bytes.HasPrefix(segment, []byte("ICC_PROFILE\x00")) {
				profile = append(profile, segment[14:]...)
			}
			rest = rest[2+n:]
		}
		return profile, nil
	}
	return nil, errors.New("neither a PNG nor a JPEG")
}

// Profile returns an ICC version 2 display profile: for "RGB " a matrix
// and gamma profile with sRGB's primaries adapted to D50 and a gamma of
// 2.2, and for "GRAY" a gray profile with that gamma. description is its
// description tag, which makes two profiles differ when a test needs them
// to; one of even length makes a profile of odd length. It is laid out by ICC.1:2001-04: a 128-byte header, a tag table, and
// each tag's data at an offset a multiple of four.
func Profile(space, description string) []byte {
	fixed := func(v float64) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(int32(math.Round(v*65536))))
		return b
	}
	xyz := func(x, y, z float64) []byte {
		out := append([]byte("XYZ \x00\x00\x00\x00"), fixed(x)...)
		return append(append(out, fixed(y)...), fixed(z)...)
	}
	// curveType with one entry, a gamma in u8Fixed8: 2.2 is 0x0233.
	curve := []byte("curv\x00\x00\x00\x00\x00\x00\x00\x01\x02\x33")
	desc := func(text string) []byte {
		var b bytes.Buffer
		b.WriteString("desc\x00\x00\x00\x00")
		_ = binary.Write(&b, binary.BigEndian, uint32(len(text)+1))
		b.WriteString(text)
		b.WriteByte(0)
		b.Write(make([]byte, 4+4+2+1+67)) // no Unicode, no ScriptCode
		return b.Bytes()
	}
	type tagData struct {
		sig  string
		data []byte
	}
	tags := []tagData{
		{"cprt", append([]byte("text\x00\x00\x00\x00"), "No copyright, test data\x00"...)},
		{"wtpt", xyz(0.9642, 1.0, 0.8249)},
	}
	if space == "GRAY" {
		tags = append(tags, tagData{"kTRC", curve})
	} else {
		tags = append(tags,
			tagData{"rXYZ", xyz(0.4361, 0.2225, 0.0139)},
			tagData{"gXYZ", xyz(0.3851, 0.7169, 0.0971)},
			tagData{"bXYZ", xyz(0.1431, 0.0606, 0.7141)},
			tagData{"rTRC", curve}, tagData{"gTRC", curve}, tagData{"bTRC", curve})
	}
	// Last, so that the profile's length follows the description's: odd
	// for one of even length, which a test of padding needs.
	tags = append(tags, tagData{"desc", desc(description)})

	offset := 128 + 4 + 12*len(tags)
	var table, data bytes.Buffer
	_ = binary.Write(&table, binary.BigEndian, uint32(len(tags)))
	for _, t := range tags {
		for (offset+data.Len())%4 != 0 {
			data.WriteByte(0)
		}
		table.WriteString(t.sig)
		_ = binary.Write(&table, binary.BigEndian, uint32(offset+data.Len()))
		_ = binary.Write(&table, binary.BigEndian, uint32(len(t.data)))
		data.Write(t.data)
	}

	header := make([]byte, 128)
	size := 128 + table.Len() + data.Len()
	binary.BigEndian.PutUint32(header[0:], uint32(size))
	binary.BigEndian.PutUint32(header[8:], 0x02100000) // version 2.1
	copy(header[12:], "mntr")
	copy(header[16:], space)
	copy(header[20:], "XYZ ")
	binary.BigEndian.PutUint16(header[24:], 2026)
	binary.BigEndian.PutUint16(header[26:], 1)
	binary.BigEndian.PutUint16(header[28:], 1)
	copy(header[36:], "acsp")
	copy(header[68:], fixed(0.9642))
	copy(header[72:], fixed(1.0))
	copy(header[76:], fixed(0.8249))
	return append(append(header, table.Bytes()...), data.Bytes()...)
}
