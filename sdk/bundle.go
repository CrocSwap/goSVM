// Package sdk supplies pinned source files for SDK-2 project snapshots.
package sdk

import "embed"

//go:embed ata/ata.go pda/pda.go cpi/cpi.go token/token.go token/lifecycle.go token/instructions.go token/extensions.go system/system.go system/rent.go fixture/fixture.go
var Sources embed.FS
