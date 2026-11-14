"""Guest-root installation, guard custody and fixed one-shot preparer launcher.

This adapter is specific to the synthetic guest.  It never accepts an executable,
argv, environment, or filesystem path in a coordinator launch message.
"""
import base64
import array
import ctypes
import errno
import fcntl
import hashlib
import re
import os
import resource
import select
import signal
import socket
import struct
import stat
import time

import preparation

PREPARER_PATH = '/opt/g2/preparer'

COORDINATOR_UID = 10002
COORDINATOR_GID = 10002
CAPS = (2, 18, 19, 21)
CAP_MASK = sum(1 << bit for bit in CAPS)
CAPTURE_LIMIT = 16 * 1024
LAUNCH_DIAGNOSTIC_BYTES = 320


def _check(condition, message):
    if not condition:
        raise preparation.ProtocolError(message)


def _object(st):
    return {'dev': st.st_dev, 'ino': st.st_ino, 'mode': st.st_mode}


def _sha256_fd(fd):
    h = hashlib.sha256()
    offset = 0
    while True:
        data = os.pread(fd, 1024 * 1024, offset)
        if not data:
            return h.hexdigest()
        h.update(data)
        offset += len(data)


def _trusted_dir(fd, name):
    st = os.fstat(fd)
    _check(stat.S_ISDIR(st.st_mode) and st.st_uid == 0 and not st.st_mode & 0o022,
           'unsafe directory ' + name)


def _mount_for_path(path):
    matches = []
    with open('/proc/self/mountinfo', 'r', encoding='ascii') as stream:
        for line in stream:
            left, separator, right = line.rstrip('\n').partition(' - ')
            left, right = left.split(), right.split()
            _check(separator and len(left) >= 6 and len(right) == 3 and
                   re.fullmatch(r'[0-9]+', left[0]) is not None and int(left[0]) > 0,
                   'malformed mountinfo')
            mountpoint = left[4].replace('\\040', ' ').replace('\\011', '\t').replace('\\012', '\n').replace('\\134', '\\')
            _check(mountpoint.startswith('/'), 'malformed mountinfo')
            if path == mountpoint or path.startswith(mountpoint.rstrip('/') + '/'):
                matches.append((len(mountpoint), mountpoint, line.rstrip('\n'), left[5], right[2]))
    _check(matches, 'no mount for preparer executable')
    longest = max(item[0] for item in matches)
    chosen = [item for item in matches if item[0] == longest]
    _check(len(chosen) == 1, 'ambiguous executable mount')
    return chosen[0]


def _execution_mount(path, executable_fd):
    """Bind the selected path mount to the held executable's actual mount."""
    mount = _mount_for_path(path)
    with open('/proc/self/fdinfo/%d' % executable_fd, 'r', encoding='ascii') as stream:
        values = []
        for line in stream:
            name, separator, value = line.partition(':')
            if name == 'mnt_id' and separator:
                values.append(value.strip())
    _check(len(values) == 1 and re.fullmatch(r'[0-9]+', values[0]) is not None and
           int(values[0]) > 0, 'invalid executable fdinfo mount ID')
    mount_id = int(values[0])
    _check(mount_id == int(mount[2].split()[0]), 'executable mount ID mismatch')
    mount_flags = set(mount[3].split(',')) | set(mount[4].split(','))
    vfs_flags = os.fstatvfs(executable_fd).f_flag
    _check('noexec' not in mount_flags and not vfs_flags & os.ST_NOEXEC, 'preparer mount is noexec')
    return {'mountinfo': mount[2], 'mount_flags': sorted(mount_flags),
            'statvfs_flags': vfs_flags, 'mount_id': mount_id}


def _copy_preparer(source_fd, directory_fd, expected_sha256):
    """Copy to an exclusive leaf and transfer only its verified read-only FD."""
    preparation.hex_string(expected_sha256, 'seed preparer digest')
    src = os.fstat(source_fd)
    _check(stat.S_ISREG(src.st_mode) and src.st_nlink == 1 and not src.st_mode & 0o022,
           'unsafe seed preparer')
    output = executable = None
    try:
        output = os.open('preparer', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                         0o500, dir_fd=directory_fd)
        os.fchmod(output, 0o500)
        created = os.fstat(output)
        _check(stat.S_ISREG(created.st_mode) and stat.S_IMODE(created.st_mode) == 0o500 and
               created.st_nlink == 1, 'unsafe preparer copy')
        offset = 0
        while True:
            chunk = os.pread(source_fd, 1024 * 1024, offset)
            if not chunk:
                break
            view = memoryview(chunk)
            while view:
                written = os.write(output, view)
                _check(written > 0, 'short preparer copy')
                view = view[written:]
            offset += len(chunk)
        os.fsync(output)
        closing = output
        output = None
        os.close(closing)
        os.fsync(directory_fd)
        executable = os.open('preparer', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory_fd)
        dst = os.fstat(executable)
        path_st = os.stat('preparer', dir_fd=directory_fd, follow_symlinks=False)
        _check(stat.S_ISREG(dst.st_mode) and stat.S_ISREG(path_st.st_mode) and
               stat.S_IMODE(dst.st_mode) == 0o500 and dst.st_nlink == 1 and
               (dst.st_dev, dst.st_ino) == (path_st.st_dev, path_st.st_ino) ==
               (created.st_dev, created.st_ino) and
               dst.st_size == src.st_size == offset and _sha256_fd(executable) == expected_sha256,
               'installed preparer identity or digest mismatch')
        result = executable
        executable = None
        return result
    finally:
        for fd in (executable, output):
            if fd is not None:
                os.close(fd)


