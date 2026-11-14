"""Single-process, fsync'd root realm journal; never reissues after restart."""
import hashlib
import json
import os
import re
import stat

from census import inventory_digest
from contract import REALM_CONTROL, RUN_TOKEN

MAX_FILE = 16 * 1024
MAX_GENESIS = 512 * 1024
HEX32 = re.compile(r'^[0-9a-f]{64}$')
BOOT_ID = re.compile(r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
EVENT_KEYS = {
    'genesis': frozenset(('genesis_digest',)),
    'app_binding': frozenset(('app_digest',)),
    'phase_enter': frozenset(('phase', 'phase_generation')),
    'phase_exit': frozenset(('phase', 'phase_generation')),
    'admission': frozenset(('name', 'phase_generation', 'action_digest')),
    'registration': frozenset(('name', 'identity_digest')),
    'launch': frozenset(('name', 'pid', 'starttime', 'executable_sha256')),
    'exit': frozenset(('name', 'pid', 'starttime', 'status', 'pidfd_exited')),
    'observation': frozenset(('phase_generation', 'observation_sequence', 'roots_digest', 'valid')),
    'invalid': frozenset(('reason',)),
}


class JournalError(Exception):
    pass


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')


def strict_json(data):
    def pairs(values):
        result = {}
        for key, value in values:
            if key in result:
                raise JournalError('duplicate JSON field')
            result[key] = value
        return result
    def bad_constant(_):
        raise JournalError('non-finite JSON number')
    try:
        return json.loads(data.decode('utf-8', 'strict'), object_pairs_hook=pairs, parse_constant=bad_constant)
    except (ValueError, UnicodeError) as exc:
        raise JournalError('invalid JSON') from exc


def process_start(pid):
    with open('/proc/%d/stat' % pid, 'rb') as stream:
        data = stream.read(65537)
    if len(data) > 65536 or b') ' not in data:
        raise JournalError('process identity unavailable')
    fields = data[data.rfind(b') ') + 2:].split()
    if len(fields) <= 19 or not fields[19].isdigit():
        raise JournalError('process starttime unavailable')
    return int(fields[19])


def boot_id():
    with open('/proc/sys/kernel/random/boot_id', 'r', encoding='ascii') as stream:
        value = stream.read(128).strip()
    if not BOOT_ID.fullmatch(value):
        raise JournalError('boot identity unavailable')
    return value


def _directory(fd):
    s = os.fstat(fd)
    if not stat.S_ISDIR(s.st_mode) or s.st_uid != 0 or s.st_gid != 0 or stat.S_IMODE(s.st_mode) != 0o700:
        raise JournalError('realm control directory identity invalid')
    return (s.st_dev, s.st_ino)


def _read_bounded(fd, name, maximum):
    item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
    try:
        before = os.fstat(item)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0 or
                stat.S_IMODE(before.st_mode) != 0o600 or before.st_nlink != 1 or
                before.st_size <= 0 or before.st_size > maximum):
            raise JournalError('realm control file identity invalid')
        data = bytearray()
        while len(data) < before.st_size:
            block = os.read(item, before.st_size - len(data))
            if not block:
                raise JournalError('realm control file shortened')
            data.extend(block)
        after = os.fstat(item)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise JournalError('realm control file changed')
        return bytes(data)
    finally:
        os.close(item)


def _read(fd, name):
    return _read_bounded(fd, name, MAX_FILE)


def _read_genesis(fd):
    return _read_bounded(fd, 'realm-genesis.json', MAX_GENESIS)


def _write_new_bounded(fd, name, value, maximum):
    data = canonical(value)
    if not 0 < len(data) <= maximum:
        raise JournalError('realm control file exceeds limit')
    item = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                   0o600, dir_fd=fd)
    try:
        os.fchmod(item, 0o600)
        view = memoryview(data)
        while view:
            n = os.write(item, view)
            if n <= 0:
                raise JournalError('realm control write failed')
            view = view[n:]
        os.fsync(item)
    finally:
        os.close(item)
    os.fsync(fd)
    if _read_bounded(fd, name, maximum) != data:
        raise JournalError('realm control readback differs')
    return data


def _write_new(fd, name, value):
    return _write_new_bounded(fd, name, value, MAX_FILE)


def _write_genesis(fd, value):
    return _write_new_bounded(fd, 'realm-genesis.json', value, MAX_GENESIS)


