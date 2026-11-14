"""Fixed fault-only FD7 cutpoint barriers for real coordinator effects."""
import base64
import binascii
import fcntl
import hashlib
import json
import os
import re
import select
import signal
import socket
import time

from callback_lifecycle import read_stdout_carrier
from contract import CALLBACK_CUTS, GUARD, MAX_FRAME, RECOVERY, RUN_TOKEN
from ipc import _receive, _send
from journal import JournalError, _write_new, canonical, process_start, strict_json
from network_verify import _read as read_kernel
from origins import _cgroup_has_container, _identity
from permit import _coordinator_live
from phase import open_control_dir, read_phases, read_private

HEX = re.compile(r'^[0-9a-f]{64}$')
READY_KEYS = frozenset(('version', 'op', 'scenario', 'boundary', 'phase_generation', 'operation_id',
                        'coordinator', 'child', 'backend', 'evidence_digest', 'nonce'))
ARMED_KEYS = frozenset(('version', 'op', 'nonce', 'evidence_digest'))
COORDINATOR_BOUNDARIES = frozenset(('app-commit', 'w1-commit', 'w2-commit', 'anchor-fsync',
                                     'handoff-install', 'handoff-return'))
CREATE_DIGESTS = frozenset(('actor_scope_digest', 'token_commitment', 'request_fingerprint', 'binding_digest',
                            'dependency_digest', 'backup_digest', 'processor_digest', 'adapter_readiness_digest',
                            'adapter_preview_digest', 'witness_hash'))
CREATE_NUMBERS = frozenset(('lock_version', 'authorization_epoch', 'content_delivery_epoch', 'witness_sequence', 'ttl_microseconds'))
CREATE_KEYS = CREATE_DIGESTS | CREATE_NUMBERS | frozenset(('project_id', 'object_kind', 'object_id', 'current_revision_id'))
RESERVE_DIGESTS = frozenset(('create_command_digest', 'request_fingerprint', 'expected_binding_digest', 'observed_witness_hash'))
RESERVE_NUMBERS = frozenset(('deletion_contract_version', 'observed_witness_sequence', 'owner_lease_microseconds'))
RESERVE_KEYS = RESERVE_DIGESTS | RESERVE_NUMBERS | frozenset(('reservation_id', 'deployment_id', 'actor_id', 'owner_id', 'reason_code'))


def _command_shape(command, keys, digests, numbers):
    return (type(command) is dict and set(command) == keys and
            all(type(command[key]) is str and HEX.fullmatch(command[key]) is not None and command[key] != '0' * 64 for key in digests) and
            all(type(command[key]) is int and 0 <= command[key] < (1 << 63) for key in numbers))


def scenario_spec(scenario):
    if scenario not in RECOVERY:
        raise JournalError('fault scenario is not admitted')
    if scenario in COORDINATOR_BOUNDARIES:
        return scenario, 'coordinator'
    if scenario == 'c81-convergence-commit':
        return scenario, 'child'
    for boundary in CALLBACK_CUTS:
        for victim in ('child', 'coordinator'):
            if scenario == boundary + '-' + victim:
                return boundary, victim
    raise JournalError('scenario is not a participant cutpoint')


