package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/store"
)

const (
	TokenValueStatusReady         = "ready"
	TokenValueStatusMaskedPending = "masked_pending"
)

// ---- Token value helpers ----

// NormalizeTokenForDisplay normalizes a token for display.
// Mirrors TS normalizeTokenForDisplay().
func NormalizeTokenForDisplay(token, platform string) string {
	value := strings.TrimSpace(token)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(value), "sk-") {
		return "sk-" + value
	}
	return value
}

// MaskToken masks a token value for display.
// Mirrors TS maskToken().
func MaskToken(token, platform string) string {
	value := NormalizeTokenForDisplay(token, platform)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "sk-") {
		if len(value) <= 7 {
			return "sk-***"
		}
		visibleMiddle := value[3:minInt(6, len(value))]
		if len(value) <= 12 {
			return fmt.Sprintf("sk-%s***%s", visibleMiddle, value[len(value)-2:])
		}
		return fmt.Sprintf("sk-%s***%s", visibleMiddle, value[len(value)-4:])
	}
	if len(value) <= 10 {
		return fmt.Sprintf("%s***%s", value[:2], value[len(value)-2:])
	}
	return fmt.Sprintf("%s***%s", value[:4], value[len(value)-4:])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IsMaskedTokenValue checks if a token value contains masking characters.
// Mirrors TS isMaskedTokenValue().
func IsMaskedTokenValue(token string) bool {
	value := strings.TrimSpace(token)
	if value == "" {
		return false
	}
	return strings.Contains(value, "*") || strings.Contains(value, "•") // bullet char
}

// ResolveAccountTokenValueStatus resolves the valueStatus for a token.
func ResolveAccountTokenValueStatus(tokenRow *store.AccountToken) string {
	if tokenRow.ValueStatus == TokenValueStatusMaskedPending {
		return TokenValueStatusMaskedPending
	}
	if IsMaskedTokenValue(tokenRow.Token) {
		return TokenValueStatusMaskedPending
	}
	return TokenValueStatusReady
}

// IsMaskedPendingAccountToken checks if a token is in masked-pending state.
func IsMaskedPendingAccountToken(token *store.AccountToken) bool {
	if token == nil {
		return false
	}
	return ResolveAccountTokenValueStatus(token) == TokenValueStatusMaskedPending
}

// IsUsableAccountToken checks if a token is usable (ready + enabled).
func IsUsableAccountToken(token *store.AccountToken) bool {
	if token == nil {
		return false
	}
	return token.Enabled && ResolveAccountTokenValueStatus(token) == TokenValueStatusReady && !IsMaskedTokenValue(token.Token)
}

// ---- Token CRUD ----

// GetTokenByID fetches a token by its ID.
func GetTokenByID(db *sqlx.DB, id int64) (*store.AccountToken, error) {
	var token store.AccountToken
	err := db.Get(&token, db.Rebind("SELECT * FROM account_tokens WHERE id = ?"), id)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// ListTokensWithRelations returns tokens with account and site relations.
// Mirrors TS listTokensWithRelations().
func ListTokensWithRelations(db *sqlx.DB, accountID *int64) ([]map[string]any, error) {
	var query string
	var args []any

	baseQuery := `SELECT t.id, t.account_id, t.name, t.token, t.token_group, t.value_status,
		t.source, t.enabled, t.is_default, t.created_at, t.updated_at,
		a.id as account_id_val, a.username, a.status as account_status,
		a.access_token, a.extra_config,
		s.id as site_id, s.name as site_name, s.url as site_url, s.platform as site_platform
		FROM account_tokens t
		INNER JOIN accounts a ON t.account_id = a.id
		INNER JOIN sites s ON a.site_id = s.id`

	if accountID != nil {
		query = baseQuery + " WHERE t.account_id = ? ORDER BY t.id"
		args = append(args, *accountID)
	} else {
		query = baseQuery + " ORDER BY t.id"
	}

	rows, err := db.Queryx(db.Rebind(query), args...)
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
		// Filter out API key connections
		if isAPIKeyConnFromRow(row) {
			continue
		}
		// Add tokenMasked
		tokenVal, _ := row["token"].(string)
		platform, _ := row["site_platform"].(string)
		row["tokenMasked"] = MaskToken(tokenVal, platform)
		// Add valueStatus
		vs, _ := row["value_status"].(string)
		if IsMaskedTokenValue(tokenVal) && vs != TokenValueStatusMaskedPending {
			row["valueStatus"] = TokenValueStatusMaskedPending
		} else {
			row["valueStatus"] = vs
		}
		delete(row, "token") // Remove plain token
		// Add account and site sub-objects
		row["account"] = map[string]any{
			"id":       row["account_id_val"],
			"username": row["username"],
			"status":   row["account_status"],
		}
		row["site"] = map[string]any{
			"id":       row["site_id"],
			"name":     row["site_name"],
			"url":      row["site_url"],
			"platform": row["site_platform"],
		}
		result = append(result, row)
	}
	return result, nil
}

