"""Root-held callback process and backend continuity across coordinator death."""
import base64
import binascii
import ctypes
import json
import fcntl
import platform
import signal
import threading
import datetime
import hashlib
import os
import re
import select
import socket
import stat
import sys
import time

import pg_bootstrap
from contract import (CHILD_CGROUP, GUARD, INTENT_KEYS, MAX_FRAME, RUN_TOKEN, callback_intent_name,
                      launch_authority_name, launch_used_name, phase_name)
from journal import JournalError, _read, _write_new, canonical, process_start, strict_json
from network_verify import _read as read_kernel
from origins import _identity, _thread_credentials
from phase import open_control_dir, read_private, read_stdout_private

HEX = re.compile(r'^[0-9a-f]{64}$')
ROLE = re.compile(r'^[a-z_][a-z0-9_]{0,62}$')


class ObserverDeadline(JournalError):
    """The bounded observer wait elapsed before its requested event."""


class ObserverAlreadyTraced(JournalError):
    """The kernel reports another tracer for the intended predecessor."""


def read_stdout_carrier(directory, generation, intent, written=None):
    raw = read_stdout_private(directory, generation)
    carrier = strict_json(raw)
    keys = {'version', 'phase_generation', 'recovery_id', 'application', 'realm_digest', 'result_digest', 'stdout_bytes'}
    if (type(carrier) is not dict or set(carrier) != keys or type(carrier['version']) is not int or carrier['version'] != 1 or
            type(carrier['phase_generation']) is not int or carrier['phase_generation'] != generation or
            any(carrier[key] != intent[key] for key in ('recovery_id', 'application', 'realm_digest')) or
            type(carrier['result_digest']) is not str or HEX.fullmatch(carrier['result_digest']) is None or
            type(carrier['stdout_bytes']) is not str):
        raise JournalError('original stdout carrier binding differs')
    try:
        encoded = carrier['stdout_bytes'].encode('ascii', 'strict')
        body = base64.b64decode(encoded, validate=True)
    except (ValueError, UnicodeError, binascii.Error) as exc:
        raise JournalError('original stdout carrier encoding is invalid') from exc
    if (base64.b64encode(body) != encoded or not 0 < len(body) <= MAX_FRAME or not body.endswith(b'\n') or
            body.count(b'\n') != 1 or hashlib.sha256(body).hexdigest() != carrier['result_digest']):
        raise JournalError('original stdout bytes or digest differ')
    result = strict_json(body)
    if (type(result) is not dict or set(result) != {'version', 'phase_generation', 'status', 'operation_id', 'reason'} or
            type(result['version']) is not int or result['version'] != 1 or
            type(result['phase_generation']) is not int or result['phase_generation'] != generation or
            type(result['operation_id']) is not str or type(result['reason']) is not str):
        raise JournalError('original stdout result shape differs')
    valid = ((result['status'] == 'committed' and result['reason'] == 'app_reserved' and
              re.fullmatch(r'rpo_[a-z0-9]{1,64}', result['operation_id']) is not None) or
             (result['status'] == 'rejected' and result['reason'] == 'source_denied' and result['operation_id'] == '') or
             (result['status'] == 'unavailable' and result['reason'] in
              ('inventory_unavailable', 'admission_unavailable', 'service_unavailable') and result['operation_id'] == ''))
    if not valid:
        raise JournalError('original stdout business result differs')
    if written is not None:
        expected = {key: carrier[key] for key in ('version', 'phase_generation', 'recovery_id', 'application', 'realm_digest', 'result_digest')}
        expected['result'] = result
        if written != expected:
            raise JournalError('written result does not bind original stdout bytes')
    return raw, carrier, result

_LIBC = ctypes.CDLL(None, use_errno=True)
_LIBC.ptrace.restype = ctypes.c_long
_LIBC.syscall.restype = ctypes.c_long
_PTRACE_CONT = 7
_PTRACE_GETEVENTMSG = 0x4201
_PTRACE_GETSIGINFO = 0x4202
_PTRACE_SEIZE = 0x4206
_PTRACE_LISTEN = 0x4208
_PTRACE_EVENT_EXIT = 6
_PTRACE_EVENT_STOP = 128
_PTRACE_OPTIONS = 0x40 | 0x100000  # TRACEEXIT and EXITKILL
_WALL = 0x40000000
_GROUP_STOPS = frozenset((signal.SIGSTOP, signal.SIGTSTP, signal.SIGTTIN, signal.SIGTTOU))


def _trace(request, pid, data=0):
    result = _LIBC.ptrace(ctypes.c_uint(request), ctypes.c_int(pid), ctypes.c_void_p(), ctypes.c_void_p(data))
    if result == -1:
        raise OSError(ctypes.get_errno(), 'predecessor ptrace failed')
    return result


def _tracer_pid(pid):
    fields = [line.split(b':', 1)[1].strip() for line in read_kernel('/proc/%d/status' % pid, 65536).encode().splitlines()
              if line.startswith(b'TracerPid:')]
    if len(fields) != 1 or not fields[0].isdigit():
        raise JournalError('predecessor tracer identity unavailable')
    return int(fields[0])


def _same_files(pid, tid):
    number = {'x86_64': 312, 'aarch64': 272}.get(platform.machine())
    if number is None:
        raise JournalError('KCMP_FILES architecture unsupported')
    result = _LIBC.syscall(ctypes.c_long(number), ctypes.c_int(pid), ctypes.c_int(tid),
                           ctypes.c_int(2), ctypes.c_ulong(0), ctypes.c_ulong(0))
    if result == -1:
        raise OSError(ctypes.get_errno(), 'KCMP_FILES unavailable')
    if result != 0:
        raise JournalError('predecessor thread does not share leader file table')


def _guard_fd(pid, guard):
    info = os.stat('/proc/%d/fd/3' % pid)
    if not stat.S_ISREG(info.st_mode) or (info.st_dev, info.st_ino) != (guard['dev'], guard['ino']):
        raise JournalError('predecessor leader guard identity changed')


def _guard_holders(guard):
    # Every task is inspected; disappearing or changing descriptors fail closed.
    processes = sorted(name for name in os.listdir('/proc') if name.isdecimal())
    holders = set()
    for name in processes:
        pid = int(name)
        taskdir = '/proc/%s/task' % name
        tasks = sorted(os.listdir(taskdir))
        if not tasks or any(not tid.isdecimal() for tid in tasks):
            raise JournalError('guard holder task inventory invalid')
        for tid in tasks:
            directory = taskdir + '/' + tid + '/fd'
            directory_info = os.stat(directory)
            def snapshot():
                descriptors, found = set(), False
                with os.scandir(directory) as entries:
                    for entry in entries:
                        if not entry.name.isdecimal():
                            raise JournalError('guard holder descriptor inventory invalid')
                        info = entry.stat(follow_symlinks=True)
                        if pid == os.getpid() and (info.st_dev, info.st_ino) == (directory_info.st_dev, directory_info.st_ino):
                            continue  # Our scanner descriptor is never the guard.
                        descriptors.add((entry.name, info.st_dev, info.st_ino))
                        found |= (info.st_dev, info.st_ino) == (guard['dev'], guard['ino'])
                return descriptors, found
            first, found = snapshot()
            second, found_again = snapshot()
            if first != second or found != found_again:
                raise JournalError('guard holder descriptors changed during census')
            if found:
                holders.add(pid)
        if sorted(os.listdir(taskdir)) != tasks:
            raise JournalError('guard holder threads changed during census')
    if sorted(name for name in os.listdir('/proc') if name.isdecimal()) != processes:
        raise JournalError('guard holder processes changed during census')
    return holders


