"""Observe the fixed Docker DNS resolver and its pre-genesis isolation probe."""
import ctypes
import hashlib
import json
import os
import re
import select
import shlex
import signal
import stat
import time

from journal import JournalError, canonical, process_start, strict_json

PORT = 53
PROBE_NETWORK = 'g2-dns-preflight'
PROBE_NAME = 'g2-dns-preflight'
PROBE_PATH = '/run/g2/dns-preflight'
PROBE_TARGET = '/var/lib/postgresql/data'
PROBE_ADDRESS = '172.31.123.2'
PROBE_SUBNET = '172.31.123.0/24'
PROBE_GATEWAY = '172.31.123.1'
MAX = 1024 * 1024
HEX = re.compile(r'^[0-9a-f]{64}$')
_LIBC = ctypes.CDLL(None, use_errno=True)


def _tool(path, prefix):
    real = os.path.realpath(path)
    if not real.startswith(prefix + '/'):
        raise JournalError('namespace observation tool escaped fixed directory')
    for directory in ('/usr', prefix):
        info = os.stat(directory, follow_symlinks=False)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or stat.S_IMODE(info.st_mode) & 0o022:
            raise JournalError('namespace observer ancestor is writable')
    fd = os.open(real, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_gid != 0 or
                stat.S_IMODE(before.st_mode) & 0o022 or not before.st_mode & 0o111):
            raise JournalError('namespace observation tool identity invalid')
        digest = hashlib.sha256()
        while True:
            data = os.read(fd, 1024 * 1024)
            if not data:
                break
            digest.update(data)
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
            raise JournalError('namespace observation tool changed while hashing')
        return {'path': real, 'dev': before.st_dev, 'ino': before.st_ino, 'sha256': digest.hexdigest()}
    finally:
        os.close(fd)


def tools():
    return {'nsenter': _tool('/usr/bin/nsenter', '/usr/bin'),
            'iptables': _tool('/usr/sbin/iptables-save', '/usr/sbin')}


def _docker(daemon, method, path, body=None):
    from guest_postgres import _UnixHTTP
    connection = _UnixHTTP(daemon, 20)
    try:
        encoded = None if body is None else json.dumps(body, sort_keys=True, separators=(',', ':')).encode('ascii')
        connection.request(method, '/v1.41' + path, body=encoded,
                           headers={'Content-Type': 'application/json'} if encoded is not None else {})
        response = connection.getresponse()
        payload = response.read(MAX + 1)
        if response.status not in (200, 201, 204) or len(payload) > MAX:
            raise JournalError('fixed DNS diagnostic Docker operation failed')
        return strict_json(payload) if payload else None
    finally:
        connection.close()


def _read(path, limit=MAX):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        data = bytearray()
        while len(data) <= limit:
            chunk = os.read(fd, min(65536, limit + 1 - len(data)))
            if not chunk:
                return bytes(data)
            data.extend(chunk)
        raise JournalError('DNS network observation exceeded limit')
    finally:
        os.close(fd)


def _socket_table(pid, family):
    lines = _read('/proc/%d/net/%s' % (pid, family)).decode('ascii', 'strict').splitlines()
    if not lines:
        raise JournalError('network namespace socket inventory unavailable')
    found = []
    for row in lines[1:]:
        fields = row.split()
        if len(fields) < 10 or ':' not in fields[1] or not fields[9].isdigit():
            raise JournalError('network namespace socket inventory malformed')
        address, port = fields[1].split(':', 1)
        if family.endswith('6'):
            if fields[3] == '0A' or family == 'udp6' and int(port, 16):
                raise JournalError('unadmitted IPv6 namespace listener')
            continue
        if len(address) != 8:
            raise JournalError('IPv4 namespace socket address malformed')
        ip = '.'.join(str(value) for value in bytes.fromhex(address)[::-1])
        port = int(port, 16)
        if family == 'tcp' and fields[3] != '0A':
            continue
        if family == 'udp' and not port:
            continue
        found.append({'address': ip, 'port': port, 'inode': int(fields[9])})
    return found


