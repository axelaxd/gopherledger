# Gopherledger

[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Бэкенд система лояльности для интернет-магазина. Позволяет регистрировать пользователей, загружать номера заказов и начислять бонусные баллы.

## 📚 Оглавление

- [Архитектура](#архитектура)
- [Технологии](#технологии)
- [API](#api)
- [Установка и запуск](#установка-и-запуск)
- [Тестирование](#тестирование)
- [Лицензия](#лицензия)

## 🏗 Архитектура

Проект построен по принципам чистой архитектуры с четким разделением слоев:

cmd/server/ # Точка входа
internal/
├── store/ # In-memory хранилище
├── service/ # Бизнес-логика
├── handler/ # HTTP-обработчики
├── auth/ # Аутентификация
├── config/ # Конфигурация
├── middleware/ # HTTP-middleware
└── router/ # Маршрутизация


### Ключевые особенности:

- **Конкурентно-безопасное хранилище** с `sync.RWMutex`
- **Атомарные операции** списания баланса
- **Фоновый воркер** с ограничением параллелизма (`errgroup`)
- **Graceful shutdown** с таймаутом
- **Алгоритм Луна** для валидации номеров заказов

## 🛠 Технологии

- **Go 1.23** — основной язык
- **Стандартная библиотека** — минимальное использование внешних зависимостей
- **golang.org/x/sync/errgroup** — управление горутинами
- **YAML** — конфигурация

## 📡 API

### Публичные маршруты

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/user/register` | Регистрация |
| POST | `/api/user/login` | Вход |

### Защищенные маршруты (требуют Bearer токен)

| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/user/orders` | Загрузить заказ |
| GET | `/api/user/orders` | Список заказов |
| GET | `/api/user/balance` | Баланс |
| POST | `/api/user/balance/withdraw` | Списать баллы |
| GET | `/api/user/withdrawals` | История списаний |
| POST | `/api/stats/export` | Экспорт статистики |

### Примеры запросов

```bash
# Регистрация
curl -X POST http://localhost:8080/api/user/register \
  -H "Content-Type: application/json" \
  -d '{"login":"alice","password":"secret"}'

# Загрузка заказа
curl -X POST http://localhost:8080/api/user/orders \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: text/plain" \
  -d "79927398713"
```

# 🚀 Установка и запуск

## Клонировать репозиторий
git clone https://github.com/yourusername/gopherledger.git
cd gopherledger

## Установить зависимости
go mod download

## Запустить сервер
go run ./cmd/server/

## Или собрать бинарный файл
go build -o gopherledger ./cmd/server/
./gopherledger

# Конфигурация
Файл config.yaml (опционально):

```yaml
server_host: "localhost"
server_port: 8080
log_level: "info"
accrual_interval_seconds: 3
worker_concurrency: 5
```
