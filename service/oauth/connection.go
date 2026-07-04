package oauth

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"context"

	"github.com/tokendancelab/metapi-go/store"
)

// OauthConnectionItem represents an OAuth connection in list responses.
type OauthConnectionItem struct {
	AccountID           int64                       `json:"accountId"`
	SiteID              int64                       `json:"siteId"`
	Provider            string                      `json:"provider"`
	Username            string                      `json:"username"`
	Email               string                      `json:"email"`
	AccountKey          string                      `json:"accountKey"`
	PlanType            string                      `json:"planType,omitempty"`
	ProjectID           string                      `json:"projectId,omitempty"`
	ModelCount          int                         `json:"modelCount"`
	ModelsPreview       []string                    `json:"modelsPreview"`
	Quota               *OauthQuotaSnapshot         `json:"quota,omitempty"`
	Status              string                      `json:"status"`
	RouteChannelCount   int                         `json:"routeChannelCount"`
	LastModelSyncAt     string                      `json:"lastModelSyncAt,omitempty"`
	LastModelSyncError  string                      `json:"lastModelSyncError,omitempty"`
	ProxyURL            string                      `json:"proxyUrl,omitempty"`
	UseSystemProxy      bool                        `json:"useSystemProxy"`
	RouteParticipation  *RouteParticipation         `json:"routeParticipation,omitempty"`
	Site                *ConnectionSite             `json:"site,omitempty"`
}

// RouteParticipation represents a route unit participation.
type RouteParticipation struct {
	Kind        string `json:"kind"`
	ID          int64  `json:"id"`
	RouteUnitID int64  `json:"routeUnitId"`
	Name        string `json:"name"`
	Strategy    string `json:"strategy"`
	MemberCount int64  `json:"memberCount"`
}

// ConnectionSite holds site info for a connection.
type ConnectionSite struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Platform string `json:"platform"`
}

// ListConnectionsInput holds input for listing OAuth connections.
type ListConnectionsInput struct {
	Limit  int
	Offset int
}

// ListConnectionsResult holds the result of listing connections.
type ListConnectionsResult struct {
	Items  []OauthConnectionItem `json:"items"`
	Total  int64                 `json:"total"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}

// ListOauthConnections lists OAuth connections with pagination.
func ListOauthConnections(input ListConnectionsInput) (*ListConnectionsResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	limit := clampInt(input.Limit, 1, 200, 50)
	offset := maxInt(input.Offset, 0)

	// Ensure OAuth identity backfill: backfill columns from extraConfig.oauth.
	ensureOauthIdentityBackfill(db)

	var total int64
	db.Get(&total, "SELECT COUNT(*) FROM accounts WHERE oauth_provider IS NOT NULL")

	var rows []struct {
		Account store.Account `db:"accounts"`
		Site    store.Site    `db:"sites"`
	}

	err := db.Select(&rows,
		`SELECT a.*, s.* FROM accounts a
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE a.oauth_provider IS NOT NULL
		 ORDER BY a.id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, err
	}

	accountIDs := make([]int64, len(rows))
	for i, row := range rows {
		accountIDs[i] = row.Account.ID
	}

	if len(accountIDs) == 0 {
		return &ListConnectionsResult{Items: []OauthConnectionItem{}, Total: total, Limit: limit, Offset: offset}, nil
	}

	// Get route unit participation.
	routeUnits := ListOauthRouteUnitsByAccountIDs(accountIDs)

	items := make([]OauthConnectionItem, 0, len(rows))
	for _, row := range rows {
		oauth := GetOauthInfoFromAccount(&row.Account)
		if oauth == nil {
			continue
		}

		status := "healthy"
		if oauth.ModelDiscoveryStatus == OauthModelDiscoveryAbnormal ||
			row.Account.Status != "active" ||
			row.Site.Status != "active" {
			status = "abnormal"
		}

		item := OauthConnectionItem{
			AccountID:  row.Account.ID,
			SiteID:     row.Site.ID,
			Provider:   oauth.Provider,
			Username:   strPtr(row.Account.Username),
			Email:      oauth.Email,
			AccountKey: oauth.AccountKey,
			PlanType:   oauth.PlanType,
			ProjectID:  oauth.ProjectID,
			Quota:      oauth.Quota,
			Status:     status,
			LastModelSyncAt:    oauth.LastModelSyncAt,
			LastModelSyncError: oauth.LastModelSyncError,
			ProxyURL:       GetProxyURLFromExtraConfig(row.Account.ExtraConfig),
			UseSystemProxy: GetUseSystemProxyFromExtraConfig(row.Account.ExtraConfig),
			Site: &ConnectionSite{
				ID:       row.Site.ID,
				Name:     row.Site.Name,
				URL:      row.Site.URL,
				Platform: row.Site.Platform,
			},
		}

		if ru, ok := routeUnits[row.Account.ID]; ok {
			item.RouteParticipation = &RouteParticipation{
				Kind:        "route_unit",
				ID:          ru.ID,
				RouteUnitID: ru.ID,
				Name:        ru.Name,
				Strategy:    string(ru.Strategy),
				MemberCount: ru.MemberCount,
			}
		}

		items = append(items, item)
	}

	return &ListConnectionsResult{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// DeleteOauthConnection deletes an OAuth connection.
func DeleteOauthConnection(accountID int64) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return fmt.Errorf("oauth account not found")
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil {
		return fmt.Errorf("account is not managed by oauth")
	}

	_, err := db.Exec("DELETE FROM accounts WHERE id = ?", accountID)
	if err != nil {
		return err
	}

	// Rebuild routes after deletion.
	hooks := getWorkflowHooks()
	if hooks != nil {
		if rebuildErr := hooks.RebuildRoutesOnly(context.Background()); rebuildErr != nil {
			log.Printf("[oauth] route rebuild failed after deleting connection %d: %v", accountID, rebuildErr)
		}
		hooks.InvalidateTokenRouterCache()
	}

	return nil
}

