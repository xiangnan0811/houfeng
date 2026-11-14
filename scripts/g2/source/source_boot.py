"""Derive fixed-kernel build inputs from the prepare-bound Ubuntu image.

This module is deliberately independent of source_build's receipt and selection
logic.  Call derive() only after its preparation gate.  Nothing here mounts a
filesystem or opens a historical VM disk.
"""
import argparse
import ctypes
import ctypes.util
import hashlib
import json
import lzma
import os
from pathlib import Path
import re
import selectors
import shutil
import stat
import struct
import subprocess
import sys
import time
import zlib

RELEASE = '6.8.0-147-generic'
VERSION = '6.8.0-147.147'
BASE_SHA256 = '04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94'
QEMU_SHA256 = '22d0a60105357cffd6c4fdbc1e8458cd29f36bbc5c7d5abe61c5ec03a51bb993'
ROOTS = ('/usr/src/linux-headers-6.8.0-147', '/usr/src/linux-headers-6.8.0-147-generic')
PACKAGES = ('linux-headers-6.8.0-147', 'linux-headers-6.8.0-147-generic',
            'linux-image-6.8.0-147-generic', 'linux-modules-6.8.0-147-generic')
NAME = re.compile(r'[A-Za-z0-9_.+@,=-]+\Z')
MAX_META = 32 * 1024 * 1024
MAX_INITRD = 512 * 1024 * 1024
RUST_LINK = '/usr/src/linux-headers-6.8.0-147-generic/rust'
RUST_TARGET = '../linux-lib-rust-6.8.0-147-generic/rust'
RUST_ABSENT_PARENT = 'linux-lib-rust-6.8.0-147-generic'
RUST_RESOLVED = '/usr/src/' + RUST_ABSENT_PARENT + '/rust'


class PrerequisiteError(Exception):
    pass


class _ValidatedHeaders(dict):
    def __init__(self, entries, image_identity):
        super().__init__(entries)
        self.image_identity = image_identity


class BootInputs:
    """Result metadata and held, read-only kernel/initrd file descriptions."""
    def __init__(self, metadata):
        self.metadata = metadata
        self.kernel_fd = None
        self.initrd_fd = None
        try:
            for name in ('kernel', 'initrd'):
                record = metadata[name]
                fd = _open_regular(record['path'])
                setattr(self, name + '_fd', fd)
                st = os.fstat(fd)
                _need((st.st_dev, st.st_ino, st.st_uid, st.st_gid, stat.S_IMODE(st.st_mode), st.st_nlink,
                       st.st_size, st.st_mtime_ns, st.st_ctime_ns) ==
                      tuple(record[k] for k in ('dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'size',
                                                'mtime_ns', 'ctime_ns')) and
                      _hash_fd(fd) == record['sha256'], 'boot artifact changed before hold: ' + name)
        except BaseException:
            self.close()
            raise

    def close(self):
        for name in ('kernel_fd', 'initrd_fd'):
            fd = getattr(self, name)
            if fd is not None:
                os.close(fd)
                setattr(self, name, None)

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, traceback):
        self.close()


def _need(value, message):
    if not value:
        raise PrerequisiteError(message)


def _identity(st):
    return (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_nlink,
            st.st_size, st.st_mtime_ns, st.st_ctime_ns)


def _hash_fd(fd, *, single_link=True):
    before = os.fstat(fd)
    _need(stat.S_ISREG(before.st_mode) and (before.st_nlink == 1 if single_link else before.st_nlink > 0), 'unsafe input file')
    h = hashlib.sha256()
    pos = 0
    while pos < before.st_size:
        data = os.pread(fd, min(1024 * 1024, before.st_size - pos), pos)
        _need(bool(data), 'short input read')
        h.update(data)
        pos += len(data)
    _need(_identity(os.fstat(fd)) == _identity(before), 'input changed while hashing')
    return h.hexdigest()


def _safe_ancestors(path):
    for parent in (path.parent, *path.parent.parents):
        info = os.lstat(parent)
        _need(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, os.geteuid()) and
              info.st_mode & 0o022 == 0, 'unsafe path ancestor: ' + str(parent))


def _open_regular(path, *, single_link=True):
    path = Path(path)
    _need(path.is_absolute() and '..' not in path.parts, 'input path must be absolute')
    _safe_ancestors(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        st = os.fstat(fd)
        _need(stat.S_ISREG(st.st_mode) and (st.st_nlink == 1 if single_link else st.st_nlink > 0) and
              _identity(st) == _identity(os.stat(path, follow_symlinks=False)), 'unsafe named input')
        return fd
    except BaseException:
        os.close(fd)
        raise


def _tool(path, expected=None):
    path = Path(os.path.realpath(path))
    fd = _open_regular(path, single_link=expected is not None)
    try:
        st = os.fstat(fd)
        _need(st.st_uid == (os.geteuid() if expected is not None else 0) and st.st_mode & 0o022 == 0,
              'unsafe tool: ' + str(path))
        digest = _hash_fd(fd, single_link=expected is not None)
        if expected is not None:
            _need(digest == expected, 'tool hash differs: ' + str(path))
        return {'path': str(path), 'sha256': digest, 'size': st.st_size}
    finally:
        os.close(fd)


def _run(args, *, env, data=None, pass_fds=(), timeout=300, maximum=MAX_META):
    input_fd = None
    proc = None
    selector = selectors.DefaultSelector()
    output = [bytearray(), bytearray()]
    try:
        if data is not None:
            input_fd = os.memfd_create('g2-debugfs-input', os.MFD_CLOEXEC)
            remaining = memoryview(data)
            while remaining:
                n = os.write(input_fd, remaining)
                _need(n > 0, 'short tool input write')
                remaining = remaining[n:]
            os.lseek(input_fd, 0, os.SEEK_SET)
        proc = subprocess.Popen(args, stdin=input_fd if input_fd is not None else subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, pass_fds=pass_fds)
        for index, stream in enumerate((proc.stdout, proc.stderr)):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, index)
        deadline = time.monotonic() + timeout
        while selector.get_map():
            remaining = deadline - time.monotonic()
            _need(remaining > 0, 'tool timed out: ' + str(args[0]))
            for key, _ in selector.select(remaining):
                chunk = os.read(key.fileobj.fileno(), 65536)
                if not chunk:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                output[key.data].extend(chunk)
                _need(len(output[key.data]) <= (maximum if key.data == 0 else 65536), 'tool output exceeds bound')
        remaining = deadline - time.monotonic()
        _need(remaining > 0, 'tool timed out: ' + str(args[0]))
        proc.wait(timeout=remaining)
        _need(proc.returncode == 0, 'tool failed: ' + str(args[0]) + ': ' +
              bytes(output[1]).decode('utf-8', 'replace')[-512:])
        return bytes(output[0]), bytes(output[1])
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise PrerequisiteError('tool execution failed: ' + str(args[0])) from exc
    finally:
        selector.close()
        if proc is not None:
            if proc.poll() is None:
                proc.kill()
            proc.wait()
            for stream in (proc.stdout, proc.stderr):
                if stream is not None:
                    stream.close()
        if input_fd is not None:
            os.close(input_fd)


