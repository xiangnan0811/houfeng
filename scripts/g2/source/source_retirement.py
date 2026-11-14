"""Ownership and durable retirement of specifically approved source artifacts."""
import hashlib
import subprocess
import errno
import ctypes
import os
import re
import secrets
import stat
from pathlib import Path


_POLICY = 'houfeng.source-retirement.v2'
_LIMIT = 4 * 1024 * 1024
_HEX = re.compile(r'[0-9a-f]{64}\Z')
_TMP = re.compile(r'\.retirement-[0-9a-f]{32}\.tmp\Z')
_DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
_FILE_FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC


def _sha(data):
    return hashlib.sha256(data).hexdigest()


def _parts(host, relative):
    host.require(type(relative) is str and relative and not relative.startswith('/') and
                 '\x00' not in relative and all(part not in ('', '.', '..') for part in relative.split('/')),
                 'unsafe retirement path')
    return relative.split('/')


def _dir_identity(info):
    return {'dev': info.st_dev, 'ino': info.st_ino, 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode)}


def _file_identity(info):
    return dict(_dir_identity(info), nlink=info.st_nlink, size=info.st_size,
                mtime_ns=info.st_mtime_ns, ctime_ns=info.st_ctime_ns,
                allocated_bytes=info.st_blocks * 512)


def _mount_id(fd):
    with open('/proc/self/fdinfo/%d' % fd, 'rb') as stream:
        values = [line.split(b':', 1)[1].strip() for line in stream if line.startswith(b'mnt_id:')]
    if len(values) != 1 or not values[0].isdigit():
        raise ValueError('mount identity unavailable')
    return int(values[0])


def _root(host):
    fd = os.open(host.RUN, _DIR_FLAGS)
    try:
        info = os.fstat(fd)
        named = os.stat(host.RUN, follow_symlinks=False)
        host.require(stat.S_ISDIR(info.st_mode) and _dir_identity(info) == _dir_identity(named) and
                     info.st_uid == os.geteuid(), 'run directory identity differs')
        return fd, info.st_dev, _mount_id(fd)
    except BaseException:
        os.close(fd)
        raise


def _directory(host, root, relative, dev, mount, expected=None):
    fd = os.dup(root)
    try:
        if relative:
            for part in _parts(host, relative):
                next_fd = os.open(part, _DIR_FLAGS, dir_fd=fd)
                try:
                    held = os.fstat(next_fd)
                    named = os.stat(part, dir_fd=fd, follow_symlinks=False)
                    host.require(stat.S_ISDIR(held.st_mode) and _dir_identity(held) == _dir_identity(named) and
                                 held.st_uid == os.geteuid() and held.st_dev == dev and
                                 _mount_id(next_fd) == mount, 'retirement directory changed or crossed mount')
                except BaseException:
                    os.close(next_fd)
                    raise
                os.close(fd)
                fd = next_fd
        if expected is not None:
            host.require(_dir_identity(os.fstat(fd)) == expected, 'retirement directory identity changed')
        return fd
    except BaseException:
        os.close(fd)
        raise


def _open_leaf(host, parent, name, dev, mount):
    host.require(len(_parts(host, name)) == 1, 'unsafe retirement leaf')
    fd = os.open(name, _FILE_FLAGS, dir_fd=parent)
    try:
        held = os.fstat(fd)
        named = os.stat(name, dir_fd=parent, follow_symlinks=False)
        host.require(stat.S_ISREG(held.st_mode) and _file_identity(held) == _file_identity(named) and
                     held.st_uid == os.geteuid() and held.st_nlink == 1 and held.st_dev == dev and
                     _mount_id(fd) == mount, 'retirement leaf identity differs')
        return fd
    except BaseException:
        os.close(fd)
        raise


def _hash_leaf(host, parent, name, dev, mount):
    fd = _open_leaf(host, parent, name, dev, mount)
    try:
        before = _file_identity(os.fstat(fd))
        digest = hashlib.sha256()
        count = 0
        while True:
            data = os.read(fd, 1024 * 1024)
            if not data:
                break
            count += len(data)
            digest.update(data)
        named = os.stat(name, dir_fd=parent, follow_symlinks=False)
        host.require(before == _file_identity(os.fstat(fd)) == _file_identity(named) and
                     count == before['size'], 'retirement input changed while hashing')
        return digest.hexdigest(), before
    finally:
        os.close(fd)


def _hash_at(host, root, relative, dev, mount):
    parts = _parts(host, relative)
    parent = _directory(host, root, '/'.join(parts[:-1]), dev, mount)
    try:
        return _hash_leaf(host, parent, parts[-1], dev, mount)[0]
    finally:
        os.close(parent)


def _read_record(host, directory, name, dev, mount):
    fd = _open_leaf(host, directory, name, dev, mount)
    try:
        before = _file_identity(os.fstat(fd))
        host.require(before['mode'] == 0o400 and 0 < before['size'] <= _LIMIT,
                     'retirement record permissions or size differ')
        data = bytearray()
        while len(data) <= _LIMIT:
            block = os.read(fd, min(65536, _LIMIT + 1 - len(data)))
            if not block:
                break
            data.extend(block)
        host.require(len(data) == before['size'] and before == _file_identity(os.fstat(fd)) ==
                     _file_identity(os.stat(name, dir_fd=directory, follow_symlinks=False)),
                     'retirement record changed while reading')
        return bytes(data)
    finally:
        os.close(fd)


def _publish(host, directory, name, data, dev, mount):
    host.require(len(_parts(host, name)) == 1 and 0 < len(data) <= _LIMIT, 'invalid retirement record')
    try:
        existing = _read_record(host, directory, name, dev, mount)
    except FileNotFoundError:
        existing = None
    if existing is not None:
        host.require(existing == data, 'conflicting retirement record')
        fd = _open_leaf(host, directory, name, dev, mount)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        os.fsync(directory)
        return
    temp = '.retirement-' + secrets.token_hex(16) + '.tmp'
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                 0o600, dir_fd=directory)
    try:
        view = memoryview(data)
        while view:
            written = os.write(fd, view)
            if written <= 0:
                raise OSError('short retirement record write')
            view = view[written:]
        os.fchmod(fd, 0o400)
        os.fsync(fd)
        held = os.fstat(fd)
        named = os.stat(temp, dir_fd=directory, follow_symlinks=False)
        host.require(stat.S_ISREG(held.st_mode) and held.st_nlink == 1 and held.st_uid == os.geteuid() and
                     _file_identity(held) == _file_identity(named), 'retirement temporary replaced')
        libc = ctypes.CDLL(None, use_errno=True)
        rename = libc.renameat2
        rename.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
        rename.restype = ctypes.c_int
        if rename(directory, os.fsencode(temp), directory, os.fsencode(name), 1) != 0:
            number = ctypes.get_errno()
            if number == errno.EEXIST:
                host.require(_read_record(host, directory, name, dev, mount) == data,
                             'conflicting retirement record')
                existing_fd = _open_leaf(host, directory, name, dev, mount)
                try:
                    os.fsync(existing_fd)
                finally:
                    os.close(existing_fd)
                os.fsync(directory)
                return
            raise OSError(number, os.strerror(number), name)
        os.fsync(directory)
    finally:
        os.close(fd)


_OBSERVER = frozenset((*range(4, 8), *range(10, 18), *range(19, 32), *range(33, 54)))
_NORMAL = frozenset(range(55, 63))
_BROKER = frozenset((*range(64, 76), *range(77, 99), 104, 105, *range(107, 112), *range(115, 144)))
_BUILDS = {2: 2, 3: 3, 8: 8, 9: 9, 32: 32, 99: 103, 100: 104, 101: 105,
           102: 106, 103: 107, 106: 110}
