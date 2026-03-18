package charset_test

import (
	"context"
	"testing"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/config"
)

func TestConvertEncodings(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    []string
		wantErr bool
	}{
		{
			name:    "empty values returns default",
			values:  []string{},
			want:    []string{charset.DefaultEncoding},
			wantErr: false,
		},
		{
			name:    "single empty string returns default",
			values:  []string{""},
			want:    []string{charset.DefaultEncoding},
			wantErr: false,
		},
		{
			name:    "single UTF-8",
			values:  []string{"ISO_IR 192"},
			want:    []string{"UTF-8"},
			wantErr: false,
		},
		{
			name:    "single Latin-1",
			values:  []string{"ISO_IR 100"},
			want:    []string{"ISO-8859-1"},
			wantErr: false,
		},
		{
			name:    "multi-valued with code extensions",
			values:  []string{"ISO 2022 IR 87", "ISO 2022 IR 13"},
			want:    []string{"ISO-2022-JP", "Shift_JIS"},
			wantErr: false,
		},
		{
			name:    "Korean",
			values:  []string{"ISO 2022 IR 149"},
			want:    []string{"EUC-KR"},
			wantErr: false,
		},
		{
			name:    "Chinese GB18030",
			values:  []string{"GB18030"},
			want:    []string{"GB18030"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.ConvertEncodings(tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertEncodings() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ConvertEncodings() got %d encodings, want %d", len(got), len(tt.want))
				return
			}

			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("ConvertEncodings()[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestConvertEncodingsWithMisspellings(t *testing.T) {
	// Test with WARN mode (default) - should correct misspellings
	tests := []struct {
		name      string
		values    []string
		wantFirst string
	}{
		{
			name:      "missing underscore in ISO_IR",
			values:    []string{"ISO IR 192"},
			wantFirst: "UTF-8",
		},
		{
			name:      "wrong separators in code extensions",
			values:    []string{"ISO_2022_IR_100"},
			wantFirst: "ISO-8859-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := charset.ConvertEncodings(tt.values)
			if err != nil {
				t.Errorf("ConvertEncodings() unexpected error = %v", err)
				return
			}

			if len(got) == 0 {
				t.Error("ConvertEncodings() returned empty slice")
				return
			}

			if got[0] != tt.wantFirst {
				t.Errorf("ConvertEncodings()[0] = %q, want %q", got[0], tt.wantFirst)
			}
		})
	}
}

func TestConvertEncodingsStandAloneValidation(t *testing.T) {
	tests := []struct {
		name       string
		values     []string
		wantCount  int
		wantFirst  string
		mode       config.ValidationMode
		wantErrMsg bool
	}{
		{
			name:       "UTF-8 with code extensions in WARN mode",
			values:     []string{"ISO_IR 192", "ISO_IR 100"},
			wantCount:  1,
			wantFirst:  "UTF-8",
			mode:       config.WARN,
			wantErrMsg: false,
		},
		{
			name:       "GB18030 with code extensions in WARN mode",
			values:     []string{"GB18030", "ISO_IR 100"},
			wantCount:  1,
			wantFirst:  "GB18030",
			mode:       config.WARN,
			wantErrMsg: false,
		},
		{
			name:       "code extension with UTF-8 in second position in WARN mode",
			values:     []string{"ISO_IR 100", "ISO_IR 192"},
			wantCount:  1,
			wantFirst:  "ISO-8859-1",
			mode:       config.WARN,
			wantErrMsg: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set validation mode
			ctx := config.WithReadingValidationMode(context.Background(), tt.mode)

			got, err := charset.ConvertEncodingsWithContext(ctx, tt.values)
			if tt.wantErrMsg && err == nil {
				t.Error("ConvertEncodingsWithContext() expected error but got nil")
				return
			}

			if !tt.wantErrMsg && err != nil {
				t.Errorf("ConvertEncodingsWithContext() unexpected error = %v", err)
				return
			}

			if len(got) != tt.wantCount {
				t.Errorf("ConvertEncodingsWithContext() got %d encodings, want %d", len(got), tt.wantCount)
				return
			}

			if len(got) > 0 && got[0] != tt.wantFirst {
				t.Errorf("ConvertEncodingsWithContext()[0] = %q, want %q", got[0], tt.wantFirst)
			}
		})
	}
}

func TestConvertEncodingsStrictMode(t *testing.T) {
	// Test with RAISE mode for unknown encodings
	ctx := config.WithReadingValidationMode(context.Background(), config.RAISE)

	_, err := charset.ConvertEncodingsWithContext(ctx, []string{"UNKNOWN_ENCODING"})
	if err == nil {
		t.Error("ConvertEncodingsWithContext() with unknown encoding in RAISE mode should return error")
	}
}

func TestConvertEncodingsIgnoreMode(t *testing.T) {
	// Test with IGNORE mode for unknown encodings
	ctx := config.WithReadingValidationMode(context.Background(), config.IGNORE)

	got, err := charset.ConvertEncodingsWithContext(ctx, []string{"UNKNOWN_ENCODING"})
	if err != nil {
		t.Errorf("ConvertEncodingsWithContext() in IGNORE mode unexpected error = %v", err)
		return
	}

	if len(got) != 1 || got[0] != charset.DefaultEncoding {
		t.Errorf("ConvertEncodingsWithContext() in IGNORE mode = %v, want [%s]", got, charset.DefaultEncoding)
	}
}

func TestNewCharacterSet(t *testing.T) {
	tests := []struct {
		name              string
		values            []string
		wantDefault       bool
		wantEncodingCount int
		wantPrimary       string
	}{
		{
			name:              "empty values creates default",
			values:            []string{},
			wantDefault:       true,
			wantEncodingCount: 1,
			wantPrimary:       charset.DefaultEncoding,
		},
		{
			name:              "UTF-8 character set",
			values:            []string{"ISO_IR 192"},
			wantDefault:       false,
			wantEncodingCount: 1,
			wantPrimary:       "UTF-8",
		},
		{
			name:              "multi-valued character set",
			values:            []string{"ISO 2022 IR 87", "ISO 2022 IR 13"},
			wantDefault:       false,
			wantEncodingCount: 2,
			wantPrimary:       "ISO-2022-JP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := charset.NewCharacterSet(tt.values)
			if err != nil {
				t.Errorf("NewCharacterSet() error = %v", err)
				return
			}

			if cs.IsDefault != tt.wantDefault {
				t.Errorf("NewCharacterSet().IsDefault = %v, want %v", cs.IsDefault, tt.wantDefault)
			}

			if len(cs.Encodings) != tt.wantEncodingCount {
				t.Errorf("NewCharacterSet() encoding count = %d, want %d", len(cs.Encodings), tt.wantEncodingCount)
			}

			if cs.PrimaryEncoding() != tt.wantPrimary {
				t.Errorf("NewCharacterSet().PrimaryEncoding() = %q, want %q", cs.PrimaryEncoding(), tt.wantPrimary)
			}

			if tt.wantEncodingCount > 1 {
				if !cs.SupportsCodeExtensions() {
					t.Error("NewCharacterSet().SupportsCodeExtensions() should be true for multi-valued")
				}
			} else {
				if cs.SupportsCodeExtensions() {
					t.Error("NewCharacterSet().SupportsCodeExtensions() should be false for single-valued")
				}
			}
		})
	}
}