def _tool_libraries(tool, env, libraries):
    output, error = _run(['/usr/bin/ldd', tool['path']], env=env, timeout=30, maximum=65536)
    _need(not error and b'not found' not in output, 'tool library resolution failed: ' + tool['path'])
    paths = []
    for line in output.decode('utf-8', 'strict').splitlines():
        match = re.search(r'(?<!\S)(/[^\s]+)', line)
        if match:
            path = os.path.realpath(match.group(1))
            record = _tool(path)
            if path in libraries:
                _need(libraries[path] == record['sha256'], 'tool dependency changed')
            libraries[path] = record['sha256']
            paths.append(path)
    _need(paths, 'tool has no identifiable shared libraries: ' + tool['path'])
    return sorted(set(paths))


def native_toolchain(env):
    """Fingerprint the fixed native build and package-inspection toolchain.

    Call again with the same environment after the build and compare the
    complete returned value to detect a tool, dependency, or selection change.
    """
    names = ('gcc', 'clang', 'make', 'ld', 'as', 'objcopy', 'nm', 'readelf', 'strip', 'dpkg-deb')
    tools = {}
    for name in names:
        path = '/usr/bin/' + name
        tools[name] = _tool(path)
        version, stderr = _run([path, '--version'], env=env, maximum=65536, timeout=30)
        lines = (version + stderr).decode('utf-8', 'strict').splitlines()
        _need(bool(lines), 'tool version unavailable: ' + name)
        tools[name]['version'] = lines[0][:512]

    def gcc_program(name):
        out, err = _run(['/usr/bin/gcc', '-print-prog-name=' + name], env=env, maximum=4096, timeout=30)
        lines = out.decode('utf-8', 'strict').splitlines()
        _need(not err and len(lines) == 1 and lines[0] and '\0' not in lines[0],
              'GCC program selection unavailable: ' + name)
        selected = lines[0]
        if not os.path.isabs(selected):
            _need('/' not in selected, 'relative GCC program path: ' + name)
            selected = shutil.which(selected, path=env.get('PATH', os.defpath))
        _need(selected is not None and os.path.isabs(selected), 'GCC program unavailable: ' + name)
        return _tool(selected)

    tools['gcc-cc1'] = gcc_program('cc1')
    for name in ('as', 'ld'):
        selected = gcc_program(name)
        _need(selected == {key: tools[name][key] for key in ('path', 'sha256', 'size')},
              'GCC selected a different ' + name + ' than the fixed build tool')
    libraries = {}
    dependencies = {name: _tool_libraries(tool, env, libraries) for name, tool in tools.items()}
    for name, tool in tools.items():
        actual = _tool(tool['path'])
        _need(all(actual[key] == tool[key] for key in ('path', 'sha256', 'size')),
              'native tool changed during fingerprint: ' + name)
    for path, sha in libraries.items():
        _need(_tool(path)['sha256'] == sha, 'native tool library changed during fingerprint: ' + path)
    return {'tools': tools, 'tool_libraries': dependencies, 'libraries': libraries}


def _sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _create(path, size=None):
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    if size is not None:
        os.ftruncate(fd, size)
    return fd


def _seal(fd, path):
    os.fsync(fd)
    os.fchmod(fd, 0o400)
    os.fsync(fd)
    _sync_dir(Path(path).parent)
    st = os.fstat(fd)
    _need(_identity(st) == _identity(os.stat(path, follow_symlinks=False)), 'output identity changed')
    sha = _hash_fd(fd)
    readfd = _open_regular(path)
    try:
        _need(_identity(os.fstat(readfd)) == _identity(st) and _hash_fd(readfd) == sha,
              'output readback differs')
    finally:
        os.close(readfd)
    return {'path': str(path), 'dev': st.st_dev, 'ino': st.st_ino, 'uid': st.st_uid,
            'gid': st.st_gid, 'mode': stat.S_IMODE(st.st_mode), 'nlink': st.st_nlink,
            'size': st.st_size, 'mtime_ns': st.st_mtime_ns, 'ctime_ns': st.st_ctime_ns,
            'sha256': sha}


def _pread(fd, offset, length):
    data = os.pread(fd, length, offset)
    _need(len(data) == length, 'short disk read')
    return data


