// Пакет store реализует хранилище данных в памяти.
// Используйте отдельные мьютексы для независимых групп данных.
// Реализуйте этот пакет самостоятельно.
package store

import (
	"gopherledger/internal/domain"
	"sort"
	"sync"
	"time"
)

// Store хранит все данные приложения в памяти.
// Добавьте средства защиты конкурентного доступа самостоятельно.
type Store struct {
	mu sync.RWMutex // предлагаю сделать RWMutex, чтобы потом защищать данные с Rlock() и Lock()
	// users хранит пользователей по их ID
	users map[int64]*domain.User

	// usersByLogin хранит пользователей по логину - для быстрого поиска при авторизации
	usersByLogin map[string]*domain.User

	// orders хранит заказы по номеру заказа
	orders map[string]*domain.Order

	// balances хранит текущий баланс каждого пользователя по его ID
	balances map[int64]*domain.Balance

	// withdrawals хранит историю списаний для каждого пользователя по его ID
	withdrawals map[int64][]*domain.Withdrawal

	// nextID используется для генерации уникальных числовых ID
	nextID int64
}

// New создаёт и возвращает новое пустое хранилище.
func New() *Store {
	return &Store{
		mu:           sync.RWMutex{},
		users:        make(map[int64]*domain.User),
		usersByLogin: make(map[string]*domain.User),
		orders:       make(map[string]*domain.Order),
		balances:     make(map[int64]*domain.Balance),
		withdrawals:  make(map[int64][]*domain.Withdrawal),
		nextID:       1, // первый айдишник будет под номером 1
	}
}

// CreateUser добавляет нового пользователя.
// Возвращает domain.ErrUserExists если логин уже занят.
func (s *Store) CreateUser(login, passwordHash string) (*domain.User, error) {

	s.mu.Lock() // здесь читаем данные -> нельзя чтобы их переписали
	defer s.mu.Unlock()

	_, ok := s.usersByLogin[login]
	if ok { // если пользователей найден -> логин уже занят, возвращаем ошибку
		return nil, domain.ErrUserExists
	}

	user := &domain.User{
		ID:           s.nextID,
		Login:        login,
		PasswordHash: passwordHash,
	}

	s.usersByLogin[login] = user
	s.users[s.nextID] = user
	s.nextID += 1 // не забываю увеличивать счётчик

	return user, nil
}

// GetUserByLogin возвращает пользователя по логину.
// Возвращает domain.ErrUserNotFound если пользователь не найден.
func (s *Store) GetUserByLogin(login string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.usersByLogin[login]
	if ok {
		return user, nil
	}

	return nil, domain.ErrUserNotFound
}

// CreateOrder добавляет новый заказ для пользователя.
// Возвращает domain.ErrOrderOwnedByUser если этот пользователь уже загружал этот номер.
// Возвращает domain.ErrOrderExists если номер принадлежит другому пользователю.
func (s *Store) CreateOrder(userID int64, number string) (*domain.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if value, ok := s.orders[number]; ok {
		if value.UserID == userID {
			return nil, domain.ErrOrderOwnedByUser
		}
		return nil, domain.ErrOrderExists
	}

	order := &domain.Order{
		ID:         s.nextID,
		UserID:     userID,
		Number:     number,
		Status:     domain.OrderStatusNew,
		Accrual:    0,
		UploadedAt: time.Now(),
	}

	s.orders[number] = order
	s.nextID++

	return order, nil

	// - **NEW** - заказ только что загружен, ожидает обработки
	// - **PROCESSING** - воркер взял заказ в работу
	// - **PROCESSED** - заказ успешно обработан, баллы начислены
	// - **INVALID** - заказ не прошёл проверку (примерно 10% заказов)
}

