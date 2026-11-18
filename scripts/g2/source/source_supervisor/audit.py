"""Supplementary guest-root Linux audit loss and forbidden-event detection.

The audit owner is registered before genesis with fixed rules and a checked
loss counter. Queue emptiness is never treated as proof of event completeness;
the fixed launch gate and trusted service tree provide that authority. Raw
command arguments are never persisted or emitted.
"""
from dataclasses import dataclass
import errno
import hashlib
import os
import re
import select
import shlex
import socket
import struct
import threading
import time

from journal import JournalError, boot_id

class AuditPending(JournalError):
    pass


NETLINK_AUDIT = 9
AUDIT_GET = 1000
AUDIT_SET = 1001
AUDIT_ADD_RULE = 1011
AUDIT_LIST_RULES = 1013
AUDIT_SYSCALL = 1300
AUDIT_CONFIG_CHANGE = 1305
AUDIT_SOCKADDR = 1306
AUDIT_EXECVE = 1309
AUDIT_EOE = 1320
AUDIT_BPF = 1334
NLMSG_ERROR = 2
NLMSG_DONE = 3
NLMSG_OVERRUN = 4
NLM_F_REQUEST = 1
NLM_F_MULTI = 2
NLM_F_ACK = 4
AUDIT_FILTER_EXIT_PREPEND = 0x14
AUDIT_ALWAYS = 2
AUDIT_ARCH = 11
AUDIT_EQUAL = 0x40000000
X86_64 = 0xc000003e
I386 = 0x40000003
SERIAL = re.compile(rb'^audit\(([0-9]+)\.([0-9]{3}):([0-9]+)\):\s*')


@dataclass(frozen=True, slots=True)
class _KernelConfigSnapshot:
    raw: bytes
    release: str
    boot_id: str


def _guest_identity():
    if os.geteuid() != 0 or os.uname().machine != 'x86_64':
        raise JournalError('audit requires the isolated x86_64 guest root')
    with open('/sys/class/dmi/id/product_name', 'r', encoding='ascii') as stream:
        machine = stream.read(256).strip()
    if machine not in ('Standard PC (Q35 + ICH9, 2009)', 'KVM') or set(os.listdir('/sys/class/net')) != {'lo'}:
        raise JournalError('audit setup refused outside the no-network guest')


def _check_kernel_config(raw):
    if type(raw) is not bytes or len(raw) > 1024 * 1024 or b'\x00' in raw:
        raise JournalError('guest kernel configuration is malformed or oversized')
    try:
        config = raw.decode('ascii')
    except UnicodeDecodeError as exc:
        raise JournalError('guest kernel configuration is malformed') from exc
    if '# CONFIG_X86_X32_ABI is not set' not in config or 'CONFIG_AUDIT=y' not in config or 'CONFIG_AUDITSYSCALL=y' not in config:
        raise JournalError('guest kernel cannot prove complete syscall audit coverage')


def capture_kernel_config():
    _guest_identity()
    release = os.uname().release
    current_boot = boot_id()
    if not release or '/' in release or release in ('.', '..'):
        raise JournalError('guest kernel release is invalid')
    try:
        with open('/boot/config-' + release, 'rb') as stream:
            raw = stream.read(1024 * 1024 + 1)
    except OSError as exc:
        raise JournalError('guest kernel configuration is unavailable') from exc
    _check_kernel_config(raw)
    if os.uname().release != release or boot_id() != current_boot:
        raise JournalError('guest kernel identity changed during configuration capture')
    return _KernelConfigSnapshot(raw, release, current_boot)


def _guest_only(snapshot, deadline):
    _guest_identity()
    if (type(snapshot) is not _KernelConfigSnapshot or type(snapshot.release) is not str or
            type(snapshot.boot_id) is not str or snapshot.release != os.uname().release or
            snapshot.boot_id != boot_id()):
        raise JournalError('guest kernel configuration identity differs')
    _check_kernel_config(snapshot.raw)
    # io_uring could otherwise provide an unobserved alternative syscall path.
    with open('/proc/sys/kernel/io_uring_disabled', 'w', encoding='ascii') as stream:
        stream.write('2\n')
    with open('/proc/sys/kernel/io_uring_disabled', 'r', encoding='ascii') as stream:
        if stream.read(16).strip() != '2':
            raise JournalError('io_uring could not be disabled')
    for pid in os.listdir('/proc'):
        if time.monotonic() >= deadline:
            raise JournalError('audit setup deadline exceeded')
        if not pid.isdecimal():
            continue
        try:
            for fd in os.listdir('/proc/%s/fd' % pid):
                if time.monotonic() >= deadline:
                    raise JournalError('audit setup deadline exceeded')
                try:
                    if os.readlink('/proc/%s/fd/%s' % (pid, fd)) == 'anon_inode:[io_uring]':
                        raise JournalError('preexisting io_uring capability')
                except FileNotFoundError:
                    continue
        except FileNotFoundError:
            continue


