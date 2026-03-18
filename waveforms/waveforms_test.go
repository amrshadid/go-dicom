package waveforms_test

import (
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/waveforms"
)

func TestNewWaveformManager(t *testing.T) {
	wm := waveforms.NewWaveformManager()
	if wm == nil {
		t.Fatal("NewWaveformManager returned nil")
	}
	if wm.GetWaveformCount() != 0 {
		t.Error("New manager should have no waveforms")
	}
}

func TestAddWaveform(t *testing.T) {
	wm := waveforms.NewWaveformManager()
	wf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 3,
		NumberOfSamples:          1000,
		SamplingFrequency:        500,
		WaveformData:             make([][]int16, 3),
		ChannelLabel:             []string{"I", "II", "III"},
	}
	for i := 0; i < 3; i++ {
		wf.WaveformData[i] = make([]int16, 1000)
	}

	err := wm.AddWaveform(0, wf)
	if err != nil {
		t.Fatalf("AddWaveform failed: %v", err)
	}
	if wm.GetWaveformCount() != 1 {
		t.Error("Count should be 1")
	}
}

func TestAddWaveformErrors(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	tests := []struct {
		name      string
		wf        *waveforms.WaveformGroup
		shouldErr bool
	}{
		{
			name:      "Nil waveform",
			wf:        nil,
			shouldErr: true,
		},
		{
			name: "Invalid channels",
			wf: &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 0,
				NumberOfSamples:          100,
			},
			shouldErr: true,
		},
		{
			name: "Invalid samples",
			wf: &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 3,
				NumberOfSamples:          0,
			},
			shouldErr: true,
		},
		{
			name: "Channel count mismatch",
			wf: &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 3,
				NumberOfSamples:          1000,
				WaveformData:             make([][]int16, 2),
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wm.AddWaveform(0, tt.wf)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestGetWaveform(t *testing.T) {
	wm := waveforms.NewWaveformManager()
	wf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 3,
		NumberOfSamples:          1000,
		SamplingFrequency:        500,
		WaveformData:             make([][]int16, 3),
		ChannelLabel:             []string{"I", "II", "III"},
	}
	for i := 0; i < 3; i++ {
		wf.WaveformData[i] = make([]int16, 1000)
	}

	wm.AddWaveform(1, wf)
	retrieved, exists := wm.GetWaveform(1)
	if !exists {
		t.Error("GetWaveform should find waveform")
	}
	if retrieved.NumberOfWaveformChannels != 3 {
		t.Error("Channel count mismatch")
	}

	_, exists = wm.GetWaveform(999)
	if exists {
		t.Error("GetWaveform should not find non-existent")
	}
}

func TestAddChannel(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	ch := waveforms.WaveformChannel{
		Label:      "ECG-I",
		Source:     "Lead I",
		Units:      "mV",
		SampleRate: 500,
		Data:       make([]int16, 100),
	}

	err := wm.AddChannel(0, ch)
	if err != nil {
		t.Fatalf("AddChannel failed: %v", err)
	}

	channels := wm.GetChannels(0)
	if len(channels) != 1 {
		t.Error("Channel count should be 1")
	}
}

