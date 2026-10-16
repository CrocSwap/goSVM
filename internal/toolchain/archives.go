package toolchain

import (
	"fmt"
	"os"
	"sort"
	"strings"

	target "gosvm/internal/targets"
)

// OfficialBase is where Anza publishes platform-tools release assets. goSVM
// never redistributes these archives; it downloads them from here (or a mirror)
// and verifies the pinned size and SHA-256 before decompressing anything.
const OfficialBase = "https://github.com/anza-xyz/platform-tools/releases/download"

// MirrorEnv names an optional base URL (https://, http:// or file://) laid out
// like OfficialBase (<base>/<version>/<asset>), for mirrors, caches and tests.
// Archives from a mirror pass the same size, SHA-256 and content checks;
// receipts keep recording the official URL.
const MirrorEnv = "GOSVM_PLATFORM_TOOLS_BASE_URL"

// officialArchive is one official platform-tools release asset for one host.
type officialArchive struct {
	Asset string
	SHA   string
	Size  int64
}

// Sizes and SHA-256s come from the GitHub release asset digests of
// anza-xyz/platform-tools and were confirmed by downloading every archive
// (October 16, 2026; darwin-arm64 earlier). Host names are GOOS-GOARCH.
var officialArchives = map[string]map[string]officialArchive{
	Version: {
		"darwin-arm64": {"platform-tools-osx-aarch64.tar.bz2", "a1e32b3137fe8199fa76da81c43ec0122bdbd27acf1279f38d8d265f7a6e6cb3", 448012397},
		"linux-amd64":  {"platform-tools-linux-x86_64.tar.bz2", "0933e05dd964826e8e2db5c58c34ae255e3baf0ce1711568552dca81b5407a6b", 542447828},
		"linux-arm64":  {"platform-tools-linux-aarch64.tar.bz2", "e281c6b38320e58ef6e827f6ca6b9c4914825f8ede29cfc2f3daa38585b760bb", 535788934},
	},
	target.ModernTools: {
		"darwin-arm64": {"platform-tools-osx-aarch64.tar.bz2", "48c32c2ec3ac3729b5caf1fdd6c4145496125edf043b2662b0586bfbc932b34a", 430747800},
		"linux-amd64":  {"platform-tools-linux-x86_64.tar.bz2", "b0f7af104adf726fff2a6a09ea2eb2f2d2965c92295f4d7388c08d140e0c2b00", 531380716},
		"linux-arm64":  {"platform-tools-linux-aarch64.tar.bz2", "f2f31c9325cb2d00244df23b4a1a0904e51819cec49d39235a3ed3ee2320375d", 528839404},
	},
}

// hostArchive returns the pinned official archive of version for this host.
func hostArchive(version string) (officialArchive, error) {
	return archiveFor(version, target.Host())
}

func archiveFor(version, host string) (officialArchive, error) {
	a, ok := officialArchives[version][host]
	if !ok {
		var hosts []string
		for h := range officialArchives[version] {
			hosts = append(hosts, h)
		}
		sort.Strings(hosts)
		return a, fmt.Errorf("no official platform-tools %s archive is pinned for host %s (supported hosts: %s); set SBF_LLVM to an existing backend", version, host, strings.Join(hosts, ", "))
	}
	return a, nil
}

// officialURL is the official download URL recorded in receipts.
func officialURL(version string, a officialArchive) string {
	return OfficialBase + "/" + version + "/" + a.Asset
}

// downloadURL is where the archive is fetched from: the mirror when set.
func downloadURL(version string, a officialArchive) string {
	if base := strings.TrimRight(os.Getenv(MirrorEnv), "/"); base != "" {
		return base + "/" + version + "/" + a.Asset
	}
	return officialURL(version, a)
}
