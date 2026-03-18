package dataelem_test

import (
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/tag"
)

func TestConvertDA(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    interface{}
		wantVM  int
		wantErr bool
	}{
		{
			name:    "FullDate",
			input:   []byte("20231015"),
			want:    time.Date(2023, 10, 15, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "YearMonth",
			input:   []byte("202310"),
			want:    time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "YearOnly",
			input:   []byte("2023"),
			want:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "EmptyValue",
			input:   []byte(""),
			want:    nil,
			wantVM:  0,
			wantErr: false,
		},
		{
			name:  "MultiValue",
			input: []byte("20231015\\20240101"),
			want: []time.Time{
				time.Date(2023, 10, 15, 0, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantVM:  2,
			wantErr: false,
		},
		{
			name:    "WithSpaces",
			input:   []byte("  20231015  "),
			want:    time.Date(2023, 10, 15, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a RawDataElement for testing
			raw := dataelem.NewRawDataElement(
				tag.New(0x0008, 0x0020), // StudyDate
				dataelem.DA,
				uint32(len(tt.input)),
				tt.input,
				0,
				false,
				true,
				false,
			)

			de, err := dataelem.ConvertRawDataElement(raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRawDataElement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if de.GetVM() != tt.wantVM {
					t.Errorf("VM = %v, want %v", de.GetVM(), tt.wantVM)
				}

				// Compare values
				switch want := tt.want.(type) {
				case time.Time:
					got, ok := de.GetValue().(time.Time)
					if !ok {
						t.Errorf("Value type = %T, want time.Time", de.GetValue())
						return
					}
					if !got.Equal(want) {
						t.Errorf("Value = %v, want %v", got, want)
					}
				case []time.Time:
					got, ok := de.GetValue().([]time.Time)
					if !ok {
						t.Errorf("Value type = %T, want []time.Time", de.GetValue())
						return
					}
					if len(got) != len(want) {
						t.Errorf("Value length = %v, want %v", len(got), len(want))
						return
					}
					for i := range want {
						if !got[i].Equal(want[i]) {
							t.Errorf("Value[%d] = %v, want %v", i, got[i], want[i])
						}
					}
				case nil:
					if de.GetValue() != nil {
						t.Errorf("Value = %v, want nil", de.GetValue())
					}
				}
			}
		})
	}
}

func TestConvertDT(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    interface{}
		wantVM  int
		wantErr bool
	}{
		{
			name:    "FullDateTime",
			input:   []byte("20231015143000"),
			want:    time.Date(2023, 10, 15, 14, 30, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "WithFractionalSeconds",
			input:   []byte("20231015143000.123456"),
			want:    time.Date(2023, 10, 15, 14, 30, 0, 123456000, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "WithTimezone",
			input:   []byte("20231015143000+0500"),
			want:    time.Date(2023, 10, 15, 14, 30, 0, 0, time.FixedZone("", 5*60*60)),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "WithNegativeTimezone",
			input:   []byte("20231015143000-0400"),
			want:    time.Date(2023, 10, 15, 14, 30, 0, 0, time.FixedZone("", -4*60*60)),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "WithFractionalSecondsAndTimezone",
			input:   []byte("20231015143000.123456+0530"),
			want:    time.Date(2023, 10, 15, 14, 30, 0, 123456000, time.FixedZone("", 5*60*60+30*60)),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "DateOnly",
			input:   []byte("20231015"),
			want:    time.Date(2023, 10, 15, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "YearOnly",
			input:   []byte("2023"),
			want:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "PartialTime",
			input:   []byte("2023101514"),
			want:    time.Date(2023, 10, 15, 14, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "EmptyValue",
			input:   []byte(""),
			want:    nil,
			wantVM:  0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := dataelem.NewRawDataElement(
				tag.New(0x0008, 0x0030), // StudyTime
				dataelem.DT,
				uint32(len(tt.input)),
				tt.input,
				0,
				false,
				true,
				false,
			)

			de, err := dataelem.ConvertRawDataElement(raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRawDataElement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if de.GetVM() != tt.wantVM {
					t.Errorf("VM = %v, want %v", de.GetVM(), tt.wantVM)
				}

				// Compare values
				if tt.want == nil {
					if de.GetValue() != nil {
						t.Errorf("Value = %v, want nil", de.GetValue())
					}
				} else {
					want := tt.want.(time.Time)
					got, ok := de.GetValue().(time.Time)
					if !ok {
						t.Errorf("Value type = %T, want time.Time", de.GetValue())
						return
					}
					if !got.Equal(want) {
						t.Errorf("Value = %v, want %v", got, want)
					}
				}
			}
		})
	}
}

func TestConvertTM(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    interface{}
		wantVM  int
		wantErr bool
	}{
		{
			name:    "FullTime",
			input:   []byte("143000"),
			want:    time.Date(0, 1, 1, 14, 30, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "WithFractionalSeconds",
			input:   []byte("143000.123456"),
			want:    time.Date(0, 1, 1, 14, 30, 0, 123456000, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "HourMinute",
			input:   []byte("1430"),
			want:    time.Date(0, 1, 1, 14, 30, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "HourOnly",
			input:   []byte("14"),
			want:    time.Date(0, 1, 1, 14, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "Midnight",
			input:   []byte("000000"),
			want:    time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "EndOfDay",
			input:   []byte("235959"),
			want:    time.Date(0, 1, 1, 23, 59, 59, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
		{
			name:    "EmptyValue",
			input:   []byte(""),
			want:    nil,
			wantVM:  0,
			wantErr: false,
		},
		{
			name:  "MultiValue",
			input: []byte("143000\\180000"),
			want: []time.Time{
				time.Date(0, 1, 1, 14, 30, 0, 0, time.UTC),
				time.Date(0, 1, 1, 18, 0, 0, 0, time.UTC),
			},
			wantVM:  2,
			wantErr: false,
		},
		{
			name:    "WithSpaces",
			input:   []byte("  143000  "),
			want:    time.Date(0, 1, 1, 14, 30, 0, 0, time.UTC),
			wantVM:  1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := dataelem.NewRawDataElement(
				tag.New(0x0008, 0x0030), // StudyTime
				dataelem.TM,
				uint32(len(tt.input)),
				tt.input,
				0,
				false,
				true,
				false,
			)

			de, err := dataelem.ConvertRawDataElement(raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRawDataElement() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if de.GetVM() != tt.wantVM {
					t.Errorf("VM = %v, want %v", de.GetVM(), tt.wantVM)
				}

				// Compare values
				switch want := tt.want.(type) {
				case time.Time:
					got, ok := de.GetValue().(time.Time)
					if !ok {
						t.Errorf("Value type = %T, want time.Time", de.GetValue())
						return
					}
					if !got.Equal(want) {
						t.Errorf("Value = %v, want %v", got, want)
					}
				case []time.Time:
					got, ok := de.GetValue().([]time.Time)
					if !ok {
						t.Errorf("Value type = %T, want []time.Time", de.GetValue())
						return
					}
					if len(got) != len(want) {
						t.Errorf("Value length = %v, want %v", len(got), len(want))
						return
					}
					for i := range want {
						if !got[i].Equal(want[i]) {
							t.Errorf("Value[%d] = %v, want %v", i, got[i], want[i])
						}
					}
				case nil:
					if de.GetValue() != nil {
						t.Errorf("Value = %v, want nil", de.GetValue())
					}
				}
			}
		})
	}
}
