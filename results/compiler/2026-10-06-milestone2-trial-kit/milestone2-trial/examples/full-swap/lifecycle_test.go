package program

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"testing"

	"example.org/gosvm/full-swap/client"
	"example.org/gosvm/full-swap/model"
	"gosvm/sdk/token"
	"gosvm/solana"
)

type nativeLedger struct {
	accounts map[string]solana.Account
	id       [32]byte
	args     model.CreatePoolArgs
	calls    int
}

func nativeFixture() *nativeLedger {
	f := &nativeLedger{accounts: map[string]solana.Account{}, id: sha256.Sum256([]byte("native managed pool")), args: model.CreatePoolArgs{ReserveX: 1000000, ReserveY: 2000000, Bump: 253, XBump: 251, YBump: 249}}
	id := token.ProgramID()
	for _, name := range []string{"Pool", "Creator", "User", "CreatorX", "CreatorY", "UserX", "UserY", "MintX", "MintY", "Authority", "VaultX", "VaultY"} {
		key := sha256.Sum256([]byte("native " + name))
		f.accounts[name] = solana.Account{Key: key[:], Owner: make([]byte, 32)}
	}
	f.accounts["System"] = solana.Account{Key: make([]byte, 32), Owner: make([]byte, 32), Executable: true}
	f.accounts["TokenProgram"] = solana.Account{Key: id[:], Owner: make([]byte, 32), Executable: true}
	setKey := func(name string, parts ...[]byte) {
		a := f.accounts[name]
		h := sha256.New()
		for _, p := range parts {
			h.Write(p)
		}
		h.Write(f.id[:])
		h.Write([]byte("ProgramDerivedAddress"))
		a.Key = h.Sum(nil)
		f.accounts[name] = a
	}
	setKey("Authority", f.accounts["Pool"].Key, []byte{f.args.Bump})
	setKey("VaultX", []byte("vaultx"), f.accounts["Pool"].Key, []byte{f.args.XBump})
	setKey("VaultY", []byte("vaulty"), f.accounts["Pool"].Key, []byte{f.args.YBump})
	a := f.accounts["Creator"]
	a.Lamports = 100000000
	f.accounts["Creator"] = a
	for _, name := range []string{"MintX", "MintY"} {
		a := f.accounts[name]
		a.Owner = id[:]
		a.Data = make([]byte, 82)
		a.Data[45] = 1
		a.Lamports = 1461600
		f.accounts[name] = a
	}
	for _, v := range []struct {
		name, mint, owner string
		amount            uint64
	}{{"CreatorX", "MintX", "Creator", 100000000}, {"CreatorY", "MintY", "Creator", 100000000}, {"UserX", "MintX", "User", 100000}, {"UserY", "MintY", "User", 11}} {
		a := f.accounts[v.name]
		a.Owner = id[:]
		a.Data = make([]byte, 165)
		copy(a.Data, f.accounts[v.mint].Key)
		copy(a.Data[32:], f.accounts[v.owner].Key)
		binary.LittleEndian.PutUint64(a.Data[64:], v.amount)
		a.Data[108] = 1
		a.Lamports = 2039280
		f.accounts[v.name] = a
	}
	return f
}

