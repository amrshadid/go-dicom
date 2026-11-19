package validate

// The rules are data: the IOD module tables and the module attribute tables of
// PS3.3, generated into tables.go by scripts/iod-tables.py. Nothing here states
// a rule of its own; the checker reads these and nothing else.

// Usage is how an IOD uses a module (PS3.3 A.1.3).
type Usage int

const (
	// Mandatory (M): the module is always present.
	Mandatory Usage = iota
	// Conditional (C): the module is present when its condition holds, and
	// not otherwise unless PS3.3 says it may be (A.1.3.2). The condition is
	// kept as text, and as data when it is in a form that can be read so.
	Conditional
	// UserOption (U): the module may be present.
	UserOption
)

// String is the letter PS3.3 uses.
func (u Usage) String() string {
	switch u {
	case Mandatory:
		return "M"
	case Conditional:
		return "C"
	}
	return "U"
}

// AttributeType is an attribute's Type in a module (PS3.5 7.4).
type AttributeType int

const (
	// Type1 is present with a value.
	Type1 AttributeType = iota
	// Type1C is Type 1 when its condition holds, and absent otherwise.
	Type1C
	// Type2 is present, with a value or empty.
	Type2
	// Type2C is Type 2 when its condition holds, and absent otherwise.
	Type2C
	// Type3 is optional.
	Type3
)

// String is the Type as PS3.3 writes it.
func (t AttributeType) String() string {
	return [...]string{"1", "1C", "2", "2C", "3"}[t]
}

// IOD is one Information Object Definition of PS3.3 Annex A: the modules an
// instance of it holds.
type IOD struct {
	Name    string // "CT Image"
	Section string // "A.3"
	Table   string // the IOD module table, "A.3-1"
	Modules []ModuleUsage
}

// ModuleUsage is one row of an IOD module table.
type ModuleUsage struct {
	Module    string // "General Series"
	Table     string // the module's attribute table, "C.7-5a"
	Usage     Usage
	Condition string // for a Conditional module, the condition as PS3.3 states it
	// When is the condition read as data, when PS3.3 states it in a form
	// that can be (see Condition); nil when it is prose.
	When *Condition
}

// ConditionTest is what a condition asks of the attribute it names.
type ConditionTest int

const (
	// IsPresent: "Required if Extended Offset Table (7FE0,0001) is
	// present."
	IsPresent ConditionTest = iota + 1
	// IsAbsent: "... is absent.", "... is not present." Two attributes
	// that each name the other this way are a pair of which one is
	// required.
	IsAbsent
	// HasValue: "... has a Value of YES.", "... is ORIGINAL or MIXED.",
	// "... equals FULL.", or of one value, "Image Type (0008,0008) Value 1
	// is ORIGINAL." An attribute that is absent does not have the value.
	HasValue
	// IsPresentWithValue: "... is present and has a Value of YES.", "...
	// is present with a Value of TILED_FULL." Evaluated as HasValue; kept
	// apart because PS3.3 words it so.
	IsPresentWithValue
)

// String is the test as PS3.3 words it.
func (t ConditionTest) String() string {
	switch t {
	case IsPresent:
		return "is present"
	case IsAbsent:
		return "is absent"
	case HasValue:
		return "has a value of"
	case IsPresentWithValue:
		return "is present and has a value of"
	}
	return "?"
}

// Condition is a condition PS3.3 states in one of a few fixed sentence forms,
// read as data by the generator: the attribute it names, the test, the values
// it is tested for, and whether PS3.3 says the attribute or module "May be
// present otherwise". The generator reads a condition so only when the whole
// of what the description says about presence is that sentence; one that
// matches a form in part, or names an attribute whose name and tag disagree,
// stays prose and has no Condition.
type Condition struct {
	Test ConditionTest
	Tag  uint32
	// Value is the value tested, counted from 1, for a condition on one
	// value of several ("Value 3 is VOLUME"); 0 for the attribute's value.
	Value int
	// Values are the values the attribute is tested for, any of which
	// satisfies the condition.
	Values []string
	// Otherwise is set when PS3.3 says "May be present otherwise". When it
	// is not, an attribute or module whose condition does not hold shall
	// not be present (PS3.5 7.4.2 and 7.4.4, PS3.3 A.1.3.2).
	Otherwise bool
}

// Table is a module or macro attribute table of PS3.3.
type Table struct {
	Name    string // "General Series Module Attributes"
	Section string // "C.7.3.1"
	Rows    []Row
}

// Row is one row of an attribute table: an attribute, or the inclusion of a
// macro's table at a depth.
//
// Depth is the nesting PS3.3 writes as ">": 0 for an attribute of the data
// set, 1 for an attribute of an item of the sequence above it, and so on.
type Row struct {
	Depth int

	// Include, when set, names the table this row includes at Depth, and the
	// other fields are empty.
	Include string

	// Tag is the attribute's tag. Mask is 0 for an attribute with one tag,
	// and otherwise the bits of Tag that are fixed: an attribute of a
	// repeating group, such as (60xx,3000), has the others free.
	Tag  uint32
	Mask uint32
	Name string
	Type AttributeType

	// Condition is the condition of a Type 1C or 2C attribute, as PS3.3
	// states it, when its description has one. When is that condition read
	// as data, or nil when it is prose.
	Condition string
	When      *Condition

	// Values are the Enumerated Values and Defined Terms the table lists.
	// ValuesFrom, when set, is the section they were taken from, which the
	// description names for the attribute's specialization.
	Values     []ValueList
	ValuesFrom string

	// Items is how many items a sequence may hold, when its description
	// says.
	Items *ItemCount
}

// ValueList is a list of Enumerated Values (the only values allowed) or
// Defined Terms (the values defined, which may be extended), for every value
// of an attribute (Value 0) or for one of them, counted from 1.
type ValueList struct {
	Value      int
	Enumerated bool
	Terms      []string
}

// ItemCount is how many items a sequence holds. Max 0 is no limit.
type ItemCount struct {
	Min, Max int
}

// Edition is the edition of PS3.3 the rules are generated from, "2026d".
func Edition() string { return standardEdition }

// IODForSOPClass returns the IOD an instance of a SOP class is, by PS3.4
// Table B.5-1, and whether this package has its rules.
func IODForSOPClass(sopClassUID string) (*IOD, bool) {
	section, ok := sopClassIODs[sopClassUID]
	if !ok {
		return nil, false
	}
	iod, ok := iods[section]
	return iod, ok
}

// LookupTable returns an attribute table of PS3.3 by its label, "C.7-5a".
func LookupTable(label string) (*Table, bool) {
	t, ok := tables[label]
	return t, ok
}
