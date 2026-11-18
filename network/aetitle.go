package network

import "strings"

// maxAETitleLength is the longest an AE title may be (PS3.5 6.2, AE).
const maxAETitleLength = 16

// cleanAETitle returns an AE title without the spaces that are not
// significant in it, leading or trailing (PS3.5 6.2, AE), and whether what is
// left is a well-formed AE value: 1 to 16 characters of the default
// character repertoire, none of them a control character or a backslash.
//
// A title that is not well formed is not one any conforming peer sends, and
// is never compared, logged or written as though it were: a newline or a
// backslash in it would split a log line or a multi-valued element, and one
// that trims to nothing names no one.
func cleanAETitle(s string) (string, bool) {
	s = strings.Trim(s, " ")
	if s == "" || len(s) > maxAETitleLength {
		return s, false
	}
	for i := 0; i < len(s); i++ {
		// The default repertoire is ISO-IR 6, printable ASCII here: an AE
		// title holds no control character (PS3.5 6.1.3) and no backslash,
		// the value delimiter.
		if c := s[i]; c < 0x20 || c > 0x7E || c == '\\' {
			return s, false
		}
	}
	return s, true
}
