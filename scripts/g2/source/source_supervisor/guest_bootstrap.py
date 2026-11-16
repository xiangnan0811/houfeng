"""Offline installation of fixed guest programs and Docker, before genesis."""
import array
import base64
import errno
import hashlib
import math
import os
import pwd
import grp
import re
import secrets
import resource
import select
import shutil
import signal
import socket
import stat
import subprocess
import struct
import sys
import threading
import time

import guest as carrier
import network_verify
import source_settlement_evidence
from contract import MIGRATION_BINDING, MIGRATION_CGROUP, RUN_TOKEN, scenarios
from guest_storage import ENV, command
from journal import JournalError, boot_id, canonical, process_start, strict_json

CIDATA = '/mnt/g2-cidata'
SEED = '/mnt/g2-seed'
RUN = '/run/g2'
OWNER_PROBE_SOURCE = '/run/g2/postgres-owner-probe'
OWNER_PROBE_TARGET = '/var/lib/postgresql/data'
OWNER_SETTLEMENT_SOCKET = '/run/g2/owner-probe-settlement-evidence/broker.sock'
PROGRAMS = ('c80-initializer', 'c80-launcher', 'c80-predecessor', 'c81-migrator',
            'domain-initializer', 'houfeng-record-platform-admin', 'dns-probe', 'c80-comparison-negative', 'c80-backup-negative',
            'g2-bpf-authority')
SUPERVISOR = ('source_guest.py', 'source_protocol.py', 'source_settlement_evidence.py', 'contract.py', 'census.py', 'journal.py', 'maintenance.py', 'smoke.py', 'phase.py', 'permit.py',
              'ipc.py', 'audit.py', 'lifecycle.py', 'guest_storage.py', 'guest_bootstrap.py', 'guest_postgres.py',
              'pg_bootstrap.py', 'systemd_realm.py', 'run_stop_observer.py', 'volume_verify.py', 'origins.py', 'invoke.py', 'heartbeat.py',
              'genesis.py', 'ceremony.py', 'cutpoints.py', 'network_verify.py', 'restart.py', 'dns.py',
              'fence_transition.py', 'pg_session.py', 'negative_channel.py', 'comparison_fixture.py',
              'extra_barrier.py', 'physical_backup.py', 'wrong_rpc.py', 'reboot_observer.py',
              'listener_negative.py', 'callback_lifecycle.py', 'browser_process.py', 'app_rollback.py', 'bpf_authority.py')
CARRIER = ('guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py')
PACKAGES = {'docker-ce.deb': 'b53a44a6ff13277710978adbdbfea27fcdd7ac9f198017e733d2b8dc2343f8dc',
            'docker-ce-cli.deb': '008b3b49b474136063fe0fbe8999b0e77c3e8556d7f7de21f98f05715e2f76ba',
            'containerd.io.deb': '2eb8c6e244fe6886f2fa2eee9ec418c4b9bb44eb44fca748504f57c23341aed2'}
PG_ID = 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610'
BROWSER_PROGRAM_SHA256 = '670ba079b75107746ba41abad131180a31a7c7219aa1bd4061fb471f4535d541'
SEED_FILES = frozenset(SUPERVISOR + CARRIER + PROGRAMS + tuple(PACKAGES) +
                       ('postgres-16.12.tar', 'browser-selection.json', 'browser-image.tar',
                        'supervisor-manifest.json', 'case.json', 'SHA256SUMS'))


def _digest(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            raise JournalError('seed input is not regular')
        h = hashlib.sha256()
        while True:
            data = os.read(fd, 1024 * 1024)
            if not data:
                break
            h.update(data)
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise JournalError('seed input changed while hashing')
        return h.hexdigest()
    finally:
        os.close(fd)


def browser_selection(expected):
    with open(SEED + '/browser-selection.json', 'rb') as stream:
        raw = stream.read(4097)
    selection = strict_json(raw)
    keys = {'version', 'run_token', 'selection_id', 'attempt_id', 'receipt_sha256', 'manifest_sha256',
            'rootfs_sha256', 'image_id', 'image_archive_sha256', 'program_sha256', 'bundle_sha256'}
    hashes = ('receipt_sha256', 'manifest_sha256', 'rootfs_sha256', 'image_archive_sha256', 'program_sha256', 'bundle_sha256')
    if (type(selection) is not dict or set(selection) != keys or type(selection['version']) is not int or
            selection['version'] != 1 or selection['run_token'] != RUN_TOKEN or canonical(selection) + b'\n' != raw or
            any(type(selection[key]) is not str or re.fullmatch(r'[0-9]{20}', selection[key]) is None
                for key in ('selection_id', 'attempt_id')) or
            any(type(selection[key]) is not str or re.fullmatch(r'[0-9a-f]{64}', selection[key]) is None for key in hashes) or
            type(selection['image_id']) is not str or re.fullmatch(r'sha256:[0-9a-f]{64}', selection['image_id']) is None or
            selection['program_sha256'] != BROWSER_PROGRAM_SHA256 or
            hashlib.sha256(raw).hexdigest() != expected['browser-selection.json'] or
            selection['image_archive_sha256'] != expected['browser-image.tar']):
        raise JournalError('browser selection does not match the pinned seed')
    return selection


def verify_seed(copy_dir=RUN):
    if copy_dir not in (RUN, SEED):
        raise JournalError('seed observation directory invalid')
    import guest_storage
    hardware = guest_storage.verify_guest()
    if set(os.listdir(SEED)) != SEED_FILES | {'lost+found'} or os.listdir(SEED + '/lost+found'):
        raise JournalError('unexpected seed inventory')
    if set(os.listdir(CIDATA)) != {'meta-data', 'network-config', 'user-data', 'seed-descriptor.json'}:
        raise JournalError('unexpected CIDATA inventory')
    with open(SEED + '/SHA256SUMS', 'r', encoding='ascii') as stream:
        lines = stream.read(65537).splitlines()
    if len(lines) > len(SEED_FILES) or len('\n'.join(lines)) > 65536:
        raise JournalError('seed manifest exceeds limit')
    expected = {}
    for line in lines:
        if len(line) < 67 or line[64:66] != '  ':
            raise JournalError('seed manifest malformed')
        sha, name = line[:64], line[66:]
        if (len(sha) != 64 or any(ch not in '0123456789abcdef' for ch in sha) or
                name not in SEED_FILES - {'SHA256SUMS'} or name in expected):
            raise JournalError('seed manifest identity invalid')
        expected[name] = sha
    if set(expected) != SEED_FILES - {'SHA256SUMS'}:
        raise JournalError('seed manifest incomplete')
    for name, sha in expected.items():
        if _digest(SEED + '/' + name) != sha:
            raise JournalError('seed input digest differs')
    if any(expected[name] != sha for name, sha in PACKAGES.items()):
        raise JournalError('offline package pin differs')
    browser_selection(expected)
    for name in SUPERVISOR + CARRIER:
        if _digest(copy_dir + '/' + name) != expected[name]:
            raise JournalError('guest bootstrap copy differs')
    with open(SEED + '/case.json', 'rb') as stream:
        case_raw = stream.read(4097)
    case = strict_json(case_raw)
    if (len(case_raw) > 4096 or type(case) is not dict or set(case) !=
            {'version', 'run_token', 'case', 'scenario', 'bpf_variant', 'qemu_sha256', 'boot_inputs', 'bpf_bootstrap'} or
            type(case['version']) is not int or case['version'] != 1 or case['run_token'] != RUN_TOKEN or
            type(case['case']) is not str or type(case['scenario']) is not str or
            (case['bpf_variant'] is not None and (type(case['bpf_variant']) is not str or not case['bpf_variant'])) or
            type(case['qemu_sha256']) is not str or re.fullmatch(r'[0-9a-f]{64}', case['qemu_sha256']) is None or
            case_raw != canonical(case) + b'\n' or case['scenario'] not in scenarios(case['case'])):
        raise JournalError('guest case binding invalid')
    _verify_boot_inputs(case, expected)
    with open(CIDATA + '/seed-descriptor.json', 'rb') as stream:
        descriptor_raw = stream.read(16385)
    descriptor = strict_json(descriptor_raw)
    keys = {'version', 'run_token', 'case', 'scenario', 'payload_case_sha256', 'payload_manifest_sha256',
            'seed_uuid', 'seed_size', 'host_launch_sha256', 'host_argv_sha256', 'qemu_sha256'}
    digests = ('payload_case_sha256', 'payload_manifest_sha256', 'host_launch_sha256', 'host_argv_sha256', 'qemu_sha256')
    if (type(descriptor) is not dict or set(descriptor) != keys or descriptor_raw != canonical(descriptor) + b'\n' or
            type(descriptor['version']) is not int or descriptor['version'] != 1 or
            any(descriptor[key] != case[key] for key in ('run_token', 'case', 'scenario', 'qemu_sha256')) or
            any(type(descriptor[key]) is not str or re.fullmatch(r'[0-9a-f]{64}', descriptor[key]) is None
                for key in digests) or descriptor['payload_case_sha256'] != hashlib.sha256(case_raw).hexdigest() or
            descriptor['payload_manifest_sha256'] != _digest(SEED + '/SHA256SUMS') or
            type(descriptor['seed_size']) is not int or descriptor['seed_size'] != hardware['seed_mount']['bytes'] or
            descriptor['seed_uuid'] != hardware['seed_mount']['uuid']):
        raise JournalError('CIDATA and payload bindings differ')
    expected_meta = ('instance-id: g2-%s-%s-%s\nlocal-hostname: g2-source\n' %
                     (RUN_TOKEN, case['case'], case['scenario'])).encode('ascii')
    with open(CIDATA + '/meta-data', 'rb') as stream:
        if stream.read(1025) != expected_meta:
            raise JournalError('guest metadata binding differs')
    with open(CIDATA + '/network-config', 'rb') as stream:
        if stream.read(1025) != b'version: 2\nethernets: {}\n':
            raise JournalError('guest network configuration differs')
    case.update(host_launch_sha256=descriptor['host_launch_sha256'], host_argv_sha256=descriptor['host_argv_sha256'],
                payload_case_sha256=descriptor['payload_case_sha256'],
                seed_descriptor_sha256=hashlib.sha256(descriptor_raw).hexdigest())
    with open(SEED + '/supervisor-manifest.json', 'rb') as stream:
        manifest = strict_json(stream.read(65537))
    if (type(manifest) is not dict or set(manifest) != {'version', 'run_token', 'files'} or
            type(manifest['version']) is not int or manifest['version'] != 1 or manifest['run_token'] != RUN_TOKEN or
            manifest['files'] != {name: expected[name] for name in SUPERVISOR}):
        raise JournalError('supervisor package manifest differs')
    return case, expected

def _verify_boot_inputs(case, expected):
    inputs, bootstrap = case['boot_inputs'], case['bpf_bootstrap']
    input_keys = {'version', 'base_sha256', 'kernel_release', 'kernel_path', 'kernel_bytes', 'kernel_sha256',
                  'original_initrd_path', 'original_initrd_sha256', 'initrd_path', 'initrd_bytes',
                  'initrd_sha256', 'bootstrap_manifest_sha256', 'observer_bindings_sha256', 'command_line'}
    bootstrap_keys = {'version', 'kernel_release', 'source_inputs_sha256', 'kernel_sha256',
                      'original_initrd_sha256', 'broker_sha256', 'guard_sha256', 'policy_sha256',
                      'generator_sha256', 'observer_bindings_sha256', 'scripts', 'package_executables'}
    def digest(value):
        return type(value) is str and re.fullmatch(r'[0-9a-f]{64}', value) is not None and value != '0' * 64
    if (type(inputs) is not dict or set(inputs) != input_keys or type(inputs['version']) is not int or
            inputs['version'] != 1 or type(bootstrap) is not dict or set(bootstrap) != bootstrap_keys or
            type(bootstrap['version']) is not int or bootstrap['version'] != 1 or
            any(not digest(value) for key, value in inputs.items() if key.endswith('_sha256')) or
            any(not digest(value) for key, value in bootstrap.items() if key.endswith('_sha256')) or
            inputs['base_sha256'] != '04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94' or
            inputs['kernel_release'] != bootstrap['kernel_release'] or inputs['kernel_release'] != os.uname().release or
            inputs['kernel_release'] != '6.8.0-147-generic' or
            inputs['kernel_sha256'] != bootstrap['kernel_sha256'] or
            inputs['original_initrd_sha256'] != bootstrap['original_initrd_sha256'] or
            inputs['observer_bindings_sha256'] != bootstrap['observer_bindings_sha256'] or
            inputs['kernel_path'] != 'boot/vmlinuz' or inputs['initrd_path'] != 'boot/initrd' or
            inputs['original_initrd_path'] != 'boot/initrd.original' or
            any(type(inputs[key]) is not int or not 0 < inputs[key] <= 1024 ** 3
                for key in ('kernel_bytes', 'initrd_bytes')) or
            hashlib.sha256(canonical(bootstrap)).hexdigest() != inputs['bootstrap_manifest_sha256'] or
            bootstrap['broker_sha256'] != expected['g2-bpf-authority'] or
            bootstrap['scripts'] != {'source_guest': expected['source_guest.py'],
                                     'reboot_observer': expected['reboot_observer.py']}):
        raise JournalError('direct boot or bootstrap manifest binding differs')
    command_line = ('root=LABEL=cloudimg-rootfs ro console=tty1 console=ttyS0 '
                    'lsm=landlock,lockdown,yama,integrity,apparmor,bpf rdinit=/g2-early-init')
    with open('/proc/cmdline', 'rb') as stream:
        actual = stream.read(4097)
    if len(actual) > 4096 or inputs['command_line'] != command_line or actual.strip() != command_line.encode('ascii'):
        raise JournalError('live direct boot command line differs')
    programs = bootstrap['package_executables']
    if type(programs) is not dict or set(programs) != {'python', 'systemd', 'runc', 'dockerd', 'containerd', 'shim'}:
        raise JournalError('bootstrap package executable set differs')
    for item in programs.values():
        if (type(item) is not dict or set(item) != {'package', 'version', 'path', 'sha256'} or
                any(type(item[key]) is not str or not item[key] for key in ('package', 'version', 'path')) or
                not item['path'].startswith('/') or '..' in item['path'].split('/') or not digest(item['sha256'])):
            raise JournalError('bootstrap package executable invalid')
    if programs['systemd']['package'] != 'systemd' or programs['systemd']['path'] != '/usr/lib/systemd/systemd':
        raise JournalError('bootstrap systemd package binding differs')



def _install_file(source, destination, expected, mode, group):
    source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    target_fd = None
    h = hashlib.sha256()
    try:
        target_fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        while True:
            data = os.read(source_fd, 1024 * 1024)
            if not data:
                break
            h.update(data)
            view = memoryview(data)
            while view:
                size = os.write(target_fd, view)
                if size <= 0:
                    raise JournalError('installation write failed')
                view = view[size:]
        if h.hexdigest() != expected:
            raise JournalError('installed program digest differs')
        os.fchown(target_fd, 0, group)
        os.fchmod(target_fd, mode)
        os.fsync(target_fd)
    finally:
        os.close(source_fd)
        if target_fd is not None:
            os.close(target_fd)
    if _digest(destination) != expected:
        raise JournalError('installed program readback differs')


def _install_managed_containerd(expected):
    directory = '/opt/g2/containerd-bin'
    os.mkdir(directory, 0o700)
    launcher = directory + '/containerd'
    _install_file(SEED + '/g2-bpf-authority', launcher, expected, 0o500, 0)
    identities = set()
    for path in ('/opt/g2/g2-bpf-authority', '/opt/g2/g2-bpf-runtime', launcher):
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                    stat.S_IMODE(info.st_mode) != 0o500 or info.st_nlink != 1 or _digest(path) != expected):
                raise JournalError('managed containerd executable differs')
            identities.add((info.st_dev, info.st_ino))
        finally:
            os.close(fd)
    if len(identities) != 3:
        raise JournalError('managed containerd executable identity differs')
    os.chmod(directory, 0o500)
    for path in (directory, '/opt/g2'):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