// GetUserOrders возвращает все заказы пользователя, сначала новые.
func (s *Store) GetUserOrders(userID int64) ([]domain.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var orders []domain.Order
	for _, value := range s.orders {
		if value.UserID == userID {
			orders = append(orders, *value)
		}
	}

	sort.Slice(orders, func(i, j int) bool {
		return orders[i].UploadedAt.After(orders[j].UploadedAt)
		// если i "новее" чем j, то возвращаем i
	})

	return orders, nil
}

// GetOrdersForProcessing возвращает все заказы в статусе NEW или PROCESSING.
func (s *Store) GetOrdersForProcessing() ([]domain.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var orders []domain.Order
	for _, value := range s.orders {
		if value.Status == domain.OrderStatusNew || value.Status == domain.OrderStatusProcessing {
			orders = append(orders, *value)
		}
	}

	return orders, nil
}

// UpdateOrderStatus обновляет статус и начисление заказа.
// Если статус PROCESSED и accrual > 0, баланс пользователя пополняется.
func (s *Store) UpdateOrderStatus(number, status string, accrual float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[number]
	if !ok {
		return domain.ErrInvalidOrder
	}

	// Обновляем статус и деньги
	order.Status = status
	order.Accrual = accrual

	if accrual > 0 && status == domain.OrderStatusProcessed {
		if _, ok := s.balances[order.UserID]; !ok { // Так как нам не передают ID, мы берем его из orders
			s.balances[order.UserID] = &domain.Balance{Current: 0, Withdrawn: 0}
		}
		s.balances[order.UserID].Current += accrual
	}

	return nil
}

// GetBalance возвращает баланс пользователя.
func (s *Store) GetBalance(userID int64) (domain.Balance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	balance, ok := s.balances[userID]
	if !ok {
		return domain.Balance{Current: 0, Withdrawn: 0}, nil
	}

	return *balance, nil
}

// Withdraw списывает сумму с баланса и записывает операцию.
// Возвращает domain.ErrInsufficientFunds если баланса не хватает.
// Обе операции должны быть атомарны: либо обе успешны, либо ни одна.
func (s *Store) Withdraw(userID int64, orderNumber string, sum float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	balance, ok := s.balances[userID]
	if !ok {
		return domain.ErrInvalidOrder
	}

	if balance.Current < sum {
		return domain.ErrInsufficientFunds
	}

	balance.Current -= sum
	balance.Withdrawn += sum
	withdraw := &domain.Withdrawal{
		ID:          s.nextID,
		UserID:      userID,
		OrderNumber: orderNumber,
		Sum:         sum,
		ProcessedAt: time.Now(),
	}

	s.nextID++
	s.withdrawals[userID] = append(s.withdrawals[userID], withdraw)

	return nil
}

// GetWithdrawals возвращает историю списаний пользователя, сначала новые.
func (s *Store) GetWithdrawals(userID int64) ([]domain.Withdrawal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var withdrawals []domain.Withdrawal
	for _, value := range s.withdrawals[userID] {
		withdrawals = append(withdrawals, *value)
	}

	sort.Slice(withdrawals, func(i, j int) bool {
		return withdrawals[i].ProcessedAt.After(withdrawals[j].ProcessedAt)
		// если i "новее" чем j, то возвращаем i
	})

	return withdrawals, nil
}

// Функция GetStatus() (StatsData struct, error)
// Собирает полную статистику и подготавливает ее
// для экспорта в текстовый файл
func (s *Store) GetStatus() (domain.StatsData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := domain.StatsData{
		TotalUsers:     int64(len(s.users)),
		TotalOrders:    int64(len(s.orders)),
		OrdersByStatus: make(map[string]int64),
	}

	// теперь посчитаем сколько начислено баллов всего

	for _, order := range s.orders {
		stats.OrdersByStatus[order.Status]++             // для каждого статуса мы делаем счётчик
		if order.Status == domain.OrderStatusProcessed { // если уже баллы начислены, то добавляем
			stats.TotalAccrued += order.Accrual
		}
	}

	for _, balance := range s.balances {
		stats.TotalWithdrawn += balance.Withdrawn
	}

	return stats, nil
}
