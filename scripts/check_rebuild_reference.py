#!/usr/bin/env python3
"""Fail unless every workload in a verify_rebuild.py report matches its reference ELF.

Usage: python3 scripts/check_rebuild_reference.py METRICS.json
Used by CI to qualify a host's backend pins: the same Go source must compile to
the same SBF bytes as the reference build recorded in results/.
"""
import json
import sys

metrics = json.load(open(sys.argv[1]))
bad = [(r['target'], r['workload'], r['elf_sha256']) for r in metrics['results'] if not r.get('matches_reference')]
if not metrics.get('passed') or bad:
    sys.exit(f'rebuild failed or ELFs differ from {metrics.get("reference")}: {bad}')
print(f'{len(metrics["results"])} reference ELFs reproduced byte for byte ({metrics.get("reference")})')
