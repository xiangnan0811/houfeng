"""Offline import of the one approved predecessor's authenticated VM assets."""
import hashlib
import json
import os
import stat
import subprocess
from pathlib import Path

SOURCE_PINS = {
    'host.py': '9a33345f1b0666806a5afb6a47a4c6afcfabe8e88362c50147cd28e0c0373c33',
    'guest.py': '3caed00b1af1240ba0c2c1b92521adbfcb3423b26e42d5f97dec1609c01bed08',
    'probe/main.go': '5432505a911df942e90ff99449c740cdcad70372fc2ecae382df050aae695d4b',
    'probe-seccomp.json': '30c2ac52c9d201f81a107f7b99e35c0024910af55af70aafb14c433350fd1e75',
    'build.json': '1c7cfe301d558634fd321d2b335e066ead80172d35d54a162819da58318b61dd',
    'result.json': '5a183e7fcebfc666c3d400a910b2a670ef0a5be57bd55267bae333394db3c813',
    'matrix-execution-evidence.json': '2354608730b6443d87cb79e75015f9b319ec9e41ec0e21bf9d8bab93d3903331',
}
QEMU_PINS = {
    'qemu-img': '22d0a60105357cffd6c4fdbc1e8458cd29f36bbc5c7d5abe61c5ec03a51bb993',
    'qemu-system-x86_64': '4d7f6ee3438a4bf40cf9a9ba2a6ce4b7e3b0f6437bc9eb17bd0f3a7ccc067107',
}
MANIFEST_SHA = 'ac3d16592eddc04af338252f199f5adfd4a6385d897a8298fcb504e5bbfb4ece'
MODULE_SUM = 'h1:o7XGOvZQCADBQQ4Y7VNq2dRWQR7JmOUW8Kxx4ZsNgWs='
MODULE_MOD_SUM = 'h1:4GL1E5IUh+htKOUEOaiffhrAeqysfVGipDYzABqnCmw='


def mkdir_private(m, path):
    path = Path(path)
    m.require(path == m.RUN or m.RUN in path.parents, 'private directory escapes RUN')
    current = m.RUN
    for part in path.relative_to(m.RUN).parts:
        current = current / part
        try:
            os.mkdir(current, 0o700)
        except FileExistsError:
            pass
        m.check_path(current)
        st = os.stat(current, follow_symlinks=False)
        m.require(st.st_uid == os.geteuid() and stat.S_IMODE(st.st_mode) == 0o700,
                  'unsafe import directory: ' + str(current))


def source_path(m, value):
    path = Path(value)
    m.require(path.is_absolute() and m.OLD in path.parents and '..' not in path.parts,
              'predecessor asset escapes fixed source')
    m.check_path(path, False)
    return path


