#!/usr/bin/env python3
"""Exercise the real realm framing and root-owned filesystem primitives.

The census/journal cases require root in a disposable guest or user namespace;
they create their own directories beneath --scratch and never use /var/lib/g2.
"""
import argparse
import array
import ctypes
import fcntl
import hashlib
import json
import os
import socket
import tempfile
import time

import census
import ipc
import journal
import maintenance
from journal import JournalError


def expect_failure(call, context=None):
    try:
        call()
    except (JournalError, census.CensusError, OSError, ValueError):
        return
    raise AssertionError('invalid operation was accepted' + (': ' + context if context else ''))


def protocol():
    left, right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    try:
        left.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        right.send(b'{"version":1,"op":"realm_observe","phase_generation":3,"nonce":"' + b'a' * 64 + b'"}')
        value, credentials = ipc._receive(left, time.monotonic() + 3)
        assert value['phase_generation'] == 3
        assert credentials == (os.getpid(), os.getuid(), os.getgid())
        seen = set()
        ipc._request_identity(value, seen)
        ipc._phase_matches(value, {'generation': 3, 'phase': 'quiescent'})
        expect_failure(lambda: ipc._request_identity(value, seen))
        expect_failure(lambda: ipc._phase_matches(value, {'generation': 4, 'phase': 'quiescent'}))
        expect_failure(lambda: ipc._phase_matches(value, {'generation': 3, 'phase': 'migration_callback'}))
        invalid = [dict(value, nonce='A' * 64), dict(value, nonce='0' * 63),
                   dict(value, phase_generation=0, nonce='b' * 64),
                   dict(value, phase_generation=True, nonce='c' * 64),
                   dict(value, op='other', nonce='d' * 64), dict(value, extra=1, nonce='e' * 64)]
        for request in invalid:
            right.send(json.dumps(request).encode('ascii'))
            received, peer = ipc._receive(left, time.monotonic() + 3)
            assert peer == (os.getpid(), os.getuid(), os.getgid())
            expect_failure(lambda: ipc._request_identity(received, seen))
        right.send(b'{"x":1,"x":2}')
        expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))
        read_fd, write_fd = os.pipe()
        try:
            right.sendmsg([b'{}'], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', [read_fd]))])
            expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))
        finally:
            os.close(read_fd)
            os.close(write_fd)
        right.send(b'x' * (ipc.MAX_FRAME + 1))
        expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))
        right.send(b'{}')
        channel = object.__new__(ipc.RealmChannel)
        channel.sock = left
        channel.coordinator = {'pid': os.getpid() if (os.getuid(), os.getgid()) != (10002, 10002)
                               else os.getpid() + 1000000}
        expect_failure(lambda: channel.serve_one(time.monotonic() + 3))
        expect_failure(lambda: ipc._receive(left, time.monotonic() - 1))
        right.close()
        expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))
    finally:
        left.close()
        right.close()


def maintenance_protocol():
    """Exercise real seqpacket framing and the checkpoint identity parser."""
    left, right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    try:
        left.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        request = {'version': 1, 'op': 'maintenance_checkpoint', 'family': 'migration',
                   'recovery_id': 'smoke-rid', 'application_digest': '1' * 64,
                   'phase_generation': 3, 'attempt': 0, 'kind': 'intent',
                   'record_sha256': '2' * 64, 'nonce': '3' * 64}

        def receive(value):
            right.send(json.dumps(value, separators=(',', ':')).encode('ascii'))
            result, peer = ipc._receive(left, time.monotonic() + 3)
            assert peer == (os.getpid(), os.getuid(), os.getgid())
            return result

        seen = set()
        received = receive(request)
        maintenance.checkpoint_request(received, 'smoke-rid', '1' * 64, seen)
        assert maintenance.checkpoint_name(received) == 'migration-intent-00000000000000000003.json'
        expect_failure(lambda: maintenance.checkpoint_request(receive(request), 'smoke-rid', '1' * 64, seen))
        invalid = [dict(request, version=True), dict(request, version=None),
                   dict(request, family='mutation'), dict(request, recovery_id='../other'),
                   dict(request, application_digest='0' * 64), dict(request, record_sha256='0' * 64),
                   dict(request, record_sha256='A' * 64), dict(request, phase_generation=True),
                   dict(request, phase_generation=-1), dict(request, phase_generation=1 << 63),
                   dict(request, attempt=True), dict(request, attempt=-1),
                   dict(request, attempt=1 << 64), dict(request, attempt=1),
                   dict(request, kind='observe-intent', attempt=0),
                   dict(request, kind='not-a-record'), dict(request, nonce=None),
                   dict(request, nonce='4' * 63), dict(request, unexpected=1)]
        for value in invalid:
            received = receive(value)
            expect_failure(lambda: maintenance.checkpoint_request(received, 'smoke-rid', '1' * 64, set()))
        observation = receive(dict(request, kind='observe-intent', attempt=1, nonce='4' * 64))
        maintenance.checkpoint_request(observation, 'smoke-rid', '1' * 64, set())
        assert maintenance.checkpoint_name(observation) == (
            'migration-observe-intent-00000000000000000003-00000000000000000001.json')
        backend = {'pid': 123, 'started': '2026-01-01T00:00:00Z', 'role': 'houfeng_admin'}
        for kind in ('begin-settled', 'ready-abandoned'):
            settlement = receive(dict(request, kind=kind, current_backend=backend, nonce='5' * 64))
            maintenance.checkpoint_request(settlement, 'smoke-rid', '1' * 64, set())
            assert maintenance.checkpoint_name(settlement) == 'migration-%s-00000000000000000003.json' % kind
            expect_failure(lambda: maintenance.checkpoint_request(receive(dict(request, kind=kind)),
                                                                   'smoke-rid', '1' * 64, set()))
            for invalid_backend in (None, dict(backend, pid=True), dict(backend, started='invalid'),
                                    dict(backend, role='admin;DROP'), dict(backend, extra=1)):
                expect_failure(lambda: maintenance.checkpoint_request(
                    receive(dict(request, kind=kind, current_backend=invalid_backend)), 'smoke-rid', '1' * 64, set()))
        expect_failure(lambda: maintenance.checkpoint_request(receive(dict(request, current_backend=backend)),
                                                               'smoke-rid', '1' * 64, set()))
        disposition = dict(request, op='maintenance_ready_disposition', kind='ready', nonce='6' * 64)
        seen_dispositions = set()
        maintenance.disposition_request(receive(disposition), 'smoke-rid', '1' * 64, seen_dispositions)
        expect_failure(lambda: maintenance.disposition_request(receive(disposition), 'smoke-rid', '1' * 64,
                                                                 seen_dispositions))
        for value in (dict(disposition, op='maintenance_checkpoint'), dict(disposition, family='mutation'),
                      dict(disposition, kind='normal'), dict(disposition, attempt=1),
                      dict(disposition, attempt=True), dict(disposition, phase_generation=True),
                      dict(disposition, phase_generation=1 << 63), dict(disposition, current_backend=backend),
                      dict(disposition, recovery_id='../other'), dict(disposition, application_digest='0' * 64),
                      dict(disposition, record_sha256='0' * 64), dict(disposition, nonce=None),
                      dict(disposition, unexpected=1)):
            expect_failure(lambda value=value: maintenance.disposition_request(
                receive(value), 'smoke-rid', '1' * 64, set()))
        expect_failure(lambda: maintenance.checkpoint_request(receive(disposition), 'smoke-rid', '1' * 64, set()))
        for body in (b'{"version":1,"version":1}', b'{"version":1} {}', b'x' * (ipc.MAX_FRAME + 1)):
            right.send(body)
            expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))

        read_fd, write_fd = os.pipe()
        try:
            right.sendmsg([json.dumps(request).encode('ascii')],
                          [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', [read_fd]))])
            os.close(read_fd)
            read_fd = -1
            expect_failure(lambda: ipc._receive(left, time.monotonic() + 3))
            # The receiver must close its copy of every rejected descriptor.
            expect_failure(lambda: os.write(write_fd, b'x'))
        finally:
            if read_fd >= 0:
                os.close(read_fd)
            os.close(write_fd)

        channel = object.__new__(ipc.MaintenanceChannel)
        channel.sock = left
        channel.coordinator = {'pid': os.getpid() if (os.getuid(), os.getgid()) != (10002, 10002)
                               else os.getpid() + 1000000}
        right.send(json.dumps(request).encode('ascii'))
        expect_failure(lambda: channel.serve_one(time.monotonic() + 3))

        # Construct the complete state object, while leaving the durable and
        # live-root behavior to the privileged ceremony and journal smokes.
        owner = object.__new__(maintenance.MaintenanceJournal)
        owner.guard, owner.cgroup, owner.keys = {'dev': 1, 'ino': 2}, {'path': '/scope', 'dev': 3, 'ino': 4}, {}
        directory = object.__new__(maintenance.RecordDirectory)
        app = {'ID': 'app', 'SystemIdentifier': '123', 'DatabaseName': 'houfeng', 'DatabaseOID': 1, 'Epoch': 1}
        state = maintenance.MigrationCheckpoint(owner, directory, app, 'smoke-rid', owner.guard, owner.cgroup,
                                               'a' * 64, lambda *args: None, lambda *args: None)
        state.generation = 3
        ready = {'kind': 'ready', 'attempt': 0, 'phase_generation': 3}
        expect_failure(lambda: state._validate_next(ready, {'intent_digest': '2' * 64}))
    finally:
        left.close()
        right.close()
    maintenance_disposition_unresolved()


def maintenance_disposition_unresolved():
    """Check classification and retained-prefix ordering with simulated scope observations."""
    from types import SimpleNamespace
    app = {'ID': 'app', 'SystemIdentifier': '123', 'DatabaseName': 'houfeng', 'DatabaseOID': 1, 'Epoch': 1}
    owner = object.__new__(maintenance.MaintenanceJournal)
    owner.guard, owner.cgroup = {'dev': 1, 'ino': 2}, {'path': '/scope', 'dev': 3, 'ino': 4}
    owner.keys, owner.entries = {}, []
    appended = []
    owner.append = lambda *args: (appended.append(args) or (2, '8' * 64))
    directory = object.__new__(maintenance.RecordDirectory)
    files = {}
    directory.read = lambda name: files[name]
    directory.inventory = lambda: ([{'name': name, 'sha256': hashlib.sha256(raw).hexdigest()}
                                    for name, raw in sorted(files.items())], {})
    state = maintenance.MigrationCheckpoint(owner, directory, app, 'smoke-rid', owner.guard, owner.cgroup,
                                           'a' * 64, lambda *args: None, lambda *args: None)
    original = {'pid': 100, 'starttime': 1, 'executable_sha256': 'b' * 64}
    replacement = {'pid': 200, 'starttime': 2, 'executable_sha256': 'b' * 64}
    state.generation, state.begin = 3, {'coordinator': original}
    state.coordinator, state.coordinators = replacement, [original, replacement]
    state.retired_coordinators[(100, 1)] = '9' * 64
    state.phase = {'phase': 'migration_callback', 'generation': 3}
    state._live = lambda: None
    state.continuity = SimpleNamespace(seen={}, directories={}, observe=lambda *args: None)
    intent_digest, preparation_digest = 'c' * 64, 'd' * 64
    ready_name = 'migration-ready-00000000000000000003.json'
    unresolved_name = 'migration-revocation-unresolved-00000000000000000003.json'
    ready = {'version': 1, 'phase_generation': 3, 'intent_digest': intent_digest,
             'child': {'pid': 101, 'starttime': 3, 'executable_sha256': 'a' * 64},
             'backend': {'pid': 301, 'started': '2026-01-01T00:00:00Z', 'role': 'migrator'}}
    files[ready_name] = journal.canonical(ready)
    files[unresolved_name] = journal.canonical({'version': 1, 'phase_generation': 3,
                                                'intent_digest': intent_digest, 'outcome': 'revocation_unresolved'})
    ready_digest = hashlib.sha256(files[ready_name]).hexdigest()
    observation = {'child': ready['child'], 'backend': ready['backend'], 'parent': original}
    observation_digest = hashlib.sha256(journal.canonical(observation)).hexdigest()
    owner.entries = [journal.canonical({'live_observation_digest': observation_digest})]
    ready_key = ('migration', 'smoke-rid', state.application_digest, 3, 0, 'ready')
    owner.keys[ready_key] = (ready_digest, ready_name, 1, '7' * 64)
    state.records = {('intent', 0): (intent_digest, {'migrator_role': 'migrator'}, 1, '1' * 64),
                     ('preparation', 0): (preparation_digest, {}, 1, '2' * 64),
                     ('phase', 3): ('3' * 64, {}, 1, '4' * 64),
                     ('ready', 0): (ready_digest, ready, 1, '7' * 64)}
    state.continuity.seen[ready_name] = ready_digest
    state.pending_ready_observations[(3, ready_digest)] = (observation_digest, observation)
    state.live_observations = [{'witness_sequence': 1, 'observation': observation}]
    request = {'version': 1, 'op': 'maintenance_ready_disposition', 'family': 'migration',
               'recovery_id': 'smoke-rid', 'application_digest': state.application_digest,
               'phase_generation': 3, 'attempt': 0, 'kind': 'ready', 'record_sha256': ready_digest,
               'nonce': '5' * 64}
    credentials = (replacement['pid'], 10002, 10002)
    original_scope = maintenance.network_verify.migration_scope
    maintenance.network_verify.migration_scope = lambda group: {'members': [replacement['pid']], 'populated': 1}
    try:
        result = state.ready_disposition(request, credentials, time.monotonic() + 3)
        assert result['disposition'] == 'witnessed' and result['ready_witness_digest'] == '7' * 64
        assert appended[-1][-1]['scope_retirement_digest'] == '9' * 64
        assert journal.strict_json(files[unresolved_name])['outcome'] == 'revocation_unresolved'
        unresolved_digest = hashlib.sha256(files[unresolved_name]).hexdigest()
        state.continuity.seen[unresolved_name] = unresolved_digest
        state.records[('revocation-unresolved', 0)] = (unresolved_digest,
            journal.strict_json(files[unresolved_name]), 2, '6' * 64)
        result = state.ready_disposition(dict(request, nonce='a' * 64), credentials, time.monotonic() + 3)
        assert result['disposition'] == 'witnessed'
        del state.records[('revocation-unresolved', 0)]
        del state.continuity.seen[unresolved_name]
        state.pending_ready_observations.clear()
        expect_failure(lambda: state.ready_disposition(dict(request, nonce='6' * 64), credentials,
                                                       time.monotonic() + 3))
        del owner.keys[ready_key]
        del state.records[('ready', 0)]
        state.continuity.seen.clear()
        expect_failure(lambda: state.ready_disposition(dict(request, nonce='7' * 64), credentials,
                                                       time.monotonic() + 3))
        assert len(appended) == 2 and unresolved_name in files
        del files[unresolved_name]
        result = state.ready_disposition(dict(request, nonce='8' * 64), credentials, time.monotonic() + 3)
        assert result['disposition'] == 'unwitnessed_retired' and result['ready_witness_digest'] is None
    finally:
        maintenance.network_verify.migration_scope = original_scope


def roots(scratch):
    if os.geteuid() != 0:
        raise JournalError('census smoke requires root in an isolated guest or user namespace')
    with tempfile.TemporaryDirectory(prefix='g2-roots-', dir=scratch) as directory:
        old_base, old_open = census.BASE, census._open_base
        census.BASE = directory
        census._open_base = lambda: os.open(directory, census.OPEN_DIR)
        try:
            actual = census.RootSet.create()
            try:
                first = actual.scan()
                assert first == actual.scan() and len(first) == 64 and first != '0' * 64
                path = os.path.join(directory, 'managed-backups', 'staging', '.hidden')
                fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
                os.close(fd)
                expect_failure(actual.scan)
                os.unlink(path)
                expect_failure(actual.scan)
            finally:
                actual.close()
        finally:
            census.BASE, census._open_base = old_base, old_open
    with tempfile.TemporaryDirectory(prefix='g2-roots-', dir=scratch) as directory:
        old_base, old_open = census.BASE, census._open_base
        census.BASE = directory
        census._open_base = lambda: os.open(directory, census.OPEN_DIR)
        try:
            actual = census.RootSet.create()
            try:
                path = os.path.join(directory, 'browser-profiles')
                os.rename(path, path + '.old')
                os.mkdir(path, 0o700)
                expect_failure(actual.scan)
            finally:
                actual.close()
        finally:
            census.BASE, census._open_base = old_base, old_open
    for kind in ('symlink', 'fifo', 'unreadable'):
        with tempfile.TemporaryDirectory(prefix='g2-roots-', dir=scratch) as directory:
            old_base, old_open = census.BASE, census._open_base
            census.BASE = directory
            census._open_base = lambda: os.open(directory, census.OPEN_DIR)
            try:
                actual = census.RootSet.create()
                try:
                    path = os.path.join(directory, 'processor-workspaces')
                    if kind == 'symlink':
                        os.rename(path, path + '.old')
                        os.symlink(path + '.old', path)
                    elif kind == 'fifo':
                        os.mkfifo(os.path.join(path, 'unexpected'), 0o600)
                    else:
                        os.chmod(path, 0)
                    expect_failure(actual.scan)
                    if kind == 'unreadable':
                        os.chmod(path, 0o700)
                    expect_failure(actual.scan)
                finally:
                    actual.close()
            finally:
                census.BASE, census._open_base = old_base, old_open


def root_mount_escape(scratch, original_fd):
    if os.geteuid() != 0 or original_fd is None or original_fd < 0:
        raise JournalError('mount smoke requires a held original mount namespace descriptor')
    # NS_GET_NSTYPE ensures a different arbitrary FD cannot satisfy this test.
    if fcntl.ioctl(original_fd, 0xB703) != 0x00020000:
        raise JournalError('original descriptor is not a mount namespace')
    current_fd = os.open('/proc/self/ns/mnt', os.O_RDONLY | os.O_CLOEXEC)
    try:
        original, current = os.fstat(original_fd), os.fstat(current_fd)
        if (original.st_dev, original.st_ino) == (current.st_dev, current.st_ino):
            raise JournalError('mount smoke requires a separate mount namespace')
    finally:
        os.close(current_fd)
    libc = ctypes.CDLL(None, use_errno=True)
    libc.mount.argtypes = (ctypes.c_char_p, ctypes.c_char_p, ctypes.c_char_p, ctypes.c_ulong, ctypes.c_void_p)
    libc.mount.restype = ctypes.c_int
    libc.umount2.argtypes = (ctypes.c_char_p, ctypes.c_int)
    libc.umount2.restype = ctypes.c_int
    with tempfile.TemporaryDirectory(prefix='g2-root-mount-', dir=scratch) as directory:
        base, replacement = os.path.join(directory, 'base'), os.path.join(directory, 'replacement')
        os.mkdir(base, 0o700)
        os.mkdir(replacement, 0o700)
        old_base, old_open = census.BASE, census._open_base
        census.BASE = base
        census._open_base = lambda: os.open(base, census.OPEN_DIR)
        mounted = False
        try:
            actual = census.RootSet.create()
            try:
                target = os.path.join(base, 'browser-profiles')
                if libc.mount(os.fsencode(replacement), os.fsencode(target), None, 4096, None) != 0:
                    error = ctypes.get_errno()
                    raise OSError(error, os.strerror(error))
                mounted = True
                expect_failure(actual.scan)
            finally:
                if mounted and libc.umount2(os.fsencode(target), 2) != 0:
                    error = ctypes.get_errno()
                    raise OSError(error, os.strerror(error))
                actual.close()
        finally:
            census.BASE, census._open_base = old_base, old_open


def durable_journal(scratch):
    if os.geteuid() != 0:
        raise JournalError('journal smoke requires root in an isolated guest or user namespace')
    with tempfile.TemporaryDirectory(prefix='g2-journal-', dir=scratch) as directory:
        old = journal.REALM_CONTROL
        journal.REALM_CONTROL = os.path.join(directory, 'control')
        try:
            actual = journal.RealmJournal.create()
            try:
                actual.genesis({'run_token': journal.RUN_TOKEN, 'boot_id': journal.boot_id(),
                                'supervisor_pid': os.getpid(), 'supervisor_starttime': journal.process_start(os.getpid())})
                first = actual.bind_application({'run_token': journal.RUN_TOKEN, 'fixture': 'isolated-smoke'})
                assert actual.realm_digest() == first and len(first) == 64
                path = os.path.join(journal.REALM_CONTROL, 'journal-00000000000000000001.json')
                with open(path, 'ab') as stream:
                    stream.write(b' ')
                expect_failure(actual.verify)
                expect_failure(actual.realm_digest)
            finally:
                actual.close()
            expect_failure(journal.RealmJournal.create)
        finally:
            journal.REALM_CONTROL = old

    def sized_facts(size, *, genesis):
        facts = {'run_token': journal.RUN_TOKEN}
        if genesis:
            facts.update(boot_id=journal.boot_id(), supervisor_pid=os.getpid(),
                         supervisor_starttime=journal.process_start(os.getpid()))
        facts['fixture'] = ''
        padding = size - len(journal.canonical(facts))
        assert padding >= 0
        facts['fixture'] = 'x' * padding
        assert len(journal.canonical(facts)) == size
        return facts

    def fresh(case):
        with tempfile.TemporaryDirectory(prefix='g2-journal-boundary-', dir=scratch) as directory:
            old = journal.REALM_CONTROL
            journal.REALM_CONTROL = os.path.join(directory, 'control')
            actual = None
            try:
                actual = journal.RealmJournal.create()
                case(actual)
            finally:
                if actual is not None:
                    actual.close()
                journal.REALM_CONTROL = old

    def accepted_genesis(size):
        def case(actual):
            facts = sized_facts(size, genesis=True)
            raw = journal.canonical(facts)
            actual.genesis(facts)
            assert journal._read_genesis(actual._fd) == raw
            event = journal.strict_json(journal._read(actual._fd, 'journal-00000000000000000001.json'))
            assert event['kind'] == 'genesis'
            assert event['fields'] == {'genesis_digest': hashlib.sha256(raw).hexdigest()}
            digest = actual.bind_application({'run_token': journal.RUN_TOKEN, 'fixture': 'isolated-smoke'})
            actual.verify()
            assert len(digest) == 64 and actual.realm_digest() == digest
        fresh(case)

    for size in (journal.MAX_FILE + 1, journal.MAX_GENESIS):
        accepted_genesis(size)

    def oversized_genesis_write(actual):
        try:
            actual.genesis(sized_facts(journal.MAX_GENESIS + 1, genesis=True))
        except JournalError as exc:
            assert str(exc) == 'realm control file exceeds limit'
        else:
            raise AssertionError('oversized genesis was accepted')
        assert not os.path.lexists(os.path.join(journal.REALM_CONTROL, 'realm-genesis.json'))
    fresh(oversized_genesis_write)

    def create_raw(actual, name, body):
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                     0o600, dir_fd=actual._fd)
        try:
            os.fchmod(fd, 0o600)
            remaining = memoryview(body)
            while remaining:
                count = os.write(fd, remaining)
                if count <= 0:
                    raise JournalError('smoke control file write failed')
                remaining = remaining[count:]
            os.fsync(fd)
        finally:
            os.close(fd)
        os.fsync(actual._fd)

    def oversized_genesis_read(actual):
        create_raw(actual, 'realm-genesis.json', b'x' * (journal.MAX_GENESIS + 1))
        expect_failure(lambda: journal._read_genesis(actual._fd))
    fresh(oversized_genesis_read)

    for name in ('realm-app.json', 'control.json'):
        def generic_limit(actual, name=name):
            facts = sized_facts(journal.MAX_FILE, genesis=False)
            raw = journal.canonical(facts)
            if name == 'realm-app.json':
                actual.genesis(sized_facts(journal.MAX_FILE + 1, genesis=True))
                digest = actual.bind_application(facts)
                assert actual.realm_digest() == digest
            else:
                assert journal._write_new(actual._fd, name, facts) == raw
            assert journal._read(actual._fd, name) == raw
        fresh(generic_limit)

        def generic_oversized_write(actual, name=name):
            facts = sized_facts(journal.MAX_FILE + 1, genesis=False)
            if name == 'realm-app.json':
                actual.genesis(sized_facts(journal.MAX_FILE + 1, genesis=True))
                expect_failure(lambda: actual.bind_application(facts))
            else:
                expect_failure(lambda: journal._write_new(actual._fd, name, facts))
            assert not os.path.lexists(os.path.join(journal.REALM_CONTROL, name))
        fresh(generic_oversized_write)

        def generic_oversized_read(actual, name=name):
            create_raw(actual, name, b'x' * (journal.MAX_FILE + 1))
            expect_failure(lambda: journal._read(actual._fd, name))
        fresh(generic_oversized_read)

    for damage in ('symlink', 'hardlink', 'mode', 'changed'):
        def damaged_genesis(actual, damage=damage):
            actual.genesis(sized_facts(journal.MAX_FILE + 1, genesis=True))
            actual.bind_application({'run_token': journal.RUN_TOKEN, 'fixture': 'isolated-smoke'})
            path = os.path.join(journal.REALM_CONTROL, 'realm-genesis.json')
            if damage == 'symlink':
                os.unlink(path)
                os.symlink('realm-app.json', path)
            elif damage == 'hardlink':
                os.link(path, os.path.join(journal.REALM_CONTROL, 'genesis-alias'))
            elif damage == 'mode':
                os.chmod(path, 0o640)
            else:
                with open(path, 'r+b') as stream:
                    stream.write(b'[')
            expect_failure(actual.verify)
        fresh(damaged_genesis)


def _private_record(directory, name, body):
    path = os.path.join(directory, name)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchown(fd, 10002, 10002)
        os.fchmod(fd, 0o600)
        data = memoryview(body)
        while data:
            count = os.write(fd, data)
            if count <= 0:
                raise JournalError('smoke record write failed')
            data = data[count:]
        os.fsync(fd)
    finally:
        os.close(fd)
    parent = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    return path


def maintenance_records(scratch):
    """Exercise root reads of actual UID 10002 files, never a mock reader."""
    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('maintenance record smoke requires root in an isolated guest or user namespace')
    with tempfile.TemporaryDirectory(prefix='g2-maint-records-', dir=scratch) as directory:
        ledger = os.path.join(directory, 'record-ledger')
        os.mkdir(ledger, 0o700)
        os.chown(ledger, 10002, 10002)
        old_path = maintenance.LEDGER_DIR
        maintenance.LEDGER_DIR = ledger
        reader = None
        try:
            reader = maintenance.RecordDirectory()
            # Keep valid JSON byte-for-byte and test the exact 8 MiB bound.
            body = b'{"x":"' + b'x' * (maintenance.MAX_RECORD - 8) + b'"}'
            assert len(body) == maintenance.MAX_RECORD
            final = _private_record(ledger, 'limit.json', body)
            assert reader.read('limit.json') == body
            oversized = _private_record(ledger, 'oversized.json', body + b' ')
            expect_failure(lambda: reader.read('oversized.json'))
            os.unlink(oversized)
            empty = _private_record(ledger, 'empty.json', b'')
            expect_failure(lambda: reader.read('empty.json'))
            os.unlink(empty)

            temporary = os.path.join(ledger, '.tmp-' + 'a' * 32)
            os.link(final, temporary)
            assert reader.read('limit.json') == body
            third = os.path.join(ledger, '.tmp-' + 'b' * 32)
            os.link(final, third)
            expect_failure(lambda: reader.read('limit.json'))
            os.unlink(third)
            os.unlink(temporary)
            alias = os.path.join(ledger, 'alias.json')
            os.link(final, alias)
            expect_failure(lambda: reader.read('limit.json'))
            os.unlink(alias)
            assert reader.read('limit.json') == body

            os.unlink(final)
            os.symlink('/etc/passwd', final)
            expect_failure(lambda: reader.read('limit.json'))
            os.unlink(final)
            expect_failure(lambda: reader.read('limit.json'))
            os.rename(ledger, ledger + '.old')
            os.mkdir(ledger, 0o700)
            os.chown(ledger, 10002, 10002)
            expect_failure(reader.check)
        finally:
            if reader is not None:
                reader.close()
            maintenance.LEDGER_DIR = old_path

    for damage in ('missing', 'changed', 'pending-changed', 'wrong-hash'):
        with tempfile.TemporaryDirectory(prefix='g2-maint-prefix-', dir=scratch) as directory:
            ledger = os.path.join(directory, 'record-ledger')
            os.mkdir(ledger, 0o700)
            os.chown(ledger, 10002, 10002)
            old_path = maintenance.LEDGER_DIR
            maintenance.LEDGER_DIR = ledger
            reader = None
            try:
                old = b'{"immutable":"original"}'
                _private_record(ledger, 'identity.json', old)
                begin_name = 'migration-begin-00000000000000000003.json'
                begin = b'{"begin":1}'
                _private_record(ledger, begin_name, begin)
                reader = maintenance.RecordDirectory()
                entries, _ = reader.inventory(exclude=(begin_name,))
                inventory = json.dumps(entries, ensure_ascii=True, separators=(',', ':')).encode('ascii')
                continuity = maintenance.RecordContinuity(reader, begin_name, hashlib.sha256(inventory).hexdigest())
                begin_digest = hashlib.sha256(begin).hexdigest()
                continuity.accept(begin_name, begin_digest)
                next_name = 'migration-intent-00000000000000000003.json'
                next_body = b'{"intent":1}'
                _private_record(ledger, next_name, next_body)
                next_digest = hashlib.sha256(next_body).hexdigest()
                continuity.accept(next_name, next_digest)
                if damage == 'missing':
                    os.unlink(os.path.join(ledger, begin_name))
                    expect_failure(lambda: continuity.observe(next_name, next_digest))
                elif damage == 'changed':
                    with open(os.path.join(ledger, begin_name), 'wb') as stream:
                        stream.write(b'{"begin":2}')
                    expect_failure(lambda: continuity.observe(next_name, next_digest))
                elif damage == 'wrong-hash':
                    expect_failure(lambda: continuity.observe(next_name, 'f' * 64))
                else:
                    pending_name = 'migration-normal-00000000000000000003.json'
                    pending_body = b'{"normal":1}'
                    _private_record(ledger, pending_name, pending_body)
                    pending_digest = hashlib.sha256(pending_body).hexdigest()
                    continuity.observe(next_name, next_digest, {pending_name: pending_digest})
                    with open(os.path.join(ledger, pending_name), 'wb') as stream:
                        stream.write(b'{"normal":2}')
                    expect_failure(lambda: continuity.observe(next_name, next_digest, {pending_name: pending_digest}))
                expect_failure(lambda: continuity.observe(next_name, next_digest))
            finally:
                if reader is not None:
                    reader.close()
                maintenance.LEDGER_DIR = old_path


def maintenance_witness(scratch):
    """Exercise the real root-only witness journal and exact retry."""
    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('maintenance witness smoke requires root in an isolated guest or user namespace')
    for damage in ('realm-invalid', 'witness-changed', 'witness-missing'):
        with tempfile.TemporaryDirectory(prefix='g2-maint-witness-', dir=scratch) as directory:
            old_realm, old_maintenance = journal.REALM_CONTROL, maintenance.REALM_CONTROL
            journal.REALM_CONTROL = maintenance.REALM_CONTROL = os.path.join(directory, 'control')
            realm = witness = None
            try:
                realm = journal.RealmJournal.create()
                realm.genesis({'run_token': journal.RUN_TOKEN, 'boot_id': journal.boot_id(),
                               'supervisor_pid': os.getpid(), 'supervisor_starttime': journal.process_start(os.getpid())})
                realm.bind_application({'run_token': journal.RUN_TOKEN, 'fixture': 'isolated-smoke'})
                guard = {'dev': 1, 'ino': 2}
                group = {'path': '/sys/fs/cgroup/g2-c81-migration', 'dev': 3, 'ino': 4}
                witness = maintenance.MaintenanceJournal(realm, guard, group)
                realm.verify()
                body = b'{"private":"do-not-export-this-value"}'
                args = ('migration', 'smoke-rid', '1' * 64, 3, 0, 'intent',
                        'migration-intent-00000000000000000003.json')
                first = witness.append(*args, body, None, None)
                assert first[0] == 1 and len(first[1]) == 64
                assert witness.append(*args, body, None, None) == first
                expect_failure(lambda: witness.append(*args, body + b' ', None, None))
                raw = witness.public_witness_bytes()
                assert len(raw) == 1 and b'do-not-export-this-value' not in raw[0]
                event = journal.strict_json(raw[0])
                assert event['record_sha256'] == hashlib.sha256(body).hexdigest()
                assert event['record_bytes'] == len(body) and hashlib.sha256(raw[0]).hexdigest() == first[1]
                second = witness.append('migration', 'smoke-rid', '1' * 64, 3, 0, 'preparation',
                                        'migration-preparation-00000000000000000003.json', b'{}', first[1], None)
                events = witness.public_witnesses()
                assert second[0] == 2 and events[1]['previous'] == first[1]
                assert events[1]['predecessor_witness_digest'] == first[1]
                expect_failure(lambda: maintenance.MaintenanceJournal(realm, guard, group))
                path = os.path.join(journal.REALM_CONTROL, 'maintenance', 'witness-00000000000000000001.json')
                if damage == 'realm-invalid':
                    realm.invalid = True
                    witness.verify()
                    expect_failure(realm.verify)
                elif damage == 'witness-changed':
                    with open(path, 'ab') as stream:
                        stream.write(b' ')
                    expect_failure(witness.verify)
                else:
                    os.unlink(path)
                    expect_failure(witness.verify)
            finally:
                if witness is not None:
                    witness.close()
                if realm is not None:
                    realm.close()
                journal.REALM_CONTROL, maintenance.REALM_CONTROL = old_realm, old_maintenance


def maintenance_selection():
    """Select the activated generation despite earlier ready and terminal records."""
    from ceremony import SourceCeremony
    from types import SimpleNamespace
    bodies, events = {}, []

    def add(kind, generation, value, attempt=0):
        name = '%s-%d-%d' % (kind, generation, attempt)
        raw = json.dumps(value, separators=(',', ':')).encode('utf-8')
        bodies[name] = raw
        events.append({'family': 'migration', 'recovery_id': 'smoke-rid', 'application_digest': '1' * 64,
                       'kind': kind, 'phase_generation': generation, 'attempt': attempt,
                       'record_name': name, 'record_sha256': hashlib.sha256(raw).hexdigest()})

    add('ready', 2, {'generation': 2})
    add('observe-settled', 2, {'outcome': 'normal_observed_refenced', 'normal_digest': '2' * 64}, 1)
    add('terminal', 4, {'phase': 'quiescent', 'generation': 4})
    add('ready', 5, {'generation': 5})
    add('observe-settled', 5, {'outcome': 'normal_observed_refenced', 'normal_digest': '3' * 64}, 1)
    add('activation', 5, {'terminal_phase_generation': 7, 'normal_source': 'observation',
                          'normal_attempt': 1, 'normal_digest': '3' * 64})
    add('terminal', 7, {'phase': 'quiescent', 'generation': 7})
    owner = SimpleNamespace(generation=5, recovery_id='smoke-rid', application_digest='1' * 64,
                            terminal_admission_ready=lambda: True,
                            journal=SimpleNamespace(public_witnesses=lambda: list(events)),
                            directory=SimpleNamespace(read=lambda name: bodies[name]))
    flow = SimpleNamespace(life=SimpleNamespace(migration_checkpoint=owner))
    selected = SourceCeremony._migration_selection(flow)
    assert selected['ready']['generation'] == 5 and selected['terminal']['generation'] == 7
    assert selected['observe-settled']['normal_digest'] == '3' * 64
    events.append(dict(events[-1]))
    expect_failure(lambda: SourceCeremony._migration_selection(flow))
    events.pop()
    bodies['observe-settled-5-1'] += b' '
    expect_failure(lambda: SourceCeremony._migration_selection(flow))


def maintenance_host_witness():
    """Exercise the host consumer with retained failures and completed successors."""
    import sys
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    import source_verify

    class Host:
        Failure = JournalError
        strict_json = staticmethod(journal.strict_json)

        @staticmethod
        def require(condition, message):
            if not condition:
                raise JournalError(message)

    def fixture(history_marker='before_abort', final_marker='before_successor', omit=None):
        app = {'ID': 'app', 'SystemIdentifier': '123', 'DatabaseName': 'houfeng', 'DatabaseOID': 1, 'Epoch': 1}
        group = {'path': '/scope', 'dev': 3, 'ino': 4}
        guard = {'dev': 1, 'ino': 2}
        coordinator = {'pid': 100, 'starttime': 10, 'executable_sha256': 'b' * 64}
        child = {'pid': 101, 'starttime': 11, 'executable_sha256': 'a' * 64}
        backend = {'pid': 102, 'started': '2026-01-01T00:00:00Z', 'role': 'houfeng_migrator'}
        live = {'child': child, 'backend': backend, 'parent': coordinator, 'exe_dev': 5, 'exe_ino': 6,
                'cgroup': group, 'pidfd_live': True}
        chain, previous, ready_sequence = [], '0' * 64, None

        def add(kind, generation, value=None):
            nonlocal previous, ready_sequence
            if value is None:
                value = {'kind': kind, 'generation': generation}
            raw = json.dumps(value, separators=(',', ':'), ensure_ascii=False).encode('utf-8')
            digest = hashlib.sha256(raw).hexdigest()
            if omit == (kind, generation):
                return digest
            request = {'kind': kind, 'phase_generation': generation, 'attempt': 0}
            event = {'version': 1, 'sequence': len(chain) + 1, 'previous': previous,
                     'run_token': journal.RUN_TOKEN, 'boot_id': 'boot',
                     'supervisor': {'pid': 1, 'starttime': 2}, 'guard': guard, 'cgroup': group,
                     'family': 'migration', 'recovery_id': 'smoke-rid',
                     'application_digest': maintenance.application_digest(app),
                     'phase_generation': generation, 'attempt': 0, 'kind': kind,
                     'record_name': maintenance.checkpoint_name(request), 'record_bytes': len(raw),
                     'record_sha256': digest, 'predecessor_witness_digest': previous if chain else None,
                     'live_observation_digest': source_verify._sha(live) if kind == 'ready' else None}
            text = journal.canonical(event)
            chain.append(text.decode('ascii'))
            previous = hashlib.sha256(text).hexdigest()
            if kind == 'ready':
                ready_sequence = event['sequence']
            return digest

        add('begin', 2)
        if history_marker == 'before_intent':
            add('revocation-unresolved', 2)
        add('intent', 2)
        add('phase', 2)
        add('phase', 3)
        add('compensate', 2)
        if history_marker == 'before_abort':
            add('revocation-unresolved', 2)
        add('abort', 2)
        if history_marker == 'after_abort':
            add('revocation-unresolved', 2)
        add('terminal', 4)
        add('begin', 5)
        intent = add('intent', 5)
        add('phase', 5)
        add('preparation', 5)
        ready = {'version': 1, 'phase_generation': 5, 'intent_digest': intent, 'child': child, 'backend': backend}
        add('ready', 5, ready)
        normal = add('normal', 5)
        refencing = add('phase', 6)
        add('restrict', 5)
        if final_marker == 'before_successor':
            add('revocation-unresolved', 5)
        successor = add('successor', 5)
        if final_marker == 'after_successor':
            add('revocation-unresolved', 5)
        activation = {'version': 1, 'phase_generation': 5, 'terminal_phase_generation': 7,
                      'recovery_id': 'smoke-rid', 'application': app, 'intent_digest': intent,
                      'original_inventory_digest': 'c' * 64, 'successor_digest': successor,
                      'normal_digest': normal, 'normal_source': 'original', 'normal_attempt': 0,
                      'manifest_digest': 'd' * 64}
        add('activation', 5, activation)
        terminal = {'version': 1, 'generation': 7, 'previous': list(bytes.fromhex(refencing)),
                    'recovery_id': 'smoke-rid', 'application': app, 'phase': 'quiescent'}
        add('terminal', 7, terminal)
        if final_marker == 'after_terminal':
            add('revocation-unresolved', 5)
        result = {'maintenance_witness': chain, 'maintenance_ready_records': [], 'maintenance_settlements': [],
                  'maintenance_observations': [{'witness_sequence': ready_sequence, 'observation': live}],
                  'migration_scopes': [{'enrollment': {'coordinator': coordinator}, 'retirement': {}}],
                  'genesis': {'boot_id': 'boot', 'supervisor_pid': 1, 'supervisor_starttime': 2,
                              'migration_cgroup': group, 'programs': {'c81-migrator': 'a' * 64}},
                  'realm': {'app_binding': {'guard': {'guard_device': 1, 'guard_inode': 2}, 'application': app}},
                  'resolver_transition': {'recovery_id': 'smoke-rid'}}
        return result, {'ready': ready, 'activation': activation, 'terminal': terminal}

    def check(**kwargs):
        result, selection = fixture(**kwargs)
        source_verify._maintenance_witness(Host(), result, selection)

    def reject(message, **kwargs):
        try:
            check(**kwargs)
        except JournalError as exc:
            assert message in str(exc), str(exc)
            return
        raise AssertionError('incomplete revocation recovery was accepted')

    check()
    check(history_marker=None, final_marker='before_successor')
    check(history_marker='before_abort', final_marker=None)
    reject('historical revocation has no later witnessed settlement', history_marker='after_abort')
    reject('historical revocation has no later witnessed settlement', history_marker='before_intent')
    reject('revocation has no later witnessed successor settlement', final_marker='after_successor')
    reject('historical compensation differs', omit=('abort', 2))
    reject('completed activation chain', omit=('successor', 5))
    reject('new migration began before prior terminal', omit=('terminal', 4))
    reject('completed activation chain', omit=('terminal', 7))
    reject('completed activation chain', final_marker='after_terminal')

def maintenance_witness_cutpoints(scratch):
    """Interrupt a real witness publication at readback and cache installation."""
    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('maintenance witness smoke requires root in an isolated guest or user namespace')
    for cut in ('readback', 'partial-cache', 'changed'):
        with tempfile.TemporaryDirectory(prefix='g2-witness-cut-', dir=scratch) as directory:
            old_realm, old_maintenance = journal.REALM_CONTROL, maintenance.REALM_CONTROL
            journal.REALM_CONTROL = maintenance.REALM_CONTROL = os.path.join(directory, 'control')
            realm = witness = None
            old_read = journal._read
            try:
                realm = journal.RealmJournal.create()
                realm.genesis({'run_token': journal.RUN_TOKEN, 'boot_id': journal.boot_id(),
                               'supervisor_pid': os.getpid(), 'supervisor_starttime': journal.process_start(os.getpid())})
                witness = maintenance.MaintenanceJournal(realm, {'dev': 1, 'ino': 2},
                    {'path': '/sys/fs/cgroup/g2-c81-migration', 'dev': 3, 'ino': 4})
                original_reconcile = witness._reconcile_append
                if cut == 'readback':
                    def interrupted_read(fd, name):
                        if name == 'witness-00000000000000000001.json':
                            raise JournalError('injected witness readback failure')
                        return old_read(fd, name)
                    journal._read = interrupted_read
                else:
                    def interrupted_cache():
                        if witness.pending_append is not None:
                            old_entries, _, _, _, _, body = witness.pending_append
                            witness.entries = old_entries + [body]
                            raise JournalError('injected witness cache failure')
                        original_reconcile()
                    witness._reconcile_append = interrupted_cache
                args = ('migration', 'smoke-rid', '1' * 64, 3, 0, 'ready',
                        'migration-ready-00000000000000000003.json', b'{"ready":true}', None, '2' * 64)
                expect_failure(lambda: witness.append(*args))
                journal._read = old_read
                witness._reconcile_append = original_reconcile
                if cut == 'changed':
                    path = os.path.join(journal.REALM_CONTROL, 'maintenance', 'witness-00000000000000000001.json')
                    with open(path, 'ab') as stream:
                        stream.write(b' ')
                    expect_failure(witness.verify)
                else:
                    sequence, digest = witness.append(*args)
                    assert sequence == 1 and witness.append(*args) == (sequence, digest)
                    event = witness.public_witnesses()[0]
                    assert event['live_observation_digest'] == '2' * 64
                    assert event['record_sha256'] == hashlib.sha256(args[7]).hexdigest()
                    # This exercises cache settlement only; the guest ceremony must
                    # separately prove the real child's executable and held pidfd.
                    from types import SimpleNamespace
                    state = object.__new__(maintenance.MigrationCheckpoint)
                    state.journal = witness
                    state.recovery_id, state.application_digest, state.generation = 'smoke-rid', '1' * 64, 3
                    state.records, state.phase_owners, state.live_observations = {}, {}, []
                    accepted = []
                    state.continuity = SimpleNamespace(accept=lambda *values: accepted.append(values))
                    record_digest = hashlib.sha256(args[7]).hexdigest()
                    request = {'phase_generation': 3, 'kind': 'ready', 'attempt': 0,
                               'record_sha256': record_digest}
                    state.pending_checkpoint = (request, args[6], {'ready': True}, {}, ('ready', 0), 3)
                    state.pending_ready_observations = {}
                    expect_failure(state._reconcile_checkpoint)
                    evidence = {'child': 'original-live-observation'}
                    state.pending_ready_observations[(3, record_digest)] = ('2' * 64, evidence)
                    state._reconcile_checkpoint()
                    assert state.live_observations == [{'witness_sequence': 1, 'observation': evidence}]
                    assert state.records[('ready', 0)][2:] == (sequence, digest) and len(accepted) == 1
            finally:
                journal._read = old_read
                if witness is not None:
                    witness.close()
                if realm is not None:
                    realm.close()
                journal.REALM_CONTROL, maintenance.REALM_CONTROL = old_realm, old_maintenance
    maintenance_disposition_witness_cutpoints(scratch)
    maintenance_disposition_channel()


def maintenance_disposition_witness_cutpoints(scratch):
    """Exercise root journal bytes and recovery, without claiming live child evidence."""
    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('disposition witness smoke requires root in an isolated guest or user namespace')
    for outcome in ('witnessed', 'unwitnessed_retired'):
        for cut in ('before-create', 'after-write', 'partial', 'extra-file'):
            with tempfile.TemporaryDirectory(prefix='g2-ready-decision-', dir=scratch) as directory:
                old_realm, old_maintenance = journal.REALM_CONTROL, maintenance.REALM_CONTROL
                old_write = maintenance._write_new
                journal.REALM_CONTROL = maintenance.REALM_CONTROL = os.path.join(directory, 'control')
                realm = witness = None
                try:
                    realm = journal.RealmJournal.create()
                    realm.genesis({'run_token': journal.RUN_TOKEN, 'boot_id': journal.boot_id(),
                                   'supervisor_pid': os.getpid(), 'supervisor_starttime': journal.process_start(os.getpid())})
                    witness = maintenance.MaintenanceJournal(realm, {'dev': 1, 'ino': 2},
                        {'path': '/sys/fs/cgroup/g2-c81-migration', 'dev': 3, 'ino': 4})
                    preparation = witness.append('migration', 'smoke-rid', '1' * 64, 3, 0, 'preparation',
                        'migration-preparation-00000000000000000003.json', b'{"preparation":true}', None, None)
                    ready_name = 'migration-ready-00000000000000000003.json'
                    ready_bytes = b'{"ready":"original bytes"}'
                    original_ready = None
                    if outcome == 'witnessed':
                        original_ready = witness.append('migration', 'smoke-rid', '1' * 64, 3, 0, 'ready',
                                                        ready_name, ready_bytes, preparation[1], '2' * 64)
                    decision = {'outcome': outcome,
                                'ready_witness_sequence': original_ready[0] if original_ready else None,
                                'ready_witness_digest': original_ready[1] if original_ready else None,
                                'scope_retirement_digest': '3' * 64}
                    predecessor = original_ready[1] if original_ready else preparation[1]
                    args = ('migration', 'smoke-rid', '1' * 64, 3, 0, 'ready-disposition',
                            ready_name, ready_bytes, predecessor, None, decision)

                    def interrupted(fd, name, value):
                        if cut == 'after-write':
                            old_write(fd, name, value)
                        elif cut == 'partial':
                            target = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=fd)
                            try:
                                os.write(target, b'{"version":')
                                os.fsync(target)
                            finally:
                                os.close(target)
                        elif cut == 'extra-file':
                            target = os.open('unexpected', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                             0o600, dir_fd=fd)
                            os.close(target)
                        raise JournalError('injected disposition publication failure')

                    maintenance._write_new = interrupted
                    expect_failure(lambda: witness.append(*args))
                    maintenance._write_new = old_write
                    if cut in ('partial', 'extra-file'):
                        expect_failure(witness.verify)
                        assert witness.pending_append is not None
                    else:
                        sequence, digest = witness.append(*args)
                        assert witness.append(*args) == (sequence, digest)
                        event = witness.public_witnesses()[-1]
                        assert event['kind'] == 'ready-disposition' and event['ready_disposition'] == decision
                        assert event['predecessor_witness_digest'] == predecessor
                        assert event['live_observation_digest'] is None and event['record_name'] == ready_name
                        assert event['record_sha256'] == hashlib.sha256(ready_bytes).hexdigest()
                        assert hashlib.sha256(witness.public_witness_bytes()[-1]).hexdigest() == digest
                        conflict = dict(decision, scope_retirement_digest='4' * 64)
                        expect_failure(lambda: witness.append(*args[:-1], conflict))
                        expect_failure(lambda: witness.append(*args[:7], ready_bytes + b' ', *args[8:]))
                finally:
                    maintenance._write_new = old_write
                    if witness is not None:
                        witness.close()
                    if realm is not None:
                        realm.close()
                    journal.REALM_CONTROL, maintenance.REALM_CONTROL = old_realm, old_maintenance


