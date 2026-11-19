package config_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/amrshadid/go-dicom/config"
)

// A context carries the logger it was given; one without a logger, or given
// nil, gives the global Logger as it stands when asked, so SetLogger still
// applies to it.
func TestLoggerFromContext(t *testing.T) {
	own := slog.New(slog.DiscardHandler)
	if got := config.LoggerFromContext(config.WithLogger(context.Background(), own)); got != own {
		t.Error("the context's logger was not returned")
	}
	saved := config.Logger
	defer func() { config.Logger = saved }()
	later := slog.New(slog.DiscardHandler)
	ctx := config.WithLogger(context.Background(), nil)
	config.Logger = later
	if got := config.LoggerFromContext(ctx); got != later {
		t.Error("a context without a logger did not give the global one as it is now")
	}
}