def _fd_inodes(pid):
    result = set()
    for name in os.listdir('/proc/%d/fd' % pid):
        try:
            target = os.readlink('/proc/%d/fd/%s' % (pid, name))
        except FileNotFoundError:
            continue
        match = re.fullmatch(r'socket:\[([0-9]+)\]', target)
        if match:
            result.add(int(match.group(1)))
    return result


def _resolv(pid):
    text = _read('/proc/%d/root/etc/resolv.conf' % pid, 16384).decode('ascii', 'strict')
    names = [line.split()[1:] for line in text.splitlines() if line.strip().startswith('nameserver ')]
    external = [line.strip() for line in text.splitlines() if line.strip().startswith('# ExtServers:')]
    if names != [['127.0.0.11']] or external != ['# ExtServers: [127.0.0.1]']:
        raise JournalError('container DNS upstream is not the fixed unreachable loopback')
    return hashlib.sha256(text.encode('ascii')).hexdigest()


def _child_stop(pid, deadline):
    while time.monotonic() < deadline:
        observed, status = os.waitpid(pid, os.WUNTRACED | os.WNOHANG)
        if observed:
            if os.WIFSTOPPED(status) and os.WSTOPSIG(status) == signal.SIGSTOP:
                return
            raise JournalError('namespace observer did not enter its pre-exec stop')
        select.select([], [], [], max(0, min(0.02, deadline - time.monotonic())))
    raise JournalError('namespace observer stop deadline exceeded')


