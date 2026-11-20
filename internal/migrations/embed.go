package migrations

import "embed"

// Files embeds the versioned SQL migrations for the service-owned schemas.
//
//go:embed service/*.sql
var Files embed.FS