func TestAddChannelErrors(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	tests := []struct {
		name      string
		ch        waveforms.WaveformChannel
		shouldErr bool
	}{
		{
			name:      "Empty label",
			ch:        waveforms.WaveformChannel{Data: make([]int16, 100)},
			shouldErr: true,
		},
		{
			name:      "Empty data",
			ch:        waveforms.WaveformChannel{Label: "Test"},
			shouldErr: true,
		},
		{
			name: "Valid channel",
			ch: waveforms.WaveformChannel{
				Label: "ECG",
				Data:  make([]int16, 100),
			},
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wm.AddChannel(0, tt.ch)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestAnalyzeChannel(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	// Create channel with known values
	data := []int16{10, 20, 30, 40, 50}
	ch := waveforms.WaveformChannel{
		Label: "Test",
		Data:  data,
	}

	wm.AddChannel(0, ch)

	analyzed, err := wm.AnalyzeChannel(0, 0)
	if err != nil {
		t.Fatalf("AnalyzeChannel failed: %v", err)
	}

	if analyzed.MinValue != 10 {
		t.Errorf("MinValue should be 10, got %d", analyzed.MinValue)
	}
	if analyzed.MaxValue != 50 {
		t.Errorf("MaxValue should be 50, got %d", analyzed.MaxValue)
	}
	if analyzed.MeanValue != 30 {
		t.Errorf("MeanValue should be 30, got %f", analyzed.MeanValue)
	}
	if analyzed.SampleCount != 5 {
		t.Errorf("SampleCount should be 5, got %d", analyzed.SampleCount)
	}
}

func TestGetChannelSegment(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	// Create channel with 100 samples
	data := make([]int16, 100)
	for i := 0; i < 100; i++ {
		data[i] = int16(i)
	}

	ch := waveforms.WaveformChannel{
		Label: "Test",
		Data:  data,
	}
	wm.AddChannel(0, ch)

	segment, err := wm.GetChannelSegment(0, 0, 10, 20)
	if err != nil {
		t.Fatalf("GetChannelSegment failed: %v", err)
	}

	if len(segment) != 10 {
		t.Errorf("Segment length should be 10, got %d", len(segment))
	}

	if segment[0] != 10 {
		t.Errorf("First value should be 10, got %d", segment[0])
	}
}

func TestDetectQRSComplex(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	// Create synthetic ECG with peaks
	data := make([]int16, 20)
	data[5] = 100  // Peak
	data[10] = 150 // Higher peak
	data[15] = 80  // Another peak

	ch := waveforms.WaveformChannel{
		Label: "ECG",
		Data:  data,
	}
	wm.AddChannel(0, ch)

	qrs, err := wm.DetectQRSComplex(0, 0, 50)
	if err != nil {
		t.Fatalf("DetectQRSComplex failed: %v", err)
	}

	if len(qrs) == 0 {
		t.Error("Should detect at least one QRS complex")
	}
}

func TestCalculateHeartRate(t *testing.T) {
	// Simulate QRS detections
	qrsIndices := []int{100, 250, 400, 550} // 150 samples between each
	samplingFreq := 250.0                   // Hz

	hr := (&waveforms.WaveformManager{}).CalculateHeartRate(qrsIndices, samplingFreq)

	// Expected: 60 * 250 / 150 = 100 BPM
	if hr < 90 || hr > 110 {
		t.Errorf("Heart rate should be ~100 BPM, got %f", hr)
	}
}

func TestRemoveWaveform(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	wf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 1,
		NumberOfSamples:          100,
		WaveformData:             make([][]int16, 1),
	}
	wf.WaveformData[0] = make([]int16, 100)

	wm.AddWaveform(0, wf)
	if wm.GetWaveformCount() != 1 {
		t.Error("Should have 1 waveform")
	}

	removed := wm.RemoveWaveform(0)
	if !removed {
		t.Error("RemoveWaveform should return true")
	}
	if wm.GetWaveformCount() != 0 {
		t.Error("Should have 0 waveforms")
	}
}

func TestValidateWaveform(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	tests := []struct {
		name      string
		wf        *waveforms.WaveformGroup
		shouldErr bool
	}{
		{
			name: "Valid waveform",
			wf: &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 3,
				NumberOfSamples:          1000,
				SamplingFrequency:        500,
				WaveformData:             make([][]int16, 3),
			},
			shouldErr: false,
		},
		{
			name: "Invalid channels",
			wf: &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 0,
				NumberOfSamples:          1000,
				SamplingFrequency:        500,
				WaveformData:             make([][]int16, 0),
			},
			shouldErr: true,
		},
	}

	for i, tt := range tests {
		if tt.wf != nil && !tt.shouldErr {
			wm.AddWaveform(i, tt.wf)
		}

		t.Run(tt.name, func(t *testing.T) {
			err := wm.ValidateWaveform(i)
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error: %v, got: %v", tt.shouldErr, err)
			}
		})
	}
}

func TestCopyWaveform(t *testing.T) {
	wm := waveforms.NewWaveformManager()

	original := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 2,
		NumberOfSamples:          100,
		SamplingFrequency:        500,
		WaveformData:             make([][]int16, 2),
		ChannelLabel:             []string{"I", "II"},
	}
	for i := 0; i < 2; i++ {
		original.WaveformData[i] = make([]int16, 100)
		original.WaveformData[i][0] = int16(i * 10)
	}

	wm.AddWaveform(0, original)

	copy, err := wm.CopyWaveform(0)
	if err != nil {
		t.Fatalf("CopyWaveform failed: %v", err)
	}

	if copy.NumberOfWaveformChannels != 2 {
		t.Error("Channel count mismatch")
	}

	if &copy.WaveformData == &original.WaveformData {
		t.Error("Should be deep copy")
	}
}

func TestConcurrentWaveformOperations(t *testing.T) {
	wm := waveforms.NewWaveformManager()
	done := make(chan bool)

	// Add waveforms concurrently
	for i := 0; i < 5; i++ {
		go func(idx int) {
			wf := &waveforms.WaveformGroup{
				NumberOfWaveformChannels: 3,
				NumberOfSamples:          1000,
				SamplingFrequency:        500,
				WaveformData:             make([][]int16, 3),
			}
			for j := 0; j < 3; j++ {
				wf.WaveformData[j] = make([]int16, 1000)
			}
			wm.AddWaveform(idx, wf)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	if wm.GetWaveformCount() != 5 {
		t.Errorf("Expected 5 waveforms, got %d", wm.GetWaveformCount())
	}
}

func TestWaveformMetadata(t *testing.T) {
	wf := &waveforms.WaveformGroup{
		NumberOfWaveformChannels: 3,
		NumberOfSamples:          1000,
		SamplingFrequency:        500,
		WaveformOriginality:      "ORIGINAL",
		MultiplexGroupLabel:      "12-Lead ECG",
		TimeStamp:                time.Now(),
		DateOfCalibration:        "20231022",
		TimeOfCalibration:        "143000",
		WaveformData:             make([][]int16, 3),
	}
	for i := 0; i < 3; i++ {
		wf.WaveformData[i] = make([]int16, 1000)
	}

	if wf.WaveformOriginality != "ORIGINAL" {
		t.Error("Originality mismatch")
	}
	if !wf.TimeStamp.IsZero() == false {
		t.Error("Timestamp should be set")
	}
}
