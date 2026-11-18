"""Build the pinned C80 and current ceremony binaries into the offline seed."""
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import re
import stat
import subprocess
import tarfile

import source_boot
import source_bpf_build
import source_host

COMMIT = 'ec38a48d3538735ee343698d7ed5b4ecfeb4ad12'
WORKTREE = source_host.WORKTREE
MODULE_CACHE = Path('/home/murray/go/pkg/mod/cache/download')
BINARY_SOURCES = ('c80-initializer', 'c80-launcher', 'c81-migrator', 'domain-initializer', 'dns-probe',
                  'c80-comparison-negative', 'c80-backup-negative', 'bpf-authority')
SUPERVISOR_FILES = ('contract.py', 'census.py', 'journal.py', 'maintenance.py', 'smoke.py', 'phase.py', 'permit.py',
                    'ipc.py', 'audit.py', 'lifecycle.py', 'guest_storage.py', 'guest_bootstrap.py', 'guest_postgres.py',
                    'pg_bootstrap.py', 'systemd_realm.py', 'run_stop_observer.py', 'volume_verify.py', 'origins.py', 'invoke.py', 'heartbeat.py',
                    'genesis.py', 'ceremony.py', 'cutpoints.py', 'network_verify.py', 'restart.py', 'dns.py',
                    'fence_transition.py', 'pg_session.py', 'negative_channel.py', 'comparison_fixture.py',
                    'extra_barrier.py', 'physical_backup.py', 'wrong_rpc.py', 'reboot_observer.py',
                    'listener_negative.py', 'callback_lifecycle.py', 'browser_process.py', 'app_rollback.py',
                    'bpf_authority.py')
ROOT_GUEST_FILES = ('source_guest.py', 'source_protocol.py', 'source_settlement_evidence.py')
GUEST_FILES = ROOT_GUEST_FILES + SUPERVISOR_FILES
RUNNER_FILES = ('source_host.py', 'source_build.py', 'source_vm.py', 'source_verify.py', 'source_protocol.py', 'source_protocol_test.py',
                'source_settlement_evidence.py', 'source_settlement_evidence_test.py', 'source_resources.py', 'source_resource_test.py', 'source_copy_test.py',
                'source_vm_failure_fixture.py',
                'source_retirement.py', 'source_retirement_test.py', 'source_boot.py', 'source_bpf_build.py',
                'source_bpf_verify.py', 'source_bpf_core.py', 'source_kernel_observer.py',
                'source_bpf_negative.py', 'browser_negative.py')
CARRIER_FILES = ('guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py')
BINARIES = ('c80-initializer', 'c80-launcher', 'c80-predecessor', 'c81-migrator',
            'domain-initializer', 'houfeng-record-platform-admin', 'dns-probe', 'c80-comparison-negative', 'c80-backup-negative',
            'g2-bpf-authority')
MKE2FS_CONFIG = b'''[defaults]\n\tbase_features = sparse_super,large_file,filetype,resize_inode,dir_index,ext_attr\n\tblocksize = 4096\n\tinode_size = 256\n\tinode_ratio = 16384\n\tenable_periodic_fsck = 0\n[fs_types]\n\text4 = {\n\t\tfeatures = has_journal,extent,huge_file,flex_bg,metadata_csum,64bit,dir_nlink,extra_isize\n\t}\n'''
SEED_TOOLS = ('/usr/bin/mke2fs', '/usr/bin/dumpe2fs', '/usr/bin/debugfs')


def _system_file_digest(host, path):
    # Distribution tools may have several root-owned hardlink names. They are
    # not mutable RUN artifacts, whose single-link rule remains unchanged.
    host.check_path(path, False)
    for parent in (path, *path.parents):
        info = os.stat(parent, follow_symlinks=False)
        host.require(info.st_uid == 0 and info.st_gid == 0 and info.st_mode & 0o022 == 0,
                     'seed tool or ancestor is writable outside root')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        host.require(stat.S_ISREG(before.st_mode) and before.st_nlink > 0, 'seed tool is not a regular file')
        digest = hashlib.sha256()
        while True:
            block = os.read(fd, 1024 * 1024)
            if not block:
                break
            digest.update(block)
        identity = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mode,
                                  value.st_uid, value.st_gid, value.st_nlink, value.st_mtime_ns, value.st_ctime_ns)
        host.require(identity(before) == identity(os.fstat(fd)) == identity(os.stat(path, follow_symlinks=False)),
                     'seed tool changed during fingerprinting')
        return digest.hexdigest()
    finally:
        os.close(fd)


