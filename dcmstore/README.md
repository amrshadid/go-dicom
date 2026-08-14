# dcmstore

An on-disk store of DICOM instances with an index that answers the hierarchical queries a C-FIND asks.

`network.QueryRetrieveHandler` takes callbacks — `OnFind`, `OnGet`, `OnMoveInstances` — and every adopter had to write the same thing behind them before serving anything: file the instances, index the query attributes, then implement the matching rules from PS3.4 C.2.2. Those rules are the fiddly part, and worth implementing once with tests.

## A working archive

```go
store, err := dcmstore.Open("./archive")
if err != nil {
    log.Fatal(err)
}

scp := network.NewSCP(network.SCPConfig{AETitle: "ARCHIVE", Port: 11112})
scp.SetHandler(dcmstore.NewHandler(store))
scp.SetSupportedAbstractSyntaxes(dcmstore.SupportedSOPClasses())

log.Fatal(scp.ListenAndServe(ctx))
```

That accepts C-STORE, answers C-FIND at all four levels, and serves C-GET and C-MOVE from what it has received. Verified over a real association in `integration_test.go`.

## Using the store directly

```go
inst, err := store.Store(ctx, ds)          // write and index an instance
ds, err := store.Load(ctx, inst)           // read one back

responses, err := store.Query(ctx, query)  // C-FIND: one response per entity
matched, err := store.MatchingInstances(ctx, query) // C-GET/C-MOVE: the instances

store.Count()                              // how many instances
store.Instances()                          // all of them, ordered
store.Remove(sopInstanceUID)
store.Rebuild(ctx)                         // re-read the index from the files
```

## On disk

```
archive/
  index.json
  1.2.1/                 <- StudyInstanceUID
    1.2.1.1/             <- SeriesInstanceUID
      1.2.1.1.1.dcm      <- SOPInstanceUID
```

The tree mirrors the hierarchy a query walks, so a study is one directory and can be copied or archived as one.

Every UID that becomes a path component is validated first, through `fileutil.InstanceFilePath`. A UID is dotted decimal (PS3.5 9.1) and arrives from a peer, so anything that could traverse out of the store is refused rather than sanitized into something resembling a UID.

## The index

`index.json` holds the query attributes of every instance, so answering a query does not mean reading the archive. It is written atomically. A store whose index is missing or unreadable rebuilds it by walking the tree — losing an index costs a slow start, not the archive — and a file that cannot be read is skipped with a warning rather than failing the rebuild.

Only the keys PS3.4 C.6.1.1 and C.6.2.1 define are indexed:

| Level | Attributes |
|---|---|
| PATIENT | PatientName, PatientID, PatientBirthDate, PatientSex |
| STUDY | StudyDate, StudyTime, AccessionNumber, ModalitiesInStudy, ReferringPhysicianName, StudyDescription, StudyInstanceUID, StudyID |
| SERIES | Modality, SeriesDescription, SeriesInstanceUID, SeriesNumber |
| IMAGE | SOPClassUID, SOPInstanceUID, InstanceNumber |

`NumberOfStudyRelatedSeries`, `NumberOfStudyRelatedInstances`, `NumberOfSeriesRelatedInstances` and `ModalitiesInStudy` are computed from the index when a query asks for them — over the whole store, not the matched set, since C.6.2.2 defines them as counting what is in the study.

## Matching

| Rule | Where | Behaviour |
|---|---|---|
| Universal | C.2.2.2.3 | A zero-length value matches everything and is returned |
| Single value | C.2.2.2.1 | Equality, padding ignored, dates and times compared at whatever precision each was written to |
| List of UID | C.2.2.2.2 | Backslash-separated, matches any member |
| Wildcard | C.2.2.2.4 | `*` and `?`, for the string VRs only — a `*` in a UI is a `*`, not "everything" |
| Range | C.2.2.2.5 | `lower-upper` for DA, TM, DT; either end optional, both inclusive; a partial bound covers the period it names, so `2024-2024` is the whole year |

Wildcard matching is written directly rather than translated to a regular expression: the pattern comes from a peer, and a real patient name contains `.` and `(`. It is linear, with a test that a pattern of forty stars against a four-thousand character value returns immediately.

An attribute the index does not hold is an **unsupported optional key**: returned with a zero-length value, as C.2.2.1.2 allows, rather than matched on. Matching would return nothing, and an empty result reads as an empty archive rather than as an unsupported query.

Sequence matching (C.2.2.2.6) is not implemented; a sequence in a query is an unsupported optional key like any other.

## Retrieval

A retrieval returns instances whatever level it names — a C-GET at STUDY level transfers every instance in the matching studies.

`Handler` implements the streaming interfaces as well as the slice-returning ones, so instances are read one at a time (a thousand-instance study is not held in memory before the first is sent) and a retrieval stops when the requestor sends C-CANCEL.

## What this is not

Files and an index, no external dependency, not a database. The computed counts scan the index, so an archive of millions of instances wants real indexes behind it. `Handler` satisfies the network package's own interfaces, so replacing this with a database-backed store means implementing those.

Instances are written as Explicit VR Little Endian whatever they arrived as. Pixel data is not recompressed — a compressed instance keeps its encapsulated bytes, and only the surrounding data set is re-encoded.
