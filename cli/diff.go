package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/filereader"
	"github.com/amrshadid/go-dicom/tag"
)

// diffSchemaVersion is the version of the document `diff --json` prints. It
// goes up when a field is renamed, removed or changes meaning; a field added
// does not change it.
const diffSchemaVersion = 1

// diffChunk is how much of a bulk value is held at a time while two are
// compared. A multiple of 8, so that a word swap never straddles a chunk.
const diffChunk = 64 * 1024

// DiffCommand compares two DICOM files element by element.
type DiffCommand struct {
	asJSON        bool
	quiet         bool
	ignoreMeta    bool
	ignorePrivate bool
	ignore        diffTagList
	help          bool
	// out is where the report goes; nil means standard output.
	out io.Writer
}

// NewDiffCommand creates a new diff command.
func NewDiffCommand() *DiffCommand {
	return &DiffCommand{}
}

// Name returns the command name.
func (dc *DiffCommand) Name() string {
	return "diff"
}

// Description returns the command description.
func (dc *DiffCommand) Description() string {
	return "Compare two DICOM files element by element"
}

// diffTagList is the repeatable --ignore flag: tags, written as
// (0010,0010), 0010,0010, 00100010 or as a keyword, separated by commas.
type diffTagList []tag.Tag

func (l *diffTagList) String() string { return fmt.Sprint(len(*l)) }

// Set adds the tags of one use of the flag.
func (l *diffTagList) Set(value string) error {
	// A tag is written with a comma in it, so the list is split on the
	// commas that are outside parentheses and not between two hex groups:
	// (0010,0010),(0008,0020) and PatientName,StudyDate are lists,
	// 0010,0010 is a tag. Told apart by trying the whole text first.
	if t, err := parseTagArgument(value); err == nil {
		*l = append(*l, t)
		return nil
	}
	for _, part := range splitTagList(value) {
		t, err := parseTagOrKeyword(part)
		if err != nil {
			return err
		}
		*l = append(*l, t)
	}
	return nil
}

