package config

import (
	"os"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	JWTSecret          string
	AppURL             string
	CORSAllowedOrigins string
	ResendAPIKey       string
	AuthEmailFrom      string
	MobileAppScheme    string

	LLMAPIKey  string
	LLMModel   string
	LLMBaseURL string
}

func Load() Config {
	return Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/eva_board?sslmode=disable"),
		JWTSecret:          getEnv("JWT_SECRET", "dev-secret-change-me-please-use-a-long-random-value"),
		AppURL:             getEnv("APP_URL", "http://localhost:8080"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:8081,http://localhost:8082,http://localhost:19006"),
		ResendAPIKey:       strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		AuthEmailFrom:      getEnv("AUTH_EMAIL_FROM", "Eva Board <onboarding@example.com>"),
		MobileAppScheme:    getEnv("MOBILE_APP_SCHEME", "eva-board"),

		LLMAPIKey:  strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:   getEnv("LLM_MODEL", "openai/gpt-4o-mini"),
		LLMBaseURL: getEnv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
	}
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
