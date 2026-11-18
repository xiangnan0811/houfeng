"""Close and continuously verify systemd activation before realm genesis."""
import base64
import contextlib
import hashlib
import os
import re
import select
import selectors
import stat
import subprocess
import sys
import time

from guest_storage import ENV
from journal import JournalError, canonical

# cloud-final is the fixed parent waiting for the supervisor.  The remaining
# services are needed by Docker's cgroup driver and guest device/logging setup.
ALLOWED = frozenset((
    'cloud-final.service', 'dbus.service', 'dbus.socket', 'g2-reboot-observer.service',
    'systemd-journald.service', 'systemd-journald.socket',
    'systemd-journald-dev-log.socket', 'systemd-journald-audit.socket',
    'systemd-udevd.service', 'systemd-udevd-control.socket', 'systemd-udevd-kernel.socket',
))
TYPES = 'service,socket,timer,path'
UNIT = re.compile(r'^[A-Za-z0-9_:.@\\-]+\.(?:service|socket|timer|path)$')
DEPENDENCY_UNIT = re.compile(r'^[A-Za-z0-9_:.@\\-]+\.[A-Za-z0-9_-]+$')
TMPFILES = 'systemd-tmpfiles-setup.service'
OVERRIDE_DIR = '/run/systemd/system/' + TMPFILES + '.d'
OVERRIDE = OVERRIDE_DIR + '/90-g2-manual-stop.conf'
OVERRIDE_BYTES = b'[Unit]\nRefuseManualStop=no\n'
NETPLAN = 'netplan-ovs-cleanup.service'
NETPLAN_RUNTIME = '/run/systemd/system/' + NETPLAN
NETPLAN_MASK = '/etc/systemd/system/' + NETPLAN
CONTROL_ROOT = '/run/systemd/system.control'
MULTIPATH = 'multipathd.service'
MULTIPATH_SOCKET = 'multipathd.socket'
MULTIPATH_PID = 'multipathd.pid'
UDEV_ALIAS = 'udev.service'
UDEV_CANONICAL = 'systemd-udevd.service'
UDEV_VENDOR = '/usr/lib/systemd/system/' + UDEV_ALIAS
FAILURE = '/var/log/g2-systemd-seal-failure.json'
MASK_FAILURE = '/var/log/g2-systemd-mask-paths.json'
LIMIT = 65536
INVENTORY_LIMIT = 1024 * 1024
PROPERTIES = (
    'Id', 'Names', 'FragmentPath', 'DropInPaths', 'LoadState', 'UnitFileState',
    'ActiveState', 'SubState', 'RefuseManualStop', 'MainPID', 'ControlPID',
    'ControlGroup', 'Type', 'RemainAfterExit', 'Restart', 'ExecStop', 'ExecStopPost',
    'PropagatesStopTo', 'StopPropagatedFrom', 'PartOf', 'BindsTo',
    'RequiredBy', 'RequisiteOf', 'BoundBy', 'ConsistsOf',
)
# The manager searches generator.early before /etc. Other higher-priority
# control/transient and attached directories are rejected when populated for a
# name we inspect; they cannot be silently omitted from the load path.
UNIT_PATHS = ('/run/systemd/generator.early', '/etc/systemd/system', '/run/systemd/system',
              '/run/systemd/generator', '/usr/local/lib/systemd/system',
              '/usr/lib/systemd/system', '/run/systemd/generator.late', '/lib/systemd/system')
VENDOR_UNIT_ROOTS = ('/usr/local/lib/systemd/system', '/usr/lib/systemd/system', '/lib/systemd/system')
MASK_ROOTS = ('/etc/systemd/system', '/run/systemd/system')
REJECT_UNIT_PATHS = ('/etc/systemd/system.control', '/run/systemd/system.control',
                     '/run/systemd/transient', '/run/systemd/generator.early',
                     '/etc/systemd/system.attached', '/run/systemd/system.attached')
_sealed_routes = None
_override_evidence = None
_override_identity = None
_override_transition = None
_netplan_mask = None
_control_masks = {}
_control_directory = None
_unit_path = None
_stop_preflight = None
_stop_transition = None
_stop_observer = None
_stop_transition_committed = False
_inventory_bytes = 0
_last_output = None
_seal_started = False
_failure_summary = None
_run_diag_active = False
_run_diag_baseline = None
_run_diag_before = None
_run_diag_first_change = None
_RUN_IDENTITY_FIELDS = ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtime_ns', 'ctime_ns')


class _Failure(JournalError):
    def __init__(self, operation, outcome, units=(), returncode=None, stdout=b'', stderr=b''):
        super().__init__('systemd seal %s: %s' % (operation, outcome))
        self.operation = operation
        self.outcome = outcome
        self.units = sorted(set(units))
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr


def _fail(operation, outcome='predicate', units=()):
    if _last_output is not None and _last_output[0] == operation:
        raise _Failure(operation, outcome, units, None, _last_output[1], _last_output[2])
    raise _Failure(operation, outcome, units)


def _command(operation, argv, deadline):
    """Run one fixed systemctl command, bounding both pipes while it runs."""
    global _inventory_bytes, _last_output
    _last_output = None
    units = [arg for arg in argv if UNIT.fullmatch(arg)]
    if time.monotonic() >= deadline:
        _fail(operation, 'timeout', units)
    end = min(deadline, time.monotonic() + 30)
    try:
        proc = subprocess.Popen(['/usr/bin/systemctl'] + argv, env=ENV, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True)
    except OSError as exc:
        raise _Failure(operation, 'spawn', units) from exc
    streams = {proc.stdout: bytearray(), proc.stderr: bytearray()}
    selector = selectors.DefaultSelector()
    outcome = None
    try:
        for stream in streams:
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ)
        while selector.get_map():
            remaining = end - time.monotonic()
            if remaining <= 0:
                outcome = 'timeout'
                break
            for key, _ in selector.select(remaining):
                data = os.read(key.fd, 16384)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                buf = streams[key.fileobj]
                remaining = LIMIT - len(buf)
                buf.extend(data[:remaining])
                if len(data) > remaining:
                    outcome = 'overflow'
                    break
            if outcome:
                break
        if outcome:
            proc.kill()
        try:
            rc = proc.wait(timeout=max(0.001, end - time.monotonic()) if not outcome else None)
        except subprocess.TimeoutExpired:
            outcome = 'timeout'
            proc.kill()
            rc = proc.wait()
    finally:
        selector.close()
        for stream in streams:
            stream.close()
        if proc.poll() is None:
            proc.kill()
            proc.wait()
    stdout, stderr = bytes(streams[proc.stdout]), bytes(streams[proc.stderr])
    if outcome:
        raise _Failure(operation, outcome, units, None, stdout, stderr)
    if rc:
        raise _Failure(operation, 'signal' if rc < 0 else 'exit', units, rc, stdout, stderr)
    if operation in ('files', 'units', 'jobs', 'show'):
        _inventory_bytes += len(stdout) + len(stderr)
        if _inventory_bytes > INVENTORY_LIMIT:
            raise _Failure(operation, 'overflow', units, None, stdout, stderr)
    _last_output = (operation, stdout, stderr)
    return stdout


def _persist_failure(exc):
    value = {'version': 1, 'operation': exc.operation, 'units': exc.units,
             'outcome': exc.outcome, 'returncode': exc.returncode,
             'stdout_base64': base64.b64encode(exc.stdout).decode('ascii'),
             'stderr_base64': base64.b64encode(exc.stderr).decode('ascii')}
    data = canonical(value)
    if len(data) > 192 * 1024:
        raise OSError('systemd failure evidence too large')
    parent = os.open(os.path.dirname(FAILURE), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(parent)
        if info.st_uid != 0 or info.st_mode & 0o002:
            raise OSError('unsafe failure evidence directory')
        fd = os.open(os.path.basename(FAILURE), os.O_RDWR | os.O_CREAT | os.O_EXCL |
                     os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent)
        try:
            os.fchmod(fd, 0o600)
            view = memoryview(data)
            while view:
                written = os.write(fd, view)
                if written <= 0:
                    raise OSError('short failure evidence write')
                view = view[written:]
            os.fsync(fd)
            os.fsync(parent)
            written_info = os.fstat(fd)
            if (not stat.S_ISREG(written_info.st_mode) or written_info.st_uid != 0 or
                    written_info.st_gid != 0 or stat.S_IMODE(written_info.st_mode) != 0o600 or
                    written_info.st_nlink != 1 or written_info.st_size != len(data)):
                raise OSError('unsafe failure evidence file')
            os.lseek(fd, 0, os.SEEK_SET)
            readback = bytearray()
            while len(readback) < len(data):
                chunk = os.read(fd, len(data) - len(readback))
                if not chunk:
                    raise OSError('short failure evidence read')
                readback.extend(chunk)
            named = os.stat(os.path.basename(FAILURE), dir_fd=parent, follow_symlinks=False)
            final = os.fstat(fd)
            directory = os.stat(os.path.dirname(FAILURE), follow_symlinks=False)
            identity = lambda item: (item.st_dev, item.st_ino, item.st_mode, item.st_uid,
                                     item.st_gid, item.st_nlink, item.st_size,
                                     item.st_mtime_ns, item.st_ctime_ns)
            if (bytes(readback) != data or identity(written_info) != identity(final) or
                    identity(final) != identity(named) or not stat.S_ISDIR(directory.st_mode) or
                    (directory.st_dev, directory.st_ino, directory.st_uid, directory.st_gid, directory.st_mode) !=
                    (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode)):
                raise OSError('failure evidence identity changed')
        finally:
            os.close(fd)
    finally:
        os.close(parent)
    return hashlib.sha256(data).hexdigest()


def _record_failure(exc):
    global _failure_summary
    if _failure_summary is not None:
        return
    artifact_sha256 = None
    try:
        artifact_sha256 = _persist_failure(exc)
    except OSError:
        exc.args = (str(exc) + '; private failure evidence unavailable',)
    operation = exc.operation if re.fullmatch(r'[a-z][a-z-]*', exc.operation) else 'systemd-seal'
    outcome = exc.outcome if exc.outcome in ('exit', 'signal', 'timeout', 'spawn', 'overflow', 'parse', 'predicate') else 'predicate'
    returncode = exc.returncode if type(exc.returncode) is int and outcome in ('exit', 'signal') else None
    _failure_summary = {'operation': operation, 'outcome': outcome, 'returncode': returncode,
                        'units': [name for name in exc.units if UNIT.fullmatch(name)],
                        'artifact_sha256': artifact_sha256}


def _diagnostic_stat(parent, name, path):
    try:
        info = os.stat(name, dir_fd=parent, follow_symlinks=False)
        item = {'path': path, 'state': 'present', 'uid': info.st_uid, 'gid': info.st_gid,
                'mode': info.st_mode, 'dev': info.st_dev, 'ino': info.st_ino,
                'nlink': info.st_nlink, 'size': info.st_size, 'ctime_ns': info.st_ctime_ns,
                'target': None}
        if stat.S_ISLNK(info.st_mode):
            item['target'] = os.readlink(name, dir_fd=parent)
        return item
    except FileNotFoundError:
        return {'path': path, 'state': 'absent'}
    except OSError as exc:
        return {'path': path, 'state': 'error', 'errno': exc.errno}


def _mask_paths_snapshot(units, deadline):
    """Inspect only fixed load paths, never following a symlink ancestor."""
    roots = list(dict.fromkeys(UNIT_PATHS + REJECT_UNIT_PATHS))
    result = []
    for root in roots:
        if time.monotonic() >= deadline:
            raise TimeoutError('mask path diagnostic deadline')
        parent = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        parents, current, inaccessible = [], '/', None
        try:
            for component in root.strip('/').split('/'):
                current = os.path.join(current, component)
                if inaccessible is not None:
                    parents.append({'path': current, 'state': inaccessible})
                    continue
                item = _diagnostic_stat(parent, component, current)
                parents.append(item)
                if item['state'] != 'present' or not stat.S_ISDIR(item['mode']):
                    inaccessible = 'absent' if item['state'] == 'absent' else 'blocked'
                    continue
                try:
                    child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                                    dir_fd=parent)
                except OSError:
                    inaccessible = 'blocked'
                    continue
                os.close(parent)
                parent = child
            entries = []
            for name in units:
                if time.monotonic() >= deadline:
                    raise TimeoutError('mask path diagnostic deadline')
                path = os.path.join(root, name)
                entries.append(_diagnostic_stat(parent, name, path) if inaccessible is None else
                               {'path': path, 'state': inaccessible})
            result.append({'root': root, 'parents': parents, 'entries': entries})
        finally:
            os.close(parent)
    return result


