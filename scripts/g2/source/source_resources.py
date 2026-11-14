"""Versioned per-member source VM resource admission and sampled protection."""
import fcntl
import hashlib
import os
from pathlib import Path
import re
import stat
import time

POLICY = 'houfeng.source-vm.operational.v2'
QUANTUM_BYTES = 268435456
RESERVE_BYTES = 1073741824
WRITABLE_VIRTUAL_BYTES = 51539607552
GROWTH_LIMIT_BYTES = 4294967296
FINAL_RECEIPT_ALLOWANCE_BYTES = 1048576
CIDATA_CAPACITY_BYTES = 528482304
_MAX = 2**63 - 1
_REASONS = frozenset(('disk_admission', 'disk_reserve', 'growth_limit', 'staging_limit', 'measurement', 'enospc'))
_PHASES = frozenset(('prepare', 'launch', 'running', 'after_wait', 'reboot', 'reboot_running', 'after_reboot_wait', 'finalize', 'receipt'))
_KEYS = frozenset(('version', 'policy', 'seed_inputs_sha256', 'payload_bytes', 'seed_capacity_bytes',
                   'cidata_capacity_bytes', 'writable_virtual_bytes', 'staging_limit_bytes',
                   'growth_limit_bytes', 'reserve_bytes', 'final_receipt_allowance_bytes'))


class SourceResourceError(RuntimeError):
    def __init__(self, reason, phase):
        if reason not in _REASONS or phase not in _PHASES:
            raise ValueError('invalid resource error')
        self.reason, self.phase = reason, phase
        super().__init__('source resource capacity: %s (%s)' % (reason, phase))


