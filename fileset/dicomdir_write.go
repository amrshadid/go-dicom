package fileset

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/filewriter"
	"github.com/amrshadid/go-dicom/tag"
)

// Writing a DICOMDIR is harder than writing any other DICOM file, for one
// reason: the records point at each other with byte offsets into the finished
// file. Nothing can be filled in until the layout is known, and the layout
// depends on what is filled in.
//
// It resolves because the offsets are UL — four bytes, whatever the value. So
// the file is written once with the offsets zeroed, read back to learn where
// each record item landed, and written again with the real values. The second
// pass cannot move anything, because nothing changed size.
//
// This is why GenerateDICOMDIR previously returned an empty data set: producing
// something that looks like a DICOMDIR is easy, and producing one whose offsets
// are right is the whole job. A file with wrong offsets is worse than no file —
// it is accepted by a reader and describes a tree that is not there.

// Attributes each record type must carry, PS3.3 F.5.1 to F.5.4. A record
// missing one of these is not a valid directory record, and a reader is
// entitled to refuse the file — pydicom does.
var requiredRecordKeys = map[string][]tag.Tag{
	RecordPatient: {
		tag.New(0x0010, 0x0010), // Patient's Name
		tag.New(0x0010, 0x0020), // Patient ID
	},
	RecordStudy: {
		tag.New(0x0008, 0x0020), // Study Date
		tag.New(0x0008, 0x0030), // Study Time
		tag.New(0x0020, 0x000D), // Study Instance UID
		tag.New(0x0020, 0x0010), // Study ID
		tag.New(0x0008, 0x0050), // Accession Number
	},
	RecordSeries: {
		tag.New(0x0008, 0x0060), // Modality
		tag.New(0x0020, 0x000E), // Series Instance UID
		tag.New(0x0020, 0x0011), // Series Number
	},
	RecordImage: {
		tag.New(0x0020, 0x0013), // Instance Number
	},
}

// DICOMDIROptions controls generation.
type DICOMDIROptions struct {
	// FileSetID is the label on the media, at most 16 characters.
	FileSetID string

	// SOPInstanceUID identifies the DICOMDIR itself. Generated when empty.
	SOPInstanceUID string
}

// GenerateDICOMDIR builds the file-set's index as a data set.
//
// The records come from the scanned files, grouped into the Patient, Study,
// Series and Instance hierarchy of PS3.3 F.5. Files that cannot be placed —
// no Study Instance UID, no Series Instance UID — are reported rather than
// dropped, since a file-set index that silently omits files is one nobody can
// trust to be complete.
func (fs *FileSet) GenerateDICOMDIR() (*dataset.Dataset, error) {
	return fs.GenerateDICOMDIRWithOptions(DICOMDIROptions{})
}

// GenerateDICOMDIRWithOptions builds the index with a File-set ID.
func (fs *FileSet) GenerateDICOMDIRWithOptions(opts DICOMDIROptions) (*dataset.Dataset, error) {
	data, err := fs.WriteDICOMDIR(opts)
	if err != nil {
		return nil, err
	}
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("fileset: the generated DICOMDIR does not read back: %w", err)
	}
	built := df.GetDataset()

	fs.mu.Lock()
	fs.DicomDirFile = built
	fs.mu.Unlock()
	return built, nil
}

