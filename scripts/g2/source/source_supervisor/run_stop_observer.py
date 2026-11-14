"""Continuous, inode-bound observation of the fixed multipathd stop window.

This observer grants no pathname or systemd authority.  Its caller supplies the
already-held directory and PID-file descriptors and authenticates the unit.
"""
import copy
import ctypes
import hashlib
import json
import os
import platform
import re
import select
import signal
import stat
import struct
import threading
import time
from dataclasses import dataclass

from callback_lifecycle import _trace, _tracer_pid
from journal import JournalError, process_start

_LIBC = ctypes.CDLL(None, use_errno=True)
_LIBC.fanotify_init.argtypes = (ctypes.c_uint, ctypes.c_uint)
_LIBC.fanotify_init.restype = ctypes.c_int
_LIBC.fanotify_mark.argtypes = (ctypes.c_int, ctypes.c_uint, ctypes.c_uint64, ctypes.c_int, ctypes.c_char_p)
_LIBC.fanotify_mark.restype = ctypes.c_int
_LIBC.name_to_handle_at.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_void_p,
                                   ctypes.POINTER(ctypes.c_int), ctypes.c_int)
_LIBC.name_to_handle_at.restype = ctypes.c_int
_LIBC.fstatfs.argtypes = (ctypes.c_int, ctypes.c_void_p)
_LIBC.fstatfs.restype = ctypes.c_int

_CREATE, _DELETE, _MOVED_FROM, _MOVED_TO = 0x100, 0x200, 0x40, 0x80
_MODIFY, _ATTRIB, _DELETE_SELF, _MOVE_SELF = 2, 4, 0x400, 0x800
_OVERFLOW, _ONDIR, _ON_CHILD = 0x4000, 0x40000000, 0x08000000
_DIR_MASK = (_CREATE | _DELETE | _MOVED_FROM | _MOVED_TO | _ATTRIB | _MODIFY |
             _DELETE_SELF | _MOVE_SELF | _ON_CHILD | _ONDIR)
_FILE_MASK = _MODIFY | _ATTRIB | _DELETE_SELF | _MOVE_SELF
_EVENT_MASK = _DIR_MASK & ~_ON_CHILD
_INIT_FLAGS = 0x1 | 0x2 | 0x80 | 0x200 | 0x400 | 0x800 | 0x1000
_AT_EMPTY_PATH, _AT_HANDLE_FID = 0x1000, 0x200
_WALL = 0x40000000
_SEIZE, _INTERRUPT, _CONT, _LISTEN, _GETEVENTMSG = 0x4206, 0x4207, 7, 0x4208, 0x4201
_TRACE_OPTIONS = 0x40 | 0x10 | 0x8 | 0x4 | 0x2  # exit, exec, clone, vfork, fork
_EVENT_EXIT, _EVENT_STOP = 6, 128
_GROUP_STOPS = frozenset((signal.SIGSTOP, signal.SIGTSTP, signal.SIGTTIN, signal.SIGTTOU))
_FIELDS = ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtime_ns', 'ctime_ns')


def _checked(result, operation):
    if result < 0:
        error = ctypes.get_errno()
        raise OSError(error, operation + ': ' + os.strerror(error))
    return result


def _identity(info):
    return {key: getattr(info, 'st_' + key) for key in _FIELDS}


def _snapshot(fd):
    before = _identity(os.fstat(fd))
    names = os.listdir(fd)
    if len(names) != len(set(names)):
        raise JournalError('duplicate directory entry')
    entries = {}
    for name in names:
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        item = _identity(info)
        if stat.S_ISLNK(info.st_mode):
            item['target'] = os.readlink(name, dir_fd=fd)
        entries[name] = item
    if _identity(os.fstat(fd)) != before or set(os.listdir(fd)) != set(names):
        raise JournalError('directory changed during snapshot')
    return before, entries


