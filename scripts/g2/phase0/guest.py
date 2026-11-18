#!/usr/bin/python3
"""Offline, task-owned guest experiment; never intended for a real host."""
import base64
import errno
import fcntl
import hashlib
import io
import json
import os
import re
import select
import selectors
import signal
import stat
import subprocess
import sys
import tarfile
import time

import handoff
import guest_matrix
import preparation
import prepare_launcher

SEED = '/mnt/g2-seed'
TOKEN_RE = re.compile(r'^[0-9a-f]{32}$')
FILES = {'guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py',
         'probe', 'preparer', 'probe-seccomp.json', 'docker-ce.deb', 'docker-ce-cli.deb',
         'containerd.io.deb', 'case.json', 'meta-data', 'user-data', 'network-config', 'SHA256SUMS'}
PREPARATION_CLIENT = None
PREPARER_SHA256 = None
PREPARER_FD = None
SUPERVISOR = None
READER_TEST_CONTROL = None
RESULT_PORT_NAME = 'org.houfeng.g2.result'
RESULT_PORT = None
PROTOCOL_FAILED = False
READER_FAULT_SCENARIOS = ('before_capture_preparer_death', 'after_capture_preparer_death',
                          'supervisor_fork_death', 'supervisor_launch_death', 'pidfd_open_failure',
                          'reader_timeout', 'reader_nonzero', 'restore_failure')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C', 'LC_ALL': 'C',
       'HOME': '/root', 'DEBIAN_FRONTEND': 'noninteractive'}
SENTINEL = b'G2-READONLY-SENTINEL\n'
LIMIT = 1024 * 1024
CASES = ('C0', 'V1', 'V2', 'V3', 'I0', 'I1', 'I2', 'I3')
PREPARATION_VARIANTS = ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce',
                        'wrong_input_digest', 'extra_fd', 'before_attach', 'after_first_attach')
BOUNDARY = {'mount_namespace_identity': 'UNPROVEN', 'all_overlay_layers': 'UNPROVEN', 'product_gate': 'NOT_READY'}
RESULT = {'experiment_kind': 'input-handoff-v1', 'preparation_version': 1,
          'run_token': '', 'versions': {}, 'isolation': {},
          'completed': False, 'uncertain': False, 'mount_namespace_identity': 'UNPROVEN',
          'all_overlay_layers': 'UNPROVEN', 'product_gate': 'NOT_READY',
          'cases': {case: {p: {'status': 'NOT_RUN', 'observations': dict(BOUNDARY)}
                           for p in ('A', 'B')} for case in CASES},
          'preparation_checks': {variant: {p: {'status': 'NOT_RUN', 'observations': dict(BOUNDARY)}
                                          for p in ('A', 'B')} for variant in PREPARATION_VARIANTS}}

class Failure(Exception):
    pass


class CreateFailure(Failure):
    def __init__(self, message, result):
        super().__init__(message)
        self.result = result


class CoordinatorFailure(Failure):
    def __init__(self, message, evidence=None, create_result=None, incomplete=False, evidence_limit=False):
        super().__init__(message)
        self.evidence = evidence
        self.create_result = create_result
        self.incomplete = incomplete
        self.evidence_limit = evidence_limit



class GuardUnsafe(BaseException):
    """A helper may still be running; keep the coordinator lock until VM shutdown."""
    pass


def check(condition, message):
    if not condition:
        raise Failure(message)


def command(argv, timeout=90, input_data=None, env=None, identity=None, capture_raw=False):
    kw = {'stdin': subprocess.PIPE if input_data is not None else subprocess.DEVNULL,
          'stdout': subprocess.PIPE, 'stderr': subprocess.PIPE, 'env': env or ENV,
          'start_new_session': True}
    if identity:
        kw.update(user=identity[0], group=identity[1], extra_groups=identity[2])
    proc = subprocess.Popen(argv, **kw)
    sel = selectors.DefaultSelector()
    outputs = {proc.stdout: bytearray(), proc.stderr: bytearray()}
    for stream in outputs:
        os.set_blocking(stream.fileno(), False)
        sel.register(stream, selectors.EVENT_READ)
    pending = memoryview(input_data) if input_data is not None else None
    if pending is not None:
        os.set_blocking(proc.stdin.fileno(), False)
        sel.register(proc.stdin, selectors.EVENT_WRITE)
    deadline = time.monotonic() + timeout
    try:
        while sel.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise Failure('deadline exceeded: ' + argv[0])
            for key, _ in sel.select(remaining):
                if key.fileobj is proc.stdin:
                    try:
                        count = os.write(key.fd, pending[:65536])
                        pending = pending[count:]
                    except BrokenPipeError:
                        pending = pending[:0]
                    if not pending:
                        sel.unregister(proc.stdin)
                        proc.stdin.close()
                    continue
                data = os.read(key.fd, 65536)
                if not data:
                    sel.unregister(key.fileobj)
                else:
                    outputs[key.fileobj].extend(data)
                    if sum(map(len, outputs.values())) > LIMIT:
                        raise Failure('output limit exceeded: ' + argv[0])
        rc = proc.wait(timeout=max(0.01, deadline - time.monotonic()))
    except BaseException as exc:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        proc.wait()
        if capture_raw:
            stdout = bytes(outputs[proc.stdout])
            stderr = bytes(outputs[proc.stderr])
            limited = len(stdout) + len(stderr) > LIMIT
            kept_stdout = stdout[:LIMIT]
            kept_stderr = stderr[:max(0, LIMIT - len(kept_stdout))]
            exc.command_evidence = {'stdout_base64': base64.b64encode(kept_stdout).decode('ascii'),
                                    'stderr_base64': base64.b64encode(kept_stderr).decode('ascii'),
                                    'stdout_observed_bytes': len(stdout), 'stderr_observed_bytes': len(stderr),
                                    'incomplete': True, 'evidence_limit': limited}
        raise
    result = {'argv': argv, 'returncode': rc,
              'stdout': bytes(outputs[proc.stdout]).decode('utf-8', 'replace'),
              'stderr': bytes(outputs[proc.stderr]).decode('utf-8', 'replace')}
    if capture_raw:
        result['raw_output'] = {'stdout_base64': base64.b64encode(outputs[proc.stdout]).decode('ascii'),
                                'stderr_base64': base64.b64encode(outputs[proc.stderr]).decode('ascii'),
                                'incomplete': False, 'evidence_limit': False}
    return result


def must(argv, **kwargs):
    result = command(argv, **kwargs)
    check(result['returncode'] == 0, json.dumps(result, ensure_ascii=True))
    return result


def text(path):
    with open(path, 'r', encoding='utf-8') as f:
        return f.read()


def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def _result_port_attribute(name, attribute, limit):
    path = os.path.join('/sys/class/virtio-ports', name, attribute)
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        with os.fdopen(fd, 'rb', closefd=False) as stream:
            value = stream.read(limit + 1)
        check(len(value) <= limit, 'virtio port attribute exceeds limit')
        return value.decode('ascii')
    finally:
        os.close(fd)


def _result_device_identity(info):
    return {'dev': info.st_dev, 'ino': info.st_ino, 'major': os.major(info.st_rdev),
            'minor': os.minor(info.st_rdev), 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode)}