def _rule(arch, syscalls):
    words = [0] * 64
    for number in syscalls:
        if not 0 <= number < 2048:
            raise JournalError('audit syscall outside rule mask')
        words[number // 32] |= 1 << (number % 32)
    fields = [0] * 64
    values = [0] * 64
    operators = [0] * 64
    fields[0], values[0], operators[0] = AUDIT_ARCH, arch, AUDIT_EQUAL
    return struct.pack('=260I', AUDIT_FILTER_EXIT_PREPEND, AUDIT_ALWAYS, 1,
                       *words, *fields, *values, *operators, 0)


def _datagrams(data):
    offset = 0
    while offset < len(data):
        if len(data) - offset < 16:
            raise JournalError('truncated audit netlink header')
        length, kind, flags, sequence, sender = struct.unpack_from('=IHHII', data, offset)
        # Kernel unicast audit records deliberately report payload length in
        # nlmsg_len; control replies retain the normal header-inclusive form.
        # See Linux v6.8 kernel/audit.c audit_log_end()/audit_make_reply().
        total = length + 16 if kind >= 1100 and sequence == 0 else length
        if total < 16 or offset + total > len(data):
            raise JournalError('truncated audit netlink message')
        yield kind, flags, sequence, sender, data[offset + 16:offset + total]
        if kind >= 1100 and sequence == 0 and offset + total == len(data):
            # audit_log_end sends the actual text length without tail padding.
            return
        offset += (total + 3) & ~3
    if offset != len(data):
        raise JournalError('invalid audit netlink alignment')


def _socket():
    sock = socket.socket(socket.AF_NETLINK, socket.SOCK_RAW | socket.SOCK_CLOEXEC, NETLINK_AUDIT)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 8 * 1024 * 1024)
    if sock.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF) < 256 * 1024:
        sock.close()
        raise JournalError('audit receive buffer too small')
    sock.bind((0, 0))
    return sock


def _request(sock, kind, payload=b'', multipart=False, ack=False, timeout=5, on_event=None):
    seq = int.from_bytes(os.urandom(4), 'little') or 1
    flags = NLM_F_REQUEST | (NLM_F_ACK if ack else 0)
    sock.send(struct.pack('=IHHII', 16 + len(payload), kind, flags, seq, sock.getsockname()[0]) + payload)
    deadline = time.monotonic() + timeout
    results = []
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([sock], [], [], remaining)[0]:
            raise JournalError('audit control request timed out')
        data, ancillary, flags, address = sock.recvmsg(65536)
        if address[0] != 0 or ancillary or flags & (socket.MSG_CTRUNC | socket.MSG_TRUNC):
            raise JournalError('audit control datagram invalid')
        completed = False
        for response_kind, response_flags, response_seq, _, body in _datagrams(data):
            if response_seq != seq:
                if response_kind in (NLMSG_ERROR, NLMSG_OVERRUN) or response_seq != 0:
                    raise JournalError('audit control stream loss or error')
                if response_kind in (AUDIT_SYSCALL, AUDIT_SOCKADDR, AUDIT_EXECVE, AUDIT_EOE, AUDIT_BPF, AUDIT_CONFIG_CHANGE):
                    if on_event is None:
                        raise JournalError('unexpected audit control event')
                    on_event(response_kind, body)
                continue
            if completed:
                raise JournalError('duplicate audit control completion')
            if response_kind == NLMSG_ERROR:
                if len(body) < 4 or struct.unpack_from('=i', body)[0] != 0:
                    raise JournalError('kernel rejected audit control request')
                completed = ack
            elif response_kind == NLMSG_DONE:
                completed = True
            elif response_kind == NLMSG_OVERRUN:
                raise JournalError('audit control overrun')
            else:
                results.append((response_kind, body))
                completed = not multipart and not ack
        if completed:
            return results


def _status(sock, *, deadline=None, on_event=None):
    timeout = 5 if deadline is None else min(5, deadline - time.monotonic())
    if timeout <= 0:
        raise JournalError('audit health deadline exceeded')
    result = _request(sock, AUDIT_GET, timeout=timeout, on_event=on_event)
    if len(result) != 1 or result[0][0] != AUDIT_GET or len(result[0][1]) < 36:
        raise JournalError('kernel audit status unavailable')
    data = result[0][1]
    values = struct.unpack_from('=9I', data)
    return dict(zip(('mask', 'enabled', 'failure', 'pid', 'rate_limit', 'backlog_limit', 'lost', 'backlog', 'features'), values))


def _set(sock, mask, enabled=0, failure=0, pid=0, rate=0, backlog=0, *, deadline=None, on_event=None):
    data = struct.pack('=11I', mask, enabled, failure, pid, rate, backlog, 0, 0, 0, 0, 0)
    timeout = 5 if deadline is None else min(5, deadline - time.monotonic())
    if timeout <= 0:
        raise JournalError('audit setup deadline exceeded')
    _request(sock, AUDIT_SET, data, ack=True, timeout=timeout, on_event=on_event)


