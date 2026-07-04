package platform

import (
	"context"
	"strings"
)

// AnyRouterAdapter extends NewApiAdapter with URL-keyword detect.
// All other methods are fully inherited from NewApiAdapter.
type AnyRouterAdapter struct {
	*NewApiAdapter
}

func init() {
	Register(&AnyRouterAdapter{NewApiAdapter: &NewApiAdapter{BaseAdapter: NewBaseAdapter("anyrouter")}})
}

// Detect matches URL keyword "anyrouter" (case-insensitive).
func (a *AnyRouterAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return strings.Contains(strings.ToLower(url), "anyrouter"), nil
}
