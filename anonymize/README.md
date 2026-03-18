# Anonymize Module

DICOM de-identification (anonymization) per DICOM PS3.15 Annex E. Supports multiple profiles, custom per-tag actions, reusable templates, and consistent UID remapping.

## Overview

The anonymize module removes or replaces Protected Health Information (PHI) from DICOM datasets according to standardized de-identification profiles. It provides:

1. **Standard Profiles** - Pre-defined de-identification profiles from DICOM PS3.15 Annex E
2. **Custom Actions** - Per-tag action overrides for fine-grained control
3. **Templates** - Reusable, composable anonymization configurations
4. **UID Remapping** - Consistent UID replacement across a session
5. **Thread Safety** - All operations safe for concurrent use

## Quick Start

```go
package main

import (
    "github.com/amrshadid/go-dicom/anonymize"
    "github.com/amrshadid/go-dicom/dataset"
)

func main() {
    ds := dataset.NewDataset()
    // ... load dataset ...

    // Basic anonymization
    anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
    anon.Anonymize(ds)
}
```

## Profiles

| Profile | Description |
|---------|-------------|
| `BasicProfile` | Standard PS3.15 Annex E basic profile |
| `CleanDescriptorsProfile` | Clean description fields instead of removing |
| `CleanGraphicsProfile` | Remove burned-in annotations |
| `CleanStructuredContentProfile` | Clean structured content |
| `RetainLongFullDatesProfile` | Keep full dates for longitudinal studies |
| `RetainPatientCharsProfile` | Keep age, sex, size, weight |
| `RetainDeviceIdentProfile` | Keep device information |
| `RetainUIDsProfile` | Keep original UIDs |
| `RetainSafePrivateProfile` | Keep safe private tags |

## Actions

| Action | Description |
|--------|-------------|
| `ActionRemove` | Delete the element entirely |
| `ActionEmpty` | Replace with zero-length value |
| `ActionReplace` | Replace with dummy value |
| `ActionKeep` | Keep unchanged |
| `ActionClean` | Clean (context-dependent) |
| `ActionUID` | Replace UID with remapped UID |
| `ActionDummy` | Replace with default value for VR |

## Templates

Templates allow creating reusable, composable anonymization configurations:

```go
// Create a custom template
tmpl := anonymize.NewTemplate("research", "Research study template")
tmpl.SetAction(tag.New(0x0010, 0x0010), anonymize.ActionReplace) // PatientName
tmpl.SetAction(tag.New(0x0008, 0x0020), anonymize.ActionKeep)    // StudyDate

// Use pre-built templates
basic := anonymize.BasicProfileTemplate()
dates := anonymize.RetainDatesTemplate()

// Merge templates (later template overrides earlier)
basic.Merge(dates)

// Apply to anonymizer
anon := anonymize.NewAnonymizer(anonymize.BasicProfile)
anon.ApplyTemplate(tmpl)
anon.Anonymize(ds)
```

### Pre-built Templates

- `BasicProfileTemplate()` - DICOM PS3.15 basic profile
- `CleanDescriptorsTemplate()` - Cleans descriptions instead of removing
- `RetainDatesTemplate()` - Keeps full dates
- `RetainDeviceTemplate()` - Keeps device identity

## Custom Per-Tag Actions

```go
anon := anonymize.NewAnonymizer(anonymize.BasicProfile)

// Override specific tags
anon.SetCustomAction(tag.New(0x0010, 0x0010), anonymize.ActionKeep)

// Retain patient name and ID
anon.SetRetainPatientName(true)
anon.SetRetainPatientID(true)
```

## UID Remapping

UIDs are consistently remapped within a session:

```go
anon := anonymize.NewAnonymizer(anonymize.BasicProfile)

// Same original UID always maps to same new UID
anon.Anonymize(ds1)
anon.Anonymize(ds2)

// Get the mapping table
mapping := anon.GetUIDMapping()

// Reset for new patient
anon.ResetUIDMapping()
```

## Testing

```bash
go test -v .
```

## Module Structure

```
anonymize/
    anonymize.go          # Core implementation
    anonymize_test.go     # Test suite
    doc.go                # Package documentation
    README.md             # This file
```

## References

- [DICOM PS3.15 Annex E](https://dicom.nema.org/medical/dicom/current/output/html/part15.html#sect_E.1) - Attribute Confidentiality Profiles
- [HIPAA De-identification](https://www.hhs.gov/hipaa/for-professionals/privacy/special-topics/de-identification/index.html)
