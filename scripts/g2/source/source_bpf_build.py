"""Fixed BPF authority artifacts for the isolated source build only."""
import hashlib
import os
from pathlib import Path
import stat
import struct
import subprocess
import tarfile

import source_boot
import source_host
import source_kernel_observer

AUDIT_SOURCE_SHA256 = 'fa17a9437084f270a8d8e560af82ad5d49e3cfb19fcf41ab19493c4d7c97f64e'
AUDIT_SOURCE = source_host.RUN / 'assets/ubuntu-6.8.0-147.147-kernel-audit.h'
COMMAND_LINE = ('root=LABEL=cloudimg-rootfs ro console=tty1 console=ttyS0 '
                'lsm=landlock,lockdown,yama,integrity,apparmor,bpf rdinit=/g2-early-init')


def native_sources(host):
    root = host.SOURCE_ROOT / 'source_supervisor/bpf-authority/native'
    host.check_path(root)
    result = {}
    for parent, directories, files in os.walk(root, followlinks=False):
        for name in directories:
            host.check_path(Path(parent) / name)
        for name in sorted(files):
            path = Path(parent) / name
            host.check_path(path, False)
            host.require(name == 'Makefile' or path.suffix in ('.h', '.c'), 'unexpected native source input')
            result[str(path.relative_to(root))] = host.digest(path)[0]
    host.require({'Makefile', 'policy.c', 'abi.h', 'g2_bpf_guard.c'} <= set(result), 'native implementation incomplete')
    host.require('pinned/kernel-audit.h' not in result, 'private audit input must be independently staged')
    return result


