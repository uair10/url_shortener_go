package httpapi

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/uair10/url_shortener_go/internal/infrastructure/config"
)

// specAPI собирает API только ради спецификации. Конфиг фиксированный: спека в
// репозитории не должна зависеть от окружения. Им пользуются cmd/openapi и тесты.
func specAPI() huma.API {
	api := newAPI(http.NewServeMux(), config.Server{})
	registerRoutes(api)
	return api
}

// OpenAPIYAML возвращает спецификацию OpenAPI в YAML (для make openapi).
func OpenAPIYAML() ([]byte, error) {
	b, err := specAPI().OpenAPI().YAML()
	if err != nil {
		return nil, fmt.Errorf("openapi yaml: %w", err)
	}
	return b, nil
}
