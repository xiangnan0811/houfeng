"""Root-owned primitives for durable maintenance record observations.

The coordinator retains its private files.  Witnesses contain their sizes and
hashes, never their contents.  A journal cannot be reopened by a new root
supervisor after a crash.
"""
import hashlib
import copy
import base64
import os
import re
import stat
import select
import time

import network_verify
from origins import _identity as process_identity, _stat as process_stat, _thread_credentials
from permit import _coordinator_live, _held_guard

from contract import LEDGER_DIR, REALM_CONTROL, RUN_TOKEN
from journal import JournalError, _directory, _read, _write_new, boot_id, canonical, process_start, strict_json

MAX_RECORD = 8 * 1024 * 1024
_TEMP = re.compile(r'^\.tmp-[0-9a-f]{32}$')
_RID = re.compile(r'^[a-z0-9_-]{1,128}$')
_REQUEST_KEYS = frozenset(('version', 'op', 'family', 'recovery_id', 'application_digest',
                           'phase_generation', 'attempt', 'kind', 'record_sha256', 'nonce'))
_MIGRATION_KINDS = frozenset(('begin', 'begin-settled', 'intent', 'phase', 'preparation', 'ready',
    'ready-abandoned', 'normal', 'restrict', 'observe-intent', 'observe-normal', 'observe-restrict',
    'observe-settled', 'compensate', 'abort', 'successor', 'activation', 'terminal', 'revocation-unresolved'))
_SETTLEMENT_KINDS = frozenset(('begin-settled', 'ready-abandoned'))


def checkpoint_request(request, recovery_id, application_digest, nonces):
    if (type(request) is not dict or frozenset(request) != (_REQUEST_KEYS | (
            {'current_backend'} if request.get('kind') in _SETTLEMENT_KINDS else set())) or
            type(request['version']) is not int or request['version'] != 1 or
            request['op'] != 'maintenance_checkpoint' or request['family'] != 'migration' or
            type(request['recovery_id']) is not str or _RID.fullmatch(request['recovery_id']) is None or
            request['recovery_id'] != recovery_id or request['application_digest'] != application_digest or
            not _digest(request['application_digest']) or not _digest(request['record_sha256']) or
            type(request['phase_generation']) is not int or not 0 < request['phase_generation'] < (1 << 63) or
            type(request['attempt']) is not int or not 0 <= request['attempt'] < (1 << 64) or
            type(request['kind']) is not str or request['kind'] not in _MIGRATION_KINDS or
            type(request['nonce']) is not str or _HEX.fullmatch(request['nonce']) is None or
            request['nonce'] in nonces or len(nonces) >= 4096):
        raise JournalError('maintenance checkpoint request invalid')
    observation = request['kind'].startswith('observe-')
    if observation != (request['attempt'] != 0):
        raise JournalError('maintenance checkpoint attempt invalid')
    if request['kind'] in _SETTLEMENT_KINDS:
        from pg_bootstrap import _settlement_backend
        _settlement_backend(request['current_backend'])
    nonces.add(request['nonce'])

def disposition_request(request, recovery_id, application_digest, nonces):
    if (type(request) is not dict or frozenset(request) != _REQUEST_KEYS or
            type(request['version']) is not int or request['version'] != 1 or
            request['op'] != 'maintenance_ready_disposition' or request['family'] != 'migration' or
            request['recovery_id'] != recovery_id or type(request['recovery_id']) is not str or
            _RID.fullmatch(request['recovery_id']) is None or
            request['application_digest'] != application_digest or not _digest(application_digest) or
            type(request['phase_generation']) is not int or not 0 < request['phase_generation'] < (1 << 63) or
            type(request['attempt']) is not int or request['attempt'] != 0 or request['kind'] != 'ready' or
            not _digest(request['record_sha256']) or type(request['nonce']) is not str or
            _HEX.fullmatch(request['nonce']) is None or request['nonce'] in nonces or len(nonces) >= 4096):
        raise JournalError('maintenance ready disposition request invalid')
    nonces.add(request['nonce'])


def checkpoint_name(request):
    kind, generation, attempt = request['kind'], request['phase_generation'], request['attempt']
    if kind in ('phase', 'terminal'):
        return 'phase-%020d.json' % generation
    if kind.startswith('observe-'):
        return 'migration-%s-%020d-%020d.json' % (kind, generation, attempt)
    return 'migration-%s-%020d.json' % (kind, generation)
_COMPONENT = re.compile(r'^[A-Za-z0-9_.-]{1,255}$')
_HEX = re.compile(r'^[0-9a-f]{64}$')


def _identity(s):
    return (s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid, s.st_nlink,
            s.st_size, s.st_mtime_ns, s.st_ctime_ns)


def _private_directory(s):
    return (stat.S_ISDIR(s.st_mode) and s.st_uid == 10002 and s.st_gid == 10002 and
            stat.S_IMODE(s.st_mode) == 0o700 and s.st_nlink > 0)


def _component(name):
    if type(name) is not str or name in ('.', '..') or _COMPONENT.fullmatch(name) is None:
        raise JournalError('invalid maintenance record component')


def _digest(value):
    return type(value) is str and _HEX.fullmatch(value) is not None and value != '0' * 64


class RecordDirectory:
    """Held no-follow path to the fixed coordinator ledger directory.

    Ancestors and the final directory are checked again on every operation.
    Paths supplied to read() are limited to one direct child, or the fixed
    sessions/<RID>/<file> layout; no symlink is followed.
    """
    def __init__(self):
        self._fds = []
        self._parts = LEDGER_DIR.strip('/').split('/')
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        self._fds.append(fd)
        try:
            for part in self._parts:
                _component(part)
                child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                self._fds.append(child)
                fd = child
                s = os.fstat(fd)
                if (not stat.S_ISDIR(s.st_mode) or s.st_uid not in (0, 10002) or
                        s.st_mode & 0o022 or s.st_nlink == 0):
                    raise JournalError('maintenance ancestor is not protected')
            if not _private_directory(os.fstat(fd)):
                raise JournalError('maintenance ledger directory is not private')
            self._pins = [(os.fstat(item).st_dev, os.fstat(item).st_ino) for item in self._fds]
            self.check()
        except BaseException:
            self.close()
            raise

    def check(self):
        if len(self._fds) != len(self._parts) + 1:
            raise JournalError('maintenance ledger directory is closed')
        for index, fd in enumerate(self._fds):
            current = os.fstat(fd)
            if (current.st_dev, current.st_ino) != self._pins[index] or not stat.S_ISDIR(current.st_mode):
                raise JournalError('maintenance directory identity changed')
            if index:
                named = os.stat(self._parts[index - 1], dir_fd=self._fds[index - 1], follow_symlinks=False)
                if (named.st_dev, named.st_ino) != self._pins[index] or not stat.S_ISDIR(named.st_mode):
                    raise JournalError('maintenance directory was replaced')
                if current.st_uid not in (0, 10002) or current.st_mode & 0o022 or current.st_nlink == 0:
                    raise JournalError('maintenance directory protection changed')
        if not _private_directory(os.fstat(self._fds[-1])):
            raise JournalError('maintenance ledger directory protection changed')

    def _open_parent(self, parts):
        self.check()
        if len(parts) not in (1, 3) or len(parts) == 3 and parts[0] != 'sessions':
            raise JournalError('maintenance record location is not fixed')
        for part in parts:
            _component(part)
        fd = os.dup(self._fds[-1])
        pins = []
        try:
            for name in parts[:-1]:
                child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                s = os.fstat(child)
                if not _private_directory(s):
                    os.close(child)
                    raise JournalError('maintenance session directory is not private')
                pins.append((fd, name, child, s.st_dev, s.st_ino))
                fd = child
            return fd, pins
        except BaseException:
            os.close(fd)
            for parent, _, _, _, _ in pins:
                os.close(parent)
            raise

    def read(self, path):
        if type(path) is not str:
            raise JournalError('maintenance record name invalid')
        parts = path.split('/')
        parent, pins = self._open_parent(parts)
        try:
            name = parts[-1]
            item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK, dir_fd=parent)
            try:
                before = os.fstat(item)
                if (not stat.S_ISREG(before.st_mode) or (before.st_uid, before.st_gid) != (10002, 10002) or
                        stat.S_IMODE(before.st_mode) != 0o600 or before.st_nlink not in (1, 2) or
                        not 0 < before.st_size <= MAX_RECORD):
                    raise JournalError('maintenance record identity invalid')
                if before.st_nlink == 2:
                    matches = []
                    for candidate in os.listdir(parent):
                        if _TEMP.fullmatch(candidate) is None:
                            continue
                        st = os.stat(candidate, dir_fd=parent, follow_symlinks=False)
                        if (st.st_dev, st.st_ino) == (before.st_dev, before.st_ino):
                            matches.append(candidate)
                    if len(matches) != 1:
                        raise JournalError('maintenance record hardlink gap is invalid')
                data = bytearray()
                while len(data) < before.st_size:
                    block = os.read(item, before.st_size - len(data))
                    if not block:
                        raise JournalError('maintenance record shortened')
                    data.extend(block)
                after = os.fstat(item)
                named = os.stat(name, dir_fd=parent, follow_symlinks=False)
                if _identity(before) != _identity(after) or _identity(before) != _identity(named):
                    raise JournalError('maintenance record changed while read')
                for held, component, child, dev, ino in pins:
                    named_dir = os.stat(component, dir_fd=held, follow_symlinks=False)
                    opened_dir = os.fstat(child)
                    if (not _private_directory(named_dir) or not _private_directory(opened_dir) or
                            (named_dir.st_dev, named_dir.st_ino) != (dev, ino) or
                            (opened_dir.st_dev, opened_dir.st_ino) != (dev, ino)):
                        raise JournalError('maintenance session directory changed')
                self.check()
                return bytes(data)
            finally:
                os.close(item)
        finally:
            os.close(parent)
            for held, _, _, _, _ in pins:
                os.close(held)
    def inventory(self, exclude=()):
        """Read the complete private ledger inventory without exporting bytes."""
        self.check()
        excluded = frozenset(exclude)
        entries = []
        directories = {}

        def visit(fd, prefix, depth):
            before = os.fstat(fd)
            if not _private_directory(before):
                raise JournalError('maintenance inventory directory invalid')
            directories[prefix] = (before.st_dev, before.st_ino)
            names = sorted(os.listdir(fd))
            if len(names) > 65536:
                raise JournalError('maintenance inventory exceeds limit')
            temps = []
            for name in names:
                _component(name)
                path = prefix + name
                st = os.stat(name, dir_fd=fd, follow_symlinks=False)
                if stat.S_ISDIR(st.st_mode):
                    if not (depth == 0 and name == 'sessions' or depth == 1 and prefix == 'sessions/'):
                        raise JournalError('unexpected maintenance inventory directory')
                    child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                    try:
                        visit(child, path + '/', depth + 1)
                        named = os.stat(name, dir_fd=fd, follow_symlinks=False)
                        if (named.st_dev, named.st_ino) != (st.st_dev, st.st_ino):
                            raise JournalError('maintenance inventory directory replaced')
                    finally:
                        os.close(child)
                elif _TEMP.fullmatch(name):
                    temps.append((name, st))
                elif path not in excluded:
                    raw = self.read(path)
                    entries.append({'name': path, 'sha256': hashlib.sha256(raw).hexdigest()})
            for _, temporary in temps:
                if (not stat.S_ISREG(temporary.st_mode) or temporary.st_nlink != 2 or
                        sum(1 for name in names if _TEMP.fullmatch(name) is None and
                            (lambda item: (item.st_dev, item.st_ino) == (temporary.st_dev, temporary.st_ino))(
                                os.stat(name, dir_fd=fd, follow_symlinks=False))) != 1):
                    raise JournalError('unrecognized maintenance temporary file')
            after = os.fstat(fd)
            if (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino) or names != sorted(os.listdir(fd)):
                raise JournalError('maintenance inventory changed while read')

        visit(self._fds[-1], '', 0)
        entries.sort(key=lambda item: item['name'])
        self.check()
        return entries, directories


    def close(self):
        while self._fds:
            os.close(self._fds.pop())


