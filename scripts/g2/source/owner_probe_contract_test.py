"""Isolated real source custody and synthetic transport owner-probe tests."""
import copy
import errno
import json
import os
from pathlib import Path
import re
import shutil
import stat
import socket
import struct
import sys
import tempfile
from types import SimpleNamespace
import threading
import time
import unittest
from unittest import mock

R = Path(__file__).resolve().parent
sys.path[:0] = [str(R / 'source_supervisor'),
                str(R.parent / 'phase0')]
import guest_bootstrap as bootstrap
import guest_postgres
from journal import JournalError

SOURCE = bootstrap.OWNER_PROBE_SOURCE
MOVED = SOURCE + '.held'


def remove_owned(path):
    if os.path.islink(path) or (os.path.lexists(path) and not os.path.isdir(path)):
        os.unlink(path)
    elif os.path.isdir(path):
        os.chmod(path, 0o700)
        shutil.rmtree(path)


class OwnerProbeStartErrorTests(unittest.TestCase):
    def test_closed_projection(self):
        cases = (
            ('bpf_prog_query(BPF_CGROUP_DEVICE) failed: SECRET', 'DEVICE_QUERY'),
            ('cannot fetch program from id: SECRET', 'PROGRAM_FETCH'),
            ('could not get complete list of CGROUP_DEVICE programs', 'DEVICE_LIST'),
            ('failed to call BPF_PROG_ATTACH (BPF_CGROUP_DEVICE, BPF_F_ALLOW_MULTI): SECRET', 'DEVICE_ATTACH'),
            ('load program: SECRET', 'PROGRAM_LOAD'),
            ('error setting cgroup config for procHooks process: SECRET', 'CGROUP_CONFIG'),
            ('unable to apply cgroup configuration: SECRET', 'CGROUP_APPLY'),
            ('procReady not received', 'PROC_READY'),
            ('error during container init: SECRET', 'INIT_SYNC'),
            ('unable to start container process: SECRET', 'PROCESS_START'),
            ('failed to create task for container', 'TASK_CREATE'),
            ('', 'OTHER'), ('SECRET', 'OTHER'),
            ('failed to create task for container; load program: SECRET', 'PROGRAM_LOAD'),
            ('/secret/load program: in a quoted verifier log', 'PROGRAM_LOAD'),
        )
        for message, expected in cases:
            with self.subTest(expected=expected, message=message):
                actual = bootstrap._owner_probe_start_error(json.dumps({'message': message}).encode())
                self.assertEqual(actual, expected)
                self.assertNotIn('SECRET', actual)
        exact = b'{"message":"' + b'x' * (65536 - len(b'{"message":""}')) + b'"}'
        self.assertEqual(len(exact), 65536)
        self.assertEqual(bootstrap._owner_probe_start_error(exact), 'OTHER')
        self.assertEqual(bootstrap._owner_probe_start_error(exact + b' '), 'INVALID')
        for invalid in (b'', b'\xff', b'{"message":"a","message":"b"}',
                        b'{"message":NaN}', b'{"message":Infinity}', b'{"message":-Infinity}',
                        b'[]', b'{"message":null}', b'{"message":"x","extra":1}',
                        b'[' * 2000 + b'0' + b']' * 2000, 'not bytes', bytearray(b'{}')):
            with self.subTest(invalid=type(invalid)):
                self.assertEqual(bootstrap._owner_probe_start_error(invalid), 'INVALID')
        class BytesSubclass(bytes):
            pass
        class TextSubclass(str):
            pass
        self.assertEqual(bootstrap._owner_probe_start_error(BytesSubclass(b'{}')), 'INVALID')
        with mock.patch.object(bootstrap, 'strict_json', return_value={'message': TextSubclass('load program:')}):
            self.assertEqual(bootstrap._owner_probe_start_error(b'{}'), 'INVALID')

    def test_normalizer_and_linkage(self):
        owner = {'purpose': 'postgres-uid', 'response': 'RESPONSE_STATUS', 'http_status': 500,
                 'collection': 'EMPTY', 'wrapper': None, 'broker': None, 'runc_exec_observed': False,
                 'mount_options': None, 'create_history': None}
        labels = ('DEVICE_QUERY', 'PROGRAM_FETCH', 'DEVICE_LIST', 'DEVICE_ATTACH', 'PROGRAM_LOAD',
                  'CGROUP_CONFIG', 'CGROUP_APPLY', 'PROC_READY', 'INIT_SYNC', 'PROCESS_START',
                  'TASK_CREATE', 'OTHER', 'INVALID')
        exc = JournalError('SECRET')
        exc._g2_owner_probe_start_diagnostic = copy.deepcopy(owner)
        for label in labels:
            with self.subTest(label=label):
                exc._g2_owner_probe_start_error = label
                self.assertEqual(bootstrap._owner_probe_start_error_value(exc, owner), label)
                record = bootstrap._preflight_failure_diagnostic(exc)
                self.assertEqual(record['start_error'], label)
                self.assertEqual(record['owner_probe'], owner)
                self.assertNotIn('SECRET', json.dumps(record))
        for invalid_owner in (None, {}, dict(owner, response='OTHER'), dict(owner, http_status=400),
                              dict(owner, http_status=500.0), dict(owner, http_status=True)):
            self.assertIsNone(bootstrap._owner_probe_start_error_value(exc, invalid_owner))
        class Text(str):
            pass
        for invalid in (None, 'UNKNOWN', Text('OTHER'), 1, True):
            exc._g2_owner_probe_start_error = invalid
            self.assertIsNone(bootstrap._owner_probe_start_error_value(exc, owner))
        class BrokenAttribute(JournalError):
            def __getattribute__(self, name):
                if name == '_g2_owner_probe_start_error':
                    raise RuntimeError('SECRET')
                return super().__getattribute__(name)
        broken = BrokenAttribute('SECRET')
        broken._g2_owner_probe_start_diagnostic = copy.deepcopy(owner)
        result = bootstrap._preflight_failure_diagnostic(broken)
        self.assertEqual(result['owner_probe'], owner)
        self.assertIsNone(result['start_error'])
        self.assertEqual(result['exception']['kind'], 'JOURNAL')
        info_owner = PreflightFailureDiagnosticTests.owner()
        info_owner['response'], info_owner['http_status'] = 'RESPONSE_STATUS', 400
        exc._g2_owner_probe_start_diagnostic = info_owner
        exc._g2_docker_runtime_info_diagnostic = {'collection': 'OBSERVED', 'features': 'RRO_PRESENT', 'reason': 'OK'}
        with mock.patch.object(bootstrap, '_owner_probe_start_error_value', side_effect=RuntimeError('SECRET')):
            result = bootstrap._preflight_failure_diagnostic(exc)
        self.assertIsNotNone(result['owner_probe'])
        self.assertEqual(result['docker_runtime_info']['features'], 'RRO_PRESENT')
        self.assertIsNone(result['start_error'])


class PreflightFailureDiagnosticTests(unittest.TestCase):
    @staticmethod
    def owner():
        return {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                'collection': 'OBSERVED', 'wrapper': ('WRAPPER_ENTRY', None),
                'broker': ('BROKER_OCI_MOUNT_OPTIONS', None), 'runc_exec_observed': True,
                'mount_options': {'mount': 'PGDATA', 'actual': 'RBIND_RO_RPRIVATE',
                                  'expected': 'RBIND_RRO_RPRIVATE', 'difference': 'VALUE'}, 'create_history': None}

    def test_exception_tail_and_filter(self):
        names = ('source_guest.py', 'guest_storage.py', 'excluded_secret.py', 'guest_bootstrap.py',
                 'census.py', 'bpf_authority.py', 'guest_postgres.py', 'journal.py', 'volume_verify.py')
        namespace = {}
        for index, name in enumerate(names):
            body = ('    raise ValueError("secret message")' if index == len(names) - 1
                    else '    return f%d()' % (index + 1))
            exec(compile('def f%d():\n%s\n' % (index, body), '/secret/path/' + name, 'exec'), namespace)
        try:
            namespace['f0']()
        except ValueError as exc:
            record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['exception']['frames'], [
            ['guest_bootstrap.py', 2], ['census.py', 2], ['bpf_authority.py', 2],
            ['guest_postgres.py', 2], ['journal.py', 2], ['volume_verify.py', 2]])
        encoded = json.dumps(record)
        for secret in ('secret message', '/secret/path/', 'excluded_secret.py'):
            self.assertNotIn(secret, encoded)
        self.assertEqual(bootstrap._preflight_failure_diagnostic(ValueError())['exception']['frames'], [])
        namespace = {}
        exec(compile('def fail():\n    raise ValueError("secret")\n', '/secret/excluded.py', 'exec'), namespace)
        try:
            namespace['fail']()
        except ValueError as exc:
            self.assertEqual(bootstrap._preflight_failure_diagnostic(exc)['exception']['frames'], [])

    def test_traceback_node_limit(self):
        namespace = {}
        exec(compile('def recurse(n):\n    if n: return recurse(n - 1)\n    raise ValueError("secret")\n',
                     '/private/guest_bootstrap.py', 'exec'), namespace)
        def capture(n):
            try:
                namespace['recurse'](n)
            except ValueError as exc:
                return exc
        for n, expected_count in ((126, 128), (127, 129)):
            with self.subTest(nodes=expected_count):
                exc = capture(n)
                tb, count = exc.__traceback__, 0
                while tb is not None:
                    count += 1
                    tb = tb.tb_next
                self.assertEqual(count, expected_count)
                record = bootstrap._preflight_failure_diagnostic(exc)
                if expected_count == 128:
                    self.assertEqual(record['exception']['frames'],
                                     [['guest_bootstrap.py', 2]] * 5 + [['guest_bootstrap.py', 3]])
                else:
                    self.assertIsNone(record)

    def test_kind_errno_bounds(self):
        for exc, kind, number in ((JournalError('secret'), 'JOURNAL', None),
                                  (OSError(13, 'secret'), 'OSERROR', 13),
                                  (ValueError('secret'), 'OTHER', None)):
            try:
                raise exc
            except BaseException as caught:
                before_tb, before_attrs = caught.__traceback__, dict(caught.__dict__)
                diagnostic = bootstrap._preflight_failure_diagnostic(caught)
                self.assertEqual((diagnostic['exception']['kind'], diagnostic['exception']['errno']), (kind, number))
                self.assertIs(caught, exc)
                self.assertIs(caught.__traceback__, before_tb)
                self.assertEqual(caught.__dict__, before_attrs)
        for actual, expected in ((True, None), (0, None), (4096, None), (1, 1), (4095, 4095)):
            exc = OSError('secret')
            exc.errno = actual
            self.assertEqual(bootstrap._preflight_failure_diagnostic(exc)['exception']['errno'], expected)
            self.assertIs(exc.errno, actual)

    def test_owner_normalization_independence(self):
        exc = JournalError('secret')
        original = self.owner()
        exc._g2_owner_probe_start_diagnostic = original
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(set(record), {'version', 'exception', 'owner_probe', 'docker_runtime_info', 'start_error', 'settlement'})
        self.assertEqual(record['version'], 6)
        self.assertIsNone(record['start_error'])
        self.assertIsNone(record['docker_runtime_info'])
        owner = record['owner_probe']
        self.assertEqual(owner, dict(original, wrapper=['WRAPPER_ENTRY', None],
                                     broker=['BROKER_OCI_MOUNT_OPTIONS', None]))
        self.assertLessEqual(len(bootstrap.canonical(record)), 2048)
        self.assertIsNot(owner, original)
        self.assertIsNot(owner['mount_options'], original['mount_options'])
        owner['wrapper'][0] = 'changed'
        owner['mount_options']['mount'] = 'PROC'
        self.assertEqual(original['wrapper'], ('WRAPPER_ENTRY', None))
        self.assertEqual(original['mount_options']['mount'], 'PGDATA')
        original['mount_options']['actual'] = 'EMPTY'
        self.assertEqual(owner['mount_options']['actual'], 'RBIND_RO_RPRIVATE')
        self.assertIs(exc._g2_owner_probe_start_diagnostic, original)

    def test_owner_invalid_preserves_exception(self):
        class Text(str):
            pass
        valid = self.owner()
        invalid = []
        missing = copy.deepcopy(valid)
        del missing['mount_options']
        invalid.append(missing)
        invalid.extend((dict(valid, extra='secret'), dict(valid, http_status=True),
                        dict(valid, purpose=Text('postgres-uid')),
                        dict(valid, broker=('BROKER_OCI_MOUNT_OPTIONS', 13)),
                        dict(valid, broker=('BROKER_OCI_MOUNT_OPTIONS', 13), mount_options=None),
                        dict(valid, broker=('BROKER_OCI_VERSION', None)),
                        dict(valid, collection='EMPTY'), dict(valid, runc_exec_observed=1)))
        exc = JournalError('secret')
        for value in invalid:
            with self.subTest(value=value):
                exc._g2_owner_probe_start_diagnostic = value
                self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
                self.assertIsNone(bootstrap._preflight_failure_diagnostic(exc)['owner_probe'])
                self.assertIs(exc._g2_owner_probe_start_diagnostic, value)
        exc._g2_owner_probe_start_diagnostic = valid
        with mock.patch.object(bootstrap, '_owner_probe_diagnostic_value', side_effect=RuntimeError('secret')):
            record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['exception'], {'kind': 'JOURNAL', 'errno': None, 'frames': []})
        self.assertIsNone(record['owner_probe'])
        self.assertIs(exc._g2_owner_probe_start_diagnostic, valid)

    def test_construction_failure_omits(self):
        class BrokenTraceback(Exception):
            def __getattribute__(self, name):
                if name == '__traceback__':
                    raise RuntimeError('secret')
                return super().__getattribute__(name)
        exc = BrokenTraceback('secret')
        self.assertIsNone(bootstrap._preflight_failure_diagnostic(exc))
        self.assertEqual(exc.args, ('secret',))
        exc = JournalError('secret')
        with mock.patch.object(bootstrap, 'canonical', side_effect=ValueError('secret')):
            self.assertIsNone(bootstrap._preflight_failure_diagnostic(exc))
        with mock.patch.object(bootstrap, 'canonical', return_value=b'x' * 2048):
            self.assertIsNotNone(bootstrap._preflight_failure_diagnostic(exc))
        with mock.patch.object(bootstrap, 'canonical', return_value=b'x' * 2049):
            self.assertIsNone(bootstrap._preflight_failure_diagnostic(exc))
        self.assertEqual(exc.args, ('secret',))
        self.assertEqual(exc.__dict__, {})


class SourceFixture(unittest.TestCase):
    def setUp(self):
        self.assertEqual((os.geteuid(), os.getegid()), (0, 0))
        for mapping in ('uid_map', 'gid_map'):
            rows = Path('/proc/self', mapping).read_text().splitlines()
            self.assertEqual(len(rows), 1)
            inside, outside, count = (int(value) for value in rows[0].split())
            self.assertEqual((inside, count), (0, 1))
            self.assertNotEqual(outside, 0)
        mounts = [line.split(' - ', 1) for line in Path('/proc/self/mountinfo').read_text().splitlines()]
        run_mounts = [right.split()[0] for left, right in mounts if left.split()[4] == '/run']
        self.assertTrue(run_mounts)
        self.assertEqual(run_mounts[-1], 'tmpfs')
        parent = os.stat('/run/g2', follow_symlinks=False)
        self.assertEqual((parent.st_uid, parent.st_gid, stat.S_IMODE(parent.st_mode)), (0, 0, 0o700))
        self.assertTrue(stat.S_ISDIR(parent.st_mode))
        self.assertFalse(os.path.lexists(SOURCE))
        self.assertFalse(os.path.lexists(MOVED))
        self.addCleanup(remove_owned, MOVED)
        self.addCleanup(remove_owned, SOURCE)

    def source(self):
        source = bootstrap._OwnerProbeSource()
        self.addCleanup(source.close)
        return source


