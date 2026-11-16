"""Owned QEMU lifecycle and strict QMP for the isolated source cases."""
import base64
import binascii
import hashlib
import json
import os
from pathlib import Path
import re
import select
import signal
import stat
import subprocess
import time
import sys
import uuid

import errno
import source_resources
import source_build
from source_protocol import MAX_PROTOCOL, MAX_RESULT_BYTES, MAX_EARLY_RESULT_CHUNKS, protocol_prefix
from source_supervisor.contract import scenarios

DISKS = (('app', 'G2APP'), ('w1', 'G2W1'), ('w2', 'G2W2'), ('control', 'G2CONTROL'))
ALLOWED_QMP = frozenset(('qmp_capabilities', 'query-status', 'query-block', 'query-pci', 'query-cpus-fast', 'quit'))
RESULT_PORT = 'org.houfeng.g2.result'
BOOT_COMMAND_LINE = ('root=LABEL=cloudimg-rootfs ro console=tty1 console=ttyS0 '
                     'lsm=landlock,lockdown,yama,integrity,apparmor,bpf rdinit=/g2-early-init')
BOOT_IDENTITY_FIELDS = ('dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'size', 'mtime_ns', 'ctime_ns', 'sha256')
EARLY_SEAL_VARIANTS = ('seal-guard-loaded', 'seal-inode-links', 'seal-bpf-link', 'seal-sealed', 'seal-audit-locked')
TERMINAL_FAULT_VARIANTS = (
    'audit-loss', 'audit-duplicate', 'audit-overflow', 'snapshot-missing', 'snapshot-bad-field',
    'guard-overflow', 'guard-miss', 'guard-pairing', 'fd-fill-failure', 'fd-restore-failure'
)
READER_FAULT_VARIANTS = (
    'reader-death', 'reader-endpoint', 'reader-timeout', 'reader-replay',
    'reader-credential', 'reader-extra-fd', 'reader-frame', 'reader-done'
)
BPF_AUTH_FAMILY_MEMBERS = (
    (('realm-negative', 'bpf-authorization', 'baseline'),) +
    tuple(('realm-negative', 'bpf-authorization', v) for v in TERMINAL_FAULT_VARIANTS) +
    tuple(('realm-negative', 'bpf-authorization', v) for v in EARLY_SEAL_VARIANTS) +
    tuple(('realm-negative', 'bpf-authorization', v) for v in READER_FAULT_VARIANTS) +
    (('realm-negative', 'observer-capability', 'fd-arm-wrong-case'),)
)


def _boot_identity(host, fd, path):
    def metadata(item):
        return {'dev': item.st_dev, 'ino': item.st_ino, 'uid': item.st_uid, 'gid': item.st_gid,
                'mode': stat.S_IMODE(item.st_mode), 'nlink': item.st_nlink, 'size': item.st_size,
                'mtime_ns': item.st_mtime_ns, 'ctime_ns': item.st_ctime_ns}
    before = os.fstat(fd)
    named = os.stat(path, follow_symlinks=False)
    host.require(stat.S_ISREG(before.st_mode) and stat.S_ISREG(named.st_mode) and
                 before.st_uid == os.geteuid() and before.st_gid == os.getegid() and
                 stat.S_IMODE(before.st_mode) == 0o400 and before.st_nlink == 1 and before.st_size > 0 and
                 metadata(before) == metadata(named), 'boot input held or named metadata differs')
    result = metadata(before)
    result['sha256'] = _held_digest(host, fd)
    host.require(metadata(before) == metadata(os.fstat(fd)) ==
                 metadata(os.stat(path, follow_symlinks=False)), 'boot input changed while hashing')
    return result


def _boot_paths(host, build):
    inputs = build.get('boot_inputs')
    keys = {'version', 'base_sha256', 'kernel_release', 'kernel_path', 'kernel_bytes', 'kernel_sha256',
            'original_initrd_path', 'original_initrd_sha256', 'initrd_path', 'initrd_bytes',
            'initrd_sha256', 'bootstrap_manifest_sha256', 'observer_bindings_sha256', 'command_line'}
    host.require(type(inputs) is dict and set(inputs) == keys and type(inputs['version']) is int and
                 inputs['version'] == 1 and inputs['kernel_release'] == '6.8.0-147-generic' and
                 inputs['command_line'] == BOOT_COMMAND_LINE and
                 all(type(inputs[key]) is str and re.fullmatch(r'[0-9a-f]{64}', inputs[key]) is not None and
                     inputs[key] != '0' * 64 for key in ('base_sha256', 'kernel_sha256',
                         'original_initrd_sha256', 'initrd_sha256', 'bootstrap_manifest_sha256',
                         'observer_bindings_sha256')) and
                 all(type(inputs[key]) is int and 0 < inputs[key] <= 1024 ** 3
                     for key in ('kernel_bytes', 'initrd_bytes')), 'direct boot input receipt invalid')
    paths = {}
    for kind in ('kernel', 'initrd', 'original_initrd'):
        relative = inputs[kind + '_path']
        host.require(type(relative) is str and relative and not relative.startswith('/') and
                     all(re.fullmatch(r'[A-Za-z0-9_.-]+', part) is not None and part not in ('.', '..')
                         for part in relative.split('/')), 'boot input path invalid')
        path = Path(build['_attempt_dir']) / relative
        host.check_path(path, False)
        paths[kind] = path
    host.require(len(set(paths.values())) == 3, 'boot input paths overlap')
    return paths


def _open_boot_fds(host, build):
    paths = _boot_paths(host, build)
    opened = {}
    result = {'version': 1}
    try:
        for kind in ('kernel', 'initrd'):
            fd = os.open(paths[kind], os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            opened[kind] = fd
            identity = _boot_identity(host, fd, paths[kind])
            host.require(identity['sha256'] == build['boot_inputs'][kind + '_sha256'] and
                         identity['size'] == build['boot_inputs'][kind + '_bytes'], 'boot input bytes differ')
            result[kind + '_fd'] = fd
            result[kind + '_identity'] = identity
        host.require(result['kernel_fd'] != result['initrd_fd'] and
                     (result['kernel_identity']['dev'], result['kernel_identity']['ino']) !=
                     (result['initrd_identity']['dev'], result['initrd_identity']['ino']), 'boot inputs alias')
        return result
    except BaseException:
        for fd in opened.values():
            os.close(fd)
        raise


def _check_boot_fds(host, build, boot_fds):
    paths = _boot_paths(host, build)
    for kind in ('kernel', 'initrd'):
        host.require(_boot_identity(host, boot_fds[kind + '_fd'], paths[kind]) ==
                     boot_fds[kind + '_identity'], 'held direct boot input changed')


def _validate_boot_fd_record(host, boot_fds):
    host.require(type(boot_fds) is dict and set(boot_fds) ==
                 {'version', 'kernel_fd', 'initrd_fd', 'kernel_identity', 'initrd_identity'} and
                 type(boot_fds['version']) is int and boot_fds['version'] == 1 and
                 all(type(boot_fds[kind + '_fd']) is int and boot_fds[kind + '_fd'] >= 0
                     for kind in ('kernel', 'initrd')) and
                 boot_fds['kernel_fd'] != boot_fds['initrd_fd'], 'boot FD launch record invalid')
    for kind in ('kernel', 'initrd'):
        identity = boot_fds[kind + '_identity']
        host.require(type(identity) is dict and set(identity) == set(BOOT_IDENTITY_FIELDS) and
                     all(type(identity[key]) is int and identity[key] >= 0 for key in BOOT_IDENTITY_FIELDS[:-1]) and
                     identity['ino'] > 0 and identity['size'] > 0 and identity['nlink'] == 1 and
                     identity['uid'] == os.geteuid() and identity['gid'] == os.getegid() and
                     identity['mode'] == 0o400 and type(identity['sha256']) is str and
                     re.fullmatch(r'[0-9a-f]{64}', identity['sha256']) is not None and
                     identity['sha256'] != '0' * 64, 'boot FD identity invalid')




def qmp_type(host, frozen):
    class StrictQmp(frozen.Qmp):
        def send(self, name, deadline, arguments=None):
            host.require(name in ALLOWED_QMP and arguments is None, 'QMP command rejected by allowlist')
            return super().send(name, deadline, None)
    return StrictQmp


class _CaseReceipt:
    def __init__(self, host, case_dir):
        self.host = host
        self.fd = os.open(case_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        self.identity = None
        self.failed = False
        self.publication_unknown = False
        self.failure = None
        try:
            host.require(not os.path.lexists(case_dir / 'receipt.json'), 'receipt already exists')
        except BaseException:
            os.close(self.fd)
            raise

    def _check_primary(self):
        try:
            info = os.stat('receipt.json', dir_fd=self.fd, follow_symlinks=False)
        except FileNotFoundError:
            self.host.require(self.identity is None, 'receipt disappeared')
            return
        self.host.require(self.identity is not None and stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                          info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600 and
                          (info.st_dev, info.st_ino) == self.identity, 'receipt identity changed')

    def save(self, value):
        self.host.require(not self.failed, 'receipt publisher failed')
        data = (json.dumps(value, sort_keys=True, indent=2) + '\n').encode('utf-8')
        self.host.require(len(data) <= source_resources.FINAL_RECEIPT_ALLOWANCE_BYTES, 'receipt exceeds limit')
        temp = None
        identity = None
        renamed = False
        try:
            self._check_primary()
            temp = os.open('receipt-next.json', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                           0o600, dir_fd=self.fd)
            item = os.fstat(temp)
            identity = (item.st_dev, item.st_ino)
            view = memoryview(data)
            while view:
                n = os.write(temp, view)
                if n <= 0:
                    raise OSError('receipt write failed')
                view = view[n:]
            os.fsync(temp)
            self._check_primary()
            named = os.stat('receipt-next.json', dir_fd=self.fd, follow_symlinks=False)
            self.host.require(stat.S_ISREG(named.st_mode) and named.st_nlink == 1 and
                              named.st_uid == os.geteuid() and stat.S_IMODE(named.st_mode) == 0o600 and
                              (named.st_dev, named.st_ino) == identity, 'receipt temporary replaced')
            os.rename('receipt-next.json', 'receipt.json', src_dir_fd=self.fd, dst_dir_fd=self.fd)
            renamed = True
            self.identity = identity
            os.fsync(self.fd)
        except BaseException as exc:
            self.failed = True
            self.failure = exc
            self.publication_unknown = renamed
            if temp is not None and not renamed:
                try:
                    named = os.stat('receipt-next.json', dir_fd=self.fd, follow_symlinks=False)
                    held = os.fstat(temp)
                    if (named.st_dev, named.st_ino) == (held.st_dev, held.st_ino) == identity:
                        os.unlink('receipt-next.json', dir_fd=self.fd)
                except OSError:
                    pass
            raise
        finally:
            if temp is not None:
                try:
                    os.close(temp)
                except OSError:
                    pass

    def close(self):
        os.close(self.fd)


def _mkdir_private(host, path):
    try:
        os.mkdir(path, 0o700)
    except FileExistsError:
        pass
    host.check_path(path)
    info = os.stat(path, follow_symlinks=False)
    host.require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
                 'unsafe runner directory')


def _sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


SEED_FEATURES = frozenset(('has_journal', 'ext_attr', 'resize_inode', 'dir_index', 'filetype', 'extent',
                           '64bit', 'flex_bg', 'sparse_super', 'large_file', 'huge_file', 'dir_nlink',
                           'extra_isize', 'metadata_csum'))


def _case_bytes(host, build, case, scenario, variant=None):
    value = {'version': 1, 'run_token': host.TOKEN, 'case': case, 'scenario': scenario,
             'bpf_variant': variant, 'qemu_sha256': build['qemu_binaries']['qemu-system-x86_64'],
             'boot_inputs': build['boot_inputs'], 'bpf_bootstrap': build['bpf_authority']['bootstrap_manifest']}
    data = host.canonical(value)
    host.require(len(data) <= 4096, 'boot-bound case exceeds existing storage limit')
    return data


def _seed_payload(host, state, case_dir, case, scenario, variant=None, resource_guard=None):
    source = Path(state['_attempt_dir']) / 'seed'
    host.require({entry.name for entry in source.iterdir()} == set(state['seed_inputs']), 'build seed contains unknown entries')
    target = case_dir / 'payload'
    os.mkdir(target, 0o700)
    for name, sha in sorted(state['seed_inputs'].items()):
        if name in ('user-data', 'network-config'):
            continue
        resource_guard.check_prepare(os.stat(source / name, follow_symlinks=False).st_size)
        host.require(source_build._copy(host, source / name, target / name, 0o400) == sha, 'seed input differs')
        resource_guard.check_prepare()
    case_bytes = _case_bytes(host, state, case, scenario, variant)
    resource_guard.check_prepare(len(case_bytes))
    source_build._write(target / 'case.json', case_bytes, 0o400)
    resource_guard.check_prepare()
    names = sorted(entry.name for entry in target.iterdir())
    sums = ''.join(host.digest(target / name)[0] + '  ' + name + '\n' for name in names).encode('ascii')
    resource_guard.check_prepare(len(sums))
    source_build._write(target / 'SHA256SUMS', sums, 0o400)
    resource_guard.check_prepare()
    hashes = {entry.name: host.digest(entry)[0] for entry in target.iterdir()}
    os.chmod(target, 0o500)
    _sync_dir(target)
    _sync_dir(case_dir)
    return hashes


def _verify_payload(host, directory, hashes):
    host.check_path(directory)
    host.require(stat.S_IMODE(os.stat(directory, follow_symlinks=False).st_mode) == 0o500, 'payload directory not frozen')
    host.require(all(type(name) is str and name not in ('.', '..') and
                     re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None for name in hashes),
                 'invalid payload name')
    host.require({entry.name for entry in directory.iterdir()} == set(hashes), 'payload inventory changed')
    for name, sha in hashes.items():
        path = directory / name
        host.require(stat.S_IMODE(os.stat(path, follow_symlinks=False).st_mode) == 0o400 and
                     host.digest(path)[0] == sha, 'payload input changed')
    lines = ''.join(hashes[name] + '  ' + name + '\n' for name in sorted(set(hashes) - {'SHA256SUMS'}))
    host.require((directory / 'SHA256SUMS').read_text(encoding='ascii') == lines, 'payload manifest differs')


def _verify_cidata(host, directory, hashes):
    host.check_path(directory)
    host.require(set(hashes) == {'meta-data', 'user-data', 'network-config', 'seed-descriptor.json'} and
                 stat.S_IMODE(os.stat(directory, follow_symlinks=False).st_mode) == 0o500 and
                 {entry.name for entry in directory.iterdir()} == set(hashes), 'CIDATA inventory differs')
    for name, sha in hashes.items():
        path = directory / name
        host.require(stat.S_IMODE(os.stat(path, follow_symlinks=False).st_mode) == 0o400 and
                     host.digest(path)[0] == sha, 'CIDATA input changed')


def _cidata_identity(directory, hashes):
    def identity(path):
        info = os.stat(path, follow_symlinks=False)
        return {'dev': info.st_dev, 'ino': info.st_ino, 'uid': info.st_uid,
                'mode': stat.S_IMODE(info.st_mode), 'size': info.st_size,
                'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}
    return {'directory': identity(directory), 'files': {name: identity(directory / name) for name in hashes}}


def _debugfs_digest(host, image, name, env):
    host.require(re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None, 'invalid payload name')
    proc = subprocess.Popen(['/usr/bin/debugfs', '-R', 'cat /' + name, str(image)], env=env,
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    h = hashlib.sha256()
    size = 0
    errors = bytearray()
    deadline = time.monotonic() + 300
    try:
        streams = {proc.stdout, proc.stderr}
        while streams:
            remaining = deadline - time.monotonic()
            host.require(remaining > 0, 'image readback timed out')
            ready, _, _ = select.select(list(streams), [], [], remaining)
            host.require(ready, 'image readback timed out')
            for stream in ready:
                block = os.read(stream.fileno(), 1024 * 1024 if stream is proc.stdout else 16385)
                if not block:
                    streams.remove(stream)
                elif stream is proc.stdout:
                    size += len(block)
                    h.update(block)
                else:
                    errors.extend(block)
                    host.require(len(errors) <= 16384, 'image readback diagnostics exceeded limit')
        host.require(proc.wait(timeout=max(1, deadline - time.monotonic())) == 0 and
                     b'checksum' not in errors.lower(), 'image file readback failed')
        return h.hexdigest(), size
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()
        proc.stdout.close()
        proc.stderr.close()


def _verify_seed_image(host, path, identity, hashes, env):
    host.require(all(type(name) is str and name not in ('.', '..') and
                     re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None for name in hashes),
                 'invalid payload name')
    info = os.stat(path, follow_symlinks=False)
    host.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o400 and
                 (info.st_dev, info.st_ino, info.st_size) == (identity['dev'], identity['ino'], identity['size']) and
                 host.digest(path)[0] == identity['sha256'], 'seed image identity changed')
    result = subprocess.run(['/usr/bin/dumpe2fs', '-h', str(path)], env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)
    host.require(result.returncode == 0 and len(result.stdout) <= 65536 and len(result.stderr) <= 16384 and
                 b'checksum' not in result.stderr.lower(), 'seed filesystem metadata unavailable')
    fields = {}
    for line in result.stdout.decode('ascii', 'strict').splitlines():
        key, sep, value = line.partition(':')
        if sep:
            host.require(key not in fields, 'duplicate filesystem metadata')
            fields[key] = value.strip()
    features = frozenset(fields.get('Filesystem features', '').split())
    host.require(identity['label'] == 'G2SEED' and fields.get('Filesystem volume name') == 'G2SEED' and
                 fields.get('Filesystem UUID') == identity['uuid'] and fields.get('Filesystem magic number') == '0xEF53' and
                 fields.get('Filesystem state') == 'clean' and fields.get('Reserved block count') == '0' and
                 fields.get('Block size') == '4096' and fields.get('Block count', '').isdigit() and
                 int(fields['Block count']) * 4096 == identity['size'] and features == SEED_FEATURES and
                 sorted(features) == identity['features'], 'seed filesystem metadata differs')
    listing = subprocess.run(['/usr/bin/debugfs', '-R', 'ls -p /', str(path)], env=env, stdin=subprocess.DEVNULL,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)
    host.require(listing.returncode == 0 and len(listing.stdout) <= 65536 and len(listing.stderr) <= 16384 and
                 b'checksum' not in listing.stderr.lower(), 'seed filesystem inventory unavailable')
    entries = {}
    for line in listing.stdout.decode('utf-8', 'strict').splitlines():
        if not line:
            continue
        parts = line.split('/')
        host.require(len(parts) == 8 and parts[0] == '' and parts[7] == '' and parts[5] not in entries and
                     re.fullmatch(r'[0-7]{6}', parts[2]) is not None and
                     parts[3].isdigit() and parts[4].isdigit(), 'seed filesystem listing malformed')
        entries[parts[5]] = (int(parts[2], 8), int(parts[3]), int(parts[4]), parts[6])
    host.require(set(entries) == set(hashes) | {'.', '..', 'lost+found'}, 'seed filesystem inventory differs')
    for name in ('.', '..', 'lost+found'):
        mode, uid, gid, _ = entries[name]
        host.require(stat.S_ISDIR(mode) and not mode & 0o022 and uid == 0 and gid == 0,
                     'seed filesystem directory ownership or mode differs')
    for name, sha in hashes.items():
        mode, uid, gid, size = entries[name]
        host.require(stat.S_ISREG(mode) and stat.S_IMODE(mode) == 0o400 and uid == 0 and gid == 0 and
                     size.isdigit(), 'seed filesystem payload ownership or mode differs')
        digest, actual_size = _debugfs_digest(host, path, name, env)
        host.require(digest == sha and actual_size == int(size), 'seed filesystem payload differs')


def _seed_image(host, env, state, case_dir, hashes, resource_guard):
    payload = case_dir / 'payload'
    _verify_payload(host, payload, hashes)
    total = sum(os.stat(payload / name, follow_symlinks=False).st_size for name in hashes)
    quantum = 256 * 1024 ** 2
    capacity = max(2 * 1024 ** 3, (((total * 5 + 3) // 4 + quantum + quantum - 1) // quantum) * quantum)
    host.require(capacity <= 8 * 1024 ** 3, 'complete seed payload exceeds 8 GiB capacity')
    path = case_dir / 'seed.raw'
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.ftruncate(fd, capacity)
        os.fsync(fd)
        created = os.fstat(fd)
        host.require(stat.S_ISREG(created.st_mode) and created.st_nlink == 1 and created.st_size == capacity,
                     'new seed image invalid')
        image_uuid = str(uuid.uuid4())
        config = Path(state['_attempt_dir']) / 'mke2fs.conf'
        host.require(host.digest(config)[0] == state['seed_toolchain']['config_sha256'], 'mke2fs config changed')
        build_env = dict(env, MKE2FS_CONFIG=str(config), LC_ALL='C', LANG='C')
        resource_guard.check_prepare(capacity)
        source_build._run(host, ['/usr/bin/mke2fs', '-q', '-F', '-t', 'ext4', '-b', '4096', '-m', '0',
                                 '-L', 'G2SEED', '-U', image_uuid, '-O', '^orphan_file,^metadata_csum_seed',
                                 '-E', 'lazy_itable_init=0,lazy_journal_init=0,root_owner=0:0', '-d', str(payload), '/proc/self/fd/%d' % fd],
                          host.RUN, build_env, 'seed-mke2fs', case_dir, timeout=1800, pass_fds=(fd,), storage_step=True)
        resource_guard.check_prepare()
        named = os.stat(path, follow_symlinks=False)
        host.require((named.st_dev, named.st_ino, named.st_size) == (created.st_dev, created.st_ino, capacity),
                     'seed image replaced during creation')
        commands = ''.join('set_inode_field /%s %s 0\n' % (name, field)
                           for name in [''] + sorted(hashes) for field in ('uid', 'gid')).encode('ascii')
        host.require(len(commands) <= 65536, 'seed ownership command file exceeds limit')
        command_path = case_dir / 'seed-ownership.debugfs'
        source_build._write(command_path, commands, 0o400)
        command_fd = os.open(command_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            command_info = os.fstat(command_fd)
            host.require(stat.S_ISREG(command_info.st_mode) and command_info.st_nlink == 1 and
                         stat.S_IMODE(command_info.st_mode) == 0o400 and command_info.st_size == len(commands) and
                         os.read(command_fd, len(commands) + 1) == commands, 'seed ownership command file changed')
            os.lseek(command_fd, 0, os.SEEK_SET)
            resource_guard.check_prepare(capacity)
            source_build._run(host, ['/usr/bin/debugfs', '-w', '-f', '/proc/self/fd/%d' % command_fd,
                                     '/proc/self/fd/%d' % fd], host.RUN, build_env, 'seed-ownership',
                              case_dir, timeout=1800, pass_fds=(fd, command_fd), storage_step=True)
            resource_guard.check_prepare()
        finally:
            os.close(command_fd)
        named, held = os.stat(path, follow_symlinks=False), os.fstat(fd)
        host.require((named.st_dev, named.st_ino, named.st_size) ==
                     (held.st_dev, held.st_ino, held.st_size) == (created.st_dev, created.st_ino, capacity) and
                     stat.S_ISREG(named.st_mode) and stat.S_ISREG(held.st_mode) and
                     named.st_nlink == held.st_nlink == 1, 'seed image replaced during ownership repair')
        os.fsync(fd)
        os.fchmod(fd, 0o400)
        os.fsync(fd)
    finally:
        os.close(fd)
    _sync_dir(case_dir)
    _verify_payload(host, payload, hashes)
    identity = {'dev': created.st_dev, 'ino': created.st_ino, 'size': capacity, 'sha256': host.digest(path)[0],
                'uuid': image_uuid, 'label': 'G2SEED', 'features': sorted(SEED_FEATURES)}
    _verify_seed_image(host, path, identity, hashes, env)
    host.require(source_build.seed_toolchain(host, env) == state['seed_toolchain'], 'seed toolchain changed during creation')
    return identity


def _cidata(host, state, case_dir, case, scenario, image, hashes, launch, resource_guard):
    source = Path(state['_attempt_dir']) / 'seed'
    target = case_dir / 'cidata'
    os.mkdir(target, 0o700)
    for name in ('network-config', 'user-data'):
        resource_guard.check_prepare(os.stat(source / name, follow_symlinks=False).st_size)
        host.require(source_build._copy(host, source / name, target / name, 0o400) == state['seed_inputs'][name],
                     'CIDATA boot template changed')
        resource_guard.check_prepare()
    meta = ('instance-id: g2-%s-%s-%s\nlocal-hostname: g2-source\n' % (host.TOKEN, case, scenario)).encode('ascii')
    source_build._write(target / 'meta-data', meta, 0o400)
    template = {name: host.digest(target / name)[0] for name in ('meta-data', 'network-config', 'user-data')}
    host.require(hashlib.sha256(host.canonical(template)).hexdigest() == launch['cidata_template_sha256'],
                 'CIDATA template digest differs')
    descriptor = {'version': 1, 'run_token': host.TOKEN, 'case': case, 'scenario': scenario,
                  'payload_case_sha256': hashes['case.json'], 'payload_manifest_sha256': hashes['SHA256SUMS'],
                  'seed_uuid': image['uuid'], 'seed_size': image['size'],
                  'host_launch_sha256': hashlib.sha256(host.canonical(launch)).hexdigest(),
                  'host_argv_sha256': launch['argv_sha256'], 'qemu_sha256': launch['qemu_sha256']}
    source_build._write(target / 'seed-descriptor.json', host.canonical(descriptor), 0o400)
    result = {entry.name: host.digest(entry)[0] for entry in target.iterdir()}
    os.chmod(target, 0o500)
    _sync_dir(target)
    _sync_dir(case_dir)
    _verify_cidata(host, target, result)
    return result


def _raw(host, path):
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.ftruncate(fd, 8 * 1024 ** 3)
        os.fsync(fd)
        info = os.fstat(fd)
        host.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size == 8 * 1024 ** 3,
                     'fresh raw volume invalid')
        return {'dev': info.st_dev, 'ino': info.st_ino, 'size': info.st_size}
    finally:
        os.close(fd)


def _disk_info(host, image, env):
    result = subprocess.run([str(host.RUN / 'build/qemu/qemu-img'), 'info', '--output=json', str(image)],
                            cwd=host.RUN, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=30, check=False)
    host.require(result.returncode == 0 and len(result.stdout) <= 65536 and len(result.stderr) <= 65536,
                 'QEMU image metadata unavailable')
    return host.strict_json(result.stdout)


def expected_vm_argv(host, case_dir, boot_fds, reboot=False, variant=None, case_sha256=None):
    _validate_boot_fd_record(host, boot_fds)
    prefix = 'reboot-' if reboot else ''
    cmdline = BOOT_COMMAND_LINE
    if variant in EARLY_SEAL_VARIANTS:
        host.require(type(case_sha256) is str and re.fullmatch(r'[0-9a-f]{64}', case_sha256) is not None and not reboot,
                     'early seal-gap launch requires case_sha256')
        cmdline = BOOT_COMMAND_LINE + ' g2.bpf_test=' + variant + ' g2.bpf_case=' + case_sha256
    else:
        host.require(variant is None or variant not in EARLY_SEAL_VARIANTS, 'unexpected early seal variant')
    argv = [str(host.RUN / 'build/qemu/qemu-system-x86_64'),
            '-machine', 'q35', '-accel', 'kvm', '-cpu', 'host', '-smp', '2', '-m', '4096',
            '-L', str(host.RUN / 'build/source/qemu-11.1.2/pc-bios'),
            '-display', 'none', '-monitor', 'none', '-qmp', 'stdio',
            '-serial', 'file:' + str(case_dir / (prefix + 'serial.log')), '-nic', 'none', '-no-reboot',
            '-device', 'virtio-serial-pci,id=g2-result-controller',
            '-chardev', 'file,id=g2-result-output,path=' + str(case_dir / (prefix + 'protocol.log')) + ',append=on',
            '-device', 'virtserialport,bus=g2-result-controller.0,nr=1,chardev=g2-result-output,name=' + RESULT_PORT,
            '-drive', 'if=none,id=boot,format=qcow2,file=' + str(case_dir / 'boot.qcow2'),
            '-device', 'virtio-blk-pci,id=bootdisk,drive=boot,bootindex=1']
    argv.extend(('-kernel', '/proc/self/fd/%d' % boot_fds['kernel_fd'],
                 '-initrd', '/proc/self/fd/%d' % boot_fds['initrd_fd'], '-append', cmdline))
    for name, serial in DISKS:
        argv.extend(('-drive', 'if=none,id=' + name + ',format=raw,file=' + str(case_dir / (name + '.raw')),
                     '-device', 'virtio-blk-pci,id=' + name + 'disk,drive=' + name + ',serial=' + serial))
    argv.extend(('-drive', 'if=none,id=cidata,format=raw,readonly=on,file=' + str(case_dir / 'cidata.raw'),
                 '-device', 'virtio-blk-pci,id=cidatadisk,drive=cidata,serial=G2CIDATA',
                 '-drive', 'if=none,id=seed,format=raw,readonly=on,file=' + str(case_dir / 'seed.raw'),
                 '-device', 'virtio-blk-pci,id=seeddisk,drive=seed,serial=G2SEED'))
    return argv


def parse_early_serial_stream(host, data, variant, case_sha256, complete):
    host.require(type(data) is bytes and len(data) <= MAX_PROTOCOL and variant in EARLY_SEAL_VARIANTS and
                 type(case_sha256) is str and re.fullmatch(r'[0-9a-f]{64}', case_sha256) is not None,
                 'early serial stream input invalid')
    begin = None
    chunks = []
    end = None
    lines = data.splitlines(keepends=True)
    if not complete and lines and not lines[-1].endswith(b'\n'):
        lines = lines[:-1]
    for raw_line in lines:
        for pfx in (b'G2_BPF_EARLY_BEGIN ', b'G2_BPF_EARLY_CHUNK ', b'G2_BPF_EARLY_END '):
            pos = raw_line.find(pfx)
            if pos < 0:
                continue
            frame_bytes = raw_line[pos + len(pfx):]
            host.require(frame_bytes.endswith(b'\n') and len(frame_bytes) <= 16384, 'early serial frame invalid')
            obj = host.strict_json(frame_bytes[:-1])
            host.require(host.canonical(obj) == frame_bytes, 'early serial frame not canonical')
            if pfx == b'G2_BPF_EARLY_BEGIN ':
                host.require(begin is None and not chunks and end is None and type(obj) is dict and
                             set(obj) == {'version', 'run_token', 'case_sha256', 'variant', 'bytes', 'sha256', 'chunks'} and
                             obj['version'] == 1 and obj['run_token'] == host.TOKEN and
                             obj['case_sha256'] == case_sha256 and obj['variant'] == variant and
                             type(obj['bytes']) is int and 0 < obj['bytes'] <= MAX_RESULT_BYTES and
                             type(obj['sha256']) is str and re.fullmatch(r'[0-9a-f]{64}', obj['sha256']) is not None and
                             type(obj['chunks']) is int and 0 < obj['chunks'] <= MAX_EARLY_RESULT_CHUNKS, 'early serial BEGIN invalid')
                begin = obj
            elif pfx == b'G2_BPF_EARLY_CHUNK ':
                host.require(begin is not None and end is None and type(obj) is dict and
                             set(obj) == {'sequence', 'data'} and obj['sequence'] == len(chunks) and
                             len(chunks) < begin['chunks'] and type(obj['data']) is str and obj['data'],
                             'early serial CHUNK invalid')
                try:
                    raw_chunk = base64.b64decode(obj['data'].encode('ascii'), validate=True)
                except (binascii.Error, UnicodeError) as exc:
                    raise host.Failure('early serial CHUNK base64 invalid') from exc
                host.require(0 < len(raw_chunk) <= 8192 and
                             base64.b64encode(raw_chunk).decode('ascii') == obj['data'],
                             'early serial CHUNK base64 invalid')
                chunks.append(raw_chunk)
            else:
                host.require(begin is not None and end is None and len(chunks) == begin['chunks'] and
                             type(obj) is dict and
                             set(obj) == {'version', 'run_token', 'case_sha256', 'bytes', 'sha256', 'chunks'} and
                             all(obj[k] == begin[k] for k in ('version', 'run_token', 'case_sha256', 'bytes', 'sha256', 'chunks')),
                             'early serial END invalid')
                end = obj
            break
    if not complete:
        return {'begin': begin, 'chunks': chunks, 'end': end['sha256'] if end is not None else None}
    host.require(begin is not None and end is not None and len(chunks) == begin['chunks'],
                 'early serial stream incomplete')
    payload_bytes = b''.join(chunks)
    host.require(len(payload_bytes) == begin['bytes'] and
                 hashlib.sha256(payload_bytes).hexdigest() == begin['sha256'] and
                 payload_bytes.endswith(b'\n'), 'early serial payload digest or size differs')
    payload = host.strict_json(payload_bytes[:-1])
    host.require(host.canonical(payload) == payload_bytes and type(payload) is dict and
                 set(payload) == {'version', 'run_token', 'case_sha256', 'seal_stage', 'init_handoff', 'terminal_observation'} and
                 payload['version'] == 1 and payload['run_token'] == host.TOKEN and
                 payload['case_sha256'] == case_sha256 and payload['seal_stage'] == variant and
                 payload['init_handoff'] is False and type(payload['terminal_observation']) is dict,
                 'early serial payload schema invalid')
    return {'begin': begin, 'chunks': chunks, 'end': end['sha256'], 'result': payload}

def _parser(host, frozen, case, scenario, hashes, build, cidata):
    expected_install = {'version': 1, 'guest_sha256': hashes['source_guest.py'],
                        'supervisor_manifest_sha256': build['supervisor_manifest_sha256'],
                        'seed_manifest_sha256': hashes['SHA256SUMS'], 'payload_case_sha256': hashes['case.json'],
                        'seed_descriptor_sha256': cidata['seed-descriptor.json']}
    def parse(data, complete):
        prefix = protocol_prefix(host, frozen, data, scenario, complete, expected_install)
        if not complete:
            return prefix
        raw = b''.join(prefix['chunks'])
        host.require(hashlib.sha256(raw).hexdigest() == prefix['end'], 'guest result digest differs')
        value = host.strict_json(raw)
        host.require(type(value) is dict and value.get('version') == 1 and value.get('run_token') == host.TOKEN and
                     value.get('case') == case and value.get('scenario') == scenario, 'guest result identity differs')
        prefix['result'] = value
        return prefix
    return parse


def _same_file(fd, path, identity):
    current, named = os.fstat(fd), os.lstat(path)
    return (current.st_dev, current.st_ino, current.st_uid, stat.S_IMODE(current.st_mode)) == identity == (
        named.st_dev, named.st_ino, named.st_uid, stat.S_IMODE(named.st_mode))


def _read_held_capture(host, fd, path, identity, limit):
    def metadata(info):
        return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
                info.st_size, info.st_mtime_ns, info.st_ctime_ns)
    before = os.fstat(fd)
    host.require(_same_file(fd, path, identity) and stat.S_ISREG(before.st_mode) and
                 before.st_uid == os.geteuid() and before.st_nlink == 1 and
                 stat.S_IMODE(before.st_mode) == 0o600 and 0 <= before.st_size <= limit and
                 metadata(before) == metadata(os.lstat(path)), 'unsafe held capture')
    parts = []
    offset = 0
    while offset < before.st_size:
        block = os.pread(fd, min(1048576, before.st_size - offset), offset)
        host.require(bool(block), 'short held capture')
        parts.append(block)
        offset += len(block)
    host.require(metadata(os.fstat(fd)) == metadata(before) and metadata(os.lstat(path)) == metadata(before),
                 'held capture changed while reading')
    return b''.join(parts)


def _new_capture(path):
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    info = os.fstat(fd)
    identity = (info.st_dev, info.st_ino, info.st_uid, stat.S_IMODE(info.st_mode))
    os.fsync(fd)
    return fd, identity


def _prepare_vm_captures(case_dir, resource_guard, reboot=False):
    prefix = 'reboot-' if reboot else ''
    opened = []
    try:
        protocol_fd, protocol_identity = _new_capture(case_dir / (prefix + 'protocol.log'))
        opened.append(protocol_fd)
        serial_fd, serial_identity = _new_capture(case_dir / (prefix + 'serial.log'))
        opened.append(serial_fd)
        qmp_fd, qmp_identity = _new_capture(case_dir / (prefix + 'qmp.log'))
        opened.append(qmp_fd)
        stderr_fd = os.open(case_dir / (prefix + 'qemu.stderr'), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        opened.append(stderr_fd)
        resource_guard.captures = (protocol_fd, protocol_identity, serial_fd, serial_identity, qmp_fd, qmp_identity, stderr_fd)
    except BaseException:
        for fd in opened:
            os.close(fd)
        raise


def _vm(host, frozen, env, case_dir, case, scenario, hashes, build, attempt, cidata, boot_fds, resource_guard, reboot=False, variant=None):
    early_mode = variant in EARLY_SEAL_VARIANTS and not reboot
    parser = None if early_mode else _parser(host, frozen, case, scenario, hashes, build, cidata)
    prefix = 'reboot-' if reboot else ''
    protocol_path, serial_path = case_dir / (prefix + 'protocol.log'), case_dir / (prefix + 'serial.log')
    qmp_path, stderr_path = case_dir / (prefix + 'qmp.log'), case_dir / (prefix + 'qemu.stderr')
    protocol_fd, protocol_identity, serial_fd, serial_identity, qmp_fd, qmp_identity, stderr_fd = resource_guard.captures
    del resource_guard.captures
    attempt['protocol_identity'] = list(protocol_identity)
    attempt['serial_identity'] = list(serial_identity)
    attempt['qmp_identity'] = list(qmp_identity)
    qlog = os.fdopen(qmp_fd, 'wb', closefd=False)
    proc = pidfd = qmp = collector = None
    failure = None
    complete_failure = False
    try:
        frozen.preflight_pidfd()
        _check_boot_fds(host, build, boot_fds)
        argv = expected_vm_argv(host, case_dir, boot_fds, reboot=reboot, variant=variant, case_sha256=hashes['case.json'])
        attempt['boot_fds'] = dict(boot_fds)
        attempt['argv'] = argv
        attempt['argv_sha256'] = hashlib.sha256(host.canonical(argv)).hexdigest()
        started = time.monotonic()
        if not hasattr(resource_guard, 'deadline'):
            resource_guard.deadline = started + 3600
        deadline = resource_guard.deadline
        proc = subprocess.Popen(argv, cwd=host.RUN, env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, bufsize=0, preexec_fn=frozen.qemu_limits,
                                pass_fds=(boot_fds['kernel_fd'], boot_fds['initrd_fd']))
        attempt['pid'] = proc.pid
        init_error = None
        try:
            pidfd = os.pidfd_open(proc.pid, 0)
            attempt['pidfd_held'] = True
        except BaseException as exc:
            attempt['pidfd_error'] = type(exc).__name__
            init_error = exc
        try:
            qmp = qmp_type(host, frozen)(proc, qlog)
        except BaseException as exc:
            attempt['qmp_init_error'] = type(exc).__name__
            if init_error is None:
                init_error = exc
        collector = frozen.StderrCollector(proc.stderr)
        if qmp is not None:
            def check_live():
                collector.check()
                resource_guard.check_live()
            qmp.check_stderr = check_live
        if init_error is not None:
            raise init_error
        attempt['starttime'] = frozen.proc_starttime(proc.pid)
        exe = os.readlink('/proc/%d/exe' % proc.pid)
        cmdline = Path('/proc/%d/cmdline' % proc.pid).read_bytes()
        host.require(exe == argv[0] and host.digest(Path(exe))[0] == build['qemu_binaries']['qemu-system-x86_64'] and
                     cmdline == b'\0'.join(os.fsencode(arg) for arg in argv) + b'\0', 'QEMU identity or argv differs')
        attempt['proc_argv_sha256'] = hashlib.sha256(cmdline).hexdigest()
        qmp.handshake(min(deadline, time.monotonic() + 20))
        if scenario == 'forbidden-qmp':
            previous_count = len(qmp.sent)
            try:
                qmp.send('dump-guest-memory', min(deadline, time.monotonic() + 5))
            except host.Failure:
                host.require(len(qmp.sent) == previous_count, 'forbidden QMP command was sent')
                attempt['forbidden_qmp_rejected_before_send'] = True
            else:
                raise host.Failure('forbidden QMP command was accepted')
        observations = {}
        for name in ('query-status', 'query-block', 'query-pci', 'query-cpus-fast'):
            observations[name] = qmp.execute(name, min(deadline, time.monotonic() + 20))
        attempt['qmp_initial'] = observations
        ready = False
        early_terminal = False
        while proc.poll() is None:
            host.require(time.monotonic() < deadline, 'guest wall deadline exceeded')
            collector.check()
            resource_guard.check_live()
            qmp.drain_events()
            host.require(_same_file(protocol_fd, protocol_path, protocol_identity) and
                         _same_file(serial_fd, serial_path, serial_identity), 'guest output file replaced')
            host.require(os.fstat(protocol_fd).st_size <= MAX_PROTOCOL and os.fstat(serial_fd).st_size <= MAX_PROTOCOL,
                         'guest output exceeded limit')
            if early_mode:
                sdata = os.pread(serial_fd, os.fstat(serial_fd).st_size, 0)
                estate = parse_early_serial_stream(host, sdata, variant, hashes['case.json'], False)
                if estate['end'] is not None:
                    host.require(proc.poll() is None, 'early terminal stream was not observed while live')
                    parse_early_serial_stream(host, sdata, variant, hashes['case.json'], True)
                    early_terminal = True
                    break
            else:
                data = os.pread(protocol_fd, os.fstat(protocol_fd).st_size, 0)
                prefix_state = parser(data, False)
                observed_ready = prefix_state['ready']
                if observed_ready and not ready:
                    host.require(proc.poll() is None, 'READY was not observed while the guest was alive')
                    ready = True
                if prefix_state['end'] is not None:
                    terminal = parser(data, True)['result']
                    if terminal.get('completed') is False:
                        host.require(ready and proc.poll() is None, 'failed terminal was not observed while ready and live')
                        attempt['ready_while_alive'] = True
                        complete_failure = True
                        raise host.Failure('guest reported terminal failure')
                    if (case == 'realm-negative' and scenario in ('observer-capability', 'bpf-authorization', 'bpf-broker-kill') and
                            terminal.get('completed') is True):
                        break
                if case == 'source-recovery' and scenario == 'vm-kill' and not reboot and prefix_state['end'] is not None:
                    prearmed = parser(data, True)['result']
                    host.require(prearmed.get('completed') is True and prearmed.get('negative', {}).get('prearmed') is True and
                                 prearmed.get('negative', {}).get('source_handoff_pending') is True and proc.poll() is None,
                                 'original VM did not publish a live prearmed source')
                    attempt['kill_ready_sha256'] = prefix_state['end']
                    signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                    attempt['kill_signal'] = 'SIGKILL via held pidfd'
                    proc.wait(timeout=10)
                    break
            select.select([pidfd], [], [], min(0.2, max(0, deadline - time.monotonic())))
        attempt['ready_while_alive'] = ready
        if early_mode:
            attempt['early_terminal_while_alive'] = early_terminal
            host.require(early_terminal and not ready, 'early seal-gap stream not observed while live')
        else:
            host.require(ready, 'guest READY not observed while live')
        if not (case == 'source-recovery' and scenario == 'vm-kill' and not reboot):
            qmp.drain_events()
    except BaseException as exc:
        failure = exc
    finally:
        def cleanup(action):
            nonlocal failure
            try:
                action()
                return True
            except BaseException as exc:
                if isinstance(exc, OSError) and exc.errno == errno.ENOSPC and resource_guard.abort is None:
                    resource_guard.abort = {'reason': 'enospc', 'phase': resource_guard.phase}
                if failure is None:
                    failure = exc
                else:
                    errors = attempt.setdefault('secondary_errors', [])
                    if len(errors) < 8:
                        errors.append(('%s: %s' % (type(exc).__name__, exc))[:256])
                return False
        if proc is not None:
            cleanup(lambda: frozen.held_shutdown(proc, pidfd, qmp, attempt))
            if qmp is not None:
                cleanup(qmp.selector.close)
            if collector is not None:
                def finish_collector():
                    output = collector.finish()
                    os.write(stderr_fd, output)
                    os.fsync(stderr_fd)
                    collector.check()
                cleanup(finish_collector)
            for stream in (proc.stdin, proc.stdout):
                if stream is not None:
                    cleanup(stream.close)
        if pidfd is not None:
            cleanup(lambda: os.close(pidfd))
        qmp_flushed = cleanup(qlog.flush)
        qmp_closed = cleanup(qlog.close)
        qmp_synced = cleanup(lambda: os.fsync(qmp_fd))
        # Keep QMP held until the output identity and hash have been checked.
        cleanup(lambda: os.close(stderr_fd))
    try:
        if complete_failure:
            try:
                host.require(qmp_flushed and qmp_closed and qmp_synced, 'failed QMP capture was not durable')
                os.fsync(protocol_fd)
                os.fsync(serial_fd)
                protocol = _read_held_capture(host, protocol_fd, protocol_path, protocol_identity, MAX_PROTOCOL)
                serial = _read_held_capture(host, serial_fd, serial_path, serial_identity, MAX_PROTOCOL)
                qmp_bytes = _read_held_capture(host, qmp_fd, qmp_path, qmp_identity, MAX_PROTOCOL)
                terminal = parser(protocol, True)['result']
                host.require(terminal.get('completed') is False, 'failed terminal changed')
                encoded = host.canonical(terminal)
                host.require(len(encoded) <= MAX_RESULT_BYTES, 'failed terminal exceeds limit')
                attempt.update(protocol_sha256=hashlib.sha256(protocol).hexdigest(), protocol_bytes=len(protocol),
                               serial_sha256=hashlib.sha256(serial).hexdigest(), serial_bytes=len(serial),
                               qmp_sha256=hashlib.sha256(qmp_bytes).hexdigest(), qmp_bytes=len(qmp_bytes))
                name = prefix + 'failed-result.json'
                source_build._write(case_dir / name, encoded)
                _sync_dir(case_dir)
                attempt['failed_result'] = {'version': 1, 'path': name, 'sha256': hashlib.sha256(encoded).hexdigest(),
                                            'bytes': len(encoded)}
            except BaseException as exc:
                if isinstance(exc, OSError) and exc.errno == errno.ENOSPC and resource_guard.abort is None:
                    resource_guard.abort = {'reason': 'enospc', 'phase': resource_guard.phase}
                errors = attempt.setdefault('secondary_errors', [])
                if len(errors) < 8:
                    errors.append(('failure capture: %s: %s' % (type(exc).__name__, exc))[:256])
        if failure is not None:
            raise failure
        host.require(attempt.get('exit_observed') is True, 'owned guest exit is uncertain')
        host.require(_same_file(protocol_fd, protocol_path, protocol_identity) and
                     _same_file(serial_fd, serial_path, serial_identity) and
                     _same_file(qmp_fd, qmp_path, qmp_identity), 'closed guest output identity differs')
        protocol = os.pread(protocol_fd, os.fstat(protocol_fd).st_size, 0)
        serial = os.pread(serial_fd, os.fstat(serial_fd).st_size, 0)
        attempt.update(protocol_sha256=hashlib.sha256(protocol).hexdigest(), protocol_bytes=len(protocol),
                       serial_sha256=hashlib.sha256(serial).hexdigest(), serial_bytes=len(serial),
                       qmp_sha256=hashlib.sha256(os.pread(qmp_fd, os.fstat(qmp_fd).st_size, 0)).hexdigest(),
                       qmp_bytes=os.fstat(qmp_fd).st_size)
        expected_exit = -signal.SIGKILL if case == 'source-recovery' and scenario == 'vm-kill' and not reboot else 0
        host.require(attempt.get('returncode') == expected_exit, 'guest exited with unexpected status')
        if early_mode:
            host.require(len(protocol) == 0, 'early seal-gap member wrote to normal protocol port')
            return parse_early_serial_stream(host, serial, variant, hashes['case.json'], True)
        return parser(protocol, True)
    finally:
        active = sys.exc_info()[1]
        for fd in (protocol_fd, serial_fd, qmp_fd):
            try:
                os.close(fd)
            except OSError as exc:
                if active is None:
                    raise
                errors = attempt.setdefault('secondary_errors', [])
                if len(errors) < 8:
                    errors.append(('%s: %s' % (type(exc).__name__, exc))[:256])


def _held_disk(host, path, expected_dev, expected_ino, expected_size=None):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info, named = os.fstat(fd), os.lstat(path)
        host.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                     (info.st_dev, info.st_ino) == (named.st_dev, named.st_ino) == (expected_dev, expected_ino) and
                     (expected_size is None or info.st_size == expected_size), 'held disk identity differs')
        return fd
    except BaseException:
        os.close(fd)
        raise


def _check_held_disk(host, fd, path, expected_dev, expected_ino, expected_size=None):
    info, named = os.fstat(fd), os.lstat(path)
    host.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                 (info.st_dev, info.st_ino) == (named.st_dev, named.st_ino) == (expected_dev, expected_ino) and
                 (expected_size is None or info.st_size == expected_size), 'held disk was replaced')


def _held_digest(host, fd):
    before = os.fstat(fd)
    h = hashlib.sha256()
    offset = 0
    while offset < before.st_size:
        data = os.pread(fd, min(1024 * 1024, before.st_size - offset), offset)
        host.require(bool(data), 'held seed image shortened')
        h.update(data)
        offset += len(data)
    after = os.fstat(fd)
    host.require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                 (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                 'held seed image changed while hashing')
    return h.hexdigest()


def _create_cidata_image(host, env, case_dir, qemu_img_sha256):
    qemu_img = str(host.RUN / 'build/qemu/qemu-img')
    host.require(host.digest(Path(qemu_img))[0] == qemu_img_sha256, 'CIDATA exporter binary changed')
    directory = case_dir / 'cidata'
    names = ('meta-data', 'user-data', 'network-config', 'seed-descriptor.json')
    hashes = {name: host.digest(directory / name)[0] for name in names}
    _verify_cidata(host, directory, hashes)
    input_identity = _cidata_identity(directory, hashes)
    source = 'driver=vvfat,dir=' + str(directory) + ',label=CIDATA,rw=off,read-only=on'
    result = subprocess.run([qemu_img, 'info', '--output=json', '--image-opts', source], env=env,
                            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=60, check=False)
    host.require(result.returncode == 0 and len(result.stdout) <= 65536 and len(result.stderr) <= 16384,
                 'CIDATA block geometry unavailable')
    info = host.strict_json(result.stdout)
    size = info.get('virtual-size') if type(info) is dict else None
    host.require(type(size) is int and size == 1024 * 16 * 63 * 512,
                 'CIDATA block geometry invalid')
    path = case_dir / 'cidata.raw'
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        _sync_dir(case_dir)
        os.ftruncate(fd, size)
        os.fsync(fd)
        created = os.fstat(fd)
        host.require(stat.S_ISREG(created.st_mode) and created.st_nlink == 1 and
                     created.st_uid == os.geteuid() and created.st_size == size,
                     'new CIDATA image invalid')
        source_build._run(host, [qemu_img, 'convert', '--image-opts', '-n', '-O', 'raw', source,
                                 '/proc/self/fd/%d' % fd], host.RUN, env, 'cidata-image',
                          case_dir, timeout=1800, pass_fds=(fd,), storage_step=True)
        _check_held_disk(host, fd, path, created.st_dev, created.st_ino, size)
        _verify_cidata(host, directory, hashes)
        host.require(_cidata_identity(directory, hashes) == input_identity,
                     'CIDATA inputs changed during export')
        os.fsync(fd)
        os.fchmod(fd, 0o400)
        os.fsync(fd)
        _sync_dir(case_dir)
        digest = _held_digest(host, fd)
        _check_held_disk(host, fd, path, created.st_dev, created.st_ino, size)
        host.require(host.digest(Path(qemu_img))[0] == qemu_img_sha256, 'CIDATA exporter binary changed during readback')
        identity = {'path': 'cidata.raw', 'dev': created.st_dev, 'ino': created.st_ino,
                    'size': size, 'uid': created.st_uid, 'mode': 0o400, 'sha256': digest}
    finally:
        os.close(fd)
    held = _held_disk(host, path, identity['dev'], identity['ino'], size)
    try:
        info = os.fstat(held)
        host.require(info.st_uid == identity['uid'] and stat.S_IMODE(info.st_mode) == identity['mode'] and
                     _held_digest(host, held) == identity['sha256'], 'CIDATA image read-only reopen differs')
        _check_held_disk(host, held, path, identity['dev'], identity['ino'], size)
        return held, identity
    except BaseException:
        os.close(held)
        raise


def _run_single_vm_case(host, frozen, env, case_dir, case, scenario, build, variant=None, manifest_sha256=None):
    import source_verify
    os.mkdir(case_dir, 0o700)
    receipt = _CaseReceipt(host, case_dir)
    record = {'version': 2, 'run_token': host.TOKEN, 'case': case, 'scenario': scenario,
              'bpf_variant': variant, 'launch_manifest_sha256': manifest_sha256,
              'build_selection': build['_selection'], 'browser_selection': build['browser_selection'],
              'status': 'IN_PROGRESS', 'stage': 'resource-gate', 'attempt': {}}
    held_disks = {}
    guard = None
    reboot_result = None
    try:
        record['resource_budget'] = source_resources.make_budget(host, build, _case_bytes(host, build, case, scenario, variant))
        guard = source_resources.SourceResourceGuard(host, case_dir, record['resource_budget'])
        record['resources'] = guard.check_prepare()
        receipt.save(record)
        base = Path(frozen.load_json(host.RUN / 'prepare.json')['assets']['ubuntu:' + frozen.IMAGE])
        base_pin = frozen.UBUNTU_HASHES[frozen.IMAGE]
        host.require(host.digest(base)[0] == base_pin, 'boot base changed')
        record['base_before'] = base_pin
        boot_fds = _open_boot_fds(host, build)
        held_disks.update(kernel=boot_fds['kernel_fd'], initrd=boot_fds['initrd_fd'])
        record['boot_inputs'] = build['boot_inputs']
        record['boot_fds'] = boot_fds
        record['stage'] = 'disks'
        receipt.save(record)
        guard.check_prepare()
        source_build._run(host, [str(host.RUN / 'build/qemu/qemu-img'), 'create', '-f', 'qcow2', '-F', 'qcow2',
                                '-b', str(base), str(case_dir / 'boot.qcow2')], host.RUN, env, 'overlay-create', case_dir, 60, storage_step=True)
        guard.check_prepare()
        source_build._run(host, [str(host.RUN / 'build/qemu/qemu-img'), 'resize', str(case_dir / 'boot.qcow2'), '16G'],
                          host.RUN, env, 'overlay-resize', case_dir, 60, storage_step=True)
        guard.check_prepare()
        boot_info = _disk_info(host, case_dir / 'boot.qcow2', env)
        host.require(boot_info.get('format') == 'qcow2' and boot_info.get('backing-filename') == str(base),
                     'boot overlay backing differs')
        disks = {}
        for name, serial in DISKS:
            path = case_dir / (name + '.raw')
            identity = _raw(host, path)
            info = _disk_info(host, path, env)
            host.require(info.get('format') == 'raw' and info.get('virtual-size') == 8 * 1024 ** 3 and
                         not info.get('backing-filename'), 'raw volume has backing or incorrect size')
            identity.update(serial=serial, sha256=host.digest(path)[0])
            disks[name] = identity
        host.require(len({(v['dev'], v['ino']) for v in disks.values()}) == 4 and
                     len({v['sha256'] for v in disks.values()}) == 1, 'fresh volume identities differ')
        record['disks_before'] = disks
        boot = os.stat(case_dir / 'boot.qcow2', follow_symlinks=False)
        held_disks['boot'] = _held_disk(host, case_dir / 'boot.qcow2', boot.st_dev, boot.st_ino)
        for name, _ in DISKS:
            held_disks[name] = _held_disk(host, case_dir / (name + '.raw'), disks[name]['dev'],
                                          disks[name]['ino'], disks[name]['size'])
        hashes = _seed_payload(host, build, case_dir, case, scenario, variant=variant, resource_guard=guard)
        host.require(sum(os.stat(case_dir / 'payload' / name, follow_symlinks=False).st_size for name in hashes) ==
                     record['resource_budget']['payload_bytes'], 'payload size changed after budgeting')
        guard.check_prepare(record['resource_budget']['seed_capacity_bytes'])
        image = _seed_image(host, env, build, case_dir, hashes, guard)
        guard.check_prepare()
        held_disks['seed'] = _held_disk(host, case_dir / 'seed.raw', image['dev'], image['ino'], image['size'])
        meta = ('instance-id: g2-%s-%s-%s\nlocal-hostname: g2-source\n' % (host.TOKEN, case, scenario)).encode('ascii')
        template = {'meta-data': hashlib.sha256(meta).hexdigest(),
                    'network-config': build['seed_inputs']['network-config'], 'user-data': build['seed_inputs']['user-data']}
        argv_expected = expected_vm_argv(host, case_dir, boot_fds, variant=variant, case_sha256=hashes['case.json'])
        launch = {'version': 1, 'run_token': host.TOKEN, 'case': case, 'scenario': scenario,
                  'bpf_variant': variant,
                  'build_selection': build['_selection'],
                  'argv_sha256': hashlib.sha256(host.canonical(argv_expected)).hexdigest(),
                  'boot_inputs': build['boot_inputs'], 'boot_fds': boot_fds,
                  'qemu_sha256': build['qemu_binaries']['qemu-system-x86_64'],
                  'base_sha256': base_pin, 'boot': {'dev': boot.st_dev, 'ino': boot.st_ino,
                                                    'size': boot.st_size,
                                                    'sha256': host.digest(case_dir / 'boot.qcow2')[0]},
                  'disks': disks, 'seed_inputs_sha256': hashlib.sha256(host.canonical(build['seed_inputs'])).hexdigest(),
                  'seed_image': image, 'payload_case_sha256': hashes['case.json'],
                  'cidata_template_sha256': hashlib.sha256(host.canonical(template)).hexdigest()}
        record['host_launch'] = launch
        record['host_launch_sha256'] = hashlib.sha256(host.canonical(launch)).hexdigest()
        guard.check_prepare()
        cidata = _cidata(host, build, case_dir, case, scenario, image, hashes, launch, guard)
        guard.check_prepare()
        cidata_identity = _cidata_identity(case_dir / 'cidata', cidata)
        record.update(seed=hashes, seed_image=image, cidata=cidata, cidata_identity=cidata_identity,
                      cidata_sha256=hashlib.sha256(host.canonical(cidata)).hexdigest())
        receipt.save(record)
        guard.check_prepare(source_resources.CIDATA_CAPACITY_BYTES)
        cidata_fd, cidata_image = _create_cidata_image(host, env, case_dir, build['qemu_binaries']['qemu-img'])
        guard.check_prepare()
        held_disks['cidata'] = cidata_fd
        _verify_cidata(host, case_dir / 'cidata', cidata)
        host.require(_cidata_identity(case_dir / 'cidata', cidata) == cidata_identity,
                     'CIDATA identity changed during image creation')
        record['cidata_image'] = cidata_image
        record['cidata_block'] = {'size': cidata_image['size'], 'sha256': cidata_image['sha256']}
        def check_carriers(stage):
            _check_boot_fds(host, build, boot_fds)
            record.setdefault('boot_observations', []).append({'stage': stage, 'boot_fds': dict(boot_fds)})
            _check_held_disk(host, held_disks['seed'], case_dir / 'seed.raw', image['dev'], image['ino'], image['size'])
            info = os.fstat(held_disks['seed'])
            sha = _held_digest(host, held_disks['seed'])
            host.require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o400 and
                         sha == image['sha256'], 'held seed image content changed')
            _check_held_disk(host, held_disks['seed'], case_dir / 'seed.raw', image['dev'], image['ino'], image['size'])
            named = os.stat(case_dir / 'seed.raw', follow_symlinks=False)
            _verify_cidata(host, case_dir / 'cidata', cidata)
            observed_cidata = _cidata_identity(case_dir / 'cidata', cidata)
            host.require(observed_cidata == cidata_identity, 'CIDATA identity changed')
            _verify_payload(host, case_dir / 'payload', hashes)
            _check_held_disk(host, cidata_fd, case_dir / 'cidata.raw', cidata_image['dev'],
                             cidata_image['ino'], cidata_image['size'])
            cidata_held = os.fstat(cidata_fd)
            cidata_sha = _held_digest(host, cidata_fd)
            _check_held_disk(host, cidata_fd, case_dir / 'cidata.raw', cidata_image['dev'],
                             cidata_image['ino'], cidata_image['size'])
            cidata_named = os.stat(case_dir / 'cidata.raw', follow_symlinks=False)
            _verify_cidata(host, case_dir / 'cidata', cidata)
            host.require(_cidata_identity(case_dir / 'cidata', cidata) == cidata_identity,
                         'CIDATA changed during image readback')
            def identity(item):
                return {'dev': item.st_dev, 'ino': item.st_ino, 'size': item.st_size,
                        'uid': item.st_uid, 'mode': stat.S_IMODE(item.st_mode)}
            host.require(identity(info) == identity(named), 'held and named seed metadata differ')
            expected_cidata = {key: cidata_image[key] for key in ('dev', 'ino', 'size', 'uid', 'mode')}
            host.require(identity(cidata_held) == identity(cidata_named) == expected_cidata and
                         cidata_sha == cidata_image['sha256'], 'held CIDATA image content or identity changed')
            record.setdefault('carrier_observations', []).append(
                {'stage': stage, 'seed_held': identity(info), 'seed_named': identity(named), 'seed_sha256': sha,
                 'cidata_identity': observed_cidata, 'cidata': dict(cidata), 'cidata_held': identity(cidata_held),
                 'cidata_named': identity(cidata_named), 'cidata_sha256': cidata_sha})
            receipt.save(record)
        def execute_vm(attempt, reboot=False):
            failure = None
            result = None
            try:
                if reboot:
                    result = _vm(host, frozen, env, case_dir, case, scenario, hashes, build, attempt,
                                 cidata, boot_fds, guard, reboot=True)
                else:
                    result = _vm(host, frozen, env, case_dir, case, scenario, hashes, build, attempt,
                                 cidata, boot_fds, guard, variant=variant)
            except BaseException as exc:
                failure = exc
            checks = {'exit': 'PASS' if attempt.get('exit_observed') is True and attempt.get('pidfd_held') is True else 'UNKNOWN',
                      'carriers': 'UNKNOWN', 'resources': 'UNKNOWN'}
            if attempt.get('exit_observed') is True:
                for key, action in (('resources', lambda: guard.after_wait(reboot)),
                                    ('carriers', lambda: check_carriers('after_reboot_wait' if reboot else 'after_wait'))):
                    try:
                        action()
                        checks[key] = 'PASS'
                    except BaseException as exc:
                        if failure is None:
                            failure = exc
                        else:
                            if isinstance(exc, source_resources.SourceResourceError) and guard.abort is None:
                                guard.abort = {'reason': exc.reason, 'phase': exc.phase}
                            if isinstance(exc, OSError) and exc.errno == errno.ENOSPC and guard.abort is None:
                                guard.abort = {'reason': 'enospc', 'phase': guard.phase}
                            errors = record.setdefault('secondary_errors', [])
                            if len(errors) < 8:
                                errors.append(('%s: %s' % (type(exc).__name__, exc))[:256])
                        checks[key] = 'FAIL'
            if failure is not None:
                record['failure_checks'] = checks
            if failure is not None:
                raise failure
            return result
        record['stage'] = 'vm'
        receipt.save(record)
        check_carriers('before_launch')
        _prepare_vm_captures(case_dir, guard)
        guard.check_prepare()
        record['launch_resources'] = guard.begin_launch()
        receipt.save(record)
        bundle = execute_vm(record['attempt'])
        record['initial_resource_usage'] = guard.snapshot()
        receipt.save(record)
        if case == 'source-recovery' and scenario == 'vm-kill':
            _check_held_disk(host, held_disks['boot'], case_dir / 'boot.qcow2', boot.st_dev, boot.st_ino)
            for name, _ in DISKS:
                _check_held_disk(host, held_disks[name], case_dir / (name + '.raw'), disks[name]['dev'],
                                 disks[name]['ino'], disks[name]['size'])
            check_carriers('before_reboot')
            record['reboot_disks'] = {'boot': {'dev': boot.st_dev, 'ino': boot.st_ino},
                                      **{name: {key: disks[name][key] for key in ('dev', 'ino', 'size')}
                                         for name, _ in DISKS}}
            record['reboot_attempt'] = {}
            record['stage'] = 'same-volume-reboot'
            _prepare_vm_captures(case_dir, guard, reboot=True)
            record['reboot_resources'] = guard.begin_launch(reboot=True)
            receipt.save(record)
            reboot = execute_vm(record['reboot_attempt'], reboot=True)
            reboot_result = reboot['result']
            source_build._write(case_dir / 'reboot-parsed.json', host.canonical(reboot['result']))
            record['reboot_parsed_sha256'] = host.digest(case_dir / 'reboot-parsed.json')[0]
            _check_held_disk(host, held_disks['boot'], case_dir / 'boot.qcow2', boot.st_dev, boot.st_ino)
        host.require(host.digest(base)[0] == base_pin, 'boot base changed during guest')
        for name, _ in DISKS:
            _check_held_disk(host, held_disks[name], case_dir / (name + '.raw'), disks[name]['dev'],
                             disks[name]['ino'], disks[name]['size'])
        source_verify._qmp(host, case_dir / 'qmp.log', record['attempt'], case_dir)
        if 'reboot_attempt' in record:
            source_verify._qmp(host, case_dir / 'reboot-qmp.log', record['reboot_attempt'], case_dir)
        if variant in EARLY_SEAL_VARIANTS:
            source_verify.validate_early_seal_result(host, case, scenario, variant, bundle['result'], record, build)
        else:
            source_verify.validate_result(host, case, scenario, bundle['result'], record, build, reboot_result=reboot_result)
        source_build._write(case_dir / 'parsed.json', host.canonical(bundle['result']))
        record['final_resources'] = guard.finish_member()
        record['resource_usage'] = guard.snapshot()
        record.update(status='PASS', stage='complete', base_after=base_pin, seed_unchanged=True,
                      parsed_sha256=host.digest(case_dir / 'parsed.json')[0])
        receipt.save(record)
        return True, None
    except BaseException as exc:
        def chain(error):
            seen = set()
            while error is not None and id(error) not in seen:
                seen.add(id(error))
                yield error
                error = error.__cause__
        errors = list(chain(exc))
        resource = next((e for e in errors if isinstance(e, source_resources.SourceResourceError)), None)
        enospc = next((e for e in errors if isinstance(e, OSError) and e.errno == errno.ENOSPC), None)
        storage = next((e for e in errors if isinstance(e, source_build.StorageToolFailure)), None)
        if resource is None and enospc is not None:
            phase = 'receipt' if receipt.failed else (guard.phase if guard is not None and guard.phase in source_resources._PHASES else 'prepare')
            resource = source_resources.SourceResourceError('enospc', phase)
            resource.__cause__ = enospc
        if resource is None and guard is not None and guard.abort is not None:
            resource = source_resources.SourceResourceError(guard.abort['reason'], guard.abort['phase'])
        if guard is not None and resource is not None and guard.abort is None:
            guard.abort = {'reason': resource.reason, 'phase': resource.phase}
        if storage is not None and isinstance(storage.__cause__, host.Failure):
            secondary = record.setdefault('secondary_errors', [])
            if len(secondary) < 8:
                detail = storage.__cause__
                secondary.append(('%s: %s' % (type(detail).__name__, detail))[:256])
        for attempt_state in (record['attempt'], record.get('reboot_attempt', {})):
            for detail in attempt_state.get('secondary_errors', []):
                secondary = record.setdefault('secondary_errors', [])
                if len(secondary) < 8:
                    secondary.append(detail[:256])
        record.update(status='FAIL', error_type=type(exc).__name__, error=str(exc)[:512],
                      failure_kind='resource' if isinstance(exc, source_resources.SourceResourceError) or
                      (enospc is not None and (exc is enospc or isinstance(exc, source_build.StorageToolFailure))) else 'source')
        record.setdefault('failure_checks', {
            'exit': 'PASS' if record.get('attempt', {}).get('pidfd_held') is True and
                              record.get('attempt', {}).get('exit_observed') is True else 'UNKNOWN',
            'carriers': 'UNKNOWN', 'resources': 'UNKNOWN'})
        if guard is not None:
            record['resource_usage'] = guard.snapshot()
        if resource is not None:
            record['resource_abort'] = {'reason': resource.reason, 'phase': resource.phase}
        if storage is not None:
            record['storage_tool_failure'] = {'label': storage.label[:128], 'returncode': storage.returncode,
                                              'stdout': storage.stdout.name, 'stderr': storage.stderr.name}
        persistence_error = None
        try:
            receipt.save(record)
        except BaseException as save_error:
            persistence_error = save_error
            errors = record.setdefault('secondary_errors', [])
            if len(errors) < 8:
                detail = receipt.failure if receipt.failure is not None else save_error
                errors.append(('receipt persistence: %s: %s' % (type(detail).__name__, detail))[:256])
            print('%s: %s; receipt persistence failed: %s' %
                  (type(exc).__name__, exc, receipt.failure if receipt.failure is not None else save_error), file=sys.stderr)
            if isinstance(save_error, OSError) and save_error.errno == errno.ENOSPC:
                resource = source_resources.SourceResourceError('enospc', 'receipt')
        if isinstance(exc, (KeyboardInterrupt, SystemExit)):
            raise
        if record['attempt'].get('pid') is not None and record['attempt'].get('exit_observed') is not True:
            raise host.Failure('owned VM exit remains uncertain') from exc
        if record.get('reboot_attempt', {}).get('pid') is not None and record['reboot_attempt'].get('exit_observed') is not True:
            raise host.Failure('reboot VM exit remains uncertain') from exc
        receipt_enospc = (isinstance(receipt.failure, OSError) and receipt.failure.errno == errno.ENOSPC)
        if receipt.publication_unknown or (persistence_error is not None and not receipt_enospc):
            raise host.Failure('receipt persistence failed; first cause: %s' % exc) from exc
        return False, 'resource_capacity' if resource is not None else ('storage_tool_failure' if storage is not None else None)
    finally:
        if guard is not None and hasattr(guard, 'captures'):
            for fd in (guard.captures[0], guard.captures[2], guard.captures[4], guard.captures[6]):
                try:
                    os.close(fd)
                except OSError:
                    pass
        for fd in held_disks.values():
            try:
                os.close(fd)
            except OSError:
                pass
        try:
            receipt.close()
        except OSError:
            pass


def _case(host, frozen, env, root, case, scenario, build, manifest_sha256=None):
    if case == 'realm-negative' and scenario == 'bpf-authorization':
        family_dir = root / 'bpf-authorization'
        os.mkdir(family_dir, 0o700)
        _sync_dir(root)
        members_out = []
        all_ok = True
        stop_reason = None
        for m_case, m_scenario, m_variant in BPF_AUTH_FAMILY_MEMBERS:
            rel_dir = 'bpf-authorization/' + m_variant
            member_dir = family_dir / m_variant
            if stop_reason is None:
                for prior in members_out:
                    prior['retained_allocated_bytes'] = source_resources.allocated_tree_bytes(host, root / prior['directory'])
                ok, stop_reason = _run_single_vm_case(host, frozen, env, member_dir, m_case, m_scenario, build,
                                                      variant=m_variant, manifest_sha256=manifest_sha256)
                receipt_path = member_dir / 'receipt.json'
                r_sha = host.digest(receipt_path)[0] if receipt_path.exists() else None
                if not ok or r_sha is None:
                    all_ok = False
                retained = source_resources.allocated_tree_bytes(host, member_dir)
                status = 'PASS' if ok and r_sha is not None else 'FAIL'
            else:
                r_sha, retained, status = None, 0, 'NOT_RUN'
                all_ok = False
            members_out.append({'variant': m_variant, 'case': m_case, 'scenario': m_scenario,
                                'directory': rel_dir, 'receipt_sha256': r_sha, 'status': status,
                                'retained_allocated_bytes': retained})
        family_index = {'version': 2, 'case': case, 'scenario': scenario, 'run_token': host.TOKEN,
                        'build_selection': build['_selection'], 'launch_manifest_sha256': manifest_sha256,
                        'members': members_out, 'stop_reason': stop_reason,
                        'retained_allocated_bytes': sum(m['retained_allocated_bytes'] for m in members_out)}
        family_bytes = host.canonical(family_index)
        host.require(len(family_bytes) <= 16384, 'family index exceeds 16KiB limit')
        source_build._write(family_dir / 'family.json', family_bytes, 0o400)
        _sync_dir(family_dir)
        return all_ok
    ok, _ = _run_single_vm_case(host, frozen, env, root / scenario, case, scenario, build,
                                variant=None, manifest_sha256=manifest_sha256)
    return ok


def run(host, frozen, env, case, selected_scenario=None):
    host.require(case in host.CASES and (selected_scenario is None or selected_scenario in scenarios(case)),
                 'unknown source case or scenario')
    lock_fd = source_build.lock_run(host)
    try:
        build = source_build.build_gate(host, frozen)
        host.require(build['qemu_binaries'] == frozen.prepare_import.QEMU_PINS, 'QEMU binary pins differ')
        runs = host.RUN / 'runs'
        _mkdir_private(host, runs)
        selection_dir = runs / ('selection-' + build['_selection']['selection_id'])
        _mkdir_private(host, selection_dir)
        case_dir = selection_dir / case
        _mkdir_private(host, case_dir)
        attempts = source_build._numbered(host, case_dir, 'attempt-', '')
        attempt_id = '%020d' % (len(attempts) + 1)
        root = case_dir / ('attempt-' + attempt_id)
        os.mkdir(root, 0o700)
        _sync_dir(case_dir)
        declared_scenarios = sorted(scenarios(case) if selected_scenario is None else (selected_scenario,))
        members_list = []
        for sc in declared_scenarios:
            if case == 'realm-negative' and sc == 'bpf-authorization':
                for m_case, m_scenario, m_variant in BPF_AUTH_FAMILY_MEMBERS:
                    members_list.append({'case': m_case, 'scenario': m_scenario, 'variant': m_variant})
            else:
                members_list.append({'case': case, 'scenario': sc, 'variant': None})
        launch_manifest = {
            'version': 1,
            'run_token': host.TOKEN,
            'build_selection': build['_selection'],
            'attempt': attempt_id,
            'case': case,
            'scenarios': declared_scenarios,
            'members': members_list,
        }
        manifest_bytes = host.canonical(launch_manifest)
        manifest_sha256 = hashlib.sha256(manifest_bytes).hexdigest()
        source_build._write(root / 'launch-manifest.json', manifest_bytes, 0o400)
        _sync_dir(root)
        def terminate(signum, _frame):
            signal.signal(signum, signal.SIG_IGN)
            raise SystemExit('runner received signal %d' % signum)
        handlers = {sig: signal.signal(sig, terminate) for sig in (signal.SIGTERM, signal.SIGHUP)}
        try:
            complete = True
            for scenario in scenarios(case) if selected_scenario is None else (selected_scenario,):
                if not _case(host, frozen, env, root, case, scenario, build, manifest_sha256=manifest_sha256):
                    complete = False
            return 0 if complete else 1
        finally:
            for sig, handler in handlers.items():
                signal.signal(sig, handler)
    finally:
        os.close(lock_fd)
