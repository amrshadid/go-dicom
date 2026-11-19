// Package validate checks a data set against what its SOP class requires.
//
// PS3.4 Table B.5-1 names the IOD of each Storage SOP class. PS3.3 Annex A
// gives each IOD its modules, each Mandatory (M), Conditional (C) or a User
// Option (U), and Annex C gives each module its attributes, each of Type 1
// (present, with a value), 1C, 2 (present), 2C or 3 (optional), nested into
// sequences, with macros included by reference (PS3.5 7.4). Those tables are
// the rules here. They are generated from the standard's DocBook XML by
// scripts/iod-tables.py, not written by hand, and Edition says which edition
// of PS3.3 they come from.
//
//	report, err := validate.Dataset(ds)
//	for _, f := range report.Findings {
//		fmt.Println(f.Severity, f.Path, f.Name, f.Rule, f.Message, f.Reference)
//	}
//
// # What is checked
//
// Every module the IOD makes Mandatory, and every Conditional or User Option
// module that is present: one an attribute only that module names is in the
// data set. In each module, into sequences at any depth:
//
//   - a Type 1 attribute is present and has a value, a Type 2 attribute is
//     present, and a Type 1C attribute that is present has a value;
//   - the number of values is the value multiplicity the dictionary gives
//     (PS3.6);
//   - a value is one of the Enumerated Values (an Error otherwise) or of the
//     Defined Terms (a Warning otherwise), where the table lists them, or
//     the section its description names for the attribute's specialization
//     does: the CT Image Module allows Bits Stored of 12 to 16 (C.8.2.1.1.5);
//   - a sequence holds as many items as its description says, "Only a
//     single Item shall be included", "One or more Items".
//
// Each Finding gives the attribute's tag, its path through the sequences
// above it, the module, the section and table of PS3.3 the rule is in, the
// rule, and a Severity.
//
// # Conditions
//
// PS3.3 states a share of its conditions in a few fixed sentence forms that
// name one attribute by its tag, and the generator reads those as a
// Condition: "Required if Pixel Data Provider URL (0028,7FE0) is not
// present", "... is present", "... has a Value of YES", "Image Type
// (0008,0008) Value 1 is ORIGINAL or MIXED", "... is present and has a Value
// of X", each optionally followed by "May be present otherwise". Of the 1681
// Type 1C and 2C attributes in the tables of PS3.3 2026d, 580 have one; of
// the 316 Conditional modules of the IODs, 25 do. Such a condition is
// evaluated:
//
//   - a Type 1C or 2C attribute, or a Conditional module, whose condition
//     holds and that is absent is an Error;
//   - one whose condition does not hold and that is present is an Error,
//     unless PS3.3 says it may be present otherwise (PS3.5 7.4.2 and 7.4.4,
//     PS3.3 A.1.3.2);
//   - a Conditional module whose condition holds is checked as a Mandatory
//     one is.
//
// A condition in a sequence item is evaluated in that item, and only when it
// names an attribute of the item's own table; one naming any other is not
// evaluated, as which data set it means is not said. Nor is one testing "a
// Value" of an attribute that holds several.
//
// # What is not checked
//
// A condition of any other form is prose, and is not evaluated. A
// Conditional module, present or not, and an absent Type 1C or 2C
// attribute, whose condition is prose, and a macro included under a
// condition ("if and only if Value Type (0040,A040) is NUM") with none of
// its attributes present, are each reported as NotChecked, with the
// condition as PS3.3 states it: never passed in silence. Also reported as
// not checked: attributes of repeating groups such as (60xx,eeee), the
// Functional Group Macros of the enhanced multi-frame IODs, and items of an
// SR content tree nested deeper than twelve levels of one table. Not
// checked, and not reported per instance: context groups (PS3.16), rules
// that tie one attribute to another (High Bit is Bits Stored minus one),
// value representation and the form of values, which the valuerep package
// checks, and attributes the IOD does not name.
package validate
