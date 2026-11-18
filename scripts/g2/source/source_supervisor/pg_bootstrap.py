"""Fixed, local-only database creation and physical identity observation."""
import base64
import datetime
import hashlib
import hmac
import os
import ipaddress
import re
import socket
import struct
import time

from contract import POSTGRES_HOSTS, POSTGRES_PORT
from journal import JournalError, strict_json

MAX_MESSAGE = 65536
MAX_TOTAL = 1024 * 1024
DATABASES = {'app': 'houfeng', 'w1': 'g2_w1', 'w2': 'g2_w2', 'control': 'g2_control'}
IDENTITY_COLUMNS = ('system_identifier', 'database_oid', 'version_num', 'archive_mode', 'archive_command',
                    'wal_senders', 'replication_slots_limit', 'replication_slots', 'replication_logins',
                    'prepared', 'other_sessions')
IDENTITY_QUERY = (b'SELECT system_identifier::text AS system_identifier, '
                  b'(SELECT oid::text FROM pg_database WHERE datname=current_database()) AS database_oid, '
                  b"current_setting('server_version_num') AS version_num, "
                  b"current_setting('archive_mode') AS archive_mode, "
                  b"current_setting('archive_command') AS archive_command, "
                  b"current_setting('max_wal_senders') AS wal_senders, "
                  b"current_setting('max_replication_slots') AS replication_slots_limit, "
                  b'(SELECT count(*)::text FROM pg_replication_slots) AS replication_slots, '
                  b"(SELECT count(*)::text FROM pg_roles WHERE rolreplication AND rolcanlogin AND rolname <> 'postgres') AS replication_logins, "
                  b'(SELECT count(*)::text FROM pg_prepared_xacts) AS prepared, '
                  b"(SELECT count(*)::text FROM pg_stat_activity WHERE backend_type='client backend' "
                  b'AND pid<>pg_backend_pid()) AS other_sessions FROM pg_control_system()\0')


def _message(sock, deadline, total):
    def read_exact(length):
        result = bytearray()
        while len(result) < length:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise JournalError('PostgreSQL bootstrap deadline')
            sock.settimeout(remaining)
            data = sock.recv(length - len(result))
            if not data:
                raise JournalError('PostgreSQL bootstrap connection closed')
            result.extend(data)
        return bytes(result)
    header = read_exact(5)
    length = struct.unpack('!I', header[1:])[0]
    if length < 4 or length > MAX_MESSAGE:
        raise JournalError('PostgreSQL bootstrap frame invalid')
    total[0] += length + 1
    if total[0] > MAX_TOTAL:
        raise JournalError('PostgreSQL bootstrap output exceeded limit')
    return header[:1], read_exact(length - 4)


def _send(sock, kind, data, deadline):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise JournalError('PostgreSQL bootstrap deadline')
    sock.settimeout(remaining)
    sock.sendall(kind + struct.pack('!I', len(data) + 4) + data)


def _attributes(value):
    result = {}
    for entry in value.split(b','):
        key, sep, item = entry.partition(b'=')
        if not sep or len(key) != 1 or key in result or not item:
            raise JournalError('PostgreSQL bootstrap authentication invalid')
        result[key] = item
    return result