// UpdateOauthConnectionProxySettings updates proxy settings for an OAuth connection.
func UpdateOauthConnectionProxySettings(accountID int64, proxyURL *string, useSystemProxy *bool) (*UpdateProxyResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return nil, fmt.Errorf("oauth account not found")
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil {
		return nil, fmt.Errorf("account is not managed by oauth")
	}

	patch := make(map[string]interface{})
	if proxyURL != nil {
		if *proxyURL != "" {
			patch["proxyUrl"] = *proxyURL
		} else {
			patch["proxyUrl"] = nil
		}
	}
	if useSystemProxy != nil {
		patch["useSystemProxy"] = *useSystemProxy
	}

	extraConfig := MergeAccountExtraConfig(account.ExtraConfig, patch)
	now := time.Now().Format(time.RFC3339)

	_, err := db.Exec("UPDATE accounts SET extra_config = ?, updated_at = ? WHERE id = ?",
		extraConfig, now, accountID)
	if err != nil {
		return nil, err
	}

	// Refresh models for account (allowInactive=true) and rebuild routes.
	modelRefreshStatus := "success"
	var modelRefreshErrMsg string
	hooks := getWorkflowHooks()
	if hooks != nil {
		if refreshErr := hooks.RefreshModelsForAccount(context.Background(), accountID, true); refreshErr != nil {
			modelRefreshStatus = "error"
			modelRefreshErrMsg = refreshErr.Error()
			log.Printf("[oauth] model refresh failed in UpdateProxySettings for account %d: %v", accountID, refreshErr)
		}
		if rebuildErr := hooks.RebuildRoutesOnly(context.Background()); rebuildErr != nil {
			log.Printf("[oauth] route rebuild failed in UpdateProxySettings for account %d: %v", accountID, rebuildErr)
		}
		hooks.InvalidateTokenRouterCache()
	}

	return &UpdateProxyResult{
		Success:         true,
		AccountID:       accountID,
		ProxyURL:        GetProxyURLFromExtraConfig(extraConfig),
		UseSystemProxy:  GetUseSystemProxyFromExtraConfig(extraConfig),
		RefreshedRoutes: true,
		ModelRefresh: ModelRefreshResult{
			Success:      modelRefreshStatus == "success",
			Status:       modelRefreshStatus,
			ErrorMessage: modelRefreshErrMsg,
		},
	}, nil
}

// UpdateProxyResult holds the result of updating proxy settings.
type UpdateProxyResult struct {
	Success         bool               `json:"success"`
	AccountID       int64              `json:"accountId"`
	ProxyURL        string             `json:"proxyUrl"`
	UseSystemProxy  bool               `json:"useSystemProxy"`
	RefreshedRoutes bool               `json:"refreshedRoutes"`
	ModelRefresh    ModelRefreshResult `json:"modelRefresh"`
}

