"""Disposable probe of actual credentialled root realm request validators."""
import array
import hashlib
import os
import secrets
import select
import signal
import socket
import time

from contract import MAX_FRAME
from invoke import PR_SET_NO_NEW_PRIVS, PR_SET_PDEATHSIG, _prctl, _wait_stop
from ipc import RealmChannel, _phase_matches, _receive, _request_identity
from journal import JournalError, canonical, process_start, strict_json
from origins import _identity, _thread_credentials
from phase import read_phases


def _child(sock, parent, phase, nonce):
    try:
        source = os.dup(sock.fileno())
        null = os.open('/dev/null', os.O_RDWR | os.O_CLOEXEC)
        for fd in (0, 1, 2):
            os.dup2(null, fd, inheritable=False)
        os.dup2(source, 3, inheritable=False)
        for name in os.listdir('/proc/self/fd'):
            if name.isdecimal() and int(name) >= 4:
                try:
                    os.close(int(name))
                except OSError:
                    pass
        os.setgroups([])
        os.setresgid(10002, 10002, 10002)
        os.setresuid(10002, 10002, 10002)
        _prctl(PR_SET_NO_NEW_PRIVS, 1)
        _prctl(PR_SET_PDEATHSIG, signal.SIGKILL)
        if os.getppid() != parent:
            os._exit(125)
        os.kill(os.getpid(), signal.SIGSTOP)
        peer = socket.socket(fileno=3)
        base = {'version': 1, 'op': 'realm_observe', 'phase_generation': phase, 'nonce': nonce}
        scenarios = ('valid', 'duplicate', 'extra-field', 'duplicate-key', 'bool-version',
                     'bad-nonce', 'wrong-phase', 'extra-fd', 'truncated')
        for index, name in enumerate(scenarios):
            if peer.recv(1) != bytes([index + 1]):
                os._exit(124)
            request = dict(base)
            if name == 'extra-field':
                request.update(nonce=secrets.token_hex(32), unknown=True)
            elif name == 'bool-version':
                request.update(nonce=secrets.token_hex(32), version=True)
            elif name == 'bad-nonce':
                request['nonce'] = 'A' * 64
            elif name == 'wrong-phase':
                request.update(nonce=secrets.token_hex(32), phase_generation=phase + 1)
            if name == 'duplicate-key':
                body = (b'{"version":1,"version":1,"op":"realm_observe","phase_generation":' +
                        str(phase).encode('ascii') + b',"nonce":"' + secrets.token_hex(32).encode('ascii') + b'"}')
            elif name == 'truncated':
                body = b'x' * (MAX_FRAME + 4096)
            else:
                body = canonical(request)
            if name == 'extra-fd':
                fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
                try:
                    if peer.sendmsg([body], [(socket.SOL_SOCKET, socket.SCM_RIGHTS, array.array('i', [fd]))]) != len(body):
                        os._exit(123)
                finally:
                    os.close(fd)
            elif peer.send(body) != len(body):
                os._exit(123)
            if name == 'valid':
                response = strict_json(peer.recv(MAX_FRAME + 1))
                if peer.send(canonical({'response': response})) <= 0:
                    os._exit(123)
        peer.shutdown(socket.SHUT_WR)
        os._exit(0)
    except BaseException:
        os._exit(126)


