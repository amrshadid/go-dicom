package waveforms

import (
	"fmt"
	"sync"
	"time"
)

// WaveformGroup represents a DICOM waveform group with multi-channel signal data.
type WaveformGroup struct {
	WaveformOriginality      string    // ORIGINAL or DERIVED
	NumberOfWaveformChannels int       // Number of channels
	NumberOfSamples          int       // Samples per channel
	SamplingFrequency        float64   // Frequency in Hz
	MultiplexGroupLabel      string    // Group label
	ChannelLabel             []string  // Per-channel labels
	ChannelSource            []string  // Signal source
	ChannelUnits             []string  // Units of measurement
	WaveformData             [][]int16 // Multi-channel data
	TimeStamp                time.Time // Acquisition time
	SampleInterpretation     string    // POINT or LINE
	DateOfCalibration        string    // YYYYMMDD format
	TimeOfCalibration        string    // HHMMSS format
}

// WaveformChannel represents a single waveform signal channel with analysis data.
type WaveformChannel struct {
	Label       string  // Channel identifier
	Source      string  // Signal source
	Units       string  // Unit of measurement
	SampleRate  float64 // Frequency in Hz
	SampleCount int     // Number of samples
	MinValue    int16   // Minimum value
	MaxValue    int16   // Maximum value
	MeanValue   float64 // Mean value
	Data        []int16 // Signal samples
}

// WaveformManager manages multiple waveform groups with thread-safe access.
type WaveformManager struct {
	waveforms map[int]*WaveformGroup    // Indexed waveform groups
	channels  map[int][]WaveformChannel // Indexed channels
	mu        sync.RWMutex              // Protects concurrent access
}

// NewWaveformManager creates a new waveform manager.
func NewWaveformManager() *WaveformManager {
	return &WaveformManager{
		waveforms: make(map[int]*WaveformGroup),
		channels:  make(map[int][]WaveformChannel),
	}
}

// AddWaveform adds a waveform group at the specified index.
func (wm *WaveformManager) AddWaveform(index int, wf *WaveformGroup) error {
	if wf == nil {
		return fmt.Errorf("waveform cannot be nil")
	}

	if wf.NumberOfWaveformChannels <= 0 {
		return fmt.Errorf("number of channels must be positive")
	}

	if wf.NumberOfSamples <= 0 {
		return fmt.Errorf("number of samples must be positive")
	}

	if len(wf.WaveformData) != wf.NumberOfWaveformChannels {
		return fmt.Errorf("waveform data channels mismatch")
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if _, exists := wm.waveforms[index]; exists {
		return fmt.Errorf("waveform at index %d already exists", index)
	}

	wm.waveforms[index] = wf
	return nil
}

// GetWaveform retrieves a waveform group by index.
func (wm *WaveformManager) GetWaveform(index int) (*WaveformGroup, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	wf, exists := wm.waveforms[index]
	return wf, exists
}

// GetWaveformCount returns the number of stored waveforms.
func (wm *WaveformManager) GetWaveformCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	return len(wm.waveforms)
}

