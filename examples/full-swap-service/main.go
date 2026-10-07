// A normal Go consumer of the actual swap types, codecs and calculation.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.org/gosvm/full-swap/client"
	"example.org/gosvm/full-swap/model"
)

func main() {
	poolHex := flag.String("pool", "", "hex-encoded pool account")
	managed := flag.Bool("managed", false, "decode a creator-managed pool and construct ManagedSwap")
	x := flag.Uint64("x", 0, "input vault balance")
	y := flag.Uint64("y", 0, "output vault balance")
	amount := flag.Uint64("amount", 0, "exact input amount")
	minimum := flag.Uint64("minimum", 0, "minimum output")
	flag.Parse()
	b, err := hex.DecodeString(*poolHex)
	if err != nil {
		fail(err)
	}
	var pool model.Pool
	var managedPool *model.ManagedPool
	if *managed {
		decoded, decodeErr := client.DecodeAccountManagedPool(b)
		if decodeErr != nil {
			fail(decodeErr)
		}
		managedPool = &decoded
		pool = decoded.Core
	} else {
		pool, err = client.DecodeAccountPool(b)
		if err != nil {
			fail(err)
		}
	}
	args := model.SwapArgs{AmountIn: *amount, MinOut: *minimum}
	instruction := client.EncodeInstructionSwap(args)
	if *managed {
		instruction = client.EncodeInstructionManagedSwap(args)
	}
	result := struct {
		Pool        model.Pool
		Quote       uint64
		Instruction string
		ManagedPool *model.ManagedPool `json:",omitempty"`
	}{
		pool, model.Quote(*x, *y, *amount), hex.EncodeToString(instruction), managedPool,
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
