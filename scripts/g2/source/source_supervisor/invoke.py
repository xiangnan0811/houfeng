"""Pinned guest-only direct launches with held pidfds and bounded private I/O."""
import ctypes
import fcntl
import hashlib
import os
import select
import selectors
import signal
import socket
import stat
import time

import guest_bootstrap
import network_verify

from contract import CASES, MAX_FRAME
from journal import JournalError, canonical, process_start

PTRACE_TRACEME = 0
PTRACE_CONT = 7
PTRACE_DETACH = 17
PTRACE_SETOPTIONS = 0x4200
PTRACE_O_TRACEEXEC = 0x10
PTRACE_O_EXITKILL = 0x100000
PTRACE_EVENT_EXEC = 4
PR_SET_PDEATHSIG = 1
PR_SET_NO_NEW_PRIVS = 38
F_ADD_SEALS = 1033
F_GET_SEALS = 1034
ALL_SEALS = 0x0f
LIBC = ctypes.CDLL(None, use_errno=True)
LIBC.ptrace.restype = ctypes.c_long
LIBC.prctl.restype = ctypes.c_int


def _ptrace(request, pid, data=0):
    result = LIBC.ptrace(ctypes.c_uint(request), ctypes.c_int(pid), ctypes.c_void_p(), ctypes.c_void_p(data))
    if result == -1:
        raise OSError(ctypes.get_errno(), 'ptrace')


def _prctl(option, value):
    if LIBC.prctl(ctypes.c_int(option), ctypes.c_ulong(value), 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), 'prctl')


def sealed_json(value):
    body = canonical(value)
    if not 0 < len(body) <= MAX_FRAME:
        raise JournalError('private configuration size invalid')
    fd = os.memfd_create('g2-fixed-private', os.MFD_CLOEXEC | os.MFD_ALLOW_SEALING)
    try:
        view = memoryview(body)
        while view:
            count = os.write(fd, view)
            if count <= 0:
                raise JournalError('private configuration write failed')
            view = view[count:]
        fcntl.fcntl(fd, F_ADD_SEALS, ALL_SEALS)
        if fcntl.fcntl(fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS or os.pread(fd, len(body) + 1, 0) != body:
            raise JournalError('private configuration seal differs')
        return fd, hashlib.sha256(body).hexdigest()
    except BaseException:
        os.close(fd)
        raise


def _program(path, expected):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 10002 or
                stat.S_IMODE(before.st_mode) != 0o550 or before.st_nlink != 1):
            raise JournalError('fixed program permissions differ')
        digest = hashlib.sha256()
        while True:
            block = os.read(fd, 1024 * 1024)
            if not block:
                break
            digest.update(block)
        after = os.fstat(fd)
        if ((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) !=
                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) or
                digest.hexdigest() != expected):
            raise JournalError('fixed program identity differs')
    finally:
        os.close(fd)
    for directory in ('/opt', '/opt/g2'):
        info = os.stat(directory, follow_symlinks=False)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise JournalError('fixed program ancestor writable')


def _check_fds(pid, config, peer_identity, fault_identity, stdout, stderr, data_identity=None):
    actual = set(os.listdir('/proc/%d/fd' % pid))
    expected = ({'0', '1', '2'} | ({'4'} if config is not None else set()) |
                ({'5'} if peer_identity or data_identity else set()) | ({'7'} if fault_identity else set()))
    if actual != expected:
        raise JournalError('fixed child descriptor set differs')
    null = os.stat('/dev/null', follow_symlinks=False)
    stdin = os.stat('/proc/%d/fd/0' % pid)
    if not stat.S_ISCHR(stdin.st_mode) or stdin.st_rdev != null.st_rdev:
        raise JournalError('fixed child stdin differs')
    for number, held in ((1, stdout), (2, stderr)):
        child, parent = os.stat('/proc/%d/fd/%d' % (pid, number)), os.fstat(held)
        if not stat.S_ISFIFO(child.st_mode) or (child.st_dev, child.st_ino) != (parent.st_dev, parent.st_ino):
            raise JournalError('fixed child output pipe differs')
    if config is not None:
        child, parent = os.stat('/proc/%d/fd/4' % pid), os.fstat(config)
        if not stat.S_ISREG(child.st_mode) or (child.st_dev, child.st_ino) != (parent.st_dev, parent.st_ino):
            raise JournalError('fixed child configuration descriptor differs')
    if peer_identity:
        child = os.stat('/proc/%d/fd/5' % pid)
        if not stat.S_ISSOCK(child.st_mode) or (child.st_dev, child.st_ino) != peer_identity:
            raise JournalError('fixed child realm descriptor differs')
    if data_identity:
        child = os.stat('/proc/%d/fd/5' % pid)
        if not stat.S_ISREG(child.st_mode) or (child.st_dev, child.st_ino) != data_identity:
            raise JournalError('fixed child sealed dump descriptor differs')
    if fault_identity:
        child = os.stat('/proc/%d/fd/7' % pid)
        if not stat.S_ISSOCK(child.st_mode) or (child.st_dev, child.st_ino) != fault_identity:
            raise JournalError('fixed child fault descriptor differs')


