// Example: Decode and Process Waveform Data (Conceptual Demonstration)
//
// NOTE: This is a conceptual/educational example that explains the
// structure of DICOM waveform data (ECG, EEG, etc.) and how to extract
// and interpret it. It uses simulated metadata rather than reading from
// an actual DICOM file. To process real waveform data, combine these
// concepts with the filereader package.
//
// This example illustrates how to:
// - Access waveform sequences in DICOM files
// - Extract waveform metadata (channels, samples, frequency)
// - Process multiplex waveform data
// - Access channel information and units
// - Prepare waveform data for visualization
//

package main

import (
	"fmt"
)

// WaveformMetadata holds information about a waveform
type WaveformMetadata struct {
	Label            string
	NumberOfChannels int
	NumberOfSamples  int
	SamplingFreq     float64 // Hz
}

// ChannelInfo holds information about a waveform channel
type ChannelInfo struct {
	ChannelSource    string
	SensitivityUnits string
	SensitivityValue float64
}

// GetWaveformMetadata creates metadata from waveform information
func GetWaveformMetadata(label string, channels, samples int, freq float64) WaveformMetadata {
	return WaveformMetadata{
		Label:            label,
		NumberOfChannels: channels,
		NumberOfSamples:  samples,
		SamplingFreq:     freq,
	}
}

// calculateTimeAxis generates time axis for waveform data
func calculateTimeAxis(numSamples int, samplingFreq float64) []float64 {
	timeAxis := make([]float64, numSamples)
	dt := 1.0 / samplingFreq

	for i := 0; i < numSamples; i++ {
		timeAxis[i] = float64(i) * dt
	}

	return timeAxis
}

