# Pointer and method implementation plan

This plan is now implemented and verified within the documented pointer and byte
view subset; see [pointer semantics](GO_POINTERS.md) and [view semantics](GO_VIEWS.md). The
[completion audit](MILESTONE2_ACCEPTANCE.md) tracks the full milestone.

Support value and pointer receivers on the existing scalar/value-struct/array
subset, including direct method calls and method expressions. A value receiver
copies its value; a pointer receiver can update caller-owned storage. Implement
Go's implicit address/dereference rules using type-checker selections and retain
cross-package method identity. Reject method values, interfaces and recursion.

Pointers must carry lifetime origins through assignments, function arguments,
forwarded calls and multiple results. Derive per-result borrow summaries for
nonrecursive functions. Local address roots cannot escape the owning function or
lexical block; returned references may borrow from caller parameters. Reject
stores that extend a reference beyond its storage scope, and reject unproven
escapes. Conservative rejection is preferable to silently changing Go lifetimes.
Do not allocate or promise Go heap behavior for escaped locals.

Start with pointers to unsigned/bool scalars, supported structs and scalar arrays.
Keep pointer fields, pointer-element arrays/slices and pointers-to-pointers out
until their storage/escape policies are implemented. Addressing supported locals,
fields and elements must retain identity and alias updates. Account-data byte
views require explicit bounds/capacity and borrow rules, especially across CPI
and lifecycle changes; a cached decoded snapshot must never overwrite CPI state.

Nil checks and left-hand captures must preserve source evaluation order. For
stores, evaluate pointer/index operands and RHSs before left-to-right assignments;
a failing later dereference may follow earlier local effects. Address creation,
loads and value-receiver calls have their own dereference timing. Test against
ordinary Go rather than inferring behavior from generated C.

Required tests include mutable aliases, value receiver copies, nil pointer
receivers that handle nil, nil implicit value receivers, fields/array elements,
receiver evaluation order, cross-package calls, returned caller borrows, local
and block/loop escapes, pointer forwarding through tuple results, recursion and
source-located rejected forms. Compile to real SBF with successful state updates,
exact failures and multi-instruction rollback; measure frames for the fixtures.
Extend safe array/slice views only with the same lifetime/bounds evidence.