class RealmJournal:
    def __init__(self, fd):
        self._fd = fd
        self._dir_identity = _directory(fd)
        self._pid = os.getpid()
        self._starttime = process_start(self._pid)
        self._boot = boot_id()
        self._sequence = 0
        self._previous = '0' * 64
        self._genesis = None
        self._app = None
        self._maintenance = None
        self.invalid = False

    @classmethod
    def create(cls):
        if os.geteuid() != 0 or os.getegid() != 0:
            raise JournalError('root supervisor required')
        # No existing directory is reused, even when a previous process died.
        os.mkdir(REALM_CONTROL, 0o700)
        fd = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            return cls(fd)
        except BaseException:
            os.close(fd)
            raise

    def _check_liveness(self):
        if self.invalid or os.getpid() != self._pid or process_start(self._pid) != self._starttime or boot_id() != self._boot:
            raise JournalError('root supervisor continuity unavailable')
        current = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            if _directory(current) != self._dir_identity or _directory(self._fd) != self._dir_identity:
                raise JournalError('realm control directory changed')
        finally:
            os.close(current)

    def register_maintenance(self, fd):
        self._check_liveness()
        if self._maintenance is not None:
            raise JournalError('maintenance witness directory already registered')
        identity = _directory(fd)
        named = os.stat('maintenance', dir_fd=self._fd, follow_symlinks=False)
        if (not stat.S_ISDIR(named.st_mode) or named.st_uid != 0 or named.st_gid != 0 or
                stat.S_IMODE(named.st_mode) != 0o700 or (named.st_dev, named.st_ino) != identity):
            raise JournalError('maintenance witness directory identity differs')
        self._maintenance = (os.dup(fd), identity)

    def genesis(self, facts):
        self._check_liveness()
        if self._genesis is not None or type(facts) is not dict or facts.get('run_token') != RUN_TOKEN:
            raise JournalError('invalid or duplicate genesis')
        if facts.get('boot_id') != self._boot or facts.get('supervisor_pid') != self._pid or facts.get('supervisor_starttime') != self._starttime:
            raise JournalError('genesis process binding differs')
        self._genesis = _write_genesis(self._fd, facts)
        self.append('genesis', {'genesis_digest': hashlib.sha256(self._genesis).hexdigest()})

    def bind_application(self, facts):
        self._check_liveness()
        if self._genesis is None or self._app is not None or type(facts) is not dict or facts.get('run_token') != RUN_TOKEN:
            raise JournalError('invalid or duplicate APP binding')
        self._app = _write_new(self._fd, 'realm-app.json', facts)
        self.append('app_binding', {'app_digest': hashlib.sha256(self._app).hexdigest()})
        return self.realm_digest()

    def realm_digest(self):
        self.verify()
        if self._genesis is None or self._app is None:
            raise JournalError('APP binding unavailable')
        return inventory_digest('HOUFENG-C80-REALM-V1', self._genesis, self._app).hex()

    def append(self, kind, fields):
        try:
            self._check_liveness()
            if kind not in EVENT_KEYS or type(fields) is not dict or frozenset(fields) != EVENT_KEYS[kind]:
                raise JournalError('unknown journal event')
            self._sequence += 1
            event = {'version': 1, 'sequence': self._sequence, 'previous': self._previous,
                     'kind': kind, 'fields': fields}
            data = _write_new(self._fd, 'journal-%020d.json' % self._sequence, event)
            self._previous = hashlib.sha256(data).hexdigest()
        except BaseException:
            self.invalid = True
            raise

    def verify(self):
        try:
            self._check_liveness()
            if self._genesis is None or _read_genesis(self._fd) != self._genesis:
                raise JournalError('genesis changed')
            if self._app is not None and _read(self._fd, 'realm-app.json') != self._app:
                raise JournalError('APP binding changed')
            fresh = os.open('.', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=self._fd)
            try:
                if _directory(fresh) != self._dir_identity:
                    raise JournalError('realm control directory changed')
                names = set(os.listdir(fresh))
            finally:
                os.close(fresh)
            if self._maintenance is not None:
                held, identity = self._maintenance
                named = os.stat('maintenance', dir_fd=self._fd, follow_symlinks=False)
                if ('maintenance' not in names or _directory(held) != identity or
                        not stat.S_ISDIR(named.st_mode) or named.st_uid != 0 or named.st_gid != 0 or
                        stat.S_IMODE(named.st_mode) != 0o700 or (named.st_dev, named.st_ino) != identity):
                    raise JournalError('registered maintenance witness directory changed')
            fixed = {'realm-genesis.json'} | ({'realm-app.json'} if self._app is not None else set())
            if self._maintenance is not None:
                fixed.add('maintenance')
            reboot_names = {'reboot-intent.json', 'reboot-credential.json'}
            if names & reboot_names:
                genesis = strict_json(self._genesis)
                if names & reboot_names != reboot_names or genesis.get('case') != 'source-recovery' or genesis.get('scenario') != 'vm-kill':
                    raise JournalError('reboot files are not confined to the prearmed negative')
            extra = {name for name in names if name not in fixed and name not in ('reboot-intent.json', 'reboot-credential.json')
                     and not re.fullmatch(r'journal-[0-9]{20}\.json', name)
                     and not re.fullmatch(r'launch-authority-[0-9]{20}\.json', name)
                     and not re.fullmatch(r'launch-used-[0-9]{20}', name)
                     and not re.fullmatch(r'heartbeat-(?:started|complete)-[0-9]{20}\.json', name)
                     and not re.fullmatch(r'fault-(?:intent|complete)-[a-z0-9-]+\.json', name)
                     and not re.fullmatch(r'callback-(?:live|join)-[0-9]{20}\.json', name)}
            if extra:
                raise JournalError('unrecognized realm control file')
            journal_names = {name for name in names if name.startswith('journal-')}
            expected = {'journal-%020d.json' % index for index in range(1, self._sequence + 1)}
            if journal_names != expected:
                raise JournalError('journal entries missing or unknown')
            previous = '0' * 64
            for index in range(1, self._sequence + 1):
                body = _read(self._fd, 'journal-%020d.json' % index)
                item = strict_json(body)
                if (type(item) is not dict or set(item) != {'version', 'sequence', 'previous', 'kind', 'fields'} or
                        item['version'] != 1 or type(item['sequence']) is not int or item['sequence'] != index or
                        item['previous'] != previous or item['kind'] not in EVENT_KEYS or
                        type(item['fields']) is not dict or frozenset(item['fields']) != EVENT_KEYS[item['kind']] or
                        canonical(item) != body):
                    raise JournalError('journal hash chain invalid')
                previous = hashlib.sha256(body).hexdigest()
            if previous != self._previous:
                raise JournalError('journal tail changed')
        except BaseException:
            self.invalid = True
            raise

    def close(self):
        self.invalid = True
        if self._maintenance is not None:
            os.close(self._maintenance[0])
            self._maintenance = None
        os.close(self._fd)