def install_preparer(seed, expected_sha256):
    """Install the verified seed at the fixed path and return an owned FD/receipt."""
    preparation.hex_string(expected_sha256, 'seed preparer digest')
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    root = os.open('/', flags)
    opt = directory = source = executable = None
    try:
        _trusted_dir(root, '/')
        opt = os.open('opt', flags, dir_fd=root)
        _trusted_dir(opt, '/opt')
        os.mkdir('g2', 0o700, dir_fd=opt)
        directory = os.open('g2', flags, dir_fd=opt)
        _trusted_dir(directory, '/opt/g2')
        held_dir = os.fstat(directory)
        path_dir = os.stat('g2', dir_fd=opt, follow_symlinks=False)
        _check(stat.S_ISDIR(path_dir.st_mode) and held_dir.st_uid == held_dir.st_gid == 0 and
               stat.S_IMODE(held_dir.st_mode) == 0o700 and
               (held_dir.st_dev, held_dir.st_ino) == (path_dir.st_dev, path_dir.st_ino),
               'unsafe preparer directory')
        os.fsync(directory)
        os.fsync(opt)
        source = os.open(os.path.join(seed, 'preparer'), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        src = os.fstat(source)
        _check(stat.S_ISREG(src.st_mode) and src.st_uid == src.st_gid == 0 and src.st_nlink == 1 and
               not src.st_mode & 0o022 and _sha256_fd(source) == expected_sha256,
               'unsafe seed preparer')
        executable = _copy_preparer(source, directory, expected_sha256)
        src_after = os.fstat(source)
        _check(_object(src_after) == _object(src) and src_after.st_uid == src_after.st_gid == 0 and
               src_after.st_nlink == 1 and src_after.st_size == src.st_size and
               _sha256_fd(source) == expected_sha256, 'seed preparer changed during copy')
        mount = _execution_mount(PREPARER_PATH, executable)
        _trusted_dir(opt, '/opt')
        path_opt = os.stat('opt', dir_fd=root, follow_symlinks=False)
        path_dir = os.stat('g2', dir_fd=opt, follow_symlinks=False)
        path_st = os.stat('preparer', dir_fd=directory, follow_symlinks=False)
        dst = os.fstat(executable)
        opt_st = os.fstat(opt)
        held_dir = os.fstat(directory)
        _check(stat.S_ISDIR(path_opt.st_mode) and
               (path_opt.st_dev, path_opt.st_ino) == (opt_st.st_dev, opt_st.st_ino) and
               stat.S_ISDIR(path_dir.st_mode) and held_dir.st_uid == held_dir.st_gid == 0 and
               stat.S_IMODE(held_dir.st_mode) == 0o700 and
               (path_dir.st_dev, path_dir.st_ino) == (held_dir.st_dev, held_dir.st_ino) and
               stat.S_ISREG(path_st.st_mode) and stat.S_ISREG(dst.st_mode) and
               dst.st_uid == dst.st_gid == 0 and stat.S_IMODE(dst.st_mode) == 0o500 and
               dst.st_nlink == 1 and (path_st.st_dev, path_st.st_ino) == (dst.st_dev, dst.st_ino) and
               dst.st_size == src.st_size and _sha256_fd(executable) == expected_sha256,
               'installed preparer identity or digest mismatch')
        receipt = {'seed_sha256': expected_sha256, 'copy_sha256': expected_sha256,
                   'seed': _object(src), 'copy': _object(dst), 'size': dst.st_size,
                   'path': PREPARER_PATH, **mount}
        result = executable
        executable = None
        return result, receipt
    finally:
        for fd in (executable, source, directory, opt, root):
            if fd is not None:
                os.close(fd)


def validate_guard_fd(fd, expected_guard, path, expected_uid):
    """Prove that fd is the locked OFD, using a separate competing open."""
    preparation.fields(expected_guard, ('dev', 'ino', 'uid', 'mode'), 'guard')
    for key in ('dev', 'ino', 'uid', 'mode'):
        preparation.integer(expected_guard[key], 'guard ' + key, 1 if key == 'ino' else 0)
    st = os.fstat(fd)
    named = os.stat(path, follow_symlinks=False)
    actual = {'dev': st.st_dev, 'ino': st.st_ino, 'uid': st.st_uid, 'mode': stat.S_IMODE(st.st_mode)}
    _check(stat.S_ISREG(st.st_mode) and st.st_nlink == 1 and
           preparation.canonical_json(actual) == preparation.canonical_json(expected_guard) and
           st.st_uid == expected_uid and stat.S_IMODE(st.st_mode) == 0o600 and
           (named.st_dev, named.st_ino, named.st_uid, stat.S_IMODE(named.st_mode)) ==
           (st.st_dev, st.st_ino, st.st_uid, stat.S_IMODE(st.st_mode)), 'guard identity changed')
    competing = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        other = os.fstat(competing)
        _check((other.st_dev, other.st_ino) == (st.st_dev, st.st_ino), 'guard path changed')
        try:
            fcntl.flock(competing, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            pass
        else:
            raise preparation.ProtocolError('guard was not held by a different OFD')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise preparation.ProtocolError('registered FD does not hold the guard OFD') from exc
    finally:
        os.close(competing)
    return actual


class GuardRegistration:
    """One retained guard.  Production supplies the fixed guest path/UID/peer."""
    def __init__(self, sock, peer_cred, run_token, session_nonce, path, expected_uid):
        self.sock = sock
        self.peer_cred = tuple(peer_cred)
        self.run_token = preparation.hex_string(run_token, 'run token', preparation.HEX32)
        self.session_nonce = preparation.hex_string(session_nonce, 'session nonce')
        self.path = path
        self.expected_uid = expected_uid
        self.fd = None
        self.registration = None
        self.accepted = False
        self.retired = False

    def receive(self):
        _check(self.fd is None and not self.accepted, 'duplicate guard registration')
        msg, fds = preparation.recv_frame(self.sock, self.peer_cred, 1)
        try:
            preparation.fields(msg, ('version', 'op', 'run_token', 'session_nonce', 'guard'), 'guard registration')
            _check(type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'guard_register' and
                   msg['run_token'] == self.run_token and msg['session_nonce'] == self.session_nonce,
                   'guard registration binding')
            validate_guard_fd(fds[0], msg['guard'], self.path, self.expected_uid)
            self.fd = fds[0]
            self.registration = msg
            return msg
        except BaseException:
            for fd in fds:
                os.close(fd)
            raise

    def accept(self, guard_owner):
        _check(self.fd is not None and not self.accepted, 'guard registration unavailable')
        preparation.fields(guard_owner, ('ready', 'status', 'groups', 'guard'), 'guard owner')
        _check(guard_owner['ready'] is False and guard_owner['guard'] == self.registration['guard'],
               'guard owner mismatch')
        validate_guard_fd(self.fd, guard_owner['guard'], self.path, self.expected_uid)
        reply = {'version': 1, 'op': 'guard_accepted', 'run_token': self.run_token,
                 'session_nonce': self.session_nonce, 'guard': guard_owner['guard']}
        preparation.send_frame(self.sock, reply)
        self.accepted = True
        return reply

    def close_after_exit(self, pidfd, wait_status, retired, no_active):
        _check(self.fd is not None and self.accepted and retired and no_active and
               bool(select.select([pidfd], [], [], 0)[0]) and os.WIFEXITED(wait_status) and
               os.WEXITSTATUS(wait_status) == 0, 'unsafe guard release')
        os.close(self.fd)
        self.fd = None
        self.retired = True


def _read_owned_json(directory_fd, name, expected_uid, canonical=False):
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory_fd)
    try:
        before = os.fstat(fd)
        _check(stat.S_ISREG(before.st_mode) and before.st_uid == expected_uid and before.st_nlink == 1 and
               stat.S_IMODE(before.st_mode) == 0o600 and before.st_size <= preparation.MAX_EVIDENCE,
               'unsafe retirement receipt file')
        chunks = []
        size = 0
        while True:
            chunk = os.read(fd, min(65536, preparation.MAX_EVIDENCE + 1 - size))
            if not chunk:
                break
            size += len(chunk)
            _check(size <= preparation.MAX_EVIDENCE, 'retirement receipt size')
            chunks.append(chunk)
        after = os.fstat(fd)
        named = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
        fingerprint = lambda st: (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_nlink, st.st_size,
                                  st.st_mtime_ns, st.st_ctime_ns)
        _check(fingerprint(before) == fingerprint(after) == fingerprint(named), 'retirement receipt changed')
        raw = b''.join(chunks)
        value = preparation.strict_json(raw)
        _check(type(value) is dict and (not canonical or raw == preparation.canonical_json(value) + b'\n'),
               'retirement receipt object or canonical bytes')
        return value
    finally:
        os.close(fd)


def validate_retired_ledger(control_dir, expected_uid, run_token, session_nonce, ledger_sha256, launches, exits):
    """Independently read all task-owned creation/retirement and preparer receipts."""
    preparation.hex_string(run_token, 'run token', preparation.HEX32)
    preparation.hex_string(session_nonce, 'session nonce')
    preparation.hex_string(ledger_sha256, 'retired ledger digest')
    try:
        directory = os.open(control_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except OSError as exc:
        raise preparation.ProtocolError('retirement receipt readback failed: ' + str(exc)) from exc
    try:
        ds = os.fstat(directory)
        _check(stat.S_ISDIR(ds.st_mode) and ds.st_uid == expected_uid and stat.S_IMODE(ds.st_mode) == 0o700,
               'unsafe retirement directory')
        names = os.listdir(directory)
        _check(len(names) <= 10000 and len(names) == len(set(names)), 'retirement directory bound')
        capture_files = {}
        for name in names:
            if name.startswith('capture-uncertain-'):
                raise preparation.ProtocolError('unresolved capture intent on disk')
            if name.startswith(('capture-intent-', 'capture-launch-', 'capture-exit-')):
                match = re.fullmatch(r'capture-(intent|launch|exit)-([0-9]{6})-([12])\.json', name)
                _check(match is not None, 'invalid capture receipt filename')
                key = (match[1], int(match[2]), int(match[3]))
                _check(key not in capture_files, 'duplicate capture receipt')
                capture_files[key] = _read_owned_json(directory, name, expected_uid, canonical=True)
        grouped = {prefix: {} for prefix in ('receipt-', 'retired-', 'pending-', 'failed-')}
        for name in names:
            for prefix in grouped:
                if name.startswith(prefix):
                    match = re.fullmatch(re.escape(prefix) + r'([0-9a-f]{64})\.json', name)
                    _check(match is not None, 'invalid retirement filename')
                    grouped[prefix][match[1]] = _read_owned_json(directory, name, expected_uid)
                    break
        outer = _read_owned_json(directory, 'coordinator-retired.json', expected_uid)
        preparation.fields(outer, ('ledger', 'sha256', 'session_nonce', 'run_token'), 'retirement ledger receipt')
        _check(outer['sha256'] == ledger_sha256 and outer['session_nonce'] == session_nonce and
               outer['run_token'] == run_token, 'retirement ledger identity')
        ledger = outer['ledger']
        preparation.fields(ledger, ('containers', 'preparers'), 'retirement ledger')
        _check(hashlib.sha256(preparation.canonical_json(ledger)).hexdigest() == ledger_sha256,
               'retirement ledger digest')
        _check(type(ledger['containers']) is list and type(ledger['preparers']) is list,
               'retirement ledger lists')
        retirements = {}
        for row in ledger['containers']:
            preparation.fields(row, ('id', 'remove', 'absent'), 'retired container')
            cid = preparation.hex_string(row['id'], 'retired container ID')
            _check(cid not in retirements and row['absent'] is True, 'duplicate or unconfirmed container retirement')
            removal = row['remove']
            preparation.fields(removal, ('argv', 'returncode', 'stdout', 'stderr'), 'container removal')
            _check(removal['argv'] == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'rm', cid] and
                   type(removal['returncode']) is int and removal['returncode'] == 0 and
                   type(removal['stdout']) is str and removal['stdout'].strip() == cid and
                   type(removal['stderr']) is str, 'container removal result')
            retirements[cid] = row
        _check(set(grouped['receipt-']) == set(grouped['retired-']) == set(retirements),
               'owned container retirement set mismatch')
        tokens = {}
        nonfixture = set()
        for cid, receipt in grouped['receipt-'].items():
            _check(type(receipt) is dict and set(receipt) in ({'id', 'spec', 'inspect'},
                                                               {'id', 'spec', 'inspect', 'origin'}),
                   'owned receipt fields')
            _check(receipt['id'] == cid and (('origin' not in receipt) or receipt['origin'] == 'root_fixture'),
                   'owned receipt identity')
            spec = receipt['spec']
            preparation.fields(spec, ('name', 'image', 'options', 'args', 'token', 'labels', 'environment'),
                               'owned creation spec')
            token = preparation.hex_string(spec['token'], 'create token')
            _check(token not in tokens and type(spec['name']) is str and spec['name'].endswith('-' + token) and
                   type(spec['image']) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', spec['image']) and
                   all(type(spec[key]) is list and all(type(x) is str for x in spec[key])
                       for key in ('options', 'args', 'labels', 'environment')), 'owned creation spec identity')
            labels = ['--label', 'owner=g2-isolated-capability', '--label', 'run=' + run_token,
                      '--label', 'runner=guest.py', '--label', 'create-token=' + token]
            _check(spec['labels'] == labels, 'owned creation labels')
            inspect = receipt['inspect']
            _check(type(inspect) is dict and inspect.get('Id') == cid and inspect.get('Name') == '/' + spec['name'] and
                   inspect.get('Image') == spec['image'] and type(inspect.get('Config')) is dict and
                   inspect['Config'].get('Labels') == dict(pair.split('=', 1) for pair in labels[1::2]) and
                   inspect['Config'].get('Entrypoint') == ['/probe'] and inspect['Config'].get('Cmd') == spec['args'] and
                   inspect['Config'].get('User') == '0:0', 'owned inspect identity')
            _check(preparation.canonical_json(grouped['retired-'][cid]) == preparation.canonical_json(retirements[cid]),
                   'durable container retirement mismatch')
            tokens[token] = spec
            if 'origin' not in receipt:
                nonfixture.add(token)
        pending = grouped['pending-']
        failed = grouped['failed-']
        _check(set(pending) == nonfixture | set(failed) and not (set(failed) & set(tokens)),
               'unresolved or conflicting create intent')
        for token, intent in pending.items():
            preparation.fields(intent, ('status', 'spec', 'argv'), 'pending creation')
            spec = intent['spec']
            preparation.fields(spec, ('name', 'image', 'options', 'args', 'token', 'labels', 'environment'),
                               'pending creation spec')
            _check(intent['status'] == 'PENDING' and spec['token'] == token and
                   type(spec['name']) is str and spec['name'].endswith('-' + token) and
                   type(spec['image']) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', spec['image']) and
                   all(type(spec[key]) is list and all(type(x) is str for x in spec[key])
                       for key in ('labels', 'environment', 'options', 'args')) and
                   spec['labels'] == ['--label', 'owner=g2-isolated-capability', '--label', 'run=' + run_token,
                                      '--label', 'runner=guest.py', '--label', 'create-token=' + token],
                   'pending creation identity')
            argv = ['create', '--name', spec['name']] + spec['labels'] + spec['environment'] + spec['options'] + \
                   ['--entrypoint', '/probe', spec['image']] + spec['args']
            _check(intent['argv'] == argv, 'pending creation command')
            if token in nonfixture:
                _check(preparation.canonical_json(spec) == preparation.canonical_json(tokens[token]),
                       'creation intent does not match receipt')
            else:
                failure = failed[token]
                preparation.fields(failure, ('result', 'name_absent'), 'failed creation')
                preparation.fields(failure['result'], ('argv', 'returncode', 'stdout', 'stderr'), 'failed create result')
                _check(failure['name_absent'] is True and type(failure['result']['returncode']) is int and
                       failure['result']['returncode'] != 0 and failure['result']['argv'] ==
                       ['/usr/bin/docker', '--host=unix:///var/run/docker.sock'] + argv and
                       type(failure['result']['stdout']) is str and type(failure['result']['stderr']) is str,
                       'failed create absence not proven')
        _check(type(launches) is list and type(exits) is list and
               len(ledger['preparers']) == len(launches) == len(exits), 'preparer retirement count')
        expected_capture_files = {}
        for index, row in enumerate(ledger['preparers']):
            preparation.fields(row, ('request', 'launch', 'exit', 'captures'), 'preparer retirement')
            request = row['request']
            preparation.fields(request, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                                         'request_digest', 'prepare_request'), 'preparer launch request')
            launch, exited = launches[index], exits[index]
            _check(preparation.canonical_json(row['launch']) == preparation.canonical_json(launch) and
                   preparation.canonical_json(row['exit']) == preparation.canonical_json(exited),
                   'preparer receipt does not match root wait')
            _check(type(request['version']) is int and request['version'] == 1 and request['op'] == 'launch_prepare' and
                   request['run_token'] == run_token and request['session_nonce'] == session_nonce and
                   type(request['ordinal']) is int and request['ordinal'] == index + 1 and
                   all(request[key] == launch[key] for key in ('run_token', 'session_nonce', 'ordinal', 'nonce',
                                                                'request_digest')) and
                   all(exited[key] == launch[key] for key in ('run_token', 'session_nonce', 'ordinal', 'nonce', 'pid')),
                   'preparer retirement binding')
            preparation.validate_request(request['prepare_request'])
            _check(request['prepare_request']['request_digest'] == request['request_digest'] and
                   type(row['captures']) is list and preparation.canonical_json(row['captures']) ==
                   preparation.canonical_json(exited['captures']) and
                   len(row['captures']) <= 2, 'preparer capture ledger mismatch')
            preparation.validate_capture_ledger(row['captures'], request['prepare_request'], session_nonce,
                                                preparer_exe=launch['exe'], root_pid=os.getpid())
            for sequence, record in enumerate(row['captures'], 1):
                preparation.fields(record, ('intent', 'intent_sha256', 'launch', 'exit'), 'capture ledger record')
                intent, child_launch, child_exit = record['intent'], record['launch'], record['exit']
                _check(type(intent) is dict and child_launch is not None and child_exit is not None and
                       hashlib.sha256(preparation.canonical_json(intent)).hexdigest() == record['intent_sha256'],
                       'unresolved or invalid durable capture')
                binding = intent['binding']
                _check(binding['ordinal'] == index + 1 and binding['sequence'] == sequence and
                       binding['phase'] == ('before' if sequence == 1 else 'after') and
                       binding['request_digest'] == request['request_digest'], 'durable capture binding')
                key = (index + 1, sequence)
                expected_capture_files[('intent', *key)] = intent
                expected_capture_files[('launch', *key)] = {'version': 1, 'op': 'capture_launch',
                                                            'binding': binding, 'launch': child_launch}
                expected_capture_files[('exit', *key)] = {'version': 1, 'op': 'capture_exit',
                                                          'binding': binding, 'exit': child_exit}
        _check(set(capture_files) == set(expected_capture_files) and all(
            preparation.canonical_json(capture_files[key]) == preparation.canonical_json(value)
            for key, value in expected_capture_files.items()), 'missing, orphaned or changed durable capture receipt')
        return {'ledger_sha256': ledger_sha256, 'containers': sorted(retirements),
                'preparers': len(launches), 'pending_resolved': len(pending)}
    except preparation.ProtocolError:
        raise
    except (OSError, TypeError, KeyError, IndexError, ValueError) as exc:
        raise preparation.ProtocolError('retirement receipt readback failed: ' + str(exc)) from exc
    finally:
        os.close(directory)


class _CapHeader(ctypes.Structure):
    _fields_ = [('version', ctypes.c_uint32), ('pid', ctypes.c_int)]


class _CapData(ctypes.Structure):
    _fields_ = [('effective', ctypes.c_uint32), ('permitted', ctypes.c_uint32),
                ('inheritable', ctypes.c_uint32)]


