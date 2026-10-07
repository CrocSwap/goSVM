// Package checkedtoken exercises SDK-2 checked transfers and generic CPI.
// This compiler fixture uses preloaded accounts; lifecycle/schema migration is
// separate milestone work. Accounts: result, user, source, destination, PDA,
// token-or-System program, frozen token destination.
package checkedtoken

import (
	"gosvm/sdk/cpi"
	"gosvm/sdk/pda"
	"gosvm/sdk/token"
	"gosvm/solana"
)

func read(b []byte, off uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(b[off+j])<<(j*8)
	}
	return v
}
func write(b []byte, off, value uint64) {
	for j := uint64(0); j < 8; j++ {
		b[off+j] = byte(value >> (j * 8))
	}
}

func Process(c solana.Context) uint64 {
	if solana.Count(c) != 7 {
		return 4000
	}
	ix := solana.Instruction(c)
	result := solana.Data(c, 0)
	if len(ix) != 16 || len(result) != 24 || !solana.Writable(c, 0) {
		return 4000
	}
	mode := ix[8]
	amount := read(ix, 0)
	if mode == 4 {
		return systemTransfer(c, amount, result)
	}
	source, code := token.Load(c, 2, true)
	if code != 0 {
		return code
	}
	destination, status := token.Load(c, 3, true)
	if status != 0 {
		return status
	}
	before, code := token.Read(c, source)
	if code != 0 {
		return code
	}
	write(result, 0, before.Amount)
	if mode == 0 || mode == 2 {
		code = token.Transfer(c, 5, source, destination, 1, amount)
	} else if mode == 1 || mode == 3 {
		var seeds pda.Seeds
		var key, program [32]byte
		inputKey := solana.Key(c, 0)
		id := solana.ProgramID(c)
		if len(inputKey) != 32 || len(id) != 32 {
			return 4001
		}
		for j := uint64(0); j < 32; j++ {
			key[j] = inputKey[j]
			program[j] = id[j]
		}
		if seeds.AddKey(key) != 0 || seeds.AddUint32(0x12345678) != 0 || seeds.AddUint64(0x1122334455667788) != 0 || seeds.AddByte(ix[9]) != 0 {
			return pda.ErrSeeds
		}
		matches, deriveCode := seeds.Matches(c, 4, program)
		if deriveCode != 0 {
			return deriveCode
		}
		if !matches {
			return 4002
		}
		if mode == 1 {
			code = token.TransferSigned(c, 5, source, destination, 4, amount, &seeds)
		} else {
			var other pda.Seeds
			if other.AddByte(13) != 0 || other.AddByte(ix[10]) != 0 {
				return pda.ErrSeeds
			}
			var signers cpi.Signers
			if signers.Add(&seeds) != 0 || signers.Add(&other) != 0 {
				return cpi.ErrSigners
			}
			var metas cpi.Metas
			if metas.Add(2, true, false) != 0 || metas.Add(3, true, false) != 0 || metas.Add(4, false, true) != 0 {
				return cpi.ErrMetas
			}
			var data [9]byte
			data[0] = 3
			write(data[:], 1, amount)
			code = cpi.Invoke(c, 5, &metas, data[:], &signers)
		}
	} else {
		return 4003
	}
	if code != 0 {
		return code
	}
	after, code := token.Read(c, source)
	if code != 0 {
		return code
	}
	received, status := token.Read(c, destination)
	if status != 0 {
		return status
	}
	write(result, 8, after.Amount)
	write(result, 16, received.Amount)
	if mode == 2 {
		frozen, status := token.Load(c, 6, true)
		if status != 0 {
			return status
		}
		return token.Transfer(c, 5, destination, frozen, 1, 1)
	}
	return 0
}

func systemTransfer(c solana.Context, amount uint64, result []byte) uint64 {
	var metas cpi.Metas
	if metas.Add(1, true, true) != 0 || metas.Add(3, true, false) != 0 {
		return cpi.ErrMetas
	}
	var data [12]byte
	data[0] = 2
	write(data[:], 4, amount)
	write(result, 0, solana.Lamports(c, 1))
	code := cpi.Invoke(c, 5, &metas, data[:], nil)
	if code != 0 {
		return code
	}
	write(result, 8, solana.Lamports(c, 1))
	write(result, 16, solana.Lamports(c, 3))
	return 0
}
