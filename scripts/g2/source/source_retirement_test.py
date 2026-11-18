"""Retirement integration regressions using private synthetic evidence and real files."""
from contextlib import contextmanager
import copy
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import tempfile
import subprocess
from types import SimpleNamespace
import unittest
from unittest import mock

import source_bpf_negative
import source_build
import source_host
import source_retirement as retirement
import source_verify
import source_vm


TOKEN = source_host.TOKEN
ID = lambda n: '%020d' % n


def sha(data):
    return hashlib.sha256(data).hexdigest()

def fixture_mount_domain(root):
    """Only unrelated Docker overlay/nsfs mounts may be exempted in these tests."""
    fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        mount_id = retirement._mount_id(fd)
    finally:
        os.close(fd)
    def decode(value):
        return re.sub(r'\\([0-7]{3})', lambda match: chr(int(match.group(1), 8)), value)
    mounts = []
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        fields = line.split()
        divider = fields.index('-')
        mounts.append({'id': int(fields[0]), 'device': fields[2], 'path': decode(fields[4]),
                       'type': fields[divider + 1]})
    target, = [row for row in mounts if row['id'] == mount_id]
    if target['type'] != 'btrfs':
        raise AssertionError('retirement fixture must use the approved btrfs scratch mount')
    excluded = [row for row in mounts if
                (row['type'] == 'overlay' and row['path'].startswith('/var/lib/docker/overlay2/') and
                 row['path'].endswith('/merged')) or
                (row['type'] == 'nsfs' and row['path'].startswith('/run/docker/netns/'))]
    for row in excluded:
        if (row['id'] == mount_id or Path(root).is_relative_to(row['path']) or
                Path(row['path']).is_relative_to(root) or row['device'] == target['device'] or
                row['type'] == 'btrfs'):
            raise AssertionError('lsof exemption intersects the fixture mount or device')
    return target, excluded



def private_dir(path):
    if path.exists():
        return
    private_dir(path.parent)
    path.mkdir(mode=0o700)


def put(path, data, mode=0o600):
    private_dir(path.parent)
    path.write_bytes(data)
    path.chmod(mode)


