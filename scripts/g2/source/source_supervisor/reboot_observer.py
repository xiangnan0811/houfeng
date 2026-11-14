"""Prearmed, one-shot read-only observer for a killed disposable VM."""
import hashlib
import os
import re
import select
import signal
import stat
import subprocess
import sys
import time

import bpf_authority
import source_protocol
import guest_storage
import pg_bootstrap
from census import inventory_digest
from contract import REALM_CONTROL, RUN_TOKEN
from journal import EVENT_KEYS, JournalError, _directory, _read, _read_genesis, _write_new, boot_id, canonical, process_start, strict_json
from phase import open_control_dir, read_phases, read_private

SEED = '/mnt/g2-seed'
CIDATA = '/mnt/g2-cidata'
NAMES = ('app', 'w1', 'w2', 'control')
SERIALS = ('G2APP', 'G2W1', 'G2W2', 'G2CONTROL')
GUEST_BPF_DEADLINE_MONO = 3300.0


def _app_identity(password, host, application, deployment):
    sql = ("SELECT json_build_object("
           "'domain',(SELECT json_build_object('id',domain_id,'kind',domain_kind,'epoch',identity_epoch,"
           "'mode',identity_mode,'system_identifier',postgres_system_identifier,'database_oid',database_oid,"
           "'database_name',database_name) FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
           "'domain_count',(SELECT count(*) FROM public.record_platform_domain_identity),"
           "'deployment',(SELECT deployment_id FROM public.deployment_contract_state WHERE project_id='default'),"
           "'live_system',(SELECT system_identifier::text FROM pg_control_system()),"
           "'live_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),"
           "'live_name',current_database())::text AS state\0")
    value = pg_bootstrap._run('app', password, 'records', query_override=sql.encode('ascii'), host_override=host)
    domain = {'id': application['ID'], 'kind': 'application', 'epoch': application['Epoch'],
              'mode': 'postgres_system', 'system_identifier': application['SystemIdentifier'],
              'database_oid': application['DatabaseOID'], 'database_name': application['DatabaseName']}
    expected = {'domain': domain, 'domain_count': 1, 'deployment': deployment,
                'live_system': domain['system_identifier'], 'live_oid': domain['database_oid'],
                'live_name': domain['database_name']}
    if value != expected:
        raise JournalError('reboot APP identity differs')
    return value


def _projection(password, host, application, deployment, source):
    expected = {'vps': r'vps_[0-9a-f]{16}', 'monitoring_instance': r'mi_[0-9a-f]{16}',
                'target': r'tg_[0-9a-f]{16}'}
    kind, source_id = source['kind'], source['id']
    if source.get('project_id') != 'default' or kind not in expected or re.fullmatch(expected[kind], source_id) is None:
        raise JournalError('reboot source identity invalid')
    sql = ("SELECT json_build_object("
           "'domain',(SELECT json_build_object('id',domain_id,'kind',domain_kind,'epoch',identity_epoch,"
           "'mode',identity_mode,'system_identifier',postgres_system_identifier,'database_oid',database_oid,"
           "'database_name',database_name) FROM public.record_platform_domain_identity WHERE domain_kind='application'),"
           "'domain_count',(SELECT count(*) FROM public.record_platform_domain_identity),"
           "'deployment',(SELECT deployment_id FROM public.deployment_contract_state WHERE project_id='default'),"
           "'live_system',(SELECT system_identifier::text FROM pg_control_system()),"
           "'live_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),"
           "'live_name',current_database(),"
           "'tombstone',(SELECT json_build_object('project_id',project_id,'kind',source_kind,'id',source_id,"
           "'floor',encode(authorization_floor_digest,'hex'),'deleted_us',floor(extract(epoch FROM deleted_at)*1000000)::text) "
           "FROM public.source_deletion_tombstones WHERE project_id='default' AND source_kind='%s' AND source_id='%s'),"
           "'tombstone_count',(SELECT count(*) FROM public.source_deletion_tombstones WHERE project_id='default' "
           "AND source_kind='%s' AND source_id='%s'))::text AS state\0") % (kind, source_id, kind, source_id)
    value = pg_bootstrap._run('app', password, 'records', query_override=sql.encode('ascii'), host_override=host)
    domain = {'id': application['ID'], 'kind': 'application', 'epoch': application['Epoch'],
              'mode': 'postgres_system', 'system_identifier': application['SystemIdentifier'],
              'database_oid': application['DatabaseOID'], 'database_name': application['DatabaseName']}
    tombstone = value.get('tombstone') if type(value) is dict else None
    if (type(value) is not dict or value.get('domain') != domain or value.get('domain_count') != 1 or
            value.get('deployment') != deployment or value.get('live_system') != domain['system_identifier'] or
            value.get('live_oid') != domain['database_oid'] or value.get('live_name') != domain['database_name'] or
            value.get('tombstone_count') != 1 or type(tombstone) is not dict or
            tombstone.get('project_id') != 'default' or tombstone.get('kind') != kind or
            tombstone.get('id') != source_id or type(tombstone.get('floor')) is not str or
            re.fullmatch(r'[0-9a-f]{64}', tombstone['floor']) is None or
            type(tombstone.get('deleted_us')) is not str or not tombstone['deleted_us'].isdigit()):
        raise JournalError('original APP identity or source projection differs')
    return value


