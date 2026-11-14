"""Root-owned synthetic fixtures and independent truth for the input-handoff matrix."""
import base64
import ctypes
import fcntl
import errno
import hashlib
import json
import os
import select
import signal
import socket
import stat
import sys
import time

import preparation

PROFILES = ('A', 'B')
HELPER_COUNTS = {'C0': 2, 'V1': 2, 'V2': 2, 'V3': 1, 'I0': 1, 'I1': 1, 'I2': 1, 'I3': 1}


def fixture_container(m, cid):
    result = m.must(['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'inspect', cid])
    values = json.loads(result['stdout'])
    m.check(len(values) == 1 and values[0]['Id'] == cid, 'fixture inspect identity mismatch')
    state = values[0]['State']
    return {'id': cid, 'pid': state['Pid'], 'running': state['Running'],
            'status': state['Status'], 'started_at': state['StartedAt']}


def same(a, b):
    return all(a[k] == b[k] for k in ('id', 'pid', 'running', 'started_at'))


def id_matches(observed, truth):
    return all(observed[k] == truth[k] for k in ('dev', 'ino', 'mode'))


def finally_capture(item, label, action):
    try:
        item[label] = action()
    except Exception as exc:
        item.setdefault('truth_errors', []).append({'phase': label, 'error': '%s: %s' % (type(exc).__name__, exc)})


def volume_truth(m, registered, anonymous):
    volume = registered['inspect']
    name, path = volume['Name'], volume['Mountpoint']
    m.check(os.path.realpath(path) == path, 'volume path is not canonical')
    os.chmod(path, 0o700)
    value = os.stat(path)
    m.check(value.st_uid == 0 and value.st_gid == 0 and stat.S_IMODE(value.st_mode) == 0o700,
            'volume fixture owner/mode mismatch')
    return {'name': name, 'path': path, 'registration': registered, 'truth': m.stat_identity(path),
            'owner': {'uid': value.st_uid, 'gid': value.st_gid}, 'anonymous': anonymous}


def make_volume(m, docker, label):
    name = 'g2-' + m.RESULT['run_token'] + '-' + label
    args = ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'volume', 'create',
            '--label', 'owner=g2-isolated-capability', '--label', 'run=' + m.RESULT['run_token'], name]
    listing = ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'volume', 'ls', '--format', '{{.Name}}']
    before = m.must(listing)['stdout'].splitlines()
    m.check(name not in before and len(before) == len(set(before)), 'fixture volume already exists')
    root_receipt('volume-pending-' + label + '.json', {'name': name, 'argv': args, 'before': sorted(before)})
    try:
        result = m.command(args)
        m.check(result['returncode'] == 0 and result['stdout'].strip() == name, 'fixture volume create failed')
        after = m.must(listing)['stdout'].splitlines()
        m.check(len(after) == len(set(after)) and set(after) == set(before) | {name},
                'fixture volume set changed unexpectedly')
        root_receipt('volume-created-' + label + '.json', {'name': name, 'after': sorted(after)})
    except Exception as exc:
        raise m.GuardUnsafe('fixture volume creation outcome uncertain: ' + str(exc))
    return volume_truth(m, docker.rpc({'op': 'volume_register', 'name': name}), False)


def root_receipt(name, value):
    path = '/lab/ext/' + name
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as out:
        json.dump(value, out, sort_keys=True, separators=(',', ':'))
        out.write('\n')
        out.flush()
        os.fsync(out.fileno())
    directory = os.open('/lab/ext', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def anonymous_fixture(m, docker, image):
    token = os.urandom(32).hex()
    name = 'g2-' + m.RESULT['run_token'] + '-v1-anonymous-' + token
    labels = ['--label', 'owner=g2-isolated-capability', '--label', 'run=' + m.RESULT['run_token'],
              '--label', 'runner=guest.py', '--label', 'create-token=' + token]
    envs = ['--env', 'G2_CASE=V1', '--env', 'G2_PHASE=before', '--env', 'G2_METADATA=none',
            '--env', 'G2_METADATA_EXE=0', '--env', 'G2_NESTED=0']
    options = m.base_options() + ['--mount', 'type=volume,target=/data,volume-nocopy']
    spec = {'name': name, 'image': image, 'options': options, 'args': ['hold'], 'token': token,
            'labels': labels, 'environment': envs}
    argv = ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'create', '--name', name] + labels + envs + \
           options + ['--entrypoint', '/probe', image, 'hold']
    listing = ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'volume', 'ls', '--format', '{{.Name}}']
    before = m.must(listing)['stdout'].splitlines()
    m.check(len(before) == len(set(before)), 'fixture volume set ambiguous')
    root_receipt('fixture-pending-' + token + '.json', {'spec': spec, 'argv': argv, 'before': sorted(before)})
    try:
        result = m.command(argv, timeout=90)
        cid = m.create_receipt_id(result)
        root_receipt('fixture-id-' + token + '.json', {'id': cid, 'spec': spec})
        fixture = docker.rpc({'op': 'fixture_register', 'id': cid, 'spec': spec})
        registered = docker.rpc({'op': 'volume_register', 'name': fixture['volume_name'], 'creator': cid})
        m.check(set(registered['volume_set']) == set(before) | {fixture['volume_name']},
                'anonymous fixture volume set changed unexpectedly')
        root_receipt('fixture-volume-' + token + '.json', {'id': cid, 'name': fixture['volume_name'],
                                                            'before': sorted(before),
                                                            'after': registered['volume_set']})
        volume = volume_truth(m, registered, True)
        volume['fixture_creation'] = {'consumer_id': cid, 'before': sorted(before),
                                      'after': registered['volume_set']}
        docker.run(['start', cid])
        docker.rpc({'op': 'watch_add', 'id': cid})
        return volume, cid
    except m.GuardUnsafe:
        raise
    except Exception as exc:
        raise m.GuardUnsafe('anonymous fixture creation or ownership uncertain: ' + str(exc))


def call_helper(docker, image, profile, case, phase, roots, nested=False, deny=False):
    return docker.rpc({'op': 'helper', 'image': image, 'profile': profile, 'case': case, 'phase': phase,
                       'roots': roots, 'nested': nested, 'deny': deny})


def capture_substep(steps, name, action, errors):
    step = {}
    steps[name] = step
    try:
        action(step)
        step['status'] = 'PASS'
    except Exception as exc:
        step.update(status='REJECTED', error_type=type(exc).__name__, error=str(exc)[:2048])
        if len(str(exc)) > 2048 or getattr(exc, 'incomplete', False) or getattr(exc, 'evidence_limit', False):
            step.update(incomplete=True, evidence_limit=True)
        if getattr(exc, 'evidence', None) is not None:
            step['evidence'] = exc.evidence
        if getattr(exc, 'create_result', None) is not None:
            step['create_result'] = exc.create_result
        errors.append(exc)


def required(item, names):
    item['required_evidence'] = {name: True for name in names}


def bind_preparation(m, item, helper, case, profile, phase):
    summary = preparation.validate_observation(helper)
    prep = helper['preparation']
    input_value = prep['request']['input']
    isolation = m.RESULT['isolation']
    install = isolation['preparer_install']
    coordinator = isolation['coordinator']
    m.check(input_value['case'] == case and input_value['profile'] == profile and input_value['phase'] == phase and
            input_value['run_token'] == m.RESULT['run_token'] and input_value['imageID'] == isolation['image'] and
            input_value['preparer_sha256'] == m.PREPARER_SHA256 == install['copy_sha256'] and
            prep['launch']['exe'] == install['copy'] and prep['launch']['session_nonce'] == coordinator['session_nonce'] and
            coordinator['guard_registered_before_ready'] is True and coordinator['ready'] is True and
            prep['coordinator_namespaces'] == isolation['fixture_namespaces'] and helper['helper_pidfd_exited'] is True,
            'helper preparation is bound to a different guest, case, image, or preparer')
    bindings = item.setdefault('preparation_bindings', [])
    m.check(all(all(old[key] != summary[key] for key in ('id', 'ordinal', 'nonce')) for old in bindings),
            'helper preparation identity was reused')
    bindings.append(summary)


def case_result(m, case, profile, action, dependencies=()):
    item = m.RESULT['cases'][case][profile]
    for dependency in dependencies:
        if m.RESULT['cases'][dependency][profile]['status'] != 'PASS':
            item['observations'].update(reason='dependency_not_passed', dependency=dependency)
            return
    try:
        action(item['observations'])
        m.check(not item['observations'].get('truth_errors'), 'independent fixture post-truth unavailable')
        bindings = item['observations'].get('preparation_bindings', [])
        m.check(len(bindings) == HELPER_COUNTS[case], 'case preparation bindings incomplete')
        m.check(type(item['observations'].get('required_evidence')) is dict,
                'case evidence missing after helper validation')
        item['observations']['required_evidence']['preparation_bound'] = True
        item['status'] = 'PASS'
    except m.GuardUnsafe as exc:
        item['status'] = 'INCONCLUSIVE'
        item['observations']['reason'] = 'helper_ownership_uncertain'
        if getattr(exc, 'incomplete', False) or getattr(exc, 'evidence_limit', False):
            item['observations'].update(incomplete=True, evidence_limit=bool(getattr(exc, 'evidence_limit', False)))
        raise
    except m.Failure as exc:
        steps = item['observations'].get('substeps', {}).values()
        evidence_limit = bool(getattr(exc, 'evidence_limit', False) or
                              any(step.get('evidence_limit') for step in steps))
        incomplete = bool(evidence_limit or getattr(exc, 'incomplete', False) or
                          item['observations'].get('truth_errors') or
                          any(step.get('incomplete') for step in steps))
        item['status'] = 'INCONCLUSIVE' if incomplete else 'FAIL'
        item['observations'].update(reason='observation_incomplete' if incomplete else 'capability_failed',
                                    error=str(exc))
        if incomplete:
            item['observations'].update(incomplete=True, evidence_limit=evidence_limit)
        if getattr(exc, 'evidence', None) is not None:
            item['observations']['diagnostic'] = exc.evidence
        if getattr(exc, 'create_result', None) is not None:
            item['observations']['create_result'] = exc.create_result
    except Exception as exc:
        item['status'] = 'INCONCLUSIVE'
        item['observations'].update(reason='observation_incomplete', error='%s: %s' % (type(exc).__name__, exc))
        if getattr(exc, 'evidence', None) is not None:
            item['observations']['diagnostic'] = exc.evidence


def root_status(m, paths):
    return {path: m.stat_identity(path) for path in paths}


def preparation_result(m, variant, profile, action, dependencies):
    item = m.RESULT['preparation_checks'][variant][profile]
    for dependency in dependencies:
        if m.RESULT['cases'][dependency][profile]['status'] != 'PASS':
            item['observations'].update(reason='dependency_not_passed', dependency=dependency)
            return
    try:
        action(item['observations'])
        m.check(not item['observations'].get('truth_errors'), 'independent fixture post-truth unavailable')
        evidence = item['observations'].get('required_evidence')
        m.check(type(evidence) is dict and evidence and all(value is True for value in evidence.values()),
                'preparation check evidence incomplete')
        item['status'] = 'PASS'
    except m.GuardUnsafe as exc:
        item['status'] = 'INCONCLUSIVE'
        item['observations'].update(reason='helper_ownership_uncertain', error=str(exc))
        if getattr(exc, 'incomplete', False) or getattr(exc, 'evidence_limit', False):
            item['observations'].update(incomplete=True, evidence_limit=bool(getattr(exc, 'evidence_limit', False)))
        raise
    except m.Failure as exc:
        incomplete = bool(getattr(exc, 'incomplete', False) or getattr(exc, 'evidence_limit', False) or
                          item['observations'].get('truth_errors'))
        item['status'] = 'INCONCLUSIVE' if incomplete else 'FAIL'
        item['observations'].update(reason='observation_incomplete' if incomplete else 'capability_failed',
                                    error=str(exc))
        if incomplete:
            item['observations'].update(incomplete=True, evidence_limit=bool(getattr(exc, 'evidence_limit', False)))
        if getattr(exc, 'evidence', None) is not None:
            item['observations']['diagnostic'] = exc.evidence
    except Exception as exc:
        item['status'] = 'INCONCLUSIVE'
        item['observations'].update(reason='observation_incomplete', error='%s: %s' % (type(exc).__name__, exc))
        if getattr(exc, 'evidence', None) is not None:
            item['observations']['diagnostic'] = exc.evidence


def control_receipt(m, name):
    m.check(type(name) is str and name and '/' not in name and name not in ('.', '..'),
            'invalid coordinator receipt name')
    fd = os.open('/lab/control/' + name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, 'rb') as stream:
        value = os.fstat(stream.fileno())
        m.check(stat.S_ISREG(value.st_mode) and value.st_uid == 10002 and value.st_nlink == 1 and
                stat.S_IMODE(value.st_mode) == 0o600 and value.st_size <= preparation.MAX_EVIDENCE,
                'coordinator receipt identity invalid')
        data = stream.read(preparation.MAX_EVIDENCE + 1)
    m.check(len(data) <= preparation.MAX_EVIDENCE, 'coordinator receipt too large')
    return preparation.strict_json(data)


def full_mountinfo(m, path):
    value = m.text(path)
    m.check(0 < len(value.encode()) <= 1024 * 1024, 'complete mountinfo unavailable')
    m.handoff.custody_graph(value)
    return value


def source_truth(m, source):
    return {'identity': m.stat_identity(source), 'mountinfo': full_mountinfo(m, '/proc/self/mountinfo')}


def sentinel_truth(m, path):
    return {'identity': m.stat_identity(path), 'sha256': m.digest(path)}


def ready_intents():
    return sorted(name for name in os.listdir('/lab/control') if name.startswith('ready-intent-'))


