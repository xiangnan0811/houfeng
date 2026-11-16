"""Independently validate source VM receipts and preserve missing-case status."""
import base64
import binascii
import datetime
import hashlib
import json
import os
from pathlib import Path
import posixpath
import re
import secrets
import signal
import stat
import sys

import source_build
import source_bpf_verify
import source_vm
import source_retirement
from source_supervisor.census import inventory_digest
from source_supervisor.contract import (CALLBACK_CUTS, CASES, MIGRATION_BINDING, MIGRATION_CGROUP,
                                        NEGATIVES, RECOVERY, RUN_TOKEN, scenarios)
sys.path.insert(0, str(Path(__file__).resolve().parent / 'source_supervisor'))
from source_supervisor.journal import EVENT_KEYS

HEX = re.compile(r'^[0-9a-f]{64}$')


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')


def _sha(value):
    return hashlib.sha256(_canonical(value)).hexdigest()

def _carrier_hardware(host, result, record):
    hardware = result.get('isolation', {}).get('hardware')
    image = record.get('seed_image')
    host.require(type(hardware) is dict and set(hardware) == {'machine', 'seed_mount', 'cidata_mount'} and
                 hardware['machine'] in ('Standard PC (Q35 + ICH9, 2009)', 'KVM') and type(image) is dict,
                 'guest carrier hardware evidence missing')
    seed, cidata = hardware['seed_mount'], hardware['cidata_mount']
    for value, target, serial, label, filesystem in (
            (seed, '/mnt/g2-seed', 'G2SEED', 'G2SEED', 'ext4'),
            (cidata, '/mnt/g2-cidata', 'G2CIDATA', 'CIDATA', 'vfat')):
        host.require(type(value) is dict and value.get('target') == target and value.get('serial') == serial and
                     value.get('label') == label and value.get('type') == filesystem and value.get('read_only') is True and
                     type(value.get('id')) is int and value['id'] > 0 and type(value.get('parent')) is int and
                     type(value.get('device')) is str and re.fullmatch(r'[0-9]+:[0-9]+', value['device']) is not None and
                     type(value.get('source')) is str and value['source'].startswith('/dev/') and
                     type(value.get('bytes')) is int and value['bytes'] > 0 and type(value.get('options')) is list and
                     {'ro', 'nosuid', 'nodev', 'noexec'} <= set(value['options']) and 'rw' not in value['options'],
                     'guest carrier mount or block identity differs')
    cidata_block = record.get('cidata_block')
    host.require(type(cidata_block) is dict and set(cidata_block) == {'size', 'sha256'} and
                 type(cidata_block['size']) is int and cidata_block['size'] > 0 and
                 type(cidata_block['sha256']) is str and HEX.fullmatch(cidata_block['sha256']) is not None and
                 type(record.get('cidata_image')) is dict and
                 cidata_block == {key: record['cidata_image'].get(key) for key in ('size', 'sha256')} and
                 seed.get('whole_device') == seed['device'] and
                 type(cidata.get('whole_device')) is str and re.fullmatch(r'[0-9]+:[0-9]+', cidata['whole_device']) is not None and
                 cidata['whole_device'] != cidata['device'] and seed['whole_device'] != cidata['whole_device'] and
                 seed['device'] != cidata['device'] and seed['id'] != cidata['id'] and
                 seed.get('uuid') == image.get('uuid') and seed['bytes'] == image.get('size') and
                 seed.get('block_sha256') == image.get('sha256') and
                 cidata['bytes'] == cidata_block['size'] and cidata.get('block_sha256') == cidata_block['sha256'],
                 'guest carrier whole-block bytes or filesystem identity differs')
    host.require(type(seed.get('super_options')) is list and
                 bool({'noload', 'norecovery'} & set(seed['super_options'])),
                 'guest payload filesystem did not disable journal recovery')
    installation = result['isolation'].get('preparer_install')
    host.require(type(installation) is dict and
                 installation.get('seed_manifest_sha256') == record['seed']['SHA256SUMS'] and
                 installation.get('payload_case_sha256') == record['seed']['case.json'] and
                 installation.get('seed_descriptor_sha256') == record['cidata']['seed-descriptor.json'],
                 'guest INSTALL did not bind both carriers')
    realm = result.get('realm')
    if type(realm) is dict and 'hardware' in realm:
        host.require(realm['hardware'] == hardware, 'guest hardware observations conflict')


def _started_us(value):
    instant = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
    if instant.tzinfo is None:
        raise ValueError('backend timestamp has no timezone')
    delta = instant.astimezone(datetime.timezone.utc) - datetime.datetime(1970, 1, 1, tzinfo=datetime.timezone.utc)
    return str((delta.days * 86400 + delta.seconds) * 1000000 + delta.microseconds)



def _dns_evidence(host, result, record):
    proof = result.get('isolation', {}).get('dns_preflight')
    host.require(type(proof) is dict and set(proof) == {'version', 'program_sha256', 'query', 'resolver', 'tools',
                                                        'probe_joined', 'endpoint_removed'} and
                 type(proof['version']) is int and proof['version'] == 1 and
                 proof['program_sha256'] == record['seed']['dns-probe'] and proof['probe_joined'] is True and
                 proof['endpoint_removed'] is True, 'pre-genesis DNS isolation proof is missing')
    query = proof['query']
    host.require(type(query) is dict and set(query) == {'version', 'label_sha256', 'upstream_refused',
                                                       'resolver_outcome', 'resolver_rcode'} and
                 type(query['version']) is int and query['version'] == 1 and query['upstream_refused'] is True and
                 type(query['label_sha256']) is str and HEX.fullmatch(query['label_sha256']) is not None and
                 query['resolver_outcome'] in ('timeout', 'noanswer') and type(query['resolver_rcode']) is int and
                 ((query['resolver_outcome'] == 'timeout' and query['resolver_rcode'] == -1) or
                  (query['resolver_outcome'] == 'noanswer' and 0 <= query['resolver_rcode'] <= 15)),
                 'actual fixed DNS query and unreachable upstream are not evidenced')
    resolver = proof['resolver']
    host.require(type(resolver) is dict and set(resolver) == {'netns', 'sockets', 'resolv_sha256', 'nat_sha256'} and
                 type(resolver['netns']) is dict and set(resolver['netns']) == {'dev', 'ino'} and
                 type(resolver['sockets']) is dict and set(resolver['sockets']) == {'tcp', 'udp'} and
                 all(type(resolver[key]) is str and HEX.fullmatch(resolver[key]) is not None
                     for key in ('resolv_sha256', 'nat_sha256')) and all(
                     type(endpoint) is dict and set(endpoint) == {'address', 'port', 'inode'} and
                     endpoint['address'] == '127.0.0.11' and type(endpoint['port']) is int and
                     0 < endpoint['port'] < 65536 and type(endpoint['inode']) is int and endpoint['inode'] > 0
                     for endpoint in resolver['sockets'].values()) and
                 len({endpoint['inode'] for endpoint in resolver['sockets'].values()}) == 2,
                 'pre-genesis resolver socket and NAT binding is missing')
    tools = proof['tools']
    host.require(type(tools) is dict and set(tools) == {'nsenter', 'iptables'} and all(
        type(item) is dict and set(item) == {'path', 'dev', 'ino', 'sha256'} and
        type(item['path']) is str and item['path'].startswith('/usr/') and type(item['dev']) is int and
        type(item['ino']) is int and item['ino'] > 0 and type(item['sha256']) is str and
        HEX.fullmatch(item['sha256']) is not None for item in tools.values()), 'DNS observation tools are not pinned')
    return proof