def _control_snapshot():
    directory = open_control_dir()
    try:
        names = sorted(name for name in os.listdir(directory) if name.startswith(('phase-', 'predecessor-', 'source-')))
        return {name: hashlib.sha256(read_private(directory, name)).hexdigest() for name in names}
    finally:
        os.close(directory)


def _pending_source(snapshot):
    keys = []
    for prefix in ('source-preview-', 'source-intent-', 'source-witnessed-', 'source-anchored-'):
        found = [name[len(prefix):-5] for name in snapshot if name.startswith(prefix) and name.endswith('.json')]
        if len(found) != 1 or re.fullmatch(r'[0-9a-f]{64}', found[0]) is None:
            raise JournalError('original source pending evidence missing')
        keys.append(found[0])
    if len(set(keys)) != 1 or any(name.startswith('source-handoff-') for name in snapshot):
        raise JournalError('source handoff was already installed or differs')


def prearm(flow, source_result, authority, deadline):
    with authority.observation(deadline):
        return _prearm(flow, source_result, authority._checkpoint_tail)


def _prearm(flow, source_result, original_bpf_tail):
    genesis = strict_json(flow.journal._genesis)
    binding = strict_json(flow.journal._app)
    if (genesis.get('case') != 'source-recovery' or genesis.get('scenario') != 'vm-kill' or
            flow.life.invalid or source_result.get('retirement', {}).get('state') != 'witnessed_handoff_pending' or
            source_result['retirement'].get('operation_id') != flow.source_operation_id):
        raise JournalError('VM kill prearm is not the original pending source')
    phase = read_phases(flow.app['application'], flow.recovery_id)
    if phase['phase'] != 'quiescent':
        raise JournalError('VM kill prearm requires quiescent maintenance')
    endpoint = flow.postgres.observe(flow.life.recoveries)
    source = source_result['retirement']['source']
    projection = _projection(flow.postgres.passwords['app'], endpoint['app_host'], flow.app['application'],
                             flow.app['deployment_id'], source)
    records = flow._readback()
    control = _control_snapshot()
    _pending_source(control)
    realm_digest = flow.journal.realm_digest()
    containers = {name: {'id': flow.postgres.containers[name]['id'], 'config': flow.postgres.containers[name]['config']}
                  for name in NAMES}
    intent = {'version': 1, 'run_token': RUN_TOKEN, 'case': 'source-recovery', 'scenario': 'vm-kill',
              'original_boot_id': genesis['boot_id'], 'realm_digest': realm_digest,
              'original_bpf_tail': original_bpf_tail,
              'genesis_sha256': hashlib.sha256(flow.journal._genesis).hexdigest(),
              'app_binding_sha256': hashlib.sha256(flow.journal._app).hexdigest(),
              'application': flow.app['application'], 'deployment_id': flow.app['deployment_id'],
              'source': source, 'source_operation_id': flow.source_operation_id,
              'phase': phase, 'recovery_id': flow.recovery_id, 'control_files': control,
              'projection': projection, 'records': records, 'fixture_records': flow.pair,
              'app_host': endpoint['app_host'], 'containers': containers,
              'postgres_uid': flow.postgres.uid, 'postgres_gid': flow.postgres.gid}
    credential = {'version': 1, 'run_token': RUN_TOKEN, 'case': 'source-recovery', 'scenario': 'vm-kill',
                  'application': flow.app['application'], 'source_operation_id': flow.source_operation_id,
                  'realm_digest': realm_digest, 'app_volume': {'serial': genesis['volumes']['app']['disk']['serial'],
                      'mount_ino': genesis['volumes']['app']['mount_ino'], 'pgdata_ino': genesis['volumes']['app']['pgdata_ino']},
                  'postgres_password': flow.postgres.passwords['app']}
    if not 0 < len(canonical(intent)) <= 16 * 1024:
        raise JournalError('reboot intent exceeds control file limit')
    _write_new(flow.journal._fd, 'reboot-credential.json', credential)
    raw = _write_new(flow.journal._fd, 'reboot-intent.json', intent)
    flow.journal.append('registration', {'name': 'vm-kill-ready', 'identity_digest': hashlib.sha256(raw).hexdigest()})
    flow.journal.verify()
    return {'prearmed': True, 'intent_sha256': hashlib.sha256(raw).hexdigest(),
            'source_operation_id': flow.source_operation_id, 'realm_digest': realm_digest,
            'original_boot_id': genesis['boot_id'], 'supervisor_pid': genesis['supervisor_pid'],
            'supervisor_starttime': genesis['supervisor_starttime'], 'source_handoff_pending': True}


