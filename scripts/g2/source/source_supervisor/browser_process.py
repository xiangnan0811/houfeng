"""Run the pinned browser only after permanently invalidating a negative realm."""
import hashlib
import os
import select
import signal
import time

from journal import JournalError, process_start
from origins import _identity, _thread_credentials

PROGRAM = '/opt/g2/browser/chrome-headless-shell'
ARGUMENTS = ['--headless', '--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage',
             '--disable-background-networking', '--no-first-run', '--no-default-browser-check',
             '--disable-breakpad', '--disable-crash-reporter', '--user-data-dir=/tmp/g2-browser-profile',
             '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=9222', 'about:blank']
ENVIRONMENT = ['LD_LIBRARY_PATH=/opt/g2/browser:/opt/g2/runtime/lib', 'HOME=/tmp', 'LANG=C',
               'LC_ALL=C', 'PATH=/nonexistent']
MEMORY_BYTES = 768 * 1024 * 1024


def exercise(life, postgres):
    if life.invalid or life.stage != 'source_maintenance':
        raise JournalError('browser negative requires a fresh source realm')
    if life.bpf is None:
        raise JournalError('browser BPF authority missing')
    image_id, expected_program = postgres.daemon['browser_image_id'], postgres.daemon['browser_program_sha256']
    action = {'scenario': 'extra-browser', 'image_id': image_id, 'program_sha256': expected_program,
              'source_content': False}
    life._admit('negative-browser-probe', action)
    life.invalidate('negative_extra_browser')
    config = {'Image': image_id, 'Entrypoint': [PROGRAM], 'Cmd': ARGUMENTS, 'User': '10002:10002',
              'Env': ENVIRONMENT, 'WorkingDir': '/tmp',
              'HostConfig': {'NetworkMode': 'none', 'ReadonlyRootfs': True, 'Privileged': False,
                             'CapDrop': ['ALL'], 'SecurityOpt': ['no-new-privileges'], 'PidsLimit': 128,
                             'Memory': MEMORY_BYTES, 'NanoCpus': 1000000000,
                             'RestartPolicy': {'Name': 'no'}, 'LogConfig': {'Type': 'none', 'Config': {}},
                             'Tmpfs': {'/tmp': 'rw,nosuid,nodev,noexec,size=134217728,mode=1777'}}}
    container_id = pidfd = None
    running = admitted = False
    try:
        created = postgres._request('POST', '/containers/create', config)
        container_id = created.get('Id') if type(created) is dict else None
        if (type(container_id) is not str or len(container_id) != 64 or
                any(ch not in '0123456789abcdef' for ch in container_id)):
            raise JournalError('negative browser container identity missing')
        life.bpf.admit_container(config, container_id, 'browser-negative')
        admitted = True
        postgres._request('POST', '/containers/' + container_id + '/start')
        running = True
        inspected = postgres._request('GET', '/containers/' + container_id + '/json')
        actual = inspected.get('Config') if type(inspected) is dict else None
        host = inspected.get('HostConfig') if type(inspected) is dict else None
        state = inspected.get('State') if type(inspected) is dict else None
        mounts = inspected.get('Mounts') if type(inspected) is dict else None
        networks = inspected.get('NetworkSettings', {}).get('Networks') if type(inspected) is dict else None
        if (type(actual) is not dict or type(host) is not dict or type(state) is not dict or
                inspected.get('Id') != container_id or inspected.get('Image') != image_id or
                actual.get('Image') != image_id or actual.get('Entrypoint') != [PROGRAM] or
                actual.get('Cmd') != ARGUMENTS or actual.get('User') != '10002:10002' or
                actual.get('WorkingDir') != '/tmp' or set(actual.get('Env') or []) != set(ENVIRONMENT) or
                state.get('Running') is not True or
                type(state.get('Pid')) is not int or state['Pid'] <= 0 or host.get('NetworkMode') != 'none' or
                host.get('ReadonlyRootfs') is not True or host.get('Privileged') is not False or
                host.get('CapDrop') != ['ALL'] or host.get('CapAdd') not in (None, []) or
                host.get('SecurityOpt') != ['no-new-privileges'] or
                any(host.get(name) == 'host' for name in ('PidMode', 'IpcMode', 'UTSMode', 'UsernsMode')) or
                host.get('PidsLimit') != 128 or host.get('Memory') != MEMORY_BYTES or
                host.get('NanoCpus') != 1000000000 or host.get('Tmpfs') != config['HostConfig']['Tmpfs'] or
                host.get('Binds') not in (None, []) or host.get('Devices') not in (None, []) or
                host.get('VolumesFrom') not in (None, []) or type(mounts) is not list or
                any(item.get('Type') != 'tmpfs' or item.get('Destination') != '/tmp' for item in mounts) or
                type(networks) is not dict or set(networks) - {'none'} or any(
                    endpoint.get('IPAddress') or endpoint.get('GlobalIPv6Address') for endpoint in networks.values())):
            raise JournalError('negative browser container restrictions differ')
        pid = state['Pid']
        pidfd = os.pidfd_open(pid, 0)
        identity = _identity(pid, full=True)
        if (identity is None or identity['exe_sha256'] != expected_program or not identity['argv'] or
                identity['argv'][0].decode('utf-8', 'strict') != PROGRAM or process_start(pid) != identity['starttime'] or
                select.select([pidfd], [], [], 0)[0]):
            raise JournalError('actual browser executable is not live')
        _thread_credentials(pid, 1)
        observation = {'container_id': container_id, 'image_id': image_id, 'pid': pid,
                       'starttime': identity['starttime'], 'executable_sha256': identity['exe_sha256'],
                       'argv_sha256': identity['argv_sha256'],
                       'cgroup_sha256': hashlib.sha256(identity['cgroup'].encode('ascii')).hexdigest(),
                       'uid': 10002, 'gid': 10002, 'no_new_privileges': True, 'capabilities_empty': True,
                       'network_mode': 'none', 'readonly_root': True, 'no_app_mounts': True,
                       'tmpfs_only': '/tmp', 'memory_bytes': MEMORY_BYTES, 'pids_limit': 128,
                       'pidfd_live': True}
        life._register('negative-browser-probe', observation)
        postgres._request('POST', '/containers/' + container_id + '/kill?signal=KILL')
        if not select.select([pidfd], [], [], 20)[0]:
            raise JournalError('actual browser process did not exit')
        waited = postgres._request('POST', '/containers/' + container_id + '/wait?condition=not-running', timeout=20)
        if type(waited) is not dict or waited.get('StatusCode') != 137:
            raise JournalError('browser container did not report its killed process')
        running = False
        postgres._request('DELETE', '/containers/' + container_id)
        completed_id, container_id = container_id, None
        if life.bpf.settle_container(completed_id, 'completed', time.monotonic() + 20) != 'retired':
            raise JournalError('browser BPF attachment settlement remains pending')
        try:
            life.roots.scan()
        except Exception:
            pass
        else:
            raise JournalError('browser realm invalidation did not remain sticky')
        return {'action': action, 'live': observation, 'pidfd_exited': True,
                'container_wait_status': waited['StatusCode'], 'sticky_invalid': True}
    finally:
        if running and pidfd is not None and not select.select([pidfd], [], [], 0)[0]:
            signal.pidfd_send_signal(pidfd, signal.SIGKILL)
            select.select([pidfd], [], [], 20)
        if container_id is not None:
            try:
                deadline = time.monotonic() + 20
                if pidfd is not None and select.select([pidfd], [], [], 0)[0]:
                    waited = postgres._request('POST', '/containers/' + container_id + '/wait?condition=not-running',
                                               deadline=deadline)
                    if type(waited) is not dict or type(waited.get('StatusCode')) is not int:
                        raise JournalError('browser cleanup wait is uncertain')
                    postgres._request('DELETE', '/containers/' + container_id + '?force=1', deadline=deadline)
                    if admitted:
                        life.bpf.settle_container(container_id, 'failed', deadline)
                else:
                    postgres._request('DELETE', '/containers/' + container_id + '?force=1', deadline=deadline)
            except Exception:
                pass
        if pidfd is not None:
            os.close(pidfd)