def _event_key(body):
    match = SERIAL.match(body)
    if match is None:
        raise JournalError('audit record has invalid event stamp')
    if len(match[1]) > 19 or len(match[3]) > 10:
        raise JournalError('audit event stamp out of range')
    seconds, milliseconds, serial = (int(value) for value in match.groups())
    if seconds >= (1 << 63) or milliseconds > 999 or not 0 < serial < (1 << 32):
        raise JournalError('audit event stamp out of range')
    return (seconds, milliseconds, serial)


def _wire_key(kind, body):
    event = _event_key(body)
    digest = hashlib.sha256(b'HOUFENG-G2-AUDIT-WIRE-V1\0' + struct.pack('<H', kind) + body).hexdigest()
    return event, kind, digest


def _row_key(row):
    return row['stamp']['seconds'], row['stamp']['milliseconds'], row['serial']


def _fields(body):
    key = _event_key(body)
    match = SERIAL.match(body)
    try:
        text = body[match.end():].rstrip(b'\0').decode('ascii')
        tokens = shlex.split(text, posix=True)
    except (UnicodeError, ValueError) as exc:
        raise JournalError('audit record is not parseable') from exc
    fields = {}
    for token in tokens:
        name, sep, value = token.partition('=')
        if not sep or not name:
            raise JournalError('audit record field is incomplete')
        if name in fields:
            raise JournalError('duplicate audit record field')
        fields[name] = value
    return key, fields


_BPF_COMMANDS = {0: 'MAP_CREATE', 1: 'MAP_LOOKUP_ELEM', 2: 'MAP_UPDATE_ELEM', 3: 'MAP_DELETE_ELEM',
                 5: 'PROG_LOAD', 8: 'PROG_ATTACH', 9: 'PROG_DETACH', 13: 'PROG_GET_FD_BY_ID',
                 15: 'OBJ_GET_INFO_BY_FD', 16: 'PROG_QUERY', 22: 'MAP_FREEZE', 28: 'LINK_CREATE'}
_MAX_PRE_CUT_RECORDS = 18432
_MAX_PRE_CUT_BYTES = 16777216
_MAX_POST_CUT_OCCURRENCES = 36864
_MAX_BPF_AUDIT_FACTS = 16384
_MAX_PENDING_AUDIT_EVENTS = 4096


def _bpf_record(body):
    key, fields = _fields(body)
    match = SERIAL.match(body)
    try:
        tokens = body[match.end():].rstrip(b'\0').decode('ascii').split()
    except UnicodeError as exc:
        raise JournalError('invalid audit BPF record') from exc
    if (set(fields) != {'prog-id', 'op'} or len(tokens) != 2 or
            not any(re.fullmatch(r'prog-id=[0-9]+', token) for token in tokens) or
            not any(re.fullmatch(r'op=(LOAD|UNLOAD)', token) for token in tokens) or
            re.fullmatch(r'[0-9]+', fields['prog-id']) is None or
            not 0 < int(fields['prog-id']) < (1 << 32) or fields['op'] not in ('LOAD', 'UNLOAD')):
        raise JournalError('invalid audit BPF record')
    return {'stamp': {'seconds': key[0], 'milliseconds': key[1]}, 'serial': key[2], 'program_id': int(fields['prog-id']), 'operation': fields['op']}


def _syscall_fact(key, values):
    shapes = {'arch': r'[0-9a-fA-F]+', 'syscall': r'[0-9]+', 'pid': r'[0-9]+', 'ppid': r'[0-9]+',
              'exit': r'-?[0-9]+', 'a0': r'[0-9a-fA-F]+'}
    if any(type(values.get(key)) is not str or re.fullmatch(pattern, values[key]) is None
           for key, pattern in shapes.items()):
        raise JournalError('audited syscall field syntax invalid')
    try:
        arch, number = int(values['arch'], 16), int(values['syscall'], 10)
        pid, ppid = int(values['pid'], 10), int(values['ppid'], 10)
        result, argument = int(values['exit'], 10), int(values['a0'], 16)
        success = values['success']
    except (KeyError, ValueError) as exc:
        raise JournalError('audited syscall identity invalid') from exc
    if (arch not in (X86_64, I386) or not 0 <= number < (1 << 32) or
            not 0 < pid < (1 << 32) or not 0 <= ppid < (1 << 32) or
            not -(1 << 63) <= result < (1 << 63) or not 0 <= argument < (1 << 64) or
            success not in ('yes', 'no') or (success == 'yes') != (result >= 0)):
        raise JournalError('audited syscall state invalid')
    return {'stamp': {'seconds': key[0], 'milliseconds': key[1]}, 'serial': key[2], 'arch': arch, 'number': number, 'pid': pid, 'ppid': ppid,
            'success': success == 'yes', 'result': result, 'argument': argument}