// splitTagList splits on the commas that separate tags.
func splitTagList(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// parseTagOrKeyword reads a tag by its numbers or by its keyword.
func parseTagOrKeyword(s string) (tag.Tag, error) {
	s = strings.TrimSpace(s)
	if t, err := parseTagArgument(s); err == nil {
		return t, nil
	}
	if t := tag.GlobalDictionary().GetByKeyword(s); t != 0 {
		return t, nil
	}
	return 0, fmt.Errorf("%q is neither a tag like (0010,0010) nor a keyword like PatientName", s)
}

// AddFlags adds command flags to the flag set.
func (dc *DiffCommand) AddFlags(fs *flag.FlagSet) {
	fs.BoolVar(&dc.help, "h", false, "Show help for this command")
	fs.BoolVar(&dc.help, "help", false, "Show help for this command")
	fs.BoolVar(&dc.asJSON, "json", false, "Print the differences as JSON")
	fs.BoolVar(&dc.quiet, "quiet", false, "Print nothing; the exit status says whether the files differ")
	fs.BoolVar(&dc.quiet, "q", false, "Print nothing; the exit status says whether the files differ")
	fs.BoolVar(&dc.ignoreMeta, "ignore-meta", false, "Leave the file meta group (0002) out of the comparison")
	fs.BoolVar(&dc.ignorePrivate, "ignore-private", false, "Leave private elements out of the comparison")
	fs.Var(&dc.ignore, "ignore", "A tag or keyword to leave out, at every depth; repeatable, or a comma list")
}

// Execute runs the diff command.
//
// The exit status is the answer, as diff(1)'s is: 0 for equal files, 1 for
// files that differ, 2 for anything that kept the comparison from being made.
func (dc *DiffCommand) Execute(args []string) error {
	err := dc.run(args)
	if err == nil {
		return nil
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return err
	}
	return &ExitError{Code: 2, Err: err}
}

func (dc *DiffCommand) run(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dc.AddFlags(fs)

	positional, err := ParseArgs(fs, args)
	if err != nil {
		return fmt.Errorf("diff: failed to parse flags: %v", err)
	}
	if dc.help {
		dc.showHelp()
		return nil
	}
	if len(positional) != 2 {
		return fmt.Errorf("diff: expected two files, got %d\nRun 'go-dicom diff -h' for usage", len(positional))
	}

	a, err := openDiffFile(positional[0])
	if err != nil {
		return fmt.Errorf("diff: %v", err)
	}
	defer a.close()
	b, err := openDiffFile(positional[1])
	if err != nil {
		return fmt.Errorf("diff: %v", err)
	}
	defer b.close()

	d := &differ{
		ignore:        map[tag.Tag]bool{},
		ignorePrivate: dc.ignorePrivate,
	}
	for _, t := range dc.ignore {
		d.ignore[t] = true
	}
	if !dc.ignoreMeta {
		d.compareSets("", a.df.MetaElements, b.df.MetaElements, a.scope, b.scope)
	}
	d.compareSets("", a.df.DataElements, b.df.DataElements, a.scope, b.scope)
	if d.err != nil {
		return fmt.Errorf("diff: %v", d.err)
	}

	out := orStdout(dc.out)
	if dc.asJSON {
		doc := diffDocument{
			SchemaVersion: diffSchemaVersion,
			A:             positional[0],
			B:             positional[1],
			Equal:         len(d.entries) == 0,
			Warnings: diffWarnings{
				A: append([]string{}, a.df.Warnings...),
				B: append([]string{}, b.df.Warnings...),
			},
			IgnoredTags: []string{},
			IgnoredMeta: dc.ignoreMeta,
			IgnoredPriv: dc.ignorePrivate,
			Differences: append([]diffEntry{}, d.entries...),
		}
		for _, t := range dc.ignore {
			doc.IgnoredTags = append(doc.IgnoredTags, t.String())
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(doc); err != nil {
			return fmt.Errorf("diff: %v", err)
		}
	} else if !dc.quiet && len(d.entries) > 0 {
		writeDiffText(out, positional[0], positional[1], d.entries)
	}
	// A file the reader could only partly read is compared as far as it went.
	// Said on standard error, where the report is not, so that "no
	// difference" is not taken for more than it was.
	if !dc.asJSON && !dc.quiet {
		for _, w := range a.df.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s: %s\n", positional[0], w)
		}
		for _, w := range b.df.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s: %s\n", positional[1], w)
		}
	}
	if len(d.entries) > 0 {
		return &ExitError{Code: 1}
	}
	return nil
}

// diffFile is a file opened for comparison.
type diffFile struct {
	f  *os.File
	df *filereader.DICOMFile
	// scope is the root scope of its elements: where a value left in the
	// file is read from.
	scope *displayScope
}

func (f *diffFile) close() { _ = f.f.Close() }

// openDiffFile reads the structure of a file, leaving its bulk values in it.
func openDiffFile(path string) (*diffFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %v", path, err)
	}
	df, err := filereader.ReadDICOMFileWithOptions(filebase.NewFileReader(f),
		filereader.ReadOptions{SkipBulkData: true, BulkDataThreshold: shownBytes})
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to read %s: %v", path, err)
	}
	// As show does: a file that yielded nothing is a mistyped name, and
	// would compare equal to any other.
	if len(df.MetaElements)+len(df.DataElements) == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%s does not look like a DICOM file: no data elements could be read from it", path)
	}
	root := &displayScope{deciding: dataset.NewDataset(), source: df.ValueSource(f), bigEndian: !df.IsLittleEndian && !df.Deflated}
	return &diffFile{f: f, df: df, scope: root}, nil
}

// diffDocument is what `diff --json` prints.
type diffDocument struct {
	SchemaVersion int `json:"schemaVersion"`
	// A and B are the two files as they were named.
	A string `json:"a"`
	B string `json:"b"`
	// Equal is true when there are no differences.
	Equal bool `json:"equal"`
	// Warnings are what the reader noticed in each file: a truncated file
	// is compared as far as it could be read, and says so here.
	Warnings    diffWarnings `json:"warnings"`
	IgnoredTags []string     `json:"ignoredTags"`
	IgnoredMeta bool         `json:"ignoredFileMeta"`
	IgnoredPriv bool         `json:"ignoredPrivate"`
	Differences []diffEntry  `json:"differences"`
}

