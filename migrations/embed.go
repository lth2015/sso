// Package migrations contains the versioned application database schemas.
package migrations

import "embed"

// SQL contains business and isolated BFF migrations, never runtime credentials.
//
//go:embed business/*.sql bff/*.sql
var SQL embed.FS
