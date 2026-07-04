package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"
)

// AccountCredentialMode is the credential mode for an account.
type AccountCredentialMode string

const (
	CredentialModeAuto    AccountCredentialMode = "auto"
	CredentialModeSession AccountCredentialMode = "session"
	CredentialModeAPIKey  AccountCredentialMode = "apikey"
)

// AccountCapabilities describes what an account can do.
type AccountCapabilities struct {
	CanCheckin        bool `json:"canCheckin"`
	CanRefreshBalance bool `json:"canRefreshBalance"`
	ProxyOnly         bool `json:"proxyOnly"`
}

// ---- Credential mode resolution ----

// GetCredentialModeFromExtraConfig reads credentialMode from extraConfig JSON.
func GetCredentialModeFromExtraConfig(extraConfig *string) AccountCredentialMode {
	config := ParseExtraConfig(extraConfig)
	if config == nil {
		return ""
	}
	if mode, ok := config["credentialMode"].(string); ok {
		normalized := NormalizeCredentialMode(mode)
		if normalized != "" {
			return normalized
		}
	}
	return ""
}

// NormalizeCredentialMode normalizes a credential mode string.
func NormalizeCredentialMode(input string) AccountCredentialMode {
	normalized := AccountCredentialMode(strings.TrimSpace(strings.ToLower(input)))
	switch normalized {
	case CredentialModeAuto, CredentialModeSession, CredentialModeAPIKey:
		return normalized
	}
	return ""
}

// ResolveStoredCredentialMode resolves the effective credential mode from storage.
// 1. Read extraConfig.credentialMode; if explicit and != "auto", return it
// 2. If accessToken is non-empty, return "session"
// 3. Otherwise return "apikey"
func ResolveStoredCredentialMode(account *store.Account) AccountCredentialMode {
	fromConfig := GetCredentialModeFromExtraConfig(account.ExtraConfig)
	if fromConfig != "" && fromConfig != CredentialModeAuto {
		return fromConfig
	}
	if strings.TrimSpace(account.AccessToken) != "" {
		return CredentialModeSession
	}
	return CredentialModeAPIKey
}

// ResolveRequestedCredentialMode resolves the requested credential mode from input.
func ResolveRequestedCredentialMode(input *string) AccountCredentialMode {
	if input == nil {
		return CredentialModeAuto
	}
	mode := NormalizeCredentialMode(*input)
	if mode == "" {
		return CredentialModeAuto
	}
	return mode
}

// HasSessionToken checks whether the account has a valid session token value.
func HasSessionToken(accessToken string) bool {
	return strings.TrimSpace(accessToken) != ""
}

// IsAPIKeyConnection checks if an account is an API key connection.
func IsAPIKeyConnection(account *store.Account) bool {
	explicit := GetCredentialModeFromExtraConfig(account.ExtraConfig)
	if explicit != "" && explicit != CredentialModeAuto {
		return explicit == CredentialModeAPIKey
	}
	return strings.TrimSpace(account.AccessToken) == ""
}

// BuildCapabilitiesForAccount returns the capabilities for an account.
func BuildCapabilitiesForAccount(account *store.Account) AccountCapabilities {
	mode := ResolveStoredCredentialMode(account)
	hasToken := HasSessionToken(account.AccessToken)
	return BuildCapabilitiesFromCredentialMode(mode, hasToken)
}

// BuildCapabilitiesFromCredentialMode returns capabilities from credential mode.
func BuildCapabilitiesFromCredentialMode(mode AccountCredentialMode, hasSessionToken bool) AccountCapabilities {
	sessionCapable := false
	switch mode {
	case CredentialModeSession:
		sessionCapable = hasSessionToken
	case CredentialModeAPIKey:
		sessionCapable = false
	default: // auto
		sessionCapable = hasSessionToken
	}
	return AccountCapabilities{
		CanCheckin:        sessionCapable,
		CanRefreshBalance: sessionCapable,
		ProxyOnly:         !sessionCapable,
	}
}

// ---- AES Password Encryption (delegates to account_credential.go) ----

// EncryptPassword encrypts a password for storage in extraConfig.autoRelogin.passwordCipher.
func EncryptPassword(cfg *config.Config, password string) (string, error) {
	return EncryptAccountPassword(cfg, password)
}

// DecryptPassword decrypts a password from extraConfig.
func DecryptPassword(cfg *config.Config, cipherText string) string {
	return DecryptAccountPassword(cfg, cipherText)
}

// ---- ExtraConfig helpers ----

