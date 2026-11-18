"""Offline source-layout regressions using only maintained phase-zero inputs."""
import hashlib
import importlib.machinery
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

import browser_negative
import source_bpf_build
import source_boot
import source_build
import source_host as host
from source_retirement_test import RetirementFixture
ORIGINAL_PHASE = host.PHASE0


class LayoutTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root / 'source'
        self.phase = self.root / 'phase0'
        self.run = self.root / 'run'
        self.source.mkdir()
        self.phase.mkdir()
        self.run.mkdir()
        shutil.copyfile(host.SOURCE_ROOT / 'source_host.py', self.source / 'source_host.py')
        for name in (*host.PHASE0_FILES, 'candidate-implementation.json', 'guest.py', 'handoff.py',
                     'guest_matrix.py', 'prepare_launcher.py'):
            shutil.copyfile(host.PHASE0 / name, self.phase / name)
        self.patches = [mock.patch.object(host, 'SOURCE_ROOT', self.source),
                        mock.patch.object(host, 'PHASE0', self.phase),
                        mock.patch.object(host, '__file__', str(self.source / 'source_host.py')),
                        mock.patch.object(host, 'RUN', self.run),
                        mock.patch.object(host, 'ASSETS', self.root / 'historical-assets')]
        for patch in self.patches:
            patch.start()
            self.addCleanup(patch.stop)

    def clean_modules(self):
        for name in ('prepare_import', 'preparation', 'verify_handoff', '_g2_frozen_phase0_host'):
            sys.modules.pop(name, None)

    def isolate_modules(self):
        names = ('prepare_import', 'preparation', 'verify_handoff', '_g2_frozen_phase0_host')
        saved = {name: sys.modules[name] for name in names if name in sys.modules}
        def restore():
            self.clean_modules()
            sys.modules.update(saved)
        self.addCleanup(restore)
        self.clean_modules()

    def test_layout_and_real_frozen_host(self):
        self.isolate_modules()
        saved_path = sys.path[:]
        self.addCleanup(lambda: sys.path.__setitem__(slice(None), saved_path))
        host.validate_source_layout()
        frozen = host.frozen_host()
        self.assertEqual((frozen.RUN, frozen.TOKEN, frozen.OLD), (self.run, host.TOKEN, host.ASSETS))
        for name in ('prepare_import', 'preparation', 'verify_handoff'):
            self.assertEqual(getattr(frozen, name).__file__, str(self.phase / (name + '.py')))

    def test_foreign_cached_and_uncached_modules_rejected(self):
        self.isolate_modules()
        saved_path = sys.path[:]
        self.addCleanup(lambda: sys.path.__setitem__(slice(None), saved_path))
        sys.modules['prepare_import'] = SimpleNamespace(__file__='/foreign/prepare_import.py')
        with self.assertRaises(host.Failure):
            host.frozen_host()
        self.clean_modules()
        spec = importlib.machinery.ModuleSpec('prepare_import', loader=object(), origin='/foreign/prepare_import.py')
        with mock.patch.object(host.importlib.util, 'find_spec', return_value=spec):
            with self.assertRaises(host.Failure):
                host.frozen_host()
        self.assertNotIn('prepare_import', sys.modules)

    def test_symlinks_and_pin_drift_rejected(self):
        original = self.phase / 'guest.py'
        original.write_bytes(original.read_bytes() + b'changed')
        with self.assertRaises(host.Failure):
            host.validate_source_layout()
        shutil.copyfile(ORIGINAL_PHASE / 'guest.py', original)
        manifest = self.phase / 'candidate-implementation.json'
        manifest.write_bytes(manifest.read_bytes() + b' ')
        with self.assertRaises(host.Failure):
            host.validate_source_layout()
        shutil.copyfile(ORIGINAL_PHASE / 'candidate-implementation.json', manifest)
        pinned = self.phase / 'prepare_import.py'
        pinned.write_bytes(pinned.read_bytes() + b' ')
        with self.assertRaises(host.Failure):
            host.validate_source_layout()
        pinned.unlink()
        pinned.symlink_to(ORIGINAL_PHASE / 'prepare_import.py')
        with self.assertRaises(host.Failure):
            host.validate_source_layout()
        pinned.unlink()
        shutil.copyfile(ORIGINAL_PHASE / 'prepare_import.py', pinned)
        alias = self.root / 'phase-link'
        alias.symlink_to(self.phase, target_is_directory=True)
        with mock.patch.object(host, 'PHASE0', alias):
            with self.assertRaises(host.Failure):
                host.validate_source_layout()

    def test_current_guard_abi_header_must_be_v5(self):
        header = self.source / 'source_supervisor/bpf-authority/native/abi.h'
        header.parent.mkdir(parents=True)
        header.write_bytes(b'#define G2_GUARD_VERSION 5U\n')
        source_build._require_settlement_guard_abi(host)
        header.write_bytes(b'#define G2_GUARD_VERSION 4U\n')
        with self.assertRaises(host.Failure):
            source_build._require_settlement_guard_abi(host)

    def test_seed_and_native_sources_use_source_tree(self):
        # Both roots have identically named but different inputs.
        for name in source_build.GUEST_FILES:
            relative = Path(name) if name in source_build.ROOT_GUEST_FILES else Path('source_supervisor') / name
            for root, content in ((self.source, b'maintained'), (self.run, b'historical')):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(content + name.encode())
        for root, content in ((self.source, b'maintained'), (self.run, b'historical')):
            native = root / 'source_supervisor/bpf-authority/native'
            native.mkdir(parents=True)
            for name in ('Makefile', 'policy.c', 'abi.h', 'g2_bpf_guard.c'):
                (native / name).write_bytes(content + name.encode())
        pg = self.run / 'postgres.tar'
        pg.write_bytes(b'postgres')
        selection = self.run / 'browser.json'
        selection.write_bytes(b'selection')
        archive = self.run / 'browser.tar'
        archive.write_bytes(b'archive')
        attempt = self.run / 'attempt'
        attempt.mkdir()
        frozen = SimpleNamespace(DEBS={}, load_json=lambda p: json.loads(p.read_bytes()))
        with mock.patch.object(host, 'PG_TAR', pg):
            state = {}
            source_build._write_seed(host, frozen, {}, {}, state, attempt,
                                     {'selection_path': selection, 'image_archive_path': archive})
        self.assertEqual((attempt / 'seed/source_guest.py').read_bytes(), b'maintainedsource_guest.py')
        self.assertEqual(state['seed_inputs']['source_guest.py'], hashlib.sha256(b'maintainedsource_guest.py').hexdigest())
        self.assertEqual(source_bpf_build.native_sources(host)['policy.c'], hashlib.sha256(b'maintainedpolicy.c').hexdigest())
        self.assertFalse((self.source / 'seed').exists())
        for name in source_build.CARRIER_FILES:
            self.assertEqual((attempt / 'seed' / name).read_bytes(), (self.phase / name).read_bytes())

    def test_missing_historical_input_is_not_synthesized(self):
        frozen = SimpleNamespace(prepare_import=SimpleNamespace(gate=lambda _: (_ for _ in ()).throw(FileNotFoundError('historical input'))))
        (self.source / 'prepare.json').write_bytes(b'not historical evidence')
        with self.assertRaises(FileNotFoundError):
            source_build._selected(host, frozen)
        self.assertFalse((self.run / 'prepare.json').exists())

    def test_bad_phase_prevents_attempt_or_selection(self):
        fixture = RetirementFixture(self.run, last=1)
        fixture.host.validate_source_layout = host.validate_source_layout
        attempts = set((self.run / 'source-build-attempts').iterdir())
        selections = set((self.run / 'source-build-selections').iterdir())
        (self.phase / 'host.py').write_bytes(b'changed')
        with self.assertRaises(host.Failure):
            source_build.build(fixture.host, fixture.frozen, {})
        self.assertEqual(set((self.run / 'source-build-attempts').iterdir()), attempts)
        self.assertEqual(set((self.run / 'source-build-selections').iterdir()), selections)
    def test_missing_audit_does_not_become_rebuild(self):
        fixture = RetirementFixture(self.run, last=1)
        fixture.host.validate_source_layout = host.validate_source_layout
        attempts = set((self.run / 'source-build-attempts').iterdir())
        selections = set((self.run / 'source-build-selections').iterdir())
        with mock.patch.object(source_build, '_validate_current', side_effect=host.Failure('old runner drift')), \
             mock.patch.object(source_bpf_build, 'AUDIT_SOURCE', self.run / 'missing-audit'):
            with self.assertRaises(FileNotFoundError):
                source_build.build(fixture.host, fixture.frozen, {})
        self.assertEqual(set((self.run / 'source-build-attempts').iterdir()), attempts)
        self.assertEqual(set((self.run / 'source-build-selections').iterdir()), selections)


    def test_historical_selection_and_current_runner_drift(self):
        fixture = RetirementFixture(self.run, last=1)
        fixture.host.validate_source_layout = host.validate_source_layout
        state = source_build._selected(fixture.host, fixture.frozen)
        self.assertEqual(state['runner_inputs']['source_host.py'], host.digest(self.run / 'source_host.py')[0])
        browser = {'selection': {'image_archive_sha256': 'archive'}, 'selection_sha256': 'selection'}
        state.update(boot_inputs={}, bpf_authority={'boot_derivation': {'native_toolchain': 'tools'}, 'native_sources': {}},
                     seed_toolchain='seed', browser_selection={'selection': browser['selection'], 'selection_sha256': 'selection'})
        state['seed_inputs'].update({'browser-selection.json': 'selection', 'browser-image.tar': 'archive'})
        frozen = SimpleNamespace(task_environment=lambda: {})
        with mock.patch.object(source_boot, 'native_toolchain', return_value='tools'), \
             mock.patch.object(source_bpf_build, 'native_sources', return_value={}), \
             mock.patch.object(source_bpf_build, 'AUDIT_SOURCE', self.run / 'prepare.json'), \
             mock.patch.object(source_bpf_build, 'AUDIT_SOURCE_SHA256', host.digest(self.run / 'prepare.json')[0]), \
             mock.patch.object(source_build, 'seed_toolchain', return_value='seed'), \
             mock.patch.object(source_build, '_selected_browser', return_value=browser), \
             mock.patch.object(source_build, 'BINARIES', ('tool',)), \
             mock.patch.object(source_build, 'GUEST_FILES', ()):
            with self.assertRaisesRegex(host.Failure, 'runner source changed'):
                source_build._validate_current(fixture.host, frozen, state)
            with mock.patch.object(source_build, '_selected', return_value=state):
                with self.assertRaisesRegex(host.Failure, 'runner source changed'):
                    source_build.build_gate(fixture.host, frozen)
            state['runner_inputs']['source_host.py'] = host.digest(fixture.host.SOURCE_ROOT / 'source_host.py')[0]
            with mock.patch.object(source_build, '_command_names', side_effect=RuntimeError('runner hash accepted')):
                with self.assertRaisesRegex(RuntimeError, 'runner hash accepted'):
                    source_build._validate_current(fixture.host, frozen, state)

    def test_old_browser_receipt_rejected(self):
        builder = self.source / 'browser_negative.py'
        builder.write_bytes(b'new builder')
        selection_path = self.run / 'selection'
        selection_path.write_bytes(b'selection')
        image = self.run / 'image'
        image.write_bytes(b'image')
        receipt = self.run / 'receipt'
        receipt.write_text(json.dumps({'builder_sha256': hashlib.sha256(b'old builder').hexdigest()}))
        selection = {'version': 1, 'run_token': host.TOKEN, 'program_sha256': browser_negative.PROGRAM_SHA256,
                     'image_id': 'sha256:' + 'a' * 64, 'image_archive_sha256': host.digest(image)[0]}
        selected = {'selection': selection, 'selection_path': str(selection_path),
                    'selection_sha256': host.digest(selection_path)[0], 'manifest_path': '',
                    'receipt_path': str(receipt), 'image_archive_path': str(image)}
        with mock.patch.object(browser_negative, 'selected_browser', return_value=selected):
            with self.assertRaisesRegex(host.Failure, 'selected browser image identity differs'):
                source_build._selected_browser(host)


if __name__ == '__main__':
    unittest.main()
