package config_test

import (
	"errors"
	"testing"

	"github.com/amrshadid/go-dicom/config"
)

// Mock handler for testing
type mockHandler struct {
	name                    string
	supportedTransferSyntax string
	available               bool
	shouldFail              bool
	needsRGB                bool
}

func (h *mockHandler) Name() string {
	return h.name
}

func (h *mockHandler) SupportsTransferSyntax(uid string) bool {
	return uid == h.supportedTransferSyntax
}

func (h *mockHandler) IsAvailable() bool {
	return h.available
}

func (h *mockHandler) GetPixelData(ds interface{}) ([]byte, error) {
	if h.shouldFail {
		return nil, errors.New("mock handler failure")
	}
	return []byte("mock pixel data"), nil
}

func (h *mockHandler) NeedsRGBConversion(ds interface{}) bool {
	return h.needsRGB
}

// TestRegisterPixelDataHandler tests handler registration.
func TestRegisterPixelDataHandler(t *testing.T) {
	// Clear handlers before test
	config.ClearPixelDataHandlers()

	handler1 := &mockHandler{
		name:                    "Handler1",
		supportedTransferSyntax: "1.2.840.10008.1.2.1",
		available:               true,
	}

	handler2 := &mockHandler{
		name:                    "Handler2",
		supportedTransferSyntax: "1.2.840.10008.1.2.5",
		available:               true,
	}

	// Register handlers
	config.RegisterPixelDataHandler(handler1)
	config.RegisterPixelDataHandler(handler2)

	// Get handlers
	handlers := config.GetPixelDataHandlers()

	if len(handlers) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(handlers))
	}

	if handlers[0].Name() != "Handler1" {
		t.Errorf("Expected first handler to be Handler1, got %s", handlers[0].Name())
	}

	if handlers[1].Name() != "Handler2" {
		t.Errorf("Expected second handler to be Handler2, got %s", handlers[1].Name())
	}
}

// TestGetPixelDataHandlers tests getting handlers.
func TestGetPixelDataHandlers(t *testing.T) {
	// Clear handlers
	config.ClearPixelDataHandlers()

	// Initially should be empty
	handlers := config.GetPixelDataHandlers()
	if len(handlers) != 0 {
		t.Errorf("Expected 0 handlers after clear, got %d", len(handlers))
	}

	// Register a handler
	handler := &mockHandler{
		name:                    "TestHandler",
		supportedTransferSyntax: "1.2.840.10008.1.2.1",
		available:               true,
	}
	config.RegisterPixelDataHandler(handler)

	// Should now have 1 handler
	handlers = config.GetPixelDataHandlers()
	if len(handlers) != 1 {
		t.Errorf("Expected 1 handler, got %d", len(handlers))
	}

	// Modifying returned slice should not affect internal list
	handlers[0] = &mockHandler{name: "ModifiedHandler"}

	handlers2 := config.GetPixelDataHandlers()
	if handlers2[0].Name() != "TestHandler" {
		t.Error("Modifying returned handlers slice should not affect internal list")
	}
}

// TestSetPixelDataHandlers tests setting handlers.
func TestSetPixelDataHandlers(t *testing.T) {
	config.ClearPixelDataHandlers()

	handler1 := &mockHandler{name: "Handler1"}
	handler2 := &mockHandler{name: "Handler2"}
	handler3 := &mockHandler{name: "Handler3"}

	// Set new handler list
	newHandlers := []config.PixelDataHandler{handler1, handler2, handler3}
	config.SetPixelDataHandlers(newHandlers)

	// Verify
	handlers := config.GetPixelDataHandlers()
	if len(handlers) != 3 {
		t.Errorf("Expected 3 handlers, got %d", len(handlers))
	}

	// Modifying original slice should not affect internal list
	newHandlers[0] = &mockHandler{name: "ModifiedHandler"}

	handlers2 := config.GetPixelDataHandlers()
	if handlers2[0].Name() != "Handler1" {
		t.Error("Modifying source slice should not affect internal list")
	}
}

// TestClearPixelDataHandlers tests clearing handlers.
func TestClearPixelDataHandlers(t *testing.T) {
	handler := &mockHandler{name: "TestHandler"}
	config.RegisterPixelDataHandler(handler)

	// Verify handler is registered
	handlers := config.GetPixelDataHandlers()
	if len(handlers) == 0 {
		t.Fatal("Handler should be registered")
	}

	// Clear
	config.ClearPixelDataHandlers()

	// Verify empty
	handlers = config.GetPixelDataHandlers()
	if len(handlers) != 0 {
		t.Errorf("Expected 0 handlers after clear, got %d", len(handlers))
	}
}