class OwnerProbeSourceTests(SourceFixture):
    def test_exclusive_identity_and_repeated_checks(self):
        source = self.source()
        info = os.fstat(source._leaf)
        self.assertEqual((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)), (0, 0, 0o500))
        self.assertEqual(os.listdir(SOURCE), [])
        source.check()
        source.check()
        fd = source._leaf
        source.close()
        source.close()
        with self.assertRaises(OSError) as caught:
            os.fstat(fd)
        self.assertEqual(caught.exception.errno, errno.EBADF)
        self.assertTrue(os.path.isdir(SOURCE))

    def test_existing_objects_are_not_adopted(self):
        for kind in ('directory', 'file', 'symlink'):
            with self.subTest(kind=kind):
                if kind == 'directory':
                    os.mkdir(SOURCE, 0o700)
                elif kind == 'file':
                    Path(SOURCE).write_bytes(b'owned')
                else:
                    os.symlink('/nonexistent-owner-target', SOURCE)
                before = os.lstat(SOURCE)
                with self.assertRaises((JournalError, OSError)):
                    bootstrap._OwnerProbeSource()
                after = os.lstat(SOURCE)
                self.assertEqual((before.st_dev, before.st_ino, before.st_mode, before.st_uid, before.st_gid),
                                 (after.st_dev, after.st_ino, after.st_mode, after.st_uid, after.st_gid))
                if kind == 'file':
                    self.assertEqual(Path(SOURCE).read_bytes(), b'owned')
                if kind == 'symlink':
                    self.assertEqual(os.readlink(SOURCE), '/nonexistent-owner-target')
                remove_owned(SOURCE)

    def test_entry_then_empty_still_changes_baseline(self):
        source = self.source()
        source.check()
        os.chmod(SOURCE, 0o700)
        Path(SOURCE, 'entry').write_bytes(b'x')
        os.chmod(SOURCE, 0o500)
        with self.assertRaises(JournalError):
            source.check()
        os.chmod(SOURCE, 0o700)
        os.unlink(os.path.join(SOURCE, 'entry'))
        os.chmod(SOURCE, 0o500)
        with self.assertRaises(JournalError):
            source.check()

    def test_leaf_replacement_and_mode_change(self):
        for change in ('replace', 'symlink', 'mode'):
            with self.subTest(change=change):
                source = bootstrap._OwnerProbeSource()
                try:
                    if change == 'mode':
                        os.chmod(SOURCE, 0o700)
                    else:
                        os.rename(SOURCE, MOVED)
                        if change == 'replace':
                            os.mkdir(SOURCE, 0o500)
                        else:
                            os.symlink(MOVED, SOURCE)
                    with self.assertRaises((JournalError, OSError)):
                        source.check()
                finally:
                    source.close()
                    remove_owned(SOURCE)
                    remove_owned(MOVED)

    def test_parent_name_drift(self):
        source = self.source()
        moved = '/run/g2-owner-test-held'
        self.assertFalse(os.path.lexists(moved))
        os.rename('/run/g2', moved)
        try:
            os.mkdir('/run/g2', 0o700)
            with self.assertRaises((JournalError, OSError)):
                source.check()
            os.rmdir('/run/g2')
        finally:
            if not os.path.exists('/run/g2'):
                os.rename(moved, '/run/g2')

    def test_remove_failure_preserves_source_and_post_rmdir_fsync_does_not_recreate(self):
        source = self.source()
        with mock.patch.object(bootstrap.os, 'rmdir', side_effect=OSError(errno.EIO, 'rmdir')):
            with self.assertRaises(OSError):
                source.remove()
        self.assertTrue(os.path.isdir(SOURCE))
        with mock.patch.object(bootstrap.os, 'fsync', side_effect=OSError(errno.EIO, 'fsync')):
            with self.assertRaises(OSError) as caught:
                source.remove()
        self.assertEqual(caught.exception.errno, errno.EIO)
        self.assertTrue(source._removed)
        self.assertFalse(os.path.lexists(SOURCE))
        with self.assertRaises(JournalError):
            source.check()

    def test_partial_construction_closes_descriptors_but_keeps_leaf(self):
        real_open = os.open
        owned = []
        def record_open(path, flags, *args, **kwargs):
            fd = real_open(path, flags, *args, **kwargs)
            if len(owned) < 2 and path in ('/run', 'g2'):
                owned.append(fd)
            return fd
        with mock.patch.object(bootstrap.os, 'open', side_effect=record_open), \
             mock.patch.object(bootstrap.os, 'fsync', side_effect=OSError(errno.EIO, 'fsync')):
            with self.assertRaises(OSError) as caught:
                bootstrap._OwnerProbeSource()
        self.assertEqual(caught.exception.errno, errno.EIO)
        self.assertTrue(os.path.isdir(SOURCE))
        self.assertEqual(len(owned), 2)
        for fd in owned:
            with self.assertRaises(OSError) as closed:
                os.fstat(fd)
            self.assertEqual(closed.exception.errno, errno.EBADF)


    def test_parent_mount_errno_preserved_and_launcher_default_unchanged(self):
        real_open = os.open
        real_mount = bootstrap._launch_failure_mount_id
        unavailable = {'status': 'unavailable', 'errno': errno.EMFILE}
        for failed_call in (1, 2):
            with self.subTest(constructor_parent=failed_call):
                owned = []
                calls = []
                def record_open(path, flags, *args, **kwargs):
                    fd = real_open(path, flags, *args, **kwargs)
                    if path in ('/run', 'g2'):
                        owned.append(fd)
                    return fd
                def observe_mount(fd):
                    calls.append(fd)
                    return unavailable if len(calls) == failed_call else real_mount(fd)
                with mock.patch.object(bootstrap.os, 'open', side_effect=record_open), \
                     mock.patch.object(bootstrap, '_launch_failure_mount_id', side_effect=observe_mount):
                    with self.assertRaises(OSError) as caught:
                        bootstrap._OwnerProbeSource()
                self.assertEqual(caught.exception.errno, errno.EMFILE)
                self.assertEqual(len(owned), failed_call)
                self.assertFalse(os.path.lexists(SOURCE))
                for fd in owned:
                    with self.assertRaises(OSError) as closed:
                        os.fstat(fd)
                    self.assertEqual(closed.exception.errno, errno.EBADF)

        source = self.source()
        original = os.stat(SOURCE)
        held_fds = (source._run, source._parent, source._leaf)
        for parent, named in (('run', False), ('run', True), ('g2', False), ('g2', True)):
            with self.subTest(recheck_parent=parent, named=named):
                opened = {}
                def record_named(path, flags, *args, **kwargs):
                    fd = real_open(path, flags, *args, **kwargs)
                    if path in ('/run', '/run/g2'):
                        opened[path] = fd
                    return fd
                def observe_mount(fd):
                    path = '/run' if parent == 'run' else '/run/g2'
                    target = opened.get(path) if named else (source._run if parent == 'run' else source._parent)
                    return unavailable if fd == target else real_mount(fd)
                with mock.patch.object(bootstrap.os, 'open', side_effect=record_named), \
                     mock.patch.object(bootstrap, '_launch_failure_mount_id', side_effect=observe_mount):
                    with self.assertRaises(OSError) as caught:
                        source.check()
                self.assertEqual(caught.exception.errno, errno.EMFILE)
                current = os.stat(SOURCE)
                self.assertEqual((current.st_dev, current.st_ino), (original.st_dev, original.st_ino))
                for fd in held_fds:
                    os.fstat(fd)
                for fd in set(opened.values()):
                    with self.assertRaises(OSError) as closed:
                        os.fstat(fd)
                    self.assertEqual(closed.exception.errno, errno.EBADF)

        with mock.patch.object(bootstrap, '_launch_failure_mount_id', return_value=unavailable):
            with self.assertRaises(JournalError):
                bootstrap._source_directory_identity(source._run)
            with self.assertRaises(JournalError):
                bootstrap._source_check_named_directory('/run', source._run, source._run_identity)
        source.check()
        source.close()
        for fd in held_fds:
            with self.assertRaises(OSError) as closed:
                os.fstat(fd)
            self.assertEqual(closed.exception.errno, errno.EBADF)


class FakeResponse:
    def __init__(self, value=None, status=200):
        self.status = status
        self.value = value
    def close(self):
        pass
    def read(self, limit):
        return b'' if self.value is None else json.dumps(self.value).encode('ascii')


class FakeSocket:
    def __init__(self, transport):
        self.transport = transport
        self.parts = [b'HTTP/1.1 101 UPGRADED\r\n\r\n', b'\x01\0\0\0' + struct.pack('!I', 4) + b'999\n', b'']
    def sendall(self, data):
        self.transport.events.append(('attach', self.transport.current))
    def settimeout(self, timeout):
        pass
    def recv(self, count):
        return self.parts.pop(0)


class ProbeTransport:
    def __init__(self, fail=None):
        self.fail = fail
        self.events = []
        self.ids = {}
        self.current = 0
        self.sources = []
        self.source_fd = None
        self.constructing = True
        self.now = 1000
        self.check_count = 0
        self.settlement_deadlines = []

    def live(self):
        if self.sources:
            if self.sources[0]._leaf != self.source_fd:
                raise AssertionError('source custody descriptor changed')
            os.fstat(self.source_fd)

    def connection(self, daemon, timeout):
        transport = self
        class Connection:
            def __init__(self):
                self.sock = FakeSocket(transport)
                self.active_sock = self.sock
                self.response = None
            def request(self, method, path, body=None, headers=None):
                transport.live()
                if path == '/v1.44/info':
                    if method != 'GET' or body is not None:
                        raise AssertionError((method, path, body))
                    transport.events.append(('info', transport.current))
                    self.response = FakeResponse({'DefaultRuntime': 'g2-bpf', 'Runtimes': {'g2-bpf': {
                        'path': '/opt/g2/g2-bpf-runtime', 'status': {
                            'org.opencontainers.runtime-spec.features': '{"mountOptions":["rro"]}'}}}})
                    return
                op = path.split('/containers/', 1)[1]
                if op == 'create':
                    config = json.loads(body)
                    transport.current += 1
                    cid = ('%064x' % transport.current)
                    transport.ids[cid] = config
                    transport.events.append(('create', transport.current))
                    self.response = FakeResponse({'Id': cid}, 500 if transport.fail == 'create-%d' % transport.current else 201)
                    return
                cid, _, suffix = op.partition('/')
                if '?' in cid:
                    cid, suffix = cid.split('?', 1)
                    suffix = '?' + suffix
                number = int(cid, 16)
                if suffix == 'json':
                    config = transport.ids[cid]
                    item = {'Id': cid, 'Image': bootstrap.PG_ID, 'HostConfig': {'Binds': None, 'Mounts': copy.deepcopy(config['HostConfig']['Mounts'])},
                            'Mounts': [{'Type': 'bind', 'Source': SOURCE, 'Destination': bootstrap.OWNER_PROBE_TARGET, 'RW': False, 'Propagation': 'rprivate', 'Name': ''}]}
                    if transport.fail == 'inspect':
                        item['Mounts'][0]['RW'] = True
                    transport.events.append(('inspect', number))
                    self.response = FakeResponse(item)
                elif suffix == 'start':
                    transport.events.append(('start', number))
                    self.response = FakeResponse(None, 500 if transport.fail == 'start-%d' % number else 204)
                elif suffix.startswith('wait'):
                    transport.events.append(('wait', number))
                    if transport.fail == 'wait-expired':
                        transport.now = 1100
                    self.response = FakeResponse({'StatusCode': 1 if transport.fail in ('wait', 'wait-expired') else 0})
                elif method == 'DELETE':
                    force = 'force=1' in suffix
                    transport.events.append(('force-delete' if force else 'delete', number))
                    self.response = FakeResponse(None, 500 if transport.fail == 'delete' and not force else 204)
                else:
                    raise AssertionError((method, path))
            def getresponse(self):
                return self.response
            def connect(self):
                transport.live()
            def close(self):
                pass
            def abort(self):
                transport.events.append(('abort', transport.current))
        return Connection()

    def admit_container(self, config, cid, purpose):
        self.live()
        number = int(cid, 16)
        self.events.append(('admit', purpose))
        if self.fail == 'admit-%d' % number:
            raise JournalError('admission failed')

    def settle_container(self, cid, outcome, deadline):
        self.live()
        number = int(cid, 16)
        self.events.append(('settle-' + outcome, number))
        self.settlement_deadlines.append((outcome, number, deadline))
        if outcome == 'completed' and self.fail in ('settle-exception', 'settle-exception-%d' % number):
            raise JournalError('settlement failed')
        if outcome == 'completed' and self.fail == 'settle-pending':
            return 'pending'
        return 'retired'


class OwnerProbeLifecycleTests(SourceFixture):
    def run_pair(self, fail=None, evidence=None):
        transport = ProbeTransport(fail)
        cls = bootstrap._OwnerProbeSource
        original_check = cls.check
        original_remove = cls.remove
        def make_source():
            source = cls()
            transport.constructing = False
            transport.sources.append(source)
            transport.source_fd = source._leaf
            return source
        def check(source):
            if transport.constructing:
                return original_check(source)
            transport.check_count += 1
            transport.now += 1
            transport.events.append(('check', transport.check_count))
            if fail in ('before-create', 'after-create', 'before-start', 'after-retired-2') and transport.check_count == {'before-create': 1, 'after-create': 2, 'before-start': 3, 'after-retired-2': 8}[fail]:
                os.chmod(SOURCE, 0o700)
            return original_check(source)
        def remove(source):
            transport.live()
            transport.events.append(('remove', 0))
            if fail == 'remove':
                with mock.patch.object(bootstrap.os, 'rmdir', side_effect=OSError(errno.EIO, 'rmdir')):
                    return original_remove(source)
            return original_remove(source)
        class Evidence:
            def __init__(inner, authority, purpose):
                inner.purpose = purpose
                inner.nonce = ('01' if purpose == 'postgres-uid' else '02') * 16
                inner.hard_deadline = 2000.0
                inner.guest = {'operation_start_ns': 0, 'operation_deadline_ns': 0,
                               'authority_deadline_ns': 0, 'ack_received_ns': 0}
            def bind(inner, cid, start, deadline):
                pass
            def collect(inner):
                return {'purpose': inner.purpose, 'nonce': inner.nonce, 'collection': 'EMPTY',
                        'guest': dict(inner.guest), 'snapshots': [], 'reference_holder': 'UNRESOLVED'}
            def close(inner):
                pass
        with mock.patch.object(bootstrap, '_OwnerProbeSource', side_effect=make_source), \
             mock.patch.object(cls, 'check', check), mock.patch.object(cls, 'remove', remove), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=transport.connection), \
             mock.patch.object(bootstrap, '_OwnerSettlementEvidenceChannel', Evidence):
            if fail is None:
                result = bootstrap._owner_probe_pair({}, transport, observe_owner_start=False, settlement_evidence=evidence)
                self.assertEqual(result, [999, 999])
            else:
                with self.assertRaises((JournalError, OSError)):
                    bootstrap._owner_probe_pair({}, transport, observe_owner_start=False, settlement_evidence=evidence)
        self.assertEqual(len(transport.sources), 1)
        self.assertIsNone(transport.sources[0]._leaf)
        with self.assertRaises(OSError) as closed:
            os.fstat(transport.source_fd)
        self.assertEqual(closed.exception.errno, errno.EBADF)
        return transport

    def test_two_retired_before_remove_and_original_deadlines(self):
        transport = self.run_pair()
        self.assertFalse(os.path.lexists(SOURCE))
        self.assertEqual([event for event in transport.events if event[0] == 'admit'], [('admit', 'postgres-uid'), ('admit', 'postgres-gid')])
        self.assertEqual(transport.settlement_deadlines, [('completed', 1, 1060), ('completed', 2, 1064)])
        self.assertLess(transport.events.index(('delete', 1)), transport.events.index(('settle-completed', 1)))
        self.assertLess(transport.events.index(('delete', 2)), transport.events.index(('settle-completed', 2)))
        self.assertLess(transport.events.index(('settle-completed', 2)), transport.events.index(('remove', 0)))

    def test_uid_evidence_survives_gid_failures(self):
        for failure in ('create-2', 'admit-2', 'start-2', 'settle-exception-2'):
            with self.subTest(failure=failure):
                evidence = []
                self.run_pair(failure, evidence=evidence)
                self.assertEqual([item['purpose'] for item in evidence], ['postgres-uid', 'postgres-gid'])
                self.assertTrue(all(item['collection'] == 'EMPTY' for item in evidence))
                remove_owned(SOURCE)

    def test_bind_can_be_processed_after_admit_without_an_ack(self):
        transport = ProbeTransport()
        source = self.source()
        transport.sources = [source]
        transport.source_fd = source._leaf
        transport.constructing = False
        broker, guest = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        self.addCleanup(broker.close)
        self.addCleanup(guest.close)
        broker.settimeout(1)
        guest.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        guest.setblocking(False)
        channel = bootstrap._OwnerSettlementEvidenceChannel.__new__(bootstrap._OwnerSettlementEvidenceChannel)
        channel.sock, channel.peer = guest, (os.getpid(), os.getuid(), os.getgid())
        channel.purpose, channel.nonce = 'postgres-uid', '03' * 16
        channel.boot, channel.epoch, channel.principal = '01' * 16, '02' * 16, 3
        channel.container_id, channel.hard_deadline = None, 0.0
        channel.armed, channel.invalid = True, False
        channel.guest = {'operation_start_ns': 0, 'operation_deadline_ns': 0,
                         'authority_deadline_ns': 0, 'ack_received_ns': 0}
        admitted, original_admit = [], transport.admit_container
        def admit(config, cid, purpose):
            original_admit(config, cid, purpose)
            admitted.append(json.loads(broker.recv(1024)))
        transport.admit_container = admit
        evidence = []
        with mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=transport.connection), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now):
            result = bootstrap._owner_probe({}, transport, '-u', source, observe_owner_start=False,
                                            settlement_evidence=evidence, evidence_channel=channel)
        self.assertEqual(result, 999)
        self.assertEqual(admitted[0]['container_id'], '%064x' % 1)
        self.assertEqual(admitted[0]['nonce'], channel.nonce)
        self.assertEqual(evidence[0]['collection'], 'EMPTY')
        self.assertIn(('settle-completed', 1), transport.events)

    def test_missing_listener_does_not_block_admission(self):
        transport = ProbeTransport()
        transport._peer = (os.getpid(), os.getuid(), os.getgid())
        transport.boot_id = '01010101-0101-0101-0101-010101010101'
        transport.epoch, transport.principal_id, transport._deadline = '02' * 16, 3, 2000.0
        source = self.source()
        transport.sources = [source]
        transport.source_fd = source._leaf
        transport.constructing = False
        evidence = []
        with mock.patch.object(bootstrap, '_OwnerProbeSource', return_value=source), \
             mock.patch.object(bootstrap, 'OWNER_SETTLEMENT_SOCKET', '/run/g2/missing-settlement-listener'), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=transport.connection), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now):
            self.assertEqual(bootstrap._owner_probe_pair({}, transport, observe_owner_start=False,
                                                        settlement_evidence=evidence), [999, 999])
        self.assertEqual([item['collection'] for item in evidence], ['UNAVAILABLE', 'UNAVAILABLE'])
        self.assertEqual([event for event in transport.events if event[0] == 'admit'],
                         [('admit', 'postgres-uid'), ('admit', 'postgres-gid')])

    def test_failure_retains_source_and_runs_original_cleanup(self):
        for failure in ('admit-1', 'admit-2', 'before-create', 'after-create', 'before-start', 'after-retired-2', 'wait', 'delete', 'settle-pending', 'settle-exception', 'inspect', 'remove'):
            with self.subTest(failure=failure):
                transport = self.run_pair(failure)
                self.assertTrue(os.path.isdir(SOURCE))
                if failure != 'remove':
                    self.assertNotIn(('remove', 0), transport.events)
                if failure == 'before-create':
                    self.assertFalse(any(event[0] == 'create' for event in transport.events))
                elif failure in ('admit-1', 'admit-2', 'after-create', 'before-start', 'wait', 'delete', 'inspect'):
                    self.assertTrue(any(event[0] == 'force-delete' for event in transport.events))
                if failure in ('admit-1', 'after-create', 'before-start', 'inspect'):
                    self.assertNotIn(('start', 1), transport.events)
                if failure == 'admit-2':
                    self.assertIn(('settle-completed', 1), transport.events)
                    self.assertNotIn(('start', 2), transport.events)
                if failure in ('settle-pending', 'settle-exception'):
                    self.assertIn(('delete', 1), transport.events)
                    self.assertNotIn(('force-delete', 1), transport.events)
                remove_owned(SOURCE)

    def test_expired_wait_preserves_cleanup_extension(self):
        transport = self.run_pair('wait-expired')
        self.assertTrue(os.path.isdir(SOURCE))
        self.assertIn(('force-delete', 1), transport.events)
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1110)])


