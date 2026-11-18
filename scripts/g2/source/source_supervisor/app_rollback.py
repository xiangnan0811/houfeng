"""Real, negative-only restore of a published earlier APP dump into the same database."""
import hashlib
import os
import re
import select
import signal
import stat
import struct
import threading
import time

import pg_bootstrap
import physical_backup
from callback_lifecycle import _cgroup
from ceremony import _private_request
from census import _mount_id
from guest_postgres import _UnixHTTP
from ipc import ClosedRealmChannel
from journal import JournalError, canonical, process_start, strict_json
from origins import _cgroup_has_container, _identity
from phase import open_control_dir, read_phases, read_private

RESTORE = '/usr/lib/postgresql/16/bin/pg_restore'
FIFO = 'g2-app-rollback.fifo'
MAX_IO = 16384


def _require_context(flow, stage):
    if (not flow.life.invalid or not flow.life.closed_maintenance or
            flow.life.invalid_reason != 'negative_app_rollback' or flow.life.stage != stage):
        raise JournalError('APP rollback fixture is outside its permanently closed realm')


def _observe_app(flow, host):
    query = ("SELECT json_build_object('system_identifier',(SELECT system_identifier::text FROM pg_control_system()),"
             "'database_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),"
             "'database_name',current_database(),"
             "'domain_id',(SELECT domain_id FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
             "'domain_epoch',(SELECT identity_epoch FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
             "'source_binding_table',to_regclass('public.source_legacy_retirement_bindings') IS NOT NULL,"
             "'catalog_digest',(SELECT encode(sha256(convert_to(coalesce(string_agg(line,E'\\n' ORDER BY line COLLATE \"C\"),''),'UTF8')),'hex') FROM ("
             "SELECT format('database:%I:%s:%s',d.datname,pg_get_userbyid(d.datdba),coalesce(d.datacl::text,'')) AS line "
             "FROM pg_database d WHERE d.datname=current_database() UNION ALL "
             "SELECT format('role:%I:%s:%s:%s:%s',r.rolname,r.rolcanlogin,r.rolsuper,r.rolinherit,r.rolcreaterole) "
             "FROM pg_roles r WHERE r.rolname IN ('houfeng_runtime','houfeng_platform_admin','houfeng_migrator','houfeng_records_authority') UNION ALL "
             "SELECT format('default-acl:%s:%s:%s:%s',pg_get_userbyid(a.defaclrole),coalesce(n.nspname,''),"
             "a.defaclobjtype,a.defaclacl::text) FROM pg_default_acl a LEFT JOIN pg_namespace n ON n.oid=a.defaclnamespace UNION ALL "
             "SELECT format('schema:%I:%s:%s',n.nspname,pg_get_userbyid(n.nspowner),coalesce(n.nspacl::text,'')) "
             "FROM pg_namespace n WHERE n.nspname IN ('public','record_platform_internal') UNION ALL "
             "SELECT format('relation:%I.%I:%s:%s:%s',n.nspname,c.relname,c.relkind,pg_get_userbyid(c.relowner),coalesce(c.relacl::text,'')) "
             "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN ('public','record_platform_internal') "
             "UNION ALL SELECT format('routine:%I.%I(%s):%s:%s:%s:%s:%s',n.nspname,p.proname,"
             "pg_get_function_identity_arguments(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,p.prosrc,"
             "coalesce(p.proconfig::text,''),coalesce(p.proacl::text,'')) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace "
             "WHERE n.nspname IN ('public','record_platform_internal') UNION ALL "
             "SELECT format('trigger:%I.%I:%s',n.nspname,c.relname,pg_get_triggerdef(t.oid)) FROM pg_trigger t "
             "JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace "
             "WHERE NOT t.tgisinternal AND n.nspname IN ('public','record_platform_internal')) catalog),"
             "'migrations',(SELECT coalesce(json_agg(json_build_object('name',name,'checksum',checksum) "
             "ORDER BY name COLLATE \"C\"),'[]'::json) FROM public.schema_migrations))::text AS state\0")
    value = pg_bootstrap._run('app', flow.postgres.passwords['app'], 'records', query.encode('ascii'), host)
    app = flow.app['application']
    if (type(value) is not dict or set(value) != {'system_identifier', 'database_oid', 'database_name',
            'domain_id', 'domain_epoch', 'source_binding_table', 'catalog_digest', 'migrations'} or
            value['system_identifier'] != app['SystemIdentifier'] or value['database_oid'] != app['DatabaseOID'] or
            value['database_name'] != app['DatabaseName'] or value['domain_id'] != app['ID'] or
            value['domain_epoch'] != app['Epoch'] or type(value['source_binding_table']) is not bool or
            type(value['catalog_digest']) is not str or re.fullmatch(r'[0-9a-f]{64}', value['catalog_digest']) is None or
            type(value['migrations']) is not list or not value['migrations'] or any(
                type(row) is not dict or set(row) != {'name', 'checksum'} or type(row['name']) is not str or
                type(row['checksum']) is not str or re.fullmatch(r'[0-9a-f]{64}', row['checksum']) is None
                for row in value['migrations'])):
        raise JournalError('APP migration or physical identity observation differs')
    names = [row['name'] for row in value['migrations']]
    if names != sorted(set(names)):
        raise JournalError('APP migration inventory is not ordered')
    if value['source_binding_table']:
        query = ("SELECT coalesce(json_agg(json_build_object('operation_id',operation_id,'source_kind',source_kind,"
                 "'source_id',source_id,'ledger_sequence',ledger_sequence,'ledger_entry_hash',encode(ledger_entry_hash,'hex')) "
                 "ORDER BY operation_id COLLATE \"C\"),'[]'::json)::text AS state "
                 "FROM public.source_legacy_retirement_bindings\0")
        value['bindings'] = pg_bootstrap._run('app', flow.postgres.passwords['app'], 'records', query.encode('ascii'), host)
        if type(value['bindings']) is not list or len(value['bindings']) > 4096:
            raise JournalError('APP source binding observation is unavailable')
    else:
        value['bindings'] = []
    return value