class ExitObserver:
    """One thread owns every ptrace and wait operation for one predecessor."""

    def __init__(self, child, pidfd, generation, live_digest):
        self.child = dict(child)
        self.pidfd = os.dup(pidfd)
        self.generation = generation
        self.live_digest = live_digest
        self.root = {'pid': os.getpid(), 'starttime': process_start(os.getpid())}
        self.thread_identity = None
        self.condition = threading.Condition()
        self.attached = False
        self.armed = False
        self.stopped = None
        self.release = False
        self.terminal = None
        self.delivered_signals = []
        self.group_stops = []
        self.error = None
        self.thread = threading.Thread(target=self._run, name='c80-exit-observer-%d' % generation, daemon=False)
        self.thread.start()

    def _wait_for(self, predicate, deadline):
        with self.condition:
            while not predicate():
                if self.error is not None:
                    raise JournalError('predecessor exit observer failed') from self.error
                if not self.thread.is_alive():
                    raise JournalError('predecessor exit observer terminated unexpectedly')
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise ObserverDeadline('predecessor exit observer deadline exceeded')
                self.condition.wait(min(remaining, 0.1))
            if self.error is not None:
                raise JournalError('predecessor exit observer failed') from self.error

    def await_attached(self, deadline):
        self._wait_for(lambda: self.attached, deadline)
        if (_tracer_pid(self.child['pid']) != self.thread_identity['pid'] or
                process_start(self.thread_identity['pid']) != self.thread_identity['starttime'] or
                process_start(self.root['pid']) != self.root['starttime']):
            raise JournalError('predecessor tracer binding changed')

    def arm(self, coordinator, guard, deadline):
        self.await_attached(deadline)
        pid = self.child['pid']
        if select.select([self.pidfd], [], [], 0)[0] or process_start(pid) != self.child['starttime']:
            raise JournalError('predecessor exited before observer arm')
        _guard_fd(pid, guard)
        tasks = sorted(os.listdir('/proc/%d/task' % pid))
        if not tasks or any(not tid.isdecimal() for tid in tasks):
            raise JournalError('predecessor task inventory unavailable')
        for tid in tasks:
            _same_files(pid, int(tid))
        if sorted(os.listdir('/proc/%d/task' % pid)) != tasks:
            raise JournalError('predecessor tasks changed during file-table observation')
        if _guard_holders(guard) != {pid, coordinator['pid']}:
            raise JournalError('guard has an unexpected holder before injection')
        with self.condition:
            if self.armed or self.stopped is not None or self.terminal is not None or self.error is not None:
                raise JournalError('predecessor observer cannot be armed')
            self.armed = True
            self.condition.notify_all()

    def exit_stop(self, coordinator_pidfd, guard, deadline):
        self._wait_for(lambda: self.stopped is not None, deadline)
        if (not select.select([coordinator_pidfd], [], [], 0)[0] or
                select.select([self.pidfd], [], [], 0)[0] or self.stopped != signal.SIGKILL or
                process_start(self.child['pid']) != self.child['starttime'] or
                _tracer_pid(self.child['pid']) != self.thread_identity['pid']):
            raise JournalError('coordinator death or predecessor exit stop differs')
        _guard_fd(self.child['pid'], guard)
        if _guard_holders(guard) != {self.child['pid']}:
            raise JournalError('guard has another holder at predecessor exit stop')
        fd = os.open(GUARD, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if (info.st_dev, info.st_ino) != (guard['dev'], guard['ino']):
                raise JournalError('guard competitor identity differs')
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                pass
            else:
                raise JournalError('guard was not held at predecessor exit stop')
        finally:
            os.close(fd)
        return {'observer_root': self.root, 'observer_thread': self.thread_identity, 'child': self.child,
                'phase_generation': self.generation, 'callback_live_digest': self.live_digest,
                'event': 'PTRACE_EVENT_EXIT', 'exit_signal': 'SIGKILL',
                'leader_guard_device': guard['dev'], 'leader_guard_inode': guard['ino'],
                'shared_file_table_verified': True, 'other_guard_holders': [],
                'coordinator_pidfd_exited': True, 'guard_contended_at_exit_stop': True}

    def resume_and_join(self, deadline):
        with self.condition:
            self.release = True
            self.condition.notify_all()
        self._wait_for(lambda: self.terminal is not None, deadline)
        self.thread.join(max(0, deadline - time.monotonic()))
        if self.thread.is_alive() or not select.select([self.pidfd], [], [], max(0, deadline - time.monotonic()))[0]:
            raise JournalError('predecessor tracer or pidfd did not settle')
        if self.armed and (not os.WIFSIGNALED(self.terminal) or os.WTERMSIG(self.terminal) != signal.SIGKILL):
            raise JournalError('predecessor terminal wait is not the observed SIGKILL')
        return {'tracee_resumed': True, 'tracer_terminal_wait': True, 'tracer_joined': True,
                'child_pidfd_exited': True}

    def close(self, deadline):
        if self.pidfd is None:
            return
        with self.condition:
            self.release = True
            self.condition.notify_all()
            while self.thread.is_alive() and not self.attached and self.error is None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise JournalError('predecessor observer attach did not settle during cleanup')
                self.condition.wait(min(remaining, 0.1))
            attached = self.attached
        if attached and self.thread.is_alive() and not select.select([self.pidfd], [], [], 0)[0]:
            try:
                signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
            except ProcessLookupError:
                pass
        self.thread.join(max(0, deadline - time.monotonic()))
        if self.thread.is_alive():
            raise JournalError('predecessor observer did not join during cleanup')
        os.close(self.pidfd)
        self.pidfd = None

    def _run(self):
        pid = self.child['pid']
        seized = stopped = False
        try:
            tid = threading.get_native_id()
            identity = {'pid': tid, 'starttime': process_start(tid)}
            if _tracer_pid(pid) != 0:
                raise ObserverAlreadyTraced('predecessor already has a tracer')
            if os.geteuid() != 0 or process_start(pid) != self.child['starttime']:
                raise JournalError('predecessor cannot be independently seized')
            _trace(_PTRACE_SEIZE, pid, _PTRACE_OPTIONS)
            seized = True
            if _tracer_pid(pid) != tid:
                raise JournalError('kernel tracer is not the observer thread')
            with self.condition:
                self.thread_identity = identity
                self.attached = True
                self.condition.notify_all()
            while True:
                observed, status = os.waitpid(pid, os.WNOHANG | _WALL)
                if not observed:
                    with self.condition:
                        self.condition.wait(0.01)
                    continue
                if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                    with self.condition:
                        self.terminal = status
                        self.condition.notify_all()
                    seized = False
                    return
                if not os.WIFSTOPPED(status):
                    raise JournalError('unknown predecessor wait status')
                stopped = True
                stop_signal, event = os.WSTOPSIG(status), status >> 16
                if event == _PTRACE_EVENT_EXIT and stop_signal == signal.SIGTRAP:
                    message = ctypes.c_ulong()
                    _trace(_PTRACE_GETEVENTMSG, pid, ctypes.addressof(message))
                    with self.condition:
                        if self.armed:
                            self.stopped = message.value
                            self.condition.notify_all()
                            while not self.release:
                                self.condition.wait()
                    _trace(_PTRACE_CONT, pid)
                elif event == _PTRACE_EVENT_STOP and stop_signal in _GROUP_STOPS:
                    _trace(_PTRACE_LISTEN, pid)
                    with self.condition:
                        if len(self.group_stops) >= 128:
                            raise JournalError('too many predecessor group stops')
                        self.group_stops.append(stop_signal)
                        self.condition.notify_all()
                elif event == _PTRACE_EVENT_STOP and stop_signal == signal.SIGTRAP:
                    # SIGCONT can notify a seized tracee before signal delivery;
                    # this observer neither interrupts nor auto-attaches tracees.
                    siginfo = ctypes.create_string_buffer(128)
                    _trace(_PTRACE_GETSIGINFO, pid, ctypes.addressof(siginfo))
                    if (ctypes.c_int.from_buffer(siginfo).value != signal.SIGTRAP or
                            ctypes.c_int.from_buffer(siginfo, 8).value != ((_PTRACE_EVENT_STOP << 8) | signal.SIGTRAP)):
                        raise JournalError('predecessor job-control event differs')
                    _trace(_PTRACE_CONT, pid)
                elif event == 0:
                    siginfo = ctypes.create_string_buffer(128)
                    _trace(_PTRACE_GETSIGINFO, pid, ctypes.addressof(siginfo))
                    if ctypes.c_int.from_buffer(siginfo).value != stop_signal:
                        raise JournalError('predecessor signal-delivery stop differs')
                    _trace(_PTRACE_CONT, pid, stop_signal)
                    with self.condition:
                        if len(self.delivered_signals) >= 128:
                            raise JournalError('too many predecessor signal stops')
                        self.delivered_signals.append(stop_signal)
                        self.condition.notify_all()
                else:
                    raise JournalError('unknown predecessor ptrace event: status=%d event=%d signal=%d' %
                                       (status, event, stop_signal))
                stopped = False
        except BaseException as exc:
            if seized:
                try:
                    signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
                    if stopped:
                        _trace(_PTRACE_CONT, pid)
                except (OSError, ProcessLookupError):
                    pass
            with self.condition:
                self.error = exc
                self.condition.notify_all()


def _key(value):
    if (type(value) is not dict or set(value) != {'pid', 'starttime', 'executable_sha256'} or
            type(value['pid']) is not int or value['pid'] <= 0 or type(value['starttime']) is not int or
            value['starttime'] <= 0 or type(value['executable_sha256']) is not str or
            HEX.fullmatch(value['executable_sha256']) is None):
        raise JournalError('callback coordinator identity invalid')
    return value['pid'], value['starttime'], value['executable_sha256']


def _cgroup(life):
    expected = life.cgroups['child_cgroup']
    if type(expected) is not dict or expected.get('path') != CHILD_CGROUP:
        raise JournalError('callback child cgroup unavailable')
    info = os.stat(CHILD_CGROUP, follow_symlinks=False)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
            (info.st_dev, info.st_ino) != (expected['dev'], expected['ino'])):
        raise JournalError('callback child cgroup identity changed')
    text = read_kernel(CHILD_CGROUP + '/cgroup.procs', 65536).split()
    if len(text) > 1 or any(not item.isdecimal() or int(item) <= 0 for item in text):
        raise JournalError('callback child cgroup contains unexpected processes')
    events = dict(line.split() for line in read_kernel(CHILD_CGROUP + '/cgroup.events', 1024).splitlines())
    if events.get('populated') not in ('0', '1') or (not text and events['populated'] != '0'):
        raise JournalError('callback child cgroup is not fully settled')
    after = os.stat(CHILD_CGROUP, follow_symlinks=False)
    if (info.st_dev, info.st_ino) != (after.st_dev, after.st_ino):
        raise JournalError('callback child cgroup changed during observation')
    return [int(item) for item in text]


