#!/usr/bin/env python3
"""Run from an extracted milestone-2 trial kit; no compiler checkout needed."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import time


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def main():
    root = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--llvm', required=True, type=Path)
    parser.add_argument('--output', type=Path, default=root / 'trial-results')
    args = parser.parse_args()
    out = args.output.resolve()
    require(not out.exists(), 'choose a new trial results directory')
    out.mkdir(parents=True)
    manifest = json.loads((root / 'manifest.json').read_text())
    summary = {'passed': False, 'independent_developer_acceptance': False,
               'machine': platform.platform(), 'manifest_sha256': sha(root / 'manifest.json'),
               'checks': [], 'reports': {},
               'limitations': ['Automated rehearsal does not establish independent developer feedback',
                               'Local installed-tool trial; no fresh-host, release or deployment claim']}
    env = dict(os.environ, GOCACHE=str(out / 'go-cache'), GOSVM_CACHE=str(out / 'gosvm-cache'),
               GOWORK='off', GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local')
    for name in ('GOFLAGS', 'SBF_LLVM', 'GOSVM_TEST_RUNNER', 'GOSVM_TEST_FEATURES'):
        env.pop(name, None)
    cli = root / 'tools/gosvm'

    def run(label, command, cwd=root):
        command = [str(v) for v in command]
        log = out / (label + '.log')
        start = time.perf_counter()
        print(label + ': ' + repr(command), flush=True)
        with log.open('wb') as stream:
            result = subprocess.run(command, cwd=cwd, env=env, stdout=stream,
                                    stderr=subprocess.STDOUT, timeout=240)
        summary['checks'].append({'label': label, 'command': command, 'cwd': str(cwd),
                                  'exit_code': result.returncode,
                                  'wall_seconds': time.perf_counter() - start, 'log': log.name})
        require(result.returncode == 0, 'command failed: ' + str(log))
        return log

    try:
        for file, digest in manifest['files'].items():
            require(sha(root / file) == digest, 'kit file differs: ' + file)
        require(args.llvm.resolve().is_dir(), 'LLVM directory missing')
        run('go-version', ['go', 'version'])
        run('runner-install', [cli, 'runner', 'install', '-archive', root / manifest['runner_archive']])
        run('runner-status', [cli, 'runner', 'status'])
        for name in ('full-swap', 'escrow'):
            project = root / 'examples' / name
            run(name + '-check', [cli, 'check'], project)
            run(name + '-native', [cli, 'test', '--', '-count=1'], project)
            run(name + '-build', [cli, 'build', '-llvm', args.llvm.resolve()], project)
            require(sha(project / 'build/program.so') == manifest['elf_sha256'][name],
                    'rebuilt ELF differs: ' + name)
        for label, info in manifest['suites'].items():
            project = root / 'examples' / info['project']
            run(label, [cli, 'test', '--svm', '-llvm', args.llvm.resolve(),
                        '-svm-fixtures', info['fixture'], '--', '-count=1'], project)
            actual = json.loads((project / 'build/svm-results.json').read_text())
            expected = json.loads((root / info['expected']).read_text())
            require(actual['cases'] == expected['cases'], 'state/error/CU/log mismatch: ' + label)
            require(actual['elf_sha256'] == manifest['elf_sha256'][info['project']], 'wrong ELF')
            require(actual['runner_sha256'] == expected['runner_sha256'], 'wrong runner')
            target = out / (label + '.json')
            target.write_text(json.dumps(actual, indent=2) + '\n')
            summary['reports'][label] = {'report': target.name, 'scenarios': len(actual['cases']),
                                         'elf_sha256': actual['elf_sha256']}
        service = root / 'examples/full-swap-service'
        run('service-tests', ['go', 'test', '-ldflags=-linkmode=external', './...', '-count=1'], service)
        deps = run('service-deps', ['go', 'list', '-deps', './...'], service).read_text().splitlines()
        require(not any(p.startswith('gosvm/') for p in deps), 'service imports runtime/compiler SDK')
        for name, case in json.loads((root / 'service-inputs.json').read_text()).items():
            log = run('service-' + name, ['go', 'run', '-ldflags=-linkmode=external', '.', *case['args']], service)
            require(json.loads(log.read_text()) == case['expected'], 'service output differs: ' + name)
        # Check that generation neither changed layouts nor consumed an ambient SDK.
        for file, digest in manifest['files'].items():
            if file.startswith('examples/'):
                require(sha(root / file) == digest, 'application/generated artifact changed: ' + file)
        summary['passed'] = True
        print('PASS: both application workflows and the ordinary Go service. Complete REPORT.md.', flush=True)
    except Exception as error:
        summary['failure'] = str(error)
        raise
    finally:
        (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')


if __name__ == '__main__':
    main()
