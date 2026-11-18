"""Held clean-boot origins and the fixed Docker/PostgreSQL process subtree."""
import hashlib
import os
import re
import select
import stat
import threading
import time

import network_verify
from contract import MIGRATION_CGROUP

from journal import JournalError, process_start
from systemd_realm import ALLOWED

MAX_PROC_FILE = 1024 * 1024
CONTAINER_ID = re.compile(r'^[0-9a-f]{64}$')
FORBIDDEN_EXE = frozenset(('firefox', 'chromium', 'google-chrome', 'pg_dump', 'pg_dumpall', 'pg_basebackup',
                           'curl', 'wget', 'scp', 'sftp', 'rsync', 'nc', 'socat', 'dns-probe'))
INSPECT_TEMPLATE = ('{{json .Id}}\t{{json .Image}}\t{{json .Name}}\t{{json .Config.Labels}}\t'
                    '{{json .HostConfig.RestartPolicy}}\t{{json .Mounts}}\t{{json .NetworkSettings.Networks}}\t'
                    '{{json .State.Status}}\t{{json .HostConfig.NetworkMode}}\t{{json .HostConfig.PidMode}}\t'
                    '{{json .HostConfig.PortBindings}}\t{{json .NetworkSettings.Ports}}')
NETWORK_TEMPLATE = ('{{json .Id}}\t{{json .Name}}\t{{json .Driver}}\t{{json .Internal}}\t'
                    '{{json .Labels}}\t{{json .Containers}}\t{{json .Scope}}\t{{json .Ingress}}')


def _read(path):
    with open(path, 'rb') as stream:
        data = stream.read(MAX_PROC_FILE + 1)
    if len(data) > MAX_PROC_FILE:
        raise JournalError('process observation exceeds limit')
    return data


def _stat(pid):
    data = _read('/proc/%d/stat' % pid)
    marker = data.rfind(b') ')
    if marker < 0:
        raise JournalError('process stat is malformed')
    fields = data[marker + 2:].split()
    if len(fields) < 20 or not fields[1].isdigit() or not fields[6].isdigit():
        raise JournalError('process stat identity unavailable')
    return {'state': fields[0], 'parent': int(fields[1]), 'flags': int(fields[6]), 'start': int(fields[19])}


def _identity(pid, full=False, *, bootstrap_broker=None, bootstrap_reader=None):
    before = _stat(pid)
    if before['state'] == b'Z':
        raise JournalError('unjoined process is present')
    if before['flags'] & 0x00200000:  # PF_KTHREAD
        return None
    executable = '/proc/%d/exe' % pid
    path = os.readlink(executable)
    broker = bootstrap_broker if bootstrap_broker is not None and bootstrap_broker['pid'] == pid else None
    reader = bootstrap_reader if bootstrap_reader is not None and bootstrap_reader['pid'] == pid else None
    principal = broker or reader
    if not path.startswith('/') or path.endswith(' (deleted)') and principal is None:
        raise JournalError('process executable identity unavailable')
    if principal is not None and (before['start'] != principal['starttime'] or
            path not in ('/g2-bpf-authority', '/g2-bpf-authority (deleted)')):
        raise JournalError('bootstrap principal executable path or incarnation differs')
    fd = os.open(executable, os.O_RDONLY | os.O_CLOEXEC)
    try:
        item = os.fstat(fd)
        if not stat.S_ISREG(item.st_mode):
            raise JournalError('process executable is not regular')
        digest = hashlib.sha256()
        while True:
            block = os.read(fd, 1024 * 1024)
            if not block:
                break
            digest.update(block)
    finally:
        os.close(fd)
    if principal is not None and ((item.st_dev, item.st_ino) != (principal['exe_dev'], principal['exe_ino']) or
                                 digest.hexdigest() != principal['executable_sha256']):
        raise JournalError('bootstrap principal held executable differs')
    status = _read('/proc/%d/status' % pid).splitlines()
    values = {}
    for row in status:
        key, sep, value = row.partition(b':')
        if sep and key in (b'Uid', b'Gid', b'Groups', b'NoNewPrivs', b'CapEff', b'CapPrm', b'CapInh', b'CapAmb'):
            if key in values:
                raise JournalError('duplicate process status field')
            values[key] = value.strip().decode('ascii')
    if b'Uid' not in values or b'Gid' not in values:
        raise JournalError('process credentials unavailable')
    cgroup = _read('/proc/%d/cgroup' % pid).decode('ascii', 'strict').strip()
    command = _read('/proc/%d/cmdline' % pid)
    if not command or not command.endswith(b'\0'):
        raise JournalError('process argument identity unavailable')
    if reader is not None and (before['parent'] != 1 or command != b'/g2-bpf-authority\0audit-reader\0' or
                               values.get(b'Uid', '').split() != ['0'] * 4 or values.get(b'Gid', '').split() != ['0'] * 4):
        raise JournalError('bootstrap audit reader role or parent differs')
    after = _stat(pid)
    if before['start'] != after['start']:
        raise JournalError('process identity changed during observation')
    result = {'pid': pid, 'starttime': before['start'], 'parent': before['parent'], 'exe': path,
              'exe_dev': item.st_dev, 'exe_ino': item.st_ino, 'exe_sha256': digest.hexdigest(),
              'status': values, 'cgroup': cgroup, 'argv_sha256': hashlib.sha256(command).hexdigest()}
    if full:
        result['argv'] = command[:-1].split(b'\0')
    return result


