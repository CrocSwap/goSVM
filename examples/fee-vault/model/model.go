// Package model is ordinary shared Go, with no runtime or compiler dependency.
package model

type PublicKey [32]byte

// Vault holds lamports for its beneficiary above its own rent-exempt minimum,
// like Hyperlane's mailbox outbox and gas paymaster accounts hold fees.
type Vault struct {
	Authority   PublicKey
	Beneficiary PublicKey
	Bump        byte
}

type CreateArgs struct {
	Beneficiary PublicKey
	Bump        byte
}

type ClaimArgs struct{}

// MoveArgs names two of Move's accounts by position (0 Vault, 1 Authority,
// 2 Recipient, 3 Observer), so tests can try every pair.
type MoveArgs struct {
	From   byte
	To     byte
	Amount uint64
}

// Claimable is the balance above the rent-exempt minimum (zero below it).
func Claimable(balance, minimum uint64) uint64 {
	if balance <= minimum {
		return 0
	}
	return balance - minimum
}
