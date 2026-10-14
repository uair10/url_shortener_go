// Package problem отдаёт ошибки API в формате RFC 9457 Problem Details.
package problem

import "strings"

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
var codes = map[Code]codeInfo{
	CodeMalformedJSON:        {400, "Malformed JSON"},
	CodeInvalidQuery:         {400, "Invalid Query"},
	CodeValidationFailed:     {422, "Validation Failed"},
	CodeUnauthorized:         {401, "Unauthorized"},
	CodeForbidden:            {403, "Forbidden"},
	CodeQuotaExceeded:        {403, "Quota Exceeded"},
	CodeNotFound:             {404, "Not Found"},
	CodeMethodNotAllowed:     {405, "Method Not Allowed"},
	CodeAliasTaken:           {409, "Alias Taken"},
	CodeEmailTaken:           {409, "Email Taken"},
	CodeLinkGone:             {410, "Link Gone"},
	CodePayloadTooLarge:      {413, "Payload Too Large"},
	CodeUnsupportedMediaType: {415, "Unsupported Media Type"},
	CodeRateLimited:          {429, "Rate Limited"},
	CodeInternal:             {500, "Internal Server Error"},
}

// TypeURI возвращает URI для поля "type": malformed_json -> https://sho.rt/errors/malformed-json.
func (c Code) TypeURI() string {
	return typeBaseURI + strings.ReplaceAll(string(c), "_", "-")
}