def receipt_bytes(value):
    return (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()


class FixtureHost:
    """Use production host validators against an isolated RUN and synthetic gate inputs."""
    RUN = None
    SOURCE_ROOT = None
    TOKEN = TOKEN
    Failure = source_host.Failure
    canonical = staticmethod(source_host.canonical)
    strict_json = staticmethod(source_host.strict_json)
    check_path = staticmethod(source_host.check_path)
    digest = staticmethod(source_host.digest)
    require = staticmethod(source_host.require)
    # The maintained checkout layout is real and tracked; validate it with the production check.
    validate_source_layout = staticmethod(source_host.validate_source_layout)

    @staticmethod
    def prepare_gate(frozen):
        # The pinned external preparation tree is outside this isolated fixture.
        return None


class RetirementFixture:
    def __init__(self, root, last=105):
        self.root = Path(root)
        self.host = FixtureHost()
        self.host.RUN = self.root
        self.host.SOURCE_ROOT = self.root / 'fixture-source'
        put(self.host.SOURCE_ROOT / 'source_host.py', b'current runner')
        put(self.root / 'source_host.py', b'historical runner')
        self.frozen = SimpleNamespace()
        self.selections = {}
        (self.root / 'source-build-selections').mkdir(mode=0o700)
        (self.root / 'source-build-attempts').mkdir(mode=0o700)
        (self.root / 'runs').mkdir(mode=0o700)
        put(self.root / 'prepare.json', b'prepare')
        put(self.root / 'postgres-image.json', b'postgres')
        mapping = {99: 103, 100: 104, 101: 105, 102: 106, 103: 107,
                   104: 108, 105: 109, 106: 110, 107: 111, 108: 112,
                   109: 113, 110: 114, 111: 115, 112: 116, 113: 117, 114: 118}
        for number in range(1, last + 1):
            self.build(number, mapping.get(number, number + 4 if number >= 115 else number))

    def build(self, selection_id, build_id):
        h = self.host
        directory = self.root / 'source-build-attempts' / ('attempt-' + ID(build_id))
        directory.mkdir(mode=0o700)
        binary = b'synthetic binary'
        archive = b'synthetic archive'
        binary_hash = sha(binary)
        archives = ('browser-image.tar', 'postgres-16.12.tar', 'containerd.io.deb',
                    'docker-ce-cli.deb', 'docker-ce.deb')
        seed = {'tool': binary_hash, **{name: sha(archive) for name in archives}}
        put(directory / 'seed/tool', binary)
        for name in archives:
            put(directory / 'seed' / name, archive)
        put(directory / 'source-build/bin/tool', binary)
        put(directory / 'source-build/c80.tar', archive)
        boot = {}
        for key, relative in (('kernel', 'boot/vmlinuz'), ('initrd', 'boot/initrd'),
                              ('original_initrd', 'boot/initrd.original')):
            put(directory / relative, archive)
            boot[key + '_path'] = relative
            boot[key + '_sha256'] = sha(archive)
        state = {key: {} for key in ('current_sources', 'wrapper_sources', 'initializer_sources',
                 'migrator_sources', 'domain_initializer_sources', 'launcher_sources', 'dns_probe_sources',
                 'comparison_negative_sources', 'backup_negative_sources', 'run_module', 'qemu_binaries',
                 'postgres_archive', 'browser_selection', 'runner_inputs')}
        state['runner_inputs'] = {'source_host.py': h.digest(self.root / 'source_host.py')[0]}
        state.update(version=1, run_token=TOKEN, attempt_id=ID(build_id), status='PASS',
                     predecessor_commit=source_build.COMMIT, c80_archive_sha256=sha(archive),
                     binaries={'tool': binary_hash}, seed_inputs=seed, boot_inputs=boot,
                     supervisor_manifest_sha256='0' * 64, prepare_sha256=sha(b'prepare'),
                     postgres_receipt_sha256=sha(b'postgres'), product_gate='NOT_READY',
                     permanent_deletion_http='CLOSED')
        raw = receipt_bytes(state)
        put(directory / 'receipt.json', raw)
        selection = {'version': 1, 'run_token': TOKEN, 'selection_id': ID(selection_id),
                     'attempt_id': ID(build_id), 'candidate_sha256': source_build._candidate_digest(h, state),
                     'receipt_sha256': sha(raw), 'seed_sha256': sha(h.canonical(seed)),
                     'binaries_sha256': sha(h.canonical(state['binaries']))}
        body = h.canonical(selection)
        put(self.root / 'source-build-selections' / ('selection-' + ID(selection_id) + '.json'), body, 0o400)
        self.selections[selection_id] = dict(selection, selection_sha256=sha(body))

    def vm(self, index, version=1):
        h = self.host
        case, scenario = retirement._candidate_vm(index)
        attempt = self.root / 'runs' / ('selection-' + ID(index)) / case / ('attempt-' + ID(1))
        directory = attempt / scenario
        private_dir(directory)
        leaves = ['boot.qcow2', 'app.raw', 'w1.raw', 'w2.raw', 'control.raw', 'seed.raw']
        if index <= 7:
            leaves += ['cidata-block-before_launch.raw', 'cidata-block-after_wait.raw']
        else:
            leaves += ['cidata.raw']
        records = {}
        for name in leaves:
            path = directory / name
            put(path, ('owned ' + name).encode())
            if name.startswith('cidata-block-'):
                with path.open('r+b') as stream:
                    stream.truncate(528482304)
            info = path.stat()
            records[name] = {'path': name, 'dev': info.st_dev, 'ino': info.st_ino, 'size': info.st_size}
        drives = [('boot', 'boot.qcow2'), ('app', 'app.raw'), ('w1', 'w1.raw'), ('w2', 'w2.raw'),
                  ('control', 'control.raw'), ('seed', 'seed.raw')]
        if index > 7:
            drives.append(('cidata', 'cidata.raw'))
        argv = ['qemu']
        for drive_id, name in drives:
            fmt = 'qcow2' if drive_id == 'boot' else 'raw'
            readonly = ',readonly=on' if drive_id in ('seed', 'cidata') else ''
            argv.extend(['-drive', 'if=none,id=%s,format=%s%s,file=%s' %
                         (drive_id, fmt, readonly, directory / name),
                         '-device', 'virtio-blk-pci,id=%sdisk,drive=%s' % (drive_id, drive_id)])
        if index <= 7:
            (directory / 'cidata').mkdir(mode=0o700)
            argv.extend(['-blockdev', 'driver=vvfat,node-name=cidata,dir=' + str(directory / 'cidata') +
                         ',label=CIDATA,rw=off,read-only=on',
                         '-device', 'virtio-blk-pci,id=cidatadisk,drive=cidata'])
        selection = self.selections[index]
        launch = {'run_token': TOKEN, 'case': case, 'scenario': scenario, 'build_selection': selection,
                  'argv_sha256': sha(h.canonical(argv)), 'boot': records['boot.qcow2'],
                  'seed_image': records['seed.raw'],
                  'disks': {name: records[name + '.raw'] for name in ('app', 'w1', 'w2', 'control')}}
        launch['boot']['size'] = 1  # The initial qcow2 size is not its post-run size.
        process = {'pid': 2147483000, 'wait_observed': True, 'pidfd_exited': True,
                   'exit_observed': True, 'exit_uncertain': False, 'argv': argv,
                   'argv_sha256': sha(h.canonical(argv))}
        record = {'version': version, 'run_token': TOKEN, 'status': 'FAIL', 'case': case,
                  'scenario': scenario, 'build_selection': selection, 'attempt': process,
                  'disks_before': copy.deepcopy(launch['disks']), 'seed_image': copy.deepcopy(launch['seed_image']),
                  'host_launch': launch, 'host_launch_sha256': sha(h.canonical(launch))}
        if index <= 7:
            record['carrier_observations'] = [
                {'stage': stage, 'cidata_block': records['cidata-block-' + stage + '.raw']}
                for stage in ('before_launch', 'after_wait')]
        else:
            record['cidata_image'] = records['cidata.raw']
        if index >= 64:
            manifest = {'version': 1, 'run_token': TOKEN, 'build_selection': selection, 'attempt': ID(1),
                        'case': case, 'scenarios': [scenario],
                        'members': [{'case': case, 'scenario': scenario, 'variant': None}]}
            raw = h.canonical(manifest)
            put(attempt / 'launch-manifest.json', raw, 0o400)
            record['launch_manifest_sha256'] = sha(raw)
        put(directory / 'receipt.json', receipt_bytes(record))
        put(directory / 'payload/keep', b'payload')
        put(directory / 'serial.log', b'log')
        put(directory / 'parsed.json', b'{}')
        return directory

    def manifest(self):
        result = retirement.inventory(self.host, self.frozen)
        if result['manifest_path'] is None:
            raise AssertionError('expected eligible retirement owners: %r' % result['excluded_owners'])
        path = Path(result['manifest_path'])
        raw = path.read_bytes()
        assert sha(raw) == result['manifest_sha256']
        return result, self.host.strict_json(raw)

    def apply(self, result):
        return retirement.apply(self.host, self.frozen, result['manifest_sha256'])


class RetirementTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.fixture = RetirementFixture(self.tmp.name)
        self.h = self.fixture.host
        self.root = self.fixture.root
        self.fixture_mount, self.exempt_mounts = fixture_mount_domain(self.root)
        self.use_mount_exemptions = True
        # Inspect real proc entries and real lsof results for only this fixture's
        # process tree; unrelated same-UID namespaces are outside this fixture.
        root_pid = os.getpid()
        children = set()
        real_fork, real_scandir, real_run = os.fork, os.scandir, subprocess.run

        def fork_owned():
            pid = real_fork()
            if pid:
                children.add(pid)
            return pid

        def owned_pids():
            return {pid for pid in children | {root_pid, os.getpid()}
                    if os.path.exists('/proc/%d' % pid)}

        class Entries:
            def __init__(self, entries):
                self.entries = entries
            def __iter__(self):
                return iter(self.entries)
            def __enter__(self):
                return self
            def __exit__(self, *args):
                return False

        def scan_owned(path):
            if str(path) != '/proc':
                return real_scandir(path)
            allowed = {str(pid) for pid in owned_pids()}
            with real_scandir(path) as entries:
                return Entries([entry for entry in entries if entry.name in allowed])

        def run_owned(args, *positional, **keywords):
            if args and args[0] == '/usr/bin/lsof':
                split = args.index('-f')
                pids = ','.join(str(pid) for pid in sorted(owned_pids()))
                if not self.use_mount_exemptions:
                    args = [arg for index, arg in enumerate(args)
                            if arg != '-e' and (index == 0 or args[index - 1] != '-e')]
                    split = args.index('-f')
                args = args[:split] + ['-a', '-p', pids] + args[split:]
            return real_run(args, *positional, **keywords)

        self.enterContext(mock.patch.object(retirement.os, 'fork', side_effect=fork_owned))
        self.enterContext(mock.patch.object(retirement.os, 'scandir', side_effect=scan_owned))
        self.enterContext(mock.patch.object(retirement.subprocess, 'run', side_effect=run_owned))

    @contextmanager
    def held_file(self, path):
        ready_r, ready_w = os.pipe()
        gate_r, gate_w = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(ready_r)
            os.close(gate_w)
            try:
                fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC)
                os.write(ready_w, str(fd).encode('ascii'))
                os.read(gate_r, 1)
                os.close(fd)
            except BaseException:
                os._exit(1)
            os._exit(0)
        os.close(ready_w)
        os.close(gate_r)
        try:
            raw = os.read(ready_r, 32)
            self.assertTrue(raw.isdigit(), 'child failed to open real fixture file')
            yield child, int(raw)
        finally:
            try:
                os.write(gate_w, b'X')
            except BrokenPipeError:
                pass
            os.close(ready_r)
            os.close(gate_w)
            _, status = os.waitpid(child, 0)
            self.assertTrue(os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0)

    @contextmanager
    def working_directory_holder(self, path):
        ready_r, ready_w = os.pipe()
        gate_r, gate_w = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(ready_r)
            os.close(gate_w)
            try:
                os.chdir(path)
                os.write(ready_w, b'R')
                os.read(gate_r, 1)
            except BaseException:
                os._exit(1)
            os._exit(0)
        os.close(ready_w)
        os.close(gate_r)
        try:
            self.assertEqual(os.read(ready_r, 1), b'R')
            yield child
        finally:
            try:
                os.write(gate_w, b'X')
            except BrokenPipeError:
                pass
            os.close(ready_r)
            os.close(gate_w)
            _, status = os.waitpid(child, 0)
            self.assertTrue(os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0)

    def real_lsof(self, path):
        exemptions = ([arg for mount in self.exempt_mounts for arg in ('-e', mount['path'])]
                      if self.use_mount_exemptions else [])
        return subprocess.run(['/usr/bin/lsof', '-n', '-P', '-Fpn'] + exemptions + ['-f', '--', str(path)],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)

    def owner(self, manifest, directory):
        relative = str(directory.relative_to(self.root))
        return next((x for x in manifest['owners'] if x['relative_directory'] == relative), None)

    def excluded(self, manifest, directory):
        relative = str(directory.relative_to(self.root))
        return [x for x in manifest['excluded_owners'] if x['path'] == relative]

    def assert_same_complete(self, first, second):
        self.assertEqual({key: value for key, value in first.items() if key != 'activity_disclosure'},
                         {key: value for key, value in second.items() if key != 'activity_disclosure'})

    def test_legacy_and_modern_owners_and_exact_retention(self):
        dirs = [self.fixture.vm(4), self.fixture.vm(10), self.fixture.vm(64), self.fixture.vm(104, version=2)]
        result, manifest = self.fixture.manifest()
        self.assertEqual(manifest['policy'], 'houfeng.source-retirement.v2')
        self.assertEqual(set(manifest['activity_disclosure']), {'inspected_processes', 'undetermined'})
        for directory, kind in zip(dirs, ('legacy_host_launch', 'legacy_host_launch', 'launch_manifest', 'launch_manifest')):
            owner = self.owner(manifest, directory)
            self.assertIsNotNone(owner)
            self.assertEqual(owner['binding_kind'], kind)
            self.assertEqual(owner['launch_manifest_sha256'] is None, kind == 'legacy_host_launch')
        four = self.owner(manifest, dirs[0])
        idx = manifest['owners'].index(four)
        names = {Path(item['path']).name for item in manifest['files'] if item['owner'] == idx}
        self.assertIn('cidata-block-before_launch.raw', names)
        self.assertIn('cidata-block-after_wait.raw', names)
        self.assertNotIn('cidata.raw', names)
        saved = {path: sha(path.read_bytes()) for directory in dirs
                 for path in (directory / 'receipt.json', directory / 'serial.log', directory / 'payload/keep', directory / 'parsed.json')}
        complete = self.fixture.apply(result)
        self.assertIn('activity_disclosure', complete)
        persisted = self.h.strict_json((self.root / 'source-retirements' /
                                       ('complete-' + result['manifest_sha256'] + '.json')).read_bytes())
        self.assertNotIn('activity_disclosure', persisted)
        self.assertEqual(complete['status'], 'RETIRED')
        self.assertEqual(complete['removed_charged_bytes'], sum(x['identity']['allocated_bytes'] for x in manifest['files']))
        self.assert_same_complete(self.fixture.apply(result), complete)
        for item in manifest['files']:
            self.assertFalse((self.root / item['path']).exists())
        for path, digest in saved.items():
            self.assertEqual(sha(path.read_bytes()), digest)
        for directory in dirs:
            with self.assertRaisesRegex(self.h.Failure, 'artifact_retired'):
                retirement.assert_available(self.h, directory)
            with self.assertRaisesRegex(self.h.Failure, 'artifact_retired'):
                retirement.assert_available(self.h, directory.parent)

    def test_extended_broker_range_and_selected_exclusion(self):
        for number in range(106, 145):
            self.fixture.build(number, number + 4)
        directories = [self.fixture.vm(number) for number in range(115, 144)]
        _, manifest = self.fixture.manifest()
        for directory in directories:
            self.assertIsNotNone(self.owner(manifest, directory))
        self.assertFalse(set(retirement._PINS) & {int(owner['selection_id']) for owner in manifest['owners']})
        self.assertNotIn(144, {int(owner['selection_id']) for owner in manifest['owners']})
        with mock.patch.object(source_build, '_selected', return_value={'_selection': self.fixture.selections[143]}):
            _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, directories[-1]))
        self.assertEqual(self.excluded(manifest, directories[-1])[0]['reason'], 'owner_pinned_or_retired')

    def test_disclosure_schema_rejects_invalid_entries(self):
        _, manifest = self.fixture.manifest()
        root_info = self.root.stat()
        for disclosure in ({'inspected_processes': True, 'undetermined': []},
                           {'inspected_processes': 1, 'undetermined': [{'pid': 1, 'comm': 'x' * 65,
                                                                        'reason': 'x', 'fields': ['fd']}]},
                           {'inspected_processes': 1, 'undetermined': [{'pid': 2, 'comm': '', 'reason': 'x',
                                                                        'fields': ['fd']}] * 2}):
            with self.subTest(disclosure=disclosure), self.assertRaises(self.h.Failure):
                retirement._manifest_schema(self.h, dict(manifest, activity_disclosure=disclosure), root_info)

    def test_all_54_legacy_shapes_bind_real_indented_receipts(self):
        indices = [*range(4, 8), *range(10, 18), *range(19, 32), *range(33, 54), *range(55, 63)]
        self.assertEqual(len(indices), 54)
        directories = [self.fixture.vm(index) for index in indices]
        _, manifest = self.fixture.manifest()
        for directory in directories:
            with self.subTest(directory=directory):
                self.assertEqual(self.owner(manifest, directory)['binding_kind'], 'legacy_host_launch')

    def test_missing_disk_and_incomplete_exit_exclude_entire_owner(self):
        missing = self.fixture.vm(4)
        uncertain = self.fixture.vm(10)
        (missing / 'seed.raw').unlink()
        path = uncertain / 'receipt.json'
        record = self.h.strict_json(path.read_bytes())
        del record['attempt']['wait_observed']
        put(path, receipt_bytes(record))
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, missing))
        self.assertEqual(self.excluded(manifest, missing)[0]['reason'], 'preexisting_artifact_missing')
        self.assertIsNone(self.owner(manifest, uncertain))
        self.assertTrue(self.excluded(manifest, uncertain))
        self.assertTrue((missing / 'app.raw').exists())

    def test_bad_launch_argv_and_manifest_bindings_are_excluded(self):
        legacy = self.fixture.vm(10)
        modern = self.fixture.vm(64)
        path = legacy / 'receipt.json'
        record = self.h.strict_json(path.read_bytes())
        record['attempt']['argv'][0] = 'different'
        put(path, receipt_bytes(record))
        path = modern.parent / 'launch-manifest.json'
        record = self.h.strict_json(path.read_bytes())
        record['run_token'] = 'incorrect'
        path.chmod(0o600)
        put(path, self.h.canonical(record), 0o400)
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, legacy))
        self.assertIsNone(self.owner(manifest, modern))

    def test_historical_build_receipt_byte_change_rejects_inventory(self):
        directory = self.fixture.vm(10)
        path = self.root / 'source-build-attempts' / ('attempt-' + ID(10)) / 'receipt.json'
        value = self.h.strict_json(path.read_bytes())
        put(path, self.h.canonical(value))  # Same JSON values, different original receipt bytes.
        with self.assertRaises(self.h.Failure):
            self.fixture.manifest()
        self.assertTrue((directory / 'app.raw').exists())
        self.assertFalse((self.root / 'source-retirements').exists())

    def test_reboot_exit_and_legacy_snapshot_identity_are_required(self):
        reboot = self.fixture.vm(10)
        snapshot = self.fixture.vm(4)
        path = reboot / 'receipt.json'
        record = self.h.strict_json(path.read_bytes())
        record['reboot_attempt'] = {'pid': 123, 'wait_observed': True, 'pidfd_exited': True,
                                    'exit_observed': False, 'exit_uncertain': False}
        put(path, receipt_bytes(record))
        path = snapshot / 'receipt.json'
        record = self.h.strict_json(path.read_bytes())
        record['carrier_observations'][0]['cidata_block']['ino'] += 1
        put(path, receipt_bytes(record))
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, reboot))
        self.assertIsNone(self.owner(manifest, snapshot))
        self.assertTrue((reboot / 'boot.qcow2').exists())
        self.assertTrue((snapshot / 'cidata-block-before_launch.raw').exists())

    def test_pins_and_status_are_not_retired(self):
        passed = self.fixture.vm(10)
        pending = self.fixture.vm(11)
        for directory, status in ((passed, 'PASS'), (pending, 'IN_PROGRESS')):
            path = directory / 'receipt.json'
            record = self.h.strict_json(path.read_bytes())
            record['status'] = status
            put(path, receipt_bytes(record))
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, passed))
        self.assertIsNone(self.owner(manifest, pending))
        self.assertFalse({1, 18, 54, 63, 112, 113, 114} & {int(x['selection_id']) for x in manifest['owners']})
        self.assertNotIn(self.fixture.selections[105]['attempt_id'], {x['build_attempt_id'] for x in manifest['owners']})

    def test_build_original_digest_is_required_before_inventory(self):
        directory = self.root / 'source-build-attempts' / ('attempt-' + ID(2))
        for relative in ('seed/browser-image.tar', 'seed/postgres-16.12.tar', 'seed/containerd.io.deb',
                         'seed/docker-ce-cli.deb', 'seed/docker-ce.deb', 'seed/tool', 'source-build/bin/tool',
                         'source-build/c80.tar', 'boot/vmlinuz', 'boot/initrd', 'boot/initrd.original'):
            path = directory / relative
            original = path.read_bytes()
            with self.subTest(relative=relative):
                put(path, b'replacement with same ownership')
                _, manifest = self.fixture.manifest()
                self.assertIsNone(self.owner(manifest, directory))
                self.assertTrue(self.excluded(manifest, directory))
                put(path, original)

    def test_external_run_reference_pins_unlaunched_build(self):
        directory = self.root / 'source-build-attempts' / ('attempt-' + ID(2))
        reference = self.root / 'runs' / ('selection-' + ID(1)) / 'realm-negative' / ('attempt-' + ID(1))
        reference_manifest = {'version': 1, 'run_token': TOKEN, 'build_selection': self.fixture.selections[2],
                              'attempt': ID(1), 'case': 'realm-negative', 'scenarios': ['observer-capability'],
                              'members': [{'case': 'realm-negative', 'scenario': 'observer-capability', 'variant': None}]}
        put(reference / 'launch-manifest.json', self.h.canonical(reference_manifest), 0o400)
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, directory))
        self.assertTrue(self.excluded(manifest, directory))

    def test_unknown_reverse_record_produces_empty_inventory(self):
        directory = self.fixture.vm(10)
        reference = self.root / 'runs' / ('selection-' + ID(1)) / 'realm-negative' / ('attempt-' + ID(1))
        put(reference / 'launch-manifest.json', self.h.canonical({'unknown': 'reference'}), 0o400)
        result = retirement.inventory(self.h, self.fixture.frozen)
        self.assertEqual(result['status'], 'EMPTY')
        self.assertIsNone(result['manifest_path'])
        self.assertIsNone(result['manifest_sha256'])
        self.assertEqual(result['files'], [])
        self.assertTrue(self.excluded(result, directory))
        self.assertTrue((directory / 'app.raw').exists())
        self.assertFalse((self.root / 'source-retirements').exists())

    def test_new_selection_before_first_apply_blocks_all_unlinks(self):
        self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        selection = dict(self.fixture.selections[104])
        del selection['selection_sha256']
        selection['selection_id'] = ID(106)
        put(self.root / 'source-build-selections' / ('selection-' + ID(106) + '.json'),
            self.h.canonical(selection), 0o400)
        with self.assertRaises((self.h.Failure, OSError)):
            self.fixture.apply(result)
        self.assertTrue(all((self.root / x['path']).exists() for x in manifest['files']))
        self.assertFalse((self.root / 'source-retirements' / ('intent-' + result['manifest_sha256'] + '.json')).exists())

    def test_leaf_replacement_after_inventory_aborts_before_unlink(self):
        directory = self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        victim = directory / 'app.raw'
        victim.unlink()
        put(victim, b'replacement')
        with self.assertRaises((self.h.Failure, OSError)):
            self.fixture.apply(result)
        self.assertEqual(victim.read_bytes(), b'replacement')
        self.assertTrue(all((self.root / item['path']).exists() for item in manifest['files']))
        self.assertFalse((self.root / 'source-retirements' / ('intent-' + result['manifest_sha256'] + '.json')).exists())

    def test_symlink_and_hardlink_owners_are_excluded(self):
        symlinked = self.fixture.vm(10)
        linked = self.fixture.vm(11)
        path = symlinked / 'app.raw'
        path.unlink()
        path.symlink_to(symlinked / 'w1.raw')
        os.link(linked / 'app.raw', linked / 'extra-link')
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, symlinked))
        self.assertIsNone(self.owner(manifest, linked))

    def test_intermediate_directory_replaced_after_inventory_is_rejected(self):
        directory = self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        parent = directory.parent
        moved = parent.with_name(parent.name + '-moved')
        parent.rename(moved)
        parent.mkdir(mode=0o700)
        moved_directory = moved / directory.name
        restored = parent / directory.name
        restored.mkdir(mode=0o700)
        for path in moved_directory.iterdir():
            path.rename(restored / path.name)
        with self.assertRaises((self.h.Failure, OSError)):
            self.fixture.apply(result)
        self.assertTrue(all((self.root / item['path']).exists() for item in manifest['files']))

    def test_lock_competition_rejects_inventory_and_negative_before_reading(self):
        lock = source_build.lock_run(self.h)
        try:
            with self.assertRaises((BlockingIOError, self.h.Failure)):
                retirement.inventory(self.h, self.fixture.frozen)
            with mock.patch.object(source_build, 'build_gate', side_effect=AssertionError('read before lock')):
                with self.assertRaises((BlockingIOError, self.h.Failure)):
                    source_bpf_negative._verify_selected(self.h, self.fixture.frozen)
        finally:
            os.close(lock)
        lock = source_build.lock_run(self.h)
        os.close(lock)

    def test_real_negative_gate_holds_lock_and_releases_after_failure(self):
        selections = self.root / 'source-build-selections'
        selections.rename(self.root / 'saved-selections')
        selections.mkdir(mode=0o700)
        ready_r, ready_w = os.pipe()
        release_r, release_w = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(ready_r)
            os.close(release_w)
            original = source_build.build_gate
            def paused(host, frozen):
                os.write(ready_w, b'R')
                os.read(release_r, 1)
                return original(host, frozen)
            try:
                with mock.patch.object(source_build, 'build_gate', side_effect=paused):
                    source_bpf_negative._verify_selected(self.h, self.fixture.frozen)
            except self.h.Failure as exc:
                os._exit(0 if str(exc) == 'no successful source build selection exists' else 4)
            except Exception:
                os._exit(5)
            os._exit(1)
        os.close(ready_w)
        os.close(release_r)
        try:
            self.assertEqual(os.read(ready_r, 1), b'R')
            with self.assertRaises((BlockingIOError, self.h.Failure)):
                source_build.lock_run(self.h)
            with self.assertRaises((BlockingIOError, self.h.Failure)):
                retirement.inventory(self.h, self.fixture.frozen)
            os.write(release_w, b'X')
            _, status = os.waitpid(child, 0)
            child = None
            self.assertTrue(os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0)
            lock = source_build.lock_run(self.h)
            os.close(lock)
        finally:
            os.close(ready_r)
            os.close(release_w)
            if child is not None:
                os.kill(child, signal.SIGKILL)
                os.waitpid(child, 0)

    def _kill_at_barrier(self, fixture, phase, result=None):
        ready_r, ready_w = os.pipe()
        gate_r, gate_w = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(ready_r)
            os.close(gate_w)
            publish = retirement._publish
            unlink = os.unlink
            fsync = os.fsync
            mkdir = os.mkdir
            root_stat = fixture.root.stat()
            def barrier():
                os.write(ready_w, b'R')
                os.read(gate_r, 1)
            def created(path, *args, **kwargs):
                value = mkdir(path, *args, **kwargs)
                parent = kwargs.get('dir_fd')
                if phase == 'mkdir' and path == 'source-retirements' and parent is not None:
                    info = os.fstat(parent)
                    if (info.st_dev, info.st_ino) == (root_stat.st_dev, root_stat.st_ino):
                        barrier()
                return value
            def published(host, directory, name, data, dev, mount):
                value = publish(host, directory, name, data, dev, mount)
                if (phase == 'manifest' and name.startswith('manifest-')) or (
                        phase == 'intent' and name.startswith('intent-')):
                    barrier()
                return value
            def unlinked(path, *args, **kwargs):
                value = unlink(path, *args, **kwargs)
                if phase == 'unlink' and kwargs.get('dir_fd') is not None:
                    barrier()
                return value
            def syncing(fd):
                if phase == 'complete' and stat.S_ISDIR(os.fstat(fd).st_mode):
                    record_dir = fixture.root / 'source-retirements'
                    if os.readlink('/proc/self/fd/%d' % fd) == str(record_dir) and list(record_dir.glob('complete-*.json')):
                        barrier()
                return fsync(fd)
            try:
                with mock.patch.object(retirement, '_publish', side_effect=published), \
                     mock.patch.object(retirement.os, 'mkdir', side_effect=created), \
                     mock.patch.object(retirement.os, 'unlink', side_effect=unlinked), \
                     mock.patch.object(retirement.os, 'fsync', side_effect=syncing):
                    if phase in ('manifest', 'mkdir'):
                        fixture.manifest()
                    else:
                        fixture.apply(result)
            except BaseException as exc:
                os.write(ready_w, b'E' + repr(exc).encode()[:400])
                os._exit(2)
            os.write(ready_w, b'F')
            os._exit(3)
        os.close(ready_w)
        os.close(gate_r)
        try:
            marker = os.read(ready_r, 1)
            if marker != b'R':
                detail = os.read(ready_r, 400)
                self.fail('child missed %s barrier: %r %r' % (phase, marker, detail))
            killed_pid = child
            os.kill(child, signal.SIGKILL)
            _, status = os.waitpid(child, 0)
            child = None
            self.assertTrue(os.WIFSIGNALED(status) and os.WTERMSIG(status) == signal.SIGKILL)
        finally:
            os.close(ready_r)
            os.close(gate_w)
            if child is not None:
                os.kill(child, signal.SIGKILL)
                os.waitpid(child, 0)
        return killed_pid

    def _snapshot_pinned_directories(self, fixture):
        snapshot = {}
        for number in (1, 18, 54, 63, 112, 113, 114):
            attempt = fixture.selections[number]['attempt_id']
            root = fixture.root / 'source-build-attempts' / ('attempt-' + attempt)
            for path in (root, *root.rglob('*')):
                if path.is_dir():
                    info = path.stat()
                    snapshot[path.relative_to(fixture.root).as_posix()] = (
                        info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode)
        return snapshot

    def _assert_pinned_directories(self, fixture, snapshot):
        for relative, expected in snapshot.items():
            info = (fixture.root / relative).stat()
            self.assertEqual((info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode), expected)

    def _snapshot_fixture_files(self, fixture):
        snapshot = {}
        for path in fixture.root.rglob('*'):
            if not path.is_file():
                continue
            info = path.stat()
            snapshot[path.relative_to(fixture.root).as_posix()] = (
                info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_size,
                sha(path.read_bytes()))
        return snapshot

    def _assert_original_files(self, fixture, snapshot, paths):
        for relative in paths:
            with self.subTest(path=relative):
                path = fixture.root / relative
                info = path.stat()
                actual = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_size,
                          sha(path.read_bytes()))
                self.assertEqual(actual, snapshot[relative])

    def _pinned_and_retained_paths(self, fixture, snapshot, manifest):
        pinned = {fixture.selections[number]['attempt_id'] for number in (1, 18, 54, 63, 112, 113, 114)}
        paths = {name for name in snapshot if name.startswith('source-build-selections/') or
                 name in ('prepare.json', 'postgres-image.json') or
                 any(name.startswith('source-build-attempts/attempt-' + attempt + '/') for attempt in pinned)}
        paths.update(item['path'] for item in manifest['retained_metadata'])
        self.assertTrue(paths <= snapshot.keys())
        return paths

    def _retry_with_root_trace(self, fixture, result=None):
        read_fd, write_fd = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(read_fd)
            root_info = fixture.root.stat()
            root_identity = (root_info.st_dev, root_info.st_ino)
            fsync, unlink = os.fsync, os.unlink
            events = []
            stage = 'inventory' if result is None else 'apply'
            leaf_names = set()
            def traced_fsync(fd):
                info = os.fstat(fd)
                value = fsync(fd)
                if stat.S_ISDIR(info.st_mode) and (info.st_dev, info.st_ino) == root_identity:
                    events.append(stage + ':root-fsync')
                return value
            def traced_unlink(path, *args, **kwargs):
                if stage == 'apply' and kwargs.get('dir_fd') is not None and str(path) in leaf_names:
                    if 'apply:unlink' not in events:
                        events.append('apply:unlink')
                return unlink(path, *args, **kwargs)
            try:
                with mock.patch.object(retirement.os, 'fsync', side_effect=traced_fsync), \
                     mock.patch.object(retirement.os, 'unlink', side_effect=traced_unlink):
                    if result is None:
                        chosen, manifest = fixture.manifest()
                    else:
                        chosen = result
                        path = fixture.root / 'source-retirements' / ('manifest-' + chosen['manifest_sha256'] + '.json')
                        manifest = fixture.host.strict_json(path.read_bytes())
                    leaf_names.update(Path(item['path']).name for item in manifest['files'])
                    stage = 'apply'
                    complete = fixture.apply(chosen)
                report = {'pid': os.getpid(), 'digest': chosen['manifest_sha256'], 'events': events,
                          'complete': complete}
                os.write(write_fd, json.dumps(report).encode())
                os._exit(0)
            except BaseException as exc:
                os.write(write_fd, json.dumps({'error': repr(exc)}).encode())
                os._exit(1)
        os.close(write_fd)
        try:
            chunks = []
            while True:
                chunk = os.read(read_fd, 65536)
                if not chunk:
                    break
                chunks.append(chunk)
        finally:
            os.close(read_fd)
        _, status = os.waitpid(child, 0)
        report = json.loads(b''.join(chunks))
        self.assertTrue(os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0, report)
        self.assertNotEqual(report['pid'], os.getpid())
        events = report['events']
        if result is None:
            self.assertIn('inventory:root-fsync', events)
        self.assertIn('apply:root-fsync', events)
        self.assertIn('apply:unlink', events)
        self.assertLess(events.index('apply:root-fsync'), events.index('apply:unlink'))
        self.assertEqual(report['complete']['status'], 'RETIRED')
        path = fixture.root / 'source-retirements' / ('manifest-' + report['digest'] + '.json')
        raw = path.read_bytes()
        self.assertEqual(sha(raw), report['digest'])
        return report, fixture.host.strict_json(raw)

    def test_root_durability_recovers_after_mkdir_sigkill(self):
        with tempfile.TemporaryDirectory() as root:
            fixture = RetirementFixture(root, last=114)
            fixture.vm(10)
            snapshot = self._snapshot_fixture_files(fixture)
            pin_directories = self._snapshot_pinned_directories(fixture)
            killed_pid = self._kill_at_barrier(fixture, 'mkdir')
            records = fixture.root / 'source-retirements'
            self.assertTrue(records.is_dir())
            self.assertEqual(list(records.iterdir()), [])
            self._assert_original_files(fixture, snapshot, snapshot.keys())
            report, manifest = self._retry_with_root_trace(fixture)
            self.assertNotEqual(report['pid'], killed_pid)
            self._assert_pinned_directories(fixture, pin_directories)
            self.assertTrue(all(not (fixture.root / item['path']).exists() for item in manifest['files']))
            self._assert_original_files(fixture, snapshot, self._pinned_and_retained_paths(fixture, snapshot, manifest))
            self.assert_same_complete(fixture.apply({'manifest_sha256': report['digest']}), report['complete'])

    def test_root_durability_recovers_after_first_root_fsync_eio(self):
        with tempfile.TemporaryDirectory() as root:
            fixture = RetirementFixture(root, last=114)
            fixture.vm(10)
            snapshot = self._snapshot_fixture_files(fixture)
            pin_directories = self._snapshot_pinned_directories(fixture)
            root_info = fixture.root.stat()
            identity = (root_info.st_dev, root_info.st_ino)
            fsync = os.fsync
            failed = []
            def fail_first_root(fd):
                info = os.fstat(fd)
                if not failed and stat.S_ISDIR(info.st_mode) and (info.st_dev, info.st_ino) == identity:
                    failed.append(True)
                    raise OSError(errno.EIO, 'injected first run-directory fsync failure')
                return fsync(fd)
            with mock.patch.object(retirement.os, 'fsync', side_effect=fail_first_root):
                with self.assertRaises(OSError) as raised:
                    fixture.manifest()
            self.assertEqual(raised.exception.errno, errno.EIO)
            self.assertEqual(failed, [True])
            records = fixture.root / 'source-retirements'
            self.assertTrue(records.is_dir())
            self.assertEqual(list(records.iterdir()), [])
            self._assert_original_files(fixture, snapshot, snapshot.keys())
            report, manifest = self._retry_with_root_trace(fixture)
            self._assert_pinned_directories(fixture, pin_directories)
            self.assertTrue(all(not (fixture.root / item['path']).exists() for item in manifest['files']))
            self._assert_original_files(fixture, snapshot, self._pinned_and_retained_paths(fixture, snapshot, manifest))
            self.assert_same_complete(fixture.apply({'manifest_sha256': report['digest']}), report['complete'])

    def test_existing_store_apply_root_fsync_eio_precedes_intent_and_unlink(self):
        with tempfile.TemporaryDirectory() as root:
            fixture = RetirementFixture(root, last=114)
            fixture.vm(10)
            snapshot = self._snapshot_fixture_files(fixture)
            pin_directories = self._snapshot_pinned_directories(fixture)
            result, manifest = fixture.manifest()
            root_info = fixture.root.stat()
            identity = (root_info.st_dev, root_info.st_ino)
            fsync, unlink = os.fsync, os.unlink
            failed, unlinks = [], []
            def fail_first_root(fd):
                info = os.fstat(fd)
                if not failed and stat.S_ISDIR(info.st_mode) and (info.st_dev, info.st_ino) == identity:
                    failed.append(True)
                    raise OSError(errno.EIO, 'injected existing-store run-directory fsync failure')
                return fsync(fd)
            def track_unlink(path, *args, **kwargs):
                unlinks.append(str(path))
                return unlink(path, *args, **kwargs)
            with mock.patch.object(retirement.os, 'fsync', side_effect=fail_first_root), \
                 mock.patch.object(retirement.os, 'unlink', side_effect=track_unlink):
                with self.assertRaises(OSError) as raised:
                    fixture.apply(result)
            self.assertEqual(raised.exception.errno, errno.EIO)
            self.assertEqual(failed, [True])
            self.assertEqual(unlinks, [])
            self.assertFalse((fixture.root / 'source-retirements' /
                              ('intent-' + result['manifest_sha256'] + '.json')).exists())
            self.assertTrue(all((fixture.root / item['path']).exists() for item in manifest['files']))
            self._assert_original_files(fixture, snapshot, snapshot.keys())
            report, recovered = self._retry_with_root_trace(fixture, result)
            self._assert_pinned_directories(fixture, pin_directories)
            self.assertEqual(report['digest'], result['manifest_sha256'])
            self.assertTrue(all(not (fixture.root / item['path']).exists() for item in recovered['files']))
            self._assert_original_files(fixture, snapshot, self._pinned_and_retained_paths(fixture, snapshot, recovered))
            self.assert_same_complete(fixture.apply(result), report['complete'])

    def test_crash_recovery_at_durable_boundaries(self):
        for phase in ('manifest', 'intent', 'unlink', 'complete'):
            with self.subTest(phase=phase), tempfile.TemporaryDirectory() as root:
                fixture = RetirementFixture(root)
                owner = fixture.vm(10)
                if phase == 'manifest':
                    self._kill_at_barrier(fixture, phase)
                    paths = list((fixture.root / 'source-retirements').glob('manifest-*.json'))
                    self.assertEqual(len(paths), 1)
                    path = paths[0]
                    result = {'manifest_path': str(path), 'manifest_sha256': path.stem[len('manifest-'):]}
                    manifest = fixture.host.strict_json(path.read_bytes())
                else:
                    result, manifest = fixture.manifest()
                    self._kill_at_barrier(fixture, phase, result)
                intent = fixture.root / 'source-retirements' / ('intent-' + result['manifest_sha256'] + '.json')
                if phase == 'manifest':
                    self.assertFalse(intent.exists())
                    self.assertTrue(all((fixture.root / x['path']).exists() for x in manifest['files']))
                else:
                    self.assertTrue(intent.exists())
                    with self.assertRaisesRegex(fixture.host.Failure, 'artifact_retired'):
                        retirement.assert_available(fixture.host, owner)
                complete = fixture.apply(result)
                self.assertEqual(complete['status'], 'RETIRED')
                self.assertTrue(all(not (fixture.root / x['path']).exists() for x in manifest['files']))
                self.assert_same_complete(fixture.apply(result), complete)

    def test_directory_replacement_after_partial_unlink_blocks_recovery(self):
        directory = self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        self._kill_at_barrier(self.fixture, 'unlink', result)
        parent = directory.parent
        old = parent.with_name(parent.name + '-old')
        parent.rename(old)
        parent.mkdir(mode=0o700)
        replacement = parent / directory.name
        replacement.mkdir(mode=0o700)
        for path in (old / directory.name).iterdir():
            path.rename(replacement / path.name)
        existing = {item['path'] for item in manifest['files'] if (self.root / item['path']).exists()}
        with self.assertRaises((self.h.Failure, OSError)):
            self.fixture.apply(result)
        self.assertTrue(all((self.root / path).exists() for path in existing))

    def test_recovery_survives_unrelated_appended_selection(self):
        self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        self._kill_at_barrier(self.fixture, 'unlink', result)
        selection = dict(self.fixture.selections[104])
        del selection['selection_sha256']
        selection['selection_id'] = ID(106)
        put(self.root / 'source-build-selections' / ('selection-' + ID(106) + '.json'),
            self.h.canonical(selection), 0o400)
        complete = self.fixture.apply(result)
        self.assertEqual(complete['status'], 'RETIRED')
        self.assertTrue(all(not (self.root / item['path']).exists() for item in manifest['files']))

    def test_publish_fchmod_precedes_last_file_fsync(self):
        events = []
        chmod, fsync = os.fchmod, os.fsync
        def chmod_record(fd, mode):
            events.append(('chmod', os.fstat(fd).st_ino, mode))
            return chmod(fd, mode)
        def sync_record(fd):
            info = os.fstat(fd)
            if stat.S_ISREG(info.st_mode):
                events.append(('fsync', info.st_ino, stat.S_IMODE(info.st_mode)))
            return fsync(fd)
        with mock.patch.object(retirement.os, 'fchmod', side_effect=chmod_record), \
             mock.patch.object(retirement.os, 'fsync', side_effect=sync_record):
            result, _ = self.fixture.manifest()
            self.fixture.apply(result)
        records = self.root / 'source-retirements'
        for kind in ('manifest', 'intent', 'complete'):
            path = records / (kind + '-' + result['manifest_sha256'] + '.json')
            ino = path.stat().st_ino
            matching = [event for event in events if event[1] == ino]
            with self.subTest(kind=kind):
                self.assertIn(('chmod', ino, 0o400), matching)
                self.assertEqual(matching[-1], ('fsync', ino, 0o400))
                self.assertLess(matching.index(('chmod', ino, 0o400)), len(matching) - 1)

    def test_unknown_intent_publication_deletes_nothing_and_can_resume(self):
        self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        records = self.root / 'source-retirements'
        intent = records / ('intent-' + result['manifest_sha256'] + '.json')
        real_fsync = os.fsync
        def fail_after_rename(fd):
            if (stat.S_ISDIR(os.fstat(fd).st_mode) and
                    os.readlink('/proc/self/fd/%d' % fd) == str(records) and intent.exists()):
                raise OSError(5, 'injected directory fsync failure')
            return real_fsync(fd)
        with mock.patch.object(retirement.os, 'fsync', side_effect=fail_after_rename):
            with self.assertRaises(OSError):
                self.fixture.apply(result)
        self.assertTrue(intent.exists())
        self.assertTrue(all((self.root / item['path']).exists() for item in manifest['files']))
        complete = self.fixture.apply(result)
        self.assertEqual(complete['status'], 'RETIRED')
        self.assertTrue(all(not (self.root / item['path']).exists() for item in manifest['files']))

    def test_manifest_and_corrupt_records_never_authorize_missing_leaves(self):
        directory = self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        victim = directory / 'app.raw'
        victim.unlink()
        with self.assertRaises((self.h.Failure, OSError)):
            self.fixture.apply(result)
        self.assertFalse((self.root / 'source-retirements' / ('intent-' + result['manifest_sha256'] + '.json')).exists())
        self.assertTrue(any((self.root / item['path']).exists() for item in manifest['files']))
        path = Path(result['manifest_path'])
        path.chmod(0o600)
        path.write_bytes(b'{}\n')
        path.chmod(0o400)
        with self.assertRaises((self.h.Failure, OSError, ValueError)):
            retirement.assert_available(self.h, directory)

    def test_orphan_completion_rejects_reader_and_apply(self):
        directory = self.fixture.vm(10)
        result, manifest = self.fixture.manifest()
        digest = result['manifest_sha256']
        complete = {'version': 1, 'run_token': TOKEN, 'manifest_sha256': digest, 'status': 'RETIRED',
                    'removed_charged_bytes': sum(item['identity']['allocated_bytes'] for item in manifest['files']),
                    'disk_available_before_bytes': 1, 'disk_available_after_bytes': 1}
        put(self.root / 'source-retirements' / ('complete-' + digest + '.json'), self.h.canonical(complete), 0o400)
        with self.assertRaises(self.h.Failure):
            retirement.assert_available(self.h, directory)
        with self.assertRaises(self.h.Failure):
            self.fixture.apply(result)
        self.assertTrue(all((self.root / item['path']).exists() for item in manifest['files']))

    def test_duplicate_manifest_leaf_fails_closed(self):
        directory = self.fixture.vm(10)
        _, manifest = self.fixture.manifest()
        manifest['files'].append(copy.deepcopy(manifest['files'][0]))
        raw = self.h.canonical(manifest)
        digest = sha(raw)
        put(self.root / 'source-retirements' / ('manifest-' + digest + '.json'), raw, 0o400)
        with self.assertRaises(self.h.Failure):
            retirement.assert_available(self.h, directory)
        self.assertTrue((directory / 'app.raw').exists())

    def test_latest_declaring_retired_scenario_never_falls_back(self):
        directory = self.fixture.vm(104, version=2)
        result, _ = self.fixture.manifest()
        self.fixture.apply(result)
        for current in (False, True):
            build = {'_selection': self.fixture.selections[104],
                     'runner_inputs': {'source_resources.py': '0' * 64} if current else {}}
            with self.subTest(current=current):
                outcome = source_verify._case(self.h, self.fixture.frozen, 'realm-negative', 'bpf-broker-kill', build)
                self.assertEqual(outcome['status'], 'FAIL')
                self.assertEqual(outcome['reason'], 'artifact_retired')
                with self.assertRaisesRegex(self.h.Failure, 'artifact_retired'):
                    source_verify._verify_single_vm_dir(self.h, self.fixture.frozen, directory,
                                                        'realm-negative', 'bpf-broker-kill', build)

    def test_real_selected_traverses_retired_historical_build_metadata(self):
        result, manifest = self.fixture.manifest()
        build2 = self.root / 'source-build-attempts' / ('attempt-' + ID(2))
        self.assertIsNotNone(self.owner(manifest, build2))
        self.fixture.apply(result)
        selected = source_build._selected(self.h, self.fixture.frozen)
        self.assertEqual(selected['_selection'], self.fixture.selections[105])
        self.assertTrue((build2 / 'receipt.json').exists())
        with self.assertRaisesRegex(self.h.Failure, 'artifact_retired'):
            retirement.assert_available(self.h, build2)
        previous = dict(self.fixture.selections[2])
        del previous['selection_sha256']
        previous['selection_id'] = ID(106)
        put(self.root / 'source-build-selections' / ('selection-' + ID(106) + '.json'),
            self.h.canonical(previous), 0o400)
        with self.assertRaisesRegex(self.h.Failure, 'artifact_retired'):
            source_build._selected(self.h, self.fixture.frozen)


    def test_latest_retired_declaration_does_not_read_older_pass_claim(self):
        selection = self.fixture.selections[105]
        root = self.root / 'runs' / ('selection-' + ID(105)) / 'realm-negative'
        scenario = 'bpf-broker-kill'
        for index in (1, 2):
            attempt = root / ('attempt-' + ID(index))
            manifest = {'version': 1, 'run_token': TOKEN, 'build_selection': selection, 'attempt': ID(index),
                        'case': 'realm-negative', 'scenarios': [scenario],
                        'members': [{'case': 'realm-negative', 'scenario': scenario, 'variant': None}]}
            put(attempt / 'launch-manifest.json', self.h.canonical(manifest), 0o400)
        old = root / ('attempt-' + ID(1)) / scenario
        put(old / 'receipt.json', receipt_bytes({'status': 'PASS'}))
        latest = root / ('attempt-' + ID(2)) / scenario
        original = retirement.assert_available
        def retired(host, path):
            if path == latest:
                raise host.Failure('artifact_retired')
            return original(host, path)
        build = {'_selection': selection, 'runner_inputs': {}}
        with mock.patch.object(retirement, 'assert_available', side_effect=retired), \
             mock.patch.object(source_verify, '_verify_single_vm_dir', side_effect=AssertionError('older result read')):
            result = source_verify._case(self.h, self.fixture.frozen, 'realm-negative', scenario, build)
        self.assertEqual((result['status'], result['reason']), ('FAIL', 'artifact_retired'))

    def test_real_lsof_detects_direct_and_hardlink_then_closed(self):
        target = self.root / 'lsof-target.raw'
        alias = self.root / 'lsof-alias.raw'
        put(target, b'real synthetic target')
        self.assertEqual(self.fixture_mount['type'], 'btrfs')
        self.assertTrue(self.exempt_mounts)
        for mount in self.exempt_mounts:
            self.assertIn(mount['type'], ('overlay', 'nsfs'))
            self.assertNotEqual(mount['id'], self.fixture_mount['id'])
            self.assertNotEqual(mount['device'], self.fixture_mount['device'])
            self.assertFalse(self.root.is_relative_to(mount['path']))
            self.assertFalse(Path(mount['path']).is_relative_to(self.root))
        for name in ('direct', 'hardlink'):
            with self.subTest(name=name):
                if name == 'hardlink':
                    os.link(target, alias)
                opened = target if name == 'direct' else alias
                with self.held_file(opened) as (child, fd):
                    self.assertEqual(os.readlink('/proc/%d/ns/mnt' % child), os.readlink('/proc/self/ns/mnt'))
                    held = os.stat('/proc/%d/fd/%d' % (child, fd))
                    expected = target.stat()
                    self.assertEqual((held.st_dev, held.st_ino), (expected.st_dev, expected.st_ino))
                    info = Path('/proc/%d/fdinfo/%d' % (child, fd)).read_text().splitlines()
                    mount_ids = [int(line.split(':', 1)[1].strip()) for line in info if line.startswith('mnt_id:')]
                    self.assertEqual(mount_ids, [self.fixture_mount['id']])
                    result = self.real_lsof(target)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertFalse(result.stderr, 'unexpected lsof stderr: %r' % result.stderr)
                    self.assertIn(('p%d\n' % child).encode(), result.stdout)
                    self.assertIn(b'\nn' + os.fsencode(target) + b'\n', result.stdout)
                closed = self.real_lsof(target)
                self.assertFalse(closed.stderr, 'unexpected lsof stderr: %r' % closed.stderr)
                self.assertEqual((closed.returncode, closed.stdout), (1, b''))
                if name == 'hardlink':
                    alias.unlink()

    def test_unexempted_real_lsof_warning_fails_closed(self):
        directory = self.fixture.vm(10)
        target = directory / 'app.raw'
        self.use_mount_exemptions = False
        with self.held_file(target) as (child, fd):
            held, expected = os.stat('/proc/%d/fd/%d' % (child, fd)), target.stat()
            self.assertEqual((held.st_dev, held.st_ino), (expected.st_dev, expected.st_ino))
            self.assertEqual(os.readlink('/proc/%d/ns/mnt' % child), os.readlink('/proc/self/ns/mnt'))
            diagnostic = self.real_lsof(target)
            self.assertEqual(diagnostic.returncode, 0)
            self.assertTrue(diagnostic.stderr, 'expected actual unexempted lsof diagnostic')
            self.assertIn(('p%d\n' % child).encode(), diagnostic.stdout)
            result = retirement.inventory(self.h, self.fixture.frozen)
            self.assertEqual(result['status'], 'EMPTY')
            self.assertEqual(result['files'], [])
            self.assertIsNone(result['manifest_sha256'])
            exclusions = self.excluded(result, directory)
            self.assertTrue(exclusions)
            self.assertIn('activity_inspection_failed', exclusions[0]['reason'])
            self.assertTrue(target.exists())
            self.assertFalse((self.root / 'source-retirements').exists())

    def test_open_external_carrier_excludes_its_owner(self):
        directory = self.fixture.vm(10)
        other = self.fixture.vm(11)
        ready_r, ready_w = os.pipe()
        gate_r, gate_w = os.pipe()
        child = os.fork()
        if child == 0:
            os.close(ready_r)
            os.close(gate_w)
            fd = os.open(directory / 'app.raw', os.O_RDONLY)
            os.write(ready_w, b'R')
            os.read(gate_r, 1)
            os.close(fd)
            os._exit(0)
        os.close(ready_w)
        os.close(gate_r)
        try:
            self.assertEqual(os.read(ready_r, 1), b'R')
            _, manifest = self.fixture.manifest()
            self.assertIsNone(self.owner(manifest, directory))
            self.assertIsNotNone(self.owner(manifest, other))
            self.assertTrue(self.excluded(manifest, directory))
            self.assertTrue((directory / 'app.raw').exists())
        finally:
            os.write(gate_w, b'X')
            os.close(ready_r)
            os.close(gate_w)
            os.waitpid(child, 0)

    def test_unreadable_process_is_disclosed_without_global_veto(self):
        directory = self.fixture.vm(10)
        unrelated = self.root / 'unrelated'
        put(unrelated, b'unrelated')
        with self.held_file(unrelated) as (child, _):
            readlink = os.readlink
            def unreadable(path, *args, **kwargs):
                if str(path) == '/proc/%d/ns/mnt' % child:
                    raise PermissionError(13, 'injected unreadable namespace', str(path))
                return readlink(path, *args, **kwargs)
            with mock.patch.object(retirement.os, 'readlink', side_effect=unreadable):
                result, manifest = self.fixture.manifest()
                complete = self.fixture.apply(result)
            self.assertIsNotNone(self.owner(manifest, directory))
            for disclosure in (manifest['activity_disclosure'], complete['activity_disclosure']):
                entry = next(item for item in disclosure['undetermined'] if item['pid'] == child)
                self.assertIn('ns/mnt', entry['fields'])
                self.assertIn('permission_denied', entry['reason'])

    def test_foreign_namespace_still_matches_fd_identity(self):
        held = self.fixture.vm(10)
        other = self.fixture.vm(11)
        with self.held_file(held / 'app.raw') as (child, fd):
            readlink = os.readlink
            def foreign(path, *args, **kwargs):
                if str(path) == '/proc/%d/ns/mnt' % child:
                    return 'mnt:[foreign]'
                if str(path) == '/proc/%d/fd/%d' % (child, fd):
                    return '/unrelated/alias'
                return readlink(path, *args, **kwargs)
            empty = SimpleNamespace(returncode=1, stdout=b'', stderr=b'')
            with mock.patch.object(retirement.os, 'readlink', side_effect=foreign), \
                 mock.patch.object(retirement.subprocess, 'run', return_value=empty):
                _, manifest = self.fixture.manifest()
            self.assertIsNone(self.owner(manifest, held))
            self.assertIsNotNone(self.owner(manifest, other))
            entry = next(item for item in manifest['activity_disclosure']['undetermined'] if item['pid'] == child)
            self.assertEqual(entry['reason'], 'foreign_mount_namespace')
            self.assertEqual(entry['fields'], ['ns/mnt'])

    def test_visible_cwd_in_foreign_or_unreadable_namespace_blocks_inventory_and_apply(self):
        held = self.fixture.vm(10)
        other = self.fixture.vm(11)
        initial, manifest = self.fixture.manifest()
        self.assertIsNotNone(self.owner(manifest, held))
        with self.working_directory_holder(held) as child:
            readlink = os.readlink
            self.assertEqual(readlink('/proc/%d/cwd' % child), str(held))
            for descriptor in Path('/proc/%d/fd' % child).iterdir():
                self.assertNotIn(str(held), readlink(descriptor))
            empty = SimpleNamespace(returncode=1, stdout=b'', stderr=b'')
            for reason in ('foreign_mount_namespace', 'permission_denied'):
                with self.subTest(reason=reason):
                    def namespace(path, *args, **kwargs):
                        if str(path) == '/proc/%d/ns/mnt' % child:
                            if reason == 'permission_denied':
                                raise PermissionError(13, 'injected unreadable namespace', str(path))
                            return 'mnt:[foreign]'
                        return readlink(path, *args, **kwargs)
                    with mock.patch.object(retirement.os, 'readlink', side_effect=namespace), \
                         mock.patch.object(retirement.subprocess, 'run', return_value=empty):
                        _, observed = self.fixture.manifest()
                        self.assertIsNone(self.owner(observed, held))
                        self.assertIsNotNone(self.owner(observed, other))
                        entry = next(item for item in observed['activity_disclosure']['undetermined']
                                     if item['pid'] == child)
                        self.assertEqual(entry['reason'], reason)
                        self.assertEqual(entry['fields'], ['ns/mnt'])
                        with self.assertRaisesRegex(self.h.Failure, 'retirement artifacts are active'):
                            self.fixture.apply(initial)
                    self.assertTrue((held / 'app.raw').exists())
                    self.assertTrue((other / 'app.raw').exists())
                    self.assertFalse((self.root / 'source-retirements' /
                                      ('intent-' + initial['manifest_sha256'] + '.json')).exists())

    def test_lsof_exemption_device_intersection_fails_closed(self):
        directory = self.fixture.vm(10)
        read_text = Path.read_text
        def mountinfo(path, *args, **kwargs):
            result = read_text(path, *args, **kwargs)
            if str(path) == '/proc/self/mountinfo':
                result += ('999999 1 %s / /var/lib/docker/overlay2/test/merged rw - overlay overlay rw\n' %
                           self.fixture_mount['device'])
            return result
        with mock.patch.object(Path, 'read_text', mountinfo):
            result = retirement.inventory(self.h, self.fixture.frozen)
        self.assertEqual(result['status'], 'EMPTY')
        self.assertIn('activity_inspection_failed', self.excluded(result, directory)[0]['reason'])
        self.assertTrue((directory / 'app.raw').exists())

    def test_same_device_different_mount_identity_is_rejected(self):
        directory = self.fixture.vm(10)
        real_mount_id = retirement._mount_id
        def cross_mount(fd):
            actual = real_mount_id(fd)
            if os.readlink('/proc/self/fd/%d' % fd) == str(directory):
                return actual + 1
            return actual
        with mock.patch.object(retirement, '_mount_id', side_effect=cross_mount):
            result = retirement.inventory(self.h, self.fixture.frozen)
        self.assertEqual(result['status'], 'EMPTY')
        self.assertIsNone(result['manifest_path'])
        self.assertEqual(result['files'], [])
        self.assertTrue(self.excluded(result, directory))
        self.assertTrue((directory / 'app.raw').exists())

    def test_replaced_owner_receipt_and_launch_digest_are_not_accepted(self):
        directory = self.fixture.vm(10)
        path = directory / 'receipt.json'
        record = self.h.strict_json(path.read_bytes())
        record['host_launch_sha256'] = '0' * 64
        put(path, receipt_bytes(record))
        _, manifest = self.fixture.manifest()
        self.assertIsNone(self.owner(manifest, directory))
        self.assertTrue(self.excluded(manifest, directory))

    def test_negative_loader_with_no_runs_is_not_ready(self):
        build = {'_selection': self.fixture.selections[105]}
        with self.assertRaisesRegex(source_bpf_negative.NotReady, 'no runs for the selected build'):
            source_bpf_negative._load_inputs(self.h, self.fixture.frozen, build)

    def test_fabricated_family_retirement_record_fails_closed(self):
        directory = self.fixture.vm(104)
        _, manifest = self.fixture.manifest()
        forged = copy.deepcopy(manifest)
        owner = self.owner(forged, directory)
        owner['relative_directory'] = str((directory.parent / 'bpf-authorization').relative_to(self.root))
        raw = self.h.canonical(forged)
        digest = sha(raw)
        records = self.root / 'source-retirements'
        put(records / ('manifest-' + digest + '.json'), raw, 0o400)
        intent = {'version': 1, 'run_token': TOKEN, 'manifest_sha256': digest, 'status': 'RETIRING'}
        put(records / ('intent-' + digest + '.json'), self.h.canonical(intent), 0o400)
        with self.assertRaises(self.h.Failure):
            retirement.assert_available(self.h, directory.parent / 'bpf-authorization')

    def test_family_guard_precedes_missing_family_and_negative_loader_is_not_ready(self):
        selection = self.fixture.selections[105]
        root = self.root / 'runs' / ('selection-' + ID(105)) / 'realm-negative' / ('attempt-' + ID(1))
        family = root / 'bpf-authorization'
        private_dir(family)
        scenario = 'bpf-authorization'
        members = [{'case': case, 'scenario': member, 'variant': variant}
                   for case, member, variant in source_vm.BPF_AUTH_FAMILY_MEMBERS]
        manifest = {'version': 1, 'run_token': TOKEN, 'build_selection': selection, 'attempt': ID(1),
                    'case': 'realm-negative', 'scenarios': [scenario], 'members': members}
        put(root / 'launch-manifest.json', self.h.canonical(manifest), 0o400)
        build = {'_selection': selection, 'runner_inputs': {'source_resources.py': '0' * 64}}
        original = retirement.assert_available
        def retired_family(host, path):
            if path == family:
                raise host.Failure('artifact_retired')
            return original(host, path)
        with mock.patch.object(retirement, 'assert_available', side_effect=retired_family):
            result = source_verify._case(self.h, self.fixture.frozen, 'realm-negative', scenario, build)
            self.assertEqual((result['status'], result['reason']), ('FAIL', 'artifact_retired'))
            with self.assertRaises(source_bpf_negative.NotReady):
                source_bpf_negative._load_inputs(self.h, self.fixture.frozen, build)

    def test_legacy_and_current_missing_receipt_cannot_bypass_retirement_guard(self):
        directory = self.fixture.vm(104)
        original_available = retirement.assert_available
        original_exists = os.path.lexists
        def retired(host, path):
            if path == directory:
                raise host.Failure('artifact_retired')
            return original_available(host, path)
        def hidden_receipt(path):
            if Path(path) == directory / 'receipt.json':
                return False
            return original_exists(path)
        with mock.patch.object(retirement, 'assert_available', side_effect=retired), \
             mock.patch.object(source_verify.os.path, 'lexists', side_effect=hidden_receipt):
            for current in (False, True):
                build = {'_selection': self.fixture.selections[104],
                         'runner_inputs': {'source_resources.py': '0' * 64} if current else {}}
                result = source_verify._case(self.h, self.fixture.frozen, 'realm-negative', 'bpf-broker-kill', build)
                self.assertEqual((result['status'], result['reason']), ('FAIL', 'artifact_retired'))


if __name__ == '__main__':
    unittest.main()
