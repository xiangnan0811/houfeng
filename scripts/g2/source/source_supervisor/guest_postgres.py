"""Fixed local Docker operations for four persistent PostgreSQL 16 guests."""
import hashlib
import http.client
import ipaddress
import json
import os
import re
import secrets
import select
import socket
import stat
import struct
import threading
import time

import dns

from census import _mount_id
from contract import APP_INITIAL_NETWORK, COMPOSE_DIR, INITIAL_NETWORKS, LEDGER_NETWORK, POSTGRES_HOSTS, POSTGRES_PORT
from guest_bootstrap import PG_ID
from journal import JournalError, process_start, strict_json

DOCKER_SOCKET = '/var/run/docker.sock'
NAMES = ('app', 'w1', 'w2', 'control')
HBA = (b'host all all 172.29.0.0/16 scram-sha-256\n'
       b'host all all 172.30.80.0/24 scram-sha-256\n'
       b'host all all 172.30.82.0/24 scram-sha-256\n')
MAX_RESPONSE = 1024 * 1024


class _UnixHTTP(http.client.HTTPConnection):
    def __init__(self, daemon, timeout):
        super().__init__('localhost', timeout=timeout)
        self.daemon = daemon
        self.active_sock = None

    def abort(self):
        sock = self.active_sock
        if sock is not None:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass

    def connect(self):
        info = os.stat(DOCKER_SOCKET, follow_symlinks=False)
        if (not stat.S_ISSOCK(info.st_mode) or info.st_uid != 0 or info.st_gid != 10002 or
                (info.st_dev, info.st_ino) != (self.daemon['socket_dev'], self.daemon['socket_ino'])):
            raise JournalError('Docker socket identity changed')
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
        self.active_sock = sock
        try:
            sock.settimeout(self.timeout)
            sock.connect(DOCKER_SOCKET)
            peer = struct.unpack('3i', sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
            if (peer[0] != self.daemon['pid'] or peer[1] != 0 or
                    process_start(peer[0]) != self.daemon['starttime']):
                raise JournalError('Docker daemon peer changed')
            self.sock = sock
        except BaseException:
            sock.close()
            raise


class PostgresRealm:
    """Only creates the four fixed initdb and PostgreSQL containers."""
    def __init__(self, daemon, volumes, postgres_uid, postgres_gid, lifecycle, authority):
        if (type(postgres_uid) is not int or type(postgres_gid) is not int or
                not 0 < postgres_uid < (1 << 31) or not 0 < postgres_gid < (1 << 31) or
                postgres_uid == 10002 or postgres_gid == 10002 or set(volumes) != set(NAMES)):
            raise JournalError('PostgreSQL owner or volume inventory invalid')
        self.daemon = daemon
        self.volumes = volumes
        self.uid = postgres_uid
        self.gid = postgres_gid
        self.lifecycle = lifecycle
        if authority is None:
            raise JournalError('PostgreSQL BPF authority missing')
        self.bpf = authority
        self.containers = {}
        self.networks = {}
        image = self._request('GET', '/images/' + PG_ID + '/json')
        if type(image) is not dict or image.get('Id') != PG_ID:
            raise JournalError('pinned PostgreSQL image disappeared')
        self.image_env = self._environment(image.get('Config', {}).get('Env'))
        self.passwords = {}

    def _request(self, method, path, body=None, timeout=20, deadline=None, expected_status=None):
        remaining = None if deadline is None else deadline - time.monotonic()
        if remaining is not None and remaining <= 0:
            raise JournalError('Docker observation deadline exceeded')
        connection = _UnixHTTP(self.daemon, min(timeout, remaining) if remaining is not None else timeout)
        timer = None
        if remaining is not None:
            timer = threading.Timer(remaining, connection.abort)
            timer.daemon = True
            timer.start()
        try:
            encoded = None if body is None else json.dumps(body, sort_keys=True, separators=(',', ':')).encode('ascii')
            connection.request(method, '/v1.41' + path, body=encoded,
                               headers={'Content-Type': 'application/json'} if encoded is not None else {})
            response = connection.getresponse()
            if deadline is not None and time.monotonic() >= deadline:
                raise JournalError('Docker observation deadline exceeded')
            payload = response.read(MAX_RESPONSE + 1)
            if deadline is not None and time.monotonic() >= deadline:
                raise JournalError('Docker observation deadline exceeded')
            if len(payload) > MAX_RESPONSE:
                raise JournalError('Docker response exceeds limit')
            if response.status not in ((expected_status,) if expected_status is not None else (200, 201, 204)):
                raise JournalError('fixed Docker operation rejected')
            return strict_json(payload) if payload else None
        finally:
            if timer is not None:
                timer.cancel()
            connection.close()

    @staticmethod
    def _environment(values):
        if type(values) is not list:
            raise JournalError('container environment is missing')
        result = {}
        for entry in values:
            if type(entry) is not str or '=' not in entry:
                raise JournalError('container environment entry is invalid')
            name, value = entry.split('=', 1)
            if re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', name) is None or name in result:
                raise JournalError('container environment has duplicate or invalid key')
            result[name] = value
        return result

    def create_networks(self):
        if self.networks:
            raise JournalError('initial networks already created')
        for name, (subnet, gateway) in INITIAL_NETWORKS.items():
            config = {'Name': name, 'Driver': 'bridge', 'CheckDuplicate': True,
                      'Internal': False, 'Attachable': False,
                      'IPAM': {'Driver': 'default', 'Config': [{'Subnet': subnet, 'Gateway': gateway}]}}
            self.lifecycle.expect_network(name, config)
            created = self._request('POST', '/networks/create', config)
            network_id = created.get('Id') if type(created) is dict else None
            if type(network_id) is not str or re.fullmatch(r'[0-9a-f]{64}', network_id) is None:
                raise JournalError('initial bridge identity invalid')
            observed = self._request('GET', '/networks/' + network_id)
            if (type(observed) is not dict or observed.get('Id') != network_id or observed.get('Name') != name or
                    observed.get('Driver') != 'bridge' or observed.get('Scope') != 'local' or
                    observed.get('Internal') is not False or observed.get('Attachable') is not False or
                    observed.get('IPAM', {}).get('Driver') != 'default' or
                    observed.get('IPAM', {}).get('Config') != [{'Subnet': subnet, 'Gateway': gateway}] or
                    observed.get('Containers') not in ({}, None)):
                raise JournalError('initial bridge observation differs')
            self.lifecycle.register_network(name, network_id, config)
            self.networks[name] = network_id

    def _config(self, name, init, secret_path=None):
        if name not in NAMES:
            raise JournalError('unknown PostgreSQL service')
        mounts = [{'Type': 'bind', 'Source': '/var/lib/g2/' + name + '/pgdata',
                   'Target': '/var/lib/postgresql/data', 'ReadOnly': False}]
        if init:
            if secret_path != '/run/g2/pg-secret-' + name:
                raise JournalError('PostgreSQL secret path differs')
            mounts.append({'Type': 'bind', 'Source': secret_path, 'Target': '/run/g2-postgres-secret', 'ReadOnly': True})
            executable = '/usr/lib/postgresql/16/bin/initdb'
            args = ['-D', '/var/lib/postgresql/data', '-U', 'postgres', '--pwfile=/run/g2-postgres-secret',
                    '--auth-local=reject', '--auth-host=scram-sha-256', '--encoding=UTF8', '--locale=C.UTF-8']
        else:
            executable = '/usr/lib/postgresql/16/bin/postgres'
            args = ['-D', '/var/lib/postgresql/data', '-c', 'listen_addresses=0.0.0.0',
                    '-c', 'port=%d' % POSTGRES_PORT, '-c', 'unix_socket_directories=',
                    '-c', 'archive_mode=off', '-c', 'archive_command=', '-c', 'max_wal_senders=0',
                    '-c', 'max_replication_slots=0', '-c', 'wal_level=replica', '-c', 'logging_collector=off']
        network = APP_INITIAL_NETWORK if name == 'app' else LEDGER_NETWORK
        labels = {'com.docker.compose.project': 'g2app' if name == 'app' else 'g2domains',
                  'com.docker.compose.service': 'db' if name == 'app' else name,
                  'com.docker.compose.project.working_dir': COMPOSE_DIR}
        result = {'Image': PG_ID, 'Entrypoint': [executable], 'Cmd': args, 'Labels': labels,
                  'User': '%d:%d' % (self.uid, self.gid), 'WorkingDir': '/var/lib/postgresql/data',
                  'Env': ['PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/lib/postgresql/16/bin',
                          'LANG=C.UTF-8'],
                  'HostConfig': {'NetworkMode': 'none' if init else network,
                                 'Dns': ['127.0.0.1'],
                                 'ReadonlyRootfs': True, 'Privileged': False,
                                 'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'],
                                 'PidsLimit': 256, 'RestartPolicy': {'Name': 'no'},
                                 'LogConfig': {'Type': 'none', 'Config': {}}, 'Mounts': mounts}}
        if not init:
            if network not in self.networks:
                raise JournalError('initial PostgreSQL network not registered')
            result['NetworkingConfig'] = {'EndpointsConfig': {network: {'IPAMConfig': {'IPv4Address': POSTGRES_HOSTS[name]}}}}
        return result

    def _inspect(self, container_id, config, initial_network=True, deadline=None):
        if type(container_id) is not str or re.fullmatch(r'[0-9a-f]{64}', container_id) is None:
            raise JournalError('container identity invalid')
        item = self._request('GET', '/containers/' + container_id + '/json', deadline=deadline)
        if type(item) is not dict or item.get('Id') != container_id or item.get('Image') != PG_ID:
            raise JournalError('PostgreSQL container image differs')
        actual, host = item.get('Config', {}), item.get('HostConfig', {})
        expected = config['HostConfig']
        expected_env = dict(self.image_env)
        expected_env.update(self._environment(config['Env']))
        if (actual.get('User') != config['User'] or actual.get('Entrypoint') != config['Entrypoint'] or
                actual.get('Cmd') != config['Cmd'] or actual.get('WorkingDir') != config['WorkingDir'] or
                actual.get('Labels') != config['Labels'] or self._environment(actual.get('Env')) != expected_env or
                host.get('NetworkMode') != expected['NetworkMode'] or host.get('Dns') != ['127.0.0.1'] or
                host.get('ReadonlyRootfs') is not True or
                host.get('Privileged') is not False or 'ALL' not in (host.get('CapDrop') or []) or
                'no-new-privileges' not in (host.get('SecurityOpt') or []) or
                host.get('RestartPolicy', {}).get('Name') != 'no' or
                host.get('LogConfig', {}).get('Type') != 'none' or host.get('PidsLimit') != 256 or
                any(value not in (None, []) for value in (host.get('PortBindings') or {}).values()) or
                any(value not in (None, []) for value in (item.get('NetworkSettings', {}).get('Ports') or {}).values())):
            raise JournalError('PostgreSQL container configuration differs')
        observed = {(mount.get('Source'), mount.get('Destination'), mount.get('RW')) for mount in item.get('Mounts', [])}
        wanted = {(mount['Source'], mount['Target'], not mount['ReadOnly']) for mount in expected['Mounts']}
        if observed != wanted:
            raise JournalError('PostgreSQL container mounts differ')
        if initial_network and config.get('NetworkingConfig') and item.get('State', {}).get('Running') is True:
            endpoints = config['NetworkingConfig']['EndpointsConfig']
            network = next(iter(endpoints))
            live = item.get('NetworkSettings', {}).get('Networks')
            if (type(live) is not dict or set(live) != {network} or
                    live[network].get('NetworkID') != self.networks.get(network) or
                    live[network].get('IPAddress') != endpoints[network]['IPAMConfig']['IPv4Address']):
                raise JournalError('PostgreSQL initial bridge attachment differs')
        return item

    def _start(self, name, init, secret_path=None):
        label = 'g2-' + ('init-' if init else 'pg-') + name
        config = self._config(name, init, secret_path)
        self.lifecycle.expect_postgres_container(label, name, init, config)
        created = self._request('POST', '/containers/create?name=' + label, config)
        if (type(created) is not dict or type(created.get('Id')) is not str or
                re.fullmatch(r'[0-9a-f]{64}', created['Id']) is None):
            raise JournalError('PostgreSQL container creation returned no identity')
        container_id = created['Id']
        admitted = False
        try:
            self._inspect(container_id, config)
            self.lifecycle.bind_created_container(label, container_id)
            self.bpf.admit_container(config, container_id, 'postgres-' + ('init-' if init else 'live-') + name)
            admitted = True
            self._request('POST', '/containers/' + container_id + '/start')
            item = self._inspect(container_id, config)
            state = item.get('State', {})
            pid = state.get('Pid')
            if state.get('Running') is not True or type(pid) is not int or pid <= 0:
                raise JournalError('PostgreSQL container was not observed running')
            start = process_start(pid)
            pidfd = os.pidfd_open(pid, 0)
            try:
                if select.select([pidfd], [], [], 0)[0] or process_start(pid) != start:
                    raise JournalError('PostgreSQL process identity changed before registration')
                self.lifecycle.register_postgres_container(label, name, init, container_id, pid, start, pidfd, config)
                return {'name': label, 'id': container_id, 'pid': pid, 'starttime': start, 'pidfd': pidfd, 'config': config}
            except BaseException:
                os.close(pidfd)
                raise
        except BaseException:
            try:
                self._request('DELETE', '/containers/' + container_id + '?force=1&v=1')
                if admitted:
                    self.bpf.settle_container(container_id, 'failed', time.monotonic() + 20)
            except BaseException:
                pass
            raise

    def _wait_initdb(self, process):
        deadline = time.monotonic() + 300
        while time.monotonic() < deadline:
            if select.select([process['pidfd']], [], [], min(0.2, deadline - time.monotonic()))[0]:
                waited = self._request('POST', '/containers/' + process['id'] + '/wait?condition=not-running',
                                       deadline=deadline, expected_status=200)
                if (type(waited) is not dict or not {'StatusCode'} <= set(waited) <= {'StatusCode', 'Error'} or
                        type(waited['StatusCode']) is not int or waited['StatusCode'] != 0):
                    raise JournalError('initdb container wait did not report successful exit')
                error = waited.get('Error')
                if error is not None and (type(error) is not dict or set(error) - {'Message'} or
                                          type(error.get('Message', '')) is not str or error.get('Message', '') != ''):
                    raise JournalError('initdb container wait reported an error')
                item = self._inspect(process['id'], process['config'], deadline=deadline)
                state = item.get('State', {})
                if (state.get('Running') is not False or type(state.get('ExitCode')) is not int or
                        state['ExitCode'] != 0 or type(state.get('Pid')) is not int or state['Pid'] != 0):
                    raise JournalError('initdb failed or exit is uncertain')
                self.lifecycle.join_postgres_container(process['name'], process['id'], process['pid'],
                                                        process['starttime'], process['pidfd'], state)
                self._request('DELETE', '/containers/' + process['id'])
                if self.bpf.settle_container(process['id'], 'completed', deadline) != 'retired':
                    raise JournalError('initdb BPF attachment settlement remains pending')
                return
        raise JournalError('initdb did not complete before deadline')

    def _seal_hba(self, name):
        self.lifecycle.expect_postgres_hba(name, hashlib.sha256(HBA).hexdigest())
        directory = os.open('/var/lib/g2/' + name + '/pgdata',
                            os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            item = self.volumes[name]
            info = os.fstat(directory)
            if (info.st_dev, info.st_ino, _mount_id(directory)) != (
                    item['pgdata_dev'], item['pgdata_ino'], item['pgdata_mount_id']):
                raise JournalError('PGDATA identity changed before HBA setup')
            fd = os.open('pg_hba.conf', os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            try:
                current = os.fstat(fd)
                if (not stat.S_ISREG(current.st_mode) or current.st_nlink != 1 or
                        (current.st_uid, current.st_gid) != (self.uid, self.gid)):
                    raise JournalError('generated PostgreSQL HBA identity invalid')
                os.ftruncate(fd, 0)
                view = memoryview(HBA)
                while view:
                    size = os.write(fd, view)
                    if size <= 0:
                        raise JournalError('PostgreSQL HBA write did not complete')
                    view = view[size:]
                os.fchmod(fd, 0o600)
                os.fsync(fd)
                if os.pread(fd, len(HBA) + 1, 0) != HBA:
                    raise JournalError('PostgreSQL HBA readback differs')
            finally:
                os.close(fd)
            os.fsync(directory)
        finally:
            os.close(directory)
        self.lifecycle.register_postgres_hba(name, hashlib.sha256(HBA).hexdigest())

    def initialize(self):
        if self.containers or self.passwords:
            raise JournalError('PostgreSQL volumes already initialized')
        for name in NAMES:
            item = self.volumes[name]
            path = '/var/lib/g2/' + name + '/pgdata'
            fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                info = os.fstat(fd)
                if ((info.st_dev, info.st_ino, _mount_id(fd)) !=
                        (item['pgdata_dev'], item['pgdata_ino'], item['pgdata_mount_id']) or os.listdir(fd)):
                    raise JournalError('PGDATA is not the registered empty directory')
                os.fchown(fd, self.uid, self.gid)
                os.fchmod(fd, 0o700)
                os.fsync(fd)
            finally:
                os.close(fd)
            password = secrets.token_hex(32)
            secret = '/run/g2/pg-secret-' + name
            secret_fd = os.open(secret, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o400)
            try:
                os.fchown(secret_fd, self.uid, self.gid)
                os.fchmod(secret_fd, 0o400)
                data = (password + '\n').encode('ascii')
                if os.write(secret_fd, data) != len(data):
                    raise JournalError('PostgreSQL secret write failed')
                os.fsync(secret_fd)
            finally:
                os.close(secret_fd)
            try:
                process = self._start(name, True, secret)
                try:
                    self._wait_initdb(process)
                except BaseException:
                    try:
                        deadline = time.monotonic() + 20
                        item = self._request('GET', '/containers/' + process['id'] + '/json', deadline=deadline)
                        state = item.get('State', {})
                        if state.get('Running') is True:
                            self._request('POST', '/containers/' + process['id'] + '/kill?signal=KILL', deadline=deadline)
                        waited = self._request('POST', '/containers/' + process['id'] + '/wait?condition=not-running', deadline=deadline)
                        if (type(waited) is not dict or type(waited.get('StatusCode')) is not int or
                                not select.select([process['pidfd']], [], [], max(0, deadline - time.monotonic()))[0]):
                            raise JournalError('failed initdb settlement is uncertain')
                        self._request('DELETE', '/containers/' + process['id'] + '?v=1', deadline=deadline)
                        self.bpf.settle_container(process['id'], 'failed', deadline)
                    except BaseException:
                        pass
                    raise
                finally:
                    os.close(process['pidfd'])
            finally:
                os.unlink(secret)
            self._seal_hba(name)
            self.passwords[name] = password
        for name in NAMES:
            self.lifecycle.expect_postgres_endpoint(name, POSTGRES_HOSTS[name], POSTGRES_PORT)
            self.lifecycle.expect_resolver(name, self.daemon)
            process = self._start(name, False)
            self.containers[name] = process
            deadline = time.monotonic() + 60
            while True:
                if select.select([process['pidfd']], [], [], 0)[0]:
                    raise JournalError('PostgreSQL service exited during startup')
                if time.monotonic() >= deadline:
                    raise JournalError('PostgreSQL service did not become ready')
                try:
                    with socket.create_connection((POSTGRES_HOSTS[name], POSTGRES_PORT), timeout=1) as client:
                        client.sendall(struct.pack('!II', 8, 80877103))
                        if client.recv(1) in (b'N', b'S'):
                            break
                except OSError:
                    pass
                select.select([process['pidfd']], [], [], min(0.1, deadline - time.monotonic()))
            resolver = dns.observe(process['pid'], process['starttime'], process['pidfd'], self.daemon,
                                   self.lifecycle.cgroups['dns_preflight']['tools'], True, self.lifecycle)
            self.lifecycle.register_resolver(name, resolver, time.monotonic() + 10)
        return {name: {'container_id': item['id'], 'pid': item['pid'], 'starttime': item['starttime'],
                       'host': POSTGRES_HOSTS[name], 'port': POSTGRES_PORT}
                for name, item in self.containers.items()}

    def observe(self, admitted_recoveries, resolver_transition=None, deadline=None):
        """Reinspect the four held services and the real post-fence APP bridge."""
        if set(self.containers) != set(NAMES) or set(self.networks) != set(INITIAL_NETWORKS):
            raise JournalError('PostgreSQL realm is incomplete')
        if (type(admitted_recoveries) is not set or any(type(rid) is not str or
                re.fullmatch(r'[a-z0-9_-]{1,128}', rid) is None for rid in admitted_recoveries)):
            raise JournalError('maintenance network admission is invalid')
        dynamic = {'houfeng-source-' + rid: rid for rid in admitted_recoveries}
        listed = self._request('GET', '/containers/json?all=1', deadline=deadline)
        if type(listed) is not list or {item.get('Id') for item in listed} != {
                item['id'] for item in self.containers.values()} or len(listed) != len(NAMES):
            raise JournalError('unregistered container exists')
        raw_networks = self._request('GET', '/networks', deadline=deadline)
        if type(raw_networks) is not list:
            raise JournalError('Docker network inventory unavailable')
        network_names = [item.get('Name') for item in raw_networks]
        allowed = {'host', 'none'} | set(INITIAL_NETWORKS) | set(dynamic)
        if (len(network_names) != len(set(network_names)) or set(network_names) - allowed or
                not set(INITIAL_NETWORKS).issubset(network_names)):
            raise JournalError('unregistered Docker network exists')
        networks = {}
        for entry in raw_networks:
            name, network_id = entry.get('Name'), entry.get('Id')
            if name in ('host', 'none'):
                continue
            if type(network_id) is not str or re.fullmatch(r'[0-9a-f]{64}', network_id) is None:
                raise JournalError('network identity unavailable')
            network = self._request('GET', '/networks/' + network_id, deadline=deadline)
            if (network.get('Id') != network_id or network.get('Name') != name or
                    network.get('Driver') != 'bridge' or network.get('Scope') != 'local' or
                    network.get('Internal') is not False or network.get('IPAM', {}).get('Driver') != 'default'):
                raise JournalError('network properties changed')
            if name in INITIAL_NETWORKS:
                subnet, gateway = INITIAL_NETWORKS[name]
                if (network_id != self.networks[name] or
                        network.get('IPAM', {}).get('Config') != [{'Subnet': subnet, 'Gateway': gateway}]):
                    raise JournalError('initial network identity changed')
            else:
                labels = {'houfeng.source.recovery_id': dynamic[name], 'com.docker.compose.project': 'g2app'}
                configs = network.get('IPAM', {}).get('Config')
                if network.get('Labels') != labels or type(configs) is not list or len(configs) != 1:
                    raise JournalError('maintenance network labels changed')
                try:
                    subnet = ipaddress.IPv4Network(configs[0]['Subnet'], strict=True)
                    gateway = ipaddress.IPv4Address(configs[0]['Gateway'])
                except (KeyError, TypeError, ValueError) as exc:
                    raise JournalError('maintenance network IPAM invalid') from exc
                if subnet.prefixlen != 24 or not subnet.subnet_of(ipaddress.IPv4Network('172.29.0.0/16')) or gateway not in subnet:
                    raise JournalError('maintenance network escaped fixed address pool')
            networks[name] = network
        members = {name: set() for name in networks}
        app_host = None
        for name, process in self.containers.items():
            if select.select([process['pidfd']], [], [], 0)[0] or process_start(process['pid']) != process['starttime']:
                raise JournalError('held PostgreSQL process exited')
            item = self._inspect(process['id'], process['config'], initial_network=False, deadline=deadline)
            state = item.get('State', {})
            if state.get('Running') is not True or state.get('Pid') != process['pid']:
                raise JournalError('PostgreSQL process identity changed')
            actual_resolver = dns.observe(process['pid'], process['starttime'], process['pidfd'], self.daemon,
                                          self.lifecycle.cgroups['dns_preflight']['tools'], True, self.lifecycle, deadline)
            attached = item.get('NetworkSettings', {}).get('Networks')
            if type(attached) is not dict or len(attached) != 1:
                raise JournalError('PostgreSQL service has unexpected endpoints')
            network_name, endpoint = next(iter(attached.items()))
            if network_name not in networks or endpoint.get('NetworkID') != networks[network_name]['Id']:
                raise JournalError('container network identity changed')
            host = endpoint.get('IPAddress')
            if name == 'app':
                if network_name != APP_INITIAL_NETWORK and network_name not in dynamic:
                    raise JournalError('APP is attached to an unadmitted network')
                if network_name == APP_INITIAL_NETWORK and host != POSTGRES_HOSTS['app']:
                    raise JournalError('APP initial address changed')
                app_host = host
            elif network_name != LEDGER_NETWORK or host != POSTGRES_HOSTS[name]:
                raise JournalError('ledger service address changed')
            try:
                address = ipaddress.IPv4Address(host)
                config = networks[network_name]['IPAM']['Config'][0]
                subnet = ipaddress.IPv4Network(config['Subnet'], strict=True)
                actual_member = networks[network_name]['Containers'][process['id']]
                member_address = ipaddress.IPv4Interface(actual_member['IPv4Address'])
            except (KeyError, TypeError, ValueError) as exc:
                raise JournalError('network membership is unreadable') from exc
            if address not in subnet or member_address.ip != address or member_address.network != subnet:
                raise JournalError('network membership address differs')
            if actual_resolver != self.lifecycle.resolvers.get(name):
                if resolver_transition is None:
                    raise JournalError('registered Docker DNS namespace or sockets changed')
                resolver_transition.accept_resolver(name, process['id'], endpoint['NetworkID'], actual_resolver)
            members[network_name].add(process['id'])
        for network_name, network in networks.items():
            if set((network.get('Containers') or {}).keys()) != members[network_name]:
                raise JournalError('unregistered network member exists')
        if deadline is not None and time.monotonic() >= deadline:
            raise JournalError('PostgreSQL endpoint observation deadline exceeded')
        return {'app_host': app_host, 'app_port': POSTGRES_PORT,
                'app_network_id': next(network['Id'] for name, network in networks.items()
                                       if self.containers['app']['id'] in members[name]),
                'network_ids': {name: item['Id'] for name, item in networks.items()}}
