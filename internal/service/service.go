// Пакет service содержит бизнес-логику приложения.
//
// Взаимодействие с хранилищем осуществляется через интерфейс.
// Определите этот интерфейс здесь, по месту использования.
package service

import (
	"context"
	"math/rand"
	"time"
	"log"
	"gopherledger/internal/domain"
	"gopherledger/internal/auth"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"golang.org/x/sync/errgroup"
	"sync"
)

// Service реализует бизнес-логику приложения.
// Замените поле repo в структуре на свой интерфейс.
//
// processingOrders хранит номера заказов, которые сейчас обрабатываются воркером.
// Защитите конкурентный доступ к этому полю самостоятельно.
type Repository interface{
	CreateUser(login, passwordHash string) (*domain.User, error)
    GetUserByLogin(login string) (*domain.User, error)
    CreateOrder(userID int64, number string) (*domain.Order, error)
    GetUserOrders(userID int64) ([]domain.Order, error)
    GetOrdersForProcessing() ([]domain.Order, error)
    UpdateOrderStatus(number, status string, accrual float64) error
    GetBalance(userID int64) (domain.Balance, error)
    Withdraw(userID int64, orderNumber string, sum float64) error
    GetWithdrawals(userID int64) ([]domain.Withdrawal, error)
}

type Service struct {
	repo            	Repository // замените на свой интерфейс в структуре
	auth 				*auth.Auth 	// Добавляем еще Auth, чтобы генерировать токены
	mu 					sync.RWMutex
	processingOrders 	map[string]bool
}

// New создаёт Service.
func New(repo Repository, a *auth.Auth) *Service {
	return &Service{
		repo:             	repo,
		auth:			  	a,
		mu:					sync.RWMutex{},
		processingOrders: 	make(map[string]bool),
	}
}

// ---------------------------------------------------------------------------
// Методы бизнес-логики - реализуйте самостоятельно
// ---------------------------------------------------------------------------

func HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

func CheckHash(password, passwordHash string) bool {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:]) == passwordHash // если одинаковые -> true
}

// RegisterUser регистрирует нового пользователя и возвращает токен аутентификации.
// Хешируйте пароль перед сохранением с помощью crypto/sha256.
func (s *Service) RegisterUser(login, password string) (string, error) {
	passwordHash := HashPassword(password)
	
	user, err := s.repo.CreateUser(login, passwordHash)
	if err != nil {
		return "", err
	}

	token, err := s.auth.GenerateToken(user.ID)
	if err != nil {
		return "", err
	}

	return token, nil
}

// LoginUser проверяет учётные данные и возвращает токен аутентификации.
func (s *Service) LoginUser(login, password string) (string, error) {
	user, err := s.repo.GetUserByLogin(login)
	if err != nil {
		return "", domain.ErrInvalidPassword
	}

	if !CheckHash(password, user.PasswordHash) {
		return "", domain.ErrInvalidPassword
	}

	// При логине у нас также создается токен
	token, err := s.auth.GenerateToken(user.ID)
	if err != nil {
		return "", err
	}
	
	return token, nil
}

func Luhn (number string) (bool, error) {
	_, err := strconv.Atoi(number)
	if err != nil {
		return false, err
	}

	var length int = len(number)
	var sum int

	for i := 1; i <= length; i++ {
		cifra, _ := strconv.Atoi(string(number[length-i])) // берем справа налево
		if i % 2 == 1  { // нечетная позиция
			cifra = cifra * 2
			if cifra > 9 {
				cifra = cifra - 9
			}
		}

		sum += cifra // четные позиции мы тоже добавляем, только без изменений
	}

	return (sum % 10 == 0), nil
}

// CreateOrder проверяет номер заказа по алгоритму Луна и сохраняет заказ.
func (s *Service) CreateOrder(userID int64, number string) (*domain.Order, error) {
	isValid, err := Luhn(number)
	if err != nil {
		return nil, err
	}

	if !isValid {
		return nil, domain.ErrInvalidOrder
	}

	order, err := s.repo.CreateOrder(userID, number)
	if err != nil {
		return nil, err
	}

	return order, nil
}

// GetUserOrders возвращает все заказы пользователя.
func (s *Service) GetUserOrders(userID int64) ([]domain.Order, error) {
	return s.repo.GetUserOrders(userID)
}

// GetBalance возвращает текущий баланс пользователя.
func (s *Service) GetBalance(userID int64) (domain.Balance, error) {
	return s.repo.GetBalance(userID)
}

