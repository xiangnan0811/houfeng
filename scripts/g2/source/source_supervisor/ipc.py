"""Credentialled, bounded realm observation channel for one coordinator."""
import array
import hashlib
import os
import re
import select
import socket
import struct
import time

from census import CensusError
from contract import MAX_FRAME, OBSERVE_KEYS
from journal import JournalError, canonical, strict_json
from permit import _coordinator_live, publish_permit
from phase import read_phases

NONCE = re.compile(r'^[0-9a-f]{64}$')


def _receive(sock, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise JournalError('realm request deadline exceeded')
    sock.settimeout(remaining)
    rights = []
    try:
        body, ancillary, flags, _ = sock.recvmsg(MAX_FRAME + 1, socket.CMSG_SPACE(struct.calcsize('3i')) + socket.CMSG_SPACE(1024),
                                                  getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
        credentials = []
        other = False
        for level, kind, data in ancillary:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                values = array.array('i')
                values.frombytes(data[:len(data) - len(data) % values.itemsize])
                rights.extend(values)
                other = True
            elif level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS and len(data) == struct.calcsize('3i'):
                credentials.append(struct.unpack('3i', data))
            else:
                other = True
        if (flags & (socket.MSG_CTRUNC | socket.MSG_TRUNC) or not 0 < len(body) <= MAX_FRAME or
                other or len(credentials) != 1):
            raise JournalError('realm request framing invalid')
        return strict_json(body), credentials[0]
    finally:
        for fd in rights:
            os.close(fd)


def _send(sock, value, deadline):
    body = canonical(value)
    if not 0 < len(body) <= MAX_FRAME:
        raise JournalError('realm response exceeds limit')
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise JournalError('realm response deadline exceeded')
    sock.settimeout(remaining)
    if sock.send(body) != len(body):
        raise JournalError('realm response was not sent atomically')


def _request_identity(request, seen):
    if (type(request) is not dict or frozenset(request) != OBSERVE_KEYS or type(request['version']) is not int or
            request['version'] != 1 or request['op'] != 'realm_observe' or type(request['phase_generation']) is not int or
            not 0 < request['phase_generation'] < (1 << 63) or type(request['nonce']) is not str or
            not NONCE.fullmatch(request['nonce']) or request['nonce'] in seen or len(seen) >= 4096):
        raise JournalError('realm request identity invalid')
    seen.add(request['nonce'])


def _phase_matches(request, phase):
    if phase['generation'] != request['phase_generation'] or phase['phase'] not in ('quiescent', 'predecessor_callback', 'refencing'):
        raise JournalError('maintenance phase changed')


class RealmChannel:
    """Root-created socket endpoint; dependencies must be root-owned live observations."""
    def __init__(self, sock, coordinator, pidfd, journal, roots, lifecycle, app_facts, recovery_id, program_sha256,
                 heartbeat, stale_reply=False):
        if sock.family != socket.AF_UNIX or sock.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) != socket.SOCK_SEQPACKET:
            raise JournalError('realm channel is not seqpacket')
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.sock = sock
        self.coordinator = coordinator
        self.pidfd = pidfd
        self.journal = journal
        self.roots = roots
        self.lifecycle = lifecycle
        self.app_facts = app_facts
        self.recovery_id = recovery_id
        self.program_sha256 = program_sha256
        self.heartbeat = heartbeat
        self.lifecycle.callbacks.pin_coordinator(coordinator, pidfd)
        self.seen = set()
        self.sequence = 0
        if type(stale_reply) is not bool:
            raise JournalError('negative reply selector invalid')
        self.stale_reply = stale_reply
        self.injected = None

    def admit_pending(self, deadline):
        phase = read_phases(self.app_facts['application'], self.recovery_id)
        if phase['phase'] == 'quiescent':
            realm_digest = self.journal.realm_digest()
            candidate = self.lifecycle.callbacks.classify(phase, self.coordinator, self.app_facts,
                                                          self.recovery_id, realm_digest)
            if candidate is None or not candidate['recovery']:
                endpoint = self.lifecycle.check_closed_realm(deadline)
                self.heartbeat.admit_pending(phase, endpoint, deadline)

    def serve_one(self, deadline):
        request, credentials = _receive(self.sock, deadline)
        if credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('realm peer credentials differ')
        _request_identity(request, self.seen)
        self.sequence += 1
        valid = False
        realm_digest = roots_digest = None
        try:
            if select.select([self.pidfd], [], [], 0)[0]:
                raise JournalError('coordinator exited')
            _coordinator_live(self.coordinator)
            phase = read_phases(self.app_facts['application'], self.recovery_id)
            _phase_matches(request, phase)
            realm_digest = self.journal.realm_digest()
            candidate = self.lifecycle.callbacks.classify(phase, self.coordinator, self.app_facts,
                                                          self.recovery_id, realm_digest)
            recovering = candidate is not None and candidate['recovery']
            if recovering or phase['phase'] == 'refencing':
                if candidate is None:
                    raise JournalError('recovery callback identity unavailable')
                endpoint = self.lifecycle.postgres.observe(self.lifecycle.recoveries)
                self.lifecycle.callbacks.settle(candidate, endpoint, deadline, self.coordinator)
                if self.lifecycle.check_closed_realm(deadline) != endpoint:
                    raise JournalError('recovery endpoint changed')
            else:
                endpoint = self.lifecycle.check_closed_realm(deadline)
                self.heartbeat.verify(phase)
            self.app_facts['runtime_host'] = endpoint['app_host']
            self.app_facts['runtime_port'] = endpoint['app_port']
            roots_digest = self.roots.scan()
            if phase['phase'] == 'predecessor_callback' and not recovering:
                if candidate is None:
                    raise JournalError('callback intent is missing')
                permit = publish_permit(self.journal, self.app_facts['application'], self.recovery_id, realm_digest,
                                        self.coordinator, self.pidfd, self.program_sha256, self.app_facts)
                self.lifecycle.callbacks.permit(candidate, permit)
                self.lifecycle.origins.allowed_predecessor = True
                self.lifecycle.callbacks.capture_ready(candidate, endpoint, deadline)
            endpoint_after = self.lifecycle.check_closed_realm(deadline)
            if (endpoint_after != endpoint or read_phases(self.app_facts['application'], self.recovery_id) != phase or
                    select.select([self.pidfd], [], [], 0)[0]):
                raise JournalError('phase or coordinator changed during observation')
            _coordinator_live(self.coordinator)
            if self.roots.scan() != roots_digest or self.journal.realm_digest() != realm_digest:
                raise JournalError('realm identity changed during observation')
            valid = True
        except Exception:
            self.roots.invalidate()
            self.lifecycle.invalidate('realm_observation')
            realm_digest = roots_digest = None
        self.journal.append('observation', {'phase_generation': request['phase_generation'],
                                            'observation_sequence': self.sequence,
                                            'roots_digest': roots_digest, 'valid': valid})
        response = {'version': 1, 'op': 'realm_observe', 'phase_generation': request['phase_generation'],
                    'nonce': request['nonce'], 'observation_sequence': self.sequence,
                    'realm_digest': realm_digest, 'roots_digest': roots_digest, 'valid': valid}
        if self.stale_reply:
            if not valid or phase['phase'] != 'quiescent' or self.injected is not None:
                raise JournalError('stale reply requires the first live quiescent observation')
            wrong = '0' * 64 if request['nonce'] != '0' * 64 else '1' * 64
            response['nonce'] = wrong
            self.injected = {'coordinator_pid': credentials[0], 'phase_generation': request['phase_generation'],
                             'request_nonce': request['nonce'], 'response_nonce': wrong,
                             'observation_sequence': self.sequence}
            self.stale_reply = False
        _send(self.sock, response, deadline)
        return valid

    def coordinator_waited(self, status):
        self.lifecycle.callbacks.coordinator_waited(self.coordinator, status)


class ClosedRealmChannel:
    """One negative-only channel that can never issue a no-copy capability."""
    def __init__(self, sock, coordinator, pidfd, lifecycle, application, recovery_id):
        if (not lifecycle.invalid or not lifecycle.closed_maintenance or
                sock.family != socket.AF_UNIX or sock.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) != socket.SOCK_SEQPACKET):
            raise JournalError('closed realm channel is unavailable')
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.sock, self.coordinator, self.pidfd = sock, coordinator, pidfd
        self.lifecycle, self.application, self.recovery_id = lifecycle, application, recovery_id
        self.seen, self.sequence = set(), 0
        self.observations = []

    def admit_pending(self, deadline):
        if (not self.lifecycle.invalid or not self.lifecycle.closed_maintenance or
                time.monotonic() >= deadline or select.select([self.pidfd], [], [], 0)[0]):
            raise JournalError('closed realm coordinator is unavailable')

    def serve_one(self, deadline):
        request, credentials = _receive(self.sock, deadline)
        if credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('closed realm peer credentials differ')
        _request_identity(request, self.seen)
        _coordinator_live(self.coordinator)
        phase = read_phases(self.application, self.recovery_id)
        _phase_matches(request, phase)
        if (not self.lifecycle.invalid or not self.lifecycle.closed_maintenance or
                select.select([self.pidfd], [], [], 0)[0]):
            raise JournalError('closed realm continuity changed')
        self.sequence += 1
        self.lifecycle.journal.append('observation', {'phase_generation': request['phase_generation'],
                                                       'observation_sequence': self.sequence,
                                                       'roots_digest': None, 'valid': False})
        response = {'version': 1, 'op': 'realm_observe', 'phase_generation': request['phase_generation'],
                    'nonce': request['nonce'], 'observation_sequence': self.sequence,
                    'realm_digest': None, 'roots_digest': None, 'valid': False}
        _send(self.sock, response, deadline)
        self.observations.append({'phase_generation': request['phase_generation'],
                                  'observation_sequence': self.sequence,
                                  'nonce_sha256': hashlib.sha256(bytes.fromhex(request['nonce'])).hexdigest(),
                                  'valid': False})
        return False

    def coordinator_waited(self, status):
        if type(status) is not int or not select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('closed realm coordinator was not directly waited')


