// Package tokenextensions exercises SDK-2 mint/token-account decoding that
// tolerates Token-2022 extensions, extension lookup and transfer-fee
// calculation against real Token-2022 accounts. It is an SBF fixture, not
// application policy. Instruction data: opcode, six account indices (the first
// is an 80-byte program-owned result), little-endian uint64 argument, decimals.
package tokenextensions

import (
	"gosvm/sdk/cpi"
	"gosvm/sdk/token"
	"gosvm/solana"
)

func put(b []byte, offset, value uint64) {
	for j := uint64(0); j < 8; j++ {
		b[offset+j] = byte(value >> (j * 8))
	}
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 16 || uint64(ix[1]) >= solana.Count(c) {
		return 4000
	}
	out := solana.Data(c, uint64(ix[1]))
	if len(out) != 80 || !solana.Writable(c, uint64(ix[1])) {
		return 4000
	}
	for j := uint64(0); j < 80; j++ {
		out[j] = 0
	}
	a1, a2, a3, a4, a5 := uint64(ix[2]), uint64(ix[3]), uint64(ix[4]), uint64(ix[5]), uint64(ix[6])
	value := uint64(0)
	for j := uint64(0); j < 8; j++ {
		value = value | uint64(ix[7+j])<<(j*8)
	}
	switch ix[0] {
	case 0:
		mint, code := token.ReadMint(c, a1)
		if code != 0 {
			return code
		}
		put(out, 0, mint.Supply)
		out[8] = mint.Decimals
		if mint.HasAuthority {
			out[9] = 1
		}
		for j := uint64(0); j < 32; j++ {
			out[10+j] = mint.Authority[j]
		}
		if mint.HasFreeze {
			out[42] = 1
		}
		return 0
	case 1:
		account, code := token.ReadAccount(c, a1)
		if code != 0 {
			return code
		}
		put(out, 0, account.Amount)
		for j := uint64(0); j < 32; j++ {
			out[8+j] = account.Mint[j]
			out[40+j] = account.Authority[j]
		}
		if account.Frozen {
			out[72] = 1
		}
		return 0
	case 2:
		return transfer(c, out, a1, a2, a3, a4, a5, value, ix[15])
	case 3:
		offset, length, code := token.FindExtension(c, a1, uint16(value))
		if code != 0 {
			return code
		}
		put(out, 0, offset)
		put(out, 8, length)
		data := solana.Data(c, a1)
		for j := uint64(0); j < length && j < 64; j++ {
			out[16+j] = data[offset+j]
		}
		return 0
	}
	return 4002
}

// transfer computes the fee for the current epoch, then has Token-2022
// apply it with a raw TransferChecked and reads back what it withheld.
func transfer(c solana.Context, out []byte, mint, program, source, destination, authority, amount uint64, decimals byte) uint64 {
	var clock [40]byte
	code := solana.Clock(c, clock[:])
	if code != 0 {
		return code
	}
	epoch := uint64(0)
	for j := uint64(0); j < 8; j++ {
		epoch = epoch | uint64(clock[16+j])<<(j*8)
	}
	fee, status := token.Fee(c, mint, epoch, amount)
	if status != 0 {
		return status
	}
	var data [10]byte
	data[0] = 12
	put(data[:], 1, amount)
	data[9] = decimals
	var metas cpi.Metas
	if metas.Add(source, true, false) != 0 || metas.Add(mint, false, false) != 0 || metas.Add(destination, true, false) != 0 || metas.Add(authority, false, true) != 0 {
		return cpi.ErrMetas
	}
	code = cpi.Invoke(c, program, &metas, data[:], nil)
	if code != 0 {
		return code
	}
	received, status := token.ReadAccount(c, destination)
	if status != 0 {
		return status
	}
	offset, length, found := token.FindExtension(c, destination, token.ExtensionTransferFeeAmount)
	if found != 0 || length != 8 {
		return token.ErrExtension
	}
	put(out, 0, fee)
	put(out, 8, received.Amount)
	withheld := uint64(0)
	destinationData := solana.Data(c, destination)
	for j := uint64(0); j < 8; j++ {
		withheld = withheld | uint64(destinationData[offset+j])<<(j*8)
	}
	put(out, 16, withheld)
	put(out, 24, epoch)
	return 0
}
