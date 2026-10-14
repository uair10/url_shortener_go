package middleware

import "net/http"

// responseRecorder запоминает статус ответа и то, что ответ уже начат:
// http.ResponseWriter этого не отдаёт.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader пропускает только первый финальный статус: клиент получает заголовки один раз,
// и статус в access log должен совпадать с тем, что он получил. 1xx не финальные и идут как есть.
func (r *responseRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	if code >= http.StatusOK {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	// Ошибку writer отдаём как есть: вызывающий код может сравнивать её напрямую.
	return r.ResponseWriter.Write(p) //nolint:wrapcheck // обёртка writer, а не граница слоя.
}

// Unwrap нужен http.ResponseController, чтобы добраться до исходного writer (Flush, дедлайны).
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
