// Command oracle runs examples/global-values natively for one vector per
// process, as every SBF invocation starts from freshly initialized package
// variables: `oracle <hex input>` prints `<result>:<hex output>` or, when
// Run panics, `P:<hex output>`, over a 1 KiB output buffer.
package main

import (
	"encoding/hex"
	"fmt"
	"os"

	globalvalues "gosvm/examples/global-values"
)

func main() {
	in, err := hex.DecodeString(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			fmt.Printf("P:%x\n", out)
		}
	}()
	code := globalvalues.Run(out, in)
	fmt.Printf("%d:%x\n", code, out)
}