def maintenance_disposition_channel():
    """Exercise actual SCM credentials and dispatch; the owner is a dispatch stub."""
    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('disposition channel smoke requires root in an isolated guest or user namespace')
    request = {'version': 1, 'op': 'maintenance_ready_disposition', 'family': 'migration',
               'recovery_id': 'smoke-rid', 'application_digest': '1' * 64, 'phase_generation': 3,
               'attempt': 0, 'kind': 'ready', 'record_sha256': '2' * 64, 'nonce': '3' * 64}

    class Owner:
        def __init__(self):
            self.calls = []

        def bind_coordinator(self, coordinator, pidfd):
            self.coordinator = coordinator

        def ready_disposition(self, value, credentials, deadline):
            maintenance.disposition_request(value, 'smoke-rid', '1' * 64, set())
            self.calls.append((value, credentials))
            return dict(value, op='maintenance_ready_disposition_ack', disposition='unwitnessed_retired',
                        decision_sequence=1, decision_digest='4' * 64,
                        ready_witness_sequence=None, ready_witness_digest=None)

        def checkpoint(self, value, credentials, deadline):
            raise AssertionError('disposition dispatched as checkpoint')

    for mode in ('dispatch', 'terminal-only', 'wrong-peer'):
        left, right = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        left.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        pid = os.fork()
        if pid == 0:
            try:
                left.close()
                os.setgroups([])
                os.setgid(10002)
                os.setuid(10002)
                right.settimeout(5)
                right.send(journal.canonical(request))
                if mode == 'dispatch':
                    response = journal.strict_json(right.recv(ipc.MAX_FRAME))
                    if (response['op'] != 'maintenance_ready_disposition_ack' or
                            response['nonce'] != request['nonce'] or response['decision_digest'] != '4' * 64):
                        os._exit(1)
                os._exit(0)
            except BaseException:
                os._exit(1)
        right.close()
        pidfd = os.pidfd_open(pid, 0)
        try:
            owner = Owner()
            coordinator = {'pid': pid + 1 if mode == 'wrong-peer' else pid,
                           'starttime': 1, 'executable_sha256': 'a' * 64}
            channel = ipc.MaintenanceChannel(left, coordinator, pidfd, owner, terminal_only=mode == 'terminal-only')
            if mode == 'dispatch':
                assert channel.serve_one(time.monotonic() + 5) is True
                assert owner.calls == [(request, (pid, 10002, 10002))]
            else:
                expect_failure(lambda: channel.serve_one(time.monotonic() + 5))
                assert owner.calls == []
        finally:
            left.close()
            os.close(pidfd)
            _, status = os.waitpid(pid, 0)
        assert os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0


def audit_parser():
    """Exercise the actual audit consumer and the two-stream identity join."""
    import copy
    import struct
    import audit

    for arch, numbers in ((audit.X86_64, (321,)), (audit.I386, (357,)),
                          (audit.X86_64, (49, 59, 322)), (audit.I386, (11, 102, 358, 361))):
        rule = struct.unpack('=260I', audit._rule(arch, numbers))
        for number in numbers:
            assert rule[3 + number // 32] & (1 << (number % 32))

    def raw(serial, fields=''):
        return ('audit(1.000:%d): %s' % (serial, fields)).encode('ascii')

    def stream():
        value = object.__new__(audit.AuditStream)
        value.active = True
        value.pending = {}
        value.bpf_facts = []
        value.syscall_facts = []
        value.bpf_serials = set()
        value.completed_serials = set()
        value.observation_version = 0
        value._prefix_remaining = {}
        value._prefix_events = set()
        value._pre_cut = []
        value._pre_cut_bytes = 0
        value._post_wire = {}
        value._post_wire_count = 0
        return value

    def syscall(value, serial, number, arg, result, *, arch='c000003e', pid=123):
        success = 'yes' if result >= 0 else 'no'
        value._record(audit.AUDIT_SYSCALL, raw(serial, 'arch=%s syscall=%d pid=%d ppid=1 success=%s exit=%d a0=%x exe="/usr/bin/test"' %
                                              (arch, number, pid, success, result, arg)))
        return value._record(audit.AUDIT_EOE, raw(serial))
    # Exercise the real consumer at each independently charged boundary.
    pre = stream()
    pre._prefix_remaining = None
    for serial in range(1, audit._MAX_PRE_CUT_RECORDS + 1):
        pre._record(audit.AUDIT_EXECVE, raw(serial))
    assert len(pre._pre_cut) == audit._MAX_PRE_CUT_RECORDS
    expect_failure(lambda: pre._record(audit.AUDIT_EXECVE, raw(20000)))
    byte_queue = stream()
    byte_queue._prefix_remaining = None
    byte_queue._record(audit.AUDIT_EXECVE, b'x' * (audit._MAX_PRE_CUT_BYTES - 1))
    assert byte_queue._pre_cut_bytes == audit._MAX_PRE_CUT_BYTES - 1
    byte_queue._record(audit.AUDIT_EXECVE, b'x')
    assert byte_queue._pre_cut_bytes == audit._MAX_PRE_CUT_BYTES
    expect_failure(lambda: byte_queue._record(audit.AUDIT_EXECVE, b'x'))
    del pre, byte_queue

    for distinct in (False, True):
        post = stream()
        for number in range(audit._MAX_POST_CUT_OCCURRENCES):
            post._record(audit.AUDIT_EXECVE, raw(1, 'n=%d' % (number if distinct else 0)))
        assert post._post_wire_count == audit._MAX_POST_CUT_OCCURRENCES
        assert len(post._post_wire) == (audit._MAX_POST_CUT_OCCURRENCES if distinct else 1)
        assert sum(post._post_wire.values()) == audit._MAX_POST_CUT_OCCURRENCES
        expect_failure(lambda: post._record(audit.AUDIT_EXECVE, raw(1, 'n=0')))
        assert post._post_wire_count == audit._MAX_POST_CUT_OCCURRENCES

    bpf_limit = stream()
    for serial in range(1, audit._MAX_BPF_AUDIT_FACTS + 1):
        bpf_limit._record(audit.AUDIT_BPF, raw(serial, 'prog-id=8 op=LOAD'))
    assert len(bpf_limit.bpf_facts) == audit._MAX_BPF_AUDIT_FACTS
    expect_failure(lambda: bpf_limit._record(audit.AUDIT_BPF, raw(20000, 'prog-id=8 op=LOAD')))
    syscall_limit = stream()
    for serial in range(1, audit._MAX_BPF_AUDIT_FACTS + 1):
        syscall(syscall_limit, serial, 321, 5, -1)
    assert len(syscall_limit.syscall_facts) == audit._MAX_BPF_AUDIT_FACTS
    expect_failure(lambda: syscall(syscall_limit, 20000, 321, 5, -1))
    ordinary_bind = stream()
    syscall(ordinary_bind, 1, 49, 3, -1)
    assert not ordinary_bind.bpf_facts and not ordinary_bind.syscall_facts

    pending = stream()
    fields = 'arch=c000003e syscall=321 pid=123 ppid=1 success=no exit=-1 a0=5'
    for serial in range(1, audit._MAX_PENDING_AUDIT_EVENTS + 1):
        pending._record(audit.AUDIT_SYSCALL, raw(serial, fields))
    assert len(pending.pending) == audit._MAX_PENDING_AUDIT_EVENTS
    pending._record(audit.AUDIT_EOE, raw(1))
    pending._record(audit.AUDIT_SYSCALL, raw(5000, fields))
    assert len(pending.pending) == audit._MAX_PENDING_AUDIT_EVENTS
    expect_failure(lambda: pending._record(audit.AUDIT_SYSCALL, raw(5001, fields)))

    expect_failure(lambda: audit._correlate([{}] * (audit._MAX_BPF_AUDIT_FACTS + 1), [], [], 0))
    expect_failure(lambda: audit._correlate([], [{}] * (audit._MAX_BPF_AUDIT_FACTS + 1), [], 0))
    too_many_rows = [{'sequence': i + 1, 'kind': 'audit', 'body': {}}
                     for i in range(2 * audit._MAX_BPF_AUDIT_FACTS + 1)]
    expect_failure(lambda: audit._correlate([], [], too_many_rows, 0))


    assert audit._bpf_record(raw(1, 'prog-id=55 op=LOAD')) == {'stamp': {'seconds': 1, 'milliseconds': 0}, 'serial': 1, 'program_id': 55, 'operation': 'LOAD'}
    for body in ('prog-id=0 op=LOAD', 'prog-id=-1 op=LOAD', 'prog-id=55 op=OTHER',
                 'prog-id=55 op=LOAD extra=1', 'prog-id=55 op=LOAD unexpected',
                 'prog-id=55 prog-id=56 op=LOAD', 'prog-id=4294967296 op=LOAD'):
        expect_failure(lambda body=body: audit._bpf_record(raw(1, body)), body)

    value = stream()
    value._record(audit.AUDIT_BPF, raw(101, 'prog-id=55 op=LOAD'))
    assert syscall(value, 101, 321, 5, 7) is None
    value._record(audit.AUDIT_BPF, raw(102, 'prog-id=55 op=UNLOAD'))
    assert syscall(value, 102, 3, 5, 0) is None
    value._record(audit.AUDIT_BPF, raw(103, 'prog-id=56 op=UNLOAD'))
    assert len(value.bpf_facts) == 3 and len(value.syscall_facts) == 2 and not value.pending

    def guard(kind, seq, obj, kernel_id, op, serial, flags=1):
        return {'kind': kind, 'sequence': seq, 'object_cookie': obj, 'kernel_id': kernel_id,
                'operation': op, 'audit_serial': serial if kind == 8 else 0, 'task_cookie': 11, 'task_start_ns': 100,
                'flags': flags if kind == 8 else flags & ~1,
                'detail': {'seconds': 1, 'nanoseconds': 0} if kind == 8 else None}

    def entry(sequence, kind, body):
        return {'sequence': sequence, 'kind': kind, 'body': body}

    def audit_body(serial, typ, obj=None, txn=None, task=11, command=None, result=None, operation=None, context='syscall'):
        return {'stamp': {'seconds': 1, 'milliseconds': 0}, 'serial': serial, 'record_type': typ, 'object_cookie': obj, 'transaction_id': txn,
                'task_cookie': task, 'command': command, 'result': result, 'operation': operation, 'context': context, 'raw': None}

    records = [entry(1, 'principal', {'task_cookie': 11, 'tgid': 123, 'tid': 123, 'start_ns': 100}),
               entry(2, 'object', {'cookie': 100, 'kernel_id': 55, 'class': 'program', 'owner_transaction': 7, 'publication_seq': 1, 'removal_seq': None}),
               entry(3, 'object', {'cookie': 101, 'kernel_id': 56, 'class': 'program', 'owner_transaction': 9, 'publication_seq': 2, 'removal_seq': None}),
               entry(4, 'transaction', {'id': 7, 'executor_cookie': 11, 'command': 'PROG_LOAD', 'result': 7,
                                         'cleanup': {'policy': 'none', 'steps': []}}),
               entry(5, 'transaction', {'id': 8, 'executor_cookie': 11, 'command': 'PROG_DETACH', 'result': 0,
                                         'cleanup': {'policy': 'close_unattached', 'steps': [{'object_cookie': 100}]}}),
               entry(6, 'transaction', {'id': 9, 'executor_cookie': 11, 'command': 'PROG_LOAD', 'result': 0,
                                         'cleanup': {'policy': 'auto_empty', 'steps': [{'object_cookie': 101}]}}),
               entry(7, 'guard', {'record': guard(7, 3, 100, 55, 0, 101)}),
               entry(8, 'guard', {'record': guard(8, 4, 100, 55, 0, 101)}),
               entry(9, 'guard', {'record': guard(7, 5, 100, 55, 1, 102)}),
               entry(10, 'guard', {'record': guard(8, 6, 100, 55, 1, 102)}),
               entry(11, 'guard', {'record': guard(7, 7, 101, 56, 1, 0, flags=0)}),
               entry(12, 'guard', {'record': guard(8, 8, 101, 56, 1, 103, flags=0)}),
               entry(13, 'audit', audit_body(101, 'BPF', 100, 7, command='PROG_LOAD', operation='LOAD')),
               entry(14, 'audit', audit_body(101, 'SYSCALL', txn=7, command='PROG_LOAD', result=7)),
               entry(15, 'audit', audit_body(102, 'BPF', 100, 8, operation='UNLOAD')),
               entry(16, 'audit', audit_body(102, 'SYSCALL', txn=8, result=0)),
               entry(17, 'audit', audit_body(103, 'BPF', 101, 9, task=None, operation='UNLOAD', context='standalone')),
               entry(18, 'guard', {'record': guard(1, 1, 100, 55, 0, 0)}),
               entry(19, 'guard', {'record': guard(1, 2, 101, 56, 0, 0)})]
    audit._correlate(value.bpf_facts, value.syscall_facts, records, 0)
    audit._correlate([], [], records, 19)
    expect_failure(lambda: audit._correlate(value.bpf_facts[:-1], value.syscall_facts, records, 0))
    expect_failure(lambda: audit._correlate(value.bpf_facts, value.syscall_facts[:-1], records, 0))
    for index, key, replacement in ((1, 'kernel_id', 57), (3, 'result', 8), (13, 'task_cookie', 99),
                                    (14, 'operation', 'LOAD'), (16, 'context', 'syscall')):
        changed = copy.deepcopy(records)
        changed[index]['body'][key] = replacement
        expect_failure(lambda changed=changed: audit._correlate(value.bpf_facts, value.syscall_facts, changed, 0))
    changed = copy.deepcopy(records)
    changed[4]['body']['cleanup']['steps'] = []
    expect_failure(lambda: audit._correlate(value.bpf_facts, value.syscall_facts, changed, 0))
    changed = copy.deepcopy(records)
    changed.append(entry(20, 'principal', {'task_cookie': 12, 'tgid': 123, 'tid': 124, 'start_ns': 101}))
    expect_failure(lambda: audit._correlate(value.bpf_facts, value.syscall_facts, changed, 0))
    expect_failure(lambda: value._record(audit.AUDIT_BPF, raw(101, 'prog-id=55 op=LOAD')))
    overflow = stream()
    overflow.bpf_facts = [{}] * audit._MAX_BPF_AUDIT_FACTS
    expect_failure(lambda: overflow._record(audit.AUDIT_BPF, raw(500, 'prog-id=8 op=LOAD')))
    incomplete = stream()
    expect_failure(lambda: incomplete._record(audit.AUDIT_EOE, raw(501)))
    duplicate_syscall = stream()
    duplicate_syscall._record(audit.AUDIT_SYSCALL, raw(502, 'arch=c000003e syscall=321 pid=123 ppid=1 success=no exit=-1 a0=5'))
    expect_failure(lambda: duplicate_syscall._record(audit.AUDIT_SYSCALL, raw(502, 'arch=c000003e syscall=321 pid=123 ppid=1 success=no exit=-1 a0=5')))
    negative = stream()
    expect_failure(lambda: syscall(negative, 201, 1, 0, 0))
    denied = stream()
    assert syscall(denied, 202, 357, 5, -1, arch='40000003') is None
    assert denied.syscall_facts[0]['number'] == 357
    interleaved = stream()
    interleaved._record(audit.AUDIT_BPF, raw(301, 'prog-id=80 op=UNLOAD'))
    delivered = syscall(interleaved, 301, 59, 0, 0)
    assert delivered['operation'] == 'exec' and delivered['exe'] == '/usr/bin/test'
    assert len(interleaved.syscall_facts) == 1
    reused = stream()
    reused._record(audit.AUDIT_BPF, raw(401, 'prog-id=88 op=LOAD'))
    reused._record(audit.AUDIT_BPF, raw(401, 'prog-id=88 op=LOAD'))
    assert syscall(reused, 401, 321, 5, 7) is None
    reuse_records = [entry(1, 'principal', {'task_cookie': 11, 'tgid': 123, 'tid': 123, 'start_ns': 100}),
                     entry(2, 'object', {'cookie': 200, 'kernel_id': 88, 'class': 'program', 'owner_transaction': 17, 'publication_seq': 1, 'removal_seq': 5}),
                     entry(3, 'object', {'cookie': 201, 'kernel_id': 88, 'class': 'program', 'owner_transaction': 18, 'publication_seq': 4, 'removal_seq': None}),
                     entry(4, 'transaction', {'id': 17, 'executor_cookie': 11, 'command': 'PROG_LOAD', 'result': 7,
                                               'cleanup': {'policy': 'none', 'steps': []}}),
                     entry(5, 'transaction', {'id': 18, 'executor_cookie': 11, 'command': 'PROG_LOAD', 'result': 7,
                                               'cleanup': {'policy': 'none', 'steps': []}}),
                     entry(6, 'guard', {'record': guard(7, 2, 200, 88, 0, 401)}),
                     entry(7, 'guard', {'record': guard(8, 3, 200, 88, 0, 401)}),
                     entry(8, 'guard', {'record': guard(7, 6, 201, 88, 0, 401)}),
                     entry(9, 'guard', {'record': guard(8, 7, 201, 88, 0, 401)}),
                     entry(10, 'audit', audit_body(401, 'BPF', 200, 17, command='PROG_LOAD', operation='LOAD')),
                     entry(11, 'audit', audit_body(401, 'BPF', 201, 18, command='PROG_LOAD', operation='LOAD')),
                     entry(12, 'audit', audit_body(401, 'SYSCALL', txn=17, command='PROG_LOAD', result=7)),
                     entry(13, 'guard', {'record': guard(1, 1, 200, 88, 0, 0)}),
                     entry(14, 'guard', {'record': guard(1, 4, 201, 88, 0, 0)}),
                     entry(15, 'guard', {'record': guard(2, 5, 200, 88, 0, 0)})]
    audit._correlate(reused.bpf_facts, reused.syscall_facts, reuse_records, 0)
    missing_removal = copy.deepcopy(reuse_records[:-1])
    expect_failure(lambda: audit._correlate(reused.bpf_facts, reused.syscall_facts, missing_removal, 0))
    duplicate = copy.deepcopy(reuse_records)
    duplicate[10]['body']['object_cookie'] = 200
    expect_failure(lambda: audit._correlate(reused.bpf_facts, reused.syscall_facts, duplicate, 0))

    for malformed in (b'audit(1.0:1): ', b'audit(1.0000:1): ', b'audit(1.000:0): ',
                      b'audit(1.000:4294967296): ', b'audit(-1.000:1): ', b'audit(1..00:1): '):
        expect_failure(lambda malformed=malformed: audit._fields(malformed))
    different_stamp = stream()
    different_stamp._record(audit.AUDIT_BPF, b'audit(1.001:7): prog-id=55 op=LOAD')
    different_stamp._record(audit.AUDIT_BPF, b'audit(2.001:7): prog-id=56 op=LOAD')
    assert audit._row_key(different_stamp.bpf_facts[0]) != audit._row_key(different_stamp.bpf_facts[1])
    same_event = stream()
    same_event._record(audit.AUDIT_BPF, b'audit(3.004:8): prog-id=55 op=LOAD')
    same_event._record(audit.AUDIT_BPF, b'audit(3.004:8): prog-id=56 op=UNLOAD')
    assert len(same_event.bpf_facts) == 2 and audit._row_key(same_event.bpf_facts[0]) == audit._row_key(same_event.bpf_facts[1])
    for missing in ('arch', 'syscall', 'pid', 'ppid', 'success', 'exit', 'a0'):
        fields = {'arch': 'c000003e', 'syscall': '321', 'pid': '123', 'ppid': '1', 'success': 'yes', 'exit': '1', 'a0': '5'}
        del fields[missing]
        expect_failure(lambda fields=fields: audit._syscall_fact((1, 0, 7), fields))
    missing_exe = stream()
    missing_exe._record(audit.AUDIT_SYSCALL, raw(610, 'arch=c000003e syscall=59 pid=123 ppid=1 success=yes exit=0 a0=0'))
    expect_failure(lambda: missing_exe._record(audit.AUDIT_EOE, raw(610)))
    missing_address = stream()
    missing_address._record(audit.AUDIT_SYSCALL, raw(611, 'arch=c000003e syscall=49 pid=123 ppid=1 success=yes exit=0 a0=3 exe="/usr/bin/test"'))
    expect_failure(lambda: missing_address._record(audit.AUDIT_EOE, raw(611)))
    prefix = stream()
    prefix._prefix_remaining = None
    old = raw(600, 'prog-id=55 op=LOAD')
    prefix._record(audit.AUDIT_BPF, old)
    event, kind, digest = audit._wire_key(audit.AUDIT_BPF, old)
    row = {'stamp': {'seconds': event[0], 'milliseconds': event[1]}, 'serial': event[2],
           'record_type': kind, 'payload_sha256': digest, 'occurrence': 1}
    prefix._install_cut({'boundary_sequence': 1, 'wire_count': 1}, [entry(1, 'audit_wire', row)])
    assert not prefix.pending and not prefix.bpf_facts and prefix._post_wire_count == 0 and not prefix._post_wire
    expect_failure(lambda: prefix._record(audit.AUDIT_BPF, old))
    partial = stream()
    partial._prefix_remaining = None
    partial._install_cut({'boundary_sequence': 1, 'wire_count': 1}, [entry(1, 'audit_wire', row)])
    assert partial._record(audit.AUDIT_BPF, old) is None and partial._post_wire_count == 0
    expect_failure(lambda: partial._record(audit.AUDIT_BPF, raw(600, 'prog-id=56 op=LOAD')))

    import bpf_authority
    bpf_authority._record_body('audit_wire', row)
    for field, invalid in (('serial', 0), ('occurrence', 0), ('record_type', True), ('payload_sha256', '0' * 64)):
        changed = copy.deepcopy(row)
        changed[field] = invalid
        expect_failure(lambda changed=changed: bpf_authority._record_body('audit_wire', changed))
    changed = copy.deepcopy(records)
    changed[7]['body']['record']['detail']['nanoseconds'] = 1000000
    expect_failure(lambda: audit._correlate(value.bpf_facts, value.syscall_facts, changed, 0))
    changed = copy.deepcopy(records)
    changed[6]['body']['record']['flags'] |= 1
    expect_failure(lambda: audit._correlate(value.bpf_facts, value.syscall_facts, changed, 0))
    unfinished = stream()
    unfinished._prefix_remaining = None
    unfinished._install_cut({'boundary_sequence': 1, 'wire_count': 1}, [entry(1, 'audit_wire', row)])
    assert unfinished._prefix_remaining[(event, kind, digest)] == 1
    assert not unfinished.pending


def bpf_runtime_root():
    """Replay rehashed root handoff evidence on both consumers."""
    import copy
    import sys
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    import bpf_authority
    import source_bpf_verify as verify

    class Host:
        Failure = JournalError

        @staticmethod
        def require(condition, message):
            if not condition:
                raise JournalError(message)

    systemd = 'a' * 64
    broker = {'id': 1, 'role': 'BOOTSTRAP_BROKER', 'task_cookie': 11, 'tgid': 20, 'tid': 20,
              'start_ns': 30, 'parent_cookie': 12, 'exe_sha256': 'b' * 64, 'script_sha256': None,
              'argv_sha256': 'c' * 64, 'uid': 0, 'gid': 0, 'cgroup_sha256': 'd' * 64,
              'registration': 'bootstrap', 'terminal': None}
    reader = dict(broker, id=2, role='BOOTSTRAP_AUDIT_READER', task_cookie=14, tgid=23, tid=23)
    observer = dict(broker, id=3, role='BOOTSTRAP_TERMINAL_OBSERVER', task_cookie=15, tgid=24, tid=24)
    actor = {'task_cookie': 12, 'tgid': 1, 'tid': 1, 'start_ns': 40, 'exe_sha256': 'b' * 64}
    control = {'stamp': {'seconds': 1, 'milliseconds': 0}, 'serial': 1, 'actor': actor,
               'phase': 'final_rules', 'change': 'add_rule', 'old_value': None, 'new_value': None,
               'rule_list': [4], 'result': 1}
    control2 = dict(control, serial=2, phase='final_lock', change='enabled', old_value=1, new_value=2,
                    rule_list=None)
    root = {'broker_cookie': 11, 'pid1_cookie': 12, 'pid1_start_ns': 40,
            'pid1_exe_sha256': systemd, 'root_mount_id': 2, 'root_dev': 0, 'root_ino': 3,
            'handoff_count': 1, 'thread_ids': [20, 21]}
    launch = dict(broker, id=4, role='BOOT_OWNER', task_cookie=13, tgid=22, tid=22,
                  registration='launch_source', script_sha256='f' * 64)
    build = {'bpf_authority': {'bootstrap_manifest': {'package_executables': {'systemd': {'sha256': systemd}}}}}
    boot, epoch = '12345678-1234-1234-1234-123456789abc', '1' * 32

    def replay(entries, consumer):
        records, previous = [], '0' * 64
        for kind, body in entries:
            event = {'version': 1, 'boot_id': boot, 'epoch': epoch, 'sequence': len(records) + 1,
                     'previous': previous, 'kind': kind, 'body': copy.deepcopy(body)}
            previous = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + verify._canonical(event)).hexdigest()
            records.append(event)
        first_tail = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + verify._canonical(records[0])).hexdigest()
        prefix = {key: None for key in verify._BOOTSTRAP}
        prefix.update(version=2, boot_id=boot, epoch=epoch, broker_principal=1,
                      bootstrap_tail=first_tail, records=records[:1])
        suffix = {'version': 2, 'boot_id': boot, 'epoch': epoch, 'genesis_tail': first_tail,
                  'records': records[1:], 'final_watermark': len(records), 'final_tail': previous,
                  'guard_status': {'state': 'sealed', 'sticky': 0, 'sequence': 0, 'lost': 0, 'probe_missed': 0}}
        if consumer == 'host':
            validated, _ = verify._chain(Host, prefix, suffix)
            verify._replay_runtime_root(Host, validated, prefix, build)
        else:
            state = {'broker': None, 'root': None, 'systemd': systemd, 'pid1': None}
            previous = '0' * 64
            for index, event in enumerate(records, 1):
                previous = bpf_authority._record(event, boot, epoch, index, previous)
                bpf_authority._observe_runtime_root(event, state)

    bootstrap = [('principal', broker), ('principal', reader), ('principal', observer),
                 ('audit_control', control), ('audit_control', control2)]
    valid = bootstrap + [('runtime_root', root), ('principal', launch)]
    for consumer in ('host', 'guest'):
        replay(valid, consumer)
        replay(bootstrap, consumer)  # Prelaunch bootstrap remains valid.
        replay([('principal', broker)], consumer)  # A shorter healthy prefix also remains valid.
    reboot = dict(launch, role='RECOVERY_PENDING', registration='launch_reboot')
    runtime = dict(launch, role='RUNTIME', registration='runtime', script_sha256=None)
    realm_self = dict(launch, role='REALM_ROOT', registration='realm_self')
    realm_child = dict(launch, role='REALM_ROOT', registration='realm_child')
    recovery = dict(launch, role='RECOVERY_OBSERVER', registration='recovery')
    for consumer in ('host', 'guest'):
        for principal in (realm_self, realm_child, recovery):
            replay(bootstrap + [('runtime_root', root), ('principal', principal)], consumer)
    bad = [bootstrap + [('runtime_root', root), ('runtime_root', root), ('principal', launch)],
           bootstrap + [('principal', launch)], bootstrap + [('principal', launch), ('runtime_root', root)],
           bootstrap + [('principal', reboot)], bootstrap + [('principal', runtime)],
           bootstrap + [('principal', realm_self)], bootstrap + [('principal', realm_child)],
           bootstrap + [('principal', recovery)],
           bootstrap + [('principal', dict(realm_self, registration='bootstrap'))],
           bootstrap + [('principal', dict(reader, registration='realm_self'))],
           [('runtime_root', root)] + bootstrap,
           [('principal', dict(broker, parent_cookie=99))] + bootstrap[1:] + [('runtime_root', root)],
           bootstrap[:3] + [('runtime_root', root), ('principal', launch)],
           bootstrap + [('runtime_root', dict(root, extra=1)), ('principal', launch)]]
    for field, value in (('task_cookie', 99), ('tgid', 2), ('tid', 2), ('start_ns', 41)):
        changed_actor = dict(actor, **{field: value})
        bad.append(bootstrap[:4] + [('audit_control', dict(control2, actor=changed_actor))] +
                   [('runtime_root', root), ('principal', launch)])
        bad.append(bootstrap[:4] + [('audit_control', dict(control2, actor=changed_actor))])
    for field, value in (('broker_cookie', 99), ('pid1_cookie', 99), ('pid1_exe_sha256', 'e' * 64),
                         ('handoff_count', 2), ('handoff_count', True), ('thread_ids', [21, 20]),
                         ('thread_ids', [20, 20]), ('thread_ids', [21]), ('thread_ids', []),
                         ('thread_ids', list(range(1, 4098))), ('root_dev', True), ('root_dev', 1 << 64),
                         ('pid1_start_ns', 0), ('pid1_start_ns', 41), ('thread_ids', [20, 1 << 32])):
        changed = dict(root, **{field: value})
        bad.append(bootstrap + [('runtime_root', changed), ('principal', launch)])
    for consumer in ('host', 'guest'):
        for entries in bad:
            expect_failure(lambda entries=entries, consumer=consumer: replay(entries, consumer))