def _handle(fd):
    # x86-64 statfs and name_to_handle_at identify the same filesystem and inode
    # used by the fanotify FID record. AT_EMPTY_PATH binds the held descriptor.
    buf = ctypes.create_string_buffer(136)
    struct.pack_into('=I', buf, 0, 128)
    mount = ctypes.c_int()
    _checked(_LIBC.name_to_handle_at(fd, b'', buf, ctypes.byref(mount),
                                     _AT_EMPTY_PATH | _AT_HANDLE_FID), 'name_to_handle_at')
    size, kind = struct.unpack_from('=Ii', buf.raw)
    if not 0 < size <= 128 or mount.value <= 0:
        raise JournalError('file handle is invalid')
    fs = ctypes.create_string_buffer(256)
    _checked(_LIBC.fstatfs(fd, fs), 'fstatfs')
    fsid = fs.raw[56:64]
    if fsid == bytes(8):
        raise JournalError('filesystem identity is unavailable')
    return fsid, kind, buf.raw[8:8 + size]


def _handle_digest(handle):
    fsid, kind, data = handle
    return hashlib.sha256(fsid + struct.pack('=iI', kind, len(data)) + data).hexdigest()


def _close_event_fds(raw):
    # A single read installs descriptors for every complete record at once.
    # Scan the entire structurally traversable batch even when validation of an
    # earlier record fails, so later PIDFDs are not leaked.
    descriptors = set()
    pos = 0
    while len(raw) - pos >= 24:
        length, _, _, header, _, fd, _ = struct.unpack_from('=IBBHQii', raw, pos)
        if fd >= 0:
            descriptors.add(fd)
        if length < 24 or pos + length > len(raw) or header < 24 or header > length:
            break
        offset, end = pos + header, pos + length
        while end - offset >= 4:
            kind, _, size = struct.unpack_from('=BBH', raw, offset)
            if size < 4 or offset + size > end:
                break
            if kind == 4 and size == 8:
                value = struct.unpack_from('=i', raw, offset + 4)[0]
                if value >= 0:
                    descriptors.add(value)
            offset += size
        pos = end
    for fd in descriptors:
        try:
            os.close(fd)
        except OSError:
            pass


def _parse_events(raw):
    """Decode native fanotify records. Caller owns returned PIDFDs.

    On failure this closes descriptors in structurally traversable records.
    Synthetic callers must supply owned descriptors as well.
    """
    records = []
    try:
        pos = 0
        while pos < len(raw):
            if len(raw) - pos < 24:
                raise JournalError('truncated fanotify metadata')
            length, version, reserved, header, mask, fd, pid = struct.unpack_from('=IBBHQii', raw, pos)
            if (version != 3 or reserved != 0 or header != 24 or length < header or
                    pos + length > len(raw) or fd != -1 or not mask or mask & _OVERFLOW or
                    mask & ~_EVENT_MASK):
                raise JournalError('invalid fanotify metadata')
            infos = {}
            offset, end = pos + header, pos + length
            while offset < end:
                if end - offset < 4:
                    raise JournalError('truncated fanotify information')
                kind, pad, size = struct.unpack_from('=BBH', raw, offset)
                if pad or size < 4 or offset + size > end or kind not in (1, 2, 3, 4) or kind in infos:
                    raise JournalError('invalid fanotify information')
                part = raw[offset:offset + size]
                if kind == 4:
                    if size != 8:
                        raise JournalError('invalid fanotify pidfd record')
                    value = struct.unpack_from('=i', part, 4)[0]
                    if value < 0:
                        raise JournalError('fanotify event lacks pidfd')
                    infos[kind] = value
                else:
                    if size < 20:
                        raise JournalError('invalid fanotify fid record')
                    handle_bytes, handle_type = struct.unpack_from('=Ii', part, 12)
                    stop = 20 + handle_bytes
                    if not 0 < handle_bytes <= 128 or stop > size:
                        raise JournalError('invalid fanotify file handle')
                    handle = (part[4:12], handle_type, part[20:stop])
                    if kind == 2:
                        rest = part[stop:]
                        nul = rest.find(b'\0')
                        if nul < 0 or any(rest[nul + 1:]) or not rest[:nul] or b'/' in rest[:nul]:
                            raise JournalError('invalid fanotify name')
                        infos[kind] = (handle, rest[:nul])
                    else:
                        if any(part[stop:]):
                            raise JournalError('invalid fanotify handle padding')
                        infos[kind] = handle
                offset += size
            if offset != end or 4 not in infos or pid <= 1:
                raise JournalError('incomplete fanotify event')
            records.append((mask, pid, infos, raw[pos:end]))
            pos = end
        if pos != len(raw) or not records:
            raise JournalError('empty or incomplete fanotify read')
        return tuple(records)
    except BaseException:
        _close_event_fds(raw)
        raise


