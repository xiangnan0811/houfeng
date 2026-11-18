#!/usr/bin/env python3
"""Single-use task-local acquisition, build and offline VM supervision."""
import argparse
import ast
import base64
import binascii
import errno
import fcntl
import email.utils
import resource
import select
import selectors
import shutil
import signal
import time
import hashlib
import http.client
import json
import os
import posixpath
import re
import ssl
import stat
import subprocess
import sys
import tarfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
import prepare_import
import preparation
import verify_handoff

RUN = Path('/home/murray/code/houfeng/.worktree/g2-durable-deletion-integrated/tmp/agent/g2-isolated-capability-89ff3c589b2b4dc2b9ee8b9c33465b2b')
TOKEN = '89ff3c589b2b4dc2b9ee8b9c33465b2b'
PREPARER_PATH = '/opt/g2/preparer'
OLD = RUN.parent / 'g2-isolated-capability-f3240b1b4e1c44c8bf3dfcb8087b5757'
OLD_TOKEN = 'f3240b1b4e1c44c8bf3dfcb8087b5757'
GO = Path('/home/murray/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.2.linux-amd64/bin/go')
QEMU = 'https://download.qemu.org/qemu-11.1.2.tar.xz'
QEMU_SHA256 = '731b5681e4bb18be313231579b8efd0296c5b015fa36dc533874b639ba838016'
QEMU_SIZE = 141815216
ROOT = 'qemu-11.1.2'
OMITTED_NAME = ROOT + '/roms/edk2/EmulatorPkg/Unix/Host/X11IncludeHack'
OMITTED = {'name': OMITTED_NAME, 'type': 'symlink', 'target': '/opt/X11/include',
           'reason': 'unused_macos_edk2_emulator_include'}
TYPE_COUNTS = {'dir': 6317, 'file': 78695, 'symlink': 72}
UBUNTU = 'https://cloud-images.ubuntu.com/releases/noble/release-20261017/'
DOCKER = 'https://download.docker.com/linux/ubuntu/'
KEYS = {
    'qemu': ('https://keys.openpgp.org/vks/v1/by-fingerprint/CEACC9E15534EBABB82D3FA03353C9CEF108B584', 'CEACC9E15534EBABB82D3FA03353C9CEF108B584'),
    'ubuntu': ('https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xD2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81', 'D2EB44626FDDC30B513D5BB71A5D6C4C7DB87C81'),
    'docker': (DOCKER + 'gpg', '9DC858229FC7DD38854AE2D88D81803C0EBFCD88'),
}
IMAGE = 'ubuntu-24.04-server-cloudimg-amd64.img'
MANIFEST = 'ubuntu-24.04-server-cloudimg-amd64.manifest'
UBUNTU_HASHES = {
    IMAGE: '04e8f2461a16a44e78a370284471f29616cdd9baf1893df25807b71f985b6c94',
    MANIFEST: '78d7da6ab1556daa6c0f685bf4e0e3da36395f170e8fad411d7ba032f14d08c2',
}
DEBS = {
    'docker-ce': ('5:29.8.2-1~ubuntu.24.04~noble', 'dists/noble/pool/stable/amd64/docker-ce_29.8.2-1~ubuntu.24.04~noble_amd64.deb', 'b53a44a6ff13277710978adbdbfea27fcdd7ac9f198017e733d2b8dc2343f8dc'),
    'docker-ce-cli': ('5:29.8.2-1~ubuntu.24.04~noble', 'dists/noble/pool/stable/amd64/docker-ce-cli_29.8.2-1~ubuntu.24.04~noble_amd64.deb', '008b3b49b474136063fe0fbe8999b0e77c3e8556d7f7de21f98f05715e2f76ba'),
    'containerd.io': ('2.3.6-1~ubuntu.24.04~noble', 'dists/noble/pool/stable/amd64/containerd.io_2.3.6-1~ubuntu.24.04~noble_amd64.deb', '2eb8c6e244fe6886f2fa2eee9ec418c4b9bb44eb44fca748504f57c23341aed2'),
}


class Failure(Exception):
    pass


class TransportFailure(Exception):
    pass


def check_path(path, directory=True):
    """Reject symlinks in every component, including ancestors of RUN."""
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts:
        raise Failure('path is not an absolute normalized path: ' + str(path))
    current = Path('/')
    for part in path.parts[1:]:
        current = current / part
        info = os.lstat(current)
        if stat.S_ISLNK(info.st_mode):
            raise Failure('symlink in path: ' + str(current))
    info = os.lstat(path)
    if directory and not stat.S_ISDIR(info.st_mode):
        raise Failure('not a directory: ' + str(path))
    if not directory and not stat.S_ISREG(info.st_mode):
        raise Failure('not a regular file: ' + str(path))


def validate_run(argument):
    if argument != str(RUN) or str(Path(__file__).absolute()) != str(RUN / 'host.py'):
        raise Failure('run-dir must be the fixed directory containing this script')
    if RUN.name != 'g2-isolated-capability-' + TOKEN or not re.fullmatch('[0-9a-f]{32}', TOKEN):
        raise Failure('invalid run token')
    check_path(RUN)
    check_path(RUN / 'host.py', False)
    info = os.stat(RUN, follow_symlinks=False)
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700 or os.geteuid() == 0:
        raise Failure('run directory must be owned by the non-root caller and mode 0700')
    check_path(RUN / 'preflight.json', False)
    with (RUN / 'preflight.json').open('r', encoding='utf-8') as f:
        preflight = json.load(f)
    if preflight.get('run_dir') != str(RUN) or preflight.get('run_token') != TOKEN or preflight.get('passed') is not True:
        raise Failure('preflight evidence does not match this run')


def task_environment():
    paths = {
        'HOME': 'home', 'TMPDIR': 'tmp', 'TMP': 'tmp', 'TEMP': 'tmp',
        'GOCACHE': 'cache/go-build', 'GOMODCACHE': 'cache/go-mod', 'GOPATH': 'gopath',
        'XDG_CACHE_HOME': 'cache/xdg', 'XDG_CONFIG_HOME': 'config',
        'GNUPGHOME': 'gnupg', 'PYTHONPYCACHEPREFIX': 'cache/python',
    }
    for relative in sorted(set(paths.values())):
        current = RUN
        for part in Path(relative).parts:
            current = current / part
            try:
                os.mkdir(current, 0o700)
            except FileExistsError:
                pass
            check_path(current)
            st = os.stat(current, follow_symlinks=False)
            require(st.st_uid == os.geteuid() and stat.S_IMODE(st.st_mode) == 0o700,
                    'unsafe private environment directory: ' + str(current))
    env = {name: str(RUN / relative) for name, relative in paths.items()}
    env.update({'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
                'PYTHONNOUSERSITE': '1', 'PYTHONDONTWRITEBYTECODE': '1',
                'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': '', 'GOTOOLCHAIN': 'local',
                'GOPROXY': 'off', 'GOSUMDB': 'off', 'GOPRIVATE': '', 'GONOPROXY': '',
                'GONOSUMDB': '', 'GOINSECURE': ''})
    os.umask(0o077)
    return env


def exclusive(path):
    return os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb')


def digest(path):
    h = hashlib.sha256()
    size = 0
    with path.open('rb') as f:
        while True:
            block = f.read(1024 * 1024)
            if not block:
                break
            h.update(block)
            size += len(block)
    return h.hexdigest(), size


class SameOriginRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        old = urllib.parse.urlsplit(req.full_url)
        new = urllib.parse.urlsplit(newurl)
        if (new.scheme != 'https' or new.hostname != old.hostname or new.port not in (None, 443)
                or new.username is not None or new.password is not None):
            raise Failure('redirect outside HTTPS origin: ' + newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class Fetcher:
    def __init__(self, env, result, result_file):
        self.env = env
        self.result = result
        self.result_file = result_file
        self.assets = RUN / 'assets'
        self.logs = RUN / 'fetch-logs'
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                                  urllib.request.HTTPSHandler(context=ssl.create_default_context()),
                                                  SameOriginRedirect())
        self.counter = 0

    def save(self):
        data = (json.dumps(self.result, indent=2, sort_keys=True) + '\n').encode()
        self.result_file.seek(0)
        self.result_file.write(data)
        self.result_file.truncate()
        self.result_file.flush()
        os.fsync(self.result_file.fileno())

    def download(self, url, name, maximum):
        parts = urllib.parse.urlsplit(url)
        allowed = {'download.qemu.org', 'keys.openpgp.org', 'cloud-images.ubuntu.com',
                   'keyserver.ubuntu.com', 'download.docker.com'}
        if (parts.scheme != 'https' or parts.hostname not in allowed or parts.port not in (None, 443)
                or parts.username is not None or parts.password is not None):
            raise Failure('disallowed download URL: ' + url)

        def single_header(headers, name):
            values = headers.get_all(name) or []
            if len(values) > 1:
                raise Failure('multiple %s headers: %s' % (name, url))
            return values[0] if values else None

        def decimal(value, label):
            if not re.fullmatch(r'[0-9]+', value):
                raise Failure('invalid %s: %s' % (label, url))
            try:
                return int(value)
            except ValueError as exc:
                raise Failure('invalid %s: %s' % (label, url)) from exc

        def usable_date(value):
            if value is None:
                return False
            try:
                parsed = email.utils.parsedate_to_datetime(value)
                return parsed.tzinfo is not None and parsed.utcoffset().total_seconds() == 0
            except (TypeError, ValueError, OverflowError):
                return False

        for attempt in (1, 2):
            path = self.assets / (name + '.attempt' + str(attempt))
            entry = {'url': url, 'path': str(path), 'attempt': attempt, 'status': 'started',
                     'segments': []}
            self.result['downloads'].append(entry)
            self.save()
            try:
                with exclusive(path) as out:
                    count = 0
                    original_length = None
                    initial_etag = None
                    initial_modified = None
                    validator = None
                    for request_index in range(3):
                        headers = {'User-Agent': 'g2-isolated-fetch/1'}
                        if request_index:
                            headers['Range'] = 'bytes=%d-' % count
                            headers['If-Range'] = validator
                        segment = {'requested_offset': count, 'range': headers.get('Range'),
                                   'if_range': headers.get('If-Range'), 'count_before': count,
                                   'count_after': count, 'status': 'started'}
                        entry['segments'].append(segment)
                        self.save()

                        def record_response(response):
                            segment['final_url'] = response.geturl()
                            segment['http_status'] = response.status
                            segment['response_headers'] = {
                                key: values for key in ('Content-Length', 'Content-Range', 'ETag',
                                                        'Last-Modified', 'Transfer-Encoding', 'Content-Encoding')
                                if (values := response.headers.get_all(key))
                            }
                            segment['status'] = 'response'
                            if request_index == 0:
                                entry['final_url'] = segment['final_url']
                                entry['http_status'] = segment['http_status']
                            self.save()

                        def append(chunk):
                            nonlocal count
                            next_count = count + len(chunk)
                            if next_count > maximum or (original_length is not None and next_count > original_length):
                                raise Failure('download size limit exceeded: ' + url)
                            out.write(chunk)
                            count = next_count

                        try:
                            try:
                                response = self.opener.open(urllib.request.Request(url, headers=headers), timeout=120)
                            except urllib.error.HTTPError:
                                raise
                            except (urllib.error.URLError, OSError, http.client.HTTPException) as exc:
                                raise TransportFailure(repr(exc)) from exc
                            with response:
                                record_response(response)
                                length = single_header(response.headers, 'Content-Length')
                                if request_index == 0:
                                    if response.status != 200:
                                        raise Failure('initial response is not HTTP 200: ' + url)
                                    etags = response.headers.get_all('ETag') or []
                                    modified_values = response.headers.get_all('Last-Modified') or []
                                    etag = etags[0] if len(etags) == 1 else None
                                    modified = modified_values[0] if len(modified_values) == 1 else None
                                    if length is not None:
                                        original_length = decimal(length, 'Content-Length')
                                        if original_length > maximum:
                                            raise Failure('invalid or excessive Content-Length: ' + url)
                                    initial_etag, initial_modified = etag, modified
                                    if etag is not None and re.fullmatch(r'"[\x21\x23-\x7e\x80-\xff]*"', etag):
                                        validator = etag
                                    elif usable_date(modified):
                                        validator = modified
                                else:
                                    if response.status != 206:
                                        raise Failure('continuation response is not HTTP 206: ' + url)
                                    if response.geturl() != entry['final_url']:
                                        raise Failure('continuation final URL changed: ' + url)
                                    etag = single_header(response.headers, 'ETag')
                                    modified = single_header(response.headers, 'Last-Modified')
                                    content_range = single_header(response.headers, 'Content-Range')
                                    match = (re.fullmatch(r'bytes ([0-9]+)-([0-9]+)/([0-9]+)', content_range,
                                                          flags=re.IGNORECASE)
                                             if content_range is not None else None)
                                    if match is None or tuple(decimal(value, 'Content-Range') for value in match.groups()) != (
                                            count, original_length - 1, original_length):
                                        raise Failure('invalid or mismatched Content-Range: ' + url)
                                    if length is not None and decimal(length, 'Content-Length') != original_length - count:
                                        raise Failure('mismatched continuation Content-Length: ' + url)
                                    if ((etag is not None and initial_etag is not None and etag != initial_etag)
                                            or (modified is not None and initial_modified is not None
                                                and modified != initial_modified)):
                                        raise Failure('changed continuation validator: ' + url)
                                while True:
                                    try:
                                        chunk = response.read(1024 * 1024)
                                    except http.client.IncompleteRead as exc:
                                        append(exc.partial)
                                        raise TransportFailure(repr(exc)) from exc
                                    except (urllib.error.URLError, OSError, http.client.HTTPException) as exc:
                                        raise TransportFailure(repr(exc)) from exc
                                    if not chunk:
                                        break
                                    append(chunk)
                                if original_length is not None and count != original_length:
                                    raise TransportFailure('incomplete response: expected %d bytes, got %d' %
                                                           (original_length, count))
                        except urllib.error.HTTPError as exc:
                            record_response(exc)
                            segment.update(status='http_error', error=str(exc), count_after=count)
                            self.save()
                            exc.close()
                            raise
                        except TransportFailure as exc:
                            segment.update(status='transport_error', error=str(exc), count_after=count)
                            self.save()
                            if (request_index < 2 and count > 0 and original_length is not None
                                    and count < original_length and validator is not None):
                                continue
                            raise
                        except Exception as exc:
                            segment.update(status='error', error=repr(exc), count_after=count)
                            self.save()
                            raise
                        else:
                            segment.update(status='completed', count_after=count)
                            self.save()
                            break
                    out.flush()
                    os.fsync(out.fileno())
                entry['sha256'], entry['size'] = digest(path)
                entry['status'] = 'downloaded'
                self.save()
                return path
            except urllib.error.HTTPError as exc:
                entry.update(status='http_error', error=str(exc), http_status=exc.code)
                self.save()
                raise Failure('HTTP failure for %s: %s' % (url, exc)) from exc
            except TransportFailure as exc:
                entry.update(status='transport_error', error=str(exc))
                if path.exists():
                    entry['partial_sha256'], entry['partial_size'] = digest(path)
                self.save()
                if attempt == 2:
                    raise Failure('transport failure for %s: %s' % (url, exc)) from exc
            except Exception as exc:
                entry.update(status='error', error=repr(exc))
                if path.exists():
                    entry['partial_sha256'], entry['partial_size'] = digest(path)
                self.save()
                raise
        raise Failure('download retry exhausted: ' + url)

    def command(self, label, args, pass_fds=()):
        self.counter += 1
        prefix = '%02d-%s' % (self.counter, label)
        entry = {'label': label, 'argv': args, 'stdout': str(self.logs / (prefix + '.stdout')),
                 'stderr': str(self.logs / (prefix + '.stderr')), 'status': 'started'}
        self.result['commands'].append(entry)
        self.save()
        try:
            with exclusive(Path(entry['stdout'])) as stdout, exclusive(Path(entry['stderr'])) as stderr:
                completed = subprocess.run(args, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr,
                                           env=self.env, cwd=str(RUN), timeout=120, check=False,
                                           pass_fds=pass_fds)
                stdout.flush()
                stderr.flush()
                os.fsync(stdout.fileno())
                os.fsync(stderr.fileno())
            entry['returncode'] = completed.returncode
            entry['status'] = 'completed'
            self.save()
            if completed.returncode != 0:
                raise Failure('%s failed with exit %d (see %s)' % (label, completed.returncode, entry['stderr']))
            return Path(entry['stdout']).read_bytes()
        except (OSError, subprocess.SubprocessError) as exc:
            entry.update(status='error', error=repr(exc))
            self.save()
            raise Failure('%s failed: %r' % (label, exc)) from exc

    def key(self, name):
        url, fingerprint = KEYS[name]
        key = self.download(url, name + '.key', 4 * 1024 * 1024)
        gpg = ['/usr/bin/gpg', '--no-options', '--batch', '--no-autostart', '--no-auto-key-retrieve',
               '--homedir', self.env['GNUPGHOME']]
        output = self.command(name + '-fingerprint', gpg + ['--with-colons', '--show-keys', str(key)])
        primaries = []
        awaiting_primary = False
        for line in output.decode('utf-8', 'strict').splitlines():
            fields = line.split(':')
            if fields[0] == 'pub':
                awaiting_primary = True
            elif fields[0] == 'sub':
                awaiting_primary = False
            elif fields[0] == 'fpr' and awaiting_primary:
                primaries.append(fields[9].upper())
                awaiting_primary = False
        if primaries != [fingerprint]:
            raise Failure('%s key primary fingerprint mismatch: %r' % (name, primaries))
        self.command(name + '-import', gpg + ['--import', str(key)])
        exported = self.command(name + '-export', gpg + ['--export', fingerprint])
        if not exported:
            raise Failure(name + ' exported key is empty')
        keyring = self.assets / (name + '.verified.gpg')
        with exclusive(keyring) as f:
            f.write(exported)
            f.flush()
            os.fsync(f.fileno())
        self.result['keys'][name] = {'primary_fingerprint': fingerprint, 'keyring': str(keyring),
                                     'keyring_sha256': digest(keyring)[0]}
        self.save()
        return keyring

    def signature(self, name, keyring, signed, detached=None, pass_fds=()):
        args = ['/usr/bin/gpgv', '--homedir', self.env['GNUPGHOME'], '--keyring', str(keyring), '--status-fd', '1']
        args += [str(detached), str(signed)] if detached is not None else [str(signed)]
        status = self.command(name + '-signature', args, pass_fds).decode('utf-8', 'strict')
        valid = [line.split() for line in status.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')]
        expected = KEYS[name][1]
        if len(valid) != 1 or (valid[0][2].upper() != expected and valid[0][-1].upper() != expected):
            raise Failure(name + ' signature did not identify exactly one pinned primary key')
        self.result['signatures'][name] = {'signed': str(signed), 'signature': str(detached) if detached else None,
                                           'validsig': valid[0][2:], 'primary_fingerprint': expected}
        self.save()

    def check_hash(self, path, expected, label):
        actual, size = digest(path)
        entry = {'path': str(path), 'sha256': actual, 'expected': expected, 'size': size, 'match': actual == expected}
        self.result['hash_checks'][label] = entry
        self.save()
        if actual != expected:
            raise Failure(label + ' SHA256 mismatch: got ' + actual + ', expected ' + expected)


def cleartext(path):
    lines = path.read_bytes().splitlines()
    if not lines or lines[0] != b'-----BEGIN PGP SIGNED MESSAGE-----':
        raise Failure('invalid InRelease cleartext header')
    try:
        start = lines.index(b'') + 1
        end = lines.index(b'-----BEGIN PGP SIGNATURE-----', start)
    except ValueError as exc:
        raise Failure('invalid InRelease cleartext structure') from exc
    return b'\n'.join(line[2:] if line.startswith(b'- ') else line for line in lines[start:end]).decode('utf-8', 'strict')


def release_packages_entry(text):
    in_section = False
    found = []
    for line in text.splitlines():
        if line == 'SHA256:':
            if in_section:
                raise Failure('duplicate Release SHA256 section')
            in_section = True
            continue
        if in_section and line and not line[0].isspace():
            break
        if in_section:
            fields = line.split()
            if len(fields) == 3 and fields[2] == 'stable/binary-amd64/Packages':
                if not re.fullmatch('[0-9a-f]{64}', fields[0]) or not fields[1].isdigit():
                    raise Failure('invalid Release Packages hash or size')
                found.append((fields[0], int(fields[1])))
    if len(found) != 1:
        raise Failure('Release must contain exactly one Packages SHA256 entry')
    return found[0]


def ubuntu_sums(path):
    values = {}
    for line in path.read_text(encoding='utf-8').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})\s+\*?(.+)', line)
        if not match:
            raise Failure('malformed Ubuntu SHA256SUMS line')
        name = match.group(2)
        if name in values:
            raise Failure('duplicate Ubuntu SHA256SUMS entry: ' + name)
        values[name] = match.group(1)
    return values


def package_entries(path):
    matches = {name: [] for name in DEBS}
    for paragraph in path.read_text(encoding='utf-8').split('\n\n'):
        fields = {}
        for line in paragraph.splitlines():
            if not line or line[0].isspace():
                continue
            if ': ' not in line:
                raise Failure('malformed Packages field')
            name, value = line.split(': ', 1)
            if name in fields:
                raise Failure('duplicate Packages field: ' + name)
            fields[name] = value
        package = fields.get('Package')
        if package in matches and fields.get('Version') == DEBS[package][0] and fields.get('Architecture') == 'amd64':
            matches[package].append(fields)
    for package, entries in matches.items():
        if len(entries) != 1:
            raise Failure('missing or ambiguous fixed Docker package: ' + package)
        fields = entries[0]
        _, filename, sha = DEBS[package]
        if fields.get('Filename') != filename or fields.get('SHA256') != sha:
            raise Failure('fixed Docker package metadata mismatch: ' + package)
    return matches


class ArchiveFailure(Failure):
    def __init__(self, reason, detail=''):
        self.reason = reason
        super().__init__(reason + (': ' + str(detail) if detail else ''))


def member_kind(member):
    if member.isfile():
        return 'file'
    if member.isdir():
        return 'dir'
    if member.issym():
        return 'symlink'
    if member.islnk():
        raise ArchiveFailure('hardlink', member.name)
    raise ArchiveFailure('unsupported_type', member.name)


def validate_member_graph(members):
    """Pure metadata validation; callers must establish the signature and pin first."""
    entries = {}
    counts = {'dir': 0, 'file': 0, 'symlink': 0}
    for member in members:
        raw = member.name
        if not raw or raw.startswith('/') or '..' in raw.split('/'):
            raise ArchiveFailure('unsafe_member_path', raw)
        name = posixpath.normpath(raw)
        if name != ROOT and not name.startswith(ROOT + '/'):
            raise ArchiveFailure('outside_root', raw)
        if name in entries:
            raise ArchiveFailure('duplicate_member', name)
        if len(entries) >= 1000000:
            raise ArchiveFailure('excessive_members')
        kind = member_kind(member)
        if name == OMITTED_NAME:
            if raw != OMITTED_NAME or kind != 'symlink' or member.linkname != OMITTED['target'] or member.size != 0:
                raise ArchiveFailure('omitted_mismatch', raw)
        elif kind == 'symlink' and (not member.linkname or member.linkname.startswith('/')):
            raise ArchiveFailure('unsafe_link', name)
        entries[name] = (kind, member)
        counts[kind] += 1
    if OMITTED_NAME not in entries:
        raise ArchiveFailure('omitted_missing')
    if ROOT not in entries or entries[ROOT][0] != 'dir':
        raise ArchiveFailure('root_missing')
    for name, (kind, member) in entries.items():
        parent = posixpath.dirname(name)
        while parent:
            if parent not in entries:
                raise ArchiveFailure('missing_ancestor', name)
            if entries[parent][0] != 'dir':
                raise ArchiveFailure('non_directory_ancestor', name)
            parent = posixpath.dirname(parent)
    for name, (kind, member) in entries.items():
        if kind != 'symlink' or name == OMITTED_NAME:
            continue
        stack = posixpath.dirname(name).split('/')
        pending = member.linkname.split('/')
        hops = 0
        while pending:
            part = pending.pop(0)
            if part in ('', '.'):
                continue
            if part == '..':
                if len(stack) <= 1:
                    raise ArchiveFailure('escaping_link', name)
                stack.pop()
                continue
            stack.append(part)
            current = '/'.join(stack)
            if current == OMITTED_NAME:
                raise ArchiveFailure('link_to_omitted', name)
            entry = entries.get(current)
            if entry is None:
                raise ArchiveFailure('missing_link_target', name)
            if entry[0] == 'symlink':
                hops += 1
                if hops > 40:
                    raise ArchiveFailure('cyclic_link', name)
                stack.pop()
                pending = entry[1].linkname.split('/') + pending
            elif pending and entry[0] != 'dir':
                raise ArchiveFailure('link_non_directory', name)
        if entries['/'.join(stack)][0] not in ('file', 'dir'):
            raise ArchiveFailure('invalid_link_target', name)
    if len(entries) != 85084 or counts != TYPE_COUNTS:
        raise ArchiveFailure('member_counts', {'total': len(entries), 'types': counts})
    return entries


def digest_stream(stream):
    stream.seek(0)
    h = hashlib.sha256()
    size = 0
    while True:
        chunk = stream.read(1024 * 1024)
        if not chunk:
            break
        size += len(chunk)
        h.update(chunk)
    stream.seek(0)
    return h.hexdigest(), size


def require_task_path(path):
    path = Path(path)
    if not path.is_absolute() or '..' in path.parts or not path.is_relative_to(RUN) or path == RUN:
        raise ArchiveFailure('outside_task', path)
    return path


def safe_archive_open(path):
    path = require_task_path(path)
    check_path(path, False)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o022:
        os.close(fd)
        raise ArchiveFailure('unsafe_archive_file', path)
    return os.fdopen(fd, 'rb')


def archive_record(member):
    # Include all tar metadata, including PAX fields, in the pass comparison.
    return {**member.get_info(), 'pax_headers': dict(member.pax_headers)}


def canonical_json(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True) + '\n').encode()


