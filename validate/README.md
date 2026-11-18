# validate

Checks a data set against the IOD of its SOP class: the modules PS3.3 Annex A
gives the IOD, and in each module the attributes Annex C lists, with their
Type, value multiplicity, Enumerated Values and Defined Terms, and the number
of items a sequence holds.

The rules are generated from the standard (`scripts/iod-tables.py`, from the
DocBook XML of PS3.3 and PS3.4), not written by hand. `Edition()` names the
edition: 153 IODs and 528 module and macro tables, for 164 Storage SOP
classes.

## Quick Start

```go
report, err := validate.Dataset(ds) // by the data set's SOP Class UID
if err != nil {
    // no SOP Class UID, or one with no IOD here
}
for _, f := range report.Findings {
    fmt.Println(f.Severity, f.Path, f.Name, f.Rule, f.Message, f.Reference)
}
```

From the command line:

```
$ go-dicom validate broken_ct.dcm
broken_ct.dcm: 1.2.840.10008.5.1.4.1.1.2, checked as the CT Image IOD (PS3.3 A.3, 2026d)
  error       (0008,0060) Modality: Type 1: absent; a Type 1 attribute is required [General Series; PS3.3 C.7.3.1 (General Series Module Attributes)]
  error       (0028,0101) Bits Stored: Enumerated Values: the value is "99"; the Enumerated Values are 12, 13, 14, 15, 16 [CT Image; PS3.3 C.8.2.1.1.5]
  ...
  not checked Synchronization: Usage C: absent; whether it is required was not checked: Required if time synchronization was applied. [...]
8 error(s), 0 warning(s), 39 not checked
```

## API Reference

```go
func Dataset(ds *dataset.Dataset) (*Report, error)
func DatasetAs(ds *dataset.Dataset, sopClassUID string) (*Report, error)

type Report struct {
    SOPClassUID, IOD, IODSection, Edition string
    Findings []Finding
}
func (r *Report) Count(s Severity) int
func (r *Report) Worst() Severity
func (r *Report) Sort()

type Finding struct {
    Severity                 Severity // Error, Warning, NotChecked
    Tag, Path, Name          string   // Path: "(0008,1140)[1] (0008,1155)"
    Module, Reference, Rule  string
    Message                  string
}

// The rules themselves.
func Edition() string
func IODForSOPClass(sopClassUID string) (*IOD, bool)
func LookupTable(label string) (*Table, bool)
type IOD struct { Name, Section, Table string; Modules []ModuleUsage }
type ModuleUsage struct { Module, Table string; Usage Usage; Condition string }
type Table struct { Name, Section string; Rows []Row }
type Row struct { Depth int; Include string; Tag, Mask uint32; Name string; Type AttributeType; ... }
```

## What is not checked

Conditions are prose in PS3.3 and none is evaluated: a Conditional module, an
absent Type 1C or 2C attribute and a macro included under a condition are
each reported as `NotChecked`, with the condition, never passed. Also
reported as not checked: repeating groups such as (60xx,eeee), the Functional
Group Macros of the enhanced IODs, and SR content nested deeper than twelve
levels. Not checked at all: context groups (PS3.16), rules between
attributes, the form of values (see `valuerep`), and attributes the IOD does
not name.

## Regenerating the tables

```
curl -O https://dicom.nema.org/medical/dicom/current/source/docbook/part03/part03.xml
curl -O https://dicom.nema.org/medical/dicom/current/source/docbook/part04/part04.xml
python3 scripts/iod-tables.py part03.xml part04.xml | gofmt > validate/tables.go
```

## References

- DICOM PS3.3 Annex A (IODs) and Annex C (modules)
- DICOM PS3.4 Table B.5-1 (Storage SOP classes)
- DICOM PS3.5 Section 7.4 (attribute types)