_PINS = frozenset((1, 18, 54, 63, 112, 113, 114))
_VM_FILES = ('boot.qcow2', 'app.raw', 'w1.raw', 'w2.raw', 'control.raw', 'seed.raw')
_SEED_ARCHIVES = frozenset(('browser-image.tar', 'postgres-16.12.tar', 'containerd.io.deb',
                            'docker-ce-cli.deb', 'docker-ce.deb'))


def _candidate_vm(index):
    if index in _OBSERVER:
        return 'realm-negative', 'observer-capability'
    if index in _NORMAL:
        return 'source-vps', 'normal'
    if index in _BROKER:
        return 'realm-negative', 'bpf-broker-kill'
    return None


def _raw_at(host, root, relative, dev, mount, limit=_LIMIT, mode=None):
    parts = _parts(host, relative)
    parent = _directory(host, root, '/'.join(parts[:-1]), dev, mount)
    try:
        fd = _open_leaf(host, parent, parts[-1], dev, mount)
        try:
            before = _file_identity(os.fstat(fd))
            host.require(before['size'] <= limit and (mode is None or before['mode'] == mode),
                         'retirement metadata size or mode differs')
            data = bytearray()
            while len(data) <= limit:
                block = os.read(fd, min(65536, limit + 1 - len(data)))
                if not block:
                    break
                data.extend(block)
            host.require(len(data) == before['size'] and before == _file_identity(os.fstat(fd)) ==
                         _file_identity(os.stat(parts[-1], dir_fd=parent, follow_symlinks=False)),
                         'retirement metadata changed while reading')
            return bytes(data)
        finally:
            os.close(fd)
    finally:
        os.close(parent)


def _selection(host, root, index, dev, mount):
    ident = '%020d' % index
    relative = 'source-build-selections/selection-' + ident + '.json'
    raw = _raw_at(host, root, relative, dev, mount, 4096, mode=0o400)
    selection = host.strict_json(raw)
    host.require(type(selection) is dict and set(selection) == {'version', 'run_token', 'selection_id',
                 'attempt_id', 'candidate_sha256', 'receipt_sha256', 'seed_sha256', 'binaries_sha256'} and
                 type(selection['version']) is int and selection['version'] == 1 and
                 host.canonical(selection) == raw and selection['run_token'] == host.TOKEN and
                 selection['selection_id'] == ident and type(selection['attempt_id']) is str and
                 re.fullmatch(r'[0-9]{20}', selection['attempt_id']) is not None and all(
                     type(selection[key]) is str and _HEX.fullmatch(selection[key]) is not None for key in
                     ('candidate_sha256', 'receipt_sha256', 'seed_sha256', 'binaries_sha256')),
                 'invalid retirement build selection')
    receipt_path = 'source-build-attempts/attempt-' + selection['attempt_id'] + '/receipt.json'
    receipt_raw = _raw_at(host, root, receipt_path, dev, mount)
    host.require(_sha(receipt_raw) == selection['receipt_sha256'], 'retirement build receipt changed')
    receipt = host.strict_json(receipt_raw)
    host.require(type(receipt) is dict and receipt.get('version') == 1 and
                 receipt.get('run_token') == host.TOKEN and receipt.get('attempt_id') == selection['attempt_id'] and
                 receipt.get('status') == 'PASS' and type(receipt.get('seed_inputs')) is dict and
                 type(receipt.get('binaries')) is dict and
                 _sha(host.canonical(receipt['seed_inputs'])) == selection['seed_sha256'] and
                 _sha(host.canonical(receipt['binaries'])) == selection['binaries_sha256'],
                 'retirement build receipt binding invalid')
    return dict(selection, selection_sha256=_sha(raw)), receipt_path, receipt


def _exited(host, attempt):
    host.require(type(attempt) is dict and type(attempt.get('pid')) is int and attempt['pid'] > 0 and
                 attempt.get('wait_observed') is True and attempt.get('pidfd_exited') is True and
                 attempt.get('exit_observed') is True and attempt.get('exit_uncertain') is False,
                 'retirement exit evidence incomplete')


def _vm_binding(host, root, index, dev, mount):
    case, scenario = _candidate_vm(index)
    ident = '%020d' % index
    attempt = 'runs/selection-' + ident + '/' + case + '/attempt-00000000000000000001'
    owner = attempt + '/' + scenario
    receipt_path = owner + '/receipt.json'
    raw = _raw_at(host, root, receipt_path, dev, mount)
    receipt = host.strict_json(raw)
    selection, build_receipt_path, _ = _selection(host, root, index, dev, mount)
    host.require(type(receipt) is dict and type(receipt.get('version')) is int and
                 receipt['version'] in (1, 2) and receipt.get('run_token') == host.TOKEN and
                 receipt.get('status') == 'FAIL' and receipt.get('case') == case and
                 receipt.get('scenario') == scenario and receipt.get('build_selection') == selection,
                 'retirement VM owner differs')
    launch = receipt.get('host_launch')
    process = receipt.get('attempt')
    _exited(host, process)
    if receipt.get('reboot_attempt') is not None:
        _exited(host, receipt['reboot_attempt'])
    host.require(type(launch) is dict and launch.get('run_token') == host.TOKEN and
                 launch.get('case') == case and launch.get('scenario') == scenario and
                 launch.get('build_selection') == selection and launch.get('disks') == receipt.get('disks_before') and
                 launch.get('seed_image') == receipt.get('seed_image') and
                 receipt.get('host_launch_sha256') == _sha(host.canonical(launch)) and
                 type(process.get('argv')) is list and all(type(x) is str for x in process['argv']) and
                 process.get('argv_sha256') == launch.get('argv_sha256') ==
                 _sha(host.canonical(process['argv'])), 'retirement host launch binding differs')
    if index in _BROKER:
        manifest_path = attempt + '/launch-manifest.json'
        manifest_raw = _raw_at(host, root, manifest_path, dev, mount, mode=0o400)
        manifest = host.strict_json(manifest_raw)
        host.require(type(manifest) is dict and set(manifest) == {'version', 'run_token', 'build_selection',
                     'attempt', 'case', 'scenarios', 'members'} and host.canonical(manifest) == manifest_raw and
                     type(manifest.get('version')) is int and manifest['version'] == 1 and
                     manifest.get('run_token') == host.TOKEN and
                     manifest.get('attempt') == '00000000000000000001' and
                     manifest.get('build_selection') == selection and manifest.get('case') == case and
                     manifest.get('scenarios') == [scenario] and
                     manifest.get('members') == [{'case': case, 'scenario': scenario, 'variant': None}] and
                     receipt.get('launch_manifest_sha256') == _sha(manifest_raw),
                     'retirement launch manifest binding differs')
        manifest_sha = _sha(manifest_raw)
        kind = 'launch_manifest'
    else:
        manifest_path = None
        manifest_sha = None
        kind = 'legacy_host_launch'
        host.require(receipt.get('launch_manifest_sha256') is None,
                     'legacy retirement owner unexpectedly declares manifest')
        attempt_fd = _directory(host, root, attempt, dev, mount)
        try:
            try:
                os.stat('launch-manifest.json', dir_fd=attempt_fd, follow_symlinks=False)
            except FileNotFoundError:
                pass
            else:
                raise host.Failure('legacy retirement owner has unexpected launch manifest')
        finally:
            os.close(attempt_fd)
    return {'kind': 'vm', 'binding_kind': kind, 'relative_directory': owner,
            'selection_id': ident, 'build_attempt_id': selection['attempt_id'],
            'case': case, 'scenario': scenario, 'attempt_id': '00000000000000000001',
            'receipt_sha256': _sha(raw), 'host_launch_sha256': receipt['host_launch_sha256'],
            'launch_manifest_sha256': manifest_sha}, receipt, (receipt_path, build_receipt_path, manifest_path)


