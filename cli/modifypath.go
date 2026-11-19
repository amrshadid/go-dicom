package cli

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/valuerep"
)

// editKind is what an edit does to the attribute its path names.
type editKind int

const (
	// editSet changes an attribute that is there, as dcmodify -m does.
	editSet editKind = iota
	// editInsert sets an attribute whether it is there or not, creating the
	// sequences and items its path passes through, as dcmodify -i does.
	editInsert
	// editDelete removes an attribute, or an item when the path ends in one,
	// as dcmodify -e does.
	editDelete
)

// edit is one -set, -insert or -delete as given on the command line.
type edit struct {
	kind  editKind
	given string
	path  []pathStep
	value string

	// flagName is the flag the edit came from, for the messages. fromFile
	// says value is the name of a file whose bytes are the value, and
	// everywhere that the attribute is looked for in the data set and in
	// every item of every sequence in it, as dcmodify's -ma and -ea do.
	flagName   string
	fromFile   bool
	everywhere bool
}

// pathStep is one component of a path: an attribute, and for a sequence the
// item or items the next component is looked for in.
type pathStep struct {
	tag tag.Tag

	// creator names the private block a private attribute is in, and offset
	// is its element number within the block (the ee of xxee). The block
	// number itself is not known until the data set is looked at: PS3.5
	// 7.8.1 lets each file reserve a different one for the same creator.
	creator string
	offset  uint8

	// vr is a private attribute's VR when the path gives it.
	vr dataelem.VR

	// item is the item from 0, -1 for none; all is [*].
	item int
	all  bool
}

func (s pathStep) hasItem() bool { return s.item >= 0 || s.all }

// errMissing reports that a path names something the file does not have.
var errMissing = errors.New("not in the file")

// parseEdit reads the argument of -set or -insert (path=value) or -delete
// (path).
func parseEdit(kind editKind, arg string) (edit, error) {
	e := edit{kind: kind, given: arg}
	pathText := arg
	if kind != editDelete {
		// The first "=" outside a quoted creator ends the path: a value may
		// hold "=" itself, a person name's groups for one.
		at := indexOutside(arg, '=')
		if at < 0 {
			return e, errors.New("expected path=value")
		}
		pathText, e.value = arg[:at], arg[at+1:]
	}
	path, err := parsePath(pathText)
	if err != nil {
		return e, err
	}
	last := path[len(path)-1]
	if last.hasItem() && kind != editDelete {
		return e, errors.New("the path ends in an item; a value is set on an attribute")
	}
	for _, step := range path {
		if err := refuseTag(step.tag, kind); err != nil {
			return e, err
		}
	}
	// The reader holds every text value as UTF-8 and declares ISO_IR 192 for
	// it, so that is what the file is written in. Another declaration, or
	// none, would describe the text as something it is not.
	if last.tag == specificCharacterSet && last.creator == "" &&
		(kind == editDelete || strings.TrimSpace(e.value) != utf8Term) {
		return e, fmt.Errorf("text is read into UTF-8 and written as %s; SpecificCharacterSet may only be set to that", utf8Term)
	}
	e.path = path
	return e, nil
}

// specificCharacterSet is (0008,0005), and utf8Term its defined term for UTF-8.
var specificCharacterSet = tag.New(0x0008, 0x0005)

const utf8Term = "ISO_IR 192"

// refuseTag rejects what a path may not name.
func refuseTag(t tag.Tag, kind editKind) error {
	switch {
	case t.Group() == 0x0002:
		// The writer makes the file meta from the data set and the transfer
		// syntax; a value given here would be overwritten or contradict them.
		return fmt.Errorf("%s is in the file meta group, which is written from the data set", t)
	case t.Element() == 0x0000:
		return fmt.Errorf("%s is a group length, which the writer computes", t)
	case t.Group() == 0xFFFE:
		return fmt.Errorf("%s is an item or delimiter, not an attribute", t)
	case t == tag.New(0x7FE0, 0x0010) && kind != editDelete:
		return fmt.Errorf("%s, Pixel Data, is not set from the command line", t)
	}
	return nil
}

// indexOutside returns the index of the first c outside double quotes and
// parentheses, or -1.
func indexOutside(s string, c byte) int {
	depth, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			quoted = !quoted
		case quoted:
		case s[i] == '(':
			depth++
		case s[i] == ')':
			depth--
		case s[i] == c && depth == 0:
			return i
		}
	}
	return -1
}

