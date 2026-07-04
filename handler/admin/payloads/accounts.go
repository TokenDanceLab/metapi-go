package payloads

// AccountCreatePayload mirrors TS AccountCreatePayload.
type AccountCreatePayload struct {
	SiteID         int      `json:"siteId"`
	Username       *string  `json:"username,omitempty"`
	AccessToken    *string  `json:"accessToken,omitempty"`
	AccessTokens   []string `json:"accessTokens,omitempty"`
	APIToken       *string  `json:"apiToken,omitempty"`
	PlatformUserID *int     `json:"platformUserId,omitempty"`
	CheckinEnabled *bool    `json:"checkinEnabled,omitempty"`
	CredentialMode *string  `json:"credentialMode,omitempty"`
	RefreshToken   *string  `json:"refreshToken,omitempty"`
	TokenExpiresAt *int64   `json:"tokenExpiresAt,omitempty"`
	SkipModelFetch *bool    `json:"skipModelFetch,omitempty"`
}

// AccountUpdatePayload mirrors TS AccountUpdatePayload.
type AccountUpdatePayload struct {
	Username       *string  `json:"username,omitempty"`
	AccessToken    *string  `json:"accessToken,omitempty"`
	APIToken       any      `json:"apiToken,omitempty"`
	Status         *string  `json:"status,omitempty"`
	CheckinEnabled *bool    `json:"checkinEnabled,omitempty"`
	UnitCost       *float64 `json:"unitCost,omitempty"`
	ExtraConfig    any      `json:"extraConfig,omitempty"`
	RefreshToken   *string  `json:"refreshToken,omitempty"`
	TokenExpiresAt *int64   `json:"tokenExpiresAt,omitempty"`
	IsPinned       *bool    `json:"isPinned,omitempty"`
	SortOrder      *int     `json:"sortOrder,omitempty"`
	ProxyURL       *string  `json:"proxyUrl,omitempty"`
}

// AccountBatchPayload mirrors TS AccountBatchPayload.
type AccountBatchPayload struct {
	IDs    []int  `json:"ids"`
	Action string `json:"action"`
}

// AccountLoginPayload mirrors TS AccountLoginPayload.
type AccountLoginPayload struct {
	SiteID   int    `json:"siteId"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AccountVerifyTokenPayload mirrors TS AccountVerifyTokenPayload.
type AccountVerifyTokenPayload struct {
	SiteID         int     `json:"siteId"`
	AccessToken    *string `json:"accessToken,omitempty"`
	PlatformUserID *int    `json:"platformUserId,omitempty"`
	CredentialMode *string `json:"credentialMode,omitempty"`
}

// AccountRebindSessionPayload mirrors TS AccountRebindSessionPayload.
type AccountRebindSessionPayload struct {
	AccessToken    *string `json:"accessToken,omitempty"`
	PlatformUserID *int    `json:"platformUserId,omitempty"`
	RefreshToken   *string `json:"refreshToken,omitempty"`
	TokenExpiresAt *int64  `json:"tokenExpiresAt,omitempty"`
}

// AccountHealthRefreshPayload mirrors TS AccountHealthRefreshPayload.
type AccountHealthRefreshPayload struct {
	AccountID *int  `json:"accountId,omitempty"`
	Wait      *bool `json:"wait,omitempty"`
}

// AccountManualModelsPayload mirrors TS AccountManualModelsPayload.
type AccountManualModelsPayload struct {
	Models []string `json:"models"`
}
