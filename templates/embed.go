// Package templates embeds the bundled template packs. The binary ships with
// these; additional packs can be loaded from disk at startup.
package templates

import "embed"

// FS holds the bundled *.yaml template files at its root.
//
//go:embed *.yaml
var FS embed.FS
