package solana

import _ "embed"

// Source is the single source of truth used by the compiler's narrow importer.
//
//go:embed api.go
var Source string
