package store

// ---- Table 1: sites ----
type Site struct {
	ID                               int64   `db:"id"`
	Name                             string  `db:"name"`
	URL                              string  `db:"url"`
	ExternalCheckinURL               *string `db:"external_checkin_url"`
	Platform                         string  `db:"platform"`
	ProxyURL                         *string `db:"proxy_url"`
	UseSystemProxy                   bool    `db:"use_system_proxy"`
	CustomHeaders                    *string `db:"custom_headers"`
	Status                           string  `db:"status"`
	IsPinned                         bool    `db:"is_pinned"`
	SortOrder                        int64   `db:"sort_order"`
	GlobalWeight                     float64 `db:"global_weight"`
	APIKey                           *string `db:"api_key"`
	PostRefreshProbeEnabled          bool    `db:"post_refresh_probe_enabled"`
	PostRefreshProbeModel            string  `db:"post_refresh_probe_model"`
	PostRefreshProbeScope            string  `db:"post_refresh_probe_scope"`
	PostRefreshProbeLatencyThresholdMs int64  `db:"post_refresh_probe_latency_threshold_ms"`
	CreatedAt                        string  `db:"created_at"`
	UpdatedAt                        string  `db:"updated_at"`
}

// ---- Table 2: site_api_endpoints ----
type SiteAPIEndpoint struct {
	ID                 int64   `db:"id"`
	SiteID             int64   `db:"site_id"`
	URL                string  `db:"url"`
	Enabled            bool    `db:"enabled"`
	SortOrder          int64   `db:"sort_order"`
	CooldownUntil      *string `db:"cooldown_until"`
	LastSelectedAt     *string `db:"last_selected_at"`
	LastFailedAt       *string `db:"last_failed_at"`
	LastFailureReason  *string `db:"last_failure_reason"`
	CreatedAt          string  `db:"created_at"`
	UpdatedAt          string  `db:"updated_at"`
}

// ---- Table 3: site_disabled_models ----
type SiteDisabledModel struct {
	ID        int64  `db:"id"`
	SiteID    int64  `db:"site_id"`
	ModelName string `db:"model_name"`
	CreatedAt string `db:"created_at"`
}

// ---- Table 4: accounts ----
type Account struct {
	ID                  int64   `db:"id"`
	SiteID              int64   `db:"site_id"`
	Username            *string `db:"username"`
	AccessToken         string  `db:"access_token"`
	APIToken            *string `db:"api_token"`
	Balance             float64 `db:"balance"`
	BalanceUsed         float64 `db:"balance_used"`
	Quota               float64 `db:"quota"`
	UnitCost            *float64 `db:"unit_cost"`
	ValueScore          float64 `db:"value_score"`
	Status              string  `db:"status"`
	IsPinned            bool    `db:"is_pinned"`
	SortOrder           int64   `db:"sort_order"`
	CheckinEnabled      bool    `db:"checkin_enabled"`
	LastCheckinAt       *string `db:"last_checkin_at"`
	LastBalanceRefresh  *string `db:"last_balance_refresh"`
	OAuthProvider       *string `db:"oauth_provider"`
	OAuthAccountKey     *string `db:"oauth_account_key"`
	OAuthProjectID      *string `db:"oauth_project_id"`
	ExtraConfig         *string `db:"extra_config"`
	CreatedAt           string  `db:"created_at"`
	UpdatedAt           string  `db:"updated_at"`
}

// ---- Table 5: account_tokens ----
type AccountToken struct {
	ID          int64   `db:"id"`
	AccountID   int64   `db:"account_id"`
	Name        string  `db:"name"`
	Token       string  `db:"token"`
	TokenGroup  *string `db:"token_group"`
	ValueStatus string  `db:"value_status"`
	Source      string  `db:"source"`
	Enabled     bool    `db:"enabled"`
	IsDefault   bool    `db:"is_default"`
	CreatedAt   string  `db:"created_at"`
	UpdatedAt   string  `db:"updated_at"`
}

// ---- Table 6: checkin_logs ----
type CheckinLog struct {
	ID        int64   `db:"id"`
	AccountID int64   `db:"account_id"`
	Status    string  `db:"status"`
	Message   *string `db:"message"`
	Reward    *string `db:"reward"`
	CreatedAt string  `db:"created_at"`
}

