package tag

import "testing"

// TestRepeaterMatching covers the patterns that stand for a range of tags.
//
// A pattern like "60xx0010" covers every overlay group. Matching was done by
// formatting the tag and comparing up to 88 strings, which ran on every
// dictionary miss — and a miss happens for each private or unrecognized tag, of
// which a real file has many. It was half the parse time of a 39 KB CT image.
// The patterns are now compiled to a value and a mask once.
func TestRepeaterMatching(t *testing.T) {
	d := globalDict

	for _, tc := range []struct {
		name  string
		tag   Tag
		match bool
	}{
		// Overlay group, which is the archetypal repeater.
		{"overlay rows, group 6000", New(0x6000, 0x0010), true},
		{"overlay rows, group 6010", New(0x6010, 0x0010), true},
		{"overlay rows, group 60FE", New(0x60FE, 0x0010), true},

		// A tag whose group is outside the repeating range.
		{"not an overlay group", New(0x6100, 0x0010), false},

		// A standard tag, which resolves directly and never reaches the
		// repeater path.
		{"patient name", New(0x0010, 0x0010), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := d.matchRepeater(tc.tag) != ""
			if got != tc.match {
				t.Errorf("matchRepeater(%s) matched=%v, want %v", tc.tag, got, tc.match)
			}
		})
	}
}

// TestRepeaterLookupIsDeterministic guards the ordering.
//
// Two patterns can match the same tag, and the source is a map, whose iteration
// order Go randomizes per run. Without a stable order the same tag could resolve
// to different dictionary entries in different runs of the same binary — a
// failure that reproduces only sometimes.
func TestRepeaterLookupIsDeterministic(t *testing.T) {
	d := globalDict
	target := New(0x6000, 0x0010)

	first := d.matchRepeater(target)
	for i := 0; i < 200; i++ {
		if got := d.matchRepeater(target); got != first {
			t.Fatalf("matchRepeater returned %q then %q for the same tag", first, got)
		}
	}
}

// TestCompileRepeaterRejectsMalformedPatterns verifies a bad entry is skipped
// rather than compiled into something that matches everything.
//
// A pattern that compiled to a zero mask would match every tag, so one malformed
// dictionary entry would give every unknown tag the wrong VR.
func TestCompileRepeaterRejectsMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{
		"",          // empty
		"60xx001",   // too short
		"60xx00100", // too long
		"60xx001g",  // not hex
		"60xx 0010", // space
	} {
		if _, ok := compileRepeater(pattern); ok {
			t.Errorf("compileRepeater(%q) accepted a malformed pattern", pattern)
		}
	}
}

// TestCompileRepeaterValueAndMask checks the compilation itself, since every
// lookup depends on it and an off-by-one nibble would be invisible in the
// matching tests above.
func TestCompileRepeaterValueAndMask(t *testing.T) {
	c, ok := compileRepeater("60xx0010")
	if !ok {
		t.Fatal("compileRepeater rejected a valid pattern")
	}
	if c.value != 0x60000010 {
		t.Errorf("value = %#08X, want 0x60000010", c.value)
	}
	if c.mask != 0xFF00FFFF {
		t.Errorf("mask = %#08X, want 0xFF00FFFF", c.mask)
	}

	// And the compiled form matches what the pattern describes.
	if uint32(New(0x6042, 0x0010))&c.mask != c.value {
		t.Error("a tag the pattern covers does not match the compiled form")
	}
	if uint32(New(0x6042, 0x0011))&c.mask == c.value {
		t.Error("a tag the pattern does not cover matches the compiled form")
	}
}

// BenchmarkDictionaryMissWithRepeaterScan measures the path that dominated
// parsing: a tag not in the standard dictionary, which falls through to the
// repeater patterns.
func BenchmarkDictionaryMissWithRepeaterScan(b *testing.B) {
	d := globalDict
	// A private tag, which is what a real file has many of.
	private := New(0x0009, 0x1010)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Get(private)
	}
}

// BenchmarkDictionaryHit measures the common case for comparison, so a
// regression in the miss path is visible as a ratio rather than an absolute.
func BenchmarkDictionaryHit(b *testing.B) {
	d := globalDict
	patientName := New(0x0010, 0x0010)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.Get(patientName)
	}
}
