// Package solana is the narrow host/SBF boundary for the experiment.
// Context fields are available to native tests; SBF source may only pass Context
// to the functions below, never inspect its fields or construct one.
package solana

type Account struct {
	Key, Owner, Data             []byte
	Signer, Writable, Executable bool
}

type Context struct {
	Accounts            []Account
	InstructionData, ID []byte
	Invoke              func(program, source, destination, authority, amount uint64, seed []byte, bump byte) uint64
	CheckPDA            func(account uint64, seed []byte, bump byte) bool
	Hash                func(input []byte) []byte
}

// SHA256 hashes an explicitly bounded region into 32 bytes of output. SBF uses
// the runtime syscall; native tests supply the standard library implementation.
func SHA256(c Context, input []byte, offset, length uint64, output []byte, outOffset uint64) uint64 {
	if offset > uint64(len(input)) || length > uint64(len(input))-offset || outOffset > uint64(len(output)) || uint64(len(output))-outOffset < 32 {
		return 2003
	}
	h := c.Hash(input[offset : offset+length])
	if len(h) != 32 {
		return 2004
	}
	for i := uint64(0); i < 32; i++ {
		output[outOffset+i] = h[i]
	}
	return 0
}

func Count(c Context) uint64                                 { return uint64(len(c.Accounts)) }
func Data(c Context, i uint64) []byte                        { return c.Accounts[i].Data }
func Key(c Context, i uint64) []byte                         { return c.Accounts[i].Key }
func Owner(c Context, i uint64) []byte                       { return c.Accounts[i].Owner }
func Signer(c Context, i uint64) bool                        { return c.Accounts[i].Signer }
func Writable(c Context, i uint64) bool                      { return c.Accounts[i].Writable }
func Executable(c Context, i uint64) bool                    { return c.Accounts[i].Executable }
func Instruction(c Context) []byte                           { return c.InstructionData }
func ProgramID(c Context) []byte                             { return c.ID }
func IsPDA(c Context, i uint64, seed []byte, bump byte) bool { return c.CheckPDA(i, seed, bump) }
func Transfer(c Context, program, source, destination, authority, amount uint64) uint64 {
	return c.Invoke(program, source, destination, authority, amount, nil, 0)
}
func TransferSigned(c Context, program, source, destination, authority, amount uint64, seed []byte, bump byte) uint64 {
	return c.Invoke(program, source, destination, authority, amount, seed, bump)
}
func IsTokenProgram(c Context, i uint64) bool {
	key := c.Accounts[i].Key
	id := [32]byte{6, 221, 246, 225, 215, 101, 161, 147, 217, 203, 225, 70, 206, 235, 121, 172, 28, 180, 133, 237, 95, 91, 55, 145, 58, 140, 245, 133, 126, 255, 0, 169}
	if len(key) != 32 || !c.Accounts[i].Executable {
		return false
	}
	for j := 0; j < 32; j++ {
		if key[j] != id[j] {
			return false
		}
	}
	return true
}
