package dataelem

import (
	"bytes"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// TestValueBytesRendersGoValues covers #119. A value set in Go as a number was
// written as nothing: network.EncodeDataset sent the element empty and reported
// no error, and filewriter dropped it with a warning. Each case is the Go value a
// caller would reach for, and the bytes the standard gives it (little endian;
// the writers swap for big endian).
func TestValueBytesRendersGoValues(t *testing.T) {
	tests := []struct {
		name  string
		vr    VR
		value any
		want  []byte
	}{
		{"Rows as uint16", US, uint16(64), []byte{64, 0}},
		{"Rows as int, as a decoder returns it", US, 512, []byte{0, 2}},
		{"US multi-valued", US, []uint16{1, 2}, []byte{1, 0, 2, 0}},
		{"US from []int", US, []int{1, 65535}, []byte{1, 0, 0xFF, 0xFF}},
		{"SS negative", SS, -2, []byte{0xFE, 0xFF}},
		{"UL", UL, uint32(0x01020304), []byte{4, 3, 2, 1}},
		{"SL negative", SL, int32(-1), []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{"UV", UV, uint64(1), []byte{1, 0, 0, 0, 0, 0, 0, 0}},
		{"SV", SV, int64(-1), []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
		{"FL", FL, float32(1), []byte{0, 0, 0x80, 0x3F}},
		{"FL from float64", FL, 1.0, []byte{0, 0, 0x80, 0x3F}},
		{"FD", FD, 1.0, []byte{0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
		{"FD multi-valued", FD, []float64{0, 1}, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
		{"IS", IS, 42, []byte("42")},
		{"IS multi-valued", IS, []int{1, -2}, []byte(`1\-2`)},
		{"DS", DS, 0.5, []byte("0.5")},
		{"DS multi-valued", DS, []float64{1.25, 2}, []byte(`1.25\2`)},
		{"DS kept within 16 characters", DS, 1.0 / 3.0, []byte("0.33333333333333")},
		{"AT", AT, tag.New(0x0028, 0x0010), []byte{0x28, 0x00, 0x10, 0x00}},
		{"AT multi-valued", AT, []tag.Tag{tag.New(0x0010, 0x0010), tag.New(0x0010, 0x0020)},
			[]byte{0x10, 0x00, 0x10, 0x00, 0x10, 0x00, 0x20, 0x00}},
		{"text multi-valued", CS, []string{"ORIGINAL", "PRIMARY"}, []byte(`ORIGINAL\PRIMARY`)},
		{"person name", PN, PersonName{Alphabetic: "Doe^Jane"}, []byte("Doe^Jane")},
		{"person name, three groups", PN, PersonName{Alphabetic: "Yamada^Tarou", Ideographic: "山田^太郎"}, []byte("Yamada^Tarou=山田^太郎")},
		{"OW pixels, signed", OW, []int16{-2, 1}, []byte{0xFE, 0xFF, 1, 0}},
		{"OW pixels, unsigned", OW, []uint16{65535}, []byte{0xFF, 0xFF}},
		{"OL", OL, []uint32{1}, []byte{1, 0, 0, 0}},
		{"OF", OF, []float32{1}, []byte{0, 0, 0x80, 0x3F}},
		{"OD", OD, []float64{1}, []byte{0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
		{"bytes pass through", OB, []byte{1, 2, 3}, []byte{1, 2, 3}},
		{"string passes through", LO, "text", []byte("text")},
		{"nil is empty", US, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValueBytes(tc.vr, tc.value)
			if err != nil {
				t.Fatalf("ValueBytes(%s, %#v): %v", tc.vr, tc.value, err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("ValueBytes(%s, %#v) = % x (%q), want % x (%q)",
					tc.vr, tc.value, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestValueBytesRefusesWhatItCannotRepresent: a value that does not fit is an
// error, never a wrapped number or an empty element. 70000 in a US would be
// written as 4464, which is a different image size, not a failure anyone sees.
func TestValueBytesRefusesWhatItCannotRepresent(t *testing.T) {
	tests := []struct {
		name  string
		vr    VR
		value any
	}{
		{"US overflow", US, 70000},
		{"OW overflow", OW, []int{70000}},
		{"US negative", US, -1},
		{"SS overflow", SS, 40000},
		{"UL negative", UL, int64(-1)},
		{"a fraction in an integer VR", US, 1.5},
		{"a fraction in IS", IS, 2.5},
		{"a number in a date", DA, 20240101},
		{"an unknown type", US, struct{}{}},
		{"FL overflow", FL, 1e300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ValueBytes(tc.vr, tc.value); err == nil {
				t.Errorf("ValueBytes(%s, %#v) = % x, want an error", tc.vr, tc.value, got)
			}
		})
	}
}

// TestValueBytesIsTheInverseOfDecoding: what the library returns for a value is
// what it must accept back, or reading a value and setting it again loses it.
func TestValueBytesIsTheInverseOfDecoding(t *testing.T) {
	for _, tc := range []struct {
		vr  VR
		raw []byte
	}{
		{US, []byte{64, 0}},
		{US, []byte{1, 0, 2, 0}},
		{SS, []byte{0xFE, 0xFF}},
		{UL, []byte{4, 3, 2, 1}},
		{FL, []byte{0, 0, 0x80, 0x3F}},
		{FD, []byte{0, 0, 0, 0, 0, 0, 0xF0, 0x3F}},
		{IS, []byte(`1\-2`)},
		{DS, []byte(`1.25\2`)},
		{AT, []byte{0x28, 0x00, 0x10, 0x00}},
		{CS, []byte(`ORIGINAL\PRIMARY`)},
	} {
		value, _, err := convertValueByVR(tc.vr, tc.raw, true)
		if err != nil {
			t.Fatalf("%s: decoding: %v", tc.vr, err)
		}
		back, err := ValueBytes(tc.vr, value)
		if err != nil {
			t.Errorf("%s: %#v, as decoded, cannot be encoded: %v", tc.vr, value, err)
			continue
		}
		if !bytes.Equal(back, tc.raw) {
			t.Errorf("%s: decoded %#v and encoded it as % x, want % x", tc.vr, value, back, tc.raw)
		}
	}
}
