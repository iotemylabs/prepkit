// Package migrations embeds the numbered schema migration files. The store
// applies any not yet recorded in schema_migrations, in filename order, each
// inside its own transaction.
package migrations

import "embed"

// FS holds the *.sql migration files at its root. Filenames are
// NNN_description.sql; the numeric prefix is the version.
//
//go:embed *.sql
var FS embed.FS
