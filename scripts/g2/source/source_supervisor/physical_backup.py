"""Disposable negative-only real PostgreSQL dump and independent store census."""
import fcntl
import hashlib
import os
import re
import select
import signal
import stat
import struct
import threading
import time

from census import _mount_id
from guest_postgres import _UnixHTTP
from invoke import ALL_SEALS, F_GET_SEALS, sealed_json
from journal import JournalError, canonical, process_start, strict_json
from origins import _cgroup_has_container, _identity
from pg_bootstrap import DATABASES, _message, _run, _send
from pg_session import ExtraSession

MAX_DUMP = 64 * 1024 * 1024
DUMP_PATH = '/usr/lib/postgresql/16/bin/pg_dump'
BACKUP_ROOT = '/var/lib/g2/managed-backups'
BACKUP_CHILDREN = ('staging', 'published', 'multipart', 'pins')


class _DumpLock(ExtraSession):
    """Hold a fixed table lock so the actual pg_dump stays observable."""

    def __init__(self, life, password, host):
        super().__init__(life, password, host, purpose='negative-dump-lock', expected_state='idle in transaction')

    def _hold(self, sock, backend_pid, deadline):
        total = [0]
        _send(sock, b'Q', b'BEGIN; LOCK TABLE public.records IN ACCESS EXCLUSIVE MODE\0', deadline)
        seen = []
        while True:
            kind, body = _message(sock, deadline, total)
            if kind == b'C' and body in (b'BEGIN\0', b'LOCK TABLE\0'):
                seen.append(body)
            elif kind == b'Z' and body == b'T' and seen == [b'BEGIN\0', b'LOCK TABLE\0']:
                break
            else:
                raise JournalError('fixed dump observation lock did not settle')
        self.backend_pid = backend_pid
        self.ready.set()
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not self.release.wait(remaining):
            raise JournalError('fixed dump observation lock deadline exceeded')
        _send(sock, b'Q', b'ROLLBACK\0', deadline)
        kind, body = _message(sock, deadline, total)
        if kind != b'C' or body != b'ROLLBACK\0':
            raise JournalError('fixed dump observation lock rollback failed')
        kind, body = _message(sock, deadline, total)
        if kind != b'Z' or body != b'I':
            raise JournalError('fixed dump observation lock did not release')



def _dump_backend(password, host):
    query = ("SELECT coalesce((SELECT json_agg(json_build_object('pid',pid,'backend_start',backend_start::text,"
             "'role',usename,'application_name',application_name,'state',state,'wait_type',wait_event_type,"
             "'database',datname) ORDER BY pid) FROM pg_stat_activity WHERE application_name='pg_dump' "
             "AND pid<>pg_backend_pid()),'[]'::json)::text AS state\0")
    return _run('app', password, 'records', query.encode('ascii'), host)


def _read_exact(response, amount, eof_ok=False):
    value = bytearray()
    while len(value) < amount:
        chunk = response.read(amount - len(value))
        if not chunk:
            if eof_ok and not value:
                return b''
            raise JournalError('Docker dump stream truncated')
        value.extend(chunk)
    return bytes(value)


def _write_all(fd, data):
    view = memoryview(data)
    while view:
        written = os.write(fd, view)
        if written <= 0:
            raise JournalError('sealed dump write failed')
        view = view[written:]


def _capture_stream(postgres, exec_id, target, outcome):
    connection = _UnixHTTP(postgres.daemon, 60)
    outcome['connection'] = connection
    digest, size, stderr_size = hashlib.sha256(), 0, 0
    try:
        body = b'{"Detach":false,"Tty":false}'
        connection.request('POST', '/v1.41/exec/' + exec_id + '/start', body=body,
                           headers={'Content-Type': 'application/json'})
        response = connection.getresponse()
        if response.status != 200:
            raise JournalError('fixed Docker dump did not start')
        while True:
            header = _read_exact(response, 8, eof_ok=True)
            if not header:
                break
            stream, pad, length = header[0], header[1:4], struct.unpack('!I', header[4:])[0]
            if stream not in (1, 2) or pad != b'\0\0\0' or length > MAX_DUMP:
                raise JournalError('Docker dump stream frame invalid')
            while length:
                chunk = _read_exact(response, min(length, 1024 * 1024))
                length -= len(chunk)
                if stream == 2:
                    stderr_size += len(chunk)
                    if stderr_size > 16384:
                        raise JournalError('Docker dump error stream exceeds limit')
                else:
                    size += len(chunk)
                    if size > MAX_DUMP:
                        raise JournalError('actual PostgreSQL dump exceeds limit')
                    digest.update(chunk)
                    _write_all(target, chunk)
        if stderr_size or not 0 < size <= MAX_DUMP or os.pread(target, 5, 0) != b'PGDMP':
            raise JournalError('actual PostgreSQL custom dump unavailable')
        outcome['stream'] = {'size': size, 'sha256': digest.hexdigest(), 'format': 'postgres-custom'}
    except BaseException as exc:
        outcome['error'] = exc
    finally:
        connection.close()
        outcome['connection'] = None


