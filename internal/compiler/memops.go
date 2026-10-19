package compiler

import (
	"fmt"
	"strings"
)

// Bulk memory moves on SBF (docs/GO_ALLOCATION.md#bulk-memory-on-sbf).
//
// Every byte move the generated C makes goes through four runtime routines:
// gosvm_move (copy, append and string building), and the hidden memcpy,
// memset and memmove symbols LLVM calls for aggregate assignment, zeroing and
// copies through pointers that may alias. On the fogo-v0 and solana-v3
// profiles, SBF builds (build.go defines GOSVM_MEMORY_SYSCALLS) hand moves of
// at least a threshold number of bytes to the VM's sol_memmove_ and
// sol_memset_ syscalls instead of looping. Host C and target-less legacy
// builds keep the loops unchanged.
//
// sol_memcpy_ is never used: the runtime fails it with CopyOverlapping when
// the ranges overlap, including src == dst, which LLVM's memcpy contract and
// Go's copy both allow. sol_memmove_ costs the same (Agave and Firedancer
// charge every memory syscall max(10, n/250) CU) and has memmove semantics.

// memorySyscallMin is the smallest byte count handed to the syscalls. The
// measurement (results/compiler/2026-10-19-memory-syscalls) found the
// syscall cheaper than the loops it replaces at every count from 1 byte, on
// both profiles and for every alignment: a syscall costs 10 CU plus a few
// instructions, the byte loop about 8 CU per byte and the word loop 6 CU per
// 8 bytes, and with the threshold at 1 LLVM deletes the loops on SBF, which
// shrinks and inlines the routines. Tests that check other thresholds (both
// paths in one program) replace it and restore it; it must be at least 1, so
// a zero-length move never reaches the VM.
var memorySyscallMin uint64 = 1

// memorySyscalls declares the syscalls and the threshold once per C file.
// Every routine that may use them includes it first.
const memorySyscalls = `
#if defined(__bpf__) && defined(GOSVM_MEMORY_SYSCALLS) && !defined(GOSVM_MEMORY_SYSCALL_MOVE)
/* Bulk moves of at least GOSVM_MEMORY_SYSCALL_MIN bytes use the VM's memory
   syscalls (docs/GO_ALLOCATION.md#bulk-memory-on-sbf): sol_memmove_ for every
   copy, never sol_memcpy_, which rejects overlapping ranges. */
#ifdef GOSVM_SBF_V3
#define GOSVM_MEMORY_SYSCALL_MOVE ((void (*)(void *,const void *,u64))0x434371f8ULL)
#define GOSVM_MEMORY_SYSCALL_SET ((void (*)(void *,u64,u64))0x3770fb22ULL)
#else
extern void sol_memmove_(void *,const void *,u64);
extern void sol_memset_(void *,u64,u64);
#define GOSVM_MEMORY_SYSCALL_MOVE sol_memmove_
#define GOSVM_MEMORY_SYSCALL_SET sol_memset_
#endif
#define GOSVM_MEMORY_SYSCALL_MIN @MIN@ULL
#endif
`

func memorySyscallDecls() string {
	if memorySyscallMin == 0 {
		panic("the memory syscall threshold must be at least 1")
	}
	return strings.Replace(memorySyscalls, "@MIN@", fmt.Sprint(memorySyscallMin), 1)
}
