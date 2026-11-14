"""Verify guest-only bridge interfaces, listeners and held cgroup identities."""
import os
import re
import stat
import select
import socket
import struct
import time

from contract import MIGRATION_CGROUP
from journal import JournalError


def _read(path, limit=1048576):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        body = os.read(fd, limit + 1)
        if len(body) > limit:
            raise JournalError('guest isolation observation too large')
        return body.decode('ascii', 'strict')
    finally:
        os.close(fd)


def _route_messages(data):
    """Decode header-inclusive, aligned netlink messages without discarding any."""
    messages = []
    offset = 0
    while offset < len(data):
        if len(data) - offset < 16:
            raise JournalError('truncated route netlink header')
        length, kind, flags, sequence, port = struct.unpack_from('=IHHII', data, offset)
        if length < 16 or length > len(data) - offset:
            raise JournalError('invalid route netlink message length')
        end = offset + length
        aligned = (end + 3) & ~3
        if aligned > len(data):
            raise JournalError('truncated route netlink alignment')
        messages.append((kind, flags, sequence, port, data[offset + 16:end]))
        offset = aligned
    if not messages:
        raise JournalError('empty route netlink datagram')
    return messages


def _attributes(data):
    result = {}
    offset = 0
    while offset < len(data):
        if len(data) - offset < 4:
            raise JournalError('truncated route netlink attribute')
        length, kind = struct.unpack_from('=HH', data, offset)
        if length < 4 or length > len(data) - offset:
            raise JournalError('invalid route netlink attribute length')
        end = offset + length
        aligned = (end + 3) & ~3
        if aligned > len(data):
            raise JournalError('truncated route netlink attribute alignment')
        kind &= 0x3fff
        result.setdefault(kind, []).append(data[offset + 4:end])
        offset = aligned
    return result


def _link_attribute(attributes, kind, *, required=False):
    values = attributes.get(kind, [])
    if len(values) > 1 or (required and not values):
        raise JournalError('missing or repeated route link attribute')
    return values[0] if values else None


def _link_string(value):
    if value is None or not value.endswith(b'\0') or b'\0' in value[:-1]:
        raise JournalError('invalid route link string')
    try:
        return value[:-1].decode('ascii', 'strict')
    except UnicodeDecodeError as exc:
        raise JournalError('invalid route link string') from exc


def _link_index(value):
    if value is None:
        return None
    if len(value) != 4:
        raise JournalError('invalid route link index')
    return struct.unpack('=I', value)[0]


def _link_message(body):
    if len(body) < 16:
        raise JournalError('truncated route link message')
    _, _, hardware, index, _, _ = struct.unpack_from('=BBHiII', body)
    if index <= 0:
        raise JournalError('invalid route interface index')
    attributes = _attributes(body[16:])
    name = _link_string(_link_attribute(attributes, 3, required=True))
    if not name or '/' in name or len(name) > 15:
        raise JournalError('invalid route interface name')
    master = _link_index(_link_attribute(attributes, 10))
    peer = _link_index(_link_attribute(attributes, 5))
    info = _link_attribute(attributes, 18)
    kind = None
    if info is not None:
        kind_value = _link_attribute(_attributes(info), 1)
        if kind_value is not None:
            kind = _link_string(kind_value)
    if name == 'lo':
        if hardware != 772 or kind is not None or master not in (None, 0):
            raise JournalError('loopback route identity invalid')
        master = 0
        peer = index if peer is None else peer
    elif kind == 'bridge':
        if master not in (None, 0):
            raise JournalError('bridge route master invalid')
        master = 0
        peer = index if peer is None else peer
    elif kind == 'veth':
        if not master or not peer:
            raise JournalError('veth route identity incomplete')
    else:
        raise JournalError('guest has an unregistered network interface')
    if not peer:
        raise JournalError('route peer index invalid')
    return name, (index, kind, master, peer)


