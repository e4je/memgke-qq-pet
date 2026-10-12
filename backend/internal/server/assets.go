//go:build !mengka

package server

import "embed"

//go:embed frontend-dist
var frontendDist embed.FS

const frontendDirectory = "frontend-dist"
