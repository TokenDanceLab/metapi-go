package oauth

import "sync"

var (
	mu         sync.RWMutex
	byID       = make(map[OAuthProviderId]*OAuthProviderDefinition)
	all        []*OAuthProviderDefinition
	registered bool
)

// RegisterProvider registers a single OAuth provider definition.
// Must be called during initialization.
func RegisterProvider(def *OAuthProviderDefinition) {
	mu.Lock()
	defer mu.Unlock()
	byID[def.Metadata.Provider] = def
	all = append(all, def)
	registered = true
}

// GetProviderDefinition returns the provider definition for the given provider ID.
func GetProviderDefinition(provider string) *OAuthProviderDefinition {
	mu.RLock()
	defer mu.RUnlock()
	return byID[OAuthProviderId(provider)]
}

// ListProviderDefinitions returns a copy of all registered provider definitions.
func ListProviderDefinitions() []*OAuthProviderDefinition {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]*OAuthProviderDefinition, len(all))
	copy(result, all)
	return result
}

// IsRegisteredProvider returns true if the provider ID is registered.
func IsRegisteredProvider(provider string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := byID[OAuthProviderId(provider)]
	return ok
}
