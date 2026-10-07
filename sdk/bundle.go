// Package sdk supplies pinned source files for SDK-2 project snapshots.
package sdk

import "embed"

//go:embed pda/pda.go cpi/cpi.go token/token.go token/lifecycle.go system/system.go system/rent.go
var Sources embed.FS
