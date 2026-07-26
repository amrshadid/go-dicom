package tag_test

import (
	"fmt"

	"github.com/amrshadid/go-dicom/tag"
)

// A tag is a group and element number packed into a uint32. Construct one with
// New and it carries its dictionary entry with it.
func Example() {
	patientName := tag.New(0x0010, 0x0010)

	fmt.Println(patientName)
	fmt.Println(patientName.GetName())
	fmt.Println(patientName.GetVR())

	// Output:
	// (0010,0010)
	// Patient's Name
	// PN
}

// Tags parse from the notations that appear in DICOM tooling and standards
// documents.
func ExampleParseTag() {
	for _, s := range []string{"(0010,0010)", "0x00100010", "00100010"} {
		t, err := tag.ParseTag(s)
		if err != nil {
			fmt.Println("error:", err)
			continue
		}
		fmt.Printf("%-14s -> %s %s\n", s, t, t.GetKeyword())
	}

	// Output:
	// (0010,0010)    -> (0010,0010) PatientName
	// 0x00100010     -> (0010,0010) PatientName
	// 00100010       -> (0010,0010) PatientName
}

// Group and Element unpack the two halves of a tag.
func ExampleTag_Group() {
	t := tag.New(0x0028, 0x0010) // Rows

	fmt.Printf("group=%04X element=%04X\n", t.Group(), t.Element())

	// Output: group=0028 element=0010
}

// Private tags live in odd-numbered groups. Vendors use them for data outside
// the standard dictionary, and they are the usual place for identity to hide
// after a naive de-identification pass.
func ExampleTag_IsPrivate() {
	standard := tag.New(0x0010, 0x0010) // even group
	private := tag.New(0x0029, 0x1010)  // odd group

	fmt.Println(standard.IsPrivate())
	fmt.Println(private.IsPrivate())

	// Output:
	// false
	// true
}

// The dictionary answers whether a tag is known and whether it has been
// retired from the standard.
func ExampleTag_GetInfo() {
	t := tag.New(0x0008, 0x0018) // SOP Instance UID

	info := t.GetInfo()
	if info == nil {
		fmt.Println("not in the dictionary")
		return
	}

	fmt.Println(info.Name)
	fmt.Println(info.VR)
	fmt.Println(info.Retired)

	// Output:
	// SOP Instance UID
	// UI
	// false
}
