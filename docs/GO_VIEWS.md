# Borrowed byte slice views

The compiler supports two- and three-index views of existing byte slices and
addressable byte arrays, including pointers to arrays. Views share their backing
storage and retain Go length/capacity behavior. A local array whose view escapes
its function or lexical scope moves to the heap. Views of any element type,
`make`, `append` and the rest of [allocation](GO_ALLOCATION.md) are covered
there; this page describes the original byte views.

## Bounds and capacity

Supported forms include `s[:]`, `s[low:high]`, `s[low:high:maximum]`, array-field
views, pointer-to-array views and reslicing a view within its capacity. The same
forms work for every admitted element type ([allocation](GO_ALLOCATION.md));
named slice types remain rejected.
The existing 1,024-byte per-array limit still applies.

A two-index slice defaults low to zero and high to length, preserving capacity
minus low. A three-index slice limits capacity to maximum minus low. Bounds must
satisfy `low <= high <= maximum <= cap(source)`. For a two-index slice, maximum is
the source capacity. An explicit high may exceed the source length when it fits
capacity, as in ordinary Go:

```go
var key [32]byte
window := key[4:8:16] // length 4, capacity 12
expanded := window[:12]
expanded[11] = 37 // updates key[15]
```

`len` and `cap` return the descriptor's actual length and capacity. Nil slices
have zero length/capacity, compare equal to nil on either side, and retain nil
identity after valid empty slicing. A zero-length array view is a non-nil empty
slice. A nil array pointer panics when sliced, including an empty view.

Bounds use their full width: large uint64 limits fail rather than truncate.
Runtime bound/nil failures abort. Explicit bound calls execute in source order;
the differential corpus checks their effects before failure. Plain variable
reads still follow the compiler's documented snapshot choice where Go leaves
relative evaluation unspecified; see [pointer evaluation](GO_POINTERS.md).

## Storage and lifetimes

Copying an array copies its elements. Slicing an array or copying a slice
preserves its backing-storage identity. Writes through views and element pointers
are visible through other aliases. Slice conversions between identical byte
slices preserve the borrow and descriptor; `[]byte(nil)` creates a nil descriptor
without allocation. Slice-to-array and array-to-slice-pointer conversions remain
unsupported.

The [borrow analysis](GO_POINTERS.md) tracks view origins through declarations,
assignments, returned results, method receivers, forwarded calls and conversions.
A function can return a view of incoming slice or pointed array/struct storage.
Returning a view of a local array or of a copied value parameter/receiver, or an
element pointer derived from it, or keeping a block/loop array's view in an outer
variable, moves that array to the invocation heap instead of failing
([escape rules](GO_ALLOCATION.md#escape-rules)). The conservative union of
origins may move an array that native Go would keep on the stack.

Slice fields, pointer-element slices, `make`, `new` and `append` are now part of
[allocation](GO_ALLOCATION.md); interfaces remain outside the subset. Programs
without allocation link no allocator; borrowed descriptors and multiple-result
aggregates keep their freestanding memory-copy helpers.

## Account buffers and native fixtures

SBF SDK byte getters expose capacity equal to the currently supplied buffer
length. Account data is bounded to its serialized data length, keys/owners/program
IDs to 32 bytes, and instruction data to its explicit length. A view cannot reach
reserved account-growth storage or adjacent loader metadata through reslicing.
Normal native tests construct Context themselves and must supply matching buffer
lengths and capacities. The native reference explicitly bounds decoded account
and instruction slices with full-slice expressions; the decoder's spare host
capacity is not part of the serialized SVM input.

Views do not automatically acquire a new length after CPI or resizing. Generated
framework/lifecycle helpers must define when a view remains usable and read
current account state when needed. Borrowing proof alone is not an account
validation, CPI freshness or custody policy.

The internal descriptor carries capacity only when the reachable source graph
uses slicing, slice `cap` or [allocation](GO_ALLOCATION.md). Historical programs without those features keep
their existing descriptor representation and tested ELFs. That representation is
an internal compiler detail, never an account wire layout or client API.

## Verification and reproduction

[Compiler tests](../internal/compiler/views_test.go) compare 1,000 random
native-Go/generated-C cases and 149 edge/boundary cases. Additional checks cover
SDK capacities, nil arguments/assignments/returns/method calls, a 24-result
borrowed-descriptor aggregate and full 1 KiB backing storage. The local, block,
loop, copied receiver and converted/forwarded escapes once rejected here now move
to the heap ([alloc_moves_test.go](../internal/compiler/alloc_moves_test.go)).

The [shared view model](../examples/slice-views/model/model.go),
[native oracle](../examples/slice-views/program.go) and
[SDK entrypoint](../examples/slice-views/context/program.go) are real Go packages.
[Compiled evidence](../results/compiler/2026-10-05-slice-views-supported/README.md)
records 262 native reference vectors and 264 SBF scenarios in each of three runs.
Exact account bytes/errors, multi-instruction nil/custom-error rollback and real
SHA-256 of nil input into a local array view pass. The 6,280-byte ELF has static
1,024-byte entry and 1,088-byte library-handler frames; hidden memory helpers use
zero-byte frames. Instrumented linking reproduces the tested ELF hash. These
measurements apply to this fixture, not every supported source.

The [installed-host regression](../results/svm/2026-10-05-m2-views-supported-regression/README.md)
retains existing starter/token/lifecycle/sysvar ELF hashes and exact cases.
Pointer and control corpora also retain their tested ELFs and outcomes.
These are fast-runner checks, not fresh-validator or clean-host claims.

```sh
python3 scripts/slice_verify.py --output results/compiler/new-views
```

Choose a new results directory. The recipe builds an isolated CLI and uses the
pinned local runner archive and installed v1.51 LLVM. The earlier fixture-capacity
mistake is preserved as a failed attempt, not counted as passing evidence.
Schema-2 validation/codecs/clients and the reference application gates remain
open in the [milestone audit](MILESTONE2_ACCEPTANCE.md).
