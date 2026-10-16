package toolchain

import (
	"strings"
	"testing"

	target "gosvm/internal/targets"
)

func TestOfficialArchivesPerHost(t *testing.T) {
	for _, version := range []string{Version, target.ModernTools} {
		for _, host := range []string{"darwin-arm64", "linux-amd64", "linux-arm64"} {
			a, err := archiveFor(version, host)
			if err != nil || len(a.SHA) != 64 || a.Size < 400e6 || !strings.HasPrefix(a.Asset, "platform-tools-") {
				t.Fatalf("%s %s: %+v %v", version, host, a, err)
			}
			if version == target.ModernTools {
				pins := target.ModernBackendPins[host]
				if len(pins["clang"]) != 64 || len(pins["ld.lld"]) != 64 {
					t.Fatalf("%s: missing content pins", host)
				}
			}
		}
		if _, err := archiveFor(version, "windows-amd64"); err == nil || !strings.Contains(err.Error(), "supported hosts: darwin-arm64, linux-amd64, linux-arm64") {
			t.Fatalf("unsupported host: %v", err)
		}
	}
	a, _ := archiveFor(target.ModernTools, "linux-amd64")
	t.Setenv(MirrorEnv, "")
	if downloadURL(target.ModernTools, a) != "https://github.com/anza-xyz/platform-tools/releases/download/v1.57/platform-tools-linux-x86_64.tar.bz2" {
		t.Fatal(downloadURL(target.ModernTools, a))
	}
	t.Setenv(MirrorEnv, "file:///srv/mirror/")
	if downloadURL(target.ModernTools, a) != "file:///srv/mirror/v1.57/platform-tools-linux-x86_64.tar.bz2" {
		t.Fatal(downloadURL(target.ModernTools, a))
	}
	// Receipts always record the official source, whatever the mirror.
	if officialURL(target.ModernTools, a) != "https://github.com/anza-xyz/platform-tools/releases/download/v1.57/platform-tools-linux-x86_64.tar.bz2" {
		t.Fatal(officialURL(target.ModernTools, a))
	}
}
