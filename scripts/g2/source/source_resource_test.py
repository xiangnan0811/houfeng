"""Resource contract regression tests; all fixtures are private and require no VM."""
import contextlib
import copy
import errno
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import stat
import signal
import socket
from types import SimpleNamespace
import subprocess
import unittest
from unittest import mock

import source_build
import source_host as host
import source_resources as resources
import source_vm_failure_fixture
import source_verify
import source_vm

GiB = 1024 ** 3
Q = 256 * 1024 ** 2
G = 4 * GiB
R = GiB
F = 1024 ** 2


def budget_for_payload(p):
    capacity = max(2 * GiB, (((5 * p + 3) // 4 + Q + Q - 1) // Q) * Q)
    staging = ((p + capacity + 528482304 + Q + Q - 1) // Q) * Q
    return {'version': 1, 'policy': 'houfeng.source-vm.operational.v2', 'seed_inputs_sha256': '0' * 64,
            'payload_bytes': p, 'seed_capacity_bytes': capacity, 'cidata_capacity_bytes': 528482304,
            'writable_virtual_bytes': 48 * GiB, 'staging_limit_bytes': staging,
            'growth_limit_bytes': G, 'reserve_bytes': R, 'final_receipt_allowance_bytes': F}


class ResourceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.build_dir = self.root / 'build'
        self.build_dir.mkdir(mode=0o700)
        self.seed = self.build_dir / 'seed'
        self.seed.mkdir(mode=0o700)
        self.case = self.root / 'case'
        self.case.mkdir(mode=0o700)
        for name, data in {'user-data': b'user', 'network-config': b'network', 'input': b'payload'}.items():
            (self.seed / name).write_bytes(data)
        self.build = {'_attempt_dir': str(self.build_dir),
                      'seed_inputs': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in self.seed.iterdir()},
                      'qemu_binaries': {'qemu-system-x86_64': 'a' * 64}, 'boot_inputs': {},
                      'bpf_authority': {'bootstrap_manifest': {}}}

    def guard(self, budget=None, free=20 * GiB, allocated=1000):
        guard = resources.SourceResourceGuard(host, self.case, budget or budget_for_payload(GiB))
        samples = {'free': free, 'allocated': allocated}
        self.enterContext(mock.patch.object(guard, '_host_sample', side_effect=lambda gate:
                                           (samples['free'], 4 * GiB if gate else None, 12 if gate else None)))
        self.enterContext(mock.patch.object(resources, 'allocated_tree_bytes', side_effect=lambda h, p: samples['allocated']))
        return guard, samples

    def test_budget_real_seed_and_drift(self):
        data = source_vm._case_bytes(host, self.build, 'source-vps', 'normal')
        budget = resources.make_budget(host, self.build, data)
        expected_p = len(b'payload') + len(data) + (64 + 2 + len('input') + 1) + (64 + 2 + len('case.json') + 1)
        self.assertEqual(budget['payload_bytes'], expected_p)
        self.assertEqual(budget['seed_capacity_bytes'], 2 * GiB)
        (self.seed / 'input').write_bytes(b'changed')
        with self.assertRaises(host.Failure):
            resources.make_budget(host, self.build, data)
        (self.seed / 'new').write_bytes(b'x')
        with self.assertRaises(host.Failure):
            resources.make_budget(host, self.build, data)

    def test_exact_gib_payload_uses_sparse_input(self):
        case_bytes = source_vm._case_bytes(host, self.build, 'source-vps', 'normal')
        manifest_bytes = (64 + 2 + len('input') + 1) + (64 + 2 + len('case.json') + 1)
        path = self.seed / 'input'
        with path.open('wb') as stream:
            stream.truncate(GiB - len(case_bytes) - manifest_bytes)
        self.build['seed_inputs']['input'] = host.digest(path)[0]
        budget = resources.make_budget(host, self.build, case_bytes)
        self.assertEqual(budget['payload_bytes'], GiB)
        self.assertEqual(budget['seed_capacity_bytes'], 2 * GiB)
        self.assertEqual(budget['staging_limit_bytes'], 15 * GiB // 4)

    def test_prepare_and_launch_boundaries(self):
        budget = budget_for_payload(GiB)
        self.assertEqual(budget['seed_capacity_bytes'], 2 * GiB)
        self.assertEqual(budget['staging_limit_bytes'], 15 * GiB // 4)
        required = 35 * GiB // 4
        guard, samples = self.guard(budget, free=required)
        self.assertEqual(guard.check_prepare()['required_disk_bytes'], required)
        samples['free'] = 5 * GiB
        self.assertEqual(guard.begin_launch()['required_disk_bytes'], 5 * GiB)
        for available in (required - 1, 5 * GiB):
            other, _ = self.guard(budget, free=available)
            with self.assertRaises(resources.SourceResourceError) as err:
                other.check_prepare()
            self.assertEqual((err.exception.reason, err.exception.phase), ('disk_admission', 'prepare'))
        other, state = self.guard(budget)
        other.check_prepare()
        state['free'] = 5 * GiB - 1
        with self.assertRaises(resources.SourceResourceError):
            other.begin_launch()
        other, _ = self.guard(budget, free=20 * GiB)
        other.check_prepare()
        other.begin_launch()

    def test_invalid_budget_and_staging(self):
        original = budget_for_payload(GiB)
        for key, value in [('version', True), ('reserve_bytes', -1), ('policy', 'unknown'),
                           ('growth_limit_bytes', True), ('staging_limit_bytes', original['staging_limit_bytes'] + 1)]:
            candidate = dict(original, **{key: value})
            with self.subTest(key=key), self.assertRaises(host.Failure):
                resources.SourceResourceGuard(host, self.case, candidate)
        for candidate in ({**original, 'extra': 1}, {k: v for k, v in original.items() if k != 'version'},
                          budget_for_payload(8 * GiB)):
            with self.assertRaises(host.Failure):
                resources.SourceResourceGuard(host, self.case, candidate)
        guard, state = self.guard()
        guard.check_prepare()
        state['allocated'] += original['staging_limit_bytes'] + 1
        with self.assertRaises(resources.SourceResourceError) as err:
            guard.check_prepare()
        self.assertEqual(err.exception.reason, 'staging_limit')
        with self.assertRaises(host.Failure):
            resources.SourceResourceGuard(host, self.case, original).after_wait()

    def test_reboot_preserves_peak_and_cut(self):
        guard, state = self.guard()
        guard.check_prepare()
        guard.begin_launch()
        state['allocated'] += 3 * GiB
        guard.check_live(force=True)
        first = guard.after_wait()
        state['allocated'] += 4096
        gate = guard.begin_launch(reboot=True)
        self.assertEqual(first['peak_growth_bytes'], 3 * GiB)
        self.assertEqual(gate['observed_growth_bytes'], 3 * GiB + 4096)
        self.assertEqual(gate['required_disk_bytes'], 2 * GiB - 4096)
        guard.after_wait(reboot=True)
        guard.finish_member()
        self.assertEqual(guard.snapshot()['baseline_allocated_bytes'], first['baseline_allocated_bytes'])

    def test_reboot_without_intervening_allocation(self):
        guard, state = self.guard()
        guard.check_prepare()
        guard.begin_launch()
        state['allocated'] += 3 * GiB
        guard.after_wait()
        gate = guard.begin_launch(reboot=True)
        self.assertEqual(gate['observed_growth_bytes'], 3 * GiB)
        self.assertEqual(gate['required_disk_bytes'], 2 * GiB)

    def test_live_growth_exact_boundary(self):
        guard, state = self.guard()
        guard.check_prepare()
        guard.begin_launch()
        state['allocated'] += G
        guard.check_live(force=True)
        self.assertEqual(guard.snapshot()['peak_growth_bytes'], G)
        state['allocated'] += 1
        with self.assertRaises(resources.SourceResourceError) as err:
            guard.check_live(force=True)
        self.assertEqual((err.exception.reason, err.exception.phase), ('growth_limit', 'running'))

    def test_live_latches_first_failure(self):
        for reason in ('growth_limit', 'disk_reserve'):
            guard, state = self.guard()
            guard.check_prepare()
            guard.begin_launch()
            if reason == 'growth_limit':
                state['allocated'] += G + 1
            else:
                state['free'] = R - 1
            with self.assertRaises(resources.SourceResourceError) as first:
                guard.check_live(force=True)
            state['free'] = 0
            with self.assertRaises(resources.SourceResourceError) as second:
                guard.check_live(force=True)
            self.assertEqual(first.exception.reason, reason)
            self.assertEqual(second.exception.reason, reason)
            self.assertEqual(guard.snapshot()['abort'], {'reason': reason, 'phase': 'running'})

    def test_final_receipt_headroom_boundaries(self):
        for amount, free, success in ((G - F, R + F, True), (G - F + 1, 20 * GiB, False),
                                      (G - F, R + F - 1, False)):
            with self.subTest(amount=amount, free=free):
                guard, state = self.guard()
                guard.check_prepare()
                guard.begin_launch()
                guard.after_wait()
                state['allocated'] += amount
                state['free'] = free
                if success:
                    observed = guard.finish_member()
                    self.assertEqual(observed['required_disk_bytes'], R + F)
                    self.assertEqual(guard.snapshot()['peak_growth_bytes'], G - F)
                else:
                    with self.assertRaises(resources.SourceResourceError):
                        guard.finish_member()

    def test_real_allocation_and_unsafe_entries(self):
        nested = self.case / 'nested'
        nested.mkdir(mode=0o700)
        one, two = self.case / 'one', nested / 'two'
        one.write_bytes(b'a' * 8192)
        two.write_bytes(b'b' * 4096)
        self.assertEqual(resources.allocated_tree_bytes(host, self.case),
                         (one.stat().st_blocks + two.stat().st_blocks) * 512)
        link = self.case / 'link'
        link.symlink_to(one)
        with self.assertRaises(host.Failure):
            resources.allocated_tree_bytes(host, self.case)
        link.unlink()
        os.link(one, link)
        with self.assertRaises(host.Failure):
            resources.allocated_tree_bytes(host, self.case)

    def test_atomic_receipt_failure_and_conflict(self):
        writer = source_vm._CaseReceipt(host, self.case)
        self.addCleanup(writer.close)
        writer.save({'status': 'IN_PROGRESS'})
        path = self.case / 'receipt.json'
        old = path.read_bytes()
        with mock.patch.object(source_vm.os, 'write', side_effect=OSError(errno.ENOSPC, 'full')):
            with self.assertRaises(OSError):
                writer.save({'status': 'FAIL'})
        self.assertEqual(path.read_bytes(), old)
        self.assertFalse((self.case / 'receipt-next.json').exists())
        with self.assertRaises(host.Failure):
            writer.save({'status': 'FAIL'})
        next_dir = self.root / 'conflict'
        next_dir.mkdir(mode=0o700)
        conflict = source_vm._CaseReceipt(host, next_dir)
        self.addCleanup(conflict.close)
        (next_dir / 'receipt-next.json').write_bytes(b'owned by another writer')
        with self.assertRaises(FileExistsError):
            conflict.save({'status': 'PASS'})
        self.assertEqual((next_dir / 'receipt-next.json').read_bytes(), b'owned by another writer')

    def test_post_rename_failure_preserves_complete_receipt(self):
        writer = source_vm._CaseReceipt(host, self.case)
        self.addCleanup(writer.close)
        writer.save({'status': 'IN_PROGRESS'})
        real_fsync = os.fsync
        def fail_directory(fd):
            if fd == writer.fd:
                raise OSError(errno.EIO, 'directory fsync failed')
            return real_fsync(fd)
        with mock.patch.object(source_vm.os, 'fsync', side_effect=fail_directory):
            with self.assertRaises(OSError):
                writer.save({'status': 'PASS'})
        self.assertEqual(host.strict_json((self.case / 'receipt.json').read_bytes())['status'], 'PASS')
        with self.assertRaises(host.Failure):
            writer.save({'status': 'FAIL'})

    def _contract_fixture(self):
        payload = self.case / 'payload'
        payload.mkdir(mode=0o700)
        case_value = {'version': 1, 'run_token': host.TOKEN, 'case': 'source-recovery', 'scenario': 'vm-kill',
                      'bpf_variant': None, 'qemu_sha256': 'a' * 64, 'boot_inputs': {}, 'bpf_bootstrap': {}}
        case_bytes = host.canonical(case_value)
        (payload / 'case.json').write_bytes(case_bytes)
        (payload / 'input').write_bytes((self.seed / 'input').read_bytes())
        names = ['case.json', 'input']
        sums = ''.join(hashlib.sha256((payload / name).read_bytes()).hexdigest() + '  ' + name + '\n'
                       for name in names).encode('ascii')
        (payload / 'SHA256SUMS').write_bytes(sums)
        p = len(case_bytes) + len(b'payload') + len(sums)
        budget = budget_for_payload(p)
        budget['seed_inputs_sha256'] = hashlib.sha256(host.canonical(self.build['seed_inputs'])).hexdigest()
        def observation(phase, required, growth, gate=True):
            return {'phase': phase, 'disk_available_bytes': 20 * GiB, 'mem_available_bytes': 4 * GiB if gate else None,
                    'kvm_api_version': 12 if gate else None, 'required_disk_bytes': required,
                    'observed_growth_bytes': growth}
        def usage(peak, count):
            return {'version': 1, 'baseline_allocated_bytes': 1000, 'peak_allocated_bytes': 1000 + peak,
                    'final_allocated_bytes': 1000 + peak, 'minimum_disk_available_bytes': 20 * GiB,
                    'sample_count': count, 'peak_growth_bytes': peak, 'abort': None}
        cut = 3 * GiB + 4096
        state = {'seed_image': {'size': budget['seed_capacity_bytes']}, 'resource_budget': budget,
                 'resources': observation('prepare', budget['staging_limit_bytes'] + G + R, 0),
                 'launch_resources': observation('launch', G + R, 0),
                 'initial_resource_usage': usage(3 * GiB, 4),
                 'reboot_resources': observation('reboot', G - cut + R, cut),
                 'resource_usage': usage(cut + 8192, 8),
                 'final_resources': observation('finalize', R + F, cut + 8192, False)}
        return state, p, cut

    def test_independent_v2_contract_and_reboot_cut(self):
        state, p, cut = self._contract_fixture()
        def verify(value):
            source_verify._resource_contract(host, self.case, value, self.build, 'source-recovery', 'vm-kill', None)
        verify(state)
        for policy, growth in (('houfeng.source-vm.operational.v1', 12 * GiB),
                               ('houfeng.source-vm.operational.v2', 12 * GiB),
                               ('houfeng.source-vm.operational.v1', G)):
            candidate = copy.deepcopy(state)
            candidate['resource_budget'].update(policy=policy, growth_limit_bytes=growth)
            with self.subTest(policy=policy, growth=growth), self.assertRaises(host.Failure):
                verify(candidate)
        for section, key, bad in [('resource_budget', 'reserve_bytes', R + 1),
                                  ('resource_budget', 'growth_limit_bytes', G + 1),
                                  ('resource_budget', 'final_receipt_allowance_bytes', F + 1),
                                  ('resource_budget', 'payload_bytes', p + 1),
                                  ('resources', 'phase', 'launch'),
                                  ('resources', 'mem_available_bytes', 4 * GiB - 1),
                                  ('launch_resources', 'kvm_api_version', True),
                                  ('reboot_resources', 'required_disk_bytes', G - cut + R + 4096),
                                  ('reboot_resources', 'observed_growth_bytes', 3 * GiB - 1),
                                  ('initial_resource_usage', 'sample_count', True),
                                  ('resource_usage', 'peak_growth_bytes', cut),
                                  ('final_resources', 'mem_available_bytes', 4 * GiB)]:
            candidate = copy.deepcopy(state)
            candidate[section][key] = bad
            with self.subTest(section=section, key=key), self.assertRaises(host.Failure):
                verify(candidate)
        candidate = copy.deepcopy(state)
        candidate['resource_budget']['extra'] = 1
        with self.assertRaises(host.Failure):
            verify(candidate)
        candidate = copy.deepcopy(state)
        candidate['resource_usage']['baseline_allocated_bytes'] += 4096
        candidate['resource_usage']['peak_allocated_bytes'] += 4096
        candidate['resource_usage']['final_allocated_bytes'] += 4096
        with self.assertRaises(host.Failure):
            verify(candidate)


    def test_publication_unknown_durable_resource_validation(self):
        state, _, _ = self._contract_fixture()
        payload = self.case / 'payload'
        state['seed'] = {path.name: host.digest(path)[0] for path in payload.iterdir()}
        state['status'] = 'PASS'
        for path in payload.iterdir():
            path.chmod(0o400)
        payload.chmod(0o500)
        self.addCleanup(payload.chmod, 0o700)
        writer = source_vm._CaseReceipt(host, self.case)
        self.addCleanup(writer.close)
        writer.save({'status': 'IN_PROGRESS'})
        real_fsync = os.fsync
        def fail_directory(fd):
            if stat.S_ISDIR(os.fstat(fd).st_mode):
                raise OSError(errno.EIO, 'directory fsync failed')
            return real_fsync(fd)
        with mock.patch.object(source_vm.os, 'fsync', side_effect=fail_directory):
            with self.assertRaises(OSError):
                writer.save(state)
        receipt = self.case / 'receipt.json'
        before = os.stat(receipt, follow_symlinks=False)
        digest = host.digest(receipt)[0]
        def validate(value):
            host.require(value['status'] == 'PASS', 'receipt is not PASS')
            source_vm._verify_payload(host, payload, value['seed'])
            source_verify._resource_contract(host, self.case, value, self.build,
                                             'source-recovery', 'vm-kill', None)
            return value['status']
        self.assertEqual(source_verify._verify_durable_receipt(host, self.case, validate), 'PASS')
        after = os.stat(receipt, follow_symlinks=False)
        self.assertEqual((before.st_dev, before.st_ino, digest),
                         (after.st_dev, after.st_ino, host.digest(receipt)[0]))
        with mock.patch.object(source_verify.os, 'fsync', side_effect=fail_directory):
            with self.assertRaises(OSError):
                source_verify._verify_durable_receipt(host, self.case, validate)
        target = payload / 'input'
        original = target.read_bytes()
        target.chmod(0o600)
        target.write_bytes(b'tampered')
        target.chmod(0o400)
        with self.assertRaises(host.Failure):
            source_verify._verify_durable_receipt(host, self.case, validate)
        target.chmod(0o600)
        target.write_bytes(original)
        target.chmod(0o400)
        def replace_after_validation(value):
            validate(value)
            replacement = self.case / 'replacement.json'
            replacement.write_bytes(receipt.read_bytes())
            replacement.chmod(0o600)
            os.replace(replacement, receipt)
        with self.assertRaises(host.Failure):
            source_verify._verify_durable_receipt(host, self.case, replace_after_validation)

    def test_storage_child_failure_preserves_diagnostic(self):
        executable = str(Path('/usr/bin/python3').resolve())
        program = "import os; os.write(2, b'No space left on device\\n'); raise SystemExit(73)"
        with self.assertRaises(source_build.StorageToolFailure) as error:
            source_build._run(host, [executable, '-c', program], self.root, os.environ.copy(),
                              'storage-child', self.root, storage_step=True)
        self.assertEqual(error.exception.label, 'storage-child')
        self.assertEqual(error.exception.returncode, 73)
        self.assertEqual(error.exception.stderr.read_bytes(), b'No space left on device\n')
        self.assertIsNone(error.exception.__cause__)

    def test_storage_nonzero_oversized_output_retains_typed_failure_and_cause(self):
        executable = str(Path('/usr/bin/python3').resolve())
        for stream in (1, 2):
            with self.subTest(stream=stream):
                label = 'oversized-storage-%d' % stream
                program = 'import os; os.write(%d, b"x" * 4194305); raise SystemExit(73)' % stream
                with self.assertRaises(source_build.StorageToolFailure) as error:
                    source_build._run(host, [executable, '-c', program], self.root, os.environ.copy(),
                                      label, self.root, storage_step=True)
                failure = error.exception
                self.assertEqual((failure.label, failure.returncode), (label, 73))
                self.assertIsInstance(failure.__cause__, host.Failure)
                self.assertEqual(failure.stdout, self.root / (label + '.stdout'))
                self.assertEqual(failure.stderr, self.root / (label + '.stderr'))
                oversized = failure.stdout if stream == 1 else failure.stderr
                other = failure.stderr if stream == 1 else failure.stdout
                self.assertEqual(oversized.read_bytes(), b'x' * 4194305)
                self.assertEqual(other.read_bytes(), b'')

    def test_storage_and_nonstorage_output_limit_remains_enforced(self):
        executable = str(Path('/usr/bin/python3').resolve())
        for storage_step in (False, True):
            for stream in (1, 2):
                for size, code in ((4194304, 0), (4194305, 0), (4194305, 73)):
                    if storage_step and code != 0:
                        continue  # Covered by the typed-failure test above.
                    with self.subTest(storage_step=storage_step, stream=stream, size=size, code=code):
                        label = 'limit-%d-%d-%d-%d' % (storage_step, stream, size, code)
                        program = 'import os; os.write(%d, b"x" * %d); raise SystemExit(%d)' % (stream, size, code)
                        args = [executable, '-c', program]
                        if size == 4194304:
                            source_build._run(host, args, self.root, os.environ.copy(), label, self.root,
                                              storage_step=storage_step)
                        else:
                            with self.assertRaises(host.Failure):
                                source_build._run(host, args, self.root, os.environ.copy(), label, self.root,
                                                  storage_step=storage_step)
                        output = self.root / (label + ('.stdout' if stream == 1 else '.stderr'))
                        self.assertEqual(output.stat().st_size, size)

    def _assert_family_real_storage_failure(self, oversized):
        qemu_dir = self.build_dir / 'qemu'
        qemu_dir.mkdir(mode=0o700)
        tool = qemu_dir / 'qemu-img'
        diagnostic = b'x' * 4194305 if oversized else b'No space left on device\n'
        program = ('os.write(2, b"x" * 4194305)' if oversized else
                   "os.write(2, b'No space left on device\\n')")
        tool.write_text('#!/usr/bin/python3\nimport os\n' + program + '\nraise SystemExit(73)\n')
        tool.chmod(0o700)
        base = self.root / 'base'
        base.write_bytes(b'immutable base')
        base_sha = host.digest(base)[0]
        frozen = SimpleNamespace(IMAGE='fixture', UBUNTU_HASHES={'fixture': base_sha},
                                 load_json=lambda path: {'assets': {'ubuntu:fixture': str(base)}})
        self.build.update(_selection={'selection_id': '1'}, browser_selection={})
        family_root = self.root / 'family-run'
        family_root.mkdir(mode=0o700)
        def boot_fds(_host, _build):
            return {'kernel_fd': os.open('/dev/null', os.O_RDONLY),
                    'initrd_fd': os.open('/dev/null', os.O_RDONLY)}
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(resources.SourceResourceGuard, '_host_sample',
                               side_effect=lambda gate: (20 * GiB, 4 * GiB if gate else None, 12 if gate else None)), \
             mock.patch.object(source_vm, '_open_boot_fds', side_effect=boot_fds):
            ok = source_vm._case(host, frozen, os.environ.copy(), family_root, 'realm-negative',
                                 'bpf-authorization', self.build, manifest_sha256='a' * 64)
        self.assertFalse(ok)
        family = host.strict_json((family_root / 'bpf-authorization' / 'family.json').read_bytes())
        self.assertEqual(family['stop_reason'], 'storage_tool_failure')
        self.assertEqual(len(family['members']), 25)
        self.assertEqual(family['members'][0]['status'], 'FAIL')
        self.assertTrue(all(item['status'] == 'NOT_RUN' and item['receipt_sha256'] is None and
                            item['retained_allocated_bytes'] == 0 and
                            not (family_root / item['directory']).exists() for item in family['members'][1:]))
        member_dir = family_root / family['members'][0]['directory']
        record = host.strict_json((member_dir / 'receipt.json').read_bytes())
        self.assertEqual(record['error_type'], 'StorageToolFailure')
        self.assertEqual(record['failure_kind'], 'source')
        self.assertEqual(record['storage_tool_failure']['returncode'], 73)
        self.assertEqual(record['storage_tool_failure']['label'], 'overlay-create')
        self.assertNotIn('resource_abort', record)
        self.assertIsNone(record['resource_usage']['abort'])
        stdout = member_dir / record['storage_tool_failure']['stdout']
        stderr = member_dir / record['storage_tool_failure']['stderr']
        self.assertEqual(stdout, member_dir / 'overlay-create.stdout')
        self.assertEqual(stderr, member_dir / 'overlay-create.stderr')
        self.assertEqual(stdout.read_bytes(), b'')
        self.assertEqual(stderr.read_bytes(), diagnostic)
        if oversized:
            secondary = record.get('secondary_errors', [])
            self.assertTrue(any(detail.startswith('Failure: ') and 'limit' in detail for detail in secondary))
            self.assertLessEqual(len(secondary), 8)
            self.assertTrue(all(len(detail) <= 256 for detail in secondary))

    def test_family_real_storage_failure_stops_without_inventing_errno(self):
        self._assert_family_real_storage_failure(oversized=False)

    def test_family_oversized_storage_failure_stops_without_inventing_errno(self):
        self._assert_family_real_storage_failure(oversized=True)

    def test_family_real_second_member_capacity_stop(self):
        self.build.update(_selection={'selection_id': '1'}, browser_selection={})
        family_root = self.root / 'capacity-run'
        family_root.mkdir(mode=0o700)
        frozen = SimpleNamespace(IMAGE='fixture', UBUNTU_HASHES={'fixture': 'a' * 64},
                                 load_json=lambda path: {'assets': {'ubuntu:fixture': str(self.root / 'absent-base')}})
        calls = []
        def sample(gate):
            calls.append(gate)
            return (20 * GiB if len(calls) == 1 else 5 * GiB, 4 * GiB if gate else None, 12 if gate else None)
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(resources.SourceResourceGuard, '_host_sample', side_effect=sample):
            ok = source_vm._case(host, frozen, os.environ.copy(), family_root, 'realm-negative',
                                 'bpf-authorization', self.build, manifest_sha256='a' * 64)
        self.assertFalse(ok)
        self.assertEqual(len(calls), 2)
        family = host.strict_json((family_root / 'bpf-authorization' / 'family.json').read_bytes())
        self.assertEqual(family['stop_reason'], 'resource_capacity')
        self.assertEqual([item['status'] for item in family['members'][:2]], ['FAIL', 'FAIL'])
        self.assertTrue(all(item['status'] == 'NOT_RUN' and item['retained_allocated_bytes'] == 0 and
                            item['receipt_sha256'] is None for item in family['members'][2:]))
        second = family_root / family['members'][1]['directory'] / 'receipt.json'
        record = host.strict_json(second.read_bytes())
        self.assertEqual(record['resource_abort'], {'reason': 'disk_admission', 'phase': 'prepare'})
        self.assertEqual(family['retained_allocated_bytes'], sum(item['retained_allocated_bytes']
                                                                for item in family['members']))

    def test_family_stops_on_capacity_and_continues_ordinary_failure(self):
        for capacity in (True, False):
            root = self.root / ('capacity' if capacity else 'ordinary')
            root.mkdir(mode=0o700)
            calls = []
            def member(_host, _frozen, _env, directory, case, scenario, build, variant=None, manifest_sha256=None):
                calls.append(variant)
                directory.mkdir(mode=0o700)
                (directory / 'receipt.json').write_bytes(host.canonical({'status': 'FAIL' if len(calls) == 2 else 'PASS'}))
                return (False, 'resource_capacity' if capacity else None) if len(calls) == 2 else (True, None)
            with mock.patch.object(source_vm, '_run_single_vm_case', side_effect=member):
                ok = source_vm._case(host, None, {}, root, 'realm-negative', 'bpf-authorization',
                                     {'_selection': {}}, manifest_sha256='a' * 64)
            self.assertFalse(ok)
            family = host.strict_json((root / 'bpf-authorization' / 'family.json').read_bytes())
            self.assertEqual(len(family['members']), 25)
            self.assertEqual(len(calls), 2 if capacity else 25)
            self.assertEqual(family['stop_reason'], 'resource_capacity' if capacity else None)
            self.assertEqual(family['members'][1]['status'], 'FAIL')
            if capacity:
                self.assertTrue(all(item['status'] == 'NOT_RUN' and item['receipt_sha256'] is None and
                                    item['retained_allocated_bytes'] == 0 for item in family['members'][2:]))
            self.assertEqual(family['retained_allocated_bytes'],
                             sum(item['retained_allocated_bytes'] for item in family['members']))


    def test_legacy_gate_and_new_generation_rejects_v1(self):
        gate = {'disk_available_bytes': 64 * GiB, 'mem_available_bytes': 4 * GiB, 'kvm_api_version': 12}
        source_verify._legacy_resources(host, {'resources': gate}, 'source-vps', 'normal')
        for available in (20 * GiB, 64 * GiB - 1):
            with self.subTest(available=available), self.assertRaises(host.Failure):
                source_verify._legacy_resources(host, {'resources': dict(gate, disk_available_bytes=available)},
                                                'source-vps', 'normal')
        new_build = {'runner_inputs': {'source_resources.py': 'a' * 64}}
        with self.assertRaises(host.Failure):
            source_verify._verify_single_vm_state(host, None, self.case, 'source-vps', 'normal', new_build,
                                                  {'version': 1, 'run_token': host.TOKEN})

    def test_family_v2_verifier_rejects_forged_not_run_and_uses_latest(self):
        selection = {'selection_id': '1'}
        build = {'_selection': selection, 'runner_inputs': {'source_resources.py': 'a' * 64}}
        root = self.root / 'runs' / 'selection-1' / 'realm-negative'
        root.mkdir(mode=0o700, parents=True)
        for number in (1, 2):
            attempt_id = '%020d' % number
            attempt = root / ('attempt-' + attempt_id)
            attempt.mkdir(mode=0o700)
            manifest = {'version': 1, 'run_token': host.TOKEN, 'build_selection': selection,
                        'attempt': attempt_id, 'case': 'realm-negative', 'scenarios': ['bpf-authorization'],
                        'members': [{'case': c, 'scenario': s, 'variant': v}
                                    for c, s, v in source_vm.BPF_AUTH_FAMILY_MEMBERS]}
            raw = host.canonical(manifest)
            (attempt / 'launch-manifest.json').write_bytes(raw)
            def fail(_host, _frozen, _env, directory, case, scenario, build, variant=None, manifest_sha256=None):
                directory.mkdir(mode=0o700)
                (directory / 'receipt.json').write_bytes(host.canonical({'status': 'FAIL'}))
                return False, 'resource_capacity'
            with mock.patch.object(source_vm, '_run_single_vm_case', side_effect=fail):
                source_vm._case(host, None, {}, attempt, 'realm-negative', 'bpf-authorization', build,
                                manifest_sha256=hashlib.sha256(raw).hexdigest())
        with mock.patch.object(host, 'RUN', self.root):
            result = source_verify._case(host, None, 'realm-negative', 'bpf-authorization', build)
            self.assertEqual(result['status'], 'FAIL')
            self.assertEqual(result['stop_reason'], 'resource_capacity')
            family_path = root / 'attempt-00000000000000000002' / 'bpf-authorization' / 'family.json'
            family = host.strict_json(family_path.read_bytes())
            family['members'][1]['status'] = 'PASS'
            os.chmod(family_path, 0o600)
            family_path.write_bytes(host.canonical(family))
            forged = source_verify._case(host, None, 'realm-negative', 'bpf-authorization', build)
            self.assertEqual(forged['status'], 'FAIL')

    def test_family_failure_retains_independently_read_evidence(self):
        selection = {'selection_id': '1'}
        self.build.update(_selection=selection, runner_inputs={'source_resources.py': 'a' * 64,
                                                                'source_settlement_evidence.py': 'b' * 64})
        root = self.root / 'runs' / 'selection-1' / 'realm-negative'
        root.mkdir(mode=0o700, parents=True)
        attempt_id = '00000000000000000001'
        attempt = root / ('attempt-' + attempt_id)
        attempt.mkdir(mode=0o700)
        manifest = {'version': 1, 'run_token': host.TOKEN, 'build_selection': selection,
                    'attempt': attempt_id, 'case': 'realm-negative', 'scenarios': ['bpf-authorization'],
                    'members': [{'case': c, 'scenario': s, 'variant': v}
                                for c, s, v in source_vm.BPF_AUTH_FAMILY_MEMBERS]}
        raw = host.canonical(manifest)
        (attempt / 'launch-manifest.json').write_bytes(raw)
        manifest_sha = hashlib.sha256(raw).hexdigest()
        original_run = source_vm._run_single_vm_case
        captured = {}
        def member(_host, _frozen, _env, directory, case, scenario, build, variant=None, manifest_sha256=None):
            case_dir, receipt, evidence, argv, outcome = source_vm_failure_fixture.exercise(
                self, 'family-error', case_dir=directory, scenario=scenario, variant=variant,
                manifest_sha=manifest_sha256, run_single=original_run)
            # Missing post-wait proof is PARTIAL; the authentic terminal and
            # independently held captures remain intact.
            receipt.pop('boot_observations', None)
            receipt.pop('carrier_observations', None)
            for name in ('seed.raw', 'cidata.raw'):
                (case_dir / name).unlink()
            receipt_path = case_dir / 'receipt.json'
            receipt_path.write_bytes(host.canonical(receipt))
            with receipt_path.open('rb') as stream:
                os.fsync(stream.fileno())
            captured.update(argv=argv, evidence=evidence, case_dir=case_dir)
            return outcome
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(source_vm, '_run_single_vm_case', side_effect=member):
            self.assertFalse(source_vm._case(host, None, {}, attempt, 'realm-negative', 'bpf-authorization',
                                             self.build, manifest_sha256=manifest_sha))
        frozen = host.frozen_host()
        base = self.root / 'failure-base'
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(frozen, 'IMAGE', 'fixture'), \
             mock.patch.object(frozen, 'UBUNTU_HASHES', {'fixture': host.digest(base)[0]}), \
             mock.patch.object(frozen, 'load_json', return_value={'assets': {'ubuntu:fixture': str(base)}}), \
             mock.patch.object(source_vm, 'expected_vm_argv', side_effect=captured['argv']):
            result = source_verify._case(host, frozen, 'realm-negative', 'bpf-authorization', self.build)
        self.assertEqual(result['status'], 'FAIL')
        self.assertEqual(result['stop_reason'], 'resource_capacity')
        self.assertEqual(len(result['members']), 1)
        member_result = result['members'][0]
        self.assertEqual(member_result['variant'], 'baseline')
        self.assertEqual(member_result['settlement_evidence']['quality'], 'PARTIAL')
        self.assertEqual(member_result['settlement_evidence']['reference_holder'], 'UNRESOLVED')
        self.assertEqual(member_result['verification_limits'], ['CARRIERS', 'RESOURCES'])
        self.assertEqual(host.strict_json((captured['case_dir'] / 'failed-result.json').read_bytes())
                         ['settlement_evidence_v1'], captured['evidence'])
        artifact_path = captured['case_dir'] / 'failed-result.json'
        with artifact_path.open('r+b') as stream:
            first = stream.read(1)
            stream.seek(0)
            stream.write(b' ' if first != b' ' else b'{')
            stream.flush()
            os.fsync(stream.fileno())
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(frozen, 'IMAGE', 'fixture'), \
             mock.patch.object(frozen, 'UBUNTU_HASHES', {'fixture': host.digest(base)[0]}), \
             mock.patch.object(frozen, 'load_json', return_value={'assets': {'ubuntu:fixture': str(base)}}), \
             mock.patch.object(source_vm, 'expected_vm_argv', side_effect=captured['argv']):
            tampered = source_verify._case(host, frozen, 'realm-negative', 'bpf-authorization', self.build)
        self.assertEqual(tampered['status'], 'FAIL')
        self.assertEqual(tampered['members'][0]['settlement_evidence']['quality'], 'INVALID')

    def test_ordinary_failure_preserves_evidence_and_limits(self):
        selection = {'selection_id': '1'}
        self.build.update(_selection=selection, runner_inputs={'source_resources.py': 'a' * 64,
                                                                'source_settlement_evidence.py': 'b' * 64})
        root = self.root / 'runs' / 'selection-1' / 'realm-negative'
        root.mkdir(mode=0o700, parents=True)
        attempt_id = '00000000000000000001'
        attempt = root / ('attempt-' + attempt_id)
        attempt.mkdir(mode=0o700)
        manifest = {'version': 1, 'run_token': host.TOKEN, 'build_selection': selection,
                    'attempt': attempt_id, 'case': 'realm-negative', 'scenarios': ['bpf-broker-kill'],
                    'members': [{'case': 'realm-negative', 'scenario': 'bpf-broker-kill', 'variant': None}]}
        raw = host.canonical(manifest)
        (attempt / 'launch-manifest.json').write_bytes(raw)
        case_dir, receipt, evidence, argv, _ = source_vm_failure_fixture.exercise(
            self, 'carrier-error', case_dir=attempt / 'bpf-broker-kill',
            manifest_sha=hashlib.sha256(raw).hexdigest())
        receipt.pop('boot_observations', None)
        receipt.pop('carrier_observations', None)
        for name in ('seed.raw', 'cidata.raw'):
            (case_dir / name).unlink()
        receipt_path = case_dir / 'receipt.json'
        receipt_path.write_bytes(host.canonical(receipt))
        with receipt_path.open('rb') as stream:
            os.fsync(stream.fileno())
        frozen = host.frozen_host()
        base = self.root / 'failure-base'
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(frozen, 'IMAGE', 'fixture'), \
             mock.patch.object(frozen, 'UBUNTU_HASHES', {'fixture': host.digest(base)[0]}), \
             mock.patch.object(frozen, 'load_json', return_value={'assets': {'ubuntu:fixture': str(base)}}), \
             mock.patch.object(source_vm, 'expected_vm_argv', side_effect=argv):
            result = source_verify._case(host, frozen, 'realm-negative', 'bpf-broker-kill', self.build)
        self.assertEqual(result['status'], 'FAIL')
        self.assertEqual(result['settlement_evidence']['quality'], 'PARTIAL')
        self.assertEqual(result['verification_limits'], ['CARRIERS'])
        self.assertEqual(host.strict_json((case_dir / 'failed-result.json').read_bytes())['settlement_evidence_v1'], evidence)

    def test_aggregation_retains_one_synthetic_pass_but_stays_not_ready(self):
        """Aggregation only: this synthetic PASS is not BPF, QEMU or seed evidence."""
        selection = {'selection_id': '1'}
        build = {'_selection': selection}
        summary = {'quality': 'COMPLETE', 'last_native_stage': 'RELEASE_RETURN',
                   'blocking_points': [], 'delivery': 'UNKNOWN', 'timeliness': 'UNKNOWN',
                   'reference_holder': 'UNRESOLVED'}
        def case_result(_host, _frozen, case, scenario, _build):
            if (case, scenario) == ('realm-negative', 'bpf-broker-kill'):
                return {'case': case, 'scenario': scenario, 'status': 'PASS', 'settlement_evidence': summary}
            return {'case': case, 'scenario': scenario, 'status': 'NOT_RUN'}
        output = io.StringIO()
        with mock.patch.object(host, 'RUN', self.root), \
             mock.patch.object(source_build, '_selected', return_value=build), \
             mock.patch.object(source_build, '_validate_current'), \
             mock.patch.object(source_verify, '_case', side_effect=case_result), \
             contextlib.redirect_stdout(output):
            code = source_verify.verify(host, None)
        result = json.loads(output.getvalue())
        report = host.strict_json(Path(result['report']).read_bytes())
        exact = [item for item in report['cases'] if
                 (item['case'], item['scenario']) == ('realm-negative', 'bpf-broker-kill')]
        self.assertEqual(code, 1)
        self.assertEqual((report['status'], report['product_gate'], report['permanent_deletion_http']),
                         ('NOT_READY', 'NOT_READY', 'CLOSED'))
        self.assertEqual(len(exact), 1)
        self.assertEqual(exact[0]['status'], 'PASS')
        self.assertEqual(exact[0]['settlement_evidence'], summary)
        self.assertEqual(result['passed'], 1)
        self.assertGreater(result['not_run'], 0)

    def test_memory_and_kvm_gate_boundaries(self):
        guard = resources.SourceResourceGuard(host, self.case, budget_for_payload(GiB))
        null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
        self.addCleanup(os.close, null_fd)
        statvfs = SimpleNamespace(f_bavail=20 * GiB, f_frsize=1)
        for mem_kib, api, accepted in ((4 * GiB // 1024, 12, True),
                                        (4 * GiB // 1024 - 1, 12, False),
                                        (4 * GiB // 1024, 11, False)):
            with self.subTest(mem_kib=mem_kib, api=api), \
                 mock.patch.object(resources.os, 'statvfs', return_value=statvfs), \
                 mock.patch.object(resources.Path, 'read_text', return_value='MemAvailable: %d kB\n' % mem_kib), \
                 mock.patch.object(resources.os, 'open', side_effect=lambda *args, **kwargs: os.dup(null_fd)), \
                 mock.patch.object(resources.fcntl, 'ioctl', return_value=api):
                if accepted:
                    self.assertEqual(guard._host_sample(True), (20 * GiB, 4 * GiB, 12))
                else:
                    with self.assertRaises(host.Failure):
                        guard._host_sample(True)

    def test_receipt_rename_failure_replacement_and_size_limit(self):
        writer = source_vm._CaseReceipt(host, self.case)
        self.addCleanup(writer.close)
        writer.save({'status': 'IN_PROGRESS'})
        path = self.case / 'receipt.json'
        old = path.read_bytes()
        with mock.patch.object(source_vm.os, 'rename', side_effect=OSError(errno.ENOSPC, 'full')):
            with self.assertRaises(OSError):
                writer.save({'status': 'FAIL'})
        self.assertEqual(path.read_bytes(), old)
        self.assertFalse((self.case / 'receipt-next.json').exists())
        other_dir = self.root / 'replacement'
        other_dir.mkdir(mode=0o700)
        other = source_vm._CaseReceipt(host, other_dir)
        self.addCleanup(other.close)
        other.save({'status': 'IN_PROGRESS'})
        replacement = other_dir / 'replacement'
        replacement.write_bytes(b'replaced')
        os.replace(replacement, other_dir / 'receipt.json')
        with self.assertRaises(host.Failure):
            other.save({'status': 'PASS'})
        self.assertEqual((other_dir / 'receipt.json').read_bytes(), b'replaced')
        oversized_dir = self.root / 'oversized'
        oversized_dir.mkdir(mode=0o700)
        oversized = source_vm._CaseReceipt(host, oversized_dir)
        self.addCleanup(oversized.close)
        with self.assertRaises(host.Failure):
            oversized.save({'data': 'x' * F})
        self.assertFalse((oversized_dir / 'receipt.json').exists())

    def test_late_parsed_output_blocks_pass_publication(self):
        for over_growth in (True, False):
            directory = self.root / ('late-growth' if over_growth else 'late-free')
            directory.mkdir(mode=0o700)
            writer = source_vm._CaseReceipt(host, directory)
            self.addCleanup(writer.close)
            writer.save({'status': 'IN_PROGRESS'})
            guard, samples = self.guard()
            guard.check_prepare()
            guard.begin_launch()
            before = guard.after_wait()
            source_build._write(directory / 'parsed.json', b'{"completed":true}\n')
            samples['allocated'] += G - F + (1 if over_growth else 0)
            samples['free'] = 20 * GiB if over_growth else R + F - 1
            with self.assertRaises(resources.SourceResourceError):
                guard.finish_member()
            self.assertGreater(guard.snapshot()['peak_growth_bytes'], before['peak_growth_bytes'])
            self.assertEqual(host.strict_json((directory / 'receipt.json').read_bytes())['status'], 'IN_PROGRESS')

    def test_qmp_handshake_resource_abort_and_uncertain_exit(self):
        frozen = host.frozen_host()
        executable = str(Path('/usr/bin/python3').resolve())
        class NoisyCollector(frozen.StderrCollector):
            def finish(self):
                super().finish()
                raise OSError(errno.ENOSPC, 'late collector failure')
        code = '''import json, os, signal, socket, sys
barrier = socket.socket(fileno=int(sys.argv[1]))
stubborn = sys.argv[2] == 'stubborn'
if stubborn:
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
barrier.send(b'ready')
os.write(1, b'{"QMP":{}}\\n')
for line in sys.stdin.buffer:
    request = json.loads(line)
    if request['execute'] == 'quit' and stubborn:
        while True:
            signal.pause()
    os.write(1, (json.dumps({'return': {}, 'id': request['id']}) + '\\n').encode())
    if request['execute'] == 'quit':
        break
if stubborn:
    while True:
        signal.pause()
'''
        for stubborn in (False, True):
            directory = self.root / ('stubborn' if stubborn else 'responsive')
            directory.mkdir(mode=0o700)
            parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
            self.addCleanup(parent.close)
            self.addCleanup(child.close)
            null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
            self.addCleanup(os.close, null_fd)
            boot = {'kernel_fd': child.fileno(), 'initrd_fd': null_fd}
            argv = [executable, '-c', code, str(child.fileno()), 'stubborn' if stubborn else 'responsive']
            build = {'qemu_binaries': {'qemu-system-x86_64': host.digest(Path(executable))[0]}}
            guard, state = self.guard()
            guard.check_prepare()
            source_vm._prepare_vm_captures(directory, guard)
            guard.begin_launch()
            state['free'] = R - 1
            original_check = guard.check_live
            def check_live(force=False):
                self.assertEqual(parent.recv(32), b'ready')
                original_check(force=True)
            guard.check_live = check_live
            processes = []
            real_popen = subprocess.Popen
            def spawn(*args, **kwargs):
                proc = real_popen(*args, **kwargs)
                processes.append(proc)
                return proc
            attempt = {}
            cleanup_reaped = False
            try:
                with mock.patch.object(source_vm, '_check_boot_fds'), \
                     mock.patch.object(source_vm, 'expected_vm_argv', return_value=argv), \
                     mock.patch.object(frozen, 'StderrCollector', NoisyCollector), \
                     mock.patch.object(source_vm.subprocess, 'Popen', side_effect=spawn):
                    with self.assertRaises(resources.SourceResourceError) as error:
                        source_vm._vm(host, frozen, os.environ.copy(), directory, 'realm-negative', 'bpf-authorization',
                                      {'case.json': 'a' * 64}, build, attempt, {}, boot, guard,
                                      variant='seal-guard-loaded')
                self.assertEqual(error.exception.reason, 'disk_reserve')
                if not stubborn:
                    self.assertTrue(any('late collector failure' in item for item in attempt['secondary_errors']))
                self.assertEqual(attempt['exit_observed'], not stubborn)
                if stubborn:
                    self.assertTrue(attempt['exit_uncertain'])
                else:
                    self.assertTrue(attempt['quit_ack'])
                    self.assertTrue(attempt['wait_observed'] and attempt['pidfd_exited'])
            finally:
                for proc in processes:
                    if proc.poll() is None:
                        pidfd = os.pidfd_open(proc.pid, 0)
                        try:
                            signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                            proc.wait(timeout=5)
                            cleanup_reaped = proc.returncode == -signal.SIGKILL
                        finally:
                            os.close(pidfd)
            if stubborn:
                self.assertTrue(cleanup_reaped, 'test cleanup did not reap the still-owned child')
                self.assertFalse(attempt['exit_observed'], 'test cleanup must not rewrite production evidence')

    def test_complete_guest_failure_lifecycle_and_secondary_errors(self):
        for mode in ('complete', 'write-error', 'write-enospc', 'write-partial', 'cleanup-error', 'carrier-error',
                     'partial', 'bad-digest', 'bad-install', 'no-ready', 'wrong-identity', 'host-timeout'):
            with self.subTest(mode=mode):
                source_vm_failure_fixture.exercise(self, mode)

    def test_complete_failure_durable_readback(self):
        source_vm_failure_fixture.exercise(self, 'complete', readback=True)

    def test_reboot_failure_durable_readback(self):
        source_vm_failure_fixture.exercise(self, 'complete', case='source-recovery', scenario='vm-kill', readback=True)

    def test_qmp_capture_is_held_read_write(self):
        import fcntl
        guard, _ = self.guard()
        source_vm._prepare_vm_captures(self.case, guard)
        protocol, _, serial, _, qmp, identity, stderr = guard.captures
        try:
            self.assertEqual(fcntl.fcntl(qmp, fcntl.F_GETFL) & os.O_ACCMODE, os.O_RDWR)
            self.assertTrue(source_vm._same_file(qmp, self.case / 'qmp.log', identity))
            os.write(qmp, b'QMP')
            self.assertEqual(os.pread(qmp, 3, 0), b'QMP')
            self.assertEqual(source_vm._read_held_capture(host, qmp, self.case / 'qmp.log', identity, 10), b'QMP')
            os.rename(self.case / 'qmp.log', self.case / 'qmp-held.log')
            (self.case / 'qmp.log').write_bytes(b'QMP')
            with self.assertRaises(host.Failure):
                source_vm._read_held_capture(host, qmp, self.case / 'qmp.log', identity, 10)
        finally:
            for fd in (protocol, serial, qmp, stderr):
                os.close(fd)
            del guard.captures

if __name__ == '__main__':
    unittest.main()