class MaintenanceChannel:
    """Fixed migration endpoint that cannot issue a realm capability."""
    def __init__(self, sock, coordinator, pidfd, maintenance, terminal_only=False):
        if type(terminal_only) is not bool:
            raise JournalError('maintenance channel selector invalid')
        if sock.family != socket.AF_UNIX or sock.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) != socket.SOCK_SEQPACKET:
            raise JournalError('maintenance channel is not seqpacket')
        peer = struct.unpack('3i', sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('3i')))
        if peer != (os.getpid(), 0, 0):
            raise JournalError('maintenance channel creator is not the root supervisor')
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.sock, self.coordinator, self.pidfd = sock, coordinator, pidfd
        self.terminal_only = terminal_only
        self.terminal_acks = []
        self.maintenance = maintenance
        maintenance.bind_coordinator(coordinator, pidfd)

    def admit_pending(self, deadline):
        if time.monotonic() >= deadline:
            raise JournalError('maintenance request deadline exceeded')
        self.maintenance._live(require_guard=False)
        if self.terminal_only:
            import network_verify
            if network_verify.migration_scope(self.maintenance.cgroup) != {
                    'members': [self.coordinator['pid']], 'populated': 1}:
                raise JournalError('terminal admission scope contains an unexpected process')

    def serve_one(self, deadline):
        request, credentials = _receive(self.sock, deadline)
        if credentials != (self.coordinator['pid'], 10002, 10002):
            raise JournalError('maintenance peer credentials differ')
        if self.terminal_only and (type(request) is not dict or request.get('op') != 'maintenance_checkpoint' or
                                   request.get('family') != 'migration' or request.get('kind') != 'terminal' or
                                   type(request.get('attempt')) is not int or request['attempt'] != 0):
            raise JournalError('post-migration channel only permits exact terminal retries')
        if type(request) is dict and request.get('op') == 'maintenance_ready_disposition':
            response = self.maintenance.ready_disposition(request, credentials, deadline)
        else:
            response = self.maintenance.checkpoint(request, credentials, deadline)
        if self.terminal_only:
            self.admit_pending(deadline)
            self.maintenance._live()
        _send(self.sock, response, deadline)
        if request.get('kind') == 'terminal':
            self.terminal_acks.append({key: response[key] for key in
                ('family', 'recovery_id', 'application_digest', 'phase_generation', 'attempt', 'kind',
                 'record_sha256', 'witness_sequence', 'witness_digest')})
        return True

    def coordinator_waited(self, status, retirement):
        if type(status) is not int or not select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('maintenance coordinator was not directly waited')
        if any(not select.select([fd], [], [], 0)[0] for fd in self.maintenance.children):
            raise JournalError('maintenance child scope has not retired')
        self.maintenance.coordinator_waited(self.coordinator, status, retirement)