_LIBC = ctypes.CDLL(None, use_errno=True)
_LIBC.setns.argtypes = (ctypes.c_int, ctypes.c_int)
_LIBC.setns.restype = ctypes.c_int
_LIBC.prctl.argtypes = (ctypes.c_int, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong)
_LIBC.prctl.restype = ctypes.c_int
_LIBC.syscall.restype = ctypes.c_long

CLONE_NEWNS = 0x00020000
CLONE_NEWPID = 0x20000000
CLONE_NEWCGROUP = 0x02000000
NS_GET_USERNS = 0xB701
NS_GET_NSTYPE = 0xB703


def _ns_identity(fd):
    st = os.fstat(fd)
    return {'dev': st.st_dev, 'ino': st.st_ino}


def _namespace(fd, expected_type, owner):
    kind = fcntl.ioctl(fd, NS_GET_NSTYPE)
    userfd = fcntl.ioctl(fd, NS_GET_USERNS)
    try:
        os.set_inheritable(userfd, False)
        user = _ns_identity(userfd)
    finally:
        os.close(userfd)
    _check(kind == expected_type and user == owner, 'namespace type or owner mismatch')
    return {**_ns_identity(fd), 'nstype': kind, 'userns': user}


def _open_namespace(procfd, name):
    return os.open('ns/' + name, os.O_RDONLY | os.O_CLOEXEC, dir_fd=procfd)


def _read_bounded(path, limit=64 * 1024):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC)
    try:
        data = bytearray()
        while len(data) <= limit:
            part = os.read(fd, min(65536, limit + 1 - len(data)))
            if not part:
                break
            data.extend(part)
        _check(len(data) <= limit, 'proc evidence exceeds bound')
        return bytes(data).decode('ascii')
    finally:
        os.close(fd)


def _proc_start(path, expected_pid):
    text = _read_bounded(path)
    left, right = text.find('('), text.rfind(')')
    _check(left > 0 and right > left and text[:left].strip().isdigit(), 'malformed proc stat')
    parts = text[right + 1:].split()
    _check(int(text[:left].strip()) == expected_pid and len(parts) >= 20 and parts[19].isdigit() and
           int(parts[19]) > 0, 'proc starttime mismatch')
    return int(parts[19])


def _tasks(pid):
    names = os.listdir('/proc/%d/task' % pid)
    _check(names and len(names) <= 1024 and all(re.fullmatch(r'[1-9][0-9]*', name) for name in names),
           'invalid task inventory')
    return sorted(int(name) for name in names)


def _setns(fd, kind):
    if _LIBC.setns(fd, kind) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))


class _SigSet(ctypes.Structure):
    _fields_ = [('bits', ctypes.c_ulong * 16)]


class _SigAction(ctypes.Structure):
    _fields_ = [('handler', ctypes.c_void_p), ('mask', _SigSet), ('flags', ctypes.c_int),
                ('restorer', ctypes.c_void_p)]


_LIBC.sigaction.argtypes = (ctypes.c_int, ctypes.POINTER(_SigAction), ctypes.POINTER(_SigAction))
_LIBC.sigaction.restype = ctypes.c_int


def _check_reaper():
    action = _SigAction()
    if _LIBC.sigaction(signal.SIGCHLD, None, ctypes.byref(action)) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))
    _check(not action.handler and not action.flags & 2, 'SIGCHLD is not directly waitable')


def _check_no_file_capability(fd):
    st = os.fstat(fd)
    _check(stat.S_ISREG(st.st_mode) and not st.st_mode & (stat.S_ISUID | stat.S_ISGID),
           'unsafe executable mode')
    try:
        os.getxattr(fd, 'security.capability')
    except OSError as exc:
        if exc.errno not in (errno.ENODATA, errno.ENOTSUP, errno.EOPNOTSUPP):
            raise
    else:
        raise preparation.ProtocolError('executable has file capabilities')


def _prctl(option, arg2=0, arg3=0, arg4=0, arg5=0):
    if _LIBC.prctl(option, arg2, arg3, arg4, arg5) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))


def _drop_capabilities(last_cap):
    _prctl(47, 4, 0, 0, 0)  # PR_CAP_AMBIENT_CLEAR_ALL
    for cap in range(last_cap + 1):
        if cap not in CAPS:
            _prctl(24, cap, 0, 0, 0)  # PR_CAPBSET_DROP, while SETPCAP is still effective
    _prctl(38, 1, 0, 0, 0)  # PR_SET_NO_NEW_PRIVS
    header = _CapHeader(0x20080522, 0)
    data = (_CapData * 2)()
    data[0].effective = CAP_MASK
    data[0].permitted = CAP_MASK
    if _LIBC.capset(ctypes.byref(header), ctypes.byref(data)) != 0:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))


def _copy_high(fd):
    return fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 20)


def _diagnostic_error(exc):
    return '%s:%s' % (type(exc).__name__[:48], getattr(exc, 'errno', None))


