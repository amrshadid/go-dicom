package anonymize

import (
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// TestRepeatingGroupRulesTakeEvenGroupsOnly proves the rules the anonymizer
// applies to curve and overlay data match the repeating groups of PS3.5 7.6,
// xx00 to xx1E and even only, and not a private group such as 6001 that
// happens to share the high byte.
func TestRepeatingGroupRulesTakeEvenGroupsOnly(t *testing.T) {
	matched := func(tg tag.Tag) bool {
		for _, rule := range ps315RepeatingGroups {
			if rule.matches(tg) {
				return true
			}
		}
		return false
	}
	for _, tg := range []tag.Tag{tag.New(0x6000, 0x3000), tag.New(0x601E, 0x4000), tag.New(0x5000, 0x0005), tag.New(0x501E, 0x3000)} {
		if !matched(tg) {
			t.Errorf("%s is curve or overlay data and was not matched", tg)
		}
	}
	for _, tg := range []tag.Tag{tag.New(0x6001, 0x3000), tag.New(0x6003, 0x4000), tag.New(0x5001, 0x0005), tag.New(0x6020, 0x3000), tag.New(0x50FF, 0x3000)} {
		if matched(tg) {
			t.Errorf("%s is not in a repeating group and was matched", tg)
		}
	}
}
