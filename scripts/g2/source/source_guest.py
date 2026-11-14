#!/usr/bin/env python3
"""Root supervisor for one offline C80 inventory-realm VM."""
import base64
import errno
import fcntl
import hashlib
import os
import re
import secrets
import select
import signal
import stat
import struct
import sys
import tempfile
import time

import app_rollback
import source_protocol
import audit
import bpf_authority
import callback_lifecycle
import browser_process
import census
import ceremony
import comparison_fixture
import dns
from contract import RUN_TOKEN, NEGATIVES, RECOVERY, POSTGRES_HOSTS
import genesis
import guest as carrier
import guest_bootstrap
import guest_postgres
import guest_storage
import invoke
import journal
import lifecycle
import listener_negative
import origins
import pg_bootstrap
import physical_backup
import reboot_observer
import restart
import systemd_realm
import wrong_rpc
from journal import JournalError, canonical, strict_json

CREDENTIAL_NAMES = ('w1_reader', 'w1_writer', 'w2_record_reader', 'w2_record_writer',
                    'w2_source_reader', 'w2_source_producer', 'w2_source_provisioner')
GUEST_BPF_DEADLINE_MONO = 3300.0


def _private_file(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if (not stat.S_ISREG(info.st_mode) or (info.st_uid, info.st_gid) != (10002, 10002) or
                stat.S_IMODE(info.st_mode) not in (0o400, 0o600) or info.st_nlink != 1 or
                not 0 < info.st_size <= 16384):
            raise JournalError('private control file identity invalid')
        body = os.read(fd, info.st_size + 1)
        if len(body) != info.st_size:
            raise JournalError('private control file changed')
        return body
    finally:
        os.close(fd)


def _verify_control(domains, credentials):
    raw = _private_file('/var/lib/g2/control/maintenance/source-authority.json')
    value = strict_json(raw)
    expected = {'guard_directory', 'guard_identity', 'witness', 'reader_database_url', 'birth_installed',
                'birth_lineage_id', 'birth_protocol_version', 'birth_reader_database_url'}
    w2 = domains['w2']
    witness = {key: w2[key] for key in ('ID', 'SystemIdentifier', 'DatabaseOID', 'DatabaseName', 'Epoch')}
    if (type(value) is not dict or set(value) != expected or
            value['guard_directory'] != '/var/lib/g2/control/maintenance/guard' or value['witness'] != witness or
            value['reader_database_url'] != ceremony._url('w2', 'g2_w2_source_reader', credentials['w2_source_reader']) or
            value['birth_installed'] is not False or value['birth_lineage_id'] != '' or
            value['birth_protocol_version'] != 0 or value['birth_reader_database_url'] != ''):
        raise JournalError('source maintenance configuration differs')
    identity = value['guard_identity']
    if (type(identity) is not dict or set(identity) != {'directory_device', 'directory_inode', 'file_device', 'file_inode', 'nonce'} or
            type(identity['nonce']) is not list or len(identity['nonce']) != 32 or
            any(type(item) is not int or not 0 <= item <= 255 for item in identity['nonce']) or
            not any(identity['nonce'])):
        raise JournalError('source maintenance guard identity malformed')
    directory = os.open(value['guard_directory'], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        d = os.fstat(directory)
        fd = os.open('maintenance.lock', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
        try:
            f = os.fstat(fd)
            if ((d.st_dev, d.st_ino, f.st_dev, f.st_ino) !=
                    (identity['directory_device'], identity['directory_inode'], identity['file_device'], identity['file_inode']) or
                    (d.st_uid, d.st_gid, stat.S_IMODE(d.st_mode)) != (10002, 10002, 0o700) or
                    (f.st_uid, f.st_gid, stat.S_IMODE(f.st_mode), f.st_nlink) != (10002, 10002, 0o600, 1) or
                    os.read(fd, 33) != bytes(identity['nonce'])):
                raise JournalError('source maintenance guard differs')
        finally:
            os.close(fd)
    finally:
        os.close(directory)
    return {'config_sha256': hashlib.sha256(raw).hexdigest(), 'guard_device': identity['file_device'],
            'guard_inode': identity['file_inode']}


def _output(result):
    if result['status'] != 0 or result['stderr'] or not result['stdout']:
        raise JournalError('fixed initializer failed')
    value = strict_json(result['stdout'])
    if type(value) is not dict:
        raise JournalError('initializer output is invalid')
    return value


def _init_domains(life, runner, postgres):
    used = set(postgres.passwords.values())
    credentials = {}
    for name in CREDENTIAL_NAMES:
        value = secrets.token_hex(32)
        while value in used:
            value = secrets.token_hex(32)
        used.add(value)
        credentials[name] = value
    value = {'version': 1, 'w1_bootstrap_url': ceremony._url('w1', 'postgres', postgres.passwords['w1']),
             'w2_bootstrap_url': ceremony._url('w2', 'postgres', postgres.passwords['w2']),
             'control_bootstrap_url': ceremony._url('control', 'postgres', postgres.passwords['control']),
             'credentials': credentials}
    fd, digest = invoke.sealed_json(value)
    try:
        result = _output(runner.domain_initializer(fd, digest, time.monotonic() + 900))
    finally:
        os.close(fd)
    if (set(result) != {'version', 'domains'} or type(result['version']) is not int or result['version'] != 1 or
            type(result['domains']) is not dict or set(result['domains']) != {'w1', 'w2', 'control'}):
        raise JournalError('domain initializer output shape differs')
    observed = {}
    for name in ('w1', 'w2', 'control'):
        observed[name] = pg_bootstrap.observe_identity(name, postgres.passwords[name])
    for name, kind in (('w1', 'deletion_ledger'), ('w2', 'deletion_witness')):
        item = result['domains'][name]
        if (type(item) is not dict or set(item) != {'ID', 'Kind', 'SystemIdentifier', 'DatabaseOID', 'DatabaseName', 'Epoch'} or
                item['Kind'] != kind or item['SystemIdentifier'] != observed[name]['system_identifier'] or
                item['DatabaseOID'] != observed[name]['database_oid'] or item['DatabaseName'] != observed[name]['database_name'] or
                type(item['Epoch']) is not int or item['Epoch'] != 1 or type(item['ID']) is not str or not item['ID']):
            raise JournalError('domain physical readback differs')
    control = result['domains']['control']
    if (type(control) is not dict or set(control) != {'ID', 'Kind', 'IdentityEpoch', 'Mode', 'PostgresSystemIdentifier',
                                                       'DatabaseOID', 'DatabaseName'} or control['Kind'] != 'recovery_control' or
            control['Mode'] != 'postgres_system' or control['IdentityEpoch'] != 1 or
            control['PostgresSystemIdentifier'] != observed['control']['system_identifier'] or
            control['DatabaseOID'] != observed['control']['database_oid'] or
            control['DatabaseName'] != observed['control']['database_name'] or not control['ID']):
        raise JournalError('control physical readback differs')
    return result['domains'], credentials, observed, _verify_control(result['domains'], credentials)


def _init_app(runner, postgres, case, intersecting=False):
    used = set(postgres.passwords.values())
    credentials = {}
    for key in ('runtime_password', 'admin_password', 'migrator_password'):
        value = secrets.token_hex(32)
        while value in used:
            value = secrets.token_hex(32)
        used.add(value)
        credentials[key] = value
    config = dict(version=1, case=case, app_bootstrap_url=ceremony._url('app', 'postgres', postgres.passwords['app']), **credentials)
    if intersecting:
        if case != 'source-vps':
            raise JournalError('overlapping fixture outside its authorized case')
        config['fixture_variant'] = 'intersecting-source-closure'
    fd, digest = invoke.sealed_json(config)
    try:
        result = _output(runner.app_initializer('initialize', fd, digest, time.monotonic() + 900))
    finally:
        os.close(fd)
    if (set(result) != {'version', 'case', 'application', 'deployment_id', 'actor_user_id', 'group_id',
                        'runtime_role', 'endpoint', 'records', 'membership_expires_at'} or result['version'] != 1 or
            result['case'] != case or result['runtime_role'] != 'houfeng_runtime' or
            result['endpoint'] != {'host': '172.30.80.2', 'port': 5432, 'database': 'houfeng'} or
            type(result['deployment_id']) is not str or not result['deployment_id'] or
            type(result['actor_user_id']) is not str or not result['actor_user_id']):
        raise JournalError('APP initializer output differs')
    physical = pg_bootstrap.observe_identity('app', postgres.passwords['app'])
    app = result['application']
    if (app['SystemIdentifier'] != physical['system_identifier'] or app['DatabaseOID'] != physical['database_oid'] or
            app['DatabaseName'] != physical['database_name']):
        raise JournalError('APP physical readback differs')
    ceremony._source_fixture(result, intersecting)
    return result, credentials, physical


def _export(journal_owner, authority, deadline):
    with authority.observation(deadline):
        journal_owner.verify()
        names = sorted(os.listdir(journal_owner._fd))
        allowed = [name for name in names if (name.startswith(('journal-', 'realm-', 'launch-authority-',
                                                                'heartbeat-', 'fault-', 'callback-live-', 'callback-join-')) and name.endswith('.json'))
                   or name == 'reboot-intent.json']
        return {name: strict_json(journal._read_genesis(journal_owner._fd) if name == 'realm-genesis.json'
                                  else journal._read(journal_owner._fd, name)) for name in allowed}


def _seed_realms(life, roots, journal_owner, postgres, expected, fixture_case, intersecting=False, realm_negative=False):
    for name in guest_postgres.NAMES:
        action = {'database': pg_bootstrap.DATABASES[name], 'cluster': name}
        life._admit('create-database-' + name, action)
        outcome = pg_bootstrap.create_database(name, postgres.passwords[name])
        life._register('create-database-' + name, outcome)
    physical = {name: pg_bootstrap.observe_identity(name, postgres.passwords[name]) for name in guest_postgres.NAMES}
    if len({entry['system_identifier'] for entry in physical.values()}) != 4:
        raise JournalError('PostgreSQL physical domains overlap')
    runner = invoke.FixedInvoker(life, expected, fixture_case, realm_negative=realm_negative)
    domains, credentials, domain_physical, guard = _init_domains(life, runner, postgres)
    app, app_secrets, app_physical = _init_app(runner, postgres, fixture_case, intersecting)
    if app_physical != physical['app'] or any(domain_physical[name] != physical[name] for name in domain_physical):
        raise JournalError('physical identity changed during initialization')
    binding = {'version': 1, 'run_token': RUN_TOKEN, 'application': app['application'],
               'deployment_id': app['deployment_id'], 'project_id': 'default', 'domains': domains,
               'physical': physical, 'guard': guard, 'roots_digest': roots.scan()}
    realm_digest = journal_owner.bind_application(binding)
    life.transition('seeded')
    life.transition('source_maintenance')
    return runner, domains, credentials, app, app_secrets, binding, realm_digest



def _contaminate_root(life, scenario):
    if scenario not in ('directory-hidden', 'directory-symlink', 'directory-unreadable'):
        raise JournalError('unknown directory negative')
    digest = life.roots.scan()
    fd = life.roots._handles[('browser-profiles',)]
    name = '.census-negative'
    if scenario == 'directory-hidden':
        item = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=fd)
        try:
            os.fsync(item)
        finally:
            os.close(item)
    elif scenario == 'directory-symlink':
        os.symlink('/etc', name, dir_fd=fd)
    else:
        os.fchmod(fd, 0)
    os.fsync(fd)
    try:
        life.roots.scan()
    except census.CensusError:
        pass
    else:
        raise JournalError('contaminated inventory root was accepted')
    if scenario == 'directory-unreadable':
        os.fchmod(fd, 0o700)
    else:
        os.unlink(name, dir_fd=fd)
    os.fsync(fd)
    if os.listdir(fd) or stat.S_IMODE(os.fstat(fd).st_mode) != 0o700:
        raise JournalError('owned census fixture was not restored')
    try:
        life.roots.scan()
    except census.CensusError:
        life.invalidate('root_contaminated')
        return {'scenario': scenario, 'initial_roots_digest': digest, 'contamination_rejected': True,
                'fixture_removed': True, 'sticky_invalid': True}
    raise JournalError('cleaning contamination revived the realm')


def _replace_root(life):
    path = '/var/lib/g2/browser-profiles'
    before = os.stat(path, follow_symlinks=False)
    os.rename(path, path + '-original')
    os.mkdir(path, 0o700)
    after = os.stat(path, follow_symlinks=False)
    try:
        life.roots.scan()
    except census.CensusError:
        pass
    else:
        raise JournalError('replacement inventory root was accepted')
    os.rmdir(path)
    os.rename(path + '-original', path)
    try:
        life.roots.scan()
    except census.CensusError:
        life.invalidate('root_replaced')
        return {'original_ino': before.st_ino, 'replacement_ino': after.st_ino,
                'replacement_rejected': True, 'sticky_invalid': True}
    raise JournalError('root replacement invalidation did not stick')


def _network_window_smoke():
    result = None
    try:
        result = guest_storage.command(['/usr/bin/python3', '-B', '/opt/g2/realm-supervisor/smoke.py',
                                        '--case', 'network-window'], timeout=30,
                                       allowed=tuple(range(-64, 256)))
        if result.returncode != 0 or result.stdout != b'PASS network-window\n' or result.stderr:
            raise JournalError('network window smoke failed')
    except BaseException as error:
        # Private, bounded first-failure evidence; never substitute it for success.
        try:
            failure = result if result is not None else error.__cause__
            stdout = getattr(failure, 'stdout', None) or b''
            stderr = getattr(failure, 'stderr', None) or b''
            if len(stdout) <= 65536 and len(stderr) <= 65536:
                fd = os.open('/var/lib/g2', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                try:
                    info = os.fstat(fd)
                    if info.st_uid == info.st_gid == 0 and info.st_mode & 0o022 == 0:
                        journal._write_new(fd, 'network-window-failure.json', {
                            'version': 1, 'operation': 'network-window',
                            'error_type': type(error).__name__,
                            'returncode': getattr(failure, 'returncode', None),
                            'stdout_base64': base64.b64encode(stdout).decode('ascii'),
                            'stderr_base64': base64.b64encode(stderr).decode('ascii')})
                finally:
                    os.close(fd)
        except BaseException:
            pass
        raise

def _authorization_fd_stage(postgres, life, audit_owner, authority, name, object_class, deadline):
    """Exercise one fixed init-container grant with the original broker FD stage."""
    if (name, object_class) not in (('app', 'map'), ('w1', 'program')):
        raise JournalError('BPF FD stage fixture differs')
    stage = {'class': object_class, 'fixture': name, 'container_id': None, 'grant_id': None,
             'nonce': None, 'armed': False, 'start_http_status': None, 'start_body_base64': '',
             'container_state': None, 'container_removed': False, 'grant_state': None,
             'stage_records': [], 'checkpoint': None}
    carrier.RESULT.setdefault('bpf_fd_stages', []).append(stage)
    directory = os.open('/var/lib/g2/' + name + '/pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info, volume = os.fstat(directory), postgres.volumes[name]
        if ((info.st_dev, info.st_ino, census._mount_id(directory)) !=
                (volume['pgdata_dev'], volume['pgdata_ino'], volume['pgdata_mount_id'])):
            raise JournalError('BPF FD fixture PGDATA is not the registered directory')
        for entry in os.listdir(directory):
            p = '/var/lib/g2/' + name + '/pgdata/' + entry
            if os.path.isdir(p) and not os.path.islink(p):
                import shutil
                shutil.rmtree(p)
            else:
                os.unlink(p)
        os.fchown(directory, postgres.uid, postgres.gid)
        os.fchmod(directory, 0o700)
        os.fsync(directory)
    finally:
        os.close(directory)
    secret = '/run/g2/pg-secret-' + name
    secret_fd = os.open(secret, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o400)
    try:
        os.fchown(secret_fd, postgres.uid, postgres.gid)
        os.fchmod(secret_fd, 0o400)
        secret_bytes = (secrets.token_hex(32) + '\n').encode('ascii')
        if os.write(secret_fd, secret_bytes) != len(secret_bytes):
            raise JournalError('BPF FD fixture secret write was short')
        os.fsync(secret_fd)
    finally:
        os.close(secret_fd)
    try:
        label = 'g2-init-' + name
        config = postgres._config(name, True, secret)
        life.expect_postgres_container(label, name, True, config)
        created = postgres._request('POST', '/containers/create?name=' + label, config, deadline=deadline)
        container_id = created.get('Id') if type(created) is dict else None
        if type(container_id) is not str or re.fullmatch(r'[0-9a-f]{64}', container_id) is None:
            raise JournalError('BPF FD fixture container identity invalid')
        stage['container_id'] = container_id
        postgres._inspect(container_id, config, deadline=deadline)
        life.bind_created_container(label, container_id)
        grant_id = authority.admit_container(config, container_id, 'postgres-init-' + name)
        stage['grant_id'] = grant_id
        nonce = secrets.token_hex(16)
        stage['nonce'] = nonce
        authority.selftest_fd_arm(container_id, object_class, nonce, deadline)
        stage['armed'] = True
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise JournalError('BPF FD stage start deadline expired')
        connection = guest_postgres._UnixHTTP(postgres.daemon, min(60, remaining))
        try:
            connection.request('POST', '/v1.41/containers/' + container_id + '/start')
            response = connection.getresponse()
            stage['start_http_status'] = response.status
            raw = response.read(16385)
            stage['start_body_base64'] = base64.b64encode(raw[:16384]).decode('ascii')
            if len(raw) > 16384 or not (response.status == 204 or 400 <= response.status <= 599):
                raise JournalError('BPF FD fixture did not report a bounded start outcome')
        finally:
            connection.close()
        item = postgres._inspect(container_id, config, deadline=deadline)
        state = item.get('State') if type(item) is dict else None
        if type(state) is not dict:
            raise JournalError('BPF FD fixture container state unavailable')
        if stage['start_http_status'] == 204:
            if object_class != 'program':
                raise JournalError('BPF map FD fixture unexpectedly started')
            wait_deadline = min(deadline, time.monotonic() + 60)
            while state.get('Running') is True and time.monotonic() < wait_deadline:
                select.select([], [], [], 0.05)
                item = postgres._inspect(container_id, config, deadline=deadline)
                state = item.get('State') if type(item) is dict else {}
            if state.get('Running') is not False or state.get('Pid') != 0 or state.get('ExitCode') != 0:
                raise JournalError('BPF program FD fixture retry start did not exit cleanly')
        else:
            if state.get('Running') is not False or type(state.get('Pid')) is not int or state['Pid'] != 0:
                raise JournalError('BPF FD fixture has an unobserved container process')
        stage['container_state'] = {key: state.get(key) for key in ('Running', 'Pid', 'Status', 'ExitCode', 'Error')}
        if (type(stage['container_state']['Status']) is not str or type(stage['container_state']['ExitCode']) is not int or
                type(stage['container_state']['Error']) is not str or len(stage['container_state']['Error']) > 4096):
            raise JournalError('BPF FD fixture state is malformed')
        postgres._request('DELETE', '/containers/' + container_id + '?force=1&v=1', deadline=deadline)
        stage['container_removed'] = True
        settle_outcome = 'completed' if stage['start_http_status'] == 204 else 'failed'
        stage['grant_state'] = authority.settle_container(container_id, settle_outcome, deadline)
        if stage['grant_state'] != 'retired':
            raise JournalError('BPF FD fixture grant remains pending')
        with authority.observation(deadline):
            audit_owner.await_health(deadline)
            records = authority.evidence_records()
            rows = [item for item in records if item['kind'] == 'selftest_fd_stage' and
                    item['body']['grant_id'] == grant_id and item['body']['nonce'] == nonce]
            if len(rows) != 3 or [item['body']['phase'] for item in rows] != ['armed', 'entered', 'restored']:
                raise JournalError('BPF FD fixture lacks all actual stage records')
            transaction_id = rows[-1]['body']['transaction_id']
            transactions = [item for item in records if item['kind'] == 'transaction' and item['body']['id'] == transaction_id]
            if ([item['body']['state'] for item in transactions] !=
                    ['prepared', 'committed', 'effect_observed', 'response_settled', 'retired']):
                raise JournalError('BPF FD fixture transaction did not settle')
            txn = transactions[-1]['body']
            objects = [item['body'] for item in records if item['kind'] == 'object' and
                       item['body']['owner_transaction'] == transaction_id]
            latest = {item['cookie']: item for item in objects}
            if (txn['grant_id'] != grant_id or txn['result'] != -24 or txn['returned_fd'] is not None or
                    txn['command'] != ('MAP_CREATE' if object_class == 'map' else 'PROG_LOAD') or
                    len(latest) != 1 or any(item['class'] != object_class or item['removal_seq'] is None
                                             for item in latest.values())):
                raise JournalError('BPF FD fixture lacks its real EMFILE object lifecycle')
            obj = next(iter(latest.values()))
            guards = {item['body']['record']['sequence']: item['body']['record'] for item in records if item['kind'] == 'guard'}
            publication, removal = guards.get(obj['publication_seq']), guards.get(obj['removal_seq'])
            attachments = {item['body']['id']: item['body'] for item in records if item['kind'] == 'attachment' and
                           item['body']['grant_id'] == grant_id}
            if (publication is None or removal is None or publication['kind'] != (3 if object_class == 'map' else 1) or
                    removal['kind'] != publication['kind'] + 1 or publication['object_cookie'] != obj['cookie'] or
                    removal['object_cookie'] != obj['cookie'] or publication['kernel_id'] != removal['kernel_id'] or
                    not any(item['kind'] == 'audit' and item['body']['record_type'] == 'SYSCALL' and
                            item['body']['transaction_id'] == transaction_id and item['body']['result'] == -24
                            for item in records) or any(item['state'] != 'released' for item in attachments.values())):
                raise JournalError('BPF FD fixture lacks matching publication, removal, audit or attachment cleanup')
            stage['stage_records'] = [item['body'] for item in rows]
        if stage['start_http_status'] != 204:
            life.settle_failed_bpf_probe(label, container_id, grant_id, state)
        else:
            life._register(label, {'container_id': container_id, 'grant_id': grant_id, 'state': state['Status']})
        evidence = authority.evidence_records()
        partial = {'records': evidence, 'watermark': authority._evidence_sequence,
                   'tail_sha256': authority._evidence_tail}
        if len(canonical(partial)) > 33554432:
            raise JournalError('BPF healthy partial prefix exceeds its bound')
        stage['checkpoint'] = {'watermark': authority._evidence_sequence, 'tail_sha256': authority._evidence_tail}
        carrier.RESULT['bpf_healthy_partial'] = partial
    finally:
        os.unlink(secret)



def _validate_authorization_attempts(attempts):
    if (type(attempts) is not list or len(attempts) != 2 or
            [item.get('command') if type(item) is dict else None for item in attempts] != ['MAP_CREATE', 'PROG_LOAD']):
        raise JournalError('BPF selftest report or direct exit differs')
    for attempt in attempts:
        attr = attempt.get('attributes')
        if (set(attempt) != {'command', 'attributes', 'program_bytes', 'license', 'result', 'close_result'} or
                type(attr) is not dict or set(attr) != {'attr_size', 'attr_hex'} or
                type(attr['attr_size']) is not int or not 0 < attr['attr_size'] <= 4096 or
                type(attr['attr_hex']) is not str or re.fullmatch(r'[0-9a-f]+', attr['attr_hex']) is None or
                len(attr['attr_hex']) != 2 * attr['attr_size'] or type(attempt['result']) is not int or
                attempt['result'] != -1 or attempt['close_result'] is not None or
                (attempt['command'] == 'MAP_CREATE' and (attempt['program_bytes'] is not None or attempt['license'] is not None)) or
                (attempt['command'] == 'PROG_LOAD' and (type(attempt['program_bytes']) is not str or
                 re.fullmatch(r'(?:[0-9a-f]{16})+', attempt['program_bytes']) is None or
                 type(attempt['license']) is not str or not attempt['license'].isascii()))):
            raise JournalError('BPF selftest did not observe both actual EPERM results')
    map_attr = bytearray(72)
    struct.pack_into('<IIII', map_attr, 0, 2, 4, 4, 1)
    map_attr[28:40] = b'feature_test'
    program_attr = bytearray(72)
    struct.pack_into('<II', program_attr, 0, 1, 2)
    if (attempts[0]['attributes'] != {'attr_size': 72, 'attr_hex': map_attr.hex()} or
            attempts[1]['attributes'] != {'attr_size': 72, 'attr_hex': program_attr.hex()} or
            attempts[1]['program_bytes'] != 'b7000000000000009500000000000000' or attempts[1]['license'] != 'GPL'):
        raise JournalError('BPF selftest fixed submitted inputs differ')


def _authorization_probe(case, expected, authority, deadline, probe_name='raw-denial', before_release=None):
    """Run the fixed raw-denial child with FD 8 barrier and preserve its actual terminal evidence."""
    if case['case'] != 'realm-negative' or case['scenario'] not in ('bpf-authorization', 'bpf-broker-kill'):
        raise JournalError('BPF authorization probe case differs')
    evidence = {'version': 1, 'case': 'bpf-authorization', 'parent_pid': os.getpid(),
                'parent_start_ticks': journal.process_start(os.getpid()), 'child_pid': None,
                'child_start_ticks': None, 'pidfd_exited': False, 'wait_status': None,
                'stdout_eof': False, 'stderr_eof': False, 'stdout_base64': '', 'stderr_base64': '',
                'output_limited': False, 'report_valid': False, 'probe_cookie': None}
    carrier.RESULT['bpf_authorization_probe'] = evidence
    opened, duplicated = [], []
    child = pidfd = None
    waited = False
    streams = {'stdout': bytearray(), 'stderr': bytearray()}
    try:
        if (guest_bootstrap._digest('/run/g2/source_guest.py') != expected['source_guest.py'] or
                guest_bootstrap._digest('/opt/g2/g2-bpf-authority') != expected['g2-bpf-authority']):
            raise JournalError('BPF selftest executable or parent script differs')
        seed = os.open('/mnt/g2-seed', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(seed)
        case_fd = os.open('case.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=seed)
        opened.append(case_fd)
        script = os.open('/run/g2/source_guest.py', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(script)
        parent_pidfd = os.pidfd_open(os.getpid(), 0)
        opened.append(parent_pidfd)
        parent_exe = os.open('/proc/self/exe', os.O_RDONLY | os.O_CLOEXEC)
        opened.append(parent_exe)
        null_fd = os.open('/dev/null', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(null_fd)
        stdout_r, stdout_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((stdout_r, stdout_w))
        stderr_r, stderr_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((stderr_r, stderr_w))
        barrier_r, barrier_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((barrier_r, barrier_w))
        for fd in (seed, case_fd, script, parent_pidfd, parent_exe, null_fd, stdout_w, stderr_w, barrier_r):
            duplicated.append(fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32))
        if select.select([parent_pidfd], [], [], 0)[0] or journal.process_start(os.getpid()) != evidence['parent_start_ticks']:
            raise JournalError('BPF selftest parent custody changed')
        actions = [(os.POSIX_SPAWN_DUP2, duplicated[5], 0),
                   (os.POSIX_SPAWN_DUP2, duplicated[6], 1),
                   (os.POSIX_SPAWN_DUP2, duplicated[7], 2),
                   (os.POSIX_SPAWN_DUP2, duplicated[8], 8)]
        actions.extend((os.POSIX_SPAWN_DUP2, duplicated[index], index + 3) for index in range(5))
        executable = '/opt/g2/g2-bpf-authority'
        child = os.posix_spawn(executable, (executable, 'selftest', 'bpf-authorization'),
                               guest_storage.ENV, file_actions=actions)
        evidence['child_pid'] = child
        pidfd = os.pidfd_open(child, 0)
        opened.append(pidfd)
        evidence['child_start_ticks'] = journal.process_start(child)
        for fd in (stdout_w, stderr_w, barrier_r):
            os.close(fd)
            opened.remove(fd)
        if authority is not None and not authority._closed:
            reg = authority.register_selftest_probe(probe_name, child, evidence['child_start_ticks'], pidfd, deadline=deadline)
            evidence['probe_cookie'] = reg['probe_cookie']
        if before_release is not None:
            before_release()
        if os.write(barrier_w, b'Z') != 1:
            raise JournalError('BPF selftest barrier release failed')
        os.close(barrier_w)
        opened.remove(barrier_w)
        for fd in (stdout_r, stderr_r):
            os.set_blocking(fd, False)
        poller = select.poll()
        poller.register(stdout_r, select.POLLIN | select.POLLHUP | select.POLLERR)
        poller.register(stderr_r, select.POLLIN | select.POLLHUP | select.POLLERR)
        poller.register(pidfd, select.POLLIN | select.POLLHUP | select.POLLERR)
        pending = {stdout_r: 'stdout', stderr_r: 'stderr'}
        limit = {'stdout': 16384, 'stderr': 4096}
        until = min(deadline, time.monotonic() + 30)
        while pending or not waited:
            remaining = until - time.monotonic()
            if remaining <= 0:
                raise JournalError('BPF selftest child deadline expired')
            for fd, events in poller.poll(max(1, int(remaining * 1000))):
                if fd == pidfd:
                    if time.monotonic() >= until:
                        raise JournalError('BPF selftest pidfd exit observed after deadline')
                    if events & (select.POLLIN | select.POLLHUP):
                        evidence['pidfd_exited'] = True
                        got, status = os.waitpid(child, os.WNOHANG)
                        if got != child:
                            raise JournalError('BPF selftest pidfd and direct wait disagree')
                        evidence['wait_status'] = status
                        waited = True
                        poller.unregister(pidfd)
                    elif events & select.POLLERR:
                        raise JournalError('BPF selftest pidfd failed')
                elif fd in pending:
                    name = pending[fd]
                    while True:
                        try:
                            data = os.read(fd, 4096)
                        except BlockingIOError:
                            break
                        if not data:
                            evidence[name + '_eof'] = True
                            poller.unregister(fd)
                            del pending[fd]
                            break
                        room = limit[name] - len(streams[name])
                        streams[name].extend(data[:max(room, 0)])
                        if len(data) > room:
                            evidence['output_limited'] = True
                            raise JournalError('BPF selftest output exceeds its bound')
        raw = bytes(streams['stdout'])
        report = strict_json(raw)
        actor = report.get('actor') if type(report) is dict else None
        attempts = report.get('attempts') if type(report) is dict else None
        if (type(report) is not dict or set(report) != {'version', 'case', 'actor', 'attempts'} or
                type(report['version']) is not int or report['version'] != 1 or report['case'] != 'bpf-authorization' or
                raw != canonical(report) + b'\n' or type(actor) is not dict or
                set(actor) != {'tgid', 'tid', 'start_ticks', 'parent_tgid'} or
                any(type(actor[key]) is not int for key in actor) or
                (actor['tgid'], actor['tid'], actor['start_ticks'], actor['parent_tgid']) !=
                (child, child, evidence['child_start_ticks'], evidence['parent_pid']) or
                bytes(streams['stderr']) or not os.WIFEXITED(evidence['wait_status']) or os.WEXITSTATUS(evidence['wait_status']) != 0):
            raise JournalError('BPF selftest report or direct exit differs')
        _validate_authorization_attempts(attempts)
        evidence['report_valid'] = True
        evidence['report'] = report
        return evidence
    finally:
        if child is not None and not waited:
            try:
                if pidfd is not None:
                    signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                else:
                    os.kill(child, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                if pidfd is not None:
                    _held_pidfd_exit(pidfd, deadline, 'BPF selftest cleanup')
                    evidence['pidfd_exited'] = True
                got, status = os.waitpid(child, os.WNOHANG)
                if got == child:
                    evidence['wait_status'] = status
            except (ChildProcessError, JournalError):
                pass
        for fd in duplicated + opened:
            try:
                os.close(fd)
            except OSError:
                pass
        for name in ('stdout', 'stderr'):
            evidence[name + '_base64'] = base64.b64encode(streams[name]).decode('ascii')
        print('G2-BPF-AUTHORIZATION-PROBE ' + canonical(evidence).decode('ascii'), file=sys.stderr, flush=True)


def _checkpoint_bound(authority, deadline):
    with authority.observation(deadline):
        records = authority.evidence_records()
        for i in range(len(records) - 1, -1, -1):
            rec = records[i]
            if rec['kind'] == 'checkpoint' and rec['body']['state'] == 'quiesced':
                seq = rec['sequence']
                return {'checkpoint_sequence': seq, 'tail_sha256': authority._evidence_hashes[seq],
                        'guard_sequence': rec['body']['end_watermark']}
    raise JournalError('checkpoint bound unavailable')


def _run_interleaved_probes(authority, deadline):
    opened, children = [], []
    try:
        seed = os.open('/mnt/g2-seed', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(seed)
        case_fd = os.open('case.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=seed)
        script = os.open('/run/g2/source_guest.py', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        parent_fd = os.pidfd_open(os.getpid(), 0)
        parent_exe = os.open('/proc/self/exe', os.O_RDONLY | os.O_CLOEXEC)
        null_fd = os.open('/dev/null', os.O_RDWR | os.O_CLOEXEC)
        opened.extend((case_fd, script, parent_fd, parent_exe, null_fd))
        for _ in range(2):
            barrier_r, barrier_w = os.pipe2(os.O_CLOEXEC)
            opened.extend((barrier_r, barrier_w))
            duplicates = [fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32) for fd in
                          (seed, case_fd, script, parent_fd, parent_exe, null_fd, barrier_r)]
            opened.extend(duplicates)
            actions = [(os.POSIX_SPAWN_DUP2, duplicates[5], n) for n in (0, 1, 2)]
            actions.extend((os.POSIX_SPAWN_DUP2, duplicates[n], n + 3) for n in range(5))
            actions.append((os.POSIX_SPAWN_DUP2, duplicates[6], 8))
            exe = '/opt/g2/g2-bpf-authority'
            child = os.posix_spawn(exe, (exe, 'selftest', 'raw-denial'), guest_storage.ENV, file_actions=actions)
            pidfd = os.pidfd_open(child, 0)
            opened.append(pidfd)
            children.append([child, pidfd, barrier_w])
            authority.register_selftest_probe('raw-denial', child, journal.process_start(child), pidfd, deadline=deadline)
        for _, _, barrier in children:
            if os.write(barrier, b'\x5a') != 1:
                raise JournalError('interleaved audit barrier release failed')
        for entry in children:
            child, pidfd, _ = entry
            _held_pidfd_exit(pidfd, deadline, 'interleaved audit probe', timeout=30.0)
            got, status = os.waitpid(child, os.WNOHANG)
            if got != child or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
                raise JournalError('interleaved audit probe did not exit successfully')
            entry[0] = None
    finally:
        for child, _, _ in children:
            if child is not None:
                try:
                    os.kill(child, signal.SIGKILL)
                    os.waitpid(child, 0)
                except (OSError, ChildProcessError):
                    pass
        for fd in opened:
            os.close(fd)


def _make_sealed_fixture_memfd(name, byte_val):
    fd = os.memfd_create(name, os.MFD_CLOEXEC | os.MFD_ALLOW_SEALING)
    try:
        os.write(fd, bytes([byte_val]) * 32)
        os.lseek(fd, 0, os.SEEK_SET)
        os.fchmod(fd, 0o400)
        seals = fcntl.F_SEAL_WRITE | fcntl.F_SEAL_GROW | fcntl.F_SEAL_SHRINK | fcntl.F_SEAL_SEAL
        fcntl.fcntl(fd, fcntl.F_ADD_SEALS, seals)
        return fd
    except BaseException:
        os.close(fd)
        raise


def _run_assembly_probe(authority, mode, deadline, *, grant_id=None, nonce=None,
                        before_first=None, before_intermediate=None):
    import socket
    import array
    barrier_r, barrier_w = os.pipe2(os.O_CLOEXEC)
    output_r, output_w = os.pipe2(os.O_CLOEXEC)
    sock_parent, sock_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | getattr(socket, 'SOCK_CLOEXEC', 0))
    fix_a = _make_sealed_fixture_memfd('probe-a', 0x41)
    fix_b = _make_sealed_fixture_memfd('probe-b', 0x42)
    null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
    dups = []
    child = pidfd = listener_fd = None
    try:
        for fd in (null_fd, barrier_r, sock_child.fileno(), fix_a, fix_b, output_w):
            dups.append(fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32))
        actions = [
            (os.POSIX_SPAWN_DUP2, dups[0], 0),
            (os.POSIX_SPAWN_DUP2, dups[5], 1),
            (os.POSIX_SPAWN_DUP2, dups[0], 2),
            (os.POSIX_SPAWN_DUP2, dups[1], 8),
            (os.POSIX_SPAWN_DUP2, dups[2], 9),
            (os.POSIX_SPAWN_DUP2, dups[3], 10),
            (os.POSIX_SPAWN_DUP2, dups[4], 11),
        ]
        executable = '/opt/g2/g2-bpf-authority'
        child = os.posix_spawn(executable, (executable, 'selftest', mode), guest_storage.ENV, file_actions=actions)
        pidfd = os.pidfd_open(child, 0)
        start_ticks = journal.process_start(child)
        os.close(barrier_r)
        barrier_r = None
        os.close(output_w)
        output_w = None
        sock_child.close()
        sock_child = None
        sock_parent.settimeout(min(10.0, max(0.001, deadline - time.monotonic())))
        data, anc, flags, _ = sock_parent.recvmsg(256, socket.CMSG_SPACE(4), getattr(socket, 'MSG_CMSG_CLOEXEC', 0))
        fds = []
        for lvl, typ, cdata in anc:
            if lvl == socket.SOL_SOCKET and typ == socket.SCM_RIGHTS:
                arr = array.array('i')
                arr.frombytes(cdata[:len(cdata) - (len(cdata) % arr.itemsize)])
                fds.extend(arr)
        if len(fds) != 1 or data != b'{"ready":true}' or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or len(anc) != 1:
            for rfd in fds:
                os.close(rfd)
            raise JournalError('assembly probe did not return listener FD')
        listener_fd = fds[0]
        if mode == 'extra-fd-delays-removal':
            if grant_id is None or nonce is None:
                raise JournalError('extra descriptor selftest requires its admitted grant and nonce')
            reg = authority.register_extra_fd_probe(child, start_ticks, pidfd, listener_fd, deadline=deadline)
            authority.selftest_baseline_step(mode, nonce, grant_id, deadline=deadline)
            if before_first is None or before_intermediate is None:
                raise JournalError('extra descriptor selftest requires container lifecycle callbacks')
            before_first()
        else:
            reg = authority.register_selftest_probe(mode, child, start_ticks, pidfd,
                                                    listener_fd=listener_fd, fixture_a_fd=fix_a, fixture_b_fd=fix_b,
                                                    deadline=deadline)
        if os.write(barrier_w, b'\x5a') != 1:
            raise JournalError('assembly probe barrier release failed')
        def read_frame():
            raw = bytearray()
            while len(raw) <= 16384:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([output_r], [], [], remaining)[0]:
                    raise JournalError('assembly probe frame deadline expired')
                byte = os.read(output_r, 1)
                if not byte:
                    raise JournalError('assembly probe frame truncated')
                if byte == b'\n':
                    frame = strict_json(bytes(raw))
                    if type(frame) is not dict or canonical(frame) != bytes(raw) or frame.get('mode') != mode:
                        raise JournalError('assembly probe frame is not canonical')
                    return frame
                raw.extend(byte)
            raise JournalError('assembly probe frame exceeds limit')

        frames = []
        intermediate = None
        if mode == 'extra-fd-delays-removal':
            held = read_frame()
            if (held.get('phase') != 'held' or type(held.get('fd')) is not int or held['fd'] < 0 or
                    held.get('tid') != child):
                raise JournalError('extra descriptor child did not report a held descriptor')
            while True:
                records = authority._complete_evidence(deadline)
                stages = [e['body'] for e in records if e['kind'] == 'selftest_runtime_stage' and
                          e['body']['mode'] == mode and e['body']['nonce'] == nonce and
                          e['body']['phase'] == 'injected']
                cases = [e['body'] for e in records if e['kind'] == 'selftest_case' and
                         e['body']['name'] == mode and e['body']['nonce'] == nonce and e['body']['phase'] == 'observed']
                if len(stages) > 1 or len(cases) > 1:
                    raise JournalError('extra descriptor readiness evidence duplicated')
                if stages and cases:
                    stage, case = stages[0], cases[0]
                    cookie = stage['mutation']['program_cookie']
                    actors = [e['body'] for e in records if e['kind'] == 'probe_actor' and
                              e['body']['probe_cookie'] == reg['probe_cookie'] and e['body']['name'] == mode]
                    objects = [e['body'] for e in records if e['kind'] == 'object' and e['body']['cookie'] == cookie]
                    attached = [e['body'] for e in records if e['kind'] == 'attachment' and
                                e['body']['grant_id'] == grant_id and e['body']['program_cookie'] == cookie and
                                e['body']['state'] == 'detached']
                    closes = [e['body'] for e in records if e['kind'] == 'grant_object_cleanup' and
                              e['body']['grant_id'] == grant_id and e['body']['object_cookie'] == cookie and
                              e['body']['step'] == 1 and e['body']['result'] == 0]
                    detaches = [e['body'] for e in records if e['kind'] == 'transaction' and
                                e['body']['id'] in case['transaction_ids'] and e['body']['command'] == 'PROG_DETACH' and
                                e['body']['grant_id'] == grant_id and e['body']['state'] == 'retired' and
                                e['body']['result'] == 0 and any(e['body']['output'] == {'attachment_id': a['id']} for a in attached)]
                    if (len(actors) == len(attached) == len(closes) == len(detaches) == 1 and objects and
                            stage['grant_id'] == grant_id and stage['invocation_id'] == case['invocation_id'] and
                            stage['notifying_cookie'] == actors[0]['task_cookie'] == case['actor_cookie'] and
                            stage['mutation']['listener_id'] == actors[0]['listener_id'] == reg['listener_id'] and
                            stage['mutation']['installed_fd'] == held['fd'] and actors[0]['tid'] == child and
                            case['reference_cookies'] == [cookie] and objects[-1]['state'] == 'live' and
                            objects[-1]['removal_seq'] is None and closes[0]['creator_transaction_id'] == objects[-1]['owner_transaction']):
                        break
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise JournalError('extra descriptor injection, detach and close readiness not observed')
                select.select([], [], [], min(0.05, remaining))
            before_intermediate()
            intermediate = _checkpoint_bound(authority, deadline)
            records = authority.evidence_records()
            objects = [e['body'] for e in records if e['kind'] == 'object' and e['body']['cookie'] == cookie]
            grants = [e['body'] for e in records if e['kind'] == 'grant' and e['body']['id'] == grant_id]
            if (not objects or objects[-1]['state'] != 'live' or objects[-1]['removal_seq'] is not None or
                    not grants or grants[-1]['phase'] in ('settling', 'retired', 'revoked') or
                    any(e['kind'] == 'guard' and e['body']['record']['kind'] == 2 and
                        e['body']['record']['object_cookie'] == cookie for e in records)):
                raise JournalError('extra descriptor checkpoint lacks the live unretired program')
            if os.write(barrier_w, b'\x5a') != 1:
                raise JournalError('extra descriptor final release failed')
            closed = read_frame()
            if (closed.get('phase') != 'closed' or closed.get('fd') != held['fd'] or
                    closed.get('close_result') != 0 or closed.get('tid') != child):
                raise JournalError('extra descriptor child did not report the matching close')
            frames.extend((held, closed))
        if mode == 'leader-exit':
            if os.write(barrier_w, b'\x5a') != 1:
                raise JournalError('assembly leader-exit second barrier release failed')
        if mode == 'tid-reuse':
            first = read_frame()
            if first.get('phase') != 'first-child-started' or type(first.get('first_tid')) is not int:
                raise JournalError('first TID child evidence invalid')
            first_tid = first['first_tid']
            first_fd = os.pidfd_open(first_tid, 0)
            try:
                authority.register_tid_reuse_child(reg['probe_cookie'], first_tid, journal.process_start(first_tid), first_fd, deadline=deadline)
                if os.write(barrier_w, b'\x5a') != 1:
                    raise JournalError('first TID child release failed')
                _held_pidfd_exit(first_fd, deadline, 'first TID child')
            finally:
                os.close(first_fd)
            second = read_frame()
            if (second.get('phase') != 'reused-child-started' or second.get('first_tid') != first_tid or
                    second.get('new_tid') != first_tid or second.get('first_wait_status') != 0):
                raise JournalError('reused TID child evidence invalid')
            second_fd = os.pidfd_open(first_tid, 0)
            try:
                authority.register_tid_reuse_child(reg['probe_cookie'], first_tid, journal.process_start(first_tid), second_fd, deadline=deadline)
                if os.write(barrier_w, b'\x5a') != 1:
                    raise JournalError('reused TID child release failed')
                _held_pidfd_exit(second_fd, deadline, 'reused TID child')
            finally:
                os.close(second_fd)
            frames.extend((first, second))
        if mode != 'extra-fd-delays-removal':
            frames.append(read_frame())
            final = frames[-1]
            if final.get('bpf_result') != -1:
                raise JournalError('assembly probe did not observe EPERM')
            if mode == 'fd-slot-reuse':
                valid = set(final) == {'mode', 'tid', 'dup_result', 'bpf_result'} and final['tid'] == child and final['dup_result'] == 10
            elif mode in ('nonleader-files', 'shared-files'):
                valid = (set(final) == {'mode', 'leader_tid', 'tid', 'clone_result', 'unshare_result', 'dup_result', 'bpf_result', 'leader_is_a', 'leader_is_b'} and
                         final['leader_tid'] == child and type(final['tid']) is int and final['tid'] > 0 and
                         final['tid'] != child and final['clone_result'] == final['tid'] and
                         final['unshare_result'] == 0 and final['dup_result'] == 10 and
                         final['leader_is_a'] is (mode == 'nonleader-files') and
                         final['leader_is_b'] is (mode == 'shared-files'))
            elif mode == 'leader-exit':
                valid = (set(final) == {'mode', 'leader_tid', 'tid', 'barrier_result', 'dup_result', 'bpf_result'} and
                         final['leader_tid'] == child and type(final['tid']) is int and
                         final['tid'] > 0 and final['tid'] != child and final['barrier_result'] == 1 and final['dup_result'] == 10)
            else:
                valid = (set(final) == {'mode', 'first_tid', 'first_wait_status', 'clone_result', 'wait_status', 'dup_result', 'bpf_result'} and
                         final['first_tid'] == first_tid == final['clone_result'] and
                         final['first_wait_status'] == final['wait_status'] == 0 and final['dup_result'] == 10)
            if not valid:
                raise JournalError('assembly probe syscall or task results differ')
        os.close(barrier_w)
        barrier_w = None
        probe_pid = child
        _held_pidfd_exit(pidfd, deadline, 'assembly selftest probe')
        got, status, _ = os.wait4(probe_pid, 0)
        child = None
        if got != probe_pid or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            raise JournalError('assembly selftest probe did not exit successfully')
        return {'pid': probe_pid, 'start_ticks': start_ticks, 'probe_cookie': reg['probe_cookie'], 'wait_pid': got, 'status': status,
                'frames': frames, 'intermediate': intermediate,
                'program_cookie': cookie if mode == 'extra-fd-delays-removal' else None}
    finally:
        if child is not None:
            try:
                os.kill(child, signal.SIGKILL)
                os.waitpid(child, 0)
            except OSError:
                pass
        for item in [barrier_r, barrier_w, output_r, output_w, fix_a, fix_b, null_fd, pidfd, listener_fd] + dups:
            if item is not None:
                try:
                    os.close(item)
                except OSError:
                    pass
        for s in (sock_parent, sock_child):
            if s is not None:
                try:
                    s.close()
                except OSError:
                    pass


def _run_seal_probe(authority, name, nonce, deadline):
    import socket
    barrier_r, barrier_w = os.pipe2(os.O_CLOEXEC)
    output_r, output_w = os.pipe2(os.O_CLOEXEC)
    null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
    duplicates, child, pidfd = [], None, None
    transfer_parent = transfer_child = None
    try:
        duplicates = [fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32) for fd in (null_fd, barrier_r, output_w)]
        actions = [(os.POSIX_SPAWN_DUP2, duplicates[0], 0), (os.POSIX_SPAWN_DUP2, duplicates[2], 1),
                   (os.POSIX_SPAWN_DUP2, duplicates[0], 2), (os.POSIX_SPAWN_DUP2, duplicates[1], 8)]
        if name == 'seal-detach-denied':
            transfer_parent, transfer_child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
            transfer_child.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            duplicates.append(fcntl.fcntl(transfer_child.fileno(), fcntl.F_DUPFD_CLOEXEC, 32))
            actions.append((os.POSIX_SPAWN_DUP2, duplicates[-1], 9))
        executable = '/opt/g2/g2-bpf-authority'
        child = os.posix_spawn(executable, (executable, 'selftest', name),
                               guest_storage.ENV, file_actions=actions)
        pidfd = os.pidfd_open(child, 0)
        if transfer_child is not None:
            transfer_child.close()
            transfer_child = None
        os.close(output_w)
        output_w = None
        os.close(duplicates[2])
        duplicates[2] = None
        if name == 'seal-unload-denied':
            authority.register_seal_unload_probe(child, journal.process_start(child), pidfd, output_r, deadline=deadline)
        else:
            authority.register_seal_detach_probe(child, journal.process_start(child), pidfd, transfer_parent.fileno(), deadline=deadline)
            authority.selftest_baseline_step(name, nonce, None, deadline=deadline)
        if os.write(barrier_w, b'Z') != 1:
            raise JournalError('seal unload barrier release failed')
        os.close(barrier_w)
        barrier_w = None
        _held_pidfd_exit(pidfd, deadline, 'seal unload probe')
        got, status = os.waitpid(child, os.WNOHANG)
        if got != child or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
            raise JournalError('seal unload child did not exit successfully')
        child = None
    finally:
        if child is not None:
            try:
                os.kill(child, signal.SIGKILL)
                os.waitpid(child, 0)
            except OSError:
                pass
        for fd in (barrier_r, barrier_w, output_r, output_w, null_fd, pidfd, *duplicates):
            if fd is not None:
                os.close(fd)
        for sock in (transfer_parent, transfer_child):
            if sock is not None:
                sock.close()


def _run_runtime_arm_stage(postgres, life, audit_owner, authority, name, mode, deadline):
    directory = os.open('/var/lib/g2/' + name + '/pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fchown(directory, postgres.uid, postgres.gid)
        os.fchmod(directory, 0o700)
        for entry in os.listdir(directory):
            p = '/var/lib/g2/' + name + '/pgdata/' + entry
            if os.path.isdir(p) and not os.path.islink(p):
                import shutil
                shutil.rmtree(p)
            else:
                os.unlink(p)
        os.fsync(directory)
    finally:
        os.close(directory)
    secret = '/run/g2/pg-secret-' + name
    secret_fd = os.open(secret, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o400)
    try:
        os.fchown(secret_fd, postgres.uid, postgres.gid)
        os.fchmod(secret_fd, 0o400)
        secret_bytes = (secrets.token_hex(32) + '\n').encode('ascii')
        os.write(secret_fd, secret_bytes)
        os.fsync(secret_fd)
    finally:
        os.close(secret_fd)
    try:
        label = 'g2-rt-' + (mode or 'snapshot')
        config = postgres._config(name, True, secret)
        created = postgres._request('POST', '/containers/create?name=' + label, config, deadline=deadline)
        container_id = created['Id']
        postgres._inspect(container_id, config, deadline=deadline)
        grant_id = authority.admit_container(config, container_id, 'postgres-init-' + name)
        nonce = secrets.token_hex(16) if mode is not None else None
        replay_after = None
        if mode == 'fd-arm-replay':
            authority.selftest_baseline_step(mode, nonce, grant_id, deadline=deadline)
            authority.selftest_fd_replay(container_id, secrets.token_hex(16), deadline)
            replay_after = _checkpoint_bound(authority, deadline)
        elif mode == 'extra-fd-delays-removal':
            def start_extra_container():
                postgres._request('POST', '/containers/' + container_id + '/start', timeout=60, deadline=deadline, expected_status=204)
                waited = postgres._request('POST', '/containers/' + container_id + '/wait?condition=not-running',
                                           timeout=60, deadline=deadline, expected_status=200)
                if type(waited) is not dict or type(waited.get('StatusCode')) is not int or waited['StatusCode'] != 0:
                    raise JournalError('extra descriptor container did not stop successfully')

            def remove_extra_container():
                postgres._request('DELETE', '/containers/' + container_id + '?force=1&v=1', deadline=deadline)

            probe = _run_assembly_probe(authority, mode, deadline, grant_id=grant_id, nonce=nonce,
                                        before_first=start_extra_container, before_intermediate=remove_extra_container)
            probe.pop('wait_pid')
            if authority.settle_container(container_id, 'completed', deadline) != 'retired':
                raise JournalError('extra descriptor container grant did not retire after child exit')
            replay_after = _checkpoint_bound(authority, deadline)
            records = authority.evidence_records()
            cookie = probe['program_cookie']
            objects = [e['body'] for e in records if e['kind'] == 'object' and e['body']['cookie'] == cookie]
            cleanup = [e['body'] for e in records if e['kind'] == 'grant_object_cleanup' and
                       e['body']['grant_id'] == grant_id and e['body']['object_cookie'] == cookie and e['body']['step'] == 2]
            if (not objects or objects[-1]['state'] != 'id_removed' or objects[-1]['removal_seq'] is None or
                    len(cleanup) != 1 or cleanup[0]['guard_sequence'] != objects[-1]['removal_seq']):
                raise JournalError('extra descriptor final checkpoint lacks matching object removal')
            return {'grant_id': grant_id, 'nonce': nonce, 'status': 204, 'replay_after': replay_after,
                    'extra_fd': probe}
        elif mode in ('feature-map', 'extended-info-ebadf', 'replace-probe-ebadf', 'allowed-device-start',
                      'cgroup-auto-retire', 'wrong-program-byte', 'wrong-flags', 'wrong-target',
                      'attachment-replace', 'failure-cleanup'):
            authority.selftest_baseline_step(mode, nonce, grant_id, deadline=deadline)
        elif mode is not None and mode != 'wrong-generation':
            authority.selftest_runtime_arm(container_id, mode, nonce, deadline=deadline)
        connection = guest_postgres._UnixHTTP(postgres.daemon, 60)
        try:
            connection.request('POST', '/v1.41/containers/' + container_id + '/start')
            response = connection.getresponse()
            status = response.status
            response.read(16385)
        finally:
            connection.close()
        if mode in ('wrong-target', 'failure-cleanup') and not 400 <= status <= 599:
            raise JournalError('BPF negative runtime case unexpectedly started')
        if mode == 'attachment-replace' and status != 204:
            raise JournalError('BPF replacement runtime case failed to start')
        if mode == 'attachment-replace':
            waited = postgres._request('POST', '/containers/' + container_id + '/wait?condition=not-running',
                                       timeout=60, deadline=deadline, expected_status=200)
            if type(waited) is not dict or type(waited.get('StatusCode')) is not int or waited['StatusCode'] != 0:
                raise JournalError('BPF replacement container did not stop successfully')
            while True:
                records = authority._complete_evidence(deadline)
                replacements = [e['body'] for e in records if e['kind'] == 'attachment' and
                                e['body']['grant_id'] == grant_id and e['body']['state'] == 'attached' and
                                e['body']['replaces'] is not None]
                detached = [e['body'] for e in records if e['kind'] == 'attachment' and
                            e['body']['grant_id'] == grant_id and e['body']['state'] == 'detached' and
                            any(e['body']['id'] == a['id'] for a in replacements)]
                if len(replacements) == 1 and len(detached) == 1:
                    break
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise JournalError('BPF replacement attachment did not detach before container removal')
                select.select([], [], [], min(0.05, remaining))
        item = postgres._inspect(container_id, config, deadline=deadline)
        state = item.get('State', {})
        if status == 204:
            wait_deadline = min(deadline, time.monotonic() + 60)
            while state.get('Running') is True and time.monotonic() < wait_deadline:
                select.select([], [], [], 0.05)
                item = postgres._inspect(container_id, config, deadline=deadline)
                state = item.get('State', {})
        if mode == 'wrong-generation':
            if status != 204 or state.get('Running') is not False or state.get('Pid') != 0:
                raise JournalError('wrong-generation fixture did not stop after a successful start')
            if authority.settle_container(container_id, 'completed', deadline) != 'retired':
                raise JournalError('wrong-generation grant did not retire')
            authority.selftest_baseline_step(mode, nonce, grant_id, deadline=deadline)
            connection = guest_postgres._UnixHTTP(postgres.daemon, min(60, max(0.001, deadline - time.monotonic())))
            try:
                connection.request('POST', '/v1.41/containers/' + container_id + '/start')
                response = connection.getresponse()
                denied_status = response.status
                response.read(16385)
            finally:
                connection.close()
            if not 400 <= denied_status <= 599:
                raise JournalError('retired grant runtime start was not rejected')
            replay_after = _checkpoint_bound(authority, deadline)
        postgres._request('DELETE', '/containers/' + container_id + '?force=1&v=1', deadline=deadline)
        if mode != 'wrong-generation':
            settle_outcome = 'completed' if status == 204 else 'failed'
            authority.settle_container(container_id, settle_outcome, deadline)
        return {'grant_id': grant_id, 'nonce': nonce, 'status': status, 'replay_after': replay_after}
    finally:
        try:
            os.unlink(secret)
        except OSError:
            pass


def _run_container_bpf_case(postgres, authority, deadline):
    config = bpf_authority.container_selftest_config()
    created = postgres._request('POST', '/containers/create?name=g2-container-bpf-selftest', config, deadline=deadline)
    container_id = created.get('Id') if type(created) is dict else None
    if type(container_id) is not str or re.fullmatch(r'[0-9a-f]{64}', container_id) is None:
        raise JournalError('container BPF fixture identity invalid')
    grant = authority.admit_container(config, container_id, 'container-bpf-selftest')
    nonce = secrets.token_hex(16)
    authority.selftest_baseline_step('container-bpf', nonce, grant, deadline=deadline)
    postgres._request('POST', '/containers/' + container_id + '/start', deadline=deadline)
    item = postgres._request('GET', '/containers/' + container_id + '/json', deadline=deadline)
    state = item.get('State') if type(item) is dict else None
    pid = state.get('Pid') if type(state) is dict else None
    if type(item) is not dict or item.get('Id') != container_id or type(state) is not dict or state.get('Running') is not True or type(pid) is not int or pid <= 1:
        raise JournalError('container BPF fixture did not start')
    pidfd = os.pidfd_open(pid, 0)
    fifo = None
    try:
        authority.register_container_raw_denial(container_id, pid, journal.process_start(pid), pidfd, deadline=deadline)
        while fifo is None:
            try:
                fifo = os.open('/run/g2/container-bpf.fifo', os.O_WRONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
            except OSError as exc:
                if exc.errno != errno.ENXIO or time.monotonic() >= deadline or select.select([pidfd], [], [], 0)[0]:
                    raise
                select.select([], [], [], min(0.01, max(0, deadline - time.monotonic())))
        st = os.fstat(fifo)
        if not stat.S_ISFIFO(st.st_mode) or st.st_uid != 0 or st.st_gid != 10002 or stat.S_IMODE(st.st_mode) != 0o640:
            raise JournalError('container BPF FIFO identity differs')
        if os.write(fifo, b'Z') != 1:
            raise JournalError('container BPF fixture release failed')
        os.close(fifo)
        fifo = None
        _held_pidfd_exit(pidfd, deadline, 'container BPF fixture', timeout=60.0)
        while True:
            item = postgres._request('GET', '/containers/' + container_id + '/json', deadline=deadline)
            state = item.get('State', {})
            if state.get('Running') is False:
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise JournalError('container BPF fixture exit was not reflected by Docker')
            select.select([], [], [], min(0.05, remaining))
        if item.get('Id') != container_id or state.get('ExitCode') != 0:
            raise JournalError('container BPF fixture failed its actual syscall checks')
        postgres._request('DELETE', '/containers/' + container_id + '?force=1&v=1', deadline=deadline)
        authority.settle_container(container_id, 'completed', deadline)
        return nonce
    finally:
        if fifo is not None:
            os.close(fifo)
        os.close(pidfd)


def _held_pidfd_exit(pidfd, deadline, label, timeout=5.0):
    until = min(deadline, time.monotonic() + timeout)
    remaining = until - time.monotonic()
    if remaining <= 0 or not select.select([pidfd], [], [], remaining)[0] or time.monotonic() >= until:
        raise JournalError(label + ' held pidfd exit not observed before deadline')


def _close_terminal_handles(broker_pidfd, observer):
    prior_failure = sys.exc_info()[0] is not None
    first_error = None
    for stage, close in (('terminal-broker-pidfd-cleanup', lambda: os.close(broker_pidfd)),
                         ('terminal-observer-cleanup', observer.close)):
        try:
            close()
        except BaseException as exc:
            carrier.RESULT.setdefault('cleanup_errors', []).append({'stage': stage, 'error_type': type(exc).__name__})
            if first_error is None:
                first_error = exc
    if first_error is not None and not prior_failure:
        raise first_error


def _terminal_records(client, finish, deadline):
    records, tail = client.read_all(deadline=deadline)
    end = finish['end_sequence']
    if type(end) is not int or end < 0 or len(records) < end:
        raise JournalError('terminal observer FINISH record chain incomplete')
    if len(records) != end:
        records = records[:end]
        tail = (hashlib.sha256(b'HOUFENG-G2-BPF-OBS-V1\0' + canonical(records[-1])).hexdigest()
                if records else '0' * 64)
    if tail != finish['terminal_sha256']:
        raise JournalError('terminal observer FINISH digest differs from record chain')
    return records


def _terminal_module_instance(arm, finish, broker, observer, epoch):
    before, after = arm['authority_before'], finish['authority_after']
    for field in ('module_instance', 'broker_cookie', 'observer_cookie'):
        value = after.get(field)
        if type(value) is not int or value <= 0 or before.get(field) != value:
            raise JournalError('terminal native ' + field + ' missing or changed')
    if (after['broker_cookie'] != broker['task_cookie'] or after['observer_cookie'] != observer['task_cookie'] or
            before.get('epoch') != epoch or after.get('epoch') != epoch):
        raise JournalError('terminal native broker, observer, or epoch identity differs')
    return after['module_instance']


def _broker_native_start(records, broker):
    principals = [r['body'] for r in records if r['kind'] == 'principal' and
                  r['body']['role'] == 'BOOTSTRAP_BROKER' and r['body']['id'] == broker['principal_id']]
    if len(principals) != 1:
        raise JournalError('bootstrap broker principal evidence missing or duplicated')
    principal = principals[0]
    start = principal['start_ns']
    if (type(start) is not int or start <= 0 or start // 10000000 != broker['starttime'] or
            principal['task_cookie'] != broker['task_cookie'] or
            (principal['tgid'], principal['tid']) != (broker['pid'], broker['pid'])):
        raise JournalError('bootstrap broker native identity differs from held task')
    return start


def _collect_terminal_child(child, pidfd, stdout_fd, stderr_fd, deadline, state):
    streams = {'stdout': bytearray(), 'stderr': bytearray()}
    limits = {'stdout': 16384, 'stderr': 4096}
    pending = {stdout_fd: 'stdout', stderr_fd: 'stderr'}
    poller = select.poll()
    for fd in pending:
        os.set_blocking(fd, False)
        poller.register(fd, select.POLLIN | select.POLLHUP | select.POLLERR)
    poller.register(pidfd, select.POLLIN | select.POLLHUP | select.POLLERR)
    until = min(deadline, time.monotonic() + 30)
    while pending or not state['waited']:
        remaining = until - time.monotonic()
        if remaining <= 0:
            raise JournalError('terminal probe output or held pidfd deadline expired')
        for fd, events in poller.poll(max(1, int(remaining * 1000))):
            if fd == pidfd:
                if time.monotonic() >= until:
                    raise JournalError('terminal probe pidfd exit observed after deadline')
                if not events & (select.POLLIN | select.POLLHUP):
                    raise JournalError('terminal probe held pidfd failed')
                got, status = os.waitpid(child, os.WNOHANG)
                if got != child:
                    raise JournalError('terminal probe held pidfd and direct wait disagree')
                state['waited'], state['status'] = True, status
                poller.unregister(pidfd)
            elif fd in pending:
                name = pending[fd]
                while True:
                    try:
                        data = os.read(fd, 4096)
                    except BlockingIOError:
                        break
                    if not data:
                        poller.unregister(fd)
                        del pending[fd]
                        break
                    if len(streams[name]) + len(data) > limits[name]:
                        raise JournalError('terminal probe output exceeds its bound')
                    streams[name].extend(data)
    return bytes(streams['stdout']), bytes(streams['stderr'])


def _realm_root_actor(authority):
    records = authority.evidence_records()
    principals = [r['body'] for r in records if r['kind'] == 'principal' and
                  r['body']['id'] == authority.principal_id and r['body']['role'] == 'REALM_ROOT' and
                  r['body']['task_cookie'] == authority.child_cookie]
    if len(principals) != 1:
        raise JournalError('realm root principal evidence missing or duplicated')
    principal = principals[0]
    pid = os.getpid()
    ticks = journal.process_start(pid)
    start = principal['start_ns']
    if (type(start) is not int or start <= 0 or start // 10000000 != ticks or
            (principal['tgid'], principal['tid']) != (pid, pid) or principal['terminal'] is not None):
        raise JournalError('realm root principal native identity differs from caller')
    return {'tgid': pid, 'tid': pid, 'start_ticks': ticks,
            'task_cookie': principal['task_cookie'], 'start_ns': start}


def _registered_probe_actor(records, name, probe_cookie, pid, start_ticks, parent_cookie):
    actors = [r['body'] for r in records if r['kind'] == 'probe_actor' and
              r['body']['name'] == name and r['body']['probe_cookie'] == probe_cookie]
    if len(actors) != 1:
        raise JournalError('registered probe actor evidence missing or duplicated')
    actor = actors[0]
    if (type(actor['task_cookie']) is not int or actor['task_cookie'] <= 0 or
            type(actor['start_ns']) is not int or actor['start_ns'] <= 0 or
            actor['start_ns'] // 10000000 != start_ticks or
            (actor['tgid'], actor['tid'], actor['parent_cookie']) != (pid, pid, parent_cookie)):
        raise JournalError('registered probe actor identity differs from held child')
    watches = [r['body']['record'] for r in records if r['kind'] == 'guard' and
               r['body']['record']['kind'] == 12 and r['body']['record']['task_cookie'] == actor['task_cookie']]
    if (len(watches) != 1 or watches[0]['task_start_ns'] != actor['start_ns'] or
            watches[0]['parent_cookie'] != parent_cookie or
            (watches[0]['detail']['tgid'], watches[0]['detail']['tid']) != (pid, pid)):
        raise JournalError('registered probe native watch evidence missing or differs')
    return actor, watches[0]

def _observed_actor(records, cookie):
    matches = []
    for event in records:
        if event['kind'] in ('principal', 'probe_actor') and event['body'].get('task_cookie') == cookie:
            body = event['body']
            matches.append((body['tgid'], body['tid'], body['start_ns']))
        elif event['kind'] == 'guard':
            guard = event['body']['record']
            if guard['task_cookie'] == cookie and guard['kind'] in (9, 10, 11, 12, 13, 15):
                matches.append((guard['detail']['tgid'], guard['detail']['tid'], guard['task_start_ns']))
    if not matches or len(set(matches)) != 1 or any(type(x) is not int or x <= 0 for x in matches[0]):
        raise JournalError('actual notifying task identity is missing or inconsistent')
    tgid, tid, start = matches[0]
    return {'tgid': tgid, 'tid': tid, 'start_ticks': start // 10000000,
            'task_cookie': cookie, 'start_ns': start}


def _terminal_guard_exit(records, kind, cookie, pid, start_ns=None, parent_cookie=None, wait_status=None, exe_identity=None):
    exits = [r['body']['record'] for r in records if r['kind'] == 'guard' and
             r['body']['record']['kind'] == kind and r['body']['record']['task_cookie'] == cookie]
    if len(exits) != 1:
        raise JournalError('terminal native exit evidence missing or duplicated for task %d' % pid)
    record = exits[0]
    detail = record['detail']
    if (type(detail) is not dict or (detail.get('tgid'), detail.get('tid')) != (pid, pid) or
            type(record['sequence']) is not int or record['sequence'] <= 0 or
            type(record['task_start_ns']) is not int or record['task_start_ns'] <= 0 or
            (start_ns is not None and record['task_start_ns'] != start_ns) or
            (parent_cookie is not None and record['parent_cookie'] != parent_cookie) or
            (exe_identity is not None and (detail.get('exe_dev'), detail.get('exe_ino')) != exe_identity) or
            type(detail.get('exit_code')) is not int or
            (kind == 15 and record['result'] != detail['exit_code']) or
            (wait_status is not None and detail['exit_code'] != wait_status)):
        raise JournalError('terminal native exit identity or wait status differs for task %d' % pid)
    return record


def _wrong_actor_observation(records, sequences, evidence, parent_cookie):
    actor, watch = _registered_probe_actor(records, 'raw-denial', evidence['probe_cookie'],
                                           evidence['child_pid'], evidence['child_start_ticks'], parent_cookie)
    interval = [records[seq - 1] for seq in sequences]
    native = [r['body']['record'] for r in interval if r['kind'] == 'guard' and
              r['body']['record']['kind'] == 13 and r['body']['record']['task_cookie'] == actor['task_cookie'] and
              r['body']['record']['operation'] == 0]
    if len(native) != 1:
        raise JournalError('wrong-actor MAP_CREATE native evidence missing or duplicated')
    guard = native[0]
    detail = guard['detail']
    attempt = evidence['report']['attempts'][0]
    if (guard['task_start_ns'] != actor['start_ns'] or
            (detail['tgid'], detail['tid']) != (actor['tgid'], actor['tid']) or
            (detail['exe_dev'], detail['exe_ino']) != (watch['detail']['exe_dev'], watch['detail']['exe_ino']) or
            guard['result'] != attempt['result'] or attempt['command'] != 'MAP_CREATE'):
        raise JournalError('wrong-actor MAP_CREATE native identity or result differs')
    stamp = {'seconds': detail['seconds'], 'milliseconds': detail['nanoseconds'] // 1000000}
    audits = [r['body'] for r in interval if r['kind'] == 'audit' and
              r['body']['record_type'] == 'SYSCALL' and r['body']['stamp'] == stamp and
              r['body']['serial'] == guard['audit_serial'] and r['body']['task_cookie'] == actor['task_cookie']]
    if (len(audits) != 1 or audits[0]['command'] != attempt['command'] or
            audits[0]['result'] != guard['result'] or type(audits[0].get('raw')) is not dict or
            audits[0]['raw'].get('tgid') != actor['tgid']):
        raise JournalError('wrong-actor MAP_CREATE typed raw audit evidence missing or differs')
    request = {'invocation_id': None, 'notification_id': None, 'command': attempt['command'],
               'attributes': attempt['attributes'], 'program_bytes': attempt['program_bytes'],
               'license': attempt['license'], 'input_objects': None}
    return ({'tgid': actor['tgid'], 'tid': actor['tid'], 'start_ticks': evidence['child_start_ticks'],
             'task_cookie': actor['task_cookie'], 'start_ns': actor['start_ns']}, request,
            dict(stamp, serial=guard['audit_serial']), attempt['result'])


def _broker_revocation_records(records, begin, broker, start_ns):
    guards = [r['body']['record'] for r in records if r['sequence'] > begin and r['kind'] == 'guard' and
              r['body']['record']['task_cookie'] == broker['task_cookie']]
    transitions = [r for r in guards if r['kind'] == 14 and r['task_start_ns'] == start_ns]
    phase2 = [r for r in transitions if r['operation'] == 2]
    phase0 = [r for r in transitions if r['operation'] == 0]
    if len(phase2) != 1 or len(phase0) != 1 or phase2[0]['sequence'] >= phase0[0]['sequence']:
        return None
    raw = [r for r in guards if r['kind'] == 13 and r['task_start_ns'] == start_ns and
           (r['detail']['tgid'], r['detail']['tid']) == (broker['pid'], broker['pid'])]
    before = [(r['operation'], r['result']) for r in raw
              if phase2[0]['sequence'] < r['sequence'] < phase0[0]['sequence']]
    after = [(r['operation'], r['result']) for r in raw if r['sequence'] > phase0[0]['sequence']]
    if (before != [(0, -1), (5, -1), (8, -1), (1, 0)] or
            after[:4] != [(0, -1), (5, -1), (8, -1), (1, -1)]):
        return None
    identity = (raw[0]['detail']['exe_dev'], raw[0]['detail']['exe_ino'])
    if any((r['detail']['exe_dev'], r['detail']['exe_ino']) != identity for r in raw):
        return None
    return identity


def _capture_terminal_healthy(case, audit_owner, authority, deadline):
    # Capture only after any stopped probe is registered, and before the
    # terminal operation can revoke or destroy the broker.
    bootstrap = authority.bootstrap_evidence(case, deadline)
    with authority.observation(deadline):
        audit_owner.await_health(deadline)
    records = authority._complete_evidence(deadline)
    watermark = len(records)
    if (records[:len(bootstrap['records'])] != bootstrap['records'] or
            authority._evidence_hashes[len(bootstrap['records'])] != bootstrap['bootstrap_tail']):
        raise JournalError('terminal bootstrap is not a healthy transcript prefix')
    carrier.RESULT['bpf_bootstrap'] = bootstrap
    carrier.RESULT['bpf_healthy_partial'] = {
        'records': records, 'watermark': watermark, 'tail_sha256': authority._evidence_hashes[watermark]
    }
    return records


def _exercise_bpf_authorization(case, expected, postgres, life, audit_owner, authority, deadline):
    variant = case.get('bpf_variant') or 'baseline'
    if variant != 'baseline':
        obs_info = authority.observer_identity(deadline)
        obs_client = bpf_authority.TerminalObserverClient(authority.boot_id, authority.epoch, obs_info, deadline)
        broker_info = authority.broker_identity(deadline)
        broker_start_ns = _broker_native_start(authority.evidence_records(), broker_info)
        broker_pidfd = os.pidfd_open(broker_info['pid'], 0)
        overflow_child = overflow_pidfd = overflow_barrier = overflow_output = None
        try:
            if select.select([broker_pidfd], [], [], 0)[0] or journal.process_start(broker_info['pid']) != broker_info['starttime']:
                raise JournalError('terminal fault held broker identity changed before ARM')
            if variant == 'guard-overflow':
                br_r, br_w = os.pipe2(os.O_CLOEXEC)
                null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
                d0 = fcntl.fcntl(null_fd, fcntl.F_DUPFD_CLOEXEC, 32)
                d8 = fcntl.fcntl(br_r, fcntl.F_DUPFD_CLOEXEC, 32)
                os.close(null_fd)
                os.close(br_r)
                actions = [(os.POSIX_SPAWN_DUP2, d0, 0), (os.POSIX_SPAWN_DUP2, d0, 1),
                           (os.POSIX_SPAWN_DUP2, d0, 2), (os.POSIX_SPAWN_DUP2, d8, 8)]
                exe = '/opt/g2/g2-bpf-authority'
                overflow_child = os.posix_spawn(exe, (exe, 'selftest', 'ring-overflow-filler'),
                                                guest_storage.ENV, file_actions=actions)
                os.close(d0)
                os.close(d8)
                overflow_pidfd = os.pidfd_open(overflow_child, 0)
                overflow_barrier = br_w
                authority.register_selftest_probe('ring-overflow-filler', overflow_child,
                                                  journal.process_start(overflow_child), overflow_pidfd, deadline=deadline)
            elif variant == 'audit-overflow':
                br_r, br_w = os.pipe2(os.O_CLOEXEC)
                out_r, out_w = os.pipe2(os.O_CLOEXEC)
                null_fd = os.open('/dev/null', os.O_RDONLY | os.O_CLOEXEC)
                d0, d8, d1 = (fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32) for fd in (null_fd, br_r, out_w))
                os.close(null_fd)
                os.close(br_r)
                os.close(out_w)
                actions = [(os.POSIX_SPAWN_DUP2, d0, 0), (os.POSIX_SPAWN_DUP2, d1, 1),
                           (os.POSIX_SPAWN_DUP2, d0, 2), (os.POSIX_SPAWN_DUP2, d8, 8)]
                exe = '/opt/g2/g2-bpf-authority'
                overflow_child = os.posix_spawn(exe, (exe, 'selftest', 'audit-overflow-bind-filler'),
                                                guest_storage.ENV, file_actions=actions)
                for fd in (d0, d8, d1):
                    os.close(fd)
                overflow_pidfd = os.pidfd_open(overflow_child, 0)
                overflow_barrier, overflow_output = br_w, out_r
                authority.register_selftest_probe('audit-overflow-bind-filler', overflow_child,
                                                  journal.process_start(overflow_child), overflow_pidfd, deadline=deadline)
            if variant in ('audit-loss', 'audit-duplicate'):
                armed = []
                def arm_registered_raw():
                    _capture_terminal_healthy(case, audit_owner, authority, deadline)
                    armed.append(obs_client.arm(variant, deadline=deadline))
                    authority.selftest_observation_fault(variant, deadline=deadline)
                _authorization_probe(case, expected, authority, deadline, 'raw-denial', before_release=arm_registered_raw)
                arm_resp = armed[0]
                if variant == 'audit-loss':
                    try:
                        _checkpoint_bound(authority, deadline)
                    except JournalError:
                        pass
            else:
                _capture_terminal_healthy(case, audit_owner, authority, deadline)
                arm_resp = obs_client.arm(variant, deadline=deadline)
            if variant in ('fd-fill-failure', 'fd-restore-failure'):
                try:
                    _authorization_fd_stage(postgres, life, audit_owner, authority, 'app', 'map', deadline)
                except Exception as exc:
                    stages = carrier.RESULT.get('bpf_fd_stages', [])
                    if not stages or stages[-1]['armed'] is not True:
                        raise
                    stage_error = {'error_type': type(exc).__name__, 'message': str(exc)[:1024]}
                    carrier.RESULT.setdefault('bpf_fd_stage_error', stage_error)
                    print('G2-BPF-FD-STAGE-ERROR ' + canonical(stage_error).decode('ascii'),
                          file=sys.stderr, flush=True)
            elif variant not in ('audit-loss', 'audit-duplicate'):
                authority.selftest_observation_fault(variant, deadline=deadline)
                if variant in ('snapshot-missing', 'snapshot-bad-field'):
                    try:
                        _run_runtime_arm_stage(postgres, life, audit_owner, authority, 'app', None, deadline)
                    except Exception:
                        # A real map publication faults the broker during this start;
                        # the observer must still independently prove the cause below.
                        pass
                if variant == 'guard-overflow' and overflow_barrier is not None:
                    if os.write(overflow_barrier, b'Z') != 1:
                        raise JournalError('guard overflow probe barrier release failed')
                    os.close(overflow_barrier)
                    overflow_barrier = None
                    _held_pidfd_exit(overflow_pidfd, deadline, 'guard overflow probe', timeout=30.0)
                    got, _ = os.waitpid(overflow_child, os.WNOHANG)
                    if got != overflow_child:
                        raise JournalError('guard overflow probe pidfd and direct wait disagree')
                    overflow_child = None
                if variant == 'audit-overflow':
                    initial, cursor_tail = obs_client.read_all(deadline=deadline)
                    cursor = len(initial)
                    phase2 = cause = False
                    observed_wires = {canonical(e['body']) for e in initial if e['kind'] == 'audit_wire'}
                    rejected_wire = None
                    for _ in range(16385):
                        if os.write(overflow_barrier, b'Z') != 1:
                            raise JournalError('audit filler permit failed')
                        raw = bytearray()
                        while True:
                            remaining = deadline - time.monotonic()
                            if remaining <= 0 or not select.select([overflow_output], [], [], remaining)[0]:
                                raise JournalError('audit filler response deadline expired')
                            ch = os.read(overflow_output, 1)
                            if not ch or len(raw) >= 1024:
                                raise JournalError('audit filler response missing or oversized')
                            if ch == b'\n':
                                break
                            raw.extend(ch)
                        frame = strict_json(bytes(raw))
                        if (type(frame) is not dict or canonical(frame) != bytes(raw) or
                                set(frame) != {'mode', 'tid', 'socket_fd', 'bind_result', 'close_result'} or
                                frame['mode'] != 'audit-overflow-bind-filler' or frame['tid'] != overflow_child or
                                type(frame['socket_fd']) is not int or frame['socket_fd'] < 0 or
                                frame['bind_result'] != 0 or frame['close_result'] != 0):
                            raise JournalError('audit filler did not complete its actual bind')
                        eoe = False
                        while not eoe or (phase2 or cause) and not (phase2 and cause and rejected_wire in observed_wires):
                            batch, cursor_tail = obs_client.read_all(deadline=deadline, after=cursor, tail=cursor_tail)
                            cursor += len(batch)
                            eoe |= any(e['kind'] == 'audit_wire' and e['body']['record_type'] == 1320 for e in batch)
                            observed_wires.update(canonical(e['body']) for e in batch if e['kind'] == 'audit_wire')
                            phase2 |= any(e['kind'] == 'guard' and e['body']['record']['kind'] == 14 and
                                          e['body']['record']['operation'] == 2 and
                                          e['body']['record']['task_cookie'] == broker_info['task_cookie'] for e in batch)
                            for event in batch:
                                if event['kind'] == 'broker_witness' and event['body'].get('detector') == 'audit_wire_capacity':
                                    body = event['body']
                                    if body.get('source') != 'broker_audit_delivery' or body.get('capacity') != 16384 or body.get('count_before') != 16384 or type(body.get('rejected_wire')) is not dict or body['rejected_wire'].get('occurrence') != 1:
                                        raise JournalError('audit capacity witness differs')
                                    cause = True
                                    rejected_wire = canonical(body['rejected_wire'])
                            if phase2 and cause and rejected_wire in observed_wires:
                                break
                            if select.select([broker_pidfd, overflow_pidfd], [], [], 0)[0]:
                                raise JournalError('audit filler or broker exited before capacity detection')
                            remaining = deadline - time.monotonic()
                            if remaining <= 0:
                                raise JournalError('audit filler did not reach observer boundary')
                            if not batch:
                                select.select([], [], [], min(0.01, remaining))
                        if phase2 and cause:
                            break
                    if not phase2 or not cause:
                        raise JournalError('audit filler did not reach the capacity detector')
                    os.close(overflow_barrier)
                    overflow_barrier = None
                    _held_pidfd_exit(overflow_pidfd, deadline, 'audit filler')
                    got, status = os.waitpid(overflow_child, os.WNOHANG)
                    if got != overflow_child or not os.WIFEXITED(status) or os.WEXITSTATUS(status) != 0:
                        raise JournalError('audit filler did not exit cleanly')
                    overflow_child = None
            detectors = {
                'snapshot-missing': ('broker_guard_copy', 'guard_snapshot_missing'),
                'snapshot-bad-field': ('broker_guard_copy', 'guard_snapshot_mismatch'),
                'audit-loss': ('broker_audit_delivery', 'audit_incomplete'),
                'audit-duplicate': ('broker_audit_delivery', 'audit_duplicate'),
                'audit-overflow': ('broker_audit_delivery', 'audit_wire_capacity'),
                'reader-death': ('broker_reader', 'reader_pidfd_exited'),
                'reader-endpoint': ('broker_reader', 'reader_endpoint_closed'),
                'reader-timeout': ('broker_reader', 'reader_deadline'),
                'reader-replay': ('broker_reader', 'reader_response_identity'),
                'reader-credential': ('broker_reader', 'reader_message_credentials'),
                'reader-extra-fd': ('broker_reader', 'reader_unexpected_rights'),
                'reader-frame': ('broker_reader', 'reader_noncanonical_frame'),
                'reader-done': ('broker_reader', 'reader_missing_done'),
                'fd-fill-failure': ('broker_fd_stage', 'fd_fill_failure'),
                'fd-restore-failure': ('broker_fd_stage', 'fd_restore_failure'),
            }
            if variant in detectors:
                while True:
                    observed, _ = obs_client.read_all(deadline=deadline)
                    interval = observed[arm_resp['begin_sequence']:]
                    phase2 = any(e['kind'] == 'guard' and e['body']['record']['kind'] == 14 and
                                 e['body']['record']['operation'] == 2 and
                                 e['body']['record']['task_cookie'] == broker_info['task_cookie'] for e in interval)
                    cause = any(e['kind'] == 'broker_witness' and e['body'].get('kind') == 'broker_receive_observation' and
                                e['body']['variant'] == variant and
                                (e['body']['source'], e['body']['detector']) == detectors[variant] for e in interval)
                    if phase2 and cause:
                        break
                    if select.select([broker_pidfd], [], [], 0)[0]:
                        raise JournalError('broker exited before the observer saw phase 2 and the fault cause')
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise JournalError('observer did not independently record phase 2 and the fault cause')
                    select.select([], [], [], min(0.05, remaining))
            try:
                signal.pidfd_send_signal(broker_pidfd, signal.SIGKILL)
            except ProcessLookupError:
                pass
            _held_pidfd_exit(broker_pidfd, deadline, 'terminal fault broker')
            fin = obs_client.finish(deadline=deadline)
            recs = _terminal_records(obs_client, fin, deadline)
            authority.close()
            broker_exit = _terminal_guard_exit(recs, 15, broker_info['task_cookie'], broker_info['pid'], broker_start_ns)
            if fin['authority_after'].get('death_sequence') != broker_exit['sequence']:
                raise JournalError('terminal fault native death status differs from guard exit')
            module_instance = _terminal_module_instance(arm_resp, fin, broker_info, obs_info, authority.epoch)
            term_obs = {
                'version': 1, 'variant': variant, 'boot_id': authority.boot_id, 'epoch': authority.epoch,
                'module_instance': module_instance,
                'observer_principal': obs_info['principal_id'],
                'authority_before': arm_resp['authority_before'],
                'authority_after': fin['authority_after'],
                'begin_sequence': arm_resp['begin_sequence'],
                'end_sequence': fin['end_sequence'],
                'terminal_sha256': fin['terminal_sha256'],
                'broker_exit': {
                    'task_cookie': broker_info['task_cookie'], 'tgid': broker_info['pid'], 'tid': broker_info['pid'],
                    'start_ns': broker_exit['task_start_ns'], 'pidfd_exited': True,
                    'guard_exit_sequence': broker_exit['sequence'], 'guard_exit_code': broker_exit['result'], 'direct_wait': None
                },
                'probe_exits': [], 'records': recs, 'losses': fin['losses'], 'outcome': fin['outcome'],
                'fault_evidence': fin['fault_evidence']
            }
            carrier.RESULT['negative'] = {'version': 1, 'scenario': 'bpf-authorization', 'variant': variant,
                                          'terminal_observation': term_obs}
            carrier.RESULT['completed'] = True
            return
        finally:
            if overflow_barrier is not None:
                os.close(overflow_barrier)
            if overflow_output is not None:
                os.close(overflow_output)
            if overflow_child is not None:
                try:
                    if overflow_pidfd is not None:
                        signal.pidfd_send_signal(overflow_pidfd, signal.SIGKILL)
                        _held_pidfd_exit(overflow_pidfd, deadline, 'guard overflow cleanup')
                    else:
                        os.kill(overflow_child, signal.SIGKILL)
                    os.waitpid(overflow_child, os.WNOHANG)
                except (OSError, JournalError):
                    pass
            if overflow_pidfd is not None:
                os.close(overflow_pidfd)
            _close_terminal_handles(broker_pidfd, obs_client)

    # Baseline 33 items
    observations = []
    generation_before = _checkpoint_bound(authority, deadline)
    generation = _run_runtime_arm_stage(postgres, life, audit_owner, authority, 'app', 'wrong-generation', deadline)
    with authority.observation(deadline):
        audit_owner.await_health(deadline)
    generation_after = generation['replay_after']
    generation_records = authority.evidence_records()
    generation_seqs = list(range(generation_before['checkpoint_sequence'] + 1, generation_after['checkpoint_sequence']))
    generation_cases = [generation_records[seq - 1]['body'] for seq in generation_seqs if
                        generation_records[seq - 1]['kind'] == 'selftest_case' and
                        generation_records[seq - 1]['body']['name'] == 'wrong-generation' and
                        generation_records[seq - 1]['body']['nonce'] == generation['nonce']]
    if [entry['phase'] for entry in generation_cases] != ['started', 'observed', 'settled']:
        raise JournalError('wrong-generation selftest did not settle')
    observations.append({'name': 'wrong-generation', 'path': 'control',
                         'actor': _observed_actor(generation_records, generation_cases[-1]['actor_cookie']),
                         'request': None, 'syscall_result': generation_cases[-1]['result'],
                         'control_error': generation_cases[-1]['error_code'], 'denial_sequence': None,
                         'audit': None, 'before': generation_before, 'after': generation_after,
                         'evidence_sequences': generation_seqs})

    def run_fd_denial(control_name, grant_id):
        before = _checkpoint_bound(authority, deadline)
        nonce = secrets.token_hex(16)
        authority.selftest_baseline_step(control_name, nonce, None, deadline=deadline)
        if control_name == 'fd-arm-wrong-role':
            authority.selftest_fd_wrong_role(grant_id, secrets.token_hex(16), deadline)
        else:
            authority.expect_error_rpc('SELFTEST_FD_ARM', {'nonce': secrets.token_hex(16),
                                       'grant_id': grant_id, 'class': 'map'}, deadline=deadline)
        after = _checkpoint_bound(authority, deadline)
        control_records = authority.evidence_records()
        seqs = list(range(before['checkpoint_sequence'] + 1, after['checkpoint_sequence']))
        cases = [control_records[seq - 1]['body'] for seq in seqs if
                 control_records[seq - 1]['kind'] == 'selftest_case' and
                 control_records[seq - 1]['body']['name'] == control_name and
                 control_records[seq - 1]['body']['nonce'] == nonce]
        if [entry['phase'] for entry in cases] != ['started', 'observed', 'settled']:
            raise JournalError('FD denial case did not settle')
        observations.append({'name': control_name, 'path': 'control',
                             'actor': _observed_actor(control_records, cases[-1]['actor_cookie']),
                             'request': None, 'syscall_result': cases[-1]['result'],
                             'control_error': cases[-1]['error_code'], 'denial_sequence': None,
                             'audit': None, 'before': before, 'after': after, 'evidence_sequences': seqs})

    run_fd_denial('fd-arm-wrong-grant', generation['grant_id'])
    b_map_before = _checkpoint_bound(authority, deadline)
    _authorization_fd_stage(postgres, life, audit_owner, authority, 'app', 'map', deadline)
    b_map_after = _checkpoint_bound(authority, deadline)
    map_seqs = list(range(b_map_before['checkpoint_sequence'] + 1, b_map_after['checkpoint_sequence']))

    b_prog_before = _checkpoint_bound(authority, deadline)
    _authorization_fd_stage(postgres, life, audit_owner, authority, 'w1', 'program', deadline)
    b_prog_after = _checkpoint_bound(authority, deadline)
    prog_seqs = list(range(b_prog_before['checkpoint_sequence'] + 1, b_prog_after['checkpoint_sequence']))

    b_raw_before = _checkpoint_bound(authority, deadline)
    raw_ev = _authorization_probe(case, expected, authority, deadline, 'raw-denial')
    with authority.observation(deadline):
        audit_owner.await_health(deadline)
    b_raw_after = _checkpoint_bound(authority, deadline)
    raw_seqs = list(range(b_raw_before['checkpoint_sequence'] + 1, b_raw_after['checkpoint_sequence']))

    for asm_mode in ('fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse'):
        b_asm_before = _checkpoint_bound(authority, deadline)
        asm_result = _run_assembly_probe(authority, asm_mode, deadline)
        with authority.observation(deadline):
            audit_owner.await_health(deadline)
        b_asm_after = _checkpoint_bound(authority, deadline)
        asm_seqs = list(range(b_asm_before['checkpoint_sequence'] + 1, b_asm_after['checkpoint_sequence']))
        records = authority.evidence_records()
        imports = [records[seq - 1]['body'] for seq in asm_seqs if records[seq - 1]['kind'] == 'probe_import' and
                   records[seq - 1]['body']['notifying_cookie'] is not None]
        if len(imports) != 1:
            raise JournalError('assembly case lacks its actual notifying task import')
        asm_actor = _observed_actor(records, imports[0]['notifying_cookie'])
        final = asm_result['frames'][-1]
        notifying_tid = final['clone_result'] if asm_mode == 'tid-reuse' else final['tid']
        if asm_actor['tid'] != notifying_tid:
            raise JournalError('assembly notifying actor differs from the child result')
        observations.append({
            'name': asm_mode, 'path': 'lifecycle',
            'actor': asm_actor,
            'request': None, 'syscall_result': final['bpf_result'], 'control_error': None, 'denial_sequence': None,
            'audit': None, 'before': b_asm_before, 'after': b_asm_after, 'evidence_sequences': asm_seqs,
            'assembly': {'probe_cookie': asm_result['probe_cookie'], 'pid': asm_result['pid'],
                         'start_ticks': asm_result['start_ticks'], 'wait_pid': asm_result['wait_pid'],
                         'wait_status': asm_result['status'], 'frames': asm_result['frames']}
        })

    for idx, rt_mode in enumerate(('nested-buffer-race', 'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel')):
        svc = ('w2', 'control', 'app', 'w1')[idx]
        b_rt_before = _checkpoint_bound(authority, deadline)
        _run_runtime_arm_stage(postgres, life, audit_owner, authority, svc, rt_mode, deadline)
        with authority.observation(deadline):
            audit_owner.await_health(deadline)
        b_rt_after = _checkpoint_bound(authority, deadline)
        rt_seqs = list(range(b_rt_before['checkpoint_sequence'] + 1, b_rt_after['checkpoint_sequence']))
        records = authority.evidence_records()
        stages = [records[seq - 1]['body'] for seq in rt_seqs if records[seq - 1]['kind'] == 'selftest_runtime_stage' and
                  records[seq - 1]['body']['mode'] == rt_mode and records[seq - 1]['body']['notifying_cookie'] is not None]
        cookies = {stage['notifying_cookie'] for stage in stages}
        if len(cookies) != 1:
            raise JournalError('runtime case lacks its actual notifying task')
        rt_actor = _observed_actor(records, next(iter(cookies)))
        observations.append({
            'name': rt_mode, 'path': 'lifecycle',
            'actor': rt_actor,
            'request': None, 'syscall_result': None,
            'control_error': None, 'denial_sequence': None,
            'audit': None, 'before': b_rt_before, 'after': b_rt_after, 'evidence_sequences': rt_seqs
        })

    for feature_name in ('feature-map', 'extended-info-ebadf', 'replace-probe-ebadf',
                         'allowed-device-start', 'cgroup-auto-retire', 'fd-arm-replay',
                         'wrong-program-byte', 'wrong-flags', 'wrong-target',
                         'attachment-replace', 'failure-cleanup', 'extra-fd-delays-removal'):
        feature_before = _checkpoint_bound(authority, deadline)
        feature = _run_runtime_arm_stage(postgres, life, audit_owner, authority, 'app', feature_name, deadline)
        with authority.observation(deadline):
            audit_owner.await_health(deadline)
        feature_after = feature['replay_after'] if feature_name in ('fd-arm-replay', 'extra-fd-delays-removal', 'wrong-generation') else _checkpoint_bound(authority, deadline)
        feature_records = authority.evidence_records()
        feature_sequences = list(range(feature_before['checkpoint_sequence'] + 1, feature_after['checkpoint_sequence']))
        feature_cases = [feature_records[seq - 1]['body'] for seq in feature_sequences if
                         feature_records[seq - 1]['kind'] == 'selftest_case' and
                         feature_records[seq - 1]['body']['name'] == feature_name and
                         feature_records[seq - 1]['body']['nonce'] == feature['nonce']]
        if [entry['phase'] for entry in feature_cases] != ['started', 'observed', 'settled']:
            raise JournalError('compatibility selftest did not settle')
        path = 'control' if feature_name in ('wrong-generation', 'fd-arm-replay', 'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second') else 'lifecycle'
        request = denial_sequence = None
        if feature_name in ('wrong-program-byte', 'wrong-flags', 'wrong-target'):
            stages = [feature_records[seq - 1]['body'] for seq in feature_sequences if
                      feature_records[seq - 1]['kind'] == 'selftest_runtime_stage' and
                      feature_records[seq - 1]['body']['mode'] == feature_name and
                      feature_records[seq - 1]['body']['nonce'] == feature['nonce'] and
                      feature_records[seq - 1]['body']['phase'] == 'mutated']
            if len(stages) != 1:
                raise JournalError('mutation case lacks the actual input capture')
            stage = stages[0]
            denials = [seq for seq in feature_sequences if feature_records[seq - 1]['kind'] == 'notification_denial' and
                       feature_records[seq - 1]['body']['invocation_id'] == stage['invocation_id'] and
                       feature_records[seq - 1]['body']['notification_id'] == stage['notification_id']]
            if len(denials) != 1:
                raise JournalError('mutation case lacks its actual notification denial')
            mutation = stage['mutation']
            path, denial_sequence = 'user_notification', denials[0]
            request = {'invocation_id': stage['invocation_id'], 'notification_id': stage['notification_id'],
                       'command': 'PROG_LOAD' if feature_name == 'wrong-program-byte' else 'PROG_ATTACH',
                       'attributes': {'attr_size': mutation['attr_size'], 'attr_hex': mutation['attr_hex']},
                       'program_bytes': mutation['program_bytes'], 'license': mutation['license'], 'input_objects': None}
        observation = {
            'name': feature_name, 'path': path,
            'actor': _observed_actor(feature_records, feature_cases[-1]['actor_cookie']),
            'request': request, 'syscall_result': feature_cases[-1]['result'],
            'control_error': feature_cases[-1]['error_code'], 'denial_sequence': denial_sequence,
            'audit': None, 'before': feature_before, 'after': feature_after,
            'evidence_sequences': feature_sequences,
        }
        if feature_name == 'extra-fd-delays-removal':
            observation['extra_fd'] = feature['extra_fd']
        observations.append(observation)

    container_before = _checkpoint_bound(authority, deadline)
    container_nonce = _run_container_bpf_case(postgres, authority, deadline)
    with authority.observation(deadline):
        audit_owner.await_health(deadline)
    container_after = _checkpoint_bound(authority, deadline)
    container_records = authority.evidence_records()
    container_sequences = list(range(container_before['checkpoint_sequence'] + 1, container_after['checkpoint_sequence']))
    container_cases = [container_records[seq - 1]['body'] for seq in container_sequences if
                       container_records[seq - 1]['kind'] == 'selftest_case' and
                       container_records[seq - 1]['body']['name'] == 'container-bpf' and
                       container_records[seq - 1]['body']['nonce'] == container_nonce]
    if [entry['phase'] for entry in container_cases] != ['started', 'observed', 'settled']:
        raise JournalError('container BPF case did not settle')
    observations.append({'name': 'container-bpf', 'path': 'lifecycle',
                         'actor': _observed_actor(container_records, container_cases[-1]['actor_cookie']),
                         'request': None, 'syscall_result': container_cases[-1]['result'],
                         'control_error': container_cases[-1]['error_code'], 'denial_sequence': None,
                         'audit': None, 'before': container_before, 'after': container_after,
                         'evidence_sequences': container_sequences})

    retired_stages = carrier.RESULT.get('bpf_fd_stages', [])
    if not retired_stages or type(retired_stages[0]['grant_id']) is not int:
        raise JournalError('FD denial cases lack a previously settled grant')
    retired_grant = retired_stages[0]['grant_id']
    for control_name in ('fd-arm-wrong-role', 'fd-arm-second'):
        run_fd_denial(control_name, retired_grant)

    records = authority.evidence_records()
    for object_class, before, after, sequences in (
            ('map', b_map_before, b_map_after, map_seqs),
            ('program', b_prog_before, b_prog_after, prog_seqs)):
        stages = [records[seq - 1]['body'] for seq in sequences if records[seq - 1]['kind'] == 'selftest_fd_stage' and
                  records[seq - 1]['body']['class'] == object_class and records[seq - 1]['body']['phase'] == 'entered']
        if len(stages) != 1:
            raise JournalError('FD selftest lacks its actual entered transaction')
        txns = [records[seq - 1]['body'] for seq in sequences if records[seq - 1]['kind'] == 'transaction' and
                records[seq - 1]['body']['id'] == stages[0]['transaction_id'] and records[seq - 1]['body']['state'] == 'retired']
        if len(txns) != 1 or txns[0]['requester_cookie'] is None:
            raise JournalError('FD selftest lacks its actual requester and result')
        observations.append({
            'name': 'fd-arm-' + object_class, 'path': 'fd_stage',
            'actor': _observed_actor(records, txns[0]['requester_cookie']),
            'request': None, 'syscall_result': txns[0]['result'], 'control_error': None, 'denial_sequence': None,
            'audit': None, 'before': before, 'after': after, 'evidence_sequences': sequences,
        })
    raw_actor, raw_request, raw_stamp, raw_result = _wrong_actor_observation(
        authority.evidence_records(), raw_seqs, raw_ev, authority.child_cookie)
    observations.append({
        'name': 'wrong-actor', 'path': 'raw_syscall',
        'actor': raw_actor, 'request': raw_request, 'syscall_result': raw_result,
        'control_error': None, 'denial_sequence': None, 'audit': raw_stamp,
        'before': b_raw_before, 'after': b_raw_after, 'evidence_sequences': raw_seqs
    })

    for name in ('audit-interleaved', 'seal-detach-denied', 'seal-unload-denied', 'id-reuse'):
        before = _checkpoint_bound(authority, deadline)
        nonce = secrets.token_hex(16)
        if name != 'seal-detach-denied':
            authority.selftest_baseline_step(name, nonce, None, deadline=deadline)
        if name in ('seal-detach-denied', 'seal-unload-denied'):
            _run_seal_probe(authority, name, nonce, deadline)
        elif name == 'audit-interleaved':
            _run_interleaved_probes(authority, deadline)
        while True:
            records = authority._complete_evidence(deadline)
            phases = [e['body'] for e in records[before['checkpoint_sequence']:] if e['kind'] == 'selftest_case' and
                      e['body']['name'] == name and e['body']['nonce'] == nonce]
            if phases and phases[-1]['phase'] == 'settled':
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise JournalError('baseline selftest case did not settle')
            select.select([], [], [], min(0.05, remaining))
        if [phase['phase'] for phase in phases] != ['started', 'observed', 'settled']:
            raise JournalError('baseline selftest case phases differ')
        with authority.observation(deadline):
            audit_owner.await_health(deadline)
        after = _checkpoint_bound(authority, deadline)
        records = authority.evidence_records()
        actor = _observed_actor(records, phases[-1]['actor_cookie'])
        observations.append({
            'name': name, 'path': 'lifecycle', 'actor': actor, 'request': None,
            'syscall_result': phases[-1]['result'], 'control_error': phases[-1]['error_code'],
            'denial_sequence': None, 'audit': None, 'before': before, 'after': after,
            'evidence_sequences': list(range(before['checkpoint_sequence'] + 1, after['checkpoint_sequence']))
        })

    required = {
        'allowed-device-start', 'wrong-actor', 'wrong-program-byte', 'wrong-flags', 'wrong-target', 'wrong-generation',
        'nested-buffer-race', 'fd-slot-reuse', 'nonleader-files', 'shared-files', 'leader-exit', 'tid-reuse',
        'feature-map', 'extended-info-ebadf', 'replace-probe-ebadf', 'audit-interleaved', 'container-bpf',
        'commit-before-cancel', 'after-effect-cancel', 'after-reply-cancel', 'seal-detach-denied',
        'seal-unload-denied', 'attachment-replace', 'failure-cleanup', 'cgroup-auto-retire',
        'extra-fd-delays-removal', 'id-reuse', 'fd-arm-map', 'fd-arm-program', 'fd-arm-replay',
        'fd-arm-wrong-role', 'fd-arm-wrong-grant', 'fd-arm-second',
    }
    if {item['name'] for item in observations} != required:
        raise JournalError('baseline authorization actions did not produce all independent observations')
    carrier.RESULT['negative'] = {'version': 1, 'scenario': 'bpf-authorization', 'variant': 'baseline',
                                  'observations': observations}
    carrier.RESULT['root_journal'] = _export(life.journal, authority, deadline)
    carrier.RESULT['completed'] = True


def _exercise_bpf_broker_kill(case, expected, audit_owner, authority, deadline):
    with authority.observation(deadline):
        audit_owner.await_health(deadline)
    obs_info = authority.observer_identity(deadline)
    broker_info = authority.broker_identity(deadline)
    broker_start_ns = _broker_native_start(authority.evidence_records(), broker_info)
    obs_client = bpf_authority.TerminalObserverClient(authority.boot_id, authority.epoch, obs_info, deadline)
    broker_pidfd = os.pidfd_open(broker_info['pid'], 0)
    opened, duplicated = [], []
    child = pidfd = None
    child_wait = {'waited': False, 'status': None}
    try:
        if select.select([broker_pidfd], [], [], 0)[0] or journal.process_start(broker_info['pid']) != broker_info['starttime']:
            raise JournalError('broker-kill held broker identity changed before ARM')
        if (guest_bootstrap._digest('/run/g2/source_guest.py') != expected['source_guest.py'] or
                guest_bootstrap._digest('/opt/g2/g2-bpf-authority') != expected['g2-bpf-authority']):
            raise JournalError('broker-kill probe executable or parent script differs')
        # Pre-register post-death-raw child stopped on FD 8 barrier before broker revoke/death
        seed = os.open('/mnt/g2-seed', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(seed)
        case_fd = os.open('case.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=seed)
        opened.append(case_fd)
        script = os.open('/run/g2/source_guest.py', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(script)
        parent_pidfd = os.pidfd_open(os.getpid(), 0)
        opened.append(parent_pidfd)
        parent_exe = os.open('/proc/self/exe', os.O_RDONLY | os.O_CLOEXEC)
        opened.append(parent_exe)
        null_fd = os.open('/dev/null', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        opened.append(null_fd)
        stdout_r, stdout_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((stdout_r, stdout_w))
        stderr_r, stderr_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((stderr_r, stderr_w))
        barrier_r, barrier_w = os.pipe2(os.O_CLOEXEC)
        opened.extend((barrier_r, barrier_w))
        for fd in (seed, case_fd, script, parent_pidfd, parent_exe, null_fd, stdout_w, stderr_w, barrier_r):
            duplicated.append(fcntl.fcntl(fd, fcntl.F_DUPFD_CLOEXEC, 32))
        actions = [(os.POSIX_SPAWN_DUP2, duplicated[5], 0),
                   (os.POSIX_SPAWN_DUP2, duplicated[6], 1),
                   (os.POSIX_SPAWN_DUP2, duplicated[7], 2),
                   (os.POSIX_SPAWN_DUP2, duplicated[8], 8)]
        actions.extend((os.POSIX_SPAWN_DUP2, duplicated[index], index + 3) for index in range(5))
        executable = '/opt/g2/g2-bpf-authority'
        child = os.posix_spawn(executable, (executable, 'selftest', 'post-death-raw'),
                               guest_storage.ENV, file_actions=actions)
        pidfd = os.pidfd_open(child, 0)
        opened.append(pidfd)
        child_start = journal.process_start(child)
        for fd in (stdout_w, stderr_w, barrier_r):
            os.close(fd)
            opened.remove(fd)
        reg = authority.register_selftest_probe('post-death-raw', child, child_start, pidfd, deadline=deadline)
        healthy_records = _capture_terminal_healthy(case, audit_owner, authority, deadline)
        probe_actor, probe_watch = _registered_probe_actor(healthy_records, 'post-death-raw', reg['probe_cookie'],
                                                          child, child_start, authority.child_cookie)
        arm_resp = obs_client.arm('bpf-broker-kill', deadline=deadline)
        nonce = secrets.token_hex(16)
        authority.selftest_revoke_and_close(nonce, deadline=deadline)
        authority.close()
        # The same held main task must complete the phase-2 cleanup and all
        # phase-0 denials before it is killed, not merely reach phase 0.
        wait_until = min(deadline, time.monotonic() + 10)
        broker_native_exe = None
        while time.monotonic() < wait_until:
            recs, _ = obs_client.read_all(deadline=wait_until)
            if select.select([broker_pidfd], [], [], 0)[0]:
                raise JournalError('broker exited before phase-2 cleanup and phase-0 denials were observed')
            broker_native_exe = _broker_revocation_records(recs, arm_resp['begin_sequence'], broker_info, broker_start_ns)
            if broker_native_exe is not None:
                break
            remaining = wait_until - time.monotonic()
            if remaining > 0 and select.select([broker_pidfd], [], [], min(0.05, remaining))[0]:
                raise JournalError('broker exited before phase-2 cleanup and phase-0 denials were observed')
        if broker_native_exe is None:
            raise JournalError('broker phase-2 denials/cleanup or phase-0 postclose denials missing before deadline')
        signal.pidfd_send_signal(broker_pidfd, signal.SIGKILL)
        _held_pidfd_exit(broker_pidfd, deadline, 'broker-kill broker')
        if os.write(barrier_w, b'Z') != 1:
            raise JournalError('post-death probe barrier release failed')
        os.close(barrier_w)
        opened.remove(barrier_w)
        raw_out, raw_err = _collect_terminal_child(child, pidfd, stdout_r, stderr_r, deadline, child_wait)
        child_status = child_wait['status']
        report = strict_json(raw_out)
        report_actor = report.get('actor') if type(report) is dict else None
        attempts = report.get('attempts') if type(report) is dict else None
        if (type(report) is not dict or set(report) != {'version', 'case', 'actor', 'attempts'} or
                report['version'] != 1 or report['case'] != 'bpf-authorization' or
                raw_out != canonical(report) + b'\n' or raw_err or
                report_actor != {'tgid': child, 'tid': child, 'start_ticks': child_start, 'parent_tgid': os.getpid()} or
                not os.WIFEXITED(child_status) or os.WEXITSTATUS(child_status) != 0 or
                type(attempts) is not list or len(attempts) != 2):
            raise JournalError('post-death probe actual report or direct exit differs')
        _validate_authorization_attempts(attempts)
        wait_until = min(deadline, time.monotonic() + 10)
        observed = False
        while time.monotonic() < wait_until:
            recs, _ = obs_client.read_all(deadline=wait_until)
            guards = [r['body']['record'] for r in recs if r['kind'] == 'guard']
            deaths = [r for r in guards if r['kind'] == 15 and r['task_cookie'] == broker_info['task_cookie']]
            exits = [r for r in guards if r['kind'] == 11 and r['task_cookie'] == probe_actor['task_cookie']]
            raw = [r for r in guards if r['kind'] == 13 and r['task_cookie'] == probe_actor['task_cookie']]
            if len(deaths) == 1 and len(exits) == 1 and len(raw) == 2:
                observed = True
                break
            remaining = wait_until - time.monotonic()
            if remaining > 0:
                select.select([], [], [], min(0.05, remaining))
        if not observed:
            raise JournalError('post-death probe native denials or task exits missing before deadline')
        fin = obs_client.finish(deadline=deadline)
        recs = _terminal_records(obs_client, fin, deadline)
        broker_exit = _terminal_guard_exit(recs, 15, broker_info['task_cookie'], broker_info['pid'], broker_start_ns,
                                           exe_identity=broker_native_exe)
        probe_exe = (probe_watch['detail']['exe_dev'], probe_watch['detail']['exe_ino'])
        probe_exit = _terminal_guard_exit(recs, 11, probe_actor['task_cookie'], child, probe_actor['start_ns'],
                                         authority.child_cookie, child_status, probe_exe)
        if fin['authority_after'].get('death_sequence') != broker_exit['sequence']:
            raise JournalError('broker-kill native death status differs from guard exit')
        raw = [r['body']['record'] for r in recs if r['kind'] == 'guard' and
               r['body']['record']['kind'] == 13 and r['body']['record']['task_cookie'] == probe_actor['task_cookie']]
        if (len(raw) != 2 or [(r['operation'], r['result']) for r in raw] != [(0, -1), (5, -1)] or
                any(r['task_start_ns'] != probe_actor['start_ns'] or
                    (r['detail']['tgid'], r['detail']['tid']) != (child, child) or
                    (r['detail']['exe_dev'], r['detail']['exe_ino']) != probe_exe or
                    not broker_exit['sequence'] < r['sequence'] < probe_exit['sequence'] for r in raw)):
            raise JournalError('post-death probe native denial identity, result, or ordering differs')
        module_instance = _terminal_module_instance(arm_resp, fin, broker_info, obs_info, authority.epoch)
        term_obs = {
            'version': 1, 'variant': 'bpf-broker-kill', 'boot_id': authority.boot_id, 'epoch': authority.epoch,
            'module_instance': module_instance, 'observer_principal': obs_info['principal_id'],
            'authority_before': arm_resp['authority_before'], 'authority_after': fin['authority_after'],
            'begin_sequence': arm_resp['begin_sequence'], 'end_sequence': fin['end_sequence'],
            'terminal_sha256': fin['terminal_sha256'],
            'broker_exit': {
                'task_cookie': broker_info['task_cookie'], 'tgid': broker_info['pid'], 'tid': broker_info['pid'],
                'start_ns': broker_exit['task_start_ns'], 'pidfd_exited': True,
                'guard_exit_sequence': broker_exit['sequence'], 'guard_exit_code': broker_exit['result'], 'direct_wait': None
            },
            'probe_exits': [{
                'task_cookie': probe_actor['task_cookie'], 'tgid': child, 'tid': child,
                'start_ns': probe_exit['task_start_ns'], 'pidfd_exited': True,
                'guard_exit_sequence': probe_exit['sequence'], 'guard_exit_code': probe_exit['detail']['exit_code'],
                'direct_wait': {'waiter_cookie': authority.child_cookie, 'wait_status': child_status}
            }],
            'records': recs, 'losses': fin['losses'], 'outcome': fin['outcome'], 'fault_evidence': fin['fault_evidence']
        }
        carrier.RESULT['negative'] = {'version': 1, 'scenario': 'bpf-broker-kill',
                                      'terminal_observation': term_obs, 'post_death_probe': report}
        carrier.RESULT['completed'] = True
    finally:
        if child is not None and not child_wait['waited']:
            try:
                if pidfd is not None:
                    signal.pidfd_send_signal(pidfd, signal.SIGKILL)
                    _held_pidfd_exit(pidfd, deadline, 'post-death probe cleanup')
                else:
                    os.kill(child, signal.SIGKILL)
                os.waitpid(child, os.WNOHANG)
            except (OSError, JournalError):
                pass
        for fd in duplicated + opened:
            try:
                os.close(fd)
            except OSError:
                pass
        _close_terminal_handles(broker_pidfd, obs_client)



def run():
    case, expected = guest_bootstrap.verify_seed()
    deadline = GUEST_BPF_DEADLINE_MONO
    authority = bpf_authority.BPFAuthority.from_launcher(
        launcher_sha256=expected['g2-bpf-authority'], broker_sha256=expected['g2-bpf-authority'],
        run_token=RUN_TOKEN, deadline=deadline, fd=3)
    carrier.RESULT = {'version': 1, 'run_token': RUN_TOKEN, 'case': case['case'], 'scenario': case['scenario'],
                      'completed': False, 'product_gate': 'NOT_READY', 'permanent_deletion_http': 'CLOSED',
                      'isolation': {}, 'steps': []}
    carrier.initialize_result_port()
    install = guest_bootstrap.install(expected)
    installation = {'version': 1, 'guest_sha256': expected['source_guest.py'],
                    'supervisor_manifest_sha256': expected['supervisor-manifest.json'],
                    'seed_manifest_sha256': guest_bootstrap._digest('/mnt/g2-seed/SHA256SUMS'),
                    'payload_case_sha256': case['payload_case_sha256'],
                    'seed_descriptor_sha256': case['seed_descriptor_sha256']}
    carrier.RESULT['isolation']['preparer_install'] = installation
    carrier.emit_installation(installation, case['scenario'], time.monotonic() + 30)
    carrier.emit_protocol('G2LAB-READY ' + RUN_TOKEN, time.monotonic() + 30)
    roots = journal_owner = policy = audit_owner = life = daemon_pidfd = None
    variant = case.get('bpf_variant') or 'baseline'
    terminal_attempt = (case['case'] == 'realm-negative' and
                        (case['scenario'] == 'bpf-broker-kill' or
                         (case['scenario'] == 'bpf-authorization' and variant != 'baseline')))
    standalone_terminal = (terminal_attempt and
                           (case['scenario'] == 'bpf-broker-kill' or
                            variant in ('guard-overflow', 'guard-miss', 'guard-pairing')))
    stage = 'guest-preflight'
    try:
        hardware = guest_storage.verify_guest()
        carrier.RESULT['isolation']['hardware'] = hardware
        blank = guest_storage.inspect_blank_disks()
        volumes = guest_storage.prepare_volumes(blank)
        roots = census.RootSet.create()
        daemon, daemon_pidfd, daemon_info = guest_bootstrap.install_docker(
            expected, authority, observe_owner_start=case['case'] == 'realm-negative' and case['scenario'] == 'bpf-broker-kill')
        stage = 'pregenesis-systemd'
        if case['case'] == 'realm-negative' and case['scenario'] == 'observer-capability':
            with tempfile.TemporaryDirectory(dir='/run', prefix='g2-systemd-smoke-') as scratch:
                result = guest_storage.command(['/usr/bin/python3', '-B', '/opt/g2/realm-supervisor/smoke.py',
                                                '--case', 'systemd-failure', '--scratch', scratch], timeout=30)
                if result.returncode != 0 or result.stdout != b'PASS systemd-failure\n' or result.stderr:
                    raise JournalError('systemd failure smoke failed')
            _network_window_smoke()
        else:
            kernel_config = audit.capture_kernel_config()
        systemd_state = systemd_realm.seal()
        stage = 'pregenesis-dns'
        install['dns_preflight'] = dns.preflight(daemon_info, expected, authority)
        carrier.RESULT['isolation']['dns_preflight'] = install['dns_preflight']
        if case['case'] == 'realm-negative' and case['scenario'] == 'observer-capability':
            if case.get('bpf_variant') == 'fd-arm-wrong-case':
                stage = 'fd-arm-wrong-case'
                self_pidfd = os.pidfd_open(os.getpid(), 0)
                try:
                    authority.register_realm(os.getpid(), journal.process_start(os.getpid()), case['scenario'], self_pidfd)
                finally:
                    os.close(self_pidfd)
                audit_claim = audit.AuditStream(lambda _: None, audit.capture_kernel_config(), authority, deadline)
                try:
                    with authority.observation(deadline):
                        audit_claim.activate()
                    b_before = _checkpoint_bound(authority, deadline)
                    authority.expect_error_rpc('SELFTEST_FD_ARM', {'nonce': secrets.token_hex(16), 'grant_id': 1, 'class': 'map'},
                                               'BPF_AUTH_DENIED', deadline=deadline)
                    b_after = _checkpoint_bound(authority, deadline)
                finally:
                    audit_claim.close()
                carrier.RESULT['bpf_bootstrap'] = authority.bootstrap_evidence(case, deadline)
                carrier.RESULT['negative'] = {
                    'version': 1, 'scenario': 'observer-capability', 'variant': 'fd-arm-wrong-case',
                    'observation': {
                        'name': 'fd-arm-wrong-case', 'path': 'control',
                        'actor': {'tgid': os.getpid(), 'tid': os.getpid(), 'start_ticks': journal.process_start(os.getpid()),
                                  'task_cookie': authority.child_cookie, 'start_ns': journal.process_start(os.getpid()) * 10000000},
                        'request': None, 'syscall_result': None, 'control_error': 'BPF_AUTH_DENIED',
                        'denial_sequence': None, 'audit': None, 'before': b_before, 'after': b_after,
                        'evidence_sequences': list(range(b_before['checkpoint_sequence'] + 1, b_after['checkpoint_sequence']))
                    }
                }
                carrier.RESULT['completed'] = True
                return
            stage = 'observer-capability'
            scratch = '/run/g2/observer-probes'
            os.mkdir(scratch, 0o700)
            try:
                import smoke
                smoke.maintenance_protocol()
                smoke.maintenance_records(scratch)
                smoke.maintenance_witness(scratch)
                observations = [callback_lifecycle._own_process_probe(scratch, mode, scenario)
                                for mode in ('single', 'two-thread') for scenario in
                                ('normal', 'child-kill', 'coordinator-kill', 'signal', 'group-stop',
                                 'already-traced', 'cancel', 'deadline', 'observer-loss')]
            finally:
                os.rmdir(scratch)
            carrier.RESULT['negative'] = {'observations': observations, 'systemd': systemd_state,
                                          'maintenance_smoke': ['maintenance-protocol', 'maintenance-records', 'maintenance-witness']}
            carrier.RESULT['bpf_bootstrap'] = authority.bootstrap_evidence(case, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['scenario'] in ('root-restart', 'root-kill'):
            stage = 'root-supervisor-death'
            seed = dict(expected, SHA256SUMS=installation['seed_manifest_sha256'])
            evidence, negative = restart.exercise(case, expected, seed, volumes, roots, install, daemon_info,
                                                  systemd_state, _seed_realms, _export, kernel_config, authority, deadline)
            carrier.RESULT['genesis'] = evidence['genesis']
            carrier.RESULT['realm'] = {'realm_digest': evidence['realm_digest'], 'app_binding': evidence['app_binding'],
                                       'hardware': hardware}
            carrier.RESULT['source'] = evidence['source']
            carrier.RESULT['resolver_inventory'] = evidence['resolvers']
            carrier.RESULT['resolver_inventory_final'] = evidence['final_resolvers']
            carrier.RESULT['resolver_transition'] = evidence['resolver_transition']
            carrier.RESULT['root_journal'] = evidence['root_journal']
            carrier.RESULT['bpf_authority'] = evidence['bpf_authority']
            carrier.RESULT['steps'].append({'name': 'postgres-initialize', 'processes': evidence['postgres_processes']})
            carrier.RESULT['negative'] = negative
            carrier.RESULT['completed'] = True
            return
        self_pidfd = os.pidfd_open(os.getpid(), 0)
        try:
            authority.register_realm(os.getpid(), journal.process_start(os.getpid()), case['scenario'], self_pidfd)
        finally:
            os.close(self_pidfd)
        if standalone_terminal:
            stage = case['scenario']
            audit_owner = audit.AuditStream(lambda _: None, kernel_config, authority, deadline)
            with authority.observation(deadline):
                audit_owner.activate()
            if case['scenario'] == 'bpf-broker-kill':
                _exercise_bpf_broker_kill(case, expected, audit_owner, authority, deadline)
            else:
                _exercise_bpf_authorization(case, expected, None, None, audit_owner, authority, deadline)
            return
        policy = origins.ProcessOrigins(authority, case)
        audit_owner = audit.AuditStream(policy.audit_event, kernel_config, authority, deadline)
        journal_owner = journal.RealmJournal.create()
        if case['scenario'] == 'genesis-missing':
            try:
                journal_owner.realm_digest()
            except (JournalError, FileNotFoundError):
                carrier.RESULT['bpf_bootstrap'] = authority.bootstrap_evidence(case, deadline)
                carrier.RESULT['negative'] = {'genesis_absent': True, 'realm_digest_unavailable': True}
                carrier.RESULT['completed'] = True
                return
            raise JournalError('missing genesis was accepted')
        seed = dict(expected, SHA256SUMS=installation['seed_manifest_sha256'])
        if case['scenario'] != 'late-genesis':
            facts = genesis.publish(journal_owner, case, seed, volumes, roots, policy, install, daemon_info, systemd_state, authority)
            carrier.RESULT['genesis'] = facts
        else:
            carrier.RESULT['bpf_bootstrap'] = authority.bootstrap_evidence(case, deadline)
        life = lifecycle.RealmLifecycle(journal_owner, roots, volumes, daemon_info['postgres_uid'],
                                        daemon_info['postgres_gid'], systemd_state, policy, install, authority)
        life.bind_audit(audit_owner)
        with authority.observation(deadline):
            audit_owner.activate()
        postgres = guest_postgres.PostgresRealm(daemon_info, volumes, daemon_info['postgres_uid'],
                                                 daemon_info['postgres_gid'], life, authority)
        life.bind_postgres(postgres)
        stage = 'postgres-initialize'
        postgres.create_networks()
        if case['case'] == 'realm-negative' and case['scenario'] == 'bpf-authorization':
            stage = 'bpf-authorization'
            _exercise_bpf_authorization(case, expected, postgres, life, audit_owner, authority, deadline)
            return
        pg_processes = postgres.initialize()
        carrier.RESULT['steps'].append({'name': 'postgres-initialize', 'processes': pg_processes})
        carrier.RESULT['resolver_inventory'] = dict(life.resolvers)
        if case['scenario'] == 'late-genesis':
            try:
                genesis.publish(journal_owner, case, seed, volumes, roots, policy, install, daemon_info, systemd_state, authority)
            except JournalError:
                life.invalidate('late_genesis')
                carrier.RESULT['negative'] = {'actual_initdb_completed': True, 'late_genesis_rejected': True,
                                              'realm_digest_unavailable': True}
                carrier.RESULT['completed'] = True
                return
            raise JournalError('late genesis was accepted')
        fixture_case = case['case'] if case['case'] != 'realm-negative' else 'source-vps'
        stage = 'domain-and-app-initialize'
        intersecting = case['case'] == 'realm-negative' and case['scenario'] == 'intersecting-source-closure'
        runner, domains, credentials, app, app_secrets, binding, realm_digest = _seed_realms(
            life, roots, journal_owner, postgres, expected, fixture_case, intersecting,
            realm_negative=case['case'] == 'realm-negative')
        carrier.RESULT['realm'] = {'realm_digest': realm_digest, 'app_binding': binding, 'hardware': hardware}
        carrier.RESULT['fixture'] = {'actor_user_id': app['actor_user_id'], 'records': app['records']}
        stage = 'scenario'
        if case['case'] == 'realm-negative' and case['scenario'] == 'extra-browser':
            carrier.RESULT['negative'] = browser_process.exercise(life, postgres)
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] in ('extra-http', 'extra-endpoint'):
            carrier.RESULT['negative'] = listener_negative.exercise(life, postgres, case['scenario'])
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] == 'other-record-comparison':
            carrier.RESULT['negative'] = comparison_fixture.exercise(life, runner, postgres, app, app_secrets)
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] in ('staging-backup', 'published-backup', 'extra-backup-producer'):
            mode = 'published' if case['scenario'] == 'published-backup' else 'staging'
            carrier.RESULT['negative'] = physical_backup.backup_negative(
                life, roots, postgres, runner, POSTGRES_HOSTS['app'], mode,
                require_forbidden_exec=case['scenario'] == 'extra-backup-producer')
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] == 'directory-replacement':
            carrier.RESULT['negative'] = _replace_root(life)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] in ('directory-hidden', 'directory-symlink', 'directory-unreadable'):
            carrier.RESULT['negative'] = _contaminate_root(life, case['scenario'])
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] == 'forbidden-qmp':
            journal_owner.verify()
            if roots.scan() != binding['roots_digest'] or journal_owner.realm_digest() != realm_digest:
                raise JournalError('realm changed during QMP rejection')
            carrier.RESULT['negative'] = {'realm_remained_valid': True, 'realm_digest': realm_digest}
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] in ('stale-rpc', 'intersecting-source-closure', 'extra-session', 'wrong-rpc'):
            flow = ceremony.SourceCeremony(life, runner, journal_owner, roots, postgres, app, domains,
                                            credentials, app_secrets, expected, intersecting=intersecting,
                                            stale_reply=case['scenario'] == 'stale-rpc')
            carrier.RESULT['source'] = flow.retire_source()
            if case['scenario'] == 'stale-rpc':
                carrier.RESULT['negative'] = flow.reject_stale_reply()
            elif case['scenario'] == 'extra-session':
                carrier.RESULT['negative'] = flow.reject_extra_session()
            elif case['scenario'] == 'wrong-rpc':
                carrier.RESULT['negative'] = wrong_rpc.exercise(flow)
            else:
                carrier.RESULT['negative'] = flow.reject_intersecting_closure()
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative' and case['scenario'] == 'coordinator-kill':
            flow = ceremony.SourceCeremony(life, runner, journal_owner, roots, postgres, app, domains,
                                            credentials, app_secrets, expected, 'create-intent-coordinator')
            carrier.RESULT['source'] = flow.retire_source()
            predecessor = flow.predecessor()
            fault = flow.fault_observation
            exit_stop = fault.get('exit_stop') if type(fault) is dict else None
            if (type(fault) is not dict or fault.get('victim') != 'coordinator' or
                    type(exit_stop) is not dict or exit_stop.get('event') != 'PTRACE_EVENT_EXIT' or
                    exit_stop.get('guard_contended_at_exit_stop') is not True or fault.get('child_pidfd_exited') is not True):
                raise JournalError('original guard was not held at the observed predecessor exit stop')
            carrier.RESULT['negative'] = {'fault': fault, 'predecessor': predecessor, 'callbacks': flow.callback_evidence,
                                          'callback_recovery': flow.callback_recovery}
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'realm-negative':
            raise JournalError('selected negative requires its real fixture and observation')
        if case['case'] == 'source-recovery' and case['scenario'] == 'root-kill':
            raise JournalError('selected recovery negative requires its real fault fixture')
        selected_fault = case['scenario'] if case['case'] == 'source-recovery' and case['scenario'] not in (
            'root-kill', 'vm-kill', 'conflicting-tuple', 'missing-journal', 'app-rollback', 'unsettled-backend') else None
        if case['case'] == 'source-recovery' and case['scenario'] in ('conflicting-tuple', 'missing-journal', 'unsettled-backend'):
            selected_fault = 'reserve-intent-coordinator'
        flow = ceremony.SourceCeremony(life, runner, journal_owner, roots, postgres, app, domains,
                                        credentials, app_secrets, expected, selected_fault,
                                        negative_scenario='unsettled-backend' if case['scenario'] == 'unsettled-backend' else None)
        source = flow.retire_source()
        carrier.RESULT['source'] = source
        if case['case'] == 'source-recovery' and case['scenario'] == 'unsettled-backend':
            carrier.RESULT['negative'] = flow.unsettled_backend_negative()
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'source-recovery' and case['scenario'] in ('conflicting-tuple', 'missing-journal'):
            carrier.RESULT['negative'] = flow.recovery_negative(case['scenario'])
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        if case['case'] == 'source-recovery' and case['scenario'] == 'vm-kill':
            carrier.RESULT['negative'] = reboot_observer.prearm(flow, source, authority, deadline)
            carrier.RESULT['resolver_inventory_final'] = dict(life.resolvers)
            if life.resolver_transition is not None:
                carrier.RESULT['resolver_transition'] = life.resolver_transition
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['bpf_authority'] = authority.export_evidence(carrier.RESULT['genesis']['bpf_authority'], deadline)
            carrier.RESULT['completed'] = True
            source_protocol.emit(carrier)
            select.select([], [], [], 120)
            os._exit(1)
        predecessor = flow.predecessor()
        carrier.RESULT['predecessor'] = predecessor
        carrier.RESULT['callbacks'] = flow.callback_evidence
        if flow.callback_recovery is not None:
            carrier.RESULT['callback_recovery'] = flow.callback_recovery
        if case['case'] == 'source-recovery' and case['scenario'] == 'app-rollback':
            prepared = app_rollback.prepare(flow, predecessor)
            closed = app_rollback.probe_closed(flow, prepared)
            handoff = flow.handoff(predecessor['operation_id'])
            restored = app_rollback.restore(flow, prepared, handoff)
            rejected = app_rollback.probe_restored(flow, prepared, restored)
            carrier.RESULT['handoff'] = handoff
            carrier.RESULT['negative'] = {'prepared': prepared, 'closed': closed, 'restored': restored, 'rejected': rejected}
            carrier.RESULT['action_order'] = flow.action_order
            carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            carrier.RESULT['completed'] = True
            return
        handoff = flow.handoff(predecessor['operation_id'])
        carrier.RESULT['handoff'] = handoff
        carrier.RESULT['action_order'] = flow.action_order
        carrier.RESULT['fault'] = flow.fault_observation
        if selected_fault and flow.fault_observation is None:
            raise JournalError('selected recovery cutpoint was not observed')
        carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
        carrier.RESULT['completed'] = True
    except BaseException as exc:
        carrier.RESULT['completed'] = False
        carrier.RESULT['failure'] = {'stage': stage, 'error_type': type(exc).__name__}
        if stage == 'guest-preflight':
            try:
                diagnostic = guest_bootstrap._preflight_failure_diagnostic(exc)
                if diagnostic is not None:
                    carrier.RESULT['failure']['preflight_diagnostic'] = diagnostic
            except BaseException:
                pass
        if stage == 'bpf-broker-kill' and isinstance(exc, JournalError):
            carrier.RESULT['failure']['message'] = str(exc)[:1024]
        if case['case'] == 'realm-negative' and case['scenario'] == 'bpf-authorization':
            carrier.RESULT['failure']['message'] = str(exc)[:1024]
            cached = authority._evidence_records
            partial = {'records': cached, 'watermark': authority._evidence_sequence,
                       'tail_sha256': authority._evidence_tail}
            if 'bpf_healthy_partial' not in carrier.RESULT and len(canonical(partial)) <= 33554432:
                carrier.RESULT['bpf_cached_partial'] = partial
        if stage == 'pregenesis-systemd':
            carrier.RESULT['failure']['systemd'] = systemd_realm.public_failure()
        if stage == 'postgres-initialize':
            # Private fixed-field diagnosis: never serialize locals, command arguments or credentials.
            try:
                import traceback
                from journal import _write_new
                frames = [{'file': os.path.basename(frame.filename), 'function': frame.name, 'line': frame.lineno}
                          for frame in traceback.extract_tb(exc.__traceback__)][-32:]
                init_state = None
                resolver_state = None
                trace = exc.__traceback__
                while trace is not None:
                    if (trace.tb_frame.f_code.co_name == '_wait_initdb' and
                            os.path.basename(trace.tb_frame.f_code.co_filename) == 'guest_postgres.py'):
                        state = trace.tb_frame.f_locals.get('state')
                        if type(state) is dict:
                            init_state = {key: state.get(key) for key in
                                          ('Running', 'ExitCode', 'Pid', 'OOMKilled', 'Dead', 'Restarting')}
                            if any(type(value) not in (bool, int, type(None)) for value in init_state.values()):
                                init_state = None
                    if (trace.tb_frame.f_code.co_name == 'register_resolver' and
                            os.path.basename(trace.tb_frame.f_code.co_filename) == 'lifecycle.py' and life is not None):
                        stream = life.audit
                        failure = None if stream is None else stream.error
                        resolver_state = {'remaining': life.origins.resolver_remaining(),
                                          'audit_present': stream is not None,
                                          'audit_invalid': None if stream is None else stream.invalid,
                                          'audit_active': None if stream is None else stream.active,
                                          'audit_thread_alive': None if stream is None else stream.thread.is_alive(),
                                          'error_type': None if failure is None else type(failure).__name__,
                                          'error_frames': [] if failure is None else
                                          [{'file': os.path.basename(frame.filename), 'function': frame.name,
                                            'line': frame.lineno}
                                           for frame in traceback.extract_tb(failure.__traceback__)][-32:]}
                        audit_trace = None if failure is None else failure.__traceback__
                        resolver_state['syscall_identity'] = None
                        while audit_trace is not None:
                            if (audit_trace.tb_frame.f_code.co_name == '_record' and
                                    os.path.basename(audit_trace.tb_frame.f_code.co_filename) == 'audit.py'):
                                identity = {key: audit_trace.tb_frame.f_locals.get(key)
                                            for key in ('arch', 'number', 'pid', 'ppid', 'success')}
                                if (all(type(identity[key]) is int for key in ('arch', 'number', 'pid', 'ppid')) and
                                        type(identity['success']) is bool):
                                    resolver_state['syscall_identity'] = identity
                            audit_trace = audit_trace.tb_next
                    trace = trace.tb_next
                fd = os.open('/var/lib/g2', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                try:
                    _write_new(fd, 'postgres-initialize-failure.json',
                               {'version': 1, 'stage': stage, 'error_type': type(exc).__name__,
                                'frames': frames, 'init_state': init_state, 'resolver_state': resolver_state})
                finally:
                    os.close(fd)
            except BaseException:
                pass
        if stage == 'pregenesis-dns':
            # Temporary private diagnosis before any application credentials exist.
            # Preserve the original exception and keep it off the public channel.
            try:
                import traceback
                from journal import _write_new
                detail = traceback.format_exc()
                if len(detail.encode('utf-8')) <= 12000:
                    fd = os.open('/var/lib/g2', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                    try:
                        info = os.fstat(fd)
                        if info.st_uid == info.st_gid == 0 and info.st_mode & 0o022 == 0:
                            _write_new(fd, 'dns-preflight-failure.json',
                                       {'version': 1, 'stage': stage, 'traceback': detail})
                    finally:
                        os.close(fd)
            except BaseException:
                pass
        if life is not None and not life.invalid:
            try:
                life.invalidate('scenario_failed')
            except BaseException:
                pass
    finally:
        if life is not None:
            try:
                life.callbacks.close()
            except BaseException as exc:
                carrier.RESULT['completed'] = False
                carrier.RESULT.setdefault('failure', {'stage': 'callback-observer-cleanup', 'error_type': type(exc).__name__})
                carrier.RESULT.setdefault('cleanup_errors', []).append({'stage': 'callback-observer-cleanup', 'error_type': type(exc).__name__})
                if not life.invalid:
                    try:
                        life.invalidate('callback_observer_cleanup')
                    except BaseException:
                        pass
        if life is not None and life.resolvers:
            carrier.RESULT['resolver_inventory_final'] = dict(life.resolvers)
            if life.resolver_transition is not None:
                carrier.RESULT['resolver_transition'] = life.resolver_transition
        if life is not None and life.migration_scopes:
            carrier.RESULT['migration_scopes'] = list(life.migration_scopes)
        if (not terminal_attempt and journal_owner is not None and journal_owner._genesis is not None and
                not journal_owner.invalid and
                not (case['case'] == 'realm-negative' and case['scenario'] == 'bpf-authorization' and not carrier.RESULT['completed'])):
            try:
                carrier.RESULT['root_journal'] = _export(journal_owner, authority, deadline)
            except BaseException:
                carrier.RESULT['completed'] = False
        if life is not None:
            try:
                carrier.RESULT['maintenance_witness'] = life.maintenance_evidence()
                carrier.RESULT['maintenance_observations'] = life.maintenance_observations()
                carrier.RESULT['maintenance_settlements'] = life.maintenance_settlements()
                carrier.RESULT['maintenance_ready_records'] = life.maintenance_ready_records()
                life.close_maintenance()
            except BaseException as exc:
                carrier.RESULT['completed'] = False
                carrier.RESULT.setdefault('failure', {'stage': 'maintenance-observer-cleanup', 'error_type': type(exc).__name__})
                carrier.RESULT.setdefault('cleanup_errors', []).append({'stage': 'maintenance-observer-cleanup', 'error_type': type(exc).__name__})
        if (not terminal_attempt and authority.role in ('BOOT_OWNER', 'REALM_ROOT') and not authority._closed and
                not (case['case'] == 'realm-negative' and case['scenario'] == 'bpf-authorization' and not carrier.RESULT['completed'])):
            try:
                proof = carrier.RESULT.get('genesis', {}).get('bpf_authority') or carrier.RESULT.get('bpf_bootstrap')
                if proof is not None:
                    carrier.RESULT['bpf_authority'] = authority.export_evidence(proof, deadline)
                elif carrier.RESULT['completed']:
                    raise JournalError('BPF genesis evidence unavailable')
            except BaseException as exc:
                carrier.RESULT['completed'] = False
                carrier.RESULT.setdefault('failure', {'stage': 'bpf-evidence', 'error_type': type(exc).__name__})
        elif carrier.RESULT['completed'] and 'bpf_authority' not in carrier.RESULT and not terminal_attempt:
            carrier.RESULT['completed'] = False
            carrier.RESULT.setdefault('failure', {'stage': 'bpf-evidence', 'error_type': 'JournalError'})
        try:
            source_protocol.emit(carrier)
        finally:
            if audit_owner is not None:
                try:
                    audit_owner.close()
                except BaseException:
                    pass
            # Callback observers and their held descriptors were joined before emission.
            if policy is not None:
                policy.close()
            if daemon_pidfd is not None:
                os.close(daemon_pidfd)
            if journal_owner is not None:
                journal_owner.close()
            if roots is not None:
                roots.close()
            authority.close()


if __name__ == '__main__':
    run()
