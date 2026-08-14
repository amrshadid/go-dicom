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

// A guide has one job: tell the caller whether they must supply a decoder. It
// used to get that backwards for two of the three codecs, instructing the reader
// to install a C library and write CGO bindings for JPEG-LS and lossless JPEG
// while this package was decoding both in pure Go. The test asserted the wrong
// claim too — it required the JPEG-LS guide to name libcharls — so the guides and
// the test agreed with each other and not with the code.
//
// These cases are therefore pinned to whether a decoder is bundled, and cross
// checked against the registry rather than against a hardcoded expectation, so
// that bundling a codec (JPEG 2000, one day) fails here until its guide is
// rewritten.
func TestGetImplementationGuide(t *testing.T) {
	bundled := []compress.CompressionType{compress.JPEG_LS, compress.JPEG_LOSSLESS}
	supplied := []compress.CompressionType{compress.JPEG_2000}

	registry := compress.GetExternalRegistry()

	for _, ct := range bundled {
		guide := compress.GetImplementationGuide(ct)

		if !registry.IsExternalDecoderAvailable(ct) {
			t.Errorf("%s is documented as bundled but no decoder is registered for it", ct)
		}
		if !strings.Contains(guide, "pure Go") {
			t.Errorf("the %s guide does not say it is decoded in pure Go:\n%s", ct, guide)
		}
		if strings.Contains(guide, "CGO_ENABLED=1") {
			t.Errorf("the %s guide still tells the caller to rebuild with CGO_ENABLED=1, "+
				"which has never enabled anything in this module:\n%s", ct, guide)
		}
		if strings.Contains(guide, "Not decoded by this package") {
			t.Errorf("the %s guide claims the codec does not decode, but it does:\n%s", ct, guide)
		}
	}

	for _, ct := range supplied {
		guide := compress.GetImplementationGuide(ct)

		if registry.IsExternalDecoderAvailable(ct) {
			t.Errorf("%s has a decoder registered, so its guide should no longer tell "+
				"the caller to supply one", ct)
		}
		if !strings.Contains(guide, "RegisterExternalDecoder") {
			t.Errorf("the %s guide does not say how to supply a decoder:\n%s", ct, guide)
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
		// Every guide should name the transfer syntaxes it covers, since that is
		// what a caller holding a file has in hand.
		if !strings.Contains(guide, "1.2.840.10008.1.2.4.") {
			t.Errorf("the %s guide names no transfer syntax:\n%s", compressionType, guide)
		}

		// And every one should say how to substitute a decoder, whether or not one
		// is bundled — substitution is the point of the registry.
		if !strings.Contains(guide, "RegisterExternalDecoder") {
			t.Errorf("the %s guide does not mention RegisterExternalDecoder:\n%s",
				compressionType, guide)
		}

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
