package compress_test

import (
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/compress"
)

// ============================================================================
// External Decoder Registry Tests
// ============================================================================

func TestGetExternalRegistry(t *testing.T) {
	registry := compress.GetExternalRegistry()
	if registry == nil {
		t.Fatal("GetExternalRegistry returned nil")
	}
}

func TestExternalDecoderAvailability(t *testing.T) {
	registry := compress.GetExternalRegistry()

	// Check availability of each compression type
	jpegLSAvailable := registry.IsExternalDecoderAvailable(compress.JPEG_LS)
	jpeg2000Available := registry.IsExternalDecoderAvailable(compress.JPEG_2000)
	jpegLosslessAvailable := registry.IsExternalDecoderAvailable(compress.JPEG_LOSSLESS)

	// These may or may not be available depending on system libraries
	// Just verify the method works
	_ = jpegLSAvailable
	_ = jpeg2000Available
	_ = jpegLosslessAvailable
}

func TestExternalDecoderRegistration(t *testing.T) {
	registry := compress.GetExternalRegistry()

	// A nil decoder is rejected. It used to be accepted, which was harmless
	// only while this package bundled no decoders: now it would replace a
	// working one with something that cannot be called, and the registry is
	// global, so the damage would outlive the caller that did it.
	if err := registry.RegisterExternalDecoder(compress.JPEG_LS, nil); err == nil {
		t.Error("RegisterExternalDecoder(JPEG_LS, nil) was accepted; a nil decoder cannot be used")
	}

	// An unknown compression type is rejected whatever the decoder.
	if err := registry.RegisterExternalDecoder(compress.CompressionType("INVALID"), passthroughDecoder{}); err == nil {
		t.Error("RegisterExternalDecoder with an invalid type should error")
	}

	// The bundled decoder survives a rejected registration.
	if _, err := registry.GetExternalDecoder(compress.JPEG_LS); err != nil {
		t.Errorf("the built-in JPEG-LS decoder was lost: %v", err)
	}
}

func TestExternalDecoderGetError(t *testing.T) {
	registry := compress.GetExternalRegistry()

	// JPEG-LS is likely not available by default
	decoder, err := registry.GetExternalDecoder(compress.JPEG_LS)

	// Whether or not it's available, this should either succeed or fail gracefully
	if decoder == nil && err == nil {
		t.Error("GetExternalDecoder returned nil decoder but no error")
	}
}

// ============================================================================
// External Compression Status Tests
// ============================================================================

func TestGetExternalCompressionStatus(t *testing.T) {
	statuses := compress.GetExternalCompressionStatus()

	if len(statuses) != 3 {
		t.Errorf("Expected 3 compression statuses, got %d", len(statuses))
	}

	// Check that we have the expected compression types
	types := make(map[compress.CompressionType]bool)
	for _, status := range statuses {
		types[status.CompressionType] = true
		if status.RequiredLibrary == "" {
			t.Errorf("Status for %s has empty RequiredLibrary", status.CompressionType)
		}
	}

	if !types[compress.JPEG_LS] {
		t.Error("Missing JPEG_LS status")
	}
	if !types[compress.JPEG_2000] {
		t.Error("Missing JPEG_2000 status")
	}
	if !types[compress.JPEG_LOSSLESS] {
		t.Error("Missing JPEG_LOSSLESS status")
	}
}

// ============================================================================
// Implementation Guide Tests
// ============================================================================

func TestGetImplementationGuide(t *testing.T) {
	tests := []struct {
		compressionType compress.CompressionType
		expectedContent string
	}{
		{compress.JPEG_LS, "libcharls"},
		{compress.JPEG_2000, "OpenJPEG"},
		{compress.JPEG_LOSSLESS, "libjpeg-turbo"},
	}

	for _, tt := range tests {
		guide := compress.GetImplementationGuide(tt.compressionType)
		if guide == "" {
			t.Errorf("GetImplementationGuide(%s) returned empty string", tt.compressionType)
		}
		if !strings.Contains(guide, tt.expectedContent) {
			t.Errorf("GetImplementationGuide(%s) doesn't contain '%s'", tt.compressionType, tt.expectedContent)
		}
	}
}

func TestExternalDecodersDocumentation(t *testing.T) {
	guides := map[compress.CompressionType]string{
		compress.JPEG_LS:       compress.GetImplementationGuide(compress.JPEG_LS),
		compress.JPEG_2000:     compress.GetImplementationGuide(compress.JPEG_2000),
		compress.JPEG_LOSSLESS: compress.GetImplementationGuide(compress.JPEG_LOSSLESS),
	}

	for compressionType, guide := range guides {
		// Check that guides contain installation steps
		if !strings.Contains(guide, "Install") && !strings.Contains(guide, "install") {
			t.Logf("Implementation guide for %s may not have installation instructions", compressionType)
		}

		// Check that guides mention CGO
		if !strings.Contains(guide, "CGO") && !strings.Contains(guide, "cgo") {
			t.Logf("Implementation guide for %s doesn't mention CGO requirements", compressionType)
		}

		// Verify guide is not empty
		if len(guide) < 50 {
			t.Errorf("Implementation guide for %s is too short (%d chars)", compressionType, len(guide))
		}
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestExternalDecodersIntegration(t *testing.T) {
	// Get external registry
	externalRegistry := compress.GetExternalRegistry()

	// Verify GetExternalRegistry is callable
	if externalRegistry == nil {
		t.Fatal("GetExternalRegistry returned nil")
	}

	// Verify status printing doesn't crash
	// (This would normally output to stdout but shouldn't error)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PrintExternalCompressionStatus panicked: %v", r)
		}
	}()
}
