package compress

import (
	"fmt"
)

// ExternalDecoderRegistry holds the decoder used for each codec that is not
// decoded inline by Decompressor implementations in this package.
//
// "External" is historical: it once meant CGO bindings to C libraries, which
// this module has never contained. What it means now is substitutable. JPEG-LS
// and lossless JPEG are seeded with this package's own pure-Go decoders and work
// out of the box; JPEG 2000 starts empty and needs one supplied. Any of the
// three can be replaced with RegisterExternalDecoder.
type ExternalDecoderRegistry struct {
	jpegLSDecoder       Decompressor
	jpeg2000Decoder     Decompressor
	jpegLosslessDecoder Decompressor
}

var externalRegistry *ExternalDecoderRegistry

// GetExternalRegistry returns the external decoder registry
func GetExternalRegistry() *ExternalDecoderRegistry {
	if externalRegistry == nil {
		externalRegistry = &ExternalDecoderRegistry{}
		initializeExternalDecoders()
	}
	return externalRegistry
}

// initializeExternalDecoders seeds the registry with the decoders this package
// implements, so that JPEG-LS and lossless JPEG pixel data decodes without the
// caller registering anything.
//
// A nil entry means no decoder is bundled for that codec, which today is JPEG
// 2000 alone. GetExternalDecoder reports it with errNoDecoder, and
// RegisterExternalDecoder is how a caller fills it in.
func initializeExternalDecoders() {
	registry := GetExternalRegistry()

	registry.jpegLSDecoder = defaultJPEGLSDecoder()
	registry.jpeg2000Decoder = defaultJPEG2000Decoder()
	registry.jpegLosslessDecoder = defaultJPEGLosslessDecoder()
}

// RegisterExternalDecoder registers an external decoder
func (reg *ExternalDecoderRegistry) RegisterExternalDecoder(compressionType CompressionType, decoder Decompressor) error {
	// A nil decoder is never what a caller wants, and it is no longer harmless:
	// JPEG-LS and JPEG Lossless are decoded in this package now, so accepting
	// nil would quietly replace a working decoder with one that cannot be
	// called, and the failure would surface far from the registration.
	if decoder == nil {
		return fmt.Errorf("cannot register a nil decoder for %s", compressionType)
	}

	switch compressionType {
	case JPEG_LS:
		reg.jpegLSDecoder = decoder
		return nil
	case JPEG_2000:
		reg.jpeg2000Decoder = decoder
		return nil
	case JPEG_LOSSLESS:
		reg.jpegLosslessDecoder = decoder
		return nil
	default:
		return fmt.Errorf("unsupported external compression type: %s", compressionType)
	}
}

// GetExternalDecoder returns an external decoder if available
func (reg *ExternalDecoderRegistry) GetExternalDecoder(compressionType CompressionType) (Decompressor, error) {
	switch compressionType {
	case JPEG_LS:
		if reg.jpegLSDecoder == nil {
			return nil, errNoDecoder(JPEG_LS)
		}
		return reg.jpegLSDecoder, nil

	case JPEG_2000:
		if reg.jpeg2000Decoder == nil {
			return nil, errNoDecoder(JPEG_2000)
		}
		return reg.jpeg2000Decoder, nil

	case JPEG_LOSSLESS:
		if reg.jpegLosslessDecoder == nil {
			return nil, errNoDecoder(JPEG_LOSSLESS)
		}
		return reg.jpegLosslessDecoder, nil

	default:
		return nil, fmt.Errorf("unsupported external compression type: %s", compressionType)
	}
}

// IsExternalDecoderAvailable checks if an external decoder is available
func (reg *ExternalDecoderRegistry) IsExternalDecoderAvailable(compressionType CompressionType) bool {
	_, err := reg.GetExternalDecoder(compressionType)
	return err == nil
}