class MaintenanceJournal:
    """Append-only witness chain owned by the current root supervisor."""
    def __init__(self, realm_journal, guard, cgroup):
        if os.geteuid() != 0 or os.getegid() != 0:
            raise JournalError('root supervisor required')
        self.pid = os.getpid()
        self.starttime = process_start(self.pid)
        self.boot = boot_id()
        self.guard = guard.copy()
        self.cgroup = cgroup.copy()
        self.sequence = 0
        self.previous = '0' * 64
        self.entries = []
        self.keys = {}
        self.pending_append = None
        self.fd = None
        root = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            self.root_identity = _directory(root)
            os.mkdir('maintenance', 0o700, dir_fd=root)
            os.fsync(root)
            self.fd = os.open('maintenance', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
            self.identity = _directory(self.fd)
            named = os.stat('maintenance', dir_fd=root, follow_symlinks=False)
            if (named.st_dev, named.st_ino) != self.identity:
                raise JournalError('maintenance witness directory differs')
            realm_journal.register_maintenance(self.fd)
        except BaseException:
            if self.fd is not None:
                os.close(self.fd)
                self.fd = None
            raise
        finally:
            os.close(root)

    def verify(self):
        if (self.fd is None or os.getpid() != self.pid or process_start(self.pid) != self.starttime or
                boot_id() != self.boot):
            raise JournalError('maintenance supervisor continuity unavailable')
        root = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            named = os.stat('maintenance', dir_fd=root, follow_symlinks=False)
            if (_directory(root) != self.root_identity or _directory(self.fd) != self.identity or
                    not stat.S_ISDIR(named.st_mode) or (named.st_dev, named.st_ino) != self.identity):
                raise JournalError('maintenance witness directory changed')
        finally:
            os.close(root)
        self._reconcile_append()
        names = {'witness-%020d.json' % n for n in range(1, self.sequence + 1)}
        if set(os.listdir(self.fd)) != names:
            raise JournalError('maintenance witness inventory changed')
        previous = '0' * 64
        for index, expected in enumerate(self.entries, 1):
            raw = _read(self.fd, 'witness-%020d.json' % index)
            item = strict_json(raw)
            if raw != expected or item.get('sequence') != index or item.get('previous') != previous:
                raise JournalError('maintenance witness chain changed')
            previous = hashlib.sha256(raw).hexdigest()
        if previous != self.previous:
            raise JournalError('maintenance witness tail changed')

    def _reconcile_append(self):
        pending = self.pending_append
        if pending is None:
            return
        old_entries, old_keys, old_sequence, old_previous, key, body = pending
        sequence = old_sequence + 1
        witness = hashlib.sha256(body).hexdigest()
        event = strict_json(body)
        new_entries = old_entries + [body]
        new_keys = dict(old_keys)
        new_keys[key] = (event['record_sha256'], event['record_name'], sequence, witness)
        if (self.entries not in (old_entries, new_entries) or self.keys not in (old_keys, new_keys) or
                self.sequence not in (old_sequence, sequence) or self.previous not in (old_previous, witness)):
            raise JournalError('maintenance pending witness cache differs')
        name = 'witness-%020d.json' % sequence
        # A failed O_EXCL before creation may be retried only against the same
        # pinned directory and exactly the complete, unmodified old chain.
        try:
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=self.fd)
        except FileNotFoundError:
            root = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                named = os.stat('maintenance', dir_fd=root, follow_symlinks=False)
                if (_directory(root) != self.root_identity or _directory(self.fd) != self.identity or
                        not stat.S_ISDIR(named.st_mode) or (named.st_dev, named.st_ino) != self.identity or
                        set(os.listdir(self.fd)) != {'witness-%020d.json' % n for n in range(1, old_sequence + 1)}):
                    raise JournalError('maintenance pending witness directory differs')
                prior = '0' * 64
                for index, expected in enumerate(old_entries, 1):
                    actual = _read(self.fd, 'witness-%020d.json' % index)
                    item = strict_json(actual)
                    if actual != expected or item.get('sequence') != index or item.get('previous') != prior:
                        raise JournalError('maintenance pending predecessor chain differs')
                    prior = hashlib.sha256(actual).hexdigest()
                if prior != old_previous or len(old_entries) != old_sequence:
                    raise JournalError('maintenance pending predecessor tail differs')
                if _write_new(self.fd, name, event) != body:
                    raise JournalError('maintenance pending witness publication differs')
            finally:
                os.close(root)
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=self.fd)
        try:
            before = os.fstat(fd)
            if (_read(self.fd, name) != body or
                    _identity(before) != _identity(os.stat(name, dir_fd=self.fd, follow_symlinks=False))):
                raise JournalError('maintenance pending witness bytes or identity differ')
            os.fsync(fd)
            os.fsync(self.fd)
            if (_identity(before) != _identity(os.fstat(fd)) or
                    _identity(before) != _identity(os.stat(name, dir_fd=self.fd, follow_symlinks=False)) or
                    _read(self.fd, name) != body):
                raise JournalError('maintenance pending witness changed during settlement')
        finally:
            os.close(fd)
        root = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            named = os.stat('maintenance', dir_fd=root, follow_symlinks=False)
            if (_directory(root) != self.root_identity or _directory(self.fd) != self.identity or
                    not stat.S_ISDIR(named.st_mode) or (named.st_dev, named.st_ino) != self.identity or
                    set(os.listdir(self.fd)) != {'witness-%020d.json' % n for n in range(1, sequence + 1)}):
                raise JournalError('maintenance witness directory changed during settlement')
        finally:
            os.close(root)
        self.entries, self.keys = new_entries, new_keys
        self.sequence, self.previous = sequence, witness
        self.pending_append = None

    def append(self, family, recovery_id, application_digest, generation, attempt, kind, record_name,
               raw, predecessor_witness_digest, live_observation_digest, ready_disposition=None):
        self.verify()
        key = (family, recovery_id, application_digest, generation, attempt, kind)
        digest = hashlib.sha256(raw).hexdigest()
        if key in self.keys:
            old_digest, old_name, sequence, witness = self.keys[key]
            prior = strict_json(self.entries[sequence - 1])
            if (digest != old_digest or record_name != old_name or
                    prior.get('ready_disposition') != ready_disposition):
                raise JournalError('maintenance witness retry differs')
            return sequence, witness
        if (not _digest(application_digest) or not _digest(digest) or
                predecessor_witness_digest is not None and not _digest(predecessor_witness_digest) or
                live_observation_digest is not None and not _digest(live_observation_digest)):
            raise JournalError('maintenance witness digest invalid')
        if kind == 'ready-disposition':
            if (family != 'migration' or attempt != 0 or live_observation_digest is not None or
                    type(ready_disposition) is not dict or set(ready_disposition) != {
                        'outcome', 'ready_witness_sequence', 'ready_witness_digest', 'scope_retirement_digest'} or
                    ready_disposition['outcome'] not in ('witnessed', 'unwitnessed_retired') or
                    not _digest(ready_disposition['scope_retirement_digest'])):
                raise JournalError('maintenance ready disposition event invalid')
            seq, ref = ready_disposition['ready_witness_sequence'], ready_disposition['ready_witness_digest']
            if (ready_disposition['outcome'] == 'witnessed' and
                    (type(seq) is not int or seq <= 0 or not _digest(ref)) or
                    ready_disposition['outcome'] == 'unwitnessed_retired' and (seq is not None or ref is not None)):
                raise JournalError('maintenance ready disposition reference invalid')
        elif ready_disposition is not None:
            raise JournalError('unexpected maintenance disposition event')
        event = {'version': 1, 'sequence': self.sequence + 1, 'previous': self.previous,
                 'run_token': RUN_TOKEN, 'boot_id': self.boot,
                 'supervisor': {'pid': self.pid, 'starttime': self.starttime},
                 'guard': self.guard, 'cgroup': self.cgroup, 'family': family,
                 'recovery_id': recovery_id, 'application_digest': application_digest,
                 'phase_generation': generation, 'attempt': attempt, 'kind': kind,
                 'record_name': record_name, 'record_bytes': len(raw), 'record_sha256': digest,
                 'predecessor_witness_digest': predecessor_witness_digest,
                 'live_observation_digest': live_observation_digest}
        if ready_disposition is not None:
            event['ready_disposition'] = copy.deepcopy(ready_disposition)
        body = canonical(event)
        self.pending_append = (list(self.entries), dict(self.keys), self.sequence, self.previous, key, body)
        if _write_new(self.fd, 'witness-%020d.json' % event['sequence'], event) != body:
            raise JournalError('maintenance witness publication differs')
        self._reconcile_append()
        return self.sequence, self.previous

    def public_witnesses(self):
        self.verify()
        return [strict_json(body) for body in self.entries]
    def public_witness_bytes(self):
        self.verify()
        return list(self.entries)


    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None


def observe_migration_child(coordinator, coordinator_pidfd, guard, cgroup, program_sha256,
                            child, backend, verify_backend, deadline):
    """Return an observation digest and held pidfd for the blocked migrator.

    verify_backend is a root-owned independent observer of the actual
    PostgreSQL backend tuple. The child is waiting for the fixed coordinator's
    ready acknowledgement. The caller owns the pidfd until retirement.
    """
    if (not callable(verify_backend) or type(child) is not dict or
            frozenset(child) != frozenset(('pid', 'starttime', 'executable_sha256')) or
            type(child['pid']) is not int or not 0 < child['pid'] < (1 << 31) or
            type(child['starttime']) is not int or not 0 < child['starttime'] < (1 << 64) or
            child['executable_sha256'] != program_sha256 or not _digest(program_sha256) or
            type(backend) is not dict or frozenset(backend) != frozenset(('pid', 'started', 'role')) or
            type(backend['pid']) is not int or not 0 < backend['pid'] < (1 << 31) or
            type(backend['started']) is not str or not backend['started'] or
            type(backend['role']) is not str or not backend['role'] or time.monotonic() >= deadline):
        raise JournalError('migration ready identity invalid')
    if select.select([coordinator_pidfd], [], [], 0)[0]:
        raise JournalError('migration coordinator exited')
    _coordinator_live(coordinator)
    _held_guard(guard, coordinator)
    pidfd = os.pidfd_open(child['pid'], 0)
    try:
        if select.select([pidfd], [], [], 0)[0]:
            raise JournalError('migration child already exited')
        actual = process_identity(child['pid'], full=True)
        state = process_stat(child['pid'])
        if (actual is None or actual['parent'] != coordinator['pid'] or
                actual['starttime'] != child['starttime'] or actual['exe'] != '/opt/g2/c81-migrator' or
                actual['exe_sha256'] != program_sha256 or actual['argv'] != [b'/opt/g2/c81-migrator'] or
                actual['cgroup'] != '0::/g2-c81-migration' or state['state'] not in (b'S', b'D') or
                network_verify.migration_scope(cgroup) != {
                    'members': sorted([coordinator['pid'], child['pid']]), 'populated': 1}):
            raise JournalError('migration live executable or scope differs')
        _thread_credentials(child['pid'], 1)
        observation = verify_backend(backend, deadline)
        if observation != backend or time.monotonic() >= deadline:
            raise JournalError('migration backend observation differs')
        again = process_identity(child['pid'], full=True)
        if (again != actual or select.select([pidfd, coordinator_pidfd], [], [], 0)[0] or
                process_stat(child['pid'])['state'] not in (b'S', b'D') or
                network_verify.migration_scope(cgroup) != {
                    'members': sorted([coordinator['pid'], child['pid']]), 'populated': 1}):
            raise JournalError('migration child changed during observation')
        _coordinator_live(coordinator)
        _held_guard(guard, coordinator)
        evidence = {'child': child, 'backend': backend, 'parent': coordinator,
                    'exe_dev': actual['exe_dev'], 'exe_ino': actual['exe_ino'],
                    'cgroup': cgroup, 'pidfd_live': True}
        return hashlib.sha256(canonical(evidence)).hexdigest(), pidfd, evidence
    except BaseException:
        os.close(pidfd)
        raise


class RecordContinuity:
    """Pin the complete pre-begin inventory and every witnessed extension."""
    def __init__(self, directory, begin_name, expected_digest):
        if not _digest(expected_digest):
            raise JournalError('begin inventory digest invalid')
        self.directory = directory
        entries, self.directories = directory.inventory(exclude=(begin_name,))
        if any(entry['name'].startswith('migration-') for entry in entries):
            raise JournalError('preexisting migration records have no root witness')
        # The producer uses json.Marshal over structs with name before sha256.
        import json
        body = json.dumps(entries, ensure_ascii=True, separators=(',', ':')).encode('ascii')
        if hashlib.sha256(body).hexdigest() != expected_digest:
            raise JournalError('begin inventory differs from the actual protected directory')
        self.seen = {entry['name']: entry['sha256'] for entry in entries}
        self.invalid = False

    def observe(self, name, digest, pending=None):
        if self.invalid or not _digest(digest):
            raise JournalError('maintenance record continuity unavailable')
        try:
            entries, directories = self.directory.inventory()
            actual = {entry['name']: entry['sha256'] for entry in entries}
            if directories != self.directories:
                raise JournalError('maintenance inventory directory identity changed')
            if any(actual.get(path) != value for path, value in self.seen.items()):
                raise JournalError('a previously seen maintenance record changed or disappeared')
            extra = set(actual) - set(self.seen)
            pending = pending or {}
            if (extra - ({name} | set(pending)) or actual.get(name) != digest or
                    any(actual.get(path) != value for path, value in pending.items())):
                raise JournalError('maintenance publication prefix differs')
            return actual
        except BaseException:
            self.invalid = True
            raise

    def accept(self, name, digest, pending=None):
        self.observe(name, digest, pending)
        self.seen[name] = digest


def application_digest(application):
    import json
    from phase import application as valid_application
    if not valid_application(application):
        raise JournalError('maintenance application identity invalid')
    ordered = {key: application[key] for key in ('ID', 'SystemIdentifier', 'DatabaseName', 'DatabaseOID', 'Epoch')}
    text = json.dumps(ordered, ensure_ascii=False, separators=(',', ':'))
    text = text.replace('&', r'\u0026').replace('<', r'\u003c').replace('>', r'\u003e')
    text = text.replace('\u2028', r'\u2028').replace('\u2029', r'\u2029')
    return hashlib.sha256(text.encode('utf-8', 'strict')).hexdigest()


