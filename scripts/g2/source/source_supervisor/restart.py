"""Exercise loss of the one supervisor against its original, live APP."""
import hashlib
import os
import select
import signal
import struct
import time

import audit
import bpf_authority
import ceremony
import genesis
import guest_postgres
import journal
import lifecycle
import origins
from invoke import PR_SET_PDEATHSIG, _prctl
from journal import JournalError, canonical, process_start, strict_json
from phase import open_control_dir, read_private

_MAX = 1024 * 1024


def _send(fd, value):
    body = canonical(value)
    if len(body) > _MAX:
        raise JournalError('restart evidence exceeds limit')
    data = struct.pack('!I', len(body)) + body
    while data:
        written = os.write(fd, data)
        if written <= 0:
            raise JournalError('restart evidence write failed')
        data = data[written:]


def _receive(fd, pidfd, deadline):
    data, length = bytearray(), None
    while length is None or len(data) < length + 4:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise JournalError('restart supervisor deadline exceeded')
        readable, _, _ = select.select([fd, pidfd], [], [], min(remaining, 1))
        if fd in readable:
            chunk = os.read(fd, 65536)
            if not chunk:
                raise JournalError('restart supervisor returned no complete evidence')
            data.extend(chunk)
            if length is None and len(data) >= 4:
                length = struct.unpack('!I', data[:4])[0]
                if length == 0 or length > _MAX:
                    raise JournalError('restart evidence length invalid')
        elif pidfd in readable:
            raise JournalError('restart supervisor exited before evidence')
    if len(data) != length + 4:
        raise JournalError('restart evidence contains trailing data')
    value = strict_json(bytes(data[4:]))
    if type(value) is not dict or canonical(value) != bytes(data[4:]):
        raise JournalError('restart evidence is not canonical')
    return value


def _old_supervisor(case, expected, seed, volumes, roots, install, daemon_info, systemd_state, seed_realms, export,
                    kernel_config, write_fd, authority, deadline):
    policy = origins.ProcessOrigins(authority, case)
    audit_owner = audit.AuditStream(policy.audit_event, kernel_config, authority, deadline)
    owner = journal.RealmJournal.create()
    facts = genesis.publish(owner, case, seed, volumes, roots, policy, install, daemon_info, systemd_state, authority)
    life = lifecycle.RealmLifecycle(owner, roots, volumes, daemon_info['postgres_uid'], daemon_info['postgres_gid'],
                                    systemd_state, policy, install, authority)
    life.bind_audit(audit_owner)
    with authority.observation(deadline):
        audit_owner.activate()
    postgres = guest_postgres.PostgresRealm(daemon_info, volumes, daemon_info['postgres_uid'],
                                             daemon_info['postgres_gid'], life, authority)
    life.bind_postgres(postgres)
    postgres.create_networks()
    processes = postgres.initialize()
    initial_resolvers = dict(life.resolvers)
    runner, domains, credentials, app, app_secrets, binding, realm_digest = seed_realms(
        life, roots, owner, postgres, expected, 'source-recovery')
    flow = ceremony.SourceCeremony(life, runner, owner, roots, postgres, app, domains, credentials, app_secrets, expected)
    source = flow.retire_source()
    evidence = {'genesis': facts, 'app_binding': binding, 'realm_digest': realm_digest,
                'source': source, 'postgres_processes': processes, 'resolvers': initial_resolvers,
                'final_resolvers': dict(life.resolvers), 'resolver_transition': life.resolver_transition,
                'root_journal': export(owner, authority, deadline)}
    evidence['bpf_authority'] = authority.export_evidence(facts['bpf_authority'], deadline)
    _send(write_fd, evidence)
    os.close(write_fd)
    while True:
        signal.pause()


def _replacement(deadline):
    read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
    parent = os.getpid()
    try:
        pid = os.fork()
    except BaseException:
        os.close(read_fd)
        os.close(write_fd)
        raise
    if pid == 0:
        os.close(read_fd)
        try:
            _prctl(PR_SET_PDEATHSIG, signal.SIGKILL)
            if os.getppid() != parent:
                os._exit(1)
            try:
                candidate = journal.RealmJournal.create()
            except FileExistsError:
                _send(write_fd, {'reissued': False, 'pid': os.getpid(), 'starttime': process_start(os.getpid())})
                os._exit(0)
            candidate.close()
            _send(write_fd, {'reissued': True, 'pid': os.getpid(), 'starttime': process_start(os.getpid())})
        except BaseException:
            pass
        os._exit(1)
    os.close(write_fd)
    try:
        pidfd = os.pidfd_open(pid, 0)
    except BaseException:
        os.close(read_fd)
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
        raise
    reaped = False
    try:
        until = min(deadline, time.monotonic() + 30)
        result = _receive(read_fd, pidfd, until)
        remaining = until - time.monotonic()
        if remaining <= 0 or not select.select([pidfd], [], [], remaining)[0]:
            raise JournalError('replacement supervisor exit not observed')
        waited, status = os.waitpid(pid, 0)
        reaped = True
        if waited != pid or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0 or result.get('reissued') is not False:
            raise JournalError('replacement supervisor did not reject old realm')
        return result
    finally:
        if not reaped:
            try:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                os.waitpid(pid, 0)
            except (OSError, ChildProcessError):
                pass
        os.close(read_fd)
        os.close(pidfd)


