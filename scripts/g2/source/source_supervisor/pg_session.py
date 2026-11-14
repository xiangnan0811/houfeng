"""One registered negative fixture holding an actual extra APP backend."""
import select
import threading
import time

import pg_bootstrap
from journal import JournalError


class ExtraSession:
    def __init__(self, life, password, host, purpose='negative-extra-session', expected_state='idle'):
        if (purpose, expected_state) not in (('negative-extra-session', 'idle'),
                                             ('negative-dump-lock', 'idle in transaction')):
            raise JournalError('held backend purpose invalid')
        self.life, self.password, self.host = life, password, host
        self.purpose, self.expected_state = purpose, expected_state
        self.ready, self.release = threading.Event(), threading.Event()
        self.backend_pid, self.error = None, None
        self.thread = None
        life._admit(purpose, {'database': 'houfeng', 'role': 'postgres',
                              'application_name': 'g2_root_identity'})

    def _hold(self, sock, backend_pid, deadline):
        self.backend_pid = backend_pid
        self.ready.set()
        while not self.release.wait(0.05):
            if time.monotonic() >= deadline or select.select([sock], [], [], 0)[0]:
                raise JournalError('extra backend did not remain live')

    def _run(self):
        try:
            pg_bootstrap._run('app', self.password, 'identity', host_override=self.host, session_hold=self._hold)
        except BaseException as exc:
            self.error = exc
            self.ready.set()

    def open(self):
        if self.thread is not None:
            raise JournalError('extra backend already started')
        self.thread = threading.Thread(target=self._run, name='g2-extra-pg-session', daemon=True)
        self.thread.start()
        if not self.ready.wait(10) or self.error is not None or type(self.backend_pid) is not int or self.backend_pid <= 0:
            raise JournalError('extra PostgreSQL backend did not authenticate')
        query = ("SELECT json_build_object('pid',pid,'role',usename,'application_name',application_name,"
                 "'state',state,'backend_start',backend_start::text)::text AS state FROM pg_stat_activity "
                 "WHERE backend_type='client backend' AND datname=current_database() AND pid=%d\0" % self.backend_pid).encode('ascii')
        observed = pg_bootstrap._run('app', self.password, 'records', query_override=query, host_override=self.host)
        if (type(observed) is not dict or set(observed) != {'pid', 'role', 'application_name', 'state', 'backend_start'} or
                observed['pid'] != self.backend_pid or observed['role'] != 'postgres' or
                observed['application_name'] != 'g2_root_identity' or observed['state'] != self.expected_state or
                type(observed['backend_start']) is not str or not observed['backend_start']):
            raise JournalError('extra backend independent readback differs')
        self.life._register(self.purpose, observed)
        return observed

    def close(self):
        self.release.set()
        if self.thread is None:
            return
        self.thread.join(5)
        if self.thread.is_alive() or self.error is not None:
            raise JournalError('extra PostgreSQL backend was not joined')