def _run(name, password, operation, query_override=None, host_override=None, session_hold=None, deadline=None):
    if name not in DATABASES or operation not in ('create', 'identity', 'records', 'settlement'):
        raise JournalError('unrecognized fixed PostgreSQL action')
    if session_hold is not None and (name != 'app' or operation != 'identity' or query_override is not None):
        raise JournalError('extra session fixture cannot change PostgreSQL action')
    if type(password) is not str or len(password) != 64 or any(ch not in '0123456789abcdef' for ch in password):
        raise JournalError('PostgreSQL bootstrap credential invalid')
    deadline = min(deadline, time.monotonic() + 60) if deadline is not None else time.monotonic() + 60
    if time.monotonic() >= deadline:
        raise JournalError('PostgreSQL bootstrap deadline')
    total = [0]
    database = 'postgres' if operation == 'create' else DATABASES[name]
    host = POSTGRES_HOSTS[name] if host_override is None else host_override
    if host_override is not None:
        try:
            address = ipaddress.IPv4Address(host_override)
        except (ValueError, TypeError) as exc:
            raise JournalError('APP observation endpoint invalid') from exc
        if name != 'app' or str(address) != host_override or not address.is_private:
            raise JournalError('APP observation endpoint invalid')
    with socket.create_connection((host, POSTGRES_PORT), timeout=min(5, deadline - time.monotonic())) as sock:
        startup = (struct.pack('!I', 196608) + b'user\0postgres\0database\0' + database.encode('ascii') +
                   b'\0client_encoding\0UTF8\0application_name\0g2_root_identity\0\0')
        sock.sendall(struct.pack('!I', len(startup) + 4) + startup)
        kind, body = _message(sock, deadline, total)
        if kind != b'R' or len(body) < 5 or struct.unpack('!I', body[:4])[0] != 10:
            raise JournalError('PostgreSQL SCRAM authentication unavailable')
        mechanisms = body[4:].split(b'\0')
        if b'SCRAM-SHA-256' not in mechanisms or mechanisms[-2:] != [b'', b'']:
            raise JournalError('PostgreSQL SCRAM mechanism differs')
        nonce = base64.b64encode(os.urandom(24)).rstrip(b'=')
        bare = b'n=postgres,r=' + nonce
        first = b'n,,' + bare
        _send(sock, b'p', b'SCRAM-SHA-256\0' + struct.pack('!I', len(first)) + first, deadline)
        kind, body = _message(sock, deadline, total)
        if kind != b'R' or len(body) < 5 or struct.unpack('!I', body[:4])[0] != 11:
            raise JournalError('PostgreSQL SCRAM challenge unavailable')
        server_first = body[4:]
        fields = _attributes(server_first)
        if set(fields) != {b'r', b's', b'i'} or not fields[b'r'].startswith(nonce) or len(fields[b'r']) <= len(nonce):
            raise JournalError('PostgreSQL SCRAM challenge differs')
        try:
            salt = base64.b64decode(fields[b's'], validate=True)
            iterations = int(fields[b'i'])
        except (ValueError, TypeError) as exc:
            raise JournalError('PostgreSQL SCRAM parameters invalid') from exc
        if not 8 <= len(salt) <= 128 or not 4096 <= iterations <= 1000000:
            raise JournalError('PostgreSQL SCRAM parameters outside bounds')
        salted = hashlib.pbkdf2_hmac('sha256', password.encode('ascii'), salt, iterations)
        client_key = hmac.new(salted, b'Client Key', hashlib.sha256).digest()
        stored_key = hashlib.sha256(client_key).digest()
        final_without_proof = b'c=biws,r=' + fields[b'r']
        auth = bare + b',' + server_first + b',' + final_without_proof
        signature = hmac.new(stored_key, auth, hashlib.sha256).digest()
        proof = bytes(a ^ b for a, b in zip(client_key, signature))
        server_key = hmac.new(salted, b'Server Key', hashlib.sha256).digest()
        expected = hmac.new(server_key, auth, hashlib.sha256).digest()
        _send(sock, b'p', final_without_proof + b',p=' + base64.b64encode(proof), deadline)
        kind, body = _message(sock, deadline, total)
        if kind != b'R' or len(body) < 5 or struct.unpack('!I', body[:4])[0] != 12:
            raise JournalError('PostgreSQL SCRAM confirmation unavailable')
        final = _attributes(body[4:])
        try:
            actual = base64.b64decode(final.get(b'v', b''), validate=True)
        except (ValueError, TypeError) as exc:
            raise JournalError('PostgreSQL SCRAM verifier invalid') from exc
        if set(final) != {b'v'} or not hmac.compare_digest(actual, expected):
            raise JournalError('PostgreSQL SCRAM verifier differs')
        authenticated, backend_pid = False, None
        while True:
            kind, body = _message(sock, deadline, total)
            if kind == b'R' and body == struct.pack('!I', 0) and not authenticated:
                authenticated = True
            elif kind == b'S' and authenticated:
                continue
            elif kind == b'K' and authenticated and backend_pid is None and len(body) == 8:
                backend_pid = struct.unpack('!I', body[:4])[0]
                if backend_pid == 0:
                    raise JournalError('PostgreSQL backend identity unavailable')
            elif kind == b'Z' and body == b'I' and authenticated:
                break
            else:
                raise JournalError('PostgreSQL bootstrap authentication did not settle')
        if session_hold is not None:
            if backend_pid is None:
                raise JournalError('extra PostgreSQL backend was not registered')
            session_hold(sock, backend_pid, deadline)
            _send(sock, b'X', b'', deadline)
            return {'backend_pid': backend_pid}
        if operation == 'settlement':
            if name != 'app' or backend_pid is None or type(query_override) is not bytes:
                raise JournalError('settlement observer configuration invalid')
            _fixed_command(sock, b'BEGIN READ ONLY ISOLATION LEVEL REPEATABLE READ\0', b'BEGIN\0', b'T', deadline, total)
            _send(sock, b'Q', query_override, deadline)
            kind, body = _message(sock, deadline, total)
            if kind != b'T' or _row_description(body, ('state',)) != ('state',):
                raise JournalError('settlement observation columns differ')
            kind, body = _message(sock, deadline, total)
            if kind != b'D':
                raise JournalError('settlement observation row missing')
            row = _data_row(body, 1)
            kind, body = _message(sock, deadline, total)
            if kind != b'C' or body != b'SELECT 1\0':
                raise JournalError('settlement observation SELECT incomplete')
            kind, body = _message(sock, deadline, total)
            if kind != b'Z' or body != b'T':
                raise JournalError('settlement observation transaction unavailable')
            _fixed_command(sock, b'COMMIT\0', b'COMMIT\0', b'I', deadline, total)
            _send(sock, b'X', b'', deadline)
            return backend_pid, strict_json(row[0].encode('ascii'))
        query = ((b'CREATE DATABASE ' + DATABASES[name].encode('ascii') + b'\0') if operation == 'create'
                 else IDENTITY_QUERY if operation == 'identity' else query_override)
        if type(query) is not bytes or not query.endswith(b'\0') or len(query) > 16384:
            raise JournalError('fixed PostgreSQL query unavailable')
        _send(sock, b'Q', query, deadline)
        completed = False
        columns = row = None
        while True:
            kind, body = _message(sock, deadline, total)
            if operation != 'create' and kind == b'T' and columns is None:
                expected_columns = IDENTITY_COLUMNS if operation == 'identity' else ('state',)
                columns = _row_description(body, expected_columns)
                if columns != expected_columns:
                    raise JournalError('PostgreSQL observation columns differ')
            elif operation != 'create' and kind == b'D' and columns is not None and row is None:
                row = _data_row(body, len(columns))
            elif kind == b'C' and not completed and body == (b'CREATE DATABASE\0' if operation == 'create' else b'SELECT 1\0'):
                completed = True
            elif kind == b'Z' and body == b'I' and completed:
                break
            else:
                raise JournalError('fixed PostgreSQL action did not complete')
        _send(sock, b'X', b'', deadline)
    if operation == 'create':
        return {'database_name': DATABASES[name], 'command': 'CREATE DATABASE', 'completed': True}
    if row is None:
        raise JournalError('PostgreSQL physical identity row unavailable')
    if operation == 'records':
        return strict_json(row[0].encode('ascii'))
    result = dict(zip(IDENTITY_COLUMNS, row))
    if (not result['system_identifier'].isdigit() or int(result['system_identifier']) <= 0 or
            not result['database_oid'].isdigit() or int(result['database_oid']) <= 0 or
            not result['version_num'].isdigit() or not 160000 <= int(result['version_num']) < 170000 or
            result['archive_mode'] != 'off' or result['archive_command'] != '' or
            any(result[key] != '0' for key in ('wal_senders', 'replication_slots_limit', 'replication_slots',
                                                'replication_logins', 'prepared', 'other_sessions'))):
        raise JournalError('PostgreSQL physical identity or isolation settings differ')
    return {'system_identifier': result['system_identifier'], 'database_oid': int(result['database_oid']),
            'database_name': DATABASES[name], 'version_num': int(result['version_num'])}