def _pidfd_pid(fd):
    with open('/proc/self/fdinfo/%d' % fd, 'rb') as stream:
        lines = stream.read(65537).splitlines()
    values = [line.split(b':', 1)[1].strip() for line in lines if line.startswith(b'Pid:')]
    if len(values) != 1 or not values[0].isdigit():
        raise JournalError('event pidfd has no live process identity')
    return int(values[0])


def _file_bytes(fd, size):
    data = os.pread(fd, size + 1, 0)
    if len(data) != size:
        raise JournalError('PID file size changed')
    return data


@dataclass(frozen=True)
class _RunStopProof:
    _owner: object
    _encoded: bytes

    def _field(self, name):
        return json.loads(self._encoded)[name]

    @property
    def actor(self): return self._field('actor')
    @property
    def before(self): return self._field('before')
    @property
    def after(self): return self._field('after')
    @property
    def pid_file(self): return self._field('pid_file')
    @property
    def event(self): return self._field('event')
    @property
    def settlement(self): return self._field('settlement')


class RunStopObserver:
    """Keep the kernel marks active through proof consumption and final drain."""

    def __init__(self, run_fd, pid_fd, actor, deadline):
        if platform.system() != 'Linux' or platform.machine() != 'x86_64' or os.geteuid() != 0:
            raise JournalError('stop observer requires isolated x86-64 root Linux')
        if (type(actor) is not dict or type(actor.get('pid')) is not int or actor['pid'] <= 1 or
                type(actor.get('starttime')) is not int or actor['starttime'] <= 0 or
                not isinstance(actor.get('exe_sha256'), str) or
                not re.fullmatch(r'[0-9a-f]{64}', actor['exe_sha256']) or
                actor['exe_sha256'] == '0' * 64):
            raise JournalError('invalid stop actor')
        self.run_fd = os.dup(run_fd)
        try:
            self.file_fd = os.dup(pid_fd)
            self.pidfd = os.pidfd_open(actor['pid'], 0)
        except BaseException:
            os.close(self.run_fd)
            if hasattr(self, 'file_fd'):
                os.close(self.file_fd)
            raise
        self.actor = copy.deepcopy(actor)
        self.deadline = deadline
        self.fan_fd = None
        self.condition = threading.Condition()
        self.read_lock = threading.RLock()
        self.stop_reader = threading.Event()
        self.reader = self.tracer = None
        self.error = None
        self.tracer_ready = False
        self.resume = False
        self.trace_done = False
        self.window_started = False
        self.final_window = False
        self.cancelled = False
        self.terminal = {}
        self.tasks = {}
        self.delete = False
        self.count = self.byte_count = 0
        self.raw_hash = hashlib.sha256()
        self.finished = self.used = self.closed = False
        self.proof = None

    def _remaining(self):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise JournalError('stop observer deadline exceeded')
        return remaining

    def _raise_error(self):
        if self.error is not None:
            raise JournalError('stop observer failed') from self.error
        if self.cancelled:
            raise JournalError('stop observer cancelled')

    def _fail(self, exc):
        with self.condition:
            if self.error is None:
                self.error = exc
            self.condition.notify_all()

    def _wait(self, predicate):
        with self.condition:
            while not predicate():
                self._raise_error()
                self.condition.wait(min(self._remaining(), 0.05))
            self._raise_error()

    def _drain(self, initial=False, final=False):
        with self.read_lock:
            initial = initial or not self.window_started
            final = final or self.final_window
            while True:
                try:
                    raw = os.read(self.fan_fd, 65536)
                except BlockingIOError:
                    return
                if not raw:
                    raise JournalError('fanotify stream ended')
                events = _parse_events(raw)
                try:
                    for mask, pid, infos, event_raw in events:
                        self.count += 1
                        self.byte_count += len(event_raw)
                        if self.count > 256 or self.byte_count > 65536 or initial or final:
                            raise JournalError('unexpected or excessive fanotify event')
                        event_fd = infos[4]
                        if (pid != self.actor['pid'] or _pidfd_pid(event_fd) != pid or
                                process_start(pid) != self.actor['starttime'] or
                                select.select([event_fd], [], [], 0)[0]):
                            raise JournalError('fanotify actor identity differs')
                        directory = infos.get(2)
                        target = infos.get(1)
                        if mask == _DELETE:
                            if (self.delete or directory != (self.dir_handle, b'multipathd.pid') or
                                    target != self.file_handle or set(infos) != {1, 2, 4}):
                                raise JournalError('unexpected directory deletion')
                            self.delete = True
                        elif mask & ~(_ATTRIB | _DELETE_SELF) or not mask:
                            raise JournalError('unexpected stop-window filesystem event')
                        elif target != self.file_handle or (directory is not None and
                                directory != (self.dir_handle, b'multipathd.pid')) or 3 in infos:
                            raise JournalError('unexpected PID inode event')
                        self.raw_hash.update(event_raw)
                finally:
                    for _, _, infos, _ in events:
                        try:
                            os.close(infos[4])
                        except OSError:
                            pass

    def _read_loop(self):
        try:
            while not self.stop_reader.is_set():
                if select.select([self.fan_fd], [], [], min(self._remaining(), 0.05))[0]:
                    self._drain()
        except BaseException as exc:
            self._fail(exc)

    def _task_list(self):
        names = os.listdir('/proc/%d/task' % self.actor['pid'])
        if not names or any(not name.isdecimal() for name in names):
            raise JournalError('invalid stop actor task inventory')
        return sorted(int(name) for name in names)

    def _trace_loop(self):
        seized = set()
        try:
            tid = threading.get_native_id()
            tasks = self._task_list()
            starts = {task: process_start(task) for task in tasks}
            for task in tasks:
                self._raise_error()
                if _tracer_pid(task) != 0:
                    raise JournalError('stop actor already traced')
                _trace(_SEIZE, task, _TRACE_OPTIONS)
                seized.add(task)
                self.tasks[task] = starts[task]
                self._raise_error()
                _trace(_INTERRUPT, task)
            stopped = set()
            while stopped != seized:
                self._remaining()
                for task in sorted(seized - stopped):
                    observed, status = os.waitpid(task, os.WNOHANG | _WALL)
                    if not observed:
                        continue
                    if not os.WIFSTOPPED(status) or status >> 16 != _EVENT_STOP or os.WSTOPSIG(status) != signal.SIGTRAP:
                        raise JournalError('actor changed before tracer arm')
                    stopped.add(task)
                if stopped != seized:
                    time.sleep(0.005)
            if (self._task_list() != tasks or any(process_start(task) != starts[task] or
                    _tracer_pid(task) != tid for task in tasks)):
                raise JournalError('actor task inventory changed during tracer arm')
            from origins import _identity as process_identity
            if process_identity(self.actor['pid'], full=False) != self.actor:
                raise JournalError('live actor executable or identity changed')
            with self.condition:
                self.tracer_ready = True
                self.condition.notify_all()
                while not self.resume:
                    self.condition.wait(min(self._remaining(), 0.05))
                    self._raise_error()
            for task in tasks:
                _trace(_CONT, task)
            while seized:
                self._remaining()
                self._raise_error()
                progressed = False
                for task in sorted(tuple(seized)):
                    observed, status = os.waitpid(task, os.WNOHANG | _WALL)
                    if not observed:
                        continue
                    progressed = True
                    if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                        self.terminal[task] = status
                        seized.remove(task)
                        continue
                    if not os.WIFSTOPPED(status):
                        raise JournalError('unknown tracer wait status')
                    event, sig = status >> 16, os.WSTOPSIG(status)
                    if event in (1, 2, 3):
                        child = ctypes.c_ulong()
                        _trace(_GETEVENTMSG, task, ctypes.addressof(child))
                        if child.value > 1:
                            seized.add(child.value)
                            self.tasks[child.value] = process_start(child.value)
                        raise JournalError('actor created a new task')
                    if event == 4:
                        raise JournalError('actor executed a program')
                    if event == _EVENT_EXIT and sig == signal.SIGTRAP:
                        self._drain()
                        self._raise_error()
                        if task == self.actor['pid'] and not self.delete:
                            raise JournalError('leader exited without authenticated deletion')
                        _trace(_CONT, task)
                    elif event == _EVENT_STOP and sig in _GROUP_STOPS:
                        _trace(_LISTEN, task)
                    elif event == _EVENT_STOP and sig == signal.SIGTRAP:
                        _trace(_CONT, task)
                    elif event == 0:
                        _trace(_CONT, task, sig)
                    else:
                        raise JournalError('unexpected actor ptrace event')
                if not progressed:
                    time.sleep(0.005)
        except BaseException as exc:
            self._fail(exc)
            # Only the pinned leader and explicitly auto-attached children are
            # eligible for termination. Never signal an unrelated recycled PID.
            if seized:
                try:
                    signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
                except (OSError, ProcessLookupError):
                    pass
            for task in tuple(seized):
                try:
                    if process_start(task) == self.tasks[task]:
                        os.kill(task, signal.SIGKILL)
                        _trace(_CONT, task, signal.SIGKILL)
                except (OSError, ProcessLookupError, KeyError):
                    pass
            while seized and time.monotonic() < self.deadline:
                for task in tuple(seized):
                    try:
                        observed, status = os.waitpid(task, os.WNOHANG | _WALL)
                        if observed and (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                            self.terminal[task] = status
                            seized.remove(task)
                        elif observed and os.WIFSTOPPED(status):
                            if status >> 16 in (1, 2, 3):
                                child = ctypes.c_ulong()
                                _trace(_GETEVENTMSG, task, ctypes.addressof(child))
                                if child.value > 1:
                                    seized.add(child.value)
                                    self.tasks[child.value] = process_start(child.value)
                                    os.kill(child.value, signal.SIGKILL)
                            _trace(_CONT, task, signal.SIGKILL)
                    except (ChildProcessError, ProcessLookupError):
                        # Lost wait ownership is not successful settlement.
                        seized.remove(task)
                    except OSError:
                        pass
                if seized:
                    time.sleep(0.005)
            if seized:
                self._fail(JournalError('traced tasks did not settle'))
        finally:
            with self.read_lock:
                self.final_window = True
            with self.condition:
                self.trace_done = True
                self.condition.notify_all()

    def _arm(self):
        if self.fan_fd is not None or self.closed:
            raise JournalError('stop observer cannot be rearmed')
        self._remaining()
        before = _identity(os.fstat(self.file_fd))
        named = _identity(os.stat('multipathd.pid', dir_fd=self.run_fd, follow_symlinks=False))
        if (before != named or not stat.S_ISREG(before['mode']) or before['uid'] != 0 or
                before['gid'] != 0 or stat.S_IMODE(before['mode']) != 0o644 or
                before['nlink'] != 1 or not 1 <= before['size'] <= 32):
            raise JournalError('PID file identity invalid')
        self.file_before = before
        self.file_data = _file_bytes(self.file_fd, before['size'])
        if not re.fullmatch(rb'[0-9]+\n?', self.file_data) or int(self.file_data.strip()) != self.actor['pid']:
            raise JournalError('PID file contents differ from actor')
        self.dir_handle, self.file_handle = _handle(self.run_fd), _handle(self.file_fd)
        self.fan_fd = _checked(_LIBC.fanotify_init(_INIT_FLAGS, os.O_RDONLY | os.O_CLOEXEC), 'fanotify_init')
        try:
            for fd, mask in ((self.run_fd, _DIR_MASK), (self.file_fd, _FILE_MASK)):
                path = ('/proc/self/fd/%d' % fd).encode('ascii')
                _checked(_LIBC.fanotify_mark(self.fan_fd, 1, mask, -100, path), 'fanotify_mark')
            self.before, self.entries = _snapshot(self.run_fd)
            if self.entries.get('multipathd.pid') != before or _identity(os.fstat(self.file_fd)) != before:
                raise JournalError('PID file changed during initial observation')
            self._drain(initial=True)
            if select.select([self.pidfd], [], [], 0)[0] or process_start(self.actor['pid']) != self.actor['starttime']:
                raise JournalError('actor exited before observation')
            self.reader = threading.Thread(target=self._read_loop, name='run-stop-events', daemon=False)
            self.reader.start()
            self.tracer = threading.Thread(target=self._trace_loop, name='run-stop-tracer', daemon=False)
            self.tracer.start()
            self._wait(lambda: self.tracer_ready)
            with self.read_lock:
                self._drain(initial=True)
                if _snapshot(self.run_fd) != (self.before, self.entries):
                    raise JournalError('directory changed before stop')
                self._drain(initial=True)
                self.window_started = True
            with self.condition:
                self.resume = True
                self.condition.notify_all()
        except BaseException:
            raise

    def arm(self):
        try:
            self._arm()
        except BaseException:
            try:
                self.close()
            except BaseException:
                pass
            raise

    def finish(self):
        if self.closed or self.finished or self.tracer is None:
            raise JournalError('stop observer cannot finish')
        self._wait(lambda: self.trace_done)
        self.tracer.join(self._remaining())
        if self.tracer.is_alive() or not select.select([self.pidfd], [], [], self._remaining())[0]:
            raise JournalError('stop actor or tracer did not settle')
        status = self.terminal.get(self.actor['pid'])
        if (status is None or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0 or
                set(self.terminal) != set(self.tasks) or not self.delete):
            raise JournalError('stop actor termination was not successful')
        self._drain()
        after, entries = _snapshot(self.run_fd)
        expected = dict(self.entries)
        del expected['multipathd.pid']
        if entries != expected or any(after[k] != self.before[k] for k in ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink')):
            raise JournalError('directory delta is not the authorized deletion')
        current = _identity(os.fstat(self.file_fd))
        if (current['nlink'] != 0 or any(current[k] != self.file_before[k] for k in
                ('dev', 'ino', 'mode', 'uid', 'gid', 'size', 'mtime_ns')) or
                _file_bytes(self.file_fd, current['size']) != self.file_data):
            raise JournalError('unlinked PID file identity changed')
        self._drain(final=True)
        self._raise_error()
        pid_file = {key: self.file_before[key] for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'size', 'mtime_ns')}
        pid_file.update(ctime_before_ns=self.file_before['ctime_ns'], ctime_after_ns=current['ctime_ns'],
                        sha256=hashlib.sha256(self.file_data).hexdigest(), nlink_before=1, nlink_after=0)
        payload = {'actor': {'pid': self.actor['pid'], 'starttime': self.actor['starttime'],
                             'executable_sha256': self.actor['exe_sha256']},
                   'before': self.before, 'after': after, 'pid_file': pid_file,
                   'event': {'kind': 'delete', 'actor_pid': self.actor['pid'],
                             'actor_starttime': self.actor['starttime'],
                             'directory_handle_sha256': _handle_digest(self.dir_handle),
                             'target_handle_sha256': _handle_digest(self.file_handle),
                             'raw_events_sha256': self.raw_hash.hexdigest()},
                   'settlement': {'tracer_wait_status': status, 'pidfd_exited': True, 'tracer_joined': True}}
        self.proof = _RunStopProof(self, json.dumps(payload, sort_keys=True, separators=(',', ':')).encode('ascii'))
        self.finished = True
        return self.proof

    def require_proof(self, proof):
        self._raise_error()
        if self.closed or not self.finished or self.used or proof is not self.proof or proof._owner is not self:
            raise JournalError('stop proof is foreign, consumed or inactive')
        self._drain(final=True)
        self._raise_error()
        self.used = True

    def close(self):
        if self.closed:
            return
        error = None
        try:
            with self.condition:
                if not self.finished:
                    self.cancelled = True
                self.resume = True
                self.condition.notify_all()
            if self.tracer is not None and self.tracer.is_alive():
                if self.tasks:
                    try:
                        signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
                    except (OSError, ProcessLookupError):
                        pass
                self.tracer.join(max(0, self.deadline - time.monotonic()))
                if self.tracer.is_alive():
                    raise JournalError('stop tracer did not join during cleanup')
        except BaseException as exc:
            error = exc
        try:
            self.stop_reader.set()
            if self.reader is not None:
                self.reader.join(max(0, self.deadline - time.monotonic()))
                if self.reader.is_alive():
                    raise JournalError('stop reader did not join during cleanup')
            self._raise_error()
            if self.fan_fd is not None and error is None:
                self._drain(final=True)
        except BaseException as exc:
            if error is None:
                error = exc
        finally:
            self.closed = True
            for fd in (self.fan_fd, self.pidfd, self.file_fd, self.run_fd):
                if fd is not None:
                    try:
                        os.close(fd)
                    except OSError:
                        pass
        if error is not None:
            raise error