def _root_file(journal, name):
    try:
        info = os.stat(name, dir_fd=journal._fd, follow_symlinks=False)
    except FileNotFoundError:
        return False
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
            stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
        raise JournalError('callback root marker identity differs')
    return True


def _sessions(life, app_facts, endpoint):
    role = app_facts['runtime_role']
    if type(role) is not str or ROLE.fullmatch(role) is None:
        raise JournalError('callback runtime role invalid')
    query = ("SELECT json_build_object("
             "'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),"
             "'database_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),"
             "'database_name',current_database(),"
             "'domain_id',(SELECT domain_id FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
             "'domain_epoch',(SELECT identity_epoch FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
             "'domain_count',(SELECT count(*) FROM public.record_platform_domain_identity),"
             "'prepared',(SELECT count(*) FROM pg_prepared_xacts),"
             "'sessions',coalesce((SELECT json_agg(json_build_object('pid',pid,'role',usename,"
             "'started_us',floor(extract(epoch FROM backend_start)*1000000)::text,'state',state) ORDER BY pid) "
             "FROM pg_stat_activity WHERE backend_type='client backend' AND datname=current_database() AND usename='%s'),"
             "'[]'::json))::text AS state\0") % role
    value = pg_bootstrap._run('app', life.postgres.passwords['app'], 'records', query_override=query.encode('ascii'),
                              host_override=endpoint['app_host'])
    app = app_facts['application']
    if (type(value) is not dict or set(value) != {'system_identifier', 'database_oid', 'database_name', 'domain_id',
                                                   'domain_epoch', 'domain_count', 'prepared', 'sessions'} or
            value['system_identifier'] != app['SystemIdentifier'] or value['database_oid'] != app['DatabaseOID'] or
            value['database_name'] != app['DatabaseName'] or value['domain_id'] != app['ID'] or
            value['domain_epoch'] != app['Epoch'] or value['domain_count'] != 1 or
            type(value['prepared']) is not int or value['prepared'] != 0 or type(value['sessions']) is not list):
        raise JournalError('callback APP identity or prepared transactions differ')
    for entry in value['sessions']:
        if (type(entry) is not dict or set(entry) != {'pid', 'role', 'started_us', 'state'} or
                type(entry['pid']) is not int or entry['pid'] <= 0 or entry['role'] != role or
                type(entry['started_us']) is not str or not entry['started_us'].isdigit() or
                type(entry['state']) is not str):
            raise JournalError('callback runtime backend identity invalid')
    return value['sessions']