def _check_result_node(fd, path, device):
    opened = os.fstat(fd)
    node = os.lstat(path)
    check(stat.S_ISCHR(opened.st_mode) and stat.S_ISCHR(node.st_mode) and
          _result_device_identity(opened) == device == _result_device_identity(node),
          'result port node identity or permissions changed')


def initialize_result_port():
    global RESULT_PORT
    check(RESULT_PORT is None, 'result port already initialized')
    matches = []
    for name in os.listdir('/sys/class/virtio-ports'):
        if _result_port_attribute(name, 'name', 256) == RESULT_PORT_NAME + '\n':
            check(re.fullmatch(r'vport[0-9]+p[1-9][0-9]*', name) is not None,
                  'invalid result port sysfs name')
            matches.append(name)
    check(len(matches) == 1, 'result port missing or duplicated')
    name = matches[0]
    dev = _result_port_attribute(name, 'dev', 64)
    match = re.fullmatch(r'([0-9]+):([0-9]+)\n', dev)
    check(match is not None, 'invalid result port device number')
    major, minor = int(match[1]), int(match[2])
    check(0 < major <= 0xffffffff and 0 <= minor <= 0xffffffff, 'result port device number out of range')
    path = '/dev/' + name
    fd = os.open(path, os.O_WRONLY | os.O_NONBLOCK | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        opened = os.fstat(fd)
        node = os.lstat(path)
        check(stat.S_ISCHR(opened.st_mode) and stat.S_ISCHR(node.st_mode) and opened.st_uid == 0 and
              opened.st_dev == node.st_dev and opened.st_ino == node.st_ino and
              opened.st_rdev == node.st_rdev and os.major(opened.st_rdev) == major and
              os.minor(opened.st_rdev) == minor, 'result port device identity mismatch')
        os.fchmod(fd, 0o600)
        opened = os.fstat(fd)
        device = _result_device_identity(opened)
        check(device['dev'] >= 0 and device['ino'] > 0 and device['uid'] == 0 and
              0 <= device['gid'] <= 0xffffffff and device['mode'] == 0o600 and
              device['major'] == major and device['minor'] == minor, 'result port permissions mismatch')
        _check_result_node(fd, path, device)
    finally:
        os.close(fd)
    RESULT_PORT = {'kind': 'virtio-serial-file-v1', 'port_name': RESULT_PORT_NAME,
                   'sysfs_name': name, 'device': device}
    return RESULT_PORT


def emit_protocol(line, batch_deadline):
    global PROTOCOL_FAILED
    check(not PROTOCOL_FAILED and RESULT_PORT is not None, 'result port unavailable')
    fd = None
    try:
        data = (line + '\n').encode('ascii')
        check('\n' not in line and '\r' not in line and 0 < len(data) <= 8192,
              'invalid result protocol frame')
        deadline = min(batch_deadline, time.monotonic() + 5)
        check(time.monotonic() < deadline, 'result protocol deadline')
        path = '/dev/' + RESULT_PORT['sysfs_name']
        fd = os.open(path, os.O_WRONLY | os.O_NONBLOCK | os.O_CLOEXEC | os.O_NOFOLLOW)
        _check_result_node(fd, path, RESULT_PORT['device'])
        poller = select.poll()
        poller.register(fd, select.POLLOUT)
        offset = 0
        while offset < len(data):
            remaining = deadline - time.monotonic()
            check(remaining > 0, 'result protocol write deadline')
            try:
                count = os.write(fd, memoryview(data)[offset:])
            except OSError as exc:
                if exc.errno == errno.EINTR:
                    continue
                if exc.errno not in (errno.EAGAIN, errno.EWOULDBLOCK):
                    raise
                try:
                    events = poller.poll(max(1, int(remaining * 1000)))
                except InterruptedError:
                    continue
                check(events and all(not event & (select.POLLERR | select.POLLHUP | select.POLLNVAL)
                                     for _, event in events), 'result port not writable before deadline')
                continue
            check(count > 0, 'zero-length result port write')
            offset += count
        _check_result_node(fd, path, RESULT_PORT['device'])
        check(time.monotonic() <= deadline, 'result protocol write deadline')
    except BaseException as exc:
        PROTOCOL_FAILED = True
        print('G2 result protocol failure: %s: %s' % (type(exc).__name__, str(exc)[:2048]),
              file=sys.stderr, flush=True)
        raise
    finally:
        if fd is not None:
            os.close(fd)


def emit_installation(install, case, deadline):
    token = RESULT['run_token']
    check(type(token) is str and TOKEN_RE.fullmatch(token) is not None, 'invalid installation token')
    payload = preparation.canonical_json({'version': 2, 'run_token': token, 'case': case,
                                          'installation': install, 'transport': RESULT_PORT})
    check(len(payload) <= 4096, 'installation payload exceeds limit')
    line = 'G2LAB-INSTALL %s %s %s' % (token, hashlib.sha256(payload).hexdigest(),
                                      base64.b64encode(payload).decode('ascii'))
    emit_protocol(line, deadline)



def diagnostic_references(value):
    references = []

    def visit(item):
        if len(references) >= 32:
            return
        if isinstance(item, dict):
            if isinstance(item.get('receipt'), str) and isinstance(item.get('rejection'), dict):
                rejection = item['rejection']
                request = item.get('request') or {}
                request = {key: str(request.get(key, ''))[:256] for key in
                           ('id', 'run_token', 'case', 'phase', 'profile')}
                references.append({'receipt': item['receipt'][:256], 'request': request,
                                   'rejection': {'type': str(rejection.get('type', ''))[:256],
                                                 'code': str(rejection.get('code', ''))[:256],
                                                 'message': str(rejection.get('message', ''))[:512]},
                                   'incomplete': True, 'evidence_limit': True})
            else:
                for child in item.values():
                    visit(child)
        elif isinstance(item, list):
            for child in item:
                visit(child)

    visit(value)
    return references

def emit():
    raw = json.dumps(RESULT, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()
    if len(raw) > 4 * 1024 * 1024:
        RESULT['completed'] = False
        RESULT['isolation']['serialization_error'] = 'result exceeded 4 MiB'
        for group in (RESULT['cases'], RESULT['preparation_checks']):
            for profiles in group.values():
                for item in profiles.values():
                    item['status'] = 'INCONCLUSIVE'
                    item['observations'] = {**BOUNDARY, 'reason': 'result_exceeded_limit',
                                            'incomplete': True, 'evidence_limit': True,
                                            'diagnostics': diagnostic_references(item['observations'])}
        raw = json.dumps(RESULT, sort_keys=True, separators=(',', ':')).encode()
        if len(raw) > 4 * 1024 * 1024:
            RESULT['isolation'] = {'serialization_error': 'result exceeded 4 MiB'}
            RESULT['versions'] = {}
            raw = json.dumps(RESULT, sort_keys=True, separators=(',', ':')).encode()
    check(len(raw) <= 4 * 1024 * 1024, 'result exceeds decoded protocol limit')
    deadline = time.monotonic() + 30
    chunks = [raw[i:i + 2048] for i in range(0, len(raw), 2048)]
    for i, chunk in enumerate(chunks):
        emit_protocol('G2LAB-DATA %s %d/%d %s' % (RESULT['run_token'], i, len(chunks),
                                                   base64.b64encode(chunk).decode()), deadline)
    emit_protocol('G2LAB-END %s %s' % (RESULT['run_token'], hashlib.sha256(raw).hexdigest()), deadline)


def seed_check():
    global PREPARER_SHA256
    check(os.geteuid() == 0, 'guest must start as root')
    check(set(os.listdir(SEED)) == FILES, 'unexpected seed entries')
    for name in FILES:
        s = os.lstat(os.path.join(SEED, name))
        check(stat.S_ISREG(s.st_mode), 'non-regular seed entry: ' + name)
    lines = text(SEED + '/SHA256SUMS').splitlines()
    expected = {}
    for line in lines:
        m = re.fullmatch(r'([0-9a-f]{64})  ([A-Za-z0-9._-]+)', line)
        check(m is not None and m[2] in FILES - {'SHA256SUMS'} and m[2] not in expected,
              'invalid seed manifest')
        expected[m[2]] = m[1]
    check(set(expected) == FILES - {'SHA256SUMS'}, 'incomplete seed manifest')
    for name, value in expected.items():
        check(digest(SEED + '/' + name) == value, 'seed digest mismatch: ' + name)
    pins = {'docker-ce.deb': 'b53a44a6ff13277710978adbdbfea27fcdd7ac9f198017e733d2b8dc2343f8dc',
            'docker-ce-cli.deb': '008b3b49b474136063fe0fbe8999b0e77c3e8556d7f7de21f98f05715e2f76ba',
            'containerd.io.deb': '2eb8c6e244fe6886f2fa2eee9ec418c4b9bb44eb44fca748504f57c23341aed2'}
    check(all(expected[name] == sha for name, sha in pins.items()), 'deb pin mismatch')
    for name in ('guest.py', 'handoff.py', 'guest_matrix.py', 'preparation.py', 'prepare_launcher.py'):
        check(digest('/run/g2/' + name) == expected[name], 'bootstrap copy mismatch: ' + name)
    PREPARER_SHA256 = expected['preparer']
    case = json.loads(text(SEED + '/case.json'))
    check(isinstance(case, dict) and set(case) == {'case', 'run_token'} and
          case['case'] in ('abort', 'matrix') + tuple('reader-fault-' + scenario for scenario in READER_FAULT_SCENARIOS) and
          isinstance(case['run_token'], str) and
          TOKEN_RE.fullmatch(case['run_token']), 'invalid case identity')
    RESULT['run_token'] = case['run_token']
    check(text(SEED + '/meta-data') == 'instance-id: g2-%s-%s\nlocal-hostname: g2-lab\n' %
          (case['run_token'], case['case']), 'seed metadata mismatch')
    return case['case']


def mounts():
    return text('/proc/self/mountinfo').splitlines()


def package_preflight():
    pins = {'docker-ce': ('docker-ce.deb', '5:29.8.2-1~ubuntu.24.04~noble'),
            'docker-ce-cli': ('docker-ce-cli.deb', '5:29.8.2-1~ubuntu.24.04~noble'),
            'containerd.io': ('containerd.io.deb', '2.3.6-1~ubuntu.24.04~noble')}
    dpkg_deb = '/usr/bin/dpkg-deb'
    executable = os.stat(dpkg_deb)
    check(stat.S_ISREG(executable.st_mode) and executable.st_uid == 0 and
          not executable.st_mode & 0o022 and os.access(dpkg_deb, os.X_OK), 'unsafe dpkg-deb')
    audit = must(['/usr/bin/dpkg', '--audit'])
    check(not audit['stdout'].strip(), 'dpkg database requires repair')
    fields = {}
    for package, (filename, version) in pins.items():
        path = SEED + '/' + filename
        values = {}
        for field in ('Package', 'Version', 'Architecture', 'Pre-Depends', 'Depends'):
            values[field] = must([dpkg_deb, '-f', path, field])['stdout'].strip()
        check(values['Package'] == package and values['Version'] == version and
              values['Architecture'] == 'amd64', 'deb metadata mismatch: ' + package)
        fields[package] = values
    for package, values in fields.items():
        for field in ('Pre-Depends', 'Depends'):
            for group in filter(None, (x.strip() for x in values[field].split(','))):
                satisfied = False
                for alternative in group.split('|'):
                    m = re.fullmatch(r'\s*([a-z0-9][a-z0-9+.-]*)(?::(?:any|amd64))?\s*(?:\((<<|<=|=|>=|>>)\s*([^()\s]+)\))?\s*', alternative)
                    check(m is not None, 'unsupported deb dependency: ' + alternative)
                    name, op, wanted = m.groups()
                    if name in pins:
                        installed = pins[name][1]
                    else:
                        query = command(['/usr/bin/dpkg-query', '-W', '-f=${db:Status-Status} ${Version}', name])
                        parts = query['stdout'].strip().split(' ', 1)
                        if query['returncode'] != 0 or len(parts) != 2 or parts[0] != 'installed':
                            continue
                        installed = parts[1]
                    if not op or command(['/usr/bin/dpkg', '--compare-versions', installed, op, wanted])['returncode'] == 0:
                        satisfied = True
                        break
                check(satisfied, 'missing dependency for %s: %s' % (package, group))
    RESULT['isolation']['package_preflight'] = fields


def preflight():
    check(os.uname().machine == 'x86_64', 'not amd64')
    release = text('/etc/os-release')
    check(re.search(r'^ID=ubuntu$', release, re.M) and re.search(r'^VERSION_ID="?24\.04"?$', release, re.M), 'not Ubuntu 24.04')
    check(set(os.listdir('/sys/class/net')) == {'lo'}, 'unexpected network interface')
    machine = text('/sys/class/dmi/id/product_name').strip()
    check(machine in ('Standard PC (Q35 + ICH9, 2009)', 'KVM'), 'not expected QEMU guest: ' + machine)
    seed_entries = [x for x in mounts() if x.split()[4] == SEED]
    check(len(seed_entries) == 1, 'seed mount not unique')
    options = set(seed_entries[0].split()[5].split(','))
    check({'ro', 'nosuid', 'nodev', 'noexec'} <= options, 'seed mount flags unsafe')
    check(text(SEED + '/network-config').strip() == 'version: 2\nethernets: {}', 'network config mismatch')
    required = ['/usr/sbin/mkfs.ext4', '/usr/sbin/mkfs.btrfs', '/usr/bin/btrfs',
                '/usr/bin/dpkg', '/usr/sbin/ip', '/usr/bin/mount', '/usr/bin/umount',
                '/usr/bin/python3', '/usr/bin/setpriv', '/usr/bin/systemctl', '/usr/sbin/blkid',
                '/usr/bin/findmnt', '/usr/sbin/blockdev', '/usr/sbin/groupadd', '/usr/sbin/useradd',
                '/usr/sbin/modprobe']
    for path in required:
        s = os.stat(path)
        check(stat.S_ISREG(s.st_mode) and s.st_uid == 0 and not s.st_mode & 0o022 and os.access(path, os.X_OK), 'unsafe or missing executable: ' + path)
    seed_source = seed_entries[0].split(' - ', 1)[1].split()[1]
    check(seed_source.startswith('/dev/') and
          must(['/usr/sbin/blkid', '-s', 'LABEL', '-o', 'value', seed_source])['stdout'].strip() == 'CIDATA',
          'seed source is not CIDATA')
    for fs in ('ext4', 'btrfs', 'overlay'):
        must(['/usr/sbin/modprobe', fs])
    available = text('/proc/filesystems')
    check('ext4' in available and 'btrfs' in available and 'overlay' in available, 'missing kernel filesystems')
    import tempfile
    with tempfile.TemporaryDirectory(prefix='g2-overlay-preflight-', dir='/run') as temporary:
        for name in ('lower', 'upper', 'work', 'merged'):
            os.mkdir(temporary + '/' + name)
        opts = 'lowerdir=%s/lower,upperdir=%s/upper,workdir=%s/work' % (temporary, temporary, temporary)
        must(['/usr/bin/mount', '-t', 'overlay', 'overlay', '-o', opts, temporary + '/merged'])
        try:
            actual = [x for x in mounts() if x.split()[4] == temporary + '/merged']
            check(len(actual) == 1 and actual[0].split(' - ', 1)[1].split()[0] == 'overlay',
                  'overlay preflight mount not observed')
            RESULT['isolation']['overlay_preflight_mount'] = actual[0]
        finally:
            must(['/usr/bin/umount', temporary + '/merged'])
    check(not os.path.lexists('/lab') and not os.path.lexists('/var/run/docker.sock') and
          not os.path.lexists('/usr/sbin/policy-rc.d') and not os.path.lexists('/etc/docker/daemon.json'),
          'existing lab or Docker configuration/socket')
    for package in ('docker-ce', 'docker-ce-cli', 'docker.io', 'docker-compose',
                    'docker-compose-v2', 'containerd', 'containerd.io', 'runc'):
        found = command(['/usr/bin/dpkg-query', '-W', '-f=${db:Status-Status}', package])
        check(found['returncode'] != 0 or found['stdout'].strip() == 'not-installed',
              'conflicting package: ' + package)
    package_preflight()
    devs = {}
    root_dev = os.stat('/').st_dev
    seed_dev = os.stat(SEED).st_dev
    mount_devices = {x.split()[2] for x in mounts()}
    for key in ('EXT', 'BTR'):
        link = '/dev/disk/by-id/virtio-G2' + key
        target = os.path.realpath(link)
        s = os.stat(link)
        check(stat.S_ISBLK(s.st_mode), 'not a block device: ' + link)
        major_minor = '%d:%d' % (os.major(s.st_rdev), os.minor(s.st_rdev))
        check(s.st_rdev not in (root_dev, seed_dev) and major_minor not in mount_devices, 'disk already mounted/root/seed')
        check(text('/sys/dev/block/' + major_minor + '/serial').strip() == 'G2' + key, 'disk serial mismatch')
        check(must(['/usr/sbin/blockdev', '--getsize64', link])['stdout'].strip() == str(8 * 1024**3), 'disk size mismatch')
        probe = command(['/usr/sbin/blkid', '-p', link])
        check(probe['returncode'] == 2 and not probe['stdout'].strip(), 'disk not blank: ' + json.dumps(probe))
        check(not os.path.exists('/sys/dev/block/' + major_minor + '/partition'), 'disk is a partition')
        check(not any(os.path.exists('/sys/class/block/' + name + '/partition') and
                      os.path.realpath('/sys/class/block/' + name).startswith(
                          os.path.realpath('/sys/dev/block/' + major_minor) + '/')
                      for name in os.listdir('/sys/class/block')), 'disk has partitions')
        devs[key] = {'path': link, 'target': target, 'rdev': s.st_rdev, 'major_minor': major_minor}
    check(devs['EXT']['rdev'] != devs['BTR']['rdev'], 'duplicate disks')
    RESULT['versions']['kernel'] = os.uname().release
    RESULT['isolation'].update({'machine': machine, 'network_interfaces': ['lo'], 'seed_mount': seed_entries[0], 'disks': devs,
                                'fixture_mntns': os.readlink('/proc/self/ns/mnt'), 'fixture_pidns': os.readlink('/proc/self/ns/pid')})
    RESULT['isolation']['fixture_namespaces'] = {
        name: {'dev': os.stat('/proc/self/ns/' + name).st_dev, 'ino': os.stat('/proc/self/ns/' + name).st_ino}
        for name in ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup')}
    return devs

def setup_disks(devs):
    for path in ('/lab', '/lab/ext', '/lab/btr', '/lab/control', '/lab/overlay-test',
                 '/lab/overlay-test/lower', '/lab/overlay-test/upper',
                 '/lab/overlay-test/work', '/lab/overlay-test/merged'):
        os.makedirs(path, mode=0o700, exist_ok=True)
    os.chmod('/lab', 0o755)
    # All device identities are checked again immediately before formatting.
    for kind, fs in (('EXT', 'ext4'), ('BTR', 'btrfs')):
        dev = devs[kind]
        s = os.stat(dev['path'])
        check(stat.S_ISBLK(s.st_mode) and s.st_rdev == dev['rdev'], 'disk identity changed')
        check(os.path.realpath(dev['path']) == dev['target'] and
              text('/sys/dev/block/' + dev['major_minor'] + '/serial').strip() == 'G2' + kind and
              must(['/usr/sbin/blockdev', '--getsize64', dev['path']])['stdout'].strip() == str(8 * 1024**3) and
              dev['major_minor'] not in {x.split()[2] for x in mounts()} and
              s.st_rdev not in (os.stat('/').st_dev, os.stat(SEED).st_dev),
              'disk changed or became mounted before mkfs')
        check(not any(os.path.exists('/sys/class/block/' + name + '/partition') and
                      os.path.realpath('/sys/class/block/' + name).startswith(
                          os.path.realpath('/sys/dev/block/' + dev['major_minor']) + '/')
                      for name in os.listdir('/sys/class/block')), 'disk acquired partitions before mkfs')
        check(command(['/usr/sbin/blkid', '-p', dev['path']])['returncode'] == 2, 'disk changed before mkfs')
        must(['/usr/sbin/mkfs.' + fs, '-F' if fs == 'ext4' else '-f', dev['path']], timeout=180)
        must(['/usr/bin/mount', '-t', fs, dev['path'], '/lab/' + kind.lower()])
        check(any(x.split()[4] == '/lab/' + kind.lower() and
                  x.split(' - ', 1)[1].split()[0] == fs for x in mounts()),
              'formatted filesystem was not mounted')
    must(['/usr/bin/mount', '-t', 'overlay', 'overlay', '-o',
          'lowerdir=/lab/overlay-test/lower,upperdir=/lab/overlay-test/upper,workdir=/lab/overlay-test/work',
          '/lab/overlay-test/merged'])
    check(any(x.split()[4] == '/lab/overlay-test/merged' and
              x.split(' - ', 1)[1].split()[0] == 'overlay' for x in mounts()),
          'overlay mount failed')
    must(['/usr/bin/umount', '/lab/overlay-test/merged'])


def finish_log_collector(pid):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            check(os.waitstatus_to_exitcode(status) == 0, 'daemon log collector failed')
            check(not os.path.exists('/run/g2-dockerd.log.overlimit'), 'daemon output exceeded limit')
            return
        time.sleep(0.05)
    os.kill(pid, signal.SIGKILL)
    os.waitpid(pid, 0)
    raise Failure('daemon log collector did not exit')


def install_and_start():
    # Reject existing packages and daemon configuration; never alter an existing installation.
    for package in ('docker-ce', 'docker-ce-cli', 'docker.io', 'docker-compose',
                    'docker-compose-v2', 'containerd', 'containerd.io', 'runc'):
        r = command(['/usr/bin/dpkg-query', '-W', '-f=${db:Status-Status}', package])
        check(r['returncode'] != 0 or r['stdout'].strip() == 'not-installed',
              'conflicting package: ' + package)
    check(not os.path.exists('/usr/sbin/policy-rc.d'), 'policy-rc.d already exists')
    check(not os.path.exists('/etc/docker/daemon.json'), 'existing daemon config')
    with open('/usr/sbin/policy-rc.d', 'x', encoding='ascii') as f:
        f.write('#!/bin/sh\nexit 101\n')
    os.chmod('/usr/sbin/policy-rc.d', 0o755)
    must(['/usr/bin/systemctl', 'mask', 'docker.service', 'docker.socket', 'containerd.service'])
    install = must(['/usr/bin/dpkg', '-i', SEED + '/containerd.io.deb',
                    SEED + '/docker-ce-cli.deb', SEED + '/docker-ce.deb'], timeout=300)
    RESULT['isolation']['dpkg_install'] = install
    expected_packages = {'docker-ce': '5:29.8.2-1~ubuntu.24.04~noble',
                         'docker-ce-cli': '5:29.8.2-1~ubuntu.24.04~noble',
                         'containerd.io': '2.3.6-1~ubuntu.24.04~noble'}
    for package, version in expected_packages.items():
        installed = must(['/usr/bin/dpkg-query', '-W', '-f=${db:Status-Status} ${Version} ${Architecture}', package])
        check(installed['stdout'] == 'installed ' + version + ' amd64', 'installed package identity mismatch: ' + package)
    for binary in ('/usr/bin/docker', '/usr/bin/dockerd', '/usr/bin/containerd', '/usr/bin/runc'):
        s = os.stat(binary)
        check(stat.S_ISREG(s.st_mode) and s.st_uid == 0 and not s.st_mode & 0o022 and
              os.access(binary, os.X_OK), 'unsafe installed executable: ' + binary)
    import grp
    import pwd
    try:
        grp.getgrnam('g2docker')
        raise Failure('g2docker already exists')
    except KeyError:
        pass
    try:
        pwd.getpwuid(10002)
        raise Failure('UID 10002 already exists')
    except KeyError:
        pass
    must(['/usr/sbin/groupadd', '--gid', '10002', 'g2coord'])
    must(['/usr/sbin/groupadd', 'g2docker'])
    docker_gid = grp.getgrnam('g2docker').gr_gid
    must(['/usr/sbin/useradd', '--uid', '10002', '--gid', '10002', '--no-create-home',
          '--shell', '/usr/sbin/nologin', 'g2coord'])
    os.chown('/lab/control', 10002, 10002)
    os.chmod('/lab/control', 0o700)
    with open('/run/g2-daemon.json', 'x', encoding='ascii') as f:
        f.write('{\"features\":{\"containerd-snapshotter\":false}}')
    args = ['/usr/bin/dockerd', '--host=unix:///var/run/docker.sock', '--group=g2docker',
            '--data-root=/lab/ext/docker', '--exec-root=/run/g2-docker', '--storage-driver=overlay2',
            '--bridge=none', '--iptables=false', '--ip6tables=false', '--ip-forward=false',
            '--ip-masq=false', '--config-file=/run/g2-daemon.json']
    read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
    logger = os.fork()
    if logger == 0:
        try:
            os.close(write_fd)
            if READER_TEST_CONTROL is not None:
                READER_TEST_CONTROL[0].close()
            size = 0
            overflow = False
            with open('/run/g2-dockerd.log', 'xb', buffering=0) as out:
                while True:
                    chunk = os.read(read_fd, 65536)
                    if not chunk:
                        break
                    if size < LIMIT:
                        out.write(chunk[:LIMIT - size])
                    size += len(chunk)
                    if size > LIMIT and not overflow:
                        with open('/run/g2-dockerd.log.overlimit', 'x', encoding='ascii') as flag:
                            flag.write('daemon output exceeded limit\n')
                        overflow = True
            os._exit(0)
        except BaseException:
            os._exit(1)
    os.close(read_fd)
    try:
        daemon = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=write_fd, stderr=write_fd,
                                  env=ENV, start_new_session=True)
    except BaseException:
        os.close(write_fd)
        finish_log_collector(logger)
        raise
    os.close(write_fd)
    try:
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline and not os.path.exists('/var/run/docker.sock'):
            check(daemon.poll() is None, 'dockerd exited before socket')
            time.sleep(0.1)
        check(os.path.exists('/var/run/docker.sock'), 'dockerd socket timeout')
        ds = os.stat('/var/run/docker.sock')
        check(stat.S_ISSOCK(ds.st_mode) and ds.st_uid == 0 and ds.st_gid == docker_gid and
              not ds.st_mode & 0o007, 'unsafe daemon socket')
        for ns in ('mnt', 'pid'):
            check(os.stat('/proc/self/ns/' + ns).st_ino == os.stat('/proc/%d/ns/%s' % (daemon.pid, ns)).st_ino,
                  'dockerd namespace differs: ' + ns)
        RESULT['isolation']['dockerd'] = {'pid': daemon.pid, 'argv': args,
                                          'mntns': os.readlink('/proc/%d/ns/mnt' % daemon.pid),
                                          'pidns': os.readlink('/proc/%d/ns/pid' % daemon.pid)}
        RESULT['versions']['runc'] = must(['/usr/bin/runc', '--version'])['stdout']
        return docker_gid, daemon, logger
    except BaseException:
        if daemon.poll() is None:
            daemon.terminate()
            try:
                daemon.wait(timeout=20)
            except subprocess.TimeoutExpired:
                daemon.kill()
                daemon.wait()
        finish_log_collector(logger)
        raise


def bounded_create_result(result):
    limit = 16 * 1024
    return {'returncode': result['returncode'],
            'stdout': result['stdout'][:limit], 'stderr': result['stderr'][:limit],
            'stdout_truncated': len(result['stdout']) > limit,
            'stderr_truncated': len(result['stderr']) > limit,
            'argv': [arg[:1024] for arg in result['argv'][:128]],
            'argv_truncated': len(result['argv']) > 128 or any(len(arg) > 1024 for arg in result['argv'])}


def create_receipt_id(result):
    check(result['returncode'] == 0 and re.fullmatch(r'[0-9a-f]{64}\n?', result['stdout']),
          'create did not return full ID')
    return result['stdout'].strip()






def coordinator(gid):
    global PREPARATION_CLIENT, SUPERVISOR
    import select
    import socket
    parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
    launch_parent, launch_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    launch_parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
    launch_child.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
    session_nonce = os.urandom(32).hex()
    parent_pid = os.getpid()
    pid = os.fork()
    if pid == 0:
        parent.close()
        launch_parent.close()
        os.close(PREPARER_FD)
        if READER_TEST_CONTROL is not None:
            READER_TEST_CONTROL[0].close()
        guard_possible = False
        clean_retirement = False
        try:
            os.setgroups([gid])
            os.setgid(10002)
            os.setuid(10002)
            lock = os.open('/lab/control/guard', os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW, 0o600)
            guard_possible = True
            fcntl.flock(lock, fcntl.LOCK_EX)

            def serve_locked():
                global PREPARATION_CLIENT
                s = os.fstat(lock)
                check(s.st_uid == 10002 and stat.S_IMODE(s.st_mode) == 0o600, 'unsafe guard')
                status = text('/proc/self/status')
                check(os.geteuid() == 10002 and os.getegid() == 10002 and os.getgroups() == [gid] and
                      re.search(r'^CapEff:\s*0+$', status, re.M), 'coordinator identity/capabilities')
                guard = {'dev': s.st_dev, 'ino': s.st_ino, 'uid': s.st_uid, 'mode': stat.S_IMODE(s.st_mode)}
                stream = child.makefile('rwb', buffering=0)
                owner = {'ready': False, 'status': status, 'groups': os.getgroups(), 'guard': guard}
                stream.write((json.dumps(owner, sort_keys=True, separators=(',', ':')) + '\n').encode())
                client = preparation.CoordinatorLaunch(launch_child, RESULT['run_token'], session_nonce, parent_pid)
                client.register_guard(lock, guard)
                PREPARATION_CLIENT = client
                stream.write((json.dumps({**owner, 'ready': True}, sort_keys=True,
                                         separators=(',', ':')) + '\n').encode())
                return handoff.serve(sys.modules[__name__], stream, lock)

            clean_retirement = handoff.guarded_call(serve_locked) is True
        except BaseException:
            if guard_possible:
                handoff.retain_guard()
        finally:
            if guard_possible and not clean_retirement:
                handoff.retain_guard()
            try:
                launch_child.close()
                child.close()
            finally:
                os._exit(0 if clean_retirement else 1)
    child.close()
    launch_child.close()
    parent.setblocking(False)
    pidfd = os.pidfd_open(pid, 0)
    SUPERVISOR = prepare_launcher.RootSupervisor(launch_parent, pid, RESULT['run_token'], session_nonce,
                                                 PREPARER_FD, RESULT['isolation']['preparer_install'],
                                                 test_control=READER_TEST_CONTROL)
    deadline = time.monotonic() + 5
    buffer = bytearray()
    owner = None
    accepted = False
    try:
        while True:
            remaining = deadline - time.monotonic()
            check(remaining > 0, 'coordinator guard handshake timeout')
            readable, _, _ = select.select([parent, launch_parent, pidfd], [], [], remaining)
            check(readable, 'coordinator guard handshake timeout')
            if launch_parent in readable:
                SUPERVISOR.receive_guard()
            if parent in readable:
                chunk = parent.recv(65536)
                check(chunk, 'coordinator handshake ended')
                buffer.extend(chunk)
                check(len(buffer) <= 1024 * 1024, 'coordinator handshake too large')
                while b'\n' in buffer:
                    line, _, rest = buffer.partition(b'\n')
                    buffer = bytearray(rest)
                    hello = handoff.strict_json(line)
                    check(isinstance(hello, dict) and set(hello) == {'ready', 'status', 'groups', 'guard'},
                          'coordinator handshake fields')
                    if owner is None:
                        check(hello['ready'] is False and hello['groups'] == [gid], 'invalid guard owner')
                        status = text('/proc/%d/status' % pid)
                        for field, pattern in (('Uid', r'10002\s+10002\s+10002\s+10002'),
                                               ('Gid', r'10002\s+10002\s+10002\s+10002'),
                                               ('CapEff', r'0+')):
                            check(re.search(r'^' + field + r':\s*' + pattern + r'\s*$', status, re.M) and
                                  re.search(r'^' + field + r':\s*' + pattern + r'\s*$', hello['status'], re.M),
                                  'coordinator status mismatch: ' + field)
                        owner = hello
                    else:
                        check(accepted and hello == {**owner, 'ready': True} and not buffer,
                              'coordinator readiness mismatch')
                        check(not select.select([pidfd], [], [], 0)[0], 'coordinator exited during handshake')
                        RESULT['isolation']['coordinator'] = {**hello, 'session_nonce': session_nonce,
                                                              'guard_registered_before_ready': True}
                        return pid, parent, pidfd, SUPERVISOR
            if pidfd in readable:
                raise GuardUnsafe('coordinator exited during guard handshake')
            if owner is not None and SUPERVISOR.guard.registration is not None and not accepted:
                SUPERVISOR.accept_guard(owner)
                accepted = True
    except BaseException as exc:
        raise GuardUnsafe('coordinator guard registration uncertain: ' + str(exc)) from exc


class Docker:
    def __init__(self, sock, supervisor, pidfd, pid):
        self.sock = sock
        self.supervisor = supervisor
        self.pidfd = pidfd
        self.pid = pid

    def exchange(self, request, timeout=300):
        import select
        import socket
        pending = memoryview((json.dumps(request, sort_keys=True, separators=(',', ':')) + '\n').encode())
        if len(pending) > 32 * 1024 * 1024:
            raise GuardUnsafe('coordinator request exceeds bound')
        response = bytearray()
        launch_eof = False
        deadline = time.monotonic() + timeout
        try:
            while True:
                self.supervisor.expire()
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise ValueError('coordinator RPC deadline exceeded')
                prep_remaining = self.supervisor.time_left()
                if prep_remaining is not None:
                    remaining = min(remaining, prep_remaining if prep_remaining > 0 else 0.1)
                watched = [self.sock, self.pidfd]
                if not launch_eof:
                    watched.append(self.supervisor.sock)
                active = self.supervisor.active
                if active is not None and active.get('pidfd') is not None and not active.get('exit_seen'):
                    watched.append(active['pidfd'])
                progress = self.supervisor.progress_socket()
                if progress is not None:
                    watched.append(progress)
                extra_read, extra_write = self.supervisor.event_sources()
                watched.extend(extra_read)
                to_write = ([self.sock] if pending else []) + extra_write
                readable, writable, _ = select.select(watched, to_write, [], remaining)
                # A readable ACK cannot overtake an expired deadline or peer loss.
                self.supervisor.expire()
                if time.monotonic() >= deadline:
                    raise ValueError('coordinator RPC deadline exceeded')
                if self.pidfd in readable and not (request.get('op') == 'finish' and
                                                   self.supervisor.retired):
                    raise ValueError('coordinator exited before retirement')
                active = self.supervisor.active
                if (active is not None and active.get('pidfd') is not None and
                        active['pidfd'] in readable):
                    active['exit_seen'] = True
                    if self.supervisor.reader is not None or self.supervisor.pending_capture is not None:
                        raise ValueError('preparer exited while reader ownership was active')
                if self.sock in readable and not self.sock.recv(1, socket.MSG_PEEK | socket.MSG_DONTWAIT):
                    raise ValueError('coordinator response ended')
                if self.sock in writable:
                    count = self.sock.send(pending[:65536])
                    if count <= 0:
                        raise ValueError('coordinator request write failed')
                    pending = pending[count:]
                if self.supervisor.sock in readable:
                    if self.supervisor.retired and request.get('op') == 'finish':
                        preparation.require_result_eof(self.supervisor.sock, deadline)
                        launch_eof = True
                    else:
                        self.supervisor.handle()
                self.supervisor.dispatch(readable, writable)
                if progress is not None and progress in readable:
                    self.supervisor.handle_progress()
                active = self.supervisor.active
                if active is not None and active.get('pidfd') is not None:
                    if (active.get('exit_seen') and active.get('capture_eof') and
                            not active['capture_out'] and not self.supervisor.control_out):
                        self.supervisor.reap()
                if self.sock in readable or (self.pidfd in readable and request.get('op') == 'finish' and
                                             self.supervisor.retired):
                    data = self.sock.recv(65536)
                    if not data:
                        raise ValueError('coordinator response ended')
                    response.extend(data)
                    if len(response) > 4 * 1024 * 1024:
                        raise ValueError('coordinator response exceeds bound')
                    if b'\n' in response:
                        line, _, rest = response.partition(b'\n')
                        if (rest or pending or self.supervisor.active is not None or
                                self.supervisor.reader is not None or self.supervisor.pending_capture is not None or
                                self.supervisor.control_out):
                            raise ValueError('unexpected coordinator response or pending preparer')
                        result = handoff.strict_json(line)
                        if not isinstance(result, dict):
                            raise ValueError('coordinator response is not an object')
                        if self.pidfd in readable and request.get('op') != 'finish':
                            raise ValueError('coordinator exited before retirement')
                        return result
                if self.pidfd in readable:
                    raise ValueError('coordinator exited without response')
        except BaseException as exc:
            try:
                signal.pidfd_send_signal(self.pidfd, signal.SIGKILL)
            except ProcessLookupError:
                pass
            except OSError:
                pass
            try:
                self.supervisor.contain()
            except BaseException as containment_error:
                self.supervisor.uncertain = True
                raise GuardUnsafe('coordinator ownership and containment uncertain: %s; %s' %
                                  (exc, containment_error)) from exc
            raise GuardUnsafe('coordinator communication or ownership uncertain: ' + str(exc)) from exc

    def rpc(self, request):
        response = self.exchange(request)
        if response.get('uncertain') is True:
            exc = GuardUnsafe(response.get('error', 'coordinator ownership uncertain'))
            exc.incomplete = response.get('incomplete') is True
            exc.evidence_limit = response.get('evidence_limit') is True
            raise exc
        if 'error' in response:
            evidence = response.get('evidence')
            if not isinstance(response['error'], str) or (evidence is not None and not isinstance(evidence, dict)):
                raise GuardUnsafe('invalid coordinator rejection')
            raise CoordinatorFailure('coordinator: ' + response['error'], evidence, response.get('create_result'),
                                     response.get('incomplete') is True or (evidence or {}).get('incomplete') is True,
                                     response.get('evidence_limit') is True or (evidence or {}).get('evidence_limit') is True)
        if set(response) != {'result'}:
            raise GuardUnsafe('invalid coordinator response')
        return response['result']

    def finish(self):
        import select
        try:
            ack = self.exchange({'op': 'finish'}, timeout=30)
            check(ack == {'finished': True} and self.supervisor.retired and self.supervisor.active is None,
                  'coordinator retirement acknowledgement missing')
            check(bool(select.select([self.pidfd], [], [], 5)[0]), 'coordinator exit missing')
            waited, status = os.waitpid(self.pid, 0)
            check(waited == self.pid and os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0,
                  'coordinator exit was not clean')
            preparation.require_result_eof(self.supervisor.sock, time.monotonic() + 5)
            self.supervisor.release_guard(self.pidfd, status)
            RESULT['isolation']['coordinator_retirement'] = {
                'pid': self.pid, 'wait_status': status, 'ledger_sha256': self.supervisor.retired_ledger,
                'readback': self.supervisor.retirement_readback,
                'preparer_launches': len(self.supervisor.launches), 'guard_released_after_exit': True}
            os.close(self.pidfd)
            self.supervisor.sock.close()
            self.sock.close()
        except BaseException as exc:
            raise GuardUnsafe('coordinator retirement uncertain: ' + str(exc)) from exc

    def run(self, args, data=None, receipt=None, timeout=90, required=True):
        req = {'op': 'docker', 'argv': args, 'timeout': timeout}
        if data is not None:
            req['input'] = base64.b64encode(data).decode()
        if receipt is not None:
            req['receipt'] = receipt
        r = self.rpc(req)
        if required:
            check(r['returncode'] == 0, json.dumps(r))
        return r

    def inspect(self, cid):
        result = json.loads(self.run(['inspect', cid])['stdout'])
        check(len(result) == 1 and result[0]['Id'] == cid, 'inspect identity mismatch')
        return result[0]

    def create(self, name, image, options, args, case, phase, metadata='none', exe=False, nested=False,
               prepare_nonce=None, prepare_input_digest=None):
        prepared = bool(args and args[0] in ('prepared-observe', 'prepared-denycheck'))
        if prepared:
            check(type(prepare_nonce) is str and re.fullmatch(r'[0-9a-f]{64}', prepare_nonce) and
                  type(prepare_input_digest) is str and re.fullmatch(r'[0-9a-f]{64}', prepare_input_digest) and
                  options.count('--interactive') == 1, 'prepared create binding missing')
        else:
            check(prepare_nonce is None and prepare_input_digest is None, 'unexpected prepared create binding')
        token = os.urandom(32).hex()
        name += '-' + token
        labels = ['--label', 'owner=g2-isolated-capability', '--label', 'run=' + RESULT['run_token'],
                  '--label', 'runner=guest.py', '--label', 'create-token=' + token]
        envs = ['--env', 'G2_CASE=' + case, '--env', 'G2_PHASE=' + phase,
                '--env', 'G2_METADATA=' + metadata, '--env', 'G2_METADATA_EXE=' + ('1' if exe else '0'),
                '--env', 'G2_NESTED=' + ('1' if nested else '0')]
        if prepared:
            envs += ['--env', 'G2_PREPARE_NONCE=' + prepare_nonce,
                     '--env', 'G2_PREPARE_INPUT_DIGEST=' + prepare_input_digest]
        spec = {'name': name, 'image': image, 'options': options, 'args': args, 'token': token,
                'labels': labels, 'environment': envs}
        r = self.run(['create', '--name', name] + labels + envs + options +
                     ['--entrypoint', '/probe', image] + args, receipt=spec)
        cid = r['stdout'].strip()
        inspect = self.inspect(cid)
        config = inspect['Config']
        check(inspect['Name'] == '/' + name and inspect['Image'] == image and
              config['Cmd'] == args and config['Entrypoint'] == ['/probe'] and config['User'] == '0:0' and
              config['Labels'].get('owner') == 'g2-isolated-capability' and
              config['Labels'].get('run') == RESULT['run_token'] and
              config['Labels'].get('runner') == 'guest.py' and config['Labels'].get('create-token') == token and
              set(envs[1::2]) <= set(config['Env']) and
              inspect['HostConfig']['NetworkMode'] == 'none', 'created spec mismatch')
        if prepared:
            check(config.get('OpenStdin') is True and config.get('AttachStdin') is True and
                  config.get('AttachStdout') is True and config.get('AttachStderr') is True and
                  config.get('StdinOnce') is True and config.get('Tty') is False, 'prepared attach configuration')
        return cid


def bind(source, target):
    os.makedirs(target, exist_ok=True)
    must(['/usr/bin/mount', '--bind', source, target])


def stat_identity(path):
    s = os.stat(path)
    return {'dev': s.st_dev, 'ino': s.st_ino, 'mode': s.st_mode, 'size': s.st_size}


def mountinfo_evidence(path, paths):
    lines, size, truncated = [], 0, False
    for line in text(path).splitlines():
        fields = line.split()
        if len(fields) < 6:
            continue
        point = fields[4]
        if not any(point == '/' or point == target or target.startswith(point + '/') or
                   point.startswith(target + '/') for target in paths):
            continue
        remaining = 32768 - size
        if remaining <= 0 or len(lines) >= 64:
            truncated = True
            continue
        retained = line[:min(4096, remaining)]
        truncated |= len(retained) != len(line)
        lines.append(retained)
        size += len(retained)
    return {'lines': lines, 'truncated': truncated}


def held_alias(source, target):
    bind(source, target)
    before = mountinfo_evidence('/proc/self/mountinfo', [source, target])
    must(['/usr/bin/mount', '--make-private', target])
    after = mountinfo_evidence('/proc/self/mountinfo', [source, target])
    return {'before_private': before, 'after_private': after}


def held_truth(source, alias, pid, inside):
    return {'source': stat_identity(source), 'old': stat_identity(alias),
            'actual': stat_identity('/proc/%d/root%s' % (pid, inside if inside != '/' else '')),
            'fixture_mountinfo': mountinfo_evidence('/proc/self/mountinfo', [source, alias]),
            'consumer_mountinfo': mountinfo_evidence('/proc/%d/mountinfo' % pid, [inside])}


def check_held_fixture(before_cover, after_cover, consumer_is_original):
    check(before_cover['source'] == before_cover['old'] and after_cover['old'] == before_cover['old'] and
          after_cover['source']['dev'] != before_cover['old']['dev'], 'held fixture object changed')
    if consumer_is_original:
        check(before_cover['actual'] == before_cover['old'] and after_cover['actual'] == before_cover['old'],
              'consumer did not hold the original object')




def image_import(docker):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode='w') as archive:
        data = open(SEED + '/probe', 'rb').read()
        entry = tarfile.TarInfo('probe')
        entry.size = len(data)
        entry.mode = 0o755
        entry.uid = entry.gid = 0
        archive.addfile(entry, io.BytesIO(data))
        for path in ['roots', 'meta'] + ['roots/%03d' % index for index in range(16)]:
            directory = tarfile.TarInfo(path)
            directory.type = tarfile.DIRTYPE
            directory.mode = 0o755
            archive.addfile(directory)
        entry = tarfile.TarInfo('meta/target-exe')
        entry.size = 0
        entry.mode = 0o644
        archive.addfile(entry, io.BytesIO(b''))
        marker = b'g2-image-nocopy-marker\n'
        entry = tarfile.TarInfo('roots/000/g2-image-marker')
        entry.size = len(marker)
        entry.mode = 0o644
        archive.addfile(entry, io.BytesIO(marker))
    image_tar = buf.getvalue()
    result = docker.run(['image', 'import', '-'], data=image_tar, timeout=120)
    image = result['stdout'].strip()
    check(re.fullmatch(r'sha256:[0-9a-f]{64}', image), 'import did not return full image ID')
    RESULT['isolation']['image_fixture'] = {'image_id': image, 'archive_sha256': hashlib.sha256(image_tar).hexdigest(),
                                            'marker_path': '/roots/000/g2-image-marker',
                                            'marker_sha256': hashlib.sha256(marker).hexdigest()}
    return image


def base_options():
    return ['--user', '0:0', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
            '--network', 'none', '--read-only', '--pids-limit', '64', '--memory', '256m',
            '--memory-swap', '256m', '--cpus', '1', '--log-driver', 'none',
            '--ipc', 'private', '--cgroupns', 'private']


def root_mount(source, target, readonly=True):
    value = 'type=bind,source=%s,target=%s' % (source, target)
    if readonly:
        value += ',readonly,bind-recursive=readonly,bind-propagation=rprivate'
    else:
        value += ',bind-propagation=rprivate'
    return ['--mount', value]


def container_truth(docker, cid):
    inspect = docker.inspect(cid)
    state = inspect['State']
    return {'id': inspect['Id'], 'pid': state['Pid'], 'running': state['Running'],
            'status': state['Status'], 'started_at': state['StartedAt'], 'inspect': inspect}


def consumer(docker, image, name, case, mounts_args, start=True):
    options = base_options() + mounts_args
    cid = docker.create(name, image, options, ['hold'], case, 'before')
    if start:
        docker.run(['start', cid])
    return cid



def main():
    global PREPARER_FD
    docker = None
    daemon = None
    logger = None
    case = None
    protocol_ready = False
    fault_emitted = False
    try:
        case = seed_check()
        initialize_result_port()
        PREPARER_FD, install = prepare_launcher.install_preparer(SEED, PREPARER_SHA256)
        RESULT['isolation']['preparer_install'] = install
        devs = preflight()
        startup_deadline = time.monotonic() + 30
        emit_installation(install, case, startup_deadline)
        emit_protocol('G2LAB-READY ' + RESULT['run_token'], startup_deadline)
        protocol_ready = True
        if case.startswith('reader-fault-'):
            fault_emitted = True
            guest_matrix.run_reader_fault_vm(sys.modules[__name__], case, devs)
            return
        if case == 'abort':
            while True:
                signal.pause()
        setup_disks(devs)
        gid, daemon, logger = install_and_start()
        coordinator_pid, sock, pidfd, supervisor = coordinator(gid)
        docker = Docker(sock, supervisor, pidfd, coordinator_pid)
        guest_matrix.run(sys.modules[__name__], docker)
        RESULT['completed'] = True
    except BaseException as exc:
        print('G2 guest failure: %s: %s' % (type(exc).__name__, str(exc)[:2048]),
              file=sys.stderr, flush=True)
        if isinstance(exc, GuardUnsafe):
            RESULT['uncertain'] = True
            docker = None
        RESULT['isolation']['error'] = '%s: %s' % (type(exc).__name__, exc)
        if getattr(exc, 'incomplete', False) or getattr(exc, 'evidence_limit', False):
            RESULT['isolation']['error_incomplete'] = True
            RESULT['isolation']['evidence_limit'] = bool(getattr(exc, 'evidence_limit', False))
        if isinstance(exc, CreateFailure):
            RESULT['isolation']['create_result'] = exc.result
    finally:
        if docker is not None and not RESULT['uncertain']:
            try:
                docker.finish()
            except BaseException as exc:
                RESULT['completed'] = False
                RESULT['uncertain'] = True
                RESULT['isolation']['coordinator_exit_error'] = str(exc)
        if not RESULT['uncertain'] and daemon is not None and daemon.poll() is None:
            daemon.terminate()
            try:
                daemon.wait(timeout=20)
            except subprocess.TimeoutExpired:
                daemon.kill()
                daemon.wait()
        if not RESULT['uncertain'] and logger is not None:
            try:
                finish_log_collector(logger)
                RESULT['isolation']['dockerd_log'] = text('/run/g2-dockerd.log')
            except Exception as exc:
                RESULT['completed'] = False
                RESULT['isolation']['dockerd_log_error'] = str(exc)
        try:
            if case == 'matrix' and protocol_ready and not fault_emitted and not PROTOCOL_FAILED:
                emit()
        finally:
            if RESULT['uncertain'] and not fault_emitted:
                handoff.retain_guard()


if __name__ == '__main__':
    main()