def verify_source(container, manifest):
    """Check every entry without following links, including the absent omitted path."""
    container = require_task_path(container)
    check_path(container)
    info = os.stat(container, follow_symlinks=False)
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise ArchiveFailure('unsafe_source_container', container)
    expected = {item['path']: item for item in manifest['members']}
    if len(expected) != len(manifest['members']):
        raise ArchiveFailure('manifest_duplicate')
    for name in list(expected) + [item['name'] for item in manifest['omitted']]:
        if not name or name.startswith('/') or posixpath.normpath(name) != name or '..' in name.split('/'):
            raise ArchiveFailure('manifest_path', name)
    sizes = {}
    actual = set()
    def visit(directory, prefix):
        for entry in os.scandir(directory):
            name = prefix + '/' + entry.name if prefix else entry.name
            actual.add(name)
            item = expected.get(name)
            if item is None:
                raise ArchiveFailure('tree_extra', name)
            st = entry.stat(follow_symlinks=False)
            sizes[name] = st.st_size
            kind = ('dir' if stat.S_ISDIR(st.st_mode) else 'file' if stat.S_ISREG(st.st_mode)
                    else 'symlink' if stat.S_ISLNK(st.st_mode) else 'other')
            if kind != item['type'] or stat.S_IMODE(st.st_mode) != item['mode']:
                raise ArchiveFailure('tree_metadata', name)
            if kind == 'dir':
                visit(entry.path, name)
            elif kind == 'file':
                with os.fdopen(os.open(entry.path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as file:
                    sha, size = digest_stream(file)
                if sha != item['sha256'] or size != item['size']:
                    raise ArchiveFailure('tree_content', name)
            elif kind == 'symlink' and os.readlink(entry.path) != item['target']:
                raise ArchiveFailure('tree_link', name)
    visit(container, '')
    if actual != set(expected):
        raise ArchiveFailure('tree_missing', sorted(set(expected) - actual)[0])
    for name, size in sizes.items():
        if size != expected[name]['size']:
            raise ArchiveFailure('tree_size', name)
    for omitted in manifest['omitted']:
        parent = posixpath.dirname(omitted['name'])
        while parent:
            if expected.get(parent, {}).get('type') != 'dir':
                raise ArchiveFailure('omitted_parent', parent)
            parent = posixpath.dirname(parent)
        try:
            os.lstat(container / omitted['name'])
        except FileNotFoundError:
            pass
        else:
            raise ArchiveFailure('tree_omitted_present', omitted['name'])


def validate_archive(path, fetcher, keyring, signature, destination=None, manifest_path=None):
    """Authenticate and validate a held FD; optionally materialize exclusively."""
    with safe_archive_open(path) as stream:
        sha, size = digest_stream(stream)
        if sha != QEMU_SHA256:
            raise ArchiveFailure('archive_sha256', sha)
        if size != QEMU_SIZE:
            raise ArchiveFailure('archive_size', size)
        fetcher.signature('qemu', keyring, '/proc/self/fd/%d' % stream.fileno(),
                          signature, pass_fds=(stream.fileno(),))
        stream.seek(0)
        with tarfile.open(fileobj=stream, mode='r|xz') as archive:
            members = list(archive)
        entries = validate_member_graph(members)
        record = {'archive_sha256': sha, 'archive_size': size, 'member_count': len(members),
                  'type_counts': dict(TYPE_COUNTS), 'materialized_count': len(members) - 1,
                  'omitted': [dict(OMITTED)]}
        if destination is None:
            return record
        if manifest_path is None:
            raise ArchiveFailure('manifest_required')
        destination = require_task_path(destination)
        manifest_path = require_task_path(manifest_path)
        check_path(destination.parent)
        check_path(manifest_path.parent)
        os.mkdir(destination, 0o700)
        os.chmod(destination, 0o700)
        manifest = {'archive_sha256': sha, 'archive_size': size,
                    'omitted': [dict(OMITTED)], 'members': []}
        items = {}
        for name, (kind, member) in sorted(entries.items(), key=lambda item: (item[0].count('/'), item[0])):
            if name == OMITTED_NAME:
                continue
            item = {'path': name, 'type': kind, 'size': member.size,
                    'mode': 0o700 if kind == 'dir' or (kind == 'file' and member.mode & 0o111) else 0o600}
            if kind == 'file':
                item['archive_mode'] = member.mode & 0o7777
            if kind == 'dir':
                os.mkdir(destination / name, 0o700)
                os.chmod(destination / name, 0o700)
            elif kind == 'symlink':
                item['mode'] = 0o777
                item['target'] = member.linkname
            items[name] = item
        stream.seek(0)
        count = 0
        with tarfile.open(fileobj=stream, mode='r|xz') as archive:
            for member in archive:
                if count >= len(members) or archive_record(member) != archive_record(members[count]):
                    raise ArchiveFailure('second_pass_mismatch', count)
                count += 1
                name = posixpath.normpath(member.name)
                if name == OMITTED_NAME or not member.isfile():
                    continue
                h = hashlib.sha256()
                remaining = member.size
                source = archive.extractfile(member)
                if source is None:
                    raise ArchiveFailure('file_unreadable', name)
                with exclusive(destination / name) as out:
                    os.fchmod(out.fileno(), items[name]['mode'])
                    while remaining:
                        chunk = source.read(min(1024 * 1024, remaining))
                        if not chunk:
                            raise ArchiveFailure('file_truncated', name)
                        out.write(chunk)
                        h.update(chunk)
                        remaining -= len(chunk)
                    out.flush()
                    os.fsync(out.fileno())
                items[name]['sha256'] = h.hexdigest()
        if count != len(members) or digest_stream(stream) != (sha, size):
            raise ArchiveFailure('second_pass_mismatch', count)
        for name, item in items.items():
            if item['type'] == 'symlink':
                os.symlink(item['target'], destination / name)
        for name, item in items.items():
            item['size'] = os.lstat(destination / name).st_size
        manifest['members'] = sorted(items.values(), key=lambda item: item['path'])
        verify_source(destination, manifest)
        data = canonical_json(manifest)
        with exclusive(manifest_path) as out:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        return record, manifest, hashlib.sha256(data).hexdigest()


def fetch(f):
    # Key identity is established before signatures; all payload checks follow signatures.
    qkey = f.key('qemu')
    qsig = f.download(QEMU + '.sig', 'qemu.tar.xz.sig', 4 * 1024 * 1024)
    qemu = f.download(QEMU, 'qemu.tar.xz', 512 * 1024 * 1024)
    validation = validate_archive(qemu, f, qkey, qsig)
    f.result['qemu'] = {'path': str(qemu), 'sha256': validation['archive_sha256'],
                        'size': validation['archive_size'], 'archive_validation': validation}
    f.save()

    ukey = f.key('ubuntu')
    sums = f.download(UBUNTU + 'SHA256SUMS', 'ubuntu.SHA256SUMS', 16 * 1024 * 1024)
    usig = f.download(UBUNTU + 'SHA256SUMS.gpg', 'ubuntu.SHA256SUMS.gpg', 4 * 1024 * 1024)
    f.signature('ubuntu', ukey, sums, usig)
    entries = ubuntu_sums(sums)
    for name, expected in UBUNTU_HASHES.items():
        if entries.get(name) != expected:
            raise Failure('signed Ubuntu SHA256SUMS does not match fixed pin for ' + name)
        payload = f.download(UBUNTU + name, name, 4 * 1024 * 1024 * 1024 if name == IMAGE else 16 * 1024 * 1024)
        f.check_hash(payload, expected, 'ubuntu:' + name)

    dkey = f.key('docker')
    for pair in (1, 2):
        release = f.download(DOCKER + 'dists/noble/InRelease', 'docker.InRelease.pair' + str(pair), 16 * 1024 * 1024)
        packages = f.download(DOCKER + 'dists/noble/stable/binary-amd64/Packages', 'docker.Packages.pair' + str(pair), 128 * 1024 * 1024)
        f.signature('docker', dkey, release)
        expected_hash, expected_size = release_packages_entry(cleartext(release))
        actual_hash, actual_size = digest(packages)
        match = (actual_hash, actual_size) == (expected_hash, expected_size)
        f.result['docker_index_pairs'].append({'pair': pair, 'release': str(release), 'packages': str(packages),
                                               'expected_sha256': expected_hash, 'expected_size': expected_size,
                                               'sha256': actual_hash, 'size': actual_size, 'match': match})
        f.save()
        if match:
            break
        if pair == 2:
            raise Failure('Docker signed Packages hash/length mismatch after one whole-pair retry')
    package_entries(packages)
    for name, (_, filename, expected) in DEBS.items():
        payload = f.download(DOCKER + filename, name + '.deb', 512 * 1024 * 1024)
        f.check_hash(payload, expected, 'docker:' + name)


def do_fetch(env):
    result_path = RUN / 'result.json'
    # Claim the stage before creating any evidence. An interrupted or completed run is never reused.
    with exclusive(result_path) as result_file:
        result = {'run_token': TOKEN, 'fetch': {'status': 'IN_PROGRESS'}, 'downloads': [], 'commands': [],
                  'keys': {}, 'signatures': {}, 'hash_checks': {}, 'docker_index_pairs': [],
                  'product_gate': 'NOT_READY'}
        f = Fetcher(env, result, result_file)
        f.save()
        try:
            os.mkdir(f.assets, 0o700)
            os.mkdir(f.logs, 0o700)
            fetch(f)
            result['fetch'] = {'status': 'PASS'}
            f.save()
            print('fetch: PASS; carrier and product remain NOT_READY')
            return 0
        except Exception as exc:
            result['fetch'] = {'status': 'FAIL', 'error_type': type(exc).__name__, 'error': str(exc)}
            if isinstance(exc, ArchiveFailure):
                result['fetch']['reason'] = exc.reason
            f.save()
            print('PREREQUISITE_FAILED: fetch: %s: %s' % (type(exc).__name__, exc), file=sys.stderr)
            return 1


def verify():
    return verify_experiment()

import threading


def firmware_identity(container, manifest):
    """Verify the authenticated pc-bios runtime subtree and bind every entry."""
    prefix = ROOT + '/pc-bios'
    members = sorted((item for item in manifest['members']
                      if item['path'] == prefix or item['path'].startswith(prefix + '/')),
                     key=lambda item: item['path'])
    roots = [item for item in members if item['path'] == prefix]
    require(len(roots) == 1 and roots[0]['type'] == 'dir', 'firmware subtree missing')
    require(all(item['type'] in ('dir', 'file') for item in members),
            'unsupported firmware subtree link or type')
    firmware_dir = Path(container) / prefix
    check_path(firmware_dir)
    root_stat = os.stat(firmware_dir, follow_symlinks=False)
    require(stat.S_ISDIR(root_stat.st_mode) and
            stat.S_IMODE(root_stat.st_mode) == roots[0]['mode'] and
            root_stat.st_size == roots[0]['size'], 'firmware subtree root changed')
    descendants = [dict(item, path=item['path'][len(prefix) + 1:])
                   for item in members if item['path'] != prefix]
    verify_source(firmware_dir, {'members': descendants, 'omitted': []})
    defaults = {}
    for role, name in (('q35_bios', 'bios-256k.bin'), ('stdvga', 'vgabios-stdvga.bin')):
        path = prefix + '/' + name
        matches = [item for item in members if item['path'] == path and item['type'] == 'file']
        require(len(matches) == 1, 'firmware default missing: ' + name)
        defaults[role] = {'path': path, 'sha256': matches[0]['sha256']}
    return {'defaults': defaults, 'pc_bios_members': members,
            'pc_bios_manifest_sha256': hashlib.sha256(canonical_json(members)).hexdigest()}


REVIEW_HASHES = {
    'archive_core_sha256': '72af900d79f33a0794de2cdc439c1a52824d857ba41b10f33eeb0885cfe29a2f',
    'fetch_result_sha256': '5a183e7fcebfc666c3d400a910b2a670ef0a5be57bd55267bae333394db3c813',
    'host_sha256': 'f9d87b347ec28da80ed4ecb6ec1d7436b95efa2475a3b04c59f9c8baf083055f',
    'smoke_receipt_sha256': '3fe5210813f582de25e66944bb898fbe1e36b4fe5cf8ddf1b594775eeac2178f',
    'smoke_sha256': 'd01ebfbc2c6826baa701f72756045234f295a5fb53ae544d7d05886f26c35e93',
    'source_manifest_sha256': 'ac3d16592eddc04af338252f199f5adfd4a6385d897a8298fcb504e5bbfb4ece',
}
SEED_PYTHON = ('guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py')
BUILD_INPUTS = SEED_PYTHON + ('probe/main.go', 'preparer/main.go', 'probe-seccomp.json',
                              'go.mod', 'go.sum', 'prepare_import.py', 'verify_handoff.py')
SEED_NAMES = set(SEED_PYTHON) | {'probe', 'preparer', 'probe-seccomp.json', 'docker-ce.deb',
              'docker-ce-cli.deb', 'containerd.io.deb', 'case.json', 'meta-data', 'user-data',
              'network-config', 'SHA256SUMS'}
EXPERIMENT_CASES = ('C0', 'V1', 'V2', 'V3', 'I0', 'I1', 'I2', 'I3')
EXPERIMENT_BOUNDARY = {'mount_namespace_identity': 'UNPROVEN', 'all_overlay_layers': 'UNPROVEN',
                       'product_gate': 'NOT_READY'}
REQUIRED_EVIDENCE = {
    'C0': ('coordinator_identity', 'guard_held', 'ext4', 'btrfs', 'helper_isolation', 'probe_complete',
           'preparation_bound'),
    'V1': ('named', 'anonymous', 'direct_eacces', 'distinct_filesystems', 'consumer_unchanged',
           'native_volume_mounts', 'exact_identity', 'preparation_bound'),
    'V2': ('recursive_readonly', 'private_propagation', 'nested_identity', 'root_write_erofs',
           'nested_write_erofs', 'sentinels_unchanged', 'preparation_bound'),
    'V3': ('empty_volume', 'nocopy', 'missing_rejected_before_create', 'volume_set_unchanged', 'preparation_bound'),
    'I0': ('daemon_credentials', 'daemon_pidfd', 'daemon_pidns', 'dockerd_exe', 'exact_metadata',
           'helper_complete', 'preparation_bound'),
    'I1': ('object_match', 'target_identity', 'target_pidfd', 'mountinfo_stable', 'consumer_unchanged',
           'helper_complete', 'preparation_bound'),
    'I2': ('old_held_alias', 'mismatch_reported', 'target_identity', 'consumer_unchanged', 'helper_complete',
           'preparation_bound'),
    'I3': ('stopped_before_create', 'stopped_after_helper', 'pidfd_exited', 'stale_output_rejected',
           'restart_rejected', 'preparation_bound'),
}
OBSERVER_NEGATIVES = ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce')
MALFORMED_NEGATIVES = ('wrong_input_digest', 'extra_fd')
KILL_NEGATIVES = ('before_attach', 'after_first_attach')
READER_FAULT_SCENARIOS = ('before_capture_preparer_death', 'after_capture_preparer_death',
                          'supervisor_fork_death', 'supervisor_launch_death', 'pidfd_open_failure',
                          'reader_timeout', 'reader_nonzero', 'restore_failure')
READER_FAULT_CASES = tuple('reader-fault-' + name for name in READER_FAULT_SCENARIOS)
VM_CASES = ('abort', 'matrix') + READER_FAULT_CASES
PREPARATION_VARIANTS = OBSERVER_NEGATIVES + MALFORMED_NEGATIVES + KILL_NEGATIVES
REQUIRED_PREPARATION_EVIDENCE = {
    **{name: ('waiting_only', 'observer_retired', 'no_ready_sent', 'source_unchanged')
       for name in OBSERVER_NEGATIVES},
    **{name: ('waiting_only', 'observer_retired', 'no_ready_sent', 'source_unchanged',
              'request_rejected', 'preparer_exited', 'fds_retired') for name in MALFORMED_NEGATIVES},
    **{name: ('fault_position', 'preparer_killed', 'no_ready_sent', 'observer_retired',
              'source_graph_unchanged', 'sentinels_unchanged', 'consumer_unchanged') for name in KILL_NEGATIVES},
}


def require(condition, message):
    if not condition:
        raise Failure(message)


def load_json(path):
    check_path(path, False)
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key: ' + key)
            result[key] = value
        return result
    with path.open('r', encoding='utf-8') as file:
        return json.load(file, object_pairs_hook=pairs)


def receipt_save(file, value):
    file.seek(0)
    file.truncate()
    file.write(json.dumps(value, indent=2, sort_keys=True).encode() + b'\n')
    file.flush()
    os.fsync(file.fileno())


def file_hash(path):
    check_path(path, False)
    return digest(path)[0]


def checked_asset(path, sha, size=None):
    path = Path(path)
    require(path.parent == RUN / 'assets', 'unexpected asset path')
    check_path(path, False)
    actual, length = digest(path)
    require(actual == sha and (size is None or length == size), 'asset changed: ' + path.name)
    return path


def archive_gate():
    result, manifest, _ = prepare_import.gate(sys.modules[__name__])
    return result, manifest


def command(argv, cwd, env, logdir, label, timeout):
    with exclusive(logdir / (label + '.stdout')) as out, exclusive(logdir / (label + '.stderr')) as err:
        proc = subprocess.Popen(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=out, stderr=err, start_new_session=True)
        try:
            proc.wait(timeout=timeout)
        except BaseException:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                proc.wait(timeout=10)
            raise
    require(proc.returncode == 0, '%s exited %s; see logs' % (label, proc.returncode))


def write_file(path, data, mode=0o600):
    with exclusive(path) as file:
        file.write(data)
        file.flush()
        os.fsync(file.fileno())
        os.fchmod(file.fileno(), mode)


def validate_probe_inputs():
    for package in ('probe', 'preparer'):
        check_path(RUN / package)
        require({entry.name for entry in os.scandir(RUN / package)} == {'main.go'},
                'unexpected Go source set: ' + package)
    for relative in BUILD_INPUTS:
        check_path(RUN / relative, False)
        require((RUN / relative).stat().st_size > 0, 'empty input: ' + relative)
    for name in SEED_PYTHON + ('prepare_import.py', 'verify_handoff.py'):
        ast.parse((RUN / name).read_text(encoding='utf-8'), filename=name)
    mod = (RUN / 'go.mod').read_text(encoding='utf-8').split()
    require(mod == ['module', 'g2isolated', 'go', '1.26.2', 'require', 'golang.org/x/sys', 'v0.47.0'],
            'unexpected Go module')
    sums = (RUN / 'go.sum').read_text(encoding='utf-8').splitlines()
    require(set(sums) == {
        'golang.org/x/sys v0.47.0 h1:o7XGOvZQCADBQQ4Y7VNq2dRWQR7JmOUW8Kxx4ZsNgWs=',
        'golang.org/x/sys v0.47.0/go.mod h1:4GL1E5IUh+htKOUEOaiffhrAeqysfVGipDYzABqnCmw='
    } and len(sums) == 2, 'unexpected Go checksums')
    allowed = ('read write close openat newfstatat fstat statx fstatfs readlink readlinkat '
               'getdents64 lseek fcntl mmap mprotect munmap madvise brk rt_sigaction '
               'rt_sigprocmask rt_sigreturn sigaltstack futex sched_yield sched_getaffinity '
               'nanosleep clock_gettime getpid getppid gettid getuid geteuid getgid getegid '
               'getrandom tgkill arch_prctl set_tid_address set_robust_list rseq prlimit64 '
               'uname exit exit_group execve epoll_create1 epoll_ctl epoll_pwait eventfd2 '
               'pipe2 dup dup3').split()
    profile = load_json(RUN / 'probe-seccomp.json')
    expected = {'defaultAction': 'SCMP_ACT_ERRNO', 'defaultErrnoRet': 1,
                'architectures': ['SCMP_ARCH_X86_64'],
                'syscalls': [
                    {'names': allowed, 'action': 'SCMP_ACT_ALLOW'},
                    {'names': ['clone'], 'action': 'SCMP_ACT_ALLOW',
                     'args': [{'index': 0, 'value': 0x7e020000, 'valueTwo': 0,
                               'op': 'SCMP_CMP_MASKED_EQ'}]},
                    {'names': ['clone3'], 'action': 'SCMP_ACT_ERRNO', 'errnoRet': 38},
                    {'names': ['ioctl'], 'action': 'SCMP_ACT_ALLOW',
                     'args': [{'index': 1, 'value': 0x8400941f, 'op': 'SCMP_CMP_EQ'}]},
                    {'names': ['ioctl'], 'action': 'SCMP_ACT_ALLOW',
                     'args': [{'index': 1, 'value': 0xd0009412, 'op': 'SCMP_CMP_EQ'}]}]}
    require(profile == expected, 'unexpected seccomp profile')


def seed_hashes(seed=None):
    seed = RUN / 'seed' if seed is None else Path(seed)
    check_path(seed)
    require({entry.name for entry in os.scandir(seed)} == SEED_NAMES, 'unexpected seed entries')
    hashes = {}
    for name in sorted(SEED_NAMES):
        path = seed / name
        check_path(path, False)
        hashes[name] = file_hash(path)
    expected = ''.join(hashes[name] + '  ' + name + '\n' for name in sorted(SEED_NAMES - {'SHA256SUMS'}))
    require((seed / 'SHA256SUMS').read_text(encoding='ascii') == expected, 'seed manifest mismatch')
    return hashes


def freeze_seed(case, seed=None):
    seed = RUN / 'seed' if seed is None else Path(seed)
    for name, data in (
        ('meta-data', ('instance-id: g2-%s-%s\nlocal-hostname: g2-lab\n' % (TOKEN, case)).encode()),
        ('case.json', canonical_json({'case': case, 'run_token': TOKEN})),
    ):
        path = seed / name
        if path.exists():
            path.unlink()
        write_file(path, data)
    sums = ''.join(file_hash(seed / name) + '  ' + name + '\n'
                   for name in sorted(SEED_NAMES - {'SHA256SUMS'}))
    path = seed / 'SHA256SUMS'
    if path.exists():
        path.unlink()
    write_file(path, sums.encode('ascii'))
    return seed_hashes(seed)


def case_seed_hashes(case, build_seed):
    require(case in VM_CASES, 'invalid seed case')
    seed = RUN / 'runs' / case / 'seed'
    hashes = seed_hashes(seed)
    require(stat.S_IMODE(os.stat(seed, follow_symlinks=False).st_mode) == 0o500,
            case + ' seed directory is not frozen')
    for name in SEED_NAMES:
        require(stat.S_IMODE(os.stat(seed / name, follow_symlinks=False).st_mode) == 0o400,
                case + ' seed file is not frozen: ' + name)
    for name in SEED_NAMES - {'case.json', 'meta-data', 'SHA256SUMS'}:
        require(hashes[name] == build_seed[name], case + ' seed input changed: ' + name)
    require((seed / 'case.json').read_bytes() == canonical_json({'case': case, 'run_token': TOKEN}),
            case + ' seed case binding changed')
    require((seed / 'meta-data').read_bytes() ==
            ('instance-id: g2-%s-%s\nlocal-hostname: g2-lab\n' % (TOKEN, case)).encode(),
            case + ' seed instance binding changed')
    if case == 'abort':
        require(hashes == build_seed, 'abort seed does not match the build')
    return hashes


def snapshot_seed(case, build_seed):
    require(case in VM_CASES, 'invalid seed case')
    require(seed_hashes() == build_seed, 'built seed changed')
    seed = RUN / 'runs' / case / 'seed'
    os.mkdir(seed, 0o700)
    for name in sorted(SEED_NAMES):
        with (RUN / 'seed' / name).open('rb') as source, exclusive(seed / name) as out:
            shutil.copyfileobj(source, out, 1024 * 1024)
            out.flush()
            os.fsync(out.fileno())
    if case != 'abort':
        freeze_seed(case, seed)
    for name in SEED_NAMES:
        fd = os.open(seed / name, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            require(stat.S_ISREG(os.fstat(fd).st_mode), 'seed file is not regular: ' + name)
            os.fchmod(fd, 0o400)
            os.fsync(fd)
        finally:
            os.close(fd)
    fd = os.open(seed, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fchmod(fd, 0o500)
        os.fsync(fd)
    finally:
        os.close(fd)
    return case_seed_hashes(case, build_seed)


def do_build(env):
    with exclusive(RUN / 'build.json') as receipt:
        state = {'run_token': TOKEN, 'experiment_kind': 'input-handoff-v1', 'status': 'IN_PROGRESS',
                 'stage': 'gate', **EXPERIMENT_BOUNDARY}
        receipt_save(receipt, state)
        try:
            result, manifest, prepared = prepare_import.gate(sys.modules[__name__])
            validate_probe_inputs()
            firmware = prepared['firmware']
            os.mkdir(RUN / 'build/logs', 0o700)
            os.mkdir(RUN / 'seed', 0o700)
            logs = RUN / 'build/logs'
            state['stage'] = 'build_helpers'
            receipt_save(receipt, state)
            goenv = dict(env, CGO_ENABLED='0', GOOS='linux', GOARCH='amd64')
            command([str(GO), 'mod', 'verify'], RUN, goenv, logs, 'module-verify', 300)
            require((logs / 'module-verify.stdout').read_text().strip() == 'all modules verified',
                    'offline module verification failed')
            command([str(GO), 'build', '-mod=readonly', '-trimpath', '-o', str(RUN / 'seed/probe'), './probe'],
                    RUN, goenv, logs, 'probe-build', 3600)
            command([str(GO), 'build', '-mod=readonly', '-trimpath', '-o', str(RUN / 'seed/preparer'), './preparer'],
                    RUN, goenv, logs, 'preparer-build', 3600)
            for name in SEED_PYTHON + ('probe-seccomp.json',):
                write_file(RUN / 'seed' / name, (RUN / name).read_bytes())
            for name in DEBS:
                src = Path(result['hash_checks']['docker:' + name]['path'])
                with src.open('rb') as inp, exclusive(RUN / 'seed' / (name + '.deb')) as out:
                    shutil.copyfileobj(inp, out, 1024 * 1024)
                    out.flush()
                    os.fsync(out.fileno())
            write_file(RUN / 'seed/network-config', b'version: 2\nethernets: {}\n')
            cloud = b"""#cloud-config
package_update: false
package_upgrade: false
runcmd:
  - [mkdir, -p, /mnt/g2-seed, /run/g2]
  - [mount, -t, vfat, -o, 'ro,nosuid,nodev,noexec', 'LABEL=CIDATA', /mnt/g2-seed]
  - [sh, -c, 'cd /mnt/g2-seed && sha256sum -c SHA256SUMS && cp guest.py handoff.py guest_matrix.py preparation.py prepare_launcher.py /run/g2/ && chmod 644 /run/g2/guest.py /run/g2/handoff.py /run/g2/guest_matrix.py /run/g2/preparation.py /run/g2/prepare_launcher.py && /usr/bin/python3 -B /run/g2/guest.py']
power_state: {mode: poweroff, delay: now, timeout: 60}
"""
            write_file(RUN / 'seed/user-data', cloud)
            frozen = freeze_seed('abort')
            for name, (_, _, pin) in DEBS.items():
                require(frozen[name + '.deb'] == pin, 'seed deb changed: ' + name)
            state.update(status='PASS', stage='complete', experiment_kind='input-handoff-v1',
                         source_manifest_sha256=prepare_import.MANIFEST_SHA,
                         prepare_sha256=file_hash(RUN / 'prepare.json'),
                         host_sha256=file_hash(RUN / 'host.py'), go_version=prepared['go_version'],
                         firmware=firmware, seed=frozen,
                         inputs={name: file_hash(RUN / name) for name in BUILD_INPUTS},
                         observer_binaries={name: frozen[name] for name in ('probe', 'preparer')},
                         binaries={name: file_hash(RUN / 'build/qemu' / name)
                                   for name in ('qemu-system-x86_64', 'qemu-img')})
            receipt_save(receipt, state)
            return 0
        except BaseException as exc:
            state.update(status='FAIL', error_type=type(exc).__name__, error=str(exc))
            receipt_save(receipt, state)
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            print('PREREQUISITE_FAILED: build: %s' % exc, file=sys.stderr)
            return 1


def build_gate():
    result, manifest = archive_gate()
    build = load_json(RUN / 'build.json')
    require(build.get('run_token') == TOKEN and build.get('status') == 'PASS'
            and build.get('experiment_kind') == 'input-handoff-v1'
            and build.get('source_manifest_sha256') == prepare_import.MANIFEST_SHA
            and build.get('prepare_sha256') == file_hash(RUN / 'prepare.json')
            and build.get('host_sha256') == file_hash(RUN / 'host.py'),
            'build is incomplete or does not bind its inputs')
    validate_probe_inputs()
    require(all(build.get(key) == value for key, value in EXPERIMENT_BOUNDARY.items()),
            'build receipt capability boundary missing')
    require(type(build.get('inputs')) is dict and set(build['inputs']) == set(BUILD_INPUTS),
            'build input set is incomplete or unexpected')
    for name, sha in build['inputs'].items():
        require(file_hash(RUN / name) == sha, 'build input changed: ' + name)
    require(type(build.get('binaries')) is dict and set(build['binaries']) == {'qemu-system-x86_64', 'qemu-img'},
            'QEMU binary set is incomplete or unexpected')
    require(type(build.get('observer_binaries')) is dict and set(build['observer_binaries']) == {'probe', 'preparer'},
            'observer binary set is incomplete or unexpected')
    for name in ('qemu-system-x86_64', 'qemu-img'):
        require(file_hash(RUN / 'build/qemu' / name) == build['binaries'][name],
                'QEMU binary changed: ' + name)
    prepared = load_json(RUN / 'prepare.json')
    require(build.get('firmware') == prepared.get('firmware') and
            build.get('go_version') == prepared.get('go_version') == 'go version go1.26.2 linux/amd64',
            'firmware or Go toolchain identity changed or missing')
    require(seed_hashes() == build.get('seed'), 'build seed changed')
    for name in SEED_PYTHON + ('probe-seccomp.json',):
        require(file_hash(RUN / 'seed' / name) == build['inputs'][name], 'seed input changed: ' + name)
    for name, sha in build['observer_binaries'].items():
        require(file_hash(RUN / 'seed' / name) == sha == build['seed'][name], 'built binary changed: ' + name)
    return result, build


def _helper_observations(value):
    if type(value) is dict:
        if 'preparation' in value and 'records' in value and 'preparation_version' in value:
            yield value
        else:
            for child in value.values():
                yield from _helper_observations(child)
    elif type(value) is list:
        for child in value:
            yield from _helper_observations(child)


def validate_guest_install(isolation):
    import handoff
    install = isolation['preparer_install']
    preparation.fields(install, ('seed_sha256', 'copy_sha256', 'seed', 'copy', 'size', 'path',
                                 'mountinfo', 'mount_flags', 'statvfs_flags', 'mount_id'),
                       'preparer installation')
    seed_path = RUN / 'seed/preparer'
    require(install['seed_sha256'] == install['copy_sha256'] == file_hash(seed_path),
            'preparer installation does not bind built binary')
    preparation._object_identity(install['seed'], 'seed executable')
    preparation._object_identity(install['copy'], 'installed executable')
    require(stat.S_ISREG(install['seed']['mode']) and not install['seed']['mode'] & 0o022 and
            stat.S_ISREG(install['copy']['mode']) and stat.S_IMODE(install['copy']['mode']) == 0o500 and
            (install['seed']['dev'], install['seed']['ino']) != (install['copy']['dev'], install['copy']['ino']) and
            type(install['size']) is int and install['size'] > 0 and install['size'] == seed_path.stat().st_size and
            type(install['statvfs_flags']) is int and install['statvfs_flags'] >= 0 and
            not (install['statvfs_flags'] & os.ST_NOEXEC), 'installed executable metadata invalid')
    require(type(install['path']) is str and install['path'] == PREPARER_PATH and
            type(install['mount_id']) is int and install['mount_id'] > 0 and
            type(install['mountinfo']) is str, 'installed executable path or mount ID invalid')
    graph = handoff.custody_graph(install['mountinfo'])
    require(len(graph) == 1 and type(install['mount_flags']) is list, 'preparer mount evidence missing')
    mount_id, mount = next(iter(graph.items()))
    point = mount['point']
    require(mount_id == install['mount_id'] and point.startswith('/') and posixpath.normpath(point) == point and
            (point == '/' or PREPARER_PATH == point or PREPARER_PATH.startswith(point + '/')),
            'preparer mount ID or path coverage invalid')
    require(install['mount_flags'] == sorted(set(mount['options'] + mount['super_options'])) and
            'noexec' not in install['mount_flags'], 'installed executable mount is noexec or changed')
    return install


RESULT_PORT = 'org.houfeng.g2.result'
PROTOCOL_LIMIT = 16 * 1024 * 1024


def validate_transport(transport):
    preparation.fields(transport, ('kind', 'port_name', 'sysfs_name', 'device'), 'result transport')
    require(transport['kind'] == 'virtio-serial-file-v1' and transport['port_name'] == RESULT_PORT and
            type(transport['sysfs_name']) is str and
            re.fullmatch(r'vport[0-9]+p[1-9][0-9]*', transport['sysfs_name']) is not None,
            'result transport identity mismatch')
    device = transport['device']
    preparation.fields(device, ('dev', 'ino', 'major', 'minor', 'uid', 'gid', 'mode'), 'result device')
    preparation.integer(device['dev'], 'result device dev')
    preparation.integer(device['ino'], 'result device inode', 1)
    preparation.integer(device['major'], 'result device major', 1, (1 << 32) - 1)
    for name in ('minor', 'uid', 'gid'):
        preparation.integer(device[name], 'result device ' + name, 0, (1 << 32) - 1)
    require(device['uid'] == 0 and type(device['mode']) is int and device['mode'] == 0o600,
            'result device owner or mode mismatch')
    return transport


def validate_abort_install_receipt(receipt, installation):
    recorded = receipt.get('preparer_install')
    require(type(recorded) is dict, 'abort installation receipt missing')
    canonical = preparation.canonical_json(installation)
    require(preparation.canonical_json(recorded) == canonical, 'abort installation receipt differs from serial')
    require(type(receipt.get('preparer_install_sha256')) is str and
            receipt['preparer_install_sha256'] == hashlib.sha256(canonical).hexdigest(),
            'abort installation receipt digest mismatch')


def validate_case_preparation(result, case_name, profile, observations, used):
    expected_count = {'C0': 2, 'V1': 2, 'V2': 2, 'V3': 1, 'I0': 1, 'I1': 1, 'I2': 1, 'I3': 1}
    helpers = list(_helper_observations(observations))
    require(len(helpers) == expected_count[case_name], 'missing or extra actual helper observations')
    isolation = result['isolation']
    try:
        install = validate_guest_install(isolation)
        coordinator = isolation['coordinator']
        fixture_ns = isolation['fixture_namespaces']
        image = isolation['image_fixture']['image_id']
        require(install['seed_sha256'] == install['copy_sha256'], 'preparer digest changed')
        require(coordinator['guard_registered_before_ready'] is True and coordinator['ready'] is True,
                'coordinator guard registration evidence missing')
        summaries, signatures, volume_names = [], [], []
        for helper in helpers:
            summary = preparation.validate_observation(helper)
            _bind_daemon(used, helper['daemon_lifecycle']['value'])
            prep = helper['preparation']
            inp = prep['request']['input']
            require(inp['run_token'] == TOKEN and inp['case'] == case_name and inp['profile'] == profile and
                    inp['imageID'] == image and inp['preparer_sha256'] == install['copy_sha256'] and
                    prep['launch']['exe'] == install['copy'] and
                    prep['launch']['session_nonce'] == coordinator['session_nonce'] and
                    prep['coordinator_namespaces'] == fixture_ns, 'helper guest identity or build binding mismatch')
            signatures.append((inp['phase'], inp['deny'], inp['nested'], inp['exe'],
                               tuple(root['kind'] for root in inp['roots'])))
            volume_names.extend(root['name'] for root in inp['roots'] if root['kind'] == 'native_volume')
            require(summary['id'] not in used['ids'] and summary['ordinal'] not in used['ordinals'] and
                    summary['nonce'] not in used['nonces'], 'replayed helper preparation')
            used['ids'].add(summary['id'])
            used['ordinals'].add(summary['ordinal'])
            used['nonces'].add(summary['nonce'])
            summaries.append(summary)
        expected_signatures = {
            'C0': [('before', False, False, False, ('declared_bind', 'declared_bind')),
                   ('after', True, False, False, ('declared_bind',))],
            'V1': [('before', False, False, False, ('native_volume', 'declared_bind'))] * 2,
            'V2': [('before', False, True, False, ('native_volume',)),
                   ('after', True, True, False, ('native_volume',))],
            'V3': [('before', False, False, False, ('native_volume',))],
            'I0': [('before', False, False, True, ('declared_bind',))],
            'I1': [('before', False, False, False, ('declared_bind', 'running_root'))],
            'I2': [('before', False, False, False, ('declared_bind', 'running_root'))],
            'I3': [('before', False, False, False, ('declared_bind', 'running_root'))],
        }
        require(sorted(signatures) == sorted(expected_signatures[case_name]) and
                (case_name != 'V1' or len(set(volume_names)) == 2), 'case helper roles mismatch')
        require(type(observations.get('preparation_bindings')) is list and
                sorted(preparation.canonical_json(value) for value in observations['preparation_bindings']) ==
                sorted(preparation.canonical_json(value) for value in summaries), 'guest preparation bindings differ')
    except (KeyError, TypeError, ValueError, OSError) as exc:
        raise Failure('invalid helper preparation: ' + str(exc)) from exc


def _bind_daemon(used, value):
    current = (value['pid'], value['starttime'])
    if 'daemon' in used:
        require(used['daemon'] == current, 'original daemon identity changed between helpers')
    else:
        used['daemon'] = current


def _coordinator_identity(isolation):
    coordinator = isolation['coordinator']
    preparation.fields(coordinator, ('ready', 'status', 'groups', 'guard', 'session_nonce',
                                     'guard_registered_before_ready'), 'coordinator handshake')
    require(coordinator['ready'] is True and coordinator['guard_registered_before_ready'] is True,
            'coordinator guard handshake incomplete')
    preparation.hex_string(coordinator['session_nonce'], 'coordinator session')
    status = coordinator['status']
    require(type(status) is str and 0 < len(status) <= 1024 * 1024, 'coordinator status missing')
    def field(name):
        matches = re.findall(r'^' + name + r':[^\S\n]*([^\n]*)$', status, re.M)
        require(len(matches) == 1, 'missing or duplicate coordinator ' + name)
        return matches[0].strip()
    pid = field('Pid')
    require(pid.isdecimal() and int(pid) > 0 and field('Uid').split() == ['10002'] * 4 and
            field('Gid').split() == ['10002'] * 4, 'coordinator process credentials differ')
    groups = coordinator['groups']
    require(type(groups) is list and len(groups) == 1 and type(groups[0]) is int and groups[0] > 0 and
            field('Groups').split() == [str(groups[0])], 'coordinator supplementary groups differ')
    for name in ('CapInh', 'CapPrm', 'CapEff', 'CapAmb'):
        require(re.fullmatch(r'0{16}', field(name)) is not None, 'coordinator capability is not zero: ' + name)
    guard = coordinator['guard']
    preparation.fields(guard, ('dev', 'ino', 'uid', 'mode'), 'coordinator guard')
    require(type(guard['dev']) is int and guard['dev'] >= 0 and type(guard['ino']) is int and guard['ino'] > 0 and
            guard['uid'] == 10002 and type(guard['uid']) is int and guard['mode'] == 0o600 and
            type(guard['mode']) is int, 'coordinator guard identity invalid')
    return guard


def _object(value, name, directory=True):
    preparation.fields(value, ('dev', 'ino', 'mode', 'size'), name)
    require(all(type(value[key]) is int and value[key] >= 0 for key in value) and value['ino'] > 0 and
            (not directory or stat.S_ISDIR(value['mode'])), name + ' identity invalid')
    return value


def _same_object(left, right):
    _object(left, 'observed object')
    _object(right, 'fixture object')
    return all(left[key] == right[key] for key in ('dev', 'ino', 'mode'))


def _record(helper, name):
    record = helper['indexed'][name]
    require(type(record) is dict and type(record.get('errno')) is int, 'observer record missing: ' + name)
    return record


def _step(observations, name, result=True):
    step = observations['substeps'][name]
    require(type(step) is dict and step.get('status') == 'PASS', 'case substep failed: ' + name)
    return step['result'] if result else step


def _consumer(value):
    preparation.fields(value, ('id', 'pid', 'running', 'status', 'started_at'), 'fixture consumer')
    preparation.hex_string(value['id'], 'consumer ID')
    require(type(value['pid']) is int and value['pid'] > 0 and value['running'] is True and
            value['status'] == 'running' and type(value['started_at']) is str and value['started_at'],
            'fixture consumer is not running')


def _held_consumer(held, consumer, process=None):
    require(type(held) is dict and set(held) == {'id', 'pid', 'running', 'status', 'started_at',
                                                'process_stat', 'pidfd_info', 'pidfd_alive'},
            'held consumer record missing')
    _consumer({key: held[key] for key in ('id', 'pid', 'running', 'status', 'started_at')})
    require(all(held[key] == consumer[key] for key in consumer) and held['pidfd_alive'] is True and
            type(held['pidfd_info']) is str and
            re.findall(r'^Pid:\s*([0-9]+)\s*$', held['pidfd_info'], re.M) == [str(consumer['pid'])] and
            re.search(r'^NSpid:\s*[0-9]+(?:\s+[0-9]+)*\s*$', held['pidfd_info'], re.M),
            'held pidfd does not identify the original consumer')
    stat_value = held['process_stat']
    preparation.fields(stat_value, ('pid', 'starttime'), 'held process stat')
    require(stat_value['pid'] == consumer['pid'] and type(stat_value['starttime']) is int and
            stat_value['starttime'] > 0 and (process is None or stat_value == process),
            'held consumer starttime differs')


def _volume(value, anonymous):
    require(type(value) is dict and value.get('anonymous') is anonymous and
            type(value.get('name')) is str and type(value.get('path')) is str,
            'volume fixture identity missing')
    name, path = value['name'], value['path']
    require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,255}', name) is not None and
            path == '/lab/ext/docker/volumes/' + name + '/_data', 'volume fixture path differs')
    registration = value['registration']
    preparation.fields(registration, ('inspect', 'volume_set'), 'volume registration')
    inspected, volumes = registration['inspect'], registration['volume_set']
    require(type(inspected) is dict and inspected.get('Name') == name and inspected.get('Mountpoint') == path and
            inspected.get('Driver') == inspected.get('Scope') == 'local' and not inspected.get('Options') and
            type(inspected.get('CreatedAt')) is str and inspected['CreatedAt'] and
            type(volumes) is list and all(type(v) is str for v in volumes) and
            volumes == sorted(set(volumes)) and name in volumes, 'native volume registration differs')
    labels = inspected.get('Labels') or {}
    require(type(labels) is dict and
            ((anonymous and re.fullmatch(r'[0-9a-f]{64}', name) is not None and
              labels.get('com.docker.volume.anonymous') == '') or
             (not anonymous and labels.get('owner') == 'g2-isolated-capability' and labels.get('run') == TOKEN)),
            'native volume ownership differs')
    _object(value['truth'], 'volume fixture')
    require(stat.S_IMODE(value['truth']['mode']) == 0o700 and value['owner'] == {'uid': 0, 'gid': 0},
            'native volume owner or mode differs')
    return name, path


def _fixture_graph(value):
    import handoff
    preparation.fields(value, ('lines', 'truncated'), 'fixture mount snapshot')
    require(value['truncated'] is False and type(value['lines']) is list and value['lines'] and
            all(type(line) is str and line for line in value['lines']), 'fixture mount snapshot incomplete')
    return handoff.custody_graph('\n'.join(value['lines']) + '\n')


def _mount_at(graph, point):
    matches = [entry for entry in graph.values() if entry['point'] == point]
    require(len(matches) == 1, 'fixture mountpoint missing or ambiguous: ' + point)
    return matches[0]


def _target_graph(text):
    import handoff
    require(type(text) is str and text.endswith('\n') and len(text.encode('utf-8')) <= 1024 * 1024,
            'complete target mount graph missing')
    return _mount_at(handoff.custody_graph(text), '/data')


def _guard_matches(guard, observed):
    require(type(observed) is dict and observed.get('held') is True and
            observed.get('dev') == guard['dev'] and observed.get('ino') == guard['ino'],
            'held guard does not match coordinator handshake')


def _fixture_guard(guard, observed):
    _object(observed, 'fixture guard', directory=False)
    require(stat.S_ISREG(observed['mode']) and stat.S_IMODE(observed['mode']) == guard['mode'] and
            observed['dev'] == guard['dev'] and observed['ino'] == guard['ino'],
            'fixture guard differs from handshake')


def _root_matches(helper, ordinal, truth, filesystem=None):
    entry = _record(helper, '%s:fstat' % ordinal)
    require(entry['errno'] == 0 and _same_object(entry['value'], truth), 'root object differs from fixture truth')
    if filesystem is not None:
        require(_record(helper, '%s:statfs' % ordinal)['value']['type'] == filesystem,
                'root filesystem differs from fixture')
        if filesystem == 0x9123683e:
            require(all(_record(helper, '%s:%s' % (ordinal, op))['errno'] == 0
                        for op in ('btrfs_fs_info', 'btrfs_ino_lookup')), 'btrfs identity evidence missing')


def _begin_matches(begin, consumer, held, source):
    preparation.fields(begin, ('id', 'pid', 'started_at', 'source', 'destination', 'stat', 'pidfd_alive'),
                       'target begin')
    require(begin['id'] == consumer['id'] and begin['pid'] == consumer['pid'] and
            begin['started_at'] == consumer['started_at'] and begin['source'] == source and
            begin['destination'] == '/data' and begin['pidfd_alive'] is True,
            'target begin differs from fixture consumer')
    _held_consumer(held, consumer, begin['stat'])


def _target_observation(observation, begin):
    require(type(observation) is dict and observation.get('status') == 'OBSERVED', 'target output missing')
    helper = observation['helper']
    request = helper['preparation']['request']
    target, process = request['target'], helper['preparation']['raw_evidence']['target']
    view = helper['preparation']['raw_evidence']['observer_view']
    require(target == {'id': begin['id'], 'pid': begin['pid'], 'starttime': begin['stat']['starttime'],
                       'destination': '/data'} and request['input']['metadata_pid'] == begin['pid'] and
            request['input']['roots'][0]['source'] == begin['source'] and
            process['pid'] == begin['pid'] and process['starttime'] == begin['stat']['starttime'] and
            process['before_mountinfo'] == process['after_mountinfo'] and
            _record(helper, 'target_mountinfo')['value']['text'] == view['before']['metadata_mountinfo'] ==
            view['after']['metadata_mountinfo'],
            'target preparation metadata differs from held target')
    import handoff
    require(handoff.proc_stat(_record(helper, 'target_stat')['value']['text']) == begin['stat'] and
            _record(helper, 'target_pidns')['value'] == process['pidns'], 'target observer metadata differs')
    return helper


def _case_c0(obs, profile, guard):
    ext, btr = '/lab/ext/c0', '/lab/btr/p'
    truth = obs['truth_before']
    require(type(truth) is dict and set(truth) == {ext, btr} and truth == obs['truth_after'] and
            obs['sentinel_before'] == obs['sentinel_after'] ==
            hashlib.sha256(b'G2-READONLY-SENTINEL\n').hexdigest(), 'C0 fixture truth changed')
    _guard_matches(guard, obs['guard'])
    helper, deny = obs['helper'], obs['deny']
    roots = helper['preparation']['request']['input']['roots']
    require([root['source'] for root in roots] == [ext, btr] and
            deny['preparation']['request']['input']['roots'][0]['source'] == ext,
            'C0 roots differ from fixture')
    _root_matches(helper, '000', truth[ext], 0xef53)
    _root_matches(helper, '001', truth[btr], 0x9123683e)
    require(_record(deny, '000:readonly_open')['errno'] == errno.EROFS, 'C0 readonly rejection missing')
    if profile == 'B':
        require(all(_record(deny, op)['errno'] == errno.EPERM for op in ('inet_socket', 'unshare', 'mount')),
                'C0 seccomp rejection missing')


def _case_v1(result, obs):
    volumes = obs['volumes']
    require(type(volumes) is list and len(volumes) == 2, 'V1 named and anonymous volumes missing')
    named = _volume(volumes[0], False)
    anonymous = _volume(volumes[1], True)
    require(named[0] != anonymous[0], 'V1 volume identities reused')
    project = '/lab/btr/p'
    truth = obs['truth_before']
    require(type(truth) is dict and set(truth) == {named[1], anonymous[1], project} and
            truth == obs['truth_after'], 'V1 fixture truth changed')
    before, after = obs['consumers_before'], obs['consumers_after']
    require(type(before) is list and len(before) == 2 and after == before and
            before[0]['id'] != before[1]['id'], 'V1 consumer before/after identity changed')
    for consumer in before:
        _consumer(consumer)
    created = volumes[1]['fixture_creation']
    preparation.fields(created, ('consumer_id', 'before', 'after'), 'anonymous fixture creation')
    require(created['consumer_id'] == before[1]['id'] and type(created['before']) is list and
            created['before'] == sorted(set(created['before'])) and
            created['after'] == sorted(set(created['before']) | {anonymous[0]}) and
            anonymous[0] not in created['before'] and
            created['after'] == volumes[1]['registration']['volume_set'],
            'anonymous volume creation set differs')
    for index, (label, volume) in enumerate(zip(('named', 'anonymous'), volumes)):
        name, path = (named, anonymous)[index]
        require(_step(obs, 'direct_' + label) == {'errno': errno.EACCES}, 'direct volume access was not EACCES')
        helper = _step(obs, label)
        roots = helper['preparation']['request']['input']['roots']
        require(roots[0]['name'] == name and roots[0]['mountpoint'] == path and roots[1]['source'] == project and
                _same_object(volume['truth'], truth[path]), 'V1 helper input differs from volume truth')
        _root_matches(helper, '000', truth[path], 0xef53)
        _root_matches(helper, '001', truth[project], 0x9123683e)
        watch = _step(obs, 'watch_%d' % index)
        require(watch == {'id': before[index]['id'], 'pid': before[index]['pid'],
                          'started_at': before[index]['started_at'], 'pidfd_alive': True},
                'V1 coordinator pidfd watch differs')
    _step(obs, 'final_truth', False)
    isolation = result['isolation']
    require(isolation['v1_consumers'] == [item['id'] for item in before] and
            isolation['v1_consumers_final'] == before and
            isolation['v1_uid_watches_final'] == [
                {'id': item['id'], 'pid': item['pid'], 'started_at': item['started_at'], 'pidfd_alive': True}
                for item in before], 'V1 consumers did not remain alive until final observation')
    held = isolation['v1_root_held_initial']
    require(type(held) is list and len(held) == 2 and held == isolation['v1_root_held_final'],
            'V1 original held pidfd evidence changed')
    for actual, consumer in zip(held, before):
        _held_consumer(actual, consumer)
    require(not isolation.get('truth_errors'), 'V1 final fixture capture failed')
    if result.get('completed') is True:
        cleanup = isolation['cleanup']
        require(type(cleanup) is dict and type(cleanup.get('remaining')) is int and cleanup['remaining'] == 0 and
                cleanup.get('watch_errors') == [] and
                type(cleanup.get('retired')) is list and len(cleanup['retired']) == len(set(cleanup['retired'])) and
                {item['id'] for item in before} <= set(cleanup['retired']),
                'V1 consumers were not watched and retired by exact ID')


def _case_v2(obs):
    name, path = _volume(obs['volume'], False)
    nested_source = '/lab/btr/p/nested-source'
    truth = obs['truth_before']
    require(type(truth) is dict and set(truth) == {path, path + '/nested', nested_source} and
            truth == obs['truth_after'] and _same_object(truth[path], obs['volume']['truth']) and
            _same_object(truth[path + '/nested'], truth[nested_source]), 'V2 nested fixture truth changed')
    expected = {path + '/g2-readonly-sentinel': hashlib.sha256(b'G2-READONLY-SENTINEL\n').hexdigest(),
                path + '/nested/g2-readonly-sentinel':
                    hashlib.sha256(b'G2-READONLY-SENTINEL\nnested\n').hexdigest()}
    require(obs['sentinel_before'] == obs['sentinel_after'] == expected, 'V2 sentinel content changed')
    helper, deny = _step(obs, 'observe'), _step(obs, 'deny')
    for value in (helper, deny):
        root = value['preparation']['request']['input']['roots'][0]
        require(root['name'] == name and root['mountpoint'] == path, 'V2 helper volume differs')
    _root_matches(helper, '000', truth[path])
    _root_matches(helper, 'nested:000', truth[nested_source], 0x9123683e)
    require('/roots/000/nested' in helper['mount_graph'] and
            all(_record(deny, '%03d:readonly_open' % index)['errno'] == errno.EROFS for index in (0, 1)),
            'V2 nested mount or readonly rejection missing')
    _step(obs, 'final_truth', False)


def _case_v3(result, obs, profile):
    name, path = _volume(obs['volume'], False)
    truth = obs['truth_before']
    preparation.fields(truth, ('identity', 'entries'), 'empty volume truth')
    require(truth == obs['truth_after'] and truth['entries'] == [] and
            _same_object(truth['identity'], obs['volume']['truth']), 'V3 empty volume changed')
    image = result['isolation']['image_fixture']
    require(obs['image_fixture'] == image and image['image_id'] == result['isolation']['image'] and
            image['marker_path'] == '/roots/000/g2-image-marker' and
            image['marker_sha256'] == hashlib.sha256(b'g2-image-nocopy-marker\n').hexdigest(),
            'V3 image fixture differs')
    _step(obs, 'fixture', False)
    helper = _step(obs, 'nocopy')
    root = helper['preparation']['request']['input']['roots'][0]
    require(root['name'] == name and root['mountpoint'] == path, 'V3 helper volume differs')
    _root_matches(helper, '000', truth['identity'])
    missing = _step(obs, 'missing')
    absent = 'g2-' + TOKEN + '-does-not-exist-' + profile.lower()
    require(missing['reason'] == 'unknown_volume' and
            missing['rejection'] == {'type': 'UnknownVolumeAdmission', 'code': 'unknown_unregistered_volume',
                                     'message': 'volume was not previously registered'} and
            missing['request'] == {'op': 'helper', 'image': image['image_id'], 'profile': profile, 'case': 'V3',
                                   'phase': 'after', 'roots': [{'type': 'volume', 'name': absent}]},
            'V3 missing-volume admission rejection differs')
    listing = ['volume', 'ls', '--format', '{{.Name}}']
    require(missing['commands'] == [listing, ['volume', 'inspect', absent], listing, listing] and
            type(missing['before']) is list and all(type(v) is str for v in missing['before']) and
            missing['before'] == sorted(set(missing['before'])) == missing['after'] and
            absent not in missing['before'] and missing['owner_state_unchanged'] is True and
            missing['pending_before'] == missing['pending_after'] == [] and
            type(missing['created_before']) is list and
            missing['created_before'] == missing['created_after'] == sorted(set(missing['created_before'])) and
            all(type(cid) is str and re.fullmatch(r'[0-9a-f]{64}', cid) for cid in missing['created_before']),
            'V3 command log, volume set, or owned create state changed')
    _step(obs, 'final_truth', False)


def _case_i0(obs, guard):
    import handoff
    require(obs['fixture_before'] == obs['fixture_after'], 'I0 guard fixture changed')
    _fixture_guard(guard, obs['fixture_before']['guard'])
    daemon = obs['daemon']
    require(daemon['mount_namespace_identity'] == 'UNPROVEN', 'I0 namespace boundary changed')
    _guard_matches(guard, daemon['guard'])
    boot, helper = daemon['bootstrap'], daemon['helper']
    sender = boot['sender']
    preparation.fields(sender, ('pid', 'uid', 'gid'), 'daemon sender credentials')
    require(type(sender['pid']) is int and sender['pid'] > 0 and sender['uid'] == 0 and sender['gid'] == 0 and
            type(sender['uid']) is int and type(sender['gid']) is int and
            boot['first_ping'] == boot['second_ping'] == sender and boot['pidfd_alive'] is True,
            'I0 daemon credential or pidfd evidence differs')
    preparation.identity(boot['socket'], 'daemon socket')
    process = helper['preparation']['raw_evidence']['daemon']
    view = helper['preparation']['raw_evidence']['observer_view']
    target_stat = handoff.proc_stat(_record(helper, 'target_stat')['value']['text'])
    require(daemon['stat'] == target_stat == {'pid': sender['pid'], 'starttime': process['starttime']} and
            process['pid'] == sender['pid'] and helper['preparation']['request']['daemon']['pid'] == sender['pid'] and
            helper['preparation']['request']['input']['metadata_pid'] == sender['pid'] and
            process['before_mountinfo'] == process['after_mountinfo'] and
            _record(helper, 'target_mountinfo')['value']['text'] == view['before']['metadata_mountinfo'] ==
            view['after']['metadata_mountinfo'] and
            _record(helper, 'target_pidns')['value'] == process['pidns'] and
            _record(helper, 'target_exe')['value'] == daemon['dockerd_exe'],
            'I0 daemon metadata differs from pinned process')
    preparation.identity(daemon['dockerd_exe'], 'dockerd executable')
    control = daemon['control_directory']
    require(type(control) is dict and set(control) == {'dev', 'ino', 'mode'} and
            helper['preparation']['request']['input']['roots'][0]['source'] == '/lab/control' and
            all(_record(helper, '000:fstat')['value'][key] == control[key] for key in control) and
            type(control['mode']) is int and stat.S_ISDIR(control['mode']) and
            stat.S_IMODE(control['mode']) == 0o700, 'I0 control directory identity differs')


def _case_running(obs, case, profile):
    source = '/lab/ext/%s-%s' % (case.lower(), profile.lower())
    before, after = obs['consumer_before'], obs['consumer_after']
    _consumer(before)
    require(before == after and obs['consumer_held_before'] == obs['consumer_held_after'],
            case + ' consumer or held pidfd changed')
    begin = obs['begin']
    _begin_matches(begin, before, obs['consumer_held_before'], source)
    helper = _target_observation(obs['observation'], begin)
    require(obs['target_mountinfo_before'] == obs['target_mountinfo_after'] ==
            helper['preparation']['raw_evidence']['target']['before_mountinfo'],
            case + ' independent target mount graphs differ')
    _target_graph(obs['target_mountinfo_before'])
    truth = obs['truth_before']
    require(truth == obs['truth_after'], case + ' fixture truth changed')
    finish = obs['finish']
    require(type(finish) is dict and finish['target_identity'] == begin['stat'] and
            finish['pidfd_alive'] is True and finish['mountinfo_stable'] is True and
            finish['source'] == _record(helper, '000:fstat')['value'] and
            finish['actual'] == _record(helper, '001:fstat')['value'],
            case + ' finish differs from actual helper output')
    if case == 'I1':
        preparation.fields(truth, ('source', 'actual'), 'I1 fixture truth')
        require(_same_object(truth['source'], truth['actual']) and finish['accepted'] is True and
                finish['reason'] == 'match', 'I1 actual object did not match source')
        _root_matches(helper, '000', truth['source'], 0xef53)
        _root_matches(helper, '001', truth['actual'], 0xef53)
        return
    require(case == 'I2', 'unknown running identity case')
    before_cover, after_cover = obs['pre_cover'], obs['post_cover']
    for snapshot in (before_cover, after_cover, truth):
        preparation.fields(snapshot, ('source', 'old', 'actual', 'fixture_mountinfo', 'consumer_mountinfo'),
                           'I2 held truth')
        for name in ('source', 'old', 'actual'):
            _object(snapshot[name], 'I2 ' + name)
        _fixture_graph(snapshot['fixture_mountinfo'])
        _fixture_graph(snapshot['consumer_mountinfo'])
    propagation = obs['alias_propagation']
    preparation.fields(propagation, ('before_private', 'after_private'), 'I2 alias propagation')
    alias = '/lab/ext/i2-alias-' + profile.lower()
    alias_before = _mount_at(_fixture_graph(propagation['before_private']), alias)
    alias_private = _mount_at(_fixture_graph(propagation['after_private']), alias)
    alias_held = _mount_at(_fixture_graph(before_cover['fixture_mountinfo']), alias)
    alias_after = _mount_at(_fixture_graph(after_cover['fixture_mountinfo']), alias)
    current = _mount_at(_fixture_graph(after_cover['fixture_mountinfo']), source)
    target_mount = _mount_at(_fixture_graph(after_cover['consumer_mountinfo']), '/data')
    object_keys = ('major_minor', 'root', 'fs_type', 'source')
    require(all(all(row[key] == alias_before[key] for key in object_keys)
                for row in (alias_private, alias_held, alias_after, target_mount)) and
            alias_after['fs_type'] == 'ext4' and current['fs_type'] == 'btrfs' and
            not any(field == 'unbindable' or field.startswith(('shared:', 'master:', 'propagate_from:'))
                    for field in alias_private['optional']), 'I2 alias or consumer mount graph differs')
    replacement = obs['replacement_truth_before']
    require(replacement == obs['replacement_truth_after'] and _same_object(replacement, after_cover['source']) and
            _same_object(before_cover['source'], before_cover['old']) and
            _same_object(before_cover['actual'], before_cover['old']) and
            _same_object(after_cover['old'], before_cover['old']) and
            _same_object(after_cover['actual'], before_cover['old']) and after_cover == truth and
            before_cover['consumer_mountinfo'] == after_cover['consumer_mountinfo'] and
            truth['source']['dev'] != truth['old']['dev'] and finish['accepted'] is False and
            finish['reason'] == 'running_mount_mismatch', 'I2 held old object or concrete mismatch missing')
    _root_matches(helper, '000', truth['source'], 0x9123683e)
    _root_matches(helper, '001', truth['old'], 0xef53)


def _case_i3(obs, profile):
    ids = []
    for index, name in enumerate(('before_create', 'after_helper')):
        detail = obs[name]
        require(not detail.get('truth_errors'), 'I3 fixture truth capture failed')
        before, held = detail['consumer_before'], detail['consumer_held_before']
        _consumer(before)
        ids.append(before['id'])
        source = '/lab/ext/i3-%s-%d' % (profile.lower(), index)
        begin = detail['begin']
        _begin_matches(begin, before, held, source)
        _target_graph(detail['target_mountinfo_before'])
        require(detail['truth_before'] == detail['truth_after'] and
                _same_object(detail['truth_before'], detail['actual_before']),
                'I3 original target source truth differs')
        stop = detail['stop']
        require(type(stop) is dict and stop.get('argv') ==
                ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'stop', '--timeout', '10', before['id']] and
                type(stop.get('returncode')) is int and stop['returncode'] == 0 and
                type(stop.get('stdout')) is str and stop['stdout'].strip() == before['id'] and
                detail['root_pidfd_exited'] is True and type(detail['root_pidfd_info_after_exit']) is str and
                re.findall(r'^Pid:\s*(-?[0-9]+)\s*$', detail['root_pidfd_info_after_exit'], re.M) in
                (['-1'], [str(before['pid'])]),
                'I3 stopped original target or held pidfd exit missing')
        stopped = detail['consumer_stopped']
        require(type(stopped) is dict and set(stopped) == set(before) and stopped['id'] == before['id'] and
                stopped['running'] is False and stopped['status'] == 'exited' and
                stopped['started_at'] == before['started_at'], 'I3 stopped target inspection differs')
        observation = detail['observation']
        if name == 'before_create':
            require(observation == {'status': 'REJECTED', 'reason': 'target_exited', 'pidfd_exited': True,
                                    'helper_create_issued': False, 'commands': []},
                    'I3 pre-create target rejection or command log missing')
        else:
            helper = _target_observation(observation, begin)
            require(detail['target_mountinfo_before'] ==
                    helper['preparation']['raw_evidence']['target']['before_mountinfo'],
                    'I3 observed target mount graph differs from original target')
            _root_matches(helper, '000', detail['truth_before'], 0xef53)
            _root_matches(helper, '001', detail['actual_before'], 0xef53)
        restart, restarted = detail['restart'], detail['consumer_restarted']
        require(type(restart) is dict and restart.get('argv') ==
                ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'start', before['id']] and
                type(restart.get('returncode')) is int and restart['returncode'] == 0 and
                type(restart.get('stdout')) is str and restart['stdout'].strip() == before['id'],
                'I3 restart command evidence missing')
        _consumer(restarted)
        require(restarted == detail['consumer_after'] and restarted['id'] == before['id'] and
                restarted['pid'] != before['pid'] and restarted['started_at'] != before['started_at'] and
                detail['finish'] == {'accepted': False, 'reason': 'target_exited', 'pidfd_exited': True},
                'I3 stale target output accepted after restart')
    require(ids[0] != ids[1], 'I3 failure paths reused a target')


