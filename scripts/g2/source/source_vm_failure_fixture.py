"""Private carrier fixture for the real source VM failure lifecycle.

The only subprocess is a small QMP/guest-protocol peer, never QEMU. Sparse-image
creation is substituted; receipt, resource guard, captures, parser and shutdown
are the production paths.
"""
import base64
from contextlib import ExitStack
import errno
import hashlib
import json
import os
from pathlib import Path
import time
from types import SimpleNamespace
from unittest import mock

import source_build
import source_host as host
import source_resources as resources
import source_settlement_evidence_test as evidence_test
import source_verify
import source_vm

GIB = 1024 ** 3

# This peer emits the real INSTALL/READY/DATA/END framing while remaining alive
# until the host has actually sent and received the QMP quit exchange.
_PROGRAM = r'''
import base64, hashlib, json, os, pathlib, sys
directory = pathlib.Path(sys.argv[1])
token, scenario, supervisor, mode, evidence_arg, case, prefix = sys.argv[2:]
def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode('ascii')
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def answer(ident, value):
    os.write(1, canonical({'return': value, 'id': ident}) + b'\n')
os.write(1, b'{"QMP":{}}\n')
for line in sys.stdin.buffer:
    request = json.loads(line)
    name, ident = request['execute'], request['id']
    if name == 'query-status':
        value = {'running': True, 'status': 'running'}
    elif name == 'query-cpus-fast':
        value = [{'cpu-index': 0, 'thread-id': 1}, {'cpu-index': 1, 'thread-id': 2}]
    elif name == 'query-pci':
        names = ['bootdisk', 'cidatadisk', 'seeddisk', 'g2-result-controller',
                 'appdisk', 'w1disk', 'w2disk', 'controldisk']
        value = [{'devices': [{'qdev_id': name, 'class_info': {'class': 0}} for name in names]}]
    elif name == 'query-block':
        wanted = [('bootdisk', 'boot.qcow2', 'qcow2', False), ('seeddisk', 'seed.raw', 'raw', True),
                  ('cidatadisk', 'cidata.raw', 'raw', True)]
        wanted += [(name + 'disk', name + '.raw', 'raw', False) for name in ('app', 'w1', 'w2', 'control')]
        value = [{'qdev': '/machine/peripheral/' + name + '/virtio-backend',
                  'inserted': {'file': str(directory / filename), 'drv': driver, 'ro': readonly}}
                 for name, filename, driver, readonly in wanted]
    else:
        value = {}
    if name == 'query-cpus-fast':
        install = {'version': 1, 'guest_sha256': digest(directory / 'payload/source_guest.py'),
                   'supervisor_manifest_sha256': supervisor,
                   'seed_manifest_sha256': digest(directory / 'payload/SHA256SUMS'),
                   'payload_case_sha256': digest(directory / 'payload/case.json'),
                   'seed_descriptor_sha256': digest(directory / 'cidata/seed-descriptor.json')}
        if mode == 'bad-install':
            install['guest_sha256'] = '0' * 64
        transport = {'kind': 'virtio-serial-file-v1', 'port_name': 'org.houfeng.g2.result',
                     'sysfs_name': 'vport0p1', 'device': {'dev': 1, 'ino': 1, 'major': 1, 'minor': 1,
                     'uid': 0, 'gid': 0, 'mode': 384}}
        envelope = {'version': 2, 'run_token': token, 'case': scenario,
                    'installation': install, 'transport': transport}
        install_bytes = canonical(envelope)
        result = {'version': 1, 'run_token': token, 'case': case, 'scenario': scenario,
                  'completed': case == 'source-recovery' and not prefix,
                  'settlement_evidence_v1': json.loads(base64.b64decode(evidence_arg))}
        if result['completed']:
            result['negative'] = {'prearmed': True, 'source_handoff_pending': True}
        if mode == 'wrong-identity':
            result['run_token'] = 'f' * 32
        result_bytes = canonical(result)
        pieces = [result_bytes[i:i + 2048] for i in range(0, len(result_bytes), 2048)]
        frames = ['G2LAB-INSTALL %s %s %s\n' % (token, hashlib.sha256(install_bytes).hexdigest(),
                  base64.b64encode(install_bytes).decode())]
        if mode != 'no-ready':
            frames.append('G2LAB-READY %s\n' % token)
        frames += ['G2LAB-DATA %s %d/%d %s\n' % (token, i, len(pieces), base64.b64encode(part).decode())
                   for i, part in enumerate(pieces)]
        if mode != 'partial':
            end = '0' * 64 if mode == 'bad-digest' else hashlib.sha256(result_bytes).hexdigest()
            frames.append('G2LAB-END %s %s\n' % (token, end))
        with open(directory / (prefix + 'protocol.log'), 'ab', buffering=0) as stream:
            stream.write(''.join(frames).encode('ascii'))
    answer(ident, value)
    if name == 'quit' or name == 'query-cpus-fast' and mode == 'partial':
        break
'''


