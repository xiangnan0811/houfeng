#!/usr/bin/env python3
"""Prepare the isolated negative-only Chromium image from pinned local inputs.

The builder never starts a browser or contacts a registry.  `selected_browser()`
performs read-only selection validation and is callable by source_build while it
holds the shared source-vm.lock.
"""
import argparse
import fcntl
import hashlib
import gzip
import json
import os
from pathlib import Path
import posixpath
import re
import stat
import subprocess
import sys
import tarfile

from source_host import SOURCE_ROOT, RUN, TOKEN
BUNDLE = Path('/home/murray/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell-linux64')
PROGRAM = 'chrome-headless-shell'
PROGRAM_SHA256 = '670ba079b75107746ba41abad131180a31a7c7219aa1bd4061fb471f4535d541'
BROWSER_REVISION = 1228
EXPECTED_BROWSER_VERSION = '149.0.7827.55'
ENTRYPOINT = '/opt/g2/browser/chrome-headless-shell'
INTERPRETER = '/lib64/ld-linux-x86-64.so.2'
INTERPRETERS = frozenset((INTERPRETER, '/usr/lib/ld-linux-x86-64.so.2', '/usr/lib64/ld-linux-x86-64.so.2'))
TOOLS = ('/usr/bin/readelf', '/usr/bin/ldd', '/usr/bin/docker', '/bin/bash')
DYNAMIC_MODULES = (
    '/usr/lib/libnss_dns.so.2', '/usr/lib/libnss_files.so.2', '/usr/lib/libresolv.so.2',
    '/usr/lib/libsoftokn3.so', '/usr/lib/libfreebl3.so', '/usr/lib/libfreeblpriv3.so',
    '/usr/lib/libnssckbi.so', '/usr/lib/libnsssysinit.so',
    '/usr/lib/libfontconfig.so.1', '/usr/lib/libfreetype.so.6',
)
IMAGE_ENV = (
    'LD_LIBRARY_PATH=/opt/g2/browser:/opt/g2/runtime/lib', 'HOME=/tmp',
    'LANG=C', 'LC_ALL=C', 'PATH=/nonexistent',
)
TOP_FILES = frozenset((
    'ABOUT', 'LICENSE.headless_shell', 'deb.deps', 'rpm.deps', PROGRAM,
    'headless_command_resources.pak', 'headless_lib_data.pak', 'headless_lib_strings.pak',
    'icudtl.dat', 'v8_context_snapshot.bin', 'vk_swiftshader_icd.json',
    'libEGL.so', 'libGLESv2.so', 'libvk_swiftshader.so', 'libvulkan.so.1',
))
TOP_DIRS = frozenset(('locales', 'hyphen-data'))
HEX = re.compile(r'^[0-9a-f]{64}$')
NUMBER = re.compile(r'^[0-9]{20}$')
LIBNAME = re.compile(r'^[A-Za-z0-9_+.-]+$')
MAX_BUNDLE = 512 * 1024 * 1024
MAX_FILES = 4096


class Failure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Failure(message)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False) + '\n').encode('ascii')


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    def bad_number(_):
        raise Failure('non-finite JSON number')
    try:
        return json.loads(raw, object_pairs_hook=pairs, parse_constant=bad_number)
    except (ValueError, UnicodeError) as exc:
        raise Failure('invalid JSON') from exc


def stat_identity(info):
    return {'dev': info.st_dev, 'ino': info.st_ino, 'mode': info.st_mode, 'uid': info.st_uid,
            'gid': info.st_gid, 'nlink': info.st_nlink, 'size': info.st_size,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_new(path, body, mode=0o600):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    try:
        view = memoryview(body)
        while view:
            count = os.write(fd, view)
            require(count > 0, 'short file write')
            view = view[count:]
        os.fchmod(fd, mode)
        os.fsync(fd)
    finally:
        os.close(fd)


def file_digest(path, limit=None):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1, 'unsafe evidence file')
        h = hashlib.sha256()
        size = 0
        while True:
            chunk = os.read(fd, 1024 * 1024)
            if not chunk:
                break
            size += len(chunk)
            require(limit is None or size <= limit, 'evidence file exceeds limit')
            h.update(chunk)
        require(stat_identity(before) == stat_identity(os.fstat(fd)) and size == before.st_size,
                'evidence file changed during read')
        return h.hexdigest(), size
    finally:
        os.close(fd)


def read_small(path, limit, mode=None):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1 and
                0 < before.st_size <= limit and (mode is None or stat.S_IMODE(before.st_mode) == mode),
                'invalid private evidence file')
        data = bytearray()
        while len(data) <= limit:
            chunk = os.read(fd, min(65536, limit + 1 - len(data)))
            if not chunk:
                break
            data.extend(chunk)
        require(len(data) == before.st_size and stat_identity(before) == stat_identity(os.fstat(fd)),
                'private evidence changed during read')
        return bytes(data)
    finally:
        os.close(fd)


def check_owned_run(argument):
    require(argument == str(RUN) and Path(__file__).absolute() == SOURCE_ROOT / 'browser_negative.py', 'incorrect run directory')
    current = Path('/')
    for part in RUN.parts[1:]:
        current /= part
        info = os.lstat(current)
        require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode), 'unsafe run path')
    info = os.lstat(RUN)
    require(os.geteuid() != 0 and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
            'run directory is not private')


def private_dir(path, create):
    if create:
        try:
            os.mkdir(path, 0o700)
            sync_dir(path.parent)
        except FileExistsError:
            pass
    info = os.lstat(path)
    require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode) and info.st_uid == os.geteuid() and
            stat.S_IMODE(info.st_mode) == 0o700, 'private inventory directory invalid')


def lock_run():
    fd = os.open(RUN / 'source-vm.lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1 and
                stat.S_IMODE(info.st_mode) == 0o600, 'shared source lock invalid')
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return fd
    except BaseException:
        os.close(fd)
        raise


