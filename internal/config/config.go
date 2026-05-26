// Пакет config загружает конфигурацию приложения из YAML-файла.
// Реализуйте этот пакет самостоятельно.
package config

import (
	"errors"
	"gopkg.in/yaml.v3"
	"os"
)

// Config содержит параметры запуска сервера.
// Изучите config.yaml и добавьте поля самостоятельно.
type Config struct {
	Server_host              string `yaml:"server_host"`
	Server_port              int    `yaml:"server_port"`
	Log_level                string `yaml:"log_level"`
	Accrual_interval_seconds int    `yaml:"accrual_interval_seconds"`
	Worker_concurrency       int    `yaml:"worker_concurrency"`
}

// Load читает конфигурацию из файла config.yaml.
// Если файл не найден или поле не задано, применяются значения по умолчанию.
func Load() (*Config, error) {
	config := &Config{
		Server_host:              "localhost",
		Server_port:              8080,
		Log_level:                "info",
		Accrual_interval_seconds: 3,
		Worker_concurrency:       5,
	}

	data, err := os.ReadFile("config.yaml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config, nil
		}
		return nil, err
	}

	err = yaml.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}
