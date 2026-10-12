package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	defaults := Config{
		Server: Server{
			Port:              8080,
			ShutdownTimeout:   10 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxBodyBytes:      1 << 20,
		},
		Log: Log{Level: slog.LevelInfo},
	}

	overridden := defaults
	overridden.Server.Port = 9090
	overridden.Server.ShutdownTimeout = 3 * time.Second
	overridden.Server.MaxBodyBytes = 512
	overridden.Log.Level = slog.LevelDebug

	tests := []struct {
		name     string
		env      map[string]string
		want     Config
		wantErrs []string // все подстроки должны быть в одной ошибке
	}{
		{
			name: "значения по умолчанию",
			env:  map[string]string{},
			want: defaults,
		},
		{
			name: "переопределение через env",
			env: map[string]string{
				"SERVER_PORT":             "9090",
				"SERVER_SHUTDOWN_TIMEOUT": "3s",
				"SERVER_MAX_BODY_BYTES":   "512",
				"LOG_LEVEL":               "debug",
			},
			want: overridden,
		},
		{
			name:     "порт 0",
			env:      map[string]string{"SERVER_PORT": "0"},
			wantErrs: []string{"SERVER_PORT must be in range"},
		},
		{
			name:     "порт 70000",
			env:      map[string]string{"SERVER_PORT": "70000"},
			wantErrs: []string{"SERVER_PORT must be in range"},
		},
		{
			name: "невалидный LOG_LEVEL",
			env:  map[string]string{"LOG_LEVEL": "verbose"},
			// env называет в ошибке разбора поле Go, а не переменную окружения.
			wantErrs: []string{`field "Level"`},
		},
		{
			name: "несколько ошибок валидации сразу",
			env: map[string]string{
				"SERVER_PORT":           "0",
				"SERVER_READ_TIMEOUT":   "-1s",
				"SERVER_MAX_BODY_BYTES": "0",
			},
			wantErrs: []string{"SERVER_PORT", "SERVER_READ_TIMEOUT", "SERVER_MAX_BODY_BYTES"},
		},
		{
			name: "несколько ошибок разбора сразу",
			env: map[string]string{
				"SERVER_PORT": "abc",
				"LOG_LEVEL":   "verbose",
			},
			wantErrs: []string{`field "Port"`, `field "Level"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := load(env.Options{Environment: tt.env})

			if len(tt.wantErrs) > 0 {
				assertErrContains(t, err, tt.wantErrs)
				return
			}

			if err != nil {
				t.Fatalf("load() error = %v", err)
			}
			if *got != tt.want {
				t.Errorf("load() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

// assertErrContains проверяет, что одна ошибка содержит все подстроки.
func assertErrContains(t *testing.T, err error, wants []string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want errors %q", wants)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
