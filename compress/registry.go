// Copyright 2025 Go DICOM authors. See LICENSE file for details.
// Code generated and added for production integration.

package compress

import (
	"fmt"
)

// ========================================================================
// Transfer Syntax UID Constants
// ========================================================================

const (
	// Uncompressed transfer syntaxes
	ImplicitVRLittleEndian = "1.2.840.10008.1.2"
	ExplicitVRLittleEndian = "1.2.840.10008.1.2.1"
	ExplicitVRBigEndian    = "1.2.840.10008.1.2.2"
	// The deflated syntax is 1.2.840.10008.1.2.1.99, an extension of Explicit
	// VR Little Endian. This constant previously read 1.2.840.10008.1.2.4.1,
	// which is in the JPEG arc and is not a transfer syntax at all — so a real
	// deflated file fell through to "unknown transfer syntax" while a UID no
	// file carries mapped to DEFLATE.
	DeflatedExplicitVRLittleEnd = "1.2.840.10008.1.2.1.99"

	// Compressed transfer syntaxes
	RLELossless             = "1.2.840.10008.1.2.5"
	JPEGBaseline            = "1.2.840.10008.1.2.4.50"
	JPEGExtended            = "1.2.840.10008.1.2.4.51"
	JPEGLossless            = "1.2.840.10008.1.2.4.70"
	JPEGLosslessNonHierarch = "1.2.840.10008.1.2.4.71"
	JPEGLSLossless          = "1.2.840.10008.1.2.4.80"
	JPEGLSNearLossless      = "1.2.840.10008.1.2.4.81"
	JPEG2000Lossless        = "1.2.840.10008.1.2.4.90"
	JPEG2000Lossy           = "1.2.840.10008.1.2.4.91"
)

// ========================================================================
// Transfer Syntax to Compression Type Mapping
// ========================================================================

// TransferSyntaxToCompressionType maps DICOM transfer syntax UIDs to compression types
func TransferSyntaxToCompressionType(uid string) (CompressionType, error) {
	switch uid {
	case ImplicitVRLittleEndian, ExplicitVRLittleEndian, ExplicitVRBigEndian:
		return UNCOMPRESSED, nil
	case RLELossless:
		return RLE, nil
	case JPEGBaseline, JPEGExtended:
		return JPEG, nil
	case JPEGLossless, JPEGLosslessNonHierarch:
		return JPEG_LOSSLESS, nil
	case JPEGLSLossless, JPEGLSNearLossless:
		return JPEG_LS, nil
	case JPEG2000Lossless, JPEG2000Lossy:
		return JPEG_2000, nil
	case DeflatedExplicitVRLittleEnd:
		return DEFLATE, nil
	default:
		return "", fmt.Errorf("unknown transfer syntax: %s", uid)
	}
}

// ========================================================================
// Helper Functions for Transfer Syntax Detection
// ========================================================================

// IsCompressed returns true if the transfer syntax indicates compressed data
func IsCompressed(uid string) bool {
	switch uid {
	case ImplicitVRLittleEndian, ExplicitVRLittleEndian, ExplicitVRBigEndian:
		return false
	default:
		return true
	}
}

// GetCompressionType returns the compression type for the given transfer syntax
func GetCompressionType(uid string) CompressionType {
	switch uid {
	case RLELossless:
		return RLE
	case JPEGBaseline, JPEGExtended:
		return JPEG
	case JPEGLossless, JPEGLosslessNonHierarch:
		return JPEG_LOSSLESS
	case JPEGLSLossless, JPEGLSNearLossless:
		return JPEG_LS
	case JPEG2000Lossless, JPEG2000Lossy:
		return JPEG_2000
	case DeflatedExplicitVRLittleEnd:
		return DEFLATE
	default:
		return UNCOMPRESSED
	}
}
