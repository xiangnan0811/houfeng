"""Bind the one pre-admitted APP fence network transition to durable receipts."""
import hashlib
import os
import re
import select
import stat
import time

from contract import COMPOSE_DIR, MAINTENANCE_CONFIG
from journal import JournalError, canonical, strict_json
from phase import read_private
import network_verify

HEX = re.compile(r'^[0-9a-f]{64}$')
FENCE_DIR = '/var/lib/g2/control/maintenance/fences'


def _anchor(item):
    labels = item.get('Config', {}).get('Labels') or {}
    actual_networks = item.get('NetworkSettings', {}).get('Networks')
    if type(actual_networks) is not dict or not actual_networks:
        raise JournalError('original APP network inventory missing')
    networks = []
    for value in actual_networks.values():
        aliases = value.get('Aliases')
        if aliases is not None and (type(aliases) is not list or any(type(alias) is not str for alias in aliases)):
            raise JournalError('original APP network aliases invalid')
        networks.append({'id': value.get('NetworkID'), 'aliases': sorted(aliases) if aliases else None})
    mounts = []
    for value in item.get('Mounts', []):
        mounts.append({'Type': value.get('Type'), 'Name': value.get('Name') or '', 'Source': value.get('Source') or ''})
    mounts.sort(key=lambda value: (value['Type'], value['Name'], value['Source']))
    restart = item.get('HostConfig', {}).get('RestartPolicy') or {}
    return {'id': item.get('Id'), 'image': item.get('Image'), 'name': item.get('Name'),
            'service': labels.get('com.docker.compose.service', ''), 'project': labels.get('com.docker.compose.project', ''),
            'working_directory': labels.get('com.docker.compose.project.working_dir', ''),
            'restart': {'Name': restart.get('Name'), 'MaximumRetryCount': restart.get('MaximumRetryCount')},
            'networks': sorted(networks, key=lambda value: value['id']), 'mounts': mounts}


class FenceTransition:
    def __init__(self, life, postgres, recovery_id):
        if (life.stage != 'source_maintenance' or life.invalid or recovery_id not in life.recoveries or
                re.fullmatch(r'[a-z0-9_-]{1,128}', recovery_id) is None):
            raise JournalError('source fence transition cannot be admitted')
        self.life, self.postgres, self.recovery_id = life, postgres, recovery_id
        process = postgres.containers.get('app')
        if process is None or select.select([process['pidfd']], [], [], 0)[0]:
            raise JournalError('original APP process is unavailable')
        self.app_id = process['id']
        self.original = _anchor(postgres._inspect(self.app_id, process['config']))
        self.previous_resolver = life.resolvers['app']
        self.new_resolver = None
        self.coordinator = None
        self.receipt = None
        self.action = {'recovery_id': recovery_id, 'app_container_id': self.app_id,
                       'original_anchor_sha256': hashlib.sha256(canonical(self.original)).hexdigest()}
        life._admit('source-network-transition', self.action)

    def launched(self, identity, pidfd):
        if self.coordinator is not None:
            raise JournalError('source fence coordinator already registered')
        self.coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                            'executable_sha256': identity['exe_sha256']}
        self.life.origins.begin_reconnect(self.postgres.daemon, self.coordinator, pidfd, self.receipts)

    def receipts(self):
        directory = os.open(FENCE_DIR, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(directory)
            if ((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (10002, 10002, 0o700) or
                    not stat.S_ISDIR(info.st_mode)):
                raise JournalError('source fence receipt directory invalid')
            original_raw = read_private(directory, self.recovery_id + '.json')
            network_raw = read_private(directory, self.recovery_id + '.network.json')
        finally:
            os.close(directory)
        original, network = strict_json(original_raw), strict_json(network_raw)
        request = {'RecoveryID': self.recovery_id, 'Project': 'g2app', 'WorkingDirectory': COMPOSE_DIR,
                   'DatabaseContainerID': self.app_id, 'ControlConfigPath': MAINTENANCE_CONFIG}
        if (type(original) is not dict or set(original) != {'version', 'request', 'containers'} or
                type(original['version']) is not int or original['version'] != 1 or original['request'] != request or
                original['containers'] != [self.original] or type(network) is not dict or
                set(network) != {'version', 'recovery_id', 'network_id'} or type(network['version']) is not int or
                network['version'] != 1 or network['recovery_id'] != self.recovery_id or
                type(network['network_id']) is not str or HEX.fullmatch(network['network_id']) is None):
            raise JournalError('source fence receipts differ from the original APP')
        return {'network_id': network['network_id'], 'inventory_sha256': hashlib.sha256(original_raw).hexdigest(),
                'network_receipt_sha256': hashlib.sha256(network_raw).hexdigest(),
                'inventory_raw': original_raw.decode('utf-8', 'strict'),
                'network_receipt_raw': network_raw.decode('utf-8', 'strict')}

    def accept_resolver(self, name, container_id, network_id, resolver):
        if (name != 'app' or container_id != self.app_id or self.receipt is None or
                network_id != self.receipt['network_id'] or self.new_resolver is not None or
                resolver.get('netns') != self.previous_resolver.get('netns')):
            raise JournalError('resolver transition is not bound to the admitted APP network')
        self.new_resolver = resolver

    def complete(self, command_result):
        with self.life.bpf.observation(time.monotonic() + 15):
            return self._complete(command_result)

    def _complete(self, command_result):
        if (self.coordinator is None or command_result.get('pid') != self.coordinator['pid'] or
                command_result.get('starttime') != self.coordinator['starttime']):
            raise JournalError('source fence coordinator identity changed')
        self.receipt = self.receipts()
        endpoint = self.postgres.observe(self.life.recoveries, self)
        if endpoint['app_network_id'] != self.receipt['network_id']:
            raise JournalError('APP did not join its receipt-bound fence network')
        network_verify.networks(endpoint['network_ids'], time.monotonic() + 5)
        network_verify.cgroups(self.life.cgroups['child_cgroup'], self.life.cgroups['writer_cgroup'],
                               self.life.cgroups['migration_cgroup'], self.life.origins)
        deadline = time.monotonic() + 10
        self.life.audit.await_health(deadline)
        self.life.origins.observe(deadline)
        bind_count = self.life.origins.finish_reconnect()
        current = self.new_resolver if self.new_resolver is not None else self.previous_resolver
        socket_changed = current['sockets'] != self.previous_resolver['sockets']
        if (socket_changed and bind_count != 2) or (not socket_changed and bind_count != 0):
            raise JournalError('resolver socket transition does not match audited binds')
        observed = {'recovery_id': self.recovery_id, 'app_container_id': self.app_id,
                    'coordinator': self.coordinator, 'network_id': self.receipt['network_id'],
                    'inventory_sha256': self.receipt['inventory_sha256'],
                    'network_receipt_sha256': self.receipt['network_receipt_sha256'],
                    'inventory_raw': self.receipt['inventory_raw'], 'network_receipt_raw': self.receipt['network_receipt_raw'],
                    'original_anchor_sha256': self.action['original_anchor_sha256'],
                    'app_host': endpoint['app_host'], 'network_ids': endpoint['network_ids'],
                    'previous_resolver': self.previous_resolver, 'current_resolver': current,
                    'resolver_bind_count': bind_count}
        self.life._register('source-network-transition', observed)
        self.life.resolvers['app'] = current
        self.life.resolver_transition = observed
        return observed