def _gpt(fd):
    size = os.fstat(fd).st_size
    _need(size >= 68 * 512 and size % 512 == 0, 'invalid disk geometry')
    sectors = size // 512
    mbr = _pread(fd, 0, 512)
    _need(mbr[510:512] == b'\x55\xaa' and mbr[450] == 0xee, 'protective MBR missing')

    def header(lba):
        raw = _pread(fd, lba * 512, 512)
        _need(raw[:8] == b'EFI PART', 'GPT signature missing')
        revision, length, checksum, reserved = struct.unpack_from('<IIII', raw, 8)
        _need(revision == 0x10000 and 92 <= length <= 512 and reserved == 0, 'invalid GPT header')
        body = bytearray(raw[:length])
        body[16:20] = b'\0' * 4
        _need(zlib.crc32(body) & 0xffffffff == checksum, 'GPT header CRC mismatch')
        current, alternate, first, last = struct.unpack_from('<QQQQ', raw, 24)
        guid = raw[56:72]
        entries_lba, count, entry_size, entries_crc = struct.unpack_from('<QIII', raw, 72)
        _need(current == lba and 0 < count <= 16384 and 128 <= entry_size <= 4096 and
              entry_size % 8 == 0 and first <= last < sectors and guid != bytes(16), 'invalid GPT fields')
        n = count * entry_size
        _need(n <= 16 * 1024 * 1024 and entries_lba * 512 + n <= size, 'GPT entries out of bounds')
        entries = _pread(fd, entries_lba * 512, n)
        _need(zlib.crc32(entries) & 0xffffffff == entries_crc, 'GPT entries CRC mismatch')
        return (alternate, first, last, guid, entries_lba, count, entry_size, entries)

    primary, backup = header(1), header(sectors - 1)
    _need(primary[0] == sectors - 1 and backup[0] == 1 and primary[1:4] == backup[1:4] and
          primary[5:] == backup[5:], 'GPT copies differ')
    count, entry_size, entries = primary[5:]
    p_end = primary[4] + (len(entries) + 511) // 512
    b_end = backup[4] + (len(entries) + 511) // 512
    _need(2 <= primary[4] < p_end <= primary[1] and primary[2] < backup[4] < b_end <= sectors - 1,
          'GPT metadata overlaps usable space')
    parts = []
    uuids = set()
    for i in range(count):
        ent = entries[i * entry_size:(i + 1) * entry_size]
        if ent[:16] == bytes(16):
            _need(ent == bytes(entry_size), 'nonzero unused GPT entry')
            continue
        ident = ent[16:32]
        start, end = struct.unpack_from('<QQ', ent, 32)
        _need(ident != bytes(16) and ident not in uuids and primary[1] <= start <= end <= primary[2],
              'invalid GPT partition extent or identity')
        uuids.add(ident)
        parts.append({'index': i + 1, 'start': start * 512, 'size': (end - start + 1) * 512,
                      'type_guid': ent[:16].hex(), 'guid': ident.hex()})
    ordered = sorted(parts, key=lambda p: p['start'])
    _need(all(a['start'] + a['size'] <= b['start'] for a, b in zip(ordered, ordered[1:])),
          'GPT partitions overlap')
    return parts


def _copy_partition(disk, part, path):
    fd = _create(path, part['size'])
    try:
        pos = 0
        while pos < part['size']:
            block = _pread(disk, part['start'] + pos, min(1024 * 1024, part['size'] - pos))
            if any(block):
                done = 0
                while done < len(block):
                    n = os.pwrite(fd, block[done:], pos + done)
                    _need(n > 0, 'short partition write')
                    done += n
            pos += len(block)
        os.fsync(fd)
    finally:
        os.close(fd)
    _sync_dir(Path(path).parent)


def _debug(image, commands, env, pass_fds=(), maximum=MAX_META):
    _need(all('\n' not in cmd and '\r' not in cmd for cmd in commands), 'invalid debugfs command')
    data = ('\n'.join(commands) + '\n').encode('ascii')
    out, err = _run(['/usr/bin/debugfs', '-f', '/dev/stdin', str(image)], env=env, data=data,
                    pass_fds=pass_fds, timeout=900, maximum=maximum)
    _need(re.fullmatch(rb'debugfs [^\n]+\n', err) is not None, 'debugfs reported an error')
    text = out.decode('utf-8', 'strict')
    chunks = []
    cursor = 0
    for command in commands:
        marker = 'debugfs: ' + command + '\n'
        _need(text.startswith(marker, cursor), 'debugfs response framing differs')
        cursor += len(marker)
        if text.startswith('debugfs: ', cursor):
            end = cursor
        else:
            next_marker = text.find('\ndebugfs: ', cursor)
            end = len(text) if next_marker == -1 else next_marker + 1
        chunks.append(text[cursor:end])
        cursor = end
    _need(cursor == len(text), 'unexpected debugfs response')
    return chunks


def _listing(image, paths, env):
    result = {}
    for start in range(0, len(paths), 64):
        group = paths[start:start + 64]
        chunks = _debug(image, ['ls -p ' + p for p in group], env)
        for parent, chunk in zip(group, chunks):
            entries = {}
            for line in chunk.splitlines():
                if not line:
                    continue
                fields = line.split('/')
                _need(len(fields) == 8 and fields[0] == '' and fields[-1] == '' and
                      fields[1].isdigit() and re.fullmatch(r'[0-7]{6}', fields[2]) and
                      fields[3].isdigit() and fields[4].isdigit(), 'invalid debugfs directory entry')
                name = fields[5]
                if name in ('.', '..'):
                    continue
                _need(NAME.fullmatch(name) is not None and name not in entries, 'unsafe directory entry name')
                mode = int(fields[2], 8)
                kind = stat.S_IFMT(mode)
                _need(kind in (stat.S_IFREG, stat.S_IFDIR, stat.S_IFLNK), 'unsupported inode type')
                _need(fields[3] == '0' and fields[4] == '0' and
                      (kind == stat.S_IFLNK or mode & 0o022 == 0), 'unsafe inode ownership or mode')
                _need(fields[6] == '' if kind == stat.S_IFDIR else fields[6].isdigit(), 'invalid inode size')
                entries[name] = {'inode': int(fields[1]), 'mode': mode,
                                 'size': None if kind == stat.S_IFDIR else int(fields[6])}
            result[parent] = entries
    return result


def _cat(image, path, env, maximum=MAX_META):
    _need(path.startswith('/') and all(NAME.fullmatch(p) for p in path.split('/')[1:]), 'unsafe image path')
    out, err = _run(['/usr/bin/debugfs', '-R', 'cat ' + path, str(image)], env=env, maximum=maximum)
    _need(re.fullmatch(rb'debugfs [^\n]+\n', err) is not None, 'debugfs cat failed: ' + path)
    return out


