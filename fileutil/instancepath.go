package fileutil

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amrshadid/go-dicom/uid"
)

// MaxUIDLength is the longest a UID may be, from PS3.5 9.1.
//
// Enforced rather than assumed: a UID reaches this code from a peer, and a
// filesystem has its own limit on a path component that a longer one would hit
// with an error far from the cause.
const MaxUIDLength = 64

// InstanceFilePath returns the path to write an instance to within dir, named
// after its SOP Instance UID.
//
// The UID is validated before it becomes a path. It arrives from a network peer —
// in the C-STORE command set or data set — and joining it into a path unchecked
// lets the peer choose where the file goes:
//
//	filepath.Join("./received", "../../etc/cron.d/pwn"+".dcm")
//	  => "../etc/cron.d/pwn.dcm"
//
// filepath.Join cleans a path but does not confine it, so the ".." segments
// survive and the write lands outside dir. A UID is dotted decimal (PS3.5 9.1) —
// digits and dots, nothing else — so anything that could traverse a directory is
// not a UID at all, and is refused here rather than sanitized into something
// that looks like one.
//
// The result is checked against dir afterwards as well. That is redundant given
// the validation, and deliberately so: it is the check that still holds if the
// validation is ever loosened.
func InstanceFilePath(dir, sopInstanceUID string) (string, error) {
	if err := ValidateUIDForPath(sopInstanceUID); err != nil {
		return "", err
	}

	path := filepath.Join(dir, sopInstanceUID+".dcm")

	// Confirm the result is under dir. filepath.Join has already cleaned the
	// path, so a traversal would show up as the result not being prefixed by it.
	within, err := isWithin(dir, path)
	if err != nil {
		return "", err
	}
	if !within {
		return "", fmt.Errorf("SOP Instance UID %q produces the path %q, which is outside %q",
			sopInstanceUID, path, dir)
	}

	return path, nil
}

// ValidateUIDForPath reports whether a UID is safe to use as a path component.
//
// It is stricter than uid.UID.IsValid in one way that matters here: the length
// limit is enforced. Callers writing files named after a UID should use this
// rather than checking the format themselves.
func ValidateUIDForPath(value string) error {
	if value == "" {
		return fmt.Errorf("the SOP Instance UID is empty, so there is nothing to name the file after")
	}
	if len(value) > MaxUIDLength {
		return fmt.Errorf("the SOP Instance UID is %d characters and PS3.5 9.1 allows %d: %q",
			len(value), MaxUIDLength, value)
	}
	if !uid.New(value).IsValid() {
		return fmt.Errorf("%q is not a valid UID — a UID is dotted decimal, digits and "+
			"dots only, so this cannot be used to name a file", value)
	}
	return nil
}

// isWithin reports whether path is dir or is contained by it.
func isWithin(dir, path string) (bool, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false, fmt.Errorf("resolving %q: %w", dir, err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("resolving %q: %w", path, err)
	}

	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false, fmt.Errorf("comparing %q with %q: %w", absPath, absDir, err)
	}

	// Rel gives a path starting with ".." when the target is outside the base.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