def exercise(test, mode, *, case_dir=None, scenario='bpf-broker-kill', variant=None,
             manifest_sha='a' * 64, run_single=None, case='realm-negative', readback=False):
    frozen = host.frozen_host()
    executable = str(Path('/usr/bin/python3').resolve())
    source = test.seed / 'source_guest.py'
    source.write_bytes(b'guest fixture')
    test.build['seed_inputs']['source_guest.py'] = host.digest(source)[0]
    test.build.update(_selection={'selection_id': '1'}, browser_selection={},
                      supervisor_manifest_sha256='d' * 64,
                      qemu_binaries={'qemu-system-x86_64': host.digest(Path(executable))[0], 'qemu-img': 'e' * 64})
    base = test.root / 'failure-base'
    base.write_bytes(b'base fixture')
    base_sha = host.digest(base)[0]
    if readback:
        paths = {}
        for kind in ('kernel', 'initrd', 'original_initrd'):
            path = test.build_dir / (kind + '.fixture')
            path.write_bytes(kind.encode())
            path.chmod(0o400)
            paths[kind] = path
        test.build['boot_inputs'] = {
            'version': 1, 'base_sha256': base_sha, 'kernel_release': '6.8.0-147-generic',
            'kernel_path': paths['kernel'].name, 'kernel_bytes': paths['kernel'].stat().st_size,
            'kernel_sha256': host.digest(paths['kernel'])[0],
            'initrd_path': paths['initrd'].name, 'initrd_bytes': paths['initrd'].stat().st_size,
            'initrd_sha256': host.digest(paths['initrd'])[0],
            'original_initrd_path': paths['original_initrd'].name,
            'original_initrd_sha256': host.digest(paths['original_initrd'])[0],
            'bootstrap_manifest_sha256': 'a' * 64, 'observer_bindings_sha256': 'b' * 64,
            'command_line': source_vm.BOOT_COMMAND_LINE}
        test.build['runner_inputs'] = {'source_resources.py': 'a' * 64, 'source_settlement_evidence.py': 'b' * 64}
    case_dir = case_dir or test.root / ('guest-' + mode)
    events = []
    evidence = evidence_test.envelope(evidence_test.snapshot(1), evidence_test.snapshot(3))
    evidence_arg = base64.b64encode(json.dumps(evidence, sort_keys=True, separators=(',', ':')).encode()).decode()

    def expected_argv(_host, directory, boot_fds, reboot=False, variant=None, case_sha256=None):
        return [executable, '-c', _PROGRAM, str(directory), host.TOKEN, scenario,
                test.build['supervisor_manifest_sha256'], mode, evidence_arg, case, 'reboot-' if reboot else '']

    def boot_fds(_host, _build):
        if readback:
            return original_open_boot_fds(_host, _build)
        return {'kernel_fd': os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC),
                'initrd_fd': os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)}

    def disk_tool(_host, argv, *args, **kwargs):
        if argv[1] == 'create':
            Path(argv[-1]).write_bytes(b'overlay')

    def raw_disk(_host, path):
        path.write_bytes(b'raw fixture')
        info = path.stat()
        return {'dev': info.st_dev, 'ino': info.st_ino, 'size': info.st_size}

    def disk_info(_host, path, _env):
        if path.name == 'boot.qcow2':
            return {'format': 'qcow2', 'backing-filename': str(base)}
        return {'format': 'raw', 'virtual-size': 8 * GIB}

    def seed_image(_host, _env, _build, directory, hashes, guard):
        path = directory / 'seed.raw'
        path.write_bytes(b'seed fixture')
        if readback:
            with path.open('r+b') as stream:
                stream.truncate(2 * GIB)
        path.chmod(0o400)
        info = path.stat()
        return {'dev': info.st_dev, 'ino': info.st_ino, 'size': info.st_size,
                'sha256': host.digest(path)[0], 'uuid': '11111111-1111-1111-1111-111111111111',
                'label': 'G2SEED', 'features': sorted(source_vm.SEED_FEATURES)}

    def cidata_image(_host, _env, directory, _sha):
        path = directory / 'cidata.raw'
        path.write_bytes(b'cidata fixture')
        if readback:
            with path.open('r+b') as stream:
                stream.truncate(1024 * 16 * 63 * 512)
        path.chmod(0o400)
        info = path.stat()
        return os.open(path, os.O_RDONLY | os.O_CLOEXEC), {
            'path': path.name, 'dev': info.st_dev, 'ino': info.st_ino, 'size': info.st_size,
            'uid': info.st_uid, 'mode': 0o400, 'sha256': host.digest(path)[0]}

    original_write, original_shutdown = source_build._write, frozen.held_shutdown
    original_open_boot_fds = source_vm._open_boot_fds
    original_disk, original_after_wait = source_vm._check_held_disk, resources.SourceResourceGuard.after_wait
    original_save, original_sync = source_vm._CaseReceipt.save, source_vm._sync_dir
    original_qmp_type = source_vm.qmp_type
    clock_offset = [0]
    clock = SimpleNamespace(monotonic=lambda: time.monotonic() + clock_offset[0])

    def qmp_factory(h, f):
        base = original_qmp_type(h, f)
        if mode != 'host-timeout':
            return base
        class TimeoutQmp(base):
            def execute(inner, name, deadline, arguments=None):
                result = super().execute(name, deadline, arguments)
                if name == 'query-cpus-fast':
                    clock_offset[0] = 7200
                return result
        return TimeoutQmp

    def write(path, data, mode_bits=0o600):
        if mode in ('write-error', 'write-enospc') and Path(path).name == 'failed-result.json':
            raise OSError(errno.ENOSPC if mode == 'write-enospc' else errno.EIO, 'failed artifact fixture')
        if mode == 'write-partial' and Path(path).name == 'failed-result.json':
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            try:
                os.write(fd, b'{')
                os.fsync(fd)
            finally:
                os.close(fd)
            raise OSError(errno.EIO, 'partial artifact fixture')
        result = original_write(path, data, mode_bits)
        if Path(path).name in ('failed-result.json', 'reboot-failed-result.json'):
            events.append('artifact-fsync')
        return result

    def sync_dir(path):
        result = original_sync(path)
        if Path(path) == case_dir and any((case_dir / name).exists() for name in ('failed-result.json', 'reboot-failed-result.json')):
            events.append('artifact-directory-fsync')
        return result

    def shutdown(proc, pidfd, qmp, attempt):
        original_shutdown(proc, pidfd, qmp, attempt)
        events.append('exit')
        if mode == 'cleanup-error':
            raise OSError(errno.EIO, 'shutdown fixture')

    def check_disk(h, fd, path, *args):
        if mode in ('carrier-error', 'family-error') and 'exit' in events and path.name == 'seed.raw':
            raise host.Failure('carrier fixture')
        return original_disk(h, fd, path, *args)

    def after_wait(guard, reboot=False):
        try:
            result = original_after_wait(guard, reboot)
        finally:
            events.append('resources')
        if mode == 'family-error':
            raise resources.SourceResourceError('growth_limit', 'after_wait')
        return result

    def save(writer, state):
        if state.get('status') == 'FAIL':
            events.append('receipt')
            if mode in ('complete', 'cleanup-error', 'carrier-error', 'family-error'):
                test.assertTrue(any((case_dir / name).is_file() for name in ('failed-result.json', 'reboot-failed-result.json')))
        elif state.get('carrier_observations', [{}])[-1].get('stage') == 'after_wait':
            events.append('carriers')
        return original_save(writer, state)

    with ExitStack() as patches:
        patches.enter_context(mock.patch.object(host, 'RUN', test.root))
        patches.enter_context(mock.patch.object(frozen, 'IMAGE', 'fixture'))
        patches.enter_context(mock.patch.object(frozen, 'UBUNTU_HASHES', {'fixture': base_sha}))
        patches.enter_context(mock.patch.object(frozen, 'load_json', return_value={'assets': {'ubuntu:fixture': str(base)}}))
        patches.enter_context(mock.patch.object(frozen, 'held_shutdown', side_effect=shutdown))
        patches.enter_context(mock.patch.object(resources.SourceResourceGuard, '_host_sample',
                           side_effect=lambda gate: (20 * GIB, 4 * GIB if gate else None, 12 if gate else None)))
        patches.enter_context(mock.patch.object(resources.SourceResourceGuard, 'after_wait', after_wait))
        patches.enter_context(mock.patch.object(source_vm._CaseReceipt, 'save', save))
        patches.enter_context(mock.patch.object(source_vm, '_open_boot_fds', side_effect=boot_fds))
        patches.enter_context(mock.patch.object(source_vm, '_check_boot_fds'))
        patches.enter_context(mock.patch.object(source_vm, 'expected_vm_argv', side_effect=expected_argv))
        patches.enter_context(mock.patch.object(source_vm, 'time', clock))
        patches.enter_context(mock.patch.object(source_vm, 'qmp_type', side_effect=qmp_factory))
        patches.enter_context(mock.patch.object(source_vm, '_raw', side_effect=raw_disk))
        patches.enter_context(mock.patch.object(source_vm, '_disk_info', side_effect=disk_info))
        patches.enter_context(mock.patch.object(source_vm, '_seed_image', side_effect=seed_image))
        patches.enter_context(mock.patch.object(source_vm, '_create_cidata_image', side_effect=cidata_image))
        patches.enter_context(mock.patch.object(source_vm, '_check_held_disk', side_effect=check_disk))
        patches.enter_context(mock.patch.object(source_vm, '_sync_dir', side_effect=sync_dir))
        patches.enter_context(mock.patch.object(source_build, '_run', side_effect=disk_tool))
        patches.enter_context(mock.patch.object(source_build, '_write', side_effect=write))
        outcome = (run_single or source_vm._run_single_vm_case)(host, frozen, os.environ.copy(), case_dir,
            case, scenario, test.build, variant=variant, manifest_sha256=manifest_sha)
        if readback:
            import copy
            receipt_path = case_dir / 'receipt.json'
            original = host.strict_json(receipt_path.read_bytes())
            def verify(state):
                receipt_path.write_bytes(host.canonical(state))
                return source_verify._verify_single_vm_dir(host, frozen, case_dir, case, scenario, test.build,
                                                           variant, manifest_sha)
            result = verify(original)
            test.assertEqual(result['status'], 'FAIL')
            test.assertEqual(result['settlement_evidence']['quality'], 'COMPLETE')
            test.assertEqual(result['verification_limits'], [])
            test.assertNotIn('base_after', original)
            for status in ('FAIL', 'UNKNOWN'):
                partial = copy.deepcopy(original)
                partial['failure_checks']['carriers'] = status
                partial['boot_observations'] = partial['boot_observations'][:1]
                partial['carrier_observations'] = partial['carrier_observations'][:1]
                result = verify(partial)
                test.assertEqual(result['settlement_evidence']['quality'], 'PARTIAL')
                test.assertEqual(result['verification_limits'], ['CARRIERS'])
                bad = copy.deepcopy(partial)
                bad['carrier_observations'][0]['seed_sha256'] = '0' * 64
                test.assertEqual(verify(bad)['settlement_evidence']['quality'], 'INVALID')
                for name in ('seed.raw', 'cidata.raw'):
                    path = case_dir / name
                    path.chmod(0o600)
                    with path.open('r+b') as stream:
                        before = stream.read(1)
                        stream.seek(0)
                        stream.write(bytes([before[0] ^ 1]))
                    path.chmod(0o400)
                    test.assertEqual(verify(partial)['settlement_evidence']['quality'], 'INVALID')
                    without_observations = copy.deepcopy(partial)
                    without_observations.pop('carrier_observations')
                    test.assertEqual(verify(without_observations)['settlement_evidence']['quality'], 'INVALID')
                    path.chmod(0o600)
                    with path.open('r+b') as stream:
                        stream.write(before)
                    path.chmod(0o400)
                base.write_bytes(b'changed fixture')
                test.assertEqual(verify(partial)['settlement_evidence']['quality'], 'INVALID')
                base.write_bytes(b'base fixture')
            verify(original)

    test.assertEqual(outcome, (False, 'resource_capacity' if mode in ('family-error', 'write-enospc') else None))
    receipt = host.strict_json((case_dir / 'receipt.json').read_bytes())
    test.assertEqual(receipt['status'], 'FAIL')
    test.assertNotEqual(receipt.get('stage'), 'complete')
    test.assertEqual(events[-1], 'receipt')
    if mode in ('complete', 'write-error', 'write-enospc', 'write-partial', 'cleanup-error', 'carrier-error', 'family-error'):
        test.assertEqual(receipt['error'], 'guest reported terminal failure')
        test.assertTrue(receipt['attempt']['exit_observed'])
        test.assertLess(events.index('exit'), events.index('resources'))
        test.assertLess(events.index('resources'), events.index('receipt'))
        test.assertEqual(receipt['failure_checks']['exit'], 'PASS')
        test.assertEqual(receipt['failure_checks']['resources'], 'FAIL' if mode in ('family-error', 'write-enospc') else 'PASS')
        if mode not in ('carrier-error', 'family-error'):
            test.assertLess(events.index('resources'), events.index('carriers'))
            test.assertLess(events.index('carriers'), events.index('receipt'))
            test.assertEqual(receipt['failure_checks']['carriers'], 'PASS')
        else:
            test.assertEqual(receipt['failure_checks']['carriers'], 'FAIL')
        if mode in ('write-error', 'write-enospc', 'write-partial', 'cleanup-error', 'carrier-error', 'family-error'):
            test.assertTrue(receipt.get('secondary_errors'))
        if mode == 'write-enospc':
            test.assertEqual(receipt['resource_abort'], {'reason': 'enospc', 'phase': 'running'})
    if mode == 'bad-digest':
        test.assertEqual(receipt['error'], 'guest result digest differs')
    if mode == 'host-timeout':
        test.assertEqual(receipt['error'], 'guest wall deadline exceeded')
    if mode in ('complete', 'cleanup-error', 'carrier-error', 'family-error'):
        test.assertLess(events.index('artifact-fsync'), events.index('artifact-directory-fsync'))
        test.assertLess(events.index('artifact-directory-fsync'), events.index('receipt'))
        prefix = 'reboot-' if case == 'source-recovery' and scenario == 'vm-kill' else ''
        attempt = receipt['reboot_attempt' if prefix else 'attempt']
        artifact = attempt['failed_result']
        raw = (case_dir / artifact['path']).read_bytes()
        test.assertEqual((artifact['bytes'], artifact['sha256']), (len(raw), hashlib.sha256(raw).hexdigest()))
        test.assertIs(host.strict_json(raw)['completed'], False)
        parser = source_vm._parser(host, frozen, case, scenario, receipt['seed'], test.build, receipt['cidata'])
        test.assertEqual(raw, host.canonical(parser((case_dir / (prefix + 'protocol.log')).read_bytes(), True)['result']))
        commands = source_verify._qmp(host, case_dir / (prefix + 'qmp.log'), attempt, case_dir)
        test.assertEqual(commands[:5], ['qmp_capabilities', 'query-status', 'query-block', 'query-pci', 'query-cpus-fast'])
    else:
        test.assertNotIn('failed_result', receipt['attempt'])
        if mode == 'write-partial':
            test.assertTrue((case_dir / 'failed-result.json').is_file())
            result = source_verify._verify_single_vm_failure_state(host, frozen, case_dir, 'realm-negative',
                                                                    scenario, test.build, receipt, variant)
            test.assertEqual(result['settlement_evidence']['quality'], 'INVALID')
        else:
            test.assertFalse((case_dir / 'failed-result.json').exists())
    return case_dir, receipt, evidence, expected_argv, outcome
