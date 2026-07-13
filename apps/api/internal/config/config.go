package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

type Config struct {
	AppEnv   string
	HTTPPort string
	Database DatabaseConfig
}

type DatabaseConfig struct {
	Host        string
	Port        string
	User        string
	Password    string
	Name        string
	SSLMode     string
	SSLRootCert string
}

func Load() (Config, error) {
	cfg := Config{
		AppEnv:   getEnv("APP_ENV", "development"),
		HTTPPort: getEnv("HTTP_PORT", "8080"),
		Database: DatabaseConfig{
			Host:        getEnv("DATABASE_HOST", "localhost"),
			Port:        getEnv("DATABASE_PORT", "5432"),
			User:        getEnv("DATABASE_USER", ""),
			Password:    getEnv("DATABASE_PASSWORD", ""),
			Name:        getEnv("DATABASE_NAME", ""),
			SSLMode:     getEnv("DATABASE_SSL_MODE", "disable"),
			SSLRootCert: getEnv("DATABASE_SSL_ROOT_CERT", ""),
		},
	}

	if cfg.Database.User == "" {
		return Config{}, fmt.Errorf("DATABASE_USER is required")
	}

	if cfg.Database.Password == "" {
		return Config{}, fmt.Errorf("DATABASE_PASSWORD is required")
	}

	if cfg.Database.Name == "" {
		return Config{}, fmt.Errorf("DATABASE_NAME is required")
	}

	if (cfg.Database.SSLMode == "verify-ca" || cfg.Database.SSLMode == "verify-full") && cfg.Database.SSLRootCert == "" {
		return Config{}, fmt.Errorf("DATABASE_SSL_ROOT_CERT is required when DATABASE_SSL_MODE is %s", cfg.Database.SSLMode)
	}

	return cfg, nil
}

func (c DatabaseConfig) URL() string {
	query := url.Values{}
	query.Set("sslmode", c.SSLMode)
	if c.SSLRootCert != "" {
		query.Set("sslrootcert", c.SSLRootCert)
	}

	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, c.Port),
		Path:     c.Name,
		RawQuery: query.Encode(),
	}).String()
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
