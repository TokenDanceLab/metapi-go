package service

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

// Helper: make a minimal config with the given credential secret.
func testCfg(secret string) *config.Config {
	cfg := &config.Config{
		AccountCredentialSecret: secret,
	}
	// Ensure fallback chain works as in Load()
	if cfg.AccountCredentialSecret == "" && cfg.AuthToken != "" {
		cfg.AccountCredentialSecret = cfg.AuthToken
	}
	return cfg
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	cfg := testCfg("my-test-secret-key")
	password := "super-secret-password-123"

	cipherText := EncryptAccountPassword(cfg, password)
	if cipherText == "" {
		t.Fatal("encrypt returned empty string")
	}

	plain := DecryptAccountPassword(cfg, cipherText)
	if plain != password {
		t.Errorf("roundtrip failed: expected %q, got %q", password, plain)
	}
}

func TestEncryptDecryptMultiplePasswords(t *testing.T) {
	cfg := testCfg("multi-test-key")
	passwords := []string{
		"simple",
		"with spaces and symbols !@#$%^&*()",
		"unicode 你好世界 emoji 🚀",
		"",
		"a",
		strings.Repeat("x", 1000),
	}

	for _, pw := range passwords {
		t.Run("pw_"+truncateForName(pw, 20), func(t *testing.T) {
			cipherText := EncryptAccountPassword(cfg, pw)
			plain := DecryptAccountPassword(cfg, cipherText)
			if plain != pw {
				t.Errorf("roundtrip failed: expected %q, got %q", pw, plain)
			}
		})
	}
}

func TestEncryptProducesV1Format(t *testing.T) {
	cfg := testCfg("format-test-key")
	cipherText := EncryptAccountPassword(cfg, "test-password")

	parts := strings.SplitN(cipherText, ":", 4)
	if len(parts) != 4 {
		t.Fatalf("expected 4 colon-separated parts, got %d: %q", len(parts), cipherText)
	}
	if parts[0] != "v1" {
		t.Errorf("expected version 'v1', got %q", parts[0])
	}
	// Each part should be non-empty base64url
	for i, part := range parts {
		if part == "" {
			t.Errorf("part %d is empty", i)
		}
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	cfg := testCfg("nd-test-key")
	password := "same-password"

	c1 := EncryptAccountPassword(cfg, password)
	c2 := EncryptAccountPassword(cfg, password)

	if c1 == c2 {
		t.Error("expected non-deterministic encryption (different IV each time)")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	cfg1 := testCfg("key-one")
	cfg2 := testCfg("key-two")

	cipherText := EncryptAccountPassword(cfg1, "my-password")
	plain := DecryptAccountPassword(cfg2, cipherText)

	if plain != "" {
		t.Errorf("decrypt with wrong key should return empty, got %q", plain)
	}
}

func TestDecryptWithTamperedCiphertext(t *testing.T) {
	cfg := testCfg("tamper-key")

	cipherText := EncryptAccountPassword(cfg, "original-password")

	// Tamper with the ciphertext part
	parts := strings.SplitN(cipherText, ":", 4)
	if len(parts) == 4 {
		// Flip the last character of the ciphertext
		tampered := parts[0] + ":" + parts[1] + ":" + parts[2] + ":" + parts[3] + "x"
		plain := DecryptAccountPassword(cfg, tampered)
		if plain != "" {
			t.Errorf("tampered ciphertext should decrypt to empty, got %q", plain)
		}
	}
}

func TestDecryptInvalidFormat(t *testing.T) {
	cfg := testCfg("invalid-key")

	tests := []string{
		"",
		"not-valid",
		"v1:only:two",
		"v2:iv:tag:cipher",
		":iv:tag:cipher",
	}

	for _, ct := range tests {
		t.Run("fmt_"+truncateForName(ct, 20), func(t *testing.T) {
			plain := DecryptAccountPassword(cfg, ct)
			if plain != "" {
				t.Errorf("expected empty for invalid format %q, got %q", ct, plain)
			}
		})
	}
}

func TestDecryptWrongIVLength(t *testing.T) {
	cfg := testCfg("iv-key")

	// Normal encrypt to get valid parts
	cipherText := EncryptAccountPassword(cfg, "test")
	parts := strings.SplitN(cipherText, ":", 4)

	// Replace IV with invalid-length one
	badIV := "YWJj" // decodes to 3 bytes, not 12
	modified := parts[0] + ":" + badIV + ":" + parts[2] + ":" + parts[3]
	plain := DecryptAccountPassword(cfg, modified)
	if plain != "" {
		t.Errorf("expected empty for wrong IV length, got %q", plain)
	}
}

func TestBuildCredentialKeyFromAuthToken(t *testing.T) {
	cfg := &config.Config{
		AccountCredentialSecret: "",
		AuthToken:               "my-auth-token",
	}

	key := buildCredentialKey(cfg)
	if len(key) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(key))
	}

	expected := sha256.Sum256([]byte("my-auth-token"))
	for i, b := range expected {
		if key[i] != b {
			t.Errorf("key mismatch at byte %d", i)
			break
		}
	}
}

func TestBuildCredentialKeyFallback(t *testing.T) {
	cfg := &config.Config{
		AccountCredentialSecret: "",
		AuthToken:               "",
	}

	key := buildCredentialKey(cfg)
	if len(key) != 32 {
		t.Errorf("expected 32-byte key from fallback, got %d", len(key))
	}

	expected := sha256.Sum256([]byte("change-me-admin-token"))
	for i, b := range expected {
		if key[i] != b {
			t.Errorf("key mismatch at byte %d", i)
			break
		}
	}
}

func TestEncryptPassword_Delegates(t *testing.T) {
	cfg := testCfg("delegate-key")
	cipherText := EncryptPassword(cfg, "delegate-pw")

	if !strings.HasPrefix(cipherText, "v1:") {
		t.Errorf("expected v1 prefix, got %q", cipherText)
	}

	plain := DecryptPassword(cfg, cipherText)
	if plain != "delegate-pw" {
		t.Errorf("roundtrip failed via delegates: expected 'delegate-pw', got %q", plain)
	}
}

func TestDecryptPassword_EmptyInput(t *testing.T) {
	cfg := testCfg("empty-key")
	plain := DecryptPassword(cfg, "")
	if plain != "" {
		t.Errorf("expected empty for empty input, got %q", plain)
	}
}

func TestDecryptWithCorruptBase64(t *testing.T) {
	cfg := testCfg("corrupt-key")
	// Valid format but invalid base64url
	badCT := "v1:!!!not-base64!!!:!!!not-base64!!!:!!!not-base64!!!"
	plain := DecryptAccountPassword(cfg, badCT)
	if plain != "" {
		t.Errorf("expected empty for corrupt base64, got %q", plain)
	}
}

func TestDecryptWrongTagLength(t *testing.T) {
	cfg := testCfg("tag-key")
	cipherText := EncryptAccountPassword(cfg, "test")
	parts := strings.SplitN(cipherText, ":", 4)

	// Replace tag with 4-byte value (not 16)
	badTag := base64URLEncode([]byte{1, 2, 3, 4})
	modified := parts[0] + ":" + parts[1] + ":" + badTag + ":" + parts[3]
	plain := DecryptAccountPassword(cfg, modified)
	if plain != "" {
		t.Errorf("expected empty for wrong tag length, got %q", plain)
	}
}

// ---- Helpers ----

func truncateForName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
