"""Root validation and one-use launcher permit for the fixed C80 callback."""
import errno
import fcntl
import hashlib
import os
import re
import select
import stat
import urllib.parse

from contract import (APPLICATION_KEYS, CGROUP_KEYS, CHILD_CGROUP, CONFIG_KEYS, COORDINATOR_KEYS,
                      GUARD, GUARD_KEYS, INTENT_KEYS, LAUNCHER, PREDECESSOR, REALM_CONTROL,
                      RUN_TOKEN, callback_intent_name, launch_authority_name)
from journal import MAX_FILE, JournalError, _read, _write_new, boot_id, process_start, strict_json
from phase import integer, open_control_dir, read_phases, read_private

HEX = re.compile(r'^[0-9a-f]{64}$')
F_GET_SEALS = 1034
ALL_SEALS = 0x0001 | 0x0002 | 0x0004 | 0x0008


def _digest_file(path, executable_mode):
    fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        parts = path.strip('/').split('/')
        for part in parts[:-1]:
            nxt = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            os.close(fd)
            fd = nxt
            s = os.fstat(fd)
            if s.st_uid != 0 or s.st_mode & 0o022:
                raise JournalError('unsafe installed program directory')
        item = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
        try:
            before = os.fstat(item)
            if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_nlink != 1 or
                    stat.S_IMODE(before.st_mode) != executable_mode or before.st_size <= 0):
                raise JournalError('installed program identity invalid')
            if path in (LAUNCHER, PREDECESSOR) and before.st_gid != 10002:
                raise JournalError('installed program group invalid')
            try:
                os.getxattr(item, 'security.capability')
            except OSError as exc:
                if exc.errno not in (errno.ENODATA, errno.ENOTSUP, errno.EOPNOTSUPP):
                    raise
            else:
                raise JournalError('installed program has file capabilities')
            h = hashlib.sha256()
            while True:
                block = os.read(item, 1024 * 1024)
                if not block:
                    break
                h.update(block)
            after = os.fstat(item)
            if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                    after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
                raise JournalError('installed program changed')
            return h.hexdigest()
        finally:
            os.close(item)
    finally:
        os.close(fd)


def _coordinator_live(pin):
    if (type(pin) is not dict or frozenset(pin) != COORDINATOR_KEYS or
            not integer(pin['pid'], (1 << 31) - 1) or not integer(pin['starttime'], (1 << 64) - 1) or
            type(pin['executable_sha256']) is not str or not HEX.fullmatch(pin['executable_sha256'])):
        raise JournalError('coordinator pin invalid')
    pid = pin['pid']
    if process_start(pid) != pin['starttime']:
        raise JournalError('coordinator process changed')
    with open('/proc/%d/status' % pid, 'r', encoding='ascii') as stream:
        rows = stream.read(65537).splitlines()
    if sum(len(row) for row in rows) > 65536:
        raise JournalError('coordinator status exceeds limit')
    entries = {}
    for row in rows:
        key, sep, value = row.partition(':')
        if sep and key in ('Uid', 'Gid', 'NoNewPrivs'):
            if key in entries:
                raise JournalError('duplicate coordinator status')
            entries[key] = value.strip()
    if (entries.get('Uid', '').split() != ['10002'] * 4 or entries.get('Gid', '').split() != ['10002'] * 4 or
            entries.get('NoNewPrivs') != '0'):
        raise JournalError('coordinator credentials invalid')
    executable = os.open('/proc/%d/exe' % pid, os.O_RDONLY | os.O_CLOEXEC)
    try:
        h = hashlib.sha256()
        while True:
            block = os.read(executable, 1024 * 1024)
            if not block:
                break
            h.update(block)
        if h.hexdigest() != pin['executable_sha256'] or process_start(pid) != pin['starttime']:
            raise JournalError('coordinator executable changed')
    finally:
        os.close(executable)


def _held_guard(pin, coordinator):
    if type(pin) is not dict or frozenset(pin) != GUARD_KEYS or not all(integer(pin[k], (1 << 64) - 1, 0) for k in GUARD_KEYS):
        raise JournalError('guard pin invalid')
    fd = os.open(GUARD, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 10002 or info.st_gid != 10002 or
                stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or
                (info.st_dev, info.st_ino) != (pin['dev'], pin['ino'])):
            raise JournalError('guard identity differs')
        held = False
        for name in os.listdir('/proc/%d/fd' % coordinator['pid']):
            if not name.isdecimal():
                raise JournalError('unexpected coordinator descriptor')
            try:
                item = os.stat('/proc/%d/fd/%s' % (coordinator['pid'], name))
            except FileNotFoundError:
                continue
            if (item.st_dev, item.st_ino) == (info.st_dev, info.st_ino):
                held = True
        if not held:
            raise JournalError('coordinator does not hold guard')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            pass
        else:
            fcntl.flock(fd, fcntl.LOCK_UN)
            raise JournalError('guard is not exclusively held')
    finally:
        os.close(fd)


