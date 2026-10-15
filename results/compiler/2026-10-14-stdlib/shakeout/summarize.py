#!/usr/bin/env python3
"""Summarizes a peel run: surviving functions per package and the messages.

    python3 summarize.py <original workspace> <peeled workspace> <log.jsonl>

Counts top-level functions and methods per package before and after peeling
and groups the log's messages: a message that names a declaration removed
earlier is a cascade; the rest are root causes, grouped by message pattern.
"""
import collections, json, pathlib, re, sys


def functions(ws):
    counts = collections.Counter()
    for pkg in ['uint256', 'int256', 'uint128', 'utils']:
        for f in (ws / pkg).glob('*.go'):
            counts[pkg] += len(re.findall(r'(?m)^func ', f.read_text()))
    return counts


def main():
    orig, peeled, log = map(pathlib.Path, sys.argv[1:4])
    before, after = functions(orig), functions(peeled)
    entries = [json.loads(line) for line in log.read_text().splitlines() if line.strip()]
    removed = set()
    groups = collections.Counter()
    examples = {}
    for e in entries:
        msg = e['msg']
        cascade = 'undefined:' in msg or 'has no field or method' in msg or 'declared and not used' in msg or 'imported and not used' in msg
        key = 'cascade' if cascade else re.sub(r'"[^"]*"|\b\d+\b|[A-Za-z_]+\.[A-Za-z_]+', '…', msg)[:110]
        groups[key] += 1
        examples.setdefault(key, (e['file'], e['decl'], msg))
        removed.add(e['decl'])
    out = {'removals': len(entries), 'functions': {p: {'before': before[p], 'after': after[p]} for p in before},
           'groups': [{'count': n, 'pattern': k, 'example': examples[k]} for k, n in groups.most_common()]}
    print(json.dumps(out, indent=1))


if __name__ == '__main__':
    main()