def package_executables(host, frozen, env, prepared):
    expected = {
        'docker-ce': {'dockerd': 'usr/bin/dockerd'},
        'containerd.io': {'runc': 'usr/bin/runc', 'containerd': 'usr/bin/containerd',
                          'shim': 'usr/bin/containerd-shim-runc-v2'},
    }
    result = {}
    for package, wanted in expected.items():
        archive = Path(prepared['hash_checks']['docker:' + package]['path'])
        host.require(host.digest(archive)[0] == frozen.DEBS[package][2], 'BPF executable package changed')
        query = subprocess.run(['/usr/bin/dpkg-deb', '-f', str(archive), 'Package', 'Version', 'Architecture'],
                               env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, timeout=30, check=False)
        host.require(query.returncode == 0 and len(query.stdout) <= 4096 and not query.stderr,
                     'BPF package metadata unavailable')
        values = {}
        for line in query.stdout.decode('ascii').splitlines():
            key, sep, value = line.partition(': ')
            host.require(sep and key not in values, 'BPF package metadata malformed')
            values[key] = value
        host.require(values == {'Package': package, 'Version': frozen.DEBS[package][0], 'Architecture': 'amd64'},
                     'BPF package metadata differs')
        process = subprocess.Popen(['/usr/bin/dpkg-deb', '--fsys-tarfile', str(archive)], env=env,
                                   stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        seen = set()
        try:
            with tarfile.open(fileobj=process.stdout, mode='r|') as contents:
                count = total = 0
                for member in contents:
                    count += 1
                    total += member.size
                    host.require(count <= 100000 and total <= 1024 ** 3, 'BPF package content exceeds bound')
                    name = member.name.removeprefix('./')
                    if name not in wanted.values():
                        continue
                    host.require(name not in seen and member.isfile() and member.uid == member.gid == 0 and
                                 member.mode & 0o022 == 0 and member.mode & 0o111 and
                                 0 < member.size <= 256 * 1024 ** 2, 'BPF package executable unsafe')
                    seen.add(name)
                    stream = contents.extractfile(member)
                    host.require(stream is not None, 'BPF package executable unreadable')
                    sha = hashlib.sha256()
                    size = 0
                    prefix = b''
                    with stream:
                        while True:
                            block = stream.read(1024 * 1024)
                            if not block:
                                break
                            if not prefix:
                                prefix = block[:64]
                            size += len(block)
                            sha.update(block)
                    host.require(size == member.size and prefix[:6] == b'\x7fELF\x02\x01' and
                                 len(prefix) >= 20 and struct.unpack_from('<H', prefix, 18)[0] == 62,
                                 'BPF package executable is not complete x86_64 ELF')
                    role = next(role for role, path in wanted.items() if path == name)
                    result[role] = {'package': package, 'version': values['Version'],
                                    'path': '/' + name, 'sha256': sha.hexdigest()}
            process.stdout.close()
            stderr = process.stderr.read(65537)
            process.wait(timeout=30)
            host.require(process.returncode == 0 and not stderr and seen == set(wanted.values()),
                         'BPF package executable inventory incomplete')
        finally:
            if process.poll() is None:
                process.kill()
            process.wait()
            process.stdout.close()
            process.stderr.close()
        host.require(host.digest(archive)[0] == frozen.DEBS[package][2], 'BPF package changed during inspection')
    return result




def _input_bytes(path, expected, maximum):
    fd = source_boot._open_regular(path)
    try:
        before = source_boot._identity(os.fstat(fd))
        size = os.fstat(fd).st_size
        source_boot._need(0 < size <= maximum, 'observer input size invalid')
        data = source_boot._pread(fd, 0, size)
        source_boot._need(source_boot._identity(os.fstat(fd)) == before ==
                          source_boot._identity(os.stat(path, follow_symlinks=False)) and
                          hashlib.sha256(data).hexdigest() == expected,
                          'observer input changed or digest differs')
        return data
    finally:
        os.close(fd)


def _binding_section(host, path, expected):
    raw = _input_bytes(path, host.digest(path)[0], 64 * 1024 ** 2)
    host.require(len(raw) >= 64 and raw[:7] == b'\x7fELF\x02\x01\x01' and
                 struct.unpack_from('<HHI', raw, 16) == (1, 62, 1), 'native module ELF invalid')
    offset = struct.unpack_from('<Q', raw, 40)[0]
    width, count, names_index = struct.unpack_from('<HHH', raw, 58)
    host.require(width == 64 and 0 < count <= 4096 and 0 < names_index < count and
                 offset <= len(raw) and count * width <= len(raw) - offset, 'native ELF table invalid')
    sections = [struct.unpack_from('<IIQQQQIIQQ', raw, offset + i * width) for i in range(count)]
    strings = sections[names_index]
    host.require(strings[1] == 3 and strings[4] <= len(raw) and strings[5] <= len(raw) - strings[4],
                 'native ELF strings invalid')
    names = raw[strings[4]:strings[4] + strings[5]]
    selected = []
    for section in sections:
        host.require(section[0] < len(names), 'native ELF section name invalid')
        end = names.find(b'\0', section[0])
        host.require(end >= section[0], 'native ELF section name unterminated')
        if names[section[0]:end] == b'.g2_observer_binding':
            selected.append(section)
    host.require(len(selected) == 1, 'native observer binding missing or duplicated')
    section = selected[0]
    host.require(section[1] == 1 and section[2] & 1 == 0 and section[5] == len(expected) and
                 section[4] <= len(raw) and section[5] <= len(raw) - section[4] and
                 raw[section[4]:section[4] + section[5]] == expected,
                 'native observer binding differs')


def newc(entries):
    result = bytearray()
    for inode, (name, mode, body) in enumerate(entries + [('TRAILER!!!', 0, b'')], 1):
        encoded = name.encode('ascii') + b'\0'
        fields = (inode, mode, 0, 0, 1, 0, len(body), 0, 0, 0, 0, len(encoded), 0)
        result.extend(b'070701' + b''.join(('%08x' % value).encode('ascii') for value in fields))
        result.extend(encoded)
        result.extend(b'\0' * (-len(result) % 4))
        result.extend(body)
        result.extend(b'\0' * (-len(result) % 4))
    return bytes(result)


def _require_static_broker(host, raw):
    host.require(len(raw) >= 64 and raw[:7] == b'\x7fELF\x02\x01\x01' and
                 struct.unpack_from('<HHI', raw, 16) == (2, 62, 1),
                 'authority seed is not a fixed x86-64 executable ELF')
    offset = struct.unpack_from('<Q', raw, 32)[0]
    size, width, count = struct.unpack_from('<HHH', raw, 52)
    host.require(size == 64 and width == 56 and 0 < count <= 128 and
                 offset >= size and offset + width * count <= len(raw),
                 'authority seed program headers invalid')
    for index in range(count):
        kind = struct.unpack_from('<I', raw, offset + width * index)[0]
        host.require(kind != 3, 'authority seed must not contain PT_INTERP')


def build(host, frozen, env, attempt, prepared, boot, binary, go_sources, supervisor_sources):
    import source_build
    metadata = boot.metadata
    manifests = native_sources(host)
    native = attempt / 'native'
    os.mkdir(native, 0o700)
    original = host.SOURCE_ROOT / 'source_supervisor/bpf-authority/native'
    for relative, expected in sorted(manifests.items()):
        target = native / relative
        target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        host.require(source_build._copy(host, original / relative, target, 0o600) == expected,
                     'native source changed while snapshotting')
    os.mkdir(native / 'pinned', 0o700)
    host.require(host.digest(AUDIT_SOURCE)[0] == AUDIT_SOURCE_SHA256 and
                 source_build._copy(host, AUDIT_SOURCE, native / 'pinned/kernel-audit.h', 0o400) == AUDIT_SOURCE_SHA256,
                 'private audit source changed')
    headers = Path(metadata['headers'])
    generic, common = headers / ('linux-headers-' + source_boot.RELEASE), headers / 'linux-headers-6.8.0-147'
    kernel = _input_bytes(metadata['kernel']['path'], metadata['kernel']['sha256'], 1024 ** 3)
    system_map = _input_bytes(metadata['system_map']['path'], metadata['system_map']['sha256'], 32 * 1024 ** 2)
    config = _input_bytes(metadata['config']['path'], metadata['config']['sha256'], 1024 * 1024)
    symvers = _input_bytes(generic / 'Module.symvers', metadata['module_symvers_sha256'], 32 * 1024 ** 2)
    binding = source_kernel_observer.observer_bindings(kernel, system_map, config,
                                                       metadata['header_manifest_sha256'], symvers)
    binding_bytes = source_kernel_observer.binding_bytes(binding)
    binding_sha256 = hashlib.sha256(binding_bytes).hexdigest()
    source_build._write(attempt / 'observer-bindings.json', binding_bytes, 0o400)
    source_build._write(native / 'observer_bindings.generated.h', source_kernel_observer.generated_header(binding), 0o400)
    source_boot._sync_dir(native)
    source_boot._sync_dir(attempt)
    host.require(_input_bytes(attempt / 'observer-bindings.json', binding_sha256, 16384) == binding_bytes,
                 'observer binding readback differs')
    logs = attempt / 'source-build/logs'
    source_build._run(host, ['/usr/bin/make', '-C', str(generic), 'M=' + str(native),
                            'CC=/usr/bin/gcc', 'HOSTCC=/usr/bin/gcc', 'modules'],
                      host.RUN, env, 'bpf-native-module', logs, timeout=900)
    include_dirs = (generic / 'include/generated/uapi', generic / 'arch/x86/include/generated/uapi',
                    common / 'include/uapi', common / 'arch/x86/include/uapi', common / 'include')
    policy_args = ['/usr/bin/clang', '-target', 'bpfel', '-O2', '-g', '-nostdinc', '-D__EXPORTED_HEADERS__']
    for directory in include_dirs:
        host.check_path(directory)
        policy_args.extend(('-I', str(directory)))
    policy_args.extend(('-c', str(native / 'policy.c'), '-o', str(native / 'g2-policy.o')))
    source_build._run(host, policy_args, host.RUN, env, 'bpf-native-policy', logs, timeout=300)
    module = native / 'g2_bpf_guard.ko'
    _binding_section(host, module, binding_bytes)
    executables = package_executables(host, frozen, env, prepared)
    host.require(type(metadata['package_executables']) is dict and set(metadata['package_executables']) == {'python', 'systemd'},
                 'BPF image executable roles differ')
    executables.update(metadata['package_executables'])
    input_manifest = {'go': go_sources, 'native': manifests, 'audit_source_sha256': AUDIT_SOURCE_SHA256,
                      'module': {name: host.digest(host.SOURCE_ROOT / name)[0] for name in ('go.mod', 'go.sum')},
                      'supervisor': supervisor_sources,
                      'runner': {'source_kernel_observer.py': host.digest(host.SOURCE_ROOT / 'source_kernel_observer.py')[0]}}
    bootstrap = {'version': 1, 'kernel_release': source_boot.RELEASE,
                 'source_inputs_sha256': hashlib.sha256(host.canonical(input_manifest)).hexdigest(),
                 'kernel_sha256': metadata['kernel']['sha256'],
                 'original_initrd_sha256': metadata['initrd']['sha256'],
                 'broker_sha256': host.digest(binary)[0], 'guard_sha256': host.digest(module)[0],
                 'policy_sha256': host.digest(native / 'g2-policy.o')[0],
                 'generator_sha256': go_sources['policy.go'], 'observer_bindings_sha256': binding_sha256,
                 'scripts': {'source_guest': supervisor_sources['source_guest.py'],
                             'reboot_observer': supervisor_sources['reboot_observer.py']},
                 'package_executables': executables}
    bootstrap_bytes = host.canonical(bootstrap).removesuffix(b'\n')
    host.require(len(bootstrap_bytes) <= 16384, 'bootstrap manifest exceeds fixed limit')
    final = attempt / 'boot'
    os.mkdir(final, 0o700)
    source_build._write(final / 'bootstrap.json', bootstrap_bytes, 0o400)
    source_build._copy(host, Path(metadata['kernel']['path']), final / 'vmlinuz', 0o400)
    source_build._copy(host, Path(metadata['initrd']['path']), final / 'initrd.original', 0o400)
    broker_bytes = _input_bytes(binary, bootstrap['broker_sha256'], 64 * 1024 ** 2)
    _require_static_broker(host, broker_bytes)
    entries = [('g2-early-init', stat.S_IFREG | 0o500, broker_bytes),
               ('g2-bpf-authority', stat.S_IFREG | 0o500, broker_bytes),
               ('g2-policy.o', stat.S_IFREG | 0o400, (native / 'g2-policy.o').read_bytes()),
               ('g2_bpf_guard.ko', stat.S_IFREG | 0o400, module.read_bytes()),
               ('g2-bootstrap.json', stat.S_IFREG | 0o400, bootstrap_bytes)]
    original_initrd = (final / 'initrd.original').read_bytes()
    inspection = source_boot.assert_initrd_no_collisions(original_initrd, [entry[0] for entry in entries])
    appended = original_initrd + b'\0' * (-len(original_initrd) % 4) + newc(entries)
    host.require(len(appended) <= 1024 ** 3, 'augmented initrd exceeds bound')
    source_build._write(final / 'initrd', appended, 0o400)
    source_boot._sync_dir(final)
    source_boot._sync_dir(attempt)
    boot_inputs = {'version': 1, 'base_sha256': metadata['base_sha256'], 'kernel_release': source_boot.RELEASE,
                   'kernel_path': 'boot/vmlinuz', 'kernel_bytes': metadata['kernel']['size'],
                   'kernel_sha256': metadata['kernel']['sha256'], 'original_initrd_path': 'boot/initrd.original',
                   'original_initrd_sha256': metadata['initrd']['sha256'], 'initrd_path': 'boot/initrd',
                   'initrd_bytes': len(appended), 'initrd_sha256': hashlib.sha256(appended).hexdigest(),
                   'bootstrap_manifest_sha256': hashlib.sha256(bootstrap_bytes).hexdigest(),
                   'observer_bindings_sha256': binding_sha256,
                   'command_line': COMMAND_LINE}
    return {'boot_inputs': boot_inputs, 'bootstrap_manifest': bootstrap, 'native_sources': manifests,
            'native_inputs': input_manifest, 'boot_derivation': metadata, 'initrd_inspection': inspection,
            'native_artifacts': {'module_sha256': host.digest(module)[0],
                                 'policy_sha256': host.digest(native / 'g2-policy.o')[0]}}
