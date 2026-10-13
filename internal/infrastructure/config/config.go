// Package config содержит конфиг приложения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/caarlos0/env/v11"
)

type (
	// Config - конфигурация приложения. Читается из переменных окружения.
	Config struct {
		Server Server `envPrefix:"SERVER_"`
		Log    Log
	}

	// Server - настройки HTTP-сервера.
	Server struct {
		Port              int           `env:"PORT" envDefault:"8080"`
		ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
		ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
		ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
		WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" envDefault:"10s"`
		IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" envDefault:"60s"`
		MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"1048576"` // 1 MiB
	}

	// Log - настройки логирования.
	Log struct {
		Level slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	}
)

// Addr возвращает адрес для http.Server: сервер слушает все интерфейсы.
func (s Server) Addr() string { return ":" + strconv.Itoa(s.Port) }

// Load читает конфиг из переменных окружения.
// Возвращает сразу все ошибки, а не только первую.
func Load() (*Config, error) {
	return load(env.Options{})
}

// load позволяет тестам передать переменные через opts.Environment, не трогая os.Environ.
func load(opts env.Options) (*Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c Config) validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(c.Server.Port > 0 && c.Server.Port <= 65535,
		"SERVER_PORT must be in range 1..65535, got %d", c.Server.Port)

	durations := []struct {
		name  string
		value time.Duration
	}{
		{"SERVER_SHUTDOWN_TIMEOUT", c.Server.ShutdownTimeout},
		{"SERVER_READ_HEADER_TIMEOUT", c.Server.ReadHeaderTimeout},
		{"SERVER_READ_TIMEOUT", c.Server.ReadTimeout},
		{"SERVER_WRITE_TIMEOUT", c.Server.WriteTimeout},
		{"SERVER_IDLE_TIMEOUT", c.Server.IdleTimeout},
	}
	for _, d := range durations {
		check(d.value > 0, "%s must be positive, got %s", d.name, d.value)
	}

	check(c.Server.MaxBodyBytes > 0, "SERVER_MAX_BODY_BYTES must be positive, got %d", c.Server.MaxBodyBytes)

	return errors.Join(errs...)
}
