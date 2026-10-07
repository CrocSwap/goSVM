#!/usr/bin/env python3
"""Record milestone 1 installation/workflow evidence on macOS arm64.

This is also runnable as an installed-host rehearsal. A successful run does not
certify a clean host or replace the independent fresh-validator acceptance gate.
No Cargo, Rust, validator, or prebuilt application ELF is needed.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
RESULTS = ROOT / 'results/svm'


def load(path):
    return json.loads(path.read_text())


def digest(path):
    sha = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            sha.update(chunk)
    return sha.hexdigest()


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True, help='new evidence directory; never overwritten')
    parser.add_argument('--runner-archive', type=Path, required=True)
    backend = parser.add_mutually_exclusive_group(required=True)
    backend.add_argument('--backend-archive', type=Path, help='official pinned v1.51 archive, installed offline')
    backend.add_argument('--download-backend', action='store_true', help='explicitly allow the pinned 448 MB download')
    backend.add_argument('--existing-backend', type=Path, help='managed backend root with receipt; rehearsal only')
    parser.add_argument('--host-note', default='Host cleanliness not established.',
                        help='record host/VM provenance and prerequisites; reviewed separately from test results')
    args = parser.parse_args()
    require(platform.system() == 'Darwin' and platform.machine() == 'arm64', 'macOS arm64 is the accepted host subset')
    archive = args.runner_archive.resolve()
    pin = load(ROOT / 'internal/runner/pins/darwin-arm64.json')
    require(digest(archive) == pin['archive_sha256'] and archive.stat().st_size == pin['archive_bytes'],
            'runner archive does not match the frontend pin')
    go = shutil.which('go')
    require(go is not None, 'Go 1.22.0 must be installed for source bootstrap')
    output = args.output.resolve()
    stage = ROOT / 'build/milestone1-acceptance' / output.name
    require(not output.exists() and not stage.exists(), 'choose a new output name; evidence/staging already exists')
    output.mkdir(parents=True)
    stage.mkdir(parents=True)
    # Record executable source, including embedded templates/SDK/pins. Historical
    # evidence is hashed separately and is never rewritten by this workflow.
    sources = [Path(__file__).resolve(), ROOT / 'scripts/install.sh', ROOT / 'scripts/build-cli.sh',
               ROOT / 'go.mod', ROOT / 'benchmarks/anchor/go-token/swap.go',
               ROOT / 'examples/tokenswap/swap.go', ROOT / 'benchmarks/svm-controls/probe.c']
    for folder in ('cmd/gosvm', 'internal', 'solana'):
        sources.extend(p for p in (ROOT / folder).rglob('*') if p.is_file() and p.suffix != '.md')
    hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}
    summary = {'schema': 2, 'passed': False, 'machine': platform.platform(), 'host_note': args.host_note,
               'checks': [], 'source_sha256': hashes, 'evidence_sha256': {}, 'artifacts': {},
               'runner_archive_sha256': digest(archive),
               'backend_mode': 'existing managed copy (rehearsal)' if args.existing_backend else 'pinned installation',
               'acceptance_gates': {'clean_host': 'requires separate host-provenance review',
                                    'final_fresh_validator': 'not executed by this script'},
               'scope': 'Installation/workflow evidence only; milestone status is recorded in ROADMAP.md',
               'limitations': ['Current-host rehearsal is not clean-machine verification',
                               'Historical expected cases are replayed; no fresh validator oracle here',
                               'Public release/signing/licenses and wider hosts are separate milestone 3 work',
                               'Bounded starter and classic-token programs only; dependency-heavy protocol not run']}
    # Do not let caller overrides select a different backend/runner, enable
    # workspace dependencies, or reuse a native test cache from this desktop.
    env = dict(os.environ)
    for name in ('SBF_LLVM', 'SBF_TOOLS', 'GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES',
                 'GOFLAGS', 'GOROOT', 'GOOS', 'GOARCH', 'GOTOOLCHAIN'):
        env.pop(name, None)
    env.update(GOSVM_CACHE=str(stage / 'cache'), GOCACHE=str(stage / 'go-cache'),
               GOMODCACHE=str(stage / 'go-mod-cache'), GOWORK='off', GOPROXY='off', GOENV='off',
               GO111MODULE='on', CGO_ENABLED='1')

    def run(label, command, *, selected_env=env, expected=0, contains=None, timeout=300):
        command = [str(x) for x in command]
        start = time.perf_counter()
        with (output / (label + '.log')).open('wb') as log:
            result = subprocess.run(command, cwd=ROOT, env=selected_env, stdout=log,
                                    stderr=subprocess.STDOUT, timeout=timeout)
        summary['checks'].append({'check': label, 'command': command, 'exit_code': result.returncode,
                                  'expected': expected, 'wall_seconds': time.perf_counter() - start,
                                  'log': label + '.log'})
        require(result.returncode == expected, f'{label}: unexpected exit {result.returncode}; see {output / (label + ".log")}')
        if contains:
            require(contains in (output / (label + '.log')).read_text(), f'{label}: missing diagnostic {contains!r}')
        print(f'{label}: PASS', flush=True)

    def reference(path):
        summary['evidence_sha256'][str(path.relative_to(ROOT))] = digest(path)
        return load(path)

    def check_report(path, expected, elf):
        actual = load(path)
        require(actual['cases'] == expected['cases'], f'{path.name}: case/state/error/CU/log results changed')
        require(actual['elf_sha256'] == expected['elf_sha256'] == digest(elf), f'{path.name}: compiled ELF changed')
        require(actual['runner_sha256'] == pin['files']['bin/gosvm-svm-runner']['sha256'], f'{path.name}: wrong runner')
        if 'program_sha256' in expected:
            require(actual['program_sha256'] == expected['program_sha256'], f'{path.name}: dependency image changed')
        summary['artifacts'][path.name] = {'sha256': digest(path), 'bytes': path.stat().st_size}
        summary['artifacts'][elf.name] = {'sha256': digest(elf), 'bytes': elf.stat().st_size}
        return actual

    try:
        run('go-version', [go, 'version'], contains='go1.22.0 darwin/arm64')
        run('source-install', ['bash', 'scripts/install.sh', stage / 'bin'])
        cli = stage / 'bin/gosvm'
        tools = stage / 'test-path'
        tools.mkdir()
        (tools / 'go').symlink_to(go)
        test_env = dict(env, PATH=str(tools), CGO_ENABLED='0')
        summary['test_path_programs'] = ['go']
        summary['cli_sha256'] = digest(cli)
        managed = stage / 'cache/toolchains/v1.51/darwin-arm64'
        if args.existing_backend:
            shutil.copytree(args.existing_backend.resolve(), managed)
            summary['acceptance_gates']['clean_host'] = 'not eligible: copied an existing backend'
        else:
            command = [cli, 'toolchain', 'install']
            if args.backend_archive:
                backend_archive = args.backend_archive.resolve()
                summary['backend_archive_sha256'] = digest(backend_archive)
                command.extend(['-archive', backend_archive])
            run('backend-install', command, selected_env=test_env, timeout=900)
        run('backend-status', [cli, 'toolchain', 'status'], selected_env=test_env)
        shutil.copyfile(managed / 'receipt.json', output / 'backend-receipt.json')
        run('runner-install', [cli, 'runner', 'install', '-archive', archive], selected_env=test_env)
        run('runner-status', [cli, 'runner', 'status'], selected_env=test_env)
        run('runner-idempotent', [cli, 'runner', 'install'], selected_env=test_env, contains='already installed and verified')
        runner_root = stage / 'cache/runners/0.5.0/darwin-arm64'
        require(load(runner_root / 'receipt.json') == pin, 'runner receipt differs from frontend pin')
        shutil.copyfile(runner_root / 'receipt.json', output / 'runner-receipt.json')
        project = stage / 'starter'
        run('scaffold', [cli, 'new', '-module', 'example.com/milestone1/acceptance', project], selected_env=test_env)
        run('doctor', [cli, 'doctor', '-dir', project], selected_env=test_env, contains='OK optional SVM runner')
        run('starter', [cli, 'test', '--svm', '-dir', project, '-timings', '--', '-count=1'], selected_env=test_env)
        shutil.copyfile(project / 'build/svm-results.json', output / 'starter.json')
        expected = reference(RESULTS / '2026-10-05-workflow-complete/bounded-restored-0-results.json')
        # The manifest supplies the program filename; the report pins its bytes.
        starter_elf = next((project / 'build').glob('*.so'))
        bounded = check_report(output / 'starter.json', expected, starter_elf)
        # General fixtures are an explicit example, not part of the default
        # scaffold. Copy that corpus rather than depending on an old project build.
        fixture = ROOT / 'examples/typed-swap/testdata/svm.json'
        reference(fixture)
        shutil.copyfile(fixture, project / 'testdata/svm.json')
        run('project-general', [cli, 'test', '--svm', '-dir', project, '-svm-fixtures', project / 'testdata/svm.json',
                                '--', '-count=1'], selected_env=test_env)
        shutil.copyfile(project / 'build/svm-results.json', output / 'project-general.json')
        expected = reference(RESULTS / '2026-10-05-general-complete/project-general.json')
        check_report(output / 'project-general.json', expected, starter_elf)
        general = RESULTS / '2026-10-05-general-complete'
        lifecycle = RESULTS / '2026-10-05-lifecycle-verified'
        controls = RESULTS / '2026-10-05-scenarios-complete'
        suites = [('full-token', ROOT / 'benchmarks/anchor/go-token/swap.go', general / 'token-fixtures.json', general / 'general-0.json'),
                  ('lifecycle', ROOT / 'examples/tokenswap/swap.go', lifecycle / 'fixtures.json', lifecycle / 'runner-results.json'),
                  ('sysvars', None, controls / 'fixtures.json', controls / 'ordered-results.json')]
        counts = {'bounded': len(bounded['cases'])}
        for label, source, fixture, baseline in suites:
            elf = output / (label + '.so')
            if source:
                run(label + '-build', [cli, '-arch', 'v3', '-no-cache', '-o', elf, source], selected_env=test_env)
            else:
                llvm = managed / 'llvm/bin'
                run('probe-compile', [llvm / 'clang', '-target', 'sbf', '-mcpu=v3', '-O2', '-fno-builtin', '-fPIC',
                    '-fno-stack-protector', '-std=c11', '-Werror', '-c', ROOT / 'benchmarks/svm-controls/probe.c',
                    '-o', stage / 'probe.o'], selected_env=test_env)
                run('probe-link', [llvm / 'ld.lld', '-z', 'notext', '-shared', '--Bdynamic', '--strip-all', '--entry',
                    'entrypoint', '--script', ROOT / 'internal/compiler/sbf-v3.ld', '--no-undefined', '-o', elf,
                    stage / 'probe.o'], selected_env=test_env)
            expected = reference(baseline)
            reference(fixture)
            suite_env = dict(test_env)
            if label != 'sysvars':
                # These are independently recorded validator feature IDs.
                features = general / 'validator-features.json' if label == 'full-token' else lifecycle / 'validator-features.json'
                reference(features)
                suite_env['GOSVM_TEST_FEATURES'] = str(features)
            report = output / (label + '.json')
            run(label, [cli, 'svm-test', '-elf', elf, '-fixtures', fixture, '-report', report], selected_env=suite_env)
            actual = check_report(report, expected, elf)
            counts[label] = {'scenarios': len(actual['cases']), 'transactions': sum(len(c['steps']) for c in actual['cases'])}
        # The recorded metadata discrepancy remains observable, not normalized.
        rejected_report = output / 'metadata-rejection.json'
        shutil.copyfile(output / 'lifecycle.json', rejected_report)
        run('metadata-rejection', [cli, 'svm-test', '-elf', output / 'lifecycle.so', '-fixtures', lifecycle / 'validator-fixtures.json',
             '-report', rejected_report], selected_env=dict(test_env, GOSVM_TEST_FEATURES=str(lifecycle / 'validator-features.json')),
             expected=1, contains='rent_epoch=0')
        require(not rejected_report.exists(), 'metadata mismatch left a stale report')
        summary['fixture_counts'] = counts
        require(hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}, 'source changed during acceptance run')
        with socket.socket() as probe:
            try:
                probe.bind(('127.0.0.1', 0))
                summary['local_rpc_bind'] = {'allowed': True, 'fresh_validator_executed': False}
            except OSError as error:
                summary['local_rpc_bind'] = {'allowed': False, 'error': str(error), 'fresh_validator_executed': False}
        summary['passed'] = True
        print(f'PASS: installation/workflows; this script does not certify host cleanliness or a fresh validator. {output}', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        (output / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
