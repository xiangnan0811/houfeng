#!/usr/bin/env python3
"""Offline preparation and owned isolated-VM runner for the C80 source realm."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile

SOURCE_ROOT = Path(__file__).absolute().parent
WORKTREE = SOURCE_ROOT.parents[2]
RUN = WORKTREE / 'tmp/agent/g2-c80-inventory-ddac5da9673f4c9ba7d2ad68570fd82d'
PHASE0 = SOURCE_ROOT.parent / 'phase0'
PHASE0_DATA = RUN.parent / 'g2-isolated-capability-89ff3c589b2b4dc2b9ee8b9c33465b2b'
ASSETS = RUN.parent / 'g2-isolated-capability-f3240b1b4e1c44c8bf3dfcb8087b5757'
TOKEN = 'ddac5da9673f4c9ba7d2ad68570fd82d'
PG_ID = 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610'
PG_REPOSITORY_DIGEST = 'postgres@sha256:23af655ba1ddf74eaa002e3deaf5fce022ab8791672336a7c1fb0ef2d57efb7f'
PG_TAR = RUN / 'assets/postgres-16.12.tar'
CASES = ('realm-negative', 'source-vps', 'source-monitoring-instance', 'source-target', 'source-recovery')
PHASE0_FILES = {
    'host.py': '64d33f3395eb4ac21dce47d1938b0f5704daaa0dbad5fe04c94e42842b57d7a2',
    'prepare_import.py': '6c36207eae0964b6ad1d82c2f8059cae015822dd89589157f1d48cb41e5188e6',
    'preparation.py': '4a57a3ca16405ec3b14debd273687b23c75f1e2a83127b63f6e7492afb4322a7',
    'verify_handoff.py': '31dbaf161a1fc5e1c66c084dfc6c9c656c14110749d246b511a5b965c808a6bb',
}


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False) + '\n').encode('ascii')


def strict_json(data):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    def invalid(_):
        raise Failure('non-finite JSON number')
    try:
        return json.loads(data, object_pairs_hook=pairs, parse_constant=invalid)
    except (UnicodeError, ValueError) as exc:
        raise Failure('invalid JSON') from exc


def digest(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1, 'unsafe input file')
        h = hashlib.sha256()
        size = 0
        while True:
            block = os.read(fd, 1024 * 1024)
            if not block:
                break
            h.update(block)
            size += len(block)
        after = os.fstat(fd)
        require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) and size == before.st_size,
                'input file changed during hashing')
        return h.hexdigest(), size
    finally:
        os.close(fd)


def check_path(path, directory=True):
    path = Path(path)
    require(path.is_absolute() and '..' not in path.parts, 'path is not absolute and normalized')
    current = Path('/')
    for part in path.parts[1:]:
        current /= part
        info = os.lstat(current)
        require(not stat.S_ISLNK(info.st_mode), 'symlink in owned path')
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), 'unexpected path type')


def validate_source_layout() -> None:
    check_path(SOURCE_ROOT)
    check_path(PHASE0)
    require(Path(__file__).absolute() == SOURCE_ROOT / 'source_host.py', 'incorrect source identity')
    check_path(SOURCE_ROOT / 'source_host.py', False)
    for path, expected in PHASE0_FILES.items():
        check_path(PHASE0 / path, False)
        require(digest(PHASE0 / path)[0] == expected, 'frozen phasezero source changed: ' + path)
    manifest_path = PHASE0 / 'candidate-implementation.json'
    check_path(manifest_path, False)
    require(digest(manifest_path)[0] == '195168200dc759f76b3b7049e333cf5f23d2802c81f07389d018477796a3a9af',
            'frozen phasezero manifest changed')
    manifest_data = manifest_path.read_bytes()
    require(hashlib.sha256(manifest_data).hexdigest() == '195168200dc759f76b3b7049e333cf5f23d2802c81f07389d018477796a3a9af',
            'frozen phasezero manifest changed while reading')
    manifest = strict_json(manifest_data)
    require(type(manifest) is dict and type(manifest.get('files')) is dict, 'invalid phasezero manifest')
    for name in ('guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py'):
        path = PHASE0 / name
        check_path(path, False)
        require(type(manifest['files'].get(name)) is str and digest(path)[0] == manifest['files'][name],
                'frozen phasezero carrier changed: ' + name)


def validate_run(argument):
    validate_source_layout()
    require(argument == str(RUN), 'incorrect run directory')
    check_path(RUN)
    info = os.stat(RUN, follow_symlinks=False)
    require(os.geteuid() != 0 and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
            'run directory must be private and owned by the non-root caller')


def frozen_host():
    validate_source_layout()
    sys.path.insert(0, str(PHASE0))
    for name in ('prepare_import', 'preparation', 'verify_handoff'):
        expected = str(PHASE0 / (name + '.py'))
        if name not in sys.modules:
            spec = importlib.util.find_spec(name)
            require(spec is not None and spec.loader is not None and spec.origin == expected and
                    spec.submodule_search_locations is None, 'foreign phasezero module spec: ' + name)
            importlib.import_module(name)
        loaded = sys.modules.get(name)
        require(loaded is not None and getattr(loaded, '__file__', None) == expected and
                getattr(getattr(loaded, '__spec__', None), 'origin', None) == expected,
                'foreign phasezero module: ' + name)
    spec = importlib.util.spec_from_file_location('_g2_frozen_phase0_host', PHASE0 / 'host.py')
    require(spec is not None and spec.loader is not None, 'phasezero host unavailable')
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    module.RUN = RUN
    module.TOKEN = TOKEN
    module.OLD = ASSETS
    return module


def _docker_inspect(env):
    try:
        result = subprocess.run(['/usr/bin/docker', 'image', 'inspect', PG_ID, '--format', '{{json .}}'],
                                cwd=RUN, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=30, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise Failure('local PostgreSQL 16.12 image inspection unavailable') from exc
    require(result.returncode == 0 and len(result.stdout) <= 1024 * 1024 and len(result.stderr) <= 65536,
            'pinned local PostgreSQL image unavailable')
    item = strict_json(result.stdout)
    require(type(item) is dict and item.get('Id') == PG_ID and item.get('Os') == 'linux' and
            item.get('Architecture') == 'amd64' and PG_REPOSITORY_DIGEST in (item.get('RepoDigests') or []) and
            'PG_MAJOR=16' in (item.get('Config', {}).get('Env') or []) and
            'PG_VERSION=16.12-1.pgdg13+1' in (item.get('Config', {}).get('Env') or []),
            'pinned PostgreSQL image identity differs')
    return {'image_id': PG_ID, 'repository_digest': PG_REPOSITORY_DIGEST,
            'os': 'linux', 'architecture': 'amd64', 'version': '16.12-1.pgdg13+1'}


def _tar_member(archive, name, limit):
    require(type(name) is str and name and not name.startswith('/') and '..' not in Path(name).parts,
            'unsafe image archive member')
    try:
        member = archive.getmember(name)
    except KeyError as exc:
        raise Failure('image archive member missing') from exc
    require(member.isfile() and 0 <= member.size <= limit, 'invalid image archive member')
    source = archive.extractfile(member)
    require(source is not None, 'image archive member unreadable')
    with source:
        data = source.read(limit + 1)
    require(len(data) == member.size, 'image archive member shortened')
    return data


def inspect_image_tar():
    check_path(PG_TAR, False)
    with tarfile.open(PG_TAR, 'r:') as archive:
        raw = _tar_member(archive, 'manifest.json', 1024 * 1024)
        manifest = strict_json(raw)
        require(type(manifest) is list and len(manifest) == 1 and type(manifest[0]) is dict,
                'image archive manifest is not a single image')
        config_name, layers = manifest[0].get('Config'), manifest[0].get('Layers')
        config = _tar_member(archive, config_name, 8 * 1024 * 1024)
        require(hashlib.sha256(config).hexdigest() == PG_ID[7:], 'image archive config digest differs')
        config_json = strict_json(config)
        require(config_json.get('os') == 'linux' and config_json.get('architecture') == 'amd64' and
                'PG_MAJOR=16' in (config_json.get('config', {}).get('Env') or []), 'image archive config differs')
        require(type(layers) is list and layers and all(type(name) is str for name in layers) and
                len(set(layers)) == len(layers), 'image archive layer set invalid')
        for name in layers:
            require(not name.startswith('/') and '..' not in Path(name).parts and archive.getmember(name).isfile(),
                    'image archive layer invalid')
    sha, size = digest(PG_TAR)
    return {'sha256': sha, 'size': size, 'config': config_name, 'layers': len(layers)}


def _save_postgres(env):
    before = _docker_inspect(env)
    fd = os.open(PG_TAR, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    error_path = RUN / 'assets/postgres-image-save.stderr'
    error_fd = os.open(error_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(fd, 'wb', closefd=False) as output, os.fdopen(error_fd, 'wb', closefd=False) as errors:
            try:
                result = subprocess.run(['/usr/bin/docker', 'image', 'save', PG_ID], cwd=RUN, env=env,
                                        stdin=subprocess.DEVNULL, stdout=output, stderr=errors,
                                        timeout=1800, check=False)
            except (OSError, subprocess.TimeoutExpired) as exc:
                raise Failure('offline PostgreSQL image export failed') from exc
            output.flush()
            errors.flush()
            os.fsync(fd)
            os.fsync(error_fd)
        require(result.returncode == 0 and os.fstat(error_fd).st_size <= 1024 * 1024,
                'offline PostgreSQL image export failed')
    finally:
        os.close(fd)
        os.close(error_fd)
    after = _docker_inspect(env)
    require(before == after, 'local PostgreSQL image changed during export')
    archive = inspect_image_tar()
    directory = os.open(PG_TAR.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {'version': 1, 'run_token': TOKEN, 'status': 'PASS', 'image': before, 'archive': archive}


def prepare(frozen, source_run, env):
    require(source_run == str(ASSETS), 'source-run must be the fixed offline asset set')
    path = RUN / 'postgres-image.json'
    with frozen.exclusive(path) as receipt:
        state = {'version': 1, 'run_token': TOKEN, 'status': 'IN_PROGRESS'}
        frozen.receipt_save(receipt, state)
        try:
            _docker_inspect(env)
            status = frozen.prepare_import.prepare(frozen, env, source_run)
            require(status == 0, 'frozen offline asset importer failed')
            state = _save_postgres(env)
            frozen.receipt_save(receipt, state)
        except BaseException as exc:
            state.update(status='FAIL', error_type=type(exc).__name__, error=str(exc)[:512])
            frozen.receipt_save(receipt, state)
            raise
    return 0


def prepare_gate(frozen):
    result, manifest, state = frozen.prepare_import.gate(frozen)
    path = RUN / 'postgres-image.json'
    check_path(path, False)
    pg = strict_json(path.read_bytes())
    require(type(pg) is dict and pg.get('version') == 1 and pg.get('run_token') == TOKEN and pg.get('status') == 'PASS' and
            pg.get('image', {}).get('image_id') == PG_ID and pg.get('image', {}).get('repository_digest') == PG_REPOSITORY_DIGEST and
            pg.get('archive') == inspect_image_tar(), 'PostgreSQL image import evidence differs')
    return result, manifest, state, pg


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    for name in ('prepare', 'build', 'run', 'verify', 'retire-plan', 'retire-apply'):
        command = sub.add_parser(name)
        command.add_argument('--run-dir', required=True)
        if name == 'prepare':
            command.add_argument('--source-run', required=True)
        if name == 'retire-apply':
            command.add_argument('--manifest-sha256', required=True)
        if name == 'run':
            command.add_argument('--case', choices=CASES, required=True)
            command.add_argument('--scenario')
    args = parser.parse_args()
    validate_run(args.run_dir)
    frozen = frozen_host()
    if args.action in ('retire-plan', 'retire-apply'):
        import source_retirement
        host = sys.modules[__name__]
        result = (source_retirement.inventory(host, frozen) if args.action == 'retire-plan' else
                  source_retirement.apply(host, frozen, args.manifest_sha256))
        print(json.dumps(result, sort_keys=True))
        return 0
    env = frozen.task_environment()
    if args.action == 'prepare':
        return prepare(frozen, args.source_run, env)
    if args.action == 'build':
        import source_build
        return source_build.build(sys.modules[__name__], frozen, env)
    if args.action == 'run':
        import source_vm
        return source_vm.run(sys.modules[__name__], frozen, env, args.case, args.scenario)
    import source_verify
    return source_verify.verify(sys.modules[__name__], frozen)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (Failure, OSError, ValueError, KeyError, tarfile.TarError) as exc:
        print('PREREQUISITE_FAILED: ' + str(exc)[:512], file=sys.stderr)
        sys.exit(1)