// WriteDICOMDIR builds the file-set's index as a complete DICOM file.
//
// The bytes are what belongs at DICOMDIR in the file-set's root directory.
func (fs *FileSet) WriteDICOMDIR(opts DICOMDIROptions) ([]byte, error) {
	fs.mu.RLock()
	records := make([]*FileRecord, len(fs.FileRecords))
	copy(records, fs.FileRecords)
	root := fs.RootDir
	fs.mu.RUnlock()

	tree, err := buildRecordTree(records, root)
	if err != nil {
		return nil, err
	}

	// The instance UID has to be settled before the first pass. Generating it
	// inside the encoder gave the two passes different UIDs, and a UID two
	// characters longer moves every record two bytes — so the offsets computed
	// from the first layout described the second one incorrectly, by exactly
	// the difference in length.
	if opts.SOPInstanceUID == "" {
		opts.SOPInstanceUID = generateDICOMDIRInstanceUID()
	}

	// First pass: offsets zeroed, to learn the layout.
	flat := flattenRecords(tree)
	first, err := encodeDICOMDIR(flat, opts)
	if err != nil {
		return nil, err
	}

	// Learn where each record item landed, and what the root offset must be.
	positions, err := recordItemPositions(first)
	if err != nil {
		return nil, err
	}
	if len(positions) != len(flat) {
		return nil, fmt.Errorf("fileset: wrote %d records and read back %d; "+
			"the offsets cannot be computed from a layout that does not match",
			len(flat), len(positions))
	}
	for i, rec := range flat {
		rec.Offset = positions[i]
	}
	applyOffsets(tree, flat)

	// Second pass. Nothing changed size, so nothing moved.
	final, err := encodeDICOMDIR(flat, opts)
	if err != nil {
		return nil, err
	}

	// And confirm it, rather than trusting the reasoning. The whole method
	// rests on the second pass laying out identically to the first, and a file
	// whose offsets are wrong is worse than no file: a reader accepts it and
	// follows it into a tree that is not there. Checking costs one parse.
	confirmed, err := recordItemPositions(final)
	if err != nil {
		return nil, err
	}
	for i, rec := range flat {
		if i >= len(confirmed) || confirmed[i] != rec.Offset {
			return nil, fmt.Errorf("fileset: record %d was written at a different offset than "+
				"the one recorded for it (%d against %d); the file would describe a tree "+
				"that is not there", i, confirmed[i], rec.Offset)
		}
	}
	return final, nil
}

// buildRecordTree groups the scanned files into the PS3.3 F.5 hierarchy.
func buildRecordTree(files []*FileRecord, rootDir string) ([]*Record, error) {
	type seriesKey struct{ study, series string }

	var roots []*Record
	patients := map[string]*Record{}
	studies := map[string]*Record{}
	seriesByKey := map[seriesKey]*Record{}

	// Sorted so the same file-set always produces the same file, which matters
	// for anything that compares or caches media.
	sorted := make([]*FileRecord, len(files))
	copy(sorted, files)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].FilePath < sorted[j].FilePath })

	var unplaceable []string
	for _, file := range sorted {
		if file.Dataset == nil {
			unplaceable = append(unplaceable, file.FilePath+" (no data set)")
			continue
		}
		studyUID := recordString(file.Dataset, tag.New(0x0020, 0x000D))
		seriesUID := recordString(file.Dataset, tag.New(0x0020, 0x000E))
		if studyUID == "" || seriesUID == "" {
			unplaceable = append(unplaceable, file.FilePath+" (no study or series UID)")
			continue
		}
		patientID := recordString(file.Dataset, tag.New(0x0010, 0x0020))

		patient, ok := patients[patientID]
		if !ok {
			patient = newRecord(RecordPatient, file.Dataset)
			patients[patientID] = patient
			roots = append(roots, patient)
		}
		study, ok := studies[studyUID]
		if !ok {
			study = newRecord(RecordStudy, file.Dataset)
			studies[studyUID] = study
			patient.Children = append(patient.Children, study)
		}
		key := seriesKey{studyUID, seriesUID}
		series, ok := seriesByKey[key]
		if !ok {
			series = newRecord(RecordSeries, file.Dataset)
			seriesByKey[key] = series
			study.Children = append(study.Children, series)
		}

		image := newRecord(RecordImage, file.Dataset)
		if err := setReferencedFile(image, file, rootDir); err != nil {
			return nil, err
		}
		series.Children = append(series.Children, image)
	}

	if len(unplaceable) > 0 {
		return nil, fmt.Errorf("fileset: %d file(s) cannot be placed in the hierarchy, "+
			"and an index that omits them silently would not be trustworthy: %s",
			len(unplaceable), strings.Join(unplaceable, ", "))
	}
	return roots, nil
}