def bpf_containerd_launcher():
    """Replay closed, rehashed launcher records on both independent consumers."""
    import copy
    import sys
    from types import SimpleNamespace
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    import bpf_authority
    import source_bpf_verify as verify

    class Host:
        Failure = JournalError
        @staticmethod
        def require(condition, message):
            if not condition:
                raise JournalError(message)

    broker_hash, systemd = 'b' * 64, 'a' * 64
    pins = {'dockerd': 'c' * 64, 'containerd': 'd' * 64, 'shim': 'e' * 64, 'runc': 'f' * 64}
    build = {'bpf_authority': {'bootstrap_manifest': {'package_executables':
             dict({name: {'sha256': digest} for name, digest in pins.items()}, systemd={'sha256': systemd})}}}
    broker = {'id': 1, 'role': 'BOOTSTRAP_BROKER', 'task_cookie': 11, 'tgid': 20, 'tid': 20,
              'start_ns': 30, 'parent_cookie': 12, 'exe_sha256': broker_hash, 'script_sha256': None,
              'argv_sha256': '1' * 64, 'uid': 0, 'gid': 0, 'cgroup_sha256': '2' * 64,
              'registration': 'bootstrap', 'terminal': None}
    actor = {'task_cookie': 12, 'tgid': 1, 'tid': 1, 'start_ns': 40, 'exe_sha256': broker_hash}
    control = {'stamp': {'seconds': 1, 'milliseconds': 0}, 'serial': 1, 'actor': actor,
               'phase': 'final_rules', 'change': 'add_rule', 'old_value': None, 'new_value': None,
               'rule_list': [4], 'result': 1}
    root = {'broker_cookie': 11, 'pid1_cookie': 12, 'pid1_start_ns': 40,
            'pid1_exe_sha256': systemd, 'root_mount_id': 2, 'root_dev': 0, 'root_ino': 3,
            'handoff_count': 1, 'thread_ids': [20]}
    pair = {'path': '/opt/g2/containerd-bin/containerd', 'exe_sha256': broker_hash,
            'exe_dev': 1, 'exe_ino': 3, 'target_path': '/usr/bin/containerd',
            'target_sha256': pins['containerd'], 'target_dev': 1, 'target_ino': 4}
    daemon = {'cookie': 100, 'owner': 1, 'mode': 'initial', 'task_cookie': 100, 'start_ns': 1000,
              'helper_sha256': broker_hash, 'dockerd_sha256': pins['dockerd'], 'argv_sha256': '3' * 64,
              'watch_sequence': 1, 'state': 'stopped'}
    def guard(seq, kind, cookie, parent, tgid, tid, ino, generation):
        return ('guard', {'record': {'sequence': seq, 'kind': kind, 'flags': 0, 'object_cookie': 0,
                'kernel_id': 0, 'root_cookie': 0, 'cgroup_id': 0, 'task_cookie': cookie,
                'task_start_ns': cookie * 10, 'audit_serial': 0, 'operation': 0, 'result': 0,
                'parent_cookie': parent, 'detail': {'tgid': tgid, 'tid': tid, 'exe_dev': 1,
                'exe_ino': ino, 'exec_generation': generation, 'exit_code': 0}, 'object_snapshot': None}})
    bootstrap = [('principal', broker), ('audit_control', control), ('runtime_root', root)]
    watch = guard(1, 12, 100, 50, 100, 100, 1, 1)
    root_exec = guard(2, 10, 100, 50, 100, 100, 2, 2)
    fork = guard(3, 9, 200, 100, 200, 200, 2, 2)
    launch = guard(4, 10, 200, 100, 200, 200, 3, 3)
    target = guard(5, 10, 200, 100, 200, 200, 4, 4)
    valid = bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec, fork, launch, target]
    boot, epoch = '12345678-1234-1234-1234-123456789abc', '1' * 32

    def invocation_history(*, worker_parent=False, self_exec=False, managed_fork=False):
        records = list(bootstrap) + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec]
        sequence = 2
        def emit(kind, cookie, parent, tgid, tid, ino, generation):
            nonlocal sequence
            sequence += 1
            records.append(guard(sequence, kind, cookie, parent, tgid, tid, ino, generation))
            return sequence
        def node(cookie, parent, generation, digest, fork_seq, exec_seq):
            return {'task_cookie': cookie, 'parent_cookie': parent, 'exec_generation': generation,
                    'exe_sha256': digest, 'fork_sequence': fork_seq, 'exec_sequence': exec_seq}
        nodes = [node(100, 50, 2, pins['dockerd'], 1, 2)]
        parent = 100
        if worker_parent:
            worker_fork = emit(9, 101, 100, 100, 101, 2, 2)
            nodes.append(node(101, 100, 2, pins['dockerd'], worker_fork, 2))
            parent = 101
        container_fork = emit(9, 200, parent, 200, 200, 2, 2)
        emit(10, 200, parent, 200, 200, 3, 3)
        container_exec = emit(10, 200, parent, 200, 200, 4, 4)
        generation = 4
        if self_exec:
            generation = 5
            container_exec = emit(10, 200, parent, 200, 200, 4, generation)
        nodes.append(node(200, parent, generation, pins['containerd'], container_fork, container_exec))
        parent = 200
        if managed_fork:
            child_fork = emit(9, 201, 200, 201, 201, 4, generation)
            nodes.append(node(201, 200, generation, pins['containerd'], child_fork, container_exec))
            parent = 201
        shim_fork = emit(9, 300, parent, 300, 300, 4, generation)
        shim_exec = emit(10, 300, parent, 300, 300, 5, generation + 1)
        nodes.append(node(300, parent, generation + 1, pins['shim'], shim_fork, shim_exec))
        wrapper_fork = emit(9, 400, 300, 400, 400, 5, generation + 1)
        wrapper_exec = emit(10, 400, 300, 400, 400, 6, generation + 2)
        nodes.append(node(400, 300, generation + 2, broker_hash, wrapper_fork, wrapper_exec))
        principal = dict(broker, id=5, role='RUNTIME', task_cookie=400, tgid=400, tid=400,
                         start_ns=4000, parent_cookie=300, registration='runtime')
        invocation = {'id': 1, 'principal_id': 5, 'task_cookie': 400, 'daemon_cookie': 100,
                      'grant_id': 1, 'container_id': '6' * 64, 'generation': 1, 'command': 'start',
                      'config_sha256': '7' * 64, 'bundle_sha256': None, 'process_sha256': None,
                      'exec_id': None, 'listener_id': 1, 'ancestry': nodes, 'state': 'bound'}
        records.extend([('principal', principal), ('invocation', invocation)])
        return records

    def replay(entries, consumer, *, late_pin=False, partial_first=False):
        records, previous = [], '0' * 64
        for kind, body in entries:
            event = {'version': 1, 'boot_id': boot, 'epoch': epoch, 'sequence': len(records) + 1,
                     'previous': previous, 'kind': kind, 'body': copy.deepcopy(body)}
            previous = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + verify._canonical(event)).hexdigest()
            records.append(event)
        first = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + verify._canonical(records[0])).hexdigest()
        prefix = {key: None for key in verify._BOOTSTRAP}
        prefix.update(version=2, boot_id=boot, epoch=epoch, broker_principal=1, broker_sha256=broker_hash,
                      bootstrap_tail=first, records=records[:1])
        suffix = {'version': 2, 'boot_id': boot, 'epoch': epoch, 'genesis_tail': first,
                  'records': records[1:], 'final_watermark': len(records), 'final_tail': previous,
                  'guard_status': {'state': 'sealed', 'sticky': 0, 'sequence': 0, 'lost': 0, 'probe_missed': 0}}
        if consumer == 'host':
            checked, _ = verify._chain(Host, prefix, suffix)
            if partial_first:
                partial, _ = verify._partial_chain(Host, prefix, {'records': records, 'watermark': len(records),
                                                                 'tail_sha256': previous})
                verify._replay_runtime_root(Host, partial, prefix, build)
                verify._replay_ancestry(Host, partial, prefix, build)
            verify._replay_runtime_root(Host, checked, prefix, build)
            verify._replay_ancestry(Host, checked, prefix, build)
            if entries == valid:
                import source_bpf_negative
                mutated = {'bpf_bootstrap': copy.deepcopy(prefix), 'bpf_authority': copy.deepcopy(suffix)}
                source_bpf_negative._mutate(mutated, 'launcher_binding_duplicate')
                duplicated, _ = verify._chain(Host, mutated['bpf_bootstrap'], mutated['bpf_authority'])
                expect_failure(lambda: verify._replay_runtime_root(Host, duplicated, mutated['bpf_bootstrap'], build))
                partial_result = {'bpf_bootstrap': copy.deepcopy(prefix), 'bpf_healthy_partial':
                                  {'records': copy.deepcopy(records), 'watermark': len(records), 'tail_sha256': previous}}
                source_bpf_negative._mutate(partial_result, 'launcher_binding_duplicate')
                duplicated, _ = verify._partial_chain(Host, partial_result['bpf_bootstrap'], partial_result['bpf_healthy_partial'])
                expect_failure(lambda: verify._replay_runtime_root(Host, duplicated, partial_result['bpf_bootstrap'], build))
            partial, _ = verify._partial_chain(Host, prefix, {'records': records, 'watermark': len(records),
                                                             'tail_sha256': previous})
            verify._replay_runtime_root(Host, partial, prefix, build)
            verify._replay_ancestry(Host, partial, prefix, build)
        else:
            authority = object.__new__(bpf_authority.BPFAuthority)
            authority._runtime_root = {'broker': None, 'root': None, 'systemd': systemd, 'pid1': None}
            authority._broker = SimpleNamespace(expected_hash=broker_hash)
            authority._containerd_launcher = None
            authority._pinned_containerd = None if late_pin else pins['containerd']
            authority._pinned_dockerd, authority._launcher_daemons = pins['dockerd'], {}
            authority._failed_checkpoint, authority._guard_sequence = None, 0
            authority._guard_tasks, authority._guard_execs, authority._runtime_execs = {}, {}, {}
            authority._observed_principals, authority._cgroup_identities = {}, {}
            authority._invocations, authority._listeners = {}, {}
            authority._exited_tasks, authority._publications, authority._live_object_ids, authority._removals = set(), {}, {}, {}
            previous = '0' * 64
            for index, event in enumerate(records, 1):
                previous = bpf_authority._record(event, boot, epoch, index, previous)
                authority._observe_snapshot(event)
            if late_pin:
                programs = {name: {'package': name, 'version': '1', 'path': '/usr/bin/' + name, 'sha256': digest}
                            for name, digest in dict(pins, python='4' * 64, systemd=systemd).items()}
                programs['systemd']['path'] = '/usr/lib/systemd/systemd'
                programs['containerd']['sha256'] = '9' * 64
                metadata = {'package_executables': programs, 'observer_bindings_sha256': '5' * 64}
                case = {'bpf_bootstrap': metadata, 'boot_inputs': {
                    'bootstrap_manifest_sha256': hashlib.sha256(bpf_authority.canonical(metadata)).hexdigest(),
                    'observer_bindings_sha256': '5' * 64}}
                expect_failure(lambda: authority.bootstrap_evidence(case, None))

    for consumer in ('host', 'guest'):
        replay(bootstrap, consumer)
        replay(valid, consumer)
        if consumer == 'guest':
            replay(valid, consumer, late_pin=True)
        replay(valid + [guard(6, 10, 200, 100, 200, 200, 4, 5),
                        guard(7, 9, 300, 200, 300, 300, 4, 5)], consumer)
        worker = bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec,
                  guard(3, 9, 101, 100, 100, 101, 2, 2), guard(4, 9, 200, 101, 200, 200, 2, 2),
                  guard(5, 10, 200, 101, 200, 200, 3, 3), guard(6, 10, 200, 101, 200, 200, 4, 4)]
        replay(worker, consumer)
        for options in ({}, {'worker_parent': True}, {'self_exec': True}, {'managed_fork': True},
                        {'worker_parent': True, 'self_exec': True, 'managed_fork': True}):
            replay(invocation_history(**options), consumer)
        original = invocation_history()
        wrong_target = copy.deepcopy(original)
        wrong_target[3][1]['target_ino'] = 99
        missing_exec = copy.deepcopy(original)
        next(body['record'] for kind, body in missing_exec if kind == 'guard' and
             body['record']['kind'] == 10 and body['record']['detail']['exe_ino'] == 3)['detail']['exe_ino'] = 2
        borrowed = copy.deepcopy(original)
        claimed = borrowed[-1][1]['ancestry'][1]
        claimed['exec_generation'], claimed['exec_sequence'] = 3, 4
        cross_watch = copy.deepcopy(original)
        second_watch = guard(10, 12, 500, 50, 500, 500, 1, 1)
        second_daemon = dict(daemon, cookie=500, task_cookie=500, start_ns=5000, watch_sequence=10)
        second_exec = guard(11, 10, 500, 50, 500, 500, 2, 2)
        cross_watch[-2:-2] = [second_watch, ('daemon', second_daemon), second_exec]
        cross_watch[-1][1]['daemon_cookie'] = 500
        cross_watch[-1][1]['ancestry'][0] = {'task_cookie': 500, 'parent_cookie': 50,
            'exec_generation': 2, 'exe_sha256': pins['dockerd'], 'fork_sequence': 10, 'exec_sequence': 11}
        semantic_cases = [
            (wrong_target, 'BPF containerd launcher ancestry differs', 'containerd launcher executed an unexpected target'),
            (missing_exec, 'BPF containerd launcher target transition differs', 'containerd launcher target transition differs'),
            (borrowed, 'BPF containerd launcher ancestry differs', 'runtime invocation containerd launcher ancestry differs'),
            (cross_watch, 'BPF ancestry differs from observed task lineage', 'runtime invocation lacks watched dockerd binding'),
        ]
        for entries, host_reason, guest_reason in semantic_cases:
            for partial_first in ((False, True) if consumer == 'host' else (False,)):
                try:
                    replay(entries, consumer, partial_first=partial_first)
                except JournalError as exc:
                    assert (host_reason if consumer == 'host' else guest_reason) in str(exc), str(exc)
                else:
                    raise AssertionError('invalid invocation ancestry accepted')
        bad = [bootstrap + [watch], bootstrap + [('containerd_launcher', pair), ('containerd_launcher', pair)],
               bootstrap + [('containerd_launcher', dict(pair, exe_sha256='9' * 64))],
               bootstrap + [('containerd_launcher', dict(pair, target_ino=3))],
               [('containerd_launcher', pair)] + bootstrap,
               valid[:-2] + [guard(4, 10, 200, 100, 200, 200, 2, 3), target],
               valid[:-1] + [guard(5, 10, 200, 100, 200, 200, 3, 4)],
               bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec, fork,
                            guard(4, 10, 200, 100, 200, 200, 4, 3)]]
        bad.extend([
            bootstrap + [('containerd_launcher', dict(pair, target_sha256='9' * 64))],
            bootstrap + [('containerd_launcher', dict(pair, unexpected=1))],
            valid[:-2] + [guard(4, 10, 200, 100, 200, 200, 3, 4)],
            bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec,
                         guard(3, 9, 200, 100, 200, 200, 2, 2),
                         guard(4, 9, 300, 200, 300, 300, 2, 2),
                         guard(5, 10, 300, 200, 300, 300, 3, 3)],
            valid[:-1] + [guard(5, 9, 300, 200, 300, 300, 3, 3),
                          guard(6, 10, 300, 200, 300, 300, 4, 4)],
            bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon),
                         guard(2, 10, 100, 50, 100, 100, 3, 2)],
            valid[:-1] + [guard(5, 9, 300, 200, 200, 201, 3, 3),
                          guard(6, 10, 300, 200, 200, 201, 4, 4)],
            bootstrap + [('containerd_launcher', pair), watch, ('daemon', daemon), root_exec, fork, launch,
                         guard(5, 9, 300, 100, 300, 300, 2, 2),
                         guard(6, 10, 300, 100, 300, 300, 4, 3)],
        ])
        for entries in bad:
            expect_failure(lambda entries=entries, consumer=consumer: replay(entries, consumer))


