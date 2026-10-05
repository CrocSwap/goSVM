#!/usr/bin/env python3
"""Generate a live kernel catalog plus independent protocol reference vectors."""
import copy
import hashlib
import json
from pathlib import Path
import struct
import subprocess

ROOT = Path(__file__).resolve().parents[1]
MASK = (1 << 64) - 1
AUTH = bytes.fromhex("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a")


def write_changed(path, text):
    # Regenerating Go must not invalidate Rust's native integration links when
    # the Rust catalog and fixtures are identical.
    if not path.exists() or path.read_text() != text:
        path.write_text(text)


def constant(k, r):
    return ((0x9e3779b97f4a7c15 * (k * 8 + r + 1)) & MASK) | 1


def mix(k, x):
    for r in range(8):
        x = ((x ^ (x >> (17 + r))) * constant(k, r)) & MASK
    return x


def get(b, offset):
    return int.from_bytes(b[offset:offset + 8], "little")


def put(b, offset, n):
    b[offset:offset + 8] = n.to_bytes(8, "little")


def command(tag, nonce=7, cursor=0, count=0, seed=123, digest=bytes(32)):
    return bytearray(struct.pack("<5Q", tag, nonce, cursor, count, seed) + digest)


def model(state, cfg, data, ix, mutation="", accounts=4):
    s = bytearray(state)
    def fail(code): return code, bytearray(state)
    if accounts != 4: return fail(101)
    if mutation == "alias": return fail(105)
    if (len(s), len(cfg), len(data), len(ix)) != (256, 64, 2048, 72): return fail(102)
    if mutation in ("wrong-owner", "config-owner", "payload-owner", "readonly"): return fail(103)
    if mutation == "no-signer" or s[:32] != AUTH or cfg[:32] != AUTH: return fail(104)
    if not 1 <= get(cfg, 32) <= 16 or get(cfg, 40) != 256 or get(cfg, 48) > 1000: return fail(106)
    tag, nonce, cursor, count, seed = struct.unpack("<5Q", ix[:40])
    if nonce != get(s, 72): return fail(201)
    phase, calls = get(s, 64), get(s, 192)
    if calls == MASK: return fail(202)
    if tag == 0:
        if phase != 0: return fail(203)
        h = hashlib.sha256(data).digest()
        if h != ix[40:]: return fail(204)
        s[128:160] = h; s[32:64] = h
        put(s, 64, 1); put(s, 80, 0); put(s, 88, seed)
    elif tag == 1:
        if phase != 1: return fail(203)
        if cursor != get(s, 80) or cursor > 256 or not 1 <= count <= get(cfg, 32) or count > 256-cursor: return fail(205)
        fee = count * get(cfg, 48)
        if get(s, 200) < fee: return fail(206)
        h = hashlib.sha256(data).digest()
        if h != s[32:64]: return fail(204)
        s[128:160] = h
        x = get(s, 88)
        for i in range(cursor, cursor + count):
            w = get(data, i*8); x = mix(w >> 58, x ^ w)
        put(s, 80, cursor + count); put(s, 88, x); put(s, 200, get(s, 200)-fee)
        if cursor + count == 256: put(s, 64, 2)
    elif tag in (2, 3):
        if (tag == 2 and phase not in (1, 2)) or (tag == 3 and phase != 2): return fail(203)
        h = hashlib.sha256(s[64:96]).digest()
        if h != ix[40:]: return fail(207)
        s[128:160] = h; s[96:128] = h
        if tag == 3: put(s, 64, 3)
    elif tag == 4:
        if phase != 3: return fail(203)
        if nonce == MASK: return fail(208)
        put(s, 64, 0); put(s, 72, nonce+1); put(s, 80, 0); put(s, 88, 0)
        s[32:64] = bytes(32); s[96:128] = bytes(32)
    else: return fail(209)
    put(s, 192, calls + 1)
    return 0, s


