package payloads

// SiteCreatePayload mirrors TS SiteCreatePayload (siteRoutePayloads.ts).
type SiteCreatePayload struct {
	Name                   string   `json:"name"`
	URL                    string   `json:"url"`
	Platform               *string  `json:"platform,omitempty"`
	InitializationPresetID *string  `json:"initializationPresetId,omitempty"`
	ProxyURL               *string  `json:"proxyUrl,omitempty"`
	UseSystemProxy         *bool    `json:"useSystemProxy,omitempty"`
	CustomHeaders          *string  `json:"customHeaders,omitempty"`
	ExternalCheckinURL     *string  `json:"externalCheckinUrl,omitempty"`
	Status                 *string  `json:"status,omitempty"`
	IsPinned               *bool    `json:"isPinned,omitempty"`
	SortOrder              *int     `json:"sortOrder,omitempty"`
	GlobalWeight           *float64 `json:"globalWeight,omitempty"`
	// MaxConcurrency caps concurrent upstream calls for this site (0 = unlimited).
	MaxConcurrency *int64                 `json:"maxConcurrency,omitempty"`
	APIEndpoints   []SiteAPIEndpointInput `json:"apiEndpoints,omitempty"`
}

// SiteAPIEndpointInput is an embedded sub-resource input for apiEndpoints.
type SiteAPIEndpointInput struct {
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	SortOrder int    `json:"sortOrder"`
}

// SiteUpdatePayload mirrors TS SiteUpdatePayload.
type SiteUpdatePayload struct {
	Name               *string  `json:"name,omitempty"`
	URL                *string  `json:"url,omitempty"`
	Platform           *string  `json:"platform,omitempty"`
	ProxyURL           *string  `json:"proxyUrl,omitempty"`
	UseSystemProxy     *bool    `json:"useSystemProxy,omitempty"`
	CustomHeaders      *string  `json:"customHeaders,omitempty"`
	ExternalCheckinURL *string  `json:"externalCheckinUrl,omitempty"`
	Status             *string  `json:"status,omitempty"`
	IsPinned           *bool    `json:"isPinned,omitempty"`
	SortOrder          *int     `json:"sortOrder,omitempty"`
	GlobalWeight       *float64 `json:"globalWeight,omitempty"`
	// MaxConcurrency caps concurrent upstream calls for this site (0 = unlimited).
	MaxConcurrency                     *int64                 `json:"maxConcurrency,omitempty"`
	APIEndpoints                       []SiteAPIEndpointInput `json:"apiEndpoints,omitempty"`
	PostRefreshProbeEnabled            *bool                  `json:"postRefreshProbeEnabled,omitempty"`
	PostRefreshProbeModel              *string                `json:"postRefreshProbeModel,omitempty"`
	PostRefreshProbeScope              *string                `json:"postRefreshProbeScope,omitempty"`
	PostRefreshProbeLatencyThresholdMs *int                   `json:"postRefreshProbeLatencyThresholdMs,omitempty"`
}

// SiteBatchPayload mirrors TS SiteBatchPayload.
type SiteBatchPayload struct {
	IDs    []int  `json:"ids"`
	Action string `json:"action"`
}

// SiteDetectPayload mirrors TS SiteDetectPayload.
type SiteDetectPayload struct {
	URL string `json:"url"`
}

// SiteDisabledModelsPayload mirrors TS SiteDisabledModelsPayload.
type SiteDisabledModelsPayload struct {
	Models []string `json:"models"`
}

// ProbeNowBody is the JSON body for POST /api/sites/:id/probe-now.
type ProbeNowBody struct {
	Scope              *string `json:"scope,omitempty"`
	ModelName          *string `json:"modelName,omitempty"`
	LatencyThresholdMs *int    `json:"latencyThresholdMs,omitempty"`
}
