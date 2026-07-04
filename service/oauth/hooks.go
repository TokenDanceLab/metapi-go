package oauth

import "context"

// OAuthWorkflowHooks defines callback hooks that the OAuth service needs from upper layers
// (model refresh, route rebuild, token router cache invalidation).
// The application assembly layer injects an implementation to avoid circular imports
// between the oauth and routing packages.
type OAuthWorkflowHooks interface {
	// RefreshModelsForAccount refreshes model availability for an OAuth account.
	// allowInactive: if true, also refresh for inactive accounts.
	RefreshModelsForAccount(ctx context.Context, accountID int64, allowInactive bool) error

	// RebuildRoutesOnly rebuilds token routes from current model availability data.
	RebuildRoutesOnly(ctx context.Context) error

	// InvalidateTokenRouterCache invalidates the cached routing state so the next
	// proxy request picks up the rebuilt routes.
	InvalidateTokenRouterCache()
}

var workflowHooks OAuthWorkflowHooks

// SetOAuthWorkflowHooks sets the workflow hooks implementation.
// Must be called during application startup before any OAuth operations.
func SetOAuthWorkflowHooks(hooks OAuthWorkflowHooks) {
	workflowHooks = hooks
}

// getWorkflowHooks returns the configured hooks, or nil if not set.
func getWorkflowHooks() OAuthWorkflowHooks {
	return workflowHooks
}
