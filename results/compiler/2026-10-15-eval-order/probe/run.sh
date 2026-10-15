#!/bin/sh
# Builds and runs the probe with the pinned Go 1.22.0 under default flags,
# -gcflags=-l and -gcflags='-N -l', writes each output to $1 (default: a
# temporary directory) and fails when they differ.
set -eu
cd "$(dirname "$0")"
out=${1:-$(mktemp -d)}
mkdir -p "$out"
go version
link=""
if [ "$(uname -s)" = Darwin ]; then link="-ldflags=-linkmode=external"; fi
GOWORK=off go build $link -o "$out/default" .
GOWORK=off go build $link -gcflags=-l -o "$out/noinline" .
GOWORK=off go build $link -gcflags='-N -l' -o "$out/noopt" .
for b in default noinline noopt; do
	if [ "$(uname -s)" = Darwin ]; then codesign --force --sign - "$out/$b" 2>/dev/null; fi
	"$out/$b" > "$out/$b.txt"
done
diff "$out/default.txt" "$out/noinline.txt"
diff "$out/default.txt" "$out/noopt.txt"
echo "identical under all three flag sets: $out/default.txt"