// diffWarnings are the warnings of reading each file.
type diffWarnings struct {
	A []string `json:"a"`
	B []string `json:"b"`
}

// diffEntry is one difference: an element or an item present in only one
// file, or an element present in both whose VR or value differ.
type diffEntry struct {
	// Path locates the element: the tags of the sequences it is inside,
	// each followed by the index of its item, then its own tag, as in
	// (0008,1140)[0]/(0008,1150).
	Path    string `json:"path"`
	Tag     string `json:"tag"`
	Keyword string `json:"keyword,omitempty"`
	Name    string `json:"name,omitempty"`
	// Kind is "onlyInA", "onlyInB" or "changed".
	Kind string `json:"kind"`
	// Item is the index of the item, for a whole item present in one
	// sequence and not the other; Path then ends in it.
	Item *int `json:"item,omitempty"`
	// Differs says what differs in a changed element: "vr" and/or "value".
	Differs []string `json:"differs,omitempty"`
	// A and B describe the element in each file, where it is there.
	A *diffValue `json:"a,omitempty"`
	B *diffValue `json:"b,omitempty"`
	// FirstDifference is the offset, in bytes of the value, of the first
	// byte that differs in a bulk or binary value.
	FirstDifference *int64 `json:"firstDifference,omitempty"`
	// Detail is a sentence for a difference the other fields do not say,
	// such as which fragment of encapsulated pixel data differs.
	Detail string `json:"detail,omitempty"`
}

// diffValue is an element as one file has it.
type diffValue struct {
	VR string `json:"vr"`
	// Value is the value as text, absent for bulk and binary values.
	Value *string `json:"value,omitempty"`
	// Length is the bytes of the value in the file.
	Length int64 `json:"length"`
	// Items is how many items a sequence has, or the number of elements of
	// an item that is the whole difference.
	Items *int `json:"items,omitempty"`
	// Fragments is how many items encapsulated pixel data has, the Basic
	// Offset Table first.
	Fragments *int `json:"fragments,omitempty"`
}

// differ collects the differences of one comparison.
type differ struct {
	ignore        map[tag.Tag]bool
	ignorePrivate bool
	entries       []diffEntry
	// err is the first failure to read a value out of a file.
	err error
}

// keyedElement is an element and which of those with its tag it is: a data
// set may repeat a tag, and the repeats are matched in order.
type keyedElement struct {
	tag tag.Tag
	n   int
}

// group indexes elements by tag and occurrence, and lists the keys in
// order.
func group(elements []*filereader.DataElementValue) (map[keyedElement]*filereader.DataElementValue, []keyedElement) {
	index := make(map[keyedElement]*filereader.DataElementValue, len(elements))
	keys := make([]keyedElement, 0, len(elements))
	seen := map[tag.Tag]int{}
	for _, e := range elements {
		k := keyedElement{e.Tag, seen[e.Tag]}
		seen[e.Tag]++
		index[k] = e
		keys = append(keys, k)
	}
	return index, keys
}

// compareSets compares the elements of a data set in each file. path is
// where the data set is, empty at the top; outerA and outerB are the scopes
// of the data sets it is an item of.
func (d *differ) compareSets(path string, a, b []*filereader.DataElementValue, outerA, outerB *displayScope) {
	scopeA, scopeB := newDisplayScope(a, outerA), newDisplayScope(b, outerB)
	indexA, keysA := group(a)
	indexB, keysB := group(b)

	seen := make(map[keyedElement]bool, len(keysA)+len(keysB))
	keys := make([]keyedElement, 0, len(keysA)+len(keysB))
	for _, k := range append(keysA, keysB...) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].tag != keys[j].tag {
			return keys[i].tag < keys[j].tag
		}
		return keys[i].n < keys[j].n
	})

	for _, k := range keys {
		if d.ignore[k.tag] || (d.ignorePrivate && k.tag.IsPrivate()) {
			continue
		}
		ea, inA := indexA[k]
		eb, inB := indexB[k]
		p := path + k.tag.String()
		switch {
		case inA && !inB:
			d.only("onlyInA", p, ea, scopeA)
		case inB && !inA:
			d.only("onlyInB", p, eb, scopeB)
		default:
			d.compareElement(p, ea, eb, scopeA, scopeB)
		}
	}
}

