// Пакет auth отвечает за генерацию и проверку токенов аутентификации.
// Токен - это случайная уникальная строка (например, UUID или hex-строка),
// которая однозначно связана с конкретным пользователем.
//
// Внутри пакета нужно хранить соответствие токен -> userID.
// Используйте для этого map с защитой от конкурентного доступа.
// Реализуйте этот пакет самостоятельно.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
)

type Auth struct {
	mu     sync.RWMutex
	tokens map[string]int64 // token -> userID
}

func New() *Auth {
	return &Auth{
		mu:     sync.RWMutex{},
		tokens: make(map[string]int64),
	}
}

// ErrInvalidToken возвращается, если токен не найден или недействителен.
var ErrInvalidToken = errors.New("недействительный токен")

// GenerateToken создаёт новый токен для пользователя с указанным ID
// и сохраняет связь токен -> userID внутри пакета.
func (a *Auth) GenerateToken(userID int64) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	a.tokens[token] = userID
	return token, nil
}

// ValidateToken проверяет токен и возвращает ID пользователя.
// Возвращает ErrInvalidToken если токен не найден.
func (a *Auth) ValidateToken(token string) (int64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	userID, ok := a.tokens[token]
	if !ok {
		return 0, ErrInvalidToken
	}

	return userID, nil
}
