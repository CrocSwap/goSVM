// Package model defines the canonical swap types shared with ordinary Go services.
package model

type PublicKey [32]byte
type Pool struct {
	VaultX PublicKey
	VaultY PublicKey
	MintX  PublicKey
	MintY  PublicKey
	Bump   byte
	Swaps  uint64
}
type SwapArgs struct {
	AmountIn uint64
	MinOut   uint64
}

// ManagedPool is a separate layout; it does not reinterpret existing Pool bytes.
// The creator owns the deposited liquidity and receives it when closing.
type ManagedPool struct {
	Core    Pool
	Creator PublicKey
}
type CreatePoolArgs struct {
	ReserveX uint64
	ReserveY uint64
	Bump     byte
	XBump    byte
	YBump    byte
}
type ClosePoolArgs struct{}