# Private owned-VM serial diagnostics, not part of the guest result protocol.
# All reads happen only after the existing launch execve has failed with EACCES.
_LAUNCH_FAILURE_PREFIX = b'G2_LAUNCH_EXEC_FAILURE '
_LAUNCH_FAILURE_LIMIT = 16 * 1024
_LAUNCH_SAFE_PATHS = frozenset(('/', '/opt', '/opt/g2-launch'))
_LAUNCH_MOUNT_OPTIONS = frozenset(('rw', 'ro', 'suid', 'nosuid', 'dev', 'nodev',
                                   'exec', 'noexec', 'sync', 'async', 'dirsync',
                                   'atime', 'noatime', 'diratime', 'nodiratime',
                                   'relatime', 'strictatime', 'lazytime',
                                   'symfollow', 'nosymfollow'))
_LAUNCH_FS_TYPES = frozenset(('tmpfs', 'devtmpfs', 'ramfs', 'rootfs', 'overlay',
                              'ext2', 'ext3', 'ext4', 'xfs', 'btrfs', 'f2fs',
                              'squashfs', 'vfat', 'zfs', 'nfs', 'nfs4', '9p',
                              'virtiofs', 'fuse', 'proc', 'sysfs', 'cgroup',
                              'cgroup2', 'devpts', 'nsfs', 'securityfs'))


def _launch_failure_error(exc):
    return {'status': 'unavailable', 'errno': exc.errno if isinstance(exc, OSError) else None}


