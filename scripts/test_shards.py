#!/usr/bin/env python3
"""Run the root module tests (`make test`) as shards of concurrent processes.

Every top-level test of every package is assigned to exactly one of N shards
(--shard i/N) by greedy bin-packing on this platform's durations in
scripts/test_timings.json (measured in CI); tests without a recorded duration
go to the lightest shard. Tests are read from each package's test files
(`go list`), and the script fails unless the shards partition them exactly.
Within a shard the tests are spread over one go test process per CPU, longest
first: most of these tests launch processes, and a
Go process launches children one at a time on macOS, so t.Parallel alone
does not use the cores there.

Extra arguments go to every go test invocation (e.g. -count=1, -v).
--update-timings PLATFORM LOG... rewrites PLATFORM's timings from the
"--- PASS: TestX (1.23s)" lines of -v runs of this script.
"""
import argparse, concurrent.futures, json, os, pathlib, platform, re, subprocess, sys, threading

ROOT = pathlib.Path(__file__).resolve().parents[1]
TIMINGS = ROOT / 'scripts/test_timings.json'


def go_flags(extra):
    flags = ['-timeout', '30m'] + extra
    if platform.system() == 'Darwin':
        flags.append('-ldflags=-linkmode=external')
    return flags


def list_tests():
    """(package, name) for every top-level test, fuzz target and example of
    every package, read from the test files `go test` would compile."""
    out = subprocess.run(['go', 'list', '-json', './...'], cwd=ROOT, check=True, capture_output=True, text=True).stdout
    tests = []
    for pkg in decode_stream(out):
        for name in pkg.get('TestGoFiles', []) + pkg.get('XTestGoFiles', []):
            source = code_only(pathlib.Path(pkg['Dir'], name).read_text())
            pattern = r'^func ((Test|Fuzz)(\w*))\(\w+ \*testing\.[TF]\)|^func (Example(\w*))\(\)'
            for m in re.finditer(pattern, source, re.M):
                name, suffix = (m.group(1), m.group(3)) if m.group(1) else (m.group(4), m.group(5))
                # go test's rule: the suffix must not start with a lower-case letter.
                if not suffix or not suffix[0].islower():
                    tests.append((pkg['ImportPath'], name))
    return tests


def code_only(src):
    """src with comments and string/rune literals blanked (newlines kept), so
    Go source embedded in test strings is not mistaken for tests."""
    out, i, n = [], 0, len(src)
    while i < n:
        c = src[i]
        if src.startswith('//', i):
            j = src.find('\n', i)
            j = n if j < 0 else j
        elif src.startswith('/*', i):
            j = src.index('*/', i) + 2
        elif c == '`':
            j = src.index('`', i + 1) + 1
        elif c in '"\'':
            j = i + 1
            while src[j] != c:
                j += 2 if src[j] == '\\' else 1
            j += 1
        else:
            out.append(c)
            i += 1
            continue
        out.append(re.sub(r'[^\n]', ' ', src[i:j]))
        i = j
    return ''.join(out)


def decode_stream(text):
    d, i = json.JSONDecoder(), 0
    while i < len(text):
        while i < len(text) and text[i].isspace():
            i += 1
        if i == len(text):
            break
        obj, i = d.raw_decode(text, i)
        yield obj


def pack(items, bins, weight):
    """Greedy longest-first bin-packing; unweighted items go to the lightest bin."""
    loads, out = [0.0] * bins, [[] for _ in range(bins)]
    known = sorted((i for i in items if weight(i) is not None), key=lambda i: (-weight(i), i))
    for item in known + [i for i in items if weight(i) is None]:
        b = loads.index(min(loads))
        out[b].append(item)
        loads[b] += weight(item) or 0
    return out, loads


def update_timings(platform_name, logs):
    times, pkg = {}, None
    for path in logs:
        for line in open(path, errors='replace'):
            line = re.sub(r'^.*?Z ', '', line.rstrip('\n'))  # GitHub log timestamps
            if m := re.match(r'^=== (\S+) \(\d+ tests\): exit', line):
                pkg = m.group(1)
            elif (m := re.match(r'^--- (?:PASS|FAIL|SKIP): (\w+) \(([\d.]+)s\)', line)) and pkg:
                times[f'{pkg} {m.group(1)}'] = float(m.group(2))
    data = json.loads(TIMINGS.read_text()) if TIMINGS.exists() else {}
    data[platform_name] = dict(sorted(times.items()))
    TIMINGS.write_text(json.dumps(dict(sorted(data.items())), indent=1) + '\n')
    print(f'wrote {len(times)} {platform_name} timings to {TIMINGS}')


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--shard', default='1/1', help='i/N: run the i-th of N shards')
    ap.add_argument('--jobs', type=int, default=os.cpu_count() or 2, help='concurrent go test processes')
    ap.add_argument('--update-timings', nargs='+', metavar=('PLATFORM', 'LOG'))
    a, extra = ap.parse_known_args()
    if a.update_timings:
        return update_timings(a.update_timings[0], a.update_timings[1:])
    index, total = map(int, a.shard.split('/'))
    if not 1 <= index <= total:
        sys.exit(f'bad --shard {a.shard}')
    flags = go_flags(extra)
    goos, goarch = subprocess.run(['go', 'env', 'GOOS', 'GOARCH'], check=True, capture_output=True, text=True).stdout.split()
    timings = json.loads(TIMINGS.read_text()).get(f'{goos}-{goarch}', {})

    def weight(unit):
        known = [timings[k] for k in (f'{p} {n}' for p, n in unit) if k in timings]
        return sum(known) if known else None

    tests = list_tests()
    shards, loads = pack([(t,) for t in tests], total, weight)
    if sorted(t for s in shards for u in s for t in u) != sorted(tests) or len(set(tests)) != len(tests):
        sys.exit('shards do not partition the test list')
    mine = shards[index - 1]
    print(f'shard {index}/{total}: {sum(map(len, mine))} of {len(tests)} tests, ~{loads[index - 1]:.0f} s recorded '
          f'(shards: {", ".join(f"{l:.0f}" for l in loads)})', flush=True)

    # One process per (bucket, package); buckets are packed like shards.
    buckets, _ = pack(mine, min(a.jobs, len(mine)) or 1, weight)
    procs = []
    for bucket in buckets:
        by_pkg = {}
        for unit in bucket:
            for pkg, test in unit:
                by_pkg.setdefault(pkg, []).append(test)
        for pkg, names in by_pkg.items():
            procs.append((weight([(pkg, n) for n in names]) or 0, pkg, names))
    procs.sort(key=lambda p: -p[0])
    lock, failed = threading.Lock(), []

    def run(proc):
        _, pkg, names = proc
        cmd = ['go', 'test'] + flags + ['-run', '^(' + '|'.join(sorted(names)) + ')$', pkg]
        r = subprocess.run(cmd, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        with lock:
            print(f'=== {pkg} ({len(names)} tests): exit {r.returncode}', flush=True)
            sys.stdout.write(r.stdout)
            sys.stdout.flush()
            if r.returncode:
                failed.append(pkg)

    with concurrent.futures.ThreadPoolExecutor(max_workers=a.jobs) as pool:
        list(pool.map(run, procs))
    if failed:
        sys.exit('failed: ' + ', '.join(sorted(failed)))


if __name__ == '__main__':
    main()