class _TopologyWindow:
    """Bounded subscription and link observations for one network check."""

    def __init__(self, deadline):
        self.deadline = min(deadline, time.monotonic() + 5)
        self.nsfd = None
        self.sock = None
        self.sequence = 0
        self.count = 0
        self.bytes = 0
        self.finished = False

    def __enter__(self):
        try:
            self.nsfd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
            ns = os.fstat(self.nsfd)
            self.namespace = (ns.st_dev, ns.st_ino)
            self.sock = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW | socket.SOCK_CLOEXEC,
                                      socket.NETLINK_ROUTE)
            self.sock.setblocking(False)
            self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 1048576)
            if self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF) < 262144:
                raise JournalError('route netlink receive buffer too small')
            self.sock.bind((0, 0x1 | 0x10 | 0x40 | 0x100 | 0x400))
            self.port = self.sock.getsockname()[0]
            self._check_namespace()
            self._barrier()
            return self
        except BaseException:
            try:
                self._close()
            except OSError:
                pass
            raise

    def _check_namespace(self):
        held = os.fstat(self.nsfd)
        named = os.stat('/proc/self/ns/net')
        if ((held.st_dev, held.st_ino) != self.namespace or
                (named.st_dev, named.st_ino) != self.namespace):
            raise JournalError('guest network namespace changed')

    def _receive(self, wait):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise JournalError('route topology observation timed out')
        if wait and not select.select([self.sock], [], [], remaining)[0]:
            raise JournalError('route topology observation timed out')
        try:
            data, ancillary, flags, address = self.sock.recvmsg(65536)
        except BlockingIOError:
            if wait:
                raise JournalError('route topology observation lost readiness')
            return None
        except OSError as exc:
            raise JournalError('route topology observation receive failed') from exc
        if (not address or address[0] != 0 or ancillary or
                flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC)):
            raise JournalError('route netlink datagram invalid')
        self.bytes += len(data)
        messages = _route_messages(data)
        self.count += len(messages)
        if self.count > 4096 or self.bytes > 1048576:
            raise JournalError('route topology observation exceeds bounds')
        return messages

    def _request(self, index, *, dump):
        if self.finished or time.monotonic() >= self.deadline:
            raise JournalError('route topology observation timed out')
        self.sequence += 1
        sequence = self.sequence
        request_flags = 1 | (0x300 if dump else 4)
        payload = struct.pack('=BBHiII', socket.AF_UNSPEC, 0, 0, index, 0, 0)
        header = struct.pack('=IHHII', 16 + len(payload), 18, request_flags, sequence, self.port)
        try:
            if self.sock.sendto(header + payload, (0, 0)) != len(header) + len(payload):
                raise JournalError('short route netlink request')
        except OSError as exc:
            raise JournalError('route netlink request failed') from exc
        links = []
        done = False
        ack = False
        while not (done if dump else (ack and len(links) == 1)):
            for kind, flags, response_sequence, _, body in self._receive(True):
                if flags & 0x10:
                    raise JournalError('interrupted route netlink dump')
                if response_sequence != sequence:
                    raise JournalError('route topology changed or unexpected sequence')
                if kind == 16:
                    if done or (not dump and links):
                        raise JournalError('unexpected route link response')
                    links.append(_link_message(body))
                elif kind == 2 and not dump:
                    if ack or len(body) < 20 or struct.unpack_from('=i', body)[0] != 0:
                        raise JournalError('route netlink acknowledgement failed')
                    if body[4:20] != header:
                        raise JournalError('route netlink acknowledgement mismatch')
                    ack = True
                elif kind == 3 and dump:
                    if done or (len(body) not in (0, 4)) or (body and struct.unpack('=i', body)[0] != 0):
                        raise JournalError('route netlink dump failed')
                    done = True
                else:
                    raise JournalError('unexpected route netlink message')
        return links

    def _barrier(self):
        try:
            index = socket.if_nametoindex('lo')
        except OSError as exc:
            raise JournalError('loopback route index unavailable') from exc
        links = self._request(index, dump=False)
        if len(links) != 1 or links[0][0] != 'lo' or links[0][1][0] != index:
            raise JournalError('loopback route barrier mismatch')

    def snapshot(self):
        self._check_namespace()
        links = self._request(0, dump=True)
        result = {}
        indexes = set()
        for name, identity in links:
            if name in result or identity[0] in indexes:
                raise JournalError('duplicate route interface identity')
            result[name] = identity
            indexes.add(identity[0])
        seen = set(os.listdir('/sys/class/net'))
        if seen != set(result):
            raise JournalError('route and sysfs interface inventories differ')
        for name, (index, kind, master, peer) in result.items():
            path = '/sys/class/net/' + name
            try:
                sys_index = int(_read(path + '/ifindex').strip())
                sys_peer = int(_read(path + '/iflink').strip())
                master_path = path + '/master'
                sys_master = (int(_read(master_path + '/ifindex').strip())
                              if os.path.lexists(master_path) else 0)
            except (OSError, ValueError) as exc:
                raise JournalError('sysfs interface identity unavailable') from exc
            if (index != sys_index or peer != sys_peer or master != sys_master or
                    (kind == 'bridge') != os.path.isdir(path + '/bridge')):
                raise JournalError('route and sysfs interface identities differ')
        self._check_namespace()
        return result

    def finish(self):
        self._check_namespace()
        self._barrier()
        while True:
            messages = self._receive(False)
            if messages is None:
                break
            raise JournalError('route topology changed after final observation')
        if self.sock.getsockopt(socket.SOL_SOCKET, socket.SO_ERROR):
            raise JournalError('route topology observation socket error')
        self._check_namespace()
        self.finished = True
        self._close()

    def _close(self):
        try:
            if self.sock is not None:
                sock, self.sock = self.sock, None
                sock.close()
        finally:
            if self.nsfd is not None:
                fd, self.nsfd = self.nsfd, None
                os.close(fd)

    def __exit__(self, exc_type, exc, traceback):
        if exc_type is not None:
            try:
                self._close()
            except OSError:
                pass
            return False
        try:
            if not self.finished:
                raise JournalError('route topology observation unfinished')
        finally:
            self._close()
        return False