def _launch_failure_read(path, limit):
    """Read a bounded proc file; never return a partial observation as complete."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        try:
            data = bytearray()
            while len(data) <= limit:
                chunk = os.read(fd, min(8192, limit + 1 - len(data)))
                if not chunk:
                    return bytes(data), {'status': 'ok'}
                data.extend(chunk)
            return None, {'status': 'over_limit', 'limit': limit}
        finally:
            os.close(fd)
    except OSError as exc:
        return None, _launch_failure_error(exc)


def _launch_failure_uint(value):
    if re.fullmatch(rb'[0-9]{1,20}', value) is None:
        return None
    number = int(value)
    return number if number <= (1 << 64) - 1 else None


def _launch_failure_mount_id(fd):
    data, status = _launch_failure_read('/proc/self/fdinfo/%d' % fd, 8192)
    if data is None:
        return status
    if not data.endswith(b'\n'):
        return {'status': 'invalid'}
    values = [line.split(b':', 1)[1].strip() for line in data.splitlines()
              if line.startswith(b'mnt_id:')]
    if len(values) != 1 or _launch_failure_uint(values[0]) is None:
        return {'status': 'missing' if not values else 'invalid'}
    return {'status': 'ok', 'value': _launch_failure_uint(values[0])}


def _launch_failure_mount(mount_id):
    data, status = _launch_failure_read('/proc/self/mountinfo', 1024 * 1024)
    if data is None:
        return status
    if data and not data.endswith(b'\n'):
        return {'status': 'invalid'}
    selected = []
    for line in data.splitlines():
        before, separator, after = line.partition(b' - ')
        left = before.split()
        if left and _launch_failure_uint(left[0]) == mount_id:
            selected.append((left, separator, after.split()))
    if not selected:
        return {'status': 'not_found'}
    if len(selected) != 1:
        return {'status': 'invalid'}
    left, separator, right = selected[0]
    if (not separator or len(left) < 6 or len(right) < 3 or
            _launch_failure_uint(left[1]) is None or
            re.fullmatch(rb'[0-9]{1,10}:[0-9]{1,10}', left[2]) is None or
            re.fullmatch(rb'[A-Za-z0-9_.+-]{1,64}', right[0]) is None):
        return {'status': 'invalid'}

    # A bind mount root, mountpoint, or option can contain private path/credential
    # material. Publish only known fixed paths and known non-value options.
    def safe_path(raw):
        value = raw.decode('ascii', 'replace')
        return {'status': 'ok', 'value': value} if value in _LAUNCH_SAFE_PATHS else {'status': 'redacted'}

    options = left[5].decode('ascii', 'replace').split(',')
    allowed = sorted({option for option in options if option in _LAUNCH_MOUNT_OPTIONS})
    fs_type = right[0].decode('ascii')
    safe_type = ({'status': 'ok', 'value': fs_type} if fs_type in _LAUNCH_FS_TYPES
                 else {'status': 'redacted'})
    return {'status': 'ok', 'id': mount_id, 'parent_id': _launch_failure_uint(left[1]),
            'dev': left[2].decode('ascii'), 'root': safe_path(left[3]),
            'mountpoint': safe_path(left[4]),
            'mount_options': {'status': 'partial' if any(option not in _LAUNCH_MOUNT_OPTIONS
                                                           for option in options) else 'ok',
                              'values': allowed},
            'fs_type': safe_type}


def _launch_failure_status():
    data, status = _launch_failure_read('/proc/self/status', 65536)
    if data is None:
        return status
    if data and not data.endswith(b'\n'):
        return {'status': 'invalid'}
    names = ('Uid', 'Gid', 'Groups', 'CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb',
             'NoNewPrivs', 'Seccomp', 'Seccomp_filters')
    fields = {name: [] for name in names}
    for line in data.splitlines():
        key, separator, value = line.partition(b':')
        for name in names:
            if separator and key == name.encode('ascii'):
                fields[name].append(value.strip())
                break
    output = {}
    for name, values in fields.items():
        if len(values) != 1:
            output[name] = {'status': 'missing' if not values else 'invalid'}
            continue
        value = values[0]
        if name.startswith('Cap'):
            output[name] = ({'status': 'ok', 'value': value.decode('ascii').lower()}
                            if re.fullmatch(rb'[0-9a-fA-F]{1,16}', value) else {'status': 'invalid'})
            continue
        parts = value.split()
        if name == 'Groups' and len(parts) > 128:
            output[name] = {'status': 'over_limit', 'limit': 128}
            continue
        numbers = [_launch_failure_uint(part) for part in parts]
        valid = all(number is not None and number <= 0xffffffff for number in numbers)
        if name in ('Uid', 'Gid'):
            valid = valid and len(numbers) == 4
        elif name != 'Groups':
            valid = valid and len(numbers) == 1
            if valid and name == 'NoNewPrivs':
                valid = numbers[0] in (0, 1)
            if valid and name == 'Seccomp':
                valid = numbers[0] in (0, 1, 2)
        if not valid:
            output[name] = {'status': 'invalid'}
        else:
            output[name] = {'status': 'ok', 'value': numbers if name in ('Uid', 'Gid', 'Groups') else numbers[0]}
    return {'status': 'ok', 'fields': output}


def _launch_failure_lsm():
    data, status = _launch_failure_read('/proc/self/attr/current', 512)
    if data is None:
        return status
    value = data.removesuffix(b'\n')
    # Do not publish path-shaped labels, control characters, or credential-like
    # labels, even on the private serial channel.
    if (re.fullmatch(rb'[A-Za-z0-9_.:, +()\-]{1,512}', value) is None or
            re.search(rb'password|token|secret|credential|authorization|bearer|verifier', value, re.I)):
        return {'status': 'redacted' if value else 'invalid'}
    return {'status': 'ok', 'value': value.decode('ascii')}


def _launch_failure_stat(info):
    return {'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode,
            'uid': info.st_uid, 'gid': info.st_gid, 'nlink': info.st_nlink,
            'size': info.st_size, 'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def _launch_failure_collect(path, before):
    result = {'pre_exec_stat': _launch_failure_stat(before)}
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    except OSError as exc:
        result['file'] = _launch_failure_error(exc)
    else:
        try:
            file_result = {'status': 'ok'}
            result['file'] = file_result
            try:
                identity = _launch_failure_stat(os.fstat(fd))
                file_result['stat'] = {'status': 'ok', 'value': identity,
                                       'same_as_pre_exec': identity == result['pre_exec_stat']}
            except OSError as exc:
                file_result['stat'] = _launch_failure_error(exc)
            try:
                flags = os.fstatvfs(fd).f_flag
                mask = getattr(os, 'ST_NOEXEC', None)
                file_result['vfs'] = {'status': 'ok', 'flags': flags,
                                      'noexec': ({'status': 'ok', 'mask': mask, 'value': bool(flags & mask)}
                                                 if mask is not None else {'status': 'unavailable', 'errno': None})}
            except OSError as exc:
                file_result['vfs'] = _launch_failure_error(exc)
            mount_id = _launch_failure_mount_id(fd)
            file_result['fd_mount_id'] = mount_id
            file_result['mount'] = (_launch_failure_mount(mount_id['value']) if mount_id['status'] == 'ok'
                                    else {'status': 'unavailable', 'errno': None})
        finally:
            os.close(fd)
    result['namespaces'] = {}
    for name in ('mnt', 'user'):
        try:
            info = os.stat('/proc/self/ns/' + name)
            result['namespaces'][name] = {'status': 'ok', 'dev': info.st_dev, 'ino': info.st_ino}
        except OSError as exc:
            result['namespaces'][name] = _launch_failure_error(exc)
    result['process_status'] = _launch_failure_status()
    result['lsm_current'] = _launch_failure_lsm()
    if path == '/opt/g2-launch/g2-bpf-authority':
        result['ancestors'] = []
        for ancestor in ('/', '/opt', '/opt/g2-launch'):
            try:
                info = os.stat(ancestor, follow_symlinks=False)
                result['ancestors'].append({'path': ancestor, 'status': 'ok', 'mode': info.st_mode,
                                            'uid': info.st_uid, 'gid': info.st_gid})
            except OSError as exc:
                result['ancestors'].append({'path': ancestor, **_launch_failure_error(exc)})
    return result


def _emit_launch_exec_failure(path, exc, before):
    record = {'path': path, 'pid': os.getpid(), 'errno': exc.errno,
              'monotonic_ns': time.monotonic_ns(), 'status': 'captured'}
    try:
        record['context'] = _launch_failure_collect(path, before)
        record['capture_finished_monotonic_ns'] = time.monotonic_ns()
    except BaseException as secondary:
        record['status'] = 'unavailable'
        record['context'] = _launch_failure_error(secondary)
    encoded = canonical(record)
    if len(_LAUNCH_FAILURE_PREFIX) + len(encoded) + 1 > _LAUNCH_FAILURE_LIMIT:
        record.pop('context', None)
        record['status'] = 'over_limit'
        encoded = canonical(record)
    output = _LAUNCH_FAILURE_PREFIX + encoded + b'\n'
    while output:
        written = os.write(2, output)
        if written <= 0:
            break
        output = output[written:]


def _source_directory_identity(fd, private=False, *, preserve_errno=False):
    info = os.fstat(fd)
    mount = _launch_failure_mount_id(fd)
    if preserve_errno and mount['status'] == 'unavailable' and mount.get('errno') is not None:
        raise OSError(mount['errno'], os.strerror(mount['errno']))
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
            info.st_mode & 0o022 or (private and stat.S_IMODE(info.st_mode) != 0o700) or
            mount['status'] != 'ok'):
        raise JournalError('source launcher directory is not trusted')
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, mount['value'])


def _source_check_named_directory(path, held, identity, private=False, *, preserve_errno=False):
    named = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        if (_source_directory_identity(held, private, preserve_errno=preserve_errno) != identity or
                _source_directory_identity(named, private, preserve_errno=preserve_errno) != identity):
            raise JournalError('source launcher directory identity changed')
    finally:
        os.close(named)


def _owner_probe_source_identity(fd):
    info = os.fstat(fd)
    mount = _launch_failure_mount_id(fd)
    if mount['status'] == 'unavailable' and mount.get('errno') is not None:
        raise OSError(mount['errno'], os.strerror(mount['errno']))
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
            stat.S_IMODE(info.st_mode) != 0o500 or mount['status'] != 'ok'):
        raise JournalError('PostgreSQL owner probe source differs')
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, mount['value'],
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class _OwnerProbeSource:
    _flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    _name = 'postgres-owner-probe'

    def __init__(self):
        self._run = None
        self._parent = None
        self._leaf = None
        self._removed = False
        try:
            if os.geteuid() != 0 or os.getegid() != 0:
                raise JournalError('PostgreSQL owner probe source differs')
            self._run = os.open('/run', self._flags)
            self._run_identity = _source_directory_identity(self._run, preserve_errno=True)
            self._parent = os.open('g2', self._flags, dir_fd=self._run)
            self._parent_identity = _source_directory_identity(self._parent, preserve_errno=True)
            self._check_parents()
            os.mkdir(self._name, 0o500, dir_fd=self._parent)
            os.fsync(self._parent)
            self._leaf = os.open(self._name, self._flags, dir_fd=self._parent)
            self._identity = _owner_probe_source_identity(self._leaf)
            if self._identity[5] != self._parent_identity[5]:
                raise JournalError('PostgreSQL owner probe source differs')
            self.check()
        except JournalError:
            self.close()
            raise JournalError('PostgreSQL owner probe source differs') from None
        except BaseException:
            self.close()
            raise

    def _check_parents(self):
        try:
            _source_check_named_directory('/run', self._run, self._run_identity, preserve_errno=True)
            _source_check_named_directory('/run/g2', self._parent, self._parent_identity, preserve_errno=True)
        except JournalError:
            raise JournalError('PostgreSQL owner probe source differs') from None

    def _check_leaf(self):
        named = os.open(self._name, self._flags, dir_fd=self._parent)
        try:
            if (_owner_probe_source_identity(self._leaf) != self._identity or
                    _owner_probe_source_identity(named) != self._identity):
                raise JournalError('PostgreSQL owner probe source differs')
        finally:
            os.close(named)

    def check(self):
        if self._removed or self._leaf is None:
            raise JournalError('PostgreSQL owner probe source differs')
        self._check_parents()
        self._check_leaf()
        scan = os.open('.', self._flags, dir_fd=self._leaf)
        try:
            if _owner_probe_source_identity(scan) != self._identity:
                raise JournalError('PostgreSQL owner probe source differs')
            entries = os.listdir(scan)
            if _owner_probe_source_identity(scan) != self._identity or entries:
                raise JournalError('PostgreSQL owner probe source differs')
        finally:
            os.close(scan)
        self._check_leaf()
        self._check_parents()

    def remove(self):
        self.check()
        os.rmdir(self._name, dir_fd=self._parent)
        self._removed = True
        os.fsync(self._parent)

    def close(self):
        for name in ('_leaf', '_parent', '_run'):
            fd = getattr(self, name)
            if fd is not None:
                setattr(self, name, None)
                os.close(fd)


_OWNER_WRAPPER_STAGES = (
    'WRAPPER_ENTRY', 'WRAPPER_PROC', 'WRAPPER_DEADLINE', 'WRAPPER_IDENTITY',
    'WRAPPER_ARGS', 'WRAPPER_RUNC_OPEN', 'WRAPPER_RUNC_IDENTITY', 'WRAPPER_BOOT_ID',
    'WRAPPER_INPUT_DIR', 'WRAPPER_BUNDLE_PREPARE', 'WRAPPER_INPUT_FILE', 'WRAPPER_INPUT_STAT',
    'WRAPPER_INPUT_BEFORE_BIND', 'WRAPPER_CONNECT', 'WRAPPER_LISTENER', 'WRAPPER_BIND',
    'WRAPPER_INPUT_AFTER_BIND', 'WRAPPER_RUNC_REOPEN', 'WRAPPER_RUNC_RECHECK',
    'WRAPPER_RUNC_CHECK_CLOSE', 'WRAPPER_LISTENER_CLOSE', 'WRAPPER_CONTROL_CLOSE',
    'WRAPPER_PRE_EXEC_LIVE', 'WRAPPER_EXEC_RETURN')
_OWNER_PREPARE_STAGES = (
    'WRAPPER_PREPARE_FILE_NAME',
    'WRAPPER_PREPARE_FILE_OPEN',
    'WRAPPER_PREPARE_FILE_NEW_FILE',
    'WRAPPER_PREPARE_FILE_STAT',
    'WRAPPER_PREPARE_FILE_TYPE',
    'WRAPPER_PREPARE_FILE_UID',
    'WRAPPER_PREPARE_FILE_GID',
    'WRAPPER_PREPARE_FILE_WRITE_MODE',
    'WRAPPER_PREPARE_FILE_LINKS',
    'WRAPPER_PREPARE_FILE_SIZE_LOW',
    'WRAPPER_PREPARE_FILE_SIZE_HIGH',
    'WRAPPER_PREPARE_FILE_READ',
    'WRAPPER_PREPARE_FILE_COUNT',
    'WRAPPER_PREPARE_FILE_HELD_STAT',
    'WRAPPER_PREPARE_FILE_NAMED_STAT',
    'WRAPPER_PREPARE_FILE_HELD_DEV',
    'WRAPPER_PREPARE_FILE_HELD_INO',
    'WRAPPER_PREPARE_FILE_HELD_MODE',
    'WRAPPER_PREPARE_FILE_HELD_UID',
    'WRAPPER_PREPARE_FILE_HELD_GID',
    'WRAPPER_PREPARE_FILE_HELD_NLINK',
    'WRAPPER_PREPARE_FILE_HELD_SIZE',
    'WRAPPER_PREPARE_FILE_HELD_MTIME',
    'WRAPPER_PREPARE_FILE_HELD_CTIME',
    'WRAPPER_PREPARE_FILE_NAMED_DEV',
    'WRAPPER_PREPARE_FILE_NAMED_INO',
    'WRAPPER_PREPARE_FILE_NAMED_MODE',
    'WRAPPER_PREPARE_FILE_NAMED_UID',
    'WRAPPER_PREPARE_FILE_NAMED_GID',
    'WRAPPER_PREPARE_FILE_NAMED_NLINK',
    'WRAPPER_PREPARE_FILE_NAMED_SIZE',
    'WRAPPER_PREPARE_FILE_NAMED_MTIME',
    'WRAPPER_PREPARE_FILE_NAMED_CTIME',
    'WRAPPER_PREPARE_FILE_SEEK',
    'WRAPPER_PREPARE_HELD_STAT',
    'WRAPPER_PREPARE_DIGEST_FD',
    'WRAPPER_PREPARE_DIGEST_STAT',
    'WRAPPER_PREPARE_DIGEST_TYPE',
    'WRAPPER_PREPARE_DIGEST_UID',
    'WRAPPER_PREPARE_DIGEST_GID',
    'WRAPPER_PREPARE_DIGEST_WRITE_MODE',
    'WRAPPER_PREPARE_DIGEST_SIZE_LOW',
    'WRAPPER_PREPARE_DIGEST_SIZE_HIGH',
    'WRAPPER_PREPARE_DIGEST_READ',
    'WRAPPER_PREPARE_DIGEST_COUNT',
    'WRAPPER_PREPARE_DIGEST_AFTER_STAT',
    'WRAPPER_PREPARE_DIGEST_AFTER_DEV',
    'WRAPPER_PREPARE_DIGEST_AFTER_INO',
    'WRAPPER_PREPARE_DIGEST_AFTER_MODE',
    'WRAPPER_PREPARE_DIGEST_AFTER_UID',
    'WRAPPER_PREPARE_DIGEST_AFTER_GID',
    'WRAPPER_PREPARE_DIGEST_AFTER_NLINK',
    'WRAPPER_PREPARE_DIGEST_AFTER_SIZE',
    'WRAPPER_PREPARE_DIGEST_AFTER_MTIME',
    'WRAPPER_PREPARE_DIGEST_AFTER_CTIME',
    'WRAPPER_PREPARE_READ_DATA_READ',
    'WRAPPER_PREPARE_READ_DATA_COUNT',
    'WRAPPER_PREPARE_READ_DATA_STAT',
    'WRAPPER_PREPARE_READ_DATA_AFTER_DEV',
    'WRAPPER_PREPARE_READ_DATA_AFTER_INO',
    'WRAPPER_PREPARE_READ_DATA_AFTER_MODE',
    'WRAPPER_PREPARE_READ_DATA_AFTER_UID',
    'WRAPPER_PREPARE_READ_DATA_AFTER_GID',
    'WRAPPER_PREPARE_READ_DATA_AFTER_NLINK',
    'WRAPPER_PREPARE_READ_DATA_AFTER_SIZE',
    'WRAPPER_PREPARE_READ_DATA_AFTER_MTIME',
    'WRAPPER_PREPARE_READ_DATA_AFTER_CTIME',
    'WRAPPER_PREPARE_HASH',
    'WRAPPER_PREPARE_JSON',
    'WRAPPER_PREPARE_PROCESS',
    'WRAPPER_PREPARE_SELFTEST')
_OWNER_WRAPPER_STAGE_NAMES = _OWNER_WRAPPER_STAGES + _OWNER_PREPARE_STAGES
_OWNER_BROKER_STAGES = (
    'BROKER_CREATE_POLICY', 'BROKER_BUNDLE_INPUT', 'BROKER_BUNDLE_DIR', 'BROKER_BUNDLE_CONFIG',
    'BROKER_BUNDLE_JSON', 'BROKER_BUNDLE_MATERIALIZE', 'BROKER_BUNDLE_EXPECTED',
    'BROKER_BUNDLE_COMPARE', 'BROKER_BUNDLE_DEVICE_RULES', 'BROKER_BUNDLE_IDENTITIES',
    'BROKER_BUNDLE_USER_FILES', 'BROKER_BUNDLE_RECHECK', 'BROKER_CREATE_FINAL')
_OWNER_HANDLER_REJECTION_STAGES = (
    'BROKER_HANDLER_REJECT_MAP_CREATE', 'BROKER_HANDLER_REJECT_PROG_LOAD',
    'BROKER_HANDLER_REJECT_INFO', 'BROKER_HANDLER_REJECT_GET_ID',
    'BROKER_HANDLER_REJECT_QUERY', 'BROKER_HANDLER_REJECT_ATTACH',
    'BROKER_HANDLER_REJECT_DETACH')
_OWNER_OCI_STAGES = (
    'BROKER_OCI_VERSION', 'BROKER_OCI_PROCESS', 'BROKER_OCI_PROCESS_TERMINAL',
    'BROKER_OCI_PROCESS_CONSOLE_SIZE', 'BROKER_OCI_PROCESS_USER', 'BROKER_OCI_PROCESS_USER_UID',
    'BROKER_OCI_PROCESS_USER_GID', 'BROKER_OCI_PROCESS_USER_UMASK', 'BROKER_OCI_PROCESS_USER_ADDITIONAL_GIDS',
    'BROKER_OCI_PROCESS_USER_USERNAME', 'BROKER_OCI_PROCESS_ARGS', 'BROKER_OCI_PROCESS_COMMAND_LINE',
    'BROKER_OCI_PROCESS_ENV', 'BROKER_OCI_PROCESS_CWD', 'BROKER_OCI_PROCESS_CAPABILITIES',
    'BROKER_OCI_PROCESS_CAP_BOUNDING', 'BROKER_OCI_PROCESS_CAP_EFFECTIVE', 'BROKER_OCI_PROCESS_CAP_INHERITABLE',
    'BROKER_OCI_PROCESS_CAP_PERMITTED', 'BROKER_OCI_PROCESS_CAP_AMBIENT', 'BROKER_OCI_PROCESS_RLIMITS',
    'BROKER_OCI_PROCESS_NO_NEW_PRIVILEGES', 'BROKER_OCI_PROCESS_APPARMOR', 'BROKER_OCI_PROCESS_OOM_SCORE',
    'BROKER_OCI_PROCESS_SCHEDULER', 'BROKER_OCI_PROCESS_SELINUX', 'BROKER_OCI_PROCESS_IO_PRIORITY',
    'BROKER_OCI_PROCESS_EXEC_CPU_AFFINITY', 'BROKER_OCI_ROOT', 'BROKER_OCI_ROOT_PATH',
    'BROKER_OCI_ROOT_READONLY', 'BROKER_OCI_HOSTNAME', 'BROKER_OCI_DOMAINNAME', 'BROKER_OCI_MOUNTS',
    'BROKER_OCI_MOUNT_DESTINATION', 'BROKER_OCI_MOUNT_TYPE', 'BROKER_OCI_MOUNT_SOURCE',
    'BROKER_OCI_MOUNT_OPTIONS', 'BROKER_OCI_MOUNT_UID_MAPPINGS', 'BROKER_OCI_MOUNT_GID_MAPPINGS',
    'BROKER_OCI_HOOKS', 'BROKER_OCI_ANNOTATIONS', 'BROKER_OCI_LINUX', 'BROKER_OCI_LINUX_UID_MAPPINGS',
    'BROKER_OCI_LINUX_GID_MAPPINGS', 'BROKER_OCI_LINUX_SYSCTL', 'BROKER_OCI_LINUX_RESOURCES',
    'BROKER_OCI_RESOURCES_DEVICES', 'BROKER_OCI_RESOURCE_DEVICE_ALLOW', 'BROKER_OCI_RESOURCE_DEVICE_TYPE',
    'BROKER_OCI_RESOURCE_DEVICE_MAJOR', 'BROKER_OCI_RESOURCE_DEVICE_MINOR', 'BROKER_OCI_RESOURCE_DEVICE_ACCESS',
    'BROKER_OCI_RESOURCES_MEMORY', 'BROKER_OCI_MEMORY_LIMIT', 'BROKER_OCI_MEMORY_SWAP',
    'BROKER_OCI_RESOURCES_CPU', 'BROKER_OCI_CPU_QUOTA', 'BROKER_OCI_CPU_PERIOD', 'BROKER_OCI_RESOURCES_PIDS',
    'BROKER_OCI_PIDS_LIMIT', 'BROKER_OCI_RESOURCES_BLOCK_IO', 'BROKER_OCI_RESOURCES_HUGEPAGE_LIMITS',
    'BROKER_OCI_RESOURCES_NETWORK', 'BROKER_OCI_RESOURCES_RDMA', 'BROKER_OCI_RESOURCES_UNIFIED',
    'BROKER_OCI_LINUX_CGROUPS_PATH', 'BROKER_OCI_LINUX_NAMESPACES', 'BROKER_OCI_NAMESPACE_TYPE',
    'BROKER_OCI_NAMESPACE_PATH', 'BROKER_OCI_LINUX_DEVICES', 'BROKER_OCI_LINUX_NET_DEVICES',
    'BROKER_OCI_LINUX_SECCOMP', 'BROKER_OCI_SECCOMP_DEFAULT_ACTION', 'BROKER_OCI_SECCOMP_DEFAULT_ERRNO',
    'BROKER_OCI_SECCOMP_ARCHITECTURES', 'BROKER_OCI_SECCOMP_FLAGS', 'BROKER_OCI_SECCOMP_LISTENER_PATH',
    'BROKER_OCI_SECCOMP_LISTENER_METADATA', 'BROKER_OCI_SECCOMP_SYSCALLS', 'BROKER_OCI_SYSCALL_NAMES',
    'BROKER_OCI_SYSCALL_ACTION', 'BROKER_OCI_SYSCALL_ERRNO', 'BROKER_OCI_SYSCALL_ARGS',
    'BROKER_OCI_LINUX_ROOTFS_PROPAGATION', 'BROKER_OCI_LINUX_MASKED_PATHS', 'BROKER_OCI_LINUX_READONLY_PATHS',
    'BROKER_OCI_LINUX_MOUNT_LABEL', 'BROKER_OCI_OTHER', 'BROKER_OCI_LIMIT', 'BROKER_OCI_DEPTH')
_OWNER_BROKER_STAGE_NAMES = _OWNER_BROKER_STAGES + _OWNER_HANDLER_REJECTION_STAGES + _OWNER_OCI_STAGES

_OWNER_MOUNT_NAMES = ('PROC', 'DEV', 'DEV_PTS', 'SYS', 'SYS_CGROUP', 'DEV_MQUEUE',
                      'DEV_SHM', 'HOSTNAME', 'HOSTS', 'RESOLV', 'PGDATA', 'OTHER')
_OWNER_OPTION_SIGNATURE_NAMES = ('MISSING', 'NULL', 'NIL_ARRAY', 'OTHER_TYPE', 'EMPTY',
                                 'NOSUID_NOEXEC_NODEV', 'DEV_TMPFS', 'DEVPTS', 'SYSFS_RO',
                                 'CGROUP_RO', 'SHM_64M', 'RBIND_RO_RPRIVATE',
                                 'RBIND_RRO_RPRIVATE', 'RBIND_RPRIVATE', 'OTHER_STRING_ARRAY',
                                 'NON_STRING_ARRAY', 'LIMIT')
_OWNER_OPTION_DIFFERENCE_NAMES = ('MISSING', 'TYPE', 'NIL_ARRAY', 'LENGTH', 'ORDER',
                                  'VALUE', 'NON_STRING', 'LIMIT', 'OTHER')


def _owner_probe_mount_options_valid(value):
    if value is None:
        return True
    if type(value) is not dict or set(value) != {'mount', 'actual', 'expected', 'difference'}:
        return False
    return all(type(value[key]) is str and value[key] in names for key, names in (
        ('mount', _OWNER_MOUNT_NAMES), ('actual', _OWNER_OPTION_SIGNATURE_NAMES),
        ('expected', _OWNER_OPTION_SIGNATURE_NAMES), ('difference', _OWNER_OPTION_DIFFERENCE_NAMES)))



_OWNER_HISTORY_COMMANDS = ('MAP_CREATE', 'PROG_LOAD', 'OBJ_GET_INFO_BY_FD',
                           'PROG_GET_FD_BY_ID', 'PROG_QUERY', 'PROG_ATTACH', 'PROG_DETACH')
_OWNER_HISTORY_GROUPS = ('ABSENT', 'LIVE', 'RETIRED', 'INVALID')
_OWNER_HISTORY_TARGETS = ('UNAVAILABLE', 'ABSENT', 'PHASE0', 'PHASE1', 'PHASE2',
                          'PHASE3', 'PHASE4', 'OTHER_PHASE', 'IDENTITY_MISMATCH')
_OWNER_HISTORY_STATES = ('PREPARED', 'COMMITTED', 'EFFECT_OBSERVED', 'RESPONSE_SETTLED', 'RETIRED')
_OWNER_HISTORY_RESULTS = ('UNRECORDED', 'NEGATIVE', 'ZERO', 'POSITIVE')
_OWNER_HISTORY_PROGRAMS = ('DEVICE', 'EXTENDED_INFO_PROBE', 'REPLACE_PROBE')
_OWNER_HISTORY_RESPONSES = ('UNRECORDED', 'SEND_OK', 'SEND_ENOENT', 'ADDFD_OK', 'ADDFD_ENOENT')


def _owner_probe_create_history_value(value, purpose):
    if type(value) is not dict or set(value) != {'version', 'purpose', 'create_exec_observed', 'group', 'target', 'program_kind', 'commands'}:
        return None
    if (type(value['version']) is not int or value['version'] != 2 or
            type(value['purpose']) is not str or value['purpose'] not in ('postgres-uid', 'postgres-gid') or
            value['purpose'] != purpose or type(value['create_exec_observed']) is not bool or
            type(value['group']) is not str or value['group'] not in _OWNER_HISTORY_GROUPS or
            type(value['target']) is not str or value['target'] not in _OWNER_HISTORY_TARGETS or
            (value['group'] in ('ABSENT', 'INVALID') and value['target'] != 'UNAVAILABLE') or
            type(value['commands']) is not list or len(value['commands']) != 7):
        return None
    commands = []
    total = 0
    for name, item in zip(_OWNER_HISTORY_COMMANDS, value['commands']):
        if (type(item) is not dict or set(item) != {'command', 'count', 'state', 'result', 'errno', 'policy_error', 'response'} or
                type(item['command']) is not str or item['command'] != name or
                type(item['count']) is not int or not 0 <= item['count'] <= 4096 or
                type(item['policy_error']) is not bool):
            return None
        count, state, result, number = item['count'], item['state'], item['result'], item['errno']
        total += count
        if total > 4096:
            return None
        if count == 0:
            if state is not None or result is not None or number is not None or item['policy_error'] or item['response'] is not None:
                return None
        elif (type(state) is not str or state not in _OWNER_HISTORY_STATES or
              type(result) is not str or result not in _OWNER_HISTORY_RESULTS or
              (result == 'NEGATIVE' and (type(number) is not int or not 1 <= number <= 4095)) or
              (result != 'NEGATIVE' and number is not None) or
              (state in ('PREPARED', 'COMMITTED') and result != 'UNRECORDED') or
              (state in ('EFFECT_OBSERVED', 'RESPONSE_SETTLED') and result == 'UNRECORDED') or
              type(item['response']) is not str or item['response'] not in _OWNER_HISTORY_RESPONSES or
              (state in ('PREPARED', 'COMMITTED') and item['response'] != 'UNRECORDED') or
              (state == 'RETIRED' and result == 'UNRECORDED' and item['response'] != 'UNRECORDED') or
              (state in ('RESPONSE_SETTLED', 'RETIRED') and result != 'UNRECORDED' and item['response'] == 'UNRECORDED') or
              (item['response'] in ('ADDFD_OK', 'ADDFD_ENOENT') and
               (name not in ('MAP_CREATE', 'PROG_LOAD', 'PROG_GET_FD_BY_ID') or result not in ('ZERO', 'POSITIVE')))):
            return None
        commands.append(dict(item))
    kind = value['program_kind']
    if ((commands[1]['count'] == 0 and kind is not None) or
            (commands[1]['count'] != 0 and (type(kind) is not str or kind not in _OWNER_HISTORY_PROGRAMS))):
        return None
    return {'version': 2, 'purpose': value['purpose'], 'create_exec_observed': value['create_exec_observed'],
            'group': value['group'], 'target': value['target'], 'program_kind': kind, 'commands': commands}


def _owner_probe_create_history_packet(data):
    if len(data) != 64 or data[:2] != b'\x04\x01' or data[2] not in (1, 2) or data[3] not in (0, 1) or data[4] > 3 or data[5] > 8 or data[6] > 3 or data[7] != 0:
        return None
    commands = []
    for index, name in enumerate(_OWNER_HISTORY_COMMANDS):
        count, state, result, number, policy, response = struct.unpack_from('<HBBHBB', data, 8 + index * 8)
        if policy not in (0, 1) or state > 5 or result > 4 or response > 5:
            return None
        if count and (state == 0 or result == 0 or response == 0):
            return None
        if not count and (state or result or number or policy or response):
            return None
        if result != 2 and number:
            return None
        commands.append({'command': name, 'count': count,
                         'state': _OWNER_HISTORY_STATES[state - 1] if state else None,
                         'result': _OWNER_HISTORY_RESULTS[result - 1] if result else None,
                         'errno': number if result == 2 else None, 'policy_error': bool(policy),
                         'response': _OWNER_HISTORY_RESPONSES[response - 1] if response else None})
    purpose = ('postgres-uid', 'postgres-gid')[data[2] - 1]
    value = {'version': 2, 'purpose': purpose, 'create_exec_observed': bool(data[3]),
             'group': _OWNER_HISTORY_GROUPS[data[4]], 'target': _OWNER_HISTORY_TARGETS[data[5]],
             'program_kind': _OWNER_HISTORY_PROGRAMS[data[6] - 1] if data[6] else None, 'commands': commands}
    return _owner_probe_create_history_value(value, purpose)


_OWNER_START_ERROR_MARKERS = (
    ('DEVICE_QUERY', 'bpf_prog_query(BPF_CGROUP_DEVICE) failed: '),
    ('PROGRAM_FETCH', 'cannot fetch program from id: '),
    ('DEVICE_LIST', 'could not get complete list of CGROUP_DEVICE programs'),
    ('DEVICE_ATTACH', 'failed to call BPF_PROG_ATTACH (BPF_CGROUP_DEVICE, BPF_F_ALLOW_MULTI): '),
    ('PROGRAM_LOAD', 'load program:'),
    ('CGROUP_CONFIG', 'error setting cgroup config for procHooks process: '),
    ('CGROUP_APPLY', 'unable to apply cgroup configuration: '),
    ('PROC_READY', 'procReady not received'),
    ('INIT_SYNC', 'error during container init: '),
    ('PROCESS_START', 'unable to start container process: '),
    ('TASK_CREATE', 'failed to create task for container'),
)


def _owner_probe_start_error(data):
    if type(data) is not bytes or len(data) > 65536:
        return 'INVALID'
    try:
        value = strict_json(data)
    except (JournalError, RecursionError):
        return 'INVALID'
    if type(value) is not dict or set(value) != {'message'} or type(value['message']) is not str:
        return 'INVALID'
    for name, marker in _OWNER_START_ERROR_MARKERS:
        if marker in value['message']:
            return name
    return 'OTHER'


def _owner_probe_start_error_value(exc, owner):
    if (type(owner) is not dict or owner.get('response') != 'RESPONSE_STATUS' or
            type(owner.get('http_status')) is not int or owner['http_status'] != 500):
        return None
    value = getattr(exc, '_g2_owner_probe_start_error', None)
    if type(value) is not str:
        return None
    if value in ('OTHER', 'INVALID') or any(value == name for name, _ in _OWNER_START_ERROR_MARKERS):
        return value
    return None


def _owner_probe_diagnostic_value(exc):
    value = getattr(exc, '_g2_owner_probe_start_diagnostic', None)
    if type(value) is not dict or set(value) != {'purpose', 'response', 'http_status', 'collection',
                                               'wrapper', 'broker', 'runc_exec_observed', 'mount_options', 'create_history'}:
        return None
    if (type(value['purpose']) is not str or value['purpose'] not in ('postgres-uid', 'postgres-gid') or
            type(value['response']) is not str or value['response'] not in ('BEFORE_REQUEST_DEADLINE', 'RESPONSE_DEADLINE', 'RESPONSE_SIZE',
                                                                           'RESPONSE_STATUS', 'OTHER') or
            type(value['collection']) is not str or value['collection'] not in ('UNAVAILABLE', 'EMPTY', 'OBSERVED', 'INVALID') or
            type(value['runc_exec_observed']) is not bool):
        return None
    status = value['http_status']
    if status is not None and (value['response'] != 'RESPONSE_STATUS' or type(status) is not int or not 100 <= status <= 599):
        return None
    for key, stages in (('wrapper', _OWNER_WRAPPER_STAGE_NAMES), ('broker', _OWNER_BROKER_STAGE_NAMES)):
        item = value[key]
        if item is not None and (type(item) is not tuple or len(item) != 2 or type(item[0]) is not str or item[0] not in stages or
                                 (item[1] is not None and (type(item[1]) is not int or not 1 <= item[1] <= 4095))):
            return None
    detail = value['mount_options']
    if not _owner_probe_mount_options_valid(detail):
        return None
    if value['broker'] is not None and value['broker'][0] == 'BROKER_OCI_MOUNT_OPTIONS' and value['broker'][1] is not None:
        return None
    if detail is not None and (value['collection'] != 'OBSERVED' or value['broker'] != ('BROKER_OCI_MOUNT_OPTIONS', None)):
        return None
    history = _owner_probe_create_history_value(value['create_history'], value['purpose'])
    if value['create_history'] is not None and (history is None or value['collection'] != 'OBSERVED'):
        return None
    present = value['wrapper'] is not None or value['broker'] is not None or value['runc_exec_observed'] or history is not None
    if (value['collection'] == 'OBSERVED') != present:
        return None
    return {'purpose': value['purpose'], 'response': value['response'], 'http_status': status,
            'collection': value['collection'],
            'wrapper': list(value['wrapper']) if value['wrapper'] is not None else None,
            'broker': list(value['broker']) if value['broker'] is not None else None,
            'runc_exec_observed': value['runc_exec_observed'],
            'mount_options': dict(detail) if detail is not None else None, 'create_history': history}

def _docker_runtime_info_eligible(owner):
    return (type(owner) is dict and owner.get('purpose') in ('postgres-uid', 'postgres-gid') and
            owner.get('collection') == 'OBSERVED' and owner.get('response') == 'RESPONSE_STATUS' and
            type(owner.get('http_status')) is int and owner['http_status'] == 400 and
            owner.get('broker') == ['BROKER_OCI_MOUNT_OPTIONS', None] and
            owner.get('mount_options') == {'mount': 'PGDATA', 'actual': 'RBIND_RO_RPRIVATE',
                                           'expected': 'RBIND_RRO_RPRIVATE', 'difference': 'VALUE'})


def _docker_runtime_info_projection(info):
    def invalid(reason):
        return {'collection': 'INVALID', 'features': None, 'reason': reason}

    def observed(features):
        return {'collection': 'OBSERVED', 'features': features, 'reason': 'OK'}

    if type(info) is not dict or type(info.get('DefaultRuntime')) is not str or info['DefaultRuntime'] != 'g2-bpf':
        return invalid('RUNTIME')
    runtimes = info.get('Runtimes')
    if type(runtimes) is not dict or type(runtimes.get('g2-bpf')) is not dict:
        return invalid('RUNTIME')
    selected = runtimes['g2-bpf']
    if (not set(selected).issubset({'path', 'runtimeArgs', 'status'}) or
            type(selected.get('path')) is not str or selected['path'] != '/opt/g2/g2-bpf-runtime' or
            ('runtimeArgs' in selected and (type(selected['runtimeArgs']) is not list or selected['runtimeArgs']))):
        return invalid('RUNTIME')
    status = selected.get('status', {})
    key = 'org.opencontainers.runtime-spec.features'
    if (type(status) is not dict or not set(status).issubset({key}) or
            any(type(value) is not str for value in status.values())):
        return invalid('RUNTIME')
    if key not in status:
        return observed('NOT_PUBLISHED')
    try:
        encoded = status[key].encode('utf-8', 'strict')
        if len(encoded) > 16384:
            return invalid('FEATURES_JSON')
        features = strict_json(encoded)
    except (Exception, RecursionError):
        return invalid('FEATURES_JSON')
    if type(features) is not dict:
        return invalid('FEATURES_SHAPE')
    options = features.get('mountOptions', [])
    if (type(options) is not list or len(options) > 128 or
            any(type(option) is not str or len(option) > 64 or not option.isascii() for option in options)):
        return invalid('FEATURES_SHAPE')
    return observed('RRO_PRESENT' if 'rro' in options else 'RRO_ABSENT')


def _docker_runtime_info_diagnostic_value(value):
    if type(value) is not dict or set(value) != {'collection', 'features', 'reason'}:
        return None
    collection, features, reason = value['collection'], value['features'], value['reason']
    if type(collection) is not str or type(reason) is not str or (features is not None and type(features) is not str):
        return None
    valid = ((collection == 'OBSERVED' and features in ('RRO_PRESENT', 'RRO_ABSENT', 'NOT_PUBLISHED') and reason == 'OK') or
             (collection == 'UNAVAILABLE' and features is None and reason in ('DEADLINE', 'RESPONSE_SIZE', 'RESPONSE_STATUS', 'TRANSPORT')) or
             (collection == 'INVALID' and features is None and reason in ('INFO_JSON', 'RUNTIME', 'FEATURES_JSON', 'FEATURES_SHAPE')))
    return dict(value) if valid else None


def _observe_docker_runtime_info(daemon, deadline):
    from guest_postgres import _UnixHTTP

    def unavailable(reason):
        return {'collection': 'UNAVAILABLE', 'features': None, 'reason': reason}

    now = time.monotonic()
    end = min(deadline, now + 1.0)
    if now >= end:
        return unavailable('DEADLINE')
    connection = response = timer = None
    result = unavailable('TRANSPORT')
    try:
        connection = _UnixHTTP(daemon, end - now)
        connection.connect()
        remaining = end - time.monotonic()
        if remaining <= 0:
            result = unavailable('DEADLINE')
        else:
            if connection.active_sock is None:
                raise JournalError('Docker observation socket missing')
            timer = threading.Timer(remaining, connection.abort)
            timer.daemon = True
            timer.start()
            connection.request('GET', '/v1.44/info')
            response = connection.getresponse()
            data = response.read(65537)
            if time.monotonic() >= end:
                result = unavailable('DEADLINE')
            elif len(data) > 65536:
                result = unavailable('RESPONSE_SIZE')
            else:
                status = response.status
                if type(status) is not int or status != 200:
                    result = unavailable('RESPONSE_STATUS')
                else:
                    try:
                        info = strict_json(data)
                    except Exception:
                        result = {'collection': 'INVALID', 'features': None, 'reason': 'INFO_JSON'}
                    else:
                        result = _docker_runtime_info_projection(info)
    except BaseException:
        result = unavailable('DEADLINE' if time.monotonic() >= end else 'TRANSPORT')
    finally:
        for owned, method in ((timer, 'cancel'), (response, 'close'), (connection, 'close')):
            if owned is not None:
                try:
                    getattr(owned, method)()
                except BaseException:
                    result = unavailable('TRANSPORT')
    return result



_OWNER_SETTLEMENT_POINTS = (
    'EFFECT_OPERATION', 'ALIAS_EXECUTION', 'OBJECT_CLEANUP_STARTED',
    'CGROUP_TARGET_UNOBSERVED', 'CGROUP_RETIRE_EAGAIN', 'CGROUP_TARGET_RELEASE',
    'INVOCATION', 'EXEC_ADMISSION', 'TRANSACTION_RETIREMENT', 'OBJECT_REMOVAL',
    'ATTACHMENT_RELEASE', 'OWNER_PROBE_RESULT', 'AUDIT_JOIN')

_OWNER_REMOVAL_FIELDS = (
    ('object', ('MISSING', 'MAP', 'PROGRAM', 'OTHER_CLASS', 'IDENTITY_MISMATCH')),
    ('held_fd', ('ABSENT', 'PRESENT', 'INVALID')),
    ('grant_cleanup', ('ABSENT', 'ZERO_PENDING', 'ZERO_REMOVED', 'NEGATIVE_PENDING',
                       'NEGATIVE_REMOVED', 'OTHER_RESULT', 'IDENTITY_MISMATCH')),
    ('creator', ('UNAVAILABLE', 'MISSING', 'IDENTITY_MISMATCH', 'PREPARED', 'COMMITTED',
                 'EFFECT_OBSERVED', 'RESPONSE_SETTLED', 'RETIRED', 'OTHER_STATE')),
    ('creation_cleanup', ('UNAVAILABLE', 'FLAG_CLEAR', 'FLAG_SET')),
    ('lifecycle', ('MISSING', 'UNAVAILABLE', 'IDENTITY_MISMATCH', 'PENDING_UNAUTHORIZED',
                   'PENDING_AUTHORIZED', 'REMOVED_UNAUTHORIZED', 'REMOVED_AUTHORIZED')),
)


def _owner_probe_removal_value(value):
    if type(value) is not dict or set(value) != {key for key, _ in _OWNER_REMOVAL_FIELDS} | {'removal_authorized'}:
        return None
    for key, choices in _OWNER_REMOVAL_FIELDS:
        if type(value[key]) is not str or value[key] not in choices:
            return None
    authorized = value['removal_authorized']
    if authorized is not None and type(authorized) is not bool:
        return None
    object_valid = value['object'] not in ('MISSING', 'IDENTITY_MISMATCH')
    if not object_valid and (value['creator'] != 'UNAVAILABLE' or
                             value['creation_cleanup'] != 'UNAVAILABLE' or authorized is not None or
                             value['lifecycle'] not in ('MISSING', 'UNAVAILABLE')):
        return None
    if object_valid and authorized is None:
        return None
    creator_valid = value['creator'] not in ('UNAVAILABLE', 'MISSING', 'IDENTITY_MISMATCH')
    if (value['creation_cleanup'] == 'UNAVAILABLE') == creator_valid:
        return None
    return dict(value)


def _owner_probe_removal_packet(detail):
    if detail == b'\0' * 7:
        return None
    if len(detail) != 7 or any(not 1 <= detail[index] <= len(choices)
                               for index, (_, choices) in enumerate(_OWNER_REMOVAL_FIELDS)) or detail[6] not in (1, 2, 3):
        raise ValueError('invalid removal diagnostic')
    value = {key: choices[detail[index] - 1] for index, (key, choices) in enumerate(_OWNER_REMOVAL_FIELDS)}
    value['removal_authorized'] = (None, False, True)[detail[6] - 1]
    result = _owner_probe_removal_value(value)
    if result is None:
        raise ValueError('invalid removal diagnostic')
    return result


_OWNER_FOLLOWUP_FIELDS = (
    ('boundary', ('CONTROL_DETACHED', 'ACK_COMMITTED', 'PRINCIPAL_TERMINAL', 'BROKER_EXIT')),
    ('last_wait', _OWNER_SETTLEMENT_POINTS + ('UNLABELLED',)),
    ('identity', ('MATCH', 'MISSING', 'MISMATCH')),
    ('grant_close', ('NOT_OBSERVED', 'ZERO', 'NEGATIVE', 'OTHER_RESULT', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
    ('guard_removal', ('NOT_OBSERVED', 'OBSERVED', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
    ('tracker', ('PENDING', 'REMOVED', 'INCONSISTENT', 'UNAVAILABLE')),
    ('lifecycle', _OWNER_REMOVAL_FIELDS[5][1]),
    ('cleanup_record', ('NOT_OBSERVED', 'CLOSE_ONLY', 'CLOSE_AND_REMOVAL', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
    ('acceptance', ('UNKNOWN', 'ACCEPTED', 'INCONSISTENT')),
    ('attachments', ('NONE', 'ACTIVE_PRESENT', 'DETACHED_OR_OFFLINING_PRESENT', 'ALL_RELEASED', 'MIXED', 'INCONSISTENT', 'TRUNCATED', 'UNAVAILABLE')),
    ('program_type', ('NOT_PROGRAM', 'LSM', 'CGROUP_DEVICE', 'OTHER', 'UNAVAILABLE', 'INCONSISTENT')),
    ('creator_response', ('NOT_OBSERVED', 'ADDFD_SEND_OK', 'ADDFD_SEND_ENOENT', 'SEND_OR_ERROR', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
)


def _owner_probe_followup_value(value):
    if type(value) is not dict or set(value) != {key for key, _ in _OWNER_FOLLOWUP_FIELDS}:
        return None
    for index, (key, choices) in enumerate(_OWNER_FOLLOWUP_FIELDS):
        item = value[key]
        if item is None:
            if index == 0 or (index >= 3 and value['identity'] == 'MATCH'):
                return None
        elif type(item) is not str or item not in choices:
            return None
    if value['boundary'] == 'ACK_COMMITTED' and value['last_wait'] is not None:
        return None
    if value['identity'] != 'MATCH' and any(value[key] is not None for key, _ in _OWNER_FOLLOWUP_FIELDS[3:]):
        return None
    if value['acceptance'] == 'ACCEPTED' and (
            value['grant_close'] != 'ZERO' or value['guard_removal'] != 'OBSERVED' or
            value['tracker'] != 'REMOVED' or value['lifecycle'] != 'REMOVED_AUTHORIZED' or
            value['cleanup_record'] != 'CLOSE_AND_REMOVAL' or
            value['attachments'] not in ('NONE', 'ACTIVE_PRESENT', 'DETACHED_OR_OFFLINING_PRESENT',
                                         'ALL_RELEASED', 'MIXED') or
            value['program_type'] not in ('NOT_PROGRAM', 'LSM', 'CGROUP_DEVICE', 'OTHER') or
            value['creator_response'] not in ('ADDFD_SEND_OK', 'ADDFD_SEND_ENOENT', 'SEND_OR_ERROR')):
        return None
    return dict(value)


def _owner_probe_followup_packet(data):
    if len(data) != 12:
        raise ValueError('invalid followup diagnostic')
    value = {}
    for index, (key, choices) in enumerate(_OWNER_FOLLOWUP_FIELDS):
        code = data[index]
        if code > len(choices):
            raise ValueError('invalid followup diagnostic')
        value[key] = choices[code - 1] if code else None
    result = _owner_probe_followup_value(value)
    if result is None:
        raise ValueError('invalid followup diagnostic')
    return result

def _owner_probe_settlement_value(value):
    if type(value) is not dict or set(value) != {'purpose', 'collection', 'started', 'wait', 'ack_committed', 'ack_sent', 'removal', 'followup'}:
        return None
    if (type(value['purpose']) is not str or value['purpose'] not in ('postgres-uid', 'postgres-gid') or
            type(value['collection']) is not str or value['collection'] not in ('UNAVAILABLE', 'EMPTY', 'OBSERVED', 'INVALID') or
            any(type(value[key]) is not bool for key in ('started', 'ack_committed', 'ack_sent')) or
            (value['wait'] is not None and (type(value['wait']) is not str or value['wait'] not in _OWNER_SETTLEMENT_POINTS))):
        return None
    observed = value['started'] or value['wait'] is not None or value['ack_committed'] or value['ack_sent'] or value['followup'] is not None
    if observed != (value['collection'] == 'OBSERVED'):
        return None
    removal = value['removal']
    if removal is not None:
        if value['collection'] != 'OBSERVED' or value['wait'] != 'OBJECT_REMOVAL':
            return None
        removal = _owner_probe_removal_value(removal)
        if removal is None:
            return None
    followup = value['followup']
    if followup is not None:
        if value['collection'] != 'OBSERVED':
            return None
        followup = _owner_probe_followup_value(followup)
        if followup is None:
            return None
    return dict(value, removal=removal, followup=followup)


def _preflight_failure_diagnostic(exc):
    try:
        kind = 'OSERROR' if isinstance(exc, OSError) else 'JOURNAL' if isinstance(exc, JournalError) else 'OTHER'
        error_number = exc.errno if isinstance(exc, OSError) else None
        if type(error_number) is not int or not 1 <= error_number <= 4095:
            error_number = None
        allowed = ('source_guest.py', 'guest_storage.py', 'guest_bootstrap.py', 'census.py',
                   'bpf_authority.py', 'guest_postgres.py', 'journal.py', 'volume_verify.py')
        frames, tb, count = [], exc.__traceback__, 0
        while tb is not None:
            count += 1
            if count > 128:
                return None
            name = os.path.basename(tb.tb_frame.f_code.co_filename)
            if name in allowed:
                line = tb.tb_lineno
                if type(line) is not int or not 1 <= line <= 2147483647:
                    return None
                if len(frames) == 6:
                    del frames[0]
                frames.append([name, line])
            tb = tb.tb_next
        exception = {'kind': kind, 'errno': error_number, 'frames': frames}
    except BaseException:
        return None
    try:
        owner = _owner_probe_diagnostic_value(exc)
    except BaseException:
        owner = None
    try:
        info = _docker_runtime_info_diagnostic_value(getattr(exc, '_g2_docker_runtime_info_diagnostic', None))
        if not _docker_runtime_info_eligible(owner):
            info = None
    except BaseException:
        info = None
    try:
        start_error = _owner_probe_start_error_value(exc, owner)
    except BaseException:
        start_error = None
    try:
        settlement = _owner_probe_settlement_value(getattr(exc, '_g2_owner_probe_settlement_diagnostic', None))
        if owner is not None or info is not None or start_error is not None:
            settlement = None
    except BaseException:
        settlement = None
    try:
        record = {'version': 6, 'exception': exception, 'owner_probe': owner,
                  'docker_runtime_info': info, 'start_error': start_error, 'settlement': settlement}
        if len(canonical(record)) > 2048:
            record['settlement'] = None
            return record if len(canonical(record)) <= 2048 else None
        return record
    except BaseException:
        return None


class _OwnerProbeStartDiagnostic:
    _name = 'owner-probe-diagnostic'
    _path = '/run/g2/owner-probe-diagnostic/'

    def __init__(self, source, cid):
        self.source, self.cid = source, cid
        self.directory = self.sock = self.identity = self.socket_identity = None
        try:
            source._check_parents()
            os.mkdir(self._name, 0o700, dir_fd=source._parent)
            self.directory = os.open(self._name, source._flags, dir_fd=source._parent)
            self.identity = _source_directory_identity(self.directory, True)
            mount = _launch_failure_mount(self.identity[5])
            if (self.identity[5] != source._parent_identity[5] or mount.get('status') != 'ok' or
                    mount.get('fs_type') != {'status': 'ok', 'value': 'tmpfs'}):
                raise JournalError('owner probe diagnostic directory differs')
            self._check_directory()
            self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM | socket.SOCK_NONBLOCK | socket.SOCK_CLOEXEC)
            self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            self.sock.bind(self._path + cid)
            relative = os.stat(cid, dir_fd=self.directory, follow_symlinks=False)
            absolute = os.stat(self._path + cid, follow_symlinks=False)
            self.socket_identity = self._socket_identity(relative)
            if self.socket_identity != self._socket_identity(absolute):
                self.socket_identity = None
                raise JournalError('owner probe diagnostic socket differs')
            self._check()
        except BaseException:
            self.close()
            raise

    @staticmethod
    def _socket_identity(info):
        if not stat.S_ISSOCK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0:
            raise JournalError('owner probe diagnostic socket differs')
        return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid)

    def _check_directory(self):
        self.source._check_parents()
        _source_check_named_directory('/run/g2/' + self._name, self.directory, self.identity, True)
        self.source._check_parents()

    def _check(self):
        self._check_directory()
        if (self._socket_identity(os.stat(self.cid, dir_fd=self.directory, follow_symlinks=False)) != self.socket_identity or
                self._socket_identity(os.stat(self._path + self.cid, follow_symlinks=False)) != self.socket_identity):
            raise JournalError('owner probe diagnostic socket changed')

    def _receive_packet(self, limit):
        data, ancillary, flags, _ = self.sock.recvmsg(limit, socket.CMSG_SPACE(struct.calcsize('=iii')),
                                                     socket.MSG_DONTWAIT | socket.MSG_CMSG_CLOEXEC)
        valid = not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC)
        credentials = []
        for level, kind, payload in ancillary:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                size = struct.calcsize('=i')
                for offset in range(0, len(payload) - len(payload) % size, size):
                    try:
                        os.close(struct.unpack_from('=i', payload, offset)[0])
                    except OSError:
                        pass
                valid = False
            elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS and len(payload) == 12:
                credentials.append(struct.unpack('=iii', payload))
            else:
                valid = False
        valid = valid and len(credentials) == 1 and credentials[0][0] > 1 and credentials[0][1:] == (0, 0)
        return data, valid

    def collect(self):
        invalid = False
        found = {'wrapper': None, 'broker': None, 'exec': False, 'mount_options': None, 'create_history': None}
        try:
            self._check()
            for index in range(5):
                try:
                    data, valid = self._receive_packet(64)
                except BlockingIOError:
                    break
                if index == 4 or not valid:
                    invalid = True
                    continue
                if len(data) == 64:
                    history = _owner_probe_create_history_packet(data)
                    if history is None or found['create_history'] is not None:
                        invalid = True
                    else:
                        found['create_history'] = history
                    continue
                if len(data) != 8:
                    invalid = True
                    continue
                version, stage, low, high, mount, actual, expected, difference = data
                number = low | high << 8
                detail = (mount, actual, expected, difference)
                valid_detail = detail == (0, 0, 0, 0) or (1 <= mount <= 12 and 1 <= actual <= 17 and
                                                         1 <= expected <= 17 and 1 <= difference <= 9)
                if (version != 2 or number > 4095 or
                        (stage != 188 and detail != (0, 0, 0, 0)) or
                        (stage == 188 and (number != 0 or not valid_detail))):
                    invalid = True
                elif 1 <= stage <= 24 and found['wrapper'] is None:
                    found['wrapper'] = (_OWNER_WRAPPER_STAGES[stage - 1], number or None)
                elif 80 <= stage <= 150 and found['wrapper'] is None:
                    found['wrapper'] = (_OWNER_PREPARE_STAGES[stage - 80], number or None)
                elif 32 <= stage <= 44 and found['broker'] is None:
                    found['broker'] = (_OWNER_BROKER_STAGES[stage - 32], number or None)
                elif 45 <= stage <= 51 and found['broker'] is None:
                    found['broker'] = (_OWNER_HANDLER_REJECTION_STAGES[stage - 45], number or None)
                elif 151 <= stage <= 241 and found['broker'] is None:
                    found['broker'] = (_OWNER_OCI_STAGES[stage - 151], number or None)
                    if stage == 188 and mount:
                        found['mount_options'] = {'mount': _OWNER_MOUNT_NAMES[mount - 1],
                                                  'actual': _OWNER_OPTION_SIGNATURE_NAMES[actual - 1],
                                                  'expected': _OWNER_OPTION_SIGNATURE_NAMES[expected - 1],
                                                  'difference': _OWNER_OPTION_DIFFERENCE_NAMES[difference - 1]}
                elif stage == 64 and number == 0 and not found['exec']:
                    found['exec'] = True
                else:
                    invalid = True
            self._check()
        except BaseException:
            invalid = True
        if invalid:
            return 'INVALID', None, None, False, None, None
        observed = found['wrapper'] is not None or found['broker'] is not None or found['exec'] or found['create_history'] is not None
        return ('OBSERVED' if observed else 'EMPTY', found['wrapper'], found['broker'], found['exec'], found['mount_options'], found['create_history'])

    def close(self):
        try:
            if self.directory is not None and self.identity is not None:
                self._check_directory()
                if self.socket_identity is not None:
                    self._check()
                    os.unlink(self.cid, dir_fd=self.directory)
                elif self.sock is not None:
                    return
                self._check_directory()
                os.rmdir(self._name, dir_fd=self.source._parent)
        except BaseException:
            pass
        finally:
            if self.sock is not None:
                try:
                    self.sock.close()
                except BaseException:
                    pass
                self.sock = None
            if self.directory is not None:
                try:
                    os.close(self.directory)
                except BaseException:
                    pass
                self.directory = None


class _OwnerProbeSettlementDiagnostic(_OwnerProbeStartDiagnostic):
    _name = 'owner-probe-settlement-diagnostic'
    _path = '/run/g2/owner-probe-settlement-diagnostic/'

    def __init__(self, source, cid, purpose):
        self.purpose = purpose
        super().__init__(source, cid)

    def collect(self, hard_deadline=None):
        result = {'purpose': self.purpose, 'collection': 'EMPTY', 'started': False,
                  'wait': None, 'ack_committed': False, 'ack_sent': False, 'removal': None, 'followup': None}
        invalid = False
        seen = set()
        try:
            self._check()
            now = time.monotonic()
            cutoff = (min(now + 0.250, hard_deadline)
                      if type(hard_deadline) is float and math.isfinite(hard_deadline) and hard_deadline > now
                      else None)
            count = 0
            while count < 6:
                if cutoff is not None and time.monotonic() >= cutoff:
                    break
                try:
                    data, valid = self._receive_packet(16)
                except BlockingIOError:
                    if cutoff is None or invalid or 5 in seen:
                        break
                    remaining = cutoff - time.monotonic()
                    if remaining <= 0:
                        break
                    try:
                        readable, _, _ = select.select([self.sock], [], [], remaining)
                    except InterruptedError:
                        continue
                    if time.monotonic() >= cutoff or not readable:
                        break
                    continue
                count += 1
                if (count == 6 or not valid or len(data) != 16 or data[:2] != b'\x07\x01' or
                        data[2] != (1 if self.purpose == 'postgres-uid' else 2) or
                        data[3] not in (1, 2, 3, 4, 5)):
                    invalid = True
                    continue
                event = data[3]
                if event in seen:
                    invalid = True
                    continue
                if event == 5:
                    try:
                        result['followup'] = _owner_probe_followup_packet(data[4:16])
                    except ValueError:
                        invalid = True
                        continue
                else:
                    point = data[4]
                    if (data[12:] != b'\0' * 4 or (event == 2 and not 1 <= point <= 13) or
                            (event != 2 and point != 0)):
                        invalid = True
                        continue
                    detail = data[5:12]
                    if event != 2 or point != 10:
                        if detail != b'\0' * 7:
                            invalid = True
                            continue
                    else:
                        try:
                            result['removal'] = _owner_probe_removal_packet(detail)
                        except ValueError:
                            invalid = True
                            continue
                    if event == 2:
                        result['wait'] = _OWNER_SETTLEMENT_POINTS[point - 1]
                    else:
                        result[{1: 'started', 3: 'ack_committed', 4: 'ack_sent'}[event]] = True
                seen.add(event)
            self._check()
        except BaseException:
            invalid = True
        if invalid:
            result.update(collection='INVALID', started=False, wait=None, ack_committed=False,
                          ack_sent=False, removal=None, followup=None)
        elif seen:
            result['collection'] = 'OBSERVED'
        return result


def _source_check_executable_mount(fd):
    noexec = getattr(os, 'ST_NOEXEC', None)
    if noexec is None or os.fstatvfs(fd).f_flag & noexec:
        raise JournalError('source launcher mount is not executable')
    mount_id = _launch_failure_mount_id(fd)
    if mount_id['status'] != 'ok':
        raise JournalError('source launcher mount identity unavailable')
    mount = _launch_failure_mount(mount_id['value'])
    if mount['status'] != 'ok':
        raise JournalError('source launcher mount identity invalid')
    options = mount['mount_options']['values']
    if 'noexec' in options:
        raise JournalError('source launcher mount is not executable')
    return mount_id['value']


def launch_source():
    """Cloud-init stages the one exclusive authority executable before launch."""
    if os.geteuid() != 0 or os.getegid() != 0 or os.path.lexists('/opt/g2'):
        raise JournalError('early source launch is unavailable')
    _, expected = verify_seed()
    path = '/opt/g2-launch/g2-bpf-authority'
    directories = []
    executable = None
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    try:
        root = os.open('/', flags)
        directories.append(root)
        root_identity = _source_directory_identity(root)
        opt = os.open('opt', flags, dir_fd=root)
        directories.append(opt)
        opt_identity = _source_directory_identity(opt)
        _source_check_named_directory('/', root, root_identity)
        _source_check_named_directory('/opt', opt, opt_identity)
        os.mkdir('g2-launch', 0o700, dir_fd=opt)
        os.fsync(opt)
        directory = os.open('g2-launch', flags, dir_fd=opt)
        directories.append(directory)
        directory_identity = _source_directory_identity(directory, private=True)
        _source_check_named_directory('/opt/g2-launch', directory, directory_identity, private=True)

        _install_file(SEED + '/g2-bpf-authority', path, expected['g2-bpf-authority'], 0o500, 0)
        os.fsync(directory)
        executable = os.open('g2-bpf-authority', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                             dir_fd=directory)
        info = os.fstat(executable)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                stat.S_IMODE(info.st_mode) != 0o500 or info.st_nlink != 1):
            raise JournalError('staged source authority differs')
        identity = _launch_failure_stat(info)
        digest = hashlib.sha256()
        while True:
            data = os.read(executable, 1024 * 1024)
            if not data:
                break
            digest.update(data)
        if digest.hexdigest() != expected['g2-bpf-authority']:
            raise JournalError('staged source authority digest differs')
        mount_id = _source_check_executable_mount(executable)
        _source_check_named_directory('/', root, root_identity)
        _source_check_named_directory('/opt', opt, opt_identity)
        _source_check_named_directory('/opt/g2-launch', directory, directory_identity, private=True)
        named = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            if (_launch_failure_stat(os.fstat(executable)) != identity or
                    _launch_failure_stat(os.fstat(named)) != identity or
                    _launch_failure_stat(os.stat('g2-bpf-authority', dir_fd=directory,
                                                 follow_symlinks=False)) != identity or
                    _launch_failure_stat(os.stat(path, follow_symlinks=False)) != identity or
                    _source_check_executable_mount(named) != mount_id or
                    _source_check_executable_mount(executable) != mount_id):
                raise JournalError('staged source authority identity changed')
        finally:
            os.close(named)
        try:
            os.execve(path, [path, 'launch-source'], ENV)
        except OSError as exc:
            if exc.errno == errno.EACCES:
                try:
                    _emit_launch_exec_failure(path, exc, info)
                except BaseException:
                    pass  # A diagnostic failure must never replace the exec failure.
            raise
    finally:
        if executable is not None:
            os.close(executable)
        for fd in reversed(directories):
            os.close(fd)


def _migration_binding(binding):
    for ancestor in ('/opt', '/opt/g2'):
        info = os.stat(ancestor, follow_symlinks=False)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise JournalError('migration binding ancestor is writable')
    fd = os.open(MIGRATION_BINDING, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or (before.st_uid, before.st_gid) != (0, 10002) or
                stat.S_IMODE(before.st_mode) != 0o440 or before.st_nlink != 1 or not 0 < before.st_size <= 16384):
            raise JournalError('migration binding permissions differ')
        raw = os.read(fd, before.st_size + 1)
        after = os.fstat(fd)
        if (len(raw) != before.st_size or
                (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) !=
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) or
                raw != canonical(binding) or strict_json(raw) != binding):
            raise JournalError('migration binding bytes changed')
        return {'path': MIGRATION_BINDING, 'dev': before.st_dev, 'ino': before.st_ino,
                'uid': before.st_uid, 'gid': before.st_gid, 'mode': stat.S_IMODE(before.st_mode),
                'nlink': before.st_nlink, 'sha256': hashlib.sha256(raw).hexdigest(), 'raw': raw.decode('ascii')}
    finally:
        os.close(fd)


def verify_migration_binding(installed):
    binding = {'version': 1, 'boot_id': boot_id(), 'program_sha256': installed['programs']['c81-migrator'],
               'coordinator_sha256': installed['programs']['houfeng-record-platform-admin'],
               'cgroup': installed['migration_cgroup']}
    if (_migration_binding(binding) != installed['migration_binding'] or
            binding['cgroup']['path'] != MIGRATION_CGROUP):
        raise JournalError('installed migration binding changed')
    return binding


def _install_migration_binding(binding):
    if any(type(binding[key]) is not str or re.fullmatch(r'[0-9a-f]{64}', binding[key]) is None or
           binding[key] == '0' * 64 for key in ('program_sha256', 'coordinator_sha256')):
        raise JournalError('migration program digest is invalid')
    raw = canonical(binding)
    fd = os.open(MIGRATION_BINDING, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchown(fd, 0, 10002)
        os.fchmod(fd, 0o440)
        view = memoryview(raw)
        while view:
            count = os.write(fd, view)
            if count <= 0:
                raise JournalError('migration binding write failed')
            view = view[count:]
        os.fsync(fd)
    finally:
        os.close(fd)
    directory = os.open('/opt/g2', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return _migration_binding(binding)


def _system_user():
    try:
        pwd.getpwuid(10002)
    except KeyError:
        pass
    else:
        raise JournalError('coordinator UID already allocated')
    try:
        grp.getgrgid(10002)
    except KeyError:
        pass
    else:
        raise JournalError('coordinator GID already allocated')
    command(['/usr/sbin/groupadd', '--gid', '10002', 'g2coord'])
    command(['/usr/sbin/useradd', '--uid', '10002', '--gid', '10002', '--no-create-home',
             '--shell', '/usr/sbin/nologin', 'g2coord'])


def _disable_copy_services():
    units = ('ssh.service', 'ssh.socket', 'getty@tty1.service', 'serial-getty@ttyS0.service',
             'cron.service', 'unattended-upgrades.service',
             'systemd-coredump.socket', 'apt-daily.timer', 'apt-daily-upgrade.timer',
             'fstrim.timer', 'e2scrub_all.timer')
    for unit in units:
        load = command(['/usr/bin/systemctl', 'show', '--property=LoadState', '--value', unit], timeout=30).stdout.strip()
        if load == b'loaded':
            command(['/usr/bin/systemctl', 'stop', unit], timeout=30)
        elif load not in (b'not-found', b'masked'):
            raise JournalError('unexpected guest service load state')
        command(['/usr/bin/systemctl', 'mask', unit], timeout=30)
        active = command(['/usr/bin/systemctl', 'is-active', unit], timeout=30, allowed=(0, 3, 4)).stdout.strip()
        if active == b'active' or command(['/usr/bin/systemctl', 'is-enabled', unit], timeout=30,
                                         allowed=(0, 1)).stdout.strip() not in (b'masked', b'masked-runtime'):
            raise JournalError('guest service remains available')
    # Templates cannot have LoadState or ActiveState; the concrete instances above
    # are stopped first, then masking the templates prevents new instances.
    for unit in ('getty@.service', 'serial-getty@.service'):
        command(['/usr/bin/systemctl', 'mask', unit], timeout=30)
        if command(['/usr/bin/systemctl', 'is-enabled', unit], timeout=30,
                   allowed=(0, 1)).stdout.strip() not in (b'masked', b'masked-runtime'):
            raise JournalError('guest service template remains available')
    command(['/usr/sbin/swapoff', '-a'])
    with open('/proc/swaps', 'r', encoding='ascii') as stream:
        if len(stream.read(65536).splitlines()) != 1:
            raise JournalError('guest swap is active')
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    with open('/proc/sys/kernel/core_pattern', 'w', encoding='ascii') as stream:
        stream.write('/dev/null\n')
    with open('/proc/sys/kernel/core_pattern', 'r', encoding='ascii') as stream:
        if stream.read(128).strip() != '/dev/null':
            raise JournalError('guest core dump handler remains enabled')


def _install_reboot_service():
    path = '/etc/systemd/system/g2-reboot-observer.service'
    body = (b'[Unit]\nDescription=G2 closed realm reboot observer\n'
            b'ConditionPathExists=/var/lib/g2/realm-control/reboot-intent.json\n'
            b'After=local-fs.target systemd-udev-settle.service\n'
            b'Wants=systemd-udev-settle.service\n'
            b'[Service]\nType=oneshot\n'
            b'ExecStart=/opt/g2/g2-bpf-authority launch-reboot\n'
            b'ExecStartPost=/usr/bin/systemctl --no-block poweroff\n'
            b'[Install]\nWantedBy=multi-user.target\n')
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644)
    try:
        os.fchmod(fd, 0o644)
        if os.write(fd, body) != len(body):
            raise JournalError('reboot observer unit write failed')
        os.fsync(fd)
    finally:
        os.close(fd)
    directory = os.open('/etc/systemd/system', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    command(['/usr/bin/systemctl', 'daemon-reload'])
    command(['/usr/bin/systemctl', 'enable', 'g2-reboot-observer.service'])
    if command(['/usr/bin/systemctl', 'is-enabled', 'g2-reboot-observer.service']).stdout.strip() != b'enabled':
        raise JournalError('reboot observer was not enabled before genesis')


def install(expected):
    if os.path.lexists('/opt/g2'):
        raise JournalError('guest program directory already exists')
    _disable_copy_services()
    _system_user()
    os.mkdir('/opt/g2', 0o755)
    os.mkdir('/opt/g2/compose', 0o755)
    supervisor_dir = '/opt/g2/realm-supervisor'
    os.mkdir(supervisor_dir, 0o700)
    for name in PROGRAMS:
        if name == 'g2-bpf-authority':
            _install_file(SEED + '/' + name, '/opt/g2/' + name, expected[name], 0o500, 0)
            _install_file(SEED + '/' + name, '/opt/g2/g2-bpf-runtime', expected[name], 0o500, 0)
            _install_file(SEED + '/' + name, '/opt/g2/g2-container-bpf-probe', expected[name], 0o550, 10002)
        else:
            mode = 0o4750 if name == 'c80-launcher' else 0o550
            _install_file(SEED + '/' + name, '/opt/g2/' + name, expected[name], mode, 10002)
    _install_managed_containerd(expected['g2-bpf-authority'])
    fifo = RUN + '/container-bpf.fifo'
    created = False
    try:
        os.mkfifo(fifo, 0o600)
        created = True
    except FileExistsError:
        pass
    fifo_fd = os.open(fifo, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fifo_fd)
        if not stat.S_ISFIFO(info.st_mode) or info.st_uid != 0:
            raise JournalError('container BPF FIFO identity differs')
        if created:
            os.fchown(fifo_fd, 0, 10002)
            os.fchmod(fifo_fd, 0o640)
            info = os.fstat(fifo_fd)
        if info.st_gid != 10002 or stat.S_IMODE(info.st_mode) != 0o640:
            raise JournalError('container BPF FIFO identity differs')
        named = os.stat(fifo, follow_symlinks=False)
        if (named.st_dev, named.st_ino) != (info.st_dev, info.st_ino):
            raise JournalError('container BPF FIFO replaced')
    finally:
        os.close(fifo_fd)
    run_fd = os.open(RUN, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(run_fd)
    finally:
        os.close(run_fd)
    for name in SUPERVISOR:
        _install_file(SEED + '/' + name, supervisor_dir + '/' + name, expected[name], 0o400, 0)
    _install_file(SEED + '/supervisor-manifest.json', supervisor_dir + '/manifest.json',
                  expected['supervisor-manifest.json'], 0o400, 0)
    _install_reboot_service()
    for path in ('/opt/g2', '/opt/g2/compose', supervisor_dir):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    groups = {}
    for name in ('g2-c80-predecessor', 'g2-c81-migration', 'g2-writers'):
        path = '/sys/fs/cgroup/' + name
        os.mkdir(path, 0o755)
        if name == 'g2-c81-migration':
            group_fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                os.fchmod(group_fd, 0o755)
            finally:
                os.close(group_fd)
        info = os.stat(path, follow_symlinks=False)
        if name == 'g2-c81-migration' and (path != MIGRATION_CGROUP or stat.S_IMODE(info.st_mode) != 0o755):
            raise JournalError('migration cgroup permissions differ')
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0:
            raise JournalError('guest cgroup identity invalid')
        with open(path + '/cgroup.procs', 'rb') as stream:
            if stream.read(4097).strip():
                raise JournalError('new guest cgroup is populated')
        groups[name] = {'path': path, 'dev': info.st_dev, 'ino': info.st_ino}
    # The coordinator may only freeze/kill the predeclared empty writer group;
    # it cannot move tasks into it or write the separate predecessor group.
    for name in ('cgroup.freeze', 'cgroup.kill'):
        path = '/sys/fs/cgroup/g2-writers/' + name
        fd = os.open(path, os.O_WRONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fchown(fd, 0, 10002)
            os.fchmod(fd, 0o660)
            info = os.fstat(fd)
            if info.st_uid != 0 or info.st_gid != 10002 or stat.S_IMODE(info.st_mode) != 0o660:
                raise JournalError('writer cgroup control permissions differ')
        finally:
            os.close(fd)
    programs = {name: expected[name] for name in PROGRAMS}
    migration_cgroup = groups['g2-c81-migration']
    if network_verify.migration_scope(migration_cgroup) != {'members': [], 'populated': 0}:
        raise JournalError('new migration cgroup is not empty')
    binding = {'version': 1, 'boot_id': boot_id(), 'program_sha256': programs['c81-migrator'],
               'coordinator_sha256': programs['houfeng-record-platform-admin'], 'cgroup': migration_cgroup}
    migration_binding = _install_migration_binding(binding)
    return {'programs': programs, 'supervisor_manifest_sha256': expected['supervisor-manifest.json'],
            'child_cgroup': groups['g2-c80-predecessor'], 'writer_cgroup': groups['g2-writers'],
            'migration_cgroup': migration_cgroup, 'migration_binding': migration_binding}


class _OwnerSettlementEvidenceChannel:
    """Optional broker diagnostics; failure never grants or extends authority."""
    def __init__(self, authority, purpose):
        self.purpose = purpose
        self.nonce = secrets.token_hex(16)
        self.peer = authority._peer
        self.boot = authority.boot_id.replace('-', '')
        self.epoch = authority.epoch
        self.principal = authority.principal_id
        self.hard_deadline = authority._deadline
        self.sock = None
        self.armed = False
        self.invalid = False
        self.container_id = None
        self.guest = {'operation_start_ns': 0, 'operation_deadline_ns': 0,
                      'authority_deadline_ns': 0, 'ack_received_ns': 0}
        try:
            deadline = min(time.monotonic() + .250, self.hard_deadline)
            if deadline <= time.monotonic():
                return
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            self.sock = sock
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            sock.settimeout(max(0.000001, deadline - time.monotonic()))
            sock.connect(OWNER_SETTLEMENT_SOCKET)
            peer = struct.unpack('3i', sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
            if peer != self.peer:
                self.invalid = True
                return
            hello = canonical({'version': 1, 'purpose': purpose, 'nonce': self.nonce, 'principal_id': self.principal})
            if len(hello) > 160 or sock.send(hello) != len(hello):
                self.invalid = True
                return
            sock.settimeout(max(0.000001, deadline - time.monotonic()))
            data = self._recv(129)
            if not data:
                return
            reply = strict_json(data)
            if (len(data) > 128 or type(reply) is not dict or
                    reply != {'version': 1, 'status': 'ARMED', 'nonce': self.nonce} or
                    type(reply['version']) is not int or canonical(reply) != data):
                self.invalid = True
                return
            self.armed = True
            sock.setblocking(False)
        except (ValueError, TypeError, AttributeError, JournalError):
            self.invalid = True
        except OSError:
            pass

    def _recv(self, size):
        data, ancillary, flags, _ = self.sock.recvmsg(size, socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(256))
        if not data and not ancillary and not flags:
            return b''
        credentials, rights = [], False
        for level, kind, payload in ancillary:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                descriptors = array.array('i')
                descriptors.frombytes(payload[:len(payload) - len(payload) % descriptors.itemsize])
                for fd in descriptors:
                    try:
                        os.close(fd)
                    except OSError:
                        pass
                rights = True
            elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS and len(payload) == struct.calcsize('3i'):
                credentials.append(struct.unpack('3i', payload))
            else:
                rights = True
        if rights or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or credentials != [self.peer]:
            self.invalid = True
            raise ValueError('invalid diagnostic frame')
        return data

    def bind(self, container_id, started, deadline):
        self.container_id = container_id
        self.guest['operation_start_ns'] = int(started * 1000000000)
        self.guest['operation_deadline_ns'] = int(deadline * 1000000000)
        if type(self.hard_deadline) is float and math.isfinite(self.hard_deadline) and self.hard_deadline > 0:
            self.guest['authority_deadline_ns'] = int(self.hard_deadline * 1000000000)
        if not self.armed or self.invalid:
            return
        body = canonical({'version': 1, 'purpose': self.purpose, 'container_id': container_id,
                          'nonce': self.nonce, 'operation_start_ns': self.guest['operation_start_ns'],
                          'operation_deadline_ns': self.guest['operation_deadline_ns'],
                          'authority_deadline_ns': self.guest['authority_deadline_ns'], 'principal_id': self.principal})
        try:
            if len(body) > 512 or self.sock.send(body) != len(body):
                self.invalid = True
        except OSError:
            self.invalid = True

    def collect(self):
        packets = []
        if self.armed and not self.invalid:
            try:
                now = time.monotonic()
                cutoff = min(now + .250, self.hard_deadline) if type(self.hard_deadline) is float and math.isfinite(self.hard_deadline) and self.hard_deadline > now else now
                while len(packets) < 4:
                    timeout = max(0, cutoff - time.monotonic())
                    if not select.select([self.sock], [], [], timeout)[0]:
                        break
                    raw = self._recv(4097)
                    if not raw:
                        break
                    if len(packets) == 3:
                        self.invalid = True
                        break
                    snapshot = source_settlement_evidence.decode_snapshot(raw)
                    if (snapshot['boot_id'] != self.boot or snapshot['epoch'] != self.epoch or
                            snapshot['owner_id'] != self.principal or
                            snapshot['container_id'] not in ('0' * 64, self.container_id)):
                        self.invalid = True
                        break
                    packets.append(base64.b64encode(raw).decode('ascii'))
                    if snapshot['phase'] == 3:
                        cutoff = time.monotonic()
            except (OSError, ValueError, TypeError):
                self.invalid = True
        collection = 'INVALID' if self.invalid else 'UNAVAILABLE' if not self.armed else 'EMPTY' if not packets else 'PARTIAL'
        value = {'purpose': self.purpose, 'nonce': self.nonce, 'collection': collection,
                 'guest': dict(self.guest), 'snapshots': [] if self.invalid else packets,
                 'reference_holder': 'UNRESOLVED'}
        if packets and not self.invalid:
            try:
                phases = [source_settlement_evidence.decode_snapshot(base64.b64decode(p)) for p in packets]
                if phases[0]['phase'] == 1 and phases[-1]['phase'] == 3 and all(not p['gap_flags'] & 128 and p['coverage'][28] == 0 for p in phases):
                    value['collection'] = 'COMPLETE'
                source_settlement_evidence.validate_evidence({'version': 1, 'probes': [value]})
            except (ValueError, TypeError):
                value.update(collection='INVALID', snapshots=[])
        return value

    def close(self):
        if self.sock is not None:
            self.sock.close()


def _owner_probe(daemon, authority, flag, source, *, observe_owner_start, settlement_evidence=None, evidence_channel=None):
    # Attach before start: the pinned log driver does not retain logs.
    from guest_postgres import _UnixHTTP
    import json
    import struct

    operation_start = time.monotonic()
    deadline = operation_start + 60
    owner_operation_deadline = deadline
    if evidence_channel is not None:
        try:
            evidence_channel.guest['operation_start_ns'] = int(operation_start * 1000000000)
            evidence_channel.guest['operation_deadline_ns'] = int(deadline * 1000000000)
            hard_deadline = evidence_channel.hard_deadline
            if type(hard_deadline) is float and math.isfinite(hard_deadline) and hard_deadline > 0:
                evidence_channel.guest['authority_deadline_ns'] = int(hard_deadline * 1000000000)
        except BaseException:
            pass
    config = {'Image': PG_ID, 'Entrypoint': ['/usr/bin/id'], 'Cmd': [flag, 'postgres'],
              'AttachStdout': True, 'AttachStderr': True, 'Tty': False,
              'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                             'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'],
                             'RestartPolicy': {'Name': 'no'}, 'LogConfig': {'Type': 'none', 'Config': {}},
                             'Mounts': [{'Type': 'bind', 'Source': OWNER_PROBE_SOURCE,
                                         'Target': OWNER_PROBE_TARGET, 'ReadOnly': True}]}}
    response_rejection = None

    def request(method, path, body=None):
        nonlocal response_rejection
        response_rejection = None
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            rejected = JournalError('PostgreSQL owner probe deadline exceeded')
            if observe_owner_start:
                response_rejection = (rejected, 'BEFORE_REQUEST_DEADLINE', None)
            raise rejected
        connection = _UnixHTTP(daemon, min(20, remaining))
        try:
            encoded = None if body is None else json.dumps(body, sort_keys=True, separators=(',', ':')).encode('ascii')
            api = '/v1.44' if method == 'POST' and path == '/containers/create' else '/v1.41'
            connection.request(method, api + path, body=encoded,
                               headers={'Content-Type': 'application/json'} if encoded is not None else {})
            response = connection.getresponse()
            data = response.read(65537)
            if time.monotonic() >= deadline:
                rejected = JournalError('PostgreSQL owner probe Docker operation failed')
                if observe_owner_start:
                    response_rejection = (rejected, 'RESPONSE_DEADLINE', None)
                raise rejected
            if len(data) > 65536:
                rejected = JournalError('PostgreSQL owner probe Docker operation failed')
                if observe_owner_start:
                    response_rejection = (rejected, 'RESPONSE_SIZE', None)
                raise rejected
            status = response.status
            if status not in (200, 201, 204):
                rejected = JournalError('PostgreSQL owner probe Docker operation failed')
                if (observe_owner_start and type(status) is int and status == 500 and method == 'POST' and
                        type(container_id) is str and path == '/containers/' + container_id + '/start'):
                    try:
                        rejected._g2_owner_probe_start_error = _owner_probe_start_error(data)
                    except BaseException:
                        pass
                if observe_owner_start:
                    response_rejection = (rejected, 'RESPONSE_STATUS', status if type(status) is int and 100 <= status <= 599 else None)
                raise rejected
            return strict_json(data) if data else None
        finally:
            connection.close()

    container_id = None
    admitted = deleted = False
    attach = None
    diagnostic = None
    settlement_diagnostic = None
    settlement_exception = None
    settlement_hard_deadline = None
    start_exception = None
    start_response = ('OTHER', None)
    try:
        source.check()
        created = request('POST', '/containers/create', config)
        container_id = created.get('Id') if type(created) is dict else None
        if type(container_id) is not str or re.fullmatch(r'[0-9a-f]{64}', container_id) is None:
            raise JournalError('PostgreSQL owner probe identity missing')
        inspected = request('GET', '/containers/' + container_id + '/json')
        mounts = inspected.get('Mounts') if type(inspected) is dict else None
        host = inspected.get('HostConfig') if type(inspected) is dict else None
        expected_mount = {'Type': 'bind', 'Source': OWNER_PROBE_SOURCE,
                          'Target': OWNER_PROBE_TARGET, 'ReadOnly': True}
        if (type(inspected) is not dict or inspected.get('Id') != container_id or inspected.get('Image') != PG_ID or
                type(host) is not dict or host.get('Binds') not in (None, []) or
                type(host.get('Mounts')) is not list or len(host['Mounts']) != 1 or
                type(host['Mounts'][0]) is not dict or host['Mounts'][0] != expected_mount or
                host['Mounts'][0].get('ReadOnly') is not True or
                type(mounts) is not list or len(mounts) != 1 or type(mounts[0]) is not dict or
                mounts[0].get('Type') != 'bind' or mounts[0].get('Source') != OWNER_PROBE_SOURCE or
                mounts[0].get('Destination') != OWNER_PROBE_TARGET or mounts[0].get('RW') is not False or
                mounts[0].get('Propagation', '') not in ('', 'rprivate') or
                mounts[0].get('Name') not in (None, '')):
            raise JournalError('PostgreSQL owner probe bind differs')
        source.check()
        if evidence_channel is not None:
            try:
                evidence_channel.bind(container_id, operation_start, owner_operation_deadline)
            except BaseException:
                pass
        authority.admit_container(config, container_id, 'postgres-uid' if flag == '-u' else 'postgres-gid')
        admitted = True
        attach = _UnixHTTP(daemon, min(20, deadline - time.monotonic()))
        attach.connect()
        sock = attach.sock
        sock.sendall(('POST /v1.41/containers/' + container_id +
                      '/attach?stream=1&stdout=1&stderr=1 HTTP/1.1\r\nHost: localhost\r\n'
                      'Connection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n').encode('ascii'))
        raw = bytearray()
        while b'\r\n\r\n' not in raw:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or len(raw) >= 8192:
                raise JournalError('PostgreSQL owner probe attach failed')
            sock.settimeout(min(20, remaining))
            chunk = sock.recv(8192 - len(raw))
            if not chunk:
                raise JournalError('PostgreSQL owner probe attach closed')
            raw.extend(chunk)
        headers, stream = bytes(raw).split(b'\r\n\r\n', 1)
        if headers.split(b'\r\n', 1)[0] not in (b'HTTP/1.1 101 UPGRADED', b'HTTP/1.1 101 Switching Protocols', b'HTTP/1.1 200 OK'):
            raise JournalError('PostgreSQL owner probe attach rejected')
        if observe_owner_start:
            try:
                diagnostic = _OwnerProbeStartDiagnostic(source, container_id)
            except BaseException:
                pass
        source.check()
        try:
            request('POST', '/containers/' + container_id + '/start')
        except BaseException as exc:
            if observe_owner_start:
                start_exception = exc
                if response_rejection is not None and response_rejection[0] is exc:
                    start_response = response_rejection[1:]
            raise
        if diagnostic is not None:
            diagnostic.close()
            diagnostic = None
        stream = bytearray(stream)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or len(stream) > 8192:
                raise JournalError('PostgreSQL owner probe output exceeded budget')
            sock.settimeout(min(20, remaining))
            chunk = sock.recv(8193 - len(stream))
            if not chunk:
                break
            stream.extend(chunk)
        output = bytearray()
        while stream:
            if len(stream) < 8 or stream[0] not in (1, 2) or stream[1:4] != b'\0\0\0':
                raise JournalError('PostgreSQL owner probe output framing invalid')
            channel, size = stream[0], struct.unpack('!I', stream[4:8])[0]
            if size > len(stream) - 8 or channel != 1:
                raise JournalError('PostgreSQL owner probe stderr or incomplete output')
            output.extend(stream[8:8 + size])
            del stream[:8 + size]
        waited = request('POST', '/containers/' + container_id + '/wait?condition=not-running')
        if type(waited) is not dict or type(waited.get('StatusCode')) is not int or waited['StatusCode'] != 0:
            raise JournalError('PostgreSQL owner probe exit invalid')
        observed = bytes(output).strip()
        if not observed.isdigit() or not 0 < int(observed) < (1 << 31) or int(observed) == 10002:
            raise JournalError('PostgreSQL image owner invalid')
        request('DELETE', '/containers/' + container_id + '?v=1')
        deleted = True
        try:
            if observe_owner_start and authority.role == 'BOOT_OWNER':
                settlement_diagnostic = _OwnerProbeSettlementDiagnostic(
                    source, container_id, 'postgres-uid' if flag == '-u' else 'postgres-gid')
        except BaseException:
            pass
        if settlement_diagnostic is not None:
            try:
                candidate_deadline = authority._deadline
                if type(candidate_deadline) is float and math.isfinite(candidate_deadline):
                    settlement_hard_deadline = candidate_deadline
            except BaseException:
                pass
        try:
            if authority.settle_container(container_id, 'completed', deadline) != 'retired':
                raise JournalError('PostgreSQL owner BPF settlement remains pending')
            if evidence_channel is not None:
                evidence_channel.guest['ack_received_ns'] = time.monotonic_ns()
        except BaseException as exc:
            settlement_exception = exc
            raise
        if settlement_diagnostic is not None:
            try:
                settlement_diagnostic.close()
            except BaseException:
                pass
            settlement_diagnostic = None
        source.check()
        return int(observed)
    finally:
        try:
            if attach is not None:
                attach.close()
            if container_id is not None and not deleted:
                deadline = max(deadline, time.monotonic() + 10)
                try:
                    request('DELETE', '/containers/' + container_id + '?force=1&v=1')
                    if admitted:
                        authority.settle_container(container_id, 'failed', deadline)
                except BaseException:
                    pass
        finally:
            try:
                if start_exception is not None:
                    collection, wrapper, broker, executed, mount_options, create_history = ('UNAVAILABLE', None, None, False, None, None)
                    if diagnostic is not None:
                        try:
                            collection, wrapper, broker, executed, mount_options, create_history = diagnostic.collect()
                        except BaseException:
                            collection, wrapper, broker, executed, mount_options, create_history = ('INVALID', None, None, False, None, None)
                    start_exception._g2_owner_probe_start_diagnostic = {
                        'purpose': 'postgres-uid' if flag == '-u' else 'postgres-gid',
                        'response': start_response[0], 'http_status': start_response[1],
                        'collection': collection, 'wrapper': wrapper, 'broker': broker,
                        'runc_exec_observed': executed, 'mount_options': mount_options, 'create_history': create_history}
            except BaseException:
                pass
            finally:
                if diagnostic is not None:
                    diagnostic.close()
                try:
                    if observe_owner_start and start_exception is not None and sys.exc_info()[1] is start_exception:
                        owner = _owner_probe_diagnostic_value(start_exception)
                        if _docker_runtime_info_eligible(owner):
                            start_exception._g2_docker_runtime_info_diagnostic = _observe_docker_runtime_info(daemon, owner_operation_deadline)
                except BaseException:
                    pass
                try:
                    if observe_owner_start and settlement_exception is not None and sys.exc_info()[1] is settlement_exception:
                        purpose = 'postgres-uid' if flag == '-u' else 'postgres-gid'
                        value = {'purpose': purpose, 'collection': 'UNAVAILABLE', 'started': False,
                                 'wait': None, 'ack_committed': False, 'ack_sent': False,
                                 'removal': None, 'followup': None}
                        if settlement_diagnostic is not None:
                            try:
                                value = settlement_diagnostic.collect(settlement_hard_deadline)
                            except BaseException:
                                value['collection'] = 'INVALID'
                        settlement_exception._g2_owner_probe_settlement_diagnostic = value
                except BaseException:
                    pass
                finally:
                    if settlement_diagnostic is not None:
                        try:
                            settlement_diagnostic.close()
                        except BaseException:
                            pass
                if evidence_channel is not None:
                    try:
                        try:
                            evidence = evidence_channel.collect()
                            source_settlement_evidence.validate_evidence({'version': 1, 'probes': [evidence]})
                        except BaseException:
                            evidence = {'purpose': evidence_channel.purpose, 'nonce': evidence_channel.nonce,
                                        'collection': 'INVALID', 'guest': dict(evidence_channel.guest),
                                        'snapshots': [], 'reference_holder': 'UNRESOLVED'}
                            source_settlement_evidence.validate_evidence({'version': 1, 'probes': [evidence]})
                        if settlement_evidence is not None:
                            settlement_evidence[:] = [item for item in settlement_evidence if item['purpose'] != evidence['purpose']]
                            settlement_evidence.append(evidence)
                        if settlement_exception is not None and sys.exc_info()[1] is settlement_exception:
                            settlement_exception._g2_settlement_evidence_v1 = evidence
                    except BaseException:
                        pass
                    finally:
                        try:
                            evidence_channel.close()
                        except BaseException:
                            pass


def _owner_probe_pair(daemon, authority, *, observe_owner_start, settlement_evidence=None):
    source = _OwnerProbeSource()
    try:
        owners = []
        for flag in ('-u', '-g'):
            channel = None
            if settlement_evidence is not None:
                try:
                    channel = _OwnerSettlementEvidenceChannel(authority, 'postgres-uid' if flag == '-u' else 'postgres-gid')
                except BaseException:
                    pass
            if settlement_evidence is None:
                owners.append(_owner_probe(daemon, authority, flag, source, observe_owner_start=observe_owner_start))
            else:
                owners.append(_owner_probe(daemon, authority, flag, source, observe_owner_start=observe_owner_start,
                                           settlement_evidence=settlement_evidence, evidence_channel=channel))
        source.remove()
        return owners
    finally:
        source.close()


def install_docker(expected, authority, *, observe_owner_start, settlement_evidence=None):
    # The inherited preflight checks the package pins and their entire local
    # dependency closure before dpkg is allowed to mutate the guest.
    if authority is None:
        raise JournalError('Docker BPF authority missing')
    carrier.package_preflight()
    if os.path.lexists('/usr/sbin/policy-rc.d') or os.path.lexists('/etc/docker/daemon.json'):
        raise JournalError('guest has preexisting Docker configuration')
    fd = os.open('/usr/sbin/policy-rc.d', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o755)
    try:
        if os.write(fd, b'#!/bin/sh\nexit 101\n') != 19:
            raise JournalError('service policy write failed')
        os.fsync(fd)
    finally:
        os.close(fd)
    command(['/usr/bin/systemctl', 'mask', 'docker.service', 'docker.socket', 'containerd.service'])
    command(['/usr/bin/dpkg', '-i', SEED + '/containerd.io.deb', SEED + '/docker-ce-cli.deb',
             SEED + '/docker-ce.deb'], timeout=300)
    for package, version in (('docker-ce', '5:29.8.2-1~ubuntu.24.04~noble'),
                             ('docker-ce-cli', '5:29.8.2-1~ubuntu.24.04~noble'),
                             ('containerd.io', '2.3.6-1~ubuntu.24.04~noble')):
        actual = command(['/usr/bin/dpkg-query', '-W', '-f=${db:Status-Status} ${Version} ${Architecture}', package]).stdout
        if actual != ('installed ' + version + ' amd64').encode():
            raise JournalError('installed Docker package identity differs')
    os.mkdir('/var/lib/g2/docker', 0o700)
    log_fd = os.open('/run/g2/dockerd.log', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    argv = ['/opt/g2/g2-bpf-authority', 'daemon-child']
    try:
        proc = subprocess.Popen(argv, env=ENV, stdin=subprocess.DEVNULL, stdout=log_fd, stderr=log_fd, close_fds=True)
    finally:
        os.close(log_fd)
    pidfd = None
    try:
        pidfd = os.pidfd_open(proc.pid, 0)
        start = process_start(proc.pid)
        deadline = time.monotonic() + 60
        while True:
            waited_pid, status = os.waitpid(proc.pid, os.WUNTRACED | os.WNOHANG)
            if waited_pid:
                if not os.WIFSTOPPED(status) or os.WSTOPSIG(status) != signal.SIGSTOP:
                    if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                        proc.returncode = os.waitstatus_to_exitcode(status)
                    raise JournalError('Docker helper did not stop for registration')
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise JournalError('Docker helper stop deadline exceeded')
            select.select([pidfd], [], [], min(0.1, remaining))
        if select.select([pidfd], [], [], 0)[0] or process_start(proc.pid) != start:
            raise JournalError('Docker helper identity changed before registration')
        authority.register_daemon(proc.pid, start, 'initial', pidfd)
        if select.select([pidfd], [], [], 0)[0] or process_start(proc.pid) != start:
            raise JournalError('Docker helper identity changed after registration')
        signal.pidfd_send_signal(pidfd, signal.SIGCONT)
        while not os.path.exists('/var/run/docker.sock'):
            if proc.poll() is not None or time.monotonic() >= deadline:
                raise JournalError('guest Docker daemon did not start')
            select.select([pidfd], [], [], min(0.1, deadline - time.monotonic()))
        info = os.stat('/var/run/docker.sock', follow_symlinks=False)
        if not stat.S_ISSOCK(info.st_mode) or info.st_uid != 0 or info.st_gid != 10002 or info.st_mode & 0o007:
            raise JournalError('guest Docker socket identity invalid')
        executable = os.stat('/proc/%d/exe' % proc.pid)
        installed = os.stat('/usr/bin/dockerd')
        if ((executable.st_dev, executable.st_ino) != (installed.st_dev, installed.st_ino) or
                select.select([pidfd], [], [], 0)[0] or process_start(proc.pid) != start):
            raise JournalError('guest Docker daemon executable differs')
        daemon = {'pid': proc.pid, 'starttime': start, 'socket_dev': info.st_dev, 'socket_ino': info.st_ino}
        command(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'image', 'load',
                 '--input', SEED + '/postgres-16.12.tar'], timeout=300)
        raw = command(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'image', 'inspect',
                       PG_ID, '--format', '{{json .}}']).stdout
        image = strict_json(raw)
        if (type(image) is not dict or image.get('Id') != PG_ID or image.get('Os') != 'linux' or
                image.get('Architecture') != 'amd64' or 'PG_MAJOR=16' not in (image.get('Config', {}).get('Env') or [])):
            raise JournalError('loaded PostgreSQL image differs')
        if image.get('Config', {}).get('Volumes') != {'/var/lib/postgresql/data': {}}:
            raise JournalError('PostgreSQL image default volume differs')
        browser = browser_selection(expected)
        command(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'image', 'load',
                 '--input', SEED + '/browser-image.tar'], timeout=300)
        raw = command(['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock', 'image', 'inspect',
                       browser['image_id'], '--format', '{{json .}}']).stdout
        browser_image = strict_json(raw)
        config = browser_image.get('Config') if type(browser_image) is dict else None
        if (type(config) is not dict or browser_image.get('Id') != browser['image_id'] or
                browser_image.get('Os') != 'linux' or browser_image.get('Architecture') != 'amd64' or
                config.get('User') != '10002:10002' or config.get('Entrypoint') != ['/opt/g2/browser/chrome-headless-shell'] or
                set(config.get('Env') or []) != {'LD_LIBRARY_PATH=/opt/g2/browser:/opt/g2/runtime/lib',
                                                 'HOME=/tmp', 'LANG=C', 'LC_ALL=C', 'PATH=/nonexistent'}):
            raise JournalError('loaded browser image metadata differs')
        if settlement_evidence is None:
            owner = _owner_probe_pair(daemon, authority, observe_owner_start=observe_owner_start)
        else:
            owner = _owner_probe_pair(daemon, authority, observe_owner_start=observe_owner_start,
                                      settlement_evidence=settlement_evidence)
        if proc.poll() is not None or process_start(proc.pid) != start:
            raise JournalError('guest Docker daemon changed')
        return proc, pidfd, {'pid': proc.pid, 'starttime': start, 'socket_dev': info.st_dev,
                             'socket_ino': info.st_ino, 'image_id': PG_ID, 'browser_image_id': browser['image_id'],
                             'browser_program_sha256': browser['program_sha256'],
                             'postgres_uid': owner[0], 'postgres_gid': owner[1]}
    except BaseException:
        if proc.poll() is None:
            try:
                if pidfd is not None:
                    signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                else:
                    proc.kill()
            except ProcessLookupError:
                pass
        try:
            proc.wait(timeout=10)
        finally:
            if pidfd is not None:
                os.close(pidfd)
        raise
