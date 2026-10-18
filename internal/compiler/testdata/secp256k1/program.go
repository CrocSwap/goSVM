// Package program exercises solana.Secp256k1Recover. Account 0 (writable,
// 72 bytes) receives status:u64 and the 64-byte key. Instruction: hash[32],
// recovery ID u64le, signature[64], then the hash, signature and output
// lengths passed to the call (32, 64, 64 for a well-formed request).
package program

import "gosvm/solana"

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	result := solana.Data(c, 0)
	if len(ix) != 107 || len(result) != 72 {
		return 1
	}
	id := uint64(0)
	for j := uint64(0); j < 8; j++ {
		id = id | uint64(ix[32+j])<<(j*8)
	}
	hashEnd := uint64(ix[104])
	signatureEnd := 40 + uint64(ix[105])
	outputEnd := 8 + uint64(ix[106])
	code := solana.Secp256k1Recover(c, ix[0:hashEnd], id, ix[40:signatureEnd], result[8:outputEnd])
	for j := uint64(0); j < 8; j++ {
		result[j] = byte(code >> (j * 8))
	}
	return 0
}
