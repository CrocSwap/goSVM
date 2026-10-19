// Package program exercises solana.GetReturnData through real CPIs to itself.
// Accounts: result (writable, program-owned), this program (executable).
// Instruction: mode byte, little-endian uint16 capacity and uint16 size.
// Setting modes set size bytes of byte(i*29+3); reading modes write
// size:u64, setter:[32]byte and output:[capacity]byte into result.
package program

import "gosvm/solana"

const (
	modeSet            = 0 // callee: set size bytes
	modeCPI            = 1 // invoke modeSet, then read
	modeSilent         = 2 // callee: set nothing
	modeOwn            = 3 // set size bytes, then read without a CPI
	modeRead           = 4 // read only
	modeShort          = 5 // read with a 31-byte program buffer: traps
	modeCPIRead        = 6 // invoke modeSilent, then read
	modeOverlap        = 7 // set and read into exactly overlapping output buffers
	modePartialOverlap = 8 // set and read into partially overlapping buffers
	modeSetSilent      = 9 // set data, invoke modeSilent, then read cleared data
)

func set(c solana.Context, size uint64) uint64 {
	data := make([]byte, size)
	for i := uint64(0); i < size; i++ {
		data[i] = byte(i*29 + 3)
	}
	return solana.SetReturnData(c, data)
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 5 {
		return 1
	}
	mode := ix[0]
	capacity := uint64(ix[1]) | uint64(ix[2])<<8
	size := uint64(ix[3]) | uint64(ix[4])<<8
	if mode == modeSet {
		return set(c, size)
	}
	if mode == modeSilent {
		return 0
	}
	if mode == modeSetSilent {
		if code := set(c, 8); code != 0 {
			return code
		}
	}
	if mode == modeCPI || mode == modeCPIRead || mode == modeSetSilent {
		var data [5]byte
		data[0] = modeSet
		if mode == modeCPIRead || mode == modeSetSilent {
			data[0] = modeSilent
		}
		data[3], data[4] = ix[3], ix[4]
		if code := solana.Invoke(c, 1, nil, data[:], nil); code != 0 {
			return code
		}
	}
	if mode == modeOwn || mode == modeOverlap || mode == modePartialOverlap {
		if code := set(c, size); code != 0 {
			return code
		}
	}
	result := solana.Data(c, 0)
	if uint64(len(result)) != 40+capacity {
		return 2
	}
	program := result[8:40]
	if mode == modeOverlap {
		program = result[40:72]
	}
	if mode == modePartialOverlap {
		program = result[41:73]
	}
	if mode == modeShort {
		program = result[8:39]
	}
	n := solana.GetReturnData(c, result[40:], program)
	for j := uint64(0); j < 8; j++ {
		result[j] = byte(n >> (j * 8))
	}
	return 0
}