def systemd_parser():
    """Exercise systemctl's repeated exec properties and alias identity checks."""
    import systemd_realm as systemd

    fsck = r'systemd-fsck@dev-disk-by\x2dlabel-BOOT.service'
    fsck_names = r'"systemd-fsck@dev-disk-by\\x2dlabel-BOOT.service"'
    assert systemd._decode_string_array(fsck_names) == [fsck]
    assert systemd._decode_string_array(' \t') == []
    assert systemd._decode_string_array(r'"a\\b\"c\$d\`e" plain') == ['a\\b"c$d`e', 'plain']
    assert systemd._array(r'"dev-disk-by\\x2dlabel-BOOT.device" boot.mount',
                         'smoke') == [r'dev-disk-by\x2dlabel-BOOT.device', 'boot.mount']
    assert systemd._array('"/run/a b.conf"', 'smoke', kind='path') == ['/run/a b.conf']
    for malformed in ('"unterminated', '"bad\\q"', '"bad\\', 'foo\\', '"foo"bar',
                      'foo"bar"', 'foo "foo"', '"" ""'):
        expect_failure(lambda malformed=malformed: systemd._decode_string_array(malformed))
    for invalid in ('""', 'relative/path', '/run/../bad', '"/run/bad\tname"'):
        expect_failure(lambda invalid=invalid: systemd._array(invalid, 'smoke', kind='path'))
    expect_failure(lambda: systemd._array('""', 'smoke', kind='name'))
    expect_failure(lambda: systemd._array('"bad/name.device"', 'smoke'))

    block = ('Id=open-iscsi.service\n'
             'Names=iscsi.service open-iscsi.service\n'
             'LoadState=loaded\nActiveState=inactive\nSubState=dead\n'
             'RefuseManualStop=no\n'
             'ExecStop={ path=/usr/lib/open-iscsi/umountiscsi.sh ; argv[]=/usr/lib/open-iscsi/umountiscsi.sh }\n'
             'ExecStop={ path=/bin/sync ; argv[]=/bin/sync }\n'
             'ExecStop={ path=/usr/lib/open-iscsi/logout-all.sh ; argv[]=/usr/lib/open-iscsi/logout-all.sh }\n'
             'ExecStopPost=\n')
    original = systemd._command
    output = [block.encode('ascii')]
    systemd._command = lambda operation, argv, deadline: output[0]
    try:
        unit_path = [systemd.CONTROL_ROOT, '/etc/systemd/system', '/run/systemd/system']
        output[0] = (' '.join(unit_path) + '\n').encode('ascii')
        assert systemd._manager_unit_path(time.monotonic() + 3) == unit_path
        for invalid in (unit_path + [unit_path[0]], unit_path[1:], [unit_path[1], unit_path[0], unit_path[2]],
                        [unit_path[0], '/etc/../etc/systemd/system', unit_path[2]]):
            output[0] = (' '.join(invalid) + '\n').encode('ascii')
            expect_failure(lambda: systemd._manager_unit_path(time.monotonic() + 3))
        output[0] = block.encode('ascii')
        props = systemd._show(['open-iscsi.service'], time.monotonic() + 3)['open-iscsi.service']
        fsck_block = ('Id=' + fsck + '\nNames=' + fsck_names + '\n'
                      'LoadState=loaded\nActiveState=active\nSubState=exited\nRefuseManualStop=no\n')
        output[0] = fsck_block.encode('ascii')
        assert systemd._show([fsck], time.monotonic() + 3)[fsck]['Names'] == fsck_names
        output[0] = fsck_block.replace(fsck_names, fsck_names + ' ' + fsck_names).encode('ascii')
        expect_failure(lambda: systemd._show([fsck], time.monotonic() + 3))
        output[0] = block.encode('ascii')
        assert len(props['ExecStop'].splitlines()) == 3 and props['ExecStopPost'] == ''
        output[0] = (block + '\n' + block).encode('ascii')
        assert list(systemd._show(['iscsi.service', 'open-iscsi.service'], time.monotonic() + 3)) == ['open-iscsi.service']
        output[0] = (block + '\n' + block.replace('ActiveState=inactive', 'ActiveState=active')).encode('ascii')
        expect_failure(lambda: systemd._show(['iscsi.service', 'open-iscsi.service'], time.monotonic() + 3))
        output[0] = block.replace('RefuseManualStop=no\n', '').encode('ascii')
        expect_failure(lambda: systemd._show(['open-iscsi.service'], time.monotonic() + 3))
        output[0] = (block + 'ActiveState=inactive\n').encode('ascii')
        expect_failure(lambda: systemd._show(['open-iscsi.service'], time.monotonic() + 3))
        output[0] = block.replace('ExecStopPost=\n', 'ExecStopPost=\nExecStopPost={ path=/bin/sync }\n').encode('ascii')
        expect_failure(lambda: systemd._show(['open-iscsi.service'], time.monotonic() + 3))
        unsafe = {key: '' for key in ('FragmentPath', 'DropInPaths', 'Type', 'RemainAfterExit', 'Restart',
                                     'ExecStop', 'ExecStopPost', 'PropagatesStopTo', 'StopPropagatedFrom',
                                     'PartOf', 'BindsTo', 'RequiredBy', 'RequisiteOf', 'BoundBy',
                                     'ConsistsOf', 'ControlGroup')}
        unsafe.update(Id=systemd.TMPFILES, LoadState='loaded', Type='oneshot', RemainAfterExit='yes', Restart='no',
                      RefuseManualStop='yes', MainPID='0', ControlPID='0', ExecStop=props['ExecStop'])
        expect_failure(lambda: systemd._tmpfiles_properties(unsafe, (), False))
        del unsafe['ExecStop'], unsafe['ExecStopPost']
        old_fragment, old_cgroup = systemd._safe_fragment, systemd._empty_cgroup
        systemd._safe_fragment = lambda path: ((1, 2, 3, 4, 5), 'a' * 64)
        systemd._empty_cgroup = lambda path, name: None
        try:
            systemd._tmpfiles_properties(unsafe, (), False)
            assert unsafe['ExecStop'] == unsafe['ExecStopPost'] == ''
            unsafe['RequiredBy'] = r'"dev-disk-by\\x2dlabel-BOOT.device" boot.mount'
            systemd._tmpfiles_properties(unsafe, (), False)
            unsafe['RequiredBy'] = '"dbus.service"'
            expect_failure(lambda: systemd._tmpfiles_properties(unsafe, (), False))
            unsafe['RequiredBy'] = '"dbus.service" dbus.service'
            expect_failure(lambda: systemd._tmpfiles_properties(unsafe, (), False))
            unsafe['RequiredBy'] = ''
            unsafe['DropInPaths'] = '"' + systemd.OVERRIDE + '"'
            unsafe['RefuseManualStop'] = 'no'
            systemd._tmpfiles_properties(unsafe, (), True)
            unsafe['DropInPaths'] = '"' + systemd.OVERRIDE + '" ' + systemd.OVERRIDE
            expect_failure(lambda: systemd._tmpfiles_properties(unsafe, (), True))
            unsafe['DropInPaths'] = ''
            unsafe['PropagatesStopTo'] = '"boot.mount"'
            unsafe['RefuseManualStop'] = 'yes'
            expect_failure(lambda: systemd._tmpfiles_properties(unsafe, (), False))
        finally:
            systemd._safe_fragment, systemd._empty_cgroup = old_fragment, old_cgroup
        saved = (systemd._files, systemd._loaded, systemd._jobs, systemd._show,
                 systemd._closed_route, systemd._quiet)
        alias = r'alias-by\x2dlabel.service'
        files = {fsck: 'masked-runtime'}
        row = {'Id': fsck, 'Names': fsck_names + r' "alias-by\\x2dlabel.service"',
               'LoadState': 'loaded', 'ActiveState': 'inactive', 'SubState': 'dead',
               'RefuseManualStop': 'no'}
        systemd._files = lambda deadline: files
        systemd._loaded = lambda deadline: [fsck]
        systemd._jobs = lambda deadline: ()
        systemd._show = lambda names, deadline: {fsck: row}
        systemd._closed_route = lambda name, state, inventory, deadline: {'name': name}
        systemd._quiet = lambda name, value: None
        try:
            expect_failure(lambda: systemd._inventory(time.monotonic() + 3, True))
            files[alias] = 'masked-runtime'
            systemd._inventory(time.monotonic() + 3, True)
            row['Names'] = fsck_names + ' "dbus.service"'
            expect_failure(lambda: systemd._inventory(time.monotonic() + 3, False))
        finally:
            (systemd._files, systemd._loaded, systemd._jobs, systemd._show,
             systemd._closed_route, systemd._quiet) = saved
    finally:
        systemd._command = original

    # The selection12 inventory reported autovt as masked through its vendor
    # alias, while only getty had a direct persistent mask. Model those paths
    # and exercise the same classification and mask-readback code as seal.
    from types import SimpleNamespace
    from contextlib import nullcontext
    import stat
    etc, runtime = '/etc/systemd/system', '/run/systemd/system'
    entries = {etc + '/getty@.service': '/dev/null',
               runtime + '/existing.service': '/dev/null',
               '/usr/lib/systemd/system/autovt@.service': 'getty@.service',
               '/usr/lib/systemd/system/cryptdisks-early.service': '/dev/null',
               '/usr/lib/systemd/system/alias.service': 'target.service',
               '/usr/lib/systemd/system/target.service': None,
               etc + '/target.service': '/dev/null',
               etc + '/collision.service': None,
               runtime + '/runtime-collision.service': None}
    dirs = {'/etc', '/etc/systemd', etc, '/run', '/run/systemd', runtime}
    saved_fs = (os.path.lexists, os.lstat, os.readlink, os.path.realpath, systemd._command,
                systemd._route_entry, systemd._sealed_routes, systemd._held_route, systemd._manager_unit_path,
                systemd._prepare_control_masks, systemd._reject_higher_unit, systemd._unit_path)
    commands = []

    def fake_lstat(path):
        if path in dirs:
            mode = stat.S_IFDIR | 0o755
        elif path in entries:
            mode = (stat.S_IFREG | 0o644) if entries[path] is None else (stat.S_IFLNK | 0o777)
        else:
            raise FileNotFoundError(path)
        return SimpleNamespace(st_mode=mode, st_uid=0, st_gid=0, st_dev=1, st_ino=2, st_ctime_ns=3)

    def fake_mask(operation, argv, deadline):
        commands.append((operation, list(argv)))
        assert operation == 'mask' and argv[:3] == ['--runtime', '--no-reload', 'mask']
        for name in argv[3:]:
            assert runtime + '/' + name not in entries
            entries[runtime + '/' + name] = '/dev/null'
        return b''

    os.path.lexists = lambda path: path in entries
    os.lstat = fake_lstat
    os.readlink = lambda path: entries[path]
    os.path.realpath = lambda path: entries[path]
    systemd._command = fake_mask
    systemd._manager_unit_path = lambda deadline: [systemd.CONTROL_ROOT, etc, runtime]
    systemd._prepare_control_masks = lambda routes, forbidden, deadline: None
    def reject_fake_higher(name):
        if any(os.path.lexists(os.path.join(root, name)) for root in systemd.REJECT_UNIT_PATHS):
            systemd._fail('mask-readback', units=(name,))
    systemd._reject_higher_unit = reject_fake_higher
    systemd._route_entry = lambda path: {'path': path, 'target': entries[path], 'dev': 1, 'ino': 2,
                                        'mode': ((stat.S_IFREG | 0o644) if entries[path] is None else
                                                 (stat.S_IFLNK | 0o777)), 'uid': 0, 'gid': 0, 'nlink': 1,
                                        'ctime_ns': 3, 'parents': []}
    try:
        files = {'autovt@.service': 'masked', 'getty@.service': 'masked',
                 'cryptdisks-early.service': 'masked',
                 'existing.service': 'masked-runtime', 'new.service': 'enabled'}
        expect_failure(lambda: systemd._mask_path('autovt@.service'))
        expect_failure(lambda: systemd._route('cryptdisks-early.service'))
        planned = systemd._install_masks(files, time.monotonic() + 3)
        assert commands == [('mask', ['--runtime', '--no-reload', 'mask', 'autovt@.service',
                                     'cryptdisks-early.service', 'new.service'])]
        assert planned['cryptdisks-early.service']['vendor_mask']['path'] == '/usr/lib/systemd/system/cryptdisks-early.service'
        assert planned['cryptdisks-early.service']['vendor_mask']['target'] == '/dev/null'
        terminal, links, effective = systemd._route('cryptdisks-early.service')
        assert terminal == 'cryptdisks-early.service' and not links and effective[0] == runtime
        systemd._sealed_routes = planned
        systemd._held_route = lambda route_entries, name: nullcontext()
        proof = systemd._closed_route('cryptdisks-early.service', 'masked-runtime',
                                      {'cryptdisks-early.service': 'masked-runtime'}, time.monotonic() + 3)
        assert proof['mask']['path'] == runtime + '/cryptdisks-early.service' and not proof['aliases']
        entries['/usr/lib/systemd/system/cryptdisks-early.service'] = None
        expect_failure(lambda: systemd._closed_route('cryptdisks-early.service', 'masked-runtime',
                                                    {'cryptdisks-early.service': 'masked-runtime'}, time.monotonic() + 3))
        entries['/usr/lib/systemd/system/cryptdisks-early.service'] = '/dev/null'
        del entries[runtime + '/cryptdisks-early.service']
        expect_failure(lambda: systemd._route('cryptdisks-early.service'))
        entries[runtime + '/cryptdisks-early.service'] = '/dev/null'
        entries['/run/systemd/generator.early/cryptdisks-early.service'] = '/dev/null'
        expect_failure(lambda: systemd._route('cryptdisks-early.service'))
        del entries['/run/systemd/generator.early/cryptdisks-early.service']
        assert systemd._mask_path('autovt@.service')[0] == runtime
        assert systemd._mask_path('getty@.service')[0] == etc
        assert entries[etc + '/getty@.service'] == '/dev/null'
        terminal, aliases, direct = systemd._route('alias.service')
        assert terminal == 'target.service' and len(aliases) == 1 and direct[0] == etc
        assert aliases[0]['target_name'] == 'target.service' and aliases[0]['source'][0]['target'] is None
        entries['/usr/lib/systemd/system/target.service'] = 'alias.service'
        expect_failure(lambda: systemd._route('alias.service'))
        entries['/usr/lib/systemd/system/target.service'] = None
        entries['/run/systemd/generator.early/alias.service'] = '/dev/null'
        expect_failure(lambda: systemd._route('alias.service'))
        del entries['/run/systemd/generator.early/alias.service']
        commands.clear()
        expect_failure(lambda: systemd._install_masks({'collision.service', 'newer.service'}, time.monotonic() + 3))
        expect_failure(lambda: systemd._install_masks({'runtime-collision.service'}, time.monotonic() + 3))
        assert not commands
        net_saved = (systemd._netplan_mask, systemd._check_netplan_mask, systemd._show, systemd._jobs)
        entries[systemd.NETPLAN_MASK] = '/dev/null'
        entries[systemd.NETPLAN_RUNTIME] = None
        try:
            expect_failure(lambda: systemd._route(systemd.NETPLAN))
            systemd._netplan_mask = object()
            systemd._check_netplan_mask = lambda deadline, **kwargs: None
            planned = systemd._install_masks({systemd.NETPLAN}, time.monotonic() + 3)
            systemd._sealed_routes = planned
            props = {'Id': systemd.NETPLAN, 'Names': systemd.NETPLAN, 'LoadState': 'masked',
                     'UnitFileState': 'masked', 'FragmentPath': systemd.NETPLAN_MASK,
                     'ActiveState': 'inactive', 'MainPID': '0', 'ControlPID': '0', 'ControlGroup': ''}
            systemd._show = lambda names, deadline: {systemd.NETPLAN: props}
            systemd._jobs = lambda deadline: ()
            proof = systemd._closed_route(systemd.NETPLAN, 'masked', {systemd.NETPLAN: 'masked'},
                                          time.monotonic() + 3)
            assert len(proof['manager']) == 1 and proof['manager'][0]['name'] == systemd.NETPLAN
            for key, invalid in (('Names', systemd.NETPLAN + ' other.service'), ('LoadState', 'loaded'),
                                 ('UnitFileState', 'enabled-runtime'), ('FragmentPath', systemd.NETPLAN_RUNTIME),
                                 ('MainPID', '9')):
                old = props[key]
                props[key] = invalid
                expect_failure(lambda: systemd._closed_route(systemd.NETPLAN, 'masked',
                                                            {systemd.NETPLAN: 'masked'}, time.monotonic() + 3))
                props[key] = old
            systemd._jobs = lambda deadline: (('1', systemd.NETPLAN),)
            expect_failure(lambda: systemd._closed_route(systemd.NETPLAN, 'masked',
                                                        {systemd.NETPLAN: 'masked'}, time.monotonic() + 3))
        finally:
            (systemd._netplan_mask, systemd._check_netplan_mask, systemd._show, systemd._jobs) = net_saved
        udev_saved = (systemd._show, systemd._jobs)
        entries[systemd.UDEV_VENDOR] = systemd.UDEV_CANONICAL
        canonical = {'Id': systemd.UDEV_CANONICAL, 'Names': systemd.UDEV_ALIAS + ' ' + systemd.UDEV_CANONICAL,
                     'LoadState': 'loaded'}
        systemd._show = lambda names, deadline: {systemd.UDEV_CANONICAL: canonical}
        systemd._jobs = lambda deadline: ()
        try:
            expect_failure(lambda: systemd._route(systemd.UDEV_ALIAS))
            entries[etc + '/' + systemd.UDEV_ALIAS] = '/dev/null'
            expect_failure(lambda: systemd._install_masks({systemd.UDEV_ALIAS}, time.monotonic() + 3))
            del entries[etc + '/' + systemd.UDEV_ALIAS]
            entries[runtime + '/' + systemd.UDEV_ALIAS] = '/dev/null'
            expect_failure(lambda: systemd._install_masks({systemd.UDEV_ALIAS}, time.monotonic() + 3))
            del entries[runtime + '/' + systemd.UDEV_ALIAS]
            entries['/run/systemd/generator.early/' + systemd.UDEV_ALIAS] = '/dev/null'
            expect_failure(lambda: systemd._install_masks({systemd.UDEV_ALIAS}, time.monotonic() + 3))
            del entries['/run/systemd/generator.early/' + systemd.UDEV_ALIAS]
            commands.clear()
            planned = systemd._install_masks({systemd.UDEV_ALIAS}, time.monotonic() + 3)
            assert commands == [('mask', ['--runtime', '--no-reload', 'mask', systemd.UDEV_ALIAS])]
            assert systemd.UDEV_CANONICAL not in commands[0][1]
            assert planned[systemd.UDEV_ALIAS]['protected_alias']['path'] == systemd.UDEV_VENDOR
            systemd._sealed_routes = planned
            masked_udev = {'Id': systemd.UDEV_ALIAS, 'Names': systemd.UDEV_ALIAS, 'LoadState': 'masked',
                           'UnitFileState': 'masked-runtime', 'FragmentPath': runtime + '/' + systemd.UDEV_ALIAS,
                           'ActiveState': 'inactive', 'MainPID': '0', 'ControlPID': '0', 'ControlGroup': ''}
            systemd._show = lambda names, deadline: {systemd.UDEV_ALIAS: masked_udev}
            proof = systemd._closed_route(systemd.UDEV_ALIAS, 'masked-runtime',
                                          {systemd.UDEV_ALIAS: 'masked-runtime'}, time.monotonic() + 3)
            assert proof['manager'] == [] and proof['terminal'] == systemd.UDEV_ALIAS
            systemd._show = lambda names, deadline: {systemd.UDEV_CANONICAL: canonical}
            expect_failure(lambda: systemd._closed_route(systemd.UDEV_ALIAS, 'masked-runtime',
                                                        {systemd.UDEV_ALIAS: 'masked-runtime'}, time.monotonic() + 3))
            systemd._show = lambda names, deadline: {systemd.UDEV_ALIAS: masked_udev}
            for key, bad in (('Names', systemd.UDEV_ALIAS + ' ' + systemd.UDEV_CANONICAL),
                             ('LoadState', 'loaded'), ('UnitFileState', 'enabled'), ('MainPID', '400')):
                old = masked_udev[key]
                masked_udev[key] = bad
                expect_failure(lambda: systemd._closed_route(systemd.UDEV_ALIAS, 'masked-runtime',
                                                            {systemd.UDEV_ALIAS: 'masked-runtime'}, time.monotonic() + 3))
                masked_udev[key] = old
            systemd._jobs = lambda deadline: (('1', systemd.UDEV_ALIAS),)
            expect_failure(lambda: systemd._closed_route(systemd.UDEV_ALIAS, 'masked-runtime',
                                                        {systemd.UDEV_ALIAS: 'masked-runtime'}, time.monotonic() + 3))
            systemd._jobs = lambda deadline: ()
            entries[systemd.UDEV_VENDOR] = 'different.service'
            expect_failure(lambda: systemd._recapture_routes(planned))
            expect_failure(lambda: systemd._closed_route(systemd.UDEV_ALIAS, 'masked-runtime',
                                                        {systemd.UDEV_ALIAS: 'masked-runtime'}, time.monotonic() + 3))
        finally:
            systemd._show, systemd._jobs = udev_saved
        parent = {'path': '/run/systemd/system', 'dev': 1, 'ino': 10, 'mode': stat.S_IFDIR | 0o755,
                  'uid': 0, 'gid': 0, 'ctime_ns': 3}
        before = {'path': '/run/systemd/system/check.service', 'dev': 1, 'ino': 20,
                  'mode': stat.S_IFLNK | 0o777, 'uid': 0, 'gid': 0, 'nlink': 1,
                  'ctime_ns': 4, 'target': '/dev/null', 'parents': [parent]}
        after = json.loads(json.dumps(before))
        after['parents'][0]['ctime_ns'] = 5
        systemd._route_entry = lambda path: json.loads(json.dumps(after))
        def route_fixture():
            return {'check.service': {'terminal': 'check.service', 'aliases': [], 'vendor_mask': None,
                                      'mask': json.loads(json.dumps(before)), 'created_mask': True}}
        refreshed = route_fixture()
        systemd._recapture_routes(refreshed)
        assert refreshed['check.service']['mask']['parents'][0]['ctime_ns'] == 5
        after['parents'][0]['ino'] = 11
        expect_failure(lambda: systemd._recapture_routes(route_fixture()))
        after['parents'][0]['ino'] = 10
        after['ctime_ns'] = 6
        expect_failure(lambda: systemd._recapture_routes(route_fixture()))
        after['ctime_ns'] = 4
        after['parents'][0]['path'] = '/run/other'
        expect_failure(lambda: systemd._recapture_routes(route_fixture()))
    finally:
        (os.path.lexists, os.lstat, os.readlink, os.path.realpath, systemd._command,
         systemd._route_entry, systemd._sealed_routes, systemd._held_route, systemd._manager_unit_path,
         systemd._prepare_control_masks, systemd._reject_higher_unit, systemd._unit_path) = saved_fs

    # The socket's systemctl properties do not necessarily include the
    # service-only process fields. The service still requires explicit values.
    saved_multipath = (systemd._show, systemd._jobs, systemd._files, systemd._closed_route,
                       systemd._last_output)
    service_name, socket_name = systemd.MULTIPATH, systemd.MULTIPATH_SOCKET
    states = {service_name: 'masked-runtime', socket_name: 'masked-runtime'}
    masks = {name: '/run/systemd/system/' + name for name in states}
    properties = {
        service_name: {'Id': service_name, 'Names': service_name, 'LoadState': 'masked',
                       'UnitFileState': states[service_name], 'FragmentPath': masks[service_name],
                       'RefuseManualStop': 'no', 'ActiveState': 'active', 'MainPID': '123',
                       'ControlPID': '0', 'ControlGroup': '/system.slice/multipathd.service', 'DropInPaths': ''},
        socket_name: {'Id': socket_name, 'Names': socket_name, 'LoadState': 'masked',
                      'UnitFileState': states[socket_name], 'FragmentPath': masks[socket_name],
                      'ActiveState': 'inactive'}}
    routes = {name: {'terminal': name, 'aliases': [], 'mask': {'path': masks[name]}} for name in states}
    def multipath_show(names, deadline):
        assert len(names) == 1
        systemd._last_output = ('show', b'fixed systemctl show', b'')
        return {names[0]: dict(properties[names[0]])}
    def multipath_route(name, state, files, deadline, *, require_quiet):
        assert not require_quiet and state == states[name] and files == states
        return routes[name]
    systemd._show = multipath_show
    systemd._jobs = lambda deadline: ()
    systemd._files = lambda deadline: dict(states)
    systemd._closed_route = multipath_route
    try:
        deadline = time.monotonic() + 5
        shown = systemd._multipath_show(deadline)
        assert shown[socket_name] == properties[socket_name]
        assert all(key not in shown[socket_name] for key in ('MainPID', 'ControlPID', 'ControlGroup'))
        systemd._quiet(socket_name, shown[socket_name])
        for field in ('MainPID', 'ControlPID', 'ControlGroup', 'DropInPaths'):
            value = properties[service_name].pop(field)
            try:
                expect_failure(lambda: systemd._multipath_show(deadline), 'service missing ' + field)
            finally:
                properties[service_name][field] = value
        for field, value in (('Id', 'other.service'), ('Names', service_name + ' other.service'),
                             ('LoadState', 'error'), ('UnitFileState', 'enabled'),
                             ('FragmentPath', '/run/unrelated.service'), ('RefuseManualStop', 'yes'),
                             ('ControlPID', '1'), ('DropInPaths', '/run/unexpected.conf')):
            original = properties[service_name][field]
            properties[service_name][field] = value
            try:
                expect_failure(lambda: systemd._multipath_show(deadline), 'service ' + field)
            finally:
                properties[service_name][field] = original
        for field, value in (('LoadState', 'error'), ('FragmentPath', '/run/unrelated.socket'),
                             ('ControlPID', '1')):
            properties[socket_name][field] = value
            try:
                expect_failure(lambda: systemd._multipath_show(deadline), 'socket ' + field)
            finally:
                if field == 'ControlPID':
                    del properties[socket_name][field]
                else:
                    properties[socket_name][field] = 'masked' if field == 'LoadState' else masks[socket_name]
        for field in ('MainPID', 'ControlPID'):
            invalid = dict(properties[socket_name], **{field: '1'})
            expect_failure(lambda: systemd._quiet(socket_name, invalid), 'socket ' + field)
        expect_failure(lambda: systemd._quiet(socket_name, dict(properties[socket_name], ControlGroup='../bad')))
        expect_failure(lambda: systemd._quiet(socket_name, dict(properties[socket_name], ActiveState='active')))
        for field, value in (('terminal', 'other.service'), ('aliases', [{'target_name': 'other.service'}])):
            original = routes[service_name][field]
            routes[service_name][field] = value
            try:
                expect_failure(lambda: systemd._multipath_show(deadline), 'service route ' + field)
            finally:
                routes[service_name][field] = original
        for name in (service_name, socket_name):
            state = states.pop(name)
            try:
                expect_failure(lambda: systemd._multipath_show(deadline), 'missing mask ' + name)
            finally:
                states[name] = state
        systemd._jobs = lambda deadline: (('1', socket_name),)
        expect_failure(lambda: systemd._multipath_show(deadline), 'socket pending job')
    finally:
        (systemd._show, systemd._jobs, systemd._files, systemd._closed_route,
         systemd._last_output) = saved_multipath

    # Exercise the actual seal's stop selection with a live allowed canonical
    # and a forbidden alias; neither the mask set nor stop batch may use it.
    saved_seal = (systemd._inventory, systemd._prepare_netplan_mask, systemd._install_masks,
                  systemd._command, systemd._recapture_routes, systemd._files, systemd._closed_route,
                  systemd._show, systemd._jobs, systemd._observe, systemd._seal_started,
                  systemd._sealed_routes, systemd._inventory_bytes, os.geteuid, os.getegid,
                  systemd._manager_unit_path, systemd._check_control_masks, systemd._unit_path,
                  systemd._stop_multipath)
    seal_commands, mask_sets = [], []
    canonical = {'Id': systemd.UDEV_CANONICAL, 'Names': systemd.UDEV_CANONICAL + ' ' + systemd.UDEV_ALIAS,
                 'RefuseManualStop': 'no', 'ActiveState': 'active'}
    other = {'Id': 'other.service', 'Names': 'other.service', 'RefuseManualStop': 'no', 'ActiveState': 'active'}
    files = {systemd.UDEV_CANONICAL: 'enabled', systemd.UDEV_ALIAS: 'alias', 'other.service': 'enabled'}
    systemd._inventory = lambda deadline, closed: (files, {systemd.UDEV_CANONICAL: canonical,
                                                           'other.service': other}, (), {}, None)
    systemd._prepare_netplan_mask = lambda forbidden, deadline: None
    def capture_masks(forbidden, deadline):
        mask_sets.append(set(forbidden))
        return {}
    systemd._install_masks = capture_masks
    systemd._unit_path = [systemd.CONTROL_ROOT, etc, runtime]
    systemd._manager_unit_path = lambda deadline: systemd._unit_path
    systemd._check_control_masks = lambda deadline, **kwargs: None
    systemd._command = lambda operation, argv, deadline: seal_commands.append((operation, list(argv))) or b''
    systemd._recapture_routes = lambda routes: None
    systemd._files = lambda deadline: files
    systemd._closed_route = lambda name, state, inventory, deadline, **kwargs: None
    systemd._show = lambda names, deadline: {name: {'ActiveState': 'inactive', 'MainPID': '0',
                                                   'ControlPID': '0', 'ControlGroup': ''} for name in names}
    systemd._jobs = lambda deadline: ()
    systemd._observe = lambda deadline: 'closed'
    systemd._stop_multipath = lambda deadline: set()
    systemd._seal_started = False
    os.geteuid = os.getegid = lambda: 0
    try:
        assert systemd.seal() == 'closed'
        assert mask_sets == [{systemd.UDEV_ALIAS, 'other.service'}]
        assert ('stop', ['stop', 'other.service']) in seal_commands
        assert all(systemd.UDEV_CANONICAL not in argv for _, argv in seal_commands)
    finally:
        (systemd._inventory, systemd._prepare_netplan_mask, systemd._install_masks,
         systemd._command, systemd._recapture_routes, systemd._files, systemd._closed_route,
         systemd._show, systemd._jobs, systemd._observe, systemd._seal_started,
         systemd._sealed_routes, systemd._inventory_bytes, os.geteuid, os.getegid,
         systemd._manager_unit_path, systemd._check_control_masks, systemd._unit_path,
         systemd._stop_multipath) = saved_seal

    saved_control = (systemd._control_masks, systemd._sealed_routes, systemd._route, systemd._route_entry,
                     systemd._held_route, systemd._check_control_masks, systemd._show, systemd._jobs)
    alias, target = 'vmtoolsd.service', 'open-vm-tools.service'
    path = systemd.CONTROL_ROOT + '/' + alias
    mask_entry = {'path': path, 'dev': 1, 'ino': 2, 'target': '/dev/null', 'parents': []}
    origin = {'terminal': target, 'aliases': [{'link': {'path': etc + '/' + alias, 'parents': []},
                                               'target_name': target, 'source': []}]}
    systemd._control_masks = {alias: {}}
    systemd._sealed_routes = {alias: {'terminal': alias, 'aliases': [], 'mask': mask_entry,
                                     'control_origin': origin}}
    systemd._route = lambda name, **kwargs: (alias, [], (systemd.CONTROL_ROOT, 1, 2))
    systemd._route_entry = lambda name: mask_entry
    systemd._held_route = lambda entries, name: nullcontext()
    systemd._check_control_masks = lambda deadline, **kwargs: None
    props = {'Id': alias, 'Names': alias, 'LoadState': 'masked', 'UnitFileState': 'masked-runtime',
             'FragmentPath': path, 'DropInPaths': '', 'ActiveState': 'inactive', 'MainPID': '0',
             'ControlPID': '0', 'ControlGroup': ''}
    target_props = dict(props, Id=target, Names=target)
    systemd._show = lambda names, deadline: {name: props if name == alias else target_props for name in names}
    systemd._jobs = lambda deadline: ()
    try:
        proof = systemd._closed_route(alias, 'masked-runtime', {alias: 'masked-runtime'}, time.monotonic() + 3)
        assert proof['control_origin'] == origin and proof['manager'][0]['id'] == alias
        for key, value in (('Id', target), ('Names', alias + ' ' + target), ('LoadState', 'error'),
                           ('UnitFileState', 'masked'), ('FragmentPath', '/run/systemd/system/' + target),
                           ('DropInPaths', '/run/extra.conf'), ('MainPID', '42')):
            before = props[key]
            props[key] = value
            expect_failure(lambda: systemd._closed_route(alias, 'masked-runtime', {alias: 'masked-runtime'},
                                                        time.monotonic() + 3))
            props[key] = before
    finally:
        (systemd._control_masks, systemd._sealed_routes, systemd._route, systemd._route_entry,
         systemd._held_route, systemd._check_control_masks, systemd._show, systemd._jobs) = saved_control

    # Exercise the actual host verifier with the raw systemctl transition,
    # rehashing every altered transition so rejection reaches its predicates.
    import sys
    sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    import source_verify

    class Host:
        def require(self, condition, message):
            if not condition:
                raise JournalError(message)

    unit = systemd.TMPFILES
    preflight = {key: '' for key in systemd.PROPERTIES}
    preflight.update(Id=unit, Names='"' + unit + '"', LoadState='loaded', UnitFileState='static',
                     ActiveState='active', SubState='exited', RefuseManualStop='yes', MainPID='0',
                     ControlPID='0', Type='oneshot', RemainAfterExit='yes', Restart='no',
                     FragmentPath='/usr/lib/systemd/system/' + unit,
                     RequiredBy=r'"dev-disk-by\\x2dlabel-BOOT.device" "boot.mount"')
    overridden = dict(preflight, RefuseManualStop='no', DropInPaths='"' + systemd.OVERRIDE + '"')
    mask = {'path': '/run/systemd/system/' + unit, 'dev': 1, 'ino': 2}
    masked = dict(overridden, LoadState='masked', FragmentPath=mask['path'])
    transition = {'preflight': preflight, 'overridden': overridden,
                  'fragment_identity': {'dev': 1, 'ino': 3, 'size': 123, 'mtime_ns': 4, 'ctime_ns': 5},
                  'fragment_sha256': 'a' * 64, 'override_directory_identity': [1, 4],
                  'override_file_identity': [1, 5], 'override_sha256': hashlib.sha256(systemd.OVERRIDE_BYTES).hexdigest(),
                  'masked_before_stop': {'mask': mask, 'properties': masked},
                  'masked_after_stop': {'mask': dict(mask),
                                        'properties': dict(masked, ActiveState='inactive', SubState='dead')}}
    def route_entry(path, target, ino=2):
        parents, current = [], ''
        for component in os.path.dirname(path).strip('/').split('/'):
            current += '/' + component
            parents.append({'path': current, 'dev': 1, 'ino': 10, 'mode': stat.S_IFDIR | 0o755,
                            'uid': 0, 'gid': 0, 'ctime_ns': 3})
        return {'path': path, 'dev': 1, 'ino': ino,
                'mode': (stat.S_IFREG | 0o644) if target is None else (stat.S_IFLNK | 0o777),
                'uid': 0, 'gid': 0, 'nlink': 1, 'ctime_ns': 3, 'target': target, 'parents': parents}

    terminal_mask = route_entry('/etc/systemd/system/target.service', '/dev/null', 12)
    alias_link = route_entry('/usr/lib/systemd/system/alias.service', 'target.service', 13)
    alias_source = route_entry('/usr/lib/systemd/system/target.service', None, 14)
    names = '"alias.service" "target.service"'
    manager = [{'name': name, 'id': 'target.service', 'names': names, 'load_state': 'masked',
                'fragment_path': terminal_mask['path']} for name in ('alias.service', 'target.service')]
    routes = [{'name': 'alias.service', 'state': 'masked-runtime', 'terminal': 'target.service',
               'mask': terminal_mask, 'aliases': [{'link': alias_link, 'target_name': 'target.service',
                                                    'source': [alias_source]}], 'manager': manager},
              {'name': unit, 'state': 'masked-runtime', 'terminal': unit,
               'mask': route_entry(mask['path'], '/dev/null'), 'aliases': [], 'manager': []},
              {'name': 'target.service', 'state': 'masked', 'terminal': 'target.service',
               'mask': terminal_mask, 'aliases': [], 'manager': []}]
    run_identity = {'dev': 1, 'ino': 10, 'mode': stat.S_IFDIR | 0o755, 'uid': 0, 'gid': 0,
                    'nlink': 2, 'size': 100, 'mtime_ns': 3, 'ctime_ns': 3}
    closure = {'allowed': sorted(systemd.ALLOWED), 'files': routes,
               'active': [['dbus.service', 'loaded', 'active', 'running']],
               'override_digest': source_verify._sha(transition),
               'unit_path': [systemd.CONTROL_ROOT, etc, runtime],
               'stop_preflight': {'unit': 'multipathd.service', 'disposition': 'already_quiet',
                                  'run_identity': run_identity, 'pid_file_present': False},
               'stop_transition': None}
    evidence = {'unit_count': 4, 'active_units': ['dbus.service'], 'override_digest': closure['override_digest'],
                'override_transition': transition, 'closure': closure, 'digest': source_verify._sha(closure)}
    source_verify._systemd_evidence(Host(), evidence)

    # Both closure branches bind every public /run ancestor to the observed
    # directory, including when all outer digests have been recomputed.
    stopped = json.loads(json.dumps(evidence))
    after = dict(run_identity)
    before = dict(after, size=101, mtime_ns=2, ctime_ns=2)
    stopped['closure']['stop_preflight'] = {'unit': 'multipathd.service', 'disposition': 'stop_required',
                                            'run_identity': before, 'pid_file_present': True}
    stopped['closure']['stop_transition'] = {
        'version': 1, 'unit': 'multipathd.service', 'entry': 'multipathd.pid',
        'actor': {'pid': 42, 'starttime': 123, 'executable_sha256': 'a' * 64},
        'before': before, 'after': after,
        'pid_file': {'dev': 1, 'ino': 11, 'mode': stat.S_IFREG | 0o644, 'uid': 0, 'gid': 0,
                     'size': 3, 'mtime_ns': 1, 'ctime_before_ns': 2, 'ctime_after_ns': 3,
                     'sha256': 'b' * 64, 'nlink_before': 1, 'nlink_after': 0},
        'event': {'kind': 'delete', 'actor_pid': 42, 'actor_starttime': 123,
                  'directory_handle_sha256': 'c' * 64, 'target_handle_sha256': 'd' * 64,
                  'raw_events_sha256': 'e' * 64},
        'settlement': {'stop_returncode': 0, 'tracer_wait_status': 0, 'pidfd_exited': True,
                       'tracer_joined': True, 'remaining_jobs': 0, 'cgroup_empty': True}}
    stopped['digest'] = source_verify._sha(stopped['closure'])
    source_verify._systemd_evidence(Host(), stopped)

    def reject_stop(change, candidate=stopped):
        altered = json.loads(json.dumps(candidate))
        change(altered['closure'])
        altered['digest'] = source_verify._sha(altered['closure'])
        expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    reject_stop(lambda c: c.update(stop_transition=None))
    reject_stop(lambda c: c['stop_transition'].update(entry='other.pid'))
    reject_stop(lambda c: c['stop_transition']['event'].update(actor_pid=43))
    reject_stop(lambda c: c['stop_transition']['event'].update(actor_starttime=124))
    reject_stop(lambda c: c['stop_transition']['after'].update(ctime_ns=4))
    reject_stop(lambda c: c['stop_transition']['after'].update(ino=12))
    reject_stop(lambda c: c['stop_transition']['pid_file'].update(nlink_after=1))
    reject_stop(lambda c: c['stop_transition']['event'].update(target_handle_sha256='0' * 64))
    reject_stop(lambda c: c['stop_transition']['settlement'].update(tracer_wait_status=9))
    reject_stop(lambda c: c['stop_transition']['settlement'].update(tracer_joined=False))
    reject_stop(lambda c: c['stop_preflight'].update(pid_file_present=False))
    reject_stop(lambda c: c['stop_preflight']['run_identity'].update(ctime_ns=3))
    reject_stop(lambda c: c['stop_preflight'].update(pid_file_present=True), evidence)
    reject_stop(lambda c: c['stop_preflight']['run_identity'].update(ctime_ns=4), evidence)
    reject_stop(lambda c: c.update(stop_transition=stopped['closure']['stop_transition']), evidence)

    def reject_host_array(stage, key, value):
        altered = json.loads(json.dumps(evidence))
        candidate = altered['override_transition']
        props = candidate[stage] if stage in ('preflight', 'overridden') else candidate[stage]['properties']
        props[key] = value
        altered['override_digest'] = source_verify._sha(candidate)
        altered['closure']['override_digest'] = altered['override_digest']
        altered['digest'] = source_verify._sha(altered['closure'])
        expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    for stage in ('preflight', 'overridden', 'masked_before_stop', 'masked_after_stop'):
        reject_host_array(stage, 'RequiredBy', '"dbus.service"')
        reject_host_array(stage, 'RequiredBy', '"bad\\q.service"')
        reject_host_array(stage, 'Names', '"' + unit + '" ' + unit)
    reject_host_array('overridden', 'DropInPaths', '"' + systemd.OVERRIDE + '" ' + systemd.OVERRIDE)
    reject_host_array('masked_before_stop', 'DropInPaths', '"/run/unterminated')

    def reject_closure(change):
        altered = json.loads(json.dumps(evidence))
        change(altered)
        altered['digest'] = source_verify._sha(altered['closure'])
        expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    reject_closure(lambda value: value['closure']['allowed'].pop())
    reject_closure(lambda value: value['closure']['files'].reverse())
    reject_closure(lambda value: value['closure']['files'].pop())
    reject_closure(lambda value: value['closure']['files'][0]['aliases'][0].update(target_name='dbus.service'))
    reject_closure(lambda value: value['closure']['files'][0]['aliases'][0]['link'].update(target='alias.service'))
    reject_closure(lambda value: value['closure']['files'][0]['aliases'][0]['source'][0].update(mode=stat.S_IFREG | 0o666))
    reject_closure(lambda value: value['closure']['files'][0]['mask']['parents'][0].update(mode=stat.S_IFDIR | 0o777))
    reject_closure(lambda value: value['closure']['files'][0]['mask'].update(target='target.service'))
    reject_closure(lambda value: value['closure']['files'][0]['manager'][0].update(id='alias.service'))
    reject_closure(lambda value: value['closure']['files'][0]['manager'][0].update(names='"alias.service"'))
    reject_closure(lambda value: value['closure']['files'][0]['manager'][0].update(fragment_path='/usr/lib/systemd/system/target.service'))
    reject_closure(lambda value: value['closure']['files'][1]['mask'].update(ino=999))
    reject_closure(lambda value: value['closure']['files'][2].update(state='masked-runtime'))
    reject_closure(lambda value: value['closure']['active'][0].__setitem__(0, 'alias.service'))
    netplan = {'name': systemd.NETPLAN, 'state': 'masked', 'terminal': systemd.NETPLAN,
               'mask': route_entry(systemd.NETPLAN_MASK, '/dev/null', 20), 'aliases': [],
               'manager': [{'name': systemd.NETPLAN, 'id': systemd.NETPLAN, 'names': systemd.NETPLAN,
                            'load_state': 'masked', 'fragment_path': systemd.NETPLAN_MASK}]}
    net_evidence = json.loads(json.dumps(evidence))
    net_evidence['closure']['files'].append(netplan)
    net_evidence['closure']['files'].sort(key=lambda route: route['name'])
    net_evidence['unit_count'] += 1
    net_evidence['digest'] = source_verify._sha(net_evidence['closure'])
    source_verify._systemd_evidence(Host(), net_evidence)

    def reject_netplan(change):
        altered = json.loads(json.dumps(net_evidence))
        route = next(item for item in altered['closure']['files'] if item['name'] == systemd.NETPLAN)
        change(route)
        altered['digest'] = source_verify._sha(altered['closure'])
        expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    reject_netplan(lambda route: route.update(manager=[]))
    reject_netplan(lambda route: route['manager'].append(dict(route['manager'][0])))
    reject_netplan(lambda route: route['manager'][0].update(id='other.service'))
    reject_netplan(lambda route: route['manager'][0].update(names=systemd.NETPLAN + ' other.service'))
    reject_netplan(lambda route: route['manager'][0].update(load_state='loaded'))
    reject_netplan(lambda route: route['manager'][0].update(fragment_path=systemd.NETPLAN_RUNTIME))
    reject_netplan(lambda route: route.update(state='masked-runtime'))
    reject_netplan(lambda route: route['mask'].update(target='other.service'))
    reject_netplan(lambda route: route['mask']['parents'][-1].update(ino=999))
    reject_closure(lambda value: value['closure']['files'][2].update(manager=[netplan['manager'][0]]))
    control = json.loads(json.dumps(evidence))
    control_route = control['closure']['files'][0]
    control_route['control_origin'] = {'terminal': control_route['terminal'], 'aliases': control_route['aliases']}
    control_route['terminal'] = control_route['name']
    control_route['aliases'] = []
    control_route['mask'] = route_entry(systemd.CONTROL_ROOT + '/alias.service', '/dev/null', 31)
    control_route['manager'] = [{'name': 'alias.service', 'id': 'alias.service', 'names': 'alias.service',
                                'load_state': 'masked', 'fragment_path': control_route['mask']['path']}]
    control['digest'] = source_verify._sha(control['closure'])
    source_verify._systemd_evidence(Host(), control)

    def reject_control(change):
        altered = json.loads(json.dumps(control))
        change(altered)
        altered['digest'] = source_verify._sha(altered['closure'])
        expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    reject_control(lambda v: v['closure']['files'][0].pop('control_origin'))
    reject_control(lambda v: v['closure']['files'][0]['control_origin'].update(aliases=[]))
    reject_control(lambda v: v['closure']['files'][0]['control_origin'].update(terminal='dbus.service'))
    reject_control(lambda v: v['closure']['files'][0]['control_origin']['aliases'][0].update(target_name='other.service'))
    reject_control(lambda v: v['closure']['files'][0]['manager'][0].update(load_state='error'))
    reject_control(lambda v: v['closure']['files'][0]['manager'][0].update(id='target.service'))
    reject_control(lambda v: v['closure']['files'][0]['manager'][0].update(names='alias.service target.service'))
    reject_control(lambda v: v['closure']['files'][0]['manager'][0].update(fragment_path=terminal_mask['path']))
    reject_control(lambda v: v['closure']['files'][2].update(control_origin={'terminal': 'target.service', 'aliases': []}))
    reject_control(lambda v: v['closure']['unit_path'].append(systemd.CONTROL_ROOT))
    reject_control(lambda v: v['closure']['unit_path'].remove(systemd.CONTROL_ROOT))
    reject_control(lambda v: v['closure']['unit_path'].reverse())
    altered = json.loads(json.dumps(evidence))
    altered['digest'] = 'b' * 64
    expect_failure(lambda: source_verify._systemd_evidence(Host(), altered))

    def reject_capability(value, expected):
        try:
            source_verify._negative(Host(), 'observer-capability', {'negative': value}, {}, reboot_result=None)
        except JournalError as exc:
            assert expected in str(exc), str(exc)
            return
        raise AssertionError('invalid observer capability was accepted')

    capability = {'observations': [], 'maintenance_smoke':
                  ['maintenance-protocol', 'maintenance-records', 'maintenance-witness'], 'systemd': evidence}
    reject_capability(capability, 'observer capability matrix differs')
    reject_capability({'observations': [], 'maintenance_smoke': capability['maintenance_smoke']},
                      'observer capability evidence schema differs')
    bad_capability = json.loads(json.dumps(capability))
    bad_capability['systemd']['digest'] = 'b' * 64
    reject_capability(bad_capability, 'systemd closure digest or schema differs')