func (d *differ) entry(kind, path string, t tag.Tag) diffEntry {
	return diffEntry{
		Path: path, Tag: t.String(), Keyword: t.GetKeyword(), Name: t.GetName(), Kind: kind,
	}
}

// only records an element that one file has and the other lacks.
func (d *differ) only(kind, path string, e *filereader.DataElementValue, scope *displayScope) {
	entry := d.entry(kind, path, e.Tag)
	v := d.describe(e, scope)
	if kind == "onlyInA" {
		entry.A = &v
	} else {
		entry.B = &v
	}
	d.entries = append(d.entries, entry)
}

// isSequence reports whether an element is a sequence.
func isSequence(e *filereader.DataElementValue) bool {
	return e.VR == "SQ" || e.Items != nil
}

// describe is an element as its file has it.
func (d *differ) describe(e *filereader.DataElementValue, scope *displayScope) diffValue {
	vr := scope.vr(e)
	if isSequence(e) {
		n := len(e.Items)
		return diffValue{VR: "SQ", Items: &n}
	}
	if e.ValueSkipped && e.Position.Fragments != nil {
		n := len(e.Position.Fragments)
		var total int64
		for _, f := range e.Position.Fragments {
			total += f.End - f.ValueOffset
		}
		return diffValue{VR: vr, Length: total, Fragments: &n}
	}
	v := diffValue{VR: vr, Length: int64(valueBytes(e))}
	if !e.ValueSkipped {
		if text, _, ok := displayValue(vr, e.Value, scope.encodings); ok {
			v.Value = &text
		}
	}
	return v
}

// compareElement compares an element both files have.
func (d *differ) compareElement(path string, a, b *filereader.DataElementValue, scopeA, scopeB *displayScope) {
	sa, sb := isSequence(a), isSequence(b)
	vrA, vrB := scopeA.vr(a), scopeB.vr(b)
	entry := d.entry("changed", path, a.Tag)

	if sa && sb {
		d.compareItems(path, a, b, scopeA, scopeB)
		return
	}
	// The VR is compared when each file states one. An implicit VR file
	// states none: the VR it is shown under is the dictionary's, or a guess
	// where the dictionary offers a choice (OB or OW), and a copy of the
	// file in another transfer syntax would differ in what the reader guessed
	// and not in what was written.
	if sa != sb || (a.Position.ExplicitVR && b.Position.ExplicitVR && vrA != vrB) {
		entry.Differs = append(entry.Differs, "vr")
	}
	va, vb := d.describe(a, scopeA), d.describe(b, scopeB)
	if sa != sb {
		entry.A, entry.B = &va, &vb
		d.entries = append(d.entries, entry)
		return
	}

	differs, first, detail := d.compareValues(a, b, scopeA, scopeB, vrA, vrB)
	if d.err != nil {
		return
	}
	if differs {
		entry.Differs = append(entry.Differs, "value")
		entry.FirstDifference = first
		entry.Detail = detail
	}
	if len(entry.Differs) == 0 {
		return
	}
	// Text that is equal is not shown as a value that differs: say what
	// each side holds, always, so the VR change is readable.
	entry.A, entry.B = &va, &vb
	d.entries = append(d.entries, entry)
}

// compareItems compares the items of two sequences by position. An item
// that one sequence has beyond the other's last is a difference of its own.
func (d *differ) compareItems(path string, a, b *filereader.DataElementValue, scopeA, scopeB *displayScope) {
	n := max(len(a.Items), len(b.Items))
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("%s[%d]", path, i)
		switch {
		case i >= len(b.Items):
			d.onlyItem("onlyInA", p, a, i, scopeA)
		case i >= len(a.Items):
			d.onlyItem("onlyInB", p, b, i, scopeB)
		default:
			d.compareSets(p+"/", a.Items[i].Elements, b.Items[i].Elements, scopeA, scopeB)
		}
	}
}