// errNoDecoder reports that a codec has no decoder, and says what to do.
//
// These messages used to instruct the caller to install a C library and rebuild
// with CGO_ENABLED=1. There is no CGO implementation in this module, so
// following that advice changed nothing: the library was named, the rebuild
// succeeded, and the same error came back. The instruction that does work is to
// supply a decoder.
func errNoDecoder(compressionType CompressionType) error {
	return fmt.Errorf("no %s decoder is registered. This module ships no %s implementation; "+
		"supply one with compress.GetExternalRegistry().RegisterExternalDecoder(compress.%s, yourDecoder). "+
		"Any type with Decompress([]byte) ([]byte, error) and CanDecompress([]byte) bool will do — "+
		"see the ExampleExternalDecoderRegistry_RegisterExternalDecoder example",
		compressionType, compressionType, compressionType)
}

// defaultJPEGLSDecoder returns this package's pure-Go JPEG-LS decoder.
//
// It goes in the registry rather than being called directly so that a caller can
// still substitute their own — for a faster codec, or one covering a case this
// one refuses. Nothing is probed and no C library is involved.
func defaultJPEGLSDecoder() Decompressor {
	return NewJPEGLSDecompressor()
}

// defaultJPEG2000Decoder returns nil: no JPEG 2000 decoder is bundled, and this
// is the only codec in the registry for which that is still true. See
// CONFORMANCE.md section 8.1 for why, and examples/jpeg2000 for one to copy.
func defaultJPEG2000Decoder() Decompressor {
	return nil
}

// defaultJPEGLosslessDecoder returns this package's pure-Go lossless JPEG
// decoder, covering .57 and .70 at every prediction selection value. Substitutable
// for the same reasons as defaultJPEGLSDecoder.
func defaultJPEGLosslessDecoder() Decompressor {
	return NewJPEGLosslessDecompressor()
}

// ExternalCompressionStatus returns information about external compression support
type ExternalCompressionStatus struct {
	CompressionType   CompressionType
	IsAvailable       bool
	RequiredLibrary   string
	InstallationSteps string
}

// GetExternalCompressionStatus returns status of external decoders
func GetExternalCompressionStatus() []ExternalCompressionStatus {
	registry := GetExternalRegistry()
	return []ExternalCompressionStatus{
		{
			CompressionType: JPEG_LS,
			IsAvailable:     registry.IsExternalDecoderAvailable(JPEG_LS),
			RequiredLibrary: "none — bundled, pure Go",
			InstallationSteps: `
JPEG-LS is decoded by this module, in pure Go: .80 and .81, lossless and
near-lossless, 2 to 16 bits, single or multi component, in both line- and
sample-interleaved modes. Nothing to install and nothing to register —
Dataset.PixelArray decodes these frames already.

CharLS (https://github.com/team-charls/charls) is the usual C library for this
codec and is not needed here. If you would rather use it, or any other decoder,
substitute yours:

  type myDecoder struct{}

  func (myDecoder) Decompress(frame []byte) ([]byte, error) { ... }
  func (myDecoder) CanDecompress(frame []byte) bool         { ... }

  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG-LS, myDecoder{})

Dataset.PixelArray then routes frames through yours instead.
`,
		},
		{
			CompressionType: JPEG_2000,
			IsAvailable:     registry.IsExternalDecoderAvailable(JPEG_2000),
			RequiredLibrary: "libopenjp2 (OpenJPEG)",
			InstallationSteps: `
JPEG 2000 is not implemented in this module. Reading pixel data in this syntax
reports that no decoder is registered until you supply one.

Supply a decoder:

  type myDecoder struct{}

  func (myDecoder) Decompress(frame []byte) ([]byte, error) { ... }
  func (myDecoder) CanDecompress(frame []byte) bool         { ... }

  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG 2000, myDecoder{})

Dataset.PixelArray then routes frames through it automatically.

What to wrap is your choice. OpenJPEG is the usual C library for this codec
(https://github.com/uclouvain/openjpeg), which means CGO and its build and distribution consequences; a pure
Go implementation avoids both. This module takes no position and bundles
neither.
`,
		},
		{
			CompressionType: JPEG_LOSSLESS,
			IsAvailable:     registry.IsExternalDecoderAvailable(JPEG_LOSSLESS),
			RequiredLibrary: "none — bundled, pure Go",
			InstallationSteps: `
Lossless JPEG is decoded by this module, in pure Go: .57 and .70, at every
prediction selection value. Nothing to install and nothing to register —
Dataset.PixelArray decodes these frames already.

libjpeg-turbo (https://github.com/libjpeg-turbo/libjpeg-turbo) is the usual C
library for this codec and is not needed here. If you would rather use it, or any
other decoder, substitute yours:

  type myDecoder struct{}

  func (myDecoder) Decompress(frame []byte) ([]byte, error) { ... }
  func (myDecoder) CanDecompress(frame []byte) bool         { ... }

  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG Lossless, myDecoder{})

Dataset.PixelArray then routes frames through yours instead.
`,
		},
	}
}