def _fixed_command(sock, query, completion, ready, deadline, total):
    _send(sock, b'Q', query, deadline)
    kind, body = _message(sock, deadline, total)
    if kind != b'C' or body != completion:
        raise JournalError('fixed PostgreSQL transaction command incomplete')
    kind, body = _message(sock, deadline, total)
    if kind != b'Z' or body != ready:
        raise JournalError('fixed PostgreSQL transaction state differs')


def _row_description(body, expected):
    if len(body) < 2 or struct.unpack_from('!H', body)[0] != len(expected):
        raise JournalError('PostgreSQL row description invalid')
    offset, columns = 2, []
    for _ in expected:
        end = body.find(b'\0', offset)
        if end < offset or end + 19 > len(body):
            raise JournalError('PostgreSQL row description truncated')
        columns.append(body[offset:end].decode('ascii', 'strict'))
        if struct.unpack_from('!H', body, end + 17)[0] != 0:
            raise JournalError('PostgreSQL identity is not text format')
        offset = end + 19
    if offset != len(body):
        raise JournalError('PostgreSQL row description has trailing bytes')
    return tuple(columns)


def _data_row(body, count):
    if len(body) < 2 or struct.unpack_from('!H', body)[0] != count:
        raise JournalError('PostgreSQL identity row invalid')
    offset, result = 2, []
    for _ in range(count):
        if offset + 4 > len(body):
            raise JournalError('PostgreSQL identity row truncated')
        size = struct.unpack_from('!i', body, offset)[0]
        offset += 4
        if size < 0 or size > 8192 or offset + size > len(body):
            raise JournalError('PostgreSQL identity value invalid')
        result.append(body[offset:offset + size].decode('ascii', 'strict'))
        offset += size
    if offset != len(body):
        raise JournalError('PostgreSQL identity row has trailing bytes')
    return result