def _mount_seed():
    for target, serial, filesystem, options in (
            (CIDATA, 'G2CIDATA-part1', 'vfat', 'ro,nosuid,nodev,noexec'),
            (SEED, 'G2SEED', 'ext4', 'ro,noload,nosuid,nodev,noexec')):
        entries = [item for item in guest_storage.mounts() if item['target'] == target]
        if not entries:
            if not os.path.isdir(target):
                raise JournalError('fixed seed mountpoint missing')
            guest_storage.command(['/usr/bin/mount', '-t', filesystem, '-o', options,
                                   '/dev/disk/by-id/virtio-' + serial, target])
    return guest_storage.verify_guest()


def _read_root():
    fd = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        _directory(fd)
        raw_genesis, raw_app = _read_genesis(fd), _read(fd, 'realm-app.json')
        raw_intent = _read(fd, 'reboot-intent.json')
        genesis, app, intent = strict_json(raw_genesis), strict_json(raw_app), strict_json(raw_intent)
        credential = strict_json(_read(fd, 'reboot-credential.json'))
        names = sorted(name for name in os.listdir(fd) if re.fullmatch(r'journal-[0-9]{20}\.json', name))
        if not names or names != ['journal-%020d.json' % index for index in range(1, len(names) + 1)]:
            raise JournalError('original root journal sequence missing')
        previous, last = '0' * 64, None
        for index, name in enumerate(names, 1):
            raw = _read(fd, name)
            event = strict_json(raw)
            if (type(event) is not dict or set(event) != {'version', 'sequence', 'previous', 'kind', 'fields'} or
                    type(event['version']) is not int or event['version'] != 1 or event['sequence'] != index or
                    event['previous'] != previous or event['kind'] not in EVENT_KEYS or
                    type(event['fields']) is not dict or frozenset(event['fields']) != EVENT_KEYS[event['kind']] or
                    canonical(event) != raw or event['kind'] == 'invalid'):
                raise JournalError('original root journal chain differs')
            previous, last = hashlib.sha256(raw).hexdigest(), event
        if (last['kind'] != 'registration' or last['fields'] !=
                {'name': 'vm-kill-ready', 'identity_digest': hashlib.sha256(raw_intent).hexdigest()}):
            raise JournalError('original root prearm was not the final journal event')
        realm = inventory_digest('HOUFENG-C80-REALM-V1', raw_genesis, raw_app).hex()
        if (intent.get('case') != 'source-recovery' or intent.get('scenario') != 'vm-kill' or
                type(intent.get('original_bpf_tail')) is not str or
                re.fullmatch(r'[0-9a-f]{64}', intent['original_bpf_tail']) is None or
                intent['original_bpf_tail'] == '0' * 64 or
                intent.get('run_token') != RUN_TOKEN or intent.get('realm_digest') != realm or
                intent.get('genesis_sha256') != hashlib.sha256(raw_genesis).hexdigest() or
                intent.get('app_binding_sha256') != hashlib.sha256(raw_app).hexdigest() or
                intent.get('original_boot_id') != genesis.get('boot_id') or boot_id() == genesis.get('boot_id') or
                intent.get('application') != app.get('application') or
                intent.get('deployment_id') != app.get('deployment_id') or
                type(credential) is not dict or set(credential) != {'version', 'run_token', 'case', 'scenario',
                    'application', 'source_operation_id', 'realm_digest', 'app_volume', 'postgres_password'} or
                type(credential.get('version')) is not int or credential.get('version') != 1 or credential.get('run_token') != RUN_TOKEN or
                credential.get('case') != 'source-recovery' or credential.get('scenario') != 'vm-kill' or
                credential.get('application') != intent['application'] or
                credential.get('source_operation_id') != intent['source_operation_id'] or
                credential.get('realm_digest') != realm or credential.get('app_volume') != {
                    'serial': genesis['volumes']['app']['disk']['serial'], 'mount_ino': genesis['volumes']['app']['mount_ino'],
                    'pgdata_ino': genesis['volumes']['app']['pgdata_ino']} or
                type(credential.get('postgres_password')) is not str or
                re.fullmatch(r'[0-9a-f]{64}', credential['postgres_password']) is None):
            raise JournalError('original reboot authority binding differs')
        return genesis, app, intent, credential['postgres_password'], hashlib.sha256(raw_intent).hexdigest(), previous
    finally:
        os.close(fd)


