package main

import (
	"bytes"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// lcUUIDAbort is how dyld on macOS 26 rejects binaries linked by Go 1.22's
// internal linker when they use networking.
const lcUUIDAbort = "missing LC_UUID load command"

const goFix = "install Go 1.23 or newer (https://go.dev/dl/), or set GOTOOLCHAIN=go1.23.12 (or newer)"

// macOSMajor returns the host's macOS major version, or 0 elsewhere or when it
// cannot be determined. It prefers sw_vers and falls back to the Darwin kernel
// release (Darwin 25 is macOS 26; earlier kernels map to macOS 15 or older).
func macOSMajor() int {
	if runtime.GOOS != "darwin" {
		return 0
	}
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		if major := leadingInt(strings.TrimSpace(string(out))); major > 0 {
			return major
		}
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		return macOSFromDarwin(leadingInt(strings.TrimSpace(string(out))))
	}
	return 0
}

func macOSFromDarwin(darwin int) int {
	switch {
	case darwin >= 25:
		return darwin + 1
	case darwin >= 20:
		return darwin - 9
	}
	return 0
}

func leadingInt(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// goVersion reports the toolchain `go` selects in dir, honoring GOTOOLCHAIN and
// go.mod as `gosvm test` does ("" if there is no go on PATH).
func goVersion(dir string) (string, error) {
	cmd := exec.Command("go", "env", "GOVERSION")
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// goHostWarning returns a doctor WARN line when binaries the selected Go builds
// abort on this host: Go 1.22.x on macOS 26 or newer. macOS is the host's
// macOS major version (0 if not macOS); version is `go env GOVERSION`.
func goHostWarning(macOS int, version string) string {
	if macOS < 26 || !(version == "go1.22" || strings.HasPrefix(version, "go1.22.") || strings.HasPrefix(version, "go1.22rc")) {
		return ""
	}
	return "WARN " + version + " on macOS " + strconv.Itoa(macOS) + ": binaries it builds that use networking (gosvm test, go run) abort with `dyld: " + lcUUIDAbort + "`\n  fix: " + goFix
}

// goMissingMessage is doctor's line when no go command is on PATH.
const goMissingMessage = "MISSING go (needed by gosvm build and gosvm test)\n  fix: install Go 1.23 or newer from https://go.dev/dl/ and put it on PATH"

// lcUUIDWatcher records whether output streamed through it contains the dyld
// LC_UUID abort, tolerating matches split across writes.
type lcUUIDWatcher struct {
	tail  []byte
	found bool
}

func (w *lcUUIDWatcher) Write(p []byte) (int, error) {
	if !w.found {
		w.tail = append(w.tail, p...)
		w.found = bytes.Contains(w.tail, []byte(lcUUIDAbort))
		if keep := len(lcUUIDAbort) - 1; len(w.tail) > keep {
			w.tail = append(w.tail[:0], w.tail[len(w.tail)-keep:]...)
		}
	}
	return len(p), nil
}

// lcUUIDHint explains a `go test` failure caused by the dyld abort.
func lcUUIDHint(version string) string {
	if version == "" {
		version = "this Go"
	}
	return "gosvm: test binaries built by " + version + " abort on this macOS with `dyld: " + lcUUIDAbort + "`\n  fix: " + goFix
}
