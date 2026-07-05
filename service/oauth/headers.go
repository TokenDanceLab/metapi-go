package oauth

import (
	"github.com/tokendancelab/metapi-go/store"
)

// BuildOauthProviderHeaders constructs provider-specific proxy headers for an OAuth account.
func BuildOauthProviderHeaders(account *store.Account, extraConfig *string, downstreamHeaders map[string]interface{}) map[string]string {
	oauth := GetOauthInfoFromAccount(account)
	if oauth == nil {
		return map[string]string{}
	}

	def := GetProviderDefinition(oauth.Provider)
	if def == nil || def.BuildProxyHeaders == nil {
		return map[string]string{}
	}

	return def.BuildProxyHeaders(nil, ProxyHeaderInput{
		OAuth: ProxyHeaderOAuth{
			Provider:     oauth.Provider,
			AccountKey:   oauth.AccountKey,
			AccountID:    oauth.AccountID,
			ProjectID:    oauth.ProjectID,
			ProviderData: oauth.ProviderData,
		},
		DownstreamHeaders: downstreamHeaders,
	})
}

// BuildCodexOauthProviderHeaders builds headers specifically for Codex provider.
func BuildCodexOauthProviderHeaders(extraConfig *string, downstreamHeaders map[string]interface{}) map[string]string {
	oauth, err := BuildOauthInfo(extraConfig, &OauthInfo{Provider: string(ProviderCodex)})
	if err != nil {
		return map[string]string{}
	}
	def := GetProviderDefinition(string(ProviderCodex))
	if def == nil || def.BuildProxyHeaders == nil {
		return map[string]string{}
	}

	return def.BuildProxyHeaders(nil, ProxyHeaderInput{
		OAuth: ProxyHeaderOAuth{
			Provider:     oauth.Provider,
			AccountKey:   oauth.AccountKey,
			AccountID:    oauth.AccountID,
			ProjectID:    oauth.ProjectID,
			ProviderData: oauth.ProviderData,
		},
		DownstreamHeaders: downstreamHeaders,
	})
}
