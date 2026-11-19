package validate

import (
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// Severity is how much a finding matters.
type Severity int

const (
	// NotChecked is a rule the checker could not apply: a condition PS3.3
	// states in prose, an attribute of a repeating group, the functional
	// groups of an IOD whose Functional Group Macros table is not its own. It
	// is reported so that nothing is passed silently; it says nothing about
	// the instance.
	NotChecked Severity = iota
	// Warning is a value PS3.3 does not define where it allows others: a
	// Defined Term not in the list.
	Warning
	// Error is a rule the instance breaks: a Type 1 attribute absent or
	// empty, a Type 2 attribute absent, a value that is not one of the
	// Enumerated Values, a wrong number of values or items.
	Error
)

// String names the severity as the reports print it.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	}
	return "not checked"
}

// MarshalText makes a severity read as its name in JSON.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding is one thing the checker found, or could not check.
type Finding struct {
	Severity Severity `json:"severity"`
	// Tag is the attribute, and Path where it is: its tag at the top level,
	// or the sequences and items above it, "(0008,1140)[1] (0008,1155)".
	// Both are empty for a finding about a module.
	Tag  string `json:"tag,omitempty"`
	Path string `json:"path,omitempty"`
	Name string `json:"name,omitempty"`
	// Module is the module whose table states the rule, and Reference the
	// section and table of PS3.3 it is in.
	Module    string `json:"module"`
	Reference string `json:"reference"`
	// Rule names the rule: "Type 1", "Type 2C", "Enumerated Values",
	// "Defined Terms", "Value Multiplicity", "Items", "Usage C".
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Report is what Dataset found for one instance.
type Report struct {
	SOPClassUID string    `json:"sopClassUID"`
	IOD         string    `json:"iod"`
	IODSection  string    `json:"iodSection"`
	Edition     string    `json:"edition"`
	Findings    []Finding `json:"findings"`
}

// Count is how many findings are of a severity.
func (r *Report) Count(s Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// Worst is the highest severity found, NotChecked when there is nothing else.
func (r *Report) Worst() Severity {
	worst := NotChecked
	for _, f := range r.Findings {
		worst = max(worst, f.Severity)
	}
	return worst
}

var (
	tagSOPClassUID = tag.New(0x0008, 0x0016)
	// The module that makes an IOD enhanced: what each frame holds is in
	// the IOD's Functional Group Macros table.
	multiFrameFunctionalGroups = "Multi-frame Functional Groups"
)

// Dataset checks a data set against the IOD of its SOP Class UID (0008,0016):
// every module the IOD uses, and in each module every attribute's Type,
// value multiplicity (from the dictionary), Enumerated Values and Defined
// Terms, and the number of items of a sequence where PS3.3 states it, into
// sequences at any depth.
//
// A module is checked when it is Mandatory, and when it is Conditional or a
// User Option and one of its attributes is present. Whatever PS3.3 states in
// prose is reported as NotChecked: the condition of a Conditional module and
// of a Type 1C or 2C attribute that is absent. A Type 1C attribute that is
// present must still have a value.
//
// It is an error when the data set has no SOP Class UID, or one whose IOD this
// package has no rules for.
func Dataset(ds *dataset.Dataset) (*Report, error) {
	if ds == nil {
		return nil, fmt.Errorf("validate: no data set")
	}
	sopClass := text(ds, tagSOPClassUID)
	if sopClass == "" {
		return nil, fmt.Errorf("validate: the data set has no SOP Class UID (0008,0016), so its IOD is not known")
	}
	return DatasetAs(ds, sopClass)
}

// DatasetAs is Dataset for the IOD of the SOP class given, for a data set
// whose own SOP Class UID is missing or not to be trusted: a file's Media
// Storage SOP Class UID (0002,0002) names it too. The data set is still
// checked for its own SOP Class UID, which the SOP Common module requires.
func DatasetAs(ds *dataset.Dataset, sopClass string) (*Report, error) {
	if ds == nil {
		return nil, fmt.Errorf("validate: no data set")
	}
	iod, ok := IODForSOPClass(sopClass)
	if !ok {
		return nil, fmt.Errorf("validate: no IOD rules for SOP class %s: it is not a Storage SOP class of PS3.4 "+
			"Table B.5-1 whose IOD this package was generated with (PS3.3 %s)", sopClass, standardEdition)
	}

	c := &checker{findings: map[string]bool{}}
	c.report = &Report{SOPClassUID: sopClass, IOD: iod.Name, IODSection: iod.Section, Edition: standardEdition}

	// Each module's table, and how many modules of the IOD name each tag at
	// the top level, for telling whether an optional module is there.
	roots := make([][]*node, len(iod.Modules))
	named := map[uint32]int{}
	for i, use := range iod.Modules {
		roots[i] = tree(use.Table)
		for t := range topLevelTags(roots[i]) {
			named[t]++
		}
	}
	for i, use := range iod.Modules {
		if _, ok := tables[use.Table]; !ok {
			c.add(Finding{Severity: NotChecked, Module: use.Module, Reference: "PS3.3 " + iod.Section + " Table " + iod.Table,
				Rule: "Usage " + use.Usage.String(), Message: "the module's attribute table was not generated, so it was not checked"})
			continue
		}
		others := map[uint32]int{}
		own := topLevelTags(roots[i])
		for t, n := range named {
			if own[t] {
				n--
			}
			others[t] = n
		}
		c.module(ds, iod, use, roots[i], others)
	}
	return c.report, nil
}

type checker struct {
	report   *Report
	findings map[string]bool // reported already, so a rule in two modules is said once
}

// add records a finding unless the same one is already there. What was not
// checked is said once for every item it applies to: its path is given with
// the item numbers left out, "(0040,A730)[*] (0040,A043)".
func (c *checker) add(f Finding) {
	if f.Severity == NotChecked {
		f.Path = itemNumber.ReplaceAllString(f.Path, "[*]")
	}
	key := fmt.Sprint(f.Severity, "|", f.Path, "|", f.Rule, "|", f.Message)
	if f.Path == "" {
		key += "|" + f.Module
	}
	if c.findings[key] {
		return
	}
	c.findings[key] = true
	c.report.Findings = append(c.report.Findings, f)
}

var itemNumber = regexp.MustCompile(`\[\d+\]`)

// maxNesting is how many times a table may include itself, directly or not,
// on one path. An SR content tree nests without limit: the Document Content
// Macro includes the Document Relationship Macro, which includes the
// Document Content Macro again. Items nested deeper than the tables were
// expanded are reported as not checked.
const maxNesting = 12

// group is one inclusion of a macro under a condition, such as the Numeric
// Measurement Macro "if and only if Value Type (0040,A040) is NUM".
type group struct {
	table     *Table
	condition string
}

// node is an attribute of a module with the attributes of its items below
// it, and the conditional inclusions it is in, outermost first.
type node struct {
	row      Row
	table    *Table
	groups   []*group
	children []*node
	// deeper marks where the expansion stopped: Include names the table
	// that would have been included again.
	deeper bool
}

// expand flattens a table, its includes resolved, into rows at absolute
// depths, each with the table it came from.
func expand(label string, depth int, groups []*group, seen map[string]int, out *[]node) {
	t, ok := tables[label]
	if !ok {
		return
	}
	if seen[label] >= maxNesting {
		*out = append(*out, node{row: Row{Depth: depth, Include: label}, table: t, groups: groups, deeper: true})
		return
	}
	seen[label]++
	defer func() { seen[label]-- }()
	for _, r := range t.Rows {
		if r.Include != "" {
			inner := groups
			if r.Condition != "" {
				if included, ok := tables[r.Include]; ok {
					inner = append(append([]*group(nil), groups...), &group{table: included, condition: r.Condition})
				}
			}
			expand(r.Include, depth+r.Depth, inner, seen, out)
			continue
		}
		r.Depth += depth
		*out = append(*out, node{row: r, table: t, groups: groups})
	}
}

// tree arranges a module's rows into attributes with their items' attributes
// below them.
func tree(label string) []*node {
	var flat []node
	expand(label, 0, nil, map[string]int{}, &flat)
	var roots []*node
	var stack []*node // the last row seen at each depth
	for i := range flat {
		n := &flat[i]
		d := n.row.Depth
		switch {
		case d == 0 || len(stack) == 0:
			roots = append(roots, n)
		case d <= len(stack):
			stack[d-1].children = append(stack[d-1].children, n)
		default:
			// Deeper than the row above allows: the table is not nested as
			// written. Kept under the deepest row there is.
			stack[len(stack)-1].children = append(stack[len(stack)-1].children, n)
		}
		stack = append(stack[:min(d, len(stack))], n)
	}
	return roots
}

// topLevelTags are the tags a module's table names at the top level.
func topLevelTags(roots []*node) map[uint32]bool {
	tags := map[uint32]bool{}
	for _, n := range roots {
		if !n.deeper && n.row.Mask == 0 {
			tags[n.row.Tag] = true
		}
	}
	return tags
}

// module checks one module of the IOD. others are the top-level tags of the
// IOD's other modules.
func (c *checker) module(ds *dataset.Dataset, iod *IOD, use ModuleUsage, roots []*node, others map[uint32]int) {
	table := tables[use.Table]
	ref := "PS3.3 " + iod.Section + " Table " + iod.Table

	// A module that need not be there is there when an attribute only it
	// names is. One that another module of the IOD also names says nothing:
	// Pixel Spacing is in the SC Image module as well as Image Plane, and
	// taking it for Image Plane reported a secondary capture for lacking
	// Image Position (Patient).
	present, anyPresent, own := false, false, 0
	for _, n := range roots {
		if n.deeper {
			continue
		}
		_, ok := find(ds, n.row)
		shared := n.row.Mask == 0 && others[n.row.Tag] > 0
		if !shared {
			own++
		}
		if ok {
			anyPresent = true
			if !shared {
				present = true
			}
		}
	}
	if own == 0 {
		present = anyPresent
	}

	switch use.Usage {
	case UserOption:
		if !present {
			return
		}
	case Conditional:
		holds, known, why := evaluate(ds, use.When, nil)
		f := Finding{Severity: Error, Module: use.Module, Reference: ref, Rule: "Usage C"}
		switch {
		case known && holds && !anyPresent:
			// One finding for the module, not one for each of its
			// attributes: none of them is there.
			f.Message = "absent; the condition holds, so the module is required: " + use.Condition
			c.add(f)
			return
		case known && holds:
			// Required, and checked as a Mandatory module is. Only
			// attributes another module names as well may be here, as in
			// a palette color image whose Palette Color Lookup Table
			// module has the descriptors and data the Image Pixel module
			// also names, and none of its own.
		case known && !holds && !present:
			return
		case known && !holds && !use.When.Otherwise:
			f.Message = "present; the condition does not hold, and PS3.3 does not say the module may be present " +
				"otherwise (A.1.3.2): " + use.Condition
			c.add(f)
		case known:
			// Present, and allowed to be: checked like any other.
		default:
			f.Severity = NotChecked
			f.Message = "absent; whether it is required was not checked" + why + ": " + use.Condition
			if present {
				f.Message = "present, and checked; whether it is required was not checked" + why + ": " + use.Condition
			}
			c.add(f)
			if !present {
				return
			}
		}
	}
	c.attributes(ds, roots, "", use.Module, table)
	if use.Module == multiFrameFunctionalGroups {
		if len(iod.FunctionalGroups) == 0 {
			c.add(Finding{Severity: NotChecked, Module: use.Module, Reference: ref, Rule: "Functional Group Macros",
				Message: "the IOD has no Functional Group Macros table that was generated; only the module's own " +
					"attributes were checked"})
			return
		}
		c.functionalGroups(ds, iod)
	}
}

var (
	tagSharedGroups   = tag.New(0x5200, 0x9229)
	tagPerFrameGroups = tag.New(0x5200, 0x9230)
	tagNumberOfFrames = tag.New(0x0028, 0x0008)
)

// functionalGroups checks the Shared and Per-Frame Functional Groups
// Sequences against the IOD's Functional Group Macros table: each macro the
// IOD requires is in the shared item or in the item of every frame, none is
// in both (PS3.3 Table C.7.6.16-1: "The selected Functional Group Macros
// shall not be present in the Per-Frame Functional Groups Sequence"), none is
// where the table says it may not be, and each that is there is checked
// attribute by attribute, as a module is.
func (c *checker) functionalGroups(ds *dataset.Dataset, iod *IOD) {
	ref := "PS3.3 " + iod.Section + " Table " + iod.FunctionalGroupsTable
	moduleRef := "PS3.3 C.7.6.16 (Multi-frame Functional Groups Module Attributes)"
	var shared *dataset.Dataset
	if seq := sequenceOf(ds, tagSharedGroups); seq != nil && seq.Length() > 0 {
		shared, _ = seq.Items()[0].(*dataset.Dataset)
	}
	var frames []*dataset.Dataset
	if seq := sequenceOf(ds, tagPerFrameGroups); seq != nil {
		for _, raw := range seq.Items() {
			item, _ := raw.(*dataset.Dataset)
			frames = append(frames, item)
		}
	}

	// "The number of Items shall be the same as the number of Frames in the
	// Multi-frame Image" (Table C.7.6.16-1).
	if n, err := strconv.Atoi(text(ds, tagNumberOfFrames)); err == nil && frames != nil && len(frames) != n {
		c.add(Finding{Severity: Error, Tag: tagPerFrameGroups.String(), Path: tagPerFrameGroups.String(),
			Name: "Per-Frame Functional Groups Sequence", Module: multiFrameFunctionalGroups, Reference: moduleRef,
			Rule: "Items", Message: fmt.Sprintf("%d item(s); the sequence holds one for each frame, and Number of "+
				"Frames (0028,0008) is %d", len(frames), n)})
	}

	for _, fg := range iod.FunctionalGroups {
		table, ok := tables[fg.Table]
		module := fg.Macro + " (functional group)"
		f := Finding{Severity: Error, Module: module, Reference: ref, Rule: "Usage " + fg.Usage.String()}
		if !ok {
			f.Severity, f.Message = NotChecked, "the macro's attribute table was not generated, so it was not checked"
			c.add(f)
			continue
		}
		roots := tree(fg.Table)
		own := topLevelTags(roots)
		holds := func(item *dataset.Dataset) bool {
			if item == nil {
				return false
			}
			for t := range own {
				if _, ok := item.Get(tag.Tag(t)); ok {
					return true
				}
			}
			return false
		}
		inShared := holds(shared)
		var with, without []int // frames, counted from 1
		for i, item := range frames {
			if holds(item) {
				with = append(with, i+1)
			} else {
				without = append(without, i+1)
			}
		}
		present := inShared || len(with) > 0

		switch {
		case inShared && len(with) > 0:
			f.Rule = "Functional Groups"
			f.Message = fmt.Sprintf("in the shared item and in %d per-frame item(s); a macro is in one or the "+
				"other (Table C.7.6.16-1)", len(with))
			c.add(f)
		case inShared && fg.Placement == PerFrameOnly:
			f.Rule = "Functional Groups"
			f.Message = "in the shared item; the IOD's table says it may not be used as a Shared Functional Group"
			c.add(f)
		case len(with) > 0 && fg.Placement == SharedOnly:
			f.Rule = "Functional Groups"
			f.Message = "in the per-frame items; the IOD's table says it may not be used as a Per-Frame Functional Group"
			c.add(f)
		}

		required := fg.Usage == Mandatory
		if fg.Usage == Conditional {
			h, known, why := evaluate(ds, fg.When, nil)
			switch {
			case known && h:
				required = true
			case known && present && !fg.When.Otherwise:
				f.Message = "present; the condition does not hold, and PS3.3 does not say the macro may be present " +
					"otherwise: " + fg.Condition
				c.add(f)
			case !known:
				g := f
				g.Severity = NotChecked
				g.Message = "absent; whether it is required was not checked" + why + ": " + fg.Condition
				if present {
					g.Message = "present, and checked; whether it is required was not checked" + why + ": " + fg.Condition
				}
				c.add(g)
			}
		}
		if required {
			reason := "the macro is required"
			if fg.Usage == Conditional {
				reason = "the condition holds, so the macro is required: " + fg.Condition
			}
			switch {
			case !present:
				f.Message = "absent from the shared item and from every per-frame item; " + reason
				c.add(f)
			case !inShared && len(without) > 0:
				f.Message = fmt.Sprintf("absent from %d of the %d per-frame items (frame %d first) and from the "+
					"shared item; %s", len(without), len(frames), without[0], reason)
				c.add(f)
			}
		}

		if inShared {
			c.attributes(shared, roots, tagSharedGroups.String()+"[1] ", module, table)
		}
		if len(with) > 0 {
			c.perFrame(frames, with, roots, module, table)
		}
	}
}

// perFrame checks a macro in the per-frame items that hold it. What is wrong
// in many frames is said once, with how many: an image of a thousand frames
// missing one attribute in each is one finding, not a thousand.
func (c *checker) perFrame(frames []*dataset.Dataset, with []int, roots []*node, module string, table *Table) {
	sub := &checker{report: &Report{}, findings: map[string]bool{}}
	for _, i := range with {
		sub.attributes(frames[i-1], roots, fmt.Sprintf("%s[%d] ", tagPerFrameGroups, i), module, table)
	}
	type seen struct {
		f      Finding
		frames []int
	}
	var order []string
	byKey := map[string]*seen{}
	for _, f := range sub.report.Findings {
		frame := 0
		if m := leadingItem.FindStringSubmatch(f.Path); m != nil {
			frame, _ = strconv.Atoi(m[1])
		}
		f.Path = leadingItem.ReplaceAllString(f.Path, tagPerFrameGroups.String()+"[*]")
		key := fmt.Sprint(f.Severity, "|", f.Path, "|", f.Rule, "|", f.Message)
		if byKey[key] == nil {
			byKey[key] = &seen{f: f}
			order = append(order, key)
		}
		byKey[key].frames = append(byKey[key].frames, frame)
	}
	for _, key := range order {
		s := byKey[key]
		f := s.f
		if f.Severity != NotChecked {
			if len(s.frames) == 1 {
				f.Path = strings.Replace(f.Path, "[*]", fmt.Sprintf("[%d]", s.frames[0]), 1)
			} else {
				f.Message += fmt.Sprintf(" (in %d of %d frames, frame %d first)", len(s.frames), len(frames), s.frames[0])
			}
		}
		c.add(f)
	}
}

var leadingItem = regexp.MustCompile(`^\(5200,9230\)\[(\d+|\*)\]`)

// sequenceOf is the sequence a data set holds at a tag, or nil.
func sequenceOf(ds *dataset.Dataset, t tag.Tag) *sequence.Sequence {
	elem, ok := ds.Get(t)
	if !ok {
		return nil
	}
	seq, _ := elem.GetValue().(*sequence.Sequence)
	return seq
}

// attributes checks attributes of one data set, the top level or an item.
func (c *checker) attributes(ds *dataset.Dataset, nodes []*node, path, module string, moduleTable *Table) {
	// A macro included under a condition is checked as written when one of
	// its attributes is here, and is otherwise one finding that it was not
	// checked: its condition is prose.
	//
	// As for a module, only an attribute no other inclusion here names
	// counts. The macros for SCOORD and SCOORD3D content items both have
	// Graphic Type and Graphic Data; taking a 2D item for 3D held its CIRCLE
	// against the 3D macro's Enumerated Values.
	namedBy := map[uint32]map[*group]bool{}
	for _, n := range nodes {
		if len(n.groups) == 0 || n.deeper || n.row.Mask != 0 {
			continue
		}
		if namedBy[n.row.Tag] == nil {
			namedBy[n.row.Tag] = map[*group]bool{}
		}
		namedBy[n.row.Tag][n.groups[len(n.groups)-1]] = true
	}
	groupPresent, hasOwn := map[*group]bool{}, map[*group]bool{}
	for _, n := range nodes {
		if len(n.groups) > 0 && !n.deeper && n.row.Mask == 0 && len(namedBy[n.row.Tag]) == 1 {
			hasOwn[n.groups[len(n.groups)-1]] = true
		}
	}
	for _, n := range nodes {
		if len(n.groups) == 0 || n.deeper {
			continue
		}
		inner := n.groups[len(n.groups)-1]
		own := n.row.Mask != 0 || len(namedBy[n.row.Tag]) == 1
		if !own && hasOwn[inner] {
			continue
		}
		if _, ok := find(ds, n.row); ok {
			for _, g := range n.groups {
				groupPresent[g] = true
			}
		}
	}

	// The attributes a condition in an item may name: those of the item's
	// own table. An item's condition naming any other is about some other
	// data set (the top level, an enclosing item), and which one is not
	// said; it is not evaluated. At the top level the whole data set is in
	// reach.
	var siblings map[uint32]bool
	if path != "" {
		siblings = map[uint32]bool{}
		for _, n := range nodes {
			if !n.deeper && n.row.Mask == 0 {
				siblings[n.row.Tag] = true
			}
		}
	}

	repeating := false
	for _, n := range nodes {
		r := n.row
		if absent := firstAbsent(n.groups, groupPresent); absent != nil {
			c.add(Finding{Severity: NotChecked, Path: path, Module: module,
				Reference: "PS3.3 " + absent.table.Section + " (" + absent.table.Name + ")", Rule: "Include",
				Message: "none of its attributes is here; whether it is required was not checked: " + absent.condition})
			continue
		}
		if n.deeper {
			if len(ds.Tags()) > 0 {
				c.add(Finding{Severity: NotChecked, Path: path, Module: module,
					Reference: "PS3.3 " + n.table.Section + " (" + n.table.Name + ")", Rule: "Nesting",
					Message: fmt.Sprintf("items nested deeper than %d levels of this table were not checked", maxNesting)})
			}
			continue
		}
		ref := "PS3.3 " + n.table.Section + " (" + n.table.Name + ")"
		if r.Mask != 0 {
			repeating = true
			continue
		}
		t := tag.Tag(r.Tag)
		where := path + t.String()
		f := Finding{Tag: t.String(), Path: where, Name: r.Name, Module: module, Reference: ref,
			Rule: "Type " + r.Type.String()}

		elem, ok := ds.Get(t)
		if !ok {
			switch r.Type {
			case Type1, Type2:
				f.Severity, f.Message = Error, "absent; a Type "+r.Type.String()+" attribute is required"
				c.add(f)
			case Type1C, Type2C:
				holds, known, why := evaluate(ds, r.When, siblings)
				switch {
				case known && holds:
					f.Severity = Error
					f.Message = "absent; the condition holds, so a Type " + r.Type.String() +
						" attribute is required: " + r.Condition
					c.add(f)
				case known:
					// Not required, and not there: nothing to say.
				default:
					f.Severity = NotChecked
					f.Message = "absent; whether it is required was not checked" + why
					if r.Condition != "" {
						f.Message += ": " + r.Condition
					}
					c.add(f)
				}
			}
			continue
		}

		// Present where its condition does not hold, and PS3.3 does not say
		// it may be present otherwise: PS3.5 7.4.2 and 7.4.4 say it shall
		// not be included.
		if (r.Type == Type1C || r.Type == Type2C) && r.When != nil && !r.When.Otherwise {
			if holds, known, _ := evaluate(ds, r.When, siblings); known && !holds {
				g := f
				g.Severity = Error
				g.Message = "present; the condition does not hold, and PS3.3 does not say it may be present " +
					"otherwise (PS3.5 7.4): " + r.Condition
				c.add(g)
			}
		}

		seq, isSeq := elem.GetValue().(*sequence.Sequence)
		empty := isEmpty(elem, isSeq, seq)
		if empty && (r.Type == Type1 || r.Type == Type1C) {
			f.Severity = Error
			f.Message = "present and empty; a Type " + r.Type.String() + " attribute has a value"
			if isSeq {
				f.Message = "present with no items; a Type " + r.Type.String() + " sequence has at least one"
			}
			c.add(f)
		}
		if isSeq {
			c.items(seq, n, where, module, ref, moduleTable)
			continue
		}
		if empty {
			continue
		}
		values := valuesOf(ds, t, elem)
		c.multiplicity(t, values, f)
		c.enumerated(r, values, f)
	}
	if repeating && path == "" {
		c.add(Finding{Severity: NotChecked, Module: module, Reference: "PS3.3 " + moduleTable.Section,
			Rule: "Repeating group", Message: "the module's attributes in repeating groups, such as (60xx,eeee), were not checked"})
	}
}

// evaluate decides a condition on a data set. known is false when it cannot
// be decided, and why then says why, as a clause to follow "not checked": the
// condition is prose (no Condition), it names an attribute outside the item
// (siblings, when not nil, are the attributes in reach), or the value it
// tests is not one value of text or numbers.
func evaluate(ds *dataset.Dataset, cond *Condition, siblings map[uint32]bool) (holds, known bool, why string) {
	if cond == nil {
		return false, false, ""
	}
	t := tag.Tag(cond.Tag)
	if siblings != nil && !siblings[cond.Tag] {
		return false, false, " (the condition names " + t.String() + ", which is not in this item)"
	}
	elem, present := ds.Get(t)
	switch cond.Test {
	case IsPresent:
		return present, true, ""
	case IsAbsent:
		return !present, true, ""
	case HasValue, IsPresentWithValue:
	default:
		return false, false, ""
	}
	if !present {
		return false, true, ""
	}
	if _, isSeq := elem.GetValue().(*sequence.Sequence); isSeq {
		return false, false, " (the condition tests the value of " + t.String() + ", a sequence)"
	}
	values := valuesOf(ds, t, elem)
	if values == nil {
		return false, false, " (the value of " + t.String() + " is not text or numbers)"
	}
	var v string
	switch {
	case cond.Value > 0 && cond.Value > len(values):
		return false, true, ""
	case cond.Value > 0:
		v = values[cond.Value-1]
	case len(values) > 1:
		// "has a Value of X" said of an attribute holding several: whether
		// one of them, or all, is meant is not said.
		return false, false, fmt.Sprintf(" (the condition tests one value of %s, which has %d)", t, len(values))
	default:
		v = values[0]
	}
	return v != "" && matchesTerm(v, cond.Values), true, ""
}

// firstAbsent is the outermost conditional inclusion of groups none of whose
// attributes is present, or nil.
func firstAbsent(groups []*group, present map[*group]bool) *group {
	for _, g := range groups {
		if !present[g] {
			return g
		}
	}
	return nil
}

// items checks a sequence's item count and each item's attributes.
func (c *checker) items(seq *sequence.Sequence, n *node, where, module, ref string, moduleTable *Table) {
	// No items at all is the Type's to judge: an error for Type 1, allowed
	// for 2 and 3, whatever the description says of the items it holds.
	if count := seq.Length(); n.row.Items != nil && count > 0 {
		ic := *n.row.Items
		if count < ic.Min || ic.Max > 0 && count > ic.Max {
			c.add(Finding{Severity: Error, Tag: tag.Tag(n.row.Tag).String(), Path: where, Name: n.row.Name,
				Module: module, Reference: ref, Rule: "Items",
				Message: fmt.Sprintf("%d item(s); the sequence holds %s", count, itemRange(ic))})
		}
	}
	for i, raw := range seq.Items() {
		item, ok := raw.(*dataset.Dataset)
		if !ok {
			continue
		}
		c.attributes(item, n.children, fmt.Sprintf("%s[%d] ", where, i+1), module, moduleTable)
	}
}

func itemRange(ic ItemCount) string {
	switch {
	case ic.Min == ic.Max:
		return fmt.Sprintf("exactly %d", ic.Min)
	case ic.Max == 0:
		return fmt.Sprintf("%d or more", ic.Min)
	}
	return fmt.Sprintf("%d to %d", ic.Min, ic.Max)
}

// multiplicity checks the number of values against the dictionary's VM.
func (c *checker) multiplicity(t tag.Tag, values []string, f Finding) {
	info, ok := tag.StandardDicomDictionary[uint32(t)]
	if !ok || values == nil {
		return
	}
	low, high, step, ok := parseVM(info.VM)
	if !ok {
		return
	}
	n := len(values)
	if n < low || high > 0 && n > high || step > 1 && n%step != 0 {
		f.Severity, f.Rule = Error, "Value Multiplicity"
		f.Message = fmt.Sprintf("%d value(s); the dictionary gives VM %s", n, info.VM)
		f.Reference = "PS3.6"
		c.add(f)
	}
}

// parseVM reads a VM as PS3.6 writes it: "1", "1-3", "1-n", "2-2n".
func parseVM(vm string) (low, high, step int, ok bool) {
	vm = strings.TrimSpace(vm)
	parts := strings.SplitN(vm, "-", 2)
	low, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false
	}
	if len(parts) == 1 {
		return low, low, 1, true
	}
	hi := parts[1]
	if strings.HasSuffix(hi, "n") {
		step = 1
		if s := strings.TrimSuffix(hi, "n"); s != "" {
			if step, err = strconv.Atoi(s); err != nil {
				return 0, 0, 0, false
			}
		}
		return low, 0, step, true
	}
	high, err = strconv.Atoi(hi)
	if err != nil {
		return 0, 0, 0, false
	}
	return low, high, 1, true
}

// enumerated checks the values against the Enumerated Values and Defined
// Terms of the row.
func (c *checker) enumerated(r Row, values []string, f Finding) {
	for _, list := range r.Values {
		for i, v := range values {
			if list.Value != 0 && list.Value != i+1 {
				continue
			}
			if v == "" || matchesTerm(v, list.Terms) {
				continue
			}
			g := f
			g.Rule, g.Severity = "Defined Terms", Warning
			if list.Enumerated {
				g.Rule, g.Severity = "Enumerated Values", Error
			}
			if r.ValuesFrom != "" {
				g.Reference = "PS3.3 " + r.ValuesFrom
			}
			which := "the value"
			if len(values) > 1 || list.Value != 0 {
				which = fmt.Sprintf("value %d", i+1)
			}
			kind := "Defined Terms"
			if list.Enumerated {
				kind = "Enumerated Values"
			}
			g.Message = fmt.Sprintf("%s is %q; the %s are %s", which, v, kind, strings.Join(list.Terms, ", "))
			c.add(g)
		}
	}
}

// matchesTerm compares a value with the terms, as text and, for a term
// written as a number ("16", "0001H"), as a number.
func matchesTerm(v string, terms []string) bool {
	for _, term := range terms {
		if v == term {
			return true
		}
		if a, ok := number(v); ok {
			if b, ok := number(term); ok && a == b {
				return true
			}
		}
	}
	return false
}

func number(s string) (float64, bool) {
	if strings.HasSuffix(s, "H") {
		if n, err := strconv.ParseUint(strings.TrimSuffix(s, "H"), 16, 64); err == nil {
			return float64(n), true
		}
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, err == nil && !math.IsNaN(n)
}

// find reports whether a row's attribute is in the data set: the tag, or for
// a repeating group any tag of it.
func find(ds *dataset.Dataset, r Row) (*dataelem.DataElement, bool) {
	if r.Mask == 0 {
		return ds.Get(tag.Tag(r.Tag))
	}
	for _, t := range ds.Tags() {
		if uint32(t)&r.Mask == r.Tag&r.Mask {
			return ds.Get(t)
		}
	}
	return nil, false
}

// isEmpty reports whether a value is zero length, or text of padding alone.
func isEmpty(elem *dataelem.DataElement, isSeq bool, seq *sequence.Sequence) bool {
	if isSeq {
		return seq.Length() == 0
	}
	// Left in the file by a reader that skips bulk data: its length is all
	// the Type needs.
	if v, inFile := elem.GetValue().(dataelem.ValueInFile); inFile {
		return v.Length == 0
	}
	// Through Bytes, so a value held as a []string or a number is judged as the
	// same value read from a file (#420); a value that holds no bytes is not
	// empty, it is something this cannot see into.
	raw, ok := elem.Bytes()
	if !ok {
		return false
	}
	if dataelem.IsTextVR(elem.GetVR()) {
		return strings.Trim(string(raw), " \x00") == ""
	}
	return len(raw) == 0
}

// valuesOf splits a value into its values as text, numbers written in
// decimal, or nil when the VR holds one value of bytes.
func valuesOf(ds *dataset.Dataset, t tag.Tag, elem *dataelem.DataElement) []string {
	raw, ok := elem.Bytes()
	if !ok {
		return nil
	}
	vr := ds.ResolveVR(t, elem)
	switch vr {
	case dataelem.LT, dataelem.ST, dataelem.UT, dataelem.UR:
		return []string{strings.TrimRight(string(raw), " \x00")}
	case dataelem.US, dataelem.SS, dataelem.UL, dataelem.SL, dataelem.FL, dataelem.FD,
		dataelem.UV, dataelem.SV, dataelem.AT:
		return binaryValues(vr, raw)
	}
	if !dataelem.IsTextVR(vr) {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(raw), " \x00"), `\`)
	for i := range parts {
		parts[i] = strings.Trim(parts[i], " \x00")
	}
	return parts
}

// binaryValues renders the numbers of a binary VR as decimal text.
func binaryValues(vr dataelem.VR, raw []byte) []string {
	size := map[dataelem.VR]int{dataelem.US: 2, dataelem.SS: 2, dataelem.UL: 4, dataelem.SL: 4,
		dataelem.FL: 4, dataelem.FD: 8, dataelem.UV: 8, dataelem.SV: 8, dataelem.AT: 4}[vr]
	if size == 0 || len(raw)%size != 0 {
		return nil
	}
	out := make([]string, 0, len(raw)/size)
	for off := 0; off < len(raw); off += size {
		b := raw[off : off+size]
		var s string
		switch vr {
		case dataelem.US:
			s = strconv.FormatUint(uint64(binary.LittleEndian.Uint16(b)), 10)
		case dataelem.SS:
			s = strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(b))), 10)
		case dataelem.AT:
			// A group and an element, as PS3.3 writes a tag in a list of
			// terms: 00181063 for (0018,1063).
			s = fmt.Sprintf("%04X%04X", binary.LittleEndian.Uint16(b), binary.LittleEndian.Uint16(b[2:]))
		case dataelem.UL:
			s = strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 10)
		case dataelem.SL:
			s = strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(b))), 10)
		case dataelem.FL:
			s = strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), 'g', -1, 32)
		case dataelem.FD:
			s = strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(b)), 'g', -1, 64)
		case dataelem.UV:
			s = strconv.FormatUint(binary.LittleEndian.Uint64(b), 10)
		case dataelem.SV:
			s = strconv.FormatInt(int64(binary.LittleEndian.Uint64(b)), 10)
		}
		out = append(out, s)
	}
	return out
}

// text reads a text value without its padding.
func text(ds *dataset.Dataset, t tag.Tag) string {
	elem, ok := ds.Get(t)
	if !ok {
		return ""
	}
	raw, ok := elem.Bytes()
	if !ok {
		return ""
	}
	return strings.Trim(string(raw), " \x00")
}

// Sort orders the findings: errors first, then warnings, then what was not
// checked, each in order of where they are.
func (r *Report) Sort() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		return a.Path < b.Path
	})
}