def _external(flow):
    key = hashlib.sha256((flow.app['deployment_id'] + '\0default\0' + flow.source_operation_id).encode('utf-8')).hexdigest()
    directory = open_control_dir()
    try:
        receipts = {}
        for prefix in ('source-preview-', 'source-intent-', 'source-witnessed-', 'source-anchored-',
                       'source-handoff-intent-', 'source-handoff-installed-'):
            name = prefix + key + '.json'
            receipts[name] = hashlib.sha256(read_private(directory, name)).hexdigest()
    finally:
        os.close(directory)
    heads = {}
    for name, table in (('w1', 'deletion_ledger_head'), ('w2', 'deletion_witness_head'),
                        ('w2_source', 'source_retirement_head')):
        domain = 'w2' if name == 'w2_source' else name
        query = ("SELECT json_build_object('sequence',sequence,'entry_hash',encode(entry_hash,'hex'))::text AS state "
                 "FROM public." + table + " WHERE singleton\0")
        heads[name] = pg_bootstrap._run(domain, flow.postgres.passwords[domain], 'records', query.encode('ascii'))
        if (type(heads[name]) is not dict or type(heads[name].get('sequence')) is not int or
                type(heads[name].get('entry_hash')) is not str or
                re.fullmatch(r'[0-9a-f]{64}', heads[name]['entry_hash']) is None):
            raise JournalError('external source ledger head is unavailable')
    identities = {}
    for name in ('w1', 'w2', 'control'):
        observed = pg_bootstrap.observe_identity(name, flow.postgres.passwords[name])
        domain = flow.domains[name]
        system_key = 'PostgresSystemIdentifier' if name == 'control' else 'SystemIdentifier'
        if (observed['system_identifier'] != domain[system_key] or observed['database_oid'] != domain['DatabaseOID'] or
                observed['database_name'] != domain['DatabaseName']):
            raise JournalError('external database physical identity changed')
        identities[name] = observed
    return {'receipts': receipts, 'heads': heads, 'identities': identities}


