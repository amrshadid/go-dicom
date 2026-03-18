package config

import (
	"sync"
)

// PixelDataHandler is an interface for pixel data decompression handlers.
//
// Handlers are tried in order until one successfully processes the pixel data.
// Each handler must implement all methods to participate in the handler chain.
//
// Example handler:
//
//	type JPEGHandler struct{}
//
//	func (h *JPEGHandler) Name() string {
//	    return "JPEG Baseline Handler"
//	}
//
//	func (h *JPEGHandler) SupportsTransferSyntax(uid string) bool {
//	    return uid == "1.2.840.10008.1.2.4.50" // JPEG Baseline
//	}
//
//	func (h *JPEGHandler) IsAvailable() bool {
//	    return true // Always available (uses stdlib)
//	}
//
//	func (h *JPEGHandler) GetPixelData(ds interface{}) ([]byte, error) {
//	    // Decompress and return pixel data
//	    return decompressedData, nil
//	}
//
//	func (h *JPEGHandler) NeedsRGBConversion(ds interface{}) bool {
//	    return false
//	}
type PixelDataHandler interface {
	// Name returns a human-readable name for this handler.
	// Example: "JPEG Baseline Handler", "RLE Handler", "JPEG-LS Handler"
	Name() string

	// SupportsTransferSyntax returns true if this handler can process
	// pixel data encoded with the given transfer syntax UID.
	//
	// Common Transfer Syntax UIDs:
	//   - "1.2.840.10008.1.2.1": Explicit VR Little Endian (uncompressed)
	//   - "1.2.840.10008.1.2.5": RLE Lossless
	//   - "1.2.840.10008.1.2.4.50": JPEG Baseline (Process 1)
	//   - "1.2.840.10008.1.2.4.80": JPEG-LS Lossless
	//   - "1.2.840.10008.1.2.4.90": JPEG 2000 Lossless
	SupportsTransferSyntax(transferSyntaxUID string) bool

	// IsAvailable returns true if this handler's dependencies are installed
	// and the handler is ready to process pixel data.
	//
	// For handlers that require external libraries (e.g., JPEG-LS, JPEG 2000),
	// this method should check if those libraries are available.
	IsAvailable() bool

	// GetPixelData extracts and decompresses pixel data from a dataset.
	// The parameter is interface{} to avoid circular dependencies with the
	// dataset package. Handlers should type-assert to *dataset.Dataset.
	//
	// Returns:
	//   - A correctly sized 1D byte slice containing the decompressed pixel data
	//   - An error if decompression fails or the handler cannot process this data
	//
	// Note: Reshaping to the correct dimensions is handled by the caller
	// using the dataset's Rows, Columns, and Frames information.
	GetPixelData(ds interface{}) ([]byte, error)

	// NeedsRGBConversion returns true if the pixel data needs to be converted
	// to the RGB colorspace after decompression.
	//
	// Some compression formats (e.g., JPEG with YCbCr color space) may require
	// color space conversion to RGB for proper display.
	NeedsRGBConversion(ds interface{}) bool
}

var (
	// pixelDataHandlers is the ordered list of handlers to try
	pixelDataHandlers []PixelDataHandler

	// handlersMu protects the handler list for thread-safe access
	handlersMu sync.RWMutex
)

// RegisterPixelDataHandler registers a new pixel data handler.
// Handlers are tried in the order they are registered.
//
// This is typically called during package initialization by handler implementations:
//
//	func init() {
//	    config.RegisterPixelDataHandler(&JPEGHandler{})
//	}
//
// Parameters:
//   - handler: the handler to register
func RegisterPixelDataHandler(handler PixelDataHandler) {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	pixelDataHandlers = append(pixelDataHandlers, handler)
}

// GetPixelDataHandlers returns a copy of the registered pixel data handlers
// in the order they will be tried.
//
// Example:
//
//	handlers := config.GetPixelDataHandlers()
//	for _, h := range handlers {
//	    if h.SupportsTransferSyntax(transferSyntaxUID) {
//	        fmt.Printf("Handler %s supports this transfer syntax\n", h.Name())
//	    }
//	}
func GetPixelDataHandlers() []PixelDataHandler {
	handlersMu.RLock()
	defer handlersMu.RUnlock()
	handlers := make([]PixelDataHandler, len(pixelDataHandlers))
	copy(handlers, pixelDataHandlers)
	return handlers
}

// SetPixelDataHandlers replaces the entire handler list with a new list.
// This is useful for customizing the handler order or removing handlers.
//
// Example:
//
//	// Use only specific handlers in a custom order
//	config.SetPixelDataHandlers([]config.PixelDataHandler{
//	    &MyJPEGHandler{},
//	    &MyRLEHandler{},
//	})
//
// Parameters:
//   - handlers: the new list of handlers (can be empty to clear all handlers)
func SetPixelDataHandlers(handlers []PixelDataHandler) {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	// Copy to avoid external modifications to the slice
	pixelDataHandlers = make([]PixelDataHandler, len(handlers))
	copy(pixelDataHandlers, handlers)
}

// ClearPixelDataHandlers removes all registered handlers.
// This is primarily useful for testing.
func ClearPixelDataHandlers() {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	pixelDataHandlers = nil
}

// FindPixelDataHandler finds the first available handler that supports
// the given transfer syntax UID.
//
// Returns:
//   - The first matching handler, or nil if no handler is found
//
// Example:
//
//	handler := config.FindPixelDataHandler("1.2.840.10008.1.2.4.50")
//	if handler != nil {
//	    fmt.Printf("Found handler: %s\n", handler.Name())
//	}
func FindPixelDataHandler(transferSyntaxUID string) PixelDataHandler {
	handlersMu.RLock()
	defer handlersMu.RUnlock()

	for _, handler := range pixelDataHandlers {
		if handler.SupportsTransferSyntax(transferSyntaxUID) && handler.IsAvailable() {
			return handler
		}
	}
	return nil
}
