"""Fresh four-volume guest preparation before any PostgreSQL initdb."""
import hashlib
import os
import re
import stat
import subprocess

from census import _mount_id
from journal import JournalError

BASE = '/var/lib/g2'
SERIALS = ('G2APP', 'G2W1', 'G2W2', 'G2CONTROL')
NAMES = ('app', 'w1', 'w2', 'control')
SIZE = 8 * 1024 ** 3
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C', 'LC_ALL': 'C',
       'HOME': '/root', 'DEBIAN_FRONTEND': 'noninteractive'}


def command(argv, timeout=60, allowed=(0,)):
    try:
        result = subprocess.run(argv, env=ENV, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise JournalError('fixed guest command unavailable') from exc
    if result.returncode not in allowed or len(result.stdout) > 1024 * 1024 or len(result.stderr) > 1024 * 1024:
        raise JournalError('fixed guest command failed')
    return result


def _text(path, limit=65536):
    with open(path, 'rb') as stream:
        data = stream.read(limit + 1)
    if len(data) > limit:
        raise JournalError('guest preflight input too large')
    return data.decode('ascii', 'strict')


def mounts():
    lines = _text('/proc/self/mountinfo', 4 * 1024 * 1024).splitlines()
    output = []
    for line in lines:
        before, sep, after = line.partition(' - ')
        left, right = before.split(), after.split()
        if not sep or len(left) < 6 or len(right) < 3 or not left[0].isdigit():
            raise JournalError('mount inventory malformed')
        output.append({'id': int(left[0]), 'parent': int(left[1]), 'device': left[2], 'target': left[4],
                       'options': frozenset(left[5].split(',')), 'type': right[0], 'source': right[1],
                       'super_options': frozenset(right[2].split(','))})
    return output


def verify_guest():
    if os.geteuid() != 0 or os.getegid() != 0 or os.uname().machine != 'x86_64':
        raise JournalError('isolated root guest required')
    machine = _text('/sys/class/dmi/id/product_name', 256).strip()
    if machine not in ('Standard PC (Q35 + ICH9, 2009)', 'KVM') or set(os.listdir('/sys/class/net')) != {'lo'}:
        raise JournalError('guest has an unexpected machine or network interface')
    release = _text('/etc/os-release')
    if not re.search(r'^ID=ubuntu$', release, re.M) or not re.search(r'^VERSION_ID="?24\.04"?$', release, re.M):
        raise JournalError('guest operating system differs')
    current = mounts()
    seed = _carrier_mount(current, '/mnt/g2-seed', 'G2SEED', 'ext4')
    cidata = _carrier_mount(current, '/mnt/g2-cidata', 'G2CIDATA', 'vfat', 'CIDATA')
    if seed['device'] == cidata['device']:
        raise JournalError('seed carriers share a block device')
    return {'machine': machine, 'seed_mount': seed, 'cidata_mount': cidata}


def _carrier_mount(current, target, serial, filesystem, label=None):
    label = label or serial
    entries = [entry for entry in current if entry['target'] == target]
    if (len(entries) != 1 or entries[0]['type'] != filesystem or
            not {'ro', 'nosuid', 'nodev', 'noexec'} <= entries[0]['options']):
        raise JournalError('read-only carrier mount unavailable')
    if filesystem == 'ext4' and not {'noload', 'norecovery'} & entries[0]['super_options']:
        raise JournalError('seed filesystem recovery is not disabled')
    link = '/dev/disk/by-id/virtio-' + serial
    device = os.path.realpath(link)
    if not device.startswith('/dev/') or '/' in device[5:]:
        raise JournalError('carrier device path differs')
    fd = os.open(device, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        info, named = os.fstat(fd), os.stat(link)
        identity = '%d:%d' % (os.major(info.st_rdev), os.minor(info.st_rdev))
        mount_link = link + '-part1' if serial == 'G2CIDATA' else link
        mount_device = os.path.realpath(mount_link)
        mount_info = os.stat(mount_link)
        mount_identity = '%d:%d' % (os.major(mount_info.st_rdev), os.minor(mount_info.st_rdev))
        parent_sys = os.path.realpath('/sys/dev/block/' + identity)
        mounted_sys = os.path.realpath('/sys/dev/block/' + mount_identity)
        if (not stat.S_ISBLK(info.st_mode) or not stat.S_ISBLK(mount_info.st_mode) or
                info.st_rdev != named.st_rdev or mount_identity != entries[0]['device'] or
                _text('/sys/dev/block/' + identity + '/serial', 128).strip() != serial or
                command(['/usr/sbin/blockdev', '--getro', device]).stdout.strip() != b'1' or
                command(['/usr/sbin/blockdev', '--getro', mount_device]).stdout.strip() != b'1' or
                (serial == 'G2CIDATA' and (not mounted_sys.startswith(parent_sys + '/') or
                 _text('/sys/dev/block/' + mount_identity + '/partition', 32).strip() != '1')) or
                (serial != 'G2CIDATA' and mount_identity != identity)):
            raise JournalError('carrier block identity or read-only state differs')
        devices = command(['/usr/sbin/blkid', '-t', 'LABEL=' + label, '-o', 'device']).stdout.decode('ascii').splitlines()
        if len(devices) != 1 or os.path.realpath(devices[0]) != mount_device:
            raise JournalError('carrier label is absent or ambiguous')
        uuid_result = command(['/usr/sbin/blkid', '-s', 'UUID', '-o', 'value', mount_device], allowed=(0, 2))
        uuid = uuid_result.stdout.decode('ascii').strip() or None
        size = command(['/usr/sbin/blockdev', '--getsize64', device]).stdout.strip()
        if not size.isdigit() or int(size) <= 0 or (filesystem == 'ext4' and not uuid):
            raise JournalError('carrier filesystem identity differs')
        size = int(size)
        digest = hashlib.sha256()
        offset = 0
        while offset < size:
            data = os.pread(fd, min(1024 * 1024, size - offset), offset)
            if not data:
                raise JournalError('carrier block device shortened')
            digest.update(data)
            offset += len(data)
        if os.fstat(fd).st_rdev != info.st_rdev or os.stat(link).st_rdev != info.st_rdev:
            raise JournalError('carrier block device changed during readback')
        result = dict(entries[0], options=sorted(entries[0]['options']), super_options=sorted(entries[0]['super_options']))
        result.update(serial=serial, label=label, uuid=uuid, bytes=size, read_only=True,
                      whole_device=identity, block_sha256=digest.hexdigest())
        return result
    finally:
        os.close(fd)


def _device(serial, known_mounts):
    link = '/dev/disk/by-id/virtio-' + serial
    target = os.path.realpath(link)
    if not target.startswith('/dev/') or '/' in target[5:]:
        raise JournalError('unexpected virtio device path')
    fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        info, named = os.fstat(fd), os.stat(link)
        if not stat.S_ISBLK(info.st_mode) or (info.st_dev, info.st_ino, info.st_rdev) != (named.st_dev, named.st_ino, named.st_rdev):
            raise JournalError('virtio disk identity differs')
        major_minor = '%d:%d' % (os.major(info.st_rdev), os.minor(info.st_rdev))
        if major_minor in {entry['device'] for entry in known_mounts}:
            raise JournalError('virtio disk is already mounted')
        sys = '/sys/dev/block/' + major_minor
        if _text(sys + '/serial', 128).strip() != serial or os.path.exists(sys + '/partition'):
            raise JournalError('virtio disk serial or type differs')
        real = os.path.realpath(sys)
        for name in os.listdir('/sys/class/block'):
            child = '/sys/class/block/' + name
            if os.path.exists(child + '/partition') and os.path.realpath(child).startswith(real + '/'):
                raise JournalError('virtio disk contains a partition')
        if command(['/usr/sbin/blockdev', '--getsize64', target]).stdout.strip() != str(SIZE).encode():
            raise JournalError('virtio disk size differs')
        probe = command(['/usr/sbin/blkid', '-p', target], allowed=(0, 2))
        if probe.returncode != 2 or probe.stdout.strip():
            raise JournalError('virtio disk has an existing signature')
        return {'serial': serial, 'path': target, 'device': major_minor, 'rdev': info.st_rdev,
                'node_dev': info.st_dev, 'node_ino': info.st_ino, 'bytes': SIZE}
    finally:
        os.close(fd)


def inspect_blank_disks():
    current = mounts()
    devices = [_device(serial, current) for serial in SERIALS]
    if len({device['rdev'] for device in devices}) != 4:
        raise JournalError('virtio disks are not independent')
    return devices


def _sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def prepare_volumes(devices):
    if type(devices) is not list or len(devices) != 4 or [x.get('serial') for x in devices] != list(SERIALS):
        raise JournalError('unexpected disk inventory')
    if inspect_blank_disks() != devices:
        raise JournalError('disk inventory changed before formatting')
    os.mkdir(BASE, 0o700)
    os.chmod(BASE, 0o711)
    for name in NAMES:
        os.mkdir(BASE + '/' + name, 0o700)
    _sync_dir(BASE)
    output = {}
    for name, device in zip(NAMES, devices):
        if _device(device['serial'], mounts()) != device:
            raise JournalError('disk changed immediately before formatting')
        command(['/usr/sbin/mkfs.ext4', '-F', '-m', '0', '-L', device['serial'], device['path']], timeout=300)
        target = BASE + '/' + name
        command(['/usr/bin/mount', '-t', 'ext4', '-o', 'rw,nosuid,nodev,noexec', device['path'], target], timeout=60)
        found = [entry for entry in mounts() if entry['target'] == target]
        if (len(found) != 1 or found[0]['type'] != 'ext4' or found[0]['device'] != device['device'] or
                not {'rw', 'nosuid', 'nodev', 'noexec'} <= found[0]['options']):
            raise JournalError('mounted volume identity differs')
        fd = os.open(target, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if info.st_uid != 0 or info.st_gid != 0:
                raise JournalError('new volume owner differs')
            # The coordinator can traverse CONTROL but cannot modify the
            # volume root or rename its separately owned PGDATA directory.
            os.fchmod(fd, 0o711 if name == 'control' else 0o700)
            os.mkdir('pgdata', 0o700, dir_fd=fd)
            pg = os.open('pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            try:
                pg_info = os.fstat(pg)
                if os.listdir(pg):
                    raise JournalError('new PGDATA is not empty')
                os.fsync(pg)
                output[name] = {'disk': device, 'mount_id': _mount_id(fd), 'mount_dev': info.st_dev,
                                'mount_ino': info.st_ino, 'pgdata_dev': pg_info.st_dev,
                                'pgdata_ino': pg_info.st_ino, 'pgdata_mount_id': _mount_id(pg)}
            finally:
                os.close(pg)
            if name == 'control':
                os.mkdir('maintenance', 0o700, dir_fd=fd)
                maintenance = os.open('maintenance', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                try:
                    os.fchown(maintenance, 10002, 10002)
                    os.fchmod(maintenance, 0o700)
                    item = os.fstat(maintenance)
                    output[name]['maintenance'] = {'dev': item.st_dev, 'ino': item.st_ino,
                                                   'mount_id': _mount_id(maintenance)}
                    os.fsync(maintenance)
                finally:
                    os.close(maintenance)
            os.fsync(fd)
        finally:
            os.close(fd)
    if len({item['mount_id'] for item in output.values()}) != 4:
        raise JournalError('mounted volumes are not distinct')
    return output