def _packages(image, env):
    status = _cat(image, '/var/lib/dpkg/status', env).decode('utf-8', 'strict')
    installed = {}
    for paragraph in status.split('\n\n'):
        fields = {}
        for line in paragraph.splitlines():
            if line.startswith((' ', '\t')):
                continue
            if ': ' in line:
                k, v = line.split(': ', 1)
                _need(k not in fields, 'duplicate dpkg status field')
                fields[k] = v
        name = fields.get('Package')
        if name in PACKAGES:
            source = 'linux-signed' if name == 'linux-image-6.8.0-147-generic' else 'linux'
            _need(name not in installed and fields.get('Status') == 'install ok installed' and
                  fields.get('Version') == VERSION and fields.get('Source') == source,
                  'kernel package status differs: ' + name)
            installed[name] = fields
    _need(set(installed) == set(PACKAGES), 'matching kernel packages unavailable')
    inventories = {}
    for name in PACKAGES:
        body = _cat(image, '/var/lib/dpkg/info/' + name + '.list', env)
        rows = body.decode('utf-8', 'strict').splitlines()
        _need(len(rows) == len(set(rows)), 'duplicate package inventory entry')
        inventories[name] = rows
    return inventories


def _python_executable(image, env):
    link = _debug(image, ['stat /usr/bin/python3'], env)[0]
    _need(re.search(r'^Inode: \d+\s+Type: symlink\s+Mode:\s+0777', link, re.M) is not None and
          re.search(r'^User:\s+0\s+Group:\s+0\b', link, re.M) is not None, 'unsafe base Python selector')
    match = re.search(r'^Fast link dest: "(python3\.[0-9]+)"$', link, re.M)
    _need(match is not None, 'base python3 is not the expected relative versioned symlink')
    path = '/usr/bin/' + match[1]
    details = _debug(image, ['stat ' + path], env)[0]
    metadata = re.search(r'^Inode: (\d+)\s+Type: regular\s+Mode:\s+([0-7]+)', details, re.M)
    owner = re.search(r'^User:\s+(\d+)\s+Group:\s+(\d+).*?Size:\s+(\d+)', details, re.M)
    _need(metadata and owner and owner[1] == owner[2] == '0' and
          int(metadata[2], 8) & 0o111 and not int(metadata[2], 8) & 0o022 and
          0 < int(owner[3]) <= MAX_META, 'unsafe base Python executable')
    content = _cat(image, path, env)
    _need(len(content) == int(owner[3]), 'base Python executable size differs')
    status = _cat(image, '/var/lib/dpkg/status', env).decode('utf-8', 'strict')
    candidates = []
    for paragraph in status.split('\n\n'):
        fields = {}
        for line in paragraph.splitlines():
            if not line.startswith((' ', '\t')) and ': ' in line:
                key, value = line.split(': ', 1)
                _need(key not in fields, 'duplicate dpkg field')
                fields[key] = value
        name = fields.get('Package', '')
        if re.fullmatch(r'python3(?:\.[0-9]+)?(?:-minimal)?', name) and fields.get('Status') == 'install ok installed':
            _need(fields.get('Version'), 'Python package version unavailable')
            inventory = _cat(image, '/var/lib/dpkg/info/' + name + '.list', env).decode('utf-8', 'strict').splitlines()
            if path in inventory:
                candidates.append((name, fields['Version']))
    _need(len(candidates) == 1, 'base Python package owner is absent or ambiguous')
    return {'package': candidates[0][0], 'version': candidates[0][1], 'path': path,
            'sha256': hashlib.sha256(content).hexdigest()}


def _systemd_executable(image, env):
    path = '/usr/lib/systemd/systemd'
    details = _debug(image, ['stat ' + path], env)[0]
    metadata = re.search(r'^Inode: (\d+)\s+Type: regular\s+Mode:\s+([0-7]+)', details, re.M)
    owner = re.search(r'^User:\s+(\d+)\s+Group:\s+(\d+).*?Size:\s+(\d+)', details, re.M)
    _need(metadata and owner and owner[1] == owner[2] == '0' and
          int(metadata[2], 8) & 0o111 and not int(metadata[2], 8) & 0o022 and
          0 < int(owner[3]) <= MAX_META, 'unsafe original systemd executable')
    content = _cat(image, path, env)
    _need(len(content) == int(owner[3]) and len(content) >= 20 and content[:6] == b'\x7fELF\x02\x01' and
          struct.unpack_from('<H', content, 18)[0] == 62, 'original systemd executable is not x86_64 ELF')
    status = _cat(image, '/var/lib/dpkg/status', env).decode('utf-8', 'strict')
    packages = []
    for paragraph in status.split('\n\n'):
        fields = {}
        for line in paragraph.splitlines():
            if not line.startswith((' ', '\t')) and ': ' in line:
                key, value = line.split(': ', 1)
                _need(key not in fields, 'duplicate dpkg field')
                fields[key] = value
        if fields.get('Package') == 'systemd':
            packages.append(fields)
    _need(len(packages) == 1 and packages[0].get('Status') == 'install ok installed' and
          packages[0].get('Architecture') == 'amd64' and packages[0].get('Version'),
          'original systemd package identity unavailable')
    inventory = _cat(image, '/var/lib/dpkg/info/systemd.list', env).decode('utf-8', 'strict').splitlines()
    _need(len(inventory) == len(set(inventory)) and inventory.count(path) == 1,
          'original systemd executable not in package inventory')
    return {'package': 'systemd', 'version': packages[0]['Version'], 'path': path,
            'sha256': hashlib.sha256(content).hexdigest()}