// parsePath reads a path: components joined by ".", each an attribute with
// an optional [n] or [*].
func parsePath(text string) ([]pathStep, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the path is empty")
	}
	var steps []pathStep
	for rest := text; ; {
		at := indexOutside(rest, '.')
		component := rest
		if at >= 0 {
			component = rest[:at]
		}
		step, err := parseStep(component)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
		if at < 0 {
			break
		}
		rest = rest[at+1:]
	}
	for _, step := range steps[:len(steps)-1] {
		if !step.hasItem() {
			return nil, fmt.Errorf("%s is followed by another attribute, so it needs an item: [0] for the first, [*] for all", step.tag)
		}
	}
	return steps, nil
}

// parseStep reads one component.
func parseStep(text string) (pathStep, error) {
	step := pathStep{item: -1}
	text = strings.TrimSpace(text)
	if strings.HasSuffix(text, "]") {
		open := strings.LastIndex(text, "[")
		if open < 0 {
			return step, fmt.Errorf("%q has a ] with no [", text)
		}
		index := text[open+1 : len(text)-1]
		text = text[:open]
		if index == "*" {
			step.all = true
		} else {
			n, err := strconv.Atoi(index)
			if err != nil || n < 0 {
				return step, fmt.Errorf("[%s] is not an item number; items count from 0, or [*] for all", index)
			}
			step.item = n
		}
	}

	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		fields := splitOutsideQuotes(text[1:len(text)-1], ',')
		switch len(fields) {
		case 2:
			t, err := parseGroupElement(fields[0], fields[1])
			step.tag = t
			return step, err
		case 3, 4:
			return parsePrivateStep(step, fields)
		}
		return step, fmt.Errorf("%q is not a tag: (gggg,eeee) or (gggg,xxee,\"CREATOR\")", text)
	}
	if group, element, ok := strings.Cut(text, ","); ok {
		t, err := parseGroupElement(group, element)
		step.tag = t
		return step, err
	}
	if len(text) == 8 && isHex(text) {
		n, _ := strconv.ParseUint(text, 16, 32)
		step.tag = tag.Tag(n)
		return step, nil
	}
	t := tag.GlobalDictionary().GetByKeyword(text)
	if t == 0 {
		return step, fmt.Errorf("%q is neither a tag nor a keyword the dictionary knows", text)
	}
	step.tag = t
	return step, nil
}

// parsePrivateStep reads (gggg,xxee,"CREATOR") and (gggg,xxee,"CREATOR",VR).
func parsePrivateStep(step pathStep, fields []string) (pathStep, error) {
	group, err := strconv.ParseUint(strings.TrimSpace(fields[0]), 16, 16)
	if err != nil || group%2 == 0 {
		return step, fmt.Errorf("a private creator is given for group %s, which is not a private (odd) group", fields[0])
	}
	element := strings.ToLower(strings.TrimSpace(fields[1]))
	if len(element) != 4 || !strings.HasPrefix(element, "xx") || !isHex(element[2:]) {
		return step, fmt.Errorf("%q: with a creator the element is xxee, where the block xx is the file's own", fields[1])
	}
	offset, _ := strconv.ParseUint(element[2:], 16, 8)
	creator := strings.TrimSpace(fields[2])
	if len(creator) < 2 || creator[0] != '"' || creator[len(creator)-1] != '"' {
		return step, fmt.Errorf("the private creator %s must be in double quotes", creator)
	}
	creator = creator[1 : len(creator)-1]
	if creator == "" {
		return step, errors.New("the private creator is empty")
	}
	step.tag = tag.New(uint16(group), 0x1000|uint16(offset))
	step.creator = creator
	step.offset = uint8(offset)
	if len(fields) == 4 {
		vr := dataelem.VR(strings.ToUpper(strings.TrimSpace(fields[3])))
		if !valuerep.IsValidVR(string(vr)) {
			return step, fmt.Errorf("%q is not a VR", fields[3])
		}
		step.vr = vr
	}
	return step, nil
}

// parseGroupElement reads the two hexadecimal halves of a tag.
func parseGroupElement(group, element string) (tag.Tag, error) {
	g, errG := strconv.ParseUint(strings.TrimSpace(group), 16, 16)
	e, errE := strconv.ParseUint(strings.TrimSpace(element), 16, 16)
	if errG != nil || errE != nil {
		return 0, fmt.Errorf("(%s,%s) is not a tag of four hexadecimal digits each", group, element)
	}
	return tag.New(uint16(g), uint16(e)), nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return s != ""
}