// PrintExternalCompressionStatus prints status of all external decoders
func PrintExternalCompressionStatus() {
	statuses := GetExternalCompressionStatus()
	for _, status := range statuses {
		fmt.Printf("\n=== %s ===\n", status.CompressionType)
		if status.IsAvailable {
			fmt.Printf("✓ Available\n")
		} else {
			fmt.Printf("✗ Not Available\n")
			fmt.Printf("Required Library: %s\n", status.RequiredLibrary)
			fmt.Printf("Installation: %s\n", status.InstallationSteps)
		}
	}
}

// JPEGLSDecoderSkeleton was a placeholder for a CGO binding to CharLS.
//
// Deprecated: JPEG-LS is decoded by this package in pure Go — see
// JPEGLSDecompressor. This type has no fields and no methods, has never had an
// implementation behind it, and will be removed in the next major version.
type JPEGLSDecoderSkeleton struct{}

// JPEGLSImplementationGuide describes the state of JPEG-LS support.
//
// It once instructed the caller to install libcharls, uncomment a CGO block and
// build with CGO_ENABLED=1. None of that was ever true: there was no CGO
// implementation to enable. The codec is now decoded in pure Go, so there is
// nothing to install either.
const JPEGLSImplementationGuide = `
JPEG-LS
=======

Decoded by this package, in pure Go. Nothing to install, nothing to register.

  Transfer syntaxes: 1.2.840.10008.1.2.4.80 (lossless)
                     1.2.840.10008.1.2.4.81 (near-lossless)
  Bit depths:        2 to 16
  Components:        single or multi
  Interleave modes:  line and sample
  Entry point:       compress.NewJPEGLSDecompressor()

Dataset.PixelArray decodes these frames without being asked.

Verified against pydicom on its own corpus, whole frames rather than leading
values, in TestPixelsAgainstWholePydicomCorpus.

To substitute your own decoder — CharLS through CGO, or anything else:

  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_LS, myDecoder)

Any type with Decompress([]byte) ([]byte, error) and CanDecompress([]byte) bool
will do.
`

/*
// JPEG-2000 Decoder Skeleton (requires OpenJPEG)

// #cgo LDFLAGS: -lopenjp2
// #include <openjpeg.h>
// #include <stdlib.h>
//
// typedef struct {
//     unsigned char* data;
//     size_t length;
//     int width;
//     int height;
//     int num_components;
// } JP2DecoderResult;
//
// JP2DecoderResult decode_jpeg2000(unsigned char* input, size_t input_len) {
//     JP2DecoderResult result = {NULL, 0, 0, 0, 0};
//
//     opj_dparameters_t parameters;
//     opj_set_default_decoder_parameters(&parameters);
//
//     opj_codec_t* codec = opj_create_decompress(OPJ_CODEC_JP2);
//     if (!codec) return result;
//
//     opj_stream_t* stream = opj_stream_create_mem_stream(
//         input, input_len, OPJ_TRUE);
//     if (!stream) {
//         opj_destroy_codec(codec);
//         return result;
//     }
//
//     opj_image_t* image = NULL;
//     opj_read(codec, stream, image);
//
//     // Convert image data to byte array
//     // ... (implementation details)
//
//     return result;
// }
*/