// MergeExtraConfig merges a patch into an existing extraConfig JSON string.
func MergeExtraConfig(existing *string, patch map[string]any) *string {
	base := ParseExtraConfig(existing)
	if base == nil {
		base = make(map[string]any)
	}
	for k, v := range patch {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	return MarshalExtraConfig(base)
}

// GetProxyURLFromExtraConfig reads proxyUrl from extraConfig.
func GetProxyURLFromExtraConfig(extraConfig *string) string {
	config := ParseExtraConfig(extraConfig)
	if config == nil {
		return ""
	}
	if url, ok := config["proxyUrl"].(string); ok {
		return url
	}
	return ""
}

// ResolvePlatformUserID resolves the platform user ID from extraConfig or username.
func ResolvePlatformUserID(extraConfig *string, username *string) int64 {
	config := ParseExtraConfig(extraConfig)
	if config != nil {
		if id, ok := config["platformUserId"].(float64); ok {
			return int64(id)
		}
	}
	return 0
}

// ---- Account CRUD helpers ----

// GetAccountByID fetches an account by its ID.
func GetAccountByID(db *sqlx.DB, id int64) (*store.Account, error) {
	var account store.Account
	err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// GetAccountWithSite joins account with site.
type AccountWithSite struct {
	Account store.Account `db:"accounts"`
	Site    store.Site    `db:"sites"`
}

// GetAccountWithSiteByID fetches an account with its site.
func GetAccountWithSiteByID(db *sqlx.DB, id int64) (*AccountWithSite, error) {
	query := `SELECT a.id AS "accounts.id", a.site_id AS "accounts.site_id", a.username AS "accounts.username",
		a.access_token AS "accounts.access_token", a.api_token AS "accounts.api_token",
		a.balance AS "accounts.balance", a.balance_used AS "accounts.balance_used",
		a.quota AS "accounts.quota", a.unit_cost AS "accounts.unit_cost",
		a.value_score AS "accounts.value_score", a.status AS "accounts.status",
		a.is_pinned AS "accounts.is_pinned", a.sort_order AS "accounts.sort_order",
		a.checkin_enabled AS "accounts.checkin_enabled", a.last_checkin_at AS "accounts.last_checkin_at",
		a.last_balance_refresh AS "accounts.last_balance_refresh",
		a.oauth_provider AS "accounts.oauth_provider", a.oauth_account_key AS "accounts.oauth_account_key",
		a.oauth_project_id AS "accounts.oauth_project_id", a.extra_config AS "accounts.extra_config",
		a.created_at AS "accounts.created_at", a.updated_at AS "accounts.updated_at",
		s.id AS "sites.id", s.name AS "sites.name", s.url AS "sites.url",
		s.platform AS "sites.platform", s.status AS "sites.status"
		FROM accounts a INNER JOIN sites s ON a.site_id = s.id WHERE a.id = ?`

	var row struct {
		Accounts struct {
			ID               int64   `db:"id"`
			SiteID           int64   `db:"site_id"`
			Username         *string `db:"username"`
			AccessToken      string  `db:"access_token"`
			APIToken         *string `db:"api_token"`
			Balance          float64 `db:"balance"`
			BalanceUsed      float64 `db:"balance_used"`
			Quota            float64 `db:"quota"`
			UnitCost         *float64 `db:"unit_cost"`
			ValueScore       float64 `db:"value_score"`
			Status           string  `db:"status"`
			IsPinned         bool    `db:"is_pinned"`
			SortOrder        int64   `db:"sort_order"`
			CheckinEnabled   bool    `db:"checkin_enabled"`
			LastCheckinAt    *string `db:"last_checkin_at"`
			LastBalanceRefresh *string `db:"last_balance_refresh"`
			OAuthProvider    *string `db:"oauth_provider"`
			OAuthAccountKey  *string `db:"oauth_account_key"`
			OAuthProjectID   *string `db:"oauth_project_id"`
			ExtraConfig      *string `db:"extra_config"`
			CreatedAt        string  `db:"created_at"`
			UpdatedAt        string  `db:"updated_at"`
		} `db:"accounts"`
		Sites struct {
			ID       int64  `db:"id"`
			Name     string `db:"name"`
			URL      string `db:"url"`
			Platform string `db:"platform"`
			Status   string `db:"status"`
		} `db:"sites"`
	}

	if err := db.Get(&row, query, id); err != nil {
		return nil, err
	}
	return &AccountWithSite{
		Account: store.Account{
			ID: row.Accounts.ID, SiteID: row.Accounts.SiteID,
			Username: row.Accounts.Username, AccessToken: row.Accounts.AccessToken,
			APIToken: row.Accounts.APIToken, Balance: row.Accounts.Balance,
			BalanceUsed: row.Accounts.BalanceUsed, Quota: row.Accounts.Quota,
			UnitCost: row.Accounts.UnitCost, ValueScore: row.Accounts.ValueScore,
			Status: row.Accounts.Status, IsPinned: row.Accounts.IsPinned,
			SortOrder: row.Accounts.SortOrder, CheckinEnabled: row.Accounts.CheckinEnabled,
			LastCheckinAt: row.Accounts.LastCheckinAt, LastBalanceRefresh: row.Accounts.LastBalanceRefresh,
			OAuthProvider: row.Accounts.OAuthProvider, OAuthAccountKey: row.Accounts.OAuthAccountKey,
			OAuthProjectID: row.Accounts.OAuthProjectID, ExtraConfig: row.Accounts.ExtraConfig,
			CreatedAt: row.Accounts.CreatedAt, UpdatedAt: row.Accounts.UpdatedAt,
		},
		Site: store.Site{
			ID: row.Sites.ID, Name: row.Sites.Name,
			URL: row.Sites.URL, Platform: row.Sites.Platform,
			Status: row.Sites.Status,
		},
	}, nil
}

// DeleteAccount deletes an account by ID.
func DeleteAccount(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM accounts WHERE id = ?", id)
	return err
}

// GetNextAccountSortOrder returns the next sortOrder value for new accounts.
func GetNextAccountSortOrder(db *sqlx.DB) (int64, error) {
	var maxOrder int64
	err := db.Get(&maxOrder, "SELECT COALESCE(MAX(sort_order), -1) FROM accounts")
	if err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

// InsertAccount inserts a new account and returns the ID.
func InsertAccount(db *sqlx.DB, account map[string]any) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	sortOrder := account["sortOrder"].(int64)
	extraConfig := account["extraConfig"]
	var extraConfigStr *string
	if extraConfig != nil {
		b, err := json.Marshal(extraConfig)
		if err == nil {
			s := string(b)
			extraConfigStr = &s
		}
	}

	result, err := db.Exec(
		`INSERT INTO accounts (site_id, username, access_token, api_token, balance, balance_used, quota,
		 unit_cost, value_score, status, is_pinned, sort_order, checkin_enabled,
		 last_checkin_at, last_balance_refresh, oauth_provider, oauth_account_key,
		 oauth_project_id, extra_config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account["siteId"], account["username"], account["accessToken"],
		account["apiToken"], account["balance"], account["balanceUsed"],
		account["quota"], account["unitCost"], account["valueScore"],
		account["status"], account["isPinned"], sortOrder,
		account["checkinEnabled"], account["lastCheckinAt"], account["lastBalanceRefresh"],
		account["oauthProvider"], account["oauthAccountKey"], account["oauthProjectID"],
		extraConfigStr, now, now,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted account id: %w", err)
	}
	return id, nil
}

// UpdateAccountFields updates specific fields on an account.
func UpdateAccountFields(db *sqlx.DB, accountID int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var setClauses []string
	var args []any

	colMap := map[string]string{
		"accessToken":    "access_token",
		"apiToken":       "api_token",
		"username":       "username",
		"status":         "status",
		"checkinEnabled": "checkin_enabled",
		"unitCost":       "unit_cost",
		"extraConfig":    "extra_config",
		"isPinned":       "is_pinned",
		"sortOrder":      "sort_order",
		"balance":        "balance",
		"balanceUsed":    "balance_used",
		"quota":          "quota",
		"valueScore":     "value_score",
	}

	for key, val := range updates {
		if col, ok := colMap[key]; ok {
			setClauses = append(setClauses, col+" = ?")
			args = append(args, val)
		}
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, now)
	args = append(args, accountID)

	query := fmt.Sprintf("UPDATE accounts SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := db.Exec(query, args...)
	return err
}

// ListAccountsWithSites returns all accounts joined with their sites.
func ListAccountsWithSites(db *sqlx.DB) ([]map[string]any, error) {
	rows, err := db.Queryx(
		`SELECT a.*, s.id as site_id_val, s.name as site_name, s.url as site_url,
		        s.platform as site_platform, s.status as site_status
		 FROM accounts a INNER JOIN sites s ON a.site_id = s.id
		 ORDER BY a.sort_order, a.id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]any
	for rows.Next() {
		row := make(map[string]any)
		if err := rows.MapScan(row); err != nil {
			continue
		}
		result = append(result, mapKeysToCamel(row))
	}
	return result, nil
}

// ---- Event helpers ----

// snakeToCamel converts snake_case to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// mapKeysToCamel returns a new map with all keys converted from snake_case to camelCase.
func mapKeysToCamel(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[snakeToCamel(k)] = v
	}
	return result
}

// CreateEvent creates an event entry.
func CreateEvent(db *sqlx.DB, eventType string, title string, message string, level string, relatedID int64, relatedType string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO events (type, title, message, level, read, related_id, related_type, created_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
		eventType, title, message, level, relatedID, relatedType, now,
	)
	return err
}
