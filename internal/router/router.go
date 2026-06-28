// Пакет router собирает маршруты и middleware в единый HTTP-обработчик.
package router

import (
	"net/http"
	"gopherledger/internal/handler"
	"gopherledger/internal/middleware"
)

// New создаёт и возвращает HTTP-обработчик со всеми маршрутами.
//
// Публичные маршруты (без авторизации):
//
//	POST /api/user/register
//	POST /api/user/login
//
// Защищённые маршруты (требуют токен):
//
//	POST /api/user/orders
//	GET  /api/user/orders
//	GET  /api/user/balance
//	POST /api/user/balance/withdraw
//	GET  /api/user/withdrawals
//	POST /api/stats/export
func New(h *handler.Handler, m *middleware.Middleware) http.Handler {
	mux := http.NewServeMux()

	// Регистрируем все обработчики в маршрутизаторе
	// Сначала без авторизации
	mux.HandleFunc("POST /api/user/register", h.Register)
	mux.HandleFunc("POST /api/user/login", h.Login)

	// Которые требуют токен
	mux.Handle("POST /api/user/orders", m.Auth(http.HandlerFunc(h.CreateOrder)))
	mux.Handle("GET /api/user/orders", m.Auth(http.HandlerFunc(h.GetOrders)))
	mux.Handle("GET /api/user/balance", m.Auth(http.HandlerFunc(h.GetBalance)))
    mux.Handle("POST /api/user/balance/withdraw", m.Auth(http.HandlerFunc(h.Withdraw)))
    mux.Handle("GET /api/user/withdrawals", m.Auth(http.HandlerFunc(h.GetWithdrawals)))
    mux.Handle("POST /api/stats/export", m.Auth(http.HandlerFunc(h.ExportStats)))

	return middleware.Logging(middleware.Recover(mux)) // заворачиваем во все функции
}
