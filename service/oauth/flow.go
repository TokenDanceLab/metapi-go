package oauth

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/store"

	"github.com/jmoiron/sqlx"
)

const manualCallbackDelayMs = 15000

// ---- OAuth Flow Types ----

// FlowStartResult is the result of starting an OAuth flow.
type FlowStartResult struct {
	Provider         string            `json:"provider"`
	State            string            `json:"state"`
	AuthorizationURL string            `json:"authorizationUrl"`
	Instructions     *FlowInstructions `json:"instructions"`
}

// FlowInstructions holds callback instructions for the user.
type FlowInstructions struct {
	RedirectURI           string `json:"redirectUri"`
	CallbackPort          int    `json:"callbackPort"`
	CallbackPath          string `json:"callbackPath"`
	ManualCallbackDelayMs int    `json:"manualCallbackDelayMs"`
	SSHTunnelCommand      string `json:"sshTunnelCommand,omitempty"`
	SSHTunnelKeyCommand   string `json:"sshTunnelKeyCommand,omitempty"`
}

// CallbackInput is the input to HandleCallback.
type CallbackInput struct {
	Provider string `json:"provider"`
	State    string `json:"state"`
	Code     string `json:"code,omitempty"`
	Error    string `json:"error,omitempty"`
}

// CallbackResult is the result of HandleCallback.
type CallbackResult struct {
	AccountID int64 `json:"accountId"`
	SiteID    int64 `json:"siteId"`
}

