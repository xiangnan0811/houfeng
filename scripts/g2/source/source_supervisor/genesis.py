"""Publish fresh realm genesis while all four real PGDATA directories are empty."""
import hashlib
import os
import stat
import time

import guest_bootstrap
import network_verify
from census import _mount_id
from guest_storage import BASE, NAMES
from journal import JournalError, boot_id, process_start


def publish(journal, case, seed, volumes, roots, origins, installed, daemon, systemd_state, authority):
    deadline = time.monotonic() + 30
    with authority.observation(deadline):
        return _publish(journal, case, seed, volumes, roots, origins, installed, daemon, systemd_state,
                        authority, deadline)


def _publish(journal, case, seed, volumes, roots, origins, installed, daemon, systemd_state, authority, deadline):
    if os.geteuid() != 0 or os.getegid() != 0 or set(volumes) != set(NAMES):
        raise JournalError('fresh realm genesis identity unavailable')
    dns = installed.get('dns_preflight')
    if (type(dns) is not dict or dns.get('version') != 1 or dns.get('program_sha256') != installed['programs']['dns-probe'] or
            dns.get('probe_joined') is not True or dns.get('endpoint_removed') is not True or
            dns.get('query', {}).get('upstream_refused') is not True):
        raise JournalError('pre-genesis DNS isolation evidence unavailable')
    binding = guest_bootstrap.verify_migration_binding(installed)
    if binding['boot_id'] != boot_id() or network_verify.migration_scope(installed['migration_cgroup']) != {
            'members': [], 'populated': 0}:
        raise JournalError('migration installation was not pinned empty at genesis')
    for name in NAMES:
        item = volumes[name]
        fd = os.open(BASE + '/' + name + '/pgdata', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(fd)
            if ((info.st_dev, info.st_ino, _mount_id(fd)) !=
                    (item['pgdata_dev'], item['pgdata_ino'], item['pgdata_mount_id']) or
                    info.st_uid != 0 or info.st_gid != 0 or stat.S_IMODE(info.st_mode) != 0o700 or os.listdir(fd)):
                raise JournalError('genesis is missing or later than first database initialization')
        finally:
            os.close(fd)
    roots_digest = roots.scan()
    supervisor_exe = os.open('/proc/self/exe', os.O_RDONLY | os.O_CLOEXEC)
    try:
        digest = hashlib.sha256()
        while True:
            block = os.read(supervisor_exe, 1024 * 1024)
            if not block:
                break
            digest.update(block)
    finally:
        os.close(supervisor_exe)
    facts = {'version': 1, 'run_token': case['run_token'], 'case': case['case'], 'scenario': case['scenario'],
             'boot_id': boot_id(), 'supervisor_pid': os.getpid(), 'supervisor_starttime': process_start(os.getpid()),
             'supervisor_executable_sha256': digest.hexdigest(), 'host_launch_sha256': case['host_launch_sha256'],
             'host_argv_sha256': case['host_argv_sha256'], 'qemu_sha256': case['qemu_sha256'],
             'payload_case_sha256': case['payload_case_sha256'],
             'seed_descriptor_sha256': case['seed_descriptor_sha256'],
             'seed_manifest_sha256': seed['SHA256SUMS'], 'supervisor_manifest_sha256': seed['supervisor-manifest.json'],
             'programs': installed['programs'], 'child_cgroup': installed['child_cgroup'],
             'writer_cgroup': installed['writer_cgroup'], 'migration_cgroup': installed['migration_cgroup'],
             'migration_binding': installed['migration_binding'], 'volumes': volumes, 'roots': roots.identities(),
             'roots_digest': roots_digest, 'systemd': systemd_state, 'origins': origins.genesis_inventory(),
             'docker': {key: daemon[key] for key in ('pid', 'starttime', 'socket_dev', 'socket_ino', 'image_id')},
             'dns_preflight': dns}
    facts['bpf_authority'] = authority.bootstrap_evidence(case, deadline)
    journal.genesis(facts)
    return facts
