// Package schema embeds the vendored PrepKit JSON Schemas so a built binary
// carries them with no runtime file dependency.
package schema

import _ "embed"

// KitTemplate is schema/kit-template.schema.json (spec v0.1).
//
//go:embed kit-template.schema.json
var KitTemplate []byte

// Inventory is schema/inventory.schema.json (spec v0.1).
//
//go:embed inventory.schema.json
var Inventory []byte