// onlyItem records an item one sequence has and the other lacks.
func (d *differ) onlyItem(kind, path string, seq *filereader.DataElementValue, i int, scope *displayScope) {
	entry := d.entry(kind, path, seq.Tag)
	entry.Item = &i
	n := len(seq.Items[i].Elements)
	v := diffValue{VR: "SQ", Items: &n}
	if kind == "onlyInA" {
		entry.A = &v
	} else {
		entry.B = &v
	}
	d.entries = append(d.entries, entry)
}

// compareValues reports whether two values differ, and where in a binary
// one.
//
// What is compared is what the value means, not its bytes, where the bytes
// are one of several ways of writing it: the padding that makes a text
// value even in length, the character set a text is in, and the byte order
// of a number do not make two files differ. Everything else is compared
// byte for byte, a bulk value being read from each file in chunks.
func (d *differ) compareValues(a, b *filereader.DataElementValue, scopeA, scopeB *displayScope, vrA, vrB string) (differs bool, first *int64, detail string) {
	fragA, fragB := a.ValueSkipped && a.Position.Fragments != nil, b.ValueSkipped && b.Position.Fragments != nil
	if fragA || fragB {
		return d.compareFragments(a, b, scopeA, scopeB)
	}

	if !a.ValueSkipped && !b.ValueSkipped {
		vr := vrA
		if vr == "" {
			vr = vrB
		}
		if vrA == vrB || vrA == "" || vrB == "" || !a.Position.ExplicitVR || !b.Position.ExplicitVR {
			ta, _, okA := displayValue(vr, a.Value, scopeA.encodings)
			tb, _, okB := displayValue(vr, b.Value, scopeB.encodings)
			if okA && okB {
				return ta != tb, nil, ""
			}
		}
		// Different VRs, or no text to compare: the bytes, less padding.
		return !bytes.Equal(trimPad(a.Value), trimPad(b.Value)), firstByteDifference(a.Value, b.Value), ""
	}

	ra, na := d.valueReader(a, scopeA)
	rb, nb := d.valueReader(b, scopeB)
	at, err := streamsDiffer(ra, rb, wordSize(a, vrA, scopeA), wordSize(b, vrB, scopeB))
	if err != nil {
		d.err = err
		return false, nil, ""
	}
	if at < 0 && na == nb {
		return false, nil, ""
	}
	if at < 0 {
		at = min(na, nb)
	}
	return true, &at, ""
}

// trimPad drops the trailing space and NUL a text value is padded with.
func trimPad(v []byte) []byte {
	return bytes.TrimRight(v, " \x00")
}

// firstByteDifference is where two byte slices first differ, or nil when
// they are the same bytes.
func firstByteDifference(a, b []byte) *int64 {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			at := int64(i)
			return &at
		}
	}
	if len(a) == len(b) {
		return nil
	}
	at := int64(n)
	return &at
}

// valueReader is an element's value, from memory or from the file it was
// left in, and how long it is.
func (d *differ) valueReader(e *filereader.DataElementValue, scope *displayScope) (io.Reader, int64) {
	if !e.ValueSkipped {
		return bytes.NewReader(e.Value), int64(len(e.Value))
	}
	n := int64(valueBytes(e))
	return io.NewSectionReader(scope.source, e.Position.ValueOffset, n), n
}

// wordSize is the width of the words of a bulk value, which a big endian
// file writes the other way round. A little endian file's are left as they
// are.
//
// The width is the one the reader swaps at, from the same function: the VR's
// for most values (2 for OW, 4 for OL and OF, 8 for OD and OV, none for OB and
// UN), and for native Pixel Data the sample width, which Bits Allocated gives.
// Taking OW's two bytes for Pixel Data of 32-bit samples transposed each
// sample's halves on one side only, so a big endian RT Dose and its correct
// little endian copy were reported as different from the first byte (#426).
func wordSize(e *filereader.DataElementValue, vr string, scope *displayScope) int {
	if !scope.bigEndian {
		return 1
	}
	bitsAllocated := 0
	if e.Tag == tag.New(0x7FE0, 0x0010) {
		// Read little endian: the reader normalizes the values it keeps.
		if elem, ok := scope.deciding.Get(tag.New(0x0028, 0x0100)); ok {
			if raw, isBytes := elem.GetValue().([]byte); isBytes && len(raw) >= 2 {
				bitsAllocated = int(binary.LittleEndian.Uint16(raw))
			}
		}
	}
	if width := dataelem.PixelDataSwapWidth(dataelem.VR(vr), bitsAllocated); width > 1 {
		return width
	}
	return 1
}