// splitOutsideQuotes splits s at sep where sep is not inside double quotes.
func splitOutsideQuotes(s string, sep byte) []string {
	var parts []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			quoted = !quoted
		case s[i] == sep && !quoted:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// checkMode holds an edit to what its flag allows once its path is parsed.
func (e edit) checkMode() error {
	if e.everywhere && (len(e.path) != 1 || e.path[0].hasItem()) {
		return fmt.Errorf("%s names one attribute, not a path: it is looked for in every item already", e.flagName)
	}
	if e.fromFile && strings.TrimSpace(e.value) == "" {
		return errors.New("expected path=file, and the file name is empty")
	}
	return nil
}

// apply makes the edit in root and returns how many attributes or items it
// changed.
func (e edit) apply(root *dataset.Dataset) (int, error) {
	if e.everywhere {
		n, err := e.applyEverywhere(root, nil)
		if err == nil && n == 0 {
			return 0, fmt.Errorf("%s: %w", e.path[0].tag, errMissing)
		}
		return n, err
	}
	return e.applyIn(root, nil, e.path)
}

// applyEverywhere makes the edit in ds, wherever its attribute is, and in
// every item of every sequence under it, to any depth. A data set that does
// not have the attribute is passed over, not refused: that is the point of
// asking for every occurrence. The attribute is dealt with before the items
// of the sequences are entered, so a deleted sequence is not walked.
func (e edit) applyEverywhere(ds *dataset.Dataset, enclosing []*dataset.Dataset) (int, error) {
	changed := 0
	t, err := e.locate(ds, e.path[0])
	switch {
	case errors.Is(err, errMissing):
	case err != nil:
		return 0, err
	case ds.Contains(t):
		n, err := e.applyIn(ds, enclosing, e.path)
		if err != nil {
			return changed, err
		}
		changed += n
	}
	inner := append([]*dataset.Dataset{ds}, enclosing...)
	for _, elem := range ds.GetAll() {
		seq, ok := elem.GetValue().(*sequence.Sequence)
		if !ok {
			continue
		}
		for _, raw := range seq.Items() {
			item, ok := raw.(*dataset.Dataset)
			if !ok {
				continue
			}
			n, err := e.applyEverywhere(item, inner)
			if err != nil {
				return changed, err
			}
			changed += n
		}
	}
	return changed, nil
}

// applyIn makes the rest of the edit in ds, an item of enclosing (nearest
// first), which is where an item's VRs and character set are found.
func (e edit) applyIn(ds *dataset.Dataset, enclosing []*dataset.Dataset, steps []pathStep) (int, error) {
	step := steps[0]
	t, err := e.locate(ds, step)
	if err != nil {
		return 0, err
	}
	elem, present := ds.Get(t)

	if len(steps) > 1 || (step.hasItem() && e.kind == editDelete) {
		if !present {
			if e.kind != editInsert {
				return 0, fmt.Errorf("%s: %w", t, errMissing)
			}
			if _, err := ds.CreateSequence(t); err != nil {
				return 0, err
			}
			elem, _ = ds.Get(t)
		}
		seq, ok := elem.GetValue().(*sequence.Sequence)
		if !ok {
			return 0, fmt.Errorf("%s is not a sequence, so it has no items", t)
		}
		if len(steps) == 1 {
			return deleteItems(seq, step, t)
		}
		items, err := e.itemsOf(seq, step, t)
		if err != nil {
			return 0, err
		}
		inner := append([]*dataset.Dataset{ds}, enclosing...)
		changed := 0
		for _, item := range items {
			n, err := e.applyIn(item, inner, steps[1:])
			if err != nil {
				return changed, err
			}
			changed += n
		}
		return changed, nil
	}

	switch e.kind {
	case editDelete:
		if !present {
			return 0, fmt.Errorf("%s: %w", t, errMissing)
		}
		ds.Remove(t)
		return 1, nil
	case editSet:
		if !present {
			return 0, fmt.Errorf("%s: %w", t, errMissing)
		}
	}

	vr, err := valueVR(ds, enclosing, t, elem, present, step)
	if err != nil {
		return 0, err
	}
	if vr == dataelem.SQ {
		if e.value != "" {
			return 0, fmt.Errorf("%s is a sequence; it holds items, not a value. Name an attribute inside it: %s[0].Keyword", t, t)
		}
		if present {
			return 0, nil // an empty insert keeps the items it has
		}
		_, err := ds.CreateSequence(t)
		return 1, err
	}
	var value []byte
	if e.fromFile {
		value, err = encodeFileValue(vr, e.value)
	} else {
		value, err = encodeValue(t, vr, e.value, characterSetOf(ds, enclosing))
	}
	if err != nil {
		return 0, fmt.Errorf("%s %s: %w", t, vr, err)
	}
	return 1, ds.Add(dataelem.NewDataElement(t, vr, value))
}

// locate finds the tag a step names in ds. A private creator with no block
// here is missing, except for an insert, which reserves one.
func (e edit) locate(ds *dataset.Dataset, step pathStep) (tag.Tag, error) {
	if step.creator == "" {
		if step.tag.IsPrivate() && e.kind == editInsert && step.tag.Element() >= 0x1000 && !ds.Contains(step.tag) {
			// A private attribute by number alone is only meaningful in the
			// block its creator reserved (PS3.5 7.8.1); without that
			// reservation the number names nothing.
			creator := tag.New(step.tag.Group(), step.tag.Element()>>8)
			if !ds.Contains(creator) {
				return 0, fmt.Errorf("%s has no private creator at %s; name it as (%04X,xx%02X,\"CREATOR\")",
					step.tag, creator, step.tag.Group(), step.tag.Element()&0xFF)
			}
		}
		return step.tag, nil
	}
	group := step.tag.Group()
	if !ds.HasPrivateCreator(group, step.creator) && e.kind != editInsert {
		return 0, fmt.Errorf("(%04X,xx%02X,%q): %w", group, step.offset, step.creator, errMissing)
	}
	block, err := ds.PrivateBlock(group, step.creator)
	if err != nil {
		return 0, err
	}
	return block.GetTag(step.offset), nil
}

// itemsOf returns the items a step names, creating them up to the one named
// for an insert.
func (e edit) itemsOf(seq *sequence.Sequence, step pathStep, t tag.Tag) ([]*dataset.Dataset, error) {
	if step.all {
		var items []*dataset.Dataset
		for _, raw := range seq.Items() {
			if item, ok := raw.(*dataset.Dataset); ok {
				items = append(items, item)
			}
		}
		return items, nil
	}
	for seq.Length() <= step.item {
		if e.kind != editInsert {
			return nil, fmt.Errorf("%s[%d]: %w; it has %d item(s)", t, step.item, errMissing, seq.Length())
		}
		if err := seq.Append(dataset.NewDataset()); err != nil {
			return nil, err
		}
	}
	raw, err := seq.Get(step.item)
	if err != nil {
		return nil, err
	}
	item, ok := raw.(*dataset.Dataset)
	if !ok {
		return nil, fmt.Errorf("%s[%d] is not an item this tool can read", t, step.item)
	}
	return []*dataset.Dataset{item}, nil
}

// deleteItems removes the item a delete path ends in, or all of them.
func deleteItems(seq *sequence.Sequence, step pathStep, t tag.Tag) (int, error) {
	if step.all {
		n := seq.Length()
		seq.Clear()
		return n, nil
	}
	if step.item >= seq.Length() {
		return 0, fmt.Errorf("%s[%d]: %w; it has %d item(s)", t, step.item, errMissing, seq.Length())
	}
	return 1, seq.Remove(step.item)
}

// valueVR settles the VR a value is encoded in: the element's own when it is
// there, else the dictionary's, else the private dictionary's, else the one
// the path gives.
func valueVR(ds *dataset.Dataset, enclosing []*dataset.Dataset, t tag.Tag, elem *dataelem.DataElement, present bool, step pathStep) (dataelem.VR, error) {
	if step.vr != "" {
		return step.vr, nil
	}
	if present {
		if _, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
			return dataelem.SQ, nil
		}
		if vr := ds.ResolveVR(t, elem, enclosing...); vr != dataelem.UN {
			return vr, nil
		}
	}
	if t.GetVR() == string(dataelem.SQ) {
		return dataelem.SQ, nil
	}
	if vr := ds.ResolveVR(t, dataelem.NewDataElement(t, "", nil), enclosing...); vr != dataelem.UN {
		return vr, nil
	}
	if t.IsPrivate() {
		return "", fmt.Errorf("the private dictionary does not know the VR of %s; give it in the path, as (%04X,xx%02X,\"CREATOR\",LO)",
			t, t.Group(), t.Element()&0xFF)
	}
	return "", fmt.Errorf("%s is not in the dictionary, so its VR is not known", t)
}