def _wait_stop(pid, pidfd, deadline, wait_status=None):
    while time.monotonic() < deadline:
        observed, status = os.waitpid(pid, os.WNOHANG | os.WUNTRACED)
        if observed:
            if wait_status is not None and (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                wait_status.append(os.waitstatus_to_exitcode(status))
            return status
        if select.select([pidfd], [], [], min(0.02, deadline - time.monotonic()))[0]:
            observed, status = os.waitpid(pid, os.WNOHANG | os.WUNTRACED)
            if observed:
                if wait_status is not None and (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                    wait_status.append(os.waitstatus_to_exitcode(status))
                return status
            raise JournalError('child exit became uncertain')
    raise JournalError('child tracing deadline exceeded')


def _enter_migration_scope(identity, pid, pidfd, starttime):
    empty = network_verify.migration_scope(identity)
    if empty != {'members': [], 'populated': 0} or select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime:
        raise JournalError('migration scope is not empty before coordinator enrollment')
    group_fd = os.open(identity['path'], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        group = os.fstat(group_fd)
        if (group.st_dev, group.st_ino) != (identity['dev'], identity['ino']):
            raise JournalError('migration scope changed before coordinator enrollment')
        procs_fd = os.open('cgroup.procs', os.O_WRONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=group_fd)
        try:
            control = os.fstat(procs_fd)
            if (not stat.S_ISREG(control.st_mode) or (control.st_uid, control.st_gid) != (0, 0) or
                    control.st_mode & 0o022 or select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime):
                raise JournalError('migration coordinator enrollment identity changed')
            payload = str(pid).encode('ascii')
            if os.write(procs_fd, payload) != len(payload):
                raise JournalError('migration coordinator enrollment was short')
        finally:
            os.close(procs_fd)
    finally:
        os.close(group_fd)
    enrolled = network_verify.migration_scope(identity)
    membership = network_verify._read('/proc/%d/cgroup' % pid, 65536).strip()
    if (enrolled != {'members': [pid], 'populated': 1} or membership != '0::/g2-c81-migration' or
            select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime):
        raise JournalError('migration coordinator did not enter the pinned scope')
    return empty, enrolled


def _retire_migration_scope(installed, deadline):
    identity = installed['migration_cgroup']
    fd = os.open(identity['path'] + '/cgroup.events', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    poller = select.poll()
    poller.register(fd, select.POLLPRI | select.POLLERR)
    try:
        while True:
            guest_bootstrap.verify_migration_binding(installed)
            state = network_verify.migration_scope(identity)
            if state == {'members': [], 'populated': 0}:
                return state
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise JournalError('migration scope did not retire before the deadline')
            os.lseek(fd, 0, os.SEEK_SET)
            os.read(fd, 1024)
            poller.poll(max(1, min(250, int(remaining * 1000))))
    finally:
        poller.unregister(fd)
        os.close(fd)


def _child(path, argv, config, channel, fault, output, error, require_nnp, parent_pid, root_producer=False):
    try:
        # This forked child is stopped before any privilege drop or business
        # effect. The root parent records its pidfd, then observes exec under
        # ptrace before releasing it to execute application code.
        _ptrace(PTRACE_TRACEME, 0)
        os.kill(os.getpid(), signal.SIGSTOP)
        nullfd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        values = ((0, nullfd), (1, output), (2, error), (4, config), (5, channel), (7, fault))
        sources = [(target, fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 10) if fd is not None else None)
                   for target, fd in values]
        for target, source in sources:
            if source is not None:
                os.dup2(source, target, inheritable=True)
        try:
            os.close(3)
        except OSError:
            pass
        if config is None:
            try:
                os.close(4)
            except OSError:
                pass
        if channel is None:
            try:
                os.close(5)
            except OSError:
                pass
        for name in os.listdir('/proc/self/fd'):
            if name.isdecimal() and int(name) >= 6 and (int(name) != 7 or fault is None):
                try:
                    os.close(int(name))
                except OSError:
                    pass
        os.setgroups([])
        os.setresgid(0, 0, 0) if root_producer else os.setresgid(10002, 10002, 10002)
        os.setresuid(0, 0, 0) if root_producer else os.setresuid(10002, 10002, 10002)
        if require_nnp:
            _prctl(PR_SET_NO_NEW_PRIVS, 1)
        _prctl(PR_SET_PDEATHSIG, signal.SIGKILL)
        if os.getppid() != parent_pid:
            os._exit(125)
        os.execve(path, argv, {'PATH': '/usr/bin:/bin', 'HOME': '/nonexistent', 'LANG': 'C', 'LC_ALL': 'C'})
    except BaseException:
        os._exit(126)


class _EndpointTransitionLease:
    def __init__(self, lifecycle, command, recovery_id, endpoint_container_id, transition):
        self.lifecycle = lifecycle
        self.command = command
        self.recovery_id = recovery_id
        self.endpoint_container_id = endpoint_container_id
        self.transition = transition
        self.transition_id = None

    def begin(self, identity, pidfd, deadline):
        scope = self.lifecycle.cgroups['migration_cgroup']
        scope_fd = os.open(scope['path'], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            self.transition_id = self.lifecycle.bpf.begin_endpoint_transition(
                self.endpoint_container_id, self.recovery_id,
                identity['pid'], identity['starttime'], pidfd, scope_fd, deadline=deadline)
        finally:
            os.close(scope_fd)
        if self.transition is not None:
            self.transition.launched(identity, pidfd)

    def end(self, result, deadline):
        if self.transition is not None:
            self.transition.complete(result)
        if self.transition_id is not None:
            self.lifecycle.bpf.end_endpoint_transition(self.transition_id, deadline=deadline)
            self.transition_id = None


class FixedInvoker:
    def __init__(self, lifecycle, program_hashes, case, realm_negative=False):
        if case not in CASES or type(program_hashes) is not dict or type(realm_negative) is not bool:
            raise JournalError('fixed launch context invalid')
        self.lifecycle = lifecycle
        self.hashes = program_hashes
        self.case = case
        self.realm_negative = realm_negative

    def app_initializer(self, mode, config_fd, config_digest, deadline):
        if mode not in ('initialize', 'heartbeat') or self.case not in CASES[1:]:
            raise JournalError('initializer mode invalid')
        return self._run('c80-initializer', ['initialize' if mode == 'initialize' else 'heartbeat', self.case],
                         config_fd, config_digest, None, deadline)

    def domain_initializer(self, config_fd, config_digest, deadline):
        return self._run('domain-initializer', ['initialize'], config_fd, config_digest, None, deadline)

    def comparison_negative(self, config_fd, config_digest, channel_factory, deadline):
        if (not self.realm_negative or not self.lifecycle.invalid or self.lifecycle.stage != 'source_maintenance' or
                channel_factory is None or self.lifecycle.negative_producer_used):
            raise JournalError('negative producer requires a fresh permanently invalid realm')
        self.lifecycle.negative_producer_used = True
        return self._run('c80-comparison-negative', ['produce'], config_fd, config_digest, channel_factory,
                         deadline, negative_producer=True)

    def backup_negative(self, config_fd, config_digest, dump_fd, deadline):
        if (not self.lifecycle.invalid or self.lifecycle.stage != 'source_maintenance' or
                not (self.realm_negative or self.lifecycle.closed_maintenance) or self.lifecycle.negative_producer_used):
            raise JournalError('backup producer requires a fresh permanently invalid negative realm')
        self.lifecycle.negative_producer_used = True
        return self._run('c80-backup-negative', ['produce'], config_fd, config_digest, None, deadline,
                         negative_producer=True, dump_fd=dump_fd)

    def admin(self, command, request_file, channel_factory, deadline, fault_factory=None, *,
              recovery_id, endpoint_container_id, transition=None):
        if command not in ('legacy-retirement-preview', 'legacy-retire', 'legacy-retirement-reconcile',
                            'legacy-c80-service', 'legacy-c81-migrate', 'reconcile-legacy'):
            raise JournalError('unrecognized fixed management action')
        if (type(recovery_id) is not str or not recovery_id or
                type(endpoint_container_id) is not str or len(endpoint_container_id) != 64 or
                any(c not in '0123456789abcdef' for c in endpoint_container_id)):
            raise JournalError('endpoint lease parameters invalid')
        if transition is not None:
            from fence_transition import FenceTransition
            if (command != 'legacy-retirement-preview' or not isinstance(transition, FenceTransition) or
                    transition.recovery_id != recovery_id or transition.app_id != endpoint_container_id):
                raise JournalError('network transition launch selector invalid')
        if (type(request_file) is not str or not request_file.startswith('/var/lib/g2/control/maintenance/requests/') or
                '/' in request_file.removeprefix('/var/lib/g2/control/maintenance/requests/') or
                not request_file.endswith('.json')):
            raise JournalError('management request path is not fixed private input')
        if (self.lifecycle.migration_checkpoint is not None and
                command not in ('legacy-c81-migrate', 'legacy-retirement-reconcile', 'reconcile-legacy')):
            raise JournalError('action is outside the active maintenance owner')
        terminal_command = command in ('legacy-retirement-reconcile', 'reconcile-legacy')
        if terminal_command and self.lifecycle.migration_checkpoint is not None:
            if channel_factory is not None:
                raise JournalError('successor maintenance channel is fixed by the supervisor')
            def channel_factory(identity, pidfd, sock):
                from ipc import MaintenanceChannel
                coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                               'executable_sha256': identity['exe_sha256']}
                return MaintenanceChannel(sock, coordinator, pidfd, self.lifecycle.migration_checkpoint,
                                          terminal_only=True)
        endpoint_lease = _EndpointTransitionLease(self.lifecycle, command, recovery_id,
                                                  endpoint_container_id, transition)
        family = 'record-deletion' if command in ('legacy-c80-service', 'legacy-c81-migrate', 'reconcile-legacy') else 'source-authority'
        self.lifecycle.origins.allowed_predecessor = False
        self.lifecycle.origins.allowed_migrator = False
        try:
            return self._run('houfeng-record-platform-admin', [family, command, '--request-file', request_file],
                             None, None, channel_factory, deadline, fault_factory, endpoint_lease)
        finally:
            self.lifecycle.origins.allowed_predecessor = False
            self.lifecycle.origins.allowed_migrator = False

    def _run(self, program, arguments, config_fd, config_digest, channel_factory, deadline, fault_factory=None,
             endpoint_lease=None, negative_producer=False, dump_fd=None):
        backup = program == 'c80-backup-negative'
        admin_launch = program == 'houfeng-record-platform-admin'
        if admin_launch != isinstance(endpoint_lease, _EndpointTransitionLease):
            raise JournalError('fixed endpoint lease binding invalid')
        migration = (admin_launch and len(arguments) == 4 and
                     arguments[:2] == ['record-deletion', 'legacy-c81-migrate'])
        terminal = (admin_launch and len(arguments) == 4 and
                    arguments[:2] in (['source-authority', 'legacy-retirement-reconcile'],
                                      ['record-deletion', 'reconcile-legacy']) and
                    self.lifecycle.migration_checkpoint is not None)
        scoped = migration or terminal
        if migration:
            if self.lifecycle.stage != 'c81_migration':
                raise JournalError('migration coordinator requires its fixed lifecycle stage')
            if channel_factory is None or self.lifecycle.migration_checkpoint is None:
                raise JournalError('migration requires the root maintenance channel')
            deadline = min(deadline, time.monotonic() + 60)
            self.lifecycle.require_migration_continuity()
        if terminal:
            if self.lifecycle.stage not in ('c81_migration', 'retired') or channel_factory is None:
                raise JournalError('successor coordinator requires its witnessed maintenance owner')
            deadline = min(deadline, time.monotonic() + 60)
            self.lifecycle.require_terminal_admission()
        if scoped and time.monotonic() >= deadline - 15:
            raise JournalError('maintenance work budget expired before coordinator launch')
        closed_admin = (self.lifecycle.closed_maintenance and self.lifecycle.invalid and
                        admin_launch and (channel_factory is None or scoped) and
                        endpoint_lease.transition is None and len(arguments) == 4 and
                        (fault_factory is None or scoped) and
                        (self.lifecycle.stage, arguments[0], arguments[1]) in (
                            ('c81_migration', 'record-deletion', 'legacy-c81-migrate'),
                            ('c81_migration', 'source-authority', 'legacy-retirement-reconcile'),
                            ('c81_migration', 'record-deletion', 'reconcile-legacy'),
                            ('retired', 'source-authority', 'legacy-retirement-reconcile'),
                            ('retired', 'record-deletion', 'reconcile-legacy')))
        retired_continuation = (self.lifecycle.invalid and not self.lifecycle.closed_maintenance and scoped and
                                admin_launch and channel_factory is not None and
                                fault_factory is None and endpoint_lease.transition is None)
        if retired_continuation:
            self.lifecycle.require_retired_maintenance_continuation()
        closed_probe = (self.lifecycle.closed_maintenance and self.lifecycle.invalid and
                        admin_launch and channel_factory is not None and
                        fault_factory is None and endpoint_lease.transition is None and len(arguments) == 4 and
                        self.lifecycle.stage == 'source_maintenance' and
                        arguments[:2] == ['record-deletion', 'legacy-c80-service'])
        if (time.monotonic() >= deadline or type(negative_producer) is not bool or
                negative_producer != (program in ('c80-comparison-negative', 'c80-backup-negative')) or
                (not self.lifecycle.invalid and negative_producer) or
                (self.lifecycle.invalid and not negative_producer and not closed_admin and not closed_probe and
                 not retired_continuation) or
                backup != (dump_fd is not None) or
                backup and (arguments != ['produce'] or channel_factory is not None or fault_factory is not None or endpoint_lease is not None)):
            raise JournalError('fixed launch unavailable')
        try:
            migration_binding = guest_bootstrap.verify_migration_binding(self.lifecycle.cgroups)
            if network_verify.migration_scope(self.lifecycle.cgroups['migration_cgroup']) != {'members': [], 'populated': 0}:
                raise JournalError('prior migration scope has not retired')
        except BaseException:
            self.lifecycle.invalidate('migration_scope_unavailable')
            raise
        path = '/opt/g2/' + program
        expected = self.hashes[program]
        _program(path, expected)
        if migration:
            _program('/opt/g2/c81-migrator', self.hashes['c81-migrator'])
        if config_fd is not None:
            if (fcntl.fcntl(config_fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS or
                    hashlib.sha256(os.pread(config_fd, MAX_FRAME + 1, 0)).hexdigest() != config_digest):
                raise JournalError('fixed launch configuration differs')
        if dump_fd is not None:
            info = os.fstat(dump_fd)
            if (not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 64 * 1024 * 1024 or
                    fcntl.fcntl(dump_fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS or
                    fcntl.fcntl(dump_fd, fcntl.F_GETFL) & os.O_ACCMODE != os.O_RDONLY):
                raise JournalError('backup input is not a bounded read-only sealed dump')
        argv = [path] + arguments
        action = {'program': program, 'argv': argv, 'config_digest': config_digest,
                  'has_fd5_channel': channel_factory is not None, 'has_fault_channel': fault_factory is not None,
                  'has_sealed_dump': dump_fd is not None}
        if retired_continuation:
            self.lifecycle.continuation_retirement = None
        name = 'direct-' + program
        self.lifecycle._admit(name, action)
        out_read, out_write = os.pipe2(os.O_CLOEXEC)
        err_read, err_write = os.pipe2(os.O_CLOEXEC)
        os.set_blocking(out_read, False)
        os.set_blocking(err_read, False)
        left = right = None
        if channel_factory is not None:
            left, right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        peer_identity = None if right is None else (os.fstat(right.fileno()).st_dev, os.fstat(right.fileno()).st_ino)
        data_identity = None if dump_fd is None else (os.fstat(dump_fd).st_dev, os.fstat(dump_fd).st_ino)
        fault_left = fault_right = None
        if fault_factory is not None:
            if program != 'houfeng-record-platform-admin':
                raise JournalError('fault channel is limited to the coordinator')
            fault_left, fault_right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        fault_identity = None if fault_right is None else (os.fstat(fault_right.fileno()).st_dev,
                                                            os.fstat(fault_right.fileno()).st_ino)
        pid = pidfd = None
        origin_joined = registered = retired = exit_logged = False
        wait_status = []
        enrollment = coordinator = channel = None
        try:
            parent_pid = os.getpid()
            pid = os.fork()
            if pid == 0:
                _child(path, argv, config_fd, dump_fd if backup else (None if right is None else right.fileno()),
                       None if fault_right is None else fault_right.fileno(), out_write, err_write,
                       program != 'houfeng-record-platform-admin', parent_pid, root_producer=backup)
            os.close(out_write)
            out_write = -1
            os.close(err_write)
            err_write = -1
            if right is not None:
                right.close()
                right = None
            if fault_right is not None:
                fault_right.close()
                fault_right = None
            pidfd = os.pidfd_open(pid, 0)
            starttime = process_start(pid)
            status = _wait_stop(pid, pidfd, min(deadline, time.monotonic() + 10), wait_status)
            if not os.WIFSTOPPED(status) or os.WSTOPSIG(status) != signal.SIGSTOP:
                raise JournalError('fixed child did not enter launch stop')
            if scoped:
                before_scope, enrolled_scope = _enter_migration_scope(
                    self.lifecycle.cgroups['migration_cgroup'], pid, pidfd, starttime)
            self.lifecycle.journal.append('registration', {'name': 'fork-' + program,
                'identity_digest': hashlib.sha256(canonical({'pid': pid, 'starttime': starttime})).hexdigest()})
            _ptrace(PTRACE_SETOPTIONS, pid, PTRACE_O_TRACEEXEC | PTRACE_O_EXITKILL)
            _ptrace(PTRACE_CONT, pid)
            status = _wait_stop(pid, pidfd, min(deadline, time.monotonic() + 10), wait_status)
            if not os.WIFSTOPPED(status) or os.WSTOPSIG(status) != signal.SIGTRAP or status >> 16 != PTRACE_EVENT_EXEC:
                raise JournalError('fixed child did not stop at exec')
            _check_fds(pid, config_fd, peer_identity, fault_identity, out_read, err_read, data_identity)
            identity = self.lifecycle.origins.register_direct(pid, pidfd, path, expected, argv, root_negative=backup)
            registered = True
            enrollment = None
            if scoped:
                scope = self.lifecycle.cgroups['migration_cgroup']
                if (network_verify.migration_scope(scope) != enrolled_scope or
                        network_verify._read('/proc/%d/cgroup' % pid, 65536).strip() != '0::/g2-c81-migration'):
                    raise JournalError('migration coordinator left its scope at exec')
                coordinator = {'pid': pid, 'starttime': starttime, 'executable_sha256': expected}
                enrollment = {'boot_id': migration_binding['boot_id'], 'cgroup': scope,
                              'binding_sha256': self.lifecycle.cgroups['migration_binding']['sha256'],
                              'command': arguments[1], 'request_file': arguments[3],
                              'fault_channel': fault_factory is not None, 'coordinator': coordinator,
                              'before': before_scope, 'enrolled': enrolled_scope}
                if migration:
                    self.lifecycle.origins.allowed_migrator = {'coordinator': coordinator, 'cgroup': scope,
                                                               'program_sha256': self.hashes['c81-migrator']}
                self.lifecycle.journal.append('registration', {'name': 'c81-scope-enrolled',
                    'identity_digest': hashlib.sha256(canonical(enrollment)).hexdigest()})
            self.lifecycle._register(name, {'pid': pid, 'starttime': starttime, 'executable_sha256': expected})
            self.lifecycle.journal.append('launch', {'name': program, 'pid': pid, 'starttime': starttime,
                                                       'executable_sha256': expected})
            if endpoint_lease is not None:
                endpoint_lease.begin(identity, pidfd, deadline)
            channel = channel_factory(identity, pidfd, left) if channel_factory is not None else None
            if scoped:
                from ipc import MaintenanceChannel
                if not isinstance(channel, MaintenanceChannel) or channel.terminal_only != terminal:
                    raise JournalError('fixed coordinator requires its maintenance-only channel')
            if closed_probe:
                from ipc import ClosedRealmChannel
                if not isinstance(channel, ClosedRealmChannel):
                    raise JournalError('closed APP recovery requires an unavailable-only realm channel')
            fault = fault_factory(identity, pidfd, fault_left) if fault_factory is not None else None
            if fault is not None:
                fault.configure(min(deadline, time.monotonic() + 10))
            _ptrace(PTRACE_DETACH, pid)
            result = self._drain(pid, pidfd, out_read, err_read, channel, fault, deadline, wait_status)
            if scoped:
                after_scope = _retire_migration_scope(self.lifecycle.cgroups, deadline)
                if not select.select([pidfd], [], [], 0)[0]:
                    raise JournalError('maintenance coordinator pidfd did not exit')
                if result['status'] == 0 and not channel.terminal_acks:
                    raise JournalError('successful maintenance coordinator omitted terminal acknowledgement')
                retirement = {'boot_id': migration_binding['boot_id'], 'cgroup': enrollment['cgroup'],
                              'binding_sha256': enrollment['binding_sha256'], 'command': arguments[1],
                              'coordinator': coordinator, 'after': after_scope, 'status': result['status'],
                              'terminal_acks': list(channel.terminal_acks),
                              'coordinator_pidfd_exited': True, 'coordinator_direct_wait': True}
                channel.coordinator_waited(result['status'], retirement)
                self.lifecycle.journal.append('registration', {'name': 'c81-scope-retired',
                    'identity_digest': hashlib.sha256(canonical(retirement)).hexdigest()})
                self.lifecycle.migration_scopes.append({'enrollment': enrollment, 'retirement': retirement})
                retired = True
            self.lifecycle.origins.join_direct(pid, pidfd)
            origin_joined = True
            self.lifecycle.journal.append('exit', {'name': program, 'pid': pid, 'starttime': starttime,
                                                     'status': result['status'], 'pidfd_exited': True})
            exit_logged = True
            if scoped and self.lifecycle.invalid and self.lifecycle.invalid_reason == 'direct_launch_failed':
                self.lifecycle.continuation_retirement = retirement
            if program == 'houfeng-record-platform-admin' and channel is not None and not scoped:
                channel.coordinator_waited(result['status'])
            if result.get('channel_error') is not None:
                raise JournalError('maintenance channel failed after coordinator settlement') from result['channel_error']
            result.update(pid=pid, starttime=starttime)
            if fault is not None:
                result['fault'] = fault.observation
            if endpoint_lease is not None:
                endpoint_lease.end(result, deadline)
            return result
        except BaseException:
            cleanup_deadline = deadline if scoped else time.monotonic() + 15
            try:
                self.lifecycle.invalidate('direct_launch_failed')
            finally:
                try:
                    if pid is not None and pidfd is not None and not select.select([pidfd], [], [], 0)[0]:
                        try:
                            signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                    if pid is not None and program == 'houfeng-record-platform-admin' and not scoped:
                        self.lifecycle.callbacks.abort_coordinator(pid, cleanup_deadline)
                finally:
                    if pid is not None and not wait_status:
                        if pidfd is not None and select.select([pidfd], [], [], max(0, cleanup_deadline - time.monotonic()))[0]:
                            waited, raw = os.waitpid(pid, 0)
                        elif pidfd is None:
                            waited, raw = os.waitpid(pid, os.WNOHANG)
                        else:
                            raise JournalError('failed coordinator did not exit during cleanup')
                        if waited != pid or not (os.WIFEXITED(raw) or os.WIFSIGNALED(raw)):
                            raise JournalError('failed coordinator direct wait unavailable')
                        wait_status.append(os.waitstatus_to_exitcode(raw))
                    if scoped and pid is not None:
                        after_scope = _retire_migration_scope(self.lifecycle.cgroups, cleanup_deadline)
                        if (not retired and enrollment is not None and channel is not None and registered and
                                pidfd is not None and wait_status and select.select([pidfd], [], [], 0)[0]):
                            retirement = {'boot_id': migration_binding['boot_id'], 'cgroup': enrollment['cgroup'],
                                          'binding_sha256': enrollment['binding_sha256'], 'command': arguments[1],
                                          'coordinator': coordinator, 'after': after_scope, 'status': wait_status[0],
                                          'terminal_acks': list(channel.terminal_acks),
                                          'coordinator_pidfd_exited': True, 'coordinator_direct_wait': True}
                            channel.coordinator_waited(wait_status[0], retirement)
                            self.lifecycle.journal.append('registration', {'name': 'c81-scope-retired',
                                'identity_digest': hashlib.sha256(canonical(retirement)).hexdigest()})
                            self.lifecycle.migration_scopes.append({'enrollment': enrollment, 'retirement': retirement})
                            retired = True
                    if registered and wait_status and not exit_logged:
                        if not origin_joined:
                            self.lifecycle.origins.join_direct(pid, pidfd)
                            origin_joined = True
                        self.lifecycle.journal.append('exit', {'name': program, 'pid': pid, 'starttime': starttime,
                                                                 'status': wait_status[0], 'pidfd_exited': True})
                        exit_logged = True
                    if (scoped and retired and origin_joined and exit_logged and
                            self.lifecycle.invalid_reason == 'direct_launch_failed'):
                        self.lifecycle.continuation_retirement = retirement
            raise
        finally:
            for fd in (out_read, out_write, err_read, err_write, pidfd):
                if fd is not None and fd >= 0:
                    os.close(fd)
            if left is not None:
                left.close()
            if right is not None:
                right.close()
            if fault_left is not None:
                fault_left.close()
            if fault_right is not None:
                fault_right.close()

    @staticmethod
    def _drain(pid, pidfd, stdout, stderr, channel, fault, deadline, wait_status):
        selector = selectors.DefaultSelector()
        output = {'stdout': bytearray(), 'stderr': bytearray()}
        selector.register(stdout, selectors.EVENT_READ, 'stdout')
        selector.register(stderr, selectors.EVENT_READ, 'stderr')
        selector.register(pidfd, selectors.EVENT_READ, 'exit')
        if channel is not None:
            selector.register(channel.sock, selectors.EVENT_READ, 'realm')
        if fault is not None:
            selector.register(fault.sock, selectors.EVENT_READ, 'fault')
        status = None
        channel_error = None
        try:
            while status is None or any(key.data in ('stdout', 'stderr') for key in selector.get_map().values()):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise JournalError('fixed child deadline exceeded')
                events = selector.select(min(remaining, 0.25))
                events.sort(key=lambda event: 0 if event[0].data == 'exit' else 1)
                for key, _ in events:
                    if status is not None and key.data in ('realm', 'fault'):
                        continue
                    if key.data in ('stdout', 'stderr'):
                        chunk = os.read(key.fd, MAX_FRAME + 1)
                        if not chunk:
                            selector.unregister(key.fd)
                        else:
                            output[key.data].extend(chunk)
                            if len(output[key.data]) > MAX_FRAME:
                                raise JournalError('fixed child output exceeded limit')
                    elif key.data == 'realm':
                        from ipc import MaintenanceChannel
                        if isinstance(channel, MaintenanceChannel) and not channel.sock.recv(1, socket.MSG_PEEK | socket.MSG_DONTWAIT):
                            poller = select.poll()
                            poller.register(channel.sock, select.POLLHUP | getattr(select, 'POLLRDHUP', 0))
                            events = poller.poll(0)
                            if events and events[0][1] & (select.POLLHUP | getattr(select, 'POLLRDHUP', 0)):
                                selector.unregister(channel.sock)
                                continue
                        try:
                            channel.admit_pending(min(deadline, time.monotonic() + 60))
                            channel.serve_one(min(deadline, time.monotonic() + 60))
                        except Exception as exc:
                            if not isinstance(channel, MaintenanceChannel):
                                raise
                            channel_error = exc
                            selector.unregister(channel.sock)
                            try:
                                channel.sock.shutdown(socket.SHUT_RDWR)
                            except OSError:
                                pass
                    elif key.data == 'fault':
                        fault.serve_one(min(deadline, time.monotonic() + 60))
                    elif key.data == 'exit':
                        _, raw = os.waitpid(pid, 0)
                        status = os.waitstatus_to_exitcode(raw)
                        wait_status.append(status)
                        selector.unregister(pidfd)
                        if channel is not None and channel.sock in selector.get_map():
                            selector.unregister(channel.sock)
                        if fault is not None:
                            selector.unregister(fault.sock)
            if status is None or (status < 0 and not (fault is not None and fault.completed and
                    getattr(fault, 'victim', None) == 'coordinator' and status == -signal.SIGKILL) and
                    not (channel is not None and callable(getattr(channel, 'coordinator_waited', None)))):
                raise JournalError('fixed child exited by signal')
            return {'status': status, 'stdout': bytes(output['stdout']), 'stderr': bytes(output['stderr']),
                    'channel_error': channel_error}
        finally:
            selector.close()