def _mount_existing(genesis, intent):
    mounted = {}
    for name, serial in zip(NAMES, SERIALS):
        link = '/dev/disk/by-id/virtio-' + serial
        target = os.path.realpath(link)
        if not target.startswith('/dev/') or '/' in target[5:]:
            raise JournalError('reboot device path differs')
        fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
        try:
            info, named = os.fstat(fd), os.stat(link)
            device = '%d:%d' % (os.major(info.st_rdev), os.minor(info.st_rdev))
            sys = '/sys/dev/block/' + device
            if (not stat.S_ISBLK(info.st_mode) or (info.st_dev, info.st_ino, info.st_rdev) !=
                    (named.st_dev, named.st_ino, named.st_rdev) or
                    guest_storage._text(sys + '/serial', 128).strip() != serial or os.path.exists(sys + '/partition') or
                    guest_storage.command(['/usr/sbin/blockdev', '--getsize64', target]).stdout.strip() != str(guest_storage.SIZE).encode() or
                    guest_storage.command(['/usr/sbin/blkid', '-s', 'TYPE', '-o', 'value', target]).stdout.strip() != b'ext4' or
                    guest_storage.command(['/usr/sbin/blkid', '-s', 'LABEL', '-o', 'value', target]).stdout.strip() != serial.encode()):
                raise JournalError('reboot volume serial, size or filesystem differs')
        finally:
            os.close(fd)
        mountpoint = '/var/lib/g2/' + name
        if not os.path.isdir(mountpoint) or any(item['target'] == mountpoint for item in guest_storage.mounts()):
            raise JournalError('reboot volume target already mounted or missing')
        guest_storage.command(['/usr/bin/mount', '-t', 'ext4', '-o', 'rw,nosuid,nodev,noexec', target, mountpoint])
        matches = [item for item in guest_storage.mounts() if item['target'] == mountpoint]
        root, pgdata = os.stat(mountpoint, follow_symlinks=False), os.stat(mountpoint + '/pgdata', follow_symlinks=False)
        expected = genesis['volumes'][name]
        if (len(matches) != 1 or matches[0]['device'] != device or matches[0]['type'] != 'ext4' or
                not {'rw', 'nosuid', 'nodev', 'noexec'} <= matches[0]['options'] or
                root.st_ino != expected['mount_ino'] or pgdata.st_ino != expected['pgdata_ino'] or
                not stat.S_ISDIR(root.st_mode) or root.st_uid != 0 or root.st_gid != 0 or
                stat.S_IMODE(root.st_mode) != (0o711 if name == 'control' else 0o700) or
                not stat.S_ISDIR(pgdata.st_mode) or pgdata.st_uid != intent['postgres_uid'] or
                pgdata.st_gid != intent['postgres_gid'] or stat.S_IMODE(pgdata.st_mode) != 0o700):
            raise JournalError('reboot mounted volume identity differs')
        mounted[name] = {'serial': serial, 'device': device, 'root_ino': root.st_ino, 'pgdata_ino': pgdata.st_ino}
    if len({value['device'] for value in mounted.values()}) != 4:
        raise JournalError('reboot volumes overlap')
    return mounted


