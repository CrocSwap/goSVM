// Package model defines canonical shared escrow state and arguments.
package model

type PublicKey [32]byte

type Escrow struct {
	Creator     PublicKey
	Beneficiary PublicKey
	Commitment  [32]byte
	Amount      uint64
	Nonce       uint64
	Bump        byte
}

type OpenArgs struct {
	Beneficiary PublicKey
	Commitment  [32]byte
	Amount      uint64
	Nonce       uint64
	Bump        byte
}

type ClaimArgs struct {
	Preimage [32]byte
	Nonce    uint64
}
type CancelArgs struct{}