// JPEG2000DecoderSkeleton provides skeleton for JPEG-2000 implementation
// Uncomment cgo directives and implement actual cgo code to enable
type JPEG2000DecoderSkeleton struct {
	// When implementing with cgo:
	// - Import OpenJPEG library
	// - Implement Decompress() using cgo to call OpenJPEG functions
	// - Handle JP2 file format parsing and memory management
}

// JPEG2000ImplementationGuide describes how to decode JPEG 2000 pixel data.
//
// Unlike the JPEG-LS and lossless JPEG guides, this one describes a real gap:
// JPEG 2000 is the only codec in the registry with no bundled decoder.
const JPEG2000ImplementationGuide = `
JPEG 2000
=========

Not decoded by this package. This is the only codec in the registry for which
that is still true.

  Transfer syntaxes: 1.2.840.10008.1.2.4.90 (lossless)
                     1.2.840.10008.1.2.4.91 (lossy)

Instances parse, store and transfer with their pixel data intact as opaque
bytes; only decoding needs a decoder you supply. See CONFORMANCE.md section 8.1
for why none is bundled.

Start from examples/jpeg2000 in this repository. It is a working decoder that
shells out to OpenJPEG's opj_decompress, verified sample-for-sample against
pydicom, and it is under 300 lines — copy it rather than starting from the
C API:

  decoder, err := jpeg2000.NewDecoder() // errors if opj_decompress is not on PATH
  if err != nil {
      return err
  }
  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_2000, decoder)

Any type with Decompress([]byte) ([]byte, error) and CanDecompress([]byte) bool
will do, so a CGO binding to OpenJPEG (https://github.com/uclouvain/openjpeg) or
a pure-Go implementation fits the same interface. Dataset.PixelArray routes
frames through whatever is registered.
`

// JPEGLosslessDecoderSkeleton was a placeholder for a CGO binding to
// libjpeg-turbo.
//
// Deprecated: lossless JPEG is decoded by this package in pure Go — see
// JPEGLosslessDecompressor. This type has no fields and no methods, has never had
// an implementation behind it, and will be removed in the next major version.
type JPEGLosslessDecoderSkeleton struct{}

// JPEGLosslessImplementationGuide describes the state of lossless JPEG support.
//
// It once instructed the caller to install libjpeg-turbo, uncomment a CGO block
// and build with CGO_ENABLED=1. None of that was ever true: there was no CGO
// implementation to enable. The codec is now decoded in pure Go, so there is
// nothing to install either.
const JPEGLosslessImplementationGuide = `
JPEG Lossless
=============

Decoded by this package, in pure Go. Nothing to install, nothing to register.

  Transfer syntaxes: 1.2.840.10008.1.2.4.57 (process 14)
                     1.2.840.10008.1.2.4.70 (process 14, first-order prediction)
  Predictors:        every prediction selection value
  Components:        single or multi
  Entry point:       compress.NewJPEGLosslessDecompressor()

Dataset.PixelArray decodes these frames without being asked.

Verified against pydicom on its own corpus, whole frames rather than leading
values, in TestPixelsAgainstWholePydicomCorpus.

To substitute your own decoder — libjpeg-turbo through CGO, or anything else:

  compress.GetExternalRegistry().RegisterExternalDecoder(compress.JPEG_LOSSLESS, myDecoder)

Any type with Decompress([]byte) ([]byte, error) and CanDecompress([]byte) bool
will do.
`

// GetImplementationGuide returns the implementation guide for a specific decoder
func GetImplementationGuide(compressionType CompressionType) string {
	switch compressionType {
	case JPEG_LS:
		return JPEGLSImplementationGuide
	case JPEG_2000:
		return JPEG2000ImplementationGuide
	case JPEG_LOSSLESS:
		return JPEGLosslessImplementationGuide
	default:
		return "Unknown compression type"
	}
}

// PrintImplementationGuide prints implementation instructions for a decoder
func PrintImplementationGuide(compressionType CompressionType) {
	fmt.Printf("\n%s\n", GetImplementationGuide(compressionType))
}
