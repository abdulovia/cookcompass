// Package web embeds templates, static assets and UI translations so the
// binary is self-contained.
package web

import "embed"

// FS holds templates/, static/ and locales/.
//
//go:embed templates static locales
var FS embed.FS