def seed_toolchain(host, env):
    """Pin the executable, reported version, and resolved shared-library bytes."""
    tools = {}
    for name in SEED_TOOLS:
        path = Path(os.path.realpath(name))
        host.check_path(path, False)
        version = subprocess.run([name, '-V'], env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, timeout=30, check=False)
        text = version.stdout + version.stderr
        host.require(version.returncode in (0, 1) and 0 < len(text) <= 16384, 'seed tool version unavailable')
        dependencies = subprocess.run(['/usr/bin/ldd', name], env=env, stdin=subprocess.DEVNULL,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30, check=False)
        host.require(dependencies.returncode == 0 and len(dependencies.stdout) <= 65536 and
                     len(dependencies.stderr) <= 16384 and b'not found' not in dependencies.stdout,
                     'seed tool dependencies unavailable')
        libraries = {}
        for line in dependencies.stdout.decode('utf-8', 'strict').splitlines():
            match = re.search(r'(?<!\S)(/[^\s]+)', line)
            if match:
                dependency = Path(os.path.realpath(match.group(1)))
                host.check_path(dependency, False)
                libraries[str(dependency)] = _system_file_digest(host, dependency)
        host.require(bool(libraries), 'seed tool dependency list is empty')
        tools[name] = {'path': str(path), 'sha256': _system_file_digest(host, path),
                       'version': text.decode('utf-8', 'strict').strip(), 'libraries': libraries}
    return {'tools': tools, 'config_sha256': hashlib.sha256(MKE2FS_CONFIG).hexdigest()}


def _private_dir(host, path):
    try:
        os.mkdir(path, 0o700)
    except FileExistsError:
        pass
    host.check_path(path)
    info = os.stat(path, follow_symlinks=False)
    host.require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
                 'build inventory directory is not private')


def lock_run(host):
    fd = os.open(host.RUN / 'source-vm.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        info = os.fstat(fd)
        host.require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1 and
                     stat.S_IMODE(info.st_mode) == 0o600, 'shared build and VM lock invalid')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return fd
    except BaseException:
        os.close(fd)
        raise


def _numbered(host, directory, prefix, suffix):
    _private_dir(host, directory)
    pattern = re.compile(re.escape(prefix) + r'([0-9]{20})' + re.escape(suffix) + r'$')
    indices = []
    for name in os.listdir(directory):
        match = pattern.fullmatch(name)
        host.require(match is not None, 'unexpected build inventory entry')
        indices.append(int(match.group(1)))
    indices.sort()
    host.require(indices == list(range(1, len(indices) + 1)), 'build inventory sequence is discontinuous')
    return indices


def _candidate_digest(host, state):
    names = ('predecessor_commit', 'c80_archive_sha256', 'current_sources', 'wrapper_sources',
             'initializer_sources', 'migrator_sources', 'domain_initializer_sources', 'launcher_sources',
             'dns_probe_sources', 'comparison_negative_sources', 'backup_negative_sources', 'run_module',
             'binaries', 'qemu_binaries', 'postgres_archive', 'browser_selection',
             'runner_inputs', 'seed_inputs', 'supervisor_manifest_sha256')
    values = {name: state[name] for name in names}
    if 'seed_toolchain' in state:
        values['seed_toolchain'] = state['seed_toolchain']
    if 'bpf_authority' in state:
        values['bpf_authority'] = state['bpf_authority']
        values['boot_inputs'] = state['boot_inputs']
        values['bpf_authority_sources'] = state['bpf_authority_sources']
    return hashlib.sha256(host.canonical(values)).hexdigest()


def _selected_browser(host):
    try:
        import browser_negative
        selected = browser_negative.selected_browser()
    except Exception as exc:
        raise host.Failure('verified offline browser selection unavailable') from exc
    host.require(type(selected) is dict and set(selected) == {'selection', 'selection_path', 'selection_sha256',
                 'manifest_path', 'receipt_path', 'image_archive_path'} and type(selected['selection']) is dict,
                 'browser selection interface differs')
    selection = selected['selection']
    receipt = host.strict_json(Path(selected['receipt_path']).read_bytes())
    host.require(selection.get('version') == 1 and selection.get('run_token') == host.TOKEN and
                 selection.get('program_sha256') == '670ba079b75107746ba41abad131180a31a7c7219aa1bd4061fb471f4535d541' and
                 type(selection.get('image_id')) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', selection['image_id']) and
                 host.digest(Path(selected['selection_path']))[0] == selected['selection_sha256'] and
                 host.digest(Path(selected['image_archive_path']))[0] == selection.get('image_archive_sha256') and
                 type(receipt) is dict and receipt.get('builder_sha256') == host.digest(host.SOURCE_ROOT / 'browser_negative.py')[0],
                 'selected browser image identity differs')
    return selected


def _write(path, data, mode=0o600):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    try:
        view = memoryview(data)
        while view:
            count = os.write(fd, view)
            if count <= 0:
                raise OSError('short write')
            view = view[count:]
        os.fchmod(fd, mode)
        os.fsync(fd)
    finally:
        os.close(fd)


