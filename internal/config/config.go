package config

import (
	"fmt"
	"net/url"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr  string
	DatabaseURL string
}

func Load() (*Config, error) {
	godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = buildDatabaseURL()
	}
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL or DB_* variables are required")
	}

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	return &Config{
		ListenAddr:  addr,
		DatabaseURL: dbURL,
	}, nil
}

func buildDatabaseURL() string {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	name := os.Getenv("DB_NAME")
	sslmode := os.Getenv("DB_SSLMODE")

	if host == "" || user == "" || name == "" {
		return ""
	}
	if port == "" {
		port = "5432"
	}
	if sslmode == "" {
		sslmode = "disable"
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(user), url.QueryEscape(password), host, port, name, sslmode)
}
