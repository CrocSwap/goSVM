#!/usr/bin/env python3
"""Run the root module tests (`make test`) as concurrent go test processes.

internal/compiler holds almost all of the test time and is mostly process
launches (host oracles, Clang, runners). One test process launches children
one at a time on macOS, so its tests are split round-robin across --shards
processes, alongside one process for every other package. The shards are
checked to cover exactly the package's test list. Extra arguments go to every
go test invocation (e.g. -count=1).
"""
import argparse, os, platform, subprocess, sys, tempfile

HEAVY = 'gosvm/internal/compiler'


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--shards', type=int, default=os.cpu_count() or 2)
    a, extra = ap.parse_known_args()
    flags = ['-timeout', '30m'] + extra
    if platform.system() == 'Darwin':
        flags.append('-ldflags=-linkmode=external')

    listed = subprocess.run(['go', 'test'] + flags + ['-list', '.', HEAVY], check=True, capture_output=True, text=True).stdout
    tests = sorted(l for l in listed.split() if l.startswith('Test'))
    if not tests:
        sys.exit(f'no tests listed for {HEAVY}')
    shards = [tests[i::a.shards] for i in range(a.shards) if tests[i::a.shards]]
    if sorted(t for s in shards for t in s) != tests:
        sys.exit('shards do not cover the test list exactly')
    others = [p for p in subprocess.run(['go', 'list', './...'], check=True, capture_output=True, text=True).stdout.split() if p != HEAVY]

    jobs = [('other packages', ['go', 'test'] + flags + others)]
    for i, s in enumerate(shards):
        jobs.append((f'{HEAVY} shard {i + 1}/{len(shards)} ({len(s)} tests)',
                     ['go', 'test'] + flags + ['-run', '^(' + '|'.join(s) + ')$', HEAVY]))
    with tempfile.TemporaryDirectory() as tmp:
        running = []
        for i, (name, cmd) in enumerate(jobs):
            log = open(os.path.join(tmp, f'{i}.log'), 'w+')
            running.append((name, subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT), log))
        failed = []
        for name, proc, log in running:
            proc.wait()
            log.seek(0)
            print(f'=== {name}: exit {proc.returncode}', flush=True)
            sys.stdout.write(log.read())
            log.close()
            if proc.returncode:
                failed.append(name)
    if failed:
        sys.exit('failed: ' + ', '.join(failed))


if __name__ == '__main__':
    main()
