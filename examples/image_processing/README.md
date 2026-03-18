# Image Processing Examples

This directory contains examples demonstrating how to work with pixel data, multi-slice datasets, and waveform information in DICOM files.

## Examples Overview

### 1. downsize_image.go
Downsize an MRI image by subsampling pixels.

**Demonstrates:**
- Reading pixel data from DICOM files
- Downsampling images by factor (every nth pixel)
- Updating image dimensions (Rows and Columns)
- Converting between pixel formats (bytes ↔ uint16)
- Writing modified pixel data back to DICOM files

**Run:**
```bash
go run downsize_image.go
```

**Example Output:**
```
=== Downsize MRI Image Example ===

Reading DICOM file: 1.2.826.0.1.3680043.8.498...

Original image dimensions: 512 x 512 pixels
Pixel data loaded: 262144 pixels

Downsampling by factor of 8...
Downsampled image dimensions: 64 x 64 pixels
Pixel data reduced from 262144 to 4096 pixels

Writing downsampled DICOM file to: /path/to/file_downsampled_1.dcm
Downsampled file written successfully!

Verifying written file...
Verified dimensions in written file: 64 x 64

✓ Downsampling complete!
```

---

### 2. reslice.go
Load multiple DICOM slices, build 3D volume, and extract reformatted planes.

**Demonstrates:**
- Loading multiple DICOM files with glob patterns
- Filtering slices by SliceLocation tag
- Sorting slices in correct anatomical order
- Accessing pixel spacing and slice thickness
- Building 3D volumes from 2D slices
- Extracting axial, sagittal, and coronal planes
- Calculating aspect ratios for correct visualization

**Run:**
```bash
go run reslice.go
```

**Example Output:**
```
=== Reslice CT Images in Different Planes ===

This example demonstrates:
  1. Loading multiple DICOM files
  2. Sorting slices by SliceLocation
  3. Building a 3D volume from 2D slices
  4. Extracting axial, sagittal, and coronal reformatted views

Loading DICOM file: 1.2.826.0.1.3680043.8.498...

Dataset dimensions: 512 x 512 pixels

=== Spacing Information ===
Pixel Spacing found: [0.5, 0.5]
  (PixelSpacing specifies spacing between adjacent rows and columns)
Slice Thickness found: 5.0
  (SliceThickness specifies distance between slices)

=== Reslicing Example ===
For a multi-slice dataset with multiple DICOM files:

Example Statistics (for 512x512x128 volume):
  Original axial slices: 128 (512x512 each)
  Sagittal slices (extracted): 512 (512x128 each)
  Coronal slices (extracted): 512 (128x512 each)

Key Steps:
  [Details of reslicing process...]

✓ Reslicing example structure demonstrated
```

---

### 3. waveforms.go
Process waveform data (ECG, EEG, EMG, etc.) from DICOM files.

**Demonstrates:**
- Accessing waveform sequences in DICOM datasets
- Extracting multiplex group metadata
- Accessing channel definitions and information
- Processing waveform data formats
- Extracting channel source and sensitivity units
- Preparing data for visualization
- Understanding DICOM waveform tag structure

**Run:**
```bash
go run waveforms.go
```

**Example Output:**
```
=== Decode and Process Waveform Data ===

Loading DICOM file: 1.2.826.0.1.3680043.8.498...

Note: This example DICOM file does not contain waveform data.

Waveform data is typically found in:
  - Cardiac imaging (ECG waveforms)
  - Electrophysiology studies (EEG, EMG)
  - Respiration monitoring

=== Waveform Processing Example ===

When a WaveformSequence (0x5400, 0x0100) is present, the process is:

1. Access the waveform sequence:
   waveSeqElem, ok := ds.Get(tag.New(0x5400, 0x0100))
   waveSeq := waveSeqElem.GetValue().(*sequence.Sequence)

2. Iterate through multiplex groups:
   for i := 0; i < waveSeq.Length(); i++ {
       multiplex, _ := waveSeq.Item(i)

[Details of waveform processing...]

✓ Waveform processing example complete
```

---

## Common Patterns

### Reading Pixel Data

```go
import "github.com/amrshadid/go-dicom/tag"

// Get pixel data element
if ds.Contains(tag.New(0x7FE0, 0x0010)) {
    pixelElem, _ := ds.Get(tag.New(0x7FE0, 0x0010))
    pixelData := pixelElem.GetValue()
}
```

### Accessing Image Dimensions

```go
// Get rows and columns
rowsElem, _ := ds.Get(tag.New(0x0028, 0x0010))
colsElem, _ := ds.Get(tag.New(0x0028, 0x0011))

rows := rowsElem.GetValue().(uint32)
cols := colsElem.GetValue().(uint32)
```

### Loading Multiple Files and Sorting

```go
import (
    "glob"
    "sort"
)

type SliceInfo struct {
    FilePath      string
    SliceLocation float64
}

// Load files
var slices []SliceInfo
for _, fname := range glob.glob("*.dcm") {
    ds, _ := filereader.ReadFile(fname)
    // Extract SliceLocation from dataset
    slices = append(slices, SliceInfo{...})
}

// Sort by slice location
sort.Slice(slices, func(i, j int) bool {
    return slices[i].SliceLocation < slices[j].SliceLocation
})
```

### Building 3D Volumes

