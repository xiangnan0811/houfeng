"""Strict wire protocol for the isolated guest's one-shot mount preparer.

The caller retains its input pidfds.  Received descriptors are close-on-exec and
ownership passes to the caller only on a successful receive.  This module does
not perform mount operations or infer a successful process exit from a result.
"""
import array
import base64
import errno
import hashlib
import json
import os
import posixpath
import re
import select
import socket
import stat
import struct
import time

MAX_FRAME = 64 * 1024
MAX_CHUNK = 32 * 1024
MAX_EVIDENCE = 4 * 1024 * 1024
HEX64 = re.compile(r'^[0-9a-f]{64}$')
HEX32 = re.compile(r'^[0-9a-f]{32}$')
CAP_MASK = '00000000002c0004'
REASONS = {'prepare_protocol', 'prepare_identity_changed', 'prepare_source_unmapped',
           'prepare_mount_rejected', 'prepare_exit_uncertain', 'observer_not_ready'}


class ProtocolError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise ProtocolError(message)


def fields(value, expected, name):
    require(type(value) is dict and set(value) == set(expected), name + ' fields')


def integer(value, name, minimum=0, maximum=(1 << 64) - 1):
    require(type(value) is int and minimum <= value <= maximum, name + ' integer')
    return value


def ascii_string(value, name, maximum=4096):
    require(type(value) is str and len(value) <= maximum and value.isascii() and '\x00' not in value,
            name + ' ASCII string')
    return value


def clean_absolute_path(value):
    """Recognize a filesystem path unchanged by Go's path.Clean."""
    return (type(value) is str and value.startswith('/') and not value.startswith('//') and
            '\x00' not in value and posixpath.normpath(value) == value)


def namespace_root(value, fs_type):
    """Recognize exactly the nsfs namespace-root spelling emitted by Linux."""
    if fs_type != 'nsfs' or type(value) is not str:
        return False
    match = re.fullmatch(r'(?:cgroup|ipc|mnt|net|pid|time|user|uts):\[([1-9][0-9]{0,19})\]', value)
    return match is not None and int(match.group(1)) <= (1 << 64) - 1


def cgroup_ancestor_root(value, fs_type):
    """Accept reader-relative ancestor roots only as cgroup graph evidence."""
    if fs_type not in ('cgroup', 'cgroup2') or type(value) is not str or not value.startswith('/'):
        return False
    if not value.isascii() or '\x00' in value:
        return False
    parts = value[1:].split('/')
    if not parts or parts[0] != '..':
        return False
    index = 0
    while index < len(parts) and parts[index] == '..':
        index += 1
    return all(part not in ('', '.', '..') for part in parts[index:])


def hex_string(value, name, pattern=HEX64):
    require(type(value) is str and pattern.fullmatch(value) is not None, name + ' digest or token')
    return value


def _json_value(value):
    if value is None or type(value) is bool:
        return
    if type(value) is int:
        integer(value, 'JSON number', -(1 << 63))
        return
    if type(value) is str:
        ascii_string(value, 'JSON string', MAX_EVIDENCE)
        return
    if type(value) is list:
        for item in value:
            _json_value(item)
        return
    require(type(value) is dict, 'unsupported JSON value')
    for key, item in value.items():
        ascii_string(key, 'JSON key', 1024)
        _json_value(item)


def canonical_json(value):
    _json_value(value)
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False,
                      allow_nan=False).encode('ascii')


def strict_json(data):
    require(type(data) is bytes and len(data) <= MAX_EVIDENCE, 'invalid JSON byte length')
    try:
        text = data.decode('ascii')

        def pairs(items):
            obj = {}
            for key, value in items:
                if key in obj:
                    raise ProtocolError('duplicate JSON key')
                obj[key] = value
            return obj

        def number(token):
            if token == '-0':
                raise ProtocolError('noncanonical integer')
            return int(token)

        def no_float(_token):
            raise ProtocolError('noninteger JSON number')

        value = json.loads(text, object_pairs_hook=pairs, parse_int=number,
                           parse_float=no_float, parse_constant=no_float)
        _json_value(value)
        return value
    except (UnicodeError, json.JSONDecodeError, RecursionError) as exc:
        raise ProtocolError('invalid JSON') from exc


def input_digest(value):
    return hashlib.sha256(b'G2-PREPARE-INPUT-V1\0' + canonical_json(value)).hexdigest()


def request_digest(value):
    require(type(value) is dict and 'request_digest' not in value, 'request digest input fields')
    return hashlib.sha256(b'G2-PREPARE-V1\0' + canonical_json(value)).hexdigest()


def canonical_vector():
    value = {'version': 1, 'run_token': '0123456789abcdef0123456789abcdef', 'ordinal': 1,
             'nonce': '1' * 64, 'imageID': 'sha256:' + '2' * 64, 'case': 'C0',
             'phase': 'before', 'profile': 'A',
             'roots': [{'kind': 'declared_bind', 'ordinal': 0, 'source': '/lab/ext',
                        'target': '/roots/000'}],
             'metadata_pid': None, 'exe': False, 'nested': False, 'deny': False,
             'preparer_sha256': '3' * 64}
    request = {'version': 1, 'op': 'prepare', 'input': value, 'input_digest': input_digest(value),
               'reader_cgroupns': {'dev': 9, 'ino': 10},
               'helper': {'id': '4' * 64, 'pid': 123, 'starttime': 456,
                          'mntns': {'dev': 5, 'ino': 6}, 'pidns': {'dev': 7, 'ino': 8},
                          'cgroupns': {'dev': 11, 'ino': 12}},
               'daemon': {'pid': 7}, 'target': None}
    return {'input': canonical_json(value).decode('ascii'), 'input_digest': input_digest(value),
            'request': canonical_json(request).decode('ascii'), 'request_digest': request_digest(request)}


def identity(value, name):
    fields(value, ('dev', 'ino'), name)
    integer(value['dev'], name + ' dev')
    integer(value['ino'], name + ' ino', 1)


def _absolute_lab_path(value, name):
    ascii_string(value, name)
    require(value.startswith('/lab/') and posixpath.normpath(value) == value, name + ' path')
    require(not any(ord(ch) < 0x20 for ch in value), name + ' control character')


