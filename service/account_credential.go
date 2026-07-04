package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/tokendancelab/metapi-go/config"
)

const (
	credentialVersion = "v1"
)

// buildCredentialKey derives a 32-byte AES key from config.AccountCredentialSecret.
// Mirrors TS buildKey(): SHA-256(accountCredentialSecret || authToken || fallback).
func buildCredentialKey(cfg *config.Config) []byte {
	secret := strings.TrimSpace(cfg.AccountCredentialSecret)
	if secret == "" {
		secret = strings.TrimSpace(cfg.AuthToken)
	}
	if secret == "" {
		secret = "change-me-admin-token" // fallback matches TS config defaults
	}
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// EncryptAccountPassword encrypts a password with AES-256-GCM.
// Returns "v1:base64url(iv):base64url(tag):base64url(ciphertext)".
// Mirrors TS encryptAccountPassword().
func EncryptAccountPassword(cfg *config.Config, password string) (string, error) {
	key := buildCredentialKey(cfg)
	iv := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher failed: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM failed: %w", err)
	}

	ciphertext := aesgcm.Seal(nil, iv, []byte(password), nil)
	// GCM appends the 16-byte auth tag at the end of ciphertext.
	tagStart := len(ciphertext) - 16
	encrypted := ciphertext[:tagStart]
	tag := ciphertext[tagStart:]

	return credentialVersion + ":" +
		base64URLEncode(iv) + ":" +
		base64URLEncode(tag) + ":" +
		base64URLEncode(encrypted), nil
}

// DecryptAccountPassword decrypts a ciphertext produced by EncryptAccountPassword.
// Returns empty string on any failure (mirrors TS: returns null/error silently).
func DecryptAccountPassword(cfg *config.Config, cipherText string) string {
	parts := strings.SplitN(cipherText, ":", 4)
	if len(parts) != 4 || parts[0] != credentialVersion {
		return ""
	}

	key := buildCredentialKey(cfg)
	iv, err := base64URLDecode(parts[1])
	if err != nil || len(iv) != 12 {
		return ""
	}
	tag, err := base64URLDecode(parts[2])
	if err != nil || len(tag) != 16 {
		return ""
	}
	encrypted, err := base64URLDecode(parts[3])
	if err != nil {
		return ""
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}

	// Recombine ciphertext + tag for GCM
	ciphertext := append(encrypted, tag...)
	plain, err := aesgcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// base64URLEncode encodes bytes to base64url without padding.
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// base64URLDecode decodes base64url without padding.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
