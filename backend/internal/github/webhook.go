package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// SignaturePrefix is the prefix GitHub puts on the X-Hub-Signature-256 header.
const SignaturePrefix = "sha256="

// VerifySignature checks an X-Hub-Signature-256 value against the raw request
// body using the configured webhook secret. It returns nil on success.
//
// The signature must be of the form "sha256=<hex>". The comparison is constant
// time. If secret is empty, an error is returned (a missing secret is treated
// as misconfiguration rather than implicitly trusting the request).
func VerifySignature(secret string, body []byte, signature string) error {
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("webhook secret is not configured")
	}
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return fmt.Errorf("missing webhook signature")
	}
	if !strings.HasPrefix(signature, SignaturePrefix) {
		return fmt.Errorf("invalid webhook signature prefix")
	}
	gotHex := strings.TrimPrefix(signature, SignaturePrefix)
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return fmt.Errorf("invalid webhook signature hex: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(expected, got) {
		return fmt.Errorf("webhook signature mismatch")
	}
	return nil
}

// Event is a parsed (and signature-verified) GitHub webhook event.
type Event struct {
	// Type is the value of the X-GitHub-Event header (e.g. "pull_request").
	Type string
	// DeliveryID is the value of the X-GitHub-Delivery header.
	DeliveryID string
	// Payload is the raw JSON body decoded into a generic map for callers
	// that don't need a typed event.
	Payload map[string]any
}

// ParseEvent verifies the signature and decodes the body into a generic Event.
// eventType is the X-GitHub-Event header, deliveryID is X-GitHub-Delivery,
// signature is X-Hub-Signature-256.
func ParseEvent(secret string, body []byte, eventType, deliveryID, signature string) (*Event, error) {
	if err := VerifySignature(secret, body, signature); err != nil {
		return nil, err
	}
	if strings.TrimSpace(eventType) == "" {
		return nil, fmt.Errorf("missing event type header")
	}
	var payload map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode webhook payload: %w", err)
		}
	}
	return &Event{
		Type:       eventType,
		DeliveryID: deliveryID,
		Payload:    payload,
	}, nil
}
