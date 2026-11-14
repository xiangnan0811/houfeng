"""Fixed guest-only C80 realm/launcher interface shared with the coordinator."""

RUN_TOKEN = 'ddac5da9673f4c9ba7d2ad68570fd82d'
MAINTENANCE_CONFIG = '/var/lib/g2/control/maintenance/source-authority.json'
LEDGER_DIR = '/var/lib/g2/control/maintenance/fences/record-ledger'
GUARD = '/var/lib/g2/control/maintenance/guard/maintenance.lock'
REALM_CONTROL = '/var/lib/g2/realm-control'
CHILD_CGROUP = '/sys/fs/cgroup/g2-c80-predecessor'
MIGRATION_CGROUP = '/sys/fs/cgroup/g2-c81-migration'
MIGRATION_BINDING = '/opt/g2/c81-migration.json'
LAUNCHER = '/opt/g2/c80-launcher'
PREDECESSOR = '/opt/g2/c80-predecessor'
ADMIN = '/opt/g2/houfeng-record-platform-admin'
AUTHORITY_STATE = '/var/lib/g2/control/maintenance/authority'
COMPOSE_DIR = '/opt/g2/compose'
APP_INITIAL_NETWORK = 'g2-initial-app'
LEDGER_NETWORK = 'g2-ledgers'
INITIAL_NETWORKS = {
    APP_INITIAL_NETWORK: ('172.30.80.0/24', '172.30.80.1'),
    LEDGER_NETWORK: ('172.30.82.0/24', '172.30.82.1'),
}
POSTGRES_HOSTS = {'app': '172.30.80.2', 'w1': '172.30.82.2',
                  'w2': '172.30.82.3', 'control': '172.30.82.4'}
POSTGRES_PORT = 5432
CASES = ('realm-negative', 'source-vps', 'source-monitoring-instance', 'source-target', 'source-recovery')
NEGATIVES = ('observer-capability', 'bpf-authorization', 'bpf-broker-kill', 'genesis-missing', 'late-genesis', 'root-restart', 'extra-browser', 'extra-http',
             'extra-backup-producer', 'forbidden-qmp', 'extra-endpoint', 'directory-replacement',
             'directory-hidden', 'directory-symlink', 'directory-unreadable',
             'other-record-comparison', 'intersecting-source-closure', 'extra-session', 'stale-rpc',
             'wrong-rpc', 'coordinator-kill', 'published-backup', 'staging-backup')
CALLBACK_CUTS = ('create-intent', 'create-commit-before-result', 'preview-ack-loss',
                 'reserve-intent', 'reserve-commit-before-stdout', 'stdout-loss')
RECOVERY = ('app-commit', 'w1-commit', 'w2-commit', 'anchor-fsync', 'c81-convergence-commit',
            'handoff-install', 'handoff-return', 'conflicting-tuple', 'missing-journal',
            'app-rollback', 'unsettled-backend', 'root-kill', 'vm-kill') + tuple(
                cut + '-' + victim for cut in CALLBACK_CUTS for victim in ('child', 'coordinator'))


def scenarios(case):
    if case == 'realm-negative':
        return NEGATIVES
    if case == 'source-recovery':
        return RECOVERY
    if case in ('source-vps', 'source-monitoring-instance', 'source-target'):
        return ('normal',)
    return ()

PHASE_KEYS = frozenset(('version', 'generation', 'previous', 'recovery_id', 'application', 'phase'))
APPLICATION_KEYS = frozenset(('ID', 'SystemIdentifier', 'DatabaseName', 'DatabaseOID', 'Epoch'))
COORDINATOR_KEYS = frozenset(('pid', 'starttime', 'executable_sha256'))
GUARD_KEYS = frozenset(('dev', 'ino'))
CGROUP_KEYS = frozenset(('path', 'dev', 'ino'))
INTENT_KEYS = frozenset(('version', 'phase_generation', 'recovery_id', 'application', 'realm_digest',
                         'coordinator', 'guard', 'config_fd', 'config_digest', 'actor_user_id', 'record_id',
                         'child_cgroup', 'launcher_sha256', 'predecessor_sha256'))
CONFIG_KEYS = frozenset(('version', 'phase_generation', 'runtime_url', 'deployment_id', 'project_id',
                         'actor_user_id', 'record_id'))
SUPERVISOR_KEYS = frozenset(('pid', 'starttime', 'program_sha256'))
PERMIT_KEYS = INTENT_KEYS | frozenset(('run_token', 'boot_id', 'supervisor'))
OBSERVE_KEYS = frozenset(('version', 'op', 'phase_generation', 'nonce'))
OBSERVATION_KEYS = frozenset(('version', 'op', 'phase_generation', 'nonce', 'observation_sequence',
                              'realm_digest', 'roots_digest', 'valid'))
MAX_FRAME = 16 * 1024


def phase_name(generation):
    return 'phase-%020d.json' % generation


def callback_intent_name(generation):
    return 'predecessor-callback-intent-%020d.json' % generation


def launch_authority_name(generation):
    return 'launch-authority-%020d.json' % generation


def launch_used_name(generation):
    return 'launch-used-%020d' % generation
