package security

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func newCipher(t *testing.T) *TokenCipher {
	t.Helper()
	key := make([]byte, tokenKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	c, err := NewTokenCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewTokenCipher: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newCipher(t)

	cases := []string{"", "ghp_short", strings.Repeat("a", 1024)}
	for _, plaintext := range cases {
		enc, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if plaintext == "" {
			if enc != "" {
				t.Fatalf("expected empty ciphertext for empty plaintext, got %q", enc)
			}
			continue
		}
		if !strings.HasPrefix(enc, tokenCipherPrefix) {
			t.Fatalf("ciphertext missing prefix: %q", enc)
		}
		got, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Fatalf("round-trip mismatch: want %q, got %q", plaintext, got)
		}
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	c := newCipher(t)
	a, err := c.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt a: %v", err)
	}
	b, err := c.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt b: %v", err)
	}
	if a == b {
		t.Fatalf("two encryptions of the same plaintext must differ (random nonce); got %q", a)
	}
}

func TestDecryptPassesThroughPlaintext(t *testing.T) {
	c := newCipher(t)
	got, err := c.Decrypt("plain-token-no-prefix")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "plain-token-no-prefix" {
		t.Fatalf("expected pass-through, got %q", got)
	}
}

func TestDecryptRejectsCorruptCiphertext(t *testing.T) {
	c := newCipher(t)
	if _, err := c.Decrypt(tokenCipherPrefix + "!!!not-base64!!!"); err == nil {
		t.Fatal("expected error decoding bad base64")
	}
	if _, err := c.Decrypt(tokenCipherPrefix + base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for too-short payload")
	}

	enc, _ := c.Encrypt("hello")
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("expected GCM auth failure on tampered ciphertext")
	}
}

func TestNewTokenCipherKeyValidation(t *testing.T) {
	if _, err := NewTokenCipher(""); err == nil {
		t.Fatal("expected error for empty key")
	}
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := NewTokenCipher(short); err == nil {
		t.Fatal("expected error for short key")
	}
}