def _start_daemon(authority, deadline):
    if not os.path.isdir('/run/g2'):
        os.mkdir('/run/g2', 0o700)
    proc = subprocess.Popen(['/opt/g2/g2-bpf-authority', 'daemon-child'], env=guest_storage.ENV,
                            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                            stderr=subprocess.DEVNULL, close_fds=True)
    pidfd = None
    deadline = min(deadline, time.monotonic() + 60)
    try:
        pidfd = os.pidfd_open(proc.pid, 0)
        start = process_start(proc.pid)
        while True:
            waited, status = os.waitpid(proc.pid, os.WUNTRACED | os.WNOHANG)
            if waited:
                if not os.WIFSTOPPED(status) or os.WSTOPSIG(status) != signal.SIGSTOP:
                    if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                        proc.returncode = os.waitstatus_to_exitcode(status)
                    raise JournalError('reboot Docker helper did not stop')
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0 or select.select([pidfd], [], [], min(0.1, max(0, remaining)))[0]:
                raise JournalError('reboot Docker helper stop unavailable')
        if process_start(proc.pid) != start or select.select([pidfd], [], [], 0)[0]:
            raise JournalError('reboot Docker helper identity changed')
        authority.register_daemon(proc.pid, start, 'reboot', pidfd)
        if process_start(proc.pid) != start or select.select([pidfd], [], [], 0)[0]:
            raise JournalError('reboot Docker helper changed after registration')
        signal.pidfd_send_signal(pidfd, signal.SIGCONT)
        while time.monotonic() < deadline:
            if proc.poll() is not None or select.select([pidfd], [], [], 0)[0]:
                raise JournalError('reboot Docker daemon exited')
            try:
                info = os.stat('/var/run/docker.sock', follow_symlinks=False)
            except FileNotFoundError:
                select.select([pidfd], [], [], min(0.1, deadline - time.monotonic()))
                continue
            if not stat.S_ISSOCK(info.st_mode) or info.st_uid != 0 or info.st_gid != 10002 or info.st_mode & 0o007:
                raise JournalError('reboot Docker socket differs')
            executable = os.stat('/proc/%d/exe' % proc.pid)
            installed = os.stat('/usr/bin/dockerd')
            if ((executable.st_dev, executable.st_ino) != (installed.st_dev, installed.st_ino) or
                    process_start(proc.pid) != start or select.select([pidfd], [], [], 0)[0]):
                raise JournalError('reboot Docker executable differs')
            return proc, pidfd, {'pid': proc.pid, 'starttime': start, 'socket_dev': info.st_dev,
                                 'socket_ino': info.st_ino}
        raise JournalError('reboot Docker daemon unavailable')
    except BaseException:
        if proc.poll() is None:
            if pidfd is None:
                proc.kill()
            else:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
        try:
            proc.wait(timeout=max(0.001, min(10, deadline - time.monotonic())))
        finally:
            if pidfd is not None:
                os.close(pidfd)
        raise


def _exec_controldata_stream(postgres, exec_id, deadline):
    from guest_postgres import _UnixHTTP
    connection = _UnixHTTP(postgres.daemon, max(1, min(60, deadline - time.monotonic())))
    stdout, stderr = bytearray(), bytearray()
    try:
        connection.request('POST', '/v1.41/exec/' + exec_id + '/start', body=b'{"Detach":false,"Tty":false}',
                           headers={'Content-Type': 'application/json'})
        response = connection.getresponse()
        if response.status != 200:
            raise JournalError('reboot pg_controldata exec did not start')
        while True:
            header = response.read(8)
            if not header:
                break
            if len(header) != 8:
                raise JournalError('reboot pg_controldata stream header truncated')
            stream, pad, length = header[0], header[1:4], int.from_bytes(header[4:], 'big')
            if stream not in (1, 2) or pad != b'\0\0\0' or length > 65536:
                raise JournalError('reboot pg_controldata stream frame invalid')
            remaining = length
            while remaining:
                chunk = response.read(remaining)
                if not chunk:
                    raise JournalError('reboot pg_controldata stream truncated')
                remaining -= len(chunk)
                target = stderr if stream == 2 else stdout
                target.extend(chunk)
                if len(target) > 65536:
                    raise JournalError('reboot pg_controldata stream exceeds limit')
        if stderr:
            raise JournalError('reboot pg_controldata produced error output')
        return bytes(stdout)
    finally:
        connection.close()