def _systemd_stop_evidence(host, closure):
    """Validate exported metadata; kernel attribution remains bound to root evidence."""
    identity_keys = {'dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtime_ns', 'ctime_ns'}

    def identity(value):
        host.require(type(value) is dict and set(value) == identity_keys and
                     all(type(value[key]) is int and value[key] >= 0 for key in identity_keys) and
                     value['ino'] > 0 and value['nlink'] > 0 and
                     value['uid'] == value['gid'] == 0 and stat.S_ISDIR(value['mode']) and
                     value['mode'] & 0o022 == 0, 'systemd stop directory identity invalid')

    def digest(value):
        return type(value) is str and HEX.fullmatch(value) is not None and value != '0' * 64

    preflight, transition = closure['stop_preflight'], closure['stop_transition']
    host.require(type(preflight) is dict and set(preflight) ==
                 {'unit', 'disposition', 'run_identity', 'pid_file_present'} and
                 preflight['unit'] == 'multipathd.service' and
                 preflight['disposition'] in ('already_quiet', 'stop_required') and
                 type(preflight['pid_file_present']) is bool, 'systemd stop preflight invalid')
    identity(preflight['run_identity'])
    if preflight['disposition'] == 'already_quiet':
        host.require(preflight['pid_file_present'] is False and transition is None,
                     'systemd quiet preflight has a stop transition')
        return preflight['run_identity']
    host.require(preflight['pid_file_present'] is True and type(transition) is dict and set(transition) ==
                 {'version', 'unit', 'entry', 'actor', 'before', 'after', 'pid_file', 'event', 'settlement'} and
                 type(transition['version']) is int and transition['version'] == 1 and
                 transition['unit'] == 'multipathd.service' and transition['entry'] == 'multipathd.pid',
                 'systemd required stop transition missing or invalid')
    before, after = transition['before'], transition['after']
    identity(before)
    identity(after)
    host.require(before == preflight['run_identity'] and
                 all(before[key] == after[key] for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink')),
                 'systemd stop directory transition differs')
    actor = transition['actor']
    host.require(type(actor) is dict and set(actor) == {'pid', 'starttime', 'executable_sha256'} and
                 type(actor['pid']) is int and actor['pid'] > 1 and
                 type(actor['starttime']) is int and actor['starttime'] > 0 and digest(actor['executable_sha256']),
                 'systemd stop actor invalid')
    pid_file = transition['pid_file']
    host.require(type(pid_file) is dict and set(pid_file) ==
                 {'dev', 'ino', 'mode', 'uid', 'gid', 'size', 'mtime_ns', 'ctime_before_ns', 'ctime_after_ns',
                  'sha256', 'nlink_before', 'nlink_after'} and
                 all(type(value) is int and value >= 0 for key, value in pid_file.items() if key != 'sha256') and
                 pid_file['dev'] == before['dev'] and pid_file['ino'] > 0 and
                 pid_file['uid'] == pid_file['gid'] == 0 and pid_file['mode'] == stat.S_IFREG | 0o644 and
                 1 <= pid_file['size'] <= 32 and pid_file['nlink_before'] == 1 and pid_file['nlink_after'] == 0 and
                 digest(pid_file['sha256']),
                 'systemd stop PID file identity differs')
    event = transition['event']
    host.require(type(event) is dict and set(event) ==
                 {'kind', 'actor_pid', 'actor_starttime', 'directory_handle_sha256', 'target_handle_sha256', 'raw_events_sha256'} and
                 event['kind'] == 'delete' and type(event['actor_pid']) is int and
                 type(event['actor_starttime']) is int and event['actor_pid'] == actor['pid'] and
                 event['actor_starttime'] == actor['starttime'] and
                 all(digest(event[key]) for key in ('directory_handle_sha256', 'target_handle_sha256', 'raw_events_sha256')),
                 'systemd stop event binding differs')
    settlement = transition['settlement']
    host.require(type(settlement) is dict and set(settlement) ==
                 {'stop_returncode', 'tracer_wait_status', 'pidfd_exited', 'tracer_joined', 'remaining_jobs', 'cgroup_empty'} and
                 all(type(settlement[key]) is int and settlement[key] == 0
                     for key in ('stop_returncode', 'tracer_wait_status', 'remaining_jobs')) and
                 all(settlement[key] is True for key in ('pidfd_exited', 'tracer_joined', 'cgroup_empty')),
                 'systemd stop settlement incomplete')
    return after


def _systemd_evidence(host, systemd):
    from source_supervisor.systemd_realm import (ALLOWED, PROPERTIES, UNIT, DEPENDENCY_UNIT, UNIT_PATHS,
                                                MASK_ROOTS, CONTROL_ROOT, NETPLAN, NETPLAN_MASK, _decode_string_array)

    def array(value, kind='unit'):
        try:
            items = _decode_string_array(value)
        except ValueError:
            host.require(False, 'systemd string array encoding invalid')
            raise
        if kind == 'path':
            valid = all(item.startswith('/') and not item.startswith('//') and item != '/' and
                        posixpath.normpath(item) == item and all(32 <= ord(char) < 127 for char in item)
                        for item in items)
        else:
            pattern = UNIT if kind == 'name' else DEPENDENCY_UNIT
            valid = all(len(item) <= 255 and pattern.fullmatch(item) for item in items)
        host.require(valid, 'systemd string array element invalid')
        return items
    host.require(type(systemd) is dict and set(systemd) ==
                 {'unit_count', 'active_units', 'override_digest', 'override_transition', 'digest', 'closure'} and
                 type(systemd['unit_count']) is int and 0 < systemd['unit_count'] <= 8192 and
                 type(systemd['active_units']) is list and all(type(unit) is str for unit in systemd['active_units']) and
                 systemd['active_units'] == sorted(set(systemd['active_units'])) and
                 set(systemd['active_units']) <= ALLOWED and type(systemd['digest']) is str and
                 HEX.fullmatch(systemd['digest']) is not None and systemd['digest'] != '0' * 64,
                 'genesis systemd closure evidence missing')
    closure = systemd['closure']
    host.require(type(closure) is dict and set(closure) ==
                 {'allowed', 'files', 'active', 'override_digest', 'unit_path', 'stop_preflight', 'stop_transition'} and
                 closure['allowed'] == sorted(ALLOWED) and closure['override_digest'] == systemd['override_digest'] and
                 type(closure['files']) is list and type(closure['active']) is list and
                 _sha(closure) == systemd['digest'], 'systemd closure digest or schema differs')
    run_identity = _systemd_stop_evidence(host, closure)
    unit_path = closure['unit_path']
    host.require(type(unit_path) is list and 0 < len(unit_path) <= 64 and
                 all(type(path) is str and path.startswith('/') and not path.startswith('//') and path != '/' and
                     posixpath.normpath(path) == path and all(32 <= ord(c) < 127 for c in path) for path in unit_path) and
                 len(set(unit_path)) == len(unit_path) and
                 all(unit_path.count(path) == 1 for path in (CONTROL_ROOT, '/etc/systemd/system', '/run/systemd/system')) and
                 unit_path.index(CONTROL_ROOT) < min(unit_path.index('/etc/systemd/system'),
                                                     unit_path.index('/run/systemd/system')),
                 'systemd manager unit path differs')
    routes, active = closure['files'], closure['active']
    host.require(len(routes) <= systemd['unit_count'] <= len(routes) + len(ALLOWED) and
                 len(routes) <= 8192 and len(active) <= len(ALLOWED), 'systemd closure inventory count differs')
    host.require(all(type(row) is list and len(row) == 4 and all(type(value) is str for value in row) and
                     row[0] in ALLOWED and row[1] == 'loaded' and row[2] in ('active', 'activating', 'reloading') and
                     bool(row[3]) and len(row[3]) <= 255 and all(32 <= ord(char) < 127 for char in row[3])
                     for row in active) and
                 active == sorted(active) and len({row[0] for row in active}) == len(active) and
                 [row[0] for row in active] == systemd['active_units'], 'systemd active closure differs')

    def unit_name(name):
        return type(name) is str and len(name) <= 255 and UNIT.fullmatch(name) is not None

    entries, parents = {}, {}

    def entry(value, *, control=False):
        host.require(type(value) is dict and set(value) ==
                     {'path', 'dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'ctime_ns', 'target', 'parents'} and
                     type(value['path']) is str and type(value['parents']) is list and
                     all(type(value[key]) is int for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'ctime_ns')) and
                     value['dev'] >= 0 and value['ino'] > 0 and value['nlink'] == 1 and value['ctime_ns'] >= 0 and
                     value['uid'] == value['gid'] == 0 and
                     (stat.S_ISLNK(value['mode']) and type(value['target']) is str and bool(value['target']) or
                      stat.S_ISREG(value['mode']) and value['target'] is None and value['nlink'] == 1 and
                      value['mode'] & 0o022 == 0), 'systemd route entry identity differs')
        root, name = posixpath.split(value['path'])
        host.require((root in UNIT_PATHS or control and root == CONTROL_ROOT) and unit_name(name), 'systemd route entry path differs')
        expected, current = [], ''
        for part in root.strip('/').split('/'):
            current += '/' + part
            expected.append(current)
        host.require(len(value['parents']) == len(expected), 'systemd route parent count differs')
        for item, path in zip(value['parents'], expected):
            host.require(type(item) is dict and set(item) == {'path', 'dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns'} and
                         item['path'] == path and
                         all(type(item[key]) is int for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns')) and
                         item['dev'] >= 0 and item['ino'] > 0 and item['ctime_ns'] >= 0 and
                         item['uid'] == item['gid'] == 0 and stat.S_ISDIR(item['mode']) and
                         item['mode'] & 0o022 == 0 and (path not in parents or parents[path] == item),
                         'systemd route parent identity differs')
            parents[path] = item
            if path == '/run':
                host.require(all(item[key] == run_identity[key]
                                 for key in ('dev', 'ino', 'mode', 'uid', 'gid', 'ctime_ns')),
                             'systemd route does not bind settled run identity')
        host.require(value['path'] not in entries or entries[value['path']] == value,
                     'systemd route entry identity changed')
        entries[value['path']] = value
        return root, name

    def link_target(link):
        root, source = entry(link)
        target = link['target']
        host.require(stat.S_ISLNK(link['mode']) and type(target) is str and target != '/dev/null' and
                     '@.' not in source, 'systemd alias link differs')
        path = target if target.startswith('/') else posixpath.join(root, target)
        target_root, name = posixpath.split(path)
        host.require(target_root in UNIT_PATHS and unit_name(name) and '@.' not in name and name != source and
                     name not in ALLOWED and name.rsplit('.', 1)[-1] == source.rsplit('.', 1)[-1],
                     'systemd alias target differs')
        return path, name

    route_names = []
    for route in routes:
        host.require(type(route) is dict and set(route) in ({'name', 'state', 'terminal', 'mask', 'aliases', 'manager'},
                                                          {'name', 'state', 'terminal', 'mask', 'aliases', 'manager', 'control_origin'}) and
                     unit_name(route['name']) and route['name'] not in ALLOWED and
                     route['state'] in ('masked', 'masked-runtime') and unit_name(route['terminal']) and
                     route['terminal'] not in ALLOWED and type(route['aliases']) is list and
                     type(route['manager']) is list and len(route['aliases']) < 16,
                     'systemd mask route schema differs')
        route_names.append(route['name'])
        control = 'control_origin' in route
        root, terminal = entry(route['mask'], control=control)
        host.require((root in MASK_ROOTS if not control else root == CONTROL_ROOT) and terminal == route['terminal'] and
                     stat.S_ISLNK(route['mask']['mode']) and route['mask']['target'] == '/dev/null',
                     'systemd route terminal mask differs')
        origin = route.get('control_origin')
        if control:
            host.require(type(origin) is dict and set(origin) == {'terminal', 'aliases'} and
                         unit_name(origin['terminal']) and origin['terminal'] not in ALLOWED and
                         type(origin['aliases']) is list and 0 < len(origin['aliases']) < 16 and
                         route['name'] == terminal and route['aliases'] == [] and route['state'] == 'masked-runtime' and
                         route['mask']['path'] == CONTROL_ROOT + '/' + route['name'], 'systemd control origin differs')
        lineage = origin['aliases'] if control else route['aliases']
        current, seen = route['name'], set()
        for alias in lineage:
            host.require(current not in seen and type(alias) is dict and set(alias) == {'link', 'target_name', 'source'} and
                         type(alias['source']) is list and 0 < len(alias['source']) <= 16,
                         'systemd alias route schema differs')
            seen.add(current)
            source_path, target = link_target(alias['link'])
            host.require(posixpath.basename(alias['link']['path']) == current and alias['target_name'] == target and
                         posixpath.dirname(alias['link']['path']) != '/run/systemd/generator.early',
                         'systemd alias route name or precedence differs')
            visited = set()
            for index, item in enumerate(alias['source']):
                source_root, _ = entry(item)
                host.require(item['path'] == source_path and source_path not in visited,
                             'systemd literal alias source differs')
                visited.add(source_path)
                if index == len(alias['source']) - 1:
                    host.require(stat.S_ISREG(item['mode']) or
                                 stat.S_ISLNK(item['mode']) and item['target'] == '/dev/null' and source_root in MASK_ROOTS,
                                 'systemd alias source does not terminate safely')
                else:
                    source_path, _ = link_target(item)
            current = target
        host.require(current == (origin['terminal'] if control else route['terminal']) and current not in seen,
                     'systemd alias route cycles or ends incorrectly')
        if not route['aliases']:
            host.require(route['state'] == ('masked' if root == '/etc/systemd/system' else 'masked-runtime'),
                         'systemd direct mask state differs')
            if control:
                host.require(len(route['manager']) == 1, 'systemd control manager coverage differs')
                manager = route['manager'][0]
                host.require(type(manager) is dict and set(manager) ==
                             {'name', 'id', 'names', 'load_state', 'fragment_path'} and
                             manager['name'] == manager['id'] == route['name'] and type(manager['names']) is str and
                             manager['load_state'] == 'masked' and
                             manager['fragment_path'] in (route['mask']['path'], '/dev/null') and
                             array(manager['names'], 'name') == [route['name']], 'systemd control manager evidence differs')
            elif route['name'] == terminal == NETPLAN and route['mask']['path'] == NETPLAN_MASK:
                host.require(len(route['manager']) == 1, 'systemd netplan manager coverage differs')
                manager = route['manager'][0]
                host.require(type(manager) is dict and set(manager) ==
                             {'name', 'id', 'names', 'load_state', 'fragment_path'} and
                             manager['name'] == manager['id'] == NETPLAN and type(manager['names']) is str and
                             manager['load_state'] == 'masked' and
                             manager['fragment_path'] in (NETPLAN_MASK, '/dev/null'),
                             'systemd netplan manager evidence differs')
                host.require(array(manager['names'], 'name') == [NETPLAN], 'systemd netplan names differ')
            else:
                host.require(route['manager'] == [], 'systemd direct manager evidence differs')
        else:
            expected = [posixpath.basename(alias['link']['path']) for alias in route['aliases']] + [terminal]
            host.require(len(route['manager']) == len(expected), 'systemd manager alias coverage differs')
            for manager, name in zip(route['manager'], expected):
                host.require(type(manager) is dict and set(manager) ==
                             {'name', 'id', 'names', 'load_state', 'fragment_path'} and manager['name'] == name and
                             manager['id'] == terminal and type(manager['names']) is str and
                             manager['load_state'] == 'masked' and
                             manager['fragment_path'] in (route['mask']['path'], '/dev/null'),
                             'systemd manager mask evidence differs')
                names = array(manager['names'], 'name')
                host.require(set(expected) <= set(names) and not (set(names) & ALLOWED),
                             'systemd manager alias identity differs')
    host.require(route_names == sorted(set(route_names)) and
                 all(route['terminal'] in route_names for route in routes), 'systemd mask route names differ')
    by_name = {route['name']: route for route in routes}
    for route in routes:
        if 'control_origin' in route:
            terminal = by_name.get(route['control_origin']['terminal'])
            host.require(terminal is not None and 'control_origin' not in terminal and
                         terminal['name'] == terminal['terminal'] and not terminal['aliases'],
                         'systemd control origin terminal missing')
    transition = systemd['override_transition']
    if transition is None:
        host.require(systemd['override_digest'] is None, 'absent override has a digest')
        return
    host.require(type(transition) is dict and set(transition) ==
                 {'preflight', 'overridden', 'fragment_identity', 'fragment_sha256', 'override_directory_identity',
                  'override_file_identity', 'override_sha256', 'masked_before_stop', 'masked_after_stop'} and
                 _sha(transition) == systemd['override_digest'], 'systemd override transition binding differs')
    unit = 'systemd-tmpfiles-setup.service'
    override = '/run/systemd/system/' + unit + '.d/90-g2-manual-stop.conf'
    preflight, overridden = transition['preflight'], transition['overridden']
    maps = [preflight, overridden]
    for stage in ('masked_before_stop', 'masked_after_stop'):
        item = transition[stage]
        host.require(type(item) is dict and set(item) == {'mask', 'properties'}, 'systemd masked transition missing')
        mask = item['mask']
        host.require(type(mask) is dict and set(mask) == {'path', 'dev', 'ino'} and
                     mask['path'] in ('/etc/systemd/system/' + unit, '/run/systemd/system/' + unit) and
                     type(mask['dev']) is int and mask['dev'] >= 0 and type(mask['ino']) is int and mask['ino'] > 0,
                     'systemd effective mask identity differs')
        maps.append(item['properties'])
    for properties in maps:
        host.require(type(properties) is dict and set(properties) <= set(PROPERTIES) and
                     all(type(value) is str for value in properties.values()) and properties.get('Id') == unit and
                     properties.get('MainPID') == properties.get('ControlPID') == '0' and
                     all(properties.get(key, '') == '' for key in ('ExecStop', 'ExecStopPost')) and
                     all(array(properties.get(key, '')) == [] for key in
                         ('PropagatesStopTo', 'StopPropagatedFrom', 'PartOf', 'BindsTo')) and
                     all(not (set(array(properties.get(key, ''))) & ALLOWED) for key in
                         ('RequiredBy', 'RequisiteOf', 'BoundBy', 'ConsistsOf')),
                     'systemd stop side effects or live process properties differ')
        host.require(unit in array(properties.get('Names', ''), 'name'), 'systemd canonical name missing')
    host.require(preflight.get('ActiveState') == 'active' and preflight.get('SubState') == 'exited' and
                 preflight.get('RefuseManualStop') == 'yes' and preflight.get('Type') == 'oneshot' and
                 preflight.get('RemainAfterExit') == 'yes' and preflight.get('Restart') == 'no' and
                 preflight.get('FragmentPath') in ('/usr/lib/systemd/system/' + unit, '/lib/systemd/system/' + unit) and
                 array(preflight.get('DropInPaths', ''), 'path') == [] and
                 array(overridden.get('DropInPaths', ''), 'path') == [override] and
                 overridden == dict(preflight, RefuseManualStop='no', DropInPaths=overridden.get('DropInPaths')),
                 'systemd override changed unapproved effective properties')
    before, after = transition['masked_before_stop'], transition['masked_after_stop']
    host.require(before['mask'] == after['mask'] and before['properties'].get('ActiveState') == 'active' and
                 before['properties'].get('SubState') == 'exited' and before['properties'].get('RefuseManualStop') == 'no' and
                 after['properties'].get('ActiveState') in ('inactive', 'failed'), 'systemd mask or stop transition differs')
    tmpfiles = [route for route in routes if route['name'] == unit]
    host.require(len(tmpfiles) == 1 and not tmpfiles[0]['aliases'] and
                 {key: tmpfiles[0]['mask'][key] for key in ('path', 'dev', 'ino')} == before['mask'],
                 'systemd tmpfiles transition and closure mask differ')
    for item in (before, after):
        properties = item['properties']
        host.require(properties.get('LoadState') == 'masked' and
                     properties.get('FragmentPath') in (item['mask']['path'], '/dev/null') and
                     array(properties.get('DropInPaths', ''), 'path') in ([], [override]), 'systemd effective final mask differs')
    identity = transition['fragment_identity']
    host.require(type(identity) is dict and set(identity) == {'dev', 'ino', 'size', 'mtime_ns', 'ctime_ns'} and
                 all(type(value) is int and value >= 0 for value in identity.values()) and identity['ino'] > 0 and
                 identity['size'] > 0 and type(transition['fragment_sha256']) is str and
                 HEX.fullmatch(transition['fragment_sha256']) is not None and transition['fragment_sha256'] != '0' * 64 and
                 transition['override_sha256'] == hashlib.sha256(b'[Unit]\nRefuseManualStop=no\n').hexdigest(),
                 'systemd retained vendor or override bytes differ')
    for key in ('override_directory_identity', 'override_file_identity'):
        identity = transition[key]
        host.require(type(identity) is list and len(identity) == 2 and all(type(value) is int for value in identity) and
                     identity[0] >= 0 and identity[1] > 0, 'systemd runtime override inode missing')


def _root_journal(host, result, record, require_valid, allow_retired_maintenance=False):
    facts, realm, history = result.get('genesis'), result.get('realm'), result.get('root_journal')
    host.require(type(facts) is dict and type(realm) is dict and type(history) is dict,
                 'root genesis, APP binding, or journal missing')
    genesis_bytes = _canonical(facts)
    host.require(0 < len(genesis_bytes) <= 512 * 1024, 'genesis evidence exceeds storage limit')
    binding = realm.get('app_binding')
    host.require(type(binding) is dict and history.get('realm-genesis.json') == facts and
                 history.get('realm-app.json') == binding, 'root genesis or APP binding changed')
    host.require(facts.get('version') == 1 and facts.get('run_token') == RUN_TOKEN and
                 facts.get('case') == record['case'] and facts.get('scenario') == record['scenario'] and
                 facts.get('host_launch_sha256') == record['host_launch_sha256'] and
                 facts.get('host_argv_sha256') == record['host_launch']['argv_sha256'] and
                 facts.get('qemu_sha256') == record['host_launch']['qemu_sha256'] and
                 facts.get('seed_manifest_sha256') == record['seed']['SHA256SUMS'] and
                 facts.get('payload_case_sha256') == record['seed']['case.json'] and
                 facts.get('seed_descriptor_sha256') == record['cidata']['seed-descriptor.json'],
                 'genesis host, seed, or case binding differs')
    _systemd_evidence(host, facts.get('systemd'))
    host.require(facts.get('programs') == {name: record['seed'][name] for name in source_build.BINARIES} and
                 facts.get('supervisor_manifest_sha256') == record['seed']['supervisor-manifest.json'],
                 'genesis program and supervisor pins differ from the selected seed')
    host.require(facts.get('dns_preflight') == result.get('isolation', {}).get('dns_preflight') and
                 facts.get('programs', {}).get('dns-probe') == record['seed']['dns-probe'],
                 'genesis does not bind its no-forwarding probe')
    host.require(type(facts.get('supervisor_pid')) is int and facts['supervisor_pid'] > 0 and
                 type(facts.get('supervisor_starttime')) is int and facts['supervisor_starttime'] > 0 and
                 type(facts.get('boot_id')) is str and len(facts['boot_id']) == 36,
                 'root supervisor continuity identity missing')
    groups = (('child_cgroup', '/sys/fs/cgroup/g2-c80-predecessor'),
              ('writer_cgroup', '/sys/fs/cgroup/g2-writers'), ('migration_cgroup', MIGRATION_CGROUP))
    for name, path in groups:
        group = facts.get(name)
        host.require(type(group) is dict and set(group) == {'path', 'dev', 'ino'} and group['path'] == path and
                     type(group['dev']) is int and group['dev'] >= 0 and type(group['ino']) is int and group['ino'] > 0,
                     'genesis cgroup identity is invalid')
    host.require(len({(facts[name]['dev'], facts[name]['ino']) for name, _ in groups}) == len(groups),
                 'genesis cgroup identities overlap')
    authority = facts.get('migration_binding')
    host.require(type(authority) is dict and set(authority) == {'path', 'dev', 'ino', 'uid', 'gid', 'mode', 'nlink', 'sha256', 'raw'} and
                 authority['path'] == MIGRATION_BINDING and type(authority['dev']) is int and authority['dev'] >= 0 and
                 type(authority['ino']) is int and authority['ino'] > 0 and
                 all(type(authority[key]) is int for key in ('uid', 'gid', 'mode', 'nlink')) and
                 (authority['uid'], authority['gid'], authority['mode'], authority['nlink']) == (0, 10002, 0o440, 1) and
                 type(authority['sha256']) is str and HEX.fullmatch(authority['sha256']) is not None and
                 type(authority['raw']) is str, 'genesis migration binding file identity is invalid')
    raw = authority['raw'].encode('ascii', 'strict')
    migration_binding = {'version': 1, 'boot_id': facts['boot_id'], 'program_sha256': record['seed']['c81-migrator'],
                         'coordinator_sha256': record['seed']['houfeng-record-platform-admin'], 'cgroup': facts['migration_cgroup']}
    host.require(raw == _canonical(migration_binding) and host.strict_json(raw) == migration_binding and
                 hashlib.sha256(raw).hexdigest() == authority['sha256'], 'migration binding bytes or pins differ')
    digest = inventory_digest('HOUFENG-C80-REALM-V1', genesis_bytes, _canonical(binding)).hex()
    host.require(realm.get('realm_digest') == digest and digest != '0' * 64,
                 'stable realm commitment differs')
    volumes = facts.get('volumes')
    physical = binding.get('physical')
    host.require(type(volumes) is dict and set(volumes) == {'app', 'w1', 'w2', 'control'} and
                 type(physical) is dict and set(physical) == set(volumes), 'four-disk identity missing')
    hardware = result['isolation']['hardware']
    host.require(not ({hardware['seed_mount']['device'], hardware['cidata_mount']['device'],
                       hardware['seed_mount']['whole_device'], hardware['cidata_mount']['whole_device']} &
                     {volumes[name]['disk']['device'] for name in volumes}),
                 'seed carrier overlaps a business volume')
    host.require(len({volumes[name]['mount_id'] for name in volumes}) == 4 and
                 len({physical[name]['system_identifier'] for name in physical}) == 4,
                 'disk mounts or PostgreSQL system identities overlap')
    for name, serial in source_vm.DISKS:
        item = volumes[name]
        host.require(item['disk']['serial'] == serial and item['disk']['bytes'] == 8 * 1024 ** 3 and
                     physical[name]['database_name'] == {'app': 'houfeng', 'w1': 'g2_w1',
                                                         'w2': 'g2_w2', 'control': 'g2_control'}[name] and
                     type(physical[name]['database_oid']) is int and physical[name]['database_oid'] > 0,
                     'volume or database identity differs')
    app = binding.get('application')
    host.require(type(app) is dict and app.get('SystemIdentifier') == physical['app']['system_identifier'] and
                 app.get('DatabaseOID') == physical['app']['database_oid'] and app.get('DatabaseName') == 'houfeng',
                 'APP binding is not its observed physical database')
    names = sorted(name for name in history if name.startswith('journal-'))
    host.require(names == ['journal-%020d.json' % index for index in range(1, len(names) + 1)] and names,
                 'root journal sequence is missing')
    previous, invalid = '0' * 64, False
    for index, name in enumerate(names, 1):
        event = history[name]
        host.require(type(event) is dict and set(event) == {'version', 'sequence', 'previous', 'kind', 'fields'} and
                     event['version'] == 1 and type(event['sequence']) is int and event['sequence'] == index and
                     event['previous'] == previous and event['kind'] in EVENT_KEYS and type(event['fields']) is dict and
                     frozenset(event['fields']) == EVENT_KEYS[event['kind']], 'root journal chain differs')
        host.require((event['kind'] == 'genesis' and
                      event['fields'] == {'genesis_digest': hashlib.sha256(genesis_bytes).hexdigest()})
                     if index == 1 else event['kind'] != 'genesis',
                     'root genesis event binding differs')
        previous = _sha(event)
        invalid = invalid or event['kind'] == 'invalid' or event['kind'] == 'observation' and event['fields']['valid'] is False
    scopes = result.get('migration_scopes', [])
    host.require(type(scopes) is list and len(scopes) <= 16, 'maintenance scope evidence is invalid')
    scoped_names = ('legacy-c81-migrate', 'legacy-retirement-reconcile', 'reconcile-legacy')
    if 'action_order' in result:
        order = result['action_order']
        host.require(type(order) is list, 'maintenance action order is invalid')
        start = order.index('legacy-c81-migrate') if 'legacy-c81-migrate' in order else len(order)
        expected_scopes = [name for name in order[start:] if name in scoped_names]
        host.require([scope.get('enrollment', {}).get('command') for scope in scopes] == expected_scopes,
                     'maintenance scope evidence does not cover fixed invocations')
    terminal_event = terminal_hash = None
    if scopes:
        raw_witness = result.get('maintenance_witness')
        host.require(type(raw_witness) is list, 'maintenance witness chain missing')
        terminals = []
        for text in raw_witness:
            host.require(type(text) is str, 'maintenance witness bytes missing')
            raw = text.encode('utf-8', 'strict')
            event = host.strict_json(raw)
            if type(event) is dict and event.get('kind') == 'terminal':
                terminals.append((event, hashlib.sha256(raw).hexdigest()))
        host.require(bool(terminals), 'witnessed terminal missing')
    registrations, seen_coordinators = [], set()
    for scope in scopes:
        host.require(type(scope) is dict and set(scope) == {'enrollment', 'retirement'} and
                     type(scope['enrollment']) is dict and type(scope['retirement']) is dict,
                     'maintenance scope evidence shape differs')
        enrollment, retirement = scope['enrollment'], scope['retirement']
        host.require(set(enrollment) == {'boot_id', 'cgroup', 'binding_sha256', 'command', 'request_file',
                                         'fault_channel', 'coordinator', 'before', 'enrolled'} and
                     set(retirement) == {'boot_id', 'cgroup', 'binding_sha256', 'command', 'coordinator', 'after',
                                         'status', 'terminal_acks', 'coordinator_pidfd_exited', 'coordinator_direct_wait'} and
                     enrollment['boot_id'] == retirement['boot_id'] == facts['boot_id'] and
                     enrollment['cgroup'] == retirement['cgroup'] == facts['migration_cgroup'] and
                     enrollment['binding_sha256'] == retirement['binding_sha256'] == authority['sha256'] and
                     enrollment['command'] == retirement['command'] and enrollment['command'] in scoped_names and
                     enrollment['coordinator'] == retirement['coordinator'] and type(enrollment['fault_channel']) is bool and
                     type(enrollment['request_file']) is str and
                     re.fullmatch(r'/var/lib/g2/control/maintenance/requests/[a-z0-9-]+\.json', enrollment['request_file']) is not None and
                     type(retirement['status']) is int and type(retirement['terminal_acks']) is list,
                     'maintenance scope continuity differs')
        coordinator, command = enrollment['coordinator'], enrollment['command']
        host.require(type(coordinator) is dict and set(coordinator) == {'pid', 'starttime', 'executable_sha256'} and
                     type(coordinator['pid']) is int and coordinator['pid'] > 0 and
                     type(coordinator['starttime']) is int and coordinator['starttime'] > 0 and
                     coordinator['executable_sha256'] == record['seed']['houfeng-record-platform-admin'] and
                     enrollment['before'] == {'members': [], 'populated': 0} and
                     enrollment['enrolled'] == {'members': [coordinator['pid']], 'populated': 1} and
                     retirement['after'] == {'members': [], 'populated': 0} and
                     retirement['coordinator_pidfd_exited'] is True and retirement['coordinator_direct_wait'] is True,
                     'maintenance scope was not entered and retired by the original coordinator')
        identity = (coordinator['pid'], coordinator['starttime'])
        host.require(identity not in seen_coordinators, 'maintenance coordinator lifetime was reused')
        seen_coordinators.add(identity)
        if retirement['status'] == 0 or command != 'legacy-c81-migrate' and retirement['status'] < 0:
            host.require(bool(retirement['terminal_acks']), 'successful or cutpoint coordinator lacked terminal ACK')
        for ack in retirement['terminal_acks']:
            host.require(type(ack) is dict and type(ack.get('witness_sequence')) is int,
                         'terminal ACK sequence missing')
            matches = [(event, digest) for event, digest in terminals if event['sequence'] == ack['witness_sequence']]
            host.require(len(matches) == 1, 'terminal ACK does not identify one historical witness')
            terminal_event, terminal_hash = matches[0]
            host.require(type(ack) is dict and set(ack) == {'family', 'recovery_id', 'application_digest',
                         'phase_generation', 'attempt', 'kind', 'record_sha256', 'witness_sequence', 'witness_digest'} and
                         ack['family'] == 'migration' and ack['kind'] == 'terminal' and type(ack['attempt']) is int and
                         ack['attempt'] == 0 and ack['recovery_id'] == terminal_event['recovery_id'] and
                         ack['application_digest'] == terminal_event['application_digest'] and
                         ack['phase_generation'] == terminal_event['phase_generation'] and
                         ack['record_sha256'] == terminal_event['record_sha256'] and
                         ack['witness_sequence'] == terminal_event['sequence'] and ack['witness_digest'] == terminal_hash,
                         'coordinator did not receive the retained terminal witness')
        family = 'source-authority' if command == 'legacy-retirement-reconcile' else 'record-deletion'
        action = {'program': 'houfeng-record-platform-admin',
                  'argv': ['/opt/g2/houfeng-record-platform-admin', family, command, '--request-file', enrollment['request_file']],
                  'config_digest': None, 'has_fd5_channel': True, 'has_fault_channel': enrollment['fault_channel'],
                  'has_sealed_dump': False}
        events = [history[name] for name in names]
        admitted = [event for event in events if event['kind'] == 'admission' and
                    event['fields'].get('name') == 'direct-houfeng-record-platform-admin' and
                    event['fields'].get('action_digest') == _sha(action)]
        launched = [event for event in events if event['kind'] == 'launch' and
                    event['fields'].get('name') == 'houfeng-record-platform-admin' and
                    event['fields'].get('pid') == coordinator['pid'] and event['fields'].get('starttime') == coordinator['starttime']]
        exited = [event for event in events if event['kind'] == 'exit' and
                  event['fields'].get('name') == 'houfeng-record-platform-admin' and
                  event['fields'].get('pid') == coordinator['pid'] and event['fields'].get('starttime') == coordinator['starttime'] and
                  event['fields'].get('status') == retirement['status'] and event['fields'].get('pidfd_exited') is True]
        host.require(len(admitted) == len(launched) == len(exited) == 1 and
                     admitted[0]['sequence'] < launched[0]['sequence'] < exited[0]['sequence'],
                     'fixed coordinator FD5 admission or direct wait is missing')
        registrations.extend((('c81-scope-enrolled', _sha(enrollment)), ('c81-scope-retired', _sha(retirement))))
    journal_scopes = [(history[name]['fields']['name'], history[name]['fields']['identity_digest']) for name in names
                      if history[name]['kind'] == 'registration' and history[name]['fields']['name'] in
                      ('c81-scope-enrolled', 'c81-scope-retired')]
    host.require(journal_scopes == registrations, 'maintenance scope journal continuity differs')
    resolvers = result.get('resolver_inventory')
    host.require(type(resolvers) is dict and set(resolvers) == {'app', 'w1', 'w2', 'control'},
                 'four registered Docker DNS resolvers are missing')
    namespaces, inodes = set(), set()
    for service, observed in resolvers.items():
        host.require(type(observed) is dict and set(observed) == {'netns', 'sockets', 'resolv_sha256', 'nat_sha256'} and
                     type(observed['netns']) is dict and set(observed['netns']) == {'dev', 'ino'} and
                     type(observed['sockets']) is dict and set(observed['sockets']) == {'tcp', 'udp'} and
                     all(type(observed[key]) is str and HEX.fullmatch(observed[key]) is not None
                         for key in ('resolv_sha256', 'nat_sha256')), 'DNS resolver identity malformed')
        namespaces.add((observed['netns']['dev'], observed['netns']['ino']))
        for endpoint in observed['sockets'].values():
            host.require(type(endpoint) is dict and set(endpoint) == {'address', 'port', 'inode'} and
                         endpoint['address'] == '127.0.0.11' and type(endpoint['port']) is int and
                         0 < endpoint['port'] < 65536 and type(endpoint['inode']) is int and endpoint['inode'] > 0,
                         'DNS listener was not bound to an exact socket')
            inodes.add(endpoint['inode'])
        registrations = [history[name]['fields'] for name in names if history[name]['kind'] == 'registration' and
                         history[name]['fields'].get('name') == 'resolver-' + service]
        host.require(len(registrations) == 1 and registrations[0]['identity_digest'] == _sha(
            {'name': service, 'observation': observed}), 'DNS endpoint was not registered in the root journal')
    host.require(len(namespaces) == 4 and len(inodes) == 8, 'container DNS namespaces or sockets overlap')
    final_resolvers = result.get('resolver_inventory_final')
    transition = result.get('resolver_transition')
    host.require(type(final_resolvers) is dict and set(final_resolvers) == set(resolvers) and
                 all(final_resolvers[name] == resolvers[name] for name in ('w1', 'w2', 'control')),
                 'ledger DNS resolver identity changed')
    if result.get('source') is None:
        host.require(transition is None and final_resolvers == resolvers, 'unexplained resolver transition')
    else:
        keys = {'recovery_id', 'app_container_id', 'coordinator', 'network_id', 'inventory_sha256',
                'network_receipt_sha256', 'inventory_raw', 'network_receipt_raw', 'original_anchor_sha256',
                'app_host', 'network_ids', 'previous_resolver', 'current_resolver', 'resolver_bind_count'}
        host.require(type(transition) is dict and set(transition) == keys and
                     transition['previous_resolver'] == resolvers['app'] and
                     transition['current_resolver'] == final_resolvers['app'] and
                     transition['resolver_bind_count'] in (0, 2) and
                     transition['previous_resolver']['netns'] == transition['current_resolver']['netns'] and
                     (transition['previous_resolver']['sockets'] != transition['current_resolver']['sockets']) ==
                     (transition['resolver_bind_count'] == 2) and
                     type(transition['recovery_id']) is str and re.fullmatch(r'[a-z0-9_-]{1,128}', transition['recovery_id']) is not None and
                     type(transition['network_ids']) is dict and
                     transition['network_ids'].get('houfeng-source-' + transition['recovery_id']) == transition['network_id'] and
                     type(transition['app_host']) is str, 'APP resolver transition is not bound to its fence network')
        current = transition['current_resolver']
        host.require(type(current) is dict and set(current) == {'netns', 'sockets', 'resolv_sha256', 'nat_sha256'} and
                     type(current['sockets']) is dict and set(current['sockets']) == {'tcp', 'udp'} and
                     all(type(current[key]) is str and HEX.fullmatch(current[key]) is not None
                         for key in ('resolv_sha256', 'nat_sha256')) and all(
                         type(endpoint) is dict and set(endpoint) == {'address', 'port', 'inode'} and
                         endpoint['address'] == '127.0.0.11' and type(endpoint['port']) is int and
                         0 < endpoint['port'] < 65536 and type(endpoint['inode']) is int and endpoint['inode'] > 0
                         for endpoint in current['sockets'].values()) and
                     len({endpoint['inode'] for endpoint in current['sockets'].values()}) == 2 and
                     not ({endpoint['inode'] for endpoint in current['sockets'].values()} &
                          {endpoint['inode'] for name in ('w1', 'w2', 'control')
                           for endpoint in resolvers[name]['sockets'].values()}),
                     'reconnected APP resolver socket identity invalid')
        raw_inventory, raw_network = transition['inventory_raw'].encode('utf-8'), transition['network_receipt_raw'].encode('utf-8')
        host.require(hashlib.sha256(raw_inventory).hexdigest() == transition['inventory_sha256'] and
                     hashlib.sha256(raw_network).hexdigest() == transition['network_receipt_sha256'],
                     'source fence receipt bytes changed')
        original, network = host.strict_json(raw_inventory), host.strict_json(raw_network)
        host.require(type(original) is dict and type(original.get('containers')) is list and len(original['containers']) == 1 and
                     original['containers'][0].get('id') == transition['app_container_id'] and
                     _sha(original['containers'][0]) == transition['original_anchor_sha256'] and
                     original.get('request', {}).get('RecoveryID') == transition['recovery_id'] and
                     original['request'].get('DatabaseContainerID') == transition['app_container_id'] and
                     network == {'version': 1, 'recovery_id': transition['recovery_id'], 'network_id': transition['network_id']},
                     'source fence receipts do not bind the original APP')
        events = [history[name] for name in names]
        admitted = [event for event in events if event['kind'] == 'admission' and
                    event['fields'].get('name') == 'source-network-transition']
        registered = [event for event in events if event['kind'] == 'registration' and
                      event['fields'].get('name') == 'source-network-transition']
        coordinator = transition['coordinator']
        launches = [event for event in events if event['kind'] == 'launch' and
                    event['fields'].get('name') == 'houfeng-record-platform-admin' and
                    event['fields'].get('pid') == coordinator.get('pid') and
                    event['fields'].get('starttime') == coordinator.get('starttime') and
                    event['fields'].get('executable_sha256') == record['seed']['houfeng-record-platform-admin']]
        action = {'recovery_id': transition['recovery_id'], 'app_container_id': transition['app_container_id'],
                  'original_anchor_sha256': transition['original_anchor_sha256']}
        host.require(len(admitted) == len(registered) == len(launches) == 1 and
                     admitted[0]['fields']['action_digest'] == _sha(action) and
                     registered[0]['fields']['identity_digest'] == _sha(transition) and
                     admitted[0]['sequence'] < launches[0]['sequence'] < registered[0]['sequence'],
                     'APP network transition was not admitted before the fixed coordinator launch')
    if require_valid:
        host.require(facts.get('roots_digest') == binding.get('roots_digest'), 'root inventory binding changed')
        if invalid:
            host.require(allow_retired_maintenance, 'root realm became invalid')
            events = [history[name] for name in names]
            invalidations = [event for event in events if event['kind'] == 'invalid']
            host.require(len(invalidations) == 1 and invalidations[0]['fields'] == {'reason': 'direct_launch_failed'},
                         'unrelated realm invalidation cannot authorize maintenance continuation')
            cut = invalidations[0]['sequence']
            host.require(not any(event['kind'] == 'observation' and
                                 (event['fields']['valid'] is False or event['sequence'] > cut) for event in events) and
                         any(event['kind'] == 'phase_enter' and event['fields']['phase'] == 'c81_migration' and
                             event['sequence'] < cut for event in events),
                         'invalid no-copy realm was used or predates migration')
            continuations = []
            for index, scope in enumerate(scopes[:-1]):
                if scope['enrollment']['command'] != 'legacy-c81-migrate':
                    continue
                enrollment = [event['sequence'] for event in events if event['kind'] == 'registration' and
                              event['fields'] == {'name': 'c81-scope-enrolled', 'identity_digest': _sha(scope['enrollment'])}]
                retirement = [event['sequence'] for event in events if event['kind'] == 'registration' and
                              event['fields'] == {'name': 'c81-scope-retired', 'identity_digest': _sha(scope['retirement'])}]
                replacement = [event['sequence'] for event in events if event['kind'] == 'registration' and
                               event['fields'] == {'name': 'c81-scope-enrolled', 'identity_digest': _sha(scopes[index + 1]['enrollment'])}]
                if (len(enrollment) == len(retirement) == len(replacement) == 1 and
                        enrollment[0] < cut < retirement[0] < replacement[0] and
                        scopes[index + 1]['enrollment']['command'] == 'legacy-c81-migrate'):
                    continuations.append(index)
            host.require(len(continuations) == 1 and scopes[-1]['retirement']['status'] == 0 and
                         bool(scopes[-1]['retirement']['terminal_acks']),
                         'invalid realm lacks exact retired migration and completed replacement')
            permitted = {(scope['enrollment']['coordinator']['pid'], scope['enrollment']['coordinator']['starttime'])
                         for scope in scopes[continuations[0] + 1:]}
            host.require(all(event['fields']['name'] == 'houfeng-record-platform-admin' and
                             (event['fields']['pid'], event['fields']['starttime']) in permitted
                             for event in events if event['kind'] == 'launch' and event['sequence'] > cut),
                         'invalid realm launched a participant outside fixed maintenance continuation')
    return invalid


def _effect_records(host, result, raw_files, joined, generation):
    host.require(type(raw_files) is dict and type(joined.get('effect_files')) is dict and
                 set(raw_files) == set(joined['effect_files']) and len(raw_files) <= 7,
                 'original callback effect inventory differs')
    records = {}
    for name, raw in raw_files.items():
        host.require(type(name) is str and re.fullmatch(
            r'predecessor-(ready|create-intent|create-result|reserve-intent|reserve-result|stdout|result-written)-%020d\.json' % generation,
            name) is not None and type(raw) is str, 'callback effect name is not admitted')
        body = raw.encode('utf-8')
        host.require(0 < len(body) <= (32768 if name.startswith('predecessor-stdout-') else 16384) and
                     hashlib.sha256(body).hexdigest() == joined['effect_files'][name],
                     'callback witnessed effect bytes changed')
        records[name.removeprefix('predecessor-').removesuffix('-%020d.json' % generation)] = host.strict_json(body)
    ready = records.get('ready')
    binding_keys = {'version', 'phase_generation', 'recovery_id', 'application', 'realm_digest', 'backend'}
    host.require(type(ready) is dict and set(ready) == binding_keys | {'child'} and ready['version'] == 1 and
                 ready['phase_generation'] == generation and ready['application'] == result['realm']['app_binding']['application'] and
                 ready['realm_digest'] == result['realm']['realm_digest'] and ready['recovery_id'] == joined['recovery_id'] and
                 ready['child'] == joined['child'] and type(ready['backend']) is dict,
                 'ready record differs from the original callback')
    backend = ready['backend']
    try:
        same_backend = (set(backend) == {'pid', 'started', 'role'} and backend['pid'] == joined['backend']['pid'] and
                        backend['role'] == joined['backend']['role'] and _started_us(backend['started']) == joined['backend']['started_us'])
    except (KeyError, TypeError, ValueError, OverflowError):
        same_backend = False
    host.require(same_backend, 'ready record backend differs from held session')
    binding = {key: ready[key] for key in binding_keys}
    kinds = {'create-intent': {'command', 'command_digest'}, 'create-result': {'command_digest', 'reservation_id', 'expires_at'},
             'reserve-intent': {'command', 'command_digest'}, 'reserve-result': {'create_command_digest', 'command_digest',
                                                                                  'reservation_id', 'operation_id'}}
    for kind, extra in kinds.items():
        item = records.get(kind)
        if item is None:
            continue
        host.require(type(item) is dict and set(item) == binding_keys | extra and
                     all(item[key] == value for key, value in binding.items()), 'callback effect binding differs')
        if kind.endswith('intent'):
            host.require(type(item['command']) is dict and item['command_digest'] == _sha(item['command']),
                         'callback command digest differs')
    create, preview, reserve, committed = (records.get(kind) for kind in kinds)
    if create is not None:
        command = create['command']
        digests = {'actor_scope_digest', 'token_commitment', 'request_fingerprint', 'binding_digest', 'dependency_digest',
                   'backup_digest', 'processor_digest', 'adapter_readiness_digest', 'adapter_preview_digest', 'witness_hash'}
        numbers = {'lock_version', 'authorization_epoch', 'content_delivery_epoch', 'witness_sequence', 'ttl_microseconds'}
        host.require(set(command) == digests | numbers | {'project_id', 'object_kind', 'object_id', 'current_revision_id'} and
                     all(type(command[key]) is str and HEX.fullmatch(command[key]) is not None and command[key] != '0' * 64 for key in digests) and
                     all(type(command[key]) is int and 0 <= command[key] < (1 << 63) for key in numbers) and
                     command['project_id'] == 'default' and command['object_kind'] == 'record' and
                     command['object_id'] == joined['record_id'] and type(command['current_revision_id']) is str and
                     re.fullmatch(r'rrv_[a-z0-9]{1,64}', command['current_revision_id']) is not None and
                     command['lock_version'] > 0 and command['authorization_epoch'] > 0 and command['witness_sequence'] > 0 and
                     command['ttl_microseconds'] == 600000000, 'create command shape or record binding differs')
    if preview is not None:
        host.require(create is not None and preview['command_digest'] == create['command_digest'], 'preview command differs')
    if reserve is not None:
        reserve_command = reserve['command']
        digests = {'create_command_digest', 'request_fingerprint', 'expected_binding_digest', 'observed_witness_hash'}
        numbers = {'deletion_contract_version', 'observed_witness_sequence', 'owner_lease_microseconds'}
        host.require(set(reserve_command) == digests | numbers | {'reservation_id', 'deployment_id', 'actor_id', 'owner_id', 'reason_code'} and
                     all(type(reserve_command[key]) is str and HEX.fullmatch(reserve_command[key]) is not None and
                         reserve_command[key] != '0' * 64 for key in digests) and
                     all(type(reserve_command[key]) is int and 0 <= reserve_command[key] < (1 << 63) for key in numbers) and
                     reserve_command['deployment_id'] == result['realm']['app_binding']['deployment_id'] and
                     reserve_command['actor_id'] == joined['actor_user_id'] and reserve_command['deletion_contract_version'] == 1 and
                     reserve_command['observed_witness_sequence'] > 0 and reserve_command['owner_id'] == 'c80_predecessor' and
                     reserve_command['owner_lease_microseconds'] == 30000000 and reserve_command['reason_code'] == 'user_confirmed',
                     'reserve command shape or actor binding differs')
        host.require(preview is not None and reserve['command'].get('create_command_digest') == create['command_digest'] and
                     reserve['command'].get('reservation_id') == preview['reservation_id'] and
                     reserve['command'].get('request_fingerprint') == create['command'].get('request_fingerprint') and
                     reserve['command'].get('expected_binding_digest') == create['command'].get('binding_digest'),
                     'reserve command does not bind the original preview')
    if committed is not None:
        host.require(reserve is not None and committed['create_command_digest'] == create['command_digest'] and
                     committed['command_digest'] == reserve['command_digest'] and
                     committed['reservation_id'] == preview['reservation_id'], 'reserve result binding differs')
    carrier, written = records.get('stdout'), records.get('result-written')
    written_binding = {key: value for key, value in binding.items() if key != 'backend'}
    host.require(written is None or carrier is not None, 'written result lacks its original stdout carrier')
    if carrier is not None:
        host.require(type(carrier) is dict and set(carrier) == set(written_binding) | {'result_digest', 'stdout_bytes'} and
                     all(carrier[key] == value for key, value in written_binding.items()) and
                     type(carrier['result_digest']) is str and HEX.fullmatch(carrier['result_digest']) is not None and
                     type(carrier['stdout_bytes']) is str, 'original stdout carrier binding differs')
        try:
            encoded = carrier['stdout_bytes'].encode('ascii', 'strict')
            output_bytes = base64.b64decode(encoded, validate=True)
        except (ValueError, UnicodeError, binascii.Error) as exc:
            raise host.Failure('original stdout base64 is invalid') from exc
        host.require(base64.b64encode(output_bytes) == encoded and 0 < len(output_bytes) <= 16384 and
                     output_bytes.endswith(b'\n') and output_bytes.count(b'\n') == 1 and
                     hashlib.sha256(output_bytes).hexdigest() == carrier['result_digest'],
                     'original stdout bytes or digest differ')
        output = host.strict_json(output_bytes)
        host.require(type(output) is dict and set(output) == {'version', 'phase_generation', 'status', 'operation_id', 'reason'} and
                     type(output['version']) is int and output['version'] == 1 and
                     type(output['phase_generation']) is int and output['phase_generation'] == generation and
                     type(output['operation_id']) is str and type(output['reason']) is str,
                     'original stdout frame shape differs')
        if output['status'] == 'committed':
            host.require(output['reason'] == 'app_reserved' and committed is not None and
                         output['operation_id'] == committed['operation_id'], 'stdout operation differs')
        elif output['status'] == 'rejected':
            host.require(output['reason'] == 'source_denied' and output['operation_id'] == '' and create is None,
                         'stdout source rejection differs')
        else:
            host.require(output['status'] == 'unavailable' and output['operation_id'] == '' and output['reason'] in
                         ('inventory_unavailable', 'admission_unavailable', 'service_unavailable'), 'stdout unavailable result differs')
        if written is not None:
            host.require(type(written) is dict and set(written) == set(written_binding) | {'result_digest', 'result'} and
                         all(written[key] == value for key, value in written_binding.items()) and
                         written['result_digest'] == carrier['result_digest'] and written['result'] == output,
                         'written receipt does not bind actual stdout bytes')
    return records


def _callback(host, result, evidence, record_name, expected_status):
    host.require(type(evidence) is dict and set(evidence) == {'receipt_name', 'receipt_sha256', 'receipt_raw',
                 'root_pidfd_exited', 'root_join_name', 'root_join_sha256', 'effect_raw', 'trace'} and
                 type(evidence['receipt_raw']) is str and evidence['root_pidfd_exited'] is True, 'callback evidence missing')
    raw = evidence['receipt_raw'].encode('utf-8')
    host.require(len(raw) <= 16384 and hashlib.sha256(raw).hexdigest() == evidence['receipt_sha256'] and
                 host.strict_json(raw) == evidence['trace'], 'callback persisted bytes do not match the trace')
    trace = evidence['trace']
    fixture, binding = result.get('fixture'), result['realm']['app_binding']
    host.require(type(trace) is dict and type(fixture) is dict and type(fixture.get('records')) is dict and
                 trace.get('application') == binding['application'] and trace.get('actor_user_id') == fixture['actor_user_id'] and
                 trace.get('record_id') == fixture['records'][record_name]['record_id'] and
                 type(trace.get('phase_generation')) is int and
                 evidence['receipt_name'] == 'predecessor-callback-result-%020d.json' % trace['phase_generation'],
                 'callback does not bind the original APP, actor and record')
    child, coordinator, exit_info = trace.get('child'), trace.get('coordinator'), trace.get('exit')
    host.require(type(child) is dict and set(child) == {'pid', 'starttime', 'executable_sha256'} and
                 child['executable_sha256'] == result['genesis']['programs']['c80-predecessor'] and type(coordinator) is dict and
                 exit_info == {'pidfd_exited': True, 'wait_status': 0} and
                 trace.get('child_result', {}).get('status') == expected_status and
                 trace['child_result'].get('phase_generation') == trace['phase_generation'],
                 'callback child outcome or wait status differs')
    generation = trace['phase_generation']
    live_name, join_name = 'callback-live-%020d.json' % generation, 'callback-join-%020d.json' % generation
    live, joined = result['root_journal'].get(live_name), result['root_journal'].get(join_name)
    backend = trace.get('backend')
    try:
        started_us = _started_us(backend['started'])
    except (KeyError, TypeError, ValueError, OverflowError) as exc:
        raise host.Failure('callback backend timestamp differs') from exc
    host.require(evidence['root_join_name'] == join_name and type(live) is dict and type(joined) is dict and
                 evidence['root_join_sha256'] == _sha(joined) and live.get('child') == joined.get('child') == child and
                 live.get('coordinator') == joined.get('original_coordinator') == coordinator and
                 live.get('intent_sha256') == joined.get('intent_sha256') and
                 type(live.get('backend')) is dict and live.get('backend') == joined.get('backend') and
                 live['backend'].get('pid') == backend.get('pid') and
                 live['backend'].get('role') == backend.get('role') and live['backend'].get('started_us') == started_us and
                 joined.get('coordinator_direct_wait_status') == 0 and joined.get('coordinator_pidfd_exited') is True and
                 joined.get('child_pidfd_exited') is True and joined.get('child_direct_wait_by_root') is False and
                 joined.get('child_cgroup_empty') is True and joined.get('backend_settled') is True and
                 joined.get('prepared_transactions') == 0,
                 'root did not retain and join the actual ready child and backend')
    effects = _effect_records(host, result, evidence['effect_raw'], joined, generation)
    expected_effects = ({'ready', 'stdout', 'result-written'} if expected_status == 'rejected' else
                        {'ready', 'create-intent', 'create-result', 'reserve-intent', 'reserve-result', 'stdout', 'result-written'})
    host.require(set(effects) == expected_effects and effects['result-written']['result'] == trace['child_result'],
                 'normal callback durable effects are incomplete')
    journal_events = [value for name, value in result['root_journal'].items() if name.startswith('journal-')]
    live_events = [event for event in journal_events if event['kind'] == 'registration' and
                   event['fields'].get('name') == 'callback-live-%020d' % generation and
                   event['fields'].get('identity_digest') == _sha(live)]
    join_events = [event for event in journal_events if event['kind'] == 'registration' and
                   event['fields'].get('name') == 'callback-join-%020d' % generation and
                   event['fields'].get('identity_digest') == _sha(joined)]
    host.require(len(live_events) == len(join_events) == 1 and live_events[0]['sequence'] < join_events[0]['sequence'],
                 'root callback capture and durable join are not ordered')
    child_exits = [event['fields'] for event in journal_events if event['kind'] == 'exit' and
                   event['fields'].get('name') == 'c80-predecessor' and event['fields'].get('pid') == child.get('pid') and
                   event['fields'].get('starttime') == child.get('starttime')]
    coordinator_exits = [event['fields'] for event in journal_events if event['kind'] == 'exit' and
                         event['fields'].get('name') == 'houfeng-record-platform-admin' and
                         event['fields'].get('pid') == coordinator.get('pid') and
                         event['fields'].get('starttime') == coordinator.get('starttime')]
    host.require(len(child_exits) == len(coordinator_exits) == 1 and child_exits[0]['pidfd_exited'] is True and
                 coordinator_exits[0]['pidfd_exited'] is True and child_exits[0]['status'] == 0 and
                 coordinator_exits[0]['status'] == 0, 'held process exits were not independently recorded')
    events = trace.get('events')
    host.require(type(events) is list and events and all(type(item) is dict and type(item.get('sequence')) is int and
                 item['sequence'] == index for index, item in enumerate(events, 1)), 'callback event sequence invalid')
    source_checks = [item for item in events if item.get('op') == 'source_check']
    allowed = {'ready', 'inventory_observe', 'witness_head', 'witness_extension', 'preview_intent', 'preview_result',
               'reserve_intent', 'reserve_result', 'result_written'}
    replayable = {'preview_intent', 'preview_result', 'reserve_intent', 'reserve_result', 'result_written'}
    seen = set()
    for event in events:
        op, replay = event.get('op'), event.get('replay', False)
        if op == 'source_check':
            host.require(set(event) == {'sequence', 'op', 'source', 'allowed'} and type(event['allowed']) is bool,
                         'source check event differs')
        else:
            host.require(op in allowed and type(replay) is bool and
                         set(event) == ({'sequence', 'op', 'replay'} if replay else {'sequence', 'op'}) and
                         (not replay or op in replayable and op in seen), 'callback replay event differs')
            seen.add(op)
    counts = {name: sum(item.get('op') == name and not item.get('replay', False) for item in events)
              for name in ('ready', 'preview_intent', 'preview_result', 'reserve_intent', 'reserve_result', 'result_written')}
    if expected_status == 'rejected':
        source = result['source']['retirement']['source']
        host.require(trace['child_result'].get('reason') == 'source_denied' and
                     trace['child_result'].get('operation_id') == '' and
                     any(item.get('source') == source and item.get('allowed') is False for item in source_checks) and
                     counts == {'ready': 1, 'preview_intent': 0, 'preview_result': 0, 'reserve_intent': 0,
                                'reserve_result': 0, 'result_written': 1}, 'actual source-denied Preview is not evidenced')
    else:
        host.require(expected_status == 'committed' and trace['child_result'].get('reason') == 'app_reserved' and
                     source_checks and all(item.get('allowed') is True for item in source_checks) and
                     counts == {'ready': 1, 'preview_intent': 1, 'preview_result': 1, 'reserve_intent': 1,
                                'reserve_result': 1, 'result_written': 1}, 'actual Preview and Reserve are not evidenced')
    return trace


def _observer_capabilities(host, value):
    modes = ('single', 'two-thread')
    scenarios = ('normal', 'child-kill', 'coordinator-kill', 'signal', 'group-stop',
                 'already-traced', 'cancel', 'deadline', 'observer-loss')
    host.require(type(value) is dict and set(value) == {'observations', 'maintenance_smoke', 'systemd'},
                 'observer capability evidence schema differs')
    _systemd_evidence(host, value['systemd'])
    observations = value['observations']
    host.require(value['maintenance_smoke'] == ['maintenance-protocol', 'maintenance-records', 'maintenance-witness'] and
                 type(observations) is list and len(observations) == len(modes) * len(scenarios) and
                 all(type(item) is dict for item in observations) and
                 {(item.get('mode'), item.get('scenario')) for item in observations} ==
                 {(mode, scenario) for mode in modes for scenario in scenarios}, 'observer capability matrix differs')
    def identity(item):
        return (type(item) is dict and set(item) == {'pid', 'starttime'} and
                type(item['pid']) is int and item['pid'] > 0 and type(item['starttime']) is int and item['starttime'] > 0)
    for item in observations:
        mode, scenario = item['mode'], item['scenario']
        host.require(item.get('version') == 1 and item.get('guard_acquired_after_exit') is True and
                     item.get('child_direct_wait_by_root') is False, 'probe did not settle its owned guard')
        if scenario == 'observer-loss':
            attachment = item.get('attachment')
            host.require(set(item) == {'version', 'mode', 'scenario', 'attachment', 'supervisor_direct_wait',
                         'supervisor_wait_status', 'coordinator_pidfd_exited', 'child_direct_wait_by_coordinator',
                         'child_wait_status', 'child_direct_wait_by_root', 'guard_acquired_after_exit'} and
                         item['supervisor_direct_wait'] is True and item['coordinator_pidfd_exited'] is True and
                         item['child_direct_wait_by_coordinator'] is True and
                         type(item['supervisor_wait_status']) is int and os.WIFSIGNALED(item['supervisor_wait_status']) and
                         os.WTERMSIG(item['supervisor_wait_status']) == signal.SIGKILL and
                         type(item['child_wait_status']) is int and os.WIFSIGNALED(item['child_wait_status']) and
                         os.WTERMSIG(item['child_wait_status']) == signal.SIGKILL and type(attachment) is dict and
                         set(attachment) == {'op', 'child', 'observer_root', 'observer_thread'} and attachment['op'] == 'attached' and
                         identity(attachment['observer_root']) and identity(attachment['observer_thread']) and
                         attachment['observer_root']['pid'] != attachment['observer_thread']['pid'],
                         'owned observer-loss and EXITKILL evidence differs')
            continue
        keys = {'version', 'mode', 'scenario', 'threads', 'exit_stop', 'child', 'coordinator', 'observer_root',
                'observer_thread', 'delivered_signals', 'group_stops', 'second_tracer_rejection', 'deadline_outcome',
                'coordinator_wait_status', 'child_wait_status', 'guard_acquired_after_exit',
                'coordinator_direct_wait', 'child_direct_wait_by_root', 'signal_eligibility', 'group_stop_evidence'}
        child = item.get('child')
        host.require(set(item) == keys and item['threads'] == (1 if mode == 'single' else 2) and
                     item['coordinator_direct_wait'] is True and identity(item['coordinator']) and
                     identity(item['observer_root']) and identity(item['observer_thread']) and
                     item['observer_root']['pid'] != item['observer_thread']['pid'] and type(child) is dict and
                     set(child) == {'pid', 'starttime', 'executable_sha256'} and type(child['pid']) is int and child['pid'] > 0 and
                     type(child['starttime']) is int and child['starttime'] > 0 and
                     type(child['executable_sha256']) is str and HEX.fullmatch(child['executable_sha256']) is not None and
                     type(item['delivered_signals']) is list and type(item['group_stops']) is list and
                     all(type(signum) is int for signum in item['delivered_signals'] + item['group_stops']) and
                     type(item['coordinator_wait_status']) is int, 'owned observer process identities differ')
        eligibility = item['signal_eligibility']
        host.require(type(eligibility) is dict and set(eligibility) == {'policy', 'leader', 'worker'} and
                     eligibility['policy'] == 'leader_only_SIGUSR1_SIGCONT', 'controlled signal eligibility is missing')
        def thread_signal_identity(value):
            return (type(value) is dict and set(value) == {'tid', 'starttime', 'sigblk'} and
                    type(value['tid']) is int and 0 < value['tid'] < (1 << 64) and
                    type(value['starttime']) is int and value['starttime'] > 0 and
                    type(value['sigblk']) is str and re.fullmatch(r'[0-9a-f]{16}', value['sigblk']) is not None)
        leader, worker = eligibility['leader'], eligibility['worker']
        mask = (1 << (signal.SIGUSR1 - 1)) | (1 << (signal.SIGCONT - 1))
        host.require(thread_signal_identity(leader) and leader['tid'] == child['pid'] and
                     leader['starttime'] == child['starttime'] and int(leader['sigblk'], 16) & mask == 0 and
                     ((worker is None) if mode == 'single' else
                      (thread_signal_identity(worker) and worker['tid'] != leader['tid'] and
                       int(worker['sigblk'], 16) & mask == mask)), 'controlled probe thread masks differ')
        status = item['coordinator_wait_status']
        if scenario == 'coordinator-kill':
            stop = item['exit_stop']
            keys = {'observer_root', 'observer_thread', 'child', 'phase_generation', 'callback_live_digest', 'event',
                    'exit_signal', 'leader_guard_device', 'leader_guard_inode', 'shared_file_table_verified',
                    'other_guard_holders', 'coordinator_pidfd_exited', 'guard_contended_at_exit_stop', 'tracee_resumed',
                    'tracer_terminal_wait', 'tracer_joined', 'child_pidfd_exited'}
            host.require(os.WIFSIGNALED(status) and os.WTERMSIG(status) == signal.SIGKILL and item['child_wait_status'] is None and
                         type(stop) is dict and set(stop) == keys and stop['observer_root'] == item['observer_root'] and
                         stop['observer_thread'] == item['observer_thread'] and stop['child'] == child and stop['phase_generation'] == 1 and
                         stop['callback_live_digest'] == hashlib.sha256(b'own-process-probe').hexdigest() and
                         stop['event'] == 'PTRACE_EVENT_EXIT' and stop['exit_signal'] == 'SIGKILL' and
                         type(stop['leader_guard_device']) is int and type(stop['leader_guard_inode']) is int and
                         stop['leader_guard_inode'] > 0 and stop['other_guard_holders'] == [] and all(stop[key] is True for key in
                         ('shared_file_table_verified', 'coordinator_pidfd_exited', 'guard_contended_at_exit_stop',
                          'tracee_resumed', 'tracer_terminal_wait', 'tracer_joined', 'child_pidfd_exited')),
                         'owned coordinator death lacks the actual predecessor exit stop')
        else:
            child_status = item['child_wait_status']
            host.require(item['exit_stop'] is None and os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0 and
                         type(child_status) is int and ((os.WIFSIGNALED(child_status) and
                         os.WTERMSIG(child_status) == signal.SIGKILL) if scenario in ('child-kill', 'cancel', 'deadline') else
                         (os.WIFEXITED(child_status) and os.WEXITSTATUS(child_status) == 0)),
                         'owned child or coordinator direct wait differs')
        if scenario == 'signal':
            host.require(signal.SIGUSR1 in item['delivered_signals'], 'ordinary signal-delivery stop is missing')
        if scenario == 'group-stop':
            host.require(signal.SIGSTOP in item['group_stops'] and signal.SIGCONT in item['delivered_signals'],
                         'group stop or SIGCONT forwarding is missing')
            group = item['group_stop_evidence']
            host.require(type(group) is dict and set(group) == {'parent_wait_status', 'challenges'} and
                         type(group['parent_wait_status']) is int and os.WIFSTOPPED(group['parent_wait_status']) and
                         os.WSTOPSIG(group['parent_wait_status']) == signal.SIGSTOP and
                         type(group['challenges']) is list and len(group['challenges']) == item['threads'],
                         'real parent group stop or thread challenge evidence differs')
            expected_threads = [('leader', leader)] + ([('worker', worker)] if worker is not None else [])
            seen = set()
            for observed, (role, thread) in zip(group['challenges'], expected_threads):
                host.require(type(observed) is dict and set(observed) == {'role', 'tid', 'challenge', 'response'} and
                             observed['role'] == role and type(observed['tid']) is int and observed['tid'] == thread['tid'] and
                             type(observed['challenge']) is str and HEX.fullmatch(observed['challenge']) is not None and
                             type(observed['response']) is str and re.fullmatch(r'[0-9a-f]{80}', observed['response']) is not None and
                             observed['challenge'] not in seen, 'post-stop thread challenge binding differs')
                seen.add(observed['challenge'])
                tid = thread['tid'].to_bytes(8, 'big')
                challenge = bytes.fromhex(observed['challenge'])
                expected = tid + hashlib.sha256(b'HOUFENG-C80-THREAD-RESUME-V1\0' + tid + challenge).digest()
                host.require(observed['response'] == expected.hex(), 'post-stop thread response differs')
        else:
            host.require(item['group_stop_evidence'] is None, 'unexpected group-stop evidence')
        host.require(item['second_tracer_rejection'] == ({'reason': 'already_traced', 'tracer_tid': item['observer_thread']['pid']}
                     if scenario == 'already-traced' else None) and
                     item['deadline_outcome'] == ('expired' if scenario == 'deadline' else None),
                     'tracer rejection or deadline outcome differs')


def _negative(host, scenario, result, record, *, reboot_result=None):
    host.require(type(record) is dict and
                 (type(reboot_result) is dict if (record.get('case'), scenario) == ('source-recovery', 'vm-kill')
                  else reboot_result is None) and 'reboot_result' not in record,
                 'unexpected or missing reboot result')
    value = result.get('negative')
    host.require(type(value) is dict, 'negative scenario observation missing')
    if scenario in ('bpf-authorization', 'bpf-broker-kill'):
        host.require('genesis' not in result and 'realm' not in result, 'BPF standalone probe must precede APP genesis')
        return
    if scenario == 'observer-capability':
        if record.get('bpf_variant') == 'fd-arm-wrong-case':
            host.require(value.get('version') == 1 and value.get('scenario') == 'observer-capability' and
                         type(value.get('observations')) is list and len(value['observations']) == 1 and
                         value['observations'][0].get('name') == 'fd-arm-wrong-case' and
                         value['observations'][0].get('control_error') == 'BPF_AUTH_DENIED' and
                         'genesis' not in result and 'realm' not in result,
                         'fd-arm-wrong-case observation invalid')
            return
        _observer_capabilities(host, value)
        host.require('genesis' not in result and 'realm' not in result, 'capability probe must precede APP genesis')
        return
    if scenario == 'genesis-missing':
        host.require(value == {'genesis_absent': True, 'realm_digest_unavailable': True} and
                     'genesis' not in result and 'realm' not in result, 'missing genesis did not fail closed')
        return
    if scenario == 'late-genesis':
        host.require(value == {'actual_initdb_completed': True, 'late_genesis_rejected': True,
                               'realm_digest_unavailable': True} and 'genesis' not in result and
                     any(item.get('name') == 'postgres-initialize' for item in result.get('steps', [])),
                     'late genesis did not fail after actual initialization')
        return
    invalid = _root_journal(host, result, record, scenario in ('forbidden-qmp', 'root-restart', 'root-kill',
                                                               'stale-rpc', 'intersecting-source-closure', 'vm-kill', 'coordinator-kill'))
    if scenario == 'app-rollback':
        prepared, closed, restored, rejected = (value.get(key) for key in ('prepared', 'closed', 'restored', 'rejected'))
        host.require(invalid and type(prepared) is dict and type(closed) is dict and type(restored) is dict and
                     type(rejected) is dict and set(value) == {'prepared', 'closed', 'restored', 'rejected'},
                     'real APP rollback evidence is incomplete')
        backup = prepared.get('backup', {})
        artifact = backup.get('producer', {}).get('artifact', {})
        restore = restored.get('restore', {})
        older, newer = restored.get('app_restored', {}), restored.get('app_newer', {})
        host.require(backup.get('mode') == 'published' and backup.get('sticky_invalid') is True and
                     artifact.get('path') == 'published/postgres_dump/db.v1' and
                     type(artifact.get('size')) is int and artifact['size'] > 0 and
                     type(artifact.get('sha256')) is str and HEX.fullmatch(artifact['sha256']) is not None and
                     backup.get('physical_files') == [artifact] and
                     restore.get('archive_sha256') == artifact['sha256'] and restore.get('archive_size') == artifact['size'] and
                     restore.get('archive_delivered') is True and restore.get('pidfd_exited') is True and restore.get('exec_exit_code') == 0 and
                     type(restore.get('pid')) is int and restore['pid'] > 0 and type(restore.get('starttime')) is int and restore['starttime'] > 0 and
                     older == restored.get('app_before') == prepared.get('app_before') and
                     older.get('source_binding_table') is False and newer.get('source_binding_table') is True and
                     restored.get('records_before') == restored.get('records_restored') == prepared.get('records_before') and
                     restored.get('external_before') == restored.get('external_after'),
                     'published APP bytes or real older-APP restore differ')
        host.require(closed.get('response') == {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'realm_unavailable'} and
                     closed.get('exit_status') == 1 and rejected.get('exit_status') == 1 and rejected.get('stdout_bytes') == 0 and
                     rejected.get('app_before') == rejected.get('app_after') == older and
                     rejected.get('records_before') == rejected.get('records_after') == restored['records_restored'] and
                     rejected.get('external_before') == rejected.get('external_after') == restored['external_after'],
                     'closed realm or restored APP was not rejected with its original state intact')
        events = [entry for name, entry in result['root_journal'].items() if name.startswith('journal-')]
        invalidations = [entry for entry in events if entry['kind'] == 'invalid' and entry['fields'].get('reason') == 'negative_app_rollback']
        completions = [entry for entry in events if entry['kind'] == 'registration' and
                       entry['fields'].get('name') == 'negative-pg-restore-complete' and
                       entry['fields'].get('identity_digest') == _sha(restore)]
        host.require(len(invalidations) == len(completions) == 1 and invalidations[0]['sequence'] < completions[0]['sequence'],
                     'backup invalidation and actual restore were not ordered')
        callbacks = result.get('callbacks')
        host.require(type(callbacks) is list and len(callbacks) == 2, 'original C80 callbacks are unavailable')
        _callback(host, result, callbacks[0], 'a', 'rejected')
        _callback(host, result, callbacks[1], 'b', 'committed')
        handoff = result.get('handoff')
        host.require(type(handoff) is dict and type(handoff.get('maintenance_selection')) is dict,
                     'rollback migration terminal selection missing')
        _maintenance_witness(host, result, handoff['maintenance_selection'])
        return
    if scenario in ('conflicting-tuple', 'missing-journal'):
        fault = value.get('fault')
        host.require(invalid and value.get('scenario') == scenario and type(fault) is dict and
                     fault.get('scenario') == 'reserve-intent-coordinator' and
                     value.get('response') == {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'service_unavailable'} and
                     value.get('exit_status') == 1 and value.get('app_before') == value.get('app_after') and
                     value.get('app_after', {}).get('b_operations') == [] and value.get('phase', {}).get('phase') == 'refencing' and
                     type(value.get('create_raw')) is str and type(value.get('reserve_raw')) is str,
                     'original interrupted callback was not rejected with its fence intact')
        _fault_evidence(host, result, 'reserve-intent-coordinator', fault, allow_unjoined=scenario == 'missing-journal')
        create_raw, reserve_raw = value['create_raw'].encode('utf-8'), value['reserve_raw'].encode('utf-8')
        host.require(0 < len(create_raw) <= 16384 and 0 < len(reserve_raw) <= 16384 and
                     hashlib.sha256(reserve_raw).hexdigest() == fault['evidence_digest'], 'original witnessed reserve bytes differ')
        create, reserve = host.strict_json(create_raw), host.strict_json(reserve_raw)
        host.require(type(create) is dict and type(reserve) is dict and type(create.get('command')) is dict and
                     type(reserve.get('command')) is dict and create.get('command_digest') == _sha(create['command']) and
                     reserve.get('command_digest') == _sha(reserve['command']) and
                     reserve['command'].get('create_command_digest') == create['command_digest'] and
                     create['command'].get('object_id') == result['fixture']['records']['b']['record_id'],
                     'negative original reservation commands differ')
        events = [entry for name, entry in result['root_journal'].items() if name.startswith('journal-')]
        action = {'scenario': scenario, 'phase_generation': fault['phase_generation'],
                  'reservation_id': reserve['command']['reservation_id'], 'receipt_sha256': fault['evidence_digest']}
        mutation = {'action': action, 'tuple_before': value.get('tuple_before'), 'tuple_after': value.get('tuple_after')}
        admissions = [entry for entry in events if entry['kind'] == 'admission' and
                      entry['fields'].get('name') == 'negative-' + scenario and entry['fields'].get('action_digest') == _sha(action)]
        registrations = [entry for entry in events if entry['kind'] == 'registration' and
                         entry['fields'].get('name') == 'negative-' + scenario and entry['fields'].get('identity_digest') == _sha(mutation)]
        host.require(len(admissions) == len(registrations) == 1 and admissions[0]['sequence'] < registrations[0]['sequence'],
                     'actual negative mutation was not admitted and recorded')
        if scenario == 'conflicting-tuple':
            changed = hashlib.sha256(b'g2-conflicting-reservation-fingerprint').hexdigest()
            host.require(value['tuple_before'] == {'reservation_id': reserve['command']['reservation_id'],
                         'fingerprint': create['command']['request_fingerprint']} and
                         changed != value['tuple_before']['fingerprint'] and
                         value['tuple_after'] == {'count': 1, 'rows': [{'reservation_id': reserve['command']['reservation_id'],
                             'fingerprint': changed, 'state': 'previewed', 'fence_epoch': 0}]} and
                         type(value.get('resolution_raw')) is str, 'real conflicting database tuple is missing')
            resolution = host.strict_json(value['resolution_raw'].encode('utf-8'))
            host.require(type(resolution) is dict and resolution.get('outcome') == 'unresolved' and
                         resolution.get('phase_generation') == fault['phase_generation'], 'conflicting tuple was not left unresolved')
            joined = result['root_journal']['callback-join-%020d.json' % fault['phase_generation']]
            effects = _effect_records(host, result, value['effect_raw'], joined, fault['phase_generation'])
            host.require(effects.get('create-intent') == create and effects.get('reserve-intent') == reserve,
                         'conflicting tuple did not retain original joined effects')
        else:
            host.require(value.get('tuple_before') is None and value.get('tuple_after') is None and
                         value.get('resolution_raw') is None and value.get('effect_raw') is None and
                         any(entry['kind'] == 'invalid' and entry['fields'].get('reason') == 'realm_observation' for entry in events),
                         'missing witnessed receipt did not invalidate recovery before resolution')
        return
    if scenario == 'unsettled-backend':
        fault, held = value.get('fault'), value.get('held_backend')
        sessions = value.get('sessions_before')
        host.require(invalid and type(fault) is dict and fault.get('scenario') == 'reserve-intent-coordinator' and
                     type(held) is dict and set(held) == {'pid', 'starttime', 'namespace_pid', 'namespace_pids',
                         'stopped_state', 'pidfd_live', 'backend_started', 'backend_role', 'container_id', 'executable_sha256'} and
                     type(held['pid']) is int and held['pid'] > 0 and type(held['starttime']) is int and held['starttime'] > 0 and
                     type(held['namespace_pids']) is list and held['namespace_pids'] and
                     all(type(pid) is int and pid > 0 for pid in held['namespace_pids']) and
                     held['namespace_pids'][0] == held['pid'] and held['namespace_pids'][-1] == held['namespace_pid'] and
                     held['stopped_state'] == 'T' and held['pidfd_live'] is True and
                     held['container_id'] == result['resolver_transition']['app_container_id'] and
                     type(held['executable_sha256']) is str and HEX.fullmatch(held['executable_sha256']) is not None and
                     type(sessions) is list and len(sessions) == 1 and sessions == value.get('sessions_after') and
                     sessions[0].get('pid') == held['namespace_pid'] == fault['backend']['pid'] and
                     sessions[0].get('role') == held['backend_role'] == fault['backend']['role'] and
                     sessions[0].get('started_us') == _started_us(held['backend_started']) == _started_us(fault['backend']['started']) and
                     value.get('backend_pidfd_exited_after_release') is True and
                     value.get('response') == {'version': 1, 'status': 'unavailable', 'operation_id': '', 'reason': 'service_unavailable'} and
                     value.get('exit_status') == 1 and value.get('phase', {}).get('phase') == 'refencing' and
                     value.get('app_before') == value.get('app_after') and value.get('app_after', {}).get('b_operations') == [],
                     'original backend was not held across the rejected recovery')
        _fault_evidence(host, result, 'reserve-intent-coordinator', fault, allow_unjoined=True)
        reserve_raw = value.get('reserve_raw')
        host.require(type(reserve_raw) is str and 0 < len(reserve_raw.encode('utf-8')) <= 16384 and
                     hashlib.sha256(reserve_raw.encode('utf-8')).hexdigest() == fault['evidence_digest'],
                     'held backend original reserve receipt differs')
        events = [entry for name, entry in result['root_journal'].items() if name.startswith('journal-')]
        admissions = [entry for entry in events if entry['kind'] == 'admission' and
                      entry['fields'].get('name') == 'negative-stop-original-backend' and entry['fields'].get('action_digest') == _sha(held)]
        registrations = [entry for entry in events if entry['kind'] == 'registration' and
                         entry['fields'].get('name') == 'negative-stop-original-backend' and entry['fields'].get('identity_digest') == _sha(held)]
        host.require(len(admissions) == len(registrations) == 1 and admissions[0]['sequence'] < registrations[0]['sequence'] and
                     any(entry['kind'] == 'invalid' and entry['fields'].get('reason') == 'negative_unsettled_backend' for entry in events),
                     'actual original backend hold was not recorded before closure')
        return
    if scenario == 'extra-browser':
        selection_info = record.get('browser_selection')
        selection = selection_info.get('selection') if type(selection_info) is dict else None
        action, live = value.get('action'), value.get('live')
        host.require(invalid and type(selection) is dict and type(action) is dict and type(live) is dict and
                     selection_info.get('selection_sha256') == hashlib.sha256(_canonical(selection) + b'\n').hexdigest() == record['seed']['browser-selection.json'] and
                     selection.get('image_archive_sha256') == record['seed']['browser-image.tar'] and
                     selection.get('program_sha256') == '670ba079b75107746ba41abad131180a31a7c7219aa1bd4061fb471f4535d541' and
                     action == {'scenario': 'extra-browser', 'image_id': selection.get('image_id'),
                                'program_sha256': selection['program_sha256'], 'source_content': False} and
                     live.get('image_id') == selection['image_id'] and live.get('executable_sha256') == selection['program_sha256'] and
                     type(live.get('pid')) is int and live['pid'] > 0 and type(live.get('starttime')) is int and live['starttime'] > 0 and
                     type(live.get('container_id')) is str and HEX.fullmatch(live['container_id']) is not None and
                     type(live.get('argv_sha256')) is str and HEX.fullmatch(live['argv_sha256']) is not None and
                     type(live.get('cgroup_sha256')) is str and HEX.fullmatch(live['cgroup_sha256']) is not None and
                     live.get('uid') == live.get('gid') == 10002 and live.get('no_new_privileges') is True and
                     live.get('capabilities_empty') is True and live.get('network_mode') == 'none' and
                     live.get('readonly_root') is True and live.get('no_app_mounts') is True and live.get('tmpfs_only') == '/tmp' and
                     live.get('pids_limit') == 128 and live.get('memory_bytes') == 768 * 1024 * 1024 and live.get('pidfd_live') is True and
                     value.get('pidfd_exited') is True and value.get('container_wait_status') == 137 and value.get('sticky_invalid') is True,
                     'real isolated browser execution or immutable image evidence is missing')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        admitted = [event for event in events if event['kind'] == 'admission' and
                    event['fields'].get('name') == 'negative-browser-probe' and event['fields'].get('action_digest') == _sha(action)]
        invalids = [event for event in events if event['kind'] == 'invalid' and event['fields'].get('reason') == 'negative_extra_browser']
        observed = [event for event in events if event['kind'] == 'registration' and
                    event['fields'].get('name') == 'negative-browser-probe' and event['fields'].get('identity_digest') == _sha(live)]
        host.require(len(admitted) == len(invalids) == len(observed) == 1 and
                     admitted[0]['sequence'] < invalids[0]['sequence'] < observed[0]['sequence'],
                     'browser was not observed after permanent realm invalidation')
        return
    if scenario in ('root-restart', 'root-kill'):
        replacement = value.get('replacement')
        source = result.get('source', {}).get('retirement', {})
        host.require(not invalid and value.get('supervisor_pid') == result['genesis']['supervisor_pid'] and
                     value.get('supervisor_starttime') == result['genesis']['supervisor_starttime'] and
                     value.get('kill_signal') == 'SIGKILL' and value.get('direct_wait_confirmed') is True and
                     type(replacement) is dict and set(replacement) == {'reissued', 'pid', 'starttime'} and
                     replacement.get('reissued') is False and type(replacement.get('pid')) is int and
                     type(replacement.get('starttime')) is int and
                     (replacement['pid'], replacement['starttime']) != (value['supervisor_pid'], value['supervisor_starttime']) and
                     HEX.fullmatch(value.get('source_intent_digest', '')) is not None and
                     value.get('original_binding_unchanged') is True and value.get('realm_reissued') is False and
                     value.get('source_handoff_pending') is True and source.get('state') == 'witnessed_handoff_pending' and
                     source.get('source_authorization_permanently_retired') is True,
                     'supervisor death did not preserve closed original APP')
        return
    if scenario == 'vm-kill':
        history, intent = result['root_journal'], result['root_journal'].get('reboot-intent.json')
        reboot = reboot_result
        host.require(not invalid and type(intent) is dict and value.get('prearmed') is True and
                     value.get('intent_sha256') == _sha(intent) and value.get('source_handoff_pending') is True and
                     value.get('realm_digest') == intent.get('realm_digest') == result['realm']['realm_digest'] and
                     value.get('original_boot_id') == intent.get('original_boot_id') == result['genesis']['boot_id'] and
                     value.get('supervisor_pid') == result['genesis']['supervisor_pid'] and
                     value.get('supervisor_starttime') == result['genesis']['supervisor_starttime'] and
                     value.get('source_operation_id') == intent.get('source_operation_id') ==
                     result.get('source', {}).get('retirement', {}).get('operation_id') and
                     result['source']['retirement'].get('state') == 'witnessed_handoff_pending' and
                     result['source']['retirement'].get('source_authorization_permanently_retired') is True,
                     'original VM did not durably prearm the pending source')
        journal_names = sorted(name for name in history if name.startswith('journal-'))
        last = history[journal_names[-1]]
        host.require(last['kind'] == 'registration' and last['fields'] ==
                     {'name': 'vm-kill-ready', 'identity_digest': _sha(intent)} and
                     record['attempt'].get('kill_signal') == 'SIGKILL via held pidfd' and
                     record['attempt'].get('returncode') == -9 and record['attempt'].get('pidfd_exited') is True and
                     record['attempt'].get('wait_observed') is True,
                     'host did not kill and directly join the prearmed original QEMU')
        host.require(type(reboot) is dict and reboot.get('version') == 1 and reboot.get('run_token') == RUN_TOKEN and
                     reboot.get('case') == 'source-recovery' and reboot.get('scenario') == 'vm-kill' and
                     reboot.get('completed') is True and reboot.get('product_gate') == 'NOT_READY' and
                     reboot.get('permanent_deletion_http') == 'CLOSED' and 'failure' not in reboot,
                     'preinstalled reboot observer did not complete')
        _carrier_hardware(host, reboot, record)
        observation = reboot.get('reboot', {})
        readback = observation.get('observation') if type(observation) is dict else None
        host.require(type(observation) is dict and observation.get('version') == 1 and
                     observation.get('boot_id') != observation.get('original_boot_id') == intent['original_boot_id'] and
                     type(observation.get('boot_id')) is str and len(observation['boot_id']) == 36 and
                     observation.get('intent_sha256') == _sha(intent) and
                     observation.get('root_journal_tail_sha256') == _sha(last) and
                     observation.get('realm_digest') == intent['realm_digest'] and type(readback) is dict and
                     readback.get('realm_available') is False and readback.get('no_copy_reissuable') is False and
                     readback.get('source_handoff_pending') is True and
                     readback.get('source_operation_id') == intent['source_operation_id'] and
                     readback.get('application') == intent['application'] and readback.get('projection') == intent['projection'] and
                     readback.get('identity') == {key: intent['projection'][key] for key in
                                                  ('domain', 'domain_count', 'deployment', 'live_system', 'live_oid', 'live_name')} and
                     readback.get('records') == intent['records'] and readback.get('control_files') == intent['control_files'] and
                     readback.get('system_identifiers') == {name: result['realm']['app_binding']['physical'][name]['system_identifier']
                                                            for name, _ in source_vm.DISKS},
                     'reboot did not retain the original closed APP and source responsibility')
        mounted = readback.get('mounted')
        host.require(type(mounted) is dict and set(mounted) == {name for name, _ in source_vm.DISKS} and all(
            mounted[name].get('serial') == serial and mounted[name].get('root_ino') == result['genesis']['volumes'][name]['mount_ino'] and
            mounted[name].get('pgdata_ino') == result['genesis']['volumes'][name]['pgdata_ino']
            for name, serial in source_vm.DISKS), 'reboot volume identity differs')
        reboot_attempt = record.get('reboot_attempt', {})
        host.require(reboot_attempt.get('exit_observed') is True and reboot_attempt.get('pidfd_held') is True and
                     reboot_attempt.get('wait_observed') is True and reboot_attempt.get('returncode') == 0 and
                     (reboot_attempt.get('pid'), reboot_attempt.get('starttime')) !=
                     (record['attempt'].get('pid'), record['attempt'].get('starttime')),
                     'second QEMU process was not independently held and joined')
        return
    if scenario == 'coordinator-kill':
        fault, predecessor, callbacks = value.get('fault'), value.get('predecessor'), value.get('callbacks')
        host.require(not invalid and type(fault) is dict and fault.get('scenario') == 'create-intent-coordinator' and
                     fault.get('victim') == 'coordinator' and fault.get('boundary') == 'create-intent' and
                     fault.get('ready_received') is True and fault.get('armed_received') is True and
                     fault.get('signal') == 'SIGKILL' and fault.get('victim_pidfd_exited') is True and
                     fault.get('child_pidfd_exited') is True and fault.get('guard_contended_before_signal') is True and
                     type(fault.get('exit_stop')) is dict and
                     result['root_journal'].get('fault-complete-create-intent-coordinator.json') == fault and
                     type(predecessor) is dict and type(callbacks) is list and len(callbacks) == 2 and
                     predecessor.get('service_results') == [
                         {'status': 'rejected', 'operation_id': '', 'reason': 'source_denied'},
                         {'status': 'committed', 'operation_id': predecessor.get('operation_id'), 'reason': 'app_reserved'}] and
                     type(predecessor.get('operation_id')) is str and predecessor['operation_id'] and
                     result.get('source', {}).get('retirement', {}).get('state') == 'witnessed_handoff_pending',
                     'real coordinator kill, guard continuity, or same-APP recovery is missing')
        _fault_evidence(host, result, 'create-intent-coordinator', fault)
        _callback(host, result, callbacks[0], 'a', 'rejected')
        committed = _callback(host, result, callbacks[1], 'b', 'committed')
        host.require(committed['child_result']['operation_id'] == predecessor['operation_id'],
                     'recovered reservation differs from the original APP operation')
        _callback_recovery(host, result, value.get('callback_recovery'), fault, predecessor, callbacks)
        return
    if scenario == 'extra-session':
        fault, before, after = value.get('fault'), value.get('app_before'), value.get('app_after')
        host.require(invalid and type(fault) is dict and fault.get('scenario') == 'extra-session' and
                     fault.get('boundary') == 'before-predecessor-admission' and fault.get('ready_received') is True and
                     fault.get('armed_received') is True and fault.get('release_sent') is True and
                     fault.get('guard_contended_before_injection') is True and fault.get('extra_backend_joined') is True and
                     type(fault.get('phase_generation')) is int and fault['phase_generation'] > 0 and
                     fault.get('receipt_name') == 'phase-%020d.json' % fault['phase_generation'] and
                     fault.get('source_operation_id') == result.get('source', {}).get('retirement', {}).get('operation_id') and
                     type(fault.get('phase_raw')) is str and
                     hashlib.sha256(fault['phase_raw'].encode('utf-8')).hexdigest() == fault.get('evidence_digest') and
                     value.get('response') == {'version': 1, 'status': 'unavailable', 'operation_id': '',
                                               'reason': 'unexpected_session'} and value.get('exit_status') == 1 and
                     value.get('quiescent_phase_unchanged') is True and value.get('durable_effects_unchanged') is True and
                     value.get('launch_authority_unchanged') is True and type(before) is dict and before == after and
                     before.get('a_reservations') == before.get('b_reservations') == 0 and
                     before.get('b_preview_rows') == [] and before.get('b_operations') == [],
                     'unexpected live backend was not rejected before callback')
        phase = host.strict_json(fault['phase_raw'].encode('utf-8'))
        backend = fault.get('extra_backend')
        host.require(type(phase) is dict and phase.get('phase') == 'quiescent' and
                     phase.get('generation') == fault['phase_generation'] and phase.get('recovery_id') == fault.get('recovery_id') and
                     phase.get('application') == result['realm']['app_binding']['application'] and
                     type(backend) is dict and type(backend.get('pid')) is int and backend['pid'] > 0 and
                     backend.get('role') == 'postgres' and backend.get('application_name') == 'g2_root_identity' and
                     backend.get('state') == 'idle' and type(backend.get('backend_start')) is str,
                     'extra backend or original quiescent phase identity differs')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        session_records = [event for event in events if event['kind'] == 'registration' and
                           event['fields'].get('name') == 'negative-extra-session' and
                           event['fields'].get('identity_digest') == _sha(backend)]
        invalids = [event for event in events if event['kind'] == 'invalid' and event['fields'].get('reason') == 'negative_extra_session']
        completions = [event for event in events if event['kind'] == 'registration' and
                       event['fields'].get('name') == 'fault-complete-extra-session' and
                       event['fields'].get('identity_digest') == _sha(fault)]
        host.require(len(session_records) == len(invalids) == len(completions) == 1 and
                     session_records[0]['sequence'] < invalids[0]['sequence'] < completions[0]['sequence'],
                     'extra backend was not independently registered and joined')
        return
    if scenario in ('staging-backup', 'published-backup', 'extra-backup-producer'):
        mode = 'published' if scenario == 'published-backup' else 'staging'
        dump = value.get('dump')
        host.require(invalid and value.get('mode') == mode and value.get('sticky_invalid') is True and
                     value.get('roots_digest_before') == result['realm']['app_binding']['roots_digest'] and
                     type(dump) is dict and type(dump.get('stream')) is dict and
                     dump['stream'].get('format') == 'postgres-custom' and type(dump['stream'].get('size')) is int and
                     0 < dump['stream']['size'] <= 64 * 1024 * 1024 and
                     HEX.fullmatch(dump['stream'].get('sha256', '')) is not None and
                     dump.get('pidfd_exited') is True and dump.get('exec_exit_code') == 0 and
                     dump.get('sealed') is True and dump.get('readonly') is True,
                     'actual sealed PostgreSQL dump evidence is missing')
        artifact = {'path': mode + '/postgres_dump/db.v1', 'size': dump['stream']['size'],
                    'sha256': dump['stream']['sha256']}
        directories = sorted(['staging', 'published', 'multipart', 'pins', 'staging/postgres_dump'] +
                             (['published/postgres_dump'] if mode == 'published' else []))
        producer = {'version': 1, 'status': 'staged' if mode == 'staging' else 'published', 'artifact': artifact}
        host.require(value.get('producer') == producer and value.get('physical_files') == [artifact] and
                     value.get('physical_directories') == directories and
                     type(value.get('producer_pid')) is int and value['producer_pid'] > 0 and
                     type(value.get('producer_starttime')) is int and value['producer_starttime'] > 0,
                     'pinned LocalStore artifact differs from independent physical census')
        steps = [step for step in result.get('steps', []) if step.get('name') == 'postgres-initialize']
        backend, lock = dump.get('backend'), dump.get('observation_lock_backend')
        host.require(len(steps) == 1 and dump.get('container_id') == steps[0]['processes']['app']['container_id'] and
                     type(dump.get('exec_id')) is str and HEX.fullmatch(dump['exec_id']) is not None and
                     type(dump.get('pid')) is int and dump['pid'] > 0 and type(dump.get('starttime')) is int and
                     dump['starttime'] > 0 and HEX.fullmatch(dump.get('executable_sha256', '')) is not None and
                     type(backend) is dict and backend.get('role') == 'postgres' and backend.get('database') == 'houfeng' and
                     backend.get('application_name') == 'pg_dump' and backend.get('state') == 'active' and
                     backend.get('wait_type') == 'Lock' and type(backend.get('pid')) is int and backend['pid'] > 0 and
                     type(backend.get('backend_start')) is str and type(lock) is dict and
                     lock.get('role') == 'postgres' and lock.get('state') == 'idle in transaction' and
                     type(lock.get('pid')) is int and lock['pid'] > 0 and lock['pid'] != backend['pid'],
                     'dump did not use the pinned APP container and observed PostgreSQL backend')
        if scenario == 'extra-backup-producer':
            host.require(dump.get('forbidden_exec') == {'operation': 'exec', 'pid': dump['pid'],
                                                         'exe': '/usr/lib/postgresql/16/bin/pg_dump'},
                         'kernel audit did not observe the actual forbidden backup producer')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        invalids = [event for event in events if event['kind'] == 'invalid' and event['fields'].get('reason') == 'negative_backup_producer']
        initial = {key: dump[key] for key in ('exec_id', 'container_id', 'pid', 'starttime', 'executable_sha256',
                                               'backend', 'observation_lock_backend')}
        registered = [event for event in events if event['kind'] == 'registration' and
                      event['fields'].get('name') == 'negative-pg-dump' and event['fields'].get('identity_digest') == _sha(initial)]
        completed = [event for event in events if event['kind'] == 'registration' and
                     event['fields'].get('name') == 'negative-pg-dump-complete' and event['fields'].get('identity_digest') == _sha(dump)]
        launches = [event for event in events if event['kind'] == 'launch' and event['fields'].get('name') == 'c80-backup-negative']
        exits = [event for event in events if event['kind'] == 'exit' and event['fields'].get('name') == 'c80-backup-negative']
        host.require(len(invalids) == len(registered) == len(completed) == len(launches) == len(exits) == 1 and
                     invalids[0]['sequence'] < registered[0]['sequence'] < completed[0]['sequence'] <
                     launches[0]['sequence'] < exits[0]['sequence'] and
                     launches[0]['fields'].get('executable_sha256') == record['seed']['c80-backup-negative'] and
                     launches[0]['fields'].get('pid') == value['producer_pid'] and
                     exits[0]['fields'].get('pid') == value['producer_pid'] and exits[0]['fields'].get('status') == 0 and
                     exits[0]['fields'].get('pidfd_exited') is True,
                     'dump and pinned producer are not ordered within the root journal')
        return
    if scenario == 'wrong-rpc':
        probe, outcomes = value.get('probe'), value.get('outcomes')
        expected = {'wrong-credentials', 'invalid-realm-response', 'duplicate', 'extra-field', 'duplicate-key',
                    'bool-version', 'bad-nonce', 'wrong-phase', 'extra-fd', 'truncated', 'cancelled-channel'}
        host.require(invalid and type(outcomes) is dict and set(outcomes) == expected and
                     all(item is True for item in outcomes.values()) and value.get('root_python_validators_only') is True and
                     value.get('direct_wait') is True and value.get('pidfd_exited') is True and
                     value.get('durable_effects_unchanged') is True and
                     value.get('source_operation_id') == result.get('source', {}).get('retirement', {}).get('operation_id') and
                     type(value.get('phase_generation')) is int and value['phase_generation'] > 0 and
                     type(value.get('app_before')) is dict and value['app_before'] == value.get('app_after') and
                     value['app_before'].get('a_reservations') == value['app_before'].get('b_reservations') == 0 and
                     value['app_before'].get('b_preview_rows') == value['app_before'].get('b_operations') == [],
                     'root RPC malformed-request probe did not preserve APP state')
        host.require(type(probe) is dict and probe.get('uid') == probe.get('gid') == 10002 and
                     probe.get('supplementary_groups') == [] and probe.get('no_new_privileges') is True and
                     type(probe.get('pid')) is int and probe['pid'] > 0 and type(probe.get('starttime')) is int and
                     probe['starttime'] > 0 and HEX.fullmatch(probe.get('executable_sha256', '')) is not None and
                     HEX.fullmatch(probe.get('cgroup_sha256', '')) is not None and
                     type(probe.get('channel_ino')) is int and probe['channel_ino'] > 0,
                     'root RPC probe identity is incomplete')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        invalids = [event for event in events if event['kind'] == 'invalid' and event['fields'].get('reason') == 'negative_wrong_rpc']
        registrations = [event for event in events if event['kind'] == 'registration' and
                         event['fields'].get('name') == 'negative-rpc-probe' and event['fields'].get('identity_digest') == _sha(probe)]
        completed = [event for event in events if event['kind'] == 'registration' and
                     event['fields'].get('name') == 'negative-rpc-probe-complete' and event['fields'].get('identity_digest') == _sha(value)]
        host.require(len(invalids) == len(registrations) == len(completed) == 1 and
                     invalids[0]['sequence'] < registrations[0]['sequence'] < completed[0]['sequence'],
                     'RPC probe was not bound to prior invalidation and process join')
        return
    if scenario == 'stale-rpc':
        request, before, after = value.get('request'), value.get('app_before'), value.get('app_after')
        host.require(type(request) is dict and type(request.get('coordinator_pid')) is int and
                     type(request.get('phase_generation')) is int and request.get('observation_sequence') == 1 and
                     HEX.fullmatch(request.get('request_nonce', '')) is not None and
                     HEX.fullmatch(request.get('response_nonce', '')) is not None and
                     request['request_nonce'] != request['response_nonce'] and value.get('exit_status') == 1 and
                     value.get('response') == {'version': 1, 'status': 'unavailable', 'operation_id': '',
                                               'reason': 'realm_unavailable'} and
                     value.get('quiescent_phase_unchanged') is True and value.get('durable_effects_unchanged') is True and
                     value.get('launch_authority_unchanged') is True and type(before) is dict and before == after and
                     before.get('a_reservations') == before.get('b_reservations') == 0 and
                     before.get('b_preview_rows') == [] and before.get('b_operations') == [] and
                     result.get('source', {}).get('retirement', {}).get('state') == 'witnessed_handoff_pending',
                     'stale live realm observation did not fail before callback')
        return
    if scenario == 'intersecting-source-closure':
        records = value.get('records')
        host.require(type(records) is dict and records == result.get('fixture', {}).get('records') and
                     set(records) == {'a', 'b'} and records['a']['source_kind'] == 'vps' and
                     records['b']['source_kind'] == 'monitoring_instance' and
                     records['b']['parent_vps_id'] == records['a']['source_id'] and
                     records['a']['closure'] == records['b']['closure'] and len(records['a']['closure']) == 2 and
                     {(item['kind'], item['id']) for item in records['a']['closure']} == {
                         ('vps', records['a']['source_id']), ('monitoring_instance', records['b']['source_id'])} and
                     value.get('service_result') == {'status': 'rejected', 'operation_id': '', 'reason': 'source_denied'} and
                     type(value.get('app_before')) is dict and value['app_before'] == value.get('app_after') and
                     value['app_before'].get('a_reservations') == value['app_before'].get('b_reservations') == 0 and
                     value['app_before'].get('b_preview_rows') == [] and value['app_before'].get('b_operations') == [],
                     'shared real source closure did not preserve the APP on rejection')
        _callback(host, result, value.get('callback'), 'b', 'rejected')
        return
    if scenario == 'other-record-comparison':
        producer, backend, readback = value.get('producer'), value.get('backend'), value.get('readback')
        fixture = result.get('fixture', {}).get('records', {})
        app = result['realm']['app_binding']['application']
        host.require(invalid and value.get('sticky_invalid') is True and value.get('keyring_removed') is True and
                     type(producer) is dict and type(producer.get('version')) is int and producer['version'] == 1 and
                     producer.get('status') == 'produced' and
                     producer.get('readback_verified') is True and type(fixture) is dict and set(fixture) == {'a', 'b'} and
                     producer.get('record_id') == fixture['b']['record_id'] and producer['record_id'] != fixture['a']['record_id'] and
                     producer.get('base_revision_id') == fixture['b']['revision_id'] and
                     len({producer.get('base_revision_id'), producer.get('compared_revision_id'), producer.get('revision_id')}) == 3 and
                     type(readback) is dict and readback.get('record_id') == producer['record_id'] and
                     readback.get('snapshot_id') == producer.get('snapshot_id') and readback.get('kind') == 'comparison.result' and
                     readback.get('schema_version') == 1 and readback.get('reference_count') == 1 and
                     readback.get('current_revision') == producer.get('revision_id') and
                     readback.get('target_revision') == fixture['a']['revision_id'] and
                     readback.get('base_exists') is True and readback.get('compared_exists') is True and
                     type(readback.get('payload_digest')) is str and HEX.fullmatch(readback['payload_digest']) is not None and
                     readback['payload_digest'] != '0' * 64 and
                     readback.get('payload_size', 0) > 0 and readback.get('compressed_size', 0) > 0 and
                     readback.get('compressed_bytes') == readback.get('compressed_size') and
                     readback.get('system_identifier') == app['SystemIdentifier'] and
                     readback.get('database_oid') == app['DatabaseOID'] and readback.get('other_sessions') == 0 and
                     type(backend) is dict and backend.get('role') == 'houfeng_runtime' and
                     backend.get('application_name') == 'g2-c80-comparison-negative' and backend.get('other_sessions') == 1 and
                     backend.get('system_identifier') == app['SystemIdentifier'] and backend.get('database_oid') == app['DatabaseOID'],
                     'actual other-record comparison or independent APP readback is missing')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        invalids = [item for item in events if item['kind'] == 'invalid' and item['fields'].get('reason') == 'negative_comparison_producer']
        launches = [item for item in events if item['kind'] == 'launch' and
                    item['fields'].get('name') == 'c80-comparison-negative' and item['fields'].get('pid') == value['helper_pid'] and
                    item['fields'].get('starttime') == value['helper_starttime'] and
                    item['fields'].get('executable_sha256') == record['seed']['c80-comparison-negative']]
        backends = [item for item in events if item['kind'] == 'registration' and
                    item['fields'].get('name') == 'comparison-negative-backend' and item['fields'].get('identity_digest') == _sha(backend)]
        exits = [item for item in events if item['kind'] == 'exit' and
                 item['fields'].get('name') == 'c80-comparison-negative' and item['fields'].get('pid') == value['helper_pid'] and
                 item['fields'].get('starttime') == value['helper_starttime'] and item['fields'].get('status') == 0 and
                 item['fields'].get('pidfd_exited') is True]
        host.require(len(invalids) == len(launches) == len(backends) == len(exits) == 1 and
                     invalids[0]['sequence'] < launches[0]['sequence'] < backends[0]['sequence'] < exits[0]['sequence'],
                     'negative producer was not invalidated, registered and joined in order')
        return
    if scenario in ('extra-http', 'extra-endpoint'):
        probe = value.get('probe')
        listener = probe.get('listener') if type(probe) is dict else None
        host.require(invalid and value.get('scenario') == scenario and value.get('live_listener_rejected') is True and
                     value.get('listener_removed') is True and value.get('direct_wait') is True and
                     value.get('pidfd_exited') is True and value.get('sticky_invalid') is True and
                     value.get('http_response_sha256') == hashlib.sha256(
                         b'HTTP/1.0 204 No Content\r\nContent-Length: 0\r\n\r\n').hexdigest() and
                     type(probe) is dict and probe.get('uid') == probe.get('gid') == 10002 and
                     type(probe.get('pid')) is int and probe['pid'] > 0 and type(probe.get('starttime')) is int and
                     probe['starttime'] > 0 and probe.get('executable_sha256') == result['genesis']['supervisor_executable_sha256'] and
                     HEX.fullmatch(probe.get('cgroup_sha256', '')) is not None and type(listener) is dict and
                     listener.get('version') == 1 and listener.get('address') == '127.0.0.1' and
                     type(listener.get('port')) is int and 0 < listener['port'] < 65536 and
                     type(listener.get('inode')) is int and listener['inode'] > 0,
                     'actual extra HTTP listener was not observed and rejected')
        events = [event for name, event in result['root_journal'].items() if name.startswith('journal-')]
        registrations = [event for event in events if event['kind'] == 'registration' and
                         event['fields'].get('name') == 'negative-listener-probe' and event['fields'].get('identity_digest') == _sha(probe)]
        invalids = [event for event in events if event['kind'] == 'invalid' and event['fields'].get('reason') == 'negative_unregistered_listener']
        completed = [event for event in events if event['kind'] == 'registration' and
                     event['fields'].get('name') == 'negative-listener-complete' and event['fields'].get('identity_digest') == _sha(value)]
        host.require(len(registrations) == len(invalids) == len(completed) == 1 and
                     registrations[0]['sequence'] < invalids[0]['sequence'] < completed[0]['sequence'],
                     'extra listener detection and process join are not ordered')
        return
    if scenario in ('directory-hidden', 'directory-symlink', 'directory-unreadable'):
        host.require(invalid and value == {'scenario': scenario,
                     'initial_roots_digest': result['realm']['app_binding']['roots_digest'],
                     'contamination_rejected': True, 'fixture_removed': True, 'sticky_invalid': True},
                     'actual directory contamination did not remain invalid after cleanup')
        return
    if scenario == 'directory-replacement':
        host.require(invalid and set(value) == {'original_ino', 'replacement_ino', 'replacement_rejected', 'sticky_invalid'} and
                     value['original_ino'] != value['replacement_ino'] and value['replacement_rejected'] is True and
                     value['sticky_invalid'] is True, 'directory replacement was not rejected permanently')
        return
    if scenario == 'forbidden-qmp':
        host.require(record.get('attempt', {}).get('forbidden_qmp_rejected_before_send') is True and
                     value.get('realm_remained_valid') is True and value.get('realm_digest') == result['realm']['realm_digest'],
                     'QMP command was not rejected before transmission')
        return
    host.require(False, 'required negative fixture evidence is missing')


def _anchor_fault_evidence(host, result, fault, evidence):
    host.require(type(evidence) is dict and set(evidence) == {'anchor_raw', 'previous_raw', 'preview_anchor_index'} and
                 type(evidence['preview_anchor_index']) is int and evidence['preview_anchor_index'] == 0 and
                 type(evidence['anchor_raw']) is str and type(evidence['previous_raw']) is str and
                 fault['receipt_name'] == 'anchor-%020d.json' % 1, 'source anchor evidence shape differs')
    def anchor(raw, index):
        body = raw.encode('ascii', 'strict')
        host.require(0 < len(body) <= 16384, 'source anchor bytes exceed their bound')
        item = host.strict_json(body)
        host.require(type(item) is dict and set(item) == {'version', 'index', 'previous', 'primary', 'witness'} and
                     type(item['version']) is int and item['version'] == 1 and
                     type(item['index']) is int and item['index'] == index, 'source anchor identity differs')
        def digest(value):
            host.require(type(value) is list and len(value) == 32 and
                         all(type(part) is int and 0 <= part <= 255 for part in value), 'source anchor digest is malformed')
            return bytes(value)
        previous = digest(item['previous'])
        heads = []
        for name in ('primary', 'witness'):
            value = item[name]
            host.require(type(value) is dict and set(value) == {'Sequence', 'Hash'} and
                         type(value['Sequence']) is int and 0 <= value['Sequence'] < (1 << 63),
                         'source anchor ledger head is malformed')
            head_hash = digest(value['Hash'])
            host.require((value['Sequence'] == 0) == (head_hash == bytes(32)), 'source anchor head position differs')
            heads.append((value['Sequence'], head_hash))
        encoded = {'version': item['version'], 'index': item['index'], 'previous': item['previous'],
                   'primary': {'Sequence': item['primary']['Sequence'], 'Hash': item['primary']['Hash']},
                   'witness': {'Sequence': item['witness']['Sequence'], 'Hash': item['witness']['Hash']}}
        host.require(body == json.dumps(encoded, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii'),
                     'source anchor bytes are not the fixed Go representation')
        return body, previous, heads
    previous_raw, previous_link, previous_heads = anchor(evidence['previous_raw'], 0)
    anchor_raw, anchor_link, heads = anchor(evidence['anchor_raw'], 1)
    retirement = result.get('source', {}).get('retirement', {})
    host.require(previous_link == bytes(32) and previous_heads == [(0, bytes(32)), (0, bytes(32))] and
                 anchor_link == hashlib.sha256(previous_raw).digest() and
                 hashlib.sha256(anchor_raw).hexdigest() == fault['evidence_digest'] and
                 heads[0] == heads[1] and heads[0][0] == 1 and type(retirement) is dict and
                 retirement.get('ledger_sequence') == heads[0][0] and retirement.get('ledger_entry_hash') == heads[0][1].hex(),
                 'source anchor lineage or W1/W2 retirement head differs')


def _fault_evidence(host, result, scenario, fault, allow_unjoined=False):
    history = result['root_journal']
    intent = history.get('fault-intent-' + scenario + '.json')
    complete = history.get('fault-complete-' + scenario + '.json')
    host.require(type(intent) is dict and type(fault) is dict and complete == fault and
                 intent.get('scenario') == fault.get('scenario') == scenario and
                 intent.get('boundary') == fault.get('boundary') and intent.get('victim') == fault.get('victim') and
                 intent.get('phase_generation') == fault.get('phase_generation') and
                 intent.get('operation_id') == fault.get('operation_id') and
                 intent.get('receipt_name') == fault.get('receipt_name') and
                 intent.get('evidence_digest') == fault.get('evidence_digest') and
                 intent.get('backend') == fault.get('backend') and fault.get('ready_received') is True and
                 fault.get('armed_received') is True and fault.get('signal') == 'SIGKILL' and
                 fault.get('victim_pidfd_exited') is True and fault.get('child_pidfd_exited') is True and
                 fault.get('guard_contended_before_signal') is True,
                 'fault intent and original participant exit evidence differ')
    intent_keys = {'version', 'run_token', 'scenario', 'boundary', 'victim', 'phase_generation', 'operation_id',
                   'coordinator', 'child', 'backend', 'receipt_name', 'evidence_digest', 'nonce_digest'}
    if fault['boundary'] == 'stdout-loss':
        intent_keys |= {'stdout_carrier_name', 'stdout_carrier_digest'}
    if fault['boundary'] == 'anchor-fsync':
        intent_keys.add('anchor_evidence')
    fault_keys = {'version', 'scenario', 'boundary', 'victim', 'phase_generation', 'operation_id', 'evidence_digest',
                  'receipt_name', 'ready_received', 'armed_received', 'signal', 'victim_pidfd_exited',
                  'child_pidfd_exited', 'guard_contended_before_signal', 'exit_stop', 'backend'}
    host.require(set(intent) == intent_keys and set(fault) == fault_keys and type(intent['version']) is int and
                 intent['version'] == fault['version'] == 1 and intent['run_token'] == RUN_TOKEN and
                 type(intent['phase_generation']) is int and intent['phase_generation'] > 0 and
                 all(type(intent[key]) is str and HEX.fullmatch(intent[key]) is not None
                     for key in ('evidence_digest', 'nonce_digest')), 'fault receipt schema differs')
    events = [value for name, value in history.items() if name.startswith('journal-')]
    initial = [event for event in events if event['kind'] == 'registration' and
               event['fields'].get('name') == 'fault-intent-' + scenario and event['fields'].get('identity_digest') == _sha(intent)]
    done = [event for event in events if event['kind'] == 'registration' and
            event['fields'].get('name') == 'fault-complete-' + scenario and event['fields'].get('identity_digest') == _sha(fault)]
    host.require(len(initial) == len(done) == 1 and initial[0]['sequence'] < done[0]['sequence'],
                 'fault intent and completion are not durably ordered')
    if fault['boundary'] == 'anchor-fsync':
        _anchor_fault_evidence(host, result, fault, intent['anchor_evidence'])
    if fault['boundary'] in CALLBACK_CUTS:
        generation = fault['phase_generation']
        receipt_kinds = {'create-intent': 'create-intent', 'create-commit-before-result': 'create-intent',
                         'preview-ack-loss': 'create-result', 'reserve-intent': 'reserve-intent',
                         'reserve-commit-before-stdout': 'reserve-result', 'stdout-loss': 'result-written'}
        host.require(fault['receipt_name'] == 'predecessor-%s-%020d.json' % (receipt_kinds[fault['boundary']], generation),
                     'fault does not name its exact durable receipt')
        live = history.get('callback-live-%020d.json' % generation)
        joined = history.get('callback-join-%020d.json' % generation)
        backend = fault.get('backend')
        try:
            backend_matches = (backend is None or type(backend) is dict and type(live) is dict and
                               type(live.get('backend')) is dict and backend.get('pid') == live['backend'].get('pid') and
                               backend.get('role') == live['backend'].get('role') and
                               _started_us(backend['started']) == live['backend'].get('started_us'))
        except (KeyError, TypeError, ValueError, OverflowError):
            backend_matches = False
        host.require(type(live) is dict and live.get('coordinator') == intent.get('coordinator') and
                     type(live.get('child')) is dict and live.get('child') == intent.get('child') and backend_matches,
                     'original callback live capture differs from the fault')
        live_events = [event for event in events if event['kind'] == 'registration' and
                       event['fields'].get('name') == 'callback-live-%020d' % generation and
                       event['fields'].get('identity_digest') == _sha(live)]
        host.require(len(live_events) == 1 and live_events[0]['sequence'] < initial[0]['sequence'],
                     'original callback capture did not precede the fault')
        if allow_unjoined:
            host.require(joined is None, 'missing original effect unexpectedly produced a durable join')
        else:
            host.require(type(joined) is dict and type(joined.get('effect_files')) is dict and
                         live.get('coordinator') == joined.get('original_coordinator') and
                         live.get('child') == joined.get('child') and live.get('backend') == joined.get('backend') and
                         live.get('intent_sha256') == joined.get('intent_sha256') and
                         joined['effect_files'].get(fault['receipt_name']) == fault['evidence_digest'] and
                         joined.get('coordinator_pidfd_exited') is True and type(joined.get('coordinator_direct_wait_status')) is int and
                         joined.get('child_pidfd_exited') is True and joined.get('child_direct_wait_by_root') is False and
                         joined.get('backend_settled') is True and joined.get('child_cgroup_empty') is True and
                         joined.get('prepared_transactions') == 0, 'original callback was not joined with its exact effect')
            if fault['boundary'] == 'stdout-loss':
                carrier_name = 'predecessor-stdout-%020d.json' % generation
                host.require(intent.get('stdout_carrier_name') == carrier_name and
                             type(intent.get('stdout_carrier_digest')) is str and
                             HEX.fullmatch(intent['stdout_carrier_digest']) is not None and
                             joined['effect_files'].get(carrier_name) == intent['stdout_carrier_digest'],
                             'original witnessed stdout carrier is missing or changed')
            join_events = [event for event in events if event['kind'] == 'registration' and
                           event['fields'].get('name') == 'callback-join-%020d' % generation and
                           event['fields'].get('identity_digest') == _sha(joined)]
            host.require(len(join_events) == 1 and done[0]['sequence'] < join_events[0]['sequence'],
                         'fault completion did not precede the durable join')
    if fault['victim'] == 'coordinator':
        coordinator = intent.get('coordinator')
        host.require(type(coordinator) is dict, 'killed coordinator identity is missing')
        exits = [event for event in events if event['kind'] == 'exit' and
                 event['fields'].get('name') == 'houfeng-record-platform-admin' and
                 event['fields'].get('pid') == coordinator.get('pid') and
                 event['fields'].get('starttime') == coordinator.get('starttime') and
                 event['fields'].get('status') == -9 and event['fields'].get('pidfd_exited') is True]
        host.require(len(exits) == 1, 'killed coordinator direct wait is missing')
        if fault['boundary'] in CALLBACK_CUTS:
            stop = fault.get('exit_stop')
            keys = {'observer_root', 'observer_thread', 'child', 'phase_generation', 'callback_live_digest', 'event',
                    'exit_signal', 'leader_guard_device', 'leader_guard_inode', 'shared_file_table_verified',
                    'other_guard_holders', 'coordinator_pidfd_exited', 'guard_contended_at_exit_stop', 'tracee_resumed',
                    'tracer_terminal_wait', 'tracer_joined', 'child_pidfd_exited'}
            guard = result['realm']['app_binding']['guard']
            thread = stop.get('observer_thread') if type(stop) is dict else None
            host.require(type(stop) is dict and set(stop) == keys and
                         stop['observer_root'] == {'pid': result['genesis']['supervisor_pid'],
                                                   'starttime': result['genesis']['supervisor_starttime']} and
                         type(thread) is dict and set(thread) == {'pid', 'starttime'} and
                         type(thread['pid']) is int and thread['pid'] > 0 and
                         thread['pid'] != stop['observer_root']['pid'] and type(thread['starttime']) is int and thread['starttime'] > 0 and
                         stop['child'] == intent['child'] == live['child'] and stop['phase_generation'] == generation and
                         stop['callback_live_digest'] == _sha(live) and stop['event'] == 'PTRACE_EVENT_EXIT' and
                         stop['exit_signal'] == 'SIGKILL' and stop['leader_guard_device'] == guard['guard_device'] and
                         stop['leader_guard_inode'] == guard['guard_inode'] and stop['other_guard_holders'] == [] and
                         all(stop[key] is True for key in ('shared_file_table_verified', 'coordinator_pidfd_exited',
                             'guard_contended_at_exit_stop', 'tracee_resumed', 'tracer_terminal_wait', 'tracer_joined', 'child_pidfd_exited')),
                         'actual predecessor exit stop and guard continuity differ')
    if not (fault['victim'] == 'coordinator' and fault['boundary'] in CALLBACK_CUTS):
        host.require(fault.get('exit_stop') is None, 'unexpected predecessor exit-stop evidence')


def _callback_recovery(host, result, evidence, fault, predecessor, callbacks):
    host.require(type(evidence) is dict and set(evidence) == {'phase_generation', 'intent_raw', 'resolution_raw',
                 'effect_raw', 'root_join_name', 'root_join_sha256', 'next_callback_phase'} and
                 evidence['phase_generation'] == fault['phase_generation'] and
                 type(evidence['intent_raw']) is str and type(evidence['resolution_raw']) is str,
                 'original callback recovery records are missing')
    generation = evidence['phase_generation']
    intent_raw, resolution_raw = evidence['intent_raw'].encode('utf-8'), evidence['resolution_raw'].encode('utf-8')
    host.require(0 < len(intent_raw) <= 16384 and 0 < len(resolution_raw) <= 16384, 'recovery record is not bounded')
    intent, resolution = host.strict_json(intent_raw), host.strict_json(resolution_raw)
    join_name = 'callback-join-%020d.json' % generation
    joined = result['root_journal'].get(join_name)
    fixture = result['fixture']
    host.require(type(joined) is dict and evidence['root_join_name'] == join_name and
                 evidence['root_join_sha256'] == _sha(joined) and
                 joined.get('intent_sha256') == hashlib.sha256(intent_raw).hexdigest() and
                 type(intent) is dict and intent.get('coordinator') == joined.get('original_coordinator') and
                 intent.get('actor_user_id') == fixture['actor_user_id'] and
                 intent.get('record_id') == fixture['records']['b']['record_id'] and
                 type(resolution) is dict and set(resolution) == {'version', 'phase_generation', 'recovery_id',
                     'application', 'realm_digest', 'backend', 'create_command_digest', 'reserve_command_digest',
                     'reservation_id', 'operation_id', 'outcome'} and resolution['version'] == 1 and
                 resolution['phase_generation'] == generation and resolution['application'] == intent.get('application') and
                 resolution['recovery_id'] == intent.get('recovery_id') and resolution['realm_digest'] == intent.get('realm_digest'),
                 'original callback resolution does not bind its intent and join')
    effects = _effect_records(host, result, evidence['effect_raw'], joined, generation)
    host.require('create-intent' in effects and resolution['backend'] == effects['ready']['backend'] and
                 resolution['create_command_digest'] == effects['create-intent']['command_digest'] and
                 joined['effect_files'].get(fault['receipt_name']) == fault['evidence_digest'],
                 'original recovery command or witnessed effect differs')
    boundary = fault['boundary']
    expected_outcomes = {'create-intent': 'preview_not_created', 'create-commit-before-result': 'preview_abandoned',
                         'preview-ack-loss': 'preview_abandoned', 'reserve-intent': 'reserve_not_committed_preview_abandoned',
                         'reserve-commit-before-stdout': 'reserve_committed', 'stdout-loss': 'reserve_committed'}
    host.require(resolution['outcome'] == expected_outcomes.get(boundary), 'callback resolution outcome differs')
    row = predecessor['reserved']['b_operations'][0]
    previews = predecessor['reserved']['b_preview_rows']
    if resolution['outcome'] == 'reserve_committed':
        host.require(resolution['operation_id'] == predecessor['operation_id'] == row['operation_id'] and
                     resolution['reservation_id'] == row['reservation_id'] and 'reserve-intent' in effects and
                     resolution['reserve_command_digest'] == effects['reserve-intent']['command_digest'] and
                     evidence['next_callback_phase'] is None and len(callbacks) == 1 and
                     predecessor['service_results'][1] == {'status': 'recovered_committed',
                         'operation_id': resolution['operation_id'], 'reason': 'reserve_committed'},
                     'committed recovery does not identify the original operation')
    else:
        host.require(not resolution['operation_id'] and len(callbacks) == 2 and
                     evidence['next_callback_phase'] == callbacks[1]['trace']['phase_generation'] and
                     evidence['next_callback_phase'] > generation, 'abandoned preview lacks its new callback')
        if resolution['outcome'] == 'preview_not_created':
            host.require(not resolution['reservation_id'], 'uncreated preview unexpectedly has a reservation')
        else:
            old = [entry for entry in previews if entry.get('reservation_id') == resolution['reservation_id']]
            host.require(len(old) == 1 and old[0].get('state') == 'previewed' and old[0].get('fence_epoch') == 0 and
                         old[0].get('operations') == 0 and old[0]['reservation_id'] != row['reservation_id'],
                         'abandoned original preview has a fence or operation')
    return resolution


def _maintenance_witness(host, result, selection):
    from source_supervisor.maintenance import application_digest, checkpoint_name
    raw_chain = result.get('maintenance_witness')
    facts = result['genesis']
    binding = result['realm']['app_binding']
    host.require(type(raw_chain) is list and 0 < len(raw_chain) <= 4096 and type(selection) is dict,
                 'maintenance witness chain or terminal selection missing')
    fields = {'version', 'sequence', 'previous', 'run_token', 'boot_id', 'supervisor', 'guard', 'cgroup',
              'family', 'recovery_id', 'application_digest', 'phase_generation', 'attempt', 'kind',
              'record_name', 'record_bytes', 'record_sha256', 'predecessor_witness_digest', 'live_observation_digest'}
    allowed = {'begin', 'intent', 'phase', 'preparation', 'ready', 'ready-disposition', 'normal', 'restrict', 'observe-intent',
               'observe-normal', 'observe-restrict', 'observe-settled', 'compensate', 'abort', 'successor',
               'activation', 'terminal', 'revocation-unresolved', 'begin-settled', 'ready-abandoned'}
    previous, records, ordered, generations = '0' * 64, {}, [], []
    rid = result['resolver_transition']['recovery_id']
    guard = {'dev': binding['guard']['guard_device'], 'ino': binding['guard']['guard_inode']}
    app_digest = application_digest(binding['application'])
    for index, text in enumerate(raw_chain, 1):
        host.require(type(text) is str, 'maintenance witness bytes missing')
        raw = text.encode('utf-8', 'strict')
        event = host.strict_json(raw)
        host.require(type(event) is dict and event.get('kind') in allowed and
                     set(event) == (fields | {'ready_disposition'} if event['kind'] == 'ready-disposition' else fields) and
                     raw == _canonical(event) and type(event['version']) is int and event['version'] == 1 and
                     type(event['sequence']) is int and event['sequence'] == index and event['previous'] == previous and
                     (event['kind'] == 'ready-disposition' or
                      event['predecessor_witness_digest'] == (previous if index > 1 else None)) and
                     event['run_token'] == RUN_TOKEN and event['boot_id'] == facts['boot_id'] and
                     event['supervisor'] == {'pid': facts['supervisor_pid'], 'starttime': facts['supervisor_starttime']} and
                     event['guard'] == guard and event['cgroup'] == facts['migration_cgroup'] and
                     event['family'] == 'migration' and event['recovery_id'] == rid and event['application_digest'] == app_digest and
                     event['kind'] in allowed and type(event['phase_generation']) is int and 0 < event['phase_generation'] < (1 << 63) and
                     type(event['attempt']) is int and 0 <= event['attempt'] < (1 << 64) and
                     (event['kind'].startswith('observe-') == (event['attempt'] > 0)) and
                     type(event['record_bytes']) is int and 0 < event['record_bytes'] <= 8 * 1024 * 1024 and
                     type(event['record_sha256']) is str and HEX.fullmatch(event['record_sha256']) is not None and
                     event['record_sha256'] != '0' * 64 and
                     ((event['kind'] == 'ready' and type(event['live_observation_digest']) is str and
                       HEX.fullmatch(event['live_observation_digest']) is not None and event['live_observation_digest'] != '0' * 64) or
                      (event['kind'] != 'ready' and event['live_observation_digest'] is None)),
                     'maintenance witness chain or binding differs')
        expected_name = ('migration-ready-%020d.json' % event['phase_generation']
                         if event['kind'] == 'ready-disposition' else checkpoint_name(event))
        host.require(event['record_name'] == expected_name, 'maintenance witness filename differs')
        if event['kind'] == 'begin':
            host.require(not generations or generations[-1][-1]['kind'] == 'terminal',
                         'new migration began before prior terminal')
            if generations:
                host.require(event['phase_generation'] == generations[-1][-1]['phase_generation'] + 1,
                             'migration generation skipped its prior terminal')
            generations.append([])
        host.require(bool(generations), 'maintenance record precedes begin')
        generation = generations[-1][0]['phase_generation'] if generations[-1] else event['phase_generation']
        key = (generation, event['kind'], event['phase_generation'] if event['kind'] in ('phase', 'terminal') else event['attempt'])
        host.require(key not in records, 'duplicate maintenance witness key')
        records[key] = event
        generations[-1].append(event)
        ordered.append(event)
        previous = hashlib.sha256(raw).hexdigest()
    all_ordered = ordered
    decisions = [event for event in all_ordered if event['kind'] == 'ready-disposition']
    ready_records = result.get('maintenance_ready_records')
    host.require(type(ready_records) is list and len(ready_records) == len(decisions),
                 'ready disposition records are missing or extra')
    disposition_records = {}
    for event, evidence in zip(decisions, ready_records):
        generation = event['phase_generation']
        host.require(type(evidence) is dict and set(evidence) ==
                     {'phase_generation', 'record_name', 'record_sha256', 'raw_base64'} and
                     type(evidence['phase_generation']) is int and evidence['phase_generation'] == generation and
                     generation not in disposition_records and evidence['record_name'] == event['record_name'] and
                     evidence['record_sha256'] == event['record_sha256'] and type(evidence['raw_base64']) is str,
                     'ready disposition record identity or order differs')
        try:
            encoded = evidence['raw_base64'].encode('ascii', 'strict')
            host.require(len(encoded) <= ((8 * 1024 * 1024 + 2) // 3) * 4,
                         'ready disposition record exceeds its bound')
            raw = base64.b64decode(encoded, validate=True)
        except (ValueError, UnicodeError, binascii.Error) as exc:
            raise host.Failure('ready disposition record base64 is invalid') from exc
        host.require(base64.b64encode(raw) == encoded and 0 < len(raw) <= 8 * 1024 * 1024 and
                     len(raw) == event['record_bytes'] and hashlib.sha256(raw).hexdigest() == event['record_sha256'],
                     'ready disposition original bytes differ')
        ready_record = host.strict_json(raw)
        host.require(type(ready_record) is dict and set(ready_record) ==
                     {'version', 'phase_generation', 'intent_digest', 'child', 'backend'} and
                     type(ready_record['version']) is int and ready_record['version'] == 1 and
                     type(ready_record['phase_generation']) is int and ready_record['phase_generation'] == generation and
                     type(ready_record['intent_digest']) is str and HEX.fullmatch(ready_record['intent_digest']) is not None and
                     ready_record['intent_digest'] != '0' * 64,
                     'ready disposition record schema differs')
        child, backend = ready_record['child'], ready_record['backend']
        host.require(type(child) is dict and set(child) == {'pid', 'starttime', 'executable_sha256'} and
                     type(child['pid']) is int and 0 < child['pid'] < (1 << 31) and
                     type(child['starttime']) is int and 0 < child['starttime'] < (1 << 64) and
                     child['executable_sha256'] == facts['programs']['c81-migrator'] and
                     type(backend) is dict and set(backend) == {'pid', 'started', 'role'} and
                     type(backend['pid']) is int and 0 < backend['pid'] < (1 << 31) and
                     type(backend['started']) is str and bool(backend['started']) and
                     backend['role'] == 'houfeng_migrator', 'ready disposition process or backend differs')
        disposition_records[generation] = ready_record
    for history in generations:
        generation = history[0]['phase_generation']
        host.require(all(event['phase_generation'] == generation for event in history
                         if event['kind'] not in ('phase', 'terminal')),
                     'maintenance record belongs to a different migration generation')
        decision_events = [event for event in history if event['kind'] == 'ready-disposition']
        host.require(len(decision_events) <= 1, 'migration has multiple ready dispositions')
        if not decision_events:
            host.require(not any(event['kind'] == 'ready-abandoned' for event in history),
                         'ready abandonment lacks an authenticated disposition')
            continue
        decision = decision_events[0]
        disposition = decision['ready_disposition']
        generation = history[0]['phase_generation']
        intents = [event for event in history if event['kind'] == 'intent']
        preparations = [event for event in history if event['kind'] == 'preparation']
        ready_events = [event for event in history if event['kind'] == 'ready']
        host.require(type(disposition) is dict and set(disposition) ==
                     {'outcome', 'ready_witness_sequence', 'ready_witness_digest', 'scope_retirement_digest'} and
                     disposition['outcome'] in ('witnessed', 'unwitnessed_retired') and
                     type(disposition['scope_retirement_digest']) is str and
                     HEX.fullmatch(disposition['scope_retirement_digest']) is not None and
                     disposition['scope_retirement_digest'] != '0' * 64 and
                     len(intents) == len(preparations) == 1 and
                     intents[0]['sequence'] < preparations[0]['sequence'] < decision['sequence'] and
                     disposition_records[generation]['intent_digest'] == intents[0]['record_sha256'],
                     'ready disposition schema or retained lineage differs')
        retired = [scope for scope in result['migration_scopes']
                   if _sha(scope['retirement']) == disposition['scope_retirement_digest'] and
                   scope['enrollment']['command'] == 'legacy-c81-migrate']
        host.require(len(retired) == 1, 'ready disposition does not reference one actual retired migration scope')
        if disposition['outcome'] == 'witnessed':
            host.require(len(ready_events) == 1 and ready_events[0]['sequence'] < decision['sequence'] and
                         type(disposition['ready_witness_sequence']) is int and
                         disposition['ready_witness_sequence'] == ready_events[0]['sequence'] and
                         disposition['ready_witness_digest'] ==
                         hashlib.sha256(raw_chain[ready_events[0]['sequence'] - 1].encode('utf-8')).hexdigest() and
                         decision['predecessor_witness_digest'] == disposition['ready_witness_digest'] and
                         decision['record_name'] == ready_events[0]['record_name'] and
                         decision['record_bytes'] == ready_events[0]['record_bytes'] and
                         decision['record_sha256'] == ready_events[0]['record_sha256'] and
                         not any(event['kind'] == 'ready-abandoned' for event in history),
                         'witnessed ready disposition or original witness differs')
        else:
            abandonments = [event for event in history if event['kind'] == 'ready-abandoned']
            refencing = [event for event in history if event['kind'] == 'phase' and
                         event['phase_generation'] == generation + 1]
            host.require(not ready_events and disposition['ready_witness_sequence'] is None and
                         disposition['ready_witness_digest'] is None and
                         decision['predecessor_witness_digest'] ==
                         hashlib.sha256(raw_chain[preparations[0]['sequence'] - 1].encode('utf-8')).hexdigest() and
                         len(abandonments) == len(refencing) == 1 and
                         decision['sequence'] < abandonments[0]['sequence'] < refencing[0]['sequence'] and
                         not any(event['kind'] in ('normal', 'successor', 'activation') or
                                 event['kind'].startswith('observe-') for event in history),
                         'unwitnessed ready disposition conflicts with witnessed authority or refencing')
    for history in generations[:-1]:
        kinds = [event['kind'] for event in history]
        generation = history[0]['phase_generation']
        host.require(all(event['phase_generation'] == generation for event in history
                         if event['kind'] not in ('phase', 'terminal')),
                     'historical record belongs to a different migration generation')
        host.require(kinds[-1] == 'terminal' and 'activation' not in kinds,
                     'historical migration lacks a restrictive terminal')
        if 'begin-settled' in kinds:
            host.require(kinds == ['begin', 'begin-settled', 'phase', 'terminal'] and
                         history[2]['phase_generation'] == generation and
                         history[3]['phase_generation'] == generation + 1,
                         'historical begin cancellation differs')
        else:
            host.require(all(kinds.count(kind) == 1 for kind in ('begin', 'intent', 'compensate', 'abort', 'terminal')) and
                         kinds.index('intent') < kinds.index('compensate') < kinds.index('abort') < kinds.index('terminal') and
                         not ({'normal', 'successor', 'activation'} & set(kinds)) and
                         not any(kind.startswith('observe-') for kind in kinds) and
                         not ('ready' in kinds and 'ready-abandoned' in kinds),
                         'historical compensation differs')
            phase_events = [event for event in history if event['kind'] == 'phase']
            host.require(len(phase_events) == 2 and
                         [event['phase_generation'] for event in phase_events] == [generation, generation + 1] and
                         history[-1]['phase_generation'] == generation + 2 and
                         history[0]['sequence'] < history[kinds.index('intent')]['sequence'] < phase_events[0]['sequence'] and
                         phase_events[0]['sequence'] < phase_events[1]['sequence'] < history[kinds.index('compensate')]['sequence'] and
                         set(kinds) <= {'begin', 'intent', 'phase', 'preparation', 'ready', 'ready-disposition',
                                        'ready-abandoned', 'restrict', 'revocation-unresolved', 'compensate', 'abort', 'terminal'},
                         'historical compensation phase grammar differs')
            if 'revocation-unresolved' in kinds:
                host.require(kinds.index('intent') < kinds.index('revocation-unresolved') < kinds.index('abort'),
                             'historical revocation has no later witnessed settlement')
            for kind in ('preparation', 'ready', 'ready-abandoned'):
                if kind in kinds:
                    host.require(kinds.count(kind) == 1 and phase_events[0]['sequence'] < history[kinds.index(kind)]['sequence'] <
                                 phase_events[1]['sequence'], 'historical callback evidence occurs outside migration phase')
            if 'ready' in kinds or 'ready-abandoned' in kinds:
                host.require('preparation' in kinds and kinds.index('preparation') <
                             kinds.index('ready' if 'ready' in kinds else 'ready-abandoned'),
                             'historical ready evidence lacks preparation')
            if 'restrict' in kinds:
                host.require(kinds.count('restrict') == 1 and 'preparation' in kinds and
                             phase_events[1]['sequence'] < history[kinds.index('restrict')]['sequence'] <
                             history[kinds.index('compensate')]['sequence'], 'historical restriction order differs')
    ordered = generations[-1]
    records = {(event['kind'], event['phase_generation'] if event['kind'] in ('phase', 'terminal') else event['attempt']): event
               for event in ordered}
    host.require(ordered[0]['kind'] == 'begin' and ordered[-1]['kind'] == 'terminal' and
                 all(sum(event['kind'] == kind for event in ordered) == 1 for kind in
                     ('begin', 'intent', 'preparation', 'ready', 'successor', 'activation', 'terminal')) and
                 not any(event['kind'] in ('abort', 'compensate', 'begin-settled', 'ready-abandoned')
                         for event in ordered),
                 'maintenance witness is not a completed activation chain')
    by_kind = {kind: next(event for event in ordered if event['kind'] == kind)
               for kind in ('begin', 'intent', 'preparation', 'ready', 'successor', 'activation', 'terminal')}
    host.require(by_kind['begin']['sequence'] < by_kind['intent']['sequence'] < by_kind['preparation']['sequence'] <
                 by_kind['ready']['sequence'] < by_kind['successor']['sequence'] < by_kind['activation']['sequence'] <
                 by_kind['terminal']['sequence'], 'maintenance witness order differs')
    unresolved = records.get(('revocation-unresolved', 0))
    if unresolved is not None:
        host.require(by_kind['intent']['sequence'] < unresolved['sequence'] < by_kind['successor']['sequence'],
                     'revocation has no later witnessed successor settlement')
    phases = [event for event in ordered if event['kind'] == 'phase']
    host.require(len(phases) == 2 and phases[0]['phase_generation'] == by_kind['intent']['phase_generation'] and
                 phases[0]['sequence'] > by_kind['intent']['sequence'] and
                 phases[0]['sequence'] < by_kind['preparation']['sequence'] and
                 phases[1]['phase_generation'] == phases[0]['phase_generation'] + 1 and
                 phases[1]['sequence'] > by_kind['ready']['sequence'] and
                 phases[1]['sequence'] < by_kind['successor']['sequence'] and
                 by_kind['terminal']['phase_generation'] == phases[1]['phase_generation'] + 1,
                 'maintenance witnessed phase sequence differs')
    host.require(all(event['sequence'] > by_kind['ready']['sequence'] for event in ordered
                     if event['kind'] == 'normal') and
                 all(event['sequence'] > phases[1]['sequence'] for event in ordered
                     if event['kind'] == 'restrict'),
                 'normal or restriction precedes its witnessed authority')
    attempts = sorted(event['attempt'] for event in ordered if event['kind'] == 'observe-intent')
    host.require(attempts == list(range(1, len(attempts) + 1)), 'maintenance attempts are not contiguous')
    prior_sequence = phases[1]['sequence']
    for number in attempts:
        intent = records.get(('observe-intent', number))
        normal = records.get(('observe-normal', number))
        restriction = records.get(('observe-restrict', number))
        settlement = records.get(('observe-settled', number))
        host.require(intent is not None and restriction is not None and settlement is not None and
                     prior_sequence < intent['sequence'] < restriction['sequence'] < settlement['sequence'] <
                     by_kind['successor']['sequence'] and
                     (normal is None or intent['sequence'] < normal['sequence'] < restriction['sequence']),
                     'maintenance observation attempt is incomplete or out of order')
        prior_sequence = settlement['sequence']
    host.require(all(event['attempt'] in attempts for event in ordered if event['kind'].startswith('observe-')),
                 'maintenance observation has no corresponding intent')
    host.require(set(selection) in ({'ready', 'activation', 'terminal'}, {'ready', 'activation', 'terminal', 'observe-settled'}),
                 'maintenance selected records differ')
    layouts = {
        'ready': ('version', 'phase_generation', 'intent_digest', 'child', 'backend'),
        'activation': ('version', 'phase_generation', 'terminal_phase_generation', 'recovery_id', 'application',
                       'intent_digest', 'original_inventory_digest', 'successor_digest', 'normal_digest',
                       'normal_source', 'normal_attempt', 'manifest_digest'),
        'terminal': ('version', 'generation', 'previous', 'recovery_id', 'application', 'phase'),
        'observe-settled': ('version', 'phase_generation', 'attempt', 'intent_digest',
                            'observation_intent_digest', 'restriction_digest', 'outcome', 'normal_digest'),
    }
    parsed = {}
    for kind, item in selection.items():
        host.require(type(item) is dict and set(item) == set(layouts[kind]), 'maintenance selected metadata differs')
        ordered_item = {key: item[key] for key in layouts[kind]}
        if 'application' in ordered_item:
            application = ordered_item['application']
            names = ('ID', 'SystemIdentifier', 'DatabaseName', 'DatabaseOID', 'Epoch')
            host.require(type(application) is dict and set(application) == set(names), 'selected application identity differs')
            ordered_item['application'] = {key: application[key] for key in names}
        if kind == 'ready':
            for name, keys in (('child', ('pid', 'starttime', 'executable_sha256')),
                               ('backend', ('pid', 'started', 'role'))):
                value = ordered_item[name]
                host.require(type(value) is dict and set(value) == set(keys), 'ready identity metadata differs')
                ordered_item[name] = {key: value[key] for key in keys}
        text = json.dumps(ordered_item, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
        text = text.replace('&', r'\u0026').replace('<', r'\u003c').replace('>', r'\u003e')
        text = text.replace('\u2028', r'\u2028').replace('\u2029', r'\u2029')
        raw = text.encode('utf-8', 'strict')
        matches = [event for event in ordered if event['kind'] == kind and
                   event['record_sha256'] == hashlib.sha256(raw).hexdigest() and event['record_bytes'] == len(raw)]
        host.require(len(matches) == 1, 'maintenance selected metadata does not match witness')
        parsed[kind] = item
    ready = parsed['ready']
    observations = result.get('maintenance_observations')
    witnessed_ready = {event['sequence']: event for event in all_ordered if event['kind'] == 'ready'}
    host.require(type(observations) is list and len(observations) == len(witnessed_ready),
                 'migration ready live observations are incomplete')
    seen_observations, selected_live = set(), None
    for observation in observations:
        host.require(type(observation) is dict and set(observation) == {'witness_sequence', 'observation'} and
                     type(observation['witness_sequence']) is int and observation['witness_sequence'] in witnessed_ready and
                     observation['witness_sequence'] not in seen_observations,
                     'migration ready live observation binding differs')
        sequence = observation['witness_sequence']
        seen_observations.add(sequence)
        historical_live = observation['observation']
        host.require(type(historical_live) is dict and set(historical_live) ==
                     {'child', 'backend', 'parent', 'exe_dev', 'exe_ino', 'cgroup', 'pidfd_live'} and
                     _sha(historical_live) == witnessed_ready[sequence]['live_observation_digest'] and
                     historical_live['cgroup'] == facts['migration_cgroup'] and historical_live['pidfd_live'] is True and
                     type(historical_live['exe_dev']) is int and historical_live['exe_dev'] >= 0 and
                     type(historical_live['exe_ino']) is int and historical_live['exe_ino'] > 0 and
                     any(scope['enrollment']['coordinator'] == historical_live['parent'] for scope in result['migration_scopes']),
                     'historical live observation or registered parent differs')
        historical_child, historical_backend = historical_live['child'], historical_live['backend']
        host.require(type(historical_child) is dict and set(historical_child) == {'pid', 'starttime', 'executable_sha256'} and
                     type(historical_child['pid']) is int and historical_child['pid'] > 0 and
                     type(historical_child['starttime']) is int and historical_child['starttime'] > 0 and
                     historical_child['executable_sha256'] == facts['programs']['c81-migrator'] and
                     type(historical_backend) is dict and set(historical_backend) == {'pid', 'started', 'role'} and
                     type(historical_backend['pid']) is int and historical_backend['pid'] > 0 and
                     type(historical_backend['started']) is str and bool(historical_backend['started']) and
                     historical_backend['role'] == 'houfeng_migrator',
                     'historical ready executable or backend identity differs')
        matching_decisions = [event for event in decisions
                              if event['ready_disposition']['ready_witness_sequence'] == sequence]
        if matching_decisions:
            decision = matching_decisions[0]
            retained = disposition_records[decision['phase_generation']]
            host.require(len(matching_decisions) == 1 and historical_child == retained['child'] and
                         historical_backend == retained['backend'] and
                         any(scope['enrollment']['coordinator'] == historical_live['parent'] and
                             _sha(scope['retirement']) == decision['ready_disposition']['scope_retirement_digest']
                             for scope in result['migration_scopes']),
                         'ready disposition lost its original live observation or retired parent')
        if sequence == by_kind['ready']['sequence']:
            selected_live = observation['observation']
    live = selected_live
    settlements = result.get('maintenance_settlements')
    host.require(type(settlements) is list, 'maintenance settlement observations missing')
    settlement_events = {event['sequence']: event for event in all_ordered
                         if event['kind'] in ('begin-settled', 'ready-abandoned')}
    seen_settlements = set()
    retirement_digests = {_sha(scope['retirement']) for scope in result['migration_scopes']}
    for settlement in settlements:
        host.require(type(settlement) is dict and set(settlement) ==
                     {'kind', 'phase_generation', 'witness_sequence', 'witness_digest', 'scope_retirement_digest', 'observation'} and
                     type(settlement['witness_sequence']) is int and settlement['witness_sequence'] in settlement_events,
                     'maintenance settlement witness reference differs')
        sequence = settlement['witness_sequence']
        event = settlement_events[sequence]
        host.require(settlement['kind'] == event['kind'] and
                     type(settlement['phase_generation']) is int and settlement['phase_generation'] == event['phase_generation'] and
                     settlement['witness_digest'] == hashlib.sha256(raw_chain[sequence - 1].encode('utf-8')).hexdigest() and
                     settlement['scope_retirement_digest'] in retirement_digests,
                     'maintenance settlement lineage differs')
        observation = settlement['observation']
        host.require(type(observation) is dict and set(observation) ==
                     {'application', 'current_backend', 'observer_backend', 'retired_backends',
                      'remaining_clients', 'prepared_transactions', 'transaction_committed'} and
                     observation['application'] == binding['application'] and
                     type(observation['remaining_clients']) is int and observation['remaining_clients'] == 0 and
                     type(observation['prepared_transactions']) is int and observation['prepared_transactions'] == 0 and
                     observation['transaction_committed'] is True and type(observation['retired_backends']) is list,
                     'maintenance independent transaction settlement differs')
        backends = [observation['current_backend'], observation['observer_backend']] + observation['retired_backends']
        for backend in backends:
            host.require(type(backend) is dict and set(backend) == {'pid', 'started', 'role'} and
                         type(backend['pid']) is int and backend['pid'] > 0 and
                         type(backend['started']) is str and bool(backend['started']) and
                         type(backend['role']) is str and bool(backend['role']),
                         'maintenance settlement backend tuple differs')
        host.require(len({_sha(backend) for backend in backends}) == len(backends),
                     'maintenance observer, current or retired backend overlaps')
        if event['kind'] == 'ready-abandoned':
            matching_decisions = [decision for decision in decisions
                                  if decision['phase_generation'] == event['phase_generation']]
            host.require(len(matching_decisions) == 1 and
                         matching_decisions[0]['ready_disposition']['outcome'] == 'unwitnessed_retired' and
                         settlement['scope_retirement_digest'] ==
                         matching_decisions[0]['ready_disposition']['scope_retirement_digest'] and
                         disposition_records[event['phase_generation']]['backend'] in observation['retired_backends'],
                         'ready abandonment did not settle its original scope and backend')
        seen_settlements.add(sequence)
    host.require(seen_settlements == set(settlement_events), 'maintenance settlement witness lacks actual observation')
    host.require(type(live) is dict and set(live) == {'child', 'backend', 'parent', 'exe_dev', 'exe_ino', 'cgroup', 'pidfd_live'} and
                 _sha(live) == by_kind['ready']['live_observation_digest'] and
                 live['child'] == ready['child'] and live['backend'] == ready['backend'] and
                 live['cgroup'] == facts['migration_cgroup'] and live['pidfd_live'] is True and
                 type(live['exe_dev']) is int and live['exe_dev'] >= 0 and type(live['exe_ino']) is int and live['exe_ino'] > 0 and
                 any(scope['enrollment']['coordinator'] == live['parent'] for scope in result['migration_scopes']) and
                 type(ready['version']) is int and ready['version'] == 1 and
                 type(ready['phase_generation']) is int and ready['phase_generation'] == by_kind['intent']['phase_generation'] and
                 ready['intent_digest'] == by_kind['intent']['record_sha256'] and
                 type(ready['child']['pid']) is int and ready['child']['pid'] > 0 and
                 type(ready['child']['starttime']) is int and ready['child']['starttime'] > 0 and
                 ready['child']['executable_sha256'] == facts['programs']['c81-migrator'] and
                 type(ready['backend']['pid']) is int and ready['backend']['pid'] > 0 and
                 type(ready['backend']['started']) is str and bool(ready['backend']['started']) and
                 ready['backend']['role'] == 'houfeng_migrator',
                 'live migrator executable or independent backend binding differs')
    activation, terminal = parsed['activation'], parsed['terminal']
    host.require(set(activation) == {'version', 'phase_generation', 'intent_digest', 'terminal_phase_generation',
                                     'recovery_id', 'application', 'original_inventory_digest', 'successor_digest',
                                     'normal_digest', 'manifest_digest', 'normal_source', 'normal_attempt'} and
                 type(activation['version']) is int and activation['version'] == 1 and
                 all(type(activation[key]) is int for key in ('phase_generation', 'terminal_phase_generation', 'normal_attempt')) and
                 all(type(activation[key]) is str and HEX.fullmatch(activation[key]) is not None and activation[key] != '0' * 64
                     for key in ('intent_digest', 'original_inventory_digest', 'successor_digest', 'normal_digest', 'manifest_digest')) and
                 activation['recovery_id'] == rid and
                 activation['application'] == binding['application'] and
                 activation['intent_digest'] == by_kind['intent']['record_sha256'] and
                 activation['successor_digest'] == by_kind['successor']['record_sha256'] and
                 activation['phase_generation'] == by_kind['activation']['phase_generation'] and
                 activation['terminal_phase_generation'] == by_kind['terminal']['phase_generation'] and
                 type(terminal) is dict and set(terminal) == {'version', 'generation', 'previous', 'phase',
                                                             'application', 'recovery_id'} and
                 type(terminal['version']) is int and terminal['version'] == 1 and type(terminal['generation']) is int and
                 terminal['phase'] == 'quiescent' and
                 terminal['generation'] == activation['terminal_phase_generation'] and
                 terminal['application'] == binding['application'] and terminal['recovery_id'] == rid and
                 type(terminal['previous']) is list and len(terminal['previous']) == 32 and
                 all(type(x) is int and 0 <= x <= 255 for x in terminal['previous']) and
                 bytes(terminal['previous']).hex() == phases[1]['record_sha256'],
                 'maintenance activation or terminal binding differs')
    normal_kind, attempt = activation['normal_source'], activation['normal_attempt']
    if normal_kind == 'original':
        normal = records.get(('normal', 0))
        restriction = records.get(('restrict', 0))
        host.require(attempt == 0 and set(selection) == {'ready', 'activation', 'terminal'} and normal is not None and
                     restriction is not None and normal['sequence'] < restriction['sequence'] < by_kind['successor']['sequence'],
                     'original normal observation was not refenced')
    else:
        host.require(normal_kind == 'observation' and type(attempt) is int and attempt > 0 and
                     set(selection) == {'ready', 'activation', 'terminal', 'observe-settled'}, 'selected observation is invalid')
        normal, settled = records.get(('observe-normal', attempt)), records.get(('observe-settled', attempt))
        observed = parsed['observe-settled']
        restriction = records.get(('observe-restrict', attempt))
        host.require(normal is not None and settled is not None and restriction is not None and
                     set(observed) == {'version', 'phase_generation', 'attempt', 'intent_digest',
                                       'observation_intent_digest', 'restriction_digest', 'outcome', 'normal_digest'} and
                     type(observed['version']) is int and observed['version'] == 1 and
                     type(observed['phase_generation']) is int and type(observed['attempt']) is int and
                     observed['phase_generation'] == by_kind['intent']['phase_generation'] and
                     observed['intent_digest'] == by_kind['intent']['record_sha256'] and
                     observed['observation_intent_digest'] == records[('observe-intent', attempt)]['record_sha256'] and
                     observed['restriction_digest'] == restriction['record_sha256'] and
                     observed['outcome'] == 'normal_observed_refenced' and observed['attempt'] == attempt and
                     observed['normal_digest'] == normal['record_sha256'] and
                     normal['sequence'] < settled['sequence'] < by_kind['successor']['sequence'],
                     'selected fresh observation is not settled')
    host.require(activation['normal_digest'] == normal['record_sha256'], 'activation selected a different normal observation')


def _positive(host, scenario, result, record):
    _root_journal(host, result, record, True, allow_retired_maintenance=True)
    source, predecessor, handoff = result.get('source'), result.get('predecessor'), result.get('handoff')
    host.require(type(source) is dict and set(source) == {'preview', 'retirement'} and
                 source['preview'].get('state') == 'preview_recorded' and
                 source['retirement'].get('state') == 'witnessed_handoff_pending' and
                 source['retirement'].get('source_authorization_permanently_retired') is True and
                 source['retirement'].get('source_asset_content_retained') is True and
                 type(source['retirement'].get('ledger_sequence')) is int and source['retirement']['ledger_sequence'] > 0 and
                 HEX.fullmatch(source['retirement'].get('ledger_entry_hash', '')) is not None,
                 'real source preview and retirement evidence missing')
    host.require(type(predecessor) is dict and set(predecessor) == {'baseline', 'denied', 'reserved', 'operation_id', 'service_results'},
                 'real predecessor readback missing')
    baseline, denied, reserved = predecessor['baseline'], predecessor['denied'], predecessor['reserved']
    host.require(all(type(item) is dict for item in (baseline, denied, reserved)) and
                 baseline.get('a_reservations') == baseline.get('b_reservations') == 0 and
                 denied.get('a_reservations') == denied.get('b_reservations') == 0 and
                 reserved.get('a_reservations') == 0 and len(reserved.get('b_operations', [])) == 1 and
                 reserved.get('a_asset') is True and reserved['b_operations'][0].get('operation_id') == predecessor['operation_id'] and
                 reserved['b_operations'][0].get('operation_state') == 'provisional_fenced',
                 'APP predecessor readback does not show one real reservation')
    callback_fault = any(scenario == cut + '-' + victim for cut in CALLBACK_CUTS for victim in ('child', 'coordinator'))
    recovered = callback_fault and scenario.startswith(('reserve-commit-before-stdout-', 'stdout-loss-'))
    expected_b = ({'status': 'recovered_committed', 'operation_id': predecessor['operation_id'], 'reason': 'reserve_committed'}
                  if recovered else {'status': 'committed', 'operation_id': predecessor['operation_id'], 'reason': 'app_reserved'})
    host.require(predecessor.get('service_results') == [
        {'status': 'rejected', 'operation_id': '', 'reason': 'source_denied'}, expected_b],
        'real C80 service results differ')
    callbacks = result.get('callbacks')
    host.require(type(callbacks) is list and len(callbacks) == (1 if recovered else 2), 'callback receipt count differs')
    _callback(host, result, callbacks[0], 'a', 'rejected')
    if not recovered:
        committed = _callback(host, result, callbacks[1], 'b', 'committed')
        host.require(committed['child_result']['operation_id'] == predecessor['operation_id'],
                     'callback and APP readback operation differ')
    host.require(type(handoff) is dict and set(handoff) == {'migration', 'source_handoff', 'record_ledger', 'maintenance_selection'} and
                 handoff['source_handoff'].get('state') == 'handoff_installed' and
                 all(handoff['source_handoff'].get(key) == source['retirement'].get(key) for key in
                     ('operation_id', 'source', 'ledger_domain', 'ledger_sequence', 'ledger_entry_hash',
                      'deletion_contract_version', 'w2_legacy_blocker_unchanged')) and
                 type(handoff['record_ledger'].get('ledger_sequence')) is int and handoff['record_ledger']['ledger_sequence'] > 0,
                 'same-APP C81 handoff or real ledger acceptance missing')
    _maintenance_witness(host, result, handoff['maintenance_selection'])
    mutations = result.get('mutations')
    host.require(type(mutations) is dict and mutations.get('retired_source_denied') is True and
                 mutations.get('independent_source_allowed') is True,
                 'real source mutation barrier evidence missing')
    migration_scopes = [scope for scope in result['migration_scopes']
                        if scope['enrollment']['command'] == 'legacy-c81-migrate']
    host.require(bool(migration_scopes) and migration_scopes[-1]['retirement']['status'] == 0,
                 'migration lacks its final successful registered invocation')
    for scope in migration_scopes[:-1]:
        host.require(scope['retirement']['status'] != 0 or bool(scope['retirement']['terminal_acks']),
                     'replacement migration lacks a prior retired responsibility')
    base_order = (['legacy-retirement-preview', 'legacy-retire', 'legacy-c80-service', 'legacy-c80-service'] +
                  ['legacy-c81-migrate'] * len(migration_scopes) + ['legacy-retirement-reconcile', 'reconcile-legacy'])
    repeat = {'app-commit': 'legacy-retire', 'w1-commit': 'legacy-retire', 'w2-commit': 'legacy-retire',
              'anchor-fsync': 'legacy-retire', 'c81-convergence-commit': 'legacy-c81-migrate',
              'handoff-install': 'legacy-retirement-reconcile', 'handoff-return': 'legacy-retirement-reconcile'}
    if scenario in repeat and repeat[scenario] != 'legacy-c81-migrate':
        base_order.insert(base_order.index(repeat[scenario]), repeat[scenario])
    if callback_fault:
        base_order.insert(base_order.index('legacy-c80-service'), 'legacy-c80-service')
    host.require(result.get('action_order') == base_order, 'source, predecessor, migration and handoff order differs')
    if scenario != 'normal':
        fault = result.get('fault')
        _fault_evidence(host, result, scenario, fault)
        if scenario in repeat:
            host.require(fault.get('operation_id') == source['retirement']['operation_id'],
                         'source recovery changed the original operation')
        elif callback_fault:
            _callback_recovery(host, result, result.get('callback_recovery'), fault, predecessor, callbacks)
        else:
            host.require(False, 'unrecognized recovery scenario')


def validate_early_seal_result(host, case, scenario, variant, result, record, build):
    host.require(type(result) is dict and
                 set(result) == {'version', 'run_token', 'case_sha256', 'seal_stage', 'init_handoff', 'terminal_observation'} and
                 result['version'] == 1 and result['run_token'] == RUN_TOKEN and
                 result['case_sha256'] == record['seed']['case.json'] and
                 result['seal_stage'] == variant and result['init_handoff'] is False,
                 'early seal-gap result schema invalid')
    source_bpf_verify._verify_terminal_observation_obj(host, result['terminal_observation'], variant, 'early_no_handoff')


def validate_result(host, case, scenario, result, record, build, *, reboot_result=None):
    host.require((type(reboot_result) is dict if case == 'source-recovery' and scenario == 'vm-kill'
                  else reboot_result is None) and 'reboot_result' not in record,
                 'unexpected or missing reboot result')
    host.require(type(result) is dict and result.get('version') == 1 and result.get('run_token') == RUN_TOKEN and
                 result.get('case') == case and result.get('scenario') == scenario and result.get('completed') is True and
                 result.get('product_gate') == 'NOT_READY' and result.get('permanent_deletion_http') == 'CLOSED' and
                 'failure' not in result, 'guest scenario did not complete')
    source_bpf_verify.verify(host, result, record, build, reboot_result=reboot_result)
    _carrier_hardware(host, result, record)
    _dns_evidence(host, result, record)
    if case == 'realm-negative' or (case == 'source-recovery' and scenario in (
            'root-kill', 'vm-kill', 'app-rollback', 'conflicting-tuple', 'missing-journal', 'unsettled-backend')):
        _negative(host, scenario, result, record, reboot_result=reboot_result)
    else:
        _positive(host, scenario, result, record)

def _boot_inputs(host, frozen, state, build, *, failure_stages=None):
    inputs = build.get('boot_inputs')
    paths = source_vm._boot_paths(host, build)
    boot_fds = state.get('boot_fds')
    source_vm._validate_boot_fd_record(host, boot_fds)
    host.require(state.get('boot_inputs') == inputs and
                 inputs['base_sha256'] == frozen.UBUNTU_HASHES[frozen.IMAGE] and
                 state['host_launch'].get('boot_inputs') == inputs and
                 state['host_launch'].get('boot_fds') == boot_fds,
                 'direct boot launch binding differs')
    for kind in ('kernel', 'initrd'):
        info = os.stat(paths[kind], follow_symlinks=False)
        expected = {'dev': info.st_dev, 'ino': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
                    'mode': stat.S_IMODE(info.st_mode), 'nlink': info.st_nlink, 'size': info.st_size,
                    'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns,
                    'sha256': host.digest(paths[kind])[0]}
        host.require(stat.S_ISREG(info.st_mode) and boot_fds[kind + '_identity'] == expected and
                     expected['sha256'] == inputs[kind + '_sha256'] and
                     expected['size'] == inputs[kind + '_bytes'], 'direct boot artifact changed')
    host.require(host.digest(paths['original_initrd'])[0] == inputs['original_initrd_sha256'],
                 'original initrd bytes changed')
    stages = ['before_launch', 'after_wait']
    if state['case'] == 'source-recovery' and state['scenario'] == 'vm-kill':
        stages.extend(('before_reboot', 'after_reboot_wait'))
    if failure_stages is not None:
        stages = failure_stages
    observations = state.get('boot_observations')
    host.require(type(observations) is list and len(observations) == len(stages) and
                 all(type(item) is dict and set(item) == {'stage', 'boot_fds'} and
                     item == {'stage': stage, 'boot_fds': boot_fds}
                     for item, stage in zip(observations, stages)) and
                 state['attempt'].get('boot_fds') == boot_fds and
                 ('reboot_attempt' not in state or state['reboot_attempt'].get('boot_fds') == boot_fds),
                 'held boot boundary observations differ')


def _seed_carrier_identity(host, image):
    host.require(type(image) is dict and set(image) == {'dev', 'ino', 'size', 'sha256', 'uuid', 'label', 'features'} and
                 type(image['dev']) is int and image['dev'] >= 0 and type(image['ino']) is int and image['ino'] > 0 and
                 type(image['size']) is int and 2 * 1024 ** 3 <= image['size'] <= 8 * 1024 ** 3 and
                 image['size'] % (256 * 1024 ** 2) == 0 and image['label'] == 'G2SEED' and
                 type(image['sha256']) is str and HEX.fullmatch(image['sha256']) is not None and
                 type(image['uuid']) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', image['uuid']) is not None and
                 type(image['features']) is list and image['features'] == sorted(source_vm.SEED_FEATURES),
                 'seed image identity invalid')


def _cidata_carrier_identity(host, raw_image, block):
    host.require(type(raw_image) is dict and set(raw_image) ==
                 {'path', 'dev', 'ino', 'size', 'uid', 'mode', 'sha256'} and
                 raw_image['path'] == 'cidata.raw' and
                 type(raw_image['dev']) is int and raw_image['dev'] >= 0 and
                 type(raw_image['ino']) is int and raw_image['ino'] > 0 and
                 type(raw_image['size']) is int and raw_image['size'] == 1024 * 16 * 63 * 512 and
                 type(raw_image['uid']) is int and raw_image['uid'] == os.geteuid() and
                 type(raw_image['mode']) is int and raw_image['mode'] == 0o400 and
                 type(raw_image['sha256']) is str and HEX.fullmatch(raw_image['sha256']) is not None and
                 type(block) is dict and set(block) == {'size', 'sha256'} and
                 type(block['size']) is int and block == {key: raw_image[key] for key in ('size', 'sha256')},
                 'CIDATA frozen raw image identity missing')


def _carrier_observations(host, state, stages, *, partial=False):
    observations = state.get('carrier_observations')
    if observations is None and partial:
        return False
    host.require(type(observations) is list and
                 (len(observations) <= len(stages) if partial else len(observations) == len(stages)),
                 'held carrier boundary observations missing')
    if partial and not observations:
        return False
    image, cidata, raw_image, block = (state.get(key) for key in
                                        ('seed_image', 'cidata', 'cidata_image', 'cidata_block'))
    _seed_carrier_identity(host, image)
    _cidata_carrier_identity(host, raw_image, block)
    host.require(type(cidata) is dict, 'CIDATA contents missing')
    seed_identity = {'dev': image['dev'], 'ino': image['ino'], 'size': image['size'],
                     'uid': os.geteuid(), 'mode': 0o400}
    raw_identity = {key: raw_image[key] for key in ('dev', 'ino', 'size', 'uid', 'mode')}
    for observation, stage in zip(observations, stages):
        host.require(type(observation) is dict and set(observation) ==
                     {'stage', 'seed_held', 'seed_named', 'seed_sha256', 'cidata_identity', 'cidata',
                      'cidata_held', 'cidata_named', 'cidata_sha256'} and
                     observation['stage'] == stage and observation['seed_held'] == seed_identity and
                     observation['seed_named'] == seed_identity and observation['seed_sha256'] == image['sha256'] and
                     observation['cidata_identity'] == state['cidata_identity'] and observation['cidata'] == cidata and
                     observation['cidata_held'] == raw_identity and observation['cidata_named'] == raw_identity and
                     observation['cidata_sha256'] == raw_image['sha256'],
                     'seed or CIDATA changed across a QEMU lifetime boundary')
    return len(observations) == len(stages)


def _failure_raw_carriers(host, directory, state):
    for name, key in (('seed.raw', 'seed_image'), ('cidata.raw', 'cidata_image')):
        path = directory / name
        if not os.path.lexists(path):
            continue
        identity = state.get(key)
        host.require(type(identity) is dict, 'retained carrier identity missing')
        if name == 'seed.raw':
            _seed_carrier_identity(host, identity)
        else:
            _cidata_carrier_identity(host, identity, state.get('cidata_block'))
        host.check_path(path, False)
        info = os.stat(path, follow_symlinks=False)
        host.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and
                     (info.st_dev, info.st_ino, info.st_size, info.st_uid, stat.S_IMODE(info.st_mode)) ==
                     (identity['dev'], identity['ino'], identity['size'], os.geteuid(), 0o400) and
                     host.digest(path)[0] == identity['sha256'], 'retained carrier identity or bytes changed')
        after = os.stat(path, follow_symlinks=False)
        fields = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_uid, item.st_mode,
                               item.st_nlink, item.st_mtime_ns, item.st_ctime_ns)
        host.require(fields(after) == fields(info), 'retained carrier changed during readback')


def _carrier_files(host, frozen, directory, state, build, *, failure_stages=None):
    _boot_inputs(host, frozen, state, build, failure_stages=failure_stages)
    payload, cidata, image, launch = (state.get(key) for key in ('seed', 'cidata', 'seed_image', 'host_launch'))
    variant = state.get('bpf_variant')
    host.require(type(payload) is dict and type(cidata) is dict and type(image) is dict and type(launch) is dict and
                 set(image) == {'dev', 'ino', 'size', 'sha256', 'uuid', 'label', 'features'} and
                 type(image['dev']) is int and image['dev'] >= 0 and type(image['ino']) is int and image['ino'] > 0 and
                 type(image['size']) is int and 2 * 1024 ** 3 <= image['size'] <= 8 * 1024 ** 3 and
                 image['size'] % (256 * 1024 ** 2) == 0 and image['label'] == 'G2SEED' and
                 type(image['sha256']) is str and HEX.fullmatch(image['sha256']) is not None and
                 type(image['uuid']) is str and re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', image['uuid']) is not None and
                 type(image['features']) is list and image['features'] == sorted(source_vm.SEED_FEATURES) and
                 launch.get('seed_image') == image and launch.get('payload_case_sha256') == payload.get('case.json') and
                 launch.get('qemu_sha256') == build['qemu_binaries']['qemu-system-x86_64'] and
                 launch.get('argv_sha256') == hashlib.sha256(host.canonical(
                     source_vm.expected_vm_argv(host, directory, state['boot_fds'], variant=variant,
                                                case_sha256=payload.get('case.json')))).hexdigest() and
                 launch.get('seed_inputs_sha256') == hashlib.sha256(host.canonical(build['seed_inputs'])).hexdigest(),
                 'host launch or seed image identity differs')
    expected_inputs = {name: digest for name, digest in build['seed_inputs'].items()
                       if name not in ('user-data', 'network-config')}
    host.require(set(payload) == set(expected_inputs) | {'case.json', 'SHA256SUMS'} and
                 all(payload.get(name) == digest for name, digest in expected_inputs.items()) and
                 set(cidata) == {'meta-data', 'user-data', 'network-config', 'seed-descriptor.json'} and
                 all(cidata[name] == build['seed_inputs'][name] for name in ('user-data', 'network-config')),
                 'seed payload or CIDATA inputs differ from the selected build')
    source_vm._verify_payload(host, directory / 'payload', payload)
    source_vm._verify_cidata(host, directory / 'cidata', cidata)
    paths = ([directory / 'payload', directory / 'cidata', directory / 'seed.raw'] +
             [directory / 'payload' / name for name in payload] +
             [directory / 'cidata' / name for name in cidata])
    host.require(all(os.stat(path, follow_symlinks=False).st_uid == os.geteuid() for path in paths) and
                 state.get('cidata_sha256') == hashlib.sha256(host.canonical(cidata)).hexdigest(),
                 'carrier ownership or complete CIDATA digest differs')
    host.require(source_vm._cidata_identity(directory / 'cidata', cidata) == state.get('cidata_identity'),
                 'CIDATA directory or file identity changed')
    
    stages = ['before_launch', 'after_wait']
    if state['case'] == 'source-recovery' and state['scenario'] == 'vm-kill':
        stages.extend(('before_reboot', 'after_reboot_wait'))
    if failure_stages is not None:
        stages = failure_stages
    _carrier_observations(host, state, stages)
    raw_image = state['cidata_image']
    raw_identity = {key: raw_image[key] for key in ('dev', 'ino', 'size', 'uid', 'mode')}
    path = directory / 'cidata.raw'
    host.check_path(path, False)
    info = os.stat(path, follow_symlinks=False)
    host.require((info.st_dev, info.st_ino, info.st_size, info.st_uid, info.st_mode & 0o7777) ==
                 tuple(raw_identity[key] for key in ('dev', 'ino', 'size', 'uid', 'mode')) and
                 host.digest(path)[0] == raw_image['sha256'], 'CIDATA frozen raw image bytes changed')
    host.check_path(directory / 'seed.raw', False)
    source_vm._verify_seed_image(host, directory / 'seed.raw', image, payload, frozen.task_environment())
    total = sum(os.stat(directory / 'payload' / name, follow_symlinks=False).st_size for name in payload)
    quantum = 256 * 1024 ** 2
    capacity = max(2 * 1024 ** 3, (((total * 5 + 3) // 4 + quantum + quantum - 1) // quantum) * quantum)
    host.require(image['size'] == capacity and launch.get('version') == 1 and launch.get('run_token') == RUN_TOKEN and
                 launch.get('case') == state['case'] and launch.get('scenario') == state['scenario'] and
                 launch.get('disks') == state['disks_before'], 'seed capacity or launch case differs')
    base = Path(frozen.load_json(host.RUN / 'prepare.json')['assets']['ubuntu:' + frozen.IMAGE])
    base_pin = frozen.UBUNTU_HASHES[frozen.IMAGE]
    boot_path = directory / 'boot.qcow2'
    host.check_path(base, False)
    host.check_path(boot_path, False)
    boot_info = os.stat(boot_path, follow_symlinks=False)
    boot_image = source_vm._disk_info(host, boot_path, frozen.task_environment())
    host.require(host.digest(base)[0] == base_pin == state.get('base_before') == state.get('base_after') ==
                 launch.get('base_sha256') and boot_image.get('format') == 'qcow2' and
                 boot_image.get('backing-filename') == str(base) and type(launch.get('boot')) is dict and
                 (boot_info.st_dev, boot_info.st_ino) == (launch['boot'].get('dev'), launch['boot'].get('ino')),
                 'boot overlay or immutable backing identity differs')
    disks = state['disks_before']
    host.require(type(disks) is dict and set(disks) == {name for name, _ in source_vm.DISKS} and
                 len({(disks[name]['dev'], disks[name]['ino']) for name in disks}) == 4 and
                 len({disks[name]['sha256'] for name in disks}) == 1,
                 'original four-volume identity differs')
    for name, serial in source_vm.DISKS:
        disk, info = disks[name], os.stat(directory / (name + '.raw'), follow_symlinks=False)
        host.require(disk['serial'] == serial and disk['size'] == 8 * 1024 ** 3 and
                     (info.st_dev, info.st_ino, info.st_size) == (disk['dev'], disk['ino'], disk['size']),
                     'original business volume identity changed')
    case_value = {'version': 1, 'run_token': RUN_TOKEN, 'case': state['case'], 'scenario': state['scenario'],
                  'bpf_variant': state.get('bpf_variant'),
                  'qemu_sha256': launch['qemu_sha256'], 'boot_inputs': build['boot_inputs'],
                  'bpf_bootstrap': build['bpf_authority']['bootstrap_manifest']}
    host.require(len(host.canonical(case_value)) <= 4096 and
                 (directory / 'payload' / 'case.json').read_bytes() == host.canonical(case_value),
                 'frozen payload case identity differs')
    meta = ('instance-id: g2-%s-%s-%s\nlocal-hostname: g2-source\n' %
            (RUN_TOKEN, state['case'], state['scenario'])).encode('ascii')
    host.require((directory / 'cidata' / 'meta-data').read_bytes() == meta, 'CIDATA case metadata differs')
    template = {name: cidata[name] for name in ('meta-data', 'network-config', 'user-data')}
    host.require(launch.get('cidata_template_sha256') == hashlib.sha256(host.canonical(template)).hexdigest(),
                 'CIDATA boot template binding differs')
    descriptor = {'version': 1, 'run_token': RUN_TOKEN, 'case': state['case'], 'scenario': state['scenario'],
                  'payload_case_sha256': payload['case.json'], 'payload_manifest_sha256': payload['SHA256SUMS'],
                  'seed_uuid': image['uuid'], 'seed_size': image['size'], 'host_launch_sha256': state['host_launch_sha256'],
                  'host_argv_sha256': launch['argv_sha256'], 'qemu_sha256': launch['qemu_sha256']}
    host.require((directory / 'cidata' / 'seed-descriptor.json').read_bytes() == host.canonical(descriptor),
                 'CIDATA seed descriptor differs from the host launch and payload')


def _qmp(host, path, attempt, directory, *, captured=None):
    if captured is None:
        host.check_path(path, False)
        captured = path.read_bytes()
    commands, requests, responses = [], {}, {}
    greeting = 0
    for line in captured.splitlines():
        if line.startswith(b'> '):
            item = host.strict_json(line[2:])
            host.require(type(item) is dict and set(item) == {'execute', 'id'} and
                         item['execute'] in source_vm.ALLOWED_QMP and type(item['id']) is int and
                         item['id'] > 0 and item['id'] not in requests, 'QMP request differs')
            commands.append(item['execute'])
            requests[item['id']] = item['execute']
            continue
        item = host.strict_json(line)
        host.require(type(item) is dict, 'QMP response is not an object')
        if 'QMP' in item:
            greeting += 1
        elif 'event' in item:
            host.require(type(item['event']) is str and not any(k in item for k in ('id', 'return', 'error')),
                         'QMP event differs')
        else:
            ident = item.get('id')
            host.require(type(ident) is int and ident in requests and ident not in responses and
                         ('return' in item) != ('error' in item), 'QMP reply identity differs')
            responses[ident] = item
    required = ('query-status', 'query-block', 'query-pci', 'query-cpus-fast')
    host.require(greeting == 1 and commands[:5] == ['qmp_capabilities', *required] and
                 commands[5:] in ([], ['quit']) and
                 all(commands.count(name) == 1 for name in ('qmp_capabilities', *required)),
                 'QMP capability or topology command sequence differs')
    capability = next(key for key, value in requests.items() if value == 'qmp_capabilities')
    host.require(capability in responses and responses[capability].get('return') == {},
                 'QMP capability negotiation did not succeed')
    initial = attempt.get('qmp_initial')
    host.require(type(initial) is dict and set(initial) == set(required), 'QMP topology receipt missing')
    for name in required:
        ident = next(key for key, value in requests.items() if value == name)
        host.require(ident in responses and 'return' in responses[ident] and responses[ident]['return'] == initial[name],
                     'QMP topology reply differs from raw transcript')
    status, blocks, pci, cpus = (initial[name] for name in required)
    host.require(type(status) is dict and status.get('running') is True and status.get('status') == 'running' and
                 type(cpus) is list and len(cpus) == 2 and
                 {item.get('cpu-index') for item in cpus if type(item) is dict} == {0, 1} and
                 all(type(item.get('thread-id')) is int and item['thread-id'] > 0 for item in cpus),
                 'QEMU did not report a running two-CPU guest')
    expected_ids = {'bootdisk', 'cidatadisk', 'seeddisk', 'g2-result-controller'} | {
        name + 'disk' for name, _ in source_vm.DISKS}
    host.require(type(pci) is list, 'QMP PCI topology missing')
    found = []
    def inspect_pci(value):
        if type(value) is dict:
            if 'qdev_id' in value:
                ident = value['qdev_id']
                host.require(type(ident) is str, 'QMP PCI identity malformed')
                if ident:
                    found.append(ident)
                kind = value.get('class_info', {}).get('class')
                host.require(type(kind) is not int or kind >> 8 != 2, 'unexpected QEMU network PCI device')
            for nested in value.values():
                inspect_pci(nested)
        elif type(value) is list:
            for nested in value:
                inspect_pci(nested)
    inspect_pci(pci)
    host.require(set(found) == expected_ids and len(found) == len(expected_ids), 'QMP PCI devices differ')
    host.require(type(blocks) is list, 'QMP block topology missing')
    attached = {}
    for block in blocks:
        host.require(type(block) is dict, 'QMP block entry malformed')
        if not block.get('inserted'):
            continue
        qdev = block.get('qdev')
        host.require(type(qdev) is str and qdev.startswith('/machine/peripheral/') and qdev.endswith('/virtio-backend'),
                     'unregistered QEMU block device')
        name = qdev.removeprefix('/machine/peripheral/').removesuffix('/virtio-backend')
        host.require(name not in attached and type(block['inserted']) is dict, 'duplicate QEMU block device')
        attached[name] = block['inserted']
    wanted = {'bootdisk': ('boot.qcow2', 'qcow2', False), 'seeddisk': ('seed.raw', 'raw', True),
              'cidatadisk': ('cidata.raw', 'raw', True)}
    wanted.update({name + 'disk': (name + '.raw', 'raw', False) for name, _ in source_vm.DISKS})
    host.require(set(attached) == set(wanted), 'QEMU block devices differ')
    for name, (filename, driver, readonly) in wanted.items():
        item = attached[name]
        host.require(item.get('file') == str(directory / filename) and item.get('drv') == driver and
                     item.get('ro') is readonly, 'QEMU block path, driver or read-only mode differs')
    return commands


def _resource_contract(host, directory, state, build, case, scenario, variant):
    """Reconstruct the v2 resource contract independently of the runner."""
    q = 268435456
    reserve, growth, allowance = 1073741824, 4294967296, 1048576
    cidata, virtual = 528482304, 51539607552
    seed = build['seed_inputs']
    host.require(type(seed) is dict and all(type(name) is str and name not in ('.', '..', 'case.json', 'SHA256SUMS') and
                 re.fullmatch(r'[A-Za-z0-9_.-]+', name) and type(digest) is str and
                 HEX.fullmatch(digest) for name, digest in seed.items()) and
                 {'user-data', 'network-config'} <= set(seed), 'invalid selected seed inputs')
    selected = Path(build['_attempt_dir']) / 'seed'
    host.check_path(selected)
    host.require({p.name for p in selected.iterdir()} == set(seed), 'selected seed names changed')
    names = sorted(set(seed) - {'user-data', 'network-config'})
    total = 0
    for name in sorted(seed):
        path = selected / name
        host.check_path(path, False)
        info = os.stat(path, follow_symlinks=False)
        host.require(info.st_uid == os.geteuid() and info.st_nlink == 1 and
                     host.digest(path)[0] == seed[name], 'selected seed identity or hash changed')
        if name in names:
            payload = directory / 'payload' / name
            host.require(os.stat(payload, follow_symlinks=False).st_size == info.st_size,
                         'selected seed and payload sizes differ')
            total += info.st_size
    case_value = {'version': 1, 'run_token': RUN_TOKEN, 'case': case, 'scenario': scenario,
                  'bpf_variant': variant, 'qemu_sha256': build['qemu_binaries']['qemu-system-x86_64'],
                  'boot_inputs': build['boot_inputs'],
                  'bpf_bootstrap': build['bpf_authority']['bootstrap_manifest']}
    case_bytes = host.canonical(case_value)
    host.require(len(case_bytes) <= 4096 and (directory / 'payload' / 'case.json').read_bytes() == case_bytes,
                 'resource case identity differs')
    manifest_length = sum(64 + 2 + len(name.encode('ascii')) + 1 for name in names + ['case.json'])
    payload_bytes = total + len(case_bytes) + manifest_length
    capacity = max(2 * 1024 ** 3, (((5 * payload_bytes + 3) // 4 + q + q - 1) // q) * q)
    staging = ((payload_bytes + capacity + cidata + q + q - 1) // q) * q
    host.require(capacity <= 8 * 1024 ** 3 and
                 sum(os.stat(directory / 'payload' / name, follow_symlinks=False).st_size
                     for name in names + ['case.json', 'SHA256SUMS']) == payload_bytes and
                 state['seed_image']['size'] == capacity, 'resource payload or seed capacity differs')
    expected = {'version': 1, 'policy': 'houfeng.source-vm.operational.v2',
                'seed_inputs_sha256': hashlib.sha256(host.canonical(seed)).hexdigest(),
                'payload_bytes': payload_bytes, 'seed_capacity_bytes': capacity,
                'cidata_capacity_bytes': cidata, 'writable_virtual_bytes': virtual,
                'staging_limit_bytes': staging, 'growth_limit_bytes': growth,
                'reserve_bytes': reserve, 'final_receipt_allowance_bytes': allowance}
    budget = state.get('resource_budget')
    host.require(type(budget) is dict and set(budget) == set(expected) and
                 all(type(budget[k]) is type(v) and budget[k] == v for k, v in expected.items()),
                 'resource budget differs')
    def integer(value):
        return type(value) is int and 0 <= value <= 2 ** 63 - 1

    usage_keys = {'version', 'baseline_allocated_bytes', 'peak_allocated_bytes', 'final_allocated_bytes',
                  'minimum_disk_available_bytes', 'sample_count', 'peak_growth_bytes', 'abort'}
    def usage(value):
        host.require(type(value) is dict and set(value) == usage_keys and
                     type(value['version']) is int and value['version'] == 1 and
                     all(integer(value[k]) for k in usage_keys - {'version', 'abort'}) and
                     value['sample_count'] > 0 and value['abort'] is None and
                     value['peak_allocated_bytes'] >= value['baseline_allocated_bytes'] and
                     value['final_allocated_bytes'] <= value['peak_allocated_bytes'] and
                     value['peak_growth_bytes'] == value['peak_allocated_bytes'] - value['baseline_allocated_bytes'] and
                     value['peak_growth_bytes'] <= growth and value['minimum_disk_available_bytes'] >= reserve,
                     'resource usage differs')
    first, final = state.get('initial_resource_usage'), state.get('resource_usage')
    usage(first)
    usage(final)
    host.require(final['baseline_allocated_bytes'] == first['baseline_allocated_bytes'] and
                 final['sample_count'] > first['sample_count'] and
                 final['peak_allocated_bytes'] >= first['peak_allocated_bytes'] and
                 final['minimum_disk_available_bytes'] <= first['minimum_disk_available_bytes'] and
                 final['peak_growth_bytes'] + allowance <= growth, 'resource usage is not monotonic')
    obs_keys = {'phase', 'disk_available_bytes', 'mem_available_bytes', 'kvm_api_version',
                'required_disk_bytes', 'observed_growth_bytes'}
    def observation(value, phase, required, observed):
        host.require(type(value) is dict and set(value) == obs_keys and value['phase'] == phase and
                     integer(value['disk_available_bytes']) and integer(value['required_disk_bytes']) and
                     integer(value['observed_growth_bytes']) and value['required_disk_bytes'] == required and
                     value['disk_available_bytes'] >= required and value['observed_growth_bytes'] == observed and
                     ((value['mem_available_bytes'] is None and value['kvm_api_version'] is None) if phase == 'finalize'
                      else (integer(value['mem_available_bytes']) and value['mem_available_bytes'] >= 4 * 1024 ** 3 and
                            type(value['kvm_api_version']) is int and value['kvm_api_version'] == 12)),
                     'resource observation differs')
    observation(state.get('resources'), 'prepare', staging + growth + reserve, 0)
    observation(state.get('launch_resources'), 'launch', growth + reserve, 0)
    observation(state.get('final_resources'), 'finalize', reserve + allowance, final['peak_growth_bytes'])
    host.require(all(first['minimum_disk_available_bytes'] <= state[key]['disk_available_bytes']
                     for key in ('resources', 'launch_resources')) and
                 final['minimum_disk_available_bytes'] <= state['final_resources']['disk_available_bytes'],
                 'resource minimum conflicts with observations')
    if case == 'source-recovery' and scenario == 'vm-kill':
        reboot = state.get('reboot_resources')
        host.require(type(reboot) is dict and integer(reboot.get('observed_growth_bytes')),
                     'reboot resource observation missing')
        cut = reboot['observed_growth_bytes']
        host.require(first['peak_growth_bytes'] <= cut <= final['peak_growth_bytes'] and cut + allowance <= growth,
                     'reboot resource cut differs')
        observation(reboot, 'reboot', max(0, growth - cut) + reserve, cut)
        host.require(final['minimum_disk_available_bytes'] <= reboot['disk_available_bytes'],
                     'resource minimum conflicts with reboot')
    else:
        host.require('reboot_resources' not in state, 'unexpected reboot resource observation')


def _verify_single_vm_dir(host, frozen, directory, case, scenario, build, variant=None, expected_manifest_sha=None):
    source_retirement.assert_available(host, directory)
    receipt = directory / 'receipt.json'
    if not os.path.lexists(receipt):
        if any(os.path.lexists(directory / name) for name in ('failed-result.json', 'reboot-failed-result.json')):
            import source_settlement_evidence
            summary = source_settlement_evidence.summarize_evidence(None)
            summary['quality'] = 'INVALID'
            return {'case': case, 'scenario': scenario, 'variant': variant, 'status': 'FAIL',
                    'settlement_evidence': summary, 'verification_limits': []}
        status = 'FAIL' if 'source_resources.py' in build['runner_inputs'] and directory.exists() else 'NOT_RUN'
        return {'case': case, 'scenario': scenario, 'variant': variant, 'status': status}
    def historical_failure():
        import source_settlement_evidence
        summary = source_settlement_evidence.summarize_evidence(None)
        if any(os.path.lexists(directory / name) for name in ('failed-result.json', 'reboot-failed-result.json')):
            summary['quality'] = 'INVALID'
        return {'case': case, 'scenario': scenario, 'variant': variant, 'status': 'FAIL',
                'settlement_evidence': summary, 'verification_limits': []}
    if 'source_resources.py' not in build['runner_inputs']:
        host.check_path(receipt, False)
        state = host.strict_json(receipt.read_bytes())
        if type(state) is dict and state.get('status') == 'FAIL':
            return historical_failure()
        return _verify_single_vm_state(host, frozen, directory, case, scenario, build,
                                       state, variant, expected_manifest_sha)
    def verify_receipt(state):
        if state.get('status') == 'FAIL':
            if 'source_settlement_evidence.py' in build['runner_inputs']:
                return _verify_single_vm_failure_state(host, frozen, directory, case, scenario, build, state,
                                                       variant, expected_manifest_sha)
            return historical_failure()
        return _verify_single_vm_state(host, frozen, directory, case, scenario, build, state,
                                       variant, expected_manifest_sha)
    return _verify_durable_receipt(host, directory, verify_receipt)


def _verify_durable_receipt(host, directory, validate):
    host.check_path(directory)
    dfd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        def directory_identity():
            held = os.fstat(dfd)
            named = os.stat(directory, follow_symlinks=False)
            host.require(stat.S_ISDIR(named.st_mode) and held.st_uid == os.geteuid() and
                         stat.S_IMODE(held.st_mode) == 0o700 and
                         (held.st_dev, held.st_ino) == (named.st_dev, named.st_ino),
                         'case directory identity changed')
        def receipt_bytes():
            fd = os.open('receipt.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=dfd)
            try:
                before = os.fstat(fd)
                host.require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and
                             before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
                             0 <= before.st_size <= 1048576, 'unsafe case receipt')
                parts = []
                remaining = before.st_size
                while remaining:
                    part = os.read(fd, min(remaining, 65536))
                    host.require(bool(part), 'short case receipt')
                    parts.append(part)
                    remaining -= len(part)
                after = os.fstat(fd)
                named = os.stat('receipt.json', dir_fd=dfd, follow_symlinks=False)
                identity = lambda x: (x.st_dev, x.st_ino, x.st_uid, x.st_mode, x.st_nlink,
                                      x.st_size, x.st_mtime_ns, x.st_ctime_ns)
                host.require(identity(before) == identity(after) == identity(named), 'case receipt identity changed')
                return identity(before), b''.join(parts)
            finally:
                os.close(fd)
        directory_identity()
        identity, raw = receipt_bytes()
        result = validate(host.strict_json(raw))
        directory_identity()
        os.fsync(dfd)
        directory_identity()
        identity_after, raw_after = receipt_bytes()
        host.require(identity_after == identity and hashlib.sha256(raw_after).digest() == hashlib.sha256(raw).digest(),
                     'case receipt changed during durable verification')
        return result
    finally:
        os.close(dfd)


def _legacy_resources(host, state, case, scenario):
    gates = [state.get('resources')]
    if case == 'source-recovery' and scenario == 'vm-kill':
        gates.append(state.get('reboot_resources'))
    host.require(all(type(gate) is dict and type(gate.get('disk_available_bytes')) is int and
                     gate['disk_available_bytes'] >= 64 * 1024 ** 3 and
                     type(gate.get('mem_available_bytes')) is int and gate['mem_available_bytes'] >= 4 * 1024 ** 3 and
                     gate.get('kvm_api_version') == 12 for gate in gates), 'VM resource gate evidence differs')


def _read_reboot_result(host, directory, state, parser):
    """Authenticate exactly the buffers consumed by the reboot parser."""
    host.require(type(state) is dict and type(state.get('reboot_attempt')) is dict and
                 type(state.get('reboot_parsed_sha256')) is str and
                 HEX.fullmatch(state['reboot_parsed_sha256']) is not None,
                 'invalid reboot evidence receipt')
    attempt = state['reboot_attempt']
    host.require(type(attempt.get('protocol_sha256')) is str and HEX.fullmatch(attempt['protocol_sha256']) is not None and
                 type(attempt.get('protocol_identity')) is list and len(attempt['protocol_identity']) == 4 and
                 all(type(item) is int for item in attempt['protocol_identity']), 'invalid reboot protocol identity')
    def read_file(name, limit, expected_hash, identity=None):
        path = directory / name
        fd = None
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            before = os.fstat(fd)
            named = os.stat(path, follow_symlinks=False)
            def metadata(info):
                return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
                        info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
            host.require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and
                         before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
                         0 <= before.st_size <= limit and metadata(before) == metadata(named),
                         'unsafe reboot evidence file')
            if identity is not None:
                host.require([before.st_dev, before.st_ino, before.st_uid, stat.S_IMODE(before.st_mode)] == identity,
                             'reboot protocol identity differs')
            blocks = []
            remaining = before.st_size
            while remaining:
                block = os.read(fd, min(remaining, 1024 * 1024))
                host.require(bool(block), 'short reboot evidence read')
                blocks.append(block)
                remaining -= len(block)
            data = b''.join(blocks)
            host.require(len(data) == before.st_size and metadata(os.fstat(fd)) == metadata(before) and
                         metadata(os.stat(path, follow_symlinks=False)) == metadata(before) and
                         hashlib.sha256(data).hexdigest() == expected_hash, 'reboot evidence changed')
            return data
        except OSError as exc:
            raise host.Failure('cannot read reboot evidence') from exc
        finally:
            if fd is not None:
                os.close(fd)
    protocol = read_file('reboot-protocol.log', source_vm.MAX_PROTOCOL, attempt['protocol_sha256'],
                         attempt['protocol_identity'])
    parsed = read_file('reboot-parsed.json', source_vm.MAX_RESULT_BYTES + 1, state['reboot_parsed_sha256'])
    result = parser(protocol, True)['result']
    decoded = host.strict_json(parsed)
    host.require(type(decoded) is dict and parsed == host.canonical(decoded) and decoded == result,
                 'reboot transcript and parsed result differ')
    return decoded


def _verify_single_vm_failure_state(host, frozen, directory, case, scenario, build, state, variant=None, expected_manifest_sha=None):
    import source_settlement_evidence
    result = {'case': case, 'scenario': scenario, 'variant': variant, 'status': 'FAIL',
              'settlement_evidence': source_settlement_evidence.summarize_evidence(None), 'verification_limits': []}
    attempts = [state.get('attempt', {}), state.get('reboot_attempt', {})]
    present = [attempt for attempt in attempts if type(attempt) is dict and 'failed_result' in attempt]
    if not present:
        if any(os.path.lexists(directory / name) for name in ('failed-result.json', 'reboot-failed-result.json')):
            result['settlement_evidence']['quality'] = 'INVALID'
        return result
    try:
        host.require(len(present) == 1 and type(state.get('version')) is int and state['version'] == 2 and
                     state.get('status') == 'FAIL' and state.get('run_token') == RUN_TOKEN and
                     state.get('case') == case and state.get('scenario') == scenario and
                     state.get('bpf_variant') == variant and state.get('build_selection') == build['_selection'] and
                     state.get('browser_selection') == build['browser_selection'] and
                     (expected_manifest_sha is None or state.get('launch_manifest_sha256') == expected_manifest_sha),
                     'failed receipt binding differs')
        attempt = present[0]
        reboot = attempt is attempts[1]
        host.require(not reboot or (case, scenario) == ('source-recovery', 'vm-kill'),
                     'unexpected reboot failure artifact')
        prefix = 'reboot-' if reboot else ''
        artifact = attempt['failed_result']
        host.require(type(artifact) is dict and set(artifact) == {'version', 'path', 'sha256', 'bytes'} and
                     type(artifact['version']) is int and artifact['version'] == 1 and
                     artifact['path'] == prefix + 'failed-result.json' and type(artifact['bytes']) is int and
                     0 <= artifact['bytes'] <= source_vm.MAX_RESULT_BYTES and
                     type(artifact['sha256']) is str and HEX.fullmatch(artifact['sha256']),
                     'failed artifact descriptor differs')
        host.require(attempt.get('ready_while_alive') is True and
                     attempt.get('argv') == source_vm.expected_vm_argv(host, directory, state['boot_fds'],
                         reboot=reboot, variant=None if reboot else variant, case_sha256=state['seed']['case.json']),
                     'failed VM launch differs')
        source_vm._verify_payload(host, directory / 'payload', state['seed'])
        source_vm._verify_cidata(host, directory / 'cidata', state['cidata'])
        expected_inputs = {name: digest for name, digest in build['seed_inputs'].items()
                           if name not in ('user-data', 'network-config')}
        host.require(set(state['seed']) == set(expected_inputs) | {'case.json', 'SHA256SUMS'} and
                     all(state['seed'][name] == digest for name, digest in expected_inputs.items()) and
                     set(state['cidata']) == {'meta-data', 'user-data', 'network-config', 'seed-descriptor.json'} and
                     all(state['cidata'][name] == build['seed_inputs'][name] for name in ('user-data', 'network-config')),
                     'failed seed or CIDATA does not match selected build')
        host.require(state['host_launch']['build_selection'] == build['_selection'] and
                     state['host_launch_sha256'] == hashlib.sha256(host.canonical(state['host_launch'])).hexdigest(),
                     'failed host launch differs')
        launch, image = state['host_launch'], state['seed_image']
        template = {name: state['cidata'][name] for name in ('meta-data', 'network-config', 'user-data')}
        expected_case = source_vm._case_bytes(host, build, case, scenario, variant)
        host.require((directory / 'payload' / 'case.json').read_bytes() == expected_case and
                     state['seed']['case.json'] == hashlib.sha256(expected_case).hexdigest() and
                     launch.get('payload_case_sha256') == state['seed']['case.json'] and
                     launch.get('seed_inputs_sha256') == hashlib.sha256(host.canonical(build['seed_inputs'])).hexdigest() and
                     launch.get('qemu_sha256') == build['qemu_binaries']['qemu-system-x86_64'] and
                     launch.get('argv_sha256') == hashlib.sha256(host.canonical(
                         source_vm.expected_vm_argv(host, directory, state['boot_fds'], variant=variant,
                                                    case_sha256=state['seed']['case.json']))).hexdigest() and
                     launch.get('boot_inputs') == build['boot_inputs'] and launch.get('seed_image') == image and
                     launch.get('cidata_template_sha256') == hashlib.sha256(host.canonical(template)).hexdigest() and
                     state.get('cidata_sha256') == hashlib.sha256(host.canonical(state['cidata'])).hexdigest() and
                     source_vm._cidata_identity(directory / 'cidata', state['cidata']) == state.get('cidata_identity'),
                     'failed case, build or CIDATA binding differs')
        descriptor = {'version': 1, 'run_token': RUN_TOKEN, 'case': case, 'scenario': scenario,
                      'payload_case_sha256': state['seed']['case.json'],
                      'payload_manifest_sha256': state['seed']['SHA256SUMS'],
                      'seed_uuid': image['uuid'], 'seed_size': image['size'],
                      'host_launch_sha256': state['host_launch_sha256'],
                      'host_argv_sha256': launch['argv_sha256'], 'qemu_sha256': launch['qemu_sha256']}
        host.require((directory / 'cidata' / 'seed-descriptor.json').read_bytes() == host.canonical(descriptor),
                     'failed CIDATA seed descriptor differs')
        def read_file(name, limit, digest, expected_size, identity=None):
            path = directory / name
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                before, named = os.fstat(fd), os.stat(path, follow_symlinks=False)
                def metadata(info):
                    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
                            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
                host.require(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and
                             before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
                             type(expected_size) is int and before.st_size == expected_size and
                             0 <= before.st_size <= limit and metadata(before) == metadata(named),
                             'unsafe failed evidence')
                if identity is not None:
                    host.require(identity == [before.st_dev, before.st_ino, before.st_uid, stat.S_IMODE(before.st_mode)],
                                 'failed capture identity differs')
                data = bytearray()
                while len(data) < before.st_size:
                    block = os.read(fd, min(1048576, before.st_size - len(data)))
                    host.require(bool(block), 'short failed evidence')
                    data.extend(block)
                host.require(metadata(os.fstat(fd)) == metadata(before) and
                             metadata(os.stat(path, follow_symlinks=False)) == metadata(before) and
                             hashlib.sha256(data).hexdigest() == digest, 'failed evidence changed')
                return bytes(data)
            finally:
                os.close(fd)
        protocol = read_file(prefix + 'protocol.log', source_vm.MAX_PROTOCOL, attempt['protocol_sha256'],
                             attempt['protocol_bytes'], attempt['protocol_identity'])
        read_file(prefix + 'serial.log', source_vm.MAX_PROTOCOL, attempt['serial_sha256'],
                  attempt['serial_bytes'], attempt['serial_identity'])
        qmp_bytes = read_file(prefix + 'qmp.log', source_vm.MAX_PROTOCOL, attempt['qmp_sha256'],
                              attempt['qmp_bytes'], attempt['qmp_identity'])
        data = read_file(artifact['path'], source_vm.MAX_RESULT_BYTES, artifact['sha256'], artifact['bytes'])
        terminal = source_vm._parser(host, frozen, case, scenario, state['seed'], build, state['cidata'])(protocol, True)['result']
        host.require(terminal.get('completed') is False and host.canonical(terminal) == data,
                     'failed terminal and artifact differ')
        import source_bpf_verify
        source_bpf_verify.require_v5_timestamps(host, terminal)
        result['settlement_evidence'] = source_settlement_evidence.summarize_evidence(terminal.get('settlement_evidence_v1'))
        checks = state.get('failure_checks', {'exit': 'UNKNOWN', 'carriers': 'UNKNOWN', 'resources': 'UNKNOWN'})
        host.require(type(checks) is dict and set(checks) == {'exit', 'carriers', 'resources'} and
                     all(v in ('PASS', 'FAIL', 'UNKNOWN') for v in checks.values()), 'failure checks invalid')
        if checks['exit'] != 'PASS' or attempt.get('pidfd_held') is not True or attempt.get('exit_observed') is not True:
            result['verification_limits'].append('EXIT')
        if reboot:
            original = state.get('attempt')
            host.require(type(original) is dict, 'original VM attempt invalid')
            if (original.get('pidfd_held') is not True or original.get('exit_observed') is not True or
                    original.get('kill_signal') != 'SIGKILL via held pidfd' or original.get('returncode') != -9):
                if 'EXIT' not in result['verification_limits']:
                    result['verification_limits'].append('EXIT')
        stages = ['before_launch', 'after_wait']
        if reboot:
            stages.extend(('before_reboot', 'after_reboot_wait'))
        boot_observations = state.get('boot_observations')
        boot_complete = False
        if boot_observations is not None:
            host.require(type(boot_observations) is list and all(type(item) is dict for item in boot_observations),
                         'failed boot observations invalid')
            observed_stages = [item.get('stage') for item in boot_observations]
            host.require(observed_stages == stages[:len(observed_stages)], 'failed boot observation order differs')
            if observed_stages:
                _boot_inputs(host, frozen, state, build, failure_stages=observed_stages)
            boot_complete = len(observed_stages) == len(stages)
        carrier_complete = _carrier_observations(host, state, stages, partial=True)
        _failure_raw_carriers(host, directory, state)
        raw_complete = all(os.path.lexists(directory / name) for name in ('seed.raw', 'cidata.raw'))
        base_complete = False
        base_inputs = build.get('boot_inputs', {})
        if ('base_before' in state or 'base_sha256' in state['host_launch'] or
                type(base_inputs) is dict and 'base_sha256' in base_inputs):
            base = Path(frozen.load_json(host.RUN / 'prepare.json')['assets']['ubuntu:' + frozen.IMAGE])
            pin = frozen.UBUNTU_HASHES[frozen.IMAGE]
            host.require(all(obj[key] == pin for obj, key in ((state, 'base_before'), (state, 'base_after'),
                         (state['host_launch'], 'base_sha256'), (base_inputs, 'base_sha256'))
                         if key in obj), 'failure boot base differs')
            if os.path.lexists(base):
                host.check_path(base, False)
                host.require(host.digest(base)[0] == pin, 'failure boot base changed')
                base_complete = 'base_before' in state and 'base_sha256' in state['host_launch']
        if checks['carriers'] != 'PASS' or not (boot_complete and carrier_complete and raw_complete and base_complete):
            result['verification_limits'].append('CARRIERS')
        usage, budget = state.get('resource_usage'), state.get('resource_budget')
        host.require(('resource_usage' not in state or type(usage) is dict) and
                     ('resource_budget' not in state or type(budget) is dict), 'failure resource record malformed')
        if type(budget) is dict:
            import source_resources
            expected_budget = source_resources.make_budget(host, build,
                source_vm._case_bytes(host, build, case, scenario, variant))
            host.require(budget == expected_budget, 'failure resource budget differs')
        if type(usage) is dict:
            keys = {'version', 'baseline_allocated_bytes', 'peak_allocated_bytes', 'final_allocated_bytes',
                    'minimum_disk_available_bytes', 'sample_count', 'peak_growth_bytes', 'abort'}
            host.require(set(usage) == keys and type(usage['version']) is int and usage['version'] == 1 and
                         all(type(usage[k]) is int and 0 <= usage[k] <= (1 << 63) - 1
                             for k in keys - {'version', 'abort'}) and
                         usage['peak_allocated_bytes'] >= usage['baseline_allocated_bytes'] and
                         usage['peak_growth_bytes'] == usage['peak_allocated_bytes'] - usage['baseline_allocated_bytes'] and
                         usage['final_allocated_bytes'] <= usage['peak_allocated_bytes'] and
                         usage['abort'] == state.get('resource_abort'), 'failure resource usage differs')
            if usage['abort'] is not None:
                import source_resources
                host.require(type(usage['abort']) is dict and set(usage['abort']) == {'reason', 'phase'} and
                             usage['abort']['reason'] in source_resources._REASONS and
                             usage['abort']['phase'] in source_resources._PHASES, 'failure resource abort differs')
            else:
                host.require(usage['peak_growth_bytes'] <= 4294967296 and
                             usage['minimum_disk_available_bytes'] >= 1073741824, 'failure resource limit exceeded')
        if checks['resources'] != 'PASS' or type(usage) is not dict or type(budget) is not dict:
            result['verification_limits'].append('RESOURCES')
        if attempt.get('qmp_initial') is None:
            result['verification_limits'].append('QMP')
        else:
            _qmp(host, directory / (prefix + 'qmp.log'), attempt, directory, captured=qmp_bytes)
        if result['verification_limits'] and result['settlement_evidence']['quality'] == 'COMPLETE':
            result['settlement_evidence']['quality'] = 'PARTIAL'
    except (OSError, ValueError, KeyError, TypeError, RecursionError, host.Failure):
        result['settlement_evidence']['quality'] = 'INVALID'
        result['verification_limits'] = []
    return result


def _verify_single_vm_state(host, frozen, directory, case, scenario, build, state, variant=None, expected_manifest_sha=None):
    reboot_result = None
    host.require(type(state) is dict and 'reboot_result' not in state, 'receipt contains reboot result')
    host.require(all('failed_result' not in state.get(key, {}) for key in ('attempt', 'reboot_attempt')),
                 'successful receipt contains failed result')
    current = 'source_resources.py' in build['runner_inputs']
    host.require(type(state) is dict and type(state.get('version')) is int and
                 state.get('version') == (2 if current else 1) and state.get('run_token') == RUN_TOKEN and
                 state.get('case') == case and state.get('scenario') == scenario and
                 state.get('bpf_variant') == variant and
                 (expected_manifest_sha is None or state.get('launch_manifest_sha256') == expected_manifest_sha) and
                 state.get('status') == 'PASS' and
                 state.get('stage') == 'complete' and state.get('seed_unchanged') is True and
                 state.get('build_selection') == build['_selection'] and
                 state.get('browser_selection') == build['browser_selection'] and
                 state.get('host_launch', {}).get('build_selection') == build['_selection'] and
                 state.get('host_launch_sha256') == hashlib.sha256(host.canonical(state['host_launch'])).hexdigest(),
                 'case receipt is not a completed success')
    _carrier_files(host, frozen, directory, state, build)
    if current:
        _resource_contract(host, directory, state, build, case, scenario, variant)
    else:
        _legacy_resources(host, state, case, scenario)
    attempt = state['attempt']
    early_mode = variant in source_vm.EARLY_SEAL_VARIANTS
    host.require(attempt.get('exit_observed') is True and attempt.get('pidfd_held') is True and
                 attempt.get('ready_while_alive') is (not early_mode) and
                 (not early_mode or attempt.get('early_terminal_while_alive') is True) and
                 attempt.get('returncode') ==
                 (-9 if case == 'source-recovery' and scenario == 'vm-kill' else 0) and
                 attempt.get('argv') == source_vm.expected_vm_argv(
                     host, directory, state['boot_fds'], variant=variant, case_sha256=state['seed']['case.json']),
                 'QEMU ownership or exact launch differs')
    for name, key in (('protocol.log', 'protocol_sha256'), ('serial.log', 'serial_sha256'), ('qmp.log', 'qmp_sha256')):
        host.require(host.digest(directory / name)[0] == attempt[key], 'runtime evidence changed')
    commands = _qmp(host, directory / 'qmp.log', attempt, directory)
    parsed = directory / 'parsed.json'
    host.require(host.digest(parsed)[0] == state['parsed_sha256'], 'parsed guest result changed')
    if early_mode:
        host.require((directory / 'protocol.log').stat().st_size == 0, 'early seal-gap member wrote protocol.log')
        result = source_vm.parse_early_serial_stream(
            host, (directory / 'serial.log').read_bytes(), variant, state['seed']['case.json'], True)['result']
        host.require(result == host.strict_json(parsed.read_bytes()), 'early serial stream and parsed result differ')
        validate_early_seal_result(host, case, scenario, variant, result, state, build)
    else:
        parser = source_vm._parser(host, frozen, case, scenario, state['seed'], build, state['cidata'])
        result = parser((directory / 'protocol.log').read_bytes(), True)['result']
        host.require(result == host.strict_json(parsed.read_bytes()), 'guest transcript and parsed result differ')
        if case == 'source-recovery' and scenario == 'vm-kill':
            reboot_attempt = state['reboot_attempt']
            host.require(reboot_attempt.get('exit_observed') is True and reboot_attempt.get('pidfd_held') is True and
                         reboot_attempt.get('ready_while_alive') is True and reboot_attempt.get('returncode') == 0 and
                         reboot_attempt.get('argv') == source_vm.expected_vm_argv(host, directory, state['boot_fds'], reboot=True) and
                         attempt.get('kill_ready_sha256') == parser((directory / 'protocol.log').read_bytes(), False)['end'],
                         'original kill or reboot QEMU evidence differs')
            for name, key in (('reboot-serial.log', 'serial_sha256'), ('reboot-qmp.log', 'qmp_sha256')):
                host.require(host.digest(directory / name)[0] == reboot_attempt[key], 'reboot runtime evidence changed')
            _qmp(host, directory / 'reboot-qmp.log', reboot_attempt, directory)
            reboot_result = _read_reboot_result(host, directory, state, parser)
            disks = state['reboot_disks']
            host.require(disks['boot'] == {key: state['host_launch']['boot'][key] for key in ('dev', 'ino')},
                         'reboot boot overlay differs')
            for name, _ in source_vm.DISKS:
                expected = {key: state['disks_before'][name][key] for key in ('dev', 'ino', 'size')}
                info = os.stat(directory / (name + '.raw'), follow_symlinks=False)
                host.require(disks[name] == expected and (info.st_dev, info.st_ino, info.st_size) ==
                             (expected['dev'], expected['ino'], expected['size']), 'reboot raw volume differs')
            boot_info = os.stat(directory / 'boot.qcow2', follow_symlinks=False)
            host.require((boot_info.st_dev, boot_info.st_ino) == (disks['boot']['dev'], disks['boot']['ino']),
                         'reboot boot overlay was replaced')
        validate_result(host, case, scenario, result, state, build, reboot_result=reboot_result)
    import source_settlement_evidence
    summary = source_settlement_evidence.summarize_evidence(result.get('settlement_evidence_v1'))
    return {'case': case, 'scenario': scenario, 'variant': variant, 'status': 'PASS', 'qmp_commands': commands,
            'settlement_evidence': summary}


def _case(host, frozen, case, scenario, build):
    root = host.RUN / 'runs' / ('selection-' + build['_selection']['selection_id']) / case
    if not root.exists():
        return {'case': case, 'scenario': scenario, 'status': 'NOT_RUN'}
    try:
        attempts = source_build._numbered(host, root, 'attempt-', '')
        if not attempts:
            return {'case': case, 'scenario': scenario, 'status': 'NOT_RUN'}
        # A case can be run one scenario at a time. Only a newer manifest that
        # actually declares this scenario supersedes its previous result.
        for attempt in reversed(attempts):
            attempt_id = '%020d' % attempt
            attempt_dir = root / ('attempt-' + attempt_id)
            manifest_path = attempt_dir / 'launch-manifest.json'
            host.check_path(manifest_path, False)
            manifest_raw = manifest_path.read_bytes()
            manifest = host.strict_json(manifest_raw)
            host.require(type(manifest) is dict and
                         set(manifest) == {'version', 'run_token', 'build_selection', 'attempt',
                                           'case', 'scenarios', 'members'} and
                         host.canonical(manifest) == manifest_raw and
                         type(manifest['version']) is int and manifest['version'] == 1 and
                         manifest['run_token'] == RUN_TOKEN and manifest['case'] == case and
                         manifest['attempt'] == attempt_id and
                         host.canonical(manifest['build_selection']) == host.canonical(build['_selection']),
                         'launch-manifest.json invalid')
            declared = manifest['scenarios']
            allowed = scenarios(case)
            host.require(type(declared) is list and len(declared) > 0 and
                         all(type(item) is str and item in allowed for item in declared) and
                         declared == sorted(set(declared)) and
                         (len(declared) == 1 or declared == sorted(allowed)),
                         'launch-manifest.json scenarios invalid')
            expected_members = []
            for declared_scenario in declared:
                if case == 'realm-negative' and declared_scenario == 'bpf-authorization':
                    expected_members.extend({'case': m_case, 'scenario': m_scenario, 'variant': m_variant}
                                            for m_case, m_scenario, m_variant in source_vm.BPF_AUTH_FAMILY_MEMBERS)
                else:
                    expected_members.append({'case': case, 'scenario': declared_scenario, 'variant': None})
            host.require(type(manifest['members']) is list and manifest['members'] == expected_members,
                         'launch-manifest.json members invalid')
            if scenario in declared:
                manifest_sha = hashlib.sha256(manifest_raw).hexdigest()
                break
        else:
            return {'case': case, 'scenario': scenario, 'status': 'NOT_RUN'}
        if case == 'realm-negative' and scenario == 'bpf-authorization':
            family_dir = attempt_dir / 'bpf-authorization'
            source_retirement.assert_available(host, family_dir)
            family_path = family_dir / 'family.json'
            if not os.path.lexists(family_path):
                status = 'FAIL' if 'source_resources.py' in build['runner_inputs'] and family_dir.exists() else 'NOT_RUN'
                return {'case': case, 'scenario': scenario, 'status': status}
            host.check_path(family_path, False)
            family_raw = family_path.read_bytes()
            host.require(len(family_raw) <= 16384, 'family.json exceeds 16KiB limit')
            family = host.strict_json(family_raw)
            current = 'source_resources.py' in build['runner_inputs']
            family_keys = {'version', 'case', 'scenario', 'run_token', 'build_selection',
                           'launch_manifest_sha256', 'members'}
            if current:
                family_keys |= {'stop_reason', 'retained_allocated_bytes'}
            host.require(host.canonical(family) == family_raw and type(family) is dict and
                         set(family) == family_keys and type(family['version']) is int and
                         family['version'] == (2 if current else 1) and
                         family['case'] == case and family['scenario'] == scenario and family['run_token'] == RUN_TOKEN and
                         host.canonical(family['build_selection']) == host.canonical(build['_selection']) and
                         family['launch_manifest_sha256'] == manifest_sha and
                         type(family['members']) is list and len(family['members']) == len(source_vm.BPF_AUTH_FAMILY_MEMBERS),
                         'family.json schema or member count invalid')
            if current:
                import source_resources
                host.require(family['stop_reason'] in (None, 'resource_capacity', 'storage_tool_failure') and
                             type(family['retained_allocated_bytes']) is int and family['retained_allocated_bytes'] >= 0,
                             'family resource summary invalid')
            commands, retained, failed, not_run, failure_members = [], 0, False, False, []
            for entry, (exp_case, exp_scenario, exp_variant) in zip(family['members'], source_vm.BPF_AUTH_FAMILY_MEMBERS):
                keys = {'variant', 'case', 'scenario', 'directory', 'receipt_sha256', 'status'}
                if current:
                    keys.add('retained_allocated_bytes')
                host.require(type(entry) is dict and set(entry) == keys and
                             entry['variant'] == exp_variant and entry['case'] == exp_case and
                             entry['scenario'] == exp_scenario and
                             entry['directory'] == 'bpf-authorization/' + exp_variant and
                             entry['status'] in (('PASS', 'FAIL', 'NOT_RUN') if current else ('PASS',)),
                             'family member entry invalid')
                member_dir = attempt_dir / entry['directory']
                if entry['status'] == 'NOT_RUN':
                    host.require(current and family['stop_reason'] is not None and failed and
                                 entry['receipt_sha256'] is None and entry['retained_allocated_bytes'] == 0 and
                                 type(entry['retained_allocated_bytes']) is int and not member_dir.exists(),
                                 'unrun family member has evidence')
                    not_run = True
                    continue
                host.require(not not_run, 'family resumed after stop')
                if current:
                    amount = entry['retained_allocated_bytes']
                    host.require(type(amount) is int and amount >= 0 and
                                 source_resources.allocated_tree_bytes(host, member_dir) == amount,
                                 'family retained allocation differs')
                    retained += amount
                receipt = member_dir / 'receipt.json'
                if entry['receipt_sha256'] is None:
                    host.require(entry['status'] == 'FAIL' and not receipt.exists(), 'family receipt missing')
                else:
                    host.require(type(entry['receipt_sha256']) is str and HEX.fullmatch(entry['receipt_sha256']) and
                                 host.digest(receipt)[0] == entry['receipt_sha256'], 'family member receipt_sha256 differs')
                if entry['status'] == 'FAIL':
                    failed = True
                    failure_members.append(_verify_single_vm_dir(host, frozen, member_dir, exp_case, exp_scenario, build,
                                           variant=exp_variant, expected_manifest_sha=manifest_sha))
                    continue
                m_res = _verify_single_vm_dir(host, frozen, member_dir, exp_case, exp_scenario, build,
                                              variant=exp_variant, expected_manifest_sha=manifest_sha)
                host.require(m_res['status'] == 'PASS', 'family member verification failed')
                commands = m_res['qmp_commands']
            if current:
                executed = [entry for entry in family['members'] if entry['status'] != 'NOT_RUN']
                host.require(retained == family['retained_allocated_bytes'] and
                             (family['stop_reason'] is None or (failed and executed[-1]['status'] == 'FAIL')),
                             'family accounting or stop differs')
                if failed or not_run:
                    return {'case': case, 'scenario': scenario, 'status': 'FAIL', 'members': failure_members,
                            'stop_reason': family['stop_reason'], 'reason': 'family member failed or was not run'}
            return {'case': case, 'scenario': scenario, 'status': 'PASS', 'qmp_commands': commands}
        directory = attempt_dir / scenario
        source_retirement.assert_available(host, directory)
        res = _verify_single_vm_dir(host, frozen, directory, case, scenario, build,
                                    variant=None, expected_manifest_sha=manifest_sha)
        return {**res, 'case': case, 'scenario': scenario, 'qmp_commands': res.get('qmp_commands', [])}
    except (OSError, ValueError, KeyError, TypeError, RecursionError, host.Failure) as exc:
        return {'case': case, 'scenario': scenario, 'status': 'FAIL', 'error_type': type(exc).__name__,
                'reason': str(exc)[:256]}

def verify(host, frozen):
    lock = source_build.lock_run(host)
    try:
        setup = {'status': 'NOT_RUN'}
        build = None
        try:
            build = source_build._selected(host, frozen)
            if build is not None:
                source_build._validate_current(host, frozen, build)
                setup = {'status': 'PASS'}
        except (OSError, ValueError, KeyError, TypeError, host.Failure) as exc:
            setup = {'status': 'FAIL', 'error_type': type(exc).__name__, 'reason': str(exc)[:256]}
            build = None
        results = ([_case(host, frozen, case, scenario, build) for case in CASES for scenario in scenarios(case)]
                   if build is not None else
                   [{'case': case, 'scenario': scenario, 'status': 'NOT_RUN'}
                    for case in CASES for scenario in scenarios(case)])
        if build is not None:
            try:
                source_build._validate_current(host, frozen, build)
            except (OSError, ValueError, KeyError, TypeError, host.Failure) as exc:
                setup = {'status': 'FAIL', 'error_type': type(exc).__name__, 'reason': str(exc)[:256]}
        passed = setup['status'] == 'PASS' and all(item['status'] == 'PASS' for item in results)
        report = {'version': 1, 'run_token': RUN_TOKEN, 'status': 'PASS' if passed else 'NOT_READY',
                  'product_gate': 'NOT_READY', 'permanent_deletion_http': 'CLOSED', 'setup': setup,
                  'build_selection': build['_selection'] if build is not None else None, 'cases': results}
        path = host.RUN / ('source-verification-' + secrets.token_hex(8) + '.json')
        source_build._write(path, host.canonical(report))
        print(json.dumps({'status': report['status'], 'report': str(path),
                          'passed': sum(item['status'] == 'PASS' for item in results),
                          'failed': sum(item['status'] == 'FAIL' for item in results),
                          'not_run': sum(item['status'] == 'NOT_RUN' for item in results)}))
        return 0 if passed else 1
    finally:
        os.close(lock)
