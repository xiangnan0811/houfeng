"""Behavioral tests for source result transport and authenticated reboot replay."""
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent / 'source_supervisor'))
from journal import JournalError

import source_host as host
import source_protocol
import source_verify
import source_vm


class Capture:
    def __init__(self, result, stream):
        self.RESULT = result
        self.stream = stream

    def emit_protocol(self, line, deadline):
        self.stream.write((line + '\n').encode('ascii'))


class SourceProtocolTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.frozen = host.frozen_host()
        cls.frozen.RUN = host.PHASE0_DATA
        cls.install = host.strict_json((host.PHASE0_DATA / 'runs/matrix/receipt.json').read_bytes())['preparer_install']
        cls.transport = {'kind': 'virtio-serial-file-v1', 'port_name': 'org.houfeng.g2.result',
                         'sysfs_name': 'vport0p1',
                         'device': {'dev': 1, 'ino': 1, 'major': 1, 'minor': 1, 'uid': 0, 'gid': 0, 'mode': 0o600}}
        cls.envelope = {'version': 2, 'run_token': host.TOKEN, 'case': 'matrix',
                        'installation': cls.install, 'transport': cls.transport}
        cls.prefix = cls.install_frame(cls.envelope) + ('G2LAB-READY ' + host.TOKEN + '\n').encode()

    @staticmethod
    def install_frame(envelope):
        raw = host.canonical(envelope)[:-1]
        return ('G2LAB-INSTALL %s %s %s\n' % (host.TOKEN, hashlib.sha256(raw).hexdigest(),
                base64.b64encode(raw).decode())).encode()

    def frames(self, result):
        with tempfile.TemporaryFile() as stream:
            carrier = Capture(result, stream)
            source_protocol.emit(carrier)
            stream.seek(0)
            return self.prefix + stream.read()

    def parse(self, data, complete=True):
        return source_protocol.protocol_prefix(host, self.frozen, data, 'matrix', complete, self.install)

    def source_parser(self):
        hashes = {'source_guest.py': 'a', 'SHA256SUMS': 'b', 'case.json': 'c'}
        build = {'supervisor_manifest_sha256': 'd'}
        cidata = {'seed-descriptor.json': 'e'}
        install = {'version': 1, 'guest_sha256': 'a', 'supervisor_manifest_sha256': 'd',
                   'seed_manifest_sha256': 'b', 'payload_case_sha256': 'c', 'seed_descriptor_sha256': 'e'}
        env = dict(self.envelope, installation=install)
        return source_vm._parser(host, self.frozen, 'source-recovery', 'matrix', hashes, build, cidata), env

    def test_real_emitter_large_and_reboot_readback(self):
        parser, envelope = self.source_parser()
        result = {'version': 1, 'run_token': host.TOKEN, 'case': 'source-recovery',
                  'scenario': 'matrix', 'padding': 'x' * (5 * 1024 * 1024)}
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            protocol = directory / 'reboot-protocol.log'
            with protocol.open('wb') as stream:
                stream.write(self.install_frame(envelope) + ('G2LAB-READY ' + host.TOKEN + '\n').encode())
                source_protocol.emit(Capture(result, stream))
            protocol.chmod(0o600)
            parsed = directory / 'reboot-parsed.json'
            parsed.write_bytes(host.canonical(result))
            parsed.chmod(0o600)
            info = protocol.stat()
            state = {'reboot_attempt': {'protocol_identity': [info.st_dev, info.st_ino, info.st_uid, 0o600],
                                        'protocol_sha256': host.digest(protocol)[0]},
                     'reboot_parsed_sha256': host.digest(parsed)[0]}
            self.assertLess(len(host.canonical(state)), 1024 * 1024)
            self.assertEqual(source_verify._read_reboot_result(host, directory, state, parser), result)
            with protocol.open('ab') as out:
                out.write(b'corrupt\n')
            state['reboot_attempt']['protocol_sha256'] = host.digest(protocol)[0]
            with self.assertRaises(host.Failure):
                source_verify._read_reboot_result(host, directory, state, parser)
            with protocol.open('r+b') as out:
                out.truncate(info.st_size)
            state['reboot_attempt']['protocol_sha256'] = host.digest(protocol)[0]
            self.assertEqual(source_verify._read_reboot_result(host, directory, state, parser), result)
            state['reboot_parsed_sha256'] = '0' * 64
            with self.assertRaises(host.Failure):
                source_verify._read_reboot_result(host, directory, state, parser)
            state['reboot_parsed_sha256'] = host.digest(parsed)[0]
            with parsed.open('ab') as out:
                out.write(b' ')
            with self.assertRaises(host.Failure):
                source_verify._read_reboot_result(host, directory, state, parser)
            parsed.unlink()
            with self.assertRaises(host.Failure):
                source_verify._read_reboot_result(host, directory, state, parser)
            parsed.symlink_to(protocol)
            with self.assertRaises(host.Failure):
                source_verify._read_reboot_result(host, directory, state, parser)

    def test_phase_zero_differential_and_4096_data(self):
        result = {'run_token': host.TOKEN, 'text': 'x' * 9000}
        frames = self.frames(result)
        source = self.parse(frames)
        old = self.frozen._protocol_prefix(frames, 'matrix', True)
        self.assertEqual(source, old)
        raw = b'x' * 4096
        data = ('G2LAB-DATA %s 0/1 %s\nG2LAB-END %s %s\n' %
                (host.TOKEN, base64.b64encode(raw).decode(), host.TOKEN, hashlib.sha256(raw).hexdigest())).encode()
        self.assertEqual(self.parse(self.prefix + data), self.frozen._protocol_prefix(self.prefix + data, 'matrix', True))
        self.assertEqual(self.parse(self.prefix + data)['chunks'], [raw])
        huge = self.frames({'run_token': host.TOKEN, 'text': 'x' * (5 * 1024 * 1024)})
        self.assertGreater(sum(map(len, self.parse(huge)['chunks'])), 4 * 1024 * 1024)
        with self.assertRaises(self.frozen.Failure):
            self.frozen._protocol_prefix(huge, 'matrix', True)

    def test_frame_rejections(self):
        frames = self.frames({'run_token': host.TOKEN, 'value': 1})
        lines = frames.splitlines(keepends=True)
        wrong_install = dict(self.envelope, installation={})
        wrong_transport = dict(self.envelope, transport=dict(self.transport, sysfs_name='bad'))
        bad = [b''.join(lines[1:]), lines[0] + b''.join(lines[2:]),
               self.install_frame(wrong_install) + b''.join(lines[1:]),
               self.install_frame(wrong_transport) + b''.join(lines[1:]),
               frames.replace(host.TOKEN.encode(), b'0' * 32, 1), frames[:-1],
               b''.join(lines[:3] + [lines[2]] + lines[3:]),
               frames.replace(b' 0/1 ', b' 1/1 '), frames + b'extra\n',
               frames.replace(b'G2LAB-DATA', b'G2LAB-FAIL', 1),
               lines[0] + lines[1] + ('G2LAB-DATA %s 0/1 YR==\n' % host.TOKEN).encode() + lines[3]]
        for data in bad:
            with self.subTest(data=data[:120]):
                with self.assertRaises(host.Failure):
                    self.parse(data)
                with self.assertRaises((self.frozen.Failure, ValueError)):
                    self.frozen._protocol_prefix(data, 'matrix', True)
        parser, envelope = self.source_parser()
        prefix = self.install_frame(envelope) + ('G2LAB-READY ' + host.TOKEN + '\n').encode()
        for raw in (b'{"version":1,"version":1}', b'{"version":NaN}'):
            frame = ('G2LAB-DATA %s 0/1 %s\nG2LAB-END %s %s\n' %
                     (host.TOKEN, base64.b64encode(raw).decode(), host.TOKEN, hashlib.sha256(raw).hexdigest())).encode()
            with self.assertRaises(host.Failure):
                parser(prefix + frame, True)
        raw = b'{"version":1}'
        bad_digest = ('G2LAB-DATA %s 0/1 %s\nG2LAB-END %s %s\n' %
                      (host.TOKEN, base64.b64encode(raw).decode(), host.TOKEN, '0' * 64)).encode()
        with self.assertRaises(host.Failure):
            parser(prefix + bad_digest, True)

    def test_emit_result_boundary(self):
        overhead = len(json.dumps({'run_token': host.TOKEN, 'padding': ''}, sort_keys=True,
                                  separators=(',', ':'), ensure_ascii=True).encode())
        with tempfile.TemporaryFile() as stream:
            carrier = Capture({'run_token': host.TOKEN, 'padding': 'x' * (source_protocol.MAX_RESULT_BYTES - overhead)}, stream)
            source_protocol.emit(carrier)
            self.assertGreater(stream.tell(), source_protocol.MAX_RESULT_BYTES)
            stream.seek(0)
            decoded = self.parse(self.prefix + stream.read())
            self.assertEqual(sum(map(len, decoded['chunks'])), source_protocol.MAX_RESULT_BYTES)
        with tempfile.TemporaryFile() as stream:
            carrier = Capture({'run_token': host.TOKEN, 'padding': 'x' * (source_protocol.MAX_RESULT_BYTES - overhead + 1)}, stream)
            with self.assertRaises(JournalError):
                source_protocol.emit(carrier)
            self.assertFalse(carrier.RESULT['completed'])
            self.assertEqual(stream.tell(), 0)

    def test_serialization_failure_precedes_data(self):
        with tempfile.TemporaryFile() as stream:
            carrier = Capture({'run_token': host.TOKEN, 'completed': True, 'value': float('nan')}, stream)
            with self.assertRaises(ValueError):
                source_protocol.emit(carrier)
            self.assertFalse(carrier.RESULT['completed'])
            self.assertEqual(stream.tell(), 0)

    def test_receipt_limit_is_unchanged(self):
        with tempfile.TemporaryDirectory() as tmp:
            receipt = source_vm._CaseReceipt(host, Path(tmp))
            try:
                receipt.save({'padding': 'x' * (1024 * 1024)})
            except host.Failure:
                pass
            else:
                self.fail('oversized receipt was accepted')
            finally:
                receipt.close()

    def test_early_result_limit_and_chunk_count(self):
        case_sha = 'a' * 64
        payload = {'version': 1, 'run_token': host.TOKEN, 'case_sha256': case_sha,
                   'seal_stage': 'seal-guard-loaded', 'init_handoff': False,
                   'terminal_observation': {'padding': ''}}
        overhead = len(host.canonical(payload))
        payload['terminal_observation']['padding'] = 'x' * (source_protocol.MAX_RESULT_BYTES - overhead)
        raw = host.canonical(payload)
        digest = hashlib.sha256(raw).hexdigest()
        begin = {'version': 1, 'run_token': host.TOKEN, 'case_sha256': case_sha,
                 'variant': 'seal-guard-loaded', 'bytes': len(raw), 'sha256': digest, 'chunks': 8192}
        end = {k: begin[k] for k in ('version', 'run_token', 'case_sha256', 'bytes', 'sha256', 'chunks')}
        frames = bytearray(b'G2_BPF_EARLY_BEGIN ' + host.canonical(begin))
        for sequence in range(8192):
            part = raw[sequence * 8192:(sequence + 1) * 8192]
            frames.extend(b'G2_BPF_EARLY_CHUNK ' + host.canonical(
                {'sequence': sequence, 'data': base64.b64encode(part).decode('ascii')}))
        frames.extend(b'G2_BPF_EARLY_END ' + host.canonical(end))
        parsed = source_vm.parse_early_serial_stream(host, bytes(frames), 'seal-guard-loaded', case_sha, True)
        self.assertEqual(parsed['result'], payload)
        begin['bytes'] += 1
        with self.assertRaises(host.Failure):
            source_vm.parse_early_serial_stream(host, b'G2_BPF_EARLY_BEGIN ' + host.canonical(begin),
                                                'seal-guard-loaded', case_sha, False)
        begin['bytes'] -= 1
        begin['chunks'] += 1
        with self.assertRaises(host.Failure):
            source_vm.parse_early_serial_stream(host, b'G2_BPF_EARLY_BEGIN ' + host.canonical(begin),
                                                'seal-guard-loaded', case_sha, False)

    def test_go_early_fixture(self):
        binary = os.environ['G2_EARLY_RESULT_TEST_BINARY']
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'early.frames'
            env = dict(os.environ, G2_EARLY_RESULT_FIXTURE_PATH=str(path))
            subprocess.run([binary, '-test.run', '^TestEarlyResultBudgetPolicy$'], env=env, check=True)
            value = source_vm.parse_early_serial_stream(host, path.read_bytes(), 'seal-guard-loaded', 'a' * 64, True)
            self.assertEqual(value['result'], {'version': 1, 'run_token': host.TOKEN, 'case_sha256': 'a' * 64,
                              'seal_stage': 'seal-guard-loaded', 'init_handoff': False,
                              'terminal_observation': {'padding': 'x' * (5 * 1024 * 1024)}})


if __name__ == '__main__':
    unittest.main()