def _walk_headers(image, inventories, env):
    image_identity = _identity(os.stat(image, follow_symlinks=False))
    _need(stat.S_ISREG(image_identity[2]) and image_identity[5] == 1, 'unsafe header source image')
    expected = set()
    for pkg in PACKAGES[:2]:
        for path in inventories[pkg]:
            if any(path == root or path.startswith(root + '/') for root in ROOTS):
                _need(all(NAME.fullmatch(p) for p in path.split('/')[1:]), 'unsafe package path')
                expected.add(path)
    _need(all(root in expected for root in ROOTS), 'header package roots missing')
    src = _listing(image, ['/usr/src'], env)['/usr/src']
    entries = {}
    for root in ROOTS:
        item = src.get(Path(root).name)
        _need(item is not None and stat.S_ISDIR(item['mode']), 'header package root is not a directory')
        entries[root] = item
    todo = list(ROOTS)
    while todo:
        current, todo = todo[:64], todo[64:]
        for parent, children in _listing(image, current, env).items():
            for name, entry in children.items():
                path = parent + '/' + name
                _need(path not in entries and path in expected, 'unexpected header entry: ' + path)
                entries[path] = entry
                if stat.S_ISDIR(entry['mode']):
                    todo.append(path)
        _need(len(entries) <= 200000, 'header tree exceeds bound')
    _need(set(entries) == expected, 'header package inventory differs from filesystem')
    # stat by inode, so symlink traversal never selects another object.
    paths = sorted(entries)
    for start in range(0, len(paths), 64):
        group = paths[start:start + 64]
        chunks = _debug(image, ['stat <%d>' % entries[p]['inode'] for p in group], env)
        for path, chunk in zip(group, chunks):
            item = entries[path]
            typ = {stat.S_IFREG: 'regular', stat.S_IFDIR: 'directory', stat.S_IFLNK: 'symlink'}[stat.S_IFMT(item['mode'])]
            match = re.search(r'^Inode: (\d+)\s+Type: (\w+)\s+Mode:\s+([0-7]+)', chunk, re.M)
            owner = re.search(r'^User:\s+(\d+)\s+Group:\s+(\d+).*?Size:\s+(\d+)', chunk, re.M)
            _need(match and owner and int(match[1]) == item['inode'] and match[2] == typ and
                  int(match[3], 8) == stat.S_IMODE(item['mode']) and owner[1] == owner[2] == '0' and
                  (item['size'] is None or int(owner[3]) == item['size']), 'inode stat differs: ' + path)
            if typ == 'symlink':
                link = re.search(r'^Fast link dest: "([^"\n]*)"$', chunk, re.M)
                if link is not None:
                    item['target'] = link[1]
                else:
                    _need(0 < item['size'] <= 4096, 'invalid header symlink size: ' + path)
                    value, err = _run(['/usr/bin/debugfs', '-R', 'cat <%d>' % item['inode'], str(image)],
                                      env=env, maximum=4096)
                    _need(re.fullmatch(rb'debugfs [^\n]+\n', err) is not None and len(value) == item['size'],
                          'cannot read header symlink: ' + path)
                    item['target'] = value.decode('utf-8', 'strict')
                _need(len(item['target'].encode('utf-8')) == item['size'], 'header symlink size differs: ' + path)
    all_nodes = dict(entries)
    all_nodes.update({p: {'mode': stat.S_IFDIR | 0o755} for p in ROOTS})

    def resolve(path):
        pending = path.split('/')[1:]
        done = []
        followed = set()
        while pending:
            component = pending.pop(0)
            if component == '.':
                continue
            if component == '..':
                _need(done, 'header symlink escapes: ' + path)
                done.pop()
                continue
            _need(NAME.fullmatch(component) is not None, 'unsafe symlink component: ' + path)
            done.append(component)
            current = '/' + '/'.join(done)
            if current in all_nodes and stat.S_ISLNK(all_nodes[current]['mode']):
                _need(current not in followed, 'header symlink cycle: ' + path + ' at ' + current)
                followed.add(current)
                target = all_nodes[current]['target']
                _need(target and not target.startswith('/'), 'absolute header symlink: ' + current)
                done.pop()
                pending = target.split('/') + pending
        result = '/' + '/'.join(done)
        if path == RUST_LINK and all_nodes[path]['target'] == RUST_TARGET and result == RUST_RESOLVED:
            _need(RUST_ABSENT_PARENT not in src, 'original Rust link target unexpectedly exists')
            entries[path]['dangling'] = True
            return
        _need(result in all_nodes and any(result == root or result.startswith(root + '/') for root in ROOTS),
              'header symlink target escapes or is absent: ' + path + ' -> ' +
              all_nodes[path]['target'] + ' -> ' + result)

    for path, item in entries.items():
        if stat.S_ISLNK(item['mode']):
            resolve(path)
    _need(_identity(os.stat(image, follow_symlinks=False)) == image_identity, 'header source image changed')
    return _ValidatedHeaders(entries, image_identity)


def _materialize(image, entries, output, env, *, image_fd=None):
    parent = os.stat(output, follow_symlinks=False)
    _need(stat.S_ISDIR(parent.st_mode) and parent.st_uid == os.geteuid() and
          stat.S_IMODE(parent.st_mode) == 0o700, 'header output directory is not private')
    headers = output / 'headers'
    os.mkdir(headers, 0o700)
    usr = headers / 'usr'
    os.mkdir(usr, 0o700)
    dest = usr / 'src'
    os.mkdir(dest, 0o700)
    absent = dest / RUST_ABSENT_PARENT
    _need(not os.path.lexists(absent), 'Rust link target unexpectedly exists before extraction')
    _need(RUST_LINK in entries and entries[RUST_LINK].get('dangling') is True and
          entries[RUST_LINK].get('target') == RUST_TARGET, 'original dangling Rust link is not validated')
    for root in ROOTS:
        os.mkdir(dest / Path(root).name, 0o700)
    for path, item in sorted(entries.items(), key=lambda x: (x[0].count('/'), x[0])):
        if path not in ROOTS and stat.S_ISDIR(item['mode']):
            os.mkdir(dest / path.removeprefix('/usr/src/'), 0o700)
    regular = [(p, e) for p, e in sorted(entries.items()) if stat.S_ISREG(e['mode'])]
    hashes = hashlib.sha256()
    for path, item in sorted(entries.items()):
        if stat.S_ISDIR(item['mode']):
            hashes.update((path + '\0directory\0' + str(item['mode']) + '\n').encode())
    for start in range(0, len(regular), 32):
        group = regular[start:start + 32]
        opened = []
        try:
            for path, item in group:
                target = dest / path.removeprefix('/usr/src/')
                opened.append((path, item, target, _create(target)))
            commands = ['dump <%d> /proc/self/fd/%d' % (item['inode'], fd) for _, item, _, fd in opened]
            fds = tuple(fd for _, _, _, fd in opened)
            _debug(image, commands, env, pass_fds=fds + (() if image_fd is None else (image_fd,)))
            for path, item, target, fd in opened:
                _need(os.fstat(fd).st_size == item['size'], 'short header extraction: ' + path)
                os.fchmod(fd, stat.S_IMODE(item['mode']))
                os.fsync(fd)
                sha = _hash_fd(fd)
                _need(_identity(os.fstat(fd)) == _identity(os.stat(target, follow_symlinks=False)), 'header output changed')
                hashes.update((path + '\0' + str(item['mode']) + '\0' + sha + '\n').encode())
        finally:
            for _, _, _, fd in opened:
                os.close(fd)
    for path, item in sorted(entries.items()):
        if stat.S_ISLNK(item['mode']):
            target = dest / path.removeprefix('/usr/src/')
            os.symlink(item['target'], target)
            kind = 'dangling' if item.get('dangling') is True else 'link'
            _need(kind != 'dangling' or (path == RUST_LINK and item['target'] == RUST_TARGET),
                  'unrecognized dangling header link')
            hashes.update((path + '\0' + kind + '\0' + item['target'] + '\n').encode())
    for current, directories, _ in os.walk(dest, topdown=False, followlinks=False):
        _sync_dir(current)
    rust_link = dest / RUST_LINK.removeprefix('/usr/src/')
    _need(not os.path.lexists(absent) and stat.S_ISLNK(os.lstat(rust_link).st_mode) and
          os.readlink(rust_link) == RUST_TARGET, 'dangling Rust link readback differs')
    return dest, hashes.hexdigest()