def _correlate(unicast_bpf, unicast_syscalls, records, baseline):
    """Compare two independently received streams using pinned kernel identities."""
    if len(unicast_bpf) > _MAX_BPF_AUDIT_FACTS or len(unicast_syscalls) > _MAX_BPF_AUDIT_FACTS:
        raise JournalError('audit fact limit exceeded')
    audit_rows, objects, transactions, tasks = [], {}, {}, {}
    covered_guard_leaves = set()
    guard_sequences = set()
    guard_by_object = {}
    publications, removals = {}, {}
    grant_closes = {}
    for entry in records:
        kind, body = entry['kind'], entry['body']
        if kind == 'principal':
            cookie = body['task_cookie']
            tasks.setdefault(cookie, set()).add((body['tgid'], body['tid'], body['start_ns']))
        elif kind == 'guard':
            row = body['record']
            if row['sequence'] in guard_sequences:
                raise JournalError('duplicate audit guard sequence')
            guard_sequences.add(row['sequence'])
            if entry['sequence'] > baseline and row['kind'] == 8:
                covered_guard_leaves.add(row['sequence'])
            if row['kind'] in (1, 2):
                table = publications if row['kind'] == 1 else removals
                key = (row['object_cookie'], row['kernel_id'])
                if key in table:
                    raise JournalError('duplicate program lifecycle event')
                table[key] = row['sequence']
            if row['kind'] in (7, 8):
                guard_by_object.setdefault((row['kind'], row['object_cookie'], row['kernel_id'], row['operation']), []).append(row)
            if row['kind'] in (9, 10, 11, 12):
                detail = row['detail']
                tasks.setdefault(row['task_cookie'], set()).add((detail['tgid'], detail['tid'], row['task_start_ns']))
        elif kind == 'object':
            objects.setdefault(body['cookie'], []).append(body)
        elif kind == 'transaction':
            transactions.setdefault(body['id'], []).append(body)
        elif kind == 'grant_object_cleanup' and body['step'] == 1:
            if body['object_cookie'] in grant_closes:
                raise JournalError('duplicate grant object close')
            grant_closes[body['object_cookie']] = body
        elif kind == 'audit' and entry['sequence'] > baseline:
            audit_rows.append(body)
    stacks, guard_pairs = {}, {}
    frames = sorted((item for values in guard_by_object.values() for item in values), key=lambda item: item['sequence'])
    for frame in frames:
        stack = stacks.setdefault(frame['task_cookie'], [])
        if frame['kind'] == 7:
            if frame['flags'] & 1 or frame['audit_serial'] != 0 or len(stack) >= 8:
                raise JournalError('invalid audit guard entry')
            stack.append(frame)
        else:
            if not stack:
                raise JournalError('audit guard return lacks entry')
            prior = stack.pop()
            if (any(prior[key] != frame[key] for key in ('object_cookie', 'kernel_id', 'operation', 'task_start_ns')) or
                    prior['flags'] & 6 != frame['flags'] & 6):
                raise JournalError('nested audit guard frame differs')
            guard_pairs[frame['sequence']] = prior
    if any(stacks.values()):
        raise JournalError('audit guard frame remains open')
    by_program_id = {}
    for cookie, snapshots in objects.items():
        if len({item['class'] for item in snapshots}) != 1:
            raise JournalError('object cookie changed class')
        programs = [item for item in snapshots if item['class'] == 'program']
        if not programs:
            continue
        first = programs[0]
        identity = (first['kernel_id'], first['owner_transaction'], first['publication_seq'])
        if any((item['kernel_id'], item['owner_transaction'], item['publication_seq']) != identity for item in programs):
            raise JournalError('program incarnation facts changed')
        key = (cookie, first['kernel_id'])
        if publications.get(key) != first['publication_seq']:
            raise JournalError('program publication guard identity differs')
        for item in programs:
            if item['removal_seq'] is not None and removals.get(key) != item['removal_seq']:
                raise JournalError('program removal guard identity differs')
        by_program_id.setdefault(first['kernel_id'], set()).add(cookie)
    for kernel_id, cookies in by_program_id.items():
        if sum((cookie, kernel_id) not in removals for cookie in cookies) > 1:
            raise JournalError('reused program ID has unsettled incarnations')
    if len(audit_rows) > 2 * _MAX_BPF_AUDIT_FACTS:
        raise JournalError('audit comparison limit exceeded')
    for values in tasks.values():
        if len(values) != 1:
            raise JournalError('audit task cookie identity changed')
    by_tgid = {}
    for cookie, values in tasks.items():
        by_tgid.setdefault(next(iter(values))[0], set()).add(cookie)
    bpf_rows = [row for row in audit_rows if row['record_type'] == 'BPF']
    syscall_rows = [row for row in audit_rows if row['record_type'] == 'SYSCALL']
    if len(bpf_rows) != len(unicast_bpf) or len(syscall_rows) != len(unicast_syscalls):
        raise JournalError('audit unicast and broker records differ')
    seen_guards = set()
    row_by_serial = {}
    for raw, row in zip(unicast_bpf, bpf_rows):
        if _row_key(raw) != _row_key(row) or raw['operation'] != row['operation']:
            raise JournalError('audit BPF sequence differs')
        cookie = row['object_cookie']
        guard_key = (8, cookie, raw['program_id'], 0 if raw['operation'] == 'LOAD' else 1)
        matches = [item for item in guard_by_object.get(guard_key, ()) if
                   bool(item['flags'] & 1) == (row['context'] == 'syscall') and
                   (item['detail']['seconds'], item['detail']['nanoseconds'] // 1000000, item['audit_serial']) == _row_key(raw) and
                   item['sequence'] not in seen_guards]
        if not matches:
            raise JournalError('audit BPF incarnation is missing')
        if len({(item['task_cookie'], item['task_start_ns']) for item in matches}) != 1:
            raise JournalError('audit BPF incarnation is ambiguous')
        leave = min(matches, key=lambda item: item['sequence'])
        enter = guard_pairs.get(leave['sequence'])
        if enter is None:
            raise JournalError('audit BPF guard pair differs')
        seen_guards.add(leave['sequence'])
        if row['context'] == 'syscall':
            identities = tasks.get(row['task_cookie'], set())
            if (row['task_cookie'] != leave['task_cookie'] or len(identities) != 1 or
                    next(iter(identities))[2] != leave['task_start_ns']):
                raise JournalError('audit BPF task binding differs')
        elif row['task_cookie'] is not None:
            raise JournalError('standalone audit invented an actor')
        owner = objects.get(cookie, [])
        if (not owner or any(item['kernel_id'] != raw['program_id'] or item['class'] != 'program' for item in owner) or
                owner[0]['publication_seq'] >= enter['sequence']):
            raise JournalError('audit BPF object identity differs')
        txns = transactions.get(row['transaction_id'], [])
        if not txns:
            raise JournalError('audit BPF transaction missing')
        if row['command'] is not None and not any(tx['command'] == row['command'] for tx in txns):
            raise JournalError('audit BPF transaction command differs')
        if raw['operation'] == 'UNLOAD':
            authorized = any(tx['cleanup']['policy'] in ('close_unattached', 'detach_then_close', 'auto_empty') and
                             any(step['object_cookie'] == cookie for step in tx['cleanup']['steps']) for tx in txns)
            close = grant_closes.get(cookie)
            if close is not None:
                authorized = authorized or (close['action'] == 'close_ref' and close['result'] == 0 and
                    close['creator_transaction_id'] == owner[0]['owner_transaction'] and
                    any(tx['id'] == close['creator_transaction_id'] and tx['grant_id'] == close['grant_id'] for tx in txns))
            if not authorized:
                raise JournalError('audit BPF unload lacks authorized cleanup')
        elif not any(item['owner_transaction'] == row['transaction_id'] for item in owner):
            raise JournalError('audit BPF load transaction differs')
        row_by_serial.setdefault(_row_key(raw), []).append(row)
    if not covered_guard_leaves.issubset(seen_guards):
        raise JournalError('guard audit event lacks both stream records')
    raw_by_serial = {_row_key(item): item for item in unicast_syscalls}
    if len(raw_by_serial) != len(unicast_syscalls):
        raise JournalError('duplicate audited syscall serial')
    compared = set()
    for raw, row in zip(unicast_syscalls, syscall_rows):
        serial = _row_key(row)
        if _row_key(raw) != serial or serial in compared or row['context'] != 'syscall' or row['result'] != raw['result']:
            raise JournalError('audit syscall facts differ')
        compared.add(serial)
        cookie = row['task_cookie']
        identities = tasks.get(cookie, set())
        if cookie is None or len(identities) != 1 or next(iter(identities))[0] != raw['pid'] or by_tgid.get(raw['pid']) != {cookie}:
            raise JournalError('audit syscall task identity is ambiguous')
        is_bpf = (raw['arch'], raw['number']) in ((X86_64, 321), (I386, 357))
        if is_bpf:
            command = _BPF_COMMANDS.get(raw['argument'])
            if command is None or row['command'] != command:
                raise JournalError('audit syscall command differs')
            txns = transactions.get(row['transaction_id'], [])
            if row['transaction_id'] is None:
                if raw['success']:
                    raise JournalError('unowned successful BPF syscall')
            elif not any(tx['executor_cookie'] == cookie and tx['command'] == command and tx['result'] == raw['result'] for tx in txns):
                raise JournalError('audit syscall transaction differs')
        else:
            attached = row_by_serial.get(serial, [])
            ordinary = ((raw['arch'] == X86_64 and raw['number'] in (49, 59, 322)) or
                        (raw['arch'] == I386 and raw['number'] in (11, 358, 361)) or
                        (raw['arch'] == I386 and raw['number'] == 102 and raw['argument'] == 2))
            cleanup_call = (raw['arch'], raw['number']) in ((X86_64, 3), (X86_64, 60), (X86_64, 231),
                                                           (I386, 6), (I386, 1), (I386, 252))
            if (not attached or row['command'] is not None or
                    (not ordinary and (not cleanup_call or any(item['operation'] != 'UNLOAD' for item in attached)))):
                raise JournalError('unexpected audited syscall lifecycle')
        if any(item['context'] != 'syscall' or item['task_cookie'] != cookie for item in row_by_serial.get(serial, [])):
            raise JournalError('audit syscall lifecycle binding differs')
    if compared != set(raw_by_serial):
        raise JournalError('audit syscall set differs')
    for serial, rows in row_by_serial.items():
        if any(row['context'] == 'syscall' for row in rows) and serial not in compared:
            raise JournalError('audit BPF syscall context missing')


class AuditStream:
    def __init__(self, consume, snapshot, authority, deadline):
        self.sock = None
        self.thread = None
        self.consume = consume
        self.authority = authority
        self.invalid = False
        self.active = False
        self.pending = {}
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.error = None
        self.bpf_facts = []
        self.syscall_facts = []
        self.bpf_serials = set()
        self.completed_serials = set()
        self.delivery_pending = 0
        self.observation_version = 0
        self._prefix_remaining = None
        self._prefix_events = set()
        self._pre_cut = []
        self._pre_cut_bytes = 0
        self._post_wire = {}
        self._post_wire_count = 0
        try:
            nonce = os.urandom(16).hex()
            with authority.checkpoint(nonce, deadline):
                deadline = min(deadline, authority._deadline, authority._checkpoint_deadline)
                _guest_only(snapshot, deadline)
                self.sock = _socket()
                status = _status(self.sock, deadline=deadline, on_event=self._record)
                if (status['pid'] != 0 or status['enabled'] != 2 or status['failure'] != 1 or
                        status['rate_limit'] != 0 or status['backlog_limit'] != 16384 or status['lost'] != 0):
                    raise JournalError('audit configuration was not sealed before owner claim')
                self.lost = status['lost']
                _set(self.sock, 4, pid=os.getpid(), deadline=deadline, on_event=self._record)
                status = _status(self.sock, deadline=deadline, on_event=self._record)
                if (status['enabled'] != 2 or status['pid'] != os.getpid() or status['failure'] != 1 or
                        status['rate_limit'] != 0 or status['backlog_limit'] != 16384 or status['lost'] != self.lost):
                    raise JournalError('audit owner claim or loss counter differs')
                cut = authority.audit_cut(nonce, deadline)
                self._install_cut(cut, authority.evidence_records())
                self.sock.setblocking(False)
                self.thread = threading.Thread(target=self._run, name='realm-audit', daemon=True)
                self.thread.start()
                self.await_health(deadline, require_active=False)
        except BaseException as exc:
            self.invalid = True
            self.stop.set()
            if self.thread is not None:
                self.thread.join(max(0, min(2, deadline - time.monotonic())))
            if self.sock is not None:
                self.sock.close()
            if self.thread is not None and self.thread.is_alive():
                raise JournalError('audit setup failed with reader not joined') from exc
            raise

    def _install_cut(self, cut, records):
        self.baseline = cut['boundary_sequence']
        remaining = {}
        events = set()
        count = 0
        for record in records:
            if record['sequence'] > self.baseline or record['kind'] != 'audit_wire':
                continue
            row = record['body']
            event = _row_key(row)
            key = (event, row['record_type'], row['payload_sha256'])
            count += 1
            remaining[key] = remaining.get(key, 0) + 1
            if row['occurrence'] != remaining[key]:
                raise JournalError('audit prefix occurrence sequence differs')
            events.add(event)
        if count != cut['wire_count']:
            raise JournalError('audit prefix wire count differs')
        self._prefix_remaining, self._prefix_events = remaining, events
        queued, self._pre_cut = self._pre_cut, []
        self._pre_cut_bytes = 0
        for kind, body in queued:
            if self._record(kind, body) is not None:
                raise JournalError('unexpected delivery before audit activation')

    def _record(self, kind, body):
        if kind not in (AUDIT_SYSCALL, AUDIT_CONFIG_CHANGE, AUDIT_SOCKADDR, AUDIT_EXECVE, AUDIT_EOE, AUDIT_BPF):
            return None
        self.observation_version += 1
        if self._prefix_remaining is None:
            if len(self._pre_cut) >= _MAX_PRE_CUT_RECORDS or len(body) > _MAX_PRE_CUT_BYTES - self._pre_cut_bytes:
                raise JournalError('audit pre-cut queue exceeded limit')
            self._pre_cut_bytes += len(body)
            self._pre_cut.append((kind, bytes(body)))
            return None
        wire = _wire_key(kind, body)
        if wire[0] in self._prefix_events:
            if self._prefix_remaining.get(wire, 0) <= 0:
                raise JournalError('audit prefix fragment is duplicate or differs')
            self._prefix_remaining[wire] -= 1
            return None
        if self._post_wire_count >= _MAX_POST_CUT_OCCURRENCES:
            raise JournalError('audit wire queue exceeded limit')
        self._post_wire_count += 1
        self._post_wire[wire] = self._post_wire.get(wire, 0) + 1
        if kind == AUDIT_CONFIG_CHANGE:
            raise JournalError('unexpected audit configuration change after cut')
        if kind == AUDIT_BPF:
            fact = _bpf_record(body)
            if _row_key(fact) in self.completed_serials or len(self.bpf_facts) >= _MAX_BPF_AUDIT_FACTS:
                raise JournalError('late or excessive audit BPF record')
            self.bpf_facts.append(fact)
            self.bpf_serials.add(_row_key(fact))
            return None
        serial, fields = _fields(body)
        if serial in self.completed_serials:
            raise JournalError('duplicate or invalid audited syscall stamp')
        event = self.pending.setdefault(serial, {})
        if len(self.pending) > _MAX_PENDING_AUDIT_EVENTS:
            raise JournalError('audit event queue exceeded limit')
        if kind == AUDIT_SYSCALL:
            if 'syscall' in event:
                raise JournalError('duplicate audit syscall record')
            event['syscall'] = fields
        elif kind == AUDIT_SOCKADDR:
            if 'sockaddr' in event:
                raise JournalError('duplicate audit socket record')
            if 'saddr' not in fields or not fields['saddr']:
                raise JournalError('audit socket address missing')
            event['sockaddr'] = fields['saddr']
        elif kind == AUDIT_EOE:
            if fields or 'syscall' not in event:
                raise JournalError('incomplete audited syscall')
            del self.pending[serial]
            values = event['syscall']
            fact = _syscall_fact(serial, values)
            arch, number, argument = fact['arch'], fact['number'], fact['argument']
            is_bpf = (arch, number) in ((X86_64, 321), (I386, 357))
            related = serial in self.bpf_serials
            if is_bpf or related:
                if len(self.syscall_facts) >= _MAX_BPF_AUDIT_FACTS:
                    raise JournalError('audit syscall fact limit exceeded')
                self.syscall_facts.append(fact)
            self.completed_serials.add(serial)
            operation = None
            if arch == X86_64 and number in (59, 322) or arch == I386 and number in (11, 358):
                operation = 'exec'
            elif arch == X86_64 and number == 49 or arch == I386 and number == 361:
                operation = 'bind'
            elif arch == I386 and number == 102 and argument == 2:
                operation = 'bind'
            elif not is_bpf:
                if not related or (arch, number) not in ((X86_64, 3), (X86_64, 60), (X86_64, 231),
                                                         (I386, 6), (I386, 1), (I386, 252)):
                    raise JournalError('unexpected audited syscall')
            if operation is not None and fact['success']:
                if not values.get('exe') or (operation == 'bind' and not event.get('sockaddr')):
                    raise JournalError('successful audited origin lacks required facts')
            if operation is not None and self.active:
                return {'operation': operation, 'pid': fact['pid'], 'ppid': fact['ppid'], 'success': fact['success'],
                        'exe': values.get('exe'), 'sockaddr': event.get('sockaddr')}
        return None

    def _run(self):
        try:
            while not self.stop.is_set():
                if not select.select([self.sock], [], [], 0.1)[0]:
                    continue
                try:
                    data, ancillary, flags, address = self.sock.recvmsg(65536)
                except BlockingIOError:
                    continue
                if address[0] != 0 or ancillary or flags & (socket.MSG_TRUNC | socket.MSG_CTRUNC) or not data:
                    raise JournalError('audit stream datagram invalid')
                deliveries = []
                with self.lock:
                    for kind, _, sequence, _, body in _datagrams(data):
                        if kind in (NLMSG_ERROR, NLMSG_OVERRUN) or sequence != 0:
                            raise JournalError('audit stream loss or error')
                        delivery = self._record(kind, body)
                        if delivery is not None:
                            deliveries.append(delivery)
                    self.delivery_pending += len(deliveries)
                for delivery in deliveries:
                    self.consume(delivery)
                    with self.lock:
                        self.delivery_pending -= 1
        except BaseException as exc:
            self.error = exc
            self.invalid = True

    def activate(self):
        self.check_health(time.monotonic() + 5, require_active=False)
        with self.lock:
            if self.pending:
                raise JournalError('partial audit events before genesis')
            self.active = True

    def check_health(self, deadline, require_active=True):
        if self.invalid or self.error is not None or (require_active and not self.active) or not self.thread.is_alive():
            raise JournalError('audit stream unavailable')
        if getattr(self.authority, '_depth', 0) <= 0:
            self.invalid = True
            raise JournalError('audit observation lacks authority checkpoint')
        checkpoint_deadline = getattr(self.authority, '_checkpoint_deadline', None)
        if checkpoint_deadline is None:
            self.invalid = True
            raise JournalError('audit checkpoint deadline unavailable')
        deadline = min(deadline, self.authority._deadline, checkpoint_deadline)
        if time.monotonic() >= deadline:
            self.invalid = True
            raise JournalError('audit health deadline exceeded')
        control = _socket()
        try:
            status = _status(control, deadline=deadline)
            if (status['enabled'] != 2 or status['failure'] != 1 or status['pid'] != os.getpid() or status['lost'] != self.lost or
                    status['rate_limit'] != 0 or status['backlog_limit'] != 16384):
                raise JournalError('audit coverage or loss counter changed')
            with self.lock:
                if self.error is not None or self.invalid:
                    raise JournalError('audit stream failed')
                pending = bool(self.pending) or self.delivery_pending != 0
                version = self.observation_version
                bpf = [dict(item) for item in self.bpf_facts]
                syscalls = [dict(item) for item in self.syscall_facts]
                wire = dict(self._post_wire)
            if status['backlog'] or pending or select.select([self.sock], [], [], 0)[0]:
                raise AuditPending('audit stream has undrained events')
            first_guard = self.authority.read_status(deadline=deadline)
            if (first_guard['state'] != 'sealed' or first_guard['sticky'] != 0 or
                    first_guard['lost'] != 0 or first_guard['probe_missed'] != 0):
                raise JournalError('BPF guard observation is unavailable or lossy')
            while not self.authority.read_evidence(deadline=deadline)['complete']:
                pass
            first_evidence = self.authority.evidence_records()
            final_guard = self.authority.read_status(deadline=deadline)
            if (final_guard['state'] != 'sealed' or final_guard['sticky'] != 0 or
                    final_guard['lost'] != 0 or final_guard['probe_missed'] != 0):
                raise JournalError('BPF guard observation is unavailable or lossy')
            while not self.authority.read_evidence(deadline=deadline)['complete']:
                pass
            evidence = self.authority.evidence_records()
            if first_guard['sequence'] != final_guard['sequence'] or len(first_evidence) != len(evidence):
                raise AuditPending('BPF evidence advanced during audit correlation')
            status = _status(control, deadline=deadline)
            if (status['enabled'] != 2 or status['failure'] != 1 or status['pid'] != os.getpid() or status['lost'] != self.lost or
                    status['rate_limit'] != 0 or status['backlog_limit'] != 16384):
                raise JournalError('audit coverage or loss counter changed')
            with self.lock:
                if self.error is not None or self.invalid:
                    raise JournalError('audit stream failed')
                if self.observation_version != version or self.pending or self.delivery_pending:
                    raise AuditPending('audit observation changed during correlation')
            if status['backlog'] or select.select([self.sock], [], [], 0)[0]:
                raise AuditPending('audit stream has undrained events')
            expected_wire = {}
            for record in evidence:
                if record['sequence'] <= self.baseline or record['kind'] != 'audit_wire':
                    continue
                row = record['body']
                key = (_row_key(row), row['record_type'], row['payload_sha256'])
                expected_wire[key] = expected_wire.get(key, 0) + 1
                if row['occurrence'] != expected_wire[key] or key[0] in self._prefix_events:
                    raise JournalError('audit wire occurrence differs after cut')
            if wire != expected_wire:
                raise AuditPending('audit multicast and unicast records differ')
            _correlate(bpf, syscalls, evidence, self.baseline)
        except AuditPending as exc:
            if time.monotonic() >= deadline:
                self.invalid = True
                raise JournalError('audit join deadline exceeded') from exc
            raise
        except BaseException:
            self.invalid = True
            raise
        finally:
            control.close()

    def await_health(self, deadline, require_active=True):
        while True:
            try:
                self.check_health(deadline, require_active=require_active)
                return
            except AuditPending as exc:
                if self.invalid or self.error is not None:
                    raise JournalError('audit stream failed while joining') from exc
                until = min(deadline, self.authority._deadline, self.authority._checkpoint_deadline)
                if time.monotonic() >= until:
                    self.invalid = True
                    raise JournalError('audit join deadline exceeded') from exc
                select.select([], [], [], min(0.02, until - time.monotonic()))

    def invalidate(self):
        self.invalid = True

    def close(self):
        self.stop.set()
        self.thread.join(2)
        if self.thread.is_alive():
            self.invalid = True
            raise JournalError('audit stream did not stop')
        self.sock.close()
        self.invalid = True
