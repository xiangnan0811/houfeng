#!/usr/bin/env python3
"""Check that real, committed BPF evidence rejects targeted semantic changes."""
import argparse
import copy
import hashlib
import json
import os
import sys

import source_bpf_verify
import source_build
import source_host
import source_verify
import source_vm


class NotReady(Exception):
    pass


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')


def _replace_hashes(value, mapping):
    if isinstance(value, dict):
        for key in value:
            value[key] = _replace_hashes(value[key], mapping)
    elif isinstance(value, list):
        for i, item in enumerate(value):
            value[i] = _replace_hashes(item, mapping)
    elif isinstance(value, str):
        return mapping.get(value, value)
    return value


def _rehash(result):
    """Recompute each affected transcript in its original domain and its references."""
    prefix = result.get('bpf_bootstrap')
    suffix = result.get('bpf_authority')
    partial = result.get('bpf_healthy_partial')
    mapping = {}
    if isinstance(prefix, dict) and (isinstance(suffix, dict) or isinstance(partial, dict)):
        original_prefix = prefix['records']
        records = original_prefix + suffix['records'] if isinstance(suffix, dict) else partial['records']
        old_hashes = [event['previous'] for event in records[1:]]
        old_hashes.append(suffix['final_tail'] if isinstance(suffix, dict) else partial['tail_sha256'])
        previous = '0' * 64
        for event, old in zip(records, old_hashes):
            _replace_hashes(event, mapping)
            event['previous'] = previous
            previous = hashlib.sha256(b'HOUFENG-G2-BPF-V1\0' + _canonical(event)).hexdigest()
            mapping[old] = previous
        if isinstance(partial, dict):
            prefix['records'] = copy.deepcopy(records[:len(original_prefix)])
            prefix['bootstrap_tail'] = mapping.get(prefix['bootstrap_tail'], prefix['bootstrap_tail'])
            partial['tail_sha256'] = previous
        else:
            prefix['bootstrap_tail'] = mapping.get(prefix['bootstrap_tail'], prefix['bootstrap_tail'])
            suffix['genesis_tail'] = prefix['bootstrap_tail']
            suffix['final_tail'] = previous
        _replace_hashes(result.get('negative'), mapping)
    term = result.get('negative', {}).get('terminal_observation')
    if isinstance(term, dict):
        previous = '0' * 64
        for event in term['records']:
            event['previous'] = previous
            previous = hashlib.sha256(b'HOUFENG-G2-BPF-OBS-V1\0' + _canonical(event)).hexdigest()
        term['terminal_sha256'] = previous


def _events(result):
    suffix = result.get('bpf_authority')
    if isinstance(suffix, dict):
        return suffix['records']
    return result['negative']['terminal_observation']['records']


def _find_event(result, kind, predicate=lambda body: True):
    for event in _events(result):
        if event['kind'] == kind and predicate(event['body']):
            return event['body']
    raise NotReady('required real %s evidence is absent' % kind)


