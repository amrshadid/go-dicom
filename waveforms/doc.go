// Package waveforms provides support for managing and analyzing DICOM waveform data.
//
// This package implements thread-safe management of DICOM waveform groups and channels,
// which represent physiological signals such as ECG (electrocardiogram), EEG
// (electroencephalogram), and other time-series biomedical data typically found in
// DICOM files (attributes 54xx,1001 onwards).
//
// # Core Concepts
//
// WaveformGroup represents a collection of related signal channels acquired at the same
// sampling frequency. Each group can contain multiple WaveformChannel instances, where each
// channel represents a single signal (e.g., one ECG lead or EEG electrode).
//
// WaveformManager provides thread-safe access to multiple waveform groups through
// synchronization primitives (sync.RWMutex), allowing safe concurrent reads and writes.
//
// # Features
//
//   - Thread-safe waveform and channel management with concurrent read/write support
//   - Channel analysis including min/max value detection and mean value calculation
//   - ECG signal processing with QRS complex detection using threshold-based methods
//   - Heart rate calculation from detected QRS peak indices
//   - Sample range extraction for focused analysis of specific time windows
//   - Deep copy functionality for creating independent waveform copies
//   - Comprehensive validation of waveform structure and metadata
//
// # Basic Usage
//
//	// Create a new waveform manager
//	wm := waveforms.NewWaveformManager()
//
//	// Create a waveform group
//	wf := &waveforms.WaveformGroup{
//		NumberOfWaveformChannels: 3,
//		NumberOfSamples:          1000,
//		SamplingFrequency:        500, // Hz
//		WaveformOriginality:      "ORIGINAL",
//		ChannelLabel:             []string{"I", "II", "III"},
//		WaveformData:             make([][]int16, 3),
//	}
//	for i := 0; i < 3; i++ {
//		wf.WaveformData[i] = make([]int16, 1000)
//		// Populate with signal data
//	}
//
//	// Add waveform to manager
//	if err := wm.AddWaveform(0, wf); err != nil {
//		log.Fatal(err)
//	}
//
//	// Add a channel with ECG data
//	ch := waveforms.WaveformChannel{
//		Label:      "ECG-I",
//		Source:     "Lead I",
//		Units:      "mV",
//		SampleRate: 500,
//		Data:       []int16{/* ECG samples */},
//	}
//	if err := wm.AddChannel(0, ch); err != nil {
//		log.Fatal(err)
//	}
//
// # ECG Processing
//
// The package includes specialized methods for ECG signal analysis:
//
//	// Detect QRS complexes (heartbeats) above a threshold
//	qrsIndices, err := wm.DetectQRSComplex(0, 0, 50)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Calculate heart rate from detected peaks
//	bpm := wm.CalculateHeartRate(qrsIndices, 500.0)
//	fmt.Printf("Heart rate: %.1f BPM\n", bpm)
//
// # Channel Analysis
//
// Perform statistical analysis on channel data:
//
//	// Analyze channel to get min, max, and mean values
//	analyzed, err := wm.AnalyzeChannel(0, 0)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Min: %d, Max: %d, Mean: %.2f\n",
//		analyzed.MinValue, analyzed.MaxValue, analyzed.MeanValue)
//
// # Sample Extraction
//
// Extract specific time windows from waveform data:
//
//	// Get samples from index 100 to 200
//	segment, err := wm.GetChannelSegment(0, 0, 100, 200)
//	if err != nil {
//		log.Fatal(err)
//	}
//	// Use segment for further processing
//
// # Thread Safety
//
// All WaveformManager methods are protected by synchronization primitives:
//
//   - Read operations (Get methods) use RWMutex.RLock() for concurrent access
//   - Write operations (Add, Remove, Copy) use RWMutex.Lock() for exclusive access
//   - This allows multiple goroutines to safely read waveforms simultaneously while
//     ensuring exclusive access during modifications
//
// # DICOM Compliance
//
// WaveformGroup fields correspond to DICOM attributes from the Waveform Module
// (5400,1001 onwards):
//
//   - WaveformOriginality: ORIGINAL | DERIVED
//   - NumberOfWaveformChannels: Number of signals per waveform
//   - NumberOfSamples: Samples per channel (typically 1000-10000)
//   - SamplingFrequency: Acquisition rate in Hz
//   - ChannelLabel: Human-readable channel names
//   - ChannelSource: Physical source identification (e.g., ECG leads)
//   - ChannelUnits: Measurement units (mV, µV, °C, etc.)
//   - TimeStamp: Waveform acquisition time
//   - DateOfCalibration, TimeOfCalibration: Equipment calibration info
//
// # Data Formats
//
// Waveform data uses int16 (16-bit signed integers) as the standard representation,
// which is compatible with DICOM's OW (Other Word) and OB (Other Byte) value representations.
// This provides a good balance between precision and storage efficiency for most biomedical signals.
//
// # Types
//
//   - WaveformGroup: Container for multi-channel signal data
//   - WaveformChannel: Single channel with analysis metrics
//   - WaveformManager: Thread-safe manager for multiple waveform groups
//
// # See Also
//
// DICOM Standard PS 3.3: http://dicom.nema.org/standard.html
package waveforms
