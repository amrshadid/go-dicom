package dcmstore_test

import (
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// TestStoreMatchesByTheSharedRules: the store matches through package
// matching, so the two rules it did not have before answer here as there. A
// key of more than one value matches any of them (C.2.2.2.8) where it
// matched nothing, and "" asks for the entities with no value (C.2.2.2.7)
// where it was compared as two characters.
func TestStoreMatchesByTheSharedRules(t *testing.T) {
	store := openWithCorpus(t)
	ctx := context.Background()

	cases := []struct {
		level string
		keys  map[tag.Tag]string
		want  int
		why   string
	}{
		{"SERIES", map[tag.Tag]string{tag.New(0x0008, 0x0060): `CT\MR`}, 3, "every series, CT or MR"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0010, 0x0010): `JONES*\NOBODY`}, 1, "one of two names"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0008, 0x0090): `""`}, 2, "no referring physician in either"},
		{"STUDY", map[tag.Tag]string{tag.New(0x0010, 0x0010): `""`}, 0, "both studies have a name"},
	}
	for _, tc := range cases {
		responses, err := store.Query(ctx, query(tc.level, tc.keys))
		if err != nil {
			t.Errorf("%s query (%s): %v", tc.level, tc.why, err)
			continue
		}
		if len(responses) != tc.want {
			t.Errorf("%s query (%s) returned %d responses, want %d",
				tc.level, tc.why, len(responses), tc.want)
		}
	}
}