// ---- Table 7: model_availability ----
type ModelAvailability struct {
	ID         int64   `db:"id"`
	AccountID  int64   `db:"account_id"`
	ModelName  string  `db:"model_name"`
	Available  *bool   `db:"available"`
	IsManual   bool    `db:"is_manual"`
	LatencyMs  *int64  `db:"latency_ms"`
	CheckedAt  string  `db:"checked_at"`
}

// ---- Table 8: token_model_availability ----
type TokenModelAvailability struct {
	ID        int64  `db:"id"`
	TokenID   int64  `db:"token_id"`
	ModelName string `db:"model_name"`
	Available *bool  `db:"available"`
	LatencyMs *int64 `db:"latency_ms"`
	CheckedAt string `db:"checked_at"`
}

// ---- Table 9: token_routes ----
type TokenRoute struct {
	ID                  int64   `db:"id"`
	ModelPattern        string  `db:"model_pattern"`
	DisplayName         *string `db:"display_name"`
	DisplayIcon         *string `db:"display_icon"`
	RouteMode           string  `db:"route_mode"`
	ModelMapping        *string `db:"model_mapping"`
	DecisionSnapshot    *string `db:"decision_snapshot"`
	DecisionRefreshedAt *string `db:"decision_refreshed_at"`
	RoutingStrategy     string  `db:"routing_strategy"`
	Enabled             bool    `db:"enabled"`
	CreatedAt           string  `db:"created_at"`
	UpdatedAt           string  `db:"updated_at"`
}

// ---- Table 10: route_group_sources ----
type RouteGroupSource struct {
	ID            int64 `db:"id"`
	GroupRouteID  int64 `db:"group_route_id"`
	SourceRouteID int64 `db:"source_route_id"`
}

