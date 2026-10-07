#!/usr/bin/env python3
"""Measure complete developer workflows with installed tools and isolated projects."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import re
import resource
import shutil
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
TOKEN = ROOT / 'results/svm/2026-10-05-general-complete'
SCENARIOS = ('project_clean', 'no_op', 'source_edit', 'source_edit_uncached_native', 'test_edit', 'uncached_native')


def load(path):
    return json.loads(path.read_text())


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def size(path):
    if path.is_file():
        return path.stat().st_size
    return sum(p.stat().st_size for p in path.rglob('*') if p.is_file() and not p.is_symlink())


def run(command, log, *, env, cwd=ROOT):
    started = time.perf_counter()
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    load_before = os.getloadavg()
    with log.open('w') as out:
        result = subprocess.run([str(x) for x in command], cwd=cwd, env=env,
                                stdout=out, stderr=subprocess.STDOUT, timeout=300)
    seconds = time.perf_counter() - started
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    if result.returncode:
        raise RuntimeError(f'Command failed ({result.returncode}); see {log}')
    return {'wall_seconds': seconds,
            'cpu_seconds': after.ru_utime + after.ru_stime - before.ru_utime - before.ru_stime,
            'load_average_before': load_before, 'log': str(log.relative_to(ROOT)),
            'command': [str(x) for x in command], 'cwd': str(cwd)}


def timings(log):
    values = [json.loads(line.removeprefix('gosvm timings: '))
              for line in log.read_text().splitlines() if line.startswith('gosvm timings: ')]
    assert len(values) == 1 and values[0]['schema'] == 1 and values[0]['passed']
    value = values[0]
    assert sum(value['phase_seconds'].values()) <= value['total_seconds']
    return value


def distribution(values):
    return {'median_seconds': statistics.median(values), 'min_seconds': min(values),
            'max_seconds': max(values), 'samples_seconds': values}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--samples', type=int, default=5)
    parser.add_argument('--runner', type=Path,
                        default=ROOT / 'build/svm-fixtures-target/release/gosvm-svm-runner')
    parser.add_argument('--llvm', type=Path,
                        default=Path.home() / '.cache/solana/v1.51/platform-tools/llvm')
    args = parser.parse_args()
    if not 3 <= args.samples <= 20:
        parser.error('--samples requires 3..20')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    stage = ROOT / 'build/svm-workflows' / output.name
    stage.mkdir(parents=True, exist_ok=False)
    runner, llvm = args.runner.resolve(), args.llvm.resolve()
    assert runner.is_file()
    sources = [ROOT / 'scripts/svm_workflow.py', ROOT / 'Makefile']
    for directory in ('cmd/gosvm', 'internal/compiler', 'internal/project', 'internal/sbftest', 'internal/testvm',
                      'solana', 'examples/tokenswap', 'benchmarks/anchor/go-token', 'benchmarks/svm-runner', 'third_party/litesvm-0.8.2'):
        sources.extend(p for p in (ROOT / directory).rglob('*') if p.is_file() and p.suffix != '.md')
    source_hashes = {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}
    # A new local cache measures frontend bootstrap, then remains the shared
    # native cache. Nothing clears the user's installed tools or global caches.
    env = dict(os.environ, GOCACHE=str(stage / 'go-cache'), GOWORK='off', GOPROXY='off',
               GOSVM_TEST_FEATURES=str(TOKEN / 'validator-features.json'), SBF_LLVM=str(llvm))
    cli = stage / 'gosvm'
    setup = {'frontend_fresh_cache': run(['bash', 'scripts/build-cli.sh', cli], output / 'cli-cold.log', env=env)}
    setup['frontend_cached'] = run(['bash', 'scripts/build-cli.sh', cli], output / 'cli-warm.log', env=env)
    setup['native_cache_after_frontend_bytes'] = size(stage / 'go-cache')
    setup['runner_version'] = subprocess.check_output([str(runner), '--version'], text=True).strip()
    assert setup['runner_version'] == 'gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)'
    setup['go_version'] = subprocess.check_output(['go', 'version'], text=True).strip()
    setup['clang_version'] = subprocess.check_output([str(llvm / 'bin/clang'), '--version'], text=True).splitlines()[0]
    setup['host_cargo_cached_build'] = run(['cargo', 'build', '--release', '--locked', '--offline',
        '--manifest-path', 'benchmarks/svm-runner/Cargo.toml', '--target-dir', 'build/svm-fixtures-target'],
        output / 'runner-cached-build.log', env=env)
    projects = {}
    starter = stage / 'starter'
    setup['scaffold'] = run([cli, 'new', '-module', 'example.com/typed-swap', starter], output / 'scaffold.log', env=env)
    # Full token remains a manually wired workflow. Native tests exercise the
    # original source. SBF uses the already validated Anchor wire adaptation of
    # the same business logic and SDK, matching the 108-scenario corpus exactly.
    full = stage / 'token'
    (full / 'original').mkdir(parents=True)
    (full / 'contract').mkdir()
    (full / 'go.mod').write_text('module gosvm\n\ngo 1.22\n')
    shutil.copytree(ROOT / 'solana', full / 'solana')
    for name in ('swap.go', 'swap_test.go'):
        shutil.copyfile(ROOT / 'examples/tokenswap' / name, full / 'original' / name)
    shutil.copyfile(ROOT / 'benchmarks/anchor/go-token/swap.go', full / 'contract/swap.go')
    for name, path, source, test_file in (
            ('bounded', starter, starter / 'program.go', starter / 'program_test.go'),
            ('full_token', full, full / 'original/swap.go', full / 'original/swap_test.go')):
        projects[name] = {'dir': path, 'source': source, 'test': test_file,
                          'source_original': source.read_text(), 'test_original': test_file.read_text()}
    projects['full_token']['adapter_original'] = (full / 'contract/swap.go').read_text()
    # Snapshot generated inputs before edits, so evidence remains reproducible
    # even if another agent changes the checkout while this script is running.
    input_files = list(starter.rglob('*')) + list(full.rglob('*'))
    save(output / 'input-files.json', {str(p.relative_to(stage)): digest(p)
                                      for p in sorted(input_files) if p.is_file()})
    save(output / 'edit-plan.json', {
        'source_edit': 'bounded: rename effective -> effectiveInput; token original+adapter: rename net -> netInput',
        'test_edit': 'append a new zero-input assertion with a unique test name each repetition',
        'scope': 'all edits are in isolated staging; original repository programs stay unchanged'})
    references = {}
    rows = []

    def build_state(name):
        project = projects[name]
        elf = project['dir'] / 'build' / ('program.so' if name == 'bounded' else 'token.so')
        receipt = Path(str(elf) + '.gosvm-cache')
        return {'elf_exists': elf.exists(), 'elf_sha256': digest(elf) if elf.exists() else None,
                'elf_mtime_ns': elf.stat().st_mtime_ns if elf.exists() else None,
                'receipt': load(receipt) if receipt.exists() else None}

    def execute(name, scenario, index, *, profile=True, first=False):
        project = projects[name]
        path = project['dir']
        label = f'{name}-{scenario}-{index}'
        before = build_state(name)
        commands = []
        count_args = ['-count=1'] if scenario in ('uncached_native', 'source_edit_uncached_native') else []
        if name == 'bounded':
            log = output / (label + '.log')
            command = [cli, 'test', '--svm', '-dir', path, '-llvm', llvm, '-svm-runner', runner]
            if profile:
                command += ['-timings']
            if count_args:
                command += ['--', *count_args]
            commands.append(run(command, log, env=env))
            phases = timings(log)['phase_seconds'] if profile else {}
            if not profile:
                assert 'gosvm timings: ' not in log.read_text()
            native_cached = '(cached)' in log.read_text()
            report_path = path / 'build/svm-results.json'
        else:
            native = output / (label + '-native.log')
            commands.append(run(['go', 'test', './original', *count_args], native, cwd=path, env=env))
            native_cached = '(cached)' in native.read_text()
            commands.append(run([cli, '-arch', 'v3', '-llvm', llvm, '-o', path / 'build/token.so', path / 'contract'],
                                output / (label + '-build.log'), env=env))
            report_path = path / 'build/svm-results.json'
            commands.append(run([cli, 'svm-test', '-elf', path / 'build/token.so', '-fixtures', TOKEN / 'token-fixtures.json',
                                 '-runner', runner, '-report', report_path], output / (label + '-svm.log'), env=env))
            phases = {'native_tests': commands[0]['wall_seconds'], 'sbf_build_command': commands[1]['wall_seconds'],
                      'svm_test_command': commands[2]['wall_seconds']}
        report = load(report_path)
        after = build_state(name)
        cases = report['cases']
        assert len(cases) == (14 if name == 'bounded' else 108)
        if name not in references:
            references[name] = cases
            if name == 'full_token':
                assert cases == load(TOKEN / 'general-0.json')['cases']
                assert after['elf_sha256'] == load(TOKEN / 'general-0.json')['elf_sha256']
        assert cases == references[name], 'A benchmark edit changed runtime outcomes/CU/logs'
        assert report['runner_sha256'] == digest(runner)
        assert report['elf_sha256'] == after['elf_sha256']
        if not first:
            assert after['elf_sha256'] == expected_elf[name]
            if scenario in ('project_clean', 'source_edit', 'source_edit_uncached_native'):
                assert before['elf_mtime_ns'] != after['elf_mtime_ns'], 'Expected SBF rebuild did not happen'
            if scenario in ('no_op', 'test_edit', 'uncached_native', 'unprofiled', 'profiled_control'):
                assert before['elf_mtime_ns'] == after['elf_mtime_ns'], 'SBF cache unexpectedly invalidated'
            if scenario == 'test_edit':
                assert not native_cached, 'Test edit did not invalidate native result cache'
            if scenario in ('uncached_native', 'source_edit_uncached_native'):
                assert not native_cached, 'Native tests did not execute under -count=1'
        shutil.copyfile(report_path, output / (label + '-results.json'))
        wall = sum(r['wall_seconds'] for r in commands)
        row = {'workload': name, 'scenario': scenario, 'sample': index,
               'wall_seconds': wall, 'cpu_seconds': sum(r['cpu_seconds'] for r in commands),
               'phases_seconds': phases, 'native_test_result_cached': native_cached,
               'svm_startup_seconds': report['startup_seconds'], 'svm_fixture_seconds': report['fixture_seconds'],
               'svm_total_seconds': report['total_seconds'],
               'vm_execution_seconds': report['runner']['execution_us'] / 1e6,
               'before': before, 'after': after, 'commands': commands}
        save(output / (label + '-timing.json'), row)
        print(f'{label}: {wall:.3f}s native_cached={native_cached}', flush=True)
        return row

    expected_elf = {}
    for name in projects:
        first = execute(name, 'first_project', 0, first=True)
        setup[name + '_first_project'] = first
        expected_elf[name] = first['after']['elf_sha256']
    rng = random.Random(20261005)
    order = []
    for scenario in SCENARIOS:
        for sample in range(args.samples):
            names = list(projects)
            rng.shuffle(names)
            for name in names:
                project = projects[name]
                if scenario == 'project_clean':
                    shutil.rmtree(project['dir'] / 'build')
                elif scenario in ('source_edit', 'source_edit_uncached_native'):
                    suffix = ('Forced' if scenario == 'source_edit_uncached_native' else '') + str(sample)
                    replacement = 'effectiveInput' + suffix if name == 'bounded' else 'netInput' + suffix
                    original = project['source_original']
                    changed = re.sub(r'\beffective\b' if name == 'bounded' else r'\bnet\b', replacement, original)
                    assert changed != original
                    project['source'].write_text(changed)
                    if name == 'full_token':
                        (full / 'contract/swap.go').write_text(re.sub(r'\bnet\b', replacement, project['adapter_original']))
                elif scenario == 'test_edit':
                    test = (f'\nfunc TestWorkflowZeroAmount{sample}(t *testing.T) {{\n'
                            + ('if program.Swap(program.Pool{ReserveX:1, ReserveY:2}, program.SwapArgs{}).Code != program.ErrInvalidAmount {t.Fatal("zero input accepted")}\n'
                               if name == 'bounded' else 'if Quote(1,2,0) != 0 {t.Fatal("zero input quote")}\n') + '}\n')
                    project['test'].write_text(project['test_original'] + test)
                order.append({'scenario': scenario, 'sample': sample, 'workload': name})
                rows.append(execute(name, scenario, sample))
    # Pair both modes on unchanged inputs, shuffling mode order within each pair.
    # This limits ordering bias when checking optional diagnostic overhead.
    controls = []
    paired_controls = []
    for sample in range(args.samples):
        modes = [False, True]
        rng.shuffle(modes)
        pair = {}
        for profile in modes:
            mode = 'profiled_control' if profile else 'unprofiled'
            row = execute('bounded', mode, sample, profile=profile)
            controls.append(row)
            pair[mode] = row['wall_seconds']
        pair['order'] = modes
        pair['profiled_minus_unprofiled_seconds'] = pair['profiled_control'] - pair['unprofiled']
        paired_controls.append(pair)
    # Finish with original source/test files and matching receipts, then preserve
    # those current reports separately from measured edit snapshots.
    for name, project in projects.items():
        project['source'].write_text(project['source_original'])
        project['test'].write_text(project['test_original'])
        if name == 'full_token':
            (full / 'contract/swap.go').write_text(project['adapter_original'])
        execute(name, 'restored', 0, first=True)
    groups = []
    for name in projects:
        for scenario in SCENARIOS:
            selected = [r for r in rows if r['workload'] == name and r['scenario'] == scenario]
            phases = {key: distribution([r['phases_seconds'][key] for r in selected])
                      for key in selected[0]['phases_seconds']}
            groups.append({'workload': name, 'scenario': scenario,
                           'wall': distribution([r['wall_seconds'] for r in selected]),
                           'cpu': distribution([r['cpu_seconds'] for r in selected]),
                           'phases': phases,
                           'svm_startup': distribution([r['svm_startup_seconds'] for r in selected]),
                           'svm_fixture': distribution([r['svm_fixture_seconds'] for r in selected]),
                           'vm_execution': distribution([r['vm_execution_seconds'] for r in selected]),
                           'native_cached_count': sum(r['native_test_result_cached'] for r in selected)})
    assert source_hashes == {str(p.relative_to(ROOT)): digest(p) for p in sorted(set(sources))}, 'Checkout sources changed during measurement'
    save(output / 'summary.json', {'schema': 1, 'passed': True, 'samples': args.samples, 'machine': platform.platform(),
         'setup': setup, 'groups': groups, 'sampling_order': order, 'seed': 20261005,
         'unprofiled_control': distribution([r['wall_seconds'] for r in controls if r['scenario'] == 'unprofiled']),
         'profiled_control': distribution([r['wall_seconds'] for r in controls if r['scenario'] == 'profiled_control']),
         'paired_controls': paired_controls,
         'source_sha256': source_hashes,
         'baseline_token_fixtures_sha256': digest(TOKEN / 'token-fixtures.json'),
         'feature_profile_sha256': digest(TOKEN / 'validator-features.json'),
         'artifacts': {p.name: {'bytes': size(p), 'sha256': digest(p)} for p in (cli, runner, llvm / 'bin/clang', llvm / 'bin/ld.lld')},
         'footprint': {'bounded_project_build_bytes': size(starter / 'build'), 'token_project_build_bytes': size(full / 'build'),
                       'shared_native_go_cache_bytes': size(stage / 'go-cache'),
                       'runner_host_target_bytes': size(ROOT / 'build/svm-fixtures-target')},
         'limitations': ['Shared desktop, shuffled serial measurements; no portable latency guarantee',
                         'Installed tools and cached crate sources; downloads/backend installation excluded',
                         'Runner cached build measured; cold runner bootstrap is the earlier compatibility-spike observation',
                         'Project clean means empty project build outputs with native Go cache retained',
                         'Full token workflow tests original native business logic and builds the matched wire adapter separately',
                         'No fresh full-validator latency measurement: local RPC binding is restricted',
                         'Dependency-heavy protocol and external program benchmark not run; Basanos untouched']})
    print(f'PASS: {len(rows)} measured complete workflows plus startup/control/restoration checks. {output}', flush=True)


if __name__ == '__main__':
    main()
