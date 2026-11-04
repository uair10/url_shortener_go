// Package httpapi содержит HTTP-слой сервиса.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/uair10/url_shortener_go/internal/httpapi/middleware"
	"github.com/uair10/url_shortener_go/internal/httpapi/problem"
	"github.com/uair10/url_shortener_go/internal/httpapi/requestid"
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
			Handler:           newHandler(logger, cfg),
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
func newHandler(logger *slog.Logger, cfg config.Server) http.Handler {
	mux := http.NewServeMux()

	// Huma регистрирует свои маршруты прямо в mux (включая /openapi.*, /docs).
	api := newAPI(mux, cfg)
	registerRoutes(api)

	// Оборачиваем mux в middleware - они работают и для Huma, и для ручных роутов.
	return withMiddleware(mux, logger, cfg.MaxBodyBytes)
}

// publicSecurity - операция без авторизации: пустое требование отменяет глобальное.
//
//nolint:gochecknoglobals // неизменяемое значение для Operation.Security
var publicSecurity = []map[string][]string{{}}

// registerRoutes регистрирует эндпоинты в Huma.
func registerRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "getHealthz",
		Method:        http.MethodGet,
		Path:          "/healthz",
		Summary:       "Healthcheck",
		Tags:          []string{"system"},
		DefaultStatus: http.StatusOK,
		Security:      publicSecurity,
	}, healthzHumaHandler)
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

// newAPI создаёт huma.API поверх готового mux.
func newAPI(mux *http.ServeMux, cfg config.Server) huma.API {
	installHumaErrors() // sync.Once — безопасно при параллельных тестах

	humaConfig := huma.DefaultConfig("URL Shortener", "1.0.0")
	humaConfig.Info.Description = "Сервис сокращения ссылок"
	serverURL := "/"
	if cfg.PublicBaseURL != "" {
		serverURL = cfg.PublicBaseURL
	}
	humaConfig.Servers = []*huma.Server{{URL: serverURL}}

	// Документация: пути и хуки
	if !cfg.DocsEnabled {
		humaConfig.OpenAPIPath = ""
		humaConfig.DocsPath = ""
	}
	humaConfig.SchemasPath = ""  // не отдаём отдельный /schemas
	humaConfig.CreateHooks = nil // убираем $schema из ответов (Huma ставит его по умолчанию)

	// SecuritySchemes: X-API-Key
	humaConfig.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"ApiKeyAuth": {
			Type: "apiKey",
			In:   "header",
			Name: "X-API-Key",
		},
	}
	// По умолчанию операции требуют ключ. Публичные переопределяют это через publicSecurity.
	humaConfig.Security = []map[string][]string{{"ApiKeyAuth": {}}}

	// Transformers: подставляем request_id в Problem, который вернул хендлер
	// Huma пишет ошибку мимо NewErrorWithContext (она уже готова), поэтому
	// request_id, установленный хендлером/сервисом, теряется. Здесь восстанавливаем.
	humaConfig.Transformers = []huma.Transformer{
		func(ctx huma.Context, _ string, v any) (any, error) {
			p, ok := v.(*problem.Problem)
			if !ok {
				return v, nil
			}
			out := *p
			if out.RequestID == "" {
				out.RequestID = requestid.From(ctx.Context())
			}
			if out.Instance == "" {
				out.Instance = ctx.URL().Path
			}
			return &out, nil
		},
	}

	return humago.New(mux, humaConfig)
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
