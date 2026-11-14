"""Real-file copy, clone, and identity regression tests; no VM required."""
import errno
import hashlib
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest
from unittest import mock

import source_build
import source_host as host


class CopyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root / 'source'
        self.target = self.root / 'target'
        self.data = os.urandom(2 * 1024 * 1024 + 137)
        self.source.write_bytes(self.data)
        self.source.chmod(0o640)

    def copy(self, mode=None):
        return source_build._copy(host, self.source, self.target, mode)

    def test_clone_content_identity_mode_and_shared_extents(self):
        digest = self.copy(0o600)
        self.assertEqual(digest, hashlib.sha256(self.data).hexdigest())
        self.assertEqual(self.target.read_bytes(), self.data)
        source_stat, target_stat = self.source.stat(), self.target.stat()
        self.assertNotEqual((source_stat.st_dev, source_stat.st_ino), (target_stat.st_dev, target_stat.st_ino))
        self.assertEqual((source_stat.st_nlink, target_stat.st_nlink), (1, 1))
        self.assertEqual(stat.S_IMODE(target_stat.st_mode), 0o600)
        # FIEMAP reports actual shared extents; allocation counts alone cannot prove CoW.
        for path in (self.source, self.target):
            result = subprocess.run(['/usr/bin/filefrag', '-v', str(path)], capture_output=True, text=True, check=True)
            self.assertIn('shared', result.stdout, result.stdout)

    def test_target_write_does_not_change_source(self):
        self.copy()
        self.assertEqual(stat.S_IMODE(self.target.stat().st_mode), 0o640)
        with self.target.open('r+b') as stream:
            stream.write(b'target changed')
        self.assertEqual(self.source.read_bytes(), self.data)
        self.assertNotEqual(self.target.read_bytes(), self.data)

    def test_source_write_does_not_change_target(self):
        self.copy()
        with self.source.open('r+b') as stream:
            stream.write(b'source changed')
        self.assertEqual(self.target.read_bytes(), self.data)
        self.assertNotEqual(self.source.read_bytes(), self.data)

    def test_only_supported_fallback_errors_copy_after_truncation(self):
        for error in (errno.EXDEV, errno.EOPNOTSUPP, errno.ENOTTY, errno.EINVAL):
            target = self.root / ('fallback-%d' % error)
            def unsupported(dst, request, src):
                self.assertEqual(request, 0x40049409)
                os.write(dst, b'partial clone output')
                raise OSError(error, 'injected unsupported clone')
            with self.subTest(error=error), mock.patch.object(source_build.fcntl, 'ioctl', side_effect=unsupported) as ioctl:
                digest = source_build._copy(host, self.source, target)
                self.assertEqual(ioctl.call_count, 1)
                self.assertEqual(target.read_bytes(), self.data)
                self.assertEqual(digest, hashlib.sha256(self.data).hexdigest())
                self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o640)

    def test_other_clone_errors_propagate_and_preserve_partial_target(self):
        for error in (errno.ENOSPC, errno.EIO, errno.EPERM):
            target = self.root / ('failed-%d' % error)
            def fail(dst, request, src):
                os.write(dst, b'partial')
                raise OSError(error, 'injected clone failure')
            with self.subTest(error=error), mock.patch.object(source_build.fcntl, 'ioctl', side_effect=fail):
                with self.assertRaises(OSError) as raised:
                    source_build._copy(host, self.source, target)
                self.assertEqual(raised.exception.errno, error)
                self.assertEqual(target.read_bytes(), b'partial')

    def test_small_file_does_not_clone(self):
        self.source.write_bytes(b'small')
        with mock.patch.object(source_build.fcntl, 'ioctl', side_effect=AssertionError('unexpected clone')):
            self.assertEqual(self.copy(), hashlib.sha256(b'small').hexdigest())
        self.assertEqual(self.target.read_bytes(), b'small')

    def test_existing_target_is_not_overwritten(self):
        self.target.write_bytes(b'existing')
        with self.assertRaises(FileExistsError):
            self.copy()
        self.assertEqual(self.target.read_bytes(), b'existing')

    def test_source_change_during_read_is_rejected(self):
        source_identity = (self.source.stat().st_dev, self.source.stat().st_ino)
        real_read = os.read
        changed = False
        def changing_read(fd, size):
            nonlocal changed
            data = real_read(fd, size)
            info = os.fstat(fd)
            if not changed and data and (info.st_dev, info.st_ino) == source_identity:
                changed = True
                with self.source.open('r+b') as stream:
                    stream.write(b'changed')
            return data
        with mock.patch.object(source_build.os, 'read', side_effect=changing_read):
            with self.assertRaises(host.Failure):
                self.copy()
        self.assertTrue(changed)
        self.assertTrue(self.target.exists())

    def test_clone_with_changed_target_contents_is_rejected(self):
        real_ioctl = source_build.fcntl.ioctl
        def corrupt(dst, request, src):
            result = real_ioctl(dst, request, src)
            os.pwrite(dst, b'corrupted', 0)
            return result
        with mock.patch.object(source_build.fcntl, 'ioctl', side_effect=corrupt):
            with self.assertRaises(host.Failure):
                self.copy()
        self.assertTrue(self.target.exists())
        self.assertEqual(self.source.read_bytes(), self.data)

    def test_replaced_target_name_is_rejected(self):
        real_ioctl = source_build.fcntl.ioctl
        detached = self.root / 'detached'
        def replace(dst, request, src):
            result = real_ioctl(dst, request, src)
            os.rename(self.target, detached)
            self.target.write_bytes(self.data)
            return result
        with mock.patch.object(source_build.fcntl, 'ioctl', side_effect=replace):
            with self.assertRaises(host.Failure):
                self.copy()
        self.assertEqual(self.target.read_bytes(), self.data)
        self.assertTrue(detached.exists())

    def test_replaced_source_name_is_rejected(self):
        real_ioctl = source_build.fcntl.ioctl
        detached = self.root / 'source-detached'
        def replace(dst, request, src):
            result = real_ioctl(dst, request, src)
            os.rename(self.source, detached)
            self.source.write_bytes(self.data)
            return result
        with mock.patch.object(source_build.fcntl, 'ioctl', side_effect=replace):
            with self.assertRaises(host.Failure):
                self.copy()
        self.assertEqual(self.source.read_bytes(), self.data)
        self.assertTrue(detached.exists())


if __name__ == '__main__':
    unittest.main()
