"""Credentialled client for the per-boot G2 BPF authority.

The broker is the policy authority.  This module only sends the fixed RPCs;
callers must supply the pinned executable hashes and one absolute monotonic
invocation deadline obtained from the verified boot inputs.
"""
import array
import contextlib
import hashlib
import json
import os
import re
import select
import socket
import stat
import struct
import threading
import time

from journal import JournalError, boot_id, canonical, process_start, strict_json

_MAX = 16 * 1024
_U64 = (1 << 64) - 1
_ZERO = '0' * 64
_HEX32 = re.compile(r'^[0-9a-f]{32}$')
_HEX64 = re.compile(r'^[0-9a-f]{64}$')
_UUID = re.compile(r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
_ROLES = {'BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_PENDING', 'RECOVERY_OBSERVER'}
_PURPOSES = {
    'BOOT_OWNER': {'postgres-uid', 'postgres-gid', 'dns-preflight'},
    'REALM_ROOT': {'postgres-init-' + name for name in ('app', 'w1', 'w2', 'control')} |
                  {'postgres-live-' + name for name in ('app', 'w1', 'w2', 'control')} | {'browser-negative', 'container-bpf-selftest'},
    'RECOVERY_OBSERVER': {'recovery-' + name for name in ('app', 'w1', 'w2', 'control')},
}
_EXEC = {'REALM_ROOT': {'app_rollback_dump', 'app_rollback_restore', 'negative_pg_dump'},
         'RECOVERY_OBSERVER': {'pg_controldata'}}
_ERRORS = {'BPF_AUTH_DENIED', 'BPF_AUTH_UNAVAILABLE', 'BPF_AUTH_PENDING', 'BPF_AUTH_INCOMPATIBLE'}
_COMMAND_IDS = {'MAP_CREATE': 0, 'MAP_LOOKUP_ELEM': 1, 'MAP_UPDATE_ELEM': 2, 'MAP_DELETE_ELEM': 3,
                'PROG_LOAD': 5, 'PROG_ATTACH': 8, 'PROG_DETACH': 9, 'PROG_GET_FD_BY_ID': 13,
                'OBJ_GET_INFO_BY_FD': 15, 'PROG_QUERY': 16, 'BTF_LOAD': 18, 'MAP_FREEZE': 22, 'LINK_CREATE': 28, 'LINK_DETACH': 34}
_ACK_KEYS = {'version', 'boot_id', 'epoch', 'run_token', 'principal_id', 'request_id',
             'request_sha256', 'status', 'body'}
_RECORD_KEYS = {
    'principal': 'id role task_cookie tgid tid start_ns parent_cookie exe_sha256 script_sha256 argv_sha256 uid gid cgroup_sha256 registration terminal',
    'runtime_root': 'broker_cookie pid1_cookie pid1_start_ns pid1_exe_sha256 root_mount_id root_dev root_ino handoff_count thread_ids',
    'containerd_launcher': 'path exe_sha256 exe_dev exe_ino target_path target_sha256 target_dev target_ino',
    'grant': 'id owner purpose container_id config config_sha256 invocation phase retirement',
    'invocation': 'id principal_id task_cookie daemon_cookie grant_id container_id generation command config_sha256 bundle_sha256 process_sha256 exec_id listener_id ancestry state',
    'daemon': 'cookie owner mode task_cookie start_ns helper_sha256 dockerd_sha256 argv_sha256 watch_sequence state',
    'transaction': 'id grant_id invocation_id requester_cookie notification_id command attributes program_bytes license input_objects executor_cookie state result returned_fd output cleanup',
    'object': 'cookie class kernel_id owner_transaction type attributes attribute_source bytes publication_seq removal_seq state',
    'attachment': 'id grant_id enrollment root_cookie cgroup_id program_cookie attach_type flags replaces transaction_id offline_seq release_seq state',
    'audit': 'stamp serial record_type context object_cookie transaction_id task_cookie command result operation raw',
    'audit_wire': 'stamp serial record_type payload_sha256 occurrence',
    'selftest_audit_reorder': 'name nonce received delivery_order',
    'audit_cut': 'principal_id readback_sequence boundary_sequence boundary_sha256 wire_count',
    'audit_control': 'stamp serial actor phase change old_value new_value rule_list result',
    'audit_readback': 'reader_cookie request_id nonce check queries',
    'checkpoint': 'nonce begin_watermark end_watermark tail_before principal_id state',
    'guard': 'record',
    'registry': 'map action key value grant_id transaction_id parent_transaction_id',
    'selftest_fd_stage': 'nonce grant_id class transaction_id phase saved_soft hard_limit test_soft original_fd_count identity_fd_count owned_fd_count errno',
    'notification_denial': 'invocation_id requester_cookie notification_id command result guard_before guard_after',
    'notification_cancel': 'invocation_id requester_cookie notification_id command validation_errno guard_before guard_after',
    'notification_response': 'transaction_id invocation_id requester_cookie notification_id operation response ioctl_errno returned_fd guard_before guard_after',
    'cgroup_enrollment': 'grant_id enrollment root_cookie cgroup_id invocation_id requester_cookie notification_id',
    'held_file': 'cookie kind source device inode rdev mode uid gid invocation_id requester_cookie notification_id requester_fd baseline_cookie',
    'grant_object_cleanup': 'grant_id creator_transaction_id object_cookie step action result guard_sequence',
    'runtime_exec': 'invocation_id task_cookie exec_generation guard_sequence exe_sha256',
    'exec_admission': 'exec_id grant_id observation endpoint_generation docker_exec_id task_cookie process_sha256 state endpoint',
    'endpoint_transition': 'transition_id grant_id recovery_id coordinator_cookie scope_sha256 endpoint_generation state endpoint',
    'module_instance': 'instance loader_cookie module_sha256 load_file observer_bindings_sha256',
    'probe_actor': 'name probe_cookie task_cookie tgid tid start_ns parent_cookie exe_sha256 listener_id',
    'probe_import': 'probe_cookie notifying_cookie notification_id slot imported_device imported_inode fixture guard_begin guard_end result',
    'selftest_runtime_stage': 'nonce grant_id invocation_id transaction_id mode phase notifying_cookie notification_id mutation signal',
    'selftest_case': 'name nonce phase actor_cookie invocation_id transaction_ids guard_sequences reference_cookies request_sha256 result error_code',
    'selftest_guard_status': 'name nonce phase status',
    'control_rejection': 'principal_id request_id operation nonce body request_sha256 status code guard_before guard_after',
}
_RECORD_KEYS = {kind: set(fields.split()) for kind, fields in _RECORD_KEYS.items()}


def container_selftest_config():
    return {
        'Image': 'sha256:23d19d971a56051f2509bb6e20efb9a96e225a336ce8174ad78c3e5d050a3610',
        'Entrypoint': ['/opt/g2/g2-container-bpf-probe'],
        'Cmd': ['selftest', 'container-raw-denial'], 'User': '10002:10002',
        'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                       'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 32,
                       'RestartPolicy': {'Name': 'no'}, 'LogConfig': {'Type': 'none', 'Config': {}},
                       'Mounts': [
                           {'Type': 'bind', 'Source': '/opt/g2/g2-container-bpf-probe',
                            'Target': '/opt/g2/g2-container-bpf-probe', 'ReadOnly': True},
                           {'Type': 'bind', 'Source': '/run/g2/container-bpf.fifo',
                            'Target': '/run/g2/container-bpf.fifo', 'ReadOnly': True}]}}


def _fail(message):
    raise JournalError('BPF authority: ' + message)


def _integer(value, *, minimum=0, maximum=_U64):
    return type(value) is int and minimum <= value <= maximum


def _hex(value, pattern=_HEX64, *, nonzero=True):
    return type(value) is str and pattern.fullmatch(value) is not None and (not nonzero or int(value, 16) != 0)


def _object(value, keys):
    return type(value) is dict and set(value) == set(keys)


def _json_value(value, depth=0):
    if depth > 64:
        _fail('JSON nesting exceeds limit')
    if value is None or type(value) in (bool, str):
        return
    if type(value) is int:
        if not -(1 << 63) <= value <= _U64:
            _fail('JSON integer outside wire range')
        return
    if type(value) is list:
        for child in value:
            _json_value(child, depth + 1)
        return
    if type(value) is dict:
        for key, child in value.items():
            if type(key) is not str:
                _fail('JSON object key invalid')
            _json_value(child, depth + 1)
        return
    _fail('unsupported JSON value')


def _encode(value):
    _json_value(value)
    try:
        raw = canonical(value)
    except (ValueError, TypeError, UnicodeError, RecursionError) as exc:
        raise JournalError('BPF authority: JSON encoding failed') from exc
    if not 0 < len(raw) <= _MAX:
        _fail('frame exceeds limit')
    return raw


def _decode(raw):
    if not 0 < len(raw) <= _MAX:
        _fail('frame exceeds limit')
    value = strict_json(raw)
    _json_value(value)
    if _encode(value) != raw:
        _fail('frame is not canonical')
    return value


def _deadline(deadline):
    if type(deadline) not in (int, float) or isinstance(deadline, bool):
        _fail('deadline invalid')
    remaining = deadline - time.monotonic()
    if not 0 < remaining < float('inf'):
        _fail('deadline expired')
    return remaining


