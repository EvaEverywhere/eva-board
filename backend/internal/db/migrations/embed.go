package migrations

import "embed"

// Files contains all SQL migration files.
//
//go:embed *.sql
var Files embed.FS
