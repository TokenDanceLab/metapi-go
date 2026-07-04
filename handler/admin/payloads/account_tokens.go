package payloads

// AccountTokenCreatePayload mirrors TS AccountTokenCreatePayload.
type AccountTokenCreatePayload struct {
	AccountID          int     `json:"accountId"`
	Name               *string `json:"name,omitempty"`
	Token              *string `json:"token,omitempty"`
	Enabled            *bool   `json:"enabled,omitempty"`
	IsDefault          *bool   `json:"isDefault,omitempty"`
	Source             *string `json:"source,omitempty"`
	Group              *string `json:"group,omitempty"`
	UnlimitedQuota     *bool   `json:"unlimitedQuota,omitempty"`
	RemainQuota        any     `json:"remainQuota,omitempty"`
	ExpiredTime        any     `json:"expiredTime,omitempty"`
	AllowIPs           *string `json:"allowIps,omitempty"`
	ModelLimitsEnabled *bool   `json:"modelLimitsEnabled,omitempty"`
	ModelLimits        *string `json:"modelLimits,omitempty"`
}

// AccountTokenUpdatePayload mirrors TS AccountTokenUpdatePayload.
type AccountTokenUpdatePayload struct {
	Name      *string `json:"name,omitempty"`
	Token     *string `json:"token,omitempty"`
	Group     *string `json:"group,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
	IsDefault *bool   `json:"isDefault,omitempty"`
	Source    *string `json:"source,omitempty"`
}

// AccountTokenBatchPayload mirrors TS AccountTokenBatchPayload.
type AccountTokenBatchPayload struct {
	IDs    []int  `json:"ids"`
	Action string `json:"action"`
}

// AccountTokenSyncAllPayload mirrors TS AccountTokenSyncAllPayload.
type AccountTokenSyncAllPayload struct {
	Wait *bool `json:"wait,omitempty"`
}
