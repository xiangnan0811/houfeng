"""Strict, bounded decoding of owner settlement evidence (wire version 1)."""
import base64
import binascii
import json
import re
import struct

_U64 = (1 << 64) - 1
_HEADER = ('generation', 'grant_id', 'owner_id', 'creator_transaction', 'object_cookie',
           'kernel_id', 'publication_sequence', 'root_cookie', 'cgroup_id', 'enrollment',
           'operation_start_ns', 'operation_deadline_ns', 'authority_deadline_ns',
           'requested_cutoff_ns', 'captured_ns')
_FACT = ('first_seq', 'last_seq', 'first_native_ns', 'last_native_ns',
         'first_broker_ns', 'last_broker_ns', 'count', 'value0', 'value1', 'state', 'flags')
_RELATION = ('grant', 'creator', 'cookie', 'kernel_id', 'publication_sequence',
             'root_cookie', 'cgroup_id', 'enrollment')
_POINTS = ('EFFECT_OPERATION', 'ALIAS_EXECUTION', 'OBJECT_CLEANUP_STARTED',
           'CGROUP_TARGET_UNOBSERVED', 'CGROUP_RETIRE_EAGAIN', 'CGROUP_TARGET_RELEASE',
           'INVOCATION', 'EXEC_ADMISSION', 'TRANSACTION_RETIREMENT', 'OBJECT_REMOVAL',
           'ATTACHMENT_RELEASE', 'OWNER_PROBE_RESULT', 'AUDIT_JOIN')


def _require(ok):
    if not ok:
        raise ValueError('invalid settlement evidence')


def _integer(value):
    return type(value) is int and 0 <= value <= _U64


def _hex(value, size):
    return type(value) is str and re.fullmatch('[0-9a-f]{%d}' % size, value) is not None