def _mutate(result, name):
    if name in ('owner_probe_bind_source', 'owner_probe_bind_target', 'owner_probe_bind_writable',
                'owner_probe_unrequested_volume'):
        records = result['bpf_bootstrap']['records'] + result['bpf_authority']['records']
        ids = {event['body']['id'] for event in records if event['kind'] == 'grant' and
               event['body']['purpose'] == 'postgres-uid'}
        if len(ids) != 1:
            raise NotReady('unique real PostgreSQL uid grant missing')
        grant_id = next(iter(ids))
        grants = [event['body'] for event in records if event['kind'] == 'grant' and
                  event['body']['id'] == grant_id]
        old_hashes = {body['config_sha256'] for body in grants}
        if len(old_hashes) != 1:
            raise NotReady('PostgreSQL uid grant config changed across phases')
        old_hash = next(iter(old_hashes))
        changed_hashes = set()
        for body in grants:
            config = body['config']
            if hashlib.sha256(_canonical(config)).hexdigest() != old_hash:
                raise NotReady('PostgreSQL uid grant config hash differs')
            host_config = config['HostConfig']
            if name == 'owner_probe_unrequested_volume':
                del host_config['Mounts']
            elif name == 'owner_probe_bind_source':
                host_config['Mounts'][0]['Source'] = '/run/g2/wrong-owner-probe'
            elif name == 'owner_probe_bind_target':
                host_config['Mounts'][0]['Target'] = '/wrong-pgdata'
            else:
                host_config['Mounts'][0]['ReadOnly'] = False
            changed_hashes.add(hashlib.sha256(_canonical(config)).hexdigest())
        if len(changed_hashes) != 1:
            raise NotReady('mutated PostgreSQL uid grant configs differ')
        _replace_hashes(result, {old_hash: next(iter(changed_hashes))})
    elif name in ('launcher_binding_digest', 'launcher_binding_duplicate', 'launcher_target_inode', 'launcher_exec_missing'):
        prefix = result['bpf_bootstrap']['records']
        suffix = result.get('bpf_authority')
        partial = result.get('bpf_healthy_partial')
        records = prefix + suffix['records'] if isinstance(suffix, dict) else partial['records']
        matches = [(index, event) for index, event in enumerate(records) if event['kind'] == 'containerd_launcher']
        if len(matches) != 1:
            raise NotReady('unique real containerd launcher binding missing')
        index, pair_event = matches[0]
        pair = pair_event['body']
        if name == 'launcher_binding_digest':
            digest = pair['exe_sha256']
            pair['exe_sha256'] = ('1' if digest[0] != '1' else '2') + digest[1:]
        elif name == 'launcher_binding_duplicate':
            container = suffix if isinstance(suffix, dict) else partial
            events = container['records']
            tail_key = 'final_tail' if isinstance(suffix, dict) else 'tail_sha256'
            old_tail = container[tail_key]
            duplicate = copy.deepcopy(pair_event)
            duplicate['sequence'] = len(records) + 1
            duplicate['previous'] = old_tail
            events.append(duplicate)
            container[tail_key] = hashlib.sha256(b'launcher-duplicate-tail-sentinel').hexdigest()
            if isinstance(suffix, dict):
                container['final_watermark'] += 1
            else:
                container['watermark'] += 1
        elif name == 'launcher_target_inode':
            used = {event['body']['record']['detail']['exe_ino'] for event in records
                    if event['kind'] == 'guard' and event['body']['record']['kind'] in (9, 10, 11, 12)}
            inode = 1
            while inode in used or inode == pair['exe_ino']:
                inode += 1
            pair['target_ino'] = inode
        else:
            launcher = (pair['exe_dev'], pair['exe_ino'])
            initial = next((event['body']['record'] for event in records if event['kind'] == 'guard' and
                            event['body']['record']['kind'] == 10 and
                            (event['body']['record']['detail']['exe_dev'],
                             event['body']['record']['detail']['exe_ino']) == launcher), None)
            if initial is None:
                raise NotReady('real launcher exec missing')
            parent = initial['parent_cookie']
            inherited = next((event['body']['record'] for event in records if event['kind'] == 'guard' and
                              event['body']['record']['kind'] == 9 and
                              event['body']['record']['task_cookie'] == initial['task_cookie'] and
                              event['body']['record']['parent_cookie'] == parent), None)
            if inherited is None:
                raise NotReady('real launcher fork missing')
            initial['detail']['exe_dev'] = inherited['detail']['exe_dev']
            initial['detail']['exe_ino'] = inherited['detail']['exe_ino']
    elif name == 'actor':
        body = _find_event(result, 'probe_actor')
        body['task_cookie'] += 1
    elif name == 'stamp':
        body = _find_event(result, 'audit', lambda b: b['record_type'] == 'SYSCALL')
        body['stamp']['seconds'] += 1
    elif name == 'eoe':
        body = _find_event(result, 'audit_wire', lambda b: b['record_type'] == 1320)
        body['record_type'] = 1300
    elif name == 'occurrence':
        body = _find_event(result, 'audit_wire')
        body['occurrence'] += 1
    elif name == 'module_identity':
        result['negative']['terminal_observation']['authority_after']['module_ino'] += 1
    elif name == 'phase':
        result['negative']['terminal_observation']['authority_after']['authority_phase'] = 1
    elif name == 'fake_attached':
        result['negative']['terminal_observation']['authority_after']['link_ids'][0] += 1
    elif name == 'id_reuse_cookie':
        observations = result['negative']['observations']
        item = next((o for o in observations if o['name'] == 'id-reuse'), None)
        if item is None:
            raise NotReady('real ID reuse case missing')
        ids = set(item['evidence_sequences'])
        event = next((e for e in _events(result) if e['sequence'] in ids and e['kind'] == 'guard' and
                      e['body']['record']['kind'] == 16), None)
        if event is None:
            raise NotReady('real ID cursor record missing')
        event['body']['record']['object_cookie'] += 1
    elif name == 'snapshot':
        body = _find_event(result, 'guard', lambda b: b['record']['kind'] == 3)
        body['record']['object_snapshot']['attributes']['value_size'] += 4
    elif name == 'source':
        result['negative']['terminal_observation']['fault_evidence']['source'] = 'broker_guard_copy'
    elif name == 'witness':
        evidence = result['negative']['terminal_observation']['fault_evidence']
        if not evidence['reader_witness_sequences']:
            raise NotReady('reader witness is missing')
        evidence['reader_witness_sequences'] = evidence['reader_witness_sequences'][1:]
    elif name == 'credential':
        evidence = result['negative']['terminal_observation']['fault_evidence']
        evidence['observed_actor'] = evidence['expected_actor']
    elif name == 'request_deadline':
        evidence = result['negative']['terminal_observation']['fault_evidence']
        if type(evidence['observed_ns']) is not int:
            raise NotReady('actual reader deadline observation missing')
        evidence['deadline_ns'] = evidence['observed_ns'] + 1
    elif name == 'cross_case_checkpoint':
        observations = result['negative']['observations']
        if len(observations) < 2:
            raise NotReady('independent authorization intervals missing')
        observations[0]['before'] = copy.deepcopy(observations[1]['before'])
    elif name in ('extra_case', 'extra_grant', 'extra_cookie', 'extra_actor', 'extra_listener', 'extra_fd', 'extra_premature_removal'):
        stage = _find_event(result, 'selftest_runtime_stage', lambda b: b['mode'] == 'extra-fd-delays-removal' and b['phase'] == 'injected')
        if name == 'extra_case':
            stage['nonce'] = ('0' if stage['nonce'][0] != '0' else '1') + stage['nonce'][1:]
        elif name == 'extra_grant':
            stage['grant_id'] += 1
        elif name == 'extra_cookie':
            stage['mutation']['program_cookie'] += 1
        elif name == 'extra_actor':
            stage['notifying_cookie'] += 1
        elif name == 'extra_listener':
            stage['mutation']['listener_id'] += 1
        elif name == 'extra_fd':
            stage['mutation']['installed_fd'] ^= 1
        else:
            cookie = stage['mutation']['program_cookie']
            obj = _find_event(result, 'object', lambda b: b['cookie'] == cookie and b['state'] == 'live')
            removed = _find_event(result, 'object', lambda b: b['cookie'] == cookie and b['state'] == 'id_removed')
            obj['state'], obj['removal_seq'] = 'id_removed', removed['removal_seq']
    elif name.startswith('assembly_'):
        mode = {'assembly_missing': 'fd-slot-reuse', 'assembly_wait_pid': 'fd-slot-reuse',
                'assembly_wait_status': 'fd-slot-reuse', 'assembly_missing_frame': 'tid-reuse',
                'assembly_dup': 'fd-slot-reuse', 'assembly_unshare': 'nonleader-files',
                'assembly_leader_a': 'nonleader-files', 'assembly_leader_b': 'shared-files',
                'assembly_barrier': 'leader-exit', 'assembly_tid_first': 'tid-reuse',
                'assembly_tid_wait': 'tid-reuse'}[name]
        item = next((o for o in result['negative']['observations'] if o['name'] == mode), None)
        if item is None or 'assembly' not in item:
            raise NotReady('real assembly observation missing: ' + mode)
        assembly = item['assembly']
        if name == 'assembly_missing':
            del item['assembly']
        elif name == 'assembly_wait_pid':
            assembly['wait_pid'] += 1
        elif name == 'assembly_wait_status':
            assembly['wait_status'] = 1
        elif name == 'assembly_missing_frame':
            assembly['frames'].pop(0)
        elif name == 'assembly_dup':
            assembly['frames'][-1]['dup_result'] += 1
        elif name == 'assembly_unshare':
            assembly['frames'][-1]['unshare_result'] = -1
        elif name == 'assembly_leader_a':
            assembly['frames'][-1]['leader_is_a'] = False
        elif name == 'assembly_leader_b':
            assembly['frames'][-1]['leader_is_b'] = False
        elif name == 'assembly_barrier':
            assembly['frames'][-1]['barrier_result'] = 0
        elif name == 'assembly_tid_first':
            assembly['frames'][0]['first_tid'] += 1
        else:
            assembly['frames'][1]['first_wait_status'] = 1
    elif name.startswith('overflow_'):
        term = result['negative']['terminal_observation']
        proof = term['fault_evidence']
        witnesses = [term['records'][seq - 1]['body'] for seq in proof['broker_witness_sequences']
                     if term['records'][seq - 1]['body']['kind'] == 'broker_receive_observation']
        if len(witnesses) != 1:
            raise NotReady('unique overflow broker witness missing')
        witness = witnesses[0]
        wire = proof['rejected_wire']
        if name in ('overflow_capacity', 'overflow_count_before'):
            field = name.removeprefix('overflow_')
            proof[field] -= 1
            witness[field] = proof[field]
        elif name == 'overflow_rejected_wire':
            digest = wire['payload_sha256']
            changed = ('0' if digest[0] != '0' else '1') + digest[1:]
            wire['payload_sha256'] = changed
            witness['rejected_wire']['payload_sha256'] = changed
            proof['input_sha256'] = witness['input_sha256'] = changed
        elif name == 'overflow_observer_copy':
            matches = [event['body'] for event in term['records'] if event['kind'] == 'audit_wire' and
                       all(event['body'][key] == wire[key] for key in
                           ('stamp', 'serial', 'record_type', 'payload_sha256'))]
            if len(matches) != 1:
                raise NotReady('unique independent overflow observer wire missing')
            digest = matches[0]['payload_sha256']
            matches[0]['payload_sha256'] = ('0' if digest[0] != '0' else '1') + digest[1:]
        else:
            raise NotReady('unknown overflow mutation')
    elif name.startswith('audit_receipt_') or name == 'audit_delivery_permutation':
        witness = _find_event(result, 'selftest_audit_reorder')
        if name == 'audit_receipt_identity':
            receipt = witness['received'][0]
            digest = receipt['payload_sha256']
            receipt['payload_sha256'] = ('0' if digest[0] != '0' else '1') + digest[1:]
        elif name == 'audit_receipt_order':
            witness['received'][0], witness['received'][1] = witness['received'][1], witness['received'][0]
        else:
            witness['delivery_order'][0], witness['delivery_order'][1] = witness['delivery_order'][1], witness['delivery_order'][0]
    else:
        raise NotReady('unknown mutation')
    _rehash(result)