def _read_system_ids(postgres, intent, authority, deadline):
    observed = {}
    for name in NAMES:
        item = intent['containers'][name]
        exec_deadline = min(deadline, time.monotonic() + 60)
        with authority.exec_observation(item['id'], 'pg_controldata', exec_deadline) as track:
            create = {'AttachStdout': True, 'AttachStderr': True, 'Tty': False,
                      'User': '%d:%d' % (intent['postgres_uid'], intent['postgres_gid']),
                      'Cmd': ['/usr/lib/postgresql/16/bin/pg_controldata', '/var/lib/postgresql/data']}
            created = postgres._request('POST', '/containers/' + item['id'] + '/exec', create, deadline=exec_deadline)
            exec_id = created.get('Id') if type(created) is dict else None
            if type(exec_id) is not str or re.fullmatch(r'[0-9a-f]{64}', exec_id) is None:
                raise JournalError('reboot pg_controldata exec identity unavailable')
            track(exec_id)
            stdout = _exec_controldata_stream(postgres, exec_id, exec_deadline)
            state = postgres._request('GET', '/exec/' + exec_id + '/json', deadline=exec_deadline)
            if type(state) is not dict or state.get('Running') is not False or state.get('ExitCode') != 0:
                raise JournalError('reboot pg_controldata exec did not exit cleanly')
        matches = re.findall(rb'^Database system identifier:\s*([0-9]+)\s*$', stdout, re.M)
        if len(matches) != 1:
            raise JournalError('reboot PostgreSQL system identity unavailable')
        observed[name] = matches[0].decode('ascii')
    return observed