// streamsDiffer compares two byte streams chunk by chunk and returns the
// offset of the first byte that differs, or -1 when they are the same up to
// the end of the shorter. A size above 1 reverses the words of that stream
// first, as a big endian file's are.
func streamsDiffer(a, b io.Reader, sizeA, sizeB int) (int64, error) {
	bufA, bufB := make([]byte, diffChunk), make([]byte, diffChunk)
	var offset int64
	for {
		na, errA := io.ReadFull(a, bufA)
		nb, errB := io.ReadFull(b, bufB)
		if errA != nil && errA != io.EOF && errA != io.ErrUnexpectedEOF {
			return 0, errA
		}
		if errB != nil && errB != io.EOF && errB != io.ErrUnexpectedEOF {
			return 0, errB
		}
		swapWords(bufA[:na], sizeA)
		swapWords(bufB[:nb], sizeB)
		n := min(na, nb)
		for i := 0; i < n; i++ {
			if bufA[i] != bufB[i] {
				return offset + int64(i), nil
			}
		}
		if na != nb {
			return offset + int64(n), nil
		}
		if na < diffChunk {
			return -1, nil
		}
		offset += int64(na)
	}
}

// swapWords reverses the bytes of each whole word of buf, in place.
func swapWords(buf []byte, size int) {
	if size <= 1 {
		return
	}
	for i := 0; i+size <= len(buf); i += size {
		for l, r := i, i+size-1; l < r; l, r = l+1, r-1 {
			buf[l], buf[r] = buf[r], buf[l]
		}
	}
}

// compareFragments compares encapsulated pixel data by its fragments: how
// many, then each one's bytes. The Basic Offset Table is the first.
func (d *differ) compareFragments(a, b *filereader.DataElementValue, scopeA, scopeB *displayScope) (bool, *int64, string) {
	fa, fb := a.Position.Fragments, b.Position.Fragments
	if (a.ValueSkipped && a.Position.Fragments == nil) || (b.ValueSkipped && b.Position.Fragments == nil) ||
		len(fa) == 0 || len(fb) == 0 {
		return true, nil, "one is encapsulated and the other is not"
	}
	if len(fa) != len(fb) {
		return true, nil, fmt.Sprintf("%d fragments against %d", len(fa), len(fb))
	}
	for i := range fa {
		ra := io.NewSectionReader(scopeA.source, fa[i].ValueOffset, fa[i].End-fa[i].ValueOffset)
		rb := io.NewSectionReader(scopeB.source, fb[i].ValueOffset, fb[i].End-fb[i].ValueOffset)
		la, lb := fa[i].End-fa[i].ValueOffset, fb[i].End-fb[i].ValueOffset
		at, err := streamsDiffer(ra, rb, 1, 1)
		if err != nil {
			d.err = err
			return false, nil, ""
		}
		if at < 0 && la == lb {
			continue
		}
		if at < 0 {
			at = min(la, lb)
		}
		name := fmt.Sprintf("fragment %d", i)
		if i == 0 {
			name = "the Basic Offset Table"
		}
		return true, &at, fmt.Sprintf("%s differs from byte %d (%d bytes against %d)", name, at, la, lb)
	}
	return false, nil, ""
}

// writeDiffText prints the differences in a form that reads as a diff: a
// line for where, then the element as each file has it, - for the first
// and + for the second.
func writeDiffText(out io.Writer, a, b string, entries []diffEntry) {
	fmt.Fprintf(out, "--- %s\n+++ %s\n", a, b)
	for _, e := range entries {
		head := e.Path
		if e.Name != "" {
			head += " " + e.Name
		}
		switch e.Kind {
		case "onlyInA":
			fmt.Fprintf(out, "%s\n- %s\n", head, e.A.text(nil))
		case "onlyInB":
			fmt.Fprintf(out, "%s\n+ %s\n", head, e.B.text(nil))
		default:
			fmt.Fprintf(out, "%s\n- %s\n", head, e.A.text(e.B.Value))
			fmt.Fprintf(out, "+ %s\n", e.B.text(e.A.Value))
			if e.FirstDifference != nil {
				fmt.Fprintf(out, "  first difference at byte %d\n", *e.FirstDifference)
			}
			if e.Detail != "" {
				fmt.Fprintf(out, "  %s\n", e.Detail)
			}
		}
	}
}

