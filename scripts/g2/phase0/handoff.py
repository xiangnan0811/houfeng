"""UID 10002 coordinator for the isolated input-handoff experiment."""
import base64
import errno
import fcntl
import hashlib
import json
import os
import re
import selectors
import select
import signal
import socket
import stat
import struct
import subprocess
import time
import preparation

MAX_HTTP = 8192
MAX_ROOTS = 16
CASES = ('C0', 'V1', 'V2', 'V3', 'I0', 'I1', 'I2', 'I3')
DIAGNOSTIC_LIMIT = 16 * 1024


class MountGraphError(ValueError):
    def __init__(self, failures, row, target, options, optional):
        super().__init__('mount graph rejected: ' + ', '.join(failures))
        self.code = failures[0]
        self.detail = {'failures': failures, 'row': row, 'target': target,
                       'options': options, 'optional_fields': optional}


class UnknownVolumeAdmission(ValueError):
    code = 'unknown_unregistered_volume'

    def __init__(self):
        super().__init__('volume was not previously registered')


def retain_guard():
    """Keep the owning process and its flock alive until the VM is terminated."""
    while True:
        try:
            signal.pause()
        except BaseException:
            continue


def guarded_call(work):
    """Permit return only after the serving path confirms clean retirement."""
    try:
        if work() is True:
            return True
    except BaseException:
        pass
    retain_guard()


def strict_json(data):
    def pairs(items):
        out = {}
        for key, value in items:
            if key in out:
                raise ValueError('duplicate JSON key: ' + key)
            out[key] = value
        return out
    return json.loads(data, object_pairs_hook=pairs,
                      parse_constant=lambda value: (_ for _ in ()).throw(ValueError('invalid constant')))