def main():
    go = [(ROOT / "benchmarks/protocol/go-body.txt").read_text()]
    rust = ["// Generated live dispatch catalog. All arms are exercised by the lifecycle.\n"]
    dispatch_go = ["func dispatch(k,x uint64) uint64 {"]
    dispatch_rust = ["pub fn dispatch(k:u64,x:u64)->u64 {match k {"]
    for k in range(64):
        go.append(f"func kernel{k}(x uint64) uint64 {{")
        rust.append(f"fn kernel{k}(mut x:u64)->u64 {{")
        for r in range(8):
            go.append(f"x=(x^(x>>{17+r}))*{constant(k,r)}")
            rust.append(f"x=(x^(x>>{17+r})).wrapping_mul({constant(k,r)});")
        go.append("return x\n}"); rust.append("x}")
        dispatch_go.append(f"if k=={k} {{return kernel{k}(x)}}")
        dispatch_rust.append(f"{k}=>kernel{k}(x),")
    dispatch_go.append("return 0\n}"); dispatch_rust.append("_=>0}}")
    dest = ROOT / "examples/protocol"
    dest.mkdir(parents=True, exist_ok=True)
    formatted = subprocess.run(["gofmt"], input="\n".join(go + dispatch_go) + "\n", text=True, capture_output=True, check=True).stdout
    write_changed(dest / "protocol.go", formatted)
    write_changed(ROOT / "benchmarks/protocol/rust/src/kernels.rs", "\n".join(rust + dispatch_rust) + "\n")
    state = bytearray(256); state[:32] = AUTH; put(state, 72, 7); put(state, 200, 1_000_000)
    cfg = bytearray(64); cfg[:32] = AUTH; put(cfg, 32, 16); put(cfg, 40, 256); put(cfg, 48, 5)
    data = bytearray().join(((i % 64 << 58) | ((i * 1125899906842597 + 17) & ((1 << 58)-1))).to_bytes(8,"little") for i in range(256))
    cases = []
    def add(name, s, ix, c=cfg, p=data, mutation="", accounts=4):
        code, expected = model(s, c, p, ix, mutation, accounts)
        case = dict(name=name, state=list(s), config=list(c), payload=list(p), instruction=list(ix), expected=list(expected), code=code, mutation=mutation, accounts=accounts)
        cases.append(case)
        return expected
    initial = state[:]
    state = add("commit", state, command(0, digest=hashlib.sha256(data).digest()))
    committed = state[:]
    lifecycle = [0]
    for i in range(16):
        state = add(f"step-{i:02}", state, command(1, cursor=i*16, count=16))
        lifecycle.append(len(cases)-1)
        if i == 7:
            state = add("checkpoint", state, command(2, digest=hashlib.sha256(state[64:96]).digest()))
            lifecycle.append(len(cases)-1)
    ready = state[:]
    state = add("finalize", state, command(3, digest=hashlib.sha256(state[64:96]).digest())); lifecycle.append(len(cases)-1)
    finalized = state[:]
    state = add("reset", state, command(4)); lifecycle.append(len(cases)-1)
    # Each kernel also gets a one-record boundary case, using maximal accumulator.
    for i in range(64):
        s = committed[:]; put(s,80,i);put(s,88,MASK)
        add(f"kernel-{i:02}",s,command(1,cursor=i,count=1))
    for mut in ("wrong-owner","config-owner","payload-owner","readonly","no-signer","alias"):
        add(mut,committed,command(1,count=1),mutation=mut)
    for n in (0,3,5):add(f"accounts-{n}",committed,command(1,count=1),accounts=n)
    add("short-state",committed[:-1],command(1,count=1))
    add("short-config",committed,command(1,count=1),c=cfg[:-1])
    add("short-payload",committed,command(1,count=1),p=data[:-1])
    add("short-instruction",committed,command(1,count=1)[:-1])
    add("long-instruction",committed,command(1,count=1)+b"\x00")
    s=committed[:];s[0]^=1;add("authority",s,command(1,count=1))
    c=cfg[:];c[0]^=1;add("config-authority",committed,command(1,count=1),c=c)
    c=cfg[:];put(c,32,17);add("config-limit",committed,command(1,count=1),c=c)
    add("nonce",committed,command(1,nonce=8,count=1))
    s=committed[:];put(s,192,MASK);add("counter-overflow",s,command(1,count=1))
    add("phase",initial,command(1,count=1))
    add("commit-digest",initial,command(0))
    add("step-zero",committed,command(1,count=0))
    add("step-large",committed,command(1,count=17))
    add("cursor",committed,command(1,cursor=1,count=1))
    s=committed[:];put(s,80,255);add("past-payload",s,command(1,cursor=255,count=2))
    s=committed[:];put(s,200,4);add("credits",s,command(1,count=1))
    p=data[:];p[0]^=1;add("payload-substitute",committed,command(1,count=1),p=p)
    add("checkpoint-digest",committed,command(2))
    add("finalize-early",committed,command(3))
    add("finalize-digest",ready,command(3))
    s=finalized[:];put(s,72,MASK);add("nonce-overflow",s,command(4,nonce=MASK))
    add("unknown-tag",committed,command(255))
    dest = ROOT / "build/protocol"
    dest.mkdir(parents=True, exist_ok=True)
    write_changed(dest / "fixtures.json", json.dumps({"authority":list(AUTH),"cases":cases,"lifecycle":lifecycle})+"\n")
    tests=ROOT / "benchmarks/protocol/harness/tests"
    tests.mkdir(exist_ok=True)
    for group in range(12):
        write_changed(tests / f"group{group:02}.rs", f"mod support;\n#[tokio::test]\nasync fn group_{group}() {{support::run({group}).await;}}\n")
    print(f"Generated 64 live kernels, {len(cases)} reference cases, {len(lifecycle)} lifecycle instructions, 12 VM test binaries")


if __name__ == "__main__":
    main()