def materialize_validated(image, entries, output, *, env):
    """Materialize an already validated header inventory into a new private directory.

    `entries` must be the direct result of `_walk_headers` for `image`; this is
    an internal same-process interface, not an authority for caller-supplied trees.
    """
    image, output = Path(image), Path(output)
    _need(image.is_absolute() and output.is_absolute() and '..' not in image.parts and '..' not in output.parts,
          'paths must be absolute')
    _safe_ancestors(output)
    parent = os.stat(output.parent, follow_symlinks=False)
    _need(parent.st_uid == os.geteuid() and stat.S_IMODE(parent.st_mode) == 0o700,
          'output parent must be private and owned by caller')
    fd = _open_regular(image)
    try:
        before = _identity(os.fstat(fd))
        _need(isinstance(entries, _ValidatedHeaders) and entries.image_identity == before,
              'header inventory does not match retained partition')
        os.mkdir(output, 0o700)
        _sync_dir(output.parent)
        path, digest = _materialize('/proc/self/fd/%d' % fd, entries, output, env, image_fd=fd)
        _need(_identity(os.fstat(fd)) == before == _identity(os.stat(image, follow_symlinks=False)),
              'retained partition changed during header materialization')
        return {'headers': str(path), 'header_manifest_sha256': digest,
                'header_dangling_links': [{'path': RUST_LINK, 'target': RUST_TARGET,
                                           'resolved': RUST_RESOLVED, 'state': 'dangling'}]}
    finally:
        os.close(fd)


def _extract_inode(image, inode, size, path, env):
    fd = _create(path)
    try:
        _debug(image, ['dump <%d> /proc/self/fd/%d' % (inode, fd)], env, pass_fds=(fd,))
        _need(os.fstat(fd).st_size == size, 'boot file size differs')
        return _seal(fd, path)
    finally:
        os.close(fd)


def _file_entry(image, directory, name, env):
    entry = _listing(image, [directory], env)[directory].get(name)
    return entry if entry and stat.S_ISREG(entry['mode']) else None