def _capacity(payload):
    q = QUANTUM_BYTES
    return max(2 * 1024**3, (((5 * payload + 3) // 4 + q + q - 1) // q) * q)


def _staging(payload, capacity):
    q = QUANTUM_BYTES
    return ((payload + capacity + CIDATA_CAPACITY_BYTES + q + q - 1) // q) * q


def _validate(host, budget):
    host.require(type(budget) is dict and set(budget) == _KEYS, 'resource budget keys invalid')
    host.require(type(budget['version']) is int and budget['version'] == 1 and budget['policy'] == POLICY,
                 'resource policy invalid')
    host.require(type(budget['seed_inputs_sha256']) is str and
                 re.fullmatch('[0-9a-f]{64}', budget['seed_inputs_sha256']) is not None, 'seed digest invalid')
    for name in _KEYS - {'version', 'policy', 'seed_inputs_sha256'}:
        host.require(type(budget[name]) is int and 0 <= budget[name] <= _MAX, 'resource number invalid')
    p = budget['payload_bytes']
    c = _capacity(p)
    host.require(c <= 8 * 1024**3 and budget['seed_capacity_bytes'] == c and
                 budget['staging_limit_bytes'] == _staging(p, c) and
                 budget['cidata_capacity_bytes'] == CIDATA_CAPACITY_BYTES and
                 budget['writable_virtual_bytes'] == WRITABLE_VIRTUAL_BYTES and
                 budget['growth_limit_bytes'] == GROWTH_LIMIT_BYTES and
                 budget['reserve_bytes'] == RESERVE_BYTES and
                 budget['final_receipt_allowance_bytes'] == FINAL_RECEIPT_ALLOWANCE_BYTES,
                 'resource budget geometry invalid')


def make_budget(host, build, case_bytes):
    host.require(type(case_bytes) is bytes and len(case_bytes) <= 4096, 'case bytes invalid')
    inputs = build['seed_inputs']
    host.require(type(inputs) is dict and {'user-data', 'network-config'} <= set(inputs), 'seed inputs invalid')
    source = Path(build['_attempt_dir']) / 'seed'
    host.check_path(source)
    host.require({entry.name for entry in source.iterdir()} == set(inputs), 'build seed contains unknown entries')
    sizes = {}
    for name, expected in inputs.items():
        host.require(type(name) is str and re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None and
                     name not in ('.', '..', 'case.json', 'SHA256SUMS') and type(expected) is str and
                     re.fullmatch('[0-9a-f]{64}', expected) is not None, 'seed input invalid')
        path = source / name
        info = os.stat(path, follow_symlinks=False)
        host.require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1,
                     'unsafe seed input')
        actual, size = host.digest(path)
        after = os.stat(path, follow_symlinks=False)
        host.require((info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
                     (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) and
                     actual == expected and size == info.st_size, 'seed input changed')
        sizes[name] = size
    names = sorted((set(inputs) - {'user-data', 'network-config'}) | {'case.json'})
    payload = sum(sizes[name] for name in names if name != 'case.json') + len(case_bytes)
    payload += sum(len(('0' * 64 + '  ' + name + '\n').encode('ascii')) for name in names)
    c = _capacity(payload)
    result = {'version': 1, 'policy': POLICY, 'seed_inputs_sha256': hashlib.sha256(host.canonical(inputs)).hexdigest(),
              'payload_bytes': payload, 'seed_capacity_bytes': c, 'cidata_capacity_bytes': CIDATA_CAPACITY_BYTES,
              'writable_virtual_bytes': WRITABLE_VIRTUAL_BYTES, 'staging_limit_bytes': _staging(payload, c),
              'growth_limit_bytes': GROWTH_LIMIT_BYTES, 'reserve_bytes': RESERVE_BYTES,
              'final_receipt_allowance_bytes': FINAL_RECEIPT_ALLOWANCE_BYTES}
    _validate(host, result)
    return result


def allocated_tree_bytes(host, directory):
    host.check_path(directory)
    root = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        root_info = os.fstat(root)
        device = root_info.st_dev
        def visit(fd):
            info = os.fstat(fd)
            host.require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and
                         info.st_dev == device and not (stat.S_IMODE(info.st_mode) & 0o022), 'unsafe charged directory')
            total = 0
            for name in os.listdir(fd):
                item = os.stat(name, dir_fd=fd, follow_symlinks=False)
                host.require(item.st_dev == device and item.st_uid == os.geteuid(), 'unsafe charged entry')
                if stat.S_ISDIR(item.st_mode):
                    child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                    try:
                        held = os.fstat(child)
                        host.require((held.st_dev, held.st_ino) == (item.st_dev, item.st_ino), 'charged directory changed')
                        total += visit(child)
                    finally:
                        os.close(child)
                else:
                    host.require(stat.S_ISREG(item.st_mode) and item.st_nlink == 1, 'unsafe charged file')
                    child = os.open(name, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                    try:
                        held = os.fstat(child)
                        host.require(stat.S_ISREG(held.st_mode) and held.st_nlink == 1 and held.st_uid == os.geteuid() and
                                     (held.st_dev, held.st_ino) == (item.st_dev, item.st_ino), 'charged file changed')
                        total += held.st_blocks * 512
                    finally:
                        os.close(child)
            return total
        return visit(root)
    finally:
        os.close(root)


class SourceResourceGuard:
    def __init__(self, host, case_dir, budget):
        _validate(host, budget)
        self.host, self.case_dir, self.budget = host, Path(case_dir), dict(budget)
        self.a0 = self.baseline = self.peak = self.final = self.minimum = None
        self.samples = 0
        self.abort = None
        self.phase = 'prepare'
        self.last_sample = 0.0

    def _error(self, reason, phase):
        if self.abort is None:
            self.abort = {'reason': reason, 'phase': phase}
        raise SourceResourceError(self.abort['reason'], self.abort['phase'])

    def _host_sample(self, gate):
        fs = os.statvfs(self.case_dir)
        free = fs.f_bavail * fs.f_frsize
        if not gate:
            return free, None, None
        matches = re.findall(r'^MemAvailable:\s+([0-9]+)\s+kB$', Path('/proc/meminfo').read_text(encoding='ascii'), re.M)
        self.host.require(len(matches) == 1, 'MemAvailable unavailable')
        mem = int(matches[0]) * 1024
        self.host.require(mem >= 4 * 1024**3, 'insufficient available memory')
        fd = os.open('/dev/kvm', os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            self.host.require(stat.S_ISCHR(os.fstat(fd).st_mode), '/dev/kvm is not a character device')
            api = fcntl.ioctl(fd, 0xAE00, 0)
            self.host.require(api == 12, 'KVM API unavailable or unexpected')
        finally:
            os.close(fd)
        return free, mem, api

    def _sample(self, phase, gate=False):
        try:
            free, mem, api = self._host_sample(gate)
            try:
                allocated = allocated_tree_bytes(self.host, self.case_dir)
            except self.host.Failure as exc:
                try:
                    self._error('measurement', phase)
                except SourceResourceError as resource:
                    raise resource from exc
        except OSError as exc:
            if exc.errno == 28:
                try:
                    self._error('enospc', phase)
                except SourceResourceError as resource:
                    raise resource from exc
            try:
                self._error('measurement', phase)
            except SourceResourceError as resource:
                raise resource from exc
        except self.host.Failure:
            if gate:
                raise
            self._error('measurement', phase)
        self.last_sample = time.monotonic()
        self.samples += 1
        self.minimum = free if self.minimum is None else min(self.minimum, free)
        self.final = allocated
        self.peak = allocated if self.peak is None else max(self.peak, allocated)
        return free, mem, api, allocated

    def _observation(self, phase, free, mem, api, required, growth):
        return {'phase': phase, 'disk_available_bytes': free, 'mem_available_bytes': mem,
                'kvm_api_version': api, 'required_disk_bytes': required, 'observed_growth_bytes': growth}

    def check_prepare(self, pending_bytes=0):
        self.host.require(self.phase == 'prepare' and type(pending_bytes) is int and pending_bytes >= 0,
                          'invalid prepare check')
        if self.abort is not None:
            self._error(self.abort['reason'], self.abort['phase'])
        free, mem, api, allocated = self._sample('prepare', self.a0 is None)
        if self.a0 is None:
            self.a0 = allocated
        used = max(0, allocated - self.a0)
        if used > self.budget['staging_limit_bytes']:
            self._error('staging_limit', 'prepare')
        required = RESERVE_BYTES + GROWTH_LIMIT_BYTES + max(0, self.budget['staging_limit_bytes'] - used, pending_bytes)
        if free < required:
            self._error('disk_admission', 'prepare')
        return self._observation('prepare', free, mem, api, required, 0)

    def begin_launch(self, reboot=False):
        phase = 'reboot' if reboot else 'launch'
        self.host.require((self.phase == 'after_wait' if reboot else self.phase == 'prepare'), 'invalid launch phase')
        if self.abort is not None:
            self._error(self.abort['reason'], self.abort['phase'])
        free, mem, api, allocated = self._sample(phase, True)
        if not reboot:
            required, growth = RESERVE_BYTES + GROWTH_LIMIT_BYTES, 0
        else:
            growth = max(0, self.peak - self.baseline)
            if growth + FINAL_RECEIPT_ALLOWANCE_BYTES > GROWTH_LIMIT_BYTES:
                self._error('growth_limit', phase)
            required = max(0, GROWTH_LIMIT_BYTES - growth) + RESERVE_BYTES
        if free < required:
            self._error('disk_admission', phase)
        if not reboot:
            self.baseline = self.peak = allocated
        self.phase = 'reboot_running' if reboot else 'running'
        return self._observation(phase, free, mem, api, required, growth)

    def check_live(self, force=False):
        self.host.require(self.phase in ('running', 'reboot_running'), 'invalid live phase')
        if self.abort is not None:
            self._error(self.abort['reason'], self.abort['phase'])
        if not force and time.monotonic() - self.last_sample < 0.2:
            return
        free, _, _, _ = self._sample(self.phase)
        if free < RESERVE_BYTES:
            self._error('disk_reserve', self.phase)
        if self.peak - self.baseline > GROWTH_LIMIT_BYTES:
            self._error('growth_limit', self.phase)

    def after_wait(self, reboot=False):
        self.host.require(self.phase == ('reboot_running' if reboot else 'running'), 'invalid wait phase')
        phase = 'after_reboot_wait' if reboot else 'after_wait'
        self.phase = phase
        free, _, _, _ = self._sample(phase)
        if free < RESERVE_BYTES:
            self._error('disk_reserve', phase)
        if self.peak - self.baseline > GROWTH_LIMIT_BYTES:
            self._error('growth_limit', phase)
        if self.abort is not None:
            self._error(self.abort['reason'], self.abort['phase'])
        return self.snapshot()

    def finish_member(self):
        self.host.require(self.phase in ('after_wait', 'after_reboot_wait'), 'invalid finalize phase')
        if self.abort is not None:
            self._error(self.abort['reason'], self.abort['phase'])
        free, _, _, _ = self._sample('finalize')
        growth = max(0, self.peak - self.baseline)
        if growth + FINAL_RECEIPT_ALLOWANCE_BYTES > GROWTH_LIMIT_BYTES:
            self._error('growth_limit', 'finalize')
        if free < RESERVE_BYTES + FINAL_RECEIPT_ALLOWANCE_BYTES:
            self._error('disk_reserve', 'finalize')
        self.phase = 'finalize'
        return self._observation('finalize', free, None, None, RESERVE_BYTES + FINAL_RECEIPT_ALLOWANCE_BYTES, growth)

    def snapshot(self):
        return {'version': 1, 'baseline_allocated_bytes': self.baseline, 'peak_allocated_bytes': self.peak,
                'final_allocated_bytes': self.final, 'minimum_disk_available_bytes': self.minimum,
                'sample_count': self.samples, 'peak_growth_bytes': None if self.baseline is None else max(0, self.peak - self.baseline),
                'abort': None if self.abort is None else dict(self.abort)}
