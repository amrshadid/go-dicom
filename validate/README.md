# validate

Checks a data set against the IOD of its SOP class: the modules PS3.3 Annex A
gives the IOD, and in each module the attributes Annex C lists, with their
Type, value multiplicity, Enumerated Values and Defined Terms, and the number
of items a sequence holds.

The rules are generated from the standard (`scripts/iod-tables.py`, from the
DocBook XML of PS3.3 and PS3.4), not written by hand. `Edition()` names the
edition: 153 IODs, 29 Functional Group Macros tables of enhanced IODs, and
633 module and macro tables, for 164 Storage SOP classes.

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
8 error(s), 0 warning(s), 36 not checked
```

## Conditions

A condition PS3.3 states in one of a few fixed sentence forms, naming one
attribute by its tag, is generated as data (`Condition`) and evaluated:

| Form | Example (PS3.3 2026d) |
|---|---|
| `IsPresent` | Required if Extended Offset Table (7FE0,0001) is present. |
| `IsAbsent` | Required if Pixel Data Provider URL (0028,7FE0) is not present. |
| `HasValue` | Required if Multi-energy CT Acquisition (0018,9361) is YES. / Image Type (0008,0008) Value 1 is ORIGINAL or MIXED. |
| `IsPresentWithValue` | Required if Partial View (0028,1350) is present with a Value of YES. |

each optionally followed by "May be present otherwise." (`Otherwise`). An
attribute or module whose condition holds and that is absent is an error, and
so is one present where its condition does not hold and PS3.3 does not say it
may be present otherwise (PS3.5 7.4.2, 7.4.4; PS3.3 A.1.3.2).

The generator reads a condition so only when the whole of what the
description says about presence is that one sentence. A sentence that matches
a form in part ("has a Value greater than 1"), two tests joined, a condition
another sentence qualifies, or a name that does not agree with its tag, is
prose. The numbers, from the tables generated from PS3.3 2026d:

| | Conditions | IsPresent | IsAbsent | HasValue | IsPresentWithValue | Prose |
|---|---|---|---|---|---|---|
| Type 1C and 2C attributes | 1867 | 104 | 73 | 455 | 13 | 1222 |
| Conditional modules | 316 | 0 | 0 | 23 | 2 | 291 |
| Conditional Functional Group Macros | 171 | 2 | 0 | 43 | 0 | 126 |

`scripts/iod-tables.py` prints them on standard error, with why each prose
condition was not read.

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
type IOD struct { Name, Section, Table string; Modules []ModuleUsage; FunctionalGroupsTable string; FunctionalGroups []FunctionalGroup }
type FunctionalGroup struct { Macro, Table string; Usage Usage; Condition string; When *Condition; Placement Placement }
type ModuleUsage struct { Module, Table string; Usage Usage; Condition string; When *Condition }
type Table struct { Name, Section string; Rows []Row }
type Row struct { Depth int; Include string; Tag, Mask uint32; Name string; Type AttributeType; Condition string; When *Condition; ... }
type Condition struct { Test ConditionTest; Tag uint32; Value int; Values []string; Otherwise bool }
```

## What is not checked

A condition in prose is not evaluated: a Conditional module and an absent
Type 1C or 2C attribute whose condition is prose, and a macro included under
a condition, are each reported as `NotChecked`, with the condition, never
passed. So is a condition in a sequence item that names an attribute outside
the item, and one testing "a Value" of an attribute holding several. Also
reported as not checked: repeating groups such as (60xx,eeee), the functional
groups of Enhanced MR Color Image, whose Functional Group Macros table is
another IOD's, and SR content nested deeper than twelve levels.

## Functional groups

An enhanced multi-frame IOD's Functional Group Macros table (29 of them, A.38-2
for Enhanced CT) is generated with each macro's usage, condition and where it
may be (`SharedOrPerFrame`, `SharedOnly`, `PerFrameOnly`, from "May not be used
as a Shared Functional Group"). The Shared and Per-Frame Functional Groups
Sequences are checked against it:

- a macro the IOD requires is in the shared item or in the item of every frame;
- none is in both (Table C.7.6.16-1), nor where the table says it may not be;
- the Per-Frame sequence holds one item for each frame;
- each macro that is there is checked attribute by attribute, and what is
  wrong in many frames is one finding saying in how many.

```
$ go-dicom validate no_plane_position_f2.dcm
  error       Plane Position (Patient) (functional group): Usage M: absent from 1 of the 2 per-frame items (frame 2 first) and from the shared item; the macro is required [...; PS3.3 A.38.1 Table A.38-2]
``` Not checked at all: context groups (PS3.16), rules between
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