// newRecord builds one directory record, copying the keys its type requires.
func newRecord(recordType string, source *dataset.Dataset) *Record {
	ds := dataset.NewDataset()

	// The four attributes every record carries. The offsets are written as
	// four zero bytes so the second pass can fill them in without moving
	// anything.
	_ = ds.Add(dataelem.NewDataElement(tagNextRecordOffset, dataelem.UL, make([]byte, 4)))
	_ = ds.Add(dataelem.NewDataElement(tagRecordInUseFlag, dataelem.US, []byte{0xFF, 0xFF}))
	_ = ds.Add(dataelem.NewDataElement(tagLowerLevelEntityOffset, dataelem.UL, make([]byte, 4)))
	_ = ds.Add(dataelem.NewDataElement(tagDirectoryRecordType, dataelem.CS,
		[]byte(evenLength(recordType))))

	for _, t := range requiredRecordKeys[recordType] {
		value := ""
		vr := dataelem.VR(t.GetVR())
		if elem, ok := source.Get(t); ok {
			if raw, ok := elem.GetValue().([]byte); ok {
				value = strings.TrimRight(string(raw), " \x00")
			}
		}
		// A required key that the source lacks is written empty rather than
		// omitted: PS3.3 requires the attribute to be present, and a type 2
		// attribute with no value is exactly how DICOM says "not known".
		if strings.Contains(string(vr), " or ") || vr == "" {
			vr = dataelem.LO
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, []byte(padByVR(vr, value))))
	}

	return &Record{Type: recordType, InUse: true, Dataset: ds}
}