// SessionStatusResult is the result of GetSessionStatus.
type SessionStatusResult struct {
	Provider  string        `json:"provider"`
	State     string        `json:"state"`
	Status    SessionStatus `json:"status"`
	AccountID int64         `json:"accountId,omitempty"`
	SiteID    int64         `json:"siteId,omitempty"`
	Error     string        `json:"error,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// StartFlowInput holds the input for starting an OAuth flow.
type StartFlowInput struct {
	Provider        string
	RebindAccountID int64
	ProjectID       string
	ProxyURL        string
	UseSystemProxy  bool
	RequestOrigin   string
}

// ManualCallbackInput holds the input for manual callback submission.
type ManualCallbackInput struct {
	State       string `json:"state"`
	CallbackURL string `json:"callbackUrl"`
}

// ---- Flow Functions ----

// StartFlow initiates an OAuth flow for the given provider.
func StartFlow(input StartFlowInput) (*FlowStartResult, error) {
	def := GetProviderDefinition(input.Provider)
	if def == nil {
		return nil, fmt.Errorf("unsupported oauth provider: %s", input.Provider)
	}

	callbackState := GetLoopbackCallbackServerState(input.Provider)
	if callbackState != nil && callbackState.Attempted && !callbackState.Ready {
		return nil, fmt.Errorf("%s oauth callback listener is unavailable: %s", input.Provider, callbackState.Error)
	}

	redirectURI := def.Loopback.RedirectURI
	session := CreateSession(CreateSessionInput{
		Provider:        input.Provider,
		RedirectURI:     redirectURI,
		RebindAccountID: input.RebindAccountID,
		ProjectID:       input.ProjectID,
		ProxyURL:        input.ProxyURL,
		UseSystemProxy:  input.UseSystemProxy,
	})

	authURL, err := def.BuildAuthorizationURL(context.Background(), BuildAuthURLInput{
		State:        session.State,
		RedirectURI:  session.RedirectURI,
		CodeVerifier: session.CodeVerifier,
		ProjectID:    session.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build authorization URL: %w", err)
	}

	instructions := buildLoopbackInstructions(def, input.RequestOrigin)

	return &FlowStartResult{
		Provider:         input.Provider,
		State:            session.State,
		AuthorizationURL: authURL,
		Instructions:     instructions,
	}, nil
}

// GetSessionStatus returns the current status of an OAuth session by state.
func GetSessionStatus(state string) *SessionStatusResult {
	session := GetSession(state)
	if session == nil {
		return nil
	}
	return &SessionStatusResult{
		Provider:  session.Provider,
		State:     session.State,
		Status:    session.Status,
		AccountID: session.AccountID,
		SiteID:    session.SiteID,
		Error:     session.Error,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
}

// HandleCallback processes an OAuth callback.
func HandleCallback(input CallbackInput) (*CallbackResult, error) {
	session := GetSession(input.State)
	if session == nil || session.Provider != input.Provider {
		return nil, fmt.Errorf("oauth session not found or provider mismatch")
	}

	def := GetProviderDefinition(input.Provider)
	if def == nil {
		MarkSessionError(input.State, fmt.Sprintf("unsupported oauth provider: %s", input.Provider))
		return nil, fmt.Errorf("unsupported oauth provider: %s", input.Provider)
	}

	if input.Error != "" {
		MarkSessionError(input.State, input.Error)
		return nil, fmt.Errorf("%s", input.Error)
	}

	code := strings.TrimSpace(input.Code)
	if code == "" {
		MarkSessionError(input.State, "missing oauth code")
		return nil, fmt.Errorf("missing oauth code")
	}

	// Resolve proxy URL with three-tier fallback:
	// a) session.proxyUrl (explicit per-flow proxy) -- highest priority
	// b) session.useSystemProxy -> system proxy from config
	// c) resolveOauthProviderProxyUrl(provider) -- provider-specific default
	var resolvedProxyURL *string
	if session.ProxyURL != "" {
		resolvedProxyURL = &session.ProxyURL
	} else if session.UseSystemProxy {
		systemProxy := strings.TrimSpace(config.Get().SystemProxyUrl)
		if systemProxy != "" {
			resolvedProxyURL = &systemProxy
		}
	}
	if resolvedProxyURL == nil {
		providerProxy := resolveOauthProviderProxyUrl(input.Provider)
		if providerProxy != nil {
			resolvedProxyURL = providerProxy
		}
	}

	token, err := def.ExchangeAuthorizationCode(context.Background(), ExchangeCodeInput{
		Code:         code,
		State:        input.State,
		RedirectURI:  session.RedirectURI,
		CodeVerifier: session.CodeVerifier,
		ProjectID:    session.ProjectID,
		ProxyURL:     resolvedProxyURL,
	})
	if err != nil {
		MarkSessionError(input.State, err.Error())
		return nil, err
	}

	// Persist the OAuth account with full pipeline.
	ctx := context.Background()
	persistResult, err := activatePersistedOAuthAccount(ctx, ActivateInput{
		Definition:      def,
		Exchange:        token,
		RebindAccountID: session.RebindAccountID,
		ProxyURL:        session.ProxyURL,
		UseSystemProxy:  session.UseSystemProxy,
	})
	if err != nil {
		MarkSessionError(input.State, err.Error())
		return nil, err
	}

	MarkSessionSuccess(input.State, persistResult.AccountID, persistResult.SiteID)
	return &CallbackResult{
		AccountID: persistResult.AccountID,
		SiteID:    persistResult.SiteID,
	}, nil
}

// SubmitManualCallback handles a manual callback URL submission.
func SubmitManualCallback(input ManualCallbackInput) error {
	session := GetSession(input.State)
	if session == nil {
		return fmt.Errorf("oauth session not found")
	}

	parsed, err := parseManualCallbackURL(input.CallbackURL)
	if err != nil {
		return err
	}

	if parsed.State != input.State {
		return fmt.Errorf("oauth callback state mismatch")
	}

	_, err = HandleCallback(CallbackInput{
		Provider: session.Provider,
		State:    parsed.State,
		Code:     parsed.Code,
		Error:    parsed.Error,
	})
	return err
}

// ---- Internal helpers ----

func isLoopbackHost(hostname string) bool {
	normalized := strings.ToLower(strings.TrimSpace(hostname))
	return normalized == "localhost" ||
		normalized == "127.0.0.1" ||
		normalized == "::1" ||
		normalized == "[::1]"
}

func resolveSSHTunnelHost(requestOrigin string) string {
	if requestOrigin == "" {
		return ""
	}
	parsed, err := url.Parse(requestOrigin)
	if err != nil {
		return ""
	}
	if parsed.Hostname() == "" || isLoopbackHost(parsed.Hostname()) {
		return ""
	}
	return parsed.Hostname()
}

func buildLoopbackInstructions(def *OAuthProviderDefinition, requestOrigin string) *FlowInstructions {
	sshHost := resolveSSHTunnelHost(requestOrigin)
	instructions := &FlowInstructions{
		RedirectURI:           def.Loopback.RedirectURI,
		CallbackPort:          def.Loopback.Port,
		CallbackPath:          def.Loopback.Path,
		ManualCallbackDelayMs: manualCallbackDelayMs,
	}
	if sshHost != "" {
		instructions.SSHTunnelCommand = fmt.Sprintf("ssh -L %d:127.0.0.1:%d root@%s -p 22",
			def.Loopback.Port, def.Loopback.Port, sshHost)
		instructions.SSHTunnelKeyCommand = fmt.Sprintf("ssh -i <path_to_your_key> -L %d:127.0.0.1:%d root@%s -p 22",
			def.Loopback.Port, def.Loopback.Port, sshHost)
	}
	return instructions
}

type manualCallbackParsed struct {
	State string
	Code  string
	Error string
}

func parseManualCallbackURL(callbackURL string) (*manualCallbackParsed, error) {
	raw := strings.TrimSpace(callbackURL)
	if raw == "" {
		return nil, fmt.Errorf("invalid oauth callback url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth callback url")
	}
	state := strings.TrimSpace(parsed.Query().Get("state"))
	code := strings.TrimSpace(parsed.Query().Get("code"))
	errorParam := strings.TrimSpace(parsed.Query().Get("error"))
	errorDesc := strings.TrimSpace(parsed.Query().Get("error_description"))

	if state == "" || (code == "" && errorParam == "") {
		return nil, fmt.Errorf("invalid oauth callback url")
	}

	errStr := errorParam
	if errStr != "" && errorDesc != "" {
		errStr = errStr + ": " + errorDesc
	}

	return &manualCallbackParsed{
		State: state,
		Code:  code,
		Error: errStr,
	}, nil
}

// ---- Account persistence ----

// ActivateInput holds the input for activating a persisted OAuth account.
type ActivateInput struct {
	Definition      *OAuthProviderDefinition
	Exchange        *TokenSet
	RebindAccountID int64
	ProxyURL        string
	UseSystemProxy  bool
	PersistedStatus string // "active" or "disabled"
}

// PersistResult holds the result of account persistence.
type PersistResult struct {
	AccountID int64
	SiteID    int64
}

// accountSnapshot captures the state of an account before mutation for rollback.
type accountSnapshot struct {
	account *store.Account
	// modelAvailability is captured as a raw row list for rollback.
	modelAvailability []map[string]interface{}
}

// activatePersistedOAuthAccount persists or updates an OAuth account with full pipeline:
// upsert -> refresh models -> rebuild routes, with rollback on failures.
func activatePersistedOAuthAccount(ctx context.Context, input ActivateInput) (*PersistResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	def := input.Definition

	// Ensure site exists.
	site, err := ensureOAuthProviderSite(db, def)
	if err != nil {
		return nil, err
	}

	// Find existing account and save snapshot for rollback.
	existing := findExistingOAuthAccount(db, def.Metadata.Provider, input.Exchange, input.RebindAccountID)
	var snapshot *accountSnapshot
	if existing != nil {
		snapshot = captureAccountSnapshot(db, existing)
	}

	// Build username.
	username := input.Exchange.Email
	if username == "" {
		username = input.Exchange.AccountKey
	}
	if username == "" {
		username = input.Exchange.AccountID
	}
	if username == "" {
		username = fmt.Sprintf("%s-user", def.Metadata.Provider)
	}

	// Build OAuth info.
	oauthInfo := BuildOauthInfo(nil, &OauthInfo{
		Provider:       string(def.Metadata.Provider),
		AccountID:      input.Exchange.AccountID,
		AccountKey:     input.Exchange.AccountKey,
		Email:          input.Exchange.Email,
		PlanType:       input.Exchange.PlanType,
		ProjectID:      input.Exchange.ProjectID,
		RefreshToken:   input.Exchange.RefreshToken,
		TokenExpiresAt: input.Exchange.TokenExpiresAt,
		IDToken:        input.Exchange.IDToken,
		ProviderData:   input.Exchange.ProviderData,
	})

	// Build extra config merge patch.
	extraPatch := map[string]interface{}{
		"credentialMode": "session",
	}
	if input.ProxyURL != "" {
		extraPatch["proxyUrl"] = input.ProxyURL
	}
	if input.UseSystemProxy {
		extraPatch["useSystemProxy"] = true
	}
	oauthMap := make(map[string]interface{})
	if oauthInfo.Email != "" {
		oauthMap["email"] = oauthInfo.Email
	}
	if oauthInfo.PlanType != "" {
		oauthMap["planType"] = oauthInfo.PlanType
	}
	if oauthInfo.TokenExpiresAt > 0 {
		oauthMap["tokenExpiresAt"] = oauthInfo.TokenExpiresAt
	}
	if oauthInfo.RefreshToken != "" {
		oauthMap["refreshToken"] = oauthInfo.RefreshToken
	}
	if oauthInfo.IDToken != "" {
		oauthMap["idToken"] = oauthInfo.IDToken
	}
	if oauthInfo.ProviderData != nil {
		oauthMap["providerData"] = oauthInfo.ProviderData
	}
	extraPatch["oauth"] = oauthMap

	var accountID int64
	created := false
	now := time.Now().Format(time.RFC3339)

	if existing != nil {
		// Update existing account.
		status := input.PersistedStatus
		if status == "" {
			status = "disabled"
		}
		accountKey := oauthInfo.AccountKey
		if accountKey == "" {
			accountKey = oauthInfo.AccountID
		}
		projectID := oauthInfo.ProjectID

		extraConfigStr := MergeAccountExtraConfig(existing.ExtraConfig, extraPatch)

		_, err := db.Exec(
			`UPDATE accounts SET site_id = ?, username = ?, access_token = ?, api_token = NULL,
			 checkin_enabled = 0, status = ?, oauth_provider = ?, oauth_account_key = ?,
			 oauth_project_id = ?, extra_config = ?, updated_at = ? WHERE id = ?`,
			site.ID, username, input.Exchange.AccessToken, status,
			string(def.Metadata.Provider), accountKey, projectID,
			extraConfigStr, now, existing.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update oauth account: %w", err)
		}
		accountID = existing.ID
	} else {
		// Create new account.
		created = true
		status := input.PersistedStatus
		if status == "" {
			status = "active"
		}
		accountKey := oauthInfo.AccountKey
		if accountKey == "" {
			accountKey = oauthInfo.AccountID
		}
		projectID := oauthInfo.ProjectID

		extraConfigStr := MergeAccountExtraConfig(nil, extraPatch)

		// Get next sort order.
		var maxSortOrder int64
		_ = db.Get(&maxSortOrder, "SELECT COALESCE(MAX(sort_order), -1) FROM accounts")
		sortOrder := maxSortOrder + 1

		result, err := db.Exec(
			`INSERT INTO accounts (site_id, username, access_token, api_token, checkin_enabled,
			 status, oauth_provider, oauth_account_key, oauth_project_id, extra_config,
			 is_pinned, sort_order, balance, balance_used, quota, value_score, created_at, updated_at)
			 VALUES (?, ?, ?, NULL, 0, ?, ?, ?, ?, ?, 0, ?, 0, 0, 0, 0, ?, ?)`,
			site.ID, username, input.Exchange.AccessToken, status,
			string(def.Metadata.Provider), accountKey, projectID,
			extraConfigStr, sortOrder, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create oauth account: %w", err)
		}
		accountID, err = result.LastInsertId()
		if err != nil {
			// Fallback for Postgres which doesn't support LastInsertId.
			_ = db.Get(&accountID, "SELECT id FROM accounts WHERE site_id = ? AND oauth_provider = ? AND oauth_account_key = ? ORDER BY id DESC LIMIT 1",
				site.ID, string(def.Metadata.Provider), accountKey)
		}
	}

	// Step 7c: Refresh models for account (allowInactive for existing updates).
	hooks := getWorkflowHooks()
	if hooks != nil {
		allowInactive := !created
		if err := hooks.RefreshModelsForAccount(ctx, accountID, allowInactive); err != nil {
			// Step 7d: Full rollback on model refresh failure.
			log.Printf("[oauth] model refresh failed for account %d, rolling back: %v", accountID, err)
			rollbackErr := revertPersistedOauthAccount(db, accountID, created, snapshot)
			if rollbackErr != nil {
				log.Printf("[oauth] rollback failed for account %d: %v", accountID, rollbackErr)
			}
			return nil, fmt.Errorf("model refresh failed for oauth account: %w", err)
		}

		// If existing and we just updated: activate the account now that model refresh succeeded.
		if !created && input.PersistedStatus == "" {
			_, _ = db.Exec("UPDATE accounts SET status = 'active', updated_at = ? WHERE id = ?", now, accountID)
		}

		// Step 7f: Rebuild routes.
		if err := hooks.RebuildRoutesOnly(ctx); err != nil {
			// Step 7g: On route rebuild fail: rollback account + restore routes.
			log.Printf("[oauth] route rebuild failed after account %d, rolling back: %v", accountID, err)
			rollbackErr := revertPersistedOauthAccount(db, accountID, created, snapshot)
			if rollbackErr != nil {
				log.Printf("[oauth] rollback failed for account %d: %v", accountID, rollbackErr)
			}
			// Best-effort retry rebuild after rollback.
			_ = hooks.RebuildRoutesOnly(ctx)
			return nil, fmt.Errorf("route rebuild failed for oauth account: %w", err)
		}

		// Invalidate token router cache after successful rebuild.
		hooks.InvalidateTokenRouterCache()
	}

	return &PersistResult{
		AccountID: accountID,
		SiteID:    site.ID,
	}, nil
}

// captureAccountSnapshot captures the current state of an account for rollback.
func captureAccountSnapshot(db *store.DB, account *store.Account) *accountSnapshot {
	snap := &accountSnapshot{
		account: account,
	}
	// Capture model availability rows.
	rows, err := db.Queryx("SELECT * FROM model_availability WHERE account_id = ?", account.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			row := make(map[string]interface{})
			if err := rows.MapScan(row); err == nil {
				snap.modelAvailability = append(snap.modelAvailability, row)
			}
		}
	}
	return snap
}

// revertPersistedOauthAccount rolls back a persisted OAuth account to its previous state.
// If created=true, deletes the account. If created=false, restores all previous fields.
func revertPersistedOauthAccount(db *store.DB, accountID int64, created bool, snapshot *accountSnapshot) error {
	if created {
		// Delete the newly created account.
		_, err := db.Exec("DELETE FROM accounts WHERE id = ?", accountID)
		return err
	}

	if snapshot == nil || snapshot.account == nil {
		return fmt.Errorf("no snapshot available for rollback")
	}

	prev := snapshot.account
	now := time.Now().Format(time.RFC3339)

	// Restore all previous fields in a transaction.
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(
		`UPDATE accounts SET site_id = ?, username = ?, access_token = ?, api_token = ?,
		 checkin_enabled = ?, status = ?, oauth_provider = ?, oauth_account_key = ?,
		 oauth_project_id = ?, extra_config = ?, updated_at = ? WHERE id = ?`,
		prev.SiteID, prev.Username, prev.AccessToken, prev.APIToken,
		prev.CheckinEnabled, prev.Status, prev.OAuthProvider, prev.OAuthAccountKey,
		prev.OAuthProjectID, prev.ExtraConfig, now, accountID,
	)
	if err != nil {
		return fmt.Errorf("rollback account update failed: %w", err)
	}

	// Restore model availability: delete current, re-insert previous.
	_, err = tx.Exec("DELETE FROM model_availability WHERE account_id = ?", accountID)
	if err != nil {
		return fmt.Errorf("rollback model_availability delete failed: %w", err)
	}

	for _, row := range snapshot.modelAvailability {
		// Build a safe insert from the snapshot.
		cols := make([]string, 0, len(row))
		vals := make([]interface{}, 0, len(row))
		placeholders := make([]string, 0, len(row))
		for k, v := range row {
			cols = append(cols, k)
			vals = append(vals, v)
			placeholders = append(placeholders, "?")
		}
		if len(cols) > 0 {
			query := fmt.Sprintf("INSERT INTO model_availability (%s) VALUES (%s)",
				strings.Join(cols, ", "), strings.Join(placeholders, ", "))
			_, err := tx.Exec(query, vals...)
			if err != nil {
				log.Printf("[oauth] rollback model_availability re-insert failed: %v", err)
			}
		}
	}

	return tx.Commit()
}

// ensureOAuthProviderSite ensures the site for a provider exists.
func ensureOAuthProviderSite(db *store.DB, def *OAuthProviderDefinition) (*store.Site, error) {
	var site store.Site
	err := db.Get(&site,
		"SELECT * FROM sites WHERE platform = ? AND url = ? LIMIT 1",
		def.Site.Platform, def.Site.URL)
	if err == nil {
		return &site, nil
	}

	// Get next sort order.
	var maxSortOrder int64
	db.Get(&maxSortOrder, "SELECT COALESCE(MAX(sort_order), -1) FROM sites")
	sortOrder := maxSortOrder + 1
	now := time.Now().Format(time.RFC3339)

	result, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, use_system_proxy, is_pinned, global_weight, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, 'active', 0, 0, 1, ?, ?, ?)`,
		def.Site.Name, def.Site.URL, def.Site.Platform, sortOrder, now, now,
	)
	if err != nil {
		// Try reading again in case of race.
		err2 := db.Get(&site,
			"SELECT * FROM sites WHERE platform = ? AND url = ? LIMIT 1",
			def.Site.Platform, def.Site.URL)
		if err2 == nil {
			return &site, nil
		}
		return nil, fmt.Errorf("failed to create oauth provider site: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		// Fallback for Postgres.
		db.Get(&id, "SELECT id FROM sites WHERE platform = ? AND url = ? LIMIT 1",
			def.Site.Platform, def.Site.URL)
	}
	site.ID = id
	site.Name = def.Site.Name
	site.URL = def.Site.URL
	site.Platform = def.Site.Platform
	site.Status = "active"
	return &site, nil
}