def copy_checked(m, src, dst, sha, size=None, mode=None):
    src, dst = source_path(m, src), Path(dst)
    m.require(m.RUN in dst.parents, 'import destination escapes RUN')
    mkdir_private(m, dst.parent)
    fd = os.open(src, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        m.require(stat.S_ISREG(before.st_mode), 'source is not a regular file')
        h = hashlib.sha256()
        outfd = os.open(dst, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            while True:
                data = os.read(fd, 1024 * 1024)
                if not data:
                    break
                h.update(data)
                view = memoryview(data)
                while view:
                    written = os.write(outfd, view)
                    m.require(written > 0, 'short asset copy')
                    view = view[written:]
            after = os.fstat(fd)
            m.require((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
                      (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns),
                      'source changed during copy: ' + str(src))
            m.require(h.hexdigest() == sha and (size is None or before.st_size == size),
                      'source asset hash or length changed: ' + str(src))
            os.fchmod(outfd, mode if mode is not None else stat.S_IMODE(before.st_mode))
            os.fsync(outfd)
            target = os.fstat(outfd)
            m.require((target.st_dev, target.st_ino) != (before.st_dev, before.st_ino), 'hardlinked copy rejected')
        finally:
            os.close(outfd)
    finally:
        os.close(fd)
    m.require(m.digest(dst) == (sha, before.st_size), 'copied asset changed: ' + str(dst))
    directory = os.open(dst.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {'source': str(src), 'destination': str(dst), 'sha256': sha, 'size': before.st_size}


def fixed_run(m, argv, env, timeout=120):
    result = subprocess.run(argv, cwd=str(m.RUN), env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
    m.require(len(result.stdout) <= 1024 * 1024 and len(result.stderr) <= 1024 * 1024,
              'command output exceeded limit')
    m.require(result.returncode == 0, 'command failed: %s: %s' % (argv[0], result.stderr.decode('utf-8', 'replace')[:4096]))
    return result.stdout.decode('utf-8', 'strict')


def verify_signature(m, env, name, keyring, signed, detached=None):
    keyring, signed = source_path(m, keyring), source_path(m, signed)
    args = ['/usr/bin/gpg', '--no-options', '--batch', '--no-autostart', '--no-auto-key-retrieve',
            '--homedir', env['GNUPGHOME'], '--with-colons', '--show-keys', str(keyring)]
    output = fixed_run(m, args, env)
    primaries, pending = [], False
    for line in output.splitlines():
        fields = line.split(':')
        if fields[0] == 'pub':
            pending = True
        elif fields[0] == 'sub':
            pending = False
        elif fields[0] == 'fpr' and pending:
            primaries.append(fields[9].upper())
            pending = False
    expected = m.KEYS[name][1]
    m.require(primaries == [expected], 'wrong predecessor signing key: ' + name)
    args = ['/usr/bin/gpgv', '--homedir', env['GNUPGHOME'], '--keyring', str(keyring), '--status-fd', '1']
    args += [str(source_path(m, detached)), str(signed)] if detached else [str(signed)]
    output = fixed_run(m, args, env, timeout=180)
    valid = [line.split() for line in output.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')]
    m.require(len(valid) == 1 and (valid[0][2].upper() == expected or valid[0][-1].upper() == expected),
              'signature does not bind pinned primary: ' + name)
    return {'primary_fingerprint': expected, 'validsig': valid[0][2:], 'signed': str(signed),
            'signed_sha256': m.file_hash(signed), 'keyring': str(keyring), 'keyring_sha256': m.file_hash(keyring),
            'signature': str(source_path(m, detached)) if detached else None,
            'signature_sha256': m.file_hash(source_path(m, detached)) if detached else None}


def firmware_check(m, root, members, source=False):
    prefix = m.ROOT + '/pc-bios'
    wanted = {item['path'][len(prefix) + 1:]: item for item in members if item['path'] != prefix}
    roots = [item for item in members if item['path'] == prefix]
    m.require(len(roots) == 1 and roots[0]['type'] == 'dir' and len(wanted) + 1 == len(members),
              'invalid firmware manifest')
    m.check_path(root)
    root_st = os.stat(root, follow_symlinks=False)
    m.require(stat.S_IMODE(root_st.st_mode) == roots[0]['mode'] and
              (not source or root_st.st_size == roots[0]['size']), 'firmware root identity changed')
    seen = set()
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            path = Path(directory) / name
            relative = str(path.relative_to(root))
            item = wanted.get(relative)
            m.require(item is not None and relative not in seen, 'unexpected firmware member: ' + relative)
            seen.add(relative)
            st = os.lstat(path)
            typ = 'dir' if stat.S_ISDIR(st.st_mode) else 'file' if stat.S_ISREG(st.st_mode) else 'other'
            m.require(typ == item['type'] and stat.S_IMODE(st.st_mode) == item['mode'],
                      'firmware type or mode mismatch: ' + relative)
            if typ == 'file':
                m.require(m.digest(path) == (item['sha256'], item['size']), 'firmware content changed: ' + relative)
            elif source:
                m.require(st.st_size == item['size'], 'firmware source directory changed: ' + relative)
    m.require(seen == set(wanted), 'firmware members missing')
    return hashlib.sha256(m.canonical_json(members)).hexdigest()


def prepare(m, env, source_run):
    m.require(source_run == str(m.OLD), 'source-run must be the fixed predecessor')
    m.check_path(m.OLD)
    with m.exclusive(m.RUN / 'prepare.json') as receipt:
        state = {'run_token': m.TOKEN, 'experiment_kind': 'input-handoff-v1', 'status': 'IN_PROGRESS',
                 'source_run': str(m.OLD), 'stage': 'source', 'imports': [], **m.EXPERIMENT_BOUNDARY}
        m.receipt_save(receipt, state)
        try:
            for name, expected in SOURCE_PINS.items():
                path = source_path(m, m.OLD / name)
                m.require(m.file_hash(path) == expected, 'fixed predecessor changed: ' + name)
            old = m.load_json(m.OLD / 'result.json')
            build = m.load_json(m.OLD / 'build.json')
            review = m.load_json(m.OLD / 'archive-review.json')
            m.require(old.get('run_token') == m.OLD_TOKEN and old.get('fetch', {}).get('status') == 'PASS' and
                      build.get('run_token') == m.OLD_TOKEN and build.get('status') == 'PASS' and
                      build.get('host_sha256') == SOURCE_PINS['host.py'] and
                      build.get('fetch_result_sha256') == SOURCE_PINS['result.json'] and
                      build.get('source_manifest_sha256') == MANIFEST_SHA and build.get('binaries') == QEMU_PINS,
                      'predecessor build/acquisition binding mismatch')
            m.require(review.get('run_token') == m.OLD_TOKEN and review.get('status') == 'PASS' and
                      review.get('model') == 'magpie/codex/gpt-6.1-sol' and review.get('thinkingLevel') == 'xhigh' and
                      review.get('resolvedModelIsFallback') is False and review.get('stopReason') == 'stop' and
                      review.get('material_findings') == [] and
                      all(review.get(k) == v for k, v in m.REVIEW_HASHES.items()),
                      'predecessor archive review binding mismatch')
            for name, key in (('archive-candidate.py', 'host_sha256'), ('archive_smoke.py', 'smoke_sha256'),
                              ('archive-smoke.json', 'smoke_receipt_sha256'),
                              ('build/source-manifest.json', 'source_manifest_sha256')):
                m.require(m.file_hash(source_path(m, m.OLD / name)) == m.REVIEW_HASHES[key],
                          'predecessor review evidence changed: ' + name)
            candidate = (m.OLD / 'archive-candidate.py').read_bytes()
            core = candidate.split(b'\nRUN = ', 1)[1].split(b'\ndef verify():', 1)[0]
            m.require(hashlib.sha256(core).hexdigest() == m.REVIEW_HASHES['archive_core_sha256'],
                      'predecessor reviewed archive core changed')
            manifest = m.load_json(m.OLD / 'build/source-manifest.json')
            m.require(manifest.get('omitted') == [m.OMITTED], 'predecessor source manifest omission mismatch')
            members = build['firmware']['pc_bios_members']
            expected_members = sorted((item for item in manifest['members'] if item['path'] == m.ROOT + '/pc-bios' or
                                       item['path'].startswith(m.ROOT + '/pc-bios/')), key=lambda item: item['path'])
            m.require(members == expected_members and
                      firmware_check(m, m.OLD / 'build/source' / m.ROOT / 'pc-bios', members, source=True) ==
                      build['firmware']['pc_bios_manifest_sha256'], 'firmware source manifest mismatch')
            state['historical'] = {'source_pins': SOURCE_PINS, 'review_sha256': m.file_hash(m.OLD / 'archive-review.json'),
                                   'qemu_build_sha256': SOURCE_PINS['build.json'],
                                   'source_manifest_sha256': MANIFEST_SHA}
            state['stage'] = 'signatures'
            m.receipt_save(receipt, state)
            keyrings = {}
            for name in m.KEYS:
                item = old['keys'][name]
                keyring = source_path(m, item['keyring'])
                m.require(item['primary_fingerprint'] == m.KEYS[name][1] and m.file_hash(keyring) == item['keyring_sha256'],
                          'predecessor keyring changed: ' + name)
                keyrings[name] = keyring
            qemu = source_path(m, old['qemu']['path'])
            m.require(m.digest(qemu) == (m.QEMU_SHA256, m.QEMU_SIZE), 'signed QEMU archive changed')
            signatures = {
                'qemu': verify_signature(m, env, 'qemu', keyrings['qemu'], qemu, old['signatures']['qemu']['signature']),
                'ubuntu': verify_signature(m, env, 'ubuntu', keyrings['ubuntu'], old['signatures']['ubuntu']['signed'],
                                           old['signatures']['ubuntu']['signature']),
                'docker': verify_signature(m, env, 'docker', keyrings['docker'], old['signatures']['docker']['signed']),
            }
            sums = m.ubuntu_sums(source_path(m, old['signatures']['ubuntu']['signed']))
            m.require(all(sums.get(name) == sha for name, sha in m.UBUNTU_HASHES.items()), 'Ubuntu signed pins mismatch')
            pairs = [x for x in old['docker_index_pairs'] if x.get('match') is True]
            m.require(len(pairs) == 1 and pairs[0]['release'] == old['signatures']['docker']['signed'],
                      'Docker signed index pair mismatch')
            packages = source_path(m, pairs[0]['packages'])
            m.require(m.digest(packages) == m.release_packages_entry(m.cleartext(source_path(m, pairs[0]['release']))),
                      'Docker signed package index mismatch')
            entries = m.package_entries(packages)
            state['signatures'] = signatures
            index_sha, index_size = m.digest(packages)
            state['package_index'] = {'source': str(packages), 'sha256': index_sha, 'size': index_size}
            state['stage'] = 'copy'
            m.receipt_save(receipt, state)
            assets = {}
            for name, pin in m.UBUNTU_HASHES.items():
                item = old['hash_checks']['ubuntu:' + name]
                m.require(item['sha256'] == pin and item['expected'] == pin and item['match'] is True,
                          'Ubuntu acquisition record mismatch')
                dst = m.RUN / 'assets' / Path(item['path']).name
                state['imports'].append(copy_checked(m, item['path'], dst, pin, item['size']))
                assets['ubuntu:' + name] = str(dst)
            for name, (_, _, pin) in m.DEBS.items():
                item = old['hash_checks']['docker:' + name]
                m.require(item['sha256'] == pin and item['expected'] == pin and item['match'] is True and
                          entries[name][0].get('Size') == str(item['size']), 'Docker package record mismatch')
                dst = m.RUN / 'assets' / Path(item['path']).name
                state['imports'].append(copy_checked(m, item['path'], dst, pin, item['size']))
                assets['docker:' + name] = str(dst)
            for name, pin in QEMU_PINS.items():
                dst = m.RUN / 'build/qemu' / name
                state['imports'].append(copy_checked(m, m.OLD / 'build/qemu' / name, dst, pin))
            state['imports'].append(copy_checked(m, m.OLD / 'build/source-manifest.json',
                                                 m.RUN / 'build/source-manifest.json', MANIFEST_SHA))
            root = m.RUN / 'build/source' / m.ROOT / 'pc-bios'
            mkdir_private(m, root)
            prefix = m.ROOT + '/pc-bios/'
            for item in members:
                if item['path'] == m.ROOT + '/pc-bios':
                    continue
                rel = item['path'][len(prefix):]
                m.require(rel and not rel.startswith('/') and '..' not in Path(rel).parts,
                          'unsafe firmware member')
                dst = root / rel
                if item['type'] == 'dir':
                    mkdir_private(m, dst)
                    m.require(item['mode'] == 0o700, 'unexpected firmware directory mode')
                else:
                    m.require(item['type'] == 'file', 'unsupported firmware member')
                    state['imports'].append(copy_checked(m, m.OLD / 'build/source' / item['path'], dst,
                                                         item['sha256'], item['size'], item['mode']))
            m.require(firmware_check(m, root, members) == build['firmware']['pc_bios_manifest_sha256'],
                      'copied firmware differs')
            cache_src = m.OLD / 'cache/go-mod/cache/download/golang.org/x/sys/@v'
            cache_dst = m.RUN / 'cache/go-mod/cache/download/golang.org/x/sys/@v'
            for name in ('v0.47.0.info', 'v0.47.0.mod', 'v0.47.0.zip', 'v0.47.0.ziphash'):
                src = source_path(m, cache_src / name)
                sha, length = m.digest(src)
                state['imports'].append(copy_checked(m, src, cache_dst / name, sha, length))
            m.require((cache_dst / 'v0.47.0.ziphash').read_text().strip() == MODULE_SUM, 'module zip checksum mismatch')
            m.require(m.file_hash(m.RUN / 'go.mod') == build['inputs']['go.mod'] and
                      m.file_hash(m.RUN / 'go.sum') == build['inputs']['go.sum'], 'module declarations changed')
            state['stage'] = 'module-verification'
            m.receipt_save(receipt, state)
            m.check_path(m.GO, False)
            version = fixed_run(m, [str(m.GO), 'version'], env).strip()
            m.require(version == 'go version go1.26.2 linux/amd64', 'incorrect Go toolchain')
            module = json.loads(fixed_run(m, [str(m.GO), 'mod', 'download', '-json', 'golang.org/x/sys@v0.47.0'], env))
            m.require(module.get('Path') == 'golang.org/x/sys' and module.get('Version') == 'v0.47.0' and
                      module.get('Sum') == MODULE_SUM and module.get('GoModSum') == MODULE_MOD_SUM and
                      not module.get('Error'), 'offline module checksum verification failed')
            verified = fixed_run(m, [str(m.GO), 'mod', 'verify'], env)
            m.require(verified.strip() == 'all modules verified', 'go mod verify failed')
            state.update(status='PASS', stage='complete', assets=assets, firmware=build['firmware'],
                         qemu_binaries=QEMU_PINS, go_version=version, module={'sum': MODULE_SUM,
                         'go_mod_sum': MODULE_MOD_SUM, 'verified': verified.strip()})
            m.receipt_save(receipt, state)
            return 0
        except BaseException as exc:
            state.update(status='PREREQUISITE_FAILED', error_type=type(exc).__name__, error=str(exc))
            m.receipt_save(receipt, state)
            if isinstance(exc, (KeyboardInterrupt, SystemExit)):
                raise
            print('PREREQUISITE_FAILED: prepare: %s' % exc, file=m.sys.stderr)
            return 1


def gate(m):
    state = m.load_json(m.RUN / 'prepare.json')
    m.require(state.get('run_token') == m.TOKEN and state.get('experiment_kind') == 'input-handoff-v1' and
              state.get('source_run') == str(m.OLD) and state.get('status') == 'PASS' and
              state.get('qemu_binaries') == QEMU_PINS and state.get('go_version') == 'go version go1.26.2 linux/amd64' and
              state.get('module') == {'sum': MODULE_SUM, 'go_mod_sum': MODULE_MOD_SUM, 'verified': 'all modules verified'},
              'current prepare receipt incomplete')
    m.require(all(state.get(key) == value for key, value in m.EXPERIMENT_BOUNDARY.items()),
              'import receipt capability boundary missing')
    m.require(state.get('historical', {}).get('source_pins') == SOURCE_PINS and
              set(state.get('signatures', {})) == set(m.KEYS) and
              all(state['signatures'][name].get('primary_fingerprint') == m.KEYS[name][1] for name in m.KEYS),
              'historical source or signature binding incomplete')
    for name, pin in SOURCE_PINS.items():
        m.require(m.file_hash(source_path(m, m.OLD / name)) == pin, 'fixed predecessor changed: ' + name)
    for signature in state['signatures'].values():
        for key in ('signed', 'keyring', 'signature'):
            if signature[key] is not None:
                m.require(m.file_hash(source_path(m, signature[key])) == signature[key + '_sha256'],
                          'signed predecessor input changed: ' + key)
    index = state['package_index']
    m.require(m.digest(source_path(m, index['source'])) == (index['sha256'], index['size']),
              'signed Docker index changed')
    expected_assets = {'ubuntu:' + name for name in m.UBUNTU_HASHES} | {'docker:' + name for name in m.DEBS}
    m.require(set(state['assets']) == expected_assets, 'runtime asset set incomplete')
    members = state['firmware']['pc_bios_members']
    expected_destinations = {str(m.RUN / 'build/qemu' / name) for name in QEMU_PINS}
    expected_destinations.add(str(m.RUN / 'build/source-manifest.json'))
    expected_destinations.update(state['assets'].values())
    expected_destinations.update(str(m.RUN / 'build/source' / item['path']) for item in members if item['type'] == 'file')
    cache = m.RUN / 'cache/go-mod/cache/download/golang.org/x/sys/@v'
    expected_destinations.update(str(cache / name) for name in
                                 ('v0.47.0.info', 'v0.47.0.mod', 'v0.47.0.zip', 'v0.47.0.ziphash'))
    destinations = [entry['destination'] for entry in state['imports']]
    m.require(len(destinations) == len(set(destinations)) and set(destinations) == expected_destinations,
              'import manifest missing, duplicated, or contains extra destinations')
    for entry in state['imports']:
        dst = Path(entry['destination'])
        src = source_path(m, entry['source'])
        m.require(m.RUN in dst.parents and m.digest(src) == (entry['sha256'], entry['size']) and
                  m.file_hash(dst) == entry['sha256'] and dst.stat().st_size == entry['size'],
                  'source or imported asset changed: ' + str(dst))
    manifest = m.load_json(m.RUN / 'build/source-manifest.json')
    m.require(m.file_hash(m.RUN / 'build/source-manifest.json') == MANIFEST_SHA, 'imported manifest changed')
    expected_members = sorted((item for item in manifest['members'] if item['path'] == m.ROOT + '/pc-bios' or
                               item['path'].startswith(m.ROOT + '/pc-bios/')), key=lambda item: item['path'])
    m.require(state['firmware']['pc_bios_members'] == expected_members, 'firmware not bound to source manifest')
    members = state['firmware']['pc_bios_members']
    m.require(firmware_check(m, m.RUN / 'build/source' / m.ROOT / 'pc-bios', members) ==
              state['firmware']['pc_bios_manifest_sha256'], 'imported firmware changed')
    for name, pin in QEMU_PINS.items():
        m.require(m.file_hash(m.RUN / 'build/qemu' / name) == pin, 'imported QEMU binary changed')
    for key, path in state['assets'].items():
        pin = m.UBUNTU_HASHES[key[7:]] if key.startswith('ubuntu:') else m.DEBS[key[7:]][2]
        m.require(m.file_hash(Path(path)) == pin, 'imported runtime asset changed')
    return {'hash_checks': {key: {'path': path} for key, path in state['assets'].items()}}, manifest, state
