// Command oracle runs the example natively against the unmodified library
// modules. It reads one hex-encoded 256-byte vector per line and prints, per
// vector, `<result>:<hex output>` over a 1 KiB output buffer or, when Run
// panics, `P:<hex output>`, preceded by `L:<hex log line> ` for a panic that
// goSVM logs (docs/ERROR_CONTRACT.md#panic): a string value is logged as
// `panic: <value>` and an error value as `panic: (error)`; runtime errors are
// traps without a line. The libraries' package-level variables are never
// written by the example, so one process serves every vector.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"

	reallibs "example.com/real-libraries"
)

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for in.Scan() {
		vector, err := hex.DecodeString(in.Text())
		if err != nil || len(vector) != 256 {
			fmt.Fprintf(os.Stderr, "oracle: bad vector %q\n", in.Text())
			os.Exit(2)
		}
		out.WriteString(run(vector))
	}
}

func run(in []byte) (result string) {
	buf := make([]byte, 1024)
	defer func() {
		if r := recover(); r != nil {
			prefix := ""
			switch v := r.(type) {
			case runtime.Error:
			case error:
				prefix = "L:" + hex.EncodeToString([]byte("panic: (error)")) + " "
			case string:
				prefix = "L:" + hex.EncodeToString([]byte("panic: "+v)) + " "
			default:
				fmt.Fprintf(os.Stderr, "oracle: unexpected panic value %T\n", r)
				os.Exit(2)
			}
			result = fmt.Sprintf("%sP:%x\n", prefix, buf)
		}
	}()
	code := reallibs.Run(buf, in)
	return fmt.Sprintf("%d:%x\n", code, buf)
}