def create_database(name, password):
    return _run(name, password, 'create')


def observe_identity(name, password):
    return _run(name, password, 'identity')

def _settlement_backend(value):
    if (type(value) is not dict or set(value) != {'pid', 'started', 'role'} or
            type(value['pid']) is not int or not 0 < value['pid'] < (1 << 31) or
            type(value['started']) is not str or type(value['role']) is not str or
            re.fullmatch(r'[a-z_][a-z0-9_]{0,62}', value['role']) is None):
        raise JournalError('settlement backend identity invalid')
    try:
        started = datetime.datetime.fromisoformat(value['started'].replace('Z', '+00:00'))
    except ValueError as exc:
        raise JournalError('settlement backend timestamp invalid') from exc
    if started.tzinfo is None:
        raise JournalError('settlement backend timestamp lacks timezone')
    return value['pid'], started, value['role']


def observe_migration_settlement(password, host, application, current_backend, retired_backends, managed_roles, deadline):
    """Observe one committed read-only snapshot using a single bootstrap socket."""
    current = _settlement_backend(current_backend)
    if (type(application) is not dict or type(retired_backends) is not list or
            type(managed_roles) is not list or not managed_roles or len(managed_roles) > 64 or
            any(type(role) is not str or re.fullmatch(r'[a-z_][a-z0-9_]{0,62}', role) is None
                for role in managed_roles)):
        raise JournalError('settlement observation inputs invalid')
    retired = [_settlement_backend(value) for value in retired_backends]
    if current in retired:
        raise JournalError('current backend is retired')
    roles = ','.join("'" + role + "'" for role in sorted(set(managed_roles)))
    pids = ','.join(str(pid) for pid in sorted({current[0]} | {value[0] for value in retired}))
    query = ("SELECT json_build_object('system_identifier',c.system_identifier::text,"
             "'database_oid',(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()),"
             "'database_name',current_database(),'observer_pid',pg_backend_pid(),"
             "'prepared',(SELECT count(*) FROM pg_catalog.pg_prepared_xacts),"
             "'clients',COALESCE((SELECT json_agg(json_build_object('pid',a.pid,"
             "'started',a.backend_start::text,'role',a.usename,'database',a.datname) ORDER BY a.pid) "
             "FROM pg_catalog.pg_stat_activity a WHERE a.backend_type='client backend' AND "
             "(a.datname=current_database() OR a.usename IN (" + roles + ") OR a.pid IN (" + pids + "))),"
             "'[]'::json))::text AS state FROM pg_catalog.pg_control_system() c\0").encode('ascii')
    observer_pid, observed = _run('app', password, 'settlement', query, host, deadline=deadline)
    if (type(observed) is not dict or set(observed) != {'system_identifier', 'database_oid', 'database_name',
            'observer_pid', 'prepared', 'clients'} or observed['system_identifier'] != application.get('SystemIdentifier') or
            observed['database_oid'] != application.get('DatabaseOID') or
            observed['database_name'] != application.get('DatabaseName') or
            observed['observer_pid'] != observer_pid or observed['prepared'] != 0 or
            type(observed['clients']) is not list):
        raise JournalError('settlement physical identity or prepared transactions differ')
    if any(type(value) is not dict or set(value) != {'pid', 'started', 'role', 'database'} or
           type(value['database']) is not str for value in observed['clients']):
        raise JournalError('settlement backend database invalid')
    clients = [(_settlement_backend({key: value[key] for key in ('pid', 'started', 'role')}), value)
               for value in observed['clients']]
    observers = [(identity, value) for identity, value in clients if identity[0] == observer_pid]
    relevant = [(identity, value) for identity, value in clients
                if value['database'] == application['DatabaseName'] or value['role'] in managed_roles]
    if (len(observers) != 1 or observers[0][0][2] != 'postgres' or
            observers[0][1]['database'] != application['DatabaseName'] or
            sum(identity == current and value['database'] == application['DatabaseName']
                for identity, value in relevant) != 1 or
            any(identity in retired for identity, _ in clients) or
            len(relevant) != 2 or current[0] == observer_pid):
        raise JournalError('settlement client backends differ')
    observer = {key: observers[0][1][key] for key in ('pid', 'started', 'role')}
    observer['started'] = observers[0][0][1].isoformat()
    return {'application': application.copy(), 'current_backend': current_backend.copy(),
            'observer_backend': observer, 'retired_backends': [item.copy() for item in retired_backends],
            'remaining_clients': 0, 'prepared_transactions': 0, 'transaction_committed': True}