// setReferencedFile fills in the attributes that point an IMAGE record at a file.
func setReferencedFile(rec *Record, file *FileRecord, rootDir string) error {
	relative := file.FilePath
	if rootDir != "" {
		if rel, err := filepath.Rel(rootDir, file.FilePath); err == nil {
			relative = rel
		}
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	rec.ReferencedFileID = components

	// PS3.10 keeps the File ID split into components because the separator
	// belongs to the media format, not to the file-set. Stored, they are one
	// multi-valued attribute.
	_ = rec.Dataset.Add(dataelem.NewDataElement(tagReferencedFileID, dataelem.CS,
		[]byte(evenLength(strings.Join(components, "\\")))))

	sopClass := recordString(file.Dataset, tag.New(0x0008, 0x0016))
	sopInstance := recordString(file.Dataset, tag.New(0x0008, 0x0018))
	if sopClass == "" || sopInstance == "" {
		return fmt.Errorf("fileset: %s has no SOP Class or SOP Instance UID, and a "+
			"directory record cannot reference a file without them", file.FilePath)
	}
	transferSyntax := file.Dataset.TransferSyntaxUID()
	if transferSyntax == "" {
		transferSyntax = ExplicitVRLittleEndianUID
	}

	_ = rec.Dataset.Add(dataelem.NewDataElement(tagReferencedSOPClassUID, dataelem.UI,
		[]byte(evenLengthUID(sopClass))))
	_ = rec.Dataset.Add(dataelem.NewDataElement(tagReferencedSOPInstanceUID, dataelem.UI,
		[]byte(evenLengthUID(sopInstance))))
	_ = rec.Dataset.Add(dataelem.NewDataElement(tagReferencedTransferSyntax, dataelem.UI,
		[]byte(evenLengthUID(transferSyntax))))
	return nil
}

// flattenRecords lists the tree depth first, which is the order the sequence
// stores them in. Any order would be legal, and this one keeps a file written
// here readable by a reader that ignores the offsets.
func flattenRecords(roots []*Record) []*Record {
	var out []*Record
	var walk func(r *Record)
	walk = func(r *Record) {
		out = append(out, r)
		for _, c := range r.Children {
			walk(c)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return out
}

// applyOffsets writes each record's links now that the positions are known.
func applyOffsets(roots []*Record, flat []*Record) {
	link := func(siblings []*Record) {
		for i, rec := range siblings {
			var next int64
			if i+1 < len(siblings) {
				next = siblings[i+1].Offset
			}
			setUint32(rec.Dataset, tagNextRecordOffset, uint32(next))

			var lower int64
			if len(rec.Children) > 0 {
				lower = rec.Children[0].Offset
			}
			setUint32(rec.Dataset, tagLowerLevelEntityOffset, uint32(lower))
		}
	}
	var walk func(siblings []*Record)
	walk = func(siblings []*Record) {
		link(siblings)
		for _, rec := range siblings {
			walk(rec.Children)
		}
	}
	walk(roots)
}

// recordItemPositions reads a written DICOMDIR back and reports where each
// record item begins.
//
// Reading the file that was just written is the only honest way to know: the
// positions depend on every encoding decision the writer made, and computing
// them separately would mean maintaining a second model of the encoding that
// can drift from the first.
func recordItemPositions(data []byte) ([]int64, error) {
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("fileset: the first pass does not read back: %w", err)
	}
	for _, elem := range df.DataElements {
		if elem.Tag != tagDirectoryRecordSequence {
			continue
		}
		out := make([]int64, len(elem.Items))
		for i, item := range elem.Items {
			out[i] = item.Offset
		}
		return out, nil
	}
	return nil, fmt.Errorf("fileset: the first pass has no Directory Record Sequence")
}

// ExplicitVRLittleEndianUID is the transfer syntax PS3.10 requires of a
// DICOMDIR. Media has to be readable by anything, so there is no choice here.
const ExplicitVRLittleEndianUID = "1.2.840.10008.1.2.1"

// encodeDICOMDIR writes the file.
func encodeDICOMDIR(flat []*Record, opts DICOMDIROptions) ([]byte, error) {
	items := make([]*filewriter.SequenceItem, 0, len(flat))
	for _, rec := range flat {
		item, err := recordToWriterItem(rec.Dataset)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	var rootFirst, rootLast uint32
	if len(flat) > 0 {
		rootFirst = uint32(flat[0].Offset)
		// The last root, not the last record: the value names the end of the
		// top-level chain, and the last record in the sequence is a leaf far
		// below it.
		for _, rec := range flat {
			if rec.Offset > int64(rootLast) && isRoot(flat, rec) {
				rootLast = uint32(rec.Offset)
			}
		}
	}

	out := &seekableBuffer{}
	w := filewriter.NewDICOMFileWriter(filebase.NewFileWriter(out))

	sopInstance := opts.SOPInstanceUID
	if sopInstance == "" {
		sopInstance = generateDICOMDIRInstanceUID()
	}
	w.SetFileMetaInfo(&filewriter.FileMetaInfo{
		MediaStorageSOPClassUID:    MediaStorageDirectoryStorageUID,
		MediaStorageSOPInstanceUID: sopInstance,
		TransferSyntaxUID:          ExplicitVRLittleEndianUID,
	})

	add := func(t tag.Tag, vr dataelem.VR, value []byte) error {
		return w.AddDataElement(&filewriter.DataElement{
			Tag: t, VR: string(vr), Value: value, Length: uint32(len(value)),
		})
	}

	fileSetID := opts.FileSetID
	if len(fileSetID) > 16 {
		fileSetID = fileSetID[:16] // CS, 16 characters at most
	}
	if err := add(tagFileSetID, dataelem.CS, []byte(evenLength(fileSetID))); err != nil {
		return nil, err
	}
	if err := add(tagRootFirstRecordOffset, dataelem.UL, uint32Bytes(rootFirst)); err != nil {
		return nil, err
	}
	if err := add(tagRootLastRecordOffset, dataelem.UL, uint32Bytes(rootLast)); err != nil {
		return nil, err
	}
	// File-set Consistency Flag: 0 means no known inconsistency.
	if err := add(tag.New(0x0004, 0x1212), dataelem.US, []byte{0x00, 0x00}); err != nil {
		return nil, err
	}
	if err := w.AddDataElement(&filewriter.DataElement{
		Tag: tagDirectoryRecordSequence, VR: string(dataelem.SQ), Items: items,
	}); err != nil {
		return nil, err
	}
	if err := w.Write(); err != nil {
		return nil, fmt.Errorf("fileset: writing the DICOMDIR: %w", err)
	}
	return out.Bytes(), nil
}

// recordToWriterItem converts a record's data set into the writer's form,
// keeping the elements in ascending tag order as PS3.5 7.1 requires.
func recordToWriterItem(ds *dataset.Dataset) (*filewriter.SequenceItem, error) {
	elems := ds.GetAll()
	converted := make([]*filewriter.DataElement, 0, len(elems))
	for _, elem := range elems {
		t, ok := elem.Tag()
		if !ok {
			return nil, fmt.Errorf("fileset: a directory record element has an unreadable tag (%T)", elem)
		}
		raw, _ := elem.GetValue().([]byte)
		converted = append(converted, &filewriter.DataElement{
			Tag: t, VR: string(elem.GetVR()), Value: raw, Length: uint32(len(raw)),
		})
	}
	sort.SliceStable(converted, func(i, j int) bool {
		return uint32(converted[i].Tag) < uint32(converted[j].Tag)
	})
	return &filewriter.SequenceItem{Elements: converted}, nil
}

// seekableBuffer is an in-memory io.WriteSeeker, which the file writer needs so
// it can go back and fill in the group lengths it could not know up front.
type seekableBuffer struct {
	data []byte
	pos  int64
}

func (b *seekableBuffer) Write(p []byte) (int, error) {
	end := b.pos + int64(len(p))
	if end > int64(len(b.data)) {
		grown := make([]byte, end)
		copy(grown, b.data)
		b.data = grown
	}
	copy(b.data[b.pos:], p)
	b.pos = end
	return len(p), nil
}

func (b *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = b.pos + offset
	case io.SeekEnd:
		next = int64(len(b.data)) + offset
	default:
		return 0, fmt.Errorf("fileset: unknown seek origin %d", whence)
	}
	if next < 0 {
		return 0, fmt.Errorf("fileset: seek to %d, before the start", next)
	}
	b.pos = next
	return next, nil
}

func (b *seekableBuffer) Bytes() []byte { return b.data }

// generateDICOMDIRInstanceUID makes a SOP Instance UID for the index itself.
//
// Rooted at this implementation's registered prefix, with a random suffix. A
// DICOMDIR is regenerated whenever the file-set changes, and each generation is
// a different instance.
func generateDICOMDIRInstanceUID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Randomness is unavailable, which says nothing about the file-set. A
		// fixed suffix is worse than a random one and better than no UID.
		return dicomdirUIDRoot + ".1"
	}
	n := binary.BigEndian.Uint64(buf[:]) >> 1 // positive, and short enough for 64 characters
	return fmt.Sprintf("%s.%d", dicomdirUIDRoot, n)
}

