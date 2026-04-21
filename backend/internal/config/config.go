package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
	AppURL              string
	CORSAllowedOrigins  string
	ResendAPIKey        string
	AuthEmailFrom       string
	MobileAppScheme     string
	LLMAPIKey           string
	LLMModel            string
	LLMBaseURL          string
	GitHubAPIBaseURL    string
	GitHubWebhookSecret string
	TokenEncryptionKey  string

	// Codegen agent configuration. See backend/internal/codegen for the
	// pluggable agent interface that consumes these.
	CodegenAgent          string
	CodegenModel          string
	CodegenTimeout        time.Duration
	CodegenMaxOutputBytes int
	CodegenCommand        string
	CodegenArgs           []string
}

func Load() Config {
	return Config{
		Port:                  getEnv("PORT", "8080"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5433/eva_board?sslmode=disable"),
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-me-please-use-a-long-random-value"),
		AppURL:                getEnv("APP_URL", "http://localhost:8080"),
		CORSAllowedOrigins:    getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:8081,http://localhost:8082,http://localhost:19006"),
		ResendAPIKey:          strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		AuthEmailFrom:         getEnv("AUTH_EMAIL_FROM", "Eva Board <onboarding@example.com>"),
		MobileAppScheme:       getEnv("MOBILE_APP_SCHEME", "eva-board"),
		LLMAPIKey:             strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:              getEnv("LLM_MODEL", "openai/gpt-4o-mini"),
		LLMBaseURL:            getEnv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
		GitHubAPIBaseURL:      getEnv("GITHUB_API_BASE_URL", "https://api.github.com"),
		GitHubWebhookSecret:   strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET")),
		TokenEncryptionKey:    strings.TrimSpace(os.Getenv("TOKEN_ENCRYPTION_KEY")),
		CodegenAgent:          getEnv("CODEGEN_AGENT", "claude-code"),
		CodegenModel:          strings.TrimSpace(os.Getenv("CODEGEN_MODEL")),
		CodegenTimeout:        getDurationEnv("CODEGEN_TIMEOUT", 30*time.Minute),
		CodegenMaxOutputBytes: getIntEnv("CODEGEN_MAX_OUTPUT_BYTES", 10*1024*1024),
		CodegenCommand:        strings.TrimSpace(os.Getenv("CODEGEN_COMMAND")),
		CodegenArgs:           splitCSV(os.Getenv("CODEGEN_ARGS")),
	}
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func getIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// splitCSV parses a comma-separated value into a trimmed string slice.
// Empty fields are dropped so "a,,b" yields ["a","b"]. An empty input yields nil.
func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