def observe_migration_backend(password, host, application, backend, deadline):
    """Observe the blocked migrator through an independent bootstrap session."""
    if (type(backend) is not dict or set(backend) != {'pid', 'started', 'role'} or
            type(backend['pid']) is not int or not 0 < backend['pid'] < (1 << 31) or
            type(backend['started']) is not str or type(backend['role']) is not str or
            type(application) is not dict):
        raise JournalError('migration backend identity invalid')
    query = ("SELECT json_build_object('pid',a.pid,'started',a.backend_start::text,"
             "'role',a.usename,'database',a.datname,'database_oid',d.oid,"
             "'system_identifier',c.system_identifier::text,'state',a.state)::text AS state "
             "FROM pg_catalog.pg_stat_activity a JOIN pg_catalog.pg_database d ON d.datname=a.datname "
             "CROSS JOIN pg_catalog.pg_control_system() c WHERE a.backend_type='client backend' "
             "AND a.pid=%d AND a.datname=current_database()\0" % backend['pid']).encode('ascii')
    observed = _run('app', password, 'records', query, host, deadline=deadline)
    if (type(observed) is not dict or set(observed) != {'pid', 'started', 'role', 'database', 'database_oid',
                                                         'system_identifier', 'state'} or
            observed['pid'] != backend['pid'] or observed['role'] != backend['role'] or
            observed['database'] != application.get('DatabaseName') or
            observed['database_oid'] != application.get('DatabaseOID') or
            observed['system_identifier'] != application.get('SystemIdentifier') or
            observed['state'] not in ('idle', 'idle in transaction')):
        raise JournalError('migration backend readback differs')
    try:
        actual = datetime.datetime.fromisoformat(observed['started'].replace('Z', '+00:00'))
        expected = datetime.datetime.fromisoformat(backend['started'].replace('Z', '+00:00'))
    except (ValueError, TypeError, AttributeError) as exc:
        raise JournalError('migration backend timestamp invalid') from exc
    if actual.tzinfo is None or expected.tzinfo is None or actual != expected:
        raise JournalError('migration backend start time differs')
    return backend.copy()


