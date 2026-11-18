"""Root-owned admission and continuous identity for a fresh C80 VM realm."""
import hashlib
import os
import re
import select
import time
import guest_bootstrap

from callback_lifecycle import CallbackLifecycle
from contract import INITIAL_NETWORKS, POSTGRES_HOSTS, POSTGRES_PORT
from guest_postgres import HBA
from journal import JournalError, canonical, process_start, strict_json
from volume_verify import verify_volumes
import network_verify
import systemd_realm

STAGES = {
    'initializing': frozenset(('seeded',)),
    'seeded': frozenset(('source_maintenance',)),
    'source_maintenance': frozenset(('predecessor', 'c81_migration', 'retired')),
    'predecessor': frozenset(('refencing',)),
    'refencing': frozenset(('source_maintenance', 'c81_migration')),
    'c81_migration': frozenset(('retired',)),
    'retired': frozenset(),
}
NAMES = ('app', 'w1', 'w2', 'control')
HEX = re.compile(r'^[0-9a-f]{64}$')


def _digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


class RealmLifecycle:
    """Admit fixed actions before effect and retain each process identity."""
    def __init__(self, journal, roots, volumes, postgres_uid, postgres_gid, systemd_state, origins, cgroups, bpf):
        if bpf.role != 'REALM_ROOT':
            raise JournalError('realm BPF principal unavailable')
        self.bpf = bpf
        self.journal = journal
        self.roots = roots
        self.volumes = volumes
        self.uid = postgres_uid
        self.gid = postgres_gid
        self.systemd_state = systemd_state
        self.origins = origins
        self.cgroups = cgroups
        self.audit = None
        self.postgres = None
        self.stage = 'initializing'
        self.generation = 1
        self.invalid = False
        self.invalid_reason = None
        self.closed_maintenance = False
        self.negative_producer_used = False
        self.pending = {}
        self.networks = {}
        self.processes = {}
        self.endpoints = {}
        self.resolvers = {}
        self.resolver_transition = None
        self.recoveries = set()
        self.migration_scopes = []
        self.continuation_retirement = None
        self.maintenance_journal = None
        self.maintenance_directory = None
        self.migration_checkpoint = None
        self.app_endpoint = None
        self.callbacks = CallbackLifecycle(self)
        self.journal.append('phase_enter', {'phase': self.stage, 'phase_generation': self.generation})

    def bind_audit(self, audit):
        if self.audit is not None or audit is None:
            raise JournalError('duplicate or missing audit owner')
        self.audit = audit

    def bind_postgres(self, postgres):
        if self.postgres is not None or postgres.lifecycle is not self:
            raise JournalError('PostgreSQL supervisor binding invalid')
        self.postgres = postgres

    def register_migration_checkpoint(self, application, recovery_id, program_sha256, verify_backend, verify_settlement):
        from contract import GUARD
        from maintenance import MaintenanceJournal, MigrationCheckpoint, RecordDirectory
        if (self.stage != 'c81_migration' or self.migration_checkpoint is not None or
                recovery_id not in self.recoveries):
            raise JournalError('migration checkpoint owner registration invalid')
        info = os.stat(GUARD, follow_symlinks=False)
        import stat
        if (not stat.S_ISREG(info.st_mode) or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode), info.st_nlink) !=
                (10002, 10002, 0o600, 1)):
            raise JournalError('maintenance guard identity invalid')
        guard = {'dev': info.st_dev, 'ino': info.st_ino}
        group = self.cgroups['migration_cgroup']
        journal = MaintenanceJournal(self.journal, guard, group)
        directory = None
        try:
            directory = RecordDirectory()
            checkpoint = MigrationCheckpoint(journal, directory, application, recovery_id, guard, group,
                                             program_sha256, verify_backend, verify_settlement)
        except BaseException:
            if directory is not None:
                directory.close()
            journal.close()
            raise
        self.maintenance_journal, self.maintenance_directory, self.migration_checkpoint = journal, directory, checkpoint

    def require_migration_continuity(self):
        with self.bpf.observation(time.monotonic() + 5):
            self._require_migration_continuity()

    def _require_migration_continuity(self):
        if self.stage != 'c81_migration' or self.migration_checkpoint is None:
            raise JournalError('migration continuity owner is unavailable')
        self.migration_checkpoint.require_retained_prefix()

    def require_terminal_admission(self):
        if (self.stage not in ('c81_migration', 'retired') or self.migration_checkpoint is None or
                not self.migration_checkpoint.terminal_admission_ready()):
            raise JournalError('witnessed terminal admission is unavailable')

    def require_retired_maintenance_continuation(self):
        retirement = self.continuation_retirement
        owner = self.migration_checkpoint
        if (not self.invalid or self.invalid_reason != 'direct_launch_failed' or
                self.stage not in ('c81_migration', 'retired') or self.pending or owner is None or
                type(retirement) is not dict or not self.migration_scopes or
                self.migration_scopes[-1]['retirement'] != retirement or owner.coordinator is None or
                retirement.get('coordinator') != owner.coordinator or
                owner.retired_coordinators.get((owner.coordinator['pid'], owner.coordinator['starttime'])) !=
                _digest(retirement)):
            raise JournalError('retired maintenance continuation is unavailable')
        owner.require_retained_prefix()

    def maintenance_evidence(self):
        if self.maintenance_journal is None:
            return None
        return [raw.decode('utf-8', 'strict') for raw in self.maintenance_journal.public_witness_bytes()]

    def maintenance_observations(self):
        if self.migration_checkpoint is None:
            return None
        return self.migration_checkpoint.public_live_observations()

    def maintenance_settlements(self):
        if self.migration_checkpoint is None:
            return None
        return self.migration_checkpoint.public_settlement_observations()

    def maintenance_ready_records(self):
        if self.migration_checkpoint is None:
            return None
        return self.migration_checkpoint.public_ready_records()

    def close_maintenance(self):
        if self.migration_checkpoint is None:
            return
        if network_verify.migration_scope(self.cgroups['migration_cgroup']) != {'members': [], 'populated': 0}:
            raise JournalError('maintenance scope has not retired')
        self.migration_checkpoint.close()
        self.maintenance_directory.close()
        self.maintenance_journal.close()
        self.migration_checkpoint = self.maintenance_directory = self.maintenance_journal = None

    def _require(self, stage='initializing'):
        if self.invalid or self.stage != stage:
            raise JournalError('realm stage is closed')
        self.journal._check_liveness()

    def _admit(self, name, payload):
        if name in self.pending:
            self.invalidate('conflicting_admission')
            raise JournalError('action already pending')
        digest = _digest(payload)
        self.journal.append('admission', {'name': name, 'phase_generation': self.generation,
                                          'action_digest': digest})
        self.pending[name] = digest
        return digest

    def _register(self, name, payload):
        if name not in self.pending:
            self.invalidate('unregistered_effect')
            raise JournalError('effect was not admitted')
        self.journal.append('registration', {'name': name, 'identity_digest': _digest(payload)})
        del self.pending[name]

    def invalidate(self, reason):
        if self.invalid:
            return
        if type(reason) is not str or re.fullmatch(r'[a-z0-9_]{1,64}', reason) is None:
            reason = 'invalid_realm'
        try:
            self.journal.append('invalid', {'reason': reason})
        finally:
            self.invalid = True
            self.invalid_reason = reason
            self.roots.invalidate()
            if self.audit is not None:
                self.audit.invalidate()

    def reject_unregistered(self, name):
        # Fixed supervisor admission refuses an unknown launch before creating a
        # child, opening a listener, or asking Docker to perform any action.
        self.invalidate('unregistered_launch')
        raise JournalError('unregistered launch is forbidden')

    def enter_closed_maintenance(self):
        self.journal._check_liveness()
        genesis = strict_json(self.journal._genesis) if self.journal._genesis is not None else None
        if (not self.invalid or self.invalid_reason != 'negative_app_rollback' or self.closed_maintenance or
                self.stage != 'source_maintenance' or self.pending or type(genesis) is not dict or
                genesis.get('case') != 'source-recovery' or genesis.get('scenario') != 'app-rollback'):
            raise JournalError('closed APP recovery is unavailable')
        self.journal.append('registration', {'name': 'closed-app-recovery',
                            'identity_digest': _digest({'phase': self.stage, 'phase_generation': self.generation,
                                                        'no_copy_available': False})})
        self.closed_maintenance = True

    def transition(self, target):
        allowed_closed = self.closed_maintenance and (self.stage, target) in (
            ('source_maintenance', 'c81_migration'), ('c81_migration', 'retired'))
        if self.invalid and not allowed_closed and (self.stage, target) == ('c81_migration', 'retired'):
            self.require_retired_maintenance_continuation()
            self.require_terminal_admission()
            allowed_closed = True
        if self.invalid and not allowed_closed or target not in STAGES.get(self.stage, ()) or self.pending:
            self.invalidate('invalid_stage_transition')
            raise JournalError('realm stage transition rejected')
        old, old_generation = self.stage, self.generation
        if old == 'predecessor' and target == 'refencing':
            self.origins.allowed_predecessor = False
        self.journal.append('phase_exit', {'phase': old, 'phase_generation': old_generation})
        self.generation += 1
        self.journal.append('phase_enter', {'phase': target, 'phase_generation': self.generation})
        self.stage = target

    def expect_network(self, name, config):
        self._require()
        if name not in INITIAL_NETWORKS or name in self.networks:
            self.reject_unregistered(name)
        subnet, gateway = INITIAL_NETWORKS[name]
        expected = {'Name': name, 'Driver': 'bridge', 'CheckDuplicate': True,
                    'Internal': False, 'Attachable': False,
                    'IPAM': {'Driver': 'default', 'Config': [{'Subnet': subnet, 'Gateway': gateway}]}}
        if config != expected:
            self.reject_unregistered(name)
        self._admit('network-' + name, expected)

    def register_network(self, name, network_id, config):
        self._require()
        if name not in INITIAL_NETWORKS or type(network_id) is not str or HEX.fullmatch(network_id) is None:
            self.reject_unregistered(name)
        self._register('network-' + name, {'name': name, 'id': network_id, 'config': config})
        self.networks[name] = network_id

    def expect_postgres_endpoint(self, name, host, port):
        self._require()
        if (name not in NAMES or (host, port) != (POSTGRES_HOSTS[name], POSTGRES_PORT) or
                name in self.endpoints):
            self.reject_unregistered('endpoint')
        action = {'name': name, 'host': host, 'port': port}
        self._admit('endpoint-' + name, action)
        self._register('endpoint-' + name, action)
        self.endpoints[name] = action

    def expect_resolver(self, name, daemon):
        self._require()
        if name not in NAMES or name in self.resolvers or name not in self.endpoints:
            self.reject_unregistered('resolver')
        self._admit('resolver-' + name, {'name': name, 'daemon_pid': daemon['pid'],
                                         'daemon_starttime': daemon['starttime'],
                                         'address': '127.0.0.11', 'protocols': ['tcp', 'udp']})
        self.origins.begin_resolver(name, daemon)

    def register_resolver(self, name, observed, deadline):
        with self.bpf.observation(deadline):
            self._register_resolver(name, observed, deadline)

    def _register_resolver(self, name, observed, deadline):
        self._require()
        if type(observed) is not dict or set(observed) != {'netns', 'sockets', 'resolv_sha256', 'nat_sha256'}:
            raise JournalError('resolver observation malformed')
        while self.origins.resolver_remaining() != 0:
            if self.audit is None or self.audit.invalid or self.audit.error is not None or time.monotonic() >= deadline:
                raise JournalError('pre-admitted resolver binds were not observed')
            select.select([], [], [], min(0.02, max(0, deadline - time.monotonic())))
        self.audit.await_health(deadline)
        self.origins.finish_resolver(name)
        self._register('resolver-' + name, {'name': name, 'observation': observed})
        self.resolvers[name] = observed

    def expect_postgres_hba(self, name, digest):
        self._require()
        if name not in NAMES or type(digest) is not str or HEX.fullmatch(digest) is None:
            self.reject_unregistered('postgres-hba')
        self._admit('postgres-hba-' + name, {'name': name, 'sha256': digest})

    def register_postgres_hba(self, name, digest):
        self._require()
        self._register('postgres-hba-' + name, {'name': name, 'sha256': digest})

    def expect_postgres_container(self, label, name, init, config):
        self._require()
        expected = 'g2-' + ('init-' if init else 'pg-') + name
        if (type(init) is not bool or name not in NAMES or label != expected or label in self.processes or
                set(self.networks) != set(INITIAL_NETWORKS) or type(config) is not dict or
                (not init and name not in self.endpoints)):
            self.reject_unregistered(label)
        self._admit('container-' + label, {'name': name, 'init': init, 'config': config})

    def bind_created_container(self, label, container_id):
        self._require()
        if ('container-' + label not in self.pending or type(container_id) is not str or
                HEX.fullmatch(container_id) is None):
            self.reject_unregistered('container-start')
        self.journal.append('registration', {'name': 'created-' + label,
                                             'identity_digest': _digest({'container_id': container_id})})
        self.origins.expect_container(container_id)

    def settle_failed_bpf_probe(self, label, container_id, grant_id, state):
        self._require()
        genesis = strict_json(self.journal._genesis) if self.journal._genesis is not None else None
        grant = self.bpf._observed_grants.get(grant_id)
        if (type(genesis) is not dict or (genesis.get('case'), genesis.get('scenario')) !=
                ('realm-negative', 'bpf-authorization') or label not in ('g2-init-app', 'g2-init-w1') or
                'container-' + label not in self.pending or container_id not in self.origins.container_ids or
                container_id in self.origins.container_processes or container_id in self.bpf._grants or
                grant is None or grant['phase'] != 'retired' or grant['container_id'] != container_id or
                type(state) is not dict or state.get('Running') is not False or
                type(state.get('Pid')) is not int or state['Pid'] != 0):
            raise JournalError('failed BPF probe container is not fully settled')
        self._register('container-' + label, {'container_id': container_id, 'grant_id': grant_id,
                                              'outcome': 'start_failed', 'running': False, 'pid': 0})
        self.origins.container_ids.remove(container_id)

    def register_postgres_container(self, label, name, init, container_id, pid, starttime, pidfd, config):
        self._require()
        if (type(pid) is not int or pid <= 0 or type(starttime) is not int or starttime <= 0 or
                type(container_id) is not str or HEX.fullmatch(container_id) is None or
                select.select([pidfd], [], [], 0)[0] or process_start(pid) != starttime):
            self.invalidate('container_identity')
            raise JournalError('container process identity unavailable')
        held = os.dup(pidfd)
        try:
            self._register('container-' + label,
                           {'name': name, 'init': init, 'container_id': container_id,
                            'pid': pid, 'starttime': starttime, 'config_digest': _digest(config)})
            self.processes[label] = {'name': name, 'init': init, 'container_id': container_id,
                                     'pid': pid, 'starttime': starttime, 'pidfd': held}
            self.origins.register_container(self.processes[label], config)
        except BaseException:
            os.close(held)
            self.invalidate('container_registration')
            raise

    def join_postgres_container(self, label, container_id, pid, starttime, pidfd, state):
        self._require()
        item = self.processes.get(label)
        if (item is None or not item['init'] or item['container_id'] != container_id or item['pid'] != pid or
                item['starttime'] != starttime or not select.select([pidfd], [], [], 0)[0] or
                not select.select([item['pidfd']], [], [], 0)[0] or
                state.get('Running') is not False or state.get('ExitCode') != 0):
            self.invalidate('initdb_join')
            raise JournalError('initdb exit not fully joined')
        self.journal.append('exit', {'name': label, 'pid': pid, 'starttime': starttime,
                                     'status': 0, 'pidfd_exited': True})
        self.origins.join_container(item)
        os.close(item['pidfd'])
        del self.processes[label]

    def admit_recovery(self, recovery_id):
        if self.stage not in ('source_maintenance', 'refencing') or self.invalid or type(recovery_id) is not str or \
                re.fullmatch(r'[a-z0-9_-]{1,128}', recovery_id) is None:
            self.reject_unregistered('maintenance-network')
        if recovery_id not in self.recoveries:
            action = {'recovery_id': recovery_id, 'project': 'g2app',
                      'network_name': 'houfeng-source-' + recovery_id}
            self._admit('maintenance-network-' + recovery_id, action)
            self._register('maintenance-network-' + recovery_id, action)
            self.recoveries.add(recovery_id)
            self.origins.recoveries.add(recovery_id)

    def check_closed_realm(self, deadline):
        try:
            with self.bpf.observation(deadline):
                return self._check_closed_realm(deadline)
        except BaseException:
            if not self.invalid:
                self.invalidate('observation_failed')
            raise

    def _check_closed_realm(self, deadline):
        if (self.invalid or self.audit is None or self.postgres is None or self.pending or
                self.stage not in ('source_maintenance', 'predecessor') or time.monotonic() >= deadline):
            raise JournalError('realm admission unavailable')
        try:
            self.journal.verify()
            guest_bootstrap.verify_migration_binding(self.cgroups)
            self.audit.await_health(deadline)
            verify_volumes(self.volumes, self.uid, self.gid, HBA)
            self.roots.scan()
            action = {'program': '/usr/bin/systemctl', 'operation': 'fixed-unit-census'}
            self._admit('unit-census', action)
            state = systemd_realm.observe(deadline)
            self._register('unit-census', state)
            if state != self.systemd_state:
                raise JournalError('systemd service inventory changed')
            self.app_endpoint = self.postgres.observe(self.recoveries, deadline=deadline)
            network_verify.networks(self.app_endpoint['network_ids'], deadline)
            network_verify.cgroups(self.cgroups['child_cgroup'], self.cgroups['writer_cgroup'],
                                   self.cgroups['migration_cgroup'], self.origins)
            self.origins.observe(deadline)
            self.audit.await_health(deadline)
            if time.monotonic() >= deadline:
                raise JournalError('realm observation deadline exceeded')
            return self.app_endpoint
        except BaseException:
            self.invalidate('observation_failed')
            raise