```go
// Create 3D array: [rows][cols][slices]
volume := make([][][]uint16, rows)
for i := range volume {
    volume[i] = make([][]uint16, cols)
    for j := range volume[i] {
        volume[i][j] = make([]uint16, numSlices)
    }
}

// Fill with data from slices
for sliceIdx, slice := range sortedSlices {
    pixelArray := slice.pixelArray
    for i := 0; i < rows; i++ {
        for j := 0; j < cols; j++ {
            volume[i][j][sliceIdx] = pixelArray[i][j]
        }
    }
}
```

### Extracting Reformatted Planes

```go
// Axial slice (transverse): constant Z
axial := volume[:, :, middleZ]

// Sagittal slice (left-right): constant Y
sagittal := volume[:, middleY, :]

// Coronal slice (front-back): constant X
coronal := volume[middleX, :, :]  // May need transpose
```

### Accessing Waveform Sequences

```go
import "github.com/amrshadid/go-dicom/sequence"

// Get waveform sequence
waveElem, _ := ds.Get(tag.New(0x5400, 0x0100))
waveSeq := waveElem.GetValue().(*sequence.Sequence)

// Iterate multiplex groups
for i := 0; i < waveSeq.Length(); i++ {
    multiplex, _ := waveSeq.Item(i)

    // Get channel definition sequence
    chanElem, _ := multiplex.Get(tag.New(0x003A, 0x0200))
    chanSeq := chanElem.GetValue().(*sequence.Sequence)

    // Access channel information
    channel, _ := chanSeq.Item(0)
    // Extract channel metadata
}
```

---

## Important DICOM Tags

### Image Dimensions and Spacing
- `(0x0028, 0x0010)` - Rows
- `(0x0028, 0x0011)` - Columns
- `(0x0028, 0x0030)` - Pixel Spacing (in mm)
- `(0x0018, 0x0050)` - Slice Thickness (in mm)
- `(0x0020, 0x1041)` - Slice Location (in mm)

### Pixel Data
- `(0x7FE0, 0x0010)` - Pixel Data
- `(0x0028, 0x0002)` - Samples per Pixel
- `(0x0028, 0x0008)` - Number of Frames
- `(0x0028, 0x0100)` - Bits Allocated
- `(0x0028, 0x0101)` - Bits Stored
- `(0x0028, 0x0103)` - Pixel Representation

### Waveform Tags
- `(0x5400, 0x0100)` - Waveform Sequence
- `(0x5400, 0x0110)` - Waveform Data
- `(0x003A, 0x0005)` - NumberOfWaveformChannels
- `(0x003A, 0x0010)` - NumberOfWaveformSamples
- `(0x003A, 0x001A)` - SamplingFrequency
- `(0x003A, 0x0200)` - ChannelDefinitionSequence
- `(0x003A, 0x0208)` - ChannelSourceSequence
- `(0x003A, 0x0210)` - ChannelSensitivityUnitsSequence

---

## Pixel Data Format Notes

### Byte Order
- DICOM uses little-endian byte order
- When converting between bytes and uint16: `uint16(b[0]) | (uint16(b[1]) << 8)`

### Data Types
- 8-bit images: 1 byte per pixel
- 16-bit images: 2 bytes per pixel (standard for CT, MR)
- 32-bit images: 4 bytes per pixel (rare)

### Multi-Frame Data
- Number of frames in `(0x0028, 0x0008)`
- Total pixels = Rows × Columns × NumberOfFrames
- Each frame has same dimensions

### Planar Configuration
- `(0x0028, 0x0006)` specifies if data is:
  - 0 (Color-by-pixel): RGBRGBRGB...
  - 1 (Color-by-plane): RRR...GGG...BBB...

---

## Tips for Image Processing

1. **Always verify dimensions** before creating arrays:
   ```go
   if rows == 0 || cols == 0 {
       log.Fatal("Invalid dimensions")
   }
   ```

2. **Check BitsAllocated before reading pixel data**:
   - Common values: 8, 16, 32 bits
   - Affects byte-to-value conversion

3. **Handle signed vs unsigned pixel data**:
   - Check PixelRepresentation tag `(0x0028, 0x0103)`
   - 0 = unsigned, 1 = signed

4. **For multi-slice sorting**:
   - Use SliceLocation `(0x0020, 0x1041)` when available
   - Fallback: use InstanceNumber `(0x0020, 0x0013)`
   - Or use ImagePositionPatient `(0x0020, 0x0032)`

5. **Aspect ratio corrections**:
   - Axial: pixel_spacing[1] / pixel_spacing[0]
   - Sagittal: pixel_spacing[1] / slice_thickness
   - Coronal: slice_thickness / pixel_spacing[0]

6. **Memory efficiency**:
   - For large 3D volumes, consider:
     - Memory-mapped file access
     - Slice-by-slice processing
     - Streaming instead of loading all into memory

7. **Waveform data handling**:
   - Always check for ChannelSensitivityUnitsSequence (type 1C)
   - Understand multiplex format (interleaved by sample)
   - Account for different sampling frequencies

---

## Example DICOM File

```
PATH_OF_DCM_FILE
```

**Characteristics:**
- Format: DICOM 3.0
- Size: ~254 KB
- Modality: MRI (Brain)
- Image Size: 512 x 512
- Bits: 16-bit
- Frames: 1 (single slice)

---

For more examples, see the parent [EXAMPLES.md](../EXAMPLES.md) document.
