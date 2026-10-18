package main

import (
	"strings"
	"testing"
)

func TestGoHostWarning(t *testing.T) {
	for _, c := range []struct {
		macOS   int
		version string
		warn    bool
	}{
		{26, "go1.22.0", true},
		{26, "go1.22.12", true},
		{27, "go1.22", true},
		{26, "go1.22rc1", true},
		{26, "go1.23.12", false},
		{26, "go1.25.0", false},
		{26, "go1.220", false},
		{15, "go1.22.0", false},
		{0, "go1.22.0", false},
	} {
		got := goHostWarning(c.macOS, c.version)
		if (got != "") != c.warn {
			t.Fatalf("goHostWarning(%d, %q) = %q", c.macOS, c.version, got)
		}
		if c.warn && (!strings.HasPrefix(got, "WARN "+c.version+" on macOS ") || !strings.Contains(got, "missing LC_UUID load command") || !strings.Contains(got, "GOTOOLCHAIN=go1.23.12")) {
			t.Fatalf("warning text: %q", got)
		}
	}
}

func TestMacOSFromDarwin(t *testing.T) {
	for darwin, want := range map[int]int{25: 26, 26: 27, 24: 15, 20: 11, 19: 0, 0: 0} {
		if got := macOSFromDarwin(darwin); got != want {
			t.Fatalf("Darwin %d -> macOS %d, want %d", darwin, got, want)
		}
	}
	if leadingInt("26.6.2") != 26 || leadingInt("25.6.0") != 25 || leadingInt("x") != 0 {
		t.Fatal("leadingInt")
	}
}

func TestLCUUIDWatcherFindsSplitMessage(t *testing.T) {
	w := &lcUUIDWatcher{}
	for _, chunk := range []string{"ok  \texample\n", "dyld[123]: missing LC_UU", "ID load command in /tmp/x.test\n", "more\n"} {
		w.Write([]byte(chunk))
	}
	if !w.found {
		t.Fatal("split abort not detected")
	}
	w = &lcUUIDWatcher{}
	w.Write([]byte("FAIL example 0.1s\n"))
	if w.found {
		t.Fatal("false positive")
	}
	if h := lcUUIDHint("go1.22.0"); !strings.Contains(h, "go1.22.0") || !strings.Contains(h, "Go 1.23 or newer") {
		t.Fatalf("hint: %q", h)
	}
}