def validate_case_detail(result, case, profile, observations):
    """Cross-check portable verdicts against the actual case and independent fixture records."""
    try:
        require(not observations.get('truth_errors'), 'case fixture truth incomplete')
        guard = _coordinator_identity(result['isolation'])
        if case == 'C0':
            _case_c0(observations, profile, guard)
        elif case == 'V1':
            _case_v1(result, observations)
        elif case == 'V2':
            _case_v2(observations)
        elif case == 'V3':
            _case_v3(result, observations, profile)
        elif case == 'I0':
            _case_i0(observations, guard)
        elif case in ('I1', 'I2'):
            _case_running(observations, case, profile)
        elif case == 'I3':
            _case_i3(observations, profile)
        else:
            raise Failure('unknown matrix case')
    except (KeyError, IndexError, TypeError, ValueError, OSError) as exc:
        raise Failure('invalid %s/%s case evidence: %s' % (case, profile, exc)) from exc


def _retired_absence(retirement, inspect, cid):
    require(type(retirement) is dict and retirement.get('id') == cid and retirement.get('absent') is True and
            type(retirement.get('remove')) is dict and retirement['remove'].get('returncode') == 0 and
            retirement['remove'].get('argv') ==
            ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'rm', cid] and
            retirement['remove'].get('stdout', '').strip() == cid, 'exact retirement missing')
    require(type(inspect) is dict and inspect.get('argv') ==
            ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'container', 'inspect', cid] and
            type(inspect.get('returncode')) is int and inspect['returncode'] != 0 and
            'No such' in inspect.get('stderr', ''), 'independent retirement inspection missing')


def _unchanged_ready_intents(obs):
    before = obs['ready_intents_before']
    require(type(before) is list and before == sorted(set(before)) and
            all(type(name) is str and re.fullmatch(r'ready-intent-[0-9]{6,}\.json', name) for name in before) and
            before == obs['ready_intents_after'], 'ready-intent ledger changed')


def _sentinel(value):
    require(type(value) is dict and set(value) == {'identity', 'sha256'} and type(value['identity']) is dict and
            type(value['identity'].get('ino')) is int and value['identity']['ino'] > 0 and
            type(value['identity'].get('mode')) is int and stat.S_ISREG(value['identity']['mode']) and
            value['sha256'] == hashlib.sha256(b'G2-READONLY-SENTINEL\n').hexdigest(), 'sentinel evidence invalid')


def validate_preparation_detail(result, obs, variant, profile):
    import handoff
    isolation = result['isolation']
    require(obs['source_before'] == obs['source_after'] and obs['sentinel_before'] == obs['sentinel_after'],
            'negative source or sentinel changed')
    _unchanged_ready_intents(obs)
    if variant not in KILL_NEGATIVES:
        value = obs['negative_observation']
        summary = preparation.validate_negative_observation(value, variant)
        require(obs['negative_binding'] == summary and value['profile'] == profile and
                value['image'] == isolation['image_fixture']['image_id'] and
                value['coordinator_namespaces'] == isolation['fixture_namespaces'] and
                value['inspect']['config']['Labels']['run'] == TOKEN, 'negative guest binding mismatch')
        owned = obs['owned_create_receipt']
        preparation.validate_negative_create_binding(value, owned)
        if value['running_inspect']['HostConfig']['OomKillDisable'] is None:
            info = isolation['docker_info']
            require(type(info) is dict and info.get('CgroupVersion') == '2' and
                    info.get('OomKillDisable') is False, 'negative OOM normalization lacks daemon evidence')
        before, after = obs['supervisor_counts_before'], obs['supervisor_counts_after']
        preparation.fields(before, ('launches', 'exits', 'faults'), 'negative supervisor counts before')
        preparation.fields(after, ('launches', 'exits', 'faults'), 'negative supervisor counts after')
        require(all(type(row[key]) is int and row[key] >= 0 for row in (before, after) for key in row) and
                before['launches'] == before['exits'] and after['faults'] == before['faults'] and
                after['launches'] == before['launches'] + (1 if variant in MALFORMED_NEGATIVES else 0) and
                after['exits'] == before['exits'] + (1 if variant in MALFORMED_NEGATIVES else 0),
                'negative supervisor launch or exit count differs')
        require(type(obs['source_before']) is dict and set(obs['source_before']) == {'identity', 'mountinfo'},
                'negative source evidence missing')
        source = obs['source_before']['identity']
        require(type(source) is dict and type(source.get('ino')) is int and source['ino'] > 0 and
                type(source.get('mode')) is int and stat.S_ISDIR(source['mode']), 'negative source identity invalid')
        handoff.custody_graph(obs['source_before']['mountinfo'])
        _sentinel(obs['sentinel_before'])
        _retired_absence(value['retirement'], obs['observer_removal_inspect'], summary['id'])
        stop = obs['observer_stop']
        require(stop == value['stop'], 'negative observer stop receipt mismatch')
        if variant == 'no_ready':
            require(type(stop) is dict and stop.get('argv') ==
                    ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'stop', '--timeout', '10', summary['id']] and
                    stop.get('returncode') == 0 and stop.get('stdout', '').strip() == summary['id'],
                    'no-ready observer stop evidence missing')
        else:
            require(stop is None, 'unexpected negative observer stop')
        if variant in MALFORMED_NEGATIVES:
            receipt = value['preparation']['receipt_value']
            install = validate_guest_install(isolation)
            require(receipt['request']['input']['preparer_sha256'] == install['copy_sha256'] and
                    receipt['launch']['exe'] == install['copy'] and
                    receipt['launch']['session_nonce'] == isolation['coordinator']['session_nonce'] and
                    receipt['request']['input']['run_token'] == TOKEN, 'negative preparer build binding mismatch')
            require(obs['supervisor_launch'] == receipt['launch'] and obs['supervisor_exit'] == receipt['exit'],
                    'negative supervisor receipts differ')
        ordinal = (value['preparation']['receipt_value']['request']['input']['ordinal']
                   if variant in MALFORMED_NEGATIVES else None)
        return {'id': summary['id'], 'nonce': summary['nonce'], 'ordinal': ordinal, 'retired_ids': []}
    return _validate_kill_detail(result, obs, variant, profile)


