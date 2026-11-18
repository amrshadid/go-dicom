package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/fileset"
	"github.com/amrshadid/go-dicom/tag"
)

// dicomdirSchemaVersion is the version of the document `dicomdir --list
// --json` prints. It goes up when a field is renamed, removed or changes
// meaning; a field added does not change it.
const dicomdirSchemaVersion = 1

// dicomdirName is the name PS3.10 8.4 gives the file that indexes a
// file-set, at the root of it.
const dicomdirName = "DICOMDIR"

// DICOMDIRCommand writes the DICOMDIR of a directory of DICOM files, and
// lists one that exists.
type DICOMDIRCommand struct {
	list      bool
	asJSON    bool
	force     bool
	copyTo    string
	fileSetID string
	help      bool
	// out and errOut are where the command writes; nil means standard output
	// and standard error.
	out    io.Writer
	errOut io.Writer
}

// NewDICOMDIRCommand creates a new dicomdir command.
func NewDICOMDIRCommand() *DICOMDIRCommand {
	return &DICOMDIRCommand{}
}

// Name returns the command name.
func (dc *DICOMDIRCommand) Name() string {
	return "dicomdir"
}

// Description returns the command description.
func (dc *DICOMDIRCommand) Description() string {
	return "Write the DICOMDIR of a directory, or list one"
}

// AddFlags adds command flags to the flag set.
func (dc *DICOMDIRCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&dc.help, "h", false, "Show help for this command")
	fs.BoolVar(&dc.help, "help", false, "Show help for this command")
	fs.BoolVar(&dc.list, "list", false, "List an existing DICOMDIR instead of writing one")
	fs.BoolVar(&dc.asJSON, "json", false, "With --list, print the records as JSON")
	fs.BoolVar(&dc.force, "force", false, "Replace a DICOMDIR that is already there")
	fs.StringVar(&dc.copyTo, "copy-to", "", "Copy the files, under names PS3.10 allows, into this new directory and index them there")
	fs.StringVar(&dc.fileSetID, "file-set-id", "", "The File-set ID, the label of the media: at most 16 characters")
}

// Execute runs the dicomdir command.
func (dc *DICOMDIRCommand) Execute(args []string) error {
	fs := flag.NewFlagSet("dicomdir", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("dicomdir: failed to parse flags: %v", err)
	}
	if dc.help {
		dc.showHelp()
		return nil
	}
	if len(positional) != 1 {
		return fmt.Errorf("dicomdir: expected one directory, got %d\nRun 'go-dicom dicomdir -h' for usage", len(positional))
	}
	if dc.list {
		if dc.copyTo != "" || dc.force || dc.fileSetID != "" {
			return fmt.Errorf("dicomdir: --copy-to, --force and --file-set-id are for writing; --list reads")
		}
		return dc.runList(positional[0])
	}
	if dc.asJSON {
		return fmt.Errorf("dicomdir: --json describes a DICOMDIR that is listed; use it with --list")
	}
	return dc.runWrite(positional[0])
}

// stderr is where what the command notices goes.
func (dc *DICOMDIRCommand) stderr() io.Writer {
	if dc.errOut == nil {
		return os.Stderr
	}
	return dc.errOut
}

// copiedFileID is the File ID the nth copied file gets: IM and six digits,
// in one directory, DICOM, as dcmtk's dcmmkdir lays media out.
func copiedFileID(n int) []string {
	return []string{"DICOM", fmt.Sprintf("IM%06d", n)}
}

// maxCopied is how many files --copy-to numbers: six digits.
const maxCopied = 999999

