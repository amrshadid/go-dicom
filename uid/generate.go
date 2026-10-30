package uid

import (
	"crypto/rand"
	"math/big"
)

// UUIDDerivedRoot is the OID arc for UIDs derived from a UUID (ITU-T X.667,
// referenced by PS3.5 Annex B.2). Anything under it is unique by construction
// rather than by registration, so an implementation can mint UIDs without
// owning a root, and without inventing one under someone else's arc.
const UUIDDerivedRoot = "2.25"

// Generate returns a new globally unique UID under the UUID-derived arc: a
// random 128-bit integer in decimal, the encoding X.667 defines for it.
// "2.25." and 39 digits is at most 44 characters, within the 64 that PS3.5
// 9.1 allows.
//
// Callers that own a registered UID root should use it instead: a UID under
// it identifies the equipment that produced the data, which this one does not.
func Generate() string {
	// 2^128, so the value spans the full range X.667 assigns to this arc.
	max := new(big.Int).Lsh(big.NewInt(1), 128)

	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		// crypto/rand fails only if the system entropy source is unavailable,
		// at which point returning a predictable UID would be worse than
		// stopping: two instances would silently share an identity.
		panic("uid: cannot generate a UID without system entropy: " + err.Error())
	}
	return UUIDDerivedRoot + "." + n.String()
}
