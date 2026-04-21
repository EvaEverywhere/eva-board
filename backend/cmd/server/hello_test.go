package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestHelloEndpoint(t *testing.T) {
	app := fiber.New()
	registerPublicRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected Content-Type to contain application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["message"] != "hello world" {
		t.Fatalf("expected message 'hello world', got %q", body["message"])
	}
}
