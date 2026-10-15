// Command oracle runs examples/global-value-types natively for one vector
// per process, as every SBF invocation starts from freshly initialized
// package variables: `oracle <hex input>` prints `<result>:<hex output>`
// or, when Run panics, `P:<hex output>` over a 1 KiB output buffer, preceded
// by `L:<hex line> ` for a panic call: the log line goSVM writes
// (docs/ERROR_CONTRACT.md#panic). Runtime errors are traps without a line.
package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"runtime"

	globalvaluetypes "gosvm/examples/global-value-types"
	"gosvm/examples/global-value-types/wide"
)

// line is goSVM's log line for the panic values the example uses.
func line(r any) string {
	switch v := r.(type) {
	case runtime.Error:
		return ""
	case wide.Limit:
		return fmt.Sprintf("panic: wide.Limit(%d)", v)
	case globalvaluetypes.Code:
		return fmt.Sprintf("panic: globalvaluetypes.Code(%d)", v)
	case string:
		return "panic: " + v
	}
	panic(fmt.Sprintf("oracle: unexpected panic value %T", r))
}

func main() {
	in, err := hex.DecodeString(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	out := make([]byte, 1024)
	defer func() {
		if r := recover(); r != nil {
			if l := line(r); l != "" {
				fmt.Printf("L:%x ", l)
			}
			fmt.Printf("P:%x\n", out)
		}
	}()
	code := globalvaluetypes.Run(out, in)
	fmt.Printf("%d:%x\n", code, out)
}