def systemd_run_stop_failure(scratch):
    """Exercise fanotify and the ptrace exit gate against actual forked actors."""
    import origins
    import select
    import signal
    import sys
    import run_stop_observer

    deadline = time.monotonic() + 24

    def prepare_transition(directory):
        import systemd_realm as systemd
        saved = {name: getattr(systemd, name) for name in (
            'NETPLAN_RUNTIME', 'NETPLAN_MASK', '_netplan_mask', '_show', '_jobs', '_reject_higher_unit',
            '_control_directory', '_control_masks', '_sealed_routes', '_unit_path', '_manager_unit_path',
            '_stop_observer', '_stop_transition_committed')}
        opened = []
        def close():
            state = systemd._netplan_mask
            if state is not None:
                for fd in (state['runtime_fd'], state['mask_fd']):
                    os.close(fd)
                for key in ('etc', 'runtime'):
                    for _, fd, _, _, _ in state[key]:
                        os.close(fd)
            if systemd._control_directory is not None:
                for _, fd, _, _, _ in systemd._control_directory['parents']:
                    os.close(fd)
            for fd in opened:
                os.close(fd)
            for name, value in saved.items():
                setattr(systemd, name, value)
        systemd._netplan_mask = None
        systemd._control_directory = None

        try:
            etc, runtime, control = (os.path.join(directory, name) for name in ('etc', 'runtime', 'control'))
            for path in (etc, runtime, control):
                os.mkdir(path, 0o700)
            systemd.NETPLAN_RUNTIME = os.path.join(runtime, systemd.NETPLAN)
            systemd.NETPLAN_MASK = os.path.join(etc, systemd.NETPLAN)
            with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                stream.write(b'x' * 337)
            os.chmod(systemd.NETPLAN_RUNTIME, 0o644)
            control_name = 'vmtoolsd.service'
            os.symlink('/dev/null', os.path.join(control, control_name))
            for name in ('mask.service', 'vendor.service', 'protected.service', 'alias.service',
                         'source.service', 'terminal.service'):
                os.symlink('/dev/null', os.path.join(directory, name))
            props = {'Id': systemd.NETPLAN, 'Names': systemd.NETPLAN, 'LoadState': 'loaded',
                     'UnitFileState': 'enabled-runtime', 'FragmentPath': systemd.NETPLAN_RUNTIME,
                     'RefuseManualStop': 'no', 'ActiveState': 'inactive', 'MainPID': '0', 'ControlPID': '0',
                     'DropInPaths': '', 'ControlGroup': ''}
            systemd._show = lambda names, until: {systemd.NETPLAN: props}
            systemd._jobs = lambda until: ()
            systemd._reject_higher_unit = lambda name: None
            systemd._netplan_mask = None
            systemd._prepare_netplan_mask({systemd.NETPLAN}, deadline)
            parents = systemd._netplan_parents(os.path.join(control, control_name))
            systemd._control_directory = {'parents': parents}
            control_fd = os.open(os.path.join(control, control_name), os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC)
            opened.append(control_fd)
            run_stat = os.stat(directory, follow_symlinks=False)

            def entry(path):
                info = os.lstat(path)
                parent = {'path': '/run', 'dev': run_stat.st_dev, 'ino': run_stat.st_ino,
                          'mode': run_stat.st_mode, 'uid': run_stat.st_uid, 'gid': run_stat.st_gid,
                          'ctime_ns': run_stat.st_ctime_ns}
                return {'path': path, 'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode,
                        'uid': info.st_uid, 'gid': info.st_gid, 'nlink': info.st_nlink,
                        'ctime_ns': info.st_ctime_ns, 'target': os.readlink(path), 'parents': [parent]}

            def item(name):
                return entry(os.path.join(directory, name))
            origin = {'terminal': 'terminal.service', 'aliases': [{'link': item('alias.service'),
                      'target_name': 'terminal.service', 'source': [item('source.service')]}]}
            control_entry = entry(os.path.join(control, control_name))
            route = {'mask': control_entry, 'vendor_mask': item('vendor.service'),
                     'protected_alias': item('protected.service'), 'aliases': [
                         {'link': item('mask.service'), 'target_name': 'terminal.service',
                          'source': [item('source.service')]}], 'control_origin': origin}
            systemd._sealed_routes = {control_name: route,
                                     'terminal.service': {'mask': item('terminal.service'), 'aliases': []}}
            systemd._control_masks = {control_name: {'fd': control_fd,
                                      'identity': systemd._netplan_identity(os.fstat(control_fd)), 'origin': origin}}
            systemd._unit_path = ['/fixture']
            systemd._manager_unit_path = lambda until: systemd._unit_path
            for chain in (systemd._netplan_mask['etc'], systemd._netplan_mask['runtime'],
                          systemd._control_directory['parents']):
                for index, (path, fd, parent, part, info) in enumerate(chain):
                    if path == '/run':
                        # Keep the real ancestor held and checked without granting
                        # it the scratch directory's transition authority.
                        chain[index] = ('fixture-real-run', fd, parent, part, info)
                    elif path == directory:
                        chain[index] = ('/run', fd, parent, part, info)
                mapped = [item for item in chain if item[0] == '/run']
                assert len(mapped) == 1 and systemd._run_identity(mapped[0][4]) == systemd._run_identity(run_stat)
            systemd._stop_transition_committed = False
            return systemd, route, close
        except BaseException:
            close()
            raise

    def exercise(scenario):
        with tempfile.TemporaryDirectory(prefix='run-stop-', dir=scratch) as directory:
            run_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            ready_r, ready_w = os.pipe2(os.O_CLOEXEC)
            go_r, go_w = os.pipe2(os.O_CLOEXEC)
            pid = os.fork()
            if pid == 0:
                try:
                    os.close(ready_r)
                    os.close(go_w)
                    if scenario == 'already-traced':
                        libc = ctypes.CDLL(None, use_errno=True)
                        if libc.ptrace(0, 0, None, None) != 0:
                            os._exit(5)
                    if scenario == 'worker-exec':
                        import threading
                        proceed = threading.Event()
                        started = threading.Event()
                        def worker():
                            started.set()
                            proceed.wait()
                            os.execl(sys.executable, sys.executable, '-c', 'import os,sys; os.unlink(sys.argv[1])',
                                     os.path.join(directory, 'multipathd.pid'))
                        thread = threading.Thread(target=worker)
                        thread.start()
                        started.wait()
                    os.write(ready_w, b'R')
                    os.close(ready_w)
                    if os.read(go_r, 1) != b'G':
                        os._exit(3)
                    if scenario == 'worker-exec':
                        proceed.set()
                        thread.join()
                    elif scenario == 'leader-exec':
                        os.execl(sys.executable, sys.executable, '-c', 'import os,sys; os.unlink(sys.argv[1])',
                                 os.path.join(directory, 'multipathd.pid'))
                    elif scenario == 'fork':
                        child = os.fork()
                        if child == 0:
                            os._exit(0)
                        os.waitpid(child, 0)
                    elif scenario == 'clone':
                        import threading
                        thread = threading.Thread(target=lambda: None)
                        thread.start()
                        thread.join()
                    elif scenario not in ('wrong-actor', 'missing', 'early-exit'):
                        os.unlink('multipathd.pid', dir_fd=run_fd)
                    os._exit(0)
                except BaseException:
                    os._exit(4)
            os.close(ready_w)
            os.close(go_r)
            observer = None
            fixture_close = None
            reaped = False
            try:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([ready_r], [], [], remaining)[0] or os.read(ready_r, 1) != b'R':
                    raise JournalError('run-stop fixture actor did not reach its barrier')
                actor = origins._identity(pid, full=False)
                file_fd = os.open('multipathd.pid', os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC,
                                  0o644, dir_fd=run_fd)
                try:
                    os.fchmod(file_fd, 0o644)
                    os.write(file_fd, ('%d\n' % pid).encode('ascii'))
                    os.fsync(file_fd)
                    initial = os.fstat(file_fd)
                    if scenario in ('transition', 'partial-chain', 'parent-replacement', 'transition-after'):
                        systemd, route, fixture_close = prepare_transition(directory)
                    if scenario == 'normal':
                        # Make the ctime change observable, rather than assuming clock resolution.
                        time.sleep(0.05)
                    observer = run_stop_observer.RunStopObserver(run_fd, file_fd, actor, deadline)
                    if scenario == 'early-exit':
                        os.write(go_w, b'G')
                        _, status = os.waitpid(pid, 0)
                        reaped = True
                        assert os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0
                        expect_failure(observer.arm, scenario)
                        return
                    if scenario == 'already-traced':
                        expect_failure(observer.arm, scenario)
                        return
                    observer.arm()
                    if scenario == 'exit-gate':
                        import threading
                        drain = observer._drain
                        def fail_exit_gate(*args, **kwargs):
                            if threading.current_thread().name == 'run-stop-tracer':
                                raise JournalError('injected tracer exit-gate failure')
                            return drain(*args, **kwargs)
                        observer._drain = fail_exit_gate
                    if scenario == 'wrong-actor':
                        os.unlink('multipathd.pid', dir_fd=run_fd)
                    elif scenario == 'temporary-entry':
                        fd = os.open('unexpected', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600, dir_fd=run_fd)
                        os.close(fd)
                        os.unlink('unexpected', dir_fd=run_fd)
                    elif scenario == 'rewrite':
                        os.pwrite(file_fd, b'X', 0)
                    elif scenario == 'replace':
                        os.unlink('multipathd.pid', dir_fd=run_fd)
                        fd = os.open('multipathd.pid', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o644, dir_fd=run_fd)
                        os.close(fd)
                    elif scenario == 'chmod-restore':
                        os.fchmod(file_fd, 0o600)
                        os.fchmod(file_fd, 0o644)
                    os.write(go_w, b'G')
                    if scenario not in ('normal', 'transition', 'after-snapshot', 'partial-chain',
                                         'parent-replacement', 'transition-after'):
                        expect_failure(observer.finish, scenario)
                        return
                    proof = observer.finish()
                    if scenario == 'after-snapshot':
                        fd = os.open('unexpected', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600, dir_fd=run_fd)
                        os.close(fd)
                        expect_failure(observer.close, scenario)
                        return
                    final = os.fstat(file_fd)
                    if scenario == 'normal':
                        assert initial.st_ctime_ns != final.st_ctime_ns
                    assert (initial.st_dev, initial.st_ino, initial.st_mode, initial.st_uid, initial.st_gid,
                            initial.st_size, initial.st_mtime_ns, initial.st_nlink) == (
                                final.st_dev, final.st_ino, final.st_mode, final.st_uid, final.st_gid,
                                final.st_size, final.st_mtime_ns, 1)
                    assert final.st_nlink == 0 and os.pread(file_fd, initial.st_size, 0) == ('%d\n' % pid).encode('ascii')
                    assert proof.actor['pid'] == pid and proof.actor['starttime'] == actor['starttime']
                    assert proof.event['actor_pid'] == pid and proof.event['actor_starttime'] == actor['starttime']
                    assert proof.pid_file['nlink_before'] == 1 and proof.pid_file['nlink_after'] == 0
                    assert proof.settlement['pidfd_exited'] and proof.settlement['tracer_joined']
                    if fixture_close is not None:
                        for chain in (systemd._netplan_mask['runtime'], systemd._netplan_mask['etc'],
                                      systemd._control_directory['parents']):
                            mapped = [item for item in chain if item[0] == '/run']
                            assert len(mapped) == 1
                            _, fd, parent, part, original = mapped[0]
                            assert systemd._run_identity(original) == proof.before
                            assert systemd._run_identity(os.fstat(fd)) == proof.after
                            assert systemd._run_identity(os.stat(part, dir_fd=parent, follow_symlinks=False)) == proof.after
                        systemd._stop_observer = observer
                        original_open, original_stat, original_lstat = os.open, os.stat, os.lstat
                        def mapped_open(path, *args, **kwargs):
                            return original_open(directory if path == '/run' else path, *args, **kwargs)
                        def mapped_stat(path, *args, **kwargs):
                            return original_stat(directory if path == '/run' else path, *args, **kwargs)
                        def mapped_lstat(path, *args, **kwargs):
                            return original_lstat(directory if path == '/run' else path, *args, **kwargs)
                        os.open, os.stat, os.lstat = mapped_open, mapped_stat, mapped_lstat
                        renamed = False
                        try:
                            if scenario == 'parent-replacement':
                                os.rename(directory, directory + '-old')
                                os.mkdir(directory, 0o700)
                                renamed = True
                                expect_failure(lambda: systemd._commit_run_stop_transition(proof, deadline), scenario)
                            elif scenario == 'partial-chain':
                                parent = route['vendor_mask']['parents'][0]
                                parent['ctime_ns'] += 1
                                unaffected = route['mask']['parents'][0]['ctime_ns']
                                expect_failure(lambda: systemd._commit_run_stop_transition(proof, deadline), scenario)
                                assert route['mask']['parents'][0]['ctime_ns'] == unaffected
                                assert not systemd._stop_transition_committed
                            else:
                                systemd._commit_run_stop_transition(proof, deadline)
                                for candidate in systemd._stop_route_entries(route):
                                    assert candidate['parents'][0]['ctime_ns'] == proof.after['ctime_ns']
                                for chain in (systemd._netplan_mask['runtime'], systemd._netplan_mask['etc'],
                                              systemd._control_directory['parents']):
                                    assert all(info.st_ctime_ns == proof.after['ctime_ns']
                                               for path, _, _, _, info in chain if path == '/run')
                                if scenario == 'transition-after':
                                    fd = os.open('unexpected', os.O_CREAT | os.O_EXCL | os.O_WRONLY,
                                                 0o600, dir_fd=run_fd)
                                    os.close(fd)
                                    expect_failure(observer.close, scenario)
                                    return
                        finally:
                            os.open, os.stat, os.lstat = original_open, original_stat, original_lstat
                            if renamed:
                                os.rmdir(directory)
                                os.rename(directory + '-old', directory)
                        if scenario in ('partial-chain', 'parent-replacement'):
                            return
                    observer.close()
                finally:
                    primary = sys.exc_info()[1]
                    cleanup_errors = []
                    if observer is not None:
                        try:
                            observer.close()
                        except (JournalError, OSError) as exc:
                            if scenario == 'normal':
                                cleanup_errors.append(exc)
                        if any(thread is not None and thread.is_alive()
                               for thread in (observer.reader, observer.tracer)):
                            cleanup_errors.append(JournalError('run-stop fixture left observer threads running'))
                    for cleanup in ((fixture_close,) if fixture_close is not None else ()) + (lambda: os.close(file_fd),):
                        try:
                            cleanup()
                        except Exception as exc:
                            cleanup_errors.append(exc)
                    if cleanup_errors:
                        if primary is not None:
                            for error in cleanup_errors:
                                primary.add_note('fixture cleanup failed: %r' % error)
                        else:
                            for error in cleanup_errors[1:]:
                                cleanup_errors[0].add_note('additional fixture cleanup failure: %r' % error)
                            raise cleanup_errors[0]
            finally:
                primary = sys.exc_info()[1]
                cleanup_errors = []
                for fd in (ready_r, go_w):
                    try:
                        os.close(fd)
                    except OSError as exc:
                        cleanup_errors.append(exc)
                try:
                    status = None
                    if (observer is not None and observer.tracer is not None and
                            observer.trace_done and not observer.tracer.is_alive()):
                        status = observer.terminal.get(pid)
                    if status is not None:
                        if not (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                            raise JournalError('tracer wait did not record actor termination')
                    elif not reaped:
                        # A missing child is not proof of a tracer wait; require
                        # the actual status from one of the two wait owners.
                        result, status = os.waitpid(pid, os.WNOHANG)
                        if not result:
                            os.kill(pid, signal.SIGKILL)
                            result, status = os.waitpid(pid, 0)
                        if result != pid or not (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                            raise JournalError('fixture actor wait did not settle')
                except (OSError, JournalError) as exc:
                    cleanup_errors.append(exc)
                try:
                    os.close(run_fd)
                except OSError as exc:
                    cleanup_errors.append(exc)
                if cleanup_errors:
                    if primary is not None:
                        for error in cleanup_errors:
                            primary.add_note('fixture cleanup failed: %r' % error)
                    else:
                        for error in cleanup_errors[1:]:
                            cleanup_errors[0].add_note('additional fixture cleanup failure: %r' % error)
                        raise cleanup_errors[0]

    def run_case(scenario):
        try:
            exercise(scenario)
        except Exception as exc:
            raise JournalError('run-stop fixture %s failed' % scenario) from exc

    run_case('normal')
    import struct
    # Synthetic records only test the parser. The positive above requires
    # actual kernel events, including a kernel-supplied PIDFD.
    def record(mask, pidfd, *, metadata_fd=-1, version=3, extra=b''):
        info = struct.pack('=BBHi', 4, 0, 8, pidfd) + extra
        return struct.pack('=IBBHQii', 24 + len(info), version, 0, 24,
                           mask, metadata_fd, os.getpid()) + info
    pidfd = os.pidfd_open(os.getpid(), 0)
    try:
        decoded = run_stop_observer._parse_events(record(0x200, pidfd))
        assert decoded[0][2][4] == pidfd
        os.close(decoded[0][2][4])
        pidfd = None
    finally:
        if pidfd is not None:
            os.close(pidfd)
    for invalid in (record(0x200, -1), record(0x200, -2), record(0x4000, -1),
                    record(0x200, -1, version=2), record(0x200, -1)[:-1],
                    record(0x200, -1, extra=struct.pack('=BBHi', 4, 0, 8, -1))):
        expect_failure(lambda invalid=invalid: run_stop_observer._parse_events(invalid))
    metadata_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
    expect_failure(lambda: run_stop_observer._parse_events(record(0x200, -1, metadata_fd=metadata_fd)))
    expect_failure(lambda: os.fstat(metadata_fd))
    duplicate_a, duplicate_b = os.pidfd_open(os.getpid(), 0), os.pidfd_open(os.getpid(), 0)
    duplicate = record(0x200, duplicate_a, extra=struct.pack('=BBHi', 4, 0, 8, duplicate_b))
    expect_failure(lambda: run_stop_observer._parse_events(duplicate))
    expect_failure(lambda: os.fstat(duplicate_a))
    expect_failure(lambda: os.fstat(duplicate_b))
    wrong_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
    try:
        expect_failure(lambda: run_stop_observer._pidfd_pid(wrong_fd))
    finally:
        os.close(wrong_fd)
    for scenario in ('transition', 'wrong-actor', 'temporary-entry', 'rewrite', 'replace', 'chmod-restore',
                     'missing', 'early-exit', 'already-traced', 'leader-exec', 'worker-exec', 'fork', 'clone',
                     'exit-gate', 'after-snapshot', 'partial-chain', 'parent-replacement', 'transition-after'):
        run_case(scenario)


def systemd_failure(scratch):
    """Persist the original failure under an Ubuntu-shaped log directory."""
    import systemd_realm as systemd

    if os.geteuid() != 0 or os.getegid() != 0:
        raise JournalError('systemd failure smoke requires root in an isolated guest or user namespace')
    systemd_run_stop_failure(scratch)
    with tempfile.TemporaryDirectory(prefix='g2-systemd-failure-', dir=scratch) as directory:
        os.chown(directory, 0, 102)
        os.chmod(directory, 0o775)
        original = systemd.FAILURE
        systemd.FAILURE = os.path.join(directory, 'g2-systemd-seal-failure.json')
        try:
            failure = systemd._Failure('show', 'parse', ('open-iscsi.service',), stdout=b'original stdout',
                                       stderr=b'original stderr')
            digest = systemd._persist_failure(failure)
            with open(systemd.FAILURE, 'rb') as evidence:
                data = evidence.read()
            info = os.stat(systemd.FAILURE, follow_symlinks=False)
            assert (info.st_uid, info.st_gid, info.st_mode & 0o777, info.st_nlink) == (0, 0, 0o600, 1)
            assert digest == hashlib.sha256(data).hexdigest()
            parsed = json.loads(data)
            assert parsed['stdout_base64'] == 'b3JpZ2luYWwgc3Rkb3V0'
            assert parsed['stderr_base64'] == 'b3JpZ2luYWwgc3RkZXJy'
            expect_failure(lambda: systemd._persist_failure(failure))
            mask_root = os.path.join(directory, 'units')
            os.mkdir(mask_root, 0o700)
            os.symlink('/dev/null', os.path.join(mask_root, 'broken.service'))
            linked_root = os.path.join(directory, 'linked-units')
            os.symlink(mask_root, linked_root)
            absent_root = os.path.join(directory, 'absent-units')
            saved = (systemd.MASK_FAILURE, systemd.UNIT_PATHS, systemd.REJECT_UNIT_PATHS,
                     systemd._command, systemd._failure_summary, systemd._last_output, systemd._inventory_bytes)
            systemd.MASK_FAILURE = os.path.join(directory, 'g2-systemd-mask-paths.json')
            systemd.UNIT_PATHS, systemd.REJECT_UNIT_PATHS = (mask_root, linked_root), (absent_root,)
            systemd._failure_summary = {'artifact_sha256': digest}
            systemd._last_output, systemd._inventory_bytes = ('original', b'out', b'err'), 17
            commands = []
            def diagnostic_show(operation, argv, deadline):
                commands.append((operation, argv))
                return (b'Id=broken.service\nNames=broken.service\nFragmentPath=/dev/null\n'
                        b'LoadState=masked\nUnitFileState=masked\nMainPID=0\nControlPID=0\nControlGroup=\n')
            systemd._command = diagnostic_show
            try:
                error = systemd._Failure('mask-readback', 'predicate', ('broken.service',))
                systemd._record_mask_diagnostic(error, time.monotonic() + 5)
                with open(systemd.MASK_FAILURE, 'rb') as evidence:
                    mask_data = evidence.read()
                mask_info = os.stat(systemd.MASK_FAILURE, follow_symlinks=False)
                assert (mask_info.st_uid, mask_info.st_gid, mask_info.st_mode & 0o777, mask_info.st_nlink) == (0, 0, 0o600, 1)
                diagnostic = json.loads(mask_data)
                assert diagnostic['primary_sha256'] == digest and diagnostic['units'] == ['broken.service']
                assert diagnostic['paths'][0]['entries'][0]['target'] == '/dev/null'
                assert diagnostic['paths'][1]['parents'][-1]['target'] == mask_root
                assert diagnostic['paths'][1]['entries'][0]['state'] == 'blocked'
                assert diagnostic['paths'][2]['entries'][0]['state'] == 'absent'
                assert diagnostic['systemctl_show']['rows'][0]['properties']['Id'] == 'broken.service'
                assert len(commands) == 1 and commands[0][0] == 'mask-diagnostic-show'
                assert commands[0][1] == ['show', '--all', '--no-pager',
                                          '--property=Id,Names,FragmentPath,LoadState,UnitFileState,MainPID,ControlPID,ControlGroup',
                                          '--', 'broken.service']
                assert systemd._last_output == ('original', b'out', b'err') and systemd._inventory_bytes == 17
                with open(systemd.FAILURE, 'rb') as original_evidence:
                    assert original_evidence.read() == data
            finally:
                (systemd.MASK_FAILURE, systemd.UNIT_PATHS, systemd.REJECT_UNIT_PATHS,
                 systemd._command, systemd._failure_summary, systemd._last_output, systemd._inventory_bytes) = saved
            os.unlink(systemd.FAILURE)
            os.chmod(directory, 0o777)
            expect_failure(lambda: systemd._persist_failure(failure))
            os.chmod(directory, 0o775)
            original_stat = os.stat
            def replace_before_readback(path, *args, **kwargs):
                if path == 'g2-systemd-seal-failure.json' and 'dir_fd' in kwargs:
                    os.unlink(path, dir_fd=kwargs['dir_fd'])
                    replacement = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600,
                                          dir_fd=kwargs['dir_fd'])
                    os.close(replacement)
                return original_stat(path, *args, **kwargs)
            systemd.os.stat = replace_before_readback
            try:
                expect_failure(lambda: systemd._persist_failure(failure))
            finally:
                systemd.os.stat = original_stat
        finally:
            systemd.FAILURE = original


    # Each fixture has its own exclusive mask path and keeps the actual dirfds.
    saved = (systemd.NETPLAN_RUNTIME, systemd.NETPLAN_MASK, systemd._netplan_mask,
             systemd._show, systemd._jobs, systemd._reject_higher_unit)
    original_symlink, original_fsync = os.symlink, os.fsync
    try:
        def exercise(scenario):
            with tempfile.TemporaryDirectory(prefix='netplan-', dir=scratch) as directory:
                etc, runtime = os.path.join(directory, 'etc'), os.path.join(directory, 'runtime')
                os.mkdir(etc, 0o700)
                os.mkdir(runtime, 0o700)
                systemd.NETPLAN_MASK = os.path.join(etc, systemd.NETPLAN)
                systemd.NETPLAN_RUNTIME = os.path.join(runtime, systemd.NETPLAN)
                systemd._netplan_mask = None
                data = b'x' * 337
                with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                    stream.write(data)
                os.chmod(systemd.NETPLAN_RUNTIME, 0o644)
                props = {'Id': systemd.NETPLAN, 'Names': systemd.NETPLAN, 'LoadState': 'loaded',
                         'UnitFileState': 'enabled-runtime', 'FragmentPath': systemd.NETPLAN_RUNTIME,
                         'RefuseManualStop': 'no', 'ActiveState': 'inactive', 'MainPID': '0',
                         'ControlPID': '0', 'DropInPaths': '', 'ControlGroup': ''}
                systemd._show = lambda names, deadline: {systemd.NETPLAN: props}
                systemd._jobs = lambda deadline: ()
                systemd._reject_higher_unit = lambda name: None
                deadline = time.monotonic() + 10
                try:
                    if scenario == 'existing':
                        os.symlink('/dev/null', systemd.NETPLAN_MASK)
                    elif scenario == 'existing-dangling':
                        os.symlink('/nonexistent', systemd.NETPLAN_MASK)
                    elif scenario == 'existing-file':
                        with open(systemd.NETPLAN_MASK, 'wb') as stream:
                            stream.write(b'preserve')
                    elif scenario == 'existing-directory':
                        os.mkdir(systemd.NETPLAN_MASK)
                    elif scenario == 'not-forbidden':
                        systemd._prepare_netplan_mask({'other.service'}, deadline)
                        assert not os.path.lexists(systemd.NETPLAN_MASK)
                        return
                    elif scenario == 'runtime-absent':
                        os.unlink(systemd.NETPLAN_RUNTIME)
                        systemd._prepare_netplan_mask({systemd.NETPLAN}, deadline)
                        assert not os.path.lexists(systemd.NETPLAN_MASK)
                        return
                    elif scenario == 'race':
                        def racing_symlink(target, name, **kwargs):
                            original_symlink('/dev/null', name, **kwargs)
                            return original_symlink(target, name, **kwargs)
                        os.symlink = racing_symlink
                    elif scenario == 'unsafe-mode':
                        os.chmod(systemd.NETPLAN_RUNTIME, 0o666)
                    elif scenario == 'unsafe-owner':
                        os.chown(systemd.NETPLAN_RUNTIME, 0, 102)
                    elif scenario == 'fsync-failure':
                        def failing_fsync(fd):
                            raise OSError('injected fsync failure')
                        os.fsync = failing_fsync
                    elif scenario == 'wrong-size':
                        with open(systemd.NETPLAN_RUNTIME, 'ab') as stream:
                            stream.write(b'x')
                    elif scenario == 'hardlink':
                        os.link(systemd.NETPLAN_RUNTIME, os.path.join(runtime, 'other'))
                    elif scenario == 'unsafe-parent':
                        os.chmod(runtime, 0o777)
                    elif scenario == 'alias':
                        props['Names'] += ' other.service'
                    elif scenario == 'job':
                        systemd._jobs = lambda deadline: (('1', systemd.NETPLAN),)
                    elif scenario == 'dropin':
                        props['DropInPaths'] = '/run/extra.conf'
                    elif scenario == 'stop':
                        props['ExecStop'] = '{ path=/bin/true }'
                    elif scenario == 'active':
                        props['ActiveState'] = 'active'
                    elif scenario == 'higher':
                        systemd._reject_higher_unit = lambda name: systemd._fail('mask-readback', units=(name,))
                    if scenario in ('existing', 'existing-dangling', 'existing-file', 'existing-directory',
                                    'race', 'unsafe-mode', 'unsafe-owner', 'fsync-failure',
                                    'wrong-size', 'hardlink', 'unsafe-parent', 'alias', 'job', 'dropin',
                                    'stop', 'active', 'higher'):
                        expect_failure(lambda: systemd._prepare_netplan_mask({systemd.NETPLAN}, deadline))
                        if scenario in ('race', 'fsync-failure'):
                            assert os.readlink(systemd.NETPLAN_MASK) == '/dev/null'
                        return
                    systemd._prepare_netplan_mask({systemd.NETPLAN}, deadline)
                    assert os.readlink(systemd.NETPLAN_MASK) == '/dev/null'
                    systemd._check_netplan_mask(deadline)
                    if scenario in ('rebuild', 'changed', 'missing', 'unsafe-rebuild'):
                        os.unlink(systemd.NETPLAN_RUNTIME)
                        if scenario != 'missing':
                            with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                                stream.write(b'y' * 337 if scenario == 'changed' else data)
                            os.chmod(systemd.NETPLAN_RUNTIME, 0o666 if scenario == 'unsafe-rebuild' else 0o644)
                        if scenario != 'rebuild':
                            expect_failure(lambda: systemd._check_netplan_mask(deadline, after_reload=True))
                            return
                    systemd._check_netplan_mask(deadline, after_reload=True)
                    systemd._check_netplan_mask(deadline)
                    if scenario == 'drift':
                        os.unlink(systemd.NETPLAN_RUNTIME)
                        with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                            stream.write(data)
                        os.chmod(systemd.NETPLAN_RUNTIME, 0o644)
                        expect_failure(lambda: systemd._check_netplan_mask(deadline))
                    elif scenario == 'parent-replaced':
                        os.rename(runtime, runtime + '-old')
                        os.mkdir(runtime, 0o700)
                        expect_failure(lambda: systemd._check_netplan_mask(deadline))
                finally:
                    os.symlink, os.fsync = original_symlink, original_fsync
                    state = systemd._netplan_mask
                    if state is not None:
                        os.close(state['runtime_fd'])
                        os.close(state['mask_fd'])
                        for group in ('etc', 'runtime'):
                            for _, fd, _, _, _ in state[group]:
                                os.close(fd)
                    systemd._netplan_mask = None
        for scenario in ('normal', 'rebuild', 'changed', 'missing', 'unsafe-rebuild', 'drift',
                         'parent-replaced', 'existing', 'existing-dangling', 'existing-file',
                         'existing-directory', 'not-forbidden', 'runtime-absent', 'race', 'unsafe-mode',
                         'unsafe-owner', 'fsync-failure', 'wrong-size', 'hardlink', 'unsafe-parent',
                         'alias', 'job', 'dropin', 'stop', 'active', 'higher'):
            exercise(scenario)
    finally:
        os.symlink, os.fsync = original_symlink, original_fsync
        (systemd.NETPLAN_RUNTIME, systemd.NETPLAN_MASK, systemd._netplan_mask,
         systemd._show, systemd._jobs, systemd._reject_higher_unit) = saved

    saved = (systemd.UDEV_VENDOR, systemd.UNIT_PATHS, systemd.REJECT_UNIT_PATHS, systemd._show)
    try:
        with tempfile.TemporaryDirectory(prefix='udev-alias-', dir=scratch) as directory:
            etc, runtime, vendor = (os.path.join(directory, name) for name in ('etc', 'run', 'vendor'))
            for root in (etc, runtime, vendor):
                os.mkdir(root, 0o700)
            systemd.UNIT_PATHS = (etc, runtime, vendor)
            systemd.REJECT_UNIT_PATHS = (os.path.join(directory, 'higher'),)
            systemd.UDEV_VENDOR = os.path.join(vendor, systemd.UDEV_ALIAS)
            os.symlink(systemd.UDEV_CANONICAL, systemd.UDEV_VENDOR)
            canonical = {'Id': systemd.UDEV_CANONICAL, 'Names': systemd.UDEV_ALIAS + ' ' + systemd.UDEV_CANONICAL,
                         'LoadState': 'loaded'}
            systemd._show = lambda names, deadline: {systemd.UDEV_CANONICAL: canonical}
            deadline = time.monotonic() + 10
            entry = systemd._udev_vendor_alias(deadline)
            assert entry['path'] == systemd.UDEV_VENDOR and entry['target'] == systemd.UDEV_CANONICAL
            for root in (etc, runtime):
                conflict = os.path.join(root, systemd.UDEV_ALIAS)
                os.symlink('/dev/null', conflict)
                expect_failure(lambda: systemd._udev_vendor_alias(deadline))
                os.unlink(conflict)
            systemd._show = lambda names, deadline: {systemd.UDEV_ALIAS: dict(canonical, Id=systemd.UDEV_ALIAS)}
            expect_failure(lambda: systemd._udev_vendor_alias(deadline))
            systemd._show = lambda names, deadline: {systemd.UDEV_CANONICAL: canonical}
            os.unlink(systemd.UDEV_VENDOR)
            os.symlink(systemd.UDEV_CANONICAL, systemd.UDEV_VENDOR)
            def check_original():
                with systemd._held_route([entry], systemd.UDEV_ALIAS):
                    pass
            expect_failure(check_original)
            os.chown(systemd.UDEV_VENDOR, 0, 102, follow_symlinks=False)
            expect_failure(lambda: systemd._udev_vendor_alias(deadline))
    finally:
        (systemd.UDEV_VENDOR, systemd.UNIT_PATHS, systemd.REJECT_UNIT_PATHS, systemd._show) = saved
    systemd_control_failure(scratch)


def systemd_control_failure(scratch):
    """Exercise exclusive control masks and the netplan parent transition with real dirfds."""
    import systemd_realm as systemd
    saved = (systemd.CONTROL_ROOT, systemd.NETPLAN_RUNTIME, systemd.NETPLAN_MASK, systemd.UNIT_PATHS,
             systemd.REJECT_UNIT_PATHS, systemd._netplan_mask, systemd._control_masks,
             systemd._control_directory, systemd._unit_path, systemd._manager_unit_path,
             systemd._show, systemd._jobs, systemd._sealed_routes)
    original_symlink, original_mkdir, original_fsync = os.symlink, os.mkdir, os.fsync
    try:
        def exercise(scenario):
            with tempfile.TemporaryDirectory(prefix='control-', dir=scratch) as directory:
                etc = os.path.join(directory, 'etc')
                base = os.path.join(directory, 'systemd')
                runtime, vendor = os.path.join(base, 'system'), os.path.join(directory, 'vendor')
                for path in (etc, base, runtime, vendor):
                    os.mkdir(path, 0o700)
                systemd.CONTROL_ROOT = os.path.join(base, 'system.control')
                systemd.NETPLAN_RUNTIME = os.path.join(runtime, systemd.NETPLAN)
                systemd.NETPLAN_MASK = os.path.join(etc, systemd.NETPLAN)
                systemd.UNIT_PATHS = (etc, runtime, vendor)
                systemd.REJECT_UNIT_PATHS = (systemd.CONTROL_ROOT,)
                systemd._netplan_mask, systemd._control_masks = None, {}
                systemd._control_directory, systemd._unit_path, systemd._sealed_routes = None, None, None
                props = {'Id': systemd.NETPLAN, 'Names': systemd.NETPLAN, 'LoadState': 'loaded',
                         'UnitFileState': 'enabled-runtime', 'FragmentPath': systemd.NETPLAN_RUNTIME,
                         'RefuseManualStop': 'no', 'ActiveState': 'inactive', 'MainPID': '0',
                         'ControlPID': '0', 'DropInPaths': '', 'ControlGroup': ''}
                systemd._show = lambda names, deadline: {systemd.NETPLAN: props}
                systemd._jobs = lambda deadline: ()
                with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                    stream.write(b'x' * 337)
                os.chmod(systemd.NETPLAN_RUNTIME, 0o644)
                alias, target = 'vmtoolsd.service', 'open-vm-tools.service'
                os.symlink(os.path.join(vendor, target), os.path.join(etc, alias))
                with open(os.path.join(vendor, target), 'wb') as stream:
                    stream.write(b'[Service]\n')
                deadline = time.monotonic() + 10
                try:
                    systemd._prepare_netplan_mask({systemd.NETPLAN}, deadline)
                    link = systemd._route_entry(os.path.join(etc, alias))
                    source = systemd._alias_source(os.path.join(vendor, target), alias)
                    routes = {alias: {'terminal': target, 'aliases': [{'link': link, 'target_name': target,
                                                                      'source': source}]}}
                    if scenario in ('existing-empty', 'existing-drift', 'nonempty', 'existing-mask'):
                        os.mkdir(systemd.CONTROL_ROOT, 0o700)
                        # Creating an existing fixture directory happened before its held netplan baseline.
                        systemd._netplan_mask['runtime'] = systemd._netplan_check_parents(
                            systemd._netplan_mask['runtime'], (base,))
                    if scenario == 'nonempty':
                        with open(os.path.join(systemd.CONTROL_ROOT, 'unrelated'), 'wb') as stream:
                            stream.write(b'keep')
                    elif scenario == 'existing-mask':
                        os.symlink('/dev/null', os.path.join(systemd.CONTROL_ROOT, alias))
                    elif scenario in ('pre-drift', 'existing-drift'):
                        baseline = next(info.st_ctime_ns for path, _, _, _, info in
                                        systemd._netplan_mask['runtime'] if path == base)
                        # Separate the fixture's baseline and mutation across filesystem clock ticks.
                        time.sleep(0.05)
                        with open(os.path.join(base, 'unexpected'), 'wb') as stream:
                            stream.write(b'drift')
                        # Directory ctime may have coarse granularity; require an actual
                        # changed timestamp before asking the held-identity checker to reject it.
                        if os.stat(base, follow_symlinks=False).st_ctime_ns == baseline:
                            raise AssertionError(scenario + ': directory ctime did not advance')
                    elif scenario == 'netplan-bytes':
                        with open(systemd.NETPLAN_RUNTIME, 'r+b') as stream:
                            stream.write(b'y')
                    elif scenario == 'netplan-inode':
                        os.unlink(systemd.NETPLAN_RUNTIME)
                        with open(systemd.NETPLAN_RUNTIME, 'wb') as stream:
                            stream.write(b'x' * 337)
                        os.chmod(systemd.NETPLAN_RUNTIME, 0o644)
                    elif scenario == 'unsafe-parent':
                        os.chmod(base, 0o777)
                    elif scenario == 'mkdir-failure':
                        def fail_mkdir(path, mode=0o777, **kwargs):
                            if path == os.path.basename(systemd.CONTROL_ROOT):
                                raise FileExistsError('injected race')
                            return original_mkdir(path, mode, **kwargs)
                        os.mkdir = fail_mkdir
                    elif scenario == 'mkdir-fsync-failure':
                        def fail_fsync(fd):
                            raise OSError('injected fsync failure')
                        os.fsync = fail_fsync
                    elif scenario == 'base-replaced':
                        os.rename(base, base + '-old')
                        os.mkdir(base, 0o700)
                    elif scenario == 'symlink-race':
                        def race(target_value, name, **kwargs):
                            original_symlink('/dev/null', name, **kwargs)
                            return original_symlink(target_value, name, **kwargs)
                        os.symlink = race
                    failure = scenario in ('nonempty', 'existing-mask', 'pre-drift', 'existing-drift',
                                           'netplan-bytes', 'netplan-inode', 'unsafe-parent',
                                           'mkdir-failure', 'mkdir-fsync-failure', 'base-replaced', 'symlink-race')
                    if failure:
                        expect_failure(lambda: systemd._prepare_control_masks(routes, {alias, target}, deadline),
                                       scenario)
                        return
                    systemd._unit_path = [systemd.CONTROL_ROOT, etc, runtime]
                    systemd._manager_unit_path = lambda deadline: systemd._unit_path
                    systemd._prepare_control_masks(routes, {alias, target}, deadline)
                    assert os.readlink(os.path.join(systemd.CONTROL_ROOT, alias)) == '/dev/null'
                    # Ordinary runtime mask creation has a separate, narrow parent transition.
                    os.symlink('/dev/null', os.path.join(runtime, target))
                    systemd._netplan_mask['runtime'] = systemd._netplan_check_parents(
                        systemd._netplan_mask['runtime'], (runtime,))
                    systemd._check_netplan_mask(deadline)
                    systemd._check_control_masks(deadline)
                    if scenario in ('control-inode', 'reload-drift'):
                        if scenario == 'reload-drift':
                            systemd._check_netplan_mask(deadline, after_reload=True)
                        os.unlink(os.path.join(systemd.CONTROL_ROOT, alias))
                        os.symlink('/dev/null', os.path.join(systemd.CONTROL_ROOT, alias))
                    elif scenario == 'extra-entry':
                        os.symlink('/dev/null', os.path.join(systemd.CONTROL_ROOT, 'extra.service'))
                    elif scenario == 'alias-inode':
                        os.unlink(os.path.join(etc, alias))
                        os.symlink(os.path.join(vendor, target), os.path.join(etc, alias))
                    elif scenario == 'alias-target':
                        os.unlink(os.path.join(etc, alias))
                        os.symlink('other.service', os.path.join(etc, alias))
                    elif scenario == 'source-inode':
                        os.unlink(os.path.join(vendor, target))
                        with open(os.path.join(vendor, target), 'wb') as stream:
                            stream.write(b'[Service]\n')
                    elif scenario == 'parent-replaced':
                        os.rename(systemd.CONTROL_ROOT, systemd.CONTROL_ROOT + '-old')
                        os.mkdir(systemd.CONTROL_ROOT, 0o700)
                    elif scenario == 'higher-entry':
                        higher = os.path.join(directory, 'higher')
                        os.mkdir(higher, 0o700)
                        os.symlink('/dev/null', os.path.join(higher, alias))
                        systemd._unit_path.append(higher)
                        expect_failure(lambda: systemd._reject_higher_unit(alias))
                    if scenario in ('control-inode', 'reload-drift', 'extra-entry', 'alias-inode',
                                    'alias-target', 'source-inode', 'parent-replaced'):
                        expect_failure(lambda: systemd._check_control_masks(deadline), scenario)
                finally:
                    os.symlink, os.mkdir, os.fsync = original_symlink, original_mkdir, original_fsync
                    for item in systemd._control_masks.values():
                        os.close(item['fd'])
                    if systemd._control_directory is not None:
                        for _, fd, _, _, _ in systemd._control_directory['parents']:
                            os.close(fd)
                    state = systemd._netplan_mask
                    if state is not None:
                        os.close(state['runtime_fd'])
                        os.close(state['mask_fd'])
                        for group in ('etc', 'runtime'):
                            for _, fd, _, _, _ in state[group]:
                                os.close(fd)
        for scenario in ('normal', 'existing-empty', 'nonempty', 'existing-mask', 'pre-drift', 'existing-drift',
                         'netplan-bytes', 'netplan-inode', 'unsafe-parent', 'mkdir-failure', 'mkdir-fsync-failure',
                         'base-replaced', 'symlink-race', 'control-inode', 'extra-entry', 'alias-inode',
                         'alias-target', 'source-inode', 'reload-drift', 'parent-replaced', 'higher-entry'):
            exercise(scenario)
    finally:
        os.symlink, os.mkdir, os.fsync = original_symlink, original_mkdir, original_fsync
        (systemd.CONTROL_ROOT, systemd.NETPLAN_RUNTIME, systemd.NETPLAN_MASK, systemd.UNIT_PATHS,
         systemd.REJECT_UNIT_PATHS, systemd._netplan_mask, systemd._control_masks,
         systemd._control_directory, systemd._unit_path, systemd._manager_unit_path,
         systemd._show, systemd._jobs, systemd._sealed_routes) = saved


class _FixturePreparation:
    """Observe initialization in the disposable namespace until route handoff."""

    def __init__(self, deadline, identities):
        self.deadline = min(deadline, time.monotonic() + 5)
        self.identities = identities
        self.progress = {name: {'ready': False, 'up': False, 'carrier': False,
                                'oper_up': False, 'forwarding': False,
                                'bridge_state': None, 'startup_closed': False,
                                'startup_disable_pending': False}
                         for name in identities}
        self.nsfd = None
        self.sock = None
        self.count = 0
        self.size = 0
        self.events = []
        self.rejected_notification = None
        self.final_reads = set()

    def __enter__(self):
        try:
            self.nsfd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
            st = os.fstat(self.nsfd)
            self.namespace = (st.st_dev, st.st_ino)
            self.sock = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW | socket.SOCK_CLOEXEC,
                                      socket.NETLINK_ROUTE)
            self.sock.setblocking(False)
            self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 1048576)
            if self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF) < 262144:
                raise JournalError('fixture preparation receive buffer too small')
            # RTNLGRP_LINK=1 and RTNLGRP_IPV6_IFINFO=12 are group numbers.
            self.sock.bind((0, (1 << (1 - 1)) | (1 << (12 - 1))))
            self.check_namespace()
            return self
        except BaseException:
            self.close()
            raise

    def close(self):
        if self.sock is not None:
            self.sock.close()
            self.sock = None
        if self.nsfd is not None:
            os.close(self.nsfd)
            self.nsfd = None

    def __exit__(self, *_):
        self.close()

    def check_namespace(self):
        held, named = os.fstat(self.nsfd), os.stat('/proc/self/ns/net')
        if ((held.st_dev, held.st_ino) != self.namespace or
                (named.st_dev, named.st_ino) != self.namespace):
            raise JournalError('fixture preparation namespace changed')

    def remaining(self):
        value = self.deadline - time.monotonic()
        if value <= 0:
            raise JournalError('fixture preparation deadline expired')
        return value

    def receive(self):
        import network_verify as network
        self.remaining()
        try:
            data, ancillary, flags, sender = self.sock.recvmsg(65536)
        except BlockingIOError:
            return False
        except OSError as exc:
            raise JournalError('fixture preparation receive failed') from exc
        if ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or not sender or sender[0] != 0:
            raise JournalError('invalid fixture preparation datagram')
        messages = network._route_messages(data)
        self.count += len(messages)
        self.size += len(data)
        if self.count > 4096 or self.size > 1048576:
            raise JournalError('fixture preparation message bound exceeded')
        for kind, msgflags, sequence, _port, body in messages:
            if kind != 16 or sequence != 0 or msgflags & 0x10:
                raise JournalError('unexpected fixture preparation notification')
            previous = {name: dict(state) for name, state in self.progress.items()}
            try:
                self.message(body)
            except BaseException:
                try:
                    import struct
                    record = {'body_length': len(body), 'body_hex': body[:4096].hex(),
                              'body_truncated': len(body) > 4096,
                              'attributes_hex': body[16:4096].hex(),
                              'previous_progress': previous,
                              'kind': kind, 'message_flags': msgflags, 'sequence': sequence}
                    if len(body) >= 16:
                        family, _, hardware, index, link_flags, change = struct.unpack_from('=BBHiII', body)
                        record.update(family=family, index=index, hardware=hardware,
                                      link_flags=link_flags, change=change)
                        try:
                            attrs = network._attributes(body[16:])
                            names, protinfo = attrs.get(3, []), attrs.get(12, [])
                            record['name_count'], record['protinfo_count'] = len(names), len(protinfo)
                            record['names'] = [value[:128].decode('ascii', 'backslashreplace')
                                               for value in names[:8]]
                            record['protinfo_hex'] = [value[:512].hex() for value in protinfo[:8]]
                        except BaseException:
                            record['attribute_decode_failed'] = True
                    self.rejected_notification = record
                except BaseException:
                    pass
                raise
        return True

    def drain(self):
        while self.receive():
            pass
        if self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR):
            raise JournalError('fixture preparation socket error')
        self.check_namespace()

    def message(self, body):
        import struct
        import network_verify as network
        if len(body) < 16:
            raise JournalError('short fixture link notification')
        family, _, hardware, index, flags, _ = struct.unpack_from('=BBHiII', body)
        if family not in (0, 10, 7):
            raise JournalError('unexpected fixture link family')
        attrs = network._attributes(body[16:])
        get = network._link_attribute
        name = network._link_string(get(attrs, 3, required=True))
        if name not in self.identities or index != self.identities[name][0]:
            raise JournalError('fixture link identity changed')
        expected = self.identities[name]
        if name == 'lo' and hardware != 772:
            raise JournalError('fixture loopback identity changed')
        master = network._link_index(get(attrs, 10))
        peer = network._link_index(get(attrs, 5))
        if master is not None and master != expected[2]:
            raise JournalError('fixture link master changed')
        if peer is not None and peer != expected[3]:
            raise JournalError('fixture link peer changed')
        info = get(attrs, 18)
        if info is not None:
            value = get(network._attributes(info), 1)
            if value is not None and network._link_string(value) != expected[1]:
                raise JournalError('fixture link kind changed')
        previous = self.progress[name]
        state = dict(previous)
        up = bool(flags & 1)
        if state['up'] and not up:
            raise JournalError('fixture link went down')
        state['up'] |= up
        carrier = get(attrs, 33)
        if carrier is not None:
            if len(carrier) != 1 or carrier[0] not in (0, 1):
                raise JournalError('invalid fixture carrier')
            if state['carrier'] and not carrier[0]:
                raise JournalError('fixture carrier regressed')
            state['carrier'] |= bool(carrier[0])
        oper = get(attrs, 16)
        if oper is not None:
            if len(oper) != 1 or oper[0] > 6:
                raise JournalError('invalid fixture operstate')
            if state['oper_up'] and oper[0] != 6:
                raise JournalError('fixture operstate regressed')
            state['oper_up'] |= oper[0] == 6
        if family == 10:
            proto = get(attrs, 12, required=True)
            value = get(network._attributes(proto), 1, required=True)
            if len(value) != 4:
                raise JournalError('invalid fixture IPv6 flags')
            ready = bool(struct.unpack('=I', value)[0] & 0x80000000)
            if state['ready'] and not ready:
                raise JournalError('fixture IPv6 readiness regressed')
            state['ready'] |= ready
        state['startup_closed'] |= state['ready'] or state['carrier'] or state['oper_up']
        if family == 7:
            if name not in ('vethaa', 'vethbb') or expected[1] != 'veth' or master != expected[2]:
                raise JournalError('invalid fixture bridge port identity')
            port = get(attrs, 12, required=True)
            port_state = get(network._attributes(port), 1, required=True)
            if len(port_state) != 1 or port_state[0] not in (0, 1, 2, 3, 4):
                raise JournalError('invalid fixture bridge port state')
            value = port_state[0]
            initial_disabled = (value == 0 and not previous['forwarding'] and
                                previous['up'] and up and previous['carrier'] and
                                not state['ready'] and not state['oper_up'])
            if state['startup_closed'] and value != 3 and not initial_disabled:
                raise JournalError('fixture bridge port regressed')
            if initial_disabled:
                state['startup_disable_pending'] = True
            if state['forwarding'] and value != 3:
                if (value != 0 or state['startup_closed'] or not previous['up'] or not up or
                        (carrier is not None and carrier[0] != 0) or
                        (oper is not None and oper[0] == 6)):
                    raise JournalError('fixture bridge port regressed')
                state['startup_disable_pending'] = True
            if value == 3:
                state['forwarding'] = True
                state['startup_disable_pending'] = False
            state['bridge_state'] = value
        if len(self.events) >= 4096:
            raise JournalError('fixture preparation diagnostic bound exceeded')
        self.progress[name] = state
        self.events.append((family, name, flags, dict(state)))

    def all_ready(self):
        return all(state['ready'] for name, state in self.progress.items() if name != 'lo')

    def wait_ready(self):
        import select
        while not self.all_ready():
            if not select.select([self.sock], [], [], self.remaining())[0]:
                raise JournalError('fixture IPv6 readiness timed out')
            self.drain()

    def require_bridge_forwarding(self):
        names = {name for name, identity in self.identities.items() if identity[1] == 'veth'}
        if names != {'vethaa', 'vethbb'} or any(
                self.progress[name]['bridge_state'] != 3 or
                not self.progress[name]['forwarding'] or
                self.progress[name]['startup_disable_pending'] for name in names):
            raise JournalError('fixture bridge forwarding incomplete')

    def final_link(self, body):
        import struct
        import network_verify as network
        if not self.all_ready() or len(body) < 16 or body[0] != socket.AF_UNSPEC:
            raise JournalError('fixture final link before readiness')
        self.require_bridge_forwarding()
        name, identity = network._link_message(body)
        if name == 'lo' or name in self.final_reads or self.identities.get(name) != identity:
            raise JournalError('fixture final link identity mismatch')
        _, _, _, _, flags, _ = struct.unpack_from('=BBHiII', body)
        attrs = network._attributes(body[16:])
        get = network._link_attribute
        if not flags & 1 or get(attrs, 33, required=True) != b'\x01' or get(attrs, 16, required=True) != b'\x06':
            raise JournalError('fixture final link is not up')
        af = network._attributes(get(attrs, 26, required=True))
        ipv6 = network._attributes(get(af, 10, required=True))
        value = get(ipv6, 1, required=True)
        if len(value) != 4 or not struct.unpack('=I', value)[0] & 0x80000000 or get(ipv6, 8, required=True) != b'\x01':
            raise JournalError('fixture final IPv6 state invalid')
        self.progress[name].update(up=True, carrier=True, oper_up=True, ready=True, startup_closed=True)
        self.final_reads.add(name)