def _observe(genesis, app, intent, password, authority, deadline):
    import guest_postgres
    with authority.observation(deadline):
        mounted = _mount_existing(genesis, intent)
        snapshot = _control_snapshot()
        _pending_source(snapshot)
        if snapshot != intent['control_files'] or read_phases(intent['application'], intent['recovery_id']) != intent['phase']:
            raise JournalError('original source maintenance records changed on reboot')
    proc, pidfd, daemon = _start_daemon(authority, deadline)
    admitted = []
    instances = {}
    try:
        postgres = guest_postgres.PostgresRealm(daemon, mounted, intent['postgres_uid'], intent['postgres_gid'], None, authority)
        for name in NAMES:
            entry = intent['containers'][name]
            inspected = postgres._inspect(entry['id'], entry['config'], initial_network=False)
            if inspected.get('State', {}).get('Running') is not False:
                raise JournalError('reboot PostgreSQL container was already running')
            authority.admit_container(entry['config'], entry['id'], 'recovery-' + name)
            admitted.append(entry['id'])
            postgres._request('POST', '/containers/' + entry['id'] + '/start')
            inspected = postgres._inspect(entry['id'], entry['config'], initial_network=False)
            if inspected.get('State', {}).get('Running') is not True:
                raise JournalError('original PostgreSQL container did not restart')
            instance_pid = inspected['State'].get('Pid')
            if type(instance_pid) is not int or instance_pid <= 1:
                raise JournalError('reboot PostgreSQL process identity unavailable')
            instance_start = process_start(instance_pid)
            instance_fd = os.pidfd_open(instance_pid, 0)
            instances[entry['id']] = (instance_fd, instance_pid, instance_start)
            if select.select([instance_fd], [], [], 0)[0] or process_start(instance_pid) != instance_start:
                raise JournalError('reboot PostgreSQL process changed')
            if name == 'app':
                networks = inspected.get('NetworkSettings', {}).get('Networks') or {}
                if sum(item.get('IPAddress') == intent['app_host'] for item in networks.values()) != 1:
                    raise JournalError('reboot APP network endpoint changed')
        system_ids = _read_system_ids(postgres, intent, authority, deadline)
        if system_ids != {name: app['physical'][name]['system_identifier'] for name in NAMES}:
            raise JournalError('original PostgreSQL system identities changed on reboot')
        read_deadline = min(deadline, time.monotonic() + 90)
        while True:
            try:
                identity = _app_identity(password, intent['app_host'], intent['application'], intent['deployment_id'])
                break
            except (OSError, JournalError):
                if time.monotonic() >= read_deadline:
                    raise JournalError('original APP readback unavailable after reboot')
                select.select([pidfd], [], [], min(0.2, read_deadline - time.monotonic()))
                if select.select([pidfd], [], [], 0)[0]:
                    raise JournalError('reboot Docker daemon exited during APP readback')
        with authority.observation(deadline):
            if _app_identity(password, intent['app_host'], intent['application'], intent['deployment_id']) != identity:
                raise JournalError('reboot APP identity changed during observation')
            for name in NAMES:
                entry = intent['containers'][name]
                current = postgres._inspect(entry['id'], entry['config'], initial_network=False, deadline=deadline)
                if current.get('State', {}).get('Running') is not True:
                    raise JournalError('reboot PostgreSQL stopped during observation')
                held_fd, held_pid, held_start = instances[entry['id']]
                if (current['State'].get('Pid') != held_pid or select.select([held_fd], [], [], 0)[0] or
                        process_start(held_pid) != held_start):
                    raise JournalError('reboot PostgreSQL process identity changed during observation')
            projection = _projection(password, intent['app_host'], intent['application'],
                                     intent['deployment_id'], intent['source'])
            records = pg_bootstrap.observe_records(password, intent['app_host'],
                                                   intent['fixture_records']['a'], intent['fixture_records']['b'])
            if (projection != intent['projection'] or records != intent['records'] or
                    system_ids != {name: app['physical'][name]['system_identifier'] for name in NAMES} or
                    _control_snapshot() != snapshot or read_phases(intent['application'], intent['recovery_id']) != intent['phase']):
                raise JournalError('original APP or source responsibility changed after reboot')
            return {'mounted': mounted, 'system_identifiers': system_ids, 'application': intent['application'], 'identity': identity,
                    'projection': projection, 'records': records, 'control_files': snapshot,
                    'source_operation_id': intent['source_operation_id'], 'source_handoff_pending': True,
                    'realm_available': False, 'no_copy_reissuable': False}
    finally:
        original_failure = sys.exc_info()[0] is not None
        cleanup_failure = None
        for container_id in reversed(admitted):
            try:
                outcome = 'failed' if original_failure else 'completed'
                config = next(intent['containers'][name]['config'] for name in NAMES
                              if intent['containers'][name]['id'] == container_id)
                state = postgres._inspect(container_id, config, initial_network=False, deadline=deadline).get('State', {})
                if type(state.get('Running')) is not bool:
                    raise JournalError('reboot container state unavailable for settlement')
                settlement = authority.settle_container(container_id, outcome, deadline)
                if state['Running']:
                    if settlement != 'settling':
                        raise JournalError('running reboot container grant already retired')
                    postgres._request('POST', '/containers/' + container_id + '/stop?t=10', deadline=deadline)
                waited = postgres._request('POST', '/containers/' + container_id + '/wait?condition=not-running',
                                           deadline=deadline, expected_status=200)
                if (type(waited) is not dict or not {'StatusCode'} <= set(waited) <= {'StatusCode', 'Error'} or
                        type(waited['StatusCode']) is not int):
                    raise JournalError('reboot container wait unavailable')
                error = waited.get('Error')
                if error is not None and (type(error) is not dict or set(error) - {'Message'} or
                                          type(error.get('Message', '')) is not str or error.get('Message', '') != ''):
                    raise JournalError('reboot container wait reported an error')
                held = instances.get(container_id)
                if held is not None:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not select.select([held[0]], [], [], remaining)[0]:
                        raise JournalError('reboot PostgreSQL process exit not observed')
                    after = postgres._inspect(container_id, config, initial_network=False, deadline=deadline).get('State', {})
                    if (after.get('Running') is not False or after.get('Pid') != 0 or
                            type(after.get('ExitCode')) is not int or after['ExitCode'] != waited['StatusCode']):
                        raise JournalError('reboot PostgreSQL wait and instance differ')
                if settlement == 'settling' and authority.settle_container(container_id, outcome, deadline) != 'retired':
                    raise JournalError('reboot container BPF settlement pending')
            except BaseException as exc:
                if cleanup_failure is None:
                    cleanup_failure = exc
        for held_fd, _, _ in instances.values():
            os.close(held_fd)
        try:
            if proc.poll() is None:
                signal.pidfd_send_signal(pidfd, signal.SIGTERM)
            try:
                proc.wait(timeout=max(0.001, min(20, deadline - time.monotonic())))
            except subprocess.TimeoutExpired:
                signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                proc.wait(timeout=max(0.001, min(10, deadline - time.monotonic())))
        except BaseException as exc:
            if cleanup_failure is None:
                cleanup_failure = exc
        finally:
            os.close(pidfd)
        if cleanup_failure is not None and not original_failure:
            raise cleanup_failure