def read_begin(directory, request, application, coordinator, guard, cgroup, current_boot, continuity=None):
    """Validate begin against independently read phase and session bytes."""
    if request['kind'] != 'begin' or request['attempt'] != 0:
        raise JournalError('first maintenance checkpoint is not begin')
    name = checkpoint_name(request)
    raw = directory.read(name)
    if hashlib.sha256(raw).hexdigest() != request['record_sha256']:
        raise JournalError('begin digest differs')
    item = strict_json(raw)
    fields = frozenset(('version', 'family', 'recovery_id', 'application', 'phase_generation',
                        'phase', 'inventory_digest', 'session_digest', 'original_inventory_digest',
                        'management_credential_digest', 'primary', 'witness', 'coordinator',
                        'guard', 'boot_id', 'cgroup'))
    generation = request['phase_generation']
    if (type(item) is not dict or frozenset(item) != fields or type(item['version']) is not int or
            item['version'] != 1 or item['family'] != 'migration' or
            item['recovery_id'] != request['recovery_id'] or item['application'] != application or
            type(item['phase_generation']) is not int or item['phase_generation'] != generation or
            item['coordinator'] not in coordinator or item['guard'] != guard or item['cgroup'] != cgroup or
            item['boot_id'] != current_boot or application_digest(application) != request['application_digest'] or
            any(not _digest(item[key]) for key in ('inventory_digest', 'session_digest',
                                                   'original_inventory_digest', 'management_credential_digest'))):
        raise JournalError('maintenance begin binding invalid')
    domain_schema = _fields(ID=str, Kind=str, SystemIdentifier=str, DatabaseOID=int, DatabaseName=str, Epoch=int)
    if (not _shape(item['primary'], domain_schema) or not _shape(item['witness'], domain_schema) or
            not _numeric_bounds(item['primary']) or not _numeric_bounds(item['witness']) or
            item['primary']['Kind'] != 'deletion_ledger' or item['witness']['Kind'] != 'deletion_witness' or
            item['primary']['ID'] == item['witness']['ID'] or
            item['primary']['SystemIdentifier'] == item['witness']['SystemIdentifier'] or
            not _shape(item['coordinator'], _PROC) or not _shape(item['guard'], _GUARD) or
            not _shape(item['cgroup'], _CGROUP)):
        raise JournalError('maintenance begin physical identity schema invalid')
    phase_name = 'phase-%020d.json' % (generation - 1)
    phase = strict_json(directory.read(phase_name))
    from phase import read_phases
    if read_phases(application, request['recovery_id']) != phase:
        raise JournalError('maintenance begin phase chain differs')
    if (type(phase) is not dict or set(phase) != {'version', 'generation', 'previous', 'recovery_id', 'application', 'phase'} or
            type(phase['version']) is not int or phase['version'] != 1 or
            type(phase['generation']) is not int or phase['generation'] != generation - 1 or
            phase['application'] != application or phase['recovery_id'] != request['recovery_id'] or
            phase['phase'] != 'quiescent' or item['phase'] != phase):
        raise JournalError('maintenance begin phase differs')
    base = 'sessions/' + request['recovery_id'] + '/'
    session_raw = directory.read(base + 'session.json')
    inventory_raw = directory.read(base + 'app-inventory.json')
    credential_raw = directory.read(base + 'management-credential.json')
    if (hashlib.sha256(session_raw).hexdigest() != item['session_digest'] or
            hashlib.sha256(inventory_raw).hexdigest() != item['original_inventory_digest'] or
            hashlib.sha256(credential_raw).hexdigest() != item['management_credential_digest']):
        raise JournalError('maintenance begin session bytes differ')
    session, inventory, credential = (strict_json(value) for value in (session_raw, inventory_raw, credential_raw))
    if (type(session) is not dict or session.get('application') != application or
            session.get('primary') != item['primary'] or session.get('witness') != item['witness'] or
            type(inventory) is not dict or inventory.get('application') != application or
            inventory.get('recovery_id') != request['recovery_id'] or
            type(credential) is not dict or credential.get('recovery_id') != request['recovery_id']):
        raise JournalError('maintenance begin session identity differs')
    if continuity is None:
        continuity = RecordContinuity(directory, name, item['inventory_digest'])
    else:
        import json
        entries, directories = directory.inventory(exclude=(name,))
        actual = {entry['name']: entry['sha256'] for entry in entries}
        body = json.dumps(entries, ensure_ascii=True, separators=(',', ':')).encode('ascii')
        if (directories != continuity.directories or actual != continuity.seen or
                hashlib.sha256(body).hexdigest() != item['inventory_digest']):
            raise JournalError('subsequent begin inventory differs from retained history')
    continuity.observe(name, request['record_sha256'])
    return item, raw, continuity


def _fields(**values):
    return values


def _shape(value, schema):
    if schema is _DIGEST_ARRAY:
        return type(value) is list and len(value) == 32 and all(type(x) is int and 0 <= x <= 255 for x in value)
    if isinstance(schema, tuple):
        return any(_shape(value, option) for option in schema)
    if isinstance(schema, list):
        return type(value) is list and all(_shape(item, schema[0]) for item in value)
    if isinstance(schema, dict):
        return type(value) is dict and set(value) == set(schema) and all(
            _shape(value[key], expected) for key, expected in schema.items())
    if schema is _SIGNED:
        return type(value) is int and -(1 << 31) <= value < (1 << 31)
    if schema is int:
        return type(value) is int and 0 <= value < (1 << 64)
    if schema is type(None):
        return value is None
    return type(value) is schema


_SIGNED = object()
_U = int
_S = str
_B = bool
_NULL = type(None)
_APP = _fields(ID=_S, SystemIdentifier=_S, DatabaseName=_S, DatabaseOID=_U, Epoch=_U)
_PROC = _fields(pid=_U, starttime=_U, executable_sha256=_S)
_BACKEND = _fields(pid=_U, started=_S, role=_S)
_GUARD = _fields(dev=_U, ino=_U)
_CGROUP = _fields(path=_S, dev=_U, ino=_U)
_ROLE = _fields(name=_S, oid=_U, superuser=_B, inherit=_B, create_role=_B, create_db=_B,
                login=_B, replication=_B, bypass_rls=_B, connection_limit=_SIGNED,
                verifier=(_S, _NULL), valid_until=(_S, _NULL), connect=_B)
_MEMBER = _fields(role_oid=_U, member_oid=_U, grantor_oid=_U, admin_option=_B,
                  inherit_option=_B, set_option=_B)
_SETTING = _fields(role_oid=_U, database_oid=_U, database=_S, config=([_S], _NULL))
_ACL = _fields(grantor_oid=_U, grantee_oid=_U, privilege=_S, grantable=_B)
_ROLES = _fields(database_oid=_U, database_owner_oid=_U, roles=([_ROLE], _NULL),
                 memberships=([_MEMBER], _NULL), settings=([_SETTING], _NULL),
                 database_acl=([_ACL], _NULL))
_TARGET = _fields(role=_S, secret=_S, verifier=_S)
_CREDENTIAL = _fields(version=_U, recovery_id=_S, role=_S, new_url=_S,
                      previous_verifier=_S, new_verifier=_S)
_DIGEST_ARRAY = [_U]
_MANIFEST_ROW = _fields(ManifestRevision=_U, MigratorCatalogRole=_S, PreviousManifestDigest=_DIGEST_ARRAY,
                        CanonicalMigrationSet=(_S, _NULL), MigrationSetDigest=_DIGEST_ARRAY,
                        CanonicalPrivilegeSet=(_S, _NULL), PrivilegeSetDigest=_DIGEST_ARRAY,
                        ManifestDigest=_DIGEST_ARRAY)
_MANIFEST = _fields(DatabaseName=_S, SessionUser=_S, CurrentUser=_S, Manifests=([_MANIFEST_ROW], _NULL),
                    Head=(_fields(ManifestRevision=_U, ManifestDigest=_DIGEST_ARRAY), _NULL),
                    AppliedMigrations=([_fields(Filename=_S, Checksum=_DIGEST_ARRAY)], _NULL))
_CATALOG_ROLE = _fields(Name=_S, Login=_B, Inherit=_B, Superuser=_B, CreateDatabase=_B,
                        CreateRole=_B, Replication=_B, BypassRLS=_B, TemporaryObjects=_B, SchemaCreate=_B)
_CATALOG_OWNER = _fields(ObjectClass=_S, SchemaName=_S, ObjectIdentity=_S, OwnerRole=_S)
_CATALOG_PRIV = _fields(Grantee=_S, ObjectClass=_S, SchemaName=_S, ObjectIdentity=_S,
                        ColumnName=_S, Privilege=_S, GrantOption=_B)
_CATALOG = _fields(DatabaseName=_S, SessionUser=_S, CurrentUser=_S,
    Roles=([_CATALOG_ROLE], _NULL), Memberships=([_fields(MemberRole=_S, ParentRole=_S)], _NULL),
    Owners=([_CATALOG_OWNER], _NULL), DirectPrivileges=([_CATALOG_PRIV], _NULL),
    EffectivePrivileges=([_CATALOG_PRIV], _NULL),
    ColumnACLs=([_fields(SchemaName=_S, RelationName=_S, ColumnName=_S, Grantee=_S, Privilege=_S, GrantOption=_B)], _NULL),
    DefaultACLs=([_fields(OwnerRole=_S, SchemaName=_S, ObjectType=_S, Grantee=_S, Privilege=_S, GrantOption=_B)], _NULL),
    Functions=([_fields(SchemaName=_S, Name=_S, IdentityArguments=_S, Identity=_S, OwnerRole=_S,
                        Kind=_S, SecurityDefiner=_B, Config=([_S], _NULL))], _NULL),
    PGCryptoExtension=_fields(ExtensionName=_S, SchemaName=_S))
_SNAPSHOT = _fields(LegacyC80=_B, Manifest=_MANIFEST, Catalog=_CATALOG)
_INVENTORY = _fields(version=_U, recovery_id=_S, application=_APP, bootstrap=_S, legacy_c80=_B,
                     admin=_S, previous=([_S], _NULL), manifest_digest=_DIGEST_ARRAY, baseline=_SNAPSHOT,
                     role_baseline=_ROLES, targets=([_TARGET], _NULL), credential=_CREDENTIAL)


def _numeric_bounds(value, key=''):
    if type(value) is dict:
        return all(_numeric_bounds(item, name) for name, item in value.items())
    if type(value) is list:
        return all(_numeric_bounds(item) for item in value)
    if type(value) is not int:
        return True
    lower = key.lower()
    if lower.endswith('oid'):
        return 0 <= value < (1 << 32)
    if lower == 'pid':
        return 0 < value < (1 << 31)
    if lower in ('phase_generation', 'previous_phase_generation', 'terminal_phase_generation', 'generation'):
        return 0 < value < (1 << 63)
    return True


def _inventory_shape(value):
    if type(value) is not dict:
        return False
    fields = dict(_INVENTORY)
    if 'original_previous' in value:
        fields['original_previous'] = ([_S], _NULL)
    return _shape(value, fields) and _numeric_bounds(value)


_BASE = _fields(version=_U, phase_generation=_U, intent_digest=_S)
_OBS = dict(_BASE, attempt=_U, observation_intent_digest=_S)
_RECORD_SCHEMAS = {
    'begin-settled': _fields(version=_U, phase_generation=_U, recovery_id=_S, application=_APP,
        begin_digest=_S, coordinator=_PROC, bootstrap=_BACKEND, catalog_digest=_S, roles_digest=_S, outcome=_S),
    'ready-abandoned': dict(_BASE, recovery_id=_S, application=_APP, preparation_digest=_S,
        ready_digest=_S, coordinator=_PROC, bootstrap=_BACKEND, restricted=_ROLES, catalog_digest=_S, outcome=_S),
    'intent': _fields(version=_U, phase_generation=_U, previous_phase_generation=_U,
        recovery_id=_S, application=_APP, original_inventory_digest=_S, guard=_GUARD, boot_id=_S,
        cgroup=_CGROUP, coordinator=_PROC, program_sha256=_S, runtime_role=_S, admin_role=_S,
        migrator_role=_S, worker_role=_S, worker_absent=_B, bootstrap=_BACKEND),
    'preparation': dict(_BASE, **{'from': _ROLES, 'normal': _ROLES, 'final_target': _ROLES,
        'emergency_target': _ROLES, 'bootstrap': _BACKEND, 'transaction_id': _S, 'worker_target': _TARGET}),
    'ready': dict(_BASE, child=_PROC, backend=_BACKEND),
    'normal': dict(_BASE, preparation_digest=_S, inventory=dict),
    'restrict': dict(_BASE, preparation_digest=_S, **{'from': _ROLES, 'to': _ROLES}),
    'observe-intent': dict(_BASE, attempt=_U, recovery_id=_S, application=_APP, preparation_digest=_S,
        previous_attempt_settled_digest=(_S, _NULL), bootstrap=_BACKEND, worker_oid=_U,
        manifest_digest=_S, catalog_digest=_S, **{'from': _ROLES, 'to': _ROLES,
        'final_target': _ROLES, 'emergency_target': _ROLES}),
    'observe-normal': dict(_OBS, inventory=dict),
    'observe-restrict': dict(_OBS, **{'from': _ROLES, 'to': _ROLES}),
    'observe-settled': dict(_OBS, restriction_digest=_S, outcome=_S, normal_digest=(_S, _NULL)),
    'compensate': dict(_BASE, preparation_digest=_S, worker_oid=_U, target_digest=_S),
    'abort': dict(_BASE, compensation_digest=_S, outcome=_S),
    'activation': dict(_BASE, terminal_phase_generation=_U, recovery_id=_S, application=_APP,
        original_inventory_digest=_S, successor_digest=_S, normal_digest=_S,
        manifest_digest=_S, normal_source=_S, normal_attempt=_U),
    'revocation-unresolved': dict(_BASE, outcome=_S),
}