def _copy(host, source, destination, mode=None):
    def identity(info):
        return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)

    host.check_path(source, False)
    before = os.stat(source, follow_symlinks=False)
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        held = os.fstat(fd)
        host.require(stat.S_ISREG(held.st_mode) and identity(before) == identity(held),
                     'input changed before copying')
        target = os.open(destination, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            target_initial = os.fstat(target)
            host.require(stat.S_ISREG(target_initial.st_mode) and target_initial.st_nlink == 1 and
                         (held.st_dev, held.st_ino) != (target_initial.st_dev, target_initial.st_ino),
                         'unsafe copy target')
            cloned = False
            if held.st_size >= 1024 * 1024 and held.st_dev == target_initial.st_dev:
                try:
                    fcntl.ioctl(target, 0x40049409, fd)
                    cloned = True
                except OSError as exc:
                    if exc.errno not in (errno.EXDEV, errno.EOPNOTSUPP, errno.ENOTTY, errno.EINVAL):
                        raise
                    os.ftruncate(target, 0)
                    os.lseek(target, 0, os.SEEK_SET)
            h = hashlib.sha256()
            size = 0
            while True:
                data = os.read(fd, 1024 * 1024)
                if not data:
                    break
                h.update(data)
                size += len(data)
                if not cloned:
                    view = memoryview(data)
                    while view:
                        written = os.write(target, view)
                        if written <= 0:
                            raise host.Failure('input copy did not progress')
                        view = view[written:]
            host.require(size == held.st_size and identity(held) == identity(os.fstat(fd)) ==
                         identity(os.stat(source, follow_symlinks=False)), 'input changed while copying')
            target_before = os.fstat(target)
            host.require(stat.S_ISREG(target_before.st_mode) and target_before.st_nlink == 1 and
                         (target_before.st_dev, target_before.st_ino) ==
                         (target_initial.st_dev, target_initial.st_ino) and target_before.st_size == size and
                         identity(target_before) == identity(os.stat(destination, follow_symlinks=False)),
                         'copy target changed')
            os.lseek(target, 0, os.SEEK_SET)
            target_hash = hashlib.sha256()
            target_size = 0
            while True:
                data = os.read(target, 1024 * 1024)
                if not data:
                    break
                target_hash.update(data)
                target_size += len(data)
            host.require(target_size == size and target_hash.digest() == h.digest() and
                         identity(target_before) == identity(os.fstat(target)) ==
                         identity(os.stat(destination, follow_symlinks=False)) and
                         os.fstat(target).st_nlink == 1 and identity(held) == identity(os.fstat(fd)) ==
                         identity(os.stat(source, follow_symlinks=False)), 'copied input changed or digest differs')
            os.fchmod(target, mode if mode is not None else stat.S_IMODE(before.st_mode))
            os.fsync(target)
            final_target = os.fstat(target)
            host.require(final_target.st_nlink == 1 and
                         (final_target.st_dev, final_target.st_ino) ==
                         (target_initial.st_dev, target_initial.st_ino) and final_target.st_size == size and
                         identity(final_target) == identity(os.stat(destination, follow_symlinks=False)) and
                         identity(held) == identity(os.fstat(fd)) ==
                         identity(os.stat(source, follow_symlinks=False)), 'copy identity changed during publication')
            return h.hexdigest()
        finally:
            os.close(target)
    finally:
        os.close(fd)


class StorageToolFailure(RuntimeError):
    def __init__(self, label, returncode, stdout, stderr):
        self.label, self.returncode = label, returncode
        self.stdout, self.stderr = Path(stdout), Path(stderr)
        super().__init__(label + ' storage tool failed; see private logs')


def _run(host, args, cwd, env, label, logdir, timeout=900, pass_fds=(), *, storage_step=False):
    stdout = logdir / (label + '.stdout')
    stderr = logdir / (label + '.stderr')
    out = os.open(stdout, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        err = os.open(stderr, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            with os.fdopen(out, 'wb', closefd=False) as out_file, os.fdopen(err, 'wb', closefd=False) as err_file:
                try:
                    result = subprocess.run(args, cwd=cwd, env=env, stdin=subprocess.DEVNULL, pass_fds=pass_fds,
                                            stdout=out_file, stderr=err_file, timeout=timeout, check=False)
                except (OSError, subprocess.TimeoutExpired) as exc:
                    if storage_step:
                        raise StorageToolFailure(label, None, stdout, stderr) from exc
                    raise host.Failure(label + ' could not finish') from exc
                out_file.flush()
                err_file.flush()
                os.fsync(out)
                os.fsync(err)
            try:
                host.require(os.fstat(out).st_size <= 4 * 1024 * 1024 and os.fstat(err).st_size <= 4 * 1024 * 1024,
                             label + ' output exceeded limit')
            except host.Failure as exc:
                if result.returncode != 0 and storage_step:
                    raise StorageToolFailure(label, result.returncode, stdout, stderr) from exc
                raise
            if result.returncode != 0 and storage_step:
                raise StorageToolFailure(label, result.returncode, stdout, stderr)
            host.require(result.returncode == 0, label + ' failed; see private build logs')
        finally:
            os.close(err)
    finally:
        os.close(out)


def _command_names(host, directory):
    host.check_path(directory)
    names = sorted(list(directory.glob('*.go')) + list(directory.glob('*.s')))
    host.require(any(not path.name.endswith('_test.go') for path in names) and
                 all(path.parent == directory for path in names),
                 'required fixed program sources unavailable: ' + directory.name)
    for path in names:
        host.check_path(path, False)
    return names


def _safe_relative(host, name):
    host.require(name and not name.startswith('/') and '\0' not in name and '..' not in Path(name).parts,
                 'unsafe project snapshot path')
    return Path(name)


def _snapshot_current(host, env, destination):
    result = subprocess.run(['/usr/bin/git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'],
                            cwd=WORKTREE, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=60, check=False)
    host.require(result.returncode == 0 and len(result.stdout) <= 32 * 1024 * 1024 and len(result.stderr) <= 65536,
                 'could not enumerate candidate sources')
    paths = result.stdout.split(b'\0')
    host.require(paths.pop() == b'' and len(paths) == len(set(paths)), 'invalid candidate source inventory')
    os.mkdir(destination, 0o700)
    manifest = {}
    for raw in paths:
        relative = _safe_relative(host, os.fsdecode(raw))
        source, target = WORKTREE / relative, destination / relative
        target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        try:
            info = os.lstat(source)
        except FileNotFoundError:
            manifest[str(relative)] = {'kind': 'deleted'}
            continue
        if stat.S_ISREG(info.st_mode):
            manifest[str(relative)] = {'kind': 'file', 'sha256': _copy(host, source, target),
                                       'mode': stat.S_IMODE(info.st_mode)}
        elif stat.S_ISLNK(info.st_mode):
            link = os.readlink(source)
            host.require(not os.path.isabs(link) and (source.parent / link).resolve().is_relative_to(WORKTREE),
                         'candidate symlink escapes project')
            os.symlink(link, target)
            manifest[str(relative)] = {'kind': 'symlink', 'target': link}
        else:
            raise host.Failure('unexpected candidate source type')
    return manifest


def _archive_c80(host, env, destination, logdir):
    archive = destination.parent / 'c80.tar'
    _run(host, ['/usr/bin/git', 'archive', '--format=tar', '--output=' + str(archive), COMMIT],
         WORKTREE, env, 'c80-archive', logdir, timeout=300)
    os.mkdir(destination, 0o700)
    with tarfile.open(archive, 'r:') as tar:
        for member in tar:
            _safe_relative(host, member.name)
            host.require(member.isfile() or member.isdir() or member.issym() or member.islnk(),
                         'unsupported archived C80 member')
        tar.extractall(destination, filter='data')
    return host.digest(archive)[0]


def _module_escape(value):
    return ''.join('!' + ch.lower() if 'A' <= ch <= 'Z' else ch for ch in value)


def _copy_module_cache(host, snapshots, env):
    destination = Path(env['GOMODCACHE']) / 'cache/download'
    seen = set()
    for snapshot in snapshots:
        for row in (snapshot / 'go.sum').read_text(encoding='utf-8').splitlines():
            parts = row.split()
            host.require(len(parts) == 3, 'invalid module checksum line')
            module, version = parts[0], parts[1].removesuffix('/go.mod')
            seen.add((_module_escape(module), _module_escape(version)))
    for module, version in sorted(seen):
        source_dir = MODULE_CACHE / module / '@v'
        target_dir = destination / module / '@v'
        for suffix in ('.mod', '.info', '.zip', '.ziphash'):
            source = source_dir / (version + suffix)
            if not source.is_file() or source.is_symlink():
                continue
            target_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
            target = target_dir / source.name
            if target.exists():
                host.require(host.digest(source) == host.digest(target), 'offline module cache conflict')
            else:
                _copy(host, source, target)


def _copy_sources(host, source_paths, destination):
    destination.mkdir(parents=True, exist_ok=False, mode=0o700)
    return {path.name: _copy(host, path, destination / path.name, 0o600) for path in source_paths}


def _write_seed(host, frozen, result, program_paths, build_state, attempt, browser):
    seed = attempt / 'seed'
    os.mkdir(seed, 0o700)
    for name in GUEST_FILES:
        source = host.SOURCE_ROOT / name if name in ROOT_GUEST_FILES else host.SOURCE_ROOT / 'source_supervisor' / name
        _copy(host, source, seed / name, 0o600)
    for name in CARRIER_FILES:
        source = host.PHASE0 / name
        host.require(host.digest(source)[0] == frozen.load_json(host.PHASE0 / 'candidate-implementation.json')['files'][name],
                     'frozen carrier source changed')
        _copy(host, source, seed / name, 0o600)
    for name, path in program_paths.items():
        _copy(host, path, seed / name, 0o500)
    for name in frozen.DEBS:
        _copy(host, Path(result['hash_checks']['docker:' + name]['path']), seed / (name + '.deb'), 0o400)
    _copy(host, host.PG_TAR, seed / 'postgres-16.12.tar', 0o400)
    _copy(host, Path(browser['selection_path']), seed / 'browser-selection.json', 0o400)
    _copy(host, Path(browser['image_archive_path']), seed / 'browser-image.tar', 0o400)
    files = {name: host.digest(seed / name)[0] for name in GUEST_FILES}
    manifest = {'version': 1, 'run_token': host.TOKEN, 'files': files}
    manifest_body = host.canonical(manifest)
    _write(seed / 'supervisor-manifest.json', manifest_body)
    _write(seed / 'network-config', b'version: 2\nethernets: {}\n')
    cloud = b"""#cloud-config
package_update: false
package_upgrade: false
runcmd:
  - [mkdir, -p, /mnt/g2-cidata, /mnt/g2-seed, /run/g2]
  - [mount, -t, vfat, -o, 'ro,nosuid,nodev,noexec', /dev/disk/by-id/virtio-G2CIDATA-part1, /mnt/g2-cidata]
  - [mount, -t, ext4, -o, 'ro,noload,nosuid,nodev,noexec', /dev/disk/by-id/virtio-G2SEED, /mnt/g2-seed]
  - [sh, -c, 'cd /mnt/g2-seed && sha256sum -c SHA256SUMS && cp source_guest.py source_protocol.py source_settlement_evidence.py contract.py census.py journal.py maintenance.py smoke.py phase.py permit.py ipc.py audit.py lifecycle.py callback_lifecycle.py browser_process.py app_rollback.py guest_storage.py guest_bootstrap.py guest_postgres.py pg_bootstrap.py pg_session.py negative_channel.py comparison_fixture.py extra_barrier.py physical_backup.py wrong_rpc.py reboot_observer.py listener_negative.py systemd_realm.py run_stop_observer.py volume_verify.py origins.py invoke.py heartbeat.py genesis.py ceremony.py cutpoints.py network_verify.py restart.py dns.py fence_transition.py bpf_authority.py guest.py handoff.py guest_matrix.py preparation.py prepare_launcher.py /run/g2/ && exec /usr/bin/python3 -B -c "import guest_bootstrap; guest_bootstrap.launch_source()"']
power_state: {mode: poweroff, delay: now, timeout: 60}
"""
    _write(seed / 'user-data', cloud)
    # Each case gets its own case.json, meta-data and final SHA256SUMS.
    build_state['seed_inputs'] = {entry.name: host.digest(entry)[0] for entry in seed.iterdir()}
    build_state['supervisor_manifest_sha256'] = hashlib.sha256(manifest_body).hexdigest()


def _perform_build(host, frozen, env, attempt, attempt_id):
    receipt_path = attempt / 'receipt.json'
    with frozen.exclusive(receipt_path) as receipt:
        state = {'version': 1, 'run_token': host.TOKEN, 'attempt_id': attempt_id,
                 'status': 'IN_PROGRESS', 'stage': 'prerequisites'}
        frozen.receipt_save(receipt, state)
        boot = None
        try:
            result, _, _, pg = host.prepare_gate(frozen)
            _require_settlement_guard_abi(host)
            boot = source_boot.derive(Path(result['hash_checks']['ubuntu:' + frozen.IMAGE]['path']),
                                      host.RUN / 'build/qemu/qemu-img', attempt / 'boot-inputs', env=env)
            browser = _selected_browser(host)
            source_dirs = {name: _command_names(host, host.SOURCE_ROOT / 'source_supervisor' / name) for name in BINARY_SOURCES}
            wrappers = [WORKTREE / 'internal/center/store/testdata/legacydeletion' / name for name in ('main.go', 'inventory.go')]
            for path in wrappers:
                host.check_path(path, False)
            for name in GUEST_FILES:
                source = host.SOURCE_ROOT / name if name in ROOT_GUEST_FILES else host.SOURCE_ROOT / 'source_supervisor' / name
                host.check_path(source, False)
            build_dir = attempt / 'source-build'
            os.mkdir(build_dir, 0o700)
            logs = build_dir / 'logs'
            os.mkdir(logs, 0o700)
            state['stage'] = 'snapshot'
            frozen.receipt_save(receipt, state)
            current, c80 = build_dir / 'current', build_dir / 'c80'
            current_manifest = _snapshot_current(host, env, current)
            archive_sha = _archive_c80(host, env, c80, logs)
            wrapper_manifest = _copy_sources(host, wrappers, c80 / 'cmd/record-deletion-predecessor-fixture')
            initializer_manifest = _copy_sources(host, source_dirs['c80-initializer'], c80 / 'cmd/g2-c80-initializer')
            comparison_manifest = _copy_sources(host, source_dirs['c80-comparison-negative'], c80 / 'cmd/g2-c80-comparison-negative')
            backup_manifest = _copy_sources(host, source_dirs['c80-backup-negative'], c80 / 'cmd/g2-c80-backup-negative')
            migrator_manifest = _copy_sources(host, source_dirs['c81-migrator'], current / 'cmd/g2-c81-migrator')
            domain_manifest = _copy_sources(host, source_dirs['domain-initializer'], current / 'cmd/g2-domain-initializer')
            _copy_module_cache(host, (current, c80), env)
            go = str(frozen.GO)
            goenv = dict(env, CGO_ENABLED='0', GOOS='linux', GOARCH='amd64')
            binary_dir = build_dir / 'bin'
            os.mkdir(binary_dir, 0o700)
            programs = {name: binary_dir / name for name in BINARIES}
            state['stage'] = 'go-build'
            frozen.receipt_save(receipt, state)
            targets = ((c80, './cmd/record-deletion-predecessor-fixture', 'c80-predecessor'),
                       (c80, './cmd/g2-c80-initializer', 'c80-initializer'),
                       (c80, './cmd/g2-c80-comparison-negative', 'c80-comparison-negative'),
                       (c80, './cmd/g2-c80-backup-negative', 'c80-backup-negative'),
                       (current, './cmd/g2-c81-migrator', 'c81-migrator'),
                       (current, './cmd/g2-domain-initializer', 'domain-initializer'),
                       (current, './cmd/houfeng-record-platform-admin', 'houfeng-record-platform-admin'),
                       (host.SOURCE_ROOT, './source_supervisor/c80-launcher', 'c80-launcher'),
                       (host.SOURCE_ROOT, './source_supervisor/dns-probe', 'dns-probe'),
                       (host.SOURCE_ROOT, './source_supervisor/bpf-authority', 'g2-bpf-authority'))
            for directory, target, name in targets:
                _run(host, [go, 'build', '-mod=readonly', '-trimpath', '-o', str(programs[name]), target],
                     directory, goenv, 'build-' + name, logs, timeout=3600)
            state['stage'] = 'bpf-native-boot'
            frozen.receipt_save(receipt, state)
            bpf_sources = {p.name: host.digest(p)[0] for p in source_dirs['bpf-authority']}
            supervisor_sources = {name: host.digest(host.SOURCE_ROOT / name if name in ROOT_GUEST_FILES else
                                  host.SOURCE_ROOT / 'source_supervisor' / name)[0] for name in GUEST_FILES}
            bpf_artifacts = source_bpf_build.build(host, frozen, env, attempt, result, boot,
                              programs['g2-bpf-authority'], bpf_sources, supervisor_sources)
            host.require(source_boot.native_toolchain(env) == boot.metadata['native_toolchain'],
                         'native toolchain changed during build')
            state.update(boot_inputs=bpf_artifacts['boot_inputs'], bpf_authority=bpf_artifacts,
                         bpf_authority_sources=bpf_sources)
            state['stage'] = 'seed'
            frozen.receipt_save(receipt, state)
            _write_seed(host, frozen, result, programs, state, attempt, browser)
            state['seed_toolchain'] = seed_toolchain(host, env)
            _write(attempt / 'mke2fs.conf', MKE2FS_CONFIG, 0o400)
            directory = os.open(attempt, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
            state.update(status='PASS', stage='complete', predecessor_commit=COMMIT, c80_archive_sha256=archive_sha,
                         current_sources=current_manifest, wrapper_sources=wrapper_manifest,
                         initializer_sources=initializer_manifest, migrator_sources=migrator_manifest,
                         domain_initializer_sources=domain_manifest,
                         launcher_sources={p.name: host.digest(p)[0] for p in source_dirs['c80-launcher']},
                         dns_probe_sources={p.name: host.digest(p)[0] for p in source_dirs['dns-probe']},
                         comparison_negative_sources=comparison_manifest, backup_negative_sources=backup_manifest,
                         run_module={name: host.digest(host.SOURCE_ROOT / name)[0] for name in ('go.mod', 'go.sum')},
                         binaries={name: host.digest(path)[0] for name, path in programs.items()},
                         qemu_binaries=dict(frozen.prepare_import.QEMU_PINS),
                         postgres_archive=pg['archive'], browser_selection={'selection': browser['selection'],
                         'selection_sha256': browser['selection_sha256']}, prepare_sha256=host.digest(host.RUN / 'prepare.json')[0],
                         postgres_receipt_sha256=host.digest(host.RUN / 'postgres-image.json')[0],
                         runner_inputs={name: host.digest(host.SOURCE_ROOT / name)[0] for name in RUNNER_FILES},
                         product_gate='NOT_READY', permanent_deletion_http='CLOSED')
            _validate_current(host, frozen, state)
            frozen.receipt_save(receipt, state)
            return state
        except BaseException as exc:
            state.update(status='FAIL', error_type=type(exc).__name__, error=str(exc)[:512])
            frozen.receipt_save(receipt, state)
            raise
        finally:
            if boot is not None:
                boot.close()


def _selected(host, frozen):
    host.prepare_gate(frozen)
    selections = host.RUN / 'source-build-selections'
    indices = _numbered(host, selections, 'selection-', '.json')
    if not indices:
        return None
    for index in indices:
        path = selections / ('selection-%020d.json' % index)
        host.check_path(path, False)
        info = os.stat(path, follow_symlinks=False)
        host.require(info.st_uid == os.geteuid() and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o400 and
                     0 < info.st_size <= 4096,
                     'build selection permissions differ')
        raw = path.read_bytes()
        selection = host.strict_json(raw)
        host.require(type(selection) is dict and set(selection) == {'version', 'run_token', 'selection_id', 'attempt_id',
                     'candidate_sha256', 'receipt_sha256', 'seed_sha256', 'binaries_sha256'} and
                     type(selection['version']) is int and selection['version'] == 1 and
                     selection['run_token'] == host.TOKEN and selection['selection_id'] == '%020d' % index and
                     type(selection['attempt_id']) is str and re.fullmatch(r'[0-9]{20}', selection['attempt_id']) and
                     all(type(selection[key]) is str and re.fullmatch(r'[0-9a-f]{64}', selection[key])
                         for key in ('candidate_sha256', 'receipt_sha256', 'seed_sha256', 'binaries_sha256')) and
                     host.canonical(selection) == raw, 'build selection record invalid')
        attempt = host.RUN / 'source-build-attempts' / ('attempt-' + selection['attempt_id'])
        host.check_path(attempt)
        receipt = attempt / 'receipt.json'
        host.require(host.digest(receipt)[0] == selection['receipt_sha256'], 'selected attempt receipt changed')
    state = host.strict_json(receipt.read_bytes())
    host.require(type(state) is dict and state.get('version') == 1 and state.get('run_token') == host.TOKEN and
                 state.get('attempt_id') == selection['attempt_id'] and state.get('status') == 'PASS' and
                 state.get('predecessor_commit') == COMMIT and
                 state.get('prepare_sha256') == host.digest(host.RUN / 'prepare.json')[0] and
                 state.get('postgres_receipt_sha256') == host.digest(host.RUN / 'postgres-image.json')[0] and
                 state.get('product_gate') == 'NOT_READY' and state.get('permanent_deletion_http') == 'CLOSED' and
                 _candidate_digest(host, state) == selection['candidate_sha256'] and
                 hashlib.sha256(host.canonical(state['seed_inputs'])).hexdigest() == selection['seed_sha256'] and
                 hashlib.sha256(host.canonical(state['binaries'])).hexdigest() == selection['binaries_sha256'],
                 'selected source build receipt incomplete')
    import source_retirement
    source_retirement.assert_available(host, attempt)
    host.require(type(state['binaries']) is dict and state['binaries'] and all(
        type(name) is str and re.fullmatch(r'[a-z0-9-]+', name) for name in state['binaries']),
        'selected source binary set invalid')
    for name, expected in state['binaries'].items():
        host.require(host.digest(attempt / 'source-build/bin' / name)[0] == expected, 'source binary changed')
    for name, expected in state['seed_inputs'].items():
        host.require(host.digest(attempt / 'seed' / name)[0] == expected, 'seed input changed')
    state['_selection'] = dict(selection, selection_sha256=host.digest(path)[0])
    state['_attempt_dir'] = str(attempt)
    return state


def _require_settlement_guard_abi(host):
    header = host.SOURCE_ROOT / 'source_supervisor/bpf-authority/native/abi.h'
    host.check_path(header, False)
    data = header.read_bytes()
    host.require(len(data) <= 65536 and
                 re.findall(rb'(?m)^#define[ \t]+G2_GUARD_VERSION[ \t]+([^\s]+)[ \t]*$', data) == [b'5U'],
                 'current settlement guard ABI is not V5')


def _validate_current(host, frozen, state):
    host.validate_source_layout()
    host.require(type(state.get('boot_inputs')) is dict and type(state.get('bpf_authority')) is dict and
                 source_boot.native_toolchain(frozen.task_environment()) ==
                     state['bpf_authority']['boot_derivation']['native_toolchain'] and
                 source_bpf_build.native_sources(host) == state['bpf_authority']['native_sources'] and
                 host.digest(source_bpf_build.AUDIT_SOURCE)[0] == source_bpf_build.AUDIT_SOURCE_SHA256,
                 'BPF boot inputs or native toolchain changed')
    if 'source_settlement_evidence.py' in state.get('runner_inputs', {}):
        _require_settlement_guard_abi(host)
    host.require('seed_toolchain' in state and seed_toolchain(host, frozen.task_environment()) == state['seed_toolchain'],
                 'seed filesystem toolchain changed after build')
    host.require(set(state['binaries']) == set(BINARIES), 'current fixed binary set changed')
    browser = _selected_browser(host)
    host.require(state['browser_selection'] == {'selection': browser['selection'], 'selection_sha256': browser['selection_sha256']} and
                 state['seed_inputs']['browser-selection.json'] == browser['selection_sha256'] and
                 state['seed_inputs']['browser-image.tar'] == browser['selection']['image_archive_sha256'],
                 'browser selection or seed changed after build')
    for name in GUEST_FILES:
        source = host.SOURCE_ROOT / name if name in ROOT_GUEST_FILES else host.SOURCE_ROOT / 'source_supervisor' / name
        host.require(host.digest(source)[0] == state['seed_inputs'][name], 'guest source changed after build')
    for name, expected in state['runner_inputs'].items():
        host.require(host.digest(host.SOURCE_ROOT / name)[0] == expected, 'runner source changed')
    helper_manifests = {'c80-initializer': 'initializer_sources', 'c80-launcher': 'launcher_sources',
                        'c81-migrator': 'migrator_sources', 'domain-initializer': 'domain_initializer_sources',
                        'dns-probe': 'dns_probe_sources', 'c80-comparison-negative': 'comparison_negative_sources',
                        'c80-backup-negative': 'backup_negative_sources', 'bpf-authority': 'bpf_authority_sources'}
    for directory, key in helper_manifests.items():
        paths = _command_names(host, host.SOURCE_ROOT / 'source_supervisor' / directory)
        host.require({path.name: host.digest(path)[0] for path in paths} == state[key],
                     'fixed helper source changed after build: ' + directory)
    host.require({name: host.digest(host.SOURCE_ROOT / name)[0] for name in ('go.mod', 'go.sum')} == state['run_module'],
                 'fixed helper module changed after build')
    project = subprocess.run(['/usr/bin/git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'],
                             cwd=WORKTREE, env=frozen.task_environment(), stdin=subprocess.DEVNULL,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)
    host.require(project.returncode == 0 and len(project.stdout) <= 32 * 1024 * 1024 and len(project.stderr) <= 65536,
                 'current candidate inventory unavailable')
    names = project.stdout.split(b'\0')
    host.require(names.pop() == b'' and len(names) == len(set(names)) and
                 {os.fsdecode(name) for name in names} == set(state['current_sources']),
                 'current candidate file set changed after build')
    for relative, expected in state['current_sources'].items():
        source = WORKTREE / _safe_relative(host, relative)
        if expected['kind'] == 'deleted':
            host.require(not os.path.lexists(source), 'deleted candidate file reappeared: ' + relative)
            continue
        current = os.lstat(source)
        if expected['kind'] == 'file':
            host.require(stat.S_ISREG(current.st_mode) and stat.S_IMODE(current.st_mode) == expected['mode'] and
                         host.digest(source)[0] == expected['sha256'], 'current candidate file changed: ' + relative)
        elif expected['kind'] == 'symlink':
            host.require(stat.S_ISLNK(current.st_mode) and os.readlink(source) == expected['target'],
                         'current candidate link changed: ' + relative)
        else:
            raise host.Failure('candidate manifest contains an unknown file type')
    return state


def build(host, frozen, env):
    host.validate_source_layout()
    lock = lock_run(host)
    try:
        selected = _selected(host, frozen)
        if selected is not None:
            try:
                _validate_current(host, frozen, selected)
            except (host.Failure, OSError, KeyError, TypeError, ValueError):
                pass
            else:
                print(json.dumps({'status': 'REUSED', 'selection': selected['_selection']['selection_id'],
                                  'candidate_sha256': selected['_selection']['candidate_sha256']}))
                return 0
        host.validate_source_layout()
        host.prepare_gate(frozen)
        host.require(host.digest(source_bpf_build.AUDIT_SOURCE)[0] == source_bpf_build.AUDIT_SOURCE_SHA256,
                     'private audit source changed')
        attempts = host.RUN / 'source-build-attempts'
        attempt_indices = _numbered(host, attempts, 'attempt-', '')
        attempt_id = '%020d' % (len(attempt_indices) + 1)
        attempt = attempts / ('attempt-' + attempt_id)
        os.mkdir(attempt, 0o700)
        parent = os.open(attempts, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        state = _perform_build(host, frozen, env, attempt, attempt_id)
        selections = host.RUN / 'source-build-selections'
        indices = _numbered(host, selections, 'selection-', '.json')
        selection_id = '%020d' % (len(indices) + 1)
        selection = {'version': 1, 'run_token': host.TOKEN, 'selection_id': selection_id, 'attempt_id': attempt_id,
                     'candidate_sha256': _candidate_digest(host, state), 'receipt_sha256': host.digest(attempt / 'receipt.json')[0],
                     'seed_sha256': hashlib.sha256(host.canonical(state['seed_inputs'])).hexdigest(),
                     'binaries_sha256': hashlib.sha256(host.canonical(state['binaries'])).hexdigest()}
        path = selections / ('selection-' + selection_id + '.json')
        _write(path, host.canonical(selection), 0o400)
        directory = os.open(selections, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        print(json.dumps({'status': 'PASS', 'selection': selection_id, 'attempt': attempt_id,
                          'candidate_sha256': selection['candidate_sha256']}))
        return 0
    finally:
        os.close(lock)


def build_gate(host, frozen):
    host.validate_source_layout()
    state = _selected(host, frozen)
    host.require(state is not None, 'no successful source build selection exists')
    _validate_current(host, frozen, state)
    return state