// ModelRefreshResult holds model refresh status.
type ModelRefreshResult struct {
	Success      bool   `json:"success"`
	Status       string `json:"status"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// StartOauthRebindFlow starts a rebind flow for an existing OAuth account.
func StartOauthRebindFlow(accountID int64, requestOrigin string, proxyURL *string, useSystemProxy *bool) (*FlowStartResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var account store.Account
	if err := db.Get(&account, "SELECT * FROM accounts WHERE id = ?", accountID); err != nil {
		return nil, fmt.Errorf("oauth account not found")
	}

	oauth := GetOauthInfoFromAccount(&account)
	if oauth == nil {
		return nil, fmt.Errorf("account is not managed by oauth")
	}

	resolvedProxyURL := ""
	if proxyURL != nil {
		resolvedProxyURL = *proxyURL
	} else {
		resolvedProxyURL = GetProxyURLFromExtraConfig(account.ExtraConfig)
	}

	resolvedUseSystemProxy := false
	if useSystemProxy != nil {
		resolvedUseSystemProxy = *useSystemProxy
	} else {
		resolvedUseSystemProxy = GetUseSystemProxyFromExtraConfig(account.ExtraConfig)
	}

	return StartFlow(StartFlowInput{
		Provider:        oauth.Provider,
		RebindAccountID: accountID,
		ProjectID:       oauth.ProjectID,
		ProxyURL:        resolvedProxyURL,
		UseSystemProxy:  resolvedUseSystemProxy,
		RequestOrigin:   requestOrigin,
	})
}

// ---- Helpers ----

func clampInt(v, lo, hi, fallback int) int {
	if v <= 0 {
		return fallback
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func strPtr(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func mathTrunc(f float64) int {
	return int(math.Trunc(f))
}

// ensureOauthIdentityBackfill backfills oauth_provider, oauth_account_key, and oauth_project_id
// columns from extraConfig.oauth for accounts that have the data in extraConfig but not in columns.
func ensureOauthIdentityBackfill(db *store.DB) {
	// Find accounts with oauth in extraConfig but missing column fields.
	rows, err := db.Queryx(
		`SELECT id, oauth_provider, oauth_account_key, oauth_project_id, extra_config
		 FROM accounts WHERE extra_config IS NOT NULL AND extra_config != ''`)
	if err != nil {
		return
	}
	defer rows.Close()

	type backfillRow struct {
		ID               int64   `db:"id"`
		OAuthProvider    *string `db:"oauth_provider"`
		OAuthAccountKey  *string `db:"oauth_account_key"`
		OAuthProjectID   *string `db:"oauth_project_id"`
		ExtraConfig      *string `db:"extra_config"`
	}

	for rows.Next() {
		var row backfillRow
		if err := rows.StructScan(&row); err != nil {
			continue
		}

		oauth := GetOauthInfoFromExtraConfig(row.ExtraConfig)
		if oauth == nil {
			continue
		}

		needsUpdate := false
		provider := ""
		accountKey := ""
		projectID := ""

		if (row.OAuthProvider == nil || *row.OAuthProvider == "") && oauth.Provider != "" {
			provider = oauth.Provider
			needsUpdate = true
		}
		if (row.OAuthAccountKey == nil || *row.OAuthAccountKey == "") && oauth.AccountKey != "" {
			accountKey = oauth.AccountKey
			needsUpdate = true
		}
		if (row.OAuthProjectID == nil || *row.OAuthProjectID == "") && oauth.ProjectID != "" {
			projectID = oauth.ProjectID
			needsUpdate = true
		}

		if needsUpdate {
			now := time.Now().Format(time.RFC3339)
			db.Exec(
				`UPDATE accounts SET oauth_provider = COALESCE(NULLIF(?, ''), oauth_provider),
				 oauth_account_key = COALESCE(NULLIF(?, ''), oauth_account_key),
				 oauth_project_id = COALESCE(NULLIF(?, ''), oauth_project_id),
				 updated_at = ?
				 WHERE id = ?`,
				provider, accountKey, projectID, now, row.ID)
		}
	}
}
