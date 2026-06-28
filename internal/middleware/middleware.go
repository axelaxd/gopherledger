// Пакет middleware содержит HTTP-middleware.
package middleware

import (
	"context"
	"gopherledger/internal/auth"
	"gopherledger/internal/handler"
	"log"
	"net/http"
	"strings"
	"time"
)

type Middleware struct {
	auth *auth.Auth
}

func New(a *auth.Auth) *Middleware {
	return &Middleware{
		auth: a,
	}
}

// Auth проверяет токен из заголовка Authorization и помещает ID пользователя в контекст.
// Запросы без валидного токена получают ответ 401 Unauthorized.
func (m *Middleware) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authToken := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authToken, "Bearer ")
		token = strings.TrimSpace(token)

		if token == "" {
			handler.WriteError(w, http.StatusUnauthorized, "NO_TOKEN", "требуется токен авторизации", nil)
			return
		}

		userID, err := m.auth.ValidateToken(token)
		if err != nil {
			handler.WriteError(w, http.StatusUnauthorized, "INVALID_TOKEN", "недействительный токен", err)
			return
		}

		// по ключу "userID" складываем что получили от ValidateToken
		ctx := context.WithValue(r.Context(), handler.CtxKeyUserID, userID)

		// передаем управление следующему handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder оборачивает http.ResponseWriter для перехвата статус-кода.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// Нам нужно создать WriteHeader, чтобы у нас сохранялся статус кода
// иначе будет использоваться встроенный метод, который не сораняет статус в наше поле
// оригинальный writer сохраняет статус внутри себя
func (rec *statusRecorder) WriteHeader(statusCode int) {
	rec.status = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

// Logging логирует метод, путь, статус ответа и время выполнения каждого запроса.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		duration := time.Since(start) // считаем время выполнения
		log.Printf("method=%s path=%s status=%d duration=%v",
			r.Method, r.URL.Path, recorder.status, duration)

	})
}

// Recover перехватывает панику внутри handler, логирует её и возвращает
// клиенту ответ 500 Internal Server Error вместо того, чтобы уронить сервер.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v", rec)
				handler.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ошибка сервера", nil)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