// ---- Table 11: oauth_route_units ----
type OAuthRouteUnit struct {
	ID        int64  `db:"id"`
	SiteID    int64  `db:"site_id"`
	Provider  string `db:"provider"`
	Name      string `db:"name"`
	Strategy  string `db:"strategy"`
	Enabled   bool   `db:"enabled"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// ---- Table 12: oauth_route_unit_members ----
type OAuthRouteUnitMember struct {
	ID                   int64   `db:"id"`
	UnitID               int64   `db:"unit_id"`
	AccountID            int64   `db:"account_id"`
	SortOrder            int64   `db:"sort_order"`
	SuccessCount         int64   `db:"success_count"`
	FailCount            int64   `db:"fail_count"`
	TotalLatencyMs       int64   `db:"total_latency_ms"`
	TotalCost            float64 `db:"total_cost"`
	LastUsedAt           *string `db:"last_used_at"`
	LastSelectedAt       *string `db:"last_selected_at"`
	LastFailAt           *string `db:"last_fail_at"`
	ConsecutiveFailCount int64   `db:"consecutive_fail_count"`
	CooldownLevel        int64   `db:"cooldown_level"`
	CooldownUntil        *string `db:"cooldown_until"`
	CreatedAt            string  `db:"created_at"`
	UpdatedAt            string  `db:"updated_at"`
}

// ---- Table 13: route_channels ----
type RouteChannel struct {
	ID                  int64    `db:"id"`
	RouteID             int64    `db:"route_id"`
	AccountID           int64    `db:"account_id"`
	TokenID             *int64   `db:"token_id"`
	OAuthRouteUnitID    *int64   `db:"oauth_route_unit_id"`
	SourceModel         *string  `db:"source_model"`
	Priority            int64    `db:"priority"`
	Weight              int64    `db:"weight"`
	Enabled             bool     `db:"enabled"`
	ManualOverride      bool     `db:"manual_override"`
	SuccessCount        int64    `db:"success_count"`
	FailCount           int64    `db:"fail_count"`
	TotalLatencyMs      int64    `db:"total_latency_ms"`
	TotalCost           float64  `db:"total_cost"`
	LastUsedAt          *string  `db:"last_used_at"`
	LastSelectedAt      *string  `db:"last_selected_at"`
	LastFailAt          *string  `db:"last_fail_at"`
	ConsecutiveFailCount int64   `db:"consecutive_fail_count"`
	CooldownLevel       int64    `db:"cooldown_level"`
	CooldownUntil       *string  `db:"cooldown_until"`
}

// ---- Table 14: proxy_logs ----
type ProxyLog struct {
	ID                   int64    `db:"id"`
	RouteID              *int64   `db:"route_id"`
	ChannelID            *int64   `db:"channel_id"`
	AccountID            *int64   `db:"account_id"`
	DownstreamAPIKeyID   *int64   `db:"downstream_api_key_id"`
	ModelRequested       *string  `db:"model_requested"`
	ModelActual          *string  `db:"model_actual"`
	Status               *string  `db:"status"`
	HTTPStatus           *int64   `db:"http_status"`
	IsStream             *bool    `db:"is_stream"`
	FirstByteLatencyMs   *int64   `db:"first_byte_latency_ms"`
	LatencyMs            *int64   `db:"latency_ms"`
	PromptTokens         *int64   `db:"prompt_tokens"`
	CompletionTokens     *int64   `db:"completion_tokens"`
	TotalTokens          *int64   `db:"total_tokens"`
	EstimatedCost        *float64 `db:"estimated_cost"`
	BillingDetails       *string  `db:"billing_details"`
	ClientFamily         *string  `db:"client_family"`
	ClientAppID          *string  `db:"client_app_id"`
	ClientAppName        *string  `db:"client_app_name"`
	ClientConfidence     *string  `db:"client_confidence"`
	ErrorMessage         *string  `db:"error_message"`
	RetryCount           int64    `db:"retry_count"`
	CreatedAt            string   `db:"created_at"`
}

// ---- Table 15: proxy_debug_traces ----
type ProxyDebugTrace struct {
	ID                         int64   `db:"id"`
	DownstreamPath             string  `db:"downstream_path"`
	ClientKind                 *string `db:"client_kind"`
	SessionID                  *string `db:"session_id"`
	TraceHint                  *string `db:"trace_hint"`
	RequestedModel             *string `db:"requested_model"`
	DownstreamAPIKeyID         *int64  `db:"downstream_api_key_id"`
	RequestHeadersJSON         *string `db:"request_headers_json"`
	RequestBodyJSON            *string `db:"request_body_json"`
	StickySessionKey           *string `db:"sticky_session_key"`
	StickyHitChannelID         *int64  `db:"sticky_hit_channel_id"`
	SelectedChannelID          *int64  `db:"selected_channel_id"`
	SelectedRouteID            *int64  `db:"selected_route_id"`
	SelectedAccountID          *int64  `db:"selected_account_id"`
	SelectedSiteID             *int64  `db:"selected_site_id"`
	SelectedSitePlatform       *string `db:"selected_site_platform"`
	EndpointCandidatesJSON     *string `db:"endpoint_candidates_json"`
	EndpointRuntimeStateJSON   *string `db:"endpoint_runtime_state_json"`
	DecisionSummaryJSON        *string `db:"decision_summary_json"`
	FinalStatus                *string `db:"final_status"`
	FinalHTTPStatus            *int64  `db:"final_http_status"`
	FinalUpstreamPath          *string `db:"final_upstream_path"`
	FinalResponseHeadersJSON   *string `db:"final_response_headers_json"`
	FinalResponseBodyJSON      *string `db:"final_response_body_json"`
	CreatedAt                  string  `db:"created_at"`
	UpdatedAt                  string  `db:"updated_at"`
}

// ---- Table 16: proxy_debug_attempts ----
type ProxyDebugAttempt struct {
	ID                  int64   `db:"id"`
	TraceID             int64   `db:"trace_id"`
	AttemptIndex        int64   `db:"attempt_index"`
	Endpoint            string  `db:"endpoint"`
	RequestPath         string  `db:"request_path"`
	TargetURL           string  `db:"target_url"`
	RuntimeExecutor     *string `db:"runtime_executor"`
	RequestHeadersJSON  *string `db:"request_headers_json"`
	RequestBodyJSON     *string `db:"request_body_json"`
	ResponseStatus      *int64  `db:"response_status"`
	ResponseHeadersJSON *string `db:"response_headers_json"`
	ResponseBodyJSON    *string `db:"response_body_json"`
	RawErrorText        *string `db:"raw_error_text"`
	RecoverApplied      bool    `db:"recover_applied"`
	DowngradeDecision   bool    `db:"downgrade_decision"`
	DowngradeReason     *string `db:"downgrade_reason"`
	MemoryWriteJSON     *string `db:"memory_write_json"`
	CreatedAt           string  `db:"created_at"`
}

// ---- Table 17: proxy_video_tasks ----
type ProxyVideoTask struct {
	ID                    int64   `db:"id"`
	PublicID              string  `db:"public_id"`
	UpstreamVideoID       string  `db:"upstream_video_id"`
	SiteURL               string  `db:"site_url"`
	TokenValue            string  `db:"token_value"`
	RequestedModel        *string `db:"requested_model"`
	ActualModel           *string `db:"actual_model"`
	ChannelID             *int64  `db:"channel_id"`
	AccountID             *int64  `db:"account_id"`
	StatusSnapshot        *string `db:"status_snapshot"`
	UpstreamResponseMeta  *string `db:"upstream_response_meta"`
	LastUpstreamStatus    *int64  `db:"last_upstream_status"`
	LastPolledAt          *string `db:"last_polled_at"`
	CreatedAt             string  `db:"created_at"`
	UpdatedAt             string  `db:"updated_at"`
}

// ---- Table 18: proxy_files ----
type ProxyFile struct {
	ID            int64   `db:"id"`
	PublicID      string  `db:"public_id"`
	OwnerType     string  `db:"owner_type"`
	OwnerID       string  `db:"owner_id"`
	Filename      string  `db:"filename"`
	MimeType      string  `db:"mime_type"`
	Purpose       *string `db:"purpose"`
	ByteSize      int64   `db:"byte_size"`
	SHA256        string  `db:"sha256"`
	ContentBase64 string  `db:"content_base64"`
	CreatedAt     string  `db:"created_at"`
	UpdatedAt     string  `db:"updated_at"`
	DeletedAt     *string `db:"deleted_at"`
}

// ---- Table 19: settings (text PK) ----
type Setting struct {
	Key   string  `db:"key"`
	Value *string `db:"value"`
}

// ---- Table 20: admin_snapshots ----
type AdminSnapshot struct {
	ID          int64  `db:"id"`
	Namespace   string `db:"namespace"`
	SnapshotKey string `db:"snapshot_key"`
	Payload     string `db:"payload"`
	GeneratedAt string `db:"generated_at"`
	ExpiresAt   string `db:"expires_at"`
	StaleUntil  string `db:"stale_until"`
	CreatedAt   string `db:"created_at"`
	UpdatedAt   string `db:"updated_at"`
}

// ---- Table 21: analytics_projection_checkpoints (text PK) ----
type AnalyticsProjectionCheckpoint struct {
	ProjectorKey         string  `db:"projector_key"`
	TimeZone             string  `db:"time_zone"`
	LastProxyLogID       int64   `db:"last_proxy_log_id"`
	WatermarkCreatedAt   *string `db:"watermark_created_at"`
	LeaseOwner           *string `db:"lease_owner"`
	LeaseToken           *string `db:"lease_token"`
	LeaseExpiresAt       *string `db:"lease_expires_at"`
	RecomputeFromID       *int64  `db:"recompute_from_id"`
	RecomputeRequestedAt *string `db:"recompute_requested_at"`
	RecomputeReason      *string `db:"recompute_reason"`
	RecomputeStartedAt   *string `db:"recompute_started_at"`
	RecomputeCompletedAt *string `db:"recompute_completed_at"`
	LastProjectedAt      *string `db:"last_projected_at"`
	LastSuccessfulAt     *string `db:"last_successful_at"`
	LastError            *string `db:"last_error"`
	CreatedAt            string  `db:"created_at"`
	UpdatedAt            string  `db:"updated_at"`
}

// ---- Table 22: site_day_usage ----
type SiteDayUsage struct {
	ID                int64   `db:"id"`
	LocalDay          string  `db:"local_day"`
	SiteID            int64   `db:"site_id"`
	TotalCalls        int64   `db:"total_calls"`
	SuccessCalls      int64   `db:"success_calls"`
	FailedCalls       int64   `db:"failed_calls"`
	TotalTokens       int64   `db:"total_tokens"`
	TotalSummarySpend float64 `db:"total_summary_spend"`
	TotalSiteSpend    float64 `db:"total_site_spend"`
	TotalLatencyMs    int64   `db:"total_latency_ms"`
	LatencyCount      int64   `db:"latency_count"`
	CreatedAt         string  `db:"created_at"`
	UpdatedAt         string  `db:"updated_at"`
}

// ---- Table 23: site_hour_usage ----
type SiteHourUsage struct {
	ID                int64   `db:"id"`
	BucketStartUTC    string  `db:"bucket_start_utc"`
	SiteID            int64   `db:"site_id"`
	TotalCalls        int64   `db:"total_calls"`
	SuccessCalls      int64   `db:"success_calls"`
	FailedCalls       int64   `db:"failed_calls"`
	TotalTokens       int64   `db:"total_tokens"`
	TotalSummarySpend float64 `db:"total_summary_spend"`
	TotalSiteSpend    float64 `db:"total_site_spend"`
	TotalLatencyMs    int64   `db:"total_latency_ms"`
	LatencyCount      int64   `db:"latency_count"`
	CreatedAt         string  `db:"created_at"`
	UpdatedAt         string  `db:"updated_at"`
}

// ---- Table 24: model_day_usage ----
type ModelDayUsage struct {
	ID             int64   `db:"id"`
	LocalDay       string  `db:"local_day"`
	SiteID         int64   `db:"site_id"`
	Model          string  `db:"model"`
	TotalCalls     int64   `db:"total_calls"`
	SuccessCalls   int64   `db:"success_calls"`
	FailedCalls    int64   `db:"failed_calls"`
	TotalTokens    int64   `db:"total_tokens"`
	TotalSpend     float64 `db:"total_spend"`
	TotalLatencyMs int64   `db:"total_latency_ms"`
	LatencyCount   int64   `db:"latency_count"`
	CreatedAt      string  `db:"created_at"`
	UpdatedAt      string  `db:"updated_at"`
}

// ---- Table 25: downstream_api_keys ----
type DownstreamAPIKey struct {
	ID                      int64    `db:"id"`
	Name                    string   `db:"name"`
	Key                     string   `db:"key"`
	Description             *string  `db:"description"`
	GroupName               *string  `db:"group_name"`
	Tags                    *string  `db:"tags"`
	Enabled                 bool     `db:"enabled"`
	ExpiresAt               *string  `db:"expires_at"`
	MaxCost                 *float64 `db:"max_cost"`
	UsedCost                float64  `db:"used_cost"`
	MaxRequests             *int64   `db:"max_requests"`
	UsedRequests            int64    `db:"used_requests"`
	SupportedModels         *string  `db:"supported_models"`
	AllowedRouteIDs         *string  `db:"allowed_route_ids"`
	SiteWeightMultipliers   *string  `db:"site_weight_multipliers"`
	ExcludedSiteIDs         *string  `db:"excluded_site_ids"`
	ExcludedCredentialRefs  *string  `db:"excluded_credential_refs"`
	LastUsedAt              *string  `db:"last_used_at"`
	CreatedAt               string   `db:"created_at"`
	UpdatedAt               string   `db:"updated_at"`
}

// ---- Table 26: site_announcements ----
type SiteAnnouncement struct {
	ID                 int64   `db:"id"`
	SiteID             int64   `db:"site_id"`
	Platform           string  `db:"platform"`
	SourceKey          string  `db:"source_key"`
	Title              string  `db:"title"`
	Content            string  `db:"content"`
	Level              string  `db:"level"`
	SourceURL          *string `db:"source_url"`
	StartsAt           *string `db:"starts_at"`
	EndsAt             *string `db:"ends_at"`
	UpstreamCreatedAt  *string `db:"upstream_created_at"`
	UpstreamUpdatedAt  *string `db:"upstream_updated_at"`
	FirstSeenAt        string  `db:"first_seen_at"`
	LastSeenAt         string  `db:"last_seen_at"`
	ReadAt             *string `db:"read_at"`
	DismissedAt        *string `db:"dismissed_at"`
	RawPayload         *string `db:"raw_payload"`
	CreatedAt          string  `db:"created_at"`
	UpdatedAt          string  `db:"updated_at"`
}

// ---- Table 27: events ----
type Event struct {
	ID          int64   `db:"id"`
	Type        string  `db:"type"`
	Title       string  `db:"title"`
	Message     *string `db:"message"`
	Level       string  `db:"level"`
	Read        bool    `db:"read"`
	RelatedID   *int64  `db:"related_id"`
	RelatedType *string `db:"related_type"`
	CreatedAt   string  `db:"created_at"`
}
