// Package model is ordinary shared Go, with no runtime or compiler dependency.
package model

type PublicKey [32]byte

// Record is created at the canonical PDA ["record", authority, bump].
type Record struct {
	Authority PublicKey
	Bump      byte
}

type CreateArgs struct{ Bump byte }

// DeriveArgs gives the seeds Derive searches: Seed[:Length], then Extra
// one-byte seeds 0, 1, ...
type DeriveArgs struct {
	Seed   [32]byte
	Length byte
	Extra  byte
}