def _validate_kill_detail(result, obs, point, profile):
    import guest_matrix
    import handoff
    diag, fault = obs['diagnostic'], obs['fault_receipt']
    prep, receipt = diag['preparation'], diag['preparation_receipt']
    preparation.fields(receipt, ('version', 'request', 'create_spec', 'observer_waiting', 'helper_state',
                                 'coordinator_namespaces', 'launch', 'exit', 'captures', 'pidfd_exited', 'result',
                                 'raw_evidence', 'channel_error', 'test_fault'), 'fault preparation receipt')
    preparation.fields(fault, ('point', 'request_digest', 'pid', 'ordinal', 'nonce', 'progress_snapshots',
                               'evidence', 'evidence_sha256', 'signaled_monotonic_ns', 'wait_status',
                               'exit_observed_monotonic_ns'), 'fault receipt')
    require(type(prep) is dict and type(receipt) is dict and prep['receipt_value'] == receipt and
            prep['pidfd_exited'] is True and receipt['pidfd_exited'] is True and
            type(receipt['version']) is int and receipt['version'] == 1 and receipt['test_fault'] is None and
            receipt['result'] is None and receipt['raw_evidence'] is None and
            type(receipt['channel_error']) is dict, 'faulted preparation receipt missing')
    preparation.fields(receipt['channel_error'], ('type', 'message'), 'fault channel error')
    require(type(receipt['channel_error']['type']) is str and bool(receipt['channel_error']['type']) and
            type(receipt['channel_error']['message']) is str and bool(receipt['channel_error']['message']) and
            prep['channel_error'] == receipt['channel_error'], 'fault channel closure evidence missing')
    preparation.hex_string(prep['receipt_sha256'], 'fault receipt digest')
    require(hashlib.sha256(preparation.canonical_json(receipt) + b'\n').hexdigest() == prep['receipt_sha256'],
            'faulted preparation receipt digest mismatch')
    request = preparation.validate_request(receipt['request'])
    inp, launch, exited = request['input'], receipt['launch'], receipt['exit']
    preparation._launch_exit(launch, exited, request, success=False)
    isolation = result['isolation']
    install, coordinator = validate_guest_install(isolation), isolation['coordinator']
    require(receipt['captures'] == exited['captures'], 'fault capture ledger differs from supervisor exit')
    preparation.validate_capture_ledger(receipt['captures'], request, launch['session_nonce'],
        preparer_exe=launch['exe'], preparer_pid=launch['pid'],
        coordinator_namespaces=receipt['coordinator_namespaces'])
    intents = diag['capture_intents']
    require(type(intents) is dict and set(intents) == {
        'capture-intent-%06d-%d.json' % (inp['ordinal'], index + 1)
        for index in range(len(receipt['captures']))}, 'fault independent capture-intent inventory differs')
    controls = diag['capture_controls']
    require(type(controls) is dict and set(controls) == {
        'capture-%s-%06d-%d.json' % (kind, inp['ordinal'], index + 1)
        for index in range(len(receipt['captures'])) for kind in ('launch', 'exit')},
        'fault independent capture-control inventory differs')
    for index, capture in enumerate(receipt['captures']):
        name = 'capture-intent-%06d-%d.json' % (inp['ordinal'], index + 1)
        require(handoff.same_json(intents[name], capture['intent']) and
                hashlib.sha256(preparation.canonical_json(intents[name])).hexdigest() == capture['intent_sha256'],
                'fault independent capture intent differs')
        for kind in ('launch', 'exit'):
            control_name = 'capture-%s-%06d-%d.json' % (kind, inp['ordinal'], index + 1)
            expected = {'version': 1, 'op': 'capture_' + kind, 'binding': capture['intent']['binding'],
                        kind: capture[kind]}
            require(handoff.same_json(controls[control_name], expected), 'fault independent capture control differs')
    require(inp['run_token'] == TOKEN and inp['case'] == 'I1' and inp['profile'] == profile and
            inp['phase'] == 'before' and inp['imageID'] == isolation['image_fixture']['image_id'] and
            inp['preparer_sha256'] == install['copy_sha256'] == install['seed_sha256'] ==
            file_hash(RUN / 'seed/preparer') and launch['exe'] == install['copy'] and
            launch['session_nonce'] == coordinator['session_nonce'] and
            receipt['coordinator_namespaces'] == isolation['fixture_namespaces'] and
            receipt['launch'] == prep['launch'] == obs['supervisor_launch'] and
            receipt['exit'] == prep['exit'] == obs['supervisor_exit'], 'fault launcher binding mismatch')
    cid = diag['helper_id']
    preparation.hex_string(cid, 'fault helper ID')
    require(request['helper']['id'] == cid and diag['request'] == request and
            diag['receipt'] == '/lab/control/observation-' + cid + '.json' and
            prep['receipt'] == '/lab/control/prepare-result-%06d.json' % inp['ordinal'] and
            diag['rejection'].get('code') == 'prepare_protocol', 'fault helper identity mismatch')
    observed_request = diag['observation_request']
    source = '/lab/ext/kill-%s-%s' % (profile.lower(), point)
    require(request['target'] is not None and inp['metadata_pid'] == request['target']['pid'] and
            inp['exe'] is False and inp['nested'] is False and inp['deny'] is False and
            inp['roots'] == [{'kind': 'declared_bind', 'ordinal': 0, 'target': '/roots/000', 'source': source},
                             {'kind': 'running_root', 'ordinal': 1, 'target': '/roots/001'}] and
            handoff.same_json(observed_request, {'id': cid, 'run_token': TOKEN, 'image': inp['imageID'],
                'profile': profile, 'case': 'I1', 'phase': 'before', 'roots': [
                    {'type': 'bind', 'source': source}, {'type': 'running_root'}],
                'metadata_pid': request['target']['pid'], 'exe': False, 'nested': False, 'deny': False}),
            'fault observation request mismatch')
    wait_status = exited['wait_status']
    require(os.WIFSIGNALED(wait_status) and os.WTERMSIG(wait_status) == signal.SIGKILL and
            exited['exit_code'] is None and fault['wait_status'] == wait_status and fault['point'] == point and
            fault['pid'] == launch['pid'] and type(fault['signaled_monotonic_ns']) is int and
            fault['signaled_monotonic_ns'] > 0 and type(fault['exit_observed_monotonic_ns']) is int and
            fault['exit_observed_monotonic_ns'] >= fault['signaled_monotonic_ns'],
            'owned preparer SIGKILL and wait evidence missing')
    attach, state = diag['start_attach'], diag['final_state']
    require(handoff.raw_capture_problem(attach) is None and type(attach['returncode']) is int and
            attach['returncode'] != 0 and attach['argv'] ==
            ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'start', '--attach', '--interactive', cid] and
            attach['stdout'].endswith('\n') and type(state) is dict and state.get('Running') is False and
            state.get('Status') == 'exited' and type(state.get('ExitCode')) is int and state['ExitCode'] != 0 and
            state.get('StartedAt') == receipt['helper_state'].get('StartedAt') and
            diag.get('helper_pidfd_exited') is True, 'fault observer output, pidfd or exit missing')
    rows = attach['stdout'].splitlines()
    require(len(rows) == 1 and preparation.strict_json(rows[0].encode()) == diag['waiting'],
            'fault observer emitted more than waiting')
    waiting = diag['waiting']
    preparation.fields(waiting, ('experiment_kind', 'preparation_version', 'metadata_mode', 'case', 'phase',
                                 'root_ordinal', 'operation', 'errno', 'value'), 'fault waiting')
    require(waiting['experiment_kind'] == 'input-handoff-v1' and type(waiting['preparation_version']) is int and
            waiting['preparation_version'] == 1 and waiting['metadata_mode'] == 'proc-v1' and
            waiting['case'] == 'I1' and waiting['phase'] == 'before' and waiting['root_ordinal'] is None and
            waiting['operation'] == 'waiting' and type(waiting['errno']) is int and waiting['errno'] == 0 and
            waiting['value'] == receipt['observer_waiting'] and waiting['value']['nonce'] == inp['nonce'] and
            waiting['value']['input_digest'] == request['input_digest'] and
            receipt['helper_state']['Pid'] == request['helper']['pid'] and
            receipt['helper_state']['Running'] is True, 'fault waiting identity mismatch')
    raw_inspect, running_inspect = diag['inspect_before'], diag['running_inspect']
    inspected = {'mounts': raw_inspect['Mounts'], 'host_config': raw_inspect['HostConfig'],
                 'apparmor': raw_inspect['AppArmorProfile'], 'config': raw_inspect['Config'],
                 'image': raw_inspect['Image'], 'id': raw_inspect['Id']}
    owned = diag['owned_create_receipt']
    preparation.validate_negative_create_binding({
        'helper_id': cid, 'create_spec': receipt['create_spec'], 'image': inp['imageID'],
        'inspect': inspected, 'running_inspect': running_inspect}, owned)
    created_hc, running_hc = raw_inspect['HostConfig'], running_inspect['HostConfig']
    require(type(created_hc) is dict and type(running_hc) is dict and created_hc.keys() == running_hc.keys() and
            created_hc.get('OomKillDisable') is False and
            (running_hc.get('OomKillDisable') is False or running_hc.get('OomKillDisable') is None) and
            all(key == 'OomKillDisable' or handoff.same_json(created_hc[key], running_hc[key])
                for key in created_hc) and
            handoff.same_json(handoff.mount_map(raw_inspect['Mounts']), handoff.mount_map(running_inspect['Mounts'])),
            'fault running HostConfig or complete mount set differs')
    if running_hc['OomKillDisable'] is None:
        info = isolation['docker_info']
        require(type(info) is dict and info.get('CgroupVersion') == '2' and info.get('OomKillDisable') is False,
                'fault OOM normalization lacks daemon evidence')
    running_state = running_inspect['State']
    require(type(running_state) is dict and handoff.same_json(running_state, receipt['helper_state']) and
            running_state.get('Running') is True and running_state.get('Status') == 'running' and
            type(running_state.get('Pid')) is int and running_state['Pid'] > 0 and
            running_state['Pid'] == request['helper']['pid'] and
            type(running_state.get('StartedAt')) is str and bool(running_state['StartedAt']) and
            running_state['StartedAt'] != '0001-01-01T00:00:00Z' and
            state.get('StartedAt') == running_state['StartedAt'], 'fault running helper identity differs')
    runtime_view = {'mounts': running_inspect['Mounts'], 'host_config': running_hc,
                    'apparmor': running_inspect['AppArmorProfile'], 'config': running_inspect['Config'],
                    'image': running_inspect['Image'], 'id': running_inspect['Id']}
    preparation._inspect_observer({'id': cid, 'inspect': inspected}, request, receipt['create_spec'])
    preparation._inspect_observer({'id': cid, 'inspect': runtime_view}, request, receipt['create_spec'])
    preparation._observer_status(waiting['value']['self_status'], runtime_view['apparmor'],
                                 receipt['coordinator_namespaces'])
    require(waiting['value']['self_status']['apparmor']['label'] == runtime_view['apparmor'] + ' (enforce)' and
            not any(option.lower().startswith('apparmor') for option in running_hc.get('SecurityOpt') or []),
            'fault observer default AppArmor is not enforced')
    preparation.validate_request_namespaces(request, receipt['coordinator_namespaces'], waiting['value'])
    require(waiting['value']['self_status']['namespaces']['mnt'] == request['helper']['mntns'],
            'fault observer namespace mismatch')
    consumer = obs['consumer_before']
    truth = obs['source_before']
    begin = obs['begin']
    preparation.fields(begin, ('id', 'pid', 'started_at', 'source', 'destination', 'stat', 'pidfd_alive'),
                       'fault target begin')
    require(begin['id'] == consumer['id'] and begin['pid'] == consumer['pid'] and
            begin['started_at'] == consumer['started_at'] and begin['source'] == source and
            begin['destination'] == '/data' and begin['stat'] == consumer['process_stat'] and
            begin['pidfd_alive'] is True, 'fault target begin differs')
    require(consumer == obs['consumer_after'] and consumer['running'] is True and consumer['pidfd_alive'] is True and
            consumer['id'] == request['target']['id'] and consumer['pid'] == request['target']['pid'] and
            consumer['process_stat']['pid'] == consumer['pid'] and
            consumer['process_stat']['starttime'] == request['target']['starttime'] and
            truth['daemon_stat']['pid'] == request['daemon']['pid'] and
            truth['target_stat'] == consumer['process_stat'] and truth['source'] == truth['actual'] and
            type(consumer['pidfd_info']) is str and
            re.findall(r'^Pid:\s*([0-9]+)\s*$', consumer['pidfd_info'], re.M) == [str(consumer['pid'])] and
            re.search(r'^NSpid:\s*[0-9]+(?:\s+[0-9]+)*\s*$', consumer['pidfd_info'], re.M),
            'fault consumer identity changed')
    sentinels = obs['sentinel_before']
    require(type(sentinels) is dict and set(sentinels) == {'source', 'actual'} and
            sentinels['source'] == sentinels['actual'], 'fault sentinel identity mismatch')
    _sentinel(sentinels['source'])
    require(diag['ready_intent_name'] == 'ready-intent-%06d.json' % inp['ordinal'] and
            diag['ready_intent_name'] not in obs['ready_intents_after'], 'fault observer ready intent exists')
    class PortableFaultChecks:
        check = staticmethod(require)
    PortableFaultChecks.handoff = handoff
    guest_matrix.validate_fault_progress(PortableFaultChecks, fault, request, truth)
    _retired_absence(obs['retirement'], diag['removal_inspect'], cid)
    lifecycle = preparation.validate_daemon_lifecycle(diag['daemon_lifecycle'], cid, retirement=obs['retirement'])
    require(lifecycle['pid'] == request['daemon']['pid'] == truth['daemon_stat']['pid'] and
            lifecycle['starttime'] == truth['daemon_stat']['starttime'], 'fault daemon lifecycle identity changed')
    _retired_absence(obs['target_retirement'], obs['target_removal_inspect'], consumer['id'])
    require(obs['target_abort'] == {'aborted': True}, 'fault target pidfd was not retired')
    return {'id': cid, 'nonce': inp['nonce'], 'ordinal': inp['ordinal'], 'retired_ids': [consumer['id']]}


def validate_preparation_checks(result, used):
    checks = result['preparation_checks']
    require(type(checks) is dict and set(checks) == set(PREPARATION_VARIANTS), 'preparation variant set mismatch')
    for variant, profiles in checks.items():
        require(type(profiles) is dict and set(profiles) == {'A', 'B'}, 'preparation profile set mismatch')
        for profile, item in profiles.items():
            require(type(item) is dict and set(item) == {'status', 'observations'} and
                    item['status'] in ('PASS', 'FAIL', 'INCONCLUSIVE', 'NOT_RUN') and
                    type(item['observations']) is dict and
                    all(item['observations'].get(key) == value for key, value in EXPERIMENT_BOUNDARY.items()),
                    'invalid preparation observation schema')
            if item['status'] != 'PASS':
                continue
            obs = item['observations']
            required = obs.get('required_evidence')
            require(type(required) is dict and set(required) == set(REQUIRED_PREPARATION_EVIDENCE[variant]) and
                    all(value is True for value in required.values()), 'missing required preparation evidence')
            dependencies = ('I0', 'I1') if variant in KILL_NEGATIVES else ('C0',)
            require(all(result['cases'][name][profile]['status'] == 'PASS' for name in dependencies),
                    'preparation check passed without dependency')
            try:
                binding = validate_preparation_detail(result, obs, variant, profile)
                lifecycle = (obs['diagnostic']['daemon_lifecycle'] if variant in KILL_NEGATIVES else
                             obs['negative_observation']['daemon_lifecycle'])
                _bind_daemon(used, lifecycle['value'])
                require(binding['id'] not in used['ids'] and binding['nonce'] not in used['nonces'] and
                        (binding['ordinal'] is None or binding['ordinal'] not in used['ordinals']),
                        'replayed negative preparation identity')
                used['ids'].add(binding['id'])
                used['nonces'].add(binding['nonce'])
                if binding['ordinal'] is not None:
                    used['ordinals'].add(binding['ordinal'])
                used['retired_ids'].update(binding['retired_ids'])
            except (KeyError, TypeError, ValueError, OSError) as exc:
                raise Failure('invalid preparation check: ' + str(exc)) from exc


def validate_coordinator_retirement(result, used):
    try:
        isolation = result['isolation']
        coordinator = isolation['coordinator']
        retired = isolation['coordinator_retirement']
        readback = retired['readback']
        preparation.hex_string(retired['ledger_sha256'], 'coordinator retirement ledger')
        pid = retired['pid']
        status = retired['wait_status']
        pids = re.findall(r'^Pid:\s*([0-9]+)\s*$', coordinator['status'], re.M)
        require(type(pid) is int and pid > 0 and pids == [str(pid)] and
                type(status) is int and os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0 and
                coordinator['guard_registered_before_ready'] is True and
                retired['guard_released_after_exit'] is True and
                readback['ledger_sha256'] == retired['ledger_sha256'] and
                type(retired['preparer_launches']) is int and retired['preparer_launches'] >= len(used['ordinals']) and
                readback['preparers'] == retired['preparer_launches'] and
                type(readback['pending_resolved']) is int and readback['pending_resolved'] >= 0,
                'coordinator retirement or waitpid evidence missing')
        containers = readback['containers']
        require(type(containers) is list and containers == sorted(set(containers)) and
                all(type(cid) is str and re.fullmatch(r'[0-9a-f]{64}', cid) for cid in containers) and
                (used['ids'] | used['retired_ids']) <= set(containers), 'owned container retirement ledger incomplete')
    except (KeyError, TypeError, ValueError) as exc:
        raise Failure('invalid coordinator retirement: ' + str(exc)) from exc


def _fault_binding_shape(value):
    preparation.fields(value, ('version', 'run_token', 'session_nonce', 'ordinal', 'nonce', 'input_digest',
                               'request_digest', 'phase', 'sequence'), 'fault binding')
    require(type(value['version']) is int and value['version'] == 1 and value['run_token'] == TOKEN and
            type(value['sequence']) is int and (value['phase'], value['sequence']) in (('before', 1), ('after', 2)),
            'fault binding identity or order differs')
    preparation.integer(value['ordinal'], 'fault binding ordinal', 1)
    for name in ('session_nonce', 'nonce', 'input_digest', 'request_digest'):
        preparation.hex_string(value[name], 'fault binding ' + name)


def _fault_unbound_capture(record, processes):
    intent = record['intent']
    for name in ('helper', 'metadata_owner'):
        process = intent[name]
        if process is None and name == 'metadata_owner':
            continue
        preparation.fields(process, ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns', 'userns'),
                           'partial capture ' + name)
        preparation.integer(process['pid'], 'partial capture PID', 1, (1 << 31) - 1)
        preparation.integer(process['starttime'], 'partial capture starttime', 1)
        for key in ('mntns', 'pidns', 'cgroupns', 'userns'):
            preparation.identity(process[key], 'partial capture ' + key)
    owner = intent['helper']['userns']
    if intent['metadata_owner'] is not None:
        require(intent['metadata_owner']['userns'] == owner, 'partial capture namespace owner differs')
    launch = record['launch']
    if launch is None:
        return
    preparation.fields(launch, ('binding', 'host_pid', 'starttime', 'exe', 'reader_pidns', 'root'),
                       'partial capture launch')
    require(launch['binding'] == intent['binding'], 'partial capture launch binding differs')
    preparation.integer(launch['host_pid'], 'partial capture reader PID', 1, (1 << 31) - 1)
    preparation.integer(launch['starttime'], 'partial capture reader starttime', 1)
    exe = launch['exe']
    preparation.fields(exe, ('dev', 'ino', 'mode', 'sha256'), 'partial capture executable')
    preparation._object_identity({key: exe[key] for key in ('dev', 'ino', 'mode')}, 'partial executable')
    preparation.hex_string(exe['sha256'], 'partial executable digest')
    require(stat.S_ISREG(exe['mode']) and not exe['mode'] & (stat.S_ISUID | stat.S_ISGID),
            'partial capture executable mode')
    preparation._namespace_evidence(launch['reader_pidns'], 'pid', owner, intent['helper']['pidns'])
    root = launch['root']
    preparation.fields(root, ('active_before', 'children_before', 'children_switched', 'active_after', 'children_after',
                              'tasks_before', 'tasks_after', 'sigchld_default', 'sigchld_nocldwait',
                              'signal_mask_restored'), 'partial root switch')
    for key in ('active_before', 'children_before', 'children_switched', 'active_after', 'children_after'):
        preparation._namespace_evidence(root[key], 'pid', owner)
    tasks = preparation._thread_inventory(root['tasks_before'], 'partial root tasks')
    require(tasks == preparation._thread_inventory(root['tasks_after'], 'partial root tasks after') and
            len(tasks) == 1 and ('root' not in processes or tasks == [processes['root']['pid']]) and
            root['active_before'] == root['children_before'] == root['active_after'] == root['children_after'] and
            root['children_switched'] == launch['reader_pidns'] and root['sigchld_default'] is True and
            root['sigchld_nocldwait'] is False and root['signal_mask_restored'] is True,
            'partial root switch proof differs')
    exited = record['exit']
    if exited is None:
        return
    preparation.fields(exited, ('binding', 'host_pid', 'starttime', 'terminal', 'terminal_digest', 'eof',
                                'pidfd_exited', 'wait_status', 'exit_code', 'diagnostic'), 'partial capture exit')
    require(exited['binding'] == intent['binding'] and exited['host_pid'] == launch['host_pid'] and
            exited['starttime'] == launch['starttime'] and
            all(type(exited[key]) is bool for key in ('terminal', 'eof', 'pidfd_exited')) and
            exited['pidfd_exited'] is True, 'partial capture exit identity differs')
    if exited['terminal']:
        preparation.hex_string(exited['terminal_digest'], 'partial capture terminal digest')
    else:
        require(exited['terminal_digest'] is None, 'partial capture unexpected terminal digest')
    status = preparation.integer(exited['wait_status'], 'partial capture wait', 0, 0xffff)
    require((os.WIFEXITED(status) and type(exited['exit_code']) is int and exited['exit_code'] == os.WEXITSTATUS(status)) or
            (status <= 0xff and os.WIFSIGNALED(status) and exited['exit_code'] is None), 'partial capture wait differs')
    diagnostic = preparation.ascii_string(exited['diagnostic'], 'partial capture diagnostic', 21848)
    try:
        decoded = base64.b64decode(diagnostic, validate=True)
    except (ValueError, TypeError) as exc:
        raise Failure('partial capture diagnostic encoding') from exc
    require(len(decoded) <= 16 * 1024 and base64.b64encode(decoded).decode('ascii') == diagnostic,
            'partial capture diagnostic differs')


def _fault_worker_state(worker, scenario, request, session, processes):
    if worker is None:
        return
    preparation.fields(worker, ('version', 'op', 'scenario', 'error', 'state'), 'fault worker result')
    require(type(worker['version']) is int and worker['version'] == 1 and
            worker['op'] == 'reader_fault_worker_result' and worker['scenario'] == scenario and
            type(worker['error']) is str and 0 < len(worker['error']) <= 2048, 'fault worker result invalid')
    state = worker['state']
    if state is None:
        return
    preparation.fields(state, ('preparer', 'reader', 'captures', 'uncertain', 'faults', 'containment',
                               'preparer_exits', 'injected_frame', 'expected_failure'), 'fault supervisor state')
    require(type(state['uncertain']) is bool and type(state['captures']) is list and len(state['captures']) <= 2 and
            type(state['faults']) is list and len(state['faults']) <= 16 and
            type(state['preparer_exits']) is list and len(state['preparer_exits']) <= 1,
            'fault supervisor state types')
    active, reader = state['preparer'], state['reader']
    if active is not None:
        preparation.fields(active, ('pid', 'ordinal', 'nonce', 'request_digest', 'pidfd_held'), 'active preparer')
        preparation.integer(active['pid'], 'active preparer PID', 1, (1 << 31) - 1)
        preparation.integer(active['ordinal'], 'active preparer ordinal', 1)
        preparation.hex_string(active['nonce'], 'active preparer nonce')
        preparation.hex_string(active['request_digest'], 'active preparer digest')
        require(type(active['pidfd_held']) is bool and
                ('preparer' not in processes or active['pid'] == processes['preparer']['pid']),
                'active preparer process differs')
        if request is not None:
            require(active['ordinal'] == request['input']['ordinal'] and active['nonce'] == request['input']['nonce'] and
                    active['request_digest'] == request['request_digest'], 'active preparer request differs')
    if reader is not None:
        preparation.fields(reader, ('pid', 'starttime', 'pidfd_held', 'stage', 'binding'), 'active reader')
        for key in ('pid', 'starttime'):
            if reader[key] is not None:
                preparation.integer(reader[key], 'active reader ' + key, 1)
        require(type(reader['pidfd_held']) is bool and type(reader['stage']) is str and
                0 < len(reader['stage']) <= 128, 'active reader fields invalid')
        _fault_binding_shape(reader['binding'])
        role = 'reader-' + str(reader['binding']['sequence'])
        if role in processes:
            require(reader['pid'] == processes[role]['pid'] and
                    (reader['starttime'] is None or reader['starttime'] == processes[role]['starttime']),
                    'active reader process differs')
        if request is not None:
            preparation.capture_binding(reader['binding'], request, session)
    for index, record in enumerate(state['captures']):
        preparation.fields(record, ('intent', 'intent_sha256', 'launch', 'exit'), 'fault capture state')
        intent = record['intent']
        preparation.fields(intent, ('version', 'op', 'binding', 'helper', 'metadata_owner'), 'fault state intent')
        require(type(intent['version']) is int and intent['version'] == 1 and intent['op'] == 'capture_intent',
                'fault state intent operation')
        _fault_binding_shape(intent['binding'])
        require(intent['binding']['sequence'] == index + 1 and
                record['intent_sha256'] == hashlib.sha256(preparation.canonical_json(intent)).hexdigest() and
                (record['launch'] is not None or record['exit'] is None), 'fault capture prefix differs')
        if request is not None:
            require(intent['binding']['session_nonce'] == session, 'fault capture session differs')
            preparation.capture_intent(intent, request, session)
            if record['launch'] is not None:
                preparation.capture_launch(record['launch'], intent, request, session,
                                           preparer_pid=processes.get('preparer', {}).get('pid'))
                if record['exit'] is not None:
                    preparation.capture_exit(record['exit'], record['launch'], intent, request, session)
        else:
            _fault_unbound_capture(record, processes)
        if index + 1 < len(state['captures']):
            require(record['launch'] is not None and record['exit'] is not None, 'overlapping fault captures')
    if reader is not None and reader['stage'] in ('exit_observation_pending', 'exit_observation_awaiting_ack'):
        require(scenario == 'reader_nonzero' and active is not None and reader['pidfd_held'] is True and
                len(state['captures']) == 1 and state['captures'][0]['exit'] is not None,
                'pending reader lost its held capture')
        saved = state['captures'][0]['exit']
        require(reader['pid'] == saved['host_pid'] and reader['starttime'] == saved['starttime'] and
                reader['binding'] == saved['binding'] and saved['eof'] is True and saved['pidfd_exited'] is True and
                saved['terminal'] is False and saved['terminal_digest'] is None and
                saved['wait_status'] == 256 and type(saved['exit_code']) is int and saved['exit_code'] == 1,
                'pending reader saved exit differs')
    for item in state['faults']:
        preparation.fields(item, ('scenario', 'point', 'binding', 'effect'), 'fault effect')
        require(item['scenario'] == scenario and type(item['point']) is str and 0 < len(item['point']) <= 128 and
                type(item['effect']) is str and 0 < len(item['effect']) <= 128, 'fault effect identity')
        _fault_binding_shape(item['binding'])
        if request is not None:
            preparation.capture_binding(item['binding'], request, session)
    containment = state['containment']
    if containment is not None:
        preparation.fields(containment, ('reader_pid', 'reader_pidfd_held', 'reader_signal_sent', 'reader_eof',
            'reader_pidfd_exited', 'reader_wait_status', 'preparer_pid', 'preparer_pidfd_held',
            'preparer_signal_sent', 'preparer_pidfd_exited', 'preparer_wait_status'), 'fault containment')
        for name in ('reader_pidfd_held', 'reader_signal_sent', 'reader_eof', 'reader_pidfd_exited',
                     'preparer_pidfd_held', 'preparer_signal_sent', 'preparer_pidfd_exited'):
            require(type(containment[name]) is bool, 'fault containment flag type')
        for name in ('reader_pid', 'preparer_pid'):
            if containment[name] is not None:
                preparation.integer(containment[name], 'fault containment PID', 1, (1 << 31) - 1)
        for name in ('reader_wait_status', 'preparer_wait_status'):
            if containment[name] is not None:
                preparation.integer(containment[name], 'fault containment wait', 0, 0xffff)
        if 'preparer' in processes and containment['preparer_pid'] is not None:
            require(containment['preparer_pid'] == processes['preparer']['pid'], 'containment preparer differs')
        if containment['reader_pid'] is not None and any(key.startswith('reader-') for key in processes):
            require(containment['reader_pid'] in [value['pid'] for key, value in processes.items()
                                                   if key.startswith('reader-')], 'containment reader differs')
        if (scenario == 'reader_nonzero' and containment['reader_pid'] is not None and reader is not None and
                reader['stage'] in ('exit_observation_pending', 'exit_observation_awaiting_ack')):
            for record in state['captures']:
                exited = record['exit']
                if exited is not None and exited['host_pid'] == containment['reader_pid'] and \
                        exited['wait_status'] == 256:
                    require(containment['reader_wait_status'] == exited['wait_status'] and
                            containment['reader_signal_sent'] is False,
                            'fault containment differs from saved reader exit')
    for exited in state['preparer_exits']:
        preparation.fields(exited, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce', 'pid',
                                    'wait_status', 'exit_code', 'capture', 'captures'), 'fault preparer exit')
        require(type(exited['version']) is int and exited['version'] == 1 and exited['op'] == 'preparer_exit' and
                exited['run_token'] == TOKEN and type(exited['capture']) is str and
                type(exited['captures']) is list and exited['captures'] == state['captures'],
                'fault preparer exit fields differ')
        preparation.ascii_string(exited['capture'], 'fault preparer capture', 64 * 1024)
        preparation.hex_string(exited['session_nonce'], 'fault exited session')
        preparation.hex_string(exited['nonce'], 'fault exited nonce')
        preparation.integer(exited['ordinal'], 'fault exited ordinal', 1)
        preparation.integer(exited['pid'], 'fault exited PID', 1, (1 << 31) - 1)
        preparation.integer(exited['wait_status'], 'fault exited wait', 0, 0xffff)
        status = exited['wait_status']
        require((os.WIFEXITED(status) and type(exited['exit_code']) is int and
                 exited['exit_code'] == os.WEXITSTATUS(status)) or
                (status <= 0xff and os.WIFSIGNALED(status) and exited['exit_code'] is None),
                'fault exited wait differs')
        if 'preparer' in processes:
            require(exited['pid'] == processes['preparer']['pid'], 'fault exited preparer differs')
        if request is not None:
            require(exited['ordinal'] == request['input']['ordinal'] and exited['nonce'] == request['input']['nonce'] and
                    exited['session_nonce'] == session, 'fault exited request differs')
    injected = state['injected_frame']
    if injected is not None:
        preparation.fields(injected, ('version', 'op', 'binding'), 'fault injected frame')
        require(type(injected['version']) is int and injected['version'] == 1 and
                injected['op'] == 'reader_allow_read', 'fault injected frame operation')
        _fault_binding_shape(injected['binding'])
        require(session is None or injected['binding']['session_nonce'] == session,
                'fault injected frame session differs')
    require(state['expected_failure'] is None or
            (type(state['expected_failure']) is str and 0 < len(state['expected_failure']) <= 128),
            'fault expected failure type')