// Withdraw проверяет номер заказа по алгоритму Луна и списывает сумму с баланса.
func (s *Service) Withdraw(userID int64, orderNumber string, sum float64) error {
	isValid, err := Luhn(orderNumber)
	if err != nil {
		return err
	}

	if !isValid {
		return domain.ErrInvalidOrder
	}

	return s.repo.Withdraw(userID, orderNumber, sum)
}

// GetWithdrawals возвращает историю списаний пользователя.
func (s *Service) GetWithdrawals(userID int64) ([]domain.Withdrawal, error) {
	return s.repo.GetWithdrawals(userID)
}

// validateLuhn проверяет контрольную сумму номера заказа по алгоритму Луна.
// Вызывается при загрузке заказа и при списании баллов.
func validateLuhn(number string) bool {
	isValid, _ := Luhn(number)
	return isValid
}

// ---------------------------------------------------------------------------
// Воркер начислений
//
// StartAccrualWorker предоставлен. Реализуйте processAllPendingOrders
// и processOrder самостоятельно.
//
// Это самая интересная часть проекта: конкурентная обработка заказов.
// Подумайте, как защитить доступ к processingOrders из нескольких горутин.
// ---------------------------------------------------------------------------

// StartAccrualWorker запускает фоновый цикл, который каждые 3 секунды
// передаёт необработанные заказы в processAllPendingOrders.
// Останавливается при отмене ctx.
func (s *Service) StartAccrualWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processAllPendingOrders(ctx)
		}
	}
}

// processAllPendingOrders получает заказы для обработки и запускает горутины.
// Реализуйте самостоятельно.
func (s *Service) processAllPendingOrders(ctx context.Context) {
	// Сначала получаем заказы из хранилища

	orders, err := s.repo.GetOrdersForProcessing()
	if err != nil {
		log.Fatalf("Worker: не удалось получить заказы: %v", err)
		return
	}
	// TODO: замените interface{} на свой интерфейс и раскомментируйте

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	// TODO: итерируйтесь по заказам, пропускайте те что уже в обработке,
	// для остальных запускайте s.processOrder через g.Go

	for _, order := range orders {
		currentOrder := order
		
		// Перед запуском надо проверить, а не находится ли заказ уже в обработке
		s.mu.Lock()
		if _, ok := s.processingOrders[currentOrder.Number]; ok {
			s.mu.Unlock()
			continue // если в обработке, то скип
		}

		s.processingOrders[currentOrder.Number] = true // ставим что обрабатываем
		s.mu.Unlock()

		// Если нам его нужно обработать, то мы запускаем через g.Go функцию
		g.Go(func() error {
			defer func() { // по окончанию мы должны будем удалить заказ из processing
				s.mu.Lock()
				delete (s.processingOrders, currentOrder.Number)
				s.mu.Unlock()
			}()

			err := s.processOrder(gctx, currentOrder.Number) // обрабатываем
			if err != nil {
				return err
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Printf("воркер: ошибка группы: %v", err)
	}
}



// processOrder обрабатывает один заказ. Реализуйте самостоятельно.
// Используйте вспомогательные функции ниже для генерации случайных значений.
func (s *Service) processOrder(ctx context.Context, number string) error {
	// сначала меняем статус заказа на processing
	err := s.repo.UpdateOrderStatus(number, "PROCESSING", 0)
	if err != nil {
		return err
	}

	// дальше "выполняем" работу
	select {
	case <- time.After(randomDelay()): // pass
	case <- ctx.Done(): // если вдруг что произошло с сервисом
		return ctx.Err()
	}

	if isInvalid() {
		return s.repo.UpdateOrderStatus(number, "INVALID", 0)
	}

	accrual := randomAccrual()
	return s.repo.UpdateOrderStatus(number, "PROCESSED", accrual)
}

// ---------------------------------------------------------------------------
// Вспомогательные функции - предоставлены
// ---------------------------------------------------------------------------

// randomAccrual возвращает случайное начисление от 10 до 500 баллов.
func randomAccrual() float64 {
	return float64(rand.Intn(491) + 10)
}

// randomDelay возвращает случайную задержку от 2 до 6 секунд.
func randomDelay() time.Duration {
	return time.Duration(rand.Intn(5)+2) * time.Second
}

// isInvalid возвращает true примерно в 10% случаев.
func isInvalid() bool {
	return rand.Intn(10) == 0
}