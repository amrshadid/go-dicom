package fileset

import (
	"fmt"
	"strings"

	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// A DICOMDIR is the index of a DICOM file-set: what makes a CD, a USB stick or
// an exported directory navigable without opening every file on it.
//
// Its records are stored in one flat sequence and describe a tree. Nothing in
// the sequence says so — the links are byte offsets, held in each record and
// pointing at the position of another record's item tag counted from the first
// byte of the file (PS3.3 F.2.2):
//
//	(0004,1200)  first record of the root entity
//	(0004,1400)  next record at the same level
//	(0004,1420)  first record one level down
//
// Reading the sequence in order gives the right records in the wrong shape, and
// often not even in the right order: pydicom ships DICOMDIR-reordered precisely
// because a conforming file may store them in any order it likes. So the tree is
// built from the offsets, and only falls back to order when a file has none.

// MediaStorageDirectoryStorageUID is the SOP class every DICOMDIR declares.
// Defined here rather than taken from the network package, which a file-set has
// no other reason to depend on.
const MediaStorageDirectoryStorageUID = "1.2.840.10008.1.3.10"

// Directory record tags, PS3.3 Table F.3-3.
var (
	tagFileSetID                = tag.New(0x0004, 0x1130)
	tagRootFirstRecordOffset    = tag.New(0x0004, 0x1200)
	tagRootLastRecordOffset     = tag.New(0x0004, 0x1202)
	tagDirectoryRecordSequence  = tag.New(0x0004, 0x1220)
	tagNextRecordOffset         = tag.New(0x0004, 0x1400)
	tagRecordInUseFlag          = tag.New(0x0004, 0x1410)
	tagLowerLevelEntityOffset   = tag.New(0x0004, 0x1420)
	tagDirectoryRecordType      = tag.New(0x0004, 0x1430)
	tagReferencedFileID         = tag.New(0x0004, 0x1500)
	tagReferencedSOPClassUID    = tag.New(0x0004, 0x1510)
	tagReferencedSOPInstanceUID = tag.New(0x0004, 0x1511)
	tagReferencedTransferSyntax = tag.New(0x0004, 0x1512)
)

// The record types PS3.3 F.5 defines for the common hierarchy. A file may use
// others; these are the ones with a defined place in the Patient/Study/Series
// tree, and the ones GenerateDICOMDIR writes.
const (
	RecordPatient = "PATIENT"
	RecordStudy   = "STUDY"
	RecordSeries  = "SERIES"
	RecordImage   = "IMAGE"
)

// Record is one directory record and everything below it.
type Record struct {
	// Type is the Directory Record Type (0004,1430) — PATIENT, STUDY, SERIES,
	// IMAGE and the rest of PS3.3 F.5.
	Type string

	// Offset is where this record's item tag begins in the file. It is what the
	// parent points at, and it is nonzero only for a record read from a file.
	Offset int64

	// InUse reports the Record In-use Flag (0004,1410). A record that is not in
	// use describes an entry that was deleted without rewriting the file. It is
	// kept here rather than dropped so a caller can see what a file claims.
	InUse bool

	// ReferencedFileID is the path to the referenced file, as components
	// relative to the file-set root. DICOM keeps it split because the separator
	// is a matter for the media format, not the file-set.
	ReferencedFileID []string

	// Dataset holds every attribute of the record, including the ones the
	// fields above summarize.
	Dataset *dataset.Dataset

	// Children are the records one level down.
	Children []*Record
}

// Path joins the Referenced File ID with the given separator.
func (r *Record) Path(separator string) string {
	return strings.Join(r.ReferencedFileID, separator)
}

// StringValue reads a text attribute of the record.
func (r *Record) StringValue(t tag.Tag) string {
	return recordString(r.Dataset, t)
}

// Walk calls fn for this record and every record below it, depth first.
//
// Returning false from fn stops the walk below that record but not beside it,
// so a caller can skip a subtree without abandoning the rest.
func (r *Record) Walk(fn func(*Record) bool) {
	if !fn(r) {
		return
	}
	for _, child := range r.Children {
		child.Walk(fn)
	}
}

// DICOMDIR is a parsed file-set index.
type DICOMDIR struct {
	// FileSetID is the File-set ID (0004,1130), the label on the media.
	FileSetID string

	// Roots are the top-level records, usually one per patient.
	Roots []*Record

	// Records is every record in the order the sequence stored them, whether or
	// not it is reachable from a root. A file may hold an unreachable record —
	// an entry deleted by unlinking rather than rewriting — and a caller
	// counting what is on the media needs to be able to see it.
	Records []*Record

	// OffsetsWereUsed reports whether the tree came from the byte offsets or
	// was rebuilt from record order because the file had none.
	OffsetsWereUsed bool
}

// Walk calls fn for every record in the tree, depth first from each root.
func (d *DICOMDIR) Walk(fn func(*Record) bool) {
	for _, root := range d.Roots {
		root.Walk(fn)
	}
}

// RecordsOfType returns every record of one type, in tree order.
func (d *DICOMDIR) RecordsOfType(recordType string) []*Record {
	var out []*Record
	d.Walk(func(r *Record) bool {
		if r.Type == recordType {
			out = append(out, r)
		}
		return true
	})
	return out
}

// ReadDICOMDIR builds the record tree of a parsed DICOMDIR file.
//
// It takes the file rather than the data set because the tree is built from
// byte offsets, and those survive only on the parsed file — a data set on its
// own no longer knows where its sequence items came from.
func ReadDICOMDIR(df *filereader.DICOMFile) (*DICOMDIR, error) {
	if df == nil {
		return nil, fmt.Errorf("fileset: no file to read a DICOMDIR from")
	}
	ds := df.GetDataset()

	seqElem, ok := ds.Get(tagDirectoryRecordSequence)
	if !ok {
		return nil, fmt.Errorf("fileset: the file has no Directory Record Sequence (0004,1220); "+
			"its media storage SOP class is %q, and a DICOMDIR is %q",
			recordString(ds, tag.New(0x0002, 0x0002)), MediaStorageDirectoryStorageUID)
	}
	seq, ok := seqElem.GetValue().(*sequence.Sequence)
	if !ok {
		return nil, fmt.Errorf("fileset: the Directory Record Sequence is not a sequence (%T)",
			seqElem.GetValue())
	}

	offsets := recordOffsets(df)

	out := &DICOMDIR{FileSetID: recordString(ds, tagFileSetID)}
	byOffset := make(map[int64]*Record, seq.Length())

	for i := 0; i < seq.Length(); i++ {
		item, err := seq.Get(i)
		if err != nil {
			return nil, fmt.Errorf("fileset: reading directory record %d: %w", i, err)
		}
		recordDS, ok := item.(*dataset.Dataset)
		if !ok {
			continue
		}
		rec := &Record{
			Type:             strings.TrimSpace(recordString(recordDS, tagDirectoryRecordType)),
			InUse:            recordUint(recordDS, tagRecordInUseFlag) != 0,
			ReferencedFileID: fileIDComponents(recordDS),
			Dataset:          recordDS,
		}
		if i < len(offsets) {
			rec.Offset = offsets[i]
			byOffset[rec.Offset] = rec
		}
		out.Records = append(out.Records, rec)
	}

	// An empty DICOMDIR is a valid one: media with no instances on it yet.
	if len(out.Records) == 0 {
		return out, nil
	}

	rootOffset := int64(recordUint(ds, tagRootFirstRecordOffset))
	if rootOffset != 0 && len(byOffset) > 0 {
		if linkByOffset(out, byOffset, rootOffset) {
			out.OffsetsWereUsed = true
			return out, nil
		}
	}

	// No usable offsets. pydicom's DICOMDIR-nooffset is such a file: every
	// offset is zero, and the hierarchy is only recoverable from the order the
	// records appear in and the levels their types sit at.
	linkByOrder(out)
	return out, nil
}

// recordOffsets returns the byte offset of each directory record item, by index.
func recordOffsets(df *filereader.DICOMFile) []int64 {
	for _, elem := range df.DataElements {
		if elem.Tag != tagDirectoryRecordSequence {
			continue
		}
		out := make([]int64, len(elem.Items))
		for i, item := range elem.Items {
			out[i] = item.Offset
		}
		return out
	}
	return nil
}

// linkByOffset builds the tree from the offsets, and reports whether it found
// anything to build.
func linkByOffset(d *DICOMDIR, byOffset map[int64]*Record, rootOffset int64) bool {
	// A malformed file can point a record at itself or into a cycle, and the
	// walk has to end either way.
	visited := make(map[int64]bool, len(byOffset))

	var chain func(offset int64) []*Record
	chain = func(offset int64) []*Record {
		var out []*Record
		for offset != 0 {
			if visited[offset] {
				return out // a cycle: stop rather than walk it forever
			}
			rec, ok := byOffset[offset]
			if !ok {
				return out // a dangling offset, which says nothing about the rest
			}
			visited[offset] = true
			out = append(out, rec)
			rec.Children = chain(int64(recordUint(rec.Dataset, tagLowerLevelEntityOffset)))
			offset = int64(recordUint(rec.Dataset, tagNextRecordOffset))
		}
		return out
	}

	d.Roots = chain(rootOffset)
	return len(d.Roots) > 0
}

// recordLevel gives a record type its depth in the common hierarchy.
var recordLevel = map[string]int{
	RecordPatient: 0,
	RecordStudy:   1,
	RecordSeries:  2,
}

// linkByOrder rebuilds the tree from record order, for a file with no offsets.
//
// Each record attaches to the most recent record one level above it. This is
// what the order means in a file written sequentially, and it is the only thing
// left to go on — which is why a file with offsets is read from the offsets
// instead, since the order there may be anything at all.
func linkByOrder(d *DICOMDIR) {
	// Anything not in the table is a leaf: IMAGE and its many siblings all sit
	// below SERIES.
	levelOf := func(r *Record) int {
		if level, ok := recordLevel[r.Type]; ok {
			return level
		}
		return 3
	}

	var stack []*Record
	for _, rec := range d.Records {
		level := levelOf(rec)
		for len(stack) > 0 && levelOf(stack[len(stack)-1]) >= level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			d.Roots = append(d.Roots, rec)
		} else {
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, rec)
		}
		stack = append(stack, rec)
	}
}

// fileIDComponents reads the Referenced File ID, which is multi-valued.
func fileIDComponents(ds *dataset.Dataset) []string {
	value := recordString(ds, tagReferencedFileID)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "\\")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// recordString reads a text value, without its padding.
func recordString(ds *dataset.Dataset, t tag.Tag) string {
	if ds == nil {
		return ""
	}
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(raw), " \x00")
}

// recordUint reads an unsigned value of two or four bytes.
//
// The offsets are UL and the in-use flag is US, and both are read here because
// a file may disagree with the dictionary about which — the value is the same
// number either way, and refusing to read a four-byte flag would lose the tree.
func recordUint(ds *dataset.Dataset, t tag.Tag) uint32 {
	if ds == nil {
		return 0
	}
	elem, ok := ds.Get(t)
	if !ok {
		return 0
	}
	raw, ok := elem.GetValue().([]byte)
	if !ok {
		return 0
	}
	switch {
	case len(raw) >= 4:
		return uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24
	case len(raw) >= 2:
		return uint32(raw[0]) | uint32(raw[1])<<8
	}
	return 0
}
