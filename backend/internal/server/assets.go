//go:build !mengka

package server

import "testing/fstest"

// Test-only fallback; distributable builds must use the mengka build tag.
var frontendDist = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>Build the plugin with scripts/build-plugin.mjs</body></html>")}}