def _proc_executable(pid, expected):
    if not _hex(expected):
        _fail('executable pin invalid')
    fd = os.open('/proc/%d/exe' % pid, os.O_RDONLY | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            _fail('executable is not regular')
        digest = hashlib.sha256()
        while True:
            block = os.read(fd, 131072)
            if not block:
                break
            digest.update(block)
        after = os.fstat(fd)
        identity = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        if identity(before) != identity(after) or digest.hexdigest() != expected:
            _fail('executable identity changed')
        return (before.st_dev, before.st_ino)
    finally:
        os.close(fd)


class _Process:
    def __init__(self, pid, expected_hash, start=None):
        if not _integer(pid, minimum=1, maximum=(1 << 31) - 1):
            _fail('process PID invalid')
        self.pid = pid
        self.expected_hash = expected_hash
        self.fd = os.pidfd_open(pid, 0)
        try:
            self.start = process_start(pid)
            if start is not None and self.start != start:
                _fail('process starttime differs')
            self.exe = _proc_executable(pid, expected_hash)
            self.check()
        except BaseException:
            os.close(self.fd)
            raise

    def check(self):
        if select.select([self.fd], [], [], 0)[0]:
            _fail('pinned process is no longer live')
        if process_start(self.pid) != self.start:
            _fail('pinned process is no longer live')
        if _proc_executable(self.pid, self.expected_hash) != self.exe:
            _fail('pinned process is no longer live')
        if select.select([self.fd], [], [], 0)[0]:
            _fail('pinned process is no longer live')

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None


def _read_exact(fd, size, deadline):
    result = bytearray()
    while len(result) < size:
        if not select.select([fd], [], [], _deadline(deadline))[0]:
            _fail('handoff deadline exceeded')
        try:
            part = os.read(fd, size - len(result))
        except BlockingIOError:
            continue
        if not part:
            _fail('handoff truncated')
        result.extend(part)
    return bytes(result)


def _handoff_read(fd, deadline):
    if not stat.S_ISFIFO(os.fstat(fd).st_mode):
        _fail('handoff is not a pipe')
    os.set_blocking(fd, False)
    size = struct.unpack('!I', _read_exact(fd, 4, deadline))[0]
    if not 0 < size <= _MAX:
        _fail('handoff length invalid')
    value = _decode(_read_exact(fd, size, deadline))
    if not select.select([fd], [], [], _deadline(deadline))[0] or os.read(fd, 1) != b'':
        _fail('handoff has trailing data or no EOF')
    return value


def _handoff_write(fd, ack, deadline):
    try:
        if not stat.S_ISFIFO(os.fstat(fd).st_mode):
            _fail('handoff is not a pipe')
        os.set_blocking(fd, False)
        raw = _encode(ack)
        data = struct.pack('!I', len(raw)) + raw
        pos = 0
        while pos < len(data):
            if not select.select([], [fd], [], _deadline(deadline))[1]:
                _fail('handoff deadline exceeded')
            try:
                written = os.write(fd, data[pos:])
            except BlockingIOError:
                continue
            if written <= 0:
                _fail('handoff write failed')
            pos += written
    finally:
        os.close(fd)


def _ack_common(ack, expected_boot, expected_run):
    if (not _object(ack, _ACK_KEYS) or type(ack['version']) is not int or ack['version'] != 1 or
            ack['boot_id'] != expected_boot or not _hex(ack['epoch'], _HEX32) or
            ack['run_token'] != expected_run or not _integer(ack['principal_id'], minimum=1) or
            not _integer(ack['request_id'], minimum=1) or not _hex(ack['request_sha256']) or
            ack['status'] != 'OK' or type(ack['body']) is not dict):
        _fail('handoff ACK invalid')


def _pidfd(fd, pid):
    if type(fd) is not int or fd < 0 or os.readlink('/proc/self/fd/%d' % fd) != 'anon_inode:[pidfd]':
        _fail('pidfd invalid')
    with open('/proc/self/fdinfo/%d' % fd, 'r', encoding='ascii') as stream:
        lines = stream.read(4097).splitlines()
    pids = [line[4:].strip() for line in lines if line.startswith('Pid:')]
    if pids != [str(pid)] or select.select([fd], [], [], 0)[0]:
        _fail('pidfd does not hold live child')


def _directory_fd(fd):
    if type(fd) is not int or fd < 0 or not stat.S_ISDIR(os.fstat(fd).st_mode):
        _fail('directory descriptor invalid')


def _record_body(kind, body):
    if kind == 'runtime_root':
        if (not _object(body, _RECORD_KEYS[kind]) or
                any(not _integer(body[key], minimum=1) for key in
                    ('broker_cookie', 'pid1_cookie', 'pid1_start_ns', 'root_mount_id', 'root_ino')) or
                not _integer(body['root_dev']) or not _integer(body['handoff_count']) or body['handoff_count'] != 1 or
                not _hex(body['pid1_exe_sha256']) or type(body['thread_ids']) is not list or
                not 1 <= len(body['thread_ids']) <= 4096 or
                any(not _integer(tid, minimum=1, maximum=(1 << 32) - 1) for tid in body['thread_ids']) or
                any(a >= b for a, b in zip(body['thread_ids'], body['thread_ids'][1:]))):
            _fail('runtime root record invalid')
    if kind == 'containerd_launcher':
        if (not _object(body, _RECORD_KEYS[kind]) or
                body['path'] != '/opt/g2/containerd-bin/containerd' or body['target_path'] != '/usr/bin/containerd' or
                not _hex(body['exe_sha256']) or not _hex(body['target_sha256']) or
                any(not _integer(body[key]) for key in ('exe_dev', 'target_dev')) or
                any(not _integer(body[key], minimum=1) for key in ('exe_ino', 'target_ino')) or
                (body['exe_dev'], body['exe_ino']) == (body['target_dev'], body['target_ino'])):
            _fail('containerd launcher binding invalid')
    if kind == 'selftest_case':
        if (type(body['name']) is not str or not body['name'] or not _hex(body['nonce'], _HEX32) or
                body['phase'] not in ('started', 'observed', 'settled') or
                any(body[k] is not None and not _integer(body[k], minimum=1) for k in ('actor_cookie', 'invocation_id')) or
                any(type(body[k]) is not list or any(not _integer(v, minimum=1) for v in body[k]) for k in
                    ('transaction_ids', 'guard_sequences', 'reference_cookies')) or
                (body['request_sha256'] is not None and not _hex(body['request_sha256'])) or
                (body['result'] is not None and not _integer(body['result'], minimum=-(1 << 63), maximum=(1 << 63) - 1)) or
                (body['error_code'] is not None and body['error_code'] not in _ERRORS)):
            _fail('selftest case record invalid')
    if kind == 'selftest_guard_status':
        status = body['status']
        if (body['name'] not in ('seal-unload-denied', 'seal-detach-denied') or
                not _hex(body['nonce'], _HEX32) or body['phase'] not in ('before', 'after') or
                type(status) is not dict or status.get('state') != 'sealed' or
                type(status.get('authority_phase')) is not int or status['authority_phase'] != 1 or
                not _integer(status.get('module_instance'), minimum=1) or
                type(status.get('link_ids')) is not list or len(status['link_ids']) != 3 or
                any(not _integer(v, minimum=1, maximum=(1 << 32) - 1) for v in status['link_ids']) or
                len(set(status['link_ids'])) != 3 or status.get('link_attached') != [True, True, True] or
                any(type(v) is not bool for v in status['link_attached'])):
            _fail('selftest guard status invalid')
    if kind == 'control_rejection':
        if (not _integer(body['request_id'], minimum=1) or
                ((body['principal_id'] is not None or body['nonce'] is not None) if body['operation'] == 'BIND_RUNTIME'
                 else (not _integer(body['principal_id'], minimum=1) or not _hex(body['nonce'], _HEX32))) or
                type(body['operation']) is not str or not body['operation'] or
                type(body['body']) is not dict or not _hex(body['request_sha256']) or
                body['status'] not in _ERRORS | {'OK'} or
                (body['status'] == 'OK' and body['code'] is not None) or
                (body['status'] != 'OK' and body['code'] != body['status']) or
                not _integer(body['guard_before']) or not _integer(body['guard_after'], minimum=body['guard_before'])):
            _fail('control rejection record invalid')
    if kind == 'audit' and not _object(body, _RECORD_KEYS['audit']):
        _fail('audit body schema invalid')
    enums = {
        'principal': {'role': _ROLES | {'RUNTIME', 'BOOTSTRAP_BROKER', 'BOOTSTRAP_AUDIT_READER', 'BOOTSTRAP_TERMINAL_OBSERVER'}, 'registration': {'launch_source', 'launch_reboot', 'realm_self', 'realm_child', 'recovery', 'runtime', 'bootstrap'}, 'terminal': {None, 'exit', 'revoked'}},
        'grant': {'phase': {'admitted', 'active', 'settling', 'retired', 'revoked'}, 'retirement': {'auto_empty', 'explicit_cleanup'}},
        'invocation': {'command': {'features', 'create', 'start', 'state', 'kill', 'delete', 'exec'}, 'state': {'bound', 'running', 'settled', 'failed'}},
        'daemon': {'mode': {'initial', 'reboot'}, 'state': {'stopped', 'executing', 'exited'}},
        'transaction': {'state': {'prepared', 'committed', 'effect_observed', 'response_settled', 'retired'}},
        'object': {'class': {'program', 'map'}, 'state': {'live', 'id_removed', 'reclaim_pending'}},
        'attachment': {'state': {'attached', 'detached', 'offlining', 'released'}},
        'audit': {'context': {'syscall', 'standalone'}},
        'checkpoint': {'state': {'quiesced', 'released', 'failed'}},
        'registry': {'action': {'insert', 'update', 'delete'}, 'map': {'broker_token', 'guard_liveness', 'realm_targets', 'retire_tasks'}},
        'exec_admission': {'observation': {'app_rollback_dump', 'app_rollback_restore', 'negative_pg_dump', 'pg_controldata'}, 'state': {'admitted', 'bound', 'cancelled', 'settled'}},
        'endpoint_transition': {'state': {'begun', 'completed', 'closed'}},
        'probe_import': {'fixture': {None, 'a', 'b'}, 'result': {'imported', 'mismatch', 'ebadf', 'esrch', 'denied'}},
        'selftest_runtime_stage': {'mode': {'nested-buffer-race', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'extra-fd-delays-removal'}, 'phase': {'armed', 'captured', 'mutated', 'committed', 'effect_observed', 'replied', 'cancelled', 'settled', 'injected'}},
    }.get(kind, {})
    for key, choices in enums.items():
        if body[key] not in choices:
            _fail('transcript enum invalid')
    ids = {
        'principal': ('id', 'task_cookie', 'tgid', 'tid', 'start_ns'),
        'grant': ('id', 'owner', 'invocation'),
        'invocation': ('id', 'principal_id', 'task_cookie', 'daemon_cookie', 'generation'),
        'daemon': ('cookie', 'owner', 'task_cookie', 'start_ns', 'watch_sequence'),
        'transaction': ('id', 'executor_cookie'),
        'object': ('cookie', 'kernel_id', 'owner_transaction', 'publication_seq'),
        'attachment': ('id', 'grant_id', 'enrollment', 'root_cookie', 'cgroup_id', 'program_cookie', 'transaction_id'),
        'cgroup_enrollment': ('grant_id', 'enrollment', 'root_cookie', 'cgroup_id', 'invocation_id', 'requester_cookie', 'notification_id'),
        'held_file': ('cookie', 'inode'), 'grant_object_cleanup': ('grant_id', 'creator_transaction_id', 'object_cookie'),
        'runtime_exec': ('invocation_id', 'task_cookie', 'exec_generation', 'guard_sequence'),
        'notification_response': ('transaction_id', 'invocation_id', 'requester_cookie', 'notification_id'),
        'checkpoint': ('principal_id',),
        'registry': ('transaction_id',),
        'exec_admission': ('exec_id', 'grant_id', 'endpoint_generation'),
        'endpoint_transition': ('transition_id', 'grant_id', 'coordinator_cookie'),
        'module_instance': ('instance', 'loader_cookie'),
        'probe_actor': ('probe_cookie', 'task_cookie', 'tgid', 'tid', 'start_ns', 'parent_cookie'),
        'probe_import': ('probe_cookie', 'notification_id'),
        'selftest_runtime_stage': ('grant_id',),
    }.get(kind, ())
    if any(not _integer(body[key], minimum=1) for key in ids):
        _fail('transcript identity invalid')
    nullable = {'principal': ('parent_cookie',), 'invocation': ('grant_id', 'exec_id', 'listener_id'),
                'transaction': ('grant_id', 'invocation_id', 'requester_cookie', 'notification_id'),
                'object': ('removal_seq',), 'attachment': ('replaces', 'offline_seq', 'release_seq'),
                'audit': ('serial', 'object_cookie', 'transaction_id', 'task_cookie'),
                'registry': ('parent_transaction_id',),
                'held_file': ('invocation_id', 'requester_cookie', 'notification_id', 'baseline_cookie'),
                'grant_object_cleanup': ('guard_sequence',),
                'exec_admission': ('task_cookie',),
                'endpoint_transition': ('endpoint_generation',),
                'probe_actor': ('listener_id',),
                'probe_import': ('notifying_cookie', 'imported_device', 'imported_inode'),
                'selftest_runtime_stage': ('invocation_id', 'transaction_id', 'notifying_cookie', 'notification_id')}.get(kind, ())
    if any(body[key] is not None and not _integer(body[key], minimum=1) for key in nullable):
        _fail('transcript nullable identity invalid')
    hashes = {'principal': ('exe_sha256', 'argv_sha256', 'cgroup_sha256'), 'grant': ('config_sha256',),
              'daemon': ('helper_sha256', 'dockerd_sha256', 'argv_sha256'), 'runtime_exec': ('exe_sha256',),
              'endpoint_transition': ('scope_sha256',),
              'module_instance': ('module_sha256', 'observer_bindings_sha256'),
              'probe_actor': ('exe_sha256',)}.get(kind, ())
    if any(not _hex(body[key]) for key in hashes):
        _fail('transcript digest invalid')
    if kind == 'grant':
        if (type(body['config']) is not dict or hashlib.sha256(_encode(body['config'])).hexdigest() != body['config_sha256'] or
                type(body['container_id']) is not str or not _HEX64.fullmatch(body['container_id']) or
                type(body['purpose']) is not str or not body['purpose']):
            _fail('grant binding invalid')
    elif kind == 'principal':
        if (not _integer(body['uid'], maximum=(1 << 32) - 1) or not _integer(body['gid'], maximum=(1 << 32) - 1) or
                (body['script_sha256'] is not None and not _hex(body['script_sha256'])) or
                (body['role'] in _ROLES and body['script_sha256'] is None)):
            _fail('principal facts invalid')
    elif kind == 'invocation':
        for key in ('config_sha256', 'bundle_sha256', 'process_sha256'):
            if body[key] is not None and not _hex(body[key]):
                _fail('invocation digest invalid')
        if type(body['ancestry']) is not list or not body['ancestry']:
            _fail('invocation ancestry invalid')
        for node in body['ancestry']:
            if (not _object(node, ('task_cookie', 'parent_cookie', 'exec_generation', 'exe_sha256', 'fork_sequence', 'exec_sequence')) or
                    any(not _integer(node[key], minimum=1) for key in ('task_cookie', 'exec_generation', 'fork_sequence', 'exec_sequence')) or
                    (node['parent_cookie'] is not None and not _integer(node['parent_cookie'], minimum=1)) or not _hex(node['exe_sha256'])):
                _fail('invocation ancestry invalid')
        if body['command'] == 'features':
            if any(body[key] is not None for key in ('grant_id', 'container_id', 'config_sha256', 'bundle_sha256', 'process_sha256', 'exec_id', 'listener_id')):
                _fail('feature invocation has grant facts')
        elif (not _integer(body['grant_id'], minimum=1) or not _integer(body['listener_id'], minimum=1) or
              type(body['container_id']) is not str or not _HEX64.fullmatch(body['container_id']) or not _hex(body['config_sha256']) or
              ((body['command'] == 'create') != (body['bundle_sha256'] is not None)) or
              ((body['command'] == 'exec') != (body['process_sha256'] is not None and _integer(body['exec_id'], minimum=1)))):
            _fail('invocation binding invalid')
    elif kind == 'transaction':
        attr, cleanup = body['attributes'], body['cleanup']
        if (not _object(attr, ('attr_size', 'attr_hex')) or not _integer(attr['attr_size'], maximum=4096) or
                type(attr['attr_hex']) is not str or re.fullmatch(r'[0-9a-f]*', attr['attr_hex']) is None or
                len(attr['attr_hex']) != 2 * attr['attr_size'] or type(body['input_objects']) is not list or
                not _object(cleanup, ('policy', 'state', 'steps')) or
                cleanup['policy'] not in ('none', 'close_unattached', 'detach_then_close', 'auto_empty', 'close_borrowed_ref') or
                cleanup['state'] not in ('not_needed', 'pending', 'settled', 'unknown') or type(cleanup['steps']) is not list or
                (body['output'] is not None and type(body['output']) is not dict)):
            _fail('transaction data invalid')
        command = body['command']
        internal_detach = (body['grant_id'] is not None and command == 'PROG_DETACH' and
                           all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
        internal_replacement = (body['grant_id'] is not None and command in ('PROG_LOAD', 'PROG_ATTACH') and
                                all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
        if internal_detach and (attr != {'attr_size': 16, 'attr_hex': struct.pack('<IIII', 0, 0, 6, 0).hex()} or
                len(body['input_objects']) != 2 or
                [(item.get('field'), item.get('kind'), item.get('requester_fd')) for item in body['input_objects']
                 if type(item) is dict] != [('target_fd', 'cgroup', None), ('attach_bpf_fd', 'program', None)] or
                body['returned_fd'] is not None):
            _fail('internal detach fixed inputs differ')
        outputs = {'MAP_CREATE': ('object_cookie',), 'PROG_LOAD': ('object_cookie', 'log_true_size'),
                   'PROG_QUERY': ('attach_flags', 'program_ids', 'program_attach_flags'),
                   'PROG_GET_FD_BY_ID': ('object_cookie',), 'OBJ_GET_INFO_BY_FD': ('info_len', 'object_cookie', 'type', 'kernel_id', 'attributes'),
                   'PROG_ATTACH': ('attachment_id',), 'PROG_DETACH': ('attachment_id',)}
        if (type(command) is not str or (body['grant_id'] is not None and command not in outputs) or
                (body['result'] is not None and not _integer(body['result'], minimum=-(1 << 63), maximum=(1 << 63) - 1)) or
                (body['returned_fd'] is not None and not _integer(body['returned_fd'], maximum=(1 << 31) - 1)) or
                (body['program_bytes'] is not None and (type(body['program_bytes']) is not str or
                 re.fullmatch(r'[0-9a-f]*', body['program_bytes']) is None or len(body['program_bytes']) % 16 != 0 or len(body['program_bytes']) > 65536)) or
                (body['license'] is not None and (type(body['license']) is not str or not body['license'].isascii() or len(body['license']) > 63))):
            _fail('transaction values invalid')
        if body['grant_id'] is None:
            if (command not in set(outputs) | {'BTF_LOAD', 'LINK_CREATE', 'MAP_LOOKUP_ELEM',
                    'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE'} or
                    any(body[key] is not None for key in ('invocation_id', 'requester_cookie', 'notification_id'))):
                _fail('bootstrap transaction binding invalid')
            output = body['output']
            if output is not None and command == 'PROG_LOAD':
                if (not _object(output, ('object_cookie', 'log_true_size', 'func_info_hex')) or
                        (output['object_cookie'] is not None and not _integer(output['object_cookie'], minimum=1)) or
                        not _integer(output['log_true_size'], maximum=(1 << 32) - 1) or
                        type(output['func_info_hex']) is not str or not output['func_info_hex'] or
                        re.fullmatch(r'[0-9a-f]+', output['func_info_hex']) is None or len(output['func_info_hex']) % 16):
                    _fail('bootstrap function info bytes invalid')
            if output is not None and command == 'BTF_LOAD':
                if (not _object(output, ('btf_bytes', 'btf_sha256')) or
                        type(output['btf_bytes']) is not str or not output['btf_bytes'] or
                        re.fullmatch(r'[0-9a-f]+', output['btf_bytes']) is None or len(output['btf_bytes']) % 2 or
                        not _hex(output['btf_sha256']) or
                        hashlib.sha256(bytes.fromhex(output['btf_bytes'])).hexdigest() != output['btf_sha256'] or
                        body['program_bytes'] is not None or body['license'] is not None or body['input_objects']):
                    _fail('bootstrap BTF bytes invalid')
            if output is not None and command == 'LINK_CREATE':
                if (not _object(output, ('hook', 'link_id', 'program_cookie')) or
                        output['hook'] not in ('bpf', 'inode_rmdir', 'inode_mkdir') or
                        not _integer(output['link_id'], minimum=1, maximum=(1 << 32) - 1) or
                        not _integer(output['program_cookie'], minimum=1)):
                    _fail('bootstrap link output invalid')
            if output is not None and command == 'OBJ_GET_INFO_BY_FD' and not body['input_objects']:
                if (not _object(output, ('info_len', 'link_id', 'program_cookie', 'info_hex')) or
                        not _integer(output['info_len'], minimum=1, maximum=65536) or
                        not _integer(output['link_id'], minimum=1, maximum=(1 << 32) - 1) or
                        not _integer(output['program_cookie'], minimum=1) or type(output['info_hex']) is not str or
                        len(output['info_hex']) != output['info_len'] * 2 or
                        re.fullmatch(r'[0-9a-f]+', output['info_hex']) is None):
                    _fail('bootstrap link info invalid')
            if output is not None and command == 'OBJ_GET_INFO_BY_FD' and body['input_objects']:
                if (not _object(output, outputs[command]) or not _integer(output['info_len'], minimum=1, maximum=65536) or
                        not _integer(output['object_cookie'], minimum=1) or len(body['input_objects']) != 1 or
                        type(body['input_objects'][0]) is not dict or output['object_cookie'] != body['input_objects'][0].get('cookie') or
                        not _integer(output['type'], minimum=1, maximum=(1 << 32) - 1) or
                        not _integer(output['kernel_id'], minimum=1, maximum=(1 << 32) - 1) or
                        type(output['attributes']) is not dict):
                    _fail('bootstrap object info invalid')
            if command == 'PROG_QUERY':
                expected = bytearray(40)
                struct.pack_into('<I', expected, 4, 6)
                struct.pack_into('<I', expected, 24, 64)
                if (attr != {'attr_size': 40, 'attr_hex': expected.hex()} or
                        len(body['input_objects']) != 1 or type(body['input_objects'][0]) is not dict or
                        body['input_objects'][0].get('field') != 'query.target_fd' or
                        body['input_objects'][0].get('kind') != 'cgroup' or body['returned_fd'] is not None or
                        cleanup != {'policy': 'none', 'state': 'not_needed', 'steps': []} or
                        (output is not None and not _object(output, outputs[command]))):
                    _fail('internal query fixed inputs or output differ')
        elif not (internal_detach or internal_replacement) and any(not _integer(body[key], minimum=1) for key in ('invocation_id', 'requester_cookie', 'notification_id')):
            _fail('runtime transaction binding missing')
        if body['grant_id'] is None and command in ('MAP_LOOKUP_ELEM', 'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE') and body['output'] is not None:
            expected_output = ('registry_sequence', 'value_hex') if command == 'MAP_LOOKUP_ELEM' else ('registry_sequence',)
            if (not _object(body['output'], expected_output) or type(body['result']) is not int or
                    not _integer(body['output']['registry_sequence'], minimum=1) or
                    not (body['result'] == 0 or command == 'MAP_LOOKUP_ELEM' and body['result'] == -2 and
                         body['output']['value_hex'] is None) or
                    (command == 'MAP_LOOKUP_ELEM' and body['result'] == 0 and
                     (type(body['output']['value_hex']) is not str or len(body['output']['value_hex']) not in (16, 48, 80) or
                      re.fullmatch(r'[0-9a-f]+', body['output']['value_hex']) is None))):
                _fail('control transaction output invalid')
        if body['grant_id'] is not None and body['output'] is not None and not _object(body['output'], outputs[command]):
            _fail('transaction output schema invalid')
        if body['grant_id'] is not None and body['output'] is not None:
            output = body['output']
            for key in ('object_cookie', 'attachment_id'):
                if key in output and output[key] is not None and not _integer(output[key], minimum=1):
                    _fail('transaction output identity invalid')
            for key in ('log_true_size', 'info_len', 'attach_flags'):
                if key in output and not _integer(output[key], maximum=(1 << 32) - 1):
                    _fail('transaction output value invalid')
            if command == 'PROG_QUERY':
                if (type(output['program_ids']) is not list or type(output['program_attach_flags']) is not list or
                        len(output['program_ids']) > 64 or len(output['program_attach_flags']) != len(output['program_ids']) or
                        any(not _integer(item, minimum=1, maximum=(1 << 32) - 1) for item in output['program_ids']) or
                        any(not _integer(item, maximum=(1 << 32) - 1) for item in output['program_attach_flags'])):
                    _fail('query result invalid')
            if command == 'OBJ_GET_INFO_BY_FD':
                if (not _integer(output['type'], minimum=1, maximum=(1 << 32) - 1) or
                        not _integer(output['kernel_id'], minimum=1, maximum=(1 << 32) - 1)):
                    _fail('object info identity invalid')
                attrs = output['attributes']
                program_fields = ('name', 'tag', 'load_time', 'created_by_uid', 'translated_insn_count', 'verified_insns', 'attach_btf_id')
                map_fields = ('name', 'key_size', 'value_size', 'max_entries', 'map_flags', 'btf_id', 'btf_key_type_id', 'btf_value_type_id', 'map_extra')
                if not _object(attrs, program_fields) and not _object(attrs, map_fields):
                    _fail('object info schema invalid')
        if command != 'PROG_LOAD' and (body['program_bytes'] is not None or body['license'] is not None):
            _fail('unexpected program payload')
        for item in body['input_objects']:
            if (not _object(item, ('field', 'kind', 'cookie', 'requester_fd')) or type(item['field']) is not str or
                    item['kind'] not in ('cgroup', 'program', 'map', 'dev_null') or not _integer(item['cookie'], minimum=1) or
                    not (((internal_detach or internal_replacement) and item['requester_fd'] is None) or
                         (not (internal_detach or internal_replacement) and _integer(item['requester_fd'], maximum=(1 << 31) - 1)))):
                _fail('transaction input invalid')
        for step in cleanup['steps']:
            if (not _object(step, ('sequence', 'action', 'object_cookie', 'attachment_id', 'guard_sequence', 'result')) or
                    not _integer(step['sequence'], minimum=1) or step['action'] not in ('close_ref', 'detach', 'await_id_removed', 'await_cgroup_release')):
                _fail('cleanup step invalid')
            for key in ('object_cookie', 'attachment_id', 'guard_sequence'):
                if step[key] is not None and not _integer(step[key], minimum=1):
                    _fail('cleanup identity invalid')
            if step['result'] is not None and not _integer(step['result'], minimum=-(1 << 63), maximum=(1 << 63) - 1):
                _fail('cleanup result invalid')
        if cleanup['policy'] == 'close_borrowed_ref':
            steps = cleanup['steps']
            if (command != 'PROG_GET_FD_BY_ID' or body['grant_id'] is None or
                    not ((cleanup['state'] == 'pending' and steps == []) or
                         (cleanup['state'] in ('settled', 'unknown') and len(steps) == 1 and
                          _object(steps[0], ('sequence', 'action', 'object_cookie', 'attachment_id', 'guard_sequence', 'result')) and
                          steps[0]['sequence'] == 1 and steps[0]['action'] == 'close_ref' and
                          _integer(steps[0]['object_cookie'], minimum=1) and steps[0]['attachment_id'] is None and
                          steps[0]['guard_sequence'] is None and type(steps[0]['result']) is int and
                          (steps[0]['result'] == 0 if cleanup['state'] == 'settled' else steps[0]['result'] < 0)))):
                _fail('borrowed descriptor cleanup invalid')
    elif kind == 'object':
        keys = ('name', 'tag', 'load_time', 'created_by_uid', 'translated_insn_count', 'verified_insns', 'attach_btf_id') if body['class'] == 'program' else ('name', 'key_size', 'value_size', 'max_entries', 'map_flags', 'btf_id', 'btf_key_type_id', 'btf_value_type_id', 'map_extra')
        if not _object(body['attributes'], keys):
            _fail('object attributes invalid')
        attrs = body['attributes']
        if (not _integer(body['type'], minimum=1, maximum=(1 << 32) - 1) or type(attrs['name']) is not str or
                not attrs['name'].isascii() or len(attrs['name']) > 16 or any(not 0x20 <= ord(c) <= 0x7e for c in attrs['name'])):
            _fail('object name or type invalid')
        for key, value in attrs.items():
            if key not in ('name', 'tag') and not _integer(value, maximum=(_U64 if key in ('load_time', 'map_extra') else (1 << 32) - 1)):
                _fail('object attribute invalid')
        if body['class'] == 'program':
            if (type(body['bytes']) is not str or re.fullmatch(r'[0-9a-f]*', body['bytes']) is None or
                    len(body['bytes']) % 16 or type(attrs['tag']) is not str or re.fullmatch(r'[0-9a-f]{16}', attrs['tag']) is None):
                _fail('program object invalid')
        elif body['bytes'] is not None:
            _fail('map object contains program bytes')
        if body['class'] == 'map' and attrs['btf_id'] == 0 and (attrs['btf_key_type_id'] != 0 or attrs['btf_value_type_id'] != 0):
            _fail('map without BTF has BTF type IDs')
        source = body['attribute_source']
        if (not _object(source, ('kind', 'guard_sequence')) or source['kind'] != 'native_publication' or
                source['guard_sequence'] != body['publication_seq'] or
                not _integer(source['guard_sequence'], minimum=1)):
            _fail('object publication source invalid')
    elif kind == 'attachment':
        if not _integer(body['attach_type'], maximum=(1 << 32) - 1) or not _integer(body['flags'], maximum=(1 << 32) - 1):
            _fail('attachment facts invalid')
    elif kind == 'selftest_audit_reorder':
        if (body['name'] != 'audit-interleaved' or not _hex(body['nonce'], _HEX32) or
                type(body['received']) is not list or len(body['received']) != 4 or
                type(body['delivery_order']) is not list or len(body['delivery_order']) != 4 or
                any(type(index) is not int for index in body['delivery_order']) or
                sorted(body['delivery_order']) != [1, 2, 3, 4]):
            _fail('audit reorder witness invalid')
        for wire in body['received']:
            if not _object(wire, _RECORD_KEYS['audit_wire']):
                _fail('audit reorder receipt schema invalid')
            stamp = wire['stamp']
            if (not _object(stamp, ('seconds', 'milliseconds')) or
                    not _integer(stamp['seconds'], maximum=(1 << 63) - 1) or
                    not _integer(stamp['milliseconds'], maximum=999) or
                    not _integer(wire['serial'], minimum=1, maximum=(1 << 32) - 1) or
                    type(wire['record_type']) is not int or wire['record_type'] not in (1300, 1320) or
                    not _hex(wire['payload_sha256']) or not _integer(wire['occurrence'], minimum=1)):
                _fail('audit reorder receipt invalid')
    elif kind in ('audit_wire', 'audit_cut', 'audit_control', 'audit_readback'):
        if kind in ('audit_wire', 'audit_control'):
            stamp = body['stamp']
            if (not _object(stamp, ('seconds', 'milliseconds')) or not _integer(stamp['seconds'], maximum=(1 << 63) - 1) or
                    not _integer(stamp['milliseconds'], maximum=999) or not _integer(body['serial'], minimum=1, maximum=(1 << 32) - 1)):
                _fail('audit event stamp invalid')
        if kind == 'audit_wire':
            if (body['record_type'] not in (1300, 1305, 1306, 1309, 1320, 1334) or type(body['record_type']) is not int or
                    not _hex(body['payload_sha256']) or not _integer(body['occurrence'], minimum=1)):
                _fail('audit wire facts invalid')
        elif kind == 'audit_cut':
            if (any(not _integer(body[key], minimum=1) for key in ('principal_id', 'readback_sequence', 'boundary_sequence')) or
                    not _hex(body['boundary_sha256']) or not _integer(body['wire_count'])):
                _fail('audit cut invalid')
        elif kind == 'audit_control':
            actor = body['actor']
            if (not _object(actor, ('task_cookie', 'tgid', 'tid', 'start_ns', 'exe_sha256')) or
                    any(not _integer(actor[key], minimum=1) for key in ('task_cookie', 'tgid', 'tid', 'start_ns')) or
                    not _hex(actor['exe_sha256']) or body['phase'] not in ('early_setup', 'final_rules', 'final_lock', 'unicast_claim') or
                    body['change'] not in ('enabled', 'failure', 'rate_limit', 'backlog_limit', 'add_rule', 'audit_pid') or
                    any(value is not None and not _integer(value, maximum=(1 << 32) - 1) for value in (body['old_value'], body['new_value'])) or
                    ((body['change'] == 'add_rule') != (type(body['rule_list']) is list)) or
                    type(body['result']) is not int or body['result'] not in (0, 1)):
                _fail('audit control invalid')
            if body['change'] == 'add_rule':
                if (body['old_value'] is not None or body['new_value'] is not None or
                        body['rule_list'] != [4] or any(type(item) is not int for item in body['rule_list'])):
                    _fail('audit rule control invalid')
            elif body['old_value'] is None or body['new_value'] is None or body['rule_list'] is not None:
                _fail('audit setting control invalid')
        else:
            if (any(not _integer(body[key], minimum=1) for key in ('reader_cookie', 'request_id')) or
                    not _hex(body['nonce'], _HEX32) or body['check'] not in ('status', 'configuration') or
                    type(body['queries']) is not list or len(body['queries']) != (1 if body['check'] == 'status' else 3)):
                _fail('audit readback invalid')
            sequences = set()
            expected = [1000] if body['check'] == 'status' else [1000, 1013, 1000]
            for query, request_type in zip(body['queries'], expected):
                if (not _object(query, ('request_type', 'request_sequence', 'request_portid', 'replies')) or
                        query['request_type'] != request_type or type(query['request_type']) is not int or
                        not _integer(query['request_sequence'], minimum=1, maximum=(1 << 32) - 1) or
                        not _integer(query['request_portid'], minimum=1, maximum=(1 << 32) - 1) or
                        type(query['replies']) is not list or not query['replies']):
                    _fail('audit query invalid')
                if query['request_sequence'] in sequences:
                    _fail('audit query sequence repeated')
                sequences.add(query['request_sequence'])
                types = [reply.get('type') if type(reply) is dict else None for reply in query['replies']]
                if ((request_type == 1000 and types != [1000]) or
                        (request_type == 1013 and (types[-1:] != [3] or any(value != 1013 for value in types[:-1])))):
                    _fail('audit query completion invalid')
                for reply in query['replies']:
                    if (not _object(reply, ('source_pid', 'source_groups', 'length', 'type', 'flags', 'sequence', 'header_pid', 'payload_hex')) or
                            any(not _integer(reply[key], maximum=(1 << 32) - 1) for key in ('source_pid', 'source_groups', 'length', 'sequence', 'header_pid')) or
                            any(not _integer(reply[key], maximum=65535) for key in ('type', 'flags')) or
                            reply['source_pid'] != 0 or reply['source_groups'] != 0 or reply['sequence'] != query['request_sequence'] or
                            type(reply['payload_hex']) is not str or re.fullmatch(r'[0-9a-f]*', reply['payload_hex']) is None or
                            len(reply['payload_hex']) % 2 or reply['length'] != 16 + len(reply['payload_hex']) // 2):
                        _fail('audit reply invalid')
    elif kind == 'audit':
        if (not _object(body['stamp'], ('seconds', 'milliseconds')) or not _integer(body['stamp']['seconds'], maximum=(1 << 63) - 1) or
                not _integer(body['stamp']['milliseconds'], maximum=999) or not _integer(body['serial'], minimum=1, maximum=(1 << 32) - 1)):
            _fail('audit stamp invalid')
        commands = {'MAP_CREATE', 'MAP_LOOKUP_ELEM', 'MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM', 'MAP_FREEZE',
                    'PROG_LOAD', 'PROG_QUERY', 'PROG_GET_FD_BY_ID', 'OBJ_GET_INFO_BY_FD',
                    'PROG_ATTACH', 'PROG_DETACH', 'LINK_CREATE', 'LINK_DETACH', 'BTF_LOAD'}
        if (body['record_type'] not in ('SYSCALL', 'BPF') or
                (body['command'] is not None and body['command'] not in commands) or
                (body['record_type'] == 'SYSCALL' and body['operation'] is not None) or
                (body['record_type'] == 'BPF' and body['operation'] not in ('LOAD', 'UNLOAD')) or
                (body['result'] is not None and not _integer(body['result'], minimum=-(1 << 63), maximum=(1 << 63) - 1))):
            _fail('audit facts invalid')
        if body['record_type'] == 'BPF':
            if (body['object_cookie'] is None or body['transaction_id'] is None or body['result'] is not None or
                    ((body['context'] == 'standalone') != (body['task_cookie'] is None))):
                _fail('audit BPF identity invalid')
        elif body['context'] != 'syscall' or body['task_cookie'] is None or body['object_cookie'] is not None or body['result'] is None:
            _fail('audit syscall identity invalid')
        raw = body['raw']
        if raw is not None:
            if (body['record_type'] != 'SYSCALL' or body['context'] != 'syscall' or
                    body['transaction_id'] is not None or body['object_cookie'] is not None or
                    body['command'] not in _COMMAND_IDS or
                    not _object(raw, ('tgid', 'syscall_payload_sha256', 'syscall_occurrence',
                                      'eoe_payload_sha256', 'eoe_occurrence')) or
                    not _integer(raw['tgid'], minimum=1, maximum=(1 << 32) - 1) or
                    not _hex(raw['syscall_payload_sha256']) or not _hex(raw['eoe_payload_sha256']) or
                    not _integer(raw['syscall_occurrence'], minimum=1) or
                    not _integer(raw['eoe_occurrence'], minimum=1)):
                _fail('raw audit provenance invalid')
    elif kind == 'checkpoint':
        if (not _hex(body['nonce'], _HEX32) or not _integer(body['begin_watermark']) or
                not _integer(body['end_watermark']) or body['end_watermark'] < body['begin_watermark'] or
                not _hex(body['tail_before'], nonzero=False)):
            _fail('checkpoint facts invalid')
    elif kind == 'guard':
        if not _object(body['record'], ('sequence', 'kind', 'flags', 'object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'task_cookie', 'task_start_ns', 'audit_serial', 'operation', 'result', 'parent_cookie', 'detail', 'object_snapshot')):
            _fail('guard record invalid')
        value = body['record']
        if (not _integer(value['sequence'], minimum=1) or not _integer(value['kind'], minimum=1, maximum=16) or
                not _integer(value['flags'], maximum=7) or
                not _integer(value['result'], minimum=-(1 << 31), maximum=(1 << 31) - 1)):
            _fail('guard record values invalid')
        for key in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'task_cookie', 'task_start_ns', 'audit_serial', 'operation', 'parent_cookie'):
            if not _integer(value[key]):
                _fail('guard field invalid')
        snapshot = value['object_snapshot']
        if value['kind'] in (1, 3):
            if (not _object(snapshot, ('version', 'type', 'attributes')) or type(snapshot['version']) is not int or
                    snapshot['version'] != 1 or not _integer(snapshot['type'], minimum=1, maximum=(1 << 32) - 1)):
                _fail('guard publication snapshot invalid')
            attrs = snapshot['attributes']
            keys = (('name', 'tag', 'load_time', 'created_by_uid', 'translated_insn_count', 'verified_insns', 'attach_btf_id')
                    if value['kind'] == 1 else
                    ('name', 'key_size', 'value_size', 'max_entries', 'map_flags', 'btf_id', 'btf_key_type_id', 'btf_value_type_id', 'map_extra'))
            if (not _object(attrs, keys) or type(attrs['name']) is not str or not attrs['name'].isascii() or
                    len(attrs['name']) > 16 or any(not 0x20 <= ord(c) <= 0x7e for c in attrs['name']) or
                    any(not _integer(v, maximum=((1 << 64) - 1 if k in ('load_time', 'map_extra') else (1 << 32) - 1))
                        for k, v in attrs.items() if k not in ('name', 'tag')) or
                    (value['kind'] == 1 and (type(attrs['tag']) is not str or re.fullmatch(r'[0-9a-f]{16}', attrs['tag']) is None))):
                _fail('guard publication attributes invalid')
            if value['kind'] == 3 and attrs['btf_id'] == 0 and (attrs['btf_key_type_id'] != 0 or attrs['btf_value_type_id'] != 0):
                _fail('guard map without BTF has BTF type IDs')
        elif snapshot is not None:
            _fail('non-publication guard has snapshot')
        if value['kind'] < 8:
            detail = value['detail']
            timestamped = (value['kind'] in (2, 4, 5, 6) and
                           _object(detail, ('observed_mono_ns',)) and
                           _integer(detail['observed_mono_ns'], minimum=1))
            if (detail is not None and not timestamped or value['flags'] & 1 or
                    value['kind'] == 7 and value['audit_serial'] != 0):
                _fail('guard detail invalid')
        elif value['kind'] == 8:
            detail = value['detail']
            if (not _object(detail, ('seconds', 'nanoseconds')) or not _integer(detail['seconds'], maximum=(1 << 63) - 1) or
                    not _integer(detail['nanoseconds'], maximum=999999999) or not _integer(value['audit_serial'], minimum=1, maximum=(1 << 32) - 1)):
                _fail('guard audit stamp invalid')
        elif value['kind'] in (9, 10, 11, 12):
            detail = value['detail']
            if (value['flags'] != 0 or any(value[key] != 0 for key in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'audit_serial', 'operation', 'result')) or
                    not _object(detail, ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation', 'exit_code')) or
                    any(not _integer(detail[key]) for key in ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation')) or
                    not _integer(detail['exit_code'], minimum=-(1 << 31), maximum=(1 << 31) - 1) or
                    (value['kind'] != 11 and detail['exit_code'] != 0)):
                _fail('guard task facts invalid')
        elif value['kind'] == 13:
            detail = value['detail']
            if (value['flags'] != 0 or any(value[key] != 0 for key in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'parent_cookie')) or
                    value['task_cookie'] == 0 or value['task_start_ns'] == 0 or
                    not _integer(value['audit_serial'], minimum=1, maximum=(1 << 32) - 1) or
                    not _object(detail, ('tgid', 'tid', 'seconds', 'nanoseconds', 'exe_dev', 'exe_ino')) or
                    any(not _integer(detail[k], minimum=1) for k in ('tgid', 'tid', 'exe_ino')) or
                    not _integer(detail['exe_dev']) or
                    not _integer(detail['seconds'], maximum=(1 << 63) - 1) or
                    not _integer(detail['nanoseconds'], maximum=999999999)):
                _fail('guard raw syscall detail invalid')
        elif value['kind'] == 14:
            if (value['flags'] != 0 or any(value[key] != 0 for key in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'audit_serial', 'result', 'parent_cookie')) or
                    value['task_cookie'] == 0 or value['task_start_ns'] == 0 or
                    value['operation'] not in (0, 2) or value['detail'] is not None):
                _fail('guard authority transition invalid')
        elif value['kind'] == 15:
            detail = value['detail']
            if (value['flags'] != 0 or any(value[key] != 0 for key in ('object_cookie', 'kernel_id', 'root_cookie', 'cgroup_id', 'audit_serial', 'operation', 'parent_cookie')) or
                    value['task_cookie'] == 0 or value['task_start_ns'] == 0 or
                    not _object(detail, ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation', 'exit_code')) or
                    any(not _integer(detail[key]) for key in ('tgid', 'tid', 'exe_dev', 'exe_ino', 'exec_generation')) or
                    not _integer(detail['exit_code'], minimum=-(1 << 31), maximum=(1 << 31) - 1) or
                    detail['exit_code'] != value['result']):
                _fail('guard broker exit detail invalid')
        else:
            detail = value['detail']
            if (value['flags'] != 0 or any(value[key] != 0 for key in ('root_cookie', 'cgroup_id', 'audit_serial', 'parent_cookie')) or
                    not _integer(value['object_cookie'], minimum=1) or not _integer(value['kernel_id'], minimum=1, maximum=(1 << 31) - 2) or
                    not _integer(value['task_cookie'], minimum=1) or not _integer(value['task_start_ns'], minimum=1) or
                    not _object(detail, ('first_removal_sequence', 'transaction_id', 'saved_cursor', 'requested_id', 'allocator_result', 'cursor_after')) or
                    any(not _integer(detail[k], minimum=1) for k in ('first_removal_sequence', 'transaction_id')) or
                    any(not _integer(detail[k], maximum=(1 << 32) - 1) for k in ('saved_cursor', 'requested_id', 'cursor_after')) or
                    not _integer(detail['allocator_result'], minimum=-(1 << 31), maximum=(1 << 31) - 1) or
                    detail['requested_id'] != value['kernel_id'] or value['operation'] not in (1, 2, 3) or
                    (value['operation'] == 1 and (detail['saved_cursor'], detail['allocator_result'], detail['cursor_after'], value['result']) != (0, 0, 0, 0)) or
                    (value['operation'] == 2 and (detail['allocator_result'] != 0 or detail['cursor_after'] != value['kernel_id'] or value['result'] != 0)) or
                    (value['operation'] == 3 and (detail['cursor_after'] != detail['saved_cursor'] or value['result'] != detail['allocator_result']))):
                _fail('guard selftest ID cursor detail invalid')
    elif kind == 'notification_denial':
        if (any(not _integer(body[key], minimum=1) for key in ('invocation_id', 'requester_cookie', 'notification_id')) or
                not _integer(body['command'], maximum=(1 << 32) - 1) or type(body['result']) is not int or
                body['result'] != -1 or not _integer(body['guard_before']) or not _integer(body['guard_after']) or
                body['guard_after'] < body['guard_before']):
            _fail('notification denial facts invalid')
    elif kind == 'notification_response':
        if (body['operation'] not in ('SEND', 'ADDFD_SEND') or type(body['ioctl_errno']) is not int or
                body['ioctl_errno'] not in (0, 2) or not _integer(body['guard_before']) or
                not _integer(body['guard_after'], minimum=body['guard_before'])):
            _fail('notification response facts invalid')
        if body['operation'] == 'SEND':
            response = body['response']
            if (not _object(response, ('value', 'error', 'flags')) or
                    not _integer(response['value'], minimum=-(1 << 63), maximum=(1 << 63) - 1) or
                    not _integer(response['error'], minimum=-4095, maximum=0) or
                    type(response['flags']) is not int or response['flags'] != 0 or
                    (response['error'] != 0 and response['value'] != 0) or body['returned_fd'] is not None):
                _fail('attempted notification SEND differs')
        elif (body['response'] is not None or
              not ((body['ioctl_errno'] == 0 and _integer(body['returned_fd'], maximum=(1 << 31) - 1)) or
                   (body['ioctl_errno'] == 2 and body['returned_fd'] is None))):
            _fail('attempted notification ADDFD_SEND differs')
    elif kind == 'notification_cancel':
        if (any(not _integer(body[key], minimum=1) for key in ('invocation_id', 'requester_cookie', 'notification_id')) or
                not _integer(body['command'], maximum=(1 << 32) - 1) or type(body['validation_errno']) is not int or
                body['validation_errno'] != 2 or not _integer(body['guard_before']) or not _integer(body['guard_after']) or
                body['guard_after'] < body['guard_before']):
            _fail('notification cancellation facts invalid')
    elif kind == 'selftest_fd_stage':
        if (not _hex(body['nonce'], _HEX32) or not _integer(body['grant_id'], minimum=1) or
                body['class'] not in ('map', 'program') or body['phase'] not in ('armed', 'entered', 'restored')):
            _fail('FD selftest stage identity invalid')
        fields = ('transaction_id', 'saved_soft', 'hard_limit', 'test_soft', 'original_fd_count', 'identity_fd_count', 'owned_fd_count', 'errno')
        if body['phase'] == 'armed':
            if any(body[key] is not None for key in fields):
                _fail('armed FD selftest has resource facts')
        elif (not _integer(body['transaction_id'], minimum=1) or
              not _integer(body['saved_soft'], minimum=256) or not _integer(body['hard_limit'], minimum=body['saved_soft']) or
              body['test_soft'] != 256 or type(body['test_soft']) is not int or
              not _integer(body['original_fd_count'], minimum=1, maximum=128) or
              not _integer(body['identity_fd_count'], minimum=1, maximum=128) or
              body['identity_fd_count'] != body['original_fd_count'] or
              not _integer(body['owned_fd_count'], maximum=256) or
              body['original_fd_count'] + body['identity_fd_count'] + body['owned_fd_count'] != 256 or
              (body['phase'] == 'entered' and body['errno'] is not None) or
              (body['phase'] == 'restored' and (type(body['errno']) is not int or body['errno'] != 24))):
            _fail('FD selftest resource facts invalid')
    elif kind == 'held_file':
        if (body['kind'] != 'dev_null' or body['source'] not in ('bootstrap', 'runtime_import') or
                any(not _integer(body[key], minimum=1) for key in ('device', 'rdev')) or
                any(not _integer(body[key], maximum=(1 << 32) - 1) for key in ('mode', 'uid', 'gid')) or
                not stat.S_ISCHR(body['mode']) or stat.S_IMODE(body['mode']) != 0o666 or
                body['uid'] != 0 or body['gid'] != 0 or os.major(body['rdev']) != 1 or os.minor(body['rdev']) != 3):
            _fail('held device facts invalid')
        binding = ('invocation_id', 'requester_cookie', 'notification_id', 'requester_fd', 'baseline_cookie')
        if not ((body['source'] == 'bootstrap' and all(body[key] is None for key in binding)) or
                (body['source'] == 'runtime_import' and all(body[key] is not None for key in binding) and
                 _integer(body['requester_fd'], maximum=(1 << 31) - 1))):
            _fail('held device source binding invalid')
    elif kind == 'grant_object_cleanup':
        if (type(body['step']) is not int or body['step'] not in (1, 2) or
                not _integer(body['result'], minimum=-(1 << 63), maximum=(1 << 63) - 1) or
                not ((body['step'] == 1 and body['action'] == 'close_ref' and body['guard_sequence'] is None) or
                     (body['step'] == 2 and body['action'] == 'await_id_removed' and body['guard_sequence'] is not None))):
            _fail('grant object cleanup facts invalid')
    elif kind == 'registry':
        if body['map'] == 'broker_token':
            if (body['grant_id'] is not None or body['parent_transaction_id'] is not None or body['action'] != 'insert' or
                    not _object(body['key'], ('pidfd',)) or not _object(body['value'], ('cookie',)) or
                    not _integer(body['key']['pidfd'], maximum=(1 << 31) - 1) or
                    not _integer(body['value']['cookie'], minimum=1)):
                _fail('bootstrap broker token binding invalid')
            return
        if body['map'] == 'guard_liveness':
            if (body['grant_id'] is not None or body['parent_transaction_id'] is not None or body['action'] != 'update' or
                    not _object(body['key'], ('slot',)) or not _object(body['value'], ('phase',)) or
                    body['key']['slot'] != 0 or type(body['key']['slot']) is not int or
                    body['value']['phase'] != 1 or type(body['value']['phase']) is not int):
                _fail('bootstrap guard liveness binding invalid')
            return
        if not _integer(body['grant_id'], minimum=1):
            _fail('registry grant unavailable')
        if type(body['key']) is not dict or (body['action'] == 'delete') != (body['value'] is None):
            _fail('registry record invalid')
        key_fields = ('root_kn_id', 'kn_id') if body['map'] == 'realm_targets' else ('root_kn_id', 'kn_id', 'tgid', 'leader_start_ns')
        value_fields = ('enrollment', 'grant_id', 'phase', 'retirement') if body['map'] == 'realm_targets' else ('enrollment', 'grant_id', 'invocation_id', 'exe_dev', 'exe_ino')
        if not _object(body['key'], key_fields) or (body['value'] is not None and not _object(body['value'], value_fields)):
            _fail('registry binding schema invalid')
        if any(not _integer(value, minimum=1) for value in body['key'].values()):
            _fail('registry key invalid')
        if body['value'] is not None:
            for key, value in body['value'].items():
                if not _integer(value, minimum=0 if key == 'invocation_id' else 1):
                    _fail('registry value invalid')
            if (body['value']['grant_id'] != body['grant_id'] or
                    (body['map'] == 'realm_targets' and
                     (body['value']['phase'] not in (1, 2, 3, 4) or body['value']['retirement'] not in (1, 2)))):
                _fail('registry grant differs')
    elif kind in ('exec_admission', 'endpoint_transition'):
        ep = body['endpoint']
        if ep is not None:
            if (not _object(ep, ('pid', 'start_ticks', 'netns_dev', 'netns_ino', 'network_id', 'endpoint_id',
                                 'ipv4', 'prefix', 'gateway', 'ipam_subnet', 'recovery_id')) or
                    any(not _integer(ep[k], minimum=1) for k in ('pid', 'start_ticks', 'netns_dev', 'netns_ino')) or
                    ep['prefix'] != 24 or type(ep['prefix']) is not int or
                    not _hex(ep['network_id']) or not _hex(ep['endpoint_id']) or
                    any(type(ep[k]) is not str or not ep[k] for k in ('ipv4', 'gateway', 'ipam_subnet')) or
                    (ep['recovery_id'] is not None and (type(ep['recovery_id']) is not str or
                     re.fullmatch(r'[a-z0-9_-]{1,128}', ep['recovery_id']) is None))):
                _fail('endpoint projection invalid')
        if kind == 'exec_admission':
            if ((body['docker_exec_id'] is not None and not _hex(body['docker_exec_id'])) or
                    (body['process_sha256'] is not None and not _hex(body['process_sha256'])) or
                    (body['observation'] == 'pg_controldata' and ep is not None) or
                    (body['observation'] != 'pg_controldata' and ep is None) or
                    (body['state'] == 'admitted' and (body['docker_exec_id'] is not None or body['task_cookie'] is not None or body['process_sha256'] is not None)) or
                    (body['state'] in ('bound', 'settled') and (body['docker_exec_id'] is None or body['process_sha256'] is None)) or
                    (body['state'] == 'settled' and body['task_cookie'] is None)):
                _fail('exec admission record invalid')
        else:
            if (type(body['recovery_id']) is not str or re.fullmatch(r'[a-z0-9_-]{1,128}', body['recovery_id']) is None or
                    (body['state'] == 'completed') != (body['endpoint_generation'] is not None and ep is not None)):
                _fail('endpoint transition record invalid')
    elif kind == 'module_instance':
        lf = body['load_file']
        if (not _object(lf, ('dev', 'ino', 'size')) or not _integer(lf['dev']) or
                not _integer(lf['ino'], minimum=1) or not _integer(lf['size'], minimum=1, maximum=128 * 1024 * 1024)):
            _fail('module instance load file invalid')
    elif kind == 'probe_actor':
        if type(body['name']) is not str or not body['name']:
            _fail('probe actor name invalid')
    elif kind == 'probe_import':
        if (not _integer(body['slot'], maximum=(1 << 32) - 1) or not _integer(body['guard_begin']) or
                not _integer(body['guard_end'], minimum=body['guard_begin'])):
            _fail('probe import record invalid')
    elif kind == 'selftest_runtime_stage':
        if not _hex(body['nonce'], _HEX32):
            _fail('selftest runtime stage nonce invalid')
        mut = body['mutation']
        if body['mode'] == 'extra-fd-delays-removal' and (
                mut is None or body['transaction_id'] is not None or body['signal'] is not None or
                not _integer(body['notifying_cookie'], minimum=1) or not _integer(body['notification_id'], minimum=1)):
            _fail('extra descriptor transfer identity invalid')
        if mut is not None:
            if body['mode'] == 'nested-buffer-race':
                if (not _object(mut, ('captured_sha256', 'changed_offset', 'original_byte', 'mutated_byte', 'write_complete', 'restored')) or
                        not _hex(mut['captured_sha256']) or mut['changed_offset'] != 0 or type(mut['changed_offset']) is not int or
                        not _integer(mut['original_byte'], maximum=255) or mut['mutated_byte'] != 255 or
                        type(mut['write_complete']) is not bool or type(mut['restored']) is not bool):
                    _fail('selftest runtime stage mutation invalid')
            elif body['mode'] in ('wrong-program-byte', 'wrong-flags', 'wrong-target'):
                fields = ('kind', 'before_sha256', 'after_sha256', 'attr_size', 'attr_hex', 'program_bytes', 'license', 'target_cookie')
                if body['mode'] == 'wrong-target':
                    fields += ('target_identity',)
                if (not _object(mut, fields) or
                        mut['kind'] != body['mode'] or not _hex(mut['before_sha256']) or not _hex(mut['after_sha256']) or
                        not _integer(mut['attr_size'], maximum=4096) or type(mut['attr_hex']) is not str or
                        re.fullmatch(r'[0-9a-f]*', mut['attr_hex']) is None or len(mut['attr_hex']) != 2 * mut['attr_size'] or
                        (mut['program_bytes'] is not None and (type(mut['program_bytes']) is not str or re.fullmatch(r'[0-9a-f]*', mut['program_bytes']) is None)) or
                        (mut['license'] is not None and type(mut['license']) is not str) or
                        (mut['target_cookie'] is not None and not _integer(mut['target_cookie'], minimum=1))):
                    _fail('selftest captured mutation invalid')
                if body['mode'] == 'wrong-target':
                    identity = mut['target_identity']
                    if (not _object(identity, ('original_device', 'original_inode', 'substitute_device', 'substitute_inode')) or
                            any(not _integer(identity[k], minimum=1) for k in identity) or
                            identity['original_device'] != identity['substitute_device'] or
                            identity['original_inode'] == identity['substitute_inode'] or mut['target_cookie'] is not None):
                        _fail('selftest target identity invalid')
            elif body['mode'] == 'extra-fd-delays-removal':
                if (body['phase'] != 'injected' or
                        not _object(mut, ('program_cookie', 'listener_id', 'installed_fd')) or
                        not _integer(mut['program_cookie'], minimum=1) or
                        not _integer(mut['listener_id'], minimum=1) or
                        not _integer(mut['installed_fd'], maximum=(1 << 31) - 1)):
                    _fail('extra descriptor transfer evidence invalid')
            else:
                _fail('unexpected selftest runtime mutation')
        sig = body['signal']
        if sig is not None:
            if (not _object(sig, ('task_cookie', 'signal', 'send_result')) or
                    not _integer(sig['task_cookie'], minimum=1) or sig['signal'] != 9 or
                    not _integer(sig['send_result'], minimum=-4095, maximum=0)):
                _fail('selftest runtime stage signal invalid')

def _record(record, boot, epoch, sequence, previous):
    if (not _object(record, ('version', 'boot_id', 'epoch', 'sequence', 'previous', 'kind', 'body')) or
            type(record['version']) is not int or record['version'] != 1 or
            record['boot_id'] != boot or record['epoch'] != epoch or
            not _integer(record['sequence'], minimum=1) or record['sequence'] != sequence or
            record['previous'] != previous or type(record['kind']) is not str or
            record['kind'] not in _RECORD_KEYS or not _object(record['body'], _RECORD_KEYS[record['kind']])):
        _fail('transcript record schema or chain differs')
    _record_body(record['kind'], record['body'])
    _encode(record)
    return hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + canonical(record)).hexdigest()


def _synchronized(method):
    def call(self, *args, **kwargs):
        if self._owner != os.getpid():
            _fail('principal belongs to another process')
        with self._lock:
            return method(self, *args, **kwargs)
    return call


def _observe_runtime_root(record, state):
    kind, body = record['kind'], record['body']
    if kind == 'principal' and body['role'] == 'BOOTSTRAP_BROKER':
        prior = state['broker']
        if (body['registration'] != 'bootstrap' or
                (prior is not None and any(body[key] != prior[key] for key in
                 ('id', 'task_cookie', 'parent_cookie', 'tgid', 'tid', 'start_ns')))):
            _fail('bootstrap broker identity changed')
        state['broker'] = body
    elif kind == 'audit_control' and body['phase'] in ('early_setup', 'final_rules', 'final_lock'):
        actor = body['actor']
        identity = tuple(actor[key] for key in ('task_cookie', 'tgid', 'tid', 'start_ns'))
        broker = state['broker']
        if (broker is None or actor['task_cookie'] != broker['parent_cookie'] or
                actor['tgid'] != 1 or actor['tid'] != 1 or
                (state['pid1'] is not None and identity != state['pid1'])):
            _fail('bootstrap PID1 control identity differs')
        state['pid1'] = identity
    elif kind == 'runtime_root':
        _record_body(kind, body)
        broker = state['broker']
        if (state['root'] is not None or broker is None or state['pid1'] is None or broker['terminal'] is not None or
                body['broker_cookie'] != broker['task_cookie'] or body['pid1_cookie'] != broker['parent_cookie'] or
                (body['pid1_cookie'], 1, 1, body['pid1_start_ns']) != state['pid1'] or
                broker['tgid'] not in body['thread_ids'] or
                (state['systemd'] is not None and body['pid1_exe_sha256'] != state['systemd'])):
            _fail('runtime root broker, parent, digest or handoff differs')
        state['root'] = body
    elif (kind == 'principal' and not (body['registration'] == 'bootstrap' and body['role'] in
          ('BOOTSTRAP_BROKER', 'BOOTSTRAP_AUDIT_READER', 'BOOTSTRAP_TERMINAL_OBSERVER')) or
          kind in ('grant', 'invocation', 'daemon', 'containerd_launcher', 'attachment', 'runtime_exec', 'cgroup_enrollment',
                   'notification_denial', 'notification_cancel', 'notification_response', 'grant_object_cleanup',
                   'exec_admission', 'endpoint_transition', 'selftest_fd_stage', 'selftest_runtime_stage') or
          kind == 'held_file' and body['source'] == 'runtime_import' or
          kind == 'registry' and body['grant_id'] is not None or
          kind == 'transaction' and (body['grant_id'] is not None or body['invocation_id'] is not None)):
        if state['root'] is None:
            _fail('runtime admission or effect precedes root handoff')


class BPFAuthority:
    """One principal, one connection and one non-extendable invocation deadline.

    Construct with ``from_launcher`` after the launcher passes FD 3.  All
    descriptors passed to registration helpers remain owned by the caller,
    except the explicitly transferred realm handoff write descriptor.
    """
    def __init__(self, ack, *, role, child_cookie, broker_sha256, deadline, handshake_deadline):
        self._owner = os.getpid()
        self._deadline = deadline
        _deadline(deadline)
        self.boot_id = boot_id()
        self.epoch = ack['epoch']
        self.run_token = ack['run_token']
        self.principal_id = ack['principal_id']
        self.role = role
        self.child_cookie = child_cookie
        self._next = 1
        self._boot_owner_principal_id = None
        self._boot_owner_next_request_id = None
        self._sock = None
        self._broker = None
        self._closed = False
        self._lock = threading.RLock()
        self._depth = 0
        self._checkpoint_nonce = None
        self._checkpoint_watermark = None
        self._checkpoint_tail = None
        self._checkpoint_deadline = None
        self._grants = {}
        self._evidence_sequence = 0
        self._evidence_tail = _ZERO
        self._evidence_hashes = [_ZERO]
        self._guard_sequence = 0
        self._invocations = {}
        self._listeners = {}
        self._observed_grants = {}
        self._cgroup_enrollments = {}
        self._cgroup_identities = {}
        self._cgroup_enrollment_ids = set()
        self._cgroup_root = None
        self._observed_transactions = {}
        self._internal_detaches = {}
        self._internal_queries = {}
        self._query_audits = {}
        self._observed_attachments = {}
        self._registry_records = {}
        self._deleted_registry_values = {}
        self._observed_registry = {}
        self._observed_principals = {}
        self._runtime_root = {'broker': None, 'root': None, 'systemd': None, 'pid1': None}
        self._guard_execs = {}
        self._guard_tasks = {}
        self._runtime_execs = {}
        self._exited_tasks = set()
        self._pinned_runc = None
        self._containerd_launcher = None
        self._launcher_daemons = {}
        self._pinned_containerd = None
        self._pinned_dockerd = None
        self._held_files = {}
        self._baseline_file = None
        self._grant_cleanup = {}
        self._notifications = set()
        self._prepared_notifications = {}
        self._cancelled_notifications = set()
        self._notification_responses = {}
        self._failed_checkpoint = None
        self._publications = {}
        self._live_object_ids = {}
        self._removals = {}
        self._objects = {}
        self._object_info = {}
        self._exec_admissions = {}
        self._endpoint_transitions = {}
        self._evidence_records = []
        self._selftest_requests = {}
        self._selftest_cases = {}
        self._extra_injections = {}
        self._selftest_internal = {}
        self._extra_probes = {}
        self._probe_actors = {}
        try:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            self._sock = sock
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            sock.settimeout(_deadline(min(deadline, handshake_deadline)))
            sock.connect('\0g2-bpf-control-' + self.boot_id)
            peer = struct.unpack('3i', sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
            if peer[1:] != (0, 0):
                _fail('broker peer credentials invalid')
            self._broker = _Process(peer[0], broker_sha256)
            self._peer = peer
            self._check()
        except BaseException:
            self.close()
            raise

    @classmethod
    def from_launcher(cls, *, launcher_sha256, broker_sha256, run_token, deadline, fd=3):
        """Consume and close FD 3; validate the actual live launcher before use."""
        parent = None
        try:
            _deadline(deadline)
            # The handshake consumes one original budget; the principal's
            # lifetime remains the independently inherited boot deadline.
            handoff_deadline = min(deadline, time.monotonic() + 60)
            if type(run_token) is not str or not run_token:
                _fail('run token invalid')
            parent = _Process(os.getppid(), launcher_sha256)
            ack = _handoff_read(fd, handoff_deadline)
            parent.check()
            if os.getppid() != parent.pid:
                _fail('launcher parent changed')
            _ack_common(ack, boot_id(), run_token)
            body = ack['body']
            if (ack['request_id'] != 1 or not _object(body, ('child_cookie', 'role')) or
                    not _integer(body['child_cookie'], minimum=1) or
                    body['role'] not in ('BOOT_OWNER', 'RECOVERY_PENDING')):
                _fail('launcher registration ACK invalid')
            return cls(ack, role=body['role'], child_cookie=body['child_cookie'],
                       broker_sha256=broker_sha256, deadline=deadline, handshake_deadline=handoff_deadline)
        finally:
            os.close(fd)
            if parent is not None:
                parent.close()

    @classmethod
    def from_realm_handoff(cls, *, fd, parent_pid, parent_starttime, parent_executable_sha256,
                           broker_sha256, run_token, epoch, deadline):
        """Consume a restart parent's REGISTER_REALM ACK and establish its new principal."""
        parent = None
        try:
            _deadline(deadline)
            handoff_deadline = min(deadline, time.monotonic() + 60)
            parent = _Process(parent_pid, parent_executable_sha256, parent_starttime)
            if os.getppid() != parent_pid:
                _fail('realm handoff parent differs')
            ack = _handoff_read(fd, handoff_deadline)
            parent.check()
            _ack_common(ack, boot_id(), run_token)
            body = ack['body']
            if (ack['epoch'] != epoch or not _object(body, ('principal_id', 'child_cookie')) or
                    not _integer(body['principal_id'], minimum=1) or
                    not _integer(body['child_cookie'], minimum=1)):
                _fail('realm transfer ACK invalid')
            transferred = dict(ack, principal_id=body['principal_id'])
            return cls(transferred, role='REALM_ROOT', child_cookie=body['child_cookie'],
                       broker_sha256=broker_sha256, deadline=deadline, handshake_deadline=handoff_deadline)
        finally:
            os.close(fd)
            if parent is not None:
                parent.close()

    def _check(self):
        if self._closed or self._owner != os.getpid() or self._sock is None or self._broker is None:
            _fail('principal is closed or belongs to another process')
        _deadline(self._deadline)
        if boot_id() != self.boot_id:
            _fail('boot changed')
        peer = struct.unpack('3i', self._sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
        if peer != self._peer:
            _fail('broker peer changed')
        self._broker.check()

    def close(self):
        self._closed = True
        if self._sock is not None:
            self._sock.close()
            self._sock = None
        if self._broker is not None:
            self._broker.close()
            self._broker = None

    def _rpc(self, operation, body, *, fds=(), deadline=None, allow_error=False):
        with self._lock:
            try:
                self._check()
                if deadline is not None:
                    _deadline(deadline)
                until = min(self._deadline, time.monotonic() + 60)
                if deadline is not None:
                    until = min(until, deadline)
                if self._checkpoint_deadline is not None:
                    until = min(until, self._checkpoint_deadline)
                _deadline(until)
                if self._next > _U64:
                    _fail('request sequence exhausted')
                request = {'version': 1, 'boot_id': self.boot_id, 'epoch': self.epoch,
                           'run_token': self.run_token, 'principal_id': self.principal_id,
                           'request_id': self._next, 'operation': operation, 'body': body}
                raw = _encode(request)
                ancillary = [] if not fds else [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', fds))]
                self._sock.settimeout(_deadline(until))
                if self._sock.sendmsg([raw], ancillary) != len(raw):
                    _fail('request was not sent atomically')
                self._sock.settimeout(_deadline(until))
                data, controls, flags, _ = self._sock.recvmsg(
                    _MAX + 1, socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(4096),
                    getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
                rights, credentials = [], []
                invalid = False
                try:
                    for level, kind, payload in controls:
                        if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                            descriptors = array.array('i')
                            descriptors.frombytes(payload[:len(payload) - len(payload) % descriptors.itemsize])
                            rights.extend(descriptors)
                            invalid = True
                        elif (level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS and
                              len(payload) == struct.calcsize('3i')):
                            credentials.append(struct.unpack('3i', payload))
                        else:
                            invalid = True
                    if flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or invalid or credentials != [self._peer]:
                        _fail('response credentials or descriptors invalid')
                    ack = _decode(data)
                finally:
                    for fd in rights:
                        os.close(fd)
                self._check()
                if (not _object(ack, _ACK_KEYS) or type(ack['version']) is not int or ack['version'] != 1 or
                        ack['boot_id'] != self.boot_id or ack['epoch'] != self.epoch or
                        ack['run_token'] != self.run_token or ack['principal_id'] != self.principal_id or
                        not _integer(ack['principal_id'], minimum=1) or
                        not _integer(ack['request_id'], minimum=1) or ack['request_id'] != self._next or
                        ack['request_sha256'] != hashlib.sha256(raw).hexdigest() or type(ack['status']) is not str or
                        type(ack['body']) is not dict):
                    _fail('response binding differs')
                if ack['status'] != 'OK':
                    if (ack['status'] not in _ERRORS or not _object(ack['body'], ('code', 'sequence')) or
                            ack['body']['code'] != ack['status'] or not _integer(ack['body']['sequence'])):
                        _fail('error response schema invalid')
                    if allow_error:
                        self._next += 1
                        return ack
                    _fail(ack['status'])
                self._next += 1
                return ack
            except BaseException:
                self.close()
                raise

    @_synchronized
    def expect_error_rpc(self, operation, body, expected_status='BPF_AUTH_DENIED', deadline=None):
        ack = self._rpc(operation, body, deadline=deadline, allow_error=True)
        if ack['status'] != expected_status:
            self.close()
            _fail('expected %s, got %s' % (expected_status, ack['status']))
        return ack['body']

    def _body(self, operation, body, keys, *, fds=(), deadline=None):
        try:
            ack = self._rpc(operation, body, fds=fds, deadline=deadline)
            if not _object(ack['body'], keys):
                _fail(operation + ' response schema invalid')
            return ack
        except BaseException:
            self.close()
            raise

    @_synchronized
    def register_realm(self, child_pid, child_starttime, scenario, child_pidfd, *, handoff_fd=None):
        if self.role != 'BOOT_OWNER' or self._grants or type(scenario) is not str or not scenario:
            _fail('realm registration unavailable')
        _pidfd(child_pidfd, child_pid)
        if not _integer(child_starttime, minimum=1) or process_start(child_pid) != child_starttime:
            _fail('realm child identity invalid')
        if child_pid != os.getpid() and handoff_fd is None:
            _fail('realm child handoff required')
        if child_pid == os.getpid() and handoff_fd is not None:
            _fail('self registration cannot hand off')
        try:
            ack = self._body('REGISTER_REALM', {'child_pid': child_pid, 'child_starttime': child_starttime,
                                               'scenario': scenario}, ('principal_id', 'child_cookie'), fds=(child_pidfd,))
            body = ack['body']
            if not _integer(body['principal_id'], minimum=1) or not _integer(body['child_cookie'], minimum=1):
                _fail('realm registration identity invalid')
            if child_pid == os.getpid():
                self._boot_owner_principal_id = self.principal_id
                self._boot_owner_next_request_id = self._next
                self.principal_id, self.child_cookie, self.role, self._next = body['principal_id'], body['child_cookie'], 'REALM_ROOT', 1
            else:
                transferred_fd, handoff_fd = handoff_fd, None
                _handoff_write(transferred_fd, ack, self._deadline)
                self.close()
            return dict(body)
        except BaseException:
            self.close()
            raise
        finally:
            if handoff_fd is not None:
                os.close(handoff_fd)

    @_synchronized
    def register_recovery(self, intent_sha256, original_boot_id, used_sha256, control_dir_fd, seed_dir_fd, case_fd):
        if (self.role != 'RECOVERY_PENDING' or not _hex(intent_sha256) or not _hex(used_sha256) or
                type(original_boot_id) is not str or not _UUID.fullmatch(original_boot_id) or original_boot_id == self.boot_id):
            _fail('recovery registration invalid')
        for fd in (control_dir_fd, seed_dir_fd):
            _directory_fd(fd)
        if not _integer(case_fd, maximum=(1 << 31) - 1):
            _fail('recovery case descriptor invalid')
        case_info = os.fstat(case_fd)
        if (not stat.S_ISREG(case_info.st_mode) or case_info.st_uid != 0 or case_info.st_gid != 0 or
                stat.S_IMODE(case_info.st_mode) != 0o400 or case_info.st_nlink != 1 or
                not 0 < case_info.st_size <= 4096):
            _fail('recovery case descriptor is not the immutable case file')
        try:
            body = self._body('REGISTER_RECOVERY', {'intent_sha256': intent_sha256, 'original_boot_id': original_boot_id,
                'used_sha256': used_sha256}, ('principal_id', 'child_cookie'), fds=(control_dir_fd, seed_dir_fd, case_fd))['body']
            if not _integer(body['principal_id'], minimum=1) or not _integer(body['child_cookie'], minimum=1):
                _fail('recovery registration identity invalid')
            self.principal_id, self.child_cookie, self.role, self._next = body['principal_id'], body['child_cookie'], 'RECOVERY_OBSERVER', 1
            return dict(body)
        except BaseException:
            self.close()
            raise

    @_synchronized
    def register_daemon(self, child_pid, child_starttime, mode, child_pidfd):
        if ((self.role, mode) not in (('BOOT_OWNER', 'initial'), ('RECOVERY_OBSERVER', 'reboot')) or
                not _integer(child_starttime, minimum=1) or process_start(child_pid) != child_starttime):
            _fail('daemon registration invalid')
        _pidfd(child_pidfd, child_pid)
        try:
            body = self._body('REGISTER_DAEMON', {'child_pid': child_pid, 'child_starttime': child_starttime, 'mode': mode},
                              ('daemon_cookie', 'watch_sequence'), fds=(child_pidfd,))['body']
            if not all(_integer(body[key], minimum=1) for key in body):
                _fail('daemon registration identity invalid')
            return dict(body)
        except BaseException:
            self.close()
            raise

    @_synchronized
    def admit_container(self, config, container_id, purpose):
        if (self.role not in _PURPOSES or type(config) is not dict or
                type(container_id) is not str or not re.fullmatch(r'[0-9a-f]{64}', container_id) or
                type(purpose) is not str or purpose not in _PURPOSES[self.role] or container_id in self._grants):
            _fail('container admission invalid')
        config_bytes = _encode(config)
        config = _decode(config_bytes)
        try:
            body = self._body('ADMIT', {'purpose': purpose, 'container_id': container_id, 'config': config,
                'config_sha256': hashlib.sha256(config_bytes).hexdigest()}, ('grant_id',))['body']
            if not _integer(body['grant_id'], minimum=1):
                _fail('grant identity invalid')
            self._grants[container_id] = body['grant_id']
            return body['grant_id']
        except BaseException:
            self.close()
            raise

    @_synchronized
    def selftest_fd_arm(self, container_id, object_class, nonce, deadline):
        if (self.role != 'REALM_ROOT' or container_id not in self._grants or
                object_class not in ('map', 'program') or not _hex(nonce, _HEX32)):
            _fail('FD selftest arm invalid')
        body = self._body('SELFTEST_FD_ARM', {'nonce': nonce, 'grant_id': self._grants[container_id],
                                               'class': object_class}, ('armed',), deadline=deadline)['body']
        if body['armed'] is not True:
            _fail('FD selftest arm response invalid')
        return body

    @_synchronized
    def selftest_fd_wrong_role(self, grant_id, nonce, deadline):
        if (self.role != 'REALM_ROOT' or not _integer(self._boot_owner_principal_id, minimum=1) or
                not _integer(self._boot_owner_next_request_id, minimum=1) or
                not _integer(grant_id, minimum=1) or not _hex(nonce, _HEX32)):
            _fail('FD wrong-role request invalid')
        principal, next_request = self.principal_id, self._next
        try:
            self.principal_id = self._boot_owner_principal_id
            self._next = self._boot_owner_next_request_id
            ack = self._rpc('SELFTEST_FD_ARM', {'nonce': nonce, 'grant_id': grant_id, 'class': 'map'},
                            deadline=deadline, allow_error=True)
            if ack['status'] != 'BPF_AUTH_DENIED':
                self.close()
                _fail('FD wrong-role request was not denied')
            self._boot_owner_next_request_id = self._next
            return ack
        finally:
            self.principal_id, self._next = principal, next_request

    @_synchronized
    def selftest_fd_replay(self, container_id, nonce, deadline):
        if self.role != 'REALM_ROOT' or container_id not in self._grants or not _hex(nonce, _HEX32):
            _fail('FD replay request invalid')
        body = {'nonce': nonce, 'grant_id': self._grants[container_id], 'class': 'map'}
        request_id = self._next
        first = self._rpc('SELFTEST_FD_ARM', body, deadline=deadline)
        if not _object(first['body'], ('armed',)) or first['body']['armed'] is not True:
            self.close()
            _fail('FD replay initial arm invalid')
        self._next = request_id
        replay = self._rpc('SELFTEST_FD_ARM', body, deadline=deadline)
        if replay != first:
            self.close()
            _fail('FD replay did not return the cached ACK')
        changed_nonce = os.urandom(16).hex()
        while changed_nonce == nonce or not _hex(changed_nonce, _HEX32):
            changed_nonce = os.urandom(16).hex()
        self._next = request_id
        changed = self._rpc('SELFTEST_FD_ARM', dict(body, nonce=changed_nonce), deadline=deadline, allow_error=True)
        if changed['status'] != 'BPF_AUTH_DENIED':
            self.close()
            _fail('changed FD replay was not denied')
        return changed

    @_synchronized
    def selftest_baseline_step(self, name, nonce, grant_id=None, deadline=None):
        if (self.role != 'REALM_ROOT' or name not in (
                'allowed-device-start', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'wrong-generation',
                'feature-map', 'extended-info-ebadf', 'replace-probe-ebadf', 'audit-interleaved', 'container-bpf',
                'seal-detach-denied', 'seal-unload-denied', 'attachment-replace', 'failure-cleanup',
                'cgroup-auto-retire', 'extra-fd-delays-removal', 'id-reuse', 'fd-arm-replay',
                'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second') or not _hex(nonce, _HEX32) or
                (grant_id is not None and not _integer(grant_id, minimum=1))):
            _fail('baseline selftest request invalid')
        body = self._body('SELFTEST_BASELINE_STEP', {'name': name, 'nonce': nonce, 'grant_id': grant_id},
                          ('started',), deadline=deadline)['body']
        if body['started'] is not True:
            _fail('baseline selftest response invalid')
        if name in ('extra-fd-delays-removal', 'attachment-replace'):
            self._selftest_requests[nonce] = (name, grant_id)
        return body

    @_synchronized
    def register_container_raw_denial(self, container_id, child_pid, child_starttime, child_pidfd, deadline=None):
        if (self.role != 'REALM_ROOT' or container_id not in self._grants or
                not _integer(child_starttime, minimum=1)):
            _fail('container raw-denial registration invalid')
        _pidfd(child_pidfd, child_pid)
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': 'container-raw-denial', 'child_pid': child_pid, 'child_starttime': child_starttime,
            'grant_id': self._grants[container_id],
        }, ('probe_cookie', 'listener_id'), fds=(child_pidfd,), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1) or body['listener_id'] is not None:
            _fail('container raw-denial registration response invalid')
        return dict(body)

    @_synchronized
    def register_extra_fd_probe(self, child_pid, child_starttime, child_pidfd, listener_fd, deadline=None):
        if self.role != 'REALM_ROOT' or not _integer(child_starttime, minimum=1):
            _fail('extra descriptor probe registration invalid')
        _pidfd(child_pidfd, child_pid)
        if not _integer(listener_fd, maximum=(1 << 31) - 1):
            _fail('extra descriptor probe listener invalid')
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': 'extra-fd-delays-removal', 'child_pid': child_pid, 'child_starttime': child_starttime,
        }, ('probe_cookie', 'listener_id'), fds=(child_pidfd, listener_fd), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1) or not _integer(body['listener_id'], minimum=1):
            _fail('extra descriptor probe registration response invalid')
        self._extra_probes[body['probe_cookie']] = (child_pid, body['listener_id'])
        return dict(body)

    @_synchronized
    def register_seal_unload_probe(self, child_pid, child_starttime, child_pidfd, result_fd, deadline=None):
        if self.role != 'REALM_ROOT' or not _integer(child_starttime, minimum=1) or not stat.S_ISFIFO(os.fstat(result_fd).st_mode):
            _fail('seal unload probe registration invalid')
        _pidfd(child_pidfd, child_pid)
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': 'seal-unload-denied', 'child_pid': child_pid, 'child_starttime': child_starttime,
        }, ('probe_cookie', 'listener_id'), fds=(child_pidfd, result_fd), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1) or body['listener_id'] is not None:
            _fail('seal unload probe registration response invalid')
        return dict(body)

    @_synchronized
    def register_seal_detach_probe(self, child_pid, child_starttime, child_pidfd, transfer_fd, deadline=None):
        if (self.role != 'REALM_ROOT' or not _integer(child_starttime, minimum=1) or
                not stat.S_ISSOCK(os.fstat(transfer_fd).st_mode)):
            _fail('seal detach probe registration invalid')
        _pidfd(child_pidfd, child_pid)
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': 'seal-detach-denied', 'child_pid': child_pid, 'child_starttime': child_starttime,
        }, ('probe_cookie', 'listener_id'), fds=(child_pidfd, transfer_fd), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1) or body['listener_id'] is not None:
            _fail('seal detach probe registration response invalid')
        return dict(body)

    @_synchronized
    def register_tid_reuse_child(self, parent_probe_cookie, child_pid, child_starttime, child_pidfd, deadline=None):
        if (self.role != 'REALM_ROOT' or not _integer(parent_probe_cookie, minimum=1) or
                not _integer(child_starttime, minimum=1)):
            _fail('tid reuse child registration invalid')
        _pidfd(child_pidfd, child_pid)
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': 'tid-reuse-child', 'parent_probe_cookie': parent_probe_cookie,
            'child_pid': child_pid, 'child_starttime': child_starttime,
        }, ('probe_cookie', 'listener_id'), fds=(child_pidfd,), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1) or not _integer(body['listener_id'], minimum=1):
            _fail('tid reuse child registration response invalid')
        return dict(body)

    @_synchronized
    def register_selftest_probe(self, name, child_pid, child_starttime, child_pidfd,
                                listener_fd=None, fixture_a_fd=None, fixture_b_fd=None, deadline=None):
        if (self.role != 'REALM_ROOT' or type(name) is not str or not name or
                not _integer(child_starttime, minimum=1)):
            _fail('selftest probe registration invalid')
        _pidfd(child_pidfd, child_pid)
        fds = [child_pidfd]
        if listener_fd is not None or fixture_a_fd is not None or fixture_b_fd is not None:
            if any(fd is None or not _integer(fd, maximum=(1 << 31) - 1) for fd in (listener_fd, fixture_a_fd, fixture_b_fd)):
                _fail('selftest probe fixture descriptors invalid')
            fds.extend((listener_fd, fixture_a_fd, fixture_b_fd))
        body = self._body('REGISTER_SELFTEST_PROBE', {
            'name': name, 'child_pid': child_pid, 'child_starttime': child_starttime
        }, ('probe_cookie', 'listener_id'), fds=tuple(fds), deadline=deadline)['body']
        if not _integer(body['probe_cookie'], minimum=1):
            _fail('selftest probe cookie invalid')
        return dict(body)

    @_synchronized
    def selftest_runtime_arm(self, container_id, mode, nonce, deadline=None):
        if (self.role != 'REALM_ROOT' or container_id not in self._grants or
                mode not in ('nested-buffer-race', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel') or
                not _hex(nonce, _HEX32)):
            _fail('selftest runtime arm invalid')
        body = self._body('SELFTEST_RUNTIME_ARM', {
            'nonce': nonce, 'grant_id': self._grants[container_id], 'mode': mode
        }, ('armed',), deadline=deadline)['body']
        if body['armed'] is not True:
            _fail('selftest runtime arm response invalid')
        return body

    @_synchronized
    def selftest_revoke_and_close(self, nonce, deadline=None):
        if self.role != 'REALM_ROOT' or not _hex(nonce, _HEX32):
            _fail('selftest revoke-and-close invalid')
        body = self._body('SELFTEST_REVOKE_AND_CLOSE', {'nonce': nonce}, ('started',), deadline=deadline)['body']
        if body['started'] is not True:
            _fail('selftest revoke-and-close response invalid')
        return body

    @_synchronized
    def selftest_observation_fault(self, variant, deadline=None):
        if self.role != 'REALM_ROOT' or type(variant) is not str or not variant:
            _fail('selftest observation fault invalid')
        body = self._body('SELFTEST_OBSERVATION_FAULT', {'variant': variant}, ('injected',), deadline=deadline)['body']
        if body['injected'] is not True:
            _fail('selftest observation fault response invalid')
        return body


    @_synchronized
    def _admit_exec(self, container_id, observation, deadline=None):
        if (self.role not in _EXEC or container_id not in self._grants or
                type(observation) is not str or observation not in _EXEC[self.role]):
            _fail('exec admission invalid')
        try:
            body = self._body('ADMIT_EXEC', {'grant_id': self._grants[container_id], 'observation': observation},
                              ('exec_id',), deadline=deadline)['body']
            if not _integer(body['exec_id'], minimum=1):
                _fail('exec identity invalid')
            return body['exec_id']
        except BaseException:
            self.close()
            raise

    @_synchronized
    def track_exec(self, exec_id, docker_exec_id, deadline=None):
        if self.role not in _EXEC or not _integer(exec_id, minimum=1) or not _hex(docker_exec_id):
            _fail('exec tracking invalid')
        try:
            body = self._body('TRACK_EXEC', {'exec_id': exec_id, 'docker_exec_id': docker_exec_id},
                              ('tracked',), deadline=deadline)['body']
            if body['tracked'] is not True:
                _fail('exec tracking response invalid')
            return True
        except BaseException:
            self.close()
            raise

    @_synchronized
    def cancel_exec(self, exec_id, deadline=None):
        if self.role not in _EXEC or not _integer(exec_id, minimum=1):
            _fail('exec cancellation invalid')
        try:
            body = self._body('CANCEL_EXEC', {'exec_id': exec_id}, ('state',), deadline=deadline)['body']
            if body['state'] not in ('cancelled', 'settling', 'settled'):
                _fail('exec cancellation state invalid')
            return body['state']
        except BaseException:
            self.close()
            raise

    @contextlib.contextmanager
    def exec_observation(self, container_id, observation, deadline):
        exec_id = self._admit_exec(container_id, observation, deadline=deadline)
        state = {'tracked': False}

        def track(docker_exec_id):
            if state['tracked']:
                _fail('exec admission tracked twice')
            self.track_exec(exec_id, docker_exec_id, deadline=deadline)
            state['tracked'] = True
            return exec_id

        try:
            yield track
        finally:
            if not self._closed:
                while True:
                    status = self.cancel_exec(exec_id, deadline=deadline)
                    if status in ('cancelled', 'settled'):
                        break
                    if time.monotonic() >= deadline:
                        _fail('exec admission did not settle before deadline')
                    time.sleep(0.02)

    @_synchronized
    def begin_endpoint_transition(self, container_id, recovery_id, coordinator_pid, coordinator_starttime,
                                  coordinator_pidfd, scope_dir_fd, deadline=None):
        if (self.role != 'REALM_ROOT' or container_id not in self._grants or
                type(recovery_id) is not str or re.fullmatch(r'[a-z0-9_-]{1,128}', recovery_id) is None or
                not _integer(coordinator_starttime, minimum=1) or process_start(coordinator_pid) != coordinator_starttime):
            _fail('endpoint transition begin invalid')
        _pidfd(coordinator_pidfd, coordinator_pid)
        _directory_fd(scope_dir_fd)
        try:
            body = self._body('BEGIN_ENDPOINT_TRANSITION', {
                'grant_id': self._grants[container_id],
                'recovery_id': recovery_id,
                'coordinator_pid': coordinator_pid,
                'coordinator_starttime': coordinator_starttime,
            }, ('transition_id',), fds=(coordinator_pidfd, scope_dir_fd), deadline=deadline)['body']
            if not _integer(body['transition_id'], minimum=1):
                _fail('endpoint transition identity invalid')
            return body['transition_id']
        except BaseException:
            self.close()
            raise

    @_synchronized
    def end_endpoint_transition(self, transition_id, deadline=None):
        if self.role != 'REALM_ROOT' or not _integer(transition_id, minimum=1):
            _fail('endpoint transition end invalid')
        try:
            body = self._body('END_ENDPOINT_TRANSITION', {'transition_id': transition_id},
                              ('endpoint_generation',), deadline=deadline)['body']
            if not _integer(body['endpoint_generation'], minimum=1):
                _fail('endpoint generation invalid')
            return body['endpoint_generation']
        except BaseException:
            self.close()
            raise
    @_synchronized
    def settle_container(self, container_id, outcome, deadline):
        if container_id not in self._grants or outcome not in ('completed', 'failed', 'cancelled'):
            _fail('container settlement invalid')
        try:
            body = self._body('SETTLE', {'grant_id': self._grants[container_id], 'outcome': outcome}, ('state',), deadline=deadline)['body']
            if body['state'] not in ('settling', 'retired'):
                _fail('settlement state invalid')
            if body['state'] == 'retired':
                del self._grants[container_id]
            return body['state']
        except BaseException:
            self.close()
            raise

    @_synchronized
    def audit_cut(self, nonce, deadline):
        if self.role not in ('REALM_ROOT', 'RECOVERY_OBSERVER') or self._depth <= 0 or nonce != self._checkpoint_nonce:
            _fail('audit cut lacks active checkpoint')
        body = self._body('AUDIT_CUT', {'nonce': nonce}, ('boundary_sequence', 'boundary_sha256', 'wire_count'), deadline=deadline)['body']
        if (not _integer(body['boundary_sequence'], minimum=1) or not _hex(body['boundary_sha256']) or
                not _integer(body['wire_count'])):
            _fail('audit cut response invalid')
        self._verify_watermark(body['boundary_sequence'], body['boundary_sha256'], deadline)
        records = self._complete_evidence(deadline)
        cuts = [record for record in records if record['kind'] == 'audit_cut']
        if len(cuts) != 1:
            _fail('audit cut transcript is missing or duplicated')
        cut = cuts[0]
        value = cut['body']
        if (value['principal_id'] != self.principal_id or cut['sequence'] <= body['boundary_sequence'] or
                any(value[key] != body[key] for key in ('boundary_sequence', 'boundary_sha256', 'wire_count')) or
                value['readback_sequence'] > body['boundary_sequence']):
            _fail('audit cut transcript binding differs')
        readback = records[value['readback_sequence'] - 1]
        readers = {record['body']['id']: record['body'] for record in records[:value['readback_sequence']]
                   if record['kind'] == 'principal' and record['body']['role'] == 'BOOTSTRAP_AUDIT_READER'}
        if (readback['kind'] != 'audit_readback' or readback['body']['check'] != 'configuration' or len(readers) != 1 or
                readback['body']['reader_cookie'] != next(iter(readers.values()))['task_cookie']):
            _fail('audit cut configuration readback missing or unbound')
        return body

    @_synchronized
    def read_status(self, deadline=None):
        try:
            body = self._body('READ_STATUS', {}, ('state', 'sticky', 'sequence', 'lost', 'probe_missed'), deadline=deadline)['body']
            if (type(body['state']) is not str or body['state'] not in ('unsealed', 'sealed', 'dead') or
                    any(not _integer(body[key]) for key in ('sticky', 'sequence', 'lost', 'probe_missed'))):
                _fail('status schema invalid')
            return dict(body)
        except BaseException:
            self.close()
            raise

    def _observe_snapshot(self, record):
        kind, body = record['kind'], record['body']
        if kind == 'probe_actor' and body['name'] == 'extra-fd-delays-removal':
            if self._extra_probes.get(body['probe_cookie']) != (body['tid'], body['listener_id']):
                _fail('extra descriptor actor differs from its registered child')
            self._probe_actors[body['probe_cookie']] = body
        if kind == 'selftest_case' and body['name'] in ('extra-fd-delays-removal', 'attachment-replace'):
            request = self._selftest_requests.get(body['nonce'])
            if request is None or request[0] != body['name']:
                _fail('selftest case has no matching authorized request')
            previous = self._selftest_cases.get(body['nonce'])
            expected_phase = 'started' if previous is None else {'started': 'observed', 'observed': 'settled'}.get(previous['phase'])
            if body['phase'] != expected_phase:
                _fail('selftest case phases are duplicated or out of order')
            if body['phase'] in ('observed', 'settled'):
                invocation = self._invocations.get(body['invocation_id'])
                if invocation is None or invocation['grant_id'] != request[1]:
                    _fail('selftest case invocation differs from its grant')
                expected = self._selftest_internal.get(body['nonce'], set())
                if not expected.issubset(set(body['transaction_ids'])):
                    _fail('selftest case omitted its internal effects')
                if body['name'] == 'extra-fd-delays-removal':
                    stage = self._extra_injections.get(body['nonce'])
                    if (stage is None or stage['grant_id'] != request[1] or stage['invocation_id'] != body['invocation_id'] or
                            stage['notifying_cookie'] != body['actor_cookie'] or
                            body['reference_cookies'] != [stage['mutation']['program_cookie']]):
                        _fail('extra descriptor case differs from its injected transfer')
            self._selftest_cases[body['nonce']] = body
        elif kind == 'selftest_runtime_stage' and body['mode'] == 'extra-fd-delays-removal':
            if (self._selftest_requests.get(body['nonce']) != (body['mode'], body['grant_id']) or
                    self._selftest_cases.get(body['nonce'], {}).get('phase') != 'started'):
                _fail('extra descriptor injection has no authorized request')
            actors = [actor for actor in self._probe_actors.values() if
                      actor['task_cookie'] == body['notifying_cookie'] and
                      actor['listener_id'] == body['mutation']['listener_id']]
            invocation = self._invocations.get(body['invocation_id'])
            obj = self._objects.get(body['mutation']['program_cookie'])
            creator = None if obj is None else self._observed_transactions.get(obj['owner_transaction'])
            if (len(actors) != 1 or invocation is None or invocation['grant_id'] != body['grant_id'] or
                    obj is None or obj['state'] != 'live' or creator is None or creator['grant_id'] != body['grant_id']):
                _fail('extra descriptor injection lacks its actor, invocation and live program')
            if body['nonce'] in self._extra_injections:
                _fail('extra descriptor injection duplicated')
            self._extra_injections[body['nonce']] = body
        _observe_runtime_root(record, self._runtime_root)
        if kind == 'containerd_launcher':
            if (self._containerd_launcher is not None or body['exe_sha256'] != self._broker.expected_hash or
                    (self._pinned_containerd is not None and body['target_sha256'] != self._pinned_containerd)):
                _fail('containerd launcher binding differs')
            self._containerd_launcher = body
        elif kind == 'daemon':
            task = self._guard_tasks.get(body['task_cookie'])
            pair = self._containerd_launcher
            prior = self._launcher_daemons.get(body['task_cookie'])
            if (pair is None or task is None or task['root'] != body['task_cookie'] or
                    task['fork'] != body['watch_sequence'] or task['start'] != body['start_ns'] or
                    task['watch_key'] in ((pair['exe_dev'], pair['exe_ino']), (pair['target_dev'], pair['target_ino'])) or
                    body['helper_sha256'] != self._broker.expected_hash or
                    (self._pinned_dockerd is not None and body['dockerd_sha256'] != self._pinned_dockerd) or
                    (prior is not None and prior['cookie'] != body['cookie'])):
                _fail('daemon launcher watch binding differs')
            self._launcher_daemons[body['task_cookie']] = body
        if self._failed_checkpoint is not None:
            if (kind in ('runtime_exec', 'cgroup_enrollment', 'checkpoint') or
                    (kind == 'selftest_fd_stage' and body['phase'] != 'restored') or
                    (kind == 'held_file' and body['source'] == 'runtime_import') or
                    (kind == 'principal' and body['id'] not in self._observed_principals) or
                    (kind == 'invocation' and (body['id'] not in self._invocations or body['state'] not in ('settled', 'failed'))) or
                    (kind == 'grant' and body['phase'] != 'revoked') or
                    (kind == 'exec_admission' and body['state'] not in ('cancelled', 'settled')) or
                    (kind == 'endpoint_transition' and body['state'] != 'closed')):
                _fail('authority resumed admission after checkpoint owner death')
            if kind == 'transaction' and body['state'] == 'committed' and body['command'] == 'PROG_QUERY':
                _fail('query committed after checkpoint owner death')
            if (kind == 'transaction' and body['state'] == 'committed' and body['grant_id'] is not None and
                    not (body['command'] == 'PROG_DETACH' and body['requester_cookie'] is None and body['notification_id'] is None)):
                _fail('runtime effect committed after checkpoint owner death')
        if kind == 'transaction':
            if body['command'] == 'MAP_LOOKUP_ELEM' and body['output'] is not None:
                output = body['output']
                registry = self._registry_records.get(output['registry_sequence'])
                inputs = body['input_objects']
                obj = None if len(inputs) != 1 else self._objects.get(inputs[0]['cookie'])
                if (registry is None or obj is None or inputs[0]['kind'] != 'map' or
                        obj['class'] != 'map' or obj['attributes']['name'] != registry['map']):
                    _fail('registry lookup lacks its actual control map binding')
                value = registry['value']
                if body['result'] == -2:
                    deleted = self._deleted_registry_values.get(output['registry_sequence'])
                    if (registry['action'] != 'delete' or registry['map'] != 'retire_tasks' or value is not None or
                            deleted is None or deleted['grant_id'] != registry['grant_id'] or
                            (registry['map'], canonical(registry['key'])) in self._observed_registry):
                        _fail('absent lookup does not identify a current exact registry deletion')
                else:
                    if value is None:
                        _fail('registry lookup has no live value')
                    if registry['map'] == 'broker_token':
                        expected = struct.pack('<Q', value['cookie'])
                    elif registry['map'] == 'guard_liveness':
                        expected = struct.pack('<Q', value['phase'])
                    elif registry['map'] == 'realm_targets':
                        expected = struct.pack('<QQII', value['enrollment'], value['grant_id'], value['phase'], value['retirement'])
                    else:
                        expected = struct.pack('<QQQQQ', value['enrollment'], value['grant_id'], value['invocation_id'],
                                               value['exe_dev'], value['exe_ino'])
                    if output['value_hex'] != expected.hex():
                        _fail('actual registry lookup bytes differ')
            grant = self._observed_grants.get(body['grant_id'])
            internal_detach = (body['grant_id'] is not None and body['command'] == 'PROG_DETACH' and
                               all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
            internal_replacement = (body['grant_id'] is not None and body['command'] in ('PROG_LOAD', 'PROG_ATTACH') and
                                    all(body[key] is None for key in ('invocation_id', 'requester_cookie', 'notification_id')))
            if internal_replacement:
                candidates = [nonce for nonce, request in self._selftest_requests.items() if
                              request == ('attachment-replace', body['grant_id']) and
                              self._selftest_cases.get(nonce, {}).get('phase') in ('started', 'observed')]
                if len(candidates) != 1 or grant is None or grant['phase'] in ('revoked', 'retired'):
                    _fail('internal replacement lacks an active case and grant')
                self._selftest_internal.setdefault(candidates[0], set()).add(body['id'])
            if grant is not None and grant['phase'] == 'revoked' and body['state'] in ('prepared', 'committed') and not internal_detach:
                _fail('revoked grant admitted a new runtime effect')
            if body['grant_id'] is None and body['command'] == 'PROG_QUERY':
                brokers = [p for p in self._observed_principals.values() if p['role'] == 'BOOTSTRAP_BROKER' and
                           p['terminal'] is None and p['task_cookie'] == body['executor_cookie']]
                if len(brokers) != 1:
                    _fail('internal query executor is not the original broker')
                if body['state'] == 'prepared':
                    target = body['input_objects'][0]
                    matches = [item for item in self._observed_attachments.values() if
                               item['enrollment'] == target['cookie'] and item['state'] == 'attached' and
                               item['id'] not in self._internal_queries.values() and
                               self._observed_transactions.get(item['transaction_id'], {}).get('state') == 'effect_observed' and
                               self._observed_transactions[item['transaction_id']]['result'] == 0 and
                               self._observed_transactions[item['transaction_id']]['output'] == {'attachment_id': item['id']}]
                    if len(matches) != 1:
                        _fail('internal query lacks a unique pending attachment')
                    attached = matches[0]
                    group = self._cgroup_enrollments.get(attached['grant_id'])
                    if (group is None or not group['used'] or group['offline_seq'] is not None or
                            group['release_seq'] is not None or group['record']['enrollment'] != target['cookie']):
                        _fail('internal query lacks its retained enrolled cgroup')
                    self._internal_queries[body['id']] = attached['id']
                attached = self._observed_attachments.get(self._internal_queries.get(body['id']))
                if attached is None:
                    _fail('internal query changed its attachment binding')
                if body['state'] in ('effect_observed', 'response_settled', 'retired'):
                    program = self._objects.get(attached['program_cookie'])
                    if (program is None or program['state'] != 'live' or body['result'] != 0 or
                            body['output'] != {'attach_flags': 2, 'program_ids': [program['kernel_id']],
                                               'program_attach_flags': [2]}):
                        _fail('internal query did not confirm the actual attached program')
            if internal_detach:
                if body['state'] == 'prepared':
                    target, program = body['input_objects']
                    matches = [item for item in self._observed_attachments.values() if item['grant_id'] == body['grant_id'] and
                               item['enrollment'] == target['cookie'] and item['program_cookie'] == program['cookie'] and
                               item['state'] == 'attached']
                    if len(matches) != 1 or grant is None:
                        _fail('internal detach lacks a unique owned attachment')
                    attached = matches[0]
                    original = self._observed_transactions.get(attached['transaction_id'])
                    response = None if original is None else self._notification_responses.get(original['id'])
                    pending_cleanup = {'policy': 'detach_then_close', 'state': 'pending', 'steps': [
                        {'sequence': 1, 'action': 'detach', 'object_cookie': attached['program_cookie'],
                         'attachment_id': attached['id'], 'guard_sequence': None, 'result': None}]}
                    extra = [nonce for nonce, stage in self._extra_injections.items() if
                             stage['grant_id'] == body['grant_id'] and stage['mutation']['program_cookie'] == program['cookie']]
                    replace = [nonce for nonce, case in self._selftest_cases.items() if
                               self._selftest_requests.get(nonce) == ('attachment-replace', body['grant_id']) and
                               case['phase'] == 'observed' and program['cookie'] in case['reference_cookies'] and
                               attached['replaces'] is not None]
                    ordinary = (original is not None and original['state'] == 'response_settled' and
                                original['cleanup'] == pending_cleanup and response is not None and response['ioctl_errno'] == 2)
                    selected = extra + replace
                    selftest = len(selected) == 1 and original is not None and original['state'] == 'retired'
                    if (original is None or original['command'] != 'PROG_ATTACH' or original['grant_id'] != body['grant_id'] or
                            original['result'] != 0 or not (ordinary or selftest)):
                        _fail('internal detach lacks its preauthorized original attach')
                    if selftest:
                        self._selftest_internal.setdefault(selected[0], set()).add(body['id'])
                    self._internal_detaches[body['id']] = attached['id']
                attachment_id = self._internal_detaches.get(body['id'])
                if attachment_id is None:
                    _fail('internal detach has no original attachment binding')
                if body['state'] in ('effect_observed', 'response_settled', 'retired'):
                    actual = self._observed_attachments.get(attachment_id)
                    if (not _object(body['output'], ('attachment_id',)) or
                            (body['result'] == 0 and (body['output']['attachment_id'] != attachment_id or
                             actual is None or actual['state'] not in ('detached', 'released'))) or
                            (body['result'] != 0 and body['output']['attachment_id'] is not None)):
                        _fail('internal detach result differs from its actual attachment')
            if body['state'] in ('response_settled', 'retired') and body['invocation_id'] is not None and body['notification_id'] is not None and body['result'] is not None:
                delivery = self._notification_responses.get(body['id'])
                if delivery is None or body['returned_fd'] != delivery['returned_fd']:
                    _fail('runtime transaction lacks its actual response ioctl')
            if body['command'] == 'PROG_GET_FD_BY_ID' and body['state'] in ('response_settled', 'retired') and body['result'] is not None and body['result'] >= 0:
                output = body['output']
                delivery = self._notification_responses.get(body['id'])
                if (not _object(output, ('object_cookie',)) or delivery is None or
                        (body['returned_fd'] is None and delivery['ioctl_errno'] != 2 and
                         not (delivery['operation'] == 'SEND' and delivery['response']['error'] < 0)) or
                        body['cleanup']['policy'] != 'close_borrowed_ref' or
                        body['cleanup']['state'] != ('pending' if body['state'] == 'response_settled' else 'settled')):
                    _fail('borrowed descriptor lacks a known response and cleanup')
                if body['state'] == 'retired':
                    cookie = output['object_cookie']
                    obj = self._objects.get(cookie)
                    creator = None if obj is None else self._observed_transactions.get(obj['owner_transaction'])
                    retained = (creator is not None and creator['grant_id'] == body['grant_id'] and
                                creator['state'] == 'retired' and creator['result'] is not None and creator['result'] >= 0 and
                                creator['returned_fd'] is not None and cookie not in self._grant_cleanup)
                    attached = any(item['grant_id'] == body['grant_id'] and item['program_cookie'] == cookie and
                                   item['state'] == 'attached' for item in self._observed_attachments.values())
                    alias = any(txn['id'] != body['id'] and txn['grant_id'] == body['grant_id'] and
                                txn['command'] == 'PROG_GET_FD_BY_ID' and txn['state'] == 'response_settled' and
                                txn['cleanup']['policy'] == 'close_borrowed_ref' and txn['cleanup']['state'] == 'pending' and
                                txn['output'] is not None and txn['output'].get('object_cookie') == cookie
                                for txn in self._observed_transactions.values())
                    if (obj is None or obj['class'] != 'program' or obj['state'] != 'live' or creator is None or
                            creator['grant_id'] != body['grant_id'] or
                            body['cleanup']['steps'][0]['object_cookie'] != cookie or not (retained or attached or alias)):
                        _fail('borrowed close lacks another proven live holder')
            delivery = self._notification_responses.get(body['id'])
            if (body['state'] == 'retired' and delivery is not None and body['output'] is not None and
                    (delivery['ioctl_errno'] == 2 or delivery['operation'] == 'SEND' and delivery['response']['error'] < 0)):
                output = body['output']
                if body['command'] in ('MAP_CREATE', 'PROG_LOAD') and output.get('object_cookie') is not None:
                    obj = self._objects.get(output['object_cookie'])
                    if (obj is None or obj['owner_transaction'] != body['id'] or obj['removal_seq'] is None or
                            obj['cookie'] not in self._removals or body['cleanup']['state'] != 'settled'):
                        _fail('undelivered creation lacks actual object cleanup')
                if body['command'] == 'PROG_ATTACH' and output.get('attachment_id') is not None:
                    attachment = self._observed_attachments.get(output['attachment_id'])
                    expected_cleanup = {'policy': 'detach_then_close', 'state': 'settled', 'steps': [
                        {'sequence': 1, 'action': 'detach', 'object_cookie': attachment['program_cookie'],
                         'attachment_id': attachment['id'], 'guard_sequence': None, 'result': 0}]} if attachment is not None else None
                    if (attachment is None or attachment['grant_id'] != body['grant_id'] or
                            attachment['state'] not in ('detached', 'released') or body['cleanup'] != expected_cleanup or
                            not any(txn['command'] == 'PROG_DETACH' and txn['grant_id'] == body['grant_id'] and
                                    txn['state'] == 'retired' and txn['result'] == 0 and
                                    self._internal_detaches.get(txn['id']) == attachment['id'] and
                                    txn['output'] == {'attachment_id': attachment['id']}
                                    for txn in self._observed_transactions.values())):
                        _fail('undelivered attach lacks its actual detach')
            self._observed_transactions[body['id']] = body
        if kind == 'attachment':
            self._observed_attachments[body['id']] = body
        elif kind == 'registry':
            key = (body['map'], canonical(body['key']))
            previous = self._observed_registry.get(key)
            grant = self._observed_grants.get(body['grant_id'])
            control = self._observed_transactions.get(body['transaction_id'])
            if (control is None or control['state'] != 'committed' or control['grant_id'] is not None or
                    control['command'] not in ('MAP_UPDATE_ELEM', 'MAP_DELETE_ELEM') or
                    (body['map'] != 'guard_liveness' and (body['action'] == 'insert') != (previous is None)) or
                    (body['map'] == 'guard_liveness' and (body['action'] != 'update' or previous is not None))):
                _fail('registry effect lacks its committed control transaction or prior state')
            if body['map'] not in ('broker_token', 'guard_liveness'):
                group = self._cgroup_enrollments.get(body['grant_id'])
                if (grant is None or group is None or body['key']['root_kn_id'] != group['record']['root_cookie'] or
                        body['key']['kn_id'] != group['record']['cgroup_id'] or
                        (body['value'] is not None and body['value']['enrollment'] != group['record']['enrollment'])):
                    _fail('registry effect differs from retained cgroup enrollment')
            if grant is not None and grant['phase'] == 'revoked':
                if (body['parent_transaction_id'] is not None or previous is None or previous['grant_id'] != body['grant_id'] or
                        not ((body['map'] == 'retire_tasks' and body['action'] == 'delete') or
                             (body['map'] == 'realm_targets' and body['action'] == 'update' and
                              body['value']['phase'] in (3, 4) and body['value']['phase'] >= previous['phase'] and
                              all(body['value'][field] == previous[field] for field in ('enrollment', 'grant_id', 'retirement'))))):
                    _fail('revoked grant acquired a registry permission')
            if body['action'] == 'delete':
                if previous['grant_id'] != body['grant_id'] or body['map'] != 'retire_tasks':
                    _fail('registry deletion changed its grant or map')
                self._deleted_registry_values[record['sequence']] = previous
                self._observed_registry.pop(key, None)
            else:
                self._observed_registry[key] = body['value']
            self._registry_records[record['sequence']] = body
        if kind == 'principal':
            self._observed_principals[body['id']] = body
        elif kind == 'grant':
            owner = self._observed_principals.get(body['owner'])
            old = self._observed_grants.get(body['id'])
            if (owner is None or not ((owner['terminal'] is None and body['phase'] != 'revoked') or
                    (owner['terminal'] == 'exit' and body['phase'] == 'revoked' and old is not None and
                     old['phase'] in ('admitted', 'active', 'settling') and
                     all(old[key] == body[key] for key in old if key != 'phase')))):
                _fail('grant lacks a live owner or actual owner-exit revocation')
            group = self._cgroup_enrollments.get(body['id'])
            if body['phase'] == 'retired' and group is not None and (not group['used'] or group['release_seq'] is None):
                _fail('grant retired before enrolled cgroup release')
            if body['phase'] == 'retired':
                for obj in self._objects.values():
                    creator = self._observed_transactions.get(obj['owner_transaction'])
                    if creator is None or creator['grant_id'] != body['id']:
                        continue
                    if obj['removal_seq'] is None or obj['cookie'] not in self._removals:
                        _fail('grant retired with a live object incarnation')
                    if (creator['result'] is not None and creator['result'] >= 0 and creator['returned_fd'] is not None and
                            self._grant_cleanup.get(obj['cookie'], {}).get('step') != 2):
                        _fail('grant retired before retained object cleanup')
            self._observed_grants[body['id']] = body
        elif kind == 'invocation':
            previous = self._invocations.get(body['id'])
            transitions = {'bound': ('bound', 'running', 'failed'), 'running': ('running', 'settled', 'failed'),
                           'settled': ('settled',), 'failed': ('failed',)}
            if ((previous is None and body['state'] != 'bound') or
                    (previous is not None and (any(previous[key] != body[key] for key in previous if key != 'state') or
                     body['state'] not in transitions[previous['state']]))):
                _fail('runtime invocation changed identity or moved backwards')
            principal = self._observed_principals.get(body['principal_id'])
            task = self._guard_tasks.get(body['task_cookie'])
            if (principal is None or task is None or principal['role'] != 'RUNTIME' or
                    principal['task_cookie'] != body['task_cookie'] or principal['start_ns'] != task['start'] or
                    principal['tgid'] != task['tgid'] or principal['tid'] != task['tid']):
                _fail('runtime invocation differs from observed task incarnation')
            pair = self._containerd_launcher
            nodes = body['ancestry']
            root_node, wrapper = nodes[0], nodes[-1]
            watched = self._guard_tasks.get(root_node['task_cookie'])
            daemon = self._launcher_daemons.get(root_node['task_cookie'])
            if (pair is None or watched is None or daemon is None or
                    watched['root'] != root_node['task_cookie'] or task['root'] != root_node['task_cookie'] or
                    wrapper['task_cookie'] != body['task_cookie'] or body['daemon_cookie'] != daemon['cookie'] or
                    root_node['exe_sha256'] != daemon['dockerd_sha256'] or
                    watched['versions'].get(root_node['exec_generation']) != watched.get('dockerd_key')):
                _fail('runtime invocation lacks watched dockerd binding')
            launcher = (pair['exe_dev'], pair['exe_ino'])
            target = (pair['target_dev'], pair['target_ino'])
            for index, node in enumerate(nodes):
                observed = self._guard_tasks.get(node['task_cookie'])
                generation = node['exec_generation']
                if (observed is None or observed['root'] != root_node['task_cookie'] or
                        node['parent_cookie'] != observed['parent'] or node['fork_sequence'] != observed['fork'] or
                        node['exec_sequence'] != observed['version_exec'].get(generation) or
                        (index > 0 and node['parent_cookie'] != nodes[index - 1]['task_cookie']) or
                        observed['versions'].get(generation) is None):
                    _fail('runtime invocation ancestry differs from observed lineage')
                key = observed['versions'][generation]
                if (key == launcher or (node['exe_sha256'] == pair['target_sha256'] and
                        (key != target or not observed['managed_versions'].get(generation, False)))):
                    _fail('runtime invocation containerd launcher ancestry differs')
            if (body['listener_id'] is not None and
                    self._listeners.setdefault(body['listener_id'], body['id']) != body['id']):
                _fail('listener identity was reused by another invocation')
            if body['state'] in ('running', 'settled') and body['id'] not in self._runtime_execs:
                _fail('runtime invocation lacks its held executable attestation')
            if body['command'] == 'create' and body['state'] == 'settled':
                for attachment in self._observed_attachments.values():
                    original = self._observed_transactions.get(attachment['transaction_id'])
                    if original is None or original['invocation_id'] != body['id']:
                        continue
                    matches = [ident for ident, aid in self._internal_queries.items() if aid == attachment['id']]
                    query = None if len(matches) != 1 else self._observed_transactions.get(matches[0])
                    audit = None if len(matches) != 1 else self._query_audits.get(matches[0])
                    if (query is None or query['state'] != 'retired' or audit is None or
                            audit['command'] != 'PROG_QUERY' or audit['result'] != query['result'] or
                            audit['task_cookie'] != query['executor_cookie']):
                        _fail('attachment lacks its independently audited post-attach query')
            self._invocations[body['id']] = body
        elif kind == 'runtime_exec':
            invocation = self._invocations.get(body['invocation_id'])
            guard = self._guard_execs.get(body['guard_sequence'])
            principal = None if invocation is None else self._observed_principals.get(invocation['principal_id'])
            if (invocation is None or invocation['state'] not in ('bound', 'running') or principal is None or
                    body['invocation_id'] in self._runtime_execs or body['task_cookie'] != invocation['task_cookie'] or
                    guard is None or guard['kind'] != 10 or
                    guard['task_cookie'] != body['task_cookie'] or guard['task_start_ns'] != principal['start_ns'] or
                    self._guard_tasks.get(body['task_cookie'], {}).get('exec') != body['guard_sequence'] or
                    guard['detail']['exec_generation'] != body['exec_generation'] or
                    body['exec_generation'] != invocation['ancestry'][-1]['exec_generation'] + 1 or
                    (self._pinned_runc is not None and body['exe_sha256'] != self._pinned_runc)):
                _fail('runtime executable attestation differs from native exec')
            self._runtime_execs[body['invocation_id']] = body
        elif kind == 'exec_admission':
            self._exec_admissions[body['exec_id']] = body
        elif kind == 'endpoint_transition':
            self._endpoint_transitions[body['transition_id']] = body
        elif kind == 'held_file':
            if body['cookie'] != record['sequence'] or body['cookie'] in self._held_files:
                _fail('held file is not its unique transcript identity')
            if body['source'] == 'bootstrap':
                if self._baseline_file is not None:
                    _fail('held device baseline duplicated')
                self._baseline_file = body
            else:
                baseline = self._baseline_file
                invocation = self._invocations.get(body['invocation_id'])
                if (baseline is None or body['baseline_cookie'] != baseline['cookie'] or
                        any(body[key] != baseline[key] for key in ('device', 'inode', 'rdev', 'mode', 'uid', 'gid')) or
                        invocation is None or invocation['state'] not in ('bound', 'running') or
                        invocation['listener_id'] is None or
                        (body['invocation_id'], body['notification_id']) in self._notifications):
                    _fail('imported device lacks its original request and baseline node')
                grant = self._observed_grants.get(invocation['grant_id'])
                if grant is None or grant['phase'] == 'revoked':
                    _fail('revoked grant imported a descriptor')
            self._held_files[body['cookie']] = {'record': body, 'used': False}
        elif kind == 'cgroup_enrollment':
            grant = self._observed_grants.get(body['grant_id'])
            invocation = self._invocations.get(body['invocation_id'])
            identity = (body['root_cookie'], body['cgroup_id'])
            if (grant is None or grant['phase'] not in ('admitted', 'active') or invocation is None or
                    invocation['grant_id'] != body['grant_id'] or invocation['state'] not in ('bound', 'running') or
                    invocation['listener_id'] is None or body['grant_id'] in self._cgroup_enrollments or
                    body['enrollment'] in self._cgroup_enrollment_ids or identity in self._cgroup_identities or
                    (self._cgroup_root is not None and self._cgroup_root != body['root_cookie'])):
                _fail('cgroup enrollment lacks a unique live grant and invocation')
            self._cgroup_root = body['root_cookie']
            self._cgroup_enrollment_ids.add(body['enrollment'])
            self._cgroup_identities[identity] = body['grant_id']
            self._cgroup_enrollments[body['grant_id']] = {'record': body, 'used': False, 'offline_seq': None, 'release_seq': None}
        elif kind == 'notification_response':
            txn = self._observed_transactions.get(body['transaction_id'])
            if (txn is None or txn['state'] != 'effect_observed' or body['transaction_id'] in self._notification_responses or
                    any(txn[key] != body[key] for key in ('invocation_id', 'requester_cookie', 'notification_id')) or
                    body['guard_after'] > self._guard_sequence):
                _fail('notification response lacks its original observed effect')
            needs_fd = txn['command'] in ('MAP_CREATE', 'PROG_LOAD', 'PROG_GET_FD_BY_ID') and txn['result'] >= 0
            if not ((not needs_fd and body['operation'] == 'SEND') or
                    (needs_fd and (body['operation'] == 'ADDFD_SEND' or
                                   body['operation'] == 'SEND' and body['response']['error'] == -1))):
                _fail('notification response operation differs from the observed effect')
            self._notification_responses[body['transaction_id']] = body
        elif kind == 'notification_denial':
            invocation = self._invocations.get(body['invocation_id'])
            identity = (body['invocation_id'], body['notification_id'])
            task = self._guard_tasks.get(body['requester_cookie'])
            if (invocation is None or invocation['listener_id'] is None or identity in self._notifications or
                    body['guard_after'] > self._guard_sequence or task is None or
                    task['fork'] > body['guard_before'] or
                    (task['exit'] is not None and task['exit'] <= body['guard_before'])):
                _fail('notification denial lacks its actual notifying task and listener')
            cookie, child = body['requester_cookie'], None
            while cookie != invocation['task_cookie']:
                node = self._guard_tasks.get(cookie)
                if node is None or node['parent'] not in self._guard_tasks:
                    _fail('notification denial requester is outside listener lineage')
                child, cookie = node, node['parent']
            if child is not None:
                execution = self._runtime_execs.get(invocation['id'])
                if execution is None or child['fork'] <= execution['guard_sequence']:
                    _fail('notification denial descendant did not inherit the runtime listener')
            self._notifications.add(identity)
        elif kind == 'notification_cancel':
            identity = (body['invocation_id'], body['notification_id'])
            prepared = self._prepared_notifications.get(identity)
            if (prepared is None or prepared['state'] != 'prepared' or identity in self._cancelled_notifications or
                    prepared['requester_cookie'] != body['requester_cookie'] or
                    _COMMAND_IDS.get(prepared['command']) != body['command'] or
                    body['guard_after'] > self._guard_sequence):
                _fail('notification cancellation lacks its original prepared request')
            self._cancelled_notifications.add(identity)
        elif kind == 'transaction' and body['state'] == 'prepared' and body['invocation_id'] is not None:
            identity = (body['invocation_id'], body['notification_id'])
            if identity in self._notifications:
                _fail('notification was decided twice')
            for item in body['input_objects']:
                if item['kind'] in ('program', 'map') and item['cookie'] in self._grant_cleanup:
                    _fail('transaction reused a closed probe incarnation')
                if item['kind'] == 'dev_null':
                    held = self._held_files.get(item['cookie'])
                    if (held is None or held['used'] or held['record']['source'] != 'runtime_import' or
                            held['record']['requester_fd'] != item['requester_fd'] or
                            any(held['record'][key] != body[key] for key in
                                ('invocation_id', 'requester_cookie', 'notification_id'))):
                        _fail('input is not the actual imported requester device')
                    held['used'] = True
                if item['kind'] == 'cgroup':
                    group = self._cgroup_enrollments.get(body['grant_id'])
                    if (group is None or group['offline_seq'] is not None or group['release_seq'] is not None or
                            group['record']['enrollment'] != item['cookie']):
                        _fail('cgroup input lacks prior live enrollment')
                    if not group['used']:
                        if any(group['record'][key] != body[key] for key in
                               ('grant_id', 'invocation_id', 'requester_cookie', 'notification_id')):
                            _fail('first cgroup input differs from its enrollment request')
                        group['used'] = True
            self._notifications.add(identity)
            self._prepared_notifications[identity] = body
        elif kind == 'transaction' and body['state'] == 'committed' and body['invocation_id'] is not None:
            identity = (body['invocation_id'], body['notification_id'])
            if identity in self._cancelled_notifications or identity not in self._prepared_notifications:
                _fail('cancelled or unknown notification committed')
            self._prepared_notifications[identity] = body
        elif kind == 'transaction' and body['state'] == 'retired' and body['result'] is None:
            identity = (body['invocation_id'], body['notification_id'])
            prepared = self._prepared_notifications.get(identity)
            if (identity not in self._cancelled_notifications or prepared is None or prepared['state'] != 'prepared' or
                    any(prepared[k] != body[k] for k in prepared if k != 'state') or
                    body['cleanup'] != {'policy': 'none', 'state': 'not_needed', 'steps': []}):
                _fail('unexecuted notification retirement differs')
        elif kind == 'registry' and body['parent_transaction_id'] is not None:
            parent = self._observed_transactions.get(body['parent_transaction_id'])
            group = self._cgroup_enrollments.get(body['grant_id'])
            grant = self._observed_grants.get(body['grant_id'])
            if (parent is None or parent['state'] != 'committed' or parent['grant_id'] != body['grant_id'] or
                    group is None or body['map'] not in ('realm_targets', 'retire_tasks') or
                    body['key']['root_kn_id'] != group['record']['root_cookie'] or
                    body['key']['kn_id'] != group['record']['cgroup_id'] or
                    not (parent['command'] == 'PROG_ATTACH' or parent['command'] == 'PROG_DETACH' and
                         grant is not None and grant['retirement'] == 'explicit_cleanup') or
                    not any(item['kind'] == 'cgroup' and item['cookie'] == group['record']['enrollment']
                            for item in parent['input_objects'])):
                _fail('nested registry effect lacks its committed parent')
        elif kind == 'grant_object_cleanup':
            obj = self._objects.get(body['object_cookie'])
            creator = self._observed_transactions.get(body['creator_transaction_id'])
            grant = self._observed_grants.get(body['grant_id'])
            prior = self._grant_cleanup.get(body['object_cookie'])
            extra = any(stage['grant_id'] == body['grant_id'] and stage['mutation']['program_cookie'] == body['object_cookie']
                        for stage in self._extra_injections.values())
            replacement = any(self._selftest_requests.get(nonce) == ('attachment-replace', body['grant_id']) and
                              case['phase'] in ('observed', 'settled') and body['object_cookie'] in case['reference_cookies'] and
                              body['creator_transaction_id'] in case['transaction_ids']
                              for nonce, case in self._selftest_cases.items())
            if (grant is None or not (grant['phase'] in ('admitted', 'settling', 'revoked') or
                    grant['phase'] == 'active' and (replacement or extra)) or obj is None or creator is None or
                    obj['owner_transaction'] != creator['id'] or creator['grant_id'] != body['grant_id'] or
                    creator['state'] != 'retired' or creator['result'] is None or creator['result'] < 0 or
                    (creator['returned_fd'] is None and not (replacement and creator['invocation_id'] is None)) or creator['command'] not in ('MAP_CREATE', 'PROG_LOAD') or
                    creator['output'] is None or creator['output'].get('object_cookie') != body['object_cookie']):
                _fail('grant cleanup lacks its original successful creator')
            if body['step'] == 1:
                if prior is not None or body['result'] != 0 or obj['removal_seq'] is not None:
                    _fail('retained reference was not closed exactly once')
                if grant['phase'] == 'admitted':
                    related = [txn for txn in self._observed_transactions.values() if txn['grant_id'] == body['grant_id'] and
                               txn['invocation_id'] == creator['invocation_id'] and txn['state'] == 'retired']
                    def answered(txn, result):
                        reply = self._notification_responses.get(txn['id'])
                        return (reply is not None and reply['operation'] == 'SEND' and reply['ioctl_errno'] == 0 and
                                reply['response']['error'] == result)
                    feature = (creator['command'] == 'MAP_CREATE' and obj['attributes']['name'] == 'feature_test' and
                               any(txn['command'] == 'OBJ_GET_INFO_BY_FD' and txn['result'] == 0 and answered(txn, 0) and
                                   len(txn['input_objects']) == 1 and txn['input_objects'][0]['cookie'] == obj['cookie']
                                   for txn in related))
                    replace = (creator['command'] == 'PROG_LOAD' and creator['program_bytes'] == 'b7000000000000009500000000000000' and
                               any(txn['command'] == 'PROG_ATTACH' and txn['result'] == -9 and answered(txn, -9) and
                                   txn['input_objects'] and txn['input_objects'][0]['kind'] == 'dev_null' and
                                   any(item['kind'] == 'program' and item['cookie'] == obj['cookie'] for item in txn['input_objects'])
                                   for txn in related))
                    if not (feature or replace or replacement or extra):
                        _fail('grant closed a non-probe creator before settlement')
            else:
                removal = self._removals.get(body['object_cookie'])
                if (prior is None or prior['step'] != 1 or
                        any(prior[key] != body[key] for key in ('grant_id', 'creator_transaction_id', 'object_cookie')) or
                        removal is None or obj['removal_seq'] != body['guard_sequence'] or
                        removal['sequence'] != body['guard_sequence'] or removal['result'] != body['result']):
                    _fail('grant cleanup lacks the actual matching ID removal')
            self._grant_cleanup[body['object_cookie']] = body
        elif kind == 'checkpoint' and body['state'] == 'failed':
            owner = self._observed_principals.get(body['principal_id'])
            if (owner is None or owner['terminal'] != 'exit' or
                    owner['role'] not in ('BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_OBSERVER') or
                    body['end_watermark'] != self._guard_sequence or body['begin_watermark'] > body['end_watermark']):
                _fail('failed checkpoint lacks its exited owner and observed cursor')
            self._failed_checkpoint = body
        elif kind == 'audit' and body['record_type'] == 'SYSCALL' and body['transaction_id'] in self._internal_queries:
            query = self._observed_transactions.get(body['transaction_id'])
            if (body['transaction_id'] in self._query_audits or query is None or query['state'] != 'retired' or
                    body['context'] != 'syscall' or body['command'] != 'PROG_QUERY' or
                    body['result'] != query['result'] or body['task_cookie'] != query['executor_cookie']):
                _fail('internal query syscall audit differs from the actual broker transaction')
            self._query_audits[body['transaction_id']] = body
        elif kind == 'guard':
            guard = body['record']
            if guard['sequence'] != self._guard_sequence + 1:
                _fail('guard sequence is not contiguous')
            self._guard_sequence = guard['sequence']
            if guard['kind'] in (9, 10, 11, 12):
                cookie, detail = guard['task_cookie'], guard['detail']
                key = (detail['exe_dev'], detail['exe_ino'])
                task = self._guard_tasks.get(cookie)
                if (cookie <= 0 or guard['task_start_ns'] <= 0 or detail['tgid'] <= 0 or detail['tid'] <= 0 or
                        key[1] <= 0 or detail['exec_generation'] <= 0):
                    _fail('guard ancestry task identity invalid')
                if guard['kind'] == 12:
                    pair = self._containerd_launcher
                    if (pair is None or task is not None or detail['exec_generation'] != 1 or
                            key == (pair['exe_dev'], pair['exe_ino'])):
                        _fail('guard daemon watch lacks launcher binding')
                    self._guard_tasks[cookie] = {'parent': guard['parent_cookie'], 'start': guard['task_start_ns'],
                        'tgid': detail['tgid'], 'tid': detail['tid'], 'generation': 1, 'key': key,
                        'fork': guard['sequence'], 'exec': guard['sequence'], 'exit': None,
                        'root': cookie, 'watch_key': key, 'managed': False, 'versions': {1: key},
                        'version_exec': {1: guard['sequence']}, 'managed_versions': {1: False}}
                elif guard['kind'] == 9:
                    parent = self._guard_tasks.get(guard['parent_cookie'])
                    if (task is not None or parent is None or parent['exit'] is not None or
                            detail['exec_generation'] != parent['generation'] or key != parent['key']):
                        _fail('guard fork does not inherit its observed parent')
                    managed = parent['managed'] and key == (self._containerd_launcher['target_dev'], self._containerd_launcher['target_ino'])
                    self._guard_tasks[cookie] = {'parent': guard['parent_cookie'], 'start': guard['task_start_ns'],
                        'tgid': detail['tgid'], 'tid': detail['tid'], 'generation': parent['generation'], 'key': key,
                        'fork': guard['sequence'], 'exec': parent['exec'], 'exit': None,
                        'root': parent['root'], 'managed': managed, 'versions': {parent['generation']: key},
                        'version_exec': {parent['generation']: parent['exec']},
                        'managed_versions': {parent['generation']: managed}}
                else:
                    if (task is None or task['exit'] is not None or task['parent'] != guard['parent_cookie'] or
                            task['start'] != guard['task_start_ns'] or task['tgid'] != detail['tgid'] or
                            task['tid'] != detail['tid']):
                        _fail('guard task incarnation changed')
                    if guard['kind'] == 10:
                        if detail['exec_generation'] != task['generation'] + 1:
                            _fail('guard exec generation differs')
                        pair = self._containerd_launcher
                        launcher = (pair['exe_dev'], pair['exe_ino'])
                        target = (pair['target_dev'], pair['target_ino'])
                        root = self._guard_tasks[task['root']]
                        daemon = self._launcher_daemons.get(task['root'])
                        if task['key'] == launcher and key != target:
                            _fail('containerd launcher executed an unexpected target')
                        if cookie == task['root'] and task['generation'] == 1:
                            if daemon is None or key in (launcher, target):
                                _fail('daemon dockerd transition lacks launcher binding')
                            root['dockerd_key'] = key
                        if key == launcher:
                            parent = self._guard_tasks.get(task['parent'])
                            if (daemon is None or cookie == task['root'] or task['tgid'] != task['tid'] or
                                    task['tgid'] == root['tgid'] or task['key'] != root.get('dockerd_key') or
                                    task['exec'] > task['fork'] or parent is None or parent['exit'] is not None or
                                    parent['tgid'] != root['tgid'] or parent['key'] != root.get('dockerd_key')):
                                _fail('containerd launcher transition differs')
                        if key == target:
                            if not ((task['key'] == launcher and task['tgid'] == task['tid'] and
                                     task['exec'] > task['fork']) or (task['key'] == target and task['managed'])):
                                _fail('containerd launcher target transition differs')
                            task['managed'] = True
                        else:
                            task['managed'] = False
                        task.update(generation=detail['exec_generation'], key=key, exec=guard['sequence'])
                        task['versions'][detail['exec_generation']] = key
                        task['version_exec'][detail['exec_generation']] = guard['sequence']
                        task['managed_versions'][detail['exec_generation']] = task['managed']
                    else:
                        if detail['exec_generation'] != task['generation'] or key != task['key']:
                            _fail('guard task exit identity differs')
                        task['exit'] = guard['sequence']
            if guard['kind'] == 10:
                if any(item['task_cookie'] == guard['task_cookie'] for item in self._runtime_execs.values()):
                    _fail('runtime executed an unapproved later image')
                self._guard_execs[guard['sequence']] = guard
            elif guard['kind'] == 11:
                self._exited_tasks.add(guard['task_cookie'])
            if guard['kind'] in (5, 6):
                grant_id = self._cgroup_identities.get((guard['root_cookie'], guard['cgroup_id']))
                if grant_id is not None:
                    group = self._cgroup_enrollments[grant_id]
                    key = 'offline_seq' if guard['kind'] == 5 else 'release_seq'
                    if (group[key] is not None or (guard['kind'] == 5 and group['release_seq'] is not None) or
                            (guard['kind'] == 6 and group['offline_seq'] is None)):
                        _fail('cgroup lifecycle event duplicated or reordered')
                    group[key] = guard['sequence']
            cookie = guard['object_cookie']
            if guard['kind'] in (1, 3):
                identity = (guard['kind'], guard['kernel_id'])
                if cookie == 0 or guard['kernel_id'] == 0 or cookie in self._publications or identity in self._live_object_ids:
                    _fail('duplicate or invalid publication incarnation')
                self._publications[cookie] = guard
                self._live_object_ids[identity] = cookie
            elif guard['kind'] in (2, 4):
                publication = self._publications.get(cookie)
                if (publication is None or publication['kind'] != guard['kind'] - 1 or
                        publication['kernel_id'] != guard['kernel_id'] or cookie in self._removals or
                        self._live_object_ids.get((publication['kind'], guard['kernel_id'])) != cookie):
                    _fail('removal does not match publication incarnation')
                self._removals[cookie] = guard
                del self._live_object_ids[(publication['kind'], guard['kernel_id'])]
        elif kind == 'object':
            cookie = body['cookie']
            publication = self._publications.get(cookie)
            if (publication is None or publication['kind'] != (1 if body['class'] == 'program' else 3) or
                    publication['kernel_id'] != body['kernel_id'] or publication['sequence'] != body['publication_seq'] or
                    publication['object_snapshot'] != {'version': 1, 'type': body['type'], 'attributes': body['attributes']}):
                _fail('object differs from native publication snapshot')
            old = self._objects.get(cookie)
            if old is not None and any(old[key] != body[key] for key in old if key not in ('state', 'removal_seq')):
                _fail('object incarnation facts changed')
            if old is not None:
                transitions = {'live': ('live', 'id_removed', 'reclaim_pending'),
                               'id_removed': ('id_removed', 'reclaim_pending'), 'reclaim_pending': ('reclaim_pending',)}
                if (body['state'] not in transitions[old['state']] or
                        (old['removal_seq'] is not None and old['removal_seq'] != body['removal_seq'])):
                    _fail('object incarnation moved backwards')
            removal = self._removals.get(cookie)
            if body['removal_seq'] is not None and (removal is None or body['removal_seq'] != removal['sequence']):
                _fail('object removal differs from guard')
            if cookie in self._object_info and self._object_info[cookie] != (body['class'], body['type'], body['kernel_id'], body['attributes']):
                _fail('object differs from actual FD information')
            self._objects[cookie] = body
        elif kind == 'transaction' and body['command'] == 'OBJ_GET_INFO_BY_FD' and body['output'] is not None:
            output = body['output']
            if 'object_cookie' in output and output['object_cookie'] is not None:
                cookie = output['object_cookie']
                if (not _integer(body['result']) or body['state'] not in ('effect_observed', 'response_settled', 'retired') or
                        len(body['input_objects']) != 1 or body['input_objects'][0]['cookie'] != cookie or
                        body['input_objects'][0]['kind'] not in ('program', 'map')):
                    _fail('object information lacks its real held-FD transaction')
                info = (body['input_objects'][0]['kind'], output['type'], output['kernel_id'], output['attributes'])
                obj = self._objects.get(cookie)
                if ((cookie in self._object_info and self._object_info[cookie] != info) or
                        (obj is not None and info != (obj['class'], obj['type'], obj['kernel_id'], obj['attributes']))):
                    _fail('actual FD information changed for incarnation')
                self._object_info[cookie] = info

    @_synchronized
    def read_evidence(self, *, after=None, max_records=64, deadline=None):
        """Read a contiguous prefix, validating and advancing the local hash chain."""
        if after is None:
            after = self._evidence_sequence
        if after != self._evidence_sequence or not _integer(after) or not _integer(max_records, minimum=1, maximum=64):
            _fail('evidence cursor invalid')
        try:
            body = self._body('READ_EVIDENCE', {'after': after, 'max_records': max_records},
                              ('records', 'next', 'complete'), deadline=deadline)['body']
            if (type(body['records']) is not list or len(body['records']) > max_records or
                    type(body['complete']) is not bool or not _integer(body['next'])):
                _fail('evidence response invalid')
            sequence, tail = after, self._evidence_tail
            hashes, encoded_bytes = [], 0
            for record in body['records']:
                sequence += 1
                tail = _record(record, self.boot_id, self.epoch, sequence, tail)
                self._observe_snapshot(record)
                hashes.append(tail)
                encoded_bytes += len(_encode(record)) + 1
            if (body['next'] != sequence or (not body['records'] and not body['complete']) or
                    self._evidence_bytes + encoded_bytes > 16777216):
                _fail('evidence cursor or total budget invalid')
            self._evidence_sequence, self._evidence_tail = sequence, tail
            self._evidence_hashes.extend(hashes)
            self._evidence_records.extend(json.loads(canonical(body['records'])))
            self._evidence_bytes += encoded_bytes
            return body
        except BaseException:
            self.close()
            raise

    def _verify_watermark(self, watermark, tail, deadline):
        while self._evidence_sequence < watermark:
            page = self.read_evidence(deadline=deadline)
            if page['complete'] and self._evidence_sequence < watermark:
                _fail('checkpoint transcript incomplete')
        if self._evidence_hashes[watermark] != tail:
            _fail('checkpoint transcript tail differs')

    @_synchronized
    def evidence_records(self):
        """Return all validated records, including checkpoint-internal reads."""
        self._check()
        return json.loads(canonical(self._evidence_records))

    @contextlib.contextmanager
    def observation(self, deadline):
        """Nest fixed root observations under the same authenticated nonce."""
        with self._lock:
            nonce = self._checkpoint_nonce if self._depth else os.urandom(16).hex()
            with self.checkpoint(nonce, deadline):
                yield self

    def _complete_evidence(self, deadline):
        while True:
            page = self.read_evidence(deadline=deadline)
            if page['complete']:
                return self.evidence_records()

    def broker_identity(self, deadline):
        """Return the unique live bootstrap principal bound to this peer."""
        records = self._complete_evidence(deadline)
        candidates = {}
        for record in records:
            if record['kind'] == 'principal' and record['body']['role'] == 'BOOTSTRAP_BROKER':
                candidates[record['body']['id']] = record['body']
        if len(candidates) != 1:
            _fail('unique bootstrap broker principal unavailable')
        principal = next(iter(candidates.values()))
        status = self.read_status(deadline=deadline)
        if (principal['terminal'] is not None or principal['tgid'] != self._broker.pid or
                principal['tid'] != self._broker.pid or principal['exe_sha256'] != self._broker.expected_hash or
                principal['uid'] != 0 or principal['gid'] != 0 or status['state'] != 'sealed' or
                status['sticky'] != 0 or status['lost'] != 0 or status['probe_missed'] != 0):
            _fail('bootstrap broker identity or seal differs')
        self._check()
        return {'pid': self._broker.pid, 'starttime': self._broker.start,
                'exe_dev': self._broker.exe[0], 'exe_ino': self._broker.exe[1],
                'executable_sha256': self._broker.expected_hash, 'boot_id': self.boot_id,
                'epoch': self.epoch, 'task_cookie': principal['task_cookie'], 'principal_id': principal['id']}

    def reader_identity(self, deadline):
        """Bind the sole bootstrap reader to its live executable and seal."""
        records = self._complete_evidence(deadline)
        candidates, brokers = {}, {}
        for record in records:
            if record['kind'] != 'principal':
                continue
            principal = record['body']
            if principal['role'] == 'BOOTSTRAP_AUDIT_READER':
                candidates[principal['id']] = principal
            elif principal['role'] == 'BOOTSTRAP_BROKER':
                brokers[principal['id']] = principal
        if len(candidates) != 1 or len(brokers) != 1:
            _fail('unique bootstrap reader and broker unavailable')
        principal = next(iter(candidates.values()))
        if (principal['terminal'] is not None or principal['registration'] != 'bootstrap' or
                principal['tgid'] != principal['tid'] or principal['uid'] != 0 or principal['gid'] != 0 or
                principal['parent_cookie'] is None or principal['parent_cookie'] != next(iter(brokers.values()))['parent_cookie'] or
                principal['exe_sha256'] != self._broker.expected_hash):
            _fail('bootstrap audit reader principal differs')
        reader = _Process(principal['tgid'], principal['exe_sha256'])
        try:
            status = self.read_status(deadline=deadline)
            if status['state'] != 'sealed' or status['sticky'] or status['lost'] or status['probe_missed']:
                _fail('bootstrap audit reader guard unavailable')
            reader.check()
            return {'pid': reader.pid, 'starttime': reader.start,
                    'exe_dev': reader.exe[0], 'exe_ino': reader.exe[1],
                    'executable_sha256': reader.expected_hash, 'boot_id': self.boot_id,
                    'epoch': self.epoch, 'task_cookie': principal['task_cookie'], 'principal_id': principal['id']}
        finally:
            reader.close()

    def observer_identity(self, deadline):
        """Bind the sole bootstrap terminal observer to its live executable and seal."""
        records = self._complete_evidence(deadline)
        candidates, brokers = {}, {}
        for record in records:
            if record['kind'] != 'principal':
                continue
            principal = record['body']
            if principal['role'] == 'BOOTSTRAP_TERMINAL_OBSERVER':
                candidates[principal['id']] = principal
            elif principal['role'] == 'BOOTSTRAP_BROKER':
                brokers[principal['id']] = principal
        if len(candidates) != 1 or len(brokers) != 1:
            _fail('unique bootstrap observer and broker unavailable')
        principal = next(iter(candidates.values()))
        if (principal['terminal'] is not None or principal['registration'] != 'bootstrap' or
                principal['tgid'] != principal['tid'] or principal['uid'] != 0 or principal['gid'] != 0 or
                principal['parent_cookie'] is None or principal['parent_cookie'] != next(iter(brokers.values()))['parent_cookie'] or
                principal['exe_sha256'] != self._broker.expected_hash):
            _fail('bootstrap terminal observer principal differs')
        observer = _Process(principal['tgid'], principal['exe_sha256'])
        try:
            observer.check()
            return {'pid': observer.pid, 'starttime': observer.start,
                    'exe_dev': observer.exe[0], 'exe_ino': observer.exe[1],
                    'executable_sha256': observer.expected_hash, 'boot_id': self.boot_id,
                    'epoch': self.epoch, 'task_cookie': principal['task_cookie'], 'principal_id': principal['id']}
        finally:
            observer.close()

    def bootstrap_evidence(self, case, deadline):
        """Bind immutable build facts to actual sealed bootstrap objects."""
        metadata, inputs = case['bpf_bootstrap'], case['boot_inputs']
        if (hashlib.sha256(canonical(metadata)).hexdigest() != inputs['bootstrap_manifest_sha256'] or
                metadata['observer_bindings_sha256'] != inputs['observer_bindings_sha256'] or
                not _hex(inputs['observer_bindings_sha256'])):
            _fail('bootstrap manifest commitment differs')
        programs = metadata.get('package_executables')
        if (type(programs) is not dict or set(programs) != {'python', 'systemd', 'runc', 'dockerd', 'containerd', 'shim'} or
                any(not _object(item, ('package', 'version', 'path', 'sha256')) or
                    any(type(item[key]) is not str or not item[key] for key in ('package', 'version', 'path')) or
                    not item['path'].startswith('/') or '..' in item['path'].split('/') or not _hex(item['sha256'])
                    for item in programs.values()) or programs['systemd']['package'] != 'systemd' or
                programs['systemd']['path'] != '/usr/lib/systemd/systemd'):
            _fail('bootstrap executable manifest differs')
        self._runtime_root['systemd'] = programs['systemd']['sha256']
        root = self._runtime_root['root']
        if root is not None and root['pid1_exe_sha256'] != self._runtime_root['systemd']:
            _fail('runtime root systemd digest differs from manifest')
        self._pinned_containerd = programs['containerd']['sha256']
        self._pinned_dockerd = programs['dockerd']['sha256']
        if (self._containerd_launcher is not None and
                self._containerd_launcher['target_sha256'] != self._pinned_containerd or
                any(item['dockerd_sha256'] != self._pinned_dockerd for item in self._launcher_daemons.values())):
            _fail('containerd launcher package binding differs')
        self._pinned_runc = programs['runc']['sha256']
        if any(item['exe_sha256'] != self._pinned_runc for item in self._runtime_execs.values()):
            _fail('runtime executable attestation differs from the pinned image')
        broker = self.broker_identity(deadline)
        records = self._complete_evidence(deadline)
        links = {}
        for record in records:
            body = record['body']
            if (record['kind'] != 'transaction' or body['command'] != 'LINK_CREATE' or
                    body['state'] not in ('response_settled', 'retired')):
                continue
            output = body['output']
            if (body['grant_id'] is not None or body['invocation_id'] is not None or
                    body['executor_cookie'] != broker['task_cookie'] or not _integer(body['result']) or
                    not _object(output, ('hook', 'link_id', 'program_cookie')) or
                    output['hook'] not in ('bpf', 'inode_rmdir', 'inode_mkdir') or
                    not _integer(output['link_id'], minimum=1, maximum=(1 << 32) - 1) or
                    not _integer(output['program_cookie'], minimum=1)):
                _fail('bootstrap link evidence invalid')
            if output['hook'] in links and links[output['hook']] != output:
                _fail('bootstrap link evidence conflicts')
            links[output['hook']] = output
        if (set(links) != {'bpf', 'inode_rmdir', 'inode_mkdir'} or
                len({item['link_id'] for item in links.values()}) != 3 or
                len({item['program_cookie'] for item in links.values()}) != 3):
            _fail('complete sealed bootstrap links unavailable')
        return {'version': 2, 'boot_id': self.boot_id, 'epoch': self.epoch,
                'boot_inputs_sha256': hashlib.sha256(canonical(inputs)).hexdigest(),
                'bootstrap_manifest_sha256': inputs['bootstrap_manifest_sha256'],
                'observer_bindings_sha256': inputs['observer_bindings_sha256'],
                'broker_sha256': metadata['broker_sha256'], 'guard_sha256': metadata['guard_sha256'],
                'policy_sha256': metadata['policy_sha256'], 'generator_sha256': metadata['generator_sha256'],
                'links': [links[hook] for hook in ('bpf', 'inode_rmdir', 'inode_mkdir')],
                'broker_principal': broker['principal_id'], 'bootstrap_tail': self._evidence_tail, 'records': records}

    def export_evidence(self, genesis, deadline):
        """Include the successful RELEASE record without another kernel read."""
        with self.observation(deadline):
            status = self.read_status(deadline=deadline)
            self._complete_evidence(deadline)
            if (status['state'] != 'sealed' or status['sticky'] != 0 or status['lost'] != 0 or
                    status['probe_missed'] != 0 or genesis['boot_id'] != self.boot_id or
                    genesis['epoch'] != self.epoch):
                _fail('BPF export health or boot differs')
            prefix = len(genesis['records'])
            if (prefix > self._evidence_sequence or self._evidence_hashes[prefix] != genesis['bootstrap_tail'] or
                    self._evidence_records[:prefix] != genesis['records']):
                _fail('BPF genesis transcript prefix changed')
        return {'version': 2, 'boot_id': self.boot_id, 'epoch': self.epoch,
                'genesis_tail': genesis['bootstrap_tail'],
                'records': self.evidence_records()[prefix:], 'final_watermark': self._evidence_sequence,
                'final_tail': self._evidence_tail, 'guard_status': status}

    @contextlib.contextmanager
    def checkpoint(self, nonce, deadline):
        if not _hex(nonce, _HEX32):
            _fail('checkpoint nonce invalid')
        if self._owner != os.getpid():
            _fail('principal belongs to another process')
        with self._lock:
            try:
                self._check()
                if self.role not in ('BOOT_OWNER', 'REALM_ROOT', 'RECOVERY_OBSERVER'):
                    _fail('checkpoint role invalid')
                _deadline(deadline)
                until = min(self._deadline, deadline)
                if self._checkpoint_deadline is not None:
                    until = min(until, self._checkpoint_deadline)
                _deadline(until)
                if self._depth:
                    if nonce != self._checkpoint_nonce:
                        _fail('nested checkpoint nonce differs')
                    self._checkpoint_deadline = until
                else:
                    body = self._body('QUIESCE', {'nonce': nonce, 'deadline_mono_ns': int(until * 1000000000)},
                                      ('watermark', 'tail_sha256'), deadline=until)['body']
                    if not _integer(body['watermark']) or not _hex(body['tail_sha256'], nonzero=False):
                        _fail('checkpoint watermark invalid')
                    self._checkpoint_deadline = until
                    self._checkpoint_nonce, self._checkpoint_watermark, self._checkpoint_tail = nonce, body['watermark'], body['tail_sha256']
                self._depth += 1
                try:
                    self._verify_watermark(self._checkpoint_watermark, self._checkpoint_tail, self._checkpoint_deadline)
                    yield self
                    self._check()
                finally:
                    self._depth -= 1
                    if self._depth == 0:
                        body = self._body('RELEASE', {'nonce': nonce, 'watermark': self._checkpoint_watermark},
                                          ('released', 'watermark', 'tail_sha256'), deadline=self._checkpoint_deadline)['body']
                        if (body['released'] is not True or not _integer(body['watermark']) or
                                body['watermark'] < self._checkpoint_watermark or not _hex(body['tail_sha256'], nonzero=False) or
                                (body['watermark'] == self._checkpoint_watermark and body['tail_sha256'] != self._checkpoint_tail)):
                            _fail('checkpoint release invalid')
                        self._verify_watermark(body['watermark'], body['tail_sha256'], self._checkpoint_deadline)
                        self._checkpoint_nonce = self._checkpoint_watermark = self._checkpoint_tail = None
                        self._checkpoint_deadline = None
            except BaseException:
                self.close()
                raise


class TerminalObserverClient:
    """Credentialled client for the read-only terminal observer."""

    def __init__(self, boot, epoch, observer_info, deadline):
        self.boot_id = boot
        self.epoch = epoch
        self.observer_info = observer_info
        self._deadline = deadline
        self._next = 1
        self._observer = _Process(observer_info['pid'], observer_info['executable_sha256'], observer_info['starttime'])
        self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET | getattr(socket, 'SOCK_CLOEXEC', 0))
        try:
            self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            self._sock.settimeout(_deadline(deadline))
            self._sock.connect('\0g2-bpf-terminal-' + boot)
            peer = struct.unpack('3i', self._sock.getsockopt(
                socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
            if peer != (observer_info['pid'], 0, 0):
                _fail('terminal observer peer credentials differ')
            self._peer = peer
            self._observer.check()
        except BaseException:
            self.close()
            raise

    def close(self):
        if self._sock is not None:
            try:
                self._sock.close()
            finally:
                self._sock = None
        if self._observer is not None:
            self._observer.close()
            self._observer = None

    def _rpc(self, operation, body, deadline=None):
        if self._sock is None or self._observer is None:
            _fail('terminal observer client closed')
        until = self._deadline if deadline is None else min(self._deadline, deadline)
        self._observer.check()
        nonce = os.urandom(16).hex()
        while int(nonce, 16) == 0:
            nonce = os.urandom(16).hex()
        req = {
            'version': 1, 'boot_id': self.boot_id, 'epoch': self.epoch,
            'request_id': self._next, 'nonce': nonce, 'operation': operation, 'body': body
        }
        raw = _encode(req)
        self._sock.settimeout(_deadline(until))
        if self._sock.sendmsg([raw], []) != len(raw):
            _fail('terminal observer request not sent atomically')
        self._sock.settimeout(_deadline(until))
        data, controls, flags, _ = self._sock.recvmsg(
            _MAX + 1, socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(4096),
            getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
        rights, credentials = [], []
        invalid = False
        try:
            for level, kind, payload in controls:
                if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                    descriptors = array.array('i')
                    descriptors.frombytes(payload[:len(payload) - len(payload) % descriptors.itemsize])
                    rights.extend(descriptors)
                    invalid = True
                elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS and len(payload) == struct.calcsize('3i'):
                    credentials.append(struct.unpack('3i', payload))
                else:
                    invalid = True
            if flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or invalid or credentials != [self._peer]:
                _fail('terminal observer response credentials invalid')
            resp = _decode(data)
        finally:
            for fd in rights:
                os.close(fd)
        self._observer.check()
        if (not _object(resp, ('version', 'boot_id', 'epoch', 'request_id', 'nonce', 'status', 'body')) or
                type(resp['version']) is not int or resp['version'] != 1 or resp['boot_id'] != self.boot_id or
                resp['epoch'] != self.epoch or resp['request_id'] != self._next or resp['nonce'] != nonce or
                type(resp['status']) is not str or type(resp['body']) is not dict):
            _fail('terminal observer response binding differs')
        if resp['status'] != 'OK':
            _fail(resp['status'])
        self._next += 1
        return resp['body']

    def arm(self, variant, deadline=None):
        body = self._rpc('ARM', {'variant': variant}, deadline=deadline)
        if (not _object(body, ('armed', 'begin_sequence', 'authority_before')) or body['armed'] is not True or
                not _integer(body['begin_sequence']) or type(body['authority_before']) is not dict):
            _fail('terminal observer ARM response invalid')
        return body

    def read_all(self, deadline=None, *, after=0, tail=_ZERO):
        if not _integer(after) or not _hex(tail, nonzero=False):
            _fail('terminal observer cursor invalid')
        records = []
        while True:
            body = self._rpc('READ', {'after': after, 'max_records': 64}, deadline=deadline)
            if (not _object(body, ('records', 'next', 'complete')) or type(body['records']) is not list or
                    not _integer(body['next']) or type(body['complete']) is not bool):
                _fail('terminal observer READ response invalid')
            for rec in body['records']:
                after += 1
                if (not _object(rec, ('version', 'boot_id', 'epoch', 'sequence', 'previous', 'kind', 'body')) or
                        type(rec['version']) is not int or rec['version'] != 1 or rec['boot_id'] != self.boot_id or
                        not _integer(rec['sequence'], minimum=1) or rec['sequence'] != after or rec['previous'] != tail or
                        rec['kind'] not in ('guard', 'audit_wire', 'audit', 'audit_readback', 'reader_witness', 'broker_witness')):
                    _fail('terminal observer record envelope invalid')
                if rec['kind'] in ('reader_witness', 'broker_witness'):
                    witness = rec['body']
                    schemas = {
                        'reader_witness_arm': 'version boot_id kind request_id nonce variant case_sha256',
                        'reader_witness_chunk': 'version boot_id kind variant request_id nonce role page_index page_count total_bytes total_sha256 data_b64',
                        'reader_witness_event': 'version boot_id kind variant request_id nonce check request_sha256 request_bytes original_reply_sha256 original_reply_bytes sent_reply_sha256 sent_reply_bytes send_result sent_rights endpoint_closed queries',
                        'reader_pidfd_exit': 'kind variant reader_pid pidfd_exited query_dup_closed_after_exit',
                        'broker_witness_chunk': 'version boot_id kind variant request_id nonce role page_index page_count total_bytes total_sha256 data_b64',
                        'broker_credential_send': 'version boot_id kind variant request_id nonce input_sha256 input_bytes send_result',
                        'broker_receive_observation': 'version boot_id kind variant source detector request_id nonce input_sha256 input_bytes original_sha256 expected_actor observed_actor deadline_ns observed_ns capacity count_before rejected_wire guard_sequence receive_observation',
                    }
                    key = witness.get('kind') if type(witness) is dict else None
                    if (key not in schemas or not _object(witness, schemas[key].split()) or
                            (key != 'reader_pidfd_exit' and (witness['version'] != 1 or witness['boot_id'] != self.boot_id)) or
                            (key == 'reader_pidfd_exit' and (witness['variant'] != 'reader-death' or
                             not _integer(witness['reader_pid'], minimum=1) or witness['pidfd_exited'] is not True or
                             witness['query_dup_closed_after_exit'] is not True)) or
                            (rec['kind'] == 'broker_witness') != key.startswith('broker_')):
                        _fail('terminal witness schema invalid')
                else:
                    if not _object(rec['body'], _RECORD_KEYS[rec['kind']]):
                        _fail('terminal observer record body invalid')
                    _record_body(rec['kind'], rec['body'])
                tail = hashlib.sha256(b'HOUFENG-G2-BPF-OBS-V1\0' + canonical(rec)).hexdigest()
                records.append(rec)
            if body['next'] != after:
                _fail('terminal observer READ cursor mismatch')
            if body['complete']:
                return records, tail

    def finish(self, deadline=None):
        body = self._rpc('FINISH', {}, deadline=deadline)
        if (not _object(body, ('end_sequence', 'terminal_sha256', 'authority_after', 'losses', 'outcome', 'fault_evidence')) or
                not _integer(body['end_sequence']) or not _hex(body['terminal_sha256'], nonzero=False) or
                type(body['authority_after']) is not dict or
                not _object(body['losses'], ('native', 'audit', 'output')) or
                any(type(body['losses'][k]) is not bool for k in ('native', 'audit', 'output')) or
                body['outcome'] not in ('denial_proved', 'closure_on_fault', 'early_no_handoff')):
            _fail('terminal observer FINISH response invalid')
        if body['fault_evidence'] is not None and not _object(body['fault_evidence'],
                'variant source detector request_id nonce input_sha256 input_bytes expected_actor observed_actor deadline_ns observed_ns capacity count_before rejected_wire guard_sequences reader_witness_sequences broker_witness_sequences'.split()):
            _fail('terminal observer fault evidence schema invalid')
        if body['outcome'] == 'denial_proved' and any(body['losses'].values()):
            _fail('terminal observer denial has observation loss')
        return body
