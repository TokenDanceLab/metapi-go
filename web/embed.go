// Package web bundles the built React SPA frontend via go:embed.
// Build before compile (from repo root):
//
//	cd web && npm ci && npm run build:web
//
// Output lands in web/dist/ and is embedded by //go:embed dist.
package web

import "embed"

// Dist contains the built frontend assets (web/dist/).
//
//go:embed dist
var Dist embed.FS