func main() {
	fmt.Println("=== Decode and Process Waveform Data ===")

	fmt.Println("This example demonstrates waveform data processing from DICOM files.")

	fmt.Println("=== What are DICOM Waveforms? ===")

	fmt.Println("DICOM waveforms represent time-series data from medical devices:")
	fmt.Println("  - ECG (Electrocardiogram): 500-1000 Hz sampling")
	fmt.Println("  - EEG (Electroencephalogram): 100-500 Hz sampling")
	fmt.Println("  - EMG (Electromyogram): 500-2000 Hz sampling")
	fmt.Println("  - Respiration: 10-20 Hz sampling")
	fmt.Println("  - Blood Pressure, Temperature, etc.")

	fmt.Println()
	fmt.Println("=== DICOM Waveform Structure ===")

	fmt.Println()
	fmt.Println("Waveform data is organized in modules:")
	fmt.Println("  - WaveformSequence (0x5400, 0x0100)")
	fmt.Println("    └── Multiplex Group")
	fmt.Println("        ├── NumberOfWaveformChannels (0x003A, 0x0005)")
	fmt.Println("        ├── NumberOfWaveformSamples (0x003A, 0x0010)")
	fmt.Println("        ├── SamplingFrequency (0x003A, 0x001A)")
	fmt.Println("        ├── MultiplexGroupLabel (0x003A, 0x0011)")
	fmt.Println("        ├── ChannelDefinitionSequence (0x003A, 0x0200)")
	fmt.Println("        │   └── Channel Item")
	fmt.Println("        │       ├── ChannelSourceSequence (0x003A, 0x0208)")
	fmt.Println("        │       └── ChannelSensitivityUnitsSequence (0x003A, 0x0210) [optional]")
	fmt.Println("        └── WaveformData (0x5400, 0x0110)")

	fmt.Println()
	fmt.Println("=== Example: ECG Waveform ===")

	// Simulated ECG waveform
	ecg := GetWaveformMetadata("ECG", 3, 1000, 500.0)

	fmt.Printf("Label: %s\n", ecg.Label)
	fmt.Printf("Channels: %d\n", ecg.NumberOfChannels)
	fmt.Println("  - Channel 0: Lead I")
	fmt.Println("  - Channel 1: Lead II")
	fmt.Println("  - Channel 2: Lead III")
	fmt.Printf("Samples per channel: %d\n", ecg.NumberOfSamples)
	fmt.Printf("Sampling frequency: %.0f Hz\n", ecg.SamplingFreq)

	fmt.Println()
	// Calculate duration
	duration := float64(ecg.NumberOfSamples) / ecg.SamplingFreq
	fmt.Printf("Duration: %.2f seconds\n", duration)

	fmt.Println()
	// Time axis
	timeAxis := calculateTimeAxis(ecg.NumberOfSamples, ecg.SamplingFreq)
	fmt.Printf("Time range: %.3f to %.3f seconds\n", timeAxis[0], timeAxis[len(timeAxis)-1])

	fmt.Println()
	fmt.Println("=== Waveform Data Format ===")

	fmt.Println("Data Storage: Interleaved multiplex format")
	fmt.Println("  Example with 2 channels, 5 samples:")
	fmt.Println("  Raw data: [ch0_s0, ch1_s0, ch0_s1, ch1_s1, ch0_s2, ch1_s2, ...]")
	fmt.Println()
	fmt.Println("  Visualization:")
	fmt.Println("    Sample 0: [ch0_s0, ch1_s0]")
	fmt.Println("    Sample 1: [ch0_s1, ch1_s1]")
	fmt.Println("    Sample 2: [ch0_s2, ch1_s2]")
	fmt.Println("    ...")

	fmt.Println()
	fmt.Println("=== Extracting Channel Data ===")

	fmt.Println()
	fmt.Println("To extract a single channel from multiplex data:")
	fmt.Println("  1. For each sample index (0 to numSamples-1)")
	fmt.Println("  2. Calculate raw index: sample * numChannels + channelIndex")
	fmt.Println("  3. Extract value at that index")

	fmt.Println()
	fmt.Println("Example code:")
	fmt.Println("  func extractChannel(multiplex []int, ch, numCh, numSamp int) []int {")
	fmt.Println("      out := make([]int, numSamp)")
	fmt.Println("      for s := 0; s < numSamp; s++ {")
	fmt.Println("          out[s] = multiplex[s*numCh + ch]")
	fmt.Println("      }")
	fmt.Println("      return out")
	fmt.Println("  }")

	fmt.Println()
	fmt.Println("=== Channel Information ===")

	fmt.Println("Each channel has associated metadata:")
	fmt.Println("  - Channel Source: What produced the signal (ECG I, II, III, etc.)")
	fmt.Println("  - Sensitivity: Gain/conversion factor")
	fmt.Println("  - Units: Measurement units (mV, µV, mA, etc.)")
	fmt.Println("  - Baseline: Baseline value (optional)")
	fmt.Println("  - Offset: Data offset (optional)")

	fmt.Println()
	fmt.Println("=== Reading DICOM Waveforms ===")

	fmt.Println("Code pattern:")
	fmt.Println("  1. Read DICOM file:")
	fmt.Println("     file, _ := os.Open(filePath)")
	fmt.Println("     reader := filebase.NewFileReader(file)")
	fmt.Println("     dicomFile, _ := filereader.ReadDICOMFile(reader)")
	fmt.Println()
	fmt.Println("  2. Find WaveformSequence (0x5400, 0x0100):")
	fmt.Println("     var waveSeq []interface{}")
	fmt.Println("     for _, elem := range dicomFile.DataElements {")
	fmt.Println("         if elem.Tag.Group() == 0x5400 && elem.Tag.Element() == 0x0100 {")
	fmt.Println("             // Parse sequence items")
	fmt.Println("         }")
	fmt.Println("     }")
	fmt.Println()
	fmt.Println("  3. For each multiplex group:")
	fmt.Println("     - Get NumberOfWaveformChannels")
	fmt.Println("     - Get NumberOfWaveformSamples")
	fmt.Println("     - Get SamplingFrequency")
	fmt.Println("     - Get ChannelDefinitionSequence")
	fmt.Println("     - Get WaveformData")
	fmt.Println()
	fmt.Println("  4. Extract and process each channel:")
	fmt.Println("     for ch := 0; ch < numChannels; ch++ {")
	fmt.Println("         channelData := extractChannel(waveData, ch, numCh, numSamp)")
	fmt.Println("         // Analyze or visualize")
	fmt.Println("     }")

	fmt.Println()
	fmt.Println("=== Visualization ===")

	fmt.Println("To visualize waveforms:")
	fmt.Println("  1. Create time axis: calculateTimeAxis(numSamples, samplingFreq)")
	fmt.Println("  2. Extract each channel: extractChannelData(...)")
	fmt.Println("  3. Plot using graphics library (e.g., gonum/plot):")
	fmt.Println("     - X-axis: Time (seconds)")
	fmt.Println("     - Y-axis: Signal value (mV, µV, etc.)")
	fmt.Println("     - Title: Channel label + source")

	fmt.Println()
	fmt.Println("=== Data Types and Ranges ===")

	fmt.Println()
	fmt.Println("Common sample value types:")
	fmt.Println("  - 16-bit signed integer: ±32,767")
	fmt.Println("  - 32-bit signed integer: ±2,147,483,647")
	fmt.Println("  - 32-bit float: ±3.4e+38")

	fmt.Println()
	fmt.Println("ECG typical ranges:")
	fmt.Println("  - Amplitude: ±5 mV (before scaling)")
	fmt.Println("  - Sensitivity: ~200-400 µV/LSB (depends on device)")

	fmt.Println()
	fmt.Println("=== Performance Tips ===")

	fmt.Println("For large waveforms:")
	fmt.Println("  - Stream data instead of loading all at once")
	fmt.Println("  - Process channels independently")
	fmt.Println("  - Use sliding window for real-time analysis")
	fmt.Println("  - Compress waveform data between acquisitions")

	fmt.Println()
	fmt.Println("=== Common Issues ===")

	fmt.Println()
	fmt.Println("1. ChannelSensitivityUnitsSequence is type 1C")
	fmt.Println("   - Not always present in all DICOM files")
	fmt.Println("   - Must check before accessing")
	fmt.Println()
	fmt.Println("2. Multiplex data format variations")
	fmt.Println("   - Some files use non-interleaved format")
	fmt.Println("   - Check WaveformBitsAllocated, BitsStored")
	fmt.Println()
	fmt.Println("3. Byte order depends on Transfer Syntax")
	fmt.Println("   - Usually little-endian")
	fmt.Println("   - Check IsLittleEndian from DICOMFile")

	fmt.Println()
	fmt.Println("✓ Waveform processing example complete!")
}