def numbered(directory, prefix, suffix, create):
    private_dir(directory, create)
    pattern = re.compile(re.escape(prefix) + r'([0-9]{20})' + re.escape(suffix) + r'$')
    found = []
    for name in os.listdir(directory):
        match = pattern.fullmatch(name)
        require(match is not None, 'unexpected inventory entry')
        found.append(int(match.group(1)))
    found.sort()
    require(found == list(range(1, len(found) + 1)), 'inventory numbering is discontinuous')
    return found


class Events:
    def __init__(self, path):
        self.fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        self.sequence = 0
        self.previous = '0' * 64

    def add(self, status, stage):
        self.sequence += 1
        value = {'version': 1, 'sequence': self.sequence, 'previous_sha256': self.previous,
                 'status': status, 'stage': stage}
        body = canonical(value)
        view = memoryview(body)
        while view:
            count = os.write(self.fd, view)
            require(count > 0, 'event write failed')
            view = view[count:]
        os.fsync(self.fd)
        self.previous = hashlib.sha256(body).hexdigest()

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None


def check_events(path, expected):
    raw = read_small(path, 1024 * 1024, 0o600)
    require(hashlib.sha256(raw).hexdigest() == expected and raw.endswith(b'\n'), 'event digest differs')
    previous = '0' * 64
    last = None
    for sequence, line in enumerate(raw.splitlines(keepends=True), 1):
        value = strict_json(line)
        require(type(value) is dict and set(value) == {'version', 'sequence', 'previous_sha256', 'status', 'stage'} and
                type(value['version']) is int and value['version'] == 1 and type(value['sequence']) is int and
                value['sequence'] == sequence and value['previous_sha256'] == previous and
                type(value['status']) is str and type(value['stage']) is str and canonical(value) == line,
                'event chain invalid')
        previous = hashlib.sha256(line).hexdigest()
        last = value
    require(last is not None and last['status'] == 'PASS' and last['stage'] == 'complete', 'attempt was not completed')


