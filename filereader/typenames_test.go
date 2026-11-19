package filereader_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/filereader"
)

// The element types are defined in the internal parser and named here by
// alias. The parser's package is named filereader so that the types print as
// they did when they were defined here: a program that prints one with %T or
// %v, or a test that compares an error's text, sees the same names.
func TestTheElementTypesPrintAsBefore(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{&filereader.DataElementValue{}, "*filereader.DataElementValue"},
		{filereader.ElementPosition{}, "filereader.ElementPosition"},
		{filereader.FragmentPosition{}, "filereader.FragmentPosition"},
		{&filereader.SequenceItemValue{}, "*filereader.SequenceItemValue"},
		{&filereader.FileMetaInfo{}, "*filereader.FileMetaInfo"},
		{filereader.ReadOptions{}, "filereader.ReadOptions"},
		{&filereader.ValueTooLongError{}, "*filereader.ValueTooLongError"},
		{&filereader.DICOMFile{}, "*filereader.DICOMFile"},
		{filereader.NewDCMFileReader(nil), "*filereader.DCMFileReader"},
	} {
		if got := fmt.Sprintf("%T", tc.value); got != tc.want {
			t.Errorf("%%T is %s, want %s", got, tc.want)
		}
	}
}