def _validate_drives(host, owner, argv):
    index = int(owner['selection_id'])
    directory = host.RUN / owner['relative_directory']
    expected = {'boot': 'boot.qcow2', 'app': 'app.raw', 'w1': 'w1.raw', 'w2': 'w2.raw',
                'control': 'control.raw', 'seed': 'seed.raw'}
    if index > 7:
        expected['cidata'] = 'cidata.raw'
    drives = {}
    for position, token in enumerate(argv):
        if token != '-drive':
            continue
        host.require(position + 1 < len(argv), 'retirement argv drive is incomplete')
        fields = argv[position + 1].split(',')
        values = {}
        for field in fields:
            key, separator, value = field.partition('=')
            host.require(separator and key not in values, 'retirement argv drive field invalid')
            values[key] = value
        name = values.get('id')
        host.require(name in expected and name not in drives and values.get('file') == str(directory / expected[name]),
                     'retirement argv carrier path differs')
        drives[name] = values
    host.require(set(drives) == set(expected), 'retirement argv carrier set differs')
    if index <= 7:
        cidata = 'dir=' + str(directory / 'cidata')
        blockdevs = [argv[position + 1] for position, token in enumerate(argv[:-1]) if token == '-blockdev']
        host.require(sum(cidata in value.split(',') for value in blockdevs) == 1,
                     'retirement argv legacy CIDATA path differs')
    return drives


def _legacy_blocks(host, receipt):
    observations = receipt.get('carrier_observations')
    host.require(type(observations) is list, 'retirement CIDATA observations unavailable')
    expected = {'cidata-block-before_launch.raw': 'before_launch', 'cidata-block-after_wait.raw': 'after_wait'}
    blocks = {}
    for observation in observations:
        host.require(type(observation) is dict and type(observation.get('cidata_block')) is dict,
                     'retirement CIDATA snapshot unavailable')
        block = observation['cidata_block']
        name = block.get('path')
        host.require(name in expected and name not in blocks and observation.get('stage') == expected[name] and
                     type(block.get('size')) is int and block['size'] == 528482304,
                     'retirement CIDATA snapshot binding invalid')
        blocks[name] = block
    host.require(set(blocks) == set(expected), 'retirement CIDATA snapshots differ')
    return blocks


def _vm_leaves(host, root, owner, receipt, dev, mount):
    relative = owner['relative_directory']
    directory = _directory(host, root, relative, dev, mount)
    try:
        launch = receipt['host_launch']
        _validate_drives(host, owner, receipt['attempt']['argv'])
        declared = {'boot.qcow2': launch.get('boot'), 'seed.raw': launch.get('seed_image')}
        disks = launch.get('disks')
        host.require(type(disks) is dict, 'retirement disk identities unavailable')
        for name in ('app', 'w1', 'w2', 'control'):
            declared[name + '.raw'] = disks.get(name)
        if int(owner['selection_id']) <= 7:
            declared.update(_legacy_blocks(host, receipt))
        else:
            image = receipt.get('cidata_image')
            host.require(type(image) is dict and image.get('path') == 'cidata.raw',
                         'retirement CIDATA identity unavailable')
            declared['cidata.raw'] = image
        result = []
        for name, recorded in declared.items():
            host.require(type(recorded) is dict and type(recorded.get('dev')) is int and
                         type(recorded.get('ino')) is int, 'retirement carrier identity missing')
            fd = _open_leaf(host, directory, name, dev, mount)
            try:
                info = os.fstat(fd)
                host.require((info.st_dev, info.st_ino) == (recorded['dev'], recorded['ino']) and
                             (name == 'boot.qcow2' or
                              type(recorded.get('size')) is int and info.st_size == recorded['size']) and
                             ('uid' not in recorded or recorded['uid'] == info.st_uid) and
                             ('mode' not in recorded or recorded['mode'] == stat.S_IMODE(info.st_mode)),
                             'retirement carrier changed')
                result.append((relative + '/' + name, _file_identity(info)))
            finally:
                os.close(fd)
        return result
    finally:
        os.close(directory)


def _build_binding(host, root, selection_index, dev, mount):
    selection, receipt_path, receipt = _selection(host, root, selection_index, dev, mount)
    attempt = _BUILDS[selection_index]
    host.require(selection['attempt_id'] == '%020d' % attempt and type(receipt) is dict and
                 receipt.get('run_token') == host.TOKEN and receipt.get('attempt_id') == selection['attempt_id'] and
                 receipt.get('status') == 'PASS', 'retirement build owner differs')
    owner = {'kind': 'build', 'binding_kind': 'build',
             'relative_directory': 'source-build-attempts/attempt-' + selection['attempt_id'],
             'selection_id': '%020d' % selection_index, 'build_attempt_id': selection['attempt_id'],
             'case': None, 'scenario': None, 'attempt_id': None,
             'receipt_sha256': selection['receipt_sha256'], 'host_launch_sha256': None,
             'launch_manifest_sha256': None}
    return owner, receipt, receipt_path