def _ipv6_route_allowed(fields, bridges, veths):
    if len(fields) != 10:
        raise JournalError('guest IPv6 route inventory malformed')
    try:
        flags = int(fields[8], 16)
        if ((fields[9] in bridges or fields[9] in veths) and
                fields[0].lower() == 'ff000000000000000000000000000000' and
                int(fields[1], 16) == 8 and fields[2].lower() == '0' * 32 and
                int(fields[3], 16) == 0 and fields[4].lower() == '0' * 32 and flags == 1):
            return True
        if fields[9] in veths:
            return (fields[0].lower() == 'fe800000000000000000000000000000' and
                    int(fields[1], 16) == 64 and fields[2].lower() == '0' * 32 and
                    int(fields[3], 16) == 0 and fields[4].lower() == '0' * 32 and flags == 1)
        if not flags & 1 or flags & 0x200:
            return True
        if fields[9] == 'lo':
            return True
        if (fields[9] in bridges and fields[0].lower().startswith('fe80') and
                int(fields[1], 16) >= 64 and int(fields[4], 16) == 0 and not flags & 2):
            return True
        return False
    except ValueError as exc:
        raise JournalError('guest IPv6 route inventory malformed') from exc


def _networks(network_ids, deadline):
    if type(network_ids) is not dict or not network_ids:
        raise JournalError('registered network inventory unavailable')
    bridges = {'br-' + value[:12] for value in network_ids.values()
               if type(value) is str and re.fullmatch(r'[0-9a-f]{64}', value)}
    if len(bridges) != len(network_ids):
        raise JournalError('registered bridge identities overlap')
    with _TopologyWindow(deadline) as window:
        initial = window.snapshot()
        seen = set(initial)
        if 'lo' not in seen or not bridges <= seen:
            raise JournalError('registered guest bridge disappeared')
        for name in bridges:
            if initial[name][1] != 'bridge':
                raise JournalError('registered bridge is not a bridge device')
        veths = seen - bridges - {'lo'}
        bridge_indexes = {initial[name][0] for name in bridges}
        for name in veths:
            if (not re.fullmatch(r'veth[0-9a-f]+', name) or initial[name][1] != 'veth' or
                    initial[name][2] not in bridge_indexes):
                raise JournalError('guest veth has an unregistered bridge')
        for family in ('tcp', 'tcp6'):
            rows = _read('/proc/net/' + family).splitlines()
            if not rows:
                raise JournalError('guest listener inventory unavailable')
            for row in rows[1:]:
                fields = row.split()
                if len(fields) < 4:
                    raise JournalError('guest listener inventory malformed')
                if fields[3] == '0A':
                    raise JournalError('guest root namespace has an unregistered TCP listener')
        for family in ('udp', 'udp6'):
            rows = _read('/proc/net/' + family).splitlines()
            if not rows:
                raise JournalError('guest datagram inventory unavailable')
            for row in rows[1:]:
                fields = row.split()
                if len(fields) < 4 or ':' not in fields[1]:
                    raise JournalError('guest datagram inventory malformed')
                try:
                    local_port = int(fields[1].rsplit(':', 1)[1], 16)
                except ValueError as exc:
                    raise JournalError('guest datagram inventory malformed') from exc
                if local_port:
                    raise JournalError('guest root namespace has an unregistered UDP endpoint')
        routes = _read('/proc/net/route').splitlines()
        if not routes:
            raise JournalError('guest route inventory unavailable')
        for row in routes[1:]:
            fields = row.split()
            if len(fields) < 8:
                raise JournalError('guest route inventory malformed')
            try:
                flags = int(fields[3], 16)
            except ValueError as exc:
                raise JournalError('guest route inventory malformed') from exc
            if flags & 1 and (fields[0] not in bridges or fields[1] == '00000000' or
                              fields[2] != '00000000' or flags & 2):
                raise JournalError('guest has an unregistered or egress route')
        ipv6_routes = _read('/proc/net/ipv6_route').splitlines()
        for row in ipv6_routes:
            fields = row.split()
            if not _ipv6_route_allowed(fields, bridges, veths):
                detail = repr({'route': row, 'bridges': sorted(bridges), 'interfaces': initial,
                               'ipv4_routes': routes, 'ipv6_routes': ipv6_routes})
                raise JournalError('guest has an active IPv6 route outside its owned bridges: ' +
                                   (detail if len(detail) <= 8192 else 'private detail exceeds bounds'))
        if window.snapshot() != initial:
            raise JournalError('guest network topology changed')
        window.finish()


