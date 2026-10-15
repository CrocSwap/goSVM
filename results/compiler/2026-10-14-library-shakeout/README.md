# Real-library shakeout (2026-10-14)

Question: which missing Go features block existing, unmodified pure-Go
libraries from compiling on chain? The answer sets the order of the remaining
core-language and standard-library work.

## Libraries

| Library | Version | Files used |
| --- | --- | --- |
| `github.com/holiman/uint256` | v1.3.1 | `uint256.go div.go mod.go decimal.go conversion.go` |
| `lukechampine.com/uint128` | v1.3.0 | `uint128.go` |
| `github.com/KyberNetwork/int256` | v0.1.4 | `int256.go conversion.go knuth_d.go` |
| `github.com/KyberNetwork/uniswapv3-sdk-uint256` | v0.5.5 | `constants/constants.go`; `utils/` `full_math int_types max_liquidity_for_amounts most_significant_bit sqrtprice_math swap_math tick_math uint256` |

The Uniswap-v3 files that only build EVM calldata, pool addresses or decimal
prices were left out. The sources were copied into one scratch module, `shake`.
The only edits were import paths rewritten to the copies. In pass 2, `errors`,
`math` and `encoding/binary` were also rewritten to the stand-ins in `std/`.
Those stand-ins are stored here as `.txt` so the root module does not build them:

- `errors`: `New` and `Is`.
- `math`: integer limit constants.
- `encoding/binary`: the byte-order part of Go 1.22's `binary.go`, unchanged.

[`entry.go.txt`](entry.go.txt) is the pass-2 entry package. It refers to the
library types and the main Uniswap-v3 math functions.

## Method

[`peel/`](peel/main.go) runs `gosvm features ./main` in the workspace and reads
the first check error, taking the innermost position when an import error is
nested. It then deletes the top-level declaration, spec or import that contains
that position, and repeats until the program checks.

Each log line records the declaration removed and the compiler's message. Many
lines are cascades, where a declaration was removed only because something it
used had been removed earlier. Every on-chain package is checked in full
(including unused helpers), so the log covers every declaration in the files.

```sh
go build -o peel ./peel
./peel <gosvm> <workspace> ./main > log.jsonl
```

Compiler: `main` at `764e4a9`.

## Results

- [Pass 1](pass1-real-stdlib.jsonl), with the real standard library: 248
  removals, mostly standard-library imports and their cascades.
- [Pass 2](pass2-std-standins.jsonl), with the stand-ins: 252 removals, then
  the remainder checks.

Functions that survive pass 2:

| Package | Functions that check |
| --- | --- |
| uint256 | 89 of 155 (all core arithmetic: Add/Sub/Mul/Div/Mod/MulMod/Exp/shifts/comparisons, Knuth division) |
| int256 | 51 of 71 |
| uint128 | 29 of 50 |
| uniswapv3 utils | 13 of 40 (MulDiv helpers; the tick/sqrt-price/swap-step functions fall to the blockers below) |

Root causes in pass 2, excluding cascades:

| Blocker | Examples | Effect |
| --- | --- | --- |
| Package-level variables | `var ErrX = errors.New(...)`; `binary.BigEndian`; lookup tables; `var sqrtConst1 = uint256.MustFromHex(...)` | Largest. It blocks every `encoding/binary` use, every sentinel error, and every Uniswap tick/sqrt-price/swap function. |
| Standard library | `errors`, `math` constants, `encoding/binary`; also `fmt`, `strconv`, `io`, `strings` and `math/big`, which are used only for text/big conversions | Pass 1 shows these first. |
| Type aliases | `type Uint160 = uint256.Int` | Blocks all of the Uniswap utils API. |
| `panic` | `uint128.Add` panics on overflow | Blocks uint128 arithmetic. |
| Struct `==` | `u == Zero` in uint128 | Blocks IsZero/Equals/Cmp, then QuoRem/Div/Mod. |
| Arrays of composite elements | `[...]Int{...}` tables in uint256/int256 decimal code | Blocks decimal parsing and `Log10`. |
| Floats | `math.Sqrt`, `Float64` | Out of scope by decision. |

The `.jsonl` logs are raw reports and stay local (`.gitignore`); rerun `peel`
to regenerate them. Counts and root causes are summarized above.

Passing the checker does not establish runtime correctness. That still needs
differential tests against native Go once the blockers are removed.