class CallbackLifecycle:
    def __init__(self, life):
        self.life = life
        self.coordinators = {}
        self.callbacks = {}

    def pin_coordinator(self, identity, pidfd):
        key = _key(identity)
        if key in self.coordinators or select.select([pidfd], [], [], 0)[0] or process_start(key[0]) != key[1]:
            raise JournalError('callback coordinator held lifetime invalid')
        self.coordinators[key] = {'identity': dict(identity), 'pidfd': os.dup(pidfd), 'wait_status': None}

    def coordinator_waited(self, identity, status):
        item = self.coordinators.get(_key(identity))
        if (item is None or item['wait_status'] is not None or type(status) is not int or
                not select.select([item['pidfd']], [], [], 0)[0]):
            raise JournalError('callback coordinator direct wait is not held')
        item['wait_status'] = status

    def _intent(self, generation, app_facts, recovery_id, realm_digest):
        directory = open_control_dir()
        try:
            try:
                raw = read_private(directory, callback_intent_name(generation))
            except FileNotFoundError:
                return None
        finally:
            os.close(directory)
        intent = strict_json(raw)
        if (type(intent) is not dict or frozenset(intent) != INTENT_KEYS or type(intent['version']) is not int or
                intent['version'] != 1 or type(intent['phase_generation']) is not int or
                intent['phase_generation'] != generation or intent['recovery_id'] != recovery_id or
                intent['application'] != app_facts['application'] or intent['realm_digest'] != realm_digest or
                type(intent['config_fd']) is not int or not 0 <= intent['config_fd'] < (1 << 31) or
                intent['child_cgroup'] != self.life.cgroups['child_cgroup'] or
                intent['launcher_sha256'] != self.life.cgroups['programs']['c80-launcher'] or
                intent['predecessor_sha256'] != self.life.cgroups['programs']['c80-predecessor'] or
                any(type(intent.get(key)) is not str or HEX.fullmatch(intent[key]) is None
                    for key in ('config_digest', 'launcher_sha256', 'predecessor_sha256')) or
                any(type(intent.get(key)) is not str or not 0 < len(intent[key]) <= 256 for key in ('actor_user_id', 'record_id'))):
            raise JournalError('original callback intent differs')
        guard = os.stat(GUARD, follow_symlinks=False)
        if (not stat.S_ISREG(guard.st_mode) or intent['guard'] != {'dev': guard.st_dev, 'ino': guard.st_ino} or
                _key(intent['coordinator']) not in self.coordinators):
            raise JournalError('original callback guard or held coordinator missing')
        return {'intent': intent, 'intent_sha256': hashlib.sha256(raw).hexdigest()}

    def classify(self, phase, current, app_facts, recovery_id, realm_digest):
        generation, name = phase['generation'], phase['phase']
        if name == 'predecessor_callback':
            candidate = generation
        elif name == 'refencing':
            if generation <= 1:
                raise JournalError('callback refencing lacks previous phase')
            directory = open_control_dir()
            try:
                previous = strict_json(read_private(directory, phase_name(generation - 1)))
            finally:
                os.close(directory)
            if previous.get('phase') != 'predecessor_callback':
                raise JournalError('refencing is not a callback recovery')
            candidate = generation - 1
        elif name == 'quiescent':
            candidate = generation + 1
        else:
            raise JournalError('callback observation phase unsupported')
        item = self._intent(candidate, app_facts, recovery_id, realm_digest)
        if name == 'quiescent' and generation > 2:
            directory = open_control_dir()
            try:
                previous = strict_json(read_private(directory, phase_name(generation - 1)))
                earlier = strict_json(read_private(directory, phase_name(generation - 2)))
            finally:
                os.close(directory)
            if previous.get('phase') == 'refencing' and earlier.get('phase') == 'predecessor_callback':
                previous_generation = generation - 2
                held = self.callbacks.get(previous_generation)
                if held is None or not held['joined']:
                    if item is not None:
                        raise JournalError('new callback intent precedes original durable join')
                    candidate = previous_generation
                    item = self._intent(candidate, app_facts, recovery_id, realm_digest)
                    if item is None:
                        raise JournalError('previous callback intent missing')
        if item is None:
            if name == 'quiescent':
                return None
            raise JournalError('original callback intent missing')
        item['phase_generation'] = candidate
        item['recovery'] = item['intent']['coordinator'] != current
        item['phase'] = name
        item['app_facts'] = app_facts
        return item

    def permit(self, candidate, permit):
        generation = candidate['phase_generation']
        intent = candidate['intent']
        if candidate['recovery'] or permit.get('coordinator') != intent['coordinator']:
            raise JournalError('recovery cannot receive launch authority')
        existing = self.callbacks.get(generation)
        if existing is not None:
            if existing['intent_sha256'] != candidate['intent_sha256'] or existing['intent'] != intent:
                raise JournalError('callback intent changed after permit')
            return
        self.callbacks[generation] = {'intent': intent, 'intent_sha256': candidate['intent_sha256'],
                                      'child': None, 'child_pidfd': None, 'backend': None, 'observer': None, 'joined': False}
        self.life.journal.append('registration', {'name': 'callback-permit-%020d' % generation,
            'identity_digest': hashlib.sha256(canonical({'phase_generation': generation,
                'intent_sha256': candidate['intent_sha256'], 'coordinator': intent['coordinator']})).hexdigest()})

    def capture_ready(self, candidate, endpoint, deadline):
        generation = candidate['phase_generation']
        item = self.callbacks.get(generation)
        if item is None or item['intent_sha256'] != candidate['intent_sha256'] or item['joined']:
            raise JournalError('callback permit registration missing')
        pids = _cgroup(self.life)
        used = _root_file(self.life.journal, launch_used_name(generation))
        if not pids:
            if used or item['child'] is not None:
                raise JournalError('used callback launch lacks live child capture')
            return
        if not used:
            raise JournalError('callback child lacks one-use launch marker')
        pid = pids[0]
        child = _identity(pid, full=True)
        owner = item['intent']['coordinator']
        if (child is None or child['parent'] != owner['pid'] or child['exe'] != '/opt/g2/c80-predecessor' or
                child['exe_sha256'] != item['intent']['predecessor_sha256'] or
                child['argv'] != [b'/opt/g2/c80-predecessor', b'service-preview-execute'] or
                not child['cgroup'].endswith('/g2-c80-predecessor') or
                process_start(pid) != child['starttime']):
            raise JournalError('actual callback child identity differs')
        _thread_credentials(pid, 1)
        sessions = _sessions(self.life, candidate['app_facts'], endpoint)
        if len(sessions) != 1 or item['child'] is None and sessions[0]['state'] != 'idle':
            raise JournalError('actual callback backend was not uniquely ready')
        identity = {'pid': pid, 'starttime': child['starttime'], 'executable_sha256': child['exe_sha256']}
        backend = sessions[0]
        if item['child'] is not None:
            if (item['child'] != identity or any(item['backend'][key] != backend[key] for key in ('pid', 'role', 'started_us')) or
                    select.select([item['child_pidfd']], [], [], 0)[0] or
                    _read(self.life.journal._fd, 'callback-live-%020d.json' % generation) != canonical(item['live'])):
                raise JournalError('callback child or backend changed')
            item['observer'].await_attached(deadline)
            return
        held = self.life.origins.predecessors.get((pid, child['starttime']))
        if (held is None or held[0] != child['exe_sha256'] or held[1] != owner['pid'] or
                select.select([held[2]], [], [], 0)[0]):
            raise JournalError('root did not retain the original child pidfd')
        item['child_pidfd'] = os.dup(held[2])
        item['child'], item['backend'] = identity, backend
        live = {'version': 1, 'run_token': RUN_TOKEN, 'phase_generation': generation,
                'intent_sha256': item['intent_sha256'], 'coordinator': owner, 'child': identity, 'backend': backend}
        _write_new(self.life.journal._fd, 'callback-live-%020d.json' % generation, live)
        item['live'] = live
        self.life.journal.append('registration', {'name': 'callback-live-%020d' % generation,
                                                    'identity_digest': hashlib.sha256(canonical(live)).hexdigest()})
        item['observer'] = ExitObserver(identity, item['child_pidfd'], generation, hashlib.sha256(canonical(live)).hexdigest())
        item['observer'].await_attached(deadline)

    def _effects(self, generation):
        directory = open_control_dir()
        try:
            result = {}
            for prefix in ('predecessor-ready-', 'predecessor-create-intent-', 'predecessor-create-result-',
                           'predecessor-reserve-intent-', 'predecessor-reserve-result-', 'predecessor-result-written-'):
                name = prefix + '%020d.json' % generation
                try:
                    raw = read_private(directory, name)
                except FileNotFoundError:
                    continue
                result[name] = hashlib.sha256(raw).hexdigest()
            written_name = 'predecessor-result-written-%020d.json' % generation
            stdout_name = 'predecessor-stdout-%020d.json' % generation
            written = strict_json(read_private(directory, written_name)) if written_name in result else None
            try:
                raw, _, _ = read_stdout_carrier(directory, generation, self.callbacks[generation]['intent'], written)
            except FileNotFoundError:
                if written is not None:
                    raise JournalError('written result lacks its original stdout carrier')
            else:
                result[stdout_name] = hashlib.sha256(raw).hexdigest()
            for entry in os.listdir(self.life.journal._fd):
                if not re.fullmatch(r'fault-intent-[a-z0-9-]+\.json', entry):
                    continue
                observed = strict_json(_read(self.life.journal._fd, entry))
                if (type(observed) is dict and observed.get('phase_generation') == generation and
                        type(observed.get('receipt_name')) is str and
                        observed['receipt_name'].startswith(('predecessor-create-', 'predecessor-reserve-', 'predecessor-result-written-')) and
                        result.get(observed['receipt_name']) != observed.get('evidence_digest')):
                    raise JournalError('previously witnessed callback effect is missing or changed')
                if (type(observed) is dict and observed.get('phase_generation') == generation and
                        observed.get('boundary') == 'stdout-loss' and
                        (observed.get('stdout_carrier_name') != stdout_name or
                         result.get(stdout_name) != observed.get('stdout_carrier_digest'))):
                    raise JournalError('previously witnessed stdout carrier is missing or changed')
            return result
        finally:
            os.close(directory)

    def settle(self, candidate, endpoint, deadline, current):
        generation = candidate['phase_generation']
        intent = candidate['intent']
        item = self.callbacks.get(generation)
        if item is not None and (item['intent_sha256'] != candidate['intent_sha256'] or item['intent'] != intent):
            raise JournalError('original callback intent changed')
        if item is not None and item['joined']:
            name = 'callback-join-%020d.json' % generation
            record = strict_json(_read(self.life.journal._fd, name))
            if (record.get('phase_generation') != generation or record.get('intent_sha256') != item['intent_sha256'] or
                    record.get('original_coordinator') != intent['coordinator'] or record.get('effect_files') != self._effects(generation) or
                    _cgroup(self.life) or _sessions(self.life, candidate['app_facts'], endpoint)):
                raise JournalError('joined callback continuity changed')
            return
        owner = self.coordinators.get(_key(intent['coordinator']))
        if owner is None:
            raise JournalError('original callback coordinator lifetime missing')
        same_owner = intent['coordinator'] == current
        if not same_owner and (owner['wait_status'] is None or owner['pidfd'] is None or
                               not select.select([owner['pidfd']], [], [], 0)[0]):
            raise JournalError('original coordinator was not directly waited')
        if item is None:
            if (_root_file(self.life.journal, launch_authority_name(generation)) or
                    _root_file(self.life.journal, launch_used_name(generation))):
                raise JournalError('old callback launch history was not captured')
            item = {'intent': intent, 'intent_sha256': candidate['intent_sha256'],
                    'child': None, 'child_pidfd': None, 'backend': None, 'observer': None, 'joined': False}
            self.callbacks[generation] = item
        if item['joined']:
            return
        if item['child'] is None:
            if (_root_file(self.life.journal, launch_authority_name(generation)) or
                    _root_file(self.life.journal, launch_used_name(generation))):
                raise JournalError('launched callback has no held child and backend')
        else:
            if (item['child_pidfd'] is None or item['backend'] is None or 'live' not in item or
                    _read(self.life.journal._fd, 'callback-live-%020d.json' % generation) != canonical(item['live'])):
                raise JournalError('old callback lifecycle was not captured')
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([item['child_pidfd']], [], [], remaining)[0]:
                raise JournalError('original child did not exit before recovery observation')
            if item['observer'] is None:
                raise JournalError('original predecessor observer is missing')
            item['observer'].resume_and_join(deadline)
        while True:
            if _cgroup(self.life):
                if time.monotonic() >= deadline:
                    raise JournalError('old callback cgroup did not empty')
            else:
                sessions = _sessions(self.life, candidate['app_facts'], endpoint)
                if not sessions:
                    break
                if item['backend'] is None or any(session['pid'] != item['backend']['pid'] or
                        session['started_us'] != item['backend']['started_us'] or
                        session['role'] != item['backend']['role'] for session in sessions):
                    raise JournalError('unregistered runtime backend during callback recovery')
                if time.monotonic() >= deadline:
                    raise JournalError('original callback backend has not settled')
            select.select([], [], [], min(0.05, max(0, deadline - time.monotonic())))
        if same_owner:
            return
        if item['child'] is not None:
            child = item['child']
            held = self.life.origins.predecessors.get((child['pid'], child['starttime']))
            if (held is None or held[0] != child['executable_sha256'] or held[1] != intent['coordinator']['pid'] or
                    not select.select([held[2]], [], [], 0)[0]):
                raise JournalError('original root child handle was not retained through join')
        evidence = {'version': 1, 'run_token': RUN_TOKEN, 'phase_generation': generation,
                    'intent_sha256': item['intent_sha256'], 'application': intent['application'],
                    'recovery_id': intent['recovery_id'], 'actor_user_id': intent['actor_user_id'],
                    'record_id': intent['record_id'], 'original_coordinator': intent['coordinator'],
                    'coordinator_direct_wait_status': owner['wait_status'], 'coordinator_pidfd_exited': True,
                    'child': item['child'], 'child_pidfd_exited': item['child'] is not None,
                    'child_direct_wait_by_root': False, 'backend': item['backend'], 'backend_settled': True,
                    'child_cgroup_empty': True, 'prepared_transactions': 0, 'effect_files': self._effects(generation)}
        _write_new(self.life.journal._fd, 'callback-join-%020d.json' % generation, evidence)
        self.life.journal.append('registration', {'name': 'callback-join-%020d' % generation,
                                                    'identity_digest': hashlib.sha256(canonical(evidence)).hexdigest()})
        if item['child'] is not None:
            self.life.origins.join_predecessor(item['child'], intent['coordinator']['pid'])
            item['observer'].close(deadline)
            item['observer'] = None
            os.close(item['child_pidfd'])
            item['child_pidfd'] = None
        os.close(owner['pidfd'])
        owner['pidfd'] = None
        item['joined'] = True

    def finish_success(self, generation, app_facts, endpoint, deadline, child, backend, coordinator):
        item = self.callbacks.get(generation)
        if (item is None or item['joined'] or item['child'] != child or
                item['intent']['coordinator'] != coordinator or type(backend) is not dict or
                set(backend) != {'pid', 'started', 'role'} or item['backend'] is None or
                backend['pid'] != item['backend']['pid'] or backend['role'] != item['backend']['role'] or
                type(backend['started']) is not str):
            raise JournalError('callback result differs from root-held ready identity')
        try:
            instant = datetime.datetime.fromisoformat(backend['started'].replace('Z', '+00:00'))
            if instant.tzinfo is None:
                raise ValueError('backend timestamp has no timezone')
            delta = instant.astimezone(datetime.timezone.utc) - datetime.datetime(1970, 1, 1, tzinfo=datetime.timezone.utc)
            micros = (delta.days * 86400 + delta.seconds) * 1000000 + delta.microseconds
        except (ValueError, OverflowError) as exc:
            raise JournalError('callback backend start is invalid') from exc
        if str(micros) != item['backend']['started_us']:
            raise JournalError('callback backend start changed')
        candidate = {'phase_generation': generation, 'intent': item['intent'],
                     'intent_sha256': item['intent_sha256'], 'app_facts': app_facts}
        self.settle(candidate, endpoint, deadline, None)
        name = 'callback-join-%020d.json' % generation
        raw = _read(self.life.journal._fd, name)
        return name, hashlib.sha256(raw).hexdigest()

    def abort_coordinator(self, pid, deadline):
        for item in self.callbacks.values():
            if item['intent']['coordinator']['pid'] == pid and item.get('observer') is not None:
                item['observer'].close(deadline)
                item['observer'] = None

    def close(self):
        error = None
        deadline = time.monotonic() + 15
        for item in self.callbacks.values():
            if item.get('observer') is not None:
                try:
                    item['observer'].close(deadline)
                    item['observer'] = None
                except BaseException as exc:
                    error = error or exc
            if item['child_pidfd'] is not None:
                os.close(item['child_pidfd'])
                item['child_pidfd'] = None
        for owner in self.coordinators.values():
            if owner['pidfd'] is not None:
                os.close(owner['pidfd'])
                owner['pidfd'] = None
        if error is not None:
            raise JournalError('callback observer cleanup is incomplete') from error


