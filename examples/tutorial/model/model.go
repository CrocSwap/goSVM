// Package model holds the counter's state, instruction arguments and rules.
// It is ordinary Go: the on-chain program and off-chain services import it.
package model

type PublicKey [32]byte

// Counter is the state account. Each owner has one, at the address derived
// from the seed "counter", the owner's key and a bump byte.
type Counter struct {
	Owner PublicKey
	Count uint64
	Limit uint64
	Bump  byte
}

type CreateArgs struct {
	Limit uint64
	Bump  byte
}

type IncrementArgs struct {
	Amount uint64
}

type PreviewArgs struct {
	Count  uint64
	Limit  uint64
	Amount uint64
}

// Application error codes, returned as the transaction's custom error.
const (
	ErrZeroAmount uint64 = 4100
	ErrLimit      uint64 = 4101
)

// Next returns the count after adding amount, or a nonzero error code.
func Next(count, limit, amount uint64) (uint64, uint64) {
	if amount == 0 {
		return count, ErrZeroAmount
	}
	if amount > limit || count > limit-amount {
		return count, ErrLimit
	}
	return count + amount, 0
}
