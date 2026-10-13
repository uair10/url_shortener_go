// Package logging содержит настройки логирования приложения.
package logging

import (
	"log/slog"
	"os"

	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
)

// NewLogger Конфигурирует логгер на основе настроек.
func NewLogger(cfg config.Log) *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.Level,
	}))
	return logger
}
