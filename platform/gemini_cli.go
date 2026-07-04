package platform

import (
	"context"
	"strings"
)

// GeminiCliAdapter extends GeminiAdapter with its own detect for cloudcode-pa.googleapis.com.
type GeminiCliAdapter struct {
	*GeminiAdapter
}

func init() {
	Register(&GeminiCliAdapter{GeminiAdapter: &GeminiAdapter{StandardAdapter: NewStandardAdapter("gemini-cli")}})
}

// Detect overrides GeminiAdapter's detect: matches cloudcode-pa.googleapis.com only.
func (g *GeminiCliAdapter) Detect(ctx context.Context, url string) (bool, error) {
	return strings.Contains(strings.ToLower(url), "cloudcode-pa.googleapis.com"), nil
}
