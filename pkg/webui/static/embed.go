// Package static provides embedded static assets for the web UI.
package static

import "embed"

// FS contains the embedded static files (JS, CSS, etc.).
//
//go:embed *.js
var FS embed.FS
