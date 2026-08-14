// Package dcmstore is an on-disk store of DICOM instances with an index that
// answers the hierarchical queries a C-FIND asks.
//
// It exists because [github.com/amrshadid/go-dicom/network.QueryRetrieveHandler]
// takes callbacks — OnFind, OnGet, OnMoveInstances — and every adopter had to
// write the same thing behind them before they could serve anything: file the
// received instances somewhere, index the attributes a query matches on, then
// implement the matching rules from PS3.4 C.2.2. Those rules are the fiddly part,
// and they are worth implementing once with tests rather than in every downstream
// project.
//
// # A working archive
//
//	store, err := dcmstore.Open("./archive")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	scp := network.NewSCP(network.SCPConfig{AETitle: "ARCHIVE", Port: 11112})
//	scp.SetHandler(dcmstore.NewHandler(store))
//	scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())
//
//	log.Fatal(scp.ListenAndServe(ctx))
//
// That accepts C-STORE, answers C-FIND at all four levels, and serves C-GET and
// C-MOVE from what it has received.
//
// # On disk
//
// Instances are files, named after their SOP Instance UID, under a directory per
// study and series:
//
//	archive/
//	  index.json
//	  1.2.1/                 <- StudyInstanceUID
//	    1.2.1.1/             <- SeriesInstanceUID
//	      1.2.1.1.1.dcm      <- SOPInstanceUID
//
// The tree mirrors the hierarchy a query walks, so a study is one directory and
// can be copied or archived as one.
//
// Every UID that becomes a path component is validated first. A UID is dotted
// decimal (PS3.5 9.1), and it arrives from a peer — see
// [github.com/amrshadid/go-dicom/fileutil.InstanceFilePath], which refuses
// anything that could traverse out of the store rather than sanitizing it into
// something that resembles a UID.
//
// # The index
//
// index.json holds the query attributes of every instance, so answering a query
// does not mean reading the archive. It is written atomically, and a store whose
// index is missing or unreadable rebuilds it by walking the tree — losing an
// index costs a slow start, not the archive. A file that cannot be read is
// skipped with a warning, so one corrupt instance does not make the rest
// unqueryable.
//
// Only the attributes PS3.4 C.6.1.1 and C.6.2.1 define as keys are indexed. An
// index of every attribute of every instance would hold the whole archive in
// memory, which is what storing the files was meant to avoid.
//
// # Matching
//
// [Store.Query] implements the matching rules of PS3.4 C.2.2:
//
//   - Universal (C.2.2.2.3) — a zero-length query value matches everything, and
//     the attribute is returned.
//
//   - Single value (C.2.2.2.1) — equality, with the padding PS3.5 6.2 permits
//     ignored, and dates and times compared at whatever precision each was
//     written to.
//
//   - List of UID (C.2.2.2.2) — a backslash-separated list of UIDs.
//
//   - Wildcard (C.2.2.2.4) — "*" and "?" for the string VRs, and literal
//     characters everywhere else. A "*" in a UI is a "*", not "everything".
//
//   - Range (C.2.2.2.5) — "lower-upper" for DA, TM and DT, either end optional,
//     both inclusive. A partial bound covers the period it names, so
//     "2024-2024" is the whole year.
//
//   - Sequence (C.2.2.2.6) — a query sequence whose single item holds attributes
//     to match against the items of the stored sequence. It matches if any one
//     stored item satisfies every attribute in the query item; the criteria must
//     be met by the same item, so a station from one scheduled step and a date
//     from another is not a match.
//
// An attribute the index does not hold is treated as an unsupported optional key
// and returned with a zero-length value, which C.2.2.1.2 allows. Matching on it
// instead would return nothing at all, and an empty result reads to the requestor
// as an empty archive rather than as an unsupported query.
//
// # Which sequences are matched
//
// Sequence matching needs the nested attributes in the index, and an index holding
// every nested attribute of every instance would put the whole data set back in
// memory. So the indexed sequences are the ones the standard defines as matching
// keys:
//
//   - (0040,0100) Scheduled Procedure Step Sequence, which every Modality Worklist
//     query filters on (PS3.4 K.6.1.2.2) — scheduled station AE title and name,
//     start date and time, modality, performing physician, description, step ID
//     and location.
//   - (0040,0275) Request Attributes Sequence — requested procedure ID, scheduled
//     step ID and description, requested procedure description.
//
// Any other sequence is an unsupported optional key, as is an attribute inside an
// indexed sequence that is not itself indexed.
//
// A response returns the stored item that matched rather than whichever came
// first, so it describes the step the query was about.
//
// # Retrieval
//
// A retrieval returns instances whatever level it names: a C-GET at STUDY level
// transfers every instance in the matching studies. [Handler] implements the
// streaming interfaces as well as the slice-returning ones, so a retrieval reads
// one instance off disk at a time — a thousand-instance study is not held in
// memory before the first is sent — and stops when the requestor sends C-CANCEL.
//
// # What this is not
//
// It is files and an index, with no external dependency, and it is not a
// database. The counts a query can ask for are computed by scanning the index, so
// an archive of millions of instances wants something with real indexes behind it.
// The interfaces [Handler] satisfies are the network package's own, so replacing
// this with a database-backed store is a matter of implementing them.
//
// Instances are written as Explicit VR Little Endian whatever they arrived as.
// Pixel data is not recompressed: a compressed instance keeps its encapsulated
// bytes and only the surrounding data set is re-encoded.
package dcmstore