// excerptRunes is the longest text printed whole.
const excerptRunes = 120

// text is a value as one line: its VR and its text or its size. other is
// the same value in the other file, so that a long text is cut around
// where the two differ and not at its start.
func (v *diffValue) text(other *string) string {
	switch {
	case v.Items != nil && v.VR == "SQ":
		return fmt.Sprintf("SQ <%d items>", *v.Items)
	case v.Fragments != nil:
		return fmt.Sprintf("%s <encapsulated, %d fragments, %d bytes>", v.VR, *v.Fragments, v.Length)
	case v.Value == nil:
		return fmt.Sprintf("%s <%d bytes>", v.VR, v.Length)
	}
	s := []rune(*v.Value)
	if len(s) <= excerptRunes {
		return strings.TrimRight(fmt.Sprintf("%s %s", v.VR, *v.Value), " ")
	}
	at := 0
	if other != nil {
		o := []rune(*other)
		for at < len(s) && at < len(o) && s[at] == o[at] {
			at++
		}
	}
	start := max(0, at-30)
	end := min(len(s), start+excerptRunes)
	cut := string(s[start:end])
	if start > 0 {
		cut = "..." + cut
	}
	if end < len(s) {
		cut += "..."
	}
	return fmt.Sprintf("%s %s (%d characters, from character %d)", v.VR, cut, len(s), start+1)
}

// showHelp displays help information for the diff command.
func (dc *DiffCommand) showHelp() {
	printHelp(`COMMAND: diff - Compare two DICOM files element by element

USAGE:
  go-dicom diff <a> <b> [options]

DESCRIPTION:
  Compare the elements of two files, into sequences and items, the file
  meta group included. It reports the elements only one file has, and those
  both have whose VR or value differ, and prints nothing when there is no
  difference.

  Elements are matched by tag, and the items of a sequence by position. A
  value is compared as what it means: the padding of a text value, the
  character set a text is in and the byte order of a number do not make
  files differ. Bulk values, Pixel Data above all, are read from the two
  files in chunks and never held whole; encapsulated Pixel Data is compared
  fragment by fragment. A VR is compared where both files state one: an
  implicit VR file states none. The File Meta Information Group Length
  (0002,0000) is not compared: it counts the bytes of the group, which a
  writer works out.

  Each difference is printed as a path, then the element in the first file
  after a minus and in the second after a plus. A path is the tags of the
  sequences an element is in, each with its item's index:
  (0008,1140)[0]/(0008,1150).

EXIT STATUS:
  0  the files are equal
  1  the files differ
  2  the comparison could not be made: a file is missing or is not DICOM,
     or the arguments are wrong

OPTIONS:
  -h, --help        Show help for this command
  --json            Print the differences as JSON, with "schemaVersion" 1,
                    each with its path, tag, kind (onlyInA, onlyInB or
                    changed), what differs ("vr", "value") and the element
                    as each file has it. The document is printed whether
                    or not the files differ; "equal" says which.
  -q, --quiet       Print nothing; the exit status is the answer
  --ignore <tag>    Leave an element out, at every depth. A tag as
                    (0010,0010), 0010,0010 or 00100010, or a keyword such
                    as PatientName. Repeat the option, or separate tags
                    with commas.
  --ignore-meta     Leave the file meta group (0002) out
  --ignore-private  Leave private elements out

EXAMPLES:
  go-dicom diff original.dcm anonymized.dcm
  go-dicom diff a.dcm b.dcm --ignore-meta --ignore SOPInstanceUID,StudyDate
  go-dicom diff a.dcm b.dcm --json
  go-dicom diff -q a.dcm b.dcm && echo same
  go-dicom diff -h
`)
}