func (f *nativeLedger) run(t *testing.T, op string, ix []byte, edit func([]solana.Account)) uint64 {
	t.Helper()
	names := []string{"Pool", "Creator", "CreatorX", "VaultX", "VaultY", "CreatorY", "Authority", "TokenProgram"}
	if op == "CreatePool" {
		names = append(names, "System", "MintX", "MintY")
	}
	if op == "ManagedSwap" {
		names[1] = "User"
		names[2] = "UserX"
		names[5] = "UserY"
	}
	c := solana.Context{ID: f.id[:], InstructionData: ix}
	for _, n := range names {
		a := f.accounts[n]
		a.Signer = n == "Creator" || n == "User" || n == "Pool" && op == "CreatePool"
		a.Writable = n != "Authority" && n != "TokenProgram" && n != "System" && n != "MintX" && n != "MintY" && n != "User"
		a.OriginalDataLen = uint64(len(a.Data))
		c.Accounts = append(c.Accounts, a)
	}
	if edit != nil {
		edit(c.Accounts)
	}
	c.ReadRent = func(out []byte) uint64 {
		binary.LittleEndian.PutUint64(out, 3480)
		binary.LittleEndian.PutUint64(out[8:], math.Float64bits(2))
		return 0
	}
	c.DeriveAddress = func(encoded, id, out []byte) uint64 {
		h := sha256.New()
		off := 1
		for j := 0; j < int(encoded[0]); j++ {
			n := int(encoded[off])
			off++
			h.Write(encoded[off : off+n])
			off += n
		}
		h.Write(id)
		h.Write([]byte("ProgramDerivedAddress"))
		copy(out, h.Sum(nil))
		return 0
	}
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		f.calls++
		account := func(meta int) *solana.Account { return &c.Accounts[binary.LittleEndian.Uint64(metas[9*meta:])] }
		source, destination := account(0), account(1)
		if names[program] == "System" {
			if len(data) != 52 || binary.LittleEndian.Uint32(data) != 0 {
				t.Fatalf("unexpected System CPI %x", data)
			}
			lamports, size := binary.LittleEndian.Uint64(data[4:]), binary.LittleEndian.Uint64(data[12:])
			if source.Lamports < lamports {
				return 1
			}
			source.Lamports -= lamports
			destination.Lamports = lamports
			destination.Data = make([]byte, size)
			destination.Owner = append([]byte(nil), data[20:52]...)
			return 0
		}
		switch data[0] {
		case 18:
			if len(data) != 33 || len(source.Data) != 165 {
				t.Fatal("bad initialization CPI")
			}
			source.Data = make([]byte, 165)
			copy(source.Data, destination.Key)
			copy(source.Data[32:], data[1:])
			source.Data[108] = 1
			return 0
		case 3:
			if source.Data[108] == 2 || destination.Data[108] == 2 {
				return 17
			}
			amount := binary.LittleEndian.Uint64(data[1:])
			x, y := binary.LittleEndian.Uint64(source.Data[64:]), binary.LittleEndian.Uint64(destination.Data[64:])
			if x < amount {
				return 1
			}
			if amount > ^uint64(0)-y {
				return 14
			}
			binary.LittleEndian.PutUint64(source.Data[64:], x-amount)
			binary.LittleEndian.PutUint64(destination.Data[64:], y+amount)
			return 0
		case 9:
			if binary.LittleEndian.Uint64(source.Data[64:]) != 0 {
				return 11
			}
			if source.Lamports > ^uint64(0)-destination.Lamports {
				return 14
			}
			destination.Lamports += source.Lamports
			source.Lamports = 0
			source.Data = nil
			source.Owner = make([]byte, 32)
			return 0
		default:
			t.Fatalf("unexpected token CPI %x", data)
			return 999
		}
	}
	code := Process(c)
	for i, n := range names {
		f.accounts[n] = c.Accounts[i]
	}
	return code
}

func TestManagedPoolNativeLifecycle(t *testing.T) {
	f := nativeFixture()
	creator := f.accounts["Creator"].Lamports
	if code := f.run(t, "CreatePool", client.EncodeInstructionCreatePool(f.args), nil); code != 0 || f.calls != 7 {
		t.Fatal(code, f.calls)
	}
	pool, err := client.DecodeAccountManagedPool(f.accounts["Pool"].Data)
	if err != nil || pool.Creator != model.PublicKey(*(*[32]byte)(f.accounts["Creator"].Key)) || pool.Core.Swaps != 0 || len(f.accounts["Pool"].Data) != 177 {
		t.Fatal(pool, err)
	}
	if got := f.accounts["Creator"].Lamports; got != creator-2122800-2*2039280 {
		t.Fatal(got)
	}
	args := model.SwapArgs{AmountIn: 10000, MinOut: 1}
	if code := f.run(t, "ManagedSwap", client.EncodeInstructionManagedSwap(args), nil); code != 0 {
		t.Fatal(code)
	}
	pool, err = client.DecodeAccountManagedPool(f.accounts["Pool"].Data)
	if err != nil || pool.Core.Swaps != 1 {
		t.Fatal(pool, err)
	}
	if code := f.run(t, "ClosePool", client.EncodeInstructionClosePool(model.ClosePoolArgs{}), nil); code != 0 {
		t.Fatal(code)
	}
	for _, n := range []string{"Pool", "VaultX", "VaultY"} {
		a := f.accounts[n]
		if len(a.Data) != 0 || a.Lamports != 0 || !bytes.Equal(a.Owner, make([]byte, 32)) {
			t.Fatal(n, a)
		}
	}
	if f.accounts["Creator"].Lamports != creator || f.calls != 13 {
		t.Fatal(f.accounts["Creator"].Lamports, f.calls)
	}
	if code := f.run(t, "CreatePool", client.EncodeInstructionCreatePool(f.args), nil); code != 0 {
		t.Fatal("recreate", code)
	}
}

func TestManagedPoolNativePolicyBeforeCPI(t *testing.T) {
	f := nativeFixture()
	code := f.run(t, "CreatePool", client.EncodeInstructionCreatePool(f.args), func(accounts []solana.Account) { accounts[0].Signer = false })
	if code != 6001 || f.calls != 0 {
		t.Fatal(code, f.calls)
	}
	f = nativeFixture()
	if code := f.run(t, "CreatePool", client.EncodeInstructionCreatePool(f.args), nil); code != 0 {
		t.Fatal(code)
	}
	before := append([]byte(nil), f.accounts["Pool"].Data...)
	calls := f.calls
	code = f.run(t, "ClosePool", client.EncodeInstructionClosePool(model.ClosePoolArgs{}), func(accounts []solana.Account) { accounts[1].Signer = false })
	if code != 6001 || f.calls != calls || !bytes.Equal(before, f.accounts["Pool"].Data) {
		t.Fatal(code, f.calls)
	}
}