// characterSetOf returns the Specific Character Set that applies to ds: its
// own, else that of the nearest data set it is an item of (PS3.5 6.1.2.5).
func characterSetOf(ds *dataset.Dataset, enclosing []*dataset.Dataset) []string {
	for _, d := range append([]*dataset.Dataset{ds}, enclosing...) {
		if d == nil {
			continue
		}
		if value := datasetText(d, specificCharacterSet); value != "" || d.Contains(specificCharacterSet) {
			return strings.Split(value, "\\")
		}
	}
	return nil
}

// singleValueVRs never hold more than one value, so a backslash in them is
// text (PS3.5 6.2: LT, ST, UT and UR have a value multiplicity of 1).
var singleValueVRs = map[dataelem.VR]bool{
	dataelem.LT: true, dataelem.ST: true, dataelem.UT: true, dataelem.UR: true,
}

// binaryWordSize is the size in bytes of what each binary VR holds, which
// the length of its value is a multiple of (PS3.5 6.2).
var binaryWordSize = map[dataelem.VR]int{
	dataelem.OB: 1, dataelem.UN: 1, dataelem.OW: 2, dataelem.OF: 4,
	dataelem.OL: 4, dataelem.OD: 8, dataelem.OV: 8,
}

// maxValueLength is the longest value an element of these VRs can hold: the
// length field is 32 bits and 0xFFFFFFFF means undefined (PS3.5 7.1.2).
const maxValueLength = 0xFFFFFFFE

