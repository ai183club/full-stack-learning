package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var databaseSchemaPattern = regexp.MustCompile(`^(public|pr_[1-9][0-9]{0,8})$`)
var previewOriginPattern = regexp.MustCompile(`^https://pr-[1-9][0-9]{0,8}\.preview\.seebyte\.xyz$`)

type Config struct {
	AppEnv             string
	HTTPPort           string
	Database           DatabaseConfig
	CORSAllowedOrigins []string
	BioJobInternalKey  string
}

type DatabaseConfig struct {
	Host        string
	Port        string
	User        string
	Password    string
	Name        string
	SSLMode     string
	SSLRootCert string
	Schema      string
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
			Schema:      getEnv("DATABASE_SCHEMA", "public"),
		},
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "")),
		BioJobInternalKey:  getEnv("BIO_JOB_INTERNAL_KEY", ""),
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
	if !databaseSchemaPattern.MatchString(cfg.Database.Schema) {
		return Config{}, fmt.Errorf("DATABASE_SCHEMA must be public or pr_<positive-number>")
	}
	for _, origin := range cfg.CORSAllowedOrigins {
		if !previewOriginPattern.MatchString(origin) {
			return Config{}, fmt.Errorf("CORS_ALLOWED_ORIGINS only supports https://pr-<positive-number>.preview.seebyte.xyz")
		}
	}

	return cfg, nil
}

func (c DatabaseConfig) URL() string {
	query := url.Values{}
	query.Set("sslmode", c.SSLMode)
	if c.SSLRootCert != "" {
		query.Set("sslrootcert", c.SSLRootCert)
	}
	query.Set("search_path", c.Schema)

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

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
