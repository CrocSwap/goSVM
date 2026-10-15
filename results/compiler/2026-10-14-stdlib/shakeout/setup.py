#!/usr/bin/env python3
"""Builds the library-shakeout workspace with the REAL standard library.

This is pass 1 of results/compiler/2026-10-14-library-shakeout (the same
library versions, files and entry package), with no stand-ins: the copies
import errors, math, encoding/binary, strconv, strings, fmt, io, math/big
and the rest from the standard library. The only edits are the module
import paths rewritten to the copies.

    python3 setup.py <modcache> <workspace>

<modcache> is a module cache holding the four library versions (for
example GOMODCACHE after `go mod download` of them); <workspace> must not
exist.
"""
import os, pathlib, shutil, sys

HERE = pathlib.Path(__file__).resolve().parent
SHAKEOUT = HERE.parents[1] / '2026-10-14-library-shakeout'

LIBRARIES = {
    'uint256': ('github.com/holiman/uint256@v1.3.1', '', ['uint256.go', 'div.go', 'mod.go', 'decimal.go', 'conversion.go']),
    'uint128': ('lukechampine.com/uint128@v1.3.0', '', ['uint128.go']),
    'int256': ('github.com/!kyber!network/int256@v0.1.4', '', ['int256.go', 'conversion.go', 'knuth_d.go']),
    'constants': ('github.com/!kyber!network/uniswapv3-sdk-uint256@v0.5.5', 'constants', ['constants.go']),
    'utils': ('github.com/!kyber!network/uniswapv3-sdk-uint256@v0.5.5', 'utils',
              ['full_math.go', 'int_types.go', 'max_liquidity_for_amounts.go', 'most_significant_bit.go',
               'sqrtprice_math.go', 'swap_math.go', 'tick_math.go', 'uint256.go']),
}

REWRITES = {
    '"github.com/holiman/uint256"': '"shake/uint256"',
    '"github.com/KyberNetwork/int256"': '"shake/int256"',
    '"github.com/KyberNetwork/uniswapv3-sdk-uint256/constants"': '"shake/constants"',
    '"lukechampine.com/uint128"': '"shake/uint128"',
}


def main():
    modcache, workspace = map(pathlib.Path, sys.argv[1:3])
    if workspace.exists():
        sys.exit(f'{workspace} exists')
    workspace.mkdir(parents=True)
    (workspace / 'go.mod').write_text('module shake\n\ngo 1.22\n')
    for pkg, (module, sub, files) in LIBRARIES.items():
        (workspace / pkg).mkdir()
        for name in files:
            text = (modcache / module / sub / name).read_text()
            for old, new in REWRITES.items():
                text = text.replace(old, new)
            (workspace / pkg / name).write_text(text)
    (workspace / 'main').mkdir()
    shutil.copy(SHAKEOUT / 'entry.go.txt', workspace / 'main' / 'program.go')


if __name__ == '__main__':
    main()