def validate_record(request, item):
    kind = request['kind']
    if kind == 'successor':
        if not _inventory_shape(item):
            raise JournalError('maintenance successor schema invalid')
        return
    if kind in ('phase', 'terminal'):
        if (not _shape(item, _fields(version=_U, generation=_U, previous=[_U], recovery_id=_S,
                                     application=_APP, phase=_S)) or
                item['version'] != 1 or item['generation'] != request['phase_generation'] or len(item['previous']) != 32 or
                any(not 0 <= byte <= 255 for byte in item['previous'])):
            raise JournalError('maintenance phase schema invalid')
        return
    schema = _RECORD_SCHEMAS.get(kind)
    if schema is None or not _shape(item, schema) or not _numeric_bounds(item):
        raise JournalError('maintenance record schema invalid')
    if (item['version'] != 1 or item['phase_generation'] != request['phase_generation'] or
            'attempt' in item and item['attempt'] != request['attempt']):
        raise JournalError('maintenance record generation differs')
    if kind in ('normal', 'observe-normal') and not _inventory_shape(item['inventory']):
        raise JournalError('maintenance inventory schema invalid')
    for key, value in item.items():
        if key.endswith('_digest') and value is not None and not _digest(value) and not (
                kind == 'compensate' and key == 'preparation_digest' and value == ''):
            raise JournalError('maintenance record digest invalid')


def _fenced_roles(snapshot, targets, admin):
    result = copy.deepcopy(snapshot)
    if type(result['roles']) is not list or type(targets) is not list:
        raise JournalError('maintenance role inventory missing')
    indexed = {target['role']: target for target in targets}
    if len(indexed) != len(targets):
        raise JournalError('maintenance role target duplicated')
    changed, found = set(), set()
    for role in result['roles']:
        target = indexed.get(role['name'])
        if target is None:
            continue
        found.add(role['name'])
        role['verifier'] = target['verifier']
        role['login'] = role['name'] == admin
        if role['name'] != admin:
            role['connect'] = False
            changed.add(role['oid'])
    if found != set(indexed):
        raise JournalError('maintenance target role missing')
    result['database_acl'] = [grant for grant in (snapshot['database_acl'] or [])
                              if grant['privilege'] != 'CONNECT' or grant['grantee_oid'] not in changed]
    return result


def _emergency_roles(snapshot, admin):
    result = copy.deepcopy(snapshot)
    roles = [role for role in result['roles'] if role['name'] == admin]
    if len(roles) != 1:
        raise JournalError('maintenance admin identity missing')
    roles[0]['login'], roles[0]['connect'] = False, False
    result['database_acl'] = [grant for grant in (snapshot['database_acl'] or [])
                              if grant['privilege'] != 'CONNECT' or grant['grantee_oid'] != roles[0]['oid']]
    return result


def _check_preparation(item, intent, inventory):
    baseline = inventory['role_baseline']
    if item['from'] != _fenced_roles(baseline, inventory['targets'], inventory['admin']):
        raise JournalError('migration preparation from-state differs')
    normal = item['normal']
    if type(normal['roles']) is not list or type(baseline['roles']) is not list:
        raise JournalError('migration preparation role inventory missing')
    worker = [role for role in normal['roles'] if role['name'] == intent['worker_role']]
    if len(worker) != 1 or any(role['name'] == intent['worker_role'] for role in baseline['roles']):
        raise JournalError('migration worker identity differs')
    w = worker[0]
    if (not 0 < w['oid'] < (1 << 32) or not w['login'] or w['connect'] or
            any(w[key] for key in ('superuser', 'inherit', 'create_role', 'create_db', 'replication', 'bypass_rls')) or
            not w['verifier'] or item['worker_target']['role'] != intent['worker_role']):
        raise JournalError('migration worker attributes differ')
    reduced = copy.deepcopy(normal)
    reduced['roles'] = [role for role in reduced['roles'] if role['name'] != intent['worker_role']]
    if len(reduced['roles']) != len(baseline['roles']):
        raise JournalError('migration role inventory differs')
    old_verifiers = {role['verifier'] for role in baseline['roles'] if role['verifier'] is not None}
    old_verifiers.update(target['verifier'] for target in inventory['targets'])
    fresh = set()
    for actual, original in zip(reduced['roles'], baseline['roles']):
        if actual['name'] != original['name'] or actual['oid'] != original['oid']:
            raise JournalError('migration role identity differs')
        verifier = actual['verifier']
        if actual['name'] != inventory['bootstrap']:
            if not verifier or verifier in old_verifiers or verifier in fresh:
                raise JournalError('migration role verifier is not fresh')
            fresh.add(verifier)
            actual['verifier'] = original['verifier']
    if reduced != baseline or w['verifier'] in old_verifiers or w['verifier'] in fresh:
        raise JournalError('migration role attributes differ')
    targets = list(inventory['targets']) + [item['worker_target']]
    if item['worker_target']['verifier'] in old_verifiers | fresh | {w['verifier']}:
        raise JournalError('migration worker final verifier is not fresh')
    final = _fenced_roles(normal, targets, inventory['admin'])
    if item['final_target'] != final or item['emergency_target'] != _emergency_roles(final, inventory['admin']):
        raise JournalError('migration cleanup targets differ')


def _post_roles(prepared, current, worker_name):
    fixed = copy.deepcopy(current)
    workers = [role for role in fixed['roles'] if role['name'] == worker_name]
    if len(workers) != 1 or not workers[0]['connect']:
        return False
    worker = workers[0]
    worker['connect'] = False
    grants = [grant for grant in (current['database_acl'] or [])
              if grant['grantee_oid'] == worker['oid'] and grant['privilege'] == 'CONNECT']
    if (len(grants) != 1 or grants[0]['grantor_oid'] != prepared['database_owner_oid'] or
            grants[0]['grantable']):
        return False
    fixed['database_acl'] = [grant for grant in (current['database_acl'] or [])
                             if grant['grantee_oid'] != worker['oid'] or grant['privilege'] != 'CONNECT']
    return fixed == prepared


def _check_observation_intent(item, intent, preparation, inventory, records):
    if (item['bootstrap']['role'] != intent['bootstrap']['role'] or item['bootstrap']['pid'] <= 0 or
            not item['bootstrap']['started'] or item['from'] not in
            (preparation['final_target'], preparation['emergency_target']) or
            item['final_target'] != preparation['final_target'] or
            item['emergency_target'] != preparation['emergency_target']):
        raise JournalError('fresh observation cleanup authorization differs')
    prepared = preparation['normal']
    workers = [role for role in prepared['roles'] if role['name'] == intent['worker_role']]
    if len(workers) != 1 or item['worker_oid'] != workers[0]['oid']:
        raise JournalError('fresh observation worker differs')
    comparison = copy.deepcopy(item['to'])
    old = {role['name']: role for role in prepared['roles']}
    if len(old) != len(prepared['roles']) or len(comparison['roles']) != len(old):
        raise JournalError('fresh observation role inventory differs')
    unavailable = {role['verifier'] for role in inventory['role_baseline']['roles'] if role['verifier']}
    unavailable.update(target['verifier'] for target in inventory['targets'])
    unavailable.add(preparation['worker_target']['verifier'])
    for (kind, _), record in records.items():
        if kind == 'observe-intent':
            unavailable.update(role['verifier'] for role in record[1]['to']['roles'] if role['verifier'])
    used = set()
    for role in comparison['roles']:
        old_role = old.get(role['name'])
        if old_role is None or role['name'] in used:
            raise JournalError('fresh observation role identity differs')
        used.add(role['name'])
        if role['name'] != inventory['bootstrap']:
            verifier = role['verifier']
            if (type(verifier) is not str or not verifier.startswith('SCRAM-SHA-256$') or
                    verifier == old_role['verifier'] or verifier in unavailable):
                raise JournalError('fresh observation verifier is not distinct')
            unavailable.add(verifier)
            role['verifier'] = old_role['verifier']
    if not _post_roles(prepared, comparison, intent['worker_role']):
        raise JournalError('fresh observation normal attributes differ')


def _check_normal_inventory(inventory, original, intent, preparation):
    if (inventory['version'] != original['version'] or inventory['legacy_c80'] or
            inventory['application'] != original['application'] or inventory['recovery_id'] != original['recovery_id'] or
            inventory['bootstrap'] != original['bootstrap'] or inventory['admin'] != original['admin'] or
            inventory.get('original_previous') != original['previous'] or inventory['credential'] != original['credential'] or
            inventory['previous'] != sorted(original['previous'] + [intent['worker_role']]) or
            inventory['targets'] != sorted(original['targets'] + [preparation['worker_target']], key=lambda value: value['role'])):
        raise JournalError('migration normal inventory lineage differs')
    before = original['baseline']['Manifest']['Manifests']
    baseline = inventory['baseline']
    after = baseline['Manifest']['Manifests']
    if (baseline['LegacyC80'] or not before or not after or len(after) != len(before) + 1 or
            after[:-1] != before or after[-1]['PreviousManifestDigest'] != original['manifest_digest'] or
            after[-1]['ManifestDigest'] != inventory['manifest_digest'] or
            after[-1]['MigratorCatalogRole'] != intent['migrator_role'] or
            baseline['Manifest']['Head'] is None or
            baseline['Manifest']['Head']['ManifestDigest'] != inventory['manifest_digest'] or
            not any(inventory['manifest_digest'])):
        raise JournalError('migration normal manifest lineage differs')


_HANDOFF_FACTS = _fields(deployment_id=_S, project_id=_S, source_kind=_S, source_id=_S,
    operation_id=_S, actor_id=_S, app_domain_id=_S, app_system_identifier=_S, app_database_oid=_U,
    app_database_name=_S, app_epoch=_U, source_revision=_S, last_live_scope=_S, final_floor_scope=_S,
    tombstone_unix_microseconds=_U, ledger_domain=_S, ledger_sequence=_U, ledger_entry_hash=_S,
    previous_hash=_S, canonical_entry=_S, token_commitment=_S, request_fingerprint=_S,
    confirmed_at_unix_microseconds=_U, witness_domain_id=_S, witness_proof_digest=_S,
    primary_head_sequence=_U, primary_head_hash=_S, witness_head_sequence=_U, witness_head_hash=_S)
_HANDOFF_INTENT = _fields(version=_U, key=_S, application=_APP, facts=_HANDOFF_FACTS,
    preview_digest=_DIGEST_ARRAY, source_intent_digest=_DIGEST_ARRAY,
    witnessed_digest=_DIGEST_ARRAY, anchored_digest=_DIGEST_ARRAY)
_HANDOFF_COMMAND = dict(_HANDOFF_FACTS, maintenance_rid=_S, maintenance_generation=_U)
_HANDOFF_INSTALL = dict(_HANDOFF_FACTS, first_installation_rid=_S, first_installation_generation=_U)
_HANDOFF_ATTEMPT = _fields(version=_U, number=_U, intent_digest=_DIGEST_ARRAY,
    command=_HANDOFF_COMMAND, backend=_fields(PID=_U, Started=_S, Role=_S))
_HANDOFF_RECEIPT = _fields(version=_U, intent_digest=_DIGEST_ARRAY, attempt=_U, installation=_HANDOFF_INSTALL)
_HEAD = _fields(Sequence=_U, Hash=_DIGEST_ARRAY)
_ANCHOR = _fields(version=_U, index=_U, previous=_DIGEST_ARRAY, primary=_HEAD, witness=_HEAD)
_RECORD_INTENT = _fields(version=_U, key=_S, canonical=_S, primary=_HEAD, witness=_HEAD)
_RECORD_DONE = _fields(version=_U, key=_S, intent_digest=_DIGEST_ARRAY,
                       anchor_index=_U, sequence=_U, hash=_DIGEST_ARRAY)


def _same_source_facts(left, right):
    heads = {'primary_head_sequence', 'primary_head_hash', 'witness_head_sequence', 'witness_head_hash'}
    return all(left[key] == right[key] for key in _HANDOFF_FACTS if key not in heads)


def _record_canonical(encoded):
    try:
        raw = base64.b64decode(encoded, validate=True)
        if base64.b64encode(raw).decode('ascii') != encoded or not 0 < len(raw) <= 1048576:
            raise ValueError('noncanonical base64')
        offset = 0

        def fixed(n):
            nonlocal offset
            if n < 0 or offset + n > len(raw):
                raise ValueError('truncated record')
            result = raw[offset:offset + n]
            offset += n
            return result

        def number():
            return int.from_bytes(fixed(8), 'big')

        def field(maximum):
            length = int.from_bytes(fixed(4), 'big')
            if length > maximum:
                raise ValueError('record field exceeds limit')
            return fixed(length)

        domain = field(128)
        version = number()
        ledger_domain = field(32)
        sequence = number()
        previous = field(32)
        names = [field(128).decode('utf-8', 'strict') for _ in range(8)]
        token, fingerprint, reason = field(32), field(32), field(128).decode('utf-8', 'strict')
        contract, release, micros = number(), number(), number()
        if (offset != len(raw) or domain != b'HOUFENG-RECORD-DELETION-V1' or version != 1 or
                len(ledger_domain) != 32 or not any(ledger_domain) or sequence != 1 or
                len(previous) != 32 or any(previous) or names[0] != 'delete_commit' or
                names[5] != 'record_permanent_delete' or names[6] != 'record' or
                len(token) != 32 or len(fingerprint) != 32 or not any(token) or not any(fingerprint) or
                contract != 1 or release != 0 or not 0 < micros < (1 << 63) or not reason):
            raise ValueError('record identity differs')
        return {'domain': ledger_domain.hex(), 'deployment': names[1], 'project': names[2],
                'operation': names[3], 'actor': names[4], 'object': names[7],
                'token': token.hex(), 'fingerprint': fingerprint.hex(), 'reason': reason}
    except (ValueError, UnicodeError, TypeError) as exc:
        raise JournalError('post-terminal record canonical bytes invalid') from exc


