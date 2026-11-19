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
// No bulk value's bytes are needed. A data set read with
// filereader.ReadOptions.SkipBulkData and built with
// GetDatasetWithValuesInFile, its Pixel Data a dataelem.ValueInFile, is
// checked as if it had been read whole; the value's length says whether it is
// empty. go-dicom validate reads files that way.
//
// # What is not checked
//
// Conditions are prose in PS3.3, and none is evaluated. A Conditional module,
// present or not, an absent Type 1C or 2C attribute, and a macro included
// under a condition ("if and only if Value Type (0040,A040) is NUM") with
// none of its attributes present, are each reported as NotChecked, with the
// condition as PS3.3 states it: never passed in silence. Also reported as
// not checked: attributes of repeating groups such as (60xx,eeee), the
// Functional Group Macros of the enhanced multi-frame IODs, and items of an
// SR content tree nested deeper than twelve levels of one table. Not
// checked, and not reported per instance: context groups (PS3.16), rules
// that tie one attribute to another (High Bit is Bits Stored minus one),
// value representation and the form of values, which the valuerep package
// checks, and attributes the IOD does not name.
package validate
