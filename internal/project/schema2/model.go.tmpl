// Package model is ordinary shared Go, with no runtime or compiler dependency.
package model

type PublicKey [32]byte
type Audit struct {
	Tag byte
	Epoch uint32
}
type Vault struct {
	Authority PublicKey
	Audit Audit
	Balance uint64
	Moves uint64
}
type Policy struct {
	Authority PublicKey
	Limit uint64
}
type MoveArgs struct { Amount uint64 }
type SetLimitArgs struct { Limit uint64 }

// CanMove is shared by the on-chain handler and ordinary Go callers.
func CanMove(source, destination, amount, limit uint64) bool {
	return amount != 0 && amount <= limit && amount <= source && destination <= 18446744073709551615-amount
}