// encodeFileValue reads the bytes of a binary attribute from a file. They
// are the value as the data set holds it, which is little endian: the writer
// swaps the words of a big endian file. The VR is the attribute's own; a
// value from a file is for the binary VRs, and text is given with -set.
func encodeFileValue(vr dataelem.VR, name string) ([]byte, error) {
	size, ok := binaryWordSize[vr]
	if !ok {
		return nil, fmt.Errorf("VR %s is not binary; a value from a file is for OB, OW, OF, OD, OL, OV and UN, and text is given with -set or -insert", vr)
	}
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not a file", name)
	}
	if info.Size() > maxValueLength-1 {
		return nil, fmt.Errorf("%s is %d bytes, more than one value can hold", name, info.Size())
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if len(data)%size != 0 {
		return nil, fmt.Errorf("%s is %d bytes, and a value of VR %s is a whole number of %d-byte words", name, len(data), vr, size)
	}
	// PS3.5 7.1.1: a value has an even length, and OB and UN are padded
	// with a NUL.
	if len(data)%2 == 1 {
		data = append(data, 0)
	}
	return data, nil
}

// encodeValue turns text from the command line into the value bytes of vr,
// refusing what the VR does not allow.
func encodeValue(t tag.Tag, vr dataelem.VR, text string, characterSet []string) ([]byte, error) {
	switch vr {
	case dataelem.OB, dataelem.OW, dataelem.OF, dataelem.OD, dataelem.OL, dataelem.OV, dataelem.UN:
		return nil, fmt.Errorf("a value of VR %s is binary and is not given as text; use -set-file or -insert-file with a file of its bytes", vr)
	}
	if text == "" {
		return []byte{}, nil
	}

	values := []string{text}
	if !singleValueVRs[vr] {
		values = strings.Split(text, "\\")
	}
	if err := checkMultiplicity(t, vr, len(values)); err != nil {
		return nil, err
	}

	switch vr {
	case dataelem.US, dataelem.SS, dataelem.UL, dataelem.SL, dataelem.UV, dataelem.SV, dataelem.FL, dataelem.FD:
		return encodeBinaryNumbers(vr, values)
	case dataelem.AT:
		var out []byte
		for _, v := range values {
			step, err := parseStep(v)
			if err != nil || step.hasItem() || step.creator != "" {
				return nil, fmt.Errorf("%q is not a tag", v)
			}
			out = binary.LittleEndian.AppendUint16(out, step.tag.Group())
			out = binary.LittleEndian.AppendUint16(out, step.tag.Element())
		}
		return out, nil
	}

	for _, v := range values {
		if err := valuerep.ValidateValue(string(vr), v); err != nil {
			return nil, fmt.Errorf("%q: %w", v, err)
		}
	}
	out := []byte(text)
	if dataelem.IsCharacterSetVR(vr) && !isASCII(text) {
		// No Specific Character Set is the default repertoire, which is
		// ASCII (PS3.5 6.1.2.2). The charset package reads it as Latin-1, as
		// pydicom does for a file that breaks the rule, and writing by that
		// reading would make this tool the one breaking it.
		if describeCharacterSet(characterSet) == defaultRepertoire {
			return nil, fmt.Errorf("%q is not ASCII and the file declares no Specific Character Set; "+
				"insert SpecificCharacterSet='ISO_IR 192' before it in the same command", text)
		}
		// The text arrives as UTF-8 and is stored in the set the data set
		// declares. Encoding strictly: a character the set lacks would
		// otherwise become a "?" that nobody asked to write.
		ctx := config.WithWritingValidationMode(context.Background(), config.RAISE)
		encodings, err := charset.NewCharacterSet(characterSet)
		if err != nil {
			return nil, fmt.Errorf("the file's Specific Character Set %q: %w", strings.Join(characterSet, "\\"), err)
		}
		out, err = charset.EncodeStringWithContext(ctx, text, encodings.Encodings)
		if err != nil {
			return nil, fmt.Errorf("%q cannot be written in the file's character set (%s); set SpecificCharacterSet to ISO_IR 192 first",
				text, describeCharacterSet(characterSet))
		}
	} else if !isASCII(text) {
		return nil, fmt.Errorf("%q: VR %s holds the default repertoire only", text, vr)
	}
	// PS3.5 7.1.1: every value has an even length, a UID padded with a NUL
	// and text with a space.
	if len(out)%2 == 1 {
		if vr == dataelem.UI {
			out = append(out, 0)
		} else {
			out = append(out, ' ')
		}
	}
	return out, nil
}

