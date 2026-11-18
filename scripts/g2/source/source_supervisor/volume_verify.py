"""Descriptor-bound live checks for the four guest PostgreSQL volumes."""
import os
import stat

from census import _mount_id
from guest_storage import BASE, NAMES, SERIALS, SIZE, _text, mounts
from journal import JournalError

def verify_volumes(expected, postgres_uid, postgres_gid, hba):
    """Reopen all four mounted disks and PGDATA directories by fixed names."""
    if type(expected) is not dict or set(expected) != set(NAMES):
        raise JournalError('volume identity inventory unavailable')
    current = mounts()
    for name, serial in zip(NAMES, SERIALS):
        entry = expected[name]
        disk = entry['disk']
        if disk['serial'] != serial or disk['bytes'] != SIZE:
            raise JournalError('volume disk binding changed')
        path = BASE + '/' + name
        matches = [item for item in current if item['target'] == path]
        if (len(matches) != 1 or matches[0]['id'] != entry['mount_id'] or
                matches[0]['device'] != disk['device'] or matches[0]['type'] != 'ext4' or
                not {'rw', 'nosuid', 'nodev', 'noexec'} <= matches[0]['options']):
            raise JournalError('volume mount changed')
        info = os.stat(disk['path'], follow_symlinks=False)
        if (not stat.S_ISBLK(info.st_mode) or
                (info.st_dev, info.st_ino, info.st_rdev) != (disk['node_dev'], disk['node_ino'], disk['rdev'])):
            raise JournalError('volume block device changed')
        sys = '/sys/dev/block/' + disk['device']
        if (_text(sys + '/serial', 128).strip() != serial or
                _text(sys + '/size', 128).strip() != str(SIZE // 512) or os.path.exists(sys + '/partition')):
            raise JournalError('volume serial or size changed')
        root = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            root_info = os.fstat(root)
            if ((root_info.st_dev, root_info.st_ino, _mount_id(root)) !=
                    (entry['mount_dev'], entry['mount_ino'], entry['mount_id']) or
                    root_info.st_uid != 0 or root_info.st_gid != 0 or
                    stat.S_IMODE(root_info.st_mode) != (0o711 if name == 'control' else 0o700)):
                raise JournalError('volume root identity changed')
            pg = os.open('pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
            try:
                pg_info = os.fstat(pg)
                if ((pg_info.st_dev, pg_info.st_ino, _mount_id(pg)) !=
                        (entry['pgdata_dev'], entry['pgdata_ino'], entry['pgdata_mount_id']) or
                        (pg_info.st_uid, pg_info.st_gid) != (postgres_uid, postgres_gid) or
                        stat.S_IMODE(pg_info.st_mode) != 0o700):
                    raise JournalError('PGDATA identity changed')
                config = os.open('pg_hba.conf', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=pg)
                try:
                    item = os.fstat(config)
                    if (not stat.S_ISREG(item.st_mode) or item.st_nlink != 1 or
                            (item.st_uid, item.st_gid) != (postgres_uid, postgres_gid) or
                            stat.S_IMODE(item.st_mode) != 0o600 or os.read(config, len(hba) + 1) != hba):
                        raise JournalError('PostgreSQL HBA policy changed')
                finally:
                    os.close(config)
            finally:
                os.close(pg)
            if name == 'control':
                fd = os.open('maintenance', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
                try:
                    actual, item = os.fstat(fd), entry['maintenance']
                    if ((actual.st_dev, actual.st_ino, _mount_id(fd)) != (item['dev'], item['ino'], item['mount_id']) or
                            (actual.st_uid, actual.st_gid) != (10002, 10002) or stat.S_IMODE(actual.st_mode) != 0o700):
                        raise JournalError('maintenance directory identity changed')
                finally:
                    os.close(fd)
        finally:
            os.close(root)
