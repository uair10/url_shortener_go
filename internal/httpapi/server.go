// Package httpapi содержит HTTP-слой сервиса.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/uair10/url_shortener_go/internal/httpapi/middleware"
	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
)

// Server - HTTP-сервер сервиса.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// New собирает http.Server с роутером и таймаутами из конфига.
func New(logger *slog.Logger, cfg config.Server) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.Addr(),
			Handler:           newHandler(logger, cfg.MaxBodyBytes),
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		},
		logger: logger,
	}
}

// newHandler собирает роутер и оборачивает его в middleware.
// Отдельно от New, чтобы роутер можно было тестировать без http.Server.
func newHandler(logger *slog.Logger, maxBodyBytes int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return withMiddleware(mux, logger, maxBodyBytes)
}

// withMiddleware оборачивает роутер в middleware. Тесты передают сюда свой mux.
func withMiddleware(mux *http.ServeMux, logger *slog.Logger, maxBodyBytes int64) http.Handler {
	// middleware оборачивают mux изнутри наружу
	handler := notFoundAsProblem(mux)
	// MaxBytesHandler только ограничивает r.Body. 413 отдаёт хендлер,
	// когда при чтении тела получает *http.MaxBytesError.
	handler = http.MaxBytesHandler(handler, maxBodyBytes)
	// Recover внутри AccessLog: после паники access log пишется со статусом 500.
	handler = middleware.Recover(handler)
	handler = middleware.AccessLog(handler)
	handler = middleware.RequestID(logger)(handler) // самый первый
	return handler
}

// Listen занимает адрес из конфига и обслуживает запросы до остановки.
// ctx ограничивает только захват порта, а не работу сервера.
func (s *Server) Listen(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return s.Serve(ln)
}

// Serve обслуживает запросы на готовом listener и блокируется до остановки.
// После Shutdown возвращает nil. Тесты передают сюда свой listener на :0.
func (s *Server) Serve(ln net.Listener) error {
	s.logger.Info("server started", slog.String("addr", ln.Addr().String()))
	if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// Shutdown перестаёт принимать соединения и ждёт текущие запросы, пока не истечёт ctx.
// Если не дождались, разрывает оставшиеся соединения принудительно.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", errors.Join(err, s.httpServer.Close()))
	}
	return nil
}
