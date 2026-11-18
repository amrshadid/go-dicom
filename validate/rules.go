package validate

// The rules are data: the IOD module tables and the module attribute tables of
// PS3.3, generated into tables.go by scripts/iod-tables.py. Nothing here states
// a rule of its own; the checker reads these and nothing else.

// Usage is how an IOD uses a module (PS3.3 A.1.3).
type Usage int

const (
	// Mandatory (M): the module is always present.
	Mandatory Usage = iota
	// Conditional (C): the module is present when its condition holds. The
	// condition is prose and is kept as text.
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
	// states it, when its description has one.
	Condition string

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