def _canonical(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')
    except (TypeError, ValueError, OverflowError) as exc:
        raise ValueError('invalid settlement evidence') from exc


def decode_snapshot(raw: bytes) -> dict:
    _require(type(raw) is bytes and len(raw) == 4096 and raw[:8] == b'G2SEV1\0\0')
    version, phase, purpose, gaps = struct.unpack_from('<HBBI', raw, 8)
    _require(version == 1 and phase in (1, 2, 3) and purpose in (1, 2) and gaps & ~0xff == 0)
    result = dict(zip(_HEADER, struct.unpack_from('<15Q', raw, 16)))
    result.update(version=version, phase=phase, purpose=purpose, gap_flags=gaps,
                  boot_id=raw[136:152].hex(), epoch=raw[152:168].hex(),
                  nonce=raw[168:184].hex(), container_id=raw[184:216].hex())
    cls, obj_type, first, last, module, terminal = struct.unpack_from('<IIQQQI', raw, 216)
    result.update(object_class=cls, object_type=obj_type, first_guard_sequence=first,
                  last_guard_sequence=last, module_instance=module, terminal_reason=terminal)
    _require(raw[252:256] == bytes(4) and raw[3840:] == bytes(256))
    _require(cls in (0, 1, 2) and (cls != 0 or obj_type == 0) and terminal in range(6))
    _require((phase == 3) == (terminal != 0))
    _require(not first or not last or first <= last)
    _require(not result['requested_cutoff_ns'] or not result['captured_ns'] or
             result['requested_cutoff_ns'] <= result['captured_ns'])
    _require(result['generation'] > 0 and result['grant_id'] > 0 and result['owner_id'] > 0)
    facts = []
    for index in range(32):
        values = struct.unpack_from('<9QII', raw, 256 + index * 80)
        fact = dict(zip(_FACT, values))
        _require(fact['state'] in range(5) and fact['flags'] & ~15 == 0)
        if index in (14, 15):
            _require(not any(values))
        if fact['state'] == 0:
            _require(not any(values))
        if fact['state'] == 1:
            _require(fact['count'] > 0)
            if index in (2, 3, 24, 25):
                _require(fact['first_seq'] > 0 and fact['last_seq'] > 0 and
                         fact['first_native_ns'] > 0 and fact['last_native_ns'] > 0 and
                         fact['value0'] == fact['first_seq'])
        for first_key, last_key in (('first_seq', 'last_seq'), ('first_native_ns', 'last_native_ns'),
                                    ('first_broker_ns', 'last_broker_ns')):
            a, b = fact[first_key], fact[last_key]
            _require(not a or not b or a <= b)
        if fact['first_seq'] and fact['first_seq'] == fact['last_seq'] and fact['first_native_ns'] and fact['last_native_ns']:
            _require(fact['first_native_ns'] == fact['last_native_ns'])
        if fact['count'] == _U64:
            _require(fact['state'] == 4 and fact['flags'] & 4)
        cutoff = result['requested_cutoff_ns']
        if phase == 3 and cutoff and any(fact[key] > cutoff for key in ('last_native_ns', 'last_broker_ns')):
            _require(bool(fact['flags'] & 8))
        if index in (26, 27):
            _require(fact['value0'] <= 2 and fact['value1'] <= 9)
            if fact['state'] == 1:
                _require((fact['value0'] == 2) == (fact['value1'] != 0))
        if index == 30:
            _require(fact['value0'] <= 9)
        if index == 5:
            _require(fact['value0'] <= 13)
        if index == 6:
            _require(fact['value0'] < 1 << 13 and fact['value1'] & ~fact['value0'] == 0)
        if index == 18:
            _require(fact['value0'] <= 2 and fact['value1'] <= (1 << 31) - 1)
        if index not in (6, 18, 19, 21, 26, 27, 30, 31):
            _require(fact['value1'] == 0)
        if index in (19, 21):
            _require(fact['value0'] == 0 and fact['value1'] <= (1 << 31) - 1)
        if index == 22:
            _require(fact['value0'] <= (1 << 31) - 1)
        if index in (9, 10, 11):
            _require(fact['value0'] <= 5)
        if index == 0 and fact['state'] == 1:
            _require(fact['value0'] == result['grant_id'])
        if index == 1 and fact['state'] == 1 and result['enrollment']:
            _require(fact['value0'] == result['enrollment'])
        if index == 20:
            _require(fact['value0'] <= 1)
        facts.append(fact)
    result['facts'] = facts
    result['relations'] = [dict(zip(_RELATION, struct.unpack_from('<8Q', raw, 2816 + i * 64)))
                           for i in range(8)]
    target = result['relations'][0]
    for header, relation in (('grant_id', 'grant'), ('creator_transaction', 'creator'),
                             ('object_cookie', 'cookie'), ('kernel_id', 'kernel_id'),
                             ('publication_sequence', 'publication_sequence'), ('root_cookie', 'root_cookie'),
                             ('cgroup_id', 'cgroup_id'), ('enrollment', 'enrollment')):
        if result['object_cookie']:
            _require(target[relation] == result[header])
        else:
            _require(target[relation] == 0)
    group = result['relations'][1]
    if result['object_cookie']:
        _require(group == {'grant': result['grant_id'], 'creator': 0, 'cookie': 0, 'kernel_id': 0,
                           'publication_sequence': 0, 'root_cookie': result['root_cookie'],
                           'cgroup_id': result['cgroup_id'], 'enrollment': result['enrollment']})
    else:
        _require(not any(group.values()))
    if any(facts[i]['state'] == 1 for i in (24, 25)):
        _require(all(result[key] > 0 for key in ('creator_transaction', 'object_cookie', 'kernel_id',
                                                'publication_sequence', 'object_class', 'object_type')))
    pin = facts[31]
    if pin['state'] == 1 and result['object_cookie']:
        _require((pin['value0'], pin['value1']) == (result['object_cookie'], result['publication_sequence']))
    coverage = struct.unpack_from('<64Q', raw, 3328)
    _require(not any(coverage[47:]) and coverage[0] <= 1 and coverage[1] <= 1 and
             coverage[21] <= 15 and coverage[22] < 1 << 13 and coverage[23] & ~coverage[22] == 0 and
             coverage[24] == gaps and coverage[25] == 384 and coverage[26] <= 384 and
             coverage[28] <= 5 and coverage[29] == 5 and coverage[30] <= 13 and
             coverage[31] <= 3 and coverage[32] <= 3 and all(v <= 5 for v in coverage[41:45]) and
             coverage[45] <= 2)
    for exists, indices in ((coverage[0], (2, 4, 6, 8, 10, 12, 14, 37, 38, 41)),
                            (coverage[1], (3, 5, 7, 9, 11, 13, 15, 39, 40, 42))):
        if not exists:
            _require(not any(coverage[i] for i in indices))
    for a, b in ((33, 34), (35, 36), (37, 38), (39, 40)):
        _require(not coverage[a] or not coverage[b] or coverage[a] <= coverage[b])
    result['coverage'] = list(coverage)
    return result


def validate_evidence(value) -> dict:
    _require(type(value) is dict and set(value) == {'version', 'probes'} and
             type(value['version']) is int and value['version'] == 1 and
             type(value['probes']) is list and len(value['probes']) <= 2 and len(_canonical(value)) <= 49152)
    purposes, nonces = set(), set()
    session_identity, grant_ids, containers = None, set(), set()
    for probe in value['probes']:
        _require(type(probe) is dict and set(probe) == {'purpose', 'nonce', 'collection', 'guest', 'snapshots', 'reference_holder'})
        purpose = probe['purpose']
        _require(purpose in ('postgres-uid', 'postgres-gid') and purpose not in purposes and
                 _hex(probe['nonce'], 32) and probe['nonce'] not in nonces)
        purposes.add(purpose)
        nonces.add(probe['nonce'])
        collection, guest, packets = probe['collection'], probe['guest'], probe['snapshots']
        _require(collection in ('INVALID', 'UNAVAILABLE', 'EMPTY', 'PARTIAL', 'COMPLETE') and
                 probe['reference_holder'] == 'UNRESOLVED' and type(guest) is dict and
                 set(guest) == {'operation_start_ns', 'operation_deadline_ns', 'authority_deadline_ns', 'ack_received_ns'} and
                 all(_integer(x) for x in guest.values()) and type(packets) is list and len(packets) <= 3)
        _require(not guest['operation_deadline_ns'] or not guest['operation_start_ns'] or
                 guest['operation_start_ns'] <= guest['operation_deadline_ns'])
        snapshots, native_times = [], {}
        for packet in packets:
            _require(type(packet) is str and len(_canonical(packet)) <= 8192)
            try:
                raw = base64.b64decode(packet, validate=True)
            except (ValueError, binascii.Error) as exc:
                raise ValueError('invalid settlement evidence') from exc
            _require(base64.b64encode(raw).decode('ascii') == packet)
            snap = decode_snapshot(raw)
            _require(snap['purpose'] == (1 if purpose == 'postgres-uid' else 2) and snap['nonce'] == probe['nonce'])
            for fact in snap['facts']:
                for seq_key, time_key in (('first_seq', 'first_native_ns'), ('last_seq', 'last_native_ns')):
                    sequence, observed = fact[seq_key], fact[time_key]
                    if sequence and observed:
                        _require(sequence not in native_times or native_times[sequence] == observed)
                        native_times[sequence] = observed
            for key in ('operation_start_ns', 'operation_deadline_ns', 'authority_deadline_ns'):
                _require(not snap[key] or not guest[key] or snap[key] == guest[key])
            snapshots.append(snap)
        phases = [s['phase'] for s in snapshots]
        _require(phases == sorted(set(phases)))
        if collection in ('INVALID', 'UNAVAILABLE', 'EMPTY'):
            _require(not packets)
        elif collection == 'COMPLETE':
            _require(phases and phases[0] == 1 and phases[-1] == 3 and
                     all(s['coverage'][28] == 0 and not s['gap_flags'] & 128 for s in snapshots))
        else:
            _require(bool(packets) and not (phases[0] == 1 and phases[-1] == 3 and
                     all(s['coverage'][28] == 0 and not s['gap_flags'] & 128 for s in snapshots)))
        if snapshots:
            first = snapshots[0]
            identity = (first['boot_id'], first['epoch'], first['owner_id'])
            if session_identity is None:
                session_identity = identity
            else:
                _require(identity == session_identity)
            _require(first['grant_id'] not in grant_ids)
            grant_ids.add(first['grant_id'])
            ids = {s['container_id'] for s in snapshots if s['container_id'] != '0' * 64}
            _require(len(ids) <= 1 and not ids.intersection(containers))
            containers.update(ids)
            for snap in snapshots[1:]:
                for key in ('generation', 'grant_id', 'owner_id', 'boot_id', 'epoch', 'nonce', 'purpose'):
                    _require(snap[key] == first[key])
                _require(snap['captured_ns'] >= first['captured_ns'])
            for key in ('creator_transaction', 'object_cookie', 'kernel_id', 'publication_sequence',
                        'root_cookie', 'cgroup_id', 'enrollment', 'object_class', 'object_type', 'container_id',
                        'operation_start_ns', 'operation_deadline_ns', 'authority_deadline_ns'):
                known = [s[key] for s in snapshots if s[key] and s[key] != '0' * 64]
                _require(not known or all(item == known[0] for item in known))
                for old, new in zip(snapshots, snapshots[1:]):
                    before, after = old[key], new[key]
                    if before and before != '0' * 64:
                        _require(after == before)
            for previous, current in zip(snapshots, snapshots[1:]):
                _require(current['captured_ns'] >= previous['captured_ns'])
                for old, new in zip(previous['facts'], current['facts']):
                    _require(new['count'] >= old['count'])
                    if old['first_seq'] and old['first_seq'] == new['first_seq'] and old['first_native_ns'] and new['first_native_ns']:
                        _require(old['first_native_ns'] == new['first_native_ns'])
                    if old['last_seq'] and old['last_seq'] == new['last_seq'] and old['last_native_ns'] and new['last_native_ns']:
                        _require(old['last_native_ns'] == new['last_native_ns'])
    return value

def summarize_evidence(value) -> dict:
    if value is None:
        return {'quality': 'UNAVAILABLE', 'last_native_stage': 'UNKNOWN', 'blocking_points': [],
                'delivery': 'UNKNOWN', 'timeliness': 'UNKNOWN', 'reference_holder': 'UNRESOLVED'}
    validate_evidence(value)
    summary = {'quality': 'UNAVAILABLE', 'last_native_stage': 'UNKNOWN', 'blocking_points': [],
               'delivery': 'UNKNOWN', 'timeliness': 'UNKNOWN', 'reference_holder': 'UNRESOLVED'}
    collections = [p['collection'] for p in value['probes']]
    if 'INVALID' in collections:
        summary['quality'] = 'INVALID'
    elif any(c == 'PARTIAL' for c in collections) or ('COMPLETE' in collections and any(c != 'COMPLETE' for c in collections)):
        summary['quality'] = 'PARTIAL'
    elif collections and all(c == 'COMPLETE' for c in collections):
        summary['quality'] = 'COMPLETE'
    deliveries, timings = [], []
    stages = ('UNKNOWN', 'OFFLINE_ENTRY', 'RELEASE_RETURN', 'ID_REMOVAL_RETURN')
    for probe in value['probes']:
        if not probe['snapshots']:
            deliveries.append('UNKNOWN')
            timings.append('UNKNOWN')
            continue
        snap = decode_snapshot(base64.b64decode(probe['snapshots'][-1]))
        facts, coverage, cutoff = snap['facts'], snap['coverage'], snap['requested_cutoff_ns']
        for i, name in ((2, 'OFFLINE_ENTRY'), (3, 'RELEASE_RETURN'), (24, 'ID_REMOVAL_RETURN')):
            if (facts[i]['state'] == 1 and facts[i]['first_native_ns'] and
                    (not cutoff or facts[i]['first_native_ns'] <= cutoff) and
                    stages.index(name) > stages.index(summary['last_native_stage'])):
                summary['last_native_stage'] = name
        predicate = facts[6]
        if (predicate['state'] == 1 and predicate['count'] == 1 and predicate['last_broker_ns'] and
                (not cutoff or predicate['last_broker_ns'] <= cutoff) and
                (predicate['value0'], predicate['value1']) == (coverage[22], coverage[23])):
            summary['blocking_points'] = list(dict.fromkeys(summary['blocking_points'] +
                [name for i, name in enumerate(_POINTS) if coverage[23] & (1 << i)]))
        delivery, timing = 'UNKNOWN', 'UNKNOWN'
        fact, deadline = facts[24], probe['guest']['operation_deadline_ns']
        if fact['state'] == 1:
            def seen(slot):
                item = facts[slot]
                return (item['state'] == 1 and item['count'] == 1 and not item['flags'] & 6 and
                        item['first_broker_ns'] and (not cutoff or item['first_broker_ns'] <= cutoff))
            if seen(24) and not snap['gap_flags'] & 15 and facts[25]['state'] in (0, 1):
                if facts[25]['state'] == 0 or (cutoff and facts[25]['first_broker_ns'] > cutoff):
                    delivery = 'NOT_ADMITTED'
                if seen(25) and facts[25]['value0'] == fact['value0']:
                    delivery = 'ADMITTED'
                    if any(seen(i) and facts[i]['value0'] == 2 and facts[i]['first_seq'] == fact['first_seq']
                           for i in (26, 27)):
                        delivery = 'REJECTED'
                    elif (all(seen(i) and facts[i]['value0'] == 1 and facts[i]['first_seq'] == fact['first_seq']
                              for i in (26, 27)) and all(seen(i) for i in (28, 29)) and
                          facts[28]['first_seq'] == fact['first_seq'] and facts[29]['value0'] == fact['value0']):
                        delivery = 'ACCEPTED'
            if (deadline and fact['first_native_ns'] and fact['last_native_ns'] and
                    not (cutoff and fact['first_native_ns'] <= cutoff < fact['last_native_ns'])):
                if fact['last_native_ns'] <= deadline:
                    timing = 'BEFORE_DEADLINE'
                elif fact['first_native_ns'] > deadline:
                    timing = 'AFTER_DEADLINE'
        deliveries.append(delivery)
        timings.append(timing)
    if deliveries and all(v == deliveries[0] for v in deliveries):
        summary['delivery'] = deliveries[0]
    if timings and all(v == timings[0] for v in timings):
        summary['timeliness'] = timings[0]
    return summary