// dicomdirUIDRoot is this implementation's registered UID root.
const dicomdirUIDRoot = "1.2.826.0.1.3680043.10.511.5"

// isRoot reports whether a record is at the top level of the tree.
func isRoot(flat []*Record, rec *Record) bool {
	for _, other := range flat {
		for _, child := range other.Children {
			if child == rec {
				return false
			}
		}
	}
	return true
}

// setUint32 replaces a four-byte unsigned value in place, so the element keeps
// its position and its size.
func setUint32(ds *dataset.Dataset, t tag.Tag, value uint32) {
	if elem, ok := ds.Get(t); ok {
		elem.SetValue(uint32Bytes(value))
		return
	}
	_ = ds.Add(dataelem.NewDataElement(t, dataelem.UL, uint32Bytes(value)))
}

func uint32Bytes(v uint32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, v)
	return out
}

// padByVR pads a value to an even length with the character its VR uses.
//
// PS3.5 6.2 pads a UI with a null byte and everything else with a space.
// Padding a UID with a space puts a character in it that is not part of the
// identifier, which dcmtk reports and strips and which makes a UID compare
// unequal to the same UID written correctly.
func padByVR(vr dataelem.VR, value string) string {
	if vr == dataelem.UI {
		return evenLengthUID(value)
	}
	return evenLength(value)
}

// evenLength pads a text value, as PS3.5 7.1.1 requires.
func evenLength(s string) string {
	if len(s)%2 == 1 {
		return s + " "
	}
	return s
}

// evenLengthUID pads a UID, which is padded with null rather than space.
func evenLengthUID(s string) string {
	if len(s)%2 == 1 {
		return s + "\x00"
	}
	return s
}
