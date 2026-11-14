"""Fixed same-APP source and record-management command orchestration."""
import hashlib
import os
import re
import secrets
import select
import stat
import time
import urllib.parse

from callback_lifecycle import _sessions
from contract import APP_INITIAL_NETWORK, COMPOSE_DIR, GUARD, LEDGER_DIR, MAINTENANCE_CONFIG, POSTGRES_HOSTS, POSTGRES_PORT
from cutpoints import COORDINATOR_BOUNDARIES, FaultBarrier, scenario_spec
from extra_barrier import ExtraSessionBarrier
from fence_transition import FenceTransition
from heartbeat import HeartbeatGate
from ipc import RealmChannel
from journal import JournalError, _read as read_root, canonical, process_start, strict_json
from phase import open_control_dir, read_phases, read_private, read_stdout_private
import pg_bootstrap

REQUESTS = '/var/lib/g2/control/maintenance/requests'
HEX = re.compile(r'^[0-9a-f]{64}$')


def _private_request(name, value):
    if re.fullmatch(r'[a-z0-9-]+\.json', name) is None:
        raise JournalError('private request name invalid')
    directory = os.open(REQUESTS, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        owner = os.fstat(directory)
        if (owner.st_uid, owner.st_gid, stat.S_IMODE(owner.st_mode)) != (10002, 10002, 0o700):
            raise JournalError('private request directory changed')
        body = canonical(value)
        if not 0 < len(body) <= 16384:
            raise JournalError('private request exceeds limit')
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
        try:
            os.fchown(fd, 10002, 10002)
            os.fchmod(fd, 0o600)
            view = memoryview(body)
            while view:
                written = os.write(fd, view)
                if written <= 0:
                    raise JournalError('private request write failed')
                view = view[written:]
            os.fsync(fd)
        finally:
            os.close(fd)
        os.fsync(directory)
        return REQUESTS + '/' + name
    finally:
        os.close(directory)


def _url(name, role, password):
    if name not in pg_bootstrap.DATABASES or re.fullmatch(r'[a-z_][a-z0-9_]*', role) is None or \
            type(password) is not str or len(password) < 32:
        raise JournalError('fixed database credential invalid')
    return ('postgresql://' + role + ':' + urllib.parse.quote(password, safe='') + '@' + POSTGRES_HOSTS[name] + ':' +
            str(POSTGRES_PORT) + '/' + pg_bootstrap.DATABASES[name] + '?sslmode=disable')


def _identity(value):
    if (type(value) is not dict or set(value) != {'ID', 'Kind', 'SystemIdentifier', 'DatabaseOID', 'DatabaseName', 'Epoch'} or
            type(value['ID']) is not str or not value['ID'] or type(value['Kind']) is not str or
            type(value['SystemIdentifier']) is not str or not value['SystemIdentifier'].isdigit() or
            type(value['DatabaseOID']) is not int or value['DatabaseOID'] <= 0 or
            type(value['DatabaseName']) is not str or type(value['Epoch']) is not int or value['Epoch'] <= 0):
        raise JournalError('ledger domain identity malformed')
    return {'id': value['ID'], 'kind': value['Kind'], 'system_identifier': value['SystemIdentifier'],
            'database_oid': value['DatabaseOID'], 'database_name': value['DatabaseName'], 'epoch': value['Epoch']}


def _app_identity(value):
    if (type(value) is not dict or set(value) != {'ID', 'SystemIdentifier', 'DatabaseOID', 'DatabaseName', 'Epoch'} or
            type(value['ID']) is not str or not value['ID'] or type(value['SystemIdentifier']) is not str or
            not value['SystemIdentifier'].isdigit() or type(value['DatabaseOID']) is not int or value['DatabaseOID'] <= 0 or
            value['DatabaseName'] != 'houfeng' or type(value['Epoch']) is not int or value['Epoch'] <= 0):
        raise JournalError('application identity malformed')
    return {'id': value['ID'], 'system_identifier': value['SystemIdentifier'], 'database_oid': value['DatabaseOID'],
            'database_name': value['DatabaseName'], 'epoch': value['Epoch']}


def _output(result):
    if result['status'] != 0 or result['stderr'] or not result['stdout']:
        raise JournalError('fixed management command did not complete')
    value = strict_json(result['stdout'])
    if type(value) is not dict:
        raise JournalError('fixed management output is not an object')
    return value


def _source_fixture(app, intersecting=False):
    pair = app.get('records')
    if type(pair) is not dict or set(pair) != {'a', 'b'}:
        raise JournalError('fixture record pair unavailable')
    closures = []
    for name in ('a', 'b'):
        item = pair[name]
        if (type(item) is not dict or set(item) != {'record_id', 'revision_id', 'lock_version', 'authorization_epoch',
                                                   'source_kind', 'source_id', 'parent_vps_id', 'source_scope', 'closure'} or
                re.fullmatch(r'rec_[0-9a-f]{16}', item['record_id']) is None or
                type(item['lock_version']) is not int or item['lock_version'] <= 0 or
                type(item['authorization_epoch']) is not int or item['authorization_epoch'] <= 0 or
                item['source_kind'] not in ('vps', 'monitoring_instance', 'target') or
                type(item['source_scope']) is not dict or
                set(item['source_scope']) != {'version', 'kind', 'project_id', 'allowed_roles', 'allowed_group_ids',
                                               'policy_version', 'policy_revision', 'canonical_hash'} or
                item['source_scope']['project_id'] != 'default' or
                type(item['source_scope']['allowed_roles']) is not list or
                type(item['source_scope']['allowed_group_ids']) is not list or
                type(item['source_scope']['canonical_hash']) is not str or
                HEX.fullmatch(item['source_scope']['canonical_hash']) is None or
                item['source_scope']['canonical_hash'] == '0' * 64 or
                type(item['closure']) is not list or not item['closure']):
            raise JournalError('fixture source scope or CAS invalid')
        keys = []
        for source in item['closure']:
            if (type(source) is not dict or set(source) != {'kind', 'id'} or
                    source['kind'] not in ('vps', 'monitoring_instance', 'target') or type(source['id']) is not str):
                raise JournalError('source closure malformed')
            keys.append((source['kind'], source['id']))
        if keys != sorted(set(keys)) or (item['source_kind'], item['source_id']) not in keys:
            raise JournalError('source closure is not complete and ordered')
        closures.append(set(keys))
    if pair['a']['record_id'] == pair['b']['record_id']:
        raise JournalError('fixture record identities overlap')
    if intersecting:
        expected = {('vps', pair['a']['source_id']), ('monitoring_instance', pair['b']['source_id'])}
        if (pair['a']['source_kind'] != 'vps' or pair['b']['source_kind'] != 'monitoring_instance' or
                pair['b']['parent_vps_id'] != pair['a']['source_id'] or closures != [expected, expected]):
            raise JournalError('negative fixture lacks the exact shared component')
    elif closures[0] & closures[1]:
        raise JournalError('fixture source closures overlap')
    return pair


class SourceCeremony:
    def __init__(self, life, invoker, journal, roots, postgres, app, domains, credentials, app_secrets, expected,
                 fault_scenario=None, intersecting=False, stale_reply=False, negative_scenario=None):
        self.life, self.invoker, self.journal, self.roots, self.postgres = life, invoker, journal, roots, postgres
        self.app, self.domains, self.credentials, self.app_secrets, self.expected = app, domains, credentials, app_secrets, expected
        self.pair = _source_fixture(app, intersecting)
        if type(stale_reply) is not bool:
            raise JournalError('negative reply selector invalid')
        self.stale_reply = stale_reply
        self.fault_scenario = fault_scenario
        self.fault_boundary = scenario_spec(fault_scenario)[0] if fault_scenario else None
        self.fault_observation = None
        if negative_scenario not in (None, 'unsettled-backend'):
            raise JournalError('negative recovery selector invalid')
        self.negative_scenario = negative_scenario
        self.fault_barrier = None
        self.source_operation_id = None
        self.source_retirement = None
        self.recovery_id = 'g2-' + secrets.token_hex(12)
        self.life.admit_recovery(self.recovery_id)
        self.admin_new = secrets.token_hex(32)
        self.fence = self._fence()
        self.app_facts = {'application': app['application'], 'deployment_id': app['deployment_id'],
                          'project_id': 'default', 'runtime_role': app['runtime_role'],
                          'runtime_host': POSTGRES_HOSTS['app'], 'runtime_port': POSTGRES_PORT}
        self.action_order = []
        self.parent_channels = []
        os.mkdir(REQUESTS, 0o700)
        os.chown(REQUESTS, 10002, 10002)
        os.chmod(REQUESTS, 0o700)
        directory = os.open(REQUESTS, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def _fence(self):
        def domain(name, reader, writer):
            return {'identity': _identity(self.domains[name]),
                    'bootstrap_url': _url(name, 'postgres', self.postgres.passwords[name]),
                    'reader_url': _url(name, 'g2_' + reader, self.credentials[reader]),
                    'writer_url': _url(name, 'g2_' + writer, self.credentials[writer]), 'previous_writers': []}
        return {'compose': {'recovery_id': self.recovery_id, 'project': 'g2app', 'working_directory': COMPOSE_DIR,
                            'database_container_id': self.postgres.containers['app']['id'], 'control_config_path': MAINTENANCE_CONFIG},
                'app_bootstrap_url': _url('app', 'postgres', self.postgres.passwords['app']),
                'app_identity': _app_identity(self.app['application']), 'app_admin_role': 'houfeng_platform_admin',
                'app_previous_logins': ['houfeng_migrator', 'houfeng_records_authority', 'houfeng_runtime'],
                'primary': domain('w1', 'w1_reader', 'w1_writer'),
                'witness': domain('w2', 'w2_record_reader', 'w2_record_writer'),
                'compose_container_ids': [self.postgres.containers['app']['id']],
                'host_cgroups': ['/sys/fs/cgroup/g2-writers']}

    def _base(self):
        return {'app_admin_url': _url('app', 'houfeng_platform_admin', self.admin_new),
                'app_previous_admin_url': _url('app', 'houfeng_platform_admin', self.app_secrets['admin_password']),
                'fence': self.fence}

    def _channel(self, identity, pidfd, sock):
        coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                       'executable_sha256': identity['exe_sha256']}
        heartbeat = HeartbeatGate(self.journal, self.invoker, self.app_facts, self.recovery_id, coordinator, pidfd)
        channel = RealmChannel(sock, coordinator, pidfd, self.journal, self.roots, self.life, self.app_facts,
                               self.recovery_id, self.expected['source_guest.py'], heartbeat,
                               stale_reply=self.stale_reply and not self.parent_channels)
        self.parent_channels.append(channel)
        return channel

    def _migration_channel(self, identity, pidfd, sock):
        from ipc import MaintenanceChannel
        coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                       'executable_sha256': identity['exe_sha256']}
        if self.life.migration_checkpoint is None:
            raise JournalError('migration checkpoint owner is unavailable')
        return MaintenanceChannel(sock, coordinator, pidfd, self.life.migration_checkpoint)

    def _migration_selection(self):
        owner = self.life.migration_checkpoint
        if owner is None:
            raise JournalError('migration checkpoint owner is unavailable')
        if not owner.terminal_admission_ready():
            raise JournalError('final migration activation is not terminal')
        generation = owner.generation
        witnesses = owner.journal.public_witnesses()
        selected = {}

        def select(kind, phase_generation, attempt=0):
            matches = [entry for entry in witnesses
                       if entry['family'] == 'migration' and entry['recovery_id'] == owner.recovery_id and
                       entry['application_digest'] == owner.application_digest and entry['kind'] == kind and
                       entry['phase_generation'] == phase_generation and entry['attempt'] == attempt]
            if len(matches) != 1:
                raise JournalError('migration selected witness is incomplete')
            entry = matches[0]
            raw = owner.directory.read(entry['record_name'])
            if hashlib.sha256(raw).hexdigest() != entry['record_sha256']:
                raise JournalError('migration selected record differs from witness')
            return strict_json(raw)

        terminal = select('terminal', generation + 2)
        activation = select('activation', generation)
        if (terminal['phase'] != 'quiescent' or terminal['generation'] != generation + 2 or
                activation['terminal_phase_generation'] != terminal['generation']):
            raise JournalError('migration selected terminal differs from activation')
        selected['terminal'] = terminal
        selected['activation'] = activation
        selected['ready'] = select('ready', generation)
        if activation['normal_source'] == 'observation':
            attempt = activation['normal_attempt']
            if type(attempt) is not int or attempt <= 0:
                raise JournalError('selected observation attempt is invalid')
            settled = select('observe-settled', generation, attempt)
            if (settled['outcome'] != 'normal_observed_refenced' or
                    settled['normal_digest'] != activation['normal_digest']):
                raise JournalError('selected observation settlement differs from activation')
            selected['observe-settled'] = settled
        elif activation['normal_source'] != 'original' or activation['normal_attempt'] != 0:
            raise JournalError('migration selected normal source is invalid')
        return selected

    def _command(self, command, name, request, realm=False):
        path = _private_request(name + '.json', request)
        self.action_order.append(command)
        transition = FenceTransition(self.life, self.postgres, self.recovery_id) if command == 'legacy-retirement-preview' else None
        factory = self._migration_channel if command == 'legacy-c81-migrate' else (self._channel if realm else None)
        result = self.invoker.admin(command, path, factory, time.monotonic() + 900,
                                    recovery_id=self.recovery_id,
                                    endpoint_container_id=self.postgres.containers['app']['id'],
                                    transition=transition)
        value = _output(result)
        if command == 'legacy-c80-service':
            if set(value) != {'version', 'status', 'operation_id', 'reason'} or type(value['version']) is not int or value['version'] != 1:
                raise JournalError('legacy service response frame differs')
            value = {name: value[name] for name in ('status', 'operation_id', 'reason')}
        return value

    def _fault_command(self, command, name, request, realm=False):
        if self.fault_scenario is None or self.fault_observation is not None:
            raise JournalError('fault scenario already consumed')
        path = _private_request(name + '.json', request)
        self.action_order.append(command)
        def factory(identity, pidfd, sock):
            coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                           'executable_sha256': identity['exe_sha256']}
            source = {'deployment_id': self.app['deployment_id'], 'project_id': 'default',
                      'operation_id': self.source_operation_id, 'actor_id': self.app['actor_user_id'],
                      'application': self.app['application'], 'recovery_id': self.recovery_id,
                      'source_kind': self.pair['a']['source_kind'], 'source_id': self.pair['a']['source_id'],
                      'record_id': self.pair['b']['record_id'], 'realm_digest': self.journal.realm_digest()}
            if self.fault_boundary == 'anchor-fsync':
                source['primary_identity'] = self.domains['w1']
                source['witness_identity'] = self.domains['w2']
            barrier = FaultBarrier(self.fault_scenario, secrets.token_hex(32), self.journal,
                                   self.app['application'], self.recovery_id, coordinator, pidfd, sock, source,
                                   self.life.callbacks, hold_backend=self.negative_scenario == 'unsettled-backend')
            self.fault_barrier = barrier
            return barrier
        channel_factory = self._migration_channel if command == 'legacy-c81-migrate' else (self._channel if realm else None)
        result = self.invoker.admin(command, path, channel_factory,
                                    time.monotonic() + 900, fault_factory=factory,
                                    recovery_id=self.recovery_id,
                                    endpoint_container_id=self.postgres.containers['app']['id'])
        fault = result.get('fault')
        if (type(fault) is not dict or fault.get('scenario') != self.fault_scenario or
                fault.get('ready_received') is not True or fault.get('armed_received') is not True or
                fault.get('victim_pidfd_exited') is not True):
            raise JournalError('selected cutpoint was not reached')
        self.fault_observation = fault
        return result

    def _readback(self):
        endpoint = self.postgres.observe(self.life.recoveries)
        value = pg_bootstrap.observe_records(self.postgres.passwords['app'], endpoint['app_host'],
                                              self.pair['a'], self.pair['b'])
        if (type(value) is not dict or set(value) != {'system_identifier', 'database_oid', 'a_record', 'b_record',
                                                     'a_asset', 'a_reservations', 'b_reservations', 'b_preview_rows', 'b_operations'} or
                value['system_identifier'] != self.app['application']['SystemIdentifier'] or
                value['database_oid'] != self.app['application']['DatabaseOID'] or
                value['a_record'] != 'active' or value['b_record'] != 'active' or value['a_asset'] is not True or
                type(value['a_reservations']) is not int or type(value['b_reservations']) is not int or
                type(value['b_preview_rows']) is not list or type(value['b_operations']) is not list):
            raise JournalError('APP readback or source retention differs')
        return value

    def retire_source(self):
        a = self.pair['a']
        source = {'deployment_id': self.app['deployment_id'], 'project_id': 'default',
                  'kind': a['source_kind'], 'id': a['source_id']}
        preview_request = dict(self._base(), actor_user_id=self.app['actor_user_id'], source=source,
                               final_floor=a['source_scope'], output_request_file=REQUESTS + '/source-original.json')
        preview = self._command('legacy-retirement-preview', 'source-preview', preview_request)
        if (preview.get('version') != 1 or preview.get('state') != 'preview_recorded' or
                preview.get('deletion_contract_version') != 2 or preview.get('source') != source or
                type(preview.get('operation_id')) is not str or not preview['operation_id']):
            raise JournalError('source preview result differs')
        self.source_operation_id = preview['operation_id']
        resume = dict(self._base(), original_request_file=REQUESTS + '/source-original.json')
        if self.fault_boundary in ('app-commit', 'w1-commit', 'w2-commit', 'anchor-fsync'):
            self._fault_command('legacy-retire', 'source-retire-cut', resume)
        retirement = self._command('legacy-retire', 'source-retire', resume)
        if (retirement.get('version') != 1 or retirement.get('state') != 'witnessed_handoff_pending' or
                retirement.get('operation_id') != preview['operation_id'] or retirement.get('source') != source or
                type(retirement.get('ledger_sequence')) is not int or retirement['ledger_sequence'] <= 0 or
                type(retirement.get('ledger_entry_hash')) is not str or HEX.fullmatch(retirement['ledger_entry_hash']) is None or
                retirement.get('source_authorization_permanently_retired') is not True or
                retirement.get('source_asset_content_retained') is not True or
                retirement.get('w2_legacy_blocker_unchanged') is not True):
            raise JournalError('source retirement result differs')
        if self.fault_boundary == 'anchor-fsync':
            fault = self.fault_observation
            saved = strict_json(read_root(self.journal._fd, 'fault-intent-anchor-fsync.json'))
            evidence = saved.get('anchor_evidence') if type(saved) is dict else None
            if (type(fault) is not dict or type(saved) is not dict or fault.get('receipt_name') != 'anchor-%020d.json' % 1 or
                    saved.get('receipt_name') != fault['receipt_name'] or saved.get('evidence_digest') != fault.get('evidence_digest') or
                    type(evidence) is not dict or set(evidence) != {'anchor_raw', 'previous_raw', 'preview_anchor_index'} or
                    type(evidence['preview_anchor_index']) is not int or evidence['preview_anchor_index'] != 0 or
                    type(evidence['anchor_raw']) is not str or
                    type(evidence['previous_raw']) is not str):
                raise JournalError('source anchor cutpoint evidence is missing')
            directory = open_control_dir()
            try:
                names = sorted(name for name in os.listdir(directory) if name.startswith('anchor-'))
                if names != ['anchor-%020d.json' % index for index in range(2)]:
                    raise JournalError('source anchor inventory changed after recovery')
                anchor = read_private(directory, fault['receipt_name'])
                previous = read_private(directory, names[0])
                if (anchor != evidence['anchor_raw'].encode('utf-8') or previous != evidence['previous_raw'].encode('utf-8') or
                        hashlib.sha256(anchor).hexdigest() != fault['evidence_digest']):
                    raise JournalError('witnessed source anchor changed after recovery')
            finally:
                os.close(directory)
        self.source_retirement = retirement
        return {'preview': preview, 'retirement': retirement}

    def _effect_snapshot(self):
        directory = open_control_dir()
        try:
            names = sorted(name for name in os.listdir(directory) if name.startswith(('phase-', 'predecessor-', 'source-')))
            result = {}
            for name in names:
                if name.startswith('predecessor-stdout-'):
                    match = re.fullmatch(r'predecessor-stdout-([0-9]{20})\.json', name)
                    if match is None:
                        raise JournalError('malformed stdout carrier name')
                    raw = read_stdout_private(directory, int(match.group(1)))
                else:
                    raw = read_private(directory, name)
                result[name] = hashlib.sha256(raw).hexdigest()
            return result
        finally:
            os.close(directory)

    def reject_stale_reply(self):
        if not self.stale_reply or self.parent_channels:
            raise JournalError('stale reply fixture was not uniquely selected')
        baseline = self._readback()
        if baseline['a_reservations'] or baseline['b_reservations'] or baseline['b_preview_rows'] or baseline['b_operations']:
            raise JournalError('stale reply baseline is not empty')
        phase = read_phases(self.app['application'], self.recovery_id)
        if phase['phase'] != 'quiescent':
            raise JournalError('stale reply fixture requires quiescent maintenance')
        before = self._effect_snapshot()
        permits = sorted(name for name in os.listdir(self.journal._fd) if name.startswith(('launch-authority-', 'launch-used-')))
        request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair['b']['record_id'])
        path = _private_request('service-stale.json', request)
        self.life.transition('predecessor')
        result = self.invoker.admin('legacy-c80-service', path, self._channel, time.monotonic() + 900,
                                    recovery_id=self.recovery_id,
                                    endpoint_container_id=self.postgres.containers['app']['id'])
        self.life.transition('refencing')
        self.life.transition('source_maintenance')
        frame = strict_json(result['stdout']) if result['stdout'] else None
        expected = {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'realm_unavailable'}
        if (result['status'] != 1 or result['stderr'] or frame != expected or type(frame.get('version')) is not int or
                len(self.parent_channels) != 1 or self.parent_channels[0].injected is None):
            raise JournalError('coordinator did not explicitly reject stale realm observation')
        injection = self.parent_channels[0].injected
        if (injection['coordinator_pid'] != result['pid'] or injection['phase_generation'] != phase['generation'] or
                injection['request_nonce'] == injection['response_nonce'] or injection['observation_sequence'] != 1 or
                read_phases(self.app['application'], self.recovery_id) != phase or self._effect_snapshot() != before or
                sorted(name for name in os.listdir(self.journal._fd) if name.startswith(('launch-authority-', 'launch-used-'))) != permits or
                self.life.origins.allowed_predecessor):
            raise JournalError('stale reply created a callback or altered maintenance state')
        after = self._readback()
        if after != baseline:
            raise JournalError('stale reply changed APP preview, reservation or source state')
        return {'request': injection, 'response': frame, 'exit_status': result['status'],
                'quiescent_phase_unchanged': True, 'durable_effects_unchanged': True,
                'launch_authority_unchanged': True, 'app_before': baseline, 'app_after': after}

    def reject_extra_session(self):
        baseline = self._readback()
        if baseline['a_reservations'] or baseline['b_reservations'] or baseline['b_preview_rows'] or baseline['b_operations']:
            raise JournalError('extra-session baseline is not empty')
        phase = read_phases(self.app['application'], self.recovery_id)
        if phase['phase'] != 'quiescent' or self.source_operation_id is None:
            raise JournalError('extra-session fixture requires the original source and quiescent phase')
        before = self._effect_snapshot()
        permits = sorted(name for name in os.listdir(self.journal._fd) if name.startswith(('launch-authority-', 'launch-used-')))
        request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair['b']['record_id'])
        path = _private_request('service-extra-session.json', request)
        barriers = []
        def factory(identity, pidfd, sock):
            coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                           'executable_sha256': identity['exe_sha256']}
            barrier = ExtraSessionBarrier(secrets.token_hex(32), self.life, self.postgres, self.app['application'],
                                          self.recovery_id, self.source_operation_id, coordinator, pidfd, sock, self.app_facts)
            barriers.append(barrier)
            return barrier
        self.life.transition('predecessor')
        joined = False
        try:
            result = self.invoker.admin('legacy-c80-service', path, self._channel, time.monotonic() + 900,
                                        fault_factory=factory,
                                        recovery_id=self.recovery_id,
                                        endpoint_container_id=self.postgres.containers['app']['id'])
            if len(barriers) != 1:
                raise JournalError('extra-session cutpoint channel missing')
            observation = barriers[0].finish()
            joined = True
        finally:
            if not joined and barriers and barriers[0].session is not None:
                barriers[0].session.close()
        frame = strict_json(result['stdout']) if result['stdout'] else None
        expected = {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'unexpected_session'}
        if (result['status'] != 1 or result['stderr'] or frame != expected or type(frame.get('version')) is not int or
                observation['coordinator']['pid'] != result['pid'] or observation['phase_generation'] != phase['generation'] or
                read_phases(self.app['application'], self.recovery_id) != phase or self._effect_snapshot() != before or
                sorted(name for name in os.listdir(self.journal._fd) if name.startswith(('launch-authority-', 'launch-used-'))) != permits or
                self.life.origins.allowed_predecessor or not self.life.invalid):
            raise JournalError('actual extra-session admission did not fail closed')
        after = self._readback()
        if after != baseline:
            raise JournalError('extra-session rejection changed APP state')
        return {'fault': observation, 'response': frame, 'exit_status': result['status'],
                'quiescent_phase_unchanged': True, 'durable_effects_unchanged': True,
                'launch_authority_unchanged': True, 'app_before': baseline, 'app_after': after}

    def _callback_names(self):
        directory = open_control_dir()
        try:
            return {name for name in os.listdir(directory)
                    if re.fullmatch(r'predecessor-callback-result-[0-9]{20}\.json', name)}
        finally:
            os.close(directory)

    def _effect_evidence(self, generation):
        join = strict_json(read_root(self.journal._fd, 'callback-join-%020d.json' % generation))
        files = join.get('effect_files') if type(join) is dict else None
        if type(files) is not dict or len(files) > 7:
            raise JournalError('callback join effect inventory is invalid')
        directory = open_control_dir()
        try:
            result = {}
            for name, digest in files.items():
                if (re.fullmatch(r'predecessor-(ready|create-intent|create-result|reserve-intent|reserve-result|stdout|result-written)-%020d\.json' % generation, name) is None or
                        type(digest) is not str or HEX.fullmatch(digest) is None):
                    raise JournalError('callback join effect name or digest is invalid')
                raw = read_stdout_private(directory, generation) if name.startswith('predecessor-stdout-') else read_private(directory, name)
                if hashlib.sha256(raw).hexdigest() != digest or type(strict_json(raw)) is not dict:
                    raise JournalError('callback witnessed effect is missing or changed')
                result[name] = raw.decode('utf-8', 'strict')
            return result
        finally:
            os.close(directory)

    def _recovery_evidence(self, fault, result):
        generation = fault['phase_generation']
        directory = open_control_dir()
        try:
            intent_raw = read_private(directory, 'predecessor-callback-intent-%020d.json' % generation)
            resolution_raw = read_private(directory, 'predecessor-resolution-%020d.json' % generation)
        finally:
            os.close(directory)
        intent, resolution = strict_json(intent_raw), strict_json(resolution_raw)
        join_name = 'callback-join-%020d.json' % generation
        join = strict_json(read_root(self.journal._fd, join_name))
        if (type(intent) is not dict or type(resolution) is not dict or type(join) is not dict or
                set(resolution) != {'version', 'phase_generation', 'recovery_id', 'application', 'realm_digest',
                                    'backend', 'create_command_digest', 'reserve_command_digest', 'reservation_id',
                                    'operation_id', 'outcome'} or
                resolution['version'] != 1 or resolution['phase_generation'] != generation or
                resolution['application'] != self.app['application'] or resolution['recovery_id'] != self.recovery_id or
                resolution['realm_digest'] != self.journal.realm_digest() or
                join.get('intent_sha256') != hashlib.sha256(intent_raw).hexdigest() or
                join.get('original_coordinator') != intent.get('coordinator') or
                intent.get('actor_user_id') != self.app['actor_user_id'] or
                intent.get('record_id') != self.pair['b']['record_id']):
            raise JournalError('original callback resolution binding differs')
        expected = {'create-intent': 'preview_not_created', 'create-commit-before-result': 'preview_abandoned',
                    'preview-ack-loss': 'preview_abandoned', 'reserve-intent': 'reserve_not_committed_preview_abandoned',
                    'reserve-commit-before-stdout': 'reserve_committed', 'stdout-loss': 'reserve_committed'}
        outcome = expected.get(fault['boundary'])
        if resolution['outcome'] != outcome:
            raise JournalError('original callback resolution outcome differs')
        if outcome == 'reserve_committed':
            if result != {'status': 'recovered_committed', 'operation_id': resolution['operation_id'],
                          'reason': 'reserve_committed'} or not resolution['operation_id']:
                raise JournalError('recovered operation differs from original reservation')
        elif resolution['operation_id'] or result.get('status') != 'committed':
            raise JournalError('abandoned original callback did not start a new real service call')
        return {'phase_generation': generation, 'intent_raw': intent_raw.decode('utf-8', 'strict'),
                'resolution_raw': resolution_raw.decode('utf-8', 'strict'), 'effect_raw': self._effect_evidence(generation),
                'root_join_name': join_name, 'root_join_sha256': hashlib.sha256(canonical(join)).hexdigest(),
                'next_callback_phase': None}

    def _callback(self, previous, record_name, result):
        added = self._callback_names() - previous
        if len(added) != 1 or not self.parent_channels:
            raise JournalError('exact callback result is unavailable')
        name = added.pop()
        directory = open_control_dir()
        try:
            raw = read_private(directory, name)
        finally:
            os.close(directory)
        receipt = strict_json(raw)
        keys = {'version', 'phase_generation', 'final_phase_generation', 'application', 'recovery_id',
                'actor_user_id', 'record_id', 'coordinator', 'child', 'backend', 'events', 'child_result', 'exit'}
        phase = read_phases(self.app['application'], self.recovery_id)
        if (type(receipt) is not dict or set(receipt) != keys or type(receipt['version']) is not int or
                receipt['version'] != 1 or type(receipt['phase_generation']) is not int or
                name != 'predecessor-callback-result-%020d.json' % receipt['phase_generation'] or
                receipt['application'] != self.app['application'] or receipt['recovery_id'] != self.recovery_id or
                receipt['actor_user_id'] != self.app['actor_user_id'] or receipt['record_id'] != self.pair[record_name]['record_id'] or
                phase['phase'] != 'quiescent' or receipt['final_phase_generation'] != phase['generation'] or
                receipt['coordinator'] != self.parent_channels[-1].coordinator):
            raise JournalError('callback result identity or final phase differs')
        child, backend, outcome, exit_info = receipt['child'], receipt['backend'], receipt['child_result'], receipt['exit']
        if (type(child) is not dict or set(child) != {'pid', 'starttime', 'executable_sha256'} or
                child['executable_sha256'] != self.expected['c80-predecessor'] or
                type(backend) is not dict or set(backend) != {'pid', 'started', 'role'} or
                type(backend['pid']) is not int or backend['pid'] <= 0 or
                type(backend['started']) is not str or not backend['started'] or backend['role'] != self.app['runtime_role'] or
                type(outcome) is not dict or outcome != {'version': 1, 'phase_generation': receipt['phase_generation'], **result} or
                type(exit_info) is not dict or exit_info != {'pidfd_exited': True, 'wait_status': 0}):
            raise JournalError('callback child, backend or original service result differs')
        events = receipt['events']
        operations = {'ready', 'inventory_observe', 'witness_head', 'witness_extension',
                      'preview_intent', 'preview_result', 'reserve_intent', 'reserve_result', 'result_written'}
        if type(events) is not list or not events:
            raise JournalError('callback event trace missing')
        source_events, seen_effects = [], set()
        for sequence, event in enumerate(events, 1):
            if (type(event) is not dict or type(event.get('sequence')) is not int or event['sequence'] != sequence):
                raise JournalError('callback event sequence invalid')
            if event.get('op') == 'source_check':
                if (set(event) != {'sequence', 'op', 'source', 'allowed'} or type(event['allowed']) is not bool or
                        type(event['source']) is not dict or set(event['source']) != {'deployment_id', 'project_id', 'kind', 'id'}):
                    raise JournalError('source check trace invalid')
                source_events.append(event)
            else:
                op = event.get('op')
                replay = event.get('replay', False)
                if (op not in operations or type(replay) is not bool or
                        set(event) != ({'sequence', 'op', 'replay'} if replay else {'sequence', 'op'}) or
                        replay and (op not in {'preview_intent', 'preview_result', 'reserve_intent', 'reserve_result', 'result_written'} or op not in seen_effects)):
                    raise JournalError('callback event is not admitted')
                seen_effects.add(op)
        retired = {'deployment_id': self.app['deployment_id'], 'project_id': 'default',
                   'kind': self.pair['a']['source_kind'], 'id': self.pair['a']['source_id']}
        counts = {op: sum(event['op'] == op and not event.get('replay', False) for event in events)
                  for op in ('ready', 'preview_intent', 'preview_result', 'reserve_intent', 'reserve_result', 'result_written')}
        if result['status'] == 'rejected':
            if (result['reason'] != 'source_denied' or result['operation_id'] != '' or
                    not any(event['source'] == retired and event['allowed'] is False for event in source_events) or
                    counts != {'ready': 1, 'preview_intent': 0, 'preview_result': 0, 'reserve_intent': 0,
                               'reserve_result': 0, 'result_written': 1}):
                raise JournalError('actual source-denied Preview evidence is incomplete')
        elif result['status'] == 'committed':
            if (result['reason'] != 'app_reserved' or not source_events or
                    any(event['allowed'] is not True for event in source_events) or
                    counts != {'ready': 1, 'preview_intent': 1, 'preview_result': 1, 'reserve_intent': 1,
                               'reserve_result': 1, 'result_written': 1}):
                raise JournalError('actual Preview and Reserve trace is incomplete')
        else:
            raise JournalError('callback result is not an admitted service outcome')
        endpoint = self.postgres.observe(self.life.recoveries)
        join_name, join_digest = self.life.callbacks.finish_success(
            receipt['phase_generation'], self.app_facts, endpoint, time.monotonic() + 60,
            child, backend, receipt['coordinator'])
        self.journal.append('exit', {'name': 'c80-predecessor', 'pid': child['pid'], 'starttime': child['starttime'],
                                     'status': exit_info['wait_status'], 'pidfd_exited': True})
        effects = self._effect_evidence(receipt['phase_generation'])
        expected = {'predecessor-%s-%020d.json' % (kind, receipt['phase_generation']) for kind in
                    (('ready', 'stdout', 'result-written') if result['status'] == 'rejected' else
                     ('ready', 'create-intent', 'create-result', 'reserve-intent', 'reserve-result', 'stdout', 'result-written'))}
        if set(effects) != expected:
            raise JournalError('callback durable effect inventory differs')
        return {'receipt_name': name, 'receipt_sha256': hashlib.sha256(raw).hexdigest(),
                'receipt_raw': raw.decode('utf-8', 'strict'), 'root_pidfd_exited': True,
                'root_join_name': join_name, 'root_join_sha256': join_digest, 'effect_raw': effects, 'trace': receipt}

    def reject_intersecting_closure(self):
        _source_fixture(self.app, True)
        baseline = self._readback()
        if baseline['a_reservations'] or baseline['b_reservations'] or baseline['b_preview_rows'] or baseline['b_operations']:
            raise JournalError('overlapping closure baseline is not empty')
        previous = self._callback_names()
        request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair['b']['record_id'])
        self.life.transition('predecessor')
        result = self._command('legacy-c80-service', 'service-intersecting', request, realm=True)
        callback = self._callback(previous, 'b', result)
        self.life.transition('refencing')
        self.life.transition('source_maintenance')
        after = self._readback()
        if result != {'status': 'rejected', 'operation_id': '', 'reason': 'source_denied'} or after != baseline:
            raise JournalError('overlapping source closure did not reject the real Preview')
        return {'records': self.pair, 'service_result': result, 'callback': callback,
                'app_before': baseline, 'app_after': after}

    def unsettled_backend_negative(self):
        if self.negative_scenario != 'unsettled-backend' or self.fault_scenario != 'reserve-intent-coordinator':
            raise JournalError('unsettled backend selector differs')
        request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair['b']['record_id'])
        self.life.transition('predecessor')
        try:
            self._fault_command('legacy-c80-service', 'negative-unsettled-cut', request, realm=True)
            self.life.transition('refencing')
            self.life.transition('source_maintenance')
            held = self.fault_barrier.held_backend
            if held is None:
                raise JournalError('actual original backend was not stopped')
            identity = held['identity']
            endpoint = self.postgres.observe(self.life.recoveries)
            sessions_before = _sessions(self.life, self.app_facts, endpoint)
            if (len(sessions_before) != 1 or sessions_before[0]['pid'] != identity['namespace_pid'] or
                    sessions_before[0]['role'] != identity['backend_role']):
                raise JournalError('stopped original backend is not the registered APP session')
            before, previous = self._readback(), self._callback_names()
            self.life.transition('predecessor')
            path = _private_request('negative-unsettled-recover.json', request)
            self.action_order.append('legacy-c80-service')
            result = self.invoker.admin('legacy-c80-service', path, self._channel, time.monotonic() + 120,
                                        recovery_id=self.recovery_id,
                                        endpoint_container_id=self.postgres.containers['app']['id'])
            response = strict_json(result['stdout']) if result['stdout'] else None
            sessions_after = _sessions(self.life, self.app_facts, endpoint)
            after = self._readback()
            phase = read_phases(self.app['application'], self.recovery_id)
            generation = self.fault_observation['phase_generation']
            directory = open_control_dir()
            try:
                create_raw = read_private(directory, 'predecessor-create-intent-%020d.json' % generation)
                reserve_raw = read_private(directory, 'predecessor-reserve-intent-%020d.json' % generation)
                if hashlib.sha256(reserve_raw).hexdigest() != self.fault_observation['evidence_digest']:
                    raise JournalError('held backend original reserve receipt changed')
                try:
                    read_private(directory, 'predecessor-resolution-%020d.json' % generation)
                except FileNotFoundError:
                    pass
                else:
                    raise JournalError('unsettled backend unexpectedly produced a resolution')
            finally:
                os.close(directory)
            if (result['status'] != 1 or result['stderr'] or response !=
                    {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'service_unavailable'} or
                    previous != self._callback_names() or phase['phase'] != 'refencing' or before != after or
                    after['b_operations'] or sessions_after != sessions_before or
                    select.select([held['pidfd']], [], [], 0)[0] or process_start(identity['pid']) != identity['starttime']):
                raise JournalError('unsettled original backend did not retain the closed fence')
            self.life.invalidate('negative_unsettled_backend')
            exited = self.fault_barrier.release_backend(time.monotonic() + 15, True)
            if not exited or _sessions(self.life, self.app_facts, endpoint):
                raise JournalError('original backend did not settle after negative fixture cleanup')
            return {'scenario': 'unsettled-backend', 'fault': self.fault_observation, 'held_backend': identity,
                    'sessions_before': sessions_before, 'sessions_after': sessions_after,
                    'backend_pidfd_exited_after_release': exited, 'response': response, 'exit_status': result['status'],
                    'app_before': before, 'app_after': after, 'phase': phase,
                    'create_raw': create_raw.decode('utf-8', 'strict'), 'reserve_raw': reserve_raw.decode('utf-8', 'strict')}
        finally:
            if self.fault_barrier is not None and self.fault_barrier.held_backend is not None:
                self.fault_barrier.release_backend(time.monotonic(), False)

    def recovery_negative(self, scenario):
        if scenario not in ('conflicting-tuple', 'missing-journal') or self.fault_scenario != 'reserve-intent-coordinator':
            raise JournalError('callback recovery negative selector differs')
        request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair['b']['record_id'])
        baseline = self._readback()
        if baseline['a_reservations'] or baseline['b_reservations'] or baseline['b_operations']:
            raise JournalError('negative callback baseline is not empty')
        self.life.transition('predecessor')
        self._fault_command('legacy-c80-service', 'negative-service-cut', request, realm=True)
        self.life.transition('refencing')
        self.life.transition('source_maintenance')
        fault = self.fault_observation
        generation = fault['phase_generation']
        directory = open_control_dir()
        try:
            create_raw = read_private(directory, 'predecessor-create-intent-%020d.json' % generation)
            reserve_name = 'predecessor-reserve-intent-%020d.json' % generation
            reserve_raw = read_private(directory, reserve_name)
            create, reserve = strict_json(create_raw), strict_json(reserve_raw)
            reservation = reserve.get('command', {}).get('reservation_id')
            original = create.get('command', {}).get('request_fingerprint')
            if (type(reservation) is not str or re.fullmatch(r'drs_[a-z0-9]{1,64}', reservation) is None or
                    type(original) is not str or HEX.fullmatch(original) is None or
                    hashlib.sha256(reserve_raw).hexdigest() != fault['evidence_digest']):
                raise JournalError('negative original reserve tuple differs')
            action = {'scenario': scenario, 'phase_generation': generation, 'reservation_id': reservation,
                      'receipt_sha256': fault['evidence_digest']}
            self.life._admit('negative-' + scenario, action)
            tuple_before = tuple_after = None
            if scenario == 'missing-journal':
                os.unlink(reserve_name, dir_fd=directory)
                os.fsync(directory)
                try:
                    read_private(directory, reserve_name)
                except FileNotFoundError:
                    pass
                else:
                    raise JournalError('witnessed reserve receipt was not removed')
            else:
                changed = hashlib.sha256(b'g2-conflicting-reservation-fingerprint').hexdigest()
                if changed == original:
                    raise JournalError('negative conflicting fingerprint did not differ')
                endpoint = self.postgres.observe(self.life.recoveries)
                sql = ("WITH changed AS (UPDATE public.deletion_reservations SET request_fingerprint=decode('%s','hex') "
                       "WHERE reservation_id='%s' AND encode(request_fingerprint,'hex')='%s' AND state='previewed' "
                       "AND fence_epoch=0 RETURNING reservation_id,encode(request_fingerprint,'hex') AS fingerprint,state,fence_epoch) "
                       "SELECT json_build_object('count',count(*),'rows',coalesce(json_agg(row_to_json(changed)),'[]'::json))::text AS state FROM changed\0") % (
                           changed, reservation, original)
                tuple_after = pg_bootstrap._run('app', self.postgres.passwords['app'], 'records', sql.encode('ascii'),
                                                host_override=endpoint['app_host'])
                tuple_before = {'reservation_id': reservation, 'fingerprint': original}
                if (type(tuple_after) is not dict or tuple_after.get('count') != 1 or
                        tuple_after.get('rows') != [{'reservation_id': reservation, 'fingerprint': changed,
                                                     'state': 'previewed', 'fence_epoch': 0}]):
                    raise JournalError('real conflicting reservation tuple was not created')
            self.life._register('negative-' + scenario, {'action': action, 'tuple_before': tuple_before,
                                                         'tuple_after': tuple_after})
        finally:
            os.close(directory)
        before = self._readback()
        callbacks = self._callback_names()
        self.life.transition('predecessor')
        path = _private_request('negative-service-recover.json', request)
        self.action_order.append('legacy-c80-service')
        result = self.invoker.admin('legacy-c80-service', path, self._channel, time.monotonic() + 120,
                                    recovery_id=self.recovery_id,
                                    endpoint_container_id=self.postgres.containers['app']['id'])
        response = strict_json(result['stdout']) if result['stdout'] else None
        after = self._readback()
        phase = read_phases(self.app['application'], self.recovery_id)
        if (result['status'] != 1 or result['stderr'] or
                response != {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'service_unavailable'} or
                callbacks != self._callback_names() or before != after or after['b_operations'] or
                phase['phase'] != 'refencing'):
            raise JournalError('original callback negative was not rejected with its fence intact')
        resolution_raw = None
        directory = open_control_dir()
        try:
            try:
                resolution_raw = read_private(directory, 'predecessor-resolution-%020d.json' % generation)
            except FileNotFoundError:
                pass
        finally:
            os.close(directory)
        if scenario == 'conflicting-tuple':
            resolution = strict_json(resolution_raw) if resolution_raw else None
            if type(resolution) is not dict or resolution.get('outcome') != 'unresolved':
                raise JournalError('conflicting original tuple was not durably unresolved')
            self.life.invalidate('negative_conflicting_tuple')
        elif not self.life.invalid or resolution_raw is not None:
            raise JournalError('missing witnessed receipt did not invalidate the realm before resolution')
        return {'scenario': scenario, 'fault': fault, 'create_raw': create_raw.decode('utf-8', 'strict'),
                'reserve_raw': reserve_raw.decode('utf-8', 'strict'), 'tuple_before': tuple_before, 'tuple_after': tuple_after,
                'response': response, 'exit_status': result['status'], 'app_before': before, 'app_after': after,
                'phase': phase, 'effect_raw': self._effect_evidence(generation) if scenario == 'conflicting-tuple' else None,
                'resolution_raw': None if resolution_raw is None else resolution_raw.decode('utf-8', 'strict')}

    def predecessor(self):
        baseline = self._readback()
        if baseline['a_reservations'] or baseline['b_reservations'] or baseline['b_operations']:
            raise JournalError('predecessor records already have reservations')
        self.callback_evidence = []
        self.callback_recovery = None
        results = []
        for name in ('a', 'b'):
            request = dict(self._base(), actor_user_id=self.app['actor_user_id'], record_id=self.pair[name]['record_id'])
            self.life.transition('predecessor')
            if name == 'b' and self.fault_boundary in ('create-intent', 'create-commit-before-result', 'preview-ack-loss',
                                                        'reserve-intent', 'reserve-commit-before-stdout', 'stdout-loss'):
                self._fault_command('legacy-c80-service', 'service-b-cut', request, realm=True)
                self.life.transition('refencing')
                self.life.transition('source_maintenance')
                self.life.transition('predecessor')
            previous = self._callback_names()
            result = self._command('legacy-c80-service', 'service-' + name, request, realm=True)
            fault = self.fault_observation if name == 'b' and self.fault_boundary in (
                'create-intent', 'create-commit-before-result', 'preview-ack-loss', 'reserve-intent',
                'reserve-commit-before-stdout', 'stdout-loss') else None
            if fault is not None:
                self.callback_recovery = self._recovery_evidence(fault, result)
            if result['status'] == 'recovered_committed':
                if fault is None or self._callback_names() != previous:
                    raise JournalError('committed recovery unexpectedly launched another callback')
            else:
                callback = self._callback(previous, name, result)
                self.callback_evidence.append(callback)
                if fault is not None:
                    self.callback_recovery['next_callback_phase'] = callback['trace']['phase_generation']
                    if self.callback_recovery['next_callback_phase'] <= fault['phase_generation']:
                        raise JournalError('abandoned preview did not move to a new phase')
            self.life.transition('refencing')
            self.life.transition('source_maintenance')
            results.append((result, self._readback()))
        denied, reserved = results
        if (denied[0] != {'status': 'rejected', 'operation_id': '', 'reason': 'source_denied'} or
                denied[1]['a_reservations'] != 0 or denied[1]['b_reservations'] != 0 or denied[1]['b_operations']):
            raise JournalError('source-denied APP readback differs')
        operation = reserved[0].get('operation_id')
        valid_result = (reserved[0]['status'], reserved[0]['reason']) in (
            ('committed', 'app_reserved'), ('recovered_committed', 'reserve_committed'))
        if (set(reserved[0]) != {'status', 'operation_id', 'reason'} or not valid_result or type(operation) is not str or
                re.fullmatch(r'rpo_[0-9a-f]{16}', operation) is None or reserved[1]['a_reservations'] != 0 or
                reserved[1]['b_reservations'] < 1 or reserved[1]['b_reservations'] > (2 if self.fault_boundary else 1) or
                len(reserved[1]['b_operations']) != 1):
            raise JournalError('provisional reservation APP readback differs')
        row = reserved[1]['b_operations'][0]
        if (row.get('operation_id') != operation or row.get('object_id') != self.pair['b']['record_id'] or
                row.get('project_id') != 'default' or row.get('actor_id') != self.app['actor_user_id'] or
                row.get('deployment_id') != self.app['deployment_id'] or row.get('contract_version') != 1 or
                row.get('operation_state') != 'provisional_fenced' or row.get('reason_code') != 'user_confirmed' or
                row.get('reservation_state') != 'fenced' or type(row.get('fence_epoch')) is not int or row['fence_epoch'] <= 0 or
                row.get('ledger_sequence') is not None or row.get('ledger_hash_null') is not True or
                row.get('owner_id') != 'c80_predecessor' or row.get('owner_id') != row.get('reservation_owner_id') or
                row.get('owner_generation') != row.get('reservation_owner_generation') or row.get('owner_expiry_equal') is not True):
            raise JournalError('real provisional operation fields differ')
        abandoned = [item for item in reserved[1]['b_preview_rows'] if item.get('reservation_id') != row['reservation_id']]
        if (len(reserved[1]['b_preview_rows']) != reserved[1]['b_reservations'] or
                any(item.get('state') != 'previewed' or item.get('fence_epoch') != 0 or item.get('operations') != 0
                    for item in abandoned)):
            raise JournalError('abandoned preview has an unexpected fence or operation')
        return {'baseline': baseline, 'denied': denied[1], 'reserved': reserved[1], 'operation_id': operation,
                'service_results': [denied[0], reserved[0]]}

    def handoff(self, operation_id):
        if self.life.invalid and not self.life.closed_maintenance:
            self.life.enter_closed_maintenance()
        self.life.transition('c81_migration')
        endpoint = self.postgres.observe(self.life.recoveries)
        def verify_backend(backend, deadline):
            deadline -= 15
            if time.monotonic() >= deadline:
                raise JournalError('migration work budget expired before backend observation')
            if self.postgres.observe(self.life.recoveries, deadline=deadline) != endpoint:
                raise JournalError('migration APP endpoint changed before backend readback')
            observed = pg_bootstrap.observe_migration_backend(self.postgres.passwords['app'], endpoint['app_host'],
                                                               self.app['application'], backend, deadline)
            if self.postgres.observe(self.life.recoveries, deadline=deadline) != endpoint:
                raise JournalError('migration APP endpoint changed after backend readback')
            return observed
        def verify_settlement(current_backend, retired_backends, managed_roles, deadline):
            deadline -= 15
            if time.monotonic() >= deadline:
                raise JournalError('migration work budget expired before settlement observation')
            if self.postgres.observe(self.life.recoveries, deadline=deadline) != endpoint:
                raise JournalError('migration APP endpoint changed before settlement')
            observed = pg_bootstrap.observe_migration_settlement(
                self.postgres.passwords['app'], endpoint['app_host'], self.app['application'],
                current_backend, retired_backends, managed_roles, deadline)
            if self.postgres.observe(self.life.recoveries, deadline=deadline) != endpoint:
                raise JournalError('migration APP endpoint changed after settlement')
            return observed
        self.life.register_migration_checkpoint(self.app['application'], self.recovery_id,
                                                self.expected['c81-migrator'], verify_backend, verify_settlement)
        if self.fault_boundary == 'c81-convergence-commit':
            self._fault_command('legacy-c81-migrate', 'c81-migrate-cut', self._base())
        migration = self._command('legacy-c81-migrate', 'c81-migrate', self._base())
        source_request = dict(self._base(), original_request_file=REQUESTS + '/source-original.json')
        if self.fault_boundary in ('handoff-install', 'handoff-return'):
            self._fault_command('legacy-retirement-reconcile', 'source-reconcile-cut', source_request)
        source = self._command('legacy-retirement-reconcile', 'source-reconcile', source_request)
        if (source.get('state') != 'handoff_installed' or source.get('source_authorization_permanently_retired') is not True or
                self.source_retirement is None or any(source.get(key) != self.source_retirement.get(key) for key in
                    ('operation_id', 'source', 'ledger_domain', 'ledger_sequence', 'ledger_entry_hash',
                     'deletion_contract_version', 'w2_legacy_blocker_unchanged'))):
            raise JournalError('source C81 handoff was not installed')
        request = dict(self._base(), deployment_id=self.app['deployment_id'], project_id='default',
                       operation_id=operation_id, ledger_sequence=0, ledger_entry_hash='', previous_sequence=0, previous_hash='')
        record = self._command('reconcile-legacy', 'record-reconcile', request)
        if (set(record) != {'ledger_domain', 'ledger_sequence', 'ledger_entry_hash', 'content_purged', 'replay_applied'} or
                type(record['ledger_sequence']) is not int or record['ledger_sequence'] <= 0 or
                type(record['ledger_entry_hash']) is not str or HEX.fullmatch(record['ledger_entry_hash']) is None or
                record['content_purged'] is not False):
            raise JournalError('record ledger reconciliation did not complete')
        self.life.transition('retired')
        return {'migration': migration, 'source_handoff': source, 'record_ledger': record,
                'maintenance_selection': self._migration_selection()}