// runWrite indexes a directory.
func (dc *DICOMDIRCommand) runWrite(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("dicomdir: %s is not a directory; to list a DICOMDIR use --list", dir)
	}
	if len(dc.fileSetID) > 16 {
		return fmt.Errorf("dicomdir: a File-set ID is at most 16 characters (PS3.3 F.3.2.1), %q is %d", dc.fileSetID, len(dc.fileSetID))
	}

	// The root of the file-set is where the DICOMDIR is written, and every
	// File ID in it counts from there.
	root := dir
	files, skipped, err := scanForDICOMDIR(dir)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("dicomdir: no DICOM files in %s", dir)
	}

	if dc.copyTo == "" {
		if bad := nonConformantFiles(root, files); len(bad) > 0 {
			return fmt.Errorf("dicomdir: %d file(s) are not named as a File ID must be (PS3.10 8.5: components of at most "+
				"8 characters from A-Z, 0-9 and _, no extension), so a DICOMDIR could not index them for media; "+
				"nothing was written. Rename them, or copy them under names that do with --copy-to <new directory>:\n  %s",
				len(bad), strings.Join(bad, "\n  "))
		}
	} else {
		if len(files) > maxCopied {
			return fmt.Errorf("dicomdir: --copy-to numbers at most %d files, and there are %d", maxCopied, len(files))
		}
		root = dc.copyTo
		if err := emptyOrMissing(root); err != nil {
			return fmt.Errorf("dicomdir: --copy-to: %v", err)
		}
	}

	target := filepath.Join(root, dicomdirName)
	if _, err := os.Stat(target); err == nil && !dc.force {
		return fmt.Errorf("dicomdir: %s already exists; --force replaces it", target)
	}

	if dc.copyTo != "" {
		for i, f := range files {
			dest := filepath.Join(append([]string{root}, copiedFileID(i+1)...)...)
			if err := copyFile(f, dest); err != nil {
				return fmt.Errorf("dicomdir: copying %s: %v", f, err)
			}
		}
		// Indexed from the copies, which are what the DICOMDIR describes.
		if files, _, err = scanForDICOMDIR(root); err != nil {
			return fmt.Errorf("dicomdir: %v", err)
		}
	}

	set, err := fileset.NewFileSet(root)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	for _, f := range files {
		if err := set.AddFile(f); err != nil {
			return fmt.Errorf("dicomdir: %v", err)
		}
	}
	data, err := set.WriteDICOMDIR(fileset.DICOMDIROptions{FileSetID: dc.fileSetID})
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}

	// Written beside its destination and renamed into place, so a failure
	// part way does not leave half an index where the whole one was.
	tmp, err := os.CreateTemp(root, ".dicomdir-*")
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("dicomdir: %v", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("dicomdir: %v", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("dicomdir: %v", err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("dicomdir: %v", err)
	}

	for _, name := range skipped {
		fmt.Fprintf(dc.stderr(), "skipped %s\n", name)
	}
	dir2, err := parseDICOMDIRBytes(data)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	fmt.Fprintf(orStdout(dc.out), "Wrote %s: %s\n", target, countsLine(dir2))
	return nil
}

// scanForDICOMDIR returns the DICOM files under dir in the order of their
// paths, and the files that are left out with the reason.
// An existing DICOMDIR at the root is neither: it is what is being replaced.
func scanForDICOMDIR(dir string) (files, skipped []string, err error) {
	set, err := fileset.NewFileSet(dir)
	if err != nil {
		return nil, nil, err
	}
	if _, err := set.ScanDirectory(true); err != nil {
		return nil, nil, err
	}
	existing := filepath.Join(dir, dicomdirName)
	_ = set.RemoveFile(existing)

	indexed := map[string]bool{}
	for _, r := range set.ListFiles() {
		indexed[r.FilePath] = true
		// The library reads a bare data set as an instance when it has the
		// UIDs of one. A file-set holds Part 10 files (PS3.10 7.1), and one
		// with no file meta information is not one: dcmmkdir refuses it, and
		// a reader that loads what the DICOMDIR names cannot read it.
		if !hasPart10Prefix(r.FilePath) {
			skipped = append(skipped, r.FilePath+" (a data set without the file meta information of PS3.10)")
			continue
		}
		files = append(files, r.FilePath)
	}
	sort.Strings(files)
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !indexed[path] && path != existing {
			skipped = append(skipped, path+" (not a DICOM instance)")
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	sort.Strings(skipped)
	return files, skipped, nil
}

// hasPart10Prefix reports whether a file has the preamble and the DICM
// prefix that begin a PS3.10 file.
func hasPart10Prefix(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 132)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	return string(head[128:]) == "DICM"
}

// nonConformantFiles names every file whose path below root is not a File
// ID.
func nonConformantFiles(root string, files []string) []string {
	var bad []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		if !fileset.ValidFileID(strings.Split(filepath.ToSlash(rel), "/")) {
			bad = append(bad, rel)
		}
	}
	return bad
}

// emptyOrMissing reports an error unless dir does not exist or is an empty
// directory: copied files are never mixed into what is there.
func emptyOrMissing(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.MkdirAll(dir, 0o755)
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty", dir)
	}
	return nil
}