def _fixture_route_reply(messages, header, sequence, kind, dump, done, result):
    """Validate one datagram without treating a reply as an ACK or dump end."""
    import struct
    for typ, flags, received, _, body in messages:
        if received != sequence or flags & 0x10:
            raise JournalError('fixture netlink sequence or dump differs')
        if typ == 2 and not dump:
            if done or len(body) < 20 or struct.unpack_from('=i', body)[0] != 0 or body[4:20] != header:
                raise JournalError('fixture netlink request failed: ' + repr({
                    'request_header': header.hex(), 'reply_flags': flags,
                    'reply_length': len(body), 'reply_prefix': body[:256].hex()}))
            done = True
        elif typ == 3 and dump:
            if done or len(body) not in (0, 4) or (body and struct.unpack('=i', body)[0] != 0):
                raise JournalError('fixture netlink dump failed')
            done = True
        elif ((kind == 18 and typ == 16) or (kind == 26 and typ == 24 and dump)) and (not done or (kind == 18 and not dump)):
            result.append((typ, body))
            if kind == 18 and not dump and len(result) > 1:
                raise JournalError('duplicate fixture GETLINK reply')
        else:
            raise JournalError('unexpected fixture netlink response')
    return done


def _fixture_route_body(body, interface_index, *, request):
    import struct
    import network_verify as network
    if len(body) < 12 or struct.unpack_from('=BBBBBBBBI', body) != (10, 64, 0, 0, 254, 3, 0, 1, 0):
        raise JournalError('fixture route body differs')
    attrs = network._attributes(body[12:])
    if any(len(values) != 1 for values in attrs.values()):
        raise JournalError('duplicate fixture route attribute')
    if request and set(attrs) != {1, 4} or not request and 5 in attrs:
        raise JournalError('fixture route attributes differ')
    if (network._link_attribute(attrs, 1, required=True) != bytes.fromhex('20010db8' + '00' * 12) or
            network._link_index(network._link_attribute(attrs, 4, required=True)) != interface_index):
        raise JournalError('fixture route target differs')
    table = network._link_attribute(attrs, 15)
    if table is not None and (len(table) != 4 or struct.unpack('=I', table)[0] != 254):
        raise JournalError('fixture route table differs')


def _fixture_route_evidence(value, interface_index, namespace):
    import struct
    import network_verify as network
    def integer(number, maximum):
        return type(number) is int and 0 < number <= maximum
    if (type(namespace) not in (tuple, list) or len(namespace) != 2 or
            type(interface_index) is not int or interface_index <= 0 or
            type(value) is not dict or set(value) != {'version', 'namespace', 'exchanges'} or
            type(value['version']) is not int or value['version'] != 1 or
            type(value['namespace']) is not list or len(value['namespace']) != 2 or
            not all(integer(item, (1 << 64) - 1) for item in value['namespace']) or
            tuple(value['namespace']) != tuple(namespace) or
            type(value['exchanges']) is not list or len(value['exchanges']) != 2):
        raise JournalError('invalid fixture route evidence')
    for exchange, kind, flags in zip(value['exchanges'], (24, 25), (0x605, 0x5)):
        if (type(exchange) is not dict or set(exchange) != {'port', 'request_hex', 'replies'} or
                not integer(exchange['port'], 0xffffffff) or type(exchange['request_hex']) is not str or
                len(exchange['request_hex']) > 8192 or type(exchange['replies']) is not list or
                not 1 <= len(exchange['replies']) <= 4):
            raise JournalError('invalid fixture route exchange')
        try:
            data = bytes.fromhex(exchange['request_hex'])
        except ValueError as exc:
            raise JournalError('invalid fixture request encoding') from exc
        if data.hex() != exchange['request_hex'] or len(data) < 28 or len(data) > 4096:
            raise JournalError('invalid fixture request bytes')
        length, actual_kind, actual_flags, seq, port = struct.unpack_from('=IHHII', data)
        if (length, actual_kind, actual_flags, seq, port) != (len(data), kind, flags, 1, 0):
            raise JournalError('fixture request header differs')
        _fixture_route_body(data[16:], interface_index, request=True)
        done = False
        for reply in exchange['replies']:
            if (type(reply) is not dict or set(reply) != {'data_hex', 'sender', 'flags', 'ancillary'} or
                    type(reply['data_hex']) is not str or len(reply['data_hex']) > 8192 or
                    type(reply['sender']) is not list or reply['sender'] != [0, 0] or
                    any(type(item) is not int for item in reply['sender']) or
                    type(reply['flags']) is not int or not 0 <= reply['flags'] <= 0xffffffff or
                    reply['flags'] & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or
                    type(reply['ancillary']) is not bool or reply['ancillary']):
                raise JournalError('invalid fixture reply metadata')
            try:
                raw = bytes.fromhex(reply['data_hex'])
            except ValueError as exc:
                raise JournalError('invalid fixture reply encoding') from exc
            if raw.hex() != reply['data_hex'] or len(raw) > 4096:
                raise JournalError('invalid fixture reply bytes')
            messages = network._route_messages(raw)
            if len(messages) != 1 or messages[0][0] != 2:
                raise JournalError('unexpected fixture ACK messages')
            done = _fixture_route_reply(messages, data[:16], seq, kind, False, done, [])
        if not done:
            raise JournalError('fixture route ACK missing')
    return value['exchanges']


def _fixture_evidence_packet(data, ancillary, flags, interface_index, namespace):
    if not data or len(data) > 49152 or ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC):
        raise JournalError('fixture evidence packet invalid')
    def unique(pairs):
        result = {}
        for key, item in pairs:
            if key in result:
                raise JournalError('duplicate fixture evidence key')
            result[key] = item
        return result
    try:
        value = json.loads(data.decode('ascii'), object_pairs_hook=unique)
    except (ValueError, UnicodeError) as exc:
        raise JournalError('invalid fixture evidence JSON') from exc
    _fixture_route_evidence(value, interface_index, namespace)
    return value


def _fixture_evidence_eof(channel):
    data, ancillary, flags, _ = channel.recvmsg(49152)
    if data or ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC):
        raise JournalError('extra fixture evidence packet')


class _FixtureReceiveTap:
    def __init__(self, sock):
        self.sock = sock
        self.result = None
        self.calls = 0

    def __getattr__(self, name):
        return getattr(self.sock, name)

    def recvmsg(self, *args, **kwargs):
        self.calls += 1
        if self.calls != 1:
            raise JournalError('fixture receive invoked more than once')
        result = self.sock.recvmsg(*args, **kwargs)
        if len(result[0]) > 65536:
            raise JournalError('fixture receive exceeds bound')
        self.result = result
        return result


def _fixture_rejected_notification(error, received, scenario, interface_index, *, writer_evidence=None, namespace=None):
    """Match the exact message rejected by the production observer."""
    import struct
    import network_verify as network
    if not received:
        raise JournalError('fixture mutation produced no observed notification')
    if scenario in ('route', 'late'):
        exchanges = _fixture_route_evidence(writer_evidence, interface_index, namespace)
        sequence, messages, raw = received[-1]
        data, ancillary, flags, address = raw
        if (address != (0, 0x400) or ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or
                network._route_messages(data) != messages or not messages):
            raise JournalError('fixture multicast evidence differs')
        message = messages[0]
        typ, msgflags, seq, port, body = message
        if str(error) == 'route topology changed or unexpected sequence':
            if seq == sequence or msgflags & 0x10:
                raise JournalError('fixture first message did not cause rejection')
        elif str(error) != 'route topology changed after final observation':
            raise error
        if typ not in (24, 25):
            raise JournalError('fixture multicast type differs')
        exchange = exchanges[typ - 24]
        if (msgflags, seq, port) != ((0x600 if typ == 24 else 0), 1, exchange['port']):
            raise JournalError('fixture multicast writer differs')
        _fixture_route_body(body, interface_index, request=False)
        return message
    sequence, messages = received[-1]
    if str(error) == 'route topology changed or unexpected sequence':
        message = next((item for item in messages if item[2] != sequence), None)
    elif str(error) == 'route topology changed after final observation':
        message = messages[0] if messages else None
    else:
        raise error
    if message is None:
        raise JournalError('fixture rejection has no attributable message')
    typ, msgflags, seq, _port, body = message
    if seq != 0 or msgflags & 0x10:
        raise JournalError('fixture rejection is not an asynchronous notification')
    if typ in (24, 25):
        if len(body) < 12 or body[0] != socket.AF_INET6:
            raise JournalError('fixture rejection is not an IPv6 route event')
        attrs = network._attributes(body[12:])
        if network._link_index(network._link_attribute(attrs, 4, required=True)) != interface_index:
            raise JournalError('fixture route event has the wrong interface')
        if scenario in ('route', 'late') and (body[1] != 64 or
                network._link_attribute(attrs, 1, required=True) != bytes.fromhex('20010db8' + '00' * 12)):
            raise JournalError('fixture route event has the wrong destination')
    elif scenario not in ('route', 'late') and typ in (16, 17):
        if len(body) < 16 or body[0] not in (0, 7, 10):
            raise JournalError('fixture rejection is not the expected link event')
        if struct.unpack_from('=i', body, 4)[0] != interface_index:
            raise JournalError('fixture link event has the wrong interface')
    else:
        raise JournalError('fixture rejection has the wrong event type')
    return message


def _fixture_after_ipv6_read(read, action):
    fired = False

    def wrapped(path, *args, **kwargs):
        nonlocal fired
        result = read(path, *args, **kwargs)
        if path == '/proc/net/ipv6_route' and not fired:
            fired = True
            action()
        return result

    return wrapped