def _launch_diagnostics(pidfd, capture, expected, observation):
    """Snapshot failure evidence only; none of these later reads authorize launch."""
    if pidfd is None:
        pidfd_now = 'unavailable'
    else:
        try:
            pidfd_now = bool(select.select([pidfd], [], [], 0)[0])
        except BaseException as exc:
            pidfd_now = 'error:' + _diagnostic_error(exc)
    waitid = 'not_exited'
    if pidfd_now is True:
        try:
            result = os.waitid(os.P_PIDFD, pidfd, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            waitid = ('no_status' if result is None else
                      '(si_pid=%s,si_code=%s,si_status=%s)' %
                      (result.si_pid, result.si_code, result.si_status))
        except BaseException as exc:
            waitid = 'error:' + _diagnostic_error(exc)
    elif pidfd_now is not False:
        waitid = 'unavailable'
    try:
        raw = os.pread(capture, LAUNCH_DIAGNOSTIC_BYTES + 1, 0)
        capture_b64 = base64.b64encode(raw[:LAUNCH_DIAGNOSTIC_BYTES]).decode('ascii')
        capture_truncated = len(raw) > LAUNCH_DIAGNOSTIC_BYTES
    except BaseException as exc:
        capture_b64 = 'error:' + _diagnostic_error(exc)
        capture_truncated = None
    pipe = observation['pipe']
    pipe_b64 = ('unobserved' if pipe is None else
                base64.b64encode(pipe[:LAUNCH_DIAGNOSTIC_BYTES]).decode('ascii'))
    expected_tuple = (expected['dev'], expected['ino'], expected['mode'])
    actual = observation['actual']
    actual_tuple = None if actual is None else (actual['dev'], actual['ino'], actual['mode'])
    return ('launch_diagnostic stage=%s pid=%s expected=%s observed=%s stat_errno=%s '
            'pidfd_ack=%s pidfd_check=%s pidfd_now=%s waitid=%s '
            'pipe_b64=%s pipe_truncated=%s pipe_errno=%s capture_b64=%s capture_truncated=%s' %
            (observation['stage'], observation['pid'], expected_tuple, actual_tuple, observation['stat_errno'],
             observation['pidfd_ack'], observation['pidfd_check'], pidfd_now, waitid,
             pipe_b64, observation['pipe_truncated'], observation['pipe_errno'],
             capture_b64, capture_truncated))


def _annotate_launch_error(exc, pidfd, capture, expected, observation):
    """Keep the original exception and custody even if diagnostics themselves fail."""
    try:
        detail = _launch_diagnostics(pidfd, capture, expected, observation)
    except BaseException:
        detail = 'launch_diagnostic unavailable'
    try:
        if isinstance(exc, OSError) and exc.errno is not None and exc.strerror is not None:
            exc.strerror = str(exc.strerror) + '; ' + detail
        else:
            exc.args = (str(exc) + '; ' + detail,)
    except BaseException:
        pass


def _launch_observation(pid, stage):
    return {'stage': stage, 'pid': pid, 'actual': None, 'stat_errno': None,
            'pidfd_ack': None, 'pidfd_check': None, 'pipe': None,
            'pipe_truncated': None, 'pipe_errno': None}


def _confirm_executable(pid, pidfd, error_read, capture, expected, deadline):
    """Check the original exec acknowledgement, executable identity, and live pidfd."""
    observation = _launch_observation(pid, 'exec_ack')
    try:
        while True:
            remaining = deadline - time.monotonic()
            _check(remaining > 0, 'preparer exec deadline')
            ready, _, _ = select.select([error_read, pidfd], [], [], remaining)
            _check(ready, 'preparer exec deadline')
            if pidfd in ready:
                observation['pidfd_ack'] = True
            if error_read in ready:
                try:
                    error = os.read(error_read, LAUNCH_DIAGNOSTIC_BYTES + 1)
                    observation['pipe'] = error
                    observation['pipe_truncated'] = len(error) > LAUNCH_DIAGNOSTIC_BYTES
                except OSError as exc:
                    observation['pipe_errno'] = exc.errno
                    if pidfd not in ready:
                        raise
                if pidfd in ready:
                    raise preparation.ProtocolError('preparer exited before exec acknowledgement')
                if error:
                    preview = error.decode('ascii', 'backslashreplace')[:128]
                    raise preparation.ProtocolError('preparer exec failed: ' + preview)
                break
            if pidfd in ready:
                raise preparation.ProtocolError('preparer exited before exec acknowledgement')
        observation['stage'] = 'proc_exe_stat'
        try:
            actual = os.stat('/proc/%d/exe' % pid)
        except OSError as exc:
            observation['stat_errno'] = exc.errno
            raise preparation.ProtocolError('executed preparer stat failed') from exc
        observation['actual'] = _object(actual)
        observation['stage'] = 'exe_identity'
        _check(observation['actual'] == expected, 'executed preparer identity mismatch')
        observation['stage'] = 'pidfd_live'
        observation['pidfd_check'] = bool(select.select([pidfd], [], [], 0)[0])
        _check(not observation['pidfd_check'], 'executed preparer exited before identity verification')
        return actual, observation
    except Exception as exc:
        _annotate_launch_error(exc, pidfd, capture, expected, observation)
        raise


def _exec_child(worker, executable, error_fd, capture, env, last_cap, progress_fd, capture_channel_fd):
    report_fd = error_fd
    try:
        # Protect the actual error channel before copying or overwriting any source.
        report_fd = _copy_high(error_fd)
        tmp = [_copy_high(fd) for fd in (worker, executable, capture, capture_channel_fd)]
        progress = _copy_high(progress_fd) if progress_fd is not None else None
        null = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        null_copy = _copy_high(null)
        os.close(null)
        os.dup2(null_copy, 0, inheritable=True)
        os.dup2(tmp[2], 1, inheritable=True)
        os.dup2(tmp[2], 2, inheritable=True)
        os.dup2(tmp[0], 3, inheritable=True)
        os.dup2(tmp[1], 4, inheritable=False)
        os.dup2(report_fd, 5, inheritable=False)
        report_fd = 5
        if progress is not None:
            os.dup2(progress, 6, inheritable=True)
        else:
            try:
                os.close(6)
            except OSError:
                pass
        os.dup2(tmp[3], 7, inheritable=True)
        # Linux x86-64 close_range: fail closed instead of leaving an unusually high FD open.
        if _LIBC.syscall(436, ctypes.c_uint(8), ctypes.c_uint(0xffffffff), 0) != 0:
            code = ctypes.get_errno()
            raise OSError(code, os.strerror(code))
        resource.setrlimit(resource.RLIMIT_AS, (2 * 1024**3, 2 * 1024**3))
        resource.setrlimit(resource.RLIMIT_NOFILE, (128, 128))
        resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
        resource.setrlimit(resource.RLIMIT_FSIZE, (CAPTURE_LIMIT, CAPTURE_LIMIT))
        os.setgroups([])
        _drop_capabilities(last_cap)
        os.execve(4, [PREPARER_PATH], env)
    except BaseException as exc:
        try:
            os.write(report_fd, ('%s: %s' % (type(exc).__name__, exc)).encode('ascii', 'backslashreplace')[:1024])
        except BaseException:
            pass
    os._exit(127)


def _exec_reader(worker, executable, error_fd, capture, guard, root_pidfd, preparer_pidfd,
                 env, last_cap, original_mask):
    report_fd = error_fd
    try:
        report_fd = _copy_high(error_fd)
        staged = [_copy_high(fd) for fd in (worker, executable, capture, guard, root_pidfd, preparer_pidfd)]
        null = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        null_copy = _copy_high(null)
        os.close(null)
        os.dup2(null_copy, 0, inheritable=True)
        os.dup2(staged[2], 1, inheritable=True)
        os.dup2(staged[2], 2, inheritable=True)
        os.dup2(staged[0], 3, inheritable=True)
        os.dup2(staged[1], 4, inheritable=False)
        os.dup2(report_fd, 5, inheritable=False)
        report_fd = 5
        os.dup2(staged[3], 6, inheritable=True)
        os.dup2(staged[4], 7, inheritable=True)
        os.dup2(staged[5], 8, inheritable=True)
        if _LIBC.syscall(436, ctypes.c_uint(9), ctypes.c_uint(0xffffffff), 0) != 0:
            code = ctypes.get_errno()
            raise OSError(code, os.strerror(code))
        _prctl(1, signal.SIGKILL)  # PR_SET_PDEATHSIG
        pdeathsig = ctypes.c_int()
        _prctl(2, ctypes.addressof(pdeathsig))  # PR_GET_PDEATHSIG
        _check(pdeathsig.value == signal.SIGKILL and not select.select([7, 8], [], [], 0)[0],
               'reader parent or preparer exited before exec')
        _check(os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
               _LIBC.prctl(27, 0, 0, 0, 0) == 0, 'reader root or securebits baseline')
        resource.setrlimit(resource.RLIMIT_AS, (2 * 1024**3, 2 * 1024**3))
        resource.setrlimit(resource.RLIMIT_NOFILE, (128, 128))
        resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
        resource.setrlimit(resource.RLIMIT_FSIZE, (CAPTURE_LIMIT, CAPTURE_LIMIT))
        os.setgroups([])
        _drop_capabilities(last_cap)
        _check(_LIBC.prctl(27, 0, 0, 0, 0) == 0, 'reader securebits changed')
        _prctl(2, ctypes.addressof(pdeathsig))
        _check(pdeathsig.value == signal.SIGKILL and not select.select([7, 8], [], [], 0)[0],
               'reader parent or preparer exited during setup')
        signal.pthread_sigmask(signal.SIG_SETMASK, original_mask)
        _prctl(2, ctypes.addressof(pdeathsig))
        _check(signal.pthread_sigmask(signal.SIG_BLOCK, set()) == original_mask and
               pdeathsig.value == signal.SIGKILL and not select.select([7, 8], [], [], 0)[0],
               'reader signal mask or participant identity changed before exec')
        os.execve(4, [PREPARER_PATH, 'reader-capture'], env)
    except BaseException as exc:
        try:
            os.write(report_fd, ('%s: %s' % (type(exc).__name__, exc)).encode('ascii', 'backslashreplace')[:1024])
        except BaseException:
            pass
    os._exit(127)



def _initial_userns():
    init = os.stat('/proc/1/ns/user')
    own = os.stat('/proc/self/ns/user')
    _check((init.st_dev, init.st_ino) == (own.st_dev, own.st_ino), 'supervisor is outside initial userns')
    return {'dev': init.st_dev, 'ino': init.st_ino}


def _process_identity(value, procfd, pidfd, owner):
    preparation.fields(value, ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns', 'userns'), 'process identity')
    pid = preparation.integer(value['pid'], 'process PID', 1, (1 << 31) - 1)
    preparation.integer(value['starttime'], 'process starttime', 1)
    for name in ('mntns', 'pidns', 'cgroupns', 'userns'):
        preparation.identity(value[name], 'process ' + name)
    _check(value['userns'] == owner, 'process user namespace changed')
    preparation.validate_pidfd(pidfd, pid)
    proc = os.fstat(procfd)
    named = os.stat('/proc/%d' % pid)
    _check(stat.S_ISDIR(proc.st_mode) and _object(proc) == _object(named) and
           _proc_start('/proc/%d/stat' % pid, pid) == value['starttime'], 'held process identity changed')
    for name, key, kind in (('mnt', 'mntns', CLONE_NEWNS), ('pid', 'pidns', CLONE_NEWPID),
                            ('cgroup', 'cgroupns', CLONE_NEWCGROUP)):
        fd = _open_namespace(procfd, name)
        try:
            evidence = _namespace(fd, kind, owner)
            _check({k: evidence[k] for k in ('dev', 'ino')} == value[key], 'active namespace changed')
        finally:
            os.close(fd)
    userfd = _open_namespace(procfd, 'user')
    try:
        _check(_ns_identity(userfd) == owner, 'active user namespace changed')
    finally:
        os.close(userfd)
    preparation.validate_pidfd(pidfd, pid)


def _validate_capture_pins(active, item):
    request, helper, owner, fds = active['request'], item['helper'], item['metadata_owner'], item['fds']
    init_user = _initial_userns()
    _check(len(fds) == (8 if owner is not None else 6), 'capture descriptor count')
    _process_identity(helper, fds[0], fds[5], init_user)
    expected = request['helper']
    _check(all(helper[key] == expected[key] for key in ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns')),
           'capture helper does not match request')
    held_root = os.fstat(fds[1])
    actual_root = os.stat('/proc/%d/root' % helper['pid'])
    _check(stat.S_ISDIR(held_root.st_mode) and _object(held_root) == _object(actual_root),
           'capture helper root changed')
    for fd, name, kind in ((fds[2], 'mntns', CLONE_NEWNS), (fds[3], 'pidns', CLONE_NEWPID),
                           (fds[4], 'cgroupns', CLONE_NEWCGROUP)):
        actual = _namespace(fd, kind, init_user)
        _check({k: actual[k] for k in ('dev', 'ino')} == helper[name], 'held namespace mismatch')
    metadata_pid = request['input']['metadata_pid']
    _check((owner is None) == (metadata_pid is None), 'metadata owner presence mismatch')
    if owner is not None:
        _process_identity(owner, fds[6], fds[7], init_user)
        _check(owner['pid'] == metadata_pid, 'metadata owner PID mismatch')
        if request['target'] is not None:
            _check(owner['pid'] == request['target']['pid'] and
                   owner['starttime'] == request['target']['starttime'], 'target owner identity changed')
        else:
            _check(request['input']['exe'] and owner['pid'] == request['daemon']['pid'],
                   'metadata owner is not selected daemon')
    preparation.validate_pidfd(active['pidfd'], active['pid'])


def _metadata_objects(item):
    fds = item['fds']
    source = os.open('mountinfo', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fds[6])
    directory = target = None
    try:
        directory = os.open('meta', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fds[1])
        target = os.open('target-mountinfo', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
        source_st, target_st = os.fstat(source), os.fstat(target)
        _check(stat.S_ISREG(source_st.st_mode) and stat.S_ISREG(target_st.st_mode) and
               _object(source_st) == _object(target_st), 'metadata mountinfo objects differ')
        return {'source': _object(source_st), 'target': _object(target_st)}
    finally:
        os.close(source)
        if target is not None:
            os.close(target)
        if directory is not None:
            os.close(directory)


def _status_fields(text):
    result = {}
    for line in text.splitlines():
        name, sep, value = line.partition(':')
        if sep:
            _check(name not in result, 'duplicate task status field')
            result[name] = value.strip()
    return result


def _barrier(active, item, reader, snapshot, executable):
    preparation.fields(snapshot, ('host_pid', 'host_tid', 'inner_pid', 'inner_tid', 'starttime',
                                  'pidns', 'mntns', 'cgroupns', 'userns', 'pdeathsig'), 'reader snapshot')
    for field in ('host_pid', 'host_tid', 'inner_pid', 'inner_tid', 'starttime'):
        preparation.integer(snapshot[field], 'reader ' + field, 1)
    preparation.identity(snapshot['userns'], 'reader user namespace')
    for name, key in (('pid', 'pidns'), ('mnt', 'mntns'), ('cgroup', 'cgroupns')):
        preparation._namespace_evidence(snapshot[key], name, item['helper']['userns'], item['helper'][key])
    _check(snapshot['host_pid'] == reader['pid'] and snapshot['starttime'] == reader['starttime'] and
           type(snapshot['pdeathsig']) is int and snapshot['pdeathsig'] == signal.SIGKILL,
           'reader snapshot process identity')
    _validate_capture_pins(active, item)
    preparation.validate_pidfd(reader['pidfd'], reader['pid'])
    before = _tasks(reader['pid'])
    _check(snapshot['host_tid'] in before, 'reader thread absent')
    threads = []
    evidence_size = 0
    init_user = _initial_userns()
    for tid in before:
        base = '/proc/%d/task/%d/' % (reader['pid'], tid)
        status = _read_bounded(base + 'status')
        parsed = _status_fields(status)
        _check(status.endswith('\n') and 'Groups' in parsed and
               parsed.get('Uid', '').split() == ['0'] * 4 and parsed.get('Gid', '').split() == ['0'] * 4 and
               parsed['Groups'].split() == [] and parsed.get('NoNewPrivs') == '1' and
               all(parsed.get(k) == preparation.CAP_MASK for k in ('CapEff', 'CapPrm', 'CapBnd')) and
               all(parsed.get(k) == '0000000000000000' for k in ('CapInh', 'CapAmb')),
               'reader thread credentials or capabilities')
        nspid = parsed.get('NSpid', '').split()
        nstgid = parsed.get('NStgid', '').split()
        _check(nspid and nstgid and all(part.isdecimal() and int(part) > 0 for part in nspid + nstgid) and
               int(nspid[0]) == tid and int(nstgid[0]) == reader['pid'] and
               int(nstgid[-1]) == snapshot['inner_pid'] and parsed.get('Pid') == str(tid) and
               parsed.get('Tgid') == str(reader['pid']), 'reader namespace thread mapping')
        pidns_fd = os.open(base + 'ns/pid', os.O_RDONLY | os.O_CLOEXEC)
        try:
            pidns = _namespace(pidns_fd, CLONE_NEWPID, init_user)
            _check({key: pidns[key] for key in ('dev', 'ino')} == item['helper']['pidns'],
                   'reader runtime thread PID namespace changed')
        finally:
            os.close(pidns_fd)
        lsm = _read_bounded(base + 'attr/current', 4096)
        _check(lsm.strip() == 'unconfined', 'reader LSM confinement changed')
        evidence_size += len(status) + len(lsm)
        _check(evidence_size <= preparation.MAX_EVIDENCE, 'reader thread evidence exceeds bound')
        threads.append({'host_tid': tid, 'inner_tid': int(nspid[-1]),
                        'starttime': _proc_start(base + 'stat', tid), 'status': status, 'lsm': lsm})
    after = _tasks(reader['pid'])
    _check(before == after, 'reader task inventory changed')
    capturers = [row for row in threads if row['inner_tid'] == snapshot['inner_tid']]
    _check(len(capturers) == 1 and capturers[0]['host_tid'] == snapshot['host_tid'] and
           snapshot['inner_pid'] == snapshot['inner_tid'] and snapshot['host_tid'] == reader['pid'],
           'reader capturing thread is not startup thread')
    leader = next(row for row in threads if row['host_tid'] == reader['pid'])
    _check(leader['inner_tid'] == snapshot['inner_pid'] and leader['starttime'] == snapshot['starttime'],
           'reader host and inner process identity mismatch')
    _check(snapshot['userns'] == init_user, 'reader user namespace mismatch')
    procfd = os.open('/proc/%d/task/%d' % (reader['pid'], snapshot['host_tid']),
                     os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for name, field, kind in (('pid', 'pidns', CLONE_NEWPID), ('mnt', 'mntns', CLONE_NEWNS),
                                  ('cgroup', 'cgroupns', CLONE_NEWCGROUP)):
            fd = _open_namespace(procfd, name)
            try:
                actual = _namespace(fd, kind, init_user)
                _check(actual == snapshot[field] and
                       {k: actual[k] for k in ('dev', 'ino')} == item['helper'][field],
                       'reader active namespace mismatch')
            finally:
                os.close(fd)
    finally:
        os.close(procfd)
    exe = _object(os.stat('/proc/%d/exe' % reader['pid']))
    _check(exe == executable, 'reader executable changed')
    _validate_capture_pins(active, item)
    preparation.validate_pidfd(reader['pidfd'], reader['pid'])
    return {'reader': snapshot, 'exe': exe, 'tasks_before': before, 'tasks_after': after, 'threads': threads}


def _recv_owned(sock, credentials):
    ancillary_size = socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(16 * array.array('i').itemsize)
    fds = []
    try:
        data, ancillary, flags, _ = sock.recvmsg(preparation.MAX_FRAME + 1, ancillary_size,
                                                  getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
        if not data and not ancillary and not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC):
            return None, []
        creds, rights, invalid = [], 0, False
        for level, kind, value in ancillary:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                rights += 1
                packed = array.array('i')
                usable = len(value) - len(value) % packed.itemsize
                packed.frombytes(value[:usable])
                fds.extend(packed)
                invalid = invalid or usable != len(value)
            elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS:
                if len(value) == struct.calcsize('3i'):
                    creds.append(struct.unpack('3i', value))
                else:
                    invalid = True
            else:
                invalid = True
        for fd in fds:
            os.set_inheritable(fd, False)
        _check(not invalid and not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) and
               0 < len(data) <= preparation.MAX_FRAME and len(fds) <= 16 and
               rights == (1 if fds else 0) and creds == [tuple(credentials)], 'capture credentials or rights')
        value = preparation.strict_json(data)
        _check(type(value) is dict and preparation.canonical_json(value) == data, 'noncanonical capture frame')
        return value, fds
    except BaseException:
        for fd in fds:
            os.close(fd)
        raise

class RootSupervisor:
    def __init__(self, sock, coordinator_pid, run_token, session_nonce, executable_fd, executable_receipt,
                 test_control=None):
        self.sock = sock
        self.pid = preparation.integer(coordinator_pid, 'coordinator PID', 1, (1 << 31) - 1)
        self.run_token = preparation.hex_string(run_token, 'run token', preparation.HEX32)
        self.session_nonce = preparation.hex_string(session_nonce, 'session nonce')
        self.executable_fd = executable_fd
        self.executable = executable_receipt
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.guard = GuardRegistration(sock, (self.pid, COORDINATOR_UID, COORDINATOR_GID),
                                       run_token, session_nonce, '/lab/control/guard', COORDINATOR_UID)
        self.active = None
        self.reader = None
        self.pending_capture = None
        self.control_out = []
        self.test_out = []
        self.uncertain = False
        self.reader_faults = []
        self.containment = None
        self.injected_frame = None
        self.expected_failure = None
        self.last_captures = []
        self.root_pidfd = os.pidfd_open(os.getpid(), 0)
        self.test_control = test_control
        if test_control is not None:
            test_sock, scenario = test_control
            _check(scenario in ('before_capture_preparer_death', 'after_capture_preparer_death',
                                'supervisor_fork_death', 'supervisor_launch_death', 'pidfd_open_failure',
                                'reader_timeout', 'reader_nonzero', 'restore_failure') and
                   test_sock.family == socket.AF_UNIX and
                   test_sock.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) == socket.SOCK_SEQPACKET,
                   'invalid trusted fault control')
            test_sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            self.test_peer = os.getppid()
        self.last_ordinal = 0
        self.nonces = set()
        self.retired = False
        self.retired_ledger = None
        self.launches = []
        self.exits = []
        self.retirement_readback = None
        self.test_arm = None
        self.fault_receipts = []
        with open('/proc/sys/kernel/cap_last_cap', 'r', encoding='ascii') as inp:
            self.last_cap = int(inp.read().strip())
        _check(21 <= self.last_cap < 64, 'unexpected kernel capability range')

    def _identity(self, op):
        return {'version': 1, 'op': op, 'run_token': self.run_token, 'session_nonce': self.session_nonce}

    def receive_guard(self):
        return self.guard.receive()

    def accept_guard(self, owner):
        return self.guard.accept(owner)

    def arm_test_kill(self, point):
        _check(point in ('before_attach', 'after_first_attach') and self.test_arm is None and
               self.active is None and not self.retired, 'invalid or overlapping fault arm')
        self.test_arm = point

    def publish_launch(self, receipt):
        active = self.active
        _check(active is not None and receipt['pid'] == active['pid'] and
               receipt['ordinal'] == active['ordinal'] and receipt['nonce'] == active['nonce'],
               'launch receipt does not match owned child')
        preparation.send_frame(self.sock, receipt, (active['pidfd'],))
        self.launches.append(receipt)

    def _publish_confirmed_launch(self, receipt, expected, observation):
        try:
            self.publish_launch(receipt)
        except Exception as exc:
            try:
                observation['stage'] = 'publish'
                active = self.active
                _annotate_launch_error(exc, active['pidfd'], active['capture'], expected, observation)
            except BaseException:
                pass
            raise

    def _launch(self, msg, worker):
        preparation.fields(msg, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                                 'request_digest', 'prepare_request'), 'launch request')
        _check(self.guard.accepted and self.guard.fd is not None and self.active is None and not self.retired and
               type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'launch_prepare' and
               msg['run_token'] == self.run_token and msg['session_nonce'] == self.session_nonce and
               type(msg['ordinal']) is int and msg['ordinal'] == self.last_ordinal + 1,
               'launch identity or ordering')
        preparation.hex_string(msg['nonce'], 'launch nonce')
        preparation.hex_string(msg['request_digest'], 'launch request digest')
        _check(msg['nonce'] not in self.nonces, 'launch nonce replay')
        request = preparation.validate_request(msg['prepare_request'])
        _check(request['request_digest'] == msg['request_digest'] and
               request['input']['run_token'] == self.run_token and request['input']['ordinal'] == msg['ordinal'] and
               request['input']['nonce'] == msg['nonce'] and
               request['input']['preparer_sha256'] == self.executable['copy_sha256'], 'launch request binding')
        validate_guard_fd(self.guard.fd, self.guard.registration['guard'], '/lab/control/guard', COORDINATOR_UID)
        endpoint = socket.socket(fileno=worker)
        try:
            _check(endpoint.family == socket.AF_UNIX and endpoint.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) ==
                   socket.SOCK_SEQPACKET, 'worker is not an AF_UNIX seqpacket socket')
            endpoint.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        finally:
            endpoint.detach()
        executable = os.fstat(self.executable_fd)
        _check(_object(executable) == self.executable['copy'] and _sha256_fd(self.executable_fd) ==
               self.executable['copy_sha256'], 'held executable changed')
        _check_no_file_capability(self.executable_fd)
        env = {'GOMAXPROCS': '1', 'G2_RUN_TOKEN': self.run_token, 'G2_SESSION_NONCE': self.session_nonce,
               'G2_PREPARE_ORDINAL': str(msg['ordinal']), 'G2_PREPARE_NONCE': msg['nonce'],
               'G2_REQUEST_DIGEST': msg['request_digest'], 'G2_COORDINATOR_PID': str(self.pid),
               'G2_ROOT_PID': str(os.getpid())}
        progress_parent = progress_child = None
        point = self.test_arm
        if point is not None:
            progress_parent, progress_child = socket.socketpair(socket.AF_UNIX,
                                                               socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            progress_parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            env['G2_PREPARER_PROGRESS_FD'] = '6'
        capture_parent, capture_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        capture_parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        capture_child.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        capture = os.memfd_create('g2-preparer-capture', os.MFD_CLOEXEC)
        error_read, error_write = os.pipe2(os.O_CLOEXEC | os.O_NONBLOCK)
        try:
            pid = os.fork()
            if pid == 0:
                _exec_child(worker, self.executable_fd, error_write, capture, env, self.last_cap,
                            progress_child.fileno() if progress_child is not None else None, capture_child.fileno())
            os.close(error_write)
            error_write = None
            capture_child.close()
            capture_child = None
            if progress_child is not None:
                progress_child.close()
                progress_child = None
            self.active = {'pid': pid, 'pidfd': None, 'capture': capture, 'ordinal': msg['ordinal'],
                           'nonce': msg['nonce'], 'request_digest': msg['request_digest'], 'request': request,
                           'capture_socket': capture_parent, 'capture_out': [], 'captures': [],
                           'deadline': time.monotonic() + 30, 'timeout': False, 'progress': progress_parent,
                           'progress_point': point, 'progress_parts': [], 'progress_size': 0,
                           'progress_snapshots': [], 'fault': None}
            progress_parent = None
            capture_parent = None
            self.test_arm = None
            capture = None
            try:
                pidfd = os.pidfd_open(pid, 0)
                self.active['pidfd'] = pidfd
            except OSError as exc:
                try:
                    observation = _launch_observation(pid, 'pidfd_unavailable_ownership_uncertain')
                    try:
                        observation['pipe'] = os.read(error_read, LAUNCH_DIAGNOSTIC_BYTES + 1)
                        observation['pipe_truncated'] = len(observation['pipe']) > LAUNCH_DIAGNOSTIC_BYTES
                    except BaseException as pipe_exc:
                        observation['pipe_errno'] = getattr(pipe_exc, 'errno', None)
                    _annotate_launch_error(exc, None, self.active['capture'], self.executable['copy'], observation)
                except BaseException:
                    pass
                raise
            actual, observation = _confirm_executable(pid, pidfd, error_read, self.active['capture'],
                                                      self.executable['copy'], time.monotonic() + 5)
            guard = {'version': 1, 'op': 'capture_guard', 'run_token': self.run_token,
                     'session_nonce': self.session_nonce, 'ordinal': msg['ordinal'], 'nonce': msg['nonce'],
                     'request_digest': msg['request_digest']}
            preparation.send_frame(self.active['capture_socket'], guard, (self.guard.fd,))
            self.last_ordinal = msg['ordinal']
            self.nonces.add(msg['nonce'])
            receipt = self._identity('launch_accepted')
            receipt.update(ordinal=msg['ordinal'], nonce=msg['nonce'], request_digest=msg['request_digest'],
                           pid=pid, exe=_object(actual))
            self._publish_confirmed_launch(receipt, self.executable['copy'], observation)
            return receipt
        finally:
            if error_write is not None:
                os.close(error_write)
            os.close(error_read)
            if capture is not None:
                os.close(capture)
            if progress_child is not None:
                progress_child.close()
            if progress_parent is not None:
                progress_parent.close()
            if capture_child is not None:
                capture_child.close()
            if capture_parent is not None:
                capture_parent.close()

    def handle(self):
        _check(self.guard.accepted and self.guard.fd is not None, 'guard not registered')
        msg, fds = _recv_owned(self.sock, (self.pid, COORDINATOR_UID, COORDINATOR_GID))
        try:
            _check(msg is not None, 'coordinator control channel ended')
            if msg.get('op') == 'launch_prepare':
                _check(len(fds) == 1, 'launch requires exactly one worker socket')
                return self._launch(msg, fds[0])
            if msg.get('op') == 'capture_intent_ack':
                _check(not fds, 'capture acknowledgement carries descriptors')
                return self._ack_capture(msg)
            preparation.fields(msg, ('version', 'op', 'run_token', 'session_nonce', 'ledger_sha256'),
                               'coordinator retirement')
            _check(not fds and not self.retired and self.active is None and self.reader is None and
                   self.pending_capture is None and not self.control_out and not self.uncertain and self.test_arm is None and
                   type(msg['version']) is int and
                   msg['version'] == 1 and msg['op'] == 'coordinator_retired' and
                   msg['run_token'] == self.run_token and msg['session_nonce'] == self.session_nonce,
                   'coordinator retirement binding')
            preparation.hex_string(msg['ledger_sha256'], 'retired ledger digest')
            lab_fd = os.open('/lab', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                _trusted_dir(lab_fd, '/lab')
            finally:
                os.close(lab_fd)
            self.retirement_readback = validate_retired_ledger('/lab/control', COORDINATOR_UID, self.run_token,
                                                              self.session_nonce, msg['ledger_sha256'],
                                                              self.launches, self.exits)
            self.retired = True
            self.retired_ledger = msg['ledger_sha256']
            reply = self._identity('retired_accepted')
            reply['ledger_sha256'] = self.retired_ledger
            preparation.send_frame(self.sock, reply)
            return reply
        finally:
            for fd in fds:
                os.close(fd)

    @staticmethod
    def _queue(queue, message, fds=(), marker=None):
        data = preparation.canonical_json(message)
        _check(0 < len(data) <= preparation.MAX_FRAME and
               sum(len(entry[0]) for entry in queue) + len(data) <= preparation.MAX_EVIDENCE,
               'outgoing capture queue exceeds bound')
        queue.append((data, tuple(fds), marker))

    @staticmethod
    def _flush(sock, queue):
        completed = []
        while queue:
            data, fds, marker = queue[0]
            ancillary = [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', fds))] if fds else []
            try:
                sent = sock.sendmsg([data], ancillary, socket.MSG_DONTWAIT | getattr(socket, 'MSG_NOSIGNAL', 0))
            except BlockingIOError:
                break
            _check(sent == len(data), 'short capture datagram write')
            queue.pop(0)
            completed.append(marker)
        return completed

    def fault_state(self):
        active, reader = self.active, self.reader
        return {'preparer': None if active is None else
                {'pid': active['pid'], 'ordinal': active['ordinal'], 'nonce': active['nonce'],
                 'request_digest': active['request_digest'], 'pidfd_held': active.get('pidfd') is not None},
                'reader': None if reader is None else
                {'pid': reader.get('pid'), 'starttime': reader.get('starttime'),
                 'pidfd_held': reader.get('pidfd') is not None, 'stage': reader.get('stage'),
                 'binding': reader['binding']},
                'captures': self.last_captures if active is None else active['captures'], 'uncertain': self.uncertain,
                'faults': self.reader_faults, 'containment': self.containment,
                'preparer_exits': self.exits[-1:] if active is None else [],
                'injected_frame': self.injected_frame, 'expected_failure': self.expected_failure}

    def _record_reader_fault(self, point, effect):
        _check(self.test_control is not None and self.pending_capture is not None, 'unarmed reader fault')
        self.reader_faults.append({'scenario': self.test_control[1], 'point': point,
                                   'binding': self.pending_capture['binding'], 'effect': effect})

    def _test_event(self, point):
        if self.test_control is None:
            return False
        sock, scenario = self.test_control
        active, reader, item = self.active, self.reader, self.pending_capture
        _check(active is not None and reader is not None and item is not None and reader.get('pid') is not None,
               'fault event lacks owned reader')
        message = {'version': 1, 'op': 'reader_fault_point', 'point': point, 'scenario': scenario,
                   'binding': item['binding'], 'root_pid': os.getpid(), 'preparer_pid': active['pid'],
                   'reader_pid': reader['pid'], 'request': active['request'], 'helper': item['helper'],
                   'metadata_owner': item['metadata_owner']}
        fds = [self.root_pidfd, active['pidfd']]
        if reader.get('pidfd') is not None:
            fds.append(reader['pidfd'])
        deadline = min(active['deadline'], reader['deadline'])
        remaining = deadline - time.monotonic()
        _check(remaining > 0 and select.select([], [sock], [], remaining)[1], 'fault event deadline')
        preparation.send_frame(sock, message, fds)
        remaining = deadline - time.monotonic()
        _check(remaining > 0 and select.select([sock], [], [], remaining)[0], 'fault acknowledgement deadline')
        reply, received = _recv_owned(sock, (self.test_peer, 0, 0))
        try:
            preparation.fields(reply, ('version', 'op', 'point', 'scenario', 'binding', 'action'), 'fault ack')
            _check(not received and type(reply['version']) is int and reply['version'] == 1 and
                   reply['op'] == 'reader_fault_continue' and reply['point'] == point and
                   reply['scenario'] == scenario and preparation.canonical_json(reply['binding']) ==
                   preparation.canonical_json(item['binding']) and reply['action'] in ('continue', 'fault'),
                   'fault acknowledgement binding')
            expected = {'pidfd_open_failure': 'fork_registered', 'restore_failure': 'before_restore',
                        'reader_timeout': 'reader_before', 'reader_nonzero': 'reader_before'}
            if reply['action'] == 'fault':
                _check(item['binding']['phase'] == 'before' and expected.get(scenario) == point,
                       'fault action outside selected cutpoint')
                return True
            return False
        finally:
            for fd in received:
                os.close(fd)

    def handle_capture(self):
        active = self.active
        _check(active is not None and not active.get('capture_eof'), 'unexpected capture channel event')
        msg, fds = _recv_owned(active['capture_socket'], (active['pid'], 0, 0))
        try:
            if msg is None:
                _check(not fds and self.pending_capture is None and self.reader is None and
                       not active['capture_out'], 'preparer capture channel ended during capture')
                active['capture_eof'] = True
                return
            _check(self.pending_capture is None and self.reader is None and not active.get('capture_failed'),
                   'overlapping capture request')
            preparation.fields(msg, ('version', 'op', 'binding', 'request', 'helper', 'metadata_owner'),
                               'capture request')
            preparation.validate_request(msg['request'])
            _check(type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'capture_request' and
                   preparation.canonical_json(msg['request']) == preparation.canonical_json(active['request']),
                   'capture request binding')
            binding = msg['binding']
            preparation.fields(binding, ('version', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                                         'input_digest', 'request_digest', 'phase', 'sequence'), 'capture binding')
            preparation.capture_binding(binding, active['request'], self.session_nonce)
            sequence = len(active['captures']) + 1
            expected = {'version': 1, 'run_token': self.run_token, 'session_nonce': self.session_nonce,
                        'ordinal': active['ordinal'], 'nonce': active['nonce'],
                        'input_digest': active['request']['input_digest'], 'request_digest': active['request_digest'],
                        'phase': {1: 'before', 2: 'after'}.get(sequence), 'sequence': sequence}
            _check(preparation.canonical_json(binding) == preparation.canonical_json(expected) and sequence <= 2 and
                   (sequence == 1 or (active['captures'][0]['exit'] is not None and
                                      active['captures'][0]['exit']['exit_code'] == 0)),
                   'capture sequence or binding')
            item = {'binding': binding, 'helper': msg['helper'], 'metadata_owner': msg['metadata_owner'],
                    'fds': fds, 'state': 'awaiting_ack'}
            _validate_capture_pins(active, item)
            intent = {'version': 1, 'op': 'capture_intent', 'binding': binding,
                      'helper': msg['helper'], 'metadata_owner': msg['metadata_owner']}
            digest = hashlib.sha256(preparation.canonical_json(intent)).hexdigest()
            active['captures'].append({'intent': intent, 'intent_sha256': digest, 'launch': None, 'exit': None})
            self.pending_capture = item
            self._queue(self.control_out, intent)
            fds = []
        finally:
            for fd in fds:
                os.close(fd)

    def _ack_capture(self, msg):
        item, active = self.pending_capture, self.active
        _check(item is not None and active is not None and item['state'] == 'awaiting_ack' and self.reader is None,
               'unknown capture acknowledgement')
        preparation.fields(msg, ('version', 'op', 'binding', 'intent_sha256'), 'capture acknowledgement')
        record = active['captures'][-1]
        _check(type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'capture_intent_ack' and
               preparation.canonical_json(msg['binding']) == preparation.canonical_json(item['binding']) and
               msg['intent_sha256'] == record['intent_sha256'],
               'capture acknowledgement mismatch')
        item['state'] = 'acknowledged'
        self._start_reader()

    def _start_reader(self):
        active, item = self.active, self.pending_capture
        _validate_capture_pins(active, item)
        validate_guard_fd(self.guard.fd, self.guard.registration['guard'], '/lab/control/guard', COORDINATOR_UID)
        _check_no_file_capability(self.executable_fd)
        _check(_object(os.fstat(self.executable_fd)) == self.executable['copy'] and
               _sha256_fd(self.executable_fd) == self.executable['copy_sha256'], 'reader executable changed')
        preparation.validate_pidfd(self.root_pidfd, os.getpid())
        owner = _initial_userns()
        tasks_before = _tasks(os.getpid())
        _check(tasks_before == [os.getpid()], 'supervisor must have exactly one thread')
        _check_reaper()
        procfd = os.open('/proc/self/task/%d' % os.getpid(), os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        original = child_target = None
        parent = child = None
        capture = error_read = error_write = None
        old_mask = None
        reader = None
        try:
            original = _open_namespace(procfd, 'pid_for_children')
            active_fd = _open_namespace(procfd, 'pid')
            try:
                active_before = _namespace(active_fd, CLONE_NEWPID, owner)
                children_before = _namespace(original, CLONE_NEWPID, owner)
            finally:
                os.close(active_fd)
            _check(active_before == children_before, 'original children PID namespace differs')
            child_target = item['fds'][3]
            target = _namespace(child_target, CLONE_NEWPID, owner)
            _check({key: target[key] for key in ('dev', 'ino')} == item['helper']['pidns'],
                   'reader PID namespace target mismatch')
            parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            child.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            capture = os.memfd_create('g2-reader-capture', os.MFD_CLOEXEC)
            error_read, error_write = os.pipe2(os.O_CLOEXEC | os.O_NONBLOCK)
            deadline = min(active['deadline'], time.monotonic() + 10)
            reader = {'pid': None, 'pidfd': None, 'starttime': None, 'binding': item['binding'],
                      'socket': parent, 'out': [], 'capture': capture, 'error': error_read,
                      'deadline': deadline, 'exec_deadline': min(deadline, time.monotonic() + 5),
                      'stage': 'forking', 'parts': [], 'size': 0, 'proof_size': 0, 'terminal': False, 'eof': False,
                      'before': None, 'after': None, 'payload': None, 'payload_digest': None}
            self.reader = reader
            parent = capture = error_read = None
            env = {'GOMAXPROCS': '1', 'G2_READER_BINDING': preparation.canonical_json(item['binding']).decode('ascii'),
                   'G2_ROOT_PID': str(os.getpid()), 'G2_PREPARER_PID': str(active['pid'])}
            blocked = signal.valid_signals() - {signal.SIGKILL, signal.SIGSTOP}
            old_mask = signal.pthread_sigmask(signal.SIG_BLOCK, blocked)
            switched = False
            restore_error = None
            pidfd_fault = False
            restore_fault = False
            try:
                _setns(child_target, CLONE_NEWPID)
                switched = True
                current_fd = _open_namespace(procfd, 'pid_for_children')
                try:
                    children_switched = _namespace(current_fd, CLONE_NEWPID, owner)
                finally:
                    os.close(current_fd)
                _check(children_switched == target, 'PID namespace switch did not take effect')
                pid = _LIBC.syscall(57)  # raw fork: bypass Python and libc at-fork callbacks
                if pid < 0:
                    code = ctypes.get_errno()
                    raise OSError(code, os.strerror(code))
                if pid == 0:
                    _exec_reader(child.fileno(), self.executable_fd, error_write, reader['capture'], self.guard.fd,
                                 self.root_pidfd, active['pidfd'], env, self.last_cap, old_mask)
                    os._exit(127)
                reader['pid'] = int(pid)  # first parent action after fork: preserve direct-child ownership
                reader['stage'] = 'registered'
                pidfd_fault = self._test_event('fork_registered')
                restore_fault = self._test_event('before_restore')
            finally:
                try:
                    if restore_fault:
                        try:
                            _setns(-1, CLONE_NEWPID)
                        except OSError:
                            self._record_reader_fault('before_restore', 'restore_failure')
                            raise
                    if switched:
                        _setns(original, CLONE_NEWPID)
                    active_fd = _open_namespace(procfd, 'pid')
                    children_fd = _open_namespace(procfd, 'pid_for_children')
                    try:
                        active_after = _namespace(active_fd, CLONE_NEWPID, owner)
                        children_after = _namespace(children_fd, CLONE_NEWPID, owner)
                    finally:
                        os.close(active_fd)
                        os.close(children_fd)
                    tasks_after = _tasks(os.getpid())
                    _check(active_after == active_before and children_after == children_before and
                           tasks_after == tasks_before, 'supervisor PID namespace restoration mismatch')
                except BaseException as exc:
                    restore_error = exc
                    self.uncertain = True
                finally:
                    signal.pthread_sigmask(signal.SIG_SETMASK, old_mask)
                    restored = signal.pthread_sigmask(signal.SIG_BLOCK, set()) == old_mask
                    if not restored:
                        self.uncertain = True
                        restore_error = preparation.ProtocolError('supervisor signal mask restoration mismatch')
                if restore_error is not None:
                    raise preparation.ProtocolError('supervisor restoration uncertain: ' + str(restore_error))
            os.close(error_write)
            error_write = None
            child.close()
            child = None
            _check(reader['pid'] is not None, 'reader fork did not register PID')
            if pidfd_fault:
                self._record_reader_fault('fork_registered', 'pidfd_open_failure')
                raise OSError(errno.EMFILE, 'injected reader pidfd_open failure')
            reader['pidfd'] = os.pidfd_open(reader['pid'], 0)
            preparation.validate_pidfd(reader['pidfd'], reader['pid'])
            reader['starttime'] = _proc_start('/proc/%d/stat' % reader['pid'], reader['pid'])
            reader['root_proof'] = {'active_before': active_before, 'children_before': children_before,
                                    'children_switched': children_switched, 'active_after': active_after,
                                    'children_after': children_after, 'tasks_before': tasks_before,
                                    'tasks_after': tasks_after, 'sigchld_default': True,
                                    'sigchld_nocldwait': False, 'signal_mask_restored': True}
            reader['stage'] = 'exec'
        except BaseException:
            self.uncertain = True
            raise
        finally:
            if original is not None:
                os.close(original)
            os.close(procfd)
            if parent is not None:
                parent.close()
            if child is not None:
                child.close()
            for fd in (capture, error_read, error_write):
                if fd is not None:
                    os.close(fd)

    def handle_reader_exec(self):
        reader, active, item = self.reader, self.active, self.pending_capture
        _check(reader is not None and reader['stage'] == 'exec', 'unexpected reader exec event')
        raw = os.read(reader['error'], LAUNCH_DIAGNOSTIC_BYTES + 1)
        _check(not raw, 'reader exec failed: ' + raw.decode('ascii', 'backslashreplace')[:128])
        os.close(reader['error'])
        reader['error'] = None
        preparation.validate_pidfd(reader['pidfd'], reader['pid'])
        _check(_proc_start('/proc/%d/stat' % reader['pid'], reader['pid']) == reader['starttime'] and
               _object(os.stat('/proc/%d/exe' % reader['pid'])) == self.executable['copy'],
               'executed reader identity mismatch')
        pidfd = os.open('/proc/%d/ns/pid' % reader['pid'], os.O_RDONLY | os.O_CLOEXEC)
        try:
            pidns = _namespace(pidfd, CLONE_NEWPID, _initial_userns())
        finally:
            os.close(pidfd)
        _check({key: pidns[key] for key in ('dev', 'ino')} == item['helper']['pidns'],
               'reader born in wrong active PID namespace')
        _validate_capture_pins(active, item)
        launch = {'binding': item['binding'], 'host_pid': reader['pid'], 'starttime': reader['starttime'],
                  'exe': {**self.executable['copy'], 'sha256': self.executable['copy_sha256']},
                  'reader_pidns': pidns, 'root': reader['root_proof']}
        active['captures'][-1]['launch'] = launch
        self._queue(self.control_out, {'version': 1, 'op': 'capture_launch', 'binding': item['binding'],
                                      'launch': launch}, marker='reader_launch')
        reader['stage'] = 'launch_queued'

    def flush_control(self):
        for marker in self._flush(self.sock, self.control_out):
            if marker == 'reader_launch':
                reader, item, active = self.reader, self.pending_capture, self.active
                _check(reader is not None and reader['stage'] == 'launch_queued', 'reader launch ordering')
                self._test_event('launch_registered')
                _validate_capture_pins(active, item)
                self._queue(reader['out'], {'version': 1, 'op': 'reader_start', 'binding': item['binding'],
                                            'request': active['request'], 'helper': item['helper'],
                                            'metadata_owner': item['metadata_owner']}, item['fds'])
                reader['stage'] = 'before'

    def handle_reader(self):
        reader, active, item = self.reader, self.active, self.pending_capture
        _check(reader is not None and reader['pidfd'] is not None and not reader['eof'],
               'unexpected reader frame')
        msg, fds = _recv_owned(reader['socket'], (reader['pid'], 0, 0))
        try:
            _check(not fds, 'reader sent unexpected descriptors')
            if msg is None:
                reader['eof'] = True
                return
            _check(not reader['terminal'], 'extra frame after reader terminal')
            binding = item['binding']
            if reader['stage'] in ('before', 'after'):
                phase = reader['stage']
                preparation.fields(msg, ('version', 'op', 'binding', 'reader'), 'reader barrier')
                _check(type(msg['version']) is int and msg['version'] == 1 and
                       msg['op'] == 'reader_' + phase and
                       preparation.canonical_json(msg['binding']) == preparation.canonical_json(binding),
                       'reader barrier binding')
                proof = _barrier(active, item, reader, msg['reader'], self.executable['copy'])
                reader['proof_size'] += len(preparation.canonical_json(proof))
                _check(active.get('capture_bytes', 0) + reader['proof_size'] <= preparation.MAX_EVIDENCE,
                       'aggregate reader barrier evidence exceeds bound')
                if phase == 'after':
                    _check(reader['before'] is not None and proof['reader'] == reader['before']['reader'],
                           'reader capturing thread changed between barriers')
                reader[phase] = proof
                fault = self._test_event('reader_' + phase)
                if fault and self.test_control[1] == 'reader_timeout':
                    reader['stage'] = 'fault_timeout'
                    self._record_reader_fault('reader_before', 'reader_timeout')
                    return
                reply_binding = binding
                if fault and self.test_control[1] == 'reader_nonzero':
                    reply_binding = {**binding, 'nonce': '0' * 64 if binding['nonce'] != '0' * 64 else '1' * 64}
                else:
                    _validate_capture_pins(active, item)
                allow = {'version': 1, 'op': 'reader_allow_read' if phase == 'before' else 'reader_allow_payload',
                         'binding': reply_binding}
                if fault and self.test_control[1] == 'reader_nonzero':
                    reader['fault_frame'] = allow
                self._queue(reader['out'], allow,
                            marker='reader_nonzero_frame' if fault and self.test_control[1] == 'reader_nonzero' else None)
                reader['stage'] = 'after' if phase == 'before' else 'payload'
                return
            _check(reader['stage'] == 'payload', 'reader frame out of order')
            if msg.get('op') == 'capture_chunk':
                preparation.fields(msg, ('version', 'op', 'seq', 'data'), 'reader chunk')
                _check(type(msg['version']) is int and msg['version'] == 1 and type(msg['seq']) is int and
                       msg['seq'] == len(reader['parts']) and type(msg['data']) is str, 'reader chunk order')
                raw = base64.b64decode(msg['data'], validate=True)
                _check(0 < len(raw) <= preparation.MAX_CHUNK and
                       base64.b64encode(raw).decode('ascii') == msg['data'], 'reader chunk encoding')
                reader['size'] += len(raw)
                _check(active.get('capture_bytes', 0) + reader['proof_size'] + reader['size'] <=
                       preparation.MAX_EVIDENCE, 'aggregate reader payload exceeds bound')
                reader['parts'].append(raw)
                return
            preparation.fields(msg, ('version', 'op', 'binding', 'payload_digest'), 'reader terminal')
            _check(type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'capture_terminal' and
                   preparation.canonical_json(msg['binding']) == preparation.canonical_json(binding) and
                   reader['parts'], 'reader terminal binding')
            preparation.hex_string(msg['payload_digest'], 'reader payload digest')
            raw = b''.join(reader['parts'])
            _check(hashlib.sha256(raw).hexdigest() == msg['payload_digest'], 'reader payload digest mismatch')
            payload = preparation.strict_json(raw)
            _check(preparation.canonical_json(payload) == raw, 'reader payload is not canonical')
            preparation.fields(payload, ('binding', 'helper_mountinfo', 'metadata_mountinfo', 'metadata_identity',
                                         'reader_before', 'reader_after'), 'reader payload')
            _check(preparation.canonical_json(payload['binding']) == preparation.canonical_json(binding) and
                   preparation.canonical_json(payload['reader_before']) == preparation.canonical_json(reader['before']['reader']) and
                   preparation.canonical_json(payload['reader_after']) == preparation.canonical_json(reader['after']['reader']),
                   'reader payload identity drift')
            preparation._complete_mountinfo(payload['helper_mountinfo'], 'reader helper')
            if item['metadata_owner'] is None:
                _check(payload['metadata_mountinfo'] is None and payload['metadata_identity'] is None,
                       'unexpected reader metadata graph')
            else:
                preparation._complete_mountinfo(payload['metadata_mountinfo'], 'reader metadata')
                preparation.fields(payload['metadata_identity'], ('source', 'target'), 'metadata identity')
                preparation._object_identity(payload['metadata_identity']['source'], 'metadata source')
                preparation._object_identity(payload['metadata_identity']['target'], 'metadata target')
                _check(preparation.canonical_json(payload['metadata_identity']) ==
                       preparation.canonical_json(_metadata_objects(item)), 'metadata object identity changed')
            _validate_capture_pins(active, item)
            reader['payload'] = payload
            reader['payload_digest'] = msg['payload_digest']
            reader['terminal'] = True
            reader['stage'] = 'exit'
            reader['exit_deadline'] = min(reader['deadline'], time.monotonic() + 5)
            self._queue(reader['out'], {'version': 1, 'op': 'reader_allow_exit', 'binding': binding})
        finally:
            for fd in fds:
                os.close(fd)

    def _finish_reader(self):
        reader, item, active = self.reader, self.pending_capture, self.active
        _check(reader is not None and reader['pidfd'] is not None and reader['eof'] and
               reader.get('wait_status') is None and select.select([reader['pidfd']], [], [], 0)[0],
               'reader exit is not fully observed')
        waited, status = os.waitpid(reader['pid'], os.WNOHANG)
        _check(waited == reader['pid'], 'reader direct wait is unavailable')
        reader['wait_status'] = status
        raw = os.pread(reader['capture'], CAPTURE_LIMIT + 1, 0)
        _check(len(raw) <= CAPTURE_LIMIT, 'reader diagnostic limit exceeded')
        exit_code = os.WEXITSTATUS(status) if os.WIFEXITED(status) else None
        exited = {'binding': item['binding'], 'host_pid': reader['pid'], 'starttime': reader['starttime'],
                  'terminal': reader['terminal'], 'terminal_digest': reader['payload_digest'], 'eof': True,
                  'pidfd_exited': True, 'wait_status': status, 'exit_code': exit_code,
                  'diagnostic': base64.b64encode(raw).decode('ascii')}
        record = active['captures'][-1]
        _check(record['launch'] is not None, 'reader exit without launch evidence')
        record['exit'] = exited
        if (self.test_control is not None and self.test_control[1] == 'reader_nonzero' and
                self.injected_frame is not None):
            _check(item['binding']['phase'] == 'before' and type(item['binding']['sequence']) is int and
                   item['binding']['sequence'] == 1 and self.expected_failure == 'reader_barrier_binding' and
                   self.reader_faults == [{'scenario': 'reader_nonzero', 'point': 'reader_before',
                                           'binding': item['binding'], 'effect': 'reader_nonzero_frame'}] and
                   not reader['out'] and exited['terminal'] is False and exited['terminal_digest'] is None and
                   exited['wait_status'] == 256 and exited['exit_code'] == 1,
                   'injected reader exit does not match the fault')
            event = {'version': 1, 'op': 'reader_fault_exit', 'scenario': 'reader_nonzero',
                     'binding': item['binding'], 'root_pid': os.getpid(), 'preparer_pid': active['pid'],
                     'reader_pid': reader['pid'], 'reader_starttime': reader['starttime'], 'exit': exited}
            reader['exit_event'] = event
            reader['stage'] = 'exit_observation_pending'
            self._queue(self.test_out, event, marker='reader_fault_exit')
            return
        self._publish_reader_exit()

    def _publish_reader_exit(self):
        reader, item, active = self.reader, self.pending_capture, self.active
        _check(reader is not None and item is not None and active is not None and
               reader.get('wait_status') is not None and active['captures'][-1]['exit'] is not None,
               'reader exit was not retained')
        status = reader['wait_status']
        record = active['captures'][-1]
        exited = record['exit']
        exit_code = exited['exit_code']
        self._queue(self.control_out, {'version': 1, 'op': 'capture_exit', 'binding': item['binding'],
                                      'exit': exited})
        success = (reader['terminal'] and reader['before'] is not None and reader['after'] is not None and
                   reader['payload'] is not None and status == 0 and exit_code == 0 and not reader['out'])
        if success:
            _validate_capture_pins(active, item)
            root = {'intent_sha256': record['intent_sha256'], 'launch': record['launch'],
                    'before': reader['before'], 'after': reader['after'],
                    'payload_digest': reader['payload_digest'], 'exit': exited}
            captured = {**reader['payload'], 'root': root}
            raw_capture = preparation.canonical_json(captured)
            total = (active.get('capture_bytes', 0) + reader['proof_size'] + reader['size'] +
                     len(raw_capture) + len(preparation.canonical_json(record)))
            _check(total <= preparation.MAX_EVIDENCE, 'combined reader evidence exceeds bound')
            active['capture_bytes'] = total
            digest = hashlib.sha256(raw_capture).hexdigest()
            for number, offset in enumerate(range(0, len(raw_capture), preparation.MAX_CHUNK)):
                chunk = raw_capture[offset:offset + preparation.MAX_CHUNK]
                self._queue(active['capture_out'], {'version': 1, 'op': 'capture_chunk', 'seq': number,
                                                    'data': base64.b64encode(chunk).decode('ascii')})
            result = {'version': 1, 'op': 'capture_result', 'binding': item['binding'], 'status': 'captured',
                      'capture_digest': digest, 'error': None}
        else:
            active['capture_failed'] = True
            result = {'version': 1, 'op': 'capture_result', 'binding': item['binding'], 'status': 'rejected',
                      'capture_digest': None, 'error': {'code': 'prepare_exit_uncertain',
                                                       'message': 'reader did not complete capture and exit cleanly'}}
        self._queue(active['capture_out'], result)
        for fd in item['fds']:
            os.close(fd)
        reader['socket'].close()
        os.close(reader['capture'])
        os.close(reader['pidfd'])
        if reader['error'] is not None:
            os.close(reader['error'])
        self.reader = None
        self.pending_capture = None

    def _handle_test_ack(self):
        sock, scenario = self.test_control
        msg, fds = _recv_owned(sock, (self.test_peer, 0, 0))
        try:
            _check(msg is not None and not fds, 'fault exit ACK ended or carried descriptors')
            reader = self.reader
            _check(not self.uncertain and scenario == 'reader_nonzero' and reader is not None and
                   reader['stage'] == 'exit_observation_awaiting_ack' and not self.test_out,
                   'unexpected fault exit ACK')
            preparation.fields(msg, ('version', 'op', 'scenario', 'binding', 'exit_sha256'), 'fault exit ACK')
            event = reader['exit_event']
            expected = {'version': 1, 'op': 'reader_fault_exit_ack', 'scenario': scenario,
                        'binding': event['binding'],
                        'exit_sha256': hashlib.sha256(preparation.canonical_json(event['exit'])).hexdigest()}
            _check(preparation.canonical_json(msg) == preparation.canonical_json(expected),
                   'fault exit ACK binding')
            self.expire()
            active = self.active
            _check(active is not None and active.get('pidfd') is not None and
                   not select.select([active['pidfd']], [], [], 0)[0], 'preparer exited before fault ACK')
            self._publish_reader_exit()
        finally:
            for fd in fds:
                os.close(fd)

    def _flush_test(self):
        for marker in self._flush(self.test_control[0], self.test_out):
            _check(marker == 'reader_fault_exit' and self.reader is not None and
                   self.reader['stage'] == 'exit_observation_pending', 'fault exit send ordering')
            self.reader['stage'] = 'exit_observation_awaiting_ack'

    def event_sources(self):
        readable, writable = [], []
        active, reader = self.active, self.reader
        if self.control_out:
            writable.append(self.sock)
        if active is not None:
            if not active.get('capture_eof'):
                readable.append(active['capture_socket'])
            if active['capture_out']:
                writable.append(active['capture_socket'])
        if reader is not None:
            if reader.get('pidfd') is not None and reader.get('wait_status') is None:
                readable.append(reader['pidfd'])
            if reader.get('error') is not None and reader['stage'] == 'exec':
                readable.append(reader['error'])
            if not reader['eof']:
                readable.append(reader['socket'])
            if reader['out']:
                writable.append(reader['socket'])
        if self.test_control is not None and self.test_control[1] == 'reader_nonzero':
            readable.append(self.test_control[0])
            if self.test_out:
                writable.append(self.test_control[0])
        return readable, writable

    def dispatch(self, readable, writable):
        if self.reader is not None and self.reader.get('wait_status') is not None:
            self.expire()
            active = self.active
            _check(active is not None and active.get('pidfd') is not None and
                   not select.select([active['pidfd']], [], [], 0)[0], 'preparer exited during fault barrier')
        if self.sock in writable:
            self.flush_control()
        active = self.active
        if active is not None and active['capture_socket'] in writable:
            self._flush(active['capture_socket'], active['capture_out'])
        reader = self.reader
        if reader is not None and reader['socket'] in writable:
            for marker in self._flush(reader['socket'], reader['out']):
                if marker == 'reader_nonzero_frame':
                    self._record_reader_fault('reader_before', marker)
                    self.injected_frame = reader['fault_frame']
                    self.expected_failure = 'reader_barrier_binding'
        reader = self.reader
        if reader is not None and reader.get('error') is not None and reader['error'] in readable:
            self.handle_reader_exec()
        reader = self.reader
        if reader is not None and reader['socket'] in readable:
            self.handle_reader()
        reader = self.reader
        if (reader is not None and reader.get('pidfd') is not None and
                reader.get('wait_status') is None and reader['pidfd'] in readable):
            if reader['stage'] == 'exec':
                raise preparation.ProtocolError('reader exited before executable identity')
            if reader['eof']:
                self._finish_reader()
        active = self.active
        if active is not None and active['capture_socket'] in readable:
            self.handle_capture()
        if self.test_control is not None and self.test_control[1] == 'reader_nonzero':
            test_sock = self.test_control[0]
            if test_sock in readable:
                self._handle_test_ack()
            if test_sock in writable and self.test_out:
                self.expire()
                self._flush_test()

    def progress_socket(self):
        return None if self.active is None else self.active.get('progress')

    def handle_progress(self):
        active = self.active
        _check(active is not None and active['progress'] is not None and active['fault'] is None,
               'unexpected preparer progress')
        msg, _ = preparation.recv_frame(active['progress'], (active['pid'], 0, 0), 0)
        if msg.get('op') == 'progress_chunk':
            preparation.fields(msg, ('version', 'op', 'seq', 'data'), 'progress chunk')
            _check(type(msg['version']) is int and msg['version'] == 1 and type(msg['seq']) is int and
                   msg['seq'] == len(active['progress_parts']) and type(msg['data']) is str, 'progress sequence')
            chunk = base64.b64decode(msg['data'], validate=True)
            _check(0 < len(chunk) <= preparation.MAX_CHUNK and
                   base64.b64encode(chunk).decode('ascii') == msg['data'], 'progress chunk')
            active['progress_size'] += len(chunk)
            _check(active['progress_size'] <= preparation.MAX_EVIDENCE, 'progress evidence limit')
            active['progress_parts'].append(chunk)
            return None
        preparation.fields(msg, ('version', 'op', 'point', 'request_digest', 'evidence_digest'), 'progress marker')
        expected = 'before_attach' if not active['progress_snapshots'] else 'after_first_attach'
        _check(type(msg['version']) is int and msg['version'] == 1 and msg['op'] == 'prepare_progress' and
               msg['point'] == expected and msg['request_digest'] == active['request_digest'] and
               active['progress_parts'], 'progress marker binding')
        preparation.hex_string(msg['evidence_digest'], 'progress digest')
        raw = b''.join(active['progress_parts'])
        _check(hashlib.sha256(raw).hexdigest() == msg['evidence_digest'], 'progress digest mismatch')
        evidence = preparation.strict_json(raw)
        _check(preparation.canonical_json(evidence) == raw, 'noncanonical progress evidence')
        active['progress_snapshots'].append({'point': msg['point'], 'evidence': evidence,
                                             'evidence_sha256': msg['evidence_digest']})
        if msg['point'] != active['progress_point']:
            _check(msg['point'] == 'before_attach' and active['progress_point'] == 'after_first_attach',
                   'unexpected progress point')
            preparation.send_frame(active['progress'], {'version': 1, 'op': 'progress_continue',
                                                       'point': msg['point'],
                                                       'request_digest': msg['request_digest']})
            active['progress_parts'] = []
            active['progress_size'] = 0
            return None
        fault = {'point': msg['point'], 'request_digest': msg['request_digest'], 'pid': active['pid'],
                 'ordinal': active['ordinal'], 'nonce': active['nonce'],
                 'progress_snapshots': active['progress_snapshots'], 'evidence': evidence,
                 'evidence_sha256': msg['evidence_digest'], 'signaled_monotonic_ns': time.monotonic_ns()}
        signal.pidfd_send_signal(active['pidfd'], signal.SIGKILL)
        active['fault'] = fault
        self.fault_receipts.append(fault)
        active['progress'].close()
        active['progress'] = None
        return fault

    def time_left(self):
        if self.active is None:
            return None
        deadlines = [self.active['deadline']]
        if self.reader is not None:
            deadlines.append(self.reader['deadline'])
            if self.reader['stage'] == 'exec':
                deadlines.append(self.reader['exec_deadline'])
            if self.reader['stage'] == 'exit':
                deadlines.append(self.reader['exit_deadline'])
        return max(0, min(deadlines) - time.monotonic())

    def expire(self):
        remaining = self.time_left()
        if remaining is not None and remaining <= 0:
            if self.active is not None:
                self.active['timeout'] = True
            raise preparation.ProtocolError('preparer or reader deadline exceeded')

    def contain(self):
        """Boundedly contain direct children without signaling an unpinned numeric PID."""
        self.uncertain = True
        self.test_out.clear()
        reader, active = self.reader, self.active
        evidence = {'reader_pid': None if reader is None else reader.get('pid'),
                    'reader_pidfd_held': reader is not None and reader.get('pidfd') is not None,
                    'reader_signal_sent': False, 'reader_eof': reader is not None and reader.get('eof', False),
                    'reader_pidfd_exited': reader is not None and reader.get('wait_status') is not None and
                                           reader.get('pidfd') is not None,
                    'reader_wait_status': None if reader is None else reader.get('wait_status'),
                    'preparer_pid': None if active is None else active['pid'],
                    'preparer_pidfd_held': active is not None and active.get('pidfd') is not None,
                    'preparer_signal_sent': False,
                    'preparer_pidfd_exited': active is not None and active.get('wait_status') is not None and
                                             active.get('pidfd') is not None,
                    'preparer_wait_status': None if active is None else active.get('wait_status')}
        self.containment = evidence
        for process, key in ((reader, 'reader'), (active, 'preparer')):
            if process is not None and process.get('pidfd') is not None and process.get('wait_status') is None:
                try:
                    signal.pidfd_send_signal(process['pidfd'], signal.SIGKILL)
                    evidence[key + '_signal_sent'] = True
                except ProcessLookupError:
                    pass
        if reader is not None and reader.get('pidfd') is None:
            reader['socket'].close()  # EOF releases the unadmitted child; never signal a numeric PID.
        if active is not None and active.get('pidfd') is None:
            active['capture_socket'].close()
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            watched = []
            for process, key in ((reader, 'reader'), (active, 'preparer')):
                if process is None or process.get('pid') is None or evidence[key + '_wait_status'] is not None:
                    continue
                pidfd = process.get('pidfd')
                exited = pidfd is not None and bool(select.select([pidfd], [], [], 0)[0])
                if exited:
                    evidence[key + '_pidfd_exited'] = True
                if pidfd is None or exited:
                    try:
                        waited, status = os.waitpid(process['pid'], os.WNOHANG)
                    except ChildProcessError:
                        waited, status = 0, None
                    if waited == process['pid']:
                        process['wait_status'] = status
                        evidence[key + '_wait_status'] = status
                elif pidfd is not None:
                    watched.append(pidfd)
            if reader is not None and reader.get('pidfd') is not None and not evidence['reader_eof']:
                watched.append(reader['socket'])
            reader_done = reader is None or reader.get('pid') is None or evidence['reader_wait_status'] is not None
            prep_done = active is None or evidence['preparer_wait_status'] is not None
            if reader_done and prep_done and (reader is None or reader.get('pidfd') is None or evidence['reader_eof']):
                break
            timeout = min(0.05, max(0, deadline - time.monotonic()))
            ready, _, _ = select.select(watched, [], [], timeout)
            if reader is not None and reader.get('pidfd') is not None and reader['socket'] in ready:
                try:
                    frame, received = _recv_owned(reader['socket'], (reader['pid'], 0, 0))
                    for fd in received:
                        os.close(fd)
                    if frame is None:
                        evidence['reader_eof'] = True
                except (OSError, ValueError):
                    pass
        if (reader is not None and active is not None and self.pending_capture is not None and
                active['captures'] and active['captures'][-1]['launch'] is not None and
                active['captures'][-1]['exit'] is None and evidence['reader_pidfd_exited'] and
                evidence['reader_wait_status'] is not None):
            status = evidence['reader_wait_status']
            raw = os.pread(reader['capture'], CAPTURE_LIMIT + 1, 0)
            if len(raw) <= CAPTURE_LIMIT:
                active['captures'][-1]['exit'] = {
                    'binding': reader['binding'], 'host_pid': reader['pid'], 'starttime': reader['starttime'],
                    'terminal': reader['terminal'], 'terminal_digest': reader['payload_digest'],
                    'eof': evidence['reader_eof'], 'pidfd_exited': True, 'wait_status': status,
                    'exit_code': os.WEXITSTATUS(status) if os.WIFEXITED(status) else None,
                    'diagnostic': base64.b64encode(raw).decode('ascii')}
        return evidence

    def reap(self):
        active = self.active
        _check(active is not None and active.get('pidfd') is not None and self.reader is None and
               self.pending_capture is None and not self.control_out and not active['capture_out'] and
               active.get('capture_eof') and select.select([active['pidfd']], [], [], 0)[0],
               'preparer or capture ownership still active')
        waited, status = os.waitpid(active['pid'], os.WNOHANG)
        _check(waited == active['pid'], 'preparer waitpid identity')
        active['wait_status'] = status
        raw = os.pread(active['capture'], CAPTURE_LIMIT + 1, 0)
        _check(len(raw) <= CAPTURE_LIMIT, 'preparer capture exceeded bound')
        receipt = self._identity('preparer_exit')
        receipt.update(ordinal=active['ordinal'], nonce=active['nonce'], pid=active['pid'], wait_status=status,
                       exit_code=os.WEXITSTATUS(status) if os.WIFEXITED(status) else None,
                       capture=raw.decode('ascii', 'backslashreplace'), captures=active['captures'])
        self._queue(self.control_out, receipt)
        self.exits.append(receipt)
        if active.get('fault') is not None:
            active['fault']['wait_status'] = status
            active['fault']['exit_observed_monotonic_ns'] = time.monotonic_ns()
        if active.get('progress') is not None:
            active['progress'].close()
        active['capture_socket'].close()
        self.last_captures = active['captures']
        os.close(active['capture'])
        os.close(active['pidfd'])
        self.active = None
        return receipt

    def release_guard(self, coordinator_pidfd, status):
        _check(self.retired and self.active is None and self.reader is None and self.pending_capture is None and
               not self.control_out and not self.uncertain and self.test_arm is None, 'unsafe coordinator retirement')
        final = validate_retired_ledger('/lab/control', COORDINATOR_UID, self.run_token, self.session_nonce,
                                        self.retired_ledger, self.launches, self.exits)
        _check(final == self.retirement_readback, 'retirement readback changed')
        self.guard.close_after_exit(coordinator_pidfd, status, self.retired, self.active is None)
        os.close(self.root_pidfd)
