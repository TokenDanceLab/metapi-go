package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/store"
)

// RuntimeHealthState is the health state of an account at runtime.
type RuntimeHealthState string

const (
	HealthHealthy   RuntimeHealthState = "healthy"
	HealthDegraded  RuntimeHealthState = "degraded"
	HealthUnhealthy RuntimeHealthState = "unhealthy"
	HealthDisabled  RuntimeHealthState = "disabled"
)

// RuntimeHealthSource is the source of a runtime health update.
type RuntimeHealthSource string

const (
	HealthSourceCheckin RuntimeHealthSource = "checkin"
	HealthSourceBalance RuntimeHealthSource = "balance"
	HealthSourceAuth    RuntimeHealthSource = "auth"
)

// RuntimeHealthEntry is a single runtime health record stored in extraConfig.
type RuntimeHealthEntry struct {
	State  RuntimeHealthState  `json:"state"`
	Reason string              `json:"reason"`
	Source RuntimeHealthSource `json:"source"`
}

// SetAccountRuntimeHealth writes runtime health state into the account's extraConfig.
// Mirrors TS setAccountRuntimeHealth().
func SetAccountRuntimeHealth(db *sqlx.DB, accountID int64, entry RuntimeHealthEntry) error {
	// Fetch current extraConfig
	var extraConfig *string
	err := db.Get(&extraConfig, "SELECT extra_config FROM accounts WHERE id = ?", accountID)
	if err != nil {
		return err
	}

	healthJSON, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	healthMap := map[string]any{}
	_ = json.Unmarshal(healthJSON, &healthMap)

	newConfig := MergeExtraConfig(extraConfig, map[string]any{
		"runtimeHealth": healthMap,
	})
	if newConfig == nil {
		// Keep empty config as null
		_, err = db.Exec("UPDATE accounts SET extra_config = NULL, updated_at = ? WHERE id = ?",
			time.Now().UTC().Format(time.RFC3339), accountID)
	} else {
		_, err = db.Exec("UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?",
			*newConfig, time.Now().UTC().Format(time.RFC3339), accountID)
	}
	return err
}

// ExtractRuntimeHealth reads the runtime health from an account's extraConfig.
// Mirrors TS extractRuntimeHealth().
func ExtractRuntimeHealth(extraConfig *string) *RuntimeHealthEntry {
	cfg := ParseExtraConfig(extraConfig)
	if cfg == nil {
		return nil
	}
	healthRaw, ok := cfg["runtimeHealth"]
	if !ok {
		return nil
	}
	// Re-marshal and unmarshal for type safety
	b, err := json.Marshal(healthRaw)
	if err != nil {
		return nil
	}
	var entry RuntimeHealthEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return nil
	}
	if entry.State == "" {
		return nil
	}
	return &entry
}

// IsUnsupportedCheckinRuntimeHealth checks if the runtime health state represents
// an unsupported checkin degraded state that should be preserved during balance refresh.
// Mirrors TS isUnsupportedCheckinRuntimeHealth().
func IsUnsupportedCheckinRuntimeHealth(health *RuntimeHealthEntry) bool {
	if health == nil || health.State != HealthDegraded {
		return false
	}
	if strings.ToLower(string(health.Source)) == "checkin" {
		return true
	}
	reason := strings.ToLower(health.Reason)
	return strings.Contains(reason, "checkin endpoint not found") ||
		strings.Contains(reason, "invalid url (post /api/user/checkin)") ||
		(strings.Contains(reason, "http 404") && strings.Contains(reason, "/api/user/checkin")) ||
		strings.Contains(reason, "unsupported checkin endpoint")
}

// GetCredentialModeFromExtraConfigStr is a string-returning variant for quick checks.
// This is defined in account_service.go; adding a note here for discoverability.
// Use GetCredentialModeFromExtraConfig for typed access.

// Ensure store.Account is compatible
var _ = (*store.Account)(nil)