def network_parser():
    """Exercise route identity and malformed netlink frame rejection."""
    import struct
    import network_verify as network

    read_events = []
    original_value = 'original IPv6 routes\n'
    def original_read(path, *args, **kwargs):
        read_events.append(('read', path, args, kwargs))
        if path == 'failure':
            raise read_error
        return original_value
    def after_read():
        read_events.append(('action',))
        assert barrier('/proc/net/ipv6_route') is original_value
    read_error = JournalError('fixture original read failure')
    barrier = _fixture_after_ipv6_read(original_read, after_read)
    try:
        barrier('failure')
    except JournalError as exc:
        assert exc is read_error
    else:
        raise AssertionError('fixture read barrier swallowed an exception')
    assert barrier('/proc/net/route') is original_value
    assert barrier('/proc/net/ipv6_route', 123) is original_value
    assert barrier('/proc/net/ipv6_route') is original_value
    assert sum(event[0] == 'action' for event in read_events) == 1
    assert read_events[2] == ('read', '/proc/net/ipv6_route', (123,), {})
    assert read_events[3][0] == 'action' and read_events[4][0] == 'read'
    action_error = JournalError('fixture action failure')
    def fail_action():
        raise action_error
    failing_barrier = _fixture_after_ipv6_read(original_read, fail_action)
    try:
        failing_barrier('/proc/net/ipv6_route')
    except JournalError as exc:
        assert exc is action_error
    else:
        raise AssertionError('fixture read barrier swallowed action failure')
    assert failing_barrier('/proc/net/ipv6_route') is original_value

    bridges, veths = {'br-111111111111'}, {'vethaa', 'vethbb'}
    row = ('fe800000000000000000000000000000 40 '
           '00000000000000000000000000000000 00 '
           '00000000000000000000000000000000 00000100 00000000 00000000 '
           '00000001 vethaa').split()
    assert network._ipv6_route_allowed(row, bridges, veths)
    for index, value in ((0, 'fe810000000000000000000000000000'), (1, '41'),
                         (2, '00000000000000000000000000000001'), (3, '01'),
                         (4, '00000000000000000000000000000001'), (8, '00000003'),
                         (9, 'vethcc')):
        bad = row.copy()
        bad[index] = value
        assert not network._ipv6_route_allowed(bad, bridges, veths), (index, value)
    multicast = ('ff000000000000000000000000000000 08 '
                 '00000000000000000000000000000000 00 '
                 '00000000000000000000000000000000 00000100 00000000 00000000 '
                 '00000001 vethaa').split()
    for interface in ('br-111111111111', 'vethaa', 'vethbb'):
        candidate = multicast.copy()
        candidate[9] = interface
        assert network._ipv6_route_allowed(candidate, bridges, veths)
    for index, value in ((0, 'ff010000000000000000000000000000'),
                         (0, 'ff020000000000000000000000000000'),
                         (0, 'ff0e0000000000000000000000000000'),
                         (1, '07'), (1, '09'), (1, '80'),
                         (2, '00000000000000000000000000000001'), (3, '01'),
                         (4, '00000000000000000000000000000001'),
                         (8, '00000003'), (8, '00000005'),
                         (9, 'br-222222222222'), (9, 'vethcc'), (9, 'extra0')):
        bad = multicast.copy()
        bad[index] = value
        assert not network._ipv6_route_allowed(bad, bridges, veths), (index, value)
    for flags in ('00000000', '00000200'):
        candidate = multicast.copy()
        candidate[8] = flags
        assert not network._ipv6_route_allowed(candidate, bridges, veths)
        candidate[9] = 'br-111111111111'
        assert network._ipv6_route_allowed(candidate, bridges, veths)
    for index, value in ((5, '00000001'), (6, '00000002'), (7, '00000003')):
        candidate = multicast.copy()
        candidate[index] = value
        assert network._ipv6_route_allowed(candidate, bridges, veths)
    assert network._ipv6_route_allowed(multicast[:-1] + ['lo'], bridges, veths)
    expect_failure(lambda: network._ipv6_route_allowed(multicast[:-1], bridges, veths), 'short IPv6 route')
    for index in (1, 3, 8):
        bad = multicast.copy()
        bad[index] = 'not-hex'
        expect_failure(lambda bad=bad: network._ipv6_route_allowed(bad, bridges, veths), 'malformed IPv6 route')
    frame = struct.pack('=IHHII', 16, 16, 0, 7, 0)
    assert network._route_messages(frame) == [(16, 0, 7, 0, b'')]
    for bad in (b'\0', struct.pack('=IHHII', 15, 16, 0, 7, 0),
                struct.pack('=IHHII', 20, 16, 0, 7, 0), frame + b'\0'):
        expect_failure(lambda bad=bad: network._route_messages(bad), 'netlink framing')
    def attribute(kind, data):
        length = 4 + len(data)
        return struct.pack('=HH', length, kind) + data + bytes((-length) % 4)

    loopback = struct.pack('=BBHiII', 0, 0, 772, 1, 0, 0) + attribute(3, b'lo\0')
    assert network._link_message(loopback) == ('lo', (1, None, 0, 1))
    expect_failure(lambda: network._link_message(loopback + attribute(3, b'lo\0')), 'duplicate name')
    expect_failure(lambda: network._link_message(loopback[:16]), 'missing name')

    pins = {'lo': (1, None, 0, 1), 'br-111111111111': (2, 'bridge', 0, 2),
            'vethaa': (3, 'veth', 2, 4), 'vethbb': (4, 'veth', 2, 3)}

    def fixture_message(name, family=0, *, up=True, ready=None, carrier=None, oper=None,
                        master=None, peer=None, port=None, extra=b''):
        idx, kind, _, _ = pins[name]
        body = struct.pack('=BBHiII', family, 0, 772 if name == 'lo' else 1,
                           idx, 1 if up else 0, 0) + attribute(3, name.encode() + b'\0')
        if master is not None:
            body += attribute(10, struct.pack('=I', master))
        if peer is not None:
            body += attribute(5, struct.pack('=I', peer))
        if carrier is not None:
            body += attribute(33, bytes([carrier]))
        if oper is not None:
            body += attribute(16, bytes([oper]))
        if ready is not None:
            body += attribute(12, attribute(1, struct.pack('=I', 0x80000000 if ready else 0)))
        if port is not None:
            body += attribute(12, attribute(1, bytes([port])))
        return body + extra

    prep = _FixturePreparation(time.monotonic() + 5, pins)
    prep.message(fixture_message('vethaa', 7, up=False, master=2, peer=4, port=0))
    prep.message(fixture_message('br-111111111111', 10, ready=True, oper=0))
    prep.message(fixture_message('vethaa', 10, ready=True, oper=0))
    prep.message(fixture_message('vethaa', 10, ready=True, oper=0))
    assert not prep.all_ready()
    expect_failure(lambda: prep.final_link(b''), 'partial ready must not unlock GET')
    prep.message(fixture_message('vethbb', 10, ready=True))
    assert prep.all_ready()
    prep.message(fixture_message('vethaa', 7, master=2, port=3))
    expect_failure(lambda: prep.message(fixture_message('vethaa', 7, master=2, port=2)), 'bridge state regression')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 10, ready=False)), 'ready regression')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 7, master=3, port=3)), 'wrong bridge master')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 7, master=2, peer=99, port=3)), 'wrong bridge peer')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 7, master=2, ready=True)), 'bridge protinfo layout')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 9)), 'unknown link family')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 10, ready=True, extra=attribute(12, b''))), 'duplicate protinfo')
    expect_failure(lambda: prep.message(fixture_message('vethbb', 10, extra=attribute(12, attribute(1, b'\x01')))), 'short IPv6 flags')
    prep.message(fixture_message('vethbb', carrier=1, oper=6))
    expect_failure(lambda: prep.message(fixture_message('vethbb', carrier=0)), 'carrier regression')
    expect_failure(lambda: prep.message(fixture_message('vethbb', oper=0)), 'operstate regression')
    expect_failure(lambda: prep.message(fixture_message('vethbb', up=False)), 'link down regression')
    prep.message(fixture_message('vethbb', 7, master=2, port=3))

    def final_message(name, *, addrgen=b'\x01', carrier=b'\x01', oper=b'\x06', flags=0x80000000):
        idx, kind, master, peer = pins[name]
        result = struct.pack('=BBHiII', 0, 0, 1, idx, 1, 0) + attribute(3, name.encode() + b'\0')
        result += attribute(18, attribute(1, kind.encode() + b'\0'))
        if kind == 'veth':
            result += attribute(10, struct.pack('=I', master)) + attribute(5, struct.pack('=I', peer))
        ipv6 = attribute(1, struct.pack('=I', flags)) + attribute(8, addrgen)
        return result + attribute(33, carrier) + attribute(16, oper) + attribute(26, attribute(10, ipv6))

    def port_event(value, **kwargs):
        return fixture_message('vethaa', 7, master=2, port=value, **kwargs)

    def forwarded_trial():
        trial = _FixturePreparation(time.monotonic() + 5, pins)
        trial.message(port_event(3))
        trial.message(fixture_message('vethbb', 7, master=2, port=3))
        return trial

    def reject_without_mutation(trial, body, reason):
        previous = {name: dict(state) for name, state in trial.progress.items()}
        events = list(trial.events)
        expect_failure(lambda: trial.message(body), reason)
        assert trial.progress == previous and trial.events == events, reason

    trial = _FixturePreparation(time.monotonic() + 5, pins)
    for name in ('vethaa', 'vethbb'):
        trial.message(fixture_message(name, carrier=1, oper=3))
        for _ in range(2):
            trial.message(fixture_message(name, 7, master=2, port=0))
            state = trial.progress[name]
            assert state['startup_closed'] and not state['forwarding']
            assert state['bridge_state'] == 0 and state['startup_disable_pending']
        trial.message(fixture_message(name, 7, master=2, port=3))
        assert trial.progress[name]['forwarding'] and not trial.progress[name]['startup_disable_pending']
    for name in ('br-111111111111', 'vethaa', 'vethbb'):
        trial.message(fixture_message(name, 10, ready=True))
    trial.require_bridge_forwarding()
    for name in ('br-111111111111', 'vethaa', 'vethbb'):
        trial.final_link(final_message(name))
    assert len(trial.final_reads) == 3

    # A carrier-only closure permits the initial DISABLED state, but each port
    # must still report actual forwarding before any final GET or route cleanup.
    trial = _FixturePreparation(time.monotonic() + 5, pins)
    for name in ('vethaa', 'vethbb'):
        trial.message(fixture_message(name, carrier=1, oper=3))
        disabled = fixture_message(name, 7, master=2, port=0)
        trial.message(disabled)
        trial.message(disabled)
        state = trial.progress[name]
        assert state['startup_closed'] and not state['forwarding']
        assert state['bridge_state'] == 0 and state['startup_disable_pending']
    for name in ('br-111111111111', 'vethaa', 'vethbb'):
        trial.message(fixture_message(name, 10, ready=True))
    assert trial.all_ready()
    assert all(trial.progress[name]['startup_disable_pending'] for name in ('vethaa', 'vethbb'))
    requests = []
    def proceed_initial():
        trial.require_bridge_forwarding()
        requests.append(('get', 'br-111111111111'))
        requests.append(('delete', 'route'))
    expect_failure(proceed_initial, 'initial disabled must prevent requests')
    expect_failure(lambda: trial.final_link(final_message('br-111111111111')), 'initial disabled prevents final GET')
    assert not requests and not trial.final_reads
    trial.message(fixture_message('vethaa', 7, master=2, port=3))
    assert not trial.progress['vethaa']['startup_disable_pending']
    assert trial.progress['vethbb']['startup_disable_pending']
    expect_failure(proceed_initial, 'other port still disabled')
    assert not requests
    trial.message(fixture_message('vethbb', 7, master=2, port=3))
    trial.require_bridge_forwarding()
    for name in ('br-111111111111', 'vethaa', 'vethbb'):
        requests.append(('get', name))
        trial.final_link(final_message(name))
    assert len(requests) == 3 and len(trial.final_reads) == 3
    for name in ('vethaa', 'vethbb'):
        reject_without_mutation(trial, fixture_message(name, 7, master=2, port=0),
                                'disabled after actual forwarding')

    for name in ('vethaa', 'vethbb'):
        def initial_trial():
            current = _FixturePreparation(time.monotonic() + 5, pins)
            current.message(fixture_message(name, carrier=1, oper=3))
            return current

        for value in (0, 1, 2, 3, 4):
            current = _FixturePreparation(time.monotonic() + 5, pins)
            current.message(fixture_message(name, 7, master=2, port=value))
            assert current.progress[name]['bridge_state'] == value
        current = _FixturePreparation(time.monotonic() + 5, pins)
        current.message(fixture_message(name))
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0, carrier=1),
                                'first carrier in disabled frame')
        current = _FixturePreparation(time.monotonic() + 5, pins)
        current.message(fixture_message(name, up=False, carrier=1, oper=3))
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0),
                                'initial disabled without previous UP')
        current = initial_trial()
        current.message(fixture_message(name, 10, ready=True))
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0),
                                'initial disabled after ready')
        current = initial_trial()
        current.message(fixture_message(name, oper=6))
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0),
                                'initial disabled after oper UP')
        current = initial_trial()
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0, oper=6),
                                'initial disabled with oper UP in same frame')
        for value in (1, 2, 4):
            reject_without_mutation(current, fixture_message(name, 7, master=2, port=value),
                                    'closed window rejects non-disabled initial state')
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0, carrier=0),
                                'initial disabled cannot regress carrier')
        reject_without_mutation(current, fixture_message(name, 7, master=2, port=0, up=False),
                                'initial disabled cannot regress UP')

    # Original selection46 notification; pin the unmodified bytes and the
    # interface indices from its retained private diagnostic.
    selection46 = bytes.fromhex(
        '070001000300000003100100000000000b000300766574686262000008000a0002000000'
        '08000400dc05000005001000030000000a0001007614e6f8119500000800050004000000'
        '50010c800500010000000000060002002000000008000300020000000500040000000000'
        '05000500000000000500060000000000050007000000000005001c000000000005000800'
        '01000000050009000100000005001b000100000005001e000100000005000a0000000000'
        '05000c00000000000c000d0080007614e6f811950c000e0080007614e6f8119506000f00'
        '028000000600100000000000060011000280000006001200020000000500130000000000'
        '050014000000000005001d000000000006001f0000000000050020000000000005002300'
        '000000000500240000000000050021000000000005002700000000000500280000000000'
        '05002b00000000000c00150000000000000000000c00160000000000000000000c001700'
        '000000000000000005001900010000000800250000020000080026000000000008002900'
        '0000000008002a00000000000c001a000800020006000100')
    assert len(selection46) == 420
    assert hashlib.sha256(selection46).hexdigest() == 'afec83e90934ef83e9a5b9059ad8e438a5f451c1b1125ef6311bd247dee34a5e'
    vector_pins = dict(pins, vethaa=(4, 'veth', 2, 3), vethbb=(3, 'veth', 2, 4))
    def vector_message(family=0, **kwargs):
        body = bytearray(fixture_message('vethbb', family, **kwargs))
        struct.pack_into('=i', body, 4, 3)
        return bytes(body)
    def vector_trial():
        current = _FixturePreparation(time.monotonic() + 5, vector_pins)
        current.message(vector_message(carrier=1, oper=3))
        return current
    current = vector_trial()
    current.message(selection46)
    state = current.progress['vethbb']
    assert state['startup_closed'] and not state['forwarding']
    assert state['bridge_state'] == 0 and state['startup_disable_pending']
    for closing in (vector_message(7, master=2, port=3),
                    vector_message(10, ready=True), vector_message(oper=6)):
        current = vector_trial()
        current.message(closing)
        reject_without_mutation(current, selection46, 'selection46 closed-context rejection')

    trial = forwarded_trial()
    for value in (0, 0, 3, 0, 3):
        trial.message(port_event(value, carrier=0, oper=0))
    assert [event[3]['bridge_state'] for event in trial.events if event[1] == 'vethaa'] == [3, 0, 0, 3, 0, 3]
    assert all(event[3]['forwarding'] for event in trial.events if event[1] == 'vethaa')
    trial.message(fixture_message('br-111111111111', 10, ready=True))
    trial.message(fixture_message('vethaa', 10, ready=True))
    trial.message(fixture_message('vethbb', 10, ready=True))
    assert trial.all_ready()
    trial.require_bridge_forwarding()
    requests = []
    for interface in ('br-111111111111', 'vethaa', 'vethbb'):
        requests.append(('get', interface))
        trial.final_link(final_message(interface))
    assert len(requests) == 3 and len(trial.final_reads) == 3

    for missing in ('pending', 'never-observed'):
        trial = forwarded_trial() if missing == 'pending' else _FixturePreparation(time.monotonic() + 5, pins)
        if missing == 'pending':
            trial.message(port_event(0))
        for interface in ('br-111111111111', 'vethaa', 'vethbb'):
            trial.message(fixture_message(interface, 10, ready=True))
        assert trial.all_ready()
        requests = []
        def proceed():
            trial.require_bridge_forwarding()
            requests.append(('get', 'br-111111111111'))
            requests.append(('delete', 'route'))
        expect_failure(proceed, 'missing bridge forwarding must prevent requests')
        expect_failure(lambda: trial.final_link(final_message('br-111111111111')), 'final GET requires forwarding')
        assert not requests and not trial.final_reads
        if missing == 'pending':
            assert trial.progress['vethaa']['startup_disable_pending']
            trial.message(port_event(3))
            trial.require_bridge_forwarding()

    for close_event in (fixture_message('vethaa', 10, ready=True),
                        fixture_message('vethaa', carrier=1),
                        fixture_message('vethaa', oper=6),
                        port_event(3, oper=6)):
        trial = forwarded_trial()
        trial.message(close_event)
        assert trial.progress['vethaa']['startup_closed']
        reject_without_mutation(trial, port_event(0), 'closed startup window')
    trial = _FixturePreparation(time.monotonic() + 5, pins)
    trial.message(fixture_message('vethaa', 10, ready=True))
    reject_without_mutation(trial, port_event(0), 'closed window before first forwarding')
    trial = forwarded_trial()
    reject_without_mutation(trial, port_event(0, oper=6), 'same-frame operstate closes window')
    reject_without_mutation(trial, port_event(0, carrier=1), 'same-frame carrier closes window')
    for value in (1, 2, 4):
        reject_without_mutation(trial, port_event(value), 'invalid bridge state regression')
    trial.message(fixture_message('vethaa', carrier=1, oper=6))
    trial.message(fixture_message('vethaa'))  # Missing attributes cannot reopen startup.
    reject_without_mutation(trial, port_event(0), 'omitted fields cannot reopen startup')
    reject_without_mutation(trial, port_event(3, carrier=0), 'bridge carrier regression')
    reject_without_mutation(trial, port_event(3, oper=0), 'bridge operstate regression')
    reject_without_mutation(trial, port_event(3, up=False), 'bridge link down regression')
    reject_without_mutation(trial, fixture_message('vethaa', 7, master=3, port=3), 'bridge wrong master')
    reject_without_mutation(trial, fixture_message('vethaa', 7, master=2, peer=99, port=3), 'bridge wrong peer')
    reject_without_mutation(trial, port_event(3, extra=attribute(12, b'')), 'duplicate bridge attributes')
    wrong_index = bytearray(port_event(3))
    struct.pack_into('=i', wrong_index, 4, 99)
    reject_without_mutation(trial, bytes(wrong_index), 'bridge wrong index')

    # A failed message cannot publish valid fields parsed before its malformed tail.
    trial = forwarded_trial()
    reject_without_mutation(trial, fixture_message('vethaa', 10, carrier=1, ready=True,
                                                    extra=attribute(12, b'')), 'transactional message')

    expect_failure(lambda: prep.final_link(final_message('vethaa', addrgen=b'\x00')), 'final addrgen')
    expect_failure(lambda: prep.final_link(final_message('vethaa', carrier=b'\x00')), 'final carrier')
    expect_failure(lambda: prep.final_link(final_message('vethaa', oper=b'\x00')), 'final operstate')
    expect_failure(lambda: prep.final_link(final_message('vethaa', flags=0)), 'final readiness')
    for interface in ('br-111111111111', 'vethaa', 'vethbb'):
        prep.final_link(final_message(interface))
    expect_failure(lambda: prep.final_link(final_message('vethaa')), 'duplicate final GET')
    assert prep.final_reads == set(pins) - {'lo'}

    class PreparationSocket:
        def __init__(self, data, flags=0, sender=(0, 0)):
            self.data, self.flags, self.sender = data, flags, sender

        def recvmsg(self, _size):
            return self.data, [], self.flags, self.sender

    def packet(kind, sequence, body, flags=0):
        length = 16 + len(body)
        return struct.pack('=IHHII', length, kind, flags, sequence, 0) + body + bytes((-length) % 4)

    header = struct.pack('=IHHII', 32, 18, 5, 7, 0)
    ack = (2, 0x100, 7, 0, struct.pack('=i', 0) + header)
    reply = (16, 0, 7, 0, b'link')
    results = []
    done = _fixture_route_reply([ack], header, 7, 18, False, False, results)
    assert done and not results  # ACK alone cannot complete GETLINK.
    done = _fixture_route_reply([reply], header, 7, 18, False, done, results)
    assert done and results == [(16, b'link')]
    expect_failure(lambda: _fixture_route_reply([reply], header, 7, 18, False, done, results), 'duplicate GET reply')
    expect_failure(lambda: _fixture_route_reply([ack], header, 7, 18, True, False, []), 'ACK cannot complete dump')
    expect_failure(lambda: _fixture_route_reply([(2, 0, 7, 0, struct.pack('=i', -1) + header)], header, 7, 18, False, False, []), 'negative fixture ACK')
    expect_failure(lambda: _fixture_route_reply([(2, 0, 7, 0, struct.pack('=i', 0) + header[:12])], header, 7, 18, False, False, []), 'short fixture ACK')
    expect_failure(lambda: _fixture_route_reply([(16, 0, 8, 0, b'')], header, 7, 18, False, False, []), 'fixture wrong sequence')
    expect_failure(lambda: _fixture_route_reply([(3, 0x10, 7, 0, b'')], header, 7, 18, True, False, []), 'fixture interrupted dump')

    notification = packet(16, 0, fixture_message('vethaa', 10, ready=True))
    interleaved = b''.join((
        packet(16, 0, fixture_message('vethaa', 7, up=False, master=2, port=0)),
        packet(16, 0, fixture_message('vethbb', 10, ready=False)),
        packet(16, 0, fixture_message('br-111111111111', 10, ready=True, oper=0)),
        packet(16, 0, fixture_message('vethaa', 10, ready=True, oper=0)),
        packet(16, 0, fixture_message('vethaa', 7, master=2, port=3)),
        packet(16, 0, fixture_message('vethbb', 10, ready=True))))
    trial = _FixturePreparation(time.monotonic() + 5, pins)
    trial.sock = PreparationSocket(interleaved)
    assert trial.receive() and trial.all_ready() and trial.count == 6
    route_body = (struct.pack('=BBBBBBBBI', socket.AF_INET6, 64, 0, 0, 254, 3, 0, 1, 0) +
                  attribute(1, bytes.fromhex('20010db8' + '00' * 12)) + attribute(4, struct.pack('=I', 3)))
    import copy
    def exchange(kind, port):
        request = struct.pack('=IHHII', 16 + len(route_body), kind, 0x605 if kind == 24 else 5, 1, 0) + route_body
        ack_body = struct.pack('=i', 0) + request[:16]
        ack_data = struct.pack('=IHHII', 16 + len(ack_body), 2, 0x100, 1, port) + ack_body
        return {'port': port, 'request_hex': request.hex(),
                'replies': [{'data_hex': ack_data.hex(), 'sender': [0, 0], 'flags': 0, 'ancillary': False}]}
    evidence = {'version': 1, 'namespace': [123, 456], 'exchanges': [exchange(24, 2472), exchange(25, 2473)]}
    assert _fixture_route_evidence(evidence, 3, (123, 456)) == evidence['exchanges']
    raw = struct.pack('=IHHII', 16 + len(route_body), 24, 0x600, 1, 2472) + route_body
    messages = network._route_messages(raw)
    received = [(7, messages, (raw, [], 0, (0, 0x400)))]
    sequence_error = JournalError('route topology changed or unexpected sequence')
    encoded = json.dumps(evidence, sort_keys=True, separators=(',', ':')).encode('ascii')
    assert _fixture_evidence_packet(encoded + b' ' * (49152 - len(encoded)), [], 0, 3, (123, 456)) == evidence
    for payload, ancillary, flags in ((b'', [], 0), (encoded + b' ' * 49153, [], 0),
                                      (encoded, [], socket.MSG_TRUNC), (encoded, [], socket.MSG_CTRUNC),
                                      (encoded, [(1, 2, b'x')], 0),
                                      (b'{"version":1,"version":1}', [], 0)):
        expect_failure(lambda: _fixture_evidence_packet(payload, ancillary, flags, 3, (123, 456)), 'bad evidence packet')
    assert _fixture_rejected_notification(sequence_error, received, 'route', 3, writer_evidence=evidence, namespace=(123, 456)) == messages[0]
    assert _fixture_rejected_notification(JournalError('route topology changed after final observation'), received,
                                          'late', 3, writer_evidence=evidence, namespace=(123, 456)) == messages[0]
    delete_raw = struct.pack('=IHHII', 16 + len(route_body), 25, 0, 1, 2473) + route_body
    delete_messages = network._route_messages(delete_raw)
    delete_received = [(7, delete_messages, (delete_raw, [], 0, (0, 0x400)))]
    assert _fixture_rejected_notification(sequence_error, delete_received, 'route', 3,
                                          writer_evidence=evidence, namespace=(123, 456)) == delete_messages[0]
    link_event = (16, 0, 0, 0, fixture_message('vethaa'))
    assert _fixture_rejected_notification(sequence_error, [(7, [link_event])], 'master', 3) == link_event
    expect_failure(lambda: _fixture_rejected_notification(sequence_error, received, 'route', 3), 'missing writer evidence')
    expect_failure(lambda: _fixture_rejected_notification(sequence_error, received, 'route', 4, writer_evidence=evidence, namespace=(123, 456)), 'wrong target')
    expect_failure(lambda: _fixture_rejected_notification(sequence_error, received, 'route', 3, writer_evidence=evidence, namespace=(123, 457)), 'wrong namespace')
    expect_failure(lambda: _fixture_rejected_notification(JournalError('route netlink acknowledgement failed'), received,
                                                          'route', 3, writer_evidence=evidence, namespace=(123, 456)), 'unrelated error')
    for changed in ((0, 0), (1, 0x400), (0, 0x100)):
        bad = [(7, messages, (raw, [], 0, changed))]
        expect_failure(lambda: _fixture_rejected_notification(sequence_error, bad, 'route', 3, writer_evidence=evidence, namespace=(123, 456)), 'wrong multicast sender')
    for change in ('port', 'sequence', 'type', 'flags', 'prefix', 'destination', 'oif', 'gateway', 'table', 'duplicate'):
        body = route_body
        kind, flags, seq, port = 24, 0x600, 1, 2472
        if change == 'port': port += 1
        if change == 'sequence': seq = 7
        if change == 'type': kind = 16
        if change == 'flags': flags |= 0x10
        if change == 'prefix': body = body[:1] + b'\x3f' + body[2:]
        if change == 'destination': body = body[:16] + bytes(16) + body[32:]
        if change == 'oif': body = body[:-4] + struct.pack('=I', 4)
        if change == 'gateway': body += attribute(5, bytes(16))
        if change == 'table': body += attribute(15, struct.pack('=I', 253))
        if change == 'duplicate': body += attribute(4, struct.pack('=I', 3))
        frame = struct.pack('=IHHII', 16 + len(body), kind, flags, seq, port) + body
        bad = [(7, network._route_messages(frame), (frame, [], 0, (0, 0x400)))]
        expect_failure(lambda: _fixture_rejected_notification(sequence_error, bad, 'route', 3, writer_evidence=evidence, namespace=(123, 456)), change)
    extra = struct.pack('=IHHII', 16, 3, 0, 7, 0) + raw
    expect_failure(lambda: _fixture_rejected_notification(sequence_error, [(7, network._route_messages(extra), (extra, [], 0, (0, 0x400)))],
                                                          'route', 3, writer_evidence=evidence, namespace=(123, 456)), 'first message mismatch')
    for change in ('bool', 'unknown', 'port', 'namespace-bool', 'sender', 'ancillary', 'reply-flags',
                   'request-type', 'request-sequence', 'missing-ack', 'duplicate-ack', 'negative-ack', 'header', 'overflow'):
        bad = copy.deepcopy(evidence)
        item = bad['exchanges'][0]
        if change == 'bool': bad['version'] = True
        if change == 'unknown': bad['extra'] = 1
        if change == 'port': item['port'] = True
        if change == 'namespace-bool': bad['namespace'][0] = True
        if change == 'sender': item['replies'][0]['sender'] = [1, 0]
        if change == 'ancillary': item['replies'][0]['ancillary'] = True
        if change == 'reply-flags': item['replies'][0]['flags'] = socket.MSG_TRUNC
        if change in ('request-type', 'request-sequence'):
            data = bytearray.fromhex(item['request_hex'])
            struct.pack_into('=H' if change == 'request-type' else '=I', data,
                             4 if change == 'request-type' else 8, 25 if change == 'request-type' else 2)
            item['request_hex'] = data.hex()
        if change == 'missing-ack': item['replies'] = []
        if change == 'duplicate-ack': item['replies'].append(copy.deepcopy(item['replies'][0]))
        if change == 'negative-ack':
            data = bytes.fromhex(item['replies'][0]['data_hex'])
            item['replies'][0]['data_hex'] = (data[:16] + struct.pack('=i', -1) + data[20:]).hex()
        if change == 'header':
            data = bytes.fromhex(item['replies'][0]['data_hex'])
            item['replies'][0]['data_hex'] = (data[:20] + bytes(16)).hex()
        if change == 'overflow': item['request_hex'] = '00' * 4097
        expect_failure(lambda: _fixture_route_evidence(bad, 3, (123, 456)), change)
    class TapSocket:
        def recvmsg(self, *args, **kwargs):
            return raw, [(1, 2, b'abc')], 4, (0, 0x400)
        def fileno(self):
            return 123
    tap = _FixtureReceiveTap(TapSocket())
    assert tap.fileno() == 123 and tap.recvmsg(65536) == (raw, [(1, 2, b'abc')], 4, (0, 0x400))
    assert tap.result == (raw, [(1, 2, b'abc')], 4, (0, 0x400))
    expect_failure(lambda: tap.recvmsg(1), 'duplicate tap read')
    class BrokenTap:
        def recvmsg(self, *args, **kwargs):
            raise BlockingIOError()
    tap = _FixtureReceiveTap(BrokenTap())
    try:
        tap.recvmsg(1)
    except BlockingIOError:
        assert tap.result is None
    else:
        raise AssertionError('tap swallowed receive exception')
    for data, flags, sender in ((notification, socket.MSG_TRUNC, (0, 0)),
                                (notification, socket.MSG_CTRUNC, (0, 0)),
                                (notification, 0, (1, 0)),
                                (packet(4, 0, b''), 0, (0, 0)),
                                (packet(17, 0, b''), 0, (0, 0)),
                                (packet(16, 9, b''), 0, (0, 0)),
                                (packet(16, 0, b'', 0x10), 0, (0, 0)),
                                (notification[:-1], 0, (0, 0))):
        trial = _FixturePreparation(time.monotonic() + 5, pins)
        trial.sock = PreparationSocket(data, flags, sender)
        expect_failure(trial.receive, 'invalid preparation datagram')
    trial = _FixturePreparation(time.monotonic() + 5, pins)
    trial.sock = PreparationSocket(notification)
    trial.count = 4096
    expect_failure(trial.receive, 'preparation message bound')
    trial = _FixturePreparation(time.monotonic() - 1, pins)
    expect_failure(trial.wait_ready, 'missing readiness deadline')
    trial = _FixturePreparation(time.monotonic() + 5, pins)
    trial.nsfd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
    try:
        trial.namespace = (0, 0)
        expect_failure(trial.check_namespace, 'preparation namespace changed')
    finally:
        trial.close()
    before_fds = set(os.listdir('/proc/self/fd'))
    original_socket = socket.socket
    try:
        def fail_preparation_socket(*_args, **_kwargs):
            raise OSError('preparation socket initialization failure')
        socket.socket = fail_preparation_socket
        expect_failure(lambda: _FixturePreparation(time.monotonic() + 5, pins).__enter__(), 'preparation initialization')
    finally:
        socket.socket = original_socket
    if set(os.listdir('/proc/self/fd')) != before_fds:
        raise AssertionError('preparation initialization leaked a descriptor')

    class SubscriptionSocket:
        def setblocking(self, value):
            assert value is False

        def setsockopt(self, level, option, value):
            assert (level, option, value) == (socket.SOL_SOCKET, socket.SO_RCVBUF, 1048576)

        def getsockopt(self, level, option):
            assert (level, option) == (socket.SOL_SOCKET, socket.SO_RCVBUF)
            return 1048576

        def bind(self, address):
            assert address == (0, 0x801)

        def close(self):
            self.closed = True

    fake_subscription = SubscriptionSocket()
    try:
        socket.socket = lambda *_args, **_kwargs: fake_subscription
        with _FixturePreparation(time.monotonic() + 5, pins):
            pass
    finally:
        socket.socket = original_socket
    assert fake_subscription.closed
    if set(os.listdir('/proc/self/fd')) != before_fds:
        raise AssertionError('preparation subscription leaked a descriptor')

    class LostPreparationSocket:
        def recvmsg(self, _size):
            import errno
            raise OSError(errno.ENOBUFS, 'lost netlink notifications')

    trial = _FixturePreparation(time.monotonic() + 5, pins)
    trial.sock = LostPreparationSocket()
    expect_failure(trial.receive, 'preparation notification loss')

    class RequestSocket:
        def sendto(self, data, address):
            self.header = data[:16]
            return len(data)

    def request(messages, dump=False):
        window = network._TopologyWindow(time.monotonic() + 1)
        window.sock = RequestSocket()
        window.port = 123
        queue = iter(messages)

        def receive(_wait):
            try:
                return next(queue)(window.sock.header)
            except StopIteration:
                raise JournalError('simulated missing acknowledgement')
        window._receive = receive
        return window._request(1, dump=dump)

    def link_message(header):
        return [(16, 0, 1, 0, loopback)]

    def good_ack(header):
        return [(2, 0x100, 1, 0, struct.pack('=i', 0) + header)]

    assert request([link_message, good_ack]) == [('lo', (1, None, 0, 1))]
    for messages, dump in (([lambda h: [(16, 0, 2, 0, loopback)]], False),
                           ([lambda h: [(4, 0, 1, 0, b'')]], False),
                           ([lambda h: [(3, 0x10, 1, 0, b'')]], True),
                           ([link_message], False),
                           ([link_message, lambda h: [(2, 0, 1, 0, struct.pack('=i', -1) + h)]], False),
                           ([link_message, lambda h: [(2, 0, 1, 0, struct.pack('=i', 0) + h[:12])]], False)):
        expect_failure(lambda messages=messages, dump=dump: request(messages, dump), 'netlink request')

    class ReceiveSocket:
        def __init__(self, flags, address):
            self.flags, self.address = flags, address

        def recvmsg(self, _size):
            return frame, [], self.flags, self.address

    for flags, address in ((socket.MSG_TRUNC, (0, 0)), (socket.MSG_CTRUNC, (0, 0)), (0, (1, 0))):
        window = network._TopologyWindow(time.monotonic() + 1)
        window.sock = ReceiveSocket(flags, address)
        expect_failure(lambda: window._receive(False), 'invalid sender or truncated datagram')
    class OverflowSocket:
        def recvmsg(self, _size):
            import errno
            raise OSError(errno.ENOBUFS, 'netlink receive overflow')

    window = network._TopologyWindow(time.monotonic() + 1)
    window.sock = OverflowSocket()
    expect_failure(lambda: window._receive(False), 'netlink receive overflow')
    window = network._TopologyWindow(time.monotonic() + 1)
    window.sock = ReceiveSocket(0, (0, 0))
    window.count = 4096
    expect_failure(lambda: window._receive(False), 'netlink message bound')
    before_fds = set(os.listdir('/proc/self/fd'))
    original_socket = network.socket.socket
    try:
        def fail_socket(*args, **kwargs):
            raise OSError('fixture socket initialization failure')
        network.socket.socket = fail_socket
        expect_failure(lambda: network._TopologyWindow(time.monotonic() + 1).__enter__(), 'partial window initialization')
    finally:
        network.socket.socket = original_socket
    if set(os.listdir('/proc/self/fd')) != before_fds:
        raise AssertionError('partial topology window initialization leaked a descriptor')
    window = network._TopologyWindow(time.monotonic() - 1)
    expect_failure(lambda: window._receive(False), 'expired topology deadline')
    window = network._TopologyWindow(time.monotonic() + 1)
    window.nsfd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
    try:
        window.namespace = (0, 0)
        expect_failure(window._check_namespace, 'changed namespace identity')
    finally:
        os.close(window.nsfd)


