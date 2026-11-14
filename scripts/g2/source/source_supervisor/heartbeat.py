"""One-use root heartbeat admission from the coordinator's durable intent."""
import fcntl
import hashlib
import os
import re
import select
import stat
import time
import urllib.parse

from contract import MAX_FRAME, RUN_TOKEN
from journal import JournalError, _read, _write_new, canonical, strict_json
from permit import _coordinator_live
from phase import open_control_dir, read_phases, read_private

INTENT_KEYS = frozenset(('version', 'phase_generation', 'callback_generation', 'recovery_id', 'application',
                         'coordinator', 'config_fd', 'config_digest', 'initializer_sha256'))
CONFIG_KEYS = frozenset(('version', 'case', 'authority_url'))
HEX = re.compile(r'^[0-9a-f]{64}$')
F_GET_SEALS = 1034
ALL_SEALS = 0x0f


class HeartbeatGate:
    def __init__(self, journal, invoker, app_facts, recovery_id, coordinator, pidfd):
        self.journal = journal
        self.invoker = invoker
        self.app_facts = app_facts
        self.recovery_id = recovery_id
        self.coordinator = coordinator
        self.pidfd = pidfd
        self.completed = {}

    def _verified_completion(self, generation, digest):
        filename = 'heartbeat-complete-%020d.json' % generation
        body = _read(self.journal._fd, filename)
        item = strict_json(body)
        if (type(item) is not dict or set(item) != {'version', 'run_token', 'callback_generation', 'intent_sha256',
                                                     'coordinator', 'child_pid', 'child_starttime', 'membership_expires_at'} or
                item['version'] != 1 or item['run_token'] != RUN_TOKEN or item['callback_generation'] != generation or
                item['intent_sha256'] != digest or item['coordinator'] != self.coordinator or
                type(item['child_pid']) is not int or item['child_pid'] <= 0 or
                type(item['child_starttime']) is not int or item['child_starttime'] <= 0 or
                type(item['membership_expires_at']) is not str or canonical(item) != body):
            raise JournalError('heartbeat completion differs')
        return item

    def verify(self, phase):
        if phase['phase'] not in ('quiescent', 'predecessor_callback'):
            raise JournalError('heartbeat verification phase invalid')
        generation = phase['generation'] + 1 if phase['phase'] == 'quiescent' else phase['generation']
        directory = open_control_dir()
        try:
            try:
                raw = read_private(directory, 'predecessor-heartbeat-intent-%020d.json' % generation)
            except FileNotFoundError:
                if phase['phase'] == 'quiescent':
                    return
                raise JournalError('predecessor heartbeat intent missing')
        finally:
            os.close(directory)
        digest = hashlib.sha256(raw).hexdigest()
        if self.completed.get(generation) != digest:
            raise JournalError('predecessor heartbeat has not completed')
        self._verified_completion(generation, digest)

    def admit_pending(self, phase, endpoint, deadline):
        if phase['phase'] != 'quiescent':
            return
        generation = phase['generation'] + 1
        filename = 'predecessor-heartbeat-intent-%020d.json' % generation
        directory = open_control_dir()
        try:
            try:
                raw = read_private(directory, filename)
            except FileNotFoundError:
                return
        finally:
            os.close(directory)
        digest = hashlib.sha256(raw).hexdigest()
        intent = strict_json(raw)
        if (type(intent) is not dict or frozenset(intent) != INTENT_KEYS or
                type(intent['version']) is not int or intent['version'] != 1 or
                type(intent['phase_generation']) is not int or intent['phase_generation'] != phase['generation'] or
                type(intent['callback_generation']) is not int or intent['callback_generation'] != generation or
                intent['recovery_id'] != self.recovery_id or intent['application'] != self.app_facts['application'] or
                intent['coordinator'] != self.coordinator or type(intent['config_fd']) is not int or
                not 0 <= intent['config_fd'] < (1 << 31) or type(intent['config_digest']) is not str or
                HEX.fullmatch(intent['config_digest']) is None or
                intent['initializer_sha256'] != self.invoker.hashes['c80-initializer']):
            raise JournalError('heartbeat intent binding differs')
        _coordinator_live(self.coordinator)
        if select.select([self.pidfd], [], [], 0)[0] or read_phases(self.app_facts['application'], self.recovery_id) != phase:
            raise JournalError('heartbeat phase or coordinator changed')
        started = 'heartbeat-started-%020d.json' % generation
        complete = 'heartbeat-complete-%020d.json' % generation
        if generation in self.completed:
            if self.completed[generation] != digest:
                raise JournalError('heartbeat intent changed')
            self._verified_completion(generation, digest)
            return
        try:
            _read(self.journal._fd, started)
        except FileNotFoundError:
            pass
        else:
            # A start without the live owner's recorded successful join is an
            # unknown result; never rerun it in the same or another phase.
            raise JournalError('heartbeat start has unknown result')
        try:
            _read(self.journal._fd, complete)
        except FileNotFoundError:
            pass
        else:
            raise JournalError('unowned heartbeat completion exists')
        source = '/proc/%d/fd/%d' % (self.coordinator['pid'], intent['config_fd'])
        link = os.readlink(source)
        if not link.startswith('/memfd:') or not link.endswith(' (deleted)'):
            raise JournalError('heartbeat configuration is not a memfd')
        fd = os.open(source, os.O_RDONLY | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= MAX_FRAME or
                    fcntl.fcntl(fd, F_GET_SEALS) & ALL_SEALS != ALL_SEALS):
                raise JournalError('heartbeat configuration is not sealed')
            body = os.pread(fd, info.st_size + 1, 0)
            if len(body) != info.st_size or hashlib.sha256(body).hexdigest() != intent['config_digest']:
                raise JournalError('heartbeat configuration digest differs')
            config = strict_json(body)
            if (type(config) is not dict or frozenset(config) != CONFIG_KEYS or type(config['version']) is not int or
                    config['version'] != 1 or config['case'] != self.invoker.case or type(config['authority_url']) is not str):
                raise JournalError('heartbeat configuration fields differ')
            parsed = urllib.parse.urlsplit(config['authority_url'])
            if (parsed.scheme not in ('postgres', 'postgresql') or parsed.hostname != endpoint['app_host'] or
                    parsed.port != endpoint['app_port'] or
                    parsed.netloc.rsplit('@', 1)[-1] != '%s:%s' % (endpoint['app_host'], endpoint['app_port']) or
                    urllib.parse.unquote(parsed.username or '') != 'houfeng_records_authority' or not parsed.password or
                    parsed.path != '/' + self.app_facts['application']['DatabaseName'] or parsed.fragment or
                    urllib.parse.parse_qsl(parsed.query, keep_blank_values=True, strict_parsing=True) != [('sslmode', 'disable')]):
                raise JournalError('heartbeat authority endpoint differs')
            marker = {'version': 1, 'run_token': RUN_TOKEN, 'callback_generation': generation,
                      'intent_sha256': digest, 'coordinator': self.coordinator}
            _write_new(self.journal._fd, started, marker)
            self.journal.append('registration', {'name': 'heartbeat-start-' + str(generation),
                                                  'identity_digest': hashlib.sha256(canonical(marker)).hexdigest()})
            result = self.invoker.app_initializer('heartbeat', fd, intent['config_digest'], deadline)
            if result['status'] != 0 or result['stderr']:
                raise JournalError('heartbeat helper did not complete')
            output = strict_json(result['stdout'])
            if (type(output) is not dict or set(output) != {'version', 'case', 'deployment_id', 'membership_expires_at'} or
                    type(output['version']) is not int or output['version'] != 1 or output['case'] != self.invoker.case or
                    output['deployment_id'] != self.app_facts['deployment_id'] or
                    type(output['membership_expires_at']) is not str or
                    re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z',
                                 output['membership_expires_at']) is None):
                raise JournalError('heartbeat output differs')
            if (select.select([self.pidfd], [], [], 0)[0] or
                    read_phases(self.app_facts['application'], self.recovery_id) != phase or time.monotonic() >= deadline):
                raise JournalError('heartbeat phase changed before completion')
            completion = dict(marker, child_pid=result['pid'], child_starttime=result['starttime'],
                              membership_expires_at=output['membership_expires_at'])
            _write_new(self.journal._fd, complete, completion)
            self.journal.append('registration', {'name': 'heartbeat-complete-' + str(generation),
                                                  'identity_digest': hashlib.sha256(canonical(completion)).hexdigest()})
            self.completed[generation] = digest
        finally:
            os.close(fd)
