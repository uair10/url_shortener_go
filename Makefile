.DEFAULT_GOAL := check

.PHONY: run fmt lint lint-fix test cover vuln tidy openapi openapi-lint check hooks pre-commit help

run: ## Запустить код (переменные из .env, если он есть)
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/shortener

fmt: ## Отформатировать код
	golangci-lint fmt ./...

lint: ## Запустить линтеры
	golangci-lint run ./...

lint-fix: ## Линтеры с автоисправлением
	golangci-lint run --fix ./...

test: ## Тесты с race detector (цветной вывод, сводка упавших в конце)
	go tool gotestsum --format pkgname -- -race -count=1 ./...

cover: ## Покрытие тестами (откроет HTML-отчёт)
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

vuln: ## Проверка уязвимостей
	go tool govulncheck ./...

tidy: ## Привести go.mod/go.sum в порядок
	go mod tidy
	go mod verify

openapi: ## Генерация openapi.yaml
	go run ./cmd/openapi -o api/openapi.yaml

openapi-lint: ## Линтер спецификации OpenAPI (нужен Node.js)
	npx -y @redocly/cli@2.65.0 lint api/openapi.yaml

check: tidy fmt lint test vuln ## Всё сразу перед коммитом

hooks: ## Установить pre-commit хуки
	pre-commit install

pre-commit: ## Прогнать все хуки по всем файлам
	pre-commit run --all-files

help:
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'