def _own_observer_loss(scratch, mode):
    """Kill an owned tracer process and observe EXITKILL on its owned tracee."""
    global GUARD
    directory = os.open(scratch, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    info = os.fstat(directory)
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        os.close(directory)
        raise JournalError('observer-loss probe directory is not private')
    name = 'exit-observer-loss-' + os.urandom(16).hex()
    GUARD = os.path.join(scratch, name)
    fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
    os.close(fd)
    left, right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    wait_read, wait_write = os.pipe2(os.O_CLOEXEC)
    supervisor = supervisor_fd = coordinator_fd = child_fd = None
    waited = False
    try:
        supervisor = os.fork()
        if supervisor == 0:
            coordinator = coordinator_handle = None
            try:
                left.close()
                os.close(wait_read)
                ready_read, ready_write = os.pipe2(os.O_CLOEXEC)
                arm_read, arm_write = os.pipe2(os.O_CLOEXEC)
                parent = os.getpid()
                coordinator = os.fork()
                if coordinator == 0:
                    try:
                        os.close(ready_read)
                        os.close(arm_write)
                        if _LIBC.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent:
                            os._exit(125)
                        ready = fcntl.fcntl(ready_write, fcntl.F_DUPFD_CLOEXEC, 32)
                        os.close(ready_write)
                        lock = os.open(GUARD, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
                        fcntl.flock(lock, fcntl.LOCK_EX)
                        if lock != 3:
                            os.dup2(lock, 3)
                            os.close(lock)
                        owner = os.getpid()
                        child = os.fork()
                        if child == 0:
                            right.close()
                            os.close(wait_write)
                            os.close(arm_read)
                            if _LIBC.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != owner:
                                os._exit(125)
                            if mode == 'two-thread':
                                started = threading.Event()
                                def worker():
                                    started.set()
                                    threading.Event().wait()
                                threading.Thread(target=worker, daemon=True).start()
                                if not started.wait(5):
                                    os._exit(125)
                            os.write(ready, ('%d\n' % os.getpid()).encode('ascii'))
                            os.close(ready)
                            while True:
                                signal.pause()
                        os.close(ready)
                        if os.read(arm_read, 1) != b'A' or _LIBC.prctl(1, 0, 0, 0, 0) != 0 or os.getppid() != parent:
                            os._exit(125)
                        os.close(arm_read)
                        right.send(canonical({'op': 'coordinator-armed', 'pid': owner}))
                        right.close()
                        _, status = os.waitpid(child, 0)
                        os.write(wait_write, ('%d\n' % status).encode('ascii'))
                        os.close(wait_write)
                        os._exit(0)
                    except BaseException:
                        os._exit(126)
                os.close(ready_write)
                os.close(arm_read)
                os.close(wait_write)
                coordinator_handle = os.pidfd_open(coordinator, 0)
                right.send(canonical({'op': 'coordinator', 'pid': coordinator, 'starttime': process_start(coordinator)}))
                if not select.select([ready_read], [], [], 15)[0]:
                    raise JournalError('observer-loss child did not become ready')
                raw = os.read(ready_read, 64)
                if not raw.endswith(b'\n') or not raw[:-1].isdigit():
                    raise JournalError('observer-loss child identity is malformed')
                child = int(raw[:-1])
                child_handle = os.pidfd_open(child, 0)
                actual = _identity(child)
                if actual is None or actual['parent'] != coordinator:
                    raise JournalError('observer-loss child parent differs')
                identity = {'pid': child, 'starttime': actual['starttime'], 'executable_sha256': actual['exe_sha256']}
                observer = ExitObserver(identity, child_handle, 1, hashlib.sha256(b'own-observer-loss').hexdigest())
                observer.await_attached(time.monotonic() + 15)
                right.send(canonical({'op': 'attached', 'child': identity, 'observer_root': observer.root,
                                      'observer_thread': observer.thread_identity}))
                if right.recv(16) != b'ACK':
                    raise JournalError('observer-loss controller did not hold the identities')
                os.write(arm_write, b'A')
                while True:
                    signal.pause()
            except BaseException:
                if coordinator_handle is not None:
                    try:
                        signal.pidfd_send_signal(coordinator_handle, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                os._exit(126)
        right.close()
        right = None
        os.close(wait_write)
        wait_write = None
        supervisor_fd = os.pidfd_open(supervisor, 0)
        left.settimeout(20)
        coordinator = strict_json(left.recv(4096))
        if type(coordinator) is not dict or set(coordinator) != {'op', 'pid', 'starttime'} or coordinator['op'] != 'coordinator':
            raise JournalError('observer-loss coordinator identity differs')
        coordinator_fd = os.pidfd_open(coordinator['pid'], 0)
        attached = strict_json(left.recv(4096))
        if type(attached) is not dict or set(attached) != {'op', 'child', 'observer_root', 'observer_thread'} or attached['op'] != 'attached':
            raise JournalError('observer-loss attachment evidence differs')
        child = attached['child']
        child_fd = os.pidfd_open(child['pid'], 0)
        if (attached['observer_root'] != {'pid': supervisor, 'starttime': process_start(supervisor)} or
                _tracer_pid(child['pid']) != attached['observer_thread']['pid'] or
                process_start(attached['observer_thread']['pid']) != attached['observer_thread']['starttime'] or
                process_start(coordinator['pid']) != coordinator['starttime'] or process_start(child['pid']) != child['starttime'] or
                select.select([supervisor_fd, coordinator_fd, child_fd], [], [], 0)[0]):
            raise JournalError('observer-loss held identities changed')
        left.send(b'ACK')
        armed = strict_json(left.recv(4096))
        if armed != {'op': 'coordinator-armed', 'pid': coordinator['pid']}:
            raise JournalError('observer-loss coordinator did not disable its parent-death signal')
        signal.pidfd_send_signal(supervisor_fd, signal.SIGKILL)
        _, status = os.waitpid(supervisor, 0)
        waited = True
        if not os.WIFSIGNALED(status) or os.WTERMSIG(status) != signal.SIGKILL:
            raise JournalError('observer-loss supervisor direct wait differs')
        deadline = time.monotonic() + 15
        if not select.select([child_fd], [], [], max(0, deadline - time.monotonic()))[0]:
            raise JournalError('EXITKILL did not terminate the owned tracee')
        if not select.select([wait_read], [], [], max(0, deadline - time.monotonic()))[0]:
            raise JournalError('coordinator did not directly wait the killed tracee')
        raw = os.read(wait_read, 64)
        if not raw.endswith(b'\n') or not raw[:-1].isdigit():
            raise JournalError('observer-loss child wait is malformed')
        child_status = int(raw[:-1])
        if (not os.WIFSIGNALED(child_status) or os.WTERMSIG(child_status) != signal.SIGKILL or
                not select.select([coordinator_fd], [], [], max(0, deadline - time.monotonic()))[0]):
            raise JournalError('EXITKILL tracee or coordinator did not settle')
        competitor = os.open(GUARD, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        finally:
            os.close(competitor)
        return {'version': 1, 'mode': mode, 'scenario': 'observer-loss', 'attachment': attached,
                'supervisor_direct_wait': True, 'supervisor_wait_status': status, 'coordinator_pidfd_exited': True,
                'child_direct_wait_by_coordinator': True, 'child_wait_status': child_status,
                'child_direct_wait_by_root': False, 'guard_acquired_after_exit': True}
    finally:
        for held in (supervisor_fd, coordinator_fd, child_fd):
            if held is not None and not select.select([held], [], [], 0)[0]:
                try:
                    signal.pidfd_send_signal(held, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        if supervisor is not None and not waited and supervisor_fd is not None and select.select([supervisor_fd], [], [], 15)[0]:
            os.waitpid(supervisor, 0)
        left.close()
        if right is not None:
            right.close()
        for held in (wait_read, wait_write, supervisor_fd, coordinator_fd, child_fd):
            if held is not None:
                os.close(held)
        os.unlink(name, dir_fd=directory)
        os.close(directory)


def _probe_signal_identity(pid, tid):
    before = process_start(tid)
    rows = [line.partition(':')[2].strip() for line in
            read_kernel('/proc/%d/task/%d/status' % (pid, tid), 65536).splitlines()
            if line.startswith('SigBlk:')]
    if len(rows) != 1 or re.fullmatch(r'[0-9a-f]{16}', rows[0]) is None or process_start(tid) != before:
        raise JournalError('own-process probe signal mask identity differs')
    return {'tid': tid, 'starttime': before, 'sigblk': rows[0]}


def _probe_thread_response(channel):
    challenge, ancillary, flags, _ = channel.recvmsg(33, 0)
    if ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or len(challenge) != 32:
        raise JournalError('own-process probe challenge differs')
    tid = threading.get_native_id().to_bytes(8, 'big')
    response = tid + hashlib.sha256(b'HOUFENG-C80-THREAD-RESUME-V1\0' + tid + challenge).digest()
    if channel.send(response) != len(response):
        raise JournalError('own-process probe thread response was short')


def _probe_read_response(channel, challenge, tid, deadline):
    if not select.select([channel], [], [], max(0, deadline - time.monotonic()))[0]:
        raise JournalError('own-process probe thread did not resume')
    response, ancillary, flags, _ = channel.recvmsg(41, 0)
    identity = tid.to_bytes(8, 'big')
    expected = identity + hashlib.sha256(b'HOUFENG-C80-THREAD-RESUME-V1\0' + identity + challenge).digest()
    if ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or response != expected:
        raise JournalError('own-process probe thread response differs')
    return response.hex()


def _own_process_probe(scratch, mode, scenario):
    """Observe only the process tree created by this bounded capability probe."""
    global GUARD
    cases = ('coordinator-kill', 'child-kill', 'normal', 'signal', 'group-stop', 'already-traced', 'cancel', 'deadline', 'observer-loss')
    if os.geteuid() != 0 or mode not in ('single', 'two-thread') or scenario not in cases:
        raise JournalError('own-process probe selector or privilege differs')
    if scenario == 'observer-loss':
        return _own_observer_loss(scratch, mode)
    directory = os.open(scratch, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    info = os.fstat(directory)
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        os.close(directory)
        raise JournalError('own-process probe directory is not private')
    name = 'exit-observer-' + os.urandom(16).hex()
    GUARD = os.path.join(scratch, name)
    guard_fd = os.open(name, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
    guard_info = os.fstat(guard_fd)
    guard = {'dev': guard_info.st_dev, 'ino': guard_info.st_ino}
    os.close(guard_fd)
    read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
    command_read, command_write = os.pipe2(os.O_CLOEXEC)
    wait_read, wait_write = os.pipe2(os.O_CLOEXEC)
    coordinator = coordinator_fd = child_fd = observer = None
    waited = False
    leader_root = leader_child = worker_root = worker_child = None
    try:
        if scenario == 'group-stop':
            leader_root, leader_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
            if mode == 'two-thread':
                worker_root, worker_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
        coordinator = os.fork()
        if coordinator == 0:
            try:
                os.close(read_fd)
                os.close(command_write)
                os.close(wait_read)
                for channel in (leader_root, worker_root):
                    if channel is not None:
                        channel.close()
                notify = fcntl.fcntl(write_fd, fcntl.F_DUPFD_CLOEXEC, 32)
                os.close(write_fd)
                lock = os.open(GUARD, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
                fcntl.flock(lock, fcntl.LOCK_EX)
                if lock != 3:
                    os.dup2(lock, 3)
                    os.close(lock)
                parent = os.getpid()
                child = os.fork()
                if child == 0:
                    os.close(wait_write)
                    if _LIBC.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent:
                        os._exit(125)
                    signal.pthread_sigmask(signal.SIG_UNBLOCK, {signal.SIGUSR1, signal.SIGCONT})
                    signal.signal(signal.SIGUSR1, lambda _signum, _frame: None)
                    worker_thread = None
                    if mode == 'two-thread':
                        started, worker_done, worker_failed = threading.Event(), threading.Event(), threading.Event()
                        def worker():
                            try:
                                signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGUSR1, signal.SIGCONT})
                                started.set()
                                if scenario == 'group-stop':
                                    _probe_thread_response(worker_child)
                                    worker_done.set()
                                else:
                                    threading.Event().wait()
                            except BaseException:
                                worker_failed.set()
                                started.set()
                        worker_thread = threading.Thread(target=worker, daemon=True)
                        worker_thread.start()
                        if not started.wait(5) or worker_failed.is_set():
                            os._exit(125)
                    os.write(notify, ('%d\n' % os.getpid()).encode('ascii'))
                    os.close(notify)
                    if scenario == 'group-stop':
                        _probe_thread_response(leader_child)
                        if worker_thread is not None:
                            worker_thread.join(20)
                            if worker_thread.is_alive() or worker_failed.is_set() or not worker_done.is_set():
                                os._exit(125)
                        os._exit(0)
                    os._exit(0 if os.read(command_read, 1) == b'Q' else 125)
                os.close(notify)
                os.close(command_read)
                for channel in (leader_child, worker_child):
                    if channel is not None:
                        channel.close()
                if scenario == 'group-stop':
                    stopped_pid, stopped_status = os.waitpid(child, os.WUNTRACED)
                    if stopped_pid != child or not os.WIFSTOPPED(stopped_status) or os.WSTOPSIG(stopped_status) != signal.SIGSTOP:
                        raise JournalError('own-process probe real parent did not wait for group stop')
                    stopped_record = ('stop:%d\n' % stopped_status).encode('ascii')
                    if os.write(wait_write, stopped_record) != len(stopped_record):
                        raise JournalError('own-process probe stop receipt was short')
                _, child_status = os.waitpid(child, 0)
                os.write(wait_write, ('%d\n' % child_status).encode('ascii'))
                os.close(wait_write)
                os._exit(0)
            except BaseException:
                os._exit(126)
        os.close(write_fd)
        write_fd = None
        os.close(command_read)
        command_read = None
        os.close(wait_write)
        wait_write = None
        for channel in (leader_child, worker_child):
            if channel is not None:
                channel.close()
        leader_child = worker_child = None
        coordinator_fd = os.pidfd_open(coordinator, 0)
        deadline = time.monotonic() + 30
        if not select.select([read_fd], [], [], max(0, deadline - time.monotonic()))[0]:
            raise JournalError('own-process probe child did not report readiness')
        raw = os.read(read_fd, 64)
        if not raw.endswith(b'\n') or not raw[:-1].isdigit():
            raise JournalError('own-process probe child identity is malformed')
        child = int(raw[:-1])
        child_fd = os.pidfd_open(child, 0)
        actual = _identity(child)
        coordinator_identity = {'pid': coordinator, 'starttime': process_start(coordinator)}
        if actual is None or actual['parent'] != coordinator:
            raise JournalError('own-process probe child is not the direct descendant')
        child_identity = {'pid': child, 'starttime': actual['starttime'], 'executable_sha256': actual['exe_sha256']}
        task_dir = '/proc/%d/task' % child
        tasks = sorted(os.listdir(task_dir))
        threads = len(tasks)
        if (threads != (1 if mode == 'single' else 2) or any(not tid.isdecimal() for tid in tasks) or
                str(child) not in tasks):
            raise JournalError('own-process probe thread inventory differs')
        leader = _probe_signal_identity(child, child)
        other_tids = [int(tid) for tid in tasks if int(tid) != child]
        worker_identity = _probe_signal_identity(child, other_tids[0]) if other_tids else None
        controlled = (1 << (signal.SIGUSR1 - 1)) | (1 << (signal.SIGCONT - 1))
        if (sorted(os.listdir(task_dir)) != tasks or leader['starttime'] != child_identity['starttime'] or
                int(leader['sigblk'], 16) & controlled or
                worker_identity is not None and int(worker_identity['sigblk'], 16) & controlled != controlled):
            raise JournalError('own-process probe signal eligibility differs')
        eligibility = {'policy': 'leader_only_SIGUSR1_SIGCONT', 'leader': leader, 'worker': worker_identity}
        observer = ExitObserver(child_identity, child_fd, 1, hashlib.sha256(b'own-process-probe').hexdigest())
        observer.await_attached(deadline)
        observer_identity = {'observer_root': observer.root, 'observer_thread': observer.thread_identity}
        proof = child_status = second_rejection = group_stop = None
        signal_events, group_events = [], []
        if scenario == 'coordinator-kill':
            observer.arm(coordinator_identity, guard, deadline)
            signal.pidfd_send_signal(coordinator_fd, signal.SIGKILL)
            if not select.select([coordinator_fd], [], [], max(0, deadline - time.monotonic()))[0]:
                raise JournalError('own-process probe coordinator did not exit')
            proof = observer.exit_stop(coordinator_fd, guard, deadline)
            proof.update(observer.resume_and_join(deadline))
        else:
            if scenario == 'already-traced':
                other = ExitObserver(child_identity, child_fd, 2, hashlib.sha256(b'other-tracer-probe').hexdigest())
                try:
                    try:
                        other.await_attached(deadline)
                    except JournalError as exc:
                        if not isinstance(exc.__cause__, ObserverAlreadyTraced) or _tracer_pid(child) != observer.thread_identity['pid']:
                            raise
                        second_rejection = {'reason': 'already_traced', 'tracer_tid': observer.thread_identity['pid']}
                    else:
                        raise JournalError('a second tracer was incorrectly accepted')
                finally:
                    other.close(deadline)
            if scenario == 'signal':
                signal.pidfd_send_signal(child_fd, signal.SIGUSR1)
                observer._wait_for(lambda: signal.SIGUSR1 in observer.delivered_signals, deadline)
            if scenario == 'group-stop':
                signal.pidfd_send_signal(child_fd, signal.SIGSTOP)
                observer._wait_for(lambda: signal.SIGSTOP in observer.group_stops, deadline)
                ready, _, _ = select.select([wait_read, coordinator_fd], [], [], max(0, deadline - time.monotonic()))
                if wait_read not in ready:
                    raise JournalError('own-process probe real parent stop receipt missing')
                stop_raw = os.read(wait_read, 64)
                if not stop_raw.startswith(b'stop:') or not stop_raw.endswith(b'\n') or not stop_raw[5:-1].isdigit():
                    raise JournalError('own-process probe real parent stop receipt malformed')
                stop_status = int(stop_raw[5:-1])
                if not os.WIFSTOPPED(stop_status) or os.WSTOPSIG(stop_status) != signal.SIGSTOP:
                    raise JournalError('own-process probe real parent stop status differs')
                pending = [('leader', leader_root, leader, os.urandom(32))]
                if worker_identity is not None:
                    pending.append(('worker', worker_root, worker_identity, os.urandom(32)))
                if len(pending) == 2 and pending[0][3] == pending[1][3]:
                    raise JournalError('own-process probe thread challenges collide')
                for _, channel, _, challenge in pending:
                    if channel.send(challenge) != len(challenge):
                        raise JournalError('own-process probe challenge was short')
                signal.pidfd_send_signal(child_fd, signal.SIGCONT)
                observer._wait_for(lambda: signal.SIGCONT in observer.delivered_signals, deadline)
                replies = [{'role': role, 'tid': thread['tid'], 'challenge': challenge.hex(),
                            'response': _probe_read_response(channel, challenge, thread['tid'], deadline)}
                           for role, channel, thread, challenge in pending]
                group_stop = {'parent_wait_status': stop_status, 'challenges': replies}
            if scenario == 'deadline':
                observer.arm(coordinator_identity, guard, deadline)
                try:
                    observer._wait_for(lambda: observer.stopped is not None, time.monotonic())
                except ObserverDeadline:
                    pass
                else:
                    raise JournalError('expired observer deadline was accepted')
            if scenario == 'child-kill':
                signal.pidfd_send_signal(child_fd, signal.SIGKILL)
            elif scenario in ('cancel', 'deadline'):
                observer.close(deadline)
                signal_events, group_events = list(observer.delivered_signals), list(observer.group_stops)
                observer = None
            elif scenario != 'group-stop':
                if os.write(command_write, b'Q') != 1:
                    raise JournalError('own-process probe release was not delivered')
            if scenario not in ('cancel', 'deadline'):
                observer.resume_and_join(deadline)
                signal_events, group_events = list(observer.delivered_signals), list(observer.group_stops)
            if not select.select([wait_read, coordinator_fd], [], [], max(0, deadline - time.monotonic()))[0]:
                raise JournalError('own-process probe direct child wait was not delivered')
            child_wait = os.read(wait_read, 64)
            if not child_wait.endswith(b'\n') or not child_wait[:-1].isdigit():
                raise JournalError('own-process probe direct child wait is malformed')
            child_status = int(child_wait[:-1])
            if scenario in ('child-kill', 'cancel', 'deadline'):
                if not os.WIFSIGNALED(child_status) or os.WTERMSIG(child_status) != signal.SIGKILL:
                    raise JournalError('own-process probe child kill was not directly waited')
            elif not os.WIFEXITED(child_status) or os.WEXITSTATUS(child_status) != 0:
                raise JournalError('own-process probe normal child exit differs')
        _, status = os.waitpid(coordinator, 0)
        waited = True
        if scenario == 'coordinator-kill':
            if not os.WIFSIGNALED(status) or os.WTERMSIG(status) != signal.SIGKILL:
                raise JournalError('own-process probe coordinator wait differs')
        elif not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            raise JournalError('own-process probe coordinator did not directly wait its child')
        competitor = os.open(GUARD, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        finally:
            os.close(competitor)
        return {'version': 1, 'mode': mode, 'scenario': scenario, 'threads': threads, 'exit_stop': proof,
                'child': child_identity, 'coordinator': coordinator_identity, **observer_identity,
                'delivered_signals': signal_events, 'group_stops': group_events, 'second_tracer_rejection': second_rejection,
                'signal_eligibility': eligibility, 'group_stop_evidence': group_stop,
                'deadline_outcome': 'expired' if scenario == 'deadline' else None,
                'coordinator_wait_status': status, 'child_wait_status': child_status,
                'guard_acquired_after_exit': True, 'coordinator_direct_wait': True, 'child_direct_wait_by_root': False}
    finally:
        if coordinator_fd is not None and not select.select([coordinator_fd], [], [], 0)[0]:
            try:
                signal.pidfd_send_signal(coordinator_fd, signal.SIGKILL)
            except ProcessLookupError:
                pass
        if observer is not None:
            observer.close(time.monotonic() + 15)
        if coordinator is not None and not waited:
            if coordinator_fd is not None and select.select([coordinator_fd], [], [], 15)[0]:
                os.waitpid(coordinator, 0)
        for fd in (read_fd, write_fd, command_read, command_write, wait_read, wait_write, coordinator_fd, child_fd):
            if fd is not None:
                os.close(fd)
        for channel in (leader_root, leader_child, worker_root, worker_child):
            if channel is not None:
                channel.close()
        os.unlink(name, dir_fd=directory)
        os.close(directory)


if __name__ == '__main__':
    if len(sys.argv) != 5 or sys.argv[1] != '--own-process-probe':
        raise SystemExit('usage: callback_lifecycle.py --own-process-probe PRIVATE_DIR single|two-thread SCENARIO')
    print(json.dumps(_own_process_probe(sys.argv[2], sys.argv[3], sys.argv[4]), sort_keys=True))
