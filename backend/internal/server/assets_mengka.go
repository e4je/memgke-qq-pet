//go:build mengka

package server

import (
	"embed"
	"io/fs"
)

//go:embed plugin-dist
var embedded embed.FS
var frontendDist, _ = fs.Sub(embedded, "plugin-dist")