def _fault_fixture(fixture, scenario, processes):
    if fixture is None:
        return
    preparation.fields(fixture, ('version', 'op', 'scenario', 'run_token', 'source', 'target_id', 'target_pid',
                                 'daemon_pid', 'coordinator_pid', 'bootstrap', 'begin'), 'fault fixture')
    require(type(fixture['version']) is int and fixture['version'] == 1 and
            fixture['op'] == 'reader_fault_fixture' and fixture['scenario'] == scenario and
            fixture['run_token'] == TOKEN and fixture['source'] == '/lab/ext/reader-fault-source',
            'fault fixture identity')
    preparation.hex_string(fixture['target_id'], 'fault target ID')
    for name in ('target_pid', 'daemon_pid', 'coordinator_pid'):
        preparation.integer(fixture[name], 'fault fixture ' + name, 1, (1 << 31) - 1)
    require('coordinator' in processes and processes['coordinator']['pid'] == fixture['coordinator_pid'],
            'fault coordinator identity differs')
    boot = fixture['bootstrap']
    preparation.fields(boot, ('sender', 'first_ping', 'second_ping', 'pidfd_alive', 'socket'), 'fault bootstrap')
    for name in ('sender', 'first_ping', 'second_ping'):
        sender = boot[name]
        preparation.fields(sender, ('pid', 'uid', 'gid'), 'fault daemon sender')
        require(type(sender['pid']) is int and sender['pid'] == fixture['daemon_pid'] and
                type(sender['uid']) is int and sender['uid'] == 0, 'fault daemon credentials differ')
        preparation.integer(sender['gid'], 'fault daemon gid', 0, (1 << 32) - 1)
    preparation.identity(boot['socket'], 'fault daemon socket')
    require(boot['sender'] == boot['first_ping'] == boot['second_ping'] and boot['pidfd_alive'] is True,
            'fault daemon bootstrap differs')
    begin = fixture['begin']
    preparation.fields(begin, ('id', 'pid', 'started_at', 'source', 'destination', 'stat', 'pidfd_alive'),
                       'fault target begin')
    preparation.fields(begin['stat'], ('pid', 'starttime'), 'fault target stat')
    preparation.integer(begin['stat']['starttime'], 'fault target starttime', 1)
    require(begin['id'] == fixture['target_id'] and type(begin['pid']) is int and
            begin['pid'] == begin['stat']['pid'] == fixture['target_pid'] and begin['source'] == fixture['source'] and
            begin['destination'] == '/data' and begin['pidfd_alive'] is True and
            type(begin['started_at']) is str and bool(begin['started_at']) and
            begin['started_at'] != '0001-01-01T00:00:00Z', 'fault target begin differs')


def _fault_source_bundle(source):
    preparation.fields(source, ('source', 'sentinel', 'actual', 'actual_sentinel', 'daemon_mountinfo',
                                'target_mountinfo'), 'fault source')
    _object(source['source'], 'fault source', directory=False)
    _object(source['actual'], 'fault target source', directory=False)
    for key in ('sentinel', 'actual_sentinel'):
        item = source[key]
        preparation.fields(item, ('identity', 'sha256'), 'fault sentinel')
        _object(item['identity'], 'fault sentinel identity', directory=False)
        preparation.hex_string(item['sha256'], 'fault sentinel digest')
    import handoff
    handoff.custody_graph(source['daemon_mountinfo'])
    handoff.custody_graph(source['target_mountinfo'])


def _fault_exit_observation(result, request, session, captures):
    observation = result['reader_exit_observation']
    if observation is None:
        return
    require(result['scenario'] == 'reader_nonzero' and request is not None and
            result['injection'] is not None and result['source_before'] is not None,
            'reader exit observation lacks prior authenticated injection')
    preparation.fields(observation, ('event', 'reader_pidfd_exited', 'helper', 'source', 'guard_index', 'ack_sent'),
                       'reader exit observation')
    event = observation['event']
    preparation.fields(event, ('version', 'op', 'scenario', 'binding', 'root_pid', 'preparer_pid', 'reader_pid',
                               'reader_starttime', 'exit'), 'reader fault exit event')
    injection = result['injection']
    live = [item for item in result['events'] if item['point'] == 'reader_before' and
            item['binding'] == injection['binding']]
    processes = result['processes']
    require(type(event['version']) is int and event['version'] == 1 and event['op'] == 'reader_fault_exit' and
            event['scenario'] == 'reader_nonzero' and len(live) == 1 and
            event['binding'] == injection['binding'] == live[0]['binding'] and
            (event['binding']['phase'], event['binding']['sequence']) == ('before', 1) and
            type(event['root_pid']) is int and event['root_pid'] == live[0]['root_pid'] == processes['root']['pid'] and
            type(event['preparer_pid']) is int and event['preparer_pid'] == live[0]['preparer_pid'] ==
            processes['preparer']['pid'] and type(event['reader_pid']) is int and
            event['reader_pid'] == live[0]['reader_pid'] == processes['reader-1']['pid'] and
            type(event['reader_starttime']) is int and
            event['reader_starttime'] == processes['reader-1']['starttime'], 'reader exit event identity differs')
    preparation.capture_binding(event['binding'], request, session, 'before')
    launch = {'binding': event['binding'], 'host_pid': event['reader_pid'], 'starttime': event['reader_starttime']}
    preparation.capture_exit(event['exit'], launch, {'binding': event['binding']}, request, session)
    exited = event['exit']
    require(exited['eof'] is True and exited['pidfd_exited'] is True and exited['terminal'] is False and
            exited['terminal_digest'] is None and type(exited['wait_status']) is int and
            exited['wait_status'] == 256 and type(exited['exit_code']) is int and exited['exit_code'] == 1,
            'reader exit status differs from the fault event')
    require(observation['reader_pidfd_exited'] is True and type(observation['ack_sent']) is bool and
            processes['reader-1']['reaped_by_watcher'] is False,
            'reader exit observation flags or direct wait differ')
    helper = observation['helper']
    preparation.fields(helper, ('pid', 'starttime', 'pidfd_alive_before', 'pidfd_alive_after'),
                       'reader exit helper observation')
    require(type(helper['pid']) is int and type(helper['starttime']) is int and
            helper['pid'] == request['helper']['pid'] and helper['starttime'] == request['helper']['starttime'] and
            all(helper[key] is None or type(helper[key]) is bool for key in
                ('pidfd_alive_before', 'pidfd_alive_after')), 'reader exit helper identity or flags differ')
    source = observation['source']
    if source is not None:
        require(helper['pidfd_alive_before'] is True, 'reader exit source lacks live helper before sampling')
        _fault_source_bundle(source)
    if helper['pidfd_alive_after'] is not None:
        require(source is not None and helper['pidfd_alive_before'] is True,
                'reader exit helper after check lacks source sample')
    index = observation['guard_index']
    if index is not None:
        guards = result['guard_checks']
        require(type(index) is int and index == len(result['events']) and index < len(guards) and
                source is not None and helper['pidfd_alive_after'] is True and
                all('reader-1' not in guards[index][key] for key in ('alive_before', 'alive_after')),
                'reader exit guard does not identify the post-exit competition')
    if captures:
        require(captures[0]['intent']['binding'] == event['binding'] and captures[0]['launch'] is not None and
                captures[0]['launch']['host_pid'] == event['reader_pid'] and
                captures[0]['launch']['starttime'] == event['reader_starttime'] and captures[0]['exit'] == exited,
                'reader exit event differs from supervisor capture')
    worker = result['worker_result']
    state = None if worker is None else worker['state']
    if state is not None:
        require(bool(captures) and any(item == {'scenario': 'reader_nonzero', 'point': 'reader_before',
                'binding': event['binding'], 'effect': 'reader_nonzero_frame'} for item in state['faults']) and
                state['expected_failure'] == 'reader_barrier_binding' and state['injected_frame'] is not None and
                state['injected_frame']['binding'] == {**event['binding'], 'nonce':
                    ('0' * 64 if event['binding']['nonce'] != '0' * 64 else '1' * 64)} and
                state['injected_frame']['binding'] != event['binding'],
                'reader exit event lacks the flushed injection')
        containment = state['containment']
        if containment is not None and containment['reader_pid'] == event['reader_pid']:
            require(containment['reader_wait_status'] == exited['wait_status'] and
                    containment['reader_signal_sent'] is False,
                    'reader containment discarded or repeated the saved exit')
    ordinal = event['binding']['ordinal']
    files = result['control_files']
    exit_name = 'capture-exit-%06d-1.json' % ordinal
    if exit_name in files:
        require(files[exit_name]['value']['exit'] == exited, 'durable reader exit differs from observation')
    launch_name = 'capture-launch-%06d-1.json' % ordinal
    if launch_name in files:
        durable = files[launch_name]['value']['launch']
        require(durable['host_pid'] == event['reader_pid'] and durable['starttime'] == event['reader_starttime'],
                'durable reader launch differs from observation')


def _fault_prepare_common(value, fixture, observed_request):
    request = preparation.validate_request(value['request'])
    require(fixture is not None and (observed_request is None or request == observed_request) and
            request['input']['case'] == 'I1' and request['input']['profile'] == 'A' and
            request['input']['metadata_pid'] == fixture['target_pid'] and
            request['input']['roots'][0]['source'] == fixture['source'] and
            request['daemon']['pid'] == fixture['daemon_pid'] and request['target'] is not None and
            request['target']['id'] == fixture['target_id'] and request['target']['pid'] == fixture['target_pid'] and
            request['target']['starttime'] == fixture['begin']['stat']['starttime'] and
            request['target']['destination'] == fixture['begin']['destination'],
            'fault preparation request differs from fixture')
    preparation._validate_observer_create_spec(value['create_spec'], request)
    waiting, namespaces = value['observer_waiting'], value['coordinator_namespaces']
    preparation.validate_request_namespaces(request, namespaces, waiting)
    state = value['helper_state']
    require(type(state) is dict and state.get('Running') is True and type(state.get('Pid')) is int and
            state['Pid'] == request['helper']['pid'] and type(state.get('StartedAt')) is str and
            bool(state['StartedAt']) and state['StartedAt'] != '0001-01-01T00:00:00Z',
            'fault waiting helper state differs')
    label = waiting['self_status']['apparmor']['label']
    suffix = ' (enforce)'
    require(type(label) is str and label.endswith(suffix) and label[:-len(suffix)] not in ('', 'unconfined'),
            'fault waiting observer AppArmor identity')
    preparation._observer_status(waiting['self_status'], label[:-len(suffix)], namespaces)
    return request


def _fault_retirement(value, helper_id):
    preparation.fields(value, ('id', 'remove', 'absent'), 'fault retirement')
    remove = value['remove']
    require(value['id'] == helper_id and value['absent'] is True and type(remove) is dict and
            remove.get('argv') == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'rm', helper_id] and
            type(remove.get('returncode')) is int and remove['returncode'] == 0 and
            type(remove.get('stdout')) is str and remove['stdout'].strip() == helper_id,
            'fault retirement removal differs')


def _fault_control_aux(result, observed_request, observed_session, captures):
    files, fixture, processes = result['control_files'], result['fixture'], result['processes']
    pending = receipt = evidence = lifecycle = None
    request = observed_request
    for name, entry in files.items():
        match = re.fullmatch(r'prepare-(pending|result)-([0-9]{6,})\.json', name)
        if not match:
            continue
        kind, ordinal = match[1], int(match[2])
        require(match[2] == '%06d' % ordinal, 'noncanonical fault preparation ordinal')
        value = entry['value']
        common = ('version', 'request', 'create_spec', 'observer_waiting', 'helper_state',
                  'coordinator_namespaces')
        extra = ('launch', 'exit', 'captures', 'pidfd_exited', 'result', 'raw_evidence', 'channel_error',
                 'test_fault') if kind == 'result' else ()
        preparation.fields(value, common + extra, 'fault preparation ' + kind)
        require(type(value['version']) is int and value['version'] == 1, 'fault preparation version')
        current = _fault_prepare_common(value, fixture, request)
        require(current['input']['ordinal'] == ordinal, 'fault preparation filename differs')
        request = current
        if kind == 'pending':
            require(pending is None, 'duplicate fault preparation pending receipt')
            pending = value
        else:
            require(receipt is None, 'duplicate fault preparation result receipt')
            receipt = value
    if request is not None:
        capture_session = observed_session
        for name, entry in files.items():
            if not name.startswith('capture-intent-'):
                continue
            intent = entry['value']
            candidate = intent['binding']['session_nonce']
            if capture_session is None:
                capture_session = candidate
            require(candidate == capture_session, 'fault durable capture session differs')
            preparation.capture_intent(intent, request, capture_session)
            sequence = intent['binding']['sequence']
            ordinal = request['input']['ordinal']
            for kind in ('launch', 'exit'):
                filename = 'capture-%s-%06d-%d.json' % (kind, ordinal, sequence)
                if filename not in files:
                    continue
                value = files[filename]['value'][kind]
                if kind == 'launch':
                    preparation.capture_launch(value, intent, request, capture_session,
                        root_pid=processes.get('root', {}).get('pid'),
                        preparer_pid=processes.get('preparer', {}).get('pid'))
                else:
                    launch_name = 'capture-launch-%06d-%d.json' % (ordinal, sequence)
                    require(launch_name in files, 'fault durable exit lacks launch')
                    preparation.capture_exit(value, files[launch_name]['value']['launch'], intent, request,
                                             capture_session)
    if receipt is not None:
        require(pending is not None and all(receipt[key] == pending[key] for key in
                ('version', 'request', 'create_spec', 'observer_waiting', 'helper_state', 'coordinator_namespaces')),
                'fault preparation result differs from pending receipt')
        launch, exited = receipt['launch'], receipt['exit']
        preparation._launch_exit(launch, exited, request, success=False)
        session = launch['session_nonce']
        require((observed_session is None or session == observed_session) and
                ('preparer' not in processes or launch['pid'] == processes['preparer']['pid']) and
                receipt['pidfd_exited'] is True and receipt['captures'] == exited['captures'] and
                (not captures or receipt['captures'] == captures), 'fault preparation launch or capture identity')
        if receipt['channel_error'] is not None:
            preparation.fields(receipt['channel_error'], ('type', 'message'), 'fault preparation channel error')
            require(type(receipt['channel_error']['type']) is str and 0 < len(receipt['channel_error']['type']) <= 128 and
                    type(receipt['channel_error']['message']) is str and
                    len(receipt['channel_error']['message']) <= 2048, 'fault preparation channel error invalid')
        require(receipt['test_fault'] is None, 'unexpected fault preparation test fault')
        envelope, raw = receipt['result'], receipt['raw_evidence']
        require((envelope is None) == (raw is None), 'fault result and raw evidence presence differs')
        if envelope is not None:
            evidence = preparation.validate_result(envelope, preparation.canonical_json(raw), request, launch['pid'],
                                                   session)
            require(all(envelope['preparer']['exe'][key] == launch['exe'][key] for key in ('dev', 'ino', 'mode')),
                    'fault preparer executable differs from launch')
            preparation.validate_reader_context(evidence, request, envelope['preparer'],
                receipt['coordinator_namespaces'], receipt['observer_waiting'], status=envelope['status'])
        preparation.validate_capture_ledger(receipt['captures'], request, session, evidence,
            success=envelope is not None and envelope['status'] == 'prepared', preparer_exe=launch['exe'],
            root_pid=processes.get('root', {}).get('pid'), coordinator_namespaces=receipt['coordinator_namespaces'],
            preparer_pid=launch['pid'])
        for row in receipt['captures']:
            sequence, ordinal = row['intent']['binding']['sequence'], request['input']['ordinal']
            for kind in ('intent', 'launch', 'exit'):
                name = 'capture-%s-%06d-%d.json' % (kind, ordinal, sequence)
                require(name in files and (files[name]['value'] if kind == 'intent' else
                        files[name]['value'][kind]) == row[kind],
                        'fault preparation receipt lacks matching durable capture')
        worker = result['worker_result']
        state = None if worker is None else worker['state']
        if state is not None:
            require(receipt['captures'] == captures, 'fault receipt and worker captures differ')
            if state['preparer_exits']:
                require(exited in state['preparer_exits'], 'fault preparation exit differs from supervisor')
    for name, entry in files.items():
        match = re.fullmatch(r'retired-([0-9a-f]{64})\.json', name)
        if match:
            helper_id = match[1]
            _fault_retirement(entry['value'], helper_id)
            if request is not None:
                require(helper_id in (request['helper']['id'], fixture['target_id']),
                        'fault retirement has an unrelated identity')
            continue
        match = re.fullmatch(r'helper-lifecycle-([0-9a-f]{64})\.json', name)
        if match:
            helper_id = match[1]
            require(lifecycle is None and fixture is not None and
                    (request is None or helper_id == request['helper']['id']),
                    'fault lifecycle helper identity differs')
            retired_name = 'retired-' + helper_id + '.json'
            require(retired_name in files, 'fault lifecycle lacks its earlier retirement receipt')
            retirement = files[retired_name]['value']
            wrapper = {'value': entry['value'], 'receipt': '/lab/control/' + name, 'sha256': entry['sha256']}
            lifecycle = preparation.validate_daemon_lifecycle(wrapper, helper_id,
                process=None if evidence is None else evidence['daemon'], retirement=retirement)
            require(lifecycle['pid'] == fixture['daemon_pid'] == fixture['bootstrap']['sender']['pid'],
                    'fault lifecycle daemon differs from bootstrap')
            continue
        match = re.fullmatch(r'ready-intent-([0-9]{6,})\.json', name)
        if match:
            ordinal = int(match[1])
            require(match[1] == '%06d' % ordinal and request is not None and receipt is not None and
                    receipt['result'] is not None and receipt['result']['status'] == 'prepared' and
                    ordinal == request['input']['ordinal'], 'fault ready intent lacks prepared result')
            value = entry['value']
            preparation.fields(value, ('helper_id', 'ready', 'preparation_receipt'), 'fault ready intent')
            ready = value['ready']
            preparation.fields(ready, ('version', 'nonce', 'input_digest', 'request_digest',
                                       'prepare_receipt_sha256', 'action'), 'fault ready')
            receipt_name = 'prepare-result-%06d.json' % ordinal
            require(value['helper_id'] == request['helper']['id'] and
                    value['preparation_receipt'] == '/lab/control/' + receipt_name and
                    type(ready['version']) is int and ready['version'] == 1 and
                    ready['nonce'] == request['input']['nonce'] and ready['input_digest'] == request['input_digest'] and
                    ready['request_digest'] == request['request_digest'] and
                    ready['prepare_receipt_sha256'] == files[receipt_name]['sha256'] and
                    ready['action'] == ('denycheck' if request['input']['deny'] else 'observe'),
                    'fault ready intent identity differs')
            continue
        require(name.startswith(('capture-', 'prepare-')) and
                (name.startswith('capture-') or re.fullmatch(r'prepare-(pending|result)-[0-9]{6,}\.json', name)),
                'unknown fault control receipt')
    return request, receipt, evidence, lifecycle


def _fault_partial_evidence(result):
    scenario, processes, fixture = result['scenario'], result['processes'], result['fixture']
    require(type(processes) is dict and
            set(processes) <= {'root', 'coordinator', 'preparer', 'reader-1', 'reader-2'} and
            (not processes or 'root' in processes), 'partial fault process inventory')
    seen_pids = set()
    for role, process in processes.items():
        preparation.fields(process, ('pid', 'starttime', 'parent_pid', 'pidfd_exited', 'wait_status',
                                     'reaped_by_watcher'), 'partial fault process')
        preparation.integer(process['pid'], 'fault process PID', 1, (1 << 31) - 1)
        preparation.integer(process['starttime'], 'fault process starttime', 1)
        preparation.integer(process['parent_pid'], 'fault process parent', 1, (1 << 31) - 1)
        require(process['pid'] not in seen_pids and type(process['pidfd_exited']) is bool and
                type(process['reaped_by_watcher']) is bool, 'partial fault process types or duplicate PID')
        seen_pids.add(process['pid'])
        if role != 'root':
            require(process['parent_pid'] == processes['root']['pid'], 'partial fault parent differs')
        if process['wait_status'] is not None:
            preparation.integer(process['wait_status'], 'partial fault wait status', 0, 0xffff)
        if process['reaped_by_watcher']:
            require(process['pidfd_exited'] is True and process['wait_status'] is not None and
                    (os.WIFEXITED(process['wait_status']) or os.WIFSIGNALED(process['wait_status'])),
                    'partial fault direct wait differs')
        else:
            require(process['wait_status'] is None, 'fault watcher wait status without direct reap')
    if not result['subreaper']:
        require(not processes and fixture is None and not result['events'] and result['worker_result'] is None,
                'unestablished subreaper has child facts')
    _fault_fixture(fixture, scenario, processes)
    events = result['events']
    require(type(events) is list and len(events) <= 10 and (fixture is not None or not events),
            'partial fault event inventory')
    request = session = None
    event_keys = set()
    for event in events:
        preparation.fields(event, ('version', 'op', 'point', 'scenario', 'binding', 'root_pid', 'preparer_pid',
                                   'reader_pid', 'request', 'helper', 'metadata_owner'), 'partial fault event')
        current = preparation.validate_request(event['request'])
        binding = event['binding']
        _fault_binding_shape(binding)
        if request is None:
            request, session = current, binding['session_nonce']
        require(current == request and binding['session_nonce'] == session and
                type(event['version']) is int and event['version'] == 1 and event['op'] == 'reader_fault_point' and
                event['scenario'] == scenario and event['point'] in
                ('fork_registered', 'before_restore', 'launch_registered', 'reader_before', 'reader_after'),
                'partial fault event identity differs')
        preparation.capture_intent({'version': 1, 'op': 'capture_intent', 'binding': binding,
                                    'helper': event['helper'], 'metadata_owner': event['metadata_owner']},
                                   request, session)
        role = 'reader-' + str(binding['sequence'])
        require('root' in processes and 'preparer' in processes and role in processes and
                all(type(event[key]) is int for key in ('root_pid', 'preparer_pid', 'reader_pid')) and
                event['root_pid'] == processes['root']['pid'] and
                event['preparer_pid'] == processes['preparer']['pid'] and
                event['reader_pid'] == processes[role]['pid'], 'partial fault event process differs')
        require(request['input']['case'] == 'I1' and request['input']['profile'] == 'A' and
                request['input']['metadata_pid'] == fixture['target_pid'] and
                request['daemon']['pid'] == fixture['daemon_pid'] and request['target'] is not None and
                request['target']['id'] == fixture['target_id'] and request['target']['pid'] == fixture['target_pid'] and
                request['target']['starttime'] == fixture['begin']['stat']['starttime'] and
                request['target']['destination'] == fixture['begin']['destination'] and
                request['input']['roots'][0]['source'] == fixture['source'], 'fault event differs from bootstrap')
        key = (binding['sequence'], event['point'])
        require(key not in event_keys, 'duplicate partial fault event')
        event_keys.add(key)
    injection = result['injection']
    if injection is not None:
        preparation.fields(injection, ('point', 'binding', 'scenario', 'signal', 'target', 'pidfd_bound'),
                           'partial fault injection')
        require(request is not None and injection['scenario'] == scenario and injection['pidfd_bound'] is True and
                (injection['signal'] is None or type(injection['signal']) is int) and
                injection['binding']['session_nonce'] == session and
                (injection['binding']['sequence'], injection['point']) in event_keys,
                'partial fault injection lacks observed event')
        preparation.capture_binding(injection['binding'], request, session)
        expected_phase = 'after' if scenario == 'after_capture_preparer_death' else 'before'
        expected_point = {'supervisor_fork_death': 'fork_registered', 'supervisor_launch_death': 'launch_registered',
                          'pidfd_open_failure': 'fork_registered', 'restore_failure': 'before_restore'}.get(
                              scenario, 'reader_before')
        require(injection['binding']['phase'] == expected_phase and injection['point'] == expected_point and
                ((scenario.startswith('supervisor_') and injection['signal'] == signal.SIGKILL and
                  injection['target'] == 'root') or
                 (scenario.endswith('preparer_death') and injection['signal'] == signal.SIGKILL and
                  injection['target'] == 'preparer') or
                 (not scenario.startswith('supervisor_') and not scenario.endswith('preparer_death') and
                  injection['signal'] is None and injection['target'] is None)), 'fault injection target differs')
    _fault_worker_state(result['worker_result'], scenario, request, session, processes)
    worker = result['worker_result']
    state = None if worker is None else worker['state']
    if state is not None and state['injected_frame'] is not None:
        require(scenario == 'reader_nonzero' and injection is not None and
                state['expected_failure'] == 'reader_barrier_binding' and
                state['injected_frame']['binding'] == {**injection['binding'], 'nonce':
                    ('0' * 64 if injection['binding']['nonce'] != '0' * 64 else '1' * 64)} and
                any(item == {'scenario': scenario, 'point': 'reader_before', 'binding': injection['binding'],
                             'effect': 'reader_nonzero_frame'} for item in state['faults']),
                'partial injected frame lacks the actual fault binding')
    if state is not None and state['expected_failure'] is not None:
        require(state['injected_frame'] is not None, 'partial expected failure lacks injected frame')
    guards = result['guard_checks']
    require(type(guards) is list and len(guards) <= 16, 'partial fault guard inventory')
    guard_identity = None
    for guard in guards:
        preparation.fields(guard, ('dev', 'ino', 'blocked', 'alive_before', 'alive_after'), 'partial fault guard')
        preparation.integer(guard['dev'], 'fault guard dev')
        preparation.integer(guard['ino'], 'fault guard ino', 1)
        require(type(guard['blocked']) is bool and all(type(guard[key]) is list and
                all(type(role) is str for role in guard[key]) and guard[key] == sorted(set(guard[key])) and
                set(guard[key]) <= set(processes) for key in ('alive_before', 'alive_after')),
                'partial fault guard state')
        identity = (guard['dev'], guard['ino'])
        require(guard_identity is None or identity == guard_identity, 'partial fault guard identity changed')
        guard_identity = identity
    for name in ('source_before', 'source_after'):
        source = result[name]
        if source is not None:
            require(fixture is not None, 'fault source lacks fixture')
            _fault_source_bundle(source)
    require(result['source_after'] is None or result['source_before'] is not None, 'fault source after lacks before')
    files = result['control_files']
    require(type(files) is dict, 'partial fault control receipts')
    for name, entry in files.items():
        require(type(name) is str and '/' not in name and name.endswith('.json') and
                name.startswith(('capture-', 'prepare-', 'ready-intent-', 'retired-', 'helper-lifecycle-')),
                'fault control filename')
        preparation.fields(entry, ('sha256', 'value'), 'fault control receipt')
        preparation.hex_string(entry['sha256'], 'fault control digest')
        require(hashlib.sha256(preparation.canonical_json(entry['value']) + b'\n').hexdigest() == entry['sha256'],
                'fault control bytes differ')
        if request is not None and name.startswith('capture-intent-'):
            intent = preparation.capture_intent(entry['value'], request, session)
            require(name == 'capture-intent-%06d-%d.json' % (request['input']['ordinal'],
                                                              intent['binding']['sequence']),
                    'fault control intent filename differs')
    worker = result['worker_result']
    state = None if worker is None else worker['state']
    captures = [] if state is None else state['captures']
    for name, entry in files.items():
        if not name.startswith('capture-'):
            continue
        match = re.fullmatch(r'capture-(intent|launch|exit|uncertain)-([0-9]{6,})-([12])\.json', name)
        require(match is not None, 'unknown fault capture receipt name')
        kind, ordinal, sequence = match[1], int(match[2]), int(match[3])
        require(match[2] == '%06d' % ordinal, 'noncanonical fault capture ordinal')
        intent_name = 'capture-intent-%06d-%d.json' % (ordinal, sequence)
        require(intent_name in files, 'fault capture record lacks durable intent')
        intent = files[intent_name]['value']
        preparation.fields(intent, ('version', 'op', 'binding', 'helper', 'metadata_owner'), 'fault durable intent')
        require(type(intent['version']) is int and intent['version'] == 1 and intent['op'] == 'capture_intent',
                'fault durable intent operation')
        _fault_binding_shape(intent['binding'])
        require(intent['binding']['ordinal'] == ordinal and intent['binding']['sequence'] == sequence,
                'fault durable intent filename binding')
        if request is not None:
            preparation.capture_intent(intent, request, session)
        else:
            _fault_unbound_capture({'intent': intent, 'launch': None, 'exit': None}, processes)
        row = captures[sequence - 1] if sequence <= len(captures) else None
        if row is not None:
            require(preparation.canonical_json(row['intent']) == preparation.canonical_json(intent),
                    'fault durable intent differs from worker state')
        if kind in ('launch', 'exit'):
            value = entry['value']
            preparation.fields(value, ('version', 'op', 'binding', kind), 'fault durable capture control')
            require(type(value['version']) is int and value['version'] == 1 and value['op'] == 'capture_' + kind and
                    value['binding'] == intent['binding'], 'fault durable capture control binding')
            if row is not None:
                require(preparation.canonical_json(value[kind]) == preparation.canonical_json(row[kind]),
                        'fault durable capture control differs from worker state')
            if kind == 'exit':
                launch_name = 'capture-launch-%06d-%d.json' % (ordinal, sequence)
                require(launch_name in files, 'fault durable exit lacks launch')
            if request is not None:
                if kind == 'launch':
                    preparation.capture_launch(value[kind], intent, request, session,
                                               preparer_pid=processes.get('preparer', {}).get('pid'))
                else:
                    preparation.capture_exit(value[kind], files[launch_name]['value']['launch'], intent, request, session)
            else:
                launch_value = value[kind] if kind == 'launch' else files[launch_name]['value']['launch']
                _fault_unbound_capture({'intent': intent, 'launch': launch_value,
                                        'exit': value[kind] if kind == 'exit' else None}, processes)
        elif kind == 'uncertain':
            value = entry['value']
            preparation.fields(value, ('version', 'capture', 'launch_outcome', 'error'), 'fault uncertain capture')
            require(type(value['version']) is int and value['version'] == 1 and
                    value['launch_outcome'] in ('unresolved', 'exit_unresolved'), 'fault uncertainty outcome')
            require('capture-exit-%06d-%d.json' % (ordinal, sequence) not in files,
                    'fault durable exit contradicts unresolved capture')
            capture = value['capture']
            preparation.fields(capture, ('intent', 'intent_sha256', 'launch', 'exit'), 'fault uncertain capture row')
            require(preparation.canonical_json(capture['intent']) == preparation.canonical_json(intent) and
                    capture['intent_sha256'] == hashlib.sha256(preparation.canonical_json(intent)).hexdigest() and
                    capture['exit'] is None and
                    ((value['launch_outcome'] == 'unresolved' and capture['launch'] is None) or
                     (value['launch_outcome'] == 'exit_unresolved' and capture['launch'] is not None)),
                    'fault uncertainty capture differs')
            preparation.fields(value['error'], ('type', 'message'), 'fault uncertainty error')
            require(type(value['error']['type']) is str and 0 < len(value['error']['type']) <= 128 and
                    type(value['error']['message']) is str and len(value['error']['message']) <= 2048,
                    'fault uncertainty error invalid')
            if capture['launch'] is not None:
                if request is not None:
                    preparation.capture_launch(capture['launch'], intent, request, session,
                                               preparer_pid=processes.get('preparer', {}).get('pid'))
                else:
                    _fault_unbound_capture(capture, processes)
            if row is not None and row['launch'] is not None and capture['launch'] is not None:
                require(preparation.canonical_json(capture['launch']) == preparation.canonical_json(row['launch']),
                        'fault uncertainty launch differs from worker state')
    _fault_exit_observation(result, request, session, captures)
    control_request, receipt, evidence, lifecycle = _fault_control_aux(result, request, session, captures)
    if control_request is not None:
        require(not ({control_request['helper']['pid'], control_request['daemon']['pid'],
                      control_request['target']['pid']} & seen_pids) and
                control_request['helper']['id'] != control_request['target']['id'],
                'fault custody processes or helper identity overlap')
    return request, session, receipt, evidence, lifecycle


