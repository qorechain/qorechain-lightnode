package web

import "embed"

// Assets holds the embedded dashboard UI files (HTML, CSS, JS, images).
//
//go:embed all:index.html all:css all:js all:assets
var Assets embed.FS
