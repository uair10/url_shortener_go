// Package problem отдаёт ошибки API в формате RFC 9457 Problem Details.
package problem

import (
	"net/http"
	"strings"
)

// Code - машиночитаемый код ошибки, отдаётся в поле "code".
type Code string

// Коды из справочника ошибок (SPEC, раздел 6). В комментарии - HTTP-статус.
const (
	CodeMalformedJSON        Code = "malformed_json"         // 400
	CodeInvalidQuery         Code = "invalid_query"          // 400
	CodeValidationFailed     Code = "validation_failed"      // 422
	CodeUnauthorized         Code = "unauthorized"           // 401
	CodeForbidden            Code = "forbidden"              // 403
	CodeQuotaExceeded        Code = "quota_exceeded"         // 403
	CodeNotFound             Code = "not_found"              // 404
	CodeMethodNotAllowed     Code = "method_not_allowed"     // 405
	CodeAliasTaken           Code = "alias_taken"            // 409
	CodeEmailTaken           Code = "email_taken"            // 409
	CodeLinkGone             Code = "link_gone"              // 410
	CodePayloadTooLarge      Code = "payload_too_large"      // 413
	CodeUnsupportedMediaType Code = "unsupported_media_type" // 415
	CodeRateLimited          Code = "rate_limited"           // 429
	CodeInternal             Code = "internal"               // 500
)

// typeBaseURI - префикс для поля "type" (RFC 9457: type - URI).
const typeBaseURI = "https://sho.rt/errors/"

type codeInfo struct {
	status int
	title  string
}

// codes - единственный источник истины для HTTP-статуса и заголовка.
// Только для чтения: менять в рантайме нельзя.
var codes = map[Code]codeInfo{ //nolint:gochecknoglobals // read-only lookup table
	CodeMalformedJSON:        {http.StatusBadRequest, "Malformed JSON"},
	CodeInvalidQuery:         {http.StatusBadRequest, "Invalid Query"},
	CodeValidationFailed:     {http.StatusUnprocessableEntity, "Validation Failed"},
	CodeUnauthorized:         {http.StatusUnauthorized, "Unauthorized"},
	CodeForbidden:            {http.StatusForbidden, "Forbidden"},
	CodeQuotaExceeded:        {http.StatusForbidden, "Quota Exceeded"},
	CodeNotFound:             {http.StatusNotFound, "Not Found"},
	CodeMethodNotAllowed:     {http.StatusMethodNotAllowed, "Method Not Allowed"},
	CodeAliasTaken:           {http.StatusConflict, "Alias Taken"},
	CodeEmailTaken:           {http.StatusConflict, "Email Taken"},
	CodeLinkGone:             {http.StatusGone, "Link Gone"},
	CodePayloadTooLarge:      {http.StatusRequestEntityTooLarge, "Payload Too Large"},
	CodeUnsupportedMediaType: {http.StatusUnsupportedMediaType, "Unsupported Media Type"},
	CodeRateLimited:          {http.StatusTooManyRequests, "Rate Limited"},
	CodeInternal:             {http.StatusInternalServerError, "Internal Server Error"},
}

// TypeURI возвращает URI для поля "type": malformed_json -> https://sho.rt/errors/malformed-json.
func (c Code) TypeURI() string {
	return typeBaseURI + strings.ReplaceAll(string(c), "_", "-")
}