def capture_dump(life, postgres, host, require_forbidden_exec=False, preserve_catalog=False):
    if (type(preserve_catalog) is not bool or not life.invalid or life.stage != 'source_maintenance' or
            preserve_catalog and not life.closed_maintenance):
        raise JournalError('dump is permitted only after permanent negative invalidation')
    container = postgres.containers['app']
    if select.select([container['pidfd']], [], [], 0)[0] or process_start(container['pid']) != container['starttime']:
        raise JournalError('original APP PostgreSQL container is not live')
    password = postgres.passwords['app']
    lock = _DumpLock(life, password, host)
    dump_fd = pidfd = None
    thread = None
    outcome = {}
    try:
        lock_state = lock.open()
        if lock_state.get('role') != 'postgres' or lock_state.get('state') != 'idle in transaction':
            raise JournalError('fixed dump observation lock backend differs')
        argv = [DUMP_PATH, '--host=' + host, '--port=5432', '--username=postgres', '--format=custom']
        if not preserve_catalog:
            argv.extend(('--no-owner', '--no-privileges'))
        argv.append(DATABASES['app'])
        deadline = time.monotonic() + 40
        obs_name = 'app_rollback_dump' if preserve_catalog else 'negative_pg_dump'
        with postgres.bpf.exec_observation(container['id'], obs_name, deadline) as track:
            life._admit('negative-pg-dump', {'container_id': container['id'], 'argv': argv,
                                            'database': DATABASES['app'], 'format': 'custom'})
            create = {'AttachStdout': True, 'AttachStderr': True, 'Tty': False,
                      'User': '%d:%d' % (postgres.uid, postgres.gid), 'Env': ['PGPASSWORD=' + password],
                      'Cmd': argv}
            result = postgres._request('POST', '/containers/' + container['id'] + '/exec', create)
            exec_id = result.get('Id') if type(result) is dict else None
            if type(exec_id) is not str or re.fullmatch(r'[0-9a-f]{64}', exec_id) is None:
                raise JournalError('fixed dump exec identity unavailable')
            track(exec_id)
            dump_fd = os.memfd_create('g2-pg-dump', os.MFD_CLOEXEC | os.MFD_ALLOW_SEALING)
            thread = threading.Thread(target=_capture_stream, args=(postgres, exec_id, dump_fd, outcome), daemon=True)
            thread.start()
            identity = None
            backend = None
            while time.monotonic() < deadline:
                if 'error' in outcome:
                    raise JournalError('fixed Docker dump failed before observation')
                state = postgres._request('GET', '/exec/' + exec_id + '/json')
                pid = state.get('Pid') if type(state) is dict and state.get('Running') is True else None
                if type(pid) is int and pid > 0:
                    candidate = _identity(pid, full=True)
                    if candidate and candidate.get('exe') == DUMP_PATH:
                        found = _dump_backend(password, host)
                        if type(found) is list and len(found) == 1 and found[0].get('wait_type') == 'Lock':
                            identity, backend = candidate, found[0]
                            break
                threading.Event().wait(0.02)
            if identity is None or backend is None:
                raise JournalError('actual pg_dump process and blocked backend were not observed')
            pid, start = identity['pid'], identity['starttime']
            pidfd = os.pidfd_open(pid, 0)
            status = identity['status']
            if (select.select([pidfd], [], [], 0)[0] or process_start(pid) != start or
                    not _cgroup_has_container(identity['cgroup'], container['id']) or
                    identity['argv'] != [os.fsencode(value) for value in argv] or
                    status.get(b'Uid', '').split() != [str(postgres.uid)] * 4 or
                    status.get(b'Gid', '').split() != [str(postgres.gid)] * 4 or
                    status.get(b'NoNewPrivs') != '1' or backend.get('role') != 'postgres' or
                    backend.get('application_name') != 'pg_dump' or backend.get('state') != 'active' or
                    backend.get('database') != DATABASES['app'] or type(backend.get('backend_start')) is not str):
                raise JournalError('actual pg_dump or APP backend identity differs')
            observed = {'exec_id': exec_id, 'container_id': container['id'], 'pid': pid, 'starttime': start,
                        'executable_sha256': identity['exe_sha256'], 'backend': backend,
                        'observation_lock_backend': lock_state}
            life._register('negative-pg-dump', observed)
            if require_forbidden_exec:
                event_deadline = min(deadline, time.monotonic() + 5)
                while life.audit.error is None and time.monotonic() < event_deadline:
                    threading.Event().wait(0.02)
                event = life.origins.forbidden_event
                if (event != {'operation': 'exec', 'pid': pid, 'exe': DUMP_PATH} or not life.origins.invalid or
                        not isinstance(life.audit.error, JournalError) or
                        str(life.audit.error) != 'forbidden producer execution observed'):
                    raise JournalError('actual unadmitted dump execution was not observed by audit')
                observed['forbidden_exec'] = event
            lock.close()
            thread.join(max(0, deadline - time.monotonic()))
            if thread.is_alive() or 'error' in outcome or 'stream' not in outcome:
                raise JournalError('actual PostgreSQL dump did not complete')
            state = postgres._request('GET', '/exec/' + exec_id + '/json')
            if (state.get('Running') is not False or state.get('ExitCode') != 0 or
                    not select.select([pidfd], [], [], 0)[0]):
                raise JournalError('actual PostgreSQL dump exit was not joined')
            stream = outcome['stream']
        fcntl.fcntl(dump_fd, fcntl.F_ADD_SEALS, ALL_SEALS)
        if fcntl.fcntl(dump_fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS:
            raise JournalError('actual PostgreSQL dump could not be sealed')
        readonly = os.open('/proc/self/fd/%d' % dump_fd, os.O_RDONLY | os.O_CLOEXEC)
        os.close(dump_fd)
        dump_fd = None
        observed.update(stream=stream, pidfd_exited=True, exec_exit_code=0, sealed=True, readonly=True,
                        ownership_and_acl_included=preserve_catalog)
        life.journal.append('registration', {'name': 'negative-pg-dump-complete',
                                              'identity_digest': hashlib.sha256(canonical(observed)).hexdigest()})
        return readonly, observed
    finally:
        if lock.thread is not None and lock.thread.is_alive():
            lock.close()
        if pidfd is not None:
            if not select.select([pidfd], [], [], 0)[0]:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
            os.close(pidfd)
        if thread is not None and thread.is_alive():
            connection = outcome.get('connection')
            if connection is not None:
                connection.close()
            thread.join(2)
        if dump_fd is not None:
            os.close(dump_fd)


def _physical_census(roots):
    """Independently open and hash every entry under the fixed root store."""
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    root = os.open(BACKUP_ROOT, flags)
    try:
        root_info = os.fstat(root)
        root_mount = _mount_id(root)
        if (root_info.st_uid != 0 or root_info.st_gid != 0 or stat.S_IMODE(root_info.st_mode) != 0o700 or
                (root_info.st_dev, root_info.st_ino, root_mount) != roots._identities[('managed-backups',)]):
            raise JournalError('backup root ownership differs')
        if set(os.listdir(root)) != set(BACKUP_CHILDREN):
            raise JournalError('backup root child set differs')
        files, directories = [], []
        def visit(directory, prefix):
            info = os.fstat(directory)
            if info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700:
                raise JournalError('backup census directory ownership differs')
            for name in sorted(os.listdir(directory)):
                item = os.stat(name, dir_fd=directory, follow_symlinks=False)
                path = prefix + '/' + name if prefix else name
                if stat.S_ISDIR(item.st_mode):
                    directories.append(path)
                    child = os.open(name, flags, dir_fd=directory)
                    try:
                        now = os.fstat(child)
                        expected_identity = roots._identities.get(('managed-backups',) + tuple(path.split('/')))
                        actual_identity = (now.st_dev, now.st_ino, _mount_id(child))
                        if ((now.st_dev, now.st_ino) != (item.st_dev, item.st_ino) or now.st_dev != root_info.st_dev or
                                actual_identity[2] != root_mount or expected_identity is not None and actual_identity != expected_identity):
                            raise JournalError('backup census directory changed or crossed mount')
                        visit(child, path)
                    finally:
                        os.close(child)
                elif stat.S_ISREG(item.st_mode):
                    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
                    try:
                        before = os.fstat(fd)
                        if ((before.st_dev, before.st_ino) != (item.st_dev, item.st_ino) or
                                before.st_dev != root_info.st_dev or before.st_uid != 0 or before.st_gid != 0 or
                                stat.S_IMODE(before.st_mode) != 0o600 or _mount_id(fd) != root_mount or
                                before.st_size > MAX_DUMP):
                            raise JournalError('backup artifact identity differs')
                        digest = hashlib.sha256()
                        size = 0
                        while True:
                            chunk = os.read(fd, 1024 * 1024)
                            if not chunk:
                                break
                            digest.update(chunk)
                            size += len(chunk)
                        after = os.fstat(fd)
                        if (before.st_size != size or (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns) !=
                                (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns)):
                            raise JournalError('backup artifact changed during census')
                        files.append({'path': path, 'size': size, 'sha256': digest.hexdigest()})
                    finally:
                        os.close(fd)
                else:
                    raise JournalError('backup census found non-regular entry')
        visit(root, '')
        return files, directories
    finally:
        os.close(root)


def backup_negative(life, roots, postgres, invoker, host, mode, require_forbidden_exec=False, preserve_catalog=False):
    rollback = life.invalid and life.closed_maintenance and life.invalid_reason == 'negative_app_rollback'
    if (mode not in ('staging', 'published') or type(require_forbidden_exec) is not bool or
            type(preserve_catalog) is not bool or preserve_catalog != rollback or
            life.stage != 'source_maintenance' or life.invalid and not rollback):
        raise JournalError('backup negative selected outside the admitted realm')
    if rollback:
        binding = strict_json(life.journal._app)
        roots_digest = binding['roots_digest']
    else:
        roots_digest = roots.scan()
        life.invalidate('negative_backup_producer')
    dump_fd = config_fd = None
    try:
        dump_fd, dump = capture_dump(life, postgres, host, require_forbidden_exec=require_forbidden_exec,
                                     preserve_catalog=preserve_catalog)
        config = {'version': 1, 'mode': mode, 'dump_size': dump['stream']['size'],
                  'dump_sha256': dump['stream']['sha256']}
        config_fd, config_digest = sealed_json(config)
        result = invoker.backup_negative(config_fd, config_digest, dump_fd, time.monotonic() + 60)
        frame = strict_json(result['stdout']) if result['stdout'] else None
        artifact = {'path': mode + '/postgres_dump/db.v1', 'size': dump['stream']['size'],
                    'sha256': dump['stream']['sha256']}
        expected = {'version': 1, 'status': 'staged' if mode == 'staging' else 'published', 'artifact': artifact}
        if result['status'] != 0 or result['stderr'] or frame != expected or type(frame.get('version')) is not int:
            raise JournalError('pinned LocalStore producer failed')
        files, directories = _physical_census(roots)
        expected_dirs = sorted(list(BACKUP_CHILDREN) + ['staging/postgres_dump'] +
                               (['published/postgres_dump'] if mode == 'published' else []))
        if files != [artifact] or sorted(directories) != expected_dirs:
            raise JournalError('independent physical backup census differs from actual dump')
        try:
            roots.scan()
        except Exception:
            sticky = True
        else:
            sticky = False
        if not sticky or not life.invalid:
            raise JournalError('backup producer did not permanently invalidate realm')
        return {'mode': mode, 'roots_digest_before': roots_digest, 'dump': dump, 'producer': frame,
                'producer_pid': result['pid'], 'producer_starttime': result['starttime'],
                'physical_files': files, 'physical_directories': sorted(directories), 'sticky_invalid': True}
    finally:
        if config_fd is not None:
            os.close(config_fd)
        if dump_fd is not None:
            os.close(dump_fd)
