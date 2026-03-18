# Config

Global configuration for DICOM operations: validation modes (IGNORE/WARN/RAISE), structured logging via `log/slog`, context-based overrides, and a pixel data handler registry.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/config"

// Global settings
settings := config.Get()
settings.SetReadingValidationMode(config.RAISE)
settings.SetWritingValidationMode(config.WARN)
settings.SetDatetimeConversion(true)

// Context-based overrides
ctx := config.StrictReading(context.Background())
ctx = config.DisableValueValidation(ctx)
mode := config.ReadingValidationModeFromContext(ctx)

// Logging
config.SetDebug(true, nil)
config.Logger.Debug("Reading element", "tag", tag)

// Pixel data handlers
config.RegisterPixelDataHandler(&MyJPEGHandler{})
handler := config.FindPixelDataHandler("1.2.840.10008.1.2.4.50")
```

## API Reference

```go
type ValidationMode int // IGNORE, WARN, RAISE
type BehaviorMode string // "IGNORE", "WARN", "RAISE"

func Get() *Settings
func Reset()

// Context functions
func DisableValueValidation(ctx context.Context) context.Context
func StrictReading(ctx context.Context) context.Context
func StrictValidation(ctx context.Context) context.Context
func WithReadingValidationMode(ctx context.Context, mode ValidationMode) context.Context
func ReadingValidationModeFromContext(ctx context.Context) ValidationMode

// Logging
func SetDebug(enabled bool, output io.Writer)
func IsDebugging() bool
func SetLogger(logger *slog.Logger)

// Handler registry
type PixelDataHandler interface {
    Name() string
    SupportsTransferSyntax(uid string) bool
    IsAvailable() bool
    GetPixelData(ds interface{}) ([]byte, error)
    NeedsRGBConversion(ds interface{}) bool
}
func RegisterPixelDataHandler(handler PixelDataHandler)
func FindPixelDataHandler(transferSyntaxUID string) PixelDataHandler
func GetPixelDataHandlers() []PixelDataHandler
```