def prepare(flow, predecessor):
    if (flow.life.invalid or flow.life.stage != 'source_maintenance' or type(predecessor) is not dict or
            type(predecessor.get('operation_id')) is not str or not predecessor['operation_id']):
        raise JournalError('APP rollback requires the already committed C80 reservation')
    endpoint = flow.life.check_closed_realm(time.monotonic() + 60)
    host = endpoint['app_host']
    before = _observe_app(flow, host)
    records = pg_bootstrap.observe_records(flow.postgres.passwords['app'], host, flow.pair['a'], flow.pair['b'])
    if records != predecessor.get('reserved'):
        raise JournalError('C80 operation differs before the actual dump')
    if before['source_binding_table'] or any(row['name'] == '0081_record_deletion_successor.sql' for row in before['migrations']):
        raise JournalError('APP rollback input is not the real C80 predecessor')
    flow.life.invalidate('negative_app_rollback')
    flow.life.enter_closed_maintenance()
    backup = physical_backup.backup_negative(flow.life, flow.roots, flow.postgres, flow.invoker, host, 'published',
                                             preserve_catalog=True)
    if backup['dump'].get('ownership_and_acl_included') is not True or not backup['sticky_invalid']:
        raise JournalError('restorable C80 APP backup was not captured')
    return {'app_before': before, 'records_before': records, 'backup': backup,
            'operation_id': predecessor['operation_id'], 'app_host': host}


def probe_closed(flow, prepared):
    _require_context(flow, 'source_maintenance')
    if type(prepared) is not dict or prepared.get('app_host') != flow.life.app_endpoint['app_host'] or _cgroup(flow.life):
        raise JournalError('closed realm probe requires its original empty callback range')
    phase = read_phases(flow.app['application'], flow.recovery_id)
    if phase['phase'] != 'quiescent':
        raise JournalError('closed realm probe requires quiescent maintenance')
    effects = flow._effect_snapshot()
    permits = sorted(name for name in os.listdir(flow.journal._fd) if name.startswith(('launch-authority-', 'launch-used-')))
    records = pg_bootstrap.observe_records(flow.postgres.passwords['app'], prepared['app_host'], flow.pair['a'], flow.pair['b'])
    if records != prepared['records_before']:
        raise JournalError('closed realm baseline differs from the original reservation')
    request = dict(flow._base(), actor_user_id=flow.app['actor_user_id'], record_id=flow.pair['a']['record_id'])
    path = _private_request('service-closed-realm.json', request)
    channels = []
    def channel_factory(identity, pidfd, sock):
        coordinator = {'pid': identity['pid'], 'starttime': identity['starttime'],
                       'executable_sha256': identity['exe_sha256']}
        channel = ClosedRealmChannel(sock, coordinator, pidfd, flow.life, flow.app['application'], flow.recovery_id)
        channels.append(channel)
        return channel
    flow.action_order.append('legacy-c80-service')
    result = flow.invoker.admin('legacy-c80-service', path, channel_factory, time.monotonic() + 120,
                                recovery_id=flow.recovery_id,
                                endpoint_container_id=flow.postgres.containers['app']['id'])
    frame = strict_json(result['stdout']) if result['stdout'] else None
    expected = {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'realm_unavailable'}
    if (result['status'] != 1 or result['stderr'] or frame != expected or type(frame.get('version')) is not int or
            len(channels) != 1 or len(channels[0].observations) != 1 or
            channels[0].observations[0]['phase_generation'] != phase['generation'] or
            read_phases(flow.app['application'], flow.recovery_id) != phase or flow._effect_snapshot() != effects or
            sorted(name for name in os.listdir(flow.journal._fd) if name.startswith(('launch-authority-', 'launch-used-'))) != permits or
            flow.life.origins.allowed_predecessor or _cgroup(flow.life) or
            pg_bootstrap.observe_records(flow.postgres.passwords['app'], prepared['app_host'], flow.pair['a'], flow.pair['b']) != records):
        raise JournalError('retired realm did not reject the service before any callback effect')
    return {'response': frame, 'exit_status': result['status'], 'observation': channels[0].observations[0],
            'coordinator': {'pid': result['pid'], 'starttime': result['starttime']},
            'phase_unchanged': True, 'effects_unchanged': True, 'launch_authority_unchanged': True,
            'child_range_empty': True, 'records_before': records, 'records_after': records}