def _claim_once(intent_sha):
    directory = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        _directory(directory)
        raw = _write_new(directory, 'reboot-observer-used.json',
                         {'version': 1, 'run_token': RUN_TOKEN, 'boot_id': boot_id(), 'intent_sha256': intent_sha})
        return hashlib.sha256(raw).hexdigest()
    finally:
        os.close(directory)


def main():
    carrier = None
    authority = None
    result = {'version': 1, 'run_token': RUN_TOKEN, 'case': 'source-recovery', 'scenario': 'vm-kill',
              'completed': False, 'product_gate': 'NOT_READY', 'permanent_deletion_http': 'CLOSED', 'isolation': {}}
    stage = 'seed'
    try:
        hardware = _mount_seed()
        sys.path.insert(0, SEED)
        import guest as carrier
        import guest_bootstrap
        case, expected = guest_bootstrap.verify_seed(copy_dir=SEED)
        deadline = GUEST_BPF_DEADLINE_MONO
        authority = bpf_authority.BPFAuthority.from_launcher(
            launcher_sha256=expected['g2-bpf-authority'], broker_sha256=expected['g2-bpf-authority'],
            run_token=RUN_TOKEN, deadline=deadline, fd=3)
        if case['case'] != 'source-recovery' or case['scenario'] != 'vm-kill':
            raise JournalError('reboot observer case differs')
        carrier.RESULT = result
        result['isolation']['hardware'] = hardware
        carrier.initialize_result_port()
        installation = {'version': 1, 'guest_sha256': expected['source_guest.py'],
                        'supervisor_manifest_sha256': expected['supervisor-manifest.json'],
                        'seed_manifest_sha256': guest_bootstrap._digest(SEED + '/SHA256SUMS'),
                        'payload_case_sha256': case['payload_case_sha256'],
                        'seed_descriptor_sha256': case['seed_descriptor_sha256']}
        result['isolation']['preparer_install'] = installation
        carrier.emit_installation(installation, case['scenario'], time.monotonic() + 30)
        carrier.emit_protocol('G2LAB-READY ' + RUN_TOKEN, time.monotonic() + 30)
        stage = 'original-binding'
        genesis, app, intent, password, intent_sha, journal_sha = _read_root()
        if (genesis.get('seed_manifest_sha256') != installation['seed_manifest_sha256'] or
                genesis.get('supervisor_manifest_sha256') != installation['supervisor_manifest_sha256'] or
                guest_bootstrap._digest('/opt/g2/realm-supervisor/reboot_observer.py') != expected['reboot_observer.py'] or
                genesis.get('host_launch_sha256') != case['host_launch_sha256'] or
                genesis.get('payload_case_sha256') != case['payload_case_sha256'] or
                genesis.get('seed_descriptor_sha256') != case['seed_descriptor_sha256']):
            raise JournalError('reboot observer or immutable seed differs')
        used_sha = _claim_once(intent_sha)
        control_fd = os.open(REALM_CONTROL, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        seed_fd = case_fd = None
        try:
            seed_fd = os.open(SEED, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            case_fd = os.open('case.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=seed_fd)
            authority.register_recovery(intent_sha, genesis['boot_id'], used_sha, control_fd, seed_fd, case_fd)
        finally:
            if case_fd is not None:
                os.close(case_fd)
            if seed_fd is not None:
                os.close(seed_fd)
            os.close(control_fd)
        with authority.observation(deadline):
            if _read_root() != (genesis, app, intent, password, intent_sha, journal_sha):
                raise JournalError('original reboot root changed during registration')
            result['bpf_bootstrap'] = authority.bootstrap_evidence(case, deadline)
        stage = 'same-volume-readback'
        observed = _observe(genesis, app, intent, password, authority, deadline)
        result['reboot'] = {'version': 1, 'boot_id': boot_id(), 'original_boot_id': genesis['boot_id'],
                            'intent_sha256': intent_sha, 'root_journal_tail_sha256': journal_sha,
                            'realm_digest': intent['realm_digest'], 'observation': observed}
        result['original_bpf_tail'] = intent['original_bpf_tail']
        result['bpf_authority'] = authority.export_evidence(result['bpf_bootstrap'], deadline)
        result['completed'] = True
    except BaseException as exc:
        result['failure'] = {'stage': stage, 'error_type': type(exc).__name__}
    finally:
        try:
            if carrier is not None and carrier.RESULT_PORT is not None:
                source_protocol.emit(carrier)
        finally:
            if authority is not None:
                authority.close()
    return 0


if __name__ == '__main__':
    sys.exit(main())