// copyFile copies src to dest, making the directories above it.
func copyFile(src, dest string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// parseDICOMDIRBytes reads a DICOMDIR that has been written.
func parseDICOMDIRBytes(data []byte) (*fileset.DICOMDIR, error) {
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	return fileset.ReadDICOMDIR(df)
}

// countsLine is how many of each record a DICOMDIR holds, as a sentence.
func countsLine(d *fileset.DICOMDIR) string {
	counts := map[string]int{}
	for _, r := range d.Records {
		counts[r.Type]++
	}
	var parts []string
	for _, kind := range []string{fileset.RecordPatient, fileset.RecordStudy, fileset.RecordSeries} {
		parts = append(parts, fmt.Sprintf("%d %s", counts[kind], strings.ToLower(kind)))
		delete(counts, kind)
	}
	var rest []string
	for kind := range counts {
		rest = append(rest, kind)
	}
	sort.Strings(rest)
	for _, kind := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", counts[kind], strings.ToLower(kind)))
	}
	return strings.Join(parts, ", ")
}

// runList prints a DICOMDIR.
func (dc *DICOMDIRCommand) runList(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	if info.IsDir() {
		path = filepath.Join(path, dicomdirName)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("dicomdir: %v", err)
	}
	defer func() { _ = f.Close() }()
	df, err := filereader.ReadDICOMFile(filebase.NewFileReader(f))
	if err != nil {
		return fmt.Errorf("dicomdir: failed to read %s: %v", path, err)
	}
	d, err := fileset.ReadDICOMDIR(df)
	if err != nil {
		return fmt.Errorf("dicomdir: %s: %v", path, err)
	}

	out := orStdout(dc.out)
	if dc.asJSON {
		doc := dicomdirDocument{
			SchemaVersion:   dicomdirSchemaVersion,
			File:            path,
			FileSetID:       d.FileSetID,
			OffsetsWereUsed: d.OffsetsWereUsed,
			Counts:          map[string]int{},
			Records:         []dicomdirRecord{},
		}
		for _, r := range d.Records {
			doc.Counts[r.Type]++
		}
		reached := 0
		for _, root := range d.Roots {
			doc.Records = append(doc.Records, describeRecord(root, &reached))
		}
		doc.Unreachable = len(d.Records) - reached
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		return encoder.Encode(doc)
	}

	fmt.Fprintf(out, "DICOMDIR %s\n", path)
	if d.FileSetID != "" {
		fmt.Fprintf(out, "File-set ID: %s\n", d.FileSetID)
	}
	reached := 0
	for _, root := range d.Roots {
		printRecord(out, root, 0, &reached)
	}
	if n := len(d.Records) - reached; n > 0 {
		fmt.Fprintf(out, "%d record(s) are not reachable from the root, and are not listed above\n", n)
	}
	fmt.Fprintf(out, "%s\n", countsLine(d))
	return nil
}

// dicomdirDocument is what `dicomdir --list --json` prints.
type dicomdirDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	File          string `json:"file"`
	FileSetID     string `json:"fileSetID"`
	// OffsetsWereUsed is false for a file whose records had no usable
	// offsets, where the tree is rebuilt from the order of the records.
	OffsetsWereUsed bool `json:"offsetsWereUsed"`
	// Counts is how many records of each type the file holds.
	Counts map[string]int `json:"counts"`
	// Unreachable is how many records no root leads to.
	Unreachable int              `json:"unreachable"`
	Records     []dicomdirRecord `json:"records"`
}

// dicomdirRecord is a directory record and the records below it.
type dicomdirRecord struct {
	Type  string `json:"type"`
	InUse bool   `json:"inUse"`
	// FileID is the Referenced File ID as its components, for a record that
	// references a file.
	FileID []string `json:"fileID,omitempty"`
	// Attributes are the record's keys, as text.
	Attributes []dicomdirAttribute `json:"attributes"`
	Children   []dicomdirRecord    `json:"children"`
}

// dicomdirAttribute is one key of a record.
type dicomdirAttribute struct {
	Tag     string `json:"tag"`
	Keyword string `json:"keyword,omitempty"`
	Value   string `json:"value"`
}

// structuralTags are those of a record that give its place and what it
// references, and are said by the fields of the record instead.
var structuralTags = map[tag.Tag]bool{
	tag.New(0x0004, 0x1400): true, // Offset of the Next Directory Record
	tag.New(0x0004, 0x1410): true, // Record In-use Flag
	tag.New(0x0004, 0x1420): true, // Offset of Referenced Lower-Level Directory Entity
	tag.New(0x0004, 0x1430): true, // Directory Record Type
	tag.New(0x0004, 0x1500): true, // Referenced File ID
}

