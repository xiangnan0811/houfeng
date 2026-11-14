#!/usr/bin/env python3
"""Throwaway own-descendant probe: exit-stop versus guard FD release."""
import ctypes
import errno
import fcntl
import json
import os
import select
import signal
import threading
import time

from source_host import RUN

ROOT = str(RUN)
libc = ctypes.CDLL(None, use_errno=True)
libc.ptrace.argtypes = [ctypes.c_uint, ctypes.c_int, ctypes.c_void_p, ctypes.c_void_p]
libc.ptrace.restype = ctypes.c_long

def trace(request, pid, data=0):
    if libc.ptrace(request, pid, None, ctypes.c_void_p(data)) == -1:
        raise OSError(ctypes.get_errno(), 'ptrace failed')

def wait_event(pid):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        observed, status = os.waitpid(pid, os.WNOHANG | 0x40000000)
        if observed:
            return status
        select.select([], [], [], 0.005)
    raise TimeoutError('child event unavailable')

def main():
    guard = ROOT + '/guard-exit-probe-' + os.urandom(16).hex()
    created = os.open(guard, os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
    os.close(created)
    read_fd, write_fd = os.pipe()
    coordinator = child = None
    coordinator_fd = child_fd = competitor = None
    trace_attached = False
    try:
        if libc.prctl(36, 1, 0, 0, 0) != 0:
            raise OSError(ctypes.get_errno(), 'subreaper failed')
        coordinator = os.fork()
        if coordinator == 0:
            os.close(read_fd)
            lock_fd = os.open(guard, os.O_RDWR)
            fcntl.flock(lock_fd, fcntl.LOCK_EX)
            parent = os.getpid()
            target = os.fork()
            if target == 0:
                if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent:
                    os._exit(20)
                ready = threading.Event()
                def worker():
                    ready.set()
                    threading.Event().wait()
                threading.Thread(target=worker, daemon=True).start()
                ready.wait()
                os.write(write_fd, (str(os.getpid()) + '\n').encode())
                os.close(write_fd)
                while True:
                    signal.pause()
            os.close(write_fd)
            os.waitpid(target, 0)
            os._exit(0)
        os.close(write_fd)
        write_fd = None
        coordinator_fd = os.pidfd_open(coordinator)
        if not select.select([read_fd], [], [], 5)[0]:
            raise TimeoutError('child ready unavailable')
        child = int(os.read(read_fd, 64).strip())
        child_fd = os.pidfd_open(child)
        thread_count = len(os.listdir('/proc/%d/task' % child))
        assert thread_count >= 2
        trace(0x4206, child, 0x40 | 0x100000)  # SEIZE, TRACEEXIT, EXITKILL
        trace_attached = True
        signal.pidfd_send_signal(coordinator_fd, signal.SIGKILL)
        _, coordinator_status = os.waitpid(coordinator, 0)
        assert os.WIFSIGNALED(coordinator_status) and os.WTERMSIG(coordinator_status) == signal.SIGKILL
        coordinator = None
        status = wait_event(child)
        assert os.WIFSTOPPED(status) and os.WSTOPSIG(status) == signal.SIGTRAP and status >> 16 == 6
        exit_message = ctypes.c_ulong()
        trace(0x4201, child, ctypes.addressof(exit_message))
        assert exit_message.value == signal.SIGKILL
        assert not select.select([child_fd], [], [], 0)[0]
        competitor = os.open(guard, os.O_RDWR)
        try:
            fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as exc:
            assert exc.errno in (errno.EWOULDBLOCK, errno.EAGAIN)
        else:
            raise AssertionError('guard released before child exit')
        trace(7, child)
        final_status = wait_event(child)
        assert os.WIFSIGNALED(final_status) and os.WTERMSIG(final_status) == signal.SIGKILL
        trace_attached = False
        child = None
        assert select.select([child_fd], [], [], 0)[0]
        fcntl.flock(competitor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        print(json.dumps({'status':'PASS', 'threads_before_kill':thread_count, 'coordinator_exit_signal':9,
                          'child_exit_event_signal':exit_message.value, 'guard_contended_at_exit_stop':True,
                          'guard_acquired_after_child_exit':True, 'scope':'local own-descendant kernel probe only'}))
    finally:
        for descriptor in (coordinator_fd, child_fd):
            if descriptor is not None:
                try: signal.pidfd_send_signal(descriptor, signal.SIGKILL)
                except ProcessLookupError: pass
        if child is not None and trace_attached:
            try: trace(7, child)
            except OSError: pass
        for pid in (coordinator, child):
            if pid is not None:
                try: wait_event(pid)
                except (ChildProcessError, TimeoutError): pass
        for descriptor in (read_fd, write_fd, coordinator_fd, child_fd, competitor):
            if descriptor is not None:
                os.close(descriptor)
        os.unlink(guard)

if __name__ == '__main__':
    main()
