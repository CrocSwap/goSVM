// An ordinary Go client using canonical types, codecs and quote logic.
package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"example.com/shared-app/model"
	"example.com/shared-app/quote"
	"example.com/shared-app/wire"
)

func main() {
	pool := model.Pool{ReserveX: 1000000, ReserveY: 2000000, Sequence: 4}
	args := model.SwapArgs{AmountIn: 1000, MinOut: 1998}
	state, instruction := make([]byte, wire.PoolBytes), make([]byte, wire.SwapBytes)
	wire.EncodePool(state, pool)
	wire.EncodeSwap(instruction, args)
	if wire.DecodePool(state) != pool || wire.DecodeSwap(instruction) != args {
		panic("wire round trip")
	}
	key := model.PublicKey{0: 11}
	marked := quote.Mark(key, 22)
	if key[31] != 0 || marked[0] != 11 || marked[31] != 22 {
		panic("key copy")
	}
	result := struct {
		Quote             model.Amount
		Pool, Instruction string
	}{quote.Quote(pool, args), hex.EncodeToString(state), hex.EncodeToString(instruction)}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