def _iptables(pid, starttime, pidfd, expected_tools, lifecycle=None, deadline=None):
    deadline = min(deadline, time.monotonic() + 10) if deadline is not None else time.monotonic() + 10
    if time.monotonic() >= deadline:
        raise JournalError('namespace observer deadline exceeded')
    if tools() != expected_tools or select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime:
        raise JournalError('network namespace or observer tool changed')
    nsfd = os.open('/proc/%d/ns/net' % pid, os.O_RDONLY | os.O_CLOEXEC)
    ns = os.fstat(nsfd)
    read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
    action = {'pid': pid, 'starttime': starttime, 'netns_dev': ns.st_dev, 'netns_ino': ns.st_ino,
              'nsenter_sha256': expected_tools['nsenter']['sha256'], 'iptables_sha256': expected_tools['iptables']['sha256']}
    if lifecycle is not None:
        lifecycle._admit('dns-netns-observe', action)
    parent_pid = os.getpid()
    child = os.fork()
    if child == 0:
        try:
            os.close(read_fd)
            os.dup2(write_fd, 1)
            os.dup2(write_fd, 2)
            os.close(write_fd)
            os.set_inheritable(nsfd, True)
            if (os.getppid() != parent_pid or _LIBC.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or
                    os.getppid() != parent_pid):
                os._exit(125)
            os.kill(os.getpid(), signal.SIGSTOP)
            os.execve(expected_tools['nsenter']['path'], [expected_tools['nsenter']['path'],
                      '--net=/proc/self/fd/%d' % nsfd, '--', '/usr/sbin/iptables-save', '-t', 'nat'],
                      {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C', 'LC_ALL': 'C'})
        except BaseException:
            os._exit(126)
    os.close(write_fd)
    childfd, reaped = None, False
    try:
        childfd = os.pidfd_open(child, 0)
        child_start = process_start(child)
        _child_stop(child, deadline)
        if lifecycle is not None:
            lifecycle.journal.append('launch', {'name': 'dns-netns-observer', 'pid': child,
                                               'starttime': child_start,
                                               'executable_sha256': expected_tools['nsenter']['sha256']})
        signal.pidfd_send_signal(childfd, signal.SIGCONT)
        output, eof, exited = bytearray(), False, False
        while not (eof and exited):
            if time.monotonic() >= deadline:
                raise JournalError('namespace observer deadline exceeded')
            ready, _, _ = select.select(([read_fd] if not eof else []) + ([childfd] if not exited else []),
                                        [], [], min(0.2, deadline - time.monotonic()))
            if read_fd in ready:
                block = os.read(read_fd, 65536)
                if block:
                    output.extend(block)
                    if len(output) > 65536:
                        raise JournalError('namespace rules exceeded limit')
                else:
                    eof = True
            if childfd in ready:
                exited = True
        waited, status = os.waitpid(child, 0)
        reaped = True
        if waited != child or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            raise JournalError('namespace rules could not be observed')
        if (select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime or
                (os.stat('/proc/%d/ns/net' % pid).st_dev, os.stat('/proc/%d/ns/net' % pid).st_ino) != (ns.st_dev, ns.st_ino)):
            raise JournalError('network namespace changed during rules observation')
        if lifecycle is not None:
            lifecycle.journal.append('exit', {'name': 'dns-netns-observer', 'pid': child, 'starttime': child_start,
                                             'status': 0, 'pidfd_exited': True})
            lifecycle._register('dns-netns-observe', dict(action, observer_pid=child, observer_starttime=child_start))
        return output.decode('ascii', 'strict'), {'dev': ns.st_dev, 'ino': ns.st_ino}
    finally:
        if childfd is not None:
            if not reaped:
                try:
                    signal.pidfd_send_signal(childfd, signal.SIGKILL)
                    os.waitpid(child, 0)
                except (OSError, ChildProcessError):
                    pass
            os.close(childfd)
        elif not reaped:
            try:
                waited, _ = os.waitpid(child, os.WNOHANG)
                if waited == 0:
                    os.kill(child, signal.SIGKILL)
                    os.waitpid(child, 0)
            except (OSError, ChildProcessError):
                pass
        os.close(nsfd)
        os.close(read_fd)


def _rules(text, sockets):
    rules = []
    for line in text.splitlines():
        if not line.startswith('-A '):
            continue
        tokens = shlex.split(line)
        if len(tokens) % 2:
            raise JournalError('Docker DNS NAT rule is malformed')
        fields = {}
        for index in range(0, len(tokens), 2):
            if tokens[index] in fields:
                raise JournalError('Docker DNS NAT rule has duplicate fields')
            fields[tokens[index]] = tokens[index + 1]
        rules.append(fields)
    expected = [{'-A': 'OUTPUT', '-d': '127.0.0.11/32', '-j': 'DOCKER_OUTPUT'},
                {'-A': 'POSTROUTING', '-d': '127.0.0.11/32', '-j': 'DOCKER_POSTROUTING'}]
    for protocol in ('tcp', 'udp'):
        port = str(sockets[protocol]['port'])
        expected.append({'-A': 'DOCKER_OUTPUT', '-d': '127.0.0.11/32', '-p': protocol, '-m': protocol,
                         '--dport': '53', '-j': 'DNAT', '--to-destination': '127.0.0.11:' + port})
        expected.append({'-A': 'DOCKER_POSTROUTING', '-s': '127.0.0.11/32', '-p': protocol, '-m': protocol,
                         '--sport': port, '-j': 'SNAT', '--to-source': ':53'})
    if len(rules) != len(expected) or any(rules.count(item) != 1 for item in expected):
        raise JournalError('Docker DNS NAT redirects differ from observed sockets')
    return hashlib.sha256(canonical(sorted(rules, key=lambda rule: canonical(rule)))).hexdigest()


def observe(pid, starttime, pidfd, daemon, expected_tools, postgres, lifecycle=None, deadline=None):
    if select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime:
        raise JournalError('container process exited before DNS observation')
    tcp, udp = _socket_table(pid, 'tcp'), _socket_table(pid, 'udp')
    _socket_table(pid, 'tcp6')
    _socket_table(pid, 'udp6')
    dns_tcp = [entry for entry in tcp if entry['address'] == '127.0.0.11']
    dns_udp = [entry for entry in udp if entry['address'] == '127.0.0.11']
    other_tcp = [entry for entry in tcp if entry['address'] != '127.0.0.11']
    if (len(dns_tcp) != 1 or len(dns_udp) != 1 or len(udp) != 1 or
            any(not 0 < item['port'] < 65536 or item['inode'] <= 0 for item in dns_tcp + dns_udp)):
        raise JournalError('embedded DNS listener set differs')
    if postgres:
        if len(other_tcp) != 1 or (other_tcp[0]['address'], other_tcp[0]['port']) != ('0.0.0.0', 5432):
            raise JournalError('PostgreSQL namespace listener set differs')
        if other_tcp[0]['inode'] not in _fd_inodes(pid):
            raise JournalError('PostgreSQL listener is not owned by the held process')
    elif other_tcp:
        raise JournalError('diagnostic namespace has an extra TCP listener')
    if process_start(daemon['pid']) != daemon['starttime']:
        raise JournalError('Docker daemon identity changed')
    daemon_inodes = _fd_inodes(daemon['pid'])
    if dns_tcp[0]['inode'] not in daemon_inodes or dns_udp[0]['inode'] not in daemon_inodes:
        raise JournalError('embedded resolver sockets are not owned by the pinned Docker daemon')
    resolv = _resolv(pid)
    output, namespace = _iptables(pid, starttime, pidfd, expected_tools, lifecycle, deadline)
    sockets = {'tcp': dns_tcp[0], 'udp': dns_udp[0]}
    rules_sha = _rules(output, sockets)
    if select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime:
        raise JournalError('container changed during DNS observation')
    if deadline is not None and time.monotonic() >= deadline:
        raise JournalError('DNS observation deadline exceeded')
    return {'netns': namespace, 'sockets': sockets, 'resolv_sha256': resolv, 'nat_sha256': rules_sha}


def preflight(daemon, expected, authority):
    import network_verify
    if authority is None:
        raise JournalError('DNS BPF authority missing')
    if _docker(daemon, 'GET', '/containers/json?all=1') != []:
        raise JournalError('pre-genesis Docker container inventory is not empty')
    initial_networks = _docker(daemon, 'GET', '/networks')
    if type(initial_networks) is not list or {item.get('Name') for item in initial_networks} != {'host', 'none'}:
        raise JournalError('pre-genesis Docker network inventory is not empty')
    safe_tools = tools()
    os.mkdir(PROBE_PATH, 0o700)
    os.chown(PROBE_PATH, 10002, 10002)
    network_id = container_id = pidfd = None
    admitted = False
    deadline = None
    try:
        config = {'Name': PROBE_NETWORK, 'Driver': 'bridge', 'CheckDuplicate': True, 'Internal': True,
                  'Attachable': False, 'IPAM': {'Driver': 'default',
                                                'Config': [{'Subnet': PROBE_SUBNET, 'Gateway': PROBE_GATEWAY}]}}
        network = _docker(daemon, 'POST', '/networks/create', config)
        network_id = network.get('Id') if type(network) is dict else None
        if type(network_id) is not str or HEX.fullmatch(network_id) is None:
            raise JournalError('diagnostic network identity unavailable')
        image = daemon['image_id']
        container = {'Image': image, 'Entrypoint': ['/opt/g2/dns-probe'], 'Cmd': [], 'User': '10002:10002',
                     'Env': ['PATH=/usr/bin:/bin', 'LANG=C'], 'StopSignal': 'SIGTERM',
                     'HostConfig': {'NetworkMode': PROBE_NETWORK, 'Dns': ['127.0.0.1'], 'ReadonlyRootfs': True,
                                    'Privileged': False, 'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'],
                                    'PidsLimit': 32, 'RestartPolicy': {'Name': 'no'},
                                    'LogConfig': {'Type': 'none', 'Config': {}},
                                    'Mounts': [{'Type': 'bind', 'Source': '/opt/g2/dns-probe',
                                                'Target': '/opt/g2/dns-probe', 'ReadOnly': True},
                                               {'Type': 'bind', 'Source': PROBE_PATH,
                                                'Target': PROBE_TARGET, 'ReadOnly': False}]},
                     'NetworkingConfig': {'EndpointsConfig': {PROBE_NETWORK: {'IPAMConfig': {'IPv4Address': PROBE_ADDRESS}}}}}
        created = _docker(daemon, 'POST', '/containers/create?name=' + PROBE_NAME, container)
        container_id = created.get('Id') if type(created) is dict else None
        if type(container_id) is not str or HEX.fullmatch(container_id) is None:
            raise JournalError('diagnostic container identity unavailable')
        authority.admit_container(container, container_id, 'dns-preflight')
        admitted = True
        _docker(daemon, 'POST', '/containers/' + container_id + '/start')
        live = _docker(daemon, 'GET', '/containers/' + container_id + '/json')
        state, host, endpoint = live.get('State', {}), live.get('HostConfig', {}), live.get('NetworkSettings', {}).get('Networks', {})
        pid = state.get('Pid')
        if (live.get('Id') != container_id or live.get('Image') != image or host.get('Dns') != ['127.0.0.1'] or
                host.get('NetworkMode') != PROBE_NETWORK or state.get('Running') is not True or type(pid) is not int or pid <= 0 or
                type(endpoint) is not dict or set(endpoint) != {PROBE_NETWORK} or
                endpoint[PROBE_NETWORK].get('NetworkID') != network_id or endpoint[PROBE_NETWORK].get('IPAddress') != PROBE_ADDRESS):
            raise JournalError('diagnostic process or isolated network differs')
        actual = live.get('Config', {})
        mounts = {(entry.get('Type'), entry.get('Source'), entry.get('Destination'), entry.get('RW'))
                  for entry in live.get('Mounts', [])}
        expected_mounts = {('bind', '/opt/g2/dns-probe', '/opt/g2/dns-probe', False),
                           ('bind', PROBE_PATH, PROBE_TARGET, True)}
        network = _docker(daemon, 'GET', '/networks/' + network_id)
        if (actual.get('Entrypoint') != ['/opt/g2/dns-probe'] or actual.get('Cmd') not in (None, []) or
                actual.get('User') != '10002:10002' or actual.get('StopSignal') != 'SIGTERM' or
                host.get('ReadonlyRootfs') is not True or
                host.get('Privileged') is not False or 'ALL' not in (host.get('CapDrop') or []) or
                'no-new-privileges' not in (host.get('SecurityOpt') or []) or host.get('PidsLimit') != 32 or
                host.get('RestartPolicy', {}).get('Name') != 'no' or host.get('LogConfig', {}).get('Type') != 'none' or
                len(live.get('Mounts', [])) != 2 or mounts != expected_mounts or
                network.get('Id') != network_id or network.get('Internal') is not True or
                network.get('Driver') != 'bridge' or network.get('IPAM', {}).get('Config') != [
                    {'Subnet': PROBE_SUBNET, 'Gateway': PROBE_GATEWAY}] or
                set((network.get('Containers') or {}).keys()) != {container_id}):
            raise JournalError('diagnostic container or private network configuration differs')
        starttime = process_start(pid)
        pidfd = os.pidfd_open(pid, 0)
        deadline = time.monotonic() + 30
        while not os.path.exists(PROBE_PATH + '/result.json'):
            if select.select([pidfd], [], [], 0)[0] or time.monotonic() >= deadline:
                raise JournalError('actual DNS diagnostic did not complete')
            select.select([pidfd], [], [], 0.05)
        fd = os.open(PROBE_PATH + '/result.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode), info.st_nlink) !=
                    (10002, 10002, 0o600, 1) or not 0 < info.st_size <= 4096):
                raise JournalError('diagnostic result identity invalid')
            body = os.read(fd, info.st_size + 1)
        finally:
            os.close(fd)
        result = strict_json(body)
        if (type(result) is not dict or set(result) != {'version', 'label_sha256', 'upstream_refused',
                                                        'resolver_outcome', 'resolver_rcode'} or
                type(result['version']) is not int or result['version'] != 1 or
                type(result['label_sha256']) is not str or HEX.fullmatch(result['label_sha256']) is None or
                result['upstream_refused'] is not True or result['resolver_outcome'] not in ('timeout', 'noanswer') or
                type(result['resolver_rcode']) is not int or
                (result['resolver_outcome'] == 'timeout' and result['resolver_rcode'] != -1) or
                (result['resolver_outcome'] == 'noanswer' and not 0 <= result['resolver_rcode'] <= 15)):
            raise JournalError('diagnostic query result differs')
        net = observe(pid, starttime, pidfd, daemon, safe_tools, False)
        network_verify.networks({PROBE_NETWORK: network_id}, time.monotonic() + 5)
        _docker(daemon, 'POST', '/containers/' + container_id + '/stop?t=3')
        if not select.select([pidfd], [], [], 10)[0]:
            raise JournalError('diagnostic container did not exit')
        waited = _docker(daemon, 'POST', '/containers/' + container_id + '/wait?condition=not-running')
        if type(waited) is not dict or type(waited.get('StatusCode')) is not int or waited['StatusCode'] != 143:
            raise JournalError('diagnostic container wait differs')
        stopped = _docker(daemon, 'GET', '/containers/' + container_id + '/json')
        if stopped.get('State', {}).get('Running') is not False or stopped['State'].get('ExitCode') != 143:
            raise JournalError('diagnostic process exit differs')
        _docker(daemon, 'DELETE', '/containers/' + container_id)
        completed_id, container_id = container_id, None
        if authority.settle_container(completed_id, 'completed', deadline) != 'retired':
            raise JournalError('DNS BPF attachment settlement remains pending')
        _docker(daemon, 'DELETE', '/networks/' + network_id)
        network_id = None
        if _docker(daemon, 'GET', '/containers/json?all=1') != [] or set(os.listdir('/sys/class/net')) != {'lo'}:
            raise JournalError('diagnostic container or endpoint remains')
        final_networks = _docker(daemon, 'GET', '/networks')
        if type(final_networks) is not list or {item.get('Name') for item in final_networks} != {'host', 'none'}:
            raise JournalError('diagnostic network was not removed')
        return {'version': 1, 'program_sha256': expected['dns-probe'], 'query': result,
                'resolver': net, 'tools': safe_tools, 'probe_joined': True, 'endpoint_removed': True}
    finally:
        if container_id is not None:
            try:
                _docker(daemon, 'POST', '/containers/' + container_id + '/stop?t=1')
                if pidfd is not None and select.select([pidfd], [], [], 0)[0]:
                    waited = _docker(daemon, 'POST', '/containers/' + container_id + '/wait?condition=not-running')
                    if type(waited) is not dict or type(waited.get('StatusCode')) is not int:
                        raise JournalError('DNS cleanup wait is uncertain')
                    _docker(daemon, 'DELETE', '/containers/' + container_id)
                    if admitted and deadline is not None and time.monotonic() < deadline:
                        authority.settle_container(container_id, 'failed', deadline)
                else:
                    _docker(daemon, 'DELETE', '/containers/' + container_id + '?force=1')
            except BaseException:
                pass
        if pidfd is not None:
            os.close(pidfd)
        if network_id is not None:
            try:
                _docker(daemon, 'DELETE', '/networks/' + network_id)
            except BaseException:
                pass
        for name in ('result.json', 'result.tmp'):
            try:
                os.unlink(PROBE_PATH + '/' + name)
            except FileNotFoundError:
                pass
        os.rmdir(PROBE_PATH)