def _fault_nonzero_complete(result, request, receipt, evidence, lifecycle):
    observation = result['reader_exit_observation']
    require(observation is not None and observation['source'] is not None and
            observation['helper']['pidfd_alive_before'] is True and
            observation['helper']['pidfd_alive_after'] is True and observation['ack_sent'] is True,
            'reader nonzero exit observation incomplete')
    require(b'prepare_protocol: reader barrier binding\n' in
            base64.b64decode(observation['event']['exit']['diagnostic']),
            'reader diagnostic does not prove the injected binding failure')
    before, after = result['source_before'], result['source_after']
    require(before == observation['source'] and type(after) is dict and
            all(after[key] == before[key] for key in
                ('source', 'sentinel', 'actual', 'actual_sentinel', 'target_mountinfo')),
            'reader nonzero source changed across observation or retirement')
    guards, index = result['guard_checks'], observation['guard_index']
    require(type(index) is int and index + 1 < len(guards) and
            any('reader-1' in guard['alive_before'] and 'reader-1' in guard['alive_after']
                for guard in guards[:index]), 'reader exit guard chronology missing')
    guard = guards[index]
    require(guard['blocked'] is True and all({'root', 'coordinator', 'preparer'} <= set(guard[key]) and
            'reader-1' not in guard[key] for key in ('alive_before', 'alive_after')),
            'reader exit guard responsibility incomplete')
    files = result['control_files']
    ordinal = request['input']['ordinal']
    names = {kind: 'capture-%s-%06d-1.json' % (kind, ordinal) for kind in ('intent', 'launch', 'exit')}
    require(all(name in files for name in names.values()) and
            not any(re.fullmatch(r'capture-(?:intent|launch|exit|uncertain)-[0-9]{6,}-2\.json', name) or
                    name.startswith('capture-uncertain-') for name in files),
            'reader nonzero durable capture chain incomplete or extended')
    state = result['worker_result']['state']
    require(len(state['captures']) == 1 and len(state['preparer_exits']) == 1, 'reader nonzero capture count')
    record = state['captures'][0]
    require(files[names['intent']]['value'] == record['intent'] and
            files[names['launch']]['value']['launch'] == record['launch'] and
            files[names['exit']]['value']['exit'] == record['exit'] == observation['event']['exit'],
            'reader nonzero durable capture differs from exit observation')
    require(receipt is not None and evidence is not None and
            'prepare-result-%06d.json' % ordinal in files and
            'prepare-pending-%06d.json' % ordinal in files and receipt['captures'] == [record] and
            receipt['exit'] == state['preparer_exits'][0] and receipt['exit']['wait_status'] == 0 and
            type(receipt['exit']['exit_code']) is int and receipt['exit']['exit_code'] == 0 and
            receipt['pidfd_exited'] is True and receipt['channel_error'] is None and receipt['test_fault'] is None and
            receipt['result']['status'] == 'rejected' and receipt['result']['reason'] == 'prepare_exit_uncertain' and
            receipt['result']['sources'] == [] and receipt['result']['attached'] == [] and
            evidence['observer_view'] == {'before': None, 'after': None},
            'reader nonzero rejected preparation receipt incomplete')
    for role, graph in (('daemon', before['daemon_mountinfo']), ('target', before['target_mountinfo'])):
        process = evidence[role]
        require(process is not None and process['before_mountinfo'] == process['after_mountinfo'] == graph,
                'reader nonzero preparer source evidence differs')
    helper_id = request['helper']['id']
    retired_name, lifecycle_name = 'retired-' + helper_id + '.json', 'helper-lifecycle-' + helper_id + '.json'
    require(retired_name in files and lifecycle_name in files and lifecycle is not None and
            lifecycle['retirement'] == files[retired_name]['value'] and
            lifecycle['after_mountinfo'] == after['daemon_mountinfo'],
            'reader nonzero retirement or daemon lifecycle incomplete')


def validate_reader_fault(result, case):
    preparation.fields(result, ('version', 'experiment_kind', 'run_token', 'case', 'scenario', 'outcome',
                       'uncertain', 'completed', 'installation', 'subreaper', 'fixture', 'events', 'injection',
                       'guard_checks', 'source_before', 'source_after', 'reader_exit_observation', 'control_files',
                       'worker_result',
                       'processes', 'error', 'failure_stage', 'cleanup_errors', 'mount_namespace_identity',
                       'all_overlay_layers', 'product_gate'),
                       'reader fault result')
    require(type(result['version']) is int and result['version'] == 2 and
            result['experiment_kind'] == 'reader-fault-v2' and result['run_token'] == TOKEN and
            case in READER_FAULT_CASES and result['case'] == case and
            result['scenario'] == case.removeprefix('reader-fault-') and result['uncertain'] is True and
            result['completed'] is False and type(result['subreaper']) is bool and
            result['outcome'] in ('EXPECTED_UNCERTAIN', 'FAIL') and
            all(result[key] == value for key, value in EXPERIMENT_BOUNDARY.items()), 'reader fault result binding')
    validate_guest_install({'preparer_install': result['installation']})
    errors = result['cleanup_errors']
    require(type(errors) is list and len(errors) <= 32 and
            all(type(error) is str and 0 < len(error) <= 2048 for error in errors), 'fault cleanup errors invalid')
    _, _, receipt, evidence, lifecycle = _fault_partial_evidence(result)
    if result['outcome'] == 'FAIL':
        require(type(result['error']) is str and 0 < len(result['error']) <= 2048 and
                result['failure_stage'] in ('setup', 'before_injection', 'after_injection', 'watcher', 'cleanup'),
                'failed reader fault lacks bounded diagnostic or stage')
        if result['failure_stage'] in ('setup', 'before_injection'):
            require(result['injection'] is None, 'pre-injection failure contains injection')
        if result['failure_stage'] == 'after_injection':
            require(result['injection'] is not None, 'post-injection failure lacks injection')
        worker = result['worker_result']
        if worker is not None and result['injection'] is None and result['failure_stage'] == 'before_injection':
            require(result['error'] == worker['error'], 'early worker error was replaced')
        return result
    require(result['subreaper'] is True and result['error'] is None and result['failure_stage'] is None and
            not errors and result['events'], 'reader fault lacks clean actual events')
    scenario = result['scenario']
    processes = result['processes']
    require(type(processes) is dict and {'root', 'coordinator', 'preparer', 'reader-1'} <= set(processes) and
            set(processes) <= {'root', 'coordinator', 'preparer', 'reader-1', 'reader-2'}, 'fault process inventory')
    for role, value in processes.items():
        preparation.fields(value, ('pid', 'starttime', 'parent_pid', 'pidfd_exited', 'wait_status',
                           'reaped_by_watcher'), 'fault process')
        preparation.integer(value['pid'], 'fault process PID', 1, (1 << 31) - 1)
        preparation.integer(value['starttime'], 'fault process starttime', 1)
        require(value['pidfd_exited'] is True and type(value['reaped_by_watcher']) is bool,
                'fault process exit unobserved')
        if role != 'root':
            require(value['parent_pid'] == processes['root']['pid'], 'fault child parent binding')
        if value['reaped_by_watcher']:
            preparation.integer(value['wait_status'], 'fault wait status', 0, (1 << 31) - 1)
            require(os.WIFEXITED(value['wait_status']) or os.WIFSIGNALED(value['wait_status']),
                    'fault wait status is not terminal')
    require(processes['root']['reaped_by_watcher'], 'root direct wait missing')
    fixture = result['fixture']
    require(type(fixture) is dict and fixture.get('coordinator_pid') == processes['coordinator']['pid'] and
            fixture.get('source') == '/lab/ext/reader-fault-source' and fixture.get('run_token') == TOKEN,
            'fault fixture process binding')
    first = result['events'][0]
    request = preparation.validate_request(first['request'])
    session_nonce = first['binding']['session_nonce']
    event_keys = set()
    for event in result['events']:
        preparation.fields(event, ('version', 'op', 'point', 'scenario', 'binding', 'root_pid', 'preparer_pid',
                           'reader_pid', 'request', 'helper', 'metadata_owner'), 'fault event')
        require(type(event['version']) is int and event['version'] == 1 and event['op'] == 'reader_fault_point' and
                event['scenario'] == scenario and event['request'] == request and
                event['root_pid'] == processes['root']['pid'] and
                event['preparer_pid'] == processes['preparer']['pid'], 'fault event process/request binding')
        preparation.capture_binding(event['binding'], request, session_nonce)
        sequence = event['binding']['sequence']
        require(event['reader_pid'] == processes['reader-' + str(sequence)]['pid'], 'fault reader identity')
        key = (sequence, event['point'])
        require(key not in event_keys and event['point'] in ('fork_registered', 'before_restore',
                'launch_registered', 'reader_before', 'reader_after'), 'duplicate or unknown fault point')
        event_keys.add(key)
        preparation.capture_intent({'version': 1, 'op': 'capture_intent', 'binding': event['binding'],
                                   'helper': event['helper'], 'metadata_owner': event['metadata_owner']},
                                  request, session_nonce)
    injection = result['injection']
    preparation.fields(injection, ('point', 'binding', 'scenario', 'signal', 'target', 'pidfd_bound'),
                       'fault injection')
    require(injection['scenario'] == scenario and injection['pidfd_bound'] is True and
            (injection['binding']['sequence'], injection['point']) in event_keys,
            'fault injection not bound to observed cutpoint')
    expected_phase = 'after' if scenario == 'after_capture_preparer_death' else 'before'
    expected_point = {'supervisor_fork_death': 'fork_registered', 'supervisor_launch_death': 'launch_registered',
                      'pidfd_open_failure': 'fork_registered', 'restore_failure': 'before_restore'}.get(
                          scenario, 'reader_before')
    require(injection['binding']['phase'] == expected_phase and injection['point'] == expected_point,
            'fault injected at wrong phase')
    before, after = result['source_before'], result['source_after']
    preparation.fields(before, ('source', 'sentinel', 'actual', 'actual_sentinel', 'daemon_mountinfo',
                       'target_mountinfo'), 'fault source')
    require((scenario == 'reader_nonzero' or before == after) and before['source'] == before['actual'] and
            before['sentinel'] == before['actual_sentinel'], 'fault source drift or wrong target')
    _object(before['source'], 'fault source')
    _object(before['actual'], 'fault target source')
    _sentinel(before['sentinel'])
    _sentinel(before['actual_sentinel'])
    import handoff
    handoff.custody_graph(before['daemon_mountinfo'])
    handoff.custody_graph(before['target_mountinfo'])
    require(type(result['guard_checks']) is list and len(result['guard_checks']) >= 2,
            'fault guard responsibility missing')
    guard_id = None
    for guard in result['guard_checks']:
        preparation.fields(guard, ('dev', 'ino', 'blocked', 'alive_before', 'alive_after'), 'fault guard check')
        identity = (guard['dev'], guard['ino'])
        require(guard_id is None or identity == guard_id, 'fault guard inode changed')
        guard_id = identity
        require(type(guard['blocked']) is bool and set(guard['alive_before']) <= set(processes) and
                set(guard['alive_after']) <= set(processes), 'fault guard live responsibility invalid')
        if set(guard['alive_before']) & set(guard['alive_after']):
            require(guard['blocked'], 'live fault responsibility released guard')
    require(result['guard_checks'][0]['blocked'], 'initial competing guard acquired')
    files = result['control_files']
    require(type(files) is dict and not any(name.startswith('ready-intent-') for name in files),
            'fault issued ready')
    intents = {}
    for name, item in files.items():
        preparation.fields(item, ('sha256', 'value'), 'fault control receipt')
        preparation.hex_string(item['sha256'], 'fault control receipt digest')
        require(hashlib.sha256(preparation.canonical_json(item['value']) + b'\n').hexdigest() == item['sha256'],
                'fault durable receipt bytes differ')
        if name.startswith('capture-intent-'):
            value = preparation.capture_intent(item['value'], request, session_nonce)
            intents[value['binding']['sequence']] = value
    sequence = injection['binding']['sequence']
    require(sequence in intents and intents[sequence]['binding'] == injection['binding'],
            'fault lost persisted intent')
    if scenario.startswith('supervisor_'):
        require(injection['signal'] == signal.SIGKILL and injection['target'] == 'root' and
                os.WIFSIGNALED(processes['root']['wait_status']) and
                os.WTERMSIG(processes['root']['wait_status']) == signal.SIGKILL,
                'supervisor death not observed')
        require(all(value['reaped_by_watcher'] for role, value in processes.items() if role.startswith('reader-')),
                'orphan reader direct subreaper wait missing')
        uncertain = [item['value'] for name, item in files.items() if name.startswith('capture-uncertain-')]
        require(any(value.get('capture', {}).get('intent') == intents[sequence] and
                    value.get('launch_outcome') in ('unresolved', 'exit_unresolved') for value in uncertain),
                'supervisor death lost unresolved intent responsibility')
    else:
        worker = result['worker_result']
        require(type(worker) is dict and worker.get('scenario') == scenario and worker.get('error') and
                type(worker.get('state')) is dict, 'fault lacks actual supervisor outcome')
        state = worker['state']
        preparation.fields(state, ('preparer', 'reader', 'captures', 'uncertain', 'faults', 'containment',
                                   'preparer_exits', 'injected_frame', 'expected_failure'), 'fault supervisor state')
        require(type(state['captures']) is list and len(state['captures']) <= 2 and
                type(state['faults']) is list and type(state['uncertain']) is bool, 'fault supervisor state types')
        require(type(state['preparer_exits']) is list and len(state['preparer_exits']) <= 1,
                'fault preparer exit inventory')
        for exit_record in state['preparer_exits']:
            preparation.fields(exit_record, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce',
                               'pid', 'wait_status', 'exit_code', 'capture', 'captures'), 'fault preparer exit')
            require(type(exit_record['version']) is int and exit_record['version'] == 1 and
                    exit_record['op'] == 'preparer_exit' and exit_record['run_token'] == TOKEN and
                    exit_record['session_nonce'] == session_nonce and
                    exit_record['ordinal'] == request['input']['ordinal'] and
                    exit_record['nonce'] == request['input']['nonce'] and
                    exit_record['pid'] == processes['preparer']['pid'] and
                    exit_record['captures'] == state['captures'], 'fault preparer exit binding')
            preparation.integer(exit_record['wait_status'], 'fault preparer wait status', 0, (1 << 31) - 1)
            status = exit_record['wait_status']
            require((os.WIFEXITED(status) and exit_record['exit_code'] == os.WEXITSTATUS(status)) or
                    (os.WIFSIGNALED(status) and exit_record['exit_code'] is None), 'fault preparer wait mismatch')
        if scenario.endswith('preparer_death'):
            require(injection['signal'] == signal.SIGKILL and injection['target'] == 'preparer',
                    'preparer death injection mismatch')
            death_status = processes['preparer']['wait_status']
            if death_status is None and state['preparer_exits']:
                death_status = state['preparer_exits'][0]['wait_status']
            if death_status is None and state['containment'] is not None:
                death_status = state['containment'].get('preparer_wait_status')
            require(type(death_status) is int and os.WIFSIGNALED(death_status) and
                    os.WTERMSIG(death_status) == signal.SIGKILL, 'preparer SIGKILL wait evidence missing')
        else:
            effect = 'reader_nonzero_frame' if scenario == 'reader_nonzero' else scenario
            require(any(item == {'scenario': scenario, 'point': expected_point, 'binding': injection['binding'],
                                  'effect': effect} for item in state.get('faults', [])),
                    'fault effect was not actually performed')
        for record in state.get('captures', []):
            intent = preparation.capture_intent(record['intent'], request, session_nonce)
            require(intent['binding']['sequence'] in intents, 'root capture lacks independent intent')
            if record['launch'] is not None:
                preparation.capture_launch(record['launch'], intent, request, session_nonce,
                                           root_pid=processes['root']['pid'], preparer_pid=processes['preparer']['pid'])
                if record['exit'] is not None:
                    preparation.capture_exit(record['exit'], record['launch'], intent, request, session_nonce)
        if scenario == 'reader_nonzero':
            require(any(record.get('exit') is not None and record['exit']['exit_code'] not in (None, 0)
                        for record in state.get('captures', [])), 'reader nonzero exit was not observed')
            frame = state['injected_frame']
            preparation.fields(frame, ('version', 'op', 'binding'), 'injected reader barrier frame')
            require(frame['version'] == 1 and frame['op'] == 'reader_allow_read' and
                    frame['binding'] != injection['binding'] and
                    state['expected_failure'] == 'reader_barrier_binding', 'reader nonzero trigger evidence')
            _fault_nonzero_complete(result, request, receipt, evidence, lifecycle)
        else:
            require(state['injected_frame'] is None and state['expected_failure'] is None,
                    'unexpected reader malformed-frame injection')
        containment = state['containment']
        for role, process in processes.items():
            if process['reaped_by_watcher']:
                continue
            if role.startswith('reader-'):
                recorded = any(record.get('exit') is not None and
                               record['exit']['host_pid'] == process['pid'] and
                               record['exit']['starttime'] == process['starttime'] and
                               record['exit']['pidfd_exited'] is True for record in state['captures'])
                prefix = 'reader'
            elif role == 'preparer':
                recorded = bool(state['preparer_exits'])
                prefix = 'preparer'
            else:
                raise Failure('fault process lacks direct wait evidence: ' + role)
            if not recorded:
                require(type(containment) is dict and containment.get(prefix + '_pid') == process['pid'] and
                        type(containment.get(prefix + '_wait_status')) is int and
                        (os.WIFEXITED(containment[prefix + '_wait_status']) or
                         os.WIFSIGNALED(containment[prefix + '_wait_status'])),
                        'fault child lacks supervisor or watcher direct wait: ' + role)
        if scenario == 'reader_timeout':
            require(type(containment) is dict and containment.get('reader_signal_sent') is True and
                    containment.get('reader_pidfd_held') is True and
                    containment.get('reader_pidfd_exited') is True and
                    type(containment.get('reader_wait_status')) is int and
                    os.WIFSIGNALED(containment['reader_wait_status']), 'reader timeout containment unproved')
    return result


def _protocol_prefix(data, case, complete):
    """Validate the same ordered frames for live observation and final parsing."""
    require(type(data) is bytes and case in VM_CASES and len(data) <= PROTOCOL_LIMIT,
            'invalid protocol bytes, case, or size')
    parts = data.split(b'\n')
    partial = parts.pop()
    require(len(partial) < 8192 and (not complete or not partial), 'unterminated or oversized protocol line')
    ready, chunks, total, end = False, [], None, None
    installation = transport = None
    payload_size = 0
    for line in parts:
        require(len(line) + 1 <= 8192 and bool(line) and end is None and
                not (case == 'abort' and ready), 'empty, oversized, or trailing protocol line')
        if line == ('G2LAB-READY ' + TOKEN).encode():
            require(installation is not None and not ready and not chunks, 'unexpected or out-of-order READY')
            ready = True
            continue
        match = re.fullmatch(rb'G2LAB-INSTALL ([0-9a-f]{32}) ([0-9a-f]{64}) ([A-Za-z0-9+/]*={0,2})', line)
        if match:
            require(installation is None and not ready and match[1].decode() == TOKEN,
                    'unexpected or out-of-order INSTALL')
            try:
                raw = base64.b64decode(match[3], validate=True)
            except binascii.Error as exc:
                raise Failure('invalid installation base64') from exc
            require(len(raw) <= 4096 and base64.b64encode(raw) == match[3] and
                    hashlib.sha256(raw).hexdigest() == match[2].decode(),
                    'installation payload, base64, or checksum mismatch')
            try:
                envelope = preparation.strict_json(raw)
                require(preparation.canonical_json(envelope) == raw, 'noncanonical installation JSON')
                preparation.fields(envelope, ('version', 'run_token', 'case', 'installation', 'transport'),
                                   'installation envelope')
                require(type(envelope['version']) is int and envelope['version'] == 2 and
                        envelope['run_token'] == TOKEN and envelope['case'] == case,
                        'installation envelope identity mismatch')
                installation = validate_guest_install({'preparer_install': envelope['installation']})
                transport = validate_transport(envelope['transport'])
            except (KeyError, TypeError, ValueError) as exc:
                raise Failure('invalid installation evidence: ' + str(exc)) from exc
            continue
        match = re.fullmatch(rb'G2LAB-DATA ([0-9a-f]{32}) ([0-9]+)/([0-9]+) ([A-Za-z0-9+/]*={0,2})', line)
        if match:
            require(case != 'abort' and ready and match[1].decode() == TOKEN, 'unexpected DATA')
            index, count = int(match[2]), int(match[3])
            require(0 < count <= 65536 and index < count and (total is None or total == count) and
                    index == len(chunks) and str(index).encode() == match[2] and
                    str(count).encode() == match[3], 'duplicate or inconsistent DATA')
            try:
                raw = base64.b64decode(match[4], validate=True)
            except binascii.Error as exc:
                raise Failure('invalid base64') from exc
            require(raw and base64.b64encode(raw) == match[4], 'empty or noncanonical DATA')
            chunks.append(raw)
            payload_size += len(raw)
            require(payload_size <= 4 * 1024 * 1024, 'JSON limit exceeded')
            total = count
            continue
        match = re.fullmatch(rb'G2LAB-END ([0-9a-f]{32}) ([0-9a-f]{64})', line)
        if match:
            require(case != 'abort' and ready and total is not None and len(chunks) == total and
                    match[1].decode() == TOKEN, 'unexpected or out-of-order END')
            end = match[2].decode()
            continue
        raise Failure('malformed protocol line')
    if partial and (end is not None or (case == 'abort' and ready)):
        raise Failure('trailing protocol bytes')
    if complete:
        require(installation is not None and transport is not None and ready, 'missing INSTALL or READY')
        require((case == 'abort' and not chunks and end is None) or
                (case != 'abort' and end is not None and total is not None and len(chunks) == total),
                'missing result chunks or END, or unexpected abort result')
    return {'installation': installation, 'transport': transport, 'ready': ready, 'chunks': chunks, 'end': end}


def parse_frames(data, case):
    prefix = _protocol_prefix(data, case, complete=True)
    bundle = {'installation': prefix['installation'], 'transport': prefix['transport'], 'result': None}
    if case == 'abort':
        return bundle
    payload = b''.join(prefix['chunks'])
    end = prefix['end']
    require(hashlib.sha256(payload).hexdigest() == end, 'result checksum mismatch')
    def pairs(items):
        obj = {}
        for key, value in items:
            require(key not in obj, 'duplicate result key')
            obj[key] = value
        return obj
    try:
        result = json.loads(payload.decode('utf-8'), object_pairs_hook=pairs,
                            parse_constant=lambda _: (_ for _ in ()).throw(ValueError('invalid JSON constant')))
    except (ValueError, UnicodeError) as exc:
        raise Failure('invalid result JSON: ' + str(exc)) from exc
    if case in READER_FAULT_CASES:
        validate_reader_fault(result, case)
        require(preparation.canonical_json(result['installation']) == preparation.canonical_json(bundle['installation']),
                'fault installation differs from INSTALL')
        bundle['result'] = result
        return bundle
    require(type(result) is dict and set(result) == {'experiment_kind', 'run_token', 'completed', 'uncertain',
            'preparation_version', 'preparation_checks', 'versions', 'isolation', 'cases',
            'mount_namespace_identity', 'all_overlay_layers', 'product_gate'} and
            result['experiment_kind'] == 'input-handoff-v1' and result['run_token'] == TOKEN and
            type(result['preparation_version']) is int and result['preparation_version'] == 1 and
            type(result['completed']) is bool and type(result['uncertain']) is bool and
            result['mount_namespace_identity'] == 'UNPROVEN' and result['all_overlay_layers'] == 'UNPROVEN' and
            result['product_gate'] == 'NOT_READY' and type(result['versions']) is dict and
            type(result['isolation']) is dict and type(result['cases']) is dict and
            set(result['cases']) == set(EXPERIMENT_CASES), 'invalid result root schema')
    recorded_install = result['isolation'].get('preparer_install')
    require((recorded_install is None and not result['completed']) or
            preparation.canonical_json(recorded_install) == preparation.canonical_json(bundle['installation']),
            'matrix installation differs from INSTALL')
    used = {'ids': set(), 'ordinals': set(), 'nonces': set(), 'retired_ids': set()}
    for case_name, profiles in result['cases'].items():
        require(isinstance(profiles, dict) and set(profiles) == {'A', 'B'}, 'invalid profile set')
        for profile, item in profiles.items():
            require(isinstance(item, dict) and set(item) == {'status', 'observations'} and
                    item['status'] in ('PASS', 'FAIL', 'INCONCLUSIVE', 'NOT_RUN') and
                    isinstance(item['observations'], dict), 'invalid observation schema')
            require(all(item['observations'].get(key) == value for key, value in EXPERIMENT_BOUNDARY.items()),
                    'missing local capability boundaries')
            if item['status'] == 'PASS':
                required = set(REQUIRED_EVIDENCE[case_name])
                if case_name == 'C0' and profile == 'B':
                    required.add('seccomp_denials')
                evidence = item['observations'].get('required_evidence')
                require(isinstance(evidence, dict) and set(evidence) == required and
                        all(value is True for value in evidence.values()), 'missing or invalid required evidence')
                dependency = 'C0' if case_name in ('V1', 'V2', 'V3', 'I0') else (
                    'I0' if case_name in ('I1', 'I2', 'I3') else None)
                require(dependency is None or result['cases'][dependency][profile]['status'] == 'PASS',
                        'observation passed without its dependency')
                validate_case_preparation(result, case_name, profile, item['observations'], used)
                validate_case_detail(result, case_name, profile, item['observations'])
                observed = item['observations']
                if case_name == 'V1':
                    used['retired_ids'].update(value['id'] for value in observed['consumers_before'])
                elif case_name in ('I1', 'I2'):
                    used['retired_ids'].add(observed['consumer_before']['id'])
                elif case_name == 'I3':
                    used['retired_ids'].update(observed[phase]['consumer_before']['id']
                                               for phase in ('before_create', 'after_helper'))
    validate_preparation_checks(result, used)
    require(not (result['uncertain'] and result['completed']), 'inconsistent completion or ownership state')
    if result['completed']:
        validate_coordinator_retirement(result, used)
    bundle['result'] = result
    return bundle


def proc_starttime(pid):
    text = Path('/proc/%d/stat' % pid).read_text(encoding='ascii')
    return int(text[text.rfind(')') + 2:].split()[19])


def qemu_limits():
    os.nice(10)
    resource.setrlimit(resource.RLIMIT_AS, (8 * 1024**3, 8 * 1024**3))