def _reset_schemas(flow, host):
    def execute(sock, backend, deadline):
        if type(backend) is not int or backend <= 0:
            raise JournalError('rollback control backend is unavailable')
        sql = (b'BEGIN; DROP SCHEMA public CASCADE; DROP SCHEMA record_platform_internal CASCADE; '
               b'CREATE SCHEMA public AUTHORIZATION pg_database_owner; COMMIT\0')
        pg_bootstrap._send(sock, b'Q', sql, deadline)
        expected = [b'BEGIN\0', b'DROP SCHEMA\0', b'DROP SCHEMA\0', b'CREATE SCHEMA\0', b'COMMIT\0']
        seen, total = [], [0]
        while True:
            kind, body = pg_bootstrap._message(sock, deadline, total)
            if kind == b'N':
                continue
            if kind == b'C':
                seen.append(body)
                if seen != expected[:len(seen)]:
                    raise JournalError('fixed APP schema reset command differs')
            elif kind == b'Z' and body == b'I' and seen == expected:
                return
            else:
                raise JournalError('fixed APP schema reset did not commit')
    pg_bootstrap._run('app', flow.postgres.passwords['app'], 'identity', host_override=host, session_hold=execute)


def _collect_restore(postgres, exec_id, outcome):
    connection = _UnixHTTP(postgres.daemon, 120)
    outcome['connection'] = connection
    counts = {1: 0, 2: 0}
    try:
        connection.request('POST', '/v1.41/exec/' + exec_id + '/start', body=b'{"Detach":false,"Tty":false}',
                           headers={'Content-Type': 'application/json'})
        response = connection.getresponse()
        if response.status != 200:
            raise JournalError('fixed restore exec did not start')
        while True:
            header = physical_backup._read_exact(response, 8, eof_ok=True)
            if not header:
                break
            stream, padding, length = header[0], header[1:4], struct.unpack('!I', header[4:])[0]
            if stream not in (1, 2) or padding != b'\0\0\0' or length > MAX_IO - counts[stream]:
                raise JournalError('fixed restore output exceeded its bounds')
            counts[stream] += length
            physical_backup._read_exact(response, length)
        outcome['output_bytes'] = counts
    except BaseException as exc:
        outcome['error'] = exc
    finally:
        connection.close()
        outcome['connection'] = None


def _app_container(flow):
    container = flow.postgres.containers['app']
    state = flow.postgres._inspect(container['id'], container['config'], initial_network=False).get('State', {})
    if (state.get('Running') is not True or state.get('Pid') != container['pid'] or
            select.select([container['pidfd']], [], [], 0)[0] or process_start(container['pid']) != container['starttime']):
        raise JournalError('original APP PostgreSQL process changed')
    return {'container_id': container['id'], 'pid': container['pid'], 'starttime': container['starttime']}


def _empty_app_sessions(flow, host):
    query = ("SELECT json_build_object('sessions',(SELECT count(*) FROM pg_stat_activity WHERE "
             "backend_type='client backend' AND datname=current_database() AND pid<>pg_backend_pid()),"
             "'prepared',(SELECT count(*) FROM pg_prepared_xacts))::text AS state\0")
    result = pg_bootstrap._run('app', flow.postgres.passwords['app'], 'records', query.encode('ascii'), host)
    if result != {'sessions': 0, 'prepared': 0}:
        raise JournalError('APP restore requires all prior backends and prepared transactions settled')


