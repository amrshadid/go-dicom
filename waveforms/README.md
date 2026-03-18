# Waveforms

Thread-safe management of DICOM physiological signal data (ECG, EEG) with multi-channel support, QRS complex detection, heart rate calculation, statistical analysis, and time window extraction.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/waveforms"

wm := waveforms.NewWaveformManager()

// Create a 3-lead ECG waveform
wf := &waveforms.WaveformGroup{
    NumberOfWaveformChannels: 3,
    NumberOfSamples:          1000,
    SamplingFrequency:        500,
    WaveformOriginality:      "ORIGINAL",
    ChannelLabel:             []string{"Lead I", "Lead II", "Lead III"},
    ChannelUnits:             []string{"mV", "mV", "mV"},
    WaveformData:             make([][]int16, 3),
}
wm.AddWaveform(0, wf)

// ECG analysis
qrsIndices, _ := wm.DetectQRSComplex(0, 0, 50)
bpm := wm.CalculateHeartRate(qrsIndices, 500.0)

// Channel statistics
analyzed, _ := wm.AnalyzeChannel(0, 0)
fmt.Printf("Min: %d, Max: %d, Mean: %.2f\n", analyzed.MinValue, analyzed.MaxValue, analyzed.MeanValue)

// Extract time window
segment, _ := wm.GetChannelSegment(0, 0, 500, 600)
```

## API Reference

```go
func NewWaveformManager() *WaveformManager

// Waveform operations
func (wm *WaveformManager) AddWaveform(index int, wf *WaveformGroup) error
func (wm *WaveformManager) GetWaveform(index int) (*WaveformGroup, bool)
func (wm *WaveformManager) RemoveWaveform(index int) bool
func (wm *WaveformManager) GetWaveformCount() int
func (wm *WaveformManager) ValidateWaveform(index int) error
func (wm *WaveformManager) CopyWaveform(index int) (*WaveformGroup, error)

// Channel operations
func (wm *WaveformManager) AddChannel(groupIndex int, channel WaveformChannel) error
func (wm *WaveformManager) GetChannels(groupIndex int) []WaveformChannel
func (wm *WaveformManager) AnalyzeChannel(groupIndex, channelIdx int) (*WaveformChannel, error)
func (wm *WaveformManager) GetChannelSegment(groupIndex, channelIdx, start, end int) ([]int16, error)

// ECG analysis
func (wm *WaveformManager) DetectQRSComplex(groupIndex, channelIdx int, threshold int16) ([]int, error)
func (wm *WaveformManager) CalculateHeartRate(qrsIndices []int, samplingFreq float64) float64

type WaveformGroup struct {
    NumberOfWaveformChannels, NumberOfSamples int
    SamplingFrequency float64; WaveformOriginality string
    ChannelLabel, ChannelSource, ChannelUnits []string
    WaveformData [][]int16; TimeStamp time.Time
}

type WaveformChannel struct {
    Label, Source, Units string; SampleRate float64
    SampleCount int; MinValue, MaxValue int16; MeanValue float64
    Data []int16
}
```

## References

- [DICOM PS3.3](https://dicom.nema.org/medical/dicom/current/output/html/part03.html) - Waveform IOD definitions
- DICOM PS3.6 - Waveform-related tags (54xx series)