class OwnerProbeDiagnosticTests(SourceFixture):
    def setUp(self):
        super().setUp()
        self.path = '/run/g2/owner-probe-diagnostic'
        self.assertFalse(os.path.lexists(self.path))
        self.addCleanup(remove_owned, self.path)
        self.cid = 'a' * 64

    def receiver(self):
        source = self.source()
        receiver = bootstrap._OwnerProbeStartDiagnostic(source, self.cid)
        self.addCleanup(receiver.close)
        return receiver

    def send(self, packet, ancillary=()):
        with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
            sender.sendmsg([packet], list(ancillary), 0, self.path + '/' + self.cid)

    @staticmethod
    def history_packet():
        packet = bytearray(64)
        packet[:8] = bytes((4, 1, 1, 1, 1, 1, 0, 0))
        struct.pack_into('<HBBHBB', packet, 40, 2, 5, 2, 28, 1, 2)
        return bytes(packet)

    def test_create_history_packets(self):
        receiver = self.receiver()
        packet = self.history_packet()
        for old in (bytes((2, 1, 0, 0, 0, 0, 0, 0)), bytes((2, 39, 0, 0, 0, 0, 0, 0)),
                    bytes((2, 64, 0, 0, 0, 0, 0, 0))):
            self.send(old)
        self.send(packet)
        collection, wrapper, broker, executed, detail, history = receiver.collect()
        self.assertEqual((collection, wrapper, broker, executed, detail),
                         ('OBSERVED', ('WRAPPER_ENTRY', None), ('BROKER_BUNDLE_COMPARE', None), True, None))
        self.assertEqual((history['purpose'], history['group'], history['target']), ('postgres-uid', 'LIVE', 'ABSENT'))
        self.assertEqual(history['commands'][4], {'command': 'PROG_QUERY', 'count': 2, 'state': 'RETIRED',
                          'result': 'NEGATIVE', 'errno': 28, 'policy_error': True, 'response': 'SEND_OK'})
        self.assertEqual(len(history['commands']), 7)
        all_slots = bytearray(packet)
        all_slots[6] = 1
        for index in range(7):
            struct.pack_into('<HBBHBB', all_slots, 8 + index * 8, index + 1, 5, 3, 0, 0, 2)
        self.send(all_slots)
        all_history = receiver.collect()[5]
        self.assertEqual([(item['command'], item['count']) for item in all_history['commands']],
                         [('MAP_CREATE', 1), ('PROG_LOAD', 2), ('OBJ_GET_INFO_BY_FD', 3),
                          ('PROG_GET_FD_BY_ID', 4), ('PROG_QUERY', 5), ('PROG_ATTACH', 6), ('PROG_DETACH', 7)])
        self.assertEqual(all_history['program_kind'], 'DEVICE')
        for kind, name in ((1, 'DEVICE'), (2, 'EXTENDED_INFO_PROBE'), (3, 'REPLACE_PROBE')):
            sample = bytearray(all_slots)
            sample[6] = kind
            self.assertEqual(bootstrap._owner_probe_create_history_packet(bytes(sample))['program_kind'], name)
        for code, name in ((1, 'UNRECORDED'), (2, 'SEND_OK'), (3, 'SEND_ENOENT'),
                           (4, 'ADDFD_OK'), (5, 'ADDFD_ENOENT')):
            sample = bytearray(64)
            sample[:8] = bytes((4, 1, 1, 0, 0, 0, 0, 0))
            state = 3 if code == 1 else 5
            struct.pack_into('<HBBHBB', sample, 8, 1, state, 3, 0, 0, code)
            self.assertEqual(bootstrap._owner_probe_create_history_packet(bytes(sample))['commands'][0]['response'], name)
        for state, result, response in ((1, 1, 2), (2, 1, 2), (3, 1, 1), (4, 3, 1),
                                         (5, 1, 2), (5, 3, 1)):
            sample = bytearray(packet)
            struct.pack_into('<HBBHBB', sample, 40, 1, state, result, 0, 0, response)
            self.assertIsNone(bootstrap._owner_probe_create_history_packet(bytes(sample)))
        sample = bytearray(packet)
        sample[47] = 4
        self.assertIsNone(bootstrap._owner_probe_create_history_packet(bytes(sample)))
        sample = bytearray(all_slots)
        sample[6] = 0
        self.assertIsNone(bootstrap._owner_probe_create_history_packet(bytes(sample)))
        sample = bytearray(packet)
        sample[6] = 1
        self.assertIsNone(bootstrap._owner_probe_create_history_packet(bytes(sample)))
        maximum = bytearray(packet)
        struct.pack_into('<HBBHBB', maximum, 40, 4096, 5, 2, 4095, 0, 2)
        self.send(maximum)
        self.assertEqual(receiver.collect()[5]['commands'][4]['count'], 4096)
        malformed = []
        for offset, value in ((2, 0), (3, 2), (4, 4), (5, 9), (47, 6), (46, 2), (42, 6), (43, 5)):
            bad = bytearray(packet)
            bad[offset] = value
            malformed.append(bytes(bad))
        for fields in ((1, 1, 2, 1, 0, 0), (1, 3, 1, 0, 0, 0),
                       (1, 5, 2, 0, 0, 0), (1, 5, 2, 4096, 0, 0), (4097, 5, 3, 0, 0, 0)):
            bad = bytearray(packet)
            struct.pack_into('<HBBHBB', bad, 40, *fields)
            malformed.append(bytes(bad))
        bad = bytearray(maximum)
        struct.pack_into('<HBBHBB', bad, 8, 1, 1, 1, 0, 0, 0)
        malformed.append(bytes(bad))
        for bad in malformed:
            self.send(bad)
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        self.send(packet)
        self.send(packet)
        self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        for bad in (packet[:-1], packet + b'\0', b'\x03\x01' + packet[2:], b'\x04\x02' + packet[2:],
                    packet[:7] + b'\x01' + packet[8:], packet[:14] + b'\x01' + packet[15:]):
            self.send(bad)
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        self.send(packet)
        self.assertEqual(receiver.collect()[0], 'OBSERVED')
        before = set(os.listdir('/proc/self/fd'))
        fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        try:
            self.send(packet, [(socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', fd))])
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
            os.fstat(fd)
            for old in (bytes((2, 1, 0, 0, 0, 0, 0, 0)), bytes((2, 39, 0, 0, 0, 0, 0, 0)),
                        bytes((2, 64, 0, 0, 0, 0, 0, 0)), packet):
                self.send(old)
            self.send(packet, [(socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', fd))])
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
            os.fstat(fd)
        finally:
            os.close(fd)
        self.assertEqual(set(os.listdir('/proc/self/fd')), before)

    def test_create_history_normalization(self):
        history = bootstrap._owner_probe_create_history_packet(self.history_packet())
        exc = JournalError('original')
        exc._g2_owner_probe_start_diagnostic = {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
            'collection': 'OBSERVED', 'wrapper': None, 'broker': None, 'runc_exec_observed': False,
            'mount_options': None, 'create_history': history}
        normalized = bootstrap._owner_probe_diagnostic_value(exc)
        self.assertEqual(normalized['create_history'], history)
        self.assertIsNot(normalized['create_history'], history)
        self.assertIsNot(normalized['create_history']['commands'], history['commands'])
        normalized['create_history']['commands'][4]['errno'] = 99
        self.assertEqual(history['commands'][4]['errno'], 28)
        invalid_slot = copy.deepcopy(history)
        invalid_slot['commands'][4]['count'] = True
        exc._g2_owner_probe_start_diagnostic['create_history'] = invalid_slot
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        invalid_slot = copy.deepcopy(history)
        invalid_slot['commands'][4]['errno'] = True
        exc._g2_owner_probe_start_diagnostic['create_history'] = invalid_slot
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        class DerivedString(str):
            pass
        class DerivedInt(int):
            pass
        for key, bad_value in (('response', DerivedString('SEND_OK')), ('count', DerivedInt(2))):
            invalid_slot = copy.deepcopy(history)
            invalid_slot['commands'][4][key] = bad_value
            exc._g2_owner_probe_start_diagnostic['create_history'] = invalid_slot
            self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        for key, bad_value in (('response', None), ('response', 'ADDFD_OK'),
                               ('response', 'SECRET'), ('count', False), ('state', 'UNKNOWN')):
            invalid_slot = copy.deepcopy(history)
            invalid_slot['commands'][4][key] = bad_value
            exc._g2_owner_probe_start_diagnostic['create_history'] = invalid_slot
            self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        invalid_slot = copy.deepcopy(history)
        invalid_slot['commands'][4]['response'] = 'UNRECORDED'
        exc._g2_owner_probe_start_diagnostic['create_history'] = invalid_slot
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        for bad in (dict(history, purpose='postgres-gid'), dict(history, extra='secret'),
                    dict(history, commands=list(reversed(history['commands']))),
                    dict(history, version=True), dict(history, version=1),
                    dict(history, program_kind='DEVICE'), dict(history, group='INVALID', target='ABSENT')):
            exc._g2_owner_probe_start_diagnostic['create_history'] = bad
            self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        exc._g2_owner_probe_start_diagnostic['create_history'] = history
        exc._g2_owner_probe_start_diagnostic['collection'] = 'EMPTY'
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        del exc._g2_owner_probe_start_diagnostic['create_history']
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))

    def test_create_history_attaches_after_cleanup(self):
        transport = ProbeTransport()
        escaped = OSError(errno.EIO, 'synthetic start failure')
        base_connection = transport.connection
        source_class = bootstrap._OwnerProbeSource
        def make_source():
            source = source_class()
            transport.sources.append(source)
            transport.source_fd = source._leaf
            return source
        def connection(daemon, timeout):
            inner = base_connection(daemon, timeout)
            original_request = inner.request
            def request(method, path, body=None, headers=None):
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    transport.live()
                    transport.events.append(('start', int(match.group(1), 16)))
                    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                        sender.sendto(self.history_packet(), self.path + '/' + match.group(1))
                    raise escaped
                return original_request(method, path, body=body, headers=headers)
            inner.request = request
            return inner
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        def collect(receiver):
            transport.live()
            transport.events.append(('collect', 1))
            return original_collect(receiver)
        with mock.patch.object(bootstrap, '_OwnerProbeSource', side_effect=make_source), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
            with self.assertRaises(OSError) as result:
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertIs(result.exception, escaped)
        value = escaped._g2_owner_probe_start_diagnostic
        self.assertEqual(value['create_history'], bootstrap._owner_probe_create_history_packet(self.history_packet()))
        self.assertEqual((value['response'], value['collection']), ('OTHER', 'OBSERVED'))
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        self.assertLess(transport.events.index(('force-delete', 1)), transport.events.index(('settle-failed', 1)))
        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
        with self.assertRaises(OSError) as closed:
            os.fstat(transport.source_fd)
        self.assertEqual(closed.exception.errno, errno.EBADF)

    def test_real_packets_credentials_and_serializer(self):
        receiver = self.receiver()
        self.assertEqual(receiver.collect(), ('EMPTY', None, None, False, None, None))
        self.send(bytes((2, 5, errno.EACCES, 0, 0, 0, 0, 0)))
        self.send(bytes((2, 39, 0, 0, 0, 0, 0, 0)))
        self.send(bytes((2, 64, 0, 0, 0, 0, 0, 0)))
        collection, wrapper, broker, executed, detail, history = receiver.collect()
        self.assertEqual((collection, wrapper, broker, executed, detail, history),
                         ('OBSERVED', ('WRAPPER_ARGS', errno.EACCES), ('BROKER_BUNDLE_COMPARE', None), True, None, None))
        exc = JournalError('original')
        exc._g2_owner_probe_start_diagnostic = {'purpose': 'postgres-uid', 'response': 'RESPONSE_STATUS',
            'http_status': 500, 'collection': collection, 'wrapper': wrapper, 'broker': broker,
            'runc_exec_observed': executed, 'mount_options': detail, 'create_history': None}
        value = bootstrap._owner_probe_diagnostic_value(exc)
        self.assertEqual(value['wrapper'], ['WRAPPER_ARGS', errno.EACCES])
        exc._g2_owner_probe_start_diagnostic['http_status'] = True
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        exc._g2_owner_probe_start_diagnostic['http_status'] = 500
        class Text(str):
            pass
        for key in ('purpose', 'response', 'collection'):
            original = exc._g2_owner_probe_start_diagnostic[key]
            exc._g2_owner_probe_start_diagnostic[key] = Text(original)
            self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
            exc._g2_owner_probe_start_diagnostic[key] = original
        exc._g2_owner_probe_start_diagnostic['wrapper'] = (Text('WRAPPER_ARGS'), errno.EACCES)
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))

    def test_prepare_stage_packets_and_serializer(self):
        receiver = self.receiver()
        stages = (
            (80, 'WRAPPER_PREPARE_FILE_NAME'),
            (87, 'WRAPPER_PREPARE_FILE_WRITE_MODE'),
            (95, 'WRAPPER_PREPARE_FILE_HELD_DEV'),
            (104, 'WRAPPER_PREPARE_FILE_NAMED_DEV'),
            (115, 'WRAPPER_PREPARE_DIGEST_FD'),
            (118, 'WRAPPER_PREPARE_DIGEST_UID'),
            (126, 'WRAPPER_PREPARE_DIGEST_AFTER_DEV'),
            (135, 'WRAPPER_PREPARE_READ_DATA_READ'),
            (147, 'WRAPPER_PREPARE_HASH'),
            (148, 'WRAPPER_PREPARE_JSON'),
            (149, 'WRAPPER_PREPARE_PROCESS'),
            (150, 'WRAPPER_PREPARE_SELFTEST'),
        )
        for stage, name in stages:
            with self.subTest(stage=stage):
                self.send(bytes((2, stage, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, 39, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, 64, 0, 0, 0, 0, 0, 0)))
                collection, wrapper, broker, executed, detail, history = receiver.collect()
                self.assertEqual((collection, wrapper, broker, executed, detail, history),
                                 ('OBSERVED', (name, None), ('BROKER_BUNDLE_COMPARE', None), True, None, None))
                exc = JournalError('original')
                exc._g2_owner_probe_start_diagnostic = {
                    'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                    'collection': collection, 'wrapper': wrapper, 'broker': broker,
                    'runc_exec_observed': executed, 'mount_options': detail,
                    'create_history': None}
                value = bootstrap._owner_probe_diagnostic_value(exc)
                self.assertEqual(set(value), {'purpose', 'response', 'http_status', 'collection',
                                              'wrapper', 'broker', 'runc_exec_observed', 'mount_options', 'create_history'})
                self.assertEqual(value['wrapper'], [name, None])
                self.assertEqual(value['broker'], ['BROKER_BUNDLE_COMPARE', None])
                self.assertIs(value['runc_exec_observed'], True)

    def test_prepare_stage_duplicates_and_unassigned_boundaries(self):
        receiver = self.receiver()
        for stages in ((1, 80), (150, 24), (80, 87)):
            with self.subTest(stages=stages):
                for stage in stages:
                    self.send(bytes((2, stage, 0, 0, 0, 0, 0, 0)))
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        for stage in (25, 31, 52, 63, 65, 79, 242, 255):
            with self.subTest(stage=stage):
                self.send(bytes((2, stage, 0, 0, 0, 0, 0, 0)))
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))

    def test_handler_rejection_packets(self):
        receiver = self.receiver()
        stages = ((45, 'BROKER_HANDLER_REJECT_MAP_CREATE'),
                  (46, 'BROKER_HANDLER_REJECT_PROG_LOAD'),
                  (47, 'BROKER_HANDLER_REJECT_INFO'),
                  (48, 'BROKER_HANDLER_REJECT_GET_ID'),
                  (49, 'BROKER_HANDLER_REJECT_QUERY'),
                  (50, 'BROKER_HANDLER_REJECT_ATTACH'),
                  (51, 'BROKER_HANDLER_REJECT_DETACH'))
        for stage, name in stages:
            with self.subTest(stage=stage):
                self.send(bytes((2, 1, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, stage, errno.EACCES, 0, 0, 0, 0, 0)))
                self.send(bytes((2, 64, 0, 0, 0, 0, 0, 0)))
                collection, wrapper, broker, executed, detail, history = receiver.collect()
                self.assertEqual((collection, wrapper, broker, executed, detail, history),
                                 ('OBSERVED', ('WRAPPER_ENTRY', None), (name, errno.EACCES), True, None, None))
                exc = JournalError('original')
                exc._g2_owner_probe_start_diagnostic = {
                    'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                    'collection': collection, 'wrapper': wrapper, 'broker': broker,
                    'runc_exec_observed': executed, 'mount_options': detail, 'create_history': None}
                self.assertEqual(bootstrap._owner_probe_diagnostic_value(exc)['broker'], [name, errno.EACCES])
        for first, second in ((39, 45), (51, 39), (45, 51)):
            with self.subTest(first=first, second=second):
                self.send(bytes((2, first, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, second, 0, 0, 0, 0, 0, 0)))
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        for packet in (bytes((2, 49, 0, 0, 1, 1, 1, 1)), bytes((2, 52, 0, 0, 0, 0, 0, 0))):
            self.send(packet)
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))

    def test_handler_rejection_attaches_after_cleanup(self):
        transport = ProbeTransport()
        escaped = OSError(errno.EIO, 'synthetic start failure')
        base_connection = transport.connection
        source_class = bootstrap._OwnerProbeSource
        def make_source():
            source = source_class()
            transport.sources.append(source)
            transport.source_fd = source._leaf
            return source
        def connection(daemon, timeout):
            inner = base_connection(daemon, timeout)
            original_request = inner.request
            def request(method, path, body=None, headers=None):
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    self.assertEqual(method, 'POST')
                    transport.live()
                    transport.events.append(('start', int(match.group(1), 16)))
                    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                        sender.sendto(bytes((2, 49, 13, 0, 0, 0, 0, 0)), self.path + '/' + match.group(1))
                    raise escaped
                return original_request(method, path, body=body, headers=headers)
            inner.request = request
            return inner
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        def collect(receiver):
            transport.live()
            transport.events.append(('collect', 1))
            return original_collect(receiver)
        with mock.patch.object(bootstrap, '_OwnerProbeSource', side_effect=make_source), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
            with self.assertRaises(OSError) as result:
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertIs(result.exception, escaped)
        self.assertEqual(escaped._g2_owner_probe_start_diagnostic,
                         {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                          'collection': 'OBSERVED', 'wrapper': None,
                          'broker': ('BROKER_HANDLER_REJECT_QUERY', 13), 'runc_exec_observed': False,
                          'mount_options': None, 'create_history': None})
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        self.assertLess(transport.events.index(('force-delete', 1)), transport.events.index(('settle-failed', 1)))
        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
        with self.assertRaises(OSError) as closed:
            os.fstat(transport.source_fd)
        self.assertEqual(closed.exception.errno, errno.EBADF)
        self.assertTrue(os.path.isdir(SOURCE))
        record = bootstrap._preflight_failure_diagnostic(escaped)
        self.assertEqual(set(record), {'version', 'exception', 'owner_probe', 'docker_runtime_info', 'start_error', 'settlement'})
        self.assertEqual(record['version'], 6)
        self.assertIsNone(record['start_error'])
        self.assertEqual(record['owner_probe']['broker'], ['BROKER_HANDLER_REJECT_QUERY', 13])
        self.assertIsNone(record['owner_probe']['mount_options'])
        self.assertIsNone(record['docker_runtime_info'])

    def test_oci_stage_packets_and_duplicates(self):
        receiver = self.receiver()
        stages = ((151, 'BROKER_OCI_VERSION'), (156, 'BROKER_OCI_PROCESS_USER_UID'),
                  (163, 'BROKER_OCI_PROCESS_ENV'), (180, 'BROKER_OCI_ROOT_PATH'),
                  (188, 'BROKER_OCI_MOUNT_OPTIONS'), (199, 'BROKER_OCI_RESOURCE_DEVICE_ALLOW'),
                  (220, 'BROKER_OCI_NAMESPACE_PATH'), (234, 'BROKER_OCI_SYSCALL_ARGS'),
                  (239, 'BROKER_OCI_OTHER'), (240, 'BROKER_OCI_LIMIT'), (241, 'BROKER_OCI_DEPTH'))
        for stage, name in stages:
            with self.subTest(stage=stage):
                self.send(bytes((2, 1, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, stage, 0, 0, 0, 0, 0, 0)))
                self.send(bytes((2, 64, 0, 0, 0, 0, 0, 0)))
                collection, wrapper, broker, executed, detail, history = receiver.collect()
                self.assertEqual((collection, wrapper, broker, executed, detail, history),
                                 ('OBSERVED', ('WRAPPER_ENTRY', None), (name, None), True, None, None))
                exc = JournalError('original')
                exc._g2_owner_probe_start_diagnostic = {
                    'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                    'collection': collection, 'wrapper': wrapper, 'broker': broker,
                    'runc_exec_observed': executed, 'mount_options': detail,
                    'create_history': None}
                value = bootstrap._owner_probe_diagnostic_value(exc)
                self.assertEqual(set(value), {'purpose', 'response', 'http_status', 'collection',
                                              'wrapper', 'broker', 'runc_exec_observed', 'mount_options', 'create_history'})
                self.assertEqual(value['broker'], [name, None])
        for stages in ((39, 151), (241, 39), (151, 188)):
            with self.subTest(stages=stages):
                for stage in stages:
                    self.send(bytes((2, stage, 0, 0, 0, 0, 0, 0)))
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))

    def test_mount_options_v2_packets_and_serializer(self):
        receiver = self.receiver()
        expected = {'mount': 'PGDATA', 'actual': 'RBIND_RO_RPRIVATE',
                    'expected': 'RBIND_RRO_RPRIVATE', 'difference': 'VALUE'}
        self.send(bytes((2, 1, 0, 0, 0, 0, 0, 0)))
        self.send(bytes((2, 188, 0, 0, 11, 12, 13, 6)))
        self.send(bytes((2, 64, 0, 0, 0, 0, 0, 0)))
        collection, wrapper, broker, executed, detail, history = receiver.collect()
        self.assertEqual((collection, wrapper, broker, executed, detail, history),
                         ('OBSERVED', ('WRAPPER_ENTRY', None), ('BROKER_OCI_MOUNT_OPTIONS', None), True, expected, None))
        exc = JournalError('original')
        exc._g2_owner_probe_start_diagnostic = {'purpose': 'postgres-uid', 'response': 'OTHER',
            'http_status': None, 'collection': collection, 'wrapper': wrapper, 'broker': broker,
            'runc_exec_observed': executed, 'mount_options': detail, 'create_history': None}
        value = bootstrap._owner_probe_diagnostic_value(exc)
        self.assertEqual(len(value), 9)
        self.assertEqual(value['mount_options'], expected)
        self.send(bytes((2, 188, 0, 0, 1, 15, 6, 5)))
        self.assertEqual(receiver.collect(), ('OBSERVED', None, ('BROKER_OCI_MOUNT_OPTIONS', None), False,
                         {'mount': 'PROC', 'actual': 'OTHER_STRING_ARRAY', 'expected': 'NOSUID_NOEXEC_NODEV', 'difference': 'ORDER'}, None))
        self.assertEqual(receiver.collect(), ('EMPTY', None, None, False, None, None))
        self.send(bytes((2, 188, 0, 0, 0, 0, 0, 0)))
        self.assertEqual(receiver.collect(), ('OBSERVED', None, ('BROKER_OCI_MOUNT_OPTIONS', None), False, None, None))
        for collection in ('UNAVAILABLE', 'EMPTY', 'INVALID'):
            exc._g2_owner_probe_start_diagnostic.update(collection=collection, wrapper=None, broker=None,
                                                        runc_exec_observed=False, mount_options=None)
            value = bootstrap._owner_probe_diagnostic_value(exc)
            self.assertIsNone(value['mount_options'])
        exc._g2_owner_probe_start_diagnostic.update(collection='OBSERVED', wrapper=('WRAPPER_ENTRY', None),
            broker=('BROKER_OCI_MOUNT_OPTIONS', None), runc_exec_observed=True, mount_options=expected)
        for bad in ({'mount': 'PGDATA'}, dict(expected, mount='UNKNOWN'), dict(expected, extra='x'), True):
            exc._g2_owner_probe_start_diagnostic['mount_options'] = bad
            self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        exc._g2_owner_probe_start_diagnostic['mount_options'] = expected
        exc._g2_owner_probe_start_diagnostic['broker'] = ('BROKER_OCI_VERSION', None)
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        exc._g2_owner_probe_start_diagnostic['broker'] = ('BROKER_OCI_MOUNT_OPTIONS', errno.EACCES)
        exc._g2_owner_probe_start_diagnostic['mount_options'] = None
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))
        del exc._g2_owner_probe_start_diagnostic['mount_options']
        self.assertIsNone(bootstrap._owner_probe_diagnostic_value(exc))

    def test_mount_options_v2_invalid_encodings(self):
        receiver = self.receiver()
        packets = (b'', bytes((1, 188, 0, 0)), bytes((1, 188, 0, 0, 0, 0, 0, 0)),
                   bytes((2, 188, 0, 0, 11, 12, 13)), bytes((2, 188, 0, 0, 11, 12, 13, 6, 0)),
                   bytes((2, 188, 0, 0, 11, 12, 13, 0)), bytes((2, 188, 0, 0, 13, 12, 13, 6)),
                   bytes((2, 188, 0, 0, 11, 18, 13, 6)), bytes((2, 188, 0, 0, 11, 12, 18, 6)),
                   bytes((2, 188, 0, 0, 11, 12, 13, 10)), bytes((2, 151, 0, 0, 11, 12, 13, 6)),
                   bytes((2, 188, 13, 0, 0, 0, 0, 0)), bytes((2, 242, 0, 0, 0, 0, 0, 0)),
                   bytes((2, 255, 0, 0, 0, 0, 0, 0)))
        for packet in packets:
            with self.subTest(packet=packet):
                self.send(packet)
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        for first, second in ((188, 188), (39, 188), (188, 39)):
            self.send(bytes((2, first, 0, 0, 0, 0, 0, 0)))
            self.send(bytes((2, second, 0, 0, 0, 0, 0, 0)))
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))

    def test_mount_options_v2_attaches_after_cleanup(self):
        transport = ProbeTransport()
        escaped = OSError(errno.EIO, 'synthetic start failure')
        base_connection = transport.connection
        def connection(daemon, timeout):
            inner = base_connection(daemon, timeout)
            original_request = inner.request
            def request(method, path, body=None, headers=None):
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    self.assertEqual(method, 'POST')
                    transport.events.append(('start', int(match.group(1), 16)))
                    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                        sender.sendto(bytes((2, 188, 0, 0, 11, 12, 13, 6)), self.path + '/' + match.group(1))
                    raise escaped
                return original_request(method, path, body=body, headers=headers)
            inner.request = request
            return inner
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        def collect(receiver):
            transport.events.append(('collect', 1))
            return original_collect(receiver)
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
            with self.assertRaises(OSError) as result:
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertIs(result.exception, escaped)
        value = escaped._g2_owner_probe_start_diagnostic
        self.assertEqual(value, {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
            'collection': 'OBSERVED', 'wrapper': None, 'broker': ('BROKER_OCI_MOUNT_OPTIONS', None),
            'runc_exec_observed': False, 'mount_options': {'mount': 'PGDATA', 'actual': 'RBIND_RO_RPRIVATE',
                'expected': 'RBIND_RRO_RPRIVATE', 'difference': 'VALUE'}, 'create_history': None})
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        self.assertLess(transport.events.index(('force-delete', 1)), transport.events.index(('settle-failed', 1)))
        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
        value = bootstrap._owner_probe_diagnostic_value(escaped)
        self.assertEqual(len(value), 9)
        self.assertFalse(os.path.lexists(self.path))
        self.assertTrue(os.path.isdir(SOURCE))

    def test_new_oci_stage_attaches_after_cleanup(self):
        transport = ProbeTransport()
        escaped = OSError(errno.EIO, 'synthetic start failure')
        base_connection = transport.connection
        def connection(daemon, timeout):
            inner = base_connection(daemon, timeout)
            original_request = inner.request
            def request(method, path, body=None, headers=None):
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    self.assertEqual(method, 'POST')
                    transport.events.append(('start', int(match.group(1), 16)))
                    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                        sender.sendto(bytes((2, 188, 0, 0, 0, 0, 0, 0)), self.path + '/' + match.group(1))
                    raise escaped
                return original_request(method, path, body=body, headers=headers)
            inner.request = request
            return inner
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        def collect(receiver):
            transport.events.append(('collect', 1))
            return original_collect(receiver)
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
            with self.assertRaises(OSError) as result:
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertIs(result.exception, escaped)
        self.assertEqual(escaped._g2_owner_probe_start_diagnostic,
                         {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                          'collection': 'OBSERVED', 'wrapper': None,
                          'broker': ('BROKER_OCI_MOUNT_OPTIONS', None), 'runc_exec_observed': False, 'mount_options': None, 'create_history': None})
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        self.assertLess(transport.events.index(('force-delete', 1)), transport.events.index(('settle-failed', 1)))
        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
        value = bootstrap._owner_probe_diagnostic_value(escaped)
        self.assertEqual(set(value), {'purpose', 'response', 'http_status', 'collection',
                                      'wrapper', 'broker', 'runc_exec_observed', 'mount_options', 'create_history'})
        self.assertEqual(value['broker'], ['BROKER_OCI_MOUNT_OPTIONS', None])
        self.assertFalse(os.path.lexists(self.path))
        self.assertTrue(os.path.isdir(SOURCE))

    def test_new_prepare_stage_attaches_after_cleanup(self):
        transport = ProbeTransport()
        escaped = OSError(errno.EIO, 'synthetic start failure')
        base_connection = transport.connection
        def connection(daemon, timeout):
            inner = base_connection(daemon, timeout)
            original_request = inner.request
            def request(method, path, body=None, headers=None):
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    self.assertEqual(method, 'POST')
                    transport.events.append(('start', int(match.group(1), 16)))
                    with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                        sender.sendto(bytes((2, 87, 0, 0, 0, 0, 0, 0)), self.path + '/' + match.group(1))
                    raise escaped
                return original_request(method, path, body=body, headers=headers)
            inner.request = request
            return inner
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        def collect(receiver):
            transport.events.append(('collect', 1))
            return original_collect(receiver)
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
            with self.assertRaises(OSError) as result:
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertIs(result.exception, escaped)
        diag = escaped._g2_owner_probe_start_diagnostic
        self.assertEqual(diag, {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                                'collection': 'OBSERVED',
                                'wrapper': ('WRAPPER_PREPARE_FILE_WRITE_MODE', None),
                                'broker': None, 'runc_exec_observed': False, 'mount_options': None, 'create_history': None})
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        self.assertLess(transport.events.index(('force-delete', 1)), transport.events.index(('settle-failed', 1)))
        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
        decoded = bootstrap._owner_probe_diagnostic_value(escaped)
        self.assertEqual(set(decoded), {'purpose', 'response', 'http_status', 'collection',
                                        'wrapper', 'broker', 'runc_exec_observed', 'mount_options', 'create_history'})
        self.assertEqual(decoded['wrapper'], ['WRAPPER_PREPARE_FILE_WRITE_MODE', None])
        self.assertFalse(os.path.lexists(self.path))
        self.assertTrue(os.path.isdir(SOURCE))

    def test_invalid_packets_and_rights(self):
        receiver = self.receiver()
        for packets in ((b'x',), (bytes((2, 25, 0, 0, 0, 0, 0, 0)),), (bytes((2, 1, 0, 16, 0, 0, 0, 0)),),
                        (bytes((2, 1, 0, 0, 0, 0, 0, 0)),) * 2, (bytes((2, 64, 0, 0, 0, 0, 0, 0)),) * 4):
            with self.subTest(packets=packets):
                for packet in packets:
                    self.send(packet)
                self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
        before = set(os.listdir('/proc/self/fd'))
        fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        try:
            self.send(bytes((2, 1, 0, 0, 0, 0, 0, 0)), [(socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', fd))])
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
            os.fstat(fd)
        finally:
            os.close(fd)
        self.assertEqual(set(os.listdir('/proc/self/fd')), before)

    def test_delivered_rights_closed_even_if_truncated(self):
        receiver = self.receiver()
        owned = os.dup(receiver.directory)
        original = receiver.sock
        class ReceiveOnce:
            def recvmsg(self, *args):
                return (bytes((2, 1, 0, 0, 0, 0, 0, 0)), [(socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', owned))],
                        socket.MSG_CTRUNC, None)
        receiver.sock = ReceiveOnce()
        try:
            self.assertEqual(receiver.collect(), ('INVALID', None, None, False, None, None))
            with self.assertRaises(OSError) as failure:
                os.fstat(owned)
            self.assertEqual(failure.exception.errno, errno.EBADF)
            os.fstat(receiver.directory)
            os.fstat(original.fileno())
        finally:
            receiver.sock = original

    def test_preexisting_directory_and_symlink_are_preserved(self):
        source = self.source()
        for is_link in (False, True):
            if is_link:
                os.symlink('/run/g2', self.path)
            else:
                os.mkdir(self.path, 0o700)
            with self.assertRaises(OSError):
                bootstrap._OwnerProbeStartDiagnostic(source, self.cid)
            self.assertTrue(os.path.lexists(self.path))
            remove_owned(self.path)

    def test_start_error_response_and_cleanup(self):
        for number, purpose in ((1, 'postgres-uid'), (2, 'postgres-gid')):
            with self.subTest(purpose=purpose):
                transport = ProbeTransport()
                source = self.source()
                source_fd = source._leaf
                transport.sources, transport.source_fd = [source], source_fd
                errors = []
                original_error = bootstrap.JournalError
                class CapturedError(original_error):
                    def __init__(self, *args):
                        super().__init__(*args)
                        errors.append(self)
                original_connection = transport.connection
                def connection(daemon, timeout):
                    inner = original_connection(daemon, timeout)
                    original_request = inner.request
                    def request(method, path, body=None, headers=None):
                        original_request(method, path, body=body, headers=headers)
                        if path.endswith('/start') and transport.current == number:
                            inner.response = FakeResponse({'message': 'load program: SECRET'}, 500)
                        elif method == 'DELETE':
                            inner.response = FakeResponse({'message': 'procReady not received'}, 204)
                    inner.request = request
                    return inner
                original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
                def collect(receiver):
                    transport.live()
                    transport.events.append(('collect', 1))
                    return original_collect(receiver)
                try:
                    with mock.patch.object(bootstrap, 'JournalError', CapturedError), \
                         mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
                         mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
                         mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect), \
                         mock.patch.object(bootstrap, '_OwnerProbeSource', return_value=source):
                        with self.assertRaises(CapturedError) as caught:
                            bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
                    exc = caught.exception
                    self.assertIs(exc, errors[0])
                    self.assertEqual(str(exc), 'PostgreSQL owner probe Docker operation failed')
                    self.assertEqual(exc._g2_owner_probe_start_error, 'PROGRAM_LOAD')
                    owner = exc._g2_owner_probe_start_diagnostic
                    self.assertEqual((owner['purpose'], owner['response'], owner['http_status']),
                                     (purpose, 'RESPONSE_STATUS', 500))
                    self.assertEqual(bootstrap._preflight_failure_diagnostic(exc)['start_error'], 'PROGRAM_LOAD')
                    self.assertLess(transport.events.index(('force-delete', number)), transport.events.index(('settle-failed', number)))
                    self.assertLess(transport.events.index(('settle-failed', number)), transport.events.index(('collect', 1)))
                    expected_deadlines = ([('completed', 1, 1060)] if number == 2 else []) + [('failed', number, 1060)]
                    self.assertEqual(transport.settlement_deadlines, expected_deadlines)
                    self.assertIsNone(source._leaf)
                    self.assertTrue(os.path.isdir(SOURCE))
                    self.assertFalse(os.path.lexists(self.path))
                finally:
                    source.close()
                    remove_owned(SOURCE)
                with self.assertRaises(OSError) as closed:
                    os.fstat(source_fd)
                self.assertEqual(closed.exception.errno, errno.EBADF)

    def test_start_error_rejection_precedence(self):
        marker = {'message': 'load program: SECRET'}
        for scenario in ('before', 'deadline', 'size', 'status400', 'status_float', 'create', 'inspect',
                         'wait', 'delete', 'unobserved', 'transport', 'close', 'classifier', 'attribute'):
            with self.subTest(scenario=scenario):
                transport = ProbeTransport()
                source = self.source()
                source_fd = source._leaf
                transport.sources, transport.source_fd = [source], source_fd
                original_connection = transport.connection
                replacement = OSError(errno.EIO, 'replacement SECRET')
                rejected = []
                original_error = bootstrap.JournalError
                class CapturedError(original_error):
                    def __init__(self, *args):
                        super().__init__(*args)
                        rejected.append(self)
                    def __setattr__(self, name, value):
                        if scenario == 'attribute' and name == '_g2_owner_probe_start_error':
                            raise RuntimeError('attribute SECRET')
                        super().__setattr__(name, value)
                class Response:
                    def __init__(self, status):
                        self._status = status
                    @property
                    def status(self):
                        if scenario in ('deadline', 'size'):
                            raise AssertionError('status evaluated after earlier rejection')
                        return self._status
                    def read(self, limit):
                        if scenario == 'deadline':
                            transport.now = 1060
                        if scenario in ('deadline', 'size'):
                            return b'{"message":"load program: SECRET"}' + b' ' * 65537
                        return json.dumps(marker).encode()
                def connection(daemon, timeout):
                    inner = original_connection(daemon, timeout)
                    class Wrapper:
                        def __init__(self):
                            self.is_start = False
                        def __getattr__(self, name):
                            return getattr(inner, name)
                        def request(self, method, path, body=None, headers=None):
                            self.is_start = path.endswith('/start')
                            if self.is_start and scenario == 'transport':
                                raise replacement
                            inner.request(method, path, body=body, headers=headers)
                            target = (self.is_start if scenario not in ('create', 'inspect', 'wait', 'delete') else
                                      path.endswith('/create') if scenario == 'create' else
                                      path.endswith('/json') if scenario == 'inspect' else
                                      '/wait?' in path if scenario == 'wait' else
                                      method == 'DELETE' and 'force=1' not in path)
                            if target:
                                status = 400 if scenario == 'status400' else 500.0 if scenario == 'status_float' else 500
                                inner.response = Response(status)
                        def close(self):
                            if self.is_start and scenario == 'close':
                                raise replacement
                            inner.close()
                    return Wrapper()
                classifier = bootstrap._owner_probe_start_error
                calls = []
                def classify(data):
                    calls.append(data)
                    if scenario == 'classifier':
                        raise RuntimeError('classifier SECRET')
                    return classifier(data)
                original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
                def collect(receiver):
                    transport.live()
                    transport.events.append(('collect', 1))
                    return original_collect(receiver)
                original_check = source.check
                checks = []
                def check():
                    original_check()
                    checks.append(1)
                    if scenario == 'before' and len(checks) == 3:
                        transport.now = 1060
                try:
                    with mock.patch.object(bootstrap, 'JournalError', CapturedError), \
                         mock.patch.object(bootstrap, '_owner_probe_start_error', side_effect=classify), \
                         mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
                         mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
                         mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect), \
                         mock.patch.object(source, 'check', side_effect=check):
                        with self.assertRaises((CapturedError, OSError)) as caught:
                            bootstrap._owner_probe({}, transport, '-u', source,
                                                   observe_owner_start=scenario != 'unobserved')
                    exc = caught.exception
                    if scenario in ('transport', 'close'):
                        self.assertIs(exc, replacement)
                    else:
                        self.assertIs(exc, rejected[0])
                    if scenario == 'close':
                        self.assertIs(exc.__context__, rejected[0])
                        self.assertEqual(rejected[0]._g2_owner_probe_start_error, 'PROGRAM_LOAD')
                        self.assertEqual(exc._g2_owner_probe_start_diagnostic['response'], 'OTHER')
                    if scenario in ('classifier', 'attribute'):
                        self.assertEqual(len(calls), 1)
                        self.assertFalse(hasattr(exc, '_g2_owner_probe_start_error'))
                        self.assertEqual(exc._g2_owner_probe_start_diagnostic['response'], 'RESPONSE_STATUS')
                    elif scenario == 'close':
                        self.assertEqual(len(calls), 1)
                    else:
                        self.assertEqual(calls, [])
                    record = bootstrap._preflight_failure_diagnostic(exc)
                    self.assertIsNone(record['start_error'])
                    if scenario not in ('create', 'inspect', 'wait', 'delete', 'unobserved'):
                        self.assertIn(('force-delete', 1), transport.events)
                        self.assertIn(('settle-failed', 1), transport.events)
                        self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
                        self.assertEqual(transport.settlement_deadlines,
                                         [('failed', 1, 1070 if scenario in ('before', 'deadline') else 1060)])
                    self.assertTrue(os.path.isdir(SOURCE))
                    os.fstat(source_fd)
                finally:
                    source.close()
                    remove_owned(SOURCE)
                with self.assertRaises(OSError) as closed:
                    os.fstat(source_fd)
                self.assertEqual(closed.exception.errno, errno.EBADF)

    def test_start_response_rejections_and_cleanup(self):
        for failure, expected in (('deadline', 'RESPONSE_DEADLINE'), ('size', 'RESPONSE_SIZE'),
                                  ('status', 'RESPONSE_STATUS'), ('before', 'BEFORE_REQUEST_DEADLINE'),
                                  ('transport', 'OTHER'), ('close', 'OTHER')):
            with self.subTest(failure=failure):
                transport = ProbeTransport()
                escaped = OSError(errno.EIO, 'synthetic')
                base_connection = transport.connection
                class Response:
                    @property
                    def status(self):
                        if failure in ('deadline', 'size'):
                            raise AssertionError('status evaluated after earlier rejection')
                        return 500
                    def read(self, limit):
                        if failure == 'deadline':
                            transport.now = 1060
                        return b'x' * 65537 if failure in ('deadline', 'size') else b''
                def connection(daemon, timeout):
                    inner = base_connection(daemon, timeout)
                    class Wrapper:
                        def __init__(self):
                            self.start = False
                        def __getattr__(self, key):
                            return getattr(inner, key)
                        def request(self, method, path, body=None, headers=None):
                            self.start = path.endswith('/start')
                            if self.start and failure == 'transport':
                                raise escaped
                            inner.request(method, path, body=body, headers=headers)
                            if self.start:
                                inner.response = Response()
                        def close(self):
                            if self.start and failure == 'close':
                                raise escaped
                            inner.close()
                    return Wrapper()
                cls = bootstrap._OwnerProbeSource
                original_check = cls.check
                checks = []
                def check(source):
                    checks.append(1)
                    original_check(source)
                    if failure == 'before' and len(checks) == 4:
                        transport.now = 1060
                original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
                def collect(receiver):
                    transport.events.append(('collect', 1))
                    return original_collect(receiver)
                with mock.patch.object(cls, 'check', check), \
                     mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
                     mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
                     mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect):
                    with self.assertRaises((JournalError, OSError)) as result:
                        bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
                exc = result.exception
                if failure in ('transport', 'close'):
                    self.assertIs(exc, escaped)
                diag = exc._g2_owner_probe_start_diagnostic
                self.assertEqual(diag['response'], expected)
                self.assertEqual(diag['http_status'], 500 if failure == 'status' else None)
                self.assertEqual(diag['collection'], 'EMPTY')
                self.assertIn(('force-delete', 1), transport.events)
                self.assertIn(('settle-failed', 1), transport.events)
                self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1070 if failure in ('deadline', 'before') else 1060)])
                self.assertLess(transport.events.index(('settle-failed', 1)), transport.events.index(('collect', 1)))
                if failure == 'before':
                    self.assertNotIn(('start', 1), transport.events)
                self.assertFalse(os.path.lexists(self.path))
                remove_owned(SOURCE)

    def test_success_closes_without_collect_and_unavailable_preserves_name(self):
        transport = ProbeTransport()
        with mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=transport.connection), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', side_effect=AssertionError('collect on success')):
            self.assertEqual(bootstrap._owner_probe_pair({}, transport, observe_owner_start=True), [999, 999])
        self.assertFalse(os.path.lexists(self.path))
        os.mkdir(self.path, 0o700)
        transport = ProbeTransport('wait')
        with mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=transport.connection):
            with self.assertRaises(JournalError):
                bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
        self.assertTrue(os.path.isdir(self.path))

    def test_start_failure_unavailable_or_collect_exception(self):
        for unavailable in (True, False):
            with self.subTest(unavailable=unavailable):
                transport = ProbeTransport()
                original = transport.connection
                failure = OSError(errno.EIO, 'start')
                def connection(daemon, timeout):
                    inner = original(daemon, timeout)
                    old_request = inner.request
                    def request(method, path, body=None, headers=None):
                        if path.endswith('/start'):
                            raise failure
                        return old_request(method, path, body=body, headers=headers)
                    inner.request = request
                    return inner
                if unavailable:
                    os.mkdir(self.path, 0o700)
                with mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
                     mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', side_effect=OSError(errno.EIO, 'collect')):
                    with self.assertRaises(OSError) as result:
                        bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
                self.assertIs(result.exception, failure)
                diag = failure._g2_owner_probe_start_diagnostic
                self.assertEqual(diag['response'], 'OTHER')
                self.assertEqual(diag['collection'], 'UNAVAILABLE' if unavailable else 'INVALID')
                self.assertIsNone(diag['mount_options'])
                self.assertIn(('settle-failed', 1), transport.events)
                if unavailable:
                    self.assertTrue(os.path.isdir(self.path))
                    os.rmdir(self.path)
                else:
                    self.assertFalse(os.path.lexists(self.path))
                remove_owned(SOURCE)


