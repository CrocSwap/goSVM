#!/usr/bin/env python3
"""Package an offline, existing-host milestone-2 developer trial."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import tarfile

ROOT = Path(__file__).resolve().parents[1]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load(path):
    return json.loads(path.read_text())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--cli', required=True, type=Path)
    args = parser.parse_args()
    out = args.output.resolve()
    if out.exists():
        raise RuntimeError('choose a new output directory')
    out.mkdir(parents=True)
    kit = out / 'milestone2-trial'
    kit.mkdir()
    (kit / 'tools').mkdir()
    (kit / 'expected').mkdir()
    shutil.copyfile(args.cli, kit / 'tools/gosvm')
    (kit / 'tools/gosvm').chmod(0o755)
    archive = 'gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz'
    shutil.copyfile(ROOT / 'results/svm/2026-10-05-runner-install-complete' / archive, kit / 'tools' / archive)
    for name in ('full-swap', 'escrow', 'full-swap-service'):
        shutil.copytree(ROOT / 'examples' / name, kit / 'examples' / name,
                        ignore=shutil.ignore_patterns('build', '__pycache__'))
    docs = ('SDK2.md', 'SCHEMA2.md', 'SVM_FIXTURES.md', 'GO_ARRAYS.md', 'GO_PACKAGES.md',
            'GO_CONTROL.md', 'GO_POINTERS.md', 'GO_VIEWS.md', 'MILESTONE2_TRIAL.md')
    (kit / 'docs').mkdir()
    for doc in docs:
        shutil.copyfile(ROOT / 'docs' / doc, kit / 'docs' / doc)
    for file in ('README.md', 'ROADMAP.md', 'THIRD_PARTY_NOTICES'):
        shutil.copyfile(ROOT / file, kit / file)
    shutil.copyfile(ROOT / 'scripts/milestone2_trial_verify.py', kit / 'verify.py')
    swap_proof = ROOT / 'results/compiler/2026-10-06-framework-swap-managed'
    managed_proof = ROOT / 'results/compiler/2026-10-06-managed-swap-supported'
    escrow_proof = ROOT / 'results/compiler/2026-10-06-escrow-token-lifecycle'
    suites = {}
    for label, project, fixture, report in (
        ('swap-preloaded', 'full-swap', 'testdata/svm.json', swap_proof / 'project-core.json'),
        ('swap-lifecycle', 'full-swap', 'testdata/lifecycle.json', managed_proof / 'svm-0.json'),
        ('escrow-lifecycle', 'escrow', 'testdata/svm.json', escrow_proof / 'svm-0.json'),
    ):
        expected = 'expected/' + label + '.json'
        shutil.copyfile(report, kit / expected)
        suites[label] = {'project': project, 'fixture': fixture, 'expected': expected}
    service_inputs = {}
    checks = load(swap_proof / 'summary.json')['checks']
    for mode, label in (('legacy', 'independent-service-run'), ('managed', 'independent-managed-service-run')):
        command = next(c['command'] for c in checks if c['check'] == label)
        service_inputs[mode] = {'args': command[command.index('-pool') - (1 if mode == 'managed' else 0):],
                                'expected': load(swap_proof / (label + '.log'))}
    (kit / 'service-inputs.json').write_text(json.dumps(service_inputs, indent=2) + '\n')
    (kit / 'TRIAL.md').write_text('''# goSVM milestone 2 trial

Start with [the trial procedure](docs/MILESTONE2_TRIAL.md), then the
[swap](examples/full-swap/README.md), [escrow](examples/escrow/README.md) and
[ordinary Go service](examples/full-swap-service/README.md). This archive needs
no goSVM checkout, Cargo, registry downloads, RPC or wallet. Go 1.22, macOS arm64
and installed platform-tools v1.51 LLVM are prerequisites.

From this directory, run:

```sh
python3 verify.py --llvm /absolute/path/to/platform-tools/llvm
```

The verifier uses an isolated Go/goSVM cache, installs the included runner,
checks/builds/tests both application modules, runs all packaged SBF suites and
tests/runs the separate ordinary Go service. Commands and expected reports are
inspectable. Fresh results go to `trial-results/`; `--output` selects another new
directory. No downloads or public transactions occur.

For manual CLI use, set `GOSVM_CACHE` to the same absolute directory reported by
the verifier and put this kit's `tools` directory on PATH. Use `-llvm` with build
and SBF tests. The native CLI path handles the macOS Go 1.22 external-linking
workaround. `gosvm test --svm` retains generation/native/build/actual-SBF phases.

Explain an invalid-authority and a rollback case from the SBF reports. Complete
`REPORT.md`, including assistance and unsupported-feature feedback. Automated
passing results alone do not establish an independent developer trial.

The app READMEs retain references to historical repository experiments; those
reports are outside this small archive. The baseline reports needed here are
under `expected/`. These examples are experimental; the managed swap gives its
creator authority to drain and close, and escrow allows creator cancellation.
''')
    (kit / 'REPORT.md').write_text('''# Independent developer trial report

- Developer identity and experience:
- Prior involvement in goSVM/compiler/application implementation:
- Date, machine, Go version, LLVM version:
- Kit manifest SHA-256:
- Commands run and results directory:
- Could both applications be built/tested from documentation? Evidence:
- Invalid-authority case and expected policy:
- Failed-CPI or multi-instruction rollback case and observed state:
- Service use of shared types, codecs and quote:
- Compiler/C edits, manual framework offsets or copied types/math required:
- Maintainer assistance, workarounds and unclear instructions:
- Unsupported features encountered (or none):
- Optional application changes and patch:
- Issues preventing independent use:
''')
    files = {str(p.relative_to(kit)): sha(p) for p in sorted(kit.rglob('*')) if p.is_file()}
    source_paths = [ROOT / 'go.mod', ROOT / 'scripts/build-cli.sh', Path(__file__).resolve(),
                    ROOT / 'scripts/milestone2_trial_verify.py']
    for folder in ('cmd/gosvm', 'internal', 'sdk', 'solana'):
        source_paths.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    manifest = {'schema': 1, 'files': files, 'suites': suites,
                'runner_archive': 'tools/' + archive,
                'elf_sha256': {'full-swap': load(swap_proof / 'summary.json')['elf_sha256'],
                               'escrow': load(escrow_proof / 'summary.json')['elf_sha256']},
                'frontend_source_sha256': {str(p.relative_to(ROOT)): sha(p) for p in sorted(set(source_paths))},
                'scope': 'Existing-host independent private-alpha workflow trial; maintainer rehearsal is not acceptance'}
    (kit / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    target = out / 'milestone2-trial.tar.gz'
    with tarfile.open(target, 'w:gz') as stream:
        stream.add(kit, arcname=kit.name)
    (out / 'package.json').write_text(json.dumps({'archive_sha256': sha(target), 'archive_bytes': target.stat().st_size,
        'manifest_sha256': sha(kit / 'manifest.json'), 'files': len(files), 'external_trial_complete': False}, indent=2) + '\n')
    print(target)


if __name__ == '__main__':
    main()