// AddChannel adds a waveform channel to a group.
func (wm *WaveformManager) AddChannel(groupIndex int, channel WaveformChannel) error {
	if channel.Label == "" {
		return fmt.Errorf("channel label cannot be empty")
	}

	if len(channel.Data) == 0 {
		return fmt.Errorf("channel data cannot be empty")
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	wm.channels[groupIndex] = append(wm.channels[groupIndex], channel)
	return nil
}

// GetChannels retrieves all channels for a waveform group.
func (wm *WaveformManager) GetChannels(groupIndex int) []WaveformChannel {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	channels := wm.channels[groupIndex]
	result := make([]WaveformChannel, len(channels))
	copy(result, channels)
	return result
}

// AnalyzeChannel computes min, max, and mean statistics for a channel.
func (wm *WaveformManager) AnalyzeChannel(groupIndex int, channelIdx int) (*WaveformChannel, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	channels, exists := wm.channels[groupIndex]
	if !exists || channelIdx >= len(channels) {
		return nil, fmt.Errorf("channel not found")
	}

	ch := channels[channelIdx]

	if len(ch.Data) > 0 {
		min, max := ch.Data[0], ch.Data[0]
		sum := 0
		for _, v := range ch.Data {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += int(v)
		}
		ch.MinValue = min
		ch.MaxValue = max
		ch.MeanValue = float64(sum) / float64(len(ch.Data))
		ch.SampleCount = len(ch.Data)
	}

	return &ch, nil
}

// GetChannelSegment extracts a sample range from a channel.
func (wm *WaveformManager) GetChannelSegment(groupIndex int, channelIdx int, startSample int, endSample int) ([]int16, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	channels, exists := wm.channels[groupIndex]
	if !exists || channelIdx >= len(channels) {
		return nil, fmt.Errorf("channel not found")
	}

	ch := channels[channelIdx]
	if startSample < 0 || endSample > len(ch.Data) || startSample >= endSample {
		return nil, fmt.Errorf("invalid sample range")
	}

	result := make([]int16, endSample-startSample)
	copy(result, ch.Data[startSample:endSample])
	return result, nil
}

// DetectQRSComplex detects QRS complexes in ECG data using threshold detection.
func (wm *WaveformManager) DetectQRSComplex(groupIndex int, channelIdx int, threshold int16) ([]int, error) {
	wm.mu.RLock()
	channels, exists := wm.channels[groupIndex]
	wm.mu.RUnlock()

	if !exists || channelIdx >= len(channels) {
		return nil, fmt.Errorf("channel not found")
	}

	ch := channels[channelIdx]
	var qrsIndices []int

	for i := 1; i < len(ch.Data)-1; i++ {
		if ch.Data[i] > threshold && ch.Data[i] > ch.Data[i-1] && ch.Data[i] > ch.Data[i+1] {
			qrsIndices = append(qrsIndices, i)
		}
	}

	return qrsIndices, nil
}

// CalculateHeartRate calculates heart rate in BPM from QRS detections.
func (wm *WaveformManager) CalculateHeartRate(qrsIndices []int, samplingFreq float64) float64 {
	if len(qrsIndices) < 2 {
		return 0
	}

	totalInterval := qrsIndices[len(qrsIndices)-1] - qrsIndices[0]
	avgInterval := float64(totalInterval) / float64(len(qrsIndices)-1)

	return (60 * samplingFreq) / avgInterval
}

// RemoveWaveform removes a waveform group and its channels.
func (wm *WaveformManager) RemoveWaveform(index int) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if _, exists := wm.waveforms[index]; exists {
		delete(wm.waveforms, index)
		delete(wm.channels, index)
		return true
	}
	return false
}

// ValidateWaveform checks if a waveform is properly structured.
func (wm *WaveformManager) ValidateWaveform(index int) error {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	wf, exists := wm.waveforms[index]
	if !exists {
		return fmt.Errorf("waveform not found")
	}

	if wf.NumberOfWaveformChannels <= 0 {
		return fmt.Errorf("invalid channel count")
	}

	if wf.NumberOfSamples <= 0 {
		return fmt.Errorf("invalid sample count")
	}

	if wf.SamplingFrequency <= 0 {
		return fmt.Errorf("invalid sampling frequency")
	}

	if len(wf.WaveformData) != wf.NumberOfWaveformChannels {
		return fmt.Errorf("data channel count mismatch")
	}

	return nil
}

// CopyWaveform creates a deep copy of a waveform group.
func (wm *WaveformManager) CopyWaveform(index int) (*WaveformGroup, error) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	wf, exists := wm.waveforms[index]
	if !exists {
		return nil, fmt.Errorf("waveform not found")
	}

	dataCopy := make([][]int16, len(wf.WaveformData))
	for i, ch := range wf.WaveformData {
		dataCopy[i] = make([]int16, len(ch))
		copy(dataCopy[i], ch)
	}

	return &WaveformGroup{
		WaveformOriginality:      wf.WaveformOriginality,
		NumberOfWaveformChannels: wf.NumberOfWaveformChannels,
		NumberOfSamples:          wf.NumberOfSamples,
		SamplingFrequency:        wf.SamplingFrequency,
		MultiplexGroupLabel:      wf.MultiplexGroupLabel,
		ChannelLabel:             append([]string{}, wf.ChannelLabel...),
		ChannelSource:            append([]string{}, wf.ChannelSource...),
		ChannelUnits:             append([]string{}, wf.ChannelUnits...),
		WaveformData:             dataCopy,
		TimeStamp:                wf.TimeStamp,
		SampleInterpretation:     wf.SampleInterpretation,
	}, nil
}