def observe_records(password, host, a, b):
    # Identifiers are exclusively the two verified initializer outputs. The
    # grammar excludes SQL syntax; table and column names remain fixed here.
    for item in (a, b):
        if (type(item) is not dict or type(item.get('record_id')) is not str or
                re.fullmatch(r'rec_[0-9a-f]{16}', item['record_id']) is None or
                type(item.get('source_id')) is not str):
            raise JournalError('record observation identity invalid')
    if a['record_id'] == b['record_id']:
        raise JournalError('record observation identities overlap')
    source_tables = {'vps': ('vps_assets', 'vps_id', 'vps_'),
                     'monitoring_instance': ('monitoring_instances', 'monitoring_instance_id', 'mi_'),
                     'target': ('targets', 'target_id', 'tg_')}
    source = source_tables.get(a.get('source_kind'))
    if source is None or re.fullmatch(re.escape(source[2]) + r'[0-9a-f]{16}', a['source_id']) is None:
        raise JournalError('source observation identity invalid')
    a_id, b_id, source_id = a['record_id'], b['record_id'], a['source_id']
    sql = ("SELECT json_build_object("
           "'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),"
           "'database_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),"
           "'a_record',coalesce((SELECT lifecycle::text FROM public.records WHERE record_id='%s' AND project_id='default'),''),"
           "'b_record',coalesce((SELECT lifecycle::text FROM public.records WHERE record_id='%s' AND project_id='default'),''),"
           "'a_asset',EXISTS(SELECT 1 FROM public.%s WHERE %s='%s'),"
           "'a_reservations',(SELECT count(*) FROM public.deletion_reservations WHERE project_id='default' AND object_kind='record' AND object_id='%s'),"
           "'b_reservations',(SELECT count(*) FROM public.deletion_reservations WHERE project_id='default' AND object_kind='record' AND object_id='%s'),"
           "'b_preview_rows',coalesce((SELECT json_agg(json_build_object('reservation_id',r.reservation_id,"
           "'state',r.state,'fence_epoch',r.fence_epoch,'operations',(SELECT count(*) FROM public.record_purge_operations o "
           "WHERE o.reservation_id=r.reservation_id AND o.project_id=r.project_id)) ORDER BY r.reservation_id) "
           "FROM public.deletion_reservations r WHERE r.project_id='default' AND r.object_kind='record' "
           "AND r.object_id='%s'),'[]'::json),"
           "'b_operations',coalesce((SELECT json_agg(json_build_object("
           "'operation_id',o.operation_id,'reservation_id',o.reservation_id,'project_id',o.project_id,"
           "'object_id',r.object_id,'actor_id',o.actor_id,'deployment_id',o.deployment_id,"
           "'operation_state',o.operation_state,'reason_code',o.reason_code,"
           "'contract_version',o.deletion_contract_version,'reservation_state',r.state,'fence_epoch',r.fence_epoch,"
           "'ledger_sequence',o.ledger_sequence,'ledger_hash_null',o.ledger_entry_hash IS NULL,"
           "'owner_id',o.owner_id,'owner_generation',o.owner_generation,"
           "'reservation_owner_id',r.owner_id,'reservation_owner_generation',r.owner_generation,"
           "'owner_expiry_equal',o.owner_expires_at=r.owner_expires_at) ORDER BY o.operation_id) "
           "FROM public.record_purge_operations o JOIN public.deletion_reservations r "
           "ON r.reservation_id=o.reservation_id AND r.project_id=o.project_id "
           "WHERE r.project_id='default' AND r.object_kind='record' AND r.object_id='%s'),'[]'::json))::text AS state\0") % (
               a_id, b_id, source[0], source[1], source_id, a_id, b_id, b_id, b_id)
    return _run('app', password, 'records', sql.encode('ascii'), host)