def _thread_credentials(pid, no_new_privileges, uid='10002', gid='10002', require_empty_caps=True):
    directory = '/proc/%d/task' % pid
    allowed_nnp = ('0', '1') if no_new_privileges is None else (str(no_new_privileges),)
    for _ in range(3):
        tasks = sorted(os.listdir(directory))
        if not tasks or any(not tid.isdecimal() for tid in tasks):
            raise JournalError('process thread inventory invalid')
        try:
            for tid in tasks:
                values = {}
                for row in _read(directory + '/' + tid + '/status').splitlines():
                    key, sep, value = row.partition(b':')
                    if sep and key in (b'Uid', b'Gid', b'Groups', b'NoNewPrivs', b'CapEff', b'CapPrm', b'CapInh', b'CapAmb'):
                        if key in values:
                            raise JournalError('duplicate thread credentials')
                        values[key] = value.strip().decode('ascii')
                if (values.get(b'Uid', '').split() != [uid] * 4 or
                        values.get(b'Gid', '').split() != [gid] * 4 or values.get(b'Groups') != '' or
                        values.get(b'NoNewPrivs') not in allowed_nnp or
                        require_empty_caps and any(int(values.get(key, '1'), 16) != 0
                                                   for key in (b'CapEff', b'CapPrm', b'CapInh', b'CapAmb'))):
                    raise JournalError('thread retained privileges or groups')
            if sorted(os.listdir(directory)) == tasks:
                return
        except FileNotFoundError:
            continue
    raise JournalError('process thread inventory did not stabilize')


def _audit_reader_threads(pid):
    directory = '/proc/%d/task' % pid
    expected = {b'CapEff': 1 << 30, b'CapPrm': 1 << 30, b'CapBnd': 1 << 30,
                b'CapInh': 0, b'CapAmb': 0}
    for _ in range(3):
        tasks = sorted(os.listdir(directory))
        if not tasks or any(not tid.isdecimal() for tid in tasks):
            raise JournalError('audit reader thread inventory invalid')
        try:
            for tid in tasks:
                values = {}
                for row in _read(directory + '/' + tid + '/status').splitlines():
                    key, sep, value = row.partition(b':')
                    if sep and key in (*expected, b'Uid', b'Gid', b'NoNewPrivs', b'Seccomp'):
                        if key in values:
                            raise JournalError('duplicate audit reader thread status')
                        values[key] = value.strip().decode('ascii')
                if (values.get(b'Uid', '').split() != ['0'] * 4 or values.get(b'Gid', '').split() != ['0'] * 4 or
                        values.get(b'NoNewPrivs') != '1' or values.get(b'Seccomp') != '2' or
                        any(key not in values or not re.fullmatch(r'[0-9a-fA-F]+', values[key]) or
                            int(values[key], 16) != value for key, value in expected.items())):
                    raise JournalError('audit reader thread confinement differs')
            if sorted(os.listdir(directory)) == tasks:
                return
        except FileNotFoundError:
            continue
    raise JournalError('audit reader thread inventory did not stabilize')


def _pids():
    return sorted(int(name) for name in os.listdir('/proc') if name.isdecimal())


def _cgroup_has_container(cgroup, container_id):
    return any(component == container_id or component == 'docker-' + container_id + '.scope'
               for component in cgroup.split('/'))