def restore(flow, prepared, handoff):
    _require_context(flow, 'retired')
    if (type(prepared) is not dict or type(handoff) is not dict or prepared.get('app_host') != flow.life.app_endpoint['app_host'] or
            handoff.get('source_handoff', {}).get('state') != 'handoff_installed'):
        raise JournalError('APP restore lacks its original same-APP handoff')
    host = prepared['app_host']
    current = _observe_app(flow, host)
    if (not current['source_binding_table'] or not any(row['name'] == '0081_record_deletion_successor.sql'
                                                        for row in current['migrations']) or
            not any(row.get('operation_id') == flow.source_operation_id for row in current['bindings'])):
        raise JournalError('C81 source binding was not actually installed before restore')
    external_before = _external(flow)
    expected = prepared['backup']['producer']['artifact']
    if expected['path'] != 'published/postgres_dump/db.v1':
        raise JournalError('restore archive is not the actual published APP dump')
    root = flow.roots._handles[('managed-backups', 'published')]
    root_identity = os.fstat(root)
    if ((root_identity.st_dev, root_identity.st_ino, _mount_id(root)) !=
            flow.roots._identities[('managed-backups', 'published')]):
        raise JournalError('published backup root identity changed')
    backup_dir = os.open('postgres_dump', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
    try:
        directory = os.fstat(backup_dir)
        if (directory.st_uid != 0 or directory.st_gid != 0 or stat.S_IMODE(directory.st_mode) != 0o700 or
                directory.st_dev != root_identity.st_dev or _mount_id(backup_dir) != _mount_id(root)):
            raise JournalError('published backup directory identity differs')
        archive = os.open('db.v1', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=backup_dir)
    finally:
        os.close(backup_dir)
    pgdata = fifo_fd = pidfd = fifo_identity = None
    exec_id = None
    collector = None
    outcome = {}
    try:
        info = os.fstat(archive)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                stat.S_IMODE(info.st_mode) != 0o600 or info.st_size != expected['size'] or
                not 0 < info.st_size <= physical_backup.MAX_DUMP):
            raise JournalError('published APP archive identity differs')
        archive_identity = (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        digest = hashlib.sha256()
        while True:
            data = os.read(archive, 1024 * 1024)
            if not data:
                break
            digest.update(data)
        if digest.hexdigest() != expected['sha256'] or os.lseek(archive, 0, os.SEEK_SET) != 0:
            raise JournalError('published APP archive bytes differ')
        _empty_app_sessions(flow, host)
        container_identity = _app_container(flow)
        pgdata = os.open('/var/lib/g2/app/pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        directory = os.fstat(pgdata)
        volume = flow.life.volumes['app']
        if ((directory.st_dev, directory.st_ino, _mount_id(pgdata)) !=
                (volume['pgdata_dev'], volume['pgdata_ino'], volume['pgdata_mount_id']) or
                (directory.st_uid, directory.st_gid) != (flow.postgres.uid, flow.postgres.gid)):
            raise JournalError('original APP PGDATA changed before restore')
        os.mkfifo(FIFO, 0o600, dir_fd=pgdata)
        created = os.stat(FIFO, dir_fd=pgdata, follow_symlinks=False)
        fifo_identity = (created.st_dev, created.st_ino)
        os.chown(FIFO, flow.postgres.uid, flow.postgres.gid, dir_fd=pgdata, follow_symlinks=False)
        fifo_fd = os.open(FIFO, os.O_RDWR | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=pgdata)
        os.fchmod(fifo_fd, 0o600)
        fifo = os.fstat(fifo_fd)
        if (not stat.S_ISFIFO(fifo.st_mode) or (fifo.st_dev, fifo.st_ino) != fifo_identity or
                (fifo.st_uid, fifo.st_gid) != (flow.postgres.uid, flow.postgres.gid) or
                stat.S_IMODE(fifo.st_mode) != 0o600):
            raise JournalError('restore FIFO identity differs')
        container = flow.postgres.containers['app']
        deadline = time.monotonic() + 120
        with flow.postgres.bpf.exec_observation(container['id'], 'app_rollback_restore', deadline) as track:
            _reset_schemas(flow, host)
            argv = [RESTORE, '--host=' + host, '--port=5432', '--username=postgres', '--dbname=houfeng',
                    '--clean', '--if-exists', '--exit-on-error', '--single-transaction', '/var/lib/postgresql/data/' + FIFO]
            action = {'container_id': container['id'], 'argv': argv, 'archive_sha256': expected['sha256'],
                      'archive_size': expected['size'], 'database': 'houfeng'}
            flow.life._admit('negative-pg-restore', action)
            create = {'AttachStdout': True, 'AttachStderr': True, 'Tty': False,
                      'User': '%d:%d' % (flow.postgres.uid, flow.postgres.gid),
                      'Env': ['PGPASSWORD=' + flow.postgres.passwords['app']], 'Cmd': argv}
            response = flow.postgres._request('POST', '/containers/' + container['id'] + '/exec', create)
            exec_id = response.get('Id') if type(response) is dict else None
            if type(exec_id) is not str or re.fullmatch(r'[0-9a-f]{64}', exec_id) is None:
                raise JournalError('APP restore exec identity is missing')
            track(exec_id)
            collector = threading.Thread(target=_collect_restore, args=(flow.postgres, exec_id, outcome), daemon=True)
            collector.start()
            identity = None
            while time.monotonic() < deadline:
                if 'error' in outcome:
                    raise JournalError('APP restore failed before observation')
                state = flow.postgres._request('GET', '/exec/' + exec_id + '/json')
                pid = state.get('Pid') if type(state) is dict and state.get('Running') is True else None
                if type(pid) is int and pid > 0:
                    candidate = _identity(pid, full=True)
                    if candidate and candidate['exe'] == RESTORE:
                        identity = candidate
                        break
                threading.Event().wait(0.02)
            if identity is None:
                raise JournalError('actual pg_restore process was not held at the FIFO')
            pidfd = os.pidfd_open(identity['pid'], 0)
            status = identity['status']
            if (select.select([pidfd], [], [], 0)[0] or process_start(identity['pid']) != identity['starttime'] or
                    not _cgroup_has_container(identity['cgroup'], container['id']) or
                    identity['argv'] != [os.fsencode(value) for value in argv] or
                    status.get(b'Uid', '').split() != [str(flow.postgres.uid)] * 4 or
                    status.get(b'Gid', '').split() != [str(flow.postgres.gid)] * 4 or status.get(b'NoNewPrivs') != '1'):
                raise JournalError('actual pg_restore process or container differs')
            observed = {'exec_id': exec_id, 'container_id': container['id'], 'pid': identity['pid'],
                        'starttime': identity['starttime'], 'executable_sha256': identity['exe_sha256'],
                        'archive_sha256': expected['sha256'], 'archive_size': expected['size']}
            flow.life._register('negative-pg-restore', observed)
            digest, size = hashlib.sha256(), 0
            while True:
                block = os.read(archive, 65536)
                if not block:
                    break
                digest.update(block)
                size += len(block)
                view = memoryview(block)
                while view:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or select.select([pidfd], [], [], 0)[0]:
                        raise JournalError('APP restore ended before the archive was delivered')
                    if not select.select([], [fifo_fd], [], min(remaining, 1))[1]:
                        continue
                    try:
                        count = os.write(fifo_fd, view)
                    except BlockingIOError:
                        continue
                    if count <= 0:
                        raise JournalError('APP restore FIFO write failed')
                    view = view[count:]
            final_archive = os.fstat(archive)
            if (size != expected['size'] or digest.hexdigest() != expected['sha256'] or
                    (final_archive.st_dev, final_archive.st_ino, final_archive.st_size,
                     final_archive.st_mtime_ns, final_archive.st_ctime_ns) != archive_identity):
                raise JournalError('APP restore stream did not equal the published archive')
            os.close(fifo_fd)
            fifo_fd = None
            collector.join(max(0, deadline - time.monotonic()))
            if collector.is_alive() or 'error' in outcome or outcome.get('output_bytes') != {1: 0, 2: 0}:
                raise JournalError('actual APP restore output did not complete cleanly')
            state = flow.postgres._request('GET', '/exec/' + exec_id + '/json')
            if state.get('Running') is not False or state.get('ExitCode') != 0 or not select.select([pidfd], [], [], 0)[0]:
                raise JournalError('actual APP restore exit was not observed')
            current_pgdata = os.fstat(pgdata)
            if (_app_container(flow) != container_identity or
                    (current_pgdata.st_dev, current_pgdata.st_ino, _mount_id(pgdata)) !=
                    (volume['pgdata_dev'], volume['pgdata_ino'], volume['pgdata_mount_id'])):
                raise JournalError('APP container or volume changed during restore')
            observed.update(pidfd_exited=True, exec_exit_code=0, archive_delivered=True,
                            app_container=container_identity)
            flow.life.journal.append('registration', {'name': 'negative-pg-restore-complete',
                                                       'identity_digest': hashlib.sha256(canonical(observed)).hexdigest()})
        after = _observe_app(flow, host)
        restored_records = pg_bootstrap.observe_records(flow.postgres.passwords['app'], host, flow.pair['a'], flow.pair['b'])
        external_after = _external(flow)
        if after != prepared['app_before'] or restored_records != prepared['records_before'] or external_after != external_before:
            raise JournalError('restored APP or retained external evidence differs')
        return {'app_before': prepared['app_before'], 'app_newer': current, 'app_restored': after,
                'records_before': prepared['records_before'], 'records_restored': restored_records,
                'external_before': external_before, 'external_after': external_after, 'restore': observed}
    finally:
        if fifo_fd is not None:
            os.close(fifo_fd)
        if pidfd is not None:
            if not select.select([pidfd], [], [], 0)[0]:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                select.select([pidfd], [], [], 10)
            os.close(pidfd)
        if collector is not None and collector.is_alive():
            connection = outcome.get('connection')
            if connection is not None:
                connection.close()
            collector.join(5)
        if pgdata is not None and fifo_identity is not None:
            try:
                current_fifo = os.stat(FIFO, dir_fd=pgdata, follow_symlinks=False)
                if not stat.S_ISFIFO(current_fifo.st_mode) or (current_fifo.st_dev, current_fifo.st_ino) != fifo_identity:
                    raise JournalError('restore FIFO was replaced before cleanup')
                os.unlink(FIFO, dir_fd=pgdata)
                os.fsync(pgdata)
            except FileNotFoundError:
                pass
        if pgdata is not None:
            os.close(pgdata)
        os.close(archive)


def probe_restored(flow, prepared, restored):
    _require_context(flow, 'retired')
    host = prepared['app_host']
    before_app, before_external = _observe_app(flow, host), _external(flow)
    before_records = pg_bootstrap.observe_records(flow.postgres.passwords['app'], host, flow.pair['a'], flow.pair['b'])
    if (before_app != restored['app_restored'] or before_records != restored['records_restored'] or
            before_external != restored['external_after'] or before_app['source_binding_table']):
        raise JournalError('restored APP is not the original older predecessor')
    request = dict(flow._base(), original_request_file='/var/lib/g2/control/maintenance/requests/source-original.json')
    path = _private_request('source-reconcile-after-rollback.json', request)
    flow.action_order.append('legacy-retirement-reconcile')
    result = flow.invoker.admin('legacy-retirement-reconcile', path, None, time.monotonic() + 120,
                                recovery_id=flow.recovery_id,
                                endpoint_container_id=flow.postgres.containers['app']['id'])
    after_app, after_external = _observe_app(flow, host), _external(flow)
    after_records = pg_bootstrap.observe_records(flow.postgres.passwords['app'], host, flow.pair['a'], flow.pair['b'])
    if (result['status'] != 1 or result['stdout'] or before_app != after_app or before_external != after_external or
            before_records != after_records or not flow.life.invalid or not flow.life.closed_maintenance):
        raise JournalError('actual source handoff did not reject the restored older APP')
    return {'exit_status': result['status'], 'stdout_bytes': len(result['stdout']),
            'coordinator': {'pid': result['pid'], 'starttime': result['starttime']},
            'app_before': before_app, 'app_after': after_app, 'records_before': before_records,
            'records_after': after_records, 'external_before': before_external, 'external_after': after_external}
