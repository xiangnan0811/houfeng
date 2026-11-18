"""Release-only FD7 barrier for one real unexpected APP backend."""
import hashlib
import os
import re
import select
import socket
import time

from contract import RUN_TOKEN
from cutpoints import ARMED_KEYS, READY_KEYS, _guard_contended
from ipc import _receive, _send
from journal import JournalError, _write_new, canonical
from permit import _coordinator_live
from phase import open_control_dir, read_phases, read_private
from pg_session import ExtraSession

HEX = re.compile(r'^[0-9a-f]{64}$')


class ExtraSessionBarrier:
    def __init__(self, nonce, life, postgres, app, recovery_id, source_operation_id, coordinator, pidfd, sock, app_facts):
        if type(nonce) is not str or HEX.fullmatch(nonce) is None:
            raise JournalError('extra-session cutpoint nonce invalid')
        self.nonce, self.life, self.postgres, self.app, self.recovery_id = nonce, life, postgres, app, recovery_id
        self.source_operation_id, self.coordinator, self.pidfd, self.sock = source_operation_id, coordinator, pidfd, sock
        self.app_facts = app_facts
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.session, self.observation = None, None
        self.completed = False

    def configure(self, deadline):
        _send(self.sock, {'version': 1, 'op': 'cutpoint_configure', 'scenario': 'extra-session',
                          'boundary': 'before-predecessor-admission', 'victim': 'coordinator', 'nonce': self.nonce}, deadline)

    def serve_one(self, deadline):
        if self.completed:
            raise JournalError('duplicate extra-session cutpoint')
        event, peer = _receive(self.sock, deadline)
        if (peer != (self.coordinator['pid'], 10002, 10002) or type(event) is not dict or
                frozenset(event) != READY_KEYS or type(event['version']) is not int or event['version'] != 1 or
                event['op'] != 'cutpoint_ready' or event['scenario'] != 'extra-session' or
                event['boundary'] != 'before-predecessor-admission' or event['nonce'] != self.nonce or
                event['coordinator'] != self.coordinator or event['operation_id'] != self.source_operation_id or
                event['child'] is not None or event['backend'] is not None or
                type(event['phase_generation']) is not int or event['phase_generation'] <= 0 or
                type(event['evidence_digest']) is not str or HEX.fullmatch(event['evidence_digest']) is None):
            raise JournalError('extra-session ready frame invalid')
        _coordinator_live(self.coordinator)
        if select.select([self.pidfd], [], [], 0)[0] or not _guard_contended():
            raise JournalError('extra-session coordinator or guard is unavailable')
        phase = read_phases(self.app, self.recovery_id)
        if phase['phase'] != 'quiescent' or phase['generation'] != event['phase_generation']:
            raise JournalError('extra-session phase is not quiescent')
        name = 'phase-%020d.json' % phase['generation']
        directory = open_control_dir()
        try:
            raw = read_private(directory, name)
        finally:
            os.close(directory)
        if hashlib.sha256(raw).hexdigest() != event['evidence_digest']:
            raise JournalError('extra-session phase receipt differs')
        intent = {'version': 1, 'run_token': RUN_TOKEN, 'scenario': 'extra-session',
                  'boundary': 'before-predecessor-admission', 'phase_generation': phase['generation'],
                  'source_operation_id': self.source_operation_id, 'coordinator': self.coordinator,
                  'receipt_name': name, 'evidence_digest': event['evidence_digest'],
                  'nonce_digest': hashlib.sha256(bytes.fromhex(self.nonce)).hexdigest()}
        _write_new(self.life.journal._fd, 'fault-intent-extra-session.json', intent)
        self.life.journal.append('registration', {'name': 'fault-intent-extra-session',
                                                   'identity_digest': hashlib.sha256(canonical(intent)).hexdigest()})
        _send(self.sock, {'version': 1, 'op': 'cutpoint_ack', 'nonce': self.nonce,
                          'evidence_digest': event['evidence_digest']}, deadline)
        armed, credentials = _receive(self.sock, deadline)
        if (credentials != peer or type(armed) is not dict or frozenset(armed) != ARMED_KEYS or
                type(armed['version']) is not int or armed != {'version': 1, 'op': 'cutpoint_armed',
                                                               'nonce': self.nonce, 'evidence_digest': event['evidence_digest']} or
                read_phases(self.app, self.recovery_id) != phase or select.select([self.pidfd], [], [], 0)[0] or
                not _guard_contended()):
            raise JournalError('extra-session cutpoint did not arm')
        self.session = ExtraSession(self.life, self.postgres.passwords['app'], self.app_facts['runtime_host'])
        observed = self.session.open()
        if read_phases(self.app, self.recovery_id) != phase or select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('extra-session coordinator or phase changed during injection')
        self.life.invalidate('negative_extra_session')
        _send(self.sock, {'version': 1, 'op': 'cutpoint_release', 'nonce': self.nonce,
                          'evidence_digest': event['evidence_digest']}, deadline)
        self.observation = {'version': 1, 'scenario': 'extra-session', 'boundary': 'before-predecessor-admission',
                            'phase_generation': phase['generation'], 'source_operation_id': self.source_operation_id,
                            'coordinator': self.coordinator, 'receipt_name': name, 'evidence_digest': event['evidence_digest'],
                            'phase_raw': raw.decode('utf-8', 'strict'), 'recovery_id': self.recovery_id,
                            'ready_received': True, 'armed_received': True, 'release_sent': True,
                            'guard_contended_before_injection': True, 'extra_backend': observed}
        self.completed = True

    def finish(self):
        if not self.completed or self.session is None or self.observation is None:
            raise JournalError('extra-session injection did not complete')
        self.session.close()
        result = dict(self.observation, extra_backend_joined=True)
        _write_new(self.life.journal._fd, 'fault-complete-extra-session.json', result)
        self.life.journal.append('registration', {'name': 'fault-complete-extra-session',
                                                   'identity_digest': hashlib.sha256(canonical(result)).hexdigest()})
        return result