func isAPIKeyConnFromRow(row map[string]any) bool {
	accessToken, _ := row["access_token"].(string)
	if strings.TrimSpace(accessToken) != "" {
		return false
	}
	extraConfig, _ := row["extra_config"].(string)
	cfg := ParseExtraConfig(&extraConfig)
	if cfg != nil {
		if mode, ok := cfg["credentialMode"].(string); ok {
			m := NormalizeCredentialMode(mode)
			if m == CredentialModeAPIKey {
				return true
			}
		}
	}
	return false
}

// SetDefaultToken sets a token as the default for its account.
// Mirrors TS setDefaultToken().
func SetDefaultToken(db *sqlx.DB, tokenID int64) (bool, error) {
	tx, err := db.Beginx()
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var token store.AccountToken
	if err := tx.Get(&token, tx.Rebind("SELECT * FROM account_tokens WHERE id = ?"), tokenID); err != nil {
		return false, err
	}
	if !IsUsableAccountToken(&token) {
		return false, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Clear all defaults for this account
	if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, updated_at = ? WHERE account_id = ?"), false, now, token.AccountID); err != nil {
		return false, err
	}

	// Set this token as default
	if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, enabled = ?, updated_at = ? WHERE id = ?"), true, true, now, tokenID); err != nil {
		return false, err
	}

	// Update account api_token
	if _, err := tx.Exec(tx.Rebind("UPDATE accounts SET api_token = ?, updated_at = ? WHERE id = ?"), token.Token, now, token.AccountID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

// RepairDefaultToken finds and sets a new default token for an account.
// Mirrors TS repairDefaultToken().
func RepairDefaultToken(db *sqlx.DB, accountID int64) (*store.AccountToken, error) {
	tx, err := db.Beginx()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var tokens []store.AccountToken
	if err := tx.Select(&tokens, tx.Rebind("SELECT * FROM account_tokens WHERE account_id = ?"), accountID); err != nil {
		return nil, err
	}

	// Find usable tokens
	var usable []store.AccountToken
	for i := range tokens {
		if IsUsableAccountToken(&tokens[i]) {
			usable = append(usable, tokens[i])
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	if len(usable) == 0 {
		// Clear api_token
		if _, err := tx.Exec(tx.Rebind("UPDATE accounts SET api_token = NULL, updated_at = ? WHERE id = ?"), now, accountID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return nil, nil
	}

	// Find current default or pick first usable
	selected := usable[0]
	for i := range usable {
		if usable[i].IsDefault {
			selected = usable[i]
			break
		}
	}

	// Clear all defaults
	if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, updated_at = ? WHERE account_id = ?"), false, now, accountID); err != nil {
		return nil, err
	}

	// Set new default
	if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, enabled = ?, updated_at = ? WHERE id = ?"), true, true, now, selected.ID); err != nil {
		return nil, err
	}

	// Update account api_token
	if _, err := tx.Exec(tx.Rebind("UPDATE accounts SET api_token = ?, updated_at = ? WHERE id = ?"), selected.Token, now, accountID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	return &selected, nil
}

// EnsureDefaultTokenForAccount ensures a token exists and is default for a given account.
// Mirrors TS ensureDefaultTokenForAccount().
func EnsureDefaultTokenForAccount(db *sqlx.DB, accountID int64, tokenValue string, name, source, tokenGroup string, enabled bool) (int64, error) {
	normalized := strings.TrimSpace(tokenValue)
	if normalized == "" || IsMaskedTokenValue(normalized) {
		return 0, nil
	}
	if tokenGroup == "" {
		tokenGroup = "default"
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Beginx()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var tokens []store.AccountToken
	if err := tx.Select(&tokens, tx.Rebind("SELECT * FROM account_tokens WHERE account_id = ?"), accountID); err != nil {
		return 0, err
	}

	// Find existing token by value
	var target *store.AccountToken
	for i := range tokens {
		if tokens[i].Token == normalized {
			target = &tokens[i]
			break
		}
	}

	if target == nil {
		// Create new
		tokenName := name
		if tokenName == "" {
			if len(tokens) == 0 {
				tokenName = "default"
			} else {
				tokenName = fmt.Sprintf("token-%d", len(tokens)+1)
			}
		}
		src := source
		if src == "" {
			src = "manual"
		}

		result, err := tx.Exec(
			tx.Rebind(`INSERT INTO account_tokens (account_id, name, token, token_group, value_status, source, enabled, is_default, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			accountID, tokenName, normalized, tokenGroup, TokenValueStatusReady, src, enabled, true, now, now,
		)
		if err != nil {
			return 0, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			// Fallback for Postgres which doesn't support LastInsertId.
			if err := tx.Get(&id, tx.Rebind("SELECT id FROM account_tokens WHERE account_id = ? AND token = ? ORDER BY id DESC LIMIT 1"), accountID, normalized); err != nil {
				return 0, err
			}
		}
		targetID := id

		// Clear other defaults
		if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, updated_at = ? WHERE account_id = ? AND id != ?"), false, now, accountID, targetID); err != nil {
			return 0, err
		}

		// Update account api_token
		if _, err := tx.Exec(tx.Rebind("UPDATE accounts SET api_token = ?, updated_at = ? WHERE id = ?"), normalized, now, accountID); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		committed = true
		return targetID, nil
	}

	// Update existing
	updateName := target.Name
	if name != "" {
		updateName = name
	}
	src := source
	if src == "" {
		src = target.Source
	}
	_, err = tx.Exec(
		tx.Rebind(`UPDATE account_tokens SET name = ?, token_group = ?, value_status = ?, source = ?, enabled = ?, is_default = ?, updated_at = ?
		 WHERE id = ?`),
		updateName, tokenGroup, TokenValueStatusReady, src, enabled, true, now, target.ID,
	)
	if err != nil {
		return 0, err
	}

	// Clear other defaults
	if _, err := tx.Exec(tx.Rebind("UPDATE account_tokens SET is_default = ?, updated_at = ? WHERE account_id = ? AND id != ?"), false, now, accountID, target.ID); err != nil {
		return 0, err
	}

	// Update account api_token
	if _, err := tx.Exec(tx.Rebind("UPDATE accounts SET api_token = ?, updated_at = ? WHERE id = ?"), normalized, now, accountID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true
	return target.ID, nil
}

// GetDefaultTokenForAccount returns the default token for an account.
func GetDefaultTokenForAccount(db *sqlx.DB, accountID int64) (*store.AccountToken, error) {
	var token store.AccountToken
	err := db.Get(&token,
		db.Rebind(`SELECT t.* FROM account_tokens t
		 INNER JOIN accounts a ON t.account_id = a.id
		 WHERE t.account_id = ? AND t.is_default = TRUE`), accountID,
	)
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// GetTokenGroups fetches token groups from the database.
// TODO(P4): call adapter.getUserGroups() on the upstream site instead of querying local DB.
// TS accountTokens.ts:939-944 calls adapter.getUserGroups() — the Go version currently returns
// what is locally cached, which may differ from server-side group management on some platforms.
func GetTokenGroups(db *sqlx.DB, accountID int64) ([]string, error) {
	var groups []string
	err := db.Select(&groups,
		db.Rebind("SELECT DISTINCT COALESCE(token_group, 'default') FROM account_tokens WHERE account_id = ?"),
		accountID,
	)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []string{"default"}, nil
	}
	return groups, nil
}

// DeleteTokenByID deletes a token by ID with upstream-first strategy.
// The caller must handle the upstream deletion before calling this.
func DeleteTokenByID(db *sqlx.DB, tokenID int64) error {
	token, err := GetTokenByID(db, tokenID)
	if err != nil {
		return fmt.Errorf("令牌不存在")
	}

	_, err = db.Exec(db.Rebind("DELETE FROM account_tokens WHERE id = ?"), tokenID)
	if err != nil {
		return err
	}

	// If this was the default, repair
	if token.IsDefault {
		if _, err := RepairDefaultToken(db, token.AccountID); err != nil {
			return err
		}
	}

	return nil
}

// UpdateTokenFields updates specific fields on a token.
func UpdateTokenFields(db *sqlx.DB, tokenID int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var setClauses []string
	var args []any

	colMap := map[string]string{
		"name":        "name",
		"token":       "token",
		"tokenGroup":  "token_group",
		"valueStatus": "value_status",
		"source":      "source",
		"enabled":     "enabled",
		"isDefault":   "is_default",
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
	args = append(args, tokenID)

	query := fmt.Sprintf("UPDATE account_tokens SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := db.Exec(db.Rebind(query), args...)
	return err
}

// BatchUpdateTokenStatus batch-updates enabled status for tokens.
func BatchUpdateTokenStatus(db *sqlx.DB, tokenIDs []int64, enabled bool) error {
	if len(tokenIDs) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	query, args, err := sqlx.In(
		"UPDATE account_tokens SET enabled = ?, updated_at = ? WHERE id IN (?)",
		enabled, now, tokenIDs,
	)
	if err != nil {
		return err
	}
	query = db.Rebind(query)
	_, err = db.Exec(query, args...)
	return err
}