def _config(intent, app_facts):
    pid, number = intent['coordinator']['pid'], intent['config_fd']
    if not integer(number, (1 << 31) - 1, 0):
        raise JournalError('configuration descriptor invalid')
    link = os.readlink('/proc/%d/fd/%d' % (pid, number))
    if not (link.startswith('/memfd:') and link.endswith(' (deleted)')):
        raise JournalError('configuration is not a memfd')
    fd = os.open('/proc/%d/fd/%d' % (pid, number), os.O_RDONLY | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= MAX_FILE or fcntl.fcntl(fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS:
            raise JournalError('configuration is not sealed')
        body = bytearray()
        while len(body) < info.st_size:
            chunk = os.read(fd, info.st_size - len(body))
            if not chunk:
                raise JournalError('configuration shortened')
            body.extend(chunk)
        if hashlib.sha256(body).hexdigest() != intent['config_digest']:
            raise JournalError('configuration digest differs')
        config = strict_json(bytes(body))
        if (type(config) is not dict or frozenset(config) != CONFIG_KEYS or type(config['version']) is not int or
                config['version'] != 1 or type(config['phase_generation']) is not int or
                config['phase_generation'] != intent['phase_generation'] or
                config['actor_user_id'] != intent['actor_user_id'] or config['record_id'] != intent['record_id'] or
                config['deployment_id'] != app_facts['deployment_id'] or config['project_id'] != app_facts['project_id'] or
                type(config['runtime_url']) is not str):
            raise JournalError('configuration binding differs')
        url = urllib.parse.urlsplit(config['runtime_url'])
        if (url.scheme not in ('postgres', 'postgresql') or url.hostname != app_facts['runtime_host'] or
                url.port != app_facts['runtime_port'] or
                url.netloc.rsplit('@', 1)[-1] != '%s:%s' % (app_facts['runtime_host'], app_facts['runtime_port']) or
                urllib.parse.unquote(url.username or '') != app_facts['runtime_role'] or
                url.path != '/' + intent['application']['DatabaseName'] or url.fragment or not url.password or
                urllib.parse.parse_qsl(url.query, keep_blank_values=True, strict_parsing=True) != [('sslmode', 'disable')]):
            raise JournalError('configuration endpoint differs')
    finally:
        os.close(fd)


def publish_permit(journal, expected_app, recovery_id, realm_digest, coordinator, coordinator_pidfd,
                   program_sha256, app_facts):
    """Verify actual callback files/FDs; create or exactly replay the root permit."""
    journal.verify()
    if not isinstance(realm_digest, str) or not HEX.fullmatch(realm_digest) or not HEX.fullmatch(program_sha256):
        raise JournalError('realm or supervisor digest invalid')
    if select.select([coordinator_pidfd], [], [], 0)[0]:
        raise JournalError('coordinator already exited')
    _coordinator_live(coordinator)
    phase = read_phases(expected_app, recovery_id)
    if phase['phase'] != 'predecessor_callback':
        raise JournalError('predecessor phase absent')
    directory = open_control_dir()
    try:
        intent = strict_json(read_private(directory, callback_intent_name(phase['generation'])))
    finally:
        os.close(directory)
    if (type(intent) is not dict or frozenset(intent) != INTENT_KEYS or type(intent['version']) is not int or
            intent['version'] != 1 or type(intent['phase_generation']) is not int or
            intent['phase_generation'] != phase['generation'] or intent['recovery_id'] != recovery_id or
            intent['application'] != expected_app or intent['realm_digest'] != realm_digest or
            intent['coordinator'] != coordinator):
        raise JournalError('callback intent does not bind the live phase')
    for name in ('config_digest', 'launcher_sha256', 'predecessor_sha256'):
        if type(intent[name]) is not str or not HEX.fullmatch(intent[name]):
            raise JournalError('callback digest invalid')
    for name in ('actor_user_id', 'record_id'):
        if type(intent[name]) is not str or not 0 < len(intent[name]) <= 256:
            raise JournalError('callback target invalid')
    cgroup = intent['child_cgroup']
    if (type(cgroup) is not dict or frozenset(cgroup) != CGROUP_KEYS or cgroup['path'] != CHILD_CGROUP or
            not integer(cgroup['dev'], (1 << 64) - 1, 0) or not integer(cgroup['ino'], (1 << 64) - 1)):
        raise JournalError('child cgroup pin invalid')
    info = os.stat(CHILD_CGROUP, follow_symlinks=False)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or
            (info.st_dev, info.st_ino) != (cgroup['dev'], cgroup['ino'])):
        raise JournalError('child cgroup changed')
    if _digest_file(LAUNCHER, 0o4750) != intent['launcher_sha256']:
        raise JournalError('launcher digest changed')
    if _digest_file(PREDECESSOR, 0o550) != intent['predecessor_sha256']:
        raise JournalError('predecessor digest changed')
    _held_guard(intent['guard'], coordinator)
    _config(intent, app_facts)
    _coordinator_live(coordinator)
    if select.select([coordinator_pidfd], [], [], 0)[0] or read_phases(expected_app, recovery_id) != phase:
        raise JournalError('callback phase or coordinator changed')
    permit = dict(intent, run_token=RUN_TOKEN, boot_id=boot_id(),
                  supervisor={'pid': os.getpid(), 'starttime': process_start(os.getpid()),
                              'program_sha256': program_sha256})
    control = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        name = launch_authority_name(phase['generation'])
        try:
            _write_new(control, name, permit)
        except FileExistsError:
            if strict_json(_read(control, name)) != permit:
                raise JournalError('existing launch authority conflicts')
    finally:
        os.close(control)
    return permit