def exercise(flow):
    phase = read_phases(flow.app['application'], flow.recovery_id)
    if phase['phase'] != 'quiescent' or flow.source_operation_id is None:
        raise JournalError('RPC probe requires the original quiescent source')
    baseline, before = flow._readback(), flow._effect_snapshot()
    flow.life.invalidate('negative_wrong_rpc')
    nonce = secrets.token_hex(32)
    parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
    parent_identity = _identity(os.getpid(), full=True)
    flow.life._admit('negative-rpc-probe', {'phase_generation': phase['generation'],
                                            'parent_pid': os.getpid(), 'parent_starttime': process_start(os.getpid())})
    pid = pidfd = None
    joined = False
    try:
        parent_pid = os.getpid()
        pid = os.fork()
        if pid == 0:
            parent.close()
            _child(child, parent_pid, phase['generation'], nonce)
        pidfd = os.pidfd_open(pid, 0)
        status = _wait_stop(pid, pidfd, time.monotonic() + 10)
        if not os.WIFSTOPPED(status) or os.WSTOPSIG(status) != signal.SIGSTOP:
            raise JournalError('RPC probe did not stop before its first frame')
        item = _identity(pid, full=True)
        peer_info = os.fstat(child.fileno())
        held = os.stat('/proc/%d/fd/3' % pid)
        null = os.stat('/dev/null')
        if (item is None or item['parent'] != parent_pid or item['exe'] != parent_identity['exe'] or
                item['exe_sha256'] != parent_identity['exe_sha256'] or item['cgroup'] != parent_identity['cgroup'] or
                set(os.listdir('/proc/%d/fd' % pid)) != {'0', '1', '2', '3'} or
                (held.st_dev, held.st_ino) != (peer_info.st_dev, peer_info.st_ino) or
                any(os.stat('/proc/%d/fd/%d' % (pid, fd)).st_rdev != null.st_rdev for fd in (0, 1, 2))):
            raise JournalError('RPC probe process or sole channel identity differs')
        _thread_credentials(pid, 1)
        observed = {'pid': pid, 'starttime': item['starttime'], 'executable_sha256': item['exe_sha256'],
                    'cgroup_sha256': hashlib.sha256(item['cgroup'].encode('ascii')).hexdigest(),
                    'channel_dev': held.st_dev, 'channel_ino': held.st_ino, 'uid': 10002, 'gid': 10002,
                    'supplementary_groups': [], 'no_new_privileges': True}
        flow.life._register('negative-rpc-probe', observed)
        channel = RealmChannel(parent, {'pid': pid, 'starttime': item['starttime'],
                                        'executable_sha256': item['exe_sha256']}, pidfd, flow.journal,
                               flow.roots, flow.life, flow.app_facts, flow.recovery_id,
                               flow.expected['source_guest.py'], None)
        # SO_PASSCRED records the sender's actual uid/pid, even on this inherited socket.
        request = {'version': 1, 'op': 'realm_observe', 'phase_generation': phase['generation'], 'nonce': nonce}
        child.send(canonical(request))
        try:
            channel.serve_one(time.monotonic() + 5)
        except JournalError:
            pass
        else:
            raise JournalError('root peer was accepted as coordinator')
        child.close()
        signal.pidfd_send_signal(pidfd, signal.SIGCONT)
        outcomes = {'wrong-credentials': True}
        cases = ('valid', 'duplicate', 'extra-field', 'duplicate-key', 'bool-version',
                 'bad-nonce', 'wrong-phase', 'extra-fd', 'truncated')
        for index, name in enumerate(cases):
            if parent.send(bytes([index + 1])) != 1:
                raise JournalError('RPC probe control failed')
            if name == 'valid':
                if channel.serve_one(time.monotonic() + 5) is not False:
                    raise JournalError('invalid realm unexpectedly produced authority')
                ack, credentials = _receive(parent, time.monotonic() + 5)
                expected = {'version': 1, 'op': 'realm_observe', 'phase_generation': phase['generation'],
                            'nonce': nonce, 'observation_sequence': 1, 'realm_digest': None,
                            'roots_digest': None, 'valid': False}
                if credentials != (pid, 10002, 10002) or ack != {'response': expected}:
                    raise JournalError('invalid realm response differed')
                outcomes['invalid-realm-response'] = True
                continue
            try:
                if name == 'wrong-phase':
                    frame, credentials = _receive(parent, time.monotonic() + 5)
                    if credentials != (pid, 10002, 10002):
                        raise JournalError('wrong-phase probe credentials differ')
                    _request_identity(frame, channel.seen)
                    _phase_matches(frame, phase)
                else:
                    channel.serve_one(time.monotonic() + 5)
            except (JournalError, ValueError):
                outcomes[name] = True
            else:
                raise JournalError('malformed RPC was accepted')
        try:
            _receive(parent, time.monotonic() + 5)
        except JournalError:
            outcomes['cancelled-channel'] = True
        else:
            raise JournalError('cancelled RPC channel was accepted')
        waited, code = os.waitpid(pid, 0)
        joined = True
        if waited != pid or not os.WIFEXITED(code) or os.WEXITSTATUS(code) != 0 or not select.select([pidfd], [], [], 0)[0]:
            raise JournalError('RPC probe exit is uncertain')
        if (read_phases(flow.app['application'], flow.recovery_id) != phase or flow._effect_snapshot() != before or
                flow._readback() != baseline or not flow.life.invalid or flow.life.origins.allowed_predecessor):
            raise JournalError('RPC probe changed original APP state')
        result = {'phase_generation': phase['generation'], 'source_operation_id': flow.source_operation_id,
                  'probe': observed, 'outcomes': outcomes, 'direct_wait': True, 'pidfd_exited': True,
                  'app_before': baseline, 'app_after': baseline, 'durable_effects_unchanged': True,
                  'root_python_validators_only': True}
        flow.journal.append('registration', {'name': 'negative-rpc-probe-complete',
                                              'identity_digest': hashlib.sha256(canonical(result)).hexdigest()})
        return result
    finally:
        try:
            child.close()
        except OSError:
            pass
        parent.close()
        if pidfd is not None:
            if not joined and not select.select([pidfd], [], [], 0)[0]:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
            if pid is not None and not joined:
                try:
                    os.waitpid(pid, 0)
                except ChildProcessError:
                    pass
            os.close(pidfd)
