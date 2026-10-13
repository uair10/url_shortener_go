// Command shortener запускает HTTP-сервис коротких ссылок.
package main

import (
	"log/slog"
	"os"

	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
	"github.com/uair10/url_shortener_go/internal/infrastructure/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cfg.Log)
	slog.SetDefault(logger)
}