def owned_absence(m, cid):
    preparation.hex_string(cid, 'retired helper ID')
    result = m.command(['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'container', 'inspect', cid])
    m.check(result['returncode'] != 0 and 'No such' in result['stderr'], 'owned container absence not confirmed')
    return result


def check_retirement(m, retirement, cid):
    m.check(type(retirement) is dict and retirement.get('id') == cid and retirement.get('absent') is True and
            type(retirement.get('remove')) is dict and retirement['remove'].get('returncode') == 0 and
            retirement['remove'].get('stdout', '').strip() == cid, 'exact container retirement missing')


def negative_input(m, value, variant):
    cid = value['helper_id']
    if variant in ('wrong_input_digest', 'extra_fd'):
        prep = value['preparation']
        name = os.path.basename(prep['receipt'])
        m.check(prep['receipt'] == '/lab/control/' + name and name.startswith('prepare-result-') and
                value['negative_intent'] is None and prep['receipt_value'] == control_receipt(m, name),
                'unexpected negative preparation receipt')
        return
    saved = value['negative_intent']
    m.check(saved == control_receipt(m, 'observer-negative-' + cid + '.json'),
            'negative observer input receipt changed')
    m.check(set(saved) == {'variant', 'helper_id', 'stdin_base64', 'waiting'} and
            saved['variant'] == variant and saved['helper_id'] == cid and
            saved['waiting'] == value['waiting']['value'], 'negative observer input identity mismatch')
    waiting = saved['waiting']
    if variant == 'no_ready':
        m.check(saved['stdin_base64'] is None, 'unexpected no-ready payload')
    else:
        payload = base64.b64decode(saved['stdin_base64'], validate=True)
        nonce = waiting['nonce']
        if variant == 'wrong_nonce':
            nonce = ('0' if nonce[0] != '0' else '1') + nonce[1:]
        ready = {'version': 1, 'nonce': nonce, 'input_digest': waiting['input_digest'],
                 'request_digest': '0' * 64, 'prepare_receipt_sha256': '0' * 64, 'action': 'observe'}
        expected = preparation.canonical_json(ready) + b'\n'
        if variant == 'empty_eof':
            expected = b''
        elif variant == 'truncated_eof':
            expected = expected[:len(expected) // 2]
        m.check(payload == expected, 'negative observer input differs from fixed variant')
    return saved


def case_negative(m, docker, image, profile, variant, item):
    source = '/lab/ext/negative-%s-%s' % (profile.lower(), variant)
    os.mkdir(source)
    sentinel = source + '/g2-readonly-sentinel'
    with open(sentinel, 'xb') as out:
        out.write(m.SENTINEL)
    item['source_before'] = source_truth(m, source)
    item['sentinel_before'] = sentinel_truth(m, sentinel)
    item['ready_intents_before'] = ready_intents()
    supervisor = docker.supervisor
    counts = {'launches': len(supervisor.launches), 'exits': len(supervisor.exits),
              'faults': len(supervisor.fault_receipts)}
    item['supervisor_counts_before'] = counts
    try:
        value = docker.rpc({'op': 'helper_negative', 'image': image, 'profile': profile,
                            'variant': variant, 'source': source})
        item['negative_observation'] = value
        helper_id = preparation.hex_string(value['helper_id'], 'negative helper ID')
        item['observer_removal_inspect'] = owned_absence(m, helper_id)
        if len(supervisor.launches) == counts['launches'] + 1:
            item['supervisor_launch'] = supervisor.launches[-1]
        if len(supervisor.exits) == counts['exits'] + 1:
            item['supervisor_exit'] = supervisor.exits[-1]
        m.check(value['source'] == source and value['profile'] == profile and value['image'] == image and
                value['inspect']['config']['Labels']['run'] == m.RESULT['run_token'] and
                value['coordinator_namespaces'] == m.RESULT['isolation']['fixture_namespaces'] and
                value['helper_pidfd_exited'] is True and
                len(supervisor.fault_receipts) == counts['faults'] and supervisor.active is None,
                'negative observation is not bound to this fixture and session')
        owned = control_receipt(m, 'receipt-' + helper_id + '.json')
        item['owned_create_receipt'] = owned
        preparation.validate_negative_create_binding(value, owned)
        if value['running_inspect']['HostConfig']['OomKillDisable'] is None:
            info = m.RESULT['isolation']['docker_info']
            m.check(type(info) is dict and info.get('CgroupVersion') == '2' and
                    info.get('OomKillDisable') is False, 'negative OOM normalization lacks daemon evidence')
        receipt_name = 'observation-' + helper_id + '.json'
        m.check(value['receipt'] == '/lab/control/' + receipt_name, 'negative observation receipt path differs')
        saved = control_receipt(m, receipt_name)
        preparation.validate_negative_create_binding(value, {'id': helper_id, 'spec': owned['spec'],
                                                              'inspect': saved['inspect_before']})
        m.check(saved['request']['id'] == helper_id and saved['request']['image'] == image and
                saved['request']['profile'] == profile and saved['request']['case'] == 'C0' and
                saved['request']['phase'] == 'before' and saved['request']['roots'] ==
                [{'type': 'bind', 'source': source}] and saved['start_attach'] == value['start_attach'] and
                m.handoff.same_json(saved['running_inspect'], value['running_inspect']) and
                saved['inspect_before']['Id'] == saved['inspect_after']['Id'] == helper_id and
                m.handoff.same_json(saved['inspect_after']['State'], value['final_state']) and
                saved['helper_pidfd_exited'] is True, 'negative RPC does not match its actual observation receipt')
        item['observer_stop'] = saved.get('stop')
        m.check(value['stop'] == item['observer_stop'], 'negative stop RPC differs from its receipt')
        if variant == 'no_ready':
            stop = item['observer_stop']
            m.check(type(stop) is dict and stop.get('argv') ==
                    ['/usr/bin/docker', '--host=unix:///var/run/docker.sock', 'stop', '--timeout', '10', helper_id] and
                    stop.get('returncode') == 0 and stop.get('stdout', '').strip() == helper_id,
                    'no-ready observer was not stopped by its exact owned fixture')
        else:
            m.check(item['observer_stop'] is None, 'unexpected observer stop for negative variant')
        if variant in ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce'):
            m.check(saved['test_variant'] == variant, 'observer negative receipt variant differs')
        else:
            m.check(saved['preparation'] == value['preparation'], 'negative preparation receipt differs')
        negative_input(m, value, variant)
        item['negative_binding'] = preparation.validate_negative_observation(value, variant)
        if variant in ('wrong_input_digest', 'extra_fd'):
            m.check(len(supervisor.launches) == counts['launches'] + 1 and
                    len(supervisor.exits) == counts['exits'] + 1 and
                    supervisor.launches[-1] == value['preparation']['launch'] and
                    supervisor.exits[-1] == value['preparation']['exit'],
                    'negative preparer launch and exact wait receipts do not match the supervisor')
            item['supervisor_launch'] = supervisor.launches[-1]
            item['supervisor_exit'] = supervisor.exits[-1]
        else:
            m.check(len(supervisor.launches) == counts['launches'] and len(supervisor.exits) == counts['exits'],
                    'observer-only negative unexpectedly launched a preparer')
        check_retirement(m, value['retirement'], value['helper_id'])
    finally:
        item['supervisor_counts_after'] = {'launches': len(supervisor.launches), 'exits': len(supervisor.exits),
                                           'faults': len(supervisor.fault_receipts)}
        finally_capture(item, 'source_after', lambda: source_truth(m, source))
        finally_capture(item, 'sentinel_after', lambda: sentinel_truth(m, sentinel))
        finally_capture(item, 'ready_intents_after', ready_intents)
    m.check(item['source_before'] == item.get('source_after') and
            item['sentinel_before'] == item.get('sentinel_after') and
            item['ready_intents_before'] == item.get('ready_intents_after'),
            'negative fixture source, sentinel, or ready-intent ledger changed')
    names = ['waiting_only', 'observer_retired', 'no_ready_sent', 'source_unchanged']
    if variant in ('wrong_input_digest', 'extra_fd'):
        names.extend(('request_rejected', 'preparer_exited', 'fds_retired'))
    required(item, names)


def case_c0(m, docker, image, profile, ext, btr, item):
    paths = [ext, btr]
    item['truth_before'] = root_status(m, paths)
    item['sentinel_before'] = m.digest(ext + '/g2-readonly-sentinel')
    item['guard'] = docker.rpc({'op': 'guard'})
    try:
        obs = call_helper(docker, image, profile, 'C0', 'before', [
            {'type': 'bind', 'source': ext}, {'type': 'bind', 'source': btr}])
        item['helper'] = obs
        bind_preparation(m, item, obs, 'C0', profile, 'before')
        idx = obs['indexed']
        m.check(id_matches(idx['000:fstat']['value'], item['truth_before'][ext]) and
                id_matches(idx['001:fstat']['value'], item['truth_before'][btr]) and
                idx['000:statfs']['value']['type'] == 0xEF53 and
                idx['001:statfs']['value']['type'] == 0x9123683e and
                idx['001:btrfs_fs_info']['errno'] == 0 and idx['001:btrfs_ino_lookup']['errno'] == 0,
                'baseline ext4/btrfs physical identity mismatch')
        deny = call_helper(docker, image, profile, 'C0', 'after', [{'type': 'bind', 'source': ext}], deny=True)
        item['deny'] = deny
        bind_preparation(m, item, deny, 'C0', profile, 'after')
        m.check(deny['indexed']['000:readonly_open']['errno'] == errno.EROFS, 'baseline readonly sentinel failed')
        if profile == 'B':
            m.check(all(deny['indexed'][name]['errno'] == errno.EPERM for name in ('inet_socket', 'unshare', 'mount')),
                    'custom seccomp denials not EPERM')
    finally:
        finally_capture(item, 'truth_after', lambda: root_status(m, paths))
        finally_capture(item, 'sentinel_after', lambda: m.digest(ext + '/g2-readonly-sentinel'))
    m.check(item['truth_before'] == item.get('truth_after') and
            item.get('sentinel_after') == item['sentinel_before'] and item['guard']['held'],
            'baseline fixture changed')
    names = ['coordinator_identity', 'guard_held', 'ext4', 'btrfs', 'helper_isolation', 'probe_complete']
    if profile == 'B':
        names.append('seccomp_denials')
    required(item, names)


def case_v1(m, docker, image, profile, volumes, consumers, project, item):
    paths = [x['path'] for x in volumes] + [project]
    item['volumes'] = volumes
    m.check(len(volumes) == 2 and not volumes[0]['anonymous'] and volumes[1]['anonymous'],
            'named and anonymous fixtures missing')
    item['truth_before'] = root_status(m, paths)
    item['consumers_before'] = [fixture_container(m, cid) for cid in consumers]
    steps = item['substeps'] = {}
    errors = []
    try:
        for label, vol in zip(('named', 'anonymous'), volumes):
            def direct(step, volume=vol):
                step['result'] = docker.rpc({'op': 'volume_access', 'name': volume['name']})
                m.check(step['result']['errno'] == errno.EACCES, 'direct volume access did not fail EACCES')
            capture_substep(steps, 'direct_' + label, direct, errors)
        for label, vol in zip(('named', 'anonymous'), volumes):
            def observe(step, volume=vol):
                obs = call_helper(docker, image, profile, 'V1', 'before', [
                    {'type': 'volume', 'name': volume['name']}, {'type': 'bind', 'source': project}])
                step['result'] = obs
                bind_preparation(m, item, obs, 'V1', profile, 'before')
                idx = obs['indexed']
                m.check(id_matches(idx['000:fstat']['value'], item['truth_before'][volume['path']]) and
                        id_matches(idx['001:fstat']['value'], item['truth_before'][project]) and
                        idx['000:statfs']['value']['type'] == 0xEF53 and
                        idx['001:statfs']['value']['type'] == 0x9123683e and
                        idx['001:btrfs_fs_info']['errno'] == 0 and idx['001:btrfs_ino_lookup']['errno'] == 0,
                        'volume physical identity mismatch')
            capture_substep(steps, label, observe, errors)
        for ordinal, cid in enumerate(consumers):
            def watch(step, consumer_id=cid, index=ordinal):
                value = docker.rpc({'op': 'watch_check', 'id': consumer_id})
                step['result'] = value
                before = item['consumers_before'][index]
                m.check(value['id'] == before['id'] and value['pid'] == before['pid'] and
                        value['started_at'] == before['started_at'] and value['pidfd_alive'],
                        'UID consumer pidfd differs from fixture identity')
            capture_substep(steps, 'watch_%d' % ordinal, watch, errors)
    finally:
        finally_capture(item, 'truth_after', lambda: root_status(m, paths))
        finally_capture(item, 'consumers_after', lambda: [fixture_container(m, cid) for cid in consumers])
    def final_truth(step):
        after = item.get('consumers_after', [])
        m.check(item['truth_before'] == item.get('truth_after') and len(after) == len(consumers) and
                len(item['consumers_before']) == len(consumers) and
                all(same(a, b) and b['running'] for a, b in zip(item['consumers_before'], after)),
                'V1 truth or consumers changed')
    capture_substep(steps, 'final_truth', final_truth, errors)
    if errors:
        raise errors[0]
    required(item, ['named', 'anonymous', 'direct_eacces', 'distinct_filesystems', 'consumer_unchanged',
                    'native_volume_mounts', 'exact_identity'])


def case_v2(m, docker, image, profile, volume, nested_source, item):
    root = volume['path']
    item['volume'] = volume
    sentinels = [root + '/g2-readonly-sentinel', root + '/nested/g2-readonly-sentinel']
    paths = [root, root + '/nested', nested_source]
    item['truth_before'] = root_status(m, paths)
    item['sentinel_before'] = {p: m.digest(p) for p in sentinels}
    steps = item['substeps'] = {}
    errors = []
    try:
        def observe(step):
            obs = call_helper(docker, image, profile, 'V2', 'before',
                              [{'type': 'volume', 'name': volume['name']}], nested=True)
            step['result'] = obs
            bind_preparation(m, item, obs, 'V2', profile, 'before')
            idx = obs['indexed']
            m.check(id_matches(idx['000:fstat']['value'], item['truth_before'][root]) and
                    id_matches(idx['nested:000:fstat']['value'], item['truth_before'][nested_source]) and
                    idx['nested:000:statfs']['value']['type'] == 0x9123683e and
                    idx['nested:000:btrfs_fs_info']['errno'] == 0 and
                    idx['nested:000:btrfs_ino_lookup']['errno'] == 0 and
                    '/roots/000/nested' in obs['mount_graph'], 'nested volume mount identity mismatch')
        capture_substep(steps, 'observe', observe, errors)
        def deny(step):
            value = call_helper(docker, image, profile, 'V2', 'after',
                                [{'type': 'volume', 'name': volume['name']}], nested=True, deny=True)
            step['result'] = value
            bind_preparation(m, item, value, 'V2', profile, 'after')
            m.check(value['indexed']['000:readonly_open']['errno'] == errno.EROFS and
                    value['indexed']['001:readonly_open']['errno'] == errno.EROFS, 'nested sentinel is writable')
        capture_substep(steps, 'deny', deny, errors)
    finally:
        finally_capture(item, 'truth_after', lambda: root_status(m, paths))
        finally_capture(item, 'sentinel_after', lambda: {p: m.digest(p) for p in sentinels})
    def final_truth(step):
        m.check(item['truth_before'] == item.get('truth_after') and
                item['sentinel_before'] == item.get('sentinel_after'), 'nested fixture changed')
    capture_substep(steps, 'final_truth', final_truth, errors)
    if errors:
        raise errors[0]
    required(item, ['recursive_readonly', 'private_propagation', 'nested_identity', 'root_write_erofs',
                    'nested_write_erofs', 'sentinels_unchanged'])


def case_v3(m, docker, image, profile, volume, item):
    path = volume['path']
    item['volume'] = volume
    item['truth_before'] = {'identity': m.stat_identity(path), 'entries': sorted(os.listdir(path))}
    item['image_fixture'] = m.RESULT['isolation']['image_fixture']
    steps = item['substeps'] = {}
    errors = []
    try:
        def fixture(step):
            m.check(item['image_fixture']['image_id'] == image and
                    item['image_fixture']['marker_path'] == '/roots/000/g2-image-marker' and
                    item['image_fixture']['marker_sha256'] == hashlib.sha256(b'g2-image-nocopy-marker\n').hexdigest(),
                    'NoCopy image marker fixture missing')
            m.check(item['truth_before']['entries'] == [], 'NoCopy fixture volume is not empty')
        capture_substep(steps, 'fixture', fixture, errors)
        def observe(step):
            obs = call_helper(docker, image, profile, 'V3', 'before', [{'type': 'volume', 'name': volume['name']}])
            step['result'] = obs
            bind_preparation(m, item, obs, 'V3', profile, 'before')
            m.check(id_matches(obs['indexed']['000:fstat']['value'], item['truth_before']['identity']),
                    'NoCopy volume object mismatch')
        capture_substep(steps, 'nocopy', observe, errors)
        def missing(step):
            absent = 'g2-' + m.RESULT['run_token'] + '-does-not-exist-' + profile.lower()
            value = docker.rpc({'op': 'volume_missing', 'name': absent, 'image': image, 'profile': profile})
            step['result'] = value
            m.check(value['reason'] == 'unknown_volume' and value['rejection']['type'] == 'UnknownVolumeAdmission' and
                    value['rejection']['code'] == 'unknown_unregistered_volume' and
                    value['request'] == {'op': 'helper', 'image': image, 'profile': profile, 'case': 'V3',
                                         'phase': 'after', 'roots': [{'type': 'volume', 'name': absent}]} and
                    value['before'] == value['after'] and value['owner_state_unchanged'] is True and
                    value['pending_before'] == value['pending_after'] == [] and
                    value['created_before'] == value['created_after'] and
                    not any(command and command[0] == 'create' for command in value['commands']),
                    'missing volume refusal incomplete')
        capture_substep(steps, 'missing', missing, errors)
    finally:
        finally_capture(item, 'truth_after', lambda: {'identity': m.stat_identity(path),
                                                     'entries': sorted(os.listdir(path))})
    def final_truth(step):
        m.check(item.get('truth_after') == item['truth_before'], 'NoCopy volume populated or changed')
    capture_substep(steps, 'final_truth', final_truth, errors)
    if errors:
        raise errors[0]
    required(item, ['empty_volume', 'nocopy', 'missing_rejected_before_create', 'volume_set_unchanged'])


def case_i0(m, docker, image, profile, item):
    item['fixture_before'] = {'guard': m.stat_identity('/lab/control/guard')}
    try:
        obs = docker.rpc({'op': 'daemon_observe', 'image': image, 'profile': profile})
        item['daemon'] = obs
        bind_preparation(m, item, obs['helper'], 'I0', profile, 'before')
        m.check(obs['bootstrap']['sender']['uid'] == 0 and obs['bootstrap']['pidfd_alive'] and
                obs['mount_namespace_identity'] == 'UNPROVEN', 'daemon bootstrap incomplete')
    finally:
        finally_capture(item, 'fixture_after', lambda: {'guard': m.stat_identity('/lab/control/guard')})
    m.check(item['fixture_before'] == item.get('fixture_after'), 'guard fixture changed')
    required(item, ['daemon_credentials', 'daemon_pidfd', 'daemon_pidns', 'dockerd_exe', 'exact_metadata',
                    'helper_complete'])


def bind_consumer(m, docker, image, case, profile, source, label):
    return m.consumer(docker, image, 'g2-' + m.RESULT['run_token'] + '-' + label + '-' + profile.lower(), case,
                      m.root_mount(source, '/data', readonly=False))


def case_i1(m, docker, image, profile, source, item):
    cid = bind_consumer(m, docker, image, 'I1', profile, source, 'i1')
    item['consumer_before'] = fixture_container(m, cid)
    pid = item['consumer_before']['pid']
    item['truth_before'] = {'source': m.stat_identity(source), 'actual': m.stat_identity('/proc/%d/root/data' % pid)}
    root_fd = os.pidfd_open(pid)
    begun = False
    try:
        item['consumer_held_before'] = held_consumer_truth(m, cid, pid, root_fd)
        item['target_mountinfo_before'] = full_mountinfo(m, '/proc/%d/mountinfo' % pid)
        item['begin'] = docker.rpc({'op': 'target_begin', 'id': cid, 'destination': '/data'})
        begun = True
        obs = docker.rpc({'op': 'target_probe', 'id': cid, 'image': image, 'profile': profile, 'case': 'I1'})
        item['observation'] = obs
        bind_preparation(m, item, obs['helper'], 'I1', profile, 'before')
        m.check(obs['status'] == 'OBSERVED', 'I1 helper did not complete')
        result = docker.rpc({'op': 'target_finish'})
        begun = False
        item['finish'] = result
        m.check(result['accepted'] is True and result['reason'] == 'match' and
                id_matches(result['source'], item['truth_before']['source']) and
                id_matches(result['actual'], item['truth_before']['actual']), 'running bind identity mismatch')
    except m.GuardUnsafe:
        begun = False
        raise
    finally:
        if begun:
            try:
                docker.rpc({'op': 'target_abort'})
            except Exception as exc:
                item.setdefault('truth_errors', []).append({'phase': 'target_abort', 'error': str(exc)})
        finally_capture(item, 'consumer_after', lambda: fixture_container(m, cid))
        finally_capture(item, 'truth_after', lambda: {'source': m.stat_identity(source),
                        'actual': m.stat_identity('/proc/%d/root/data' % pid)})
        finally_capture(item, 'consumer_held_after', lambda: held_consumer_truth(m, cid, pid, root_fd))
        finally_capture(item, 'target_mountinfo_after', lambda: full_mountinfo(m, '/proc/%d/mountinfo' % pid))
        os.close(root_fd)
    m.check(item['truth_before'] == item.get('truth_after') and same(item['consumer_before'], item['consumer_after']) and
            item['consumer_after']['running'], 'I1 fixture or consumer changed')
    required(item, ['object_match', 'target_identity', 'target_pidfd', 'mountinfo_stable', 'consumer_unchanged',
                    'helper_complete'])


def case_i2(m, docker, image, profile, source, alias, replacement, item):
    propagation = m.held_alias(source, alias)
    cid = bind_consumer(m, docker, image, 'I2', profile, source, 'i2')
    item['consumer_before'] = fixture_container(m, cid)
    pid = item['consumer_before']['pid']
    item['replacement_truth_before'] = m.stat_identity(replacement)
    item['pre_cover'] = m.held_truth(source, alias, pid, '/data')
    m.bind(replacement, source)
    item['post_cover'] = m.held_truth(source, alias, pid, '/data')
    item['alias_propagation'] = propagation
    m.check_held_fixture(item['pre_cover'], item['post_cover'], True)
    item['truth_before'] = m.held_truth(source, alias, pid, '/data')
    root_fd = os.pidfd_open(pid)
    begun = False
    try:
        item['consumer_held_before'] = held_consumer_truth(m, cid, pid, root_fd)
        item['target_mountinfo_before'] = full_mountinfo(m, '/proc/%d/mountinfo' % pid)
        item['begin'] = docker.rpc({'op': 'target_begin', 'id': cid, 'destination': '/data'})
        begun = True
        obs = docker.rpc({'op': 'target_probe', 'id': cid, 'image': image, 'profile': profile, 'case': 'I2'})
        item['observation'] = obs
        bind_preparation(m, item, obs['helper'], 'I2', profile, 'before')
        m.check(obs['status'] == 'OBSERVED', 'I2 helper did not complete')
        result = docker.rpc({'op': 'target_finish'})
        begun = False
        item['finish'] = result
        m.check(result['accepted'] is False and result['reason'] == 'running_mount_mismatch' and
                id_matches(result['source'], item['truth_before']['source']) and
                id_matches(result['actual'], item['truth_before']['old']) and
                result['source']['dev'] != result['actual']['dev'], 'expected running_mount_mismatch missing')
    except m.GuardUnsafe:
        begun = False
        raise
    finally:
        if begun:
            try:
                docker.rpc({'op': 'target_abort'})
            except Exception as exc:
                item.setdefault('truth_errors', []).append({'phase': 'target_abort', 'error': str(exc)})
        finally_capture(item, 'consumer_after', lambda: fixture_container(m, cid))
        finally_capture(item, 'truth_after', lambda: m.held_truth(source, alias, pid, '/data'))
        finally_capture(item, 'replacement_truth_after', lambda: m.stat_identity(replacement))
        finally_capture(item, 'consumer_held_after', lambda: held_consumer_truth(m, cid, pid, root_fd))
        finally_capture(item, 'target_mountinfo_after', lambda: full_mountinfo(m, '/proc/%d/mountinfo' % pid))
        os.close(root_fd)
    m.check(item['truth_before'] == item.get('truth_after') and same(item['consumer_before'], item['consumer_after']) and
            item['consumer_after']['running'], 'I2 fixture or consumer changed')
    required(item, ['old_held_alias', 'mismatch_reported', 'target_identity', 'consumer_unchanged', 'helper_complete'])


def case_i3(m, docker, image, profile, sources, item):
    for step, source in enumerate(sources):
        cid = bind_consumer(m, docker, image, 'I3', profile, source, 'i3-%d' % step)
        before = fixture_container(m, cid)
        root_fd = os.pidfd_open(before['pid'])
        detail = {'consumer_before': before}
        item['before_create' if step == 0 else 'after_helper'] = detail
        begun = False
        try:
            detail['truth_before'] = m.stat_identity(source)
            detail['actual_before'] = m.stat_identity('/proc/%d/root/data' % before['pid'])
            detail['consumer_held_before'] = held_consumer_truth(m, cid, before['pid'], root_fd)
            detail['target_mountinfo_before'] = full_mountinfo(m, '/proc/%d/mountinfo' % before['pid'])
            detail['begin'] = docker.rpc({'op': 'target_begin', 'id': cid, 'destination': '/data'})
            begun = True
            if step == 1:
                detail['observation'] = docker.rpc({'op': 'target_probe', 'image': image, 'profile': profile, 'case': 'I3'})
                bind_preparation(m, item, detail['observation']['helper'], 'I3', profile, 'before')
                m.check(detail['observation']['status'] == 'OBSERVED', 'I3 successful output missing')
            detail['stop'] = docker.run(['stop', '--timeout', '10', cid], timeout=30)
            detail['root_pidfd_exited'] = bool(select.select([root_fd], [], [], 0)[0])
            detail['root_pidfd_info_after_exit'] = m.text('/proc/self/fdinfo/%d' % root_fd)
            m.check(detail['root_pidfd_exited'], 'fixture pidfd did not observe target exit')
            detail['consumer_stopped'] = fixture_container(m, cid)
            m.check(not detail['consumer_stopped']['running'] and detail['consumer_stopped']['status'] == 'exited',
                    'fixture target did not stop')
            if step == 0:
                detail['observation'] = docker.rpc({'op': 'target_probe', 'image': image, 'profile': profile, 'case': 'I3'})
                m.check(detail['observation']['status'] == 'REJECTED' and
                        detail['observation']['reason'] == 'target_exited' and
                        detail['observation']['pidfd_exited'] and
                        detail['observation']['helper_create_issued'] is False, 'I3 pre-create exit accepted')
            detail['restart'] = docker.run(['start', cid])
            detail['consumer_restarted'] = fixture_container(m, cid)
            m.check(detail['consumer_restarted']['running'] and
                    detail['consumer_restarted']['pid'] != before['pid'] and
                    detail['consumer_restarted']['started_at'] != before['started_at'] and
                    bool(select.select([root_fd], [], [], 0)[0]), 'old pidfd became valid after restart')
            detail['finish'] = docker.rpc({'op': 'target_finish'})
            begun = False
            m.check(detail['finish']['accepted'] is False and detail['finish']['reason'] == 'target_exited' and
                    detail['finish']['pidfd_exited'], 'I3 stale observation accepted')
        except m.GuardUnsafe:
            begun = False
            raise
        finally:
            if begun:
                try:
                    docker.rpc({'op': 'target_abort'})
                except Exception as exc:
                    item.setdefault('truth_errors', []).append({'phase': 'target_abort', 'error': str(exc)})
            finally_capture(detail, 'consumer_after', lambda: fixture_container(m, cid))
            finally_capture(detail, 'truth_after', lambda: m.stat_identity(source))
            os.close(root_fd)
        m.check(not detail.get('truth_errors') and detail['truth_before'] == detail['truth_after'] and
                detail['consumer_after']['running'] and same(detail['consumer_after'], detail['consumer_restarted']),
                'I3 fixture restart truth incomplete')
    required(item, ['stopped_before_create', 'stopped_after_helper', 'pidfd_exited',
                    'stale_output_rejected', 'restart_rejected'])


def kill_source_truth(m, source, daemon_pid, target_pid):
    daemon_ns = os.stat('/proc/%d/ns/mnt' % daemon_pid)
    target_ns = os.stat('/proc/%d/ns/mnt' % target_pid)
    return {'source': m.stat_identity(source), 'actual': m.stat_identity('/proc/%d/root/data' % target_pid),
            'daemon_stat': m.handoff.proc_stat(m.text('/proc/%d/stat' % daemon_pid)),
            'target_stat': m.handoff.proc_stat(m.text('/proc/%d/stat' % target_pid)),
            'daemon_namespace': {'dev': daemon_ns.st_dev, 'ino': daemon_ns.st_ino},
            'target_namespace': {'dev': target_ns.st_dev, 'ino': target_ns.st_ino},
            'source_mountinfo': full_mountinfo(m, '/proc/%d/mountinfo' % daemon_pid),
            'target_mountinfo': full_mountinfo(m, '/proc/%d/mountinfo' % target_pid)}


def kill_sentinel_truth(m, source, target_pid):
    return {'source': sentinel_truth(m, source + '/g2-readonly-sentinel'),
            'actual': sentinel_truth(m, '/proc/%d/root/data/g2-readonly-sentinel' % target_pid)}


def held_consumer_truth(m, cid, pid, pidfd):
    preparation.validate_pidfd(pidfd, pid)
    state = fixture_container(m, cid)
    state['process_stat'] = m.handoff.proc_stat(m.text('/proc/%d/stat' % pid))
    state['pidfd_info'] = m.text('/proc/self/fdinfo/%d' % pidfd)
    state['pidfd_alive'] = not bool(select.select([pidfd], [], [], 0)[0])
    m.check(state['running'] and state['pid'] == pid and state['process_stat']['pid'] == pid and
            state['pidfd_alive'], 'held target identity changed')
    return state


def validate_fault_progress(m, fault, request, truth):
    point = fault['point']
    expected_points = ['before_attach'] + (['after_first_attach'] if point == 'after_first_attach' else [])
    snapshots = fault['progress_snapshots']
    m.check(type(snapshots) is list and [value['point'] for value in snapshots] == expected_points,
            'fault progress sequence differs')
    m.check(fault['request_digest'] == request['request_digest'] and
            fault['ordinal'] == request['input']['ordinal'] and fault['nonce'] == request['input']['nonce'],
            'fault marker is not bound to the prepare request')
    for snapshot in snapshots:
        m.check(set(snapshot) == {'point', 'evidence', 'evidence_sha256'} and
                hashlib.sha256(preparation.canonical_json(snapshot['evidence'])).hexdigest() ==
                snapshot['evidence_sha256'], 'fault graph digest differs')
        evidence = snapshot['evidence']
        m.check(set(evidence) == {'version', 'helper_pid', 'helper_mountinfo', 'sources', 'before', 'attached'} and
                type(evidence['version']) is int and evidence['version'] == 1 and
                evidence['helper_pid'] == request['helper']['pid'], 'fault graph helper identity differs')
    first = snapshots[0]['evidence']
    m.check(fault['evidence'] == snapshots[-1]['evidence'] and
            fault['evidence_sha256'] == snapshots[-1]['evidence_sha256'] and
            type(first['sources']) is list and len(first['sources']) == 2 and
            type(first['before']) is list and len(first['before']) == 2 and first['attached'] == [],
            'before-attach graph is incomplete')
    initial_graph = m.handoff.custody_graph(first['helper_mountinfo'])
    source_graphs = (m.handoff.custody_graph(truth['source_mountinfo']),
                     m.handoff.custody_graph(truth['target_mountinfo']))
    roots = request['input']['roots']
    for ordinal, role in enumerate(('declared_bind', 'running_root')):
        source = first['sources'][ordinal]
        before = first['before'][ordinal]
        preparation._source_or_attachment(source, False)
        m.check(set(before) == {'role', 'ordinal', 'target', 'before'} and source['role'] == role and
                source['ordinal'] == ordinal and before['role'] == role and before['ordinal'] == ordinal and
                before['target'] == '/roots/%03d' % ordinal and roots[ordinal]['kind'] == role and
                source['source_path'] == (roots[0]['source'] if ordinal == 0 else '/data') and
                source['namespace'] == truth['daemon_namespace' if ordinal == 0 else 'target_namespace'] and
                all(source[key] == truth['source' if ordinal == 0 else 'actual'][key]
                    for key in ('dev', 'ino', 'mode')), 'fault source custody differs from held truth')
        if ordinal == 0:
            m.check(source['clone'] is None, 'configured root unexpectedly cloned')
        else:
            m.check(source['clone'] == {key: source[key] for key in ('dev', 'ino', 'mode')},
                    'running-root clone identity differs')
        m.handoff.check_custody_subtree(m, source_graphs[ordinal], source['source_path'], source['subtree'])
        before_record = before['before']
        preparation.fields(before_record, ('dev', 'ino', 'mode', 'mount_id', 'native_root', 'subtree'),
                           'fault before target')
        preparation._object_identity({key: before_record[key] for key in ('dev', 'ino', 'mode')},
                                     'fault before object')
        preparation._subtree(before_record['subtree'], before_record['mount_id'], before_record['native_root'])
        m.handoff.check_custody_subtree(m, initial_graph, before['target'], before_record['subtree'])
    m.check(not any(row['point'] == '/roots/001' for row in initial_graph.values()),
            'deferred root was already attached before fault')
    metadata = ['/meta/target-stat', '/meta/target-mountinfo', '/meta/target-pidns']
    m.handoff.mount_graph(first['helper_mountinfo'], ['/roots/000'] + metadata)
    if point == 'after_first_attach':
        current = snapshots[1]['evidence']
        m.check(current['sources'] == first['sources'] and current['before'] == first['before'] and
                type(current['attached']) is list and len(current['attached']) == 2,
                'partial graph does not contain the first running-root attach')
        current_graph = m.handoff.custody_graph(current['helper_mountinfo'])
        for ordinal, attachment in enumerate(current['attached']):
            preparation._source_or_attachment(attachment, True)
            source, before = first['sources'][ordinal], first['before'][ordinal]
            m.check(attachment['role'] == source['role'] and attachment['ordinal'] == ordinal and
                    attachment['target'] == before['target'] and attachment['before'] == before['before'] and
                    attachment['namespace'] == request['helper']['mntns'] and
                    all(attachment[key] == source[key] for key in ('dev', 'ino', 'mode')) and
                    preparation._same_subtree(source['subtree'], attachment['subtree']) and
                    preparation._private_readonly(attachment['subtree']),
                    'partial attached graph is not a readonly private source clone')
            m.handoff.check_custody_subtree(m, current_graph, attachment['target'], attachment['subtree'])
        m.handoff.mount_graph(current['helper_mountinfo'], ['/roots/000', '/roots/001'] + metadata)


def kill_diagnostic(m, docker, exc, item, point, profile, image, target_id, source, counts):
    evidence = exc.evidence
    m.check(type(evidence) is dict and type(evidence.get('request')) is dict and
            type(evidence.get('rejection')) is dict and evidence['rejection'].get('code') == 'prepare_protocol',
            'killed preparer did not produce the expected preparation rejection')
    helper_id = evidence['request']['id']
    preparation.hex_string(helper_id, 'fault helper ID')
    name = 'observation-' + helper_id + '.json'
    m.check(evidence['receipt'] == '/lab/control/' + name, 'fault observation receipt path differs')
    saved = control_receipt(m, name)
    attached = saved['start_attach']
    state = saved['inspect_after']['State']
    prep = saved['preparation']
    owned = control_receipt(m, 'receipt-' + helper_id + '.json')
    item['diagnostic'] = {'helper_id': helper_id, 'observation_request': saved['request'],
                          'start_attach': attached, 'final_state': state, 'preparation': prep,
                          'owned_create_receipt': owned, 'inspect_before': saved['inspect_before'],
                          'running_inspect': saved['running_inspect'], 'rejection': evidence['rejection'],
                          'daemon_lifecycle': evidence['daemon_lifecycle'],
                          'helper_pidfd_exited': saved['helper_pidfd_exited'], 'receipt': evidence['receipt']}
    m.check(saved['request'] == evidence['request'] and saved['inspect_after']['Id'] == helper_id and
            m.handoff.raw_capture_problem(attached) is None and attached['returncode'] != 0 and
            attached['argv'] == ['/usr/bin/docker', '--host=unix:///var/run/docker.sock',
                                 'start', '--attach', '--interactive', helper_id] and
            saved['helper_pidfd_exited'] is True and state['Running'] is False and
            state['Status'] == 'exited' and state['ExitCode'] != 0 and
            attached['stdout'].endswith('\n'), 'killed helper output or final state incomplete')
    rows = [m.handoff.strict_json(line) for line in attached['stdout'].splitlines()]
    m.check(len(rows) == 1 and set(rows[0]) == {'experiment_kind', 'preparation_version', 'metadata_mode',
            'case', 'phase', 'root_ordinal', 'operation', 'errno', 'value'} and
            rows[0]['experiment_kind'] == 'input-handoff-v1' and rows[0]['preparation_version'] == 1 and
            rows[0]['metadata_mode'] == 'proc-v1' and rows[0]['case'] == 'I1' and
            rows[0]['phase'] == 'before' and rows[0]['root_ordinal'] is None and
            rows[0]['operation'] == 'waiting' and rows[0]['errno'] == 0,
            'killed helper did not emit only its waiting record')
    item['diagnostic']['waiting'] = rows[0]
    prep_name = os.path.basename(prep['receipt'])
    m.check(prep['receipt'] == '/lab/control/' + prep_name and prep_name.startswith('prepare-result-'),
            'fault preparation receipt path differs')
    prep_value = control_receipt(m, prep_name)
    item['diagnostic']['preparation_receipt'] = prep_value
    request = preparation.validate_request(prep_value['request'])
    item['diagnostic']['request'] = request
    inp = request['input']
    inspected = saved['inspect_before']
    running = saved['running_inspect']
    m.check(type(inspected) is dict and type(running) is dict and type(owned) is dict,
            'fault created or running inspection missing')
    portable_created = {'mounts': inspected['Mounts'], 'host_config': inspected['HostConfig'],
                        'apparmor': inspected.get('AppArmorProfile'), 'config': inspected['Config'],
                        'image': inspected['Image'], 'id': inspected['Id']}
    portable_running = {'mounts': running['Mounts'], 'host_config': running['HostConfig'],
                        'apparmor': running.get('AppArmorProfile'), 'config': running['Config'],
                        'image': running['Image'], 'id': running['Id']}
    preparation.validate_negative_create_binding(
        {'helper_id': helper_id, 'image': image, 'create_spec': prep_value['create_spec'],
         'inspect': portable_created, 'running_inspect': running}, owned)
    original = owned['inspect']
    m.check(all(m.handoff.same_json(original[key], inspected[key]) for key in
                ('Id', 'Name', 'Image', 'Config', 'Created', 'Path', 'Args', 'HostConfig')) and
            m.handoff.same_json(m.handoff.mount_map(original['Mounts']),
                                m.handoff.mount_map(inspected['Mounts'])) and
            m.handoff.same_json(m.handoff.mount_map(inspected['Mounts']),
                                m.handoff.mount_map(running['Mounts'])),
            'fault owned, created, and running inspection identities differ')
    created_hc, running_hc = inspected['HostConfig'], running['HostConfig']
    m.check(type(created_hc) is dict and type(running_hc) is dict and created_hc.keys() == running_hc.keys() and
            created_hc.get('OomKillDisable') is False and
            (running_hc['OomKillDisable'] is False or running_hc['OomKillDisable'] is None) and
            all(key == 'OomKillDisable' or m.handoff.same_json(created_hc[key], running_hc[key])
                for key in created_hc), 'fault running HostConfig differs from creation')
    if running_hc['OomKillDisable'] is None:
        info = m.RESULT['isolation']['docker_info']
        m.check(type(info) is dict and info.get('CgroupVersion') == '2' and
                info.get('OomKillDisable') is False, 'fault OOM normalization lacks daemon evidence')
    runtime_state = running['State']
    m.check(type(runtime_state) is dict and runtime_state.get('Running') is True and
            runtime_state.get('Status') == 'running' and type(runtime_state.get('Pid')) is int and
            runtime_state['Pid'] > 0 and type(runtime_state.get('StartedAt')) is str and
            bool(runtime_state['StartedAt']) and runtime_state['StartedAt'] != '0001-01-01T00:00:00Z' and
            m.handoff.same_json(runtime_state, prep_value['helper_state']) and
            runtime_state['Pid'] == request['helper']['pid'] and state.get('StartedAt') == runtime_state['StartedAt'],
            'fault running state is not bound to the prepared helper')
    preparation._inspect_observer({'id': helper_id, 'inspect': portable_created}, request, prep_value['create_spec'])
    preparation._inspect_observer({'id': helper_id, 'inspect': portable_running}, request, prep_value['create_spec'])
    preparation._observer_status(rows[0]['value']['self_status'], portable_running['apparmor'],
                                 prep_value['coordinator_namespaces'])
    m.check(rows[0]['value']['self_status']['apparmor']['label'] == portable_running['apparmor'] + ' (enforce)' and
            not any(option.lower().startswith('apparmor') for option in running_hc.get('SecurityOpt') or []),
            'fault observer AppArmor profile is not enforced')
    preparation.validate_request_namespaces(request, prep_value['coordinator_namespaces'], rows[0]['value'])
    preparation._launch_exit(prep_value['launch'], prep_value['exit'], request, success=False)
    captures = prep_value['captures']
    m.check(captures == prep_value['exit']['captures'] and captures == prep['exit']['captures'],
            'fault capture ledger differs from the supervisor exit')
    preparation.validate_capture_ledger(captures, request, prep_value['launch']['session_nonce'],
        success=False, preparer_exe=prep_value['launch']['exe'], root_pid=os.getpid(),
        coordinator_namespaces=prep_value['coordinator_namespaces'], preparer_pid=prep_value['launch']['pid'])
    intent_names = ['capture-intent-%06d-%d.json' % (inp['ordinal'], index + 1)
                    for index in range(len(captures))]
    actual_intents = sorted(name for name in os.listdir('/lab/control')
                            if name.startswith('capture-intent-%06d-' % inp['ordinal']))
    m.check(actual_intents == intent_names, 'fault durable capture intent prefix differs')
    intents = {name: control_receipt(m, name) for name in intent_names}
    for name, record in zip(intent_names, captures):
        m.check(m.handoff.same_json(intents[name], record['intent']) and
                hashlib.sha256(preparation.canonical_json(intents[name])).hexdigest() == record['intent_sha256'],
                'fault durable intent differs from independent capture ledger')
    item['diagnostic']['capture_intents'] = intents
    control_names = ['capture-%s-%06d-%d.json' % (kind, inp['ordinal'], index + 1)
                     for index in range(len(captures)) for kind in ('launch', 'exit')]
    actual_controls = sorted(name for name in os.listdir('/lab/control') if any(
        name.startswith('capture-%s-%06d-' % (kind, inp['ordinal'])) for kind in ('launch', 'exit')))
    m.check(actual_controls == sorted(control_names), 'fault durable capture control prefix differs')
    controls = {name: control_receipt(m, name) for name in control_names}
    for index, record in enumerate(captures):
        for kind in ('launch', 'exit'):
            name = 'capture-%s-%06d-%d.json' % (kind, inp['ordinal'], index + 1)
            expected = {'version': 1, 'op': 'capture_' + kind, 'binding': record['intent']['binding'],
                        kind: record[kind]}
            m.check(m.handoff.same_json(controls[name], expected),
                    'fault durable capture control differs from supervisor ledger')
    item['diagnostic']['capture_controls'] = controls
    m.check(inp['run_token'] == m.RESULT['run_token'] and inp['imageID'] == image and inp['case'] == 'I1' and
            inp['profile'] == profile and inp['phase'] == 'before' and inp['roots'][0]['source'] == source and
            request['helper']['id'] == helper_id and request['target']['id'] == target_id and
            request['target']['pid'] == item['consumer_before']['pid'] and
            request['target']['starttime'] == item['consumer_before']['process_stat']['starttime'] and
            request['target']['destination'] == '/data' and
            request['daemon']['pid'] == item['source_before']['daemon_stat']['pid'] and
            prep_value['observer_waiting'] == rows[0]['value'] and
            prep_value['helper_state']['Pid'] == request['helper']['pid'] and
            prep_value['helper_state']['Running'] is True and
            rows[0]['value']['self_status']['namespaces']['mnt'] == request['helper']['mntns'] and
            rows[0]['value']['nonce'] == inp['nonce'] and rows[0]['value']['input_digest'] == request['input_digest'],
            'fault observation is not bound to the dedicated target and request')
    m.check(prep_value['result'] is None and prep_value['raw_evidence'] is None and
            prep_value['pidfd_exited'] is True and prep['pidfd_exited'] is True and
            type(prep_value['channel_error']) is dict and prep_value['channel_error'] == prep['channel_error'] and
            prep_value['test_fault'] is None and prep_value == prep['receipt_value'] and
            hashlib.sha256(preparation.canonical_json(prep_value) + b'\n').hexdigest() == prep['receipt_sha256'] and
            prep_value['launch'] == prep['launch'] and prep_value['exit'] == prep['exit'] and
            inp['preparer_sha256'] == m.PREPARER_SHA256 == m.RESULT['isolation']['preparer_install']['copy_sha256'] and
            prep['launch']['exe'] == m.RESULT['isolation']['preparer_install']['copy'] and
            prep['launch']['session_nonce'] == m.RESULT['isolation']['coordinator']['session_nonce'] and
            prep_value['coordinator_namespaces'] == m.RESULT['isolation']['fixture_namespaces'],
            'fault preparer result, receipt, guest binding, or pidfd exit evidence missing')
    supervisor = docker.supervisor
    m.check(len(supervisor.fault_receipts) == counts['faults'] + 1 and
            len(supervisor.launches) == counts['launches'] + 1 and len(supervisor.exits) == counts['exits'] + 1,
            'owned supervisor fault, launch, or exit receipt missing')
    fault = supervisor.fault_receipts[-1]
    item['fault_receipt'] = fault
    launch, exited = supervisor.launches[-1], supervisor.exits[-1]
    item['supervisor_launch'] = launch
    item['supervisor_exit'] = exited
    status = fault['wait_status']
    m.check(fault['point'] == point and fault['pid'] == launch['pid'] == exited['pid'] and
            launch == prep_value['launch'] and exited == prep_value['exit'] and
            exited['ordinal'] == fault['ordinal'] and exited['nonce'] == fault['nonce'] and
            exited['wait_status'] == status and exited['exit_code'] is None and type(status) is int and
            os.WIFSIGNALED(status) and os.WTERMSIG(status) == signal.SIGKILL and
            type(fault['signaled_monotonic_ns']) is int and fault['signaled_monotonic_ns'] > 0 and
            type(fault['exit_observed_monotonic_ns']) is int and
            fault['exit_observed_monotonic_ns'] >= fault['signaled_monotonic_ns'] and
            supervisor.active is None and supervisor.test_arm is None,
            'owned pidfd signal and waitpid did not prove SIGKILL')
    validate_fault_progress(m, fault, request, item['source_before'])
    retirement = control_receipt(m, 'retired-' + helper_id + '.json')
    item['retirement'] = retirement
    check_retirement(m, retirement, helper_id)
    item['diagnostic']['removal_inspect'] = owned_absence(m, helper_id)
    intent = 'ready-intent-%06d.json' % inp['ordinal']
    item['diagnostic']['ready_intent_name'] = intent
    m.check(not os.path.lexists('/lab/control/' + intent), 'ready intent exists for killed preparer')


def case_kill(m, docker, image, profile, point, item):
    source = '/lab/ext/kill-%s-%s' % (profile.lower(), point)
    os.mkdir(source)
    with open(source + '/g2-readonly-sentinel', 'xb') as out:
        out.write(m.SENTINEL)
    cid = bind_consumer(m, docker, image, 'I1', profile, source, 'kill-' + point.replace('_', '-'))
    pidfd = None
    pid = None
    begun = False
    unsafe = False
    daemon_pid = m.RESULT['cases']['I0'][profile]['observations']['daemon']['bootstrap']['sender']['pid']
    try:
        initial = fixture_container(m, cid)
        pid = initial['pid']
        pidfd = os.pidfd_open(pid)
        item['consumer_before'] = held_consumer_truth(m, cid, pid, pidfd)
        item['source_before'] = kill_source_truth(m, source, daemon_pid, pid)
        item['sentinel_before'] = kill_sentinel_truth(m, source, pid)
        item['ready_intents_before'] = ready_intents()
        m.check(item['source_before']['source'] == item['source_before']['actual'] and
                item['sentinel_before']['source'] == item['sentinel_before']['actual'] and
                item['source_before']['target_stat'] == item['consumer_before']['process_stat'],
                'dedicated target does not hold the original source and sentinel')
        item['begin'] = docker.rpc({'op': 'target_begin', 'id': cid, 'destination': '/data'})
        begun = True
        m.check(item['begin']['id'] == cid and item['begin']['pid'] == pid and
                item['begin']['started_at'] == item['consumer_before']['started_at'] and
                item['begin']['source'] == source and item['begin']['destination'] == '/data' and
                item['begin']['stat'] == item['consumer_before']['process_stat'] and
                item['begin']['pidfd_alive'] is True, 'coordinator target differs from held root pidfd')
        supervisor = docker.supervisor
        counts = {'faults': len(supervisor.fault_receipts), 'launches': len(supervisor.launches),
                  'exits': len(supervisor.exits)}
        supervisor.arm_test_kill(point)
        try:
            observed = docker.rpc({'op': 'target_probe', 'id': cid, 'image': image,
                                   'profile': profile, 'case': 'I1'})
        except m.CoordinatorFailure as exc:
            item['diagnostic'] = {'transport_error': str(exc), 'rejection_evidence': exc.evidence}
            if len(supervisor.fault_receipts) == counts['faults'] + 1:
                item['fault_receipt'] = supervisor.fault_receipts[-1]
            if len(supervisor.launches) == counts['launches'] + 1:
                item['supervisor_launch'] = supervisor.launches[-1]
            if len(supervisor.exits) == counts['exits'] + 1:
                item['supervisor_exit'] = supervisor.exits[-1]
            kill_diagnostic(m, docker, exc, item, point, profile, image, cid, source, counts)
        else:
            item['unexpected_observation'] = observed
            raise m.Failure('faulted preparer unexpectedly produced an observation')
    except m.GuardUnsafe:
        unsafe = True
        raise
    finally:
        try:
            if pidfd is not None:
                finally_capture(item, 'source_after', lambda: kill_source_truth(m, source, daemon_pid, pid))
                finally_capture(item, 'sentinel_after', lambda: kill_sentinel_truth(m, source, pid))
                finally_capture(item, 'consumer_after', lambda: held_consumer_truth(m, cid, pid, pidfd))
            finally_capture(item, 'ready_intents_after', ready_intents)
            if not unsafe and (docker.supervisor.test_arm is not None or docker.supervisor.active is not None):
                unsafe = True
                raise m.GuardUnsafe('fault arm or owned preparer remains pending')
            if not unsafe:
                try:
                    if 'source_before' in item:
                        m.check(not item.get('truth_errors') and item['source_before'] == item.get('source_after') and
                                item['sentinel_before'] == item.get('sentinel_after') and
                                item['consumer_before'] == item.get('consumer_after') and
                                item['ready_intents_before'] == item.get('ready_intents_after'),
                                'fault fixture source, target, sentinel, or ready-intent ledger changed')
                finally:
                    try:
                        if begun:
                            item['target_abort'] = docker.rpc({'op': 'target_abort'})
                            m.check(item['target_abort'] == {'aborted': True}, 'target pidfd retirement failed')
                        item['target_remove'] = docker.run(['rm', cid], timeout=30)
                        item['target_retirement'] = control_receipt(m, 'retired-' + cid + '.json')
                        check_retirement(m, item['target_retirement'], cid)
                        item['target_removal_inspect'] = owned_absence(m, cid)
                    except m.GuardUnsafe:
                        raise
                    except Exception as exc:
                        raise m.GuardUnsafe('dedicated target retirement uncertain: ' + str(exc)) from exc
        finally:
            if pidfd is not None:
                os.close(pidfd)
    required(item, ['fault_position', 'preparer_killed', 'no_ready_sent', 'observer_retired',
                    'source_graph_unchanged', 'sentinels_unchanged', 'consumer_unchanged'])


def run(m, docker):
    info = json.loads(docker.run(['info', '--format', '{{json .}}'])['stdout'])
    version = json.loads(docker.run(['version', '--format', '{{json .}}'])['stdout'])
    m.check(info.get('Driver') == 'overlay2' and version.get('Server', {}).get('Version') == '29.8.2',
            'wrong daemon version or driver')
    m.RESULT['versions']['engine'] = version
    m.RESULT['isolation']['docker_info'] = info
    image = m.image_import(docker)
    inspected_image = json.loads(docker.run(['image', 'inspect', image])['stdout'])
    m.check(len(inspected_image) == 1 and inspected_image[0]['Id'] == image, 'probe image identity mismatch')
    m.RESULT['isolation']['image'] = image
    ext = '/lab/ext/c0'
    os.mkdir(ext)
    with open(ext + '/g2-readonly-sentinel', 'xb') as out:
        out.write(m.SENTINEL)
    m.must(['/usr/bin/btrfs', 'subvolume', 'create', '/lab/btr/p'])
    m.must(['/usr/bin/btrfs', 'subvolume', 'create', '/lab/btr/q'])
    btr = '/lab/btr/p'
    volumes = []
    consumers = []
    v1_pidfds = []
    try:
        for profile in PROFILES:
            case_result(m, 'C0', profile, lambda item, p=profile: case_c0(m, docker, image, p, ext, btr, item))
        for variant in ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce', 'wrong_input_digest', 'extra_fd'):
            for profile in PROFILES:
                preparation_result(m, variant, profile,
                                   lambda item, p=profile, v=variant: case_negative(m, docker, image, p, v, item),
                                   ('C0',))
        if any(m.RESULT['cases']['C0'][p]['status'] == 'PASS' for p in PROFILES):
            setup_error = None
            try:
                named = make_volume(m, docker, 'v1-named')
                cid = m.consumer(docker, image, 'g2-' + m.RESULT['run_token'] + '-v1-named', 'V1',
                                 ['--mount', 'type=volume,source=' + named['name'] + ',target=/data,volume-nocopy'])
                consumers.append(cid)
                docker.rpc({'op': 'watch_add', 'id': cid})
                anonymous, anonymous_cid = anonymous_fixture(m, docker, image)
                consumers.append(anonymous_cid)
                volumes = [named, anonymous]
                m.RESULT['isolation']['v1_consumers'] = consumers
                for consumer_id in consumers:
                    current = fixture_container(m, consumer_id)
                    fd = os.pidfd_open(current['pid'])
                    v1_pidfds.append((consumer_id, current['pid'], fd))
                m.RESULT['isolation']['v1_root_held_initial'] = [
                    held_consumer_truth(m, consumer_id, pid, fd) for consumer_id, pid, fd in v1_pidfds]
            except Exception as exc:
                setup_error = str(exc)
            for profile in PROFILES:
                def v1(item, p=profile):
                    m.check(setup_error is None, 'V1 fixture setup failed: ' + str(setup_error))
                    case_v1(m, docker, image, p, volumes, consumers, btr, item)
                case_result(m, 'V1', profile, v1, ('C0',))
            setup_error = None
            try:
                nested = make_volume(m, docker, 'v2-nested')
                os.mkdir(nested['path'] + '/nested')
                nested_source = btr + '/nested-source'
                os.mkdir(nested_source)
                with open(nested['path'] + '/g2-readonly-sentinel', 'xb') as out:
                    out.write(m.SENTINEL)
                with open(nested_source + '/g2-readonly-sentinel', 'xb') as out:
                    out.write(m.SENTINEL + b'nested\n')
                m.bind(nested_source, nested['path'] + '/nested')
            except Exception as exc:
                setup_error = str(exc)
            for profile in PROFILES:
                def v2(item, p=profile):
                    m.check(setup_error is None, 'V2 fixture setup failed: ' + str(setup_error))
                    case_v2(m, docker, image, p, nested, nested_source, item)
                case_result(m, 'V2', profile, v2, ('C0',))
            for profile in PROFILES:
                def v3(item, p=profile):
                    empty = make_volume(m, docker, 'v3-' + p.lower())
                    case_v3(m, docker, image, p, empty, item)
                case_result(m, 'V3', profile, v3, ('C0',))
        else:
            for case in ('V1', 'V2', 'V3'):
                for profile in PROFILES:
                    m.RESULT['cases'][case][profile]['observations'].update(
                        reason='dependency_not_passed', dependency='C0')
        for profile in PROFILES:
            case_result(m, 'I0', profile, lambda item, p=profile: case_i0(m, docker, image, p, item), ('C0',))
        for profile in PROFILES:
            source = '/lab/ext/i1-' + profile.lower()
            os.mkdir(source)
            case_result(m, 'I1', profile, lambda item, p=profile, s=source: case_i1(m, docker, image, p, s, item), ('I0',))
        for point in ('before_attach', 'after_first_attach'):
            for profile in PROFILES:
                preparation_result(m, point, profile,
                                   lambda item, p=profile, v=point: case_kill(m, docker, image, p, v, item),
                                   ('I0', 'I1'))
        for profile in PROFILES:
            source, alias = '/lab/ext/i2-' + profile.lower(), '/lab/ext/i2-alias-' + profile.lower()
            os.mkdir(source)
            os.mkdir(alias)
            case_result(m, 'I2', profile, lambda item, p=profile, s=source, a=alias: case_i2(m, docker, image, p, s, a,
                        '/lab/btr/q', item), ('I0',))
        for profile in PROFILES:
            paths = ['/lab/ext/i3-%s-%d' % (profile.lower(), i) for i in range(2)]
            for path in paths:
                os.mkdir(path)
            case_result(m, 'I3', profile, lambda item, p=profile, s=paths: case_i3(m, docker, image, p, s, item), ('I0',))
        if consumers:
            final = m.RESULT['isolation']
            finally_capture(final, 'v1_consumers_final', lambda: [fixture_container(m, cid) for cid in consumers])
            finally_capture(final, 'v1_uid_watches_final',
                            lambda: [docker.rpc({'op': 'watch_check', 'id': cid}) for cid in consumers])
            finally_capture(final, 'v1_root_held_final',
                            lambda: [held_consumer_truth(m, cid, pid, fd) for cid, pid, fd in v1_pidfds])
            if final.get('truth_errors'):
                for profile in PROFILES:
                    item = m.RESULT['cases']['V1'][profile]
                    if item['status'] == 'PASS':
                        item['status'] = 'FAIL'
                        item['observations']['reason'] = 'consumer_changed_after_observation'
    except m.GuardUnsafe:
        m.RESULT['uncertain'] = True
        raise
    finally:
        try:
            if not m.RESULT['uncertain']:
                cleanup = docker.rpc({'op': 'cleanup'})
                m.RESULT['isolation']['cleanup'] = cleanup
                if cleanup['watch_errors']:
                    for profile in PROFILES:
                        item = m.RESULT['cases']['V1'][profile]
                        if item['status'] == 'PASS':
                            item['status'] = 'FAIL'
                            item['observations']['reason'] = 'consumer_changed_before_cleanup'
                m.check(cleanup['remaining'] == 0, 'owned container cleanup incomplete')
        finally:
            for _, _, fd in v1_pidfds:
                os.close(fd)


READER_FAULT_SCENARIOS = ('before_capture_preparer_death', 'after_capture_preparer_death',
                          'supervisor_fork_death', 'supervisor_launch_death', 'pidfd_open_failure',
                          'reader_timeout', 'reader_nonzero', 'restore_failure')


def _fault_alive(fd):
    return not select.select([fd], [], [], 0)[0]


def _fault_identity(pid):
    with open('/proc/%d/stat' % pid, encoding='ascii') as inp:
        raw = inp.read()
    fields = raw[raw.rfind(')') + 2:].split()
    return {'pid': pid, 'starttime': int(fields[19]), 'parent_pid': int(fields[1])}


def _fault_guard(processes):
    # A fresh open creates a competing OFD; the watcher never receives the guard.
    before = sorted(name for name, value in processes.items() if _fault_alive(value['fd']))
    fd = os.open('/lab/control/guard', os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        st = os.fstat(fd)
        blocked = False
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            blocked = True
        after = sorted(name for name, value in processes.items() if _fault_alive(value['fd']))
        return {'dev': st.st_dev, 'ino': st.st_ino, 'blocked': blocked,
                'alive_before': before, 'alive_after': after}
    finally:
        os.close(fd)


def _fault_control_files():
    result = {}
    total = 0
    directory = os.open('/lab/control', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for name in sorted(os.listdir(directory)):
            if not name.endswith('.json') or not name.startswith(
                    ('capture-', 'prepare-', 'ready-intent-', 'retired-', 'helper-lifecycle-')):
                continue
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            with os.fdopen(fd, 'rb') as inp:
                info = os.fstat(inp.fileno())
                preparation.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                                    info.st_uid == 10002 and not info.st_mode & 0o077,
                                    'fault control receipt identity')
                raw = inp.read(preparation.MAX_EVIDENCE + 1)
            total += len(raw)
            preparation.require(total <= preparation.MAX_EVIDENCE, 'fault control evidence limit')
            result[name] = {'sha256': hashlib.sha256(raw).hexdigest(),
                            'value': preparation.strict_json(raw)}
        return result
    finally:
        os.close(directory)


def _fault_wait_unresolved(binding, deadline):
    # Watch completion of the independent coordinator's durable failure receipt;
    # root pidfd readiness alone does not mean it has processed channel EOF yet.
    libc = ctypes.CDLL(None, use_errno=True)
    libc.inotify_init1.argtypes = [ctypes.c_int]
    libc.inotify_init1.restype = ctypes.c_int
    libc.inotify_add_watch.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_uint32]
    libc.inotify_add_watch.restype = ctypes.c_int
    fd = libc.inotify_init1(os.O_CLOEXEC | os.O_NONBLOCK)
    preparation.require(fd >= 0, 'fault receipt notification unavailable')
    try:
        preparation.require(libc.inotify_add_watch(fd, b'/lab/control', 0x8 | 0x80) >= 0,
                            'fault receipt directory watch failed')
        name = '/lab/control/capture-uncertain-%06d-%d.json' % (binding['ordinal'], binding['sequence'])
        while time.monotonic() < deadline:
            try:
                inp = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            except FileNotFoundError:
                inp = None
            if inp is not None:
                with os.fdopen(inp, 'rb') as stream:
                    raw = stream.read(preparation.MAX_FRAME + 1)
                try:
                    value = preparation.strict_json(raw)
                except preparation.ProtocolError:
                    value = None
                if value is not None:
                    preparation.require(value.get('capture', {}).get('intent', {}).get('binding') == binding and
                                        value.get('launch_outcome') in ('unresolved', 'exit_unresolved'),
                                        'fault unresolved receipt binding')
                    return
            preparation.require(select.select([fd], [], [], max(0, deadline - time.monotonic()))[0],
                                'fault unresolved receipt deadline')
            os.read(fd, 65536)
        raise preparation.ProtocolError('fault unresolved receipt deadline')
    finally:
        os.close(fd)


def _fault_source(m, fixture):
    return {'source': m.stat_identity(fixture['source']),
            'sentinel': sentinel_truth(m, fixture['source'] + '/g2-readonly-sentinel'),
            'actual': m.stat_identity('/proc/%d/root/data' % fixture['target_pid']),
            'actual_sentinel': sentinel_truth(m, '/proc/%d/root/data/g2-readonly-sentinel' % fixture['target_pid']),
            'daemon_mountinfo': full_mountinfo(m, '/proc/%d/mountinfo' % fixture['daemon_pid']),
            'target_mountinfo': full_mountinfo(m, '/proc/%d/mountinfo' % fixture['target_pid'])}


def _fault_pin_helper(request):
    helper = request['helper']
    pid = preparation.integer(helper['pid'], 'fault helper PID', 1, (1 << 31) - 1)
    starttime = preparation.integer(helper['starttime'], 'fault helper starttime', 1)
    before = _fault_identity(pid)
    preparation.require(before['starttime'] == starttime, 'fault helper identity differs')
    fd = os.pidfd_open(pid)
    try:
        preparation.validate_pidfd(fd, pid)
        preparation.require(_fault_alive(fd) and _fault_identity(pid) == before, 'fault helper changed while pinning')
        return {'pid': pid, 'starttime': starttime, 'fd': fd}
    except BaseException:
        os.close(fd)
        raise


def _fault_error(report, exc, stage):
    if report['error'] is None:
        report['error'] = ('%s: %s' % (type(exc).__name__, exc))[:2048]
        report['failure_stage'] = stage


def _fault_exit_observation(m, report, processes, fixture, helper, message, worker):
    preparation.fields(message, ('version', 'op', 'scenario', 'binding', 'root_pid', 'preparer_pid',
                                 'reader_pid', 'reader_starttime', 'exit'), 'fault reader exit event')
    m.check(type(message['version']) is int and message['version'] == 1 and
            message['op'] == 'reader_fault_exit' and message['scenario'] == 'reader_nonzero' and
            report['scenario'] == 'reader_nonzero' and report['reader_exit_observation'] is None and
            report['worker_result'] is None and fixture is not None and helper is not None,
            'unexpected fault reader exit event')
    injection = report['injection']
    m.check(injection is not None and injection['scenario'] == 'reader_nonzero' and
            injection['point'] == 'reader_before' and
            preparation.canonical_json(message['binding']) == preparation.canonical_json(injection['binding']),
            'fault reader exit preceded its injection')
    binding = message['binding']
    _fault_binding_shape(binding)
    m.check(binding['phase'] == 'before' and type(binding['sequence']) is int and binding['sequence'] == 1,
            'fault reader exit phase differs')
    previous = [event for event in report['events'] if event['point'] == 'reader_before' and
                preparation.canonical_json(event['binding']) == preparation.canonical_json(binding)]
    m.check(len(previous) == 1 and 'preparer' in processes and 'reader-1' in processes and
            all(type(message[key]) is int for key in ('root_pid', 'preparer_pid', 'reader_pid', 'reader_starttime')) and
            message['root_pid'] == worker == processes['root']['pid'] and
            message['preparer_pid'] == previous[0]['preparer_pid'] == processes['preparer']['pid'] and
            message['reader_pid'] == previous[0]['reader_pid'] == processes['reader-1']['pid'] and
            message['reader_starttime'] == processes['reader-1']['starttime'], 'fault reader exit identity differs')
    request = previous[0]['request']
    m.check(helper['pid'] == request['helper']['pid'] and helper['starttime'] == request['helper']['starttime'],
            'fault helper pin differs from request')
    launch = {'binding': binding, 'host_pid': message['reader_pid'], 'starttime': message['reader_starttime']}
    preparation.capture_exit(message['exit'], launch, {'binding': binding}, request, binding['session_nonce'])
    exited = message['exit']
    m.check(exited['eof'] is True and exited['pidfd_exited'] is True and exited['terminal'] is False and
            exited['terminal_digest'] is None and type(exited['wait_status']) is int and
            exited['wait_status'] == 256 and type(exited['exit_code']) is int and exited['exit_code'] == 1 and
            not _fault_alive(processes['reader-1']['fd']), 'fault reader exit status or held pidfd differs')
    observation = {'event': message, 'reader_pidfd_exited': True,
                   'helper': {'pid': helper['pid'], 'starttime': helper['starttime'],
                              'pidfd_alive_before': None, 'pidfd_alive_after': None},
                   'source': None, 'guard_index': None, 'ack_sent': False}
    report['reader_exit_observation'] = observation
    try:
        observation['helper']['pidfd_alive_before'] = _fault_alive(helper['fd'])
        m.check(observation['helper']['pidfd_alive_before'], 'fault helper exited before source observation')
        observation['source'] = _fault_source(m, fixture)
        observation['helper']['pidfd_alive_after'] = _fault_alive(helper['fd'])
        m.check(observation['helper']['pidfd_alive_after'], 'fault helper exited during source observation')
        guard = _fault_guard(processes)
        observation['guard_index'] = len(report['guard_checks'])
        report['guard_checks'].append(guard)
        required = {'root', 'coordinator', 'preparer'}
        m.check(guard['blocked'] and required <= set(guard['alive_before']) and
                required <= set(guard['alive_after']) and 'reader-1' not in guard['alive_before'] and
                'reader-1' not in guard['alive_after'] and
                all((guard['dev'], guard['ino']) == (prior['dev'], prior['ino'])
                    for prior in report['guard_checks'][:-1]), 'fault exit guard responsibility differs')
        m.check(observation['source'] == report['source_before'], 'fault exit changed original source')
    except BaseException as exc:
        _fault_error(report, exc, 'watcher')
    return observation


def _fault_send_exit_ack(sock, root_fd, observation, deadline):
    event = observation['event']
    ack = {'version': 1, 'op': 'reader_fault_exit_ack', 'scenario': 'reader_nonzero',
           'binding': event['binding'], 'exit_sha256': hashlib.sha256(
               preparation.canonical_json(event['exit'])).hexdigest()}
    data = preparation.canonical_json(ack)
    while True:
        remaining = deadline - time.monotonic()
        preparation.require(remaining > 0 and _fault_alive(root_fd), 'fault exit ACK deadline or root exit')
        readable, writable, _ = select.select([sock, root_fd], [sock], [], remaining)
        preparation.require(root_fd not in readable and sock not in readable and sock in writable,
                            'fault exit ACK channel ended or received another frame')
        try:
            sent = sock.send(data, socket.MSG_DONTWAIT | getattr(socket, 'MSG_NOSIGNAL', 0))
        except BlockingIOError:
            continue
        preparation.require(sent == len(data), 'short fault exit ACK')
        observation['ack_sent'] = True
        return


def _fault_nonzero_receipts(m, report, processes, daemon_identity):
    files = report['control_files']
    event = report['reader_exit_observation']['event']
    request = report['events'][0]['request']
    session = event['binding']['session_nonce']
    ordinal = request['input']['ordinal']
    state = report['worker_result']['state']
    for entry in files.values():
        m.check(hashlib.sha256(preparation.canonical_json(entry['value']) + b'\n').hexdigest() == entry['sha256'],
                'fault control receipt is not canonical')
    name = 'prepare-result-%06d.json' % ordinal
    receipt = files[name]['value']
    preparation.fields(receipt, ('version', 'request', 'create_spec', 'observer_waiting', 'helper_state',
                       'coordinator_namespaces', 'launch', 'exit', 'captures', 'pidfd_exited', 'result',
                       'raw_evidence', 'channel_error', 'test_fault'), 'fault rejected preparation receipt')
    m.check(type(receipt['version']) is int and receipt['version'] == 1 and receipt['request'] == request and
            receipt['pidfd_exited'] is True and receipt['channel_error'] is None and receipt['test_fault'] is None,
            'fault rejected preparation receipt identity differs')
    preparation._validate_observer_create_spec(receipt['create_spec'], request)
    helper_state = receipt['helper_state']
    m.check(type(helper_state) is dict and helper_state.get('Running') is True and
            type(helper_state.get('Pid')) is int and helper_state['Pid'] == request['helper']['pid'] and
            type(helper_state.get('StartedAt')) is str and bool(helper_state['StartedAt']),
            'fault rejected helper state differs')
    preparation.validate_request_namespaces(request, receipt['coordinator_namespaces'], receipt['observer_waiting'])
    launch, exited = receipt['launch'], receipt['exit']
    preparation._launch_exit(launch, exited, request)
    m.check(launch['pid'] == processes['preparer']['pid'] and exited['wait_status'] == 0 and
            exited['exit_code'] == 0 and state['preparer'] is None and state['reader'] is None and
            state['preparer_exits'] == [exited] and state['captures'] == receipt['captures'] == exited['captures'] and
            len(receipt['captures']) == 1, 'fault preparer or capture ledger differs')
    result = receipt['result']
    evidence = preparation.validate_result(result, preparation.canonical_json(receipt['raw_evidence']), request,
                                           launch['pid'], session)
    m.check(result['status'] == 'rejected' and result['reason'] == 'prepare_exit_uncertain' and
            result['sources'] == [] and result['attached'] == [] and
            all(result['preparer']['exe'][key] == launch['exe'][key] for key in ('dev', 'ino', 'mode')),
            'fault preparer did not report the expected rejection')
    preparation.validate_reader_context(evidence, request, result['preparer'], receipt['coordinator_namespaces'],
                                        receipt['observer_waiting'], status='rejected')
    preparation.validate_capture_ledger(receipt['captures'], request, session, evidence, success=False,
                                        preparer_exe=launch['exe'], root_pid=processes['root']['pid'],
                                        coordinator_namespaces=receipt['coordinator_namespaces'],
                                        preparer_pid=launch['pid'])
    row = receipt['captures'][0]
    m.check(row['exit'] == event['exit'] and row['intent']['binding'] == event['binding'],
            'fault durable capture differs from watcher exit')
    binding = event['binding']
    expected = {'capture-intent-%06d-1.json' % ordinal: row['intent'],
                'capture-launch-%06d-1.json' % ordinal: {'version': 1, 'op': 'capture_launch',
                                                         'binding': binding, 'launch': row['launch']},
                'capture-exit-%06d-1.json' % ordinal: {'version': 1, 'op': 'capture_exit',
                                                       'binding': binding, 'exit': row['exit']}}
    m.check({key for key in files if key.startswith('capture-')} == set(expected) and
            all(files[key]['value'] == value for key, value in expected.items()),
            'fault durable capture receipts are incomplete or differ')
    injected = state['injected_frame']
    m.check(state['expected_failure'] == 'reader_barrier_binding' and type(injected) is dict and
            injected['version'] == 1 and injected['op'] == 'reader_allow_read' and
            injected['binding']['nonce'] != binding['nonce'] and
            all(injected['binding'][key] == binding[key] for key in binding if key != 'nonce'),
            'fault injected reader frame differs')
    m.check(evidence['daemon']['pid'] == daemon_identity['pid'] and
            evidence['daemon']['starttime'] == daemon_identity['starttime'], 'fault daemon evidence differs')
    for role, key in (('daemon', 'daemon_mountinfo'), ('target', 'target_mountinfo')):
        m.check(evidence[role] is not None and
                evidence[role]['before_mountinfo'] == evidence[role]['after_mountinfo'] == report['source_before'][key],
                'fault preparer source graph differs from independent observation')


def _validate_fault_bootstrap(m, value, daemon_pid):
    preparation.fields(value, ('sender', 'first_ping', 'second_ping', 'pidfd_alive', 'socket'),
                       'fault daemon bootstrap')
    preparation.integer(daemon_pid, 'fault daemon PID', 1, (1 << 31) - 1)
    for key in ('sender', 'first_ping', 'second_ping'):
        sender = value[key]
        preparation.fields(sender, ('pid', 'uid', 'gid'), 'fault daemon sender')
        m.check(type(sender['pid']) is int and sender['pid'] == daemon_pid and
                type(sender['uid']) is int and sender['uid'] == 0 and
                type(sender['gid']) is int and 0 <= sender['gid'] <= 0xffffffff, 'fault daemon sender differs')
    preparation.fields(value['socket'], ('dev', 'ino'), 'fault daemon socket')
    preparation.integer(value['socket']['dev'], 'fault daemon socket device')
    preparation.integer(value['socket']['ino'], 'fault daemon socket inode', 1)
    info = os.stat('/var/run/docker.sock', follow_symlinks=False)
    m.check(value['first_ping'] == value['second_ping'] == value['sender'] and
            value['pidfd_alive'] is True and stat.S_ISSOCK(info.st_mode) and info.st_uid == 0 and
            not info.st_mode & 0o007 and value['socket'] == {'dev': info.st_dev, 'ino': info.st_ino},
            'fault daemon socket or bootstrap identity differs')
    pidfd = os.pidfd_open(daemon_pid)
    try:
        preparation.validate_pidfd(pidfd, daemon_pid)
        identity = _fault_identity(daemon_pid)
        m.check(_fault_alive(pidfd) and _fault_identity(daemon_pid) == identity,
                'fault daemon exited or changed after bootstrap')
        return identity
    finally:
        os.close(pidfd)


def _fault_binding_shape(binding):
    preparation.fields(binding, ('version', 'run_token', 'session_nonce', 'ordinal', 'nonce', 'input_digest',
                                 'request_digest', 'phase', 'sequence'), 'fault state binding')
    preparation.require(type(binding['version']) is int and binding['version'] == 1 and
                        type(binding['sequence']) is int and
                        (binding['phase'], binding['sequence']) in (('before', 1), ('after', 2)),
                        'fault state binding version or phase')
    preparation.integer(binding['ordinal'], 'fault state ordinal', 1)
    preparation.hex_string(binding['run_token'], 'fault state run token', preparation.HEX32)
    for name in ('session_nonce', 'nonce', 'input_digest', 'request_digest'):
        preparation.hex_string(binding[name], 'fault state ' + name)


def _fault_process_shape(process):
    preparation.fields(process, ('pid', 'starttime', 'mntns', 'pidns', 'cgroupns', 'userns'),
                       'fault capture process')
    preparation.integer(process['pid'], 'fault capture process PID', 1, (1 << 31) - 1)
    preparation.integer(process['starttime'], 'fault capture process starttime', 1)
    for name in ('mntns', 'pidns', 'cgroupns', 'userns'):
        preparation.identity(process[name], 'fault capture process ' + name)


def _fault_capture_shape(record):
    preparation.fields(record, ('intent', 'intent_sha256', 'launch', 'exit'), 'fault capture state')
    intent = record['intent']
    preparation.fields(intent, ('version', 'op', 'binding', 'helper', 'metadata_owner'), 'fault state intent')
    preparation.require(type(intent['version']) is int and intent['version'] == 1 and
                        intent['op'] == 'capture_intent', 'fault state intent identity')
    _fault_binding_shape(intent['binding'])
    _fault_process_shape(intent['helper'])
    if intent['metadata_owner'] is not None:
        _fault_process_shape(intent['metadata_owner'])
    preparation.hex_string(record['intent_sha256'], 'fault state intent digest')
    preparation.require(record['intent_sha256'] == hashlib.sha256(preparation.canonical_json(intent)).hexdigest(),
                        'fault state intent digest differs')
    launch = record['launch']
    if launch is not None:
        preparation.fields(launch, ('binding', 'host_pid', 'starttime', 'exe', 'reader_pidns', 'root'),
                           'fault reader launch')
        preparation.require(launch['binding'] == intent['binding'], 'fault launch binding')
        preparation.integer(launch['host_pid'], 'fault reader launch PID', 1, (1 << 31) - 1)
        preparation.integer(launch['starttime'], 'fault reader launch starttime', 1)
        preparation.fields(launch['exe'], ('dev', 'ino', 'mode', 'sha256'), 'fault reader executable')
        preparation._object_identity({key: launch['exe'][key] for key in ('dev', 'ino', 'mode')},
                                     'fault reader executable')
        preparation.hex_string(launch['exe']['sha256'], 'fault reader executable digest')
        owner = intent['helper']['userns']
        preparation._namespace_evidence(launch['reader_pidns'], 'pid', owner)
        root = launch['root']
        preparation.fields(root, ('active_before', 'children_before', 'children_switched', 'active_after',
                           'children_after', 'tasks_before', 'tasks_after', 'sigchld_default',
                           'sigchld_nocldwait', 'signal_mask_restored'), 'fault root switch proof')
        for key in ('active_before', 'children_before', 'children_switched', 'active_after', 'children_after'):
            preparation._namespace_evidence(root[key], 'pid', owner)
        for key in ('tasks_before', 'tasks_after'):
            preparation._thread_inventory(root[key], 'fault root tasks')
        preparation.require(all(type(root[key]) is bool for key in
                                ('sigchld_default', 'sigchld_nocldwait', 'signal_mask_restored')),
                            'fault root switch flag types')
    exited = record['exit']
    if exited is not None:
        preparation.require(launch is not None, 'fault reader exit without launch')
        preparation.fields(exited, ('binding', 'host_pid', 'starttime', 'terminal', 'terminal_digest', 'eof',
                                    'pidfd_exited', 'wait_status', 'exit_code', 'diagnostic'), 'fault reader exit')
        preparation.integer(exited['host_pid'], 'fault reader exit PID', 1, (1 << 31) - 1)
        preparation.integer(exited['starttime'], 'fault reader exit starttime', 1)
        preparation.require(exited['binding'] == intent['binding'] and exited['host_pid'] == launch['host_pid'] and
                            exited['starttime'] == launch['starttime'] and
                            all(type(exited[key]) is bool for key in ('terminal', 'eof', 'pidfd_exited')),
                            'fault reader exit identity or flag types')
        if exited['terminal']:
            preparation.hex_string(exited['terminal_digest'], 'fault terminal digest')
        else:
            preparation.require(exited['terminal_digest'] is None, 'fault digest without terminal')
        status = preparation.integer(exited['wait_status'], 'fault reader wait status', 0, 0xffff)
        preparation.require((os.WIFEXITED(status) and type(exited['exit_code']) is int and
                             exited['exit_code'] == os.WEXITSTATUS(status)) or
                            (os.WIFSIGNALED(status) and exited['exit_code'] is None),
                            'fault reader exit status differs')
        diagnostic = preparation.ascii_string(exited['diagnostic'], 'fault reader diagnostic',
                                              4 * ((16 * 1024 + 2) // 3))
        try:
            decoded = base64.b64decode(diagnostic, validate=True)
        except (ValueError, TypeError) as exc:
            raise preparation.ProtocolError('fault reader diagnostic encoding') from exc
        preparation.require(len(decoded) <= 16 * 1024 and base64.b64encode(decoded).decode('ascii') == diagnostic,
                            'fault reader diagnostic size or encoding')


def _validate_fault_worker_result(message, scenario, run_token=None):
    preparation.fields(message, ('version', 'op', 'scenario', 'error', 'state'), 'fault worker result')
    preparation.require(type(message['version']) is int and message['version'] == 1 and
                        message['op'] == 'reader_fault_worker_result' and message['scenario'] == scenario and
                        type(message['error']) is str and 0 < len(message['error']) <= 2048,
                        'fault worker result identity or error')
    state = message['state']
    if state is None:
        return
    preparation.fields(state, ('preparer', 'reader', 'captures', 'uncertain', 'faults', 'containment',
                               'preparer_exits', 'injected_frame', 'expected_failure'), 'fault supervisor state')
    preparation.require(type(state['uncertain']) is bool and type(state['captures']) is list and
                        len(state['captures']) <= 2 and type(state['faults']) is list and
                        len(state['faults']) <= 16 and type(state['preparer_exits']) is list and
                        len(state['preparer_exits']) <= 1, 'fault supervisor state types')
    active = state['preparer']
    if active is not None:
        preparation.fields(active, ('pid', 'ordinal', 'nonce', 'request_digest', 'pidfd_held'),
                           'fault active preparer')
        preparation.integer(active['pid'], 'fault preparer PID', 1, (1 << 31) - 1)
        preparation.integer(active['ordinal'], 'fault preparer ordinal', 1)
        preparation.hex_string(active['nonce'], 'fault preparer nonce')
        preparation.hex_string(active['request_digest'], 'fault preparer digest')
        preparation.require(type(active['pidfd_held']) is bool, 'fault preparer pidfd state')
    reader = state['reader']
    if reader is not None:
        preparation.fields(reader, ('pid', 'starttime', 'pidfd_held', 'stage', 'binding'), 'fault active reader')
        for key in ('pid', 'starttime'):
            if reader[key] is not None:
                preparation.integer(reader[key], 'fault reader ' + key, 1)
        preparation.require(type(reader['pidfd_held']) is bool and type(reader['stage']) is str and
                            0 < len(reader['stage']) <= 128, 'fault reader state types')
        _fault_binding_shape(reader['binding'])
    for record in state['captures']:
        _fault_capture_shape(record)
    for fault in state['faults']:
        preparation.fields(fault, ('scenario', 'point', 'binding', 'effect'), 'fault effect state')
        preparation.require(fault['scenario'] == scenario and type(fault['point']) is str and
                            bool(fault['point']) and type(fault['effect']) is str and bool(fault['effect']),
                            'fault effect identity')
        _fault_binding_shape(fault['binding'])
    containment = state['containment']
    if containment is not None:
        preparation.fields(containment, ('reader_pid', 'reader_pidfd_held', 'reader_signal_sent', 'reader_eof',
                           'reader_pidfd_exited', 'reader_wait_status', 'preparer_pid', 'preparer_pidfd_held',
                           'preparer_signal_sent', 'preparer_pidfd_exited', 'preparer_wait_status'),
                           'fault containment state')
        for key in ('reader_pidfd_held', 'reader_signal_sent', 'reader_eof', 'reader_pidfd_exited',
                    'preparer_pidfd_held', 'preparer_signal_sent', 'preparer_pidfd_exited'):
            preparation.require(type(containment[key]) is bool, 'fault containment flag')
        for key in ('reader_pid', 'preparer_pid'):
            if containment[key] is not None:
                preparation.integer(containment[key], 'fault containment PID', 1, (1 << 31) - 1)
        for key in ('reader_wait_status', 'preparer_wait_status'):
            if containment[key] is not None:
                preparation.integer(containment[key], 'fault containment wait status', 0, 0xffff)
    for exited in state['preparer_exits']:
        preparation.fields(exited, ('version', 'op', 'run_token', 'session_nonce', 'ordinal', 'nonce', 'pid',
                                    'wait_status', 'exit_code', 'capture', 'captures'), 'fault preparer exit')
        preparation.require(type(exited['version']) is int and exited['version'] == 1 and
                            exited['op'] == 'preparer_exit' and type(exited['capture']) is str and
                            type(exited['captures']) is list, 'fault preparer exit types')
        preparation.integer(exited['pid'], 'fault exited preparer PID', 1, (1 << 31) - 1)
        preparation.integer(exited['wait_status'], 'fault preparer wait status', 0, 0xffff)
        preparation.require(exited['exit_code'] is None or type(exited['exit_code']) is int,
                            'fault preparer exit code')
        preparation.hex_string(exited['run_token'], 'fault exited run token', preparation.HEX32)
        preparation.hex_string(exited['session_nonce'], 'fault exited session nonce')
        preparation.hex_string(exited['nonce'], 'fault exited nonce')
        preparation.integer(exited['ordinal'], 'fault exited ordinal', 1)
        preparation.require(len(exited['captures']) <= 2 and exited['captures'] == state['captures'] and
                            len(exited['capture']) <= 4 * 16 * 1024, 'fault preparer exit evidence differs')
    injected = state['injected_frame']
    if injected is not None:
        preparation.fields(injected, ('version', 'op', 'binding'), 'fault injected frame')
        preparation.require(type(injected['version']) is int and injected['version'] == 1 and
                            injected['op'] == 'reader_allow_read', 'fault injected frame identity')
        _fault_binding_shape(injected['binding'])
    preparation.require(state['expected_failure'] is None or
                        (type(state['expected_failure']) is str and 0 < len(state['expected_failure']) <= 128),
                        'fault expected failure type')
    bindings = [record['intent']['binding'] for record in state['captures']]
    bindings.extend(fault['binding'] for fault in state['faults'])
    if reader is not None:
        bindings.append(reader['binding'])
    if run_token is not None:
        preparation.hex_string(run_token, 'fault expected run token', preparation.HEX32)
        preparation.require(all(binding['run_token'] == run_token for binding in bindings) and
                            all(exited['run_token'] == run_token for exited in state['preparer_exits']),
                            'fault state run token differs')
    if bindings:
        first = bindings[0]
        preparation.require(all(all(binding[key] == first[key] for key in
                                    ('run_token', 'session_nonce', 'ordinal', 'nonce', 'input_digest',
                                     'request_digest')) for binding in bindings), 'fault state request tuple differs')
        if active is not None:
            preparation.require(all(active[key] == first[key] for key in ('ordinal', 'nonce', 'request_digest')),
                                'fault preparer tuple differs')
        for exited in state['preparer_exits']:
            preparation.require(all(exited[key] == first[key] for key in
                                    ('run_token', 'session_nonce', 'ordinal', 'nonce')),
                                'fault preparer exit tuple differs')


def _reader_fault_worker(m, endpoint, scenario, devs):
    # This is the actual RootSupervisor parent, not a reader monitor grandchild.
    m.READER_TEST_CONTROL = (endpoint, scenario)
    supervisor = None
    try:
        m.setup_disks(devs)
        gid, daemon, logger = m.install_and_start()
        coordinator_pid, sock, pidfd, supervisor = m.coordinator(gid)
        docker = m.Docker(sock, supervisor, pidfd, coordinator_pid)
        m.check(supervisor.active is None and supervisor.reader is None and supervisor.pending_capture is None and
                not supervisor.launches and not supervisor.reader_faults, 'fault bootstrap already has a reader')
        bootstrap = docker.rpc({'op': 'daemon_bootstrap'})
        _validate_fault_bootstrap(m, bootstrap, daemon.pid)
        m.check(supervisor.active is None and supervisor.reader is None and supervisor.pending_capture is None and
                not supervisor.launches and not supervisor.reader_faults, 'fault bootstrap launched a reader')
        image = m.image_import(docker)
        source = '/lab/ext/reader-fault-source'
        os.mkdir(source)
        with open(source + '/g2-readonly-sentinel', 'xb') as out:
            out.write(m.SENTINEL)
        cid = bind_consumer(m, docker, image, 'I1', 'A', source, 'reader-fault')
        target = fixture_container(m, cid)
        begun = docker.rpc({'op': 'target_begin', 'id': cid, 'destination': '/data'})
        fixture = {'version': 1, 'op': 'reader_fault_fixture', 'scenario': scenario,
                   'run_token': m.RESULT['run_token'], 'source': source, 'target_id': cid,
                   'target_pid': target['pid'], 'daemon_pid': daemon.pid,
                   'coordinator_pid': coordinator_pid, 'bootstrap': bootstrap, 'begin': begun}
        m.check(supervisor.active is None and supervisor.reader is None and supervisor.pending_capture is None and
                not supervisor.launches and not supervisor.reader_faults, 'reader event preceded fault fixture')
        preparation.send_frame(endpoint, fixture, (pidfd,))
        preparation.require(select.select([endpoint], [], [], 5)[0], 'fault fixture ACK deadline')
        ack, fds = preparation.recv_frame(endpoint, (os.getppid(), 0, 0), 0)
        preparation.require(ack == {'version': 1, 'op': 'reader_fault_fixture_ack',
                                    'scenario': scenario, 'run_token': m.RESULT['run_token']},
                            'fault fixture ACK mismatch')
        result = docker.rpc({'op': 'target_probe', 'id': cid, 'image': image, 'profile': 'A', 'case': 'I1'})
        raise m.Failure('reader fault unexpectedly returned helper result: ' + str(result)[:512])
    except BaseException as exc:
        current = supervisor if supervisor is not None else getattr(m, 'SUPERVISOR', None)
        state = None
        if current is not None:
            try:
                state = current.fault_state()
            except BaseException as state_error:
                print('G2 fault state unavailable: %s: %s' %
                      (type(state_error).__name__, str(state_error)[:2048]), file=sys.stderr, flush=True)
        message = {'version': 1, 'op': 'reader_fault_worker_result', 'scenario': scenario,
                   'error': ('%s: %s' % (type(exc).__name__, exc))[:2048], 'state': state}
        try:
            _validate_fault_worker_result(message, scenario, m.RESULT['run_token'])
            preparation.require(len(preparation.canonical_json(message)) <= preparation.MAX_FRAME,
                                'fault worker state exceeds frame limit')
        except (preparation.ProtocolError, TypeError, ValueError) as state_error:
            print('G2 fault state unavailable: %s: %s' %
                  (type(state_error).__name__, str(state_error)[:2048]), file=sys.stderr, flush=True)
            message['state'] = None
        preparation.send_frame(endpoint, message)
        # The outer watcher owns the explicit containment; retaining this stack
        # retains the root's same-OFD guard until its confirmed pidfd termination.
        select.select([endpoint], [], [], 30)
    finally:
        os._exit(71)


def run_reader_fault_vm(m, case, devs):
    scenario = case.removeprefix('reader-fault-')
    report = {'version': 2, 'experiment_kind': 'reader-fault-v2', 'run_token': m.RESULT['run_token'],
              'case': case, 'scenario': scenario, 'outcome': 'FAIL', 'uncertain': True, 'completed': False,
              'installation': m.RESULT['isolation']['preparer_install'], 'subreaper': False,
              'fixture': None, 'events': [], 'injection': None, 'guard_checks': [],
              'source_before': None, 'source_after': None, 'reader_exit_observation': None, 'control_files': {},
              'worker_result': None, 'processes': {}, 'error': None, 'failure_stage': None,
              'cleanup_errors': [], **m.BOUNDARY}
    parent = child = None
    processes = {}
    fixture = None
    helper = None
    daemon_identity = None
    stage = 'setup'
    deadline = time.monotonic() + 300
    source_pinned = False
    try:
        m.check(scenario in READER_FAULT_SCENARIOS, 'unknown reader fault scenario')
        libc = ctypes.CDLL(None, use_errno=True)
        libc.prctl.argtypes = [ctypes.c_int, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong]
        libc.prctl.restype = ctypes.c_int
        m.check(libc.prctl(36, 1, 0, 0, 0) == 0, 'enable fault watcher subreaper')
        active_subreaper = ctypes.c_int()
        m.check(libc.prctl(37, ctypes.addressof(active_subreaper), 0, 0, 0) == 0 and active_subreaper.value == 1,
                'fault watcher subreaper readback')
        report['subreaper'] = True
        parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
        parent.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        child.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
        worker = os.fork()
        if worker == 0:
            parent.close()
            _reader_fault_worker(m, child, scenario, devs)
            os._exit(71)
        child.close()
        child = None
        root_fd = os.pidfd_open(worker)
        try:
            # An unreaped direct child cannot have its PID reused, even if it has already exited.
            identity = _fault_identity(worker)
            processes['root'] = {'fd': root_fd, **identity}
        except BaseException:
            os.close(root_fd)
            raise
        stage = 'before_injection'
        while time.monotonic() < deadline:
            ready = select.select([parent, processes['root']['fd']], [], [],
                                  min(1, max(0, deadline - time.monotonic())))[0]
            if parent not in ready:
                if processes['root']['fd'] in ready:
                    # A queued datagram takes precedence over the root's exit.
                    if not select.select([parent], [], [], 0)[0]:
                        break
                else:
                    continue
            stage = 'watcher'
            try:
                message, rights = preparation.recv_frame(parent, (worker, 0, 0), None, allow_eof=True)
            except (preparation.ProtocolError, OSError, ValueError) as exc:
                if scenario != 'reader_nonzero' or report['injection'] is None:
                    raise
                _fault_error(report, exc, 'watcher')
                continue
            if message is None:
                stage = 'after_injection' if report['injection'] is not None else 'before_injection'
                break
            try:
                op = message.get('op')
                if op == 'reader_fault_fixture':
                    preparation.fields(message, ('version', 'op', 'scenario', 'run_token', 'source', 'target_id',
                                       'target_pid', 'daemon_pid', 'coordinator_pid', 'bootstrap', 'begin'),
                                       'fault fixture')
                    m.check(fixture is None and type(message['version']) is int and message['version'] == 1 and
                            message['scenario'] == scenario and message['run_token'] == m.RESULT['run_token'] and
                            len(rights) == 1 and message['source'] == '/lab/ext/reader-fault-source',
                            'fault fixture binding')
                    preparation.hex_string(message['target_id'], 'fault target ID')
                    preparation.integer(message['target_pid'], 'fault target PID', 1, (1 << 31) - 1)
                    preparation.integer(message['coordinator_pid'], 'fault coordinator PID', 1, (1 << 31) - 1)
                    daemon_identity = _validate_fault_bootstrap(m, message['bootstrap'], message['daemon_pid'])
                    begin = message['begin']
                    preparation.fields(begin, ('id', 'pid', 'started_at', 'source', 'destination', 'stat',
                                               'pidfd_alive'), 'fault target begin')
                    preparation.fields(begin['stat'], ('pid', 'starttime'), 'fault target stat')
                    target = _fault_identity(message['target_pid'])
                    m.check(begin['id'] == message['target_id'] and type(begin['pid']) is int and
                            begin['pid'] == message['target_pid'] and begin['source'] == message['source'] and
                            begin['destination'] == '/data' and begin['pidfd_alive'] is True and
                            type(begin['started_at']) is str and bool(begin['started_at']) and
                            begin['started_at'] != '0001-01-01T00:00:00Z' and
                            begin['stat'] == {'pid': target['pid'], 'starttime': target['starttime']},
                            'fault target begin differs from fixture')
                    preparation.validate_pidfd(rights[0], message['coordinator_pid'])
                    m.check(_fault_alive(rights[0]), 'fault coordinator pidfd exited')
                    identity = _fault_identity(message['coordinator_pid'])
                    m.check(identity['parent_pid'] == worker, 'fault coordinator parent')
                    processes['coordinator'] = {'fd': rights.pop(), **identity}
                    fixture = report['fixture'] = message
                    preparation.send_frame(parent, {'version': 1, 'op': 'reader_fault_fixture_ack',
                                           'scenario': scenario, 'run_token': m.RESULT['run_token']})
                    stage = 'before_injection'
                    continue
                if op == 'reader_fault_worker_result':
                    m.check(not rights, 'fault worker result has unexpected descriptors')
                    _validate_fault_worker_result(message, scenario, m.RESULT['run_token'])
                    m.check(report['worker_result'] is None, 'duplicate fault worker result')
                    report['worker_result'] = message
                    if report['injection'] is None and report['error'] is None:
                        report['error'] = message['error']
                        report['failure_stage'] = 'before_injection'
                    stage = 'after_injection' if report['injection'] is not None else 'before_injection'
                    break
                if op == 'reader_fault_exit':
                    observation = None
                    try:
                        m.check(report['error'] is None, 'fault exit channel already failed')
                        m.check(not rights, 'fault reader exit carried descriptors')
                        observation = _fault_exit_observation(m, report, processes, fixture, helper, message, worker)
                    except BaseException as exc:
                        _fault_error(report, exc, 'watcher')
                    if observation is not None:
                        try:
                            _fault_send_exit_ack(parent, processes['root']['fd'], observation, deadline)
                        except BaseException as exc:
                            _fault_error(report, exc, 'watcher')
                    stage = 'after_injection' if report['injection'] is not None else 'before_injection'
                    continue
                m.check(report['reader_exit_observation'] is None, 'fault point after reader exit observation')
                preparation.fields(message, ('version', 'op', 'point', 'scenario', 'binding', 'root_pid',
                                   'preparer_pid', 'reader_pid', 'request', 'helper', 'metadata_owner'), 'fault event')
                m.check(op == 'reader_fault_point' and fixture is not None and type(message['version']) is int and
                        message['version'] == 1 and message['scenario'] == scenario and
                        message['root_pid'] == worker and len(rights) in (2, 3), 'fault event identity')
                binding = message['binding']
                request = preparation.validate_request(message['request'])
                preparation.capture_intent({'version': 1, 'op': 'capture_intent', 'binding': binding,
                                            'helper': message['helper'], 'metadata_owner': message['metadata_owner']},
                                           request, binding['session_nonce'])
                m.check(binding['run_token'] == m.RESULT['run_token'] and request['input']['case'] == 'I1' and
                        request['input']['profile'] == 'A' and request['input']['metadata_pid'] == fixture['target_pid'] and
                        request['daemon']['pid'] == fixture['daemon_pid'] and request['target'] is not None and
                        request['target']['id'] == fixture['target_id'] and
                        request['target']['pid'] == fixture['target_pid'] and
                        request['target']['starttime'] == fixture['begin']['stat']['starttime'] and
                        request['target']['destination'] == fixture['begin']['destination'] and
                        request['input']['roots'][0]['source'] == fixture['source'],
                        'fault event request differs from bootstrap and target fixture')
                preparation.validate_pidfd(rights[0], worker)
                for role, pid, supplied in (('preparer', message['preparer_pid'], rights[1]),
                                            ('reader-' + str(binding['sequence']), message['reader_pid'],
                                             rights[2] if len(rights) == 3 else None)):
                    if supplied is not None:
                        preparation.validate_pidfd(supplied, pid)
                    identity = _fault_identity(pid)
                    m.check(identity['parent_pid'] == worker, 'fault child is not direct supervisor child')
                    if role not in processes:
                        # fork_registered has no reader pidfd yet: the watcher
                        # pins the registered direct child before permitting kill.
                        fd = os.pidfd_open(pid) if supplied is None else os.dup(supplied)
                        try:
                            preparation.validate_pidfd(fd, pid)
                            m.check(_fault_identity(pid) == identity, 'fault child identity changed while pinning')
                            processes[role] = {'fd': fd, **identity}
                        except BaseException:
                            os.close(fd)
                            raise
                    else:
                        m.check(all(processes[role][key] == identity[key] for key in identity), 'fault child changed')
                if not source_pinned:
                    helper = _fault_pin_helper(request)
                    report['source_before'] = _fault_source(m, fixture)
                    source_pinned = True
                guard = _fault_guard(processes)
                m.check(guard['blocked'], 'guard available while supervisor responsibility is live')
                report['guard_checks'].append(guard)
                m.check(message['point'] in ('fork_registered', 'before_restore', 'launch_registered',
                                             'reader_before', 'reader_after') and
                        all(event['request'] == request and
                            (event['binding']['sequence'], event['point']) != (binding['sequence'], message['point'])
                            for event in report['events']), 'fault event repeated or changed request')
                report['events'].append(message)
                point = message['point']
                phase = binding['phase']
                expected = {'supervisor_fork_death': 'fork_registered',
                            'supervisor_launch_death': 'launch_registered',
                            'pidfd_open_failure': 'fork_registered', 'restore_failure': 'before_restore'}
                match = point == expected.get(scenario, 'reader_before') and phase == (
                    'after' if scenario == 'after_capture_preparer_death' else 'before')
                action = 'continue'
                if match:
                    m.check(report['injection'] is None, 'duplicate fault injection')
                    report['injection'] = {'point': point, 'binding': binding, 'scenario': scenario,
                                           'signal': None, 'target': None, 'pidfd_bound': True}
                    stage = 'after_injection'
                    deadline = min(deadline, time.monotonic() + 20)
                    if scenario.startswith('supervisor_'):
                        report['injection'].update(signal=int(signal.SIGKILL), target='root')
                        signal.pidfd_send_signal(processes['root']['fd'], signal.SIGKILL)
                        continue
                    if scenario.endswith('preparer_death'):
                        report['injection'].update(signal=int(signal.SIGKILL), target='preparer')
                        signal.pidfd_send_signal(processes['preparer']['fd'], signal.SIGKILL)
                    else:
                        action = 'fault'
                preparation.send_frame(parent, {'version': 1, 'op': 'reader_fault_continue', 'point': point,
                                       'scenario': scenario, 'binding': binding, 'action': action})
                stage = 'after_injection' if report['injection'] is not None else 'before_injection'
            except BaseException as exc:
                if scenario != 'reader_nonzero' or report['injection'] is None:
                    raise
                _fault_error(report, exc, 'watcher')
                continue
            finally:
                for fd in rights:
                    os.close(fd)
        m.check(report['injection'] is not None, 'fault injection was not reached')
        if scenario.startswith('supervisor_'):
            m.check(not _fault_alive(processes['root']['fd']), 'supervisor death was not observed')
        else:
            m.check(report['worker_result'] is not None and report['worker_result']['state'] is not None,
                    'supervisor did not report bounded containment state')
            if not scenario.endswith('preparer_death'):
                effect = 'reader_nonzero_frame' if scenario == 'reader_nonzero' else scenario
                m.check(any(entry == {'scenario': scenario, 'point': report['injection']['point'],
                                      'binding': report['injection']['binding'], 'effect': effect}
                            for entry in report['worker_result']['state']['faults']),
                        'authorized fault effect was not observed')
        if scenario.startswith('supervisor_'):
            _fault_wait_unresolved(report['injection']['binding'], min(deadline, time.monotonic() + 5))
        if scenario == 'reader_nonzero':
            m.check(helper is not None and not _fault_alive(helper['fd']),
                    'helper was not retired before fault source readback')
        report['source_after'] = _fault_source(m, fixture)
        report['control_files'] = _fault_control_files()
        if scenario == 'reader_nonzero':
            observation = report['reader_exit_observation']
            m.check(observation is not None and observation['source'] == report['source_before'] and
                    observation['helper']['pidfd_alive_before'] is True and
                    observation['helper']['pidfd_alive_after'] is True and
                    type(observation['guard_index']) is int and observation['ack_sent'] is True,
                    'fault reader exit observation is incomplete')
            for key in ('source', 'sentinel', 'actual', 'actual_sentinel', 'target_mountinfo'):
                m.check(report['source_before'][key] == report['source_after'][key],
                        'reader fault changed original source ' + key)
            helper_id = report['events'][0]['request']['helper']['id']
            retired = report['control_files']['retired-' + helper_id + '.json']
            lifecycle = report['control_files']['helper-lifecycle-' + helper_id + '.json']
            value = preparation.validate_daemon_lifecycle(
                {'value': lifecycle['value'], 'sha256': lifecycle['sha256'],
                 'receipt': '/lab/control/helper-lifecycle-' + helper_id + '.json'}, helper_id,
                process=daemon_identity, retirement=retired['value'])
            m.check(value['after_mountinfo'] == report['source_after']['daemon_mountinfo'],
                    'fault daemon source readback differs from helper retirement')
            _fault_nonzero_receipts(m, report, processes, daemon_identity)
        else:
            m.check(report['source_before'] == report['source_after'], 'reader fault changed original source')
        m.check(not any(name.startswith('ready-intent-') for name in report['control_files']),
                'reader fault issued ready')
        intents = [entry['value'] for name, entry in report['control_files'].items()
                   if name.startswith('capture-intent-')]
        m.check(any(value.get('binding') == report['injection']['binding'] for value in intents),
                'fault lost durable capture intent')
        if not scenario.startswith('supervisor_'):
            state = report['worker_result']['state']
            request = report['events'][0]['request']
            session_nonce = report['events'][0]['binding']['session_nonce']
            for record in state['captures']:
                intent = preparation.capture_intent(record['intent'], request, session_nonce)
                m.check(any(m.handoff.same_json(value, intent) for value in intents),
                        'supervisor capture lacks its independent durable intent')
                if record['launch'] is not None:
                    preparation.capture_launch(record['launch'], intent, request, session_nonce,
                        root_pid=processes['root']['pid'], preparer_pid=processes['preparer']['pid'])
                    if record['exit'] is not None:
                        preparation.capture_exit(record['exit'], record['launch'], intent, request, session_nonce)
                        if scenario == 'reader_nonzero':
                            m.check(record['exit'] == report['reader_exit_observation']['event']['exit'],
                                    'fault watcher and supervisor reader exits differ')
            active = state['preparer']
            if active is not None:
                m.check(active['pid'] == processes['preparer']['pid'] and
                        active['ordinal'] == request['input']['ordinal'] and
                        active['nonce'] == request['input']['nonce'] and
                        active['request_digest'] == request['request_digest'],
                        'fault active preparer differs from the actual request')
            reader = state['reader']
            if reader is not None and reader['pid'] is not None:
                role = 'reader-' + str(reader['binding']['sequence'])
                m.check(role in processes and reader['pid'] == processes[role]['pid'] and
                        (reader['starttime'] is None or reader['starttime'] == processes[role]['starttime']) and
                        any(event['binding'] == reader['binding'] and event['reader_pid'] == reader['pid']
                            for event in report['events']), 'fault active reader differs from the observed process')
            for exited in state['preparer_exits']:
                m.check(exited['pid'] == processes['preparer']['pid'] and exited['run_token'] == m.RESULT['run_token'] and
                        exited['session_nonce'] == session_nonce and exited['ordinal'] == request['input']['ordinal'] and
                        exited['nonce'] == request['input']['nonce'], 'fault preparer exit differs from request')
            containment = state['containment']
            if containment is not None:
                m.check(containment['preparer_pid'] is None or
                        containment['preparer_pid'] == processes['preparer']['pid'],
                        'fault containment preparer differs')
                m.check(containment['reader_pid'] is None or any(
                    role.startswith('reader-') and value['pid'] == containment['reader_pid']
                    for role, value in processes.items()), 'fault containment reader differs')
        guard = _fault_guard(processes)
        report['guard_checks'].append(guard)
        if set(guard['alive_before']) & set(guard['alive_after']):
            m.check(guard['blocked'], 'live responsibility released guard')
        if report['error'] is None:
            report['outcome'] = 'EXPECTED_UNCERTAIN'
    except BaseException as exc:
        _fault_error(report, exc, stage)
    finally:
        def cleanup_error(message):
            message = str(message)[:2048] or 'unknown cleanup failure'
            if len(report['cleanup_errors']) < 32:
                report['cleanup_errors'].append(message)
            report['outcome'] = 'FAIL'
            if report['error'] is None:
                report['error'] = message
                report['failure_stage'] = 'cleanup'

        if report['error'] is not None and fixture is not None and source_pinned:
            if (report['source_after'] is None and
                    (scenario != 'reader_nonzero' or (helper is not None and not _fault_alive(helper['fd'])))):
                try:
                    report['source_after'] = _fault_source(m, fixture)
                except BaseException as exc:
                    cleanup_error('fault source readback: %s: %s' % (type(exc).__name__, exc))
            if not report['control_files']:
                try:
                    report['control_files'] = _fault_control_files()
                except BaseException as exc:
                    cleanup_error('fault control readback: %s: %s' % (type(exc).__name__, exc))
        if helper is not None:
            try:
                os.close(helper['fd'])
            except OSError as exc:
                cleanup_error('close helper pidfd: %s: %s' % (type(exc).__name__, exc))
        # Signal only pinned processes; root exits first so surviving children can be reaped here.
        for role in ('root', 'coordinator', 'preparer') + tuple(
                key for key in processes if key.startswith('reader-')):
            if role not in processes:
                continue
            try:
                if _fault_alive(processes[role]['fd']):
                    signal.pidfd_send_signal(processes[role]['fd'], signal.SIGKILL)
            except ProcessLookupError:
                pass
            except BaseException as exc:
                cleanup_error('signal %s: %s: %s' % (role, type(exc).__name__, exc))
        join_deadline = time.monotonic() + 5
        for role, value in processes.items():
            exited = False
            status = None
            reaped = False
            try:
                exited = bool(select.select([value['fd']], [], [], max(0, join_deadline - time.monotonic()))[0])
                observation = report['reader_exit_observation']
                reaped_by_root = (role == 'reader-1' and observation is not None and
                                  observation['event']['reader_pid'] == value['pid'])
                if exited and not reaped_by_root:
                    try:
                        child_pid, child_status = os.waitpid(value['pid'], os.WNOHANG)
                        if child_pid == value['pid']:
                            reaped = True
                            status = child_status
                    except ChildProcessError:
                        pass
            except BaseException as exc:
                cleanup_error('wait %s: %s: %s' % (role, type(exc).__name__, exc))
            report['processes'][role] = {key: value[key] for key in ('pid', 'starttime', 'parent_pid')}
            report['processes'][role].update(pidfd_exited=exited, wait_status=status,
                                             reaped_by_watcher=reaped)
            try:
                os.close(value['fd'])
            except OSError as exc:
                cleanup_error('close %s pidfd: %s: %s' % (role, type(exc).__name__, exc))
            if not exited:
                cleanup_error('registered %s exit remained uncertain' % role)
        if scenario == 'reader_nonzero' and report['worker_result'] is not None and parent is not None:
            try:
                if select.select([parent], [], [], 0)[0]:
                    extra, rights = preparation.recv_frame(parent, (worker, 0, 0), None, allow_eof=True)
                    for fd in rights:
                        os.close(fd)
                    if extra is not None:
                        cleanup_error('fault message after worker result')
            except BaseException as exc:
                cleanup_error('fault channel after worker result: %s: %s' % (type(exc).__name__, exc))
        for endpoint in (parent, child):
            if endpoint is not None:
                try:
                    endpoint.close()
                except OSError as exc:
                    cleanup_error('close fault channel: %s: %s' % (type(exc).__name__, exc))
        if 'root' in processes and report['processes']['root']['reaped_by_watcher'] is not True:
            cleanup_error('root direct wait evidence missing')
        if scenario.startswith('supervisor_') and not all(
                value['reaped_by_watcher'] for role, value in report['processes'].items() if role.startswith('reader-')):
            cleanup_error('orphan reader subreaper wait evidence missing')
        payload = preparation.canonical_json(report)
        m.check(len(payload) <= preparation.MAX_EVIDENCE, 'fault result exceeds evidence limit')
        chunks = [payload[offset:offset + 3072] for offset in range(0, len(payload), 3072)]
        batch_deadline = time.monotonic() + 30
        for index, chunk in enumerate(chunks):
            m.emit_protocol('G2LAB-DATA %s %d/%d %s' % (m.RESULT['run_token'], index, len(chunks),
                                                        base64.b64encode(chunk).decode('ascii')), batch_deadline)
        m.emit_protocol('G2LAB-END %s %s' % (m.RESULT['run_token'], hashlib.sha256(payload).hexdigest()),
                        batch_deadline)
