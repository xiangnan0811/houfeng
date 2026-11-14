"""Managed containerd installation against isolated tmpfs paths."""
import errno
import collections
import hashlib
import os
from pathlib import Path
import shutil
import stat
import sys
import unittest
from unittest import mock

R = Path(__file__).resolve().parent
sys.path[:0] = [str(R / 'source_supervisor'),
                str(R.parent / 'phase0')]
import guest_bootstrap as bootstrap
from journal import JournalError


class ManagedContainerdInstallTests(unittest.TestCase):
    def setUp(self):
        self.assertEqual((os.geteuid(), os.getegid()), (0, 0))
        mounts = [line.split(' - ', 1) for line in Path('/proc/self/mountinfo').read_text().splitlines()]
        for path in ('/opt', '/mnt'):
            self.assertEqual([right.split()[0] for left, right in mounts if left.split()[4] == path][-1], 'tmpfs')
        self.root = Path('/opt/g2')
        self.seed = Path('/mnt/g2-seed')
        self.assertTrue(self.root.is_dir())
        self.assertTrue(self.seed.is_dir())
        self.assertFalse((self.root / 'containerd-bin').exists())
        self.payload = b'managed-containerd-owned-fixture\n'
        self.digest = hashlib.sha256(self.payload).hexdigest()
        (self.seed / 'g2-bpf-authority').write_bytes(self.payload)
        for name in ('g2-bpf-authority', 'g2-bpf-runtime'):
            path = self.root / name
            path.write_bytes(self.payload)
            path.chmod(0o500)
        self.addCleanup(self.cleanup_fixture)

    def cleanup_fixture(self):
        directory = self.root / 'containerd-bin'
        if directory.is_symlink():
            directory.unlink()
        elif directory.exists():
            directory.chmod(0o700)
            shutil.rmtree(directory)
        for path in (self.root / 'g2-bpf-authority', self.root / 'g2-bpf-runtime', self.seed / 'g2-bpf-authority'):
            path.unlink(missing_ok=True)

    def test_install_distinct_files_and_mode(self):
        bootstrap._install_managed_containerd(self.digest)
        directory = self.root / 'containerd-bin'
        self.assertEqual(stat.S_IMODE(directory.stat().st_mode), 0o500)
        paths = [self.root / 'g2-bpf-authority', self.root / 'g2-bpf-runtime', directory / 'containerd']
        identities = set()
        for path in paths:
            st = path.stat(follow_symlinks=False)
            self.assertEqual((st.st_uid, st.st_gid, stat.S_IMODE(st.st_mode), st.st_nlink), (0, 0, 0o500, 1))
            self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), self.digest)
            identities.add((st.st_dev, st.st_ino))
        self.assertEqual(len(identities), 3)

    def test_existing_directory_and_symlink_preserved(self):
        directory = self.root / 'containerd-bin'
        for symlink in (False, True):
            if symlink:
                directory.symlink_to('/does-not-exist')
            else:
                directory.mkdir(mode=0o700)
            before = directory.lstat()
            with self.assertRaises(FileExistsError):
                bootstrap._install_managed_containerd(self.digest)
            after = directory.lstat()
            self.assertEqual((before.st_dev, before.st_ino, before.st_mode), (after.st_dev, after.st_ino, after.st_mode))
            directory.unlink() if symlink else directory.rmdir()

    def test_digest_and_readback_failures_preserve_partial(self):
        directory = self.root / 'containerd-bin'
        real_open, real_close = os.open, os.close
        opened, closed = [], []
        def capture_open(*args, **kwargs):
            fd = real_open(*args, **kwargs)
            opened.append(fd)
            return fd
        def capture_close(fd):
            closed.append(fd)
            return real_close(fd)
        def failed_install(expected):
            opened.clear()
            closed.clear()
            with mock.patch.object(bootstrap.os, 'open', side_effect=capture_open), \
                    mock.patch.object(bootstrap.os, 'close', side_effect=capture_close):
                with self.assertRaises(JournalError):
                    bootstrap._install_managed_containerd(expected)
            self.assertTrue(opened)
            self.assertEqual(collections.Counter(opened), collections.Counter(closed))
            self.assertTrue((directory / 'containerd').is_file())
        failed_install('0' * 64)
        shutil.rmtree(directory)
        original = bootstrap._digest
        def wrong_readback(path):
            if path == str(directory / 'containerd'):
                return '0' * 64
            return original(path)
        with mock.patch.object(bootstrap, '_digest', side_effect=wrong_readback):
            failed_install(self.digest)

    def test_target_open_failure_closes_owned_source(self):
        source = str(self.seed / 'g2-bpf-authority')
        target = str(self.root / 'existing')
        Path(target).write_bytes(b'preserve')
        opened = []
        original = os.open
        def capture(path, flags, *args, **kwargs):
            fd = original(path, flags, *args, **kwargs)
            if path == source:
                opened.append(fd)
            return fd
        try:
            with mock.patch.object(bootstrap.os, 'open', side_effect=capture):
                with self.assertRaises(FileExistsError):
                    bootstrap._install_file(source, target, self.digest, 0o500, 0)
            self.assertEqual(len(opened), 1)
            with self.assertRaises(OSError) as caught:
                os.fstat(opened[0])
            self.assertEqual(caught.exception.errno, errno.EBADF)
            self.assertEqual(Path(target).read_bytes(), b'preserve')
        finally:
            Path(target).unlink()


if __name__ == '__main__':
    unittest.main()
