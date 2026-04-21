// Package security provides shared cryptographic helpers for the board
// backend (e.g. encrypting per-user GitHub tokens at rest).
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	tokenCipherPrefix = "enc:v1:"
	tokenKeySize      = 32
)

// TokenCipher encrypts/decrypts secrets at rest using AES-GCM. Encrypted
// values are base64-encoded and prefixed with "enc:v1:". Plaintext values
// without the prefix are passed through unchanged for backward
// compatibility / dev mode.
type TokenCipher struct {
	aead cipher.AEAD
}

// NewTokenCipher constructs a cipher from a base64-encoded 32-byte key.
// Generate one with: openssl rand -base64 32.
func NewTokenCipher(base64Key string) (*TokenCipher, error) {
	key, err := decodeKey(base64Key)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher block: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm cipher: %w", err)
	}

	return &TokenCipher{aead: aead}, nil
}

// Encrypt seals plaintext and returns the prefixed base64 ciphertext. An
// empty input yields an empty output so callers can safely round-trip
// "no value stored".
func (t *TokenCipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, t.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	cipherText := t.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, cipherText...)
	return tokenCipherPrefix + base64.StdEncoding.EncodeToString(payload), nil
}

// Decrypt opens a value previously sealed by Encrypt. Values lacking the
// "enc:v1:" prefix are returned unchanged (dev / migration support).
func (t *TokenCipher) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	if !strings.HasPrefix(ciphertext, tokenCipherPrefix) {
		return ciphertext, nil
	}

	encoded := strings.TrimPrefix(ciphertext, tokenCipherPrefix)
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	nonceSize := t.aead.NonceSize()
	if len(payload) < nonceSize {
		return "", fmt.Errorf("invalid ciphertext payload")
	}

	nonce := payload[:nonceSize]
	enc := payload[nonceSize:]
	plain, err := t.aead.Open(nil, nonce, enc, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}

	return string(plain), nil
}

func decodeKey(rawKey string) ([]byte, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY is required")
	}

	decoders := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}

	for _, decode := range decoders {
		key, err := decode(rawKey)
		if err == nil {
			if len(key) != tokenKeySize {
				return nil, fmt.Errorf("decoded encryption key must be %d bytes", tokenKeySize)
			}
			return key, nil
		}
	}

	if len(rawKey) != tokenKeySize {
		return nil, fmt.Errorf("encryption key must be %d raw bytes or base64-encoded", tokenKeySize)
	}

	return []byte(rawKey), nil
}