def _mask_diagnostic_show(units, deadline):
    properties = ('Id', 'Names', 'FragmentPath', 'LoadState', 'UnitFileState',
                  'MainPID', 'ControlPID', 'ControlGroup')
    names = [name for name in units if not _template(name)]
    if not names:
        return {'status': 'templates-only', 'rows': []}
    try:
        raw = _command('mask-diagnostic-show', ['show', '--all', '--no-pager',
                       '--property=' + ','.join(properties), '--'] + names, deadline)
        blocks = _text('mask-diagnostic-show', raw).strip().split('\n\n') if raw.strip() else []
        if len(blocks) != len(names):
            return {'status': 'invalid', 'rows': []}
        rows = []
        for name, block in zip(names, blocks):
            values = {}
            for line in block.splitlines():
                key, sep, value = line.partition('=')
                if not sep or key not in properties or key in values:
                    return {'status': 'invalid', 'rows': []}
                values[key] = value
            rows.append({'requested': name, 'properties': values})
        return {'status': 'ok', 'rows': rows}
    except _Failure as exc:
        return {'status': 'failed', 'outcome': exc.outcome, 'returncode': exc.returncode, 'rows': []}


def _persist_mask_diagnostic(value, deadline):
    data = canonical(value)
    if len(data) > 192 * 1024 or time.monotonic() >= deadline:
        raise OSError('mask diagnostic size or deadline')
    parent = os.open(os.path.dirname(MASK_FAILURE), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(parent)
        if info.st_uid != 0 or info.st_mode & 0o002:
            raise OSError('unsafe mask diagnostic directory')
        fd = os.open(os.path.basename(MASK_FAILURE), os.O_WRONLY | os.O_CREAT | os.O_EXCL |
                     os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent)
        try:
            os.fchmod(fd, 0o600)
            view = memoryview(data)
            while view:
                if time.monotonic() >= deadline:
                    raise TimeoutError('mask diagnostic deadline')
                written = os.write(fd, view)
                if written <= 0:
                    raise OSError('short mask diagnostic write')
                view = view[written:]
            os.fsync(fd)
            file_info = os.fstat(fd)
            named = os.stat(os.path.basename(MASK_FAILURE), dir_fd=parent, follow_symlinks=False)
            identity = lambda item: (item.st_dev, item.st_ino, item.st_mode, item.st_uid,
                                     item.st_gid, item.st_nlink, item.st_size, item.st_ctime_ns)
            if (not stat.S_ISREG(file_info.st_mode) or file_info.st_uid != 0 or file_info.st_gid != 0 or
                    stat.S_IMODE(file_info.st_mode) != 0o600 or file_info.st_nlink != 1 or
                    file_info.st_size != len(data) or identity(file_info) != identity(named)):
                raise OSError('unsafe mask diagnostic file')
            os.fsync(parent)
        finally:
            os.close(fd)
    finally:
        os.close(parent)


def _record_mask_diagnostic(exc, deadline):
    global _last_output, _inventory_bytes
    if (exc.operation not in ('mask-readback', 'alias-route', 'alias-manager') or _failure_summary is None or
            _failure_summary['artifact_sha256'] is None or not 0 < len(exc.units) <= 32 or
            any(len(name) > 255 or not UNIT.fullmatch(name) for name in exc.units)):
        return
    end = min(deadline, time.monotonic() + 5)
    saved_output, saved_inventory = _last_output, _inventory_bytes
    try:
        value = {'version': 1, 'primary_sha256': _failure_summary['artifact_sha256'],
                 'operation': exc.operation, 'units': exc.units,
                 'paths': _mask_paths_snapshot(exc.units, end),
                 'systemctl_show': _mask_diagnostic_show(exc.units, end)}
        _persist_mask_diagnostic(value, end)
    except Exception:
        # The original failure and its first private artifact remain authoritative.
        pass
    finally:
        _last_output, _inventory_bytes = saved_output, saved_inventory


def public_failure():
    """Return only validated first-failure metadata and the private artifact hash."""
    if _failure_summary is None:
        return None
    return dict(_failure_summary, units=list(_failure_summary['units']))


def _text(operation, raw):
    try:
        return raw.decode('ascii', 'strict')
    except UnicodeError as exc:
        raise _Failure(operation, 'parse', stdout=raw) from exc


def _decode_string_array(value):
    """Decode systemctl's shell-quoted string array without interpreting escapes."""
    if not isinstance(value, str) or any(ord(c) > 126 or (ord(c) < 32 and c != '\t') for c in value):
        raise ValueError('invalid array characters')
    items, seen = [], set()
    pos = 0
    while pos < len(value):
        while pos < len(value) and value[pos] in ' \t':
            pos += 1
        if pos == len(value):
            break
        item = []
        if value[pos] == '"':
            pos += 1
            while pos < len(value) and value[pos] != '"':
                char = value[pos]
                if char == '\\':
                    pos += 1
                    if pos == len(value) or value[pos] not in '\\"$`':
                        raise ValueError('invalid quoted escape')
                    char = value[pos]
                item.append(char)
                pos += 1
            if pos == len(value):
                raise ValueError('unclosed quote')
            pos += 1
            if pos < len(value) and value[pos] not in ' \t':
                raise ValueError('invalid token boundary')
        else:
            while pos < len(value) and value[pos] not in ' \t':
                if value[pos] == '"' or (value[pos] == '\\' and
                                           (pos + 1 == len(value) or value[pos + 1] in ' \t')):
                    raise ValueError('invalid token boundary')
                item.append(value[pos])
                pos += 1
        item = ''.join(item)
        if item in seen:
            raise ValueError('duplicate array element')
        seen.add(item)
        items.append(item)
    return items


def _array(value, operation, units=(), kind='unit'):
    try:
        items = _decode_string_array(value)
        if kind == 'path':
            valid = all(item.startswith('/') and not item.startswith('//') and item != '/' and
                        os.path.normpath(item) == item and all(32 <= ord(char) < 127 for char in item)
                        for item in items)
        else:
            pattern = UNIT if kind == 'name' else DEPENDENCY_UNIT
            valid = all(len(item) <= 255 and pattern.fullmatch(item) for item in items)
        if not valid:
            raise ValueError('invalid array element')
        return items
    except ValueError:
        _fail(operation, 'parse', units)


def _rows(operation, args, deadline):
    text = _text(operation, _command(operation, args + ['--no-pager', '--no-legend', '--plain'], deadline))
    rows = [line.split() for line in text.splitlines() if line.strip()]
    if len(rows) > 8192:
        _fail(operation, 'parse')
    return rows


def _files(deadline):
    rows = _rows('files', ['list-unit-files', '--all', '--type=' + TYPES], deadline)
    if (not rows or any(len(row) not in (2, 3) or not UNIT.fullmatch(row[0]) for row in rows) or
            len({row[0] for row in rows}) != len(rows)):
        _fail('files', 'parse')
    return {row[0]: row[1] for row in rows}


def _loaded(deadline):
    rows = _rows('units', ['list-units', '--all', '--type=' + TYPES], deadline)
    if (any(len(row) < 4 or not UNIT.fullmatch(row[0]) for row in rows) or
            len({row[0] for row in rows}) != len(rows)):
        _fail('units', 'parse')
    return [row[0] for row in rows]


def _jobs(deadline):
    rows = _rows('jobs', ['list-jobs', '--all'], deadline)
    if any(len(row) != 4 or not row[0].isdigit() or
           not re.fullmatch(r'[A-Za-z0-9_:.@\\-]+\.[A-Za-z0-9_-]+', row[1]) for row in rows):
        _fail('jobs', 'parse')
    if len({row[0] for row in rows}) != len(rows):
        _fail('jobs', 'parse')
    return tuple(sorted(tuple(row) for row in rows if UNIT.fullmatch(row[1])))


def _batches(values, size=32):
    values = sorted(values)
    for start in range(0, len(values), size):
        yield values[start:start + size]


def _show(names, deadline):
    result = {}
    for batch in _batches(names):
        raw = _command('show', ['show', '--all', '--no-pager',
                                 '--property=' + ','.join(PROPERTIES)] + batch, deadline)
        blocks = _text('show', raw).strip().split('\n\n') if raw.strip() else []
        if len(blocks) != len(batch):
            _fail('show', 'parse', batch)
        for requested, block in zip(batch, blocks):
            props = {}
            for line in block.splitlines():
                key, sep, value = line.partition('=')
                if not sep or key not in PROPERTIES:
                    _fail('show', 'parse', (requested,))
                if key in props:
                    # systemctl prints one line per configured exec command.
                    # Keep duplicate scalar properties and empty/mixed command
                    # entries invalid; tmpfiles still requires both lists empty.
                    if key not in ('ExecStop', 'ExecStopPost') or not props[key] or not value:
                        _fail('show', 'parse', (requested,))
                    props[key] += '\n' + value
                else:
                    props[key] = value
            if not {'Id', 'Names', 'LoadState', 'ActiveState', 'SubState', 'RefuseManualStop'} <= props.keys():
                _fail('show', 'parse', (requested,))
            ident = props['Id']
            nameset = _array(props['Names'], 'show', (requested,), 'name')
            if (not UNIT.fullmatch(ident) or not nameset or ident not in nameset or requested not in nameset or
                    (ident in result and result[ident] != props)):
                _fail('show', 'parse', (requested,))
            result[ident] = props
    return result


def _template(name):
    return '@.' in name


def _verify_direct_mask(root, name):
    path = os.path.join(root, name)
    info = os.lstat(path)
    if (root not in ('/etc/systemd/system', '/run/systemd/system') and
            not (root == CONTROL_ROOT and name in _control_masks) or
            not stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0):
        _fail('mask-readback', units=(name,))
    current = '/'
    for part in root.strip('/').split('/'):
        current = os.path.join(current, part)
        directory = os.lstat(current)
        if (not stat.S_ISDIR(directory.st_mode) or directory.st_uid != 0 or
                directory.st_gid != 0 or directory.st_mode & 0o022):
            _fail('mask-readback', units=(name,))
    if os.readlink(path) != '/dev/null' or os.path.realpath(path) != '/dev/null':
        _fail('mask-readback', units=(name,))
    after = os.lstat(path)
    if (info.st_dev, info.st_ino, info.st_ctime_ns) != (after.st_dev, after.st_ino, after.st_ctime_ns):
        _fail('mask-readback', units=(name,))
    return root, info.st_dev, info.st_ino


def _reject_higher_unit(name):
    roots = set(REJECT_UNIT_PATHS)
    if _unit_path is not None:
        roots.update(root for root in _unit_path if root not in UNIT_PATHS)
    for root in roots:
        path = os.path.join(root, name)
        if root == CONTROL_ROOT and name in _control_masks:
            _check_control_entry(name)
            continue
        try:
            parents = _netplan_parents(path)
        except FileNotFoundError:
            continue
        try:
            try:
                os.stat(name, dir_fd=parents[-1][1], follow_symlinks=False)
            except FileNotFoundError:
                continue
            _fail('mask-readback', units=(name,))
        finally:
            for _, fd, _, _, _ in parents:
                os.close(fd)


def _mask_path(name):
    _reject_higher_unit(name)
    if name in _control_masks:
        return _verify_direct_mask(CONTROL_ROOT, name)
    for root in UNIT_PATHS:
        if os.path.lexists(os.path.join(root, name)):
            return _verify_direct_mask(root, name)
    _fail('mask-readback', units=(name,))


def _route_entry(path):
    """Hold and compare each named component without following the last link."""
    root, name = os.path.split(path)
    if (root not in UNIT_PATHS and not (root == CONTROL_ROOT and name in _control_masks)) or not UNIT.fullmatch(name):
        _fail('alias-route', units=(name,))
    held = []
    try:
        parents = []
        current = '/'
        for part in root.strip('/').split('/'):
            current = os.path.join(current, part)
            fd = os.open(current, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC)
            info = os.fstat(fd)
            held.append((fd, current, info))
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
                _fail('alias-route', units=(name,))
            parents.append({'path': current, 'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode,
                            'uid': info.st_uid, 'gid': info.st_gid, 'ctime_ns': info.st_ctime_ns})
        fd = os.open(path, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC)
        info = os.fstat(fd)
        held.append((fd, path, info))
        if (info.st_uid != 0 or info.st_gid != 0 or info.st_nlink != 1 or
                not (stat.S_ISLNK(info.st_mode) or stat.S_ISREG(info.st_mode)) or
                (stat.S_ISREG(info.st_mode) and info.st_mode & 0o022)):
            _fail('alias-route', units=(name,))
        target = os.readlink(path) if stat.S_ISLNK(info.st_mode) else None
        result = {'path': path, 'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode,
                  'uid': info.st_uid, 'gid': info.st_gid, 'nlink': info.st_nlink,
                  'ctime_ns': info.st_ctime_ns, 'target': target, 'parents': parents}
        for held_fd, held_path, original in held:
            before, named = os.fstat(held_fd), os.lstat(held_path)
            identity = lambda item: (item.st_dev, item.st_ino, item.st_mode, item.st_uid, item.st_gid,
                                     item.st_nlink, item.st_ctime_ns)
            if identity(original) != identity(before) or identity(before) != identity(named):
                _fail('alias-route', units=(name,))
        return result
    finally:
        for fd, _, _ in held:
            os.close(fd)


def _alias_target(entry, original):
    target = entry['target']
    source = os.path.basename(entry['path'])
    if not target or target == '/dev/null' or _template(source):
        _fail('alias-route', units=(original,))
    path = target if target.startswith('/') else os.path.join(os.path.dirname(entry['path']), target)
    root, name = os.path.split(path)
    if (root not in UNIT_PATHS or not UNIT.fullmatch(name) or _template(name) or name == source or
            name in ALLOWED or name.rsplit('.', 1)[-1] != source.rsplit('.', 1)[-1]):
        _fail('alias-route', units=(original,))
    return path, name


def _alias_source(path, original):
    """Validate the literal linked-file chain independently of load priority."""
    entries, seen = [], set()
    while True:
        if path in seen or len(seen) >= 16:
            _fail('alias-route', units=(original,))
        seen.add(path)
        entry = _route_entry(path)
        entries.append(entry)
        if entry['target'] is None:
            return entries
        if entry['target'] == '/dev/null':
            _verify_direct_mask(os.path.dirname(path), os.path.basename(path))
            return entries
        path, _ = _alias_target(entry, original)


def _netplan_identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def _run_diagnostic_snapshot(stage, units):
    """Read bounded, immediate /run metadata without following entries."""
    result = {'stage': stage, 'units': list(units), 'monotonic_ns': time.monotonic_ns(),
              'root_before': None, 'root_after': None, 'root_named': None,
              'entries': [], 'complete': False, 'error': None}

    def identity(info):
        return [getattr(info, 'st_' + key) for key in _RUN_IDENTITY_FIELDS]

    fd = None
    try:
        fd = os.open('/run', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        result['root_before'] = identity(os.fstat(fd))
        entries = []
        with os.scandir(fd) as iterator:
            for entry in iterator:
                if len(entries) == 128:
                    result['error'] = 'entry_limit'
                    break
                try:
                    item = [entry.name, identity(entry.stat(follow_symlinks=False)), None]
                except OSError as exc:
                    item = [entry.name, None, exc.errno]
                entries.append(item)
            else:
                result['complete'] = True
        result['entries'] = sorted(entries, key=lambda item: item[0])
        if result['complete'] and any(item[1] is None for item in entries):
            result['complete'], result['error'] = False, 'entry_stat_failed'
        result['root_after'] = identity(os.fstat(fd))
        result['root_named'] = identity(os.stat('/run', follow_symlinks=False))
        if result['complete'] and (result['root_before'] != result['root_after'] or
                                   result['root_after'] != result['root_named']):
            result['complete'], result['error'] = False, 'directory_changed_during_scan'
        if len(canonical(result)) > 10 * 1024:
            result['entries'], result['complete'], result['error'] = [], False, 'byte_limit'
    except Exception:
        # Diagnostics cannot change acceptance or replace the first failure.
        result['entries'], result['complete'], result['error'] = [], False, 'snapshot_unavailable'
    finally:
        if fd is not None:
            try:
                os.close(fd)
            except OSError:
                pass
    return result


def _run_diagnostic_record(stage, units=(), *, baseline=False):
    global _run_diag_baseline, _run_diag_before, _run_diag_first_change
    try:
        snapshot = _run_diagnostic_snapshot(stage, units)
        if baseline:
            _run_diag_baseline = snapshot
        elif stage == 'before-stop':
            _run_diag_before = snapshot
        elif stage == 'after-stop':
            before = _run_diag_before
            if _run_diag_first_change is None:
                if before is None or not before['complete'] or not snapshot['complete']:
                    _run_diag_first_change = {'status': 'incomplete', 'before': before, 'after': snapshot}
                elif before['root_after'] != snapshot['root_after']:
                    _run_diag_first_change = {'status': 'changed', 'before': before, 'after': snapshot}
            _run_diag_before = None
    except Exception:
        # Preserve an explicit incomplete marker if diagnostics cannot observe the interval.
        if _run_diag_first_change is None:
            _run_diag_first_change = {'status': 'incomplete', 'before': None, 'after': None}


def _netplan_parent_failure(path, check, baseline, held, named, *, allowed_ctime_change=False):
    """Attach only bounded identity facts to the existing private first failure."""
    def identity(info):
        if info is None:
            return None
        return {key: getattr(info, 'st_' + key) for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns')}

    frame = sys._getframe(2)
    caller = {'function': frame.f_code.co_name, 'line': frame.f_lineno}
    outer = frame.f_back
    via = None if outer is None else {'function': outer.f_code.co_name, 'line': outer.f_lineno}
    evidence = {'path': path[:4096], 'path_truncated': len(path) > 4096, 'check': check,
                'baseline': identity(baseline), 'held': identity(held), 'named': identity(named),
                'allowed_ctime_change': allowed_ctime_change, 'caller': caller, 'via': via,
                'run': {'identity_fields': _RUN_IDENTITY_FIELDS, 'entry_fields': ('name', 'identity', 'errno'),
                        'control_baseline': _run_diag_baseline, 'first_change': _run_diag_first_change,
                        'pending_before': _run_diag_before,
                        'mismatch': _run_diagnostic_snapshot('parent-mismatch', ())}}
    detail = canonical(evidence)
    if len(detail) > LIMIT:
        evidence['run'] = {'complete': False, 'error': 'diagnostic_byte_limit'}
        detail = canonical(evidence)
    raise _Failure('netplan-parent', 'predicate', (NETPLAN,), stderr=detail)


def _netplan_parents(path):
    """Hold each directory and its parent, resolving every component without links."""
    held = []
    try:
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        held.append(('/', fd, None, None, os.fstat(fd)))
        current = ''
        for part in os.path.dirname(path).strip('/').split('/'):
            parent = fd
            fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            current += '/' + part
            info = os.fstat(fd)
            held.append((current, fd, parent, part, info))
        for path, fd, parent, part, info in held:
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
                try:
                    current = os.fstat(fd)
                    named = os.stat(part, dir_fd=parent, follow_symlinks=False) if parent is not None else os.stat('/')
                except OSError:
                    current, named = None, None
                _netplan_parent_failure(path, 'unsafe_attributes', info, current, named)
        _netplan_check_parents(held)
        return held
    except Exception:
        for _, fd, _, _, _ in held:
            os.close(fd)
        raise


def _netplan_check_parents(held, changed=()):
    updated = []
    for path, fd, parent, part, original in held:
        actual = os.fstat(fd)
        named = os.stat(part, dir_fd=parent, follow_symlinks=False) if parent is not None else os.stat('/')
        identity = lambda item: (item.st_dev, item.st_ino, item.st_mode, item.st_uid, item.st_gid)
        checks = (('held_baseline_identity', identity(actual) != identity(original)),
                  ('named_held_identity', identity(named) != identity(actual)),
                  ('held_named_ctime', actual.st_ctime_ns != named.st_ctime_ns),
                  ('baseline_ctime', path not in changed and actual.st_ctime_ns != original.st_ctime_ns))
        for check, failed in checks:
            if failed:
                _netplan_parent_failure(path, check, original, actual, named,
                                        allowed_ctime_change=path in changed)
        updated.append((path, fd, parent, part, actual))
    return updated


def _netplan_read(fd, parent, *, expected=None):
    before = os.fstat(fd)
    named = os.stat(NETPLAN, dir_fd=parent, follow_symlinks=False)
    if (not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) != 0o644 or
            (before.st_uid, before.st_gid, before.st_nlink, before.st_size) != (0, 0, 1, 337) or
            _netplan_identity(before) != _netplan_identity(named)):
        _fail('netplan-runtime', units=(NETPLAN,))
    data = os.pread(fd, 338, 0)
    after = os.fstat(fd)
    named_after = os.stat(NETPLAN, dir_fd=parent, follow_symlinks=False)
    if (len(data) != 337 or (expected is not None and data != expected) or
            _netplan_identity(before) != _netplan_identity(after) or
            _netplan_identity(after) != _netplan_identity(named_after)):
        _fail('netplan-runtime', units=(NETPLAN,))
    return data, _netplan_identity(after)


def _prepare_netplan_mask(forbidden, deadline):
    global _netplan_mask
    if NETPLAN not in forbidden or not os.path.lexists(NETPLAN_RUNTIME):
        return
    info = os.lstat(NETPLAN_RUNTIME)
    if not stat.S_ISREG(info.st_mode):
        return
    if _netplan_mask is not None or time.monotonic() >= deadline:
        _fail('netplan-preflight', units=(NETPLAN,))
    _reject_higher_unit(NETPLAN)
    shown = _show([NETPLAN], deadline)
    if set(shown) != {NETPLAN}:
        _fail('netplan-preflight', units=(NETPLAN,))
    props = shown[NETPLAN]
    required = {'Id', 'Names', 'LoadState', 'UnitFileState', 'FragmentPath', 'RefuseManualStop',
                'ActiveState', 'MainPID', 'ControlPID', 'DropInPaths', 'ControlGroup'}
    if (not required <= props.keys() or props['Id'] != NETPLAN or
            _array(props['Names'], 'netplan-preflight', (NETPLAN,), 'name') != [NETPLAN] or
            props['LoadState'] != 'loaded' or props.get('UnitFileState') != 'enabled-runtime' or
            props.get('FragmentPath') != NETPLAN_RUNTIME or props['RefuseManualStop'] != 'no' or
            props['ActiveState'] not in ('inactive', 'failed') or props.get('MainPID') != '0' or
            props.get('ControlPID') != '0' or _array(props.get('DropInPaths', ''), 'netplan-preflight',
                                                    (NETPLAN,), 'path') or
            props.get('ExecStop', '') or props.get('ExecStopPost', '') or
            any(job[1] == NETPLAN for job in _jobs(deadline))):
        _fail('netplan-preflight', units=(NETPLAN,))
    _empty_cgroup(props.get('ControlGroup', ''), NETPLAN)
    etc = runtime = None
    runtime_fd = mask_fd = None
    try:
        etc, runtime = _netplan_parents(NETPLAN_MASK), _netplan_parents(NETPLAN_RUNTIME)
        etc_fd, runtime_parent = etc[-1][1], runtime[-1][1]
        try:
            os.stat(NETPLAN, dir_fd=etc_fd, follow_symlinks=False)
        except FileNotFoundError:
            pass
        else:
            _fail('netplan-existing-mask', units=(NETPLAN,))
        runtime_fd = os.open(NETPLAN, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=runtime_parent)
        data, identity = _netplan_read(runtime_fd, runtime_parent)
        os.symlink('/dev/null', NETPLAN, dir_fd=etc_fd)
        os.fsync(etc_fd)
        mask_fd = os.open(NETPLAN, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=etc_fd)
        mask = os.fstat(mask_fd)
        named = os.stat(NETPLAN, dir_fd=etc_fd, follow_symlinks=False)
        if (not stat.S_ISLNK(mask.st_mode) or (mask.st_uid, mask.st_gid, mask.st_nlink) != (0, 0, 1) or
                _netplan_identity(mask) != _netplan_identity(named) or
                os.readlink(NETPLAN, dir_fd=etc_fd) != '/dev/null'):
            _fail('netplan-mask', units=(NETPLAN,))
        etc = _netplan_check_parents(etc, (os.path.dirname(NETPLAN_MASK),))
        runtime = _netplan_check_parents(runtime)
        if _netplan_read(runtime_fd, runtime_parent, expected=data)[1] != identity:
            _fail('netplan-runtime', units=(NETPLAN,))
        _netplan_check_parents(etc)
        _netplan_check_parents(runtime)
        _netplan_mask = {'etc': etc, 'runtime': runtime, 'runtime_fd': runtime_fd, 'mask_fd': mask_fd,
                         'mask_identity': _netplan_identity(mask), 'runtime_identity': identity, 'bytes': data,
                         'sha256': hashlib.sha256(data).hexdigest()}
        etc = runtime = runtime_fd = mask_fd = None
    finally:
        for fd in (runtime_fd, mask_fd):
            if fd is not None:
                os.close(fd)
        for parents in (etc, runtime):
            if parents is not None:
                for _, fd, _, _, _ in parents:
                    os.close(fd)


def _check_netplan_mask(deadline, *, after_reload=False):
    state = _netplan_mask
    if state is None or time.monotonic() >= deadline:
        _fail('netplan-mask', units=(NETPLAN,))
    _reject_higher_unit(NETPLAN)
    changed = (os.path.dirname(NETPLAN_RUNTIME), os.path.dirname(os.path.dirname(NETPLAN_RUNTIME))) if after_reload else ()
    etc = _netplan_check_parents(state['etc'])
    runtime = _netplan_check_parents(state['runtime'], changed)
    etc_fd, runtime_parent = etc[-1][1], runtime[-1][1]
    mask = os.fstat(state['mask_fd'])
    named = os.stat(NETPLAN, dir_fd=etc_fd, follow_symlinks=False)
    if (_netplan_identity(mask) != state['mask_identity'] or _netplan_identity(named) != state['mask_identity'] or
            os.readlink(NETPLAN, dir_fd=etc_fd) != '/dev/null'):
        _fail('netplan-mask', units=(NETPLAN,))
    if after_reload:
        held_before = os.fstat(state['runtime_fd'])
        held_data = os.pread(state['runtime_fd'], 338, 0)
        held_after = os.fstat(state['runtime_fd'])
        original = state['runtime_identity']
        if (held_data != state['bytes'] or _netplan_identity(held_before) != _netplan_identity(held_after) or
                (held_before.st_dev, held_before.st_ino, held_before.st_mode, held_before.st_uid,
                 held_before.st_gid, held_before.st_size) !=
                (original[0], original[1], original[2], original[3], original[4], original[6])):
            _fail('netplan-runtime', units=(NETPLAN,))
        new_fd = os.open(NETPLAN, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=runtime_parent)
        try:
            _, identity = _netplan_read(new_fd, runtime_parent, expected=state['bytes'])
        except Exception:
            os.close(new_fd)
            raise
        os.close(state['runtime_fd'])
        state['runtime_fd'], state['runtime_identity'] = new_fd, identity
        state['etc'], state['runtime'] = etc, runtime
    elif _netplan_read(state['runtime_fd'], runtime_parent, expected=state['bytes'])[1] != state['runtime_identity']:
        _fail('netplan-runtime', units=(NETPLAN,))
    _netplan_check_parents(etc)
    _netplan_check_parents(runtime)


def _manager_unit_path(deadline):
    raw = _command('unit-path', ['show', '--property=UnitPath', '--value'], deadline)
    text = _text('unit-path', raw)
    if not text.endswith('\n') or '\n' in text[:-1]:
        _fail('unit-path', 'parse')
    paths = _array(text[:-1], 'unit-path', kind='path')
    required = (CONTROL_ROOT, '/etc/systemd/system', '/run/systemd/system')
    if (len(paths) > 64 or any(paths.count(path) != 1 for path in required) or
            paths.index(CONTROL_ROOT) >= min(paths.index(required[1]), paths.index(required[2]))):
        _fail('unit-path')
    return paths


def _check_control_entry(name):
    state = _control_masks[name]
    actual = os.fstat(state['fd'])
    named = os.stat(name, dir_fd=_control_directory['parents'][-1][1], follow_symlinks=False)
    if (_netplan_identity(actual) != state['identity'] or _netplan_identity(named) != state['identity'] or
            not stat.S_ISLNK(actual.st_mode) or (actual.st_uid, actual.st_gid, actual.st_nlink) != (0, 0, 1) or
            os.readlink(name, dir_fd=_control_directory['parents'][-1][1]) != '/dev/null'):
        _fail('control-mask', units=(name,))


def _check_control_masks(deadline, *, check_unit_path=True):
    if _unit_path is None or (check_unit_path and _manager_unit_path(deadline) != _unit_path):
        _fail('unit-path')
    if _control_directory is None:
        if _control_masks:
            _fail('control-mask')
        if _run_diag_active:
            _run_diagnostic_record('control-check', baseline=True)
        return
    parents = _netplan_check_parents(_control_directory['parents'])
    if sorted(os.listdir(parents[-1][1])) != sorted(_control_masks):
        _fail('control-directory')
    for name in sorted(_control_masks):
        _check_control_entry(name)
        origin = _control_masks[name]['origin']
        entries = [alias['link'] for alias in origin['aliases']]
        entries += [item for alias in origin['aliases'] for item in alias['source']]
        if _sealed_routes is not None:
            target = _sealed_routes.get(origin['terminal'])
            if target is None or target.get('control_origin') is not None:
                _fail('control-origin', units=(name,))
            entries.append(target['mask'])
        with _held_route(entries, name):
            pass
    _netplan_check_parents(parents)
    if _run_diag_active:
        _run_diagnostic_record('control-check', baseline=True)


def _prepare_control_masks(routes, forbidden, deadline):
    global _control_directory
    names = sorted(name for name, route in routes.items() if route['aliases'])
    if not names:
        return
    if _control_directory is not None or _control_masks:
        _fail('control-directory')
    for name in names:
        route = routes[name]
        if name not in forbidden or route['terminal'] not in forbidden or _template(name):
            _fail('control-preflight', units=(name,))
    for name, route in routes.items():
        entries = [entry for entry in (route.get('mask'), route.get('vendor_mask'), route.get('protected_alias'))
                   if entry is not None]
        for alias in route['aliases']:
            entries.append(alias['link'])
            entries.extend(alias['source'])
        with _held_route(entries, name):
            pass
    if _netplan_mask is not None:
        _check_netplan_mask(deadline)
    parents = _netplan_parents(CONTROL_ROOT)
    try:
        parent = parents[-1][1]
        leaf = os.path.basename(CONTROL_ROOT)
        try:
            info = os.stat(leaf, dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            os.mkdir(leaf, 0o700, dir_fd=parent)
            os.fsync(parent)
            parents = _netplan_check_parents(parents, (os.path.dirname(CONTROL_ROOT),))
            info = os.stat(leaf, dir_fd=parent, follow_symlinks=False)
            if (not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or
                    info.st_uid != 0 or info.st_gid != 0):
                _fail('control-directory')
            probe = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            try:
                if _netplan_identity(os.fstat(probe)) != _netplan_identity(info) or os.listdir(probe):
                    _fail('control-directory')
                _netplan_check_parents(parents)
            finally:
                os.close(probe)
            if _netplan_mask is not None:
                _netplan_mask['runtime'] = _netplan_check_parents(
                    _netplan_mask['runtime'], (os.path.dirname(CONTROL_ROOT),))
                _check_netplan_mask(deadline)
        else:
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
                _fail('control-directory')
        control_fd = os.open(leaf, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
        actual = os.fstat(control_fd)
        if _netplan_identity(actual) != _netplan_identity(info) or os.listdir(control_fd):
            os.close(control_fd)
            _fail('control-directory')
        parents.append((CONTROL_ROOT, control_fd, parent, leaf, actual))
        _netplan_check_parents(parents)
        _control_directory = {'parents': parents}
        parents = None
        for name in names:
            route = routes[name]
            if name not in forbidden or route['terminal'] not in forbidden or _template(name):
                _fail('control-preflight', units=(name,))
            os.symlink('/dev/null', name, dir_fd=control_fd)
            os.fsync(control_fd)
            fd = os.open(name, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=control_fd)
            info = os.fstat(fd)
            named = os.stat(name, dir_fd=control_fd, follow_symlinks=False)
            if (not stat.S_ISLNK(info.st_mode) or (info.st_uid, info.st_gid, info.st_nlink) != (0, 0, 1) or
                    _netplan_identity(info) != _netplan_identity(named) or
                    os.readlink(name, dir_fd=control_fd) != '/dev/null'):
                os.close(fd)
                _fail('control-mask', units=(name,))
            _control_masks[name] = {'fd': fd, 'identity': _netplan_identity(info),
                                    'origin': {'terminal': route['terminal'], 'aliases': route['aliases']}}
        os.fsync(control_fd)
        if sorted(os.listdir(control_fd)) != names:
            _fail('control-directory')
        _control_directory['parents'] = _netplan_check_parents(_control_directory['parents'], (CONTROL_ROOT,))
    finally:
        if parents is not None:
            for _, fd, _, _, _ in parents:
                os.close(fd)


def _route(name, allow_unmasked=False, vendor_sources=None, *, deadline=None):
    original, seen, aliases = name, set(), []
    if name in _control_masks:
        return name, [], _mask_path(name)
    while True:
        if name in seen or len(seen) >= 16 or name in ALLOWED:
            _fail('alias-route', units=(original,))
        seen.add(name)
        _reject_higher_unit(name)
        for root in MASK_ROOTS:
            path = os.path.join(root, name)
            if os.path.lexists(path):
                entry = _route_entry(path)
                if entry['target'] == '/dev/null':
                    _verify_direct_mask(root, name)
                elif (path == NETPLAN_RUNTIME and original == name == NETPLAN and
                      entry['target'] is None and _netplan_mask is not None):
                    _check_netplan_mask(deadline if deadline is not None else time.monotonic() + 30)
                elif entry['target'] is None or _template(name):
                    _fail('mask-readback', units=(name,))
        selected = next((os.path.join(root, name) for root in UNIT_PATHS
                         if os.path.lexists(os.path.join(root, name))), None)
        if selected is None:
            if allow_unmasked and name == original:
                return name, aliases, None
            _fail('alias-route', units=(original,))
        entry = _route_entry(selected)
        if entry['target'] == '/dev/null':
            if allow_unmasked and os.path.dirname(selected) in VENDOR_UNIT_ROOTS:
                if os.path.realpath(selected) != '/dev/null':
                    _fail('mask-readback', units=(name,))
                if vendor_sources is not None:
                    vendor_sources[name] = entry
                return name, aliases, None
            return name, aliases, _mask_path(name)
        if entry['target'] is None or _template(name):
            if allow_unmasked:
                return name, aliases, None
            _fail('mask-readback', units=(name,))
        if os.path.lexists(os.path.join('/run/systemd/system', name)) and selected != os.path.join('/run/systemd/system', name):
            _fail('alias-route', units=(original,))
        path, target = _alias_target(entry, original)
        aliases.append({'link': entry, 'target_name': target, 'source': _alias_source(path, original)})
        name = target


def _udev_vendor_alias(deadline):
    """Recognize only the pinned alias to the separately allowed udev unit."""
    if not os.path.lexists(UDEV_VENDOR):
        return None
    vendor = _route_entry(UDEV_VENDOR)
    if vendor['target'] != UDEV_CANONICAL:
        return None
    _reject_higher_unit(UDEV_ALIAS)
    for root in UNIT_PATHS:
        candidate = os.path.join(root, UDEV_ALIAS)
        if candidate == UDEV_VENDOR:
            break
        if os.path.lexists(candidate):
            _fail('udev-alias-preflight', units=(UDEV_ALIAS,))
    else:
        _fail('udev-alias-preflight', units=(UDEV_ALIAS,))
    with _held_route([vendor], UDEV_ALIAS):
        shown = _show([UDEV_ALIAS], deadline)
        if set(shown) != {UDEV_CANONICAL}:
            _fail('udev-alias-preflight', units=(UDEV_ALIAS,))
        props = shown[UDEV_CANONICAL]
        if (props['Id'] != UDEV_CANONICAL or props['LoadState'] != 'loaded' or
                not {UDEV_ALIAS, UDEV_CANONICAL} <=
                set(_array(props['Names'], 'udev-alias-preflight', (UDEV_ALIAS,), 'name'))):
            _fail('udev-alias-preflight', units=(UDEV_ALIAS,))
    return vendor


def _install_masks(forbidden, deadline):
    global _unit_path
    _unit_path = _manager_unit_path(deadline)
    routes, missing = {}, set()
    for name in sorted(forbidden):
        vendor_sources = {}
        protected_alias = _udev_vendor_alias(deadline) if name == UDEV_ALIAS else None
        if protected_alias is not None:
            terminal, aliases, mask = name, [], None
        else:
            terminal, aliases, mask = _route(name, True, vendor_sources, deadline=deadline)
        if terminal not in forbidden:
            _fail('alias-route', units=(name,))
        routes[name] = {'terminal': terminal, 'aliases': aliases, 'vendor_mask': vendor_sources.get(terminal),
                        'mask': _route_entry(os.path.join(mask[0], terminal)) if mask else None,
                        'created_mask': mask is None, 'protected_alias': protected_alias}
        if mask is None:
            missing.add(terminal)
    protected = routes.get(UDEV_ALIAS, {}).get('protected_alias')
    if protected is not None:
        _reject_higher_unit(UDEV_ALIAS)
        if (_route_entry(protected['path']) != protected or
                any(os.path.lexists(os.path.join(root, UDEV_ALIAS)) for root in MASK_ROOTS)):
            _fail('udev-alias-preflight', units=(UDEV_ALIAS,))
    _prepare_control_masks(routes, forbidden, deadline)
    for batch in _batches(missing):
        _command('mask', ['--runtime', '--no-reload', 'mask'] + batch, deadline)
    for route in routes.values():
        if route['mask'] is None:
            root, _, _ = _mask_path(route['terminal'])
            route['mask'] = _route_entry(os.path.join(root, route['terminal']))
    for name, control in _control_masks.items():
        route = routes[name]
        route['control_origin'] = control['origin']
        route['terminal'], route['aliases'] = name, []
        route['mask'] = _route_entry(os.path.join(CONTROL_ROOT, name))
        route['vendor_mask'] = None
    return routes


def _recapture_routes(routes):
    """Refresh only authorized parent ctime changes after masks and reload."""
    allowed = {'/run/systemd'}
    if _netplan_mask is not None or any(route['created_mask'] for route in routes.values()):
        allowed.add('/run/systemd/system')
    if _control_directory is not None:
        allowed.add(CONTROL_ROOT)
        _control_directory['parents'] = _netplan_check_parents(
            _control_directory['parents'], ('/run/systemd',))

    def refresh(old, name):
        new = _route_entry(old['path'])
        if {key: value for key, value in old.items() if key != 'parents'} != {
                key: value for key, value in new.items() if key != 'parents'}:
            _fail('alias-route', units=(name,))
        if len(old['parents']) != len(new['parents']):
            _fail('alias-route', units=(name,))
        for before, after in zip(old['parents'], new['parents']):
            if ({key: value for key, value in before.items() if key != 'ctime_ns'} !=
                    {key: value for key, value in after.items() if key != 'ctime_ns'} or
                    (before['path'] not in allowed and before['ctime_ns'] != after['ctime_ns'])):
                _fail('alias-route', units=(name,))
        return new

    for name, route in routes.items():
        route['mask'] = refresh(route['mask'], name)
        if route['vendor_mask'] is not None:
            route['vendor_mask'] = refresh(route['vendor_mask'], name)
        if route.get('protected_alias') is not None:
            route['protected_alias'] = refresh(route['protected_alias'], name)
        for alias in route['aliases']:
            alias['link'] = refresh(alias['link'], name)
            alias['source'] = [refresh(entry, name) for entry in alias['source']]
        origin = route.get('control_origin')
        if origin is not None:
            for alias in origin['aliases']:
                alias['link'] = refresh(alias['link'], name)
                alias['source'] = [refresh(entry, name) for entry in alias['source']]


@contextlib.contextmanager
def _held_route(entries, name):
    expected = {}
    for entry in entries:
        for item in entry['parents'] + [entry]:
            path = item['path']
            if path in expected and expected[path] != item:
                _fail('alias-route', units=(name,))
            expected[path] = item
    held = []
    try:
        for path, item in sorted(expected.items()):
            fd = os.open(path, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC)
            held.append((fd, path, item))
        def check():
            for fd, path, item in held:
                actual, named = os.fstat(fd), os.lstat(path)
                for info in (actual, named):
                    if any(getattr(info, 'st_' + key) != item[key] for key in
                           ('dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns')):
                        _fail('alias-route', units=(name,))
                    if 'nlink' in item and info.st_nlink != item['nlink']:
                        _fail('alias-route', units=(name,))
                if 'target' in item and item['target'] is not None and os.readlink(path) != item['target']:
                    _fail('alias-route', units=(name,))
        check()
        yield
        check()
    finally:
        for fd, _, _ in held:
            os.close(fd)


def _closed_route(name, state, files, deadline, *, require_quiet=True):
    if state not in ('masked', 'masked-runtime'):
        _fail('mask-readback', units=(name,))
    terminal, aliases, mask = _route(name, deadline=deadline)
    original = _sealed_routes.get(name) if _sealed_routes is not None else None
    if (original is None or original.get('terminal') != terminal or original.get('aliases') != aliases or
            terminal not in files or files[terminal] not in ('masked', 'masked-runtime')):
        _fail('alias-route', units=(name,))
    root, dev, ino = mask
    if not aliases and (state == 'masked') != (root == '/etc/systemd/system'):
        _fail('mask-readback', units=(name,))
    path = os.path.join(root, terminal)
    mask_entry = _route_entry(path)
    if ((mask_entry['dev'], mask_entry['ino'], mask_entry['target']) != (dev, ino, '/dev/null') or
            mask_entry != original.get('mask')):
        _fail('mask-readback', units=(name,))
    manager = []
    entries = [mask_entry]
    vendor = original.get('vendor_mask')
    if vendor is not None:
        if _route_entry(vendor['path']) != vendor:
            _fail('alias-route', units=(name,))
        entries.append(vendor)
    protected = original.get('protected_alias')
    if protected is not None:
        if (name != UDEV_ALIAS or terminal != UDEV_ALIAS or aliases or state != 'masked-runtime' or
                path != '/run/systemd/system/' + UDEV_ALIAS or protected['path'] != UDEV_VENDOR or
                protected['target'] != UDEV_CANONICAL or _route_entry(UDEV_VENDOR) != protected):
            _fail('udev-alias-readback', units=(name,))
        entries.append(protected)
    for alias in aliases:
        entries.append(alias['link'])
        entries.extend(alias['source'])
    control_origin = original.get('control_origin')
    if control_origin is not None:
        if name not in _control_masks or path != os.path.join(CONTROL_ROOT, name):
            _fail('control-mask', units=(name,))
        for alias in control_origin['aliases']:
            entries.append(alias['link'])
            entries.extend(alias['source'])
    with _held_route(entries, name):
        if control_origin is not None:
            _check_control_masks(deadline, check_unit_path=False)
            shown = _show([name], deadline)
            if set(shown) != {name}:
                _fail('control-manager', units=(name,))
            props = shown[name]
            if (props['Id'] != name or _array(props['Names'], 'control-manager', (name,), 'name') != [name] or
                    props['LoadState'] != 'masked' or props.get('UnitFileState') != 'masked-runtime' or
                    props.get('FragmentPath') not in (path, '/dev/null') or 'DropInPaths' not in props or
                    _array(props['DropInPaths'], 'control-manager', (name,), 'path')):
                _fail('control-manager', units=(name,))
            if require_quiet:
                _quiet(name, props)
                target = control_origin['terminal']
                target_shown = _show([target], deadline)
                if set(target_shown) != {target} or any(job[1] in (name, target) for job in _jobs(deadline)):
                    _fail('control-manager', units=(name,))
                _quiet(target, target_shown[target])
            _check_control_masks(deadline, check_unit_path=False)
            manager.append({'name': name, 'id': props['Id'], 'names': props['Names'],
                            'load_state': props['LoadState'], 'fragment_path': props['FragmentPath']})
        if protected is not None:
            shown = _show([UDEV_ALIAS], deadline)
            if set(shown) != {UDEV_ALIAS}:
                _fail('udev-alias-readback', units=(UDEV_ALIAS,))
            props = shown[UDEV_ALIAS]
            if (props['Id'] != UDEV_ALIAS or
                    _array(props['Names'], 'udev-alias-readback', (UDEV_ALIAS,), 'name') != [UDEV_ALIAS] or
                    props['LoadState'] != 'masked' or props.get('UnitFileState') != 'masked-runtime' or
                    props.get('FragmentPath') not in (path, '/dev/null') or
                    any(job[1] == UDEV_ALIAS for job in _jobs(deadline))):
                _fail('udev-alias-readback', units=(UDEV_ALIAS,))
            _quiet(UDEV_ALIAS, props)
        if name == terminal == NETPLAN and not aliases and path == NETPLAN_MASK:
            if _netplan_mask is not None:
                _check_netplan_mask(deadline)
            shown = _show([NETPLAN], deadline)
            if set(shown) != {NETPLAN}:
                _fail('netplan-manager', units=(NETPLAN,))
            props = shown[NETPLAN]
            if (props['Id'] != NETPLAN or
                    _array(props['Names'], 'netplan-manager', (NETPLAN,), 'name') != [NETPLAN] or
                    props['LoadState'] != 'masked' or props.get('UnitFileState') != 'masked' or
                    props.get('FragmentPath') not in (NETPLAN_MASK, '/dev/null') or
                    any(job[1] == NETPLAN for job in _jobs(deadline))):
                _fail('netplan-manager', units=(NETPLAN,))
            _quiet(NETPLAN, props)
            if _netplan_mask is not None:
                _check_netplan_mask(deadline)
            manager.append({'name': NETPLAN, 'id': props['Id'], 'names': props['Names'],
                            'load_state': props['LoadState'], 'fragment_path': props['FragmentPath']})
        if aliases:
            names = [os.path.basename(alias['link']['path']) for alias in aliases] + [terminal]
            for requested in names:
                shown = _show([requested], deadline)
                if set(shown) != {terminal}:
                    _fail('alias-manager', units=(name,))
                props = shown[terminal]
                decoded = _array(props['Names'], 'alias-manager', (name,), 'name')
                if (not set(names) <= set(decoded) or props['LoadState'] != 'masked' or
                        props.get('FragmentPath') not in (path, '/dev/null')):
                    _fail('alias-manager', units=(name,))
                manager.append({'name': requested, 'id': props['Id'], 'names': props['Names'],
                                'load_state': props['LoadState'], 'fragment_path': props['FragmentPath']})
        if (_route(name, deadline=deadline) != (terminal, aliases, mask) or _route_entry(path) != mask_entry or
                (vendor is not None and _route_entry(vendor['path']) != vendor) or
                (protected is not None and _route_entry(UDEV_VENDOR) != protected)):
            _fail('alias-route', units=(name,))
    result = {'name': name, 'state': state, 'terminal': terminal, 'mask': mask_entry,
              'aliases': aliases, 'manager': manager}
    if control_origin is not None:
        result['control_origin'] = control_origin
    return result


def _empty_cgroup(path, name):
    if not path:
        return
    if not path.startswith('/') or '..' in path.split('/') or '\x00' in path:
        _fail('cgroup', units=(name,))
    root = '/sys/fs/cgroup' + path
    if not os.path.isdir(root) or os.path.islink(root):
        _fail('cgroup', units=(name,))
    current = '/sys/fs/cgroup'
    for part in path.strip('/').split('/'):
        if part:
            current = os.path.join(current, part)
            if os.path.islink(current):
                _fail('cgroup', units=(name,))
    try:
        with open(root + '/cgroup.events', 'r', encoding='ascii') as stream:
            events = dict(line.split() for line in stream.read(4097).splitlines())
        if events.get('populated') != '0':
            _fail('cgroup', units=(name,))
        for base, dirs, _ in os.walk(root, followlinks=False):
            if any(os.path.islink(os.path.join(base, item)) for item in dirs):
                _fail('cgroup', units=(name,))
            with open(os.path.join(base, 'cgroup.procs'), 'r', encoding='ascii') as stream:
                if stream.read(4097).strip():
                    _fail('cgroup', units=(name,))
    except (OSError, ValueError, UnicodeError) as exc:
        raise _Failure('cgroup', 'predicate', (name,)) from exc


def _quiet(name, props):
    if props['ActiveState'] not in ('inactive', 'failed'):
        _fail('unit-state', units=(name,))
    if props.get('MainPID', '0') != '0' or props.get('ControlPID', '0') != '0':
        _fail('unit-process', units=(name,))
    _empty_cgroup(props.get('ControlGroup', ''), name)


def _safe_fragment(path):
    if path not in ('/usr/lib/systemd/system/' + TMPFILES, '/lib/systemd/system/' + TMPFILES):
        _fail('tmpfiles-fragment', units=(TMPFILES,))
    resolved = os.path.realpath(path)
    if resolved != '/usr/lib/systemd/system/' + TMPFILES:
        _fail('tmpfiles-fragment', units=(TMPFILES,))
    root = os.lstat('/')
    if not stat.S_ISDIR(root.st_mode) or root.st_uid != 0 or root.st_gid != 0 or root.st_mode & 0o022:
        _fail('tmpfiles-fragment', units=(TMPFILES,))
    if path.startswith('/lib/'):
        link = os.lstat('/lib')
        if not stat.S_ISLNK(link.st_mode) or link.st_uid != 0 or link.st_gid != 0:
            _fail('tmpfiles-fragment', units=(TMPFILES,))
    current = '/'
    for component in resolved.strip('/').split('/')[:-1]:
        current = os.path.join(current, component)
        info = os.lstat(current)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
            _fail('tmpfiles-fragment', units=(TMPFILES,))
    fd = os.open(resolved, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0 or
                before.st_mode & 0o022 or before.st_nlink != 1 or before.st_size > LIMIT):
            _fail('tmpfiles-fragment', units=(TMPFILES,))
        data = bytearray()
        while len(data) < before.st_size:
            block = os.read(fd, before.st_size - len(data))
            if not block:
                _fail('tmpfiles-fragment', units=(TMPFILES,))
            data.extend(block)
        after, named = os.fstat(fd), os.stat(path)
        identity = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        if identity(before) != identity(after) or identity(before) != identity(named):
            _fail('tmpfiles-fragment', units=(TMPFILES,))
        return identity(before), hashlib.sha256(data).hexdigest()
    finally:
        os.close(fd)


def _tmpfiles_properties(props, jobs, overridden):
    required = {'FragmentPath', 'DropInPaths', 'Type', 'RemainAfterExit', 'Restart', 'ExecStop',
                'ExecStopPost', 'PropagatesStopTo', 'StopPropagatedFrom', 'PartOf', 'BindsTo',
                'RequiredBy', 'RequisiteOf', 'BoundBy', 'ConsistsOf', 'ControlGroup'}
    if not (required - {'ExecStop', 'ExecStopPost'}) <= props.keys():
        _fail('tmpfiles-preflight', 'parse', (TMPFILES,))
    exact = {'Id': TMPFILES, 'LoadState': 'loaded', 'Type': 'oneshot', 'RemainAfterExit': 'yes', 'Restart': 'no',
             'RefuseManualStop': 'no' if overridden else 'yes', 'MainPID': '0', 'ControlPID': '0'}
    if any(props.get(key) != value for key, value in exact.items()):
        _fail('tmpfiles-preflight', units=(TMPFILES,))
    # systemctl's Exec array printer emits no line for an empty array, even
    # with --all. Only normalize the verified loaded service's two stop lists.
    props.setdefault('ExecStop', '')
    props.setdefault('ExecStopPost', '')
    for key in ('ExecStop', 'ExecStopPost'):
        if props.get(key, ''):
            _fail('tmpfiles-preflight', units=(TMPFILES,))
    for key in ('PropagatesStopTo', 'StopPropagatedFrom', 'PartOf', 'BindsTo'):
        if _array(props[key], 'tmpfiles-preflight', (TMPFILES,)):
            _fail('tmpfiles-preflight', units=(TMPFILES,))
    for key in ('RequiredBy', 'RequisiteOf', 'BoundBy', 'ConsistsOf'):
        if set(_array(props[key], 'tmpfiles-preflight', (TMPFILES,))) & ALLOWED:
            _fail('tmpfiles-preflight', units=(TMPFILES,))
    expected = [OVERRIDE] if overridden else []
    if _array(props['DropInPaths'], 'tmpfiles-preflight', (TMPFILES,), 'path') != expected or any(job[1] == TMPFILES for job in jobs):
        _fail('tmpfiles-preflight', units=(TMPFILES,))
    _empty_cgroup(props.get('ControlGroup', ''), TMPFILES)
    fragment = _safe_fragment(props['FragmentPath'])
    semantics = tuple((key, props[key]) for key in sorted(required - {'DropInPaths', 'ControlGroup'}))
    return fragment[0], fragment[1], semantics


def _create_override():
    global _override_identity
    # A pre-existing directory, file, or competing drop-in is never adopted.
    for path in ('/run', '/run/systemd', '/run/systemd/system'):
        info = os.lstat(path)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                info.st_mode & 0o022):
            _fail('override-preflight', units=(TMPFILES,))
    higher = '/etc/systemd/system/' + TMPFILES + '.d'
    if os.path.lexists(higher):
        info = os.lstat(higher)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                info.st_mode & 0o022 or any(name.endswith('.conf') and name >= '90-g2-manual-stop.conf'
                                            for name in os.listdir(higher))):
            _fail('override-preflight', units=(TMPFILES,))
    parent = os.open('/run/systemd/system', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.mkdir(TMPFILES + '.d', 0o700, dir_fd=parent)
        directory = os.open(TMPFILES + '.d', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=parent)
        try:
            os.fchmod(directory, 0o700)
            fd = os.open('90-g2-manual-stop.conf', os.O_WRONLY | os.O_CREAT | os.O_EXCL |
                         os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
            try:
                os.fchmod(fd, 0o600)
                if os.write(fd, OVERRIDE_BYTES) != len(OVERRIDE_BYTES):
                    raise OSError('short override write')
                file_info = os.fstat(fd)
                os.fsync(fd)
            finally:
                os.close(fd)
            dir_info = os.fstat(directory)
            _override_identity = ((dir_info.st_dev, dir_info.st_ino), (file_info.st_dev, file_info.st_ino))
            os.fsync(directory)
        finally:
            os.close(directory)
        os.fsync(parent)
    finally:
        os.close(parent)


def _override_file_check():
    info = os.lstat(OVERRIDE_DIR)
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
            stat.S_IMODE(info.st_mode) != 0o700 or os.listdir(OVERRIDE_DIR) != ['90-g2-manual-stop.conf'] or
            (info.st_dev, info.st_ino) != _override_identity[0]):
        _fail('override-readback', units=(TMPFILES,))
    fd = os.open(OVERRIDE, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        data = os.read(fd, len(OVERRIDE_BYTES) + 1)
        named = os.stat(OVERRIDE, follow_symlinks=False)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or data != OVERRIDE_BYTES or
                (info.st_dev, info.st_ino) != (named.st_dev, named.st_ino) or
                (info.st_dev, info.st_ino) != _override_identity[1]):
            _fail('override-readback', units=(TMPFILES,))
    finally:
        os.close(fd)


def _override_check(deadline):
    if _override_evidence is None:
        if os.path.lexists(OVERRIDE_DIR):
            _fail('override-readback', units=(TMPFILES,))
        return None
    _override_file_check()
    # The mask replaces the effective fragment on daemon-reload.  Check the
    # retained vendor file by its original path, not the masked FragmentPath.
    if _override_transition is None or _safe_fragment(_override_transition['preflight']['FragmentPath']) != _override_evidence[:2]:
        _fail('override-readback', units=(TMPFILES,))
    masked = _masked_tmpfiles(deadline)
    if masked['mask'] != _override_transition['masked_before_stop']['mask']:
        _fail('override-readback', units=(TMPFILES,))
    _quiet(TMPFILES, masked['properties'])
    return hashlib.sha256(canonical(_override_transition)).hexdigest()


def _masked_tmpfiles(deadline):
    """Read the effective mask without treating its properties as vendor ones."""
    root, dev, ino = _mask_path(TMPFILES)
    props = _show([TMPFILES], deadline)[TMPFILES]
    fragment = props.get('FragmentPath')
    if (props['Id'] != TMPFILES or props['LoadState'] != 'masked' or
            fragment not in (os.path.join(root, TMPFILES), '/dev/null') or
            _array(props.get('DropInPaths', ''), 'masked-tmpfiles', (TMPFILES,), 'path') not in ([], [OVERRIDE]) or
            any(job[1] == TMPFILES for job in _jobs(deadline))):
        _fail('masked-tmpfiles', units=(TMPFILES,))
    # If systemd exposes stop or propagation settings after masking, reject
    # dangerous values rather than assuming the original fragment still applies.
    for key in ('ExecStop', 'ExecStopPost'):
        if props.get(key, ''):
            _fail('masked-tmpfiles', units=(TMPFILES,))
    for key in ('PropagatesStopTo', 'StopPropagatedFrom', 'PartOf', 'BindsTo'):
        if _array(props.get(key, ''), 'masked-tmpfiles', (TMPFILES,)):
            _fail('masked-tmpfiles', units=(TMPFILES,))
    for key in ('RequiredBy', 'RequisiteOf', 'BoundBy', 'ConsistsOf'):
        if set(_array(props.get(key, ''), 'masked-tmpfiles', (TMPFILES,))) & ALLOWED:
            _fail('masked-tmpfiles', units=(TMPFILES,))
    return {'mask': {'path': os.path.join(root, TMPFILES), 'dev': dev, 'ino': ino},
            'properties': props}


def _inventory(deadline, require_closed):
    files, loaded, jobs = _files(deadline), _loaded(deadline), _jobs(deadline)
    if any(_template(name) for name in loaded):
        _fail('units', 'parse')
    props = _show(loaded, deadline)
    active = {}
    for name, value in props.items():
        if value['ActiveState'] not in ('active', 'inactive', 'failed', 'activating', 'reloading', 'deactivating'):
            _fail('unit-state', units=(name,))
        if value['RefuseManualStop'] not in ('yes', 'no'):
            _fail('unit-stop-policy', units=(name,))
        if name not in ALLOWED and set(_array(value['Names'], 'allowed-alias', (name,), 'name')) & ALLOWED:
            _fail('allowed-alias', units=(name,))
        if name in ALLOWED:
            if value['LoadState'] != 'loaded':
                _fail('allowed-unit', units=(name,))
            if value['ActiveState'] in ('active', 'activating', 'reloading'):
                active[name] = (value['LoadState'], value['ActiveState'], value['SubState'])
        elif require_closed:
            _quiet(name, value)
    if require_closed:
        protected = (_sealed_routes or {}).get(UDEV_ALIAS, {}).get('protected_alias')
        if protected is not None and (UDEV_CANONICAL not in props or
                                      props[UDEV_CANONICAL]['LoadState'] != 'loaded'):
            _fail('udev-census', units=(UDEV_CANONICAL,))
        forbidden_jobs = [job[1] for job in jobs if job[1] not in ALLOWED]
        if forbidden_jobs:
            _fail('pending-jobs', units=forbidden_jobs)
        masks = []
        for name, state in sorted(files.items()):
            if name not in ALLOWED:
                masks.append(_closed_route(name, state, files, deadline))
        for name, value in props.items():
            for alias in _array(value['Names'], 'alias-mask', (name,), 'name'):
                if alias not in ALLOWED and alias not in files:
                    _fail('alias-mask', units=(alias,))
        return files, props, jobs, active, masks
    return files, props, jobs, active, None


def _run_identity(info):
    return {key: getattr(info, 'st_' + key) for key in _RUN_IDENTITY_FIELDS}


def _stop_route_entries(route):
    entries = [item for item in (route.get('mask'), route.get('vendor_mask'),
                                 route.get('protected_alias')) if item is not None]
    for alias in route['aliases']:
        entries.append(alias['link'])
        entries.extend(alias['source'])
    origin = route.get('control_origin')
    if origin is not None:
        for alias in origin['aliases']:
            entries.append(alias['link'])
            entries.extend(alias['source'])
    return entries


def _commit_run_stop_transition(proof, deadline):
    """Install only the ctime proved by the currently armed stop observer."""
    global _stop_transition_committed
    if _stop_observer is None or _stop_transition_committed or time.monotonic() >= deadline:
        _fail('multipath-transition', units=(MULTIPATH,))
    _stop_observer.require_proof(proof)
    before, after = dict(proof.before), dict(proof.after)
    if (set(before) != set(_RUN_IDENTITY_FIELDS) or set(after) != set(_RUN_IDENTITY_FIELDS) or
            any(before[key] != after[key] for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink')) or
            _run_identity(os.stat('/run', follow_symlinks=False)) != after):
        _fail('multipath-transition', units=(MULTIPATH,))
    updates, seen = [], set()
    for name, route in sorted((_sealed_routes or {}).items()):
        for entry in _stop_route_entries(route):
            replacement = dict(entry)
            replacement['parents'] = []
            for parent in entry['parents']:
                new = dict(parent)
                if parent['path'] == '/run':
                    if any(parent[key] != before[key] for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns')):
                        _fail('multipath-transition', units=(name,))
                    new['ctime_ns'] = after['ctime_ns']
                    if id(parent) not in seen:
                        updates.append((parent, after['ctime_ns']))
                        seen.add(id(parent))
                replacement['parents'].append(new)
            with _held_route([replacement], name):
                pass
    for name, control in sorted(_control_masks.items()):
        route = (_sealed_routes or {}).get(name)
        if route is None or route.get('control_origin') is not control['origin']:
            _fail('multipath-transition', units=(name,))
    chains = []
    if _netplan_mask is not None:
        chains.extend(((_netplan_mask, 'etc'), (_netplan_mask, 'runtime')))
    if _control_directory is not None:
        chains.append((_control_directory, 'parents'))
    staged_chains = []
    for owner, key in chains:
        old = owner[key]
        for path, fd, parent, part, original in old:
            if path == '/run':
                held = os.fstat(fd)
                named = os.stat(part, dir_fd=parent, follow_symlinks=False)
                if (_run_identity(original) != before or _run_identity(held) != after or
                        _run_identity(named) != after):
                    _fail('multipath-transition', units=(MULTIPATH,))
        staged_chains.append((owner, key, _netplan_check_parents(old, ('/run',))))
    for parent, ctime in updates:
        parent['ctime_ns'] = ctime
    for owner, key, chain in staged_chains:
        owner[key] = chain
    _stop_transition_committed = True
    if _netplan_mask is not None:
        _check_netplan_mask(deadline)
    _check_control_masks(deadline)
    for name, route in sorted((_sealed_routes or {}).items()):
        with _held_route(_stop_route_entries(route), name):
            pass


def _multipath_props(deadline, raw_shows=None):
    names = (MULTIPATH, MULTIPATH_SOCKET)
    shown = {}
    for name in names:
        values = _show([name], deadline)
        if len(values) != 1:
            _fail('multipath-preflight', units=(name,))
        shown[name] = next(iter(values.values()))
        if raw_shows is not None:
            raw_shows[name] = _last_output[1]
    if any(job[1] in set(names) | {props['Id'] for props in shown.values()} for job in _jobs(deadline)):
        _fail('multipath-preflight', units=names)
    return shown


def _multipath_show(deadline):
    names = (MULTIPATH, MULTIPATH_SOCKET)
    raw_shows = {}
    shown = _multipath_props(deadline, raw_shows)
    files = _files(deadline)
    for name in names:
        try:
            if name in ALLOWED or name not in files:
                _fail('multipath-preflight', units=(name,))
            route = _closed_route(name, files[name], files, deadline, require_quiet=False)
            props = shown[name]
            if (props['LoadState'] != 'masked' or
                    props.get('FragmentPath') not in (route['mask']['path'], '/dev/null') or
                    props.get('ControlPID', '0') != '0'):
                _fail('multipath-preflight', units=(name,))
            if (name == MULTIPATH and
                    (route['terminal'] != name or route['aliases'] or props['Id'] != name or
                     _array(props['Names'], 'multipath-preflight', (name,), 'name') != [name] or
                     props.get('UnitFileState') != files[name] or props['RefuseManualStop'] != 'no' or
                     props.get('ControlPID') != '0' or
                     not {'MainPID', 'ControlGroup', 'DropInPaths'} <= props.keys() or
                     _array(props['DropInPaths'], 'multipath-preflight', (name,), 'path'))):
                _fail('multipath-preflight', units=(name,))
        except _Failure as exc:
            if exc.outcome == 'predicate':
                # Preserve the original failure and its output. The raw show
                # contains only the fixed property allowlist and stays private.
                exc.stderr += b'\n[fixed systemctl show]\n' + raw_shows[name]
            raise
    return shown


def _stop_multipath(deadline):
    global _stop_preflight, _stop_transition, _stop_observer
    from origins import _identity
    from run_stop_observer import RunStopObserver

    if _stop_preflight is not None or _stop_observer is not None:
        _fail('multipath-reentry', units=(MULTIPATH,))
    run_fd = os.open('/run', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    pid_fd = actor_fd = observer = None
    try:
        run = os.fstat(run_fd)
        before = _run_identity(run)
        if (not stat.S_ISDIR(run.st_mode) or run.st_uid != 0 or run.st_gid != 0 or
                run.st_mode & 0o022 or _run_identity(os.stat('/run', follow_symlinks=False)) != before):
            _fail('multipath-preflight', units=(MULTIPATH,))
        shown = _multipath_show(deadline)
        service, socket = shown[MULTIPATH], shown[MULTIPATH_SOCKET]
        try:
            pid_fd = os.open(MULTIPATH_PID, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=run_fd)
        except FileNotFoundError:
            pass
        if service['ActiveState'] in ('inactive', 'failed') and pid_fd is None:
            _quiet(MULTIPATH, service)
            _quiet(MULTIPATH_SOCKET, socket)
            if (_run_identity(os.fstat(run_fd)) != before or
                    _run_identity(os.stat('/run', follow_symlinks=False)) != before):
                _fail('multipath-preflight', units=(MULTIPATH,))
            _stop_preflight = {'unit': MULTIPATH, 'disposition': 'already_quiet',
                               'run_identity': before, 'pid_file_present': False}
            return {MULTIPATH, MULTIPATH_SOCKET, socket['Id']}
        if (service['ActiveState'] != 'active' or pid_fd is None or
                not service.get('MainPID', '').isdecimal() or int(service['MainPID']) <= 1):
            _fail('multipath-preflight', units=(MULTIPATH,))
        _command('multipath-socket-stop', ['stop', MULTIPATH_SOCKET], deadline)
        shown = _multipath_show(deadline)
        _quiet(MULTIPATH_SOCKET, shown[MULTIPATH_SOCKET])
        if (shown[MULTIPATH]['ActiveState'] != 'active' or
                shown[MULTIPATH]['MainPID'] != service['MainPID'] or
                shown[MULTIPATH].get('ControlGroup') != service.get('ControlGroup') or
                _run_identity(os.fstat(run_fd)) != before or
                _run_identity(os.stat('/run', follow_symlinks=False)) != before):
            _fail('multipath-preflight', units=(MULTIPATH,))
        item = os.fstat(pid_fd)
        named = os.stat(MULTIPATH_PID, dir_fd=run_fd, follow_symlinks=False)
        data = os.pread(pid_fd, 33, 0)
        if (not stat.S_ISREG(item.st_mode) or stat.S_IMODE(item.st_mode) != 0o644 or
                (item.st_uid, item.st_gid, item.st_nlink) != (0, 0, 1) or not 1 <= item.st_size <= 32 or
                _netplan_identity(item) != _netplan_identity(named) or
                _netplan_identity(item) != _netplan_identity(os.fstat(pid_fd)) or
                len(data) != item.st_size or re.fullmatch(rb'[0-9]+\n?', data) is None or
                int(data) != int(service['MainPID'])):
            _fail('multipath-pid-file', units=(MULTIPATH,))
        pid = int(service['MainPID'])
        actor_fd = os.pidfd_open(pid, 0)
        actor = _identity(pid, full=False)
        group = service.get('ControlGroup', '')
        if (actor is None or not group.startswith('/') or group.split('/')[-1] != MULTIPATH or
                actor['cgroup'] != '0::' + group or select.select([actor_fd], [], [], 0)[0]):
            _fail('multipath-actor', units=(MULTIPATH,))
        observer = RunStopObserver(run_fd, pid_fd, actor, deadline)
        _stop_observer = observer
        observer.arm()
        _command('multipath-stop', ['stop', MULTIPATH], deadline)
        proof = observer.finish()
        shown = _multipath_props(deadline)
        _quiet(MULTIPATH, shown[MULTIPATH])
        _quiet(MULTIPATH_SOCKET, shown[MULTIPATH_SOCKET])
        file_proof = dict(proof.pid_file)
        if (dict(proof.before) != before or
                any(file_proof[key] != getattr(item, 'st_' + key) for key in
                    ('dev', 'ino', 'mode', 'uid', 'gid', 'size', 'mtime_ns')) or
                file_proof['ctime_before_ns'] != item.st_ctime_ns or
                file_proof['sha256'] != hashlib.sha256(data).hexdigest()):
            _fail('multipath-transition', units=(MULTIPATH,))
        _commit_run_stop_transition(proof, deadline)
        _multipath_show(deadline)
        observer.close()
        observer = None
        _stop_observer = None
        _stop_preflight = {'unit': MULTIPATH, 'disposition': 'stop_required',
                           'run_identity': before, 'pid_file_present': True}
        settlement = dict(proof.settlement)
        settlement.update(stop_returncode=0, remaining_jobs=0, cgroup_empty=True)
        _stop_transition = {'version': 1, 'unit': MULTIPATH, 'entry': MULTIPATH_PID,
                            'actor': dict(proof.actor), 'before': dict(proof.before),
                            'after': dict(proof.after), 'pid_file': dict(proof.pid_file),
                            'event': dict(proof.event), 'settlement': settlement}
        return {MULTIPATH, MULTIPATH_SOCKET, socket['Id']}
    finally:
        failed = sys.exc_info()[0] is not None
        try:
            if observer is not None:
                try:
                    observer.close()
                except Exception:
                    if not failed:
                        raise
        finally:
            _stop_observer = None
            for fd in (actor_fd, pid_fd, run_fd):
                if fd is not None:
                    os.close(fd)


def _observe(deadline):
    if _stop_preflight is None:
        _fail('multipath-preflight', units=(MULTIPATH,))
    _check_control_masks(deadline)
    first = _inventory(deadline, True)
    override = _override_check(deadline)
    second = _inventory(deadline, True)
    _check_control_masks(deadline)
    if first[3:] != second[3:] or first[2] != second[2] or override != _override_check(deadline):
        _fail('closure-stability')
    files, _, _, active, masks = second
    state = {'allowed': sorted(ALLOWED), 'files': masks,
             'active': sorted((name,) + value for name, value in active.items()),
             'override_digest': override, 'unit_path': _unit_path,
             'stop_preflight': _stop_preflight, 'stop_transition': _stop_transition}
    return {'unit_count': len(files), 'active_units': sorted(active), 'closure': state,
            'override_digest': override, 'override_transition': _override_transition,
            'digest': hashlib.sha256(canonical(state)).hexdigest()}


def seal():
    """Mask routes before stopping units; persist the first failure privately."""
    global _override_evidence, _override_transition, _inventory_bytes, _seal_started, _sealed_routes
    global _run_diag_active
    _inventory_bytes = 0
    deadline = time.monotonic() + 120
    try:
        if _seal_started:
            _fail('seal-reentry')
        _seal_started = True
        if os.geteuid() != 0 or os.getegid() != 0:
            _fail('identity')
        files, props, jobs, _, _ = _inventory(deadline, False)
        override_needed = False
        for name, value in props.items():
            if name not in ALLOWED and value['RefuseManualStop'] == 'yes':
                if (name != TMPFILES or value['LoadState'] != 'loaded' or
                        value['ActiveState'] != 'active' or value['SubState'] != 'exited'):
                    _fail('refuse-manual-stop', units=(name,))
                _override_evidence = _tmpfiles_properties(value, jobs, False)
                preflight = dict(value)
                override_needed = True
        if override_needed:
            _create_override()
            _command('override-reload', ['daemon-reload'], deadline)
            overridden = _show([TMPFILES], deadline)[TMPFILES]
            if _tmpfiles_properties(overridden, _jobs(deadline), True) != _override_evidence:
                _fail('override-readback', units=(TMPFILES,))
            _override_file_check()
            _override_transition = {
                'preflight': preflight,
                'overridden': dict(overridden),
                'fragment_identity': dict(zip(('dev', 'ino', 'size', 'mtime_ns', 'ctime_ns'), _override_evidence[0])),
                'fragment_sha256': _override_evidence[1],
                'override_directory_identity': list(_override_identity[0]),
                'override_file_identity': list(_override_identity[1]),
                'override_sha256': hashlib.sha256(OVERRIDE_BYTES).hexdigest(),
            }
        names = set(files)
        for value in props.values():
            names.update(_array(value['Names'], 'alias-mask', (value['Id'],), 'name'))
        forbidden = names - ALLOWED
        _prepare_netplan_mask(forbidden, deadline)
        _sealed_routes = _install_masks(forbidden, deadline)
        if _netplan_mask is not None:
            _netplan_mask['runtime'] = _netplan_check_parents(
                _netplan_mask['runtime'], (os.path.dirname(NETPLAN_RUNTIME),))
            _check_netplan_mask(deadline)
        if _manager_unit_path(deadline) != _unit_path:
            _fail('unit-path')
        _command('mask-reload', ['daemon-reload'], deadline)
        if _manager_unit_path(deadline) != _unit_path:
            _fail('unit-path')
        if _netplan_mask is not None:
            _check_netplan_mask(deadline, after_reload=True)
        _recapture_routes(_sealed_routes)
        _run_diag_active = True
        _check_control_masks(deadline)
        masked = _files(deadline)
        if set(masked) != names:
            _fail('mask-inventory')
        for name in forbidden:
            _closed_route(name, masked[name], masked, deadline, require_quiet=False)
        if override_needed:
            before_stop = _masked_tmpfiles(deadline)
            value = before_stop['properties']
            if (value['ActiveState'] != 'active' or value['SubState'] != 'exited' or
                    value['RefuseManualStop'] != 'no' or value.get('MainPID') != '0' or
                    value.get('ControlPID') != '0'):
                _fail('masked-tmpfiles', units=(TMPFILES,))
            _empty_cgroup(value.get('ControlGroup', ''), TMPFILES)
            _override_file_check()
            if _safe_fragment(preflight['FragmentPath']) != _override_evidence[:2]:
                _fail('override-readback', units=(TMPFILES,))
            _override_transition['masked_before_stop'] = before_stop
        _run_diag_active = False
        settled_multipath = _stop_multipath(deadline)
        for batch in _batches(name for name, value in props.items()
                              if name not in ALLOWED and name not in settled_multipath and
                              value['ActiveState'] not in ('inactive', 'failed')):
            _run_diagnostic_record('before-stop', batch)
            try:
                _command('stop', ['stop'] + batch, deadline)
            finally:
                _run_diagnostic_record('after-stop', batch)
            after = _show(batch, deadline)
            for name in batch:
                _quiet(name, after[name])
            if any(job[1] in batch for job in _jobs(deadline)):
                _fail('stop-jobs', units=batch)
        if override_needed:
            after_stop = _masked_tmpfiles(deadline)
            if after_stop['mask'] != _override_transition['masked_before_stop']['mask']:
                _fail('masked-tmpfiles', units=(TMPFILES,))
            _quiet(TMPFILES, after_stop['properties'])
            _override_transition['masked_after_stop'] = after_stop
        return _observe(deadline)
    except _Failure as exc:
        _record_failure(exc)
        _record_mask_diagnostic(exc, deadline)
        raise
    except Exception as exc:
        failure = _Failure('systemd-seal', 'predicate')
        _record_failure(failure)
        raise failure from exc


def observe(deadline=None):
    """Recheck every loaded origin and effective mask without changing systemd."""
    global _inventory_bytes
    _inventory_bytes = 0
    end = time.monotonic() + 120
    return _observe(min(end, deadline) if deadline is not None else end)
