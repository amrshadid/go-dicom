package dataelem_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

// TestTagAcceptsTheFormsCallersUse covers every representation NewDataElement's
// interface{} parameter allows a tag to arrive as.
//
// The parameter is untyped, so a tag can be stored as a tag.Tag, a raw uint32,
// or one of the string forms the dictionary uses. Callers previously asserted
// on tag.Tag alone and discarded the element when the assertion failed, which
// made every other form a silent data loss rather than a type error.
func TestTagAcceptsTheFormsCallersUse(t *testing.T) {
	want := tag.New(0x0008, 0x0060)

	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{"tag.Tag", want},
		{"uint32", uint32(0x00080060)},
		{"parenthesized string", "(0008,0060)"},
		{"bare hex string", "00080060"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			elem := dataelem.NewDataElement(tc.value, dataelem.CS, []byte("MR"))

			got, ok := elem.Tag()
			if !ok {
				t.Fatalf("Tag() reported %v as unreadable", tc.value)
			}
			if got != want {
				t.Errorf("Tag() = %s, want %s", got, want)
			}
			if got := elem.MustTag(); got != want {
				t.Errorf("MustTag() = %s, want %s", got, want)
			}
		})
	}
}

// TestTagReportsWhatItCannotRead verifies an element built with something that
// is not a tag is reported rather than passed off as tag zero.
//
// The distinction matters at the call sites: encoding refuses to send a data set
// with an element omitted, which it can only do if it is told the difference
// between an unreadable tag and a valid (0000,0000).
func TestTagReportsWhatItCannotRead(t *testing.T) {
	for _, value := range []interface{}{
		nil,
		42,
		3.14,
		"not a tag",
		"(00zz,0060)",
		struct{}{},
	} {
		elem := dataelem.NewDataElement(value, dataelem.CS, []byte("MR"))
		if got, ok := elem.Tag(); ok {
			t.Errorf("Tag() accepted %#v as tag %s", value, got)
		}
	}
}

// TestTagZeroIsReadable guards against conflating "unreadable" with "zero".
// (0000,0000) is Command Group Length — a real tag, and one that appears in
// every DIMSE command data set.
func TestTagZeroIsReadable(t *testing.T) {
	elem := dataelem.NewDataElement(tag.New(0x0000, 0x0000), dataelem.UL, []byte{0, 0, 0, 0})

	got, ok := elem.Tag()
	if !ok {
		t.Fatal("Tag() reported (0000,0000) as unreadable; it is Command Group Length")
	}
	if got != tag.New(0x0000, 0x0000) {
		t.Errorf("Tag() = %s, want (0000,0000)", got)
	}
}

// TestTagAfterSetTag verifies the typed accessor reflects a later SetTag, since
// SetTag still takes interface{} and can therefore install any of the forms.
func TestTagAfterSetTag(t *testing.T) {
	elem := dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("MR"))

	elem.SetTag("(0010,0010)")

	got, ok := elem.Tag()
	if !ok {
		t.Fatal("Tag() reported the value set by SetTag as unreadable")
	}
	if want := tag.New(0x0010, 0x0010); got != want {
		t.Errorf("Tag() = %s, want %s", got, want)
	}
}
