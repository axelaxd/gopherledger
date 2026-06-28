// Пакет handler содержит HTTP-обработчики.
//
// Взаимодействие с бизнес-логикой осуществляется через интерфейс.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gopherledger/internal/domain"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Service interface {
	RegisterUser(login, password string) (string, error)
	LoginUser(login, password string) (string, error)
	CreateOrder(userID int64, number string) (*domain.Order, error)
	GetUserOrders(userID int64) ([]domain.Order, error)
	GetBalance(userID int64) (domain.Balance, error)
	Withdraw(userID int64, orderNumber string, sum float64) error
	GetWithdrawals(userID int64) ([]domain.Withdrawal, error)
	ExportStats() (domain.StatsData, error)
}

// Handler хранит зависимость от бизнес-логики.
type Handler struct {
	svc Service
}

// New создаёт Handler.
func New(svc Service) *Handler {
	return &Handler{svc: svc}
}

// ---------------------------------------------------------------------------
// Вспомогательные функции для ответов
// ---------------------------------------------------------------------------

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError записывает JSON-ответ с ошибкой.
// Клиент видит только userMsg. Внутренние детали пишутся только в лог.
func WriteError(w http.ResponseWriter, status int, code, userMsg string, internalErr error) {
	if internalErr != nil {
		log.Printf("ошибка code=%s status=%d: %v", code, status, internalErr)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// TODO: создайте структуру ответа и сериализуйте её

	response := ErrorResponse{
		Code:    code,
		Message: userMsg,
	}

	json.NewEncoder(w).Encode(response)

}

// WriteJSON записывает успешный JSON-ответ.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("WriteJSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Обработчики
// ---------------------------------------------------------------------------

type Req struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// Register обрабатывает POST /api/user/register.
// При успехе: 200 OK, заголовок Authorization с токеном.
// При дублировании логина: 409 Conflict.
// При некорректных данных: 400 Bad Request.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	req := Req{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "неверный формат запроса", err)
		return
	}

	token, err := h.svc.RegisterUser(req.Login, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrUserExists):
			WriteError(w, http.StatusConflict, "USER_EXISTS", "пользователь уже существует", err)
		default:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		}
		return
	}

	w.Header().Set("Authorization", token)
	WriteJSON(w, http.StatusOK, nil)
}

// Login обрабатывает POST /api/user/login.
// При успехе: 200 OK, заголовок Authorization с токеном.
// При неверных данных: 401 Unauthorized.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	req := Req{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "неверный формат запроса", err)
		return
	}

	token, err := h.svc.LoginUser(req.Login, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPassword):
			WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "неверный пользователь или пароль", err)
		default:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		}
		return
	}

	w.Header().Set("Authorization", token)
	WriteJSON(w, http.StatusOK, nil)
}

// CreateOrder обрабатывает POST /api/user/orders.
// Тело запроса: номер заказа в виде обычного текста.
// 202 Accepted  - новый заказ принят в обработку.
// 200 OK        - заказ уже загружен этим пользователем.
// 409 Conflict  - заказ принадлежит другому пользователю.
// 422 Unprocessable Entity - номер не прошёл проверку Луна.
func (h *Handler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	// так как тело запроса в виде обычного текста, то читаем через io
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "неверный формат запроса", err)
		return
	}
	number := strings.TrimSpace(string(body))

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "NO_USER", "не авторизован", nil)
		return
	}

	order, err := h.svc.CreateOrder(userID, number)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidOrder):
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_NUMBER", "номер не прошёл проверку Луна", err)
		case errors.Is(err, domain.ErrOrderOwnedByUser):
			WriteJSON(w, http.StatusOK, order) // 200 уже загружен этим пользователем
		case errors.Is(err, domain.ErrOrderExists):
			WriteError(w, http.StatusConflict, "ORDER_EXISTS", "заказ принадлежит другому пользователю", err)
		default:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		}
		return
	}

	// 202 новый заказ
	WriteJSON(w, http.StatusAccepted, order)
}

// GetOrders обрабатывает GET /api/user/orders.
// 200 OK с JSON-массивом заказов или 204 No Content если заказов нет.
func (h *Handler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "NO_USER", "не авторизован", nil)
		return
	}

	orders, err := h.svc.GetUserOrders(userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	WriteJSON(w, http.StatusOK, orders)
}

// GetBalance обрабатывает GET /api/user/balance.
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "NO_USER", "не авторизован", nil)
		return
	}

	balance, err := h.svc.GetBalance(userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		return
	}

	WriteJSON(w, http.StatusOK, balance)
}

type WithdrawStruct struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

// Withdraw обрабатывает POST /api/user/balance/withdraw.
// 200 OK при успехе.
// 402 Payment Required при нехватке баллов.
// 422 Unprocessable Entity при неверном номере заказа.
func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "NO_USER", "не авторизован", nil)
		return
	}

	req := WithdrawStruct{}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "неверный формат запроса", err)
		return
	}

	orderNumber := strings.TrimSpace(req.Order)
	sum := req.Sum

	err = h.svc.Withdraw(userID, orderNumber, sum)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInsufficientFunds):
			WriteError(w, 402, "INSUFFICIENT_FUNDS", "недостаточно средства", err)
		case errors.Is(err, domain.ErrInvalidOrder):
			WriteError(w, 422, "INVALID_NUMBER", "неверный номер заказа", err)
		default:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	WriteJSON(w, 200, nil)
}

// GetWithdrawals обрабатывает GET /api/user/withdrawals.
// 200 OK с массивом или 204 No Content если списаний нет.
func (h *Handler) GetWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "NO_USER", "не авторизован", nil)
		return
	}

	spisok, err := h.svc.GetWithdrawals(userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка", err)
		return
	}

	if len(spisok) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	WriteJSON(w, http.StatusOK, spisok)
}

// ExportStats обрабатывает POST /api/stats/export.
// Собирает статистику системы и записывает её в текстовый файл stats.txt
// в корне проекта. Возвращает 200 OK при успехе.
func (h *Handler) ExportStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.ExportStats()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "STATS_ERROR", "не удалось собрать статистику", err)
		return
	}

	// создаем что мы будем записывать в файл
	content := fmt.Sprintf(
		`Время генерации отчёта: %s
	Общее число зарегистрированных пользователей: %d
    Общее число заказов: %d
	Распределение заказов по статусам: %v
    Суммарное количество начисленных баллов: %.2f
    Суммарное количество списанных баллов: %.2f`,
		time.Now(),
		stats.TotalUsers,
		stats.TotalOrders,
		stats.OrdersByStatus,
		stats.TotalAccrued,
		stats.TotalWithdrawn,
	)

	err = os.WriteFile("stats.txt", []byte(content), 0644)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "WRITE_ERROR", "не удалось записать файл экспорта", err)
		return
	}

	WriteJSON(w, http.StatusOK, nil)
}

// ---------------------------------------------------------------------------
// Вспомогательная функция для работы с контекстом
// ---------------------------------------------------------------------------

type contextKey string

const CtxKeyUserID contextKey = "userID"

// UserIDFromContext извлекает ID аутентифицированного пользователя из контекста.
// Возвращает 0, false если значение отсутствует.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(CtxKeyUserID).(int64)
	if !ok || userID <= 0 {
		return 0, false
	}
	return userID, true
}
