"""Read the coordinator's existing durable maintenance phase chain as root."""
import hashlib
import os
import re
import stat

from contract import APPLICATION_KEYS, LEDGER_DIR, PHASE_KEYS, phase_name
from journal import MAX_FILE, JournalError, strict_json

TRANSITIONS = {'': frozenset(('quiescent',)),
               'quiescent': frozenset(('predecessor_callback', 'migration_callback', 'refencing')),
               'predecessor_callback': frozenset(('refencing',)),
               'migration_callback': frozenset(('refencing',)),
               'refencing': frozenset(('quiescent',))}


def integer(value, maximum=(1 << 63) - 1, minimum=1):
    return type(value) is int and minimum <= value <= maximum


def application(value):
    return (type(value) is dict and frozenset(value) == APPLICATION_KEYS and
            all(type(value[k]) is str and 0 < len(value[k]) <= 256 for k in ('ID', 'SystemIdentifier', 'DatabaseName')) and
            integer(value['DatabaseOID'], (1 << 32) - 1) and integer(value['Epoch']))


def open_control_dir():
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for name in LEDGER_DIR.strip('/').split('/'):
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            os.close(fd)
            fd = child
        info = os.fstat(fd)
        if (info.st_uid != 10002 or info.st_gid != 10002 or stat.S_IMODE(info.st_mode) != 0o700 or
                not stat.S_ISDIR(info.st_mode)):
            raise JournalError('coordinator ledger directory invalid')
        return fd
    except BaseException:
        os.close(fd)
        raise


def _read_private(fd, name, limit):
    child = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
    try:
        before = os.fstat(child)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 10002 or before.st_gid != 10002 or
                stat.S_IMODE(before.st_mode) != 0o600 or before.st_nlink != 1 or
                not 0 < before.st_size <= limit):
            raise JournalError('coordinator ledger file invalid')
        data = bytearray()
        while len(data) < before.st_size:
            chunk = os.read(child, before.st_size - len(data))
            if not chunk:
                raise JournalError('coordinator ledger file shortened')
            data.extend(chunk)
        after = os.fstat(child)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise JournalError('coordinator ledger file changed')
        return bytes(data)
    finally:
        os.close(child)


def read_private(fd, name):
    return _read_private(fd, name, MAX_FILE)


def read_stdout_private(fd, generation):
    if not integer(generation):
        raise JournalError('stdout carrier phase is invalid')
    return _read_private(fd, 'predecessor-stdout-%020d.json' % generation, 32 * 1024)


def read_phases(expected_app, expected_recovery_id):
    if not application(expected_app) or type(expected_recovery_id) is not str or not expected_recovery_id:
        raise JournalError('invalid expected maintenance identity')
    directory = open_control_dir()
    try:
        names = sorted(name for name in os.listdir(directory) if name.startswith('phase-'))
        if not names or len(names) > 65536 or names != [phase_name(n) for n in range(1, len(names) + 1)]:
            raise JournalError('phase sequence missing, malformed or incomplete')
        previous = bytes(32)
        previous_phase = ''
        latest = None
        for generation, name in enumerate(names, 1):
            raw = read_private(directory, name)
            item = strict_json(raw)
            if (type(item) is not dict or frozenset(item) != PHASE_KEYS or type(item['version']) is not int or
                    item['version'] != 1 or type(item['generation']) is not int or item['generation'] != generation or
                    type(item['previous']) is not list or len(item['previous']) != 32 or
                    any(type(x) is not int or x < 0 or x > 255 for x in item['previous']) or
                    bytes(item['previous']) != previous or item['application'] != expected_app or
                    item['recovery_id'] != expected_recovery_id or type(item['phase']) is not str or
                    item['phase'] not in TRANSITIONS[previous_phase]):
                raise JournalError('phase chain or identity differs')
            previous = hashlib.sha256(raw).digest()
            previous_phase = item['phase']
            latest = item
        return latest
    finally:
        os.close(directory)
