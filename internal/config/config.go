// Package config loads typed application configuration from the environment.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

const envPrefix = "DISPATCHR"

// WebServerConfig contains HTTP server settings.
type WebServerConfig struct {
	Port string `envconfig:"SERVER_PORT" default:"8080"`
}

// PostgresConfig contains PostgreSQL connection settings.
type PostgresConfig struct {
	Username string `envconfig:"DB_USER"`
	DBName   string `envconfig:"DB_NAME"`
	Password string `envconfig:"DB_PASSWORD"`
	Host     string `envconfig:"DB_HOST"`
	SSLMode  string `envconfig:"DB_SSL_MODE" default:"disable"`
}

// LoggingConfig contains structured logging settings.
type LoggingConfig struct {
	Level string `envconfig:"LOG_LEVEL" default:"info"`
}

// AppConfig contains all application configuration.
type AppConfig struct {
	WebServer *WebServerConfig
	DB        *PostgresConfig
	Logging   *LoggingConfig
}

// FromEnv loads configuration from the environment. If present, the local env
// file at etc/config/config.local.env is loaded first without overriding shell
// environment variables.
func FromEnv() (*AppConfig, error) {
	if err := loadLocalEnv(); err != nil {
		return nil, err
	}

	cfg := &AppConfig{
		WebServer: &WebServerConfig{},
		DB:        &PostgresConfig{},
		Logging:   &LoggingConfig{},
	}
	if err := envconfig.Process(envPrefix, cfg.WebServer); err != nil {
		return nil, err
	}
	if err := envconfig.Process(envPrefix, cfg.DB); err != nil {
		return nil, err
	}
	if err := envconfig.Process(envPrefix, cfg.Logging); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadLocalEnv() error {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("determine configuration file path")
	}

	filename := filepath.Join(filepath.Dir(currentFile), "../../etc/config/config.local.env")
	if err := godotenv.Load(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