def _guard_contended():
    fd = os.open(GUARD, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return True
        else:
            fcntl.flock(fd, fcntl.LOCK_UN)
            return False
    finally:
        os.close(fd)


def _bytes32(value):
    if type(value) is not list or len(value) != 32 or any(type(part) is not int or not 0 <= part <= 255 for part in value):
        raise JournalError('anchor digest shape differs')
    return bytes(value)


def _anchor_head(value):
    if (type(value) is not dict or set(value) != {'Sequence', 'Hash'} or type(value['Sequence']) is not int or
            not 0 <= value['Sequence'] < (1 << 63)):
        raise JournalError('anchor ledger head shape differs')
    digest = _bytes32(value['Hash'])
    if (value['Sequence'] == 0) != (digest == bytes(32)):
        raise JournalError('anchor ledger head position differs')
    return value['Sequence'], digest


def _anchor_receipt(directory, source_key, preview, preview_raw, digest, source):
    if (set(preview) != {'version', 'action', 'key', 'request', 'recovery_id', 'phase_generation',
                         'primary_identity', 'witness_identity', 'anchor'} or type(preview['version']) is not int or
            preview['version'] != 1 or preview['action'] != 'retire_source_authorization' or
            preview['primary_identity'] != source.get('primary_identity') or
            preview['witness_identity'] != source.get('witness_identity') or
            type(preview['primary_identity']) is not dict or type(preview['primary_identity'].get('ID')) is not str or
            preview['primary_identity'].get('Kind') != 'deletion_ledger' or
            type(preview['witness_identity']) is not dict or preview['witness_identity'].get('Kind') != 'deletion_witness'):
        raise JournalError('source anchor preview identity differs')
    if type(preview['phase_generation']) is not int or not 0 < preview['phase_generation'] < (1 << 63):
        raise JournalError('source preview phase is malformed')
    for key, kind in (('primary_identity', 'deletion_ledger'), ('witness_identity', 'deletion_witness')):
        identity = preview[key]
        if (set(identity) != {'ID', 'Kind', 'SystemIdentifier', 'DatabaseOID', 'DatabaseName', 'Epoch'} or
                identity['Kind'] != kind or any(type(identity[name]) is not str or not identity[name]
                                                for name in ('ID', 'SystemIdentifier', 'DatabaseName')) or
                any(type(identity[name]) is not int or identity[name] <= 0 for name in ('DatabaseOID', 'Epoch'))):
            raise JournalError('source physical ledger identity is malformed')
    preview_anchor = preview['anchor']
    if (type(preview_anchor) is not dict or set(preview_anchor) != {'version', 'index', 'previous', 'primary', 'witness'} or
            type(preview_anchor['version']) is not int or preview_anchor['version'] != 1 or
            type(preview_anchor['index']) is not int or preview_anchor['index'] != 0 or
            _bytes32(preview_anchor['previous']) != bytes(32)):
        raise JournalError('source preview did not start from the fixture genesis anchor')
    names = sorted(name for name in os.listdir(directory) if name.startswith('anchor-'))
    if names != ['anchor-%020d.json' % index for index in range(2)]:
        raise JournalError('source fixture has extra or missing anchors')
    previous_raw, previous_heads, selected = None, None, []
    for index, name in enumerate(names):
        if name != 'anchor-%020d.json' % index:
            raise JournalError('source anchor sequence differs')
        raw = read_private(directory, name)
        anchor = strict_json(raw)
        if (type(anchor) is not dict or set(anchor) != {'version', 'index', 'previous', 'primary', 'witness'} or
                type(anchor['version']) is not int or anchor['version'] != 1 or
                type(anchor['index']) is not int or anchor['index'] != index or
                _bytes32(anchor['previous']) != (bytes(32) if previous_raw is None else hashlib.sha256(previous_raw).digest())):
            raise JournalError('source anchor lineage differs')
        heads = (_anchor_head(anchor['primary']), _anchor_head(anchor['witness']))
        encoded = {'version': anchor['version'], 'index': anchor['index'], 'previous': anchor['previous'],
                   'primary': {'Sequence': anchor['primary']['Sequence'], 'Hash': anchor['primary']['Hash']},
                   'witness': {'Sequence': anchor['witness']['Sequence'], 'Hash': anchor['witness']['Hash']}}
        if raw != json.dumps(encoded, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii'):
            raise JournalError('source anchor encoding is not the fixed Go representation')
        if index == 0 and heads != ((0, bytes(32)), (0, bytes(32))):
            raise JournalError('source anchor genesis differs')
        if previous_heads is not None and any(
                current[0] < old[0] or current[0] == old[0] and current[1] != old[1]
                for current, old in zip(heads, previous_heads)):
            raise JournalError('source anchor heads do not extend their predecessors')
        if index == preview_anchor['index'] and anchor != preview_anchor:
            raise JournalError('source preview anchor differs from its protected lineage')
        if hashlib.sha256(raw).hexdigest() == digest:
            selected.append((name, anchor, raw, previous_raw, previous_heads))
        previous_raw, previous_heads = raw, heads
    if len(selected) != 1 or selected[0][0] != names[-1]:
        raise JournalError('source anchor digest is not the unique latest anchor')
    name, anchor, raw, prior_raw, prior_heads = selected[0]
    intent_raw = read_private(directory, 'source-intent-' + source_key + '.json')
    intent = strict_json(intent_raw)
    witness_raw = read_private(directory, 'source-witnessed-' + source_key + '.json')
    witness = strict_json(witness_raw)
    if (type(intent) is not dict or set(intent) != {'version', 'kind', 'key', 'preview_digest', 'original_absent',
                                                    'expected_floor', 'expected_recorded_at', 'backend'} or
            type(intent['version']) is not int or intent['version'] != 1 or intent['key'] != source_key or
            intent['kind'] != 'source_app_commit_pending' or intent['original_absent'] is not True or
            _bytes32(intent['preview_digest']) != hashlib.sha256(preview_raw).digest() or
            type(witness) is not dict or set(witness) != {'version', 'key', 'intent_digest', 'entry', 'phase'} or
            type(witness['version']) is not int or witness['version'] != 1 or witness['key'] != source_key or
            witness['phase'] != 'handoff_pending' or _bytes32(witness['intent_digest']) != hashlib.sha256(intent_raw).digest()):
        raise JournalError('source witnessed entry is not bound to the preview and intent')
    entry = witness['entry']
    if (type(entry) is not dict or set(entry) != {'Request', 'LedgerDomain', 'Sequence', 'PreviousHash',
                                                  'ConfirmedAt', 'Canonical', 'EntryHash'} or
            entry['Request'] != preview['request'] or type(entry['Sequence']) is not int or
            not 0 < entry['Sequence'] < (1 << 63) or type(entry['ConfirmedAt']) is not str or
            not entry['ConfirmedAt'] or type(entry['Canonical']) is not str):
        raise JournalError('source witnessed entry shape differs')
    entry_hash, previous_hash, domain = (_bytes32(entry[key]) for key in ('EntryHash', 'PreviousHash', 'LedgerDomain'))
    try:
        encoded = entry['Canonical'].encode('ascii', 'strict')
        body = base64.b64decode(encoded, validate=True)
    except (UnicodeError, ValueError, binascii.Error) as exc:
        raise JournalError('source witnessed canonical entry is malformed') from exc
    if not body or len(body) > 1048576 or base64.b64encode(body) != encoded or hashlib.sha256(body).digest() != entry_hash:
        raise JournalError('source witnessed canonical entry hash differs')
    def framed(value):
        return len(value).to_bytes(4, 'big') + value
    domain_id = preview['primary_identity']['ID'].encode('utf-8')
    expected_domain = hashlib.sha256(framed(b'HOUFENG-RECORD-LEDGER-DOMAIN-V1') + framed(domain_id)).digest()
    prefix = (framed(b'HOUFENG-SOURCE-RETIREMENT-LEDGER-V1') + (1).to_bytes(8, 'big') + framed(domain) +
              entry['Sequence'].to_bytes(8, 'big') + framed(previous_hash))
    for value in ('delete_commit', source['deployment_id'], source['project_id'], source['operation_id'],
                  source['actor_id'], 'source_permanent_delete', source['source_kind'], source['source_id']):
        prefix += framed(value.encode('utf-8'))
    preview_heads = (_anchor_head(preview_anchor['primary']), _anchor_head(preview_anchor['witness']))
    target_heads = (_anchor_head(anchor['primary']), _anchor_head(anchor['witness']))
    expected_head = (entry['Sequence'], entry_hash)
    if (domain != expected_domain or not body.startswith(prefix) or entry_hash == bytes(32) or
            preview_heads[0] != preview_heads[1] or prior_heads != preview_heads or
            entry['Sequence'] != preview_heads[0][0] + 1 or previous_hash != preview_heads[0][1] or
            target_heads != (expected_head, expected_head)):
        raise JournalError('source entry and W1/W2 anchor heads differ')
    try:
        os.stat('source-anchored-' + source_key + '.json', dir_fd=directory, follow_symlinks=False)
    except FileNotFoundError:
        pass
    else:
        raise JournalError('source anchored receipt existed before anchor-fsync cutpoint')
    return name, {'anchor_raw': raw.decode('utf-8', 'strict'), 'previous_raw': prior_raw.decode('utf-8', 'strict'),
                  'preview_anchor_index': preview_anchor['index']}


def _receipt(boundary, phase, digest, source, event):
    source_key = hashlib.sha256((source['deployment_id'] + '\0' + source['project_id'] + '\0' +
                                 source['operation_id']).encode('utf-8')).hexdigest()
    carrier_digest = None
    if boundary in ('app-commit', 'w1-commit', 'w2-commit'):
        name = 'source-intent-' + source_key + '.json'
    elif boundary == 'anchor-fsync':
        name = None
    elif boundary == 'handoff-install':
        name = 'source-handoff-intent-' + source_key + '.json'
    elif boundary == 'handoff-return':
        name = 'source-handoff-installed-' + source_key + '.json'
    elif boundary == 'c81-convergence-commit':
        name = 'phase-%020d.json' % phase['generation']
    elif boundary in ('create-intent', 'create-commit-before-result'):
        name = 'predecessor-create-intent-%020d.json' % phase['generation']
    elif boundary == 'preview-ack-loss':
        name = 'predecessor-create-result-%020d.json' % phase['generation']
    elif boundary == 'reserve-intent':
        name = 'predecessor-reserve-intent-%020d.json' % phase['generation']
    elif boundary == 'reserve-commit-before-stdout':
        name = 'predecessor-reserve-result-%020d.json' % phase['generation']
    elif boundary == 'stdout-loss':
        name = 'predecessor-result-written-%020d.json' % phase['generation']
    else:
        raise JournalError('cutpoint receipt mapping is unavailable')
    directory = open_control_dir()
    try:
        preview_raw = read_private(directory, 'source-preview-' + source_key + '.json')
        preview = strict_json(preview_raw)
        request = preview.get('request') if type(preview) is dict else None
        origin = request.get('Origin') if type(request) is dict else None
        expected_source = {'DeploymentID': source['deployment_id'], 'ProjectID': source['project_id'],
                           'Kind': source['source_kind'], 'ID': source['source_id']}
        if (type(request) is not dict or type(origin) is not dict or preview.get('key') != source_key or
                preview.get('recovery_id') != source['recovery_id'] or request.get('OperationID') != source['operation_id'] or
                request.get('ActorUserID') != source['actor_id'] or origin.get('Application') != source['application'] or
                origin.get('Source') != expected_source):
            raise JournalError('cutpoint source request binding differs')
        if boundary == 'anchor-fsync':
            anchor_name, anchor_evidence = _anchor_receipt(directory, source_key, preview, preview_raw, digest, source)
            return anchor_name, None, anchor_evidence
        body = read_private(directory, name)
        if hashlib.sha256(body).hexdigest() != digest:
            raise JournalError('cutpoint expected receipt digest differs')
        item = strict_json(body)
        if type(item) is not dict:
            raise JournalError('cutpoint receipt is not an object')
        if boundary == 'c81-convergence-commit':
            if item != phase or phase['phase'] != 'migration_callback':
                raise JournalError('cutpoint migration phase differs')
        elif boundary in CALLBACK_CUTS:
            if item.get('phase_generation') != phase['generation'] or phase['phase'] != 'predecessor_callback':
                raise JournalError('cutpoint predecessor phase differs')
            generation = phase['generation']
            def read_effect(kind):
                return strict_json(read_private(directory, 'predecessor-%s-%020d.json' % (kind, generation)))
            ready = read_effect('ready')
            create = read_effect('create-intent')
            binding = {'version': 1, 'phase_generation': generation, 'recovery_id': source['recovery_id'],
                       'application': source['application'], 'realm_digest': source['realm_digest'], 'backend': event['backend']}
            if (type(ready) is not dict or set(ready) != set(binding) | {'child'} or
                    any(ready[key] != value for key, value in binding.items()) or ready['child'] != event['child'] or
                    type(create) is not dict or set(create) != set(binding) | {'command', 'command_digest'} or
                    any(create[key] != value for key, value in binding.items())):
                raise JournalError('cutpoint ready or create binding differs')
            command = create['command']
            if (not _command_shape(command, CREATE_KEYS, CREATE_DIGESTS, CREATE_NUMBERS) or
                    command['project_id'] != source['project_id'] or command['object_id'] != source['record_id'] or
                    command['object_kind'] != 'record' or type(command['current_revision_id']) is not str or
                    re.fullmatch(r'rrv_[a-z0-9]{1,64}', command['current_revision_id']) is None or
                    command['lock_version'] == 0 or command['authorization_epoch'] == 0 or command['witness_sequence'] == 0 or
                    command['ttl_microseconds'] != 600000000 or
                    create['command_digest'] != hashlib.sha256(canonical(command)).hexdigest()):
                raise JournalError('cutpoint original record command differs')
            if boundary == 'preview-ack-loss':
                if (set(item) != set(binding) | {'command_digest', 'reservation_id', 'expires_at'} or
                        any(item[key] != value for key, value in binding.items()) or
                        item['command_digest'] != create['command_digest'] or
                        type(item['reservation_id']) is not str or not item['reservation_id'].startswith('drs_') or
                        type(item['expires_at']) is not str):
                    raise JournalError('cutpoint preview result differs')
            if boundary in ('reserve-intent', 'reserve-commit-before-stdout', 'stdout-loss'):
                preview, reserve = read_effect('create-result'), read_effect('reserve-intent')
                if (type(preview) is not dict or set(preview) != set(binding) | {'command_digest', 'reservation_id', 'expires_at'} or
                        any(preview[key] != value for key, value in binding.items()) or
                        preview['command_digest'] != create['command_digest'] or type(reserve) is not dict or
                        set(reserve) != set(binding) | {'command', 'command_digest'} or
                        any(reserve[key] != value for key, value in binding.items()) or
                        not _command_shape(reserve['command'], RESERVE_KEYS, RESERVE_DIGESTS, RESERVE_NUMBERS) or
                        reserve['command_digest'] != hashlib.sha256(canonical(reserve['command'])).hexdigest() or
                        reserve['command']['create_command_digest'] != create['command_digest'] or
                        reserve['command']['reservation_id'] != preview['reservation_id'] or
                        reserve['command']['deployment_id'] != source['deployment_id'] or
                        reserve['command']['actor_id'] != source['actor_id'] or
                        reserve['command']['deletion_contract_version'] != 1 or
                        reserve['command']['owner_id'] != 'c80_predecessor' or
                        reserve['command']['owner_lease_microseconds'] != 30000000 or
                        reserve['command']['reason_code'] != 'user_confirmed' or
                        reserve['command']['request_fingerprint'] != command['request_fingerprint'] or
                        reserve['command']['expected_binding_digest'] != command['binding_digest'] or
                        reserve['command']['observed_witness_sequence'] == 0):
                    raise JournalError('cutpoint reserve command differs')
                if boundary != 'reserve-intent':
                    committed = read_effect('reserve-result')
                    if (type(committed) is not dict or set(committed) != set(binding) |
                            {'create_command_digest', 'command_digest', 'reservation_id', 'operation_id'} or
                            any(committed[key] != value for key, value in binding.items()) or
                            committed['create_command_digest'] != create['command_digest'] or
                            committed['command_digest'] != reserve['command_digest'] or
                            committed['reservation_id'] != preview['reservation_id'] or
                            committed['operation_id'] != event['operation_id'] or
                            type(committed['operation_id']) is not str or not committed['operation_id'].startswith('rpo_')):
                        raise JournalError('cutpoint committed operation differs')
                if boundary == 'stdout-loss':
                    written_binding = {key: value for key, value in binding.items() if key != 'backend'}
                    output = item.get('result')
                    if (set(item) != set(written_binding) | {'result_digest', 'result'} or
                            any(item[key] != value for key, value in written_binding.items()) or
                            type(item['result_digest']) is not str or HEX.fullmatch(item['result_digest']) is None or
                            output != {'version': 1, 'phase_generation': generation, 'status': 'committed',
                                       'operation_id': committed['operation_id'], 'reason': 'app_reserved'}):
                        raise JournalError('cutpoint written result differs')
                    carrier_raw, carrier, actual_output = read_stdout_carrier(directory, generation, source, item)
                    if actual_output != output or carrier['result_digest'] != item['result_digest']:
                        raise JournalError('cutpoint original stdout bytes differ')
                    carrier_digest = hashlib.sha256(carrier_raw).hexdigest()
        elif item.get('key') != source_key:
            raise JournalError('cutpoint source receipt key differs')
        return name, carrier_digest, None
    finally:
        os.close(directory)


class FaultBarrier:
    def __init__(self, scenario, nonce, journal, app, recovery_id, coordinator, pidfd, sock, source, callbacks, hold_backend=False):
        self.boundary, self.victim = scenario_spec(scenario)
        if type(nonce) is not str or HEX.fullmatch(nonce) is None:
            raise JournalError('fault nonce invalid')
        self.scenario, self.nonce, self.journal = scenario, nonce, journal
        self.app, self.recovery_id, self.coordinator, self.pidfd = app, recovery_id, coordinator, pidfd
        self.source = source
        self.callbacks = callbacks
        if type(hold_backend) is not bool or hold_backend and scenario != 'reserve-intent-coordinator':
            raise JournalError('backend hold is limited to the fixed negative scenario')
        self.hold_backend = hold_backend
        self.held_backend = None
        self.sock = sock
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.armed = False
        self.completed = False
        self.observation = None

    def _stop_original_backend(self, backend, deadline):
        life = self.callbacks.life
        container = life.postgres.containers['app']['id']
        candidates = []
        for name in os.listdir('/proc'):
            if not name.isdecimal():
                continue
            pid = int(name)
            try:
                status = read_kernel('/proc/%d/status' % pid, 65536)
                namespace = [line.split()[1:] for line in status.splitlines() if line.startswith('NSpid:')]
                cgroup = read_kernel('/proc/%d/cgroup' % pid, 65536)
                if (len(namespace) != 1 or not namespace[0] or any(not item.isdecimal() for item in namespace[0]) or
                        namespace[0][-1] != str(backend['pid']) or not _cgroup_has_container(cgroup, container)):
                    continue
                identity = _identity(pid)
                if identity is not None and identity['exe'] == '/usr/lib/postgresql/16/bin/postgres':
                    candidates.append((identity, [int(item) for item in namespace[0]]))
            except FileNotFoundError:
                continue
        if len(candidates) != 1:
            raise JournalError('original PostgreSQL backend host identity is ambiguous')
        identity, namespace_pids = candidates[0]
        pidfd = os.pidfd_open(identity['pid'], 0)
        try:
            if (select.select([pidfd], [], [], 0)[0] or process_start(identity['pid']) != identity['starttime'] or
                    identity['status'].get(b'Uid', '').split() != [str(life.postgres.uid)] * 4):
                raise JournalError('original PostgreSQL backend is not held')
            observed = {'pid': identity['pid'], 'starttime': identity['starttime'], 'namespace_pid': backend['pid'],
                        'namespace_pids': namespace_pids, 'stopped_state': 'T', 'pidfd_live': True,
                        'backend_started': backend['started'], 'backend_role': backend['role'], 'container_id': container,
                        'executable_sha256': identity['exe_sha256']}
            life._admit('negative-stop-original-backend', observed)
            signal.pidfd_send_signal(pidfd, signal.SIGSTOP)
            while time.monotonic() < deadline:
                if select.select([pidfd], [], [], 0)[0] or process_start(identity['pid']) != identity['starttime']:
                    raise JournalError('original backend exited while being held')
                status = read_kernel('/proc/%d/status' % identity['pid'], 65536)
                if any(line.startswith('State:') and line.split()[1] == 'T' for line in status.splitlines()):
                    break
                select.select([], [], [], min(0.02, max(0, deadline - time.monotonic())))
            else:
                raise JournalError('original backend did not enter the stopped state')
            life._register('negative-stop-original-backend', observed)
            self.held_backend = {'pidfd': pidfd, 'identity': observed}
            pidfd = None
        finally:
            if pidfd is not None:
                try:
                    signal.pidfd_send_signal(pidfd, signal.SIGCONT)
                except ProcessLookupError:
                    pass
                os.close(pidfd)

    def release_backend(self, deadline, require_exit):
        held = self.held_backend
        if held is None:
            raise JournalError('original backend hold is missing')
        try:
            signal.pidfd_send_signal(held['pidfd'], signal.SIGCONT)
            exited = bool(select.select([held['pidfd']], [], [], max(0, deadline - time.monotonic()))[0]) if require_exit else False
            if require_exit and not exited:
                raise JournalError('original backend did not settle after SIGCONT')
            return exited
        finally:
            os.close(held['pidfd'])
            self.held_backend = None

    def configure(self, deadline):
        _send(self.sock, {'version': 1, 'op': 'cutpoint_configure', 'scenario': self.scenario,
                          'boundary': self.boundary, 'victim': self.victim, 'nonce': self.nonce}, deadline)

    def serve_one(self, deadline):
        if self.completed or self.armed:
            raise JournalError('duplicate cutpoint event')
        event, credentials = _receive(self.sock, deadline)
        if credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('cutpoint peer credentials differ')
        if (type(event) is not dict or frozenset(event) != READY_KEYS or type(event['version']) is not int or
                event['version'] != 1 or event['op'] != 'cutpoint_ready' or event['scenario'] != self.scenario or
                event['boundary'] != self.boundary or event['nonce'] != self.nonce or
                event['coordinator'] != self.coordinator or type(event['phase_generation']) is not int or
                event['phase_generation'] <= 0 or type(event['operation_id']) is not str or
                len(event['operation_id']) > 128 or type(event['evidence_digest']) is not str or
                HEX.fullmatch(event['evidence_digest']) is None):
            raise JournalError('cutpoint ready frame differs')
        _coordinator_live(self.coordinator)
        if select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('cutpoint coordinator exited early')
        phase = read_phases(self.app, self.recovery_id)
        if phase['generation'] != event['phase_generation']:
            raise JournalError('cutpoint durable phase changed')
        if self.boundary not in CALLBACK_CUTS and event['operation_id'] != self.source['operation_id']:
            raise JournalError('cutpoint original operation differs')
        backend = event['backend']
        if backend is not None and (type(backend) is not dict or set(backend) != {'pid', 'started', 'role'} or
                type(backend['pid']) is not int or backend['pid'] <= 0 or type(backend['started']) is not str or
                not backend['started'] or type(backend['role']) is not str or
                re.fullmatch(r'[a-z_][a-z0-9_]{0,62}', backend['role']) is None):
            raise JournalError('cutpoint backend identity invalid')
        child = event['child']
        child_pidfd = None
        try:
            if child is not None:
                if (type(child) is not dict or set(child) != {'pid', 'starttime', 'executable_sha256'} or
                        type(child['pid']) is not int or child['pid'] <= 0 or
                        type(child['starttime']) is not int or child['starttime'] <= 0 or
                        type(child['executable_sha256']) is not str or HEX.fullmatch(child['executable_sha256']) is None):
                    raise JournalError('cutpoint child identity invalid')
                child_pidfd = os.pidfd_open(child['pid'], 0)
                actual = _identity(child['pid'])
                expected_exe = '/opt/g2/c81-migrator' if self.boundary == 'c81-convergence-commit' else '/opt/g2/c80-predecessor'
                if (select.select([child_pidfd], [], [], 0)[0] or actual is None or actual['parent'] != self.coordinator['pid'] or
                        actual['starttime'] != child['starttime'] or actual['exe_sha256'] != child['executable_sha256'] or
                        actual['exe'] != expected_exe or process_start(child['pid']) != child['starttime']):
                    raise JournalError('cutpoint child identity changed')
            if self.victim == 'child' and child_pidfd is None:
                raise JournalError('selected child is not held')
            receipt, carrier_digest, anchor_evidence = _receipt(self.boundary, phase, event['evidence_digest'], self.source, event)
            if not _guard_contended():
                raise JournalError('cutpoint guard is not held')
            intent = {'version': 1, 'run_token': RUN_TOKEN, 'scenario': self.scenario, 'boundary': self.boundary,
                      'victim': self.victim, 'phase_generation': event['phase_generation'],
                      'operation_id': event['operation_id'], 'coordinator': self.coordinator, 'child': child,
                      'backend': backend, 'receipt_name': receipt, 'evidence_digest': event['evidence_digest'],
                      'nonce_digest': hashlib.sha256(bytes.fromhex(self.nonce)).hexdigest()}
            if carrier_digest is not None:
                intent['stdout_carrier_name'] = 'predecessor-stdout-%020d.json' % event['phase_generation']
                intent['stdout_carrier_digest'] = carrier_digest
            if anchor_evidence is not None:
                intent['anchor_evidence'] = anchor_evidence
            name = 'fault-intent-' + self.scenario + '.json'
            _write_new(self.journal._fd, name, intent)
            self.journal.append('registration', {'name': 'fault-intent-' + self.scenario,
                                                  'identity_digest': hashlib.sha256(canonical(intent)).hexdigest()})
            _send(self.sock, {'version': 1, 'op': 'cutpoint_ack', 'nonce': self.nonce,
                              'evidence_digest': event['evidence_digest']}, deadline)
            armed, peer = _receive(self.sock, deadline)
            if (peer != credentials or type(armed) is not dict or frozenset(armed) != ARMED_KEYS or
                    armed != {'version': 1, 'op': 'cutpoint_armed', 'nonce': self.nonce,
                              'evidence_digest': event['evidence_digest']}):
                raise JournalError('cutpoint did not arm')
            if (read_phases(self.app, self.recovery_id) != phase or select.select([self.pidfd], [], [], 0)[0] or
                    child_pidfd is not None and select.select([child_pidfd], [], [], 0)[0]):
                raise JournalError('cutpoint identity changed before injection')
            observer = None
            if self.boundary in CALLBACK_CUTS:
                callback = self.callbacks.callbacks.get(phase['generation'])
                if (callback is None or callback['child'] != child or callback['intent']['coordinator'] != self.coordinator or
                        callback['observer'] is None or backend is None):
                    raise JournalError('cutpoint lacks the original held callback observer')
                observer = callback['observer']
                if self.victim == 'coordinator':
                    observer.arm(self.coordinator, callback['intent']['guard'], deadline)
            if self.hold_backend:
                self._stop_original_backend(backend, deadline)
            _coordinator_live(self.coordinator)
            if (read_phases(self.app, self.recovery_id) != phase or select.select([self.pidfd], [], [], 0)[0] or
                    child_pidfd is not None and select.select([child_pidfd], [], [], 0)[0] or not _guard_contended()):
                raise JournalError('cutpoint identity or guard changed at injection')
            self.armed = True
            victimfd = child_pidfd if self.victim == 'child' else self.pidfd
            signal.pidfd_send_signal(victimfd, signal.SIGKILL)
            remaining = max(0, min(15, deadline - time.monotonic()))
            if not select.select([victimfd], [], [], remaining)[0]:
                raise JournalError('cutpoint victim exit was not observed')
            exit_stop = None
            if observer is not None and self.victim == 'coordinator':
                exit_stop = observer.exit_stop(self.pidfd, callback['intent']['guard'], deadline)
                exit_stop.update(observer.resume_and_join(deadline))
            child_exited = child_pidfd is None or bool(select.select([child_pidfd], [], [], 0)[0])
            if self.victim == 'coordinator' and child_pidfd is not None and not child_exited:
                remaining = max(0, min(15, deadline - time.monotonic()))
                child_exited = bool(select.select([child_pidfd], [], [], remaining)[0])
                if not child_exited:
                    raise JournalError('original child exit was not observed')
            observation = {'version': 1, 'scenario': self.scenario, 'boundary': self.boundary, 'victim': self.victim,
                           'phase_generation': event['phase_generation'], 'operation_id': event['operation_id'],
                           'evidence_digest': event['evidence_digest'], 'receipt_name': receipt,
                           'ready_received': True, 'armed_received': True, 'signal': 'SIGKILL',
                           'victim_pidfd_exited': True, 'child_pidfd_exited': child_exited,
                           'guard_contended_before_signal': True, 'exit_stop': exit_stop,
                           'backend': backend}
            _write_new(self.journal._fd, 'fault-complete-' + self.scenario + '.json', observation)
            self.journal.append('registration', {'name': 'fault-complete-' + self.scenario,
                                                  'identity_digest': hashlib.sha256(canonical(observation)).hexdigest()})
            self.observation, self.completed = observation, True
        finally:
            if not self.completed and self.held_backend is not None:
                self.release_backend(time.monotonic(), False)
            if child_pidfd is not None:
                os.close(child_pidfd)