// TestFindPixelDataHandler tests finding handlers.
func TestFindPixelDataHandler(t *testing.T) {
	config.ClearPixelDataHandlers()

	// Register multiple handlers
	jpegHandler := &mockHandler{
		name:                    "JPEG Handler",
		supportedTransferSyntax: "1.2.840.10008.1.2.4.50",
		available:               true,
	}

	rleHandler := &mockHandler{
		name:                    "RLE Handler",
		supportedTransferSyntax: "1.2.840.10008.1.2.5",
		available:               true,
	}

	unavailableHandler := &mockHandler{
		name:                    "Unavailable Handler",
		supportedTransferSyntax: "1.2.840.10008.1.2.4.80",
		available:               false,
	}

	config.RegisterPixelDataHandler(jpegHandler)
	config.RegisterPixelDataHandler(rleHandler)
	config.RegisterPixelDataHandler(unavailableHandler)

	// Test finding JPEG handler
	t.Run("FindJPEGHandler", func(t *testing.T) {
		handler := config.FindPixelDataHandler("1.2.840.10008.1.2.4.50")
		if handler == nil {
			t.Fatal("Should find JPEG handler")
		}
		if handler.Name() != "JPEG Handler" {
			t.Errorf("Expected JPEG Handler, got %s", handler.Name())
		}
	})

	// Test finding RLE handler
	t.Run("FindRLEHandler", func(t *testing.T) {
		handler := config.FindPixelDataHandler("1.2.840.10008.1.2.5")
		if handler == nil {
			t.Fatal("Should find RLE handler")
		}
		if handler.Name() != "RLE Handler" {
			t.Errorf("Expected RLE Handler, got %s", handler.Name())
		}
	})

	// Test not finding unavailable handler
	t.Run("DontFindUnavailableHandler", func(t *testing.T) {
		handler := config.FindPixelDataHandler("1.2.840.10008.1.2.4.80")
		if handler != nil {
			t.Error("Should not find unavailable handler")
		}
	})

	// Test not finding unsupported transfer syntax
	t.Run("DontFindUnsupported", func(t *testing.T) {
		handler := config.FindPixelDataHandler("9.9.9.9.9")
		if handler != nil {
			t.Error("Should not find handler for unsupported transfer syntax")
		}
	})
}

// TestHandlerOrdering tests that handlers are tried in registration order.
func TestHandlerOrdering(t *testing.T) {
	config.ClearPixelDataHandlers()

	// Register two handlers for the same transfer syntax
	handler1 := &mockHandler{
		name:                    "Handler1",
		supportedTransferSyntax: "1.2.840.10008.1.2.1",
		available:               true,
	}

	handler2 := &mockHandler{
		name:                    "Handler2",
		supportedTransferSyntax: "1.2.840.10008.1.2.1",
		available:               true,
	}

	config.RegisterPixelDataHandler(handler1)
	config.RegisterPixelDataHandler(handler2)

	// Should find the first one (Handler1)
	handler := config.FindPixelDataHandler("1.2.840.10008.1.2.1")
	if handler == nil {
		t.Fatal("Should find a handler")
	}
	if handler.Name() != "Handler1" {
		t.Errorf("Should find Handler1 (first registered), got %s", handler.Name())
	}
}

// TestHandlerInterface tests the handler interface methods.
func TestHandlerInterface(t *testing.T) {
	handler := &mockHandler{
		name:                    "Test Handler",
		supportedTransferSyntax: "1.2.840.10008.1.2.1",
		available:               true,
		shouldFail:              false,
		needsRGB:                true,
	}

	// Test Name
	if handler.Name() != "Test Handler" {
		t.Errorf("Name() = %s, want Test Handler", handler.Name())
	}

	// Test SupportsTransferSyntax
	if !handler.SupportsTransferSyntax("1.2.840.10008.1.2.1") {
		t.Error("Should support 1.2.840.10008.1.2.1")
	}
	if handler.SupportsTransferSyntax("9.9.9.9.9") {
		t.Error("Should not support 9.9.9.9.9")
	}

	// Test IsAvailable
	if !handler.IsAvailable() {
		t.Error("Should be available")
	}

	// Test GetPixelData
	data, err := handler.GetPixelData(nil)
	if err != nil {
		t.Errorf("GetPixelData() error = %v", err)
	}
	if string(data) != "mock pixel data" {
		t.Errorf("GetPixelData() = %s, want mock pixel data", string(data))
	}

	// Test GetPixelData failure
	handler.shouldFail = true
	_, err = handler.GetPixelData(nil)
	if err == nil {
		t.Error("GetPixelData() should fail when shouldFail is true")
	}

	// Test NeedsRGBConversion
	if !handler.NeedsRGBConversion(nil) {
		t.Error("NeedsRGBConversion() should return true")
	}
}
