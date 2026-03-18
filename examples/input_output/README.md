# Input/Output Examples

This directory contains examples demonstrating how to read and write DICOM files, and how to format output.

## Examples Overview

### 1. read_dicom.go
Read a DICOM file and display dataset information.

**Demonstrates:**
- Reading DICOM files with `filereader.ReadFile()`
- Accessing string and numeric elements
- Checking for element existence
- Getting pixel data metadata
- Computing pixel statistics

**Run:**
```bash
go run read_dicom.go
```

**Example Output:**
```
Reading DICOM file: /path/to/file.dcm

=== Dataset Information ===
SOP Class UID..: 1.2.840.10008.5.1.4.1.2
Patient's Name.: Doe^John
Patient ID.....: 12345678
Modality........: MR
Study Date.....: 20231224
Image Rows.....: 512
Image Columns..: 512

=== Pixel Data Information ===
Pixel Rows................: 512
Pixel Columns............: 512
Bits Allocated...........: 16
Number of Frames.........: 1
Samples Per Pixel........: 1

Calculating sampled pixel statistics (10% sample)...
  Min Pixel Value: 0.00
  Max Pixel Value: 4095.00
  Mean Pixel Value: 512.50

=== Total Elements ===
Total elements in dataset: 147
```

---

### 2. write_dicom.go
Create a DICOM file from scratch and write it to disk.

**Demonstrates:**
- Creating new datasets with `dataset.NewDataset()`
- Adding DICOM elements
- Setting file meta information
- Writing DICOM files with `filewriter.WriteFile()`
- Reading back written files for verification

**Run:**
```bash
go run write_dicom.go
```

**Example Output:**
```
Creating new DICOM dataset...
Setting dataset values...
Writing dataset to: /tmp/dicom_XXXXXXXXXX.dcm
Dataset written successfully!

Loading dataset from: /tmp/dicom_XXXXXXXXXX.dcm ...

=== Verification - First 10 Elements ===
[0008,0016] UI: 1.2.840.10008.5.1.4.1.1.2
[0008,0018] UI: 1.2.3.4.5.6.7.8.9
...and 137 more elements

Cleaning up: Deleting /tmp/dicom_XXXXXXXXXX.dcm
Done!
```

---

### 3. printing_dataset.go
Display DICOM dataset contents in custom formats.

**Demonstrates:**
- Custom dataset printing with indentation
- Element iteration with `GetAll()`
- VR type checking
- Sequence handling
- Dataset statistics

**Run:**
```bash
go run printing_dataset.go
```

**Example Output:**
```
Reading DICOM file for custom printing...

=== Dataset Information ===
Total elements: 147
Total bytes: 524288

Elements by VR:
  UI: 8 elements
  LO: 12 elements
  DA: 3 elements
  ...

Elements by Group:
  (0008,xxxx): 18 elements
  (0010,xxxx): 8 elements
  (0020,xxxx): 12 elements
  ...

=== Dataset Elements (Custom Format) ===
[0008,0008] (CS) = ORIGINAL\PRIMARY
[0008,0016] (UI) = 1.2.840.10008.5.1.4.1.2
[0008,0018] (UI) = 1.2.3.4.5.6.7.8.9
...
```

---

## Common Patterns

### Reading a File
```go
import "github.com/amrshadid/go-dicom/filereader"

ds, err := filereader.ReadFile("path/to/file.dcm")
if err != nil {
    log.Fatal(err)
}
```

### Accessing Elements
```go
import "github.com/amrshadid/go-dicom/tag"

// Get element by tag
if elem, ok := ds.Get(tag.New(0x0010, 0x0010)); ok {
    value := elem.GetValue()
    fmt.Printf("Value: %v\n", value)
}

// Check if element exists
if ds.Contains(tag.New(0x0010, 0x0010)) {
    // Element exists
}
```

### Iterating Elements
```go
elements := ds.GetAll()
for _, elem := range elements {
    tag := elem.GetTag()
    vr := elem.GetVR()
    value := elem.GetValue()
    fmt.Printf("%v (%s): %v\n", tag, vr, value)
}
```

### Writing a File
```go
import "github.com/amrshadid/go-dicom/filewriter"

err := filewriter.WriteFile("output.dcm", ds)
if err != nil {
    log.Fatal(err)
}
```

---

## DICOM File Used in Examples

```
PATH_OF_DCM_FILE
```

**Characteristics:**
- Format: DICOM 3.0
- Size: ~254 KB
- Modality: MRI (Brain)
- Image Size: 512 x 512
- Bits: 16-bit

To use your own DICOM file, edit the `filePath` variable in the example.

---

## Tips

1. Always check error returns when reading files
2. Use `Contains()` before `Get()` to safely check element existence
3. For large images, use `GetPixelStatisticsSampled()` instead of `GetPixelStatistics()`
4. Clone datasets before modifying them: `cloned := ds.Clone()`
5. Use `GetStatistics()` to analyze dataset composition

---

For more examples, see the parent [EXAMPLES.md](../EXAMPLES.md) document.