def run_negative_mutations(host, baseline_result, baseline_record, fault_results_by_variant, build):
    """Verify real originals first, then require rejection of each changed copy.

    fault_results_by_variant maps each variant to (result, record).
    """
    source_bpf_verify.verify(host, baseline_result, baseline_record, build, reboot_result=None)
    variants = {'module_identity': 'reader-death', 'phase': 'reader-death', 'fake_attached': 'reader-death',
                'source': 'reader-frame', 'witness': 'reader-frame', 'credential': 'reader-credential',
                'request_deadline': 'reader-timeout',
                'overflow_capacity': 'audit-overflow', 'overflow_count_before': 'audit-overflow',
                'overflow_rejected_wire': 'audit-overflow', 'overflow_observer_copy': 'audit-overflow'}
    required = set(source_bpf_verify._TERMINAL_FAULT_VARIANTS | source_bpf_verify._READER_FAULT_VARIANTS)
    if not required <= set(fault_results_by_variant):
        raise NotReady('complete successful fault inputs are missing')
    for variant in required:
        result, record = fault_results_by_variant[variant]
        source_bpf_verify.verify(host, result, record, build, reboot_result=None)
    results = {}
    for name in ('actor', 'stamp', 'eoe', 'occurrence', 'module_identity', 'phase', 'fake_attached',
                 'id_reuse_cookie', 'snapshot', 'source', 'witness', 'credential', 'request_deadline',
                 'cross_case_checkpoint', 'extra_case', 'extra_grant', 'extra_cookie', 'extra_actor',
                 'extra_listener', 'extra_fd', 'extra_premature_removal',
                 'assembly_missing', 'assembly_wait_pid', 'assembly_wait_status', 'assembly_missing_frame',
                 'assembly_dup', 'assembly_unshare', 'assembly_leader_a', 'assembly_leader_b',
                 'assembly_barrier', 'assembly_tid_first', 'assembly_tid_wait', 'audit_receipt_identity',
                 'audit_receipt_order', 'audit_delivery_permutation', 'overflow_capacity', 'overflow_count_before',
                 'overflow_rejected_wire', 'overflow_observer_copy', 'owner_probe_bind_source',
                 'owner_probe_bind_target', 'owner_probe_bind_writable', 'owner_probe_unrequested_volume',
                 'launcher_binding_digest', 'launcher_binding_duplicate', 'launcher_target_inode', 'launcher_exec_missing'):
        original, record = fault_results_by_variant[variants[name]] if name in variants else (baseline_result, baseline_record)
        changed = copy.deepcopy(original)
        _mutate(changed, name)
        try:
            source_bpf_verify.verify(host, changed, record, build, reboot_result=None)
        except host.Failure as exc:
            if name.startswith('owner_probe_') and str(exc) != 'BPF bootstrap grant differs from its fixed template':
                raise host.Failure('owner probe mutation rejected for unexpected reason: ' + name) from exc
            if name.startswith('launcher_') and not str(exc).startswith('BPF containerd launcher'):
                raise host.Failure('launcher mutation rejected for unexpected reason: ' + name) from exc
            results[name] = 'REJECTED'
        else:
            raise host.Failure('semantic mutation accepted: ' + name)
    return results