class MigrationCheckpoint:
    """One root supervisor's migration witness state, shared across invocations.

    bind_coordinator must be called after root has retired the old coordinator
    and enrolled the new one in the same pinned cgroup. The root-owned observer
    independently verifies the actual PostgreSQL backend tuple.
    """
    def __init__(self, journal, directory, application, recovery_id, guard, cgroup,
                 program_sha256, verify_backend, verify_settlement):
        from phase import application as valid_application
        if (not isinstance(journal, MaintenanceJournal) or not isinstance(directory, RecordDirectory) or
                not valid_application(application) or type(recovery_id) is not str or
                _RID.fullmatch(recovery_id) is None or guard != journal.guard or cgroup != journal.cgroup or
                not _digest(program_sha256) or not callable(verify_backend) or not callable(verify_settlement)):
            raise JournalError('migration checkpoint registration invalid')
        self.journal, self.directory = journal, directory
        self.application, self.recovery_id = application.copy(), recovery_id
        self.application_digest = application_digest(application)
        self.guard, self.cgroup = guard.copy(), cgroup.copy()
        self.program_sha256, self.verify_backend, self.verify_settlement = program_sha256, verify_backend, verify_settlement
        self.coordinator = self.pidfd = None
        self.nonces, self.records = set(), {}
        self.continuity = self.begin = self.phase = None
        self.original_inventory = None
        self.generation = None
        self.generations = {}
        self.phase_owners = {}
        self.children = []
        self.coordinators = []
        self.live_observations = []
        self.pending_ready_observations = {}
        self.pending_checkpoint = None
        self.current_backend = None
        self.settlement_observations = []
        self.retired_coordinators = {}
        self.post_terminal_seen = {}

    def bind_coordinator(self, coordinator, pidfd):
        if self.pidfd is not None and not select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('previous maintenance coordinator is still live')
        if any(not select.select([fd], [], [], 0)[0] for fd in self.children):
            raise JournalError('previous maintenance child is still live')
        if self.pidfd is not None:
            os.close(self.pidfd)
        self.pidfd = os.dup(pidfd)
        self.coordinator = coordinator.copy()
        self.current_backend = None
        self._live(require_guard=False)
        self.coordinators.append(self.coordinator.copy())

    def coordinator_waited(self, coordinator, status, retirement):
        if (type(status) is not int or coordinator != self.coordinator or self.pidfd is None or
                not select.select([self.pidfd], [], [], 0)[0] or type(retirement) is not dict or
                retirement.get('coordinator') != coordinator or retirement.get('status') != status or
                retirement.get('coordinator_direct_wait') is not True or retirement.get('coordinator_pidfd_exited') is not True or
                retirement.get('after') != {'members': [], 'populated': 0} or
                network_verify.migration_scope(self.cgroup) != {'members': [], 'populated': 0}):
            raise JournalError('maintenance direct retirement is not settled')
        self.retired_coordinators[(coordinator['pid'], coordinator['starttime'])] = hashlib.sha256(canonical(retirement)).hexdigest()

    def _live(self, require_guard=True):
        self.journal.verify()
        self._reconcile_checkpoint()
        if (self.pidfd is None or select.select([self.pidfd], [], [], 0)[0] or
                boot_id() != self.journal.boot):
            raise JournalError('maintenance coordinator continuity unavailable')
        _coordinator_live(self.coordinator)
        if require_guard:
            _held_guard(self.guard, self.coordinator)
        scope = network_verify.migration_scope(self.cgroup)
        members = scope['members']
        if scope['populated'] != 1 or self.coordinator['pid'] not in members or len(members) > 2:
            raise JournalError('maintenance migration scope differs')
        for pid in members:
            if pid != self.coordinator['pid'] and process_stat(pid)['parent'] != self.coordinator['pid']:
                raise JournalError('unregistered migration descendant')

    def _reconcile_checkpoint(self):
        pending = self.pending_checkpoint
        if pending is None:
            return
        request, name, item, remaining, key, owner_generation = pending
        generation, kind, attempt = request['phase_generation'], request['kind'], request['attempt']
        witness_key = ('migration', self.recovery_id, self.application_digest, generation, attempt, kind)
        witness = self.journal.keys.get(witness_key)
        if (witness is None or witness[0] != request['record_sha256'] or witness[1] != name or
                self.generation != owner_generation):
            raise JournalError('pending checkpoint witness or generation differs')
        sequence, witness_digest = witness[2:4]
        record = (request['record_sha256'], item, sequence, witness_digest)
        if self.records.get(key) not in (None, record):
            raise JournalError('pending checkpoint cache differs')
        if kind == 'ready':
            cached = self.pending_ready_observations.get((generation, request['record_sha256']))
            event = strict_json(self.journal.entries[sequence - 1])
            if cached is None or event.get('live_observation_digest') != cached[0]:
                raise JournalError('ready witness lacks its original live observation')
            observed = {'witness_sequence': sequence, 'observation': cached[1]}
            existing = [value for value in self.live_observations if value['witness_sequence'] == sequence]
            if existing and existing != [observed]:
                raise JournalError('ready witness observation cache differs')
            if not existing:
                self.live_observations.append(observed)
        self.continuity.accept(name, request['record_sha256'], remaining)
        if kind == 'ready-abandoned':
            ready_name = 'migration-ready-%020d.json' % self.generation
            if ready_name in remaining:
                self.continuity.seen[ready_name] = remaining[ready_name]
        self.records[key] = record
        if kind in ('phase', 'terminal'):
            self.phase = dict(item, digest=request['record_sha256'])
            self.phase_owners[generation] = self.generation
        self.pending_checkpoint = None

    def _get(self, kind, attempt=0):
        return self.records.get((kind, attempt))

    def _require(self, kind, attempt=0):
        item = self._get(kind, attempt)
        if item is None:
            raise JournalError('maintenance predecessor witness missing')
        return item

    def _ready_decision(self, generation):
        key = ('migration', self.recovery_id, self.application_digest, generation, 0, 'ready-disposition')
        witness = self.journal.keys.get(key)
        if witness is None:
            return None
        event = strict_json(self.journal.entries[witness[2] - 1])
        if (event.get('kind') != 'ready-disposition' or event.get('record_sha256') != witness[0] or
                event.get('record_name') != witness[1] or type(event.get('ready_disposition')) is not dict):
            raise JournalError('maintenance ready disposition cache differs')
        return witness, event['ready_disposition']

    def _validate_next(self, request, item):
        kind, attempt = request['kind'], request['attempt']
        if kind not in ('phase', 'terminal') and request['phase_generation'] != self.generation:
            raise JournalError('maintenance migration generation differs')
        decision = self._ready_decision(self.generation)
        if (decision is not None and decision[1]['outcome'] == 'unwitnessed_retired' and
                (kind in ('ready', 'normal', 'successor', 'activation') or kind.startswith('observe-'))):
            raise JournalError('abandoned ready cannot authorize normal progression')
        if kind in ('phase', 'terminal'):
            if (item['application'] != self.application or item['recovery_id'] != self.recovery_id or
                    item['generation'] != self.phase['generation'] + 1 or
                    bytes(item['previous']).hex() != self.phase['digest']):
                raise JournalError('maintenance phase predecessor differs')
            prior = self.phase['phase']
            if kind == 'terminal':
                activation, abort, cancelled = self._get('activation'), self._get('abort'), self._get('begin-settled')
                if (prior != 'refencing' or item['phase'] != 'quiescent' or
                        sum(value is not None for value in (activation, abort, cancelled)) != 1):
                    raise JournalError('maintenance terminal lacks its unique predecessor')
                if activation is not None and activation[1]['terminal_phase_generation'] != item['generation']:
                    raise JournalError('maintenance terminal generation differs from activation')
            elif not ((prior == 'quiescent' and item['phase'] == 'migration_callback' and self._get('intent')) or
                      (prior == 'quiescent' and item['phase'] == 'refencing' and
                       self._get('begin-settled') and not self._get('intent')) or
                      (prior == 'migration_callback' and item['phase'] == 'refencing')):
                raise JournalError('maintenance phase transition invalid')
            if prior == 'migration_callback' and item['phase'] == 'refencing':
                ready_name = 'migration-ready-%020d.json' % self.generation
                entries, _ = self.directory.inventory()
                if any(entry['name'] == ready_name for entry in entries):
                    ready_key = ('migration', self.recovery_id, self.application_digest, self.generation, 0, 'ready')
                    abandoned_key = ('migration', self.recovery_id, self.application_digest, self.generation, 0, 'ready-abandoned')
                    decision = self._ready_decision(self.generation)
                    if (ready_key not in self.journal.keys and not (decision is not None and
                            decision[1]['outcome'] == 'unwitnessed_retired' and abandoned_key in self.journal.keys)):
                        raise JournalError('refencing lacks witnessed ready or abandonment')
            return
        if kind == 'begin-settled':
            if (self._get('intent') is not None or item['begin_digest'] != self._require('begin')[0] or
                    item['recovery_id'] != self.recovery_id or item['application'] != self.application or
                    item['outcome'] != 'no_effect_c80' or item['coordinator'] not in self.coordinators or
                    (item['coordinator']['pid'], item['coordinator']['starttime']) not in self.retired_coordinators and
                    item['coordinator'] != self.coordinator):
                raise JournalError('begin settlement lineage differs')
            return
        if kind == 'intent':
            if (item['previous_phase_generation'] != self.generation - 1 or item['recovery_id'] != self.recovery_id or
                    item['application'] != self.application or item['guard'] != self.guard or item['cgroup'] != self.cgroup or
                    item['boot_id'] != self.journal.boot or item['coordinator'] != self.begin['coordinator'] or
                    item['program_sha256'] != self.program_sha256 or
                    item['original_inventory_digest'] != self.begin['original_inventory_digest'] or
                    item['worker_absent'] is not True):
                raise JournalError('migration intent binding differs')
            original = self.original_inventory
            manifests = original['baseline']['Manifest']['Manifests']
            if (not manifests or item['admin_role'] != original['admin'] or
                    item['bootstrap']['role'] != original['bootstrap'] or item['bootstrap']['pid'] <= 0 or
                    item['migrator_role'] != manifests[-1]['MigratorCatalogRole'] or
                    item['worker_role'] != 'houfeng_record_deletion_worker' or
                    item['runtime_role'] not in original['previous'] or
                    item['migrator_role'] not in original['previous'] or
                    len({item['runtime_role'], item['migrator_role'], item['worker_role'],
                         item['admin_role'], original['bootstrap']}) != 5):
                raise JournalError('migration role binding differs')
            return
        intent_digest, intent = self._require('intent')[:2]
        if kind != 'successor' and item['intent_digest'] != intent_digest:
            raise JournalError('migration intent digest differs')
        if kind == 'ready-abandoned':
            preparation = self._require('preparation')
            ready_name = 'migration-ready-%020d.json' % self.generation
            ready_raw = self.directory.read(ready_name)
            ready = strict_json(ready_raw)
            validate_record({'kind': 'ready', 'phase_generation': self.generation, 'attempt': 0}, ready)
            decision = self._ready_decision(self.generation)
            if (decision is None or decision[1]['outcome'] != 'unwitnessed_retired' or
                    decision[0][0] != hashlib.sha256(ready_raw).hexdigest() or
                    self.phase['phase'] != 'migration_callback' or self._get('phase', self.generation) is None or
                    self._get('ready') is not None or
                    ('migration', self.recovery_id, self.application_digest, self.generation, 0, 'ready') in self.journal.keys or
                    self._get('normal') is not None or
                    any(k.startswith('observe-') or k in ('successor', 'activation') for k, _ in self.records) or
                    item['preparation_digest'] != preparation[0] or item['ready_digest'] != hashlib.sha256(ready_raw).hexdigest() or
                    ready['intent_digest'] != intent_digest or item['restricted'] != preparation[1]['emergency_target'] or
                    item['application'] != self.application or item['recovery_id'] != self.recovery_id or
                    item['outcome'] != 'unwitnessed_ready_c80' or item['coordinator'] not in self.coordinators):
                raise JournalError('ready abandonment lineage differs')
            return
        if kind == 'preparation':
            if self.phase['phase'] != 'migration_callback' or item['bootstrap'] != intent['bootstrap'] or not item['transaction_id'].isdigit():
                raise JournalError('migration preparation binding differs')
            _check_preparation(item, intent, self.original_inventory)
        elif kind == 'ready':
            decision = self._ready_decision(self.generation)
            if decision is not None and decision[1]['outcome'] == 'unwitnessed_retired':
                raise JournalError('abandoned ready cannot acquire a witness')
            self._require('preparation')
            if self.phase['phase'] != 'migration_callback' or item['backend']['role'] != intent['migrator_role']:
                raise JournalError('migration ready binding differs')
        elif kind == 'normal':
            preparation = self._require('preparation')
            self._require('ready')
            if (item['preparation_digest'] != preparation[0] or
                    item['inventory']['application'] != self.application or
                    item['inventory']['recovery_id'] != self.recovery_id or
                    not _post_roles(preparation[1]['normal'], item['inventory']['role_baseline'], intent['worker_role'])):
                raise JournalError('migration normal preparation differs')
            _check_normal_inventory(item['inventory'], self.original_inventory, intent, preparation[1])
        elif kind == 'restrict':
            preparation = self._require('preparation')
            if (item['preparation_digest'] != preparation[0] or self.phase['phase'] != 'refencing' or
                    item['to'] not in (preparation[1]['final_target'], preparation[1]['emergency_target']) or
                    item['from'] != preparation[1]['normal'] and
                    not _post_roles(preparation[1]['normal'], item['from'], intent['worker_role'])):
                raise JournalError('migration restriction binding differs')
        elif kind == 'observe-intent':
            preparation = self._require('preparation')
            if (self.phase['phase'] != 'refencing' or item['preparation_digest'] != preparation[0] or
                    item['recovery_id'] != self.recovery_id or item['application'] != self.application):
                raise JournalError('fresh observation intent binding differs')
            _check_observation_intent(item, intent, preparation[1], self.original_inventory, self.records)
            previous = self._get('observe-settled', attempt - 1) if attempt > 1 else None
            if (attempt > 1 and previous is None or item['previous_attempt_settled_digest'] !=
                    (previous[0] if previous else None) or self._get('observe-intent', attempt - 1) is not None and previous is None):
                raise JournalError('fresh observation attempt is not contiguous')
            if attempt != 1 + max((n for k, n in self.records if k == 'observe-intent'), default=0):
                raise JournalError('fresh observation attempt is not next')
        elif kind in ('observe-normal', 'observe-restrict', 'observe-settled'):
            observation = self._require('observe-intent', attempt)
            if item['observation_intent_digest'] != observation[0]:
                raise JournalError('fresh observation lineage differs')
            if kind == 'observe-normal' and (item['inventory']['application'] != self.application or
                    item['inventory']['recovery_id'] != self.recovery_id or
                    item['inventory']['role_baseline'] != observation[1]['to']):
                raise JournalError('fresh normal role observation differs')
            if kind == 'observe-normal':
                _check_normal_inventory(item['inventory'], self.original_inventory, intent,
                                        self._require('preparation')[1])
                head = item['inventory']['baseline']['Manifest']['Head']['ManifestDigest']
                if bytes(head).hex() != observation[1]['manifest_digest']:
                    raise JournalError('fresh normal manifest digest differs')
            if kind == 'observe-restrict' and (item['from'] != observation[1]['to'] or
                                                item['to'] not in (observation[1]['final_target'], observation[1]['emergency_target'])):
                raise JournalError('fresh observation restriction target differs')
            if kind == 'observe-settled':
                restriction = self._require('observe-restrict', attempt)
                normal = self._get('observe-normal', attempt)
                if (item['restriction_digest'] != restriction[0] or item['outcome'] not in
                        ('grant_not_committed', 'refenced_without_normal', 'normal_observed_refenced') or
                        item['normal_digest'] != (normal[0] if item['outcome'] == 'normal_observed_refenced' and normal else None) or
                        item['outcome'] == 'normal_observed_refenced' and normal is None or
                        item['outcome'] != 'normal_observed_refenced' and normal is not None):
                    raise JournalError('fresh observation settlement differs')
        elif kind == 'compensate':
            if self.phase['phase'] != 'refencing':
                raise JournalError('compensation requires refencing')
            preparation = self._get('preparation')
            if item['preparation_digest']:
                if preparation is None or item['preparation_digest'] != preparation[0]:
                    raise JournalError('compensation preparation differs')
                worker = [role for role in preparation[1]['normal']['roles'] if role['name'] == intent['worker_role']]
                if len(worker) != 1 or item['worker_oid'] != worker[0]['oid']:
                    raise JournalError('compensation worker identity differs')
            elif item['worker_oid'] != 0:
                raise JournalError('compensation has an unbound worker')
        elif kind == 'abort':
            if item['compensation_digest'] != self._require('compensate')[0] or self._get('activation'):
                raise JournalError('compensation abort differs')
        elif kind == 'successor':
            if self.phase['phase'] != 'refencing' or self._get('abort'):
                raise JournalError('successor requires refencing')
            allowed = []
            normal = self._get('normal')
            if normal is not None and self._get('restrict') is not None:
                allowed.append(normal[1]['inventory'])
            for (record_kind, number), record in self.records.items():
                if record_kind == 'observe-settled' and record[1]['outcome'] == 'normal_observed_refenced':
                    observed = self._require('observe-normal', number)
                    allowed.append(observed[1]['inventory'])
            if item not in allowed:
                raise JournalError('successor is not a settled normal inventory')
        elif kind == 'activation':
            successor = self._require('successor')
            normal = self._get('normal') if item['normal_source'] == 'original' and item['normal_attempt'] == 0 else None
            if item['normal_source'] == 'observation' and item['normal_attempt'] > 0:
                settled = self._require('observe-settled', item['normal_attempt'])
                if settled[1]['outcome'] == 'normal_observed_refenced':
                    normal = self._get('observe-normal', item['normal_attempt'])
            if (normal is None or item['normal_digest'] != normal[0] or item['successor_digest'] != successor[0] or
                    normal[1]['inventory'] != successor[1] or
                    item['manifest_digest'] != bytes(successor[1]['manifest_digest']).hex() or
                    item['application'] != self.application or item['recovery_id'] != self.recovery_id or
                    item['original_inventory_digest'] != self.begin['original_inventory_digest'] or
                    item['terminal_phase_generation'] != self.phase['generation'] + 1):
                raise JournalError('migration activation lineage differs')
        elif kind == 'revocation-unresolved':
            if item['outcome'] != 'revocation_unresolved':
                raise JournalError('migration revocation outcome invalid')
        else:
            raise JournalError('unrecognized migration checkpoint kind')

    def _post_read(self, name, current):
        if name not in current:
            raise JournalError('post-terminal predecessor missing')
        raw = self.directory.read(name)
        if hashlib.sha256(raw).hexdigest() != current[name]:
            raise JournalError('post-terminal record changed while read')
        return raw, strict_json(raw)

    def _post_source(self, current, names):
        keys = set()
        pattern = re.compile(r'^source-handoff-(?:intent|installed)-([0-9a-f]{64})\.json$')
        attempt_pattern = re.compile(r'^source-handoff-attempt-([0-9a-f]{64})-([0-9]{6,20})\.json$')
        for name in names:
            match = pattern.fullmatch(name) or attempt_pattern.fullmatch(name)
            if match is None:
                raise JournalError('unknown post-terminal source record')
            keys.add(match.group(1))
        binding_name = 'sessions/' + self.recovery_id + '/binding.json'
        if binding_name not in self.continuity.seen:
            raise JournalError('source handoff has no pinned session binding')
        _, binding = self._post_read(binding_name, current)
        if (type(binding) is not dict or set(binding) != {'version', 'recovery_id', 'generation'} or
                type(binding['version']) is not int or binding['version'] != 1 or
                binding['recovery_id'] != self.recovery_id or type(binding['generation']) is not int or
                not 0 < binding['generation'] < (1 << 63)):
            raise JournalError('source handoff session binding invalid')
        for key in keys:
            originals = {}
            for prefix in ('source-preview-', 'source-intent-', 'source-witnessed-', 'source-anchored-'):
                original_name = prefix + key + '.json'
                if original_name not in self.continuity.seen:
                    raise JournalError('source handoff predecessor was not pinned at begin')
                originals[prefix] = self._post_read(original_name, current)
            preview = originals['source-preview-'][1]
            try:
                request = preview['request']
                origin = request['Origin']
                source = origin['Source']
                source_key = hashlib.sha256((source['DeploymentID'] + '\0' + source['ProjectID'] + '\0' +
                                             request['OperationID']).encode('utf-8')).hexdigest()
            except (KeyError, TypeError, UnicodeError) as exc:
                raise JournalError('pinned source preview is malformed') from exc
            if (type(preview) is not dict or preview.get('key') != key or source_key != key or
                    preview.get('recovery_id') != self.recovery_id or origin.get('Application') != self.application):
                raise JournalError('source handoff preview identity differs')
            intent_name = 'source-handoff-intent-' + key + '.json'
            raw, intent = self._post_read(intent_name, current)
            if (not _shape(intent, _HANDOFF_INTENT) or not _numeric_bounds(intent) or intent['version'] != 1 or
                    intent['key'] != key or intent['application'] != self.application):
                raise JournalError('source handoff intent schema differs')
            for prefix, field in (('source-preview-', 'preview_digest'), ('source-intent-', 'source_intent_digest'),
                                  ('source-witnessed-', 'witnessed_digest'), ('source-anchored-', 'anchored_digest')):
                if bytes(intent[field]) != hashlib.sha256(originals[prefix][0]).digest():
                    raise JournalError('source handoff predecessor digest differs')
            facts = intent['facts']
            expected = {'deployment_id': source['DeploymentID'], 'project_id': source['ProjectID'],
                        'source_kind': source['Kind'], 'source_id': source['ID'],
                        'operation_id': request['OperationID'], 'actor_id': request['ActorUserID'],
                        'app_domain_id': self.application['ID'], 'app_system_identifier': self.application['SystemIdentifier'],
                        'app_database_oid': self.application['DatabaseOID'], 'app_database_name': self.application['DatabaseName'],
                        'app_epoch': self.application['Epoch'], 'witness_domain_id': self.begin['witness']['ID']}
            if (any(facts[k] != v for k, v in expected.items()) or
                    any(facts[k] != 0 for k in ('primary_head_sequence', 'witness_head_sequence')) or
                    any(facts[k] != '' for k in ('primary_head_hash', 'witness_head_hash')) or
                    facts['ledger_sequence'] <= 0 or not _digest(facts['ledger_domain']) or
                    not _digest(facts['ledger_entry_hash']) or not _digest(facts['witness_proof_digest'])):
                raise JournalError('source handoff facts differ from pinned source')
            digest = hashlib.sha256(raw).digest()
            attempts = {}
            for name in sorted(current):
                match = attempt_pattern.fullmatch(name)
                if match is None or match.group(1) != key:
                    continue
                number = int(match.group(2))
                if number != len(attempts) + 1 or number > 65536 or match.group(2) != '%06d' % number:
                    raise JournalError('source handoff attempt sequence differs')
                _, attempt = self._post_read(name, current)
                if (not _shape(attempt, _HANDOFF_ATTEMPT) or not _numeric_bounds(attempt) or
                        attempt['version'] != 1 or attempt['number'] != number or
                        bytes(attempt['intent_digest']) != digest or
                        not _same_source_facts(attempt['command'], facts) or
                        attempt['command']['maintenance_rid'] != self.recovery_id or
                        attempt['command']['maintenance_generation'] != binding['generation'] or
                        not 0 < attempt['backend']['PID'] < (1 << 31) or not attempt['backend']['Started'] or
                        attempt['backend']['Role'] != self.original_inventory['admin']):
                    raise JournalError('source handoff attempt binding differs')
                attempts[number] = attempt
            installed_name = 'source-handoff-installed-' + key + '.json'
            if installed_name in current:
                _, receipt = self._post_read(installed_name, current)
                if (not _shape(receipt, _HANDOFF_RECEIPT) or not _numeric_bounds(receipt) or
                        receipt['version'] != 1 or bytes(receipt['intent_digest']) != digest or
                        receipt['attempt'] not in attempts):
                    raise JournalError('source handoff installation receipt differs')
                installation, command = receipt['installation'], attempts[receipt['attempt']]['command']
                if (any(installation[k] != command[k] for k in _HANDOFF_FACTS) or
                        installation['first_installation_rid'] != command['maintenance_rid'] or
                        installation['first_installation_generation'] != command['maintenance_generation']):
                    raise JournalError('source handoff installation lineage differs')

    def _post_record_authorities(self, current):
        allowed = {}
        for name in self.continuity.seen:
            match = re.fullmatch(r'predecessor-reserve-result-([0-9]{20})\.json', name)
            if match is None:
                continue
            suffix = match.group(1) + '.json'
            reserve_name = 'predecessor-reserve-intent-' + suffix
            create_name = 'predecessor-create-intent-' + suffix
            if reserve_name not in self.continuity.seen or create_name not in self.continuity.seen:
                raise JournalError('record predecessor provenance is incomplete')
            _, result = self._post_read(name, current)
            _, reserve = self._post_read(reserve_name, current)
            _, create = self._post_read(create_name, current)
            try:
                command, creation = reserve['command'], create['command']
                operation = result['operation_id']
                deployment, project = command['deployment_id'], creation['project_id']
                key = hashlib.sha256((deployment + '\0' + project + '\0' + operation).encode('utf-8')).hexdigest()
                values = {'deployment': deployment, 'project': project, 'operation': operation,
                          'actor': command['actor_id'], 'object': creation['object_id'],
                          'token': creation['token_commitment'], 'fingerprint': creation['request_fingerprint'],
                          'reason': command['reason_code']}
                valid = (result['application'] == self.application and reserve['application'] == self.application and
                         create['application'] == self.application and result['recovery_id'] == self.recovery_id and
                         reserve['recovery_id'] == self.recovery_id and create['recovery_id'] == self.recovery_id and
                         result['command_digest'] == reserve['command_digest'] and
                         result['create_command_digest'] == create['command_digest'] and
                         command['create_command_digest'] == create['command_digest'] and
                         command['reservation_id'] == result['reservation_id'] and
                         command['request_fingerprint'] == creation['request_fingerprint'] and
                         command['deletion_contract_version'] == 1 and creation['object_kind'] == 'record' and
                         re.fullmatch(r'rpo_[a-z0-9]{1,64}', operation) is not None and
                         _digest(values['token']) and _digest(values['fingerprint']))
            except (KeyError, TypeError, UnicodeError) as exc:
                raise JournalError('record predecessor provenance is malformed') from exc
            if not valid or key in allowed and allowed[key] != values:
                raise JournalError('record predecessor provenance differs')
            allowed[key] = values
        return allowed

    def _post_records(self, current, names):
        authorities = self._post_record_authorities(current)
        anchors = []
        anchor_names = sorted(name for name in current if re.fullmatch(r'anchor-[0-9]{20}\.json', name))
        previous = bytes(32)
        for index, name in enumerate(anchor_names):
            if name != 'anchor-%020d.json' % index:
                raise JournalError('post-terminal anchor sequence differs')
            raw, anchor = self._post_read(name, current)
            if (not _shape(anchor, _ANCHOR) or not _numeric_bounds(anchor) or anchor['version'] != 1 or
                    anchor['index'] != index or bytes(anchor['previous']) != previous):
                raise JournalError('post-terminal anchor chain differs')
            if index == 0 and (anchor['primary'] != {'Sequence': 0, 'Hash': [0] * 32} or
                               anchor['witness'] != {'Sequence': 0, 'Hash': [0] * 32}):
                raise JournalError('post-terminal anchor genesis differs')
            if anchors:
                for side in ('primary', 'witness'):
                    old, new = anchors[-1][side], anchor[side]
                    if (new['Sequence'] < old['Sequence'] or
                            new['Sequence'] == old['Sequence'] and new['Hash'] != old['Hash']):
                        raise JournalError('post-terminal anchor head regressed')
            previous = hashlib.sha256(raw).digest()
            anchors.append(anchor)
        if not anchors:
            raise JournalError('post-terminal anchor chain missing')
        domain_id = self.begin['primary']['ID'].encode('utf-8')
        label = b'HOUFENG-RECORD-LEDGER-DOMAIN-V1'
        domain = hashlib.sha256(len(label).to_bytes(4, 'big') + label +
                                len(domain_id).to_bytes(4, 'big') + domain_id).hexdigest()
        for name in names:
            anchor_match = re.fullmatch(r'anchor-([0-9]{20})\.json', name)
            if anchor_match:
                continue
            match = re.fullmatch(r'(intent|done)-([0-9a-f]{64})\.json', name)
            if match is None:
                raise JournalError('unknown post-terminal record ledger file')
            kind, key = match.groups()
            if key not in authorities:
                raise JournalError('record ledger file lacks pinned predecessor authority')
            intent_name = 'intent-' + key + '.json'
            raw, intent = self._post_read(intent_name, current)
            if (not _shape(intent, _RECORD_INTENT) or not _numeric_bounds(intent) or
                    intent['version'] != 1 or intent['key'] != key):
                raise JournalError('record ledger intent schema differs')
            decoded = _record_canonical(intent['canonical'])
            if (decoded.pop('domain') != domain or decoded != authorities[key] or
                    not any(anchor['primary'] == intent['primary'] and anchor['witness'] == intent['witness']
                            for anchor in anchors)):
                raise JournalError('record ledger intent provenance differs')
            if kind == 'done':
                _, done = self._post_read(name, current)
                if (not _shape(done, _RECORD_DONE) or not _numeric_bounds(done) or done['version'] != 1 or
                        done['key'] != key or bytes(done['intent_digest']) != hashlib.sha256(raw).digest() or
                        not 0 < done['sequence'] < (1 << 63) or done['anchor_index'] >= len(anchors) or
                        not any(done['hash'])):
                    raise JournalError('record ledger completion differs')
                anchor = anchors[done['anchor_index']]
                for side in ('primary', 'witness'):
                    head = anchor[side]
                    if (head['Sequence'] < done['sequence'] or
                            head['Sequence'] == done['sequence'] and head['Hash'] != done['hash']):
                        raise JournalError('record ledger completion anchor differs')

    def _post_terminal_scan(self):
        entries, directories = self.directory.inventory()
        if directories != self.continuity.directories:
            raise JournalError('post-terminal directory identity changed')
        current = {entry['name']: entry['sha256'] for entry in entries}
        for name, digest in self.continuity.seen.items():
            if current.get(name) != digest:
                raise JournalError('root-witnessed or begin inventory record changed')
        for name, digest in self.post_terminal_seen.items():
            if current.get(name) != digest:
                raise JournalError('previously observed post-terminal record changed')
        extra = set(current) - set(self.continuity.seen)
        source = {name for name in extra if name.startswith('source-handoff-')}
        records = {name for name in extra if name.startswith(('intent-', 'done-', 'anchor-'))}
        if extra != source | records:
            raise JournalError('unrecognized post-terminal publication')
        if source:
            self._post_source(current, source)
        if records:
            self._post_records(current, records)
        observed = {name: current[name] for name in extra}
        # These are immutable protected-file observations, not root witnesses
        # and do not authorize the source or record effects they describe.
        self.post_terminal_seen = observed.copy()
        return observed

    def _require_retired_scope(self):
        self.journal.verify()
        self._reconcile_checkpoint()
        if self.coordinator is not None and (self.coordinator['pid'], self.coordinator['starttime']) not in self.retired_coordinators:
            raise JournalError('previous maintenance coordinator was not directly waited')
        if self.pidfd is not None and not select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('previous maintenance coordinator is not retired')
        if any(not select.select([fd], [], [], 0)[0] for fd in self.children):
            raise JournalError('previous maintenance child is not retired')
        if network_verify.migration_scope(self.cgroup) != {'members': [], 'populated': 0}:
            raise JournalError('previous maintenance scope is not empty')

    def require_retained_prefix(self):
        self._require_retired_scope()
        self.directory.check()
        if self.begin is None:
            if self.journal.sequence:
                raise JournalError('maintenance witness state has no begin')
            return
        terminal = self._get('terminal', self.generation + 2)
        if terminal is not None and self._get('activation') is not None:
            self._post_terminal_scan()
        else:
            self._pending_prefix()

    def terminal_admission_ready(self):
        self._require_retired_scope()
        if self.begin is None or self.phase is None or self.generation is None:
            return False
        activation = self._get('activation')
        terminal = self._get('terminal', self.generation + 2)
        if (activation is None or terminal is None or self._get('abort') is not None or
                self.phase['phase'] != 'quiescent' or self.phase['generation'] != self.generation + 2 or
                activation[1]['terminal_phase_generation'] != self.phase['generation'] or
                terminal[0] != self.phase['digest']):
            return False
        self._post_terminal_scan()
        return True

    def _completed_outcome(self):
        if self.phase is None or self.phase['phase'] != 'quiescent':
            return None
        terminal = self._get('terminal', self.phase['generation'])
        if terminal is None or terminal[0] != self.phase['digest']:
            return None
        if self._get('activation') is not None:
            return 'activated'
        if self._get('abort') is not None:
            return 'compensated'
        if self._get('begin-settled') is not None:
            return 'begin_cancelled'
        return None

    def _pending_prefix(self):
        entries, directories = self.directory.inventory()
        if directories != self.continuity.directories:
            raise JournalError('maintenance inventory directory identity changed')
        current = {entry['name']: entry['sha256'] for entry in entries}
        if any(current.get(name) != digest for name, digest in self.continuity.seen.items()):
            raise JournalError('a previously seen maintenance record changed or disappeared')
        names = set(current) - set(self.continuity.seen)
        if not names:
            return [], {}
        current_begin = 'migration-begin-%020d.json' % self.generation
        if current_begin in names and self._get('begin') is None:
            if names != {current_begin}:
                raise JournalError('unwitnessed begin has an unexpected suffix')
            return [current_begin], {current_begin: current[current_begin]}
        next_begin = 'migration-begin-%020d.json' % (self.phase['generation'] + 1)
        if next_begin in names:
            if names != {next_begin} or self._completed_outcome() not in ('begin_cancelled', 'compensated'):
                raise JournalError('new migration begin lacks a unique completed predecessor')
            request = {'kind': 'begin', 'attempt': 0, 'phase_generation': self.phase['generation'] + 1,
                       'recovery_id': self.recovery_id, 'application_digest': self.application_digest,
                       'record_sha256': current[next_begin]}
            read_begin(self.directory, request, self.application, self.coordinators, self.guard, self.cgroup,
                       self.journal.boot, self.continuity)
            return [next_begin], {next_begin: current[next_begin]}
        unresolved = 'migration-revocation-unresolved-%020d.json' % self.generation
        cancelling = (self._get('begin-settled') is not None or
                      'migration-begin-settled-%020d.json' % self.generation in names)
        refencing = 'phase-%020d.json' % (self.generation if cancelling else self.generation + 1)
        terminal = 'phase-%020d.json' % (self.generation + 1 if cancelling else self.generation + 2)
        ready = 'migration-ready-%020d.json' % self.generation
        abandonment = 'migration-ready-abandoned-%020d.json' % self.generation
        retained = {}
        decision = self._ready_decision(self.generation)
        if ready in names and decision is not None and decision[1]['outcome'] == 'unwitnessed_retired':
            if self._get('ready') is not None or current[ready] != decision[0][0]:
                raise JournalError('retained unwitnessed ready differs from disposition')
            retained[ready] = current[ready]
            names.remove(ready)
        if retained and unresolved in names:
            raise JournalError('unwitnessed ready has an unresolved cleanup suffix')
        phases = {refencing, terminal}
        ordinary = names - ({unresolved} | phases)
        if len(ordinary) > 1 or len(names) > 4:
            raise JournalError('unfinished maintenance prefix is not bounded')
        ordered = sorted(ordinary)
        ordered.extend(sorted(names & phases))
        if unresolved in names:
            ordered.append(unresolved)
        saved_phase, saved_records = self.phase, self.records
        self.records = self.records.copy()
        try:
            for index, name in enumerate(ordered):
                raw = self.directory.read(name)
                digest = hashlib.sha256(raw).hexdigest()
                if digest != current[name]:
                    raise JournalError('unfinished maintenance publication changed')
                item = strict_json(raw)
                match = re.fullmatch(r'migration-(observe-(?:intent|normal|restrict|settled))-([0-9]{20})-([0-9]{20})\.json', name)
                if match:
                    kind, generation, attempt = match.group(1), int(match.group(2)), int(match.group(3))
                else:
                    match = re.fullmatch(r'migration-([a-z-]+)-([0-9]{20})\.json', name)
                    if match:
                        kind, generation, attempt = match.group(1), int(match.group(2)), 0
                    else:
                        match = re.fullmatch(r'phase-([0-9]{20})\.json', name)
                        if not match or type(item) is not dict:
                            raise JournalError('unfinished maintenance publication is not recognized')
                        kind = 'terminal' if item.get('phase') == 'quiescent' else 'phase'
                        generation, attempt = int(match.group(1)), 0
                if kind not in _MIGRATION_KINDS or kind == 'begin' or generation <= 0 or generation >= (1 << 63):
                    raise JournalError('unfinished maintenance publication identity invalid')
                request = {'kind': kind, 'phase_generation': generation, 'attempt': attempt}
                if checkpoint_name(request) != name or (kind.startswith('observe-') != (attempt != 0)):
                    raise JournalError('unfinished maintenance publication name differs')
                if index and name not in (refencing, terminal, unresolved):
                    raise JournalError('unfinished publication follows pending record')
                validate_record(request, item)
                self._validate_next(request, item)
                key = (kind, generation) if kind in ('phase', 'terminal') else (kind, attempt)
                self.records[key] = (digest, item, None, None)
                if kind in ('phase', 'terminal'):
                    self.phase = dict(item, digest=digest)
        finally:
            self.phase, self.records = saved_phase, saved_records
        return ordered, dict(retained, **{name: current[name] for name in ordered})

    def ready_disposition(self, request, credentials, deadline):
        if time.monotonic() >= deadline:
            raise JournalError('maintenance disposition deadline exceeded')
        disposition_request(request, self.recovery_id, self.application_digest, self.nonces)
        if self.coordinator is None or credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('maintenance coordinator credentials differ')
        self._live()
        generation = request['phase_generation']
        if self.begin is None or generation != self.generation or self._completed_outcome() is not None:
            raise JournalError('ready disposition requires the current pending generation')
        original = self.begin['coordinator']
        retirement = self.retired_coordinators.get((original['pid'], original['starttime']))
        if (original == self.coordinator or not _digest(retirement) or
                original not in self.coordinators or self.coordinator not in self.coordinators or
                any(not select.select([fd], [], [], 0)[0] for fd in self.children) or
                network_verify.migration_scope(self.cgroup) != {'members': [self.coordinator['pid']], 'populated': 1}):
            raise JournalError('original migration scope is not directly retired')
        intent, preparation = self._require('intent'), self._require('preparation')
        if self._get('phase', generation) is None:
            raise JournalError('migration callback was not witnessed')
        name = checkpoint_name(request)
        raw = self.directory.read(name)
        ready = strict_json(raw)
        validate_record(request, ready)
        if (hashlib.sha256(raw).hexdigest() != request['record_sha256'] or
                ready['intent_digest'] != intent[0] or ready['backend']['role'] != intent[1]['migrator_role'] or
                ready['child']['executable_sha256'] != self.program_sha256):
            raise JournalError('ready disposition record lineage differs')
        ordered, pending = self._pending_prefix()
        self.continuity.observe(name, request['record_sha256'], pending)
        prior = self._ready_decision(generation)
        ready_key = ('migration', self.recovery_id, self.application_digest, generation, 0, 'ready')
        witness = self.journal.keys.get(ready_key)
        if witness is not None:
            if witness[0] != request['record_sha256'] or witness[1] != name:
                raise JournalError('ready witness bytes differ')
            cached = self.pending_ready_observations.get((generation, request['record_sha256']))
            event = strict_json(self.journal.entries[witness[2] - 1])
            observed = [value for value in self.live_observations if value['witness_sequence'] == witness[2]]
            if (cached is None or hashlib.sha256(canonical(cached[1])).hexdigest() != cached[0] or
                    event.get('live_observation_digest') != cached[0] or
                    observed != [{'witness_sequence': witness[2], 'observation': cached[1]}] or
                    self._get('ready') is None or self._get('ready')[2:4] != witness[2:4] or
                    self._get('ready-abandoned') is not None):
                raise JournalError('ready witness lacks its original live observation')
            outcome, predecessor, ready_sequence, ready_digest = 'witnessed', witness[3], witness[2], witness[3]
        else:
            unresolved = 'migration-revocation-unresolved-%020d.json' % generation
            if unresolved in pending or self._get('revocation-unresolved') is not None:
                raise JournalError('unresolved cleanup blocks unwitnessed ready disposition')
            if (self._get('ready') is not None or self._get('normal') is not None or
                    any(k.startswith('observe-') or k in ('successor', 'activation') for k, _ in self.records) or
                    prior is None and (ordered != [name] or self.phase['phase'] != 'migration_callback')):
                raise JournalError('unwitnessed ready has a conflicting suffix')
            outcome, predecessor, ready_sequence, ready_digest = 'unwitnessed_retired', preparation[3], None, None
        disposition = {'outcome': outcome, 'ready_witness_sequence': ready_sequence,
                       'ready_witness_digest': ready_digest, 'scope_retirement_digest': retirement}
        if prior is not None and (prior[0][0] != request['record_sha256'] or prior[0][1] != name or prior[1] != disposition):
            raise JournalError('ready disposition conflicts with its persisted decision')
        sequence, digest = self.journal.append('migration', self.recovery_id, self.application_digest,
            generation, 0, 'ready-disposition', name, raw, predecessor, None, disposition)
        self._live()
        if time.monotonic() >= deadline:
            raise JournalError('maintenance disposition expired before acknowledgement')
        return dict(request, op='maintenance_ready_disposition_ack', disposition=outcome,
                    decision_sequence=sequence, decision_digest=digest,
                    ready_witness_sequence=ready_sequence, ready_witness_digest=ready_digest)

    def checkpoint(self, request, credentials, deadline):
        if time.monotonic() >= deadline:
            raise JournalError('maintenance checkpoint deadline exceeded')
        checkpoint_request(request, self.recovery_id, self.application_digest, self.nonces)
        if credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('maintenance coordinator credentials differ')
        self._live()
        kind, attempt, name = request['kind'], request['attempt'], checkpoint_name(request)
        if (self.current_backend is not None and kind not in _SETTLEMENT_KINDS and
                self.verify_backend(self.current_backend, deadline) != self.current_backend):
            raise JournalError('current maintenance backend changed within invocation')
        owner = self.phase_owners.get(request['phase_generation']) if kind == 'terminal' else None
        if owner is not None and owner != self.generation:
            previous = self.generations[owner]['records'].get(('terminal', request['phase_generation']))
            if previous is None or previous[0] != request['record_sha256']:
                raise JournalError('historical terminal retry differs')
            if self._completed_outcome() == 'activated':
                pending = self._post_terminal_scan()
            else:
                _, pending = self._pending_prefix()
            self.continuity.observe(name, request['record_sha256'], pending)
            raw = self.directory.read(name)
            if hashlib.sha256(raw).hexdigest() != previous[0]:
                raise JournalError('historical terminal bytes changed')
            sequence, witness = self.journal.append('migration', self.recovery_id, self.application_digest,
                request['phase_generation'], 0, 'terminal', name, raw, None, None)
            if (sequence, witness) != previous[2:4] or time.monotonic() >= deadline:
                raise JournalError('historical terminal witness differs')
            response = dict(request, op='maintenance_checkpoint_ack', witness_sequence=sequence, witness_digest=witness)
            self._live()
            return response
        if (self.begin is not None and kind == 'begin' and request['phase_generation'] != self.generation):
            ordered, _ = self._pending_prefix()
            if (self._completed_outcome() not in ('begin_cancelled', 'compensated') or
                    request['phase_generation'] != self.phase['generation'] + 1 or ordered != [name]):
                raise JournalError('next migration generation is not authorized')
            begin, _, _ = read_begin(self.directory, request, self.application, self.coordinators,
                                     self.guard, self.cgroup, self.journal.boot, self.continuity)
            self.begin, self.generation, self.records = begin, request['phase_generation'], {}
            self.generations[self.generation] = {'begin': begin, 'records': self.records}
        if (self.begin is not None and kind not in ('phase', 'terminal') and
                request['phase_generation'] != self.generation):
            raise JournalError('maintenance migration generation differs')
        key = (kind, attempt) if kind not in ('phase', 'terminal') else (kind, request['phase_generation'])
        existing = self.records.get(key)
        pending = {}
        if (kind == 'terminal' and existing is not None and self._get('activation') is not None and
                self.phase['phase'] == 'quiescent' and self.phase['generation'] == request['phase_generation']):
            pending = self._post_terminal_scan()
        elif self.begin is not None and not (kind == 'begin' and existing is None):
            ordered, pending = self._pending_prefix()
            if existing is None:
                if not ordered or ordered[0] != name:
                    raise JournalError('maintenance recovery must acknowledge pending prefix in order')
                pending.pop(name)
        if self.begin is None:
            if kind != 'begin':
                raise JournalError('maintenance begin was not witnessed')
            begin, raw, continuity = read_begin(self.directory, request, self.application, self.coordinators,
                                               self.guard, self.cgroup, self.journal.boot)
            self.begin, self.generation, self.continuity = begin, request['phase_generation'], continuity
            self.generations[self.generation] = {'begin': begin, 'records': self.records}
            self.original_inventory = strict_json(self.directory.read(
                'sessions/' + self.recovery_id + '/app-inventory.json'))
            if not _inventory_shape(self.original_inventory):
                raise JournalError('original maintenance inventory schema invalid')
            phase_raw = self.directory.read('phase-%020d.json' % (self.generation - 1))
            self.phase = dict(begin['phase'], digest=hashlib.sha256(phase_raw).hexdigest())
        else:
            self.continuity.observe(name, request['record_sha256'], pending)
            raw = self.directory.read(name)
            if hashlib.sha256(raw).hexdigest() != request['record_sha256']:
                raise JournalError('maintenance record digest differs')
            if existing is None and kind != 'begin':
                item = strict_json(raw)
                validate_record(request, item)
                self._validate_next(request, item)
            elif existing is None and kind == 'begin' and strict_json(raw) != self.begin:
                raise JournalError('maintenance begin changed before acknowledgement')
        settlement = None
        if kind in _SETTLEMENT_KINDS:
            current = request['current_backend']
            if self.current_backend is not None and self.current_backend != current:
                raise JournalError('current maintenance backend changed within invocation')
            if self.verify_backend(current, deadline) != current:
                raise JournalError('current maintenance backend readback differs')
            self.current_backend = current.copy()
            settlement_record = item if existing is None else existing[1]
            observer = settlement_record['coordinator']
            if (observer not in self.coordinators or
                    observer == self.coordinator and settlement_record['bootstrap'] != current or
                    observer != self.coordinator and
                    (observer['pid'], observer['starttime']) not in self.retired_coordinators):
                raise JournalError('settlement historical observer provenance differs')
            if network_verify.migration_scope(self.cgroup) != {'members': [self.coordinator['pid']], 'populated': 1}:
                raise JournalError('settlement scope has additional processes')
            old = self.begin['coordinator']
            if (old == self.coordinator or (old['pid'], old['starttime']) not in self.retired_coordinators or
                    any(not select.select([fd], [], [], 0)[0] for fd in self.children)):
                raise JournalError('settlement original scope was not directly retired')
            retired = []
            intent = self._get('intent')
            if intent is not None:
                retired.append(intent[1]['bootstrap'])
            if kind == 'ready-abandoned':
                ready = strict_json(self.directory.read('migration-ready-%020d.json' % self.generation))
                if ready['backend'] not in retired:
                    retired.append(ready['backend'])
            inventory = self.original_inventory
            roles = sorted(set(inventory['previous'] + [inventory['admin'], inventory['bootstrap'],
                                                        'houfeng_record_deletion_worker']))
            settlement = self.verify_settlement(current, retired, roles, deadline)
            if (type(settlement) is not dict or set(settlement) != {'application', 'current_backend',
                    'observer_backend', 'retired_backends', 'remaining_clients', 'prepared_transactions',
                    'transaction_committed'} or settlement['application'] != self.application or
                    settlement['current_backend'] != current or settlement['retired_backends'] != retired or
                    settlement['remaining_clients'] != 0 or settlement['prepared_transactions'] != 0 or
                    settlement['transaction_committed'] is not True):
                raise JournalError('settlement observation differs')
            from pg_bootstrap import _settlement_backend
            _settlement_backend(settlement['observer_backend'])
        live_digest = None
        if existing is None and kind == 'ready':
            witness_key = ('migration', self.recovery_id, self.application_digest,
                           request['phase_generation'], attempt, kind)
            observation_key = (request['phase_generation'], request['record_sha256'])
            if witness_key in self.journal.keys:
                cached = self.pending_ready_observations.get(observation_key)
                if cached is None:
                    raise JournalError('ready witness lacks its retained live observation')
                live_digest, live_evidence = cached
            else:
                live_digest, child_pidfd, live_evidence = observe_migration_child(
                    self.coordinator, self.pidfd, self.guard, self.cgroup,
                    self.program_sha256, item['child'], item['backend'], self.verify_backend, deadline)
                self.children.append(child_pidfd)
                self.pending_ready_observations[observation_key] = (live_digest, live_evidence)
        if existing is None:
            self._live()
        if existing is None:
            if kind == 'begin':
                item = self.begin
            self.pending_checkpoint = (dict(request), name, item, dict(pending), key, self.generation)
        predecessor = self.journal.previous if self.journal.sequence else None
        sequence, witness = self.journal.append('migration', self.recovery_id, self.application_digest,
            request['phase_generation'], attempt, kind, name, raw, predecessor, live_digest)
        if existing is None:
            self._reconcile_checkpoint()
        else:
            if kind == 'ready':
                cached = self.pending_ready_observations.get((request['phase_generation'], request['record_sha256']))
                event = strict_json(self.journal.entries[sequence - 1])
                if cached is None or event.get('live_observation_digest') != cached[0]:
                    raise JournalError('ready witness lacks its original live observation')
            self.continuity.accept(name, request['record_sha256'], pending)
        self._live()
        if time.monotonic() >= deadline:
            raise JournalError('maintenance checkpoint expired before acknowledgement')
        response = dict(request)
        response['op'] = 'maintenance_checkpoint_ack'
        if settlement is not None:
            response['observer_backend'] = settlement['observer_backend']
            self.settlement_observations.append({'kind': kind, 'phase_generation': request['phase_generation'],
                'witness_sequence': sequence, 'witness_digest': witness,
                'scope_retirement_digest': self.retired_coordinators[(old['pid'], old['starttime'])],
                'observation': copy.deepcopy(settlement)})
        response['witness_sequence'], response['witness_digest'] = sequence, witness
        return response

    def public_live_observations(self):
        self.journal.verify()
        return copy.deepcopy(self.live_observations)

    def public_settlement_observations(self):
        self.journal.verify()
        return copy.deepcopy(self.settlement_observations)

    def public_ready_records(self):
        self.journal.verify()
        result = []
        evidence_bytes = 2
        generations = set()
        for body in self.journal.entries:
            event = strict_json(body)
            if event['kind'] != 'ready-disposition':
                continue
            generation = event['phase_generation']
            name = 'migration-ready-%020d.json' % generation
            if generation in generations or event['record_name'] != name:
                raise JournalError('ready disposition evidence is not unique')
            raw = self.directory.read(name)
            digest = hashlib.sha256(raw).hexdigest()
            ready = strict_json(raw)
            validate_record({'kind': 'ready', 'phase_generation': generation, 'attempt': 0}, ready)
            if digest != event['record_sha256'] or len(raw) != event['record_bytes']:
                raise JournalError('ready disposition evidence bytes changed')
            generations.add(generation)
            # The shared result transport admits at most 4 MiB in total.
            if evidence_bytes + ((len(raw) + 2) // 3) * 4 > 4 * 1024 * 1024:
                raise JournalError('ready disposition evidence exceeds result budget')
            entry = {'phase_generation': generation, 'record_name': name,
                     'record_sha256': digest, 'raw_base64': base64.b64encode(raw).decode('ascii')}
            evidence_bytes += len(canonical(entry)) + 1
            if evidence_bytes > 4 * 1024 * 1024:
                raise JournalError('ready disposition evidence exceeds result budget')
            result.append(entry)
        return result

    def close(self):
        if self.pidfd is not None and not select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('cannot close a live maintenance coordinator')
        if any(not select.select([fd], [], [], 0)[0] for fd in self.children):
            raise JournalError('cannot close a live maintenance child')
        if self.pidfd is not None:
            os.close(self.pidfd)
            self.pidfd = None
        while self.children:
            os.close(self.children.pop())