// findExistingOAuthAccount finds an existing OAuth account for dedup.
func findExistingOAuthAccount(db *store.DB, provider OAuthProviderId, exchange *TokenSet, rebindAccountID int64) *store.Account {
	if rebindAccountID > 0 {
		var account store.Account
		err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", rebindAccountID)
		if err == nil {
			return &account
		}
		return nil
	}

	accountKey := exchange.AccountKey
	if accountKey == "" {
		accountKey = exchange.AccountID
	}

	if accountKey != "" {
		var account store.Account
		projectID := strings.TrimSpace(exchange.ProjectID)
		var err error
		if projectID != "" {
			err = db.Get(&account,
				"SELECT * FROM accounts WHERE oauth_provider = ? AND oauth_account_key = ? AND oauth_project_id = ? LIMIT 1",
				string(provider), accountKey, projectID)
		} else {
			err = db.Get(&account,
				"SELECT * FROM accounts WHERE oauth_provider = ? AND oauth_account_key = ? AND (oauth_project_id IS NULL OR oauth_project_id = '') LIMIT 1",
				string(provider), accountKey)
		}
		if err == nil {
			return &account
		}
	}

	email := exchange.Email
	if accountKey == "" && email != "" && provider != ProviderCodex {
		var account store.Account
		err := db.Get(&account,
			"SELECT * FROM accounts WHERE oauth_provider = ? AND username = ? LIMIT 1",
			string(provider), email)
		if err == nil {
			return &account
		}
	}

	return nil
}