class ProcessOrigins:
    """Freeze clean system services; admit later fixed service descendants only."""
    def __init__(self, authority, case):
        self.bpf = authority
        self.bootstrap_broker = authority.broker_identity(time.monotonic() + 5)
        self.bootstrap_reader = authority.reader_identity(time.monotonic() + 5)
        if self.bootstrap_reader['pid'] == self.bootstrap_broker['pid']:
            raise JournalError('bootstrap reader and broker are not distinct')
        _audit_reader_threads(self.bootstrap_reader['pid'])
        self.bootstrap_manifest_sha256 = case['boot_inputs']['bootstrap_manifest_sha256']
        self.bootstrap_guard_sha256 = case['bpf_bootstrap']['guard_sha256']
        self.baseline = {}
        self.container_ids = set()
        self.container_processes = {}
        self.direct = {}
        self.predecessors = {}
        self.resolver_lock = threading.Lock()
        self.resolver_pending = None
        self.recoveries = set()
        self.allowed_predecessor = False
        self.allowed_migrator = False
        self.forbidden_event = None
        self.invalid = False
        for pid in _pids():
            try:
                identity = _identity(pid, bootstrap_broker=self.bootstrap_broker, bootstrap_reader=self.bootstrap_reader)
                if identity is None:
                    continue
                cgroup = identity['cgroup']
                if pid not in (1, self.bootstrap_broker['pid'], self.bootstrap_reader['pid']) and not any(component in ALLOWED for component in cgroup.split('/')):
                    raise JournalError('clean boot has an unregistered process origin')
                pidfd = os.pidfd_open(pid, 0)
                if select.select([pidfd], [], [], 0)[0] or process_start(pid) != identity['starttime']:
                    os.close(pidfd)
                    raise JournalError('clean boot process exited during census')
                self.baseline[pid] = (identity, pidfd)
            except FileNotFoundError as exc:
                raise JournalError('process exited during clean boot census') from exc
        if 1 not in self.baseline or os.getpid() not in self.baseline:
            raise JournalError('clean boot supervisor origins unavailable')

    def genesis_inventory(self):
        result = []
        for _, value in sorted(self.baseline.items()):
            item = value[0]
            entry = {key: item[key] for key in ('pid', 'starttime', 'parent', 'exe', 'exe_sha256', 'argv_sha256', 'cgroup')}
            if item['pid'] == self.bootstrap_broker['pid']:
                entry.update(kind='bpf_bootstrap_broker', boot_id=self.bootstrap_broker['boot_id'],
                             epoch=self.bootstrap_broker['epoch'], task_cookie=self.bootstrap_broker['task_cookie'],
                             principal_id=self.bootstrap_broker['principal_id'],
                             bootstrap_manifest_sha256=self.bootstrap_manifest_sha256,
                             guard_sha256=self.bootstrap_guard_sha256)
            elif item['pid'] == self.bootstrap_reader['pid']:
                entry.update(kind='bpf_bootstrap_audit_reader', boot_id=self.bootstrap_reader['boot_id'],
                             epoch=self.bootstrap_reader['epoch'], task_cookie=self.bootstrap_reader['task_cookie'],
                             principal_id=self.bootstrap_reader['principal_id'],
                             bootstrap_manifest_sha256=self.bootstrap_manifest_sha256,
                             guard_sha256=self.bootstrap_guard_sha256)
            result.append(entry)
        return result

    def expect_container(self, container_id):
        if type(container_id) is not str or CONTAINER_ID.fullmatch(container_id) is None or container_id in self.container_ids:
            raise JournalError('invalid container process admission')
        self.container_ids.add(container_id)

    def register_container(self, process, config):
        container_id = process['container_id']
        if container_id not in self.container_ids:
            raise JournalError('container process was not preadmitted')
        identity = _identity(process['pid'])
        if (identity is None or identity['starttime'] != process['starttime'] or
                not _cgroup_has_container(identity['cgroup'], container_id) or
                not identity['exe'].endswith(config['Entrypoint'][0])):
            raise JournalError('container process executable or cgroup differs')
        self.container_processes[container_id] = (identity, os.dup(process['pidfd']))

    def join_container(self, process):
        container_id = process['container_id']
        held = self.container_processes.get(container_id)
        if held is None or not select.select([held[1]], [], [], 0)[0]:
            raise JournalError('container process was not joined')
        os.close(held[1])
        del self.container_processes[container_id]
        self.container_ids.remove(container_id)

    def register_direct(self, pid, pidfd, path, sha256, argv, root_negative=False):
        if (pid in self.baseline or pid in self.direct or path not in (
                '/opt/g2/c80-initializer', '/opt/g2/domain-initializer', '/opt/g2/c81-migrator',
                '/opt/g2/houfeng-record-platform-admin', '/opt/g2/c80-comparison-negative',
                '/opt/g2/c80-backup-negative') or CONTAINER_ID.fullmatch(sha256) is None or
                select.select([pidfd], [], [], 0)[0]):
            raise JournalError('management process admission invalid')
        item = _identity(pid, full=True)
        expected_argv = [os.fsencode(value) for value in argv]
        credentials = item['status'] if item else {}
        if type(root_negative) is not bool or root_negative != (path == '/opt/g2/c80-backup-negative'):
            raise JournalError('root-only negative helper selection invalid')
        uid = '0' if root_negative else '10002'
        if (item is None or item['parent'] != os.getpid() or item['exe'] != path or item['exe_sha256'] != sha256 or
                item['argv'] != expected_argv or credentials.get(b'Uid', '').split() != [uid] * 4 or
                credentials.get(b'Gid', '').split() != [uid] * 4 or credentials.get(b'Groups') != '' or
                process_start(pid) != item['starttime']):
            raise JournalError('management process identity differs')
        _thread_credentials(pid, 0 if path == '/opt/g2/houfeng-record-platform-admin' else 1,
                            uid=uid, gid=uid, require_empty_caps=not root_negative)
        del item['argv']
        self.direct[pid] = (item, os.dup(pidfd))
        return item

    def join_direct(self, pid, pidfd):
        held = self.direct.get(pid)
        if held is None or not select.select([held[1]], [], [], 0)[0] or not select.select([pidfd], [], [], 0)[0]:
            raise JournalError('management process exit was not joined')
        os.close(held[1])
        del self.direct[pid]

    def begin_resolver(self, name, daemon):
        with self.resolver_lock:
            if (self.resolver_pending is not None or name not in ('app', 'w1', 'w2', 'control') or
                    type(daemon.get('pid')) is not int or process_start(daemon['pid']) != daemon.get('starttime')):
                raise JournalError('resolver creation was not uniquely admitted')
            self.resolver_pending = {'name': name, 'pid': daemon['pid'], 'starttime': daemon['starttime'], 'remaining': 2}

    def begin_reconnect(self, daemon, coordinator, pidfd, verify_receipt):
        with self.resolver_lock:
            if (self.resolver_pending is not None or not callable(verify_receipt) or
                    coordinator['pid'] not in self.direct or select.select([pidfd], [], [], 0)[0] or
                    process_start(coordinator['pid']) != coordinator['starttime'] or
                    process_start(daemon['pid']) != daemon['starttime']):
                raise JournalError('APP network reconnect was not uniquely admitted')
            self.resolver_pending = {'name': 'app-reconnect', 'pid': daemon['pid'], 'starttime': daemon['starttime'],
                                     'remaining': 2, 'coordinator_pidfd': os.dup(pidfd),
                                     'verify_receipt': verify_receipt}

    def finish_reconnect(self):
        with self.resolver_lock:
            item = self.resolver_pending
            if item is None or item['name'] != 'app-reconnect' or item['remaining'] not in (0, 2):
                raise JournalError('APP resolver reconnect audit is incomplete')
            count = 2 - item['remaining']
            os.close(item['coordinator_pidfd'])
            self.resolver_pending = None
            return count

    def resolver_remaining(self):
        with self.resolver_lock:
            if self.resolver_pending is None:
                raise JournalError('resolver admission missing')
            return self.resolver_pending['remaining']

    def finish_resolver(self, name):
        with self.resolver_lock:
            if self.resolver_pending is None or self.resolver_pending['name'] != name or self.resolver_pending['remaining'] != 0:
                raise JournalError('resolver bind audit is incomplete')
            self.resolver_pending = None

    def audit_event(self, event):
        # This stream detects additional forbidden events and audit loss. It is
        # not an admission gate and an empty queue never proves completeness.
        if not event['success']:
            return
        executable = event.get('exe') or ''
        if event['operation'] == 'exec' and os.path.basename(executable) in FORBIDDEN_EXE:
            self.forbidden_event = {'operation': 'exec', 'pid': event['pid'], 'exe': executable}
            self.invalid = True
            raise JournalError('forbidden producer execution observed')
        if event['operation'] == 'bind' and event.get('sockaddr'):
            try:
                address = bytes.fromhex(event['sockaddr'])
            except ValueError as exc:
                self.invalid = True
                raise JournalError('unparseable listener address') from exc
            if len(address) >= 4 and int.from_bytes(address[:2], 'little') in (2, 10):
                family = int.from_bytes(address[:2], 'little')
                port = int.from_bytes(address[2:4], 'big')
                if family == 2 and port == 0 and len(address) >= 8 and address[4:8] == b'\x7f\x00\x00\x0b':
                    with self.resolver_lock:
                        item = self.resolver_pending
                        if (item is not None and item['remaining'] > 0 and executable == '/usr/bin/dockerd' and
                                process_start(item['pid']) == item['starttime'] and
                                os.path.exists('/proc/%d/task/%d' % (item['pid'], event['pid']))):
                            if item['name'] == 'app-reconnect':
                                if select.select([item['coordinator_pidfd']], [], [], 0)[0]:
                                    raise JournalError('resolver rebind observed after coordinator exit')
                                item['verify_receipt']()
                            item['remaining'] -= 1
                            return
                if family != 2 or port != 5432:
                    self.invalid = True
                    raise JournalError('unadmitted network listener observed')

    def _management_descendant(self, item):
        parent = self.direct.get(item['parent'])
        if parent is None or parent[0]['exe'] != '/opt/g2/houfeng-record-platform-admin':
            return False
        status = item['status']
        argv = [value.decode('ascii', 'strict') for value in item['argv']]
        if item['exe'] == '/opt/g2/c80-launcher':
            return (self.allowed_predecessor and argv == ['/opt/g2/c80-launcher', 'service-preview-execute'] and
                    status.get(b'Uid', '').split()[:1] == ['10002'] and status.get(b'Gid', '').split()[:1] == ['10002'])
        if status.get(b'Uid', '').split() != ['10002'] * 4 or status.get(b'Gid', '').split() != ['10002'] * 4:
            return False
        if item['exe'] == '/opt/g2/c81-migrator':
            admission = self.allowed_migrator
            if not (type(admission) is dict and set(admission) == {'coordinator', 'cgroup', 'program_sha256'} and
                    admission['coordinator'] == {'pid': parent[0]['pid'], 'starttime': parent[0]['starttime'],
                                                   'executable_sha256': parent[0]['exe_sha256']} and
                    admission['cgroup']['path'] == MIGRATION_CGROUP and
                    parent[0]['cgroup'] == item['cgroup'] == '0::/g2-c81-migration' and
                    item['exe_sha256'] == admission['program_sha256'] and argv == ['/opt/g2/c81-migrator'] and
                    status.get(b'Groups') == '' and status.get(b'NoNewPrivs') in ('0', '1')):
                return False
            members = network_verify.migration_scope(admission['cgroup'])['members']
            if item['pid'] not in members or parent[0]['pid'] not in members:
                return False
            # UID, groups and capabilities are fixed across startup; NNP is
            # set on all Go threads by the child before its authenticated ready.
            _thread_credentials(item['pid'], None)
            return True
        if item['exe'] == '/opt/g2/c80-predecessor':
            if (not self.allowed_predecessor or argv != ['/opt/g2/c80-predecessor', 'service-preview-execute'] or
                    not item['cgroup'].endswith('/g2-c80-predecessor') or status.get(b'NoNewPrivs') != '1'):
                return False
            _thread_credentials(item['pid'], 1)
            key = (item['pid'], item['starttime'])
            if key not in self.predecessors:
                pidfd = os.pidfd_open(item['pid'], 0)
                if select.select([pidfd], [], [], 0)[0] or process_start(item['pid']) != item['starttime']:
                    os.close(pidfd)
                    raise JournalError('predecessor exited before root pidfd registration')
                self.predecessors[key] = (item['exe_sha256'], item['parent'], pidfd)
            return True
        if item['exe'] != '/usr/bin/docker' or argv[:3] != ['/usr/bin/docker', '--host', 'unix:///var/run/docker.sock']:
            return False
        environment = _read('/proc/%d/environ' % item['pid']).split(b'\0')
        if set(environment) != {b'PATH=/usr/bin:/bin', b'HOME=/nonexistent', b'DOCKER_CONFIG=/nonexistent', b''}:
            return False
        args = argv[3:]
        if args in (['info', '--format', '{{.Swarm.LocalNodeState}}'],
                    ['ps', '-a', '--no-trunc', '--format', '{{.ID}}']):
            return True
        if len(args) == 4 and args[:3] == ['inspect', '--format', INSPECT_TEMPLATE]:
            return CONTAINER_ID.fullmatch(args[3]) is not None and args[3] in self.container_ids
        if len(args) == 5 and args[:3] == ['network', 'inspect', '--format'] and args[3] == NETWORK_TEMPLATE:
            return CONTAINER_ID.fullmatch(args[4]) is not None
        if len(args) == 7 and args[:4] == ['network', 'ls', '--no-trunc', '--filter']:
            return (args[4] in {'name=^houfeng-source-' + rid + '$' for rid in self.recoveries} and
                    args[5:] == ['--format', '{{.ID}}'])
        if len(args) == 9 and args[:5] == ['network', 'create', '--driver', 'bridge', '--label']:
            return any(args[5:] == ['houfeng.source.recovery_id=' + rid, '--label',
                                    'com.docker.compose.project=g2app', 'houfeng-source-' + rid]
                       for rid in self.recoveries)
        if len(args) == 4 and args[:2] in (['network', 'connect'], ['network', 'disconnect']):
            return all(CONTAINER_ID.fullmatch(value) is not None for value in args[2:]) and args[3] in self.container_ids
        if len(args) == 3 and args[:2] == ['update', '--restart=no']:
            return args[2] in self.container_ids
        if len(args) == 2 and args[0] == 'stop':
            return args[1] in self.container_ids
        return False

    def _container_descendant(self, identity):
        for container_id in self.container_ids:
            if _cgroup_has_container(identity['cgroup'], container_id):
                return identity['exe'].endswith(('/usr/lib/postgresql/16/bin/postgres',
                                                  '/usr/lib/postgresql/16/bin/initdb'))
        if identity['exe'] == '/usr/bin/containerd-shim-runc-v2':
            argv = identity['argv']
            return any(argv[index] == b'-id' and index + 1 < len(argv) and
                       argv[index + 1].decode('ascii', 'ignore') in self.container_ids
                       for index in range(len(argv)))
        return False

    def join_predecessor(self, identity, coordinator_pid):
        if (type(identity) is not dict or set(identity) != {'pid', 'starttime', 'executable_sha256'} or
                type(identity['pid']) is not int or type(identity['starttime']) is not int):
            raise JournalError('predecessor receipt identity invalid')
        key = (identity['pid'], identity['starttime'])
        held = self.predecessors.pop(key, None)
        if held is None:
            raise JournalError('predecessor root-held pidfd missing')
        try:
            if (held[0] != identity['executable_sha256'] or held[1] != coordinator_pid or
                    not select.select([held[2]], [], [], 0)[0]):
                raise JournalError('predecessor identity or exit differs')
        finally:
            os.close(held[2])


    def observe(self, deadline):
        if self.invalid:
            raise JournalError('process origin history invalid')
        if self.bpf.broker_identity(deadline) != self.bootstrap_broker:
            self.invalid = True
            raise JournalError('bootstrap broker provenance changed')
        if self.bpf.reader_identity(deadline) != self.bootstrap_reader:
            self.invalid = True
            raise JournalError('bootstrap audit reader provenance changed')
        _audit_reader_threads(self.bootstrap_reader['pid'])
        try:
            import time
            for pid, (expected, pidfd) in self.baseline.items():
                if select.select([pidfd], [], [], 0)[0]:
                    raise JournalError('clean boot process exited')
                actual = _identity(pid, bootstrap_broker=self.bootstrap_broker, bootstrap_reader=self.bootstrap_reader)
                if actual != expected:
                    raise JournalError('clean boot process identity changed')
            for pid, (expected, pidfd) in self.direct.items():
                if select.select([pidfd], [], [], 0)[0] or _identity(pid) != expected:
                    raise JournalError('registered management process changed')
            for pid in _pids():
                if time.monotonic() >= deadline:
                    raise JournalError('process census deadline exceeded')
                if pid in self.baseline or pid in self.direct:
                    continue
                actual = _identity(pid, full=True)
                if actual is None:
                    continue
                if not self._container_descendant(actual) and not self._management_descendant(actual):
                    raise JournalError('unregistered process origin observed')
        except BaseException:
            self.invalid = True
            raise

    def close(self):
        self.invalid = True
        with self.resolver_lock:
            if self.resolver_pending is not None and 'coordinator_pidfd' in self.resolver_pending:
                os.close(self.resolver_pending['coordinator_pidfd'])
            self.resolver_pending = None
        for _, pidfd in self.baseline.values():
            os.close(pidfd)
        for _, pidfd in self.container_processes.values():
            os.close(pidfd)
        for _, pidfd in self.direct.values():
            os.close(pidfd)
        for _, _, pidfd in self.predecessors.values():
            os.close(pidfd)
