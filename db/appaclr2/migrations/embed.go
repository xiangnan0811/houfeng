package migrations

import "embed"

// FS is the isolated APP ACL R2 migration source. It is intentionally separate
// from db/migrations so generic and frozen R1 discovery cannot enumerate 0052.
//
//go:embed *.sql
var FS embed.FS
