"""Actual isolated loopback HTTP/listener negatives; no APP data is served."""
import hashlib
import os
import select
import signal
import socket
import time

import dns
import network_verify
from invoke import PR_SET_NO_NEW_PRIVS, PR_SET_PDEATHSIG, _prctl
from ipc import _receive
from journal import JournalError, canonical, process_start
from origins import _identity, _thread_credentials

REQUEST = b'GET / HTTP/1.0\r\nHost: localhost\r\n\r\n'
RESPONSE = b'HTTP/1.0 204 No Content\r\nContent-Length: 0\r\n\r\n'


def _child(channel, parent):
    try:
        fd = os.dup(channel.fileno())
        null = os.open('/dev/null', os.O_RDWR | os.O_CLOEXEC)
        for target in (0, 1, 2):
            os.dup2(null, target, inheritable=False)
        os.dup2(fd, 3, inheritable=False)
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
        peer = socket.socket(fileno=3)
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM | socket.SOCK_CLOEXEC)
        listener.settimeout(15)
        listener.bind(('127.0.0.1', 0))
        listener.listen(1)
        body = canonical({'version': 1, 'address': '127.0.0.1', 'port': listener.getsockname()[1],
                          'inode': os.fstat(listener.fileno()).st_ino})
        if peer.send(body) != len(body):
            os._exit(124)
        connection, address = listener.accept()
        with connection:
            connection.settimeout(5)
            if address[0] != '127.0.0.1' or connection.recv(4096) != REQUEST:
                os._exit(123)
            connection.sendall(RESPONSE)
        if peer.recv(1) != b'X':
            os._exit(122)
        listener.close()
        os._exit(0)
    except BaseException:
        os._exit(126)


def exercise(life, postgres, scenario):
    if scenario not in ('extra-http', 'extra-endpoint') or life.invalid or life.stage != 'source_maintenance':
        raise JournalError('listener negative requires a fresh source realm')
    endpoint = postgres.observe(life.recoveries)
    network_verify.networks(endpoint['network_ids'], time.monotonic() + 5)
    action = {'scenario': scenario, 'address': '127.0.0.1', 'source_content': False}
    life._admit('negative-listener-probe', action)
    parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
    pid = pidfd = None
    joined = False
    try:
        parent_pid = os.getpid()
        parent_identity = _identity(parent_pid, full=True)
        pid = os.fork()
        if pid == 0:
            parent.close()
            _child(child, parent_pid)
        child.close()
        pidfd = os.pidfd_open(pid, 0)
        frame, credentials = _receive(parent, time.monotonic() + 10)
        item = _identity(pid, full=True)
        if (credentials != (pid, 10002, 10002) or type(frame) is not dict or
                set(frame) != {'version', 'address', 'port', 'inode'} or type(frame['version']) is not int or
                frame['version'] != 1 or frame['address'] != '127.0.0.1' or type(frame['port']) is not int or
                not 0 < frame['port'] < 65536 or type(frame['inode']) is not int or frame['inode'] <= 0 or
                item is None or item['parent'] != parent_pid or item['exe'] != parent_identity['exe'] or
                item['exe_sha256'] != parent_identity['exe_sha256'] or item['cgroup'] != parent_identity['cgroup'] or
                select.select([pidfd], [], [], 0)[0] or process_start(pid) != item['starttime'] or
                frame['inode'] not in dns._fd_inodes(pid)):
            raise JournalError('extra live listener identity differs')
        _thread_credentials(pid, 1)
        entries = dns._socket_table(os.getpid(), 'tcp')
        if frame not in [{'version': 1, **entry} for entry in entries]:
            raise JournalError('actual listener is absent from kernel socket table')
        probe = {'pid': pid, 'starttime': item['starttime'], 'executable_sha256': item['exe_sha256'],
                 'cgroup_sha256': hashlib.sha256(item['cgroup'].encode('ascii')).hexdigest(),
                 'uid': 10002, 'gid': 10002, 'listener': frame}
        life._register('negative-listener-probe', probe)
        with socket.create_connection(('127.0.0.1', frame['port']), timeout=5) as connection:
            connection.settimeout(5)
            connection.sendall(REQUEST)
            data = bytearray()
            while len(data) < len(RESPONSE):
                chunk = connection.recv(len(RESPONSE) - len(data))
                if not chunk:
                    break
                data.extend(chunk)
        if bytes(data) != RESPONSE:
            raise JournalError('actual loopback HTTP response differs')
        try:
            network_verify.networks(endpoint['network_ids'], time.monotonic() + 5)
        except JournalError as error:
            if str(error) != 'guest root namespace has an unregistered TCP listener':
                raise
        else:
            raise JournalError('unadmitted live listener was not detected')
        life.invalidate('negative_unregistered_listener')
        if parent.send(b'X') != 1:
            raise JournalError('listener probe could not close')
        waited, status = os.waitpid(pid, 0)
        joined = True
        if (waited != pid or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0 or
                not select.select([pidfd], [], [], 0)[0]):
            raise JournalError('actual listener process exit was not joined')
        network_verify.networks(endpoint['network_ids'], time.monotonic() + 5)
        try:
            life.roots.scan()
        except Exception:
            pass
        else:
            raise JournalError('listener invalidation did not remain sticky')
        result = {'scenario': scenario, 'probe': probe, 'http_response_sha256': hashlib.sha256(RESPONSE).hexdigest(),
                  'live_listener_rejected': True, 'listener_removed': True, 'direct_wait': True,
                  'pidfd_exited': True, 'sticky_invalid': True}
        life.journal.append('registration', {'name': 'negative-listener-complete',
                                             'identity_digest': hashlib.sha256(canonical(result)).hexdigest()})
        return result
    finally:
        child.close()
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