def _leaf_exists(host, root, relative, dev, mount):
    parts = _parts(host, relative)
    try:
        parent = _directory(host, root, '/'.join(parts[:-1]), dev, mount)
    except FileNotFoundError:
        return False
    try:
        try:
            os.stat(parts[-1], dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            return False
        return True
    finally:
        os.close(parent)


def _build_leaves(host, root, owner, receipt, dev, mount):
    prefix = owner['relative_directory']
    seed = receipt.get('seed_inputs')
    binaries = receipt.get('binaries')
    host.require(type(seed) is dict and type(binaries) is dict and binaries,
                 'retirement build digest map unavailable')
    targets = {}
    for name in _SEED_ARCHIVES:
        if name in seed:
            targets['seed/' + name] = seed[name]
        else:
            host.require(not _leaf_exists(host, root, prefix + '/seed/' + name, dev, mount),
                         'retirement build seed digest missing')
    for name, digest in binaries.items():
        host.require(type(name) is str and re.fullmatch(r'[a-z0-9-]+', name) is not None and
                     seed.get(name) == digest, 'retirement binary digest differs')
        targets['seed/' + name] = digest
        targets['source-build/bin/' + name] = digest
    targets['source-build/c80.tar'] = receipt.get('c80_archive_sha256')
    boot = receipt.get('boot_inputs')
    if boot is not None:
        host.require(type(boot) is dict, 'retirement boot digest map invalid')
        for kind, path in (('kernel', 'boot/vmlinuz'), ('initrd', 'boot/initrd'),
                           ('original_initrd', 'boot/initrd.original')):
            host.require(boot.get(kind + '_path') == path, 'retirement boot path differs')
            targets[path] = boot.get(kind + '_sha256')
    else:
        for name in ('boot/vmlinuz', 'boot/initrd', 'boot/initrd.original'):
            host.require(not _leaf_exists(host, root, prefix + '/' + name, dev, mount),
                         'retirement build boot digest missing')
    result = []
    for relative, digest in sorted(targets.items()):
        host.require(type(digest) is str and _HEX.fullmatch(digest) is not None,
                     'retirement build digest missing')
        parts = _parts(host, prefix + '/' + relative)
        try:
            directory = _directory(host, root, '/'.join(parts[:-1]), dev, mount)
        except FileNotFoundError:
            continue
        try:
            try:
                actual, identity = _hash_leaf(host, directory, parts[-1], dev, mount)
            except FileNotFoundError:
                continue
            host.require(actual == digest, 'retirement build content differs from receipt')
            result.append(('/'.join(parts), identity))
        finally:
            os.close(directory)
    return result


_DIR_KEYS = frozenset(('dev', 'ino', 'uid', 'gid', 'mode'))
_FILE_KEYS = _DIR_KEYS | {'nlink', 'size', 'mtime_ns', 'ctime_ns', 'allocated_bytes'}
_OWNER_KEYS = frozenset(('kind', 'binding_kind', 'relative_directory', 'selection_id',
                         'build_attempt_id', 'case', 'scenario', 'attempt_id', 'receipt_sha256',
                         'host_launch_sha256', 'launch_manifest_sha256'))
_MANIFEST_KEYS = frozenset(('version', 'policy', 'run_token', 'run_directory', 'selected_at_plan',
                            'directories', 'owners', 'files', 'retained_metadata', 'excluded_owners',
                            'activity_disclosure'))


def _records_dir(host, root, dev, mount, create=False):
    try:
        fd = _directory(host, root, 'source-retirements', dev, mount)
    except FileNotFoundError:
        if not create:
            return None
        os.mkdir('source-retirements', 0o700, dir_fd=root)
        fd = _directory(host, root, 'source-retirements', dev, mount)
    try:
        host.require(stat.S_IMODE(os.fstat(fd).st_mode) == 0o700,
                     'retirement record directory is not private')
        if create:
            os.fsync(fd)
            os.fsync(root)
        return fd
    except BaseException:
        os.close(fd)
        raise


def _identity_schema(host, value, keys):
    host.require(type(value) is dict and set(value) == keys and all(
        type(value[key]) is int and value[key] >= 0 for key in keys), 'retirement identity schema invalid')
    host.require(value['uid'] == os.geteuid(), 'retirement identity owner differs')
    if keys == _FILE_KEYS:
        host.require(value['nlink'] == 1 and value['allocated_bytes'] % 512 == 0,
                     'retirement leaf link count or allocation invalid')


def _manifest_schema(host, manifest, root_info):
    host.require(type(manifest) is dict and set(manifest) == _MANIFEST_KEYS and
                 type(manifest['version']) is int and manifest['version'] == 1 and
                 manifest['policy'] == _POLICY and manifest['run_token'] == host.TOKEN and
                 type(manifest['run_directory']) is dict and set(manifest['run_directory']) == {'dev', 'ino'} and
                 all(type(manifest['run_directory'][key]) is int for key in ('dev', 'ino')) and
                 manifest['run_directory'] == {'dev': root_info.st_dev, 'ino': root_info.st_ino} and
                 type(manifest['selected_at_plan']) is dict and
                 all(type(manifest[key]) is list for key in
                     ('directories', 'owners', 'files', 'retained_metadata', 'excluded_owners')),
                 'retirement manifest schema invalid')
    disclosure = manifest['activity_disclosure']
    host.require(type(disclosure) is dict and set(disclosure) == {'inspected_processes', 'undetermined'} and
                 type(disclosure['inspected_processes']) is int and disclosure['inspected_processes'] >= 0 and
                 type(disclosure['undetermined']) is list and len(disclosure['undetermined']) <= 4096,
                 'retirement activity disclosure invalid')
    previous_pid = 0
    for item in disclosure['undetermined']:
        host.require(type(item) is dict and set(item) == {'pid', 'comm', 'reason', 'fields'} and
                     type(item['pid']) is int and item['pid'] > previous_pid and
                     type(item['comm']) is str and len(item['comm']) <= 64 and
                     type(item['reason']) is str and 0 < len(item['reason']) <= 128 and
                     type(item['fields']) is list and bool(item['fields']) and
                     all(type(field) is str and field in ('comm', 'ns/mnt', 'cmdline', 'cwd', 'maps', 'fd')
                         for field in item['fields']) and len(item['fields']) == len(set(item['fields'])),
                     'retirement activity disclosure entry invalid')
        previous_pid = item['pid']
    paths = set()
    for item in manifest['directories']:
        host.require(type(item) is dict and set(item) == {'path', 'identity'} and type(item['path']) is str,
                     'retirement directory schema invalid')
        if item['path']:
            _parts(host, item['path'])
        host.require(item['path'] not in paths, 'duplicate retirement directory')
        paths.add(item['path'])
        _identity_schema(host, item['identity'], _DIR_KEYS)
        host.require(item['identity']['dev'] == root_info.st_dev, 'retirement directory device differs')
    host.require('' in paths, 'retirement root identity missing')
    owners = set()
    for owner in manifest['owners']:
        host.require(type(owner) is dict and set(owner) == _OWNER_KEYS and
                     type(owner['selection_id']) is str and re.fullmatch(r'[0-9]{20}', owner['selection_id']) and
                     type(owner['build_attempt_id']) is str and re.fullmatch(r'[0-9]{20}', owner['build_attempt_id']) and
                     type(owner['receipt_sha256']) is str and _HEX.fullmatch(owner['receipt_sha256']),
                     'retirement owner schema invalid')
        _parts(host, owner['relative_directory'])
        host.require(owner['relative_directory'] not in owners, 'duplicate retirement owner')
        owners.add(owner['relative_directory'])
        index = int(owner['selection_id'])
        host.require(index not in _PINS, 'retirement owner is pinned')
        if owner['kind'] == 'vm':
            candidate = _candidate_vm(index)
            host.require(candidate is not None and (owner['case'], owner['scenario']) == candidate and
                         owner['attempt_id'] == '00000000000000000001' and
                         owner['binding_kind'] == ('launch_manifest' if index in _BROKER else 'legacy_host_launch') and
                         type(owner['host_launch_sha256']) is str and _HEX.fullmatch(owner['host_launch_sha256']) and
                         ((index in _BROKER and type(owner['launch_manifest_sha256']) is str and
                           _HEX.fullmatch(owner['launch_manifest_sha256'])) or
                          (index not in _BROKER and owner['launch_manifest_sha256'] is None)),
                         'retirement VM schema invalid')
        else:
            host.require(owner['kind'] == 'build' and owner['binding_kind'] == 'build' and
                         index in _BUILDS and int(owner['build_attempt_id']) == _BUILDS[index] and
                         all(owner[key] is None for key in ('case', 'scenario', 'attempt_id',
                                                            'host_launch_sha256', 'launch_manifest_sha256')),
                         'retirement build schema invalid')
    leaves = set()
    for item in manifest['files']:
        host.require(type(item) is dict and set(item) == {'path', 'owner', 'identity'} and
                     type(item['owner']) is int and 0 <= item['owner'] < len(manifest['owners']),
                     'retirement file schema invalid')
        _parts(host, item['path'])
        owner = manifest['owners'][item['owner']]
        host.require(item['path'].startswith(owner['relative_directory'] + '/') and item['path'] not in leaves,
                     'retirement leaf owner or uniqueness differs')
        leaves.add(item['path'])
        _identity_schema(host, item['identity'], _FILE_KEYS)
        host.require(item['identity']['dev'] == root_info.st_dev, 'retirement leaf device differs')
        parent = str(Path(item['path']).parent)
        host.require(parent in paths, 'retirement leaf parent is unrecorded')
    metadata = set()
    for item in manifest['retained_metadata']:
        host.require(type(item) is dict and set(item) == {'path', 'sha256'} and
                     type(item['sha256']) is str and _HEX.fullmatch(item['sha256']),
                     'retirement metadata schema invalid')
        _parts(host, item['path'])
        host.require(item['path'] not in metadata, 'duplicate retirement metadata')
        metadata.add(item['path'])
    excluded = set()
    for item in manifest['excluded_owners']:
        host.require(type(item) is dict and set(item) == {'path', 'reason'} and
                     type(item['reason']) is str and item['reason'], 'retirement exclusion invalid')
        _parts(host, item['path'])
        host.require(item['path'] not in excluded and item['path'] not in owners,
                     'duplicate or eligible retirement exclusion')
        excluded.add(item['path'])


def _validate_directories(host, root, manifest, dev, mount):
    recorded = {item['path']: item['identity'] for item in manifest['directories']}
    host.require(_dir_identity(os.fstat(root)) == recorded.get('') ==
                 _dir_identity(os.stat(host.RUN, follow_symlinks=False)), 'retirement run directory changed')
    for relative, identity in recorded.items():
        fd = _directory(host, root, relative, dev, mount, identity)
        os.close(fd)
        parts = relative.split('/') if relative else []
        for index in range(len(parts)):
            host.require('/'.join(parts[:index]) in recorded, 'retirement ancestor identity missing')


def _validate_metadata(host, root, manifest, dev, mount):
    for item in manifest['retained_metadata']:
        host.require(_hash_at(host, root, item['path'], dev, mount) == item['sha256'],
                     'retained retirement metadata changed')


def _owner_bindings(host, root, manifest, dev, mount):
    selected = manifest['selected_at_plan']
    host.require(type(selected.get('selection_id')) is str and
                 re.fullmatch(r'[0-9]{20}', selected['selection_id']) is not None,
                 'retirement selected build identity invalid')
    original, _, _ = _selection(host, root, int(selected['selection_id']), dev, mount)
    host.require(original == selected, 'retirement selected build binding changed')
    recorded_metadata = {item['path'] for item in manifest['retained_metadata']}
    for owner in manifest['owners']:
        index = int(owner['selection_id'])
        if owner['kind'] == 'vm':
            actual, _, _ = _vm_binding(host, root, index, dev, mount)
        else:
            actual, _, _ = _build_binding(host, root, index, dev, mount)
        host.require(actual == owner, 'retirement owner binding changed')
        host.require(_metadata_paths(host, owner) <= recorded_metadata,
                     'retirement retained metadata list incomplete')


def _validate_owner_files(host, root, manifest, dev, mount):
    for number, owner in enumerate(manifest['owners']):
        files = {item['path']: item['identity'] for item in manifest['files'] if item['owner'] == number}
        prefix = owner['relative_directory'] + '/'
        index = int(owner['selection_id'])
        if owner['kind'] == 'vm':
            _, receipt, _ = _vm_binding(host, root, index, dev, mount)
            launch = receipt['host_launch']
            declared = {'boot.qcow2': launch['boot'], 'seed.raw': launch['seed_image']}
            declared.update({name + '.raw': launch['disks'][name] for name in ('app', 'w1', 'w2', 'control')})
            if index <= 7:
                declared.update(_legacy_blocks(host, receipt))
            else:
                declared['cidata.raw'] = receipt.get('cidata_image')
            host.require(set(files) == {prefix + name for name in declared}, 'retirement VM leaf set differs')
            _validate_drives(host, owner, receipt['attempt']['argv'])
            for name, recorded in declared.items():
                identity = files[prefix + name]
                host.require(type(recorded) is dict and identity['dev'] == recorded.get('dev') and
                             identity['ino'] == recorded.get('ino') and
                             (name == 'boot.qcow2' or identity['size'] == recorded.get('size')) and
                             ('uid' not in recorded or identity['uid'] == recorded['uid']) and
                             ('mode' not in recorded or identity['mode'] == recorded['mode']),
                             'retirement VM leaf binding differs')
        else:
            _, receipt, _ = _build_binding(host, root, index, dev, mount)
            seed, binaries = receipt.get('seed_inputs'), receipt.get('binaries')
            host.require(type(seed) is dict and type(binaries) is dict and binaries,
                         'retirement build digests unavailable')
            allowed = {'seed/' + name for name in _SEED_ARCHIVES if name in seed}
            for name, digest in binaries.items():
                host.require(type(name) is str and re.fullmatch(r'[a-z0-9-]+', name) and
                             type(digest) is str and _HEX.fullmatch(digest) and seed.get(name) == digest,
                             'retirement binary digest invalid')
                allowed.update(('seed/' + name, 'source-build/bin/' + name))
            host.require(type(receipt.get('c80_archive_sha256')) is str and
                         _HEX.fullmatch(receipt['c80_archive_sha256']), 'retirement archive digest missing')
            allowed.add('source-build/c80.tar')
            boot = receipt.get('boot_inputs')
            if boot is not None:
                host.require(type(boot) is dict, 'retirement boot inputs invalid')
                for kind, path in (('kernel', 'boot/vmlinuz'), ('initrd', 'boot/initrd'),
                                   ('original_initrd', 'boot/initrd.original')):
                    host.require(boot.get(kind + '_path') == path and type(boot.get(kind + '_sha256')) is str and
                                 _HEX.fullmatch(boot[kind + '_sha256']), 'retirement boot digest invalid')
                    allowed.add(path)
            host.require(files and all(path.startswith(prefix) and path[len(prefix):] in allowed for path in files),
                         'retirement build leaf is outside whitelist')


def _read_records(host, root, dev, mount):
    directory = _records_dir(host, root, dev, mount)
    if directory is None:
        return {}
    try:
        names = os.listdir(directory)
        grouped = {}
        for name in names:
            if _TMP.fullmatch(name):
                fd = _open_leaf(host, directory, name, dev, mount)
                try:
                    host.require(stat.S_IMODE(os.fstat(fd).st_mode) in (0o400, 0o600),
                                 'retirement temporary permissions differ')
                finally:
                    os.close(fd)
                continue
            match = re.fullmatch(r'(manifest|intent|complete)-([0-9a-f]{64})\.json', name)
            host.require(match is not None, 'unknown retirement record')
            kind, digest = match.groups()
            raw = _read_record(host, directory, name, dev, mount)
            value = host.strict_json(raw)
            host.require(host.canonical(value) == raw, 'retirement record is not canonical')
            group = grouped.setdefault(digest, {})
            host.require(kind not in group, 'duplicate retirement record')
            group[kind] = value
            if kind == 'manifest':
                host.require(_sha(raw) == digest, 'retirement manifest hash differs')
        retired = set()
        for digest, group in grouped.items():
            host.require('manifest' in group and ('complete' not in group or 'intent' in group),
                         'orphan retirement record')
            manifest = group['manifest']
            _manifest_schema(host, manifest, os.fstat(root))
            _validate_directories(host, root, manifest, dev, mount)
            _validate_metadata(host, root, manifest, dev, mount)
            _owner_bindings(host, root, manifest, dev, mount)
            _validate_owner_files(host, root, manifest, dev, mount)
            if 'intent' in group:
                host.require(type(group['intent']) is dict and type(group['intent'].get('version')) is int and
                             group['intent'] == {'version': 1, 'run_token': host.TOKEN,
                                                'manifest_sha256': digest, 'status': 'RETIRING'},
                             'retirement intent invalid')
                for owner in manifest['owners']:
                    host.require(not any(path == owner['relative_directory'] or
                                         path.startswith(owner['relative_directory'] + '/') or
                                         owner['relative_directory'].startswith(path + '/') for path in retired),
                                 'conflicting retirement intents')
                    retired.add(owner['relative_directory'])
            if 'complete' in group:
                complete = group['complete']
                host.require(type(complete) is dict and set(complete) == {
                    'version', 'run_token', 'manifest_sha256', 'status', 'removed_charged_bytes',
                    'disk_available_before_bytes', 'disk_available_after_bytes'} and
                    type(complete['version']) is int and complete['version'] == 1 and complete['run_token'] == host.TOKEN and
                    complete['manifest_sha256'] == digest and complete['status'] == 'RETIRED' and
                    all(type(complete[key]) is int and complete[key] >= 0 for key in
                        ('removed_charged_bytes', 'disk_available_before_bytes', 'disk_available_after_bytes')) and
                    complete['removed_charged_bytes'] == sum(item['identity']['allocated_bytes']
                                                              for item in manifest['files']),
                    'retirement completion invalid')
        return grouped
    finally:
        os.close(directory)


def assert_available(host, path):
    path = Path(path)
    host.require(path.is_absolute() and '..' not in path.parts, 'unsafe retirement lookup path')
    try:
        relative = path.relative_to(host.RUN).as_posix()
    except ValueError as exc:
        raise host.Failure('retirement lookup outside run directory') from exc
    root, dev, mount = _root(host)
    try:
        for group in _read_records(host, root, dev, mount).values():
            if 'intent' not in group:
                continue
            for owner in group['manifest']['owners']:
                retired = owner['relative_directory']
                if relative in ('', '.') or relative == retired or relative.startswith(retired + '/') or retired.startswith(relative + '/'):
                    raise host.Failure('artifact_retired')
    finally:
        os.close(root)


def _references_build(value, attempt):
    if type(value) is dict:
        selected = value.get('build_selection')
        if type(selected) is dict and selected.get('attempt_id') == attempt:
            return True
        return any(_references_build(child, attempt) for child in value.values())
    if type(value) is list:
        return any(_references_build(child, attempt) for child in value)
    return False


def _build_unreferenced(host, root, owner, dev, mount):
    import source_build
    run_root = host.RUN / 'runs' / ('selection-' + owner['selection_id'])
    try:
        os.lstat(run_root)
    except FileNotFoundError:
        pass
    else:
        raise host.Failure('retirement build has a run root')
    selections = source_build._numbered(host, host.RUN / 'source-build-selections', 'selection-', '.json')
    for index in selections:
        selection, _, _ = _selection(host, root, index, dev, mount)
        host.require(index == int(owner['selection_id']) or selection['attempt_id'] != owner['build_attempt_id'],
                     'retirement build has another selection reference')
    run_directory = host.RUN / 'runs'
    if not run_directory.exists():
        return
    for path, dirs, files in os.walk(run_directory, followlinks=False, onerror=lambda e: (_ for _ in ()).throw(e)):
        for name in dirs:
            host.require(not (Path(path) / name).is_symlink(), 'symlink in retirement reference scan')
        relative_dir = Path(path).relative_to(host.RUN).as_posix()
        fd = _directory(host, root, relative_dir, dev, mount)
        os.close(fd)
        for name in files:
            if name not in ('receipt.json', 'launch-manifest.json', 'family.json'):
                continue
            relative = relative_dir + '/' + name
            value = host.strict_json(_raw_at(host, root, relative, dev, mount, 64 * _LIMIT))
            host.require(not _references_build(value, owner['build_attempt_id']),
                         'retirement build has a runtime reference')


def _lsof_exemptions(host):
    root = os.path.abspath(host.RUN)
    fd = os.open(root, _DIR_FLAGS)
    try:
        mount_id = _mount_id(fd)
    finally:
        os.close(fd)
    mounts = []
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        fields = line.split()
        separator = fields.index('-')
        path = re.sub(r'\\([0-7]{3})', lambda match: chr(int(match.group(1), 8)), fields[4])
        mounts.append((int(fields[0]), fields[2], path, fields[separator + 1]))
    targets = [row for row in mounts if row[0] == mount_id]
    host.require(len(targets) == 1, 'retirement root mount missing or ambiguous')
    device = targets[0][1]
    excluded = []
    for mid, dev, path, kind in mounts:
        if not ((kind == 'overlay' and path.startswith('/var/lib/docker/overlay2/') and
                 path.endswith('/merged')) or (kind == 'nsfs' and path.startswith('/run/docker/netns/'))):
            continue
        common = os.path.commonpath((root, path))
        host.require(mid != mount_id and dev != device and common not in (root, path),
                     'retirement lsof exemption intersects root mount')
        excluded.append(path)
    return sorted(set(excluded))


def _process_activity(host, owners, files):
    disclosure = {'inspected_processes': 0, 'undetermined': []}
    if not owners:
        return set(), disclosure
    paths = {str(host.RUN / owner['relative_directory']): number for number, owner in enumerate(owners)}
    identities = {(item['identity']['dev'], item['identity']['ino']): item['owner'] for item in files}
    active = set()
    self_namespace = os.readlink('/proc/self/ns/mnt')

    def mark_path(value):
        for number, owner in enumerate(owners):
            if str(host.RUN / owner['relative_directory']) in value or owner['relative_directory'] in value:
                active.add(number)

    with os.scandir('/proc') as processes:
        for entry in processes:
            if not entry.name.isdigit() or int(entry.name) == os.getpid():
                continue
            try:
                if entry.stat(follow_symlinks=False).st_uid != os.geteuid():
                    continue
                process = Path('/proc') / entry.name
                fields, reasons = set(), set()
                def denied(field):
                    fields.add(field)
                    reasons.add('permission_denied')
                def unavailable(field):
                    process.stat()
                    fields.add(field)
                    reasons.add('field_unavailable')
                try:
                    comm = (process / 'comm').read_text(errors='replace').strip()[:64]
                except PermissionError:
                    comm = ''
                    denied('comm')
                except (FileNotFoundError, ProcessLookupError):
                    comm = ''
                    unavailable('comm')
                try:
                    if os.readlink(process / 'ns/mnt') != self_namespace:
                        fields.add('ns/mnt')
                        reasons.add('foreign_mount_namespace')
                except PermissionError:
                    denied('ns/mnt')
                except (FileNotFoundError, ProcessLookupError):
                    unavailable('ns/mnt')
                for field in ('cmdline', 'cwd', 'maps'):
                    try:
                        value = (os.readlink(process / field) if field == 'cwd' else
                                 (process / field).read_bytes().decode('utf-8', 'surrogateescape'))
                        mark_path(value)
                    except PermissionError:
                        denied(field)
                    except (FileNotFoundError, ProcessLookupError):
                        unavailable(field)
                try:
                    with os.scandir(process / 'fd') as descriptors:
                        for descriptor in descriptors:
                            try:
                                held = os.stat(descriptor.path)
                                number = identities.get((held.st_dev, held.st_ino))
                                if number is not None:
                                    active.add(number)
                            except (FileNotFoundError, ProcessLookupError):
                                continue
                            except PermissionError:
                                denied('fd')
                            try:
                                mark_path(os.readlink(descriptor.path))
                            except (FileNotFoundError, ProcessLookupError):
                                continue
                            except PermissionError:
                                denied('fd')
                except PermissionError:
                    denied('fd')
                except (FileNotFoundError, ProcessLookupError):
                    unavailable('fd')
                disclosure['inspected_processes'] += 1
                if fields:
                    disclosure['undetermined'].append({'pid': int(entry.name), 'comm': comm,
                                                       'reason': '; '.join(sorted(reasons))[:128],
                                                       'fields': sorted(fields)})
            except (FileNotFoundError, ProcessLookupError):
                continue
    disclosure['undetermined'].sort(key=lambda item: item['pid'])
    host.require(len(disclosure['undetermined']) <= 4096, 'retirement activity disclosure exceeds limit')
    if files:
        exemptions = [arg for path in _lsof_exemptions(host) for arg in ('-e', path)]
        argv = (['/usr/bin/lsof', '-n', '-P', '-Fpn'] + exemptions + ['-f', '--'] +
                [str(host.RUN / item['path']) for item in files])
        result = subprocess.run(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)
        host.require(result.returncode in (0, 1) and not result.stderr, 'retirement lsof inspection failed')
        pid = None
        for raw in result.stdout.splitlines():
            line = os.fsdecode(raw)
            if line.startswith('p') and line[1:].isdigit():
                pid = int(line[1:])
            elif line.startswith('n') and pid != os.getpid():
                matches = ({number for path, number in paths.items() if path in line[1:]}
                           if pid is not None else set())
                if matches:
                    active.update(matches)
                else:
                    active.update(range(len(owners)))
    return active, disclosure



def _metadata_paths(host, owner):
    paths = {'source-build-selections/selection-' + owner['selection_id'] + '.json',
             'source-build-attempts/attempt-' + owner['build_attempt_id'] + '/receipt.json'}
    if owner['kind'] == 'vm':
        directory = host.RUN / owner['relative_directory']
        paths.add(owner['relative_directory'] + '/receipt.json')
        if owner['launch_manifest_sha256'] is not None:
            paths.add((directory.parent / 'launch-manifest.json').relative_to(host.RUN).as_posix())
        family = directory.parent / 'family.json'
        if family.exists():
            paths.add(family.relative_to(host.RUN).as_posix())
        with os.scandir(directory) as entries:
            for entry in entries:
                if entry.name.endswith(('.log', '.json', '.stderr', '.stdout')) and entry.is_file(follow_symlinks=False):
                    paths.add(Path(entry.path).relative_to(host.RUN).as_posix())
    return paths


def _manifest_directories(host, root, paths, dev, mount):
    names = {''}
    for relative in paths:
        parts = _parts(host, relative)
        for index in range(1, len(parts)):
            names.add('/'.join(parts[:index]))
    result = []
    for name in sorted(names):
        fd = _directory(host, root, name, dev, mount)
        try:
            result.append({'path': name, 'identity': _dir_identity(os.fstat(fd))})
        finally:
            os.close(fd)
    return result


def _available(host):
    info = os.statvfs(host.RUN)
    return info.f_bavail * info.f_frsize


def inventory(host, frozen):
    import source_build
    lock = source_build.lock_run(host)
    try:
        selected = source_build._selected(host, frozen)
        host.require(type(selected) is dict and type(selected.get('_selection')) is dict,
                     'retirement requires selected build')
        selected_at_plan = selected['_selection']
        root, dev, mount = _root(host)
        try:
            records = _read_records(host, root, dev, mount)
            retired = {owner['relative_directory'] for group in records.values() if 'intent' in group
                       for owner in group['manifest']['owners']}
            candidates, excluded = [], []
            specs = [(index, 'vm') for index in sorted(_OBSERVER | _NORMAL | _BROKER)] + [
                (index, 'build') for index in sorted(_BUILDS)]
            for index, kind in specs:
                if kind == 'vm':
                    case, scenario = _candidate_vm(index)
                    path = ('runs/selection-%020d/%s/attempt-00000000000000000001/%s' %
                            (index, case, scenario))
                else:
                    path = 'source-build-attempts/attempt-%020d' % _BUILDS[index]
                try:
                    host.require(index not in _PINS and index != int(selected_at_plan['selection_id']) and
                                 path not in retired, 'owner_pinned_or_retired')
                    if kind == 'vm':
                        owner, receipt, _ = _vm_binding(host, root, index, dev, mount)
                        try:
                            leaves = _vm_leaves(host, root, owner, receipt, dev, mount)
                        except FileNotFoundError as exc:
                            raise host.Failure('preexisting_artifact_missing') from exc
                    else:
                        owner, receipt, _ = _build_binding(host, root, index, dev, mount)
                        host.require(owner['build_attempt_id'] != selected_at_plan['attempt_id'],
                                     'owner_pinned_or_retired')
                        _build_unreferenced(host, root, owner, dev, mount)
                        leaves = _build_leaves(host, root, owner, receipt, dev, mount)
                    host.require(bool(leaves), 'owner_has_no_eligible_leaves')
                    candidates.append((owner, leaves))
                except (host.Failure, OSError, ValueError, KeyError, TypeError) as exc:
                    excluded.append({'path': path, 'reason': str(exc)[:256] or type(exc).__name__})
            owners = [item[0] for item in candidates]
            files = [{'path': path, 'owner': number, 'identity': identity}
                     for number, (_, leaves) in enumerate(candidates) for path, identity in leaves]
            disclosure = {'inspected_processes': 0, 'undetermined': []}
            try:
                active = _runtime_references(host, root, owners, dev, mount)
                process_active, disclosure = _process_activity(host, owners, files)
                active |= process_active
            except (host.Failure, OSError, ValueError, KeyError, TypeError, RecursionError,
                    subprocess.SubprocessError) as exc:
                active = set(range(len(owners)))
                activity_error = 'activity_inspection_failed: ' + str(exc)[:200]
            else:
                activity_error = 'active_reference'
            if active:
                old, candidates = candidates, []
                for number, item in enumerate(old):
                    if number in active:
                        excluded.append({'path': item[0]['relative_directory'], 'reason': activity_error})
                    else:
                        candidates.append(item)
                owners = [item[0] for item in candidates]
                files = [{'path': path, 'owner': number, 'identity': identity}
                         for number, (_, leaves) in enumerate(candidates) for path, identity in leaves]
            files.sort(key=lambda item: item['path'])
            if not files:
                return {'status': 'EMPTY', 'manifest_path': None, 'manifest_sha256': None,
                        'eligible_owners': 0, 'excluded_owners': excluded, 'files': [],
                        'charged_bytes': 0, 'disk_available_bytes': _available(host)}
            metadata_paths = set()
            for owner in owners:
                metadata_paths.update(_metadata_paths(host, owner))
            retained = [{'path': path, 'sha256': _hash_at(host, root, path, dev, mount)}
                        for path in sorted(metadata_paths)]
            directory_paths = [item['path'] for item in files] + list(metadata_paths)
            directory_paths += [owner['relative_directory'] + '/receipt.json' for owner in owners]
            directories = _manifest_directories(host, root, directory_paths, dev, mount)
            root_info = os.fstat(root)
            manifest = {'version': 1, 'policy': _POLICY, 'run_token': host.TOKEN,
                        'run_directory': {'dev': root_info.st_dev, 'ino': root_info.st_ino},
                        'selected_at_plan': selected_at_plan, 'directories': directories,
                        'owners': owners, 'files': files, 'retained_metadata': retained,
                        'excluded_owners': excluded, 'activity_disclosure': disclosure}
            _manifest_schema(host, manifest, root_info)
            _validate_owner_files(host, root, manifest, dev, mount)
            raw = host.canonical(manifest)
            host.require(len(raw) <= _LIMIT, 'retirement manifest exceeds limit')
            digest = _sha(raw)
            directory = _records_dir(host, root, dev, mount, create=True)
            try:
                _publish(host, directory, 'manifest-' + digest + '.json', raw, dev, mount)
            finally:
                os.close(directory)
            return {'manifest_path': str(host.RUN / 'source-retirements' / ('manifest-' + digest + '.json')),
                    'manifest_sha256': digest, 'eligible_owners': len(owners), 'excluded_owners': excluded,
                    'files': files, 'charged_bytes': sum(item['identity']['allocated_bytes'] for item in files),
                    'disk_available_bytes': _available(host)}
        finally:
            os.close(root)
    finally:
        os.close(lock)


def _checked_parent(host, root, relative, manifest, dev, mount):
    parts = _parts(host, relative)
    recorded = {item['path']: item['identity'] for item in manifest['directories']}
    host.require(_dir_identity(os.stat(host.RUN, follow_symlinks=False)) == recorded.get(''),
                 'retirement run root changed')
    for end in range(len(parts)):
        path = '/'.join(parts[:end])
        host.require(path in recorded, 'retirement ancestor unrecorded')
        fd = _directory(host, root, path, dev, mount, recorded[path])
        os.close(fd)
    parent = '/'.join(parts[:-1])
    return _directory(host, root, parent, dev, mount, recorded[parent]), parts[-1]


def _check_leaves(host, root, manifest, dev, mount, missing_allowed):
    present = []
    for item in manifest['files']:
        parent, name = _checked_parent(host, root, item['path'], manifest, dev, mount)
        try:
            try:
                fd = _open_leaf(host, parent, name, dev, mount)
            except FileNotFoundError:
                host.require(missing_allowed, 'retirement leaf missing before intent')
                continue
            try:
                host.require(_file_identity(os.fstat(fd)) == item['identity'], 'retirement leaf changed')
                present.append(item)
            finally:
                os.close(fd)
        finally:
            os.close(parent)
    return present


def _current_references(host, root, selected, manifest, dev, mount):
    for owner in manifest['owners']:
        host.require(owner['selection_id'] != selected['selection_id'], 'retirement owner is current selection')
        if owner['kind'] == 'build':
            host.require(owner['build_attempt_id'] != selected['attempt_id'], 'retirement build is current')
            _build_unreferenced(host, root, owner, dev, mount)


def _check_build_contents(host, root, manifest, present, dev, mount):
    for number, owner in enumerate(manifest['owners']):
        if owner['kind'] != 'build':
            continue
        _, receipt, _ = _build_binding(host, root, int(owner['selection_id']), dev, mount)
        actual = dict(_build_leaves(host, root, owner, receipt, dev, mount))
        expected = {item['path']: item['identity'] for item in present if item['owner'] == number}
        host.require(actual == expected, 'retirement build contents or leaf set changed')


def apply(host, frozen, manifest_sha256):
    import source_build
    host.require(type(manifest_sha256) is str and _HEX.fullmatch(manifest_sha256) is not None,
                 'invalid retirement manifest digest')
    lock = source_build.lock_run(host)
    try:
        selected = source_build._selected(host, frozen)
        host.require(type(selected) is dict and type(selected.get('_selection')) is dict,
                     'retirement requires selected build')
        root, dev, mount = _root(host)
        try:
            groups = _read_records(host, root, dev, mount)
            host.require(manifest_sha256 in groups, 'retirement manifest missing')
            group = groups[manifest_sha256]
            manifest = group['manifest']
            host.require(bool(manifest['files']) and bool(manifest['owners']), 'retirement manifest is empty')
            owned = {owner['relative_directory'] for owner in manifest['owners']}
            for digest, other in groups.items():
                if digest != manifest_sha256 and 'intent' in other:
                    for owner in other['manifest']['owners']:
                        path = owner['relative_directory']
                        host.require(not any(path == current or path.startswith(current + '/') or
                                             current.startswith(path + '/') for current in owned),
                                     'retirement owner already has another intent')
            recovering = 'intent' in group
            if not recovering:
                host.require(manifest['selected_at_plan'] == selected['_selection'],
                             'retirement selection changed after inventory')
            _current_references(host, root, selected['_selection'], manifest, dev, mount)
            host.require(not _runtime_references(host, root, manifest['owners'], dev, mount),
                         'retirement owner has another runtime reference')
            present = _check_leaves(host, root, manifest, dev, mount, recovering)
            _check_build_contents(host, root, manifest, present, dev, mount)
            active, disclosure = _process_activity(host, manifest['owners'], present)
            host.require(not active, 'retirement artifacts are active')
            directory = _records_dir(host, root, dev, mount, create=True)
            host.require(directory is not None, 'retirement record directory missing')
            try:
                if 'complete' in group:
                    host.require(not present, 'completed retirement has present leaf')
                    for item in manifest['files']:
                        parent, _ = _checked_parent(host, root, item['path'], manifest, dev, mount)
                        try:
                            os.fsync(parent)
                        finally:
                            os.close(parent)
                    _publish(host, directory, 'complete-' + manifest_sha256 + '.json',
                             host.canonical(group['complete']), dev, mount)
                    return dict(group['complete'], activity_disclosure=disclosure)
                before = _available(host)
                intent = {'version': 1, 'run_token': host.TOKEN, 'manifest_sha256': manifest_sha256,
                          'status': 'RETIRING'}
                _publish(host, directory, 'intent-' + manifest_sha256 + '.json', host.canonical(intent), dev, mount)
                for item in sorted(manifest['files'], key=lambda value: value['path']):
                    parent, name = _checked_parent(host, root, item['path'], manifest, dev, mount)
                    try:
                        try:
                            fd = _open_leaf(host, parent, name, dev, mount)
                        except FileNotFoundError:
                            host.require(recovering, 'retirement leaf disappeared after preflight')
                            os.fsync(parent)
                            continue
                        try:
                            host.require(_file_identity(os.fstat(fd)) == item['identity'] ==
                                         _file_identity(os.stat(name, dir_fd=parent, follow_symlinks=False)),
                                         'retirement leaf changed before unlink')
                            os.unlink(name, dir_fd=parent)
                            os.fsync(parent)
                        finally:
                            os.close(fd)
                    finally:
                        os.close(parent)
                _validate_directories(host, root, manifest, dev, mount)
                _validate_metadata(host, root, manifest, dev, mount)
                _owner_bindings(host, root, manifest, dev, mount)
                host.require(not _check_leaves(host, root, manifest, dev, mount, True),
                             'retirement leaves remain after unlink')
                complete = {'version': 1, 'run_token': host.TOKEN, 'manifest_sha256': manifest_sha256,
                            'status': 'RETIRED', 'removed_charged_bytes': sum(
                                item['identity']['allocated_bytes'] for item in manifest['files']),
                            'disk_available_before_bytes': before, 'disk_available_after_bytes': _available(host)}
                _publish(host, directory, 'complete-' + manifest_sha256 + '.json',
                         host.canonical(complete), dev, mount)
                return dict(complete, activity_disclosure=disclosure)
            finally:
                os.close(directory)
        finally:
            os.close(root)
    finally:
        os.close(lock)


def _runtime_references(host, root, owners, dev, mount):
    referenced = set()
    run_directory = host.RUN / 'runs'
    if not run_directory.exists() or not owners:
        return referenced
    owner_paths = [str(host.RUN / owner['relative_directory']) for owner in owners]
    for path, dirs, files in os.walk(run_directory, followlinks=False, onerror=lambda e: (_ for _ in ()).throw(e)):
        for name in dirs:
            host.require(not (Path(path) / name).is_symlink(), 'symlink in runtime reference scan')
        relative_dir = Path(path).relative_to(host.RUN).as_posix()
        fd = _directory(host, root, relative_dir, dev, mount)
        os.close(fd)
        for name in files:
            if name not in ('receipt.json', 'family.json', 'launch-manifest.json'):
                continue
            relative = relative_dir + '/' + name
            value = host.strict_json(_raw_at(host, root, relative, dev, mount, 64 * _LIMIT))
            host.require(type(value) is dict, 'runtime reference record invalid')
            if name == 'receipt.json':
                for field in ('attempt', 'reboot_attempt'):
                    process = value.get(field)
                    if process is None:
                        continue
                    host.require(type(process) is dict, 'runtime process reference invalid')
                    argv = process.get('argv')
                    if argv is None:
                        continue
                    host.require(type(argv) is list and all(type(arg) is str for arg in argv),
                                 'runtime argv reference invalid')
                    for number, target in enumerate(owner_paths):
                        if relative_dir == owners[number]['relative_directory']:
                            continue
                        if any(target in arg or owners[number]['relative_directory'] in arg for arg in argv):
                            referenced.add(number)
            elif name == 'launch-manifest.json':
                scenarios = value.get('scenarios')
                host.require(type(scenarios) is list and all(type(item) is str and
                             len(_parts(host, item)) == 1 for item in scenarios),
                             'runtime launch scenarios invalid')
                for scenario in scenarios:
                    declared_path = str(Path(path) / scenario)
                    for number, target in enumerate(owner_paths):
                        if declared_path == target and str(Path(target).parent) == path:
                            continue
                        if target == declared_path or target.startswith(declared_path + '/') or declared_path.startswith(target + '/'):
                            referenced.add(number)
            else:
                members = value.get('members')
                host.require(type(members) is list, 'runtime family members unavailable')
                base = Path(path).parent
                for member in members:
                    host.require(type(member) is dict and type(member.get('directory')) is str,
                                 'runtime family member path unavailable')
                    _parts(host, member['directory'])
                    member_path = str(base / member['directory'])
                    for number, target in enumerate(owner_paths):
                        if target == member_path or target.startswith(member_path + '/') or member_path.startswith(target + '/'):
                            referenced.add(number)
    return referenced
