"""Root-owned, descriptor-bound no-copy store census for the C80 realm.

The caller must create these roots before genesis and keep this object alive for
that realm.  An error permanently poisons the object; it never recreates a root
or turns a later empty directory into proof of an uninterrupted lifetime.
"""
import hashlib
import os
import stat
import struct

BASE = '/var/lib/g2'
BACKUP = 'managed-backups'
BACKUP_CHILDREN = ('staging', 'published', 'multipart', 'pins')
ROOTS = (('managed-backups', 'staging'), ('managed-backups', 'published'),
         ('managed-backups', 'multipart'), ('managed-backups', 'pins'),
         ('browser-profiles',), ('processor-workspaces',), ('portability-artifacts',))
OPEN_DIR = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


class CensusError(Exception):
    pass


def _frame(value):
    if len(value) > 0xffffffff:
        raise CensusError('inventory field exceeds framing limit')
    return struct.pack('>I', len(value)) + value


def inventory_digest(domain, *fields):
    h = hashlib.sha256()
    h.update(_frame(domain.encode('utf-8')))
    for value in fields:
        h.update(_frame(value))
    return h.digest()


def _u64(number):
    if type(number) is not int or not 0 <= number < (1 << 64):
        raise CensusError('inventory integer out of range')
    return struct.pack('>Q', number)


def _mount_id(fd):
    data = open('/proc/self/fdinfo/%d' % fd, 'rb').read(4097)
    if len(data) > 4096:
        raise CensusError('fdinfo exceeds limit')
    values = [line[7:].strip() for line in data.splitlines() if line.startswith(b'mnt_id:')]
    if len(values) != 1 or not values[0].isdigit():
        raise CensusError('mount identity unavailable')
    value = int(values[0])
    if value <= 0:
        raise CensusError('invalid mount identity')
    return value


def _identity(fd, private=True):
    s = os.fstat(fd)
    if (not stat.S_ISDIR(s.st_mode) or s.st_uid != 0 or s.st_gid != 0 or
            (stat.S_IMODE(s.st_mode) != 0o700 if private else bool(s.st_mode & 0o022))):
        raise CensusError('inventory root permissions or type invalid')
    return (s.st_dev, s.st_ino, _mount_id(fd))


def _names(fd):
    # Opening '.' yields an independent directory offset on every observation.
    scan = os.open('.', OPEN_DIR, dir_fd=fd)
    try:
        with os.scandir(scan) as entries:
            names = [os.fsencode(entry.name) for entry in entries]
        return sorted(names)
    finally:
        os.close(scan)


def _open_path(base_fd, parts):
    fd = os.dup(base_fd)
    try:
        for part in parts:
            next_fd = os.open(part, OPEN_DIR, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        return fd
    except BaseException:
        os.close(fd)
        raise


def _open_base():
    # Do not follow a symlink in any ancestor, including /var/lib/g2 itself.
    fd = os.open('/', OPEN_DIR)
    try:
        for part in ('var', 'lib', 'g2'):
            next_fd = os.open(part, OPEN_DIR, dir_fd=fd)
            os.close(fd)
            fd = next_fd
            _identity(fd, private=False)
        return fd
    except BaseException:
        os.close(fd)
        raise


class RootSet:
    def __init__(self, base_fd, handles, identities):
        self._base = base_fd
        self._handles = handles
        self._identities = identities
        self._base_identity = _identity(base_fd, private=False)
        self.invalid = False

    @classmethod
    def create(cls):
        """Exclusively create the fixed roots before genesis, or fail closed."""
        base = _open_base()
        handles = {}
        try:
            for name in (BACKUP, 'browser-profiles', 'processor-workspaces', 'portability-artifacts'):
                os.mkdir(name, 0o700, dir_fd=base)
            backup = os.open(BACKUP, OPEN_DIR, dir_fd=base)
            try:
                for name in BACKUP_CHILDREN:
                    os.mkdir(name, 0o700, dir_fd=backup)
                os.fsync(backup)
            finally:
                os.close(backup)
            os.fsync(base)
            paths = ((BACKUP,),) + ROOTS
            identities = {}
            for parts in paths:
                fd = _open_path(base, parts)
                handles[parts] = fd
                identities[parts] = _identity(fd)
            roots = cls(base, handles, identities)
            roots.scan()
            return roots
        except BaseException:
            for fd in handles.values():
                os.close(fd)
            os.close(base)
            raise

    def invalidate(self):
        self.invalid = True

    def scan(self):
        """Return stable digest of the actual fixed root identities and empty sets."""
        if self.invalid:
            raise CensusError('realm already invalid')
        current_base = None
        try:
            current_base = _open_base()
            if _identity(current_base, private=False) != self._base_identity or _identity(self._base, private=False) != self._base_identity:
                raise CensusError('inventory base replaced')
            for parts, expected in self._identities.items():
                fd = _open_path(current_base, parts)
                try:
                    if _identity(fd) != expected or _identity(self._handles[parts]) != expected:
                        raise CensusError('inventory root replaced')
                finally:
                    os.close(fd)
            backup_fd = self._handles[(BACKUP,)]
            if _names(backup_fd) != sorted(name.encode('ascii') for name in BACKUP_CHILDREN):
                raise CensusError('backup root contains unexpected entries')
            observed = []
            for parts in ((BACKUP,),) + ROOTS:
                fd = self._handles[parts]
                entries = _names(fd)
                if parts != (BACKUP,) and entries:
                    raise CensusError('managed copy root is not empty')
                dev, ino, mount_id = self._identities[parts]
                name = (BASE + '/' + '/'.join(parts)).encode('utf-8')
                # A collection is framed as a count followed by framed entries.
                encoded_entries = _u64(len(entries)) + b''.join(_frame(entry) for entry in entries)
                observed.append(_frame(name) + _u64(dev) + _u64(ino) + _u64(mount_id) + encoded_entries)
            collection = _u64(len(observed)) + b''.join(_frame(item) for item in observed)
            return inventory_digest('HOUFENG-C80-ROOTS-V1', collection).hex()
        except BaseException:
            self.invalid = True
            raise
        finally:
            if current_base is not None:
                os.close(current_base)

    def identities(self):
        if self.invalid:
            raise CensusError('realm already invalid')
        return {BASE + '/' + '/'.join(parts): {'dev': value[0], 'ino': value[1], 'mount_id': value[2]}
                for parts, value in self._identities.items()}

    def close(self):
        for fd in self._handles.values():
            os.close(fd)
        self._handles.clear()
        os.close(self._base)
        self.invalid = True
