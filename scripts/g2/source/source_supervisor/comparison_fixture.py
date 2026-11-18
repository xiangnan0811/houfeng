"""Run one real other-record comparison only after sticky realm invalidation."""
import os
import re
import stat
import time

import census
import ceremony
import invoke
from journal import JournalError, strict_json
from negative_channel import NegativeBackendChannel
import pg_bootstrap

KEY_PARENT = '/var/lib/g2/control/maintenance/comparison-negative'


def _directory():
    parent = os.open('/var/lib/g2/control/maintenance', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(parent)
        if (not stat.S_ISDIR(info.st_mode) or (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (10002, 10002, 0o700)):
            raise JournalError('negative producer parent directory invalid')
        os.mkdir('comparison-negative', 0o700, dir_fd=parent)
        os.chown('comparison-negative', 10002, 10002, dir_fd=parent, follow_symlinks=False)
        fd = os.open('comparison-negative', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
        try:
            item = os.fstat(fd)
            if ((item.st_uid, item.st_gid, stat.S_IMODE(item.st_mode)) != (10002, 10002, 0o700) or
                    os.listdir(fd)):
                raise JournalError('negative producer key directory invalid')
            os.fsync(fd)
        finally:
            os.close(fd)
        os.fsync(parent)
    finally:
        os.close(parent)


def _remove_directory():
    fd = os.open(KEY_PARENT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        item = os.fstat(fd)
        if ((item.st_uid, item.st_gid, stat.S_IMODE(item.st_mode)) != (10002, 10002, 0o700) or os.listdir(fd)):
            raise JournalError('negative producer did not remove its keyring')
        os.fsync(fd)
    finally:
        os.close(fd)
    parent = os.open('/var/lib/g2/control/maintenance', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.rmdir('comparison-negative', dir_fd=parent)
        os.fsync(parent)
    finally:
        os.close(parent)


def _readback(postgres, app, result):
    a, b = app['records']['a'], app['records']['b']
    identifiers = (('rec_', a['record_id']), ('rec_', b['record_id']),
                   ('rrv_', result['base_revision_id']), ('rrv_', result['compared_revision_id']),
                   ('rrv_', result['revision_id']), ('evs_', result['snapshot_id']))
    if any(type(value) is not str or re.fullmatch(re.escape(prefix) + r'[a-z0-9]{1,64}', value) is None
           for prefix, value in identifiers):
        raise JournalError('negative comparison readback identity invalid')
    a_id, b_id, base, compared, revision, snapshot = [value for _, value in identifiers]
    query = ("SELECT json_build_object('record_id',e.record_id,'snapshot_id',e.snapshot_id,'kind',e.kind,"
             "'schema_version',e.schema_version,'payload_digest',encode(p.payload_digest,'hex'),"
             "'payload_size',p.canonical_size_bytes,'compressed_size',p.compressed_size_bytes,"
             "'compressed_bytes',octet_length(p.compressed_payload),'current_revision',r.current_revision_id,"
             "'reference_count',(SELECT count(*)::int FROM record_revision_evidence x WHERE x.record_id=e.record_id "
             "AND x.revision_id='%s' AND x.snapshot_id=e.snapshot_id),'base_exists',"
             "EXISTS(SELECT 1 FROM record_revisions v WHERE v.record_id=e.record_id AND v.revision_id='%s'),"
             "'compared_exists',EXISTS(SELECT 1 FROM record_revisions v WHERE v.record_id=e.record_id AND v.revision_id='%s'),"
             "'target_revision',(SELECT current_revision_id FROM records WHERE record_id='%s'),"
             "'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),"
             "'database_oid',(SELECT oid::int FROM pg_database WHERE datname=current_database()),"
             "'other_sessions',(SELECT count(*)::int FROM pg_stat_activity WHERE backend_type='client backend' "
             "AND pid<>pg_backend_pid()))::text AS state FROM evidence_snapshots e "
             "JOIN evidence_payloads p ON p.payload_digest=e.payload_digest JOIN records r ON r.record_id=e.record_id "
             "WHERE e.record_id='%s' AND e.snapshot_id='%s'\0" %
             (revision, base, compared, a_id, b_id, snapshot)).encode('ascii')
    observed = pg_bootstrap._run('app', postgres.passwords['app'], 'records', query_override=query)
    if (type(observed) is not dict or observed.get('record_id') != b_id or observed.get('snapshot_id') != snapshot or
            observed.get('kind') != 'comparison.result' or observed.get('schema_version') != 1 or
            type(observed.get('payload_digest')) is not str or re.fullmatch(r'[0-9a-f]{64}', observed['payload_digest']) is None or
            observed['payload_digest'] == '0' * 64 or type(observed.get('payload_size')) is not int or observed['payload_size'] <= 0 or
            type(observed.get('compressed_size')) is not int or observed['compressed_size'] <= 0 or
            observed.get('compressed_bytes') != observed['compressed_size'] or observed.get('current_revision') != revision or
            observed.get('reference_count') != 1 or observed.get('base_exists') is not True or
            observed.get('compared_exists') is not True or observed.get('target_revision') != a['revision_id'] or
            observed.get('system_identifier') != app['application']['SystemIdentifier'] or
            observed.get('database_oid') != app['application']['DatabaseOID'] or observed.get('other_sessions') != 0):
        raise JournalError('independent comparison snapshot, payload or reference readback differs')
    return observed


def exercise(life, runner, postgres, app, app_secrets):
    pair = ceremony._source_fixture(app)
    if pair['a']['record_id'] == pair['b']['record_id']:
        raise JournalError('comparison negative records overlap')
    _directory()
    life.invalidate('negative_comparison_producer')
    channels = []
    config = {'version': 1, 'runtime_url': ceremony._url('app', 'houfeng_runtime', app_secrets['runtime_password']),
              'deployment_id': app['deployment_id'], 'actor_user_id': app['actor_user_id'],
              'record_id': pair['b']['record_id']}
    config_fd, digest = invoke.sealed_json(config)
    try:
        def factory(identity, pidfd, sock):
            channel = NegativeBackendChannel(sock, identity, pidfd, life, postgres.passwords['app'],
                                             '172.30.80.2', app['application'])
            channels.append(channel)
            return channel
        result = runner.comparison_negative(config_fd, digest, factory, time.monotonic() + 180)
    finally:
        os.close(config_fd)
    if result['status'] != 0 or result['stderr'] or not result['stdout'] or len(channels) != 1 or channels[0].observation is None:
        raise JournalError('real comparison producer did not complete its backend handshake')
    frame = strict_json(result['stdout'])
    keys = {'version', 'status', 'record_id', 'base_revision_id', 'compared_revision_id', 'revision_id',
            'snapshot_id', 'readback_verified'}
    if (type(frame) is not dict or set(frame) != keys or type(frame['version']) is not int or frame['version'] != 1 or
            frame['status'] != 'produced' or frame['record_id'] != pair['b']['record_id'] or
            frame['base_revision_id'] != pair['b']['revision_id'] or frame['readback_verified'] is not True or
            len({frame['base_revision_id'], frame['compared_revision_id'], frame['revision_id']}) != 3):
        raise JournalError('comparison producer result differs')
    observed = _readback(postgres, app, frame)
    _remove_directory()
    try:
        life.roots.scan()
    except census.CensusError:
        pass
    else:
        raise JournalError('comparison producer revived the invalid realm')
    return {'producer': frame, 'backend': channels[0].observation, 'readback': observed,
            'helper_pid': result['pid'], 'helper_starttime': result['starttime'],
            'keyring_removed': True, 'sticky_invalid': True}
