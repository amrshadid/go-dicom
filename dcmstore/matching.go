package dcmstore

import (
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/internal/attrmatch"
)

// Attribute matching from PS3.4 C.2.2.2 is package matching's, built on the
// value rules of internal/attrmatch: the same rules decide what this store
// answers and what a worklist or any other caller matching a data set answers,
// so a fix to one is a fix to both. What stays here is the index the rules
// are run against, in query.go and sequences.go. These names are kept so the
// store reads as it did; each is the shared rule, not a copy of it.

// matchValue reports whether stored satisfies the query value for a VR.
func matchValue(vr dataelem.VR, query, stored string) bool {
	return attrmatch.MatchValue(vr, query, stored)
}

// matchWildcard implements wildcard matching: "*" for zero or more characters,
// "?" for exactly one.
func matchWildcard(patternText, valueText string) bool {
	return attrmatch.MatchWildcard(patternText, valueText)
}

// normalizeTemporal expands a DA, TM or DT value to a fixed-width string that
// compares correctly with others of the same VR, padding missing fields with
// their maximum when toUpper is set.
func normalizeTemporal(vr dataelem.VR, value string, toUpper bool) (string, bool) {
	return attrmatch.NormalizeTemporal(vr, value, toUpper)
}

// trimPadding removes the padding a DICOM value may carry.
func trimPadding(s string) string {
	return attrmatch.TrimPadding(s)
}
