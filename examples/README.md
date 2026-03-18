# go-dicom Examples

Standalone examples demonstrating the go-dicom library. Each example is its own `main` package and can be run directly with `go run`.

## Input/Output

### Read a DICOM file
```bash
go run ./input_output/read_dicom <file.dcm>
```
Reads a DICOM file, displays file meta information, and prints the first 20 data elements.

### Write a DICOM file
```bash
go run ./input_output/write_dicom <output.dcm>
```
Creates a minimal DICOM file from scratch with patient and study information using the `filewriter` package.

### Print a dataset
```bash
go run ./input_output/printing_dataset
```
Builds a dataset in memory and demonstrates the various `String()`, `PrettyString()`, `CompactString()`, and `FormatString()` methods.

### Modify a DICOM file
```bash
go run ./input_output/modify_dicom <file.dcm>
```
Reads a DICOM file, modifies element values (patient name, ID, etc.), removes elements for anonymization, and saves the result.

### Read element values
```bash
go run ./input_output/read_element_values <file.dcm>
```
Reads a DICOM file and extracts specific element values by keyword (patient info, study info, image dimensions).

## Metadata Processing

### Sequences
```bash
go run ./metadata_processing/sequences
```
Demonstrates creating, nesting, iterating, and modifying DICOM sequences using the `sequence` and `dataset` packages.

## Image Processing

These examples are conceptual demonstrations that explain algorithms and DICOM image data structures. They use simulated data and do not require DICOM files.

### Downsize image
```bash
go run ./image_processing/downsize_image
```
Shows the pixel downsampling algorithm and memory calculations.

### Reslice
```bash
go run ./image_processing/reslice
```
Explains multi-slice volume reconstruction and multiplanar reformatting (axial, sagittal, coronal).

### Waveforms
```bash
go run ./image_processing/waveforms
```
Explains DICOM waveform data structure (ECG/EEG), multiplex format, and channel extraction.