class DockerRuntimeInfoTests(unittest.TestCase):
    @staticmethod
    def info(features='{"mountOptions":["rbind","rro"]}'):
        return {'DefaultRuntime': 'g2-bpf', 'Runtimes': {'g2-bpf': {
            'path': '/opt/g2/g2-bpf-runtime', 'status': {
                'org.opencontainers.runtime-spec.features': features}}}}

    @staticmethod
    def result(collection, features, reason):
        return {'collection': collection, 'features': features, 'reason': reason}

    @staticmethod
    def owner():
        return {'purpose': 'postgres-uid', 'response': 'RESPONSE_STATUS', 'http_status': 400,
                'collection': 'OBSERVED', 'wrapper': None, 'broker': ['BROKER_OCI_MOUNT_OPTIONS', None],
                'runc_exec_observed': False, 'mount_options': {'mount': 'PGDATA',
                'actual': 'RBIND_RO_RPRIVATE', 'expected': 'RBIND_RRO_RPRIVATE', 'difference': 'VALUE'}, 'create_history': None}

    def test_projection_semantics(self):
        for raw, expected in (('{"mountOptions":["rbind","rro"]}', 'RRO_PRESENT'),
                              ('{"mountOptions":[]}', 'RRO_ABSENT'), ('{}', 'RRO_ABSENT'),
                              ('{"mountOptions":["RRO"]}', 'RRO_ABSENT'),
                              ('{"mountOptions":["rro","rro"],"secret":"hidden"}', 'RRO_PRESENT')):
            info = self.info(raw)
            info['secret'] = 'hidden'
            before = copy.deepcopy(info)
            value = bootstrap._docker_runtime_info_projection(info)
            self.assertEqual(value, self.result('OBSERVED', expected, 'OK'))
            self.assertEqual(info, before)
            self.assertNotIn('hidden', repr(value))
        for status in (None, {}):
            info = self.info()
            if status is None:
                del info['Runtimes']['g2-bpf']['status']
            else:
                info['Runtimes']['g2-bpf']['status'] = status
            self.assertEqual(bootstrap._docker_runtime_info_projection(info), self.result('OBSERVED', 'NOT_PUBLISHED', 'OK'))

    def test_projection_bounds_and_strict_json(self):
        class Text(str):
            pass
        invalid = []
        for key, value in (('DefaultRuntime', 'other'), ('DefaultRuntime', Text('g2-bpf')), ('Runtimes', None)):
            item = self.info()
            item[key] = value
            invalid.append(item)
        for key, value in (('path', 'wrong'), ('path', Text('/opt/g2/g2-bpf-runtime')),
                           ('runtimeArgs', ['x']), ('runtimeArgs', None), ('runtimeType', 'secret'),
                           ('status', None), ('status', {'unknown': 'x'}),
                           ('status', {'org.opencontainers.runtime-spec.features': True})):
            item = self.info()
            item['Runtimes']['g2-bpf'][key] = value
            invalid.append(item)
        for item in invalid:
            self.assertEqual(bootstrap._docker_runtime_info_projection(item), self.result('INVALID', None, 'RUNTIME'))
        for raw in ('{"x":1,"x":2}', '{"x":NaN}', '{"x":Infinity}', '{', '\ud800', ' ' * 16385):
            self.assertEqual(bootstrap._docker_runtime_info_projection(self.info(raw)), self.result('INVALID', None, 'FEATURES_JSON'))
        # The actual decoder accepts this nesting; the decoded array fails the features shape gate.
        deep = '[' * 1100 + '0' + ']' * 1100
        self.assertEqual(bootstrap._docker_runtime_info_projection(self.info(deep)),
                         self.result('INVALID', None, 'FEATURES_SHAPE'))
        # Inject a decoder recursion failure separately to exercise its error boundary.
        with mock.patch.object(bootstrap, 'strict_json', side_effect=RecursionError('synthetic')):
            self.assertEqual(bootstrap._docker_runtime_info_projection(self.info('{}')),
                             self.result('INVALID', None, 'FEATURES_JSON'))
        for raw in ('[]', '{"mountOptions":null}', '{"mountOptions":true}',
                    json.dumps({'mountOptions': ['x'] * 129}), json.dumps({'mountOptions': [True]}),
                    json.dumps({'mountOptions': ['x' * 65]}), json.dumps({'mountOptions': ['é']})):
            self.assertEqual(bootstrap._docker_runtime_info_projection(self.info(raw)), self.result('INVALID', None, 'FEATURES_SHAPE'))
        for raw in ('{}' + ' ' * 16382, json.dumps({'mountOptions': ['x'] * 128}),
                    json.dumps({'mountOptions': ['x' * 64]})):
            self.assertEqual(bootstrap._docker_runtime_info_projection(self.info(raw)), self.result('OBSERVED', 'RRO_ABSENT', 'OK'))

    def test_normalizer_copy_and_closed_shape(self):
        for collection, features, reasons in (('OBSERVED', ('RRO_PRESENT', 'RRO_ABSENT', 'NOT_PUBLISHED'), ('OK',)),
                ('UNAVAILABLE', (None,), ('DEADLINE', 'RESPONSE_SIZE', 'RESPONSE_STATUS', 'TRANSPORT')),
                ('INVALID', (None,), ('INFO_JSON', 'RUNTIME', 'FEATURES_JSON', 'FEATURES_SHAPE'))):
            for feature in features:
                for reason in reasons:
                    original = self.result(collection, feature, reason)
                    value = bootstrap._docker_runtime_info_diagnostic_value(original)
                    self.assertEqual(value, original)
                    self.assertIsNot(value, original)
                    value['reason'] = 'changed'
                    self.assertEqual(original['reason'], reason)
        class Text(str):
            pass
        for value in (None, True, [], (), {}, {'collection': 'OBSERVED', 'features': 'RRO_PRESENT', 'reason': 'OK', 'extra': 1},
                      self.result(Text('OBSERVED'), 'RRO_PRESENT', 'OK'), self.result('OBSERVED', None, 'OK'),
                      self.result('INVALID', 'RRO_PRESENT', 'RUNTIME')):
            self.assertIsNone(bootstrap._docker_runtime_info_diagnostic_value(value))
        owner = self.owner()
        self.assertTrue(bootstrap._docker_runtime_info_eligible(owner))
        for change in ({'response': 'OTHER'}, {'http_status': 500}, {'http_status': True},
                       {'broker': ['BROKER_OCI_MOUNT_OPTIONS', 13]}, {'mount_options': None},
                       {'mount_options': dict(owner['mount_options'], mount='PROC')}, {'purpose': 'other'}):
            self.assertFalse(bootstrap._docker_runtime_info_eligible(dict(owner, **change)))

    def observer(self, response, *, clock=None, failure=None):
        events = []
        now = [100.0] if clock is None else clock
        class Connection:
            active_sock = object()
            def __init__(self, daemon, timeout):
                events.append(('construct', timeout))
            def connect(self):
                events.append(('connect',))
                if failure == 'connect':
                    raise OSError('connect')
                if failure == 'expire-connect':
                    now[0] = 101.0
            def request(self, method, path):
                events.append(('request', method, path))
                if failure == 'request':
                    raise OSError('request')
            def getresponse(self):
                return response
            def abort(self):
                events.append(('abort',))
            def close(self):
                events.append(('connection-close',))
                if failure == 'connection-close':
                    raise OSError('close')
        class Timer:
            def __init__(self, delay, callback):
                events.append(('timer', delay))
                self.callback = callback
                self.daemon = False
            def start(self):
                events.append(('timer-start', self.daemon))
                if failure == 'timer-start':
                    raise OSError('timer')
            def cancel(self):
                events.append(('cancel',))
                if failure == 'cancel':
                    raise OSError('cancel')
        response.events = events
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: now[0]), \
             mock.patch.object(guest_postgres, '_UnixHTTP', Connection), mock.patch.object(bootstrap.threading, 'Timer', Timer):
            value = bootstrap._observe_docker_runtime_info({}, 101.0)
        return value, events

    class Response:
        def __init__(self, body=b'{}', status=200, failure=None, on_read=None):
            self.body, self._status, self.failure, self.on_read = body, status, failure, on_read
            self.events = []
        @property
        def status(self):
            if self.failure == 'status-read':
                raise AssertionError('status evaluated too early')
            return self._status
        def read(self, limit):
            self.events.append(('read', limit))
            if self.failure == 'read':
                raise OSError('read')
            if self.on_read:
                self.on_read()
            return self.body
        def close(self):
            self.events.append(('response-close',))
            if self.failure == 'close':
                raise OSError('close')

    def test_observer_deadline_and_short_circuit(self):
        response = self.Response()
        value, events = self.observer(response, clock=[101.0])
        self.assertEqual(value, self.result('UNAVAILABLE', None, 'DEADLINE'))
        self.assertEqual(events, [])
        value, events = self.observer(response, failure='expire-connect')
        self.assertEqual(value, self.result('UNAVAILABLE', None, 'DEADLINE'))
        self.assertFalse(any(event[0] in ('request', 'timer') for event in events))
        response = self.Response(b'x' * 65537, failure='status-read')
        value, events = self.observer(response)
        self.assertEqual(value, self.result('UNAVAILABLE', None, 'RESPONSE_SIZE'))
        self.assertEqual(events[:4], [('construct', 1.0), ('connect',), ('timer', 1.0), ('timer-start', True)])
        self.assertIn(('request', 'GET', '/v1.44/info'), events)
        now = [100.0]
        response = self.Response(b'x' * 65537, failure='status-read', on_read=lambda: now.__setitem__(0, 101.0))
        self.assertEqual(self.observer(response, clock=now)[0], self.result('UNAVAILABLE', None, 'DEADLINE'))
        for status in (True, 201, '200'):
            self.assertEqual(self.observer(self.Response(status=status))[0], self.result('UNAVAILABLE', None, 'RESPONSE_STATUS'))
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}', b'\xff'):
            self.assertEqual(self.observer(self.Response(raw))[0], self.result('INVALID', None, 'INFO_JSON'))
        # The actual decoder accepts this nesting; the decoded array fails the Info runtime gate.
        deep = b'[' * 1100 + b'0' + b']' * 1100
        self.assertEqual(self.observer(self.Response(deep))[0], self.result('INVALID', None, 'RUNTIME'))
        # Inject a decoder recursion failure separately to exercise its error boundary.
        with mock.patch.object(bootstrap, 'strict_json', side_effect=RecursionError('synthetic')):
            self.assertEqual(self.observer(self.Response(b'{}'))[0], self.result('INVALID', None, 'INFO_JSON'))
        data = json.dumps(self.info()).encode('ascii')
        self.assertEqual(self.observer(self.Response(data))[0], self.result('OBSERVED', 'RRO_PRESENT', 'OK'))

    def test_observer_transport_cleanup(self):
        for failure in ('connect', 'request', 'timer-start', 'cancel', 'connection-close'):
            value, events = self.observer(self.Response(), failure=failure)
            self.assertEqual(value, self.result('UNAVAILABLE', None, 'TRANSPORT'))
            self.assertEqual(sum(event[0] == 'connection-close' for event in events), 1)
        for failure in ('read', 'close'):
            value, events = self.observer(self.Response(failure=failure))
            self.assertEqual(value, self.result('UNAVAILABLE', None, 'TRANSPORT'))
            self.assertLess(events.index(('cancel',)), events.index(('response-close',)))
            self.assertLess(events.index(('response-close',)), events.index(('connection-close',)))

    def test_real_timer_aborts_blocked_read(self):
        left, right = socket.socketpair()
        fd = left.fileno()
        completed = threading.Event()
        class Connection:
            def __init__(self, daemon, timeout):
                self.active_sock = left
            abort = guest_postgres._UnixHTTP.abort
            def connect(self):
                pass
            def request(self, method, path):
                pass
            def getresponse(self):
                class Response:
                    status = 200
                    def read(self, limit):
                        try:
                            return left.recv(limit)
                        finally:
                            completed.set()
                    def close(self):
                        pass
                return Response()
            def close(self):
                left.close()
        try:
            with mock.patch.object(guest_postgres, '_UnixHTTP', Connection):
                value = bootstrap._observe_docker_runtime_info({}, time.monotonic() + 0.05)
            self.assertTrue(completed.wait(1))
            self.assertEqual(value, self.result('UNAVAILABLE', None, 'DEADLINE'))
            with self.assertRaises(OSError) as error:
                os.fstat(fd)
            self.assertEqual(error.exception.errno, errno.EBADF)
        finally:
            left.close()
            right.close()