// resolveOauthProviderProxyUrl returns the provider-specific default proxy URL.
// Each provider may have its own proxy configuration.
func resolveOauthProviderProxyUrl(provider string) *string {
	cfg := config.Get()
	switch provider {
	case "codex":
		// Codex provider uses system proxy if configured.
		if cfg.SystemProxyUrl != "" {
			return &cfg.SystemProxyUrl
		}
	case "claude":
		if cfg.SystemProxyUrl != "" {
			return &cfg.SystemProxyUrl
		}
	case "gemini-cli":
		if cfg.SystemProxyUrl != "" {
			return &cfg.SystemProxyUrl
		}
	case "antigravity":
		if cfg.SystemProxyUrl != "" {
			return &cfg.SystemProxyUrl
		}
	}
	return nil
}

// ---- List OAuth Providers ----

// ListOauthProviders returns provider metadata with callback server state.
func ListOauthProviders() []ProviderMetadata {
	defs := ListProviderDefinitions()
	result := make([]ProviderMetadata, len(defs))
	for i, def := range defs {
		meta := def.Metadata
		state := GetLoopbackCallbackServerState(string(def.Metadata.Provider))
		if state != nil {
			meta.Enabled = state.Ready || !state.Attempted
		}
		result[i] = meta
	}
	return result
}

func init() {
	// Ensure sqlx import is used.
	_ = sqlx.BindType("")
}
