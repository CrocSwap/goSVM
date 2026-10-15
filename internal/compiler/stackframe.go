package compiler

import (
	"fmt"
	"regexp"
	"strings"
)

// SBF stack frames are 4 KiB. The SBF LLVM backend reports a function whose
// frame exceeds them (or a call that overwrites its caller's frame) with a
// line starting "Error: " but still compiles it, and Clang exits
// successfully; the program would then read or write outside its frame at
// run time (an access violation, or silent corruption with dynamic frames).
// frameDiagnostics turns those lines into a build error naming the Go
// function. Functions inlined into a reported function count toward its
// frame.

// frameFunction finds the C function a frame diagnostic names (platform-tools
// v1.57 and v1.51 wordings).
var frameFunction = regexp.MustCompile(`^Error: (?:Function|A function call in method) (\S+)`)

func frameDiagnostics(output []byte, functionNames map[string]string) error {
	var problems []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Error: ") {
			continue
		}
		where := "clang"
		if m := frameFunction.FindStringSubmatch(line); m != nil {
			if goName, ok := functionNames[m[1]]; ok {
				where = fmt.Sprintf("%s (C %s)", goName, m[1])
			} else if m[1] == "entrypoint" {
				where = "the entrypoint (Process, with what is inlined into it)"
			} else {
				where = m[1]
			}
		}
		problems = append(problems, fmt.Sprintf("%s: %s", where, strings.TrimPrefix(line, "Error: ")))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("SBF stack frame exceeds 4096 bytes; reduce large local values or split the function "+
		"(functions inlined into it count toward its frame):\n  %s", strings.Join(problems, "\n  "))
}
