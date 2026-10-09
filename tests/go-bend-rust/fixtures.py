"""Independent model of the trio program: Bend mixed.evaluate then Rust quote."""
import json, struct, sys
POOL = bytes([33, 65, 37, 242, 140, 68, 141, 192])
SWAP = bytes([184, 167, 8, 246, 65, 96, 167, 23])
def pool(reserve, last): return (POOL + struct.pack('<IQ', reserve, last)).hex()
def ix(enabled, value, amount): return (SWAP + struct.pack('<BIQ', enabled, value, amount)).hex()
def model(reserve, last, enabled, value, amount):
    if enabled > 1: return reserve, last, 104
    if not (enabled == 1 and value < 41): return reserve, last, 200
    admitted = value + 2
    if reserve == 0: return reserve, last, 304
    out = (amount * reserve // (admitted + amount)) & (2**64 - 1)
    return reserve, out, 0
cases = []
def case(name, reserve, last, steps):
    state, err, index = (reserve, last), 0, 0
    for i, s in enumerate(steps):
        r, l, e = model(state[0], state[1], *s)
        if e:
            err, index = e, i
            state = (reserve, last)  # whole transaction rolls back
            break
        state = (r, l)
    cases.append({"name": name, "initial": pool(reserve, last), "instructions": [ix(*s) for s in steps],
                  "expected": pool(*state), "error": err, "error_index": index})
case("accepted", 2000, 7, [(1, 10, 100)])
case("boundary-forty", 5000, 7, [(1, 40, 1000)])
case("bend-refused", 2000, 7, [(1, 41, 100)])
case("bend-disabled", 2000, 7, [(0, 3, 100)])
case("bend-malformed-bool", 2000, 7, [(2, 3, 100)])
case("rust-malformed-reserve", 0, 7, [(1, 3, 100)])
case("u128-boundary", 2**32 - 1, 0, [(1, 0, 2**64 - 1)])
case("zero-amount", 2000, 7, [(1, 5, 0)])
case("success-then-malformed-rollback", 2000, 7, [(1, 10, 100), (2, 1, 1)])
json.dump({"schema": 1, "cases": cases}, open(sys.argv[1], "w"), indent=2)
print(len(cases), "cases")
