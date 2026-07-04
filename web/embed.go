// Package web bundles the built React SPA frontend via go:embed.
// The frontend must be built from the metapi TypeScript project before
// compilation: cd ../metapi && npm run build:web && cp -r dist/web/ ../metapi-go/web/dist/
package web

import "embed"

// Dist contains the built frontend assets (web/dist/).
//
//go:embed dist
var Dist embed.FS
