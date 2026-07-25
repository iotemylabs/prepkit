// Package model defines the entities of the PrepKit data model, mirroring
// schema/kit-template.schema.json and schema/inventory.schema.json from the
// PrepKit spec (v0.1). It carries no persistence or scoring behaviour; only
// the types, their enumerations, validation helpers, and the category/colour
// mapping from docs/categories.md.
//
// Field sensitivity follows docs/threat-model.md. Fields marked SENSITIVE in
// comments must never reach a log line and are excluded from unencrypted
// exports by default (some unconditionally); the export and web layers
// enforce this by field classification, not pattern matching.
package model

// SpecVersion is the PrepKit spec version this model implements. Both schemas
// pin it with a const, so an export or template claiming any other version is
// rejected rather than reinterpreted.
const SpecVersion = "0.1"