class DockerRuntimeInfoLifecycleTests(SourceFixture):
    def setUp(self):
        super().setUp()
        self.path = '/run/g2/owner-probe-diagnostic'
        self.assertFalse(os.path.lexists(self.path))
        self.addCleanup(remove_owned, self.path)

    def exercise(self, *, status=400, packet=bytes((2, 188, 0, 0, 11, 12, 13, 6)),
                 failure=None, replace=None, info_failure=None, expire_cleanup=False):
        transport = ProbeTransport(failure)
        original_connection = transport.connection
        original_collect = bootstrap._OwnerProbeStartDiagnostic.collect
        original_close = bootstrap._OwnerProbeStartDiagnostic.close
        original_source = bootstrap._OwnerProbeSource
        original_check = bootstrap._OwnerProbeSource.check
        checks = []
        original_observe = bootstrap._observe_docker_runtime_info
        replacement = OSError(errno.EIO, 'replacement')
        start_error = OSError(errno.EIO, 'start')
        callbacks = []
        observed_deadlines = []
        sources = []

        class Timer:
            def __init__(self, delay, callback):
                self.callback = callback
                self.daemon = False
                callbacks.append(self)
                transport.events.append(('timer', delay))
            def start(self):
                transport.events.append(('timer-start', self.daemon))
            def cancel(self):
                transport.events.append(('timer-cancel',))

        def source():
            result = original_source()
            sources.append(result)
            transport.sources.append(result)
            transport.source_fd = result._leaf
            return result

        def check(held):
            if sources:
                checks.append(1)
                if failure == 'before-start' and len(checks) == 3:
                    raise JournalError('before start')
            return original_check(held)

        def connection(daemon, timeout):
            inner = original_connection(daemon, timeout)
            request = inner.request
            close = inner.close
            connected = [False]
            requested = [False]
            connect = inner.connect
            def connect_recorded():
                connected[0] = True
                connect()
            def request_recorded(method, path, body=None, headers=None):
                requested[0] = True
                match = re.fullmatch(r'/v1\.41/containers/([0-9a-f]{64})/start', path)
                if match:
                    if packet is not None:
                        with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                            sender.sendto(packet, self.path + '/' + match.group(1))
                    if failure == 'start-transport':
                        raise start_error
                    request(method, path, body=body, headers=headers)
                    inner.response = FakeResponse(None, status)
                    return
                if path == '/v1.44/info' and info_failure == 'transport':
                    raise OSError(errno.EIO, 'info')
                request(method, path, body=body, headers=headers)
                if path == '/v1.44/info' and info_failure == 'timer-fire':
                    callbacks[-1].callback()
                if path == '/v1.44/info' and info_failure == 'close':
                    inner.response.close = mock.Mock(side_effect=OSError(errno.EIO, 'response close'))
            def close_recorded():
                if connected[0] and not requested[0]:
                    transport.events.append(('attach-close',))
                    if expire_cleanup:
                        transport.now = 1100
                    if replace == 'attach':
                        raise replacement
                close()
            inner.connect, inner.request, inner.close = connect_recorded, request_recorded, close_recorded
            return inner

        def collect(receiver):
            transport.events.append(('collect',))
            return original_collect(receiver)

        def close_diagnostic(receiver):
            transport.events.append(('diagnostic-close',))
            original_close(receiver)
            if replace == 'diagnostic':
                raise replacement

        def observe(daemon, deadline):
            transport.live()
            transport.events.append(('observe',))
            transport.active_exception = sys.exc_info()[1]
            observed_deadlines.append(deadline)
            if info_failure == 'observer':
                raise OSError(errno.EIO, 'observer')
            return original_observe(daemon, deadline)

        with mock.patch.object(bootstrap, '_OwnerProbeSource', side_effect=source), \
             mock.patch.object(original_source, 'check', check), \
             mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: transport.now), \
             mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connection), \
             mock.patch.object(bootstrap.threading, 'Timer', Timer), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'collect', collect), \
             mock.patch.object(bootstrap._OwnerProbeStartDiagnostic, 'close', close_diagnostic), \
             mock.patch.object(bootstrap, '_observe_docker_runtime_info', side_effect=observe):
            try:
                result = bootstrap._owner_probe_pair({}, transport, observe_owner_start=True)
            except BaseException as exc:
                result = exc
        return result, transport, replacement, start_error, callbacks, observed_deadlines, sources

    def test_eligible_failure_observes_after_cleanup(self):
        exc, transport, _, _, _, deadlines, sources = self.exercise()
        self.assertIsInstance(exc, JournalError)
        self.assertIs(transport.active_exception, exc)
        self.assertEqual(exc._g2_owner_probe_start_diagnostic['response'], 'RESPONSE_STATUS')
        self.assertEqual(exc._g2_owner_probe_start_diagnostic['http_status'], 400)
        self.assertEqual(exc._g2_docker_runtime_info_diagnostic,
                         {'collection': 'OBSERVED', 'features': 'RRO_PRESENT', 'reason': 'OK'})
        self.assertEqual(deadlines, [1060])
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1060)])
        events = transport.events
        for first, second in ((('force-delete', 1), ('settle-failed', 1)),
                              (('settle-failed', 1), ('collect',)), (('collect',), ('diagnostic-close',)),
                              (('diagnostic-close',), ('observe',)), (('observe',), ('info', 1))):
            self.assertLess(events.index(first), events.index(second))
        self.assertEqual(sum(event[0] == 'info' for event in events), 1)
        self.assertIsNone(sources[0]._leaf)
        self.assertTrue(os.path.isdir(SOURCE))

    def test_info_failure_preserves_start_exception(self):
        for failure in ('transport', 'observer'):
            with self.subTest(failure=failure):
                exc, transport, _, _, _, _, _ = self.exercise(info_failure=failure)
                self.assertIsInstance(exc, JournalError)
                self.assertEqual(exc._g2_owner_probe_start_diagnostic['http_status'], 400)
                if failure == 'transport':
                    self.assertEqual(exc._g2_docker_runtime_info_diagnostic,
                                     {'collection': 'UNAVAILABLE', 'features': None, 'reason': 'TRANSPORT'})
                else:
                    self.assertFalse(hasattr(exc, '_g2_docker_runtime_info_diagnostic'))
                self.assertIn(('settle-failed', 1), transport.events)
                remove_owned(SOURCE)

    def test_cleanup_extension_does_not_extend_info(self):
        exc, transport, _, _, _, deadlines, _ = self.exercise(expire_cleanup=True)
        self.assertIsInstance(exc, JournalError)
        self.assertEqual(deadlines, [1060])
        self.assertEqual(transport.settlement_deadlines, [('failed', 1, 1110)])
        self.assertEqual(exc._g2_docker_runtime_info_diagnostic,
                         {'collection': 'UNAVAILABLE', 'features': None, 'reason': 'DEADLINE'})
        self.assertFalse(any(event[0] == 'info' for event in transport.events))

    def test_success_and_noneligible_skip_info(self):
        cases = ((204, None, None), (400, None, None), (500, bytes((2, 188, 0, 0, 11, 12, 13, 6)), None),
                 (400, bytes((2, 39, 0, 0, 0, 0, 0, 0)), None),
                 (400, bytes((2, 188, 0, 0, 1, 12, 13, 6)), None),
                 (400, bytes((2, 188, 0, 0, 11, 13, 12, 6)), None),
                 (400, bytes((2, 188, 0, 0, 11, 12, 13, 6)), 'start-transport'),
                 (400, None, 'admit-1'), (400, None, 'before-start'))
        for status, packet, failure in cases:
            with self.subTest(status=status, packet=packet, failure=failure):
                exc, transport, _, _, _, _, _ = self.exercise(status=status, packet=packet, failure=failure)
                self.assertFalse(any(event[0] in ('observe', 'info') for event in transport.events))
                if status == 204 and failure is None:
                    self.assertEqual(exc, [999, 999])
                else:
                    self.assertIsInstance(exc, BaseException)
                remove_owned(SOURCE)

    def test_cleanup_replacement_skips_info(self):
        for replacement in ('attach', 'diagnostic'):
            with self.subTest(replacement=replacement):
                exc, transport, expected, _, _, _, _ = self.exercise(replace=replacement)
                self.assertIs(exc, expected)
                self.assertFalse(any(event[0] in ('observe', 'info') for event in transport.events))
                remove_owned(SOURCE)

    def test_normalization_and_close_faults_do_not_query(self):
        for side_effect in (ValueError('invalid'), None):
            with self.subTest(side_effect=side_effect):
                with mock.patch.object(bootstrap, '_owner_probe_diagnostic_value', side_effect=side_effect,
                                       return_value=None):
                    exc, transport, _, _, _, _, _ = self.exercise()
                self.assertIsInstance(exc, JournalError)
                self.assertFalse(any(event[0] in ('observe', 'info') for event in transport.events))
                remove_owned(SOURCE)
        exc, transport, _, _, _, _, _ = self.exercise(info_failure='close')
        self.assertIsInstance(exc, JournalError)
        self.assertEqual(exc._g2_docker_runtime_info_diagnostic,
                         {'collection': 'UNAVAILABLE', 'features': None, 'reason': 'TRANSPORT'})
        self.assertIn(('settle-failed', 1), transport.events)

    def test_preflight_info_independent_omission(self):
        exc = JournalError('original')
        owner = PreflightFailureDiagnosticTests.owner()
        owner.update(response='RESPONSE_STATUS', http_status=400)
        exc._g2_owner_probe_start_diagnostic = owner
        info = {'collection': 'OBSERVED', 'features': 'RRO_PRESENT', 'reason': 'OK'}
        exc._g2_docker_runtime_info_diagnostic = info
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(set(record), {'version', 'exception', 'owner_probe', 'docker_runtime_info', 'start_error', 'settlement'})
        self.assertEqual(record['version'], 6)
        self.assertIsNone(record['start_error'])
        self.assertEqual(record['docker_runtime_info'], info)
        self.assertIsNot(record['docker_runtime_info'], info)
        for value in (None, {}, {'collection': 'OBSERVED', 'features': 'other', 'reason': 'OK'}):
            exc._g2_docker_runtime_info_diagnostic = value
            record = bootstrap._preflight_failure_diagnostic(exc)
            self.assertIsNotNone(record['owner_probe'])
            self.assertIsNone(record['docker_runtime_info'])
        exc._g2_docker_runtime_info_diagnostic = info
        with mock.patch.object(bootstrap, '_docker_runtime_info_diagnostic_value', side_effect=OSError('failure')):
            self.assertIsNone(bootstrap._preflight_failure_diagnostic(exc)['docker_runtime_info'])
        owner['http_status'] = 500
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertIsNotNone(record['owner_probe'])
        self.assertIsNone(record['docker_runtime_info'])

    def test_timer_does_not_escape_probe_cleanup(self):
        fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        try:
            exc, transport, _, _, timers, _, _ = self.exercise(info_failure='timer-fire')
            self.assertIsInstance(exc, JournalError)
            self.assertEqual(len(timers), 1)
            self.assertIn(('timer-cancel',), transport.events)
            self.assertLess(transport.events.index(('abort', 1)), transport.events.index(('timer-cancel',)))
            os.fstat(fd)
            timers[0].callback()
            os.fstat(fd)
            self.assertEqual(sum(event[0] == 'abort' for event in transport.events), 2)
        finally:
            os.close(fd)