def exercise(case, expected, seed, volumes, roots, install, daemon_info, systemd_state, seed_realms, export,
             kernel_config, authority, deadline):
    read_fd, write_fd = os.pipe2(os.O_CLOEXEC)
    handoff_read, handoff_write = os.pipe2(os.O_CLOEXEC)
    parent = os.getpid()
    parent_start = process_start(parent)
    with open('/proc/self/exe', 'rb') as executable:
        parent_hash = hashlib.file_digest(executable, 'sha256').hexdigest()
    epoch = authority.epoch
    try:
        pid = os.fork()
    except BaseException:
        for fd in (read_fd, write_fd, handoff_read, handoff_write):
            os.close(fd)
        raise
    if pid == 0:
        os.close(read_fd)
        os.close(handoff_write)
        authority.close()
        try:
            _prctl(PR_SET_PDEATHSIG, signal.SIGKILL)
            if os.getppid() != parent:
                raise JournalError('restart parent died before handoff')
            child_authority = bpf_authority.BPFAuthority.from_realm_handoff(
                fd=handoff_read, parent_pid=parent, parent_starttime=parent_start,
                parent_executable_sha256=parent_hash, broker_sha256=expected['g2-bpf-authority'],
                run_token=authority.run_token, epoch=epoch, deadline=deadline)
            _old_supervisor(case, expected, seed, volumes, roots, install, daemon_info, systemd_state,
                            seed_realms, export, kernel_config, write_fd, child_authority, deadline)
        except BaseException as exc:
            try:
                _send(write_fd, {'failure': type(exc).__name__})
            except BaseException:
                pass
        os._exit(1)
    os.close(write_fd)
    os.close(handoff_read)
    try:
        pidfd = os.pidfd_open(pid, 0)
    except BaseException:
        os.close(read_fd)
        os.close(handoff_write)
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
        raise
    reaped = False
    try:
        start = process_start(pid)
        transferred, handoff_write = handoff_write, None
        authority.register_realm(pid, start, case['scenario'], pidfd, handoff_fd=transferred)
        evidence = _receive(read_fd, pidfd, min(deadline, time.monotonic() + 1800))
        if (set(evidence) != {'genesis', 'app_binding', 'realm_digest', 'source', 'postgres_processes', 'resolvers',
                              'final_resolvers', 'resolver_transition', 'root_journal', 'bpf_authority'} or
                evidence['genesis'].get('supervisor_pid') != pid or evidence['genesis'].get('supervisor_starttime') != start or
                evidence['source'].get('retirement', {}).get('state') != 'witnessed_handoff_pending'):
            raise JournalError('original supervisor evidence incomplete')
        realm = os.open('/var/lib/g2/realm-control', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            before = (journal._read_genesis(realm), journal._read(realm, 'realm-app.json'))
            if before != (canonical(evidence['genesis']), canonical(evidence['app_binding'])):
                raise JournalError('original realm binding differs from persisted files')
            source = evidence['source']['retirement']
            key = hashlib.sha256((source['source']['deployment_id'] + '\0' + source['source']['project_id'] + '\0' +
                                  source['operation_id']).encode('utf-8')).hexdigest()
            ledger = open_control_dir()
            try:
                preview = strict_json(read_private(ledger, 'source-preview-' + key + '.json'))
                intent_bytes = read_private(ledger, 'source-intent-' + key + '.json')
                intent = strict_json(intent_bytes)
                if (preview.get('key') != key or preview.get('request', {}).get('OperationID') != source['operation_id'] or
                        intent.get('key') != key or intent.get('version') != 1):
                    raise JournalError('source pending identity differs')
                intent_digest = hashlib.sha256(intent_bytes).hexdigest()
            finally:
                os.close(ledger)
            signal.pidfd_send_signal(pidfd, signal.SIGKILL)
            waited, status = os.waitpid(pid, 0)
            reaped = True
            if waited != pid or not os.WIFSIGNALED(status) or os.WTERMSIG(status) != signal.SIGKILL:
                raise JournalError('original supervisor kill not observed')
            replacement = _replacement(deadline)
            if replacement['pid'] == pid and replacement['starttime'] == start:
                raise JournalError('replacement process identity reused')
            if before != (journal._read_genesis(realm), journal._read(realm, 'realm-app.json')):
                raise JournalError('old realm files changed during restart')
            ledger = open_control_dir()
            try:
                if read_private(ledger, 'source-intent-' + key + '.json') != intent_bytes:
                    raise JournalError('original source intent changed during restart')
                try:
                    read_private(ledger, 'source-handoff-installed-' + key + '.json')
                except FileNotFoundError:
                    pass
                else:
                    raise JournalError('source handoff unexpectedly installed')
            finally:
                os.close(ledger)
        finally:
            os.close(realm)
        return evidence, {'supervisor_pid': pid, 'supervisor_starttime': start, 'kill_signal': 'SIGKILL',
                          'direct_wait_confirmed': True, 'replacement': replacement,
                          'source_intent_digest': intent_digest, 'original_binding_unchanged': True,
                          'realm_reissued': False, 'source_handoff_pending': True}
    finally:
        if handoff_write is not None:
            os.close(handoff_write)
        if not reaped:
            try:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                os.waitpid(pid, 0)
            except (OSError, ChildProcessError):
                pass
        os.close(read_fd)
        os.close(pidfd)
