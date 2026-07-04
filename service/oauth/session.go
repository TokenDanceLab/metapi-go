package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// ---- Session Store Interface ----

// OAuthSessionStore is the interface for storing OAuth sessions.
type OAuthSessionStore interface {
	Create(input CreateSessionInput) (*SessionRecord, error)
	Get(state string) *SessionRecord
	MarkSuccess(state string, accountID int64, siteID int64) *SessionRecord
	MarkError(state string, errorMsg string) *SessionRecord
}

// CreateSessionInput holds the input for creating a new OAuth session.
type CreateSessionInput struct {
	Provider        string
	RedirectURI     string
	RebindAccountID int64
	ProjectID       string
	ProxyURL        string
	UseSystemProxy  bool
}

const sessionTTL = 10 * time.Minute

// ---- MemoryOAuthSessionStore ----

// MemoryOAuthSessionStore is an in-memory implementation of OAuthSessionStore.
type MemoryOAuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*SessionRecord
}

// NewMemoryOAuthSessionStore creates a new in-memory session store.
func NewMemoryOAuthSessionStore() *MemoryOAuthSessionStore {
	return &MemoryOAuthSessionStore{
		sessions: make(map[string]*SessionRecord),
	}
}

func (s *MemoryOAuthSessionStore) pruneExpiredSessions(now time.Time) {
	for state, session := range s.sessions {
		if !session.ExpiresAt.After(now) {
			delete(s.sessions, state)
		}
	}
}

// Create creates a new OAuth session with a random state and PKCE verifier.
func (s *MemoryOAuthSessionStore) Create(input CreateSessionInput) (*SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.pruneExpiredSessions(now)

	state, err := randomBase64URL(24)
	if err != nil {
		return nil, err
	}
	codeVerifier, err := randomBase64URL(48)
	if err != nil {
		return nil, err
	}
	createdAt := now
	expiresAt := now.Add(sessionTTL)

	record := &SessionRecord{
		Provider:        input.Provider,
		State:           state,
		Status:          SessionPending,
		CodeVerifier:    codeVerifier,
		RedirectURI:     input.RedirectURI,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
		ExpiresAt:       expiresAt,
		RebindAccountID: input.RebindAccountID,
		ProjectID:       input.ProjectID,
		ProxyURL:        input.ProxyURL,
		UseSystemProxy:  input.UseSystemProxy,
	}
	s.sessions[state] = record
	return record, nil
}

// Get retrieves a session by state.
func (s *MemoryOAuthSessionStore) Get(state string) *SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredSessions(time.Now())
	return s.sessions[state]
}

// MarkSuccess marks a session as successful.
func (s *MemoryOAuthSessionStore) MarkSuccess(state string, accountID int64, siteID int64) *SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.sessions[state]
	if existing == nil {
		return nil
	}
	now := time.Now()
	existing.Status = SessionSuccess
	existing.UpdatedAt = now
	existing.AccountID = accountID
	existing.SiteID = siteID
	existing.Error = ""
	s.sessions[state] = existing
	return existing
}

// MarkError marks a session as errored.
func (s *MemoryOAuthSessionStore) MarkError(state string, errorMsg string) *SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.sessions[state]
	if existing == nil {
		return nil
	}
	now := time.Now()
	existing.Status = SessionError
	existing.UpdatedAt = now
	existing.Error = trimOr(errorMsg, "OAuth failed")
	s.sessions[state] = existing
	return existing
}

// ---- Global session store ----

var globalSessionStore OAuthSessionStore = NewMemoryOAuthSessionStore()

// SetSessionStore replaces the global session store. Used for testing.
func SetSessionStore(store OAuthSessionStore) {
	globalSessionStore = store
}

// CreateSession creates a session using the global store.
func CreateSession(input CreateSessionInput) (*SessionRecord, error) {
	return globalSessionStore.Create(input)
}

// GetSession retrieves a session using the global store.
func GetSession(state string) *SessionRecord {
	return globalSessionStore.Get(state)
}

// MarkSessionSuccess marks a session as successful using the global store.
func MarkSessionSuccess(state string, accountID int64, siteID int64) *SessionRecord {
	return globalSessionStore.MarkSuccess(state, accountID, siteID)
}

// MarkSessionError marks a session as errored using the global store.
func MarkSessionError(state string, errorMsg string) *SessionRecord {
	return globalSessionStore.MarkError(state, errorMsg)
}

// ---- PKCE Utilities ----

// CreatePKCEVerifier generates a random 48-byte base64url code verifier.
func CreatePKCEVerifier() (string, error) {
	return randomBase64URL(48)
}

// CreatePKCEChallenge computes the S256 code challenge from a code verifier.
func CreatePKCEChallenge(codeVerifier string) string {
	hash := sha256.Sum256([]byte(codeVerifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// ---- Helpers ----

func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand.Read failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func trimOr(s, fallback string) string {
	trimmed := trimString(s)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func trimString(s string) string {
	// Simple trim of whitespace.
	start, end := 0, len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}

func asNonEmptyString(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		trimmed := trimString(s)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