def _load_inputs(host, frozen, build):
    root = host.RUN / 'runs' / ('selection-' + build['_selection']['selection_id']) / 'realm-negative'
    if not root.exists():
        raise NotReady('no runs for the selected build')
    attempts = source_build._numbered(host, root, 'attempt-', '')
    for attempt in reversed(attempts):
        attempt_dir = root / ('attempt-%020d' % attempt)
        path = attempt_dir / 'launch-manifest.json'
        host.check_path(path, False)
        raw = path.read_bytes()
        manifest = host.strict_json(raw)
        host.require(host.canonical(manifest) == raw and manifest.get('run_token') == host.TOKEN and
                     manifest.get('case') == 'realm-negative' and manifest.get('build_selection') == build['_selection'],
                     'launch manifest identity differs')
        if 'bpf-authorization' not in manifest.get('scenarios', []):
            continue
        verified = source_verify._case(host, frozen, 'realm-negative', 'bpf-authorization', build)
        if verified.get('status') != 'PASS':
            raise NotReady('latest committed authorization family is not a verified success')
        family_dir = attempt_dir / 'bpf-authorization'
        family = host.strict_json((family_dir / 'family.json').read_bytes())
        if any(member['status'] != 'PASS' for member in family['members']):
            raise NotReady('latest authorization family has failed members')
        inputs = {}
        for entry in family['members']:
            member = attempt_dir / entry['directory']
            record = host.strict_json((member / 'receipt.json').read_bytes())
            result = host.strict_json((member / 'parsed.json').read_bytes())
            inputs[entry['variant']] = (result, record)
        if 'baseline' not in inputs:
            raise NotReady('baseline result missing')
        return inputs.pop('baseline'), inputs
    raise NotReady('no committed authorization manifest for selected build')


def _verify_selected(host, frozen):
    lock = source_build.lock_run(host)
    try:
        build = source_build.build_gate(host, frozen)
        baseline, faults = _load_inputs(host, frozen, build)
        return run_negative_mutations(host, baseline[0], baseline[1], faults, build)
    finally:
        os.close(lock)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-dir', required=True)
    args = parser.parse_args()
    source_host.validate_run(args.run_dir)
    frozen = source_host.frozen_host()
    results = _verify_selected(source_host, frozen)
    print(json.dumps({'status': 'PASS', 'mutations': results}, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (NotReady, source_host.Failure, OSError, ValueError, KeyError, TypeError) as exc:
        print(json.dumps({'status': 'NOT_READY', 'reason': str(exc)}, sort_keys=True))
        sys.exit(1)
