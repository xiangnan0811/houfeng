"""Credentialled startup barrier for the already-invalid negative producer."""
import datetime
import hashlib
import re
import select
import socket
import time

from ipc import _receive, _send
from journal import JournalError, canonical, process_start
import pg_bootstrap

START = re.compile(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.(\d{1,9}))?Z$')


def _timestamp(value):
    if type(value) is not str:
        raise JournalError('backend start time invalid')
    match = START.fullmatch(value)
    if match is None or match.group(1) and len(match.group(1)) > 6 and any(ch != '0' for ch in match.group(1)[6:]):
        raise JournalError('backend start time is not exact PostgreSQL precision')
    try:
        return datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError as exc:
        raise JournalError('backend start time invalid') from exc


class NegativeBackendChannel:
    def __init__(self, sock, identity, pidfd, life, password, app_host, application):
        if (sock.family != socket.AF_UNIX or sock.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE) != socket.SOCK_SEQPACKET or
                not life.invalid or identity['exe'] != '/opt/g2/c80-comparison-negative'):
            raise JournalError('negative producer channel identity invalid')
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        self.sock, self.identity, self.pidfd, self.life = sock, identity, pidfd, life
        self.password, self.app_host, self.application = password, app_host, application
        self.observation = None

    def admit_pending(self, deadline):
        if (not self.life.invalid or self.observation is not None or time.monotonic() >= deadline or
                select.select([self.pidfd], [], [], 0)[0] or
                process_start(self.identity['pid']) != self.identity['starttime']):
            raise JournalError('negative producer backend admission is unavailable')

    def serve_one(self, deadline):
        event, peer = _receive(self.sock, deadline)
        if (peer != (self.identity['pid'], 10002, 10002) or type(event) is not dict or
                set(event) != {'version', 'op', 'backend_pid', 'backend_start', 'role'} or
                type(event['version']) is not int or event['version'] != 1 or event['op'] != 'backend_ready' or
                type(event['backend_pid']) is not int or not 0 < event['backend_pid'] < (1 << 31) or
                event['role'] != 'houfeng_runtime'):
            raise JournalError('negative producer backend frame invalid')
        started = _timestamp(event['backend_start'])
        query = ("SELECT json_build_object('pid',a.pid,'role',a.usename,'application_name',a.application_name,"
                 "'state',a.state,'backend_start',a.backend_start::text,'database_oid',d.oid::int,"
                 "'system_identifier',c.system_identifier::text,'other_sessions',"
                 "(SELECT count(*)::int FROM pg_stat_activity WHERE backend_type='client backend' "
                 "AND pid<>pg_backend_pid()))::text AS state FROM pg_stat_activity a "
                 "JOIN pg_database d ON d.datname=a.datname CROSS JOIN pg_control_system() c "
                 "WHERE a.backend_type='client backend' AND a.datname=current_database() AND a.pid=%d\0" %
                 event['backend_pid']).encode('ascii')
        actual = pg_bootstrap._run('app', self.password, 'records', query_override=query, host_override=self.app_host)
        if (type(actual) is not dict or set(actual) != {'pid', 'role', 'application_name', 'state', 'backend_start',
                                                        'database_oid', 'system_identifier', 'other_sessions'} or
                actual['pid'] != event['backend_pid'] or actual['role'] != 'houfeng_runtime' or
                actual['application_name'] != 'g2-c80-comparison-negative' or actual['state'] != 'idle' or
                actual['database_oid'] != self.application['DatabaseOID'] or
                actual['system_identifier'] != self.application['SystemIdentifier'] or actual['other_sessions'] != 1):
            raise JournalError('negative runtime backend independent observation differs')
        try:
            observed_start = datetime.datetime.fromisoformat(actual['backend_start'])
        except (TypeError, ValueError) as exc:
            raise JournalError('observed backend start time invalid') from exc
        if observed_start.tzinfo is None or observed_start != started or select.select([self.pidfd], [], [], 0)[0]:
            raise JournalError('negative runtime backend identity changed')
        observation = {'backend_pid': event['backend_pid'], 'backend_start': event['backend_start'],
                       'role': event['role'], 'application_name': actual['application_name'],
                       'database_oid': actual['database_oid'], 'system_identifier': actual['system_identifier'],
                       'other_sessions': actual['other_sessions']}
        self.life.journal.append('registration', {'name': 'comparison-negative-backend',
                                                   'identity_digest': hashlib.sha256(canonical(observation)).hexdigest()})
        self.observation = observation
        _send(self.sock, {'version': 1, 'op': 'backend_ack'}, deadline)