const defaultRepertoire = "the default repertoire"

func describeCharacterSet(values []string) string {
	if len(values) == 0 || strings.TrimSpace(strings.Join(values, "")) == "" {
		return defaultRepertoire
	}
	return strings.Join(values, "\\")
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7F {
			return false
		}
	}
	return true
}

// checkMultiplicity holds the number of values to the dictionary's VM (PS3.6),
// through the same check the element type makes of what it reads.
func checkMultiplicity(t tag.Tag, vr dataelem.VR, n int) error {
	probe := dataelem.NewDataElement(t, vr, nil)
	probe.VM = n
	if err := probe.ValidateValueMultiplicity(); err != nil {
		return fmt.Errorf("%d value(s), and the dictionary gives %s a multiplicity of %s", n, t, t.GetVM())
	}
	return nil
}

// encodeBinaryNumbers writes numbers in the little endian the data set holds
// them in; the writer swaps them for a big endian file.
func encodeBinaryNumbers(vr dataelem.VR, values []string) ([]byte, error) {
	var out []byte
	for _, v := range values {
		v = strings.TrimSpace(v)
		switch vr {
		case dataelem.US, dataelem.UL, dataelem.UV:
			bits := map[dataelem.VR]int{dataelem.US: 16, dataelem.UL: 32, dataelem.UV: 64}[vr]
			n, err := strconv.ParseUint(v, 10, bits)
			if err != nil {
				return nil, fmt.Errorf("%q is not an unsigned %d-bit integer", v, bits)
			}
			switch bits {
			case 16:
				out = binary.LittleEndian.AppendUint16(out, uint16(n))
			case 32:
				out = binary.LittleEndian.AppendUint32(out, uint32(n))
			default:
				out = binary.LittleEndian.AppendUint64(out, n)
			}
		case dataelem.SS, dataelem.SL, dataelem.SV:
			bits := map[dataelem.VR]int{dataelem.SS: 16, dataelem.SL: 32, dataelem.SV: 64}[vr]
			n, err := strconv.ParseInt(v, 10, bits)
			if err != nil {
				return nil, fmt.Errorf("%q is not a signed %d-bit integer", v, bits)
			}
			switch bits {
			case 16:
				out = binary.LittleEndian.AppendUint16(out, uint16(int16(n)))
			case 32:
				out = binary.LittleEndian.AppendUint32(out, uint32(int32(n)))
			default:
				out = binary.LittleEndian.AppendUint64(out, uint64(n))
			}
		case dataelem.FL:
			f, err := strconv.ParseFloat(v, 32)
			if err != nil {
				return nil, fmt.Errorf("%q is not a 32-bit floating point number", v)
			}
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(f)))
		case dataelem.FD:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a 64-bit floating point number", v)
			}
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(f))
		}
	}
	return out, nil
}
