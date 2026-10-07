package solana

import _ "embed"

// Source is the single source of truth used by the compiler's narrow importer.
//
//go:embed api.go
var Source string

// LegacySource preserves the exact schema-1/SDK-1 snapshot.
//
//go:embed api-v1.go.txt
var LegacySource string