def same_json(left, right):
    """Compare JSON values without treating booleans and numbers as equal."""
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(same_json(left[key], right[key]) for key in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(same_json(a, b) for a, b in zip(left, right))
    return left == right


def mount_map(mounts):
    """Compare the complete Docker mount set independently of map iteration order."""
    if not isinstance(mounts, list):
        raise ValueError('mount list missing or invalid')
    result = {}
    for entry in mounts:
        if not isinstance(entry, dict):
            raise ValueError('invalid mount entry')
        destination = entry.get('Destination')
        if not isinstance(destination, str) or not destination or destination in result:
            raise ValueError('missing or duplicate mount destination')
        result[destination] = entry
    return result


def running(fd):
    return not select.select([fd], [], [], 0)[0]


def proc_stat(text):
    end = text.rfind(')')
    if end < 0:
        raise ValueError('invalid process stat')
    pid = int(text[:text.find(' ')])
    fields = text[end + 2:].split()
    if len(fields) < 20:
        raise ValueError('short process stat')
    return {'pid': pid, 'starttime': int(fields[19])}


def unescape_mount(value):
    if type(value) is not str or not value.isascii() or '\x00' in value:
        raise ValueError('invalid mountinfo field')
    decoded = []
    index = 0
    escapes = {'040': ' ', '011': '\t', '012': '\n', '134': '\\'}
    while index < len(value):
        if value[index] != '\\':
            decoded.append(value[index])
            index += 1
            continue
        digits = value[index + 1:index + 4]
        if len(digits) != 3 or digits not in escapes:
            raise ValueError('invalid mountinfo escape')
        decoded.append(escapes[digits])
        index += 4
    return ''.join(decoded)


def mount_graph(text, expected):
    if len(text.encode()) > 1024 * 1024:
        raise ValueError('mountinfo exceeds limit')
    matches = {}
    for line in text.splitlines():
        fields = line.split()
        if len(fields) < 10 or '-' not in fields:
            raise ValueError('invalid mountinfo line')
        separator = fields.index('-')
        if separator < 6 or len(fields) < separator + 4:
            raise ValueError('short mountinfo line')
        target = unescape_mount(fields[4])
        if not any(target == root or target.startswith(root + '/') for root in expected):
            if target.startswith(('/roots/', '/meta/')):
                raise ValueError('unrecognized helper input mount')
            continue
        if target in matches:
            raise ValueError('stacked or duplicate mountpoint')
        options = set(fields[5].split(','))
        optional = fields[6:separator]
        failures = []
        if 'ro' not in options or 'rw' in options:
            failures.append('mount_not_readonly')
        if any(x.startswith(('shared:', 'master:', 'propagate_from:')) or x == 'unbindable' for x in optional):
            failures.append('mount_not_private')
        if failures:
            raise MountGraphError(failures, line, target, fields[5], optional)
        matches[target] = line
    if set(matches) != set(expected):
        raise ValueError('missing or extra input mount: %s' % sorted(set(matches) ^ set(expected)))
    return matches

def check_observer_status(m, value, apparmor):
    fields = {'cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb', 'no_new_privs',
              'seccomp', 'namespaces', 'apparmor', 'fd_targets'}
    m.check(type(value) is dict and set(value) == fields, 'helper status fields mismatch')
    for name in ('cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb'):
        m.check(type(value[name]) is str and re.fullmatch('[0-9a-f]{16}', value[name]) is not None,
                'invalid helper capability')
        m.check(int(value[name], 16) == (4 if name in ('cap_eff', 'cap_prm', 'cap_bnd') else 0),
                'helper capability mismatch')
    m.check(type(value['no_new_privs']) is int and value['no_new_privs'] == 1 and
            type(value['seccomp']) is int and value['seccomp'] == 2, 'helper privilege mismatch')
    label = value['apparmor']
    m.check(type(label) is dict and set(label) == {'label', 'errno'} and type(label['errno']) is int and
            label['errno'] == 0 and type(label['label']) is str and type(apparmor) is str and
            bool(apparmor) and apparmor != 'unconfined' and
            label['label'] in (apparmor, apparmor + ' (enforce)'), 'helper AppArmor identity mismatch')
    namespaces = value['namespaces']
    m.check(type(namespaces) is dict and set(namespaces) == {'pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'},
            'helper namespaces incomplete')
    for name, actual in namespaces.items():
        here = os.stat('/proc/self/ns/' + name)
        m.check(type(actual) is dict and set(actual) == {'dev', 'ino'} and
                all(type(actual[key]) is int and actual[key] >= 0 for key in ('dev', 'ino')) and
                (actual['dev'], actual['ino']) != (here.st_dev, here.st_ino),
                'helper shares coordinator namespace: ' + name)
    targets = value['fd_targets']
    m.check(type(targets) is dict and {'0', '1', '2'} <= set(targets) and
            all(type(key) is str and key.isdecimal() and type(target) is str and
            (target in ('/dev/null', 'anon_inode:[eventpoll]', 'anon_inode:[eventfd]') or
             re.fullmatch(r'pipe:\[[0-9]+\]', target) is not None)
            for key, target in targets.items()), 'helper inherited an unexpected file descriptor')



def parse_probe(m, result, case, phase, roots, metadata, exe=False, nested=False, deny=False,
                waiting=None, ready=None, apparmor=None):
    m.check(metadata in ('none', 'proc-v1') and 1 <= roots <= MAX_ROOTS and case in CASES and
            phase in ('before', 'after') and (not exe or metadata == 'proc-v1'), 'invalid probe expectation')
    m.check(result['returncode'] == 0, 'probe process did not exit successfully')
    stdout = result['stdout']
    m.check(len(stdout.encode()) <= 1024 * 1024 and stdout.endswith('\n') and '\ufffd' not in stdout,
            'truncated, invalid, or oversized probe output')
    records, indexed = [], {}
    for line in stdout.splitlines():
        record = strict_json(line)
        m.check(isinstance(record, dict) and set(record) == {'experiment_kind', 'preparation_version',
                 'metadata_mode', 'case', 'phase', 'root_ordinal', 'operation', 'errno', 'value'} and
                record['experiment_kind'] == 'input-handoff-v1' and type(record['preparation_version']) is int and
                record['preparation_version'] == 1 and record['metadata_mode'] == metadata and
                record['case'] == case and record['phase'] == phase and type(record['errno']) is int and
                isinstance(record['value'], dict), 'invalid probe record')
        key = (record['root_ordinal'], record['operation'])
        m.check(key not in indexed, 'duplicate probe operation')
        indexed[key] = record
        records.append(record)
    wanted = {(None, 'waiting'), (None, 'ready'), (None, 'self_status'), (None, 'complete')}
    if deny:
        wanted |= {(None, x) for x in ('inet_socket', 'unshare', 'mount')}
        wanted |= {('%03d' % i, 'readonly_open') for i in range(2 if nested else 1)}
    else:
        wanted.add((None, 'mountinfo'))
        ops = ('fstat', 'statx', 'statfs', 'btrfs_fs_info', 'btrfs_ino_lookup')
        wanted |= {('%03d' % i, op) for i in range(roots) for op in ops}
        if nested:
            wanted |= {('nested:000', op) for op in ops}
        if metadata == 'proc-v1':
            wanted |= {(None, x) for x in ('target_stat', 'target_mountinfo', 'target_pidns')}
            if exe:
                wanted.add((None, 'target_exe'))
    m.check(set(indexed) == wanted and records[0]['operation'] == 'waiting' and
            records[1]['operation'] == 'ready' and records[-1]['operation'] == 'complete',
            'missing, extra or out-of-order probe operation')
    first, confirmed = indexed[(None, 'waiting')], indexed[(None, 'ready')]
    m.check(first['errno'] == confirmed['errno'] == 0 and set(first['value']) ==
            {'nonce', 'input_digest', 'self_status'} and waiting is not None and ready is not None and
            first['value'] == waiting and confirmed['value'] == ready and
            first['value']['nonce'] == ready['nonce'] and first['value']['input_digest'] == ready['input_digest'],
            'observer waiting or ready binding differs')
    done = indexed[(None, 'complete')]
    m.check(done['errno'] == 0 and done['value'] == {'complete': True, 'expected_roots': roots}, 'incomplete probe')
    status = indexed[(None, 'self_status')]
    value = status['value']
    check_observer_status(m, first['value']['self_status'], apparmor)
    check_observer_status(m, value, apparmor)
    m.check(status['errno'] == 0 and all(first['value']['self_status'][key] == value[key] for key in
            ('cap_eff', 'cap_prm', 'cap_bnd', 'cap_inh', 'cap_amb', 'no_new_privs', 'seccomp',
             'namespaces', 'apparmor')), 'helper security identity changed after waiting')
    schemas = {
        'fstat': {'dev', 'ino', 'mode', 'size'},
        'statx': {'mask', 'dev_major', 'dev_minor', 'ino', 'mode', 'mount_id'},
        'statfs': {'type', 'fsid'}, 'btrfs_fs_info': {'fsid'}, 'btrfs_ino_lookup': {'subvol'},
        'mountinfo': {'text'}, 'target_stat': {'text'}, 'target_mountinfo': {'text'},
        'target_pidns': {'dev', 'ino'}, 'target_exe': {'dev', 'ino'},
        'readonly_open': set(), 'inet_socket': set(), 'unshare': set(), 'mount': set(),
    }
    for key, entry in indexed.items():
        op, error, val = key[1], entry['errno'], entry['value']
        m.check(error >= 0, 'negative probe errno')
        if op in ('waiting', 'ready', 'self_status', 'complete'):
            continue
        m.check(op in schemas, 'unknown operation')
        if error:
            m.check(not val and op in ('btrfs_fs_info', 'btrfs_ino_lookup', 'readonly_open',
                                       'inet_socket', 'unshare', 'mount'), 'failed probe operation')
            continue
        m.check(set(val) == schemas[op], 'unknown or missing operation value field')
        if op in ('fstat', 'statx', 'target_pidns', 'target_exe', 'btrfs_ino_lookup'):
            m.check(all(type(v) is int and v >= 0 for v in val.values()), 'invalid numeric metadata')
        elif op in ('mountinfo', 'target_stat', 'target_mountinfo'):
            m.check(type(val['text']) is str and len(val['text'].encode()) <= 1024 * 1024,
                    'invalid metadata text')
        elif op == 'btrfs_fs_info':
            m.check(type(val['fsid']) is str and re.fullmatch('[0-9a-f]{32}', val['fsid']), 'invalid btrfs FSID')
        elif op == 'statfs':
            m.check(type(val['type']) is int and isinstance(val['fsid'], list) and len(val['fsid']) == 2 and
                    all(type(v) is int for v in val['fsid']), 'invalid statfs')
    if not deny:
        ordinals = ['%03d' % i for i in range(roots)] + (['nested:000'] if nested else [])
        for ordinal in ordinals:
            fst, sx, fs = (indexed[(ordinal, op)]['value'] for op in ('fstat', 'statx', 'statfs'))
            m.check(fst['ino'] == sx['ino'] and fst['mode'] == sx['mode'] and
                    stat.S_IFMT(fst['mode']) in (stat.S_IFDIR, stat.S_IFREG), 'root metadata inconsistent')
            if fs['type'] == 0x9123683e:
                m.check(indexed[(ordinal, 'btrfs_fs_info')]['errno'] == 0 and
                        indexed[(ordinal, 'btrfs_ino_lookup')]['errno'] == 0, 'btrfs identity missing')
    return records, indexed


class Coordinator:
    def __init__(self, m, lock):
        self.m = m
        self.lock = lock
        self.created = {}
        self.pending = {}
        self.volumes = {}
        self.anonymous_owners = {}
        self.watchers = {}
        self.target = None
        self.daemon = None
        self.commands = []
        self.inspection_failures = 0
        self.docker = DirectDocker(self)
        self.launch = m.PREPARATION_CLIENT
        self.preparation_ordinal = 0
        self.retired_containers = []
        self.helper_deadline = None
        reader = os.stat('/proc/thread-self/ns/cgroup')
        self.reader_cgroupns = {'dev': reader.st_dev, 'ino': reader.st_ino}
        preparation.identity(self.reader_cgroupns, 'coordinator reader cgroup namespace')

    def check_reader(self):
        try:
            reader = os.stat('/proc/thread-self/ns/cgroup')
        except OSError as exc:
            raise PreparationFailure('prepare_identity_changed', 'coordinator cgroup reader unavailable: ' +
                                     str(exc)) from exc
        current = {'dev': reader.st_dev, 'ino': reader.st_ino}
        if current != self.reader_cgroupns:
            raise PreparationFailure('prepare_identity_changed', 'coordinator cgroup reader changed')
        return current

    def save_new(self, name, value):
        path = '/lab/control/' + name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        with os.fdopen(fd, 'w', encoding='utf-8') as out:
            json.dump(value, out, sort_keys=True, separators=(',', ':'), ensure_ascii=False)
            out.write('\n')
            out.flush()
            os.fsync(out.fileno())
        directory = os.open('/lab/control', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def guard(self):
        st = os.fstat(self.lock)
        path = os.stat('/lab/control/guard', follow_symlinks=False)
        self.m.check((st.st_dev, st.st_ino, st.st_uid, stat.S_IMODE(st.st_mode)) ==
                     (path.st_dev, path.st_ino, 10002, 0o600), 'guard identity changed')
        fd = os.open('/lab/control/guard', os.O_RDWR | os.O_NOFOLLOW)
        try:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return {'held': True, 'dev': st.st_dev, 'ino': st.st_ino}
            fcntl.flock(fd, fcntl.LOCK_UN)
            raise self.m.Failure('coordinator guard is not held')
        finally:
            os.close(fd)

    def run(self, argv, data=None, timeout=90):
        self.m.check(isinstance(argv, list) and argv and all(isinstance(x, str) and '\x00' not in x for x in argv),
                     'invalid Docker command')
        self.m.check(0 < timeout <= 300, 'invalid Docker command timeout')
        if self.helper_deadline is not None:
            remaining = self.helper_deadline - time.monotonic()
            if remaining <= 0:
                raise PreparationFailure('observer_not_ready', 'helper deadline exceeded')
            timeout = min(timeout, remaining)
        self.guard()
        if argv[0] in ('create', 'start'):
            self.m.check(self.launch is not None and self.launch.registered and not self.launch.retired,
                         'guard registration must precede create/start')
        self.commands.append(list(argv))
        kwargs = {'timeout': timeout, 'input_data': data, 'env': {**self.m.ENV, 'HOME': '/lab/control'}}
        if argv[:2] == ['start', '--attach']:
            kwargs['capture_raw'] = True
        return self.m.command(['/usr/bin/docker', '--host=unix:///var/run/docker.sock'] + argv, **kwargs)

    def inspect(self, cid, capture=None):
        self.m.check(re.fullmatch('[0-9a-f]{64}', cid) is not None, 'container ID must be complete')
        result = self.run(['inspect', cid])
        if capture is not None:
            capture['command_result'] = result
        self.m.check(result['returncode'] == 0, 'container inspect failed')
        values = strict_json(result['stdout'])
        self.m.check(isinstance(values, list) and len(values) == 1 and values[0]['Id'] == cid,
                     'container inspect identity changed')
        return values[0]

    def validate_created(self, observed, spec):
        m = self.m
        config, hc = observed['Config'], observed['HostConfig']
        m.check('OomKillDisable' in hc and hc['OomKillDisable'] is False,
                'created OOM killer setting differs from the fixed default')
        labels = dict(pair.split('=', 1) for pair in spec['labels'][1::2])
        m.check(config['Labels'] == labels and set(spec['environment'][1::2]) <= set(config['Env']) and
                config['Entrypoint'] == ['/probe'] and config['Cmd'] == spec['args'] and config['User'] == '0:0',
                'created command, label or environment mismatch')
        prepared = bool(spec['args'] and spec['args'][0] in ('prepared-observe', 'prepared-denycheck'))
        if prepared:
            m.check(spec['options'].count('--interactive') == 1 and config.get('OpenStdin') is True and
                    config.get('AttachStdin') is True and config.get('AttachStdout') is True and
                    config.get('AttachStderr') is True and config.get('StdinOnce') is True and
                    config.get('Tty') is False, 'prepared container stdin/attach mismatch')
        cap_add = ['CAP_DAC_READ_SEARCH'] if '--cap-add' in spec['options'] else []
        m.check(hc['NetworkMode'] == 'none' and hc['ReadonlyRootfs'] is True and hc['CapDrop'] == ['ALL'] and
                (hc.get('CapAdd') or []) == cap_add and hc['PidMode'] == '' and hc['IpcMode'] == 'private' and
                hc['UTSMode'] == '' and hc['CgroupnsMode'] == 'private' and hc['AutoRemove'] is False and
                hc['PidsLimit'] == 64 and hc['Memory'] == 268435456 and hc['MemorySwap'] == 268435456 and
                hc['NanoCpus'] == 1000000000 and hc['LogConfig']['Type'] == 'none' and
                not hc.get('Privileged') and not hc.get('Devices') and not hc.get('DeviceRequests') and
                not hc.get('Binds') and not hc.get('VolumesFrom') and not hc.get('Links') and
                not hc.get('PortBindings') and not hc.get('PublishAllPorts'), 'created isolation mismatch')
        security = hc.get('SecurityOpt') or []
        m.check(any(x in ('no-new-privileges', 'no-new-privileges=true') for x in security) and
                not any('unconfined' in x for x in security), 'created security mismatch')
        mounts = []
        for index, option in enumerate(spec['options']):
            if option != '--mount':
                continue
            m.check(index + 1 < len(spec['options']), 'mount option missing value')
            parts = spec['options'][index + 1].split(',')
            fields, flags = {}, set()
            for part in parts:
                if '=' in part:
                    key, value = part.split('=', 1)
                    m.check(key not in fields, 'duplicate mount option')
                    fields[key] = value
                else:
                    m.check(part not in flags, 'duplicate mount flag')
                    flags.add(part)
            m.check(set(fields) <= {'type', 'source', 'target', 'bind-recursive', 'bind-propagation'} and
                    flags <= {'readonly', 'volume-nocopy'} and {'type', 'target'} <= set(fields) and
                    ('source' in fields or (fields['type'] == 'volume' and fields['target'] == '/data' and
                     spec['args'] == ['hold'] and 'G2_CASE=V1' in spec['environment'] and
                     flags == {'volume-nocopy'})), 'unknown mount option')
            mounts.append((fields, flags))
        actual, configured = observed['Mounts'], hc.get('Mounts') or []
        m.check(len(mounts) == len(actual) == len(configured) and
                len({fields['target'] for fields, _ in mounts}) == len(mounts), 'created mount set mismatch')
        for fields, flags in mounts:
            targets = [x for x in actual if x['Destination'] == fields['target']]
            definitions = [x for x in configured if x['Target'] == fields['target']]
            m.check(len(targets) == len(definitions) == 1, 'created mount destination ambiguous')
            mounted, definition = targets[0], definitions[0]
            declared = fields.get('source')
            observed_source = definition.get('Source') or ''
            m.check(mounted['Type'] == definition['Type'] == fields['type'] and
                    (observed_source == declared if declared is not None else
                     observed_source in ('', mounted.get('Name'))) and
                    bool(definition.get('ReadOnly')) == ('readonly' in flags) and
                    mounted['RW'] == ('readonly' not in flags), 'created mount attributes mismatch')
            if fields['type'] == 'bind':
                m.check(mounted['Source'] == fields['source'] and mounted.get('Propagation') == 'rprivate' and
                        fields.get('bind-propagation') == 'rprivate', 'created bind source mismatch')
            elif fields['type'] == 'volume':
                name = fields.get('source')
                if name is None:
                    name = mounted.get('Name')
                    m.check(isinstance(name, str) and re.fullmatch('[0-9a-f]{64}', name) and
                            mounted['Source'] == '/lab/ext/docker/volumes/' + name + '/_data',
                            'anonymous fixture volume name invalid')
                else:
                    m.check(name in self.volumes and mounted.get('Name') == name and
                            mounted['Source'] == self.volumes[name]['Mountpoint'], 'created volume source mismatch')
                m.check(bool((definition.get('VolumeOptions') or {}).get('NoCopy')) == ('volume-nocopy' in flags),
                        'created volume NoCopy mismatch')
            else:
                raise m.Failure('unsupported created mount type')

    def create(self, argv, spec, timeout):
        self.m.check(self.launch is not None and self.launch.registered and not self.launch.retired,
                     'guard registration must precede create')
        self.launch.require_no_pending()
        self.m.check(set(spec) == {'name', 'image', 'options', 'args', 'token', 'labels', 'environment'} and
                     re.fullmatch('[0-9a-f]{64}', spec['token']) and spec['name'].endswith('-' + spec['token']) and
                     re.fullmatch('sha256:[0-9a-f]{64}', spec['image']) and
                     argv == ['create', '--name', spec['name']] + spec['labels'] + spec['environment'] +
                     spec['options'] + ['--entrypoint', '/probe', spec['image']] + spec['args'],
                     'invalid exact create specification')
        token = spec['token']
        self.require_no_pending()
        self.pending[token] = {'spec': spec, 'argv': argv}
        try:
            self.save_new('pending-' + token + '.json', {'status': 'PENDING', 'spec': spec, 'argv': argv})
            result = self.run(argv, timeout=timeout)
            if result['returncode'] != 0:
                name = spec['name']
                check = self.run(['container', 'inspect', name])
                listing = self.run(['container', 'ls', '-a', '--no-trunc', '--filter', 'name=^/' + name + '$',
                                    '--format', '{{.ID}}'])
                absent = (check['returncode'] != 0 and 'No such' in check['stderr'] and
                          listing['returncode'] == 0 and not listing['stdout'].strip())
                self.m.check(absent, 'create failed and exact name absence is unknown')
                self.save_new('failed-' + token + '.json', {'result': result, 'name_absent': True})
                del self.pending[token]
                raise self.m.CreateFailure('container create failed with proven absence',
                                           self.m.bounded_create_result(result))
            cid = self.m.create_receipt_id(result)
            observed = self.inspect(cid)
            config = observed['Config']
            self.m.check(observed['Name'] == '/' + spec['name'] and observed['Image'] == spec['image'] and
                         observed['State']['Status'] == 'created' and config['Entrypoint'] == ['/probe'] and
                         config['Cmd'] == spec['args'] and config['User'] == '0:0' and
                         config['Labels']['create-token'] == token and
                         config['Labels']['run'] == self.m.RESULT['run_token'], 'created object mismatch')
            self.validate_created(observed, spec)
            owned = {'id': cid, 'spec': spec, 'inspect': observed}
            self.save_new('receipt-' + cid + '.json', owned)
            self.created[cid] = owned
            del self.pending[token]
            return result
        except self.m.CreateFailure:
            if token in self.pending:
                raise self.m.GuardUnsafe('create failure left a pending receipt unresolved')
            raise
        except BaseException as exc:
            raise self.m.GuardUnsafe('create ownership or absence uncertain: ' + str(exc)) from exc

    def require_no_pending(self):
        if self.pending:
            raise self.m.GuardUnsafe('unresolved pending create prevents progression')

    def fixed_daemon_info(self):
        path = '/var/run/docker.sock'
        before = os.stat(path, follow_symlinks=False)
        self.m.check(stat.S_ISSOCK(before.st_mode) and before.st_uid == 0 and
                     before.st_gid in os.getgroups() and not before.st_mode & 0o007,
                     'unsafe fixed daemon socket')
        result = self.run(['info', '--format', '{{json .}}'])
        self.m.check(result['returncode'] == 0, 'fixed daemon info query failed')
        info = strict_json(result['stdout'])
        after = os.stat(path, follow_symlinks=False)
        self.m.check((before.st_dev, before.st_ino, before.st_uid, before.st_gid, before.st_mode) ==
                     (after.st_dev, after.st_ino, after.st_uid, after.st_gid, after.st_mode),
                     'fixed daemon socket changed during info query')
        self.m.check(isinstance(info, dict) and isinstance(info.get('ID'), str) and info['ID'] and
                     info.get('ServerVersion') == '29.8.2' and info.get('Driver') == 'overlay2' and
                     info.get('DockerRootDir') == '/lab/ext/docker' and
                     isinstance(info.get('CgroupVersion'), str) and type(info.get('OomKillDisable')) is bool,
                     'fixed daemon info is incomplete or differs from the experiment')
        return {'id': info['ID'], 'version': info['ServerVersion'], 'driver': info['Driver'],
                'root': info['DockerRootDir'], 'cgroup_version': info['CgroupVersion'],
                'oom_kill_disable': info['OomKillDisable'],
                'socket': {'dev': before.st_dev, 'ino': before.st_ino, 'uid': before.st_uid,
                           'gid': before.st_gid, 'mode': before.st_mode}}

    def verify_owned(self, cid):
        self.m.check(cid in self.created, 'container is not registered to this experiment')
        current, capture = None, {}
        try:
            current = self.inspect(cid, capture)
            owned = self.created[cid]
            initial = owned['inspect']
            for field in ('Id', 'Name', 'Image', 'Config'):
                self.m.check(field in current and field in initial and same_json(current[field], initial[field]),
                             'owned container %s changed' % field)
            old_hc, new_hc = initial['HostConfig'], current['HostConfig']
            self.m.check(isinstance(old_hc, dict) and isinstance(new_hc, dict) and
                         old_hc.keys() == new_hc.keys() and 'OomKillDisable' in old_hc and
                         old_hc['OomKillDisable'] is False, 'owned HostConfig fields or original OOM setting changed')
            new_oom = new_hc['OomKillDisable']
            if new_oom is None:
                starts = owned.get('starts', [])
                self.m.check(bool(starts), 'OOM normalization occurred before a recorded start')
                recorded = starts[-1]['daemon']
                self.m.check(recorded['cgroup_version'] == '2' and recorded['oom_kill_disable'] is False and
                             same_json(self.fixed_daemon_info(), recorded), 'OOM normalization daemon changed or unsupported')
            else:
                self.m.check(new_oom is False, 'owned OOM setting is not false or the permitted null')
            self.m.check(all(key == 'OomKillDisable' or same_json(old_hc[key], new_hc[key]) for key in old_hc),
                         'owned HostConfig changed')
            self.m.check(same_json(mount_map(initial['Mounts']), mount_map(current['Mounts'])),
                         'owned container mounts changed')
            return current
        except BaseException as exc:
            self.inspection_failures += 1
            detail = {'id': cid, 'current_inspect': current, 'capture': capture,
                      'error_type': type(exc).__name__, 'error': str(exc)}
            try:
                self.save_new('rejected-inspect-%s-%d.json' % (cid, self.inspection_failures), detail)
            except BaseException as receipt_exc:
                raise self.m.GuardUnsafe('owned inspection rejected: %s: %s; rejection receipt failed: %s: %s' %
                                         (type(exc).__name__, exc, type(receipt_exc).__name__, receipt_exc)) from receipt_exc
            raise self.m.GuardUnsafe('owned inspection rejected: %s: %s' % (type(exc).__name__, exc)) from exc

    def remove(self, cid):
        self.require_no_pending()
        try:
            state = self.verify_owned(cid)['State']
            if state['Running']:
                stopped = self.run(['stop', '--timeout', '10', cid], timeout=30)
                self.m.check(stopped['returncode'] == 0, 'owned container stop failed')
                state = self.verify_owned(cid)['State']
            self.m.check(not state['Running'] and state['Status'] in ('created', 'exited', 'dead'),
                         'owned container exit unknown')
            result = self.run(['rm', cid], timeout=30)
            self.m.check(result['returncode'] == 0 and result['stdout'].strip() == cid, 'owned container remove failed')
            missing = self.run(['container', 'inspect', cid])
            self.m.check(missing['returncode'] != 0 and 'No such' in missing['stderr'], 'container removal not confirmed')
            retirement = {'id': cid, 'remove': result, 'absent': True}
            self.save_new('retired-' + cid + '.json', retirement)
            self.retired_containers.append(retirement)
            del self.created[cid]
        except BaseException as exc:
            raise self.m.GuardUnsafe('owned container removal/exit uncertain: ' + str(exc)) from exc

    def docker_request(self, request):
        argv = request['argv']
        data = base64.b64decode(request['input'], validate=True) if 'input' in request else None
        timeout = request.get('timeout', 90)
        if request.get('receipt') is not None:
            return self.create(argv, request['receipt'], timeout)
        if argv[0] == 'create':
            raise self.m.Failure('create requires durable pending receipt')
        if argv[0] == 'start' and argv[-1] in self.created:
            args = self.created[argv[-1]]['spec']['args']
            self.m.check(not args or args[0] not in ('prepared-observe', 'prepared-denycheck'),
                         'prepared observers require the managed attach protocol')
        if argv[0] in ('start', 'stop', 'kill', 'rm'):
            try:
                cid = argv[-1]
                self.verify_owned(cid)
                if argv[0] == 'rm':
                    self.remove(cid)
                    return {'argv': argv, 'returncode': 0, 'stdout': cid + '\n', 'stderr': ''}
                if argv[0] == 'start':
                    owned = self.created[cid]
                    daemon = self.fixed_daemon_info()
                    starts = owned.get('starts', [])
                    if starts:
                        self.m.check(same_json(daemon, starts[-1]['daemon']), 'daemon changed between starts')
                    sequence = len(starts) + 1
                    intent = {'id': cid, 'sequence': sequence, 'argv': list(argv), 'daemon': daemon}
                    self.save_new('start-intent-%s-%d.json' % (cid, sequence), intent)
                    try:
                        result = self.run(argv, data, timeout)
                    except BaseException as exc:
                        if argv[:2] == ['start', '--attach']:
                            detail = {'id': cid, 'sequence': sequence, 'argv': list(argv), 'spec': owned['spec'],
                                      'inspect_before': owned['inspect'], 'incomplete': True,
                                      'error_type': type(exc).__name__, 'error': str(exc)[:2048],
                                      'capture': getattr(exc, 'command_evidence',
                                                         {'incomplete': True, 'evidence_limit': False})}
                            try:
                                self.save_new('attach-incomplete-%s-%d.json' % (cid, sequence), detail)
                            except BaseException as receipt_exc:
                                unsafe = self.m.GuardUnsafe('start attach failed: %s: %s; evidence receipt failed: %s: %s' %
                                                            (type(exc).__name__, exc, type(receipt_exc).__name__,
                                                             receipt_exc))
                                unsafe.incomplete = True
                                unsafe.evidence_limit = bool(getattr(exc, 'command_evidence', {}).get('evidence_limit'))
                                raise unsafe from receipt_exc
                        raise
                    if argv[:2] == ['start', '--attach']:
                        try:
                            self.save_new('attach-output-%s-%d.json' % (cid, sequence),
                                          {'id': cid, 'sequence': sequence, 'spec': owned['spec'],
                                           'inspect_before': owned['inspect'], 'start_attach': result,
                                           'incomplete': raw_capture_problem(result) is not None})
                        except BaseException as receipt_exc:
                            unsafe = self.m.GuardUnsafe('start attach output receipt failed: %s: %s' %
                                                        (type(receipt_exc).__name__, receipt_exc))
                            unsafe.incomplete = True
                            raise unsafe from receipt_exc
                    completed = {**intent, 'returncode': result['returncode']}
                    self.save_new('start-result-%s-%d.json' % (cid, sequence), completed)
                    owned.setdefault('starts', []).append(completed)
                    return result
                return self.run(argv, data, timeout)
            except BaseException as exc:
                unsafe = self.m.GuardUnsafe('container lifecycle response or ownership uncertain: ' + str(exc))
                capture = getattr(exc, 'command_evidence', {})
                unsafe.incomplete = bool(capture.get('incomplete') or getattr(exc, 'incomplete', False))
                unsafe.evidence_limit = bool(capture.get('evidence_limit') or getattr(exc, 'evidence_limit', False))
                raise unsafe from exc
        return self.run(argv, data, timeout)

    def volume_list(self):
        result = self.run(['volume', 'ls', '--format', '{{.Name}}'])
        self.m.check(result['returncode'] == 0, 'volume listing failed')
        names = result['stdout'].splitlines()
        self.m.check(len(names) == len(set(names)), 'duplicate volume listing')
        return sorted(names)

    def register_fixture(self, cid, spec):
        m = self.m
        m.check(cid not in self.created and set(spec) == {'name', 'image', 'options', 'args', 'token',
                'labels', 'environment'} and spec['args'] == ['hold'] and
                'G2_CASE=V1' in spec['environment'] and spec['name'].endswith('-' + spec['token']) and
                re.fullmatch('[0-9a-f]{64}', spec['token']) and re.fullmatch('sha256:[0-9a-f]{64}', spec['image']) and
                'type=volume,target=/data,volume-nocopy' in spec['options'], 'invalid anonymous fixture')
        observed = self.inspect(cid)
        m.check(observed['Id'] == cid and observed['Name'] == '/' + spec['name'] and
                observed['Image'] == spec['image'] and observed['State']['Status'] == 'created',
                'anonymous fixture identity mismatch')
        self.validate_created(observed, spec)
        owned = {'id': cid, 'spec': spec, 'inspect': observed, 'origin': 'root_fixture'}
        self.save_new('receipt-' + cid + '.json', owned)
        self.created[cid] = owned
        mounts = [x for x in observed['Mounts'] if x['Destination'] == '/data' and x['Type'] == 'volume']
        m.check(len(mounts) == 1, 'anonymous fixture mount missing')
        return {'id': cid, 'volume_name': mounts[0]['Name']}

    def volume_inspect(self, name, creator=None):
        self.m.check(isinstance(name, str) and re.fullmatch('[A-Za-z0-9][A-Za-z0-9_.-]{0,255}', name),
                     'invalid complete volume name')
        result = self.run(['volume', 'inspect', name])
        if result['returncode'] != 0:
            error = result['stderr'].lower()
            self.m.check('no such volume' in error or 'no such object' in error,
                         'volume inspection failed for an unknown reason')
            return None
        data = strict_json(result['stdout'])
        self.m.check(len(data) == 1, 'ambiguous volume')
        value = data[0]
        self.m.check(value.get('Name') == name and value.get('Driver') == 'local' and
                     value.get('Scope') == 'local' and not value.get('Options') and
                     isinstance(value.get('CreatedAt'), str) and value['CreatedAt'] and
                     value.get('Mountpoint') == '/lab/ext/docker/volumes/' + name + '/_data',
                     'unsupported volume metadata')
        labels = value.get('Labels') or {}
        labelled = labels.get('owner') == 'g2-isolated-capability' and labels.get('run') == self.m.RESULT['run_token']
        if not labelled:
            cid = creator or self.anonymous_owners.get(name)
            self.m.check(cid in self.created and labels.get('com.docker.volume.anonymous') == '' and
                         re.fullmatch('[0-9a-f]{64}', name), 'anonymous volume owner not established')
            owner = self.verify_owned(cid)
            self.m.check(any(x['Destination'] == '/data' and x['Type'] == 'volume' and x.get('Name') == name
                             for x in owner['Mounts']) and
                         'type=volume,target=/data,volume-nocopy' in self.created[cid]['spec']['options'],
                         'anonymous volume is not bound to its fixture consumer')
        return value

    def register_volume(self, name, creator=None):
        before = self.volume_list()
        value = self.volume_inspect(name, creator)
        self.m.check(value is not None and name in before, 'volume not already present')
        self.m.check(name not in self.volumes or self.volumes[name] == value, 'volume identity changed')
        if creator is not None:
            self.anonymous_owners[name] = creator
        self.volumes[name] = value
        return {'inspect': value, 'volume_set': before}

    def missing_volume(self, name, image, profile):
        self.require_no_pending()
        owner_before = json.dumps({'created': self.created, 'pending': self.pending}, sort_keys=True,
                                  separators=(',', ':'))
        created_before, pending_before = sorted(self.created), sorted(self.pending)
        index = len(self.commands)
        before = self.volume_list()
        self.m.check(name not in self.volumes and name not in before, 'missing test name unexpectedly exists')
        self.m.check(self.volume_inspect(name) is None, 'missing volume exists')
        request = {'op': 'helper', 'image': image, 'profile': profile, 'case': 'V3', 'phase': 'after',
                   'roots': [{'type': 'volume', 'name': name}]}
        try:
            dispatch(self, request)
        except UnknownVolumeAdmission as exc:
            rejection = {'type': type(exc).__name__, 'code': exc.code, 'message': str(exc)}
        else:
            raise self.m.Failure('missing-volume helper admission unexpectedly succeeded')
        after = self.volume_list()
        issued = self.commands[index:]
        owner_after = json.dumps({'created': self.created, 'pending': self.pending}, sort_keys=True,
                                 separators=(',', ':'))
        self.m.check(before == after and owner_before == owner_after and
                     not any(cmd and cmd[0] == 'create' for cmd in issued),
                     'missing-volume refusal changed volume set, ownership, or created helper')
        return {'reason': 'unknown_volume', 'request': request, 'rejection': rejection,
                'before': before, 'after': after, 'commands': issued,
                'created_before': created_before, 'created_after': sorted(self.created),
                'pending_before': pending_before, 'pending_after': sorted(self.pending),
                'owner_state_unchanged': True}

    def direct_access(self, name):
        self.m.check(name in self.volumes and self.volume_inspect(name) == self.volumes[name], 'volume not registered')
        try:
            fd = os.open(self.volumes[name]['Mountpoint'], os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC | os.O_NOFOLLOW)
            os.close(fd)
            return {'errno': 0}
        except OSError as exc:
            return {'errno': exc.errno}

    def ping(self):
        daemon = self.daemon
        sock = daemon['socket']
        deadline = min(time.monotonic() + 10, self.helper_deadline or float('inf'))
        remaining = deadline - time.monotonic()
        self.m.check(remaining > 0, 'daemon ping deadline')
        sock.settimeout(remaining)
        sock.sendall(b'GET /_ping HTTP/1.1\r\nHost: docker\r\nConnection: keep-alive\r\n\r\n')
        data = bytearray()
        sender = None
        header_end = None
        length = None
        while True:
            remaining = deadline - time.monotonic()
            self.m.check(remaining > 0, 'daemon ping deadline')
            sock.settimeout(remaining)
            chunk, ancillary, flags, _ = sock.recvmsg(MAX_HTTP + 1 - len(data), socket.CMSG_SPACE(struct.calcsize('3i')))
            self.m.check(chunk and not flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC), 'truncated daemon response')
            credentials = [payload for level, kind, payload in ancillary
                           if level == socket.SOL_SOCKET and kind == socket.SCM_CREDENTIALS]
            self.m.check(len(ancillary) == 1 and len(credentials) == 1 and
                         len(credentials[0]) == struct.calcsize('3i'), 'missing or extra daemon credentials')
            current = struct.unpack('3i', credentials[0])
            self.m.check(current[0] > 0 and current[1] == 0 and (sender is None or sender == current),
                         'daemon sender changed or is not root')
            sender = current
            data.extend(chunk)
            self.m.check(len(data) <= MAX_HTTP, 'daemon response exceeded bound')
            if header_end is None and b'\r\n\r\n' in data:
                header_end = data.index(b'\r\n\r\n') + 4
                lines = bytes(data[:header_end - 4]).split(b'\r\n')
                self.m.check(lines[0] == b'HTTP/1.1 200 OK', 'unexpected daemon HTTP status')
                headers = {}
                for line in lines[1:]:
                    self.m.check(b':' in line and not line.startswith((b' ', b'\t')), 'malformed HTTP header')
                    key, value = line.split(b':', 1)
                    key = key.lower()
                    self.m.check(key not in headers, 'duplicate HTTP header')
                    headers[key] = value.strip()
                self.m.check(headers.get(b'content-length') == b'2' and b'transfer-encoding' not in headers and
                             headers.get(b'connection', b'').lower() != b'close', 'unexpected HTTP framing')
                length = 2
            if header_end is not None and len(data) >= header_end + length:
                self.m.check(len(data) == header_end + length and bytes(data[header_end:]) == b'OK',
                             'unexpected ping response body or trailing bytes')
                return {'pid': sender[0], 'uid': sender[1], 'gid': sender[2]}

    def bootstrap(self):
        self.guard()
        if self.daemon is None:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(10)
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
            path = os.stat('/var/run/docker.sock', follow_symlinks=False)
            self.m.check(stat.S_ISSOCK(path.st_mode) and path.st_uid == 0 and
                         path.st_gid in os.getgroups() and not path.st_mode & 0o007, 'unsafe daemon socket')
            sock.connect('/var/run/docker.sock')
            self.daemon = {'socket': sock, 'socket_dev': path.st_dev, 'socket_ino': path.st_ino}
            try:
                first = self.ping()
                fd = os.pidfd_open(first['pid'])
                self.daemon.update(pid=first['pid'], pidfd=fd, sender=first)
                self.m.check(running(fd), 'daemon exited before second ping')
                second = self.ping()
                self.m.check(first == second and running(fd), 'daemon changed between pings')
                exe = os.open('/usr/bin/dockerd', os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
                self.daemon['exe_fd'] = exe
                exe_stat = os.fstat(exe)
                self.m.check(stat.S_ISREG(exe_stat.st_mode) and exe_stat.st_uid == 0 and
                             not exe_stat.st_mode & 0o022, 'unsafe daemon executable')
                self.daemon['first_ping'] = first
                self.daemon['second_ping'] = second
            except BaseException:
                for key in ('pidfd', 'exe_fd'):
                    if key in self.daemon:
                        os.close(self.daemon[key])
                sock.close()
                self.daemon = None
                raise
        return self.check_daemon()

    def check_daemon(self):
        self.m.check(self.daemon is not None, 'daemon bootstrap missing')
        daemon = self.daemon
        path = os.stat('/var/run/docker.sock', follow_symlinks=False)
        self.m.check((path.st_dev, path.st_ino) == (daemon['socket_dev'], daemon['socket_ino']) and
                     stat.S_ISSOCK(path.st_mode) and path.st_uid == 0 and path.st_gid in os.getgroups() and
                     not path.st_mode & 0o007 and running(daemon['pidfd']), 'daemon socket changed or daemon exited')
        sender = self.ping()
        self.m.check(sender == daemon['sender'] and running(daemon['pidfd']), 'daemon sender changed')
        return {'sender': sender, 'first_ping': daemon['first_ping'], 'second_ping': daemon['second_ping'],
                'pidfd_alive': True, 'socket': {'dev': path.st_dev, 'ino': path.st_ino}}

    def begin(self, cid, destination):
        self.m.check(self.target is None and destination == '/data', 'invalid or reentrant target begin')
        reader_cgroupns = self.check_reader()
        self.check_daemon()
        self.verify_owned(cid)
        inspected = self.inspect(cid)
        state = inspected['State']
        self.m.check(state['Running'] and state['Pid'] > 0 and state['StartedAt'], 'target not running')
        mounts = [x for x in inspected['Mounts'] if x['Destination'] == destination]
        self.m.check(len(mounts) == 1 and mounts[0]['Type'] == 'bind' and
                     os.path.isabs(mounts[0]['Source']) and os.path.normpath(mounts[0]['Source']) == mounts[0]['Source'],
                     'target destination is not an exact bind')
        pid = state['Pid']
        fd = os.pidfd_open(pid)
        try:
            self.m.check(running(fd), 'target exited before begin')
            stat_text = self.m.text('/proc/%d/stat' % pid)
            start = proc_stat(stat_text)
            self.m.check(start['pid'] == pid, 'target stat PID mismatch')
            mountinfo = self.m.text('/proc/%d/mountinfo' % pid)
            self.check_reader()
            self.m.check(len(mountinfo.encode()) <= 1024 * 1024, 'target mountinfo too large')
            # UID 10002 does not have permission to stat the target's namespace.
            self.m.check(running(fd), 'target exited during begin')
            self.target = {'id': cid, 'pid': pid, 'pidfd': fd, 'started_at': state['StartedAt'],
                           'source': mounts[0]['Source'], 'destination': destination, 'stat': start,
                           'mountinfo': mountinfo, 'reader_cgroupns': reader_cgroupns, 'helper': None}
        except BaseException:
            os.close(fd)
            raise
        return {'id': cid, 'pid': pid, 'started_at': state['StartedAt'], 'source': mounts[0]['Source'],
                'destination': destination, 'stat': start, 'pidfd_alive': True}

    def target_alive(self):
        t = self.target
        self.m.check(t is not None, 'no active target')
        self.check_daemon()
        if not running(t['pidfd']):
            return False
        current = self.inspect(t['id'])['State']
        return bool(current['Running'] and current['Pid'] == t['pid'] and current['StartedAt'] == t['started_at'])

    def finish(self):
        t = self.target
        self.m.check(t is not None, 'no active target')
        try:
            self.check_reader()
            self.m.check(t['reader_cgroupns'] == self.reader_cgroupns, 'target reader identity changed')
            if not self.target_alive():
                return {'accepted': False, 'reason': 'target_exited', 'pidfd_exited': not running(t['pidfd'])}
            current = proc_stat(self.m.text('/proc/%d/stat' % t['pid']))
            current_mountinfo = self.m.text('/proc/%d/mountinfo' % t['pid'])
            self.check_reader()
            self.m.check(current == t['stat'] and current_mountinfo == t['mountinfo'],
                         'target identity or mountinfo changed')
            h = t['helper']
            self.m.check(h is not None, 'successful helper output missing')
            idx = h['indexed']
            process = h['preparation']['raw_evidence']['target']
            view = h['preparation']['raw_evidence']['observer_view']
            self.m.check(h['preparation']['request']['reader_cgroupns'] == t['reader_cgroupns'],
                         'target and helper reader identities differ')
            self.m.check(process is not None and process['pid'] == t['pid'] and
                         process['starttime'] == t['stat']['starttime'] and
                         process['before_mountinfo'] == process['after_mountinfo'] == t['mountinfo'] and
                         proc_stat(idx['target_stat']['value']['text']) == t['stat'] and
                         idx['target_mountinfo']['value']['text'] == view['before']['metadata_mountinfo'] ==
                         view['after']['metadata_mountinfo'] and
                         idx['target_pidns']['value'] == process['pidns'] and running(t['pidfd']),
                         'helper target metadata changed')
            source, actual = idx['000:fstat']['value'], idx['001:fstat']['value']
            self.check_reader()
            match = (source['dev'], source['ino'], source['mode']) == (actual['dev'], actual['ino'], actual['mode'])
            return {'accepted': match, 'reason': 'match' if match else 'running_mount_mismatch',
                    'target_identity': t['stat'], 'pidfd_alive': True, 'mountinfo_stable': True,
                    'source': source, 'actual': actual}
        finally:
            os.close(t['pidfd'])
            self.target = None

    def watch_add(self, cid):
        self.m.check(cid not in self.watchers, 'consumer already watched')
        state = self.verify_owned(cid)['State']
        self.m.check(state['Running'] and state['Pid'] > 0 and state['StartedAt'], 'consumer not running')
        fd = os.pidfd_open(state['Pid'])
        if not running(fd):
            os.close(fd)
            raise self.m.Failure('consumer exited before pidfd hold')
        self.watchers[cid] = {'id': cid, 'pid': state['Pid'], 'started_at': state['StartedAt'], 'pidfd': fd}
        return self.watch_check(cid)

    def watch_check(self, cid):
        self.m.check(cid in self.watchers, 'consumer is not watched')
        watch = self.watchers[cid]
        state = self.verify_owned(cid)['State']
        alive = running(watch['pidfd'])
        self.m.check(alive and state['Running'] and state['Pid'] == watch['pid'] and
                     state['StartedAt'] == watch['started_at'], 'watched consumer changed or exited')
        return {'id': cid, 'pid': watch['pid'], 'started_at': watch['started_at'], 'pidfd_alive': True}

    def close_watches(self):
        for watch in self.watchers.values():
            os.close(watch['pidfd'])
        self.watchers.clear()

    def abort_target(self):
        if self.target is not None:
            os.close(self.target['pidfd'])
            self.target = None
        return {'aborted': True}

    def close(self):
        self.require_no_pending()
        self.abort_target()
        self.close_watches()
        if self.daemon:
            self.daemon['socket'].close()
            os.close(self.daemon['pidfd'])
            os.close(self.daemon['exe_fd'])
            self.daemon = None


class DirectDocker:
    def __init__(self, owner):
        self.owner = owner

    def run(self, args, data=None, receipt=None, timeout=90, required=True):
        request = {'op': 'docker', 'argv': args, 'timeout': timeout}
        if data is not None:
            request['input'] = base64.b64encode(data).decode()
        if receipt is not None:
            request['receipt'] = receipt
        result = self.owner.docker_request(request)
        if required:
            self.owner.m.check(result['returncode'] == 0, json.dumps(result))
        return result

    def inspect(self, cid):
        return self.owner.inspect(cid)

    def create(self, *args, **kwargs):
        return self.owner.m.Docker.create(self, *args, **kwargs)


def raw_capture_problem(result):
    try:
        raw = result['raw_output']
        if not isinstance(raw, dict) or raw.get('incomplete') is not False or raw.get('evidence_limit') is not False:
            return 'raw capture is missing or incomplete'
        stdout = base64.b64decode(raw['stdout_base64'], validate=True)
        stderr = base64.b64decode(raw['stderr_base64'], validate=True)
        if len(stdout) + len(stderr) > 1024 * 1024:
            return 'raw capture exceeded command limit'
        if stdout.decode('utf-8', 'replace') != result['stdout'] or stderr.decode('utf-8', 'replace') != result['stderr']:
            return 'raw capture does not match decoded output'
    except Exception as exc:
        return 'invalid raw capture: %s: %s' % (type(exc).__name__, str(exc)[:256])
    return None


def rejection_evidence(exc, receipt, request, result, inspected, final, capture_error):
    raw = result.get('raw_output', {})
    stdout = base64.b64decode(raw.get('stdout_base64', ''), validate=True)
    stderr = base64.b64decode(raw.get('stderr_base64', ''), validate=True)
    message = str(exc)
    rejection = {'type': type(exc).__name__, 'message': message[:2048],
                 'code': getattr(exc, 'code', None)}
    if hasattr(exc, 'detail'):
        rejection['detail'] = exc.detail
    summary = {'returncode': result['returncode'], 'stdout_bytes': len(stdout), 'stderr_bytes': len(stderr),
               'stdout_sha256': hashlib.sha256(stdout).hexdigest(), 'stderr_sha256': hashlib.sha256(stderr).hexdigest(),
               'output_in_receipt': True}
    limited = len(message) > 2048
    evidence = {'receipt': '/lab/control/' + receipt, 'request': request, 'rejection': rejection,
                'start_attach': {**summary, 'stdout_excerpt': stdout[:1024].decode('utf-8', 'replace'),
                                 'stderr_excerpt': stderr[:1024].decode('utf-8', 'replace'),
                                 'stdout_excerpt_incomplete': len(stdout) > 1024,
                                 'stderr_excerpt_incomplete': len(stderr) > 1024},
                'inspect': {'mounts': inspected['Mounts'], 'host_config': inspected['HostConfig'],
                            'apparmor': inspected.get('AppArmorProfile')},
                'final_state': final, 'incomplete': capture_error is not None or limited,
                'evidence_limit': raw.get('evidence_limit') is True or limited}
    encoded = json.dumps(evidence, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()
    if len(encoded) <= DIAGNOSTIC_LIMIT:
        return evidence
    compact = {'receipt': evidence['receipt'], 'request': request, 'rejection': rejection,
               'start_attach': summary,
               'inspect': {'mounts': inspected['Mounts'],
                           'configured_mounts': inspected['HostConfig'].get('Mounts')},
               'final_state': final, 'diagnostic_sha256': hashlib.sha256(encoded).hexdigest(),
               'incomplete': True, 'evidence_limit': True}
    if len(json.dumps(compact, sort_keys=True, separators=(',', ':'), ensure_ascii=True).encode()) <= DIAGNOSTIC_LIMIT:
        return compact
    return {'receipt': evidence['receipt'],
            'request': {key: request[key] for key in ('id', 'run_token', 'case', 'phase', 'profile')},
            'rejection': {'type': rejection['type'], 'message': rejection['message'][:512],
                          'code': rejection['code']},
            'start_attach': summary, 'diagnostic_sha256': hashlib.sha256(encoded).hexdigest(),
            'incomplete': True, 'evidence_limit': True}


class PreparationFailure(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


class AttachedObserver:
    """The owned CLI retains stdin until ready and drains both outputs through exit."""
    def __init__(self, owner, cid, deadline):
        self.owner, self.cid, self.deadline = owner, cid, deadline
        self.proc = None
        self.selector = selectors.DefaultSelector()
        self.stdout, self.stderr = bytearray(), bytearray()
        self.truncated = False
        owner.guard()
        owner.m.check(owner.launch.registered and not owner.launch.retired, 'guard registration missing')
        owner.launch.require_no_pending()
        owner.verify_owned(cid)
        owned = owner.created[cid]
        daemon = owner.fixed_daemon_info()
        starts = owned.get('starts', [])
        if starts:
            owner.m.check(same_json(daemon, starts[-1]['daemon']), 'daemon changed between starts')
        self.sequence = len(starts) + 1
        self.argv = ['start', '--attach', '--interactive', cid]
        self.full_argv = ['/usr/bin/docker', '--host=unix:///var/run/docker.sock'] + self.argv
        self.intent = {'id': cid, 'sequence': self.sequence, 'argv': self.argv, 'daemon': daemon}
        owner.save_new('start-intent-%s-%d.json' % (cid, self.sequence), self.intent)
        owner.commands.append(list(self.argv))
        try:
            self.proc = subprocess.Popen(self.full_argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                         stderr=subprocess.PIPE, env={**owner.m.ENV, 'HOME': '/lab/control'},
                                         start_new_session=True, bufsize=0)
            issued = {**self.intent, 'cli_pid': self.proc.pid}
            owner.save_new('start-issued-%s-%d.json' % (cid, self.sequence), issued)
            owned.setdefault('starts', []).append(issued)
            for stream in (self.proc.stdout, self.proc.stderr):
                os.set_blocking(stream.fileno(), False)
                self.selector.register(stream, selectors.EVENT_READ)
        except BaseException as exc:
            try:
                captured = self.abort()
                if captured is not None:
                    owner.save_new('attach-incomplete-%s-%d.json' % (cid, self.sequence),
                                   {'id': cid, 'sequence': self.sequence, 'argv': self.argv,
                                    'spec': owned['spec'], 'inspect_before': owned['inspect'],
                                    'error_type': type(exc).__name__, 'error': str(exc)[:2048],
                                    'capture': captured, 'incomplete': True})
                    if isinstance(exc, Exception):
                        exc.evidence = {'receipt': '/lab/control/attach-incomplete-%s-%d.json' % (cid, self.sequence),
                                        'incomplete': True,
                                        'evidence_limit': captured['raw_output']['evidence_limit']}
            except BaseException as capture_exc:
                unsafe = owner.m.GuardUnsafe('attach startup capture or exit uncertain: ' + str(capture_exc))
                unsafe.incomplete = True
                raise unsafe from capture_exc
            raise

    def remaining(self):
        left = self.deadline - time.monotonic()
        if left <= 0:
            raise PreparationFailure('observer_not_ready', 'helper deadline exceeded')
        return left

    def pump(self, timeout):
        for key, _ in self.selector.select(timeout):
            try:
                data = os.read(key.fd, 65536)
            except BlockingIOError:
                continue
            if not data:
                self.selector.unregister(key.fileobj)
                continue
            target = self.stdout if key.fileobj is self.proc.stdout else self.stderr
            room = 1024 * 1024 - len(self.stdout) - len(self.stderr)
            target.extend(data[:max(0, room)])
            if len(data) > room:
                if not self.truncated:
                    self.truncated = True
                    raise PreparationFailure('observer_not_ready', 'helper output limit exceeded')

    def wait_for_waiting(self, case, phase, mode, nonce, input_digest, apparmor):
        while b'\n' not in self.stdout:
            if self.proc.stdout not in self.selector.get_map():
                raise PreparationFailure('observer_not_ready', 'observer exited before waiting')
            self.pump(self.remaining())
            if len(self.stdout) > preparation.MAX_FRAME:
                raise PreparationFailure('observer_not_ready', 'waiting record too large')
        self.pump(0)
        line, rest = bytes(self.stdout).split(b'\n', 1)
        self.owner.m.check(not rest and len(line) <= preparation.MAX_FRAME, 'extra output before ready')
        record = strict_json(line)
        self.owner.m.check(type(record) is dict and set(record) ==
            {'experiment_kind', 'preparation_version', 'metadata_mode', 'case', 'phase', 'root_ordinal',
             'operation', 'errno', 'value'} and record['experiment_kind'] == 'input-handoff-v1' and
            type(record['preparation_version']) is int and record['preparation_version'] == 1 and
            record['metadata_mode'] == mode and record['case'] == case and record['phase'] == phase and
            record['root_ordinal'] is None and record['operation'] == 'waiting' and type(record['errno']) is int and
            record['errno'] == 0, 'invalid waiting record')
        value = record['value']
        self.owner.m.check(type(value) is dict and set(value) == {'nonce', 'input_digest', 'self_status'} and
                           value['nonce'] == nonce and value['input_digest'] == input_digest,
                           'waiting identity mismatch')
        if apparmor:
            check_observer_status(self.owner.m, value['self_status'], apparmor)
        return value

    def send_ready(self, ready):
        self.send_payload(preparation.canonical_json(ready) + b'\n')

    def send_payload(self, data):
        self.owner.m.check(type(data) is bytes and len(data) <= 4096, 'ready exceeds bound')
        self.pump(0)
        self.owner.m.check(self.stdout.count(b'\n') == 1 and self.stdout.endswith(b'\n') and
                           self.proc.poll() is None, 'observer emitted or exited before ready')
        os.set_blocking(self.proc.stdin.fileno(), False)
        view = memoryview(data)
        while view:
            _, writable, _ = select.select([], [self.proc.stdin], [], self.remaining())
            if not writable:
                raise PreparationFailure('observer_not_ready', 'ready write deadline')
            try:
                count = os.write(self.proc.stdin.fileno(), view)
            except BlockingIOError:
                continue
            if count <= 0:
                raise PreparationFailure('observer_not_ready', 'ready write failed')
            view = view[count:]
        self.proc.stdin.close()

    def result(self, incomplete=False):
        raw_out, raw_err = bytes(self.stdout), bytes(self.stderr)
        return {'argv': self.full_argv, 'returncode': self.proc.returncode,
                'stdout': raw_out.decode('utf-8', 'replace'), 'stderr': raw_err.decode('utf-8', 'replace'),
                'raw_output': {'stdout_base64': base64.b64encode(raw_out).decode('ascii'),
                               'stderr_base64': base64.b64encode(raw_err).decode('ascii'),
                               'incomplete': incomplete, 'evidence_limit': self.truncated}}

    def finish(self):
        while self.selector.get_map():
            self.pump(self.remaining())
        self.proc.wait(timeout=self.remaining())
        if not self.proc.stdin.closed:
            self.proc.stdin.close()
        self.proc.stdout.close()
        self.proc.stderr.close()
        self.selector.close()
        result = self.result()
        self.owner.save_new('start-result-%s-%d.json' % (self.cid, self.sequence),
                            {**self.intent, 'returncode': result['returncode']})
        return result

    def abort(self):
        if self.proc is None:
            self.selector.close()
            return None
        if self.proc.stdin and not self.proc.stdin.closed:
            self.proc.stdin.close()
        if self.proc.poll() is None:
            try:
                os.killpg(self.proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        try:
            self.proc.wait(timeout=5)
            while self.selector.get_map():
                self.pump(0)
                if self.selector.get_map() and not any(select.select([key.fileobj], [], [], 0)[0]
                                                       for key in self.selector.get_map().values()):
                    break
        finally:
            self.proc.stdout.close()
            self.proc.stderr.close()
            self.selector.close()
        return self.result(incomplete=True)


def custody_graph(text):
    graph = {}
    for line in text.splitlines():
        parts = line.split()
        if len(parts) < 10 or '-' not in parts:
            raise ValueError('invalid custody mount graph')
        sep = parts.index('-')
        if sep < 6 or len(parts) != sep + 4:
            raise ValueError('invalid custody mount fields')
        mid, parent = int(parts[0]), int(parts[1])
        if mid <= 0 or parent <= 0 or mid in graph:
            raise ValueError('duplicate or invalid custody mount ID')
        graph[mid] = {'parent_id': parent, 'major_minor': parts[2], 'root': unescape_mount(parts[3]),
                      'point': unescape_mount(parts[4]), 'options': parts[5].split(','),
                      'optional': parts[6:sep], 'fs_type': parts[sep + 1],
                      'source': unescape_mount(parts[sep + 2]), 'super_options': parts[sep + 3].split(',')}
        row = graph[mid]
        if not preparation.clean_absolute_path(row['point']):
            raise ValueError('invalid custody mount point')
        if not (preparation.clean_absolute_path(row['root']) or
                preparation.namespace_root(row['root'], row['fs_type']) or
                preparation.cgroup_ancestor_root(row['root'], row['fs_type'])):
            raise ValueError('invalid custody mount root')
    if not graph:
        raise ValueError('empty custody mount graph')
    return graph


def check_custody_subtree(m, graph, path, subtree):
    descendants = {mid for mid, entry in graph.items() if entry['point'].startswith(path.rstrip('/') + '/')}
    recorded = {row['mount_id'] for row in subtree if row['relative'] != '.'}
    at_root = {mid for mid, entry in graph.items() if entry['point'] == path}
    m.check(descendants == recorded and (not at_root or at_root == {subtree[0]['mount_id']}),
            'mount subtree omitted or stacked in raw graph')
    for row in subtree:
        actual = graph.get(row['mount_id'])
        m.check(actual is not None, 'mount custody ID missing')
        m.check(preparation.clean_absolute_path(actual['root']) and
                preparation.clean_absolute_path(row['root']), 'mount custody root is not a filesystem path')
        expected_path = path if row['relative'] == '.' else path + '/' + row['relative']
        if row['relative'] == '.':
            point = actual['point']
            m.check(expected_path == point or expected_path.startswith(point.rstrip('/') + '/'),
                    'mount custody root is outside mount')
            suffix = expected_path[len(point):].lstrip('/') if point != '/' else expected_path.lstrip('/')
            root = os.path.normpath(os.path.join(actual['root'], suffix))
        else:
            m.check(actual['point'] == expected_path, 'mount custody descendant path mismatch')
            root = actual['root']
        m.check(root == row['root'] and all(actual[key] == row[key] for key in
                ('parent_id', 'major_minor', 'fs_type', 'source', 'options', 'optional', 'super_options')),
                'mount custody record differs from raw graph')


def verify_custody(m, result, evidence, indexed, metadata, exe):
    daemon = custody_graph(evidence['daemon']['before_mountinfo'])
    helper_before = custody_graph(evidence['helper']['before_mountinfo'])
    helper_after = custody_graph(evidence['helper']['after_mountinfo'])
    target = custody_graph(evidence['target']['before_mountinfo']) if evidence['target'] else None
    for source, attached in zip(result['sources'], result['attached']):
        source_graph = target if source['role'] == 'running_root' else daemon
        check_custody_subtree(m, source_graph, source['source_path'], source['subtree'])
        check_custody_subtree(m, helper_before, attached['target'], attached['before']['subtree'])
        check_custody_subtree(m, helper_after, attached['target'], attached['subtree'])
        if source['role'] != 'daemon_exe' and indexed is not None:
            ordinal = '%03d' % source['ordinal']
            observed = indexed[(ordinal, 'fstat')]['value']
            sx = indexed[(ordinal, 'statx')]['value']
            m.check(all(observed[key] == attached[key] for key in ('dev', 'ino', 'mode')) and
                    sx['mount_id'] == attached['mount_id'], 'observer root differs from attached custody')
        if source['role'] == 'daemon_exe' and indexed is not None:
            observed = indexed[(None, 'target_exe')]['value']
            m.check(observed == {key: attached[key] for key in ('dev', 'ino')}, 'observer executable differs')
    if metadata == 'proc-v1':
        process = evidence['daemon'] if exe else evidence['target']
        m.check(process is not None, 'pinned pid namespace process missing')
        preparation.identity(process['pidns'], 'pinned pid namespace')
        pidns = process['pidns']
        expected_root = 'pid:[%d]' % pidns['ino']
        expected_device = '%d:%d' % (os.major(pidns['dev']), os.minor(pidns['dev']))
        for graph in (helper_before, helper_after):
            rows = [row for row in graph.values() if row['point'] == '/meta/target-pidns']
            m.check(len(rows) == 1, 'pid namespace mount missing or stacked')
            row = rows[0]
            m.check(row['fs_type'] == 'nsfs' and row['root'] == expected_root and
                    row['major_minor'] == expected_device, 'pid namespace mount identity differs')
            m.check('ro' in row['options'] and 'rw' not in row['options'] and
                    not any(field == 'unbindable' or field.startswith(('shared:', 'master:', 'propagate_from:'))
                            for field in row['optional']), 'pid namespace mount is not readonly and private')
    if indexed is not None:
        m.check(indexed[(None, 'mountinfo')]['value']['text'] ==
                evidence['observer_view']['after']['helper_mountinfo'],
                'observer graph differs from common final graph')
        if metadata == 'proc-v1':
            m.check(indexed[(None, 'target_pidns')]['value'] == process['pidns'],
                    'observer pid namespace differs from pinned namespace')
            m.check(indexed[(None, 'target_mountinfo')]['value']['text'] ==
                    evidence['observer_view']['before']['metadata_mountinfo'] ==
                    evidence['observer_view']['after']['metadata_mountinfo'],
                    'observer metadata differs from common graphs')


def prepare_observer(owner, cid, input_value, waiting, state, helper_pidfd, deadline, negative=None):
    m, launch_client = owner.m, owner.launch
    daemon = owner.daemon
    target = owner.target if input_value['metadata_pid'] is not None and not input_value['exe'] else None
    target_request = None if target is None else {'id': target['id'], 'pid': target['pid'],
                      'starttime': target['stat']['starttime'], 'destination': target['destination']}
    reader_cgroupns = owner.check_reader()
    coordinator_namespaces = {}
    for name in ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'):
        namespace = os.stat('/proc/self/ns/' + name)
        coordinator_namespaces[name] = {'dev': namespace.st_dev, 'ino': namespace.st_ino}
    m.check(coordinator_namespaces['cgroup'] == reader_cgroupns, 'coordinator reader namespace differs')
    if target is not None:
        m.check(target['reader_cgroupns'] == reader_cgroupns, 'target baseline reader changed')
    helper_stat = proc_stat(m.text('/proc/%d/stat' % state['Pid']))
    m.check(helper_stat['pid'] == state['Pid'] and running(helper_pidfd), 'waiting helper identity changed')
    daemon_stat = proc_stat(m.text('/proc/%d/stat' % daemon['pid']))
    daemon_mountinfo = m.text('/proc/%d/mountinfo' % daemon['pid'])
    owner.check_reader()
    req = {'version': 1, 'op': 'prepare', 'input': input_value,
           'input_digest': preparation.input_digest(input_value), 'reader_cgroupns': reader_cgroupns,
           'helper': {'id': cid, 'pid': state['Pid'], 'starttime': helper_stat['starttime'],
                      'mntns': waiting['self_status']['namespaces']['mnt'],
                      'pidns': waiting['self_status']['namespaces']['pid'],
                      'cgroupns': waiting['self_status']['namespaces']['cgroup']},
           'daemon': {'pid': daemon['pid']}, 'target': target_request}
    req['request_digest'] = preparation.request_digest(req)
    preparation.validate_request(req)
    preparation.validate_request_namespaces(req, coordinator_namespaces, waiting)
    ordinal, nonce = input_value['ordinal'], input_value['nonce']
    pending = {'version': 1, 'request': req, 'create_spec': owner.created[cid]['spec'],
               'observer_waiting': waiting, 'helper_state': state,
               'coordinator_namespaces': coordinator_namespaces}
    owner.save_new('prepare-pending-%06d.json' % ordinal, pending)
    client, worker = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET | socket.SOCK_CLOEXEC)
    client.setsockopt(socket.SOL_SOCKET, socket.SO_PASSCRED, 1)
    pidfd = None
    launch = exited = result = raw = None
    problem = None
    problem_reason = 'prepare_protocol'
    receipt = receipt_name = receipt_sha = None
    fault = {'variant': negative, 'sent': False, 'sender_extra_fd_closed': None}
    try:
        try:
            remaining = deadline - time.monotonic()
            m.check(remaining > 0, 'helper deadline expired before preparer launch')
            launch, pidfd = launch_client.launch(ordinal, nonce, req['request_digest'], worker.fileno(), req,
                                                 timeout=min(5, remaining), deadline=deadline)
            owner.preparation_ordinal = ordinal
        except BaseException as exc:
            raise m.GuardUnsafe('preparer launch ownership uncertain: ' + str(exc)) from exc
        finally:
            worker.close()
        fds = [daemon['pidfd'], helper_pidfd] + ([target['pidfd']] if target else [])
        extra = os.dup(helper_pidfd) if negative == 'extra_fd' else None
        def send_request(channel, original, originals):
            if negative == 'wrong_input_digest':
                changed = ('0' if original['input_digest'][0] != '0' else '1') + original['input_digest'][1:]
                preparation.send_frame(channel, {**original, 'input_digest': changed}, originals)
            elif negative == 'extra_fd':
                preparation.send_frame(channel, original, originals + [extra])
            else:
                preparation.send_prepare(channel, original, originals)
            fault['sent'] = True
        try:
            result, raw, exited, problem = launch_client.exchange(
                client, req, fds, pidfd, deadline, owner.save_new, send_request=send_request)
        except BaseException as exc:
            raise m.GuardUnsafe('preparer or capture ownership uncertain: ' + str(exc)) from exc
        finally:
            if extra is not None:
                os.close(extra)
                fault['sender_extra_fd_closed'] = True
        try:
            owner.check_reader()
        except PreparationFailure as exc:
            problem = {'type': type(exc).__name__, 'message': str(exc)[:2048]}
            problem_reason = exc.code
        evidence = None if result is None else preparation.validate_result(
            result, raw, req, launch['pid'], launch_client.session_nonce)
        captures = launch_client.records[-1]['captures']
        reconciliation_error = None
        try:
            preparation.validate_capture_ledger(captures, req, launch_client.session_nonce, evidence,
                success=result is not None and result['status'] == 'prepared', preparer_exe=launch['exe'],
                root_pid=launch_client.parent_pid, coordinator_namespaces=coordinator_namespaces,
                preparer_pid=launch['pid'])
        except preparation.ProtocolError as exc:
            reconciliation_error = exc
            problem = {'type': type(exc).__name__, 'message': str(exc)[:2048]}
        receipt = {'version': 1, 'request': req, 'create_spec': owner.created[cid]['spec'],
                   'observer_waiting': waiting, 'helper_state': state, 'launch': launch, 'exit': exited,
                   'captures': captures, 'pidfd_exited': True, 'result': result, 'raw_evidence': evidence,
                   'channel_error': problem, 'test_fault': fault if negative else None,
                   'coordinator_namespaces': coordinator_namespaces}
        if len(preparation.canonical_json(receipt)) + 1 > preparation.MAX_EVIDENCE:
            raise m.GuardUnsafe('preparation receipt exceeded aggregate evidence limit')
        receipt_name = 'prepare-result-%06d.json' % ordinal
        try:
            owner.save_new(receipt_name, receipt)
            receipt_sha = m.digest('/lab/control/' + receipt_name)
        except BaseException as exc:
            raise m.GuardUnsafe('preparation receipt persistence uncertain: ' + str(exc)) from exc
        if reconciliation_error is not None:
            raise m.GuardUnsafe('capture identity reconciliation failed: ' + str(reconciliation_error))
        if negative:
            expected_error = ('prepare_protocol: input digest mismatch' if negative == 'wrong_input_digest'
                              else 'prepare_protocol: wrong pidfd count')
            m.check(negative in ('wrong_input_digest', 'extra_fd') and fault['sent'] and result is None and
                    problem is not None and exited['exit_code'] is not None and exited['exit_code'] != 0 and
                    expected_error in exited['capture'] and
                    (negative != 'extra_fd' or fault['sender_extra_fd_closed'] is True),
                    'malformed preparation was not refused at the expected boundary')
            failure = PreparationFailure('prepare_protocol', 'fixed malformed preparation was rejected')
            failure.preparation_evidence = {'receipt': '/lab/control/' + receipt_name, 'launch': launch,
                'exit': exited, 'channel_error': problem, 'pidfd_exited': True, 'test_fault': fault}
            raise failure
        if problem is not None or result is None:
            failure = PreparationFailure(problem_reason, 'preparer result missing or invalid')
            failure.preparation_evidence = {'receipt': '/lab/control/' + receipt_name, 'launch': launch,
                                            'exit': exited, 'channel_error': problem, 'pidfd_exited': True}
            raise failure
        if exited['exit_code'] != 0:
            failure = PreparationFailure('prepare_exit_uncertain', 'preparer did not exit successfully')
            failure.preparation_evidence = {'receipt': '/lab/control/' + receipt_name, 'launch': launch,
                                            'exit': exited, 'pidfd_exited': True}
            raise failure
        if result['status'] != 'prepared':
            failure = PreparationFailure(result['reason'], 'preparer rejected the input')
            failure.preparation_evidence = {'receipt': '/lab/control/' + receipt_name, 'launch': launch,
                                            'exit': exited, 'pidfd_exited': True}
            raise failure
        preparation.validate_reader_context(evidence, req, result['preparer'], coordinator_namespaces, waiting)
        owner.check_reader()
        m.check(all(result['preparer']['exe'][key] == launch['exe'][key] for key in ('dev', 'ino', 'mode')),
                'preparer executed object differs from launcher')
        m.check(evidence['daemon']['starttime'] == daemon_stat['starttime'] and
                evidence['daemon']['before_mountinfo'] == evidence['daemon']['after_mountinfo'] == daemon_mountinfo and
                proc_stat(m.text('/proc/%d/stat' % daemon['pid'])) == daemon_stat and
                m.text('/proc/%d/mountinfo' % daemon['pid']) == daemon_mountinfo,
                'daemon source identity or mount graph changed')
        if target is not None:
            m.check(owner.target_alive() and evidence['target']['before_mountinfo'] == target['mountinfo'] and
                    evidence['target']['after_mountinfo'] == target['mountinfo'], 'target source identity changed')
        owner.check_reader()
        owner.check_daemon()
        m.check(running(helper_pidfd) and proc_stat(m.text('/proc/%d/stat' % state['Pid'])) == helper_stat,
                'helper exited during preparation')
        verify_custody(m, result, evidence, None,
                       'proc-v1' if input_value['metadata_pid'] is not None else 'none', input_value['exe'])
        return {'version': 1, 'receipt': '/lab/control/' + receipt_name, 'receipt_sha256': receipt_sha,
                'request': req, 'launch': launch, 'exit': exited, 'captures': captures, 'pidfd_exited': True,
                'result': result, 'raw_evidence': evidence, 'waiting': waiting,
                'create_spec': owner.created[cid]['spec'], 'helper_state': state,
                'coordinator_namespaces': coordinator_namespaces}
    except Exception as exc:
        if receipt is not None and receipt_name is not None and receipt_sha is not None:
            failure_evidence = getattr(exc, 'preparation_evidence', None) or {}
            failure_evidence.update(receipt='/lab/control/' + receipt_name, receipt_sha256=receipt_sha,
                                    receipt_value=receipt)
            exc.preparation_evidence = failure_evidence
        raise
    finally:
        client.close()
        if pidfd is not None:
            os.close(pidfd)


def helper(owner, image, profile, case, phase, roots, metadata_pid=None, exe=False, nested=False, deny=False):
    return _helper(owner, image, profile, case, phase, roots, metadata_pid, exe, nested, deny)


def _helper(owner, image, profile, case, phase, roots, metadata_pid=None, exe=False, nested=False, deny=False,
            negative=None):
    m = owner.m
    m.check(profile in ('A', 'B') and case in CASES and phase in ('before', 'after') and
            1 <= len(roots) <= MAX_ROOTS and re.fullmatch('sha256:[0-9a-f]{64}', image),
            'invalid helper request')
    m.check(not exe or (metadata_pid is not None and case == 'I0'), 'invalid daemon metadata request')
    m.check(not nested or case == 'V2', 'invalid nested request')
    owner.guard()
    before_volumes = owner.volume_list()
    expected, preparation_roots = [], []
    options = m.base_options() + ['--cap-add', 'DAC_READ_SEARCH', '--interactive']
    if profile == 'B':
        options += ['--security-opt', 'seccomp=' + m.SEED + '/probe-seccomp.json']
    for ordinal, root in enumerate(roots):
        target = '/roots/%03d' % ordinal
        m.check(isinstance(root, dict) and set(root) in ({'type', 'source'}, {'type', 'name'}, {'type'}),
                'invalid typed helper root')
        if root['type'] == 'volume':
            name = root.get('name')
            if name not in owner.volumes:
                raise UnknownVolumeAdmission()
            m.check(owner.volume_inspect(name) == owner.volumes[name] and name in before_volumes,
                    'volume not previously registered or changed')
            options += ['--mount', 'type=volume,source=%s,target=%s,readonly,volume-nocopy' % (name, target)]
            expected.append({'type': 'volume', 'name': name, 'source': owner.volumes[name]['Mountpoint'],
                             'target': target})
            preparation_roots.append({'kind': 'native_volume', 'ordinal': ordinal, 'target': target,
                                      'name': name, 'mountpoint': owner.volumes[name]['Mountpoint']})
        elif root['type'] == 'bind':
            source = root.get('source')
            m.check(set(root) == {'type', 'source'} and isinstance(source, str) and
                    os.path.isabs(source) and os.path.normpath(source) == source and source.startswith('/lab/'),
                    'invalid exact bind source')
            options += m.root_mount(source, target)
            expected.append({'type': 'bind', 'source': source, 'target': target})
            preparation_roots.append({'kind': 'declared_bind', 'ordinal': ordinal, 'target': target,
                                      'source': source})
        elif root['type'] == 'running_root':
            m.check(set(root) == {'type'} and ordinal == 1 and owner.target is not None and
                    metadata_pid == owner.target['pid'] and case in ('I1', 'I2', 'I3'),
                    'invalid deferred running root')
            preparation_roots.append({'kind': 'running_root', 'ordinal': ordinal, 'target': target})
        else:
            raise m.Failure('unsupported mount type')
    if metadata_pid is not None:
        m.check(type(metadata_pid) is int and metadata_pid > 0, 'invalid metadata PID')
        for source, target in (('stat', 'target-stat'), ('mountinfo', 'target-mountinfo'),
                               ('ns/pid', 'target-pidns')):
            path = '/proc/%d/%s' % (metadata_pid, source)
            destination = '/meta/' + target
            options += m.root_mount(path, destination)
            expected.append({'type': 'bind', 'source': path, 'target': destination})
        # The exact executable is installed from the daemon's held proc FD by the preparer.
    mode = 'proc-v1' if metadata_pid is not None else 'none'
    args = ['prepared-denycheck'] if deny else ['prepared-observe'] + ['/roots/%03d' % i for i in range(len(roots))]
    owner.bootstrap()
    owner.check_reader()
    daemon_before = proc_stat(m.text('/proc/%d/stat' % owner.daemon['pid']))
    m.check(daemon_before['pid'] == owner.daemon['pid'], 'daemon baseline identity differs')
    daemon_mounts_before = m.text('/proc/%d/mountinfo' % owner.daemon['pid'])
    owner.check_reader()
    daemon_lifecycle = {}
    m.check(owner.launch is not None and owner.launch.registered and not owner.launch.retired and
            owner.preparation_ordinal == owner.launch.last_ordinal, 'preparation session is not ready')
    ordinal = owner.preparation_ordinal + 1
    nonce = os.urandom(32).hex()
    input_value = {'version': 1, 'run_token': m.RESULT['run_token'], 'ordinal': ordinal, 'nonce': nonce,
                   'imageID': image, 'case': case, 'phase': phase, 'profile': profile,
                   'roots': preparation_roots, 'metadata_pid': metadata_pid, 'exe': exe, 'nested': nested,
                   'deny': deny, 'preparer_sha256': m.PREPARER_SHA256}
    preparation.validate_input(input_value)
    input_sha = preparation.input_digest(input_value)
    name = 'g2-%s-%s-%s-%s-%s' % (m.RESULT['run_token'], case.lower(), profile.lower(), phase,
                                  'deny' if deny else 'observe')
    before_ids = set(owner.created)
    cid = None
    spec_verified = False
    primary_error = None
    receipt = None
    receipt_saved = False
    result = None
    final = None
    final_inspect = None
    running_inspect = None
    capture_error = None
    attached = None
    helper_pidfd = None
    helper_pidfd_exited = None
    prepared = None
    request = {'run_token': m.RESULT['run_token'], 'image': image, 'profile': profile, 'case': case,
               'phase': phase, 'roots': roots, 'metadata_pid': metadata_pid, 'exe': exe,
               'nested': nested, 'deny': deny}
    try:
        cid = owner.docker.create(name, image, options, args, case, phase, mode, exe, nested,
                                  prepare_nonce=nonce, prepare_input_digest=input_sha)
        inspected = owner.verify_owned(cid)
        hc = inspected['HostConfig']
        m.check(hc['NetworkMode'] == 'none' and hc['ReadonlyRootfs'] is True and
                hc['CapDrop'] == ['ALL'] and hc['CapAdd'] == ['CAP_DAC_READ_SEARCH'] and
                hc['PidsLimit'] == 64 and hc['Memory'] == 268435456 and hc['MemorySwap'] == 268435456 and
                hc['NanoCpus'] == 1000000000 and hc['LogConfig']['Type'] == 'none' and
                hc['PidMode'] == '' and hc['IpcMode'] == 'private' and hc['UTSMode'] == '' and
                hc['CgroupnsMode'] == 'private' and hc['AutoRemove'] is False and
                not hc.get('Privileged') and not hc.get('Devices') and not hc.get('Binds') and
                not hc.get('PortBindings') and not hc.get('PublishAllPorts'), 'helper isolation mismatch')
        security = hc.get('SecurityOpt') or []
        m.check(any(x in ('no-new-privileges', 'no-new-privileges=true') for x in security) and
                not any('unconfined' in x for x in security) and
                inspected.get('AppArmorProfile') not in ('unconfined',), 'helper security profile mismatch')
        profiles = [x[len('seccomp='):] for x in security if x.startswith('seccomp=')]
        if profile == 'A':
            m.check(not profiles, 'unexpected builtin seccomp override')
        else:
            m.check(len(profiles) == 1 and strict_json(profiles[0]) == strict_json(m.text(m.SEED + '/probe-seccomp.json')),
                    'custom seccomp profile differs')
        actual = inspected['Mounts']
        m.check(len(actual) == len(expected) and {x['Destination'] for x in actual} ==
                {x['target'] for x in expected}, 'unexpected helper mount set')
        configured = hc.get('Mounts') or []
        m.check(len(configured) == len(expected), 'helper HostConfig mount set incomplete')
        for item in expected:
            actual_mounts = [x for x in actual if x['Destination'] == item['target']]
            config_mounts = [x for x in configured if x['Target'] == item['target']]
            m.check(len(actual_mounts) == len(config_mounts) == 1, 'ambiguous helper mount')
            mounted, config = actual_mounts[0], config_mounts[0]
            m.check(mounted['Type'] == item['type'] and mounted['Source'] == item['source'] and
                    mounted['RW'] is False and config['Type'] == item['type'] and config.get('ReadOnly') is True,
                    'helper mount source or readonly mismatch')
            if item['type'] == 'volume':
                m.check(mounted.get('Name') == item['name'] and config['Source'] == item['name'] and
                        (config.get('VolumeOptions') or {}).get('NoCopy') is True,
                        'volume name or NoCopy mismatch')
            else:
                m.check(mounted.get('Propagation') == 'rprivate' and config['Source'] == item['source'],
                        'bind propagation or source mismatch')
        spec_verified = True
        if owner.target is not None and metadata_pid == owner.target['pid']:
            m.check(owner.target_alive(), 'target_exited_before_helper_start')
        request['id'] = cid
        receipt = 'observation-' + cid + '.json'
        deadline = time.monotonic() + 90
        owner.helper_deadline = deadline
        attached = AttachedObserver(owner, cid, deadline)
        waiting = attached.wait_for_waiting(case, phase, mode, nonce, input_sha, inspected.get('AppArmorProfile'))
        running_inspect = owner.verify_owned(cid)
        state = running_inspect['State']
        m.check(state['Running'] and type(state['Pid']) is int and state['Pid'] > 0 and state['StartedAt'],
                'waiting helper is not running')
        check_observer_status(m, waiting['self_status'], running_inspect.get('AppArmorProfile'))
        if negative is not None:
            m.check(state['Running'] is True and state['Status'] == 'running' and
                    type(state['StartedAt']) is str and state['StartedAt'] != '0001-01-01T00:00:00Z' and
                    not any(option.lower().startswith('apparmor') for option in security) and
                    waiting['self_status']['apparmor']['label'] ==
                    running_inspect['AppArmorProfile'] + ' (enforce)',
                    'negative observer running identity or default AppArmor mismatch')
        helper_pidfd = os.pidfd_open(state['Pid'])
        m.check(running(helper_pidfd), 'waiting helper already exited')
        if owner.target is not None and metadata_pid == owner.target['pid']:
            m.check(owner.target_alive(), 'target exited before preparation')
        if negative in ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce'):
            bad_nonce = ('0' if nonce[0] != '0' else '1') + nonce[1:]
            invalid_ready = {'version': 1, 'nonce': bad_nonce if negative == 'wrong_nonce' else nonce,
                             'input_digest': input_sha, 'request_digest': '0' * 64,
                             'prepare_receipt_sha256': '0' * 64, 'action': 'observe'}
            payload = preparation.canonical_json(invalid_ready) + b'\n'
            if negative == 'empty_eof':
                payload = b''
            elif negative == 'truncated_eof':
                payload = payload[:len(payload) // 2]
            owner.save_new('observer-negative-%s.json' % cid, {'variant': negative, 'helper_id': cid,
                           'stdin_base64': None if negative == 'no_ready' else base64.b64encode(payload).decode(),
                           'waiting': waiting})
            stop_result = None
            if negative == 'no_ready':
                stop_result = owner.docker.run(['stop', '--timeout', '10', cid], timeout=30)
            else:
                attached.send_payload(payload)
            result = attached.finish()
            attached = None
            final_inspect = owner.verify_owned(cid)
            final = final_inspect['State']
            capture_error = raw_capture_problem(result)
            rows = [strict_json(line) for line in result['stdout'].splitlines()]
            helper_pidfd_exited = not running(helper_pidfd)
            m.check(capture_error is None and result['returncode'] != 0 and helper_pidfd_exited and
                    final['Status'] == 'exited' and final['Running'] is False and final['ExitCode'] != 0 and
                    len(rows) == 1 and rows[0]['operation'] == 'waiting' and rows[0]['value'] == waiting,
                    'negative observer did not stop after its sole waiting record')
            owner.save_new(receipt, {'request': request, 'start_attach': result, 'inspect_before': inspected,
                'running_inspect': running_inspect, 'inspect_after': final_inspect, 'inspection_error': None,
                'capture_error': None, 'incomplete': False, 'evidence_limit': False, 'test_variant': negative,
                'stop': stop_result, 'helper_pidfd_exited': helper_pidfd_exited})
            receipt_saved = True
            raise PreparationFailure('observer_not_ready', 'fixed observer negative was rejected')
        prepared = prepare_observer(owner, cid, input_value, waiting, state, helper_pidfd, deadline,
                                    negative if negative in ('wrong_input_digest', 'extra_fd') else None)
        again = owner.verify_owned(cid)['State']
        m.check(running(helper_pidfd) and again['Running'] and again['Pid'] == state['Pid'] and
                again['StartedAt'] == state['StartedAt'], 'waiting helper changed before ready')
        owner.check_daemon()
        owner.check_reader()
        if owner.target is not None and metadata_pid == owner.target['pid']:
            m.check(owner.target_alive(), 'target exited before ready')
        ready = {'version': 1, 'nonce': nonce, 'input_digest': input_sha,
                 'request_digest': prepared['request']['request_digest'],
                 'prepare_receipt_sha256': prepared['receipt_sha256'], 'action': 'denycheck' if deny else 'observe'}
        ready_name = 'ready-intent-%06d.json' % ordinal
        owner.save_new(ready_name, {'helper_id': cid, 'ready': ready, 'preparation_receipt': prepared['receipt']})
        prepared['ready'] = ready
        prepared['ready_intent'] = '/lab/control/' + ready_name
        ready_text = m.text(prepared['ready_intent'])
        ready_value = strict_json(ready_text)
        m.check(ready_value == {'helper_id': cid, 'ready': ready, 'preparation_receipt': prepared['receipt']} and
                ready_text.encode('utf-8') == preparation.canonical_json(ready_value) + b'\n',
                'persisted ready intent changed')
        prepared['ready_intent_value'] = ready_value
        prepared['ready_intent_sha256'] = hashlib.sha256(ready_text.encode('utf-8')).hexdigest()
        attached.send_ready(ready)
        result = attached.finish()
        attached = None
        helper_pidfd_exited = not running(helper_pidfd)
        m.check(helper_pidfd_exited, 'helper process did not exit with attached CLI')
        capture_error = raw_capture_problem(result)
        final_inspect = None
        inspection_error = None
        try:
            final_inspect = owner.verify_owned(cid)
            final = final_inspect['State']
        except BaseException as exc:
            inspection_error = exc
        try:
            owner.save_new(receipt, {'request': request, 'start_attach': result, 'inspect_before': inspected,
                                     'running_inspect': running_inspect,
                                     'inspect_after': final_inspect,
                                     'inspection_error': None if inspection_error is None else
                                     {'type': type(inspection_error).__name__, 'message': str(inspection_error)[:2048]},
                                     'capture_error': capture_error,
                                     'helper_pidfd_exited': helper_pidfd_exited,
                                     'incomplete': inspection_error is not None or capture_error is not None,
                                     'evidence_limit': isinstance(result.get('raw_output'), dict) and
                                     result['raw_output'].get('evidence_limit') is True})
            receipt_saved = True
        except BaseException as receipt_error:
            prior = '' if inspection_error is None else '; inspection failed: %s: %s' % (
                type(inspection_error).__name__, inspection_error)
            unsafe = m.GuardUnsafe('helper evidence receipt failed: %s: %s%s' %
                                   (type(receipt_error).__name__, receipt_error, prior))
            unsafe.incomplete = True
            raise unsafe from receipt_error
        if inspection_error is not None:
            if isinstance(inspection_error, m.GuardUnsafe):
                raise inspection_error
            raise m.GuardUnsafe('helper exit inspection uncertain: ' + str(inspection_error)) from inspection_error
        if final['Running'] or final['Status'] not in ('created', 'exited'):
            raise m.GuardUnsafe('helper exit is uncertain')
        m.check(capture_error is None, 'helper raw output evidence incomplete: ' + str(capture_error))
        m.check(final['Status'] == 'exited' and final['ExitCode'] == 0,
                'helper exit or status invalid: ' + json.dumps(final))
        records, indexed = parse_probe(m, result, case, phase, len(roots), mode, exe, nested, deny,
                                       waiting=waiting, ready=ready, apparmor=running_inspect.get('AppArmorProfile'))
        owner.check_reader()
        preparation.validate_reader_context(prepared['raw_evidence'], prepared['request'],
                                            prepared['result']['preparer'], prepared['coordinator_namespaces'],
                                            waiting, indexed[(None, 'self_status')]['value'])
        graph = {}
        if not deny:
            paths = [item['target'] for item in expected if item['target'].startswith('/meta/')]
            for item in prepared['result']['attached']:
                paths.extend(item['target'] if row['relative'] == '.' else item['target'] + '/' + row['relative']
                             for row in item['subtree'])
            graph = mount_graph(indexed[(None, 'mountinfo')]['value']['text'], paths)
            verify_custody(m, prepared['result'], prepared['raw_evidence'], indexed, mode, exe)
            if nested:
                nested_rows = [row for row in prepared['result']['attached'][0]['subtree']
                               if row['relative'] == 'nested']
                m.check(len(nested_rows) == 1 and
                        indexed[('nested:000', 'statx')]['value']['mount_id'] == nested_rows[0]['mount_id'],
                        'nested observer mount custody differs')
        else:
            m.check(all(indexed[('%03d' % i, 'readonly_open')]['errno'] == errno.EROFS
                        for i in range(2 if nested else 1)), 'sentinel did not return EROFS')
            if profile == 'B':
                m.check(all(indexed[(None, op)]['errno'] == errno.EPERM for op in
                            ('inet_socket', 'unshare', 'mount')), 'seccomp denial differs from EPERM')
        owner.check_daemon()
        daemon_evidence = prepared['raw_evidence']['daemon']
        daemon_now = proc_stat(m.text('/proc/%d/stat' % owner.daemon['pid']))
        m.check(daemon_now['pid'] == daemon_evidence['pid'] == daemon_before['pid'] and
                daemon_now['starttime'] == daemon_evidence['starttime'] == daemon_before['starttime'],
                'daemon identity changed after observer completion')
        if owner.target is not None and metadata_pid == owner.target['pid']:
            target = owner.target
            m.check(owner.target_alive() and proc_stat(m.text('/proc/%d/stat' % target['pid'])) == target['stat'] and
                    m.text('/proc/%d/mountinfo' % target['pid']) == target['mountinfo'],
                    'target changed after observer completion')
        owner.check_reader()
        for name, value in owner.volumes.items():
            if any(x.get('name') == name for x in expected):
                m.check(owner.volume_inspect(name) == value, 'volume changed during helper')
        after_volumes = owner.volume_list()
        m.check(before_volumes == after_volumes, 'helper creation changed the volume set')
        flat = {(op if ordinal is None else ordinal + ':' + op): entry
                for (ordinal, op), entry in indexed.items()}
        return {'id': cid, 'preparation_version': 1, 'preparation': prepared,
                'records': records, 'indexed': flat, 'mount_graph': graph, **m.BOUNDARY,
                'inspect': {'mounts': actual, 'host_config': hc, 'apparmor': running_inspect.get('AppArmorProfile'),
                            'config': running_inspect['Config'], 'image': running_inspect['Image'],
                            'id': running_inspect['Id']},
                'final_state': final, 'attach_exit_code': result['returncode'],
                'helper_pidfd_exited': helper_pidfd_exited,
                'daemon_lifecycle': daemon_lifecycle,
                'volume_set_before': before_volumes, 'volume_set_after': after_volumes,
                'receipt': '/lab/control/' + receipt}
    except BaseException as exc:
        primary_error = exc
        owner.helper_deadline = None
        if attached is not None:
            try:
                if isinstance(exc, PreparationFailure) and not attached.proc.stdin.closed and time.monotonic() < attached.deadline:
                    attached.send_payload(b'')
                    result = attached.finish()
                    helper_pidfd_exited = helper_pidfd is not None and not running(helper_pidfd)
                    m.check(helper_pidfd_exited, 'failed observer original pidfd exit is uncertain')
                    final_inspect = owner.verify_owned(cid)
                    final = final_inspect['State']
                else:
                    result = attached.abort()
                    final_inspect = None
                attached = None
                capture_error = raw_capture_problem(result)
                if receipt is not None:
                    owner.save_new(receipt, {'request': request, 'start_attach': result, 'inspect_before': inspected,
                        'running_inspect': running_inspect,
                        'inspect_after': final_inspect, 'inspection_error': None, 'capture_error': capture_error,
                        'helper_pidfd_exited': helper_pidfd_exited,
                        'incomplete': capture_error is not None, 'evidence_limit': result['raw_output']['evidence_limit'],
                        'preparation': getattr(exc, 'preparation_evidence', None)})
                    receipt_saved = True
            except BaseException as capture_exc:
                unsafe = m.GuardUnsafe('failed observer capture or CLI exit uncertain: ' + str(capture_exc))
                unsafe.incomplete = True
                primary_error = unsafe
                raise unsafe from capture_exc
        if receipt is not None and result is not None and not receipt_saved:
            try:
                capture_error = raw_capture_problem(result)
                owner.save_new(receipt, {'request': request, 'start_attach': result, 'inspect_before': inspected,
                    'running_inspect': running_inspect,
                    'inspect_after': final_inspect, 'inspection_error': {'type': type(exc).__name__,
                    'message': str(exc)[:2048]}, 'capture_error': capture_error, 'incomplete': True,
                    'evidence_limit': result['raw_output']['evidence_limit'],
                    'helper_pidfd_exited': helper_pidfd_exited,
                    'preparation': getattr(exc, 'preparation_evidence', None)})
                receipt_saved = True
            except BaseException as capture_exc:
                unsafe = m.GuardUnsafe('failed observer evidence receipt: ' + str(capture_exc))
                unsafe.incomplete = True
                primary_error = unsafe
                raise unsafe from capture_exc
        if isinstance(exc, Exception) and receipt is not None and result is not None:
            try:
                exc.evidence = rejection_evidence(exc, receipt, request, result, inspected, final, capture_error)
            except Exception as evidence_error:
                exc.evidence = {'receipt': '/lab/control/' + receipt,
                                'request': {key: request[key] for key in ('id', 'run_token', 'case', 'phase', 'profile')},
                                'rejection': {'type': type(exc).__name__, 'message': str(exc)[:512],
                                              'code': getattr(exc, 'code', None)},
                                'diagnostic_error': '%s: %s' % (type(evidence_error).__name__, str(evidence_error)[:512]),
                                'incomplete': True, 'evidence_limit': True}
            if final_inspect is None or final is None:
                exc.evidence['incomplete'] = True
            if getattr(exc, 'preparation_evidence', None) is not None:
                exc.evidence['preparation'] = exc.preparation_evidence
            exc.evidence['helper_pidfd_exited'] = helper_pidfd_exited
        raise
    finally:
        owner.helper_deadline = None
        if helper_pidfd is not None:
            os.close(helper_pidfd)
        new_ids = set(owner.created) - before_ids
        if isinstance(primary_error, m.GuardUnsafe):
            pass
        elif (cid is not None and new_ids != {cid}) or (new_ids and not spec_verified):
            message = 'helper ownership or exact specification is uncertain'
            if primary_error is not None:
                message += ': %s: %s' % (type(primary_error).__name__, primary_error)
            raise m.GuardUnsafe(message) from primary_error
        else:
            try:
                for created in new_ids:
                    owner.remove(created)
                if cid is not None and cid in new_ids:
                    owner.check_daemon()
                    owner.check_reader()
                    daemon_after = proc_stat(m.text('/proc/%d/stat' % owner.daemon['pid']))
                    daemon_mounts_after = m.text('/proc/%d/mountinfo' % owner.daemon['pid'])
                    owner.check_reader()
                    m.check(daemon_after == daemon_before and daemon_mounts_after == daemon_mounts_before,
                            'daemon source graph changed across helper create/remove lifecycle')
                    retirement = owner.retired_containers[-1]
                    m.check(retirement['id'] == cid and retirement['absent'] is True,
                            'helper lifecycle lacks exact retirement')
                    value = {'pid': daemon_before['pid'], 'starttime': daemon_before['starttime'],
                             'helper_id': cid, 'before_mountinfo': daemon_mounts_before,
                             'after_mountinfo': daemon_mounts_after, 'retirement': retirement}
                    lifecycle_name = 'helper-lifecycle-' + cid + '.json'
                    owner.save_new(lifecycle_name, value)
                    lifecycle_path = '/lab/control/' + lifecycle_name
                    lifecycle_bytes = m.text(lifecycle_path).encode('utf-8')
                    m.check(lifecycle_bytes == preparation.canonical_json(value) + b'\n',
                            'helper lifecycle receipt changed')
                    daemon_lifecycle.update(value=value, receipt=lifecycle_path,
                                            sha256=hashlib.sha256(lifecycle_bytes).hexdigest())
                    if isinstance(getattr(primary_error, 'evidence', None), dict):
                        primary_error.evidence['daemon_lifecycle'] = daemon_lifecycle
            except BaseException as cleanup_error:
                if primary_error is None:
                    raise m.GuardUnsafe('helper cleanup/source verification uncertain: ' + str(cleanup_error)) from cleanup_error
                raise m.GuardUnsafe('helper operation failed: %s: %s; cleanup failed: %s: %s' %
                                    (type(primary_error).__name__, primary_error,
                                     type(cleanup_error).__name__, cleanup_error)) from cleanup_error


def daemon_observation(owner, image, profile):
    m = owner.m
    boot = owner.bootstrap()
    owner.check_reader()
    daemon = owner.daemon
    pid = daemon['pid']
    before = proc_stat(m.text('/proc/%d/stat' % pid))
    guard = owner.guard()
    control = os.stat('/lab/control', follow_symlinks=False)
    m.check(stat.S_ISDIR(control.st_mode) and control.st_uid == 10002 and stat.S_IMODE(control.st_mode) == 0o700,
            'coordinator control directory identity mismatch')
    observed = helper(owner, image, profile, 'I0', 'before',
                      [{'type': 'bind', 'source': '/lab/control'}], pid, exe=True)
    owner.check_daemon()
    owner.check_reader()
    m.check(owner.guard() == guard, 'coordinator guard changed during daemon observation')
    after = proc_stat(m.text('/proc/%d/stat' % pid))
    idx = observed['indexed']
    exe = os.fstat(daemon['exe_fd'])
    pinned = observed['preparation']['raw_evidence']['daemon']
    view = observed['preparation']['raw_evidence']['observer_view']
    probe_control = idx['000:fstat']['value']
    m.check(before == after == proc_stat(idx['target_stat']['value']['text']) and before['pid'] == pid and
            pinned['pid'] == pid and pinned['starttime'] == before['starttime'] and
            idx['target_pidns']['value'] == pinned['pidns'] and
            idx['target_exe']['value'] == {'dev': exe.st_dev, 'ino': exe.st_ino} and
            (probe_control['dev'], probe_control['ino'], probe_control['mode']) ==
            (control.st_dev, control.st_ino, control.st_mode) and
            pinned['before_mountinfo'] == pinned['after_mountinfo'] and
            idx['target_mountinfo']['value']['text'] == view['before']['metadata_mountinfo'] ==
            view['after']['metadata_mountinfo'] and
            running(daemon['pidfd']),
            'daemon metadata identity mismatch')
    return {'bootstrap': boot, 'helper': observed, 'guard': guard, 'stat': before,
            'control_directory': {'dev': control.st_dev, 'ino': control.st_ino, 'mode': control.st_mode},
            'dockerd_exe': {'dev': exe.st_dev, 'ino': exe.st_ino},
            'mount_namespace_identity': 'UNPROVEN'}


def target_probe(owner, image, profile, case):
    t = owner.target
    owner.m.check(t is not None and case in ('I1', 'I2', 'I3'), 'invalid target observation')
    command_index = len(owner.commands)
    if not owner.target_alive():
        commands = owner.commands[command_index:]
        owner.m.check(not any(cmd and cmd[0] == 'create' for cmd in commands), 'unexpected helper creation')
        return {'status': 'REJECTED', 'reason': 'target_exited', 'pidfd_exited': not running(t['pidfd']),
                'helper_create_issued': False, 'commands': commands}
    roots = [{'type': 'bind', 'source': t['source']},
             {'type': 'running_root'}]
    value = helper(owner, image, profile, case, 'before', roots, t['pid'])
    t['helper'] = value
    return {'status': 'OBSERVED', 'helper': value}


def helper_negative(owner, image, profile, source, variant):
    m = owner.m
    variants = ('no_ready', 'empty_eof', 'truncated_eof', 'wrong_nonce', 'wrong_input_digest', 'extra_fd')
    m.check(variant in variants and profile in ('A', 'B') and type(source) is str and
            source.startswith('/lab/') and os.path.normpath(source) == source, 'invalid fixed negative fixture')
    before = len(owner.retired_containers)
    try:
        _helper(owner, image, profile, 'C0', 'before', [{'type': 'bind', 'source': source}], negative=variant)
    except PreparationFailure as exc:
        detail = getattr(exc, 'evidence', None)
        m.check(type(detail) is dict and type(detail.get('receipt')) is str, 'negative fixture evidence missing')
        saved = strict_json(m.text(detail['receipt']))
        result = saved['start_attach']
        rows = [strict_json(line) for line in result['stdout'].splitlines()]
        cid = saved['request']['id']
        running_inspect = saved['running_inspect']
        m.check(type(running_inspect) is dict and running_inspect.get('Id') == cid and
                type(running_inspect.get('State')) is dict and running_inspect['State'].get('Running') is True and
                raw_capture_problem(result) is None and result['returncode'] != 0 and
                len(rows) == 1 and rows[0]['operation'] == 'waiting' and
                saved['inspect_after']['State']['Status'] == 'exited' and
                saved['inspect_after']['State']['ExitCode'] != 0 and
                len(owner.retired_containers) == before + 1 and owner.retired_containers[-1]['id'] == cid and
                owner.retired_containers[-1]['absent'] is True and cid not in owner.created,
                'negative fixture observer was not fully observed and retired')
        if variant in ('wrong_input_digest', 'extra_fd'):
            prep = getattr(exc, 'preparation_evidence', None)
            m.check(type(prep) is dict and prep['pidfd_exited'] is True and prep['test_fault']['sent'] is True and
                    prep['test_fault']['variant'] == variant and
                    (variant != 'extra_fd' or prep['test_fault']['sender_extra_fd_closed'] is True),
                    'negative preparer exit evidence missing')
            owner.launch.require_no_pending()
        else:
            prep = None
        negative_intent = (None if variant in ('wrong_input_digest', 'extra_fd') else
                           strict_json(m.text('/lab/control/observer-negative-%s.json' % cid)))
        coordinator_namespaces = {}
        for name in ('pid', 'mnt', 'ipc', 'uts', 'net', 'cgroup'):
            namespace = os.stat('/proc/self/ns/' + name)
            coordinator_namespaces[name] = {'dev': namespace.st_dev, 'ino': namespace.st_ino}
        inspected = saved['inspect_before']
        owned = strict_json(m.text('/lab/control/receipt-%s.json' % cid))
        m.check(owned['id'] == cid, 'negative original create receipt identity mismatch')
        return {'status': 'REJECTED', 'variant': variant, 'reason': exc.code, 'helper_id': cid,
                'waiting': rows[0], 'start_attach': result, 'final_state': saved['inspect_after']['State'],
                'helper_pidfd_exited': saved.get('helper_pidfd_exited'),
                'stop': saved.get('stop'),
                'daemon_lifecycle': detail['daemon_lifecycle'], 'rejection': detail['rejection'],
                'preparation': prep, 'receipt': detail['receipt'], 'negative_intent': negative_intent,
                'source': source, 'profile': profile, 'image': image,
                'create_spec': owned['spec'],
                'coordinator_namespaces': coordinator_namespaces,
                'running_inspect': running_inspect,
                'inspect': {'mounts': inspected['Mounts'], 'host_config': inspected['HostConfig'],
                            'apparmor': inspected.get('AppArmorProfile'), 'config': inspected['Config'],
                            'image': inspected['Image'], 'id': inspected['Id']},
                'retirement': owner.retired_containers[-1], 'no_ready_or_root_output': True}
    raise m.Failure('fixed negative observer unexpectedly succeeded')


def dispatch(owner, request):
    m = owner.m
    owner.require_no_pending()
    m.check(isinstance(request, dict) and isinstance(request.get('op'), str), 'invalid coordinator request')
    op = request['op']
    if op == 'daemon_bootstrap':
        m.check(set(request) == {'op'}, 'invalid daemon bootstrap fields')
        return owner.bootstrap()
    if op == 'docker':
        return owner.docker_request(request)
    if op == 'fixture_register':
        return owner.register_fixture(request['id'], request['spec'])
    if op == 'volume_register':
        return owner.register_volume(request['name'], request.get('creator'))
    if op == 'volume_access':
        return owner.direct_access(request['name'])
    if op == 'volume_missing':
        return owner.missing_volume(request['name'], request['image'], request['profile'])
    if op == 'helper':
        return helper(owner, request['image'], request['profile'], request['case'], request['phase'],
                      request['roots'], nested=request.get('nested', False), deny=request.get('deny', False))
    if op == 'helper_negative':
        m.check(set(request) == {'op', 'image', 'profile', 'source', 'variant'}, 'invalid negative fixture fields')
        return helper_negative(owner, request['image'], request['profile'], request['source'], request['variant'])
    if op == 'daemon_observe':
        return daemon_observation(owner, request['image'], request['profile'])
    if op == 'target_begin':
        return owner.begin(request['id'], request['destination'])
    if op == 'target_probe':
        return target_probe(owner, request['image'], request['profile'], request['case'])
    if op == 'target_finish':
        return owner.finish()
    if op == 'target_abort':
        return owner.abort_target()
    if op == 'watch_add':
        return owner.watch_add(request['id'])
    if op == 'watch_check':
        return owner.watch_check(request['id'])
    if op == 'guard':
        return owner.guard()
    if op == 'cleanup':
        m.check(owner.target is None, 'active target must be finished before cleanup')
        watch_errors = []
        for cid in owner.watchers:
            try:
                owner.watch_check(cid)
            except Exception as exc:
                watch_errors.append({'id': cid, 'error': str(exc)})
        owner.close_watches()
        retired = []
        for cid in list(owner.created):
            owner.remove(cid)
            retired.append(cid)
        return {'retired': retired, 'remaining': len(owner.created), 'watch_errors': watch_errors}
    raise m.Failure('unknown coordinator operation')


def serve(m, stream, lock):
    owner = Coordinator(m, lock)
    while True:
        try:
            line = stream.readline(32 * 1024 * 1024)
        except BaseException:
            retain_guard()
        if not line or not line.endswith(b'\n'):
            retain_guard()
        try:
            request = strict_json(line)
            if request.get('op') == 'finish':
                owner.require_no_pending()
                m.check(not owner.created and owner.target is None, 'resources remain at coordinator finish')
                try:
                    owner.close()
                    m.check(not owner.pending and not owner.created and owner.target is None and
                            not owner.watchers and owner.daemon is None, 'coordinator retirement incomplete')
                    owner.launch.require_no_pending()
                    ledger = {'containers': owner.retired_containers, 'preparers': owner.launch.records}
                    ledger_sha = hashlib.sha256(preparation.canonical_json(ledger)).hexdigest()
                    owner.save_new('coordinator-retired.json', {'ledger': ledger, 'sha256': ledger_sha,
                                   'session_nonce': owner.launch.session_nonce,
                                   'run_token': owner.launch.run_token})
                    owner.launch.retire(ledger_sha)
                    stream.write(b'{"finished":true}\n')
                except BaseException:
                    retain_guard()
                return True
            result = dispatch(owner, request)
            response = {'result': result}
        except m.GuardUnsafe as exc:
            try:
                message = str(exc)
                stream.write((json.dumps({'uncertain': True, 'error': message[:2048],
                                          'incomplete': len(message) > 2048 or getattr(exc, 'incomplete', False),
                                          'evidence_limit': len(message) > 2048 or
                                          getattr(exc, 'evidence_limit', False)}) + '\n').encode())
            finally:
                retain_guard()
        except Exception as exc:
            message = '%s: %s' % (type(exc).__name__, exc)
            response = {'error': message[:2048]}
            if len(message) > 2048:
                response.update(incomplete=True, evidence_limit=True)
            if isinstance(exc, m.CreateFailure):
                response['create_result'] = exc.result
            if getattr(exc, 'evidence', None) is not None:
                response['evidence'] = exc.evidence
        try:
            stream.write((json.dumps(response, sort_keys=True, separators=(',', ':')) + '\n').encode())
        except BaseException:
            retain_guard()