// describeRecord is a record for the JSON listing. reached counts the
// records it visits.
func describeRecord(r *fileset.Record, reached *int) dicomdirRecord {
	*reached++
	out := dicomdirRecord{
		Type:       r.Type,
		InUse:      r.InUse,
		FileID:     r.ReferencedFileID,
		Attributes: []dicomdirAttribute{},
		Children:   []dicomdirRecord{},
	}
	if r.Dataset != nil {
		for _, e := range r.Dataset.ElementsInTagOrder() {
			if structuralTags[e.MustTag()] || e.GetVR() == "SQ" {
				continue
			}
			// The keys are text; what is not is not a key of the record.
			raw, ok := e.GetValue().([]byte)
			if !ok {
				continue
			}
			out.Attributes = append(out.Attributes, dicomdirAttribute{
				Tag: e.MustTag().String(), Keyword: e.MustTag().GetKeyword(),
				Value: strings.TrimRight(string(raw), " \x00"),
			})
		}
	}
	for _, c := range r.Children {
		out.Children = append(out.Children, describeRecord(c, reached))
	}
	return out
}

// listedKeys are the attributes the text listing shows of each record type,
// the keys PS3.3 F.5 gives it; any other type shows its file and instance.
var listedKeys = map[string][]string{
	fileset.RecordPatient: {"PatientName", "PatientID"},
	fileset.RecordStudy:   {"StudyDate", "StudyID", "AccessionNumber", "StudyInstanceUID"},
	fileset.RecordSeries:  {"Modality", "SeriesNumber", "SeriesInstanceUID"},
}

// printRecord prints a record and those below it, indented by level.
func printRecord(out io.Writer, r *fileset.Record, level int, reached *int) {
	*reached++
	keys, ok := listedKeys[r.Type]
	if !ok {
		keys = []string{"InstanceNumber", "ReferencedSOPInstanceUIDInFile"}
	}
	var parts []string
	for _, keyword := range keys {
		t := tag.GlobalDictionary().GetByKeyword(keyword)
		if keyword == "ReferencedSOPInstanceUIDInFile" {
			t = tag.New(0x0004, 0x1511)
		}
		if value := r.StringValue(t); value != "" {
			parts = append(parts, keyword+"="+value)
		}
	}
	line := strings.Repeat("  ", level) + r.Type
	if len(r.ReferencedFileID) > 0 {
		line += "  " + r.Path("/")
	}
	if len(parts) > 0 {
		line += "  " + strings.Join(parts, "  ")
	}
	if !r.InUse {
		line += "  [not in use]"
	}
	fmt.Fprintln(out, line)
	for _, c := range r.Children {
		printRecord(out, c, level+1, reached)
	}
}

// showHelp displays help information for the dicomdir command.
func (dc *DICOMDIRCommand) showHelp() {
	printHelp(`COMMAND: dicomdir - Write the DICOMDIR of a directory, or list one

USAGE:
  go-dicom dicomdir <directory> [options]
  go-dicom dicomdir --list <DICOMDIR or directory> [--json]

DESCRIPTION:
  Without --list, index the DICOM files under <directory>, and the
  directories below it, in a DICOMDIR written at its root (PS3.3 Annex F,
  PS3.10): one record for each patient, study, series and instance, and for
  an instance that is not an image the record type of its SOP class. Files
  that are not DICOM are left out and named on standard error.

  With --list, print the records of an existing DICOMDIR as a tree, or as
  JSON with "schemaVersion" 1.

  File IDs. A DICOMDIR names the files it indexes by File IDs, and PS3.10
  8.5 allows at most eight components of at most eight characters from A-Z,
  0-9 and underscore: no lower case, no extension. Most directories do not
  keep to it (ct.dcm, IMG0001.DCM), and an index of names a reader of the
  media cannot follow is wrong. So the command refuses by name: it lists
  every file whose name breaks the rule and writes nothing. It never
  renames a file. To index files as they are on a medium, --copy-to copies
  them into a new directory as DICOM/IM000001, DICOM/IM000002 and so on,
  in the order of their paths, and writes the DICOMDIR there; the originals
  are left as they were.

  A DICOMDIR already in the directory is replaced only with --force, and is
  not itself indexed.

OPTIONS:
  -h, --help            Show help for this command
  --list                List an existing DICOMDIR
  --json                With --list, print the records as JSON
  --copy-to <dir>       Copy the files into <dir>, which must not exist or
                        be empty, under File IDs PS3.10 allows, and write
                        the DICOMDIR there
  --force               Replace a DICOMDIR that is already there
  --file-set-id <id>    The File-set ID, the label of the media (at most
                        16 characters)

EXAMPLES:
  go-dicom dicomdir study/
  go-dicom dicomdir --copy-to media/ export/
  go-dicom dicomdir --list study/DICOMDIR
  go-dicom dicomdir --list --json study/
  go-dicom dicomdir -h
`)
}
