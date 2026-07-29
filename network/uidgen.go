package network

import (
	"crypto/rand"
	"math/big"
)

// UUIDDerivedUIDRoot is the OID arc for UIDs derived from a UUID (ITU-T X.667,
// referenced by PS3.5 Annex B.2).
//
// It exists so that an implementation can mint globally unique UIDs without
// owning a registered root. Anything under it is unique by construction rather
// than by registration, which is what makes it safe to use here: a library
// cannot know its user's assigned root, and inventing one under someone else's
// arc would produce UIDs that collide with theirs.
const UUIDDerivedUIDRoot = "2.25"

// GenerateUID returns a new globally unique UID under the UUID-derived arc.
//
// The value is a random 128-bit integer rendered in decimal, which is the
// encoding X.667 defines for that arc. UIDs are limited to 64 characters
// (PS3.5 §9.1); "2.25." plus 39 decimal digits is at most 44, so the result
// always fits.
//
// Callers that own a registered UID root should use it instead — a UID under
// it identifies the equipment that produced the data, which this one does not.
func GenerateUID() string {
	// 2^128, so the value spans the full range X.667 assigns to this arc.
	max := new(big.Int).Lsh(big.NewInt(1), 128)

	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		// crypto/rand fails only if the system entropy source is unavailable,
		// at which point returning a predictable UID would be worse than
		// stopping: two instances would silently share an identity.
		panic("network: cannot generate a UID without system entropy: " + err.Error())
	}

	return UUIDDerivedUIDRoot + "." + n.String()
}
