// Package middleware содержит HTTP-middleware сервиса.
//
// Порядок подключения задаёт httpapi.withMiddleware. RequestID должен стоять первым:
// остальные берут логгер с request_id через logging.FromCtx.
package middleware
