// Точка входа сервера. Реализуйте самостоятельно.
//
// Порядок инициализации:
//  1. Загрузить конфигурацию (пакет config)
//  2. Создать хранилище (пакет store)
//  3. Создать сервис (пакет service)
//  4. Запустить воркер начислений в горутине (svc.StartAccrualWorker)
//  5. Создать обработчик и роутер (пакеты handler, router)
//  6. Запустить HTTP-сервер
//  7. Реализовать graceful shutdown по сигналам SIGINT и SIGTERM
package main

import (
	"context"
	"gopherledger/internal/auth"
	"gopherledger/internal/config"
	"gopherledger/internal/handler"
	"gopherledger/internal/middleware"
	"gopherledger/internal/router"
	"gopherledger/internal/service"
	"gopherledger/internal/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	// 1.Загрузить конфигурацию (пакет config)
	config, err := config.Load()
	if err != nil {
		panic("failed to load config")
	}

	// 2.Создать хранилище (пакет store)
	st := store.New()

	// 2.1. Создаем auth (пакет auth)
	a := auth.New()

	// 3. Создать сервис (пакет service)
	svc := service.New(st, a)

	//  4.1 Создаем контекст
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // для graceful

	// 4.2 Запускаем воркера начислений в горутине (svc.StartAccrualWorker)
	go svc.StartAccrualWorker(ctx)

	// 5. Создаем обработчик и роутер (handler, router)
	hand := handler.New(svc)
	midl := middleware.New(a)
	rout := router.New(hand, midl)

	// 6. Запускаем HTTP-сервер
	serviceChan := make(chan os.Signal, 1)
	signal.Notify(serviceChan, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(config.Server_port), // :8080 по умолчанию
		Handler: rout,                                   // все наши api/user/
	}

	go func() {
		log.Printf("сервер запущен на порту %d", config.Server_port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ошибка запуска сервера: %v", err)
		}
	}()

	// Ожидаем сигнал завершения
	<-serviceChan
	log.Println("Получен сигнал завершения, останавливаем сервер...")

	cancel() // остановка

	// 7. Реализовать Graceful shutdown
	// ставим timeout 5 секунд чтобы сервер успел завершить все операции
	ctxShut, ctxCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctxCancel()

	if err := server.Shutdown(ctxShut); err != nil {
		log.Printf("ошибка при остановке сервера: %v", err)
	}

	log.Println("сервер остановлен")
}
