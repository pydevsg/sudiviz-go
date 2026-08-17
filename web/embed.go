package web

import "embed"

// StaticFS is the embedded Cytoscape frontend (HTML/CSS/JS + AWS icons).
//
//go:embed all:static
var StaticFS embed.FS