def network_window():
    """Run all topology mutations in disposable network and mount namespaces."""
    import select
    import signal
    import struct
    import traceback
    import network_verify as network

    if os.geteuid() != 0:
        raise JournalError('network-window requires root')
    deadline = time.monotonic() + 30
    libc = ctypes.CDLL(None, use_errno=True)
    libc.unshare.argtypes = [ctypes.c_int]
    libc.mount.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_char_p,
                           ctypes.c_ulong, ctypes.c_void_p]

    def checked(result, operation):
        if result != 0:
            number = ctypes.get_errno()
            raise OSError(number, operation + ': ' + os.strerror(number))

    def fork_call(call, *, route_evidence=False):
        ready, release = os.pipe2(os.O_CLOEXEC)
        parent_channel = child_channel = None
        try:
            if route_evidence:
                parent_channel, child_channel = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
                parent_channel.setblocking(False)
                child_channel.setblocking(False)
            parent_pid = os.getpid()
            pid = os.fork()
        except BaseException:
            os.close(ready)
            os.close(release)
            if parent_channel is not None:
                parent_channel.close()
                child_channel.close()
            raise
        if pid == 0:
            os.close(release)
            if parent_channel is not None:
                parent_channel.close()
            if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent_pid:
                os._exit(1)
            try:
                if os.read(ready, 1) != b'G':
                    raise JournalError('fixture barrier closed')
                os.close(ready)
                value = call()
                if child_channel is not None:
                    payload = json.dumps(value, sort_keys=True, separators=(',', ':')).encode('ascii')
                    if not payload or len(payload) > 49152:
                        raise JournalError('fixture route evidence exceeds bound')
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not select.select([], [child_channel], [], remaining)[1]:
                        raise JournalError('fixture evidence send timed out')
                    if child_channel.send(payload) != len(payload):
                        raise JournalError('fixture evidence send short')
                    child_channel.close()
            except BaseException:
                traceback.print_exc(limit=16)
                os._exit(1)
            os._exit(0)
        os.close(ready)
        if child_channel is not None:
            child_channel.close()
        fd = None
        reaped = False
        try:
            fd = os.pidfd_open(pid, 0)
            if os.write(release, b'G') != 1:
                raise JournalError('fixture barrier failed')
            os.close(release)
            release = None
            value = None
            if parent_channel is not None:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([parent_channel], [], [], remaining)[0]:
                    raise JournalError('fixture evidence receive timed out')
                data, ancillary, flags, _ = parent_channel.recvmsg(49152)
                info = os.stat('/proc/self/ns/net')
                value = _fixture_evidence_packet(data, ancillary, flags, socket.if_nametoindex('vethaa'),
                                                 (info.st_dev, info.st_ino))
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([fd], [], [], remaining)[0]:
                raise JournalError('network fixture child timed out')
            waited, status = os.waitpid(pid, 0)
            reaped = True
            if waited != pid or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
                raise JournalError('network fixture child failed: ' + str(status))
            if parent_channel is not None:
                _fixture_evidence_eof(parent_channel)
            return value
        finally:
            if release is not None:
                os.close(release)
            if parent_channel is not None:
                parent_channel.close()
            try:
                if not reaped:
                    try:
                        if fd is None:
                            os.kill(pid, signal.SIGKILL)
                        else:
                            signal.pidfd_send_signal(fd, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    os.waitpid(pid, 0)
                    reaped = True
            finally:
                if fd is not None:
                    os.close(fd)

    def scenario(name):
        check_parent_namespaces()
        checked(libc.unshare(0x40000000 | 0x00020000), 'unshare network/mount')
        child_stats = [os.stat('/proc/self/ns/' + item) for item in ('net', 'mnt')]
        child_ns = tuple((info.st_dev, info.st_ino) for info in child_stats)
        if any(a == b for a, b in zip(parent_ns, child_ns)):
            raise JournalError('fixture namespace did not change')
        for fd, identity in zip(parent_fds, parent_ns):
            held = os.fstat(fd)
            if (held.st_dev, held.st_ino) != identity:
                raise JournalError('fixture parent namespace pin changed')
        checked(libc.mount(None, b'/', None, 0x4000 | 0x40000, None), 'make mounts private')

        def mount_inventory():
            with open('/proc/self/mountinfo', encoding='ascii') as mountinfo:
                return [line.rstrip('\n') for line in mountinfo]

        def sys_mount_id():
            fd = os.open('/sys', os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
            try:
                with open('/proc/self/fdinfo/' + str(fd), encoding='ascii') as info:
                    values = [line.split(':', 1)[1].strip() for line in info
                              if line.startswith('mnt_id:')]
                if len(values) != 1 or not values[0].isdecimal():
                    raise JournalError('fixture sysfs mount identity unavailable')
                return int(values[0])
            finally:
                os.close(fd)

        previous_mounts = mount_inventory()
        previous_ids = {int(line.split()[0]) for line in previous_mounts}
        previous_sys_id = sys_mount_id()
        if previous_sys_id not in previous_ids:
            raise JournalError('original sysfs mount identity unavailable')
        checked(libc.mount(b'sysfs', b'/sys', b'sysfs', 2 | 4 | 8, None), 'mount private sysfs')
        current_sys_id = sys_mount_id()
        mounts = [line for line in mount_inventory() if int(line.split()[0]) == current_sys_id]
        if len(mounts) != 1 or current_sys_id in previous_ids or current_sys_id == previous_sys_id:
            raise JournalError('private sysfs mount is not new')
        left, right = mounts[0].split(' - ', 1)
        fields = left.split()
        if (len(fields) < 6 or fields[4] != '/sys' or right.split()[0] != 'sysfs' or
                not {'nosuid', 'nodev', 'noexec'} <= set(fields[5].split(','))):
            raise JournalError('private sysfs mount flags or type differ')
        if os.listdir('/sys/class/net') != ['lo']:
            raise JournalError('fixture namespace is not initially empty')

        def attr(kind, value):
            length = 4 + len(value)
            return struct.pack('=HH', length, kind) + value + bytes((-length) % 4)

        def nested(kind, value):
            return attr(kind | 0x8000, value)

        def link(index=0, flags=0, change=0):
            return struct.pack('=BBHiII', 0, 0, 0, index, flags, change)

        route_exchanges = []
        preparation = [None]

        class RouteSocket:
            def __init__(self):
                self.sock = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW | socket.SOCK_CLOEXEC, 0)
                try:
                    self.sock.bind((0, 0))
                    self.port, groups = self.sock.getsockname()
                    if type(self.port) is not int or not 0 < self.port <= 0xffffffff or groups != 0:
                        raise JournalError('fixture writer socket address invalid')
                    self.sock.setblocking(False)
                    self.sequence = 0
                except BaseException:
                    self.sock.close()
                    raise

            def close(self):
                self.sock.close()

            def request(self, kind, flags, payload, dump=False, *, evidence=None):
                self.sequence += 1
                seq = self.sequence
                header = struct.pack('=IHHII', 16 + len(payload), kind, flags | 1, seq, 0)
                observed = None
                if evidence is not None:
                    if dump or kind not in (24, 25) or len(header) + len(payload) > 4096:
                        raise JournalError('invalid fixture evidence request')
                    observed = {'port': self.port, 'request_hex': (header + payload).hex(), 'replies': []}
                if preparation[0] is not None:
                    observer = preparation[0]
                    observer.events.append(('request', kind, flags, seq, payload[:32].hex()))
                    if len(observer.events) > 4096:
                        raise JournalError('fixture preparation diagnostic bound exceeded')
                exchange = None
                if kind in (25, 26):
                    exchange = {'request': (header + payload).hex(), 'dump': dump, 'responses': []}
                    route_exchanges.append(exchange)
                if self.sock.sendto(header + payload, (0, 0)) != len(header) + len(payload):
                    raise JournalError('short fixture netlink request')
                result = []
                done = False
                while True:
                    observer = preparation[0]
                    remaining = observer.remaining() if observer else deadline - time.monotonic()
                    if remaining <= 0:
                        raise JournalError('fixture netlink deadline expired')
                    readers = [self.sock] + ([observer.sock] if observer else [])
                    ready, _, _ = select.select(readers, [], [], remaining)
                    if not ready:
                        raise JournalError('fixture netlink deadline expired')
                    if observer and observer.sock in ready:
                        observer.drain()
                    if self.sock not in ready:
                        continue
                    try:
                        data, ancillary, msgflags, address = self.sock.recvmsg(65536)
                    except OSError as exc:
                        raise JournalError('fixture netlink receive failed') from exc
                    if observed is not None:
                        if len(data) > 4096 or len(observed['replies']) >= 4:
                            raise JournalError('fixture writer evidence exceeds bound')
                        observed['replies'].append({'data_hex': data.hex(), 'sender': list(address),
                                                    'flags': msgflags, 'ancillary': bool(ancillary)})
                    if exchange is not None:
                        exchange['responses'].append({'data': data.hex(), 'sender': address,
                                                      'flags': msgflags, 'ancillary': bool(ancillary)})
                        if len(repr(route_exchanges)) > 24000:
                            raise JournalError('fixture route diagnostic capture incomplete: bound exceeded')
                    if ancillary or msgflags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or not address or address[0] != 0:
                        raise JournalError('invalid fixture netlink response')
                    # A GETLINK needs both its one reply and its ACK, in either order.
                    messages = network._route_messages(data)
                    if observer is not None:
                        observer.count += len(messages)
                        observer.size += len(data)
                        if observer.count > 4096 or observer.size > 1048576:
                            raise JournalError('fixture preparation message bound exceeded')
                    done = _fixture_route_reply(messages, header, seq, kind, dump, done, result)
                    if done and (kind != 18 or dump or len(result) == 1):
                        if self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR):
                            raise JournalError('fixture request socket error')
                        if observed is not None:
                            evidence.append(observed)
                        return result

        def write(kind, payload, flags=0, *, evidence=None):
            route = RouteSocket()
            try:
                route.request(kind, flags | 4, payload, evidence=evidence)
            finally:
                route.close()

        def index(interface):
            return socket.if_nametoindex(interface)

        def set_link(interface, *, master=None, up=None):
            attrs = b'' if master is None else attr(10, struct.pack('=I', master))
            write(16, link(index(interface), 1 if up else 0, 1 if up is not None else 0) + attrs)

        def kind_info(kind, extra=b''):
            return nested(18, attr(1, kind.encode() + b'\0') + extra)

        def addrgen_none():
            return nested(26, nested(10, attr(8, b'\x01')))

        def create_bridge():
            # IPv6's per-device state exists only after registration; configure
            # the newly registered, still-down link before any IFF_UP request.
            write(16, link() + attr(3, b'br-111111111111\0') + kind_info('bridge'), 0x600)
            write(16, link(index('br-111111111111')) + addrgen_none())

        def create_veth():
            peer = link() + attr(3, b'vethbb\0')
            info = kind_info('veth', nested(2, nested(1, peer)))
            write(16, link() + attr(3, b'vethaa\0') + info, 0x600)
            for interface in ('vethaa', 'vethbb'):
                write(16, link(index(interface)) + addrgen_none())

        def route_dump():
            route = RouteSocket()
            try:
                messages = route.request(26, 0x300, struct.pack('=BBBBBBBBI', 10, 0, 0, 0, 0, 0, 0, 0, 0), True)
            finally:
                route.close()
            return [body for typ, body in messages if typ == 24]

        def attributes(data):
            parsed = network._attributes(data)
            if any(len(values) != 1 for values in parsed.values()):
                raise JournalError('duplicate fixture route attribute')
            return {kind: values[0] for kind, values in parsed.items()}

        def change_route(kind, destination, prefix, interface, *, table=254, protocol=3, route_type=1, gateway=None, evidence=None):
            header = struct.pack('=BBBBBBBBI', 10, prefix, 0, 0, table, protocol, 0, route_type,
                                 4 if gateway is not None else 0)
            payload = header + attr(1, destination) + attr(4, struct.pack('=I', index(interface)))
            if gateway is not None:
                payload += attr(5, gateway)
            write(kind, payload, 0x600 if kind == 24 else 0, evidence=evidence)

        initial = RouteSocket()
        try:
            messages = initial.request(18, 0x300, link(), True)
            identities = [network._link_message(body)[0] for typ, body in messages if typ == 16]
            if identities != ['lo']:
                raise JournalError('initial netlink interface inventory is not loopback only')
        finally:
            initial.close()

        create_bridge()
        create_veth()
        for interface in ('vethaa', 'vethbb'):
            set_link(interface, master=index('br-111111111111'))
        pin = RouteSocket()
        try:
            pin_messages = pin.request(18, 0x300, link(), True)
        finally:
            pin.close()
        identities = dict(network._link_message(body) for typ, body in pin_messages if typ == 16)
        bridge = index('br-111111111111')
        expected = {'lo': (index('lo'), None, 0, index('lo')),
                    'br-111111111111': (bridge, 'bridge', 0, bridge),
                    'vethaa': (index('vethaa'), 'veth', bridge, index('vethbb')),
                    'vethbb': (index('vethbb'), 'veth', bridge, index('vethaa'))}
        if identities != expected or len(pin_messages) != len(expected):
            raise JournalError('fixture initial link identities differ')
        fixture_indices = {expected[item][0] for item in ('br-111111111111', 'vethaa', 'vethbb')}
        before, after, removed, route_rows = [], [], [], []
        observer = _FixturePreparation(deadline, expected)
        try:
            with observer:
                preparation[0] = observer
                for interface in ('lo', 'br-111111111111', 'vethaa', 'vethbb'):
                    set_link(interface, up=True)
                observer.wait_ready()
                observer.drain()
                observer.require_bridge_forwarding()
                for interface in ('br-111111111111', 'vethaa', 'vethbb'):
                    route = RouteSocket()
                    try:
                        replies = route.request(18, 4, link(expected[interface][0]))
                    finally:
                        route.close()
                    if len(replies) != 1 or replies[0][0] != 16:
                        raise JournalError('fixture final GETLINK reply missing')
                    observer.final_link(replies[0][1])
                if observer.final_reads != set(expected) - {'lo'}:
                    raise JournalError('fixture final GETLINK set incomplete')
                observer.drain()
                observer.require_bridge_forwarding()
                before = route_dump()
                removed_outputs = set()
                for body in before:
                    if len(body) < 12:
                        raise JournalError('short fixture route')
                    family, prefix, source_len, tos, table, protocol, scope, route_type, route_flags = struct.unpack_from('=BBBBBBBBI', body)
                    attrs = attributes(body[12:])
                    destination = attrs.get(1, b'')
                    output = struct.unpack('=I', attrs[4])[0] if len(attrs.get(4, b'')) == 4 else 0
                    if family == 10 and prefix == 8 and destination == bytes.fromhex('ff' + '00' * 15) and output in fixture_indices:
                        if (protocol != 2 or source_len or tos or
                                attrs.get(5, bytes(16)) != bytes(16) or output in removed_outputs):
                            raise JournalError('fixture multicast route has unexpected origin or duplicate target')
                        removed_outputs.add(output)
                        if name != 'multicast':
                            interface = socket.if_indextoname(output)
                            change_route(25, destination, prefix, interface, table=table, protocol=protocol, route_type=route_type)
                            removed.append(body)
                after = route_dump()
                if sorted(before) != sorted(after + removed):
                    raise JournalError('unexpected fixture route deletion')
                if name == 'multicast' and (removed or expected['br-111111111111'][0] not in removed_outputs or
                                            not removed_outputs.intersection({expected['vethaa'][0], expected['vethbb'][0]})):
                    raise JournalError('fixture retained multicast routes are incomplete')
                change_route(24, bytes.fromhex('fe80' + '00' * 14), 64, 'vethaa')
                installed = route_dump()
                new = list(installed)
                for body in after:
                    if body not in new:
                        raise JournalError('fixture route inventory changed during installation')
                    new.remove(body)
                if len(new) != 1:
                    raise JournalError('fixture route installation inventory differs')
                row = new[0]
                if len(row) < 12:
                    raise JournalError('short installed fixture route')
                family, prefix, source_len, tos, table, protocol, scope, route_type, route_flags = struct.unpack_from('=BBBBBBBBI', row)
                attrs = attributes(row[12:])
                if (family, prefix, source_len, table, protocol, route_type) != (10, 64, 0, 254, 3, 1) or attrs.get(1) != bytes.fromhex('fe80' + '00' * 14) or attrs.get(4) != struct.pack('=I', expected['vethaa'][0]) or attrs.get(5, bytes(16)) != bytes(16):
                    raise JournalError('installed fixture route differs')
                route_rows = network._read('/proc/net/ipv6_route').splitlines()
                parsed_rows = [row.split() for row in route_rows]
                if not any(len(row) == 10 and
                           (row[0].lower(), row[1], row[2].lower(), row[3], row[4].lower(), row[8], row[9]) ==
                           ('fe800000000000000000000000000000', '40', '0' * 32, '00', '0' * 32,
                            '00000001', 'vethaa') for row in parsed_rows):
                    raise JournalError('fixture route was not installed')
                if name == 'multicast':
                    multicast_interfaces = {row[9] for row in parsed_rows if len(row) == 10 and
                                            (row[0].lower(), row[1], row[2].lower(), row[3], row[4].lower(), row[8]) ==
                                            ('ff000000000000000000000000000000', '08', '0' * 32, '00',
                                             '0' * 32, '00000001')}
                    if ('br-111111111111' not in multicast_interfaces or
                            not multicast_interfaces.intersection({'vethaa', 'vethbb'})):
                        raise JournalError('fixture multicast routes missing from IPv6 inventory')
                observer.drain()
                observer.require_bridge_forwarding()
                requests = [event for event in observer.events if event[0] == 'request']
                up_requests = [struct.unpack_from('=BBHiII', bytes.fromhex(event[4]))
                               for event in requests if event[1] == 16]
                if [(item[3], item[4], item[5]) for item in up_requests] != [
                        (expected[interface][0], 1, 1)
                        for interface in ('lo', 'br-111111111111', 'vethaa', 'vethbb')]:
                    raise JournalError('fixture UP request sequence differs')
                get_requests = [struct.unpack_from('=BBHiII', bytes.fromhex(event[4]))[3]
                                for event in requests if event[1] == 18]
                if get_requests != [expected[interface][0] for interface in
                                    ('br-111111111111', 'vethaa', 'vethbb')]:
                    raise JournalError('fixture final GET request sequence differs')
                if sum(event[1] == 25 for event in requests) != len(removed):
                    raise JournalError('fixture route deletion request count differs')
        except BaseException:
            import sys
            try:
                if observer.rejected_notification is not None:
                    print('network rejected notification: ' + json.dumps(observer.rejected_notification,
                                                                          sort_keys=True), file=sys.stderr)
                detail = repr({'scenario': name, 'events': observer.events, 'final_reads': sorted(observer.final_reads),
                               'before': [body.hex() for body in before], 'after': [body.hex() for body in after],
                               'removed': [body.hex() for body in removed], 'exchanges': route_exchanges})
                print('network preparation evidence: ' + (detail if len(detail) <= 32000 else detail[:31960] + ' [diagnostic truncated]'), file=sys.stderr)
            except BaseException:
                pass
            raise
        finally:
            preparation[0] = None
        networks = {'fixture': '1' * 64}

        def verify():
            before_fds = set(os.listdir('/proc/self/fd'))
            try:
                network.networks(networks, min(deadline, time.monotonic() + 5))
            finally:
                if set(os.listdir('/proc/self/fd')) != before_fds:
                    raise AssertionError('topology window leaked a file descriptor')
        try:
            verify()
        except BaseException:
            detail = repr({'scenario': name, 'indices': sorted(fixture_indices),
                           'before': [body.hex() for body in before],
                           'after': [body.hex() for body in after],
                           'removed': [body.hex() for body in removed], 'exchanges': route_exchanges,
                           'installed_routes': route_rows})
            import sys
            print('network preparation evidence: ' +
                  (detail if len(detail) <= 32000 else 'diagnostic capture incomplete: bound exceeded'),
                  file=sys.stderr)
            raise


        if name == 'positive':
            def quiet_child():
                null = os.open('/dev/null', os.O_WRONLY | os.O_CLOEXEC)
                os.dup2(null, 2)
                os.close(null)

            def failed_child():
                quiet_child()
                raise JournalError('intentional fixture child failure')

            def oversized_child():
                quiet_child()
                return 'x' * 49153

            descriptors = set(os.listdir('/proc/self/fd'))
            for task in (lambda: os._exit(0), failed_child, oversized_child):
                expect_failure(lambda task=task: fork_call(task, route_evidence=True), 'fixture fork failure')
                if set(os.listdir('/proc/self/fd')) != descriptors:
                    raise JournalError('fixture fork leaked descriptors')
            receiver, sender = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            try:
                def extra_packet_child():
                    receiver.close()
                    if sender.send(b'first') != 5 or sender.send(b'extra') != 5:
                        raise JournalError('fixture packet send failed')
                    sender.close()
                fork_call(extra_packet_child)
                sender.close()
                if receiver.recv(32) != b'first':
                    raise JournalError('fixture first packet differs')
                expect_failure(lambda: _fixture_evidence_eof(receiver), 'extra fixture packet')
                _fixture_evidence_eof(receiver)
            finally:
                receiver.close()
                sender.close()
            if set(os.listdir('/proc/self/fd')) != descriptors:
                raise JournalError('fixture packet test leaked descriptors')
            # The installed supervisor directory excludes the frozen carrier;
            # use its existing verified first-boot location for this import.
            import sys
            original_path = sys.path[:]
            try:
                sys.path.append('/run/g2')
                import listener_negative
            finally:
                sys.path[:] = original_path

            class Roots:
                def scan(self):
                    raise JournalError('fixture realm invalid')

            class Life:
                def __init__(self):
                    self.invalid = False
                    self.stage = 'source_maintenance'
                    self.recoveries = []
                    self.roots = Roots()
                    self.journal = self
                    self.events = []

                def _admit(self, kind, action):
                    self.events.append(('admit', kind))

                def _register(self, kind, item):
                    self.events.append(('register', kind))

                def invalidate(self, reason):
                    self.invalid = True
                    self.events.append(('invalidate', reason))

                def append(self, kind, item):
                    self.events.append((kind, item['name']))

            class Postgres:
                def observe(self, recoveries):
                    return {'network_ids': networks}

            life = Life()
            result = listener_negative.exercise(life, Postgres(), 'extra-http')
            if not all(result[key] for key in ('live_listener_rejected', 'listener_removed',
                                                'direct_wait', 'pidfd_exited', 'sticky_invalid')):
                raise JournalError('normal listener negative did not complete')
            life = Life()
            original_receive = network._TopologyWindow._receive
            injected_count = [0]

            def injected(window, wait):
                if ('register', 'negative-listener-probe') in life.events and not injected_count[0]:
                    rows = network._read('/proc/net/tcp').splitlines()[1:]
                    if not any(len(row.split()) > 3 and row.split()[3] == '0A' for row in rows):
                        raise AssertionError('ACK failure injection lacks a live listener')
                    injected_count[0] += 1
                    return [(2, 0, window.sequence, 0, struct.pack('=i', -1) + bytes(16))]
                return original_receive(window, wait)

            network._TopologyWindow._receive = injected
            try:
                try:
                    listener_negative.exercise(life, Postgres(), 'extra-http')
                except JournalError as exc:
                    if str(exc) != 'route netlink acknowledgement failed':
                        raise
                else:
                    raise AssertionError('topology ACK failure was swallowed')
            finally:
                network._TopologyWindow._receive = original_receive
            if injected_count[0] != 1 or life.invalid or any(event[1] == 'negative-listener-complete' for event in life.events):
                raise JournalError('ACK failure incorrectly completed listener negative')
        elif name == 'multicast':
            return
        elif name in ('master', 'route', 'recreate', 'updown', 'late'):
            original = network._TopologyWindow.snapshot
            original_receive = network._TopologyWindow._receive
            original_read = network._read
            received = []
            count = [0]
            completed = [False]
            writer_evidence = [None]
            capture_size = [0, 0]
            namespace = child_ns[0]

            def mutate():
                if name == 'master':
                    set_link('vethaa', master=0)
                    set_link('vethaa', master=index('br-111111111111'))
                elif name in ('route', 'late'):
                    nsfd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
                    try:
                        held = os.fstat(nsfd)
                        original_ns = (held.st_dev, held.st_ino)
                        if original_ns != namespace:
                            raise JournalError('fixture writer namespace differs')
                        exchanges = []
                        destination = bytes.fromhex('20010db8' + '00' * 12)
                        change_route(24, destination, 64, 'vethaa', evidence=exchanges)
                        change_route(25, destination, 64, 'vethaa', evidence=exchanges)
                        held = os.fstat(nsfd)
                        named = os.stat('/proc/self/ns/net')
                        if (held.st_dev, held.st_ino) != original_ns or (named.st_dev, named.st_ino) != original_ns:
                            raise JournalError('fixture writer namespace changed')
                        return {'version': 1, 'namespace': list(original_ns), 'exchanges': exchanges}
                    finally:
                        os.close(nsfd)
                elif name == 'updown':
                    set_link('vethaa', up=False)
                    set_link('vethaa', up=True)
                else:
                    write(17, link(index('vethaa')))
                    create_veth()
                    for interface in ('vethaa', 'vethbb'):
                        set_link(interface, master=index('br-111111111111'))
                        set_link(interface, up=True)

            def topology():
                route = RouteSocket()
                try:
                    links = route.request(18, 0x300, link(), True)
                finally:
                    route.close()
                identities = sorted(network._link_message(body) for typ, body in links if typ == 16)
                return (identities, network._read('/proc/net/route'), network._read('/proc/net/ipv6_route'))

            def snapshot(window):
                result = original(window)
                count[0] += 1
                if name in ('master', 'route', 'late') and count[0] == (2 if name == 'late' else 1):
                    baseline = topology()
                    writer_evidence[0] = fork_call(mutate, route_evidence=name in ('route', 'late'))
                    if name in ('master', 'route', 'late'):
                        current = topology()
                        if (baseline[0] != current[0] or
                                (name != 'master' and baseline != current)):
                            raise JournalError('fixture mutation did not restore original topology')
                    completed[0] = True
                return result

            def mutate_after_ipv6_read():
                if count[0] != 1:
                    raise JournalError('fixture mutation requires the initial topology snapshot')
                fork_call(mutate)
                completed[0] = True

            def capture(window, wait):
                if name in ('route', 'late'):
                    sock = window.sock
                    tap = _FixtureReceiveTap(sock)
                    window.sock = tap
                    try:
                        messages = original_receive(window, wait)
                    finally:
                        window.sock = sock
                    if messages is not None:
                        if tap.result is None or network._route_messages(tap.result[0]) != messages:
                            raise JournalError('fixture receive evidence missing')
                        capture_size[0] += len(messages)
                        capture_size[1] += len(tap.result[0])
                        if capture_size[0] > 4096 or capture_size[1] > 1048576:
                            raise JournalError('fixture receive evidence exceeds bound')
                        if completed[0]:
                            received.append((window.sequence, messages, tap.result))
                else:
                    messages = original_receive(window, wait)
                    if completed[0] and messages is not None:
                        received.append((window.sequence, messages))
                return messages

            def rejected_message(error):
                return _fixture_rejected_notification(error, received, name, expected['vethaa'][0],
                                                      writer_evidence=writer_evidence[0], namespace=namespace)

            network._TopologyWindow.snapshot = snapshot
            network._TopologyWindow._receive = capture
            if name in ('recreate', 'updown'):
                network._read = _fixture_after_ipv6_read(original_read, mutate_after_ipv6_read)
            try:
                try:
                    verify()
                except JournalError as exc:
                    try:
                        rejected_message(exc)
                    except JournalError as attribution_error:
                        if name in ('route', 'late'):
                            import sys
                            last = None
                            if received:
                                seq, _, raw = received[-1]
                                last = {'sequence': seq, 'data_hex': raw[0].hex(), 'address': list(raw[3]),
                                        'flags': raw[2], 'ancillary': bool(raw[1])}
                            detail = json.dumps({'scenario': name, 'error': str(exc),
                                                 'writer_evidence': writer_evidence[0], 'received': last},
                                                sort_keys=True, separators=(',', ':'))
                            diagnostic = 'network route attribution failure: ' + detail
                            if len(diagnostic.encode('ascii')) > 49152:
                                diagnostic = 'fixture route attribution diagnostic bound exceeded'
                            print(diagnostic, file=sys.stderr)
                            raise JournalError('fixture route attribution failed') from None
                        detail = repr({'scenario': name, 'completed': completed[0], 'received': received})
                        raise JournalError(str(attribution_error) + ': ' + detail[:4000]) from attribution_error
                else:
                    raise AssertionError('topology mutation was accepted: ' + name)
                if not completed[0]:
                    raise JournalError('topology mutation did not complete successfully')
            finally:
                network._TopologyWindow.snapshot = original
                network._TopologyWindow._receive = original_receive
                network._read = original_read
        elif name == 'wrong-master':
            set_link('vethaa', master=0)
            expect_failure(verify, name)
        elif name == 'extra-interface':
            write(16, link() + attr(3, b'extra0\0') + kind_info('dummy'), 0x600)
            expect_failure(verify, name)
        elif name in ('global-route', 'default-route', 'gateway-route'):
            destination = bytes.fromhex('20010db8' + '00' * 12) if name != 'default-route' else bytes(16)
            gateway = bytes.fromhex('fe80' + '00' * 13 + '01') if name == 'gateway-route' else None
            change_route(24, destination, 64 if name != 'default-route' else 0, 'vethaa', gateway=gateway)
            expect_failure(verify, name)
        elif name in ('tcp', 'udp'):
            kind = socket.SOCK_STREAM if name == 'tcp' else socket.SOCK_DGRAM
            with socket.socket(socket.AF_INET, kind | socket.SOCK_CLOEXEC) as listener:
                listener.bind(('127.0.0.1', 0))
                if name == 'tcp':
                    listener.listen(1)
                expect_failure(verify, name)

    import contextlib
    with contextlib.ExitStack() as pins:
        parent_fds = []
        parent_ns = []
        for item in ('net', 'mnt'):
            fd = os.open('/proc/self/ns/' + item, os.O_RDONLY | os.O_CLOEXEC)
            pins.callback(os.close, fd)
            info = os.fstat(fd)
            parent_fds.append(fd)
            parent_ns.append((info.st_dev, info.st_ino))

        def check_parent_namespaces():
            for item, fd, identity in zip(('net', 'mnt'), parent_fds, parent_ns):
                held = os.fstat(fd)
                named = os.stat('/proc/self/ns/' + item)
                if (held.st_dev, held.st_ino) != identity or (named.st_dev, named.st_ino) != identity:
                    raise JournalError('parent namespace changed during fixture')

        check_parent_namespaces()
        try:
            for case in ('positive', 'multicast', 'master', 'route', 'recreate', 'updown', 'late',
                         'wrong-master', 'extra-interface', 'global-route', 'default-route', 'gateway-route', 'tcp', 'udp'):
                try:
                    fork_call(lambda case=case: scenario(case))
                finally:
                    check_parent_namespaces()
        finally:
            check_parent_namespaces()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=('protocol', 'roots', 'roots-mount', 'journal',
                                           'maintenance-protocol', 'maintenance-records', 'maintenance-selection',
                                           'maintenance-witness', 'maintenance-witness-cutpoints', 'maintenance-host-witness',
                                           'systemd-parser', 'systemd-failure', 'network-parser', 'network-window', 'audit-parser', 'bpf-runtime-root', 'bpf-containerd-launcher'), required=True)
    parser.add_argument('--scratch')
    parser.add_argument('--original-mount-fd', type=int)
    args = parser.parse_args()
    if args.case == 'protocol':
        protocol()
    elif args.case == 'maintenance-protocol':
        maintenance_protocol()
    elif args.case == 'maintenance-selection':
        maintenance_selection()
    elif args.case == 'maintenance-host-witness':
        maintenance_host_witness()
    elif args.case == 'audit-parser':
        audit_parser()
    elif args.case == 'bpf-runtime-root':
        bpf_runtime_root()
    elif args.case == 'bpf-containerd-launcher':
        bpf_containerd_launcher()
    elif args.case == 'systemd-parser':
        systemd_parser()
    elif args.case == 'network-parser':
        network_parser()
    elif args.case == 'network-window':
        network_window()
    else:
        if not args.scratch or not os.path.isdir(args.scratch) or os.path.islink(args.scratch):
            raise JournalError('a real isolated scratch directory is required')
        if args.case == 'roots':
            roots(args.scratch)
        elif args.case == 'roots-mount':
            root_mount_escape(args.scratch, args.original_mount_fd)
        elif args.case == 'journal':
            durable_journal(args.scratch)
        elif args.case == 'maintenance-records':
            maintenance_records(args.scratch)
        elif args.case == 'maintenance-witness-cutpoints':
            maintenance_witness_cutpoints(args.scratch)
        elif args.case == 'systemd-failure':
            try:
                systemd_failure(args.scratch)
            except Exception:
                import traceback
                import systemd_realm
                diagnostic = traceback.format_exc(limit=24).encode('utf-8')
                systemd_realm._record_failure(systemd_realm._Failure(
                    'systemd-smoke', 'predicate', stderr=diagnostic[:systemd_realm.LIMIT]))
                raise
        else:
            maintenance_witness(args.scratch)
    print('PASS ' + args.case)


if __name__ == '__main__':
    main()
