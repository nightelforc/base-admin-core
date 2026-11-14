// Package migrations embeds the SQL schema migrations so the server binary is
// self-contained: deploying a new version is enough to apply new migrations.
package migrations

import "embed"

// FS holds every .sql migration, applied in lexical filename order.
//
//go:embed *.sql
var FS embed.FS
