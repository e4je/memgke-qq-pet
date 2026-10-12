package config

import (
	"os"
	"strconv"
	"strings"
)

var buildVersion = "1.0.1"

type Config struct {
	// ManagedConnection is process-local; never persist the framework token.
	ManagedConnection *ManagedConnection `json:"-"`
	Addr              string
	Version           string
	Environment       string
	DatabasePath      string
	FrontendOrigin    string
	CookieName        string
	SessionHours      int
	CookieSecure      bool
	LoginLimit        int
	LoginWindowSec    int
}

type ManagedConnection struct {
	Host  string
	Port  int
	Token string `json:"-"`
}

func Load() Config {
	return Config{
		Addr:           envString("APP_ADDR", ":8080"),
		Version:        envString("APP_VERSION", buildVersion),
		Environment:    envString("APP_ENV", "development"),
		DatabasePath:   envString("DATABASE_PATH", "./data/qq-pet.db"),
		FrontendOrigin: envString("FRONTEND_ORIGIN", "http://localhost:5173"),
		CookieName:     envString("COOKIE_NAME", "qq_pet_session"),
		SessionHours:   envInt("SESSION_HOURS", 168),
		CookieSecure:   envBool("COOKIE_SECURE", false),
		LoginLimit:     envInt("LOGIN_LIMIT", 10),
		LoginWindowSec: envInt("LOGIN_WINDOW_SECONDS", 300),
	}
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}