def networks(network_ids, deadline):
    try:
        return _networks(network_ids, deadline)
    except (OSError, UnicodeError) as exc:
        raise JournalError('guest network observation failed') from exc


def migration_scope(identity):
    if (type(identity) is not dict or set(identity) != {'path', 'dev', 'ino'} or
            identity['path'] != MIGRATION_CGROUP or type(identity['dev']) is not int or
            type(identity['ino']) is not int or identity['dev'] < 0 or identity['ino'] <= 0):
        raise JournalError('migration cgroup inventory invalid')
    fd = os.open(MIGRATION_CGROUP, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISDIR(before.st_mode) or (before.st_uid, before.st_gid) != (0, 0) or
                stat.S_IMODE(before.st_mode) != 0o755 or
                (before.st_dev, before.st_ino) != (identity['dev'], identity['ino'])):
            raise JournalError('migration cgroup identity changed')
        with os.scandir(fd) as entries:
            for count, entry in enumerate(entries, 1):
                if count > 256 or entry.is_symlink() or entry.is_dir(follow_symlinks=False):
                    raise JournalError('migration cgroup inventory contains an invalid scope')
        for name in ('cgroup.procs', 'cgroup.threads', 'cgroup.subtree_control'):
            control = os.stat(name, dir_fd=fd, follow_symlinks=False)
            if not stat.S_ISREG(control.st_mode) or (control.st_uid, control.st_gid) != (0, 0) or control.st_mode & 0o022:
                raise JournalError('migration cgroup control is writable by the coordinator')
        def read_control(name, limit):
            control_fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            try:
                raw = os.read(control_fd, limit + 1)
                if len(raw) > limit:
                    raise JournalError('migration cgroup observation exceeds limit')
                return raw.decode('ascii', 'strict')
            finally:
                os.close(control_fd)
        words = read_control('cgroup.procs', 65536).split()
        if len(words) > 256 or any(not word.isdecimal() or int(word) <= 0 for word in words):
            raise JournalError('migration cgroup members are invalid')
        members = sorted(int(word) for word in words)
        if len(set(members)) != len(members):
            raise JournalError('migration cgroup members are duplicated')
        lines = [line.split() for line in read_control('cgroup.events', 1024).splitlines()]
        if any(len(line) != 2 for line in lines) or len({line[0] for line in lines}) != len(lines):
            raise JournalError('migration cgroup events are malformed')
        events = dict(lines)
        if events.get('populated') not in ('0', '1'):
            raise JournalError('migration cgroup populated state unavailable')
        after, path = os.fstat(fd), os.stat(MIGRATION_CGROUP, follow_symlinks=False)
        if (after.st_dev, after.st_ino) != (before.st_dev, before.st_ino) or (path.st_dev, path.st_ino) != (before.st_dev, before.st_ino):
            raise JournalError('migration cgroup changed during observation')
        return {'members': members, 'populated': int(events['populated'])}
    finally:
        os.close(fd)


def cgroups(child, writer, migration, origins):
    for name, identity in (('g2-c80-predecessor', child), ('g2-writers', writer)):
        if type(identity) is not dict or set(identity) != {'path', 'dev', 'ino'} or identity['path'] != '/sys/fs/cgroup/' + name:
            raise JournalError('cgroup inventory is invalid')
        info = os.stat(identity['path'], follow_symlinks=False)
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or
                (info.st_dev, info.st_ino) != (identity['dev'], identity['ino'])):
            raise JournalError('registered cgroup was replaced')
    writer_path = writer['path']
    if _read(writer_path + '/cgroup.procs', 65536).strip() or _read(writer_path + '/cgroup.freeze', 64).strip() != '1':
        raise JournalError('writer cgroup is not empty and frozen')
    events = dict(line.split() for line in _read(writer_path + '/cgroup.events', 1024).splitlines())
    if events.get('populated') != '0' or events.get('frozen') != '1':
        raise JournalError('writer cgroup did not settle')
    for filename in ('cgroup.freeze', 'cgroup.kill'):
        info = os.stat(writer_path + '/' + filename, follow_symlinks=False)
        if (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (0, 10002, 0o660):
            raise JournalError('writer cgroup delegation changed')
    procs = _read(child['path'] + '/cgroup.procs', 65536).split()
    if len(procs) > 1 or any(not value.isdecimal() for value in procs):
        raise JournalError('predecessor cgroup has unexpected processes')
    if procs and not origins.allowed_predecessor:
        raise JournalError('predecessor process exists outside callback')
    scope = migration_scope(migration)
    if not origins.allowed_migrator and (scope['members'] or scope['populated'] != 0):
        raise JournalError('migration cgroup is populated outside migration')
