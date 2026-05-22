package utilities

import (
	"log/slog"
	"os"

	"github.com/google/uuid"
)

func InitializeLog() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // Log everything from Debug up to Error
	})

	logger := slog.
		New(handler).
		With("application_id", uuid.New().String())

	slog.SetDefault(logger)
}
