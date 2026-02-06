// Package migrations provides embedded SQL migrations for Goose.
package migrations

import "embed"

// FS embeds all .sql migration files in this directory.
//
//go:embed *.sql
var FS embed.FS