def validate_input(value):
    fields(value, ('version', 'run_token', 'ordinal', 'nonce', 'imageID', 'case', 'phase',
                   'profile', 'roots', 'metadata_pid', 'exe', 'nested', 'deny', 'preparer_sha256'), 'input')
    require(type(value['version']) is int and value['version'] == 1, 'input version')
    hex_string(value['run_token'], 'run token', HEX32)
    integer(value['ordinal'], 'launch ordinal', 1, (1 << 63) - 1)
    hex_string(value['nonce'], 'nonce')
    require(type(value['imageID']) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', value['imageID']), 'imageID')
    require(value['case'] in ('C0', 'V1', 'V2', 'V3', 'I0', 'I1', 'I2', 'I3'), 'case')
    require(value['phase'] in ('before', 'after') and value['profile'] in ('A', 'B'), 'phase/profile')
    for name in ('exe', 'nested', 'deny'):
        require(type(value[name]) is bool, name + ' boolean')
    require(not value['exe'] or value['case'] == 'I0', 'exe case')
    require(not value['nested'] or value['case'] == 'V2', 'nested case')
    if value['metadata_pid'] is not None:
        integer(value['metadata_pid'], 'metadata PID', 1, (1 << 31) - 1)
    hex_string(value['preparer_sha256'], 'preparer digest')
    roots = value['roots']
    require(type(roots) is list and 1 <= len(roots) <= 16, 'root count')
    for number, root in enumerate(roots):
        require(type(root) is dict and root.get('kind') in ('declared_bind', 'native_volume', 'running_root'),
                'root kind')
        kind = root['kind']
        extra = {'declared_bind': ('source',), 'native_volume': ('name', 'mountpoint'), 'running_root': ()}[kind]
        fields(root, ('kind', 'ordinal', 'target') + extra, 'root')
        require(type(root['ordinal']) is int and root['ordinal'] == number and
                root['target'] == '/roots/%03d' % number, 'root ordinal/target')
        if kind == 'declared_bind':
            _absolute_lab_path(root['source'], 'bind source')
        elif kind == 'native_volume':
            name = ascii_string(root['name'], 'volume name', 255)
            require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', name) is not None, 'volume name')
            require(root['mountpoint'] == '/lab/ext/docker/volumes/' + name + '/_data', 'volume mountpoint')
        else:
            require(number == 1 and value['case'] in ('I1', 'I2', 'I3'), 'running root scope')
    return value


def validate_request(value):
    fields(value, ('version', 'op', 'input', 'input_digest', 'request_digest', 'reader_cgroupns',
                   'helper', 'daemon', 'target'), 'prepare request')
    require(type(value['version']) is int and value['version'] == 1 and value['op'] == 'prepare', 'prepare op')
    inp = validate_input(value['input'])
    hex_string(value['input_digest'], 'input digest')
    hex_string(value['request_digest'], 'request digest')
    require(value['input_digest'] == input_digest(inp), 'input digest mismatch')
    unsigned = {key: item for key, item in value.items() if key != 'request_digest'}
    require(value['request_digest'] == request_digest(unsigned), 'request digest mismatch')
    identity(value['reader_cgroupns'], 'reader cgroup namespace')
    helper = value['helper']
    fields(helper, ('id', 'pid', 'starttime', 'mntns', 'pidns', 'cgroupns'), 'helper')
    hex_string(helper['id'], 'helper ID')
    integer(helper['pid'], 'helper PID', 1, (1 << 31) - 1)
    integer(helper['starttime'], 'helper starttime', 1)
    identity(helper['mntns'], 'helper mount namespace')
    identity(helper['pidns'], 'helper PID namespace')
    identity(helper['cgroupns'], 'helper cgroup namespace')
    daemon = value['daemon']
    fields(daemon, ('pid',), 'daemon')
    integer(daemon['pid'], 'daemon PID', 1, (1 << 31) - 1)
    target = value['target']
    if target is not None:
        fields(target, ('id', 'pid', 'starttime', 'destination'), 'target')
        hex_string(target['id'], 'target ID')
        integer(target['pid'], 'target PID', 1, (1 << 31) - 1)
        integer(target['starttime'], 'target starttime', 1)
        require(target['destination'] == '/data' and inp['case'] in ('I1', 'I2', 'I3'), 'target scope')
        require(inp['metadata_pid'] == target['pid'], 'target metadata PID mismatch')
    if inp['exe']:
        require(target is None and inp['metadata_pid'] == daemon['pid'], 'exe PID mismatch')
    if any(root['kind'] == 'running_root' for root in inp['roots']):
        require(target is not None, 'running root without target')
    if inp['metadata_pid'] is not None:
        require((inp['exe'] and inp['metadata_pid'] == daemon['pid']) or
                (target is not None and inp['metadata_pid'] == target['pid']), 'unbound metadata PID')
    require(len({helper['pid'], daemon['pid']} | ({target['pid']} if target else set())) ==
            (3 if target else 2), 'duplicate process PID')
    return value


def send_frame(sock, value, fds=()):
    data = canonical_json(value)
    require(0 < len(data) <= MAX_FRAME, 'frame size')
    rights = []
    if fds:
        require(len(fds) <= 16 and all(type(fd) is int and fd >= 0 for fd in fds), 'descriptor list')
        rights = [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', fds))]
    sent = sock.sendmsg([data], rights, getattr(socket, 'MSG_NOSIGNAL', 0))
    require(sent == len(data), 'short datagram write')


def recv_frame(sock, expected_cred, fd_count=0, allow_eof=False):
    """Receive one credentialled datagram, closing every delivered FD on failure."""
    require(fd_count is None or (type(fd_count) is int and 0 <= fd_count <= 16), 'invalid expected FD count')
    ancsize = socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(256 * array.array('i').itemsize)
    received_fds = []
    try:
        data, ancillary, flags, _ = sock.recvmsg(MAX_FRAME + 1, ancsize, getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
        credentials = []
        unknown = False
        rights_messages = 0
        for level, kind, payload in ancillary:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                rights_messages += 1
                fds = array.array('i')
                usable = len(payload) - len(payload) % fds.itemsize
                fds.frombytes(payload[:usable])
                received_fds.extend(fds)
                if usable != len(payload):
                    unknown = True
            elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS:
                if len(payload) == struct.calcsize('3i'):
                    credentials.append(struct.unpack('3i', payload))
                else:
                    unknown = True
            else:
                unknown = True
        for fd in received_fds:
            os.set_inheritable(fd, False)
        if allow_eof and not data and not ancillary and not flags & ~getattr(socket, 'MSG_CMSG_CLOEXEC', 0):
            return None, []
        require(not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) and 0 < len(data) <= MAX_FRAME,
                'empty or truncated frame')
        require(not unknown and credentials == [tuple(expected_cred)], 'credentials or ancillary data mismatch')
        require(len(received_fds) <= 16 and rights_messages == (1 if received_fds else 0) and
                (fd_count is None or len(received_fds) == fd_count), 'unexpected rights')
        obj = strict_json(data)
        require(type(obj) is dict and canonical_json(obj) == data, 'frame is not a canonical object')
        return obj, received_fds
    except BaseException:
        for fd in received_fds:
            try:
                os.close(fd)
            except OSError:
                pass
        raise


def _wait_read(sock, deadline):
    remaining = deadline - time.monotonic()
    require(remaining > 0 and bool(select.select([sock], [], [], remaining)[0]), 'protocol deadline')


def _send_bounded(sock, value, fds, deadline):
    previous = sock.gettimeout()
    sock.setblocking(False)
    try:
        while True:
            remaining = deadline - time.monotonic()
            require(remaining > 0 and bool(select.select([], [sock], [], remaining)[1]), 'send deadline')
            try:
                send_frame(sock, value, fds)
                return
            except BlockingIOError:
                continue
    finally:
        sock.settimeout(previous)


def _receive(sock, credentials, fd_count, deadline):
    _wait_read(sock, deadline)
    return recv_frame(sock, credentials, fd_count)


def validate_pidfd(fd, pid):
    integer(fd, 'pidfd', 0, (1 << 31) - 1)
    integer(pid, 'pidfd PID', 1, (1 << 31) - 1)
    try:
        info = os.open('/proc/self/fdinfo/%d' % fd, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            data = os.read(info, 4097)
        finally:
            os.close(info)
        require(len(data) <= 4096, 'pidfd fdinfo too large')
        text = data.decode('ascii')
        matches = re.findall(r'^Pid:\s*([0-9]+)\s*$', text, re.M)
        require(len(matches) == 1 and int(matches[0]) == pid and
                re.search(r'^NSpid:\s*[0-9]+(?:\s+[0-9]+)*\s*$', text, re.M) is not None,
                'pidfd PID mismatch')
        require(not select.select([fd], [], [], 0)[0], 'pidfd already exited')
    except (OSError, UnicodeError, ValueError) as exc:
        raise ProtocolError('pidfd identity unavailable') from exc


def send_prepare(sock, request, pidfds):
    validate_request(request)
    require(len(pidfds) == (3 if request['target'] is not None else 2), 'pidfd count')
    pids = [request['daemon']['pid'], request['helper']['pid']]
    if request['target'] is not None:
        pids.append(request['target']['pid'])
    for fd, pid in zip(pidfds, pids):
        validate_pidfd(fd, pid)
    send_frame(sock, request, pidfds)


def _object_identity(value, name):
    fields(value, ('dev', 'ino', 'mode'), name)
    integer(value['dev'], name + ' dev')
    integer(value['ino'], name + ' ino', 1)
    integer(value['mode'], name + ' mode')


def _mount_record(value):
    fields(value, ('relative', 'mount_id', 'parent_id', 'root', 'major_minor', 'fs_type',
                   'source', 'options', 'optional', 'super_options'), 'mount record')
    for name in ('relative', 'root', 'major_minor', 'fs_type', 'source'):
        ascii_string(value[name], 'mount ' + name, 4096)
    integer(value['mount_id'], 'mount ID', 1)
    integer(value['parent_id'], 'parent mount ID', 1)
    for name in ('options', 'optional', 'super_options'):
        require(type(value[name]) is list and all(type(x) is str and x.isascii() for x in value[name]),
                'mount ' + name)

def _subtree(rows, mount_id, native_root):
    require(type(rows) is list and rows, 'empty mount subtree')
    require(clean_absolute_path(native_root), 'native root is not a clean absolute filesystem path')
    for item in rows:
        _mount_record(item)
        require(clean_absolute_path(item['root']), 'mount root is not a clean absolute filesystem path')
    relatives = [item['relative'] for item in rows]
    ids = [item['mount_id'] for item in rows]
    require(relatives == sorted(set(relatives)) and relatives[0] == '.' and len(ids) == len(set(ids)) and
            rows[0]['mount_id'] == mount_id and rows[0]['root'] == native_root, 'ambiguous mount subtree')
    for relative in relatives[1:]:
        require(not relative.startswith('/') and posixpath.normpath(relative) == relative and
                all(part not in ('', '.', '..') for part in relative.split('/')), 'invalid relative mount')


def _same_subtree(left, right):
    keys = ('relative', 'root', 'major_minor', 'fs_type', 'source')
    return len(left) == len(right) and all(tuple(a[k] for k in keys) == tuple(b[k] for k in keys)
                                           for a, b in zip(left, right))


def _private_readonly(rows):
    return all('ro' in row['options'] and 'rw' not in row['options'] and
               not any(field == 'unbindable' or field.startswith(('shared:', 'master:', 'propagate_from:'))
                       for field in row['optional']) for row in rows)


def _source_or_attachment(value, attached):
    common = ('role', 'ordinal', 'namespace', 'mount_id', 'dev', 'ino', 'mode', 'native_root', 'subtree')
    fields(value, common + (('target', 'before') if attached else ('clone', 'source_path', 'path_evidence')),
           'attachment' if attached else 'source')
    require(value['role'] in ('declared_bind', 'native_volume', 'running_root', 'daemon_exe'), 'source role')
    if value['role'] == 'daemon_exe':
        require(value['ordinal'] is None, 'exe ordinal')
    else:
        integer(value['ordinal'], 'root ordinal', 0, 15)
    identity(value['namespace'], 'source namespace')
    for name in ('mount_id', 'ino'):
        integer(value[name], name, 1)
    integer(value['dev'], 'source dev')
    integer(value['mode'], 'source mode')
    ascii_string(value['native_root'], 'native root')
    _subtree(value['subtree'], value['mount_id'], value['native_root'])
    if attached:
        target = '/meta/target-exe' if value['role'] == 'daemon_exe' else '/roots/%03d' % value['ordinal']
        require(value['target'] == target, 'attachment target')
        before = value['before']
        fields(before, ('dev', 'ino', 'mode', 'mount_id', 'native_root', 'subtree'), 'attachment before')
        _object_identity({key: before[key] for key in ('dev', 'ino', 'mode')}, 'attachment before')
        integer(before['mount_id'], 'before mount ID', 1)
        ascii_string(before['native_root'], 'before native root')
        _subtree(before['subtree'], before['mount_id'], before['native_root'])
    else:
        if value['clone'] is not None:
            _object_identity(value['clone'], 'clone')
        source_path = ascii_string(value['source_path'], 'source path')
        require(clean_absolute_path(source_path) and
                not source_path.endswith(' (deleted)') and not any(ord(ch) < 0x20 for ch in source_path),
                'source path')
        expected = {'declared_bind': 'declared_input', 'native_volume': 'native_mountpoint',
                    'running_root': 'target_root_data', 'daemon_exe': 'daemon_proc_exe_readlink'}
        require(value['path_evidence'] == expected[value['role']], 'source path evidence')


def _process_evidence(value, name):
    fields(value, ('pid', 'starttime', 'pidns', 'mntns', 'cgroupns', 'userns',
                   'before_mountinfo', 'after_mountinfo'), name)
    integer(value['pid'], name + ' PID', 1, (1 << 31) - 1)
    integer(value['starttime'], name + ' starttime', 1)
    for key in ('pidns', 'mntns', 'cgroupns', 'userns'):
        identity(value[key], name + ' ' + key)
    for key in ('before_mountinfo', 'after_mountinfo'):
        ascii_string(value[key], name + ' ' + key, MAX_EVIDENCE)


def _complete_mountinfo(value, name):
    text = ascii_string(value, name + ' mountinfo', MAX_EVIDENCE)
    require(bool(text) and text.endswith('\n'), 'incomplete ' + name + ' graph')
    import handoff
    try:
        handoff.custody_graph(text)
    except (ValueError, TypeError, OverflowError) as exc:
        raise ProtocolError('invalid ' + name + ' graph: ' + str(exc)) from exc


def validate_request_namespaces(request, coordinator_namespaces, waiting, final_status=None):
    """Bind both reader identities even when a preparer has no result."""
    validate_request(request)
    fields(coordinator_namespaces, ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'),
           'coordinator namespaces')
    for name, value in coordinator_namespaces.items():
        identity(value, 'coordinator ' + name)
    fields(waiting, ('nonce', 'input_digest', 'self_status'), 'observer waiting')
    require(waiting['nonce'] == request['input']['nonce'] and waiting['input_digest'] == request['input_digest'],
            'observer waiting request binding differs')
    require(request['reader_cgroupns'] == coordinator_namespaces['cgroup'],
            'coordinator cgroup namespace differs from request')
    statuses = [waiting['self_status']]
    if final_status is not None:
        statuses.append(final_status)
    for status in statuses:
        require(type(status) is dict and type(status.get('namespaces')) is dict,
                'observer namespaces missing')
        namespaces = status['namespaces']
        fields(namespaces, ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'), 'observer namespaces')
        for name, value in namespaces.items():
            identity(value, 'observer ' + name)
        require(namespaces['mnt'] == request['helper']['mntns'] and
                namespaces['pid'] == request['helper']['pidns'] and
                namespaces['cgroup'] == request['helper']['cgroupns'],
                'observer namespaces differ from request')


def capture_binding(value, request, session_nonce, phase=None):
    validate_request(request)
    hex_string(session_nonce, 'capture session nonce')
    fields(value, ('version', 'run_token', 'session_nonce', 'ordinal', 'nonce', 'input_digest',
                   'request_digest', 'phase', 'sequence'), 'capture binding')
    inp = request['input']
    require(type(value['version']) is int and value['version'] == 1 and
            type(value['ordinal']) is int and type(value['sequence']) is int and
            all(value[key] == inp[key] for key in ('run_token', 'ordinal', 'nonce')) and
            value['session_nonce'] == session_nonce and
            all(value[key] == request[key] for key in ('input_digest', 'request_digest')) and
            (value['phase'], value['sequence']) in (('before', 1), ('after', 2)) and
            (phase is None or value['phase'] == phase), 'capture binding mismatch')
    return value


def capture_process(value, request, name):
    fields(value, ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns', 'userns'), name)
    integer(value['pid'], name + ' PID', 1, (1 << 31) - 1)
    integer(value['starttime'], name + ' starttime', 1)
    for key in ('mntns', 'pidns', 'cgroupns', 'userns'):
        identity(value[key], name + ' ' + key)
    if name == 'capture helper':
        require(all(value[key] == request['helper'][key] for key in
                    ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns')), 'capture helper differs')
    else:
        require(value['pid'] == request['input']['metadata_pid'], 'capture metadata PID differs')
        if request['target'] is not None:
            require(value['starttime'] == request['target']['starttime'], 'capture metadata starttime differs')
    return value


def capture_intent(value, request, session_nonce, phase=None):
    fields(value, ('version', 'op', 'binding', 'helper', 'metadata_owner'), 'capture intent')
    require(type(value['version']) is int and value['version'] == 1 and value['op'] == 'capture_intent',
            'capture intent operation')
    capture_binding(value['binding'], request, session_nonce, phase)
    capture_process(value['helper'], request, 'capture helper')
    if request['input']['metadata_pid'] is None:
        require(value['metadata_owner'] is None, 'unexpected capture metadata owner')
    else:
        capture_process(value['metadata_owner'], request, 'capture metadata owner')
        require(value['metadata_owner']['userns'] == value['helper']['userns'], 'capture user namespace differs')
    return value


def _namespace_evidence(value, kind, owner, expected=None):
    fields(value, ('dev', 'ino', 'nstype', 'userns'), kind + ' namespace evidence')
    plain = {key: value[key] for key in ('dev', 'ino')}
    identity(plain, kind + ' namespace')
    identity(value['userns'], kind + ' namespace owner')
    nstypes = {'pid': 0x20000000, 'mnt': 0x00020000, 'cgroup': 0x02000000}
    require(type(value['nstype']) is int and value['nstype'] == nstypes[kind] and
            value['userns'] == owner and (expected is None or plain == expected),
            kind + ' namespace type, owner, or identity differs')
    return plain


def _thread_inventory(value, name):
    require(type(value) is list and value and len(value) <= 1024, name + ' inventory')
    for tid in value:
        integer(tid, name + ' TID', 1, (1 << 31) - 1)
    require(value == sorted(set(value)), name + ' inventory ordering')
    return value


def capture_launch(value, intent, request, session_nonce, root_pid=None, preparer_exe=None, preparer_pid=None):
    fields(value, ('binding', 'host_pid', 'starttime', 'exe', 'reader_pidns', 'root'), 'capture launch')
    require(value['binding'] == intent['binding'], 'capture launch binding differs')
    capture_binding(value['binding'], request, session_nonce)
    integer(value['host_pid'], 'reader host PID', 1, (1 << 31) - 1)
    integer(value['starttime'], 'reader starttime', 1)
    exe = value['exe']
    fields(exe, ('dev', 'ino', 'mode', 'sha256'), 'reader executable')
    _object_identity({key: exe[key] for key in ('dev', 'ino', 'mode')}, 'reader executable')
    require(stat.S_ISREG(exe['mode']) and not exe['mode'] & (stat.S_ISUID | stat.S_ISGID) and
            exe['sha256'] == request['input']['preparer_sha256'], 'reader executable differs')
    if preparer_exe is not None:
        require(all(exe[key] == preparer_exe[key] for key in ('dev', 'ino', 'mode')),
                'reader executable is not the held preparer')
    owner = intent['helper']['userns']
    _namespace_evidence(value['reader_pidns'], 'pid', owner, request['helper']['pidns'])
    root = value['root']
    fields(root, ('active_before', 'children_before', 'children_switched', 'active_after', 'children_after',
                  'tasks_before', 'tasks_after', 'sigchld_default', 'sigchld_nocldwait',
                  'signal_mask_restored'), 'root switch proof')
    for key in ('active_before', 'children_before', 'children_switched', 'active_after', 'children_after'):
        _namespace_evidence(root[key], 'pid', owner)
    require(root['active_before'] == root['children_before'] == root['active_after'] == root['children_after'] and
            root['children_switched'] == value['reader_pidns'], 'root PID namespace restoration differs')
    before = _thread_inventory(root['tasks_before'], 'root before')
    after = _thread_inventory(root['tasks_after'], 'root after')
    require(len(before) == 1 and before == after and (root_pid is None or before == [root_pid]) and
            root['sigchld_default'] is True and root['sigchld_nocldwait'] is False and
            root['signal_mask_restored'] is True, 'root fork safety proof differs')
    occupied = [request['daemon']['pid'], request['helper']['pid'], before[0]]
    if request['target'] is not None:
        occupied.append(request['target']['pid'])
    if preparer_pid is not None:
        occupied.append(integer(preparer_pid, 'preparer PID', 1, (1 << 31) - 1))
    require(len(set(occupied)) == len(occupied) and value['host_pid'] not in occupied,
            'reader or custody host PID overlaps an active process')
    return value


def capture_exit(value, launch, intent, request, session_nonce, success=False):
    fields(value, ('binding', 'host_pid', 'starttime', 'terminal', 'terminal_digest', 'eof', 'pidfd_exited',
                   'wait_status', 'exit_code', 'diagnostic'), 'capture exit')
    integer(value['host_pid'], 'reader exit host PID', 1, (1 << 31) - 1)
    integer(value['starttime'], 'reader exit starttime', 1)
    require(value['binding'] == intent['binding'] == launch['binding'] and
            value['host_pid'] == launch['host_pid'] and value['starttime'] == launch['starttime'],
            'capture exit identity differs')
    capture_binding(value['binding'], request, session_nonce)
    require(all(type(value[key]) is bool for key in ('terminal', 'eof', 'pidfd_exited')) and
            value['pidfd_exited'], 'reader pidfd exit is not proven')
    if value['terminal']:
        hex_string(value['terminal_digest'], 'capture terminal digest')
    else:
        require(value['terminal_digest'] is None, 'terminal digest without terminal')
    status = integer(value['wait_status'], 'reader wait status', 0, 0xffff)
    require((os.WIFEXITED(status) and type(value['exit_code']) is int and
             value['exit_code'] == os.WEXITSTATUS(status)) or
            (status <= 0xff and os.WIFSIGNALED(status) and value['exit_code'] is None),
            'reader wait status differs')
    diagnostic = ascii_string(value['diagnostic'], 'reader diagnostic', 4 * ((16 * 1024 + 2) // 3))
    try:
        decoded = base64.b64decode(diagnostic, validate=True)
    except (ValueError, TypeError) as exc:
        raise ProtocolError('reader diagnostic encoding') from exc
    require(len(decoded) <= 16 * 1024 and base64.b64encode(decoded).decode('ascii') == diagnostic,
            'reader diagnostic size or encoding')
    if success:
        require(value['terminal'] and value['eof'] and os.WIFEXITED(status) and value['exit_code'] == 0,
                'reader exit is not successful')
    return value


def _reader_snapshot(value, launch, intent, request):
    fields(value, ('host_pid', 'host_tid', 'inner_pid', 'inner_tid', 'starttime', 'pidns', 'mntns',
                   'cgroupns', 'userns', 'pdeathsig'), 'reader snapshot')
    for key in ('host_pid', 'host_tid', 'inner_pid', 'inner_tid'):
        integer(value[key], 'reader ' + key, 1, (1 << 31) - 1)
    integer(value['starttime'], 'reader thread starttime', 1)
    identity(value['userns'], 'reader user namespace')
    require(value['host_pid'] == launch['host_pid'] and value['host_tid'] == value['host_pid'] and
            value['inner_pid'] == value['inner_tid'] and value['starttime'] == launch['starttime'] and
            type(value['pdeathsig']) is int and value['pdeathsig'] == 9 and
            value['userns'] == intent['helper']['userns'], 'reader thread identity differs')
    for kind, key in (('pid', 'pidns'), ('mnt', 'mntns'), ('cgroup', 'cgroupns')):
        _namespace_evidence(value[key], kind, value['userns'], request['helper'][key])
    return value


def _status_fields(text):
    ascii_string(text, 'reader thread status', 64 * 1024)
    require(text.endswith('\n'), 'incomplete reader thread status')
    wanted = {'Pid', 'Tgid', 'NSpid', 'NStgid', 'Uid', 'Gid', 'Groups', 'CapInh', 'CapPrm', 'CapEff',
              'CapBnd', 'CapAmb', 'NoNewPrivs'}
    result = {}
    for line in text.splitlines():
        key, sep, value = line.partition(':')
        if sep and key in wanted:
            require(key not in result, 'duplicate reader status field')
            result[key] = value.strip()
    require(set(result) == wanted, 'reader status fields missing')
    return result


def capture_barrier(value, snapshot, launch, intent, request):
    fields(value, ('reader', 'exe', 'tasks_before', 'tasks_after', 'threads'), 'reader barrier')
    require(canonical_json(value['reader']) == canonical_json(snapshot), 'root and reader barrier snapshots differ')
    _reader_snapshot(snapshot, launch, intent, request)
    _object_identity(value['exe'], 'barrier executable')
    require(value['exe'] == {key: launch['exe'][key] for key in ('dev', 'ino', 'mode')},
            'barrier executable differs')
    before = _thread_inventory(value['tasks_before'], 'barrier before')
    require(_thread_inventory(value['tasks_after'], 'barrier after') == before, 'reader task inventory changed')
    threads = value['threads']
    require(type(threads) is list and len(threads) == len(before), 'reader thread status count')
    seen_inner = set()
    for tid, thread in zip(before, threads):
        fields(thread, ('host_tid', 'inner_tid', 'starttime', 'status', 'lsm'), 'barrier thread')
        require(type(thread['host_tid']) is int and thread['host_tid'] == tid, 'barrier thread order')
        inner = integer(thread['inner_tid'], 'barrier inner TID', 1, (1 << 31) - 1)
        integer(thread['starttime'], 'barrier thread starttime', 1)
        require(inner not in seen_inner, 'duplicate inner thread')
        seen_inner.add(inner)
        status = _status_fields(thread['status'])
        require(all(part.isdecimal() and int(part) > 0 for key in ('NSpid', 'NStgid')
                    for part in status[key].split()), 'invalid namespace PID chain')
        require(status['Pid'] == str(tid) and status['Tgid'] == str(snapshot['host_pid']) and
                status['NSpid'].split()[0:1] == [str(tid)] and status['NSpid'].split()[-1:] == [str(inner)] and
                status['NStgid'].split()[0:1] == [str(snapshot['host_pid'])] and
                status['NStgid'].split()[-1:] == [str(snapshot['inner_pid'])] and
                status['Uid'].split() == ['0'] * 4 and status['Gid'].split() == ['0'] * 4 and
                status['Groups'] == '' and status['NoNewPrivs'] == '1' and
                all(status[key] == CAP_MASK for key in ('CapPrm', 'CapEff', 'CapBnd')) and
                all(status[key] == '0000000000000000' for key in ('CapInh', 'CapAmb')) and
                ascii_string(thread['lsm'], 'reader LSM', 4096).strip() == 'unconfined',
                'reader thread credentials or namespace PID differ')
        if tid == snapshot['host_tid']:
            require(inner == snapshot['inner_tid'] and thread['starttime'] == snapshot['starttime'],
                    'capturing thread differs from root inventory')
    require(snapshot['host_tid'] in before, 'capturing thread absent from inventory')
    return value


def _intent_from_evidence(evidence, request, session_nonce, phase):
    binding = {'version': 1, 'run_token': request['input']['run_token'], 'session_nonce': session_nonce,
               'ordinal': request['input']['ordinal'], 'nonce': request['input']['nonce'],
               'input_digest': request['input_digest'], 'request_digest': request['request_digest'],
               'phase': phase, 'sequence': 1 if phase == 'before' else 2}
    keys = ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns', 'userns')
    helper = {key: evidence['helper'][key] for key in keys}
    process = evidence['daemon'] if request['input']['exe'] else evidence['target']
    metadata = None if request['input']['metadata_pid'] is None else {key: process[key] for key in keys}
    return {'version': 1, 'op': 'capture_intent', 'binding': binding, 'helper': helper,
            'metadata_owner': metadata}


def _completed_capture(value, intent, request, session_nonce, preparer_exe=None, root_pid=None, preparer_pid=None):
    fields(value, ('binding', 'helper_mountinfo', 'metadata_mountinfo', 'metadata_identity',
                   'reader_before', 'reader_after', 'root'), 'completed capture')
    require(canonical_json(value['binding']) == canonical_json(intent['binding']), 'capture payload binding differs')
    root = value['root']
    fields(root, ('intent_sha256', 'launch', 'before', 'after', 'payload_digest', 'exit'), 'capture root proof')
    require(root['intent_sha256'] == hashlib.sha256(canonical_json(intent)).hexdigest(),
            'capture intent digest differs')
    launch = capture_launch(root['launch'], intent, request, session_nonce, root_pid, preparer_exe, preparer_pid)
    capture_barrier(root['before'], value['reader_before'], launch, intent, request)
    capture_barrier(root['after'], value['reader_after'], launch, intent, request)
    require(canonical_json(value['reader_before']) == canonical_json(value['reader_after']),
            'capturing thread changed between barriers')
    _complete_mountinfo(value['helper_mountinfo'], 'common helper')
    if request['input']['metadata_pid'] is None:
        require(value['metadata_mountinfo'] is None and value['metadata_identity'] is None,
                'unexpected common metadata evidence')
    else:
        _complete_mountinfo(value['metadata_mountinfo'], 'common metadata')
        metadata = value['metadata_identity']
        fields(metadata, ('source', 'target'), 'common metadata identity')
        _object_identity(metadata['source'], 'metadata source')
        _object_identity(metadata['target'], 'metadata target')
        require(metadata['source'] == metadata['target'] and stat.S_ISREG(metadata['source']['mode']),
                'metadata source and target object identities differ')
    payload = {key: value[key] for key in ('binding', 'helper_mountinfo', 'metadata_mountinfo', 'metadata_identity',
                                            'reader_before', 'reader_after')}
    digest = hashlib.sha256(canonical_json(payload)).hexdigest()
    require(root['payload_digest'] == digest, 'capture payload digest differs')
    capture_exit(root['exit'], launch, intent, request, session_nonce, success=True)
    require(root['exit']['terminal_digest'] == digest, 'reader terminal digest differs')
    return value


def validate_capture_ledger(captures, request, session_nonce, evidence=None, success=False,
                            preparer_exe=None, root_pid=None, coordinator_namespaces=None, preparer_pid=None):
    validate_request(request)
    hex_string(session_nonce, 'capture session nonce')
    if root_pid is not None:
        integer(root_pid, 'root PID', 1, (1 << 31) - 1)
    if coordinator_namespaces is not None:
        fields(coordinator_namespaces, ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'), 'coordinator namespaces')
        for name, value in coordinator_namespaces.items():
            identity(value, 'coordinator ' + name)
    require(type(captures) is list and len(captures) <= 2, 'capture ledger size')
    if success:
        require(evidence is not None and len(captures) == 2, 'successful capture evidence incomplete')
    view = None
    if evidence is not None:
        fields(evidence, ('version', 'daemon', 'helper', 'target', 'reader_context', 'observer_view'),
               'capture raw evidence')
        require(type(evidence['version']) is int and evidence['version'] == 3, 'capture raw evidence version')
        _process_evidence(evidence['daemon'], 'capture daemon')
        _process_evidence(evidence['helper'], 'capture helper')
        if evidence['target'] is not None:
            _process_evidence(evidence['target'], 'capture target')
        fields(evidence['reader_context'], ('tid', 'original', 'helper', 'source_before', 'source_after',
               'original_pidns', 'original_mntns', 'source_pid_before', 'source_pid_after',
               'source_mnt_before', 'source_mnt_after'), 'capture reader context')
        view = evidence['observer_view']
        fields(view, ('before', 'after'), 'capture observer view')
        require(view['before'] is not None or view['after'] is None, 'capture observer prefix differs')
    for index, row in enumerate(captures):
        phase = ('before', 'after')[index]
        fields(row, ('intent', 'intent_sha256', 'launch', 'exit'), 'capture ledger record')
        intent = capture_intent(row['intent'], request, session_nonce, phase)
        require(row['intent_sha256'] == hashlib.sha256(canonical_json(intent)).hexdigest(),
                'independent intent digest differs')
        require(row['launch'] is not None and row['exit'] is not None, 'capture launch or exit unresolved')
        launch = capture_launch(row['launch'], intent, request, session_nonce, root_pid, preparer_exe, preparer_pid)
        if coordinator_namespaces is not None:
            require({key: launch['root']['active_before'][key] for key in ('dev', 'ino')} ==
                    coordinator_namespaces['pid'], 'root active PID namespace differs from coordinator')
        capture_exit(row['exit'], launch, intent, request, session_nonce, success=success or index + 1 < len(captures))
        if evidence is not None:
            require(canonical_json(intent) == canonical_json(_intent_from_evidence(evidence, request, session_nonce, phase)),
                    'independent capture process identity differs from preparer evidence')
            value = view[phase]
            if value is not None:
                _completed_capture(value, intent, request, session_nonce, preparer_exe, root_pid, preparer_pid)
                require(canonical_json(value['root']['launch']) == canonical_json(row['launch']) and
                        canonical_json(value['root']['exit']) == canonical_json(row['exit']) and
                        value['root']['intent_sha256'] == row['intent_sha256'],
                        'independent capture ledger differs from preparer proof')
            else:
                require(not success, 'completed observer capture missing')
    if view is not None:
        for index, phase in enumerate(('before', 'after')):
            require(view[phase] is None or index < len(captures), 'preparer capture absent from root ledger')
    return captures


def validate_reader_context(evidence, request, preparer, coordinator_namespaces=None,
                            waiting=None, final_status=None, status='prepared'):
    """Validate original source identity and the direct child common-view captures."""
    require(status in ('prepared', 'rejected'), 'reader evidence status')
    context = evidence['reader_context']
    fields(context, ('tid', 'original', 'helper', 'source_before', 'source_after', 'original_pidns',
                     'original_mntns', 'source_pid_before', 'source_pid_after', 'source_mnt_before',
                     'source_mnt_after'), 'reader context')
    tid = integer(context['tid'], 'source reader thread ID', 1, (1 << 31) - 1)
    require(type(preparer.get('threads')) is list and
            any(type(thread) is dict and type(thread.get('tid')) is int and thread['tid'] == tid
                for thread in preparer['threads']), 'source reader thread absent from preparer')
    owner = evidence['daemon']['userns']
    _namespace_evidence(context['original'], 'cgroup', owner, request['reader_cgroupns'])
    _namespace_evidence(context['helper'], 'cgroup', owner, request['helper']['cgroupns'])
    original_pid = _namespace_evidence(context['original_pidns'], 'pid', owner)
    original_mnt = _namespace_evidence(context['original_mntns'], 'mnt', owner)
    for prefix, expected in (('source', request['reader_cgroupns']), ('source_pid', original_pid),
                             ('source_mnt', original_mnt)):
        before, after = context[prefix + '_before'], context[prefix + '_after']
        identity(before, prefix + ' before')
        require(before == expected, prefix + ' before differs')
        if after is None:
            require(status == 'rejected', prefix + ' after missing')
        else:
            identity(after, prefix + ' after')
            require(after == expected, prefix + ' restoration differs')
    require(evidence['helper']['cgroupns'] == request['helper']['cgroupns'] and
            evidence['helper']['pidns'] == request['helper']['pidns'], 'pinned helper namespace differs')
    if coordinator_namespaces is not None or waiting is not None or final_status is not None:
        require(coordinator_namespaces is not None and waiting is not None, 'reader binding evidence missing')
        validate_request_namespaces(request, coordinator_namespaces, waiting, final_status)
        require(original_pid == coordinator_namespaces['pid'] and original_mnt == coordinator_namespaces['mnt'],
                'source reader original namespaces differ from coordinator')
    view = evidence['observer_view']
    fields(view, ('before', 'after'), 'observer view')
    require(view['before'] is not None or view['after'] is None, 'observer capture prefix differs')
    for process_name in ('daemon', 'helper', 'target'):
        process = evidence[process_name]
        if status == 'prepared' and process is not None:
            for phase in ('before', 'after'):
                _complete_mountinfo(process[phase + '_mountinfo'], process_name + ' ' + phase)
    session = preparer['session_nonce']
    for phase in ('before', 'after'):
        value = view[phase]
        if value is None:
            require(status == 'rejected', 'observer ' + phase + ' capture missing')
            continue
        intent = _intent_from_evidence(evidence, request, session, phase)
        capture_intent(intent, request, session, phase)
        _completed_capture(value, intent, request, session, preparer['exe'], preparer_pid=preparer['pid'])
    if view['before'] is not None and view['after'] is not None and request['input']['metadata_pid'] is not None:
        require(view['before']['metadata_mountinfo'] == view['after']['metadata_mountinfo'],
                'common metadata graph changed')
    return view


def validate_result(result, raw, request, preparer_pid, session_nonce):
    validate_request(request)
    fields(result, ('version', 'run_token', 'ordinal', 'nonce', 'input_digest', 'request_digest',
                    'status', 'reason', 'preparer', 'sources', 'attached', 'raw_evidence_digest'), 'result')
    inp = request['input']
    require(type(result['version']) is int and result['version'] == 1 and type(result['ordinal']) is int and
            all(result[key] == inp[key] for key in ('run_token', 'ordinal', 'nonce')) and
            all(result[key] == request[key] for key in ('input_digest', 'request_digest')), 'result binding')
    require(result['status'] in ('prepared', 'rejected') and
            ((result['status'] == 'prepared' and result['reason'] == '') or
             (result['status'] == 'rejected' and result['reason'] in REASONS)), 'result status/reason')
    hex_string(result['raw_evidence_digest'], 'raw evidence digest')
    require(hashlib.sha256(raw).hexdigest() == result['raw_evidence_digest'], 'raw evidence digest mismatch')
    evidence = strict_json(raw)
    require(canonical_json(evidence) == raw, 'raw evidence is not canonical')
    fields(evidence, ('version', 'daemon', 'helper', 'target', 'reader_context', 'observer_view'), 'raw evidence')
    require(type(evidence['version']) is int and evidence['version'] == 3, 'raw evidence version')
    _process_evidence(evidence['daemon'], 'daemon evidence')
    _process_evidence(evidence['helper'], 'helper evidence')
    if evidence['target'] is not None:
        _process_evidence(evidence['target'], 'target evidence')
    require(evidence['daemon']['pid'] == request['daemon']['pid'] and
            evidence['helper']['pid'] == request['helper']['pid'] and
            evidence['helper']['starttime'] == request['helper']['starttime'] and
            evidence['helper']['mntns'] == request['helper']['mntns'] and
            evidence['helper']['pidns'] == request['helper']['pidns'] and
            evidence['helper']['cgroupns'] == request['helper']['cgroupns'] and
            ((request['target'] is None and evidence['target'] is None) or
             (request['target'] is not None and evidence['target'] is not None and
              evidence['target']['pid'] == request['target']['pid'] and
              evidence['target']['starttime'] == request['target']['starttime'])), 'process evidence binding')
    prep = result['preparer']
    fields(prep, ('pid', 'exe', 'uid', 'gid', 'threads', 'lsm', 'session_nonce'), 'preparer')
    require(type(prep['pid']) is int and prep['pid'] == preparer_pid and
            type(prep['uid']) is int and prep['uid'] == 0 and type(prep['gid']) is int and prep['gid'] == 0 and
            prep['lsm'] == 'unconfined' and prep['session_nonce'] == session_nonce, 'preparer identity')
    fields(prep['exe'], ('dev', 'ino', 'mode', 'sha256'), 'preparer executable')
    _object_identity({key: prep['exe'][key] for key in ('dev', 'ino', 'mode')}, 'preparer executable')
    require(prep['exe']['sha256'] == inp['preparer_sha256'], 'preparer executable digest')
    require(type(prep['threads']) is list and prep['threads'], 'preparer threads missing')
    seen = set()
    for thread in prep['threads']:
        fields(thread, ('tid', 'cap_inh', 'cap_prm', 'cap_eff', 'cap_bnd', 'cap_amb', 'nnp'), 'thread')
        tid = integer(thread['tid'], 'thread ID', 1, (1 << 31) - 1)
        require(tid not in seen, 'duplicate thread')
        seen.add(tid)
        require(all(thread[name] == CAP_MASK for name in ('cap_prm', 'cap_eff', 'cap_bnd')) and
                all(thread[name] == '0000000000000000' for name in ('cap_inh', 'cap_amb')) and
                type(thread['nnp']) is int and thread['nnp'] == 1, 'thread capability set')
    validate_reader_context(evidence, request, prep, status=result['status'])
    for name in ('sources', 'attached'):
        require(type(result[name]) is list, name + ' is not a list')
        for row in result[name]:
            _source_or_attachment(row, name == 'attached')
    expected_roles = [(root['kind'], root['ordinal']) for root in inp['roots']]
    if inp['exe']:
        expected_roles.append(('daemon_exe', None))
    for name in ('sources', 'attached'):
        require([(row['role'], row['ordinal']) for row in result[name]] == expected_roles[:len(result[name])],
                name + ' role/order mismatch')
    for source in result['sources']:
        if source['role'] == 'daemon_exe':
            continue
        root = inp['roots'][source['ordinal']]
        expected_path = (root['source'] if source['role'] == 'declared_bind' else
                         root['mountpoint'] if source['role'] == 'native_volume' else '/data')
        require(source['source_path'] == expected_path, 'source path/input mismatch')
    if result['status'] == 'prepared':
        expected = [(root['kind'], root['ordinal']) for root in inp['roots']]
        if inp['exe']:
            expected.append(('daemon_exe', None))
        require([(row['role'], row['ordinal']) for row in result['sources']] == expected and
                [(row['role'], row['ordinal']) for row in result['attached']] == expected,
                'prepared source or attachment set')
        for process in (evidence['daemon'], evidence['helper'], evidence['target']):
            if process is not None:
                require(process['before_mountinfo'] and process['after_mountinfo'], 'process mount graphs missing')
                require(process['userns'] == evidence['daemon']['userns'], 'process user namespace changed')
        for process in (evidence['daemon'], evidence['target']):
            if process is not None:
                require(process['before_mountinfo'] == process['after_mountinfo'], 'source graph changed')
        for source, mounted in zip(result['sources'], result['attached']):
            owner = evidence['target'] if source['role'] == 'running_root' else evidence['daemon']
            require(owner is not None and source['namespace'] == owner['mntns'] and
                    mounted['namespace'] == evidence['helper']['mntns'], 'mount namespace custody mismatch')
            identity_keys = ('dev', 'ino', 'mode')
            require(all(source[key] == mounted[key] for key in identity_keys) and
                    source['native_root'] == mounted['native_root'] and
                    _same_subtree(source['subtree'], mounted['subtree']) and
                    _private_readonly(mounted['subtree']), 'installed object or subtree differs')
            if source['role'] in ('running_root', 'daemon_exe'):
                require(source['clone'] is not None and
                        all(source[key] == source['clone'][key] for key in identity_keys),
                        'detached clone custody missing')
            else:
                before = mounted['before']
                require(source['clone'] is None and all(source[key] == before[key] for key in identity_keys) and
                        _same_subtree(source['subtree'], before['subtree']) and
                        [row['mount_id'] for row in before['subtree']] ==
                        [row['mount_id'] for row in mounted['subtree']], 'configured root changed during preparation')
    return evidence


class EvidenceAccumulator:
    def __init__(self):
        self.parts = []
        self.size = 0
        self.final = None

    def add(self, frame):
        require(self.final is None and type(frame) is dict, 'extra or invalid result frame')
        if frame.get('op') != 'evidence_chunk':
            require(self.parts, 'missing raw evidence')
            hex_string(frame.get('raw_evidence_digest'), 'raw evidence digest')
            self.final = frame
            return True
        fields(frame, ('version', 'op', 'seq', 'data'), 'evidence chunk')
        require(type(frame['version']) is int and frame['version'] == 1 and
                type(frame['seq']) is int and frame['seq'] == len(self.parts) and type(frame['data']) is str,
                'evidence chunk order')
        try:
            chunk = base64.b64decode(frame['data'], validate=True)
        except (ValueError, TypeError) as exc:
            raise ProtocolError('invalid evidence chunk') from exc
        require(0 < len(chunk) <= MAX_CHUNK and base64.b64encode(chunk).decode('ascii') == frame['data'],
                'invalid evidence chunk size/encoding')
        self.size += len(chunk)
        require(self.size <= MAX_EVIDENCE, 'evidence size exceeded')
        self.parts.append(chunk)
        return False

    def finish(self):
        require(self.final is not None, 'missing final result')
        raw = b''.join(self.parts)
        require(hashlib.sha256(raw).hexdigest() == self.final['raw_evidence_digest'],
                'raw evidence digest mismatch')
        return self.final, raw




def require_result_eof(sock, deadline):
    """Require clean EOF after an authenticated final result or retirement; reject extra frames/FDs."""
    _wait_read(sock, deadline)
    data, ancillary, flags, _ = sock.recvmsg(MAX_FRAME + 1, socket.CMSG_SPACE(256 * array.array('i').itemsize),
                                             getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
    delivered = []
    for level, kind, payload in ancillary:
        if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
            values = array.array('i')
            values.frombytes(payload[:len(payload) - len(payload) % values.itemsize])
            delivered.extend(values)
    for fd in delivered:
        os.close(fd)
    require(not data and not ancillary and not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC),
            'extra frame or descriptors after final result or retirement')


class CoordinatorLaunch:
    def __init__(self, sock, run_token, session_nonce, parent_pid):
        self.sock = sock
        self.run_token = hex_string(run_token, 'run token', HEX32)
        self.session_nonce = hex_string(session_nonce, 'session nonce')
        self.parent_pid = integer(parent_pid, 'parent PID', 1, (1 << 31) - 1)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.registered = False
        self.records = []
        self.last_ordinal = 0
        self.active = None
        self.active_deadline = None
        self.retired = False

    def _identity(self, op):
        return {'version': 1, 'op': op, 'run_token': self.run_token, 'session_nonce': self.session_nonce}

    def register_guard(self, lock_fd, guard, timeout=5):
        require(not self.registered and not self.retired, 'guard registration already completed')
        fields(guard, ('dev', 'ino', 'uid', 'mode'), 'guard')
        value = self._identity('guard_register')
        value['guard'] = guard
        deadline = time.monotonic() + timeout
        _send_bounded(self.sock, value, (lock_fd,), deadline)
        reply, _ = _receive(self.sock, (self.parent_pid, 0, 0), 0, deadline)
        expected = self._identity('guard_accepted')
        expected['guard'] = guard
        require(canonical_json(reply) == canonical_json(expected), 'guard acceptance mismatch')
        self.registered = True
        return reply

    def launch(self, ordinal, nonce, digest, worker_fd, prepare_request, timeout=5, deadline=None):
        require(self.registered and not self.retired and self.active is None and
                ordinal == self.last_ordinal + 1, 'launch ordering')
        integer(ordinal, 'launch ordinal', 1, (1 << 63) - 1)
        hex_string(nonce, 'launch nonce')
        hex_string(digest, 'launch request digest')
        validate_request(prepare_request)
        inp = prepare_request['input']
        require((inp['run_token'], inp['ordinal'], inp['nonce'], prepare_request['request_digest']) ==
                (self.run_token, ordinal, nonce, digest), 'launch full request differs')
        request = self._identity('launch_prepare')
        request.update(ordinal=ordinal, nonce=nonce, request_digest=digest, prepare_request=prepare_request)
        self.last_ordinal = ordinal
        self.active = (ordinal, nonce, None)
        self.active_deadline = time.monotonic() + 30
        self.records.append({'request': request, 'launch': None, 'exit': None, 'captures': []})
        launch_deadline = min(self.active_deadline, time.monotonic() + timeout)
        if deadline is not None:
            launch_deadline = min(launch_deadline, deadline)
        _send_bounded(self.sock, request, (worker_fd,), launch_deadline)
        reply, fds = _receive(self.sock, (self.parent_pid, 0, 0), 1, launch_deadline)
        try:
            fields(reply, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                           'request_digest', 'pid', 'exe'), 'launch receipt')
            require(type(reply['version']) is int and reply['version'] == 1 and reply['op'] == 'launch_accepted' and
                    reply['run_token'] == self.run_token and reply['session_nonce'] == self.session_nonce and
                    type(reply['ordinal']) is int and reply['ordinal'] == ordinal and reply['nonce'] == nonce and
                    reply['request_digest'] == digest, 'launch receipt binding')
            integer(reply['pid'], 'preparer PID', 1, (1 << 31) - 1)
            _object_identity(reply['exe'], 'launch executable')
            validate_pidfd(fds[0], reply['pid'])
            self.active = (ordinal, nonce, reply['pid'])
            self.records[-1]['launch'] = reply
            return reply, fds[0]
        except BaseException:
            for fd in fds:
                os.close(fd)
            raise

    def exchange(self, sock, request, pidfds, pidfd, deadline, save_new, send_request=None):
        """Drive the whole prepare transaction, including root capture intents, without serial waits."""
        validate_request(request)
        require(self.registered and self.active is not None and self.records, 'no active prepare')
        record = self.records[-1]
        launch = record['launch']
        require(launch is not None and self.active == (request['input']['ordinal'], request['input']['nonce'],
                                                       launch['pid']) and
                record['request']['prepare_request'] == request and callable(save_new), 'active prepare mismatch')
        deadline = min(deadline, self.active_deadline)
        require(time.monotonic() < deadline, 'prepare deadline expired')
        require(len(pidfds) == (3 if request['target'] is not None else 2), 'pidfd count')
        pids = [request['daemon']['pid'], request['helper']['pid']]
        if request['target'] is not None:
            pids.append(request['target']['pid'])
        for fd, pid in zip(pidfds, pids):
            validate_pidfd(fd, pid)
        validate_pidfd(pidfd, launch['pid'])
        sender = send_prepare if send_request is None else send_request
        require(callable(sender), 'invalid prepare sender')
        accumulator = EvidenceAccumulator()
        captures = record['captures']
        sent = eof = pid_exited = False
        acknowledgement = None
        exited = result = raw = problem = None
        total = 0
        settle_deadline = deadline
        previous_sock_timeout, previous_control_timeout = sock.gettimeout(), self.sock.gettimeout()
        sock.setblocking(False)
        self.sock.setblocking(False)
        try:
            while True:
                if sent and eof and pid_exited and exited is not None and acknowledgement is None:
                    if not select.select([self.sock], [], [], 0)[0]:
                        break
                remaining = settle_deadline - time.monotonic()
                require(remaining > 0, 'prepare exchange deadline')
                readers = [self.sock]
                if not eof:
                    readers.append(sock)
                if not pid_exited:
                    readers.append(pidfd)
                writers = ([sock] if not sent else []) + ([self.sock] if acknowledgement is not None else [])
                readable, writable, _ = select.select(readers, writers, [], remaining)
                require(readable or writable, 'prepare exchange deadline')
                if sock in writable and not sent:
                    try:
                        sender(sock, request, pidfds)
                    except BlockingIOError:
                        pass
                    else:
                        sent = True
                if self.sock in writable and acknowledgement is not None and self.sock not in readable:
                    try:
                        send_frame(self.sock, acknowledgement)
                    except BlockingIOError:
                        pass
                    else:
                        acknowledgement = None
                if pidfd in readable:
                    pid_exited = True
                    settle_deadline = min(settle_deadline, time.monotonic() + 5)
                if self.sock in readable:
                    message, _ = recv_frame(self.sock, (self.parent_pid, 0, 0), 0, allow_eof=True)
                    require(message is not None, 'root control channel closed before retirement')
                    total += len(canonical_json(message))
                    require(total + accumulator.size <= MAX_EVIDENCE, 'aggregate evidence size exceeded')
                    op = message.get('op')
                    require(exited is None, 'control message after preparer exit')
                    if op == 'capture_intent':
                        require(sent and acknowledgement is None and problem is None and accumulator.final is None and
                                not pid_exited and not select.select([pidfd], [], [], 0)[0] and len(captures) < 2,
                                'capture intent outside active prepare')
                        phase = ('before', 'after')[len(captures)]
                        capture_intent(message, request, self.session_nonce, phase)
                        if captures:
                            prior = captures[-1]
                            require(prior['launch'] is not None and prior['exit'] is not None,
                                    'overlapping capture intent')
                            capture_exit(prior['exit'], prior['launch'], prior['intent'], request,
                                         self.session_nonce, success=True)
                        digest = hashlib.sha256(canonical_json(message)).hexdigest()
                        sequence = message['binding']['sequence']
                        save_new('capture-intent-%06d-%d.json' % (request['input']['ordinal'], sequence), message)
                        captures.append({'intent': message, 'intent_sha256': digest, 'launch': None, 'exit': None})
                        acknowledgement = {'version': 1, 'op': 'capture_intent_ack',
                                           'binding': message['binding'], 'intent_sha256': digest}
                        total += len(canonical_json(acknowledgement))
                        require(total + accumulator.size <= MAX_EVIDENCE, 'aggregate evidence size exceeded')
                    elif op in ('capture_launch', 'capture_exit'):
                        fields(message, ('version', 'op', 'binding', 'launch' if op == 'capture_launch' else 'exit'),
                               'capture control message')
                        require(type(message['version']) is int and message['version'] == 1 and captures and
                                acknowledgement is None and message['binding'] == captures[-1]['intent']['binding'],
                                'capture control binding or ordering')
                        capture_binding(message['binding'], request, self.session_nonce)
                        row = captures[-1]
                        sequence = message['binding']['sequence']
                        if op == 'capture_launch':
                            require(row['launch'] is None and row['exit'] is None, 'duplicate capture launch')
                            capture_launch(message['launch'], row['intent'], request, self.session_nonce,
                                           self.parent_pid, launch['exe'], launch['pid'])
                            save_new('capture-launch-%06d-%d.json' % (request['input']['ordinal'], sequence), message)
                            row['launch'] = message['launch']
                        else:
                            require(row['launch'] is not None and row['exit'] is None, 'capture exit out of order')
                            capture_exit(message['exit'], row['launch'], row['intent'], request, self.session_nonce)
                            save_new('capture-exit-%06d-%d.json' % (request['input']['ordinal'], sequence), message)
                            row['exit'] = message['exit']
                    elif op == 'preparer_exit':
                        _launch_exit(launch, message, request, success=False)
                        require(acknowledgement is None and canonical_json(message['captures']) == canonical_json(captures),
                                'root capture ledger differs or ACK is pending')
                        validate_capture_ledger(captures, request, self.session_nonce, preparer_exe=launch['exe'],
                                                root_pid=self.parent_pid, preparer_pid=launch['pid'])
                        exited = message
                        record['exit'] = message
                        settle_deadline = min(settle_deadline, time.monotonic() + 5)
                    else:
                        raise ProtocolError('unknown root control message')
                if sock in readable and not eof:
                    frame, _ = recv_frame(sock, (launch['pid'], 0, 0), 0, allow_eof=True)
                    try:
                        if frame is None:
                            eof = True
                            if accumulator.final is None and problem is None:
                                problem = {'type': 'ProtocolError', 'message': 'result channel closed'}
                        elif problem is None:
                            before_size = accumulator.size
                            finished = accumulator.add(frame)
                            if accumulator.size == before_size:
                                total += len(canonical_json(frame))
                            require(total + accumulator.size <= MAX_EVIDENCE, 'aggregate evidence size exceeded')
                            if finished:
                                result, raw = accumulator.finish()
                                validate_result(result, raw, request, launch['pid'], self.session_nonce)
                        else:
                            total += len(canonical_json(frame))
                            require(total + accumulator.size <= MAX_EVIDENCE, 'aggregate evidence size exceeded')
                    except (ProtocolError, ValueError, TypeError) as exc:
                        if str(exc) in ('aggregate evidence size exceeded', 'evidence size exceeded'):
                            raise
                        if problem is None:
                            problem = {'type': type(exc).__name__, 'message': str(exc)[:2048]}
                        result = raw = None
            evidence = None if result is None else validate_result(result, raw, request, launch['pid'],
                                                                   self.session_nonce)
            validate_capture_ledger(captures, request, self.session_nonce, evidence,
                                    success=result is not None and result['status'] == 'prepared',
                                    preparer_exe=launch['exe'], root_pid=self.parent_pid, preparer_pid=launch['pid'])
            self.active = None
            self.active_deadline = None
            return result, raw, exited, problem
        except BaseException as exc:
            for index, row in enumerate(captures):
                if row['exit'] is None:
                    uncertainty = {'version': 1, 'capture': row,
                                   'launch_outcome': 'unresolved' if row['launch'] is None else 'exit_unresolved',
                                   'error': {'type': type(exc).__name__, 'message': str(exc)[:2048]}}
                    save_new('capture-uncertain-%06d-%d.json' % (request['input']['ordinal'], index + 1), uncertainty)
            raise
        finally:
            sock.settimeout(previous_sock_timeout)
            self.sock.settimeout(previous_control_timeout)

    def require_no_pending(self):
        require(self.registered and self.active is None and
                all(record['exit'] is not None and
                    all(row['launch'] is not None and row['exit'] is not None for row in record['captures'])
                    for record in self.records), 'preparer or capture ownership is unresolved')

    def retire(self, ledger_sha256, timeout=5):
        self.require_no_pending()
        require(not self.retired, 'duplicate coordinator retirement')
        hex_string(ledger_sha256, 'retired ledger digest')
        message = self._identity('coordinator_retired')
        message['ledger_sha256'] = ledger_sha256
        deadline = time.monotonic() + timeout
        _send_bounded(self.sock, message, (), deadline)
        reply, _ = _receive(self.sock, (self.parent_pid, 0, 0), 0, deadline)
        expected = self._identity('retired_accepted')
        expected['ledger_sha256'] = ledger_sha256
        require(canonical_json(reply) == canonical_json(expected), 'retirement acknowledgement mismatch')
        self.retired = True
        return reply


class _PortableChecks:
    @staticmethod
    def check(condition, message):
        require(condition, message)


def _launch_exit(launch, exited, request=None, success=True):
    fields(launch, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                    'request_digest', 'pid', 'exe'), 'launch receipt')
    fields(exited, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                    'pid', 'wait_status', 'exit_code', 'capture', 'captures'), 'exit receipt')
    require(type(launch['version']) is int and launch['version'] == 1 and launch['op'] == 'launch_accepted' and
            type(exited['version']) is int and exited['version'] == 1 and exited['op'] == 'preparer_exit',
            'launcher receipt operations')
    hex_string(launch['run_token'], 'launch run token', HEX32)
    hex_string(launch['session_nonce'], 'launch session nonce')
    hex_string(launch['nonce'], 'launch nonce')
    hex_string(launch['request_digest'], 'launch request digest')
    integer(launch['ordinal'], 'launch ordinal', 1, (1 << 63) - 1)
    integer(launch['pid'], 'launch PID', 1, (1 << 31) - 1)
    integer(exited['ordinal'], 'exit ordinal', 1, (1 << 63) - 1)
    integer(exited['pid'], 'exit PID', 1, (1 << 31) - 1)
    _object_identity(launch['exe'], 'launch executable')
    require(stat.S_ISREG(launch['exe']['mode']) and not launch['exe']['mode'] & (stat.S_ISUID | stat.S_ISGID),
            'launch executable is not a safe regular file')
    require(all(launch[key] == exited[key] for key in ('run_token', 'session_nonce', 'ordinal', 'nonce', 'pid')),
            'launch/exit identity mismatch')
    if request is not None:
        inp = request['input']
        require(all(launch[key] == inp[key] for key in ('run_token', 'ordinal', 'nonce')) and
                launch['request_digest'] == request['request_digest'], 'launch/request mismatch')
    status = integer(exited['wait_status'], 'wait status', 0, 0xffff)
    require((os.WIFEXITED(status) and type(exited['exit_code']) is int and
             exited['exit_code'] == os.WEXITSTATUS(status)) or
            (status <= 0xff and os.WIFSIGNALED(status) and exited['exit_code'] is None),
            'launcher wait status mismatch')
    ascii_string(exited['capture'], 'preparer capture', 64 * 1024)
    require(type(exited['captures']) is list and len(exited['captures']) <= 2, 'exit capture ledger')
    if request is not None:
        validate_capture_ledger(exited['captures'], request, launch['session_nonce'],
                                preparer_exe=launch['exe'], preparer_pid=launch['pid'])
    if success:
        require(os.WIFEXITED(status) and exited['exit_code'] == 0, 'preparer did not exit successfully')


def _observer_status(value, apparmor, guest_namespaces):
    fields(value, ('cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb', 'no_new_privs',
                   'seccomp', 'namespaces', 'apparmor', 'fd_targets'), 'observer status')
    for name in ('cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb'):
        require(value[name] == ('0000000000000004' if name in ('cap_eff', 'cap_prm', 'cap_bnd')
                                else '0000000000000000'), 'observer capability mismatch')
    require(type(value['no_new_privs']) is int and value['no_new_privs'] == 1 and
            type(value['seccomp']) is int and value['seccomp'] == 2, 'observer privilege mismatch')
    label = value['apparmor']
    fields(label, ('label', 'errno'), 'observer AppArmor')
    require(type(label['errno']) is int and label['errno'] == 0 and type(apparmor) is str and
            apparmor not in ('', 'unconfined') and label['label'] in (apparmor, apparmor + ' (enforce)'),
            'observer AppArmor mismatch')
    names = ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup')
    fields(value['namespaces'], names, 'observer namespaces')
    fields(guest_namespaces, names, 'guest namespaces')
    for name in names:
        identity(value['namespaces'][name], 'observer ' + name)
        identity(guest_namespaces[name], 'guest ' + name)
        require(value['namespaces'][name] != guest_namespaces[name], 'observer shares guest ' + name)
    targets = value['fd_targets']
    require(type(targets) is dict and {'0', '1', '2'} <= set(targets) and
            all(type(key) is str and key.isdecimal() and type(target) is str and
                (target in ('/dev/null', 'anon_inode:[eventpoll]', 'anon_inode:[eventfd]') or
                 re.fullmatch(r'pipe:\[[0-9]+\]', target) is not None)
                for key, target in targets.items()), 'observer inherited unexpected descriptors')


def _expected_observer_options(inp):
    options = ['--user', '0:0', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--read-only', '--pids-limit', '64', '--memory', '256m',
               '--memory-swap', '256m', '--cpus', '1', '--log-driver', 'none',
               '--ipc', 'private', '--cgroupns', 'private', '--cap-add', 'DAC_READ_SEARCH', '--interactive']
    if inp['profile'] == 'B':
        options += ['--security-opt', 'seccomp=/mnt/g2-seed/probe-seccomp.json']
    for root in inp['roots']:
        if root['kind'] == 'declared_bind':
            options += ['--mount', 'type=bind,source=%s,target=%s,readonly,bind-recursive=readonly,'
                        'bind-propagation=rprivate' % (root['source'], root['target'])]
        elif root['kind'] == 'native_volume':
            options += ['--mount', 'type=volume,source=%s,target=%s,readonly,volume-nocopy' %
                        (root['name'], root['target'])]
    if inp['metadata_pid'] is not None:
        for source, target in (('stat', 'target-stat'), ('mountinfo', 'target-mountinfo'),
                               ('ns/pid', 'target-pidns')):
            options += ['--mount', 'type=bind,source=/proc/%d/%s,target=/meta/%s,readonly,'
                        'bind-recursive=readonly,bind-propagation=rprivate' %
                        (inp['metadata_pid'], source, target)]
    return options


def _validate_observer_create_spec(spec, request):
    inp = request['input']
    fields(spec, ('name', 'image', 'options', 'args', 'token', 'labels', 'environment'), 'create spec')
    require(spec['image'] == inp['imageID'] and type(spec['name']) is str and
            type(spec['token']) is str and HEX64.fullmatch(spec['token']) and
            spec['name'].endswith('-' + spec['token']), 'create identity mismatch')
    for name in ('options', 'args', 'labels', 'environment'):
        require(type(spec[name]) is list and all(type(item) is str for item in spec[name]), 'create ' + name)
    require(spec['options'] == _expected_observer_options(inp), 'create options differ from fixed contract')
    mode = 'proc-v1' if inp['metadata_pid'] is not None else 'none'
    args = (['prepared-denycheck'] if inp['deny'] else
            ['prepared-observe'] + [root['target'] for root in inp['roots']])
    env = ['--env', 'G2_CASE=' + inp['case'], '--env', 'G2_PHASE=' + inp['phase'],
           '--env', 'G2_METADATA=' + mode, '--env', 'G2_METADATA_EXE=' + ('1' if inp['exe'] else '0'),
           '--env', 'G2_NESTED=' + ('1' if inp['nested'] else '0'),
           '--env', 'G2_PREPARE_NONCE=' + inp['nonce'],
           '--env', 'G2_PREPARE_INPUT_DIGEST=' + request['input_digest']]
    labels = ['--label', 'owner=g2-isolated-capability', '--label', 'run=' + inp['run_token'],
              '--label', 'runner=guest.py', '--label', 'create-token=' + spec['token']]
    require(spec['args'] == args and spec['environment'] == env and spec['labels'] == labels,
            'create command or identity environment mismatch')
    return mode


def _inspect_observer(helper, request, spec):
    mode = _validate_observer_create_spec(spec, request)
    inp = request['input']
    args, env, labels = spec['args'], spec['environment'], spec['labels']
    inspected = helper['inspect']
    fields(inspected, ('mounts', 'host_config', 'apparmor', 'config', 'image', 'id'), 'observer inspect')
    require(inspected['id'] == helper['id'] == request['helper']['id'] and inspected['image'] == inp['imageID'],
            'inspected observer identity mismatch')
    config, hc = inspected['config'], inspected['host_config']
    require(type(config) is dict and type(hc) is dict and config.get('Entrypoint') == ['/probe'] and
            config.get('Cmd') == args and config.get('User') == '0:0' and
            config.get('Labels') == dict(item.split('=', 1) for item in labels[1::2]) and
            type(config.get('Env')) is list and set(env[1::2]) <= set(config['Env']) and
            all(config.get(key) is True for key in ('OpenStdin', 'AttachStdin', 'AttachStdout',
                                                    'AttachStderr', 'StdinOnce')) and config.get('Tty') is False,
            'observer image configuration mismatch')
    require(all(type(item) is str for item in config['Env']) and
            sorted(item for item in config['Env'] if item.startswith('G2_')) == sorted(env[1::2]),
            'observer environment is ambiguous')
    require(hc.get('NetworkMode') == 'none' and hc.get('ReadonlyRootfs') is True and
            hc.get('CapDrop') == ['ALL'] and hc.get('CapAdd') == ['CAP_DAC_READ_SEARCH'] and
            hc.get('PidsLimit') == 64 and hc.get('Memory') == hc.get('MemorySwap') == 268435456 and
            hc.get('NanoCpus') == 1000000000 and type(hc.get('LogConfig')) is dict and
            hc['LogConfig'].get('Type') == 'none' and hc.get('PidMode') == '' and
            hc.get('IpcMode') == 'private' and hc.get('UTSMode') == '' and hc.get('CgroupnsMode') == 'private' and
            hc.get('AutoRemove') is False and
            not any(hc.get(name) for name in ('Privileged', 'Devices', 'DeviceRequests', 'Binds',
                                              'VolumesFrom', 'Links', 'PortBindings', 'PublishAllPorts')),
            'observer isolation configuration mismatch')
    security = hc.get('SecurityOpt') or []
    require(type(security) is list and all(type(x) is str for x in security) and
            any(x in ('no-new-privileges', 'no-new-privileges=true') for x in security) and
            not any('unconfined' in x for x in security), 'observer security options mismatch')
    profiles = [x[len('seccomp='):] for x in security if x.startswith('seccomp=')]
    allowed = ('read write close openat newfstatat fstat statx fstatfs readlink readlinkat '
               'getdents64 lseek fcntl mmap mprotect munmap madvise brk rt_sigaction '
               'rt_sigprocmask rt_sigreturn sigaltstack futex sched_yield sched_getaffinity '
               'nanosleep clock_gettime getpid getppid gettid getuid geteuid getgid getegid '
               'getrandom tgkill arch_prctl set_tid_address set_robust_list rseq prlimit64 '
               'uname exit exit_group execve epoll_create1 epoll_ctl epoll_pwait eventfd2 pipe2 dup dup3').split()
    fixed_profile = {'defaultAction': 'SCMP_ACT_ERRNO', 'defaultErrnoRet': 1,
                     'architectures': ['SCMP_ARCH_X86_64'], 'syscalls': [
                         {'names': allowed, 'action': 'SCMP_ACT_ALLOW'},
                         {'names': ['clone'], 'action': 'SCMP_ACT_ALLOW',
                          'args': [{'index': 0, 'value': 0x7e020000, 'valueTwo': 0, 'op': 'SCMP_CMP_MASKED_EQ'}]},
                         {'names': ['clone3'], 'action': 'SCMP_ACT_ERRNO', 'errnoRet': 38},
                         {'names': ['ioctl'], 'action': 'SCMP_ACT_ALLOW',
                          'args': [{'index': 1, 'value': 0x8400941f, 'op': 'SCMP_CMP_EQ'}]},
                         {'names': ['ioctl'], 'action': 'SCMP_ACT_ALLOW',
                          'args': [{'index': 1, 'value': 0xd0009412, 'op': 'SCMP_CMP_EQ'}]}]}
    require((inp['profile'] == 'A' and not profiles) or
            (inp['profile'] == 'B' and len(profiles) == 1 and
             canonical_json(strict_json(profiles[0].encode())) == canonical_json(fixed_profile)),
            'observer seccomp selection mismatch')
    expected = []
    for root in inp['roots']:
        if root['kind'] == 'declared_bind':
            expected.append(('bind', root['source'], root['target'], None))
        elif root['kind'] == 'native_volume':
            expected.append(('volume', root['mountpoint'], root['target'], root['name']))
    if inp['metadata_pid'] is not None:
        for source, target in (('stat', 'target-stat'), ('mountinfo', 'target-mountinfo'),
                               ('ns/pid', 'target-pidns')):
            expected.append(('bind', '/proc/%d/%s' % (inp['metadata_pid'], source), '/meta/' + target, None))
    actual, configured = inspected['mounts'], hc.get('Mounts') or []
    require(type(actual) is list and type(configured) is list and len(actual) == len(configured) == len(expected),
            'observer mount set mismatch')
    for kind, source, target, name in expected:
        a = [row for row in actual if type(row) is dict and row.get('Destination') == target]
        c = [row for row in configured if type(row) is dict and row.get('Target') == target]
        require(len(a) == len(c) == 1 and a[0].get('Type') == c[0].get('Type') == kind and
                a[0].get('Source') == source and a[0].get('RW') is False and c[0].get('ReadOnly') is True,
                'observer mount identity mismatch')
        if kind == 'bind':
            require(c[0].get('Source') == source and a[0].get('Propagation') == 'rprivate',
                    'observer bind configuration mismatch')
        else:
            require(a[0].get('Name') == c[0].get('Source') == name and
                    type(c[0].get('VolumeOptions')) is dict and c[0]['VolumeOptions'].get('NoCopy') is True,
                    'observer volume configuration mismatch')
    return mode


def _observer_records(helper, request, waiting, ready, mode, guest_namespaces):
    inp = request['input']
    records = helper['records']
    require(type(records) is list and len(records) >= 4, 'observer records missing')
    indexed = {}
    for row in records:
        fields(row, ('experiment_kind', 'preparation_version', 'metadata_mode', 'case', 'phase',
                     'root_ordinal', 'operation', 'errno', 'value'), 'observer record')
        require(row['experiment_kind'] == 'input-handoff-v1' and type(row['preparation_version']) is int and
                row['preparation_version'] == 1 and row['metadata_mode'] == mode and
                row['case'] == inp['case'] and row['phase'] == inp['phase'] and
                type(row['errno']) is int and row['errno'] >= 0 and type(row['value']) is dict,
                'observer record identity mismatch')
        key = (row['root_ordinal'], row['operation'])
        require(key not in indexed, 'duplicate observer record')
        indexed[key] = row
    expected = {(None, name) for name in ('waiting', 'ready', 'self_status', 'complete')}
    if inp['deny']:
        expected |= {(None, x) for x in ('inet_socket', 'unshare', 'mount')}
        expected |= {('%03d' % i, 'readonly_open') for i in range(2 if inp['nested'] else 1)}
    else:
        expected.add((None, 'mountinfo'))
        ops = ('fstat', 'statx', 'statfs', 'btrfs_fs_info', 'btrfs_ino_lookup')
        ordinals = ['%03d' % i for i in range(len(inp['roots']))] + (['nested:000'] if inp['nested'] else [])
        expected |= {(ordinal, op) for ordinal in ordinals for op in ops}
        if mode == 'proc-v1':
            expected |= {(None, x) for x in ('target_stat', 'target_mountinfo', 'target_pidns')}
            if inp['exe']:
                expected.add((None, 'target_exe'))
    require(set(indexed) == expected and records[0]['operation'] == 'waiting' and
            records[1]['operation'] == 'ready' and records[-1]['operation'] == 'complete',
            'observer operation set or sequence mismatch')
    flat = {(op if ordinal is None else ordinal + ':' + op): row for (ordinal, op), row in indexed.items()}
    require(canonical_json(flat) == canonical_json(helper['indexed']), 'observer indexed records mismatch')
    for op, value in (('waiting', waiting), ('ready', ready)):
        require(indexed[(None, op)]['errno'] == 0 and indexed[(None, op)]['value'] == value,
                'observer ' + op + ' record mismatch')
    done = indexed[(None, 'complete')]
    require(done['errno'] == 0 and done['value'] == {'complete': True, 'expected_roots': len(inp['roots'])},
            'observer completion missing')
    final = indexed[(None, 'self_status')]
    require(final['errno'] == 0, 'observer final status failed')
    _observer_status(waiting['self_status'], helper['inspect']['apparmor'], guest_namespaces)
    _observer_status(final['value'], helper['inspect']['apparmor'], guest_namespaces)
    for name in ('cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb', 'no_new_privs',
                 'seccomp', 'namespaces', 'apparmor'):
        require(waiting['self_status'][name] == final['value'][name], 'observer security identity changed')
    validate_request_namespaces(request, guest_namespaces, waiting, final['value'])
    schemas = {'fstat': {'dev', 'ino', 'mode', 'size'},
               'statx': {'mask', 'dev_major', 'dev_minor', 'ino', 'mode', 'mount_id'},
               'statfs': {'type', 'fsid'}, 'btrfs_fs_info': {'fsid'}, 'btrfs_ino_lookup': {'subvol'},
               'mountinfo': {'text'}, 'target_stat': {'text'}, 'target_mountinfo': {'text'},
               'target_pidns': {'dev', 'ino'}, 'target_exe': {'dev', 'ino'},
               'readonly_open': set(), 'inet_socket': set(), 'unshare': set(), 'mount': set()}
    for (_, op), entry in indexed.items():
        if op in ('waiting', 'ready', 'self_status', 'complete'):
            continue
        require(op in schemas, 'unknown observer operation')
        if entry['errno']:
            require(not entry['value'] and op in ('btrfs_fs_info', 'btrfs_ino_lookup', 'readonly_open',
                                                  'inet_socket', 'unshare', 'mount'),
                    'failed observer operation')
            continue
        value = entry['value']
        require(set(value) == schemas[op], 'observer operation value fields')
        if op in ('fstat', 'statx', 'target_pidns', 'target_exe', 'btrfs_ino_lookup'):
            require(all(type(part) is int and part >= 0 for part in value.values()), 'observer numeric metadata')
        elif op in ('mountinfo', 'target_stat', 'target_mountinfo'):
            require(type(value['text']) is str and len(value['text'].encode()) <= 1024 * 1024,
                    'observer metadata text')
        elif op == 'btrfs_fs_info':
            hex_string(value['fsid'], 'observer btrfs FSID', HEX32)
        elif op == 'statfs':
            require(type(value['type']) is int and type(value['fsid']) is list and len(value['fsid']) == 2 and
                    all(type(part) is int for part in value['fsid']), 'observer filesystem identity')
    if inp['deny']:
        require(all(indexed[('%03d' % i, 'readonly_open')]['errno'] == errno.EROFS
                    for i in range(2 if inp['nested'] else 1)), 'observer readonly denial missing')
        if inp['profile'] == 'B':
            require(all(indexed[(None, op)]['errno'] == errno.EPERM for op in ('inet_socket', 'unshare', 'mount')),
                    'observer seccomp denials missing')
    else:
        for ordinal in ordinals:
            fst, sx, fs = (indexed[(ordinal, op)] for op in ('fstat', 'statx', 'statfs'))
            require(fst['errno'] == sx['errno'] == fs['errno'] == 0 and
                    fst['value'].get('ino') == sx['value'].get('ino') and
                    fst['value'].get('mode') == sx['value'].get('mode') and
                    type(fst['value'].get('mode')) is int and
                    stat.S_IFMT(fst['value']['mode']) in (stat.S_IFDIR, stat.S_IFREG),
                    'observer object identity mismatch')
            if fs['value'].get('type') == 0x9123683e:
                require(all(indexed[(ordinal, op)]['errno'] == 0 for op in ('btrfs_fs_info', 'btrfs_ino_lookup')),
                        'observer btrfs identity missing')
    return indexed


def validate_daemon_lifecycle(lifecycle, helper_id, process=None, retirement=None):
    """Validate the complete graph across the exact helper create/remove lifecycle."""
    import handoff
    fields(lifecycle, ('value', 'receipt', 'sha256'), 'daemon lifecycle')
    value = lifecycle['value']
    fields(value, ('pid', 'starttime', 'helper_id', 'before_mountinfo', 'after_mountinfo', 'retirement'),
           'daemon lifecycle value')
    integer(value['pid'], 'daemon lifecycle PID', 1)
    integer(value['starttime'], 'daemon lifecycle starttime', 1)
    hex_string(helper_id, 'lifecycle helper ID')
    require(value['helper_id'] == helper_id and
            lifecycle['receipt'] == '/lab/control/helper-lifecycle-' + helper_id + '.json',
            'daemon lifecycle helper or receipt mismatch')
    hex_string(lifecycle['sha256'], 'daemon lifecycle receipt digest')
    require(hashlib.sha256(canonical_json(value) + b'\n').hexdigest() == lifecycle['sha256'],
            'daemon lifecycle receipt digest mismatch')
    before, after = value['before_mountinfo'], value['after_mountinfo']
    require(type(before) is str and before.endswith('\n') and len(before.encode('utf-8')) <= 1024 * 1024 and
            before == after, 'daemon lifecycle mount graph changed or missing')
    handoff.custody_graph(before)
    retired = value['retirement']
    fields(retired, ('id', 'remove', 'absent'), 'daemon lifecycle retirement')
    remove = retired['remove']
    require(retired['id'] == helper_id and retired['absent'] is True and type(remove) is dict and
            remove.get('argv') == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'rm', helper_id] and
            type(remove.get('returncode')) is int and remove['returncode'] == 0 and
            type(remove.get('stdout')) is str and remove['stdout'].strip() == helper_id,
            'daemon lifecycle exact helper removal missing')
    if retirement is not None:
        require(retired == retirement, 'daemon lifecycle retirement differs from owned removal')
    if process is not None:
        require(type(process) is dict and value['pid'] == process['pid'] and
                value['starttime'] == process['starttime'], 'daemon lifecycle process identity changed')
    return value


def validate_observation(helper):
    """Validate portable guest evidence; never compare guest namespaces to host /proc."""
    try:
        require(type(helper) is dict and type(helper.get('preparation_version')) is int and
                helper['preparation_version'] == 1, 'helper preparation version missing')
        prep = helper['preparation']
        fields(prep, ('version', 'receipt', 'receipt_sha256', 'request', 'launch', 'exit', 'captures', 'pidfd_exited',
                      'result', 'raw_evidence', 'waiting', 'ready', 'ready_intent', 'ready_intent_value',
                      'ready_intent_sha256', 'create_spec', 'helper_state', 'coordinator_namespaces'),
               'helper preparation')
        require(type(prep['version']) is int and prep['version'] == 1 and prep['pidfd_exited'] is True,
                'preparer exit evidence missing')
        req = validate_request(prep['request'])
        _launch_exit(prep['launch'], prep['exit'], req)
        result = prep['result']
        raw = canonical_json(prep['raw_evidence'])
        evidence = validate_result(result, raw, req, prep['launch']['pid'], prep['launch']['session_nonce'])
        require(prep['captures'] == prep['exit']['captures'], 'portable root capture ledger differs')
        validate_capture_ledger(prep['captures'], req, prep['launch']['session_nonce'], evidence, success=True,
                                preparer_exe=prep['launch']['exe'], preparer_pid=prep['launch']['pid'],
                                coordinator_namespaces=prep['coordinator_namespaces'])
        validate_daemon_lifecycle(helper['daemon_lifecycle'], helper['id'], evidence['daemon'])
        volumes = helper['volume_set_before']
        require(type(volumes) is list and all(type(name) is str and
                re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,255}', name) for name in volumes) and
                volumes == sorted(set(volumes)) == helper['volume_set_after'] and
                all(root['name'] in volumes for root in req['input']['roots'] if root['kind'] == 'native_volume'),
                'helper volume set changed or omitted an admitted volume')
        require(result['status'] == 'prepared' and all(result['preparer']['exe'][key] == prep['launch']['exe'][key]
                                                       for key in ('dev', 'ino', 'mode')),
                'preparer result or executed identity mismatch')
        ordinal = req['input']['ordinal']
        require(prep['receipt'] == '/lab/control/prepare-result-%06d.json' % ordinal and
                prep['ready_intent'] == '/lab/control/ready-intent-%06d.json' % ordinal, 'receipt path mismatch')
        receipt = {'version': 1, 'request': req, 'create_spec': prep['create_spec'],
                   'observer_waiting': prep['waiting'], 'helper_state': prep['helper_state'],
                   'coordinator_namespaces': prep['coordinator_namespaces'],
                   'launch': prep['launch'], 'exit': prep['exit'], 'captures': prep['captures'],
                   'pidfd_exited': True, 'result': result, 'raw_evidence': evidence,
                   'channel_error': None, 'test_fault': None}
        hex_string(prep['receipt_sha256'], 'preparation receipt digest')
        require(len(canonical_json(receipt)) + 1 <= MAX_EVIDENCE, 'portable preparation receipt exceeds bound')
        require(hashlib.sha256(canonical_json(receipt) + b'\n').hexdigest() == prep['receipt_sha256'],
                'preparation receipt digest mismatch')
        state = prep['helper_state']
        require(type(state) is dict and state.get('Running') is True and state.get('Pid') == req['helper']['pid'] and
                type(state.get('StartedAt')) is str and bool(state['StartedAt']), 'waiting process state mismatch')
        waiting, ready = prep['waiting'], prep['ready']
        fields(waiting, ('nonce', 'input_digest', 'self_status'), 'observer waiting')
        fields(ready, ('version', 'nonce', 'input_digest', 'request_digest', 'prepare_receipt_sha256', 'action'),
               'observer ready')
        require(type(ready['version']) is int and ready['version'] == 1 and
                waiting['nonce'] == ready['nonce'] == req['input']['nonce'] and
                waiting['input_digest'] == ready['input_digest'] == req['input_digest'] and
                ready['request_digest'] == req['request_digest'] and
                ready['prepare_receipt_sha256'] == prep['receipt_sha256'] and
                ready['action'] == ('denycheck' if req['input']['deny'] else 'observe'), 'observer ready binding')
        intent = {'helper_id': helper['id'], 'ready': ready, 'preparation_receipt': prep['receipt']}
        hex_string(prep['ready_intent_sha256'], 'ready intent digest')
        require(prep['ready_intent_value'] == intent and
                hashlib.sha256(canonical_json(intent) + b'\n').hexdigest() == prep['ready_intent_sha256'],
                'durable ready intent binding mismatch')
        mode = _inspect_observer(helper, req, prep['create_spec'])
        indexed = _observer_records(helper, req, waiting, ready, mode, prep['coordinator_namespaces'])
        validate_reader_context(evidence, req, result['preparer'], prep['coordinator_namespaces'],
                                waiting, indexed[(None, 'self_status')]['value'])
        final = helper['final_state']
        require(type(final) is dict and final.get('Status') == 'exited' and final.get('Running') is False and
                type(final.get('ExitCode')) is int and final['ExitCode'] == 0 and
                final.get('StartedAt') == state['StartedAt'] and
                type(helper.get('attach_exit_code')) is int and helper['attach_exit_code'] == 0 and
                helper.get('helper_pidfd_exited') is True, 'observer or attached CLI exit missing')
        import handoff
        handoff.verify_custody(_PortableChecks, result, evidence, None if req['input']['deny'] else indexed,
                               mode, req['input']['exe'])
        if mode == 'proc-v1' and not req['input']['deny']:
            process = evidence['daemon'] if req['input']['exe'] else evidence['target']
            require(process is not None and
                    handoff.proc_stat(indexed[(None, 'target_stat')]['value']['text']) ==
                    {'pid': process['pid'], 'starttime': process['starttime']} and
                    process['before_mountinfo'] == process['after_mountinfo'] and
                    indexed[(None, 'target_mountinfo')]['value']['text'] ==
                    evidence['observer_view']['before']['metadata_mountinfo'] ==
                    evidence['observer_view']['after']['metadata_mountinfo'],
                    'observer metadata differs from pinned process')
        if req['input']['nested'] and not req['input']['deny']:
            nested = [row for row in result['attached'][0]['subtree'] if row['relative'] == 'nested']
            require(len(nested) == 1 and indexed[('nested:000', 'statx')]['value']['mount_id'] ==
                    nested[0]['mount_id'], 'nested observer mount custody mismatch')
        if not req['input']['deny']:
            paths = [mount['Destination'] for mount in helper['inspect']['mounts']
                     if mount['Destination'].startswith('/meta/')]
            for attached in result['attached']:
                paths.extend(attached['target'] if row['relative'] == '.' else
                             attached['target'] + '/' + row['relative'] for row in attached['subtree'])
            graph = handoff.mount_graph(indexed[(None, 'mountinfo')]['value']['text'], paths)
            require(graph == helper['mount_graph'], 'observer mount graph mismatch')
        else:
            require(helper['mount_graph'] == {}, 'deny observer has fabricated mount graph')
        require(all(helper.get(key) == value for key, value in
                    {'mount_namespace_identity': 'UNPROVEN', 'all_overlay_layers': 'UNPROVEN',
                     'product_gate': 'NOT_READY'}.items()), 'helper capability boundary mismatch')
        return {'id': helper['id'], 'ordinal': ordinal, 'nonce': req['input']['nonce'],
                'request_digest': req['request_digest'], 'receipt_sha256': prep['receipt_sha256']}
    except (KeyError, TypeError, IndexError, AttributeError) as exc:
        raise ProtocolError('incomplete helper evidence: ' + str(exc)) from exc


def validate_negative_create_binding(value, owned):
    """Bind both negative inspections to the original, full create receipt."""
    try:
        import handoff
        fields(owned, ('id', 'spec', 'inspect'), 'negative owned receipt')
        original, created, running = owned['inspect'], value['inspect'], value['running_inspect']
        require(type(original) is dict and type(running) is dict and set(original) == set(running) and
                owned['id'] == value['helper_id'] and handoff.same_json(owned['spec'], value['create_spec']) and
                original['State']['Status'] == 'created' and
                original['Id'] == created['id'] == value['helper_id'] and
                original['Name'] == '/' + owned['spec']['name'] and
                original['Image'] == created['image'] == value['image'] and
                handoff.same_json(original['Config'], created['config']) and
                handoff.same_json(original['HostConfig'], created['host_config']) and
                handoff.same_json(original['AppArmorProfile'], created['apparmor']) and
                handoff.same_json(handoff.mount_map(original['Mounts']), handoff.mount_map(created['mounts'])) and
                all(handoff.same_json(original[key], running[key]) for key in
                    ('Id', 'Name', 'Image', 'Config', 'Created', 'Path', 'Args')),
                'negative owned creation and running inspections differ')
    except (KeyError, TypeError, IndexError, AttributeError, ValueError) as exc:
        if isinstance(exc, ProtocolError):
            raise
        raise ProtocolError('incomplete negative create binding: ' + str(exc)) from exc


def validate_negative_observation(value, variant):
    """Check the actual failed attach and exact owned-process retirement."""
    try:
        observer = ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce')
        malformed = ('wrong_input_digest', 'extra_fd')
        require(variant in observer + malformed and type(value) is dict and
                value.get('status') == 'REJECTED' and value.get('variant') == variant and
                value.get('reason') == ('observer_not_ready' if variant in observer else 'prepare_protocol'),
                'negative outcome mismatch')
        cid = hex_string(value['helper_id'], 'negative helper ID')
        attach = value['start_attach']
        import handoff
        require(type(attach) is dict and handoff.raw_capture_problem(attach) is None and
                type(attach.get('returncode')) is int and attach['returncode'] != 0 and
                attach.get('argv') == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock',
                                       'start', '--attach', '--interactive', cid] and
                attach.get('stdout', '').endswith('\n'), 'negative attached output incomplete')
        lines = attach['stdout'].splitlines()
        require(len(lines) == 1, 'negative observer emitted extra output')
        row = strict_json(lines[0].encode('utf-8'))
        fields(row, ('experiment_kind', 'preparation_version', 'metadata_mode', 'case', 'phase',
                     'root_ordinal', 'operation', 'errno', 'value'), 'negative waiting record')
        require(row == value['waiting'] and row['experiment_kind'] == 'input-handoff-v1' and
                type(row['preparation_version']) is int and row['preparation_version'] == 1 and
                row['metadata_mode'] == 'none' and row['case'] == 'C0' and row['phase'] == 'before' and
                row['root_ordinal'] is None and row['operation'] == 'waiting' and
                type(row['errno']) is int and row['errno'] == 0, 'negative waiting record mismatch')
        waiting = row['value']
        fields(waiting, ('nonce', 'input_digest', 'self_status'), 'negative waiting')
        hex_string(waiting['nonce'], 'negative nonce')
        _absolute_lab_path(value['source'], 'negative source')
        require(value['profile'] in ('A', 'B') and type(value['image']) is str and
                re.fullmatch(r'sha256:[0-9a-f]{64}', value['image']), 'negative fixture identity')
        running = value['running_inspect']
        required_inspect = {'AppArmorProfile', 'Args', 'Config', 'Created', 'Driver', 'ExecIDs', 'GraphDriver',
                            'HostConfig', 'HostnamePath', 'HostsPath', 'Id', 'Image', 'LogPath', 'MountLabel',
                            'Mounts', 'Name', 'NetworkSettings', 'Path', 'Platform', 'ProcessLabel',
                            'ResolvConfPath', 'RestartCount', 'State'}
        require(type(running) is dict and required_inspect <= set(running) and
                running['Id'] == cid and running['Image'] == value['image'] and
                running['Name'] == '/' + value['create_spec']['name'] and
                type(running['Created']) is str and bool(running['Created']) and
                running['Path'] == '/probe' and handoff.same_json(running['Args'], value['create_spec']['args']) and
                handoff.same_json(running['Config'], value['inspect']['config']) and
                handoff.same_json(handoff.mount_map(running['Mounts']),
                                  handoff.mount_map(value['inspect']['mounts'])),
                'negative running inspection identity differs from creation')
        created_hc, running_hc = value['inspect']['host_config'], running['HostConfig']
        require(type(created_hc) is dict and type(running_hc) is dict and created_hc.keys() == running_hc.keys() and
                created_hc.get('OomKillDisable') is False and
                (running_hc['OomKillDisable'] is False or running_hc['OomKillDisable'] is None) and
                all(key == 'OomKillDisable' or handoff.same_json(created_hc[key], running_hc[key])
                    for key in created_hc), 'negative running HostConfig differs from creation')
        state = running['State']
        require(type(state) is dict and state.get('Running') is True and state.get('Status') == 'running' and
                type(state.get('Pid')) is int and state['Pid'] > 0 and
                type(state.get('StartedAt')) is str and bool(state['StartedAt']) and
                state['StartedAt'] != '0001-01-01T00:00:00Z', 'negative running process state missing')
        apparmor = running['AppArmorProfile']
        _observer_status(waiting['self_status'], apparmor, value['coordinator_namespaces'])
        require(waiting['self_status']['apparmor']['label'] == apparmor + ' (enforce)' and
                not any(option.lower().startswith('apparmor') for option in running_hc.get('SecurityOpt') or []),
                'negative observer default AppArmor is not enforced')
        inp = {'imageID': value['image'], 'run_token': value['inspect']['config']['Labels']['run'],
               'case': 'C0', 'phase': 'before', 'profile': value['profile'], 'metadata_pid': None,
               'exe': False, 'nested': False, 'deny': False, 'nonce': waiting['nonce'],
               'roots': [{'kind': 'declared_bind', 'source': value['source'], 'target': '/roots/000'}]}
        req = {'input': inp, 'input_digest': waiting['input_digest'], 'helper': {'id': cid}}
        _inspect_observer({'id': cid, 'inspect': value['inspect']}, req, value['create_spec'])
        runtime_view = {'id': running['Id'], 'image': running['Image'], 'apparmor': apparmor,
                        'config': running['Config'], 'host_config': running_hc, 'mounts': running['Mounts']}
        _inspect_observer({'id': cid, 'inspect': runtime_view}, req, value['create_spec'])
        hex_string(waiting['input_digest'], 'negative input digest')
        final = value['final_state']
        require(type(final) is dict and final.get('Running') is False and final.get('Status') == 'exited' and
                type(final.get('ExitCode')) is int and final['ExitCode'] != 0 and
                final.get('StartedAt') == state['StartedAt'] and value.get('helper_pidfd_exited') is True,
                'negative observer exit missing or process identity changed')
        retirement = value['retirement']
        fields(retirement, ('id', 'remove', 'absent'), 'negative retirement')
        removed = retirement['remove']
        require(retirement['id'] == cid and retirement['absent'] is True and type(removed) is dict and
                removed.get('argv') == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'rm', cid] and
                type(removed.get('returncode')) is int and removed['returncode'] == 0 and
                removed.get('stdout', '').strip() == cid, 'negative observer retirement missing')
        validate_daemon_lifecycle(value['daemon_lifecycle'], cid, retirement=retirement)
        require(type(value['rejection']) is dict and value['rejection'].get('code') == value['reason'] and
                value['rejection'].get('type') == 'PreparationFailure',
                'negative lifecycle lacks the actual rejection summary')
        require(value.get('no_ready_or_root_output') is True, 'negative output classification missing')
        stop = value['stop']
        if variant == 'no_ready':
            require(type(stop) is dict and stop.get('argv') ==
                    ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'stop', '--timeout', '10', cid] and
                    type(stop.get('returncode')) is int and stop['returncode'] == 0 and
                    stop.get('stdout', '').strip() == cid, 'negative observer stop evidence missing')
        else:
            require(stop is None, 'unexpected negative stop command')
        if variant in observer:
            require(value.get('preparation') is None, 'unexpected negative preparer')
            attempt = value['negative_intent']
            fields(attempt, ('variant', 'helper_id', 'stdin_base64', 'waiting'), 'negative input')
            require(attempt['variant'] == variant and attempt['helper_id'] == cid and attempt['waiting'] == waiting,
                    'negative input identity mismatch')
            if variant == 'no_ready':
                require(attempt['stdin_base64'] is None, 'no-ready variant sent input')
            else:
                encoded = attempt['stdin_base64']
                require(type(encoded) is str, 'negative input missing')
                payload = base64.b64decode(encoded, validate=True)
                require(base64.b64encode(payload).decode('ascii') == encoded, 'negative input encoding')
                candidate = {'version': 1, 'nonce': waiting['nonce'], 'input_digest': waiting['input_digest'],
                             'request_digest': '0' * 64, 'prepare_receipt_sha256': '0' * 64, 'action': 'observe'}
                if variant == 'wrong_nonce':
                    candidate['nonce'] = ('0' if waiting['nonce'][0] != '0' else '1') + waiting['nonce'][1:]
                complete = canonical_json(candidate) + b'\n'
                expected = b'' if variant == 'empty_eof' else (
                    complete[:len(complete) // 2] if variant == 'truncated_eof' else complete)
                require(payload == expected, 'negative ready payload mismatch')
        else:
            require(value.get('negative_intent') is None, 'unexpected observer input')
            prep = value['preparation']
            require(type(prep) is dict and prep.get('pidfd_exited') is True, 'negative preparer exit missing')
            receipt = prep['receipt_value']
            fields(receipt, ('version', 'request', 'create_spec', 'observer_waiting', 'helper_state',
                             'coordinator_namespaces', 'launch', 'exit', 'captures', 'pidfd_exited', 'result',
                             'raw_evidence', 'channel_error', 'test_fault'), 'negative preparation receipt')
            req = validate_request(receipt['request'])
            validate_request_namespaces(req, receipt['coordinator_namespaces'], waiting)
            require(value['daemon_lifecycle']['value']['pid'] == req['daemon']['pid'],
                    'negative request daemon differs from lifecycle')
            require(type(receipt['version']) is int and receipt['version'] == 1 and
                    req['helper']['id'] == cid and receipt['observer_waiting'] == waiting and
                    receipt['pidfd_exited'] is True and receipt['result'] is None and
                    receipt['raw_evidence'] is None and type(receipt['channel_error']) is dict and
                    receipt['launch'] == prep['launch'] and receipt['exit'] == prep['exit'] and
                    receipt['captures'] == prep['exit']['captures'] == [] and
                    receipt['test_fault'] == prep['test_fault'], 'negative preparation receipt mismatch')
            fields(receipt['channel_error'], ('type', 'message'), 'negative channel error')
            require(type(receipt['channel_error']['type']) is str and bool(receipt['channel_error']['type']) and
                    type(receipt['channel_error']['message']) is str and
                    0 < len(receipt['channel_error']['message']) <= 2048 and
                    prep['channel_error'] == receipt['channel_error'] and
                    waiting['nonce'] == req['input']['nonce'] and waiting['input_digest'] == req['input_digest'] and
                    waiting['self_status']['namespaces']['mnt'] == req['helper']['mntns'] and
                    waiting['self_status']['namespaces']['pid'] == req['helper']['pidns'] and
                    req['helper']['pid'] == state['Pid'] and
                    handoff.same_json(receipt['helper_state'], state), 'negative request or channel binding')
            hex_string(prep['receipt_sha256'], 'negative receipt digest')
            require(hashlib.sha256(canonical_json(receipt) + b'\n').hexdigest() == prep['receipt_sha256'],
                    'negative preparation receipt digest mismatch')
            require(req['input']['profile'] == value['profile'] and req['input']['imageID'] == value['image'] and
                    req['input']['roots'][0]['source'] == value['source'] and
                    receipt['create_spec'] == value['create_spec'] and
                    receipt['coordinator_namespaces'] == value['coordinator_namespaces'],
                    'negative fixture/request binding mismatch')
            _inspect_observer({'id': cid, 'inspect': runtime_view}, req, receipt['create_spec'])
            _launch_exit(prep['launch'], prep['exit'], req, success=False)
            require(os.WIFEXITED(prep['exit']['wait_status']) and prep['exit']['exit_code'] != 0,
                    'malformed preparer did not reject')
            expected_error = ('prepare_protocol: input digest mismatch' if variant == 'wrong_input_digest'
                              else 'prepare_protocol: wrong pidfd count')
            fault = prep['test_fault']
            fields(fault, ('variant', 'sent', 'sender_extra_fd_closed'), 'negative fault')
            require(fault['variant'] == variant and fault['sent'] is True and
                    (variant != 'extra_fd' or fault['sender_extra_fd_closed'] is True) and
                    expected_error in prep['exit']['capture'], 'malformed request rejection missing')
        return {'id': cid, 'variant': variant, 'nonce': waiting['nonce'], 'input_digest': waiting['input_digest']}
    except (KeyError, TypeError, IndexError, AttributeError, UnicodeError, ValueError) as exc:
        if isinstance(exc, ProtocolError):
            raise
        raise ProtocolError('incomplete negative evidence: ' + str(exc)) from exc