class Sources:
    """Record actual paths, including symlinks, and recheck all input bytes."""
    def __init__(self):
        self.nodes = {}
        self.files = {}

    def node(self, path):
        info = os.lstat(path)
        value = stat_identity(info)
        if stat.S_ISLNK(info.st_mode):
            value['target'] = os.readlink(path)
        name = str(path)
        if name in self.nodes:
            require(self.nodes[name] == value, 'source path changed')
        else:
            self.nodes[name] = value
        return info

    def resolve(self, path, allowed):
        path = os.path.abspath(os.fspath(path))
        require(path.startswith('/') and '\0' not in path, 'invalid source path')
        current = path
        for _ in range(40):
            parts = Path(current).parts[1:]
            here = Path('/')
            followed = False
            for index, part in enumerate(parts):
                require(part not in ('', '.', '..'), 'unsafe source path')
                here /= part
                info = self.node(here)
                if stat.S_ISLNK(info.st_mode):
                    target = os.readlink(here)
                    require('\0' not in target, 'unsafe source symlink')
                    base = target if os.path.isabs(target) else os.path.join(str(here.parent), target)
                    current = os.path.normpath(os.path.join(base, *parts[index + 1:]))
                    require(current.startswith('/'), 'source symlink escaped')
                    followed = True
                    break
                if index < len(parts) - 1:
                    require(stat.S_ISDIR(info.st_mode), 'source ancestor is not a directory')
            if followed:
                continue
            resolved = Path(current)
            require(any(resolved == root or root in resolved.parents for root in allowed), 'source outside allowed roots')
            require(stat.S_ISREG(os.lstat(resolved).st_mode), 'source is not a regular file')
            return resolved
        raise Failure('source symlink chain too long')

    def open_file(self, path, allowed):
        resolved = self.resolve(path, allowed)
        fd = os.open(resolved, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        before = os.fstat(fd)
        expected = self.nodes[str(resolved)]
        require(stat_identity(before) == expected and stat.S_ISREG(before.st_mode) and
                before.st_uid in (0, os.geteuid()) and stat.S_IMODE(before.st_mode) & 0o022 == 0,
                'source identity or permissions invalid')
        if Path('/usr/lib') in resolved.parents or Path('/usr/bin') in resolved.parents:
            require(before.st_uid == 0, 'system binary is not root-owned')
        return resolved, fd, before

    def finish(self, resolved, fd, before, digest, size):
        require(stat_identity(before) == stat_identity(os.fstat(fd)) and size == before.st_size,
                'source changed while reading')
        value = {'sha256': digest, 'size': size, 'identity': stat_identity(before)}
        key = str(resolved)
        if key in self.files:
            require(self.files[key] == value, 'source content changed')
        self.files[key] = value

    def pin(self, path, allowed):
        resolved, fd, before = self.open_file(path, allowed)
        h, size = hashlib.sha256(), 0
        try:
            while True:
                chunk = os.read(fd, 1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                h.update(chunk)
            self.finish(resolved, fd, before, h.hexdigest(), size)
        finally:
            os.close(fd)
        return resolved, self.files[str(resolved)]

    def verify(self):
        for name, expected in self.nodes.items():
            info = os.lstat(name)
            current = stat_identity(info)
            if stat.S_ISLNK(info.st_mode):
                current['target'] = os.readlink(name)
            require(current == expected, 'source path changed before selection')
        for name, expected in list(self.files.items()):
            _, actual = self.pin(name, (BUNDLE, Path('/usr/lib'), Path('/usr/bin')))
            require(actual == expected, 'source bytes changed before selection')


def checked_relative(name):
    require(type(name) is str and name and not name.startswith('/') and '\0' not in name and
            all(part not in ('', '.', '..') for part in name.split('/')), 'unsafe image path')
    return name


def ensure_tree_dir(root, tree, relative, mode=0o755):
    checked_relative(relative)
    parts = relative.split('/')
    for index in range(1, len(parts) + 1):
        name = '/'.join(parts[:index])
        wanted = mode if index == len(parts) else 0o755
        if name in tree:
            require(tree[name]['type'] == 'dir' and (index != len(parts) or tree[name]['mode'] == wanted),
                    'image directory collision')
            continue
        os.mkdir(root / name, 0o700)
        tree[name] = {'type': 'dir', 'mode': wanted}


def copy_source(sources, source, allowed, root, tree, relative, target_mode):
    checked_relative(relative)
    parent = posixpath.dirname(relative)
    if parent:
        ensure_tree_dir(root, tree, parent)
    require(relative not in tree, 'image file collision')
    resolved, src, before = sources.open_file(source, allowed)
    dst = os.open(root / relative, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    h, size = hashlib.sha256(), 0
    try:
        while True:
            chunk = os.read(src, 1024 * 1024)
            if not chunk:
                break
            h.update(chunk)
            size += len(chunk)
            view = memoryview(chunk)
            while view:
                count = os.write(dst, view)
                require(count > 0, 'image copy did not progress')
                view = view[count:]
        sources.finish(resolved, src, before, h.hexdigest(), size)
        os.fchmod(dst, 0o500 if target_mode & 0o111 else 0o400)
        os.fsync(dst)
    finally:
        os.close(src)
        os.close(dst)
    tree[relative] = {'type': 'file', 'mode': target_mode, 'size': size, 'sha256': h.hexdigest()}
    return resolved, before


def add_link(root, tree, relative, target):
    checked_relative(relative)
    require(type(target) is str and target and not target.startswith('/') and '\0' not in target,
            'unsafe image symlink')
    final = posixpath.normpath(posixpath.join(posixpath.dirname(relative), target))
    checked_relative(final)
    ensure_tree_dir(root, tree, posixpath.dirname(relative))
    if relative in tree:
        require(tree[relative] == {'type': 'symlink', 'mode': 0o777, 'target': target}, 'image alias collision')
        return
    os.symlink(target, root / relative)
    tree[relative] = {'type': 'symlink', 'mode': 0o777, 'target': target}


def bundle_files(sources):
    root_info = sources.node(BUNDLE)
    require(stat.S_ISDIR(root_info.st_mode) and not stat.S_ISLNK(root_info.st_mode), 'browser bundle unavailable')
    entries = sorted(os.listdir(BUNDLE))
    require(set(entries) == TOP_FILES | TOP_DIRS, 'browser bundle inventory differs')
    result = []
    for name in entries:
        path = BUNDLE / name
        info = sources.node(path)
        if name in TOP_DIRS:
            require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode), 'browser data directory invalid')
            children = sorted(os.listdir(path))
            require(children, 'browser data directory is empty')
            pattern = re.compile(r'^[A-Za-z0-9_-]+\.(?:pak|hyb)$')
            for child in children:
                require(pattern.fullmatch(child) is not None and
                        ((name == 'locales' and child.endswith('.pak')) or
                         (name == 'hyphen-data' and child.endswith('.hyb'))), 'unexpected browser data')
                item = path / child
                child_info = sources.node(item)
                require(stat.S_ISREG(child_info.st_mode) and not stat.S_ISLNK(child_info.st_mode), 'browser data is not regular')
                result.append((name + '/' + child, item))
        else:
            require(stat.S_ISREG(info.st_mode) and not stat.S_ISLNK(info.st_mode), 'browser input is not regular')
            result.append((name, path))
    require(0 < len(result) <= MAX_FILES, 'browser bundle count exceeds limit')
    return result


def command(args, env, timeout=60, stdin=None):
    try:
        completed = subprocess.run(args, env=env, cwd=RUN, stdin=stdin if stdin is not None else subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise Failure('fixed command did not complete') from exc
    require(len(completed.stdout) <= 2 * 1024 * 1024 and len(completed.stderr) <= 2 * 1024 * 1024,
            'fixed command output exceeds limit')
    return completed


def is_elf(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        return os.read(fd, 4) == b'\x7fELF'
    finally:
        os.close(fd)


def inspect_elf(path, sources, env, logs, index):
    resolved, identity = sources.pin(path, (BUNDLE, Path('/usr/lib')))
    require(is_elf(resolved), 'runtime dependency is not ELF')
    result = command(['/usr/bin/readelf', '-h', '-l', '-d', str(resolved)], env)
    require(result.returncode == 0 and not result.stderr, 'readelf failed')
    header = result.stdout.decode('utf-8', 'strict')
    require(re.search(r'Class:\s+ELF64\b', header) is not None and
            re.search(r'Machine:\s+Advanced Micro Devices X86-64\b', header) is not None, 'unexpected ELF architecture')
    needed = re.findall(r'\(NEEDED\).*?Shared library: \[([^\]]+)\]', header)
    require(all(LIBNAME.fullmatch(name) and '..' not in name for name in needed), 'unsafe ELF dependency name')
    interpreters = re.findall(r'\[Requesting program interpreter:\s*([^\]]+)\]', header)
    require(len(interpreters) <= 1 and all(value in INTERPRETERS for value in interpreters), 'unexpected ELF interpreter')
    ldd_env = dict(env, LD_LIBRARY_PATH=str(BUNDLE) + ':/usr/lib')
    output = command(['/usr/bin/ldd', str(resolved)], ldd_env)
    stdout, stderr = output.stdout.decode('utf-8', 'strict'), output.stderr.decode('utf-8', 'strict')
    combined = stdout + stderr
    require('not found' not in combined, 'ELF dependency missing')
    resolved_deps = {}
    for line in stdout.splitlines():
        line = line.strip()
        if not line or re.fullmatch(r'linux-vdso\.so\.\d+\s+\(0x[0-9a-fA-F]+\)', line):
            continue
        if line in ('statically linked', 'not a dynamic executable'):
            require(not needed, 'dynamic ELF reported static')
            continue
        match = re.fullmatch(r'(\S+)\s+=>\s+(/\S+)\s+\(0x[0-9a-fA-F]+\)', line)
        if match:
            name, dependency = match.groups()
        else:
            match = re.fullmatch(r'(/\S+)\s+\(0x[0-9a-fA-F]+\)', line)
            require(match is not None, 'unrecognized ldd output')
            dependency = match.group(1)
            name = os.path.basename(dependency)
        if name.startswith('/'):
            require(name in INTERPRETERS and sources.resolve(name, (Path('/usr/lib'),)) ==
                    sources.resolve(dependency, (Path('/usr/lib'),)), 'unexpected absolute ldd name')
            name = os.path.basename(name)
        require(LIBNAME.fullmatch(name) and '..' not in name, 'unsafe ldd library name')
        actual = sources.resolve(dependency, (BUNDLE, Path('/usr/lib')))
        value = str(actual)
        require(name not in resolved_deps or resolved_deps[name] == value, 'conflicting ELF dependency')
        resolved_deps[name] = value
    require(output.returncode == 0 or (not needed and ('statically linked' in combined or 'not a dynamic executable' in combined)),
            'ldd failed')
    require(all(name in resolved_deps for name in needed), 'ldd omitted a needed dependency')
    prefix = '%05d' % index
    write_new(logs / (prefix + '.readelf'), result.stdout)
    write_new(logs / (prefix + '.ldd'), output.stdout + output.stderr)
    sources.pin(resolved, (BUNDLE, Path('/usr/lib')))
    return resolved, {'source_sha256': identity['sha256'], 'readelf_sha256': hashlib.sha256(result.stdout).hexdigest(),
                      'ldd_sha256': hashlib.sha256(output.stdout + output.stderr).hexdigest(),
                      'needed': needed, 'resolved': resolved_deps,
                      'interpreter': interpreters[0] if interpreters else None}


def install_bundle(sources, root, tree):
    inventory, elfs, total = {}, [], 0
    for relative, source in bundle_files(sources):
        info = os.lstat(source)
        mode = 0o755 if info.st_mode & 0o111 else 0o644
        target = 'opt/g2/browser/' + relative
        resolved, before = copy_source(sources, source, (BUNDLE,), root, tree, target, mode)
        entry = tree[target]
        total += entry['size']
        require(total <= MAX_BUNDLE, 'browser bundle exceeds limit')
        inventory[relative] = {'sha256': entry['sha256'], 'size': entry['size'], 'mode': stat.S_IMODE(before.st_mode)}
        if is_elf(resolved):
            elfs.append(resolved)
        elif relative == PROGRAM or relative.startswith('lib') and '.so' in relative:
            raise Failure('browser executable is not ELF')
    require(inventory[PROGRAM]['sha256'] == PROGRAM_SHA256 and PROGRAM in inventory, 'pinned browser executable differs')
    require(elfs, 'browser ELF inventory is empty')
    return inventory, elfs


def install_libraries(sources, root, tree, initial, env, logs):
    pending = list(initial)
    for name in DYNAMIC_MODULES:
        pending.append(sources.resolve(name, (Path('/usr/lib'),)))
    inspected = {}
    aliases = {}
    while pending:
        path = pending.pop(0)
        resolved = sources.resolve(path, (BUNDLE, Path('/usr/lib')))
        key = str(resolved)
        if key in inspected:
            continue
        require(len(inspected) < 1024, 'ELF dependency closure exceeds limit')
        _, item = inspect_elf(resolved, sources, env, logs, len(inspected) + 1)
        inspected[key] = item
        for name, target in item['resolved'].items():
            if name in aliases:
                require(aliases[name] == target, 'inconsistent SONAME resolution')
            else:
                aliases[name] = target
            pending.append(Path(target))
    for name in DYNAMIC_MODULES:
        target = str(sources.resolve(name, (Path('/usr/lib'),)))
        alias = os.path.basename(name)
        require(alias not in aliases or aliases[alias] == target, 'dynamic module alias differs')
        aliases[alias] = target
    system = sorted({target for target in aliases.values() if Path('/usr/lib') in Path(target).parents})
    require(system, 'system ELF dependency closure is empty')
    installed = {}
    for source in system:
        name = os.path.basename(source)
        require(LIBNAME.fullmatch(name) and '..' not in name, 'unsafe runtime library path')
        relative = 'opt/g2/runtime/lib/' + name
        if relative in tree:
            require(installed[relative] == source, 'runtime library filename collision')
            continue
        copy_source(sources, source, (Path('/usr/lib'),), root, tree, relative, 0o755)
        installed[relative] = source
    for alias, source in sorted(aliases.items()):
        actual = Path(source)
        if BUNDLE in actual.parents:
            require(actual.name == alias, 'browser library alias is not contained in bundle')
            continue
        relative = 'opt/g2/runtime/lib/' + alias
        target = actual.name
        if relative == 'opt/g2/runtime/lib/' + target:
            continue
        add_link(root, tree, relative, target)
    loader = str(sources.resolve('/lib64/ld-linux-x86-64.so.2', (Path('/usr/lib'),)))
    require(loader in system, 'ELF loader missing from dependency closure')
    for interpreter in sorted({INTERPRETER} | {item['interpreter'] for item in inspected.values() if item['interpreter']}):
        require(str(sources.resolve(interpreter, (Path('/usr/lib'),))) == loader, 'ELF interpreters resolve differently')
        relative = interpreter.lstrip('/')
        target = posixpath.relpath('opt/g2/runtime/lib/' + os.path.basename(loader), posixpath.dirname(relative))
        add_link(root, tree, relative, target)
    return inspected, aliases


def validate_tree(root, tree):
    found = {}
    for current, directories, files in os.walk(root, topdown=True, followlinks=False):
        directories.sort()
        files.sort()
        for name in directories + files:
            path = Path(current) / name
            relative = path.relative_to(root).as_posix()
            checked_relative(relative)
            info = os.lstat(path)
            expected = tree.get(relative)
            require(expected is not None, 'unexpected image tree entry')
            if stat.S_ISLNK(info.st_mode):
                require(expected['type'] == 'symlink' and os.readlink(path) == expected['target'], 'image link differs')
                if name in directories:
                    directories.remove(name)
            elif stat.S_ISDIR(info.st_mode):
                require(expected['type'] == 'dir', 'image directory differs')
            elif stat.S_ISREG(info.st_mode):
                digest, size = file_digest(path)
                require(expected['type'] == 'file' and digest == expected['sha256'] and size == expected['size'],
                        'image bytes differ')
            else:
                raise Failure('unsafe image tree entry')
            found[relative] = True
    require(set(found) == set(tree), 'image tree is incomplete')


class DigestReader:
    def __init__(self, source, limit=None):
        self.source = source
        self.hash = hashlib.sha256()
        self.size = 0
        self.limit = limit

    def read(self, size=-1):
        data = self.source.read(size)
        self.hash.update(data)
        self.size += len(data)
        require(self.limit is None or self.size <= self.limit, 'stream exceeds limit')
        return data


def make_rootfs_tar(root, tree, path):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(fd, 'wb', closefd=False) as output:
            with tarfile.open(fileobj=output, mode='w', format=tarfile.USTAR_FORMAT) as tar:
                for name, item in sorted(tree.items()):
                    entry = tarfile.TarInfo(name)
                    entry.uid = entry.gid = 0
                    entry.uname = entry.gname = ''
                    entry.mtime = 0
                    entry.mode = item['mode']
                    if item['type'] == 'dir':
                        entry.type = tarfile.DIRTYPE
                        tar.addfile(entry)
                    elif item['type'] == 'symlink':
                        entry.type = tarfile.SYMTYPE
                        entry.linkname = item['target']
                        tar.addfile(entry)
                    else:
                        require(item['type'] == 'file', 'unknown image tree type')
                        src = os.open(root / name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
                        try:
                            before = os.fstat(src)
                            require(stat.S_ISREG(before.st_mode) and before.st_size == item['size'], 'image file changed')
                            entry.size = item['size']
                            with os.fdopen(src, 'rb', closefd=False) as source:
                                reader = DigestReader(source)
                                tar.addfile(entry, reader)
                            require(reader.size == item['size'] and reader.hash.hexdigest() == item['sha256'] and
                                    stat_identity(before) == stat_identity(os.fstat(src)), 'image file changed during tar')
                        finally:
                            os.close(src)
            output.flush()
            os.fsync(fd)
    finally:
        os.close(fd)
    return file_digest(path, 1024 * 1024 * 1024)


def docker_image(rootfs_tar, archive_path, env, logs):
    changes = ['ENTRYPOINT ["' + ENTRYPOINT + '"]', 'USER 10002:10002'] + ['ENV ' + item for item in IMAGE_ENV]
    args = ['/usr/bin/docker', 'image', 'import']
    for change in changes:
        args.extend(('--change', change))
    args.append('-')
    with open(rootfs_tar, 'rb') as source:
        imported = command(args, env, timeout=1800, stdin=source)
    write_new(logs / 'docker-import.stdout', imported.stdout)
    write_new(logs / 'docker-import.stderr', imported.stderr)
    image_id = imported.stdout.decode('ascii', 'strict').strip()
    require(imported.returncode == 0 and re.fullmatch(r'sha256:[0-9a-f]{64}', image_id) is not None,
            'local browser image import failed')
    inspected = command(['/usr/bin/docker', 'image', 'inspect', image_id], env, timeout=60)
    write_new(logs / 'docker-inspect.stdout', inspected.stdout)
    write_new(logs / 'docker-inspect.stderr', inspected.stderr)
    require(inspected.returncode == 0, 'local browser image inspect failed')
    value = strict_json(inspected.stdout)
    require(type(value) is list and len(value) == 1 and type(value[0]) is dict, 'invalid browser image inspect')
    verify_config(value[0], image_id, inspect=True)
    fd = os.open(archive_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(fd, 'wb', closefd=False) as output:
            try:
                saved = subprocess.run(['/usr/bin/docker', 'image', 'save', image_id], env=env, cwd=RUN,
                                       stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.PIPE,
                                       timeout=1800, check=False)
            except (OSError, subprocess.TimeoutExpired) as exc:
                raise Failure('local browser image export failed') from exc
            output.flush()
            os.fsync(fd)
        require(len(saved.stderr) <= 2 * 1024 * 1024, 'browser export log exceeds limit')
        write_new(logs / 'docker-save.stderr', saved.stderr)
        require(saved.returncode == 0 and os.fstat(fd).st_size > 0, 'local browser image export failed')
    finally:
        os.close(fd)
    return image_id


def verify_config(value, image_id, inspect=False):
    if inspect:
        require(value.get('Id') == image_id and value.get('Os') == 'linux' and value.get('Architecture') == 'amd64',
                'browser image platform differs')
        config = value.get('Config')
    else:
        require(value.get('os') == 'linux' and value.get('architecture') == 'amd64', 'browser image platform differs')
        config = value.get('config')
    require(type(config) is dict and config.get('Entrypoint') == [ENTRYPOINT] and config.get('User') == '10002:10002' and
            config.get('Cmd') in (None, []) and config.get('WorkingDir') in (None, '', '/') and
            type(config.get('Env')) is list and len(config['Env']) == len(IMAGE_ENV) and
            set(config['Env']) == set(IMAGE_ENV) and not config.get('Volumes') and not config.get('OnBuild'),
            'browser image configuration differs')


def archive_name(value):
    require(type(value) is str and value and not value.startswith('/') and '\0' not in value, 'unsafe archive name')
    while value.startswith('./'):
        value = value[2:]
    value = value.rstrip('/')
    if value in ('', '.'):
        return ''
    checked_relative(value)
    return value


def tar_member(tar, name, limit):
    checked_relative(name)
    item = tar.getmember(name)
    require(item.isfile() and 0 <= item.size <= limit, 'invalid image archive member')
    source = tar.extractfile(item)
    require(source is not None, 'missing image archive member')
    with source:
        raw = source.read(limit + 1)
    require(len(raw) == item.size, 'short image archive member')
    return raw


def verify_layer(source, tree, diff_id):
    seen = set()
    reader = DigestReader(source, 1024 * 1024 * 1024)
    with tarfile.open(fileobj=reader, mode='r|') as contents:
        for member in contents:
            name = archive_name(member.name)
            if not name:
                require(member.isdir(), 'invalid layer root')
                continue
            require(name not in seen and name in tree and member.uid == 0 and member.gid == 0,
                    'unexpected browser layer entry')
            seen.add(name)
            expected = tree[name]
            require(member.mode == expected['mode'], 'browser layer mode differs')
            if expected['type'] == 'dir':
                require(member.isdir(), 'browser layer directory differs')
            elif expected['type'] == 'symlink':
                require(member.issym() and member.linkname == expected['target'], 'browser layer link differs')
            else:
                require(member.isfile() and member.size == expected['size'], 'browser layer file differs')
                entry = contents.extractfile(member)
                require(entry is not None, 'browser layer file missing')
                h, size = hashlib.sha256(), 0
                with entry:
                    while True:
                        chunk = entry.read(1024 * 1024)
                        if not chunk:
                            break
                        size += len(chunk)
                        require(size <= expected['size'], 'browser layer file exceeds bound')
                        h.update(chunk)
                require(size == expected['size'] and h.hexdigest() == expected['sha256'], 'browser layer bytes differ')
    while reader.read(1024 * 1024):
        pass
    require(seen == set(tree) and 'sha256:' + reader.hash.hexdigest() == diff_id,
            'browser layer inventory or digest differs')
    return reader.size


def verify_image_archive(path, image_id, tree):
    with tarfile.open(path, mode='r:') as archive:
        names = [member.name for member in archive]
        require(len(names) == len(set(names)) and 'manifest.json' in names, 'image archive inventory invalid')
        manifest = strict_json(tar_member(archive, 'manifest.json', 1024 * 1024))
        require(type(manifest) is list and len(manifest) == 1 and type(manifest[0]) is dict, 'image archive manifest invalid')
        entry = manifest[0]
        require(entry.get('RepoTags') in (None, []) and type(entry.get('Config')) is str and
                type(entry.get('Layers')) is list and len(entry['Layers']) == 1 and type(entry['Layers'][0]) is str,
                'image archive is not one untagged image')
        config_raw = tar_member(archive, entry['Config'], 8 * 1024 * 1024)
        require(hashlib.sha256(config_raw).hexdigest() == image_id[7:], 'image archive configuration digest differs')
        config = strict_json(config_raw)
        require(type(config) is dict, 'invalid image archive configuration')
        verify_config(config, image_id)
        diff_ids = config.get('rootfs', {}).get('diff_ids')
        require(type(diff_ids) is list and len(diff_ids) == 1 and
                type(diff_ids[0]) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', diff_ids[0]),
                'image layer identity unavailable')
        layer_name = checked_relative(entry['Layers'][0])
        layer = archive.getmember(layer_name)
        require(layer.isfile() and 0 < layer.size <= 1024 * 1024 * 1024, 'invalid browser layer')
        stream = archive.extractfile(layer)
        require(stream is not None, 'browser layer unavailable')
        with stream:
            magic = stream.read(4)
            stream.seek(0)
            require(magic != b'\x28\xb5\x2f\xfd', 'unsupported browser layer compression')
            compressed = magic.startswith(b'\x1f\x8b')
            decoded = gzip.GzipFile(fileobj=stream, mode='rb') if compressed else stream
            try:
                uncompressed_size = verify_layer(decoded, tree, diff_ids[0])
            finally:
                if compressed:
                    decoded.close()
            require(stream.tell() == layer.size and (compressed or uncompressed_size == layer.size),
                    'browser layer length differs')
        return {'config': entry['Config'], 'layer': layer_name, 'diff_id': diff_ids[0]}


def build_attempt(attempt, attempt_id):
    events = Events(attempt / 'events.jsonl')
    stage = 'starting'
    try:
        events.add('IN_PROGRESS', stage)
        root, logs, home = attempt / 'rootfs', attempt / 'inspection', attempt / 'home'
        for directory in (root, logs, home):
            os.mkdir(directory, 0o700)
        sync_dir(attempt)
        env = {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'DOCKER_CONFIG': str(home / 'docker'),
               'LANG': 'C', 'LC_ALL': 'C', 'TZ': 'UTC'}
        sources = Sources()
        tool_sources = {}
        for tool in TOOLS:
            _, item = sources.pin(tool, (Path('/usr/bin'), Path('/usr/lib')))
            tool_sources[tool] = item['sha256']
        script_sha, _ = file_digest(SOURCE_ROOT / 'browser_negative.py', 1024 * 1024)
        stage = 'bundle'
        events.add('IN_PROGRESS', stage)
        tree = {}
        for directory in ('dev', 'proc', 'sys', 'etc', 'tmp', 'lib64', 'opt/g2/browser', 'opt/g2/runtime/lib'):
            ensure_tree_dir(root, tree, directory, 0o1777 if directory == 'tmp' else 0o755)
        bundle, elfs = install_bundle(sources, root, tree)
        bundle_sha = hashlib.sha256(canonical(bundle)).hexdigest()
        stage = 'elf_closure'
        events.add('IN_PROGRESS', stage)
        inspections, aliases = install_libraries(sources, root, tree, elfs, env, logs)
        browser_source = str(sources.resolve(BUNDLE / PROGRAM, (BUNDLE,)))
        require(inspections.get(browser_source, {}).get('interpreter') == INTERPRETER, 'pinned browser interpreter missing')
        validate_tree(root, tree)
        for current, _, _ in os.walk(root, topdown=False, followlinks=False):
            sync_dir(current)
        stage = 'rootfs_tar'
        events.add('IN_PROGRESS', stage)
        rootfs_sha, rootfs_size = make_rootfs_tar(root, tree, attempt / 'rootfs.tar')
        validate_tree(root, tree)
        stage = 'local_image'
        events.add('IN_PROGRESS', stage)
        image_id = docker_image(attempt / 'rootfs.tar', attempt / 'browser-image.tar', env, logs)
        image_sha, image_size = file_digest(attempt / 'browser-image.tar', 1024 * 1024 * 1024)
        image_contents = verify_image_archive(attempt / 'browser-image.tar', image_id, tree)
        sources.verify()
        require(file_digest(SOURCE_ROOT / 'browser_negative.py', 1024 * 1024)[0] == script_sha, 'builder script changed')
        tree_sha = hashlib.sha256(canonical(tree)).hexdigest()
        manifest = {'version': 1, 'run_token': TOKEN, 'attempt_id': attempt_id, 'program_path': ENTRYPOINT,
                    'program_sha256': PROGRAM_SHA256, 'bundle_path': str(BUNDLE), 'bundle': bundle,
                    'browser_revision': BROWSER_REVISION, 'expected_browser_version': EXPECTED_BROWSER_VERSION,
                    'bundle_sha256': bundle_sha, 'tool_sources': tool_sources, 'source_nodes': sources.nodes,
                    'source_files': sources.files, 'elf_inspections': inspections, 'library_aliases': aliases,
                    'rootfs_tree': tree, 'rootfs_tree_sha256': tree_sha, 'rootfs_tar_size': rootfs_size,
                    'image_archive_size': image_size, 'image_contents': image_contents,
                    'image_environment': list(IMAGE_ENV)}
        manifest_body = canonical(manifest)
        write_new(attempt / 'manifest.json', manifest_body, 0o400)
        manifest_sha = hashlib.sha256(manifest_body).hexdigest()
        events.add('PASS', 'complete')
        events.close()
        events_sha, _ = file_digest(attempt / 'events.jsonl', 1024 * 1024)
        receipt = {'version': 1, 'run_token': TOKEN, 'attempt_id': attempt_id, 'status': 'PASS',
                   'builder_sha256': script_sha, 'manifest_sha256': manifest_sha, 'rootfs_sha256': rootfs_sha,
                   'image_id': image_id, 'image_archive_sha256': image_sha, 'program_sha256': PROGRAM_SHA256,
                   'bundle_sha256': bundle_sha, 'events_sha256': events_sha}
        write_new(attempt / 'receipt.json', canonical(receipt), 0o400)
        sync_dir(attempt)
        return receipt
    except BaseException as exc:
        if events.fd is not None:
            try:
                events.add('FAIL', stage)
            finally:
                events.close()
        failed = {'version': 1, 'run_token': TOKEN, 'attempt_id': attempt_id, 'status': 'FAIL',
                  'stage': stage, 'error_type': type(exc).__name__}
        try:
            write_new(attempt / 'receipt.json', canonical(failed), 0o400)
            sync_dir(attempt)
        except (OSError, Failure):
            pass
        raise


def selected_browser():
    """Return the latest validated selection and fixed artifact paths; read only.

    Call under source-vm.lock when coordinating with a source build. Selection
    and receipt objects have exact fixed keys; their digests bind the immutable
    rootfs tar, browser image archive, pinned executable, bundle, and manifest.
    """
    check_owned_run(str(RUN))
    selections = RUN / 'browser-build-selections'
    indices = numbered(selections, 'selection-', '.json', False)
    require(indices, 'no browser image selection exists')
    selection_id = '%020d' % indices[-1]
    path = selections / ('selection-' + selection_id + '.json')
    raw = read_small(path, 4096, 0o400)
    selection = strict_json(raw)
    keys = {'version', 'run_token', 'selection_id', 'attempt_id', 'receipt_sha256', 'manifest_sha256',
            'rootfs_sha256', 'image_id', 'image_archive_sha256', 'program_sha256', 'bundle_sha256'}
    require(type(selection) is dict and set(selection) == keys and type(selection['version']) is int and
            selection['version'] == 1 and selection['run_token'] == TOKEN and selection['selection_id'] == selection_id and
            type(selection['attempt_id']) is str and NUMBER.fullmatch(selection['attempt_id']) and
            type(selection['image_id']) is str and re.fullmatch(r'sha256:[0-9a-f]{64}', selection['image_id']) and
            all(type(selection[key]) is str and HEX.fullmatch(selection[key]) for key in
                ('receipt_sha256', 'manifest_sha256', 'rootfs_sha256', 'image_archive_sha256', 'program_sha256', 'bundle_sha256')) and
            selection['program_sha256'] == PROGRAM_SHA256 and canonical(selection) == raw, 'browser selection invalid')
    attempt = RUN / 'browser-build-attempts' / ('attempt-' + selection['attempt_id'])
    private_dir(RUN / 'browser-build-attempts', False)
    private_dir(attempt, False)
    receipt_path, manifest_path = attempt / 'receipt.json', attempt / 'manifest.json'
    receipt_raw = read_small(receipt_path, 8192, 0o400)
    require(hashlib.sha256(receipt_raw).hexdigest() == selection['receipt_sha256'], 'browser receipt digest differs')
    receipt = strict_json(receipt_raw)
    receipt_keys = {'version', 'run_token', 'attempt_id', 'status', 'builder_sha256', 'manifest_sha256', 'rootfs_sha256',
                    'image_id', 'image_archive_sha256', 'program_sha256', 'bundle_sha256', 'events_sha256'}
    require(type(receipt) is dict and set(receipt) == receipt_keys and receipt.get('version') == 1 and
            receipt.get('run_token') == TOKEN and receipt.get('attempt_id') == selection['attempt_id'] and
            receipt.get('status') == 'PASS' and canonical(receipt) == receipt_raw and
            all(receipt[key] == selection[key] for key in ('manifest_sha256', 'rootfs_sha256', 'image_id',
                                                            'image_archive_sha256', 'program_sha256', 'bundle_sha256')) and
            type(receipt['builder_sha256']) is str and HEX.fullmatch(receipt['builder_sha256']) and
            type(receipt['events_sha256']) is str and HEX.fullmatch(receipt['events_sha256']), 'browser receipt invalid')
    check_events(attempt / 'events.jsonl', receipt['events_sha256'])
    manifest_raw = read_small(manifest_path, 16 * 1024 * 1024, 0o400)
    require(hashlib.sha256(manifest_raw).hexdigest() == selection['manifest_sha256'], 'browser manifest digest differs')
    manifest = strict_json(manifest_raw)
    manifest_keys = {'version', 'run_token', 'attempt_id', 'program_path', 'program_sha256', 'bundle_path', 'bundle',
                     'browser_revision', 'expected_browser_version',
                     'bundle_sha256', 'tool_sources', 'source_nodes', 'source_files', 'elf_inspections', 'library_aliases',
                     'rootfs_tree', 'rootfs_tree_sha256', 'rootfs_tar_size', 'image_archive_size', 'image_contents',
                     'image_environment'}
    require(type(manifest) is dict and set(manifest) == manifest_keys and manifest.get('version') == 1 and
            manifest.get('run_token') == TOKEN and manifest.get('attempt_id') == selection['attempt_id'] and
            manifest.get('program_path') == ENTRYPOINT and manifest.get('program_sha256') == PROGRAM_SHA256 and
            manifest.get('browser_revision') == BROWSER_REVISION and
            manifest.get('expected_browser_version') == EXPECTED_BROWSER_VERSION and
            manifest.get('bundle_path') == str(BUNDLE) and manifest.get('image_environment') == list(IMAGE_ENV) and
            type(manifest.get('bundle')) is dict and manifest['bundle'].get(PROGRAM, {}).get('sha256') == PROGRAM_SHA256 and
            hashlib.sha256(canonical(manifest['bundle'])).hexdigest() == selection['bundle_sha256'] == manifest['bundle_sha256'] and
            type(manifest.get('rootfs_tree')) is dict and
            manifest['rootfs_tree'].get('opt/g2/browser/' + PROGRAM, {}).get('sha256') == PROGRAM_SHA256 and
            hashlib.sha256(canonical(manifest['rootfs_tree'])).hexdigest() == manifest['rootfs_tree_sha256'] and
            canonical(manifest) == manifest_raw, 'browser manifest invalid')
    rootfs_path, archive_path = attempt / 'rootfs.tar', attempt / 'browser-image.tar'
    rootfs_sha, rootfs_size = file_digest(rootfs_path, 1024 * 1024 * 1024)
    archive_sha, archive_size = file_digest(archive_path, 1024 * 1024 * 1024)
    require(rootfs_sha == selection['rootfs_sha256'] and rootfs_size == manifest['rootfs_tar_size'] and
            archive_sha == selection['image_archive_sha256'] and archive_size == manifest['image_archive_size'],
            'selected browser archives changed')
    return {'selection': selection, 'selection_path': str(path), 'selection_sha256': hashlib.sha256(raw).hexdigest(),
            'manifest_path': str(manifest_path), 'receipt_path': str(receipt_path), 'image_archive_path': str(archive_path)}


def prepare(argument):
    check_owned_run(argument)
    lock = lock_run()
    try:
        attempts = RUN / 'browser-build-attempts'
        indices = numbered(attempts, 'attempt-', '', True)
        attempt_id = '%020d' % (len(indices) + 1)
        attempt = attempts / ('attempt-' + attempt_id)
        os.mkdir(attempt, 0o700)
        sync_dir(attempts)
        receipt = build_attempt(attempt, attempt_id)
        selections = RUN / 'browser-build-selections'
        indices = numbered(selections, 'selection-', '.json', True)
        selection_id = '%020d' % (len(indices) + 1)
        receipt_sha, _ = file_digest(attempt / 'receipt.json', 8192)
        selection = {'version': 1, 'run_token': TOKEN, 'selection_id': selection_id, 'attempt_id': attempt_id,
                     'receipt_sha256': receipt_sha, 'manifest_sha256': receipt['manifest_sha256'],
                     'rootfs_sha256': receipt['rootfs_sha256'], 'image_id': receipt['image_id'],
                     'image_archive_sha256': receipt['image_archive_sha256'], 'program_sha256': receipt['program_sha256'],
                     'bundle_sha256': receipt['bundle_sha256']}
        write_new(selections / ('selection-' + selection_id + '.json'), canonical(selection), 0o400)
        sync_dir(selections)
        print(json.dumps({'status': 'PASS', 'selection_id': selection_id, 'attempt_id': attempt_id,
                          'image_id': selection['image_id'], 'image_archive_sha256': selection['image_archive_sha256']}))
        return 0
    finally:
        os.close(lock)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    item = sub.add_parser('prepare')
    item.add_argument('--run-dir', required=True)
    args = parser.parse_args()
    return prepare(args.run_dir)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (Failure, OSError, ValueError, KeyError, TypeError, tarfile.TarError) as exc:
        print('PREREQUISITE_FAILED: ' + str(exc)[:512], file=sys.stderr)
        sys.exit(1)
