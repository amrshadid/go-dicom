// Package matching decides whether a C-FIND query identifier matches a data
// set, and builds the answer a match is sent as, by the rules of PS3.4
// C.2.2.2.
//
// A query is a data set of keys. [Compile] reads one once, and the
// [Identifier] it returns is matched against as many candidates as there are:
//
//	id, err := matching.Compile(query, matching.Options{})
//	if err != nil {
//		return err
//	}
//	for _, entry := range worklist {
//		if !id.MatchDataset(entry) {
//			continue
//		}
//		answer, err := matching.Response(query, entry, matching.Options{})
//		...
//	}
//
// It is the matching the archive in package dcmstore answers with, run there
// against its index through [Attributes] rather than against a data set, so
// both answer one query the same way.
//
// # The rules
//
//   - Universal (C.2.2.2.3): a key with no value matches every candidate, and
//     is returned.
//   - Single value (C.2.2.2.1): equality, with the padding PS3.5 6.2 permits
//     ignored, and dates and times compared at whatever precision each was
//     written to. A person name is matched whatever its case, so "smith*"
//     finds SMITH^JOHN, as C.2.2.2.1 allows; every other VR is matched as
//     written, and accents are kept: Jerome is not Jérôme.
//   - List of UID (C.2.2.2.2): a backslash-separated list of UIDs.
//   - Wildcard (C.2.2.2.4): "*" and "?" for AE, CS, LO, LT, PN, SH, ST, UC and
//     UT, by character, not by byte. Elsewhere they are literal: a "*" in a UI
//     is a "*".
//   - Range (C.2.2.2.5): "lower-upper" for DA, TM and DT, either end optional,
//     both inclusive. A partial bound covers the period it names, so
//     "2024-2024" is the whole year. A date written 1997.04.24 or a time
//     written 09:30:00, the forms of ACR-NEMA, is read as the date or time it
//     is.
//   - Sequence (C.2.2.2.6): a sequence key's one item holds keys to match
//     against the items of the candidate's sequence, and the candidate
//     matches when one of its items meets every one of them. They must be met
//     by the same item: a step scheduled at station A on Monday and another at
//     station B on Tuesday does not match a query for station A on Tuesday.
//     This is how a Modality Worklist query finds its scheduled step (PS3.4
//     K.6.1.2.2). A sequence key with no item, or an item of universal keys
//     only, is universal matching.
//   - Empty value (C.2.2.2.7): a value of "" (two quotation marks) for a key
//     of VR AE, CS, DA, DT, LO, LT, PN, SH, ST, TM, UC, UR or UT matches a
//     candidate where the attribute is empty or absent.
//   - Multiple values (C.2.2.2.8): a key of more than one value matches a
//     candidate that matches any one of them. A candidate of more than one
//     value matches when any one of its values does: a query for AXIAL
//     matches an Image Type of ORIGINAL\PRIMARY\AXIAL.
//
// The last two are applied to every query. A query that does not use them is
// answered as it would be without them; before them, "" was compared as two
// characters and CT\MR as one value, and neither ever matched.
//
// A candidate without the attribute a key asks about has no value for it: it
// matches universal matching, "*" and empty value matching, and nothing else.
// A range does not match a study with no date.
//
// # Character sets
//
// The query is read in the Specific Character Set it declares, and the
// candidate in its own, so a name matches however each was written. Specific
// Character Set is not a key. An answer holds text as UTF-8 and declares
// ISO_IR 192 when any of it is outside ASCII, and nothing when none is
// (PS3.4 C.4.1.1.3.2).
//
// # What a caller decides
//
// [Options.Skip] names the keys a caller does not support. A worklist has no
// Query/Retrieve Level to match; an index that does not hold an attribute
// cannot match on it, and returning nothing for a key it never stored would
// read as an empty archive rather than as an unsupported key (C.2.2.1.2).
package matching