def vm_resource_gate():
    fs = os.statvfs(RUN)
    disk_available = fs.f_bavail * fs.f_frsize
    meminfo = Path('/proc/meminfo').read_text(encoding='ascii')
    matches = re.findall(r'^MemAvailable:\s+([0-9]+)\s+kB$', meminfo, re.M)
    require(len(matches) == 1, 'MemAvailable unavailable')
    mem_available = int(matches[0]) * 1024
    require(disk_available >= 64 * 1024**3 and mem_available >= 4 * 1024**3,
            'insufficient disk or available memory')
    fd = os.open('/dev/kvm', os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        require(stat.S_ISCHR(os.fstat(fd).st_mode), '/dev/kvm is not a character device')
        api = fcntl.ioctl(fd, 0xAE00, 0)
        require(api == 12, 'KVM API unavailable or unexpected')
    finally:
        os.close(fd)
    return {'disk_available_bytes': disk_available, 'mem_available_bytes': mem_available, 'kvm_api_version': api}


def preflight_pidfd():
    require(callable(getattr(os, 'pidfd_open', None)) and
            callable(getattr(signal, 'pidfd_send_signal', None)), 'pidfd support unavailable')
    fd = os.pidfd_open(os.getpid(), 0)
    try:
        signal.pidfd_send_signal(fd, 0)
    finally:
        os.close(fd)


class StderrCollector:
    """Drain a pipe continuously, retaining only the first MiB."""
    LIMIT = 1024 * 1024

    def __init__(self, stream):
        self.stream = stream
        self.prefix = bytearray()
        self.overflow = threading.Event()
        self.stop = threading.Event()
        self.error = None
        os.set_blocking(stream.fileno(), False)
        self.thread = threading.Thread(target=self._drain, daemon=True)
        self.thread.start()

    def _drain(self):
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(self.stream, selectors.EVENT_READ)
                while not self.stop.is_set():
                    if not selector.select(0.2):
                        continue
                    try:
                        block = os.read(self.stream.fileno(), 8192)
                    except BlockingIOError:
                        continue
                    if not block:
                        return
                    remaining = self.LIMIT - len(self.prefix)
                    self.prefix.extend(block[:remaining])
                    if len(block) > remaining:
                        self.overflow.set()
        except Exception as exc:
            self.error = exc

    def check(self):
        require(not self.overflow.is_set(), 'QEMU stderr limit exceeded')
        require(self.error is None, 'QEMU stderr collection failed: ' + str(self.error))

    def finish(self):
        # Give a closed child pipe time to drain, but never wait on a live child.
        self.thread.join(1)
        if self.thread.is_alive():
            self.stop.set()
            self.thread.join(1)
        require(not self.thread.is_alive(), 'QEMU stderr collector did not stop')
        self.stream.close()
        return bytes(self.prefix)


class QmpCommandError(Failure):
    def __init__(self, name, request_id, error):
        self.name = name
        self.request_id = request_id
        self.error_class = error['class']
        self.desc = error['desc']
        super().__init__('%s (%s): %s: %s' %
                         (name, request_id, self.error_class, self.desc))


class Qmp:
    def __init__(self, proc, log):
        self.proc, self.log, self.buffer, self.bytes = proc, log, b'', 0
        self.sent = set()
        self.requests = {}
        self.last_request = None
        self.next_id = 1
        self.capabilities_id = None
        self.greeted = self.negotiated = self.write_broken = False
        self.eof = False
        self.check_stderr = None
        self.snapshots = []
        self.selector = selectors.DefaultSelector()
        try:
            os.set_blocking(proc.stdin.fileno(), False)
            os.set_blocking(proc.stdout.fileno(), False)
            self.selector.register(proc.stdout, selectors.EVENT_READ)
        except BaseException:
            self.selector.close()
            raise

    def read(self, deadline):
        while time.monotonic() < deadline:
            if self.check_stderr is not None:
                self.check_stderr()
            if b'\n' in self.buffer:
                line, self.buffer = self.buffer.split(b'\n', 1)
                self.log.write(line + b'\n')
                self.log.flush()
                try:
                    value = json.loads(line)
                except (ValueError, UnicodeError) as exc:
                    raise Failure('malformed QMP output') from exc
                require(isinstance(value, dict), 'QMP message is not an object')
                return value
            if self.eof:
                break
            if self.selector.select(min(0.2, max(0, deadline - time.monotonic()))):
                try:
                    block = os.read(self.proc.stdout.fileno(), 65536)
                except BlockingIOError:
                    continue
                if not block:
                    self.eof = True
                    require(not self.buffer, 'QMP closed with a partial message')
                    break
                self.bytes += len(block)
                require(self.bytes <= 1024 * 1024, 'QMP output limit exceeded')
                self.buffer += block
            elif self.proc.poll() is not None:
                break
        raise Failure('QMP response unavailable')

    def drain_events(self):
        while True:
            while b'\n' in self.buffer:
                line, self.buffer = self.buffer.split(b'\n', 1)
                self.log.write(line + b'\n')
                self.log.flush()
                try:
                    message = json.loads(line)
                except (ValueError, UnicodeError) as exc:
                    raise Failure('malformed QMP output') from exc
                require(type(message) is dict and type(message.get('event')) is str and
                        not any(key in message for key in ('id', 'return', 'error')),
                        'unexpected QMP message while observing VM')
            if self.eof:
                require(not self.buffer, 'QMP closed with a partial message')
                require(all(self.requests[request_id]['completed_monotonic'] is not None
                            for request_id in self.sent), 'QMP closed with an outstanding response')
                break
            if not self.selector.select(0):
                break
            try:
                block = os.read(self.proc.stdout.fileno(), 65536)
            except (BlockingIOError, InterruptedError):
                break
            if not block:
                self.eof = True
                continue
            self.bytes += len(block)
            require(self.bytes <= 1024 * 1024, 'QMP output limit exceeded')
            self.buffer += block

    def send(self, name, deadline, arguments=None):
        require(not self.eof, 'QMP output stream is closed')
        request_id = self.next_id
        self.next_id += 1
        record = {'id': request_id, 'name': name, 'sent_monotonic': None,
                  'completed_monotonic': None}
        self.requests[request_id] = self.last_request = record
        require(not self.write_broken, 'QMP write stream is broken')
        request = {'execute': name, 'id': request_id}
        if arguments is not None:
            request['arguments'] = arguments
        payload = canonical_json(request)
        require(len(payload) <= 4096, 'QMP request exceeds pipe atomic bound')
        written = 0
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(self.proc.stdin, selectors.EVENT_WRITE)
                while written < len(payload):
                    if self.check_stderr is not None:
                        self.check_stderr()
                    remaining = deadline - time.monotonic()
                    require(remaining > 0, 'QMP send deadline exceeded')
                    if not selector.select(min(0.2, remaining)):
                        continue
                    try:
                        count = os.write(self.proc.stdin.fileno(), payload[written:])
                    except (BlockingIOError, InterruptedError):
                        continue
                    require(count > 0, 'QMP write made no progress')
                    written += count
                    if written == len(payload):
                        record['sent_monotonic'] = time.monotonic()
                        self.sent.add(request_id)
        except BaseException:
            if 0 < written < len(payload):
                self.write_broken = True
            raise
        self.log.write(b'> ' + payload)
        self.log.flush()
        require(record['sent_monotonic'] <= deadline, 'QMP send deadline exceeded after complete write')
        return request_id

    def response(self, request_id, name, deadline):
        while True:
            reply = self.read(deadline)
            if 'event' in reply:
                require(isinstance(reply['event'], str) and 'id' not in reply
                        and 'return' not in reply and 'error' not in reply,
                        'malformed QMP event')
                continue
            reply_id = reply.get('id')
            require(type(reply_id) is int and reply_id > 0, 'QMP response missing valid ID')
            require(('return' in reply) != ('error' in reply), 'malformed QMP response')
            if 'error' in reply:
                error = reply['error']
                require(isinstance(error, dict) and isinstance(error.get('class'), str)
                        and isinstance(error.get('desc'), str), 'malformed QMP error')
            if reply_id != request_id:
                continue
            require(request_id in self.sent and self.requests[request_id]['name'] == name,
                    'QMP response does not match sent request')
            self.requests[request_id]['completed_monotonic'] = time.monotonic()
            require(self.requests[request_id]['completed_monotonic'] < deadline,
                    'QMP response deadline exceeded')
            if 'error' in reply:
                raise QmpCommandError(name, request_id, reply['error'])
            return reply['return']

    def execute(self, name, deadline, arguments=None):
        request_id = self.send(name, deadline, arguments)
        return self.response(request_id, name, deadline)

    def handshake(self, deadline):
        if not self.greeted:
            greeting = self.read(deadline)
            require(isinstance(greeting.get('QMP'), dict), 'missing QMP greeting')
            self.greeted = True
        if not self.negotiated:
            if self.capabilities_id is None:
                try:
                    self.capabilities_id = self.send('qmp_capabilities', deadline)
                finally:
                    last = self.last_request
                    if (self.capabilities_id is None and last is not None
                            and last['name'] == 'qmp_capabilities' and last['id'] in self.sent):
                        self.capabilities_id = last['id']
            self.response(self.capabilities_id, 'qmp_capabilities', deadline)
            self.negotiated = True


def same_open_path(path, fd, expected_kind):
    path_info = os.lstat(path)
    fd_info = os.fstat(fd)
    require(not stat.S_ISLNK(path_info.st_mode) and expected_kind(path_info.st_mode)
            and expected_kind(fd_info.st_mode)
            and (path_info.st_dev, path_info.st_ino) == (fd_info.st_dev, fd_info.st_ino),
            'capture path replaced: ' + str(path))


def same_capture_fd(fd, expected_fd):
    if expected_fd is not None:
        actual, expected = os.fstat(fd), os.fstat(expected_fd)
        require((actual.st_dev, actual.st_ino) == (expected.st_dev, expected.st_ino),
                'capture image descriptor was replaced')


def image_digest(path, expected_fd=None):
    check_path(path, False)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as file:
        same_open_path(path, file.fileno(), stat.S_ISREG)
        same_capture_fd(file.fileno(), expected_fd)
        sha = hashlib.sha256()
        size = 0
        while True:
            block = file.read(1024 * 1024)
            if not block:
                break
            sha.update(block)
            size += len(block)
        require(os.fstat(file.fileno()).st_size == size, 'capture image changed while hashing')
        same_open_path(path, file.fileno(), stat.S_ISREG)
        same_capture_fd(file.fileno(), expected_fd)
    return sha.hexdigest(), size


def ppm_info(path, expected_fd=None):
    """Validate the complete binary P6 image, not merely its magic prefix."""
    check_path(path, False)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as file:
        same_open_path(path, file.fileno(), stat.S_ISREG)
        same_capture_fd(file.fileno(), expected_fd)
        header_bytes = 0

        def byte():
            nonlocal header_bytes
            header_bytes += 1
            require(header_bytes <= 4096, 'PPM header too long')
            value = file.read(1)
            require(value, 'truncated PPM header')
            return value

        def token():
            value = byte()
            while value in b' \t\r\n\v\f' or value == b'#':
                if value == b'#':
                    while value not in (b'\n', b'\r'):
                        value = byte()
                value = byte()
            result = bytearray()
            while value not in b' \t\r\n\v\f':
                require(value != b'#', 'invalid PPM token')
                result.extend(value)
                value = byte()
            return bytes(result), value

        magic, _ = token()
        width, _ = token()
        height, _ = token()
        maximum, separator = token()
        require(magic == b'P6' and width.isdigit() and height.isdigit() and maximum.isdigit(),
                'invalid PPM header')
        width, height, maximum = int(width), int(height), int(maximum)
        require(width > 0 and height > 0 and 0 < maximum <= 65535, 'invalid PPM dimensions')
        if separator == b'\r':
            following = file.read(1)
            if following == b'\n':
                header_bytes += 1
            else:
                file.seek(-len(following), os.SEEK_CUR)
        payload_size = width * height * 3 * (1 if maximum < 256 else 2)
        require(os.fstat(file.fileno()).st_size - file.tell() == payload_size,
                'incomplete PPM payload')
        same_open_path(path, file.fileno(), stat.S_ISREG)
    return {'width': width, 'height': height, 'maxval': maximum, 'payload_bytes': payload_size}


class FifoCapture:
    LIMIT = 16 * 1024 * 1024

    def __init__(self, directory, deadline=None):
        self.directory = directory
        self.fifo = directory / 'screen.fifo'
        self.image = directory / 'screen.ppm'
        self.directory_fd = self.reader_fd = self.dummy_fd = self.image_fd = None
        self.ready = threading.Event()
        self.eof = threading.Event()
        self.stop = threading.Event()
        self.overflow = False
        self.error = None
        self.size = 0
        self.thread = None
        thread_started = False
        try:
            check_path(directory)
            self.directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            same_open_path(directory, self.directory_fd, stat.S_ISDIR)
            os.mkfifo(self.fifo, 0o600)
            created = os.lstat(self.fifo)
            require(stat.S_ISFIFO(created.st_mode), 'capture FIFO was replaced')
            self.reader_fd = os.open(self.fifo, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
            same_open_path(self.fifo, self.reader_fd, stat.S_ISFIFO)
            opened = os.fstat(self.reader_fd)
            require((created.st_dev, created.st_ino) == (opened.st_dev, opened.st_ino),
                    'capture FIFO changed during creation')
            self.dummy_fd = os.open(self.fifo, os.O_WRONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
            same_open_path(self.fifo, self.dummy_fd, stat.S_ISFIFO)
            require(os.fstat(self.reader_fd).st_ino == os.fstat(self.dummy_fd).st_ino
                    and os.fstat(self.reader_fd).st_dev == os.fstat(self.dummy_fd).st_dev,
                    'capture FIFO changed during open')
            self.image_fd = os.open(self.image, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            same_open_path(self.image, self.image_fd, stat.S_ISREG)
            self.thread = threading.Thread(target=self._drain, daemon=True)
            self.thread.start()
            thread_started = True
            wait = 1 if deadline is None else min(1, max(0, deadline - time.monotonic()))
            require(self.ready.wait(wait), 'FIFO reader did not become ready')
            require(self.error is None, 'FIFO reader failed: ' + str(self.error))
        except BaseException:
            self.stop.set()
            try:
                if thread_started:
                    self.thread.join(1)
            except BaseException:
                pass
            finally:
                try:
                    self._close_fds()
                except BaseException:
                    pass
            raise

    def _drain(self):
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(self.reader_fd, selectors.EVENT_READ)
                self.ready.set()
                while not self.stop.is_set():
                    if not selector.select(0.1):
                        continue
                    try:
                        block = os.read(self.reader_fd, 8192)
                    except BlockingIOError:
                        continue
                    if not block:
                        self.eof.set()
                        return
                    if self.error is not None:
                        continue
                    keep = block[:max(0, self.LIMIT - self.size)]
                    if len(keep) != len(block):
                        self.overflow = True
                    view = memoryview(keep)
                    try:
                        while view:
                            count = os.write(self.image_fd, view)
                            if count <= 0:
                                raise OSError('image write made no progress')
                            self.size += count
                            view = view[count:]
                    except OSError as exc:
                        # Keep draining the real writer after a disk failure until shutdown.
                        self.error = '%s: %s' % (type(exc).__name__, exc)
        except BaseException as exc:
            self.error = '%s: %s' % (type(exc).__name__, exc)
            self.ready.set()

    def check_paths(self):
        check_path(self.directory)
        same_open_path(self.directory, self.directory_fd, stat.S_ISDIR)
        same_open_path(self.fifo, self.reader_fd, stat.S_ISFIFO)
        same_open_path(self.image, self.image_fd, stat.S_ISREG)

    def close_dummy(self):
        if self.dummy_fd is not None:
            os.close(self.dummy_fd)
            self.dummy_fd = None

    def _close_fds(self):
        first_error = None
        for name in ('dummy_fd', 'reader_fd', 'image_fd', 'directory_fd'):
            fd = getattr(self, name)
            if fd is not None:
                try:
                    os.close(fd)
                except BaseException as exc:
                    if first_error is None:
                        first_error = exc
                finally:
                    setattr(self, name, None)
        if first_error is not None:
            raise first_error

    def complete(self, deadline):
        self.close_dummy()
        self.thread.join(min(1, max(0, deadline - time.monotonic())))
        require(not self.thread.is_alive() and self.eof.is_set(), 'FIFO real EOF not observed')
        require(self.error is None, 'FIFO reader failed: ' + str(self.error))
        require(not self.overflow, 'screenshot limit exceeded')
        self.check_paths()
        os.fsync(self.image_fd)
        info = ppm_info(self.image, self.image_fd)
        sha, size = image_digest(self.image, self.image_fd)
        require(size == self.size, 'screenshot size changed')
        self.check_paths()
        self._close_fds()
        return {'status': 'COMPLETE', 'sha256': sha, 'bytes': size, 'ppm': info}

    def unsupported(self, deadline):
        self.close_dummy()
        self.thread.join(min(1, max(0, deadline - time.monotonic())))
        require(not self.thread.is_alive() and self.eof.is_set(), 'FIFO real EOF not observed')
        require(self.error is None and not self.overflow and self.size == 0,
                'unsupported screenshot had unexpected FIFO output or reader error')
        self.check_paths()
        os.fsync(self.image_fd)
        self._close_fds()
        return {'status': 'UNSUPPORTED', 'bytes': 0, 'eof': True}

    def finish(self):
        started = time.monotonic()
        self.close_dummy()
        self.thread.join(1)
        if self.thread.is_alive():
            self.stop.set()
            self.thread.join(1)
        alive = self.thread.is_alive()
        try:
            if self.image_fd is not None:
                os.fsync(self.image_fd)
        finally:
            if not alive:
                self._close_fds()
        result = {'status': 'INCOMPLETE', 'bytes': self.size, 'overflow': self.overflow,
                  'eof': self.eof.is_set(), 'reader_error': self.error,
                  'reader_stopped': not alive, 'cleanup_seconds': time.monotonic() - started}
        require(not alive, 'FIFO reader did not stop')
        return result


def snapshot_due(elapsed, ready, taken):
    if elapsed >= 300:
        return None
    if 'initial' not in taken:
        return 'initial'
    if 'final' not in taken and (ready or elapsed >= 285):
        return 'final'
    if not ready and 'final' not in taken and 'early' not in taken and elapsed >= 15:
        return 'early'
    return None


def snapshot_save(entry):
    receipt_save(entry['file'], entry['data'])


def capture_snapshot(qmp, case_dir, label, deadline):
    require(label in ('initial', 'early', 'final'), 'invalid snapshot label')
    started = time.monotonic()
    snapshot_deadline = min(deadline, started + 10)
    diagnostics = case_dir / 'diagnostics'
    try:
        os.mkdir(diagnostics, 0o700)
    except FileExistsError:
        check_path(diagnostics)
    directory = diagnostics / label
    os.mkdir(directory, 0o700)
    file = exclusive(directory / 'snapshot.json')
    data = {'label': label, 'status': 'IN_PROGRESS', 'started_monotonic': started,
            'deadline_monotonic': snapshot_deadline, 'completed_monotonic': None,
            'commands': [], 'screenshot': {'status': 'NOT_RUN'}}
    entry = {'data': data, 'file': file, 'path': directory / 'snapshot.json', 'capture': None}
    qmp.snapshots.append(entry)
    try:
        snapshot_save(entry)
        for name in ('query-status', 'query-pci', 'query-block', 'query-blockstats', 'screendump'):
            require(time.monotonic() < snapshot_deadline, 'snapshot deadline exceeded')
            arguments = None
            record = {'name': name, 'id': None, 'started_monotonic': None,
                      'deadline_monotonic': None, 'sent_monotonic': None,
                      'completed_monotonic': None, 'return': None, 'error': None}
            data['commands'].append(record)
            qmp.last_request = None
            try:
                if name == 'screendump':
                    entry['capture'] = FifoCapture(directory, snapshot_deadline)
                    entry['capture'].check_paths()
                    arguments = {'filename': str(entry['capture'].fifo), 'format': 'ppm'}
                record['started_monotonic'] = time.monotonic()
                command_deadline = min(snapshot_deadline, record['started_monotonic'] + 2)
                record['deadline_monotonic'] = command_deadline
                result = qmp.execute(name, command_deadline, arguments)
                record['return'] = result
                if name == 'screendump':
                    data['screenshot'] = entry['capture'].complete(snapshot_deadline)
                    entry['capture'] = None
            except QmpCommandError as exc:
                record['error'] = {'type': type(exc).__name__, 'class': exc.error_class,
                                   'desc': exc.desc, 'request_id': exc.request_id}
                if name == 'screendump' and exc.error_class == 'CommandNotFound':
                    data['screenshot'] = entry['capture'].unsupported(snapshot_deadline)
                    entry['capture'] = None
                else:
                    raise
            except BaseException as exc:
                record['error'] = {'type': type(exc).__name__, 'message': str(exc)}
                raise
            finally:
                if qmp.last_request is not None:
                    record.update({key: qmp.last_request[key] for key in
                                   ('id', 'sent_monotonic', 'completed_monotonic')})
        data['completed_monotonic'] = time.monotonic()
        require(data['completed_monotonic'] < snapshot_deadline, 'snapshot deadline exceeded')
        data['status'] = 'PASS'
    except BaseException as exc:
        data.update(status='FAIL', error='%s: %s' % (type(exc).__name__, exc))
        raise
    finally:
        if data['completed_monotonic'] is None:
            data['completed_monotonic'] = time.monotonic()
        try:
            snapshot_save(entry)
        finally:
            if entry['capture'] is None:
                file.close()
    return data


class SnapshotFinalizationError(Failure):
    def __init__(self, results, errors):
        self.results = results
        self.errors = errors
        super().__init__('snapshot finalization: ' + '; '.join(
            '%s %s: %s' % (item['label'], item['stage'], item['error']) for item in errors))


def finish_snapshots(qmp, case_dir):
    errors, interrupted = [], []

    def record_error(entry, stage, exc):
        detail = {'label': entry['data']['label'], 'stage': stage,
                  'error': '%s: %s' % (type(exc).__name__, exc)}
        entry.setdefault('finalization_errors', []).append(detail)
        errors.append(detail)
        if isinstance(exc, (KeyboardInterrupt, SystemExit)):
            interrupted.append(exc)

    # Release every owner before any path validation or hashing can fail.
    for entry in qmp.snapshots:
        if entry['capture'] is not None:
            started = time.monotonic()
            try:
                entry['data']['screenshot'] = entry['capture'].finish()
            except BaseException as exc:
                entry['data']['screenshot'] = {'status': 'INCOMPLETE', 'cleanup_error': str(exc)}
                entry['data']['status'] = 'FAIL'
                entry['data'].setdefault('error', 'FIFO cleanup failed: ' + str(exc))
                record_error(entry, 'FIFO cleanup', exc)
            else:
                entry['capture'] = None
            finally:
                entry['data']['screenshot']['cleanup_seconds'] = time.monotonic() - started
            try:
                snapshot_save(entry)
            except BaseException as exc:
                record_error(entry, 'snapshot persistence', exc)
        try:
            if not entry['file'].closed:
                entry['file'].close()
        except BaseException as exc:
            record_error(entry, 'snapshot close', exc)

    results = []
    for entry in qmp.snapshots:
        data = entry['data']
        summary = {'label': data['label'], 'path': str(entry['path'].relative_to(case_dir)),
                   'sha256': None, 'status': data['status'], 'error': data.get('error')}
        try:
            summary['sha256'] = file_hash(entry['path'])
        except BaseException as exc:
            record_error(entry, 'snapshot hash', exc)
        if entry.get('finalization_errors'):
            summary['evidence_errors'] = entry['finalization_errors']
        results.append(summary)
    if interrupted:
        raise interrupted[0]
    if errors:
        raise SnapshotFinalizationError(results, errors)
    return results


def capture_identity(info):
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'capture is not a single-link regular file')
    return {'dev': info.st_dev, 'ino': info.st_ino, 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode)}


def create_protocol_file(path):
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchmod(fd, 0o600)
        info = os.fstat(fd)
        identity = capture_identity(info)
        require(info.st_size == 0 and identity['uid'] == os.geteuid() and identity['mode'] == 0o600 and
                capture_identity(os.lstat(path)) == identity, 'new protocol file identity invalid')
        os.fsync(fd)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return fd, identity
    except BaseException:
        os.close(fd)
        raise


def read_capture(path, expected=None, fd=None, quiescent=False):
    """Read a bounded, non-symlink capture while binding pathname and held inode."""
    check_path(path, False)
    owned = fd is None
    if owned:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        initial = os.fstat(fd)
        identity = capture_identity(initial)
        require(capture_identity(os.lstat(path)) == identity and (expected is None or identity == expected),
                'capture path or identity changed: ' + str(path))
        require(0 <= initial.st_size <= PROTOCOL_LIMIT, 'capture size limit exceeded: ' + str(path))
        parts, offset = [], 0
        while offset < initial.st_size:
            block = os.pread(fd, min(65536, initial.st_size - offset), offset)
            require(bool(block), 'capture shortened during read: ' + str(path))
            parts.append(block)
            offset += len(block)
        final = os.fstat(fd)
        require(capture_identity(final) == identity == capture_identity(os.lstat(path)) and
                initial.st_size <= final.st_size <= PROTOCOL_LIMIT and
                (not quiescent or initial.st_size == final.st_size),
                'capture changed during read: ' + str(path))
        return b''.join(parts), identity
    finally:
        if owned:
            os.close(fd)


def serial_bytes(path):
    if not os.path.lexists(path):
        return b''
    return read_capture(path)[0]


def observe_protocol_ready(path, case, attempt, deadline, fd, identity):
    data, actual = read_capture(path, identity, fd)
    require(actual['uid'] == os.geteuid() and actual['mode'] == 0o600, 'protocol file permissions changed')
    previous = attempt.get('protocol_bytes_observed', 0)
    require(len(data) >= previous, 'protocol file shortened')
    attempt['protocol_bytes_observed'] = len(data)
    prefix = _protocol_prefix(data, case, complete=False)
    observed = time.monotonic()
    require(observed < deadline, 'VM wall timeout')
    if prefix['ready'] and not attempt.get('ready'):
        attempt['ready'] = True
        attempt['ready_monotonic'] = observed


def save_capture_evidence(case_dir, state, protocol_fd):
    attempt = state['attempts'][-1]
    require(attempt.get('exit_observed') is True and attempt.get('pidfd_exited') is True,
            'cannot finalize captures before owned VM exit')
    errors, protocol = [], None
    for name in ('serial', 'protocol'):
        try:
            expected = state['protocol_file'] if name == 'protocol' else None
            data, identity = read_capture(case_dir / (name + '.log'), expected,
                                          protocol_fd if name == 'protocol' else None, quiescent=True)
            require(len(data) >= attempt.get(name + '_bytes_observed', 0), name + ' capture shortened after observation')
            sha = hashlib.sha256(data).hexdigest()
            for key, value in ((name + '_sha256', sha), (name + '_bytes', len(data)),
                               (name + '_file_after', identity)):
                require(key not in state or state[key] == value, 'final capture evidence changed: ' + key)
                state[key] = value
            if name == 'protocol':
                protocol = data
        except (OSError, Failure, ValueError) as exc:
            errors.append(name + ': ' + str(exc))
    if errors:
        state['capture_errors'] = errors
        raise Failure('; '.join(errors))
    return protocol



def expected_vm_argv(case_dir):
    return [str(RUN / 'build/qemu/qemu-system-x86_64'),
            '-machine', 'q35', '-accel', 'kvm', '-cpu', 'host', '-smp', '2', '-m', '4096',
            '-L', str(RUN / 'build/source' / ROOT / 'pc-bios'),
            '-display', 'none', '-monitor', 'none', '-qmp', 'stdio',
            '-serial', 'file:' + str(case_dir / 'serial.log'), '-nic', 'none', '-no-reboot',
            '-device', 'virtio-serial-pci,id=g2-result-controller',
            '-chardev', 'file,id=g2-result-output,path=' + str(case_dir / 'protocol.log') + ',append=on',
            '-device', 'virtserialport,bus=g2-result-controller.0,nr=1,chardev=g2-result-output,name=' + RESULT_PORT,
            '-drive', 'if=none,id=boot,format=qcow2,file=' + str(case_dir / 'boot.qcow2'),
            '-device', 'virtio-blk-pci,id=bootdisk,drive=boot,bootindex=1',
            '-drive', 'if=none,id=ext,format=raw,file=' + str(case_dir / 'ext.raw'),
            '-device', 'virtio-blk-pci,id=extdisk,drive=ext,serial=G2EXT',
            '-drive', 'if=none,id=btr,format=raw,file=' + str(case_dir / 'btr.raw'),
            '-device', 'virtio-blk-pci,id=btrdisk,drive=btr,serial=G2BTR',
            '-blockdev', 'driver=vvfat,node-name=seed,dir=' + str(case_dir / 'seed') +
            ',label=CIDATA,rw=off,read-only=on',
            '-device', 'virtio-blk-pci,id=seeddisk,drive=seed']


def verify_result_argv(argv, case_dir):
    require(type(argv) is list and all(type(value) is str for value in argv) and argv == expected_vm_argv(case_dir),
            'QEMU argv differs from fixed result transport or resource contract')


def validate_capture_identity(value, name):
    preparation.fields(value, ('dev', 'ino', 'uid', 'gid', 'mode'), name)
    preparation.integer(value['dev'], name + ' dev')
    preparation.integer(value['ino'], name + ' inode', 1)
    for key in ('uid', 'gid'):
        preparation.integer(value[key], name + ' ' + key, 0, (1 << 32) - 1)
    preparation.integer(value['mode'], name + ' mode', 0, 0o7777)
    return value


def verify_vm_outputs(case_dir, receipt, case):
    """Independently bind both closed capture files, launch identity and parsed result."""
    attempts = receipt.get('attempts')
    require(type(attempts) is list and len(attempts) == 1, 'VM attempt inventory differs')
    attempt = attempts[0]
    require(type(attempt) is dict and attempt.get('backend') == 'kvm' and
            attempt.get('pidfd_held') is True and attempt.get('pidfd_exited') is True and
            attempt.get('wait_observed') is True and attempt.get('exit_observed') is True and
            receipt.get('exit_observed') is True and attempt.get('qmp_handshake') is True and
            attempt.get('ready') is True and attempt.get('protocol_monitor_complete') is True,
            'VM owned exit or live protocol observation missing')
    require(type(attempt.get('returncode')) is int, 'VM wait status missing')
    times = [attempt.get(key) for key in ('observation_started_monotonic', 'spawned_monotonic',
             'handshake_monotonic', 'ready_monotonic', 'observation_deadline_monotonic', 'exit_observed_monotonic')]
    require(all(type(value) in (int, float) and 0 <= value < float('inf') for value in times),
            'VM observation timeline missing')
    started, spawned, handshake, ready, deadline, exited = times
    require(case in VM_WALL_SECONDS and deadline == started + VM_WALL_SECONDS[case] and
            started <= spawned <= handshake <= ready < deadline and exited >= ready,
            'VM observation or READY deadline differs')
    if receipt.get('status') in ('PASS', 'IN_PROGRESS'):
        require(attempt['returncode'] == 0 and attempt.get('stderr_overflow') is False and
                not any(attempt.get(key) for key in ('error', 'stderr_error', 'pidfd_error', 'qmp_init_error',
                                                     'cleanup_errors')) and not receipt.get('capture_errors'),
                'successful VM has an exit or capture error')
    argv = attempt.get('argv')
    verify_result_argv(argv, case_dir)
    require(attempt.get('argv_sha256') == hashlib.sha256(canonical_json(argv)).hexdigest() and
            attempt.get('proc_argv_sha256') == hashlib.sha256(b'\0'.join(os.fsencode(x) for x in argv) + b'\0').hexdigest() and
            attempt.get('exe') == argv[0] and attempt.get('exe_sha256') == file_hash(Path(argv[0])),
            'QEMU executable or actual argv binding differs')
    protocol_identity = receipt.get('protocol_file')
    validate_capture_identity(protocol_identity, 'protocol file identity')
    validate_capture_identity(receipt.get('protocol_file_after'), 'final protocol file identity')
    serial_expected = validate_capture_identity(receipt.get('serial_file_after'), 'final console file identity')
    require(protocol_identity['uid'] == os.geteuid() and protocol_identity['mode'] == 0o600 and
            preparation.canonical_json(attempt.get('protocol_identity_before')) ==
            preparation.canonical_json(attempt.get('protocol_identity_after')) ==
            preparation.canonical_json(receipt['protocol_file_after']) == preparation.canonical_json(protocol_identity),
            'protocol lifecycle identity differs')
    protocol, _ = read_capture(case_dir / 'protocol.log', protocol_identity, quiescent=True)
    serial, serial_identity = read_capture(case_dir / 'serial.log', serial_expected, quiescent=True)
    require(preparation.canonical_json(serial_identity) ==
            preparation.canonical_json(attempt.get('serial_identity_observed')) and
            type(receipt.get('serial_bytes')) is int and receipt['serial_bytes'] == len(serial) and
            receipt.get('serial_sha256') == hashlib.sha256(serial).hexdigest() and
            type(receipt.get('protocol_bytes')) is int and receipt['protocol_bytes'] == len(protocol) and
            receipt.get('protocol_sha256') == hashlib.sha256(protocol).hexdigest(),
            'capture identity, length, or digest differs')
    require(type(attempt.get('protocol_bytes_observed')) is int and
            0 <= attempt['protocol_bytes_observed'] <= len(protocol) and
            type(attempt.get('serial_bytes_observed')) is int and
            0 <= attempt['serial_bytes_observed'] <= len(serial), 'capture observation sizes differ')
    bundle = parse_frames(protocol, case)
    validate_abort_install_receipt(receipt, bundle['installation'])
    require(preparation.canonical_json(bundle['transport']) == preparation.canonical_json(receipt.get('transport')),
            'transport receipt differs from INSTALL')
    return bundle

def held_shutdown(proc, pidfd, qmp, state):
    state['shutdown_started_monotonic'] = time.monotonic()
    state['quit_sent'] = False
    state['quit_ack'] = False
    if proc.poll() is None:
        try:
            if qmp is not None and not qmp.write_broken:
                qmp.check_stderr = None
                qmp_deadline = time.monotonic() + 3
                if not qmp.negotiated:
                    qmp.handshake(qmp_deadline)
                request_id = qmp.send('quit', qmp_deadline)
                state['quit_id'] = request_id
                state['quit_sent'] = True
                qmp.response(request_id, 'quit', qmp_deadline)
                state['quit_ack'] = True
                state['quit'] = 'QMP'
        except Exception as exc:
            state['qmp_quit_error'] = '%s: %s' % (type(exc).__name__, exc)
        finally:
            if qmp is not None and qmp.last_request is not None:
                last = qmp.last_request
                if last['name'] == 'quit' and last['id'] in qmp.sent:
                    state['quit_id'] = last['id']
                    state['quit_sent'] = True
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            if pidfd is not None:
                try:
                    signal.pidfd_send_signal(pidfd, signal.SIGTERM)
                    state['signal'] = 'SIGTERM via held pidfd'
                except OSError as exc:
                    state['pidfd_signal_error'] = str(exc)
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                state['exit_observed'] = False
                state['exit_uncertain'] = True
                state['shutdown_completed_monotonic'] = time.monotonic()
                return
    state['wait_observed'] = proc.returncode is not None
    state['pidfd_exited'] = pidfd is not None and bool(select.select([pidfd], [], [], 0)[0])
    state['exit_observed'] = state['wait_observed'] and state['pidfd_exited']
    state['exit_uncertain'] = not state['exit_observed']
    state['returncode'] = proc.returncode
    state['exit_observed_monotonic'] = time.monotonic()
    state['shutdown_completed_monotonic'] = time.monotonic()


VM_WALL_SECONDS = {'abort': 300, 'matrix': 1800, **{case: 600 for case in READER_FAULT_CASES}}


def observe_console(path, attempt):
    if not os.path.lexists(path):
        require('serial_identity_observed' not in attempt, 'console capture disappeared')
        return
    check_path(path, False)
    info = os.lstat(path)
    identity = capture_identity(info)
    require(info.st_size <= PROTOCOL_LIMIT and
            attempt.get('serial_identity_observed', identity) == identity and
            info.st_size >= attempt.get('serial_bytes_observed', 0), 'console capture changed or overflowed')
    attempt['serial_identity_observed'] = identity
    attempt['serial_bytes_observed'] = info.st_size


def observe_vm(case, proc, collector, qmp, case_dir, attempt, started, deadline, protocol_fd, protocol_identity):
    taken = set()
    if case == 'abort':
        capture_snapshot(qmp, case_dir, 'initial', deadline)
        taken.add('initial')
    while proc.poll() is None:
        now = time.monotonic()
        require(now < deadline, 'VM wall timeout')
        collector.check()
        qmp.drain_events()
        observe_console(case_dir / 'serial.log', attempt)
        was_ready = attempt.get('ready') is True
        observe_protocol_ready(case_dir / 'protocol.log', case, attempt, deadline, protocol_fd, protocol_identity)
        if not was_ready and attempt.get('ready') is True:
            require(proc.poll() is None, 'READY was not observed while the VM was alive')
        if case == 'abort':
            label = snapshot_due(time.monotonic() - started, attempt.get('ready') is True, taken)
            if label is not None:
                capture_snapshot(qmp, case_dir, label, deadline)
                taken.add(label)
                continue
            if attempt.get('ready') and 'final' in taken:
                if 'early' not in taken:
                    attempt['early_skipped'] = 'READY before early snapshot'
                break
        time.sleep(min(0.2, max(0, deadline - time.monotonic())))
    if case != 'abort':
        require(time.monotonic() < deadline, 'VM wall timeout')
        require(proc.poll() is not None, 'VM did not exit')
    require(attempt.get('ready') is True, 'READY was not observed while the VM was alive')
    observe_console(case_dir / 'serial.log', attempt)
    qmp.drain_events()
    observe_protocol_ready(case_dir / 'protocol.log', case, attempt, deadline, protocol_fd, protocol_identity)
    attempt['protocol_monitor_complete'] = True


def vm_attempt(case, backend, argv, case_dir, env, receipt, state, protocol_fd):
    require(case in VM_WALL_SECONDS, 'invalid VM case')
    attempt = {'backend': backend, 'argv': argv, 'argv_sha256': hashlib.sha256(canonical_json(argv)).hexdigest(),
               'intent': 'PRE_START', 'exit_observed': False}
    state['attempts'].append(attempt)
    state['diagnostics'] = []
    receipt_save(receipt, state)
    proc = pidfd = qmp = collector = failure = None
    stderr_data = b''
    try:
        with exclusive(case_dir / (backend + '.stderr')) as stderr, exclusive(case_dir / (backend + '.qmp')) as qlog:
            try:
                preflight_pidfd()
                data, identity = read_capture(case_dir / 'protocol.log', state['protocol_file'], protocol_fd)
                require(not data, 'protocol file is not empty before VM start')
                attempt['protocol_identity_before'] = identity
                t0 = time.monotonic()
                deadline = t0 + VM_WALL_SECONDS[case]
                attempt['observation_started_monotonic'] = t0
                attempt['observation_deadline_monotonic'] = deadline
                proc = subprocess.Popen(argv, cwd=RUN, env=env, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0,
                                        preexec_fn=qemu_limits)
                attempt['spawned_monotonic'] = time.monotonic()
                attempt['pid'] = proc.pid
                init_error = None
                try:
                    pidfd = os.pidfd_open(proc.pid, 0)
                    attempt['pidfd_held'] = True
                except BaseException as exc:
                    attempt['pidfd_error'] = '%s: %s' % (type(exc).__name__, exc)
                    init_error = exc
                # Either independent holder remains usable if the other fails.
                try:
                    qmp = Qmp(proc, qlog)
                except BaseException as exc:
                    attempt['qmp_init_error'] = '%s: %s' % (type(exc).__name__, exc)
                    if init_error is None:
                        init_error = exc
                collector = StderrCollector(proc.stderr)
                if qmp is not None:
                    qmp.check_stderr = collector.check
                attempt['intent'] = 'STARTED'
                receipt_save(receipt, state)
                if init_error is not None:
                    raise init_error
                collector.check()
                if proc.poll() is None:
                    attempt['starttime'] = proc_starttime(proc.pid)
                    attempt['exe'] = os.readlink('/proc/%d/exe' % proc.pid)
                    attempt['exe_sha256'] = file_hash(Path(attempt['exe']))
                    require(attempt['exe'] == argv[0] and
                            attempt['exe_sha256'] == file_hash(Path(argv[0])),
                            'QEMU executable identity mismatch')
                    actual_cmdline = Path('/proc/%d/cmdline' % proc.pid).read_bytes()
                    require(actual_cmdline == b'\0'.join(os.fsencode(x) for x in argv) + b'\0',
                            'QEMU argv identity mismatch')
                    attempt['proc_argv_sha256'] = hashlib.sha256(actual_cmdline).hexdigest()
                receipt_save(receipt, state)
                require(pidfd is not None and 'proc_argv_sha256' in attempt,
                        'held pidfd or running QEMU argv identity unavailable')
                qmp.handshake(min(deadline, time.monotonic() + 20))
                attempt['qmp_handshake'] = True
                attempt['handshake_monotonic'] = time.monotonic()
                observe_vm(case, proc, collector, qmp, case_dir, attempt, t0, deadline,
                           protocol_fd, state['protocol_file'])
            except BaseException as exc:
                failure = exc
            finally:
                def cleanup_error(exc):
                    nonlocal failure
                    attempt.setdefault('cleanup_errors', []).append('%s: %s' % (type(exc).__name__, exc))
                    if failure is None:
                        failure = exc

                if proc is not None:
                    try:
                        held_shutdown(proc, pidfd, qmp, attempt)
                    except BaseException as exc:
                        cleanup_error(exc)
                    try:
                        _, attempt['protocol_identity_after'] = read_capture(
                            case_dir / 'protocol.log', state['protocol_file'], protocol_fd,
                            quiescent=attempt.get('exit_observed') is True)
                        observe_console(case_dir / 'serial.log', attempt)
                    except BaseException as exc:
                        cleanup_error(exc)
                    if qmp is not None:
                        try:
                            state['diagnostics'] = finish_snapshots(qmp, case_dir)
                        except SnapshotFinalizationError as exc:
                            state['diagnostics'] = exc.results
                            cleanup_error(exc)
                        except BaseException as exc:
                            cleanup_error(exc)
                        qmp.selector.close()
                    if pidfd is not None:
                        os.close(pidfd)
                    for stream in (proc.stdin, proc.stdout):
                        if stream:
                            try:
                                stream.close()
                            except OSError as exc:
                                cleanup_error(exc)
                    if collector is not None:
                        try:
                            started = time.monotonic()
                            try:
                                stderr_data = collector.finish()
                            finally:
                                attempt['stderr_cleanup_seconds'] = time.monotonic() - started
                                if collector.error is not None:
                                    attempt['stderr_error'] = '%s: %s' % (
                                        type(collector.error).__name__, collector.error)
                            stderr.write(stderr_data)
                            stderr.flush()
                            os.fsync(stderr.fileno())
                            attempt['stderr_overflow'] = collector.overflow.is_set()
                            collector.check()
                        except BaseException as exc:
                            cleanup_error(exc)
                    elif proc.stderr:
                        proc.stderr.close()
                receipt_save(receipt, state)
    except BaseException as exc:
        if failure is None:
            failure = exc
    if failure is not None:
        if collector is not None and collector.overflow.is_set():
            attempt['stderr_overflow'] = True
        attempt['error'] = '%s: %s' % (type(failure).__name__, failure)
        receipt_save(receipt, state)
        raise failure
    require(attempt.get('exit_observed') is True, 'QEMU exit uncertain')
    require(attempt.get('returncode') == 0, 'QEMU exited nonzero: %s' % attempt.get('returncode'))
    require(attempt.get('ready') is True and attempt.get('protocol_monitor_complete') is True and
            attempt.get('protocol_identity_before') == attempt.get('protocol_identity_after') == state['protocol_file'],
            'VM protocol readiness or capture identity unproved')
    if case == 'abort':
        require(attempt.get('ready') is True and attempt.get('quit_sent') is True,
                'abort did not receive READY and send QMP quit')
    return 'PASS'


def diagnostic_report(case_dir, receipt):
    attempts = receipt.get('attempts', [])
    conclusion = {'status': 'NOT_RUN', 'reasons': [], 'snapshots': [],
                  'ready': False, 'quit_sent': False, 'quit_ack': False,
                  'exit_observed': False, 'returncode': None}
    if not attempts:
        conclusion['reasons'].append('no VM attempt')
        return conclusion
    attempt = attempts[-1]
    for key in ('ready', 'quit_sent', 'quit_ack', 'exit_observed', 'returncode'):
        conclusion[key] = attempt.get(key, False if key != 'returncode' else None)
    failures, unknown = [], []
    for key in ('error', 'stderr_error', 'pidfd_error', 'qmp_init_error'):
        if attempt.get(key):
            failures.append('attempt %s: %s' % (key, attempt[key]))
    if attempt.get('stderr_overflow') is True:
        failures.append('attempt stderr overflow')
    elif attempt.get('stderr_overflow') is not False:
        failures.append('stderr collector completion not established')
    if receipt.get('persistence_error'):
        failures.append('abort receipt persistence failure: ' + str(receipt['persistence_error']))
    if receipt.get('diagnostic_fatal_error'):
        failures.append('abort diagnostic failure: ' + str(receipt['diagnostic_fatal_error']))
    for error in attempt.get('cleanup_errors', []):
        failures.append('attempt cleanup failure: ' + str(error))

    def valid_time(value):
        return type(value) in (int, float) and 0 <= value < float('inf')

    started = attempt.get('observation_started_monotonic')
    deadline = attempt.get('observation_deadline_monotonic')
    spawned = attempt.get('spawned_monotonic')
    handshake = attempt.get('handshake_monotonic')
    ready = attempt.get('ready_monotonic')
    observation_valid = (all(valid_time(value) for value in (started, deadline, spawned, handshake, ready))
                         and deadline == started + 300
                         and started <= spawned <= handshake < deadline
                         and handshake <= ready < deadline)
    if not observation_valid:
        failures.append('observation, handshake or READY timeline invalid')
    conclusion['timeline'] = {'observation_started_monotonic': started,
                              'observation_deadline_monotonic': deadline,
                              'spawned_monotonic': spawned, 'handshake_monotonic': handshake,
                              'ready_monotonic': ready}
    if len(attempts) != 1 or attempt.get('backend') != 'kvm':
        failures.append('expected exactly one KVM attempt')
    if not (attempt.get('ready') is True and attempt.get('quit_sent') is True
            and attempt.get('exit_observed') is True and attempt.get('returncode') == 0):
        failures.append('READY, sent quit and owned exit 0 are required')
    if receipt.get('base_unchanged') is not True:
        failures.append('base immutability not established')
    if receipt.get('seed_unchanged') is not True:
        failures.append('seed immutability not established')
    if attempt.get('ready') is True:
        try:
            verify_vm_outputs(case_dir, receipt, 'abort')
        except (OSError, Failure, ValueError, TypeError, KeyError) as exc:
            failures.append('READY protocol evidence invalid: ' + str(exc))
    entries = receipt.get('diagnostics', [])
    require(isinstance(entries, list), 'invalid diagnostics list')
    snapshots = {}
    seen_labels = set()
    for entry in entries:
        require(isinstance(entry, dict), 'invalid diagnostic entry')
        label = entry.get('label')
        require(label in ('initial', 'early', 'final') and label not in seen_labels,
                'invalid or duplicate diagnostic label')
        seen_labels.add(label)
        relative = 'diagnostics/%s/snapshot.json' % label
        require(entry.get('path') == relative, 'invalid diagnostic snapshot path')
        if entry.get('evidence_errors'):
            failures.append('%s snapshot evidence failure: %s' % (label, entry['evidence_errors']))
            conclusion['snapshots'].append({'label': label, 'path': relative,
                                            'sha256': entry.get('sha256'), 'status': entry.get('status'),
                                            'evidence_errors': entry['evidence_errors']})
            continue
        path = case_dir / relative
        require(file_hash(path) == entry.get('sha256'), 'snapshot hash changed: ' + label)
        data = load_json(path)
        require(data.get('label') == label and data.get('status') == entry.get('status')
                and data.get('error') == entry.get('error'), 'snapshot receipt mismatch: ' + label)
        screenshot = data.get('screenshot')
        require(isinstance(screenshot, dict), 'missing screenshot record')
        if screenshot.get('status') == 'COMPLETE':
            image = case_dir / 'diagnostics' / label / 'screen.ppm'
            sha, size = image_digest(image)
            require(sha == screenshot.get('sha256') and size == screenshot.get('bytes')
                    and ppm_info(image) == screenshot.get('ppm'), 'screenshot changed: ' + label)
        elif screenshot.get('status') == 'UNSUPPORTED':
            commands = data.get('commands', [])
            require(any(item.get('name') == 'screendump'
                        and item.get('error', {}).get('class') == 'CommandNotFound'
                        and item.get('error', {}).get('request_id') == item.get('id')
                        for item in commands if isinstance(item, dict)
                        and isinstance(item.get('error'), dict)),
                    'unsupported screenshot lacks matching command error')
        elif data.get('status') == 'PASS':
            raise Failure('successful snapshot has incomplete screenshot')
        snapshots[label] = data
        conclusion['snapshots'].append({'label': label, 'path': relative,
                                        'sha256': entry['sha256'], 'status': data['status'],
                                        'screenshot': screenshot})
        if data['status'] != 'PASS':
            failures.append('%s snapshot failed: %s' % (label, data.get('error', 'unknown error')))
    for label in ('initial', 'final'):
        if label not in snapshots:
            failures.append(label + ' snapshot missing')
    if list(snapshots) != sorted(snapshots, key=('initial', 'early', 'final').index):
        failures.append('snapshot observation order invalid')
    previous_end = handshake if observation_valid else None
    for label, data in snapshots.items():
        if data.get('status') != 'PASS':
            continue
        begin = data.get('started_monotonic')
        end = data.get('completed_monotonic')
        limit = data.get('deadline_monotonic')
        valid = (observation_valid and all(valid_time(value) for value in (begin, end, limit))
                 and started <= handshake <= begin <= end < limit <= deadline
                 and limit == min(deadline, begin + 10)
                 and (previous_end is None or previous_end <= begin))
        if valid and label == 'initial':
            valid = end <= ready
        if valid and label == 'early':
            valid = begin >= started + 15
        if valid and label == 'final':
            valid = begin >= min(ready, started + 285)
        if not valid:
            failures.append(label + ' snapshot timeline invalid')
        if valid_time(end):
            previous_end = end

    def snapshot_values(label):
        data = snapshots.get(label)
        if not data or data.get('status') != 'PASS':
            return None
        commands = data.get('commands')
        require(isinstance(commands, list) and len(commands) == 5
                and [item.get('name') for item in commands] ==
                ['query-status', 'query-pci', 'query-block', 'query-blockstats', 'screendump'],
                'invalid snapshot command sequence: ' + label)
        ids = set()
        previous_command_end = data.get('started_monotonic')
        for item in commands:
            request_id = item.get('id')
            require(type(request_id) is int and request_id > 0 and request_id not in ids,
                    'snapshot command ID invalid: ' + label)
            ids.add(request_id)
            begin = item.get('started_monotonic')
            limit = item.get('deadline_monotonic')
            sent = item.get('sent_monotonic')
            end = item.get('completed_monotonic')
            snapshot_begin = data.get('started_monotonic')
            snapshot_end = data.get('completed_monotonic')
            snapshot_limit = data.get('deadline_monotonic')
            valid = (all(valid_time(value) for value in
                         (previous_command_end, begin, limit, sent, end,
                          snapshot_begin, snapshot_end, snapshot_limit))
                     and snapshot_begin <= previous_command_end <= begin <= sent <= end < limit
                     and end <= snapshot_end < snapshot_limit
                     and limit == min(snapshot_limit, begin + 2))
            if not valid:
                failures.append('%s %s command timeline invalid' % (label, item.get('name')))
            if valid_time(end):
                previous_command_end = end
        require(all(item.get('error') is None for item in commands[:4]),
                'ordinary snapshot query failed: ' + label)
        if data['screenshot']['status'] == 'COMPLETE':
            require(commands[4].get('error') is None, 'completed screenshot command failed: ' + label)
        values = [item.get('return') for item in commands[:4]]
        require(isinstance(values[0], dict) and all(isinstance(value, list) for value in values[1:]),
                'invalid ordinary query results: ' + label)
        return values
    if 'early' in snapshots:
        snapshot_values('early')

    def pci_devices(value):
        result = []

        def visit(devices):
            require(isinstance(devices, list), 'invalid PCI devices')
            for device in devices:
                require(isinstance(device, dict), 'invalid PCI device')
                result.append(device)
                bridge = device.get('pci_bridge')
                if isinstance(bridge, dict) and 'devices' in bridge:
                    visit(bridge['devices'])
        for bus in value:
            require(isinstance(bus, dict), 'invalid PCI bus')
            visit(bus.get('devices'))
        return result

    mappings = {}
    for label in ('initial', 'final'):
        values = snapshot_values(label)
        if values is None:
            continue
        _, pci, blocks, stats = values
        devices = pci_devices(pci)
        mapping = {}
        for backend, qdev in (('boot', 'bootdisk'), ('ext', 'extdisk'),
                              ('btr', 'btrdisk'), ('seed', 'seeddisk')):
            matched_pci = [item for item in devices if item.get('qdev_id') == qdev]
            matched_block = [item for item in blocks if isinstance(item, dict) and
                             (item.get('device') == backend if backend != 'seed' else
                              isinstance(item.get('inserted'), dict) and
                              item['inserted'].get('node-name') == 'seed')]
            item = {'qdev_id': qdev, 'pci_matches': len(matched_pci), 'block_matches': len(matched_block)}
            if len(matched_pci) == 1:
                dev = matched_pci[0]
                bdf = [dev.get('bus'), dev.get('slot'), dev.get('function')]
                if all(type(number) is int for number in bdf) and 0 <= bdf[0] <= 255 \
                        and 0 <= bdf[1] <= 31 and 0 <= bdf[2] <= 7:
                    item['bdf'] = '%02x:%02x.%x' % tuple(bdf)
            if len(matched_block) == 1:
                block = matched_block[0]
                inserted = block.get('inserted')
                item['block_qdev'] = block.get('qdev')
                item['file'] = inserted.get('file') if isinstance(inserted, dict) else None
                if backend == 'seed' and isinstance(inserted, dict):
                    item['read_only'] = inserted.get('ro')
            mapping[backend] = item
        controllers = [item for item in devices if item.get('qdev_id') == 'g2-result-controller']
        controller = {'pci_matches': len(controllers)}
        if len(controllers) == 1:
            device = controllers[0]
            bdf = [device.get('bus'), device.get('slot'), device.get('function')]
            if all(type(number) is int for number in bdf) and 0 <= bdf[0] <= 255 and \
                    0 <= bdf[1] <= 31 and 0 <= bdf[2] <= 7:
                controller['bdf'] = '%02x:%02x.%x' % tuple(bdf)
        mapping['result_controller'] = controller
        if controller.get('pci_matches') != 1 or controller.get('bdf') is None:
            unknown.append(label + ' result controller PCI identity incomplete')
        for backend, qdev, filename in (('boot', 'bootdisk', 'boot.qcow2'),
                                        ('ext', 'extdisk', 'ext.raw'), ('btr', 'btrdisk', 'btr.raw'),
                                        ('seed', 'seeddisk', None)):
            item = mapping[backend]
            allowed_qdevs = {qdev, '/machine/peripheral/' + qdev + '/virtio-backend'}
            if item.get('block_qdev') is not None and item['block_qdev'] not in allowed_qdevs:
                failures.append(label + ' ' + backend + ' backend qdev mismatch')
            if (item.get('pci_matches') != 1 or item.get('block_matches') != 1 or
                    item.get('bdf') is None or item.get('block_qdev') is None or item.get('file') is None):
                unknown.append(label + ' ' + backend + ' PCI/block association incomplete or ambiguous')
            elif filename is not None and item['file'] != str(case_dir / filename):
                failures.append(label + ' ' + backend + ' backend file mismatch')
            if backend == 'seed' and item.get('file') is not None:
                file_value = item['file']
                try:
                    seed_file = preparation.strict_json(file_value[5:].encode('ascii')) if \
                        type(file_value) is str and file_value.startswith('json:') else None
                except (ValueError, UnicodeError):
                    seed_file = None
                if seed_file != {'dir': str(case_dir / 'seed'), 'driver': 'vvfat', 'label': 'CIDATA', 'rw': False}:
                    failures.append(label + ' seed backend file mismatch')
                if item.get('read_only') is not True:
                    failures.append(label + ' seed backend is not read-only')
        addresses = [mapping[key].get('bdf') for key in ('boot', 'ext', 'btr', 'seed', 'result_controller')]
        if all(address is not None for address in addresses) and len(set(addresses)) != len(addresses):
            failures.append(label + ' PCI identities overlap')
        boot = mapping['boot']
        expected_qdevs = {'bootdisk', '/machine/peripheral/bootdisk/virtio-backend'}
        found_stats = [item for item in stats if isinstance(item, dict)
                       and item.get('device') == 'boot' and boot.get('block_qdev') in expected_qdevs
                       and item.get('qdev') == boot['block_qdev']]
        if len(found_stats) == 1 and isinstance(found_stats[0].get('stats'), dict):
            values = found_stats[0]['stats']
            if all(type(values.get(key)) is int and values[key] >= 0
                   for key in ('rd_bytes', 'rd_operations')):
                boot['stats'] = {key: values[key] for key in ('rd_bytes', 'rd_operations')}
        if 'stats' not in boot:
            unknown.append(label + ' boot block statistics cannot be associated')
        mappings[label] = mapping
    conclusion['mapping'] = mappings
    if 'initial' in mappings and 'final' in mappings:
        first, last = mappings['initial']['boot'], mappings['final']['boot']
        if first.get('bdf') and last.get('bdf') and first['bdf'] != last['bdf']:
            failures.append('boot PCI address changed between snapshots')
        for name in ('ext', 'btr', 'seed', 'result_controller'):
            before_bdf = mappings['initial'][name].get('bdf')
            after_bdf = mappings['final'][name].get('bdf')
            if before_bdf and after_bdf and before_bdf != after_bdf:
                failures.append(name + ' PCI address changed between snapshots')
        if 'stats' in first and 'stats' in last:
            delta = {key: last['stats'][key] - first['stats'][key]
                     for key in ('rd_bytes', 'rd_operations')}
            conclusion['boot_read_delta'] = delta
            if any(value < 0 for value in delta.values()):
                failures.append('boot read counters decreased')
    conclusion['status'] = 'FAIL' if failures else 'INCONCLUSIVE' if unknown else 'PASS'
    conclusion['reasons'] = failures + unknown or ['READY, owned exit, immutable inputs and boot mapping established']
    return conclusion


def do_run(env, case):
    runs = RUN / 'runs'
    try:
        os.mkdir(runs, 0o700)
    except FileExistsError:
        check_path(runs)
    case_dir = runs / case
    os.mkdir(case_dir, 0o700)
    with exclusive(case_dir / 'receipt.json') as receipt:
        state = {'run_token': TOKEN, 'experiment_kind': 'input-handoff-v1', 'case': case, 'status': 'IN_PROGRESS',
                 'stage': 'gate', 'attempts': [], **EXPERIMENT_BOUNDARY}
        receipt_save(receipt, state)
        for directory in (RUN, runs, case_dir):
            fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        protocol_fd = None
        try:
            result, build = build_gate()
            if case != 'abort':
                abort_dir = runs / 'abort'
                abort = load_json(abort_dir / 'receipt.json')
                require(abort.get('run_token') == TOKEN and abort.get('case') == 'abort'
                        and abort.get('status') == 'PASS' and abort.get('exit_observed') is True
                        and abort.get('base_unchanged') is True and abort.get('seed_unchanged') is True
                        and abort.get('base_before') == UBUNTU_HASHES[IMAGE]
                        and abort.get('base_after') == UBUNTU_HASHES[IMAGE]
                        and abort.get('build_sha256') == file_hash(RUN / 'build.json'),
                        'abort exit or input evidence missing')
                require(abort.get('experiment_kind') == 'input-handoff-v1' and
                        all(abort.get(key) == value for key, value in EXPERIMENT_BOUNDARY.items()),
                        'abort capability boundary missing')
                require(case_seed_hashes('abort', build['seed']) == abort.get('seed'),
                        'abort seed changed')
                verify_vm_outputs(abort_dir, abort, 'abort')
                diagnostic = diagnostic_report(abort_dir, abort)
                require(diagnostic.get('status') == 'PASS' and diagnostic == abort.get('diagnostic'),
                        'abort owned-exit diagnostic evidence invalid')
            state['stage'] = 'resources'
            receipt_save(receipt, state)
            state['resources'] = vm_resource_gate()
            state['seed'] = snapshot_seed(case, build['seed'])
            state['build_sha256'] = file_hash(RUN / 'build.json')
            base = Path(result['hash_checks']['ubuntu:' + IMAGE]['path'])
            pin = UBUNTU_HASHES[IMAGE]
            require(file_hash(base) == pin, 'base changed before VM')
            state['base_before'] = pin
            state['stage'] = 'disks'
            receipt_save(receipt, state)
            qimg = RUN / 'build/qemu/qemu-img'
            command([str(qimg), 'create', '-f', 'qcow2', '-F', 'qcow2', '-b', str(base),
                     str(case_dir / 'boot.qcow2')], RUN, env, case_dir, 'overlay-create', 60)
            command([str(qimg), 'resize', str(case_dir / 'boot.qcow2'), '16G'],
                    RUN, env, case_dir, 'overlay-resize', 60)
            for name in ('ext.raw', 'btr.raw'):
                with exclusive(case_dir / name) as disk:
                    disk.truncate(8 * 1024**3)
                    disk.flush()
                    os.fsync(disk.fileno())
            state['disks'] = {name: {'path': str(case_dir / name), 'dev': (case_dir / name).stat().st_dev,
                                      'ino': (case_dir / name).stat().st_ino,
                                      'size': (case_dir / name).stat().st_size}
                              for name in ('boot.qcow2', 'ext.raw', 'btr.raw')}
            state['stage'] = 'vm'
            protocol_fd, state['protocol_file'] = create_protocol_file(case_dir / 'protocol.log')
            receipt_save(receipt, state)
            require(case_seed_hashes(case, build['seed']) == state['seed'], 'seed changed before VM')
            def terminate(signum, _frame):
                signal.signal(signum, signal.SIG_IGN)
                raise SystemExit('received signal %d' % signum)
            handlers = {sig: signal.signal(sig, terminate) for sig in (signal.SIGTERM, signal.SIGHUP)}
            try:
                build_gate()
                outcome = vm_attempt(case, 'kvm', expected_vm_argv(case_dir), case_dir, env, receipt, state, protocol_fd)
            finally:
                for sig, handler in handlers.items():
                    signal.signal(sig, handler)
            require(outcome == 'PASS', 'VM attempt failed')
            state['exit_observed'] = state['attempts'][-1]['exit_observed']
            state['base_after'] = file_hash(base)
            state['base_unchanged'] = state['base_after'] == pin
            require(state['base_unchanged'], 'base changed after VM')
            state['seed_unchanged'] = case_seed_hashes(case, build['seed']) == state['seed']
            require(state['seed_unchanged'], 'seed changed during VM')
            data = save_capture_evidence(case_dir, state, protocol_fd)
            bundle = parse_frames(data, case)
            state['preparer_install'] = bundle['installation']
            state['preparer_install_sha256'] = hashlib.sha256(
                preparation.canonical_json(bundle['installation'])).hexdigest()
            state['transport'] = bundle['transport']
            parsed = bundle['result']
            verify_vm_outputs(case_dir, state, case)
            if case == 'matrix':
                write_file(case_dir / 'parsed.json', canonical_json(parsed))
                state['parsed_sha256'] = file_hash(case_dir / 'parsed.json')
                require(parsed['completed'] is True, 'guest returned incomplete matrix')
            if case in READER_FAULT_CASES:
                write_file(case_dir / 'parsed.json', canonical_json(parsed))
                state['parsed_sha256'] = file_hash(case_dir / 'parsed.json')
                require(parsed['outcome'] == 'EXPECTED_UNCERTAIN', 'reader fault expectation failed')
                state['guest_outcome'] = 'UNCERTAIN'
                state['fault_validation'] = 'PASS'
            if case == 'abort':
                require(parsed is None, 'abort unexpectedly emitted a result')
                state['diagnostic'] = diagnostic_report(case_dir, state)
                require(state['diagnostic']['status'] == 'PASS',
                        'diagnostic ' + state['diagnostic']['status'] + ': ' +
                        '; '.join(state['diagnostic']['reasons']))
            state.update(status='PASS', stage='complete')
            try:
                receipt_save(receipt, state)
            except BaseException as persist_error:
                state['persistence_error'] = '%s: %s' % (type(persist_error).__name__, persist_error)
                raise
            return 0
        except BaseException as exc:
            if state.get('attempts'):
                state['exit_observed'] = state['attempts'][-1].get('exit_observed') is True
            if state.get('base_before') and 'base' in locals():
                try:
                    state['base_after'] = file_hash(base)
                    state['base_unchanged'] = state['base_after'] == state['base_before']
                except (OSError, Failure) as evidence_error:
                    state['base_error'] = str(evidence_error)
            if state.get('seed'):
                try:
                    state['seed_unchanged'] = case_seed_hashes(case, build['seed']) == state['seed']
                except (OSError, Failure) as evidence_error:
                    state['seed_unchanged'] = False
                    state['seed_error'] = str(evidence_error)
            if state.get('exit_observed') and protocol_fd is not None:
                try:
                    data = save_capture_evidence(case_dir, state, protocol_fd)
                    bundle = parse_frames(data, case)
                    state['preparer_install'] = bundle['installation']
                    state['preparer_install_sha256'] = hashlib.sha256(
                        preparation.canonical_json(bundle['installation'])).hexdigest()
                    state['transport'] = bundle['transport']
                    if case != 'abort' and not os.path.lexists(case_dir / 'parsed.json'):
                        write_file(case_dir / 'parsed.json', canonical_json(bundle['result']))
                        state['parsed_sha256'] = file_hash(case_dir / 'parsed.json')
                except (OSError, ValueError, Failure, KeyError, TypeError) as evidence_error:
                    state['parse_error'] = str(evidence_error)
            state.update(status='FAIL', error_type=type(exc).__name__, error=str(exc))
            if case == 'abort':
                if not (isinstance(exc, Failure) and
                        state.get('diagnostic', {}).get('status') == 'INCONCLUSIVE'):
                    state['diagnostic_fatal_error'] = '%s: %s' % (type(exc).__name__, exc)
                try:
                    state['diagnostic'] = diagnostic_report(case_dir, state)
                except (OSError, ValueError, Failure, KeyError, TypeError) as evidence_error:
                    state['diagnostic'] = {'status': 'FAIL', 'reasons': [str(evidence_error)]}
            receipt_save(receipt, state)
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            print('PREREQUISITE_FAILED: %s: %s' % (case, exc), file=sys.stderr)
            return 1
        finally:
            if protocol_fd is not None:
                os.close(protocol_fd)


def verify_experiment():
    return verify_handoff.verify(sys.modules[__name__])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('prepare', 'build', 'run', 'verify'))
    parser.add_argument('--run-dir', required=True)
    parser.add_argument('--source-run')
    parser.add_argument('--case', choices=VM_CASES)
    args = parser.parse_args()
    if (args.command == 'run') != (args.case is not None):
        parser.error('--case is required only for run')
    if (args.command == 'prepare') != (args.source_run is not None):
        parser.error('--source-run is required only for prepare')
    try:
        validate_run(args.run_dir)
        if args.command == 'verify':
            return verify()
        env = task_environment()
        if args.command == 'prepare':
            return prepare_import.prepare(sys.modules[__name__], env, args.source_run)
        if args.command == 'build':
            return do_build(env)
        if args.command == 'run':
            return do_run(env, args.case)
        return verify()
    except (Failure, OSError, ValueError) as exc:
        print('PREREQUISITE_FAILED: %s' % exc, file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