def derive(base, qemu_img, output, *, env, base_sha256=BASE_SHA256, qemu_sha256=QEMU_SHA256):
    """Create an exclusive output directory and return bounded boot/tool metadata.

    Caller supplies the prepare-gated base and qemu-img, absolute new output path,
    and the clean task environment. On failure the attempt directory is retained.
    """
    base, qemu_img, output = Path(base), Path(qemu_img), Path(output)
    _need(all(p.is_absolute() and '..' not in p.parts for p in (base, qemu_img, output)), 'paths must be absolute')
    _need(base_sha256 == BASE_SHA256 and qemu_sha256 == QEMU_SHA256, 'unsupported image or QEMU pin')
    _safe_ancestors(output)
    parent = os.stat(output.parent, follow_symlinks=False)
    _need(parent.st_uid == os.geteuid() and stat.S_IMODE(parent.st_mode) == 0o700,
          'output parent must be private and owned by caller')
    tools = {'qemu-img': _tool(qemu_img, qemu_sha256), 'debugfs': _tool('/usr/bin/debugfs')}
    native = native_toolchain(env)
    libraries = {}
    tool_libraries = {name: _tool_libraries(tool, env, libraries) for name, tool in tools.items()}
    basefd = _open_regular(base)
    try:
        initial = _identity(os.fstat(basefd))
        _need(_hash_fd(basefd) == base_sha256, 'base image hash differs')
        info_bytes, _ = _run([str(qemu_img), 'info', '--output=json', '/proc/self/fd/%d' % basefd],
                             env=env, pass_fds=(basefd,), maximum=65536, timeout=60)
        info = json.loads(info_bytes)
        _need(info.get('format') == 'qcow2' and 'backing-filename' not in info and
              type(info.get('virtual-size')) is int and 0 < info['virtual-size'] <= 128 * 1024**3,
              'unsupported base image geometry or backing file')
        os.mkdir(output, 0o700)
        _sync_dir(output.parent)
        raw_path = output / 'base.raw'
        rawfd = _create(raw_path, info['virtual-size'])
        try:
            _run([str(qemu_img), 'convert', '-f', 'qcow2', '-O', 'raw', '-n',
                  '/proc/self/fd/%d' % basefd, '/proc/self/fd/%d' % rawfd],
                 env=env, pass_fds=(basefd, rawfd), timeout=3600)
            os.fsync(rawfd)
            _need(os.fstat(rawfd).st_size == info['virtual-size'], 'raw image geometry differs')
            partitions = _gpt(rawfd)
            images = []
            for part in partitions:
                if part['size'] >= 2048 and _pread(rawfd, part['start'] + 1080, 2) == b'\x53\xef':
                    image = output / ('partition-%d.raw' % part['index'])
                    _copy_partition(rawfd, part, image)
                    images.append((part, image))
        finally:
            os.close(rawfd)
        roots = []
        for part, image in images:
            try:
                inv = _packages(image, env)
            except PrerequisiteError:
                continue
            roots.append((part, image, inv))
        _need(len(roots) == 1, 'root package inventory is absent or ambiguous')
        root_part, root_image, inventories = roots[0]
        python = _python_executable(root_image, env)
        systemd = _systemd_executable(root_image, env)
        kernel_name, initrd_name, config_name, map_name = (
            'vmlinuz-' + RELEASE, 'initrd.img-' + RELEASE, 'config-' + RELEASE, 'System.map-' + RELEASE)
        boot = []
        for part, image in images:
            for directory in ('/', '/boot'):
                try:
                    listing = _listing(image, [directory.rstrip('/') or '/'], env)[directory.rstrip('/') or '/']
                except PrerequisiteError:
                    continue
                if all(name in listing and stat.S_ISREG(listing[name]['mode']) for name in (kernel_name, initrd_name, config_name, map_name)):
                    boot.append((part, image, directory, listing))
        _need(len(boot) == 1, 'kernel and initrd location is absent or ambiguous')
        boot_part, boot_image, directory, listing = boot[0]
        kernel_package_path = '/boot/' + kernel_name
        _need(kernel_package_path in inventories[PACKAGES[2]], 'kernel path not in installed image package')
        _need('/boot/' + map_name in inventories[PACKAGES[3]] and
              '/boot/' + config_name in inventories[PACKAGES[3]],
              'kernel map or configuration not in installed image package')
        header_entries = _walk_headers(root_image, inventories, env)
        headers, header_sha = _materialize(root_image, header_entries, output, env)
        boot_dir = output / 'boot'
        os.mkdir(boot_dir, 0o700)
        kernel = _extract_inode(boot_image, listing[kernel_name]['inode'], listing[kernel_name]['size'], boot_dir / 'vmlinuz', env)
        initrd = _extract_inode(boot_image, listing[initrd_name]['inode'], listing[initrd_name]['size'], boot_dir / 'initrd.original', env)
        config = _extract_inode(boot_image, listing[config_name]['inode'], listing[config_name]['size'], boot_dir / 'config', env)
        system_map = _extract_inode(boot_image, listing[map_name]['inode'], listing[map_name]['size'], boot_dir / 'System.map', env)
        for _, image in images:
            os.unlink(image)
        os.unlink(raw_path)
        _sync_dir(output)
        generic = headers / ('linux-headers-' + RELEASE)
        cfgfd = _open_regular(generic / '.config')
        symfd = _open_regular(generic / 'Module.symvers')
        try:
            _need(_hash_fd(cfgfd) == config['sha256'], 'header and boot config differ')
            config_size = os.fstat(cfgfd).st_size
            _need(0 < config_size <= 1024 * 1024, 'kernel config exceeds bound')
            config_lines = _pread(cfgfd, 0, config_size).decode('utf-8', 'strict').splitlines()
            for line in ('CONFIG_BPF_LSM=y', 'CONFIG_BPF_SYSCALL=y', 'CONFIG_MODVERSIONS=y',
                         'CONFIG_AUDIT=y', 'CONFIG_AUDITSYSCALL=y', 'CONFIG_KPROBES=y',
                         'CONFIG_KRETPROBES=y', '# CONFIG_BPF_PRELOAD is not set',
                         '# CONFIG_X86_X32_ABI is not set',
                         '# CONFIG_MODULE_FORCE_LOAD is not set', '# CONFIG_MODULE_FORCE_UNLOAD is not set'):
                _need(config_lines.count(line) == 1, 'kernel configuration prerequisite missing: ' + line)
            symvers = _hash_fd(symfd)
        finally:
            os.close(cfgfd)
            os.close(symfd)
        _need(_identity(os.fstat(basefd)) == initial == _identity(os.stat(base, follow_symlinks=False)) and
              _hash_fd(basefd) == base_sha256, 'base image changed during extraction')
        _need(_tool(qemu_img, qemu_sha256) == tools['qemu-img'] and _tool('/usr/bin/debugfs') == tools['debugfs'],
              'extraction tool changed')
        _need(native_toolchain(env) == native, 'native toolchain changed during extraction')
        for path, sha in libraries.items():
            _need(_tool(path)['sha256'] == sha, 'tool dependency changed: ' + path)
        metadata = {'version': 1, 'base_sha256': base_sha256, 'kernel_release': RELEASE,
                'root_partition': root_part, 'boot_partition': boot_part,
                'kernel_source': kernel_package_path, 'initrd_source': directory.rstrip('/') + '/' + initrd_name,
                'kernel': kernel, 'initrd': initrd, 'config': config, 'system_map': system_map, 'headers': str(headers),
                'header_manifest_sha256': header_sha, 'module_symvers_sha256': symvers, 'tools': tools,
                'header_dangling_links': [{'path': RUST_LINK, 'target': RUST_TARGET,
                                           'resolved': RUST_RESOLVED, 'state': 'dangling'}],
                'tool_libraries': tool_libraries, 'libraries': libraries, 'native_toolchain': native,
                'package_executables': {'python': python, 'systemd': systemd},
                'native_compatibility': 'unverified_until_module_build'}
        return BootInputs(metadata)
    finally:
        os.close(basefd)