class OwnerProbeSettlementDiagnosticTests(SourceFixture):
    points = ('EFFECT_OPERATION', 'ALIAS_EXECUTION', 'OBJECT_CLEANUP_STARTED',
              'CGROUP_TARGET_UNOBSERVED', 'CGROUP_RETIRE_EAGAIN', 'CGROUP_TARGET_RELEASE',
              'INVOCATION', 'EXEC_ADMISSION', 'TRANSACTION_RETIREMENT', 'OBJECT_REMOVAL',
              'ATTACHMENT_RELEASE', 'OWNER_PROBE_RESULT', 'AUDIT_JOIN')

    def setUp(self):
        super().setUp()
        self.path = '/run/g2/owner-probe-settlement-diagnostic'
        self.assertFalse(os.path.lexists(self.path))
        self.addCleanup(remove_owned, self.path)
        self.cid = 'a' * 64

    def receiver(self, purpose='postgres-uid'):
        if not hasattr(self, '_receiver_source'):
            self._receiver_source = self.source()
        receiver = bootstrap._OwnerProbeSettlementDiagnostic(self._receiver_source, self.cid, purpose)
        self.addCleanup(receiver.close)
        return receiver

    def send(self, packet, ancillary=()):
        with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
            sender.sendmsg([packet], list(ancillary), 0, self.path + '/' + self.cid)

    @staticmethod
    def packet(event, point=0, detail=(0, 0, 0, 0, 0, 0, 0), purpose=1):
        return bytes((7, 1, purpose, event, point, *detail, 0, 0, 0, 0))

    def test_settlement_packets_and_missing_events(self):
        receiver = self.receiver()
        for mask in range(16):
            for event in range(1, 5):
                if mask & (1 << (event - 1)):
                    self.send(self.packet(event, 13 if event == 2 else 0))
            value = receiver.collect()
            self.assertEqual(value, {'purpose': 'postgres-uid', 'collection': 'OBSERVED' if mask else 'EMPTY',
                                    'started': bool(mask & 1), 'wait': 'AUDIT_JOIN' if mask & 2 else None,
                                    'ack_committed': bool(mask & 4), 'ack_sent': bool(mask & 8), 'removal': None, 'followup': None})
        for point, name in enumerate(self.points, 1):
            self.send(self.packet(2, point))
            self.assertEqual(receiver.collect()['wait'], name)
        receiver.close()
        receiver = self.receiver('postgres-gid')
        self.send(self.packet(4, purpose=2))
        self.assertTrue(receiver.collect()['ack_sent'])

    def test_settlement_invalid_packets_and_rights(self):
        receiver = self.receiver()
        valid = self.packet(1)
        for packet in (b'', valid[:15], valid + b'x', bytes((5, 1, 1, 1, 0, 0, 0, 0)),
                       bytes((5, 1, 1, 1, 0, *([0] * 11))),
                       bytes((6, 2, 1, 1, 0, *([0] * 11))), self.packet(1, purpose=2),
                       self.packet(0), self.packet(5), self.packet(1, 1), self.packet(2, 0),
                       self.packet(2, 14), self.packet(1, detail=(1, 0, 0, 0, 0, 0, 0)),
                       valid[:-1] + b'\x01'):
            with self.subTest(packet=packet):
                self.send(packet)
                self.assertEqual(receiver.collect()['collection'], 'INVALID')
        self.send(valid)
        self.send(valid)
        self.assertEqual(receiver.collect()['collection'], 'INVALID')
        fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        self.addCleanup(os.close, fd)
        before = set(os.listdir('/proc/self/fd'))
        for event in range(1, 5):
            self.send(self.packet(event, 1 if event == 2 else 0))
        self.send(valid, ((socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', fd)),))
        value = receiver.collect()
        self.assertEqual(value, {'purpose': 'postgres-uid', 'collection': 'INVALID', 'started': False,
                                 'wait': None, 'ack_committed': False, 'ack_sent': False, 'removal': None, 'followup': None})
        self.assertEqual(set(os.listdir('/proc/self/fd')), before)
        delivered = os.dup(fd)
        with mock.patch.object(receiver, 'sock') as sock:
            sock.recvmsg.side_effect = [(valid, [(socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', delivered))], socket.MSG_CTRUNC, None), BlockingIOError()]
            self.assertEqual(receiver.collect()['collection'], 'INVALID')
        with self.assertRaises(OSError) as closed:
            os.fstat(delivered)
        self.assertEqual(closed.exception.errno, errno.EBADF)
        os.fstat(fd)

    def test_settlement_custody_and_close_replacement(self):
        receiver = self.receiver()
        moved = self.path + '.held'
        self.addCleanup(remove_owned, moved)
        os.rename(self.path, moved)
        os.mkdir(self.path, 0o700)
        self.assertEqual(receiver.collect()['collection'], 'INVALID')
        replacement = os.stat(self.path)
        receiver.close()
        self.assertEqual(os.stat(self.path).st_ino, replacement.st_ino)
        self.assertTrue(os.path.exists(moved))

    def exercise(self, outcome='timeout', fault=None, observed=True, role='BOOT_OWNER', detail=None,
                 hard_deadline=None, mutate_deadline=False, evidence_channel=None, evidence_list=None):
        class RoleFaultTransport(ProbeTransport):
            @property
            def role(inner):
                raise OSError(errno.EIO, 'role')
        transport = RoleFaultTransport() if role == 'throwing' else ProbeTransport()
        if role not in (None, 'throwing'):
            transport.role = role
        if hard_deadline is not None:
            transport._deadline = hard_deadline
        source = self.source()
        transport.sources = [source]
        transport.source_fd = source._leaf
        transport.constructing = False
        original = bootstrap._OwnerProbeSettlementDiagnostic
        events, armed = [], []
        class AttributeFault(TimeoutError):
            def __setattr__(inner, name, value):
                if name == '_g2_owner_probe_settlement_diagnostic':
                    raise OSError(errno.EIO, 'attribute')
                super().__setattr__(name, value)
        failure = AttributeFault('same settlement failure') if fault == 'attribute' else TimeoutError('same settlement failure')
        replacement = OSError(errno.EIO, 'replacement')
        def settle(cid, state, deadline):
            transport.live()
            transport.settle_container_original(cid, state, deadline)
            events.append(('settle', state, deadline))
            if mutate_deadline:
                transport._deadline = 1000.0
            if observed and role == 'BOOT_OWNER' and fault != 'construct':
                with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                    sender.sendto(self.packet(2, 10 if detail is not None else 6,
                                              detail if detail is not None else (0, 0, 0, 0, 0, 0, 0)), self.path + '/' + cid)
            if outcome == 'timeout':
                raise failure
            return 'pending' if outcome == 'pending' else 'retired'
        transport.settle_container_original = transport.settle_container
        transport.settle_container = settle
        class Receiver(original):
            def __init__(inner, *args):
                events.append(('arm',))
                if fault == 'construct':
                    raise OSError(errno.EIO, 'arm')
                super().__init__(*args)
                armed.append(inner)
            def collect(inner, hard_deadline=None):
                os.fstat(inner.source._leaf)
                events.append(('collect',))
                if fault == 'collect':
                    raise OSError(errno.EIO, 'collect')
                return super().collect(hard_deadline)
            def close(inner):
                events.append(('close',))
                super().close()
                if fault == 'close':
                    raise replacement
        connection = transport.connection
        def connect(*args):
            value = connection(*args)
            prior = value.close
            def close():
                prior()
                if value.sock.parts == []:
                    events.append(('attach-close',))
                    if fault == 'attach-close':
                        raise replacement
            value.close = close
            return value
        escaped = None
        original_check = source.check
        checks = [0]
        def checked():
            checks[0] += 1
            if fault == 'source-after' and checks[0] == 4:
                raise JournalError('source check after successful settlement')
            return original_check()
        with mock.patch.object(guest_postgres, '_UnixHTTP', side_effect=connect), \
             mock.patch.object(bootstrap.time, 'monotonic', return_value=1000), \
             mock.patch.object(bootstrap, '_OwnerProbeSettlementDiagnostic', Receiver), \
             mock.patch.object(source, 'check', side_effect=checked):
            try:
                bootstrap._owner_probe({}, transport, '-u', source, observe_owner_start=observed,
                                       settlement_evidence=evidence_list, evidence_channel=evidence_channel)
            except BaseException as exc:
                escaped = exc
        self.assertEqual(transport.settlement_deadlines, [('completed', 1, 1060)])
        self.assertEqual(sum(e[0] == 'delete' for e in transport.events), 1)
        self.assertFalse(any(e[0] in ('force-delete', 'settle-failed') for e in transport.events))
        source.check()
        source.close()
        if escaped is not None and fault not in ('attach-close', 'source-after'):
            self.assertIs(escaped, failure if outcome == 'timeout' else escaped)
        return escaped, failure, replacement, events, armed

    def test_new_evidence_stays_with_the_escaping_exception(self):
        class Channel:
            def __init__(self):
                self.guest = {}
            def bind(self, cid, start, deadline):
                pass
            def collect(self):
                return {'purpose': 'postgres-uid', 'nonce': 'ab' * 16, 'collection': 'EMPTY',
                        'guest': {'operation_start_ns': 0, 'operation_deadline_ns': 0,
                                  'authority_deadline_ns': 0, 'ack_received_ns': 0},
                        'snapshots': [], 'reference_holder': 'UNRESOLVED'}
            def close(self):
                pass
        values = []
        exc, failure, _, _, _ = self.exercise(evidence_channel=Channel(), evidence_list=values)
        self.assertIs(exc, failure)
        self.assertEqual(values, [failure._g2_settlement_evidence_v1])
        remove_owned(SOURCE)
        values = []
        exc, failure, replacement, _, _ = self.exercise(fault='attach-close', evidence_channel=Channel(),
                                                         evidence_list=values)
        self.assertIs(exc, replacement)
        self.assertFalse(hasattr(replacement, '_g2_settlement_evidence_v1'))
        self.assertFalse(hasattr(failure, '_g2_settlement_evidence_v1'))
        self.assertEqual(len(values), 1)

    def test_timeout_collects_final_from_independent_seqpacket(self):
        from source_settlement_evidence_test import snapshot
        sender, receiver = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        self.addCleanup(sender.close)
        self.addCleanup(receiver.close)
        receiver.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        receiver.setblocking(False)
        channel = bootstrap._OwnerSettlementEvidenceChannel.__new__(bootstrap._OwnerSettlementEvidenceChannel)
        channel.sock, channel.peer = receiver, (os.getpid(), os.getuid(), os.getgid())
        channel.purpose, channel.nonce = 'postgres-uid', '03' * 16
        channel.boot, channel.epoch, channel.principal = '01' * 16, '02' * 16, 3
        channel.container_id, channel.hard_deadline = None, 0.0
        channel.armed, channel.invalid = True, False
        channel.guest = {'operation_start_ns': 0, 'operation_deadline_ns': 0,
                         'authority_deadline_ns': 0, 'ack_received_ns': 0}
        for phase in (1, 3):
            raw = bytearray(snapshot(phase))
            struct.pack_into('<3Q', raw, 16 + 10 * 8, 0, 0, 0)
            if phase == 3:
                struct.pack_into('<2Q', raw, 16 + 13 * 8, 1060 * 1000000000, 1060 * 1000000000 + 1)
            sender.send(raw)
        values = []
        exc, failure, _, _, _ = self.exercise(evidence_channel=channel, evidence_list=values)
        self.assertIs(exc, failure)
        self.assertEqual(values[0]['collection'], 'COMPLETE')
        self.assertEqual(values[0], failure._g2_settlement_evidence_v1)
        self.assertEqual(values[0]['guest']['ack_received_ns'], 0)
        self.assertEqual(len(values[0]['snapshots']), 2)

    def test_settlement_failure_same_exception_and_order(self):
        exc, failure, _, events, armed = self.exercise()
        self.assertIs(exc, failure)
        self.assertEqual(len(armed), 1)
        self.assertLess(events.index(('arm',)), events.index(('settle', 'completed', 1060)))
        self.assertLess(events.index(('settle', 'completed', 1060)), events.index(('collect',)))
        value = exc._g2_owner_probe_settlement_diagnostic
        self.assertEqual(value['wait'], 'CGROUP_TARGET_RELEASE')
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['version'], 6)
        self.assertEqual(record['settlement'], value)
        self.assertIsNone(record['owner_probe'])

    def test_settlement_success_and_optional_faults(self):
        for outcome, fault, role, observed in (
                ('retired', None, 'BOOT_OWNER', True), ('timeout', 'construct', 'BOOT_OWNER', True),
                ('timeout', 'collect', 'BOOT_OWNER', True), ('timeout', 'close', 'BOOT_OWNER', True),
                ('pending', None, 'BOOT_OWNER', True), ('timeout', None, 'OTHER', True),
                ('timeout', None, None, True), ('timeout', None, 'BOOT_OWNER', False)):
            with self.subTest(outcome=outcome, fault=fault, role=role, observed=observed):
                exc, failure, _, events, armed = self.exercise(outcome, fault, observed, role)
                if outcome == 'retired':
                    self.assertIsNone(exc)
                    self.assertNotIn(('collect',), events)
                else:
                    self.assertIsInstance(exc, TimeoutError if outcome == 'timeout' else JournalError)
                    if observed:
                        value = bootstrap._owner_probe_settlement_value(getattr(exc, '_g2_owner_probe_settlement_diagnostic', None))
                        self.assertIsNotNone(value)
                        self.assertEqual(value['collection'], 'UNAVAILABLE' if fault == 'construct' or role != 'BOOT_OWNER' else 'INVALID' if fault == 'collect' else 'OBSERVED')
                if not observed or role != 'BOOT_OWNER':
                    self.assertFalse(armed)
                remove_owned(SOURCE)

    def test_settlement_normalizer_and_mutual_exclusion(self):
        value = {'purpose': 'postgres-uid', 'collection': 'OBSERVED', 'started': False,
                 'wait': None, 'ack_committed': False, 'ack_sent': True, 'removal': None, 'followup': None}
        normalized = bootstrap._owner_probe_settlement_value(value)
        self.assertEqual(normalized, value)
        self.assertIsNot(normalized, value)
        for key, bad in (('purpose', 'OTHER'), ('collection', 'EMPTY'), ('started', 1), ('wait', 'UNKNOWN')):
            invalid = dict(value, **{key: bad})
            self.assertIsNone(bootstrap._owner_probe_settlement_value(invalid))
        exc = TimeoutError()
        exc._g2_owner_probe_settlement_diagnostic = value
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['settlement'], value)
        owner = {'purpose': 'postgres-uid', 'response': 'OTHER', 'http_status': None,
                 'collection': 'EMPTY', 'wrapper': None, 'broker': None, 'runc_exec_observed': False,
                 'mount_options': None, 'create_history': None}
        exc._g2_owner_probe_start_diagnostic = owner
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertIsNone(record['settlement'])
        self.assertEqual(record['owner_probe'], owner)

    def test_settlement_replacement_and_late_source_failure(self):
        exc, failure, replacement, events, armed = self.exercise(fault='attach-close')
        self.assertIs(exc, replacement)
        self.assertIs(exc.__context__, failure)
        self.assertFalse(hasattr(exc, '_g2_owner_probe_settlement_diagnostic'))
        self.assertNotIn(('collect',), events)
        self.assertTrue(all(item.sock is None for item in armed))
        remove_owned(SOURCE)
        exc, _, _, events, armed = self.exercise(outcome='retired', fault='source-after')
        self.assertIsInstance(exc, JournalError)
        self.assertFalse(hasattr(exc, '_g2_owner_probe_settlement_diagnostic'))
        self.assertNotIn(('collect',), events)

    def test_settlement_attribute_and_role_read_failure(self):
        exc, failure, _, _, armed = self.exercise(fault='attribute')
        self.assertIs(exc, failure)
        self.assertFalse(hasattr(exc, '_g2_owner_probe_settlement_diagnostic'))
        self.assertTrue(all(item.sock is None for item in armed))
        remove_owned(SOURCE)
        exc, failure, _, events, armed = self.exercise(role='throwing')
        self.assertIs(exc, failure)
        self.assertFalse(armed)
        self.assertNotIn(('arm',), events)
        self.assertEqual(exc._g2_owner_probe_settlement_diagnostic['collection'], 'UNAVAILABLE')

    def test_removal_snapshot_values(self):
        receiver = self.receiver()
        base = [2, 1, 1, 4, 2, 4, 2]
        names = (
            ('object', ('MISSING', 'MAP', 'PROGRAM', 'OTHER_CLASS', 'IDENTITY_MISMATCH')),
            ('held_fd', ('ABSENT', 'PRESENT', 'INVALID')),
            ('grant_cleanup', ('ABSENT', 'ZERO_PENDING', 'ZERO_REMOVED', 'NEGATIVE_PENDING',
                               'NEGATIVE_REMOVED', 'OTHER_RESULT', 'IDENTITY_MISMATCH')),
            ('creator', ('UNAVAILABLE', 'MISSING', 'IDENTITY_MISMATCH', 'PREPARED', 'COMMITTED',
                         'EFFECT_OBSERVED', 'RESPONSE_SETTLED', 'RETIRED', 'OTHER_STATE')),
            ('creation_cleanup', ('UNAVAILABLE', 'FLAG_CLEAR', 'FLAG_SET')),
            ('lifecycle', ('MISSING', 'UNAVAILABLE', 'IDENTITY_MISMATCH', 'PENDING_UNAUTHORIZED',
                           'PENDING_AUTHORIZED', 'REMOVED_UNAUTHORIZED', 'REMOVED_AUTHORIZED')),
            ('removal_authorized', (None, False, True)),
        )
        for index, (field, choices) in enumerate(names):
            for code, expected in enumerate(choices, 1):
                detail = list(base)
                detail[index] = code
                if index == 0 and code in (1, 5):
                    detail[3], detail[4], detail[5], detail[6] = 1, 1, 1, 1
                elif index == 3 and code <= 3:
                    detail[4] = 1
                elif index == 4 and code == 1:
                    detail[3] = 1
                elif index == 6 and code == 1:
                    detail[0], detail[3], detail[4], detail[5] = 1, 1, 1, 1
                with self.subTest(field=field, code=code):
                    self.send(self.packet(2, 10, detail))
                    value = receiver.collect()
                    self.assertEqual(value['collection'], 'OBSERVED')
                    self.assertEqual(value['wait'], 'OBJECT_REMOVAL')
                    self.assertEqual(value['removal'][field], expected)
                    self.assertEqual(set(value['removal']), {name for name, _ in names})
                    normalized = bootstrap._owner_probe_settlement_value(value)
                    self.assertEqual(normalized, value)
                    self.assertIsNot(normalized['removal'], value['removal'])
        self.send(self.packet(2, 10))
        self.assertIsNone(receiver.collect()['removal'])
        for event in (4, 2, 1, 3):
            self.send(self.packet(event, 10 if event == 2 else 0,
                                  base if event == 2 else (0, 0, 0, 0, 0, 0, 0)))
        combined = receiver.collect()
        self.assertTrue(combined['started'])
        self.assertTrue(combined['ack_committed'])
        self.assertTrue(combined['ack_sent'])
        self.assertEqual(combined['wait'], 'OBJECT_REMOVAL')
        self.assertEqual(combined['removal']['object'], 'MAP')
        receiver.close()
        self._receiver_source.close()
        remove_owned(SOURCE)
        detail = (3, 2, 3, 8, 3, 7, 3)
        exc, failure, _, events, armed = self.exercise(detail=detail)
        self.assertIs(exc, failure)
        self.assertLess(events.index(('settle', 'completed', 1060)), events.index(('collect',)))
        self.assertTrue(all(item.sock is None for item in armed))
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['version'], 6)
        self.assertEqual(record['settlement']['removal'], {
            'object': 'PROGRAM', 'held_fd': 'PRESENT', 'grant_cleanup': 'ZERO_REMOVED',
            'creator': 'RETIRED', 'creation_cleanup': 'FLAG_SET', 'lifecycle': 'REMOVED_AUTHORIZED',
            'removal_authorized': True})
        self.assertFalse(record['settlement']['ack_sent'])

    def test_removal_snapshot_rejections(self):
        receiver = self.receiver()
        base = (2, 1, 1, 4, 2, 4, 2)
        malformed = []
        for index, maximum in enumerate((5, 3, 7, 9, 3, 7, 3)):
            for code in (0, maximum + 1):
                detail = list(base)
                detail[index] = code
                malformed.append(self.packet(2, 10, detail))
        malformed.extend((self.packet(1, detail=base), self.packet(2, 9, base),
                          self.packet(2, 10, (1, 1, 1, 4, 2, 4, 2)),
                          self.packet(2, 10, (5, 1, 1, 1, 1, 4, 1)),
                          self.packet(2, 10, (2, 1, 1, 1, 2, 4, 2)),
                          self.packet(2, 10, (2, 1, 1, 4, 1, 4, 2))))
        for packet in malformed:
            with self.subTest(packet=packet):
                self.send(self.packet(1))
                self.send(packet)
                result = receiver.collect()
                self.assertEqual(result, {'purpose': 'postgres-uid', 'collection': 'INVALID',
                                          'started': False, 'wait': None, 'ack_committed': False,
                                          'ack_sent': False, 'removal': None, 'followup': None})
        good = {'object': 'MAP', 'held_fd': 'ABSENT', 'grant_cleanup': 'ABSENT',
                'creator': 'PREPARED', 'creation_cleanup': 'FLAG_CLEAR',
                'lifecycle': 'PENDING_UNAUTHORIZED', 'removal_authorized': False}
        settlement = {'purpose': 'postgres-uid', 'collection': 'OBSERVED', 'started': False,
                      'wait': 'OBJECT_REMOVAL', 'ack_committed': False, 'ack_sent': False, 'removal': good, 'followup': None}
        class Label(str):
            pass
        for invalid in (dict(good, removal_authorized=0), dict(good, object='UNKNOWN'),
                        dict(good, object=Label('MAP')), dict(good, creator='MISSING'),
                        dict(good, creation_cleanup='UNAVAILABLE'), dict(good, object='MISSING'),
                        dict(good, extra='secret'),
                        {key: value for key, value in good.items() if key != 'held_fd'}):
            with self.subTest(value=invalid):
                self.assertIsNone(bootstrap._owner_probe_settlement_value(dict(settlement, removal=invalid)))
        self.assertIsNone(bootstrap._owner_probe_settlement_value(dict(settlement, wait='INVOCATION')))
        self.assertIsNone(bootstrap._owner_probe_settlement_value(dict(settlement, collection='EMPTY')))
        self.assertIsNone(bootstrap._owner_probe_settlement_value({key: value for key, value in settlement.items() if key != 'removal'}))
        exc = TimeoutError('SECRET')
        exc._g2_owner_probe_settlement_diagnostic = dict(settlement, removal=dict(good, removal_authorized=1))
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(set(record), {'version', 'exception', 'owner_probe', 'docker_runtime_info', 'start_error', 'settlement'})
        self.assertEqual(record['version'], 6)
        self.assertIsNone(record['settlement'])
        self.assertEqual(record['exception']['kind'], 'OSERROR')
        self.assertNotIn('SECRET', json.dumps(record))
        exc._g2_owner_probe_settlement_diagnostic = settlement
        with mock.patch.object(bootstrap, 'canonical', side_effect=(b'x' * 2049, b'x' * 2048)):
            record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertIsNone(record['settlement'])
        self.assertEqual(record['exception']['kind'], 'OSERROR')

    def test_removal_followup_values(self):
        receiver = self.receiver()
        choices = (
            ('boundary', ('CONTROL_DETACHED', 'ACK_COMMITTED', 'PRINCIPAL_TERMINAL', 'BROKER_EXIT')),
            ('last_wait', self.points + ('UNLABELLED',)),
            ('identity', ('MATCH', 'MISSING', 'MISMATCH')),
            ('grant_close', ('NOT_OBSERVED', 'ZERO', 'NEGATIVE', 'OTHER_RESULT', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
            ('guard_removal', ('NOT_OBSERVED', 'OBSERVED', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
            ('tracker', ('PENDING', 'REMOVED', 'INCONSISTENT', 'UNAVAILABLE')),
            ('lifecycle', ('MISSING', 'UNAVAILABLE', 'IDENTITY_MISMATCH', 'PENDING_UNAUTHORIZED', 'PENDING_AUTHORIZED', 'REMOVED_UNAUTHORIZED', 'REMOVED_AUTHORIZED')),
            ('cleanup_record', ('NOT_OBSERVED', 'CLOSE_ONLY', 'CLOSE_AND_REMOVAL', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
            ('acceptance', ('UNKNOWN', 'ACCEPTED', 'INCONSISTENT')),
            ('attachments', ('NONE', 'ACTIVE_PRESENT', 'DETACHED_OR_OFFLINING_PRESENT', 'ALL_RELEASED', 'MIXED', 'INCONSISTENT', 'TRUNCATED', 'UNAVAILABLE')),
            ('program_type', ('NOT_PROGRAM', 'LSM', 'CGROUP_DEVICE', 'OTHER', 'UNAVAILABLE', 'INCONSISTENT')),
            ('creator_response', ('NOT_OBSERVED', 'ADDFD_SEND_OK', 'ADDFD_SEND_ENOENT', 'SEND_OR_ERROR', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE')),
        )
        base = [1, 10, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
        for index, (field, names) in enumerate(choices):
            for code, name in enumerate(names, 1):
                detail = base.copy()
                detail[index] = code
                if index == 0 and code == 2:
                    detail[1] = 0
                if index == 2 and code != 1:
                    detail[3:] = [0] * 9
                if index == 8 and code == 2:
                    detail[3:9] = [2, 2, 2, 7, 3, 2]
                    detail[11] = 2
                with self.subTest(field=field, code=code):
                    self.send(bytes((7, 1, 1, 5, *detail)))
                    result = receiver.collect()
                    self.assertEqual(result['collection'], 'OBSERVED')
                    self.assertEqual(result['followup'][field], name)
                    self.assertIsNone(result['wait'])
                    self.assertEqual(bootstrap._owner_probe_settlement_value(result), result)
                    self.assertIsNot(bootstrap._owner_probe_settlement_value(result)['followup'], result['followup'])
        self.send(bytes((7, 1, 1, 5, 1, 0, 0, *([0] * 9))))
        self.assertIsNone(receiver.collect()['followup']['identity'])
        for event in (1, 2, 3, 4):
            self.send(self.packet(event, 10 if event == 2 else 0))
        self.send(bytes((7, 1, 1, 5, *base)))
        value = receiver.collect()
        self.assertEqual(value['collection'], 'OBSERVED')
        self.assertEqual(value['wait'], 'OBJECT_REMOVAL')
        self.assertEqual(value['followup']['boundary'], 'CONTROL_DETACHED')

    def test_removal_followup_rejections(self):
        receiver = self.receiver()
        base = [1, 10, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
        packets = [bytes((6, 1, 1, 5, *base)), bytes((7, 1, 2, 5, *base)),
                   bytes((7, 1, 1, 5, 0, *base[1:])), bytes((7, 1, 1, 5, 2, 10, *base[2:])),
                   bytes((7, 1, 1, 5, 1, 10, 2, *base[3:])),
                   bytes((7, 1, 1, 5, 1, 10, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1))]
        maxima = (4, 14, 3, 8, 6, 4, 7, 7, 3, 8, 6, 8)
        for index, maximum in enumerate(maxima):
            detail = base.copy()
            detail[index] = maximum + 1
            packets.append(bytes((7, 1, 1, 5, *detail)))
        accepted = [1, 10, 1, 2, 2, 2, 7, 3, 2, 1, 1, 2]
        for attachment in range(1, 6):
            for program in range(1, 5):
                for response in range(2, 5):
                    detail = accepted.copy()
                    detail[9:12] = [attachment, program, response]
                    with self.subTest(attachment=attachment, program=program, response=response):
                        self.send(bytes((7, 1, 1, 5, *detail)))
                        self.assertEqual(receiver.collect()['followup']['acceptance'], 'ACCEPTED')
        for index, forbidden in ((9, (6, 7, 8)), (10, (5, 6)), (11, (1, 5, 6, 7, 8))):
            for code in forbidden:
                detail = accepted.copy()
                detail[index] = code
                packets.append(bytes((7, 1, 1, 5, *detail)))
        for packet in packets:
            with self.subTest(packet=packet):
                self.send(packet)
                result = receiver.collect()
                self.assertEqual(result['collection'], 'INVALID')
                self.assertIsNone(result['followup'])
        good = bytes((7, 1, 1, 5, *base))
        self.send(good)
        self.send(good)
        self.assertEqual(receiver.collect()['collection'], 'INVALID')
        fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        self.addCleanup(os.close, fd)
        before = set(os.listdir('/proc/self/fd'))
        for event in (1, 2, 3, 4):
            self.send(self.packet(event, 1 if event == 2 else 0))
        self.send(good)
        self.send(good, ((socket.SOL_SOCKET, socket.SCM_RIGHTS, struct.pack('=i', fd)),))
        self.assertEqual(receiver.collect()['collection'], 'INVALID')
        self.assertEqual(set(os.listdir('/proc/self/fd')), before)
        self.send(good)
        value = receiver.collect()
        followup = value['followup']
        accepted_value = dict(followup, grant_close='ZERO', guard_removal='OBSERVED', tracker='REMOVED',
                              lifecycle='REMOVED_AUTHORIZED', cleanup_record='CLOSE_AND_REMOVAL',
                              acceptance='ACCEPTED', attachments='NONE', program_type='CGROUP_DEVICE',
                              creator_response='ADDFD_SEND_OK')
        self.assertEqual(bootstrap._owner_probe_followup_value(accepted_value), accepted_value)
        for field, rejected in (
                ('attachments', ('INCONSISTENT', 'TRUNCATED', 'UNAVAILABLE')),
                ('program_type', ('UNAVAILABLE', 'INCONSISTENT')),
                ('creator_response', ('NOT_OBSERVED', 'INCONSISTENT', 'AMBIGUOUS', 'TRUNCATED', 'UNAVAILABLE'))):
            for item in rejected:
                with self.subTest(field=field, item=item):
                    incomplete = dict(accepted_value, **{field: item})
                    self.assertIsNone(bootstrap._owner_probe_followup_value(incomplete))
                    incomplete['acceptance'] = 'UNKNOWN'
                    self.assertEqual(bootstrap._owner_probe_followup_value(incomplete), incomplete)
        for bad in (dict(followup, identity='MISSING'), dict(followup, boundary='ACK_COMMITTED'),
                    dict(followup, acceptance='ACCEPTED'), dict(followup, tracker=True),
                    dict(followup, extra='SECRET'), {k: v for k, v in followup.items() if k != 'tracker'}):
            self.assertIsNone(bootstrap._owner_probe_settlement_value(dict(value, followup=bad)))
        self.assertIsNone(bootstrap._owner_probe_settlement_value(dict(value, collection='EMPTY')))
        self.assertIsNone(bootstrap._owner_probe_settlement_value({k: v for k, v in value.items() if k != 'followup'}))

    def test_settlement_followup_window(self):
        receiver = self.receiver()
        packet = bytes((7, 1, 1, 5, 1, 10, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1))
        clock = [10.0]
        waits = []
        def ready(readers, writers, errors, timeout):
            waits.append(timeout)
            clock[0] += 0.05
            self.send(packet)
            return readers, [], []
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: clock[0]), \
             mock.patch.object(bootstrap.select, 'select', side_effect=ready):
            result = receiver.collect(20.0)
        self.assertEqual(waits, [0.25])
        self.assertEqual(result['followup']['boundary'], 'CONTROL_DETACHED')
        self.send(packet)
        with mock.patch.object(bootstrap.time, 'monotonic', return_value=10.0), \
             mock.patch.object(bootstrap.select, 'select') as wait:
            self.assertIsNotNone(receiver.collect(20.0)['followup'])
            wait.assert_not_called()
        for deadline in (None, True, float('nan'), float('inf'), 9.0, 10.0):
            with self.subTest(deadline=deadline), mock.patch.object(bootstrap.time, 'monotonic', return_value=10.0), \
                 mock.patch.object(bootstrap.select, 'select') as wait:
                self.assertEqual(receiver.collect(deadline)['collection'], 'EMPTY')
                wait.assert_not_called()
        clock[0] = 10.0
        waits.clear()
        def interrupt(readers, writers, errors, timeout):
            waits.append(timeout)
            clock[0] += 0.03
            raise InterruptedError()
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: clock[0]), \
             mock.patch.object(bootstrap.select, 'select', side_effect=interrupt):
            self.assertEqual(receiver.collect(10.1)['collection'], 'EMPTY')
        self.assertLessEqual(clock[0], 10.12)
        self.assertTrue(all(a > b for a, b in zip(waits, waits[1:])))
        clock[0] = 10.0
        def late(readers, writers, errors, timeout):
            self.assertAlmostEqual(timeout, 0.1)
            clock[0] = 10.1
            self.send(packet)
            return readers, [], []
        with mock.patch.object(bootstrap.time, 'monotonic', side_effect=lambda: clock[0]), \
             mock.patch.object(bootstrap.select, 'select', side_effect=late):
            self.assertEqual(receiver.collect(10.1)['collection'], 'EMPTY')
        self.assertIsNotNone(receiver.collect()['followup'])
        with mock.patch.object(bootstrap.time, 'monotonic', return_value=10.0), \
             mock.patch.object(receiver, '_receive_packet', return_value=(b'bad', False)) as receive, \
             mock.patch.object(bootstrap.select, 'select') as wait:
            self.assertEqual(receiver.collect(20.0)['collection'], 'INVALID')
            self.assertEqual(receive.call_count, 6)
            wait.assert_not_called()
        self.send(packet)
        self.send(packet)
        with mock.patch.object(bootstrap.time, 'monotonic', return_value=10.0), \
             mock.patch.object(bootstrap.select, 'select') as wait:
            self.assertEqual(receiver.collect(20.0)['collection'], 'INVALID')
            wait.assert_not_called()

    def test_settlement_followup_cleanup(self):
        packet = bytes((7, 1, 1, 5, 1, 10, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1))
        calls = []
        def ready(readers, writers, errors, timeout):
            calls.append(timeout)
            with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as sender:
                sender.sendto(packet, self.path + '/' + ('%064x' % 1))
            return readers, [], []
        with mock.patch.object(bootstrap.select, 'select', side_effect=ready):
            exc, failure, _, events, armed = self.exercise(hard_deadline=1000.125, mutate_deadline=True)
        self.assertIs(exc, failure)
        self.assertEqual(calls, [0.125])
        self.assertEqual(exc._g2_owner_probe_settlement_diagnostic['followup']['boundary'], 'CONTROL_DETACHED')
        self.assertLess(events.index(('attach-close',)), events.index(('collect',)))
        self.assertLess(events.index(('settle', 'completed', 1060)), events.index(('collect',)))
        self.assertTrue(all(item.sock is None for item in armed))
        self.assertFalse(os.path.exists(self.path))
        record = bootstrap._preflight_failure_diagnostic(exc)
        self.assertEqual(record['version'], 6)
        self.assertEqual(record['settlement']['followup'], exc._g2_owner_probe_settlement_diagnostic['followup'])


class OwnerSettlementEvidenceChannelTests(unittest.TestCase):
    def test_missing_armed_reply_respects_authority_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            self.addCleanup(os.close, directory_fd)
            path = '/proc/self/fd/%d/broker.sock' % directory_fd
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            self.addCleanup(listener.close)
            listener.bind(path)
            listener.listen(1)
            release = threading.Event()
            received = []
            def broker():
                conn, _ = listener.accept()
                with conn:
                    received.append(conn.recv(512))
                    release.wait(1)
            thread = threading.Thread(target=broker, daemon=True)
            thread.start()
            start = time.monotonic()
            authority = SimpleNamespace(_peer=(os.getpid(), os.getuid(), os.getgid()),
                boot_id='01010101-0101-0101-0101-010101010101', epoch='02' * 16,
                principal_id=3, _deadline=start + .05)
            try:
                with mock.patch.object(bootstrap, 'OWNER_SETTLEMENT_SOCKET', path):
                    channel = bootstrap._OwnerSettlementEvidenceChannel(authority, 'postgres-gid')
                self.assertLess(time.monotonic() - start, .5)
                self.assertEqual(channel.collect()['collection'], 'UNAVAILABLE')
                channel.close()
            finally:
                release.set()
                thread.join(timeout=1)
            self.assertEqual(len(received), 1)
            self.assertEqual(json.loads(received[0])['purpose'], 'postgres-gid')

    def test_real_hello_and_bind(self):
        with tempfile.TemporaryDirectory() as directory:
            directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            self.addCleanup(os.close, directory_fd)
            path = '/proc/self/fd/%d/broker.sock' % directory_fd
            listener = socket.socket(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            self.addCleanup(listener.close)
            listener.bind(path)
            listener.listen(1)
            listener.settimeout(1)
            received = []
            def broker():
                conn, _ = listener.accept()
                with conn:
                    hello = json.loads(conn.recv(512))
                    received.append(hello)
                    conn.send(bootstrap.canonical({'version': 1, 'status': 'ARMED', 'nonce': hello['nonce']}))
                    received.append(json.loads(conn.recv(1024)))
            thread = threading.Thread(target=broker, daemon=True)
            thread.start()
            authority = SimpleNamespace(_peer=(os.getpid(), os.getuid(), os.getgid()),
                boot_id='01010101-0101-0101-0101-010101010101', epoch='02' * 16,
                principal_id=3, _deadline=time.monotonic() + 5)
            with mock.patch.object(bootstrap, 'OWNER_SETTLEMENT_SOCKET', path):
                channel = bootstrap._OwnerSettlementEvidenceChannel(authority, 'postgres-uid')
                self.assertTrue(channel.armed)
                start = time.monotonic()
                channel.bind('ab' * 32, start, start + 60)
                channel.close()
            thread.join(timeout=1)
            self.assertFalse(thread.is_alive())
            self.assertEqual(set(received[0]), {'version', 'purpose', 'nonce', 'principal_id'})
            self.assertEqual(received[1]['nonce'], received[0]['nonce'])
            self.assertEqual(received[1]['container_id'], 'ab' * 32)
            self.assertEqual(received[1]['operation_deadline_ns'], int((start + 60) * 1000000000))

    def channel(self):
        sender, receiver = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        self.addCleanup(sender.close)
        self.addCleanup(receiver.close)
        receiver.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        receiver.setblocking(False)
        item = bootstrap._OwnerSettlementEvidenceChannel.__new__(bootstrap._OwnerSettlementEvidenceChannel)
        item.sock, item.peer = receiver, (os.getpid(), os.getuid(), os.getgid())
        item.purpose, item.nonce = 'postgres-uid', '03' * 16
        item.boot, item.epoch, item.principal = '01' * 16, '02' * 16, 3
        item.container_id, item.hard_deadline = '00' * 32, 0.0
        item.armed, item.invalid = True, False
        item.guest = {'operation_start_ns': 10, 'operation_deadline_ns': 100,
                      'authority_deadline_ns': 1000, 'ack_received_ns': 0}
        return sender, item

    def test_seqpacket_rejects_rights_and_truncation(self):
        import array
        for rights in (True, False):
            sender, channel = self.channel()
            fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
            try:
                if rights:
                    sender.sendmsg([b'x'], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', [fd]))])
                else:
                    sender.send(b'x' * 5000)
                self.assertEqual(channel.collect()['collection'], 'INVALID')
            finally:
                os.close(fd)

    def test_fourth_packet_is_invalid(self):
        from source_settlement_evidence_test import snapshot
        sender, channel = self.channel()
        for phase in (1, 2, 3, 3):
            sender.send(snapshot(phase))
        value = channel.collect()
        self.assertEqual(value['collection'], 'INVALID')
        self.assertEqual(value['snapshots'], [])

    def test_final_without_wait_and_authority_socket_independent(self):
        from source_settlement_evidence_test import snapshot
        sender, channel = self.channel()
        sender.send(snapshot(1))
        sender.send(snapshot(3))
        sender.close()
        value = channel.collect()
        self.assertEqual(value['collection'], 'COMPLETE')
        self.assertEqual(len(value['snapshots']), 2)

    def test_wrong_peer_is_invalid(self):
        from source_settlement_evidence_test import snapshot
        sender, channel = self.channel()
        channel.peer = (os.getpid() + 1, os.getuid(), os.getgid())
        sender.send(snapshot(1))
        self.assertEqual(channel.collect()['collection'], 'INVALID')

    def test_registration_precedes_each_probe_and_failure_does_not_block(self):
        events = []
        source = mock.Mock()
        def register(authority, purpose):
            events.append(('register', purpose))
            if purpose == 'postgres-gid':
                raise OSError('unavailable')
            return object()
        def probe(daemon, authority, flag, held_source, **kwargs):
            events.append(('probe', flag, kwargs['evidence_channel'] is not None))
            return 999
        with mock.patch.object(bootstrap, '_OwnerProbeSource', return_value=source), \
             mock.patch.object(bootstrap, '_OwnerSettlementEvidenceChannel', side_effect=register), \
             mock.patch.object(bootstrap, '_owner_probe', side_effect=probe):
            self.assertEqual(bootstrap._owner_probe_pair({}, object(), observe_owner_start=True,
                                                        settlement_evidence=[]), [999, 999])
        self.assertEqual(events, [('register', 'postgres-uid'), ('probe', '-u', True),
                                  ('register', 'postgres-gid'), ('probe', '-g', False)])