def _newc_names(data):
    """Return consumed bytes and canonical names from one uncompressed newc archive."""
    names = set()
    pos = 0
    while True:
        _need(pos + 110 <= len(data) and data[pos:pos + 6] in (b'070701', b'070702'), 'invalid newc header')
        crc_format = data[pos:pos + 6] == b'070702'
        try:
            fields = [int(data[pos + 6 + n * 8:pos + 14 + n * 8], 16) for n in range(13)]
        except ValueError as exc:
            raise PrerequisiteError('invalid newc numeric field') from exc
        size, namesize, checksum = fields[6], fields[11], fields[12]
        _need(0 < namesize <= 4096 and size <= MAX_INITRD, 'invalid newc entry size')
        pos += 110
        _need(pos + namesize <= len(data) and data[pos + namesize - 1] == 0, 'truncated newc name')
        name = data[pos:pos + namesize - 1].decode('utf-8', 'strict')
        _need('\0' not in name, 'embedded NUL in newc name')
        pos = (pos + namesize + 3) & ~3
        _need(pos + size <= len(data), 'truncated newc data')
        payload = data[pos:pos + size]
        if crc_format:
            _need(sum(payload) & 0xffffffff == checksum, 'newc checksum differs')
        pos = (pos + size + 3) & ~3
        if name == 'TRAILER!!!':
            _need(size == 0, 'invalid newc trailer')
            return pos, names
        parts = name.split('/')
        _need(not name.startswith('/') and all(p not in ('', '..') for p in parts), 'unsafe newc path')
        normalized = '/'.join(p for p in parts if p != '.')
        if not normalized:
            _need(stat.S_ISDIR(fields[1]), 'invalid newc root entry')
        else:
            _need(normalized not in names, 'duplicate newc path')
            names.add(normalized)



def _zstd_frame(data):
    name = ctypes.util.find_library('zstd')
    _need(name is not None, 'zstd library unavailable for initrd inspection')
    try:
        lib = ctypes.CDLL(name)
        lib.ZSTD_findFrameCompressedSize.argtypes = (ctypes.c_void_p, ctypes.c_size_t)
        lib.ZSTD_findFrameCompressedSize.restype = ctypes.c_size_t
        lib.ZSTD_decompressBound.argtypes = (ctypes.c_void_p, ctypes.c_size_t)
        lib.ZSTD_decompressBound.restype = ctypes.c_ulonglong
        lib.ZSTD_decompress.argtypes = (ctypes.c_void_p, ctypes.c_size_t, ctypes.c_void_p, ctypes.c_size_t)
        lib.ZSTD_decompress.restype = ctypes.c_size_t
        lib.ZSTD_isError.argtypes = (ctypes.c_size_t,)
        lib.ZSTD_isError.restype = ctypes.c_uint
        source = ctypes.create_string_buffer(data)
        length = lib.ZSTD_findFrameCompressedSize(source, len(data))
        _need(not lib.ZSTD_isError(length) and 0 < length <= len(data), 'invalid zstd initrd frame')
        bound = lib.ZSTD_decompressBound(source, length)
        _need(0 < bound <= MAX_INITRD, 'zstd initrd exceeds bound')
        target = ctypes.create_string_buffer(bound)
        actual = lib.ZSTD_decompress(target, bound, source, length)
        _need(not lib.ZSTD_isError(actual) and actual <= bound, 'invalid zstd initrd data')
        return length, target.raw[:actual]
    except (OSError, AttributeError) as exc:
        raise PrerequisiteError('zstd initrd inspection unavailable') from exc

def assert_initrd_no_collisions(raw, added_names):
    """Reject collisions in uncompressed, gzip, xz, and zstd initrd archives.

    Unsupported compression is rejected; callers must not append on that error.
    """
    _need(isinstance(raw, bytes) and 0 < len(raw) <= MAX_INITRD, 'initrd exceeds bound')
    _need(isinstance(added_names, (tuple, list)) and 0 < len(added_names) <= 64 and
          all(isinstance(name, str) for name in added_names), 'invalid bootstrap archive names')
    wanted = {name.removeprefix('/') for name in added_names}
    _need(len(wanted) == len(added_names) and
          all(name not in ('.', '..') and re.fullmatch(r'[A-Za-z0-9_.-]+', name) for name in wanted),
          'invalid bootstrap archive names')
    seen = set()
    pos = 0
    expanded = 0
    archive_count = 0
    while pos < len(raw):
        if raw[pos] == 0:
            pos += 1
            continue
        if raw[pos:pos + 6] in (b'070701', b'070702'):
            used, names = _newc_names(raw[pos:])
            pos += used
            expanded += used
        elif raw[pos:pos + 2] == b'\x1f\x8b':
            dec = zlib.decompressobj(31)
            body = dec.decompress(raw[pos:], MAX_INITRD + 1)
            _need(dec.eof and len(body) <= MAX_INITRD, 'invalid or oversized gzip initrd')
            pos = len(raw) - len(dec.unused_data)
            used, names = _newc_names(body)
            _need(not body[used:].strip(b'\0'), 'extra gzip initrd content')
            expanded += len(body)
        elif raw[pos:pos + 6] == b'\xfd7zXZ\x00':
            dec = lzma.LZMADecompressor()
            body = dec.decompress(raw[pos:], max_length=MAX_INITRD + 1)
            _need(dec.eof and len(body) <= MAX_INITRD, 'invalid or oversized xz initrd')
            pos = len(raw) - len(dec.unused_data)
            used, names = _newc_names(body)
            _need(not body[used:].strip(b'\0'), 'extra xz initrd content')
            expanded += len(body)
        elif raw[pos:pos + 4] == b'\x28\xb5\x2f\xfd':
            used, body = _zstd_frame(raw[pos:])
            pos += used
            consumed, names = _newc_names(body)
            _need(not body[consumed:].strip(b'\0'), 'extra zstd initrd content')
            expanded += len(body)
        else:
            raise PrerequisiteError('unsupported initrd compression or archive format')
        _need(expanded <= MAX_INITRD, 'oversized initrd archive')
        seen.update(names)
        archive_count += 1
    _need(archive_count > 0 and not seen.intersection(wanted), 'missing initrd archive or bootstrap name collision')
    return {'archive_entries': len(seen), 'original_sha256': hashlib.sha256(raw).hexdigest()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', required=True)
    parser.add_argument('--qemu-img', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    env = {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C', 'TZ': 'UTC'}
    with derive(args.base, args.qemu_img, args.output, env=env) as result:
        encoded = json.dumps(result.metadata, sort_keys=True, separators=(',', ':'), allow_nan=False)
        _need(len(encoded) <= 16384, 'metadata exceeds output limit')
        print(encoded)


if __name__ == '__main__':
    try:
        main()
    except (PrerequisiteError, OSError, ValueError, UnicodeError, json.JSONDecodeError) as exc:
        print('PREREQUISITE_FAILED: ' + str(exc)[:512], file=sys.stderr)
        sys.exit(1)
